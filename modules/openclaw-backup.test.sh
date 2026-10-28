#!/usr/bin/env bash
# Tests for openclaw-backup.sh's staging pipeline (#1034):
#   modules/openclaw-backup.sh staged the live ~/.openclaw tree with
#     tar -cf - ... | tar -xf - -C "$STAGE" || fail "staging copy ..."
#   and never set pipefail, so the pipeline's status was the extraction
#   tar's alone. A source tar failing mid-read ("file changed as we
#   read it" — the normal case while the gateway appends to its files)
#   exited 1, extraction exited 0, staging "succeeded" with a torn
#   copy, and the manifest — generated from the staged tree — verified
#   by construction, so the run reported full success to all three
#   destinations.
# This drives the REAL script (no pasted copy) with stubbed tools:
# a scratch HOME holding a fake ~/.openclaw, a stub bws printing fake
# credential values, stub journalctl/sqlite3, and a stub rclone backed
# by a local store directory. Scenarios:
#   * staging source-tar failure — a tar stub whose create-to-stdout
#     mode emits a valid archive stream and then exits 1 (exactly the
#     changed-mid-read shape): the run must exit non-zero with the
#     FAILED staging line, upload nothing, create no local archive,
#     and leave the journal marker unchanged.
#   * happy path — exit 0, verified local archive containing the
#     staged files (sqlite snapshot included, scratch tree excluded),
#     both offsite copies in the stub store, marker advances.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$SELF_DIR/openclaw-backup.sh" ]]; then
  SCRIPT="$SELF_DIR/openclaw-backup.sh"
else
  SCRIPT="$SELF_DIR/modules/openclaw-backup.sh"
fi
REAL_TAR="$(command -v tar)"

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
    cp "$1" "$store/$(key "$2")"
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

run_scenario() {
  local name="$1" tar_overlay="$2"
  RUN_HOME="$TMP_ROOT/$name/home"
  mkdir -p "$RUN_HOME/.openclaw/state" "$RUN_HOME/.openclaw/tmp" \
    "$RUN_HOME/backups/openclaw"
  printf 'session log line\n' >"$RUN_HOME/.openclaw/session.log"
  printf 'fake sqlite bytes\n' >"$RUN_HOME/.openclaw/state/openclaw.sqlite"
  printf 'scratch\n' >"$RUN_HOME/.openclaw/tmp/scratch.txt"
  echo "$PRESET" >"$RUN_HOME/backups/openclaw/.last-run-epoch"
  local path_prefix="$BIN"
  [[ -n "$tar_overlay" ]] && path_prefix="$tar_overlay:$BIN"
  HOME="$RUN_HOME" PATH="$path_prefix:$PATH" BWS_ACCESS_TOKEN="fake-token" \
    OPENCLAW_REAL_TAR="$REAL_TAR" \
    RCLONE_STUB_DIR="$TMP_ROOT/$name/rclone-store" \
    RCLONE_CALLS_LOG="$TMP_ROOT/$name/rclone-calls.log" \
    bash "$SCRIPT" 2026-10-28 >"$TMP_ROOT/$name.log" 2>&1
  RC=$?
  MARKER_AFTER="$(cat "$RUN_HOME/backups/openclaw/.last-run-epoch" 2>/dev/null || echo missing)"
}

# --- staging source-tar failure fails the run (#1034) ---------------------
run_scenario staging-fail "$TARBIN"

if [[ "$RC" -ne 0 ]]; then
  echo "ok: staging-fail: script exited non-zero ($RC)"
else
  fail "staging-fail: expected non-zero exit, got 0; log: $(cat "$TMP_ROOT/staging-fail.log")"
fi
if grep -q 'openclaw-backup: FAILED staging copy of' "$TMP_ROOT/staging-fail.log"; then
  echo "ok: staging-fail: FAILED staging line printed"
else
  fail "staging-fail: no FAILED staging line in log: $(cat "$TMP_ROOT/staging-fail.log")"
fi
if [[ -f "$TMP_ROOT/staging-fail/rclone-calls.log" ]] \
  && grep -q '^copyto' "$TMP_ROOT/staging-fail/rclone-calls.log"; then
  fail "staging-fail: an upload was attempted after the staging failure"
else
  echo "ok: staging-fail: no upload attempted"
fi
if find "$RUN_HOME/backups/openclaw" -name '*.tar.zst' | grep -q .; then
  fail "staging-fail: a local archive was created despite the staging failure"
else
  echo "ok: staging-fail: no local archive created"
fi
if [[ "$MARKER_AFTER" == "$PRESET" ]]; then
  echo "ok: staging-fail: journal marker unchanged at $PRESET"
else
  fail "staging-fail: marker moved on a failed run — was $PRESET, now $MARKER_AFTER"
fi

# --- happy path -------------------------------------------------------------
run_scenario happy ""

if [[ "$RC" -eq 0 ]]; then
  echo "ok: happy: script exited 0"
else
  fail "happy: expected exit 0, got $RC; log: $(cat "$TMP_ROOT/happy.log")"
fi
ARCHIVE="$RUN_HOME/backups/openclaw/2026-10-28/openclaw-2026-10-28.tar.zst"
if [[ -f "$ARCHIVE" ]]; then
  echo "ok: happy: local archive created"
  LISTING="$("$REAL_TAR" -I zstd -tf "$ARCHIVE")"
  if grep -qx './session.log' <<<"$LISTING" \
    && grep -qx './state/openclaw.sqlite' <<<"$LISTING" \
    && ! grep -q '^\./tmp/' <<<"$LISTING"; then
    echo "ok: happy: archive holds staged files + sqlite snapshot, scratch excluded"
  else
    fail "happy: unexpected archive contents: $LISTING"
  fi
else
  fail "happy: local archive missing at $ARCHIVE; log: $(cat "$TMP_ROOT/happy.log")"
fi
if [[ "$(find "$TMP_ROOT/happy/rclone-store" -type f | wc -l)" -eq 2 ]]; then
  echo "ok: happy: both offsite copies uploaded to the stub store"
else
  fail "happy: expected 2 offsite copies, found: $(find "$TMP_ROOT/happy/rclone-store" -type f)"
fi
if grep -q 'OK openclaw 2026-10-28 backed up to local + R2 + Drive' "$TMP_ROOT/happy.log"; then
  echo "ok: happy: success line printed"
else
  fail "happy: no success line in log: $(cat "$TMP_ROOT/happy.log")"
fi
if [[ "$MARKER_AFTER" =~ ^[0-9]+$ ]] && [[ "$MARKER_AFTER" -gt "$PRESET" ]]; then
  echo "ok: happy: journal marker advanced past $PRESET (now $MARKER_AFTER)"
else
  fail "happy: marker did not advance on a fully successful run — was $PRESET, now $MARKER_AFTER"
fi

if (( failures > 0 )); then
  echo "openclaw-backup tests: $failures failure(s)" >&2
  exit 1
fi
echo "openclaw-backup tests: all passed"
