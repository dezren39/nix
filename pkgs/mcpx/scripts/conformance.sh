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
# The tasks-extension scenarios (src/scenarios/server/tasks/*.ts) ask for slow_compute, failing_job, greet,
# confirm_delete, multi_input, protocol_error_job and test_tool_with_task. The suite's everything-server does not
# define them, so mcpx's own fixture, internal/testsupport/taskmcp, provides exactly those, and both upstreams are
# offered through one /mcp: --passthrough takes a list and merges their surfaces.
#
# Client leg: builds internal/conformance/officialclient, the adapter that makes mcpx the client under test
# (see its package doc), and runs the client scenarios for both requirement sets.
#
# Revision precedence (#307, internal/spec): every per-revision leg runs mcpx with that revision first --
# `mcpx daemon --mcp-spec <rev>` for a server leg, MCPX_MCP_SPEC=<rev> for a client leg, which the adapter's mcpx
# and any daemon it starts inherit. server-all sets nothing and measures the defaults. A server leg therefore
# gets a daemon of its own, started for the leg and stopped after it.
#
# Output: <out-dir>/<leg>-<set>/{out.txt,results/...} and <out-dir>/failures.txt, one line per failed check.
set -uo pipefail

: "${CONFORMANCE_DIR:?set CONFORMANCE_DIR to a clone of modelcontextprotocol/conformance}"
HERE=$(cd "$(dirname "$0")/.." && pwd)
OUT=${1:-$PWD/conformance-results}
# Three consecutive free ports. A fixed default let two runs on one machine
# collide: the second daemon failed to bind, and the suite tested the first
# run's server without saying so, which invalidated a measurement before
# anyone noticed. MCPX_CONFORMANCE_PORT still pins the base when you want it.
free_port_base() {
  local base
  for base in $(seq 18731 20 19500); do
    if ! { exec 3<>/dev/tcp/127.0.0.1/"$base"; } 2>/dev/null &&
      ! { exec 3<>/dev/tcp/127.0.0.1/"$((base + 1))"; } 2>/dev/null &&
      ! { exec 3<>/dev/tcp/127.0.0.1/"$((base + 2))"; } 2>/dev/null; then
      echo "$base"
      return 0
    fi
    exec 3>&- 2>/dev/null
  done
  echo "no free port triple in 18731..19500" >&2
  return 1
}
PORT=${MCPX_CONFORMANCE_PORT:-$(free_port_base)} || exit 1
FIXTURE_PORT=$((PORT + 1))
LEGACY_PORT=$((PORT + 2))
echo "ports: daemon $PORT, fixture $FIXTURE_PORT, legacy $LEGACY_PORT"
FIXTURE_DIR=$CONFORMANCE_DIR/examples/servers/typescript
LEGS=${LEGS:-"server-2025-03-26 server-2025-06-18 server-2025-11-25 server-2026-07-28 server-all client-2025-03-26 client-2025-06-18 client-2025-11-25 client-2026-07-28"}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/mcpx-conformance.XXXXXX")

DAEMON_PIDS=()
cleanup() {
  for dir in "$WORK"/server-*; do
    [ -d "$dir" ] && MCPX_CONFIG=$dir/.mcpx.json MCPX_STATE_DIR=$dir/state \
      MCPX_CACHE_DIR=$dir/cache "$WORK/mcpx" stop --all >/dev/null 2>&1
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
  go build -o "$WORK/officialclient" ./internal/conformance/officialclient &&
  go build -o "$WORK/taskmcp" ./internal/testsupport/taskmcp) || exit 1

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

# start_daemon <era> <port> <dir> [rev]: a daemon fronting the fixture over one protocol era, end to end. The
# modern daemon uses protocol: follow, so a caller on a pre-2026 revision gets a legacy upstream
# session and the fixture's server-to-client requests (elicitation, sampling) reach it. Without it
# the --suite all leg fails four scenarios that pass in the 2025-11-25 leg. The
# 2025-11-25 leg gets a daemon that speaks 2025-11-25 to the fixture too: the fixture's legacy tools
# (test_elicitation, test_sampling, ...) push requests to their client, which only a legacy session can
# carry -- over 2026-07-28 the fixture itself answers them -32601 -- and mcpx relays what arrives.
start_daemon() { # era port dir [mcp-spec]
  local era=$1 port=$2 dir=$3 first=${4:-} protocol=follow
  local specflag=()
  [ -n "$first" ] && specflag=(--mcp-spec "$first")
  [ "$era" = legacy ] && protocol=force-initialize
  [ -n "${FIXTURE_PID:-}" ] || start_fixture
  mkdir -p "$WORK/$dir"
  printf '{"mcpServers":{"demo":{"url":"http://127.0.0.1:%s/mcp","protocol":"%s","mcpx":{"sharing":"shared","scope":"global"}},"tasks":{"command":"%s","mcpx":{"sharing":"shared","scope":"global"}}}}\n' \
    "$FIXTURE_PORT" "$protocol" "$WORK/taskmcp" >"$WORK/$dir/.mcpx.json"
  MCPX_CONFIG=$WORK/$dir/.mcpx.json MCPX_STATE_DIR=$WORK/$dir/state MCPX_CACHE_DIR=$WORK/$dir/cache \
    MCPX_REGISTRY_URL=http://127.0.0.1:1/ \
    "$WORK/mcpx" daemon --port "$port" --passthrough demo,tasks "${specflag[@]}" >"$OUT/daemon-$dir.log" 2>&1 &
  DAEMON_PIDS+=($!)
  local pid=${DAEMON_PIDS[-1]}
  for _ in $(seq 1 100); do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "daemon exited at once (port $port already in use?); see $OUT/daemon-$dir.log" >&2
      exit 1
    fi
    # Ours, not whatever else is listening: the health answer carries the pid.
    got=$(curl -sf "http://127.0.0.1:$port/v1/health" 2>/dev/null) || { sleep 0.1; continue; }
    case $got in
    *"\"pid\":$pid"*) return 0 ;;
    *) echo "port $port answers, but not our daemon (pid $pid): $got" >&2; exit 1 ;;
    esac
  done
  echo "daemon did not come up; see $OUT/daemon-$dir.log" >&2
  exit 1
}

# stop_daemon <dir>: stop the daemon a leg started, so the next leg's --mcp-spec takes effect. Each leg
# has its own state directory: a daemon restarted over the previous leg's state served no upstream
# prompts (prompts-get-* and completion-complete failed in server-2025-06-18 and -11-25), so sharing it
# would measure that, not the leg.
stop_daemon() {
  local dir=$1
  MCPX_CONFIG=$WORK/$dir/.mcpx.json MCPX_STATE_DIR=$WORK/$dir/state MCPX_CACHE_DIR=$WORK/$dir/cache \
    "$WORK/mcpx" stop --all >/dev/null 2>&1
  local pid=${DAEMON_PIDS[-1]}
  kill "$pid" 2>/dev/null
  wait "$pid" 2>/dev/null
  unset 'DAEMON_PIDS[-1]'
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
  rev=${leg#*-} # server-2025-11-25 -> 2025-11-25; server-all -> all
  [ "$rev" = all ] && rev=
  case $leg in
  server-2025-11-25 | server-2025-06-18 | server-2025-03-26) start_daemon legacy "$LEGACY_PORT" "$leg" "$rev" ;;
  server-*) start_daemon modern "$PORT" "$leg" "$rev" ;;
  esac
  case $leg in
  server-all) run_leg "$leg" server --url "http://127.0.0.1:$PORT/mcp" --suite all ;;
  server-2025-11-25) run_leg "$leg" server --url "http://127.0.0.1:$LEGACY_PORT/mcp" --requirements 2025-11-25 ;;
  # The suite has no frozen requirement set for these two, only the scenarios
  # tagged for each version, which is what --spec-version selects.
  server-2025-06-18 | server-2025-03-26)
    run_leg "$leg" server --url "http://127.0.0.1:$LEGACY_PORT/mcp" --spec-version "$rev" ;;
  server-*) run_leg "$leg" server --url "http://127.0.0.1:$PORT/mcp" --requirements "$rev" ;;
  # As on the server side, the suite has frozen requirement sets only for
  # 2025-11-25 and 2026-07-28; the two older revisions run their tagged
  # scenarios.
  client-2025-06-18 | client-2025-03-26)
    MCPX_MCP_SPEC=$rev run_leg "$leg" client --command "$CLIENT" --suite all --spec-version "$rev" ;;
  client-*) MCPX_MCP_SPEC=$rev run_leg "$leg" client --command "$CLIENT" --requirements "$rev" ;;
  *) echo "unknown leg $leg" >&2 ;;
  esac
  case $leg in
  server-*) stop_daemon "$leg" ;;
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
