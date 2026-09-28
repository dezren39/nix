# opencode v2 Code Mode vs mcpx

Reviewed against `anomalyco/opencode` branch `v2` at `37049a5` (2026-09-26), and
the build pinned in `~/.config/nix` (`d0a9028`, v2.0.18).

## What opencode v2 actually shipped

`packages/codemode` is a **complete JavaScript interpreter written from
scratch** — acorn for parsing, then a tree-walking evaluator that implements
only the language subset they chose to support. 10,764 lines of implementation,
20,075 lines of tests across 34 files, running upstream **test262** conformance
files verbatim with a 1,703-line skip list.

```
packages/codemode/src/interpreter/   scope, references, promises, generators,
                                     intrinsics, limits, native boundary
packages/codemode/src/stdlib/        array, string, regexp, date, json, url,
                                     headers, math, object, collections, web
packages/codemode/src/openapi/       OpenAPI 3.x spec -> tools
packages/core/src/codemode/          catalog, instructions, execute tool, fetch
```

The framing from their README: *"Rather than trying to sandbox arbitrary
JavaScript, CodeMode only runs the language features we implement."* That is a
genuinely different security posture from Deno permissions or an isolate — the
attack surface is the interpreter, not a runtime with holes plugged.

It is wired in as a first-class feature, not an experiment:

- **MCP servers default to code mode.** `codemode: Schema.Boolean ... "Defaults
  to true"` on both `LocalConfig` and `RemoteConfig`. MCP tools are no longer in
  the model's tool list at all.
- Built-in tools opt *out* individually (`edit`, `grep`, `glob`, `patch`,
  `question` carry `codemode: false`), so the model still calls those directly.
- The model sees one `execute` tool plus a **budgeted catalog** rendered into
  instructions — 2,000 characters inline, namespaces always listed, full
  signatures selected round-robin until the budget runs out.
- When the catalog is partial, a synchronous `search(...)` built-in is available
  *inside* the interpreter for the model to find the rest.
- Catalog changes are delivered as **diffs** (`instructions.ts` computes
  added/changed/removed and emits whichever is shorter).
- `fetch` is provided as an extension. Timers, imports, filesystem and process
  access are absent.
- There is a plugin (`mcp-codemode-defaults.ts`) that turns opencode's code mode
  *off* for servers that are themselves code mode, so they don't nest.

This is a better-engineered context story than what I built. The budgeted
catalog with diff updates is genuinely clever, and `search()` inside the
interpreter means the model never round-trips to discover a tool.

## What it does not do

### It is not reachable from a command line

There is no `opencode2 exec`, no `opencode2 codemode`, no HTTP route. I checked
every CLI handler (`packages/cli/src/commands/handlers/`), the server routes and
the SDKs. `execute` is a tool that a **model** calls inside a session. Nothing
else can invoke it.

So it does not do the thing you asked for — "scripts that operate against MCP" —
unless the script is a model turn.

### It does not solve the Chrome problem

From `packages/core/src/mcp/index.ts`:

```ts
const entries = new Map<ServerName, ServerEntry>()
type ServerEntry = { ...; client?: McpClient.Connection; ... }
// "Connections remain Location-scoped"
```

One connection per configured server per Location (workspace). Every session in
that workspace shares it.

`callTool` does take a `sessionID`, which raised my hopes. It is used in exactly
one place:

```ts
...(input.sessionID === undefined ? {} : { _meta: { "ai.opencode/sessionID": input.sessionID } })
```

An advisory `_meta` hint under a vendor-specific key, passed to the server so a
*server that chooses to* can partition its own state. `chrome-devtools-mcp` does
not. Two opencode sessions in one workspace driving Chrome still share one
browser and one selected page — the same failure lootbox has.

They did fix lootbox's other lifecycle bugs: connections start asynchronously so
one slow server does not block startup, there's a reconnect path for expired
sessions, and `endpointLoads` is a keyed mutex preventing startup bursts against
a shared remote endpoint.

### The language is a subset

251 checked items, 17 gaps in `interpreter-support.md`. Present: async/await,
generators, destructuring, spread, template literals, tagged templates, regex,
`Map`/`Set`/`URL`/`URLSearchParams`/`Headers`/`Uint8Array`, labeled control flow,
TDZ semantics, `var` hoisting. Absent:

- **Classes and private fields**
- **User-defined constructor calls**
- Getters/setters in object literals
- `ArrayBuffer`, `DataView`, typed arrays other than `Uint8Array`
- `Request`, `Response`, `Blob`
- BigInt, arbitrary Symbols
- Program functions passed *into* extension code (so no callback into `fetch`)
- Array methods on array-likes (`Array.prototype.slice.call(arguments, 1)`)

For scripting tool calls this is close to irrelevant. It matters if you wanted
to paste in an existing module.

TypeScript is rejected rather than stripped — programs are JavaScript.

## Side by side

| | opencode v2 Code Mode | mcpx |
| --- | --- | --- |
| Shape | feature inside opencode's agent loop | standalone CLI + daemon |
| Invoked by | the model, via an `execute` tool | the shell: any agent, script, human, CI |
| Runtime | purpose-built JS interpreter | real deno / bun / node |
| Language | JS subset, no classes or constructors | full TypeScript |
| Confinement | strong: only implemented features exist | none (you said you didn't want it) |
| Discovery | budgeted catalog in instructions + in-program `search()` | `mcpx ls` / `search` / `types`, outside context entirely |
| Always-on context cost | one `execute` tool + ~2 KB catalog | zero |
| MCP processes | one per server per workspace, shared by all sessions | `shared` / `pooled` / `session` pools |
| Stateful server isolation | `_meta` sessionID hint; server must opt in | real, one process per run; verified |
| Remote MCP | Streamable HTTP + OAuth + elicitation | Streamable HTTP, no OAuth |
| OpenAPI specs as tools | yes (`OpenAPI.fromSpec`) | no |
| Portable to other agents | no | yes |
| Implementation | ~10.8 k LOC interpreter + ~2.7 k glue, TypeScript/Effect | ~4.2 k LOC, Go, no dependencies |
| Tests | 34 files / 20 k lines, incl. test262 | 84 tests |

## How to read this

They are not really competitors. They answer different questions.

**opencode v2 asks:** how should *opencode's own model* call MCP tools without
drowning in schemas? Its answer is better than mine — the budgeted catalog, diff
updates and in-program `search()` are a more refined context story than "run
`mcpx ls` first", and the interpreter is a real piece of engineering with
conformance tests behind it.

**mcpx asks:** how do *I*, from a shell, script against MCP servers, with real
process isolation for stateful ones? opencode v2 has no answer, because nothing
outside a model turn can reach its interpreter.

The overlap is narrower than it first appears, and it shrinks further once you
notice that the specific thing that pushed you off lootbox — concurrent Chrome —
is unsolved in v2 as well.

## Recommendation

**If you move to opencode v2:** let its Code Mode own MCP for the agent loop.
That is what it is for, it is on by default, and it will do a better job of
keeping schemas out of your context than routing through a shell tool. Delete
the lootbox instructions file; it is describing a problem v2 no longer has.

**Keep mcpx for the two things v2 cannot do:**

1. **Chrome, and any other stateful server.** Leave `chrome-devtools` out of
   opencode's `mcp.servers` entirely and reach it through `mcpx` via the bash
   tool. One browser per run, up to `max`, released when the script ends. If you
   would rather keep it in opencode's config, set `codemode: false` for it so at
   least the model calls it directly — but the sharing problem remains either
   way.
2. **Anything outside a model turn.** Shell scripts, CI, cron, a Makefile,
   debugging by hand, or a different agent entirely. `mcpx call` and `mcpx run`
   work with no model and no session.

**If you stay on opencode v1** (you are: `claude-opus-5`, `default_agent:
build`, and v2 rejects both — `opencode2 debug agents` returns `[]` against your
current config), then none of v2's Code Mode is available to you today and mcpx
replaces lootbox outright.

**Worth stealing from them regardless:**

- The budgeted catalog with round-robin signature selection. `mcpx ls` currently
  prints every namespace with a count; a `--catalog` mode that emits a
  context-budgeted listing with inline signatures would let an agent paste one
  block instead of making two calls.
- Catalog diffing, if mcpx ever grows a long-lived agent integration.
- `OpenAPI.fromSpec`. Turning an OpenAPI document into tools is a large amount
  of reach for a contained amount of code, and it composes with pools the same
  way MCP servers do.

**Not worth copying:** the interpreter. It is excellent work and it exists
because opencode must run untrusted model output inside its own process. mcpx
shells out to a real runtime, and you explicitly do not want a sandbox.

## One correction to the earlier assessment

`ASSESSMENT.md` says no maintained code-mode tool pools stateful servers per
caller. That is still true, and opencode v2 is now the most prominent data point
for it: a well-funded team built a bespoke JavaScript interpreter for code mode
and still routes every session through one shared MCP connection per server,
with per-session isolation left as an advisory `_meta` field that no server
implements.
