#!/usr/bin/env bash
# Test for the /private/var/folders sweep clean.sh runs with --tmpdir.
#
# This drives the REAL sweep — var-folders-clean.sh, the script
# clean.sh calls — against a mock root (#557: the original version
# pasted a copy of the loops into itself, so it passed even when the
# shipped logic regressed, and its failure branch ended in
# `return 1 2>/dev/null || true` at top level, exiting 0 on failure).
# It is run by the flake check `var-folders-clean-tests`.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELPER="$SELF_DIR/var-folders-clean.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

mock_folders="$TMP_ROOT/folders"
mkdir -p "$mock_folders/aa" "$mock_folders/bb" "$mock_folders/zz/sub1" "$mock_folders/zz/sub2"
touch "$mock_folders/aa/file1" "$mock_folders/zz/file2" "$mock_folders/zz/sub1/file3"

bash "$HELPER" "$mock_folders"

# Non-zz folders are deleted
if [ -e "$mock_folders/aa" ] || [ -e "$mock_folders/bb" ]; then
  fail "non-zz folders were not removed"
fi

# The zz directory itself is kept
if [ ! -d "$mock_folders/zz" ]; then
  fail "zz directory was deleted"
fi

# Contents of zz are deleted
zz_contents=$(find "$mock_folders/zz" -mindepth 1 2>/dev/null | wc -l)
if [ "$zz_contents" -ne 0 ]; then
  fail "zz directory contents were not cleared"
fi

# Edge case: missing zz directory — the rest is still swept, no error
rm -rf "$mock_folders/zz"
mkdir -p "$mock_folders/cc"
bash "$HELPER" "$mock_folders"
if [ -e "$mock_folders/cc" ]; then
  fail "folders were not removed when zz is missing"
fi

# Edge case: missing root entirely — a no-op, not an error
bash "$HELPER" "$TMP_ROOT/does-not-exist"

if [ "$failures" -eq 0 ]; then
  echo "PASS: /private/var/folders cleaning logic verified successfully."
else
  echo "FAIL: test failed with $failures errors" >&2
  exit 1
fi
