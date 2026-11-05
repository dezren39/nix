#!/usr/bin/env bash
# Keep the install commands, config.toml, and vsc-extensions.txt in sync.
# Also mutation-test each copy so the drift check cannot silently stop
# detecting a changed list.
set -euo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG="${1:-$SELF_DIR/config.toml}"
SCRIPT="${2:-$SELF_DIR/code-install-extensions.sh}"
TEXT="${3:-$SELF_DIR/vsc-extensions.txt}"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

fail() { echo "FAIL: $1" >&2; exit 1; }
ok() { echo "ok: $1"; }

extract_config() {
  awk '
    /^extensions[[:space:]]*=[[:space:]]*\[/ { in_extensions = 1; next }
    in_extensions && /^[[:space:]]*\]/ { exit }
    in_extensions && /^[[:space:]]*"/ {
      sub(/^[[:space:]]*"/, "")
      sub(/",?[[:space:]]*$/, "")
      print
    }
  ' "$1"
}

extract_installer() {
  sed -nE 's/^[[:space:]]*code --install-extension ([^[:space:]]+) --force[[:space:]]*$/\1/p' "$1"
}

extract_text() {
  sed -e '/^[[:space:]]*$/d' -e 's/\r$//' "$1"
}

compare_pair() {
  local left_label="$1" left="$2" right_label="$3" right="$4"
  if cmp -s "$left" "$right"; then
    return 0
  fi

  echo "extension list drift: $left_label differs from $right_label" >&2
  awk -v left_label="$left_label" -v right_label="$right_label" '
    FNR == NR { left[FNR] = $0; left_count = FNR; next }
    { right[FNR] = $0; right_count = FNR }
    END {
      count = left_count > right_count ? left_count : right_count
      for (i = 1; i <= count; i++) {
        a = (i in left) ? left[i] : "<missing>"
        b = (i in right) ? right[i] : "<missing>"
        if (a != b)
          printf "  line %d: %s=%s; %s=%s\n", i, left_label, a, right_label, b > "/dev/stderr"
      }
    }
  ' "$left" "$right"
  return 1
}

check_sync() {
  local config="$1" script="$2" text="$3" name="$4"
  local dir="$TMP_ROOT/$name" failed=0
  mkdir -p "$dir"
  extract_config "$config" >"$dir/config.list"
  extract_installer "$script" >"$dir/installer.list"
  extract_text "$text" >"$dir/text.list"

  [ -s "$dir/config.list" ] || fail "no extensions found in $config"
  [ -s "$dir/installer.list" ] || fail "no install commands found in $script"
  [ -s "$dir/text.list" ] || fail "no extensions found in $text"

  compare_pair config.toml "$dir/config.list" installer "$dir/installer.list" || failed=1
  compare_pair config.toml "$dir/config.list" vsc-extensions.txt "$dir/text.list" || failed=1
  compare_pair installer "$dir/installer.list" vsc-extensions.txt "$dir/text.list" || failed=1
  return "$failed"
}

check_sync "$CONFIG" "$SCRIPT" "$TEXT" current || fail "extension lists are out of sync"
ok "config.toml, installer, and vsc-extensions.txt lists match in order"

awk '
  /^[[:space:]]*\][[:space:]]*$/ && !inserted {
    print "\"queue.test-drift\","
    inserted = 1
  }
  { print }
  END { if (!inserted) exit 1 }
' "$CONFIG" >"$TMP_ROOT/config-mutated.toml"
cp "$SCRIPT" "$TMP_ROOT/installer-mutated.sh"
chmod u+w "$TMP_ROOT/installer-mutated.sh"
printf '\ncode --install-extension queue.test-drift --force\n' >>"$TMP_ROOT/installer-mutated.sh"
cp "$TEXT" "$TMP_ROOT/text-mutated.txt"
chmod u+w "$TMP_ROOT/text-mutated.txt"
printf 'queue.test-drift\n' >>"$TMP_ROOT/text-mutated.txt"

expect_drift() {
  local label="$1"
  local log="$TMP_ROOT/$label.log"
  shift
  if check_sync "$@" "$label" >"$log" 2>&1; then
    fail "$label mutation was not detected"
  fi
  grep -Fq 'queue.test-drift' "$log" || {
    cat "$log" >&2
    fail "$label mutation did not name the differing extension"
  }
  ok "$label mutation is detected and names queue.test-drift"
}

expect_drift config-mutation "$TMP_ROOT/config-mutated.toml" "$SCRIPT" "$TEXT"
expect_drift installer-mutation "$CONFIG" "$TMP_ROOT/installer-mutated.sh" "$TEXT"
expect_drift text-mutation "$CONFIG" "$SCRIPT" "$TMP_ROOT/text-mutated.txt"
