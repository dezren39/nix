# The script environment

Settings are what a person tells mcpx. This page is the other direction: what
mcpx, and the opencode plugin, tell a program they start.

A script reads its session from here. The generated client finds the daemon
from here -- the endpoint, the socket, whether the daemon can see this
filesystem. An `mcpx` command run inside a script, or inside a shell the
editor opened, learns which session it belongs to from here, which is what
lets a session-scoped server give each agent its own browser instead of
sharing one. It is an interface with as many consumers as the settings have.

Until #172 it was declared nowhere. The only list was the code that happened
to set each name, spread across five packages and a TypeScript file, and the
documentation had already drifted from it: it said the plugin sets
`MCPX_SESSION` and `MCPX_CALL`. The plugin sets `MCPX_SESSION_ID` and
`MCPX_CALL_ID`; `MCPX_SESSION` is a different variable, set by mcpx for its
own scripts; and `MCPX_CALL` has never existed. Two variables, one renamed in
somebody's head, is exactly the drift a hand-written list produces.

## How it is kept true

The table below is generated from `settings.ScriptEnv()` in
`internal/settings/scriptenv.go`, and tests hold that table to the code:

- **Everything set is declared, by the right setter.** The Go sources are
  walked as syntax -- a map key, an assignment to an index, a `KEY=VALUE`
  string, `os.Setenv` -- and the plugin's `put(e, "MCPX_...")` calls are
  read, and every name found must be in the table with the part of mcpx that
  set it listed. A setter the table names that sets nothing fails too.
- **Everything read is declared.** A lookup of an `MCPX_` variable anywhere
  outside `internal/settings` must be either a setting read through the
  resolved settings, a variable in this table, or one of four lookups on an
  allowlist that carries the reason for each -- three happen before any
  settings exist, the fourth where there is no App to ask. An entry nothing
  needs fails the test. A variable the registry owns may not be read by name
  even when it is also here, because that would ignore the config file and
  the flag.
- **The "read by" column is checked.** If it says the generated client reads
  a variable, the generated TypeScript does; if the plugin reads one, the
  column says so.
- **This page is current.** The test regenerates the table and fails when it
  differs.

## This table is what mcpx adds, not all a child gets

Every one of these is *added to* the environment mcpx itself was started with.
A child gets the parent's environment entire, and then these on top:

- `internal/runner/runner.go:359` — `cmd.Env = append(os.Environ(), …)`, every
  script the runner starts
- `internal/adapter/adapter.go:239` — `cmd.Env = os.Environ()`
- `internal/mcpclient/stdio.go:66` — `env = append(env, os.Environ()...)`,
  under `InheritEnv`, for every stdio MCP server the pool starts

So a script also sees `SSH_AUTH_SOCK`, `GITHUB_TOKEN`, `AWS_*`, and whatever
else was in the shell that started the daemon. `PATH` is inherited rather than
constructed, which means a daemon started from a shell with an unusual `PATH`
passes it on.

`script.permissions` does not change this. It sandboxes the filesystem and the
network through the runtime's own flags; there is no allowlist or denylist for
the environment, and nothing here is filtered.

This is worth knowing before putting a secret in the environment of a shell
you then run `mcpx serve` from. It is stated rather than fixed because the
alternative — an allowlist — would break every server that expects its own API
key to arrive this way, which is most of them.

## Configuration is not here

A variable here is a fact about one run -- which session, which daemon, which
file -- handed from a parent to a child. A knob is a setting, with a
configuration key, a flag, a `/v1/settings` row and an MCP tool; those are in
[configuration.md](configuration.md). Two names are both: `MCPX_LOG_LEVEL` and
`MCPX_DAEMON_ENDPOINT` are written for a child, and a child that is `mcpx`
reads each as the setting it names.

Some variables are read by mcpx and are neither: which configuration file to
load (`MCPX_CONFIG`, the setting `paths.configFile`) and where the state lives
(`MCPX_STATE_DIR`, `MCPX_CACHE_DIR`) are needed before there is a
configuration to resolve settings from. They are settings, marked as such,
and read by name for that reason.

## The variables

<!-- BEGIN GENERATED: settings.ScriptEnv(); `go test ./internal/settings -run TestTheEnvironmentDocIsCurrent -update` -->

| variable | set by | when | what it is | read by |
| --- | --- | --- | --- | --- |
| `MCPX_SESSION` | `mcpx exec` / `mcpx run`; the exec service (`/v1/exec`, `mcpx_exec`); an event consumer | always | the session key this run's tool calls are leased under: --session, else MCPX_SESSION_ID, else one minted for the run | the generated client, which sends it with every call |
| `MCPX_SESSION_ID` | `mcpx exec` / `mcpx run`; the exec service (`/v1/exec`, `mcpx_exec`); an event consumer; the opencode plugin | the CLI passes through the value it was given, which may be empty; the plugin sets it at every plugin.env level | the session the host assigned. Unlike MCPX_SESSION it survives from one command to the next, so it is what session-scoped servers key on | `mcpx` run inside the script or shell, as its session when --session is not given |
| `MCPX_PARENT_SESSION_ID` | `mcpx exec` / `mcpx run`; the opencode plugin | the CLI passes through what it was given; the plugin at plugin.env=full, when there is a parent | the session that spawned this one, so a parent agent and its subagents can share a server | `mcpx` run inside the script or shell, and the generated client for parent-session scoping |
| `MCPX_EPHEMERAL` | `mcpx exec` / `mcpx run` | always | 1 when the session key was minted for this run alone, so its instances may be stopped as soon as it ends | the generated client |
| `MCPX_RUN` | `mcpx exec` / `mcpx run`; the exec service (`/v1/exec`, `mcpx_exec`) | always | this execution's run id, which groups the artifacts it produces | the generated client, when storing an artifact |
| `MCPX_INPUT` | `mcpx exec` / `mcpx run` | mcpx exec and mcpx run, locally and with --remote; not when mcpx serve collects the output | report: nothing in this run can answer a server's question mid-call, so a call that asks one exits 75 with the question instead of waiting for it to expire | the generated client, sent as X-Mcpx-Input |
| `MCPX_ENDPOINT` | `mcpx exec` / `mcpx run`; the exec service (`/v1/exec`, `mcpx_exec`); an event consumer | always | the daemon's HTTP endpoint | the generated client; the plugin as a fallback for MCPX_DAEMON_ENDPOINT |
| `MCPX_SOCKET` | `mcpx exec` / `mcpx run`; the exec service (`/v1/exec`, `mcpx_exec`) | empty when the daemon is remote, since its socket is not reachable from here | the daemon's unix socket, which is faster than the endpoint. Also where a daemon binds and a client dials, for anyone who sets it by hand | the generated client, `mcpx` run inside the script or shell, the plugin |
| `MCPX_ARTIFACTS_LOCAL` | `mcpx exec` / `mcpx run`; the exec service (`/v1/exec`, `mcpx_exec`) | always | 1 when the daemon shares this filesystem, so artifact({path}) may hand over a path instead of the bytes | the generated client |
| `MCPX_CWD` | `mcpx exec` / `mcpx run`; the exec service (`/v1/exec`, `mcpx_exec`) | the exec service only when the caller sent its working directory | the directory the caller ran mcpx from, which a remote run cannot discover | the generated client (paths.cwd, and cwd scoping) |
| `MCPX_PID` | `mcpx exec` / `mcpx run`; the exec service (`/v1/exec`, `mcpx_exec`) | always | the pid of the mcpx process that started the script | the generated client, for pid scoping |
| `MCPX_ENTRY` | the runner, for every script | always | the file the runtime was started on: mcpx's generated launcher, or the script itself under --no-launcher. For exec the script is a generated file | the generated client (paths.entry) |
| `MCPX_CLIENT` | the runner, for every script | always | the generated client module | the generated client (paths.client) |
| `MCPX_CONFIG_PATH` | `mcpx exec` / `mcpx run` | always | the nearest configuration file in effect, empty when there is none | the generated client (paths.config) |
| `MCPX_SCRIPT_DIRS` | `mcpx exec` / `mcpx run` | always | the directories searched for named scripts, nearest first, colon-separated | the generated client (paths.scriptDirs) |
| `MCPX_RUNTIME` | the runner, for every script | always | deno, bun or node | the generated client |
| `MCPX_ALLOW_READ` | the runner, for every script | only when the permissions allow reading files | 1: the client's file helpers may read. Without it they refuse, naming the permission, rather than leaving it to the sandbox | the generated client |
| `MCPX_LOG_LEVEL` | `mcpx exec` / `mcpx run` | always | the lowest level the script's log keeps | the generated client; `mcpx` run inside the script or shell reads it as logging.level; also the setting `logging.level` |
| `MCPX_LOG_SOURCE` | `mcpx exec` / `mcpx run` | always | which levels record a call site: a level name for that level and above, 0 for none | the generated client |
| `MCPX_DAEMON_ENDPOINT` | the opencode plugin | plugin.env=standard or above, once the plugin has chosen a daemon | the daemon this editor session settled on, so a shell command that runs mcpx talks to the same one instead of looking for its own | `mcpx` run inside the script or shell, as daemon.endpoint; the plugin, as the daemon to use; also the setting `daemon.endpoint` |
| `MCPX_TRACE_IDS` | the opencode plugin | plugin.env=standard or above, when there is anything to say | JSON pairs such as [["session_id","abc"],["worktree","/p"]], extended at full with the parent session and the whole ancestry | `mcpx` run inside the script or shell, which puts every pair on every log record |
| `MCPX_CALL_ID` | the opencode plugin | plugin.env=standard or above | the editor's id for this one tool call | the command |
| `MCPX_OPENCODE_CWD` | the opencode plugin | plugin.env=standard or above | the directory the command runs in, which is not always the project root | the command |
| `MCPX_OPENCODE_DIRECTORY` | the opencode plugin | plugin.env=standard or above | the project directory the editor opened | the command |
| `MCPX_OPENCODE_WORKTREE` | the opencode plugin | plugin.env=standard or above | the project's worktree | the command |
| `MCPX_WORKTREE_NAME` | the opencode plugin | plugin.env=standard or above, when there is a worktree | the worktree's last path element | the command |
| `MCPX_PROJECT_ID` | the opencode plugin | plugin.env=standard or above | the editor's project id | the command |
| `MCPX_HARNESS` | the opencode plugin | plugin.env=standard or above | opencode | the command |
| `MCPX_HARNESS_VERSION` | the opencode plugin | plugin.env=standard or above | the editor's version | the command |
| `MCPX_HARNESS_PID` | the opencode plugin | plugin.env=standard or above | the editor's pid | the command |
| `MCPX_HARNESS_STARTED` | the opencode plugin | plugin.env=standard or above | when the plugin started | the command |
| `MCPX_SHELL_SEQ` | the opencode plugin | plugin.env=standard or above | a counter of shell commands in this editor process | the command |
| `MCPX_SESSION_TITLE` | the opencode plugin | plugin.env=full | the session's title | the command |
| `MCPX_SESSION_DIRECTORY` | the opencode plugin | plugin.env=full | the session's directory | the command |
| `MCPX_SESSION_VERSION` | the opencode plugin | plugin.env=full | the editor version that created the session | the command |
| `MCPX_SESSION_DEPTH` | the opencode plugin | plugin.env=full | how many parents the session has | the command |
| `MCPX_SESSION_CREATED` | the opencode plugin | plugin.env=full, when known | when the session was created, ISO 8601 | the command |
| `MCPX_SESSION_AGE_MS` | the opencode plugin | plugin.env=full, when known | the session's age when the command started, in milliseconds | the command |

<!-- END GENERATED -->
