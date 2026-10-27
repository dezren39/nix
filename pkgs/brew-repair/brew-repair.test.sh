#!/usr/bin/env bash
# Tests for brew-repair.sh's -s/--skip argument parsing (#927):
#   -s/--skip guarded only a missing value ($# -lt 2, from #897), so a
#   following option token was consumed as the cask name. Worst case
#   '-s --dry-run' silently discarded the dry-run safety flag and ran
#   real reinstalls, exit 0. An option-like value (leading '-') is now
#   a parse error: option named, usage on stderr, exit 2, no brew calls.
# Scenarios G–J cover #926: a failed or unparseable `brew info` scan used
# to exit 0 printing "All cask apps are healthy!" after checking zero
# casks; now it names the failing step on stderr and exits 1, and a cask
# absent from a successful scan counts as unchecked, never "healthy".
# Scenarios K–L cover #949: a failed `brew list` used to exit 0 printing
# "No casks installed." (its status and stderr were lost in a process
# substitution); now it names the step on stderr and exits 1, while a
# genuinely empty successful list keeps the exit-0 empty path.
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
  list)
    case "${BREW_STUB_MODE:-broken}" in
      list-fail) echo "Error: brew list blew up" >&2; exit 1 ;;
      list-empty) exit 0 ;;
      *) printf '%s\n' alpha beta ;;
    esac ;;
  info)
    case "${BREW_STUB_MODE:-broken}" in
      info-fail) echo "Error: brew info blew up" >&2; exit 1 ;;
      info-garbage) printf '%s\n' 'this is not json' ;;
      partial) printf '%s\n' '{"casks":[{"token":"alpha","artifacts":[{"app":["Alpha.app"]}]}]}' ;;
      *) printf '%s\n' '{"casks":[{"token":"alpha","artifacts":[{"app":["Alpha.app"]}]},{"token":"beta","artifacts":[{"app":["Beta.app"]}]}]}' ;;
    esac ;;
  reinstall) exit 0 ;;
esac
EOS
chmod +x "$STUB_BIN/brew"

run_brew_repair() {
  : > "$LOG"
  HOME="$FAKE_HOME" PATH="$STUB_BIN:$PATH" BREW_STUB_LOG="$LOG" \
    BREW_STUB_MODE="${BREW_STUB_MODE:-broken}" \
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

# ─── Scenario G (#926): brew info failure is a loud scan failure ────────────
start=$failures
out_g="$(BREW_STUB_MODE=info-fail run_brew_repair)"
rc_g=$?
[[ $rc_g -eq 1 ]] || fail "G: expected exit 1, got $rc_g"
grep -q "'brew info' failed" "$TMP_ROOT/stderr" || fail "G: failing step not named on stderr"
grep -q "All cask apps are healthy" <<<"$out_g" && fail "G: healthy verdict printed for a failed scan"
[[ $(reinstall_count) -eq 0 ]] || fail "G: reinstall called after a failed scan"
[[ $failures -eq $start ]] && ok "G: brew info failure exits 1, names the step, no healthy verdict"

# ─── Scenario H (#926): non-JSON brew info output is a loud scan failure ────
start=$failures
out_h="$(BREW_STUB_MODE=info-garbage run_brew_repair)"
rc_h=$?
[[ $rc_h -eq 1 ]] || fail "H: expected exit 1, got $rc_h"
grep -q "could not parse 'brew info' output" "$TMP_ROOT/stderr" || fail "H: parse failure not named on stderr"
grep -q "All cask apps are healthy" <<<"$out_h" && fail "H: healthy verdict printed for an unparseable scan"
[[ $failures -eq $start ]] && ok "H: non-JSON brew info exits 1, names the parse step, no healthy verdict"

# ─── Scenario I (#926 control): all apps present is still healthy, exit 0 ───
mkdir -p "$FAKE_HOME/Applications/Alpha.app" "$FAKE_HOME/Applications/Beta.app"
start=$failures
out_i="$(run_brew_repair)"
rc_i=$?
[[ $rc_i -eq 0 ]] || fail "I: expected exit 0, got $rc_i"
grep -q "All cask apps are healthy!" <<<"$out_i" || fail "I: healthy verdict missing for a good scan"
grep -q "2 healthy" <<<"$out_i" || fail "I: summary does not count both casks healthy"
[[ $failures -eq $start ]] && ok "I: all-healthy control exits 0 with the healthy verdict"

# ─── Scenario J (#926): cask absent from a successful scan is unchecked ─────
start=$failures
out_j="$(BREW_STUB_MODE=partial run_brew_repair)"
rc_j=$?
[[ $rc_j -eq 1 ]] || fail "J: expected exit 1, got $rc_j"
grep -q "beta (installed but unknown to brew" <<<"$out_j" || fail "J: beta not reported unknown"
grep -q "1 unchecked" <<<"$out_j" || fail "J: summary does not count beta as unchecked"
grep -q "All cask apps are healthy" <<<"$out_j" && fail "J: healthy verdict printed with an unchecked cask"
[[ $failures -eq $start ]] && ok "J: unknown cask counts as unchecked, exit 1, no healthy verdict"

# ─── Scenario K (#949): brew list failure is a loud failure ─────────────────
start=$failures
out_k="$(BREW_STUB_MODE=list-fail run_brew_repair)"
rc_k=$?
[[ $rc_k -eq 1 ]] || fail "K: expected exit 1, got $rc_k"
grep -q "'brew list' failed" "$TMP_ROOT/stderr" || fail "K: failing step not named on stderr"
grep -q "brew list blew up" "$TMP_ROOT/stderr" || fail "K: brew's own stderr not surfaced"
grep -q "No casks installed" <<<"$out_k" && fail "K: 'No casks installed.' printed for a failed brew list"
[[ $(reinstall_count) -eq 0 ]] || fail "K: reinstall called after a failed brew list"
[[ $failures -eq $start ]] && ok "K: brew list failure exits 1, names the step, surfaces stderr, no empty verdict"

# ─── Scenario L (#949 control): empty successful brew list stays exit 0 ─────
start=$failures
out_l="$(BREW_STUB_MODE=list-empty run_brew_repair)"
rc_l=$?
[[ $rc_l -eq 0 ]] || fail "L: expected exit 0, got $rc_l"
grep -q "No casks installed." <<<"$out_l" || fail "L: empty-list message missing for a successful empty brew list"
[[ $failures -eq $start ]] && ok "L: successful empty brew list exits 0 with 'No casks installed.'"

if [[ $failures -gt 0 ]]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all brew-repair tests passed"
