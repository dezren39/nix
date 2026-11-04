#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
wrapper="$script_dir/mcp-clients-activation.sh"
mcp_clients="$script_dir/mcp-clients.py"
python=${PYTHON:-python3}
tmp=$(mktemp -d "${TMPDIR:-/tmp}/mcp-clients-activation-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/bin"
printf '#!%s\n' "$(command -v bash)" >"$tmp/bin/service-event"
cat >>"$tmp/bin/service-event" <<'STUB'
set -euo pipefail
printf '%s\n' "$*" >>"$EVENT_LOG"
if [ "${FAIL_SERVICE_EVENT:-0}" = 1 ]; then
  exit 1
fi
STUB
chmod +x "$tmp/bin/service-event"

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

seed() {
  local home=$1 rel=$2 contents=$3
  mkdir -p "$home/$(dirname -- "$rel")"
  printf '%s' "$contents" >"$home/$rel"
}

run_wrapper() {
  local home=$1 event_log=$2 stdout=$3 stderr=$4 fail_event=${5:-0}
  env HOME="$home" EVENT_LOG="$event_log" FAIL_SERVICE_EVENT="$fail_event" \
    PATH="$tmp/bin:$PATH" bash "$wrapper" "$python" "$mcp_clients" service-event \
    >"$stdout" 2>"$stderr"
}

# All clients are valid: the real tool enforces them, and no failure event is
# emitted.
home="$tmp/all-good"
seed "$home" .copilot/mcp-config.json '{"mcpServers":{"other":{"command":"x"}}}'
seed "$home" .claude.json '{"mcpServers":{"other":{"command":"x"}}}'
seed "$home" .codex/config.toml $'[mcp_servers.other]\ncommand = "x"\n'
seed "$home" .config/zed/settings.json '{"theme":"Dark"}'
if run_wrapper "$home" "$tmp/all-good.events" "$tmp/all-good.out" "$tmp/all-good.err"; then
  status=0
else
  status=$?
fi
check "all-good wrapper exits 0" test "$status" -eq 0
check "all-good records no failure event" test ! -e "$tmp/all-good.events"
check "all-good enforces Copilot" contains '"mcpx"' "$home/.copilot/mcp-config.json"
check "all-good enforces Claude" contains '"mcpx"' "$home/.claude.json"

# A malformed Copilot config does not prevent the real tool from enforcing
# Claude; the wrapper records the exact failed client and exits successfully.
home="$tmp/partial"
seed "$home" .copilot/mcp-config.json '{not json'
seed "$home" .claude.json '{"mcpServers":{"foreign":{"command":"x"}}}'
if run_wrapper "$home" "$tmp/partial.events" "$tmp/partial.out" "$tmp/partial.err"; then
  status=0
else
  status=$?
fi
if [ "$status" -ne 0 ]; then
  echo "partial wrapper stderr:" >&2
  cat "$tmp/partial.err" >&2
fi
check "partial enforcement is recorded and wrapper exits 0" test "$status" -eq 0
check "event names source, event, and Copilot CLI" contains \
  'mcp-clients enforcement.failed unenforced_clients=Copilot CLI' "$tmp/partial.events"
check "warning names Copilot CLI" contains \
  'WARNING: MCP enforcement failed for: Copilot CLI' "$tmp/partial.err"
check "original failure summary is forwarded" contains \
  '1 of 4 clients not enforced: Copilot CLI' "$tmp/partial.err"
check "Claude remains enforced despite Copilot failure" contains \
  '"mcpx"' "$home/.claude.json"
check "Claude foreign server was removed" not_contains \
  'foreign' "$home/.claude.json"

# If the durable event cannot be written, activation must still fail.
home="$tmp/record-fails"
seed "$home" .copilot/mcp-config.json '{not json'
seed "$home" .claude.json '{"mcpServers":{"foreign":{"command":"x"}}}'
if run_wrapper "$home" "$tmp/record-fails.events" "$tmp/record-fails.out" \
  "$tmp/record-fails.err" 1; then
  status=0
else
  status=$?
fi
check "event write failure makes wrapper exit non-zero" test "$status" -ne 0
check "event write failure is diagnosed" contains \
  'could not durably record unenforced clients: Copilot CLI' "$tmp/record-fails.err"

if [ "$failures" -ne 0 ]; then
  echo "$failures mcp-clients activation test(s) failed" >&2
  exit 1
fi
echo "All mcp-clients activation tests passed"
