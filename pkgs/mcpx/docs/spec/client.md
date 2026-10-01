# mcpx as a client: every revision

What mcpx sends to the servers it fronts, and what it accepts from them,
revision by revision. The server side is [transport.md](transport.md); how the
era is found is [era-probe.md](era-probe.md). Everything here is tested; test
names are `<revision>/<area>/<requirement>` in
`internal/mcpclient/{conformance_client,http_conformance,http_version}_test.go`,
`internal/pool/sse_test.go` and `internal/e2e/client_test.go`. Every frame the
scripted servers receive is checked against the schema of the revision in use,
in strict mode (`mcpspec.ValidateClientMessageStrict`): a key another revision
defines is a failure, because "send conservatively" is exactly the rule a
lenient check cannot see.

mcpx as a client speaks 2024-11-05, 2025-03-26, 2025-06-18 and 2025-11-25
(legacy, `initialize`) and 2026-07-28 (modern, per-request `_meta`).

## Headers

- **Legacy Streamable HTTP.** `MCP-Protocol-Version` is the version
  `initialize` *returned*, on every request after it. It used to be the
  constant `2025-11-25`, so a server that negotiated down to 2025-06-18 was told
  on every request that the client was speaking a revision it had just declined.
  `initialize` itself carries no header (nothing is agreed yet), and a server
  that negotiated 2025-03-26 or earlier gets none: the header did not exist
  before 2025-06-18.
- **Modern Streamable HTTP.** The header is the version in the frame's `_meta`;
  `Mcp-Method` is on every POST; `Mcp-Name` on `tools/call`, `prompts/get` and
  `resources/read`. `Mcp-Session-Id` is never sent on a modern frame — there are
  no sessions in 2026-07-28, and one minted for an earlier legacy exchange means
  nothing to a modern request. mcpx's own daemon rejects a modern POST without
  the mirrored headers, which is why `TestMcpxIsAModernClientOfMcpx` (one mcpx's
  `/mcp` as another's upstream) is the real proof rather than a fake written to
  agree.
- **Value encoding.** A value outside visible ASCII, space and tab, with
  leading or trailing whitespace, or one that already looks like the sentinel
  (`=?base64?…?=`), is sent as the sentinel around standard Base64. The last
  case matters: a plain `=?base64?literal?=` would otherwise be decoded by the
  server into something the client never sent.

## x-mcp-header

A modern server over HTTP may annotate tool parameters with `x-mcp-header`;
the client MUST mirror them into `Mcp-Param-{Name}` headers.

- **Validation, at `tools/list`.** An annotation must be a non-empty RFC 9110
  token, case-insensitively unique, on an `integer`, `string` or `boolean`
  (never `number`), reachable from the root through `properties` alone. The walk
  visits the *whole* schema, so an annotation under `items`, `anyOf`, `not`,
  `$defs` or at the root is found and makes the tool invalid rather than being
  silently ignored. An invalid tool is **excluded** from the list mcpx returns
  and a `server.warning` lifecycle event names it and the reason. Only on a
  modern HTTP connection: stdio clients MAY ignore annotations, and legacy
  revisions never defined them.
- **Mirroring, at `tools/call`.** The value at the annotated path; absent or
  `null` omits the header; an empty string is sent as an empty header (the
  value was provided). Integers outside ±(2^53−1) and non-integers for an
  integer header are refused before sending — a header the server will reject
  is worse than no call. A connection that has not listed tools lists them once
  before its first call, since the annotations come from the list.
- **`-32020` on a call** means the schema most likely changed since it was
  listed: mcpx reads `tools/list` again and retries once (the transport page's
  SHOULD).

## Results and errors

- **`resultType`.** Absent is `complete`. `complete` and `input_required` are
  understood; anything else is an `InvalidResultError`. It used to be treated as
  complete, so a result meaning something mcpx does not know reached a caller as
  if it were a finished answer.
- **Resource not found** is `ResourceNotFoundError`: `-32002` in every revision,
  and `-32602` from a modern server (2026-07-28 uses `-32602` and asks clients to
  accept `-32002` too). A legacy `-32602` stays invalid-params: in those
  revisions it means the request was wrong, not that the resource is missing.
- **Pagination.** A missing `nextCursor` ends a list in every revision. In
  2026-07-28 an empty string is a cursor ("don't make any determination based on
  cursor value other than whether a non-null value was provided"), so mcpx asks
  again with `""`. The legacy pages never said so, and a legacy server that
  writes an unset cursor as `""` means the end, so there `""` ends the list. A
  cursor that repeats the one just sent also ends it.
- **Version retry.** A `-32022` to `server/discover` is retried with a version
  from `data.supported` — first one not yet tried, then, once, one already tried
  that the server nonetheless lists. The official suite rejects the first
  request with `supported` naming the very version it rejected; retrying it once
  is what "select a mutually supported version and retry" means there. Each
  version is sent at most `upstream.versionAttempts` (2) times, so a server that
  keeps doing it cannot loop the probe.

## Lifecycle and cancellation

- **Negotiated version check.** If `initialize` returns a version mcpx does not
  speak, mcpx disconnects with `UnsupportedVersionError` and does not send
  `notifications/initialized`. Carrying on meant speaking a revision nobody
  agreed to.
- **`initialize` is never cancelled.** Every legacy cancellation page forbids
  it; a timed-out `initialize` used to send `notifications/cancelled`.
- **Modern HTTP:** closing the response stream *is* the cancellation; no
  `notifications/cancelled` is POSTed. **Modern stdio:** the notification MUST
  be sent, and is. **Legacy:** sent -- including for an HTTP request timed out
  while its POST still waited for response headers, which was never cancelled:
  `Send` lasts until the headers arrive, so that timeout surfaced as a send
  error, before the code that sends `notifications/cancelled`.
- **`ping` and `logging/setLevel` are not sent to a modern server** — 2026-07-28
  removed both. `Ping` becomes `server/discover`, which every modern server MUST
  implement and which likewise only answers. A log level becomes
  `io.modelcontextprotocol/logLevel` in every later request's `_meta` (the daemon
  asks for `info`); without it a modern server MUST NOT log at all.
- **A modern response stream that closes without its response** is answered at
  once with an error (`-32000`), where the caller used to wait for its deadline:
  2026-07-28 has no resumption, and "a broken response stream loses the
  in-flight request".
- **Capabilities.** `roots` is `{}` on a modern request (2026-07-28 dropped
  `listChanged` with `notifications/roots/list_changed`), `{"listChanged":
  false}` in `initialize`.

## Notifications from a modern server: subscriptions/listen

A modern server delivers list changes and resource updates only on a
`subscriptions/listen` stream. mcpx opens one per modern connection, asking only
for what the server declared (`tools|prompts|resources.listChanged`, and
`resourceSubscriptions` when `resources.subscribe` is declared and something is
subscribed). `SubscribeResource` on a modern connection adds the URI and
replaces the stream: the old one is ended the way the transport requires
(closing it over HTTP, `notifications/cancelled` naming the listen id on stdio)
and a new one opened with the new filter.

Notifications are correlated by `_meta["io.modelcontextprotocol/subscriptionId"]`
— the stdio MUST. One tagged with the id of a stream mcpx has since replaced is
dropped; untagged ones (progress, log lines, which belong to a request) pass. The
acknowledgement's filter is compared with what was asked and anything declined
is reported as a `server.warning`. When the server ends the stream — gracefully
with a response or not — it is reopened after `upstream.listenReopenDelay`.

The spec disagrees with itself on how a server ends one (subscriptions page: a
successful response; cancellation page: `notifications/cancelled` naming the
listen id). mcpx as a client accepts both: a response ends the pending request,
and a cancelled notification is harmless.

**Resource updates forwarded to mcpx's own clients** now carry the `mcpx://`
URI the client subscribed with; they carried the upstream URI, which matches no
subscription the client holds. The match is on the upstream URI, because the
event names a server and the listing names a namespace that a config may
override and `mcpx serve` cannot see.

## Server requests and elicitation

- **Ids.** A server's request id is kept raw: a string id is as valid as a
  number, and one was dropped as unparseable.
- **Batches.** A 2025-03-26 server may send a JSON array; each element is now
  dispatched. On stdio the array arrived as one line starting with `[` and was
  dropped whole.
- **Declarations.** `elicitation.form` always (mcpx answers `cancel` when nobody
  is listening, which is truthful); `elicitation.url` only with a handler
  installed — the broker stores the URL, shows it (`mcpx elicit list`), takes
  the answer and honours `notifications/elicitation/complete`, so it is carried
  end to end; `sampling` only with a handler. A request in a mode that was not
  declared *for the revision in use* is `-32602`, as the elicitation page
  requires: url mode to a server that negotiated 2025-06-18 is refused even
  though `initialize` offered it, because that revision has no url mode.
- **Defaults.** An accepted form answer's missing fields are filled from the
  requested schema's `default`s ("clients that support defaults SHOULD
  pre-populate form fields"); a field the answerer set is never overwritten.
- **The hang the official suite found.** An `elicitation/create` sent during a
  `tools/call` over Streamable HTTP never reached the broker. The TypeScript
  SDK's `server.request()` inside a tool handler is not related to the POST, so
  it goes to the **standalone GET stream** — which mcpx never opened. mcpx now
  opens it after `initialize` on any Streamable HTTP server that negotiated
  2025-03-26 or later; a server without one answers `405` and that is the end
  of it.

## Resumption (2025-11-25)

A POST's SSE stream that the server closes before the response is resumed:
wait the `retry` the server sent (or `upstream.sseReconnectDelay`), then GET with
`Last-Event-ID`, up to `upstream.sseReconnectAttempts` times. A stream with no
event id cannot be resumed and the caller is told at once. The standalone GET
stream reconnects the same way.

## HTTP+SSE (2024-11-05)

When a Streamable HTTP endpoint refuses both the modern probe and `initialize`
with a bare `400`, `404` or `405` (`LegacyHTTPRefusedError`), the pool GETs the
URL expecting an `endpoint` event, POSTs every message there and reads replies
from the stream. The endpoint must be on the stream's origin: it is where every
message — and the configured credential headers — goes, and a server able to
name any URL could otherwise have mcpx post credentials anywhere. The result is
cached like any era, with `"transport": "http+sse"` in the era record, so the
next start goes straight there without the two refused POSTs.

## Official suite, client leg (before → after)

| set | before | after |
| --- | --- | --- |
| `--requirements 2025-11-25` | 5 passed, 64 failed | 20 passed, 56 failed |
| `--requirements 2026-07-28` | 23 passed, 101 failed, 1 warning | 63 passed, 68 failed, 0 warnings |

Every remaining failure is `auth/*`: mcpx has no OAuth client. One non-auth
scenario, `json-schema-ref-no-deref`, is flaky because of the adapter, not
mcpx; [official-suite.md](official-suite.md#client-both-sets) has the cause
and the current numbers. The
json-schema-2020-12-preservation pass needed the adapter
(`internal/conformance/officialclient`) to echo the schema back as mcpx holds
it; it used to call every tool with `{}`.

## Not done

- **OAuth.** The `auth` config key is applied to every upstream connection
  (`internal/pool` `resolveAuth`, #240): bearer, basic and header become
  headers, env becomes the child's environment, `query` is added to the URL.
  `type: oauth` is refused before connecting. The spec forbids access tokens in
  a query string; `query` exists for services that require it and sends one
  only when you declare it.
