# Parity

> Everything the CLI can do can be done from a plugin, can be done over
> `/v1`, can be done from MCP.

That is a claim, not a wish, and this file is where it is checked. A gap that
nobody wrote down is a gap nobody fixes: the surface an agent reaches through
is whichever one it happens to be holding, and a capability missing from that
one is missing entirely as far as it is concerned.

Three of the four columns are held together by a test rather than by this
document. `internal/api/ops.go` declares every `/v1` operation once;
`internal/api/parity_test.go` fails if a route is registered without an entry,
if an entry has no route, or if a non-streaming entry has no MCP tool. Adding
a `/v1` route that MCP cannot reach is therefore not possible without
deleting a test.

The plugin column is not enforced, because a typed method is a convenience
rather than a capability: the plugin holds a `DaemonClient` with `get` and
`post`, so every `/v1` operation is already reachable from it. A tick means a
named method exists in `plugin/opencode/mcpx/daemon.ts`.

## /v1 operations

Generated tools are named `mcpx_<operation>`. Where the column says something
else, a hand-written tool already covers the operation and a second one would
be a duplicate with a different spelling.

| Operation | Route | CLI | MCP tool | Plugin | Notes |
| --- | --- | --- | --- | --- | --- |
| `health` | `GET /v1/health` | `mcpx status` | `mcpx_health` | `alive()` | |
| `status` | `GET /v1/status` | `mcpx status` | `mcpx_status` | – | |
| `events` | `GET /v1/events` | `mcpx elicit watch`, `mcpx tui` | **none** | – | A stream; MCP carries the same events through `subscriptions/listen`. |
| `elicit_list` | `GET /v1/elicit` | `mcpx elicit` | `mcpx_elicit_list` | `pendingElicitations()` | |
| `elicit_get` | `GET /v1/elicit/{id}` | `mcpx elicit show` | `mcpx_elicit_get` | – | |
| `elicit_answer` | `POST /v1/elicit/{id}/{action}` | `mcpx elicit answer` | `mcpx_elicit_answer` | `answer()` | Privileged. |
| `log_record` | `POST /v1/log` | `mcpx log record` | `mcpx_log_record` | `record()` | Privileged. |
| `log_query` | `GET /v1/log` | `mcpx log` | `mcpx_log_query` | `logQuery()` | `--follow` is CLI-only: it is a stream. |
| `stats_query` | `GET /v1/stats` | `mcpx stats` | `mcpx_stats_query` | `stats()` | |
| `registry_search` | `GET /v1/registry/search` | `mcpx registry search` | `mcpx_registry_search` | `registrySearch()` | |
| `complete` | `POST /v1/complete` | – | `mcpx_complete` | `complete()` | See the gap below. |
| `resource_templates` | `GET /v1/resource-templates` | `mcpx resources` | `mcpx_resource_templates` | – | |
| `prompts` | `GET /v1/prompts` | `mcpx prompts` | `mcpx_prompts` | – | Also `prompts/list` over MCP. |
| `resources` | `GET /v1/resources` | `mcpx resources` | `mcpx_resources` | – | Also `resources/list` over MCP. |
| `prompt_get` | `POST /v1/prompt` | `mcpx prompts <ns.name>` | `mcpx_prompt_get` | – | Also `prompts/get` over MCP. |
| `namespaces` | `GET /v1/namespaces` | `mcpx ls` | `mcpx_namespaces` | `namespaces()` | |
| `tools` | `GET /v1/tools` | `mcpx types` | `mcpx_tools` | – | |
| `search` | `GET /v1/search` | `mcpx search` | `mcpx_search` | – | |
| `types` | `GET /v1/types` | `mcpx types` | `mcpx_types` | – | |
| `catalog` | `GET /v1/catalog` | `mcpx catalog` | `mcpx_catalog` | – | |
| `client_module` | `GET /v1/client.ts` | `mcpx client` | `mcpx_client_module` | – | |
| `globals` | `GET /v1/globals.d.ts` | – | `mcpx_globals` | – | The CLI reads it while building a script's launcher rather than as a command. |
| `call` | `POST /v1/call` | `mcpx call` | `mcpx_call` | `call()`, `callAsTask()` | `task` is `/v1`-only; MCP has tasks natively on `tools/call`. |
| `resource_read` | `POST /v1/resource` | `mcpx resources <ns/uri>` | `mcpx_resource_read` | – | Also `resources/read` over MCP. |
| `tasks_list` | `GET /v1/tasks` | – | `mcpx_tasks_list` | `tasks()` | MCP also has `tasks/list`. |
| `task_get` | `GET /v1/tasks/{id}` | – | `mcpx_task_get` | `task()` | MCP also has `tasks/get`. |
| `task_result` | `GET /v1/tasks/{id}/result` | – | `mcpx_task_result` | `taskResult()` | MCP also has `tasks/result`. |
| `task_cancel` | `POST /v1/tasks/{id}/cancel` | – | `mcpx_task_cancel` | `cancelTask()` | MCP also has `tasks/cancel`. |
| `session_release` | `POST /v1/session/release` | – | `mcpx_session_release` | – | Privileged. |
| `refresh` | `POST /v1/refresh` | `mcpx refresh` | `mcpx_refresh` | – | Privileged. |
| `restart` | `POST /v1/restart` | `mcpx restart` | `mcpx_restart` | – | Privileged, destructive. |
| `shutdown` | `POST /v1/shutdown` | `mcpx stop` | `mcpx_shutdown` | – | Privileged, destructive. |
| `openapi` | `GET /v1/openapi.json` | `mcpx openapi` | `mcpx_openapi` | – | `mcpx openapi` prints the *tool* document; this one describes `/v1`. |

Privileged operations are offered rather than hidden. A caller that can reach
the socket can already stop the daemon, so withholding the tool buys no safety
and costs an agent the ability to restart a server that has wedged. What they
carry instead is a description that says so and MCP annotations
(`readOnlyHint`, `destructiveHint`) for a client that wants to scope on them.

## CLI commands

Many commands are local work with no daemon behind them. Those rows are
honest gaps rather than oversights: `mcpx init` writes a file in the current
directory, and a daemon that did it would be writing somewhere else.

| Command | /v1 | MCP tool | Plugin | Notes |
| --- | --- | --- | --- | --- |
| `run` | – | `mcpx_exec` (source, not a name) | – | Running a *named* script is local: the search path is the caller's. |
| `exec` | – | `mcpx_exec` | – | The runtime is spawned by whoever runs the command. |
| `scripts` | – | – | – | Local: lists the caller's search path. |
| `ls` / `list` / `namespaces` | `namespaces` | `mcpx_namespaces` | `namespaces()` | |
| `catalog` | `catalog` | `mcpx_catalog` | – | |
| `types` | `types` | `mcpx_types` | – | |
| `search` | `search` | `mcpx_search` | – | |
| `call` | `call` | `mcpx_call` | `call()` | |
| `client` | `client_module` | `mcpx_client_module` | – | |
| `status` | `status`, `health` | `mcpx_status` | `alive()` | |
| `daemons` | – | – | – | Local: reads every daemon's info file. One daemon cannot see another. |
| `refresh` | `refresh` | `mcpx_refresh` | – | |
| `restart` | `restart` | `mcpx_restart` | – | |
| `stop` | `shutdown` | `mcpx_shutdown` | – | |
| `daemon` | – | – | – | Starting a daemon over a daemon's API is a bootstrap that cannot work. |
| `config` | – | – | – | Reads and writes files the daemon does not own. |
| `init` | – | – | – | Writes into the current directory. |
| `schema` | – | – | – | Renders the setting registry, which is compiled in. |
| `elicit` | `elicit_list`, `elicit_get`, `elicit_answer` | three tools | `pendingElicitations()`, `answer()` | `elicit watch` is the event stream. |
| `doctor` | – | – | – | Checks the machine: binaries on PATH, permissions, config files. |
| `prompts` | `prompts`, `prompt_get` | `mcpx_prompts`, `mcpx_prompt_get` | – | |
| `resources` | `resources`, `resource_read`, `resource_templates` | three tools | – | |
| `tui` | `events` and most reads | – | – | A terminal interface; it consumes the API rather than being in it. |
| `explore` | – | – | – | As `tui`. |
| `log` | `log_query`, `log_record` | `mcpx_log`, `mcpx_log_query`, `mcpx_log_record` | `logQuery()`, `record()` | `log sql` and `log --follow` are CLI-only. |
| `stats` | `stats_query` | `mcpx_stats`, `mcpx_stats_query` | `stats()` | `stats opencode` reads opencode's own database, not mcpx's log. |
| `registry` | `registry_search` | `mcpx_registry`, `mcpx_registry_search` | `registrySearch()` | `registry add` writes a config file, which is local. |
| `serve` | – | – | – | This is the MCP surface; it cannot be a tool on itself. |
| `openapi` | `openapi` | `mcpx_openapi` | – | Two documents: tools, and `/v1`. |
| `adapter` | – | adapted programs become tools | – | Declarations are files the daemon does not read. |
| `api` | – | declared operations become tools | – | As `adapter`. |
| `help`, `man`, `completion` | – | – | – | Text about the binary, produced by the binary. |

## Known gaps

- **`log --follow` and `elicit watch` have no tool.** Both are streams, and a
  tool result is one value. `/v1/events` carries them, and MCP carries them as
  `subscriptions/listen`.
- **`log sql` has no route.** Raw SQL against the index is a different trust
  boundary from a filter, and exposing it over an unauthenticated socket is a
  decision rather than an omission.
- **`POST /v1/complete` does not yet reach the upstream server.** The route,
  the tool and the plugin method exist and validate, and the answer says
  `upstream: false` when it came from what mcpx already knows -- prompt names
  and resource template URIs -- rather than from the server. Forwarding needs
  a raw-request method on the pooled MCP session, which lives in
  `internal/pool`; the route picks it up automatically, through an interface
  assertion, the moment that method exists. A client cannot otherwise tell an
  empty answer from an unimplemented one, which is why the flag is in the body
  rather than in a comment.
- **The plugin column is thin on purpose.** `DaemonClient.get`/`post` reach
  everything; a named method is added when something actually calls it.
