#!/usr/bin/env bash
# Regression test for the argument parsers of opencode-db-vacuum.sh and
# opencode-db-prune.sh (#941, the #930 pattern).
#
# A value-taking option must reject an empty or flag-shaped value, dying
# with a message that names the option — otherwise the next option is
# swallowed as the value and never parsed: "opencode-db-vacuum.sh --into
# --dry-run" used to run a REAL vacuum against the live database, because
# --dry-run became the --into path. The missing-value case (option last
# on the line) was already guarded by ${2:?}; this test pins both cases
# for every value-taking option in both scripts, plus controls showing
# valid values still parse.
#
# Run: ./opencode-db-args.test.sh
# It is run by the flake check `opencode-db-args-tests`.
set -u

cd "$(dirname "$0")" || exit

HOME="$(mktemp -d)"
export HOME
trap 'rm -rf "$HOME"' EXIT

failures=0
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }
ok() { echo "ok: $1"; }

# check_rejected <script> <option> <flag-shaped value...>
# The invocation must exit non-zero before doing any work, with stderr
# naming the option.
check_rejected() {
  local script="$1" opt="$2"; shift 2
  local out rc
  out="$(bash "$script" "$opt" "$@" 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "$script $opt $*: exited 0"
  elif ! printf '%s' "$out" | grep -q -- "$opt"; then
    fail "$script $opt $*: error does not name the option: $out"
  else
    ok "$script $opt $* rejected, naming $opt"
  fi
}

VAC=./opencode-db-vacuum.sh
PRUNE=./opencode-db-prune.sh

# Flag-shaped values: the swallowed flag is the dangerous case (#930).
for opt in --db --into; do
  check_rejected "$VAC" "$opt" --dry-run
  check_rejected "$VAC" "$opt"        # no value at all
  check_rejected "$VAC" "$opt" ""     # empty value
done
for opt in --db --protect-days --backup --archive --restore-session --from --batch; do
  check_rejected "$PRUNE" "$opt" --apply
  check_rejected "$PRUNE" "$opt"      # no value at all
  check_rejected "$PRUNE" "$opt" ""   # empty value
done

# Controls: a valid value is consumed and parsing continues to the next
# token (an unknown option after it must be reported as unknown, proving
# the value was not confused with a flag and the flag was not swallowed).
check_consumes_value() {
  local script="$1"
  local out rc
  out="$(bash "$script" --db /tmp/args-test-nonexistent.db --bogus 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ] || ! printf '%s' "$out" | grep -q "unknown option: --bogus"; then
    fail "$script --db <path> --bogus: expected 'unknown option: --bogus', got rc=$rc: $out"
  else
    ok "$script --db consumes a valid value"
  fi
}
check_consumes_value "$VAC"
check_consumes_value "$PRUNE"

# Control: vacuum happy path — a real scratch DB in --dry-run mode exits
# 0 and changes nothing.
if command -v sqlite3 >/dev/null; then
  scratch="$HOME/scratch.db"
  sqlite3 "$scratch" "create table t(x); insert into t values(1);"
  out="$(bash "$VAC" --db "$scratch" --dry-run 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then
    fail "vacuum --dry-run on scratch DB exited $rc: $out"
  else
    ok "vacuum --dry-run happy path"
  fi
fi

if [ "$failures" -ne 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
echo "all argument-parser checks passed"
