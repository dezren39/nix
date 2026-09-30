# Stateless and every revision: can mcpx do both?

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare, area:protocol
description:  whether one mcpx can serve and consume the stateless
              2026-07-28 protocol and every legacy revision at once, what
              "stateless" actually requires, where mcpx keeps state, and
              what stands in the way at 05c78b2.
```

The question: can mcpx speak the most modern, fully stateless protocol
(2026-07-28) and also every older revision, in both directions?

## The answer

**The specification says yes, and says how. mcpx at `05c78b2` does not yet.**

The specification allows exactly this. A *dual-era* server "**MAY** serve both
eras concurrently on the same endpoint or process"
(`2026-07-28/basic/versioning.mdx:182`), and it chooses per request: "A request
carrying modern per-request `_meta` is served statelessly according to this
revision. An `initialize` request selects legacy semantics, scoped to the
stdio process (stdio) or the session (HTTP)"
(`2026-07-28/basic/versioning.mdx:176-180`). A legacy session is therefore
compatible with statelessness. What breaks statelessness is *requiring* state
from a modern request, not having state somewhere.

"Stateless" is a property of the protocol surface, not of the process. A
2026-07-28 server may keep caches, durable tasks, open subscription streams
and application handles. What it may not do is infer anything from the
connection. It must not rely on earlier requests for version, capabilities or
identity (`2026-07-28/basic/index.mdx:191-193`), should not require related
operations to use the same connection (`:196-197`), must reference
cross-request state "by an explicit identifier the client passes on each
request" (`:200-202`), and "must not treat connection or process identity as
a proxy for conversation or session continuity" (`:204-208`).

mcpx at `05c78b2` meets the dual-era shape. It serves `initialize` and
per-request `_meta` on one endpoint, and it reads `_meta` before the
connection's handshake (`internal/mcpserver/conn.go:133-146`). It fails on
details that decide interoperability:

1. **It cannot talk to a conformant 2026 peer in either direction.**
   `server/discover` answers `protocolVersions` and the client reads
   `protocolVersions`; the schema field is `supportedVersions`
   (`internal/mcpserver/server.go:534`, `internal/mcpclient/client.go:284`,
   `schema/2026-07-28/schema.ts:683`). mcpx talks to itself because both
   halves are wrong the same way.
2. **Its modern `input_required` path needs the session 2026 removed.** A
   `requestState` is bound to `Mcp-Session-Id`
   (`internal/mcpserver/state.go:47-51`, `internal/mcpserver/server.go:1208-1230`).
   A conformant sessionless 2026 client that declares `elicitation` gets
   `-32603 "tools/call was still asking for input after 8 rounds"` within
   milliseconds, and the upstream call is abandoned
   (`internal/mcpserver/ask.go:195-209`; reproduced on the wire, see
   [the M1 transcript](#evidence)).
3. **It mints `Mcp-Session-Id` for modern clients** on `server/discover`
   (`internal/mcpserver/server.go:1214-1229`). 2026-07-28 does not define the
   header, and tells a server that supports only that revision to "ignore it,
   and do not mint or echo session IDs"
   (`2026-07-28/basic/transports/streamable-http.mdx:680-686`). A dual-era
   server is not literally bound by that sentence. But a modern request is to
   be "served statelessly according to this revision", and the harm is not the
   header, which a conformant client ignores. The harm is that mcpx then
   *depends* on the client echoing it (item 2).
4. **Its pool treats process identity as session continuity.** A host that
   reaches mcpx over MCP is keyed as `mcp-<pid>` of the serving process
   (`internal/cli/serve.go:292-297`). Over the daemon's `/mcp` that pid is the
   daemon's, so every HTTP host shares one key. The comment says the host has
   "no way to tell us" (`internal/cli/serve.go:289-290`). opencode v2 does tell
   us: it sends `_meta["ai.opencode/sessionID"]` on every `tools/call`
   (`v2:packages/core/src/mcp/client.ts:278`), and mcpx discards it.

None of these is structural. Each has a known fix, and none needs mcpx to
drop legacy support. §5 is the list.

---

## 1. What "stateless" requires of a 2026-07-28 server

The checklist, with mcpx's status at `05c78b2`. A server is stateless in the
2026-07-28 sense if all of these hold. "Dual-era" in the status column means
the item applies to modern requests only, and mcpx handles legacy ones as the
revision they negotiated.

| # | requirement | source | mcpx @ 05c78b2 |
| --- | --- | --- | --- |
| 1 | Every request carries `_meta["io.modelcontextprotocol/protocolVersion"]` and `…/clientCapabilities`; a request missing either is rejected with `-32602` (HTTP 400) | `2026-07-28/basic/index.mdx:380-381` | partial: a request with a version and no capabilities is served as if it declared nothing (`internal/mcpserver/conn.go:150-165`). A request with neither is served as 2025-03-26, which is the dual-era reading (`internal/mcpserver/conn.go:137-146`) |
| 2 | Version decided per request; an unsupported one gets `-32022` with `data.supported` and `data.requested` | `2026-07-28/basic/versioning.mdx:48-67` | partial: `-32022` is sent, but `requested` is `""` for a modern request and the HTTP status is 200, where 2026 says 400 (`internal/mcpserver/server.go:469-471`; W13) |
| 3 | Nothing inferred from earlier requests on the same connection | `2026-07-28/basic/index.mdx:191-193` | ✓ for modern requests: `_meta` wins over the connection (`internal/mcpserver/conn.go:133-136`) |
| 4 | A capability the request did not declare is never relied on; if one is required, `-32021` | `2026-07-28/basic/index.mdx:387-392` | partial: send-conservatively holds, but `-32021` is never used (`rg -- -32021 internal` finds nothing) |
| 5 | Related operations need not share a connection or process | `2026-07-28/basic/index.mdx:196-197` | ✗: a `requestState` retry must carry the same `Mcp-Session-Id` or it is refused "this requestState belongs to another session" (M4) |
| 6 | Cross-request state is named by an explicit identifier the client sends | `2026-07-28/basic/index.mdx:200-202` | partial: task ids ✓; `requestState` ✓, but it also needs the session header (item 5) |
| 7 | No protocol session: `Mcp-Session-Id` is neither minted nor required; GET has no stream (the literal SHOULD is for a server supporting only 2026-07-28) | `2026-07-28/basic/transports/streamable-http.mdx:680-686` | ✗ minted on `server/discover` (item 3); ✓ GET answers 405 (`internal/mcpserver/server.go:1124-1131`) |
| 8 | `tools/list`, `prompts/list`, `resources/list` do not vary per connection | `2026-07-28/server/tools.mdx:65-68` | ✓: the MCP surface is built once per daemon (`internal/cli/serve_ask.go:234-254`) |
| 9 | The server never sends a JSON-RPC request; it asks through `input_required`, and only on `tools/call`, `resources/read` and `prompts/get` | `2026-07-28/basic/patterns/mrtr.mdx:182-192` | ✓ for modern peers (`internal/mcpserver/ask.go:201-209`, `internal/mcpserver/ask.go:320-327`) |
| 10 | `requestState` is treated as attacker-controlled and integrity-protected. SHOULD bind the principal, a short expiry, and the method plus a digest of the parameters | `2026-07-28/basic/patterns/mrtr.mdx:232-243` | partial: HMAC-SHA256 with an expiry ✓. It binds the *session*, not a principal, and not the method or its arguments. A state was accepted on a different tool call on the same session (observed) |
| 11 | Request-scoped notifications go only on that request's stream, and log messages only if the request set `logLevel` | `2026-07-28/server/utilities/logging.mdx:63` | ✓ by omission: mcpx forwards neither progress nor log messages to hosts |
| 12 | The only long-lived construct is `subscriptions/listen`, whose state belongs to that request | `2026-07-28/basic/index.mdx:211-214` | ✗ over HTTP: `subscriptions/listen` gets `202` with an empty body (W17). Partial on stdio: the acknowledgement has the wrong shape |
| 13 | A closed response stream means cancellation; no resumption | `2026-07-28/basic/transports/streamable-http.mdx:233-237` | ✓ by context propagation (inferred from code, not driven), and mcpx has no resumption |
| 14 | `server/discover` answers `supportedVersions`, `capabilities`, optional `instructions`, `ttlMs`, `cacheScope` | `schema/2026-07-28/schema.ts:678-697` | ✗: `protocolVersions`, a top-level `serverInfo`, no `ttlMs` or `cacheScope` (W5) |
| 15 | Every result has `resultType`; list, read and discover results have `ttlMs` and `cacheScope`; results SHOULD carry `_meta["io.modelcontextprotocol/serverInfo"]` | `2026-07-28/server/utilities/caching.mdx:13-16` | partial: `resultType` ✓ (wrong on the task handle); `ttlMs`, `cacheScope` and `serverInfo` ✗ |
| 16 | On HTTP, `MCP-Protocol-Version`, `Mcp-Method` and (for three methods) `Mcp-Name` are required and checked against the body; a mismatch is `-32020` with HTTP 400 | `2026-07-28/basic/transports/streamable-http.mdx:597-603` | ✗: `Mcp-Method` and `Mcp-Name` are never read (W11). A version mismatch gets 400 with a body that is not a JSON-RPC error (W10) |

What remains legitimately stateful in a conformant 2026 server: open listen
streams, durable tasks (extension), client-held `requestState`, application
handles, per-request auth context, process caches that no connection can
observe, and legacy sessions for clients that opened with `initialize`.

---

## 2. Where mcpx keeps state

Every place mcpx remembers something across requests, verified at
`05c78b2` (paths relative to `pkgs/mcpx`). The last two columns answer the
question for 2026-07-28.

| # | item | where | keyed by | lifetime | compatible with 2026 statelessness? | could it move into the request? |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | pool instances (upstream processes and sessions) | `internal/pool/pool.go:57-72` | server × scope key (global, repo, worktree, cwd, session, parent-session, pid, call) × sharing | until idle with no holders, a `Min` floor, `Max` with LRU eviction, restart or reload | **yes**: upstream-side and invisible to hosts. The *key* is the problem (row 13) | the key can: from `_meta["ai.opencode/sessionID"]`, or a server-minted handle passed as a tool argument, which is the 2026 pattern (`2026-07-28/changelog.mdx:12`) |
| 2 | instance ↔ scope-key back-reference, for attributing questions | `internal/pool/request.go:18-33` | instance | instance | yes (internal) | n/a |
| 3 | upstream schema cache (tools, resources, prompts, instructions) | `internal/pool/pool.go:121-128` | server | until `list_changed`, refresh or reload; persisted and reseeded | yes | n/a. It could honour an upstream `ttlMs` |
| 4 | upstream client state (era, negotiated version, capabilities) | `internal/mcpclient/client.go:69-100` | instance | instance | yes: the spec tells clients to cache the era per server (`2026-07-28/basic/versioning.mdx:148-152`) | n/a |
| 5 | upstream HTTP session id | `internal/mcpclient/http.go:29-30` | transport | transport; DELETE on close | legacy upstreams only; it should not be sent to a modern one | n/a |
| 6 | Streamable HTTP sessions (`Mcp-Session-Id` → connection) | `internal/mcpserver/server.go:1206-1230` | `sess-<32 hex>` | idle `proto.sessionIdle` (30m), reaped only when another session is issued; DELETE | **legacy only**. Minting one for a modern client (`server/discover`) is the violation | for legacy, no: a legacy client's answer to `elicitation/create` arrives on a separate POST, and the session is how it finds the waiting request. For modern, its only job is binding `requestState`, which can bind to something in the request instead (row 9) |
| 7 | the connection's handshake state (capabilities, version) | `internal/mcpserver/conn.go:27-34` | connection | connection | n/a for modern (`_meta` first) | modern: already in the request; legacy: inherent |
| 8 | pending server→client requests (legacy elicitation and sampling) | `internal/mcpserver/conn.go:52-53` | negative integer id | until answered or timed out | legacy only | n/a |
| 9 | `requestState` signing key and tokens | `internal/mcpserver/state.go:25-51` | key: per process, never persisted; token: `{c: call id, b: session binding, e: expiry}` | key: process; token: `proto.stateTTL` (30m) | **partly**: this *is* the 2026 mechanism; binding it to the session is what makes it depend on `Mcp-Session-Id` | yes: bind the principal (or nothing; the HMAC already authenticates the token) plus a method and argument digest. The call id still names daemon-side state (row 10) |
| 10 | interruptible calls: the upstream call keeps running as a daemon task while the client is away answering | `internal/daemon/routes_proto.go:53-65` | call id (= task id); (server, scope key) for attribution | the call; its result kept `proto.askTTL` (15m) | **yes, as a handle**. The spec permits server-side state behind `requestState` and requires it for single-use (`2026-07-28/basic/patterns/mrtr.mdx:238-243`) | no: a live upstream call cannot be serialised into a token |
| 11 | ask-loop bounds | `internal/mcpserver/timing.go:22-34` | request | `proto.askTimeout` 10m, `proto.askRounds` 8, `proto.askPoll` 500ms | n/a (per request) | n/a |
| 12 | elicitation broker rows | `internal/elicit/elicit.go:87-136` | question id | `elicit.ttl` (120s) per question; rows persist in SQLite | yes (mcpx-internal, not protocol) | n/a |
| 13 | the MCP session key for pool scope | `internal/cli/serve.go:287-297` | `$MCPX_SESSION_ID`, else `mcp-<pid>` of the serving process | process | **no**: it is process identity used as session continuity, which `2026-07-28/basic/index.mdx:204-208` rules out; over `/mcp` every HTTP host shares the daemon's pid | yes: from request `_meta` or a tool argument |
| 14 | MCP task store | `internal/mcpserver/tasks.go:31-38` | `tsk-<16 hex>` | per-task TTL (client `ttl` or 10m) | yes: server-minted handles are the extension's model (but the shape is 2025-11-25's, and tasks are not bound to their requestor) | already a handle |
| 15 | the daemon's `/v1` task store, a second store | `internal/daemon/ops.go:428-429` | `tsk-…` | same TTL rules | yes | should be the same store as row 14 |
| 16 | subscription state (`subs` per URI, the listen filter) | `internal/mcpserver/conn.go:54-55` | connection | until replaced, DELETE or process exit | listen: 2026's own mechanism; per-URI `subs`: legacy | listen filter: already in the request |
| 17 | recorded inbound cancellations | `internal/mcpserver/conn.go:56` | request id | connection; unbounded on stdio | n/a | nothing reads it |
| 18 | pagination cursor | `internal/mcpserver/server.go:1477-1513` | none: the cursor encodes the offset | none | **yes**: already stateless | already in the request |
| 19 | the MCP surface (`lazyMCP`) | `internal/cli/serve_ask.go:234-254` | daemon | daemon (never rebuilt) | yes | n/a |
| 20 | event-bus subscriptions for `subscriptions/listen` | `internal/cli/serve.go:726-764` | listen | listen | yes | n/a |
| 21 | the default stdio connection | `internal/mcpserver/server.go:148-151` | process | process | legacy-inherent: stdio is one connection by nature | n/a |

Two observations the table makes:

- **Almost all of mcpx's state is on the upstream side of the proxy.** Pools,
  schema caches, upstream sessions and the broker are invisible to a host.
  2026 says nothing about them and does not need to. A proxy that keeps a
  browser warm between calls keeps state, and statelessness never forbade that.
- **The state that faces the host is small and fixable.** It is the HTTP
  session (legacy-only by right), the `requestState` binding, the MCP session
  key, and two task stores. Each has a request-scoped replacement that the
  specification already names.

The one thing that cannot become request-scoped is row 10, the live upstream
call behind an `input_required`. Nor should it. mcpx is a single local process,
not a fleet behind a load balancer, and the pattern's motivation ("without
requiring a shared storage layer across server instances",
`2026-07-28/basic/patterns/mrtr.mdx:30-31`) does not apply to it. The
requirement that does apply is that the handle is explicit, bound and
expiring, and mcpx's `requestState` already is.

---

## 3. Every revision, both directions

What happens today when each era meets mcpx. Server columns: a host of that
revision connecting to mcpx. Client columns: mcpx connecting to an upstream
server of that revision. The per-feature detail is in
[matrix-mcpx.md](matrix-mcpx.md).

| peer revision | mcpx as server | mcpx as client |
| --- | --- | --- |
| 2024-11-05 | ✗ `initialize` is refused with `-32022` (W1, S1). Every legacy revision says a server MUST counter-offer a version it supports (`2025-03-26/basic/lifecycle.mdx:132`) | partial, by accident. mcpx offers 2025-11-25; a 2024-11-05 server counter-offers 2024-11-05 and mcpx accepts whatever comes back (`internal/mcpclient/client.go:261-275`). There is no HTTP+SSE transport, so only stdio and a Streamable-HTTP-capable server work |
| 2025-03-26 | ✓ with gaps: JSON-RPC batches, which that revision says servers MUST receive, are rejected `-32700` (`2025-03-26/basic/index.mdx:97`; W16); prompt `title` leaks to it | ✓ |
| 2025-06-18 | ✓ with gaps: `mcpx_call` drops `isError` and flattens results to text for every revision (W20, W21) | ✓ |
| 2025-11-25 | ✓ with gaps: sent `capabilities.extensions`, a 2026-only field | ✓ (`notifications/tasks/status` dropped) |
| 2026-07-28 | partial: works for mcpx's own client and for a client that keeps the `Mcp-Session-Id` from `server/discover`. A conformant client fails at `server/discover` (field names) and at `input_required` (session binding) | ✗ against a conformant modern-only server: discovery reads the wrong field; the legacy-first probe gives up when `initialize` is answered `-32022` (`internal/mcpclient/client.go:246`); `Mcp-Method`/`Mcp-Name` are never sent, so a conformant HTTP server answers 400 |

"Every older one" also needs a floor. The oldest published revision is
2024-11-05, and mcpx's is 2025-03-26
(`internal/mcpserver/revisions.go:14`). Serving 2024-11-05 is cheap to add, and
it is the only revision mcpx refuses outright. It means a counter-offer in
`negotiate`, plus one `floors` row for each thing 2025-03-26 added (audio,
tool annotations, the `completions` capability:
`2025-03-26/changelog.mdx:18`, `:25-27`). With those rows the `downgrade()` audio branch,
dead today, starts to matter.

---

## 4. The conflict at the centre

Statelessness and "accept liberally" pull in opposite directions in one place:
a request that carries **neither** `_meta` **nor** a session. 2026-07-28 says
such a request is malformed (`2026-07-28/basic/index.mdx:380-381`). The
legacy HTTP rules say a server that receives no `MCP-Protocol-Version` "SHOULD
assume protocol version `2025-03-26`" (`2025-06-18/basic/transports.mdx:250-254`),
and a legacy client may legitimately never send a session header when the
server issued none. mcpx takes the legacy reading and serves the request as
2025-03-26 (`internal/mcpserver/conn.go:137-146`).

For a dual-era server that is the right call. Rejecting it would break every
legacy client that ignores sessions, and nothing in 2026-07-28 tells a
dual-era server to prefer the modern reading. The specification never states
it, and this page records it so nobody "fixes" it into a regression. See
[conflicts.md](conflicts.md#c-01).

---

## 5. What it would take

Ordered by how much interoperability each item buys. The register row for
each is in the linked area file. The PROTOCOL work in progress at the time of
writing (a modern-first probe and conformance fixes) is expected to close some
of these; the status column in each register file is where that shows.

| # | change | area | issue | effort |
| --- | --- | --- | --- | --- |
| 1 | `supportedVersions` on both sides; `serverInfo` into result `_meta`; `ttlMs` and `cacheScope` on discover, list and read results | [lifecycle](register/lifecycle-versioning.md), [pagination-caching](register/pagination-caching.md) | #199, #200 | S |
| 2 | Bind `requestState` to the request, not the session: the method, an argument digest, the principal if there is one, and the expiry. Stop minting `Mcp-Session-Id` for modern clients. Fix the no-binding busy loop so a question the client cannot receive goes back to the broker, as the comment says it does | [mrtr](register/mrtr.md), [transports](register/transports.md) | #201 | M |
| 3 | Client: treat `-32022` from `initialize` as a modern server and retry with `server/discover`; send `Mcp-Method`, `Mcp-Name` and `Mcp-Param-*` | [lifecycle](register/lifecycle-versioning.md), [transports](register/transports.md) | #200 | S–M |
| 4 | Server: check `Mcp-Method` and `Mcp-Name`; answer header mismatches with `-32020` and HTTP 400; map `-32601` → 404 and `-32022`/`-32021`/`-32602` → 400 for modern requests | [transports](register/transports.md), [errors](register/errors.md) | #199 | S |
| 5 | Take the pool's session key from `_meta["ai.opencode/sessionID"]` (or a namespaced equivalent) when present, instead of `mcp-<pid>` | [process-model](register/process-model.md), [meta](register/meta.md) | #211 | S |
| 6 | Validate `Origin` on `/mcp` and `/v1`. It has been a MUST since 2024-11-05, and it is the one gap here with a working attack: DNS rebinding from a web page to a loopback port that runs scripts | [transports](register/transports.md) | #204 | S |
| 7 | `subscriptions/listen` over HTTP as an SSE response stream; the acknowledgement as `notifications/subscriptions/acknowledged` with `_meta` subscription ids | [notifications](register/notifications.md) | #208 | M |
| 8 | The tasks extension for 2026 clients (`tasks/update`, `resultType: "task"`, `ttlMs`, no `tasks/list`/`tasks/result`), and no `extensions` or core `tasks` capability to a revision that does not define it | [tasks](register/tasks.md), [capabilities](register/capabilities.md) | #209 | M–L |
| 9 | Counter-offer on `initialize` (2024-11-05 and unknown versions) and add the 2024-11-05 floors | [lifecycle](register/lifecycle-versioning.md) | #202 | S |

Nothing on this list conflicts with keeping every legacy revision. Items 1–8
touch only the modern path or add checks the legacy path already skips.

---

## Evidence

The wire observations (W*, M*, S*) come from a session recorded against a
binary built from `05c78b2` with an isolated configuration, state and socket,
driving the repository's own test servers (`internal/testsupport/fakemcp`,
`internal/testsupport/askmcp`). W1, W5, W12, W15, W16, W17, W18, W20, W21, M1
and M2 were reproduced independently for this page. The requests, verbatim:

- **W5** `server/discover` with 2026 `_meta` → HTTP 200, a `Mcp-Session-Id`
  header, and `{"capabilities":{…,"extensions":{"io.modelcontextprotocol/tasks":{}},…,"tasks":{…}},"instructions":…,"protocolVersions":[…],"serverInfo":{…}}`.
- **M1** `tools/call` `mcpx_call` → `ask.need_repo` with 2026 `_meta`
  declaring `elicitation`, no session → `{"error":{"code":-32603,"message":"tools/call was still asking for input after 8 rounds"}}` in about 40 ms.
- **M2** the same after taking `Mcp-Session-Id` from `server/discover` →
  `{"resultType":"input_required","inputRequests":{"elc-…":{"method":"elicitation/create",…}},"requestState":"<base64>.<hmac>"}`.
  The base64 part decodes to `{"c":"tsk-…","b":"sess-…","e":<unix expiry>}`.
- **M4** the M3 retry without the session header →
  `{"error":{"code":-32602,"message":"this requestState belongs to another session"}}`.
- **W17** `subscriptions/listen` over HTTP → `202 Accepted`, `Content-Length: 0`.
- **W13** a request whose `_meta` version is `9999-01-01` → HTTP 200,
  `-32022`, `"requested":""`.
