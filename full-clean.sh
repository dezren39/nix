#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# full-clean.sh — switch, prune opencode's event log, then reclaim disk.
#
# ORDER MATTERS
#   1. ./s                    switch first, so the new generation is the one kept
#   2. opencode-db-prune      delete superseded event snapshots (needs opencode STOPPED)
#   3. opencode-db-vacuum     return the freed pages to the OS
#   4. nix GC + optimise      last, and only after the DB work is done, because
#                             the vacuum needs scratch space on the same volume
#
# The prune/vacuum steps REQUIRE every opencode process to be stopped. Both
# scripts refuse to run otherwise, so a stray session aborts them rather than
# corrupting anything.

set -euo pipefail

usage() {
    echo "Usage: full-clean.sh"
}

# This script takes no arguments (#1167): any argument was silently
# ignored and the full destructive sequence (switch, prune --apply,
# vacuum, sudo GC) ran with exit 0 -- so --help and --dry-run cleaned
# instead of printing usage / doing nothing. Reject the first argument
# by name, with usage on stderr and exit 2, before any step -- the
# house argument contract (#1145 rules 1 and 8). --help prints usage
# and exits 0.
if [[ $# -gt 0 ]]; then
    case "$1" in
        -h|--help) usage; exit 0 ;;
        *) echo "full-clean: unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
fi

cd "$(dirname "${BASH_SOURCE[0]}")"

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

# ---------------------------------------------------------------------------
step "Checking for running opencode processes"
# ---------------------------------------------------------------------------
if pgrep -f '(^|/)opencode($| )' >/dev/null 2>&1; then
  pgrep -lf '(^|/)opencode($| )' | cut -c1-100 | head -10
  echo
  echo "opencode is still running. Stop it first:" >&2
  echo "  pkill -f opencode && sleep 5 && pgrep -lf opencode" >&2
  exit 1
fi
echo "none running"

# ---------------------------------------------------------------------------
step "Switching darwin configuration"
# ---------------------------------------------------------------------------
./s

# ---------------------------------------------------------------------------
step "Pruning opencode event log"
# ---------------------------------------------------------------------------
# --protect-days 0 : age does not affect readability; every session stays fully
#                    readable because export/import and the TUI both read the
#                    message/part projections, never the event table.
# Backup is TRANSIENT crash insurance. Keeping it would defeat the point (a
# retained 40 GB copy costs more than the prune reclaims), so it is deleted
# after integrity_check and row counts verify.
./opencode-db-prune.sh --protect-days 0 --apply -y

# ---------------------------------------------------------------------------
step "Vacuuming opencode database"
# ---------------------------------------------------------------------------
# Deleting rows alone does not shrink the file: auto_vacuum is disabled, so the
# pages sit on the freelist until VACUUM hands them back. --auto-vacuum sets
# INCREMENTAL so future deletes reclaim without a full rebuild.
./opencode-db-vacuum.sh --auto-vacuum -y

# ---------------------------------------------------------------------------
step "Removing the transient prune backup"
# ---------------------------------------------------------------------------
BACKUP="${HOME}/.local/share/opencode/opencode.db.prune-backup"
OLD="${HOME}/.local/share/opencode/opencode.db.old"
for f in "$BACKUP" "$OLD"; do
  [ -e "$f" ] && { echo "removing $f"; rm -f "$f"; }
done

# ---------------------------------------------------------------------------
step "Nix garbage collection"
# ---------------------------------------------------------------------------
sudo nix-collect-garbage -d
nix-collect-garbage -d
nix store optimise

step "Done"
df -h / | tail -1
