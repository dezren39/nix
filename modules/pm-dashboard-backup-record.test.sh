#!/usr/bin/env bash
# Tests for pm-dashboard-backup.sh's run record (#1026, design #1024
# phase 3): every run leaves $HOME/backups/dashboard/.last-run.json.
# Drives the REAL script with the same stubbed bws/journalctl/rclone
# harness as pm-dashboard-backup.test.sh. Scenarios:
#   * happy path — record result=complete, archive_sha256 equals the
#     built archive's sha256, all three destinations "ok", date and
#     job named, started/finished timestamps present
#   * stubbed R2 upload failure — exit non-zero and the record still
#     written: result=failed, failing step names the R2 leg,
#     destinations local=ok / r2=failed / drive=not-attempted
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
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }

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

run_scenario() {
  local name="$1" fail_mode="$2"
  local home="$TMP_ROOT/$name/home"
  mkdir -p "$home/backups/dashboard" "$home/.local/state/pm-aggregator" \
    "$home/atlas/meta-quota-export"
  for f in actions.json comments.json fleet-feed.json quota-current.json registry.json quota-gatherer-state.json quota-gatherer-heartbeat.json; do
    printf '{}\n' >"$home/.local/state/pm-aggregator/$f"
  done
  printf '{}\n' >"$home/atlas/meta-quota-export/latest.json"
  printf '{}\n' >"$home/atlas/meta-quota-export/history.jsonl"
  HOME="$home" PATH="$BIN:$PATH" BWS_ACCESS_TOKEN="<redacted>" \
    RCLONE_FAIL="$fail_mode" RCLONE_STUB_DIR="$TMP_ROOT/$name/rclone-store" \
    PM_DB_DUMP_SCRIPT="$home/no-such-dump-script.sh" \
    bash "$SCRIPT" 2026-10-28 >"$TMP_ROOT/$name.log" 2>&1
  RC=$?
  SCEN_HOME="$home"
}

check_record() { # $1 scenario, $2 python assertions file
  local rec="$SCEN_HOME/backups/dashboard/.last-run.json"
  if [[ -f "$rec" ]]; then echo "ok: $1: run record written"
  else fail "$1: no run record at $rec; log: $(cat "$TMP_ROOT/$1.log")"; return; fi
  if RECORD="$rec" HOME_DIR="$SCEN_HOME" python3 "$2"; then
    echo "ok: $1: record contents as expected"
  else
    fail "$1: record contents wrong: $(cat "$rec")"
  fi
}

cat >"$TMP_ROOT/assert-happy.py" <<'PYEOF'
import hashlib, json, os
rec = json.load(open(os.environ["RECORD"]))
arc = os.path.join(os.environ["HOME_DIR"], "backups/dashboard/2026-10-28/dashboard-2026-10-28.tar.zst")
sha = hashlib.sha256(open(arc, "rb").read()).hexdigest()
assert rec["job"] == "pm-dashboard-backup", rec
assert rec["date"] == "2026-10-28", rec
assert rec["result"] == "complete", rec
assert rec["failing_step"] is None, rec
assert rec["archive_sha256"] == sha, (rec, sha)
assert rec["destinations"] == {"local": "ok", "r2": "ok", "drive": "ok"}, rec
assert rec["started_at"] and rec["finished_at"], rec
PYEOF

cat >"$TMP_ROOT/assert-r2fail.py" <<'PYEOF'
import json, os
rec = json.load(open(os.environ["RECORD"]))
assert rec["result"] == "failed", rec
assert rec["failing_step"] == "r2-upload", rec
assert rec["destinations"]["local"] == "ok", rec
assert rec["destinations"]["r2"] == "failed", rec
assert rec["destinations"]["drive"] == "not-attempted", rec
assert rec["archive_sha256"], rec
PYEOF

run_scenario happy ""
if [[ "$RC" -eq 0 ]]; then echo "ok: happy: script exited 0"
else fail "happy: expected exit 0, got $RC; log: $(cat "$TMP_ROOT/happy.log")"; fi
check_record happy "$TMP_ROOT/assert-happy.py"

run_scenario r2fail R2
if [[ "$RC" -ne 0 ]]; then echo "ok: r2fail: script exited non-zero ($RC)"
else fail "r2fail: expected non-zero exit, got 0"; fi
check_record r2fail "$TMP_ROOT/assert-r2fail.py"

if (( failures > 0 )); then
  echo "pm-dashboard-backup record tests: $failures failure(s)" >&2
  exit 1
fi
echo "pm-dashboard-backup record tests: all passed"
