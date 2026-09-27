# Proposals

Open design questions and answers. Nothing here is committed to; the point is
to make the decisions visible before they are made. Shipped work moves to
[`story.md`](./story.md) under the feature list.

---

## Answers about what exists today

Before proposing anything, the current state, verified in the code.

**Shared servers are started lazily and *do* shut down.** Nothing starts at
daemon boot except the schema warm, which starts each server once to read
`tools/list` and lets it idle out. `ReapIdle` runs on a 30-second timer against
each server's `idleTimeout` (default 5m), and a shared instance is reaped when
`len(instances) > Min`, where `Min` defaults to 0. So the steady state for an
unused server is zero processes, and the next call pays a cold start.

**Today's `session` mode is per-script-run.** `mcpx run` mints a fresh key per
invocation and releases it when the process exits. `--session <key>` overrides
it, which means "named" already half-exists. Nothing is aware of an opencode
session, a repo, or a worktree.

**There are no ports.** stdio servers are pipes; only `url` servers have a
port and it is theirs. The predecessor's port-assignment machinery was deleted
on purpose.

**Config is first-file-wins with no merge.** A project `.mcpx.json` replaces
the user config entirely rather than adding to it. See below — this is wrong.

**Server-provided `instructions` are parsed and discarded.** `initialize`
returns an `instructions` string; `mcpx` reads it into a struct field nothing
consumes. Descriptions and namespaces come only from config.

**Scripts have no declared contract.** stdout and stderr both pass through,
the exit code propagates, and there is no log helper, no structured return, and
no entry-point convention.

---

## Modes and scopes

```
status:  proposed
impact:  breaking config change, with a compatibility shim
```

The current single `mode` axis conflates two independent questions. The
proposal is to split them.

**`sharing`** — how many callers may use one process at a time.

| value | meaning |
| --- | --- |
| `shared` | many concurrent callers per process; MCP multiplexes by JSON-RPC id |
| `exclusive` | one caller at a time; others queue |

**`scope`** — what key selects a process. One process per distinct key value.

| scope | key | who computes it |
| --- | --- | --- |
| `global` | constant | mcpx |
| `repo` | `git rev-parse --git-common-dir` | mcpx |
| `worktree` | `git rev-parse --show-toplevel` | mcpx |
| `cwd` | working directory | mcpx |
| `session` | `$MCPX_SESSION_ID`, else the run key | **caller** |
| `parent-session` | `$MCPX_PARENT_SESSION_ID`, else `$MCPX_SESSION_ID` | **caller** |
| `call` | fresh per invocation | mcpx |
| `named:<x>` | the literal string | caller |
| `pid:<n>` | the pid | caller |

Today's modes become aliases, so existing configs keep working:

| today | becomes |
| --- | --- |
| `shared` | `sharing: shared`, `scope: global` |
| `pooled` | `sharing: exclusive`, `scope: call` |
| `session` | `sharing: exclusive`, `scope: call` |

That last row is the honest mapping: today's `session` **is** per-call.

### The part mcpx cannot do alone

`scope: session` needs an identifier mcpx has no way to discover. A subagent
and its parent are different processes with different pids and the same cwd;
only the agent host knows they differ.

So the contract is an environment variable. Callers that know their session
export `MCPX_SESSION_ID` and, if nested, `MCPX_PARENT_SESSION_ID`. Everything
else — `repo`, `worktree`, `cwd`, `call` — mcpx computes itself and works with
no cooperation.

For opencode specifically that means either a plugin that adds the variable to
the shell environment (v1 `shell.env`, v2 `shell.hook["create.before"]`), or an
instruction telling the agent to pass `--session`. The plugin is better because
it cannot be forgotten.

**Recommendation for the current servers:** `chrome-devtools` at
`sharing: exclusive, scope: session` once the variable exists, `scope: call`
until then. Everything else stays `sharing: shared, scope: global`.

### Lifetime

A scope needs a teardown rule or it leaks.

| scope | released when |
| --- | --- |
| `call` | the process exits (today) |
| `cwd`, `repo`, `worktree`, `global` | idle timeout |
| `session`, `named` | idle timeout, or `mcpx session stop <key>` |
| `pid` | the watched pid exits |

`pid:<n>` is worth building early and is cheap: poll `kill(pid, 0)` on the
existing 30-second reaper tick. It gives an agent host a way to say "this
browser belongs to that process" without any protocol.

---

## Session introspection

```
status:  proposed
depends: modes and scopes
```

A lease should be an addressable object.

```
mcpx session ls                     scope, key, server, pid, uptime, idle, calls
mcpx session show <key>
mcpx session stop <key>
mcpx session rename <key> <name>
mcpx session set <key> --idle-timeout 30m
mcpx session watch <key>            follow its calls live
```

Recorded per lease at creation: created-at, scope, key, server, instance id,
child pid, creator pid, creator cwd, creator hostname, creator username,
`MCPX_SESSION_ID` and parent if supplied, and the mcpx version. Recorded per
attach and detach: timestamp, pid, cwd, and how it ended (released, idle,
killed, pid-gone).

The same data behind `mcpx --json session ls` and as a client import, so a
script can look at its own lease and adjust its own timeout.

---

## Logging

```
status:  proposed
```

Two stores, because they answer different questions.

**JSONL files**, one per daemon, at
`$XDG_STATE_HOME/mcpx/logs/<config-hash>/<date>.jsonl`. Append-only, buffered,
flushed on a 200 ms tick and on shutdown. One object per line with `ts`,
`level`, `server`, `instance`, `session`, `scope`, `tool`, `dur_ms`, `msg`,
plus free-form fields. Cheap to write, trivially greppable, survives anything
that does not `SIGKILL`.

**A SQLite index** at `.../logs/index.db` with one row per *call* rather than
per log line — server, tool, session, start, duration, ok, bytes in and out.
That is the table `mcpx stats` reads. Keeping it call-grained rather than
line-grained is what keeps it small enough to stay fast.

```
mcpx log   --since 1h --server chrome-devtools --level warn --session <key>
mcpx log   --follow
mcpx stats --since 24h --by server      calls, p50/p95/p99, failure rate
```

Levels resolve most-specific first: per-session, then per-server, then global,
from config or `MCPX_LOG_LEVEL`. **Default `info` to stderr, `debug` to file.**
Erring toward more logs is right for the file and wrong for stderr — stderr is
shared with the script's own diagnostics and an agent reads it.

On `SIGTERM` the buffer flushes before listeners close, the same ordering fix
already made for the schema cache. `mcpx stop --force` skips the flush.

**Is there a standalone mode?** Today, no: every command talks to a daemon and
auto-starts one. That is right for pooling, which needs a process that outlives
the call. A `--standalone` flag that runs the pools in-process for a single
command is worth having for CI, where a lingering daemon is a nuisance.

---

## Configuration

```
status:  proposed
impact:  behaviour change; project configs currently replace rather than add
```

**Merge the chain instead of taking the first file.** Today a project config
hides every user-level server, which makes "add one server for this repo"
impossible without copying the whole file. Proposed: walk the whole chain,
nearest first, and merge by server name. A nearer file overrides a server of
the same name and adds new ones; `"disabled": true` turns off an inherited one.
`defaults` merges key by key.

`mcpx config --sources` prints the chain with provenance per server, because a
merge you cannot inspect is worse than no merge.

**Scripts already continue the whole chain**, nearest wins per name. That is
the behaviour config should match.

**`.config/mcpx` is now checked everywhere `.mcpx` is**, at higher precedence,
with a warning when both exist in one directory. *(Shipped.)*

**Per-user versus global.** The daemon is already keyed to a config file, and a
config file is reached through `$HOME`, so "global" means "this user's default
config". A true per-host config is `/etc/mcpx/config.json`, already last on the
chain. No third concept is needed; the docs should just say per-user and stop
saying global.

**Name collisions.** Servers aggregate by name. Two servers with the same name
in different files merge; two servers with different names that map to one
namespace is already a hard error that names both.

**Scale.** Discovery is a map read regardless of server count. The real limit
is process count, `sum(max)` across servers times the number of daemons.
Proposed: a daemon-wide `maxInstances` with LRU eviction of idle leases, so a
pathological config degrades into slower leasing rather than fork-bombing.

---

## The script contract

```
status:  proposed
```

Underspecified today and worth pinning down.

**Streams.** stdout is the result. stderr is diagnostics. A script that prints
nothing to stdout returns nothing.

**A log helper**, exported by the client, writing structured lines to stderr at
a level, so script output and script logging stop competing:

```ts
import { log } from "./mcpx-client.ts";
log.debug("snapshot", { bytes: snapshot.length });
log.warn("retrying", { attempt });
```

**Returning a value.** If a module has a default export that is a function,
mcpx calls it with parsed arguments and JSON-prints whatever it returns; if it
does not, top-level await runs and stdout is the result. That makes a file both
importable and runnable without ceremony:

```ts
export function helper() { /* importable */ }
export default async function main(args: string[]) {
  return { ok: true };            // printed as JSON
}
```

`mcpx run <name> --export <fn>` calls a named export instead of the default,
which covers "one file, several entry points".

**Arguments** already arrive as `Deno.args` / `process.argv`. Proposed
addition: `--json-args '<json>'` parsed and passed as the first parameter to
the entry function, so callers do not have to serialise through argv.

**`mcpx run --json`** wraps the whole thing — stdout, stderr, exit code,
duration, tool calls made — in one envelope for programmatic callers.

---

## Modules and libraries

```
status:  proposed
```

Scripts run on a real runtime, so imports already work: `npm:`, `jsr:`,
`https:` on Deno, `node_modules` on bun and node. What is missing is a declared
place to put shared code.

Proposed: `.config/mcpx/lib/` beside `scripts/`, on the same upward search, and
a generated import map aliasing `@lib/` to the nearest one. A project gets its
own `lib` automatically; a user-level `lib` is the fallback; a project can
shadow a user module by name, which is the same rule as scripts.

**Isolation.** Separate import maps per project give natural isolation between
projects. Within one process there is none — a script can monkey-patch anything
it imports. That is inherent to running on a real runtime with no sandbox,
which is a deliberate choice. If isolation ever matters more than capability,
that is the argument for v2-style interpretation, not for bolting a sandbox on.

**Version overrides** fall out of the import map: a project pins whatever it
wants and the user-level pin is irrelevant to it.

---

## Types

```
status:  proposed
```

**`mcpx types <ns>.<tool>`** — emit one function with its argument and return
types expanded recursively, plus the namespace's description. Small, obviously
useful, no design risk.

**Surface the server's own instructions.** `initialize` returns an
`instructions` string that mcpx currently discards. chrome-devtools-mcp almost
certainly explains `pageId` there. Proposed: cache it and emit it as a
namespace-level doc comment in `types` output. This is the cheapest fix for the
"how do I get a pageId" problem and it is nearly free.

**Producer inference.** The real problem: `pageId: number` does not say to call
`list_pages` first, and including every tool that mentions `pageId` is all 27.

Heuristic worth trying: for a required parameter `P` of tool `T`, a *producer*
is a tool that does **not** require `P` and whose name or description contains
`P`'s noun. For `pageId` that selects `list_pages`, `new_page`, `select_page` —
three, not twenty-seven. Emit them as a comment:

```typescript
/** Targets a specific page by ID. @see list_pages, new_page, select_page */
pageId: number;
```

It is a guess and should be labelled as one, but a wrong hint costs a few
tokens and a missing hint costs a failed call and a retry.

**Context-budgeted catalog.** `mcpx types` is all-or-nothing: 4,402 tokens for
`chrome_devtools` whether three tools are needed or all 27.

opencode v2 solves this with a **round-robin fit**, worth copying exactly:
every namespace is always listed with its tool count, so nothing is invisible;
each namespace's signatures are ranked cheapest first; then it loops over
namespaces taking one signature each per pass until the token budget is spent.
The effect is that a namespace with three small tools shows all three, while a
27-tool namespace shows as many as fit, and no single large namespace can
crowd out the others. v2's budget is 2,000 tokens, which on the current servers
shows 33 of 43 tools.

Proposed: `mcpx catalog [--budget 2000]`, emitting one paste-ready block whose
size is bounded no matter how many servers are configured. Pair it with
`search` for the tools that did not fit.

**OpenAPI.** `fromSpec` is v2's, and mcpx does **not** use it — there is no
OpenAPI support here at all. The proposal is to build an equivalent: compile an
OpenAPI 3.x document into a namespace of tools. It composes with pools exactly
as an MCP server does, and it is a large amount of reach for a contained amount
of code.

---

## Suggested order

1. Server `instructions` surfaced in `types`. Hours, no design risk, directly
   attacks the `pageId` problem.
2. `mcpx types <ns>.<tool>`. Same.
3. Config merge with `--sources`. Fixes a real limitation.
4. Script contract: `log`, default-export entry, `run --json`.
5. `mcpx catalog --budget`. The one number where v2 currently wins.
6. `sharing` / `scope` split, with today's modes as aliases.
7. Logging and `mcpx session ls`, which the scope work will want anyway.
8. Producer inference, `lib/`, OpenAPI.
