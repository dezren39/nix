# The opencode plugin

What it is, why each part exists, what is opencode-specific, and what happens
to all of it in opencode v2.

```
created:      2026-09-29T17:00:00-05:00
last-updated: 2026-09-29T17:00:00-05:00
status:       findings + design sketch
```

This document has three jobs. It records what was found comparing opencode
v1 and v2 (§1–2), sketches the v1 design that follows from it (§3), and
separates the parts that are about opencode from the parts that are about
connecting *any* agent harness to mcpx (§5), because the same plugin is
wanted for codex, claude, pi and manus.

---

## 1. The two opencodes

They are not two products. They are two revisions of one monorepo.

| | "v1" | "v2" |
| --- | --- | --- |
| flake input | `opencode`, rev `e03db9b` | `opencode2`, rev `d0a9028` |
| version | `1.18.31` (`packages/opencode/package.json:3`) | `2.0.3` (`packages/cli/package.json:4`) |
| shipped app | `packages/opencode` | `packages/cli`, renamed `opencode2` |

The v1 pin already contains the whole v2 codebase — `packages/core`,
`packages/cli`, `packages/tui` are all there at `1.18.31`. The v2 pin has
**deleted** `packages/opencode`. So the difference is which binary is built,
not which source exists.

Three corrections to `OPENCODE-V2.md`, which predates this:

- **Code mode is not v2-only.** v1 ships `packages/codemode` and
  `packages/opencode/src/tool/code-mode.ts`, behind
  `OPENCODE_EXPERIMENTAL_CODE_MODE` (`src/effect/runtime-flags.ts:48`). With
  it on, MCP tools leave the model's tool list exactly as in v2.
- **Both accept TypeScript** in code mode; neither rejects it.
- The pinned v2 reports `2.0.3`, not `2.0.18`.

And one thing that matters for anyone writing a plugin today: **v1.18.31 runs
both plugin systems at once.** `PluginV2.node` is in the location service
group (`packages/core/src/location-services.ts:52`), which the v1 app depends
on. A well-formed v2 plugin file dropped in `.opencode/plugin/` is loaded by
the v2 host and *also* rejected by the v1 loader, which logs a complaint and
carries on. Noise, not breakage.

---

## 2. What changes for a plugin

### 2.1 Loading

| | v1 | v2 |
| --- | --- | --- |
| glob | `{plugin,plugins}/*.{ts,js}`, **not recursive** | `plugin`/`plugins` dirs, files **or directories** |
| accepted shapes | three: structured `{id, server}`, legacy "every export is a plugin", and v2 `{id, effect\|setup}` | **one**: `{id, setup}` or `{id, effect}` |
| v1 compatibility | — | **none**. No shim, no fallback |

The legacy loader is why `mcpx/daemon.ts` sits in a subdirectory: v1 calls
*every export of every file in the glob* as a plugin, and one export that is
not a function aborts the whole module. The glob not being recursive is what
makes the subdirectory work.

In v2 that workaround becomes unnecessary — only `default` is read — but the
plugin itself must be rewritten, because **v1 plugins do not load in v2 at
all**.

### 2.2 The four hooks mcpx uses

| mcpx hook | v1 | v2 |
| --- | --- | --- |
| `shell.env` | ✅ carries `cwd`, `sessionID`, `callID` | ⚠️ `shell.create.before` carries `command, cwd, timeout, shell, env` — **no `sessionID`** |
| `experimental.chat.system.transform` | ✅ | ✅ `session.hook("context")`, push onto `event.system` |
| `tool: {...}` map | ✅ un-namespaced | ✅ `tool.transform(e => e.add(...))`, namespaced and typed |
| `tool.execute.after` | ✅ success only | ✅ a union on `status` — **failures are visible too** |

`shell.env` is the load-bearing one: it is how a session id reaches mcpx
without the agent ever seeing it. Its v2 replacement drops exactly the field
that makes it work.

The v2 answer is better than a workaround, though. v2 has
`PUT /api/experimental/session/:id/environment`
(`packages/protocol/src/groups/session.ts:786-795`), which sets a session's
shell environment *once per session* rather than once per command. That is
the API v2 added for this, and it turns a per-command hook into a
per-session call.

### 2.3 `$` is gone

v1 hands plugins Bun's shell (`packages/plugin/src/index.ts:56-66`). In v2
`packages/plugin/src/effect/shell.ts` is seventeen lines of interface and no
executor — verified directly against the pinned source.

`import("node:child_process")` still works, since plugins are `import()`ed
into the server process in both versions and nothing sandboxes them. But the
runtime no longer *offers* a way to run a command, and that is a clear signal
about direction.

mcpx's plugin uses `$` for exactly two things: `mcpx --json status` to find
the daemon, and `mcpx log record` as the fallback when it cannot. **Both must
stop being required.** That is what §3 is about, and v2 turns it from a
performance argument into a portability requirement.

### 2.4 What a plugin can do to the user

This is the part that surprised me most: **the server-side plugin API is
almost UI-less in both versions.** What a *server* plugin has is the SDK's
`client.tui.*`, identical in v1 and v2:

| | |
| --- | --- |
| `showToast` | `{title?, message, variant: info\|success\|warning\|error, duration?}` |
| `appendPrompt`, `submitPrompt`, `clearPrompt` | edit the composer |
| `executeCommand` | run a command by name |
| `openHelp`, `openSessions`, `openThemes`, `openModels` | fixed dialogs |

There is **no generic dialog and no select** over HTTP, in either version.

Real UI lives in a **separate TUI plugin**, loaded in a different realm, with
its own API:

| | v1 `TuiPluginApi` | v2 TUI `Context` |
| --- | --- | --- |
| ask a question | `DialogPrompt`, `DialogSelect<V>` | `prompt()`, `select<V>()` — promises |
| toasts | `ui.toast(...)` | `ui.toast.show(...)` |
| slash commands | `TuiCommand.slash` (deprecated, use keymap layers) | `KeymapCommand.slash` + `palette: true` |
| persistent state | `kv.get/set` | `storage.store(key, {initial})`, disk-backed, live-synced |
| slots | 12 host slots | 10 slots with `prepend/append/before/after/replace` |
| panels, tabs, routes | routes only | panels, tabs, router |

So a guided "which daemon?" flow that *selects* rather than *types* needs a
TUI plugin, in both versions. A toast and a suggestion can come from the
server plugin alone.

The other route to a question is a **tool with a permission prompt**: a
plugin tool calls `ctx.ask({permission, patterns, always, metadata})`, and
the user sees an allow/deny. That is a yes/no channel available from the
server plugin, without a TUI plugin — and `always` gives "remember this",
which is most of what a daemon choice needs.

⚠️ v1's `permission.ask` **hook** is dead code: declared
(`packages/plugin/src/index.ts:261`), documented, and never once invoked.
Implementing it is a silent no-op. v2's `permission.hook("evaluate")` is
real.

### 2.5 Calling a model

|  | v1 | v2 |
| --- | --- | --- |
| full agent turn | ✅ `client.session.prompt` / `promptAsync` / `command` | ✅ same |
| **one-shot completion** | ❌ no such route | ✅ `ctx.generate.text({prompt, model?}) → {text}` |
| in-session generation | ❌ | ✅ `ctx.session.generate(...)` |
| replace the model object | via the v2-style `aisdk` hooks present in the v1 tree | ✅ `aisdk.hook("language")` |

This answers a question from the sampling design directly: **in v2 the plugin
can answer an MCP `sampling/createMessage` cheaply**, with
`ctx.generate.text`. In v1 the only way is a full agent turn, which is far
too heavy for what sampling is.

### 2.6 opencode's own MCP client

Relevant because mcpx is an MCP server to opencode:

| | v1 | v2 |
| --- | --- | --- |
| SDK | `@modelcontextprotocol/sdk@1.29.0` + a 647-line downstream patch | `@modelcontextprotocol/client@2.0.0`, one hunk |
| declared client capabilities | **`roots` only** (sampling, elicitation, tasks are commented out with issue links) | `roots` + **elicitation** (form and url) |
| `sampling/createMessage` | ❌ | ❌ |
| protocol negotiation | ❌ fixed to the SDK's latest | ✅ `legacy \| auto \| 2026-07-28` per server |
| transports | stdio, Streamable HTTP, **SSE** | Streamable HTTP, hand-written stdio. **No SSE** |
| server log forwarding | ✅ | ❌ regression |
| prompts/resources `list_changed` | ❌ | ✅ |
| OAuth | PKCE + DCR, shared callback port | + CIMD, RFC 8707 resource binding, ephemeral callback |
| execution timeout | one value, 30 s | three tiers; execution **12 h** |

Consequences for mcpx:

- **Neither version will ever send mcpx a sampling request**, because neither
  declares the capability. mcpx's sampling passthrough is for other clients.
- **v2 can answer mcpx's elicitations**; v1 cannot, and will get `cancel`.
  Until v2, elicitation has to reach the user another way — which is what the
  broker, `mcpx elicit` and `/v1/elicit` are for.
- v2 negotiating `2026-07-28` per server means mcpx's modern-era serving path
  will actually be exercised.
- v2's 12-hour execution timeout makes long `exec` runs viable without tasks.

### 2.7 Process model

| | v1 | v2 |
| --- | --- | --- |
| shape | one process, two Bun realms (worker = server, main = TUI) | two processes, HTTP/RPC between |
| default network | **none** — the TUI talks to the server over an in-process fetch; a port opens only with `--port` | HTTP |
| plugin isolation | none; `import()` into the realm | none; `import()` into the process |

"Default network: none" is why the plugin must not assume it can reach
anything over TCP, and why the unix socket matters. Bun's
`fetch(url, {unix})` works in both, since plugins run under Bun in the server
realm either way.

---

## 3. Design: finding the daemon without a binary

See the tracking issue for the full problem statement. The short version:
today the plugin runs `mcpx --json status` once per session, and if there is
no `mcpx` on `PATH`, it has nothing. In v2 it will not even have `$`.

### 3.1 The ladder

Each rung answers "which daemon?" and stops when it has an answer.

| # | rung | cost | when it applies |
| --- | --- | --- | --- |
| 0 | explicit: `MCPX_DAEMON_ENDPOINT`, `MCPX_SOCKET`, or plugin options | free | configured, or remote |
| 1 | cached choice for this directory, still alive | one health check | every session after the first |
| 2 | **read the daemon info files** | one readdir + one health check each | **no binary needed** |
| 3 | ask a live daemon to resolve the directory | one request | two or more candidates |
| 4 | `mcpx --json status` in the session directory | one spawn, ~23 ms | a binary exists and rungs 2–3 were ambiguous |
| 5 | ask the user | a toast, then a choice | still ambiguous |
| 6 | pick the best candidate, warn, keep working | free | headless, or the user did not answer |

Rung 6 is the rule the user set: **never fail because there are two.**

### 3.2 Rung 2: the info files already exist

The daemon writes `daemon-<hash>.json` into its state directory with `pid`,
`socket`, `endpoint`, `configPath`, `configHash`, `version` and `startedAt`
(`daemon.Paths.WriteInfo`). Everything discovery needs is already on disk.
Reading them needs `node:fs`, which both versions have, and no binary at all.

Candidates are then filtered to those actually listening — a `GET /v1/health`
over each socket, in parallel, a few milliseconds total. A pid file whose
daemon is gone is not a candidate.

This rung alone resolves the single-daemon case, which is almost every case,
**with no `mcpx` on `PATH`**.

### 3.3 Rung 3: any daemon can answer for all of them

A daemon already knows how mcpx resolves configuration for a directory. So
one live daemon can answer "which socket serves `/path/to/project`?" for
*any* directory, including ones it does not serve itself:

```
GET /v1/resolve?dir=/path/to/project  →  { socket, configPath, configHash, running }
```

That turns an ambiguous scan into a definite answer without a binary, using a
daemon that is already running. It is a small addition to the daemon API and
makes rung 4 a rarity.

Without it, the cheap approximation is to walk up from the session directory
to the nearest `.mcpx.json` and compare it against each info file's
`configPath`. That is right most of the time and wrong exactly when
configuration is inherited from a parent — which is the case the fingerprint
exists to handle.

### 3.4 Rung 5: asking, in ascending order of capability

**a. Toast + suggestion (server plugin, v1 and v2).** On boot and on the
first mcpx tool use:

> mcpx: 2 daemons match this directory. Using `nix` (started 3 h ago, 4
> servers). `/mcpx-daemon` to change.

Rate-limited to once per session per reason, so it cannot become noise.

**b. A tool with a permission prompt (server plugin, v1 and v2).** An
`mcpx_daemon_select` tool whose execution asks `ctx.ask(...)`. The user
sees an allow/deny with the daemon's details and an "always" option. This is
the only *interactive* channel a v1 server plugin has, and "always" is
exactly "remember this choice".

**c. A real picker (TUI plugin).** v1 `DialogSelect`, v2 `select()`. Shows
each candidate's start time, config path, server count and live connections,
best first. After choosing, a second question offers: this session only /
until it goes away / permanently in config / and optionally stop the others.

**d. The guided-skill variant.** Instead of a coded walkthrough, a skill
tells the agent to call `mcpx_daemon_status`, reason about which daemon
fits, and then call `mcpx_daemon_select` with that candidate's index, which is
what actually sets the socket. The agent proposes; the *tool* decides. This is worth doing because
it degrades correctly: the agent cannot set the wrong socket by typing a path
wrong, because it does not type the path.

⚠️ **The gating idea — block every mcpx tool until a daemon is chosen — is a
trap.** It converts "we guessed" into "nothing works", which is the failure
mode the user ruled out. Better: proceed with the best candidate, and let
every affected tool result carry one line saying which daemon answered and
how to change it.

### 3.5 Remembering

| scope | where | lifetime |
| --- | --- | --- |
| this session | plugin memory | the session |
| until it goes away | v2 `ctx.storage`; v1 needs a file | until that socket stops answering |
| permanent | `daemon.endpoint` in the user's mcpx config | until changed |

v1 server plugins have **no storage API**, so "until it goes away" means a
small JSON file under the mcpx state directory. v2 has `ctx.storage`,
namespaced per plugin. This is a real v1-workaround-fixed-in-v2.

### 3.6 Headless

`opencode run`, CI, a GitHub Action: nobody can answer. There, rung 5 is
skipped entirely and rung 6 applies — best candidate, one warning line into
the log, carry on. The plugin can tell it is headless by whether a TUI client
is attached.

### 3.7 Also needed for "no binary"

Discovery is only half. The plugin's other `$` use is `mcpx log record`, and
its opt-in tools (`mcpx_discover`, `mcpx_exec`, `mcpx_observe`) each spawn
`mcpx`. Those must move onto the daemon API:

| plugin feature | today | needs |
| --- | --- | --- |
| tool timing | `POST /v1/log` ✅, no fallback | nothing; with no daemon the record is dropped |
| `mcpx_discover` | `mcpx types` / `mcpx ls` | `GET /v1/types`, `GET /v1/namespaces` ✅ (exist) |
| `mcpx_exec` | `mcpx exec <src>` | `POST /v1/exec` ✅ (exists) |
| `mcpx_observe` | `mcpx log` / `mcpx stats` | `GET /v1/log`, `GET /v1/stats` ✅ (exist) |

`exec` over `/v1` was the one real gap, and it has since landed as
`POST /v1/exec`; the streaming-output question it raised is answered by the
frames `exec.output` selects (`docs/exec.md`).

---

## 4. What v2 changes about all of this

Things that become **unnecessary**:

- the `mcpx/` subdirectory (only `default` is read)
- hand-namespacing tool names (`tool.transform` namespaces them)
- a state file for the remembered daemon (`ctx.storage`)
- the `shell.env` per-command hook (session environment API, once per session)

Things that become **possible**:

- answering MCP sampling with `ctx.generate.text` — cheap, no agent turn
- registering MCP servers per session (`mcp.transform`), so mcpx could hand
  opencode a *filtered* server set rather than a hand-edited config
- putting a budgeted catalog into `session.hook("context")` → `event.tools`
  instead of appending to the system prompt, with diffs between turns
- seeing tool *failures* in `execute.after`

Things that **break**:

- the plugin itself: v1 plugins do not load, so v2 needs a rewrite, not a port
- `$`
- `sessionID` in the shell hook
- MCP SSE transport, and MCP server log forwarding (both regressions in v2)

Things that are **the same**:

- `client.tui.showToast` and the rest of the TUI SDK surface
- no isolation; plugins run in the host's realm
- Bun's `fetch(url, {unix})`, so the socket fast path survives
- **one MCP connection per server per location** — the chrome-sharing problem
  `OPENCODE-V2.md` describes is unchanged

---

## 5. What is opencode, and what is general

For porting to codex, claude, pi, manus, this is the part that matters.

| concern | general | opencode-specific |
| --- | --- | --- |
| **find the daemon** | the whole ladder in §3. Info files, health checks, a resolve endpoint, a cached choice | where config lives; how "the current directory" is known |
| **inject a session id** | *that* mcpx wants one, to attribute calls and route elicitations | `shell.env` (v1) / session environment API (v2). Every harness has a different hook, or none |
| **tell the model mcpx exists** | a short, budgeted description; never the tool schemas | `experimental.chat.system.transform` (v1) / `session.hook("context")` (v2) |
| **offer tools** | `discover`, `exec`, `observe` as a minimal set | `tool()` + the map (v1) / `tool.transform` (v2) |
| **record timings** | `POST /v1/log` over the socket | which hook fires after a tool call, and what it carries |
| **ask the user** | that a question must have a default and a deadline | toast vs permission prompt vs TUI dialog |
| **remember a choice** | session / until-gone / permanent | `ctx.storage` vs a file |
| **answer sampling** | a harness with a model should answer; one without should decline | `ctx.generate.text` (v2 only) |

The shape that ports: **a small core that speaks the daemon API over a unix
socket, plus a thin adapter per harness.** The core is
`plugin/opencode/mcpx/daemon.ts` today, and it is already almost harness-free
— it takes `$` only for the rung-4 spawn, which §3 removes. Lifting it into a
harness-free `core` directory beside `opencode` once a second harness exists
would make the split explicit.

What each harness needs to provide its adapter:

1. a directory (to resolve which daemon)
2. a session identifier (to attribute and route)
3. somewhere to put environment variables, or a way to wrap tool calls
4. a hook after a tool call, if timings are wanted
5. a way to show a line of text, if ambiguity is to be reported
6. optionally, a way to ask a question and a way to run a model

Only (1) and (2) are required. Everything else degrades.

---

## 6. Per-part reference

### `shell.env` → session id injection
- **What.** Sets `MCPX_SESSION_ID`, `MCPX_CALL_ID` and friends in the
  environment of every shell command opencode runs; the full list, with who
  reads each, is generated in [environment.md](environment.md).
- **Why.** mcpx needs a session id to attribute calls, lease stateful servers
  and route elicitations. Passing it through the model's context would spend
  tokens on something the model neither chooses nor should see.
- **How.** v1: the `shell.env` hook, per command, carrying `sessionID` and
  `callID`. v2: one `PUT /api/experimental/session/:id/environment` per
  session.
- **When it degrades.** No hook, no ids: mcpx attributes calls to "unknown"
  and leases fall back to whole-daemon scope.
- **General or opencode?** The *need* is general. The mechanism is entirely
  opencode's.

### The system-prompt line
- **What.** A short paragraph telling the model mcpx exists and when to reach
  for it.
- **Why.** The whole point of mcpx is that tool schemas stay out of context.
  The model still has to know the door is there.
- **How.** v1 `experimental.chat.system.transform`; v2 `session.hook("context")`.
- **When it degrades.** Without it, the opt-in tools still work when named.

### The opt-in tools
- **What.** `mcpx_discover`, `mcpx_exec`, `mcpx_observe`, off by default.
- **Why.** An agent that can *write a script* against the catalog beats one
  that can only call tools one at a time. Off by default because each adds a
  tool to every request's tool list, which is exactly the cost mcpx exists to
  avoid.
- **How.** v1 the `tool` map (watch for name collisions — it is not
  namespaced); v2 `tool.transform` with a namespace.

### The timing hook
- **What.** One log record per opencode tool call, into mcpx's log.
- **Why.** One `mcpx stats` then covers the harness and mcpx on one timeline.
- **How.** `POST /v1/log` over the socket, 0.09 ms. No spawn fallback.
- **When it degrades.** No daemon: the record is dropped
  (`plugin/opencode/mcpx-session.ts:783`). Dropping is correct — a timing is
  not worth 23 ms on every tool call, and certainly not worth failing one.

### The socket client
- **What.** `plugin/opencode/mcpx/daemon.ts`: find a daemon, then speak
  `/v1` to it.
- **Why.** 0.17 ms against 23 ms for a spawn, on a path that runs for every
  tool call. And in v2 there is no spawn available at all.
- **How.** Bun's `fetch(url, {unix})`. Node's fetch cannot do this without
  undici, so a Node harness uses the TCP endpoint instead.
- **General or opencode?** Almost entirely general. This is the part to
  extract when a second harness appears.
