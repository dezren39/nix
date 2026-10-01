# Declared but never read

A knob that does nothing is worse than one that does not exist. A missing flag
fails loudly at the prompt; a flag that parses, appears in `--help`, appears in
`mcpx config --schema` and then changes nothing costs somebody an afternoon
before they conclude the feature is broken rather than the wiring.

This has shipped repeatedly. `--typecheck` on exec (#66). Five exec flags at
once (#75). MCP `sampling`, declared and unreachable (#39). The opencode
plugin's `permission.ask` hook. Every one of them was found by a person trying
the thing, which is the expensive way.

This is the audit that went looking for the rest of them, and the guard that
makes the next one a test failure.

## What the audit was

The settings registry had 149 entries when this audit ran; it has 164 today,
and the counts below are the ones the audit measured, not current figures. Each one derives a config key, an
`MCPX_` variable and a flag from a single declaration, which is the whole
argument for the registry existing. The question is therefore not "is this path
string mentioned anywhere" but "does anything read the **resolved value**".

Those are not the same question, and the difference is where the interesting
half of the findings came from. `pool.max` was read — from a config file,
through `config.Config.Pool`, which the JSON decoder fills. `MCPX_POOL_MAX` and
`--pool-max` landed in the resolved `Set` and nothing on that path ever looked
at it. Grepping for `"pool.max"` finds a match in `internal/defaults` (an error
label) and calls it live. A user who set the variable, saw nothing change,
moved it into their config file and saw it work would reasonably conclude the
variable name was wrong rather than that the layer was.

So the audit looked for one specific shape: the path handed to an accessor on a
resolved `Set` — `set.Int("pool.max")`, `a.Settings().Bool(...)`,
`cs.Duration(...)`. That is the only reader in the program that has seen the
file, the variable and the flag together. `os.Getenv` has seen one layer. A
config struct field has seen one. A hand-written flag variable has seen one.

It found **54**, in a registry of 149.

## Verdicts

Counts: **24 wired**, **3 deleted**, **9 legitimately external** (the plugin — seven are named below, and two more
were reclassified while the audit ran; there are twelve plugin-scoped settings
today),
**1 left open**, **2 added** (the inverse bug), plus 15 settings the naive
path-string grep called dead that the accessor scan cleared.

### Wired

| setting | layer | verdict | evidence | fix |
| --- | --- | --- | --- | --- |
| `pool.max` `pool.min` `pool.sharing` `pool.scope` `pool.idleTimeout` `pool.callTimeout` `pool.startTimeout` | settings | partially dead — file only | `config.Config.Pool` filled by the decoder (`config.go:174`); `Resolve` picks from it (`config.go:448-476`); no accessor anywhere | `config.ApplyPoolSettings` folds the resolved value into `Config.Pool` — the layer beneath a server's own `mcpx` block, so a per-server choice still wins. Called at the five places a config is loaded next to a resolved set. |
| `logging.maxBytes` `logging.maxLines` `logging.maxAge` `logging.keep` | settings | dead | every `NewFileSink` call passed `FileOptions{Dir: dir}` and nothing else (`cli/root.go`, `cli/commands.go`, `cli/logs.go`); `file.go:87-96` fell back to the built-in constants | `cli.fileOptions` builds the whole policy from the set; both durable sinks use it |
| `logging.file` | settings | dead | nothing decided whether to open a sink at all | guards sink creation in the daemon and in `run`/`exec` |
| `logging.include` | settings | partially dead — flag and file only | `os.Getenv("MCPX_INCLUDE")`, a name the registry does not own (`root.go:157`) | reads `set.List("logging.include")` |
| `script.before` `script.prefix` `script.onSuccess` `script.onError` `script.suffix` | settings | partially dead — file and short flag only | `cfg.ScriptPhase(...)` plus a hand-written repeatable flag; `--script-prefix` and `MCPX_SCRIPT_PREFIX` were bound, recorded and never read | `App.phaseValues` adds `Set.ListAboveFile(path)`. Only the layers above the file: `cfg.ScriptPhase` folds the file layers itself, with an inheritance marker the registry does not model, and taking both would run a line twice. |
| `script.env` | settings | dead twice over | `--env` is a hand-written alias so `Bind` skipped it; and `envPairs` was assembled, preflight-checked and then **ignored** in favour of `envVars.Values()` at the point the environment was actually built | the loop reads `envPairs` |
| `script.launcher` | settings | dead | only `*launcherFlag` was read | falls back to the resolved value; the `Bare` form resolves to `none`, so `--script-launcher` and `--no-launcher` agree |
| `paths.state` `paths.cache` | settings | partially dead — env alias only | `daemon.ResolvePaths` reads `MCPX_STATE_DIR`/`MCPX_CACHE_DIR` by name (`paths.go:37-42`) | `daemon.PathsAt`, applied in `parseFlags` once all three layers are known. The by-name reads stay, allowlisted: they run before any config file has been found. |
| `output.json` | settings | partially dead — global flag only | `--json` consumed in `main` before the subcommand; `mcpx ls --json` set `output.json` and left `App.JSON` false | `App.adoptSettings` in `parseFlags` |
| `catalog.instructions` | settings | dead | only `--no-instructions` on `types` was read | ANDed with the setting; `Commands` narrowed to `types`, the only output that carries instructions |
| `proto.askTimeout` `proto.askPoll` `proto.askRounds` `proto.stateTTL` `proto.sessionIdle` | settings | dead | `internal/mcpserver` read `defaults.Proto*` at the point of use (`ask.go:155,165,196`, `state.go:59`, `server.go:1253`) | `mcpserver.Timing`, passed in from `cli/serve.go`. Passed rather than read there, because the protocol package should not know a config file exists. |
| `proto.askTTL` | settings | dead | `routes_proto.go:260` | `s.set.Duration("proto.askTTL")` |
| `plumbing.sourceDirAllowed` `plumbing.sourceDirRecursive` `plumbing.sourceProbePaths` | settings | dead — and `source.Options`' own doc comment claimed otherwise | both call sites wrote the three booleans as literals, and **disagreed**: a launcher refused a directory while a phase accepted one | `cli.plumbingSourceOptions` |
| `plumbing.strictUnknownKeys` | settings | dead | `Set.Unknown()` was reported by `doctor` as a warning and nothing else | refuses in `App.Settings()`, after the files that carry the switch have been read |
| `plumbing.validatePaths` | settings | dead | the one preflight path check ran unconditionally, on `os.Getenv("MCPX_LOGGING_DIR")` | guarded by the switch, and reads the resolved `logging.dir` |
| `plumbing.indexOnQuery` | settings | dead | four unconditional `st.Ingest()` calls | guarded in `cli.openStore` and `daemon.openStore`. `log --follow` still ingests: its whole purpose is to see what was just written. |
| `plumbing.launcherPlaceholderRepeat` | settings | dead | `opts.AllowRepeat` came only from `--allow-repeat` | appended from the setting |
| `elicit.pendingLimit` | settings | dead | `elicit.go:415` used `defaults.ElicitPending` | passed in as `Filter.Limit`; the broker is a store and should not read policy |
| `registry.timeout` `registry.pageSize` | settings | dead | `registry.go:43,129` used the constants | `registry.Options`, passed by both callers |
| `tasks.ttl` | settings | dead | `tasks.DefaultTTL()` from the constant | the daemon supplies the configured value when a caller names none |
| `plugin.discoveryRetry` | plugin | dead | `TUNING.missCooldownMs: 60_000`, hardcoded | `goDurationMs(env.MCPX_PLUGIN_DISCOVERY_RETRY)` — the plugin parses Go's duration spelling, because that is the syntax every `MCPX_` duration is written in |
| `completion.maxValues` | settings | half-read | `/v1/complete` read it (`ops.go:350`); the MCP handler wrote `100` (`server.go:1558`) | `Server.MaxCompletions` |

### Deleted

| setting | why |
| --- | --- |
| `plugin.skills` | `MCPX_PLUGIN_SKILLS` appears in no TypeScript and no Go. Skills are directories a user copies into `~/.config/opencode/skills/` by hand; nothing registers them from a list, and there is no code for the setting to be wired into. |
| `plumbing.consoleReleaseOnExit` | "hand the original console back before the process ends". The console wrapper lives in the script's runtime, which is a child process that is about to die. Restoring it changes nothing anybody can observe. `releaseConsole()` remains a global a script can call mid-run, which is the case that was real. |
| `Setting.AllowDir` (a schema field, not a setting) | declared on six `KindSource` settings, always `false`, read by nothing. It also contradicted `plumbing.sourceDirAllowed`, which is the switch that actually decides this. |

### Legitimately external

`plugin.bin`, `plugin.binArgs`, `plugin.backend`, `plugin.toolTiming`,
`plugin.env`, `plugin.instructions`, `plugin.tools` are read by the TypeScript
plugin through `process.env`, by the exact name the registry derives
(`mcpx-session.ts:180-191`, `mcpx/daemon.ts:848-850`). Verified, and now
guarded: a plugin-scoped setting whose variable appears nowhere in
`plugin/**/*.ts` fails the test. README mentions do not count — a README can
describe a knob nobody wired.

### Left open

`paths.config` is circular by construction (#5): the config search path is what
produces the settings, so the settings cannot decide it. It is declared so that
`mcpx config --schema` names the thing, and `config.SearchPath` reads
`MCPX_PATHS_CONFIG` directly. It is the only entry on the allowlist.

### The inverse bug

The plugin honoured four variables the registry had never heard of, which
breaks the registry's promise from the other side: `mcpx settings` was supposed
to answer "what will the plugin do" without anybody reading TypeScript.
`plugin.remember` and `plugin.annotate` are now declared.
`MCPX_PLUGIN_HEADLESS` and `MCPX_PLUGIN_DAEMON_TOOLS` were allowlisted at the
time, because their defaults are computed at boot — whether the plugin is on
the main thread, whether discovery was ambiguous — so a declared *value* would
be right in one of the two realms the plugin runs in and wrong in the other.
They are declared now, as `plugin.headless` and `plugin.daemonTools`
(`internal/settings/plugin.go:105`, `:114`), with the default `auto`: `auto` is
the declared name for "work it out from the process shape", which is the thing
a number could not express.

## The guard

`internal/settings/consumed_test.go`, three tests.

**`TestEverySettingIsReadSomewhere`** scans every non-test `.go` file outside
`internal/settings` for each path used as the argument to a `Set` accessor. A
plugin-scoped setting is checked differently: its `EnvName()` must appear in
the plugin's own TypeScript sources.

The obvious alternative was a `ReadBy: "daemon"` field on `Setting`. It was
rejected, and the reason is worth stating because it generalises: that note
lives in the same struct literal as the declaration. Whoever writes the
declaration writes the note in the same keystroke, believing both, and nothing
ever checks the second half. It would have been a second declaration, subject
to exactly the bug it was meant to catch. A scan asks the code, which cannot be
optimistic.

The narrowness is the other half of the design. A scan for the path string
anywhere would have passed `pool.max`, `script.prefix`, `logging.include` and
every other half-wired setting in the table above — the ones that were hardest
to find and most confusing to hit. Requiring the accessor is requiring the
whole promise, because the `Set` is the only reader that has all three layers.

**`TestNoHandRolledSettingEnv`** closes the other direction: `os.Getenv` of a
variable the registry owns is how a setting becomes env-only. Three entries on
its allowlist, each because the read genuinely happens before a `Set` exists.
(Since replaced by `TestNoUndeclaredEnvRead`, which walks the syntax tree and
also refuses a variable *nothing* declares -- see `docs/environment.md`.)

**`TestThePluginReadsNoUndeclaredSetting`** closes the third: a variable the
plugin honours that no setting declares.

Every allowlist entry carries its reason in the map. There is deliberately no
way to silence any of the three in bulk.

## The other three layers

The same shape exists wherever something is declared in one file and consumed
in another.

### /v1 parameters

161 declared across 60 operations at the time of the audit; 162 across 60
today. The parity tests in `internal/api` already
bind every route to an op and every op to an MCP tool — but they match *paths*,
not *parameters*, so a parameter can be declared, published in `InputSchema()`,
rendered into the OpenAPI document, marshalled into the body by a generated MCP
tool, decoded by the handler and discarded, with every test green.

- **`POST /v1/diagnose`, `session`** — decoded into the request struct and never
  referenced. Its own description said "for the record", and no record was
  written. Fixed: a `diagnose.run` line now goes to the durable log, with the
  session on it, which is what makes it joinable to the run it was diagnosing.
- **`GET /v1/log?chain=`** — `Chain(id, limitPerLevel)` takes no filters, so
  `level`, `event`, `server`, `tool`, `session`, `trace`, `grep`, `since` and
  `until` were all accepted and dropped. The result is the whole trace tree at
  every level, which reads as "that trace touched everything" rather than as a
  filter that did not run. Fixed: the combination is refused, in `/v1` and in
  `mcpx log`. The e2e test for chains was itself passing `--level debug`.
- `GET /v1/stats?top=` is inert for four of seven dimensions and
  `GET /v1/events?since=` loses to `Last-Event-ID`; both say so in their own
  descriptions. Not defects.

### CLI flags

No flag declared with `fs.String`/`fs.Bool` in `internal/cli` is wholly unread.
The defects are of two kinds.

The first is structural and was fixed: `Schema.Bind` registers every setting
whose `Commands` list contains the command **or is empty**, so a command
accepts flags it never reads. `--budget` and `--bias` were offered on `types`,
`ls` and `search`; `--catalog-instructions` on `ls` and `catalog`. Narrowed.

The second is a class this audit did not resolve and is reported rather than
fixed, because each case is a judgement about the command rather than a wiring
error:

- `logging.*` declares no `Commands`, so `--log-level`, `--logging-format`,
  `--include`, `--keep` and the rotation flags bind on **every** command. Only
  `daemon` and `run`/`exec` read them. `mcpx log --log-level debug` parses and
  does nothing. Narrowing `logging.*` would be correct and is a larger change
  than this one.
- `mcpx ls -v --json` silently drops the per-instance detail `-v` asks for; the
  JSON branch returns before it is used. `mcpx doctor -v --json` likewise.
- `mcpx stats errors --by slowest` reports errors: a positional dimension wins
  over `--by` through `firstSet`, silently.
- `mcpx stats --opencode` returns before `--server`, `--tool`, `--session` and
  `--log-dir` are used.
- `mcpx log --format json --fields ts,msg` ignores `--format`: `--fields`
  bypasses the writer entirely.
- `mcpx log --reverse --follow` reverses the backlog and not the tail;
  `followLog` resets `q.Reverse` unconditionally.
- `mcpx prompt --mode script --run` becomes `run`; `mcpx recipes run x --mode
  run --script` becomes script mode. Both overwrite the flag they were given.
- `mcpx servers add x --header K=V -- cmd` accepts the headers and drops them:
  they are read only inside the `--url` branch.
- `mcpx api tools --methods all` does nothing without `--spec`: six flags are
  read only inside `if *specFlag != ""`.
- `mcpx elicit show --session S` accepts `--session` and `--audience` on every
  subcommand and reads them in two.
- `parseFlags` leaks into the package-level `flagSetCommand` map when
  `fs.Parse` fails, and the map is unguarded while `lazyMCP` builds on a
  request goroutine.

### MCP capabilities

- **`logging`** — declared to every pre-2026-07-28 client. The capability means
  "this server sends log messages to the client"; mcpx emits no
  `notifications/message` anywhere in the package. Withdrawn. The
  `logging/setLevel` method is still answered, because refusing would make a
  well-behaved client that asked anyway treat the whole connection as degraded.
- **`resources.subscribe`, `*.listChanged`** — gated on `s.Notify != nil`, a
  property of the *server*. Delivery is a property of the *connection*, and
  every HTTP connection the daemon serves is built with a nil send function. So
  every client over the transport the daemon actually mounts was told to
  subscribe, had its `subscriptions/listen` acknowledged, and then heard
  nothing for ever. Now gated on `Conn.canPush()`.
- **`*.listChanged`** additionally requires a stream the client opened, and only
  2026-07-28 has `subscriptions/listen`. An older client was told it would
  receive notifications it has no mechanism to receive. Now gated on the
  revision too.
- **`subscriptionId` in `_meta`** — 2026-07-28 makes it mandatory on every
  notification delivered on a listen stream (`2026-07-28.ts:121-132`). mcpx sets
  it on the acknowledgement only. A client with two subscriptions cannot
  correlate anything. **Since fixed**: every notification on a listen stream
  carries it (`internal/mcpserver/listen.go:94`), pinned by
  `internal/e2e/subscribe_test.go` and `internal/e2e/protomsg_test.go`.
- **`prompts` and `resources`** are declared unconditionally; both schemas say
  "present if the server offers any". The handlers return `[]` correctly, so
  this is over-declaration rather than a lie. **Not fixed** — reported.
- `events.MCPNotification` translates `ElicitCompleted` to
  `notifications/elicitation/complete`, and nothing can reach that branch:
  `ListenFilter` has no field for it, correctly, since 2026-07-28 dropped it.

## One thing the audit found that it did not fix

`Setting.EnvName()` derived `MCPX_PROTO_ASK_T_T_L` from `proto.askTTL`, and
`--proto-ask-t-t-l` with it. The camel-case splitter did not know that `TTL`
is one word. `proto.stateTTL`, `proto.askTTL`, `daemon.leaseTTL` and
`proto.serveMCP` were affected.

Fixed since: a run of capitals is now one word (`MCPX_PROTO_ASK_TTL`,
`--proto-serve-mcp`), and `TestNoDeclaredNameHasASingleLetterWord` fails if a
derived flag or variable contains a one-letter word, which is what the next
acronym split into letters would look like.
