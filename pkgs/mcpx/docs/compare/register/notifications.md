# Subscriptions and notifications

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  subscriptions/listen and the listen-stream rules of 2026, list_changed delivery per revision, request-scoped notifications, and notification hygiene in mcpx and opencode
```

Until 2025-11-25 a server pushes change notifications (`…/list_changed`, `resources/updated` after
`resources/subscribe`) whenever it has a channel to the client. 2026-07-28 replaces that with one long-lived request,
`subscriptions/listen`, whose filter says what the client wants; every message on it is tagged with the listen request's
id, and progress and log messages stay on the stream of the request they belong to. mcpx implements the listen request on
stdio but gets the acknowledgement and the tagging wrong, answers it over HTTP with an empty 202, and, as a client, never
subscribes to anything upstream. Other notifications are registered with their feature: `notifications/cancelled` and
`notifications/progress` in [progress-cancellation.md](progress-cancellation.md), `notifications/message` in
[logging.md](logging.md), `notifications/roots/list_changed` in [roots.md](roots.md),
`notifications/elicitation/complete` in [elicitation.md](elicitation.md), `notifications/tasks/status` and
`notifications/tasks` in [tasks.md](tasks.md); `notifications/initialized` goes with lifecycle, and
`resources/subscribe`, `resources/updated` and mcpx's `mcpx://` URI rewriting (its R-7 and R-8) go with resources.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| SUB-01 | `subscriptions/listen`, a long-lived notification request | `2026-07-28 has` | `26-07 only` | partial — works on stdio; HTTP see SUB-12 | + high | M | med |
| SUB-02 | `SubscriptionFilter`: three list flags and `resourceSubscriptions[]` | `2026-07-28 has` | `26-07 only` (+ `taskIds` from the tasks extension) | ✓ — four fields mapped to daemon event kinds | + med | S | low |
| SUB-03 | Acknowledgement carries `notifications` and `_meta` subscription id | `2026-07-28 has` `mcpx missing` | `26-07 only` | ✗ — `params.subscriptionId`; no `notifications` field (#208) | + med | S | med |
| SUB-04 | Acknowledgement first; nothing on the subscription before it | `2026-07-28 has` `mcpx missing` | `26-07 only` | ✗ — listener starts before the ack is written (#208) | + low | S | low |
| SUB-05 | Every listen-stream message tagged with `_meta` subscription id | `2026-07-28 has` `specs conflict` `mcpx missing` | `26-07 only` | ✗ — list_changed and updated sent untagged (#208) | + med | S | med |
| SUB-06 | `list_changed` delivered only on an opted-in listen stream | `2024-11-05 has` `2026-07-28 has` | `24-11..25-11 unsolicited · 26-07 listen only` | ✓ — gated by the filter | + med | S | low |
| SUB-07 | Progress and log messages stay on their request's stream | `2026-07-28 has` | `25-03..25-11 SHOULD relate · 26-07 MUST relate, never on listen` | n/a — mcpx sends neither to hosts | + med | M | med |
| SUB-08 | Several concurrent subscriptions per client | `2026-07-28 has` `mcpx missing` | `26-07 only` | ✗ — a second listen replaces the first (#208) | + med | M | med |
| SUB-09 | Client ends a subscription: close SSE, or `notifications/cancelled` on stdio | `2026-07-28 has` `mcpx missing` | `26-07 only` | ✗ — stdio cancel is recorded; the stream continues (#208) | + med | S | med |
| SUB-10 | Graceful end: `SubscriptionsListenResult` with required subscription id | `2026-07-28 has` `mcpx missing` | `26-07 only` | ✗ — never sent; stdio exit ends silently (#208) | + low | S | low |
| SUB-11 | Server teardown: `notifications/cancelled` versus a final result | `2026-07-28 has` `specs conflict` `mcpx missing` | `26-07 only` | ✗ — neither sent; comment says the transport sends one (#208) | + low | S | low |
| SUB-12 | `subscriptions/listen` over Streamable HTTP | `2026-07-28 has` `mcpx missing` `2025-03-26 has` | `26-07 only` | ✗ — `202 Accepted`, empty body (wire W17) (#208) | + high | M | high |
| SUB-13 | `subscriptions/listen` from a legacy client | `2026-07-28 has` | `26-07 method` | acc. — accepted on stdio from any era | + low | S | low |
| SUB-14 | stdio reconnect: client re-sends listen; server keeps no state | `2026-07-28 has` | `26-07 only` | ✓ — subscription state dies with the stdio process | + low | S | low |
| SUB-15 | Client subscribes upstream for changes | `2026-07-28 has` `opencode v2 has` `mcpx missing` `2024-11-05 has` | spec: listen (26-07) or `resources/subscribe`; opencode v2 SDK listens | ✗ — never listens, never subscribes upstream (#200) | + med | M | med |
| SUB-16 | Client reaction to `list_changed` | `opencode v1 has` `opencode v2 has` | opencode v1 tools only; v2 tools, prompts, resources | ✓ — invalidates the upstream schema cache | + low | S | low |
| SUB-17 | Client notifications shrink to `notifications/cancelled` only | `2026-07-28 removes` | `24-11..25-11 four or five kinds · 26-07 one` | ✓ — sends no client progress or roots notifications | − moot | S | low |
| SUB-18 | A notification never gets a response | `mcpx missing` | every revision | ✗ — unknown notifications answered with `-32601` (wire S5) (#202) | + med | S | med |

## SUB-01 `subscriptions/listen`, a long-lived notification request

- **What.** A client request that opens a notification stream from the server, with a `notifications` filter. It replaces
  the legacy HTTP GET stream and `resources/subscribe`, and means the same on HTTP and stdio. Its response arrives only
  when the stream ends gracefully (SUB-10).
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** Implemented and accepted from any era (`internal/mcpserver/server.go:622-640`); the daemon notifier
  maps the filter to its event bus (`internal/cli/serve.go:726-763`). Without a notifier it answers `-32601` "this mcpx
  pushes no notifications". On stdio it works apart from SUB-03, SUB-05 and SUB-08..SUB-11; over HTTP it does not
  (SUB-12).
- **Value to mcpx.** + high: the only way a 2026 host learns that mcpx's resources or prompts changed.
- **Effort.** M — mostly the HTTP half.
- **Risk.** med: see the rows below.
- **Detail.** The daemon notifier sends only kinds the filter asked for, as the spec requires ("a server MUST NOT send
  what the client did not ask for", in the code's words).
- **Sources.** `schema/2026-07-28/schema.ts:1315` "subscriptions/listen";
  `2026-07-28/basic/patterns/subscriptions.mdx:7` "opens a long-lived notification stream from the server to the";
  `internal/mcpserver/server.go:622-640`; `internal/cli/serve.go:726-763`.

## SUB-02 `SubscriptionFilter`: three list flags and `resourceSubscriptions[]`

- **What.** `toolsListChanged`, `promptsListChanged`, `resourcesListChanged` (booleans) and `resourceSubscriptions`
  (URIs). All optional; omitting one means not subscribed; the server MUST NOT send types that were not requested.
- **Where.** 2026-07-28 only. The tasks extension adds `taskIds` ([tasks.md](tasks.md)).
- **mcpx @ 05c78b2.** All four map to daemon event kinds (`internal/cli/serve.go:726-763`; struct at
  `internal/mcpserver/server.go:77-80`). `resourceSubscriptions` URIs are stripped of `mcpx://<ns>/` to match upstream
  events; that loses the namespace, which the resources register covers.
- **Value to mcpx.** + med: done.
- **Effort.** S.
- **Risk.** low.
- **Detail.** `taskIds` is not accepted, consistent with mcpx having no extension tasks.
- **Sources.** `schema/2026-07-28/schema.ts:1270` "export interface SubscriptionFilter {";
  `2026-07-28/basic/patterns/subscriptions.mdx:49` "All fields are optional. Omitting a field is equivalent to not subscribing to that";
  `internal/cli/serve.go:726-763`.

## SUB-03 Acknowledgement carries `notifications` and `_meta` subscription id

- **What.** For each subscription the server MUST first send `notifications/subscriptions/acknowledged` with
  `params.notifications` set to the subset of the filter it will honour, and the listen request's id in
  `_meta["io.modelcontextprotocol/subscriptionId"]`.
- **Where.** 2026-07-28 only. The changelog says only that "the server acknowledges"; the notification is not named there.
- **mcpx @ 05c78b2.** Sends `params: {"subscriptionId": <id>}`: the id as a top-level param instead of the `_meta` key,
  and no `notifications` field (`internal/mcpserver/server.go:637-639`). Wire S4:
  `{"method":"notifications/subscriptions/acknowledged","params":{"subscriptionId":4}}`.
- **Value to mcpx.** + med: clients use the echoed filter to learn which kinds are unsupported, and the id to correlate.
- **Effort.** S.
- **Risk.** med: a schema-invalid notification; a strict client may never believe the stream is live.
- **Detail.** Unsupported kinds are simply omitted from the echoed filter. The 2026 `_meta` key registration is in the
  `_meta` register.
- **Sources.** `schema/2026-07-28/schema.ts:1399` "notifications/subscriptions/acknowledged";
  `schema/2026-07-28/schema.ts:1378` "notifications: SubscriptionFilter;";
  `2026-07-28/basic/patterns/subscriptions.mdx:61` "reflects the subset the server agreed to";
  `internal/mcpserver/server.go:638` "\"subscriptionId\": json.RawMessage(req.ID),"; wire S4.

## SUB-04 Acknowledgement first; nothing on the subscription before it

- **What.** No notification for a subscription may precede its acknowledgement. On stdio the ordering is per subscription
  id: messages of other subscriptions MAY come first.
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** `restartListen` starts the listener goroutine (`internal/mcpserver/server.go:1427-1444`) before the
  acknowledgement is pushed (`internal/mcpserver/server.go:636-639`), so a change arriving in that window can be written
  first.
- **Value to mcpx.** + low: a narrow race.
- **Effort.** S — push the ack, then start listening.
- **Risk.** low.
- **Detail.** Found as a code-order nuance, not observed on the wire.
- **Sources.** `2026-07-28/basic/patterns/subscriptions.mdx:56` "send any notification on the";
  `2026-07-28/basic/patterns/subscriptions.mdx:57`; `internal/mcpserver/server.go:636-639`.

## SUB-05 Every listen-stream message tagged with `_meta` subscription id

- **What.** Every notification on a listen stream (and the acknowledgement and the graceful-close result) MUST carry
  `_meta["io.modelcontextprotocol/subscriptionId"]` = the listen request's id; on stdio clients MUST use it to
  demultiplex.
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** list_changed and resources/updated go out with empty or `{uri}` params and no `_meta`
  (`internal/events/events.go:252-261`); the stdio push adds nothing (`internal/mcpserver/server.go:1010-1012`).
- **Value to mcpx.** + med: required for stdio hosts with more than one subscription.
- **Effort.** S — tag in the push closure of a listen, not in the event mapper.
- **Risk.** med: 2026 hosts cannot attribute notifications.
- **Detail.** The same notification builder serves legacy `resources/subscribe` listeners, which must not get the key.
  The 2026 prose examples of `notifications/tools/list_changed` and `…/prompts/list_changed` omit the tag themselves,
  contradicting the MUST.
- **Also recorded from the meta register.** The acknowledgement sends `{"subscriptionId": <id>}` as a top-level param
  with no `_meta` (`internal/mcpserver/server.go:637-639`; wire S4). `list_changed` and `resources/updated` are built
  without `_meta` (`internal/events/events.go:252-271`) and pushed as is (`internal/mcpserver/conn.go:78-85`). The
  client never opens a listen stream, so the client side is not exercised. The same notification builders serve legacy
  `resources/subscribe` clients, for whom the key must not be added. 2026's own prose example of
  `notifications/tools/list_changed` omits the key. The acknowledgement's missing `notifications` field and the rest
  of listen are in the notifications register.
- **Sources.** `schema/2026-07-28/schema.ts:133` "\"io.modelcontextprotocol/subscriptionId\"?: RequestId;";
  `2026-07-28/basic/index.mdx:412` "On notifications delivered via a"; `2026-07-28/basic/index.mdx:413`;
  `2026-07-28/server/tools.mdx:246` "\"method\": \"notifications/tools/list_changed\""; `2026-07-28/server/prompts.mdx:177`;
  `internal/events/events.go:252-261`.; `schema/2026-07-28/schema.ts:1334`; `internal/mcpserver/server.go:638` "\"subscriptionId\": json.RawMessage(req.ID),"; `internal/events/events.go:255`; wire S4.

## SUB-06 `list_changed` delivered only on an opted-in listen stream

- **What.** `notifications/{tools,prompts,resources}/list_changed` exist in every revision. Until 2025-11-25 a server that
  declared `listChanged` may send them unsolicited; in 2026 `listChanged: true` means the server sends them only on a listen
  stream whose filter asked.
- **Where.** All five; the delivery model changes in 2026-07-28.
- **mcpx @ 05c78b2.** For 2026 (listen) delivery is gated by the filter (`internal/cli/serve.go:728-736`). The legacy half
  is CAP-20.
- **Value to mcpx.** + med: done for 2026 on stdio.
- **Effort.** S.
- **Risk.** low: a 2026 host that never opens a listen never hears of changes, by design.
- **Detail.** The capability flags are unchanged in shape; only what they promise changed. The capabilities register owns
  the flags themselves.
- **Sources.** `schema/2024-11-05/schema.ts:681` "notifications/tools/list_changed";
  `schema/2025-11-25/schema.ts:1159` "This may be issued by servers without any previous subscription from the client.";
  `schema/2026-07-28/schema.ts:1888` "This is only delivered on a";
  `2026-07-28/server/tools.mdx:238` "When the list of available tools changes, servers that declared the";
  `internal/cli/serve.go:728-736`.

## SUB-07 Progress and log messages stay on their request's stream

- **What.** 2026: `notifications/progress` and `notifications/message` flow only on the response stream of the request
  they relate to (they MUST relate to it), before its response, and never on a listen stream, which carries only opted-in
  change notifications. Legacy servers could send notifications on a POST's stream that SHOULD relate to the request, or
  on the standalone GET stream.
- **Where.** SHOULD relate in 2025-03-26..2025-11-25; MUST relate, and listen-free, in 2026-07-28.
- **mcpx @ 05c78b2.** Sends neither progress nor log messages to hosts
  ([progress-cancellation.md](progress-cancellation.md), [logging.md](logging.md)).
- **Value to mcpx.** + med: the rule to follow when mcpx starts relaying upstream progress: route it to the host request
  that caused it, never to a shared bus or a listen stream.
- **Effort.** M — per-call correlation; on stdio all streams share one channel, so correlation is by `progressToken` or
  request context.
- **Risk.** med: cross-host leakage of progress or logs in a pooled proxy if done wrong.
- **Detail.** The same rule is stated three times in 2026: the changelog, the transport page, and the logging page. The
  tasks extension further forbids both on a task's listen stream ([tasks.md](tasks.md)).
- **Sources.** `2026-07-28/changelog.mdx:18` "Request-scoped notifications such as";
  `2026-07-28/basic/transports/streamable-http.mdx:115` "before the final response. These notifications **MUST** relate to the";
  `2025-03-26/basic/transports.mdx:112` "These messages **SHOULD** relate to the originating client";
  `2026-07-28/server/utilities/logging.mdx:69` "the server **MUST NOT** deliver it on a".

## SUB-08 Several concurrent subscriptions per client

- **What.** A client MAY hold several subscriptions at once (say, tools changes and resource updates), each identified by
  its own listen request id.
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** One stream per connection: a new listen stops the previous listener
  (`internal/mcpserver/server.go:1427-1444`). The comment calls replacement "the simplest faithful implementation", but the
  spec explicitly allows concurrency.
- **Value to mcpx.** + med: match the spec.
- **Effort.** M — per-subscription listeners, which also needs SUB-05's tagging to be useful.
- **Risk.** med: a host that opens two subscriptions silently loses the first.
- **Detail.** The previous listen request never gets its graceful-close result either (SUB-10).
- **Sources.** `2026-07-28/basic/patterns/subscriptions.mdx:109` "A client **MAY** have multiple active subscriptions concurrently";
  `internal/mcpserver/server.go:1425-1426` "the simplest faithful implementation"; `internal/mcpserver/server.go:1427-1444`.

## SUB-09 Client ends a subscription: close SSE, or `notifications/cancelled` on stdio

- **What.** On HTTP the client closes the listen's SSE stream; on stdio it sends `notifications/cancelled` naming the listen
  request id.
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** On stdio the cancel is only recorded (`internal/mcpserver/server.go:1521-1538`); the listener keeps
  running and the host keeps receiving. Over HTTP there is no listen stream to close (SUB-12).
- **Value to mcpx.** + med: a host that unsubscribes should stop receiving.
- **Effort.** S — when the cancelled id is the active listen, stop it.
- **Risk.** med: an unsubscribed host keeps getting notifications.
- **Detail.** The general inbound-cancel gap is in [progress-cancellation.md](progress-cancellation.md).
- **Sources.** `2026-07-28/basic/patterns/subscriptions.mdx:120-121`; `internal/mcpserver/server.go:1533`
  "c.cancelled[fmt.Sprint(p.RequestID)] = p.Reason".

## SUB-10 Graceful end: `SubscriptionsListenResult` with required subscription id

- **What.** When the server ends a subscription itself (shutdown, say), it SHOULD answer the original listen request with a
  `complete` result whose `_meta` carries the subscription id (required on this result type). A transport that closes
  without it is an unexpected disconnect, which the client MAY treat as a reason to reconnect.
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** Never sends it: the listen request gets no reply while open (`internal/mcpserver/server.go:622-640`),
  and `ServeStdio` stops the listener on exit without a message (`internal/mcpserver/server.go:1014`).
- **Value to mcpx.** + low: lets clients tell shutdown from failure.
- **Effort.** S.
- **Risk.** low: clients may reconnect-loop on a clean shutdown.
- **Detail.** `SubscriptionsListenResultMetaObject` makes the key required here, unlike on ordinary results.
- **Sources.** `schema/2026-07-28/schema.ts:1334` "\"io.modelcontextprotocol/subscriptionId\": RequestId;";
  `2026-07-28/basic/patterns/subscriptions.mdx:132` "with a completion result before closing the stream.";
  `2026-07-28/basic/patterns/subscriptions.mdx:158` "treat as a trigger to reconnect."; `internal/mcpserver/server.go:1014`.

## SUB-11 Server teardown: `notifications/cancelled` versus a final result

- **What.** Three 2026 texts disagree on how a server ends a subscription. The cancellation page: the server MUST send
  `notifications/cancelled` naming the listen id when it tears the subscription down, and for nothing else. The
  subscriptions page: it SHOULD send a successful listen result, then close. The schema: the server sends that cancel "On
  stdio" only.
- **Where.** 2026-07-28 only. Legacy servers could cancel any request they had issued.
- **mcpx @ 05c78b2.** Sends neither. The listen comment says "the transport sends the terminating notifications/cancelled
  when the client closes it" (`internal/mcpserver/server.go:624-626`); no code does, and the comment has the direction
  backwards (a client closing is a client cancel).
- **Value to mcpx.** + low: on a graceful stdio teardown, send both the cancel and the result, which satisfies all three
  texts.
- **Effort.** S.
- **Risk.** low: clients waiting for one signal see the other, or nothing.
- **Detail.** On HTTP it is unclear whether a `notifications/cancelled` should appear on a listen SSE stream that otherwise
  carries only opted-in kinds. Server-sent cancellations MUST reference a `subscriptions/listen` id.
- **Sources.** `2026-07-28/basic/patterns/cancellation.mdx:11` "A server **MUST** send";
  `2026-07-28/basic/patterns/subscriptions.mdx:122` "it **SHOULD** send a";
  `schema/2026-07-28/schema.ts:637` "On stdio, the server also sends this notification, solely to terminate a";
  `2026-07-28/basic/patterns/cancellation.mdx:71` "Server-sent cancellation notifications **MUST** reference a";
  `internal/mcpserver/server.go:624-626`.

## SUB-12 `subscriptions/listen` over Streamable HTTP

- **What.** A JSON-RPC request over HTTP MUST get either `application/json` or `text/event-stream`. For a listen that means
  an SSE stream carrying the acknowledgement and then the notifications.
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** An HTTP connection has no push function, so `restartListen` returns at once
  (`internal/mcpserver/server.go:1433-1437`), the acknowledgement goes nowhere, `handle` returns nil
  (`internal/mcpserver/server.go:640`), and `ServeHTTP` writes `202 Accepted` with no body
  (`internal/mcpserver/server.go:1188-1190`). Wire W17. `docs/protocol.md:84` presents listen as working in any era.
- **Value to mcpx.** + high: remote 2026 hosts can never hear of changes otherwise.
- **Effort.** M to stream (hold the POST open and push SSE; mcpx already streams SSE for legacy elicitation); S to refuse
  with an error.
- **Risk.** high: a 2026 client waits forever for an acknowledgement, or treats 202 as a transport error.
- **Detail.** The capabilities are truthfully `listChanged: false` over HTTP (wire W5), yet the method is "accepted".
- **Also recorded from the transports register.** Any request whose handler returns no response gets 202
  (`internal/mcpserver/server.go:1188-1190`). That happens to `subscriptions/listen` over HTTP: the HTTP connection
  has no push function, the listener returns early (`internal/mcpserver/server.go:1433-1437`), the acknowledgement
  goes nowhere and the handler returns nil (`internal/mcpserver/server.go:640`). Wire W17: `202 Accepted`, empty body.
  Listen semantics are in the notifications register; this row is the transport rule.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:88-90` "If the body is a JSON-RPC _request_, the server **MUST** return either";
  `internal/mcpserver/server.go:1188-1190`; `docs/protocol.md:84`; wire W17.; `2026-07-28/basic/transports/streamable-http.mdx:88` "6. If the body is a JSON-RPC _request_, the server **MUST** return either"

## SUB-13 `subscriptions/listen` from a legacy client

- **What.** A 2025-06-18 stdio client can open a 2026-style listen and get notifications.
- **Where.** The method is 2026-07-28's.
- **mcpx @ 05c78b2.** Accepted on purpose (`internal/mcpserver/server.go:622`; `docs/protocol.md:84`); wire S4 is a legacy
  client doing exactly this. It is not declared to legacy clients.
- **Value to mcpx.** + low: accept-liberally, harmless.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The acknowledgement a legacy client gets has the same wrong shape (SUB-03).
- **Sources.** `internal/mcpserver/server.go:622`; `docs/protocol.md:84`; wire S4.

## SUB-14 stdio reconnect: client re-sends listen; server keeps no state

- **What.** On stdio, after the connection is re-established the client MUST send `subscriptions/listen` again; the server
  holds no subscription state across reconnections.
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** As a server, subscription state lives on the stdio connection and dies with the process
  (`internal/mcpserver/server.go:1014`). As a client it never listens, so it has nothing to re-send (SUB-15).
- **Value to mcpx.** + low: nothing to change server-side.
- **Effort.** S.
- **Risk.** low.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/patterns/subscriptions.mdx:161` "client **MUST** re-send";
  `2026-07-28/basic/patterns/subscriptions.mdx:162` "the server holds no subscription state across reconnections.".

## SUB-15 Client subscribes upstream for changes

- **What.** To learn about upstream changes a client opens `subscriptions/listen` to a 2026 server (the only way to get
  list_changed there) or calls `resources/subscribe` on a legacy one; unsolicited legacy list_changed needs no request.
- **Where.** 2026 listen; legacy `resources/subscribe`. opencode v2's SDK opens the listen itself on modern connections.
- **mcpx @ 05c78b2.** The pool never opens a listen to a modern upstream and never calls `resources/subscribe` on a legacy
  one; `Client.SubscribeResource` exists and nothing calls it (`internal/mcpclient/client.go:536-543`). Its comment defers
  modern servers to "the listening stream", which the client never opens (`internal/mcpclient/client.go:532-535`). Cache
  invalidation therefore depends on unsolicited legacy notifications (`internal/daemon/hooks.go:37-51`).
- **Value to mcpx.** + med: fresh catalogues from modern and remote upstreams.
- **Effort.** M.
- **Risk.** med: stale tool lists from modern upstreams until `mcpx refresh`.
- **Detail.** A modern listen would also have to be re-opened after an upstream stdio restart (SUB-14's MUST applies to
  mcpx as a client).
- **Also recorded from the resources register.** Every revision (by one mechanism or the other).
  `Client.SubscribeResource` exists (`internal/mcpclient/client.go:536-543`) and nothing calls it; the pool never
  opens `subscriptions/listen` to a modern upstream. A host's subscription starts a listener on the daemon's event
  bus, which sees `resources/updated` only if an upstream sends it unasked. `docs/protocol.md:83` says subscribe
  "forwards from the same bus". `SubscribeResource` is a deliberate no-op for modern upstreams, deferring to "the
  listening stream", which does not exist (notifications register).
- **Sources.** `internal/mcpclient/client.go:532-535` "that is handled by";
  `internal/mcpclient/client.go:536-543`; `internal/daemon/hooks.go:37-51`;
  `v2:packages/core/src/mcp/client.ts:132` "the SDK opens the subscriptions/listen stream behind these itself.".; `internal/mcpclient/client.go:536` "func (c *Client) SubscribeResource(ctx context.Context, uri string) error {"; `docs/protocol.md:83`

## SUB-16 Client reaction to `list_changed`

- **What.** What a client does when told a list changed.
- **Where.** opencode v1 handles `tools/list_changed` only, and only when the server declares `tools`: it re-lists tools
  and publishes `ToolsChanged`. opencode v2 re-lists tools (reconcile debounced by 100 ms), refreshes prompts (republished
  as slash commands) and publishes `ResourcesChanged`.
- **mcpx @ 05c78b2.** As a client, list_changed invalidates the upstream schema cache (`internal/daemon/hooks.go:37-51`;
  `internal/mcpclient/client.go:495-500`). As a server, its own tool list is fixed once built, so a host refreshing mcpx's
  tools gains nothing; prompts and resources are where changes would show (CAP-20).
- **Value to mcpx.** + low: v2 hosts would show mcpx prompt changes live if mcpx sent them.
- **Effort.** S.
- **Risk.** low.
- **Detail.** v1 fetches prompts fresh on every command listing, so ignoring `prompts/list_changed` costs it little.
- **Sources.** `v1:packages/opencode/src/mcp/index.ts:462`; `v1:packages/opencode/src/mcp/index.ts:461`;
  `v2:packages/core/src/tool/mcp.ts:140`; `v2:packages/core/src/mcp/index.ts:374`; `v2:packages/core/src/mcp/index.ts:375`;
  `internal/daemon/hooks.go:37-51`.

## SUB-17 Client notifications shrink to `notifications/cancelled` only

- **What.** 2026 clients can no longer send `notifications/initialized`, `notifications/progress`,
  `notifications/roots/list_changed` or `notifications/tasks/status`; `ClientNotification` is `CancelledNotification`
  alone.
- **Where.** Legacy client notifications: cancelled, progress, initialized, roots/list_changed (+ tasks/status in
  2025-11-25).
- **mcpx @ 05c78b2.** As a client it sends no progress and declares roots without `listChanged`, so only `initialized`
  (legacy) and `cancelled` go upstream.
- **Value to mcpx.** − moot.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Client-to-server progress vanishes because server-initiated requests vanish; the changelog never says so.
  Over 2026 HTTP not even `cancelled` is defined (see [progress-cancellation.md](progress-cancellation.md)).
- **Sources.** `schema/2025-11-25/schema.ts:2525` "export type ClientNotification =";
  `schema/2026-07-28/schema.ts:3166` "export type ClientNotification = CancelledNotification;".

## SUB-18 A notification never gets a response

- **What.** In every revision the receiver of a notification MUST NOT reply.
- **Where.** Every revision (JSON-RPC and MCP both).
- **mcpx @ 05c78b2.** Any notification other than `notifications/initialized`, `initialized` and `notifications/cancelled`
  falls through to `fail(-32601, "no method …")` (`internal/mcpserver/server.go:766`), and the stdio loop writes it
  (`internal/mcpserver/server.go:1063-1066`); over HTTP it goes out as 200 with an error body instead of 202. Wire S5:
  `notifications/roots/list_changed` and `notifications/progress` each got an id-less `-32601`.
- **Value to mcpx.** + med: protocol hygiene; legacy hosts that declare `roots.listChanged` trigger it on every change.
- **Effort.** S — return nothing when the message has no id; an unknown *request* must still get `-32601`.
- **Risk.** med: hosts log or fail on unexpected error frames.
- **Detail.** A send-conservatively conflict. Wire W26 shows the known case working: `notifications/cancelled` over HTTP
  gets 202.
- **Sources.** `2025-03-26/basic/index.mdx:78` "The receiver **MUST NOT** send a response.";
  `2026-07-28/basic/index.mdx:160` "The receiver **MUST NOT** send a response.";
  `internal/mcpserver/server.go:766` "return fail(codeMethodNotFound, \"no method \"+req.Method)";
  `internal/mcpserver/server.go:1063-1066`; wire S5, W26.
