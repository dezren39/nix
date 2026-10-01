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

## ~~Modes and scopes~~ — shipped

```
status:  shipped
```

Built. See [Sharing and scope](./story.md) in the feature list. The retired
`mode` key is now a hard error carrying a migration hint rather than a silent
default. What follows is the original proposal, kept for the reasoning.

```
impact:  breaking config change
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

## ~~Configuration~~ — merge shipped

```
status:  shipped
```

The chain now merges, nearest first, per server name. A project adds servers
instead of hiding the user's. `mcpx config --sources` shows every contributing
file and which one defined each server.

One consequence worth recording: the daemon fingerprint now hashes **every**
source, not just the nearest. Two projects with byte-identical local configs
can still inherit different servers from different parents, and keying on the
nearest file alone would have given one project the other's servers.

The rest of this entry is the original proposal.

```
impact:  behaviour change
```

**Merge the chain instead of taking the first file.** Today a project config
hides every user-level server, which makes "add one server for this repo"
impossible without copying the whole file. Proposed: walk the whole chain,
nearest first, and merge by server name. A nearer file overrides a server of
the same name and adds new ones; `"disabled": true` turns off an inherited one.
`pool` merges key by key.

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

## Round-robin catalogue budgeting

```
status:  proposed
effort:  small; the ranking already exists in a different form
```

`mcpx types <ns>` is all-or-nothing. `mcpx catalog [--budget N]` would fit a
paste-ready block to a token budget instead.

The algorithm, which is opencode v2's and worth copying exactly:

1. List every namespace with its tool count first. Nothing is ever invisible,
   even at budget zero.
2. Rank each namespace's signatures **cheapest first**, where cost is
   `len(line)/4`.
3. Loop namespaces, taking **one signature per namespace per pass**, until the
   budget is spent.

The rotation is load-bearing. Ranking globally by cost lets one namespace's
cheap tools crowd out everything else; taking whole namespaces in turn lets a
27-tool namespace eat the budget. Per-pass rotation gives a small namespace all
its tools and a large one as many as fit.

**Ranking is by cost, not relevance.** There is no similarity search and no
model involved: it is `sort by length`, ties broken by path. That is a
deliberate difference from `mcpx search`, which *is* relevance-ranked by
substring scoring (exact 100, prefix 60, contains 40, function path 30,
namespace 20, description 10) with an any-term fallback. Neither uses
embeddings or an external library.

**Keep whole-namespace `types` as well.** They answer different questions:
`catalog` is "what is available, within a budget", `types` is "I have chosen
this namespace, give me all of it". An agent that knows it needs Chrome should
not be rationed. Deleting `types` in favour of `catalog --budget huge` would
also make the common case require a flag.

**Open question worth trying:** `mcpx catalog --budget 2000 --bias screenshot`,
where a query biases which signatures win their pass. That merges the two
ranking systems and may be better than either.

2026-09-27T07:00:00-05:00

## Protocol adapters, including OpenAPI

```
status:  proposed
effort:  medium for the interface, small per adapter
```

A namespace is currently always an MCP server. The generalisation is an
adapter interface: something that can list tools with JSON Schemas and call
one. MCP is the first implementation; OpenAPI would be the second.

**Yes, an OpenAPI service could be imported as a namespace.** Its operations
already carry typed parameters and responses, which is exactly what the
existing JSON-Schema-to-TypeScript compiler consumes, and it would inherit
pooling, scoping, `types` and `run` unchanged.

**MCP stays native.** Nothing about this replaces it.

**Emitting an OpenAPI document per namespace** is the interesting inverse and
is cheap once schemas are cached: it would let non-agent consumers — a
dashboard, a test harness, another language's client generator — use the same
servers without speaking MCP. Worth doing after import, not before.

**Would you ever not want it available?** Two honest arguments against:

- Every transport is surface area. An OpenAPI document can describe
  authentication, content negotiation and streaming semantics that MCP does
  not, and partially supporting them is worse than not offering it.
- A script can already `fetch()` an HTTP API directly. The adapter buys typed
  signatures and pooling, which matters for a large API and is pure overhead
  for a small one.

So: build the adapter interface, because it is the right shape regardless;
treat OpenAPI as the proof that the interface is general; and do not present it
as a headline. **Reordered to the bottom of the shipped-work queue.**

2026-09-27T07:00:00-05:00

## Per-instance argument templating

```
status:  proposed
effort:  small
```

Pooled instances currently get identical argv. Some servers need each instance
to differ — a distinct port, profile directory, or upstream URL.

```jsonc
"chrome-attached": {
  "command": "chrome-devtools-mcp",
  "args": ["--browserUrl", "http://127.0.0.1:{{alloc.port}}"],
  "mcpx": {
    "scope": "session", "max": 4,
    "allocate": { "port": { "range": [9222, 9299] } }
  }
}
```

Substitutions available to `args`, `env` and `url`: `{{instance.id}}`,
`{{instance.index}}`, `{{key}}`, `{{alloc.<name>}}`, `{{tmpdir}}`.

**This is not needed for Chrome as configured today.** `chrome-devtools-mcp`
takes no listen port: with `--isolated` it launches its own browser with a
temporary profile and an ephemeral debugging port of its own choosing. That is
precisely why mcpx has no port machinery, and why the predecessor's
port-assignment code was solving a problem this server does not have.

It becomes necessary for `--browserUrl` / `--wsEndpoint`, where each instance
must attach to a *different already-running* Chrome. And a real allocator has
to survive the time-of-check-to-time-of-use gap that sank the earlier attempt:
bind the port, hold the listener, pass it down, release on start.

2026-09-27T07:00:00-05:00

## Scope policy

```
status:  proposed
effort:  small
```

Scope is currently set per server and cannot be constrained or overridden.

```jsonc
"pool": {
  "scope": "session",
  "scopePolicy": {
    "allow": ["session", "parent-session", "call"],
    "allowOverride": ["flag", "script"]
  }
}
```

`allow` restricts what a per-server block may choose. `allowOverride` says who
may change it at call time: `flag` for `mcpx run --scope`, `script` for a
client call, `none` to pin it. A denied override is an error naming the policy,
never a silent downgrade.

The case for it is a shared machine or a checked-in config where one project
should not be able to widen a browser's scope and start sharing state with
another.

2026-09-27T07:00:00-05:00

## Named sessions and keepalive control

```
status:  proposed
effort:  medium
```

A key exists today and can be supplied with `--session`, which is already
naming by another word. Missing: discovery, persistence and adjustment.

```
mcpx session ls
mcpx session name <key> <name>        name one that started anonymous
mcpx session set <name> --idle 30m
mcpx session stop <name>
mcpx session show <name>
```

Naming an anonymous session is the interesting one, because an ephemeral key
is currently torn down when its caller exits. Naming must flip that bit: a
named session outlives its creator by definition.

**Per-scope keepalive defaults.** One `idleTimeout` for everything is wrong,
because the scopes have different natural lifetimes:

| scope | default | why |
| --- | --- | --- |
| `global` | 30m | expensive to lose, cheap to hold |
| `repo`, `worktree` | 15m | tracks a working session |
| `cwd` | 10m | narrower, more of them |
| `session`, `parent-session` | 10m | agent turns have gaps |
| `pid` | none | released when the pid exits |
| `call` | none | released when the caller exits |

Overridable in config per server, and at runtime per session.

2026-09-27T07:00:00-05:00

## Health-based and webhook reaping

```
status:  proposed
effort:  small for health, medium for webhooks
```

**Health check.** A server that has wedged without exiting currently occupies
its slot until idle timeout. `"health": { "url": "...", "interval": "30s",
"expect": 200 }` would reap on a failing probe. MCP already defines `ping`, so
the first version should use that rather than HTTP — it needs no configuration
and works for every server.

**Webhook.** Two directions, and they are different features:

- *Outbound*: mcpx notifies a URL on lifecycle events. This is just the event
  stream with an HTTP sink and is easy.
- *Inbound*: something tells mcpx to reap. That is a control API needing
  authentication, and `mcpx session stop` over the existing socket already
  does the job for anything local.

Recommendation: ping-based health first, outbound events second, inbound
webhooks only if a concrete need appears.

2026-09-27T07:00:00-05:00

## Structured logging

```
status:  proposed
effort:  medium
supersedes: the earlier "Logging" entry
```

Consolidates the whole logging intake. **Nothing here exists yet**; the daemon
prints unstructured lines to stderr and scripts have no logging helper at all.

### Records

One record type everywhere, emitted by daemon and scripts alike. Go's `slog`
is the model, and its `slog.Handler` interface is the right seam for formats.

Every record is **enriched** by the daemon with ambient context the caller did
not have to supply: session id, parent session, scope, resolved key, server,
instance, tool, pid, cwd, config path, mcpx version, session start, script
start, elapsed. A script logs `log.info("done")` and the record that lands has
all of it.

### Templates

A message keeps both forms:

```ts
log.info("status: {status}", { status: "ok" });
```

```json
{"ts":"...","level":"info","msg":"status: ok","template":"status: {status}","status":"ok"}
```

`msg` interpolated for humans, `template` preserved for grouping records that
differ only in values. That is what makes "how often did this line fire"
answerable. Named `{placeholders}` over `%s` because they map onto the
attributes without positional ambiguity — the same choice Serilog and
structlog make.

A bare string with no attributes is still wrapped into the same shape, so
there is one record type and no second path.

### Formats

`--format` on every command that emits records:

| format | shape |
| --- | --- |
| `json` | one object per line |
| `json-pretty` | indented, for reading |
| `logfmt` | `ts=… level=… msg=…` |
| `text` (default) | `HH:MM:SS LEVEL msg key=value` |
| `compact` | `LEVEL msg`, attributes interpolated only |
| `bare` | message only |

`text` interpolates the template and appends attributes not consumed by it.
`compact` and `bare` drop the timestamp.

### Everything as JSON

`--json` already exists on several commands and should exist on all of them,
with every human table having a JSON counterpart. Scripts should get the same:
`mcpx run --json` wrapping stdout, stderr, exit code, duration and the tool
calls made.

### Storage

JSONL per daemon under `$XDG_STATE_HOME/mcpx/logs/`, buffered and flushed on a
tick and at shutdown, plus a SQLite index of one row **per call** rather than
per line. Call-grained is what keeps `mcpx stats` fast; line-grained would not.

Default `info` to stderr, `debug` to file. More logs to the file is right;
more to stderr is not, because an agent reads stderr.

2026-09-27T07:00:00-05:00

## Harness integration and installation

```
status:  proposed
effort:  large; several independent pieces
```

Consolidates the distribution intake.

### Division of labour

Three things a harness needs, and they should not be bundled:

1. **Instructions** — the `AGENTS.md` text. Any harness that reads a markdown
   instructions directory can use it with no code.
2. **Session identity** — exporting `MCPX_SESSION_ID` and
   `MCPX_PARENT_SESSION_ID` into the shell environment. This genuinely needs a
   plugin, and it is what makes `scope: session` work.
3. **Tool surface** — optional. A harness could expose `mcpx` as a tool rather
   than relying on shell access.

Only (2) requires code. Keeping them separate means a harness with no plugin
support still gets most of the value.

### Avoiding double-injection

If a plugin injects instructions *and* an instructions file exists, the text
lands twice. The plugin should look for a marker — a known first line in the
file — and skip injection when it finds one. Detection beats configuration
because it cannot drift.

### Install command

```
mcpx install opencode|opencode2|claude|codex [--dry-run] [--platform nix|nix-flake|darwin|darwin-brew]
mcpx install --list
```

Writes instructions, plugin and config edits for that harness. **Hash-matching
is the important part**: record a hash of what was written, and on the next
install update the file only when it still matches. A user-modified file is
reported, never overwritten.

Daemon-side auto-update of harness configs is listed in the intake and should
be declined, or made strictly opt-in. A background process editing another
tool's configuration is surprising in a way that is hard to debug.

### Documents

- An install document fetchable with curl.
- A **setup document written for an agent** — different text, assuming tool
  access and no human. `mcpx setup-prompt` emits it. This is the genuinely
  novel piece: the artefact is a prompt, not a script.

2026-09-27T07:00:00-05:00

## Packaging and service management

```
status:  proposed
effort:  medium
```

A nix flake package exists. Missing: NixOS module, darwin module (one existed
and was dropped when the package moved into the nix repo), Homebrew formula,
and a plain installer for non-nix systems.

**Service installation.** `mcpx service install|uninstall|status` writing a
launchd plist or systemd user unit.

**Never with sudo.** The daemon is a per-user process owning per-user MCP
servers; a system-wide unit would run servers as the wrong user and share
state between users. A user-level unit needs no elevation, and an installer
that asks for a password to do something that does not require one teaches a
bad reflex.

**Autostart.** Both, and they already coexist: any command starts a daemon on
demand today, and a service unit only means the schema cache is warm before
the first agent asks. Keep on-demand as the supported path so that nothing
*requires* installation.

2026-09-27T07:00:00-05:00

## Transport, addressing and scale

```
status:  proposed
effort:  small for addressing, unknown for the rest
```

**What exists today.** The daemon serves HTTP over two listeners: a unix
socket at `$XDG_STATE_HOME/mcpx/daemon-<hash>.sock` for the CLI, and
`127.0.0.1` on an **ephemeral** port for generated script clients, because
`fetch()` over a unix socket is not portable across deno, bun and node. The
port is chosen by the OS, so there is nothing to collide with, and it is
published in a daemon record file. Not stdio, not named pipes.

**Multiple daemons already run**, one per config fingerprint, each with its
own socket. `mcpx daemons` lists them.

**Addressing.** `--server` accepting `example.com`, `example.com:8008`, `:42`
or a unix path would let the CLI drive a remote daemon. The blocker is not
parsing but authentication and trust, which loopback currently provides for
free. Worth specifying before building.

**Load.** Untested beyond 12 concurrent runs. Discovery is a map read and
should be flat; the interesting limits are process count and the reaper's
O(instances) sweep. A benchmark should come before any optimisation.

**Go versus Rust.** No reason to move. The workload is process supervision and
JSON, where the runtime is idle most of the time and goroutines map onto the
problem directly. Rust would buy a smaller binary and no GC pauses, neither of
which is a constraint here, at the cost of a rewrite and harder async process
handling. Revisit only if profiling finds something the GC is responsible for.

2026-09-27T07:00:00-05:00

## Name

```
status:  proposed
effort:  none, but it gets more expensive every week
```

`mcpx` reads as "MCP extended" and is already in the docs, the binary, the
config key and the environment variables.

Alternatives worth a moment: `xmcp` (same idea, scans worse aloud); `mcpsh`
(says "shell", which is the actual pitch); `tsx`-adjacent names (taken);
`conduit`, `broker`, `switchboard` (descriptive, generic, unsearchable).

Honest assessment: `mcpx` is fine and the rename cost only grows. The one
argument for changing is that the tool's distinguishing feature is *shell
access to MCP*, and `mcpsh` says that. Not worth the churn unless it is
decided before anything publishes.

2026-09-27T07:00:00-05:00

## Parity tracking

```
status:  proposed
effort:  small, ongoing
```

opencode v2 is the closest comparable and moves fast. A short table in
[`OPENCODE-V2.md`](../OPENCODE-V2.md) listing each capability and whether mcpx
has an equivalent, reviewed per release, is cheap and prevents drift being
noticed only when it is expensive.

Other systems worth reading for concepts rather than parity: Claude Code's
skills and hooks, Codex's sandboxing, and the several MCP-adjacent efforts
that expose tool catalogues as code. The specific thing to look for is how
each keeps a large tool surface out of context, because that is the problem
this tool exists to solve.

2026-09-27T07:00:00-05:00

## Extra discovery paths

```
status:  proposed
effort:  small
```

Config and scripts are searched upward from the working directory, then the
user directory. Missing: a way to add directories to that chain.

```
--override-paths <dir|file>[,...]     tried before everything
--fallback-paths <dir|file>[,...]     tried after everything
MCPX_OVERRIDE_PATHS / MCPX_FALLBACK_PATHS
```

Two flags rather than one plus an ordering rule, because the two uses are
genuinely different. An override is "for this invocation, use these"; a
fallback is "if nothing local matches, look here too" — a shared team
directory, for instance. Collapsing them into one list with a position
argument makes every call site state a position it does not care about.

Accepting files and not just directories lets a single script be injected
without building a directory around it.

**Aliasing a file to a different name** was raised alongside this and is
probably the wrong shape. Shadowing already resolves name conflicts by
precedence, and `mcpx scripts` already shows what is shadowed. An alias adds a
second mechanism for the same job, and a second place to look when a name
resolves to something unexpected. The case it would uniquely solve — wanting
*both* of two same-named scripts — is better served by naming one of them
differently, which the author controls.

2026-09-27T08:00:00-05:00

## Better tool search

```
status:  proposed
effort:  small to large depending on how far it goes
```

Search is substring scoring today: exact 100, prefix 60, contains 40, function
path 30, namespace 20, description 10, all terms required with an any-term
fallback. It is fast and has no dependencies, and it fails on typos and on
concepts that share no substring with the tool name.

Three tiers, in increasing cost:

**Tags.** Cheap, exact, and author-controlled. `"tags": ["browser", "visual"]`
per server, and `mcpx search --tag browser`. Also lets a config author fix a
bad match without touching the server.

**Typo tolerance.** Levenshtein distance ≤ 2 against tool and namespace names
only, applied after exact scoring returns nothing. Tool names are short and
few, so this is microseconds and needs no index. `screenshto` finds
`take_screenshot`.

**Concept similarity.** The expensive one. Either an embedding model — a new
dependency, a download, and a cache to invalidate — or a hand-built synonym
table, which is cheap but needs curating. Worth deferring until tags and typo
tolerance are in and still insufficient.

Recommended order: tags, typos, then reassess. Most "I could not find the
tool" cases are probably vocabulary, and tags fix vocabulary directly.

2026-09-27T08:00:00-05:00

## Types improvements

```
status:  proposed
effort:  small each
```

Beyond `mcpx types <ns>.<tool>`:

- **`--no-instructions`** to omit server guidance. *Shipped* — `fff` returns
  3,768 characters of it, which more than doubles that namespace's output.
- **`--depth N`** to control how far nested object types expand. Deeply nested
  schemas are most of the cost of a large namespace.
- **`--only <tool>[,...]`** for several tools without the whole namespace.
- **Output-type rendering.** `outputSchema` is fetched and ignored; every tool
  currently returns `Promise<ToolResult>`. Rendering it would make results
  typed rather than `any`.
- **`--format ts|json|markdown`**, since the underlying model is structured
  and only the rendering is TypeScript.

2026-09-27T08:00:00-05:00

## mcpx as a server

```
status:  proposed
effort:  medium
```

Distinct from importing or emitting OpenAPI: this makes mcpx itself something
other tools connect *to*.

**As an MCP server.** mcpx already aggregates servers, pools them and caches
their schemas. Re-exporting that as one MCP server would let a harness with no
shell access reach everything through a single connection, with pooling and
scoping applied underneath. Two shapes, and they are different products:

- *Passthrough*: every underlying tool re-exported under its namespace. Simple,
  and reintroduces exactly the context problem mcpx exists to avoid.
- *Code-mode*: two tools, `search` and `run`, where `run` takes a script. This
  is what opencode v2 does internally, and it keeps the context win.

The second is the interesting one, and it makes mcpx usable from Claude Desktop
or anything else that speaks MCP but cannot run a shell.

**As an OpenAPI endpoint.** The daemon already serves HTTP with a stable shape.
Publishing an OpenAPI document for it, and optionally binding beyond loopback,
would let non-MCP consumers use the same pooling.

**Security.** Everything today is loopback plus a `0600` unix socket, which is
why there is no authentication. Binding wider needs a token at minimum, scoped
per client, plus a decision about whether a remote caller may name a `cwd`
— which currently determines `repo` and `worktree` keys and would otherwise be
a way to reach another user's instances.

Recommendation: build the code-mode MCP server first, loopback only, no
authentication. Treat remote binding as a separate proposal with its own
threat model.

2026-09-27T08:00:00-05:00

## MCP protocol coverage

```
status:  proposed
effort:  medium, several independent pieces
```

mcpx implements `initialize`, `tools/list`, `tools/call`, `resources/list`,
`resources/read` and `ping`. The protocol defines more, and opencode v2
implements most of it. Meeting parity means, roughly in order of value:

- **`instructions` from initialize.** *Shipped.* It was being parsed and
  discarded; `fff` alone returns 3.7 KB of tool-selection guidance.
- **`prompts/list` and `prompts/get`.** Servers ship reusable prompts nobody
  can currently see.
- **Progress notifications.** A long tool call is opaque today. The protocol
  has `notifications/progress`; surfacing it would let a script show progress.
- **Cancellation.** `notifications/cancelled` is sent on timeout, and an
  incoming one stops the question it names (docs/protocol.md §4.1).
- **`tools/list_changed`.** Servers announce catalogue changes; mcpx caches
  schemas until told to refresh, so it can be wrong until `mcpx refresh`.
- **Sampling.** Servers can ask the client to run a model. Requires a model
  binding mcpx does not have and may never want.
- **Roots.** Tells a server which directories it may touch. Cheap, and pairs
  naturally with `worktree` scope.
- **Elicitation.** Servers ask the user a question mid-call. Needs a UI story
  for a tool with no UI; probably means failing the call with a clear message.
- **OAuth.** Required by some hosted servers. The largest single piece.

2026-09-27T08:00:00-05:00

## Chrome and stateful-server ergonomics

```
status:  proposed
effort:  small each
```

Chrome is the hardest server to drive well and the lessons generalise.

**`pageId` discovery.** The concrete failure: `click` requires `pageId` and its
description says only "Targets a specific page by ID". Note that
`close_page` *does* say "Call list_pages to list pages" — so the information
exists in some schemas and not others, which is exactly the inconsistency a
host should paper over. Chrome returns **no** `instructions`, so the
instructions work does not fix this. Producer inference is the remaining
answer.

**Per-server prelude.** A config-supplied snippet prepended to a namespace's
`types` output, so a user can state a convention the server does not. Half a
day, fixes `pageId` immediately, and is a general escape hatch for every server
with bad descriptions.

**Recipes.** A named script shipped per server — `mcpx run chrome:screenshot
<url>` — turning the common sequence into one call. The generated flow from the
story is already most of one.

**`--pageIdRouting`** is on by default and documented as being for concurrent
sessions, which suggests Chrome expects several agents against one instance.
Worth measuring against pooling before assuming pooling is required; pooling
gives real isolation, but if routing is sufficient for a caller who does not
mind sharing cookies, `sharing: shared, scope: session` may be cheaper.

**Generalises to:** any server whose required identifiers come from another
call. A prelude and a recipe are the two cheap answers, and both are
server-agnostic.

2026-09-27T08:00:00-05:00

## Log context and enrichment layering

```
status:  proposed
effort:  small
```

Enrichment happens in one place today: the runner folds a fixed map (session,
cwd) into every record arriving from a script. That is enough for the values
mcpx knows at launch and nothing else.

Two gaps.

**~~A script cannot bind context.~~** Shipped as `log.with`. What remains open
is whether bindings should propagate without being passed, which on Node would
mean `AsyncLocalStorage` and on Deno has no equivalent; a hidden global is the
likely alternative and is worse than an argument. Original text follows.

Every call repeats what does not change:

```typescript
log.info("scanned {file}", { file, run: runId, phase: "scan" });
```

Wanted: `const scoped = log.with({ run: runId, phase: "scan" })`, matching
slog's `Logger.With`. Bound attributes merge under per-call ones, so a call
can still override.

**Go-side context does not reach records.** slog passes a `context.Context`
into `Handler.Handle`, which is exactly the seam for request-scoped values,
but nothing is read from it. A daemon handler could stash the session, the
resolved scope key and the server name in the context once, and every record
below it would carry them without being told.

Proposed layering, outermost first, each merging under the next:

1. daemon context (server, scope, instance)
2. runner enrichment (session, cwd, entry)
3. script `log.with` bindings
4. per-call attributes

The rule that makes it predictable is that the innermost wins, because it is
the most specific statement about this one record.

2026-09-27T21:00:00-05:00

## Reserved attribute names

```
status:  shipped (collision handling), proposed (the rest)
```

A rendered record flattens attributes next to `ts`, `level`, `msg` and
`template`. A user attribute with one of those names used to be dropped
silently; it is now renamed to `attr.<name>`, so nothing is lost.

What remains open is whether mcpx should *own* a namespace instead. An
underscore prefix (`_kind`, `_session`) or a dotted one (`mcpx.session`) would
make the boundary obvious and remove the renaming rule entirely. The argument
against is that every record then carries punctuation for a collision that
almost never happens, and `jq '.level'` becomes `jq '."mcpx.level"'`.

Current view: renaming is the better trade while the reserved set is four
names. If it grows -- a log store will want `id`, `run`, `seq` -- revisit,
because at that point the collisions stop being hypothetical.

2026-09-27T21:00:00-05:00

## ~~Level filtering in the script~~ — shipped

```
status:  shipped
```

The script now knows the threshold and returns before encoding. Measured at
0.026 microseconds per filtered call, against 0.10 for the previous
encode-and-write. Source capture moved to a per-level setting at the same
time, defaulting to warn and above.

The original proposal follows.

```
effort:  small
```

A script serialises and writes every record; the threshold is applied in Go.
`log.debug()` in a loop therefore costs a JSON encode and a write even when
nothing will be printed.

Fix: pass the active level to the script as `MCPX_LOG_LEVEL` (already passed
for other reasons) and have the client return early below it. About five lines,
and it makes debug logging free to leave in.

The same argument applies harder to source capture, which costs ~4.9 us per
call: checking the level first means an unprinted debug record costs nothing
rather than a stack trace.

2026-09-27T21:00:00-05:00

## Layered script prefixes

```
status:  shipped (list + null splice), rejected (ancestor indexing)
```

`mcpx exec` generates a whole file, so it can wrap a snippet in lines from
configuration. The question was how a nearer config should combine with a
farther one.

**Shipped: a list whose elements are strings or null.** A null splices in
whatever the setting inherited, at that position:

```jsonc
"script": { "prefix": ["import { h } from '@lib/h.ts';", null] }
```

A list with no null replaces outright. On the command line the marker is `-`:
`--prefix - --prefix 'const X = 1;'` keeps the configured lines and adds one.

This is one concept doing both jobs. Inheritance is explicit and local: you can
read a single file and know whether it extends or replaces, without knowing
what is above it.

**Rejected: integers addressing ancestors.** The idea was `0` for the current
inherited value, `1` for the parent's, `2` for the grandparent's. It is
rejected because the number means nothing without the chain:

- Chain depth varies by where a config was found. The same file has different
  ancestors in a repo, under `~/.config`, and under `/etc`.
- Moving a directory silently changes what `2` refers to.
- It cannot be read locally. `["mine", 2]` is unresolvable without
  reconstructing every file above it, which is the opposite of what a config
  should require.
- The case it uniquely serves -- "skip my parent but take my grandparent" --
  has not come up, and if it did, naming the thing wanted would be clearer
  than counting hops to it.

Null covers extend-or-replace, which is the distinction people actually make.

**Corrected: prefixes *do* apply to file scripts, for effects rather than
bindings.** The first version rejected them outright, which conflated two
claims. A prefix cannot *bind* anything inside a module, because ESM gives it
its own scope. It can perfectly well *act* before and after one, because mcpx
already generates a launcher around it.

So the launcher imports the module **dynamically**:

```typescript
const script = { path, name, args, export };
<prefix lines>                 // runs first, for real
const mod = await import("./user.ts");
...
finally { <suffix lines> }     // runs even when the body throws
```

A static import would be hoisted and evaluate the module before any prefix
line whatever the source order, which is why the dynamic form is required
rather than merely tidier.

Prefix lines see `script` (path, name, args, export). Suffix lines also see
`result` (ok, value, error, ms), and run in a `finally`, so cleanup happens on
the failure path too. Both can set globals the script will read, which is the
monkeypatching seam.

What remains impossible is a prefix declaring a `const` the script can see.
That is ESM, not a limitation worth fighting, and the tests assert it stays
impossible so nobody comes to depend on an accident.

A snippet is different: `mcpx exec` generates the whole file, so its prefix
shares scope and *can* declare bindings the snippet uses.

2026-09-27T22:30:00-05:00

## Launcher as a template with placeholders

```
status:  proposed
effort:  medium
```

The launcher is a `fmt.Sprintf` with five named phase slots. That is enough
for "run something here", and not enough for "reorder this" or "wrap the entry
in my own try". The next step is a real template.

Proposed: the launcher ships as a template file with named placeholders, and
configuration may both fill them and reference them:

```
@globals  @before  @prefix  @import  @entry  @onSuccess  @onError  @suffix
```

A phase body could then say `@prefix` to splice another phase in, which is the
mechanism behind the interesting cases: running a prefix twice, or moving the
import after a guard.

Two rules make it safe:

- **References form a DAG.** A cycle is rejected at generation time with the
  path that closed it, not discovered as a hang.
- **Each placeholder resolves once.** Referencing one twice is an error unless
  `allowRepeat` names it, because the common case of a double reference is a
  mistake, and the rare deliberate one should have to say so.

Also wanted, and cheap: a `minimal` mode that emits the module import and
nothing else -- no globals, no console capture, no phases -- for a script that
wants the runtime and none of the harness.

The argument for doing this at all is that a launcher which is only
half-configurable invites someone copying it out and maintaining a fork. The
argument against doing it *now* is that five named phases have not yet been
shown insufficient.

2026-09-28T01:00:00-05:00

## Structured stack traces

```
status:  shipped
```

`Error.prepareStackTrace` is V8-specific and not in any standard, and was
verified to work in Deno, Node and Bun. It hands back CallSite objects instead
of a formatted string, so frames arrive as data:

```json
{ "function": "boom", "file": ".../script.ts", "line": 9, "column": 22, "async": false }
```

Parsing the string form was the alternative and is strictly worse: it loses the
async and native flags, and breaks on any path containing the characters the
format uses as delimiters.

One trap worth recording. **V8 memoises whatever `prepareStackTrace` returned
the first time `.stack` is read.** Asking for frames destroys the string;
asking for the string destroys the frames. Both are wanted -- the string for a
human, the frames for a query -- so frames are captured and the string is
rendered from them. The first two attempts at this each lost one form, and a
test now asserts both survive.

`captureFrames(skip, limit)` and `errorFrames(err)` are exposed to scripts.

2026-09-28T01:00:00-05:00

## Defaults as an embedded layer

```
status:  shipped
```

Every default now lives in `internal/defaults/defaults.json`, embedded with
`go:embed`, and is printable with `mcpx config --defaults`.

Before this they were four `const` blocks in four packages plus two literals
in a ticker. That is the arrangement where "what is the idle timeout" takes a
grep across the tree and still misses one -- which is exactly what happened:
a test asserting no package redeclares a default timeout found a five-minute
literal in the daemon's save ticker that three readings had walked past.

`internal/defaults` is a leaf with no mcpx imports, so `logging` can read it
without depending on `config`. Parsing happens in a variable initialiser
rather than `init()`, because Go evaluates package variables first and the
`init()` version silently handed out zero values.

`DisallowUnknownFields` is set, so a misspelled key in defaults.json is a
startup panic instead of a zero value discovered three layers down.

2026-09-28T02:00:00-05:00

## Console fidelity

```
status:  shipped
```

All 25 of Deno's console members are handled. The ones with a level become
that level, `log` still reaches stdout, `trace` carries structured frames,
`clear` resets indentation without erasing a durable log, and the devtools
markers (`profile`, `profileEnd`, `timeStamp`) become file-only records rather
than vanishing.

Two fidelity details that a test caught rather than review:

- **Names are preserved.** `console.info.name` is still `"info"`. Five methods
  had been assigned directly instead of through the helper that sets it.
- **Messages use the runtime's own inspect**, so `console.info({a:1})` reads
  `{ a: 1 }` exactly as it would have unwrapped. The JSON form is redundant --
  the structured value is already in `args`.

`console.createTask()` throws when called bare. So does Deno's. The test
compares against the unwrapped console rather than asserting nothing throws,
because matching the original includes matching its failures.

2026-09-28T02:00:00-05:00

## Protocol methods still missing

```
status:  audited 2026-09-29, method by method against all three schemas
```

An earlier audit assembled a feature list from the specification's index page
and probed a few URLs. That was not good enough: doing it properly -- diffing
every `method:` literal in `schema.ts` for `2025-06-18`, `2025-11-25` and
`2026-07-28` against what mcpx sends and serves -- found more.

Still missing, in rough order of what would be noticed:

**`resources/subscribe` / `unsubscribe` / `notifications/resources/updated`.**
A client watching a resource for changes. Present in every revision. mcpx
declares `subscribe: false`, so it is honest, but a server publishing live
data cannot tell mcpx when it changes.

**`resources/templates/list` when serving.** mcpx *consumes* templates from
upstream servers and does not offer them onward, so a templated resource
becomes invisible one hop down.

**`sampling/createMessage`.** A server asking the client to run a model
completion. mcpx is not a model host, so the honest implementation is a
pass-through to whatever is driving it -- the same shape elicitation uses.
Worth building when something asks. *Shipped, then found unreachable: `sampling` was
never declared, because the handler was installed after the handshake that
declares capabilities. Fixed 2026-09-29, with roots, which the same ordering
had broken.*

**`notifications/elicitation/complete`** (`2025-11-25`+). A url-mode
elicitation finishing out of band. Without it, a server that sends somebody
to a browser has no way to say the flow completed, and the caller waits for
the TTL.

**`subscriptions/listen` and `notifications/subscriptions/acknowledged`**
(`2026-07-28`). The modern revisions generalised subscriptions into one
mechanism. This is also the answer to "can a client subscribe to the
daemon", which mcpx currently has no way to do at all.

**`tasks/*`** (`2025-11-25` core, an extension in `2026-07-28`). Long-running
work with polling and durable handles. An extension now, so optional by
definition, but it is what a genuinely slow tool should use.

**`notifications/roots/list_changed`.** mcpx declares `listChanged: false`,
so nothing is promised, but a root set that can change is more useful than
one fixed at startup.

2026-09-29T00:30:00-05:00

## Suggested order

Shipped since this document was written: sharing/scope split, pid-scoped
lifetime, realpath-canonical keys, `.config/mcpx` lookup, named scripts, path
globals.

1. ~~Server `instructions` surfaced in `types`.~~ **Shipped.** Worth recording
   that it did *not* do what it was ranked first for: Chrome returns no
   instructions, so `pageId` is untouched. It turned out valuable for a
   different reason — `fff` returns 3.7 KB of tool-selection guidance that was
   being thrown away.
2. ~~`mcpx types <ns>.<tool>`, plus a per-server prelude.~~ **Shipped.**
   17,851 to 923 characters for one tool, and the prelude states where a
   `pageId` comes from, which is what instructions failed to supply.
3. ~~Config merge with `--sources`.~~ **Shipped.**
4. ~~Script contract: `log`, default-export entry, `run --json`.~~ **Shipped.**
5. ~~Structured logging.~~ **Shipped**, minus the on-disk store and
   `mcpx log` / `mcpx stats`, which still want a JSONL file and a
   call-grained index.
6. ~~`mcpx catalog --budget`.~~ **Shipped**, with `--bias`.
7. Named sessions, `mcpx session ls`, per-scope keepalive.
8. Scope policy.
9. Harness integration and `mcpx install`.
10. Packaging and `mcpx service install`.
11. Ping-based health reaping.
12. Producer inference for parameter hints.
13. Per-instance argument templating and allocation.
14. Protocol adapter interface, then OpenAPI import, then OpenAPI emit.

Deliberately unscheduled: remote addressing, Rust, rename.
