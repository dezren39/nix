# mcpx plugin for opencode

Tells mcpx which opencode session it is working for, and reaches mcpx without
needing the `mcpx` binary.

## Why

mcpx leases MCP servers per session, so two agents running at once get separate
processes instead of corrupting one shared browser. That only works if mcpx can
tell the sessions apart, and nothing in a shell command carries that: the agent
does not know its own session id, and asking it to pass one would spend tokens
on plumbing and be forgotten half the time.

So the harness supplies it. Every shell command gets a few environment
variables, mcpx reads them, and the agent never learns any of it happened.

Everything else the plugin does — listing servers, running scripts, reading the
log, recording timings — goes over the daemon's unix socket. **No `mcpx` on
`PATH` is required for any of it.** That is both a feature (a daemon under
launchd, in a container, or on another host works the same) and a necessity:
opencode v2 removes Bun's `$` from the plugin API, so a plugin that had to spawn
a command would simply stop working.

## Install

```sh
mkdir -p ~/.config/opencode/plugin
cp -R mcpx-session.ts mcpx ~/.config/opencode/plugin/

# optional, and a separate artifact: the arrow-key daemon picker
cp mcpx-tui.tsx ~/.config/opencode/plugin/
```

`mcpx/` must stay a subdirectory. opencode v1 loads every `plugin/*.ts` file and
calls each of its exports as a plugin; `daemon.ts` beside `mcpx-session.ts`
would have its helpers called as plugins and its class invoked without `new`.
The glob is not recursive, so one level down it is only ever imported. For the
same reason `mcpx-session.ts` exports nothing but its default.

Skills are separate again:

```sh
cp -R skills/mcpx-daemon ~/.config/opencode/skills/
```

The same skill directories are embedded in the mcpx binary (`skills/skills.go`)
and served over MCP through the `io.modelcontextprotocol/skills` extension, so
any MCP host connected to mcpx can list and load them as
`skill://mcpx/<name>/SKILL.md` without copying anything. Copy only the
directories; `skills.go` is not a skill.

### Passing options

A plugin dropped into `plugin/` gets no options — opencode only passes them for
a plugin named in config, as a two-element array:

```jsonc
{
  "plugin": [
    ["./.opencode/plugin/mcpx-session.ts", { "tools": true, "backend": "v1" }]
  ]
}
```

Every option below has an environment variable that does the same thing, which
is the way to configure a drop-in installation.

## The files

| file | what it is for |
| --- | --- |
| `mcpx-session.ts` | the opencode adapter: hooks, tools, toasts, and the session id. The only file opencode loads as a plugin. |
| `mcpx/daemon.ts` | the portable core: the daemon HTTP client, the discovery ladder, and the remembered-choice file. Imports nothing from opencode. |
| `mcpx/daemon.test.ts` | `bun test` over the ladder, against real sockets and real temporary state directories. |
| `mcpx/ops.gen.ts` | a typed method for every `/v1` operation, generated from the Go operation table. Do not edit; `go test ./internal/api -run TestPluginOpsAreGenerated -update` rewrites it. |
| `mcpx/ops.test.ts` | `bun test` over the generated methods, against a fake daemon and -- run from the Go suite -- a real one. |
| `mcpx-tui.tsx` | optional TUI plugin: a real picker for the daemon. Different realm, different API, installed separately. |
| `skills/mcpx-basics` | getting started (install, first config, first `ls`/`call`/`exec`), writing an `mcpx exec` script, and filtering in the script rather than in context. `examples/` holds seven runnable scripts. |
| `skills/mcpx-observability` | investigating a failure or a slowdown through the log rather than by re-running it. |
| `skills/mcpx-browser` | driving a stateful server, and what exclusive leasing is for. `examples/screenshot-and-errors.ts` is a full chrome-devtools run. |
| `skills/mcpx-daemon` | the guided flow for choosing between daemons. |

## Configuration

Read in this order: **plugin option, then environment variable, then default.**

| option | variable | default | effect |
| --- | --- | --- | --- |
| `endpoint` | `MCPX_DAEMON_ENDPOINT`, `MCPX_ENDPOINT` | — | a daemon named outright: `http://host:port` or `unix:///path`. Stops the ladder. |
| `socket` | `MCPX_SOCKET` | — | a socket named outright. Stops the ladder. |
| `bin` | `MCPX_PLUGIN_BIN` | `mcpx` | the binary for rung 4. `""` forbids spawning anything. |
| `binArgs` | `MCPX_PLUGIN_BIN_ARGS` | — | arguments before mcpx's own. Whitespace-separated, or a JSON array when one contains a space. |
| `backend` | `MCPX_PLUGIN_BACKEND` | `auto` | `auto` prefers the socket and spawns only as a fallback; `v1` never spawns; `cli` never uses the socket. |
| `remember` | `MCPX_PLUGIN_REMEMBER` | `session` | default scope for a daemon choice. |
| `env` | `MCPX_PLUGIN_ENV` | `full` | how much to inject into shell environments: `minimal`, `standard`, `full`. |
| `instructions` | `MCPX_PLUGIN_INSTRUCTIONS` | off | add mcpx usage to the system prompt. |
| `toolTiming` | `MCPX_PLUGIN_TOOL_TIMING` | off | record opencode's tool timings into mcpx's log. |
| `tools` | `MCPX_PLUGIN_TOOLS` | off | offer `mcpx_discover`, `mcpx_exec` and `mcpx_observe`. |
| `daemonTools` | `MCPX_PLUGIN_DAEMON_TOOLS` | when unclear | offer `mcpx_daemon_status`, `mcpx_daemon_select` and `mcpx_daemon_forget`. Default: on when `tools` is on, or when more than one daemon was found at boot. |
| `annotate` | `MCPX_PLUGIN_ANNOTATE` | on | put one line on each mcpx tool result naming the daemon that answered, when more than one matched. |
| `headless` | `MCPX_PLUGIN_HEADLESS` | detected | `1` suppresses toasts. Detected from whether the server is running in the TUI's worker realm. |

`MCPX_STATE_DIR` and `XDG_STATE_HOME` are read too, because that is where the
daemon publishes its info files and where the remembered choice is kept. They
are mcpx's variables, not the plugin's.

## Finding the daemon

The plugin used to run `mcpx --json status` once per session. It no longer
needs to. The ladder, in order, stopping at the first answer:

| # | rung | cost | applies when |
| --- | --- | --- | --- |
| 0 | `endpoint` / `socket`, by option or environment | free | configured, or remote |
| 1 | the choice remembered for this directory, still answering | one health check | every session after the first |
| 2 | the `daemon-*.json` files the daemon publishes | one readdir, parallel health checks | **the usual case, no binary** |
| 3 | `GET /v1/resolve?dir=…` against any live daemon | one request | two or more candidates |
| 4 | `mcpx --json status` in the session directory | one spawn, ~23 ms | a binary exists and the daemons are too old for rung 3 |
| 5 | ask | a toast, or the picker, or `mcpx_daemon_select` | still ambiguous, and someone is there |
| 6 | the best candidate, with a warning | free | headless, or unanswered |

**Rung 6 is the rule: it never fails because there are two.** A guess with a
visible warning beats refusing to work.

Rung 2 is why no binary is needed. The daemon already writes its socket,
endpoint, config path, version and start time into its state directory; reading
that needs `node:fs`. Candidates are then filtered by a parallel
`GET /v1/health`, because a file outlives the process that wrote it. Sockets in
the private runtime directory are found too — a state path too long for
`sun_path` moves the socket to `$XDG_RUNTIME_DIR/mcpx` or to a per-user
directory under the temp directory, and a scan that only looked in the state
directory found nothing at all on those machines.

Rung 3 is the interesting one: a daemon can answer "which daemon serves
`/some/path`?" for *any* directory, including one it does not serve, because the
answer is a function of mcpx's configuration search path. So one request turns
an ambiguous scan into a definite answer, with no binary.

Two rules the ladder will not break:

- **A named remote endpoint never falls back to a local daemon.** Silently
  answering from the wrong machine, with the wrong servers and the wrong
  credentials, is worse than answering not at all.
- **A socket that is not ours is never a candidate.** The socket's permissions
  are the access control; a socket owned by another user is skipped rather than
  dialed.

### When two match

The best candidate is chosen in this order: a config path that is an ancestor of
the session directory, then the most recently started, then the one with the
most servers. And then it says so, three ways:

- **a toast**, at boot and on the first mcpx tool use, once per session per
  reason, never when headless;
- **one line on every mcpx tool result**, naming the daemon that answered —
  impossible to miss in a transcript, and cheaper than a toast;
- **`mcpx_daemon_status`**, which lists them all with the facts that
  distinguish them.

Nothing is gated on choosing. The `skills/mcpx-daemon` skill describes the
flow: the agent inspects, recommends and explains; `mcpx_daemon_select` is what
actually sets the socket, after the user confirms through opencode's permission
prompt. The agent never types a path, so it cannot get one wrong.

### Remembering a choice

| scope | where | lasts |
| --- | --- | --- |
| `session` | plugin memory | this session |
| `until-gone` | a JSON file under the mcpx state directory | until that daemon stops answering |
| `indefinite` | the same file | this directory, until changed |
| `permanent` | `PUT /v1/settings/daemon.endpoint` | until changed |

A file rather than a storage API because **v1 server plugins have no storage
API**; `ctx.storage` arrives in v2. It lives at
`$MCPX_STATE_DIR/opencode-daemons.json`, keyed by directory, and
`mcpx_daemon_forget` clears this directory's entry.

If the daemon has no settings API yet, `permanent` says so and remembers the
choice indefinitely instead, rather than failing at the last step.

## What gets injected

`minimal`:

```
MCPX_SESSION_ID
```

`standard` adds everything the hook is handed directly, plus what the plugin
knew at startup, plus the daemon this session settled on — so a shell command
that runs `mcpx` talks to the same daemon the plugin does instead of walking its
own ladder:

```
MCPX_OPENCODE_CWD, MCPX_CALL_ID, MCPX_PROJECT_ID
MCPX_OPENCODE_DIRECTORY, MCPX_OPENCODE_WORKTREE, MCPX_WORKTREE_NAME
MCPX_HARNESS, MCPX_HARNESS_VERSION, MCPX_HARNESS_PID, MCPX_HARNESS_STARTED
MCPX_SHELL_SEQ
MCPX_DAEMON_ENDPOINT     unix:///... or http://... — the daemon this session chose
MCPX_TRACE_IDS          [["session_id","abc"],["call_id","x"],["worktree","/p"]]
```

`full` adds what needs a lookup, done once per session and cached:

```
MCPX_PARENT_SESSION_ID, MCPX_SESSION_TITLE, MCPX_SESSION_DIRECTORY
MCPX_SESSION_VERSION, MCPX_SESSION_DEPTH
MCPX_SESSION_CREATED, MCPX_SESSION_AGE_MS
```

and extends `MCPX_TRACE_IDS` with `parent_session_id` and the full `ancestry`.

### Why so many

Environment variables are not context. The model never sees them, and an unread
one costs a few bytes. So anything cheap goes in, on the reasoning that a
variable nobody reads is cheaper than a variable that is missing.

### Why not more

Transcript lengths, token counts and cost would each mean a database query per
shell command — a real cost paid on every invocation for a number almost nobody
reads. Those come from `mcpx stats --opencode`, which asks once.

## Trace ids

`MCPX_TRACE_IDS` is an array of pairs rather than a flat id, because a flat one
cannot say "this session, whose parent is that one, in this worktree". A reader
looks up the keys it knows and ignores the rest, so the list can grow without
any consumer changing.

## The tools

### `tools: true` — mcpx as opencode tools

Adds `mcpx_discover`, `mcpx_exec` and `mcpx_observe`.

Each one speaks the daemon API first and falls back to the binary only when
there is no daemon, or when the daemon is too old to have the route:

| tool | over `/v1` | fallback |
| --- | --- | --- |
| `mcpx_discover` | `GET /v1/namespaces`, `GET /v1/types` | `mcpx ls`, `mcpx types` |
| `mcpx_exec` | `POST /v1/exec` | `mcpx exec` |
| `mcpx_observe` | `GET /v1/log`, `GET /v1/stats` | `mcpx log`, `mcpx stats` |

`backend: "v1"` removes the fallback entirely, which is the honest setting for a
machine with no binary: a missing daemon then reports a missing daemon rather
than a missing command.

**Off by default** because an agent with a shell can already run mcpx, and a
tool definition costs context on every request whether or not it is used.

**Turn it on when:**

- the agent has no shell, or a heavily restricted one — then this is the only
  way it reaches MCP servers at all;
- you want mcpx calls to show up as tool calls in the transcript, which makes
  them visible to opencode's timing, permissions and replay;
- a model keeps forgetting mcpx exists. A tool in the list fixes that; a
  sentence in the system prompt reliably does not.

### The daemon tools

`mcpx_daemon_status`, `mcpx_daemon_select`, `mcpx_daemon_forget`. Present when
more than one daemon was found at boot, or whenever `tools` is on, or forced
either way with `daemonTools`. They exist to answer a question, not to guard a
door — no other tool is gated on them.

### `instructions: true` — usage in the system prompt

Four lines explaining that MCP servers are reached through mcpx.

**Off by default** because it is the expensive kind of help: every token is paid
on every request for the life of the session, whether or not any MCP tool is
ever reached for.

**Turn it on when** a project leans on mcpx constantly. Leave it off for a repo
that touches an MCP server twice a week.

### `toolTiming: true` — harness timings into mcpx's log

Records every opencode tool call into mcpx's durable log, so one `mcpx stats`
covers the harness as well as mcpx. Each record is a 0.09 ms POST over the
socket.

With no daemon the record is **dropped**, not spawned. A timing is not worth
23 ms on every tool call and certainly not worth failing one.

## Talking to the daemon directly

`mcpx/daemon.ts` connects over the unix socket instead of spawning. Measured
here, same request, mean of thirty:

| | |
| --- | --- |
| spawn `mcpx status` | 23.12 ms |
| unix socket, new connection | 0.27 ms |
| unix socket, keep-alive | **0.17 ms** |
| tcp loopback, new connection | 1.59 ms |
| tcp loopback, keep-alive | 0.65 ms |

Roughly **135× faster**, and almost all of the difference is process startup
rather than transport. That does not matter for something called once a session.
It matters a great deal for anything on the path of every tool call.

```ts
import { connect } from "./mcpx/daemon.ts"

const { client, discovery } = await connect({ directory })
if (client) await client.call("fff", "grep", { query: "x" }, sessionID)
if (discovery.ambiguous) console.warn(discovery.warning)
```

Every operation the daemon serves is also a typed method on `client.ops`,
generated from the same table as the routes, the MCP tools and the CLI
commands, so nothing is reachable only through a hand-built URL:

```ts
const { artifacts } = (await client.ops.artifactsList({ limit: 5 })) as { artifacts: unknown[] }
const bytes = await (await client.ops.artifactGet({ id })).arrayBuffer()
await client.ops.taskCancel({ id: "tsk-..." })
```

A refusal throws `OpError` carrying the status and the daemon's own message.
The named methods (`call`, `logQuery`, `resolve`...) are conveniences with
typed answers over some of these; `call` used to be hand-built and sent its
arguments under a key `/v1/call` does not read, which is why it now goes
through `ops`.

`connect` returns `{ client: undefined }` rather than throwing. A plugin that
fails to load because mcpx is not running has broken the editor for a tool the
user may not even be using.

Bun's `fetch(url, { unix })` is what dials the socket. Node's fetch ignores that
option, so a Node harness uses `MCPX_DAEMON_ENDPOINT` and the TCP listener
instead. Everything else in the file is runtime-neutral.

### Pointing at another machine

```sh
MCPX_DAEMON_ENDPOINT=http://mcpx.internal:8899   # a daemon on a VPN
MCPX_DAEMON_ENDPOINT=unix:///run/mcpx/other.sock # a different local socket
```

The same variable works for the CLI. One daemon can serve a LAN, provided you
understand that the API is unauthenticated and the network is therefore the
access control — see `daemon.address`, which is loopback until somebody
deliberately widens it.

## The TUI picker

`mcpx-tui.tsx` adds one command to the palette, **mcpx: choose daemon**: a list
with each daemon's project, start time, server count and version, then a second
list for how long the choice should stick, and an option to stop the others.

It is a separate plugin because it runs in a different realm with a different
API — a v1 module may export `server` or `tui`, never both. A choice made there
reaches the server plugin through the remembered-choice file, which the server
plugin re-reads when it has gone stale; that is also why the picker's scopes
start at "until it stops answering" rather than "this session", since session
memory lives in the other realm.

Typechecking it needs `@opencode-ai/plugin` and its three optional peers
(`@opentui/core`, `@opentui/keymap`, `@opentui/solid`). opencode supplies all of
them at runtime.

## Tests

```sh
bun test plugin/opencode/mcpx/daemon.test.ts
```

Real unix sockets and real temporary state directories, because every failure
the ladder exists to survive is about the filesystem: a stale info file, a
socket with nothing behind it, two daemons at once, a remembered daemon that has
gone. A mocked `fetch` would pass all of them.
