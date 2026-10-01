# What the specification asks of a client

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T16:00:00-05:00
increment:    2
status:       standard
tags:         area:protocol, area:spec
description:  per revision, what an MCP client MUST, SHOULD and MAY do,
              including the obligations the prose implies but never states.
```

This is the counterpart to [`server-obligations.md`](server-obligations.md)
and follows the same conventions. It is about the specification. mcpx is a
client of every upstream server it runs, and its client behaviour is in
[`../protocol.md`](../protocol.md) §4. Comparisons with other clients belong
in [`../compare/`](../compare/).

"Legacy" means 2024-11-05 through 2025-11-25, and "modern" means 2026-07-28.
Several obligations below attach to the *host*, the application that shows
things to a user. The spec addresses these to clients. A proxy is a client
with no user of its own, so for each of them it has to decide whether it meets
the obligation itself or passes it downstream.

---

## 1. Lifecycle

Sources: [2025-11-25 lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle), [2026-07-28 basic](https://modelcontextprotocol.io/specification/2026-07-28/basic), [2026-07-28 versioning](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning).

### Legacy

- **`initialize` comes first.** A client **MUST** send `initialize` before
  anything else, offering the latest version it supports.
- **Check the answer.** If the server answers with a version the client
  cannot speak, the client **SHOULD** disconnect. The schema's doc comment
  says **MUST**. A client that accepts whatever comes back is running a
  protocol it never agreed to.
- **Then `notifications/initialized`.** After a successful result the client
  **MUST** send `notifications/initialized`. Before the server responds it
  **SHOULD NOT** send requests other than pings.
- **Declare what it can do.** The client declares `roots`, `sampling` and,
  from 2025-06-18, `elicitation`. What it declares is what the server may ask
  of it.
- **Never cancel `initialize`.** `notifications/cancelled` for `initialize` is
  forbidden in every legacy revision, including after the request times out.
  The client closes the connection instead.
- **stdio shutdown.** Close stdin, wait, then SIGTERM, then SIGKILL. In 2026
  this rule moved to the stdio transport page.

### Modern (2026-07-28)

- **Every request carries `_meta`.** Every request **MUST** carry
  `_meta["io.modelcontextprotocol/protocolVersion"]` and `clientCapabilities`.
  The client capabilities apply to *that request only*. An empty object means
  none.
- **`clientInfo`.** A client **SHOULD** send `clientInfo`.
- **`resultType`.** An absent `resultType` means `"complete"`. An unknown
  value **MUST** be treated as invalid, never as complete.
- **`-32022` (unsupported version).** The client **SHOULD** pick a version
  from `data.supported` and retry.

Declaring capabilities per request has an effect that shows up in
multiplexing clients. The client can widen or narrow what it offers one call
at a time. For example, it can declare `elicitation` only on calls made while
a user is present. The server cannot hold it to an earlier declaration.

---

## 2. Finding the era

Source: [2026-07-28 versioning, backward compatibility](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions).

The 2026 versioning page describes the probe. The result is "a property of the
server". A client **SHOULD** cache it for the life of the process (stdio) or
the origin (HTTP), and **MAY** persist it across restarts of the same
configuration.

**stdio.** Send `server/discover` first (**SHOULD**). If the answer is
anything other than a recognised modern result or modern error, fall back to
`initialize`, and that includes a timeout. The fallback **MUST NOT** be keyed
to one error code, because legacy servers answer an unknown method in
different ways.

**HTTP.** A dual-era client tries the steps in order:

1. POST a modern request with every header. Success means the server is
   modern.
2. A 400, 404 or 405 whose body is a JSON-RPC error with `-32020`, `-32021`,
   `-32022`, or `-32601` together with 404, also means modern. Fix the request
   or pick a version, and retry.
3. Anything else: POST `initialize`, which gives legacy Streamable HTTP.
4. If that fails with 400, 404 or 405 (the 2025-11-25 rule), GET the URL and
   wait for an `endpoint` event, which gives HTTP+SSE (2024-11-05).

A server that returns a bare-text 400 is therefore classified as legacy. That
is the server's fault, but the client can only act on what it received.

**A pool of processes of one configuration shares one era answer.** The cache
key is the configuration, not the process. A freshly spawned legacy process
still needs its own `initialize`, because the session is per process. A
modern one needs nothing. If a pooled process answers a different era from its
siblings, the binary changed, and the cached answer has to be dropped and the
probe run again.

---

## 3. Transports

Sources: [2024-11-05 transports](https://modelcontextprotocol.io/specification/2024-11-05/basic/transports), [2025-11-25 transports](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports), [2026-07-28 stdio](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio), [2026-07-28 Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http), [2025-11-25 authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).

### stdio

- The client launches the server as a subprocess.
- It **MUST NOT** write anything to stdin that is not an MCP message.
- It **SHOULD NOT** treat stderr output as an error (2025-11-25 and later).
- In 2026 it **SHOULD** restart a server that exited and retry.

### HTTP+SSE (2024-11-05)

- The client opens the SSE stream first and POSTs to the URL from the
  `endpoint` event.
- A backwards-compatible client uses this transport only as the last fallback
  (§2).

### Streamable HTTP, legacy (2025-03-26 .. 2025-11-25)

- **Accept header.** POST with `Accept: application/json, text/event-stream`,
  and handle both kinds of response.
- **Session id.** Echo `Mcp-Session-Id` on every request once it has been
  assigned. On a 404 for a request that carried a session id, **MUST** start
  a new session with `initialize`. The client **SHOULD** DELETE the session
  when it is done.
- **Version header (2025-06-18 and later).** Send
  `MCP-Protocol-Version: <negotiated version>` on every request after
  `initialize`. This is the version the server *answered*, not the one the
  client prefers. A constant header is wrong whenever negotiation settled
  lower.
- **Disconnects.** A disconnect is not a cancellation. To cancel, send
  `notifications/cancelled` on a new POST.
- **Server-initiated messages.** The client MAY open a GET stream for them,
  and MAY resume with `Last-Event-ID`.

### Streamable HTTP, modern (2026-07-28)

- **Headers.** Send `MCP-Protocol-Version` from the request's own `_meta`, plus
  `Mcp-Method`, `Mcp-Name`, and `Mcp-Param-*` for every tool parameter the
  server annotated with `x-mcp-header`.
  - A tool whose `x-mcp-header` annotations are invalid **MUST** be excluded
    from the client's tool list.
  - A proxy that re-exports such a tool takes on both sides: it sends these
    headers upstream, and it validates them from its own clients.
- **No sessions or background streams.** There is no session, no GET stream,
  and no resumption. A broken request is re-issued under a **new id**. That
  MUST appears only in the 2026 changelog.
- **Cancellation.** Closing the stream is cancellation.

### Authorization (HTTP, 2025-03-26 and later)

A client that authenticates:

- follows the OAuth flow the server's metadata describes (PKCE is REQUIRED);
- sends `Authorization: Bearer` on every request, **never** as a URL query
  parameter;
- uses a token only for the resource it was issued for (the `resource`
  parameter from 2025-06-18 on).

On stdio the client passes credentials through the environment. A client
that sends static configured headers meets "never in the query" only if none
of its auth modes puts a token in the URL. mcpx's `query` auth type does,
and since #240 an `auth` block is applied, so `query` sends one when declared.

---

## 4. Messages and utilities

Sources: [2025-11-25 utilities](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation) (cancellation, progress, ping), [2026-07-28 cancellation](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/cancellation), [subscriptions](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/subscriptions), [caching](https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/caching), [tasks extension](https://modelcontextprotocol.io/extensions/tasks/overview).

- **Ids.**
  - Request ids are unique within a session (legacy).
  - A client answering server-to-client requests keeps its own id space for
    them.
  - A proxy that re-issues upstream questions downstream must never let the
    two id spaces meet.
- **Timeouts.**
  - A client **SHOULD** set a timeout on every request, and **SHOULD** cancel
    upstream when one fires.
  - It MAY reset the timer on progress, but **SHOULD** still enforce a
    maximum.
  - A proxy sits between two timeouts. Its upstream timeout has to be shorter
    than its downstream client's, or the client gives up first and the
    upstream work runs on with no observer.
  - A `subscriptions/listen` request is an implied exception, because it
    normally never gets a response.
- **Cancellation.** A client **MUST NOT** cancel `initialize`. After it
  cancels a request it ignores any response that still arrives.
- **Progress.** A `progressToken` **MUST** be unique among active requests.
  In 2025-11-25, a token given to a task stays valid until the task is
  terminal. That can be longer than the connection that carried the original
  request.
- **Pagination.**
  - Cursors are opaque. The client **MUST NOT** parse or persist them across
    sessions.
  - In 2026 an empty-string `nextCursor` is **not** the end of the list. Only
    an absent `nextCursor` ends it.
- **Ping (legacy).** A client **MUST** answer the server's pings. 2026 has no
  `ping`, so the health probe for a modern upstream is a cheap real request,
  and `server/discover` is cacheable and made for it.
- **Logging.**
  - Legacy: `logging/setLevel` is per session.
  - 2026: the level is per request, in `_meta`. A client that wants logs asks
    on each call.
- **Caching (2026).**
  - A client honours `ttlMs` and `cacheScope`.
  - It **MUST NOT** share a `private` result across authorization contexts.
  - Every page of one list carries the same `cacheScope`.
  - An intermediary cache is bound by the same rules.
- **Subscriptions (2026).**
  - `list_changed` and `resources/updated` arrive only on a
    `subscriptions/listen` stream whose filter asked for them. A client that
    never listens never hears about changes.
  - A client **SHOULD** check the filter in the acknowledgement against the
    one it requested.
  - A client may open several listens, and tells them apart by
    `_meta.subscriptionId`, which equals the listen request's id.
  - Teardown arrives as a result or as a stdio `notifications/cancelled`. The
    spec disagrees with itself on which
    ([conflicts C-21](../compare/conflicts.md#b-one-revision-against-itself)),
    so a client accepts both.
- **Tasks.**
  - A client that creates a task **SHOULD** store the task id durably. The
    handle outlives the request that created it and the connection it was
    created on.
  - The client polls with `tasks/get`, at no faster than `pollInterval`.
  - With the 2026 extension, a client receives a task only if it declared the
    extension. It has to handle `resultType: "task"` on any request where it
    declared the extension.

---

## 5. Features the client provides

Sources: [2025-11-25 roots](https://modelcontextprotocol.io/specification/2025-11-25/client/roots), [sampling](https://modelcontextprotocol.io/specification/2025-11-25/client/sampling), [elicitation](https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation); [2026-07-28 MRTR](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/mrtr).

### Roots

- **Declaring.** Declare `roots`, with `listChanged` only if the client will
  send `notifications/roots/list_changed`.
- **Roots themselves.** Roots **MUST** be `file://` URIs. The client
  **SHOULD** validate them against path traversal and ask the user before
  exposing them.
- **2026.** Roots are requested through MRTR only. There is no change signal
  and no session, so a server caches roots per request, or per
  `requestState`.

### Sampling

- **Human in the loop.** A client **SHOULD** let a human review and edit the
  request and the completion, and deny either. A host that auto-approves
  makes that choice for the user and should say so.
- **Model preferences.** `modelPreferences` are advisory, and hints are
  matched in order.
- **Tools and context (2025-11-25 and later).**
  - Declaring `sampling.tools` promises to run tool loops. A client must not
    declare it unless every path that can answer supports them.
  - A server **MUST NOT** send `tools` or `toolChoice` to a client that did
    not declare `sampling.tools`.
  - `includeContext` other than `"none"` is soft-deprecated, and needs
    `sampling.context`.
- **2026.**
  - Sampling is deprecated, but it is fully functional during the deprecation
    window.
  - The request travels through MRTR.
  - The client **SHOULD NOT** retain sampling messages between requests.

### Elicitation (2025-06-18 and later)

- **Form mode.**
  - An empty `elicitation: {}` means form mode only.
  - The requested schema is flat, made of primitives only.
  - The client **MUST** let the user review and change a response before it
    is sent, and can decline or cancel.
  - The client **MUST** show which server is asking.
  - It **MUST NOT** ask for sensitive information through a form (2025-11-25
    and later).
- **URL mode (2025-11-25 and later)** requires `elicitation.url`.
  - A client that did not declare it and receives a URL request **MUST**
    answer `-32602`.
  - A client that did declare it **MUST** show the full URL with the domain
    highlighted, **MUST NOT** open it without explicit consent, and **MUST
    NOT** prefetch it.
  - URL mode exists precisely so the client never sees the secret.
  - `-32042` and `notifications/elicitation/complete` exist only in
    2025-11-25. The notification goes only to the client that received the
    elicitation.
- **Binding the answer to a user.** Elicitation state **MUST NOT** be bound to
  session ids alone (2025-11-25), so the elicited authorization has to reach
  the user who started the call.
- **2026.**
  - Elicitation travels through MRTR only.
  - `elicitationId`, the completion notification, `-32042`, the session-id
    rule and client rate limiting are all gone.

### What a proxy owes, in addition

A proxy answers these requests for upstream servers by asking its own
clients. The spec does not address proxies here, but the rules above force
the following:

- **Check the capability per hop.** Before forwarding a question downstream,
  the proxy checks that *that* downstream declared the capability, **on the
  request being answered** (2026), and down to the mode: `elicitation.url`,
  `sampling.tools` and `sampling.context` each need their own check.
- **Scope `includeContext`.** Through a proxy, `"thisServer"` means every
  aggregated server. The proxy forwards `"none"` unless it can scope the
  context to the originating upstream.
- **Translate between eras.**
  - Legacy upstream to modern downstream: keep the upstream's wire request
    open while the downstream receives `input_required`.
  - Modern upstream to legacy downstream: ask each input request on the wire,
    then retry upstream with the upstream's own `requestState`, byte for
    byte.
  - Never relay `-32042` to a 2026 client. Convert it into a URL-mode input
    request.
  - Strip `elicitationId` for 2026 clients, and synthesise one for 2025-11-25
    clients.
- **Always answer upstream.** If a downstream never retries, a legacy
  upstream request stays pending forever. The proxy has to answer it with an
  error or `cancel`.
- **Attribute the question and leave it intact.** Name the originating
  upstream. Leave `url`, `requestedSchema`, hint order and `maxTokens`
  untouched.
- **Pooling breaks user binding.** An elicited third-party authorization is
  bound to the proxy's client identity. With a shared pooled upstream, one
  user's URL completion becomes visible to another user's calls. Per-user
  scoping is the only remedy.

---

## 6. Where mcpx stands

What mcpx does as a client is [`client.md`](client.md) and
[`era-probe.md`](era-probe.md), summarised in [`../protocol.md`](../protocol.md)
§4; the per-requirement status, each row with the test that verifies it, is
[`conformance-matrix.md`](conformance-matrix.md). Of the gaps the first draft
of this page listed, the discover field names, the unchecked `initialize`
answer, the `-32022` retry, the unknown-`resultType` fallback, the constant
version header, the missing `Mcp-Method`/`Mcp-Name`/`Mcp-Param-*` headers, the
HTTP+SSE fallback, the cancelled `initialize`, paging that stopped at an empty
cursor, undeclared url-mode elicitation and `elicitationId` reaching 2026
clients were fixed by #224, #232 and #239. What the rules above still expose:

- No OAuth (#253). The per-server `auth` block is applied (#240); `type: oauth`
  is refused before connecting.
- `sampling` and `elicitation.url` are declared whenever a handler is
  installed, and the daemon always installs one; `roots` is declared and
  always answered with an empty list (#210).
- `sampling.tools` and `sampling.context` are not checked per hop when a
  question is forwarded (#210, #256).
- No `progressToken` is sent upstream, so progress cannot extend a timeout
  (#212).
- A legacy HTTP session answered `404` is an error, not a reason to send
  `initialize` again (#203). (stdio shutdown now closes stdin, waits, then
  SIGTERM, then SIGKILL, as the lifecycle page asks — #284.)
