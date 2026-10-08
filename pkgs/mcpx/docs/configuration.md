# Configuration

Every number in mcpx is a setting, including the ones nobody will ever
change.

That sounds like an overreaction until you have hit the alternative. A
constant written next to the code that needs it is fine right up to the day
somebody's proxy kills an idle event stream at thirty seconds, or their
sandbox refuses to exec the running binary, or a tool legitimately takes a
five-megabyte argument. At that point the only remedies are a patched binary
or a fork, and both are absurd outcomes for a number that was chosen in five
seconds and never revisited. Declaring it costs one line and removes that
outcome entirely.

So the rule is: nothing is hardcoded. A value lives in
`internal/defaults/defaults.json` as data, and -- wherever a user could
conceivably want it different -- it is declared once in the settings registry,
from which its configuration key, its environment variable, its command-line
flag, its `/v1` representation and its MCP tool are all derived. There is no
second place to update, which is the point: the arrangement this replaced had
four, and they drifted.

The handful of exceptions are permission bits. `0700` on the state directory
and `0600` on the socket are not preferences. The socket grants the power to
run tools as you; a configuration key that widens it would be a footgun with
no legitimate use. They are in `defaults.json` as data so there is still one
place to read them, and they are deliberately absent from the registry.

A guard test enforces this. It matches inline durations in both spellings
gofmt produces, byte sizes written as shifts, bounds assigned to variables
whose names say they are bounds, and permission literals, with an allowlist
that carries a reason for each exemption. The version it replaced matched
fixed strings and missed every duration gofmt had compacted into a function
argument -- nine of them, including the daemon's shutdown grace and the event
stream's keepalive.

## Where a value can come from

Lowest to highest:

1. **default** -- `internal/defaults/defaults.json`, embedded in the binary.
2. **file** -- every configuration file on the search path, ranked by
   distance, nearest highest. `mcpx config --sources` lists them.
3. **env** -- `MCPX_*`. Every setting has one, derived from its path, whether
   or not anybody uses it.
4. **flag** -- the command line.
5. **runtime** -- an override set through `PUT /v1/settings/{path}` or
   `mcpx settings set`. Highest because it is the most recent statement of
   intent: somebody changed their mind while the thing was running, and having
   a flag from ten minutes ago win would be inexplicable.

`mcpx settings get <path>` prints the winning value, where it came from, and
what it displaced. "Why is this not what my config says" is the most common
configuration question there is, and the answer is always in that list.

Two spellings of one setting at the same level is an error rather than a race:
`--log-level` and `--logging-level` together, or two variables that mean the
same thing with different values. The environment has no order, so there is
genuinely nothing to prefer, and picking one silently is how a configuration
becomes unexplainable.

## Scope: who reads it

Every setting says which process acts on it. This exists because of a failure
with no symptom.

A daemon is started once, in whatever environment it happened to have, and
then serves every command for hours. A flag on one of those commands that
governs something the *daemon* does was simply lost: accepted, validated,
printed back, and then ignored, because the process that would act on it had
read its configuration an hour earlier. Nothing anywhere said so.

- **daemon** -- the pooled servers, the listener, the log the daemon writes.
  A client changes one by persisting it and restarting, or through
  `mcpx settings set` if the setting is hot.
- **client** -- the CLI process itself: how it renders, how it finds a daemon,
  whether it may start one. Setting it on the daemon does nothing.
- **call** -- meaningful per request. The CLI sends its effective value with
  every request in an `X-Mcpx-Settings` header, and the daemon honours it for
  that request only. This is what makes `mcpx search --limit 3` work against
  a daemon that was started without it. Only call-scoped settings are honoured
  from that header: widening it would make a header a way for any client to
  reconfigure a shared daemon.
- **plugin** -- read by the opencode plugin, which is neither. The plugin
  reads `process.env` directly, so the variable names are the contract between
  the two and a test pins them.

## Hot: does it take effect now

A **hot** setting is read afresh every time it is used, so changing it at
runtime works. A cold one was consumed once at startup -- a listener address,
a log file that is already open -- and recording a change to it would do
nothing. `PUT /v1/settings/{path}` says so rather than accepting a value that
would be silently inert.

## Reaching them

| surface | read | change |
| --- | --- | --- |
| config file | any `mcpx` config on the search path | edit it |
| environment | `MCPX_*` | export it |
| CLI | `mcpx settings list`, `mcpx settings get <path>` | `mcpx settings set <path> <value> [--persist runtime\|project\|user]`, `mcpx settings unset <path>` |
| `/v1` | `GET /v1/settings`, `GET /v1/settings/{path}` | `PUT /v1/settings/{path}`, `DELETE /v1/settings/{path}` |
| MCP | `mcpx_settings_list`, `mcpx_settings_get` | `mcpx_settings_set`, `mcpx_settings_unset` |

`mcpx settings list` resolves locally, which is what the *client* will use.
`--daemon` asks the daemon what it resolved, which is what a daemon-scoped
setting actually is. The two differing is information, not a bug: it is
exactly the case that used to be invisible.

`--persist runtime` changes the running daemon and nothing on disk, which is
what an experiment wants. `project` and `user` write the corresponding
configuration file *and* apply at once where the setting allows it -- a change
written to a file but not applied would leave `mcpx settings get` answering
with the old value, which reads as the write having failed.

## Servers

Servers are configuration too, and until now adding one meant an editor and a
restart.

| surface | list | add | remove |
| --- | --- | --- | --- |
| CLI | `mcpx servers list` | `mcpx servers add <name> -- <cmd>...`, `mcpx registry add <n> --write` | `mcpx servers remove <name>` |
| `/v1` | `GET /v1/servers` | `POST /v1/servers` | `DELETE /v1/servers/{name}` |
| MCP | `mcpx_servers_list` | `mcpx_servers_add` | `mcpx_servers_remove` |

The entry is written to a configuration file and the daemon reloads, so the
server is callable immediately and is still there tomorrow. A server whose
process definition did not change keeps its running child: adding one server
must not restart the rest, or a stateful server -- a browser holding a session
-- loses it because a neighbour appeared.

The daemon also re-reads its configuration files when they change on disk,
checked on the same tick that reaps idle instances (`daemon.watchConfig`). An
edit by hand therefore takes effect within `daemon.reapInterval` without
anything being restarted.

Making that work required changing what identifies a daemon. The socket used
to be keyed to the *contents* of the config files, so that editing one got a
fresh daemon rather than a stale one. A daemon that reloads changes the
contents of the files it is keyed by, which moves its own key, which makes it
unreachable by the client that just edited it -- and hands the next command a
different daemon that happens to match the new key with old servers loaded.
That was observed: `mcpx servers remove` succeeded, the file was correct, and
the next `mcpx servers list` showed the removed server. The key is now the
file *set*, and staleness is handled where it belongs.

### Hiding tools

`tools` (an allowlist) and `excludeTools` (a denylist) go in a server's `mcpx`
block, or in the top-level `pool` block to apply to every server. A hidden
tool is left out of every listing -- `mcpx tools`, generated types, search,
the MCP pass-through -- and a call to it by name is refused with 400.

```json
{
  "pool": { "excludeTools": ["delete_*"] },
  "mcpServers": {
    "datadog": { "command": "mcp-remote", "args": ["..."],
      "mcpx": { "excludeTools": ["execute_*", "/^(update|upsert)_.*dashboard/"] } }
  }
}
```

Each entry is one of:

| entry | means | example |
| --- | --- | --- |
| `/.../` | a Go regular expression, unanchored as regexps are | `/delete/`, `/^delete_/` |
| contains `*`, `?` or `[` | a glob over the whole name: `*` any run, `?` one character, `[a-z]` / `[!a-z]` a class | `delete_*`, `*_monitor` |
| anything else | the exact tool name | `delete_datadog_workflow` |

A pattern that cannot compile is a configuration error naming the field, not
a rule that silently matches nothing.

`extraIncludeTools` takes the same entries and adds them to the allowlist
instead of replacing it, so a server can widen a pool-wide `tools` by one tool
without restating the rest:

```json
{
  "pool": { "tools": ["get_*", "list_*", "search_*"] },
  "mcpServers": {
    "datadog": { "mcpx": { "extraIncludeTools": ["create_datadog_monitor"] } }
  }
}
```

With no `tools` anywhere, `extraIncludeTools` is the allowlist -- it includes,
as `tools` does, so everything it does not name is hidden.

`excludeTools` wins over both. A server's own `tools` replaces the pool's;
`extraIncludeTools` and `excludeTools` from the pool and the server all apply,
so a server that hides one more tool does not undo a pool-wide rule. A nearer
config file's `pool.extraIncludeTools` likewise adds to a farther one's. An alias (`aliasOf`) filters
independently of the server it shares a process with.

Names are the server's own tool names, as `mcpx tools` prints them in its
`tool` column, not the generated function names.

Some servers can filter on their side too -- Datadog's `omit_tools` URL
parameter, GitHub's `GITHUB_READ_ONLY` -- which keeps the tool from ever
reaching mcpx. Those are each server's own convention; this one works for any
server.

## Every setting

Everything between the markers below is generated from `settings.Registry()` by
`TestTheDocumentationListsEverySetting`, which fails when it is stale:

```
go test ./internal/settings -run TestTheDocumentationListsEverySetting -update
```

It used to say it was generated while being written by hand, and it had
drifted: two settings were missing an alias their flag really answers to.
`mcpx config --schema --plumbing` prints the same inventory, and
`mcpx settings list` prints it with the values that are actually in force.

Entries marked *(plumbing)* are internals. They work and they are supported,
but there is no ordinary reason to change one, so they are kept out of
ordinary help rather than hidden -- a flag a user can find in the source and
that `--help` denies exists is worse than a long list.

`paths.configFile` is the one setting a configuration file may not set: it
decides which file is read, so it is accepted from the environment and from
the global `--config`, which goes before the command.

`plumbing.strictUnknownKeys` governs keys **no setting claims**, and keys a
server entry declares that mcpx does not know — `type`, `alwaysAllow` and the
rest, which belong to the other hosts that read the same file. Those are
recorded, reported by `mcpx doctor`, and refused only when this is on.

A key inside a server's `"mcpx"` block is not governed by it and is always
refused. That block is mcpx's alone — nothing else writes into it — so a key
it does not know is a typo or a name that no longer exists, not another host's
field. This is deliberately not a setting: it is how `mcpx init` wrote
`"mode": "session"` into every new config for months without anyone noticing,
and a switch to turn the report off would have kept it quiet.

<!-- BEGIN GENERATED: settings.Registry(); `go test ./internal/settings -run TestTheDocumentationListsEverySetting -update` -->

### artifacts

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `artifacts.chunkBytes` | bytes | `256KiB` | daemon | no | `--artifacts-chunk-bytes` | `MCPX_ARTIFACTS_CHUNK_BYTES` | *(plumbing)* how much of a body one streamed frame carries |
| `artifacts.delivery` | enum | `reference` | call | yes | `--artifacts-delivery` | `MCPX_ARTIFACTS_DELIVERY` | how artifact bodies reach the caller |
| `artifacts.dir` | string | *(empty)* | call | yes | `--artifacts-dir` | `MCPX_ARTIFACTS_DIR` | write this run's artifacts here |
| `artifacts.enabled` | bool | `true` | daemon | no | `--artifacts-enabled` | `MCPX_ARTIFACTS_ENABLED` | whether the daemon keeps files scripts produce |
| `artifacts.gcInterval` | duration | `10m` | daemon | no | `--artifacts-gc-interval` | `MCPX_ARTIFACTS_GC_INTERVAL` | *(plumbing)* how often expired artifacts are swept |
| `artifacts.inlineMaxBytes` | bytes | `1MiB` | call | yes | `--artifacts-inline-max-bytes` | `MCPX_ARTIFACTS_INLINE_MAX_BYTES` | the largest artifact that may be base64'd into a result |
| `artifacts.interceptImages` | bool | `true` | call | yes | `--artifacts-intercept-images` | `MCPX_ARTIFACTS_INTERCEPT_IMAGES` | turn image and audio content in a result into artifact references |
| `artifacts.maxBytes` | bytes | `64MiB` | daemon | no | `--artifacts-max-bytes` | `MCPX_ARTIFACTS_MAX_BYTES` | the largest single artifact that may be stored |
| `artifacts.quota` | bytes | `1GiB` | daemon | no | `--artifacts-quota` | `MCPX_ARTIFACTS_QUOTA` | the total the artifact store may hold |
| `artifacts.ttl` | duration | `24h` | daemon | no | `--artifacts-ttl` | `MCPX_ARTIFACTS_TTL` | how long an artifact is kept before it is collected |

### autonomy

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `autonomy.max` | enum | `run` | daemon | no | `--autonomy-max` | `MCPX_AUTONOMY_MAX` | the most autonomy this daemon exercises, whatever a caller asks for |

### autostart

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `autostart.args` | list | `daemon,--detached` | client | no | `--autostart-args` | `MCPX_AUTOSTART_ARGS` | *(plumbing)* the arguments an auto-started daemon is given |
| `autostart.bin` | string | *(empty)* | client | no | `--autostart-bin` | `MCPX_AUTOSTART_BIN` | which executable is started as the daemon |
| `autostart.connectTimeout` | duration | `20s` | client | no | `--autostart-connect-timeout` | `MCPX_AUTOSTART_CONNECT_TIMEOUT` | how long to wait for a started daemon to answer |
| `autostart.idleExit` | duration | `4h0m0s` | client | no | `--autostart-idle-exit` | `MCPX_AUTOSTART_IDLE_EXIT` | how long an auto-started daemon survives with nothing to do |
| `autostart.logTail` | bytes | `2000` | client | no | `--autostart-log-tail` | `MCPX_AUTOSTART_LOG_TAIL` | *(plumbing)* how much of the daemon log is shown when it will not start |
| `autostart.pingTimeout` | duration | `2s` | client | no | `--autostart-ping-timeout` | `MCPX_AUTOSTART_PING_TIMEOUT` | *(plumbing)* how long a health check waits before calling it dead |
| `autostart.pollInterval` | duration | `50ms` | client | no | `--autostart-poll-interval` | `MCPX_AUTOSTART_POLL_INTERVAL` | *(plumbing)* how often a starting daemon is probed |

### catalog

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `catalog.bias` | list | *(empty)* | call | yes | `--catalog-bias`, `--bias` | `MCPX_CATALOG_BIAS` | words that pull matching tools toward the front |
| `catalog.budget` | int | `2000` | call | yes | `--catalog-budget`, `--budget` | `MCPX_CATALOG_BUDGET` | token ceiling for the catalog listing |
| `catalog.instructions` | bool | `true` | call | yes | `--catalog-instructions` | `MCPX_CATALOG_INSTRUCTIONS` | include each server's own instructions |
| `catalog.pinnedTools` | list | *(empty)* | daemon | no | `--catalog-pinned-tools` | `MCPX_CATALOG_PINNED_TOOLS` | tools always exposed in reactive mode |

### completion

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `completion.maxValues` | int | `100` | daemon | yes | `--completion-max-values` | `MCPX_COMPLETION_MAX_VALUES` | *(plumbing)* how many completions one reply carries |

### daemon

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `daemon.address` | string | `127.0.0.1` | daemon | no | `--daemon-address`, `--address` | `MCPX_DAEMON_ADDRESS` | which interface the daemon listens on |
| `daemon.autostart` | bool | `true` | client | no | `--daemon-autostart` | `MCPX_DAEMON_AUTOSTART` | start the daemon on demand when it is not running |
| `daemon.endpoint` | string | *(empty)* | client | no | `--daemon-endpoint` | `MCPX_DAEMON_ENDPOINT` | a daemon somewhere else, instead of the local socket |
| `daemon.idleExit` | duration | `0s` | daemon | no | `--daemon-idle-exit`, `--idle-exit` | `MCPX_DAEMON_IDLE_EXIT` | stop the daemon after this long with nothing to do |
| `daemon.inline` | bool | `false` | client | no | `--daemon-inline` | `MCPX_DAEMON_INLINE` | as a last resort, run servers inside this process |
| `daemon.inlineStartPoll` | duration | `20ms` | client | no | `--daemon-inline-start-poll` | `MCPX_DAEMON_INLINE_START_POLL` | *(plumbing)* how often a starting in-process daemon is probed |
| `daemon.inlineStartTimeout` | duration | `5s` | client | no | `--daemon-inline-start-timeout` | `MCPX_DAEMON_INLINE_START_TIMEOUT` | *(plumbing)* how long an in-process daemon has to become reachable |
| `daemon.leaseTTL` | duration | `30m0s` | daemon | yes | `--daemon-lease-ttl` | `MCPX_DAEMON_LEASE_TTL` | *(plumbing)* how long a silent caller's instances are remembered |
| `daemon.port` | int | `0` | daemon | no | `--daemon-port`, `--port` | `MCPX_DAEMON_PORT` | listen on a TCP port instead of choosing one |
| `daemon.probeTimeout` | duration | `200ms` | client | no | `--daemon-probe-timeout` | `MCPX_DAEMON_PROBE_TIMEOUT` | *(plumbing)* how long another daemon's socket is given to answer |
| `daemon.reapInterval` | duration | `30s` | daemon | no | `--daemon-reap-interval` | `MCPX_DAEMON_REAP_INTERVAL` | *(plumbing)* how often idle instances are swept |
| `daemon.refreshTimeout` | duration | `3m0s` | daemon | yes | `--daemon-refresh-timeout` | `MCPX_DAEMON_REFRESH_TIMEOUT` | how long POST /v1/refresh may take |
| `daemon.saveInterval` | duration | `5m` | daemon | no | `--daemon-save-interval` | `MCPX_DAEMON_SAVE_INTERVAL` | *(plumbing)* how often daemon state is written to disk |
| `daemon.socketProbeTimeout` | duration | `500ms` | daemon | no | `--daemon-socket-probe-timeout` | `MCPX_DAEMON_SOCKET_PROBE_TIMEOUT` | *(plumbing)* how long a socket left by a crashed daemon is given to answer |
| `daemon.warm` | bool | `true` | daemon | no | `--daemon-warm`, `--warm` | `MCPX_DAEMON_WARM` | read every server's schemas in the background at startup |
| `daemon.warmTimeout` | duration | `3m0s` | daemon | no | `--daemon-warm-timeout` | `MCPX_DAEMON_WARM_TIMEOUT` | *(plumbing)* how long the background schema fetch may take |
| `daemon.watchConfig` | bool | `true` | daemon | yes | `--daemon-watch-config` | `MCPX_DAEMON_WATCH_CONFIG` | re-read the configuration files when they change on disk |

### diagnose

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `diagnose.history` | int | `8` | daemon | no | `--diagnose-history` | `MCPX_DIAGNOSE_HISTORY` | *(plumbing)* how many changes are kept per tool |
| `diagnose.preflight` | bool | `true` | call | yes | `--diagnose-preflight` | `MCPX_DIAGNOSE_PREFLIGHT` | check a script's tool calls against the live schemas first |

### doctor

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `doctor.timeout` | duration | `20s` | client | no | `--doctor-timeout` | `MCPX_DOCTOR_TIMEOUT` | how long `mcpx doctor` gives the daemon to answer |

### elicit

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `elicit.askTimeout` | duration | `45s` | daemon | no | `--elicit-ask-timeout` | `MCPX_ELICIT_ASK_TIMEOUT` | how long mcpx waits for an answer to a question it raised itself |
| `elicit.confirmDestructive` | bool | `false` | daemon | no | `--elicit-confirm-destructive` | `MCPX_ELICIT_CONFIRM_DESTRUCTIVE` | ask before a call to a tool that may be destructive |
| `elicit.disambiguate` | enum | `never` | daemon | no | `--elicit-disambiguate` | `MCPX_ELICIT_DISAMBIGUATE` | ask which instance of a stateful server to use when several are live and the script did not choose |
| `elicit.disambiguateDefault` | enum | `new` | daemon | no | `--elicit-disambiguate-default` | `MCPX_ELICIT_DISAMBIGUATE_DEFAULT` | which instance is used when nobody answers in time: the one the scope would have chosen, or the most recently used |
| `elicit.pendingLimit` | int | `100` | daemon | yes | `--elicit-pending-limit` | `MCPX_ELICIT_PENDING_LIMIT` | *(plumbing)* how many unanswered questions one listing returns |

### embeddings

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `embeddings.apiKey` | string | *(empty)* | daemon | yes | `--embeddings-api-key` | `MCPX_EMBEDDINGS_API_KEY` | API key for remote embeddings endpoint |
| `embeddings.backend` | string | `auto` | daemon | yes | `--embeddings-backend` | `MCPX_EMBEDDINGS_BACKEND` | which embedding engine to use (auto, local, remote, wasm) |
| `embeddings.model` | string | `text-embedding-3-small` | daemon | yes | `--embeddings-model` | `MCPX_EMBEDDINGS_MODEL` | model name for remote embeddings endpoint |
| `embeddings.url` | string | *(empty)* | daemon | yes | `--embeddings-url` | `MCPX_EMBEDDINGS_URL` | OpenAI-compatible embeddings endpoint URL |
| `embeddings.wasmPath` | string | *(empty)* | daemon | yes | `--embeddings-wasm-path` | `MCPX_EMBEDDINGS_WASM_PATH` | path to compiled WebAssembly neural embedding model |

### events

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `events.history` | int | `1024` | daemon | no | `--events-history` | `MCPX_EVENTS_HISTORY` | how many past events a late subscriber can replay |
| `events.reconnect` | duration | `2s` | client | no | `--events-reconnect` | `MCPX_EVENTS_RECONNECT` | how long a client waits before resuming a dropped stream |

### exec

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `exec.output` | enum | `text` | call | yes | `--exec-output`, `--output` | `MCPX_EXEC_OUTPUT` | text for a terminal, structured for one document, stream for frames |
| `exec.timeout` | duration | `120s` | call | yes | `--exec-timeout` | `MCPX_EXEC_TIMEOUT` | kill a script after this long |
| `exec.where` | enum | `auto` | call | yes | `--exec-where` | `MCPX_EXEC_WHERE` | in this process, or on the daemon |

### hooks

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `hooks.autonomy` | enum | `run` | call | yes | `--hooks-autonomy` | `MCPX_HOOKS_AUTONOMY` | whether configured script hooks (before, onSuccess, onError) run |

### http

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `http.bodyLimit` | bytes | `1048576` | daemon | yes | `--http-body-limit` | `MCPX_HTTP_BODY_LIMIT` | *(plumbing)* the ceiling on an ordinary /v1 request body |
| `http.callBodyLimit` | bytes | `67108864` | daemon | yes | `--http-call-body-limit` | `MCPX_HTTP_CALL_BODY_LIMIT` | the ceiling on a POST /v1/call body |
| `http.controlBodyLimit` | bytes | `65536` | daemon | yes | `--http-control-body-limit` | `MCPX_HTTP_CONTROL_BODY_LIMIT` | *(plumbing)* the ceiling on a body that should hold one field |
| `http.idleConnTimeout` | duration | `30s` | client | no | `--http-idle-conn-timeout` | `MCPX_HTTP_IDLE_CONN_TIMEOUT` | *(plumbing)* how long an unused connection is kept |
| `http.idleConns` | int | `8` | client | no | `--http-idle-conns` | `MCPX_HTTP_IDLE_CONNS` | *(plumbing)* idle connections kept to a local daemon |
| `http.readHeaderTimeout` | duration | `10s` | daemon | no | `--http-read-header-timeout` | `MCPX_HTTP_READ_HEADER_TIMEOUT` | *(plumbing)* how long a client has to finish sending its headers |
| `http.remoteIdleConns` | int | `16` | client | no | `--http-remote-idle-conns` | `MCPX_HTTP_REMOTE_IDLE_CONNS` | *(plumbing)* idle connections kept to a daemon over the network |
| `http.requestTimeout` | duration | `10m0s` | client | no | `--http-request-timeout` | `MCPX_HTTP_REQUEST_TIMEOUT` | how long a CLI request to the daemon may take |
| `http.shutdownGrace` | duration | `5s` | daemon | yes | `--http-shutdown-grace` | `MCPX_HTTP_SHUTDOWN_GRACE` | how long in-flight requests have when the daemon stops |
| `http.ssePing` | duration | `15s` | daemon | yes | `--http-sse-ping` | `MCPX_HTTP_SSE_PING` | *(plumbing)* how often a comment is sent on an idle event stream |
| `http.sseRetry` | duration | `2s` | daemon | yes | `--http-sse-retry` | `MCPX_HTTP_SSE_RETRY` | *(plumbing)* how long a dropped subscriber is told to wait |
| `http.streamBufferInit` | bytes | `65536` | client | no | `--http-stream-buffer-init` | `MCPX_HTTP_STREAM_BUFFER_INIT` | *(plumbing)* the initial line buffer when reading an event stream |
| `http.streamBufferMax` | bytes | `8388608` | client | no | `--http-stream-buffer-max` | `MCPX_HTTP_STREAM_BUFFER_MAX` | *(plumbing)* the largest single event line that will be read |

### logging

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `logging.dir` | string | *(empty)* | daemon | no | `--logging-dir`, `--log-dir` | `MCPX_LOGGING_DIR` | where the JSONL files are written |
| `logging.file` | bool | `true` | daemon | no | `--logging-file` | `MCPX_LOGGING_FILE` | whether the durable JSONL log is written at all |
| `logging.format` | enum | `text` | client | no | `--logging-format`, `--format` | `MCPX_LOGGING_FORMAT` | how records are rendered |
| `logging.include` | list | `host,user,process,version` | daemon | no | `--logging-include`, `--include` | `MCPX_LOGGING_INCLUDE` | which context blocks are attached to lifecycle records |
| `logging.keep` | int | `8` | daemon | no | `--logging-keep`, `--keep` | `MCPX_LOGGING_KEEP` | how many rolled files to keep |
| `logging.level` | enum | `info` | daemon | yes | `--logging-level`, `--log-level` | `MCPX_LOGGING_LEVEL`, `MCPX_LOG_LEVEL` | the lowest level that is kept |
| `logging.maxAge` | duration | `24h` | daemon | no | `--logging-max-age` | `MCPX_LOGGING_MAX_AGE` | roll the log file once it is this old |
| `logging.maxBytes` | bytes | `16MB` | daemon | no | `--logging-max-bytes` | `MCPX_LOGGING_MAX_BYTES` | roll the log file once it reaches this size |
| `logging.maxLines` | int | `0` | daemon | no | `--logging-max-lines` | `MCPX_LOGGING_MAX_LINES` | roll the log file once it holds this many records |
| `logging.source` | enum | `warn` | daemon | yes | `--logging-source`, `--log-source` | `MCPX_LOGGING_SOURCE` | from which level upward to record the calling file and line |
| `logging.trace` | bool | `false` | daemon | no | `--logging-trace` | `MCPX_TRACE` | log which instance served every tool call |

### logstore

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `logstore.followBacklog` | int | `1000` | client | no | `--logstore-follow-backlog` | `MCPX_LOGSTORE_FOLLOW_BACKLOG` | *(plumbing)* how many records one poll of `mcpx log --follow` may emit |
| `logstore.queryLimit` | int | `100` | call | yes | `--logstore-query-limit` | `MCPX_LOGSTORE_QUERY_LIMIT` | how many records a log query returns by default |

### mcp

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `mcp.pageSize` | int | `100` | client | no | `--mcp-page-size` | `MCPX_MCP_PAGE_SIZE` | how many items one tools/list reply carries |
| `mcp.passthrough` | string | *(empty)* | client | no | `--mcp-passthrough`, `--passthrough` | `MCPX_MCP_PASSTHROUGH` | serve upstreams' tools, prompts and resources under their own names |

### output

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `output.color` | enum | `auto` | client | no | `--output-color` | `MCPX_OUTPUT_COLOR` | whether to colourise the browser |
| `output.json` | bool | `false` | client | no | `--output-json`, `--json` | `MCPX_OUTPUT_JSON` | emit one machine-readable document |

### paths

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `paths.adapters` | paths | *(empty)* | client | no | `--paths-adapters` | `MCPX_PATHS_ADAPTERS` | files declaring command-line programs as MCP servers |
| `paths.apis` | paths | *(empty)* | client | no | `--paths-apis` | `MCPX_PATHS_APIS` | files naming OpenAPI documents to expose as tools |
| `paths.cache` | string | *(empty)* | client | no | `--paths-cache` | `MCPX_PATHS_CACHE`, `MCPX_CACHE_DIR` | where generated clients and schemas are kept |
| `paths.config` | paths | *(empty)* | client | no | `--paths-config` | `MCPX_PATHS_CONFIG` | where configuration files are looked for |
| `paths.configFile` | string | *(empty)* | client | no | `--config` | `MCPX_CONFIG` | read exactly this configuration file instead of searching |
| `paths.placeholders` | paths | *(empty)* | client | no | `--paths-placeholders` | `MCPX_PATHS_PLACEHOLDERS` | directories of files declaring launcher placeholders |
| `paths.scripts` | paths | *(empty)* | client | no | `--paths-scripts` | `MCPX_PATHS_SCRIPTS` | where named scripts are looked for |
| `paths.state` | string | *(empty)* | client | no | `--paths-state` | `MCPX_PATHS_STATE`, `MCPX_STATE_DIR` | where the daemon socket, logs and index live |

### plugin

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `plugin.annotate` | bool | `true` | plugin | no | `--plugin-annotate` | `MCPX_PLUGIN_ANNOTATE` | add mcpx's own notes to a tool result |
| `plugin.backend` | enum | `auto` | plugin | no | `--plugin-backend` | `MCPX_PLUGIN_BACKEND` | whether the plugin talks to the daemon or spawns the binary |
| `plugin.bin` | string | `mcpx` | plugin | no | `--plugin-bin` | `MCPX_PLUGIN_BIN` | the executable the plugin invokes |
| `plugin.binArgs` | list | *(empty)* | plugin | no | `--plugin-bin-args` | `MCPX_PLUGIN_BIN_ARGS` | arguments put in front of every plugin invocation of mcpx |
| `plugin.daemonTools` | enum | `auto` | plugin | no | `--plugin-daemon-tools` | `MCPX_PLUGIN_DAEMON_TOOLS` | offer the tools that list and choose between daemons |
| `plugin.discoveryRetry` | duration | `1m0s` | plugin | no | `--plugin-discovery-retry` | `MCPX_PLUGIN_DISCOVERY_RETRY` | how long the plugin waits before looking for mcpx again |
| `plugin.env` | enum | `full` | plugin | no | `--plugin-env` | `MCPX_PLUGIN_ENV` | how much session context is put into each command's environment |
| `plugin.headless` | enum | `auto` | plugin | no | `--plugin-headless` | `MCPX_PLUGIN_HEADLESS` | suppress toasts, because nobody is looking at a screen |
| `plugin.instructions` | bool | `false` | plugin | no | `--plugin-instructions` | `MCPX_PLUGIN_INSTRUCTIONS` | add mcpx usage guidance to the system prompt |
| `plugin.remember` | enum | `session` | plugin | no | `--plugin-remember` | `MCPX_PLUGIN_REMEMBER` | how long a chosen daemon stays chosen |
| `plugin.toolTiming` | bool | `false` | plugin | no | `--plugin-tool-timing` | `MCPX_PLUGIN_TOOL_TIMING` | write every tool outcome into mcpx's log |
| `plugin.tools` | bool | `false` | plugin | no | `--plugin-tools` | `MCPX_PLUGIN_TOOLS` | expose mcpx itself as tools the model can call |

### plumbing

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `plumbing.allowTsJsOverlap` | bool | `false` | client | no | `--plumbing-allow-ts-js-overlap` | `MCPX_PLUMBING_ALLOW_TS_JS_OVERLAP` | *(plumbing)* permit a script directory holding both foo.ts and foo.js |
| `plumbing.indexOnQuery` | bool | `true` | daemon | no | `--plumbing-index-on-query` | `MCPX_PLUMBING_INDEX_ON_QUERY` | *(plumbing)* bring the log index up to date before answering a query |
| `plumbing.launcherPlaceholderRepeat` | list | *(empty)* | client | no | `--plumbing-launcher-placeholder-repeat` | `MCPX_PLUMBING_LAUNCHER_PLACEHOLDER_REPEAT` | *(plumbing)* launcher placeholders permitted to resolve more than once |
| `plumbing.sourceDirAllowed` | bool | `true` | client | no | `--plumbing-source-dir-allowed` | `MCPX_PLUMBING_SOURCE_DIR_ALLOWED` | *(plumbing)* whether a directory may stand in for a source string at all |
| `plumbing.sourceDirRecursive` | bool | `false` | client | no | `--plumbing-source-dir-recursive` | `MCPX_PLUMBING_SOURCE_DIR_RECURSIVE` | *(plumbing)* when a directory is given as source, descend into subdirectories |
| `plumbing.sourceProbePaths` | bool | `true` | client | no | `--plumbing-source-probe-paths` | `MCPX_PLUMBING_SOURCE_PROBE_PATHS` | *(plumbing)* treat a source argument that names an existing file as a file |
| `plumbing.strictUnknownKeys` | bool | `false` | client | no | `--plumbing-strict-unknown-keys` | `MCPX_PLUMBING_STRICT_UNKNOWN_KEYS` | *(plumbing)* fail on a configuration key no setting claims |
| `plumbing.validatePaths` | bool | `true` | client | no | `--plumbing-validate-paths` | `MCPX_PLUMBING_VALIDATE_PATHS` | *(plumbing)* resolve and verify every referenced path before doing any work |

### pool

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `pool.callTimeout` | duration | `120s` | daemon | yes | `--pool-call-timeout` | `MCPX_POOL_CALL_TIMEOUT` | how long one tool call may take |
| `pool.idleTimeout` | duration | `5m` | daemon | yes | `--pool-idle-timeout` | `MCPX_POOL_IDLE_TIMEOUT` | how long an unused server lingers before it is stopped |
| `pool.max` | int | `4` | daemon | yes | `--pool-max` | `MCPX_POOL_MAX` | how many copies of one server may run at once |
| `pool.min` | int | `0` | daemon | yes | `--pool-min` | `MCPX_POOL_MIN` | how many copies to keep started even when idle |
| `pool.scope` | enum | `global` | daemon | no | `--pool-scope` | `MCPX_POOL_SCOPE` | what counts as the same caller for sharing purposes |
| `pool.sharing` | enum | `shared` | daemon | no | `--pool-sharing` | `MCPX_POOL_SHARING` | whether callers reuse one instance or each get their own |
| `pool.startTimeout` | duration | `60s` | daemon | yes | `--pool-start-timeout` | `MCPX_POOL_START_TIMEOUT` | how long a server has to become ready |

### preset

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `preset` | list | *(empty)* | client | no | `--preset` | `MCPX_PRESET` | which presets to apply, in order |

### presets

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `presets` | string | *(empty)* | client | no | `--presets` | `MCPX_PRESETS` | named flag bundles: {name: ["--json", "--timeout=30s"]} |

### prompt

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `prompt.autonomy` | enum | `propose` | call | yes | `--prompt-autonomy` | `MCPX_PROMPT_AUTONOMY` | whether a request returns the script (propose) or runs it (run) |
| `prompt.catalogBudget` | int | `1200` | call | yes | `--prompt-catalog-budget` | `MCPX_PROMPT_CATALOG_BUDGET` | approximate token ceiling for the slice of catalog sent with a sampling request |
| `prompt.maxTokens` | int | `1500` | call | yes | `--prompt-max-tokens` | `MCPX_PROMPT_MAX_TOKENS` | *(plumbing)* maxTokens on the sampling request |
| `prompt.runTimeout` | duration | `5m0s` | call | yes | `--prompt-run-timeout` | `MCPX_PROMPT_RUN_TIMEOUT` | how long a recipe or generated script may run |
| `prompt.sample` | enum | `never` | daemon | no | `--prompt-sample` | `MCPX_PROMPT_SAMPLE` | whether a request with no matching recipe may ask the caller's model to write one |
| `prompt.sampleTimeout` | duration | `1m30s` | daemon | no | `--prompt-sample-timeout` | `MCPX_PROMPT_SAMPLE_TIMEOUT` | how long a generation request waits for a model |

### proto

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `proto.askPoll` | duration | `500ms` | daemon | no | `--proto-ask-poll` | `MCPX_PROTO_ASK_POLL` | *(plumbing)* how long one wait for a question may block |
| `proto.askRounds` | int | `8` | daemon | no | `--proto-ask-rounds` | `MCPX_PROTO_ASK_ROUNDS` | how many times one request may come back asking for more |
| `proto.askTTL` | duration | `15m` | daemon | no | `--proto-ask-ttl` | `MCPX_PROTO_ASK_TTL` | *(plumbing)* how long the daemon keeps a call waiting for an answer |
| `proto.askTimeout` | duration | `10m` | daemon | no | `--proto-ask-timeout` | `MCPX_PROTO_ASK_TIMEOUT` | how long one request may be held while a question goes unanswered |
| `proto.mcpPath` | string | `/mcp` | daemon | no | `--proto-mcp-path` | `MCPX_PROTO_MCP_PATH` | where the daemon serves MCP |
| `proto.native` | bool | `true` | daemon | no | `--proto-native` | `MCPX_PROTO_NATIVE` | put an upstream server's questions to mcpx's own MCP client |
| `proto.serveMCP` | bool | `true` | daemon | no | `--proto-serve-mcp` | `MCPX_PROTO_SERVE_MCP` | mount mcpx's own MCP server on the daemon's listeners |
| `proto.sessionIdle` | duration | `30m` | daemon | no | `--proto-session-idle` | `MCPX_PROTO_SESSION_IDLE` | *(plumbing)* how long an unused Streamable HTTP session is kept |
| `proto.stateTTL` | duration | `30m` | daemon | no | `--proto-state-ttl` | `MCPX_PROTO_STATE_TTL` | *(plumbing)* how long a client may resume an interrupted request with |

### protoMessages

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `protoMessages.listMaxAge` | duration | `1m0s` | daemon | no | `--proto-messages-list-max-age` | `MCPX_PROTO_MESSAGES_LIST_MAX_AGE` | ttlMs on server/discover and every list result sent to a 2026-07-28 client |
| `protoMessages.readMaxAge` | duration | `0s` | daemon | no | `--proto-messages-read-max-age` | `MCPX_PROTO_MESSAGES_READ_MAX_AGE` | ttlMs on resources/read results sent to a 2026-07-28 client |
| `protoMessages.taskAfter` | duration | `2s` | daemon | no | `--proto-messages-task-after` | `MCPX_PROTO_MESSAGES_TASK_AFTER` | how long a tools/call runs in line before a tasks-extension client is handed a task |

### protoTasks

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `protoTasks.pollInterval` | duration | `1s` | daemon | no | `--proto-tasks-poll-interval` | `MCPX_PROTO_TASKS_POLL_INTERVAL` | the pollInterval every task carries |

### recipes

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `recipes.limit` | int | `5` | call | yes | `--recipes-limit` | `MCPX_RECIPES_LIMIT` | how many ranked recipes are returned |
| `recipes.matchMargin` | int | `150` | call | yes | `--recipes-match-margin` | `MCPX_RECIPES_MATCH_MARGIN` | how far ahead the best recipe must be, as a percentage of the next one |
| `recipes.minScore` | int | `30` | call | yes | `--recipes-min-score` | `MCPX_RECIPES_MIN_SCORE` | the score below which a request is not considered a match for any recipe |

### registry

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `registry.limit` | int | `20` | call | yes | `--registry-limit` | `MCPX_REGISTRY_LIMIT` | how many registry results are returned |
| `registry.maxPages` | int | `10` | client | no | `--registry-max-pages` | `MCPX_REGISTRY_MAX_PAGES` | *(plumbing)* how many requests one registry search may make |
| `registry.pageSize` | int | `30` | client | no | `--registry-page-size` | `MCPX_REGISTRY_PAGE_SIZE` | *(plumbing)* how many entries are fetched per registry request |
| `registry.timeout` | duration | `30s` | client | no | `--registry-timeout` | `MCPX_REGISTRY_TIMEOUT` | how long a registry request may take |
| `registry.url` | string | `https://registry.modelcontextprotocol.io` | client | no | `--registry-url` | `MCPX_REGISTRY_URL` | where `mcpx registry` looks for servers |

### repair

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `repair.autonomy` | enum | `advise` | call | yes | `--repair-autonomy` | `MCPX_REPAIR_AUTONOMY` | how far mcpx goes about a failed call: off attaches nothing, advise attaches diagnostics |

### retention

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `retention.maxTools` | int | `24` | daemon | no | `--retention-max-tools` | `MCPX_RETENTION_MAX_TOOLS` | maximum active tools in reactive working set |
| `retention.stickyWindow` | duration | `2h` | daemon | no | `--retention-sticky-window` | `MCPX_RETENTION_STICKY_WINDOW` | duration explicitly requested tools remain retained |

### script

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `script.before` | source | *(empty)* | client | no | `--script-before` | `MCPX_SCRIPT_BEFORE` | runs first, ahead of the globals being installed |
| `script.captureConsole` | bool | `true` | client | no | `--script-capture-console` | `MCPX_SCRIPT_CAPTURE_CONSOLE` | route console calls into the log |
| `script.env` | list | *(empty)* | client | no | `--script-env`, `--env` | `MCPX_SCRIPT_ENV` | KEY=VALUE pairs added to the script's environment |
| `script.launcher` | source | *(empty)* | client | no | `--script-launcher` | `MCPX_SCRIPT_LAUNCHER` | replace the generated launcher entirely |
| `script.onError` | source | *(empty)* | client | no | `--script-on-error` | `MCPX_SCRIPT_ON_ERROR` | runs when the script throws; the error still propagates |
| `script.onSuccess` | source | *(empty)* | client | no | `--script-on-success` | `MCPX_SCRIPT_ON_SUCCESS` | runs when the script returns without throwing |
| `script.permissions` | string | `all` | client | no | `--script-permissions`, `--permissions` | `MCPX_SCRIPT_PERMISSIONS`, `MCPX_PERMISSIONS` | permission profiles, composed in order, or raw: flags |
| `script.prefix` | source | *(empty)* | client | no | `--script-prefix` | `MCPX_SCRIPT_PREFIX` | runs after globals are installed, before the script |
| `script.profiles` | string | *(empty)* | client | no | `--script-profiles` | `MCPX_SCRIPT_PROFILES` | user profiles: {name: {deno: [flags], node: [flags]}} or {name: "raw flags"} |
| `script.runtime` | string | `auto` | client | no | `--script-runtime`, `--runtime` | `MCPX_SCRIPT_RUNTIME` | which JavaScript runtime executes the script |
| `script.runtimeOrder` | list | `deno,bun,node` | client | no | `--script-runtime-order` | `MCPX_SCRIPT_RUNTIME_ORDER` | the runtimes auto tries, in order |
| `script.runtimes` | string | *(empty)* | client | no | `--script-runtimes` | `MCPX_SCRIPT_RUNTIMES` | named runtime binaries: {name: {kind, bin, args}} |
| `script.suffix` | source | *(empty)* | client | no | `--script-suffix` | `MCPX_SCRIPT_SUFFIX` | runs last on both paths, like a finally |
| `script.typecheck` | enum | `off` | client | no | `--script-typecheck` | `MCPX_SCRIPT_TYPECHECK` | check the generated program before running it |

### search

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `search.limit` | int | `20` | call | yes | `--search-limit`, `--limit`, `--n` | `MCPX_SEARCH_LIMIT` | how many tools a search returns |
| `search.semantic` | bool | `false` | call | yes | `--search-semantic` | `MCPX_SEARCH_SEMANTIC` | rank search results using local dense vector embeddings |

### serve

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `serve.mode` | string | `code-mode` | daemon | no | `--serve-mode`, `--mode` | `MCPX_SERVE_MODE` | server mode: code-mode, reactive, or full |

### session

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `session.releaseTimeout` | duration | `10s` | client | no | `--session-release-timeout` | `MCPX_SESSION_RELEASE_TIMEOUT` | *(plumbing)* how long releasing a finished session may take |

### spec

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `spec.first` | string | *(empty)* | daemon | no | `--mcp-spec` | `MCPX_MCP_SPEC` | move one MCP revision to the front of spec.precedence |
| `spec.lenient` | list | *(empty)* | daemon | no | `--spec-lenient` | `MCPX_SPEC_LENIENT` | MCP revisions not held strictly; every revision not listed is strict |
| `spec.precedence` | list | `2026-07-28,2025-11-25,2025-06-18,2025-03-26,2024-11-05` | daemon | no | `--spec-precedence` | `MCPX_SPEC_PRECEDENCE` | MCP revisions in the order their rules win a conflict, first wins |

### stats

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `stats.top` | int | `20` | call | yes | `--stats-top` | `MCPX_STATS_TOP` | how many rows a ranked statistic shows |

### tasks

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `tasks.resultWait` | duration | `2m0s` | call | yes | `--tasks-result-wait` | `MCPX_TASKS_RESULT_WAIT` | how long collecting a task result blocks before giving up |
| `tasks.ttl` | duration | `10m0s` | daemon | yes | `--tasks-ttl` | `MCPX_TASKS_TTL` | how long a finished task's result is kept |

### transport

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `transport.allowedOrigins` | list | *(empty)* | daemon | no | `--transport-allowed-origins` | `MCPX_TRANSPORT_ALLOWED_ORIGINS` | browser Origins, beyond loopback, that may reach /mcp |
| `transport.sseKeepAlive` | duration | `15s` | daemon | no | `--transport-sse-keep-alive` | `MCPX_TRANSPORT_SSE_KEEP_ALIVE` | how often a quiet MCP event stream carries a comment line |
| `transport.stdioDrain` | duration | `30s` | client | no | `--transport-stdio-drain` | `MCPX_TRANSPORT_STDIO_DRAIN` | how long `mcpx serve` finishes in-flight requests after its input closes |

### upstream

| setting | kind | default | scope | hot | flag | variable | governs |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `upstream.eraCache` | bool | `true` | daemon | no | `--upstream-era-cache` | `MCPX_UPSTREAM_ERA_CACHE` | remember which era each server configuration speaks, across restarts |
| `upstream.probeTimeout` | duration | `2s` | daemon | no | `--upstream-probe-timeout` | `MCPX_UPSTREAM_PROBE_TIMEOUT` | how long a stdio server/discover may go unanswered before initialize is also sent |
| `upstream.protocol` | enum | `prefer-discover` | daemon | no | `--upstream-protocol` | `MCPX_UPSTREAM_PROTOCOL` | which request to send first to a server that names no protocol of its own; one of prefer-discover (also modern, prefer-modern, prefer-stateless, prefer-newest), prefer-initialize (also legacy, prefer-legacy, prefer-session, prefer-oldest), force-discover (also force-modern, force-stateless), force-initialize (also force-legacy, force-session), follow |

<!-- END GENERATED -->
