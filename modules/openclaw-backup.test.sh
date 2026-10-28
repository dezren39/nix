#!/usr/bin/env bash
# Test suite for openclaw-backup.sh (#1036; the #1034 scenario landed
# first, with its fix in PR #1037).
#   modules/openclaw-backup.sh (added 2026-10-28, PR #1033) shipped
#   with no test suite at all, while its sibling pm-dashboard-backup.sh
#   has three suites wired as flake checks. The script re-implements
#   the same fragile behaviours with its own variations: DATE
#   validation, flock single-instance skip, local + remote 30-day
#   prune that must never remove the run's own date, the journal
#   window min(last success, 24h) from .last-run-epoch with the cursor
#   advancing only after all three legs succeed, the sqlite .backup
#   snapshot over the staged tree, and manifest + per-leg sha256
#   verification. Two defects were found by inspection on day one
#   (#1034 staging pipefail, #1035 prune_remote lsf swallow).
# This drives the REAL script (no pasted copy) with stubbed tools:
# a scratch HOME holding a fake ~/.openclaw, a stub bws printing fake
# credential values, stub journalctl (records its --since argument),
# stub sqlite3, and a stub rclone backed by a local store directory
# with a selectable copyto failure and a scripted lsf listing.
# Scenarios:
#   * staging source-tar failure (#1034) — a tar stub whose
#     create-to-stdout mode emits a valid archive stream and then
#     exits 1 (exactly the changed-mid-read shape): the run must exit
#     non-zero with the FAILED staging line, upload nothing, create
#     no local archive, and leave the journal marker unchanged.
#   * happy path — exit 0, verified local archive containing the
#     staged files (sqlite snapshot included, scratch tree excluded),
#     both offsite copies in the stub store, marker advances.
#   * invalid DATE — a nonexistent calendar date and a malformed date
#     are both rejected non-zero with the value named, nothing
#     uploaded, marker unchanged.
#   * lock skip — a second instance holding the flock makes the run
#     print SKIP and exit 0, creating no archive.
#   * prune — local and remote dirs older than 30 days are pruned;
#     a recent dir and the run's own date are never pruned.
#   * journal window — with a cursor 3 days old, journalctl is
#     called with --since at the cursor, not at 24h ago.
#   * upload failure — a forced R2 copyto failure exits non-zero and
#     leaves the journal marker un-advanced.
#   * prune listing failure (#1035) — stubbed rclone lsf exits
#     non-zero: the run fails naming the remote listing step, the
#     stub's stderr is in the log, no full OK line, the Drive leg
#     never runs, and the journal marker is unchanged (the #1021
#     pattern, mirrored from pm-dashboard-backup).
# Dates are computed relative to today (the script's prune cutoff
# uses the real date), so the suite does not rot on a fixed calendar.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$SELF_DIR/openclaw-backup.sh" ]]; then
  SCRIPT="$SELF_DIR/openclaw-backup.sh"
else
  SCRIPT="$SELF_DIR/modules/openclaw-backup.sh"
fi
REAL_TAR="$(command -v tar)"
RUN_DATE="$(date +%F)"
OLD_DATE="$(date -d '40 days ago' +%F)"
RECENT_DATE="$(date -d '10 days ago' +%F)"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}
ok() { echo "ok: $1"; }

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
echo "$*" >>"${JOURNALCTL_ARGS_LOG:-/dev/null}"
echo "stub journal line"
exit 0
STUB
} >"$BIN/journalctl"

{ printf '#!%s\n' "$BASH"; cat <<'STUB'
# emulate: sqlite3 SRC ".backup 'DEST'" — copy SRC to DEST
dest="${2:-}"
dest="${dest#.backup }"
dest="${dest#\'}"
dest="${dest%\'}"
[[ -n "$dest" ]] || { echo "stub sqlite3: no dest" >&2; exit 1; }
cp "$1" "$dest"
STUB
} >"$BIN/sqlite3"

{ printf '#!%s\n' "$BASH"; cat <<'STUB'
store="${RCLONE_STUB_DIR:?RCLONE_STUB_DIR unset}"
calls="${RCLONE_CALLS_LOG:?RCLONE_CALLS_LOG unset}"
mkdir -p "$store"
echo "$*" >>"$calls"
key() { printf '%s' "$1" | tr '/:' '__'; }
cmd="${1:-}"
shift || true
case "$cmd" in
  copyto)
    case "$2" in
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
    cp "$1" "$store/$(key "$2")"
    ;;
  cat)
    f="$store/$(key "$1")"
    [[ -f "$f" ]] || exit 1
    cat "$f"
    ;;
  lsf)
    if [[ "${RCLONE_LSF_FAIL:-}" == "1" ]]; then
      echo "stub rclone: forced lsf failure" >&2
      exit 1
    fi
    if [[ -n "${RCLONE_LSF_FILE:-}" && -f "${RCLONE_LSF_FILE:-}" ]]; then
      cat "$RCLONE_LSF_FILE"
    fi
    exit 0
    ;;
  purge) exit 0 ;;
  *)
    echo "stub rclone: unsupported command: $cmd" >&2
    exit 1
    ;;
esac
STUB
} >"$BIN/rclone"
chmod +x "$BIN/bws" "$BIN/journalctl" "$BIN/sqlite3" "$BIN/rclone"

# tar stub for the staging-failure scenario: create-to-stdout emits a
# valid archive stream (so the extraction tar exits 0) and then exits
# 1 — the exact "file changed as we read it" shape from #1034. Every
# other tar mode passes through to the real tar.
TARBIN="$TMP_ROOT/tar-stub-bin"
mkdir -p "$TARBIN"
{ printf '#!%s\n' "$BASH"; cat <<'STUB'
real="${OPENCLAW_REAL_TAR:?OPENCLAW_REAL_TAR unset}"
if [[ "${1:-}" == "-cf" && "${2:-}" == "-" ]]; then
  d="$(mktemp -d)"
  printf 'partial\n' >"$d/partial.txt"
  "$real" -cf - -C "$d" .
  rm -rf "$d"
  echo "stub tar: ./live.log: file changed as we read it" >&2
  exit 1
fi
exec "$real" "$@"
STUB
} >"$TARBIN/tar"
chmod +x "$TARBIN/tar"

PRESET=1000000000
RC=0
MARKER_AFTER=""
RUN_HOME=""
DATE_ARG="$RUN_DATE"
FAIL_MODE=""
LSF_FAIL=""
TAR_OVERLAY=""

setup_home() {
  local name="$1" marker="${2:-$PRESET}"
  RUN_HOME="$TMP_ROOT/$name/home"
  mkdir -p "$RUN_HOME/.openclaw/state" "$RUN_HOME/.openclaw/tmp" \
    "$RUN_HOME/backups/openclaw"
  printf 'session log line\n' >"$RUN_HOME/.openclaw/session.log"
  printf 'fake sqlite bytes\n' >"$RUN_HOME/.openclaw/state/openclaw.sqlite"
  printf 'scratch\n' >"$RUN_HOME/.openclaw/tmp/scratch.txt"
  echo "$marker" >"$RUN_HOME/backups/openclaw/.last-run-epoch"
}

run_backup() {
  local name="$1"
  local path_prefix="$BIN"
  [[ -n "$TAR_OVERLAY" ]] && path_prefix="$TAR_OVERLAY:$BIN"
  HOME="$RUN_HOME" PATH="$path_prefix:$PATH" BWS_ACCESS_TOKEN="fake-token" \
    OPENCLAW_REAL_TAR="$REAL_TAR" \
    RCLONE_FAIL="$FAIL_MODE" RCLONE_LSF_FAIL="${LSF_FAIL:-}" \
    RCLONE_STUB_DIR="$TMP_ROOT/$name/rclone-store" \
    RCLONE_CALLS_LOG="$TMP_ROOT/$name/rclone-calls.log" \
    RCLONE_LSF_FILE="$TMP_ROOT/$name/lsf.txt" \
    JOURNALCTL_ARGS_LOG="$TMP_ROOT/$name/journalctl-args.log" \
    bash "$SCRIPT" "$DATE_ARG" >"$TMP_ROOT/$name.log" 2>&1
  RC=$?
  MARKER_AFTER="$(cat "$RUN_HOME/backups/openclaw/.last-run-epoch" 2>/dev/null || echo missing)"
}

no_upload() { # $1 scenario name — true when no copyto was attempted
  [[ ! -f "$TMP_ROOT/$1/rclone-calls.log" ]] \
    || ! grep -q '^copyto' "$TMP_ROOT/$1/rclone-calls.log"
}

# --- staging source-tar failure fails the run (#1034) ---------------------
setup_home staging-fail
TAR_OVERLAY="$TARBIN"
run_backup staging-fail
TAR_OVERLAY=""

if [[ "$RC" -ne 0 ]]; then
  ok "staging-fail: script exited non-zero ($RC)"
else
  fail "staging-fail: expected non-zero exit, got 0; log: $(cat "$TMP_ROOT/staging-fail.log")"
fi
if grep -q 'openclaw-backup: FAILED staging copy of' "$TMP_ROOT/staging-fail.log"; then
  ok "staging-fail: FAILED staging line printed"
else
  fail "staging-fail: no FAILED staging line in log: $(cat "$TMP_ROOT/staging-fail.log")"
fi
if no_upload staging-fail; then
  ok "staging-fail: no upload attempted"
else
  fail "staging-fail: an upload was attempted after the staging failure"
fi
if find "$RUN_HOME/backups/openclaw" -name '*.tar.zst' | grep -q .; then
  fail "staging-fail: a local archive was created despite the staging failure"
else
  ok "staging-fail: no local archive created"
fi
if [[ "$MARKER_AFTER" == "$PRESET" ]]; then
  ok "staging-fail: journal marker unchanged at $PRESET"
else
  fail "staging-fail: marker moved on a failed run — was $PRESET, now $MARKER_AFTER"
fi

# --- happy path -------------------------------------------------------------
setup_home happy
run_backup happy

if [[ "$RC" -eq 0 ]]; then
  ok "happy: script exited 0"
else
  fail "happy: expected exit 0, got $RC; log: $(cat "$TMP_ROOT/happy.log")"
fi
ARCHIVE="$RUN_HOME/backups/openclaw/$RUN_DATE/openclaw-$RUN_DATE.tar.zst"
if [[ -f "$ARCHIVE" ]]; then
  ok "happy: local archive created"
  LISTING="$("$REAL_TAR" -I zstd -tf "$ARCHIVE")"
  if grep -qx './session.log' <<<"$LISTING" \
    && grep -qx './state/openclaw.sqlite' <<<"$LISTING" \
    && ! grep -q '^\./tmp/' <<<"$LISTING"; then
    ok "happy: archive holds staged files + sqlite snapshot, scratch excluded"
  else
    fail "happy: unexpected archive contents: $LISTING"
  fi
else
  fail "happy: local archive missing at $ARCHIVE; log: $(cat "$TMP_ROOT/happy.log")"
fi
if [[ "$(find "$TMP_ROOT/happy/rclone-store" -type f | wc -l)" -eq 2 ]]; then
  ok "happy: both offsite copies uploaded to the stub store"
else
  fail "happy: expected 2 offsite copies, found: $(find "$TMP_ROOT/happy/rclone-store" -type f)"
fi
if grep -q "OK openclaw $RUN_DATE backed up to local + R2 + Drive" "$TMP_ROOT/happy.log"; then
  ok "happy: success line printed"
else
  fail "happy: no success line in log: $(cat "$TMP_ROOT/happy.log")"
fi
if [[ "$MARKER_AFTER" =~ ^[0-9]+$ ]] && [[ "$MARKER_AFTER" -gt "$PRESET" ]]; then
  ok "happy: journal marker advanced past $PRESET (now $MARKER_AFTER)"
else
  fail "happy: marker did not advance on a fully successful run — was $PRESET, now $MARKER_AFTER"
fi

# --- invalid DATE is rejected ------------------------------------------------
setup_home bad-date
DATE_ARG="2026-02-30"
run_backup bad-date
if [[ "$RC" -ne 0 ]] && grep -q "FAILED invalid DATE '2026-02-30'" "$TMP_ROOT/bad-date.log"; then
  ok "bad-date: nonexistent calendar date rejected with the value named"
else
  fail "bad-date: 2026-02-30 not rejected as expected (rc=$RC): $(cat "$TMP_ROOT/bad-date.log")"
fi
DATE_ARG="not-a-date"
run_backup bad-date
if [[ "$RC" -ne 0 ]] && grep -q "FAILED invalid DATE 'not-a-date'" "$TMP_ROOT/bad-date.log"; then
  ok "bad-date: malformed date rejected with the value named"
else
  fail "bad-date: not-a-date not rejected as expected (rc=$RC): $(cat "$TMP_ROOT/bad-date.log")"
fi
DATE_ARG="$RUN_DATE"
if no_upload bad-date && [[ "$MARKER_AFTER" == "$PRESET" ]]; then
  ok "bad-date: nothing uploaded, marker unchanged"
else
  fail "bad-date: upload attempted or marker moved on a rejected date"
fi

# --- concurrent run skips on the lock -----------------------------------------
setup_home lock-skip
( flock -x 9; sleep 5 ) 9>"$RUN_HOME/backups/openclaw/.openclaw-backup.lock" &
lock_holder=$!
sleep 1
run_backup lock-skip
kill "$lock_holder" 2>/dev/null
wait "$lock_holder" 2>/dev/null
if [[ "$RC" -eq 0 ]] && grep -q 'SKIP another openclaw-backup run holds the lock' "$TMP_ROOT/lock-skip.log"; then
  ok "lock-skip: concurrent run printed SKIP and exited 0"
else
  fail "lock-skip: expected SKIP + exit 0, got rc=$RC: $(cat "$TMP_ROOT/lock-skip.log")"
fi
if find "$RUN_HOME/backups/openclaw" -name '*.tar.zst' | grep -q .; then
  fail "lock-skip: the skipped run created an archive"
else
  ok "lock-skip: skipped run created no archive"
fi

# --- prune: only >30-day dirs, never recent or the run date -------------------
setup_home prune
mkdir -p "$RUN_HOME/backups/openclaw/$OLD_DATE" "$RUN_HOME/backups/openclaw/$RECENT_DATE"
printf 'old\n' >"$RUN_HOME/backups/openclaw/$OLD_DATE/old.txt"
printf 'recent\n' >"$RUN_HOME/backups/openclaw/$RECENT_DATE/recent.txt"
printf '%s/\n%s/\n%s/\n' "$OLD_DATE" "$RECENT_DATE" "$RUN_DATE" >"$TMP_ROOT/prune/lsf.txt"
run_backup prune
if [[ "$RC" -eq 0 ]]; then
  ok "prune: script exited 0"
else
  fail "prune: expected exit 0, got $RC; log: $(cat "$TMP_ROOT/prune.log")"
fi
if [[ ! -d "$RUN_HOME/backups/openclaw/$OLD_DATE" ]]; then
  ok "prune: local dir older than 30 days pruned ($OLD_DATE)"
else
  fail "prune: local old dir $OLD_DATE was not pruned"
fi
if [[ -d "$RUN_HOME/backups/openclaw/$RECENT_DATE" && -d "$RUN_HOME/backups/openclaw/$RUN_DATE" ]]; then
  ok "prune: local recent dir and run date kept"
else
  fail "prune: local recent dir or run date was pruned"
fi
purges="$(grep -c "^purge .*/$OLD_DATE\$" "$TMP_ROOT/prune/rclone-calls.log" 2>/dev/null || true)"
if [[ "$purges" -eq 2 ]]; then
  ok "prune: remote old dir purged on both remotes"
else
  fail "prune: expected 2 remote purges of $OLD_DATE, got $purges: $(cat "$TMP_ROOT/prune/rclone-calls.log" 2>/dev/null)"
fi
if grep -q "^purge .*/\($RECENT_DATE\|$RUN_DATE\)\$" "$TMP_ROOT/prune/rclone-calls.log"; then
  fail "prune: a recent or run-date remote dir was purged"
else
  ok "prune: no remote purge of recent or run-date dirs"
fi

# --- prune listing failure fails the run (#1035) ------------------------------
setup_home lsf-fail
LSF_FAIL=1
run_backup lsf-fail
LSF_FAIL=""
if [[ "$RC" -ne 0 ]]; then
  ok "lsf-fail: script exited non-zero ($RC)"
else
  fail "lsf-fail: expected non-zero exit, got 0; log: $(cat "$TMP_ROOT/lsf-fail.log")"
fi
if grep -q 'openclaw-backup: FAILED prune listing failed on R2' "$TMP_ROOT/lsf-fail.log"; then
  ok "lsf-fail: FAILED line names the remote and the listing step"
else
  fail "lsf-fail: no FAILED prune-listing line: $(cat "$TMP_ROOT/lsf-fail.log")"
fi
if grep -q 'stub rclone: forced lsf failure' "$TMP_ROOT/lsf-fail.log"; then
  ok "lsf-fail: rclone stderr surfaced in the log"
else
  fail "lsf-fail: rclone stderr not in log: $(cat "$TMP_ROOT/lsf-fail.log")"
fi
if grep -q 'OK openclaw' "$TMP_ROOT/lsf-fail.log"; then
  fail "lsf-fail: full OK line printed on a failed run"
else
  ok "lsf-fail: no full OK line printed"
fi
copytos="$(grep -c '^copyto' "$TMP_ROOT/lsf-fail/rclone-calls.log" 2>/dev/null || true)"
if [[ "$copytos" -eq 1 ]]; then
  ok "lsf-fail: only the R2 copy (before its prune) was attempted; Drive never ran"
else
  fail "lsf-fail: expected exactly 1 copyto attempt, got $copytos"
fi
if [[ "$MARKER_AFTER" == "$PRESET" ]]; then
  ok "lsf-fail: journal marker unchanged at $PRESET"
else
  fail "lsf-fail: marker moved on a failed run — was $PRESET, now $MARKER_AFTER"
fi

# --- journal window honours an older-than-24h cursor ---------------------------
JW_PRESET="$(date -d '3 days ago' +%s)"
setup_home journal-window "$JW_PRESET"
run_backup journal-window
EXPECTED_SINCE="$(date -d "@$JW_PRESET" '+%F %T')"
if [[ "$RC" -eq 0 ]] && grep -qF -- "--since $EXPECTED_SINCE" "$TMP_ROOT/journal-window/journalctl-args.log"; then
  ok "journal-window: journalctl called with --since at the 3-day-old cursor"
else
  fail "journal-window: expected --since '$EXPECTED_SINCE' (rc=$RC), args: $(cat "$TMP_ROOT/journal-window/journalctl-args.log" 2>/dev/null)"
fi

# --- upload failure leaves the cursor un-advanced -------------------------------
setup_home upload-fail
FAIL_MODE="R2"
run_backup upload-fail
FAIL_MODE=""
if [[ "$RC" -ne 0 ]] && grep -q 'FAILED R2 upload failed' "$TMP_ROOT/upload-fail.log"; then
  ok "upload-fail: R2 failure exited non-zero with the FAILED line"
else
  fail "upload-fail: expected non-zero + FAILED R2 line, got rc=$RC: $(cat "$TMP_ROOT/upload-fail.log")"
fi
if [[ "$MARKER_AFTER" == "$PRESET" ]]; then
  ok "upload-fail: journal marker unchanged at $PRESET"
else
  fail "upload-fail: marker advanced on a failed run — was $PRESET, now $MARKER_AFTER"
fi

if (( failures > 0 )); then
  echo "openclaw-backup tests: $failures failure(s)" >&2
  exit 1
fi
echo "openclaw-backup tests: all passed"
