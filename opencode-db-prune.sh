#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# opencode-db-prune.sh — drop superseded snapshot rows from opencode's event log.
#
# WHY THIS EXISTS
#   opencode's `event` table is append-only with no retention. Three event types
#   store a FULL cumulative snapshot of an entity on every streaming update, so a
#   single message or tool call accumulates hundreds or thousands of rows of which
#   only the last is current. Upstream: anomalyco/opencode#41175, #47223, #47729,
#   #46833, #33356, #31526. PR #43456 would add `db prune`/`db vacuum` but is
#   unmerged.
#
# WHAT IT DELETES
#   ONLY rows in `event`, of these three types, that are NOT the newest row for
#   their entity:
#     message.updated.1       entity = json_extract(data,'$.info.id')     (msg_*)
#     message.part.updated.1  entity = json_extract(data,'$.part.id')     (prt_*)
#     session.updated.1       entity = json_extract(data,'$.sessionID')   (ses_*)
#   The final snapshot per entity is always kept.
#
# WHAT IT NEVER TOUCHES
#   session, message, part, event_sequence, or any other event type.
#   `message`/`part` hold the content the TUI actually renders, so pruning does
#   not change what any session displays, at any age.
#
# WHY THAT IS SAFE
#   Projectors run synchronously at write time in the same transaction as the
#   insert (core/src/event.ts), and are latest-wins onConflictDoUpdate
#   (core/src/session/projector.ts). message/part are already materialized and
#   are never rebuilt by replaying events. EventTable is read by only four
#   non-test files: event.ts, event/sql.ts, the sync handler, and
#   control-plane/workspace.ts (remote session warp). None is in the session
#   open/render path.
#
#   event_sequence is left alone deliberately: new events take their seq from
#   event_sequence.seq, not MAX(event.seq), so numbering stays correct across
#   the gaps this creates.
#
# WHAT YOU GIVE UP
#   replayAll() requires contiguous seqs and dies on gaps. That path is used by
#   remote-workspace sync/warp, so any aggregate with event_sequence.owner_id
#   set is excluded here. You also lose the ability to rebuild message/part from
#   the log if a projection ever drifted. That loss is permanent -- unless you
#   kept an --archive, which is what --restore-session reads back.
#
# NET vs GROSS -- WHY THE FULL BACKUP MUST BE TRANSIENT
#   The obvious mistake is to take the full `.backup` copy, prune, vacuum, and
#   then KEEP the backup "just in case". Do the arithmetic on a 40 GB database:
#
#       40 GB  pruned+vacuumed down to  24 GB
#     + 40 GB  retained full backup
#     = 64 GB  ... which is MORE than the 40 GB you started with.
#
#   That is gross reclaim masquerading as progress. The full backup exists for
#   exactly one reason -- crash safety for the minutes the delete is running --
#   and should be deleted as soon as opencode starts cleanly on the pruned file.
#
#   What is worth RETAINING is --archive: a compressed dump of ONLY the rows
#   that were deleted. Measured on real event rows, zstd -19 gets 4.5x and
#   gzip -6 gets 3.0x, so the same 16 GB of deleted rows is ~3.6 GB on disk
#   instead of a 40 GB whole-database copy. The dry-run report prints both the
#   gross and the net numbers so you can see this before committing.
#
#   Under --protect-roots the archive is usually unnecessary anyway: every
#   pruned session is a subagent session, and the only thing restoring buys you
#   is the ability to warp a pruned session -- which you do not do to subagent
#   sessions. Keep the archive only if you specifically want that escape hatch.
#
# ORDER OF OPERATIONS
#   ./opencode-db-prune.sh                 # dry run, changes nothing
#   ./opencode-db-prune.sh --apply         # prune
#   ./opencode-db-vacuum.sh --auto-vacuum  # reclaim the freed pages
#   rm <db>.prune-backup                   # once opencode starts cleanly
#
#   ./opencode-db-prune.sh --restore-session ses_X --from <archive|backup>
#                                          # put one session's rows back

set -euo pipefail

DB_DEFAULT="${HOME}/.local/share/opencode/opencode.db"
DB="$DB_DEFAULT"
PROTECT_DAYS=30
PROTECT_ROOTS=0
APPLY=0
BACKUP=""
DO_BACKUP=1
BATCH=50000
ASSUME_YES=0
ARCHIVE=""
ARCHIVE_TMP=""
RESTORE_SESSION=""
FROM=""
WORKDIR=""

# Archive size model, used only for the dry-run estimate.
#   RATIO       measured on real event rows (zstd -19 = 4.5x, gzip -6 = 3.0x)
#   ESCAPE      `data` is itself JSON, so wrapping it in JSON doubles every
#               quote; ~10% growth before compression
#   ENVELOPE    bytes per line for {"id":..,"aggregate_id":..,"seq":..,"type":..}
ARCHIVE_ESCAPE_FACTOR=1.10
ARCHIVE_ENVELOPE_BYTES=110

die() { printf 'opencode-db-prune: %s\n' "$*" >&2; exit 1; }
note() { printf '  %s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }
gb() { awk -v b="$1" 'BEGIN{ printf "%.2f GB", b/1073741824 }'; }
commas() { printf "%'d" "$1" 2>/dev/null || printf '%s' "$1"; }

# gb() is right for a 40 GB database but prints "0.00 GB" for an archive of a
# test database, so new output uses an auto-scaling unit instead.
hsize() {
  awk -v b="$1" 'BEGIN{
    if (b >= 1073741824)   printf "%.2f GB", b/1073741824;
    else if (b >= 1048576) printf "%.2f MB", b/1048576;
    else if (b >= 1024)    printf "%.2f KB", b/1024;
    else                   printf "%d B", b;
  }'
}

filesize() { wc -c <"$1" | tr -d ' '; }

usage() {
  cat <<'EOF'
Usage: ./opencode-db-prune.sh [options]
       ./opencode-db-prune.sh --restore-session ses_XXXX --from PATH [--apply]

Deletes superseded snapshot rows from opencode's `event` table. Sessions,
messages and parts are never touched, so every session stays fully readable.

Options:
  --db PATH            Database (default ~/.local/share/opencode/opencode.db)
  --protect-days N     Skip sessions active within the last N days.
                       Default 30. Use 0 to prune everything eligible.
                       NOTE: age does not affect readability -- this is a
                       comfort margin, and it is expensive. See --dry-run.
  --protect-roots      Skip ALL root sessions (parent_id IS NULL), pruning only
                       subagent/child sessions. Warp operates on the session you
                       interact with, which is always a root, so this removes
                       essentially all of the warp risk while still reclaiming
                       the bulk of the space -- subagent sessions hold far more
                       event data than roots do. Combines with --protect-days.
  --apply              Actually delete. Without this the script only reports.
  --backup PATH        Backup destination (default: <db>.prune-backup).
                       This copy is TRANSIENT crash insurance, not an archive:
                       it is as large as the whole database, so retaining it
                       leaves you using MORE disk than before you pruned.
                       Delete it once opencode starts cleanly. See --archive.
  --no-backup          Skip the backup. Not recommended.
  --archive PATH       Before deleting, dump ONLY the rows about to be deleted
                       to a compressed NDJSON archive at PATH. This is the
                       thing worth KEEPING: it holds the same data at ~4.5x
                       compression (zstd -19, or ~3.0x with gzip -6 if zstd is
                       not on PATH) instead of copying the whole database.
                       Written to PATH.partial and renamed only on success, and
                       verified by decompressing it and counting rows, so the
                       delete never runs against a failed archive.
                       Read back with --restore-session.
  --batch N            Rows per delete transaction (default 50000).
  -y, --yes            Do not prompt before deleting.
  -h, --help           This text.

Restore mode (standalone -- prunes nothing):
  --restore-session ID Re-insert one session's pruned `event` rows so that its
                       seq values become contiguous again and it can be warped.
                       Dry run unless --apply, and --apply requires opencode to
                       be stopped, same as pruning. Uses INSERT OR IGNORE, so
                       re-running is safe and existing rows are never rewritten.
                       Reports whether the session's seqs ended up contiguous.
  --from PATH          Source for --restore-session. Either a full `.backup`
                       SQLite file or an --archive file (zstd or gzip);
                       auto-detected from the file's magic bytes.

Always excluded:
  * any aggregate with event_sequence.owner_id set (remote-sync sessions --
    replay requires contiguous seqs)
  * the newest snapshot for every entity
  * every event type other than the three snapshot types

Net, not gross:
  Pruning 16 GB out of a 40 GB database and then keeping the 40 GB backup
  leaves you at 64 GB. The dry-run report prints the net reclaim for both
  "backup deleted afterward" and "archive retained" so the real arithmetic is
  visible before you commit. Under --protect-roots the archive is usually
  pointless -- every pruned session is a subagent session, and restoring only
  buys back the ability to warp, which you do not do to subagent sessions.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --db) DB="${2:?--db needs a path}"; shift 2 ;;
    --protect-days) PROTECT_DAYS="${2:?--protect-days needs a number}"; shift 2 ;;
    --protect-roots) PROTECT_ROOTS=1; shift ;;
    --apply) APPLY=1; shift ;;
    --backup) BACKUP="${2:?--backup needs a path}"; shift 2 ;;
    --no-backup) DO_BACKUP=0; shift ;;
    --archive) ARCHIVE="${2:?--archive needs a path}"; shift 2 ;;
    --restore-session) RESTORE_SESSION="${2:?--restore-session needs a session id}"; shift 2 ;;
    --from) FROM="${2:?--from needs a path}"; shift 2 ;;
    --batch) BATCH="${2:?--batch needs a number}"; shift 2 ;;
    -y|--yes) ASSUME_YES=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
done

case "$PROTECT_DAYS" in (*[!0-9]*|'') die "--protect-days must be a non-negative integer" ;; esac
case "$BATCH" in (*[!0-9]*|'') die "--batch must be a positive integer" ;; esac
[ "$BATCH" -gt 0 ] || die "--batch must be > 0"

# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

# Deleting or inserting under a live writer risks tearing an in-flight stream's
# snapshot sequence, so any write mode refuses while opencode is running.
check_not_running() {
  local running
  # pgrep exits 1 when nothing matches, and under `pipefail` a bare assignment
  # would inherit that and abort the script -- i.e. exactly when opencode IS
  # stopped, which is the only case in which --apply is allowed to proceed.
  running="$(pgrep -f '(^|/)opencode' 2>/dev/null | wc -l | tr -d ' ' || true)"
  running="${running:-0}"
  [ "$running" -gt 0 ] || return 0

  # Only a writer holding THIS database matters. Guarding on "any opencode
  # process" makes the script untestable against a scratch --db, and would also
  # block work on a copy while an unrelated session is open. lsof is the direct
  # question; if it is unavailable, fall back to the conservative check.
  if command -v lsof >/dev/null 2>&1; then
    holders="$(lsof -t -- "$DB" 2>/dev/null | wc -l | tr -d ' ' || true)"
    holders="${holders:-0}"
    if [ "$holders" -eq 0 ]; then
      [ "$APPLY" -eq 1 ] && note "note: $running opencode process(es) running, but none has $DB open"
      return 0
    fi
    running="$holders"
  fi

  if [ "$APPLY" -eq 1 ]; then
    printf '\n' >&2
    # Truncate: a matching sqlite3 argv can be an entire multi-line query.
    pgrep -lf '(^|/)opencode' 2>/dev/null | cut -c1-120 | head -10 >&2 || true
    die "$running opencode process(es) running. Writing under a live writer risks
    corrupting snapshots an in-flight stream is still emitting.
    Stop them first:  pkill -f opencode && sleep 5 && pgrep -lf opencode"
  fi
  note "NOTE: $running opencode process(es) running. Dry run is read-only and safe;"
  note "      --apply would refuse until they are stopped."
}

# zstd is preferred purely on measured ratio (4.5x vs 3.0x on real event rows).
# Both are set up to read stdin and write stdout so the call sites are identical.
COMPRESSOR=""
COMPRESS_CMD=()
DECOMPRESS_CMD=()
ARCHIVE_RATIO=1
pick_compressor() {
  if command -v zstd >/dev/null 2>&1; then
    COMPRESSOR='zstd -19'
    COMPRESS_CMD=(zstd -19 -T0 -q -c)
    DECOMPRESS_CMD=(zstd -dc)
    ARCHIVE_RATIO=4.5
  elif command -v gzip >/dev/null 2>&1; then
    COMPRESSOR='gzip -6'
    COMPRESS_CMD=(gzip -6 -c)
    DECOMPRESS_CMD=(gzip -dc)
    ARCHIVE_RATIO=3.0
  else
    die "neither zstd nor gzip found on PATH -- cannot build an archive"
  fi
}

# Auto-detect what --from actually is, rather than trusting the extension.
source_kind() {
  local m
  m="$(od -An -N16 -tx1 "$1" 2>/dev/null | tr -d ' \n')"
  case "$m" in
    53514c69746520666f726d6174203300*) printf 'sqlite' ;;  # "SQLite format 3\0"
    28b52ffd*)                         printf 'zstd' ;;    # zstd frame magic
    1f8b*)                             printf 'gzip' ;;
    *)                                 printf 'unknown' ;;
  esac
}

# SQLite's URI parser only treats % ? and # specially inside the path, and the
# result still has to survive being pasted into a single-quoted SQL literal.
sql_uri_literal() {
  printf '%s' "$1" | sed -e 's/%/%25/g' -e 's/?/%3f/g' -e 's/#/%23/g' -e "s/'/''/g"
}

# ---------------------------------------------------------------------------
# Restore mode
#
# Standalone: prunes nothing, only puts rows back. The point is seq contiguity
# -- replayAll() (remote sync / session warp) dies on gaps, so a pruned session
# cannot be warped until its deleted snapshots are reinstated.
# ---------------------------------------------------------------------------
restore_session_mode() {
  step "Preflight (restore)"
  command -v sqlite3 >/dev/null || die "sqlite3 not found on PATH"
  [ -f "$DB" ] || die "no database at $DB"
  [ -n "$FROM" ] || die "--restore-session requires --from <archive-or-backup-path>"
  [ -f "$FROM" ] || die "no such --from source: $FROM"
  # The id is interpolated into SQL, so refuse anything that is not an id.
  case "$RESTORE_SESSION" in
    (*[!A-Za-z0-9_-]*|'') die "--restore-session takes a bare session id (got: $RESTORE_SESSION)" ;;
  esac

  local kind
  kind="$(source_kind "$FROM")"
  note "database      : $DB"
  note "session       : $RESTORE_SESSION"
  note "source        : $FROM ($(hsize "$(filesize "$FROM")"))"
  note "source type   : $kind"
  note "mode          : $([ "$APPLY" -eq 1 ] && echo 'APPLY (will insert)' || echo 'dry run (read-only)')"
  [ "$kind" = "unknown" ] && die "cannot identify $FROM -- expected a SQLite file, a zstd archive, or a gzip archive"

  check_not_running

  sqlite3 -readonly "file:${DB}?mode=ro" "PRAGMA table_info(event);" \
    | cut -d'|' -f2 | grep -qx 'aggregate_id' \
    || die "event table shape unexpected -- refusing to run"

  local before_rows
  before_rows="$(sqlite3 -readonly "file:${DB}?mode=ro" \
    "SELECT COUNT(*) FROM event WHERE aggregate_id = '${RESTORE_SESSION}';")"
  note "rows now      : $(commas "$before_rows") event row(s) for this session"

  local candidates inserted
  if [ "$kind" = "sqlite" ]; then
    restore_from_sqlite
  else
    restore_from_archive "$kind"
  fi
  candidates="$RESTORE_CANDIDATES"
  inserted="$RESTORE_INSERTED"

  step "Result"
  if [ "$APPLY" -eq 0 ]; then
    note "would restore : $(commas "$candidates") row(s)"
  else
    note "restored      : $(commas "$inserted") row(s)"
  fi
  note "untouched     : event_sequence, session, message, part"

  # Contiguity is the whole reason to restore, so report it explicitly rather
  # than leaving the user to work out whether warp will now succeed. In dry run
  # this describes the CURRENT state, which is by definition still gappy.
  local stats mn mx cnt hw
  stats="$(sqlite3 -readonly "file:${DB}?mode=ro" \
    "SELECT COALESCE(MIN(seq),0), COALESCE(MAX(seq),0), COUNT(*) FROM event WHERE aggregate_id = '${RESTORE_SESSION}';")"
  IFS='|' read -r mn mx cnt <<EOF
$stats
EOF
  # Internal contiguity is NOT sufficient. replayAll walks the aggregate from
  # the beginning up to the event_sequence high-water mark, so a run of rows
  # that is dense but starts above 0, or stops below the high-water mark, still
  # dies. A single surviving row trivially satisfies count == max-min+1, which
  # is exactly the false positive this avoids.
  hw="$(sqlite3 -readonly "file:${DB}?mode=ro" \
    "SELECT COALESCE(MAX(seq),-1) FROM event_sequence WHERE aggregate_id = '${RESTORE_SESSION}';")"
  step "Sequence contiguity for ${RESTORE_SESSION}"
  note "min seq        : $(commas "$mn")"
  note "max seq        : $(commas "$mx")"
  note "count          : $(commas "$cnt")"
  note "high-water mark: $(commas "$hw")  (event_sequence.seq)"
  if [ "$cnt" -eq 0 ]; then
    note "status   : no events for this session"
  elif [ "$cnt" -ne $(( mx - mn + 1 )) ]; then
    note "status   : GAPPY -- $(commas $(( mx - mn + 1 - cnt ))) row(s) missing inside the run; warp/replay will fail"
  elif [ "$mn" -ne 0 ]; then
    note "status   : INCOMPLETE -- dense but starts at $(commas "$mn"), not 0; warp/replay will fail"
  elif [ "$hw" -ge 0 ] && [ "$mx" -ne "$hw" ]; then
    note "status   : INCOMPLETE -- ends at $(commas "$mx") but high-water mark is $(commas "$hw"); warp/replay will fail"
  else
    note "status   : COMPLETE (0..$(commas "$mx"), no gaps) -- warp/replay should work"
  fi

  if [ "$APPLY" -eq 0 ]; then
    cat <<EOF

Dry run only -- nothing was changed.

To restore:  ./opencode-db-prune.sh --restore-session ${RESTORE_SESSION} --from ${FROM} --apply
EOF
  fi
}

RESTORE_CANDIDATES=0
RESTORE_INSERTED=0

# A full .backup is just a database, so ATTACH it and copy rows across. The
# connection's read-only flag propagates to attached files, and apply mode
# attaches with an explicit mode=ro URI so the source is never written either.
restore_from_sqlite() {
  local uri
  uri="$(sql_uri_literal "$FROM")"

  sqlite3 -readonly "file:${DB}?mode=ro" "ATTACH 'file:${uri}?mode=ro' AS src; SELECT 1 FROM src.event LIMIT 1;" >/dev/null 2>&1 \
    || sqlite3 -readonly "file:${DB}?mode=ro" "ATTACH 'file:${uri}?mode=ro' AS src; SELECT COUNT(*) FROM src.event;" >/dev/null \
    || die "$FROM is a SQLite file but has no readable 'event' table"

  step "Scanning backup"
  RESTORE_CANDIDATES="$(sqlite3 -readonly "file:${DB}?mode=ro" <<SQL
ATTACH 'file:${uri}?mode=ro' AS src;
SELECT COUNT(*) FROM src.event s
WHERE s.aggregate_id = '${RESTORE_SESSION}'
  AND NOT EXISTS (SELECT 1 FROM main.event m WHERE m.id = s.id)
  AND NOT EXISTS (SELECT 1 FROM main.event m WHERE m.aggregate_id = s.aggregate_id AND m.seq = s.seq);
SQL
)"
  note "restorable rows in source: $(commas "${RESTORE_CANDIDATES:-0}")"

  [ "$APPLY" -eq 1 ] || return 0

  step "Inserting"
  # INSERT OR IGNORE: re-running is a no-op and any row already present wins.
  # foreign_keys=ON so a row whose event_sequence parent is gone fails loudly
  # rather than landing as an orphan (OR IGNORE does not suppress FK errors).
  RESTORE_INSERTED="$(sqlite3 "$DB" <<SQL
PRAGMA foreign_keys=ON;
ATTACH 'file:${uri}?mode=ro' AS src;
INSERT OR IGNORE INTO main.event (id, aggregate_id, seq, type, data)
SELECT s.id, s.aggregate_id, s.seq, s.type, s.data
FROM src.event s WHERE s.aggregate_id = '${RESTORE_SESSION}';
SELECT changes();
SQL
)"
}

# Archive source: stream-decompress, cheaply pre-filter to this session's lines
# with a fixed-string grep, then let SQL do the authoritative filtering.
restore_from_archive() {
  local kind="$1"
  case "$kind" in
    zstd) command -v zstd >/dev/null 2>&1 || die "$FROM is a zstd archive but zstd is not on PATH"
          DECOMPRESS_CMD=(zstd -dc) ;;
    gzip) command -v gzip >/dev/null 2>&1 || die "$FROM is a gzip archive but gzip is not on PATH"
          DECOMPRESS_CMD=(gzip -dc) ;;
  esac

  WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/opencode-db-prune.XXXXXX")"
  trap 'rm -rf "$WORKDIR"' EXIT

  local lines="$WORKDIR/lines.jsonl"
  local pat="\"aggregate_id\":\"${RESTORE_SESSION}\""

  step "Scanning archive"
  note "decompressing with: ${DECOMPRESS_CMD[*]}"
  # grep exits 1 on "no matches", which is not an error here, and under pipefail
  # that would be indistinguishable from a decompressor failure -- so the
  # decompressor's own status is stashed separately. LC_ALL=C + -a because
  # `data` may hold bytes that are not valid UTF-8.
  local grc=0
  (
    set +o pipefail
    { "${DECOMPRESS_CMD[@]}" "$FROM"; printf '%s' "$?" >"$WORKDIR/decomp.rc"; } \
      | LC_ALL=C grep -aF -- "$pat" >"$lines"
  ) || grc=$?
  # grep exit 1 means "this session has nothing in the archive", which is a
  # legitimate answer; 2+ is a real failure. The decompressor is judged on the
  # status it stashed, since the pipeline only ever reports grep's.
  [ "$grc" -le 1 ] || die "filtering $FROM failed (grep exit $grc)"
  local rc
  rc="$(cat "$WORKDIR/decomp.rc" 2>/dev/null || echo 1)"
  [ "$rc" -eq 0 ] || die "decompressing $FROM failed (exit $rc)"
  [ -f "$lines" ] || : >"$lines"

  local matched
  matched="$(wc -l <"$lines" | tr -d ' ')"
  note "candidate line(s): $(commas "$matched")"
  if [ "$matched" -eq 0 ]; then
    RESTORE_CANDIDATES=0
    return 0
  fi

  # Staging load. `.mode ascii` makes .import byte-literal (no CSV quote
  # rewriting), and a tab column separator is safe because json_object escapes
  # every control character, so a raw tab cannot occur inside a line.
  local import_sql
  import_sql="$(cat <<SQL
.mode ascii
.separator "\t" "\n"
CREATE TEMP TABLE stg(j TEXT);
.import '${lines}' stg
SQL
)"

  # Both sanity checks in one pass, because each sqlite3 invocation re-imports
  # the staging file. json_valid is checked before anything calls json_extract,
  # which would abort the whole statement on a malformed line.
  local checks staged bad
  checks="$(sqlite3 -readonly "file:${DB}?mode=ro" <<SQL
${import_sql}
SELECT COUNT(*) FROM stg;
SELECT COUNT(*) FROM stg WHERE json_valid(j) = 0;
SQL
)"
  staged="$(printf '%s\n' "$checks" | sed -n 1p)"
  bad="$(printf '%s\n' "$checks" | sed -n 2p)"
  [ "${staged:-0}" -eq "$matched" ] \
    || die "staged ${staged:-0} line(s) but grep matched ${matched} -- archive is malformed, refusing to insert"
  [ "${bad:-0}" -eq 0 ] || die "${bad} archive line(s) are not valid JSON -- refusing to insert"

  RESTORE_CANDIDATES="$(sqlite3 -readonly "file:${DB}?mode=ro" <<SQL
${import_sql}
SELECT COUNT(*) FROM stg s
WHERE json_extract(s.j,'\$.aggregate_id') = '${RESTORE_SESSION}'
  AND NOT EXISTS (SELECT 1 FROM main.event m WHERE m.id = json_extract(s.j,'\$.id'))
  AND NOT EXISTS (SELECT 1 FROM main.event m
                  WHERE m.aggregate_id = json_extract(s.j,'\$.aggregate_id')
                    AND m.seq = json_extract(s.j,'\$.seq'));
SQL
)"
  note "restorable rows in archive: $(commas "${RESTORE_CANDIDATES:-0}")"

  [ "$APPLY" -eq 1 ] || return 0

  step "Inserting"
  # data_hex is the lossless fallback the archiver emits for any value JSON
  # could not carry verbatim; unhex() reproduces the original bytes exactly.
  RESTORE_INSERTED="$(sqlite3 "$DB" <<SQL
PRAGMA foreign_keys=ON;
${import_sql}
INSERT OR IGNORE INTO main.event (id, aggregate_id, seq, type, data)
SELECT json_extract(s.j,'\$.id'),
       json_extract(s.j,'\$.aggregate_id'),
       json_extract(s.j,'\$.seq'),
       json_extract(s.j,'\$.type'),
       CASE WHEN json_extract(s.j,'\$.data_hex') IS NOT NULL
            THEN unhex(json_extract(s.j,'\$.data_hex'))
            ELSE json_extract(s.j,'\$.data') END
FROM stg s
WHERE json_extract(s.j,'\$.aggregate_id') = '${RESTORE_SESSION}';
SELECT changes();
SQL
)"
}

if [ -n "$RESTORE_SESSION" ]; then
  [ -n "$ARCHIVE" ] && die "--archive is a prune option; --restore-session reads from --from"
  restore_session_mode
  exit 0
fi
[ -n "$FROM" ] && die "--from only applies to --restore-session"

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
step "Preflight"
command -v sqlite3 >/dev/null || die "sqlite3 not found on PATH"
[ -f "$DB" ] || die "no database at $DB"
[ -n "$ARCHIVE" ] && pick_compressor
note "database      : $DB"
note "protect-days  : $PROTECT_DAYS"
note "protect-roots : $([ "$PROTECT_ROOTS" -eq 1 ] && echo 'yes (root sessions skipped entirely)' || echo 'no')"
note "mode          : $([ "$APPLY" -eq 1 ] && echo 'APPLY (will delete)' || echo 'dry run (read-only)')"
[ -n "$ARCHIVE" ] && note "archive       : $ARCHIVE (${COMPRESSOR})"

check_not_running

# Guard against a schema that does not match what this script was written for.
for col in aggregate_id seq owner_id; do
  sqlite3 -readonly "file:${DB}?mode=ro" "PRAGMA table_info(event_sequence);" \
    | cut -d'|' -f2 | grep -qx "$col" \
    || die "event_sequence has no '$col' column -- schema changed, refusing to run"
done
sqlite3 -readonly "file:${DB}?mode=ro" "PRAGMA table_info(event);" \
  | cut -d'|' -f2 | grep -qx 'aggregate_id' \
  || die "event table shape unexpected -- refusing to run"

# ---------------------------------------------------------------------------
# Shared SQL: the set of rows eligible for deletion.
#
# A row is eligible when it is a snapshot type, its entity key is non-NULL,
# its aggregate is not sync-owned, its session is older than the protect
# window, and a NEWER row exists for the same (aggregate, type, entity).
# ---------------------------------------------------------------------------
CUTOFF_SQL="(strftime('%s','now') - ${PROTECT_DAYS}*86400)*1000"
# Warp ships a single session's event history and the receiver requires
# contiguous seqs. You only ever warp a session you interact with, i.e. a root.
ROOT_SQL=""
[ "$PROTECT_ROOTS" -eq 1 ] && ROOT_SQL="AND s.parent_id IS NOT NULL"

read -r -d '' ELIGIBLE <<SQL || true
WITH keyed AS (
  SELECT e.rowid AS rid,
         e.aggregate_id AS agg,
         e.type         AS typ,
         e.seq          AS seq,
         LENGTH(e.data) AS len,
         CASE e.type
           WHEN 'message.updated.1'      THEN json_extract(e.data,'\$.info.id')
           WHEN 'message.part.updated.1' THEN json_extract(e.data,'\$.part.id')
           WHEN 'session.updated.1'      THEN json_extract(e.data,'\$.sessionID')
         END AS entity
  FROM event e
  JOIN session s ON s.id = e.aggregate_id
  WHERE e.type IN ('message.updated.1','message.part.updated.1','session.updated.1')
    AND s.time_updated < ${CUTOFF_SQL}
    ${ROOT_SQL}
    AND e.aggregate_id NOT IN (
      SELECT aggregate_id FROM event_sequence WHERE owner_id IS NOT NULL
    )
),
ranked AS (
  SELECT rid, typ, len, entity,
         MAX(seq) OVER (PARTITION BY agg, typ, entity) AS max_seq,
         seq
  FROM keyed
  WHERE entity IS NOT NULL
)
SELECT rid, typ, len FROM ranked WHERE seq < max_seq
SQL

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------
step "Analyzing (this scans the event table and takes a while)"

before_bytes="$(filesize "$DB")"
note "current file size: $(gb "$before_bytes")"

summary="$(sqlite3 -readonly "file:${DB}?mode=ro" "
WITH elig AS ($ELIGIBLE)
SELECT typ, COUNT(*), SUM(len) FROM elig GROUP BY typ ORDER BY SUM(len) DESC;")"

if [ -z "$summary" ]; then
  printf '\nNothing eligible to prune with --protect-days %s.\n' "$PROTECT_DAYS"
  exit 0
fi

total_rows=0; total_bytes=0
printf '\n  %-26s %14s %14s\n' 'event type' 'rows' 'bytes'
printf '  %-26s %14s %14s\n' '--------------------------' '--------------' '--------------'
while IFS='|' read -r typ n b; do
  [ -z "$typ" ] && continue
  printf '  %-26s %14s %14s\n' "$typ" "$(commas "$n")" "$(gb "$b")"
  total_rows=$(( total_rows + n )); total_bytes=$(( total_bytes + b ))
done <<EOF
$summary
EOF
printf '  %-26s %14s %14s\n' 'TOTAL' "$(commas "$total_rows")" "$(gb "$total_bytes")"

excluded="$(sqlite3 -readonly "file:${DB}?mode=ro" \
  "SELECT COUNT(*) FROM event_sequence WHERE owner_id IS NOT NULL;")"
protected="$(sqlite3 -readonly "file:${DB}?mode=ro" \
  "SELECT COUNT(*) FROM session WHERE time_updated >= ${CUTOFF_SQL};")"
note ""
note "excluded: $excluded sync-owned aggregate(s) (event_sequence.owner_id set)"
note "excluded: $protected session(s) active within ${PROTECT_DAYS} day(s)"
if [ "$PROTECT_ROOTS" -eq 1 ]; then
  roots="$(sqlite3 -readonly "file:${DB}?mode=ro" "SELECT COUNT(*) FROM session WHERE parent_id IS NULL;")"
  note "excluded: $roots root session(s) (--protect-roots)"
fi
note "kept    : the newest snapshot of every entity"
note "untouched: session, message, part, event_sequence, all other event types"

# ---------------------------------------------------------------------------
# Size projection
#
# The gross number ("we deleted 16 GB!") is misleading because the run also
# creates a full-size backup. What matters is what you are left holding once
# the dust settles, under each retention choice.
# ---------------------------------------------------------------------------
step "Size projection (net, not gross)"

est_after=$(( before_bytes - total_bytes ))
[ "$est_after" -lt 0 ] && est_after=0
backup_bytes=0
[ "$DO_BACKUP" -eq 1 ] && backup_bytes="$before_bytes"

est_archive=0
if [ -n "$ARCHIVE" ]; then
  est_archive="$(awk -v b="$total_bytes" -v r="$total_rows" \
                     -v esc="$ARCHIVE_ESCAPE_FACTOR" -v env="$ARCHIVE_ENVELOPE_BYTES" \
                     -v ratio="$ARCHIVE_RATIO" \
                 'BEGIN{ printf "%d", (b*esc + r*env) / ratio }')"
fi

printf '\n  %-34s %12s\n' 'current database file' "$(hsize "$before_bytes")"
printf '  %-34s %12s   %s\n' 'rows deleted by this prune' "$(hsize "$total_bytes")" "$(commas "$total_rows") row(s)"
printf '  %-34s %12s   %s\n' 'database after prune + VACUUM' "$(hsize "$est_after")" 'ESTIMATE'
if [ "$DO_BACKUP" -eq 1 ]; then
  printf '  %-34s %12s   %s\n' 'full backup during the run' "$(hsize "$backup_bytes")" 'TRANSIENT -- delete after'
else
  printf '  %-34s %12s   %s\n' 'full backup during the run' 'none' '--no-backup given'
fi
if [ -n "$ARCHIVE" ]; then
  printf '  %-34s %12s   %s\n' "archive of deleted rows" "$(hsize "$est_archive")" \
    "ESTIMATE, ${COMPRESSOR} ~${ARCHIVE_RATIO}x"
fi
printf '\n'
printf '  %-34s %12s\n' 'net reclaim, backup deleted' "$(hsize "$(( before_bytes - est_after ))")"
if [ -n "$ARCHIVE" ]; then
  printf '  %-34s %12s\n' 'net reclaim, archive retained' \
    "$(hsize "$(( before_bytes - est_after - est_archive ))")"
fi
printf '  %-34s %12s   %s\n' 'peak disk during the run' \
  "$(hsize "$(( before_bytes + backup_bytes + est_archive ))")" 'db + backup + archive'
printf '  %-34s %12s   %s\n' 'peak disk during VACUUM' \
  "$(hsize "$(( before_bytes + backup_bytes + est_archive + est_after ))")" 'VACUUM writes a full copy'

cat <<'EOF'

Retaining the full backup is a net LOSS: it is the size of the whole database,
so keeping it leaves you using more disk than before you pruned. Keep it only
until opencode starts cleanly on the pruned file, then delete it. If you want a
durable escape hatch, keep the (much smaller) --archive instead.
EOF

if [ "$PROTECT_ROOTS" -eq 1 ]; then
  cat <<'EOF'

With --protect-roots the archive is usually unnecessary: every session pruned
here is a subagent session, and the only capability restoring gives back is
warping a pruned session -- which you do not do to subagent sessions.
EOF
fi

if [ -n "$ARCHIVE" ]; then
  # Fail on an unusable archive path now, not after a 40 GB backup copy.
  # In dry run these are warnings: the report is the point, and refusing to
  # print the suggested command because of a stale file would be unhelpful.
  archive_dir="$(dirname "$ARCHIVE")"
  archive_problem=""
  if [ ! -d "$archive_dir" ]; then
    archive_problem="archive directory does not exist: $archive_dir"
  elif [ ! -w "$archive_dir" ]; then
    archive_problem="archive directory is not writable: $archive_dir"
  elif [ -e "$ARCHIVE" ]; then
    archive_problem="refusing to overwrite existing archive at $ARCHIVE"
  else
    archive_avail_kb="$(df -Pk "$archive_dir" | awk 'NR==2 {print $4}')"
    [ $(( archive_avail_kb * 1024 )) -gt "$est_archive" ] \
      || archive_problem="not enough space for the archive at $ARCHIVE (estimate $(hsize "$est_archive"))"
  fi
  if [ -n "$archive_problem" ]; then
    [ "$APPLY" -eq 1 ] && die "$archive_problem"
    printf '\nWARNING: %s\n         --apply would refuse until that is fixed.\n' "$archive_problem" >&2
  fi
fi

if [ "$APPLY" -eq 0 ]; then
  # Echo back exactly the flags that produced this report, so the suggested
  # command cannot silently differ from what was just measured.
  SUGGEST_FLAGS=" --protect-days ${PROTECT_DAYS}"
  [ "$PROTECT_ROOTS" -eq 1 ] && SUGGEST_FLAGS="${SUGGEST_FLAGS} --protect-roots"
  [ -n "$ARCHIVE" ] && SUGGEST_FLAGS="${SUGGEST_FLAGS} --archive ${ARCHIVE}"
  cat <<EOF

Dry run only -- nothing was changed.

To prune:   ./opencode-db-prune.sh${SUGGEST_FLAGS} --apply
Then:       ./opencode-db-vacuum.sh --auto-vacuum
Finally:    rm '${BACKUP:-${DB}.prune-backup}'   # once opencode starts cleanly

Deleting rows alone will NOT shrink the file; auto_vacuum is disabled on this
database, so VACUUM is required to return pages to the OS.
EOF
  exit 0
fi

# ---------------------------------------------------------------------------
# Apply
# ---------------------------------------------------------------------------
step "Integrity check"
integrity="$(sqlite3 "$DB" 'PRAGMA integrity_check;' | head -1)"
note "integrity_check: $integrity"
[ "$integrity" = "ok" ] || die "integrity check failed -- refusing to modify a damaged database"

if [ "$DO_BACKUP" -eq 1 ]; then
  BACKUP="${BACKUP:-${DB}.prune-backup}"
  [ -e "$BACKUP" ] && die "refusing to overwrite existing backup at $BACKUP"
  avail_kb="$(df -Pk "$(dirname "$BACKUP")" | awk 'NR==2 {print $4}')"
  [ $(( avail_kb * 1024 )) -gt "$before_bytes" ] \
    || die "not enough space for a backup at $BACKUP (need $(gb "$before_bytes"))"
  step "Backing up"
  note "$BACKUP"
  note "transient crash insurance -- delete this once opencode starts cleanly"
  # .backup is WAL-safe; plain cp is not.
  sqlite3 "$DB" ".backup '$BACKUP'"
  note "done ($(gb "$(filesize "$BACKUP")"))"
else
  note "skipping backup (--no-backup)"
fi

if [ "$ASSUME_YES" -eq 0 ]; then
  printf '\nDelete %s rows (%s) from `event`? [y/N] ' "$(commas "$total_rows")" "$(gb "$total_bytes")"
  read -r reply
  case "$reply" in
    [yY]|[yY][eE][sS]) ;;
    *) note "aborted; nothing deleted"; exit 0 ;;
  esac
fi

# ---------------------------------------------------------------------------
# Archive (must complete before anything is deleted)
# ---------------------------------------------------------------------------
cleanup_prune_tmp() {
  [ -n "$ARCHIVE_TMP" ] && [ -e "$ARCHIVE_TMP" ] && rm -f "$ARCHIVE_TMP"
  # Dying during archiving would otherwise leave the staging table sitting in
  # the user's database. Nothing has been deleted at that point, so drop it
  # rather than leave debris -- the next run recomputes it from scratch. Once
  # the delete loop starts, PRUNE_STAGED is cleared and the table is left alone.
  if [ "$PRUNE_STAGED" -eq 1 ]; then
    sqlite3 "$DB" "DROP TABLE IF EXISTS opencode_db_prune_targets;" 2>/dev/null || true
  fi
  return 0
}

archive_targets() {
  local dest="$1" expect="$2"
  ARCHIVE_TMP="${dest}.partial"
  rm -f "$ARCHIVE_TMP"

  step "Archiving $(commas "$expect") target row(s) with ${COMPRESSOR}"
  note "$dest"

  # FORMAT: newline-delimited JSON, one object per deleted row, produced by
  # SQLite's own json_object(). Chosen over a CSV or a raw .dump because:
  #   * json_object escapes newlines, quotes, tabs and every other control
  #     character, so one row is always exactly one line -- which makes the
  #     restore path a fixed-string grep instead of a stateful parser;
  #   * SQLite emits and re-reads the value itself, so there is no third-party
  #     escaping convention to disagree about;
  #   * .import in `.mode ascii` is byte-literal (no CSV quote rewriting), so
  #     the line goes back in exactly as it came out.
  #
  # JSON cannot carry a BLOB, and a TEXT value could in principle survive the
  # round trip imperfectly, so every row is TESTED, per row, in the same
  # statement that emits it: if json_extract(json_object(...)) does not compare
  # IS-identical to the original value, that row is written as data_hex instead.
  # unhex() reproduces those bytes exactly. Correctness is therefore structural,
  # not a property we hope holds -- no row can be archived lossily.
  sqlite3 -readonly "file:${DB}?mode=ro" <<SQL | "${COMPRESS_CMD[@]}" >"$ARCHIVE_TMP"
.mode list
.headers off
SELECT CASE
         WHEN typeof(e.data) = 'blob'
           THEN json_object('id',e.id,'aggregate_id',e.aggregate_id,'seq',e.seq,
                            'type',e.type,'data_hex',hex(e.data))
         WHEN json_extract(json_object('d', e.data), '\$.d') IS NOT e.data
           THEN json_object('id',e.id,'aggregate_id',e.aggregate_id,'seq',e.seq,
                            'type',e.type,'data_hex',hex(e.data))
         ELSE json_object('id',e.id,'aggregate_id',e.aggregate_id,'seq',e.seq,
                          'type',e.type,'data',e.data)
       END
FROM event e
WHERE e.rowid IN (SELECT rid FROM opencode_db_prune_targets);
SQL

  # Verify by actually decompressing it. A truncated or half-written archive
  # would otherwise look fine on disk, and we are about to delete its contents.
  step "Verifying archive"
  local got
  got="$("${DECOMPRESS_CMD[@]}" "$ARCHIVE_TMP" | wc -l | tr -d ' ')"
  [ "$got" -eq "$expect" ] \
    || die "archive holds $(commas "$got") row(s) but $(commas "$expect") were staged --
    refusing to delete rows that are not archived. Partial file removed."
  note "decompressed cleanly: $(commas "$got") row(s)"

  mv "$ARCHIVE_TMP" "$dest"
ARCHIVE_TMP=""
PRUNE_STAGED=0
  note "archive: $dest"
  note "size   : $(hsize "$(filesize "$dest")") (${COMPRESSOR})"
}

step "Staging target rows"
# A real (not TEMP) table so the id set survives across connections and the
# delete can be batched and resumed. Recomputing the window function per batch
# would be O(n^2) over millions of rows.
sqlite3 "$DB" "DROP TABLE IF EXISTS opencode_db_prune_targets;"
staged="$(sqlite3 "$DB" <<SQL
CREATE TABLE opencode_db_prune_targets (rid INTEGER PRIMARY KEY);
INSERT INTO opencode_db_prune_targets (rid) SELECT rid FROM ($ELIGIBLE);
SELECT changes();
SQL
)"
note "staged $(commas "${staged:-0}") row id(s)"
# Between here and the first DELETE, an abort must leave no trace: no staging
# table in the user's database and no half-written archive on disk.
PRUNE_STAGED=1
trap cleanup_prune_tmp EXIT

if [ -n "$ARCHIVE" ]; then
  archive_targets "$ARCHIVE" "${staged:-0}"
fi

# Past this point the staging table is deliberately left alone: the delete loop
# is batched, and an interrupted delete is resumed by the next run, not undone.
PRUNE_STAGED=0

step "Deleting in batches of $(commas "$BATCH")"
deleted=0
while :; do
  n="$(sqlite3 "$DB" <<SQL
PRAGMA foreign_keys=ON;
DELETE FROM event WHERE rowid IN (
  SELECT rid FROM opencode_db_prune_targets LIMIT $BATCH
);
DELETE FROM opencode_db_prune_targets WHERE rid IN (
  SELECT rid FROM opencode_db_prune_targets LIMIT $BATCH
);
SELECT (SELECT COUNT(*) FROM opencode_db_prune_targets);
SQL
)"
  remaining="${n:-0}"
  deleted=$(( staged - remaining ))
  printf '\r  deleted %s / %s ...' "$(commas "$deleted")" "$(commas "$staged")"
  [ "$remaining" -eq 0 ] && break
done
printf '\n'
sqlite3 "$DB" "DROP TABLE IF EXISTS opencode_db_prune_targets;"
note "deleted $(commas "$deleted") row(s)"

step "Post-delete verification"
post_integrity="$(sqlite3 "$DB" 'PRAGMA integrity_check;' | head -1)"
note "integrity_check: $post_integrity"
if [ "$post_integrity" != "ok" ]; then
  die "integrity check FAILED after delete.
    Restore from the backup:  mv '$BACKUP' '$DB'"
fi

for t in session message part event_sequence; do
  n="$(sqlite3 -readonly "file:${DB}?mode=ro" "SELECT COUNT(*) FROM $t;")"
  note "$(printf '%-16s %12s rows (untouched)' "$t" "$(commas "$n")")"
done

sqlite3 "$DB" 'PRAGMA wal_checkpoint(TRUNCATE);' >/dev/null

after_bytes="$(filesize "$DB")"
freelist="$(sqlite3 -readonly "file:${DB}?mode=ro" 'PRAGMA freelist_count;')"
page_size="$(sqlite3 -readonly "file:${DB}?mode=ro" 'PRAGMA page_size;')"

cat <<EOF

Done.

  file size        $(gb "$before_bytes")  ->  $(gb "$after_bytes")   (unchanged is expected)
  free pages now   $(commas "$freelist")  =  $(gb $(( freelist * page_size )))

The file does not shrink on delete: auto_vacuum is disabled, so those pages sit
on the freelist until a VACUUM hands them back to the OS. Reclaim them with:

    ./opencode-db-vacuum.sh --auto-vacuum

EOF

if [ -n "$ARCHIVE" ]; then
  cat <<EOF
Archive of the deleted rows ($(hsize "$(filesize "$ARCHIVE")"), ${COMPRESSOR}):

    $ARCHIVE

Put one session back with:

    ./opencode-db-prune.sh --restore-session ses_XXXX --from '$ARCHIVE' --apply

EOF
fi

if [ "$DO_BACKUP" -eq 1 ]; then
  cat <<EOF
Backup at $BACKUP ($(gb "$(filesize "$BACKUP")")).

DELETE IT once opencode starts cleanly:  rm '$BACKUP'
Keeping it cancels out the prune -- it is a full-size copy, so you would be
holding $(gb "$(( after_bytes + before_bytes ))") instead of the $(gb "$before_bytes") you started with.
EOF
fi
