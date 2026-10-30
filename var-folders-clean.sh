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

if [ -d "$folders" ]; then
  while IFS= read -r -d "" i; do
    [ "$(basename "$i")" != "zz" ] && rm -rf "$i" 2>/dev/null
  done < <(find "$folders" -mindepth 1 -maxdepth 1 -print0 2>/dev/null)
  if [ -d "$folders/zz" ]; then
    while IFS= read -r -d "" i; do
      rm -rf "$i" 2>/dev/null
    done < <(find "$folders/zz" -mindepth 1 -maxdepth 1 -print0 2>/dev/null)
  fi
fi
