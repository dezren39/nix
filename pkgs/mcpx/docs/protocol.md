# Protocol: every revision, both directions

```
created:      2026-09-29T20:00:00-05:00
last-updated: 2026-09-29T20:00:00-05:00
increment:    1
status:       standard
tags:         area:protocol
description:  what each revision defines, what mcpx accepts, what it sends,
              and why there is one HTTP server rather than two.
```

mcpx speaks four revisions of MCP and sits in the middle of two of everything:
two eras, two directions, two mechanisms for the same question. The rules that
hold it together are short, and this document is mostly the table that falls
out of them.

**Accept liberally, send conservatively.** A server implementing a method
beyond what the negotiated revision defines is not violating anything — it
withholds nothing the revision requires, and a client that reached for the
method would otherwise be told mcpx cannot do a thing it can. Sending is the
opposite: a field, a notification or a server-initiated request that the peer
did not negotiate *and* did not declare is a lie about what was agreed, and
the failure is silent. Either the peer ignores it, or it rejects the frame,
and nothing says which.

So mcpx accepts `tasks/*` from a 2025-06-18 client, `subscriptions/listen`
from a legacy one and `resources/subscribe` from a modern one — and it will
not put `structuredContent` in front of a 2025-03-26 client, will not stamp
`resultType` on a reply to anything but 2026-07-28, and will never send
`elicitation/create` to a client that did not declare `elicitation`.

---

## 1. The revisions

| | era | handshake | version carried | mcpx serves | mcpx speaks upstream |
| --- | --- | --- | --- | --- | --- |
| `2025-03-26` | legacy | `initialize` | once | yes | on request (`protocol: force-legacy`) |
| `2025-06-18` | legacy | `initialize` | once | yes | on request |
| `2025-11-25` | legacy | `initialize` | once | yes | **default** |
| `2026-07-28` | modern | none | `_meta`, every request | yes | on fallback or `protocol: modern` |

The split is the thing everything else follows from. A legacy implementation
negotiates once and can send its peer a request at any time afterwards. A
modern one has no handshake, restates its version and capabilities on every
request, and — as a server — has no channel at all on which to send a request
of its own. The matrix is unforgiving: modern against legacy fails, legacy
against modern fails, only a dual-era implementation bridges them.

The schemas for `2025-06-18`, `2025-11-25` and `2026-07-28` were read
directly. **`2025-03-26` was not among them**, and what this document says
about it is from the specification's own change notes: it added audio
content, tool annotations and the `completions` capability, and it has none of
`structuredContent`, `resource_link`, `elicitation` or `tasks`. mcpx treats it
as the floor — the oldest revision it serves, and what a request that declared
nothing at all is assumed to be, because every shape that revision defines is
understood by everything newer. Guessing downward is recoverable; guessing
upward hands a peer a frame it cannot parse.

`internal/mcpserver/revisions.go` holds the floors and ceilings as data, and
`GET /v1/protocol` serves them. A matrix in a document and a matrix in code
agree on the day they are written and never again, so the document defers to
the endpoint.

---

## 2. mcpx as a server

What a host connected to mcpx gets.

### 2.1 Methods

| method | defined in | mcpx accepts | mcpx sends / answers |
| --- | --- | --- | --- |
| `initialize` | legacy only | any legacy version; a **modern** version over `initialize` is refused `-32022` | capabilities for the revision agreed |
| `server/discover` | 2026-07-28 | always | the full `Supported` list, newest first |
| `ping` | legacy (dropped from the 2026-07-28 schema) | always | `{}` |
| `tools/list` | all | always, paginated | opaque cursor, `mcp.pageSize` per page |
| `tools/call` | all | always | see §3 |
| `prompts/list`, `prompts/get` | all | always | §3 |
| `resources/list`, `resources/read`, `resources/templates/list` | all | always | §3 |
| `resources/subscribe`, `resources/unsubscribe` | legacy only | **any** era | forwards from the same bus `subscriptions/listen` reads |
| `subscriptions/listen` | 2026-07-28 | **any** era | only the four notification kinds the filter names |
| `completion/complete` | all | always | from mcpx's own names |
| `logging/setLevel` | legacy only | **any** era | accepted; mcpx emits no `notifications/message` of its own |
| `tasks/get`, `tasks/list`, `tasks/result`, `tasks/cancel` | 2025-11-25 core, 2026-07-28 extension | **any** era | handles and results |
| `notifications/cancelled` | all | always | recorded; mcpx cannot yet interrupt an upstream call mid-flight |

Three rows are the "accept liberally" rule doing visible work:
`resources/subscribe` from a modern client, `subscriptions/listen` from a
legacy one, and `tasks/*` from anything. mcpx answers all of them. It
**declares** none of them outside the revision that defines them, because a
declaration is a promise about the revision in force.

### 2.2 Capabilities mcpx declares

| capability | 2025-03-26 | 2025-06-18 | 2025-11-25 | 2026-07-28 |
| --- | --- | --- | --- | --- |
| `tools.listChanged` | ✓ | ✓ | ✓ | ✓ |
| `resources.subscribe` / `.listChanged` | ✓ | ✓ | ✓ | ✓ |
| `prompts.listChanged` | ✓ | ✓ | ✓ | ✓ |
| `completions` | ✓ | ✓ | ✓ | ✓ |
| `logging` | ✓ | ✓ | ✓ | — removed in 2026-07-28 |
| `tasks` (core shape) | — | — | ✓ | ✓ |
| `extensions["io.modelcontextprotocol/tasks"]` | — | — | ✓ | ✓ |

The push-dependent ones are declared only when a notifier is behind them.
Claiming `listChanged` with nothing to push invites a client to wait for
notifications that will never arrive, which is worse than not offering it.

### 2.3 Result shapes, and what is downgraded

Everything is built in the newest shape and spelled down once, at the edge, in
`downgrade()`. Building several shapes and choosing between them is how the
shapes drift apart.

| carried | defined from | to an older client |
| --- | --- | --- |
| `structuredContent` | 2025-06-18 | removed, and rendered into the `content` array as text — the data survives, in a vocabulary the client has |
| `resource_link` block | 2025-06-18 | becomes an embedded `resource`, which keeps the URI machine-readable where text would not |
| `audio` block | 2025-03-26 | always available; mcpx serves nothing older |
| `resultType` | 2026-07-28 | removed — sending it says mcpx is speaking a revision it is not |
| `inputRequests` / `requestState` | 2026-07-28 | never sent; a legacy client is asked on the wire instead |

A revision that defines all of them pays nothing: the function returns the
result untouched. An older one gets a copy, never an edit, because a task's
stored result is handed out more than once and rewriting it in place would
downgrade it permanently for whoever reads it next.

---

## 3. A server asks a question

This is the part with two mechanisms for one thing, and the reason the rest of
the design is shaped the way it is.

When mcpx proxies a call and the upstream server elicits or samples, the
question becomes a row in the broker: an identity, a deadline, a routing
decision (`docs/elicitation.md`). That has always worked and still does. What
it could not do is ask **mcpx's own client** — the agent or editor that made
the call, which is very often the only thing that knows the answer.

It does now, native-first and broker-backed. The broker remains the store and
the fallback; the client is asked first when it said it could answer.

### 3.1 Which client gets asked

| the client declared | version | what happens |
| --- | --- | --- |
| nothing | any | broker only — current behaviour, unchanged |
| `elicitation` | 2025-03-26 | broker only; that revision has no elicitation |
| `elicitation` | 2025-06-18 | asked, form mode only, `mode` and `elicitationId` stripped |
| `elicitation.form` | 2025-11-25, 2026-07-28 | asked in form mode; a url-mode question stays with the broker |
| `elicitation.url` | 2025-11-25, 2026-07-28 | asked in either mode |
| `sampling` | any | asked for `sampling/createMessage` only |

Declaring one is not declaring the other. A sampling-only client is never sent
an elicitation, and vice versa. Anything mcpx may not send this client is left
with the broker and its default audience, so nothing is lost by a client's
narrowness — it just does not get to answer.

The message is rewritten to name the originator: *"github (via mcpx) asks:
which repository?"*. A client asked "are you sure?" with no idea who is asking
cannot answer it.

### 3.2 Legacy: a request on the wire

The server sends `elicitation/create` while the client's `tools/call` is still
open, and waits.

**Over stdio** this is straightforward — the pipe is the session, it stays
open for the life of the process, and mcpx's own receive loop already had to
learn to route inbound frames by shape rather than by id. Requests that may
stop to ask something run on their own goroutine; handling them in line would
mean waiting for a frame that arrives on the very loop that is blocked.

**Over Streamable HTTP** it is not, and this is the part worth recording. The
POST is the only channel, and it is the thing waiting for the answer. So:

1. mcpx issues `Mcp-Session-Id` on `initialize` (and on `server/discover`,
   since a modern client never sends `initialize`).
2. A request that has to ask turns its POST response into an event stream —
   lazily, so a request that asks nothing still gets a plain JSON object — and
   writes `elicitation/create` as an SSE frame.
3. The client answers with a **separate POST** carrying the same session
   header, whose body is a JSON-RPC response. mcpx routes it to the request
   waiting on that session and answers `202`.
4. The original stream carries the final result and closes.

That works, and is tested end to end. It is also the reason `Conn` exists: a
subscription filter, a push function and a client's declared capabilities used
to live on the `Server`, which was correct while stdio was the only transport
and wrong the moment one server answered several HTTP clients — those fields
decide *whose* client gets asked a question.

Server ids are negative on the wire. JSON-RPC only requires uniqueness within
a direction, but several clients key one table by id, and they would otherwise
see mcpx's request answer their own.

### 3.3 Modern: a result the client retries

A 2026-07-28 server has no connection to send a request on. It answers
`resultType: "input_required"` with a map of `inputRequests` and an opaque
`requestState`; the client answers each and sends the **same request again**
with `inputResponses` and the state attached.

Which means the call has already returned by the time the answer exists. The
upstream call therefore cannot be tied to the client's request — if it were,
there would be nothing left to resume. It runs as a daemon task
(`internal/tasks`, the same store `/v1/call`'s task option uses, collectable
from the same `/v1/tasks/{id}`), and questions are correlated to it.

**`requestState` is signed.** The specification says the client must treat it
as opaque and says nothing about what a server should put in it, and that
silence is the risk: a bare call id is opaque to a well-behaved client and a
handle anyone can guess to a hostile one, and resuming someone else's call is
reading their tool results. So it is `base64(payload).HMAC-SHA256`, with a key
generated per daemon process and never persisted, carrying the call id, the
connection it was issued to, and an expiry. A token that verifies but names
another connection is refused with a message that says so — the difference
between a confused client and somebody replaying matters to whoever reads the
log.

A connection with no identity that outlives one request — a stateless HTTP
POST from a client that ignored the session header — is issued **no** token at
all, and its questions stay with the broker. A token bound to nothing is a
token anyone may present.

### 3.4 Correlation: which call asked?

A question arrives on a *connection*, not on a call. The server sends
`elicitation/create` down the same pipe it is answering `tools/call` on, and
nothing in the frame says which call it belongs to.

What mcpx knows is that one pooled instance serves one scope key. So a
question arriving on `(server, key)` belongs to whatever is calling on
`(server, key)`, and the pool now tells the handler which key the connection
serves.

When two calls share a key — which a `shared` server allows — the attribution
is genuinely ambiguous. mcpx makes none, and the question goes to the broker.
Guessing would hand one client's credential prompt to another client, and that
is not a trade worth making for a convenience.

### 3.5 Bounds

- `proto.askTimeout` (10m) — how long one client request may be held. A legacy
  client is blocked for all of it, so it has to sit inside whatever that
  client's own timeout is. A modern one is not blocked at all.
- `proto.askRounds` (8) — how many times one request may come back asking. A
  server that never stops asking is broken or adversarial.
- `proto.stateTTL` (30m) — how long a `requestState` may be resumed with.
- `proto.askTTL` (15m) — how long the daemon keeps the call and its result.
- `elicit.ttl` (120s) — the question's own deadline, unchanged. Expiry answers
  `cancel`, never `decline`: dismissed without choosing is not the same as
  refused.

---

## 4. mcpx as a client

What mcpx sends upstream, and what it will accept from a server.

| | |
| --- | --- |
| era probed first | modern (`server/discover`), with the era cached per server configuration; see [spec/era-probe.md](spec/era-probe.md) |
| legacy version sent | `2025-11-25` offered; `2024-11-05` .. `2025-11-25` accepted, anything else disconnects |
| modern versions offered | `2026-07-28` |
| per-server override | `protocol: legacy \| modern \| force-legacy \| force-modern` |
| declared to servers | `elicitation.form`, `roots`; `elicitation.url` and `sampling` only when a handler exists to answer them |
| transports | stdio, Streamable HTTP, and HTTP+SSE (2024-11-05) as the last fallback |

The per-revision rules -- headers, `x-mcp-header`, `resultType`, cancellation,
pagination, `subscriptions/listen`, resumption -- are in
[spec/client.md](spec/client.md).

### 4.1 Server-initiated requests mcpx answers

| request | mcpx answers | regardless of era |
| --- | --- | --- |
| `ping` | `{}` | yes |
| `roots/list` | the configured roots, before consulting any handler | yes |
| `elicitation/create` | through the broker, or `{"action":"cancel"}` when nothing can answer | yes |
| `sampling/createMessage` | through the broker; a decline becomes an error, since the specification has no decline result for sampling | yes |
| anything else | `-32601`, but **always answered** — silence is indistinguishable from a hung server | yes |

`ping` was the gap here: mcpx answered method-not-found, which a server reads
as a dead connection, so the liveness probe reported the opposite of the
truth.

A modern server asks the same three things by *answering* `input_required`,
and mcpx resolves them through the identical function. One function is what
stops the two eras answering the same question differently.

### 4.2 Notifications mcpx listens to

`notifications/message`, `notifications/progress`,
`notifications/{tools,resources,prompts}/list_changed`,
`notifications/resources/updated`, `notifications/elicitation/complete`.

All of them from any era. `notifications/tasks/status` (2025-11-25) is
received and dropped — mcpx polls task state rather than tracking it, so
nothing would act on it. That is a gap, named rather than hidden.

### 4.3 Generic requests

`Client.Request` and `Pool.Request` send any method. Before them the typed
helpers were the only way to the wire, and anything outside that set had no
path at all — which is why `POST /v1/complete` answered from cache with
`"upstream": false`. Not because the server could not complete: because
nothing could ask it.

`Complete` now forwards `completion/complete` to the server, which knows the
*values* an argument may take where mcpx knows only the names it has cached. A
server that never declared `completions` is not asked — a well-behaved one
answers method-not-found and the rest answer something unpredictable, so the
declaration is the only thing worth trusting — and the reply says `"source":
"cache"` with the reason.

---

## 5. One HTTP server

The daemon served `/v1` on a unix socket and a loopback port. `mcpx serve
--transport http` was a **separate process** serving `/mcp`, `/v1/tools/*`,
`/v1/call/<ns>/<tool>`, `/health` and `/openapi.json`. Two HTTP servers, two
overlapping `/v1` prefixes, and which one a caller reached decided which half
of the API existed — with no way to tell from the outside.

Now:

| was | is |
| --- | --- |
| `serve --transport http` → `/mcp` | the daemon mounts `/mcp` on its own listeners (`proto.mcpPath`, `proto.serveMCP`) |
| `serve --transport http` → `/v1/tools/<tool>` | `POST /v1/tools/{tool}` in the ops table |
| `serve --transport http` → `/v1/call/<ns>/<tool>` | `POST /v1/call/{server}/{tool}` in the ops table |
| `serve --transport http` → `/health` | `GET /v1/health`, which already existed |
| `serve --transport http` → `/openapi.json` | `GET /v1/openapi.json`, which already existed, and which now describes the per-tool paths the second server used to publish alone |
| `serve --transport stdio` | unchanged |

`serve --transport http` is **removed**, and asking for it fails with the
address to use instead. A command that fails without saying where the thing
went costs somebody an afternoon.

The daemon package cannot import the CLI — the dependency runs one way, and
inverting it to put the two servers together would be a worse cure than the
disease. So the CLI builds the handler and the daemon mounts it:
`Server.MCP`, `Server.MCPPath`, `Server.MCPTool`. Three fields.

**stdio stays**, and stays a thin shim. It is inherent rather than a choice:
an MCP host starts its servers by spawning a process and talking down the
pipe, so something has to be spawnable. Everything else the daemon already
had — a socket, a port, a lifetime, pooled servers kept warm between calls —
and giving MCP a second copy of all of it bought nothing.

To put MCP on a network: `mcpx daemon --port 8899 --address 0.0.0.0`, then
`http://host:8899/mcp`. The warning about an unauthenticated API binding
beyond loopback applies to `/mcp` exactly as it applies to `/v1`, and for the
same reason: whatever can route to the port can run tools as you.

### Why not an extra listener

An `--mcp-addr` on the daemon was the obvious alternative, and it is one
address away from what `--port` already does. The reason not to is that the
two surfaces are not separable: `/mcp` calls `/v1` for everything, the MCP
tools are *generated* from the `/v1` ops table, and a deployment that exposed
one without the other would be exposing half a program. One listener, one
access-control decision.

---

## 6. What is still missing

Named, because a gap nobody wrote down is a gap somebody rediscovers.

- **Inbound cancellation does not reach an upstream call.** `notifications/cancelled`
  is recorded; interrupting the in-flight call needs the request id plumbed
  through the pool.
- **`notifications/tasks/status` is dropped.** mcpx polls instead.
- **url-mode elicitation raised by mcpx itself.** The pass-through works; mcpx
  never starts one of its own.
- **OAuth for remote servers.** Declared and not performed, unchanged.
- **A modern client cannot answer a question raised by `mcpx_exec`.** A script
  makes many upstream calls and the question belongs to one of them; the
  correlation in §3.4 identifies a *call*, and a script is not one. Those
  questions go to the broker.

2026-09-29T20:00:00-05:00
