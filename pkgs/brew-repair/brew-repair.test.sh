#!/usr/bin/env bash
# Tests for brew-repair.sh's -s/--skip argument parsing (#927):
#   -s/--skip guarded only a missing value ($# -lt 2, from #897), so a
#   following option token was consumed as the cask name. Worst case
#   '-s --dry-run' silently discarded the dry-run safety flag and ran
#   real reinstalls, exit 0. An option-like value (leading '-') is now
#   a parse error: option named, usage on stderr, exit 2, no brew calls.
# This drives the REAL brew-repair.sh against a call-logging stub brew
# with two broken casks (alpha, beta; both .apps missing).
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/brew-repair.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}
ok() { echo "ok: $1"; }

STUB_BIN="$TMP_ROOT/bin"
FAKE_HOME="$TMP_ROOT/home"
LOG="$TMP_ROOT/brew.log"
mkdir -p "$STUB_BIN" "$FAKE_HOME/Applications"

cat > "$STUB_BIN/brew" <<'EOS'
#!/usr/bin/env bash
echo "brew $*" >> "$BREW_STUB_LOG"
case "$1" in
  list) printf '%s\n' alpha beta ;;
  info) printf '%s\n' '{"casks":[{"token":"alpha","artifacts":[{"app":["Alpha.app"]}]},{"token":"beta","artifacts":[{"app":["Beta.app"]}]}]}' ;;
  reinstall) exit 0 ;;
esac
EOS
chmod +x "$STUB_BIN/brew"

run_brew_repair() {
  : > "$LOG"
  HOME="$FAKE_HOME" PATH="$STUB_BIN:$PATH" BREW_STUB_LOG="$LOG" \
    bash "$SCRIPT" "$@" 2>"$TMP_ROOT/stderr"
}

reinstall_count() { grep -c 'reinstall' "$LOG" || true; }

# ─── Scenario A (#927): -s --dry-run is a parse error, no brew calls ────────
start=$failures
run_brew_repair -s --dry-run >/dev/null
rc_a=$?
[[ $rc_a -eq 2 ]] || fail "A: expected exit 2, got $rc_a"
grep -q -- "-s requires a cask name" "$TMP_ROOT/stderr" || fail "A: option not named on stderr"
grep -q "Usage:" "$TMP_ROOT/stderr" || fail "A: usage not on stderr"
[[ $(reinstall_count) -eq 0 ]] || fail "A: reinstall called for -s --dry-run (regression)"
[[ ! -s "$LOG" ]] || fail "A: brew was called at all (scan side effect) for -s --dry-run"
[[ $failures -eq $start ]] && ok "A: -s --dry-run exits 2, usage on stderr, zero brew calls"

# ─── Scenario B (#927): -s -n is a parse error, no brew calls ───────────────
start=$failures
run_brew_repair -s -n >/dev/null
rc_b=$?
[[ $rc_b -eq 2 ]] || fail "B: expected exit 2, got $rc_b"
grep -q -- "-s requires a cask name" "$TMP_ROOT/stderr" || fail "B: option not named on stderr"
[[ $(reinstall_count) -eq 0 ]] || fail "B: reinstall called for -s -n (regression)"
[[ ! -s "$LOG" ]] || fail "B: brew was called at all for -s -n"
[[ $failures -eq $start ]] && ok "B: -s -n exits 2, zero brew calls"

# ─── Scenario C: --skip with an option-like value is also a parse error ─────
start=$failures
run_brew_repair --skip --help >/dev/null
rc_c=$?
[[ $rc_c -eq 2 ]] || fail "C: expected exit 2, got $rc_c"
grep -q -- "--skip requires a cask name" "$TMP_ROOT/stderr" || fail "C: --skip not named on stderr"
[[ ! -s "$LOG" ]] || fail "C: brew was called at all for --skip --help"
[[ $failures -eq $start ]] && ok "C: --skip --help exits 2, zero brew calls"

# ─── Scenario D (control): -s alpha -n still means dry run ──────────────────
start=$failures
out_d="$(run_brew_repair -s alpha -n)"
rc_d=$?
[[ $rc_d -eq 0 ]] || fail "D: expected exit 0, got $rc_d"
grep -q "alpha (skipped)" <<<"$out_d" || fail "D: alpha not skipped"
grep -q "Dry run" <<<"$out_d" || fail "D: -n after a real value no longer means dry run"
grep -q "brew reinstall --cask beta" <<<"$out_d" || fail "D: beta not listed as would-reinstall"
[[ $(reinstall_count) -eq 0 ]] || fail "D: dry run reinstalled something"
[[ $failures -eq $start ]] && ok "D: -s alpha -n skips alpha, dry-runs beta, no reinstall"

# ─── Scenario E (control): valid values, repeated -s, --skip form ───────────
start=$failures
out_e="$(run_brew_repair --skip alpha -s beta)"
rc_e=$?
[[ $rc_e -eq 0 ]] || fail "E: expected exit 0, got $rc_e"
grep -q "alpha (skipped)" <<<"$out_e" || fail "E: alpha not skipped via --skip"
grep -q "beta (skipped)" <<<"$out_e" || fail "E: beta not skipped via repeated -s"
[[ $(reinstall_count) -eq 0 ]] || fail "E: reinstall called though all broken casks skipped"
[[ $failures -eq $start ]] && ok "E: --skip alpha -s beta skips both, no reinstall"

# ─── Scenario F (control): trailing -s with no value still exits 2 ──────────
start=$failures
run_brew_repair -s >/dev/null
rc_f=$?
[[ $rc_f -eq 2 ]] || fail "F: expected exit 2, got $rc_f"
grep -q -- "-s requires a cask name" "$TMP_ROOT/stderr" || fail "F: missing-value error not printed"
[[ $failures -eq $start ]] && ok "F: trailing -s exits 2 (pre-existing #897 guard intact)"

if [[ $failures -gt 0 ]]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all brew-repair tests passed"
