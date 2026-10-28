#!/usr/bin/env bash
# Tests for laptop-backups-watchdog.sh (#1026): the watchdog must exit
# 0 only when pm-dashboard-backup's run record is fresh + complete AND
# both backup timers fired within the threshold; every violation
# exits non-zero naming the job/timer. Drives the REAL script with a
# stub `systemctl` (per-timer LastTriggerUSec values from files) and
# generated record files whose timestamps are relative to now.
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$SELF_DIR/laptop-backups-watchdog.sh" ]]; then
  SCRIPT="$SELF_DIR/laptop-backups-watchdog.sh"
else
  SCRIPT="$SELF_DIR/modules/laptop-backups-watchdog.sh"
fi

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }

BIN="$TMP_ROOT/bin"
TRIG="$TMP_ROOT/triggers"
mkdir -p "$BIN" "$TRIG"
{ printf '#!%s\n' "$BASH"; cat <<'STUB'
unit="${@: -1}"
f="$WATCHDOG_STUB_TRIGGERS/$unit"
[[ -f "$f" ]] || exit 0
cat "$f"
STUB
} >"$BIN/systemctl"
chmod +x "$BIN/systemctl"

iso_ago() { date -u -d "$1" +%Y-%m-%dT%H:%M:%SZ; }
trig_ago() { date -d "$1" '+%a %Y-%m-%d %H:%M:%S UTC'; }

set_triggers() { # $1 pm trigger spec, $2 repo-archive trigger spec
  trig_ago "$1" >"$TRIG/pm-dashboard-backup.timer"
  trig_ago "$2" >"$TRIG/repo-archive-backup.timer"
}
write_record() { # $1 result, $2 finished spec
  python3 - "$1" "$(iso_ago "$2")" <<'PYEOF'
import json, sys
rec = {"job": "pm-dashboard-backup", "date": "2026-10-28",
       "started_at": sys.argv[2], "finished_at": sys.argv[2],
       "result": sys.argv[1], "exit_code": 0 if sys.argv[1] == "complete" else 1,
       "failing_step": None if sys.argv[1] == "complete" else "r2-upload",
       "archive_sha256": "abc123",
       "destinations": {"local": "ok", "r2": "ok", "drive": "ok"}}
with open(__import__("os").environ["RECORD_OUT"], "w") as fh:
    json.dump(rec, fh)
PYEOF
}

RECORD="$TMP_ROOT/record.json"
run_watchdog() {
  LAPTOP_BACKUPS_RECORD="$RECORD" WATCHDOG_STUB_TRIGGERS="$TRIG" \
    PATH="$BIN:$PATH" bash "$SCRIPT" 26 >"$TMP_ROOT/out.log" 2>&1
  RC=$?
}

expect_ok() {
  if [[ "$RC" -eq 0 ]]; then echo "ok: $1: watchdog exited 0"
  else fail "$1: expected exit 0, got $RC; log: $(cat "$TMP_ROOT/out.log")"; fi
}
expect_named_failure() { # $1 scenario, $2 needle
  if [[ "$RC" -ne 0 ]]; then echo "ok: $1: watchdog exited non-zero ($RC)"
  else fail "$1: expected non-zero exit, got 0"; fi
  if grep -q "$2" "$TMP_ROOT/out.log"; then echo "ok: $1: output names $2"
  else fail "$1: output does not name $2: $(cat "$TMP_ROOT/out.log")"; fi
}

# fresh complete record + recently fired timers -> 0
RECORD_OUT="$RECORD" write_record complete "1 hour ago"
set_triggers "2 hours ago" "1 hour ago"
run_watchdog; expect_ok fresh

# stale record -> non-zero naming pm-dashboard-backup
RECORD_OUT="$RECORD" write_record complete "30 hours ago"
set_triggers "2 hours ago" "1 hour ago"
run_watchdog; expect_named_failure stale-record pm-dashboard-backup

# failed record (however fresh) -> non-zero naming pm-dashboard-backup
RECORD_OUT="$RECORD" write_record failed "10 minutes ago"
run_watchdog; expect_named_failure failed-record pm-dashboard-backup

# stale repo-archive timer -> non-zero naming it
RECORD_OUT="$RECORD" write_record complete "1 hour ago"
set_triggers "1 hour ago" "40 hours ago"
run_watchdog; expect_named_failure stale-timer repo-archive-backup

# missing record -> non-zero naming pm-dashboard-backup
rm -f "$RECORD"
set_triggers "1 hour ago" "1 hour ago"
run_watchdog; expect_named_failure missing-record pm-dashboard-backup

if (( failures > 0 )); then
  echo "laptop-backups-watchdog tests: $failures failure(s)" >&2
  exit 1
fi
echo "laptop-backups-watchdog tests: all passed"
