#!/usr/bin/env bash
# Run the official MCP conformance suite against mcpx, as a server and as a client. One command:
#
#   CONFORMANCE_DIR=/path/to/modelcontextprotocol/conformance scripts/conformance.sh [out-dir]
#
# CONFORMANCE_DIR is a clone of github.com/modelcontextprotocol/conformance; it is installed and built on
# first use (that is the only step that needs the network). Everything mcpx-side is isolated in a scratch
# directory and every daemon started is stopped on exit, including on Ctrl-C.
#
# Server leg: starts the suite's own reference fixture server (examples/servers/typescript/everything-server.ts,
# run with tsx exactly as its package.json `start` script does) and puts `mcpx daemon --port` in front of it as
# its one upstream, with --passthrough so mcpx offers the fixture's tools, prompts and resources under their
# own names. The scenarios therefore reach mcpx's protocol handling rather than stopping at "no tool named".
# Runs at /mcp for --requirements 2025-11-25, --requirements 2026-07-28 and --suite all. The suite has no stdio
# server mode, so `mcpx serve` over stdio is not covered here.
#
# The tasks-extension scenarios (src/scenarios/server/tasks/*.ts) ask for slow_compute, failing_job, greet and
# protocol_error_job. No fixture in this repository defines them -- everything-server.ts does not -- they come
# from each SDK's own conformance server. They fail here as "no tool named", honestly: mcpx forwards a tool
# call to its upstream and does not invent fixtures.
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
FIXTURE_PORT=$((PORT + 1))
LEGACY_PORT=$((PORT + 2))
FIXTURE_DIR=$CONFORMANCE_DIR/examples/servers/typescript
LEGS=${LEGS:-"server-2025-11-25 server-2026-07-28 server-all client-2025-11-25 client-2026-07-28"}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/mcpx-conformance.XXXXXX")

DAEMON_PIDS=()
cleanup() {
  for era in modern legacy; do
    [ -d "$WORK/$era" ] && MCPX_CONFIG=$WORK/$era/.mcpx.json MCPX_STATE_DIR=$WORK/$era/state \
      MCPX_CACHE_DIR=$WORK/$era/cache "$WORK/mcpx" stop --all >/dev/null 2>&1
  done
  for pid in "${DAEMON_PIDS[@]}"; do
    kill "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
  done
  if [ -n "${FIXTURE_PID:-}" ]; then
    kill "$FIXTURE_PID" 2>/dev/null
    wait "$FIXTURE_PID" 2>/dev/null
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

if [ ! -f "$CONFORMANCE_DIR/dist/index.js" ]; then
  (cd "$CONFORMANCE_DIR" && npm install --no-audit --no-fund && npm run build) || exit 1
fi
if [ ! -x "$FIXTURE_DIR/node_modules/.bin/tsx" ]; then
  (cd "$FIXTURE_DIR" && npm install --no-audit --no-fund) || exit 1
fi
SUITE=(node "$CONFORMANCE_DIR/dist/index.js")

(cd "$HERE" && go build -o "$WORK/mcpx" ./cmd/mcpx &&
  go build -o "$WORK/officialclient" ./internal/conformance/officialclient) || exit 1

start_fixture() {
  (cd "$FIXTURE_DIR" && PORT=$FIXTURE_PORT exec node_modules/.bin/tsx everything-server.ts) \
    >"$OUT/fixture.log" 2>&1 &
  FIXTURE_PID=$!
  for _ in $(seq 1 300); do
    grep -q "running on" "$OUT/fixture.log" 2>/dev/null && return 0
    sleep 0.1
  done
  echo "fixture server did not come up; see $OUT/fixture.log" >&2
  exit 1
}

# start_daemon <era> <port>: a daemon fronting the fixture over one protocol era, end to end. The
# 2025-11-25 leg gets a daemon that speaks 2025-11-25 to the fixture too: the fixture's legacy tools
# (test_elicitation, test_sampling, ...) push requests to their client, which only a legacy session can
# carry -- over 2026-07-28 the fixture itself answers them -32601 -- and mcpx relays what arrives.
start_daemon() {
  local era=$1 port=$2 protocol=modern
  [ "$era" = legacy ] && protocol=force-legacy
  [ -n "${FIXTURE_PID:-}" ] || start_fixture
  mkdir -p "$WORK/$era"
  printf '{"mcpServers":{"demo":{"url":"http://127.0.0.1:%s/mcp","protocol":"%s","mcpx":{"sharing":"shared","scope":"global"}}}}\n' \
    "$FIXTURE_PORT" "$protocol" >"$WORK/$era/.mcpx.json"
  MCPX_CONFIG=$WORK/$era/.mcpx.json MCPX_STATE_DIR=$WORK/$era/state MCPX_CACHE_DIR=$WORK/$era/cache \
    MCPX_REGISTRY_URL=http://127.0.0.1:1/ \
    "$WORK/mcpx" daemon --port "$port" --passthrough demo >"$OUT/daemon-$era.log" 2>&1 &
  DAEMON_PIDS+=($!)
  for _ in $(seq 1 100); do
    curl -sf "http://127.0.0.1:$port/v1/health" >/dev/null && return 0
    sleep 0.1
  done
  echo "daemon did not come up; see $OUT/daemon-$era.log" >&2
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
  server-2025-11-25) [ -n "${LEGACY_UP:-}" ] || { start_daemon legacy "$LEGACY_PORT"; LEGACY_UP=1; } ;;
  server-*) [ -n "${MODERN_UP:-}" ] || { start_daemon modern "$PORT"; MODERN_UP=1; } ;;
  esac
  case $leg in
  server-all) run_leg "$leg" server --url "http://127.0.0.1:$PORT/mcp" --suite all ;;
  server-2025-11-25) run_leg "$leg" server --url "http://127.0.0.1:$LEGACY_PORT/mcp" --requirements 2025-11-25 ;;
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
