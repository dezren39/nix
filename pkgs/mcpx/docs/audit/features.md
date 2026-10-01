# Audit: features (end to end)

```
created:      2026-10-01
base:         origin/main 19237a0
area:         features -- opencode plugin, exec/run, daemon, recipes/prompt, adapters/apis, diagnostics, elicit
method:       each feature run against scratch state (HOME/MCPX_STATE_DIR/MCPX_CACHE_DIR under a temp dir),
              or its code path read end to end; entries already in docs/in-name-only.md not repeated
```

States: **absent**, **minimal**, **partial**, **untested** (as in `in-name-only.md`). Rows marked *works*
are kept as positive controls and are not counted.

## Opencode plugin (opencode 1.18.31, plugin types 1.15.5)

Hooks invoked under bun with fake `client`/`ctx` against a scratch daemon; not loaded into a live opencode.

| what | state | evidence | issue |
| --- | --- | --- | --- |
| `instructions` option (`experimental.chat.system.transform`) | absent | pushes onto `output.parts` (`mcpx-session.ts:745-746`); v1 type is `output: { system: string[] }` (`@opencode-ai/plugin` `index.d.ts:264-268`). `OPTS='{"instructions":true}'` → `system after: {"system":[]}` | #217 |
| `remember: "permanent"` (`mcpx_daemon_select`, TUI picker) | absent | `putSetting` sends `persist: true` (`mcpx/daemon.ts:665-667`); daemon field is a string (`internal/daemon/routes_settings.go:191`). Tool → "Writing the setting failed"; curl same body → `cannot unmarshal bool into … .persist of type string` | |
| `until-gone` remembered scope | absent | `Remembered.scope` written (`daemon.ts:1231`), never read; after `stop --all` the file still has `"scope":"until-gone"` — same as `indefinite` | |
| `toolTiming` ("one `mcpx stats` covers both") | minimal | record has no duration (`mcpx-session.ts:775-781`); `mcpx log` shows `harness.tool … tool=bash`, `mcpx stats calls` → "No tool calls in the log yet." | |
| `MCPX_HARNESS_VERSION` injection | absent | read from `OPENCODE_VERSION` (`mcpx-session.ts:373,685`), which opencode 1.18.31 does not set (env inside it lists `OPENCODE_PID` etc.) | |
| `mcpx_exec` tool session | partial | `DaemonClient.exec` posts only `{source, options}` (`daemon.ts:484-488`), `ctx.sessionID` never passed (`mcpx-session.ts:619`); session-scoped pools ignore the tool path (shell path gets `MCPX_SESSION_ID`) | #189 |
| Discovery rung 5 "ask the user" | absent | documented `docs/opencode-plugin.md:223`; `discover` skips it (`daemon.ts:1119-1122`), adapter only toasts (`mcpx-session.ts:304-308`) | |
| Headless "one warning line into the log" | absent | doc `opencode-plugin.md:310-313`; `toast()` returns early headless and writes nothing (`mcpx-session.ts:294`) | |
| `annotate` "when more than one matched" | partial | only when `d.ambiguous` (rung 6) (`mcpx-session.ts:319`); with 2 daemons resolved, no `mcpx: answered by` line | |
| `plugin.discoveryRetry` | partial | env only (`mcpx-session.ts:196`), no option despite header "option, env, default" (`:65-68`); absent from `Options` and README table | |
| `mcpx stats --opencode <dim>` (as in `skills/mcpx-observability/SKILL.md:50`) | partial | dim stays `""` when flag first (`cli/logs.go:431-460`); `stats --opencode --db $DB --top 3 models` → overview; `--by` never passed | |
| `mcpx stats opencode <dim> --db/--top/--since` | partial | flags after the dim not parsed (`logs.go:452-457`); `--db` ignored ("no opencode database found"), `--top 2 --since 24h` printed all rows, all-time | |
| `mcpx_observe` `errors`/`slowest` | partial | return `"rows": null` where others return `[]` | |
| Options `bin`, `binArgs`, `backend`, `env`, `tools`, `daemonTools`; tools discover/exec/observe/daemon_* | untested | all ran as documented (works); no test imports `mcpx-session.ts`; `internal/e2e/plugin_test.go:32` runs `bun test plugin/opencode/mcpx/` only (31 pass) — the `persist` mismatch above is what that gap let through | #141 |
| `headless` detection, TUI picker `mcpx-tui.tsx` | untested | no test; `mcpx-tui.tsx` not loadable outside opencode (`react/jsx-dev-runtime`) | |
| `internal/opencode` (stats over opencode DB) | untested | only `opencode.go`, no `_test.go`; nothing in cli/e2e tests exercises it (ran fine against the real DB) | |

## Script execution (`exec`, `run`)

| what | state | evidence | issue |
| --- | --- | --- | --- |
| Permission profiles as a boundary | partial | a script reaching the daemon can ask it for more: under `--runtime deno --permissions strict`, `fetch(MCPX_ENDPOINT+"/v1/exec", {source:"await Deno.writeTextFile(…)", options:{permissions:"all"}})` → `200`, file written; same under node `readnet`. `/v1/exec` does not cap requested permissions | |
| `artifact({path})` reads files the profile denies | partial | under `strict` `Deno.readTextFile` fails, `artifact("leak.txt",{path:"$T/secret.txt"})` succeeds (daemon reads via `x-mcpx-path`, `codegen/emit.go:1355-1358`); fetched back `[200,"secret-data\n"]`. No test for `x-mcpx-path` | |
| `read` profile | partial | `--permissions read` + any tool call → `cannot reach the mcpx daemon … Requires net access`; undocumented (`runtimes.md:63-69`) | |
| `exec.timeout` / `--exec-timeout` (default 120s) | partial | local: `--exec-timeout 1s` and env both let a 5s script print "late", rc 0; also not honoured with `--remote`. Read only at `internal/daemon/routes_exec.go:173`; CLI `--timeout` default 0 = none (`cli/commands.go:478`). No test | |
| Local `--timeout` message | minimal | local: rc 124, nothing printed; remote prints `script timed out after 1s` | |
| `--suffix` "runs last on both paths, like a finally" (`settings/registry.go:302`) | partial | `exec` snippet that throws: suffix never runs (deno/node/bun); `run` file that throws: runs. Tests cover file path only (`e2e/phases_test.go:47`) | |
| `--remote` text output | partial | `--remote --format bare 'log.info("hello"); emit(2)'` → `2` (local `hello 2`); `renderExecResult` ignores `res.Logs` (`cli/exec.go:276-283`); stderr dropped (`"logs": []`) | |
| `--remote` carries the caller's environment (`exec.md:117-121`) | partial | `FOO=bar … --remote --env A=1 'emit([env A, env FOO])'` → `["1",null]`; only `--env` sent (`exec.go:491`) | |
| `--remote` and caller's `script.profiles`/`runtimes`/`runtimeOrder` | partial | resolved from daemon settings (`routes_exec.go:173-178`): `--permissions mine` → `no permission profile "mine"` remote, works local; `runtimeOrder=bun,node` → deno remote | |
| `diagnose.preflight` on `--remote` | partial | remote run proceeds to `TypeError: tools.demo.nosuch is not a function`; `ConsumerPolicy.DiagnosePreflight` assigned `internal/daemon/consumer.go:121`, never read | |
| `--hooks-autonomy off` | partial | refuses hooks from env/config; hooks given as CLI flags all still run; help does not say | |
| `script.typecheck` | partial | deno only; node/bun warn and run `"str"`; deno detected by `strings.Contains(base,"deno")` (`preflight.go:205`) so a renamed deno runtime skips it | |
| Generated client result types from `outputSchema` | absent | `structured(...): Promise<ToolResult>`, `type ToolResult = any` despite an `outputSchema`; codegen never reads it | #216 |
| `artifact()` misuse | minimal | `artifact({name:"x",data:"y"})` stores a 0-byte `_object-Object_` under every runtime instead of throwing | |
| runtimes deno/bun/node, profile enforcement (deno, node), bun refusal, phases, artifacts store/list/get/inline, `--keep`, `--export`, `mcpx scripts` | works | `process.execPath` matched each runtime; `read`/`strict`/`readnet` denials observed; phases fired in order; artifacts listed and fetched | |

## Daemon (pools, cache, reload)

| what | state | evidence | issue |
| --- | --- | --- | --- |
| `pool.max`/`pool.idleTimeout` via `settings set` ("hot: yes") | partial | "applied to the running daemon", but `status` unchanged (`rp … 1/4`) and no reaping after 33s until a config-file edit reloaded. Handler only `SetRuntime` (`routes_settings.go:225-231`); pool reads them in `reloadConfig` → `ApplyPoolSettings` (`:555`). Test only checks persistence (`e2e/settings_test.go:76`) | |
| `min` ("keep started even when idle") | partial | `min:2` → `mn … 0/4` after a reap tick; `cfg.Min` read only in reap guard (`pool.go:992`); nothing pre-starts. No test sets it | |
| `call` scope (and session fallback) frees instances when the call ends | partial | after 4 `mcpx call ca.state`, `ca#6`/`ca#7` still live, idle 55s; only exec/run call `ReleaseCaller` (`commands.go:595`, `routes_exec.go:202`), not `CmdCall` | |
| `pid` scope from the CLI | partial | key is the mcpx client's own `os.Getpid()` (`commands.go:1521`) → two calls, two pids: per-call, not per calling program | #211 |
| Identically defined servers share one process | partial | `z` and `g` same command → same `PoolID` (`config.go:357-382`); `z.open "from-z"` then `g.state` → `seen:["from-z"]`. Undocumented | |
| Schema history on reload / `servers add` | partial | only `Warm` calls `ObserveCatalog` (`registry.go:300`, `consumer.go:93`); added server showed `No tools.` in `history` until a second `refresh` | |
| Config reload failure | partial | bad file logged at INFO ("could not be loaded"), hidden by `log --level warn`; old config kept silently | |
| Per-server pool setting change restarts its processes | minimal | `max`/`min`/`idleTimeout`/timeouts/`sharing`/`scope` hashed into `PoolID` (`config.go:379-381`) → new pool, old closed, state lost; observed `cw` back with 0 tools/instances. Undocumented | |
| shared/exclusive; global/cwd/session/parent-session/repo/worktree; `max`; idle TTL; `restart` (all/one/`--lazy`); `stop --all`; `servers add/remove` live; file reload; `refresh` | works | distinct pids per cwd/session/worktree, shared per repo; exclusive queued 3s; `idleTimeout:3s` reaped; `servers add` callable with existing pid unchanged | |

## Diagnostics (`diagnose`, `log`, `stats`, `doctor`)

| what | state | evidence | issue |
| --- | --- | --- | --- |
| Failed tool calls (`isError`) logged as success | partial | `mcpx call g.boom` rc 1, log `mcp.call … ok=true`, `stats calls` ERRORS 0; `"ok": err == nil` (`pool/pool.go:959`) | related #206 (closed) |
| `stats errors` | partial | filters `level=error` (`stats.go:308-309`); a logged `ok:false` call → `stats calls` 100% errors, `stats errors` "No errors in the log yet." | |
| `stats servers` CALLS | partial | sums `calls` from `server.stop` only (`stats.go:185-187`): 5 vs 12 in `stats calls` | |
| Attribution by server | partial | shared-PoolID calls from `z` logged as `server=g` (`pool.go:956`); `stats sessions` merges global-scope servers into one row | |
| `diagnose` argument types | absent | `g.echo({message: 1})` (string) → "Nothing to report"; no type kind (`diagnose.go:28-37`); `argument-retyped` history never checked | #111 |
| `diagnose` unknown namespace | partial | `nons.x({})` → "Nothing to report"; static check returns nil (`diagnose.go:279-283`), kind only on runtime path (`:501`) | |
| `diagnose <name>` with no such script | partial | `mcpx diagnose zz_no_such_script` and help's `mcpx diagnose report` → "Nothing to report", rc 0; falls back to treating name as source (`consumer.go:207-210`) | |
| `doctor` server health | partial | never probes; before `refresh` only the missing-binary server FAILs, a crashing and an unreachable one pass; after `refresh` all three FAIL | |
| `doctor` with invalid config | minimal | prints parse error, then spawns a daemon on a different socket → three FAILs incl. "daemon did not become ready" from one cause | |
| `diagnose` missing/unknown/renamed args, unknown tool, history; `log record`/`sql`/`--chain`/`--since`; `doctor -v` | works | e.g. ``py2.t: b is required … (schema changed …: `b` became required)``, rc 1; `log sql "delete…"` refused | |

## Recipes, `mcpx prompt`, autonomy

| what | state | evidence | issue |
| --- | --- | --- | --- |
| Settings flags on `prompt` as `--flag value` | partial | `mcpx prompt --prompt-autonomy run "…"` → `want one of propose, run, got "--set"`; `hoistFlags` knows only `autonomy`/`session`/`set` (`cli/consumer.go:397`) | |
| `prompt --mode` (decision 0002 table) | absent | `flag provided but not defined: -mode`; renamed to `--autonomy`, noted only in the decision's "Landed" note | |
| `prompt` exit status when nothing produced | partial | `prompt.sample=never` → "No recipe matched…" rc 0; `sample=ask` 5s timeout → "no model answered" rc 0 | #294 |
| Undeclared recipe values | partial | `recipes run touch-marker tag=c bogus=1` → rc 0, `bogus` dropped silently | |
| Recipe hole, no TTY/answerer | partial | blocks 45s, no `mcpx elicit answer` hint, then fails; not exit 75 like `call`/`exec`/`run` | |
| Recipe hole answered via `elicit answer` | partial | form asked `tag`; `elicit answer <id> tag=zz n=oops` accepted, overrode declared `n` → "n is a number, got \"oops\"" | |
| `recipes` CALLS column | minimal | lists `fs.writeFileSync` as a call and feeds it into match scoring | |
| CLI tests for ceiling, flag hoisting, exit codes | untested | `internal/recipes/*_test.go` covers parse/match only | |
| `autonomy.max` ceiling on `recipes run` / `prompt --run` | works | daemon restarted at each of off…run: only `run` created the marker; `propose`/`apply` printed "clamped to propose" | |

## Adapters and `paths.apis`

| what | state | evidence | issue |
| --- | --- | --- | --- |
| Adapter `timeout` | partial | `"timeout":"1s"` on `sh -c "sleep 5 & wait"` → 5.05s, `exited -1`, never "did not finish within"; `ExitError` checked before `cctx.Err()`, no process-group kill/`WaitDelay` (`adapter/adapter.go:255-261`). No test | |
| Adapter exit status | partial | program exit 3 → "fail exited 3", rc 1 (`cli/adapter.go:155`); `--json` prints no result | |
| Adapter parameter `type` at call time | absent | `{"n":"abc"}` for a number → argv `abc`; `buildArgs` checks enums only (`adapter.go:277-330`) | |
| Unknown adapter arguments | partial | `"bogus":1` ignored, rc 0 | |
| Positional starting with `-` | partial | `{"pos":"--evil"}` → argv `--evil -v`; no `--` separator, option injection | |
| Missing adapter program | partial | `adapter check` reports it; `mcpx ls` lists namespace as `unread`, 0 tools, no error | |
| API cookie parameters | absent | dropped from schema (`openapi/openapi.go:467`); when passed, sent as query `?sess=C1` (default branch `:578`) | |
| Required query params / body | absent | only required path params checked (`openapi.go:565`); missing `q` sent; `{}` → POST with `Content-Length: 0` | |
| `securitySchemes` / `security` | absent | no reference in `internal/openapi` or `internal/cli`; auth only via hand-written `headers` | |
| Auth with `mcpx api --spec` | absent | `--spec` builds `APISpec` without `Headers` (`cli/openapi_tools.go:100-106`), no header flag; server saw no `X-Key` | |
| HTTP error exit with `--json` | partial | `pets_getpet {"id":404}` rc 1, with `--json` rc 0 (`openapi_tools.go:181-183`) | |
| Spec build error | partial | `continue`d (`openapi_tools.go:173-175`); user sees only "no operation named" | |
| API tool names | minimal | `getPet` → `pets_getpet`, `pets_getPet` not found; description `"GET /pets/{id} (GET /pets/{id})"` | |
| APIs as namespaces in scripts | partial | only `mcpx serve` extras (`cli/serve.go:529`); `mcpx ls` has no `pets`, `exec 'typeof pets'` → `undefined` (adapters do appear) | |
| CLI/serve API paths | untested | no `_test.go` references `paths.apis`, `apiTools`, `CmdAPI` | |

## Elicitation and sampling via the CLI

| what | state | evidence | issue |
| --- | --- | --- | --- |
| `elicit answer key=value` types | partial | `age=3` reaches the server as `"3"` for an integer field; every value a string (`cli/elicit.go:119-126`); `show`'s hint quotes numbers too | #255 |
| `elicit show` constraints | partial | shows type/optional/enum, not `minLength`/`minimum` | #255 |
| Sampling question on the CLI | minimal | `call el.sample` rc 75 prints `asks` empty and suggests `'{}'`; `elicit show` omits the messages (only in `--json`) | #256 |
| `ask answers` rejection | partial | wrong shape → `accepted: 0`, problems listed, rc 0, HTTP always 200 (`internal/daemon/routes_proto.go:672-676`); help does not give the `<answers>` shape | |
| `ask abandon` on finished call | partial | `status: completed`, rc 0, no indication nothing was abandoned | |
| CLI elicitation tests | untested | no test on JSON types from key=value or on `ask answers` exit code | |
| exit 75, answer/decline/cancel, re-answer refused | works | server received accept/decline/cancel; re-answer rc 1 | |

## Changes to `in-name-only.md` entries

- "Elicitation answering UI … does not show which server asks": **fixed for the CLI** — `elicit show`/`list`
  show `from el` / SERVER `el` for upstream questions. Schema checking of answers is still absent.

## Side findings (outside this area, not traced further)

- Two daemons on one state dir overwrite `daemon.json`; `stop --all` then reported none running while one lived.
- `DELETE /v1/settings/daemon.endpoint?persist=user` → `"removed":false`, key still in `config.json`.

## Skipped

Live opencode instance and TUI picker at runtime; rung-6 ambiguity; `stopOthers`; opencode v2 sections.
Custom `--launcher` templates, `--allow-repeat`, `--no-capture-console`, artifact TTL/quota/GC/`put`/`delete`,
`--exec-output stream` remote, `--session` reuse, non-local `daemon.endpoint`, tasks via exec.
`log --follow`, log rotation, per-server logging, `daemon.leaseTTL`, `idleExit`, `protocol: follow` lanes,
exclusive × non-global scopes. URL-mode elicitation, `elicit watch`/`result`, `adapter serve`, `mcpx serve`
with API tools (read only), Swagger 2 body/formData (read only), YAML specs, `--methods` filters,
`prompt.sample=ask` with a real model, `repair.autonomy`. `go test` was not run.

## Most consequential five

1. **Permission profiles do not contain a script that can reach the daemon** — `/v1/exec` accepts
   `permissions:"all"` from a `strict`/`readnet` script, and `artifact({path})` reads denied files.
2. **Failed tool calls are logged `ok=true`** (`pool.go:959`), so `stats calls`, `stats errors` and every
   error rate built on the log report zero failures.
3. **`exec.timeout` is not applied** — the documented 120s default does nothing locally or with `--remote`.
4. **Adapter `timeout` does not stop the program**, and adapter/API arguments are not type- or
   required-checked (positional option injection, cookie params sent as query, missing body sent).
5. **opencode plugin: `instructions`, `remember:"permanent"` and `until-gone` do nothing on v1**, and no test
   loads `mcpx-session.ts`, which is how they went unnoticed.
