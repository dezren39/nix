# What the specification asks of a server

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T16:00:00-05:00
increment:    3
status:       standard
tags:         area:protocol, area:spec
description:  per revision, what an MCP server MUST, SHOULD and MAY do,
              including the obligations the prose implies but never states.
```

This document is about the specification, not about mcpx. It exists because
the spec's MUSTs are scattered: across the lifecycle, transport, versioning and
utility pages, and across the doc comments in `schema.ts`, which carry several
rules the prose never repeats. A server written from the feature pages alone
misses most of them. What mcpx does is summarised in a line where it matters
and documented in [`../protocol.md`](../protocol.md); the per-requirement
status, each with its test, is [`conformance-matrix.md`](conformance-matrix.md).
The feature-by-revision table (which method, field, code and header exists
where) is [`../compare/matrix-revisions.md`](../compare/matrix-revisions.md),
the character of each revision is
[`../compare/what-a-server-should-be.md`](../compare/what-a-server-should-be.md),
and every place two sources disagree is
[`../compare/conflicts.md`](../compare/conflicts.md); none of that is repeated
here. The client side is [`client-obligations.md`](client-obligations.md).

This document is the distilled form. The authority for what mcpx actually
does is the code — `internal/mcpserver/revisions.go` holds the floors,
ceilings and removals as data, and `internal/mcpspec` validates real traffic
against the official schemas for all five revisions. A matrix in a document
and a matrix in code agree on the day they are written and never again, so
where they differ, the code is right and this file is stale.

Revisions are written by date. **Legacy** means 2024-11-05 through 2025-11-25,
which share an `initialize` handshake. **Modern** means 2026-07-28, which has
no handshake.

URLs abbreviate `https://modelcontextprotocol.io/specification/` as `spec/`.

---

## 1. Lifecycle

### Legacy (2024-11-05 .. 2025-11-25)

The session begins with `initialize`
([spec/2025-11-25/basic/lifecycle#initialization](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#initialization)).

- A server **MUST** respond to `initialize` with its capabilities and
  `serverInfo`.
- If it supports the requested `protocolVersion` it **MUST** echo that version.
  Otherwise it **MUST** answer with another version it supports, which
  **SHOULD** be its latest
  ([#version-negotiation](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#version-negotiation)).
  The answer is a *result*, not an error, and the client decides whether it
  can continue. Answering with an error takes that decision away from the
  client. The modern error code `-32022` did not exist in any legacy revision,
  so a legacy client has no rule for recognising it.
- Before `notifications/initialized`, a server **SHOULD NOT** send anything
  except pings and logging.
- A server **MUST NOT** send a request or notification outside the
  capabilities it declared. Nor may it use a client capability the client did
  not declare. Sampling, roots and elicitation are all gated on the client's
  declaration.
- Shutdown is transport-level. On stdio the client closes stdin, and a server
  **MAY** also exit on its own. The legacy HTTP transports have no shutdown
  message.

The negotiated version, the client's capabilities, the log level and the
`resources/subscribe` set are **session state**. §3.4 covers where a dual-era
server keeps it.

### Modern (2026-07-28)

There is no handshake. Every request carries
`_meta["io.modelcontextprotocol/protocolVersion"]` and
`_meta["io.modelcontextprotocol/clientCapabilities"]`, and both are REQUIRED
([spec/2026-07-28/basic/versioning](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning)).

- A server **MUST** implement `server/discover`
  ([spec/2026-07-28/server/discover](https://modelcontextprotocol.io/specification/2026-07-28/server/discover)).
  The `DiscoverResult` is `{supportedVersions, capabilities, instructions?,
  _meta: {"io.modelcontextprotocol/serverInfo": …}, ttlMs, cacheScope,
  resultType}`. The field is `supportedVersions`, not `protocolVersions`, and
  `serverInfo` lives in `_meta`, not at the top level. Discovery is a
  cacheable result, so `ttlMs` and `cacheScope` are required here too, even
  though the changelog's list omits `server/discover`.
- A request whose version the server does not support **MUST** be rejected
  with `-32022`, and the supported versions go in `data.supported`.
- A request that omits the required `_meta` fields **MUST** be rejected with
  `-32602`.
- A server **MUST NOT** infer client capabilities from earlier requests (schema
  `RequestMetaObject`). The capabilities on *this* request are the only ones
  that exist for it.
- Every result **MUST** carry `resultType`. A client treats an absent
  `resultType` as `"complete"`.
- A server **SHOULD** put its `serverInfo` in every result's `_meta`.
- A server **MUST NOT** send requests to the client at all. Questions for the
  client travel as MRTR `inputRequests` inside a result (§4.4).

**Statelessness is a rule, not a transport detail.** State that spans requests
"MUST be referenced by an explicit identifier the client passes on each
request", and "an open connection, such as a STDIO process, is not a
conversation or session". A server "SHOULD NOT require that a client reuse the
same connection or process". Three consequences follow, none of them spelled
out:

- A `requestState`, a task id or a subscription cannot be *validated* by
  comparing it with the connection it arrived on. If a server needs to bind a
  handle to a caller, the binding has to be inside the handle (signed claims:
  principal, expiry, originating method and parameters) or come from the
  authorization identity. mcpx binds `requestState` to the method, a digest
  of the parameters and an expiry, and to no connection
  ([`messages.md`](messages.md#requeststate)).
- A retry may reach a different process or instance. Any integrity key a
  handle depends on has to be shared by every process that might receive the
  retry. With a per-process key the retry still fails verification, which is
  safe, but the work is lost.
- The server's capabilities are advertised once, by `server/discover`, and
  cached for `ttlMs`. The client's capabilities are restated on every request
  and may change from one request to the next. There is no symmetric
  restatement: a client acts on server capabilities that may be up to `ttlMs`
  stale, and a server acts on client capabilities that are exactly current.

What "stateless" does and does not forbid, item by item, and where a proxy
legitimately keeps state, is [`../compare/stateless.md`](../compare/stateless.md).

---

## 2. Versioning and eras

The 2026 versioning page makes era "a property of the server, not of an
individual request". A client probes once, then caches the result for the
lifetime of the process (stdio) or origin (HTTP), and MAY persist it across
restarts of the same configuration.

For a **pool of processes of one configuration**, that sentence has a
consequence the spec does not draw. Every process in the pool must answer the
same era. If two members disagree, the binary changed underneath the pool, and
the client's cached answer is wrong for part of it. Since the cache key is the
configuration rather than the process, a server must not be dual-era on some
launches and modern-only on others. Each freshly spawned stdio process still
needs its own `initialize` if it is legacy, because a new process is a new
session, even though the era answer is inherited.

A **dual-era server** answers both `initialize` and `_meta`-versioned requests.
It faces three kinds of inbound request, and the spec defines two of them:

| inbound | spec says | what the server has to do |
| --- | --- | --- |
| `_meta` carries a protocol version | modern semantics, stateless | ignore any legacy session state; use this request's capabilities |
| `initialize`, or a request on a session that was initialised | legacy semantics, scoped to the process (stdio) or session (HTTP) | keep the version and capabilities from `initialize` |
| neither | modern: MUST reject `-32602`; legacy: "not the first interaction" | unspecified. mcpx serves it as 2025-03-26, a choice rather than a rule ([`../compare/stateless.md` §4](../compare/stateless.md#4-the-conflict-at-the-centre)) |

A modern-only server that receives `initialize` **SHOULD** name its supported
versions in the error. A dual-era server has to decide per request whether a
`Mcp-Session-Id` header is legacy state or noise. The reliable signal is the
body: a `_meta` protocol version means modern.

---

## 3. Transports

### 3.1 stdio (all revisions)

- Messages are newline-delimited JSON-RPC and **MUST NOT** contain embedded
  newlines.
- The server **MUST NOT** write anything to stdout that is not a valid MCP
  message. stderr is for logs, and in 2025-11-25 and later it MAY carry any
  output, which clients must not treat as an error.
- Modern: stdio is still stateless. `initialize` makes the whole pipe legacy
  for its lifetime, because one process serves one client. In 2026 a server
  sends `notifications/cancelled` only to end a `subscriptions/listen` stream,
  and only on stdio (§4.2).
- The 2026 page recommends newline-delimited JSON for custom transports over
  Unix sockets and TCP as well. Only process launch, stderr, EOF shutdown and
  restart are specific to stdio.

### 3.2 HTTP+SSE (2024-11-05; deprecated)

The server exposes an SSE endpoint and a POST endpoint.

- On connect it **MUST** send an `endpoint` event naming the POST URL.
- Every server message travels as an SSE `message` event.

From 2025-03-26 on, a server that still offers this transport does so only
for older clients, and new implementations **SHOULD NOT** adopt it.

### 3.3 Streamable HTTP (2025-03-26 .. 2026-07-28)

One endpoint answers POST and, in the legacy revisions, GET.

- **Origin.** Every revision: a server **MUST** validate `Origin` to prevent
  DNS rebinding, and from 2025-11-25 an invalid origin gets 403. On a
  loopback, unauthenticated server this is the *only* defence the transport
  offers against any web page the user visits. Binding to loopback alone does
  not stop DNS rebinding. (mcpx checks it on `/mcp` and on the daemon's `/v1` routes beside it: [`transport.md`](transport.md#origin-every-revision-with-streamable-http), `internal/daemon/origin.go`.)
- **Local binding.** A local server **SHOULD** bind to 127.0.0.1 and **SHOULD**
  authenticate.
- **POST responses.** A POST carrying only responses or notifications gets
  202. A POST carrying a request gets either `application/json` or an SSE
  stream, and the client must accept both.
- **Batches.** 2025-03-26 alone requires a server to accept JSON-RPC batches.
  2025-06-18 removed them.
- **Sessions (legacy).** The server MAY assign `Mcp-Session-Id` in the
  `initialize` response. After that it **SHOULD** reject requests without the
  id with 400, and it **MUST** answer 404 for an id it has terminated. The 404
  is how a client learns it has to re-initialise.
- **Protocol version header (2025-06-18 and later).** The server **MUST**
  reject an unsupported `MCP-Protocol-Version` with 400. With no header, it
  assumes 2025-03-26 (**SHOULD**, and **MAY** in 2026).
- **Resumability and redelivery (legacy).** Event ids and `Last-Event-ID`
  resumption are optional. Where they are offered, a message **MUST NOT** be
  broadcast on more than one stream.
- **Modern (2026-07-28).**
  - No sessions, no GET stream, no resumption. A modern-only server should
    neither mint nor echo `Mcp-Session-Id`.
  - `Mcp-Method` and `Mcp-Name` headers mirror the body, and so do
    `Mcp-Param-*` headers for tool parameters annotated `x-mcp-header`. The
    server that processes the body **MUST** validate them and reject a
    mismatch with `-32020` (HTTP 400).
  - An unknown method gets 404 with `-32601`.
  - Errors that the schema assigns HTTP statuses **MUST** be JSON-RPC error
    bodies. A bare `{"error": …}` or a 200-status error makes a dual-era
    client misclassify the server as legacy and fall back to `initialize`.
    Era detection is only as reliable as the server's error bodies.
  - A client disconnect **MUST** be treated as cancellation. In every legacy
    revision it **SHOULD NOT** be (§4.2).

### 3.4 Where a dual-era server keeps legacy state

The 2026 versioning page scopes an `initialize` "to the stdio process (stdio)
or the session (HTTP)". So legacy state is **per process on stdio and per
session on HTTP**:

- **On stdio**, there is no smaller unit. A second `initialize` on the same
  process had no defined meaning in any legacy revision.
- **On HTTP**, each `Mcp-Session-Id` carries its own negotiated version,
  client capabilities, log level, subscriptions and pending server-to-client
  requests. Modern requests on the same endpoint must not see any of it.
- **A legacy HTTP client that ignores the session header** has no state
  beyond one POST. The server can ask it a question only on that POST's own
  SSE stream, and the answer arrives on a separate POST that cannot be routed
  back without a session.

### 3.5 Authorization (2025-03-26 and later, HTTP only)

Authorization is an HTTP concern. A stdio server gets its credentials from the
environment and **SHOULD NOT** use the OAuth flow.

An HTTP server that implements authorization:
- **MUST** publish Protected Resource Metadata. From 2025-06-18 it answers 401
  with `WWW-Authenticate` naming that metadata.
- **MUST** validate that tokens were issued for it (audience).
- **MUST NOT** pass a client's token through to an upstream API. This is the
  confused-deputy rule.
- A proxy that uses a static client id upstream owes **per-client consent**
  (2025-06-18 and later).

A server that does no authorization has none of these obligations. It also
has no principal to bind `requestState`, tasks or elicitations to, which
matters in §4.4 and §5.

---

## 4. Messages and utilities

### 4.1 JSON-RPC and errors

- A request id **MUST NOT** be null (all revisions).
- A request id **MUST NOT** be reused within a session (legacy). In 2026
  reuse is legal, but a server that also speaks legacy, or that correlates
  subscriptions by id, should never reuse one.
- A parse error omits `id` from 2025-11-25 on. The 2025-03-26 and 2025-06-18
  schemas have no conformant form for it.
- Error codes are era-scoped:
  - `-32002` (resource not found) is legacy; 2026 uses `-32602`.
  - `-32042` (URL elicitation required) exists only in 2025-11-25.
  - `-32020`, `-32021` and `-32022` exist only in 2026.
  - A server sends a peer only the codes that peer's revision defines.
- `_meta` keys under `io.modelcontextprotocol/` are reserved from 2025-11-25.
  In 2025-06-18 that prefix was not reserved, so a peer on that revision may
  use it for its own data. Send reserved keys only to modern peers.

### 4.2 Cancellation

- **Legacy.** Either side may send `notifications/cancelled`, but never for
  `initialize`. The receiver **SHOULD** stop work and **MUST NOT** respond
  afterwards. A late or unknown cancellation is ignored.
- **2026-07-28.** Cancellation depends on the transport:
  - **HTTP:** closing the response stream is the cancellation. The server
    **MUST** treat a disconnect as cancellation and **MUST NOT** send further
    messages for that request.
  - **stdio:** the client sends `notifications/cancelled`.
  - **Server-sent cancellation:** a server **MUST NOT** send
    `notifications/cancelled` for anything except ending a
    `subscriptions/listen` stream.
- **Subscription teardown is contradictory in 2026.**
  - The cancellation page: the server **MUST** send `notifications/cancelled`
    referencing the listen id.
  - The subscriptions page: it **SHOULD** send a successful
    `subscriptions/listen` result instead.
  - The `CancelledNotification` doc comment: the server-sent cancel happens
    *on stdio*.

  Only one of the three is a MUST, and only one ordering satisfies it:
  a cancellation may name only a request that is still in progress, so the
  notification has to go **first** and the result after it. That also
  satisfies the SHOULD, and it costs nothing — a client that acted on the
  cancellation ignores a late response, exactly as the cancellation page
  tells it to, and a client that waits for the result still gets it. The
  schema's "on stdio" then reads as the case it was written for rather than
  as a restriction, because on HTTP closing the SSE stream is a second,
  redundant signal, not a substitute for the one the MUST names. mcpx sends
  both on both transports (`internal/mcpserver/listen.go`, `announceEnd`).
  The quotes are in
  [conflicts C-21](../compare/conflicts.md#b-one-revision-against-itself).
- **A proxy has to propagate cancellation.** When the downstream cancels, the
  proxy cancels upstream:
  - legacy upstream: `notifications/cancelled`;
  - modern HTTP upstream: close the stream;
  - modern stdio upstream: `notifications/cancelled`.

  An upstream call parked behind MRTR has no downstream stream to watch. Its
  lifetime ends only when its `requestState` or task TTL expires.

### 4.3 Progress, logging, pagination, completion, ping

- **Progress.** Sent only for a `progressToken` the client supplied, and
  `progress` **MUST** increase. In 2026 it travels only on that request's
  stream.
- **Logging.**
  - Legacy: sent only if the `logging` capability was declared. Before any
    `logging/setLevel` a server MAY send at its own level.
  - 2026: logging is deprecated, `logging/setLevel` is removed, and the level
    arrives in the request's `_meta`. A server **MUST NOT** log for a request
    that set no level.
  - A dual-era server tracks the `setLevel` value per legacy session and
    ignores it for modern requests.
- **Pagination.**
  - Cursors are opaque. An invalid cursor **SHOULD** get `-32602`, not a
    silent restart from the first page.
  - In 2026 every page of one list **MUST** carry the same `cacheScope`, and
    a client **MUST NOT** treat an empty-string cursor as the end.
  - An aggregating server's cursors are stable only if its merged order is
    deterministic, and the 2026 changelog asks for deterministic `tools/list`
    order.
- **Completion.**
  - Declare `completions`.
  - Return at most 100 values, sorted by relevance.
  - Apply rate limiting on the server side.
- **Ping.**
  - Legacy: a server **MUST** answer promptly, and MAY send pings of its own.
  - 2026 removes `ping`, and servers may not send requests at all. Liveness
    becomes process liveness (stdio) and per-request success (HTTP).
    `server/discover` is the cheap probe.

### 4.4 Multi-round-trip requests (MRTR, 2026-07-28)

A modern server that needs input from the client (elicitation, sampling or
roots) returns `resultType: "input_required"`. The result carries
`inputRequests` and an opaque `requestState`, and the client retries the same
method under a new id with `inputResponses` and the `requestState`.

- `input_required` is allowed only on `tools/call`, `prompts/get` and
  `resources/read`. A question that arises during any other method cannot
  reach a modern client.
- The server **MUST NOT** put an input request in `inputRequests` unless
  *that* request declared the matching client capability. A retry carries its
  own capabilities, and the check is repeated on every round.
- The server **MUST NOT** assume the retry will come. It **MUST** treat
  `requestState` as attacker-controlled, and **SHOULD** make it tamper-evident
  and bound to the principal, an expiry, and the originating method and
  parameters (§1).
- If the call cannot proceed without a capability the client did not
  declare, the server answers `-32021` with `data.requiredCapabilities`. A
  server with a fallback does not have to, but then it owes an eventual
  result or error, never a hang.

### 4.5 Tasks

- **2025-11-25 (core).**
  - A server that declares `tasks.requests.tools.call` **MUST** respect each
    tool's `execution.taskSupport`, and a tool without it is *forbidden* by
    default. So the capability alone enables nothing: a server has to mark
    tools.
  - Task ids **MUST** be unique.
  - `tasks/result` **MUST** block until the task is terminal, and **MUST**
    carry `io.modelcontextprotocol/related-task`.
  - A cancelled task **MUST** stay `cancelled`.
  - `tasks/cancel` on a terminal task **MUST** return `-32602`.
  - `tasks/list` must be scoped by authorization context.
- **2026-07-28 (extension `io.modelcontextprotocol/tasks`).**
  - The server returns `resultType: "task"`, and the result arrives on
    `tasks/get`.
  - A task **must be durably created before the response is sent**.
  - A server **must never** return a task to a client that did not declare
    the extension. `resultType: "task"` is the only thing that distinguishes a
    task handle from a real result, and a client that did not negotiate the
    extension has no schema for it — so returning one unasked hands it a
    result it cannot parse. The changelog's "unsolicited" reads otherwise;
    see [conflicts C-29](../compare/conflicts.md#b-one-revision-against-itself).
    This is also why `tasks/list` is absent from the extension: polling a
    client-hosted task would itself be the server→client request 2026-07-28
    forbids ([`../compare/register/capabilities.md`](../compare/register/capabilities.md)).
  - The extension's methods from a client that did not declare it are
    `-32021` with `data.requiredCapabilities`.
- **What the prose implies.** A task handle outlives the request that created
  it, and the connection. Task state cannot live in connection or session
  state, and `tasks/list` visibility cannot be keyed by connection.
  "Durably" is stronger than "in memory". A pool of processes serving one
  configuration must either share the task store or route `tasks/*` to the
  owning process.

### 4.6 Caching (2026-07-28)

Cacheable results carry `ttlMs` and `cacheScope`, and both are REQUIRED:
`server/discover`, `tools/list`, `resources/list`, `resources/templates/list`,
`prompts/list` and `resources/read`.

- A `private` result **MUST NOT** be served to another authorization context.
  A gateway is the "shared" cache the schema names.
- An aggregator computes its own scope: private if any contributing upstream
  page is private. Its `ttlMs` should not exceed the smallest upstream TTL.
- Lists **MUST NOT** vary per connection.

### 4.7 Subscriptions (2026-07-28)

`subscriptions/listen` replaces `resources/subscribe`, the legacy GET stream
and unsolicited `list_changed` notifications.

- The request carries a `notifications` filter.
- The server acknowledges with `notifications/subscriptions/acknowledged`,
  which echoes the honoured filter. The subscription id travels in `_meta`,
  and the acknowledgement **MUST** be the first message for that
  subscription.
- A server **MUST NOT** send a notification type the filter did not request.
  Every notification on the stream carries
  `_meta["io.modelcontextprotocol/subscriptionId"]`.
- A client may hold **several concurrent listens**, keyed by the listen
  request's id. Each has its own filter, acknowledgement and teardown. The
  server keeps `listenId → {filter, stream}` and fans each event out to every
  matching stream. A new listen must never replace an existing one.
- On stdio the tag is the only way to tell streams apart. On HTTP each listen
  is its own SSE response, and the tag is still required.
- A listen request normally never gets a response, and the general "establish
  timeouts for all requests" rule has no exception for it. Keep-alives
  (SSE comments) exist so intermediaries do not time it out.
- The 2026 resources page still says to send `list_changed` "when the list
  changes" with no mention of a listen stream. The schema's filter rule takes
  precedence.

---

## 5. Features

### Tools

- **Invalid input.** Up to 2025-06-18, invalid input is a protocol error. From
  2025-11-25 it is a tool result with `isError: true`, so the model can
  correct itself. An unknown tool is a protocol error (`-32602`) in every
  revision.
- **Structured output.**
  - `structuredContent` and `outputSchema` arrive in 2025-06-18. With an
    `outputSchema`, the server **MUST** return conforming
    `structuredContent`, and **SHOULD** also include it as text.
  - Both must be objects in 2025-06-18 and 2025-11-25. 2026 allows any JSON
    value and any 2020-12 schema.
- **Names.** 2025-11-25 adds constraints on tool names.
- **Headers.** 2026 adds `x-mcp-header` parameters that are mirrored to
  headers. A server re-listing another server's tools inherits their
  `x-mcp-header` annotations, and with them the obligation to validate the
  mirrored headers (§3.3).
- **Proxies.** Human-in-the-loop confirmation is a host SHOULD, and a proxy
  must not claim it on the host's behalf.

### Resources

- URIs are validated.
- Not-found is `-32002` (legacy) or `-32602` (2026).
- `resources/updated` goes only to clients that subscribed: through
  `resources/subscribe` in legacy revisions, and through a listen filter that
  names the resource in 2026.
- The 2026 prompts page requires an embedded resource to carry a MIME type,
  but the schema makes `mimeType` optional. Send it.

### Prompts

- Arguments are validated.
- 2026 is the only revision whose prompts page requires the capability to be
  declared in `DiscoverResult`. Declare it there regardless.

---

## 6. Accept liberally, send conservatively, field by field

A server that also serves older revisions must not send a peer anything its
revision did not define. Unknown fields are *usually* ignored, but no revision
guarantees it.

| carried | send to | otherwise |
| --- | --- | --- |
| `resultType` | 2026 requests | omit; accept absence as `complete` |
| `structuredContent` | ≥ 2025-06-18; must be an object for 2025-06-18 and 2025-11-25 | render into `content` as text |
| `outputSchema` with a non-object root | 2026 | withhold the tool's schema, or wrap it |
| `resource_link` block | ≥ 2025-06-18 | embedded `resource` |
| `audio` block | ≥ 2025-03-26 | drop, or describe as text |
| tool `annotations` | ≥ 2025-03-26 | strip |
| `title`, resource `lastModified` | ≥ 2025-06-18 | strip |
| `icons`, `description`, `websiteUrl` on `Implementation` | ≥ 2025-11-25 | strip |
| `ttlMs`, `cacheScope` | 2026 (REQUIRED there) | omit |
| `serverInfo` | 2026 in result `_meta` | `InitializeResult.serverInfo` |
| `io.modelcontextprotocol/*` `_meta` keys | ≥ 2025-11-25, and only keys that revision defines | omit |
| error codes | only those the peer's revision defines | nearest defined code |
| batches | never send | accept on input (2025-03-26 MUST; nothing forbids receiving them) |
| server → client requests | legacy peers that declared the capability | MRTR `inputRequests` for 2026 |
| `list_changed`, `resources/updated` | legacy: the session; 2026: matching listen streams only | nothing |
| logs | legacy: at or above the session level; 2026: only when the request set a level | nothing |

Accepting is the other direction. A method beyond the negotiated revision
(for example `subscriptions/listen` from a legacy client) withholds nothing,
and answering it is not a violation. *Declaring* it is: a capability is a
promise about the revision in force. For 2026 peers the answer is narrower,
because an unknown method is `-32601` with 404 on HTTP, and a modern peer
should expect exactly the methods 2026 defines.

---

## 7. Where mcpx stands

Measured, not asserted: the official suite's results, and which of its
failures are defects, are [`official-suite.md`](official-suite.md); the
per-requirement status is [`conformance-matrix.md`](conformance-matrix.md).
Two obligations on this page were corrected in mcpx because the suite found
them: a modern request without the required `_meta` is `-32602`, not `-32020`
(a missing body field is not a header disagreeing with a body), and the five
methods 2026-07-28 removed are `-32601` (404 on HTTP) to a 2026-07-28 peer
(#250; [`../protocol.md`](../protocol.md) §2.1).

Two readings on this page are mcpx decisions, recorded with their reasons in
[`../protocol.md`](../protocol.md): a tool whose upstream may ask a question
the request cannot receive does not get `-32021`, because the broker answers
instead (§3.1); and a legacy `resources/subscribe` for a URI whose updates
cannot be delivered succeeds, with a warning event, because the legacy
revisions have no way to say "agreed, but nothing will come" (§4.3, #251).
