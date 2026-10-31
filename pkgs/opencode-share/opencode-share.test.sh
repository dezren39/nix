#!/usr/bin/env bash
# Tests for opencode-share.sh's multi-target dispatch (#907):
#   ((ERRORS++)) post-increment evaluates to 0 when ERRORS=0, so as the
#   command after the final `||` it returned status 1 and `set -e`
#   aborted the whole run at the first failing target — later targets
#   were never processed and the "N target(s) had errors" summary never
#   printed. The dispatch now uses ERRORS=$((ERRORS + 1)).
# This drives the REAL opencode-share.sh in --dry-run mode (no bindfs /
# macFUSE needed) against temp dirs.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SELF_DIR/opencode-share.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}
ok() { echo "ok: $1"; }

SRC="$TMP_ROOT/src"; mkdir -p "$SRC"
GOOD1="$TMP_ROOT/good1"; mkdir -p "$GOOD1"
GOOD2="$TMP_ROOT/good2"; mkdir -p "$GOOD2"
MISSING="$TMP_ROOT/missing"
MISSING2="$TMP_ROOT/missing2"

# ─── Scenario A (#907): [good, missing, good] processes all three ───────────
start=$failures
out_a="$(bash "$SCRIPT" --dry-run --source "$SRC" "$GOOD1" "$MISSING" "$GOOD2" 2>&1)"
rc_a=$?
[[ $rc_a -eq 1 ]] || fail "A: expected exit 1, got $rc_a"
grep -q "Mounting: $GOOD1" <<<"$out_a" || fail "A: good1 not processed"
grep -q "Mounting: $MISSING" <<<"$out_a" || fail "A: missing target not processed"
grep -q "Mounting: $GOOD2" <<<"$out_a" || fail "A: good2 after the failing target was never processed (regression)"
grep -q "1 target(s) had errors" <<<"$out_a" || fail "A: error summary never printed"
[[ $failures -eq $start ]] && ok "A: [good, missing, good] all processed, summary prints, exit 1"

# ─── Scenario B: all-good run still exits 0 with the Done line ──────────────
start=$failures
out_b="$(bash "$SCRIPT" --dry-run --source "$SRC" "$GOOD1" "$GOOD2" 2>&1)"
rc_b=$?
[[ $rc_b -eq 0 ]] || fail "B: expected exit 0, got $rc_b"
grep -q "Done (mount 2 target(s))" <<<"$out_b" || fail "B: Done line missing"
[[ $failures -eq $start ]] && ok "B: all-good exits 0"

# ─── Scenario C: all-fail run reports the full count ────────────────────────
start=$failures
out_c="$(bash "$SCRIPT" --dry-run --source "$SRC" "$MISSING" "$MISSING2" 2>&1)"
rc_c=$?
[[ $rc_c -eq 1 ]] || fail "C: expected exit 1, got $rc_c"
grep -q "Mounting: $MISSING2" <<<"$out_c" || fail "C: second failing target never processed"
grep -q "2 target(s) had errors" <<<"$out_c" || fail "C: summary did not report both failures"
[[ $failures -eq $start ]] && ok "C: all-fail reports the full count, exit 1"

# ─── Scenario D: no ((x++)) post-increment traps remain in the script ───────
start=$failures
if grep -q '((.*++))' "$SCRIPT"; then
  fail "D: a ((x++)) post-increment remains in opencode-share.sh"
else
  [[ $failures -eq $start ]] && ok "D: no ((x++)) post-increment traps remain"
fi

# ─── Scenario E (#908): --source with no value errors cleanly ───────────────
start=$failures
out_e="$(bash "$SCRIPT" --source 2>&1)"
rc_e=$?
[[ $rc_e -eq 1 ]] || fail "E: expected exit 1, got $rc_e"
grep -q -- "--source requires a directory argument" <<<"$out_e" || fail "E: missing-value error not printed"
grep -q "Usage:" <<<"$out_e" || fail "E: usage not printed"
if grep -q "unbound variable" <<<"$out_e"; then
  fail "E: still crashes on unbound variable (regression)"
fi
[[ $failures -eq $start ]] && ok "E: --source with no value prints error + usage, exit 1"

# ─── Scenario F (#930): --source with a flag-shaped value errors ────────────
# --source --dry-run used to swallow --dry-run as the source directory, so
# the "preview" ran the real mount path (check_deps and beyond).
start=$failures
out_f="$(bash "$SCRIPT" --source --dry-run "$GOOD1" 2>&1)"
rc_f=$?
[[ $rc_f -eq 1 ]] || fail "F: expected exit 1, got $rc_f"
grep -q -- "--source requires a directory argument" <<<"$out_f" || fail "F: flag-shaped value not rejected with the missing-value error"
if grep -q "Missing dependencies" <<<"$out_f"; then
  fail "F: entered the real-mount check_deps path (regression)"
fi
if grep -q "Mounting:" <<<"$out_f"; then
  fail "F: a mount was attempted (regression)"
fi
[[ $failures -eq $start ]] && ok "F: --source --dry-run errors at parse time, no mount attempted"

# ─── Scenario G (#1106): conflicting action flags error at parse time ──────
# --unmount --status used to run the status report and exit 0 (the unmount
# was silently ignored); --status --unmount proceeded down the unmount path.
# Both orders must exit non-zero naming the conflict, before any work.
# Repeating the SAME action flag is accepted idempotently (pinned here).
start=$failures
out_g1="$(bash "$SCRIPT" --unmount --status "$GOOD1" 2>&1)"
rc_g1=$?
[[ $rc_g1 -ne 0 ]] || fail "G: --unmount --status expected non-zero exit, got $rc_g1"
grep -q "conflicting action flags" <<<"$out_g1" || fail "G: --unmount --status did not name the conflict"
if grep -q "Done (status" <<<"$out_g1"; then
  fail "G: --unmount --status ran the status report anyway (regression)"
fi
out_g2="$(bash "$SCRIPT" --status --unmount "$GOOD1" 2>&1)"
rc_g2=$?
[[ $rc_g2 -ne 0 ]] || fail "G: --status --unmount expected non-zero exit, got $rc_g2"
grep -q "conflicting action flags" <<<"$out_g2" || fail "G: --status --unmount did not name the conflict"
if grep -q "Missing dependencies" <<<"$out_g2"; then
  fail "G: --status --unmount reached the unmount dependency check (regression)"
fi
out_g3="$(bash "$SCRIPT" -u -s "$GOOD1" 2>&1)"
rc_g3=$?
[[ $rc_g3 -ne 0 ]] || fail "G: -u -s expected non-zero exit, got $rc_g3"
grep -q "conflicting action flags" <<<"$out_g3" || fail "G: -u -s did not name the conflict"
out_g4="$(bash "$SCRIPT" --status --status "$GOOD1" 2>&1)"
rc_g4=$?
[[ $rc_g4 -eq 0 ]] || fail "G: repeated same action --status --status expected exit 0 (idempotent), got $rc_g4"
grep -q "Done (status 1 target(s))" <<<"$out_g4" || fail "G: repeated --status did not run the status report"
[[ $failures -eq $start ]] && ok "G: conflicting action flags exit non-zero naming the conflict; same action repeats idempotently"

# ─── Scenario H (#1106): repeated --source errors at parse time ────────────
# --source A --source B used to silently discard A (the #1103 shape).
start=$failures
out_h="$(bash "$SCRIPT" --source "$SRC" --source "$GOOD1" --dry-run "$GOOD1" 2>&1)"
rc_h=$?
[[ $rc_h -ne 0 ]] || fail "H: repeated --source expected non-zero exit, got $rc_h"
grep -q -- "--source may be given only once" <<<"$out_h" || fail "H: repeated --source did not name the repetition"
if grep -q "Mounting:" <<<"$out_h"; then
  fail "H: a mount was attempted after repeated --source (regression)"
fi
[[ $failures -eq $start ]] && ok "H: repeated --source exits non-zero naming the repetition, no mount attempted"

# ─── Scenario I (#1220): --source with an empty value errors ─────────────
# --source "" used to be accepted (blank Source, late failure / a status
# run that exited 0). It must hit the same parse error as a missing value.
start=$failures
out_i="$(bash "$SCRIPT" --source '' --status "$GOOD1" 2>&1)"
rc_i=$?
[[ $rc_i -eq 1 ]] || fail "I: expected exit 1, got $rc_i"
grep -q -- "--source requires a directory argument" <<<"$out_i" || fail "I: empty-value error not printed"
grep -q "Usage:" <<<"$out_i" || fail "I: usage not printed"
if grep -q "Done (status" <<<"$out_i"; then
  fail "I: status report ran with an empty --source (regression)"
fi
[[ $failures -eq $start ]] && ok "I: --source '' prints error + usage, exit 1, no status work"

if [[ $failures -gt 0 ]]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all opencode-share tests passed"
