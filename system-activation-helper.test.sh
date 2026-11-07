#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
wrapper="$script_dir/system-activation-helper.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/system-activation-helper-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/bin"
for event_runner in service-event service-event-as-user; do
  printf '#!%s\n' "$(command -v bash)" >"$tmp/bin/$event_runner"
  cat >>"$tmp/bin/$event_runner" <<'STUB'
set -euo pipefail
case "${0##*/}" in
  service-event) identity=user ;;
  service-event-as-user) identity=primary-user-context ;;
esac
if [ "$FAIL_SERVICE_EVENT" = 1 ]; then
  exit 1
fi
echo "$identity $*" >>"$EVENT_LOG"
STUB
  chmod +x "$tmp/bin/$event_runner"
done

failures=0
check() {
  local name=$1
  shift
  if "$@"; then
    echo "PASS: $name"
  else
    echo "FAIL: $name" >&2
    failures=$((failures + 1))
  fi
}

contains() {
  grep -Fq -- "$1" "$2"
}

not_contains() {
  ! grep -Fq -- "$1" "$2"
}

run_wrapper() {
  local event_log=$1 fail_event=$2 helper=$3 scope=$4 event_runner=$5
  shift 5
  env EVENT_LOG="$event_log" FAIL_SERVICE_EVENT="$fail_event" \
    bash "$wrapper" "$helper" "$scope" "$event_runner" -- "$@"
}

# Successful helpers leave their output and result untouched and record no
# failure in either logging context.
for helper in spotlight-exclude-artifacts git-maintain-repos; do
  for scope in root user; do
    if [ "$scope" = root ]; then
      event_runner="$tmp/bin/service-event-as-user"
    else
      event_runner="$tmp/bin/service-event"
    fi
    case_name="$helper-$scope"
    if run_wrapper "$tmp/success-$case_name.events" 0 "$helper" "$scope" "$event_runner" \
      bash -c 'echo helper stdout; echo helper stderr >&2' \
      >"$tmp/success-$case_name.out" 2>"$tmp/success-$case_name.err"; then
      status=0
    else
      status=$?
    fi
    check "$case_name success exits 0" test "$status" -eq 0
    check "$case_name success preserves stdout" contains 'helper stdout' "$tmp/success-$case_name.out"
    check "$case_name success preserves stderr" contains 'helper stderr' "$tmp/success-$case_name.err"
    check "$case_name success adds no warning" not_contains 'system activation:' "$tmp/success-$case_name.err"
    check "$case_name success records no event" test ! -e "$tmp/success-$case_name.events"
  done
done

# Each helper/scope pair records the failure through the correct logger. Root
# events must reach the primary user's monitored log; successful writes let
# activation continue to later entries.
status_code=7
for helper in spotlight-exclude-artifacts git-maintain-repos; do
  for scope in root user; do
    case_name="$helper-$scope"
    if [ "$scope" = root ]; then
      event_runner="$tmp/bin/service-event-as-user"
      logger_identity=primary-user-context
    else
      event_runner="$tmp/bin/service-event"
      logger_identity=user
    fi
    if run_wrapper "$tmp/failure-$case_name.events" 0 "$helper" "$scope" "$event_runner" \
      bash -c "echo helper failed >&2; exit $status_code" \
      >"$tmp/failure-$case_name.out" 2>"$tmp/failure-$case_name.err"; then
      status=0
    else
      status=$?
    fi
    check "$case_name failure is recoverable after recording" test "$status" -eq 0
    check "$case_name event records logger, helper, scope and status" contains \
      "$logger_identity system-activation helper.failed helper=$helper scope=$scope exit_status=$status_code" \
      "$tmp/failure-$case_name.events"
    check "$case_name helper diagnostic is preserved" contains 'helper failed' "$tmp/failure-$case_name.err"
    check "$case_name warning names recorded failure" contains 'recorded in service-events' "$tmp/failure-$case_name.err"
    status_code=$((status_code + 1))
  done
done

# A logging failure must not turn an unrecorded helper failure into a green
# system activation.
if run_wrapper "$tmp/log-failure.events" 1 git-maintain-repos root "$tmp/bin/service-event-as-user" \
  bash -c 'exit 3' >"$tmp/log-failure.out" 2>"$tmp/log-failure.err"; then
  status=0
else
  status=$?
fi
check "event-write failure fails activation" test "$status" -ne 0
check "event-write failure is diagnosed" contains \
  'could not durably record git-maintain-repos failure (scope=root exit_status=3)' \
  "$tmp/log-failure.err"
check "event-write failure is not reported as recorded" test ! -e "$tmp/log-failure.events"

if [ "$failures" -ne 0 ]; then
  echo "$failures system activation helper test(s) failed" >&2
  exit 1
fi
echo "All system activation helper tests passed"
