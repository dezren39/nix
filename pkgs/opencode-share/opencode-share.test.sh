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

# ─── Scenario J (#1223): empty target argument errors ─────────────
# An empty target ("") used to resolve to the caller's cwd (cd "" succeeds
# in the resolution subshell), so --status '' reported on the cwd at exit 0
# and the default mount action would have mounted it. Both spellings --
# plain and after -- -- must be rejected at parse time.
start=$failures
out_j="$(bash "$SCRIPT" --status '' 2>&1)"
rc_j=$?
[[ $rc_j -eq 1 ]] || fail "J: expected exit 1 for empty target, got $rc_j"
grep -q -- "empty target argument" <<<"$out_j" || fail "J: empty-target error not printed"
grep -q "Usage:" <<<"$out_j" || fail "J: usage not printed"
if grep -q "Done (status" <<<"$out_j"; then
  fail "J: status report ran for an empty target (cwd regression)"
fi
out_j2="$(bash "$SCRIPT" --status -- '' 2>&1)"
rc_j2=$?
[[ $rc_j2 -eq 1 ]] || fail "J: expected exit 1 for empty target after --, got $rc_j2"
grep -q -- "empty target argument" <<<"$out_j2" || fail "J: empty-target error not printed after --"
grep -q "Usage:" <<<"$out_j2" || fail "J: usage not printed after --"
if grep -q "Done (status" <<<"$out_j2"; then
  fail "J: status report ran for an empty target after -- (cwd regression)"
fi
[[ $failures -eq $start ]] && ok "J: empty target (plain and after --) prints error + usage, exit 1, no status work"

# ─── Scenario K (#1323): a failing umount must not report success ──────────
# do_unmount used to run both umount calls unchecked and end with a
# successful log, so a failed unmount printed "Unmounted", counted no
# error, and exited 0 with both mounts still up (verified with stub
# mount/umount on main). This scenario drives the REAL (non-dry-run)
# --unmount path with stateful stubs: stub `mount` reports the mounts
# listed in $MOUNT_STATE, stub `umount` removes a mountpoint from that
# state only when it succeeds, stub `rmdir` only logs its calls.
# check_deps normally needs a macFUSE marker at an absolute path. The
# test-only marker override keeps this scenario runnable in the Nix sandbox;
# real-marker and user+mount-namespace paths remain usable on developer hosts.
start=$failures
STUB_BIN="$TMP_ROOT/stub-bin"; mkdir -p "$STUB_BIN"
UMOUNT_TARGET="$TMP_ROOT/umount-target"
mkdir -p "$UMOUNT_TARGET/.opencode" "$UMOUNT_TARGET/plans"
touch "$UMOUNT_TARGET/plans/keep.md"
export MOUNT_STATE="$TMP_ROOT/mount-state"
export UMOUNT_LOG="$TMP_ROOT/umount-calls.log"
export RMDIR_LOG="$TMP_ROOT/rmdir-calls.log"
BASH_BIN="$(command -v bash)"
write_bash_stub() {
  local path="$1"
  printf '#!%s\n' "$BASH_BIN" > "$path"
  cat >> "$path"
}
write_bash_stub "$STUB_BIN/mount" <<'STUB'
mount_output=""
while IFS= read -r mp; do
  [[ -n "$mp" ]] && mount_output+="bindfs on $mp (fuse, local)"$'\n'
done < "$MOUNT_STATE"
printf '%s' "$mount_output"
STUB
write_bash_stub "$STUB_BIN/umount" <<'STUB'
echo "umount $*" >> "$UMOUNT_LOG"
if [[ "${UMOUNT_RC:-0}" != "0" ]]; then
  exit "$UMOUNT_RC"
fi
grep -v -F -x -- "$1" "$MOUNT_STATE" > "$MOUNT_STATE.tmp" || true
mv "$MOUNT_STATE.tmp" "$MOUNT_STATE"
exit 0
STUB
write_bash_stub "$STUB_BIN/bindfs" <<'STUB'
exit 0
STUB
write_bash_stub "$STUB_BIN/rmdir" <<'STUB'
echo "rmdir $*" >> "$RMDIR_LOG"
exit 0
STUB
chmod +x "$STUB_BIN/mount" "$STUB_BIN/umount" "$STUB_BIN/bindfs" "$STUB_BIN/rmdir"
write_bash_stub "$TMP_ROOT/fake-macfuse.sh" <<'STUB'
set -e
mount -t overlay overlay -o lowerdir=/usr,upperdir="$1",workdir="$2" /usr
mkdir -p /usr/local/lib
touch /usr/local/lib/libfuse.dylib
shift 2
exec "$@"
STUB
chmod +x "$TMP_ROOT/fake-macfuse.sh"
k_mode=""
if [[ -d "/Library/Filesystems/macfuse.fs" || -f "/usr/local/lib/libfuse.dylib" || -f "/opt/homebrew/lib/libfuse.dylib" ]]; then
  k_mode="direct"
else
  FAKE_MACFUSE_MARKER="$TMP_ROOT/fake-macfuse-marker"
  : > "$FAKE_MACFUSE_MARKER"
  mkdir -p "$TMP_ROOT/ov-probe-up" "$TMP_ROOT/ov-probe-work"
  if unshare -rm bash "$TMP_ROOT/fake-macfuse.sh" "$TMP_ROOT/ov-probe-up" "$TMP_ROOT/ov-probe-work" true 2>/dev/null; then
    k_mode="namespace"
  else
    k_mode="injected"
  fi
fi
run_k() {
  export UMOUNT_RC="$1"
  printf '%s\n%s\n' "$UMOUNT_TARGET/.opencode" "$UMOUNT_TARGET/.opencode/plans" > "$MOUNT_STATE"
  : > "$UMOUNT_LOG"; : > "$RMDIR_LOG"
  mkdir -p "$UMOUNT_TARGET/.opencode"
  if [[ "$k_mode" == "direct" ]]; then
    env -u OPENCODE_SHARE_TEST_MACFUSE_MARKER PATH="$STUB_BIN:$PATH" \
      bash "$SCRIPT" --unmount --source "$SRC" "$UMOUNT_TARGET" 2>&1
  elif [[ "$k_mode" == "namespace" ]]; then
    local up="$TMP_ROOT/ov-up-$1-$RANDOM" work="$TMP_ROOT/ov-work-$1-$RANDOM"
    mkdir -p "$up" "$work"
    unshare -rm bash "$TMP_ROOT/fake-macfuse.sh" "$up" "$work" \
      env -u OPENCODE_SHARE_TEST_MACFUSE_MARKER PATH="$STUB_BIN:$PATH" \
      bash "$SCRIPT" --unmount --source "$SRC" "$UMOUNT_TARGET" 2>&1
  else
    OPENCODE_SHARE_TEST_MACFUSE_MARKER="$FAKE_MACFUSE_MARKER" \
      PATH="$STUB_BIN:$PATH" bash "$SCRIPT" --unmount --source "$SRC" "$UMOUNT_TARGET" 2>&1
  fi
}
if [[ "$k_mode" != "direct" ]]; then
  out_k_negative="$(env -u OPENCODE_SHARE_TEST_MACFUSE_MARKER PATH="$STUB_BIN:$PATH" \
    bash "$SCRIPT" --unmount --source "$SRC" "$UMOUNT_TARGET" 2>&1)"
  rc_k_negative=$?
  [[ $rc_k_negative -ne 0 ]] || fail "K: without a real or injected marker, check_deps expected failure"
  grep -q "macFUSE — install via:" <<<"$out_k_negative" || fail "K: negative control did not name missing macFUSE"
fi

out_k="$(run_k 1)"; rc_k=$?
[[ $rc_k -ne 0 ]] || fail "K: failing umount expected non-zero exit, got $rc_k"
grep -q "Failed to unmount plans overlay: $UMOUNT_TARGET/.opencode/plans" <<<"$out_k" || fail "K: plans-overlay failure not named on stderr"
grep -q "Failed to unmount .opencode: $UMOUNT_TARGET/.opencode" <<<"$out_k" || fail "K: outer-mount failure not named on stderr; output: $out_k"
grep -q "1 target(s) had errors" <<<"$out_k" || fail "K: target not counted as an error in the summary"
if grep -q "Unmounted .opencode from" <<<"$out_k"; then
  fail "K: still-mounted target reported as Unmounted (regression)"
fi
if grep -q "Done (unmount" <<<"$out_k"; then
  fail "K: success Done line printed for a failed unmount (regression)"
fi
grep -q "umount $UMOUNT_TARGET/.opencode/plans" "$UMOUNT_LOG" || fail "K: plans umount was never attempted"
grep -q "umount $UMOUNT_TARGET/.opencode" "$UMOUNT_LOG" || fail "K: outer umount was never attempted"
if [[ -s "$RMDIR_LOG" ]]; then
  fail "K: rmdir ran against a still-mounted directory (regression)"
fi
grep -q "Project plans preserved" <<<"$out_k" || fail "K: plans-preserved line missing on the failure path"
out_k2="$(run_k 0)"; rc_k2=$?
[[ $rc_k2 -eq 0 ]] || fail "K: successful umount control expected exit 0, got $rc_k2"
grep -q "Unmounted .opencode from $UMOUNT_TARGET" <<<"$out_k2" || fail "K: successful control did not report Unmounted"
grep -q "Done (unmount 1 target(s))" <<<"$out_k2" || fail "K: successful control missing the Done line"
[[ -s "$RMDIR_LOG" ]] || fail "K: successful control did not rmdir the empty, unmounted mount point"
[[ $failures -eq $start ]] && ok "K: failing umount exits non-zero, names both failures, counts the target, no Unmounted/rmdir; successful control unchanged"

# ─── Scenario L (#1455): is_mounted must not lose an early match to SIGPIPE ─
# is_mounted was `mount | grep -qF ...` under `set -euo pipefail`: grep -q
# exits on the first match, mount takes SIGPIPE while still writing, and
# pipefail reports the pipeline as failed -- a mounted share read as not
# mounted whenever the match came early in a large mount table (the suite
# failed scenario K in 8/20 runs this way). This scenario drives the REAL
# --status path (which skips check_deps) with a stub `mount` that prints
# the target lines FIRST and then 5000 filler lines (far more than the
# pipe buffer), plus a negative control with the same large output but
# no target lines.
start=$failures
STUB_BIN_L="$TMP_ROOT/stub-bin-l"; mkdir -p "$STUB_BIN_L"
write_bash_stub "$STUB_BIN_L/mount" <<'STUB'
if [[ -n "${MOUNT_BIG_TARGET:-}" ]]; then
  printf 'bindfs on %s (fuse, local)\n' "$MOUNT_BIG_TARGET"
  printf 'bindfs on %s (fuse, local)\n' "$MOUNT_BIG_TARGET/plans"
fi
i=0
while [[ $i -lt 5000 ]]; do
  printf 'fillerfs on /nonexistent/filler-%s (fuse, local)\n' "$i"
  i=$((i + 1))
done
STUB
chmod +x "$STUB_BIN_L/mount"
run_l() {
  MOUNT_BIG_TARGET="$1" PATH="$STUB_BIN_L:$PATH" \
    bash "$SCRIPT" --status --source "$SRC" "$UMOUNT_TARGET" 2>&1
}
out_l="$(run_l "$UMOUNT_TARGET/.opencode")"; rc_l=$?
[[ $rc_l -eq 0 ]] || fail "L: status with early-match large mount output expected exit 0, got $rc_l"
grep -q ".opencode: mounted (source: bindfs)" <<<"$out_l" || fail "L: early match in large mount output reported as not mounted (SIGPIPE/pipefail regression); output: $out_l"
grep -q "plans/:   mounted (source: bindfs)" <<<"$out_l" || fail "L: plans early match in large mount output reported as not mounted; output: $out_l"
out_l2="$(run_l "")"; rc_l2=$?
[[ $rc_l2 -eq 0 ]] || fail "L: negative control expected exit 0, got $rc_l2"
grep -q ".opencode: directory exists but not mounted" <<<"$out_l2" || fail "L: negative control (no target lines) did not report not mounted; output: $out_l2"
if grep -q ".opencode: mounted" <<<"$out_l2"; then
  fail "L: negative control reported a genuinely unmounted target as mounted (regression)"
fi
grep -q "^set -euo pipefail$" "$SCRIPT" || fail "L: script no longer runs under set -euo pipefail (fix must not disable pipefail)"
[[ $failures -eq $start ]] && ok "L: early match in large mount output reports mounted for .opencode and plans; no-target control still reports not mounted; pipefail intact"

# ─── Scenario M (#1465): the color gate honours NO_COLOR ─────────────────
# The tty half of the gate predates this; NO_COLOR (https://no-color.org)
# is the half a terminal user sets explicitly. A pty-driven run is not
# portable across the test environments, so pin the gate's shape (the
# Scenario D precedent) and the plain captured output together.
start=$failures
grep -q 'NO_COLOR' "$SCRIPT" || fail "M: opencode-share.sh does not consult NO_COLOR"
grep -q '\-t 1' "$SCRIPT" || fail "M: opencode-share.sh lost its not-a-terminal color gate"
out_m="$(bash "$SCRIPT" --dry-run --source "$SRC" "$GOOD1" 2>&1)"
if [[ "$out_m" == *$'\033'* ]]; then
  fail "M: captured (non-terminal) output contains ANSI escape bytes"
fi
[[ $failures -eq $start ]] && ok "M: color gate consults NO_COLOR and the tty; captured output is plain"


# ─── Scenario N (#1559): --status source extraction is fixed-string ───────
# do_status used a plain regex grep to extract the mount source, so a
# sibling mount whose path differs from the target at a '.' position
# (projXect vs proj.ect) matched the target's pattern and its source was
# reported (newline-joined) as the target's source. This scenario drives
# the REAL --status path with a stub `mount` listing the sibling FIRST.
start=$failures
STUB_BIN_N="$TMP_ROOT/stub-bin-n"; mkdir -p "$STUB_BIN_N"
STATUS_TARGET="$TMP_ROOT/proj.ect"
mkdir -p "$STATUS_TARGET/.opencode"
write_bash_stub "$STUB_BIN_N/mount" <<'STUB'
printf 'wrongsrce on %s (fuse, local)\n' "$STATUS_SIBLING/.opencode"
printf 'rightsrc on %s (fuse, local)\n' "$STATUS_TARGET_DIR/.opencode"
printf 'rightsrc on %s (fuse, local)\n' "$STATUS_TARGET_DIR/.opencode/plans"
STUB
chmod +x "$STUB_BIN_N/mount"
out_n="$(STATUS_SIBLING="$TMP_ROOT/projXect" STATUS_TARGET_DIR="$STATUS_TARGET" PATH="$STUB_BIN_N:$PATH" bash "$SCRIPT" --status --source "$SRC" "$STATUS_TARGET" 2>&1)"; rc_n=$?
[[ $rc_n -eq 0 ]] || fail "N: status expected exit 0, got $rc_n; output: $out_n"
grep -qF ".opencode: mounted (source: rightsrc)" <<<"$out_n" || fail "N: --status did not report only the target's own source; output: $out_n"
grep -qF "plans/:   mounted (source: rightsrc)" <<<"$out_n" || fail "N: plans --status did not report only the target's own source; output: $out_n"
if grep -q "wrongsrce" <<<"$out_n"; then
  fail "N: sibling mount source reported as the target's source (regression)"
fi
[[ $failures -eq $start ]] && ok "N: sibling mount differing at a '.' position listed first; --status reports only the target's own source"

if [[ $failures -gt 0 ]]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all opencode-share tests passed"
