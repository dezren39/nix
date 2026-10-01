# Progress and cancellation

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  notifications/progress and progress tokens, timeouts, notifications/cancelled and 2026 transport-specific cancellation, in the spec, mcpx, opencode and lootbox
```

Progress and cancellation let a long request report on itself and be stopped: progress gained a `message` in 2025-03-26 and
became server-only in 2026-07-28, and 2026 made cancellation transport-specific, so that closing an HTTP response stream
*is* the cancel and a client's `notifications/cancelled` is defined only on stdio. mcpx never asks upstreams for progress
and never relays it to hosts, so long calls look hung and opencode v1's 60-second timer cannot be kept alive (PC-04,
PC-05); an inbound `notifications/cancelled` is recorded and acted on by nothing, while an HTTP disconnect does cancel the
upstream call, for legacy hosts too (PC-11, PC-12). The request-scoping rule shared with log messages and a server ending a
listen stream with `notifications/cancelled` are in [notifications.md](notifications.md); progress on tasks is in
[tasks.md](tasks.md).

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| PC-01 | `notifications/progress` fields; `message` added | `2025-03-26 has` | `24-11 token, progress, total · 25-03+ + message` | partial — parsed from upstreams onto the event bus only | + low | S | low |
| PC-02 | `progressToken` rules: string or integer, unique among active requests | `2024-11-05 has` | all five; chosen by "sender" (24-11) → "client" (26-07) | n/a — never issues or forwards a token | + low | S | low |
| PC-03 | Progress direction: either side → server only | `2026-07-28 removes` | `24-11..25-11 both ways · 26-07 server → client` | partial — host progress answered with a `-32601` error | + low | S | low |
| PC-04 | Client requests progress with `_meta.progressToken` | `opencode v1 has` `opencode v2 has` `mcpx missing` `2024-11-05 has` | opencode v1/v2: token = request id; mcpx ✗; lootbox ✗ | ✓ — a call whose host sent a token carries one of mcpx's own (#212) | + med | S | med |
| PC-05 | Proxy relays progress between host and upstream | `mcpx missing` | every revision (server → client) | ✓ — relayed under the host's token (#212; `docs/protocol.md` §4.4) | + high | M | med |
| PC-06 | Timeouts, reset on progress, hard maximum | `2025-03-26 has` `opencode v1 has` | `24-11 "appropriate" · 25-03+ SHOULD time out, MAY reset, SHOULD cap` | partial — fixed 120 s call timeout; no progress reset | + med | S | med |
| PC-07 | `notifications/cancelled` `requestId`: required → optional → required | `2025-11-25 has` `2026-07-28 has` | `24-11..25-06 required · 25-11 optional · 26-07 required` | ✓ — always sends it; tolerates its absence | + low | S | low |
| PC-08 | Cancellation `reason` | `mcpx missing` | optional string in every revision | ✗ — client always says `"timeout"` (#203) | + low | S | low |
| PC-09 | `initialize` MUST NOT be cancelled | `2024-11-05 has` `2026-07-28 removes` `mcpx missing` | `24-11..25-11 ✓ · 26-07 —` (no `initialize`) | ✗ — a timed-out `initialize` gets a cancel (#203) | + low | S | low |
| PC-10 | Who may cancel: either side → client only (+ listen) | `2026-07-28 has` | `24-11..25-11 either side · 26-07 client; server only for listen` | ✓ — cancels only its own relayed legacy requests | + low | S | low |
| PC-11 | Receiver stops work on `notifications/cancelled` | `impl deferred` `mcpx missing` | every revision (stdio only in 26-07) | partial — recorded; no upstream call is interrupted (#77) | + med | M | med |
| PC-12 | HTTP: closing the stream is cancellation (2026), not (legacy) | `2026-07-28 has` `specs conflict` `opencode v2 has` | `25-03..25-11 SHOULD NOT · 26-07 MUST` | partial — disconnect cancels for every era; interruptible calls survive | + med | M | med |
| PC-13 | Client `notifications/cancelled`: stdio only in 2026 | `2026-07-28 has` `specs conflict` `mcpx missing` | `24-11..25-11 any transport · 26-07 stdio MUST, HTTP undefined` | partial — aborts the POST and also sends the notification (#203) | + low | S | low |
| PC-14 | A caller's abort or timeout cancels the upstream call | `opencode v1 has` `opencode v2 has` | opencode ✓; mcpx ✓; lootbox ✗ | ✓ — context end sends `notifications/cancelled` upstream | + med | S | low |

## PC-01 `notifications/progress` fields; `message` added

- **What.** `progressToken`, `progress` (MUST increase with each notification) and optional `total`, in every revision;
  2025-03-26 adds an optional human-readable `message` that SHOULD be relevant. `progress` and `total` MAY be floating
  point.
- **Where.** Base fields in all five; `message` from 2025-03-26.
- **mcpx @ 05c78b2.** As a client it parses all four fields, `message` included (`internal/mcpclient/client.go:436-442`),
  and publishes them as a `Progress` event on the bus (`internal/daemon/hooks.go:34-36`). As a server it emits none
  (PC-05).
- **Value to mcpx.** + low: the parsing is done; what is missing is the relay.
- **Effort.** S.
- **Risk.** low.
- **Detail.** The 2026 changelog also makes progress request-scoped ([notifications.md](notifications.md), SUB-07). A
  2026 embedded sampling request has no `_meta`, so no token and no progress for it ([sampling.md](sampling.md)).
- **Sources.** `schema/2024-11-05/schema.ts:271` "notifications/progress"; `schema/2025-03-26/schema.ts:316`
  "message?: string;"; `2025-03-26/changelog.mdx:24` "Added"; `2024-11-05/basic/utilities/progress.mdx:52`
  "values **MAY** be floating point."; `internal/mcpclient/client.go:436-442`; `internal/daemon/hooks.go:34-36`.

## PC-02 `progressToken` rules: string or integer, unique among active requests

- **What.** A requester opts in by putting `progressToken` in the request's `_meta`. Tokens MUST be a string or integer and
  MUST be unique across all active requests; notifications MUST reference only tokens from an active request, and MUST
  stop after completion. The receiver MAY send none at all.
- **Where.** All five. 2024-11-05 says the token is "chosen by the sender"; 2026 says "chosen by the client", since only
  clients request progress there (PC-03).
- **mcpx @ 05c78b2.** Issues no tokens upstream and forwards none from hosts (PC-04, PC-05).
- **Value to mcpx.** + low: the rules a relay must follow when it starts relaying.
- **Effort.** S.
- **Risk.** low.
- **Detail.** A host's token is unique only among that host's active requests. A relay that multiplexes many hosts onto one
  upstream connection therefore has to mint its own upstream tokens and map them back, rather than pass hosts' tokens
  through. Unspecified in 2026: whether an MRTR retry may reuse the original request's token ([mrtr.md](mrtr.md)).
  opencode uses the JSON-RPC request id as the token (PC-04).
- **Sources.** `2024-11-05/basic/utilities/progress.mdx:15` "Progress tokens **MUST** be a string or integer value";
  `2024-11-05/basic/utilities/progress.mdx:16-17` "across all active requests.";
  `2026-07-28/basic/patterns/progress.mdx:17` "Progress tokens can be chosen by the client using any means";
  `2024-11-05/basic/utilities/progress.mdx:56` "Progress notifications **MUST** only reference tokens that:";
  `2024-11-05/basic/utilities/progress.mdx:88` "Progress notifications **MUST** stop after completion".

## PC-03 Progress direction: either side → server only

- **What.** Legacy: either party may request and send progress, so a client can report progress on a server-initiated
  request (a long elicitation, say). 2026: only the server sends progress, only for tokens from an active client request,
  only on that request's stream.
- **Where.** Both ways in 2024-11-05..2025-11-25; server to client in 2026-07-28.
- **mcpx @ 05c78b2.** mcpx never asks a host for progress on the questions it relays, and a host that sends
  `notifications/progress` anyway gets an id-less `-32601` error back (wire S5; the hygiene bug is
  [notifications.md](notifications.md), SUB-18).
- **Value to mcpx.** + low: nothing to build; stop replying to the notification.
- **Effort.** S.
- **Risk.** low.
- **Detail.** Client-to-server progress disappears in 2026 because server-initiated requests do; legacy
  `ClientNotification` includes `ProgressNotification` and 2026's does not.
- **Sources.** `2024-11-05/basic/utilities/progress.mdx:7` "Either side can send progress notifications to";
  `2026-07-28/basic/patterns/progress.mdx:8` "The server **MAY** send progress notifications"; wire S5.

## PC-04 Client requests progress with `_meta.progressToken`

- **What.** Whether a client asks its server for progress at all. Without a token, a conforming server sends none.
- **Where.** opencode v1 and v2 pass a no-op `onprogress` to every `callTool`, which makes the SDK attach
  `_meta.progressToken` equal to the numeric request id; the values are otherwise ignored (not shown to the user or the
  model). v1 code-mode child calls do the same. lootbox sends no token. mcpx sends none.
- **mcpx @ 05c78b2.** `CallTool` sends `{name, arguments}` only (`internal/mcpclient/client.go:849`); `progressToken`
  appears only in the struct it parses (`internal/mcpclient/client.go:438`).
- **Value to mcpx.** + med: upstream tools that report progress (browsers, crawls) could extend mcpx's call deadline
  (PC-06) and feed `/v1/exec` streams and plugin metadata.
- **Effort.** S — attach a token per upstream call; the notification path already exists.
- **Risk.** med: without it, a long upstream call is killed at the fixed call timeout even while it is making progress.
- **Detail.** opencode's v1 comment says why: "The MCP SDK only sends a progress token when this hook is present, enabling
  timeout resets."
- **Also recorded from the meta register.** Every revision. opencode v1 and v2 pass a no-op progress handler, which
  makes their SDKs set `_meta.progressToken` to the numeric request id on every `tools/call`; the values are not shown
  to anyone. v1 resets its 60 s call timeout on progress; v2 does not. `tools/call` reads only `name` and `arguments`
  (`internal/mcpserver/server.go:658-665`), and the ask path deletes `_meta` before dispatch
  (`internal/mcpserver/ask.go:100`), so the host's token reaches nothing. The client never sets a token upstream.
  Forwarding progress itself is in the progress register.
- **Sources.** `v1:packages/opencode/src/mcp/catalog.ts:64`; `v1:packages/opencode/src/tool/code-mode.ts:154-158`;
  `v2:packages/core/src/mcp/client.ts:281`; <https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/shared/protocol.js>
  (L649, `progressToken: messageId`); `internal/mcpclient/client.go:849`; `internal/mcpclient/client.go:438`.; `schema/2024-11-05/schema.ts:25`; `2026-07-28/basic/index.mdx:352`; `internal/mcpserver/ask.go:100`; https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/shared/protocol.js L649 "progressToken: messageId".

## PC-05 Proxy relays progress between host and upstream

- **What.** A proxy in the middle of a call can pass the host's progress request upstream and the upstream's progress
  back to the host, on the host request's own stream.
- **Where.** Progress exists in every revision; mcpx relays none of it.
- **mcpx @ 05c78b2.** The host's `_meta` (and with it `progressToken`) is stripped before the call (`forAsk`,
  `internal/mcpserver/ask.go:95-108`); upstream calls carry no token (PC-04); progress that does arrive goes to the event
  bus only (`internal/daemon/hooks.go:34-36`); `internal/mcpserver` has no `notifications/progress` sender.
- **Value to mcpx.** + high: progress for long `mcpx_call` and `mcpx_exec` runs in hosts, and, for opencode v1, the only
  way to keep a call alive past the SDK's 60-second default, because v1 resets its timer on progress (PC-06).
- **Effort.** M — token mapping per call (PC-02) and request-scoped delivery on HTTP (an SSE response) and stdio.
- **Risk.** med: long calls look hung, and v1 kills them at 60 s.
- **Detail.** In 2026 progress MUST go only on the originating request's stream, never on a listen stream
  ([notifications.md](notifications.md), SUB-07). mcpx already opens an SSE response for legacy elicitation, which is the
  same mechanism.
- **Sources.** `internal/mcpserver/ask.go:100` "delete(m, \"_meta\")"; `internal/daemon/hooks.go:34-36`;
  `2026-07-28/basic/patterns/progress.mdx:14`; `v1:packages/opencode/src/mcp/catalog.ts:61`.

## PC-06 Timeouts, reset on progress, hard maximum

- **What.** 2024-11-05 says only to implement appropriate timeouts. From 2025-03-26: implementations SHOULD time out every
  request and SHOULD cancel on timeout, SDKs SHOULD allow per-request timeouts, implementations MAY reset the clock on
  progress but SHOULD always enforce a maximum. 2026 moves the text from the lifecycle page to the cancellation page and
  phrases "cancel" per transport.
- **Where.** 2025-03-26 onward. opencode v1: connect and list 30 s; `tools/call` uses the per-server `timeout` or
  `experimental.mcp_timeout`, else the SDK's 60 s, with `resetTimeoutOnProgress: true` and no `maxTotalTimeout`. opencode
  v2: startup 30 s, catalog 30 s, execution 12 h, with no reset on progress. lootbox: a `Promise.race` per call that
  progress cannot reset.
- **mcpx @ 05c78b2.** A fixed `pool.callTimeout` of 120 s (`internal/defaults/defaults.json:6`), configurable but not
  extended by progress, since mcpx requests none (PC-04). On timeout the client cancels upstream (PC-14).
- **Value to mcpx.** + med: reset-on-progress up to a hard wall is the spec's recommended shape and what long tools need.
- **Effort.** S once PC-04 exists.
- **Risk.** med: a server that sends progress forever never times out under a reset-only policy, which is why the SHOULD
  asks for a maximum. opencode v1 has no maximum.
- **Detail.** opencode v2's comment says "Requesting progress keeps long calls alive under the SDK's timeout; execution is
  the hard wall", but it never sets `resetTimeoutOnProgress`, so progress has no effect and the 12 h value *is* the
  timeout. (The code-mode findings say v2 resets on progress too; the v2 source says otherwise.) opencode v1's config text
  says the default is 5 s while its code uses 30 s, and `docs/opencode-plugin.md:178` says "one value, 30 s", which is only
  the connect and list timeout.
- **Sources.** `2025-03-26/basic/lifecycle.mdx:199` "Implementations **SHOULD** establish timeouts for all sent requests";
  `2025-03-26/basic/lifecycle.mdx:208` "Implementations **MAY** choose to reset the timeout clock when receiving a";
  `2026-07-28/basic/patterns/cancellation.mdx:63` "always enforce a maximum timeout, regardless of progress notifications";
  `v1:packages/opencode/src/mcp/catalog.ts:61` "resetTimeoutOnProgress: true,";
  `v1:packages/opencode/src/mcp/index.ts:38` "const DEFAULT_TIMEOUT = 30_000";
  `v2:packages/core/src/mcp/client.ts:34` "const DEFAULT_EXECUTION_TIMEOUT = 12 * 60 * 60 * 1_000";
  `v2:packages/core/src/mcp/client.ts:280` "Requesting progress keeps long calls alive under the SDK's timeout";
  `v1:packages/core/src/v1/config/mcp.ts:21`; `.lootbox/src/lib/rpc/execute_mcp.ts:199-208`;
  `internal/defaults/defaults.json:6` "\"callTimeout\": \"120s\",".

## PC-07 `notifications/cancelled` `requestId`: required → optional → required

- **What.** `requestId` is required through 2025-06-18. 2025-11-25 makes it optional so the notification's shape can
  coexist with tasks (it MUST be present for non-task requests and MUST NOT be used for tasks, which use `tasks/cancel`).
  2026 makes it required again, tasks having left the core.
- **Where.** Required 2024-11-05..2025-06-18 and 2026-07-28; optional in 2025-11-25.
- **mcpx @ 05c78b2.** Always sends it, as client and as server (`internal/mcpclient/client.go:714`;
  `internal/mcpserver/conn.go:266`). Inbound, a cancel without `requestId` is accepted and recorded under the key `<nil>`
  (`internal/mcpserver/server.go:1521-1538`), which does no harm because nothing reads the record (PC-11).
- **Value to mcpx.** + low: accept a missing `requestId` from 2025-11-25 peers without error, which mcpx does.
- **Effort.** S.
- **Risk.** low: a strict parser would reject 2025-11-25 cancels without the id.
- **Detail.** Neither changelog mentions either change.
- **Sources.** `schema/2024-11-05/schema.ts:132` "requestId: RequestId;"; `schema/2025-11-25/schema.ts:223`
  "requestId?: RequestId;"; `schema/2026-07-28/schema.ts:626` "requestId: RequestId;";
  `internal/mcpserver/server.go:1533` "c.cancelled[fmt.Sprint(p.RequestID)] = p.Reason".

## PC-08 Cancellation `reason`

- **What.** An optional free-text `reason` on `notifications/cancelled`; both parties SHOULD log it.
- **Where.** Every revision.
- **mcpx @ 05c78b2.** The client says `"timeout"` whatever ended the context: a host's cancellation, a disconnect, or a real
  timeout (`internal/mcpclient/client.go:714`). A legacy question mcpx relayed and then gave up on is cancelled with
  `"timed out"` (`internal/mcpserver/conn.go:266`), which is accurate.
- **Value to mcpx.** + low: honest upstream logs.
- **Effort.** S — take the reason from the context's cause.
- **Risk.** low: misleading upstream logs.
- **Detail.** None beyond the above.
- **Sources.** `schema/2024-11-05/schema.ts:137` "reason?: string;";
  `2026-07-28/basic/patterns/cancellation.mdx:108` "Both parties **SHOULD** log cancellation reasons for debugging";
  `internal/mcpclient/client.go:714` "\"reason\": \"timeout\"".

## PC-09 `initialize` MUST NOT be cancelled

- **What.** Every legacy revision forbids a client to cancel its `initialize` request, in prose and schema.
- **Where.** 2024-11-05..2025-11-25; 2026 has no `initialize`.
- **mcpx @ 05c78b2.** The client's generic path sends `notifications/cancelled` for any request whose context ends
  (`internal/mcpclient/client.go:708-716`), `initialize` included.
- **Value to mcpx.** + low: skip the cancel for `initialize`.
- **Effort.** S.
- **Risk.** low: a strict server may treat it as a protocol error.
- **Detail.** Found by reading the code path; not driven on the wire.
- **Sources.** `2024-11-05/basic/utilities/cancellation.mdx:34` "request **MUST NOT** be cancelled by clients";
  `schema/2025-11-25/schema.ts:238` "A client MUST NOT attempt to cancel its"; `internal/mcpclient/client.go:708-716`.

## PC-10 Who may cancel: either side → client only (+ listen)

- **What.** Legacy: either party may cancel a request it issued, in the same direction. 2026: the client cancels its own
  requests; a server may send `notifications/cancelled` only to end a `subscriptions/listen` stream (see
  [notifications.md](notifications.md), SUB-11), because it has no requests of its own to cancel.
- **Where.** Either side in 2024-11-05..2025-11-25; client (plus listen teardown) in 2026-07-28.
- **mcpx @ 05c78b2.** As a legacy server it cancels its own relayed questions when it stops waiting for them
  (`internal/mcpserver/conn.go:258-266`). For 2026 hosts it sends no requests, so it has nothing to cancel.
- **Value to mcpx.** + low: correct.
- **Effort.** S.
- **Risk.** low.
- **Detail.** The 2026 page still opens with "A client **SHOULD** send a cancellation notification", generically, although
  on HTTP the client cancels by closing (PC-13).
- **Sources.** `2024-11-05/basic/utilities/cancellation.mdx:7` "Either side can send a cancellation notification to";
  `2026-07-28/basic/patterns/cancellation.mdx:8` "A client **SHOULD** send a cancellation notification";
  `internal/mcpserver/conn.go:265-266`.

## PC-11 Receiver stops work on `notifications/cancelled`

- **What.** A receiver SHOULD stop processing the cancelled request, free its resources and not send a response; it MAY
  ignore the cancel if the request is unknown, finished, or cannot be cancelled.
- **Where.** Every revision; in 2026 the notification exists only on stdio (PC-13).
- **mcpx @ 05c78b2.** The cancel is recorded in a per-connection map (`internal/mcpserver/server.go:642-647`, `:1521-1538`)
  and `OnCancel` is a field nothing sets (`internal/mcpserver/server.go:134-135`), so no upstream call is interrupted. The
  doc says so and defers it (`docs/protocol.md:88`, `:372-374`).
- **Value to mcpx.** + med: stdio hosts could stop long scripts and calls.
- **Effort.** M — keep a `context.CancelFunc` per request id on each connection; on stdio this first needs requests handled
  off the read loop (a transports-register item).
- **Risk.** med: work runs on after the host gave up; the recorded map on the default stdio connection grows for the
  process lifetime.
- **Detail.** Recording is conformant under the MAY (the request "cannot be cancelled" today), but useless. Over HTTP the
  disconnect path already cancels (PC-12), which makes stdio the gap.
- **Sources.** `2025-06-18/basic/utilities/cancellation.mdx:36` "Receivers of cancellation notifications **SHOULD**:";
  `2026-07-28/basic/patterns/cancellation.mdx:73` "Servers receiving cancellation notifications **SHOULD**:";
  `docs/protocol.md:88` "recorded; mcpx cannot yet interrupt an upstream call mid-flight";
  `docs/protocol.md:372` "Inbound cancellation does not reach an upstream call."; `internal/mcpserver/server.go:134-135`.

## PC-12 HTTP: closing the stream is cancellation (2026), not (legacy)

- **What.** Legacy Streamable HTTP: a disconnect SHOULD NOT be read as cancellation; to cancel, the client SHOULD send
  `notifications/cancelled`. 2026: closing the request's SSE response stream MUST be treated as cancellation of that
  request.
- **Where.** 2025-03-26..2025-11-25 versus 2026-07-28. opencode v2 on a modern HTTP connection aborts the HTTP request.
- **mcpx @ 05c78b2.** When an HTTP host closes its POST, `r.Context()` ends (`internal/mcpserver/server.go:1176-1177`),
  which cancels the backend's `/v1/call` to the daemon (`internal/cli/client.go:197`), the daemon handler's
  `reg.Call` (`internal/daemon/server.go:696`), and the pool call, whose client then cancels upstream
  (`internal/mcpclient/client.go:709-716`). This matches 2026, is not era-gated, and is not described in the docs. For an
  interruptible call the upstream work runs as a daemon task and is deliberately not cancelled
  (`internal/mcpserver/ask.go:157-162`; [mrtr.md](mrtr.md), MRTR-20).
- **Value to mcpx.** + med: already works; document and test it, and gate it by era.
- **Effort.** M — era gating plus a test that drives a real disconnect.
- **Risk.** med: treating a legacy host's dropped stream as a cancel kills work a resuming legacy client expected to keep;
  not treating a 2026 host's as one wastes upstream work.
- **Detail.** Inferred from the context chain; no disconnect was driven on the wire, and buffering in the unix-socket HTTP
  client was not traced. On 2026 HTTP a plain JSON (non-SSE) response has no stream to close, so dropping the connection
  is the only signal.
- **Sources.** `2025-03-26/basic/transports.mdx:122` "Disconnection **SHOULD NOT** be interpreted as the client cancelling its request.";
  `2026-07-28/basic/transports/streamable-http.mdx:235` "Closing the SSE response stream **MUST** be treated by the server as";
  `2026-07-28/basic/patterns/cancellation.mdx:39` "Closing the SSE response stream is the cancellation signal.";
  `internal/mcpserver/server.go:1176-1177`; `internal/daemon/server.go:696`; `internal/mcpclient/client.go:709-716`;
  `v2:packages/core/src/mcp/client.ts:273`.

## PC-13 Client `notifications/cancelled`: stdio only in 2026

- **What.** 2026 stdio: the client MUST send `notifications/cancelled` (there is no per-request stream to close). 2026 HTTP
  defines no client-to-server notifications at all; closing the stream is the cancel and no notification is expected.
  Yet the cancellation page opens with a generic "A client SHOULD send a cancellation notification".
- **Where.** Any transport in 2024-11-05..2025-11-25; stdio only in 2026-07-28.
- **mcpx @ 05c78b2.** On a 2026 HTTP upstream the client does both: the request's own POST is aborted by the same context
  (`internal/mcpclient/http.go:115`), which is the correct signal, and it also sends `notifications/cancelled`
  (`internal/mcpclient/client.go:713-715`), which 2026 HTTP does not define.
- **Value to mcpx.** + low: drop the notification for 2026 HTTP upstreams.
- **Effort.** S.
- **Risk.** low: a strict 2026 server may reject the notification POST.
- **Detail.** Read the SHOULD as applying to stdio. Header requirements for notification POSTs are "not defined by this
  revision", so a dual-era server should not reject a header-less notification; mcpx's own server answers one with 202
  (wire W26).
- **Sources.** `2026-07-28/basic/patterns/cancellation.mdx:42` "There is no per-request stream to close. The client **MUST** send a";
  `2026-07-28/basic/transports/streamable-http.mdx:95` "This revision of the core protocol defines no client-to-server";
  `2026-07-28/basic/transports/streamable-http.mdx:98-104`; `internal/mcpclient/client.go:713-715`;
  `internal/mcpclient/http.go:115`; wire W26.

## PC-14 A caller's abort or timeout cancels the upstream call

- **What.** When the thing that started a tool call gives up (the user presses Esc, a script times out or dies), the
  in-flight upstream request should be cancelled rather than left running.
- **Where.** opencode v1 and v2 pass the call's `AbortSignal` to `callTool`, and the SDK sends
  `notifications/cancelled {requestId, reason}` on abort or timeout (v2 on modern HTTP aborts the request instead). mcpx
  cancels through the request context. lootbox's per-call timeout is a `Promise.race` that abandons the promise without
  cancelling it, and no signal is passed, so a killed script's calls keep running in the daemon until the SDK's own 60 s
  timeout cancels them.
- **mcpx @ 05c78b2.** `/v1/call` passes `r.Context()` to `reg.Call` (`internal/daemon/server.go:696`); when it ends, the
  client sends `notifications/cancelled` upstream (`internal/mcpclient/client.go:709-716`). A script that times out has its
  process group killed (`internal/runner/runner.go:424-438`), which drops its socket requests and so cancels their
  upstream calls the same way.
- **Value to mcpx.** + med: already done; it keeps pool slots and upstream work from leaking.
- **Effort.** S — done.
- **Risk.** low. The opencode plugin's `mcpx_exec` passes no abort signal to the daemon, so a user's cancel in opencode
  does not reach an mcpx script (a code-mode register item).
- **Detail.** The `reason` is always `"timeout"` (PC-08), and inbound cancellation of mcpx itself is PC-11.
- **Sources.** `v1:packages/opencode/src/mcp/catalog.ts:62` "signal: options.abortSignal,";
  `v2:packages/core/src/mcp/client.ts:273`; <https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/shared/protocol.js>
  (L677); `internal/daemon/server.go:696`; `internal/mcpclient/client.go:709-716`;
  `.lootbox/src/lib/rpc/execute_mcp.ts:199-208`; `.lootbox/src/lib/rpc/execute_mcp.ts:61`.
