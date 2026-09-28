#!/usr/bin/env bash
# Stress mcpx against the real MCP servers configured in .mcpx.json.
#
# Checks three things the lootbox setup got wrong:
#   1. discovery stays fast under sustained load,
#   2. concurrent runs against a stateful server stay isolated, repeatedly,
#   3. nothing leaks: no orphan browsers, no unbounded instance growth.
set -uo pipefail

MCPX=${MCPX:?set MCPX to the mcpx binary}
ROUNDS=${ROUNDS:-5}
CONC=${CONC:-3}
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail=0
note() { printf '%s\n' "$*"; }
check() {
  if [ "$1" = 0 ]; then note "  ok   $2"; else note "  FAIL $2"; fail=1; fi
}

note "== discovery latency under load =="
for i in $(seq 1 30); do "$MCPX" ls >/dev/null 2>&1 & done
wait
start=$(python3 -c 'import time;print(time.time())')
for i in $(seq 1 20); do "$MCPX" ls >/dev/null 2>&1; done
end=$(python3 -c 'import time;print(time.time())')
per=$(python3 -c "print(($end-$start)/20*1000)")
note "  ${per}ms per \`mcpx ls\` (20 calls after 30 concurrent)"
python3 -c "import sys; sys.exit(0 if $per < 200 else 1)"
check $? "discovery stays under 200ms"

note "== concurrent isolation, $ROUNDS rounds of $CONC =="
cat > "$work/iso.ts" <<'TS'
import tools from "./mcpx-client.ts";
const url = Deno.args[0];
await tools.chrome_devtools.new_page({ url });
const pages = String(await tools.chrome_devtools.list_pages({}));
const seen = [...new Set([...pages.matchAll(/https?:\/\/[^\s")']+/g)].map((m) => m[0]))];
console.log(JSON.stringify({ url, seen }));
TS

urls=(https://example.com https://www.iana.org https://httpbin.org/html)
leaks=0
for round in $(seq 1 "$ROUNDS"); do
  pids=()
  for i in $(seq 0 $((CONC - 1))); do
    u=${urls[$((i % ${#urls[@]}))]}
    ( "$MCPX" run "$work/iso.ts" "$u" > "$work/r-$round-$i.json" 2>"$work/e-$round-$i.log" ) &
    pids+=($!)
  done
  for p in "${pids[@]}"; do wait "$p" || true; done

  for i in $(seq 0 $((CONC - 1))); do
    line=$(grep -h '^{' "$work/r-$round-$i.json" 2>/dev/null | tail -1)
    if [ -z "$line" ]; then
      note "  round $round run $i produced no result:"
      tail -3 "$work/e-$round-$i.log" | sed 's/^/      /'
      leaks=$((leaks + 1))
      continue
    fi
    python3 - "$line" <<'PY' || leaks=$((leaks + 1))
import json, sys
d = json.loads(sys.argv[1])
own = d["url"].rstrip("/")
seen = [s.rstrip("/)") for s in d["seen"]]
if len(seen) != 1 or not seen[0].startswith(own.split("://")[0] + "://" + own.split("://")[1].split("/")[0]):
    print(f"      LEAK: run for {d['url']} saw {d['seen']}")
    sys.exit(1)
PY
  done
done
check "$leaks" "no cross-run state leaks across $((ROUNDS * CONC)) runs"

note "== instance accounting =="
live=$("$MCPX" --json status | python3 -c '
import json,sys
d=json.load(sys.stdin)
print(sum(s["live"] for s in d["servers"]))')
note "  $live live instances"
maxsum=$("$MCPX" --json status | python3 -c '
import json,sys
d=json.load(sys.stdin)
print(sum(s["max"] for s in d["servers"]))')
[ "$live" -le "$maxsum" ]
check $? "live instances ($live) within configured maxima ($maxsum)"

note "== orphan check =="
"$MCPX" restart chrome-devtools >/dev/null
sleep 2
orphans=$(pgrep -f 'chrome-devtools-mcp' | wc -l | tr -d ' ')
note "  $orphans chrome-devtools-mcp processes after restart"
[ "$orphans" -le 1 ]
check $? "no orphaned chrome-devtools-mcp processes"

note "== repeated restart stability =="
for i in 1 2 3; do
  "$MCPX" restart >/dev/null 2>&1 || { check 1 "restart $i"; break; }
  "$MCPX" call fff_nix.find_files '{"query":"flake.nix","maxResults":1}' >/dev/null 2>&1
  check $? "call succeeds after restart $i"
done

exit "$fail"
