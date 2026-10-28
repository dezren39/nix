#!/usr/bin/env bash
# Tests for pm-dashboard-backup.sh's journal cursor (#981):
#   modules/pm-dashboard-backup.sh wrote .last-run-epoch right after the
#   local leg verified, before the R2 and Drive legs, contradicting its
#   own contract that the journal window starts at the last SUCCESSFUL
#   run (line: "window start = min(last successful run, 24h ago)").
#   A run that failed on an offsite leg therefore advanced the cursor
#   anyway, and that run's journal window was never included in any
#   later archive — it never reached R2/Drive, and the local copy is
#   pruned after 30 days.
# This drives the REAL script (no pasted copy) with stubbed tools:
# a scratch HOME, a stub bws printing fake credential names/values, a
# stub journalctl, and a stub rclone backed by a local store directory
# with a selectable R2/Drive copyto failure. Scenarios:
#   * R2 failure    — exit non-zero, marker unchanged
#   * Drive failure — exit non-zero, marker unchanged
#   * happy path    — exit 0, marker advances past its preset value
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$SELF_DIR/pm-dashboard-backup.sh" ]]; then
  SCRIPT="$SELF_DIR/pm-dashboard-backup.sh"
else
  SCRIPT="$SELF_DIR/modules/pm-dashboard-backup.sh"
fi

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

BIN="$TMP_ROOT/bin"
mkdir -p "$BIN"

{ printf '#!%s\n' "$BASH"; cat <<'STUB'
if [[ "${1:-} ${2:-}" == "secret list" ]]; then
  printf '%s\n' '[{"key":"cloudflare.r2-backups-access-key-id","value":"fake-access-key-id"},{"key":"cloudflare.r2-backups-secret-access-key","value":"fake-secret-access-key"},{"key":"cloudflare.r2-endpoint","value":"https://example.invalid"}]'
  exit 0
fi
echo "stub bws: unsupported args: $*" >&2
exit 1
STUB
} >"$BIN/bws"

{ printf '#!%s\n' "$BASH"; cat <<'STUB'
echo "stub journal line"
exit 0
STUB
} >"$BIN/journalctl"

{ printf '#!%s\n' "$BASH"; cat <<'STUB'
store="${RCLONE_STUB_DIR:?RCLONE_STUB_DIR unset}"
mkdir -p "$store"
key() { printf '%s' "$1" | tr '/:' '__'; }
cmd="${1:-}"
shift || true
case "$cmd" in
  copyto)
    src="$1" dest="$2"
    case "$dest" in
      r2bak:*)
        if [[ "${RCLONE_FAIL:-}" == "R2" ]]; then
          echo "stub rclone: forced R2 failure" >&2
          exit 42
        fi
        ;;
      drive:*)
        if [[ "${RCLONE_FAIL:-}" == "Drive" ]]; then
          echo "stub rclone: forced Drive failure" >&2
          exit 42
        fi
        ;;
    esac
    cp "$src" "$store/$(key "$dest")"
    ;;
  cat)
    f="$store/$(key "$1")"
    [[ -f "$f" ]] || exit 1
    cat "$f"
    ;;
  lsf) exit 0 ;;
  purge) exit 0 ;;
  *)
    echo "stub rclone: unsupported command: $cmd" >&2
    exit 1
    ;;
esac
STUB
} >"$BIN/rclone"
chmod +x "$BIN/bws" "$BIN/journalctl" "$BIN/rclone"

PRESET=1000000000
RC=0
MARKER_AFTER=""

run_scenario() {
  local name="$1" fail_mode="$2"
  local home="$TMP_ROOT/$name/home"
  mkdir -p "$home/backups/dashboard" "$home/.local/state/pm-aggregator" \
    "$home/atlas/meta-quota-export"
  printf '{}\n' >"$home/.local/state/pm-aggregator/actions.json"
  printf '{}\n' >"$home/atlas/meta-quota-export/latest.json"
  echo "$PRESET" >"$home/backups/dashboard/.last-run-epoch"
  HOME="$home" PATH="$BIN:$PATH" BWS_ACCESS_TOKEN="fake-token" \
    RCLONE_FAIL="$fail_mode" RCLONE_STUB_DIR="$TMP_ROOT/$name/rclone-store" \
    PM_DB_DUMP_SCRIPT="$home/no-such-dump-script.sh" \
    bash "$SCRIPT" 2026-10-28 >"$TMP_ROOT/$name.log" 2>&1
  RC=$?
  MARKER_AFTER="$(cat "$home/backups/dashboard/.last-run-epoch" 2>/dev/null || echo missing)"
}

expect_marker_unchanged() {
  if [[ "$RC" -ne 0 ]]; then
    echo "ok: $1: script exited non-zero ($RC)"
  else
    fail "$1: expected non-zero exit, got 0"
  fi
  if [[ "$MARKER_AFTER" == "$PRESET" ]]; then
    echo "ok: $1: marker unchanged at $PRESET"
  else
    fail "$1: marker advanced on a failed run — was $PRESET, now $MARKER_AFTER"
  fi
}

run_scenario r2-fail R2
expect_marker_unchanged r2-fail

run_scenario drive-fail Drive
expect_marker_unchanged drive-fail

run_scenario happy ""

# --- post-staging failure keeps the FAILED-exit marker (#1010) ------------
# The :40 EXIT trap used to be replaced by the staging dir's own EXIT
# trap, so a bare command failing after staging (production runs under
# the writeShellScriptBin `bash -e` shebang) exited with no
# "FAILED exit=" line. Force tar to fail at the archive step, run the
# script the production way (bash -e), and require: non-zero exit, the
# FAILED marker with tar's exit code, and no staging dir left behind.
TARBIN="$TMP_ROOT/tar-fail-bin"
mkdir -p "$TARBIN"
{ printf '#!%s\n' "$BASH"; cat <<'STUB'
echo "stub tar: forced failure" >&2
exit 3
STUB
} >"$TARBIN/tar"
chmod +x "$TARBIN/tar"

tf_home="$TMP_ROOT/tar-fail/home"
mkdir -p "$tf_home/backups/dashboard" "$tf_home/.local/state/pm-aggregator" \
  "$tf_home/atlas/meta-quota-export" "$tf_home/tmp"
printf '{}\n' >"$tf_home/.local/state/pm-aggregator/actions.json"
printf '{}\n' >"$tf_home/atlas/meta-quota-export/latest.json"
HOME="$tf_home" PATH="$TARBIN:$BIN:$PATH" BWS_ACCESS_TOKEN="<redacted>" \
  RCLONE_STUB_DIR="$TMP_ROOT/tar-fail/rclone-store" \
  PM_DB_DUMP_SCRIPT="$tf_home/no-such-dump-script.sh" \
  TMPDIR="$tf_home/tmp" \
  bash -e "$SCRIPT" 2026-10-28 >"$TMP_ROOT/tar-fail.log" 2>&1
tf_rc=$?
if [[ "$tf_rc" -eq 3 ]]; then
  echo "ok: tar-fail: script exited with tar's code (3)"
else
  fail "tar-fail: expected exit 3, got $tf_rc; log: $(cat "$TMP_ROOT/tar-fail.log")"
fi
if grep -q 'pm-dashboard-backup: FAILED exit=3' "$TMP_ROOT/tar-fail.log"; then
  echo "ok: tar-fail: FAILED exit=3 marker printed for a post-staging failure"
else
  fail "tar-fail: no 'FAILED exit=3' marker in log: $(cat "$TMP_ROOT/tar-fail.log")"
fi
if [[ -z "$(ls -A "$tf_home/tmp")" ]]; then
  echo "ok: tar-fail: staging dir removed by the EXIT handler"
else
  fail "tar-fail: staging dir left behind in TMPDIR: $(ls -A "$tf_home/tmp")"
fi
if [[ "$RC" -eq 0 ]]; then
  echo "ok: happy: script exited 0"
else
  fail "happy: expected exit 0, got $RC; log: $(cat "$TMP_ROOT/happy.log")"
fi
if [[ "$MARKER_AFTER" =~ ^[0-9]+$ ]] && [[ "$MARKER_AFTER" -gt "$PRESET" ]]; then
  echo "ok: happy: marker advanced past $PRESET (now $MARKER_AFTER)"
else
  fail "happy: marker did not advance on a fully successful run — was $PRESET, now $MARKER_AFTER"
fi

if (( failures > 0 )); then
  echo "pm-dashboard-backup tests: $failures failure(s)" >&2
  exit 1
fi
echo "pm-dashboard-backup tests: all passed"
