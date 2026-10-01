# Audit: mcpx's own surface

```
created:      2026-10-01
base:         origin/main 19237a0
binary:       go build -o /tmp/mcpx-surface ./cmd/mcpx
fixtures:     internal/testsupport/fakemcp, internal/testsupport/askmcp
scratch:      HOME, MCPX_STATE_DIR, MCPX_CACHE_DIR under a private dir
status:       audit only -- nothing fixed
```

Scope: commands (`mcpx --help`, `mcpx help <cmd>`), settings
(`internal/settings`), `MCPX_*` variables (`docs/environment.md`), flags, exit
codes, and `/v1` routes against the CLI. Entries already in
`docs/in-name-only.md` are not repeated; one is re-confirmed below.

## How the settings were sampled

The registry holds **173** settings, not ~150:
`rg -o 'Path: +"[^"]+"' internal/settings -g '!*_test.go' | sort -u` = 173.
`mcpx settings --json` shows 129; the other 44 are `plumbing`-flagged and appear
only in `<cmd> --help` and `config --schema`.

1. **Static, all 173:** for every path, which non-test files name it
   (`rg -F '"<path>"'`). Every non-plugin path has at least one accessor read
   except `paths.config` (only a `ReadBy` string in `settings/scriptenv.go:177`;
   config discovery is done elsewhere -- not traced, so not classified). The
   twelve `plugin.*` settings are read by the TypeScript plugin
   (`plugin/opencode/mcpx-session.ts`) and were **skipped** -- the plugin
   cannot be run from here.
2. **Behavioural, 31 settings:** run the binary with the setting at two values
   and compare. Chosen to cover every scope (client, call, daemon), every layer
   (flag, env, file, `mcpx daemon` flag, `X-Mcpx-Settings` header), and every
   read site that is not the command the help lists it under (where the static
   scan showed the read happens in a different process from the one the help
   implies). Verified working: `output.json`, `search.limit`, `catalog.budget`,
   `catalog.bias`, `daemon.autostart`, `daemon.endpoint`, `http.requestTimeout`,
   `http.callBodyLimit`, `script.runtime`, `script.typecheck`,
   `script.permissions` (`net` denies reads; bun/node refuse unenforceable
   profiles), `logging.level` and `logging.format` on exec, `logging.file`,
   `daemon.watchConfig`, `events.history`, `mcp.pageSize`,
   `registry.url`, `plumbing.strictUnknownKeys`, `pool.callTimeout` (env and
   `mcpx daemon` flag), `artifacts.enabled` (env and file),
   `logstore.queryLimit` and `stats.top` (over HTTP), `tasks.ttl` (exec
   tasks). Found broken or partial: below.
3. Not verified: `catalog.instructions` (fakemcp sends no instructions, so
   both values print the same), `autostart.idleExit` (the daemon also waits
   for no live instances, `internal/daemon/server.go:376`; not isolated), every
   `proto.*`, `protoMessages.*`, `upstream.*`, `transport.*`, `prompt.*`,
   `recipes.*` setting.

## Settings

| what | state | evidence | issue |
| --- | --- | --- | --- |
| `exec.timeout` (default "120s", "kill a script after this long") on `mcpx exec` | absent | Local exec never reads it: `cli/commands.go:478` has its own `--timeout` (default 0 = no limit) and only that reaches the runner. `mcpx exec --exec-timeout 1s 'await fake.slow({ms:3000}); console.log("done")'` printed `done` after 3.1s, rc 0; `MCPX_EXEC_TIMEOUT=1s` same; `--exec-where remote --exec-timeout 1s` same. The advertised 120s default does not apply locally either. `--timeout 1s` does kill (rc 124). | #77 (call-scoped timeout, related) |
| Call-scoped settings on `POST /v1/exec` (`exec.timeout`, `artifacts.delivery`, `artifacts.inlineMaxBytes`, `artifacts.interceptImages` -- all `scope: call, hot: true`) | absent | `ExecService` reads `s.execSettings()` (`internal/daemon/routes_exec.go:170-185`), a set built **once** (`sync.Once`, `:53-77`) from the config file and environment only -- never from the `X-Mcpx-Settings` header that `docs/configuration.md` "Scope" says every call-scoped setting is honoured from, and never re-read, so "hot" is false. `curl --unix-socket … -H 'X-Mcpx-Settings: {"exec.timeout":"1s"}' -d '{"source":"await new Promise(r=>setTimeout(r,3000))…"}' /v1/exec` → `exitCode 0, durationMs 3046`; the same with `"options":{"timeout":"1s"}` → `exitCode 124, timedOut`. | -- |
| Exec-service settings given as `mcpx daemon --<flag>` | partial | Same cause: `execSettings` applies file and env, not the daemon's flags. `mcpx daemon --artifacts-enabled=false` then `artifact("a.txt","hi")` in exec → stored (`mcpx://artifacts/art-2608…`); `MCPX_ARTIFACTS_ENABLED=false` or `{"artifacts":{"enabled":false}}` → `artifacts are disabled`. Also affects `script.runtime`, `script.permissions`, `script.runtimes`, `script.captureConsole`, `script.typecheck`, `artifacts.*` on the remote path (read at `routes_exec.go:173-184`). | -- |
| Daemon-scoped settings given on a client command (`call --pool-call-timeout 1s`, any of the ~40 daemon settings `help call` lists) | absent | Accepted, validated, no warning, no effect -- including when that very command auto-starts the daemon (`autostart` passes `daemon --detached` plus `--idle-exit`/`--config` only, `cli/client.go:339-343`). `mcpx stop --all; mcpx call --pool-call-timeout 1s fake.slow '{"ms":3000}'` → `slept 3000ms`, rc 0. `mcpx daemon --pool-call-timeout 1s` then the same call → `pool.callTimeout exceeded (1s)`, rc 1. `docs/configuration.md` "Scope" describes exactly this ("accepted, validated, printed back, and then ignored … Nothing anywhere said so") as the failure scopes exist to prevent; nothing in `internal/cli` warns (`rg -i 'daemon.scoped\|ignored.*running daemon' internal/cli` → none). | #185 (related) |
| `logstore.queryLimit` on `mcpx log` | partial | `mcpx log` reads the store in-process with its own `--limit` default 100 (`cli/logs.go:83`); the setting is only read by `GET /v1/log` (`internal/daemon/ops.go:130`). `mcpx log --logstore-query-limit 2` → 100 lines; `MCPX_LOGSTORE_QUERY_LIMIT=2 mcpx log` → 100. Over HTTP with the header → 3 records for 3. `help log` lists the flag. | -- |
| `stats.top` on `mcpx stats` | partial | Same shape: `--top` default 20 (`cli/logs.go:442`); setting read only by `GET /v1/stats` (`internal/daemon/ops.go:200-202`). `stats --by slowest --stats-top 1` and `--stats-top 5` → 23 lines each. | -- |
| `tasks.ttl` ("how long a finished task's result is kept") | partial | Applied only to exec tasks (`routes_exec.go:233-237`). `mcpx daemon --tasks-ttl 2s`: `POST /v1/exec {"options":{"task":{}}}` → `"ttl":2000`; `POST /v1/call {…,"task":{}}` → `"ttl":600000`, and the task was still readable after 5s. | #87 (related) |
| `script.permissions none` | minimal | The profile named `none` is `--allow-all` (`defaults/defaults.json:43`), as are `off` and `unsandboxed`. `mcpx exec --script-permissions none 'Deno.readTextFileSync("/etc/hosts")'` → reads it. A reader picking "none" for no permissions gets all of them. | -- |
| `mcpx.namespace` validation (already in in-name-only) | still open | `{"namespace":"1 bad-ns"}` → `mcpx ls` lists namespace `1 bad-ns`, rc 0. | -- |

## Commands, help and flags

| what | state | evidence | issue |
| --- | --- | --- | --- |
| `mcpx help <command>` "usage, flags and settings for one command" | absent (flags) | No command's help has a flags section; it lists settings only. `help exec` does not mention `--timeout`, `--keep`, `--session`, `--export`, `--ns`, `--runtime`, `--format`, `--prefix`… (they appear only in examples prose). Counted for exec, run, call, search, catalog, types, log, stats, restart, stop, doctor, serve, daemon: 0 flag lines each. `exec --help` (Go's default usage) does list them, mixed into ~170 settings. `man` does not mention `--keep`. | #144, #147 |
| Settings listed on every command whether or not it reads them | minimal | `help init`, `help man`, `help completion`, `help health`, `help stop` each list the same 71 settings (`artifacts-quota`, `plugin-*`, `prompt-*` …). `mcpx man --artifacts-quota 5` is accepted. The list says nothing about which command acts on which. | -- |
| Global flags after the command | partial | Main help lists `--config`, `--profile`, `--skip-default`, `--all-profiles` under GLOBAL FLAGS. `--json` works after the command; the others do not: `mcpx ls --profile nope` → `flag provided but not defined: -profile`, `ls --config …` and `ls --all-profiles` likewise. | -- |
| `--profile <unknown>` | untested/absent check | `mcpx --profile nope ls` → full list, rc 0; a misspelt profile is indistinguishable from the default set. | -- |
| `mcpx resolve <dir>` with a broken config | partial | `resolve` on a dir whose `.mcpx.json` fails to parse prints `configPath:` empty, `socket: …/daemon.sock`, rc 0 -- a confident wrong answer; `mcpx ls` in that dir fails. Valid dirs resolve correctly. | #88 (related) |
| `mcpx exec --timeout` | minimal | Kills at the limit but prints nothing: rc 124, empty output. `/v1/exec` with the same limit returns `"error":"script timed out after 1s"`. | -- |
| Every other command | works (smoke) | `prompt`, `recipes`, `scripts`, `doctor`, `daemons`, `adapter`, `api`, `servers`, `schema`, `client`, `completion`, `man`, `refresh`, `restart --lazy`, `status`, `elicit list`, `config --schema`, `health`, `protocol`, `resolve`, `task list/get/result/cancel`, `artifact list/get/delete`, `globals`, `history`, `tools [--ns]`, `session release`, `ask poll/begin`, `tool invoke`, `complete` run and return plausible output or a precise usage error. `explore`/`tui`/`events` (interactive/streaming) skipped. | -- |

## Exit codes

| what | state | evidence | issue |
| --- | --- | --- | --- |
| 75 when a call stops for input (`help elicit`, `cli/elicit.go:23-28`) | partial | First question on a fresh daemon: `mcpx call ask.need_repo` → rc 75, `mcpx exec 'await ask.need_repo({})'` → rc 75. While any earlier question for the same (shared) server is still pending, later callers **block** instead: `exec` with one pending → killed by `timeout 20` (rc 124); `call` → killed at 15s; with no outer timeout `exec` waited 2:00 then printed `no repository was chosen (cancel)` and exited **0** -- the pre-#286 behaviour. `mcpx elicit list` showed each blocked call's question was created. Cause not traced; the e2e tests (`inputrequired_test.go`) start from an empty question store. | #286 (closed) |
| 2 for usage errors, 1 for failures, 124 for timeout | works | `unknown command` → 2; `unknown global flag` → 2; tool `isError` → 1; unknown tool → 1; `--timeout` → 124. Only 75 is promised in docs. | -- |

## `MCPX_*` environment

All 42 names in `docs/environment.md` resolve to a setter or reader
(`scriptenv.go` registry, plugin, or generated client). Behaviour checked by
printing the script's environment.

| what | state | evidence | issue |
| --- | --- | --- | --- |
| Variables documented as set "always" for `mcpx exec`/`run` | partial | Local exec sets all of them. `--exec-where remote` omits `MCPX_CONFIG_PATH`, `MCPX_SCRIPT_DIRS`, `MCPX_EPHEMERAL`, `MCPX_LOG_LEVEL`, `MCPX_LOG_SOURCE`, `MCPX_PARENT_SESSION_ID`, and sets `MCPX_SESSION_ID` to the minted session where the doc says it passes through what it was given (empty). The doc row for each says "always". | #106 (related) |
| `MCPX_CALL` | n/a | Doc itself says it has never existed. | -- |
| `MCPX_DAEMON_ENDPOINT` read by mcpx as `daemon.endpoint` | works | `MCPX_DAEMON_ENDPOINT=http://127.0.0.1:1 mcpx ls` → `no daemon answering; it is named explicitly`. | -- |
| `MCPX_STATE_DIR`, `MCPX_CACHE_DIR` | works | the whole audit ran under them; sockets and caches landed there. | -- |
| Plugin-set variables (`MCPX_HARNESS*`, `MCPX_OPENCODE_*`, `MCPX_SESSION_*`, `MCPX_TRACE_IDS`, `MCPX_CALL_ID`, …) | skipped | need the opencode plugin running. | -- |

## `/v1` routes against the CLI

The OpenAPI document (`mcpx openapi`) lists 71 method+path pairs; the daemon
mux registers 60 (`rg 'HandleFunc\("…' internal/daemon`). Every registered
route is in the document; the 11 extra document entries are `POST /mcp` and
`POST /v1/tools/mcpx_*`, served by the `/v1/tools/{tool}` pattern. No route
without a handler and no handler without a document entry. Every op has a CLI
word (`Op.CLIWords`, `api/ops.go:51`), and each generated command smoke-ran.

| what | state | evidence | issue |
| --- | --- | --- | --- |
| CLI command and its route read different settings | partial | `mcpx log`/`mcpx stats` read the store locally instead of calling `/v1/log`/`/v1/stats`, so the route's setting defaults (`logstore.queryLimit`, `stats.top`) and the CLI's hard-coded defaults (100, 20) differ -- see Settings. | -- |
| `/v1/exec` ignores `X-Mcpx-Settings` | absent | see Settings; the CLI's own `--exec-timeout` cannot reach it either because the CLI never maps it into `options.timeout`. | -- |
| `/v1/call` tasks ignore `tasks.ttl` | partial | see Settings. | -- |

## Most consequential five

1. **`exec.timeout` does nothing** -- not from flag, env, or header; not locally,
   not remotely. Scripts have no default limit despite the documented 120s.
2. **`/v1/exec` builds its settings once from file+env**: call-scoped "hot"
   settings in the header are ignored, `mcpx daemon --flags` are ignored, and
   runtime changes never land -- the scope model in `configuration.md` does not
   hold on the exec path.
3. **Daemon-scoped flags on client commands are silently dropped**, including
   when that command starts the daemon -- the exact failure `configuration.md`
   says the scope field exists to prevent, and `help <cmd>` advertises ~40 of them.
4. **Exit 75 holds only for the first pending question per server**; later
   callers block until expiry and `exec` then exits 0 with a cancel result
   (#286's original symptom).
5. **`mcpx help <cmd>` shows no flags**, only a 71-plus-entry settings list that
   is identical for `init`, `man` and `call`; the per-command flags exist only in
   Go's raw `--help` output.
