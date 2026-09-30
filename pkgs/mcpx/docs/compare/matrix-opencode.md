# opencode v1 vs v2

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare, area:opencode
description:  the plugin API, the MCP client, code mode and the process
              model of opencode 1.18.31 and 2.0.3, side by side, as they
              bear on mcpx.
```

The two opencodes pinned in this repository: **v1** is 1.18.31
(`v1:` = the source at `/nix/store/xczwr0rfqkd99yjg4aqv28yfxhim4pg1-source`),
**v2** is 2.0.3 (`v2:` = `nix eval --raw .#opencode2.src`, at
`/nix/store/y08jwgxhq18hm706fisnqx68n39bid9i-source`). Every cell cites the
line it was read from. SDK constants that live outside both trees are cited by
their unpkg URL. Individual differences, with value, effort and risk to mcpx,
are in [register/plugin-apis.md](register/plugin-apis.md),
[register/process-model.md](register/process-model.md) and
[register/code-mode.md](register/code-mode.md); the MCP-client rows are spread
across the protocol areas of the register.

What matters most for mcpx, in one paragraph each:

- **v1 already runs code mode here.** `experimentalCodeMode` is on whenever
  `OPENCODE_EXPERIMENTAL` is set (`v1:packages/opencode/src/effect/runtime-flags.ts:48`),
  and this machine's wrapper sets it (`nix:flake.nix:297`). Every MCP server
  added to v1 sits behind its `execute` tool, mcpx's `/mcp` included.
- **v2 tells mcpx who is calling, and mcpx does not listen.** Every v2
  `tools/call` carries `_meta["ai.opencode/sessionID"]`
  (`v2:packages/core/src/mcp/client.ts:278`), including calls from inside code
  mode. That is the session identity the v1 plugin injects through
  `shell.env`, delivered on the wire, and it is the cleanest way for mcpx to
  give each opencode session its own stateful servers.
- **v2 shares more, not less.** One background server per machine by default,
  one MCP connection per server per Location that never idles out: every
  session in every window in a directory drives the same browser.
- **The v2 plugin surface is a rewrite.** No `client`, no `$`, no `ask` in a
  tool context, no toast from a server plugin, and the only per-session
  environment route replaces the whole environment.

## 1. Plugin API

| item | v1 (1.18.31) | v2 (2.0.3) |
| --- | --- | --- |
| package | `@opencode-ai/plugin` — `v1:packages/plugin/package.json:3` | `@opencode/plugin` — `v2:packages/plugin/package.json:3` |
| config key | `plugin: (string \| [string, opts])[]` — `v1:packages/plugin/src/index.ts:71` | `plugins` native; legacy `plugin` normalized in — `v2:packages/core/src/config/normalize.ts:185` |
| directory discovery | `{plugin,plugins}/*.{ts,js}`, non-recursive — `v1:packages/opencode/src/config/plugin.ts:21` | `plugin/`,`plugins/` files **and** dirs — `v2:packages/core/src/plugin/source-directory.ts:25` |
| npm vs file | npm via `Npm.add`, files via `file://`; `@opencode-ai/plugin` auto-installed into config dirs — `v1:packages/opencode/src/config/config.ts:456` | npm via `npm.add`, local via source loader with watch — `v2:packages/core/src/plugin/module.ts:91` |
| runtime | Bun (`Bun.$` handed in) — `v1:packages/opencode/src/plugin/index.ts:167` | Bun build (source loader uses `Bun.Transpiler`) — `v2:packages/plugin/src/source.bun.ts:27`; Node build exists — `v2:packages/cli/package.json:21` |
| module shape | function export or `{id?, server}`; legacy "every export" — `v1:packages/opencode/src/plugin/index.ts:114` | `{id, setup}` or `{id, effect}` only — `v2:packages/core/src/plugin/module.ts:60` |
| signature | `(input: PluginInput, options?) => Promise<Hooks>` — `v1:packages/plugin/src/index.ts:74` | `setup(ctx: Context) => Cleanup \| void` — `v2:packages/plugin/src/promise/plugin.ts:60` |
| `client` | SDK client (in-process fetch if no port) — `v1:packages/opencode/src/plugin/index.ts:146` | absent — `v2:packages/plugin/src/promise/plugin.ts:26` |
| `project`/`directory`/`worktree` | fields — `v1:packages/plugin/src/index.ts:58` | `ctx.location` — `v2:packages/plugin/src/promise/plugin.ts:28` |
| `serverUrl` | getter — `v1:packages/opencode/src/plugin/index.ts:163` | absent — `v2:packages/plugin/src/promise/plugin.ts:26` |
| `$` shell | `BunShell` — `v1:packages/plugin/src/index.ts:65` | absent — `v2:packages/plugin/src/promise/shell.ts:12` |
| options | 2nd arg — `v1:packages/plugin/src/index.ts:74` | `ctx.options` — `v2:packages/plugin/src/promise/plugin.ts:29` |
| instance scope | per directory — `v1:packages/opencode/src/plugin/index.ts:134` | per Location — `v2:packages/core/src/plugin.ts:296` |
| `dispose` | hook — `v1:packages/plugin/src/index.ts:223` | setup return value — `v2:packages/plugin/src/promise/plugin.ts:60` |
| `event` | hook, pushed — `v1:packages/opencode/src/plugin/index.ts:259` | `ctx.event.subscribe()` — `v2:packages/core/src/plugin/host.ts:256` |
| `config` | hook, once after load — `v1:packages/opencode/src/plugin/index.ts:247` | none; domain transforms instead — `v2:packages/plugin/src/README.md:68` |
| `tool` | map of `tool({args: zod})` — `v1:packages/plugin/src/index.ts:226` | `ctx.tool.transform(e => e.add(…))` — `v2:packages/plugin/src/promise/tool.ts:66` |
| `auth` / `provider` | hooks — `v1:packages/plugin/src/index.ts:229` | `integration`/`provider`/`model` transforms — `v2:packages/plugin/src/README.md:70` |
| `chat.message` | hook — `v1:packages/plugin/src/index.ts:234` | `session.hook("prompt")` — `v2:packages/plugin/src/promise/session.ts:114` |
| `chat.params` | hook — `v1:packages/plugin/src/index.ts:247` | `session.hook("context").options` — `v2:packages/plugin/src/promise/session.ts:30` |
| `chat.headers` | hook — `v1:packages/plugin/src/index.ts:257` | `session.hook("model.request").headers` — `v2:packages/plugin/src/promise/session.ts:69` |
| `permission.ask` | declared, never triggered — `v1:packages/plugin/src/index.ts:261` | `permission.hook("evaluate")`, live — `v2:packages/core/src/permission.ts:174` |
| `command.execute.before` | hook — `v1:packages/plugin/src/index.ts:262` | none (commands are transforms) — `v2:packages/plugin/src/promise/session.ts:113` |
| `tool.execute.before` | `{tool, sessionID, callID}` / `{args}` — `v1:packages/plugin/src/index.ts:266` | `{tool, sessionID, agent, messageID, id, input}` — `v2:packages/plugin/src/promise/tool.ts:38` |
| `shell.env` | `{cwd, sessionID?, callID?}` / `{env}` — `v1:packages/plugin/src/index.ts:270` | `shell.hook("create.before")` `{command, cwd, timeout, shell, env}` — `v2:packages/plugin/src/promise/shell.ts:3` |
| `tool.execute.after` | success only — `v1:packages/plugin/src/index.ts:274` | `status: completed \| error` — `v2:packages/plugin/src/promise/tool.ts:46` |
| `experimental.chat.messages.transform` | hook — `v1:packages/plugin/src/index.ts:282` | `session.hook("context").messages` — `v2:packages/plugin/src/promise/session.ts:29` |
| `experimental.chat.system.transform` | `{system: string[]}` — `v1:packages/plugin/src/index.ts:291` | `session.hook("context").system: SystemPart[]` — `v2:packages/plugin/src/promise/session.ts:28` |
| `experimental.provider.small_model` | hook — `v1:packages/plugin/src/index.ts:297` | none found — `v2:packages/plugin/src/promise/session.ts:113` |
| `experimental.session.compacting` | hook — `v1:packages/plugin/src/index.ts:305` | `session.hook("compaction")` — `v2:packages/plugin/src/promise/session.ts:116` |
| `experimental.compaction.autocontinue` | hook — `v1:packages/plugin/src/index.ts:316` | none found — `v2:packages/plugin/src/promise/session.ts:113` |
| `experimental.text.complete` | hook — `v1:packages/plugin/src/index.ts:327` | none found — `v2:packages/plugin/src/promise/session.ts:113` |
| `tool.definition` | hook (registry tools only) — `v1:packages/opencode/src/tool/registry.ts:318` | `session.hook("context").tools` — `v2:packages/plugin/src/promise/session.ts:35` |
| v2-only session hooks | — | `generate`, `title`, `http.request`, `http.response`, `experimental.ws.handshake`, `retry` — `v2:packages/plugin/src/promise/session.ts:117` |
| v2-only AI SDK hooks | (v2 host present in v1 tree) — `v1:packages/core/src/location-services.ts:52` | `aisdk.hook("sdk" \| "language")` — `v2:packages/plugin/src/promise/aisdk.ts:5` |
| MCP servers from a plugin | `config` hook mutation only | `ctx.mcp.transform/reload/list` (Location) — `v2:packages/plugin/src/promise/mcp.ts:15` |
| toast | `client.tui.showToast` — `v1:packages/sdk/js/src/gen/sdk.gen.ts:1118` | TUI plugin only: `ui.toast.show` — `v2:packages/plugin/src/tui/context.ts:279` |
| ask the user from a tool | `ToolContext.ask` — `v1:packages/plugin/src/tool.ts:19` | none — `v2:packages/schema/src/tool.ts:14` |
| TUI dialogs | `api.ui.DialogSelect` JSX — `v1:packages/plugin/src/tui.ts:604` | `ui.dialog.select/prompt/confirm` promises — `v2:packages/plugin/src/tui/context.ts:379` |
| storage | none (server); `kv` (TUI) — `v1:packages/plugin/src/tui.ts:611` | `ctx.storage` KV — `v2:packages/plugin/src/promise/storage.ts:4` |
| call a model | `client.session.prompt` (full turn) — `v1:packages/sdk/js/src/gen/sdk.gen.ts:615` | `ctx.generate.text` — `v2:packages/core/src/plugin/host.ts:271` |
| server↔TUI channel | filesystem (none built in) | `ctx.rpc` — `v2:packages/plugin/src/promise/rpc.ts:27` |

### 1.1 mcpx's four hooks under v2

| mcpx hook (v1) | mcpx use | v2 equivalent | works as-is? |
| --- | --- | --- | --- |
| `shell.env` — `plugin/opencode/mcpx-session.ts:664` | inject `MCPX_SESSION_ID` etc. | `shell.hook("create.before")` — `v2:packages/plugin/src/promise/shell.ts:12` | **No session id.** Only `tool.hook("execute.before")` sees `sessionID` (`v2:packages/plugin/src/promise/tool.ts:40`); the session-environment route is TUI-owned and replaces env. Best v2 answer is server-side: read `_meta["ai.opencode/sessionID"]`. |
| `experimental.chat.system.transform` — `plugin/opencode/mcpx-session.ts:737` | optional instructions | `session.hook("context")` push `{type:"text", text}` onto `event.system` — `v2:packages/plugin/src/promise/session.ts:28` | Broken in v1 today (writes `parts`, PLUGIN-16). |
| `tool` map — `plugin/opencode/mcpx-session.ts:762` | `mcpx_daemon_*`, `mcpx_discover/exec/observe` | `ctx.tool.transform` with `options.codemode: false` — `v2:packages/core/src/tool.ts:228` | Rewrite; `ctx.ask` unavailable. |
| `tool.execute.after` — `plugin/opencode/mcpx-session.ts:773` | timing into mcpx log | `tool.hook("execute.after")` with `status` — `v2:packages/plugin/src/promise/tool.ts:46` | Rewrite; gains failures. |

## 2. opencode as an MCP client (what mcpx is talking to)

| capability / behaviour | v1 | v2 |
| --- | --- | --- |
| SDK | `@modelcontextprotocol/sdk@1.29.0` + patch — `v1:packages/opencode/package.json:83` | `@modelcontextprotocol/client@2.0.0` + patch — `v2:packages/core/package.json:113` |
| `initialize.protocolVersion` | `2025-11-25` (SDK latest) — `v1:patches/@modelcontextprotocol%2Fsdk@1.29.0.patch:36` | `2025-11-25` unless `protocol` set — `v2:packages/core/src/mcp/client.ts:312` |
| 2026-07-28 / `server/discover` | no — `v1:packages/opencode/src/mcp/index.ts:6` (1.x Client) | opt-in `protocol: auto \| 2026-07-28` — `v2:packages/schema/src/mcp.ts:20` |
| MRTR `input_required` | no | SDK auto-fulfil, 10 rounds (`SDK2C/src-D_zzAWoS.mjs` L5044) — `v2:packages/core/src/mcp/client.ts:157` |
| `_meta` protocolVersion | no | modern era via SDK (`SDK2K/auth-CUe6YdwF.mjs` L24) — `v2:packages/core/src/mcp/client.ts:248` |
| vendor `_meta` on tools/call | none (params are `name`, `arguments`) — `v1:packages/opencode/src/mcp/catalog.ts:56` | `ai.opencode/sessionID` — `v2:packages/core/src/mcp/client.ts:278` |
| transports | stdio, Streamable HTTP, SSE fallback — `v1:packages/opencode/src/mcp/index.ts:269` | stdio (own), Streamable HTTP — `v2:packages/core/src/mcp/client.ts:220` |
| stdio stderr | piped, unread — `v1:packages/opencode/src/mcp/index.ts:348` | drained to debug log — `v2:packages/core/src/mcp/stdio.ts:162` |
| stdio frame cap | none found | 16 MiB — `v2:packages/core/src/mcp/stdio.ts:15` |
| declared `roots` | yes — `v1:packages/opencode/src/mcp/index.ts:46` | yes (legacy only) — `v2:packages/core/src/mcp/client.ts:146` |
| declared `sampling` | no — `v1:packages/opencode/src/mcp/index.ts:42` | no — `v2:packages/core/src/mcp/client.ts:142` |
| declared `elicitation` | no — `v1:packages/opencode/src/mcp/index.ts:44` | form (applyDefaults) + url — `v2:packages/core/src/mcp/client.ts:143` |
| declared `tasks` | no — `v1:packages/opencode/src/mcp/index.ts:48` | no — `v2:packages/core/src/mcp/client.ts:142` |
| `roots/list` handler | one root = directory — `v1:packages/opencode/src/mcp/index.ts:78` | one root = directory — `v2:packages/core/src/mcp/client.ts:155` |
| `elicitation/create` | −32601 (no handler) — `v1:packages/opencode/src/mcp/index.ts:77` | Location-global form — `v2:packages/core/src/mcp/index.ts:82` |
| `notifications/elicitation/complete` | no | yes (legacy url mode) — `v2:packages/core/src/mcp/client.ts:160` |
| `sampling/createMessage` | no | no |
| `tools/list_changed` | refetch — `v1:packages/opencode/src/mcp/index.ts:462` | refetch, 100 ms debounce — `v2:packages/core/src/tool/mcp.ts:140` |
| `prompts/list_changed` | ignored | refetch — `v2:packages/core/src/mcp/index.ts:374` |
| `resources/list_changed` | ignored | event — `v2:packages/core/src/mcp/index.ts:375` |
| `notifications/message` | → opencode log — `v1:packages/opencode/src/mcp/index.ts:457` | ignored — `v2:packages/core/src/mcp/client.ts:140` |
| `logging/setLevel` | never sent — `v1:packages/opencode/src/mcp/index.ts:164` | never sent — `v2:packages/core/src/mcp/client.ts:91` |
| resources to the model | 3 tools if any server has `resources` — `v1:packages/opencode/src/session/tools.ts:136` | none (TUI `@` list only) — `v2:packages/tui/src/component/prompt/autocomplete.tsx:413` |
| `resources/read` | via `read_mcp_resource` — `v1:packages/opencode/src/session/tools.ts:27` | defined, no caller — `v2:packages/core/src/mcp/index.ts:713` |
| `resources/subscribe` | no | no (SDK may `subscriptions/listen` on modern) — `v2:packages/core/src/mcp/client.ts:131` |
| prompts | slash commands, text only — `v1:packages/opencode/src/command/index.ts:105` | slash commands `server:prompt` — `v2:packages/core/src/plugin/command.ts:105` |
| `completion/complete` | never — `v1:packages/opencode/src/mcp/index.ts:164` | never — `v2:packages/core/src/mcp/client.ts:91` |
| server `instructions` | system prompt — `v1:packages/opencode/src/session/system.ts:129` | system prompt (+ code-mode hint) — `v2:packages/core/src/mcp/instructions.ts:23` |
| progress | token = request id, resets timer — `v1:packages/opencode/src/mcp/catalog.ts:61` | token = request id, no reset — `v2:packages/core/src/mcp/client.ts:281` |
| cancellation | AbortSignal → `notifications/cancelled` — `v1:packages/opencode/src/mcp/catalog.ts:62` | AbortSignal → cancelled / HTTP abort — `v2:packages/core/src/mcp/client.ts:273` |
| connect timeout | 30 s — `v1:packages/opencode/src/mcp/index.ts:38` | `timeout.startup` 30 s — `v2:packages/core/src/mcp/client.ts:32` |
| list timeout | 30 s — `v1:packages/opencode/src/mcp/catalog.ts:11` | `timeout.catalog` 30 s — `v2:packages/core/src/mcp/client.ts:33` |
| `tools/call` timeout | per-server / `experimental.mcp_timeout` / SDK 60 s — `v1:packages/opencode/src/mcp/index.ts:672` | `timeout.execution` 12 h — `v2:packages/core/src/mcp/client.ts:34` |
| tool naming | `sanitize(server)_sanitize(tool)` — `v1:packages/opencode/src/mcp/catalog.ts:119` | same, namespace + ≤64 check — `v2:packages/core/src/tool/mcp.ts:17` |
| pagination cap | 1000 pages — `v1:packages/opencode/src/mcp/catalog.ts:12` | 64 pages (`SDK2C/index.mjs` L2904) — `v2:packages/core/src/mcp/client.ts:251` |
| `outputSchema` enforcement | SDK validates; tolerant re-list — `v1:packages/opencode/src/mcp/catalog.ts:155` | not traced |
| text | text — `v1:packages/opencode/src/session/tools.ts:429` | text — `v2:packages/core/src/mcp/client.ts:324` |
| image | attachment — `v1:packages/opencode/src/session/tools.ts:430` | media — `v2:packages/core/src/mcp/client.ts:325` |
| audio | dropped (direct) — `v1:packages/opencode/src/session/tools.ts:429` | media — `v2:packages/core/src/mcp/client.ts:325` |
| resource_link | dropped (direct) / `name: uri` (code mode) — `v1:packages/opencode/src/tool/code-mode.ts:104` | `uri` text — `v2:packages/core/src/mcp/client.ts:327` |
| embedded resource | text / allow-listed blob — `v1:packages/opencode/src/session/tools.ts:436` | text / media — `v2:packages/core/src/mcp/client.ts:328` |
| structuredContent | used only if content empty — `v1:packages/opencode/src/mcp/catalog.ts:75` | becomes `output` — `v2:packages/core/src/tool/mcp.ts:97` |
| isError | thrown with text — `v1:packages/opencode/src/mcp/catalog.ts:68` | `ToolFailure` with text — `v2:packages/core/src/tool/mcp.ts:78` |
| OAuth | PKCE+DCR, port 19876, `mcp-auth.json` — `v1:packages/opencode/src/mcp/oauth-provider.ts:11` | + CIMD, RFC 8707, ephemeral port — `v2:packages/core/src/mcp/oauth.ts:25` |
| HTTP session expiry | re-initialize (patch) — `v1:patches/@modelcontextprotocol%2Fsdk@1.29.0.patch:210` | reconnect + retry — `v2:packages/core/src/mcp/index.ts:336` |
| per-call permission | `ctx.ask(permission=key)` — `v1:packages/opencode/src/session/tools.ts:408` | `permission.assert(action=name)` — `v2:packages/core/src/tool/mcp.ts:51` |

## 3. Code mode

| item | v1 | v2 |
| --- | --- | --- |
| enabled | `OPENCODE_EXPERIMENTAL_CODE_MODE`, else `OPENCODE_EXPERIMENTAL` — `v1:packages/opencode/src/effect/runtime-flags.ts:48` (on for this user: `nix:flake.nix:297`) | default; per-server `codemode` — `v2:packages/schema/src/mcp.ts:34` |
| package | `@opencode-ai/codemode` 1.18.31 — `v1:packages/codemode/package.json:3` | `@opencode/codemode` 2.0.3 — `v2:packages/codemode/package.json:3` |
| engine | TS transpile → acorn → interpreter — `v1:packages/codemode/src/interpreter/runtime.ts:116` | same — `v2:packages/codemode/src/interpreter/transpile.node.ts:10` |
| TypeScript | stripped — `v1:packages/codemode/src/interpreter/runtime.ts:3` | stripped — `v2:packages/codemode/src/interpreter/transpile.node.ts:1` |
| what is wrapped | MCP tools only — `v1:packages/opencode/src/tool/code-mode.ts:210` | every tool with `codemode !== false` — `v2:packages/core/src/tool.ts:229` |
| tool paths | `tools.<server>.<tool>` — `v1:packages/opencode/src/tool/code-mode.ts:47` | `tools.<namespace>.<tool>` — `v2:packages/core/src/codemode/tool.ts:282` |
| catalog | in `execute` description — `v1:packages/opencode/src/tool/registry.ts:288` | 2 000-char inline budget + diffs — `v2:packages/core/src/codemode/catalog.ts:50` |
| `search()` | yes — `v1:packages/codemode/src/codemode.ts:142` | yes — `v2:packages/codemode/README.md:170` |
| limits set by host | none — `v1:packages/opencode/src/tool/code-mode.ts:239` | none — `v2:packages/core/src/codemode/tool.ts:222` |
| fetch / timers / imports | no | no — `v2:packages/core/src/codemode/tool.ts:63` |
| concurrency | unrestricted | unrestricted; model told to `Promise.all` — `v2:packages/core/src/codemode/tool.ts:67` |
| child results | structuredContent, else text — `v1:packages/opencode/src/tool/code-mode.ts:109` | `output` (structured or parsed text) — `v2:packages/core/src/codemode/tool.ts:102` |
| session `_meta` on child MCP calls | none | `ai.opencode/sessionID` — `v2:packages/core/src/tool/mcp.ts:69` |
| plugin hooks per child | yes, callID `<parent>/<n>` — `v1:packages/opencode/src/tool/code-mode.ts:227` | yes — `v2:packages/core/src/tool.ts:235` |
| reachable outside a model turn | no | no — `v2:packages/protocol/src/groups/generate.ts:8` (only text generation) |
| opt-out | env flag | deny `execute`, or `codemode:false` — `v2:packages/core/src/tool.ts:232` |

## 4. Process model

| item | v1 | v2 |
| --- | --- | --- |
| processes | 1: TUI main thread + server Worker — `v1:packages/opencode/src/cli/cmd/tui.ts:210` | TUI + shared background server — `v2:packages/cli/src/commands/handlers/default.ts:31` |
| default network | none (in-process fetch) — `v1:packages/opencode/src/cli/cmd/tui.ts:246` | HTTP 127.0.0.1:0xc0de (service) — `v2:packages/cli/src/services/service-config.ts:35` |
| opt-out of sharing | n/a | `--standalone` — `v2:packages/cli/src/services/server-connection.ts:41` |
| MCP client scope | per directory — `v1:packages/opencode/src/effect/instance-state.ts:49` | per Location — `v2:packages/core/src/mcp/index.ts:736` |
| connections per server | 1 per directory per opencode process | 1 per Location per daemon — `v2:packages/core/src/mcp/index.ts:152` |
| start | all at first MCP use, awaited — `v1:packages/opencode/src/mcp/index.ts:528` | forked async per server — `v2:packages/core/src/mcp/index.ts:507` |
| lifetime | until instance dispose / exit — `v1:packages/opencode/src/mcp/index.ts:531` | forever (no idle TTL) — `v2:packages/core/src/location-services.ts:52` |
| on crash | `failed`, no restart — `v1:packages/opencode/src/mcp/index.ts:448` | `failed`, no restart — `v2:packages/core/src/mcp/index.ts:358` |
| stdio spawn | host — `v1:packages/opencode/src/mcp/index.ts:347` | Location `Environment` (may be remote workspace) — `v2:packages/core/src/mcp/client.ts:198` |
| teardown | SIGTERM descendants — `v1:packages/opencode/src/mcp/index.ts:546` | group SIGTERM→SIGKILL 2 s — `v2:packages/core/src/mcp/stdio.ts:57` |
| sessions sharing a stateful server | share; nothing sent to tell them apart | share (also across windows); `_meta["ai.opencode/sessionID"]` sent — `v2:packages/core/src/mcp/client.ts:278` |
| plugin realm | server Worker, unsandboxed — `v1:packages/opencode/src/plugin/index.ts:118` | daemon process, unsandboxed — `v2:packages/core/src/plugin/module.ts:102` |
| architecture | `packages/opencode` app over `packages/core` pieces — `v1:packages/opencode/package.json:3` | `cli` → `server` (`/api/*`, `protocol`) → `core` Effect services — `v2:packages/cli/package.json:7` |
| DB filename | channel-dependent — `v1:packages/core/src/database/database.ts:49` | channel-dependent (different list) — `v2:packages/cli/src/database-path.ts:7` |
