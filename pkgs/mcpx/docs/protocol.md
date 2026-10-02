# Protocol: every revision, both directions

```
created:      2026-09-29T20:00:00-05:00
last-updated: 2026-09-30T16:00:00-05:00
increment:    3
status:       standard
tags:         area:protocol
description:  what each revision defines, what mcpx accepts, what it sends,
              and why there is one HTTP server rather than two.
```

mcpx speaks all five revisions of MCP and sits in the middle of two of
everything: two eras, two directions, two mechanisms for the same question.
The rules that hold it together are short, and this document is mostly the
table that falls out of them. The per-revision detail and its tests are in
[`spec/`](spec/README.md); what the specification itself asks, independent of
mcpx, is [`spec/server-obligations.md`](spec/server-obligations.md) and
[`spec/client-obligations.md`](spec/client-obligations.md).

**Accept liberally, send conservatively.** A server implementing a method
beyond what the negotiated revision defines is not violating anything — it
withholds nothing the revision requires, and a client that reached for the
method would otherwise be told mcpx cannot do a thing it can. Sending is the
opposite: a field, a notification or a server-initiated request that the peer
did not negotiate *and* did not declare is a lie about what was agreed, and
the failure is silent. Either the peer ignores it, or it rejects the frame,
and nothing says which.

So mcpx accepts `tasks/*` from a 2025-06-18 client and `subscriptions/listen`
from a legacy one — and it will not put `structuredContent` in front of a
2025-03-26 client, will not stamp `resultType` on a reply to anything but
2026-07-28, and will never send `elicitation/create` to a client that did not
declare `elicitation`.

**The rule stops at removal** (§2.1): a method a revision *removed* is
method-not-found to a peer on that revision.

Since the first version of this page, every sentence in it has been checked
against a schema validator (`internal/mcpspec`, the five official schemas,
strict mode) and the official conformance suite
([`spec/official-suite.md`](spec/official-suite.md)). Several were wrong, and
the corrections are why this is increment 3.

---

## 1. The revisions

| | era | handshake | version carried | mcpx serves | mcpx speaks upstream |
| --- | --- | --- | --- | --- | --- |
| `2024-11-05` | legacy | `initialize` | once | yes, over stdio and Streamable HTTP; its HTTP+SSE server transport is not hosted | when the server answers with it; HTTP+SSE as the last fallback |
| `2025-03-26` | legacy | `initialize` | once | yes | when the server answers with it |
| `2025-06-18` | legacy | `initialize` | once | yes | when the server answers with it |
| `2025-11-25` | legacy | `initialize` | once | yes | offered on every `initialize` |
| `2026-07-28` | modern | none | `_meta`, every request | yes | **probed first** |

The split is the thing everything else follows from. A legacy implementation
negotiates once and can send its peer a request at any time afterwards. A
modern one has no handshake, restates its version and capabilities on every
request, and — as a server — has no channel at all on which to send a request
of its own. The matrix is unforgiving: modern against legacy fails, legacy
against modern fails, only a dual-era implementation bridges them. mcpx is
dual-era in both directions.

Upstream, mcpx probes modern first and remembers the answer: the 2026-07-28
versioning page makes era "a property of the server", and prescribes that
order for a dual-era client. The era is cached per server configuration, in
memory and in `<state>/upstream-eras.json`, so the probe costs one round trip
per configuration rather than one per start. It used to probe legacy first,
on the argument that nearly every server is legacy; the cache removed the
cost that argument was about. [`spec/era-probe.md`](spec/era-probe.md) has the
order, the fallbacks and the cache.

Downstream, `Oldest` is 2024-11-05: what a request that declared nothing is
spelled for, because every shape that revision defines is understood by
everything newer. Guessing downward is recoverable; guessing upward hands a
peer a frame it cannot parse. A *headerless* Streamable HTTP request, though,
is judged as 2025-03-26 (`Headerless`): 2024-11-05 has no Streamable HTTP, so
nothing arriving on `/mcp` can be a 2024-11-05 request that omitted the
header, and the later transport pages say to assume 2025-03-26.

### 1.1 When revisions conflict: `spec.precedence` and `spec.lenient` (#307)

Most differences between revisions are settled by the revision the peer
states. Where two revisions require incompatible behaviour and the peer's
version does not settle it, two settings decide
([`configuration.md`](configuration.md)):

- **`spec.precedence`** — the revisions in the order their rules win, first
  wins. Default: every revision mcpx serves, newest first
  (`2026-07-28,2025-11-25,2025-06-18,2025-03-26,2024-11-05`). A partial list
  is completed with the rest, newest first. **`--mcp-spec <rev>`** /
  `MCPX_MCP_SPEC` (`spec.first`) moves one revision to the front.
- **`spec.lenient`** — revisions mcpx does not hold strictly. Empty by
  default: every revision is strict.

An unknown revision in any of the three is refused with the list of valid
ones. The daemon reports the effective policy under `spec` in
`GET /v1/protocol`; `mcpx settings` shows each setting and where it came from.
Code consults it through `internal/spec` (`First`, `Strict`, and the
predicates `AtOrAfter`, `Before`, `Only`, `Between`).

Two behaviours consult it today:

| behaviour | default | changed by |
| --- | --- | --- |
| progress `message` relayed to a 2024-11-05 client (whose schema has no such field) | **sent** | stripped only when 2024-11-05 is first *and* strict: `--mcp-spec 2024-11-05` |
| a request from a 2026-07-28 upstream server (that revision has none; its stdio page says the client MUST NOT answer) | **dropped** | answered when `spec.lenient` includes `2026-07-28` |

The other conflicts in [`spec/revision-conflicts.md`](spec/revision-conflicts.md)
are still decided in code, not by these settings.

`internal/mcpserver/revisions.go` holds the floors, ceilings and removals as
data, and `GET /v1/protocol` serves them. A matrix in a document and a matrix
in code agree on the day they are written and never again, so the document
defers to the endpoint.

---

## 2. mcpx as a server

What a host connected to mcpx gets, over `mcpx serve` (stdio) or the daemon's
`/mcp` (Streamable HTTP).

### 2.1 Methods

| method | defined in | mcpx accepts | mcpx answers |
| --- | --- | --- | --- |
| `initialize` | legacy | legacy peers | the version asked for if mcpx speaks it, otherwise the latest legacy version it speaks — a result, not an error, as every legacy lifecycle page requires |
| `server/discover` | 2026-07-28 | always | `supportedVersions` (newest first), `capabilities`, `instructions`; `serverInfo` in `_meta`; `ttlMs`/`cacheScope` |
| `ping` | legacy | legacy peers | `{}` |
| `tools/list` | all | always | paginated (`mcp.pageSize`), opaque cursor, a deterministic order |
| `tools/call` | all | always | §2.3 and §3; an unknown tool is `-32602` |
| `prompts/list`, `prompts/get` | all | always | not found is `-32602`; an upstream failure `-32603` |
| `resources/list`, `resources/read`, `resources/templates/list` | all | always | binary contents as `blob`; not found is `-32002` (legacy) or `-32602` (2026), both with `data.uri` |
| `resources/subscribe`, `resources/unsubscribe` | legacy | legacy peers | `{}`, always; subscribes upstream where the owning server can deliver, and otherwise publishes a warning event (§4.3) |
| `subscriptions/listen` | 2026-07-28 | **any** era | an acknowledgement, then only the notifications the filter asked for, each tagged with the subscription id |
| `completion/complete` | all | always | forwarded to the server that owns the `ref`, as `/v1/complete` does, with `context.arguments`; a 2024-11-05 upstream is asked without a capability (that revision had none); unknown ref `-32602`, upstream failure `-32603` |
| `logging/setLevel` | legacy | legacy peers | `{}`; sets the level of upstream log messages relayed to this connection (§4.4) |
| `tasks/get`, `tasks/list`, `tasks/result`, `tasks/cancel` | 2025-11-25 core | **any** legacy peer | the core shapes; a task is visible only to the connection that started it |
| `tasks/get`, `tasks/update`, `tasks/cancel` | 2026-07-28 extension | modern peers that declared `io.modelcontextprotocol/tasks` on the request; `-32021` with `data.requiredCapabilities` (HTTP 400) otherwise | the extension's shapes; `tasks/list` and `tasks/result` are `-32601`, as the extension says |
| `skills/list`, `skills/get` | skills extension (SEP-2640) | always, unless the server offers only contributed tools | mcpx's own skills (`plugin/opencode/skills`, embedded at build), each file at `skill://mcpx/<name>/<file>` and also listed and readable as a resource; an unknown URI is `-32602`. Upstreams' skills are not relayed: [in-name-only.md](in-name-only.md#skills-extension) |
| `resources/directory/read` | skills extension, `directoryRead` | wherever `skills/list` is | the direct children of a directory inside one of mcpx's skills: files as `resources/list` lists them, subdirectories as `inode/directory`; paginated. A file or an unknown URI is `-32602` |
| `notifications/cancelled` | all | always | cancels the request it names — its context, and so its upstream call — and withholds the reply |

Two rows are the "accept liberally" rule doing visible work:
`subscriptions/listen` from a legacy client and core `tasks/*` from any legacy
client. mcpx answers both. It **declares** neither outside the revision that
defines it, because a declaration is a promise about the revision in force.

**The rule stops at removal.** Offering a client a method its revision never
had withholds nothing. Answering one its revision *removed* is different: the
removal is the specification naming a replacement, and 2026-07-28 requires
method-not-found — "If the server does not implement the requested RPC method,
it MUST respond with `404 Not Found` and a JSON-RPC error with code `-32601`".
The 404 is load-bearing rather than decorative: it is how a dual-era client
tells a modern server from a legacy endpoint that simply is not there. So
`initialize`, `ping`, `logging/setLevel`, `resources/subscribe` and
`resources/unsubscribe` are `-32601` to a 2026-07-28 peer and unchanged for
every older one.
`internal/mcpserver/revisions.go` holds the list as `removedIn`, keyed to the
same feature ceilings the rest of the matrix uses. mcpx answered all of them to
everyone until the official suite scored it (#250).

**Tasks, per era.** 2025-11-25 core tasks are client-directed: a `task` field
on `tools/call`, `{task}` back at once, `tasks/result` blocks. mcpx honours the
field from any legacy client. The 2026-07-28 extension is server-directed and
forbids the other way: the `task` field is ignored, and a client that declared
`io.modelcontextprotocol/tasks` on the request gets a `CreateTaskResult`
(`resultType: "task"`, `ttlMs`, `pollIntervalMs`). Which calls become tasks
follows the tool's `execution.taskSupport`: a task-supporting tool's call is a
task once it has run 250 ms, one that declared nothing only after
`protoMessages.taskAfter`, and a `required` one is `-32021` to a client
without the extension. `tasks/get` inlines the result. Questions a
task-supporting call asks at once go inline as `input_required` on the
original request, before any task exists; ones it asks later park the task in
`input_required` with `inputRequests`, answered through `tasks/update`.
A legacy task shows
`input_required` while one of its questions is out, and the question carries
the related-task `_meta`, as does the `tasks/result` response. A legacy
task-augmented call to a tool that declares no `execution.taskSupport` is
refused `-32601`, as 2025-11-25 asks: of mcpx's own tools only `mcpx_call` and
`mcpx_exec` declare `"optional"` (to 2025-11-25 clients only; 2026-07-28's
`Tool` has no `execution`). A 2026-07-28 client that names tasks in a
`subscriptions/listen`'s `taskIds` gets the ones it can see back in the
acknowledgement, then `notifications/tasks` on that stream -- the task's
current state at once, then every change, each the `DetailedTask` `tasks/get`
would answer. [`spec/messages.md`](spec/messages.md#tasks) has the
rest.

**Unknown tools are a protocol error.** `tools/call` naming a tool mcpx does
not have is `-32602` ("no tool named …"), as every revision's tools page lists
it. It used to be an `isError` result, which told the model to retry a call
that could never work (#284).

### 2.2 Capabilities mcpx declares

| capability | 2024-11-05 | 2025-03-26 | 2025-06-18 | 2025-11-25 | 2026-07-28 |
| --- | --- | --- | --- | --- | --- |
| `tools.listChanged` | true in pass-through mode when the connection can push, else false | same | same | same | same |
| `resources.subscribe`, `.listChanged` | when the connection can push | same | same | same | same |
| `prompts.listChanged` | when the connection can push | same | same | same | same |
| `completions` | — (the revision has no such capability; the method is answered anyway) | ✓ | ✓ | ✓ | ✓ |
| `logging` | ✓ | ✓ | ✓ | ✓ | — (the level travels in each request's `_meta`) |
| `tasks` (core) | — | — | — | ✓ | — |
| `extensions["io.modelcontextprotocol/tasks"]` | — | — | — | — | ✓ |
| `extensions["io.modelcontextprotocol/skills"]` | — | — | — | — | `{"directoryRead": true}` |

- **`tools.listChanged` is false unless mcpx passes tools through.** mcpx's
  own tool list is fixed when its server is built. It used to be declared and
  fed by upstream tool changes, which are not changes to *this* list: a client
  re-listed on every one and got the same tools back. In pass-through mode
  (`--passthrough`) the list *is* the upstreams', so an upstream's
  `tools/list_changed` is relayed -- unsolicited to a legacy client, and on a
  `subscriptions/listen` that asked for `toolsListChanged` to a modern one.
- **`logging` is declared to legacy clients.** The capability means "this
  server sends log messages": mcpx relays its upstreams' `notifications/message`
  to a client that set a level, during that client's calls (§4.4, #212).
  2026-07-28 has no such capability.
- **"Can push"** is a property of the connection, not of the server. A stdio
  connection can. A legacy HTTP session can, through its GET stream (which
  used to be missing, so both were declared and never delivered). A legacy
  POST outside a session cannot, and gets neither. A 2026-07-28 client receives
  these only on a `subscriptions/listen` stream, which is its own request's
  response, so any transport that can stream a response can deliver them.
- **Core `tasks` only to 2025-11-25, `extensions` only to 2026-07-28.** Both
  were sent to both; 2025-11-25's schema has no `extensions` field, and the
  extension's SEP says a server must not keep advertising core tasks under a
  revision that has the extension.

### 2.3 Result shapes, and what is downgraded

Everything is built in the newest shape and spelled down once, at the edge, in
`downgrade()`. An upstream's `tools/call` result (through `mcpx_call` or a
pass-through tool) and `prompts/get` result are carried as the upstream sent
them -- `isError`, every content block, `structuredContent` and `_meta` -- with
resource URIs rewritten to ones `/mcp` can read; listed tools, resources,
templates and prompts keep their optional fields. Building several shapes and choosing between them is how the
shapes drift apart.

| carried | defined from | to an older client |
| --- | --- | --- |
| `structuredContent` | 2025-06-18 | removed, and rendered into the `content` array as text — the data survives, in a vocabulary the client has |
| `resource_link` block | 2025-06-18 | becomes an embedded `resource` with a `text/plain` label naming the URI, which keeps the URI machine-readable where text would not |
| `audio` block | 2025-03-26 | a text block describing it, for 2024-11-05 |
| `_meta` on listed tools, resources, templates, prompts and read contents; `annotations.lastModified` | 2025-06-18 | stripped |
| tool `annotations`, the `completions` capability | 2025-03-26 | stripped for 2024-11-05 |
| `title` | 2025-06-18 | stripped |
| `icons` | 2025-11-25 | stripped |
| `resultType` | 2026-07-28 | removed — sending it says mcpx is speaking a revision it is not |
| `ttlMs`, `cacheScope`, `serverInfo` in `_meta` | 2026-07-28 | removed; `serverInfo` travels in the `initialize` result instead |
| `inputRequests` / `requestState` | 2026-07-28 | never sent; a legacy client is asked on the wire instead |
| `elicitationId` | 2025-11-25 | never sent to 2026-07-28 (it used to leak through `inputRequests`); minted for a 2025-11-25 url-mode question an upstream sent without one |

A revision that defines all of them pays nothing: the function returns the
result untouched. An older one gets a copy, never an edit, because a task's
stored result is handed out more than once and rewriting it in place would
downgrade it permanently for whoever reads it next.

**The 2026-07-28 envelope.** Every modern result carries `resultType`
(`complete` unless it is `input_required` or `task`) and
`_meta["io.modelcontextprotocol/serverInfo"]`. The cacheable ones carry the
REQUIRED `ttlMs` and `cacheScope`: `public` for `server/discover`,
`tools/list`, `prompts/list` and `resources/templates/list`, which follow
configuration rather than the caller; `private` for `resources/list`, which
includes the caller's own session artifacts, and `resources/read`. The ages
are `protoMessages.listMaxAge` (1m) and `protoMessages.readMaxAge` (0s). An
`input_required` result carries no hints — it is an interim answer.

The strict schema sweep (`internal/mcpserver/schema_sweep_test.go`) validates
every frame mcpx sends against each revision's `schema.json` and rejects a
property the revision never defined. It is how the discover shape, the missing
cache hints, the listen acknowledgement's shape, `extensions` sent to
2025-11-25 and a `uri` where `resources/templates/list` needed `uriTemplate`
(in every revision) were found. [`spec/messages.md`](spec/messages.md) has the
per-revision list.

### 2.4 Transport and identity

The rules are in [`spec/transport.md`](spec/transport.md); what matters for
the rest of this page:

- **stdio runs requests concurrently**, each under its own context, so a
  `notifications/cancelled` can be read while the request it names is running,
  and cancelling it reaches the upstream call. Notifications and `initialize`
  stay in line. At EOF, requests in flight get `transport.stdioDrain` to
  answer.
- **Streamable HTTP, legacy**: only `initialize` mints `Mcp-Session-Id`; an
  unknown or terminated session is 404; `DELETE` ends one; a session has a GET
  stream for what mcpx pushes; within a session a dropped connection is not a
  cancellation, and `notifications/cancelled` is.
- **Streamable HTTP, 2026-07-28**: `MCP-Protocol-Version`, `Mcp-Method` and
  `Mcp-Name` are required and must agree with the body (`-32020`, 400); a
  missing `_meta` is `-32602` (400); the HTTP status is a function of the
  error code; closing the stream is cancellation; no sessions, none minted,
  none echoed.
- **`Origin`** is validated on every request: loopback, the daemon's own
  address and `transport.allowedOrigins`; anything else is 403.
- **Who a request is from** is decided per connection
  ([`spec/identity.md`](spec/identity.md)): `_meta["dev.mcpx/session"]` if the
  client names one; otherwise the process on stdio, the `Mcp-Session-Id` on a
  legacy HTTP session, and nothing — its own per-request scope — for a
  2026-07-28 request. Every `/mcp` client used to share the daemon's pid and
  cwd, and so one stateful instance and one set of artifacts.

---

### 2.5 Pass-through: upstreams under their own names

By default mcpx is a gateway: a small tool set (`mcpx_call`, `mcpx_exec`,
discovery), prompts as `<namespace>_<prompt>`, resources as
`mcpx://<namespace>/<uri>`. `mcp.passthrough` (`--passthrough <server>` on
`mcpx serve` and `mcpx daemon`, `MCPX_MCP_PASSTHROUGH`) names a configured
server -- or several, comma-separated, `--passthrough demo,tasks` -- whose
surface is offered **as itself** instead:

- `tools/list` carries its tools under their own names, ahead of the gateway's;
  `tools/call` on one of them is forwarded and its result returned verbatim —
  images, audio, embedded resources, `structuredContent` and `isError` intact;
  a JSON-RPC error the upstream answers with is relayed as that error, not
  turned into an `isError` result. Its `execution.taskSupport` is carried on
  `tools/list` and decides whether a call runs as a task ("Tasks, per era", §2.1).
  Its questions reach the calling client exactly as `mcpx_call`'s do (§3), with
  the upstream's own `inputRequests` keys where it asked in a 2026-07-28 result.
- `prompts/list` names its prompts without the prefix; `prompts/get` returns
  the upstream's result verbatim.
- `resources/list`, `resources/templates/list` and `resources/read` use its own
  URIs, as do subscriptions and `completion/complete` refs. An `mcpx://` URI
  still reaches every other server and mcpx's artifacts.
- **Its declaration, not mcpx's.** A 2026-07-28 client's
  `clientCapabilities` go to the upstream with the call, narrowed from mcpx's
  own: mcpx never declares to the upstream a capability its client did not
  (a tool that needs sampling is refused rather than run on mcpx's say-so).
  An upstream JSON-RPC error — `-32021` with its `requiredCapabilities`,
  `-32602` for a name it does not know — is returned as that error, HTTP 400
  included, not as a tool result with `isError`.
- **Names it does not list.** A `tools/call` for a name that is neither the
  upstream's listed tool nor a gateway tool is forwarded; the upstream says
  whether it exists. A server may answer to diagnostic or hidden tools it does
  not list.
- **Roots.** A `roots/list` the upstream asks in an `input_required` round is
  put to a client that declared `roots`, as an elicitation is; for one that
  did not, mcpx answers with its own configured roots.
- **Several upstreams** are merged into one surface. A tool or prompt name two
  of them offer is refused -- `tools/list` or `prompts/list` fails with an
  error naming both -- rather than handed to either, since the loser would be
  unreachable under the name the client was shown. A bare resource URI goes
  to the first upstream, in the order given, that lists it as a resource or
  offers a template it falls under; one none of them lists goes to the first.
- **Collisions with the gateway:** the upstream wins. A gateway tool whose name the upstream
  also uses is neither listed nor callable over MCP (it stays on the CLI and
  `/v1`). The upstream is the server in this mode, and a client must be able to
  call every name `tools/list` shows and get that tool.

Every other server stays reachable through the gateway tools. Not carried
over: a pass-through tool's `outputSchema`, `title` and `annotations` (the
daemon's catalogue keeps name, description, input schema and `execution`
only). The upstream's progress and log messages during a call are relayed
(§4.4). `scripts/conformance.sh` runs the official suite's own fixture server
this way, merged with `internal/testsupport/taskmcp`, mcpx's fixture for the
suite's tasks-extension scenarios, which the suite's server does not define.

## 3. A server asks a question

This is the part with two mechanisms for one thing, and the reason the rest of
the design is shaped the way it is.

When mcpx proxies a call and the upstream server elicits or samples, the
question becomes a row in the broker: an identity, a deadline, a routing
decision (`docs/elicitation.md`). What the broker could not do alone is ask
**mcpx's own client** — the agent or editor that made the call, which is very
often the only thing that knows the answer.

It does now, native-first and broker-backed (`proto.native`). The broker
remains the store and the fallback; the client is asked first when it said it
could answer.

### 3.1 Which client gets asked

| the client declared | version | what happens |
| --- | --- | --- |
| nothing | any | broker only |
| `elicitation` | 2024-11-05, 2025-03-26 | broker only; those revisions have no elicitation |
| `elicitation` | 2025-06-18 | asked, form mode only, `mode` and `elicitationId` stripped |
| `elicitation` (any shape) | 2025-11-25, 2026-07-28 | asked in form mode; a url-mode question stays with the broker |
| `elicitation.url` | 2025-11-25, 2026-07-28 | asked in either mode |
| `sampling` | any | asked for `sampling/createMessage` only |

Declaring one is not declaring the other. A sampling-only client is never sent
an elicitation, and vice versa. Anything mcpx may not send this client is left
with the broker and its default audience, so nothing is lost by a client's
narrowness — it just does not get to answer. Two gates are coarser than they
should be (#210): any `elicitation` declaration counts as form mode, though a
2025-11-25 client that declared only `url` has not offered forms; and
`sampling` is not checked for `sampling.tools` or `sampling.context`.

**Why a non-declaring 2026-07-28 client never gets `-32021` for a tool that
asks.** 2026-07-28 `basic/index` says a server "MUST NOT rely on capabilities
the client has not declared" and "if processing a request requires a
capability the client did not include ... MUST return" `-32021`
(`MissingRequiredClientCapabilityError`). The error is conditional on
*requiring* the capability, and mcpx never does: a question it may not send
the client goes to the broker, which is a durable queue answerable by any
consumer (`mcpx elicit`, `/v1`, the plugin, a human) whether or not one is
listening when it is asked -- there is no state in which the broker has no
audience, only one in which nobody has answered yet, which ends in the
question's own timeout. mcpx also cannot know in advance: whether an upstream
tool elicits is decided by the upstream mid-call, so a pre-dispatch `-32021`
would be a guess. The official suite's
`sep-2575-server-rejects-undeclared-capability` assumes a fixture tool
(`test_missing_capability`) that mcpx does not publish, and so does not test
this path (#252 item 1). The extension's own methods are different:
`tasks/get`, `tasks/update` and `tasks/cancel` from a client that did not
declare `io.modelcontextprotocol/tasks` answer `-32021` with
`data.requiredCapabilities` (HTTP 400), because there the capability is the
whole request; so does a `subscriptions/listen` asking for task notifications
without it.

The message is rewritten to name the originator: *"github (via mcpx) asks:
which repository?"*. A client asked "are you sure?" with no idea who is asking
cannot answer it.

### 3.2 Legacy: a request on the wire

The server sends `elicitation/create` while the client's `tools/call` is still
open, and waits.

**Over stdio** this is straightforward — the pipe is the session, it stays
open for the life of the process, and mcpx's own receive loop routes inbound
frames by shape rather than by id. Requests run on their own goroutines, so
the answer can arrive while the request that asked is blocked on it.

**Over Streamable HTTP** it is not, and this is the part worth recording. The
POST is the only channel, and it is the thing waiting for the answer. So:

1. mcpx issues `Mcp-Session-Id` on `initialize`.
2. A request that has to ask turns its POST response into an event stream —
   lazily, so a request that asks nothing still gets a plain JSON object — and
   writes `elicitation/create` as an SSE frame.
3. The client answers with a **separate POST** carrying the same session
   header, whose body is a JSON-RPC response. mcpx routes it to the request
   waiting on that session and answers `202`.
4. The original stream carries the final result and closes.

A request that asks nothing but is slow — a pass-through call waiting out
`pool.callTimeout` on an upstream that went quiet — also becomes an event
stream once it has been silent for `transport.sseKeepAlive`, carrying a comment
each interval until the answer, when the client's `Accept` offered
`text/event-stream`. Before, such a POST got no bytes at all, not even headers,
for up to two minutes, and a client or proxy with an idle timeout gave up on a
call that was still running. The stream carries no event ids: mcpx does not
offer SEP-1699 resumption, and an id would promise a replay it cannot make.

A legacy POST outside any session cannot be asked anything: its answer would
arrive on a POST nothing can route back. Its questions stay with the broker.

That is the reason `Conn` exists: a subscription filter, a push function and a
client's declared capabilities used to live on the `Server`, which was correct
while stdio was the only transport and wrong the moment one server answered
several HTTP clients — those fields decide *whose* client gets asked a
question.

Server ids are negative on the wire. JSON-RPC only requires uniqueness within
a direction, but several clients key one table by id, and they would otherwise
see mcpx's request answer their own.

### 3.3 Modern: a result the client retries

A 2026-07-28 server has no connection to send a request on. It answers
`resultType: "input_required"` with a map of `inputRequests` and an opaque
`requestState`; the client answers each and sends the **same request again**
with `inputResponses` and the state attached. Only `tools/call`, `prompts/get`
and `resources/read` may do this.

`inputResponses` sent on a request that resumes nothing — a client that knows
what will be asked and answers up front — are held until the upstream asks, and
given to the questions whose keys they match. Keys that match nothing are
ignored, as SEP-2322 asks of information a server does not recognise.

Which means the call has already returned by the time the answer exists. The
upstream call therefore cannot be tied to the client's request — if it were,
there would be nothing left to resume. It runs as a daemon task
(`internal/tasks`, the same store `/v1/call`'s task option uses, collectable
from the same `/v1/tasks/{id}`), and questions are correlated to it.

**`requestState` is signed and bound to the request.** The specification says
the client must treat it as opaque, and that it SHOULD be tamper-evident and
bound to the originating method and parameters, an expiry and — where there is
one — the principal. mcpx's is `base64(payload).HMAC-SHA256` under a key
generated per daemon process and never persisted, carrying the call id, a
binding (the method and a SHA-256 of the parameters without `_meta`,
`inputResponses` and `requestState`) and an expiry (`proto.stateTTL`). A token
that verifies but was issued for another request is refused with a message
that says so — the difference between a confused client and somebody
replaying matters to whoever reads the log.

It used to be bound to the connection: an `Mcp-Session-Id`, which 2026-07-28
does not have. mcpx minted one on `server/discover` to have something to bind
to, a conformant client never sent it back, and every such client got
"still asking for input after 8 rounds" within milliseconds. Binding to the
request needs nothing from the transport. mcpx has no authenticated principal
to add, so a state is replayable by anyone who holds it, on the same request,
within its TTL.

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

The rule is one comparison. The ask table holds the upstream requests a
call *owns*; the pool counts every upstream request in flight on the key,
whoever made it (`/v1/call`, exec, the CLI, the plugin). A call owns a question
when all the table's entries on the key are its own **and** their number equals
the pool's count — nobody else is on the connection. Until #229 the table
alone decided, and an interruptible call sharing a key with somebody's
ordinary `mcpx call` was handed that caller's question. One window remains: an
entry is registered an instant before its request is counted, so a stranger's
question arriving in exactly that instant can still be misattributed.

**A script** (`mcpx_exec`) is one call that makes many. It runs as an
interruptible call too (`/v1/ask` kind `exec`), and its run id is chosen and
registered *before* the script starts. The generated client sends that id as
`X-Mcpx-Run` on every `/v1/call`, so each upstream call the script makes joins
the table as a member of the run. A question on a key whose in-flight calls all
belong to one run is that run's — including calls the script makes in
parallel. Two runs, or a run and anyone else, on one key: ambiguous, broker.
When the run ends (finished, timed out, abandoned) its membership is removed
everywhere, so a straggling upstream call killed with the script cannot pin a
question to a call nobody polls; that question stays with the broker.

A script that asks more than `proto.askRounds` questions in one legacy request
is abandoned like any other call — scripts that elicit in a loop hit it first.
The script's own `exec.timeout` (120s) is usually shorter than
`proto.askTimeout` and a person answering eats into it; set `timeoutSec` on
the tool call for a script that expects to ask.

### 3.5 Bounds

- `proto.askTimeout` (10m) — how long one client request may be held. A legacy
  client is blocked for all of it, so it has to sit inside whatever that
  client's own timeout is. A modern one is not blocked at all.
- `proto.askRounds` (8) — how many times one call may come back asking. A
  server that never stops asking is broken or adversarial. For a 2026-07-28
  client each round is a new request, so the count rides in the signed
  `requestState` and holds across the retries; it used to restart at zero on
  every one (#201).
- `proto.stateTTL` (30m) — how long a `requestState` may be resumed with.
- `proto.askTTL` (15m) — how long the daemon keeps the call and its result.
- `pool.callTimeout` pauses while a question is pending on the instance
  (#229). It used to kill a call while a person was answering it, which made
  the three settings above promises the pool did not keep. The precedence of
  all five timeouts is written down in `internal/pool/budget.go`.
- The question's own deadline is `elicit.ttl` in `defaults.json` (120s; a
  built-in, not a user setting). Expiry answers `cancel`, never `decline`:
  dismissed without choosing is not the same as refused.

A call the client stops waiting for — it cancelled, or its connection went —
keeps running as a task, and its questions keep their deadlines. The result
can still be collected from `/v1/tasks/{id}`; nothing interrupts the upstream
call. That is deliberate for work somebody may come back for, and it is also
why cancellation does not reach an interruptible call (§6).

---

## 4. mcpx as a client

What mcpx sends upstream, and what it will accept from a server.

| | |
| --- | --- |
| era | modern probed first (`server/discover`), legacy on fallback; the answer cached per server configuration ([spec/era-probe.md](spec/era-probe.md)) |
| per-server override | `protocol: prefer-discover \| prefer-initialize \| force-discover \| force-initialize \| follow`, over `upstream.protocol` (default `prefer-discover`); the old names `modern`, `legacy`, `force-modern`, `force-legacy` and others are accepted as aliases ([spec/era-probe.md](spec/era-probe.md#values)); `follow` adds a legacy session for legacy callers ([spec/era-probe.md](spec/era-probe.md#follow)) |
| legacy version | `2025-11-25` offered; `2024-11-05` .. `2025-11-25` accepted; anything else and mcpx disconnects |
| modern version | `2026-07-28` |
| declared to servers | `elicitation.form` and `roots` always; `elicitation.url` and `sampling` when a handler is installed — which in the daemon is always (#210) |
| transports | stdio, Streamable HTTP, and HTTP+SSE (2024-11-05) as the last fallback of the HTTP probe, remembered in the era record |
| stdio shutdown | close stdin, wait `stdioShutdown.stdinGrace` (2s, a built-in default) for the server to exit, then `SIGTERM` the process group, wait `stdioShutdown.termGrace` (3s), then `SIGKILL` — every revision's stdio SHOULD (#203 LV-39; it used to `SIGTERM` at once) |
| headers | legacy: `MCP-Protocol-Version` is the negotiated version, omitted on `initialize` and up to 2025-03-26; modern: `Mcp-Method`, `Mcp-Name`, `Mcp-Param-*` for `x-mcp-header` parameters, never a session id |

The per-revision rules — headers, `x-mcp-header`, `resultType`, cancellation,
pagination, `subscriptions/listen`, resumption, the GET stream — are in
[spec/client.md](spec/client.md). The official suite's client leg passes every
non-auth scenario ([spec/official-suite.md](spec/official-suite.md)).

The probe used to be the other way round, and it did not matter which way,
because mcpx read `protocolVersions` from `server/discover` where the schema
says `supportedVersions`. mcpx could not connect to any real modern server.
Its own server made the same mistake, which is why mcpx talking to mcpx
passed every test.

### 4.1 Server-initiated requests mcpx answers

| request | mcpx answers | regardless of era |
| --- | --- | --- |
| `ping` | `{}` | yes |
| `roots/list` | the daemon's roots, before consulting any handler — and the daemon passes none, so the list is empty (#210) | yes |
| `elicitation/create` | through the broker, or `{"action":"cancel"}` when nothing can answer; a mode that was not declared for the revision in use is refused | yes |
| `sampling/createMessage` | through the broker; a decline becomes an error, since the specification has no decline result for sampling | yes |
| anything else | `-32601`, but **always answered** — silence is indistinguishable from a hung server | yes |

Always answered, unless the server withdraws the question first: a
`notifications/cancelled` naming a request still being answered ends the
handler's context (so a question relayed to the host is withdrawn too) and no
response is sent. The reason, if given, goes to the daemon log as a
`server.warning`. A cancellation naming anything else is ignored.

A modern server asks the same three things by *answering* `input_required`,
and mcpx resolves them through the identical function. One function is what
stops the two eras answering the same question differently.

### 4.2 Notifications mcpx listens to

`notifications/message`, `notifications/progress`, `notifications/cancelled`,
`notifications/{tools,resources,prompts}/list_changed`,
`notifications/resources/updated`, `notifications/elicitation/complete`.

From a legacy server they arrive unsolicited (on the standalone GET stream,
over HTTP). A 2026-07-28 server sends them only on a `subscriptions/listen`
stream, so mcpx opens one per upstream connection, with a filter built from
what the server declared and the URIs somebody subscribed to, and reopens it
when it ends. Messages and progress go to `/v1/events`; a list change drops
the cached schema. `notifications/tasks/status` (2025-11-25) is received and
dropped — mcpx polls task state rather than tracking it. A request from a
2026-07-28 server is not answered: that revision has no server-to-client
requests (a server asks through `input_required` results), and the stdio
transport page says the client MUST NOT respond (#200). With `2026-07-28` in
`spec.lenient` it is answered like a legacy server's (§1.1).

### 4.4 Progress, log messages and trace context, relayed (#212)

During a `tools/call` that reaches an upstream, mcpx carries what the host
asked for through to the server doing the work, and what that server sends
back to the host:

- **Progress.** A host's `_meta.progressToken` is replaced upstream by a token
  of mcpx's own (`mcpx-<n>`, unique on that upstream connection, which two
  hosts' tokens are not), and each `notifications/progress` for it is sent to
  the host with the host's token restored. Only progress for a call still in
  flight is passed on: a token nobody asked for, or one whose response is in,
  is dropped. A report that does not exceed the last one passed on is dropped,
  so the host never sees progress go backwards; once progress reaches `total`
  nothing more is sent; and reports for one token are passed on no more often
  than every 20 ms (`mcpclient.ProgressMinInterval`), except the final one. A
  `message` goes to every host by default; it is removed for a 2024-11-05
  host only when 2024-11-05 is first in `spec.precedence` and strict
  (`--mcp-spec 2024-11-05`), since that revision's schema has no such field
  (§1.1).
- **Log messages.** A 2026-07-28 host's `_meta` `logLevel` is sent upstream
  (or the daemon's own level, if more verbose); a legacy host's level is the one
  it set with `logging/setLevel`. Upstream `notifications/message` at or above
  that level are sent to the host. No level, no messages. A log message names
  no request, so one arriving while a host has several calls in flight on the
  same upstream connection goes to each of them.
- **Trace context.** `traceparent`, `tracestate` and `baggage` in `_meta` are
  passed upstream unchanged.

Over Streamable HTTP these go on the call's own response stream, which
becomes `text/event-stream` only when something is relayed; over stdio, as
notifications. Between `mcpx serve` and the daemon the call carries a `relay`
object on `/v1/call` (answered as `application/x-ndjson` when anything was
relayed, plain JSON otherwise) or `/v1/ask` (collected by the polls as
`notifications`). `mcpx call` and `mcpx exec` send no relay and print only the
result. Messages and progress still reach `/v1/events` as before.

Not yet: progress does not extend mcpx's own call timeout (CODE-13), and
prompts/get and resources/read relay nothing.

### 4.3 Resource subscriptions, end to end (#241, #247)

mcpx declared `resources.subscribe` to its own clients and never subscribed
upstream, so `notifications/resources/updated` never flowed: a capability
declared and not delivered. Now a client subscribing to
`mcpx://<namespace>/<uri>` -- by legacy `resources/subscribe` (stdio, or
Streamable HTTP with the update on the GET stream) or by `resourceSubscriptions`
on a 2026-07-28 `subscriptions/listen` -- makes the daemon subscribe `<uri>`
on the server that owns the namespace, with whichever mechanism that server's
era has.

- **The lifetime is a `/v1/events` stream.** `mcpx serve` hears the daemon's
  events over `GET /v1/events?uri=mcpx://...`; a stream naming a resource in
  mcpx:// form (or bare with `server=`) *is* a subscription to it, held until
  the stream closes. So a listen stream ending, a connection ending, a legacy
  unsubscribe (which replaces the connection's stream), or `mcpx serve`
  crashing all release it without a separate call that could be missed, and a
  stream that reconnects to a restarted daemon subscribes again by
  reconnecting. The stream's first event, `resource.watching`, says what was
  subscribed and why anything was not.
- **Counted per URI across every client.** The upstream sees one connection,
  mcpx's, and one `resources/unsubscribe` from it would end the updates for
  everyone. The first watcher subscribes; the last one unsubscribes
  (`internal/pool/watch.go`).
- **Instances.** One holding a subscription is not idle and is not reaped. A
  newly started instance is given every subscription still wanted, and one
  that ends while its subscriptions are wanted -- `mcpx restart`, a crash -- is
  replaced at once, since nobody else would start it and the updates would
  stop without a word. A replacement that fails to start is the pool's ordinary
  start failure; the next call to that server retries and resubscribes.
- **A server that does not declare `resources.subscribe`** is never sent a
  subscription (that would be asking for an undeclared capability). mcpx keeps
  declaring `resources.subscribe` itself -- it is one capability over many
  servers, and it does support the mechanism -- and is honest per resource
  instead: the listen acknowledgement's `resourceSubscriptions` lists only the
  URIs whose updates will arrive, which is what the acknowledgement exists to
  report. A legacy `resources/subscribe` for such a URI -- or for one no
  configured server owns, like a bare `test://x` -- **succeeds** and delivers
  nothing (#251). A subscription is a standing interest, not a lookup: the
  spec does not require the resource to exist or its updates to be
  deliverable, and the legacy revisions have no way to say "agreed, but
  nothing will come". Refusing with `-32602`, as #247 first did, broke clients
  that subscribe before they know (the official suite's
  `server-resources-subscribe` and `-unsubscribe` among them). The reason is
  not lost: the daemon publishes a `server.log` event at level `warning`
  ("no updates will be delivered for <uri>: <reason>"), visible on
  `/v1/events`. Honesty stays where the protocol has a mechanism for it, the
  listen acknowledgement. Only agreed URIs' updates are ever sent on a stream.
- **Absolute-path URIs.** A listing drops a leading `/` when it namespaces
  (`/abs/doc` is listed as `mcpx://demo/abs/doc`). The daemon resolves the
  inner part against the server's own resource list to recover `/abs/doc`,
  and `events.Filter` compares URIs without a leading `/`; before, the two
  forms never matched.

### 4.4 Generic requests

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

- **Cancelling an interruptible call does not stop it.** On a direct call,
  `notifications/cancelled` (stdio, legacy HTTP session) and a closed stream
  (2026-07-28 HTTP, sessionless legacy) cancel the upstream request. A call on
  the ask path runs as a daemon task and keeps running (§3.5).
- **Progress does not extend mcpx's own call timeout.** It is relayed to the
  host (§4.4), so a host's timeout can reset on it; the daemon's
  per-server `callTimeout` still cannot.
- **`-32021` for a tool that asks** is deliberately not raised (§3.1); whether
  the broker fallback discharges the MUST is #252 item 1.
- **Tasks.** `notifications/tasks/status` is dropped; mcpx never
  task-augments a call upstream, so a tool with `taskSupport: "required"`
  cannot be called; an extension task never reaches `input_required` (#209).
- **Capabilities declared upstream that nothing answers**: `sampling` and
  `elicitation.url` whenever a handler exists, `roots` with an empty list
  (#210).
- **Authorization.** No OAuth client and no OAuth on `/mcp` (#253). The
  per-server `auth` block (bearer, basic, header, query, env) is applied to
  every connection (#240); `type: oauth` is refused with a message, since the
  flow is not implemented.
- **Validation.** Tool inputs and outputs and elicitation answers are not
  checked against their JSON Schemas (#254); sampling results are relayed
  unreviewed (#256); elicitation storage is readable by any local user and
  never pruned (#255).
- **Smaller conformance gaps**: an invalid cursor restarts at page one instead
  of `-32602`, `mcpx://` URIs are built without percent-encoding, no rate
  limits, annotations shown as trusted, and the rest of #257.
- **url-mode elicitation raised by mcpx itself.** The pass-through works; mcpx
  never starts one of its own.
- **One attribution window** (§3.4): an entry is registered an instant before
  its request is counted.

2026-09-30T16:00:00-05:00
