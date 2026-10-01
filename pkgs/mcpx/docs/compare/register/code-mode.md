# Code mode and script execution

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  Every difference in how lootbox, mcpx, opencode code mode (v1, v2) and Cloudflare code mode run model-written code against MCP tools.
```

Code mode means the model writes a program that calls MCP tools as typed functions, and only the program's output
goes back into context. The products agree on that idea and differ on engine, sandbox, limits, how results and
failures come back, how tools are discovered, and whether anything outside a model turn can run it. For mcpx, two
things matter most: it has **no sandbox, on purpose**, which the MCP client guide treats as a precondition (CODE-10);
and where others already do something mcpx needs, the gaps are small and concrete: an output cap, typed failures,
`outputSchema` return types, reserved-name checks. Differences whose area another register owns (result unwrapping,
the opencode plugin's `mcpx_exec`, pools, credentials) are referred to by that register's ID; in the Where column
`v1`/`v2` are opencode 1.18.31/2.0.3, `CF` is Cloudflare, and lootbox is the deployed build unless a row says "fork"
(CODE-01).

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| CODE-01 | Deployed lootbox is upstream `587a5a1` plus four patches, not the fork | `lootbox has` | deployed: upstream + 4 patches · `.lootbox/`: fork `887db38` | n/a — decides which lootbox the other rows describe | + low | S | low |
| CODE-02 | Real runtime chosen per run: deno, then bun, then node | `mcpx has, others don't` | mcpx 3 runtimes · lootbox deno only · v1/v2 interpreter · CF isolate | ✓ — `script.runtime` auto, deno, bun or node | + med | S | low |
| CODE-03 | opencode runs a confined interpreter with no ambient APIs | `opencode v1 has` `opencode v2 has` `does not match mcpx's goal` | v1 ✓ · v2 ✓ · lootbox, mcpx, CF run real engines | n/a — real runtime with full authority, by choice | − authority | XL | low |
| CODE-04 | Cloudflare runs each execution pass in a fresh V8 isolate | `does not match mcpx's goal` | CF ✓ · others ✗ | n/a — one OS process group per run | − workerd | XL | low |
| CODE-05 | opencode supports only a subset of JavaScript | `opencode v1 has` `opencode v2 has` | v1 no classes or generators · v2 no classes · others full | ✓ — full TypeScript on a real runtime | + low | S | low |
| CODE-06 | Script type-checked against generated types before it runs | `lootbox has` | lootbox on by default · mcpx opt-in · v1, v2, CF never | partial — `script.typecheck` exists, defaults to `off` | + med | S | med |
| CODE-07 | TypeScript stripped without checking; v2 workerd build skips transpile | `opencode v1 has` `opencode v2 has` | v1 ✓ · v2 ✓ (workerd: JS only) · CF JS only | n/a — the real runtime parses TypeScript | + low | S | low |
| CODE-08 | Default script permissions: `--allow-net` versus `--allow-all` | `lootbox has` | lootbox net only · mcpx all · v1/v2, CF no ambient access | ✓ — `--allow-all` by design; `net`, `strict` profiles exist | − policy | S | med |
| CODE-09 | Cloudflare blocks or proxies all outbound network | `does not match mcpx's goal` | CF ✓ · v1/v2 no network API · lootbox, mcpx open | n/a — no egress control beyond Deno `strict` | − authority | L | low |
| CODE-10 | MCP client guide: programmatic tool calling requires a sandbox | `does not match mcpx's goal` | guide (every docs version) · lootbox, v1, v2, CF sandbox · mcpx none | n/a — no sandbox, deliberately | − goal | L | med |
| CODE-11 | Default whole-script wall-clock timeout | `lootbox has` `opencode v1 has` `opencode v2 has` | lootbox 10 s · mcpx 120 s · v1/v2 none · CF 60 s or 30 s | ✓ — `exec.timeout` 120 s, then process-group kill | + low | S | low |
| CODE-12 | Per-tool-call timeout inside a script | `lootbox has` `opencode v1 has` `opencode v2 has` | lootbox 30 s · mcpx 120 s · v1 SDK 60 s · v2 12 h | ✓ — `pool.callTimeout` 120 s | + low | S | med |
| CODE-13 | Progress token requested so long calls outlive the timeout | `opencode v1 has` `opencode v2 has` `mcpx missing` | v1 token and reset · v2 token, no reset · lootbox, mcpx none | partial — sends a token when the host did and relays progress (#212); the daemon's own timeout does not reset on it | + med | S | med |
| CODE-14 | CPU-time and subrequest caps per invocation | `does not match mcpx's goal` | CF `cpuMs`, `subRequests` · others wall clock only | n/a — wall clock only | − marginal | M | low |
| CODE-15 | Tool-call budget per execution | `opencode v1 has` `opencode v2 has` | v1/v2 knob, unset by host · lootbox, mcpx, CF none | ✗ — only the timeout bounds a looping script | + low | S | low |
| CODE-16 | Cap on the size of returned output | `opencode v1 has` `opencode v2 has` `mcpx missing` | v1/v2 knob · CF 6,000 tokens · lootbox, mcpx whole stdout | ✗ — stdout returned whole; only stderr is bounded (#216) | + high | S | med |
| CODE-17 | Concurrent tool calls within one program | `opencode v1 has` | v1 8 · v2 unrestricted · lootbox, mcpx, CF no cap | n/a — per-server pool max 4, no per-run cap | + low | S | low |
| CODE-18 | Upstream call cancelled when the script times out or dies | `has better replacement` | mcpx ✓ · v1/v2 ✓ · lootbox ✗ · CF unverified | ✓ — context cancel sends `notifications/cancelled` | + low | S | low |
| CODE-19 | Final top-level expression becomes the result | `opencode v2 has` `mcpx missing` | v2 ✓ · CF ✓ · v1 `null` · lootbox none · mcpx stdout | ✗ — result is stdout, `emit()` or a default export (#216) | + med | M | low |
| CODE-20 | What happens to un-awaited calls when the program ends | `opencode v1 has` `opencode v2 has` | v1 awaited · v2 interrupted · lootbox, mcpx run on | n/a — the runtime's event loop finishes them | + low | S | low |
| CODE-21 | Working directory the script runs in | `lootbox has` `has better replacement` | lootbox daemon's cwd · mcpx caller's cwd · v1, v2, CF none | ✓ — caller's cwd; `/v1/exec` takes `cwd` | + low | S | low |
| CODE-22 | Caller chooses local or remote execution | `mcpx has, others don't` | mcpx ✓ · lootbox always daemon · v1/v2 in-process · CF platform | ✓ — `exec.where` auto, local or remote | + med | S | low |
| CODE-23 | How arguments reach a script | `lootbox has` `has better replacement` | lootbox stdin only · mcpx argv, exports, `@param` · CF one input | ✓ — argv, default export, `--export`, placeholders | + low | S | low |
| CODE-24 | Where named scripts are looked up | `lootbox has` `has better replacement` | lootbox one dir · mcpx upward search · CF snippets · v1/v2 none | ✓ — `.mcpx/scripts` upward, then user dir | + low | S | low |
| CODE-25 | Named injection points in the script launcher | `mcpx has, others don't` | mcpx ✓ · others wrap code, no hooks | ✓ — before, prefix, onSuccess, onError, suffix | + low | S | low |
| CODE-26 | `emit()` streams values out while the script runs | `mcpx has, others don't` | mcpx ✓ · others return at the end | ✓ — SSE or NDJSON frames from `/v1/exec` | + med | S | low |
| CODE-27 | Structured log records on a side channel | `mcpx has, others don't` | mcpx records · v1/v2, CF `logs: string[]` · lootbox raw stderr | ✓ — `log.*` behind a sentinel, parsed into `logs[]` | + low | S | low |
| CODE-28 | lootbox drops stderr on success and stdout on failure | `has better replacement` | lootbox ✗ · mcpx, v1/v2, CF keep both | ✓ — keeps both; stderr tail-bounded | + low | S | low |
| CODE-29 | One JSON document per run | `has better replacement` | mcpx ✓ · v1/v2 internal · CF three variants · lootbox text | ✓ — `--json run`, `/v1/exec`, `mcpx_exec` | + low | S | low |
| CODE-30 | Failures reported as typed kinds | `opencode v1 has` `opencode v2 has` `mcpx missing` | v1/v2 `kind` · CF `status: "error"` · mcpx, lootbox strings | ✗ — `error` string, `exitCode`, `timedOut` only (#216) | + med | M | low |
| CODE-31 | Budgeted catalogue: every namespace, signatures to about 2,000 tokens | `opencode v1 has` `opencode v2 has` | mcpx ✓ · v1 ✓ · v2 ✓ · lootbox counts only · CF all or none | ✓ — `mcpx catalog`, `mcpx_catalog`, 2,000-token default | + low | S | low |
| CODE-32 | Catalogue changes sent as a delta when shorter | `opencode v2 has` | v2 ✓ · mcpx machinery ✓ · v1 full re-render | ✓ — `Fingerprint`, `Diff`, `ShorterOf` exist | + low | S | low |
| CODE-33 | Pinned tools always shown in the catalogue | `opencode v2 has` | v2 ✓ · mcpx `--bias` only · others ✗ | ✗ — bias scoring favours terms; nothing is pinned | + low | S | low |
| CODE-34 | Tool search callable from inside the program | `opencode v1 has` `opencode v2 has` | mcpx sync · v1 async · v2 sync · CF async · lootbox ✗ | ✓ — synchronous `search()` and `describe()` | + low | S | low |
| CODE-35 | In-program search pages its results | `opencode v1 has` `opencode v2 has` `mcpx missing` | v1/v2 `offset`, `next` · CF `truncated` · mcpx `limit` only | ✗ — `search(query, limit = 20)`, no offset (#216) | + low | S | low |
| CODE-36 | In-program search also finds saved snippets | `mcpx missing` | CF ✓ · mcpx recipes outside scripts · others ✗ | ✗ — in-script `search()` covers tools only (#216) | + low | M | low |
| CODE-37 | Approval pauses a run by aborting it and replaying it | `does not match mcpx's goal` | CF ✓ · v1/v2 ask per call · mcpx elicitation · lootbox ✗ | n/a — elicitation blocks in-process; no replay | − determinism | XL | low |
| CODE-38 | Rollback through a per-tool `revert` | `does not match mcpx's goal` | CF ✓ · others ✗ | n/a — MCP tools have no `revert` | − no-revert | XL | low |
| CODE-39 | Only the host may promote a run to a saved snippet | `does not match mcpx's goal` | CF host only · lootbox, mcpx agent writes files · v1/v2 none | n/a — agents write `.mcpx/scripts/` directly | − workflow | M | med |
| CODE-40 | Tool names that are JavaScript reserved words get a suffix | `mcpx missing` | CF ✓ · mcpx, lootbox ✗ · v1/v2 bracket notation | ✗ — `mcpx types` prints `function delete(…)` (#215) | + low | S | low |
| CODE-41 | Namespace names checked against reserved words and prelude names | `mcpx missing` | CF ✓ · lootbox `mcp_` prefix · v1/v2 brackets · mcpx ✗ | ✗ — a server named `log` may break every script (#215) | + med | S | high |
| CODE-42 | Two tool names that sanitise to the same identifier | `mcpx missing` | CF throws · mcpx last wins · lootbox first wins | ✗ — duplicate keys; wrong tool called silently (#215) | + med | S | med |
| CODE-43 | opencode keeps real tool names and uses bracket notation | `opencode v1 has` `opencode v2 has` | v1/v2 ✓ · mcpx, lootbox, CF rename | n/a — renames `fancy-name` to `fancy_name` | + low | M | low |
| CODE-44 | Return types generated from a tool's `outputSchema` | `opencode v1 has` `opencode v2 has` `2025-06-18 has` `mcpx missing` | v1/v2 ✓ · CF field · mcpx `any` · lootbox envelope | ✗ — parses `outputSchema`; every result typed `any` (#216) | + med | S | low |
| CODE-45 | lootbox's generated types carry no descriptions | `has better replacement` | lootbox ✗ · mcpx, v1/v2, CF ✓ | ✓ — JSDoc for tools and properties | + low | S | low |
| CODE-46 | lootbox generates one function per MCP resource | `lootbox has` | lootbox ✓ · mcpx generic reader · v1/v2 none · CF undocumented | n/a — one `readResource(uri)` | − bloat | S | low |
| CODE-47 | lootbox exposes local TypeScript files as namespaces | `lootbox has` `has better replacement` | lootbox ✓ · mcpx adapters and imports | n/a — a plain module import instead, by choice | − cost | M | low |
| CODE-48 | lootbox YAML prompt workflows | `lootbox has` `does not match mcpx's goal` | lootbox ✓ · others ✗ | n/a — left out on purpose | − scope | M | low |
| CODE-49 | lootbox's script-to-gateway protocol is its own WebSocket JSON | `lootbox has` `has better replacement` | lootbox ✓ · mcpx REST and MCP · v1/v2 in-process · CF RPC | ✓ — documented REST `/v1/call`, `/v1/exec` | + low | S | low |
| CODE-50 | Script client reaches the daemon over a unix socket | `mcpx has, others don't` | mcpx deno/bun socket, node TCP · others ✗ | ✓ — socket mode is the access control | + med | S | low |
| CODE-51 | Cloudflare connects script and host through Workers RPC | `does not match mcpx's goal` | CF ✓ · others ✗ | n/a — HTTP to the daemon | − Workers-only | XL | low |
| CODE-52 | Every run recorded with its script text and output | `lootbox has` | lootbox SQLite · mcpx log without source · CF last 50 · v1/v2 transcript | partial — run ids and log records; no script source | + low | M | med |
| CODE-53 | opencode wraps MCP servers in code mode by default | `opencode v1 has` `opencode v2 has` `mcpx missing` | v2 default, per server · v1 behind a flag this build sets · opt-out only for `executor` | ✗ — docs never tell users to opt out (#218) | + high | S | med |
| CODE-54 | Code execution reachable from a shell, HTTP or CI | `lootbox has` | mcpx ✓ · lootbox CLI and WS · CF Worker · v1/v2 model turn only | ✓ — `mcpx exec`, `/v1/exec`, `mcpx_exec` | + high | S | low |

## CODE-01 The deployed lootbox is upstream `587a5a1` plus four patches, not the fork

- **What.** The daemon answering on 127.0.0.1:9420 is `~/.local/bin/lootbox`, built by `lootbox-update` from upstream
  `jx-codes/lootbox@587a5a1` plus four patches. The `.lootbox/` checkout cited throughout this register is the
  `dezren39` fork (branch `configurable-timeout`, HEAD `887db38`), 51 commits ahead of that upstream.
- **Where.** Deployed: upstream plus `lootbox-deno-2.9`, `lootbox-global-config`, `lootbox-loopback` and
  `lootbox-ui-dir`. The fork is not deployed.
- **mcpx @ 05c78b2.** n/a. `ASSESSMENT.md` audited "`jx-codes/lootbox` (and the `dezren39` fork)" against the live
  installation (`ASSESSMENT.md:3-4`) and counts the fork 51 commits ahead (`ASSESSMENT.md:9-10`).
- **Value to mcpx.** + low: every lootbox cell in this register is only as accurate as the build it describes.
- **Effort.** S — a note; no code.
- **Risk.** Reading a fork-only feature (the health monitor, `per-session`) as behaviour of the running daemon.
- **Detail.** Fork-only: the health monitor, the multi-client strategies (`warn`/`fail`/`auto-port`/`per-session`),
  configurable timeout and permissions, the deep `/health`, and `lootbox health`. The live `/health` returned
  `{"status":"ok"}`, not the fork's subsystem report, which confirms the upstream build is what runs. The core code-mode
  path (`parse_mcp_schemas.ts`, `mcp_schema_fetcher.ts`) is the same in both; `git diff --stat 587a5a1 HEAD` does not
  list those files. Upstream hard-codes what the fork makes configurable: `git show
  587a5a1:src/lib/execute_llm_script.ts` has a 10-second script timeout (line 26) and `--allow-net`,
  `--allow-import=localhost:<port>`, `--reload=…/client.ts`, `--no-check=remote` (lines 31-34), and its
  `src/lib/rpc/execute_mcp.ts` hard-codes a 30-second per-call timeout (line 60), so CODE-06, CODE-08, CODE-11 and
  CODE-12 hold for both. Rows about fork-only process behaviour (startup pings, multi-instance strategies) are in the
  process-model register (PM-22, PM-24).
- **Sources.** `nix:flake.nix:386-398` (pins `rev = "587a5a1b…"` and the four `lootboxPatches`); `ASSESSMENT.md:9-10`
  "the fork is already 51 commits ahead"

## CODE-02 Real runtime chosen per run: deno, then bun, then node

- **What.** mcpx runs a script on a real JavaScript runtime: the one requested, otherwise the first of deno, bun or node
  on `PATH`. lootbox always uses Deno; opencode uses its own interpreter (CODE-03); Cloudflare uses a V8 isolate
  (CODE-04).
- **Where.** mcpx only.
- **mcpx @ 05c78b2.** `Detect` (`internal/runner/runner.go:72-106`); setting `script.runtime`, enum `auto|deno|bun|node`
  (`internal/settings/registry.go:176-178`).
- **Value to mcpx.** + med: scripts run wherever any one of three runtimes is installed. − the three behave differently:
  only Deno has permission flags (`internal/runner/runner.go:43-45`), and node reaches the daemon over TCP only
  (`internal/codegen/emit.go:289-291`, CODE-50).
- **Effort.** S — built.
- **Risk.** A script that works under Deno can fail under node's strip-only TypeScript, which rejects enums, namespaces
  and parameter properties. A cross-runtime test already caught the generated client failing on node for exactly that
  reason (`ASSESSMENT.md:260-261`).
- **Detail.** Deno runs `run --quiet --no-check <perms> <file>`; bun runs `bun run <file>`; node runs `--no-warnings
  --experimental-strip-types <file>` (`internal/runner/runner.go:84-101`). On bun and node a shim fills in `Deno.args`,
  `Deno.env` and `Deno.readTextFile`, and `readTextFile` refuses unless `MCPX_ALLOW_READ=1`
  (`internal/codegen/emit.go:348-387`, guard at `:373`). ASSESSMENT measured a trivial script against a local client
  file at 30–60 ms on any of the three (`ASSESSMENT.md:79-80`).
- **Sources.** `internal/runner/runner.go:73` `candidates := []string{"deno", "bun", "node"}`;
  `.lootbox/src/lib/execute_llm_script.ts:38` `await new Deno.Command("deno", {`

## CODE-03 opencode runs a confined interpreter with no ambient APIs

- **What.** opencode transpiles the program, parses it with acorn and runs it in its own tree-walking evaluator. The
  program can reach only the host's `tools` tree: no filesystem, network, process, module, import or timer access.
- **Where.** opencode v1 and v2 (`packages/codemode`). lootbox and mcpx run a real runtime; Cloudflare runs a real V8
  isolate.
- **mcpx @ 05c78b2.** Runs deno/bun/node with `--allow-all` by default (`internal/runner/runner.go:47-54`), and
  `AGENTS.md:28-29` promises scripts "the full runtime: filesystem, network, subprocesses".
- **Value to mcpx.** − adopting it contradicts mcpx's stated choice of full authority (`internal/runner/runner.go:47`)
  and breaks every script that touches the filesystem or a subprocess.
- **Effort.** XL — `packages/codemode/src` is 6,878 lines of TypeScript in v1 and 9,456 in v2 (`wc -l`).
- **Risk.** None if not adopted.
- **Detail.** v2's `execute` description tells the model "Do not use `fetch`; all external access goes through `tools`"
  (`v2:packages/core/src/codemode/tool.ts:63`). v2 has an opt-in host extension mechanism for classes and functions
  (`v2:packages/codemode/interpreter-support.md:455-456`), but opencode core registers none: `Extension.make` appears
  only in the package's own tests and a doc comment (`v2:packages/codemode/src/codemode.ts:40`). The interpreter yields
  between steps, so a timeout also stops `while (true) {}` (`v1:packages/codemode/README.md:286`), and data crossing a
  boundary nests at most 32 levels (`v1:packages/codemode/README.md:288`, `v2:packages/codemode/README.md:187`). mcpx's
  own comparison calls this "a genuinely stronger posture than permissions" (`OPENCODE-V2.md:190-193`). `OPENCODE-V2.md`
  reviewed branch `v2@37049a5`, not the pinned 2.0.3 (`OPENCODE-V2.md:12`), and is wrong at the pin in three places
  here. The doc says `packages/core/src/codemode/` contains "fetch" (`OPENCODE-V2.md:29`) and that fetch "is provided as
  an extension" (`OPENCODE-V2.md:51`); the source has only `catalog.ts`, `instructions.ts` and `tool.ts` there, and the
  tool says not to use `fetch`. The doc says "10,764 lines of implementation, 20,075 lines of tests across 34 files"
  (`OPENCODE-V2.md:19`); the source has 9,456 lines under `src/`, and `test/` holds 43 files, 35 of them `.ts` totalling
  18,056 lines.
- **Sources.** `v2:packages/codemode/README.md:7` "Rather than trying to sandbox arbitrary JavaScript, CodeMode only
  runs the language features we implement."; `v1:packages/codemode/src/interpreter/runtime.ts:1` `import { parse } from
  "acorn"`; `v1:packages/codemode/README.md:5`; `v2:packages/core/src/codemode/tool.ts:63`; `OPENCODE-V2.md:51` "is
  provided as an extension. Timers, imports, filesystem and process"

## CODE-04 Cloudflare runs each execution pass in a fresh V8 isolate

- **What.** Cloudflare's `DynamicWorkerExecutor` loads the generated code into a new Dynamic Worker for every pass,
  through the Worker Loader binding, and discards it afterwards.
- **Where.** Cloudflare only.
- **mcpx @ 05c78b2.** One OS process per run, in its own process group (`internal/runner/runner.go:413`).
- **Value to mcpx.** − needs workerd or the Cloudflare platform; mcpx scripts need the local filesystem and processes.
- **Effort.** XL — a workerd dependency.
- **Risk.** None; not a candidate.
- **Detail.** After an approval, a paused execution re-runs the whole program in a new pass, so "Durable state therefore
  cannot live inside the sandbox" (how-it-works, "Executor"). Dynamic Workers have no build step, so TypeScript must be
  compiled to JavaScript first (Dynamic Workers "Getting started"). Blog-only claims not confirmed in the docs: an
  isolate starts "in a handful of milliseconds using only a few megabytes of memory" (2026 blog); the Worker Loader API
  was in "closed beta" (2025 blog). Current availability and pricing were not checked. The process-model register has
  the same fact from the process angle (CODE-04).
- **Also recorded from the process model register.** Dynamic Workers have no build step, so TypeScript must be
  compiled first.
- **Sources.** `internal/runner/runner.go:413`;
  https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#executor ("Executor": "uses a Dynamic Worker
  Loader to create an isolated Worker for each execution pass"); https://blog.cloudflare.com/code-mode/ ("Dynamic Worker
  loading: no containers here"); <https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#executor> "uses a Dynamic Worker Loader to create an isolated Worker for each execution pass"; <https://blog.cloudflare.com/code-mode/> "Isolates are so fast that we can just create a new one for every piece of code the agent runs."

## CODE-05 opencode supports only a subset of JavaScript

- **What.** opencode's interpreter implements a chosen subset. v1 has no classes, no generators, no `new Promise` and no
  promise chaining; v2 adds generators and `new Promise` but still has no classes.
- **Where.** opencode v1 and v2. lootbox, mcpx and Cloudflare run the full language.
- **mcpx @ 05c78b2.** Full TypeScript on a real runtime (CODE-02).
- **Value to mcpx.** + low: confirms mcpx scripts can paste in an existing module; nothing to build.
- **Effort.** S — no work.
- **Risk.** A model used to one harness writes classes and fails under opencode with `UnsupportedSyntax`.
- **Detail.** v1 exposes no `eval`, dynamic imports, modules, classes, generators, timers, host globals, prototype
  mutation, `new Promise`, or `.then`/`.catch`/`.finally` (`v1:packages/codemode/README.md:254`). v2.0.3's checklist has
  211 `[x]` and 30 `[ ]` lines (`rg -c`); gaps include tagged-template calls
  (`v2:packages/codemode/interpreter-support.md:60`), user-defined constructor calls (`:139`), and classes and private
  fields (`:141`), while generators (`:152`) and `new Promise` (`:232`) are present. The doc says "251 checked items, 17
  gaps" and lists tagged templates as present (`OPENCODE-V2.md:104-105`); the source at the pin says 211/30 and tagged
  templates are a gap.
- **Sources.** `v1:packages/codemode/README.md:254`; `v2:packages/codemode/interpreter-support.md:60` "- [ ]
  Tagged-template calls."; `v2:packages/codemode/interpreter-support.md:141`; `OPENCODE-V2.md:104` "251 checked items,
  17 gaps"

## CODE-06 Script type-checked against generated types before it runs

- **What.** lootbox runs scripts with `deno run --no-check=remote`. That flag skips remote modules but type-checks local
  ones, so the script is checked against the generated client's types and a type error stops the run.
- **Where.** lootbox, on by default (both builds, CODE-01). mcpx: opt-in, off by default. opencode strips types without
  checking (CODE-07). Cloudflare: the model writes JavaScript and nothing checks it.
- **mcpx @ 05c78b2.** `script.typecheck` defaults to `off`, with `on`/`strict` available
  (`internal/settings/registry.go:244-246`); Deno runs with `--no-check` (`internal/runner/runner.go:87-90`); the check
  runs through `preflight.TypeCheck` when enabled (`internal/runner/runner.go:328-340`).
- **Value to mcpx.** + med: a wrong argument name or type is caught before any tool call has a side effect. − adds
  latency to every run; the default-off choice is written down (`internal/runner/runner.go:87-89`).
- **Effort.** S — flip the default, or say in the agent instructions that nothing checks.
- **Risk.** lootbox's instructions tell agents "the sandbox will reject any scripts that don't pass". An agent moving
  from lootbox to mcpx loses that check without being told.
- **Detail.** Verified with deno 2.9.6: `--no-check=remote` on a file with a type error exits 1 with "Type checking
  failed"; plain `deno run` runs it. mcpx's runner comment says lootbox's `--reload` forced "a full type check of the
  module graph on every single execution" (`internal/runner/runner.go:6-7`); what is checked is the local script, not
  the remote client. The check is shallow: lootbox maps nested objects to `Record<string, unknown>`
  (`.lootbox/src/lib/external-mcps/parse_mcp_schemas.ts:218-220`), whereas mcpx renders `$ref`/`allOf`/`enum`/`const`/
  `prefixItems` (`internal/codegen/ts.go:41-61`), so mcpx's check would catch more once turned on. How each loads the
  generated client per run (lootbox's HTTP import with `--reload`) is the process-model register's PM-25.
- **Sources.** `.lootbox/src/lib/execute_llm_script.ts:34` `"--no-check=remote",`; `.lootbox/LLM_QUICK_START.md:113`
  "Type safety is paramount (the sandbox will reject any scripts that don't pass)"

## CODE-07 TypeScript stripped without checking; v2 workerd build skips transpile

- **What.** opencode wraps the program in an async function and runs TypeScript's `transpileModule`, which reports
  syntax diagnostics only, before acorn parses the output. v2 chooses the transpiler through a conditional import; its
  workerd variant is the identity function, so TypeScript-only syntax becomes a parse error there.
- **Where.** opencode v1 and v2. lootbox and mcpx use the runtime's TypeScript support; Cloudflare needs JavaScript.
- **mcpx @ 05c78b2.** Full TypeScript through the runtime (CODE-02); no check by default (CODE-06).
- **Value to mcpx.** + low: corrects mcpx's own comparison. Nothing for mcpx to build.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The doc says "TypeScript is rejected rather than stripped" (`OPENCODE-V2.md:121`); the source transpiles
  in both versions. The doc's correction note already says so (`OPENCODE-V2.md:3-6`). v2's `package.json` maps
  `#transpile` to `workerd` and `default` conditions (`v2:packages/codemode/package.json:23-25`). A workerd profile
  suggests opencode runs code mode inside Cloudflare Workers somewhere, probably the web or console app; not traced.
- **Sources.** `v1:packages/codemode/src/interpreter/runtime.ts:116`;
  `v2:packages/codemode/src/interpreter/transpile.node.ts:10`;
  `v2:packages/codemode/src/interpreter/transpile.workerd.ts:7` "so codemode programs are passed through";
  `OPENCODE-V2.md:121` "TypeScript is rejected rather than stripped"

## CODE-08 Default script permissions: `--allow-net` versus `--allow-all`

- **What.** A lootbox script gets network access and nothing else by default: no filesystem, no environment, no
  subprocess. The network is also its only path to the daemon (`ws://localhost:<port>/ws`), so it cannot be removed. An
  mcpx script gets `--allow-all`.
- **Where.** lootbox: fork default `["--allow-net"]` plus `--allow-import=localhost:<port>`; upstream hard-codes the
  same (CODE-01). mcpx: `all`. opencode and Cloudflare have no ambient access at all (CODE-03, CODE-09).
- **mcpx @ 05c78b2.** `Permissions("")` gives `--allow-all`; `net` gives `--allow-net --allow-env` (lootbox's default
  plus env); `strict` gives `--allow-net=127.0.0.1 --allow-env` (`internal/runner/runner.go:51-64`); `none`/`off` mean
  "no sandbox", i.e. `--allow-all`; the default `script.permissions` is `"all"` (`internal/defaults/defaults.json:34`).
  Only Deno honours any of it (`internal/runner/runner.go:43-45`).
- **Value to mcpx.** − the difference is policy: the user asked for no sandbox (`ASSESSMENT.md:168-169`,
  `ASSESSMENT.md:290-292`). mcpx's `strict` is tighter than lootbox's default (loopback only).
- **Effort.** S — nothing to change; state it where agents read.
- **Risk.** An agent following lootbox's instructions assumes a sandbox exists; under mcpx it does not.
- **Detail.** lootbox's `--allow-net` is unrestricted, so a "sandboxed" lootbox script can send anything a tool returns
  anywhere. lootbox's own tool workers (`.lootbox/tools/*.ts`) always run `--allow-all`
  (`.lootbox/src/lib/rpc/worker_manager.ts:298-301`), and `--no-sandbox` switches scripts to `--allow-all`
  (`.lootbox/src/lib/get_config.ts:250-251`); the launchd arguments pass no `--no-sandbox`
  (`nix:configuration.nix:743-749`). `ASSESSMENT.md:73` quotes lootbox's command as `deno run --allow-all --reload=…
  --no-check=remote`; the source says `--allow-net` in both the fork and upstream, so that line is wrong. What a script
  can read of the daemon's environment, and the missing `Origin` check on the endpoints that run scripts, are the auth
  register's AUTH-25 and AUTH-24.
- **Sources.** `.lootbox/src/lib/constants.ts:78` `export const DEFAULT_PERMISSION_FLAGS: readonly string[] =
  ["--allow-net"];`; `.lootbox/src/lib/get_config.ts:485`; `internal/runner/runner.go:47` "The default is wide open.";
  `ASSESSMENT.md:73` "deno run --allow-all --reload=http://localhost:PORT/client.ts --no-check=remote <tmpfile>"

## CODE-09 Cloudflare blocks or proxies all outbound network

- **What.** `DynamicWorkerExecutor` sets `globalOutbound: null` by default, so `fetch()` and `connect()` throw. A host
  may pass a `Fetcher` instead, which receives every outbound request.
- **Where.** Cloudflare. opencode has no network API (CODE-03). lootbox allows any network. mcpx allows everything;
  `strict` narrows to 127.0.0.1 under Deno only.
- **mcpx @ 05c78b2.** No egress proxy. `strict` relies on Deno's `--allow-net=127.0.0.1`
  (`internal/runner/runner.go:61-63`), which bun and node ignore (`internal/runner/runner.go:43-45`).
- **Value to mcpx.** − mcpx scripts are meant to have the user's full authority.
- **Effort.** L — an HTTP proxy plus per-runtime wiring.
- **Risk.** None; not a candidate.
- **Detail.** Cloudflare's argument: "Limiting access via bindings is much cleaner than doing it via, say, network-level
  filtering or HTTP proxies" (2025 blog, "Isolated by default, but connected with bindings"). The MCP client guide asks
  for the same isolation (CODE-10).
- **Sources.** `internal/runner/runner.go:61-63`;
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#dynamicworkerexecutor ("`globalOutbound` …
  `null` blocks access. A `Fetcher` receives all outbound requests.");
  https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#executor ("External `fetch()` and `connect()`
  calls are blocked by default.")

## CODE-10 MCP client guide: programmatic tool calling requires a sandbox

- **What.** The MCP documentation's client best-practices guide describes code mode as "programmatic tool calling" and
  says it "requires clients to implement a sandbox environment": no direct network access, credentials held by the host,
  timeouts and memory limits, truncated output, and per-call authorization. mcpx has no sandbox by design.
- **Where.** The guide is documentation, not specification. The same section is in the docs folder of every revision
  (2024-11-05 through 2026-07-28, and draft); the 2026-07-28 copy is cited. lootbox (Deno, network only), opencode
  (interpreter) and Cloudflare (isolate) each run a sandbox of some kind. mcpx does not.
- **mcpx @ 05c78b2.** No sandbox, chosen by the user: "I dropped the sandbox, as you asked" (`ASSESSMENT.md:168`) and
  "No sandbox. You said you did not want one" (`ASSESSMENT.md:290`); the runner's default is `--allow-all`
  (`internal/runner/runner.go:47-54`).
- **Value to mcpx.** − adopting the guide's sandbox contradicts the goal. + low: mcpx's docs should say plainly that it
  departs from the guide, and why (local-first, the user's own authority, stateful servers).
- **Effort.** L — a real sandbox means a runtime choice (CODE-03, CODE-04) or an egress proxy (CODE-09).
- **Risk.** Readers who take "the MCP spec now blesses the pattern" at face value (`ASSESSMENT.md:132-134`,
  `ASSESSMENT.md:303-304`) miss that the guide's blessing comes with a sandbox mcpx does not have.
- **Detail.** The guide's integration pattern intercepts calls "over an in-process or stdio channel (so network
  permissions can stay fully denied)" (`:260`). lootbox and mcpx both reach their broker over the network or a socket
  (CODE-49, CODE-50), which is why lootbox cannot drop `--allow-net` (CODE-08). The guide's other points map to rows
  here: typed functions from `outputSchema` (`:181`, CODE-44), `isError` as a thrown exception (`:301`; content-types
  register, CT-19), resource limits (`:294`, CODE-11, CODE-14), output truncation (`:295`, CODE-16), per-call
  authorization (`:290`; plugin-API register, PLG-36: opencode checks every child call, mcpx sees one opaque call), and
  credentials held by the host (`:293`; auth register, AUTH-25). Cloudflare argues the same point against shell-based
  tools by name: the agent "needs a shell … a much broader attack surface than a sandboxed isolate" (2026 blog,
  "Comparing approaches to context reduction").
- **Sources.** `docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:169-170` "requires clients to implement a
  sandbox environment."; `docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:292` "The sandbox should have
  no direct network access."; https://modelcontextprotocol.io/docs/2026-07-28/develop/clients/client-best-practices;
  `ASSESSMENT.md:168` "I dropped the sandbox, as you asked."

## CODE-11 Default whole-script wall-clock timeout

- **What.** Defaults: lootbox 10 s, mcpx 120 s, opencode none, Cloudflare 60 s by its API reference and 30 s by its
  changelog and examples.
- **Where.** All five differ. opencode's `timeoutMs` has no default and neither host sets one.
- **mcpx @ 05c78b2.** `exec.timeout` is `120s` (`internal/settings/exec.go:16`). On timeout the process group gets
  SIGTERM, then SIGKILL after a grace period, and the exit code is 124 (`internal/runner/runner.go:424-438`).
- **Value to mcpx.** + low: 120 s suits a browser-driving script; lootbox's 10 s killed them.
- **Effort.** S.
- **Risk.** An opencode `execute` can run until the user cancels; the plugin's `mcpx_exec` ignores that cancel
  (plugin-API register, PLG-38).
- **Detail.** lootbox's CLI client clears its own timeout on **any** message, including the server's `welcome` frame, so
  `client_timeout` never fires and a stuck daemon hangs `lootbox exec` indefinitely
  (`.lootbox/src/lib/lootbox-cli/exec.ts:43-44`; welcome sent at
  `.lootbox/src/lib/rpc/managers/connection_manager.ts:163`); upstream `587a5a1` has the same `clearTimeout` in
  `onmessage`. opencode v1's host passes no `limits` (`v1:packages/opencode/src/tool/code-mode.ts:239-260`) and v2's
  calls `CodeMode.make({ tools, ...hooks })` (`v2:packages/core/src/codemode/tool.ts:222`); v1's README calls that
  deliberate for a host "that can interrupt the execution fiber (as OpenCode does on user cancel)"
  (`v1:packages/codemode/README.md:268`). In v2 a result returned before cleanup times out stays successful with a
  `TimeoutExceeded` warning (`v2:packages/codemode/README.md:184-187`). Cloudflare's conflict is unresolved; the API
  reference is the newest page (2026-07-22). The guide asks for "timeouts and memory limits"
  (`docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:294`).
- **Sources.** `.lootbox/src/lib/constants.ts:17` `export const DEFAULT_TIMEOUT_MS = 10_000;`;
  `v2:packages/codemode/README.md:182` "Execution limits have no default values.";
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#dynamicworkerexecutor ("`timeout` | `number` |
  No | `60000`"); https://developers.cloudflare.com/changelog/post/2026-02-20-codemode-sdk-rewrite/ ("default 30s")

## CODE-12 Per-tool-call timeout inside a script

- **What.** Each tool call a script makes has its own deadline, separate from the script's: lootbox 30 s, mcpx 120 s,
  opencode v1 the SDK's 60 s (reset by progress), opencode v2 12 hours.
- **Where.** All four local products differ; Cloudflare documents only the executor timeout.
- **mcpx @ 05c78b2.** `pool.callTimeout` is `"120s"` (`internal/defaults/defaults.json:6`), the same length as
  `exec.timeout`, so one slow call can use the whole script budget.
- **Value to mcpx.** + low: informational; the split exists.
- **Effort.** S.
- **Risk.** A call that legitimately runs past 120 s dies (see CODE-13 for the fix other clients use).
- **Detail.** lootbox wraps `client.callTool(...)` in a `Promise.race` against `rpcTimeout` (default 30 s,
  `.lootbox/src/lib/constants.ts:20`), which abandons the promise without cancelling it (CODE-18). The generated
  client's per-call timeout is `config.timeout` at startup but `client_timeout` after a tool file changes
  (`.lootbox/src/lib/rpc/websocket_server.ts:91` versus `:151`). opencode v1 passes the per-server `timeout` or
  `experimental.mcp_timeout`, else nothing, leaving the SDK's 60,000 ms, with `resetTimeoutOnProgress: true` and no
  `maxTotalTimeout` (`v1:packages/opencode/src/mcp/index.ts:672`); code-mode child calls pass `timeout:
  input.entry.tool.timeout` (`v1:packages/opencode/src/tool/code-mode.ts:154-158`). v1's config text says "Defaults to
  5000" while the code uses 30,000 for connect and list (`v1:packages/core/src/v1/config/mcp.ts:21`,
  `v1:packages/opencode/src/mcp/index.ts:38`). opencode v2 splits `timeout.startup`/`catalog`/`execution`, defaulting to
  30 s, 30 s and 12 h (`v2:packages/core/src/mcp/client.ts:34`, `:241`).
- **Sources.** `internal/defaults/defaults.json:6`; `.lootbox/src/lib/rpc/execute_mcp.ts:60-64`;
  `v2:packages/core/src/mcp/client.ts:34` "const DEFAULT_EXECUTION_TIMEOUT = 12 * 60 * 60 * 1_000 // 12 hours"

## CODE-13 Progress token requested so long calls outlive the timeout

- **What.** opencode passes a no-op `onprogress` to every `callTool`, which makes the SDK send `_meta.progressToken`
  (equal to the JSON-RPC id). v1 also sets `resetTimeoutOnProgress`, so a server that keeps reporting progress keeps the
  call alive. mcpx never sends a progress token, so upstream servers cannot report progress to it and its 120 s call
  timeout cannot be extended.
- **Where.** opencode v1 (token and reset) and v2 (token, no reset). lootbox: no token, and its `Promise.race` is not
  reset. Cloudflare: not documented.
- **mcpx @ 05c78b2.** `progressToken` appears only in the `Progress` struct it parses
  (`internal/mcpclient/client.go:438`).
- **Value to mcpx.** + med: long browser or crawl tools that report progress would not die at 120 s, and progress could
  flow into `/v1/exec` stream frames (CODE-26) and plugin metadata (plugin-API register, PLG-37). Today the stream's
  progress plumbing can never carry upstream progress for a tool call.
- **Effort.** S — add `_meta.progressToken` to `tools/call`, reset the deadline on each progress notification, up to a
  hard maximum.
- **Risk.** A server that sends progress forever never times out, so a hard wall is needed; v1 sets no
  `maxTotalTimeout`.
- **Detail.** v2's comment says "Requesting progress keeps long calls alive under the SDK's timeout; execution is the
  hard wall", but v2 never sets `resetTimeoutOnProgress` (rg finds none in `v2:packages/core/src`), and the SDK resets
  only when it is set; the 12-hour `execution` value is the SDK timeout itself. The 1.x SDK reads
  `resetTimeoutOnProgress ?? false` (`@modelcontextprotocol/sdk@1.22.0/dist/esm/shared/protocol.js:306-308`). v1's
  comment: "The MCP SDK only sends a progress token when this hook is present, enabling timeout resets".
- **Sources.** `v1:packages/opencode/src/tool/code-mode.ts:154-158`; `v1:packages/opencode/src/mcp/catalog.ts:61`
  `resetTimeoutOnProgress: true,`; `v2:packages/core/src/mcp/client.ts:280-281`; `internal/mcpclient/client.go:438`;
  `internal/defaults/defaults.json:6`

## CODE-14 CPU-time and subrequest caps per invocation

- **What.** A Cloudflare Dynamic Worker accepts `limits: { cpuMs, subRequests }`, in its code or at `getEntrypoint()`,
  and the lower value wins. Hitting either throws at once.
- **Where.** Cloudflare. No local product caps CPU. opencode's interpreter yields between steps, so its timeout also
  stops busy loops (`v1:packages/codemode/README.md:286`).
- **mcpx @ 05c78b2.** Wall clock only (CODE-11).
- **Value to mcpx.** − scripts run on the user's own machine with full authority; a CPU cap adds little beyond the
  timeout.
- **Effort.** M — `setrlimit` per runtime.
- **Risk.** None; not a candidate.
- **Detail.** Without custom limits the Workers plan limits apply. The guide asks for "memory limits" as well as
  timeouts (`docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:294`); no local product sets one.
- **Sources.** `v1:packages/codemode/README.md:286`;
  https://developers.cloudflare.com/dynamic-workers/usage/limits/#set-custom-limits (`limits: { cpuMs: 10, subRequests:
  5 }`)

## CODE-15 Tool-call budget per execution

- **What.** opencode's library has a `maxToolCalls` knob; going over it gives a `ToolCallLimitExceeded` diagnostic. In
  v2, `search` calls count too. opencode's own host never sets it.
- **Where.** opencode v1 and v2 (library only). lootbox, mcpx and Cloudflare have no per-run budget.
- **mcpx @ 05c78b2.** None; a looping script is stopped by the 120 s timeout or by pool contention.
- **Value to mcpx.** + low: bounds fan-out cost from `mcpx_exec`; the timeout already bounds it roughly.
- **Effort.** S — count calls per `MCPX_RUN` in `/v1/call`.
- **Risk.** Low. From the other side: an opencode code-mode script calling mcpx over MCP is itself unbounded, so mcpx
  should expect long many-call bursts.
- **Detail.** v1 README: "none - unlimited" (`v1:packages/codemode/README.md:265`); v2 README: "Search counts toward
  `maxToolCalls`" (`v2:packages/codemode/README.md:171-172`).
- **Sources.** `v1:packages/codemode/README.md:265`; `v2:packages/codemode/README.md:179`;
  `v2:packages/core/src/codemode/tool.ts:222`

## CODE-16 Cap on the size of returned output

- **What.** opencode's `maxOutputBytes` truncates the value and logs with an in-band marker and sets `truncated: true`.
  Cloudflare ships `truncateResult`/`truncateResponse` with a 6,000-token default, and `openApiMcpServer` clips text at
  about 6,000 tokens. mcpx returns all of stdout.
- **Where.** opencode library (the host leaves it unset and relies on its generic tool-output truncation). Cloudflare
  utilities. lootbox and mcpx return stdout whole.
- **mcpx @ 05c78b2.** stdout accumulates in an unbounded `strings.Builder` and is returned whole
  (`internal/execsvc/execsvc.go:413-417`, `:510`). Only stderr is tail-bounded (`defaults.ExecStderrLimit`,
  `internal/execsvc/execsvc.go:436`), and only inline artifacts are capped (`artifacts.inlineMaxBytes`,
  `docs/exec.md:323`).
- **Value to mcpx.** + high: one `console.log(bigObject)` in `mcpx_exec`, over MCP or through the plugin (which prints
  the whole envelope; plugin-API register, PLG-40), floods the caller's context, which is exactly what code mode exists
  to prevent.
- **Effort.** S — an `exec.maxOutputBytes` setting that truncates with a marker and sets `truncated`.
- **Risk.** Without it, one chatty script can cost more tokens than the direct call it replaced.
- **Detail.** v1's README tells hosts without their own truncation to set it, "or oversized results silently flood model
  context" (`v1:packages/codemode/README.md:268`). v2 gives warnings a separate budget equal to `maxOutputBytes`
  (`v2:packages/codemode/README.md:184`). Cloudflare's durable runtime also limits stored values to 1,000,000 characters
  (how-it-works, "Durable value and result limits"). The guide: "Validate and truncate sandbox console output before
  feeding it back to the model" (`docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:295`).
- **Sources.** `v1:packages/codemode/README.md:284` "Exceeding a configured `maxOutputBytes` never fails the
  execution."; `internal/execsvc/execsvc.go:413-417`;
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#code-and-output-utilities ("The default budget
  is `6000` estimated tokens"); https://developers.cloudflare.com/agents/tools/codemode/api-reference/#openapimcpserver
  ("Text responses are limited to approximately 6,000 tokens")

## CODE-17 Concurrent tool calls within one program

- **What.** opencode v1 runs at most 8 eagerly started tool calls at once, through a semaphore. v2 removed the cap and
  tells the model to use `Promise.all`.
- **Where.** opencode v1 (8), v2 (unrestricted). Cloudflare advises sequential calls whenever a run might pause
  (CODE-37). lootbox and mcpx have no per-script cap.
- **mcpx @ 05c78b2.** None per script. The pool caps instances per server at 4 (`internal/defaults/defaults.json:3`);
  `Promise.all` over 50 calls to a shared server multiplexes on one process.
- **Value to mcpx.** + low: a per-run cap would protect upstream servers from a single fan-out.
- **Effort.** S.
- **Risk.** Low. The other direction matters more: an opencode v2 script sends concurrent `tools/call` to mcpx on one
  connection, and mcpx's stdio server handles requests in line (transports register), so they serialise.
- **Detail.** v1: "At most 8 tool calls run concurrently" and "Neither is part of the public contract"
  (`v1:packages/codemode/README.md:249`, `:288`); the constant feeds `Semaphore.makeUnsafe(TOOL_CALL_CONCURRENCY)`
  (`v1:packages/codemode/src/interpreter/runtime.ts:628`).
- **Sources.** `v1:packages/codemode/src/stdlib/promise.ts:6` `export const TOOL_CALL_CONCURRENCY = 8`;
  `v2:packages/codemode/README.md:187` "Tool-call concurrency is unrestricted.";
  `v2:packages/core/src/codemode/tool.ts:67`

## CODE-18 Upstream call cancelled when the script times out or dies

- **What.** When a script times out or dies, mcpx cancels its in-flight upstream request with `notifications/cancelled`.
  lootbox's per-call timeout abandons the promise, and a killed script's calls keep running in the daemon.
- **Where.** mcpx (request context, then `notifications/cancelled`). opencode (`signal` on `callTool`, or fiber
  interruption; v2 on modern HTTP aborts the request instead). lootbox: neither. Cloudflare: unverified.
- **mcpx @ 05c78b2.** `/v1/call` passes `r.Context()` to `reg.Call` (`internal/daemon/server.go:696`); when that context
  ends, the client sends `notifications/cancelled` (`internal/mcpclient/client.go:709-716`).
- **Value to mcpx.** + low: already works.
- **Effort.** S.
- **Risk.** n/a.
- **Detail.** mcpx's `reason` is always `"timeout"`, even when the cause is a disconnect or a kill
  (`internal/mcpclient/client.go:714`). lootbox passes no `signal` (`.lootbox/src/lib/rpc/execute_mcp.ts:61`); the SDK's
  own 60 s default (`@modelcontextprotocol/sdk@1.22.0/dist/esm/shared/protocol.js:5`) eventually sends
  `notifications/cancelled` itself, 30 s after lootbox's `rpcTimeout` gave up.
- **Sources.** `.lootbox/src/lib/rpc/execute_mcp.ts:199-208`; `internal/daemon/server.go:696`;
  `internal/mcpclient/client.go:709-716`

## CODE-19 Final top-level expression becomes the result

- **What.** In opencode v2 a program without `return` returns its last top-level expression, REPL-style. Cloudflare's
  `createCodeTool` "auto-returns the last expression". v1 returns `null`. lootbox has no return value at all, and mcpx
  uses stdout.
- **Where.** opencode v2 and Cloudflare. v1: `null`. lootbox: "Do not return from scripts always log". mcpx: stdout,
  `emit()`, or a default export's return value.
- **mcpx @ 05c78b2.** The `mcpx exec` prelude only binds names (`internal/execsvc/execsvc.go:607-625`); output is
  stdout, `emit()` or a default export (`AGENTS.md:53`, `AGENTS.md:69-71`).
- **Value to mcpx.** + med: models naturally end with `await ns.tool({...})` and expect a result; REPL semantics save a
  turn when they forget `console.log`.
- **Effort.** M — last-expression detection and rewrite for `exec` snippets only, not named scripts.
- **Risk.** A snippet ending in an expression with side effects prints where it did not before.
- **Detail.** v1: "A program that returns `undefined`, including by reaching the end without `return`, produces `null`"
  (`v1:packages/codemode/README.md:65`); v2 also maps `undefined` to `null`. Cloudflare's 2025 blog said results come
  back "by invoking `console.log()`"; the current SDK returns the function's value as `result` with `logs` beside it
  (`CodeOutput = { result: unknown; logs?: string[] }`, API reference), so the blog is historical. The guide says the
  model sees "typically the output of `console.log` statements or a final return value"
  (`docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:284`).
- **Sources.** `v2:packages/codemode/README.md:20` "Without an explicit `return`, the final top-level expression becomes
  the result."; `.lootbox/LLM_QUICK_START.md:93`; https://developers.cloudflare.com/dynamic-workers/examples/codemode/
  ("`createCodeTool`": "auto-returns the last expression")

## CODE-20 What happens to un-awaited calls when the program ends

- **What.** When the program finishes, opencode v1 awaits un-awaited tool calls and reports a failure among them as an
  unhandled rejection; v2 interrupts them and returns unhandled rejections as warnings.
- **Where.** opencode v1 awaits; v2 interrupts. In lootbox and mcpx a pending `fetch` keeps the runtime's event loop
  alive, so the calls complete (runtime semantics, not code). Cloudflare: unverified.
- **mcpx @ 05c78b2.** No explicit handling; pending calls finish unless the timeout kills the process group
  (`internal/runner/runner.go:424-438`).
- **Value to mcpx.** + low: informational; a model moving between harnesses meets different semantics.
- **Effort.** S.
- **Risk.** Under v2, a forgotten `await` on a write cancels it silently.
- **Detail.** v2's tool description warns the model: "Await every call whose completion matters; pending calls are
  interrupted when execution ends" (`v2:packages/core/src/codemode/tool.ts:67`). The flip between versions is one of two
  (with CODE-17) where instructions written for one version mislead on the other.
- **Sources.** `v1:packages/codemode/README.md:249` "When a program completes, still-running un-awaited calls are
  awaited"; `v2:packages/codemode/README.md:18-19` "anything still running is interrupted."

## CODE-21 Working directory the script runs in

- **What.** The lootbox CLI sends the script's **text** over the WebSocket; the daemon writes it to a temp file and
  spawns `deno` with no `cwd`, so relative paths resolve against the daemon's directory (launchd sets `~/.config/nix`),
  not the caller's.
- **Where.** lootbox always runs daemon-side. mcpx runs locally with the caller's cwd by default; `/v1/exec` and
  `--remote` pass the caller's `cwd`. opencode and Cloudflare have no working directory.
- **mcpx @ 05c78b2.** The runner inherits the caller's directory (`internal/runner/runner.go:354-358`); `/v1/exec` takes
  `cwd`/`env`/`stdin` as the caller's context (`docs/exec.md:102-105`).
- **Value to mcpx.** + low: already solved (but the plugin's `mcpx_exec` never sends `cwd`; plugin-API register,
  PLG-39).
- **Effort.** S — done.
- **Risk.** A lootbox script that writes `./out.json` writes into `~/.config/nix`.
- **Detail.** lootbox sends stdin as a JS constant, `const $STDIN = <json>`, prepended to the source
  (`.lootbox/src/lib/lootbox-cli/exec.ts:155-168`). With no `--allow-read` by default (CODE-08), the cwd mostly matters
  for writes, which also need a permission.
- **Sources.** `.lootbox/src/lib/lootbox-cli/exec.ts:40` `ws.send(JSON.stringify({ script, id }));`;
  `.lootbox/src/lib/execute_llm_script.ts:20`; `nix:configuration.nix:742` `WorkingDirectory =
  "/Users/drewry.pope/.config/nix";`

## CODE-22 Caller chooses local or remote execution

- **What.** `exec.where` is `auto`, `local` or `remote`. `auto` runs locally when the daemon is local. A remote run
  sends the script text, not its path. `--local` against a remote daemon is refused.
- **Where.** mcpx only. lootbox is always daemon-side (CODE-21); opencode always in-process; Cloudflare always on the
  platform.
- **mcpx @ 05c78b2.** `docs/exec.md:54-73`; the CLI resolves where to run at `internal/cli/commands.go:520-530`.
- **Value to mcpx.** + med: a remote daemon is usable without every call crossing the network.
- **Effort.** S — done.
- **Risk.** `--export` and relative imports do not survive a remote run; both are refused (`docs/exec.md:69-73`).
- **Detail.** Remoteness is declared by the caller, never inferred from the transport (`docs/exec.md:189-192`).
- **Sources.** `docs/exec.md:54` "`exec.where` is `auto`, `local` or `remote`"

## CODE-23 How arguments reach a script

- **What.** A lootbox script receives input only on stdin, through an injected `stdin()` helper with `.json()`,
  `.text()`, `.lines()` and `.raw()`. mcpx passes `Deno.args`, calls a default export with argv, calls a named export
  with `--export`, and fills recipe `@param` placeholders. A Cloudflare snippet takes one input value.
- **Where.** lootbox stdin; mcpx argv, exports, placeholders; Cloudflare `codemode.run(name, input?)`; opencode none.
- **mcpx @ 05c78b2.** `AGENTS.md:25`, `AGENTS.md:69-71`; launcher at `internal/runner/runner.go:530-579`; recipes at
  `internal/recipes/recipes.go:12-17`.
- **Value to mcpx.** + low: already covered.
- **Effort.** S.
- **Risk.** n/a.
- **Detail.** lootbox injects `stdin` only when stdin is not a TTY (`.lootbox/src/lib/lootbox-cli/exec.ts:156`).
  Combined with the type check (CODE-06), a script that calls `stdin()` run from a terminal would fail with "Cannot find
  name 'stdin'" (inferred, not run).
- **Sources.** `.lootbox/src/lib/lootbox-cli/exec.ts:158-167`; `.lootbox/README.md:219-226`; `AGENTS.md:25`

## CODE-24 Where named scripts are looked up

- **What.** lootbox tries the path as given, then `<scripts_dir>/<file>` in one directory. mcpx walks up from the
  working directory looking for `.mcpx/scripts/`, then checks `~/.config/mcpx/scripts/`; the nearest wins.
- **Where.** lootbox (one directory, shared across repos in practice through `lootbox-link` symlinks); mcpx (search
  path); Cloudflare (named snippets in durable storage, CODE-39); opencode (none).
- **mcpx @ 05c78b2.** `AGENTS.md:21-22`; `builtinScriptDirs` at `internal/cli/scripts.go:69-77`.
- **Value to mcpx.** + low: per-repo scripts already work.
- **Effort.** S.
- **Risk.** n/a.
- **Detail.** lootbox lists scripts by parsing the first JSDoc block for a description and `@example` lines
  (`.lootbox/src/lib/lootbox-cli/scripts.ts:14-51`).
- **Sources.** `.lootbox/src/lib/lootbox-cli/exec.ts:103-120`; `nix:pkgs/lootbox-link/lootbox-link.sh:5`; `AGENTS.md:21`
  "A named script lives at"

## CODE-25 Named injection points in the script launcher

- **What.** mcpx wraps each script in a generated launcher with named injection points: before, prefix, onSuccess,
  onError, suffix. The launcher can be replaced by a template or removed (`none`).
- **Where.** mcpx only. lootbox prepends an `import` line (`.lootbox/src/lib/execute_llm_script.ts:18`); opencode and
  Cloudflare wrap the code in an async function.
- **mcpx @ 05c78b2.** `internal/runner/runner.go:190-205` (phases); launcher `none` at `:497-502`.
- **Value to mcpx.** + low: an operator can add tracing or cleanup around every script without editing scripts.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The launcher imports the module dynamically so that injected lines really run first
  (`internal/runner/runner.go:523-525`). `docs/exec.md:39-43` explains why these knobs stay CLI-only rather than joining
  the `/v1/exec` wire type.
- **Sources.** `internal/runner/runner.go:162-165`; `internal/runner/runner.go:190-205`

## CODE-26 `emit()` streams values out while the script runs

- **What.** `emit(value)` writes each value as it is produced. `/v1/exec` can stream ordered frames: start,
  log/emit/stdout, artifact metadata, result or error, end, then artifact bodies. The format is SSE, or NDJSON on
  request.
- **Where.** mcpx only. lootbox buffers everything with `.output()` until the process exits. opencode streams only
  tool-call status to its UI (plugin-API register, PLG-37), never program output. Cloudflare returns `{result, logs}` at
  the end.
- **mcpx @ 05c78b2.** `AGENTS.md:62-67`; `docs/exec.md:137-148`; `emitResult` at `internal/codegen/emit.go:847`.
- **Value to mcpx.** + med: long browser scripts can report progress, and a consumer can hang up before paying for
  artifact bodies.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** A disconnecting stream consumer cancels the run context and the process group is killed
  (`docs/exec.md:156-160`). The plugin does not consume the stream (PLG-37).
- **Sources.** `.lootbox/src/lib/execute_llm_script.ts:38-43`; `docs/exec.md:129` "**stream** — frames as they happen."

## CODE-27 Structured log records on a side channel

- **What.** `log.info("msg {x}", {x})` writes a record to stderr behind a sentinel (`\x1emcpx\x1e`). The runner
  separates records from plain stderr and collects them into `logs[]` with level and attributes; call sites are captured
  for warn and above.
- **Where.** mcpx only. lootbox gives raw stderr (and drops it on success, CODE-28). opencode and Cloudflare give `logs:
  string[]` of console output.
- **mcpx @ 05c78b2.** Sentinel at `internal/codegen/emit.go:556`; parsing at `internal/runner/runner.go:374-398`.
- **Value to mcpx.** + low: a machine-readable account of why a script failed, queryable later through `mcpx_log`.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** `captureConsole` mirrors `console.*` into records and is on by default
  (`internal/defaults/defaults.json:35`).
- **Sources.** `internal/codegen/emit.go:556` `const LOG_SENTINEL = "\u001emcpx\u001e";`; `AGENTS.md:56-57`

## CODE-28 lootbox drops stderr on success and stdout on failure

- **What.** lootbox's executor returns `{output, warnings: stderr}` on success and `{error: stderr, output: stdout}` on
  failure, but the message router forwards only `output` on success and only `error` on failure. `console.error` from a
  successful run, and partial stdout from a failed one, never reach the caller.
- **Where.** lootbox. mcpx keeps both. opencode appends logs to the output on both paths. Cloudflare returns `logs` in
  both its `completed` and `error` variants.
- **mcpx @ 05c78b2.** stderr is kept so a non-zero exit is explained (`internal/execsvc/execsvc.go:430-438`,
  `:518-525`).
- **Value to mcpx.** + low: already correct.
- **Effort.** S — done.
- **Risk.** n/a.
- **Detail.** lootbox's history DB stores both fields (`.lootbox/src/lib/execute_llm_script.ts:59-67`, `:77-84`), so the
  data exists; only the WebSocket reply drops it.
- **Sources.** `.lootbox/src/lib/rpc/managers/message_router.ts:111-115`; `.lootbox/src/lib/execute_llm_script.ts:86-90`

## CODE-29 One JSON document per run

- **What.** mcpx returns `{runId, result, emits[], logs[], stdout, artifacts[], exitCode, durationMs, error?}` from
  `--json run`, `/v1/exec` and `mcpx_exec`. lootbox's CLI prints the stdout text or the error text and exits 0 or 1.
- **Where.** mcpx; opencode (`CodeMode.Result` internally, flattened to text for the model); Cloudflare
  (`ProxyToolOutput` with status `completed`, `paused` or `error`). lootbox has nothing comparable.
- **mcpx @ 05c78b2.** `internal/execsvc/execsvc.go:201-213`; `AGENTS.md:73-74`.
- **Value to mcpx.** + low: already there.
- **Effort.** S.
- **Risk.** n/a.
- **Detail.** When stdout parses as JSON it is also offered parsed as `result` (`internal/execsvc/execsvc.go:527-535`),
  a rule inherited from `mcpx --json run` (`docs/exec.md:134-135`). opencode's `Success` carries `value`, `logs`,
  `truncated` and `toolCalls` (`v1:packages/codemode/README.md:128-135`).
- **Sources.** `internal/execsvc/execsvc.go:201-213`; `.lootbox/src/lib/lootbox-cli/exec.ts:82-93`;
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#runtime-tool-input-and-output (`| { status:
  "paused"; executionId: string; pending: PendingAction[]; }`)

## CODE-30 Failures reported as typed kinds

- **What.** opencode returns failures as data with a `kind`: ParseError, UnsupportedSyntax, UnknownTool,
  InvalidToolInput, InvalidToolOutput, InvalidDataValue, ToolCallLimitExceeded, TimeoutExceeded, ToolFailure,
  ExecutionFailure, and Truncated in v2. Cloudflare's runtime uses `status: "error"` and never throws through the tool
  call.
- **Where.** opencode v1/v2 and Cloudflare. mcpx reports `exitCode`, `timedOut` and an `error` string; lootbox an error
  string and exit 1.
- **mcpx @ 05c78b2.** `Result.Error string`, `TimedOut bool`, `ExitCode int` (`internal/execsvc/execsvc.go:201-213`);
  nothing separates "script threw", "tool failed", "argument invalid", "did not start" and "timed out".
- **Value to mcpx.** + med: an agent could retry on a tool failure and fix its code on a parse error. The information
  exists today and is lost in a string.
- **Effort.** M — a kind enum, propagated from the runner's failure paths plus a `ToolError` marker.
- **Risk.** Low.
- **Detail.** v2 adds non-fatal `warnings` beside success (`v2:packages/codemode/README.md:127`). Cloudflare: "Custom
  executors should report failures in `ExecuteResult.error` instead of throwing" (API reference, "Executor"). The guide
  asks that an uncaught error be surfaced "as the script's result so the model can self-correct"
  (`docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:302-303`).
- **Sources.** `v2:packages/codemode/README.md:145-159`; `v1:packages/codemode/README.md:292-305`;
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#runtime-tool-input-and-output ("Sandbox and
  replay errors use the `error` output variant.")

## CODE-31 Budgeted catalogue: every namespace, signatures to about 2,000 tokens

- **What.** Every namespace is listed with its tool count; full signatures are then added one namespace at a time,
  cheapest first, until an estimated budget of 2,000 tokens (4 characters per token) runs out. The catalogue says when
  it is partial.
- **Where.** mcpx (`mcpx catalog`, `mcpx_catalog`), opencode v1 and v2, all defaulting to 2,000. lootbox lists namespace
  names and counts only. Cloudflare either puts every type in the tool description (`createCodeTool`, `codeMcpServer`)
  or lists only connector names (durable runtime).
- **mcpx @ 05c78b2.** `Catalog` (`internal/codegen/catalog.go:53-68`); budget in
  `internal/defaults/defaults.json:30-32`; `charsPerToken = 4` (`internal/codegen/catalog.go:30`).
- **Value to mcpx.** + low: parity. The difference is delivery: opencode puts the catalogue in front of the model
  automatically (v1 in the `execute` tool description, v2 in session instructions); mcpx needs a call, except through
  the plugin.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The unit is tokens, not characters, in all three. `OPENCODE-V2.md:45` says v2 inlines "2,000 characters";
  the source estimates tokens as `Math.round(text.length / CHARACTERS_PER_TOKEN)` with `CHARACTERS_PER_TOKEN = 4`
  (`v2:packages/core/src/codemode/catalog.ts:49-50`, `:187-188`), about 8,000 characters. v2 charges namespace header
  lines against the budget (`v2:packages/core/src/codemode/catalog.ts:96-113`); v1 counts only full entries
  (`v1:packages/codemode/README.md:184`); mcpx charges headers too (`internal/codegen/catalog.go:64-66`). lootbox's
  `/rpc-namespaces` still tells the model to call a `get_namespace_types()` function that does not exist
  (`.lootbox/src/lib/rpc/managers/type_generator_manager.ts:258-266`). Cloudflare's durable runtime spends a turn
  deliberately: the model calls `codemode.search()`/`describe()` in one execution and acts in the next (how-it-works,
  "What the model sees"). lootbox's discovery re-lists every server on every request and waits for every server at
  startup (process-model register, PM-23 and PM-22).
- **Sources.** `v2:packages/core/src/codemode/catalog.ts:50` `const INLINE_BUDGET = 2_000`;
  `v1:packages/codemode/src/tool-runtime.ts:86` `const defaultCatalogBudget = 2_000`;
  `internal/codegen/catalog.go:53-68`; `.lootbox/src/lib/rpc/managers/type_generator_manager.ts:188-200`;
  `OPENCODE-V2.md:45` "2,000 characters inline"

## CODE-32 Catalogue changes sent as a delta when shorter

- **What.** When tools change mid-session, v2 re-renders its instructions as added/changed/removed entries (or namespace
  count changes when the catalogue is partial), and falls back to the full catalogue when that is shorter.
- **Where.** opencode v2. v1's `describeCatalog` re-renders everything
  (`v1:packages/opencode/src/tool/registry.ts:283-289`). mcpx has the same machinery. lootbox and Cloudflare have none.
- **mcpx @ 05c78b2.** `Fingerprint`, `Diff`, `ShorterOf` (`internal/codegen/catalog.go:244-357`).
- **Value to mcpx.** + low: parity; it needs a long-lived consumer, and the plugin is that consumer.
- **Effort.** S.
- **Risk.** None.
- **Detail.** v2 always sends a full replacement when the catalogue flips between partial and complete, or when any
  namespace description changes (`v2:packages/core/src/codemode/instructions.ts:39-50`).
- **Sources.** `v2:packages/core/src/codemode/instructions.ts:34-37`; `internal/codegen/catalog.go:244-357`

## CODE-33 Pinned tools always shown in the catalogue

- **What.** A v2 tool registered with `options.pinned === true` is always in the inlined catalogue, and its cost comes
  off the budget first.
- **Where.** opencode v2. mcpx has `--bias words` to favour matching tools. The others have neither.
- **mcpx @ 05c78b2.** Bias scoring only (`internal/codegen/catalog.go:223`).
- **Value to mcpx.** + low: an operator could pin, say, `chrome_devtools.take_screenshot`.
- **Effort.** S — a per-server `pinned` list in config.
- **Risk.** None.
- **Detail.** Pinned entries are removed from the round-robin selection order
  (`v2:packages/core/src/codemode/catalog.ts:79-91`).
- **Sources.** `v2:packages/core/src/codemode/catalog.ts:79-84`; `v2:packages/core/src/codemode/tool.ts:162-166`

## CODE-34 Tool search callable from inside the program

- **What.** A script can search the catalogue without ending the run. mcpx: synchronous `search(query, limit = 20)` and
  `describe(name)`. opencode v1: `await tools.$codemode.search({query, namespace, limit, offset})`, through a reserved
  namespace. opencode v2: a synchronous `search(...)` built-in. Cloudflare: `await codemode.search(query)` (top 50) and
  `codemode.describe(target)`.
- **Where.** mcpx, opencode v1/v2, Cloudflare. lootbox has no search, not even a CLI command.
- **mcpx @ 05c78b2.** `search` at `internal/codegen/emit.go:1121`, `describe` at `:1167`; also `mcpx search` and the
  `mcpx_search` MCP tool (`internal/mcpserver/server.go:307`).
- **Value to mcpx.** + low: parity.
- **Effort.** S.
- **Risk.** None.
- **Detail.** v1 is asynchronous and namespaced, not a synchronous built-in like v2's: the reserved namespace is
  `"$codemode"` and the default limit 10 (`v1:packages/codemode/src/tool-runtime.ts:85-87`). v1 scores exact path 20,
  path substring 8, description 4, searchable text 2, with naive singularisation (`v1:packages/codemode/README.md:206`),
  and advertises search only when the catalogue is partial but always registers it
  (`v1:packages/codemode/README.md:195`). v2 search also matches enclosing namespace descriptions and counts toward
  `maxToolCalls` (`v2:packages/codemode/README.md:170-172`). Cloudflare's search also returns snippets (CODE-36).
- **Sources.** `internal/codegen/emit.go:1121` `export function search(query: string, limit = 20): ToolMatch[] {`;
  `v1:packages/codemode/README.md:198`; `v2:packages/codemode/README.md:170`;
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#sandbox-codemode-api ("Results are ranked and
  limited to `50`.")

## CODE-35 In-program search pages its results

- **What.** opencode's search takes `offset` and returns `remaining` and `next: {offset} | null`. An empty query with a
  `namespace` lists that namespace; a query naming an exact path is a lookup.
- **Where.** opencode v1/v2. mcpx's `search` has no offset. Cloudflare returns `total` and `truncated` but no cursor.
- **mcpx @ 05c78b2.** `export function search(query: string, limit = 20)` (`internal/codegen/emit.go:1121`).
- **Value to mcpx.** + low: a model can browse a large namespace (chrome's 27 tools) without guessing words.
- **Effort.** S.
- **Risk.** None.
- **Detail.** v2's `search` counts against `maxToolCalls` (CODE-15). v1 says to "spread it into the original request to
  preserve its query, namespace, and limit" (`v1:packages/codemode/README.md:206`).
- **Sources.** `v1:packages/codemode/README.md:206` "`next` is `{ offset }` when another page exists and `null` on the
  final page"; `internal/codegen/emit.go:1121`

## CODE-36 In-program search also finds saved snippets

- **What.** Cloudflare's `codemode.search()` ranks connector methods and saved snippets together, and a snippet runs
  with `codemode.run(name, input)`. mcpx's in-script `search()` covers tools only; saved scripts ("recipes") are matched
  from the CLI or the daemon, not from inside a script.
- **Where.** Cloudflare. mcpx has recipe matching outside scripts (`internal/recipes/match.go`). lootbox lists scripts
  with `lootbox scripts`. opencode has no saved scripts.
- **mcpx @ 05c78b2.** `search()` reads `toolMeta` only (`internal/codegen/emit.go:138-146`, `:1121`).
- **Value to mcpx.** + low: an agent mid-script could find "screenshot-and-diff" and reuse it.
- **Effort.** M — emit recipe metadata into the client and add `run(name, args)`.
- **Risk.** None.
- **Detail.** Cloudflare snippets come from host-reviewed executions (CODE-39), so what search returns was vetted; mcpx
  recipes are whatever is on disk.
- **Sources.** `internal/recipes/recipes.go:1` "Package recipes turns saved scripts into parameterised, matchable
  units."; https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#platform-sdk ("`codemode.search()`
  searches connector methods and saved snippets.")

## CODE-37 Approval pauses a run by aborting it and replaying it

- **What.** A Cloudflare connector method marked `requiresApproval` records a pending action and aborts the pass. After
  approval the whole program re-runs with the same execution id; calls already applied return their recorded results,
  and a replay that diverges fails. `codemode.step(name, fn)` runs a closure once and records its result so replays
  return the recorded value.
- **Where.** Cloudflare durable runtime only. opencode asks a permission question per call, synchronously, for every
  child call (plugin-API register, PLG-36). mcpx can ask through elicitation (`elicit.confirmDestructive`,
  `internal/settings/consumer.go:47`). lootbox: none.
- **mcpx @ 05c78b2.** No durable pause; a script waiting on an elicitation stays blocked in its own process.
- **Value to mcpx.** − mcpx scripts have full local authority and side effects (filesystem, subprocesses) that cannot be
  replayed deterministically; the design assumes determinism mcpx cannot give.
- **Effort.** XL.
- **Risk.** None; not a candidate.
- **Detail.** Cloudflare warns: "Calls in `Promise.all()` can arrive in different orders across passes and cause replay
  divergence" (how-it-works, "Deterministic replay"). Connector calls do not need `step()`; the runtime records them
  already. The guide allows "categorical approval" per run but says "the broker must still evaluate each call against
  that grant" (`docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:290`).
- **Sources.** `internal/settings/consumer.go:47`;
  https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#approvals-through-abort-and-replay ("the runtime
  records the action as pending and aborts the current pass.");
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#sandbox-codemode-api ("`step(name, fn)` | Runs
  a closure once and records its result.")

## CODE-38 Rollback through a per-tool `revert`

- **What.** `runtime.rollback()` walks the applied connector calls in reverse and calls each tool's `revert(args,
  result)` where one is defined.
- **Where.** Cloudflare only.
- **mcpx @ 05c78b2.** None.
- **Value to mcpx.** − MCP tools have no `revert`; mcpx would have to invent one per server.
- **Effort.** XL.
- **Risk.** None; not a candidate.
- **Detail.** "Rollback is compensation, not database transaction isolation." (how-it-works, "Rollback").
- **Sources.** `internal/settings/consumer.go:47` (mcpx's only confirmation mechanism);
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#connector-tool-types (`revert?: (args: unknown,
  result: unknown, ctx?: ToolExecuteContext) => Promise<void> | void;`)

## CODE-39 Only the host may promote a run to a saved snippet

- **What.** In Cloudflare the model cannot save its own code: the application reviews an execution and calls
  `runtime.saveSnippet(name, {executionId, description})`.
- **Where.** Cloudflare. lootbox and mcpx let the agent write files into the scripts directory. opencode has no saved
  scripts.
- **mcpx @ 05c78b2.** Agents write `.mcpx/scripts/<name>.ts` directly (`AGENTS.md:21-22`).
- **Value to mcpx.** − mcpx's model is "the agent is the user"; a review gate would get in the way of the main workflow.
- **Effort.** M.
- **Risk.** An agent can plant a script that a later agent runs through recipe matching (CODE-36).
- **Detail.** "The API accepts any execution status, so verify that the execution completed successfully before saving
  it." (how-it-works, "Snippets").
- **Sources.** `AGENTS.md:21-22`; https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#snippets ("The
  model does not promote its own code.")

## CODE-40 Tool names that are JavaScript reserved words get a suffix

- **What.** Cloudflare's `sanitizeToolName` replaces separators, strips invalid characters, prefixes a leading digit
  (`3d-render` becomes `_3d_render`) and suffixes reserved words (`delete` becomes `delete_`). mcpx does the first three
  but not reserved words.
- **Where.** Cloudflare (`McpConnector`, `generateTypesFromJsonSchema`). mcpx: invalid characters and leading digit.
  lootbox: invalid characters only. opencode keeps the original name with bracket notation (CODE-43).
- **mcpx @ 05c78b2.** `ToolFuncName` keeps `delete`, `new` or `class` as is (`internal/codegen/ts.go:397-418`).
  `toolDecl` emits `function delete(args…)` inside a declaration block (`internal/codegen/emit.go:97`), which is not
  valid TypeScript; `toolImpl` emits method shorthand (`internal/codegen/emit.go:217`), which is.
- **Value to mcpx.** + low: `mcpx types` prints invalid TypeScript for a tool named `delete` or `import`, and
  `AGENTS.md:99-100` calls that output authoritative. The runtime client and `--typecheck` (which checks against `typeof
  __mcpx.<ns>`, `internal/codegen/emit.go:1432`) are unaffected.
- **Effort.** S — a reserved-word table in `ToolFuncName`.
- **Risk.** An agent that pastes `mcpx types` output into a `.d.ts` gets a parse error. Low.
- **Detail.** `AGENTS.md:108-109` documents `fancy-name` becoming `fancy_name` and says nothing about reserved words.
  ASSESSMENT lists a `ToolFuncName("9lives")` bug already fixed (`ASSESSMENT.md:258-259`).
- **Sources.** `internal/codegen/ts.go:413-416`; https://developers.cloudflare.com/agents/tools/codemode/mcp/ (step 2:
  "`delete` becomes `delete_`");
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#code-and-output-utilities ("suffixes JavaScript
  reserved words")

## CODE-41 Namespace names checked against reserved words and prelude names

- **What.** Cloudflare validates namespaces: each must be a valid identifier, unique, and must not shadow an executor
  global. mcpx's `SanitizeNamespace` fixes invalid characters and a leading digit but checks neither reserved words
  (`default`, `import`, `new`, `delete`) nor the prelude's own names (`tools`, `log`, `emit`, `call`, `artifact`,
  `readResource`, `ToolError`).
- **Where.** Cloudflare validates. mcpx does not. lootbox is safe because MCP namespaces always carry an `mcp_` prefix.
  opencode uses bracket notation.
- **mcpx @ 05c78b2.** `SanitizeNamespace` (`internal/config/config.go:407-416`). The client emits `export const <ns> =
  {` per namespace (`internal/codegen/emit.go:122`); the `exec` prelude imports `tools, { call, readResource, artifact,
  log, emit, ToolError, installGlobals, captureConsole }` and then `const { <every ns> } = tools;`
  (`internal/execsvc/execsvc.go:610-617`).
- **Value to mcpx.** + med: a server configured as `log` or `default` would, going by the code, make the client or the
  prelude fail to parse, breaking **every** `mcpx exec` and script, not just calls to that server. Not run.
- **Effort.** S — reject or suffix reserved and prelude names in `SanitizeNamespace` and say so in `mcpx ls`, or have
  the prelude skip colliding names.
- **Risk.** One unlucky server name takes out all scripting, with a parse error that does not name the server.
- **Detail.** An explicit `namespace` in config bypasses the sanitiser: `pick(ex.Namespace, SanitizeNamespace(name))`
  (`internal/config/config.go:480`); whether that value is validated elsewhere is unchecked.
- **Sources.** `internal/config/config.go:407-416`; `internal/execsvc/execsvc.go:610-617`;
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#dynamicworkerexecutor ("Names must be valid
  JavaScript identifiers, unique, and must not shadow executor globals.")

## CODE-42 Two tool names that sanitise to the same identifier

- **What.** Cloudflare's `McpConnector` throws when two source names produce the same identifier, and `toolName()` can
  be overridden to tell them apart. mcpx emits duplicate object keys, so the last one wins at run time and a type check
  fails. lootbox's lookup uses `find`, so the first wins, and colliding server names silently overwrite each other.
- **Where.** Cloudflare detects it; mcpx and lootbox do not; opencode avoids it by not renaming (CODE-43).
- **mcpx @ 05c78b2.** No collision check in `Module` or `toolImpl` (`internal/codegen/emit.go:108-146`, `:204-219`); rg
  finds no "collid" or "duplicate" in codegen.
- **Value to mcpx.** + med: `a-b` and `a_b` on one server currently call whichever sorts last, silently.
- **Effort.** S — detect while building the namespace and suffix `_2`, or warn.
- **Risk.** The wrong tool is called with no error.
- **Detail.** lootbox sanitises server names at load (`.lootbox/src/lib/lootbox-cli/server.ts:10-11`, `:82-83`), so
  `fff-nix` and `fff_nix` in one config collide; its tool lookup is `schemas.tools.find((t) => t.name === toolName)`
  (`.lootbox/src/lib/rpc/execute_mcp.ts:46`). The spec's SHOULD on aggregators disambiguating names is the tools
  register's TOOL-02.
- **Sources.** `internal/codegen/emit.go:108-146`; `.lootbox/src/lib/rpc/execute_mcp.ts:46`;
  https://developers.cloudflare.com/agents/tools/codemode/mcp/ (step 2: "If two source names produce the same
  identifier, the connector throws an error.")

## CODE-43 opencode keeps real tool names and uses bracket notation

- **What.** opencode keeps the original tool name in its path and renders non-identifier segments with brackets:
  `tools.context7["resolve-library-id"](…)`. Only characters outside `[a-zA-Z0-9_-]` become `_`, so hyphens survive.
- **Where.** opencode v1/v2. mcpx, lootbox and Cloudflare rename to identifiers.
- **mcpx @ 05c78b2.** Renames (`fancy-name` to `fancy_name`, `AGENTS.md:108-109`).
- **Value to mcpx.** + low: the name the model reads in upstream docs is the name it calls, and nothing can collide. −
  bracket calls are less natural, and bare-identifier binding (`chrome_devtools.x`) needs identifier namespaces anyway.
- **Effort.** M — keep the rename but also accept `tools.ns["orig-name"]` aliases.
- **Risk.** Low.
- **Detail.** v2 tells the model "Do not infer or normalize tool names; preserve bracket notation"
  (`v2:packages/core/src/codemode/tool.ts:65`) and rejects any tool part over 64 characters (names must match
  `^[A-Za-z0-9_-]{1,64}$`, `v2:packages/core/src/tool.ts:297`). v1 derives paths from `server_tool` keys by the longest
  matching server prefix (`v1:packages/opencode/src/tool/code-mode.ts:40`).
- **Sources.** `v2:packages/codemode/README.md:77-78`; `v2:packages/core/src/tool/runtime.ts:274`; `AGENTS.md:108-109`

## CODE-44 Return types generated from a tool's `outputSchema`

- **What.** opencode feeds each MCP tool's `outputSchema` into its signature, so structured results are typed. mcpx
  types every result as `any`; lootbox as the raw envelope.
- **Where.** opencode v1 (`output: entry.tool.def.outputSchema`), v2 (`output: tool.outputSchema ?? {}`), Cloudflare
  (`JsonSchemaToolDescriptor.outputSchema`). lootbox: `Promise<McpToolResult>`. mcpx: `ToolResult = any`. `outputSchema`
  first appears in 2025-06-18.
- **mcpx @ 05c78b2.** `mcpclient.Tool` parses `OutputSchema` (`internal/mcpclient/client.go:122`), but `codegen.Tool`
  has no such field (`internal/codegen/ts.go:19-23`) and every function returns `Promise<ToolResult>` with `export type
  ToolResult = any;` (`internal/codegen/emit.go:458`).
- **Value to mcpx.** + med: mcpx already unwraps `structuredContent` (content-types register, CT-15), so typing it is
  nearly free and makes `--typecheck` meaningful for results as well as arguments. The guide's step 1 is exactly this
  (`docs/docs/2026-07-28/develop/clients/client-best-practices.mdx:181`).
- **Effort.** S — thread `OutputSchema` into `codegen.Tool` and render `Promise<T & {raw}>` when present.
- **Risk.** A server's `structuredContent` may not match its schema; types are advisory. The spec says clients SHOULD
  validate structured results against it.
- **Detail.** With an outputSchema present, v2 also stops JSON-parsing text (content-types register, CT-16), so the
  declared type and the value agree. Cloudflare's illustrative types show `Output = unknown` or `[key:string]: any`. The
  guide's advice when no schema exists: "Use a generic type and move on" (`:212`), which is what mcpx does today for
  every tool. The `outputSchema` field itself, across revisions, is the tools register's TOOL-13.
- **Sources.** `v1:packages/opencode/src/tool/code-mode.ts:127` `output: entry.tool.def.outputSchema as
  SandboxTool.JsonSchema | undefined,`; `v2:packages/core/src/tool/mcp.ts:48`; `schema/2025-06-18/schema.ts:951`
  `outputSchema?: {`; `2025-06-18/server/tools.mdx:315` "Clients **SHOULD** validate structured results against this
  schema."; `internal/codegen/emit.go:458`

## CODE-45 lootbox's generated types carry no descriptions

- **What.** lootbox turns MCP tools into interfaces and method signatures with no JSDoc: tool descriptions are stored
  but never printed, and property descriptions are dropped. mcpx and opencode render JSDoc for tools and properties;
  Cloudflare renders doc comments.
- **Where.** lootbox lacks them; mcpx, opencode, Cloudflare have them.
- **mcpx @ 05c78b2.** Tool JSDoc at `internal/codegen/emit.go:81-90`; per-property `jsdoc(p, inner)` at
  `internal/codegen/ts.go:229-231`; server `Instructions` and operator `Prelude` above the signatures
  (`internal/codegen/ts.go:31-37`).
- **Value to mcpx.** + low: already done.
- **Effort.** S.
- **Risk.** n/a.
- **Detail.** Verified live: `curl 127.0.0.1:9420/types/mcp_codedb` returned bare interfaces such as
  `codedb_callpath(args: Codedb_McpCodedb_Codedb_callpathArgs): Promise<Codedb_McpToolResult>;` with no comments.
  `generateArgsInterface` keeps only name, type and optional
  (`.lootbox/src/lib/external-mcps/parse_mcp_schemas.ts:168-178`). opencode v1 adds `@default`/`@format` tags
  (`v1:packages/codemode/README.md:214`).
- **Sources.** `.lootbox/src/lib/type_system/client_generator.ts:404-479`;
  `.lootbox/src/lib/external-mcps/parse_mcp_schemas.ts:195-242`; `internal/codegen/emit.go:81-90`

## CODE-46 lootbox generates one function per MCP resource

- **What.** lootbox lists each server's resources and generates `tools.mcp_x.resource_<name>(args)` per resource, with
  arguments from URI-template variables, calling `readResource`.
- **Where.** lootbox. mcpx has one generic `readResource` export. opencode keeps resources out of code mode.
  Cloudflare's code-mode docs do not cover resources.
- **mcpx @ 05c78b2.** `readResource(uri)` (`internal/execsvc/execsvc.go:610-611`).
- **Value to mcpx.** − one function per resource bloats the types for servers with many resources; one reader is
  simpler.
- **Effort.** S — nothing to build.
- **Risk.** n/a.
- **Detail.** The template branch never runs: lootbox reads `uriTemplate` off `resources/list` entries
  (`.lootbox/src/lib/external-mcps/mcp_schema_fetcher.ts:110-116`), but templates arrive from
  `resources/templates/list`, which lootbox never calls (rg finds no `listResourceTemplates` in `.lootbox/src`). Every
  generated resource function therefore takes an empty argument object. The resources register carries the same row
  (CODE-46).
- **Also recorded from the resources register.** lootbox only. mcpx has one generic `readResource(uri)`; opencode
  keeps resources out of code mode. n/a — `readResource(uri)` (`internal/execsvc/execsvc.go:610-611`). lootbox's
  template branch never runs: it reads `uriTemplate` off `resources/list` entries, but templates come from
  `resources/templates/list`, which lootbox never calls, so every generated function takes an empty argument object.
- **Sources.** `.lootbox/src/lib/external-mcps/parse_mcp_schemas.ts:130`; `.lootbox/src/lib/rpc/execute_mcp.ts:160-194`;
  `internal/execsvc/execsvc.go:610-611`; `.lootbox/src/lib/external-mcps/mcp_schema_fetcher.ts:110-116`

## CODE-47 lootbox exposes local TypeScript files as namespaces

- **What.** Any `.lootbox/tools/<ns>.ts` that exports 0- or 1-argument functions becomes `tools.<ns>.<fn>`. Types are
  extracted with ts-morph, and each file runs in a worker process with `--allow-all`, restarted when the file changes.
- **Where.** lootbox only. mcpx turns non-MCP things into servers through config-declared CLI adapters
  (`internal/adapter/adapter.go:1-12`) and OpenAPI (`mcpx api`), and scripts import other scripts (`--export`).
- **mcpx @ 05c78b2.** No TypeScript-module-as-namespace adapter, by choice: a plain module import "already does this
  without a protocol in between" (`ASSESSMENT.md:296-299`).
- **Value to mcpx.** − ts-morph on the request path was measured as the main cost and the OOM source
  (`ASSESSMENT.md:62-64`). ASSESSMENT still calls it "the one real gap" if the user relies on it.
- **Effort.** M — a small adapter that serves a module's exports as an MCP server.
- **Risk.** Low.
- **Detail.** The deployed lootbox serves five such namespaces (`fs`, `kv`, `sqlite`, `memory`, `graphql`), per a live
  `/namespaces` call. ASSESSMENT measured extracting them at 46–209 ms each, on every discovery request
  (`ASSESSMENT.md:38`).
- **Sources.** `.lootbox/README.md:86-99`; `.lootbox/src/lib/rpc/worker_manager.ts:298-301`; `ASSESSMENT.md:299` "If you
  use that feature, it is the one real gap."

## CODE-48 lootbox YAML prompt workflows

- **What.** `lootbox workflow start|step|status|reset|abort` walks an agent through YAML-defined prompt steps:
  Handlebars templates that can loop between a minimum and maximum count, with state in `.lootbox-workflow.json` and
  events logged to SQLite.
- **Where.** lootbox only.
- **mcpx @ 05c78b2.** None, deliberately: "No workflow engine or script history. Lootbox has both; they are separable
  concerns" (`ASSESSMENT.md:294-295`).
- **Value to mcpx.** − harness-level prompt sequencing, unrelated to reaching MCP servers.
- **Effort.** M.
- **Risk.** None.
- **Detail.** None beyond the above.
- **Sources.** `.lootbox/README.md:460-471`; `.lootbox/src/lib/db.ts:71-80`; `ASSESSMENT.md:294`

## CODE-49 lootbox's script-to-gateway protocol is its own WebSocket JSON

- **What.** A lootbox client sends `{script, id}` or `{method: "ns.fn" | "mcp_ns.fn", args, id}` over `ws://…/ws`. The
  server first sends `{type: "welcome", functions, config}`, then answers `{result | error, id}`. It is neither JSON-RPC
  nor MCP; MCP calls are told apart only by the `mcp_` prefix.
- **Where.** lootbox. mcpx serves REST (`/v1/call`, `/v1/exec`) over a unix socket or loopback TCP, and MCP at `/mcp`.
  opencode is in-process. Cloudflare uses Workers RPC (CODE-51).
- **mcpx @ 05c78b2.** `POST /v1/call` (`internal/daemon/server.go:468`); the generated client posts `{server, tool,
  args}` (`internal/codegen/emit.go:524-535`).
- **Value to mcpx.** + low: mcpx already has a documented REST surface with an OpenAPI description.
- **Effort.** S.
- **Risk.** n/a.
- **Detail.** lootbox has a second socket, `/worker-ws`, for its tool workers. Any local client that sends
  `{type:"identify", workerId}` is registered as that worker's sender with no check, and that namespace's calls are then
  routed to it (`.lootbox/src/lib/rpc/managers/connection_manager.ts:125-131`); inferred, not exploited.
- **Sources.** `.lootbox/src/lib/rpc/managers/message_router.ts:16-32`;
  `.lootbox/src/lib/rpc/managers/connection_manager.ts:86-93`; `.lootbox/src/lib/rpc/websocket_server.ts:273-291`

## CODE-50 Script client reaches the daemon over a unix socket

- **What.** mcpx's generated client uses the daemon's unix socket when `MCPX_SOCKET` is set: through
  `Deno.createHttpClient({proxy:{transport:"unix"}})` on Deno and `fetch(u, {unix})` on Bun. It falls back to loopback
  TCP, which is what node always uses.
- **Where.** mcpx. lootbox uses a TCP WebSocket. opencode is in-process. Cloudflare uses Workers RPC.
- **mcpx @ 05c78b2.** `internal/codegen/emit.go:268-293`.
- **Value to mcpx.** + med: the socket's file mode becomes the access control (`defaults.PrivateMode`,
  `internal/daemon/server.go:202`), and calls are about four times faster (0.17 ms versus 0.65 ms, per the comment).
- **Effort.** S — done.
- **Risk.** node scripts fall back to TCP, which is unauthenticated and checks no `Origin` (auth register, AUTH-24).
- **Detail.** ASSESSMENT recorded a fix for socket paths over macOS's 104-byte `sun_path` limit
  (`ASSESSMENT.md:262-264`).
- **Sources.** `internal/codegen/emit.go:258-261`; `internal/codegen/emit.go:268-293`

## CODE-51 Cloudflare connects script and host through Workers RPC

- **What.** Cloudflare's sandbox gets one `Proxy` per namespace, and each call crosses to the host over Workers RPC
  (`ToolDispatcher` / `ConnectorBinding.callTool`). The host intercepts every call for approval, logging and replay.
- **Where.** Cloudflare.
- **mcpx @ 05c78b2.** HTTP to the daemon (CODE-50; `internal/codegen/emit.go:524-535`).
- **Value to mcpx.** − Workers-only.
- **Effort.** XL.
- **Risk.** None; not a candidate.
- **Detail.** "The runtime intercepts each call before the connector executes it." (how-it-works, "Connectors").
- **Sources.** `internal/codegen/emit.go:524-535`;
  https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#connectors ("Connector calls cross the sandbox
  boundary through Workers remote procedure calls (RPC).");
  https://developers.cloudflare.com/agents/tools/codemode/api-reference/#executor (`binding: { callTool(method: string,
  args: unknown): Promise<unknown>; };`)

## CODE-52 Every run recorded with its script text and output

- **What.** Every lootbox execution is written to a `script_runs` table: id, timestamp, full script text, success flag,
  full output, error, duration and session id. The write is fire-and-forget, and nothing in lootbox reads the table
  back.
- **Where.** lootbox. mcpx keeps a durable structured log (`mcpx_log`) and an artifact index. Cloudflare keeps a durable
  execution log capped at `maxExecutions` 50. opencode keeps only the session transcript.
- **mcpx @ 05c78b2.** Log records and run ids, not the script source (`internal/mcpserver/server.go:339-341`).
- **Value to mcpx.** + low: storing the source per run would let an agent "re-run what worked yesterday" and would feed
  recipe candidates. − stored output is where secrets end up.
- **Effort.** M — a retention setting and redaction.
- **Risk.** Output is stored unredacted.
- **Detail.** Observed by the research worker on 2026-09-30: the live table held 1,713 runs from 2026-04-05 to
  2026-09-30, of which 1,100 succeeded (a 36% failure rate), and **none** had a session id, because the CLI never sends
  one (`.lootbox/src/lib/lootbox-cli/exec.ts:40`). That is a baseline mcpx's own success rate can be compared with. The
  live DB is `~/Library/Application Support/lootbox/lootbox.db` (`.lootbox/src/lib/db.ts:19-23`); the
  `.lootbox/lootbox.db` file in the checkout is a stray. ASSESSMENT left history out on purpose
  (`ASSESSMENT.md:294-295`). The process-model register has the same row (CODE-52).
- **Also recorded from the process model register.** Cloudflare keeps a durable execution log capped at
  `maxExecutions` 50. mcpx keeps a structured log and an artifact index but not the script source. opencode keeps only
  the transcript. `mcpx_log` (`internal/mcpserver/server.go:339-341`); run ids without source. The live lootbox table
  holds 1,713 runs (2026-04-05 to 2026-09-30), 36% failed, and none carries a session id because the CLI never sends
  one. `lootbox.db` in the checkout is a stray.
- **Sources.** `.lootbox/src/lib/script_history.ts:33` "Don't await - fire and forget";
  `.lootbox/src/lib/script_history.ts:29-63`; `.lootbox/src/lib/db.ts:97`;
  https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#durable-execution-log ("Retention and stale
  executions"); `internal/mcpserver/server.go:339-341`

## CODE-53 opencode wraps MCP servers in code mode by default

- **What.** In v2 each MCP server config has `codemode`, default true, and every tool whose `options.codemode !== false`
  disappears from the direct tool list into `execute`. In v1 code mode is gated by `OPENCODE_EXPERIMENTAL`, which the
  user's build sets, so the installed v1 does the same for MCP tools. mcpx's own `/mcp` is therefore nested inside
  opencode's code mode unless the user opts out.
- **Where.** opencode v2: on by default, per server; a built-in plugin opts out only `executor` servers. opencode v1:
  `experimentalCodeMode` is `enabledByExperimental("OPENCODE_EXPERIMENTAL_CODE_MODE")`, the specific flag when set, else
  `OPENCODE_EXPERIMENTAL`; the nix wrapper and home profile set `OPENCODE_EXPERIMENTAL=1`.
- **mcpx @ 05c78b2.** Not listed by the exclusion plugin; nothing in mcpx's docs tells users to opt out (`codemode:
  false` in v2, `OPENCODE_EXPERIMENTAL_CODE_MODE=0` in v1). The model would write opencode JS that calls
  `tools.mcpx.mcpx_exec({source: "<TS>"})`: code nested in code.
- **Value to mcpx.** + high: one docs sentence (or a v2 plugin that sets it through `mcp.transform`) removes two
  catalogues, two error surfaces and double cost per call.
- **Effort.** S.
- **Risk.** Not done: models get confused and every call costs twice. Plans that assume v1 shows MCP tools directly are
  wrong for this machine.
- **Detail.** v2 wraps all tools, not only MCP ones; built-ins opt out individually
  (`v2:packages/core/src/tool/plugin/edit.ts:123`, `v2:packages/core/src/tool/plugin/grep.ts:77`), and the whole of code
  mode is switched off only by a deny rule on `execute`. v1 wraps MCP tools only
  (`v1:packages/opencode/src/tool/code-mode.ts:210`), registers none of them directly once the flag is on, and still
  adds three resource tools when a server declares `resources`. The exclusion plugin matches local `executor mcp` and
  remote `https://executor.sh/<x>/mcp` (`v2:packages/core/src/plugin/mcp-codemode-exclusion.ts:7`, `:16`). For remote
  servers in code mode, v2 also appends `?codemode=false` to the URL, asking servers that bundle their own code mode for
  raw tools, and retries without it on 400 or 404; mcpx neither sends nor honours it (plugin-API register, PLG-35). The
  plugin-API register holds the same default from the plugin angle (CODE-53, and PLG-27 for plugin tools).
  `OPENCODE-V2.md` is wrong at the pin in two places here. The doc calls the exclusion plugin
  "`mcp-codemode-defaults.ts`" and says it turns code mode off "for servers that are themselves code mode"
  (`OPENCODE-V2.md:53-54`); the source names it `mcp-codemode-exclusion.ts` and matches only `executor`. The doc says
  "If you stay on opencode v1 … then none of v2's Code Mode is available to you today" (`OPENCODE-V2.md:222-225`); the
  source enables it from the environment the user's build sets, and the doc's correction note says only that v1 ships it
  "behind" the flag (`OPENCODE-V2.md:4-5`).
- **Also recorded from the plugin apis register.** opencode v2 (per server, default on); opencode v1 (global, off
  unless the flag is set; this user's nix build sets `OPENCODE_EXPERIMENTAL=1`, so v1 here already runs code mode).
  Not excluded. mcpx's `/mcp` is wrapped: the model writes opencode code-mode JS that calls
  `tools.mcpx.mcpx_exec({source: "<TS>"})`, code nested in code. `OPENCODE-V2.md:53` calls the file
  `mcp-codemode-defaults.ts` and says it turns code mode off "for servers that are themselves code mode"; the file is
  `mcp-codemode-exclusion.ts` and matches only executor. v2 also adds "Use tools from this server through `execute`"
  to a code-mode server's instructions (capabilities area).
- **Sources.** `v2:packages/schema/src/mcp.ts:34-36` "Expose this server's tools through Code Mode. Defaults to true.";
  `v2:packages/core/src/tool.ts:228-229`; `v2:packages/core/src/plugin/mcp-codemode-exclusion.ts:16`;
  `v1:packages/opencode/src/effect/runtime-flags.ts:11-14`; `v1:packages/opencode/src/effect/runtime-flags.ts:48`;
  `v1:packages/opencode/src/session/tools.ts:388` `if (flags.experimentalCodeMode) return tools`; `nix:flake.nix:297`
  "--set-default OPENCODE_EXPERIMENTAL 1"; `nix:homeUser.nix:115`; `OPENCODE-V2.md:53` "that turns opencode's code mode"; `v2:packages/core/src/plugin/mcp-codemode-exclusion.ts:7`

## CODE-54 Code execution reachable from a shell, HTTP or CI

- **What.** opencode's code mode runs only when a model calls `execute` inside a session; no CLI command or HTTP route
  invokes it. mcpx runs scripts from the shell, `/v1/exec` and MCP; lootbox from `lootbox exec` and its WebSocket.
- **Where.** mcpx; lootbox; Cloudflare (from the Worker's own code). opencode v1 and v2: model turn only.
- **mcpx @ 05c78b2.** `mcpx exec`/`mcpx run`, `POST /v1/exec` (`internal/daemon/routes_exec.go:36`), and `mcpx_exec`
  over MCP (`internal/mcpserver/server.go:327`).
- **Value to mcpx.** + high: scripting from shell, CI, cron or another agent has no opencode equivalent; this is one of
  the two things `OPENCODE-V2.md` says to keep mcpx for (`OPENCODE-V2.md:218-220`).
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The only mention of code-mode execution in v2's protocol code is a simulation schema, and `POST
  /api/generate` is text generation (`v2:packages/protocol/src/groups/generate.ts:8`); rg over `v2:packages/server/src`
  and `v2:packages/cli/src` finds only an ACP tool-kind mapping (`v2:packages/cli/src/acp/tool.ts:15`). The doc's claim
  that nothing outside a model turn can invoke it (`OPENCODE-V2.md:62-67`) holds at the pin. Serving code execution *as*
  an MCP server (mcpx `/mcp`, Cloudflare `codeMcpServer` and `openApiMcpServer`; lootbox never) is the plugin-API
  register's PLG-41.
- **Sources.** `v2:packages/protocol/src/simulation.ts:500`; `v2:packages/protocol/src/groups/generate.ts:8`;
  `v1:packages/opencode/src/tool/registry.ts:118-119`; `internal/daemon/routes_exec.go:36`
