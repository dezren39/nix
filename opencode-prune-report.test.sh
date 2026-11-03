#!/usr/bin/env bash
# Regression test for the opencode prune dry-run report (#659).
#
# #659: clean.sh's scratch sweep and opencode-db-backup-prune.sh both existed
# but nothing scheduled them, so the tmp store grew between manual runs. This
# test extracts the REAL report script from opencode-prune-report.nix, points
# its two tool paths at stubs, and drives it:
#   both succeed        -> exit 0, both commands invoked
#   clean.sh fails      -> exit nonzero, pruner still invoked (one failure
#                          does not hide the other pruner's report)
#   arguments           -> clean.sh gets --dry-run and --no-git-maintenance;
#                          neither command ever gets --apply or
#                          --prune-unknown-scratch (the no-deletion guarantee)
#   job definition      -> StartCalendarInterval present, RunAtLoad false,
#                          report log under ~/Library/Logs
#
# Run: bash opencode-prune-report.test.sh (exit 0 = all passed)
set -u

cd "$(dirname "$0")" || exit 1
failures=0

check() { # $1 name, $2 condition-result (0 = pass)
  if [ "$2" -eq 0 ]; then
    echo "PASS: $1"
  else
    echo "FAIL: $1"
    failures=$((failures + 1))
  fi
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/tools" "$work/home"

# Stubs stand in for the two real pruners; each records its argv.
cat > "$work/tools/clean.sh" <<'STUB'
#!/usr/bin/env bash
echo "clean.sh $*" >> "$ARGS_LOG"
exit "${STUB_CLEAN_RC:-0}"
STUB

cat > "$work/tools/opencode-db-backup-prune.sh" <<'STUB'
#!/usr/bin/env bash
echo "opencode-db-backup-prune.sh $*" >> "$ARGS_LOG"
exit "${STUB_PRUNE_RC:-0}"
STUB

# Extract the report script body from the nix module.
python3 - "$work/report.sh" "$work/tools" <<'PYEOF'
import os, re, sys
out, tools = sys.argv[1], sys.argv[2]
src = open("opencode-prune-report.nix").read()
m = re.search(r'writeShellScript "opencode-prune-report" +\'\'\n(.*?)\n  \'\'', src, re.S)
if not m:
    sys.exit("could not extract report script from opencode-prune-report.nix")
body = m.group(1).replace("''${", "${")
# Nix indented strings strip the common leading indentation; the built
# script is de-indented, so extraction must do the same.
lines = body.split("\n")
indents = [len(l) - len(l.lstrip(" ")) for l in lines if l.strip()]
strip = min(indents) if indents else 0
body = "\n".join(l[strip:] if l.strip() else l for l in lines)
# Resolve the two nix interpolations the extraction cannot evaluate.
body = body.replace("${tools}", tools)
body = re.sub(r"\$\{pkgs\.bash\}/bin/bash", "bash", body)
open(out, "w").write(body + "\n")
PYEOF

run_report() { # rest = env assignments
  : > "$work/args"
  env HOME="$work/home" ARGS_LOG="$work/args" "$@" bash "$work/report.sh" > "$work/out" 2>&1
}

# --- both pruners succeed ---------------------------------------------------
run_report
rc=$?
check "report exits 0 when both pruners succeed" "$rc"
check "clean.sh invoked" "$([ "$(grep -c '^clean.sh ' "$work/args")" -ge 1 ]; echo $?)"
check "db backup pruner invoked" "$([ "$(grep -c '^opencode-db-backup-prune.sh ' "$work/args")" -ge 1 ]; echo $?)"
check "clean.sh given --dry-run" "$(grep -q '^clean.sh .*--dry-run' "$work/args"; echo $?)"
check "clean.sh given --no-git-maintenance" "$(grep -q '^clean.sh .*--no-git-maintenance' "$work/args"; echo $?)"
check "pruner given --db-dir" "$(grep -q '^opencode-db-backup-prune.sh .*--db-dir' "$work/args"; echo $?)"
check "no --apply anywhere" "$(! grep -q -- '--apply' "$work/args"; echo $?)"
check "no --prune-unknown-scratch anywhere" "$(! grep -q -- '--prune-unknown-scratch' "$work/args"; echo $?)"
check "report written to the log stream" "$(grep -q 'report complete (rc=0)' "$work/out"; echo $?)"

# --- clean.sh failing does not hide the pruner ------------------------------
run_report STUB_CLEAN_RC=1
rc=$?
check "report exits nonzero when clean.sh fails" "$([ "$rc" -ne 0 ]; echo $?)"
check "pruner still invoked after clean.sh failure" "$([ "$(grep -c '^opencode-db-backup-prune.sh ' "$work/args")" -ge 1 ]; echo $?)"
check "failure is reported in the log" "$(grep -q 'report complete (rc=1)' "$work/out"; echo $?)"

# --- the job definition itself ---------------------------------------------
check "StartCalendarInterval defined" "$(grep -q 'StartCalendarInterval' opencode-prune-report.nix; echo $?)"
check "RunAtLoad is false" "$(grep -q 'RunAtLoad = false;' opencode-prune-report.nix; echo $?)"
check "report log under Library/Logs" "$(grep -q 'Library/Logs/opencode-prune-report.log' opencode-prune-report.nix; echo $?)"
# Comment lines legitimately mention --apply; only executable lines matter.
check "extracted report never passes --apply" "$(! grep -v '^[[:space:]]*#' "$work/report.sh" | grep -q -- '--apply'; echo $?)"

echo
if [ "$failures" -eq 0 ]; then
  echo "all checks passed"
  exit 0
fi
echo "$failures check(s) failed"
exit 1
