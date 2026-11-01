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
# once the rest of the sweep has run (#1262). An enumeration that
# fails is likewise named on stderr and exits 1 (#1321): each find
# runs in a pipeline whose first-command status is read back from
# PIPESTATUS — a process substitution's status is invisible to
# the script, so a failed find used to run the loops zero times and
# exit 0 with nothing (or only part) deleted. No temp file holds
# the enumeration: mktemp honours TMPDIR, which on the real macOS
# layout lives inside the swept root, so the sweep itself could
# delete the file's directory mid-run (Codex review, PR #1322).
# lastpipe keeps the delete loops in this shell so the failed
# counter survives the pipeline.
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
  shopt -s lastpipe
  find "$folders" -mindepth 1 -maxdepth 1 -print0 2>/dev/null | while IFS= read -r -d "" i; do
    if [ "$(basename "$i")" != "zz" ] && ! rm -rf "$i" 2>/dev/null; then
      failed=$((failed + 1))
      echo "var-folders-clean.sh: failed to delete: $i" >&2
    fi
  done
  root_enum_rc=${PIPESTATUS[0]}
  if [ "$root_enum_rc" -ne 0 ]; then
    failed=$((failed + 1))
    echo "var-folders-clean.sh: failed to enumerate: $folders" >&2
  elif [ -d "$folders/zz" ]; then
    find "$folders/zz" -mindepth 1 -maxdepth 1 -print0 2>/dev/null | while IFS= read -r -d "" i; do
      if ! rm -rf "$i" 2>/dev/null; then
        failed=$((failed + 1))
        echo "var-folders-clean.sh: failed to delete: $i" >&2
      fi
    done
    zz_enum_rc=${PIPESTATUS[0]}
    if [ "$zz_enum_rc" -ne 0 ]; then
      failed=$((failed + 1))
      echo "var-folders-clean.sh: failed to enumerate: $folders/zz" >&2
    fi
  fi
fi
if [ "$failed" -gt 0 ]; then
  echo "var-folders-clean.sh: $failed deletion(s) failed" >&2
  exit 1
fi
