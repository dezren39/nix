# What mcpx sends, per revision

mcpx serves five revisions from one server: 2024-11-05, 2025-03-26, 2025-06-18
and 2025-11-25 through an `initialize` handshake, and 2026-07-28 through
per-request `_meta`. Everything is built in the newest shape and rewritten at
one edge (`downgrade` in `internal/mcpserver/revisions.go`), because building
several shapes and choosing is how they drift apart. This page records what
that edge does and what was wrong before it did.

## The modern envelope

Every 2026-07-28 result gets `resultType` (MUST), and
`_meta["io.modelcontextprotocol/serverInfo"]` (SHOULD), merged into whatever
`_meta` the result already carries. `server/discover`, the four list methods
and `resources/read` also get `ttlMs` and `cacheScope`, which the caching page
makes a MUST on complete results -- and only complete ones: an
`input_required` or a task handle carries none.

| method | cacheScope | ttlMs from |
| --- | --- | --- |
| `server/discover`, `tools/list`, `prompts/list`, `resources/templates/list` | public | `protoMessages.listMaxAge` |
| `resources/list` | private (it includes the caller's session artifacts) | `protoMessages.listMaxAge` |
| `resources/read` | private | `protoMessages.readMaxAge` (0: a resource is whatever upstream says now) |

Legacy clients get none of it; `downgrade` strips all four fields.

`server/discover` was answered with `protocolVersions` and a top-level
`serverInfo` -- neither is a `DiscoverResult` field -- so no real modern
client could read it. It is `supportedVersions` now, with the identity in
`_meta`.

A modern request missing `io.modelcontextprotocol/clientCapabilities`, or
naming a log level that does not exist, is `-32602`. mcpx emits no
`notifications/message` at all, so the rule that none go to a request without
a level holds trivially; a test pins it.

## Per-revision fields

Checked against each revision's `schema.ts`, for everything mcpx sends:

| field | first revision | older clients get |
| --- | --- | --- |
| audio content | 2025-03-26 | a text block describing it |
| `Tool.annotations`, `completions` capability | 2025-03-26 | nothing |
| `title` (tools, prompts, arguments, resources), `resource_link`, `structuredContent`, `outputSchema` | 2025-06-18 | nothing; a link becomes an embedded resource, structured content becomes text |
| `icons`, `execution`, core `tasks` capability, url elicitation | 2025-11-25 | nothing |
| `extensions` capability, `resultType`, cache hints, `serverInfo` in `_meta` | 2026-07-28 | nothing |

Two mistakes this found. `extensions` was sent to 2025-11-25, whose
`ServerCapabilities` has no such field. And `resources/templates/list` sent
each template as `{uri: ...}`; every revision's `ResourceTemplate` spells it
`uriTemplate`, so the list was invalid everywhere.

2024-11-05 is served over stdio and Streamable HTTP. Its own HTTP+SSE
transport is not: 2025-03-26 replaced it and 2026-07-28 says new
implementations SHOULD NOT adopt it.

Serving 2024-11-05 does not make it the default for a message that names no
revision on the transport. A Streamable HTTP request with no
`MCP-Protocol-Version` header and no session, and a stdio batch before
`initialize`, are taken as 2025-03-26 (`Headerless`): 2024-11-05 has no
Streamable HTTP, so nothing on `/mcp` can be a 2024-11-05 request, the
2025-06-18/2025-11-25 transport pages say a server SHOULD assume 2025-03-26
there (2026-07-28: MAY), and a batch can only come from a client that believes
batches exist -- 2024-11-05's schema has no batch type, 2025-03-26 says a
server MUST accept them. `Oldest` still governs how a result is spelled for a
stdio client that declared nothing, where guessing downward is the safe
direction.

## initialize

Every legacy lifecycle page: if the server does not support the requested
version it MUST respond with another it supports. mcpx answered `-32022`
instead -- a 2026-07-28 code a legacy client cannot interpret, and no version
to fall back to. An unknown or modern version now gets the latest legacy one
mcpx serves, and the client decides.

## subscriptions/listen

The acknowledgement comes first and carries the listen request id in
`_meta["io.modelcontextprotocol/subscriptionId"]` with the agreed filter; every
notification on the stream carries the same key. A connection holds as many
streams as the client opens, keyed by request id -- mcpx kept one and
replaced it on each listen. A notification kind no filter field names is
dropped on the way out, whatever the notifier produced.

Over Streamable HTTP the listen POST's response is the stream: an SSE response
held open, with a comment every `transport.sseKeepAlive` (one keep-alive for every stream mcpx serves), until the
client closes it (the transport's cancellation). mcpx used to return at once,
ending the response with only the acknowledgement on it. On stdio the client
ends a stream with `notifications/cancelled` naming the listen id.

When mcpx ends a stream itself -- the connection is going away, or the event
source stopped -- the specification contradicts itself. The subscriptions page
says the server SHOULD send a successful `subscriptions/listen` result; the
cancellation page says it MUST send `notifications/cancelled` for the listen id
and MUST NOT send that notification for anything else. mcpx sends the
cancellation first, while the request is still in progress, then the result,
which a client that acted on the cancellation is told to ignore.

Legacy `resources/subscribe` keeps its own untagged stream.

## Resource not found

`-32602` to a 2026-07-28 client, `-32002` to every earlier one, both with
`data.uri`. The backend reports not-found as `mcpserver.ErrResourceNotFound`
for four causes: not an `mcpx://` URI, no namespace in it, a namespace that is
not configured, or an upstream server that said `-32002`/`-32602`. Anything
else is `-32603`; every failure used to be `-32602`, blaming the client's URI
for an upstream timeout. The upstream cases are recognised by the daemon's
error text, because the daemon returns them as text.

## Error codes

mcpx's server emits `-32700`, `-32600`..`-32603`, `-32021` (a listen asking
for `taskIds` without the tasks extension), `-32022` (modern, unsupported
version), and `-32002` to legacy clients only. Nothing else in the reserved
range; a test sweeps both eras.

## Tasks

2025-11-25 core tasks are client-directed: a `task` field on `tools/call`,
`{task}` back, `tasks/list` and a blocking `tasks/result`. Declared only to
2025-11-25; honoured from any legacy client.

The 2026-07-28 extension is server-directed, and forbids the other way: a
server MUST ignore the `task` field and MUST NOT return a task to a client that
did not declare `io.modelcontextprotocol/tasks` on that request. Whether a
call becomes a task follows the tool's `execution.taskSupport`, which a
pass-through upstream declares and mcpx forwards on `tools/list`:

- `required`, from a client that did not declare the extension: `-32021`
  with `data.requiredCapabilities.extensions["io.modelcontextprotocol/tasks"]`,
  before anything runs.
- `optional` or `required`: the call gets 250 ms (`Timing.TaskEager`) to
  finish or to ask its first question. Finished, it is answered directly; a
  question is put to the client inline (`input_required` on the request, no
  task yet), and the retry carrying the answer goes through the same window.
  Past it, the call is a task. A question the call asks after that parks
  the task in `input_required`, with the question in `inputRequests` on
  `tasks/get`; `tasks/update` answers it key by key -- answered keys leave
  `inputRequests` at once, keys not outstanding are ignored -- and the task
  goes back to `working`. `tasks/cancel` abandons the call behind it.
- anything else: run in line, and answered with a task only if it has not
  finished within `protoMessages.taskAfter`. A client that can answer
  questions inline is never handed one of these.
- `forbidden`: never a task.

The `CreateTaskResult` is flat (`resultType: "task"`, `ttlMs`,
`pollIntervalMs`). `tasks/get` inlines the result or error; a tool result with
`isError` is `completed`, not `failed` (the store's 2025-11-25 rule is the
opposite and is translated), and an upstream's JSON-RPC error is `failed` with
that `error`. `tasks/update` and `tasks/cancel` acknowledge with an empty
result, `tasks/cancel` also on a task already finished. `Mcp-Name` on
`tasks/get`, `tasks/update` and `tasks/cancel` mirrors the `taskId`; one that
contradicts it is `-32020`, one left out is tolerated. `tasks/list` and
`tasks/result` are `-32601` to a modern client: the SEP says so, which
overrides accepting liberally.

A task started on a legacy connection with an identity belongs to it:
another connection cannot list it, and gets not-found for its id. Before, one
server-wide store meant any HTTP client could list every other client's tasks
and read their results.

## Elicitation

A 2026-07-28 client is never sent `elicitationId`; it was relayed verbatim
inside `inputRequests`. A 2025-11-25 url-mode request REQUIRES one, so a
question relayed from an upstream that did not supply it gets `mcpx-<id>`.

## requestState

Bound to the request that carries it -- its method and a SHA-256 of its
parameters without `_meta`, `inputResponses` and `requestState` -- plus expiry,
under the per-process HMAC key. It was bound to an `Mcp-Session-Id`, which
2026-07-28 does not have: mcpx minted a session on `server/discover` to have
something to bind to, and a client that skipped discover could not resume at
all. That minting is gone. mcpx has no authenticated principal to add, so a
state is replayable by anyone who holds it on the same request, within its
TTL; the MRTR page notes that binding does not by itself make a state
single-use.

## tools/list order

The fixed tools come first in their written order; contributed tools --
adapters, OpenAPI operations, /v1 operations -- are sorted by name once, when
they are added, so the list is the same across calls and restarts.
