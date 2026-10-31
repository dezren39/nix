#!/usr/bin/env bash
# Clear a /private/var/folders-style root, keeping the zz directory
# itself but emptying its contents (#553, factored out for #557).
#
# Usage: var-folders-clean.sh <folders-root>
#
# Deletes every top-level entry of <folders-root> except zz, then
# deletes every entry inside zz (zz itself is kept). A missing root
# is a no-op. This is the exact sweep clean.sh runs with
# --tmpdir (CLEAN_TMPDIR=1); clean.sh calls this script so the test
# (var-folders-clean.test.sh) exercises the shipped logic, not a copy.
#
# A deletion that fails is named on stderr and the script exits 1
# once the rest of the sweep has run (#1262).
set -uo pipefail

usage() {
  echo "usage: var-folders-clean.sh <folders-root>"
}

# Argument guard (#1157): exactly one root. A second positional was
# silently ignored (the named target was never cleaned, exit 0) and a
# flag-shaped first argument was treated as a missing root (a silent
# no-op). Reject both by name, with usage on stderr and exit 2, before
# any deletion. --help prints usage and exits 0. The no-argument case
# still fails through the ${1:?} expansion below, unchanged.
if [ $# -ge 1 ]; then
  case "$1" in
    -h|--help) usage; exit 0 ;;
    -*) echo "var-folders-clean.sh: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
fi
if [ $# -gt 1 ]; then
  echo "var-folders-clean.sh: unexpected argument: $2" >&2; usage >&2; exit 2
fi

folders="${1:?usage: var-folders-clean.sh <folders-root>}"

failed=0
if [ -d "$folders" ]; then
  while IFS= read -r -d "" i; do
    if [ "$(basename "$i")" != "zz" ] && ! rm -rf "$i" 2>/dev/null; then
      failed=$((failed + 1))
      echo "var-folders-clean.sh: failed to delete: $i" >&2
    fi
  done < <(find "$folders" -mindepth 1 -maxdepth 1 -print0 2>/dev/null)
  if [ -d "$folders/zz" ]; then
    while IFS= read -r -d "" i; do
      if ! rm -rf "$i" 2>/dev/null; then
        failed=$((failed + 1))
        echo "var-folders-clean.sh: failed to delete: $i" >&2
      fi
    done < <(find "$folders/zz" -mindepth 1 -maxdepth 1 -print0 2>/dev/null)
  fi
fi
if [ "$failed" -gt 0 ]; then
  echo "var-folders-clean.sh: $failed deletion(s) failed" >&2
  exit 1
fi
