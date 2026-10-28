#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# opencode-db-vacuum.sh — compact ~/.local/share/opencode/opencode.db safely.
#
# WHAT THIS DOES AND DOES NOT DO
#   VACUUM only returns *already-free* pages to the OS. It does not delete
#   anything. If PRAGMA freelist_count is 0, vacuuming reclaims ~0 bytes and
#   this script will tell you so and stop.
#
#   As of writing, opencode's `event` table is append-only with no retention
#   (upstream anomalyco/opencode#47223, #47729, #41175). `message.updated.1`
#   stores a full message snapshot per streaming update, so the event log is
#   typically the bulk of the file. Freeing that requires deleting whole
#   sessions first -- `opencode session delete <id>` is the only supported
#   path, and it FK-cascades session -> message -> part and removes the
#   matching event rows. Run that first, then run this to shrink the file.
#
# STRUCTURE
#   1. preflight: sqlite3 present, db present, no opencode running
#   2. measure: size, page/freelist counts, projected reclaim
#   3. integrity_check + wal_checkpoint(TRUNCATE)
#   4. optional auto_vacuum=INCREMENTAL (must be set BEFORE the vacuum)
#   5. VACUUM INTO a scratch copy, verify it, then swap
#
#   VACUUM INTO is used rather than plain VACUUM because it writes the compacted
#   copy to a path you choose (possibly another volume), never mutates the
#   original, and leaves a failure as a no-op.

set -euo pipefail

DB_DEFAULT="${HOME}/.local/share/opencode/opencode.db"
DB="$DB_DEFAULT"
INTO=""
DRY_RUN=0
ASSUME_YES=0
SET_AUTOVACUUM=0
KEEP_BACKUP=1
FORCE=0

die() {
  printf 'opencode-db-vacuum: %s\n' "$*" >&2
  exit 1
}

# Percent-encode the characters SQLite's URI parser treats specially in a
# path (% first, then ? and #), so a --db/--into path containing them opens
# the file actually named (#890). For sqlite3 CLI "file:" arguments, which
# are URIs, not SQL literals -- a single quote needs no escaping there.
uri_escape_path() {
  printf '%s' "$1" | sed -e 's/%/%25/g' -e 's/?/%3f/g' -e 's/#/%23/g'
}

# Escape a path for interpolation into a single-quoted SQL literal (#891).
sql_literal_escape() {
  printf '%s' "$1" | sed -e "s/'/''/g"
}
note() { printf '  %s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }

# Pids of opencode processes other than this script, the invocation
# chain that launched it, and its own children.
# Matching full cmdlines for opencode also matches this script: it is
# invoked as
# "bash ./opencode-db-vacuum.sh ..." -- "/opencode" appears in its own
# cmdline, and every command-substitution subshell inherits it, so a raw
# count includes the script itself (typically 2) and can never be 0
# (#465). Resolve each candidate's ancestry and drop any process that is this
# script ($$), is an ancestor of it (the launching shell/sudo/timeout --
# their cmdlines name this script too), or descends from it. $$ is the main
# script's pid even inside
# subshells (unlike $BASHPID), so the check holds from any call depth.
opencode_pids() {
  # One consistent snapshot of the process table, filtered in awk: with
  # pgrep + per-pid ps walks, the script's own forked-but-not-yet-exec'd
  # subshell images match during the snapshot and vanish before their
  # ancestry can be checked, leaking through as phantom processes.
  ps -eo pid=,ppid=,args= 2>/dev/null | awk -v self="$$" '
    {
      pid = $1 + 0; ppid = $2 + 0
      parent[pid] = ppid
      $1 = ""; $2 = ""
      # Clearing $1/$2 rebuilds $0 joined by OFS, so the stored cmdline
      # starts with spaces and the "(^|/)opencode" test below can never
      # anchor ^ at the real argv[0] -- a process started as a bare
      # "opencode" (the normal PATH invocation) was missed entirely
      # (#467). Trim the padding so cmd holds the true cmdline. The
      # match itself requires a boundary after the name ($ or space),
      # so argv[0]s that merely start with "opencode" (opencode-share,
      # opencode-server, opencodefoo) are not counted (#828).
      cmd[pid] = $0
      sub(/^ +/, "", cmd[pid])
      order[++n] = pid
    }
    END {
      # The invocation chain: self plus every ancestor of self.
      c = self
      for (d = 0; d < 32; d++) {
        chain[c] = 1
        if (!(c in parent)) break
        c = parent[c]
      }
      for (i = 1; i <= n; i++) {
        pid = order[i]
        if (cmd[pid] !~ "(^|/)opencode($| )") continue
        if (pid in chain) continue
        # Descendants of self (subshells, pipelines) are self too.
        c = pid; isself = 0
        for (d = 0; d < 32; d++) {
          if (c == self) { isself = 1; break }
          if (!(c in parent)) break
          c = parent[c]
        }
        if (isself) continue
        print pid
      }
    }'
}
opencode_list() {
  local pid
  for pid in $(opencode_pids); do
    ps -o pid=,args= -p "$pid" 2>/dev/null || printf '%s\n' "$pid"
  done
}

usage() {
  cat <<'EOF'
Usage: ./opencode-db-vacuum.sh [options]

Compacts opencode.db with VACUUM INTO, after verifying it is safe to do so.

Options:
  --db PATH         Database to compact (default ~/.local/share/opencode/opencode.db)
  --into PATH       Where to write the compacted copy. Default: alongside the db.
                    Point this at another volume if the current one is tight.
  --auto-vacuum     Also set auto_vacuum=INCREMENTAL so future deletes can be
                    reclaimed without a full rebuild. Applied before the vacuum,
                    which is the only time the pragma can take effect.
  --dry-run         Report only. Makes no changes.
  --no-backup       Do not keep the original as opencode.db.old after swapping.
  --force           Proceed even if projected reclaim is negligible.
  -y, --yes         Do not prompt before swapping the file in.
  -h, --help        This text.

Typical use, in order:

  # 1. See where you stand. Safe, read-only.
  ./opencode-db-vacuum.sh --dry-run

  # 2. If freelist is ~0, there is nothing to reclaim yet. Delete old sessions
  #    first (supported path, cascades to messages/parts/events):
  #      opencode session delete <sessionID>

  # 3. Stop every opencode process, then compact.
  ./opencode-db-vacuum.sh --auto-vacuum -y
EOF
}

# Value-taking options reject an empty or flag-shaped value (#941, the
# #930 pattern): "--into --dry-run" must die naming --into, not swallow
# --dry-run as the target path and run a real vacuum.
while [ $# -gt 0 ]; do
  case "$1" in
    --db) { [ -n "${2:-}" ] && [ "${2#-}" = "$2" ]; } || die "--db needs a path"; DB="$2"; shift 2 ;;
    --into) { [ -n "${2:-}" ] && [ "${2#-}" = "$2" ]; } || die "--into needs a path"; INTO="$2"; shift 2 ;;
    --auto-vacuum) SET_AUTOVACUUM=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --no-backup) KEEP_BACKUP=0; shift ;;
    --force) FORCE=1; shift ;;
    -y|--yes) ASSUME_YES=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
done

# ---------------------------------------------------------------------------
# 1. Preflight
# ---------------------------------------------------------------------------
step "Preflight"

command -v sqlite3 >/dev/null || die "sqlite3 not found on PATH"
[ -f "$DB" ] || die "no database at $DB"
note "database: $DB"

# A real run's swap verification counts session/message/part/event; a
# missing table used to surface only after VACUUM INTO had already built
# the compacted copy (#1051). Refuse up front -- read-only, so it precedes
# the running-process check and a drifted DB gets the schema refusal even
# on a machine where opencode is up. --dry-run is read-only measurement
# and stays exempt: it never reaches the verification, and a scratch DB
# must still measure cleanly.
if [ "$DRY_RUN" -eq 0 ]; then
  for t in session message part event; do
    sqlite3 -readonly "file:$(uri_escape_path "$DB")?mode=ro" \
      "SELECT 1 FROM sqlite_master WHERE type='table' AND name='$t';" \
      | grep -qx '1' \
      || die "$t table missing -- schema changed, refusing to run"
  done
fi

# opencode holds the db open with busy_timeout=5000; VACUUM needs an exclusive
# lock, and the swap below replaces the file outright, so a live holder must
# stop the run. Same shape as opencode-db-prune.sh's check_not_running.
# pgrep exits 1 when nothing matches, and under `pipefail` a bare assignment
# would inherit that and abort the script -- i.e. exactly when opencode IS
# stopped, which is the only case in which a real run may proceed.
running="$(opencode_pids | wc -l | tr -d ' ' || true)"
running="${running:-0}"
if [ "$running" -gt 0 ] && command -v lsof >/dev/null 2>&1; then
  # Only a process holding THIS database matters. Guarding on "any opencode
  # process" would block work on a scratch --db while an unrelated session
  # is open. lsof is the direct question; without it, the (self-excluded)
  # count above stands as the conservative answer.
  holders="$(lsof -t -- "$DB" 2>/dev/null | wc -l | tr -d ' ' || true)"
  holders="${holders:-0}"
  if [ "$holders" -eq 0 ]; then
    note "note: $running opencode process(es) running, but none has $DB open"
    running=0
  else
    running="$holders"
  fi
fi
if [ "$running" -gt 0 ]; then
  printf '\n'
  opencode_list | head -20 >&2 || true
  printf '\n' >&2
  if [ "$DRY_RUN" -eq 1 ]; then
    note "WARNING: $running opencode process(es) running. Measurement below is"
    note "         read-only and safe, but a real run would fail. Stop them with:"
    note "           pkill -f opencode && sleep 5 && pgrep -lf opencode"
  else
    die "$running opencode process(es) still running. VACUUM needs an exclusive lock.
    Stop them first:  pkill -f opencode && sleep 5 && pgrep -lf opencode"
  fi
fi

# ---------------------------------------------------------------------------
# 2. Measure
# ---------------------------------------------------------------------------
step "Measuring"

# Read-only URI so this is safe even while opencode is attached. A failed
# open must die here: blank measurements flowing onward used to end in a
# line-233 integer error and a successful exit (#890).
measure="$(sqlite3 -readonly "file:$(uri_escape_path "$DB")?mode=ro" \
  "PRAGMA page_size; PRAGMA page_count; PRAGMA freelist_count; PRAGMA auto_vacuum; PRAGMA journal_mode;")" \
  || die "sqlite3 could not open/read $DB"
read -r page_size page_count freelist auto_vac journal <<EOF
$(printf '%s' "$measure" | tr '\n' ' ')
EOF
case "${page_size}${page_count}${freelist}" in
  *[!0-9]*|'') die "unexpected measurements from $DB (page_size='$page_size' page_count='$page_count' freelist_count='$freelist')" ;;
esac

live_pages=$(( page_count - freelist ))
total_bytes=$(( page_size * page_count ))
live_bytes=$(( page_size * live_pages ))
free_bytes=$(( page_size * freelist ))

gb() { awk -v b="$1" 'BEGIN{ printf "%.2f GB", b/1073741824 }'; }

note "page_size      : $page_size"
note "page_count     : $page_count"
note "freelist_count : $freelist"
note "auto_vacuum    : $auto_vac  (0=NONE, 1=FULL, 2=INCREMENTAL)"
note "journal_mode   : $journal"
note "file size      : $(gb "$total_bytes")"
note "live data      : $(gb "$live_bytes")"
note "reclaimable    : $(gb "$free_bytes")"

if [ "$freelist" -eq 0 ] && [ "$FORCE" -eq 0 ]; then
  cat <<EOF

Nothing to reclaim: freelist_count is 0, so every page holds live data.
VACUUM would rewrite $(gb "$total_bytes") to save nothing.

To actually shrink this database you must delete data first. The supported
route deletes whole sessions and cascades to messages, parts and events:

    opencode session list --format json > ~/sessions-backup.json
    opencode session delete <sessionID>

Then re-run this script. Use --force to vacuum anyway.
EOF
  exit 0
fi

# VACUUM INTO writes a full copy of the live data. Require headroom for it.
target_dir="$(dirname "${INTO:-$DB}")"
[ -d "$target_dir" ] || die "target directory does not exist: $target_dir"
avail_kb="$(df -Pk "$target_dir" | awk 'NR==2 {print $4}')"
avail_bytes=$(( avail_kb * 1024 ))
needed_bytes=$(( live_bytes + live_bytes / 10 ))   # live + 10% margin

note "target volume  : $target_dir"
note "available      : $(gb "$avail_bytes")"
note "needed         : $(gb "$needed_bytes")  (live data + 10%)"

if [ "$avail_bytes" -lt "$needed_bytes" ]; then
  die "not enough free space on $target_dir.
    Need $(gb "$needed_bytes"), have $(gb "$avail_bytes").
    Use --into /path/on/another/volume, or free space first."
fi

if [ "$DRY_RUN" -eq 1 ]; then
  printf '\n--dry-run: stopping here. Nothing was changed.\n'
  exit 0
fi

# ---------------------------------------------------------------------------
# 3. Integrity + WAL checkpoint
# ---------------------------------------------------------------------------
step "Integrity check"
integrity="$(sqlite3 "$DB" 'PRAGMA integrity_check;' | head -1)"
note "integrity_check: $integrity"
[ "$integrity" = "ok" ] || die "integrity check failed -- do NOT vacuum a corrupt database"

step "Checkpointing WAL"
sqlite3 "$DB" 'PRAGMA wal_checkpoint(TRUNCATE);' >/dev/null
note "WAL folded into main database"

# ---------------------------------------------------------------------------
# 4. auto_vacuum, which only takes effect on the NEXT vacuum
# ---------------------------------------------------------------------------
if [ "$SET_AUTOVACUUM" -eq 1 ]; then
  step "Setting auto_vacuum=INCREMENTAL"
  sqlite3 "$DB" 'PRAGMA auto_vacuum=INCREMENTAL;' >/dev/null
  note "set; it is baked in by the vacuum below"
  note "future reclaim becomes: sqlite3 \"$DB\" 'PRAGMA incremental_vacuum;'"
fi

# ---------------------------------------------------------------------------
# 5. VACUUM INTO, verify, swap
# ---------------------------------------------------------------------------
COMPACT="${INTO:-${DB}.compact}"
[ -e "$COMPACT" ] && die "refusing to overwrite existing $COMPACT"
# The swap below would silently replace an existing ${DB}.old -- the only
# rollback copy from a previous run (#892). Same refusal shape as $COMPACT
# above and prune's $BACKUP guard.
[ -e "${DB}.old" ] && die "refusing to overwrite existing backup ${DB}.old -- move or delete it first"

step "Vacuuming into $COMPACT"
note "this rewrites $(gb "$live_bytes") and may take a while..."
sqlite3 "$DB" "VACUUM INTO '$(sql_literal_escape "$COMPACT")';"

new_bytes="$(wc -c <"$COMPACT" | tr -d ' ')"
note "new size: $(gb "$new_bytes")  (was $(gb "$total_bytes"))"

step "Verifying the compacted copy"
new_integrity="$(sqlite3 -readonly "file:$(uri_escape_path "$COMPACT")?mode=ro" 'PRAGMA integrity_check;' | head -1)"
note "integrity_check: $new_integrity"
[ "$new_integrity" = "ok" ] || die "compacted copy failed integrity check; original untouched at $DB"

for t in session message part event; do
  old_n="$(sqlite3 -readonly "file:$(uri_escape_path "$DB")?mode=ro" "SELECT COUNT(*) FROM $t;" 2>/dev/null || echo skip)"
  new_n="$(sqlite3 -readonly "file:$(uri_escape_path "$COMPACT")?mode=ro" "SELECT COUNT(*) FROM $t;" 2>/dev/null || echo skip)"
  [ "$old_n" = skip ] && continue
  note "$(printf '%-8s %12s -> %12s' "$t" "$old_n" "$new_n")"
  [ "$old_n" = "$new_n" ] || die "row count mismatch in $t; original untouched at $DB"
done

if [ "$ASSUME_YES" -eq 0 ]; then
  printf '\nSwap the compacted copy in? Original kept as %s.old [y/N] ' "$DB"
  read -r reply
  case "$reply" in
    [yY]|[yY][eE][sS]) ;;
    *) note "left in place: $COMPACT"; note "original untouched: $DB"; exit 0 ;;
  esac
fi

step "Swapping"
mv "$DB" "${DB}.old"
mv "$COMPACT" "$DB"
# Safe only now: clean checkpoint above, no process attached. opencode
# re-enables WAL on next open.
rm -f "${DB}-wal" "${DB}-shm"
note "active: $DB  ($(gb "$new_bytes"))"

if [ "$KEEP_BACKUP" -eq 1 ]; then
  note "backup: ${DB}.old  ($(gb "$total_bytes"))"
  note "delete it once opencode has started cleanly:  rm '${DB}.old'"
else
  rm -f "${DB}.old"
  note "backup removed (--no-backup)"
fi

printf '\nDone. Reclaimed %s.\n' "$(gb $(( total_bytes - new_bytes )) )"
