# Plugin APIs (opencode v1 and v2) and harness integration

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  Every opencode v1 vs v2 plugin-API difference, the state of mcpx's opencode plugin under each, and how code execution meets the harness.
```

opencode v2 replaces the v1 plugin API wholesale: a new package name, a `{id, setup}` module instead of a function that
returns hooks, a `Context` of domains instead of an SDK client, and a hook set in which `shell.env` no longer carries a
session id. mcpx's plugin is v1-only and uses four hooks (`shell.env` PLG-13, `experimental.chat.system.transform`
PLG-16, the `tool` map PLG-26, `tool.execute.after` PLG-24); none of them works unchanged under v2, and one has never
worked under v1. The more robust v2 route for session identity is not a plugin at all: v2 sends
`_meta["ai.opencode/sessionID"]` on every MCP call (the `_meta` and process-model areas), and v2's code mode will wrap
mcpx itself unless mcpx is configured `codemode: false`.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| PLG-01 | Plugin package `@opencode-ai/plugin` renamed `@opencode/plugin` | `opencode v1 has` `opencode v2 has` `mcpx missing` | `opencode v1 @opencode-ai/plugin · v2 @opencode/plugin` | ✗ — both files import the v1 name (#47) | + high | S | high |
| PLG-02 | Module shape: hooks-returning function vs `{id, setup\|effect}` | `opencode v1 has` `opencode v2 has` `mcpx missing` | `opencode v1 function or {id?, server} · v2 {id, setup\|effect}` | ✗ — v1 shape; v2 rejects the file (#47) | + high | M | high |
| PLG-03 | v1 legacy loader calls every export of a plugin file | `opencode v1 has` `has better replacement` | `opencode v1 ✓ · v2 default export only` | ✓ — helpers kept in a subdirectory | + low | S | med |
| PLG-04 | Plugin discovery: flat file glob vs files or directories | `opencode v1 has` `opencode v2 has` | `opencode v1 {plugin,plugins}/*.{ts,js} · v2 + directories` | partial — `mcpx/` subdirectory becomes a package in v2 | + low | S | med |
| PLG-05 | Config key `plugin` vs `plugins`; where options arrive | `opencode v1 has` `opencode v2 has` | `opencode v1 plugin · v2 plugins + legacy plugin` | partial — config carries over, module does not | + low | S | low |
| PLG-06 | v1 also runs the v2 plugin host | `opencode v1 has` | `opencode v1 ✓` | n/a — mcpx ships the v1 shape | + low | S | low |
| PLG-07 | Plugin runtime: Bun vs build-dependent | `opencode v1 has` `opencode v2 has` | `opencode v1 Bun · v2 Bun build, Node build exists` | partial — assumes Bun `fetch({unix})` | + low | S | low |
| PLG-08 | Plugin lifecycle: `dispose` vs setup cleanup and hot reload | `opencode v1 has` `opencode v2 has` | `opencode v1 dispose · v2 cleanup + file-watch reload` | n/a — no dispose used | + low | S | low |
| PLG-09 | Plugin instance per directory vs per Location | `opencode v1 has` `opencode v2 has` | `opencode v1 per directory · v2 per Location` | partial — "session" cache is per place | + med | S | med |
| PLG-10 | `client` SDK and `serverUrl` in plugin input | `opencode v1 has` `opencode v2 has` | `opencode v1 ✓ · v2 absent (domains instead)` | ✗ — uses `client.session.get`, `client.tui.showToast` | + med | M | high |
| PLG-11 | `directory`/`worktree`/`project` vs `ctx.location` | `opencode v1 has` `opencode v2 has` | `opencode v1 fields · v2 ctx.location` | partial — destructures v1 fields | + low | S | low |
| PLG-12 | Bun `$` shell handed to plugins | `opencode v1 has` `has no replacement` | `opencode v1 ✓ · v2 —` | ✓ — uses `execFile` and the socket | + low | S | low |
| PLG-13 | `shell.env` hook vs `shell.hook("create.before")` without session | `opencode v1 has` `opencode v2 has` `mcpx missing` | `opencode v1 sessionID, callID · v2 neither` | ✗ — `MCPX_SESSION_ID` lost under v2 (#47) | + high | M | high |
| PLG-14 | v1 `shell.env` has no session id on PTY terminals | `opencode v1 has` | `opencode v1 bash tool and ! shell ✓ · PTY —` | partial — terminal-run `mcpx` gets no session | + low | S | low |
| PLG-15 | v2 per-session environment route replaces the whole env | `opencode v2 has` | `opencode v2 only; TUI-owned; in-memory` | n/a — docs propose it; it would break shells | − harmful | S | high |
| PLG-16 | `experimental.chat.system.transform` output is `{system: string[]}` | `opencode v1 has` `mcpx missing` | `opencode v1 ✓ · v2 no hook of that name` | ✗ broken — writes `output.parts`; never injected (#217) | + med | S | med |
| PLG-17 | v2 `session.hook("context")` edits system, messages, tools | `opencode v2 has` | `opencode v1 three hooks · v2 one; invented tools dropped` | n/a — not used | + med | S | med |
| PLG-18 | `chat.message` vs `session.hook("prompt")` | `opencode v1 has` `opencode v2 has` | `opencode v1 chat.message · v2 prompt` | n/a — not used | + low | S | low |
| PLG-19 | `chat.params`/`chat.headers` vs `context.options`/`model.request.headers` | `opencode v1 has` `opencode v2 has` | `opencode v1 two hooks · v2 context, model.request (+ v2-only)` | n/a — not used | + low | S | low |
| PLG-20 | `experimental.session.compacting` vs `session.hook("compaction")` | `opencode v1 has` `opencode v2 has` | `opencode v1 hook · v2 compaction` | n/a — not used | + low | S | low |
| PLG-21 | v1 hooks with no v2 hook of the same purpose | `opencode v1 has` | `opencode v1 only` | n/a — mcpx uses none | + low | S | low |
| PLG-22 | `event` hook vs `ctx.event.subscribe()` stream | `opencode v1 has` `opencode v2 has` | `opencode v1 pushed · v2 subscribed` | n/a — not used | + low | S | low |
| PLG-23 | `tool.execute.before`: v2 adds session and mutable input | `opencode v1 has` `opencode v2 has` | `opencode v1 {args} · v2 {sessionID, agent, messageID, id, input}` | n/a — not used | + med | M | med |
| PLG-24 | `tool.execute.after`: success-only vs status union | `opencode v1 has` `opencode v2 has` `has better replacement` | `opencode v1 success only · v2 completed or error` | partial — reads `title`, undefined for MCP | + low | S | low |
| PLG-25 | `permission.ask` declared but dead vs live `evaluate` hook | `opencode v1 has` `opencode v2 has` `has better replacement` | `opencode v1 never triggered · v2 live` | n/a — not used | + low | S | low |
| PLG-26 | Defining a tool: zod map vs `ctx.tool.transform` editor | `opencode v1 has` `opencode v2 has` | `opencode v1 map, un-namespaced · v2 editor, optional namespace` | partial — v1 map only | + high | M | med |
| PLG-27 | v2 plugin tools hide behind code mode by default | `opencode v2 has` `mcpx missing` | `opencode v2 codemode !== false · v1 plugin tools direct` | ✗ — no v2 port; it would need `codemode: false` (#47) | + high | S | high |
| PLG-28 | A plugin tool can ask the user | `opencode v1 has` `has no replacement` | `opencode v1 ToolContext.ask · v2 —` | partial — `mcpx_daemon_select` relies on it | − lost | M | med |
| PLG-29 | Toast from a server plugin | `opencode v1 has` `opencode v2 has` | `opencode v1 client.tui.showToast · v2 TUI plugin only` | partial — v1 toast only | + med | M | med |
| PLG-30 | TUI plugin shape and dialogs | `opencode v1 has` `opencode v2 has` `mcpx missing` | `opencode v1 {id?, tui} JSX · v2 {id, setup} promises` | ✗ — v1 TUI plugin only (#47) | + med | M | low |
| PLG-31 | `ctx.rpc` between server and TUI plugins | `opencode v2 has` | `opencode v1 — · v2 ✓` | n/a — v1 uses a file as the channel | + med | M | low |
| PLG-32 | Plugin storage | `opencode v1 has` `opencode v2 has` | `opencode v1 TUI kv only · v2 ctx.storage` | n/a — own remember file | + low | S | low |
| PLG-33 | Calling a model from a plugin | `opencode v1 has` `opencode v2 has` | `opencode v1 full turn · v2 ctx.generate.text` | n/a — not examined | + med | M | low |
| PLG-34 | Plugins add or change MCP servers (Location-scoped) | `opencode v2 has` | `opencode v1 config hook · v2 ctx.mcp.transform/reload/list` | n/a — not used | + med | S | med |
| PLG-35 | `?codemode=false` appended to remote MCP URLs | `opencode v2 has` `mcpx missing` | `opencode v2 client` | ✗ — neither sends nor honours it (#218) | + med | M | med |
| PLG-36 | Hooks and permission checks per child call in code mode | `opencode v1 has` `opencode v2 has` `mcpx missing` | `opencode v1/v2 per child · mcpx one opaque call` | ✗ — script calls bypass opencode permissions (#218) | + high | L | high |
| PLG-37 | Live status of each child call during a script | `opencode v1 has` `opencode v2 has` `mcpx missing` | `opencode v1 metadata · v2 progress` | ✗ — plugin makes one blocking POST (#218) | + med | S | low |
| PLG-38 | Cancelling in the harness stops the script | `opencode v1 has` `opencode v2 has` `mcpx missing` | `opencode v1 abort signal · v2 fiber interrupt` | ✗ — plugin ignores the abort signal (#218) | + med | S | med |
| PLG-39 | Harness session and working directory reach the script | `opencode v2 has` `mcpx missing` | `opencode v2 _meta on child calls · mcpx bash path only` | ✗ — plugin `mcpx_exec` sends neither (#189) | + high | S | high |
| PLG-40 | Plugin `mcpx_exec` reads a field the daemon never returns | `mcpx missing` | mcpx plugin only | ✗ broken — whole envelope shown to model (#189) | + med | S | med |
| PLG-41 | Code execution offered as an MCP server | `mcpx has, others don't` | `mcpx /mcp · Cloudflare codeMcpServer · opencode — · lootbox —` | ✓ — `mcpx_exec` and friends over `/mcp` | + med | S | low |

## PLG-01 Plugin package `@opencode-ai/plugin` renamed `@opencode/plugin`

- **What.** The plugin SDK changed npm scope. v1 exports `.`, `./tool`, `./tui`, `./v2/effect`, `./v2/promise`; v2 exports
  `.`, `./effect`, `./host`, `./tui`. v2 has no `@opencode-ai/plugin` anywhere in core, cli, server or plugin sources.
- **Where.** opencode v1 and v2, incompatible.
- **mcpx @ 05c78b2.** Both plugin files import the v1 name (`plugin/opencode/mcpx-session.ts:1`,
  `plugin/opencode/mcpx-tui.tsx:42`).
- **Value to mcpx.** + high: the v2 port is a new file, not an edit.
- **Effort.** S for the import; the API behind it differs everywhere (PLG-02 onward).
- **Risk.** High: under v2 the v1 plugin cannot even resolve its import.
- **Detail.** v1 auto-installs `@opencode-ai/plugin@<version>` into every config directory so imports resolve; no
  equivalent auto-install of `@opencode/plugin` was found in v2, whose loader takes npm packages via `npm.add` and local
  files via a watching source loader. How a local v2 plugin file resolves the import in the Bun-compiled binary is
  unverified (v2's Node entry publishes the API on a global symbol).
- **Sources.** `v1:packages/plugin/package.json:3` "\"name\": \"@opencode-ai/plugin\""; `v2:packages/plugin/package.json:3` "\"name\": \"@opencode/plugin\""; `plugin/opencode/mcpx-session.ts:1` "import { tool, type Plugin } from \"@opencode-ai/plugin\""; `v1:packages/opencode/src/config/config.ts:456`; `v2:packages/core/src/plugin/module.ts:91`; `v2:packages/cli/src/node/plugin-runtime.promise.ts:14`

## PLG-02 Module shape: hooks-returning function vs `{id, setup|effect}`

- **What.** v1 calls `Plugin = (input, options) => Promise<Hooks>` and uses the hooks object it returns. v2 requires a
  default export `{id, setup(ctx)}` (Promise) or `{id, effect(ctx)}` (Effect) that registers hooks imperatively;
  `setup` returns an optional cleanup.
- **Where.** v1: plain function export or `PluginModule {id?, server, tui?: never}`. v2: only the `{id, setup|effect}`
  union; anything else is a `PluginModule.LoadError`.
- **mcpx @ 05c78b2.** `export default (async ({ directory, worktree, project, client }, options) => {…}) satisfies Plugin`
  (`plugin/opencode/mcpx-session.ts:169`), v1 only.
- **Value to mcpx.** + high: a v2 build is required before mcpx's plugin works on v2. − two artifacts to maintain.
- **Effort.** M — every hook maps to a different v2 domain call.
- **Risk.** High: v2 rejects the current file with "Plugin must export a default definition with an id and an effect
  or setup function."
- **Detail.** v2 validates with Effect Schema `Struct({id: String, effect|setup: function})`; `id` is mandatory and
  duplicate ids fail (`v2:packages/core/src/plugin/supervisor.ts:108`).
- **Sources.** `v1:packages/plugin/src/index.ts:74` "export type Plugin = (input: PluginInput, options?: PluginOptions) => Promise<Hooks>"; `v2:packages/core/src/plugin/module.ts:60`; `v2:packages/core/src/plugin/module.ts:111`; `v2:packages/plugin/src/promise/plugin.ts:58`; `v2:packages/plugin/src/promise/plugin.ts:60`

## PLG-03 v1 legacy loader calls every export of a plugin file

- **What.** When a v1 module's default is not a `{id|server|tui}` object, every export is treated as a plugin function;
  one non-function export throws and the whole module is skipped (logged).
- **Where.** v1 only; v2 reads only `default`.
- **mcpx @ 05c78b2.** Works around it by keeping helpers in a subdirectory and exporting only the default
  (`plugin/opencode/mcpx-session.ts:2`).
- **Value to mcpx.** + low: the workaround is still needed for v1.
- **Effort.** S.
- **Risk.** Med: a helper added beside `mcpx-session.ts` silently disables the plugin in v1.
- **Detail.** The throw happens in `getLegacyPlugins` before any export is invoked, so there is no partial registration.
- **Sources.** `v1:packages/opencode/src/plugin/index.ts:107` "if (!plugin) throw new TypeError(\"Plugin export is not a function\")"; `v1:packages/opencode/src/plugin/index.ts:123`; `v2:packages/core/src/plugin/module.ts:115`

## PLG-04 Plugin discovery: flat file glob vs files or directories

- **What.** v1 scans `{plugin,plugins}/*.{ts,js}` in each config directory, non-recursively. v2 lists `plugin/` and
  `plugins/` entries and accepts `.ts`/`.js` files and directories (package-style, resolved through `server`, index,
  `tui` or `rpc` entrypoints).
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Relies on the non-recursive glob to hide its `mcpx/` helper directory
  (`plugin/opencode/README.md:34`).
- **Value to mcpx.** + low: v2 lets one directory ship server, TUI and rpc together.
- **Effort.** S.
- **Risk.** Med: copying the v1 layout into a v2 plugin directory makes v2 try to load `mcpx/` as a plugin package and
  fail ("Plugin entrypoint not found").
- **Detail.** v2 `Host.resolve` looks for `server`, then `""` (index), `tui`, `rpc`.
- **Sources.** `v1:packages/opencode/src/config/plugin.ts:21` "Glob.scan(\"{plugin,plugins}/*.{ts,js}\""; `v2:packages/core/src/plugin/source-directory.ts:25` "if (entry.type === \"directory\") return Option.some(entry.target)"; `v2:packages/plugin/src/host.ts:43`

## PLG-05 Config key `plugin` vs `plugins`; where options arrive

- **What.** v1 config is `plugin: Array<string | [string, options]>`; v2's native key is `plugins: [{package, options}]`,
  and it still accepts legacy `plugin` entries, normalising both into `plugins`. Options reach a v1 plugin as its
  second argument and a v2 plugin as `ctx.options`.
- **Where.** Both accept `plugin`; only v2 has `plugins`.
- **mcpx @ 05c78b2.** The README documents `"plugin": [["./…/mcpx-session.ts", {…}]]` (`plugin/opencode/README.md:53`).
- **Value to mcpx.** + low: configuration is the one thing that carries across.
- **Effort.** S.
- **Risk.** Low: a user who keeps the v1 entry on v2 gets a module load error (PLG-02), not a config error.
- **Detail.** None beyond the above.
- **Sources.** `v1:packages/plugin/src/index.ts:71` "plugin?: Array<string | [string, PluginOptions]>"; `v2:packages/core/src/config/normalize.ts:185`; `v2:packages/core/src/config/normalize.ts:190`; `v2:packages/plugin/src/promise/plugin.ts:29`

## PLG-06 v1 also runs the v2 plugin host

- **What.** v1.18.31 contains `packages/core` and wires its location services, including `PluginV2.node` and a
  `config-plugin` loader that globs the same `{plugin,plugins}/*.{ts,js}` and accepts `{id, effect|setup}`.
- **Where.** v1 only (v2 is the host).
- **mcpx @ 05c78b2.** Not affected; mcpx ships the v1 shape.
- **Value to mcpx.** + low: a v2-shaped plugin can be tried under v1. − a v2 plugin in a shared directory is also offered
  to the v1 loader, which rejects it ("must default export an object with server()"), logged and non-fatal.
- **Effort.** S.
- **Risk.** Low: log noise.
- **Detail.** Whether v1 sessions consume v2 hooks such as `session.hook("context")` is unverified: the host is wired,
  but v1's prompt path uses v1 triggers (`v1:packages/opencode/src/session/llm/request.ts:69`).
- **Sources.** `v1:packages/core/src/location-services.ts:52` "PluginV2.node,"; `v1:packages/core/src/config/plugin/external.ts:60`; `v1:packages/core/src/plugin/internal.ts:119`; `v1:packages/opencode/src/plugin/shared.ts:297`

## PLG-07 Plugin runtime: Bun vs build-dependent

- **What.** v1 plugins run under Bun (the server hands in `Bun.$`). v2's shipped build is Bun (its source loader uses
  `Bun.Transpiler`), but v2 also has a Node build target. In both, plugins are imported unsandboxed into the server
  process (v1 the server Worker, v2 the daemon).
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** The plugin reaches the daemon socket with Bun's `fetch(url, {unix})`; its own comment notes that
  Node's fetch ignores the option (`plugin/opencode/mcpx/daemon.ts:348-351`).
- **Value to mcpx.** + low: holds for the nix build installed here.
- **Effort.** S.
- **Risk.** Low: a Node build of v2 would break unix-socket fetch.
- **Detail.** `docs/opencode-plugin.md:201` says `fetch(url, {unix})` "works in both, since plugins run under Bun"; the
  source says that depends on the build (`v2:packages/cli/package.json:21`).
- **Sources.** `v1:packages/opencode/src/plugin/index.ts:167`; `v2:packages/plugin/src/source.bun.ts:27`; `v2:packages/cli/package.json:21` "\"build:node\": \"bun run script/build-node.ts\","; `v1:packages/opencode/src/plugin/index.ts:118`; `v2:packages/core/src/plugin/module.ts:102`; `docs/opencode-plugin.md:201`

## PLG-08 Plugin lifecycle: `dispose` vs setup cleanup and hot reload

- **What.** v1 plugins may return a `dispose` hook, run on the instance finalizer. v2 `setup` may return a cleanup, and
  local plugin sources (and their local imports) are file-watched and reloaded on change.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** No dispose used.
- **Value to mcpx.** + low: v2 reload makes iteration fast.
- **Effort.** S.
- **Risk.** Low: module-level state (mcpx's `sessions` cache, the chosen daemon) resets on every v2 reload.
- **Detail.** v2's Bun source loader scans imports with `Bun.Transpiler` to decide what to watch.
- **Sources.** `v1:packages/plugin/src/index.ts:223` "dispose?: () => Promise<void>"; `v2:packages/plugin/src/promise/plugin.ts:60`; `v2:packages/core/src/plugin/module.ts:22`; `v2:packages/plugin/src/source.bun.ts:27`

## PLG-09 Plugin instance per directory vs per Location

- **What.** v1 instantiates each plugin once per directory instance; v2 once per Location (directory plus workspace id).
  Neither is per session.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Caches per-session data inside one plugin instance (`plugin/opencode/mcpx-session.ts:375`).
- **Value to mcpx.** + med: in-memory "session" state for the daemon choice is really "this directory in this process".
- **Effort.** S.
- **Risk.** Med: in v2 one plugin instance serves every TUI window attached to the daemon, so that memory is shared across
  windows (process-model area).
- **Detail.** v1's `event` hook only receives events whose `location.directory` equals the instance directory.
- **Sources.** `v1:packages/opencode/src/plugin/index.ts:134`; `v1:packages/opencode/src/plugin/index.ts:256`; `v2:packages/core/src/plugin.ts:296` "Node.makeLocationNode({"

## PLG-10 `client` SDK and `serverUrl` in plugin input

- **What.** v1 hands a plugin an SDK `client` bound to the server (through an in-process fetch shim when no port is
  open) and a `serverUrl` getter. v2's `Context` has neither; it has typed domains instead (`app`, `location`,
  `session`, `mcp`, `tool`, `shell`, `permission`, `generate`, `storage`, `rpc`, `event` and more).
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Uses `client.session.get` to describe sessions (`plugin/opencode/mcpx-session.ts:393`) and
  `client.tui.showToast` for its boot and ambiguity toast (`plugin/opencode/mcpx-session.ts:298`).
- **Value to mcpx.** + med: v2 domains are typed and direct (`ctx.session.get`). − every `client` call must be
  re-sourced, and the toast has no server-side equivalent (PLG-29).
- **Effort.** M.
- **Risk.** High: the v1 code paths do not exist on v2.
- **Detail.** v2 also removes the TUI routes from its HTTP protocol, so there is no URL a plugin could call instead.
- **Sources.** `v1:packages/plugin/src/index.ts:56`; `v1:packages/opencode/src/plugin/index.ts:146`; `v1:packages/opencode/src/plugin/index.ts:163`; `v2:packages/plugin/src/promise/plugin.ts:26`; `v2:packages/plugin/src/promise/plugin.ts:46`

## PLG-11 `directory`/`worktree`/`project` vs `ctx.location`

- **What.** v1 passes `project`, `directory` and `worktree` as input fields; v2 carries directory, workspace and project
  in `ctx.location`.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Destructures `directory, worktree, project, client` (`plugin/opencode/mcpx-session.ts:169`).
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** v1 also passes `experimental_workspace`; v2 folds workspace into the Location key.
- **Sources.** `v1:packages/plugin/src/index.ts:58`; `v2:packages/plugin/src/promise/plugin.ts:28`; `plugin/opencode/mcpx-session.ts:169`

## PLG-12 Bun `$` shell handed to plugins

- **What.** v1 passes `Bun.$` as `input.$`; v2's `Context` has no command executor (its `shell` domain is only the
  `create.before` hook).
- **Where.** opencode v1 only.
- **mcpx @ 05c78b2.** Already independent of `$`: it spawns with `node:child_process` `execFile`
  (`plugin/opencode/mcpx/daemon.ts:41`) and talks to the daemon over the unix socket
  (`plugin/opencode/mcpx/daemon.ts:352`).
- **Value to mcpx.** + low: nothing to port.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** `docs/opencode-plugin.md:103` says mcpx's plugin "uses `$` for exactly two things"; the source at 05c78b2
  uses `execFile` and the socket, and does not destructure `$`. `import("node:child_process")` still works in v2 because
  plugins are imported unsandboxed.
- **Sources.** `v1:packages/plugin/src/index.ts:65` "$: BunShell"; `v2:packages/plugin/src/promise/shell.ts:12` "readonly \"create.before\": ShellCreateBefore"; `plugin/opencode/mcpx/daemon.ts:41`; `docs/opencode-plugin.md:103`

## PLG-13 `shell.env` hook vs `shell.hook("create.before")` without session

- **What.** v1 `shell.env(input: {cwd, sessionID?, callID?}, output: {env})` runs per command; v2's `create.before` event
  is `{command, cwd, timeout, shell, env}` with a mutable env and no session or call id.
- **Where.** v1 bash tool and `!` shell pass `sessionID`/`callID`. v2 never exposes them to this hook, although its shell
  tool carries `metadata.sessionID` internally.
- **mcpx @ 05c78b2.** Sets `MCPX_SESSION_ID` from `input.sessionID` (`plugin/opencode/mcpx-session.ts:664`, `:670`); this
  is how `mcpx` run from an agent's shell gets a session-scoped browser.
- **Value to mcpx.** + high: session attribution is the whole reason mcpx uses the hook.
- **Effort.** M. The only v2 hook that sees both `sessionID` and the command is `tool.hook("execute.before")` (PLG-23);
  the robust v2 answer is server-side: read `_meta["ai.opencode/sessionID"]` when opencode calls mcpx over MCP (`_meta`
  area).
- **Risk.** High: a naive port injects no session id and session leasing silently degrades to one shared instance.
- **Detail.** v2's `create.before` env starts from the session environment if one was set (PLG-15), else `process.env`,
  plus `TERM` and `OPENCODE_TERMINAL=1`.
- **Sources.** `v1:packages/plugin/src/index.ts:270`; `v1:packages/opencode/src/tool/shell.ts:418`; `v2:packages/plugin/src/promise/shell.ts:3`; `v2:packages/core/src/shell.ts:268` "yield* hooks.trigger(\"shell\", \"create.before\", invocation)"; `v2:packages/core/src/tool/plugin/shell.ts:205` "metadata: { sessionID: context.sessionID },"; `v2:packages/plugin/src/promise/tool.ts:40`

## PLG-14 v1 `shell.env` has no session id on PTY terminals

- **What.** v1 fires `shell.env` from three places; the PTY paths (the TUI terminal, HTTP `pty.create`) pass only
  `{cwd}`.
- **Where.** opencode v1.
- **mcpx @ 05c78b2.** The hook comment says it "receives exactly `{ cwd, sessionID?, callID? }`"
  (`plugin/opencode/mcpx-session.ts:44`), which is right, but `mcpx` run inside an opencode terminal gets no
  `MCPX_SESSION_ID`.
- **Value to mcpx.** + low: explains "unknown" sessions in mcpx logs from terminals.
- **Effort.** S (document).
- **Risk.** Low.
- **Detail.** `docs/opencode-plugin.md:411-412` says `shell.env` carries `sessionID` and `callID`; the source passes only
  `cwd` on PTY paths.
- **Sources.** `v1:packages/opencode/src/plugin/pty-environment.ts:18`; `v1:packages/opencode/src/server/routes/instance/httpapi/handlers/pty.ts:71`; `v1:packages/opencode/src/session/prompt.ts:554`; `docs/opencode-plugin.md:411`

## PLG-15 v2 per-session environment route replaces the whole env

- **What.** `PUT /api/experimental/session/:sessionID/environment {variables}` stores a per-session map that replaces
  `process.env` for that session's shell commands. The v2 TUI itself calls it with the terminal's environment whenever a
  session view is active.
- **Where.** opencode v2 only; stored in an in-memory global map (lost on daemon restart); ignored when the Location has
  a workspace id.
- **mcpx @ 05c78b2.** Not used. `docs/opencode-plugin.md` proposes it as the v2 replacement for `shell.env`.
- **Value to mcpx.** − as a plugin channel it is unusable: a plugin that PUTs `{MCPX_SESSION_ID}` wipes `PATH` and `HOME`
  for that session and is overwritten by the TUI's next update; and a v2 plugin has no HTTP client to call it with.
- **Effort.** S (to drop the plan).
- **Risk.** High if adopted: shell commands break.
- **Detail.** `docs/opencode-plugin.md:86` and `:412` call it "the API v2 added for this"; the route's own description is
  "Replace the process environment used by local shell commands for this session."
- **Sources.** `v2:packages/protocol/src/groups/session.ts:795`; `v2:packages/core/src/shell.ts:263` "...(sessionEnvironment ?? process.env),"; `v2:packages/core/src/shell.ts:254`; `v2:packages/core/src/session/environment.ts:18`; `v2:packages/tui/src/app.tsx:511`; `docs/opencode-plugin.md:86`

## PLG-16 `experimental.chat.system.transform` output is `{system: string[]}`

- **What.** v1 triggers the hook with `output = { system }`, an array of strings. mcpx's handler reads `output.parts`,
  finds nothing, and returns, so the plugin's `instructions` option has never injected anything.
- **Where.** opencode v1 (both trigger sites pass `{system}`); v2 has no hook of this name.
- **mcpx @ 05c78b2.** `const parts = (output as any)?.parts; if (Array.isArray(parts)) parts.push({ type: "text", text })`
  (`plugin/opencode/mcpx-session.ts:745-746`, hook at `:737`).
- **Value to mcpx.** + med: a one-line fix, `output.system.push(text)`.
- **Effort.** S.
- **Risk.** Med: users who set `instructions: true` believe the model is told about mcpx; it is not.
- **Detail.** v1 request preparation re-joins extra system entries into one message when a plugin pushes more than one
  (`v1:packages/opencode/src/session/llm/request.ts:74`). The v2 analogue is `session.hook("context")`, whose `system` is
  `Array<SystemPart>` (`{type: "text", text}`): the shape mcpx pushes, on a different key (PLG-17).
- **Sources.** `v1:packages/plugin/src/index.ts:291`; `v1:packages/opencode/src/session/llm/request.ts:69`; `v1:packages/opencode/src/agent/agent.ts:381`; `plugin/opencode/mcpx-session.ts:745` "const parts = (output as any)?.parts"; `v2:packages/plugin/src/promise/session.ts:28` "system: Array<SystemPart>"

## PLG-17 v2 `session.hook("context")` edits system, messages, tools

- **What.** The v2 `context` event carries `system`, `messages`, `options` and `tools` (description and input schema).
  After the hook, tool entries that match neither an original definition nor an original name are dropped. v1 spreads
  this over `experimental.chat.system.transform`, `experimental.chat.messages.transform` and `tool.definition`.
- **Where.** opencode v2 (one hook); v1 (three hooks, and `tool.definition` applies only to registry tools, never to MCP
  tools).
- **mcpx @ 05c78b2.** Not used.
- **Value to mcpx.** + med: the right place for a short "mcpx exists" line in v2. − it cannot add a catalogue pseudo-tool.
- **Effort.** S.
- **Risk.** Med: `docs/opencode-plugin.md:348` suggests putting a budgeted catalogue into `event.tools`; the source drops
  invented entries.
- **Detail.** Tools can be renamed (moved to a new key) and re-described; execution maps back by object identity.
- **Sources.** `v2:packages/plugin/src/promise/session.ts:35`; `v2:packages/core/src/session/model-request.ts:220` "// Match by identity first, then by key. Entries matching neither were invented by a"; `v2:packages/core/src/session/model-request.ts:361`; `v1:packages/plugin/src/index.ts:282`; `v1:packages/opencode/src/tool/registry.ts:318`; `docs/opencode-plugin.md:348`

## PLG-18 `chat.message` vs `session.hook("prompt")`

- **What.** v1's `chat.message` hook becomes v2's `session.hook("prompt")`.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Not used.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** Listed in the plugin-API matrix only; the payload shapes were not compared.
- **Sources.** `v1:packages/plugin/src/index.ts:234`; `v2:packages/plugin/src/promise/session.ts:114`

## PLG-19 `chat.params`/`chat.headers` vs `context.options`/`model.request.headers`

- **What.** v1's per-request LLM parameter and header hooks map to v2 `context` (mutable `options`: typed generation keys
  plus provider options) and `model.request` (mutable `headers`, `baseURL`). v2 adds `http.request`, `http.response`,
  `retry`, `title`, `generate`, `compaction` and `experimental.ws.handshake`.
- **Where.** opencode v1 and v2, different shapes.
- **mcpx @ 05c78b2.** Not used.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** v2's `SessionRequestKind` distinguishes `primary | compaction | title | generate`.
- **Sources.** `v1:packages/plugin/src/index.ts:247`; `v1:packages/plugin/src/index.ts:257`; `v2:packages/plugin/src/promise/session.ts:115`; `v2:packages/plugin/src/promise/session.ts:119`; `v2:packages/plugin/src/promise/session.ts:117`; `v2:packages/core/src/session/model-request.ts:257`

## PLG-20 `experimental.session.compacting` vs `session.hook("compaction")`

- **What.** v1's compaction hook becomes v2's `session.hook("compaction")`.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Not used.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** v1's companion `experimental.compaction.autocontinue` has no v2 hook (PLG-21).
- **Sources.** `v1:packages/plugin/src/index.ts:305`; `v2:packages/plugin/src/promise/session.ts:116`

## PLG-21 v1 hooks with no v2 hook of the same purpose

- **What.** `experimental.text.complete`, `experimental.provider.small_model`, `experimental.compaction.autocontinue`,
  `command.execute.before` and `config` have no v2 hook; `auth`/`provider` are replaced by `integration`, `provider` and
  `model` transforms. The v2 hook set is `session.{prompt, context, compaction, generate, title, model.request,
  http.request, http.response, experimental.ws.handshake, retry}`, `tool.{execute.before, execute.after}`,
  `shell.create.before`, `permission.evaluate`, `aisdk.{sdk, language}`, plus domain transforms.
- **Where.** opencode v1 only for the listed names; `aisdk` hooks are v2-only.
- **mcpx @ 05c78b2.** Uses none of them.
- **Value to mcpx.** + low: confirms mcpx's four hooks are the only ones to port.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** v1's `config` hook runs once after load and is the only way a v1 plugin can add MCP servers (PLG-34).
- **Sources.** `v1:packages/plugin/src/index.ts:327`; `v1:packages/plugin/src/index.ts:297`; `v1:packages/plugin/src/index.ts:316`; `v1:packages/plugin/src/index.ts:262`; `v1:packages/opencode/src/plugin/index.ts:247`; `v2:packages/plugin/src/README.md:68`; `v2:packages/plugin/src/promise/session.ts:113`; `v2:packages/plugin/src/promise/aisdk.ts:5`

## PLG-22 `event` hook vs `ctx.event.subscribe()` stream

- **What.** v1 pushes every bus event for the instance directory to `hook.event({event})`; v2 exposes
  `event.subscribe()`, a stream of server and rpc events.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Not used.
- **Value to mcpx.** + low: a v2 plugin could watch `mcp.*` status events to know when mcpx connects.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** v1 fires `void hook.event(...)` without awaiting it.
- **Sources.** `v1:packages/opencode/src/plugin/index.ts:259`; `v2:packages/core/src/plugin/host.ts:256`

## PLG-23 `tool.execute.before`: v2 adds session and mutable input

- **What.** v1 `tool.execute.before(input {tool, sessionID, callID}, output {args})`; v2 `execute.before {tool,
  sessionID, agent, messageID, id, input}`, where the possibly replaced `event.input` is what executes, for direct and
  code-mode calls alike.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Not used.
- **Value to mcpx.** + med: the only v2 hook that sees both the session id and a shell command before it runs; a plugin
  could prefix the `shell` tool's command or record `sessionID → callID` for correlation.
- **Effort.** M.
- **Risk.** Med: rewriting commands is visible in the transcript; correlating with `create.before` races under
  concurrency.
- **Detail.** v2's shell tool is named `shell` (`v2:packages/core/src/tool/plugin/shell.ts:22`).
- **Sources.** `v1:packages/plugin/src/index.ts:266`; `v2:packages/plugin/src/promise/tool.ts:38`; `v2:packages/core/src/tool.ts:102`; `v2:packages/core/src/tool.ts:257` "const event = yield* beforeExecute(input.call.name, input.call.input, context)"

## PLG-24 `tool.execute.after`: success-only vs status union

- **What.** v1 fires `tool.execute.after(input {tool, sessionID, callID, args}, output)` only after a successful execute;
  for MCP tools `output` is the raw `CallToolResult`, not `{title, output, metadata}`. v2 fires `execute.after` for both
  outcomes with `status: "completed"` (result) or `"error"` (error).
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** Records timing into mcpx's log (`plugin/opencode/mcpx-session.ts:773`) and reads
  `(output as any)?.title` (`:780`), which is always undefined for MCP tools in v1; failures are never recorded.
- **Value to mcpx.** + low: v2 lets mcpx record failures.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** v1 errors skip the hook entirely (it is triggered after `item.execute` returns). In v2 an MCP `isError`
  becomes a `ToolFailure` that does reach this hook with `status: "error"`. Both versions fire it per child call in code
  mode (PLG-36).
- **Sources.** `v1:packages/plugin/src/index.ts:274`; `v1:packages/opencode/src/session/tools.ts:421`; `v2:packages/plugin/src/promise/tool.ts:46`; `v2:packages/core/src/tool.ts:131`; `plugin/opencode/mcpx-session.ts:780` "title: (output as any)?.title,"

## PLG-25 `permission.ask` declared but dead vs live `evaluate` hook

- **What.** v1 declares a `"permission.ask"` hook that no code triggers; v2 triggers `permission`/`evaluate` with a
  mutable `effect` and `message`.
- **Where.** v1 declared only; v2 live.
- **mcpx @ 05c78b2.** Not used.
- **Value to mcpx.** + low: a v2 plugin could auto-allow mcpx tools.
- **Effort.** S.
- **Risk.** Low: implementing the v1 hook is a silent no-op.
- **Detail.** The only `permission.ask(` call in v1 is the permission service itself, not a plugin trigger.
- **Sources.** `v1:packages/plugin/src/index.ts:261`; `v1:packages/opencode/src/session/processor.ts:372`; `v2:packages/core/src/permission.ts:174` "const event = yield* hooks.trigger(\"permission\", \"evaluate\", {"

## PLG-26 Defining a tool: zod map vs `ctx.tool.transform` editor

- **What.** v1 plugin tools are a `{[name]: ToolDefinition}` map with zod raw-shape `args` (`tool.schema = z`) returning a
  string or `{title, output, metadata, attachments}`. v2 tools are added through `ctx.tool.transform(e => e.add({name,
  options: {namespace, codemode, permission, pinned}, description, input, output, execute}))`, where input and output are
  Effect Schema, Standard Schema (zod 4 qualifies) or JSON Schema, and `execute` returns `{output, content, metadata}`.
- **Where.** opencode v1 map; v2 editor.
- **mcpx @ 05c78b2.** v1 map (`plugin/opencode/mcpx-session.ts:762`) with zod args (`:499`): `mcpx_daemon_*`,
  `mcpx_discover`, `mcpx_exec`, `mcpx_observe`.
- **Value to mcpx.** + high: v2 gives typed structured output. − a rewrite.
- **Effort.** M.
- **Risk.** Med: v2 rejects names outside `^[A-Za-z0-9_-]{1,64}$`.
- **Detail.** v1 plugin tools are not namespaced (id = map key), while v1 file tools in `{tool,tools}/*.{js,ts}` get
  `<file>_<export>`. v2 namespaces only when `options.namespace` is set (id `ns_name`, dots become `_`).
  `docs/opencode-plugin.md:78` calls v2 tools "namespaced and typed"; the namespace is optional, and without
  `codemode: false` the tool disappears into code mode (PLG-27).
- **Sources.** `v1:packages/plugin/src/tool.ts:45`; `v1:packages/plugin/src/tool.ts:52` "tool.schema = z"; `v1:packages/opencode/src/tool/registry.ts:202`; `v2:packages/plugin/src/promise/tool.ts:66`; `v2:packages/core/src/tool/runtime.ts:276`; `v2:packages/core/src/tool.ts:297`; `docs/opencode-plugin.md:78`

## PLG-27 v2 plugin tools hide behind code mode by default

- **What.** In v2 every registered tool whose `options.codemode !== false` is removed from the model's direct tool list
  and exposed only inside the `execute` tool; built-ins opt out one by one with `codemode: false`.
- **Where.** opencode v2 only. v1's code mode wraps MCP tools only, never plugin tools.
- **mcpx @ 05c78b2.** n/a (v1 plugin).
- **Value to mcpx.** + high: a v2 port must set `options: { codemode: false }` on `mcpx_daemon_*` and `mcpx_exec`, or they
  vanish behind `execute`.
- **Effort.** S.
- **Risk.** High: tools the model should call directly become code-mode-only functions.
- **Detail.** `execute` is a reserved name for a direct tool. Code mode itself can be switched off only by a
  wholly-denying permission rule on `execute`.
- **Sources.** `v2:packages/core/src/tool.ts:228` "const direct = new Map(Array.from(active).filter(([, tool]) => tool.options?.codemode === false))"; `v2:packages/core/src/tool.ts:229`; `v2:packages/core/src/tool.ts:299`; `v2:packages/core/src/tool/plugin/shell.ts:191` "options: { codemode: false },"

## PLG-28 A plugin tool can ask the user

- **What.** v1 `ToolContext.ask({permission, patterns, always, metadata})` raises a permission prompt. v2's tool context
  is `{sessionID, agent, messageID, id, progress}`, with no `ask`, and the permission domain offers list, get, reply,
  rules and the hook, but no ask.
- **Where.** opencode v1 only.
- **mcpx @ 05c78b2.** `mcpx_daemon_select` confirms through `ctx.ask` (`plugin/opencode/mcpx-session.ts:529`).
- **Value to mcpx.** − the only v1 server-side interactive channel is gone.
- **Effort.** M — a v2 TUI plugin plus `ctx.rpc` (PLG-30, PLG-31), or drop the confirmation.
- **Risk.** Med.
- **Detail.** `docs/opencode-plugin.md:272` lists "a tool with a permission prompt (server plugin, v1 and v2)"; v2's tool
  context has no `ask`. v2 plugin tools are gated only by wholly-disabling permission rules on
  `options.permission ?? name`.
- **Sources.** `v1:packages/plugin/src/tool.ts:19` "ask(input: AskInput): Promise<void>"; `v1:packages/opencode/src/tool/registry.ts:150`; `v2:packages/schema/src/tool.ts:14`; `v2:packages/core/src/tool.ts:225`; `docs/opencode-plugin.md:272`

## PLG-29 Toast from a server plugin

- **What.** v1 plugins call `client.tui.showToast`. A v2 server plugin has no `client`, and the v2 HTTP protocol has no
  toast route; toasts exist only in the TUI plugin API (`ui.toast.show`).
- **Where.** v1 server plugin: yes. v2 server plugin: no. v2 TUI plugin: yes.
- **mcpx @ 05c78b2.** Boot and ambiguity toast via `client.tui.showToast` (`plugin/opencode/mcpx-session.ts:298`).
- **Value to mcpx.** + med: on v2 ambiguity must go on tool results (already done through `annotate`) or through a TUI
  plugin.
- **Effort.** M — a v2 TUI plugin plus `ctx.rpc`.
- **Risk.** Med.
- **Detail.** `docs/opencode-plugin.md:111` and `:361` say the server plugin's `client.tui.*` is "identical in v1 and
  v2"; v2's server context has no `client`. v2 defines an ephemeral `tui.toast.show` event schema, but the plugin
  `event` domain is subscribe-only.
- **Sources.** `v1:packages/sdk/js/src/gen/sdk.gen.ts:1118` "public showToast"; `v2:packages/core/src/plugin/host.ts:255`; `v2:packages/plugin/src/tui/context.ts:279` "show(options: ToastOptions): void"; `v2:packages/schema/src/tui-event.ts:42`; `docs/opencode-plugin.md:111`

## PLG-30 TUI plugin shape and dialogs

- **What.** v1 TUI plugins default-export `{id?, tui(api, options, meta)}` and render `api.ui.DialogSelect` through
  `api.ui.dialog.replace`. v2 TUI plugins default-export `{id, setup(ctx)}` with promise dialogs
  (`ui.dialog.select<V>()`, `prompt()`, `confirm()`, `alert()`), `ui.toast.show()` and `keymap.layer(() => …)`.
- **Where.** opencode v1 and v2, incompatible.
- **mcpx @ 05c78b2.** v1 only (`plugin/opencode/mcpx-tui.tsx:123`, `:189`, `:206`).
- **Value to mcpx.** + med: v2's promise dialogs are simpler than JSX dialogs, and a v2 TUI plugin is where toasts and
  confirmations must now live.
- **Effort.** M.
- **Risk.** Low today.
- **Detail.** v1 forbids `server` and `tui` in one module; v2 packages declare `server`, `tui` and `rpc` entrypoints side
  by side (`v2:packages/plugin/src/host.ts:43`).
- **Sources.** `v1:packages/plugin/src/tui.ts:630`; `v1:packages/plugin/src/tui.ts:604`; `v2:packages/plugin/src/tui/plugin.ts:7`; `v2:packages/plugin/src/tui/context.ts:379`; `v2:packages/plugin/src/tui/context.ts:440`

## PLG-31 `ctx.rpc` between server and TUI plugins

- **What.** v2 server plugins can `rpc.register(definition, handlers)` and emit events; a plugin package can ship an `rpc`
  entrypoint. It is the only channel a v2 server plugin has to UI.
- **Where.** opencode v2.
- **mcpx @ 05c78b2.** Uses the filesystem as the channel between the server and TUI plugins
  (`plugin/opencode/mcpx-tui.tsx:17`).
- **Value to mcpx.** + med: replaces the remember-file hand-off.
- **Effort.** M.
- **Risk.** Low.
- **Detail.** `RpcDomain.register` returns `{events.emit}` for push.
- **Sources.** `v2:packages/plugin/src/promise/rpc.ts:27`; `v2:packages/core/src/plugin/host.ts:119`

## PLG-32 Plugin storage

- **What.** v1 server plugins have no persistence API (v1 TUI plugins have `kv`); v2 server plugins have
  `storage.get/set/remove/scan`, namespaced per plugin id, and v2 TUI plugins have `storage.store/memory`.
- **Where.** opencode v1 (TUI only) and v2.
- **mcpx @ 05c78b2.** Remembers the daemon choice in its own file (`plugin/opencode/mcpx-session.ts:455`).
- **Value to mcpx.** + low: the remember file can go on v2; sharing between TUI and server still needs `rpc` or a file.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** The namespace is `plugin:<hex of id>:`.
- **Sources.** `v1:packages/plugin/src/tui.ts:611` "kv: TuiKV"; `v2:packages/plugin/src/promise/storage.ts:4`; `v2:packages/core/src/plugin/host.ts:450`; `v2:packages/core/src/plugin/host.ts:578`

## PLG-33 Calling a model from a plugin

- **What.** v1 plugins can only drive an agent turn (`client.session.prompt`/`promptAsync`/`command`). v2 plugins have
  `ctx.generate.text({prompt, model?}) → {text}` and `ctx.session.generate`, also exposed as `POST /api/generate`.
- **Where.** opencode v1 (full turn) and v2 (one-shot).
- **mcpx @ 05c78b2.** Not examined (the sampling area covers what mcpx forwards).
- **Value to mcpx.** + med: a v2 plugin could answer an MCP `sampling/createMessage` that mcpx forwards.
- **Effort.** M.
- **Risk.** Low.
- **Detail.** `generate.text` takes a plain prompt string: no messages, system prompt or tool use, so a sampling request
  with `messages[]`, `systemPrompt` or `tools` must be flattened.
- **Sources.** `v1:packages/sdk/js/src/gen/sdk.gen.ts:615`; `v2:packages/core/src/plugin/host.ts:271`; `v2:packages/protocol/src/groups/generate.ts:8`; `v2:packages/core/src/generate.ts:10`

## PLG-34 Plugins add or change MCP servers (Location-scoped)

- **What.** v2 `ctx.mcp.transform(editor => editor.set/update/remove)` and `ctx.mcp.reload()` edit the Location's MCP
  server set; `ctx.mcp.list()` reads status. A v1 plugin can only mutate config through the `config` hook, once.
- **Where.** opencode v2. Both versions also expose runtime MCP registration over HTTP (v1 `POST /mcp`; v2
  `PUT /api/experimental/mcp/:server`, which needs the server password).
- **mcpx @ 05c78b2.** Not used.
- **Value to mcpx.** + med: a v2 plugin could register mcpx itself with `codemode: false` and `protocol: "auto"`. v2's own
  built-in `opencode.mcp.codemode.exclusion` plugin uses exactly this editor (CODE-53).
- **Effort.** S.
- **Risk.** Med: the scope is the Location, shared by all sessions and windows.
- **Detail.** `docs/opencode-plugin.md:346` says "registering MCP servers per session (`mcp.transform`)"; the MCP service
  is a Location node, and reconcile replaces changed servers.
- **Sources.** `v2:packages/plugin/src/promise/mcp.ts:15`; `v2:packages/core/src/plugin/host.ts:384`; `v2:packages/core/src/mcp/index.ts:736` "return makeLocationNode({"; `v1:packages/opencode/src/server/routes/instance/httpapi/groups/mcp.ts:55`; `v2:packages/protocol/src/groups/mcp.ts:24`; `docs/opencode-plugin.md:346`

## PLG-35 `?codemode=false` appended to remote MCP URLs

- **What.** For a remote server whose config does not set `codemode: false` and whose URL has no `codemode` parameter, v2
  adds `codemode=false` to the query, because "Servers that bundle their own Code Mode (Cloudflare and others) expose raw
  tools when asked". On HTTP 400 or 404 it retries once at the configured URL.
- **Where.** opencode v2 (client side). No Cloudflare document describing the parameter was found.
- **mcpx @ 05c78b2.** Neither sends nor honours it: `/mcp` (`proto.mcpPath`, `internal/defaults/defaults.json:77`) always
  exposes mcpx's meta-tools (`internal/mcpserver/server.go:1-13`). Whether `/mcp` tolerates unknown query parameters was
  not examined.
- **Value to mcpx.** + med: honouring it by exposing every upstream tool flat (namespaced) would let v2's code mode
  orchestrate mcpx's pooled servers directly, keeping mcpx's per-caller isolation under v2's catalogue. As a client, mcpx
  could send it to Cloudflare-style servers so its own codegen sees raw tools.
- **Effort.** M — a flat-tools mode plus a query switch.
- **Risk.** Med: a flat mode lists hundreds of tools, the context cost mcpx exists to avoid; it should activate only on
  request. A strict endpoint costs v2 a failed first connect on every start.
- **Detail.** The configured URL, without the parameter, stays the OAuth identity; opencode's SDK patch keeps the
  parameter out of metadata discovery. With `codemode: false` set (CODE-53) the parameter is never added.
- **Also recorded from the transports register.** The endpoint is routed by path (`internal/daemon/server.go:488`), so
  the query string is ignored and mcpx's meta-tools are served regardless; mcpx as a client never sends the parameter.
  The configured URL, without the parameter, stays the OAuth identity; v2's SDK patch stops the parameter leaking into
  metadata discovery.
- **Sources.** `v2:packages/core/src/mcp/client.ts:214` "// Servers that bundle their own Code Mode (Cloudflare and others) expose raw tools when asked"; `v2:packages/core/src/mcp/client.ts:217-218` "if (addedCodemode) url.searchParams.set(\"codemode\", \"false\")"; `v2:packages/core/src/mcp/client.ts:228-233`; `internal/defaults/defaults.json:77`; `v2:packages/core/src/mcp/client.ts:217`; `v2:packages/core/src/mcp/client.ts:218` "if (addedCodemode) url.searchParams.set(\"codemode\", \"false\")"; `v2:packages/core/src/mcp/client.ts:233`.

## PLG-36 Hooks and permission checks per child call in code mode

- **What.** Each MCP call made inside opencode's `execute` goes through opencode's permission system and fires the plugin
  hooks. v1 fires `ctx.ask({permission: key})` and `tool.execute.before/after` with `callID = "<parent>/<n>"`; v2 routes
  child calls through `beforeExecute` and `executeTool`, so `permission.assert` and `execute.before/after` fire per child.
  User rules such as "deny chrome_devtools_*" therefore apply inside code.
- **Where.** opencode v1 and v2. Calls inside `mcpx_exec` or `mcpx exec` go script → daemon → server, and opencode sees one
  `mcpx_exec` or `bash` call. Cloudflare connectors can set `requiresApproval`; lootbox has nothing.
- **mcpx @ 05c78b2.** The plugin only forwards opencode's own tool outcomes to mcpx's log
  (`plugin/opencode/mcpx-session.ts:773-789`); nothing flows from daemon calls to opencode's permission system.
- **Value to mcpx.** + high: closes a policy bypass: a tool denied in opencode is still reachable through `mcpx exec`.
- **Effort.** L — the daemon would ask the harness per call through an elicitation-style round trip to the plugin
  (`docs/elicitation.md` §5.5).
- **Risk.** High: today mcpx is a way around opencode permissions.
- **Detail.** v1 builds the permission key from the MCP tool key (`server_tool`), so rules written for direct MCP tools
  apply unchanged inside code mode. mcpx's timing hook would see v1 child calls (PLG-24).
- **Sources.** `v1:packages/opencode/src/tool/code-mode.ts:141-145`; `v1:packages/opencode/src/tool/code-mode.ts:147`; `v1:packages/opencode/src/tool/code-mode.ts:227`; `v2:packages/core/src/tool/mcp.ts:51-63`; `v2:packages/core/src/tool.ts:235`; `plugin/opencode/mcpx-session.ts:773-789`

## PLG-37 Live status of each child call during a script

- **What.** While `execute` runs, opencode publishes `toolCalls: [{tool, status: running|completed|error, input}]` as tool
  metadata (v1 `ctx.metadata`) or progress (v2 `context.progress`), and the TUI shows it.
- **Where.** opencode v1 and v2. mcpx's daemon has an event stream and `/v1/exec` can stream frames, but the plugin does
  not use them; lootbox has nothing.
- **mcpx @ 05c78b2.** The plugin's `mcpx_exec` makes one blocking POST and publishes nothing; its `execute(args)` does
  not even take the tool context (`plugin/opencode/mcpx-session.ts:615-625`; `plugin/opencode/mcpx/daemon.ts:451-455`).
- **Value to mcpx.** + med: a user watching a two-minute browser script sees nothing until it ends.
- **Effort.** S — read `/v1/exec` with `output: "stream"` and map log and emit frames to metadata.
- **Risk.** Low.
- **Detail.** None beyond the above.
- **Sources.** `v1:packages/opencode/src/tool/code-mode.ts:216-217`; `v2:packages/core/src/codemode/tool.ts:85-88`; `plugin/opencode/mcpx-session.ts:615-625`

## PLG-38 Cancelling in the harness stops the script

- **What.** When the user cancels, opencode aborts the running program: v1 races the interpreter against `ctx.abort` and
  passes `signal: ctx.abort` to each `callTool`; v2 interrupts the Effect fiber. The mcpx plugin's `mcpx_exec` ignores
  the abort signal, so the daemon-side script runs until it finishes or times out.
- **Where.** opencode v1 and v2. mcpx CLI: a killed CLI process kills its local script's process group. lootbox: the
  CLI's WebSocket closes but the daemon's `deno` keeps running. Cloudflare: unverified.
- **mcpx @ 05c78b2.** `postRaw` takes only an optional timeout, and `exec()` passes neither timeout nor signal
  (`plugin/opencode/mcpx/daemon.ts:380-386`, `:452-455`). The daemon would cancel on client disconnect
  (`internal/daemon/routes_exec.go:252`), but the plugin never disconnects.
- **Value to mcpx.** + med: Esc on a runaway browser script should stop it.
- **Effort.** S — pass the tool context's abort signal to `fetch`.
- **Risk.** Med: runaway scripts keep holding pooled browsers.
- **Detail.** The progress-cancellation area records how an HTTP disconnect propagates to the upstream call.
- **Sources.** `v1:packages/opencode/src/tool/code-mode.ts:262-274`; `v1:packages/opencode/src/tool/code-mode.ts:155` "signal: input.ctx.abort,"; `plugin/opencode/mcpx/daemon.ts:380-386`; `internal/daemon/routes_exec.go:252`

## PLG-39 Harness session and working directory reach the script

- **What.** opencode v2 passes the session id to every child MCP call as `_meta["ai.opencode/sessionID"]`. mcpx's plugin
  sets `MCPX_SESSION_ID` for `bash`, so `mcpx exec` from bash is session-scoped; but the plugin's own `mcpx_exec` posts
  `/v1/exec` with no `session` and no `cwd`, so each call gets a fresh `exec-<runID>` session and the daemon's working
  directory.
- **Where.** opencode v2; mcpx bash path yes, plugin tool path no.
- **mcpx @ 05c78b2.** `exec()` sends only `{source, options: {output: "structured"}}`
  (`plugin/opencode/mcpx/daemon.ts:452-455`); with `options.session` empty the service falls back to `"exec-"+runID`
  (`internal/execsvc/execsvc.go:382`).
- **Value to mcpx.** + high: with the session, a browser persists across `mcpx_exec` calls in one opencode session, which
  `AGENTS.md:87-89` promises; with `cwd`, `repo` and `cwd` scopes resolve to the right repository.
- **Effort.** S — pass `ctx.sessionID` and `ctx.directory` as `options.session` and `options.cwd`.
- **Risk.** High: repo-scoped servers resolve against the daemon's directory, and session-scoped browsers are recreated
  on every call and linger (process-model area).
- **Detail.** `call()` in the same class already accepts a session (`plugin/opencode/mcpx/daemon.ts:462-467`).
- **Sources.** `plugin/opencode/mcpx/daemon.ts:452-455`; `internal/execsvc/execsvc.go:382` "session := firstNonEmpty(opts.Session, ropts.Env[\"MCPX_SESSION\"], \"exec-\"+runID)"; `plugin/opencode/mcpx-session.ts:664-670`; `v2:packages/core/src/tool/mcp.ts:69` "sessionID: context.sessionID,"

## PLG-40 Plugin `mcpx_exec` reads a field the daemon never returns

- **What.** The plugin returns `out.output ?? pretty(out.result ?? out)`. `execsvc.Result` has no `output` field (its
  fields are `result`, `stdout`, `logs`, `emits`, `artifacts` and the rest), so when stdout is not JSON the model sees
  the entire envelope instead of the script's stdout.
- **Where.** mcpx plugin only. opencode's own `execute` returns the value, then warnings, then logs.
- **mcpx @ 05c78b2.** `plugin/opencode/mcpx-session.ts:619-623`; `Result` fields at `internal/execsvc/execsvc.go:201-213`.
  The client type even declares `output?: string` (`plugin/opencode/mcpx/daemon.ts:451`).
- **Value to mcpx.** + med: the model should see stdout, then error, then compact logs, not `runId`, `emits: []`,
  `artifacts: []` and `durationMs` on every call.
- **Effort.** S — use `out.stdout` (or `out.error`).
- **Risk.** Med: every `mcpx_exec` wastes tokens, against the point of the feature.
- **Detail.** A declared behaviour that is not delivered (build-brief rule 8), verified by reading only. The fallback used
  when the daemon route is absent (`viaCLI(["exec", …])`) returns what the CLI prints, so the two paths show the model
  different things for the same script.
- **Sources.** `plugin/opencode/mcpx-session.ts:621-623` ": (out.output ?? pretty(out.result ?? out))"; `internal/execsvc/execsvc.go:201-212`; `v2:packages/core/src/codemode/tool.ts:293-305`

## PLG-41 Code execution offered as an MCP server

- **What.** mcpx serves `mcpx_catalog`, `mcpx_search`, `mcpx_types`, `mcpx_exec`, `mcpx_call` and more over `/mcp`, and
  also runs scripts from the CLI and `/v1`. Cloudflare publishes code mode as an MCP server two ways: `codeMcpServer()`
  (one `code` tool with the upstream typed in its description) and `openApiMcpServer()` (`search` plus `execute` over an
  OpenAPI document kept in the sandbox). opencode's code mode is reachable only from a model turn: no CLI, HTTP or MCP
  route. lootbox has never been an MCP server.
- **Where.** mcpx and Cloudflare. The label counts the products this register tracks with labels (opencode, lootbox);
  Cloudflare has it too.
- **mcpx @ 05c78b2.** `internal/mcpserver/server.go:1-13`, `:327-337`.
- **Value to mcpx.** + med: a fixed-footprint surface any MCP host can use; Cloudflare's "search the spec from code" idea
  is what `mcpx_exec` with in-script `search()`/`describe()` already allows.
- **Effort.** S (done).
- **Risk.** Low.
- **Detail.** Cloudflare's argument for server-side code mode names the CLI approach mcpx takes and rejects it for the
  web ("the agent needs a shell, which not every environment provides"); mcpx's counter-argument (local-first,
  stateful servers, full authority) is not yet written down in its docs. In opencode v2 the only code-mode mention in
  protocol code is a simulation schema; `POST /api/generate` is text generation, not code execution.
- **Sources.** `internal/mcpserver/server.go:327`; `v2:packages/protocol/src/simulation.ts:500`; `v2:packages/protocol/src/groups/generate.ts:8`; <https://developers.cloudflare.com/agents/tools/codemode/api-reference/#codemcpserver> "Wraps an existing MCP server with one `code` tool."; <https://developers.cloudflare.com/agents/model-context-protocol/codemode/#search-and-execute> "`search` runs generated code against an OpenAPI document."
