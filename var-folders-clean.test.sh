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

# Deletion failure (#1262): an rm shim on PATH fails only for the aa
# entry, regardless of the privileges the suite runs with (a read-only
# fixture dir does not stop root — the #1257 lesson). aa is not the
# last thing swept: the zz contents are removed after it, so a final-
# command accident cannot leak the failure out as the exit code. The
# script must exit non-zero, name aa on stderr, leave aa in place,
# and still sweep everything else.
shim_dir="$TMP_ROOT/rm-shim"
mkdir -p "$shim_dir"
# Resolve the real rm before the shim shadows it: there is no
# portable absolute path (/bin/rm does not exist on NixOS).
real_rm="$(command -v rm)"
cat > "$shim_dir/rm" <<SHIM_EOF
#!/bin/sh
for arg in "\$@"; do
  case "\$arg" in
    */aa) echo "rm: cannot remove (shimmed failure)" >&2; exit 1 ;;
  esac
done
exec $real_rm "\$@"
SHIM_EOF
chmod +x "$shim_dir/rm"
fail_root="$TMP_ROOT/failfolders"
mkdir -p "$fail_root/aa" "$fail_root/zz"
touch "$fail_root/aa/file" "$fail_root/zz/file2"
rc=0
PATH="$shim_dir:$PATH" bash "$HELPER" "$fail_root" >"$TMP_ROOT/out" 2>"$TMP_ROOT/err" || rc=$?
if [ "$rc" -eq 0 ]; then fail "rm failure: expected non-zero exit, got 0"; fi
if ! grep -qF "$fail_root/aa" "$TMP_ROOT/err"; then fail "rm failure: failed entry not named on stderr"; fi
if [ ! -e "$fail_root/aa/file" ]; then fail "rm failure: shimmed entry was removed anyway"; fi
if [ -e "$fail_root/zz/file2" ]; then fail "rm failure: zz contents were not swept after the failure"; fi

# Enumeration failure (#1321): a find shim on PATH fails only for one
# named directory, regardless of the privileges the suite runs with
# (a chmod 000 fixture does not stop root — the #1257 lesson, same
# pattern as the #1262 rm shim above). The script must exit non-zero
# and name the directory it could not enumerate on stderr, with one
# clean line — find's own raw stderr stays suppressed.
find_shim_dir="$TMP_ROOT/find-shim"
mkdir -p "$find_shim_dir"
# Resolve the real find before the shim shadows it.
real_find="$(command -v find)"
make_find_shim() {
  # $1 = the exact path argument the shimmed find fails on
  cat > "$find_shim_dir/find" <<SHIM_EOF
#!/bin/sh
for arg in "\$@"; do
  if [ "\$arg" = "$1" ]; then
    echo "find: $1: Permission denied (shimmed failure)" >&2
    exit 1
  fi
done
exec $real_find "\$@"
SHIM_EOF
  chmod +x "$find_shim_dir/find"
}

# Case 1: enumerating the folders root fails — non-zero exit, the
# root named on stderr, and every entry (top-level and zz contents)
# left in place.
enum_root="$TMP_ROOT/enumfolders"
mkdir -p "$enum_root/aa" "$enum_root/zz/inner"
touch "$enum_root/aa/f" "$enum_root/topfile" "$enum_root/zz/inner/g"
make_find_shim "$enum_root"
rc=0
PATH="$find_shim_dir:$PATH" bash "$HELPER" "$enum_root" >"$TMP_ROOT/out" 2>"$TMP_ROOT/err" || rc=$?
if [ "$rc" -eq 0 ]; then fail "find failure (root): expected non-zero exit, got 0"; fi
if ! grep -qF "failed to enumerate: $enum_root" "$TMP_ROOT/err"; then fail "find failure (root): failed root not named on stderr"; fi
if [ ! -e "$enum_root/aa/f" ] || [ ! -e "$enum_root/topfile" ] || [ ! -e "$enum_root/zz/inner/g" ]; then fail "find failure (root): entries were swept despite the failed enumeration"; fi

# Case 2: enumerating zz/ fails — non-zero exit and zz named on
# stderr even though the top-level sweep still ran (the sibling is
# deleted, zz contents remain).
enum_zz="$TMP_ROOT/enumzz"
mkdir -p "$enum_zz/aa" "$enum_zz/zz"
touch "$enum_zz/aa/f" "$enum_zz/zz/keepme"
make_find_shim "$enum_zz/zz"
rc=0
PATH="$find_shim_dir:$PATH" bash "$HELPER" "$enum_zz" >"$TMP_ROOT/out" 2>"$TMP_ROOT/err" || rc=$?
if [ "$rc" -eq 0 ]; then fail "find failure (zz): expected non-zero exit, got 0"; fi
if ! grep -qF "failed to enumerate: $enum_zz/zz" "$TMP_ROOT/err"; then fail "find failure (zz): failed zz not named on stderr"; fi
if [ -e "$enum_zz/aa" ]; then fail "find failure (zz): top-level sweep did not run"; fi
if [ ! -e "$enum_zz/zz/keepme" ]; then fail "find failure (zz): zz contents were removed anyway"; fi

if [ "$failures" -eq 0 ]; then
  echo "PASS: /private/var/folders cleaning logic verified successfully."
else
  echo "FAIL: test failed with $failures errors" >&2
  exit 1
fi
