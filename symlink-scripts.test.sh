#!/usr/bin/env bash
# Tests for symlink-commands / symlink-plugins (#967):
#   The pair that links this repo's commands/ and plugins/*.{js,ts} into
#   ${XDG_CONFIG_HOME:-$HOME/.config}/opencode/{commands,plugins} had no
#   test suite and was run by nothing automated, so its
#   link / replace-identical / preserve-conflict / exit-code contract —
#   and the deliberate glob divergence between the two scripts — could
#   regress or drift silently.
# This drives the REAL scripts (no pasted copies) in two ways:
#   * Scenario R runs the shipped scripts in place against a scratch
#     XDG_CONFIG_HOME and checks the links against the repo's real
#     commands/ and plugins/ contents.
#   * The fixture scenarios copy the shipped scripts byte-for-byte into
#     a fixture tree (the same thing the flake check does) so their
#     source dirs are deterministic, then pin every edge case:
#     fresh link, already-linked no-op, identical regular file replaced
#     (not a warning), conflicting regular file preserved + exit 1,
#     symlink pointing elsewhere (incl. broken) preserved + exit 1,
#     --dry-run changes nothing, unknown option exits 2, and the glob
#     divergence (commands links every regular file; plugins links only
#     *.{js,ts}, so *.test.mjs is NOT linked).
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}
check() { # check <description> <command...>
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "ok: $desc"
  else
    fail "$desc"
  fi
}

RC=0
run_script() { # run_script <script> <config-home> [args...] — sets RC; out/err in $TMP_ROOT/{out,err}
  local script="$1" cfg="$2"; shift 2
  XDG_CONFIG_HOME="$cfg" bash "$script" "$@" >"$TMP_ROOT/out" 2>"$TMP_ROOT/err"
  RC=$?
}

expect_rc() { # expect_rc <desc> <want>
  if [[ "$RC" -eq "$2" ]]; then
    echo "ok: $1 (exit $2)"
  else
    fail "$1 — expected exit $2, got $RC; stderr: $(cat "$TMP_ROOT/err")"
  fi
}

is_link_to() { # is_link_to <dest> <source>
  [[ -L "$1" && "$(readlink "$1")" == "$(realpath "$2")" ]]
}

# ─── Fixture tree ────────────────────────────────────────────────────────────
FIX="$TMP_ROOT/fixture"
mkdir -p "$FIX/commands/nested" "$FIX/plugins/nested"
cp "$SELF_DIR/symlink-commands" "$SELF_DIR/symlink-plugins" "$FIX/"
chmod +x "$FIX/symlink-commands" "$FIX/symlink-plugins"
check "fixture uses the shipped symlink-commands verbatim" cmp -s "$SELF_DIR/symlink-commands" "$FIX/symlink-commands"
check "fixture uses the shipped symlink-plugins verbatim" cmp -s "$SELF_DIR/symlink-plugins" "$FIX/symlink-plugins"

printf 'alpha command\n' >"$FIX/commands/alpha.md"
printf 'beta command\n' >"$FIX/commands/beta.txt"
printf 'gamma command\n' >"$FIX/commands/gamma"
printf 'nested\n' >"$FIX/commands/nested/inner.md"

printf 'console.log("one")\n' >"$FIX/plugins/one.js"
printf 'export {}\n' >"$FIX/plugins/two.ts"
printf 'test suite — must not be linked\n' >"$FIX/plugins/three.test.mjs"
printf 'docs — must not be linked\n' >"$FIX/plugins/four.md"
printf 'nested\n' >"$FIX/plugins/nested/five.js"

# ─── Scenario R: the real scripts, in place, real repo contents ─────────────
R="$TMP_ROOT/real"
run_script "$SELF_DIR/symlink-commands" "$R/cfg"
expect_rc "R commands: fresh run over real commands/" 0
grep -q "Command symlinks are up to date." "$TMP_ROOT/out" ||
  fail "R commands: success message missing"
shopt -s nullglob
for src in "$SELF_DIR/commands"/*; do
  [[ -f "$src" ]] || continue
  check "R commands: ${src##*/} linked" is_link_to "$R/cfg/opencode/commands/${src##*/}" "$src"
done
run_script "$SELF_DIR/symlink-plugins" "$R/cfg"
expect_rc "R plugins: fresh run over real plugins/" 0
grep -q "Plugin symlinks are up to date." "$TMP_ROOT/out" ||
  fail "R plugins: success message missing"
for src in "$SELF_DIR/plugins"/*.{js,ts}; do
  [[ -f "$src" ]] || continue
  check "R plugins: ${src##*/} linked" is_link_to "$R/cfg/opencode/plugins/${src##*/}" "$src"
done
for src in "$SELF_DIR/plugins"/*; do
  [[ -f "$src" ]] || continue
  case "$src" in
    *.js | *.ts) ;;
    *) check "R plugins: ${src##*/} NOT linked (glob is *.{js,ts} only)" test ! -e "$R/cfg/opencode/plugins/${src##*/}" ;;
  esac
done
shopt -u nullglob

# ─── Fixture scenarios, run for both scripts ─────────────────────────────────
# suite_for <label> <script> <dest-subdir> <primary> <linked...> -- <excluded...>
suite_for() {
  local label="$1" script="$2" sub="$3" primary="$4"; shift 4
  local linked=() excluded=() name dest src_dir
  while [[ "$1" != "--" ]]; do linked+=("$1"); shift; done
  shift
  excluded=("$@")
  src_dir="$(dirname "$script")"
  case "$sub" in
    commands) src_dir="$src_dir/commands" ;;
    plugins) src_dir="$src_dir/plugins" ;;
  esac

  # Fresh link + already-linked no-op.
  local cfg="$TMP_ROOT/$label-fresh/cfg"
  run_script "$script" "$cfg"
  expect_rc "$label: fresh link" 0
  for name in "${linked[@]}"; do
    check "$label: fresh: $name linked" is_link_to "$cfg/opencode/$sub/$name" "$src_dir/$name"
  done
  for name in "${excluded[@]}"; do
    check "$label: fresh: $name NOT linked" test ! -e "$cfg/opencode/$sub/$name"
    [[ ! -L "$cfg/opencode/$sub/$name" ]] || fail "$label: fresh: $name is a symlink"
  done
  run_script "$script" "$cfg"
  expect_rc "$label: already-linked no-op" 0
  for name in "${linked[@]}"; do
    check "$label: no-op: $name still linked" is_link_to "$cfg/opencode/$sub/$name" "$src_dir/$name"
  done

  # --dry-run makes no changes.
  cfg="$TMP_ROOT/$label-dry/cfg"
  run_script "$script" "$cfg" --dry-run
  expect_rc "$label: --dry-run" 0
  grep -q "Dry run complete; no changes made." "$TMP_ROOT/out" ||
    fail "$label: --dry-run completion message missing"
  check "$label: --dry-run created no destination dir" test ! -e "$cfg/opencode/$sub"

  # Identical regular file is replaced and does NOT count as a warning.
  cfg="$TMP_ROOT/$label-identical/cfg"
  mkdir -p "$cfg/opencode/$sub"
  cp "$src_dir/$primary" "$cfg/opencode/$sub/$primary"
  run_script "$script" "$cfg"
  expect_rc "$label: identical regular file replaced" 0
  check "$label: identical: $primary is now a symlink to the source" \
    is_link_to "$cfg/opencode/$sub/$primary" "$src_dir/$primary"
  grep -q "replacing identical regular file" "$TMP_ROOT/err" ||
    fail "$label: identical: replacement warning missing on stderr"
  if grep -q "Completed with" "$TMP_ROOT/err"; then
    fail "$label: identical: replacement counted toward the warning total"
  else
    echo "ok: $label: identical: replacement not counted as a warning"
  fi

  # Conflicting regular file is preserved byte-for-byte + exit 1.
  cfg="$TMP_ROOT/$label-conflict/cfg"
  mkdir -p "$cfg/opencode/$sub"
  printf 'conflicting local content\n' >"$cfg/opencode/$sub/$primary"
  printf 'conflicting local content\n' >"$TMP_ROOT/$label-expected"
  run_script "$script" "$cfg"
  expect_rc "$label: conflicting regular file" 1
  dest="$cfg/opencode/$sub/$primary"
  check "$label: conflict: $primary preserved byte-for-byte" cmp -s "$dest" "$TMP_ROOT/$label-expected"
  [[ ! -L "$dest" ]] || fail "$label: conflict: $primary was replaced with a symlink"
  grep -q "Completed with 1 warning(s)." "$TMP_ROOT/err" ||
    fail "$label: conflict: expected 'Completed with 1 warning(s).' on stderr"

  # Symlink pointing elsewhere is preserved + exit 1.
  cfg="$TMP_ROOT/$label-elsewhere/cfg"
  mkdir -p "$cfg/opencode/$sub" "$TMP_ROOT/$label-other"
  printf 'other\n' >"$TMP_ROOT/$label-other/target"
  ln -s "$TMP_ROOT/$label-other/target" "$cfg/opencode/$sub/$primary"
  run_script "$script" "$cfg"
  expect_rc "$label: symlink pointing elsewhere" 1
  check "$label: elsewhere: link preserved" \
    test "$(readlink "$cfg/opencode/$sub/$primary")" = "$TMP_ROOT/$label-other/target"
  grep -q "symlink points elsewhere" "$TMP_ROOT/err" ||
    fail "$label: elsewhere: warning missing on stderr"

  # Broken symlink is preserved + exit 1.
  cfg="$TMP_ROOT/$label-broken/cfg"
  mkdir -p "$cfg/opencode/$sub"
  ln -s "$cfg/opencode/$sub/does-not-exist" "$cfg/opencode/$sub/$primary"
  run_script "$script" "$cfg"
  expect_rc "$label: broken symlink" 1
  check "$label: broken: link preserved" \
    test "$(readlink "$cfg/opencode/$sub/$primary")" = "$cfg/opencode/$sub/does-not-exist"
  grep -q "symlink points elsewhere" "$TMP_ROOT/err" ||
    fail "$label: broken: warning missing on stderr"

  # Unknown option exits 2 with usage on stderr, before touching anything.
  cfg="$TMP_ROOT/$label-bogus/cfg"
  run_script "$script" "$cfg" --bogus
  expect_rc "$label: unknown option" 2
  grep -q "unknown option: --bogus" "$TMP_ROOT/err" ||
    fail "$label: unknown option: error missing on stderr"
  grep -q "Usage:" "$TMP_ROOT/err" ||
    fail "$label: unknown option: usage missing on stderr"
  check "$label: unknown option created nothing" test ! -e "$cfg/opencode/$sub"
}

suite_for commands "$FIX/symlink-commands" commands alpha.md \
  alpha.md beta.txt gamma -- inner.md
suite_for plugins "$FIX/symlink-plugins" plugins one.js \
  one.js two.ts -- three.test.mjs four.md five.js

if (( failures > 0 )); then
  echo "symlink-scripts tests: $failures failure(s)" >&2
  exit 1
fi
echo "symlink-scripts tests: all passed"
