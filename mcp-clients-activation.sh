#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: mcp-clients-activation.sh PYTHON MCP_CLIENTS_PY SERVICE_EVENT" >&2
  exit 2
fi

python=$1
mcp_clients=$2
service_event=$3
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/mcp-clients-activation.XXXXXX")
trap 'rm -rf "$tmpdir"' EXIT

set +e
"$python" "$mcp_clients" >"$tmpdir/stdout" 2>"$tmpdir/stderr"
status=$?
set -e

cat "$tmpdir/stdout"
cat "$tmpdir/stderr" >&2

if [ "$status" -eq 0 ]; then
  exit 0
fi

# mcp-clients.py exits non-zero when one or more individual clients could not
# be enforced. Preserve the failed state in the service-events log so the
# switch remains successful without making the partial failure invisible.
failed_clients=$(sed -n \
  's/^mcp-clients: [0-9][0-9]* of [0-9][0-9]* clients not enforced: //p' \
  "$tmpdir/stderr")
if [ "$status" -ne 1 ] || [ -z "$failed_clients" ]; then
  echo "mcp-clients activation: ERROR: mcp-clients exited $status without a recognized enforcement-failure summary; refusing to report success." >&2
  exit "$status"
fi

if ! "$service_event" mcp-clients enforcement.failed "unenforced_clients=$failed_clients"; then
  echo "mcp-clients activation: ERROR: could not durably record unenforced clients: $failed_clients" >&2
  exit 1
fi

echo "mcp-clients activation: WARNING: MCP enforcement failed for: $failed_clients (recorded in service-events)." >&2
