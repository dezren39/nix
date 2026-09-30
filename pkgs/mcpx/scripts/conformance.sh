#!/usr/bin/env bash
# Run the official MCP conformance suite against mcpx, as a server and as a client. One command:
#
#   CONFORMANCE_DIR=/path/to/modelcontextprotocol/conformance scripts/conformance.sh [out-dir]
#
# CONFORMANCE_DIR is a clone of github.com/modelcontextprotocol/conformance; it is installed and built on
# first use (that is the only step that needs the network). Everything mcpx-side is isolated in a scratch
# directory and every daemon started is stopped on exit, including on Ctrl-C.
#
# Server leg: builds mcpx and the fakemcp test server, starts `mcpx daemon --port` with fakemcp as its one
# upstream, and runs the server scenarios at /mcp for --requirements 2025-11-25, --requirements 2026-07-28
# and --suite all. The suite has no stdio server mode, so `mcpx serve` over stdio is not covered here.
#
# Client leg: builds internal/conformance/officialclient, the adapter that makes mcpx the client under test
# (see its package doc), and runs the client scenarios for both requirement sets.
#
# Output: <out-dir>/<leg>-<set>/{out.txt,results/...} and <out-dir>/failures.txt, one line per failed check.
set -uo pipefail

: "${CONFORMANCE_DIR:?set CONFORMANCE_DIR to a clone of modelcontextprotocol/conformance}"
HERE=$(cd "$(dirname "$0")/.." && pwd)
OUT=${1:-$PWD/conformance-results}
PORT=${MCPX_CONFORMANCE_PORT:-18731}
LEGS=${LEGS:-"server-2025-11-25 server-2026-07-28 server-all client-2025-11-25 client-2026-07-28"}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/mcpx-conformance.XXXXXX")

cleanup() {
  if [ -n "${DAEMON_PID:-}" ]; then
    MCPX_CONFIG=$WORK/srv/.mcpx.json MCPX_STATE_DIR=$WORK/srv/state MCPX_CACHE_DIR=$WORK/srv/cache \
      "$WORK/mcpx" stop --all >/dev/null 2>&1
    kill "$DAEMON_PID" 2>/dev/null
    wait "$DAEMON_PID" 2>/dev/null
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

if [ ! -f "$CONFORMANCE_DIR/dist/index.js" ]; then
  (cd "$CONFORMANCE_DIR" && npm install --no-audit --no-fund && npm run build) || exit 1
fi
SUITE=(node "$CONFORMANCE_DIR/dist/index.js")

(cd "$HERE" && go build -o "$WORK/mcpx" ./cmd/mcpx &&
  go build -o "$WORK/fakemcp" ./internal/testsupport/fakemcp &&
  go build -o "$WORK/officialclient" ./internal/conformance/officialclient) || exit 1

start_daemon() {
  mkdir -p "$WORK/srv"
  printf '{"mcpServers":{"demo":{"command":"%s","mcpx":{"sharing":"shared","scope":"global"}}}}\n' \
    "$WORK/fakemcp" >"$WORK/srv/.mcpx.json"
  MCPX_CONFIG=$WORK/srv/.mcpx.json MCPX_STATE_DIR=$WORK/srv/state MCPX_CACHE_DIR=$WORK/srv/cache \
    MCPX_REGISTRY_URL=http://127.0.0.1:1/ \
    "$WORK/mcpx" daemon --port "$PORT" >"$OUT/daemon.log" 2>&1 &
  DAEMON_PID=$!
  for _ in $(seq 1 100); do
    curl -sf "http://127.0.0.1:$PORT/v1/health" >/dev/null && return 0
    sleep 0.1
  done
  echo "daemon did not come up; see $OUT/daemon.log" >&2
  exit 1
}

run_leg() { # name, suite args...
  local name=$1; shift
  rm -rf "${OUT:?}/$name"
  mkdir -p "$OUT/$name"
  echo "== $name"
  (cd "$OUT/$name" && "${SUITE[@]}" "$@" -o "$OUT/$name/results" >out.txt 2>&1)
  sed -n '/SUMMARY ===/,$p' "$OUT/$name/out.txt" | tail -n +2
}

CLIENT="$WORK/officialclient -mcpx $WORK/mcpx"
for leg in $LEGS; do
  case $leg in
  server-*) [ -z "${DAEMON_PID:-}" ] && start_daemon ;;
  esac
  case $leg in
  server-all) run_leg "$leg" server --url "http://127.0.0.1:$PORT/mcp" --suite all ;;
  server-*) run_leg "$leg" server --url "http://127.0.0.1:$PORT/mcp" --requirements "${leg#server-}" ;;
  client-*) run_leg "$leg" client --command "$CLIENT" --requirements "${leg#client-}" ;;
  *) echo "unknown leg $leg" >&2 ;;
  esac
done

# One line per non-passing check, across every leg: scenario | check | status | message.
python3 - "$OUT" >"$OUT/failures.txt" <<'EOF'
import json, glob, os, sys
root = sys.argv[1]
for f in sorted(glob.glob(os.path.join(root, '*', 'results', '**', 'checks.json'), recursive=True)):
    leg = os.path.relpath(f, root).split(os.sep)[0]
    scen = os.path.basename(os.path.dirname(f))
    for c in json.load(open(f)):
        if c.get('status') in ('SUCCESS', 'INFO', 'SKIPPED'):
            continue
        msg = (c.get('errorMessage') or '').replace('\n', ' ')
        print(f"{leg} | {scen} | {c.get('id')} | {c.get('status')} | {msg[:400]}")
EOF
echo "failures: $OUT/failures.txt ($(wc -l <"$OUT/failures.txt") lines)"
