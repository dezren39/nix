#!/usr/bin/env bash
# Regression test for patch-vscode-insiders.sh's exit-code and
# write-back contracts (#969).
#
# PR #925 (for #909) made the patch script exit non-zero when a patch
# target is missing and write files back only when their content
# changed (the launchd agent's WatchPaths are the patched files, so a
# no-change rewrite retriggers the agent); PR #938 (for #931) extended
# the missing-target rule to a missing JS workbench file. Neither
# contract had a committed test: both PRs were verified by hand with
# the /Applications prefix retargeted in a scratch copy.
#
# This drives the REAL script against a fixture app tree via the
# VSCODE_INSIDERS_APP_DIR seam (which defaults to the shipped
# /Applications path). Covered:
#   all targets present      -> exit 0, all five patches applied
#   second run (idempotent)  -> exit 0, neither bytes nor mtimes change
#   unrelated CSS/JS content -> exit 1, Patch 1, Patch 2, Patch 5 named
#   missing JS file          -> exit 1, Patch 5 named as missing-file
#   missing CSS file         -> exit 1, CSS-not-found error
#   --help                   -> exit 0, usage, fixture files unchanged
#   unknown argument         -> exit 2 naming it, usage on stderr,
#                               fixture files unchanged (#1177)
#   no override (when no real Insiders app is installed)
#                            -> exit 1 naming the default /Applications
#                               path, pinning the seam's default
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/patch-vscode-insiders.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

APP="$TMP_ROOT/Visual Studio Code - Insiders.app"
WB="$APP/Contents/Resources/app/out/vs/workbench"
CSS="$WB/workbench.desktop.main.css"
JS="$WB/workbench.desktop.main.js"

CSS_OLD1='.monaco-workbench .part>.composite.header-or-footer,.monaco-workbench .part>.composite.title{display:flex}'
CSS_OLD2='.monaco-workbench .part>.title,.monaco-workbench .part>.header-or-footer{height:35px;display:flex;box-sizing:border-box;overflow:hidden}'
JS_OLD='.HEADER_HEIGHT=35}static{this.TITLE_HEIGHT=35}static{this.Footer_HEIGHT=35}'

make_fixture() {
  mkdir -p "$WB"
  printf '%s\n%s\n' "$CSS_OLD1" "$CSS_OLD2" >"$CSS"
  printf 'x%sy\n' "$JS_OLD" >"$JS"
}

run_patch() { # sets OUT, STATUS
  OUT="$(VSCODE_INSIDERS_APP_DIR="$APP" bash "$SCRIPT" 2>&1)"
  STATUS=$?
}

mtime() { stat -c %y "$1" 2>/dev/null || stat -f %m "$1"; }

# ─── All targets present: every patch applies, exit 0 ─────────────────
make_fixture
run_patch
[ "$STATUS" -eq 0 ] || fail "all-targets run exited $STATUS (want 0; got: $OUT)"
grep -qF 'display:none!important;height:0!important' "$CSS" \
  && echo "ok: Patch 1 applied" || fail "Patch 1 replacement not in CSS"
grep -qF 'height:0px!important' "$CSS" \
  && echo "ok: Patch 2 applied" || fail "Patch 2 replacement not in CSS"
grep -qF 'Patch 5' <<<"$OUT" && grep -qF 'HEADER_HEIGHT=0' "$JS" \
  && echo "ok: Patch 5 applied" || fail "Patch 5 replacement not in JS"

# ─── Second run: no write-back when nothing changed (#925) ────────────
CSS_SUM="$(sha256sum "$CSS")"; JS_SUM="$(sha256sum "$JS")"
CSS_MT="$(mtime "$CSS")"; JS_MT="$(mtime "$JS")"
run_patch
[ "$STATUS" -eq 0 ] || fail "second run exited $STATUS (want 0; got: $OUT)"
[ "$(sha256sum "$CSS")" = "$CSS_SUM" ] && [ "$(sha256sum "$JS")" = "$JS_SUM" ] \
  && echo "ok: second run changed no bytes" || fail "second run changed file bytes"
[ "$(mtime "$CSS")" = "$CSS_MT" ] && [ "$(mtime "$JS")" = "$JS_MT" ] \
  && echo "ok: second run rewrote neither file (mtimes unchanged)" \
  || fail "second run rewrote a file whose content did not change"

# ─── Unrelated content: exit 1 naming Patch 1, Patch 2 and Patch 5 ────
printf 'body{color:red}\n' >"$CSS"
printf 'var x = 1;\n' >"$JS"
run_patch
[ "$STATUS" -eq 1 ] || fail "unrelated-content run exited $STATUS (want 1)"
for p in "Patch 1: Target not found" "Patch 2: Target not found" "Patch 5: Target not found"; do
  case "$OUT" in
    *"$p"*) echo "ok: unrelated content names $p" ;;
    *) fail "unrelated-content output did not name '$p' (got: $OUT)" ;;
  esac
done

# ─── Missing JS file: exit 1 naming Patch 5 (#938) ────────────────────
make_fixture
rm "$JS"
run_patch
[ "$STATUS" -eq 1 ] || fail "missing-JS run exited $STATUS (want 1)"
case "$OUT" in
  *"Patch 5: Target not found (JS workbench file missing)"*) echo "ok: missing JS names Patch 5" ;;
  *) fail "missing-JS output did not name Patch 5 as missing-file (got: $OUT)" ;;
esac

# ─── Missing CSS file: exit 1 with the CSS-not-found error ────────────
rm -rf "$APP"
run_patch
[ "$STATUS" -eq 1 ] || fail "missing-CSS run exited $STATUS (want 1)"
case "$OUT" in
  *"ERROR: VSCode Insiders CSS file not found at:"*) echo "ok: missing CSS prints the not-found error" ;;
  *) fail "missing-CSS output lacked the not-found error (got: $OUT)" ;;
esac

# ─── Seam default: no override names the shipped /Applications path ───
DEFAULT_CSS="/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/out/vs/workbench/workbench.desktop.main.css"
if [ ! -e "$DEFAULT_CSS" ]; then
  OUT="$(env -u VSCODE_INSIDERS_APP_DIR bash "$SCRIPT" 2>&1)"
  STATUS=$?
  [ "$STATUS" -eq 1 ] || fail "no-override run exited $STATUS (want 1 when no app is installed)"
  case "$OUT" in
    *"$DEFAULT_CSS"*) echo "ok: no override uses the default /Applications path" ;;
    *) fail "no-override output did not name the default path (got: $OUT)" ;;
  esac
else
  echo "ok: seam-default case skipped (a real Insiders app is installed here)"
fi

# ─── --help: exit 0, usage, no files modified (#1177) ─────────────────
make_fixture
CSS_SUM="$(sha256sum "$CSS")"; JS_SUM="$(sha256sum "$JS")"
OUT="$(VSCODE_INSIDERS_APP_DIR="$APP" bash "$SCRIPT" --help 2>"$TMP_ROOT/help.err")"
STATUS=$?
[ "$STATUS" -eq 0 ] || fail "--help exited $STATUS (want 0; got: $OUT)"
case "$OUT" in
  *"Usage: bash ~/nix/patch-vscode-insiders.sh"*) echo "ok: --help prints usage" ;;
  *) fail "--help did not print usage (got: $OUT)" ;;
esac
[ "$(sha256sum "$CSS")" = "$CSS_SUM" ] && [ "$(sha256sum "$JS")" = "$JS_SUM" ] \
  && echo "ok: --help modified no files" || fail "--help modified a fixture file"

# ─── Unknown argument: exit 2 naming it, usage on stderr, no change ──
OUT="$(VSCODE_INSIDERS_APP_DIR="$APP" bash "$SCRIPT" --bogus 2>"$TMP_ROOT/bogus.err")"
STATUS=$?
[ "$STATUS" -eq 2 ] || fail "unknown-argument run exited $STATUS (want 2; got: $OUT)"
grep -q -- "--bogus" "$TMP_ROOT/bogus.err" \
  && echo "ok: unknown argument named on stderr" || fail "unknown argument not named on stderr"
grep -q "Usage: bash ~/nix/patch-vscode-insiders.sh" "$TMP_ROOT/bogus.err" \
  && echo "ok: unknown argument prints usage on stderr" || fail "unknown argument did not print usage on stderr"
[ "$(sha256sum "$CSS")" = "$CSS_SUM" ] && [ "$(sha256sum "$JS")" = "$JS_SUM" ] \
  && echo "ok: unknown argument modified no files" || fail "unknown argument modified a fixture file"

[ "$failures" -eq 0 ] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all patch-vscode-insiders contract checks passed"
