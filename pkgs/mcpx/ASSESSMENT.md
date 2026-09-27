# lootbox: assessment and path forward

Written after auditing `jx-codes/lootbox` (and the `dezren39` fork) against the
live installation in `~/.config/nix`, profiling it, and building a replacement.

## Summary

Lootbox's idea is right and its implementation is not worth carrying. Upstream
is a three-issue project whose last commit was 2026-02-27; the fork is already
51 commits ahead and every one of them is yours. You are not maintaining
lootbox, you are maintaining a fork with no upstream — with the cost of a
158 MB Deno artifact, a `ts-morph` dependency on the request path, and an
architecture that cannot express the one thing you actually need (more than one
Chrome).

The replacement, `mcpx`, is built, tested and packaged. It is ~3,500 lines of
dependency-free Go, 84 passing tests, a 7 MB binary.

## What is actually wrong with lootbox

### Discovery has no cache

`GET /namespaces`, `GET /rpc-namespaces` and `GET /types/:ns` recompute
everything on every request. `/types` and `/client.ts` are cached;
these three are not. Each call:

- issues `tools/list` **and** `resources/list` to every configured MCP server
  (`McpIntegrationManager.getSchemas`, a sequential `for` loop), and
- constructs a fresh `ts-morph` `Project` and re-parses every RPC tool file
  (`TypeGeneratorManager.extractTypesFromFiles`).

Measured with instrumentation added to a from-source build against the real
config:

```
fetchSchemas codedb=2ms  fff_nix=22ms  fff_worktree=24ms  fff=12ms
             chrome_devtools=7ms  context7=93-304ms
extract fs=209ms  kv=60ms  sqlite=58ms  memory=46ms  graphql=49ms
                                            total ≈ 650 ms, every request
```

The schema cache that would fix this exists but is disabled:
`McpSchemaFetcher.getAllSchemas()` has its body commented out and returns `[]`.

### The consequence is unbounded, not merely slow

Because there is no fast path, the cost of discovery is whatever the process's
current health happens to be. Measured on the live daemon over one session:

| State | `GET /namespaces` |
| --- | --- |
| Long-running daemon, as found | 34.7 s (`lootbox tools`: 42.8 s) |
| Immediately after `launchctl kickstart` | 15.6 s |
| Later the same session | 28.6 s |
| After a second restart, light load | 0.17–0.86 s |
| From-source build, same config, fresh | 0.42–0.81 s |

I could not force the 30-second state on demand within one session, so I will
not claim a single mechanism. What is reproducible is the memory behaviour:
under eight concurrent `exec` calls plus six `/namespaces` calls per round, the
server's RSS went 231 → 248 → 283 → 407 → 442 → 480 MB before GC clawed it back
to 223 MB. The logs from the as-found daemon show
`[WorkerManager] Worker sqlite exited with code 137` — OOM kills. Constructing
a TypeScript compiler instance per HTTP request is the obvious source.

mcpx under the identical load: 14 MB → 19 MB, and `ls` stays at 10 ms.

### Script execution re-downloads and re-typechecks every time

`execute_llm_script` runs:

```
deno run --allow-all --reload=http://localhost:PORT/client.ts --no-check=remote <tmpfile>
```

The URL already carries a `?v=<version>` cache-buster, so `--reload` is pure
cost: it forces a re-fetch and re-transpile of the client module on every
execution. `lootbox exec 'console.log(1)'` took 10.3 s when first measured and
0.74 s on a healthy daemon. The same script against a local client file costs
30–60 ms on any of deno, bun or node — I measured all three.

### The Chrome problem is structural

One launchd singleton holds one `chrome-devtools-mcp` client, which holds one
browser with one selected page. Every repo, every agent, every script funnels
through it. Interleaved `navigate_page` / `take_snapshot` calls from two agents
read each other's pages. This is not a bug to fix; it is what a single shared
stdio client means.

Your fork's `configurable-timeout` branch already tried to address it with
`McpSessionRegistry` and `McpAutoPortAssigner` — 283 and 187 lines respectively,
plus a `KNOWN_LIMITATIONS.md` documenting a read-modify-write race, a TOCTOU
window, and silent registry corruption. All that machinery assigns *ports* to
*lootbox instances*. But there is only ever one lootbox instance, because it
runs as a launchd singleton. The abstraction is at the wrong level: the thing
that needs isolating is the caller, not the process.

### Smaller things found along the way

- Startup blocks the HTTP listener until every MCP server has connected: 43 s
  before the port opened, measured.
- `codebase-memory` fails to start (`CBM daemon could not start within 30000ms`).
  Lootbox's response is to omit `mcp_codebase_memory` from the namespace list
  with no error anywhere a user would look. Your instructions file documents
  this class of problem as "these tools fail silently" — that is the tool's
  fault, not an inherent property of MCP.
- The dev build binds `localhost`, which resolves to IPv6 only, while the
  compiled build binds `127.0.0.1`. `curl 127.0.0.1:PORT` against a dev server
  gets connection-refused.
- The compiled binary is 158 MB. `deno compile` embeds the whole runtime.
- `context7` is reached through an `mcp-remote` Node shim. Streamable HTTP is
  well supported now; the shim is an extra process and an extra failure mode.

## Options considered

**Fix lootbox in place.** The caching and `--reload` fixes are each a few lines
and would recover most of the speed. The Chrome fix is not: it needs a pool and
per-caller leases, which means reworking `McpClientManager`, `McpIntegrationManager`,
the session registry and the auto-port assigner — most of the MCP subsystem. And
you would still own a 158 MB Deno artifact and a dead upstream.

**Adopt a maintained code-mode tool.** I looked at the current field:

| Project | Shape | Why not here |
| --- | --- | --- |
| [`mKeRix/toolscript`](https://github.com/mkerix/toolscript) | Deno CLI + gateway, semantic tool search, Claude Code plugin | Closest fit and genuinely good. Optimised for Claude Code plugins/hooks; no per-session server pooling, so the Chrome problem remains |
| [`tmustier/code-mode-mcp`](https://github.com/tmustier/code-mode-mcp) | stdio MCP server exposing one `exec` tool | It is an MCP server, which is what you are trying to avoid; still puts a tool in context |
| [`assimelha/cmcp`](https://github.com/assimelha/cmcp) | MCP proxy, `search()` + `execute()`, QuickJS | Same objection; QuickJS sandbox limits what scripts can do |
| [`mcp-use`](https://docs.mcp-use.com/typescript/client/code-mode) | Node library | A library, not a CLI; no process pooling |
| [`portofcontext/pctx`](https://github.com/portofcontext/pctx) | early-stage library | Too early |

The MCP spec itself now documents this pattern as "programmatic tool calling"
in the [client best-practices guide](https://modelcontextprotocol.io/docs/2026-07-28/develop/clients/client-best-practices),
so the concept is stable even though no implementation owns the niche.

None of them pool stateful servers per caller. That is the specific thing you
asked for and the thing none of the field does.

**Rewrite.** Chosen. The surface is small — MCP is JSON-RPC over stdio or HTTP,
and the code generator is a JSON Schema walk. The value is entirely in process
lifecycle management, which is what Go is good at.

## Why Go

You asked whether a rewrite should stay in TypeScript. It should not, for the
base layer:

- The daemon's whole job is supervising child processes, multiplexing
  concurrent requests and managing pools. Goroutines, `context`, process groups
  and `sync.Cond` are the right tools; in Node this is callback-heavy and in
  Deno you additionally inherit the packaging problem.
- Startup matters. The CLI runs on every agent invocation. Go starts in ~5 ms;
  the Deno-compiled lootbox binary starts in 130 ms and is 158 MB.
- Zero dependencies is achievable. mcpx imports nothing outside the standard
  library, so `vendorHash = null` and the Nix build is trivial.
- `ts-morph` disappears. Generating TypeScript does not require parsing
  TypeScript — the input is JSON Schema.

TypeScript stays where it belongs: the language agents write scripts in. mcpx
generates a typed client module and hands it to whichever runtime is present.

You also said you were not thrilled with the Deno complexity. mcpx does not
require Deno; it prefers it if present, and falls back to bun or node. The
generated client is written to avoid TypeScript syntax that Node's
strip-only mode rejects — a test caught parameter properties failing there, so
the client uses plain fields.

I dropped the sandbox, as you asked. Scripts run with the runtime's normal
permissions.

## What was built

```
mcpx
├── cmd/mcpx               CLI entry point
├── internal/config        mcpServers format + mcpx extras, JSONC, search path
├── internal/mcpclient     MCP client: stdio and Streamable HTTP transports
├── internal/pool          shared / pooled / session process pools, leases
├── internal/daemon        registry, schema cache, HTTP API, paths
├── internal/codegen       JSON Schema -> TypeScript, client module emitter
├── internal/runner        JS runtime detection and script execution
└── internal/e2e           end-to-end tests against a real fake MCP server
```

### How it addresses each requirement you gave

**Every MCP server reachable from the command line.** `mcpx ls`, `types`,
`search`, `call`, `exec`, `run`, `client`.

**Scripts in TypeScript.** `mcpx run file.ts` writes a typed `mcpx-client.ts`
beside the script; `mcpx exec` takes a snippet with every namespace in scope.

**No MCP pollution in context.** Nothing is registered as an MCP server. The
agent runs `mcpx ls` to see namespaces, then `mcpx types <ns>` to pull in only
the ones it wants. That is your "app discovers MCP and pulls in the ones it
wants", made explicit.

**Performance.** Measured on your machine, same servers:

```
                                      lootbox        mcpx
list namespaces                       4157.7 ms     29.4 ms     141x
types for one namespace                617.5 ms     22.0 ms      28x
trivial script                         737.4 ms     60.9 ms      12x
script with one real tool call         931.4 ms    101.3 ms       9x
one-shot tool call, no JS runtime            n/a     47.1 ms
artifact size                             141 MB      9.8 MB
daemon RSS under load                223-480 MB    14-19 MB
```

The lootbox column is its *healthy* state, right after a restart. The
as-found numbers were 42.8 s, 34.7 s and 10.3 s for the first three rows.

**No Chrome singleton.** `session` mode. Verified: 12 concurrent runs across 4
rounds, each opening a different URL, each seeing only its own page, zero
orphaned processes afterwards. `MCPX_TRACE=1` shows distinct PIDs per session.
The fake-MCP test suite asserts the same property hermetically.

**Many repos.** The daemon is keyed to the config file, so each repo with its
own `.mcpx.json` gets its own daemon, and editing a config starts a fresh one
rather than leaving a stale daemon answering. Auto-started daemons idle out
after four hours. `mcpx daemons` and `mcpx stop --all` manage the set.

**Reliability.** Failures are reported rather than swallowed: a server that
will not start shows `error` in `mcpx ls` with its stderr, instead of silently
vanishing from the namespace list. Start failures enter a backoff cooldown so a
broken server cannot spin. Child processes are killed by process group, so
browsers do not outlive the daemon.

### Tests

84 tests. The notable ones:

- `TestSessionModeIsolatesState` — concurrent sessions cannot see each other's
  writes, and use distinct pids.
- `TestConcurrentRunsGetIsolatedInstances` — the same property end to end,
  through the CLI and a real JavaScript runtime.
- `TestSharedModeHandlesConcurrentCalls` — ten concurrent 200 ms calls on one
  shared process finish in under 1.5 s, proving requests multiplex.
- `TestSessionIsolationHoldsOnEveryRuntime` — deno, bun and node each read
  `MCPX_SESSION`; a regression on one runtime would silently re-share servers.
- `TestSeparateConfigsGetSeparateDaemons`, `TestEditingConfigTakesEffect`.
- `TestNoOrphanProcessesAfterStop` — asserts on the exact pids the daemon
  reported owning.
- `TestSchemaCacheSurvivesDaemonRestart` — moves the server binary away, then
  checks `mcpx types` still answers.

Plus `scripts/stress.sh` against your real servers and `scripts/bench.sh` for
the head-to-head above.

## Bugs found and fixed while building this

Worth listing because they are the kind that would have bitten later:

1. Concurrent runs raced on the shared generated client file, so all of them
   baked the same session and collapsed onto one browser. Fixed by reading the
   session from the environment at run time.
2. `ToolFuncName("9lives")` returned `_lives` — a dropped character. Caught by a
   table test.
3. Node's type-stripping mode rejects TypeScript parameter properties, so the
   generated client failed on Node only. Caught by the cross-runtime test.
4. Unix socket paths exceeded macOS's 104-byte `sun_path` limit under deep
   state directories, failing with an opaque `bind: invalid argument`. Now
   falls back to a short hashed path.
5. A server that failed to start still recorded a cache timestamp, so it
   persisted as "cached, 0 tools" and the error was lost across restarts.
6. `mcpx stop` returned before the schema cache finished flushing, racing with
   whatever the caller did next.

## Recommendation

Adopt mcpx and retire lootbox. Concretely:

1. Try it alongside lootbox for a week. They do not conflict — different
   sockets, different config files, different state directories.
2. Point `~/.config/nix/mcpx.json` at the same servers as
   `lootbox.config.json`, marking `chrome-devtools` as `session` mode.
3. Replace the `lootbox` launchd agent with `services.mcpx` from
   `nix/darwin-module.nix`.
4. Update the instructions file. The current one says "these tools fail
   silently ... an empty result means 'not indexed', never 'not present'". With
   mcpx that caveat is unnecessary — `mcpx ls` distinguishes `ready`, `unread`
   and `error`, and errors carry the server's own message.
5. Drop `mcp-remote` for `context7`; mcpx speaks Streamable HTTP directly.
6. Keep `pkgs/lootbox-*` until you are confident, then remove them along with
   the four patches in `patches/lootbox-*.patch`.

### What is deliberately not there

- No sandbox. You said you did not want one. Scripts run with the runtime's
  normal permissions; the daemon is the only thing holding credentials, and it
  is on a `0600` unix socket.
- No web UI. Lootbox ships a Vite app; `mcpx status` covers what it was for.
- No workflow engine or script history. Lootbox has both; they are separable
  concerns and nothing in your usage referenced them.
- No RPC tool files. Lootbox lets you drop `.ts` files in a `tools/` directory
  and exposes them as namespaces. mcpx does not, because a plain TypeScript
  module that a script imports directly already does this without a protocol in
  between. If you use that feature, it is the one real gap.

### Open question for you

Whether to publish it. It is a small, focused tool in a niche where nothing is
both maintained and pooling-aware, and the MCP spec now blesses the pattern.
The Nix flake, module, README and agent instructions are already written for
public consumption.
