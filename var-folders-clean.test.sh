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

# Argument guard (#1157): a second positional exits 2 naming it, and
# neither root is swept.
root1="$TMP_ROOT/root1"; root2="$TMP_ROOT/root2"
mkdir -p "$root1/aa" "$root2/aa"
rc=0
bash "$HELPER" "$root1" "$root2" >"$TMP_ROOT/out" 2>"$TMP_ROOT/err" || rc=$?
if [ "$rc" -ne 2 ]; then fail "two roots: expected exit 2, got $rc"; fi
if ! grep -qF "$root2" "$TMP_ROOT/err"; then fail "two roots: second root not named on stderr"; fi
if ! grep -q "usage: var-folders-clean.sh" "$TMP_ROOT/err"; then fail "two roots: usage not on stderr"; fi
if [ ! -e "$root1/aa" ] || [ ! -e "$root2/aa" ]; then fail "two roots: a root was swept despite the rejection"; fi

# Argument guard (#1157): a flag-shaped first argument is not a silent
# no-op — exit 2 naming it, with usage on stderr.
rc=0
bash "$HELPER" --bogus >"$TMP_ROOT/out" 2>"$TMP_ROOT/err" || rc=$?
if [ "$rc" -ne 2 ]; then fail "--bogus: expected exit 2, got $rc"; fi
if ! grep -q -- "--bogus" "$TMP_ROOT/err"; then fail "--bogus: argument not named on stderr"; fi
if ! grep -q "usage: var-folders-clean.sh" "$TMP_ROOT/err"; then fail "--bogus: usage not on stderr"; fi

# --help prints usage and exits 0.
rc=0
bash "$HELPER" --help >"$TMP_ROOT/out" 2>"$TMP_ROOT/err" || rc=$?
if [ "$rc" -ne 0 ]; then fail "--help: expected exit 0, got $rc"; fi
if ! grep -q "usage: var-folders-clean.sh" "$TMP_ROOT/out"; then fail "--help: usage not printed"; fi

if [ "$failures" -eq 0 ]; then
  echo "PASS: /private/var/folders cleaning logic verified successfully."
else
  echo "FAIL: test failed with $failures errors" >&2
  exit 1
fi
