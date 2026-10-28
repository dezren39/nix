#!/usr/bin/env bash
# Tests for pm-dashboard-backup.sh's DATE validation (#989):
#   modules/pm-dashboard-backup.sh took DATE from $1 with no validation.
#   DATE becomes a directory under the backup root, the archive
#   filename, and both rclone destination prefixes, and the retention
#   prunes assume a real YYYY-MM-DD date — so 'not-a-date' backed up
#   successfully and could never be pruned, '../../x' created
#   directories outside the backup root, and '2026-02-30' was accepted
#   as a day name. The script now rejects anything that is not a real
#   calendar date in strict YYYY-MM-DD form, before any side effect.
# This drives the REAL script (no pasted copy) with stubbed tools:
# a scratch HOME, a stub bws printing fake credential names/values, a
# stub journalctl, and a stub rclone backed by a local store directory.
# Stub shebangs are generated from $BASH so the suite also runs inside
# the nix build sandbox (which has no /usr/bin/env). Scenarios:
#   * 'not-a-date'  — exit non-zero, error names the value, nothing created
#   * '../../escaped' — exit non-zero, nothing created outside the root
#   * '2026-02-30'  — exit non-zero (not a real calendar date)
#   * '2026-2-3'    — exit non-zero (not the strict YYYY-MM-DD shape)
#   * valid date    — exit 0, archive exists locally and in the stub store
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
chmod +x "$BIN/bws" "$BIN/journalctl" "$BIN/rclone"

RC=0
LOG=""
run_scenario() {
  local name="$1" date_arg="$2"
  local home="$TMP_ROOT/$name/home"
  mkdir -p "$home/.local/state/pm-aggregator" "$home/atlas/meta-quota-export"
  printf '{}\n' >"$home/.local/state/pm-aggregator/actions.json"
  printf '{}\n' >"$home/atlas/meta-quota-export/latest.json"
  HOME="$home" PATH="$BIN:$PATH" BWS_ACCESS_TOKEN="<redacted>" \
    RCLONE_STUB_DIR="$TMP_ROOT/$name/rclone-store" \
    PM_DB_DUMP_SCRIPT="$home/no-such-dump-script.sh" \
    bash "$SCRIPT" "$date_arg" >"$TMP_ROOT/$name.log" 2>&1
  RC=$?
  LOG="$TMP_ROOT/$name.log"
  SCEN_HOME="$home"
  SCEN_STORE="$TMP_ROOT/$name/rclone-store"
}

expect_rejected() {
  local name="$1" value="$2"
  if [[ "$RC" -ne 0 ]]; then
    echo "ok: $name: script exited non-zero ($RC)"
  else
    fail "$name: expected non-zero exit for DATE '$value', got 0"
  fi
  if grep -q "invalid DATE '$value'" "$LOG"; then
    echo "ok: $name: error names the rejected value"
  else
    fail "$name: error does not name '$value'; log: $(cat "$LOG")"
  fi
  if [[ ! -e "$SCEN_HOME/backups" ]]; then
    echo "ok: $name: backup root was never created (rejected before any side effect)"
  else
    fail "$name: backup root exists after rejection: $(find "$SCEN_HOME/backups" -mindepth 1 | head -5)"
  fi
  if [[ ! -e "$SCEN_HOME/escaped" && ! -e "$TMP_ROOT/escaped" ]]; then
    echo "ok: $name: nothing created outside the backup root"
  else
    fail "$name: an 'escaped' directory was created outside the backup root"
  fi
  if [[ ! -d "$SCEN_STORE" ]] || [[ -z "$(ls -A "$SCEN_STORE" 2>/dev/null)" ]]; then
    echo "ok: $name: nothing uploaded"
  else
    fail "$name: stub remote store is not empty after rejection"
  fi
}

run_scenario not-a-date not-a-date
expect_rejected not-a-date not-a-date

run_scenario traversal ../../escaped
expect_rejected traversal ../../escaped

run_scenario feb30 2026-02-30
expect_rejected feb30 2026-02-30

run_scenario ragged 2026-2-3
expect_rejected ragged 2026-2-3

run_scenario valid 2026-10-28
if [[ "$RC" -eq 0 ]]; then
  echo "ok: valid: script exited 0"
else
  fail "valid: expected exit 0, got $RC; log: $(cat "$LOG")"
fi
if [[ -f "$SCEN_HOME/backups/dashboard/2026-10-28/dashboard-2026-10-28.tar.zst" ]]; then
  echo "ok: valid: archive exists locally"
else
  fail "valid: local archive missing"
fi
if compgen -G "$SCEN_STORE/*dashboard-2026-10-28.tar.zst*" >/dev/null; then
  echo "ok: valid: archive uploaded to the stub remotes"
else
  fail "valid: archive missing from the stub remotes"
fi

if (( failures > 0 )); then
  echo "pm-dashboard-backup DATE tests: $failures failure(s)" >&2
  exit 1
fi
echo "pm-dashboard-backup DATE tests: all passed"
