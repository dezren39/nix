#!/usr/bin/env bash
# Head-to-head benchmark: lootbox vs mcpx, on the same machine and the same
# set of MCP servers.
#
# Both tools are asked to do the same three things an agent actually does:
#   1. list namespaces (pure discovery),
#   2. fetch the signatures for one namespace,
#   3. run a trivial script that makes one real tool call.
set -uo pipefail

LOOTBOX=${LOOTBOX:-lootbox}
MCPX=${MCPX:?set MCPX to the mcpx binary}
N=${N:-5}

now() { python3 -c 'import time;print(time.time())'; }

bench() { # name, count, command...
  local name=$1 count=$2; shift 2
  local start end
  start=$(now)
  local ok=0
  for _ in $(seq 1 "$count"); do
    if "$@" >/dev/null 2>&1; then ok=$((ok + 1)); fi
  done
  end=$(now)
  python3 -c "
per = ($end - $start) / $count
print(f'{\"$name\":<34} {per*1000:9.1f} ms   ({$ok}/$count ok)')"
}

echo "=== discovery: list namespaces (x$N) ==="
bench "lootbox tools" "$N" "$LOOTBOX" tools
bench "mcpx ls" "$N" "$MCPX" ls

echo
echo "=== discovery: types for one namespace (x$N) ==="
bench "lootbox tools types mcp_fff" "$N" "$LOOTBOX" tools types mcp_fff
bench "mcpx types fff" "$N" "$MCPX" types fff

echo
echo "=== execute a trivial script, no tool call (x$N) ==="
bench "lootbox exec" "$N" "$LOOTBOX" exec 'console.log(1)'
bench "mcpx exec" "$N" "$MCPX" exec 'console.log(1)'

echo
echo "=== execute a script with one real tool call (x$N) ==="
bench "lootbox exec + fff" "$N" "$LOOTBOX" exec \
  'console.log(await tools.mcp_fff_nix.find_files({ query: "flake.nix", maxResults: 1 }))'
bench "mcpx exec + fff" "$N" "$MCPX" exec \
  'console.log(await fff_nix.find_files({ query: "flake.nix", maxResults: 1 }))'

echo
echo "=== one-shot tool call without any JS runtime (x$N) ==="
bench "mcpx call" "$N" "$MCPX" call fff_nix.find_files '{"query":"flake.nix","maxResults":1}'
echo "(lootbox has no equivalent: every call goes through a Deno process)"

echo
echo "=== artifact size ==="
for f in "$(command -v "$LOOTBOX" || true)" "$MCPX"; do
  [ -n "$f" ] && [ -f "$f" ] && ls -lh "$f" | awk '{printf "%-34s %9s\n", $NF, $5}'
done
