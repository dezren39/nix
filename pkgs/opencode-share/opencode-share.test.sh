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

if [[ $failures -gt 0 ]]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all opencode-share tests passed"
