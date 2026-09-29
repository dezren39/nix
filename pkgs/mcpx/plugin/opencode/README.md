# mcpx session plugin for opencode

Tells mcpx which opencode session it is working for.

## Why

mcpx leases MCP servers per session, so two agents running at once get separate
processes instead of corrupting one shared browser. That only works if mcpx can
tell the sessions apart, and nothing in a shell command carries that: the agent
does not know its own session id, and asking it to pass one would spend tokens
on plumbing and be forgotten half the time.

So the harness supplies it. Every shell command gets a few environment
variables, mcpx reads them, and the agent never learns any of it happened.

## Install

```sh
mkdir -p ~/.config/opencode/plugin
cp -R mcpx-session.ts mcpx ~/.config/opencode/plugin/
```

`mcpx/` must stay a subdirectory. opencode loads every `plugin/*.ts` file and
calls each of its exports as a plugin; `daemon.ts` beside `mcpx-session.ts`
would have its helpers called as plugins and its class invoked without `new`.
The glob is not recursive, so one level down it is only ever imported.

## Configuration

Everything past the session id is opt-in.

| Variable | Default | Effect |
| --- | --- | --- |
| `MCPX_PLUGIN_ENV` | `full` | `minimal`, `standard` or `full` |
| `MCPX_PLUGIN_INSTRUCTIONS` | off | add mcpx usage to the system prompt |
| `MCPX_PLUGIN_TOOL_TIMING` | off | record opencode's tool timings into mcpx's log |
| `MCPX_PLUGIN_TOOLS` | off | offer mcpx as three opencode tools |

`full` is the default: everything already in hand, plus a session lookup done
once and cached.

`standard` drops the lookup, for anyone who would rather not have that request
at all. `minimal` is the session id alone — the only part leasing strictly
requires.

## What gets injected

`minimal`:

```
MCPX_SESSION_ID
```

`standard` adds everything the hook is handed directly, plus what the plugin
knew at startup:

```
MCPX_OPENCODE_CWD, MCPX_CALL_ID, MCPX_PROJECT_ID
MCPX_OPENCODE_DIRECTORY, MCPX_OPENCODE_WORKTREE, MCPX_WORKTREE_NAME
MCPX_HARNESS, MCPX_HARNESS_VERSION, MCPX_HARNESS_PID, MCPX_HARNESS_STARTED
MCPX_SHELL_SEQ
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


## Optional pieces, and why you would turn each one on

Everything past the session id is off by default. Each is off for a reason,
and each has a reason to enable it — not "it might be useful".

### `MCPX_PLUGIN_TOOLS=1` — mcpx as tools

Adds `mcpx_discover`, `mcpx_exec` and `mcpx_observe` to the agent's tool list.

**Off by default** because an agent with a shell can already run mcpx, and a
tool definition costs context on every request whether or not it is used.
Three definitions is cheap; cheap is not free, and most sessions never touch
an MCP server.

**Turn it on when:**

- the agent has no shell, or a heavily restricted one — then this is the only
  way it reaches MCP servers at all;
- you want mcpx calls to show up as tool calls in the transcript, which makes
  them visible to opencode's timing, permissions and replay;
- a model keeps forgetting mcpx exists. A tool in the list fixes that; a
  sentence in the system prompt reliably does not.

### `MCPX_PLUGIN_INSTRUCTIONS=1` — usage in the system prompt

Four lines explaining that MCP servers are reached through mcpx.

**Off by default** because it is the expensive kind of help: every token is
paid on every request for the life of the session, whether or not any MCP tool
is ever reached for.

**Turn it on when** a project leans on mcpx constantly and you are tired of
the model reaching for tools that are not there. Leave it off for a repo that
touches an MCP server twice a week.

### `MCPX_PLUGIN_TOOL_TIMING=1` — harness timings into mcpx's log

Records every opencode tool call into mcpx's durable log, so one `mcpx stats`
covers the harness as well as mcpx.

**Off by default.** With a daemon running each record is a 0.09 ms POST over
its socket, but with none it spawns `mcpx log record` per tool call, and
either way it adds a log line for every tool call opencode makes.

**Turn it on when** you are actually investigating where time goes, and want
opencode's tools and mcpx's calls on one timeline rather than two.

### Skills

Three, in `skills/`. Copy the ones you want into `~/.config/opencode/skills/`
or your project's skills directory.

| skill | when it earns its place |
| --- | --- |
| `mcpx-basics` | Any agent that will write an `mcpx exec` script. Teaches filtering in the script rather than in context, which is the mistake mcpx exists to prevent. |
| `mcpx-observability` | Investigating a failure or a slowdown. Turns "run it again and watch" into a query against what already happened. |
| `mcpx-browser` | Driving chrome-devtools or another stateful server. Covers exclusive leasing, which is the difference between two agents working and two agents corrupting one browser. |

Skills are loaded on demand, so an unused one costs nothing. That is why
there are three rather than one: a browser skill is dead weight in a repo with
no browser, and merging it into a general skill would make it dead weight
everywhere.


## Talking to the daemon directly

`mcpx/daemon.ts` connects to the mcpx daemon over its unix socket instead of
spawning the binary. Measured here, same request, mean of thirty:

| | |
| --- | --- |
| spawn `mcpx status` | 23.12 ms |
| unix socket, new connection | 0.27 ms |
| unix socket, keep-alive | **0.17 ms** |
| tcp loopback, new connection | 1.59 ms |
| tcp loopback, keep-alive | 0.65 ms |

Roughly **135× faster**, and almost all of the difference is process startup
rather than transport. That does not matter for something called once a
session. It matters a great deal for anything on the path of every tool call,
which is where a plugin sits.

The socket also beats loopback TCP by about 4×, and keep-alive is worth
another 2.4× on TCP.

```ts
import { connect } from "./mcpx/daemon.ts"

const mcpx = await connect($, { directory })   // undefined if no daemon
if (mcpx) await mcpx.call("fff", "grep", { query: "x" }, sessionID)
```

`connect` finds the socket by asking `mcpx --json status` in `directory` --
one spawn, so cache the client. It does not list the state directory: the
socket's name carries a hash of the resolved configuration, and a long state
path moves it elsewhere entirely, so a listing finds the wrong daemon or none.

The tool-timing hook (`MCPX_PLUGIN_TOOL_TIMING`) uses it: one lookup per
session, then each record is a POST to `/v1/log` -- measured at **0.09 ms**
against ~23 ms for spawning `mcpx log record`. With no daemon it falls back to
spawning, and waits a minute before looking again, so the absence of a daemon
does not double the cost of the fallback.

`connect` returns `undefined` rather than throwing. A plugin that fails to
load because mcpx is not running has broken the editor for a tool the user may
not even be using.

### Pointing at another machine

```sh
MCPX_DAEMON_ENDPOINT=http://mcpx.internal:8899   # a daemon on a VPN
MCPX_DAEMON_ENDPOINT=unix:///run/mcpx/other.sock # a different local socket
```

The same variable works for the CLI. One daemon can serve a LAN, provided you
understand that the API is unauthenticated and the network is therefore the
access control -- see `daemon.address`, which is loopback until somebody
deliberately widens it.
