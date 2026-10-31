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

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Sourced up front because the argument parser below uses
# lib/common.sh's guard helpers (arg_die and friends) -- design #1145
# phase 2 (#1147), the clean.sh SELF_DIR precedent. die() below stays
# for runtime failures; parse errors go through the shared helpers.
# (Inserted after the header comment on purpose: --help prints header
# lines 2-26 verbatim, and they must not shift.)
# shellcheck source=lib/common.sh
. "$SELF_DIR/lib/common.sh"
ARG_PROGRAM="opencode-db-backup-prune"

usage() { sed -n '2,26p' "$0"; }

DIR="$HOME/.local/share/opencode/backup"
KEEP=3
MAX_AGE_DAYS=""
APPLY=0
DIR_SET=0
KEEP_SET=0
MAX_AGE_DAYS_SET=0
APPLY_SET=0
DRY_RUN_SET=0

die() { echo "opencode-db-backup-prune: $*" >&2; exit 2; }

# Value-taking options reject a repeat (#1125, the #1112 / adopt #1103
# pattern): a repeated --dir/--keep/--max-age-days used to silently
# discard the first value. --apply combined with --dry-run (either
# order) is likewise rejected: both set the same flag, so whichever
# came last used to win -- "--dry-run --apply" actually deleted.
while [ $# -gt 0 ]; do
  case "$1" in
    --dir) arg_require_value "$1" "${2:-}"; arg_once "$1" DIR_SET; DIR="$2"; DIR_SET=1; shift 2 ;;
    --keep) arg_require_value "$1" "${2:-}"; arg_once "$1" KEEP_SET; KEEP="$2"; KEEP_SET=1; shift 2 ;;
    --max-age-days) arg_require_value "$1" "${2:-}"; arg_once "$1" MAX_AGE_DAYS_SET; MAX_AGE_DAYS="$2"; MAX_AGE_DAYS_SET=1; shift 2 ;;
    --apply) arg_conflict --apply --dry-run "$DRY_RUN_SET"; APPLY=1; APPLY_SET=1; shift ;;
    --dry-run) arg_conflict --apply --dry-run "$APPLY_SET"; APPLY=0; DRY_RUN_SET=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) arg_die "unknown option: $1" ;;
  esac
done

# Numeric values are judged whole, at parse time, with the shared
# arg_decimal helper (lib/common.sh) (#1147): strict plain decimal
# (#1087: leading zeros are data, never octal), INT_MAX backstop by
# digit count first, judged form is the used form. --keep is positive
# (the newest backup is never deleted); --max-age-days non-negative.
arg_decimal KEEP --keep "$KEEP" positive
if [ -n "$MAX_AGE_DAYS" ]; then
  arg_decimal MAX_AGE_DAYS --max-age-days "$MAX_AGE_DAYS" non-negative
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
failed=0
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
  if [ "$APPLY" -eq 1 ]; then
    # A failed deletion must not be reported as success (#1257, the
    # PR #1063 Codex P2): count and print only removals that actually
    # happened, and exit nonzero below if any candidate survived.
    if rm -rf -- "$DIR/$name"; then
      listed=$((listed + 1))
      echo "deleted: $DIR/$name"
    else
      failed=$((failed + 1))
      echo "opencode-db-backup-prune: failed to delete: $DIR/$name" >&2
    fi
  else
    listed=$((listed + 1))
    echo "would delete: $DIR/$name"
  fi
done

if [ "$APPLY" -eq 1 ]; then
  echo "opencode-db-backup-prune: deleted $listed of $total backup(s) in $DIR (kept newest $KEEP)"
  if [ "$failed" -gt 0 ]; then
    echo "opencode-db-backup-prune: $failed deletion(s) failed" >&2
    exit 1
  fi
else
  echo "opencode-db-backup-prune: dry run — $listed of $total backup(s) in $DIR would be deleted (kept newest $KEEP); re-run with --apply to delete"
fi
