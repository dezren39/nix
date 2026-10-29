#!/usr/bin/env bash
# Retention for `just opencode-db-backup` backups (#659).
#
# opencode-db-backup writes a full copy of the OpenCode database into
# ~/.local/share/opencode/backup/<YYYYMMDDTHHMMSS>/ on every run and
# nothing ever deleted the old ones — each is ~26 GB, so the backups
# alone could re-fill the disk the prune/vacuum work reclaims (the
# 2026-10-19 reliability review found the disk at 99%).
#
# Policy: among directories whose names are backup timestamps
# (YYYYMMDDTHHMMSS — lexicographic order is chronological), keep the
# newest --keep (default 3); anything older is a deletion candidate.
# With --max-age-days, a candidate must also be older than that many
# days (by its name timestamp) before it is listed. The newest backup
# is never listed (--keep >= 1 is enforced). Entries that are not
# timestamp-named directories are never touched.
#
# Dry-run is the default: candidates are printed, nothing is deleted.
# Deletion happens only with --apply.
#
# Run: ./opencode-db-backup-prune.sh [--dir DIR] [--keep N]
#        [--max-age-days D] [--apply]
# It is run by the flake check `opencode-db-backup-prune-tests`
# (via opencode-db-backup-prune.test.sh) and by the just recipe
# `opencode-db-backup-prune`.
set -u

DIR="$HOME/.local/share/opencode/backup"
KEEP=3
MAX_AGE_DAYS=""
APPLY=0

die() { echo "opencode-db-backup-prune: $*" >&2; exit 2; }

while [ $# -gt 0 ]; do
  case "$1" in
    --dir) { [ -n "${2:-}" ] && [ "${2#-}" = "$2" ]; } || die "--dir needs a value"; DIR="$2"; shift 2 ;;
    --keep) { [ -n "${2:-}" ] && [ "${2#-}" = "$2" ]; } || die "--keep needs a value"; KEEP="$2"; shift 2 ;;
    --max-age-days) { [ -n "${2:-}" ] && [ "${2#-}" = "$2" ]; } || die "--max-age-days needs a value"; MAX_AGE_DAYS="$2"; shift 2 ;;
    --apply) APPLY=1; shift ;;
    --dry-run) APPLY=0; shift ;;
    -h|--help) sed -n '2,26p' "$0"; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

case "$KEEP" in *[!0-9]*|'') die "--keep must be a non-negative integer, got: $KEEP" ;; esac
[ "$KEEP" -ge 1 ] || die "--keep must be at least 1 (the newest backup is never deleted)"
if [ -n "$MAX_AGE_DAYS" ]; then
  case "$MAX_AGE_DAYS" in *[!0-9]*|'') die "--max-age-days must be a non-negative integer, got: $MAX_AGE_DAYS" ;; esac
fi

[ -d "$DIR" ] || { echo "opencode-db-backup-prune: no backup dir at $DIR — nothing to do"; exit 0; }

# Timestamp-named directories only, oldest first (name order = time order).
candidates=()
while IFS= read -r name; do
  candidates+=("$name")
done < <(
  for entry in "$DIR"/*/; do
    [ -d "$entry" ] || continue
    name="${entry%/}"; name="${name##*/}"
    case "$name" in
      [0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]) echo "$name" ;;
    esac
  done | sort
)

total=${#candidates[@]}
if [ "$total" -le "$KEEP" ]; then
  echo "opencode-db-backup-prune: $total backup(s) in $DIR, keep=$KEEP — nothing to prune"
  exit 0
fi

now_epoch="$(date +%s)"
cutoff=$((total - KEEP))
listed=0
for ((i = 0; i < cutoff; i++)); do
  name="${candidates[$i]}"
  if [ -n "$MAX_AGE_DAYS" ]; then
    # Parse YYYYMMDDTHHMMSS as local time, portably: BSD date (macOS,
    # where the backups live) has no GNU -d; it parses with -j -f.
    # GNU date rejects -j, so try BSD first and fall back to GNU (#1065).
    ts="${name:0:4}-${name:4:2}-${name:6:2} ${name:9:2}:${name:11:2}:${name:13:2}"
    epoch="$(date -j -f "%Y-%m-%d %H:%M:%S" "$ts" +%s 2>/dev/null || date -d "$ts" +%s 2>/dev/null)" || die "cannot parse backup timestamp: $name"
    age_days=$(( (now_epoch - epoch) / 86400 ))
    [ "$age_days" -gt "$MAX_AGE_DAYS" ] || continue
  fi
  listed=$((listed + 1))
  if [ "$APPLY" -eq 1 ]; then
    rm -rf -- "$DIR/$name"
    echo "deleted: $DIR/$name"
  else
    echo "would delete: $DIR/$name"
  fi
done

if [ "$APPLY" -eq 1 ]; then
  echo "opencode-db-backup-prune: deleted $listed of $total backup(s) in $DIR (kept newest $KEEP)"
else
  echo "opencode-db-backup-prune: dry run — $listed of $total backup(s) in $DIR would be deleted (kept newest $KEEP); re-run with --apply to delete"
fi
