---
name: mcpx-observability
description: Use when investigating why an MCP call was slow, failed, or behaved oddly - reading mcpx's durable log, following a trace chain, or aggregating statistics. Also for questions about cost and token use across sessions.
---

# Finding out what happened

mcpx keeps a durable log of every call, every server start and stop, and
everything scripts wrote. It is on disk, indexed, and queryable. Nothing here
re-runs anything.

## Start with the shape of the problem

| question | command |
| --- | --- |
| what is slow | `mcpx stats calls` |
| what keeps failing | `mcpx stats errors` |
| is a server unstable | `mcpx stats servers` |
| which process died | `mcpx stats instances` |
| what happened just now | `mcpx log --since 5m` |
| what led to this call | `mcpx log --chain <trace>` |

`mcpx stats calls` is ordered by **total time spent**, not by count, because
the question behind it is "what is costing me" and a rare ten-second tool
beats a fast one called constantly.

## The trace chain

Every record carries a `trace`, and most carry `trace.parent`. `--chain` walks
it: from a tool call, back through the server instance that served it, back to
the daemon that started that instance. It is a tree, oldest first.

This answers "why did this call go to a cold server" without guessing.

## When the flags are not enough

```sh
mcpx log sql "SELECT server, tool, COUNT(*), AVG(duration_ms)
              FROM records WHERE event='mcp.call' GROUP BY 1,2"
```

Read-only. `mcpx log sql --schema` prints the tables.

## Harness statistics

`mcpx stats --opencode` reads opencode's own database: cost, tokens, which
model answered, which agent ran. Those numbers are pruned over time, so they
are worth asking for before they are gone.

`mcpx stats --opencode agents` and `... models` group them.

## Interactively

`mcpx tui` puts the log, the statistics, the server processes and what it all
costs on disk into one screen. Enter opens a record.
