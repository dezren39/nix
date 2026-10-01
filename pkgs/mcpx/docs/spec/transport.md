# mcpx as a server: the transport

What `mcpx serve` (stdio) and the daemon's `/mcp` (Streamable HTTP) do on
the wire, revision by revision, and why. Everything here is tested; the test
names are `<revision>/<area>/<requirement>` in
`internal/mcpserver/transport_test.go` and `internal/e2e/transport_test.go`.

mcpx serves both eras at once: 2024-11-05, 2025-03-26, 2025-06-18 and
2025-11-25, which negotiate with `initialize` (2024-11-05 over stdio and
Streamable HTTP; its own HTTP+SSE server transport is not hosted), and 2026-07-28, which carries its version in
every request's `_meta`. Over HTTP the two share one endpoint, so the first
thing the transport does with a POST is decide which era wrote it.

## Which era a POST belongs to

A POST is **modern** if its body carries
`_meta["io.modelcontextprotocol/protocolVersion"]`, or its
`MCP-Protocol-Version` header names a revision at or after 2026-07-28 (a date
mcpx does not implement still counts: that request is answered with `-32022`,
not treated as legacy). Everything else is **legacy**.

The header is enough on its own because a modern header on a body without the
`_meta` field is exactly the disagreement the modern header rules exist to
catch, and it should be refused as that rather than quietly served as legacy.

## Streamable HTTP, 2026-07-28

- **Mirrored headers.** `MCP-Protocol-Version` and `Mcp-Method` are required
  on every request POST, `Mcp-Name` on `tools/call`, `prompts/get` and
  `resources/read`. Missing, different from the body, or carrying bytes a
  header may not carry (anything outside visible ASCII, space and tab) is
  `400` with `-32020 HeaderMismatch` and the request's id. `Mcp-Name` in the
  `=?base64?…?=` sentinel is decoded before the comparison; the Base64 is
  strict -- padding required, alphabet only -- as the specification's
  test-case table requires. Header names are matched case-insensitively (Go canonicalises
  them), values case-sensitively. A notification POST has no header
  requirements in this revision, so none are demanded, but any it sends must
  still agree.
- **`Mcp-Param-*` headers.** On `tools/call`, every `x-mcp-header`
  annotation in the named tool's `inputSchema` -- mcpx re-lists upstream
  schemas verbatim, so an upstream's annotation counts -- is checked: a
  header that carries bytes a header may not, fails to decode, differs from
  the body value, is missing while the body has a value, or is present while
  the body has none, is `400` with `-32020`. Integers compare numerically.
  The rules live in `internal/mcpheaders`, shared with the client side that
  sends these headers.
- **Status is a function of the response**, in one place (`modernStatus`):
  `-32022`, `-32021`, `-32020`, `-32602`, `-32600`, `-32700` are `400`;
  `-32601` is `404`; everything else `200`. Every `-32602` is `400`, not only
  the missing-`_meta` one the spec names, because a status that depended on
  which handler produced an invalid-params error would not be a function of
  the response, and every invalid-params error is a client error.
- **No sessions.** An `Mcp-Session-Id` on a modern request never binds it to a
  legacy session: an unknown one is ignored (no `404`), and none is echoed or
  minted -- `server/discover` used to mint one so a `requestState` had
  something to bind to; it is now bound to the request instead
  ([messages.md](messages.md#requeststate)).
- **No batches.** An array body is `400 -32600`.
- **Notifications** are `202` with no body; one that is not accepted is `400`
  with an id-less error. An unknown notification method is ignored (`202`):
  JSON-RPC gives a notification no reply, and every revision says to ignore
  what is not understood.
- **Closing the stream is cancellation.** The request runs under the HTTP
  request's context, so a client that disconnects cancels the upstream call
  (the test asserts the backend's context ends, not only the handler's), and
  nothing is written afterwards.
- No `Last-Event-ID`, no SSE event ids.

## Streamable HTTP, 2025-03-26 … 2025-11-25

- **Sessions** are minted by `initialize`. A request naming an unknown,
  expired or deleted session is `404`, which tells the client to initialize
  again. `DELETE` with a session ends it and closes its GET stream (`204`);
  without one it is `400` (mcpx does allow termination, so `405` would be
  wrong); for an unknown one `404`. A legacy request with no session header is
  served statelessly, as before.
- **`MCP-Protocol-Version`** present and not a legacy revision mcpx implements
  is `400` with `-32022` and the legacy list. Absent means 2025-03-26 for a
  sessionless request. In a session, the version negotiated at `initialize`
  governs even if the header names another supported one: the spec says the
  client SHOULD send the negotiated version and the server may rely on
  "the protocol version negotiated during initialization", so a disagreeing
  header is a client quirk to tolerate, not a reason to refuse.
- **Batches** exist only in 2025-03-26, where a server MUST accept them;
  2025-06-18 removed them. So a batch is accepted when the session negotiated
  2025-03-26, or a sessionless request has no header or a 2025-03-26 one, and
  is otherwise `400 -32600`. Elements run concurrently; responses in a batch
  are delivered to waiting questions; the reply is a JSON array (or the
  elements' replies as separate events if something had to stream); a batch of
  only notifications and responses gets `202`. `initialize` inside a batch is
  refused as that element (2025-03-26 lifecycle: it MUST NOT be batched), and
  mints nothing.
- **GET** without a session, or with a modern header, is `405`; with an
  unknown session `404`; otherwise it opens the session's notification stream
  (`text/event-stream`, `X-Accel-Buffering: no`). One per session: a second is
  `409`, because the spec forbids broadcasting a message across streams and a
  stray second stream would silently take half the notifications. The stream
  carries a `:` comment every `transport.sseKeepAlive`, closes on `DELETE`,
  and carries `notifications/{tools,prompts,resources}/list_changed` (which a
  legacy client opts into simply by mcpx declaring `listChanged`) and
  `notifications/resources/updated` for `resources/subscribe`. A subscription
  made before the stream opens delivers once it does; with no stream open a
  notification is dropped, since mcpx keeps no redelivery buffer and the
  legacy transport lets a client choose not to listen.
- **Cancellation.** In a session, a dropped connection is *not* a
  cancellation — the legacy revisions say it SHOULD NOT be read as one — and
  `notifications/cancelled` posted on the session cancels the request's
  context and withholds its reply (the POST ends as an event stream with
  nothing on it, the only shape that carries no response). Without a session
  no cancellation could ever reach the request, so there the disconnect
  remains the signal.
- Responses and notifications: `202` no body; a response nothing is waiting
  for is `400`.

## Origin (every revision with Streamable HTTP)

A request whose `Origin` is present and not allowed is `403` with an id-less
JSON-RPC error, on every method. Allowed: no `Origin` at all (every non-browser
client), the loopback hosts (`transport` defaults: `localhost`, `127.0.0.1`,
`::1`) at any port over http or https, the daemon's own `daemon.address` when
it is a specific address, and the exact origins in
`transport.allowedOrigins`. The daemon's address is named rather than taken
from the request's `Host`, which is precisely what DNS rebinding controls.
`Origin: null` is refused: it names nobody.

## stdio, every revision

- **Requests run concurrently**, each under its own context. Before, they ran
  in line, so a `notifications/cancelled` for a running request could not be
  read until that request had finished and cancellation did nothing.
  `notifications/cancelled` now cancels the context (which reaches the
  upstream call) and the reply is suppressed. Notifications and `initialize`
  stay in line: a notification must land before whatever follows it, and
  `initialize` settles what every later reply looks like. Writes are
  serialised, one frame per line. Replies are no longer in request order,
  which JSON-RPC never promised; the one test that assumed it now matches by
  id.
- **Notifications are never answered**, not even with an error.
- **Batches**: the same per-version rule as HTTP, from the connection's
  negotiated version (or 2025-03-26 before any `initialize`); the reply is an
  array, nothing for notifications only, and `initialize` in a batch is an
  error element.
- **EOF** stops reading. Requests already in flight get `transport.stdioDrain`
  to answer, then are cancelled; the process then exits. A host that closes
  stdin after writing its requests still gets their answers.
- **Before `initialize`**: mcpx is dual-era and accepts liberally, so `ping`
  (and anything else) before `initialize` is answered under the oldest
  revision's shapes. A modern client sends no `initialize` at all, and
  refusing the legacy ones would make the stdio probe order matter for no
  gain.
- **stdout carries only MCP.** Checked against the real binary: initialize,
  list, a successful call with a newline in its text, a tool error, an unknown
  tool and an unknown method, and every stdout line must be one JSON-RPC
  message.
- After a legacy `initialize`, `list_changed` is forwarded as on the GET
  stream: it was declared, so it must be delivered.

## Found along the way

- A legacy client was declared `listChanged: true` on stdio and never sent a
  single `list_changed`: nothing started a notifier for it. Only
  `resources/subscribe` and the modern `subscriptions/listen` did.
- Over HTTP a legacy session declared neither `subscribe` nor `listChanged`,
  honestly, because there was no GET stream to deliver on; now there is, and
  both are declared and delivered.
- stdio answered a notification with an unknown method with a
  method-not-found error carrying no id.
- The previous header check compared `MCP-Protocol-Version` with `_meta` only
  when both were present and answered with a non-JSON-RPC body, so a modern
  POST with no headers at all was served.
