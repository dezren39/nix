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
