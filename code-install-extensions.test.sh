#!/usr/bin/env bash
# Regression test for code-install-extensions.sh's failure reporting
# (#1287).
#
# The script used to be 691 bare `code --install-extension <id> --force`
# lines with no failure handling, so its exit status was the last
# line's alone: a failed install exited 0 unless it happened to be the
# last line, and nothing named or counted the failures.
#
# This drives the REAL code-install-extensions.sh with a stub `code`
# binary on PATH (the repo's stub-binary style) and pins:
#   all succeed           -> exit 0, success summary, every extension
#                            attempted once, in list order, with --force
#   middle-of-list failure -> non-zero exit, the failed extension named
#                            and counted in the summary, and every later
#                            extension still attempted
#   last-line failure      -> non-zero exit, the failed extension named
#   two failures           -> non-zero exit, both named, count is 2
#   -h / --help (#1319)    -> usage on stdout, exit 0, zero installs
#   any other argument     -> the argument named on stderr, usage on
#                            stderr, exit 2, zero installs attempted
#
# Run: ./code-install-extensions.test.sh
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/code-install-extensions.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }
ok() { echo "ok: $1"; }

# The expected list, read from the real script itself: if the list
# changes, this test follows it instead of pinning a stale copy.
EXPECTED_LOG="$TMP_ROOT/expected.log"
grep '^code --install-extension ' "$SCRIPT" | sed 's/^code //' >"$EXPECTED_LOG"
EXPECTED_COUNT="$(wc -l <"$EXPECTED_LOG" | tr -d ' ')"
[ "$EXPECTED_COUNT" -gt 0 ] || { echo "no install lines found in $SCRIPT" >&2; exit 1; }
FIRST_EXT="$(head -1 "$EXPECTED_LOG" | awk '{print $2}')"
LAST_EXT="$(tail -1 "$EXPECTED_LOG" | awk '{print $2}')"

# Stub `code`: logs "$*" one line per call, fails (exit 1) for any
# extension id listed in the space-separated $FAIL_EXTENSIONS.
# The shebang must name a real interpreter: /usr/bin/env does not
# exist in the nix build sandbox. Resolved bash works in both the
# sandbox and direct runs.
STUB_DIR="$TMP_ROOT/stub"
mkdir -p "$STUB_DIR"
{
  printf '#!%s\n' "$(command -v bash)"
  cat <<'STUB_EOF'
printf '%s\n' "$*" >>"$CODE_STUB_LOG"
for _ext in ${FAIL_EXTENSIONS:-}; do
  if [ "${2:-}" = "$_ext" ]; then
    echo "stub code: install failed for ${2:-}" >&2
    exit 1
  fi
done
exit 0
STUB_EOF
} >"$STUB_DIR/code"
chmod +x "$STUB_DIR/code"

run() { # run <fail-extensions> — sets OUT, ERR, STATUS, CALL_LOG
  CALL_LOG="$TMP_ROOT/calls.log"
  : >"$CALL_LOG"
  OUT="$(FAIL_EXTENSIONS="$1" CODE_STUB_LOG="$CALL_LOG" PATH="$STUB_DIR:$PATH" bash "$SCRIPT" 2>"$TMP_ROOT/err")"; STATUS=$?
  ERR="$(cat "$TMP_ROOT/err")"
}

run_args() { # run_args <arg> -- sets OUT, ERR, STATUS, CALL_LOG
  CALL_LOG="$TMP_ROOT/calls.log"
  : >"$CALL_LOG"
  OUT="$(FAIL_EXTENSIONS="" CODE_STUB_LOG="$CALL_LOG" PATH="$STUB_DIR:$PATH" bash "$SCRIPT" "$1" 2>"$TMP_ROOT/err")"; STATUS=$?
  ERR="$(cat "$TMP_ROOT/err")"
}

check_no_attempts() { # check_no_attempts <label>
  if [ ! -s "$CALL_LOG" ]; then
    ok "$1: zero installs attempted"
  else
    fail "$1: installs were attempted for a non-install invocation"
  fi
}

check_all_attempted() { # check_all_attempted <label>
  if [ "$(cat "$CALL_LOG")" = "$(cat "$EXPECTED_LOG")" ]; then
    ok "$1: all $EXPECTED_COUNT extensions attempted in order with --force"
  else
    fail "$1: attempted calls differ from the script's list/order"
  fi
}

# ─── All succeed ─────────────────────────────────────────────────────
run ""
[ "$STATUS" -eq 0 ] || fail "all-succeed exited $STATUS (want 0, stderr: $ERR)"
case "$OUT" in
  *"all $EXPECTED_COUNT extensions installed successfully"*) ok "all-succeed: success summary names the count" ;;
  *) fail "all-succeed: success summary missing (got: $OUT)" ;;
esac
check_all_attempted "all-succeed"

# ─── Middle-of-list failure ──────────────────────────────────────────
# vscode.go is line 22 of the list — well before the end, so the old
# script exited 0 for exactly this shape.
run "vscode.go"
[ "$STATUS" -ne 0 ] || fail "middle failure (vscode.go) exited 0 (want non-zero)"
case "$ERR" in
  *"1 of $EXPECTED_COUNT extension(s) failed"*"failed: vscode.go"*) ok "middle failure: summary counts 1 and names vscode.go" ;;
  *) fail "middle failure: summary did not count/name vscode.go (got: $ERR)" ;;
esac
check_all_attempted "middle failure"

# ─── Last-line failure ───────────────────────────────────────────────
run "$LAST_EXT"
[ "$STATUS" -ne 0 ] || fail "last-line failure ($LAST_EXT) exited 0 (want non-zero)"
case "$ERR" in
  *"failed: $LAST_EXT"*) ok "last-line failure: summary names $LAST_EXT" ;;
  *) fail "last-line failure: summary did not name $LAST_EXT (got: $ERR)" ;;
esac
check_all_attempted "last-line failure"

# ─── Two failures are both named and counted ─────────────────────────
run "$FIRST_EXT vscode.go"
[ "$STATUS" -ne 0 ] || fail "two failures exited 0 (want non-zero)"
case "$ERR" in
  *"2 of $EXPECTED_COUNT extension(s) failed"*"failed: $FIRST_EXT"*"failed: vscode.go"*) ok "two failures: summary counts 2 and names both" ;;
  *) fail "two failures: summary did not count/name both (got: $ERR)" ;;
esac
check_all_attempted "two failures"

# ─── --help prints usage and installs nothing (#1319) ────────────────
run_args --help
[ "$STATUS" -eq 0 ] || fail "--help exited $STATUS (want 0, stderr: $ERR)"
case "$OUT" in
  *"Usage: code-install-extensions.sh"*) ok "--help: usage printed" ;;
  *) fail "--help: usage missing from stdout (got: $OUT)" ;;
esac
check_no_attempts "--help"

# ─── -h prints usage and installs nothing (#1319) ────────────────────
run_args -h
[ "$STATUS" -eq 0 ] || fail "-h exited $STATUS (want 0, stderr: $ERR)"
case "$OUT" in
  *"Usage: code-install-extensions.sh"*) ok "-h: usage printed" ;;
  *) fail "-h: usage missing from stdout (got: $OUT)" ;;
esac
check_no_attempts "-h"

# ─── Unknown argument is named, exits 2, installs nothing (#1319) ────
run_args --bogus
[ "$STATUS" -eq 2 ] || fail "unknown argument exited $STATUS (want 2, stderr: $ERR)"
case "$ERR" in
  *"unknown argument: --bogus"*"Usage: code-install-extensions.sh"*) ok "unknown argument: named on stderr with usage" ;;
  *) fail "unknown argument: not named with usage on stderr (got: $ERR)" ;;
esac
check_no_attempts "unknown argument"

if [ "$failures" -gt 0 ]; then
  echo "$failures code-install-extensions check(s) failed" >&2
  exit 1
fi
echo "all code-install-extensions checks passed"
