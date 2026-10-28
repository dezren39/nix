# laptop-backups-watchdog — freshness check for the laptop backups
# (#1026, design #1024 phase 3; the #976 acceptance "a freshness check
# exists so a stopped backup gets named").
#
# Read-only: it changes nothing and attempts no repair. It exits 0
# only when BOTH hold, and otherwise exits 1 with one line per
# violation naming the job/timer:
#   (a) pm-dashboard-backup's run record ($HOME/backups/dashboard/
#       .last-run.json, written by modules/pm-dashboard-backup.sh on
#       every exit path) says result=complete and finished within
#       the threshold — a failed record is a violation however fresh,
#       so a nightly failure can never masquerade as freshness;
#   (b) both backup timers (pm-dashboard-backup.timer and
#       repo-archive-backup.timer) last triggered within the same
#       threshold, per `systemctl --user show -p LastTriggerUSec`.
# repo-archive-backup is covered via its timer's last-trigger time
# only: its script and restic credentials live outside this repo, so
# no run record is required of it (stated in laptop-backups.nix too).
#
# Usage: laptop-backups-watchdog [max-age-hours]   (default 26, a
# little over one daily period; the module option
# services.laptop-backups.watchdog.maxAgeHours feeds this argument).
# Test hooks: LAPTOP_BACKUPS_RECORD overrides the record path and
# `systemctl` is resolved via PATH, so the suite can stub both.
MAX_AGE_HOURS="${1:-26}"
RECORD="${LAPTOP_BACKUPS_RECORD:-$HOME/backups/dashboard/.last-run.json}"
NOW_EPOCH="$(date +%s)"
MAX_AGE_SEC=$(( MAX_AGE_HOURS * 3600 ))
violations=0

complain() { echo "laptop-backups-watchdog: FAIL $*" >&2; violations=$((violations + 1)); }

# --- (a) the dashboard run record -------------------------------------------
if [ ! -f "$RECORD" ]; then
  complain "pm-dashboard-backup: no run record at $RECORD"
else
  verdict="$(RECORD="$RECORD" NOW_EPOCH="$NOW_EPOCH" MAX_AGE_SEC="$MAX_AGE_SEC" \
    python3 - <<'PYEOF' 2>/dev/null
import json, os
from datetime import datetime, timezone
try:
    rec = json.load(open(os.environ["RECORD"]))
    finished = datetime.strptime(rec["finished_at"], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)
except Exception:
    print("unreadable")
    raise SystemExit
age = int(os.environ["NOW_EPOCH"]) - int(finished.timestamp())
if rec.get("result") != "complete":
    print(f"failed result={rec.get('result')} failing_step={rec.get('failing_step')} finished_at={rec.get('finished_at')}")
elif age > int(os.environ["MAX_AGE_SEC"]):
    print(f"stale age_hours={age // 3600} finished_at={rec.get('finished_at')}")
else:
    print("ok")
PYEOF
  )"
  case "$verdict" in
    ok) ;;
    unreadable|"") complain "pm-dashboard-backup: run record at $RECORD is unreadable" ;;
    *) complain "pm-dashboard-backup: run record $verdict (threshold ${MAX_AGE_HOURS}h)" ;;
  esac
fi

# --- (b) both timers' last triggers ------------------------------------------
for timer in pm-dashboard-backup.timer repo-archive-backup.timer; do
  job="${timer%.timer}"
  raw="$(systemctl --user show -p LastTriggerUSec --value "$timer" 2>/dev/null || true)"
  if [ -z "$raw" ]; then
    complain "$job: timer $timer has never triggered"
    continue
  fi
  trig_epoch="$(date -d "$raw" +%s 2>/dev/null || echo "")"
  if [ -z "$trig_epoch" ]; then
    complain "$job: timer $timer last trigger unparseable: '$raw'"
    continue
  fi
  age=$(( NOW_EPOCH - trig_epoch ))
  if [ "$age" -gt "$MAX_AGE_SEC" ]; then
    complain "$job: timer $timer last triggered $(( age / 3600 ))h ago (threshold ${MAX_AGE_HOURS}h)"
  fi
done

if [ "$violations" -gt 0 ]; then
  echo "laptop-backups-watchdog: $violations violation(s)" >&2
  exit 1
fi
echo "laptop-backups-watchdog: OK pm-dashboard-backup record fresh+complete, both timers fired within ${MAX_AGE_HOURS}h"
