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
note() { printf '  %s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }

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

while [ $# -gt 0 ]; do
  case "$1" in
    --db) DB="${2:?--db needs a path}"; shift 2 ;;
    --into) INTO="${2:?--into needs a path}"; shift 2 ;;
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

# opencode holds the db open with busy_timeout=5000; VACUUM needs an exclusive
# lock and will simply fail against a live process.
running="$(pgrep -f '(^|/)opencode' 2>/dev/null | wc -l | tr -d ' ')"
if [ "$running" -gt 0 ]; then
  printf '\n'
  pgrep -lf '(^|/)opencode' 2>/dev/null | head -20 >&2 || true
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

# Read-only URI so this is safe even while opencode is attached.
read -r page_size page_count freelist auto_vac journal <<EOF
$(sqlite3 -readonly "file:${DB}?mode=ro" \
  "PRAGMA page_size; PRAGMA page_count; PRAGMA freelist_count; PRAGMA auto_vacuum; PRAGMA journal_mode;" \
  | tr '\n' ' ')
EOF

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

step "Vacuuming into $COMPACT"
note "this rewrites $(gb "$live_bytes") and may take a while..."
sqlite3 "$DB" "VACUUM INTO '$COMPACT';"

new_bytes="$(wc -c <"$COMPACT" | tr -d ' ')"
note "new size: $(gb "$new_bytes")  (was $(gb "$total_bytes"))"

step "Verifying the compacted copy"
new_integrity="$(sqlite3 -readonly "file:${COMPACT}?mode=ro" 'PRAGMA integrity_check;' | head -1)"
note "integrity_check: $new_integrity"
[ "$new_integrity" = "ok" ] || die "compacted copy failed integrity check; original untouched at $DB"

for t in session message part event; do
  old_n="$(sqlite3 -readonly "file:${DB}?mode=ro" "SELECT COUNT(*) FROM $t;" 2>/dev/null || echo skip)"
  new_n="$(sqlite3 -readonly "file:${COMPACT}?mode=ro" "SELECT COUNT(*) FROM $t;" 2>/dev/null || echo skip)"
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
