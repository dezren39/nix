#!/usr/bin/env bash
# Tests for search.sh (#983):
#   PR #959 fixed #957 — a failed `brew list --cask` used to be hidden
#   by process substitutions (status invisible, stderr discarded), so
#   brew breakage looked like "no casks installed" and every app was
#   reported as not brew-installed with exit 0. The fix captures the
#   list once with its status checked and exits 1 naming the step —
#   but shipped with no test suite and no flake check; its only
#   verification was a one-off manual stub-brew run.
# This drives the REAL search.sh with a stub brew and scratch app
# dirs, and pins:
#   * failing `brew list` -> exit 1, the step named on stderr, and
#     no findings reported;
#   * missing brew -> the same exit-1 path;
#   * working brew -> only apps not in the cask list are reported;
#   * empty successful list -> all apps reported, exit 0;
#   * `brew list` runs exactly once per invocation (the stub logs
#     every call; per-app `brew search` calls are separate);
#   * an unknown argument -> exit 2 naming it, usage on stderr, and
#     no brew call at all; --help -> usage, exit 0, no brew call (#1158).
# The first leg's /Applications is redirected with the script's
# SEARCH_APPLICATIONS_DIR seam (#983); the second leg uses the
# scratch HOME's ~/Applications. Stubs use #!/bin/sh: the nix build
# sandbox has no /usr/bin/env.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/search.sh"

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

BIN="$TMP_ROOT/bin"
HOME_DIR="$TMP_ROOT/home"
APPS="$TMP_ROOT/Applications"
mkdir -p "$BIN" "$HOME_DIR/Applications" "$APPS/Foo.app" "$APPS/Bar.app" "$HOME_DIR/Applications/Bar.app"
LOG="$TMP_ROOT/brew.log"
: > "$LOG"

cat > "$BIN/brew" <<'EOF'
#!/bin/sh
# brew stub: modes via STUB_BREW_MODE (working|empty|failing);
# every invocation is appended to $STUB_BREW_LOG.
echo "$1" >> "$STUB_BREW_LOG"
case "$1" in
  list)
    case "$STUB_BREW_MODE" in
      working) echo "foo" ;;
      empty) : ;;
      failing) echo "Error: brew is broken" >&2; exit 1 ;;
    esac
    ;;
  search)
    : # no similar packages
    ;;
  *)
    echo "brew stub: unexpected command: $1" >&2
    exit 1
    ;;
esac
exit 0
EOF
chmod +x "$BIN/brew"

RC=0
run_search() { # run_search <mode|missing> — sets RC; out/err under $TMP_ROOT
  local mode="$1"
  : > "$LOG"
  if [ "$mode" = "missing" ]; then
    # no brew stub, and neither test environment (laptop, nix sandbox) has a real brew on PATH
    HOME="$HOME_DIR" SEARCH_APPLICATIONS_DIR="$APPS" STUB_BREW_LOG="$LOG" \
      bash "$SCRIPT" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  else
    HOME="$HOME_DIR" SEARCH_APPLICATIONS_DIR="$APPS" STUB_BREW_LOG="$LOG" \
      STUB_BREW_MODE="$mode" PATH="$BIN:$PATH" \
      bash "$SCRIPT" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  fi
  RC=$?
}
expect_rc() { # expect_rc <desc> <want>
  if [ "$RC" -eq "$2" ]; then
    echo "ok: $1 (exit $2)"
  else
    fail "$1 — expected exit $2, got $RC; stderr: $(cat "$TMP_ROOT/err")"
  fi
}
list_calls() { grep -c '^list$' "$LOG" || true; }

# --- working brew: foo is a cask, bar is not --------------------------------
run_search working
expect_rc "working brew exits 0" 0
check "working brew does not report the cask app (foo)" \
  bash -c "! grep -qx foo '$TMP_ROOT/out'"
check "working brew reports the non-cask app (bar)" \
  grep -qx bar "$TMP_ROOT/out"
if [ "$(list_calls)" = "1" ]; then
  echo "ok: brew list ran exactly once (working)"
else
  fail "brew list ran exactly once (working) — got $(list_calls)"
fi

# --- empty successful list: everything is reported ---------------------------
run_search empty
expect_rc "empty brew list exits 0" 0
check "empty list reports foo" grep -qx foo "$TMP_ROOT/out"
check "empty list reports bar" grep -qx bar "$TMP_ROOT/out"
if [ "$(list_calls)" = "1" ]; then
  echo "ok: brew list ran exactly once (empty)"
else
  fail "brew list ran exactly once (empty) — got $(list_calls)"
fi

# --- failing brew list: loud exit 1, no findings -----------------------------
run_search failing
expect_rc "failing brew list exits 1" 1
check "failing brew list names the step on stderr" \
  grep -q "brew list --cask" "$TMP_ROOT/err"
check "failing brew list surfaces brew's own stderr" \
  grep -q "brew is broken" "$TMP_ROOT/err"
check "failing brew list reports no findings" \
  bash -c "! grep -qx -e foo -e bar '$TMP_ROOT/out'"

# --- unknown argument: exit 2, named, before any brew call (#1158) -----------
: > "$LOG"
HOME="$HOME_DIR" SEARCH_APPLICATIONS_DIR="$APPS" STUB_BREW_LOG="$LOG" \
  STUB_BREW_MODE=working PATH="$BIN:$PATH" \
  bash "$SCRIPT" --bogus > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
RC=$?
expect_rc "unknown argument exits 2" 2
check "unknown argument is named on stderr" \
  grep -q -- "--bogus" "$TMP_ROOT/err"
check "unknown argument prints usage on stderr" \
  grep -q "Usage: search.sh" "$TMP_ROOT/err"
check "unknown argument reports no findings" \
  bash -c "! grep -qx -e foo -e bar '$TMP_ROOT/out'"
if [ ! -s "$LOG" ]; then
  echo "ok: unknown argument made no brew calls"
else
  fail "unknown argument made no brew calls -- log: $(cat "$LOG")"
fi

# --- --help: usage, exit 0, no brew calls -------------------------------------
: > "$LOG"
HOME="$HOME_DIR" SEARCH_APPLICATIONS_DIR="$APPS" STUB_BREW_LOG="$LOG" \
  STUB_BREW_MODE=working PATH="$BIN:$PATH" \
  bash "$SCRIPT" --help > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
RC=$?
expect_rc "--help exits 0" 0
check "--help prints usage" grep -q "Usage: search.sh" "$TMP_ROOT/out"
if [ ! -s "$LOG" ]; then
  echo "ok: --help made no brew calls"
else
  fail "--help made no brew calls -- log: $(cat "$LOG")"
fi

# --- missing brew: same exit-1 path ------------------------------------------
run_search missing
expect_rc "missing brew exits 1" 1
check "missing brew names the step on stderr" \
  grep -q "brew list --cask" "$TMP_ROOT/err"
check "missing brew reports no findings" \
  bash -c "! grep -qx -e foo -e bar '$TMP_ROOT/out'"

if [ "$failures" -gt 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all search.sh checks passed"
