#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 5 ]; then
  echo "usage: system-activation-helper.sh HELPER SCOPE SERVICE_EVENT -- COMMAND [ARG ...]" >&2
  exit 2
fi

helper=$1
scope=$2
service_event=$3
shift 3

case "$scope" in
  root | user) ;;
  *)
    echo "system-activation-helper: SCOPE must be root or user" >&2
    exit 2
    ;;
esac

if [ "$1" != "--" ]; then
  echo "system-activation-helper: expected -- before helper command" >&2
  exit 2
fi
shift
if [ "$#" -eq 0 ]; then
  echo "system-activation-helper: COMMAND must not be empty" >&2
  exit 2
fi

# Keep helper output and its exit status intact. A helper failure is a
# recoverable partial-activation warning only after the service-events log has
# durably recorded which helper and scope failed.
set +e
"$@"
status=$?
set -e

if [ "$status" -eq 0 ]; then
  exit 0
fi

if ! "$service_event" system-activation helper.failed \
  "helper=$helper" "scope=$scope" "exit_status=$status"; then
  echo "system activation: ERROR: could not durably record $helper failure (scope=$scope exit_status=$status)." >&2
  exit 1
fi

echo "system activation: WARNING: $helper failed for $scope scope (exit $status; recorded in service-events)." >&2
