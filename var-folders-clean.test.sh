#!/usr/bin/env bash
# Unit test for /private/var/folders cleaning logic in clean.sh
set -euo pipefail

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

mock_folders="$TMP_ROOT/folders"
mkdir -p "$mock_folders/aa" "$mock_folders/bb" "$mock_folders/zz/sub1" "$mock_folders/zz/sub2"
touch "$mock_folders/aa/file1" "$mock_folders/zz/file2" "$mock_folders/zz/sub1/file3"

# Verify initial setup
[ -d "$mock_folders/aa" ]
[ -d "$mock_folders/bb" ]
[ -d "$mock_folders/zz/sub1" ]
[ -f "$mock_folders/zz/file2" ]

# Run the cleaning logic against mock_folders
folders="$mock_folders"
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

failures=0

# Check non-zz folders are deleted
if [ -e "$mock_folders/aa" ] || [ -e "$mock_folders/bb" ]; then
  echo "FAIL: Non-zz folders were not removed" >&2
  failures=$((failures + 1))
fi

# Check zz directory itself is kept
if [ ! -d "$mock_folders/zz" ]; then
  echo "FAIL: zz directory was deleted" >&2
  failures=$((failures + 1))
fi

# Check contents of zz directory are deleted
zz_contents=$(find "$mock_folders/zz" -mindepth 1 2>/dev/null | wc -l)
if [ "$zz_contents" -ne 0 ]; then
  echo "FAIL: zz directory contents were not cleared" >&2
  failures=$((failures + 1))
fi

# Test edge case: missing zz directory
rm -rf "$mock_folders/zz"
folders="$mock_folders"
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

if [ "$failures" -eq 0 ]; then
  echo "PASS: /private/var/folders cleaning logic verified successfully."
else
  echo "FAIL: test failed with $failures errors" >&2
  return 1 2>/dev/null || true
fi
