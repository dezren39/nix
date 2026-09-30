# Transports

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  stdio, HTTP+SSE and Streamable HTTP across revisions: framing, streams, sessions, headers, Origin, and mcpx's server and client sides.
```

MCP runs over stdio (newline-delimited JSON-RPC) and over HTTP: the two-endpoint HTTP+SSE transport of 2024-11-05,
Streamable HTTP from 2025-03-26 with optional `Mcp-Session-Id` sessions and resumable SSE, and the 2026-07-28 reshape
that is POST-only, sessionless, and mirrors body fields into required headers (`Mcp-Method`, `Mcp-Name`,
`Mcp-Param-*`). For mcpx two things matter most: its `/mcp` endpoint runs tools as the user on a loopback port and does
not validate `Origin`, the one MUST in this area with a working browser exploit; and its HTTP client sends none of the
2026 headers, so a conformant 2026 HTTP server rejects every modern request from it.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TR-01 | stdio framing: one message per line, stdout only MCP | `2024-11-05 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — both sides; client skips non-JSON banner lines | + high | S | low |
| TR-02 | stdio `stderr`: logging only, not an error signal | `2025-11-25 has` `opencode v1 has` `opencode v2 has` | `24-11 "logging" · 25-11 any logging, not errors · 26-07 same` · opencode v1 piped unread · v2 drained | ✓ — server silent; client drains to a ring buffer | + low | S | low |
| TR-03 | Maximum stdio frame size | `opencode v2 has` | spec: none · opencode v2 16 MiB · mcpx 64 MiB | ✓ — 64 MiB both sides; larger than opencode v2 accepts | + med | S | med |
| TR-04 | stdio server launch: environment and working directory | `opencode v1 has` `opencode v2 has` | opencode v1 ✓ · opencode v2 ✓ · mcpx ✓ | ✓ — config env and cwd over the daemon's env | + low | S | low |
| TR-05 | stdio requests handled one at a time | `mcpx missing` | none required; cancellation and ping assume concurrency | partial — only calls that may ask the client run concurrently (#202) | + med | M | med |
| TR-06 | 2026 stdio direction rules | `2026-07-28 has` | `26-07 ✓` | ✓ — legacy gets requests, modern gets `input_required` | + med | S | low |
| TR-07 | Custom byte-stream transports reuse stdio framing | `2026-07-28 has` `mcpx has, others don't` | `26-07` SHOULD | partial — MCP over HTTP on a Unix socket, not NDJSON | + low | M | low |
| TR-08 | JSON-RPC messages MUST be UTF-8 | `2025-03-26 has` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — Go encoding/json emits UTF-8 | + low | S | low |
| TR-09 | JSON-RPC batching | `2025-03-26 has` `2025-06-18 removes` `mcpx missing` | `25-03 ✓` only | ✗ — server rejects batches; client splits HTTP, drops stdio (#202) | + low | M | low |
| TR-10 | HTTP+SSE transport (2024-11-05) | `2024-11-05 has` `2025-03-26 deprecates` `2026-07-28 deprecates` `mcpx missing` `opencode v1 has` | `24-11 ✓ · 25-03 dep · 25-06 dep · 25-11 dep · 26-07 dep` · opencode v1 fallback | ✗ — neither served nor spoken; `transport: sse` ignored (#220) | + med | M | med |
| TR-11 | Client fallback from Streamable HTTP to HTTP+SSE | `specs conflict` `mcpx missing` `opencode v1 has` | `25-03` any 4xx · `25-11` 400/404/405 · `26-07` + body check | ✗ — no fallback (#220) | + med | M | med |
| TR-12 | Streamable HTTP methods: POST+GET → POST only | `2025-03-26 has` `2026-07-28 removes` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 POST` | ✓ — POST served, GET 405, DELETE 204 | + low | S | low |
| TR-13 | Standalone GET SSE stream: server side | `2025-03-26 has` `2026-07-28 removes` `has better replacement` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 405` | ✓ — 405 with `Allow: POST, DELETE` | + low | S | low |
| TR-14 | Standalone GET SSE stream: client side | `2025-03-26 has` `mcpx missing` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | ✗ — never opened; unsolicited legacy messages lost (#203) | + med | M | med |
| TR-15 | `Accept: application/json, text/event-stream` on POST | `2025-03-26 has` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — client sends it; server does not require it | + low | S | low |
| TR-16 | Request answered with JSON or SSE, chosen per request | `2025-03-26 has` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — JSON unless mcpx must ask the client something | + med | S | low |
| TR-17 | 202 Accepted for notifications (and legacy responses) | `2025-03-26 has` `2026-07-28 has` | `25-03..25-11` notifications and responses · `26-07` notifications only | partial — known notifications 202; unknown ones get a 200 error | + low | S | low |
| TR-18 | Client responses POSTed back to the server | `2025-03-26 has` `2026-07-28 removes` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 forbidden` | ✓ — routed by session to the waiting request | + med | S | low |
| TR-19 | Server requests on a request's SSE stream | `2025-03-26 has` `2026-07-28 removes` `has better replacement` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 forbidden` | ✓ — legacy elicitation there; modern gets `input_required` | + med | S | low |
| TR-20 | Each message on exactly one stream (no broadcast) | `2025-03-26 has` `2026-07-28 removes` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07` per-request streams | ✓ — only per-request streams exist | + low | S | low |
| TR-21 | SSE resumability via event ids and `Last-Event-ID` | `2025-03-26 has` `2025-11-25 has` `2026-07-28 removes` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓+ · 26-07 —` | ✗ — no event ids sent; no resume attempted | + low | M | low |
| TR-22 | SSE polling: priming event, `retry`, reconnect | `2025-11-25 has` `2026-07-28 removes` `mcpx missing` | `25-11 ✓` only | ✗ — client ignores `retry`; never reconnects (#203) | + low | M | med |
| TR-23 | `X-Accel-Buffering: no` on SSE responses | `2026-07-28 has` | `26-07` SHOULD | ✓ — set on every SSE response | + low | S | low |
| TR-24 | SSE comment lines as keep-alive | `2026-07-28 has` | `26-07` encouraged | ✓ — client ignores comments; server sends none | + low | S | low |
| TR-25 | `Mcp-Session-Id` value and assignment rules | `2025-03-26 has` `2026-07-28 removes` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | ✓ — `sess-` + 32 random hex; never required | + low | S | low |
| TR-26 | `Mcp-Session-Id` minted on `server/discover` | `2026-07-28 removes` `mcpx missing` | `26-07`: do not mint or echo | ✗ — minted for every discover, modern or not (#199) | + med | M | med |
| TR-27 | `Mcp-Session-Id` minted on a failed `initialize` | `2025-03-26 has` `mcpx missing` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓` | ✗ — issued even when the handshake is refused (#202) | + low | S | low |
| TR-28 | Unknown or expired session id → 404 | `2025-03-26 has` `mcpx missing` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ignore` | ✗ — served statelessly as 2025-03-26 (#202) | + med | S | med |
| TR-29 | Client re-initializes after a session 404 | `2025-03-26 has` `opencode v1 has` `opencode v2 has` `mcpx missing` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓` · opencode v1 ✓ · opencode v2 ✓ | ✗ — 404 is a plain error; no re-initialize (#203) | + med | M | med |
| TR-30 | Client sends a session id to modern servers | `2026-07-28 removes` `mcpx missing` | `26-07`: no sessions | ✗ — captured from any response, echoed always (#200) | + low | S | low |
| TR-31 | `DELETE` handled by the server | `2025-03-26 has` `2026-07-28 removes` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 405` | ✓ — 204, even for an unknown session | + low | S | low |
| TR-32 | `DELETE` sent by the client on close | `2025-03-26 has` | `25-03 ✓ · 25-06 ✓ · 25-11 ✓` | ✓ — sent when a session id is held | + low | S | low |
| TR-33 | `MCP-Protocol-Version` header requirement timeline | `2025-06-18 has` `specs conflict` | `25-03` auth only · `25-06 ✓ · 25-11 ✓ · 26-07 ✓ (= _meta)` | partial — sent always; server checks only against `_meta` | + med | S | med |
| TR-34 | Client header value after a legacy handshake | `2025-06-18 has` `mcpx missing` | `25-06 ✓ · 25-11 ✓` | ✗ — always 2025-11-25, not the negotiated version (#203) | + med | S | med |
| TR-35 | Server uses the legacy header as the version | `2025-06-18 has` `mcpx missing` | `25-06 ✓ · 25-11 ✓` | partial — header ignored; no-session requests served as 2025-03-26 (#202) | + med | S | med |
| TR-36 | Invalid legacy header value → 400 | `2025-06-18 has` `mcpx missing` | `25-06 ✓ · 25-11 ✓` | ✗ — `1999-01-01` accepted and served (#202) | + low | S | low |
| TR-37 | `Mcp-Method` header (2026) | `2026-07-28 has` `mcpx missing` | `26-07` REQUIRED | ✗ — client never sends it; server never checks it (#199) | + high | S | high |
| TR-38 | `Mcp-Name` header (2026) | `2026-07-28 has` `mcpx missing` | `26-07` REQUIRED on three methods | ✗ — client never sends it; server never checks it (#199) | + high | S | high |
| TR-39 | Mirror `x-mcp-header` arguments into `Mcp-Param-{Name}` | `2026-07-28 has` `mcpx missing` | `26-07` clients MUST | ✗ — not implemented (#200) | + med | M | med |
| TR-40 | Intermediaries and mirrored headers | `2026-07-28 has` | `26-07 ✓` | n/a — mcpx terminates and re-originates | + low | S | low |
| TR-41 | `Origin` validation against DNS rebinding | `2024-11-05 has` `2025-11-25 has` `mcpx missing` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ 403 · 26-07 ✓ 403` · lootbox — | ✗ — any `Origin` accepted on `/mcp` and `/v1` (#204) | + high | S | high |
| TR-42 | Bind to localhost when running locally | `2024-11-05 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — loopback unless `daemon.address` says otherwise | + med | S | low |
| TR-43 | `/mcp` mounted on the daemon's own listeners | `2025-03-26 has` | mcpx design | ✓ — Unix socket and loopback TCP; `serve --transport http` gone | + med | S | low |
| TR-44 | Client drops server requests with string ids | `2024-11-05 has` `mcpx missing` | `RequestId = string \| number` in every revision | ✗ — ids decoded as int64; frame silently dropped (#203) | + med | S | med |
| TR-45 | Client HTTP timeout caps whole SSE responses | `mcpx missing` | implementation | ✗ — 10 min cap cuts long streams (#203) | + low | S | med |

## TR-01 stdio framing: one message per line, stdout only MCP

- **What.** One JSON-RPC message per line with no embedded newlines; the server writes only valid MCP messages to
  stdout and the client writes only valid MCP messages to stdin.
- **Where.** Every revision. 2026 adds "each message is a single JSON-RPC request, notification, or response" (no
  batches) and UTF-8.
- **mcpx @ 05c78b2.** Server: one object per line through a 64 MiB scanner
  (`internal/mcpserver/server.go:1021-1025`), compact JSON per line under a write lock
  (`internal/mcpserver/server.go:1004-1011`); `mcpx serve` switches the CLI to machine output so nothing else reaches
  stdout (`internal/cli/serve.go:378-382`); stderr was empty in every recorded stdio run. Client: writes each frame plus
  a newline under a lock (`internal/mcpclient/stdio.go:112-121`), skips blank lines and any line that does not start
  with `{` or `[` (`internal/mcpclient/stdio.go:139-142`).
- **Value to mcpx.** + high: this is the path every spawning host uses.
- **Effort.** S — done.
- **Risk.** None observed.
- **Detail.** Skipping banner lines is accept-liberally against the server's MUST; the comment calls it deliberate
  tolerance of servers that log on stdout.
- **Sources.** `2024-11-05/basic/transports.mdx:24` "- Messages are delimited by newlines, and **MUST NOT** contain
  embedded newlines."; `2026-07-28/basic/transports/stdio.mdx:13`; `internal/mcpserver/server.go:1025`;
  `internal/mcpclient/stdio.go:139`; wire S1–S5.

## TR-02 stdio `stderr`: logging only, not an error signal

- **What.** 2024-11-05..2025-06-18: the server MAY write UTF-8 to stderr "for logging purposes". From 2025-11-25: any
  logging (informational, debug, error), the client MAY capture, forward or ignore it, and SHOULD NOT treat it as an
  error indicator. 2026 names stderr as the migration path for the deprecated Logging feature.
- **Where.** opencode v1 spawns with `stderr: "pipe"` and never reads it; a chatty server could block once the pipe
  fills (inferred from stream back-pressure, not reproduced). opencode v2 drains it line by line into a debug log "so
  chatty servers cannot stall on a full pipe".
- **mcpx @ 05c78b2.** Server: `mcpx serve` writes nothing to stderr (wire S1–S5). Client: drains upstream stderr
  continuously into a 64 KiB ring buffer (`internal/mcpclient/stdio.go:92`, `:101`) and quotes its tail only inside
  error messages (`internal/mcpclient/stdio.go:119`, `:131`); it never treats stderr as failure. The pool sets no
  `StderrTo`, so upstream stderr is not logged anywhere else (`internal/pool/pool.go:369-375`).
- **Value to mcpx.** + low: done; forwarding upstream stderr to mcpx's logs would help diagnosis.
- **Effort.** S.
- **Risk.** Treating stderr as fatal breaks chatty servers; mcpx does not.
- **Detail.** Changelog 2025-11-25 minor item 1 (PR #670).
- **Sources.** `2024-11-05/basic/transports.mdx:25` "The server **MAY** write UTF-8 strings to its standard error
  (`stderr`) for logging"; `2025-11-25/basic/transports.mdx:31`; `internal/mcpclient/stdio.go:92`;
  `internal/mcpclient/stdio.go:101`; `v1:packages/opencode/src/mcp/index.ts:348` "stderr: \"pipe\","; 
  `v2:packages/core/src/mcp/stdio.ts:162`.

## TR-03 Maximum stdio frame size

- **What.** No revision sets a maximum message size, so every implementation picks one, and a sender larger than the
  receiver's limit kills the connection.
- **Where.** opencode v2 fails the connection if one line exceeds 16 MiB ("MCP stdio frame exceeded 16 MiB"), with an
  outgoing queue of 64 frames; no cap was found in opencode v1.
- **mcpx @ 05c78b2.** Server stdio reads up to 64 MiB per line (`internal/mcpserver/server.go:1025`); HTTP bodies are
  read through a 64 MiB `LimitReader` (`internal/mcpserver/server.go:1132`), so a larger body is truncated and fails as
  a parse error. Client stdio and SSE use `plumbing.stdioMaxLine`, 64 MiB (`internal/mcpclient/stdio.go:49`;
  `internal/defaults/defaults.json:41`; `internal/mcpclient/http.go:166`).
- **Value to mcpx.** + med: an `mcpx_exec` or `mcpx_call` result over 16 MiB drops opencode v2's connection to mcpx.
- **Effort.** S — cap or spill results below the smallest known host limit.
- **Risk.** One oversized result silently removes mcpx from an opencode v2 Location until reconnect.
- **Detail.** The server-side limits are inline numbers while the client's is a setting, against mcpx's "nothing
  hardcoded" rule.
- **Sources.** `v2:packages/core/src/mcp/stdio.ts:15` "const MAX_FRAME_BYTES = 16 * 1024 * 1024";
  `v2:packages/core/src/mcp/stdio.ts:117`; `internal/mcpserver/server.go:1025`; `internal/mcpserver/server.go:1132`;
  `internal/defaults/defaults.json:41`.

## TR-04 stdio server launch: environment and working directory

- **What.** How a client spawns a stdio server: command, arguments, working directory and environment. The spec leaves
  this to the client.
- **Where.** opencode v1 and v2 run `command[0]` with the rest as arguments, cwd from config resolved against the
  directory (default: the directory), env = host env plus the config's `environment`. v2 spawns through the Location's
  environment, so workspace-backed Locations run servers remotely, and with v2's background daemon "host env" is the
  daemon's.
- **mcpx @ 05c78b2.** The pool launches with the config's command, args, env and cwd and `InheritEnv: true`
  (`internal/pool/pool.go:369-375`); the child's environment is the daemon's plus the config's
  (`internal/mcpclient/stdio.go:60-66`), in its own process group (`internal/mcpclient/stdio.go:69`).
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** `mcpx serve` spawned by opencode inherits opencode's process env, not the per-session shell env (so no
  `MCPX_SESSION_ID`); upstreams spawned by the mcpx daemon inherit the daemon's env, not the caller's.
- **Sources.** `internal/pool/pool.go:369-375`; `internal/mcpclient/stdio.go:60-66`;
  `v1:packages/opencode/src/mcp/index.ts:346`; `v2:packages/core/src/mcp/stdio.ts:84` "extendEnv: true,".

## TR-05 stdio requests handled one at a time

- **What.** Over stdio, mcpx handles each request on the read loop, except those that may need to ask the client
  something. A 120 s `mcpx_exec` blocks `ping`, `notifications/cancelled` and every other request until it returns.
- **Where.** No revision requires concurrency, but cancellation and `ping` assume the receiver keeps reading while it
  works; 2026 stdio makes `notifications/cancelled` the client's MUST.
- **mcpx @ 05c78b2.** `internal/mcpserver/server.go:1051-1067`; `mayBlockOnClient` covers only `tools/call`,
  `prompts/get` and `resources/read`, and only for a client that can answer (`internal/mcpserver/ask.go:318-327`).
- **Value to mcpx.** + med: cancellation and liveness on stdio; hosts that ping during long calls stop treating mcpx
  as hung.
- **Effort.** M — dispatch every request on its own goroutine; the writer is already locked.
- **Risk.** A host with a ping watchdog kills mcpx during long scripts.
- **Detail.** Reply order is the stated reason for in-line handling (`internal/mcpserver/server.go:1051-1054`), but
  JSON-RPC does not require ordered replies.
- **Sources.** `internal/mcpserver/server.go:1063` "} else if resp := s.HandleOn(ctx, c, req); resp != nil {";
  `2026-07-28/basic/patterns/cancellation.mdx:42-43`; `internal/mcpserver/ask.go:318-327`.

## TR-06 2026 stdio direction rules

- **What.** On 2026 stdio the server MUST NOT write JSON-RPC requests to stdout (only responses, request-scoped
  notifications and listen-stream notifications), and the client MUST NOT write responses. Server-to-client
  interaction goes through `InputRequiredResult`.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Modern peers get `input_required` (`internal/mcpserver/ask.go:262`); legacy connections still get
  real requests (`internal/mcpserver/conn.go:255-259`), which is right per era.
- **Value to mcpx.** + med: done.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** Because stdio shares one channel, clients MUST demultiplex listen notifications by
  `io.modelcontextprotocol/subscriptionId` (`meta.md` SUB-05) and progress by `progressToken`.
- **Sources.** `2026-07-28/basic/transports/stdio.mdx:55` "The server **MUST NOT** write JSON-RPC _requests_ to
  `stdout`."; `2026-07-28/basic/transports/stdio.mdx:36`; `internal/mcpserver/ask.go:262`.

## TR-07 Custom byte-stream transports reuse stdio framing

- **What.** 2026 says newline-delimited JSON-RPC works unchanged over Unix sockets or TCP, and custom transports on
  such streams SHOULD reuse it; only launch, stderr and shutdown need equivalents. Custom transports MUST preserve the
  per-request metadata model.
- **Where.** 2026-07-28. Earlier revisions only say custom transports must preserve the JSON-RPC format and lifecycle.
- **mcpx @ 05c78b2.** MCP is served as Streamable HTTP over the daemon's Unix socket and loopback TCP
  (`internal/daemon/server.go:198-219`, `:483-488`), not as raw NDJSON on the socket.
- **Value to mcpx.** + low: an NDJSON-over-Unix-socket endpoint would be a cheap, header-free surface for local agents.
- **Effort.** M.
- **Risk.** None.
- **Detail.** Serving MCP over a Unix socket at all is something mcpx has that the other products compared here do
  not.
- **Sources.** `2026-07-28/basic/transports/index.mdx:75` "Unix domain sockets or TCP) **SHOULD** reuse the";
  `2026-07-28/basic/transports/stdio.mdx:26`; `internal/daemon/server.go:488`.

## TR-08 JSON-RPC messages MUST be UTF-8

- **What.** From 2025-03-26 the transports page says JSON-RPC messages MUST be UTF-8 encoded; 2024-11-05 mentions
  UTF-8 only for stderr.
- **Where.** 2025-03-26..2026-07-28.
- **mcpx @ 05c78b2.** Go's `encoding/json` emits UTF-8 (`internal/mcpserver/server.go:1004-1011`).
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** RFC 8259 already requires UTF-8 for JSON exchanged between systems.
- **Sources.** `2025-03-26/basic/transports.mdx:5` "JSON-RPC messages **MUST** be UTF-8 encoded.";
  `2026-07-28/basic/transports/index.mdx:29`.

## TR-09 JSON-RPC batching

- **What.** 2025-03-26 adds `JSONRPCBatchRequest` and `JSONRPCBatchResponse`: implementations MAY send batches but MUST
  support receiving them, on stdio and as HTTP POST bodies. 2025-06-18 removes batching from schema and prose.
- **Where.** 2025-03-26 only.
- **mcpx @ 05c78b2.** mcpx lists 2025-03-26 in `Supported` (`internal/mcpserver/server.go:778`) but accepts no arrays:
  stdio unmarshals into a single request and answers `-32700` with no id (`internal/mcpserver/server.go:1040-1044`;
  wire S3); HTTP answers 400 with `-32700` (`internal/mcpserver/server.go:1153-1157`; wire W16). The client splits
  batch arrays on HTTP (`internal/mcpclient/http.go:197-210`); on stdio it passes them up and `recvLoop` drops them
  because they do not decode into a struct (`internal/mcpclient/client.go:356-368`).
- **Value to mcpx.** + low: honest 2025-03-26 support requires it; few clients batch.
- **Effort.** M — split arrays, answer with an array of responses, omit notifications; or drop 2025-03-26 from
  `Supported`, which is honest but loses clients.
- **Risk.** A 2025-03-26 client that batches gets a parse error from a server that claimed 2025-03-26.
- **Detail.** 2025-03-26 HTTP: a POST of only notifications or responses gets 202; one containing requests gets JSON or
  SSE; SSE responses may themselves be batched; `initialize` must not be batched (`lifecycle-versioning.md` LV-12).
- **Sources.** `schema/2025-03-26/schema.ts:17` "export type JSONRPCBatchRequest = (JSONRPCRequest |
  JSONRPCNotification)[];"; `2025-03-26/basic/index.mdx:97` "batches, but **MUST** support receiving JSON-RPC
  batches."; `2025-06-18/changelog.mdx:12`; `internal/mcpserver/server.go:1041-1044`; wire W16, S3.

## TR-10 HTTP+SSE transport (2024-11-05)

- **What.** The 2024-11-05 HTTP transport: the client opens an SSE endpoint, the server MUST first send an `endpoint`
  event naming the POST URI, the client POSTs every message there, and server messages arrive as SSE `message`
  events. Streamable HTTP replaced it in 2025-03-26.
- **Where.** Native in 2024-11-05; later revisions describe it only as a compatibility fallback, and the 2026
  deprecation registry lists it with an earliest removal of three months after SEP-2596 reaches Final, shorter than the
  twelve-month minimum stated for everything else. opencode v1 falls back to it for remote servers; v2 does not.
- **mcpx @ 05c78b2.** Not served. The client speaks Streamable HTTP to every `url:` server: `transport` defaults to
  `http` and is not consulted for the wire shape, so `transport: "sse"` is silently treated as Streamable HTTP
  (`internal/config/config.go:706-708`; `internal/pool/pool.go:368-381`). The SSE reader handles only `data:` lines and
  would not recognise an `endpoint` event (`internal/mcpclient/http.go:180-192`).
- **Value to mcpx.** + med: some hosted servers still speak only HTTP+SSE, and the client fallback is cheap. − serving
  it is not worth it.
- **Effort.** M for the client fallback; L to serve it.
- **Risk.** mcpx cannot reach 2024-11-05-only remote servers, and a config asking for SSE is quietly ignored.
- **Detail.** The client fallback procedure is TR-11.
- **Sources.** `2024-11-05/basic/transports.mdx:61` "The server **MUST** provide two endpoints:";
  `2024-11-05/basic/transports.mdx:67`; `2026-07-28/deprecated.mdx:31`; `2026-07-28/changelog.mdx:110`;
  `internal/pool/pool.go:368` "if p.cfg.Stdio() {"; `v1:packages/opencode/src/mcp/index.ts:278`.

## TR-11 Client fallback from Streamable HTTP to HTTP+SSE

- **What.** 2025-03-26: POST an `InitializeRequest`; if it fails with any 4xx, GET the URL and expect an `endpoint`
  event. 2025-11-25: only on 400, 404 or 405. 2026-07-28: POST a modern request; fall back only on 400, 404 or 405
  **whose body is not a recognised modern JSON-RPC error**, otherwise stay modern.
- **Where.** Each of 2025-03-26, 2025-11-25 and 2026-07-28 words it differently. opencode v1 tries Streamable HTTP,
  then SSE, stopping on authentication errors; opencode v2 has no fallback.
- **mcpx @ 05c78b2.** No fallback (TR-10; `internal/pool/pool.go:368-381`).
- **Value to mcpx.** + med: a dual-era client should implement the 2026 version, the strictest.
- **Effort.** M.
- **Risk.** A client following the 2025-03-26 wording classifies a modern server's 400 `-32022` as HTTP+SSE.
- **Detail.** 401 is a 4xx, so the literal 2025-03-26 rule makes authentication failures look like an old transport.
  As a server, mcpx's 200-status modern errors and non-JSON-RPC 400 bodies (`errors.md` ERR-07, ERR-13) make a 2026
  client's classification of mcpx less reliable.
- **Sources.** `2025-03-26/basic/transports.mdx:271` "- If it fails with an HTTP 4xx status code";
  `2025-11-25/basic/transports.mdx:303`; `2026-07-28/basic/transports/streamable-http.mdx:729`;
  `2026-07-28/basic/transports/streamable-http.mdx:724-735`; `v1:packages/opencode/src/mcp/index.ts:269`.

## TR-12 Streamable HTTP methods: POST+GET → POST only

- **What.** Legacy Streamable HTTP MUST expose one endpoint supporting POST and GET, optionally DELETE. 2026 requires
  only POST.
- **Where.** POST and GET in 2025-03-26..2025-11-25; POST in 2026-07-28.
- **mcpx @ 05c78b2.** POST handled, GET answered 405, DELETE answered 204 after dropping the session
  (`internal/mcpserver/server.go:1119-1131`).
- **Value to mcpx.** + low: legacy allows "SSE or 405" on GET, and 405 is the 2026 SHOULD.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** For DELETE, 2026 says a modern-only server SHOULD answer 405; mcpx's 204 is right for its legacy sessions
  (TR-31).
- **Sources.** `2025-03-26/basic/transports.mdx:69` "**MCP endpoint**) that supports both POST and GET methods.";
  `2026-07-28/basic/transports/streamable-http.mdx:47`; `internal/mcpserver/server.go:1119-1131`.

## TR-13 Standalone GET SSE stream: server side

- **What.** 2025-03-26..2025-11-25: a client MAY GET the endpoint to open an SSE stream for unsolicited server requests
  and notifications; the server returns SSE or 405 and MUST NOT send responses there unless resuming. 2026 removes the
  GET stream; long-lived notifications use `subscriptions/listen` over POST, and GET SHOULD get 405.
- **Where.** 2025-03-26..2025-11-25; 405 in 2026-07-28.
- **mcpx @ 05c78b2.** 405 with `Allow: POST, DELETE` and a text/plain body "mcpx sends no unsolicited messages; POST a
  request" (`internal/mcpserver/server.go:1124-1131`; wire W27).
- **Value to mcpx.** + low: correct in both eras.
- **Effort.** S — done.
- **Risk.** Legacy HTTP hosts never hear a `list_changed` from mcpx; the capability is truthfully declared false over
  HTTP (capabilities register).
- **Detail.** The 405 body is not JSON-RPC, which is fine for a GET.
- **Sources.** `2025-03-26/basic/transports.mdx:134`; `2025-11-25/basic/transports.mdx:141` "or else return HTTP 405
  Method Not Allowed"; `2026-07-28/basic/transports/streamable-http.mdx:683`; `2026-07-28/changelog.mdx:18`;
  `internal/mcpserver/server.go:1129`; wire W27.

## TR-14 Standalone GET SSE stream: client side

- **What.** A legacy Streamable HTTP server sends `list_changed`, `resources/updated` and server requests outside any
  POST on the GET stream; a client that never opens it never receives them.
- **Where.** 2025-03-26..2025-11-25 (the client MAY open it); replaced in 2026 by `subscriptions/listen`.
- **mcpx @ 05c78b2.** `HTTPTransport` only POSTs and DELETEs (`internal/mcpclient/http.go:97-162`, `:235-254`).
- **Value to mcpx.** + med: cache invalidation and resource updates from remote legacy servers.
- **Effort.** M.
- **Risk.** Remote servers' tool lists go stale until `mcpx refresh`.
- **Detail.** stdio upstreams deliver the same messages on the pipe. For 2026 upstreams the equivalent is a listen
  request, which the client also never opens (notifications register).
- **Sources.** `2025-11-25/basic/transports.mdx:135-141`; `internal/mcpclient/http.go:115`.

## TR-15 `Accept: application/json, text/event-stream` on POST

- **What.** The client MUST send an `Accept` header listing both `application/json` and `text/event-stream` on every
  POST (and `text/event-stream` on the legacy GET).
- **Where.** 2025-03-26..2026-07-28.
- **mcpx @ 05c78b2.** The client sends it on every POST (`internal/mcpclient/http.go:80`). The server never reads
  `Accept` and answers requests that omit it, an accept-liberally choice.
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** None.
- **Sources.** `2025-03-26/basic/transports.mdx:88`; `2026-07-28/basic/transports/streamable-http.mdx:76` "2. The client
  **MUST** include an `Accept` header listing both"; `internal/mcpclient/http.go:80`.

## TR-16 Request answered with JSON or SSE, chosen per request

- **What.** For a POSTed request the server MUST answer with `Content-Type: application/json` (one object) or
  `text/event-stream` (an SSE stream); clients MUST handle both.
- **Where.** 2025-03-26..2026-07-28.
- **mcpx @ 05c78b2.** Plain JSON unless mcpx has to send the client a request mid-call (legacy elicitation or
  sampling), in which case the response turns into SSE lazily (`internal/mcpserver/server.go:1299-1324`; wire L2).
  Frames carry only `data:` lines, with `Cache-Control: no-cache` (`internal/mcpserver/server.go:1310`, `:1319`).
  Without an `http.Flusher` the stream cannot start, `ErrNoPush` is returned and the question stays with the broker
  (`internal/mcpserver/server.go:1302-1307`). The client handles both content types.
- **Value to mcpx.** + med: plain calls pay nothing for streaming.
- **Effort.** S — done.
- **Risk.** No progress or log notification ever rides the stream (progress and logging registers).
- **Detail.** None.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:88-90`; `internal/mcpserver/server.go:1311`;
  `internal/mcpserver/server.go:1319`; wire L2.

## TR-17 202 Accepted for notifications (and legacy responses)

- **What.** Legacy: a POST carrying only responses or notifications gets 202 with no body, or an HTTP error whose body
  MAY be an id-less JSON-RPC error. 2026: only notifications get 202, clients MUST NOT POST responses, and 2026 defines
  no client-to-server notification for HTTP at all (cancellation is closing the stream).
- **Where.** 2025-03-26..2025-11-25; narrowed in 2026-07-28.
- **mcpx @ 05c78b2.** 202 for a delivered client response (`internal/mcpserver/server.go:1142-1150`) and for known
  notifications (`internal/mcpserver/server.go:1188-1190`; wire W26). An unrecognised notification is answered with a
  `-32601` error and status 200 (notifications register). The client treats 202 and 204 as acknowledged
  (`internal/mcpclient/http.go:132`).
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** 2026 leaves header requirements for notification POSTs undefined, so a dual-era server should not reject
  a header-less notification.
- **Sources.** `2025-03-26/basic/transports.mdx:98` "- If the server accepts the input, the server **MUST** return HTTP
  status code 202"; `2026-07-28/basic/transports/streamable-http.mdx:95` "This revision of the core protocol defines
  no client-to-server"; `2026-07-28/basic/transports/streamable-http.mdx:102`; `internal/mcpserver/server.go:1189`;
  wire W26.

## TR-18 Client responses POSTed back to the server

- **What.** In legacy Streamable HTTP, a client answers a server request (elicitation, sampling, roots) by POSTing the
  JSON-RPC response. 2026 forbids clients sending responses at all.
- **Where.** 2025-03-26..2025-11-25.
- **mcpx @ 05c78b2.** A response POSTed with the session header is routed to the request waiting on that session and
  acknowledged 202 (`internal/mcpserver/server.go:1142-1151`; wire L3). With no waiter the client gets 400 and a
  non-JSON-RPC body (`internal/mcpserver/server.go:1146`). `replyOf` parses the id as `*int64`
  (`internal/mcpserver/conn.go:306-320`), which is enough because mcpx picks its own negative integer ids.
- **Value to mcpx.** + med: legacy elicitation over HTTP works end to end.
- **Effort.** S — done.
- **Risk.** None for mcpx's own ids.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:81` "The client **MUST NOT** send JSON-RPC
  _responses_."; `internal/mcpserver/server.go:1149`; wire L3.

## TR-19 Server requests on a request's SSE stream

- **What.** Legacy servers MAY send JSON-RPC requests and notifications on a POST's SSE stream before the response, and
  those notifications SHOULD relate to the originating request. 2026: servers MUST NOT send requests on any stream;
  notifications MUST relate to the originating request; the response SHOULD end the stream.
- **Where.** 2025-03-26..2025-11-25; replaced by MRTR in 2026-07-28.
- **mcpx @ 05c78b2.** Legacy: elicitation goes out on the POST's own SSE stream and the answer comes back by separate
  POST (`docs/protocol.md:183`; wire L1–L3); the stream carries the result and then ends
  (`internal/mcpserver/server.go:1179-1186`). Modern: `input_required`.
- **Value to mcpx.** + med: done per era.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The legacy wording is SHOULD for "relate", 2026's is MUST.
- **Sources.** `2025-03-26/basic/transports.mdx:111` "- The server **MAY** send JSON-RPC _requests_ and
  _notifications_ before sending a"; `2026-07-28/basic/transports/streamable-http.mdx:117` "- The server **MUST NOT**
  send independent JSON-RPC _requests_ on this stream."; `docs/protocol.md:183`; wire L2.

## TR-20 Each message on exactly one stream (no broadcast)

- **What.** Legacy: a client MAY hold several SSE streams, and the server MUST send each message on exactly one of them.
  2026: every request has its own stream; listen notifications go only on the matching listen stream, and
  request-scoped notifications never go on a listen stream.
- **Where.** 2025-03-26..2025-11-25; per-subscription filtering in 2026-07-28.
- **mcpx @ 05c78b2.** mcpx opens no GET streams (`internal/mcpserver/server.go:1124-1131`), so its only streams are
  per-request ones.
- **Value to mcpx.** + low.
- **Effort.** S — nothing to do.
- **Risk.** None.
- **Detail.** 2026's per-subscription filter replaces "any unsolicited message on any GET stream".
- **Sources.** `2025-03-26/basic/transports.mdx:153` "streams; that is, it **MUST NOT** broadcast the same message
  across multiple streams."; `2026-07-28/basic/transports/streamable-http.mdx:132`.

## TR-21 SSE resumability via event ids and `Last-Event-ID`

- **What.** 2025-03-26: servers MAY attach SSE `id`s (unique per session and client); clients SHOULD resume a broken
  stream with GET and `Last-Event-ID`; servers MAY replay only that stream's messages. 2025-11-25: ids SHOULD encode the
  originating stream, and resumption is always by GET however the stream began. 2026: not supported; a broken stream
  loses the request and the client MUST re-issue it with a new id; modern-only servers SHOULD ignore `Last-Event-ID`.
- **Where.** 2025-03-26..2025-11-25.
- **mcpx @ 05c78b2.** The server writes no `id:` field (`internal/mcpserver/server.go:1319`); the client reads only
  `data:` lines and never sends `Last-Event-ID` (`internal/mcpclient/http.go:180-192`). The `Last-Event-ID` handling in
  `internal/daemon/server.go:807-823` is mcpx's own `/v1` event feed, not MCP.
- **Value to mcpx.** + low: clients re-issue anyway, and replay needs server-side buffering.
- **Effort.** M.
- **Risk.** None; every part is MAY or SHOULD.
- **Detail.** Whether a dropped stream means the client cancelled (legacy: SHOULD NOT be read as cancellation; 2026:
  MUST be) is in the progress/cancellation register.
- **Sources.** `2025-03-26/basic/transports.mdx:162` "1. Servers **MAY** attach an `id` field to their SSE events";
  `2025-11-25/basic/transports.mdx:186`; `2026-07-28/basic/transports/streamable-http.mdx:157` "Resumable SSE streams
  via `Last-Event-ID` are not supported."; `2026-07-28/changelog.mdx:28`; `internal/mcpserver/server.go:1319`.

## TR-22 SSE polling: priming event, `retry`, reconnect

- **What.** 2025-11-25: when a server opens an SSE stream it SHOULD immediately send an event with an id and empty
  `data` (priming); it MAY then close the connection without ending the stream, SHOULD send `retry` first, and the
  client MUST honour `retry` and reconnect with GET and `Last-Event-ID`.
- **Where.** 2025-11-25 only (SEP-1699).
- **mcpx @ 05c78b2.** The client harmlessly skips an empty priming event (`internal/mcpclient/http.go:174`) but ignores
  `retry:` and `id:` and does not reconnect when the server closes the connection
  (`internal/mcpclient/http.go:180-192`).
- **Value to mcpx.** + low: only matters for upstreams that opt into polling.
- **Effort.** M.
- **Risk.** A 2025-11-25 server in polling mode looks as if it dropped mcpx's request; the result is lost.
- **Detail.** None.
- **Sources.** `2025-11-25/basic/transports.mdx:107` "ID and an empty `data` field in order to prime the client to
  reconnect"; `2025-11-25/basic/transports.mdx:115` "closing the connection. The client **MUST** respect the `retry`
  field,"; `internal/mcpclient/http.go:174`.

## TR-23 `X-Accel-Buffering: no` on SSE responses

- **What.** When starting an SSE response, servers SHOULD send `X-Accel-Buffering: no` so reverse proxies do not buffer
  the stream.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Set on every SSE response (`internal/mcpserver/server.go:1311`; wire L2).
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:136` "When initiating an SSE stream, servers **SHOULD**
  include the"; `internal/mcpserver/server.go:1311`.

## TR-24 SSE comment lines as keep-alive

- **What.** Long-lived streams are encouraged to emit SSE comment lines (starting with `:`) as keep-alive; clients MUST
  NOT treat them as malformed. 2026 recommends this in place of `ping`.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The client skips comment lines (`internal/mcpclient/http.go:183-184`). The server sends none,
  including while a legacy call waits on an elicitation answer.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** A proxy with an idle timeout between host and mcpx could cut a long elicitation wait.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:148` "encouraged to periodically emit an SSE comment
  line (a line beginning with a"; `internal/mcpclient/http.go:184`.

## TR-25 `Mcp-Session-Id` value and assignment rules

- **What.** A legacy server MAY assign a session id in the HTTP response carrying `InitializeResult`. It SHOULD be
  globally unique and cryptographically secure, MUST contain only visible ASCII (0x21–0x7E), and the client MUST echo it
  on every later request; a server that requires sessions SHOULD answer 400 to a request without one. 2025-11-25 spells
  it `MCP-Session-Id` (header names are case-insensitive).
- **Where.** 2025-03-26..2025-11-25; removed in 2026-07-28.
- **mcpx @ 05c78b2.** `sess-` plus 32 hex characters from `crypto/rand` (`internal/mcpserver/server.go:1278-1284`). A
  request without a session is served statelessly rather than refused (`internal/mcpserver/server.go:1214-1218`).
  Idle sessions expire after `proto.sessionIdle` (30 min), but reaping runs only when another session is issued
  (`internal/mcpserver/server.go:1226`, `:1265-1276`).
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** Low.
- **Detail.** If `crypto/rand` fails, `newSessionID` returns the fixed id `sess-0`
  (`internal/mcpserver/server.go:1281`), so every session created during the failure would share one id.
- **Sources.** `2025-03-26/basic/transports.mdx:187` "initialization time, by including it in an `Mcp-Session-Id`
  header on the HTTP"; `2025-03-26/basic/transports.mdx:189`; `2025-03-26/basic/transports.mdx:191`;
  `2025-03-26/basic/transports.mdx:197`; `2025-11-25/basic/transports.mdx:199`; `internal/mcpserver/server.go:1283`.

## TR-26 `Mcp-Session-Id` minted on `server/discover`

- **What.** 2026 has no sessions: a modern-only server SHOULD ignore an incoming `Mcp-Session-Id` and neither mint nor
  echo one.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** `sessionFor` mints a session for `initialize` and for `server/discover`
  (`internal/mcpserver/server.go:1214`, `:1220-1229`), so every modern discover over HTTP gets one (wire W5, W9, M2).
  `docs/protocol.md:181-182` documents this as intended: it is the only way mcpx can bind a `requestState` for a modern
  HTTP client (elicitation and MRTR registers).
- **Value to mcpx.** + med: conformance. − removing it without another binding breaks MRTR for the clients that do
  keep the header.
- **Effort.** M — bind `requestState` to something the request carries.
- **Risk.** A strict 2026 client ignores the header and so gets no working MRTR anyway; the session map grows by one
  per discover probe; a gateway that sees the header may start sticky routing.
- **Detail.** SEP-2567 names the residual problem for gateways that bridge HTTP to stdio by spawning one subprocess per
  session: they need another correlation key (authenticated principal, cookie). The process-model register covers how
  mcpx keys pooled instances.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:685` "- An `Mcp-Session-Id` header on a request:
  ignore it, and do not mint or echo"; `2026-07-28/changelog.mdx:12`; `internal/mcpserver/server.go:1214` "if
  req.Method != \"initialize\" && req.Method != \"server/discover\" {"; `docs/protocol.md:181`;
  `seps/2567-sessionless-mcp.md:243`; wire W5.

## TR-27 `Mcp-Session-Id` minted on a failed `initialize`

- **What.** The legacy session id is assigned "at initialization time" in the response carrying the
  `InitializeResult`; a refused handshake has no result to attach it to.
- **Where.** 2025-03-26..2025-11-25.
- **mcpx @ 05c78b2.** `sessionFor` runs before dispatch (`internal/mcpserver/server.go:1170-1173`), so refused
  handshakes (2024-11-05, 2026-07-28, 9999-01-01) still get a session header (wire W1, W3, W4).
- **Value to mcpx.** + low: no phantom sessions.
- **Effort.** S — issue after a successful result.
- **Risk.** Each failed probe adds a map entry that is reaped only after 30 min and only when another session is
  issued.
- **Detail.** Sending a session for a handshake that never happened is a send-conservatively conflict.
- **Sources.** `2025-11-25/basic/transports.mdx:196-200`; `internal/mcpserver/server.go:1170` "c, issued :=
  s.sessionFor(r, req)"; wire W1.

## TR-28 Unknown or expired session id → 404

- **What.** A legacy server that has terminated a session MUST answer requests carrying that id with 404, which tells
  the client to start a new session with `initialize`.
- **Where.** 2025-03-26..2025-11-25. 2026 servers ignore the header.
- **mcpx @ 05c78b2.** An id mcpx does not hold (deleted, reaped or invented) falls through to a fresh stateless
  connection (`internal/mcpserver/server.go:1209-1219`). Wire L4: a deleted session id got 200 and a 2025-03-26-shaped
  result.
- **Value to mcpx.** + med: clients recover after a daemon restart instead of silently degrading.
- **Effort.** S.
- **Risk.** After a daemon restart every legacy HTTP host drops to 2025-03-26 shapes and loses elicitation, with no
  error to notice; opencode's recovery (TR-29) is triggered only by the 404 mcpx never sends.
- **Detail.** This is also why a stateless legacy client is served as 2025-03-26 (TR-35).
- **Sources.** `2025-11-25/basic/transports.mdx:211-215`; `2025-03-26/basic/transports.mdx:201`;
  `internal/mcpserver/server.go:1218` "return s.newConn(\"\", nil), \"\""; wire L4.

## TR-29 Client re-initializes after a session 404

- **What.** A legacy client that gets 404 for a request carrying a session id MUST start a new session with a fresh
  `initialize`.
- **Where.** 2025-03-26..2025-11-25. opencode v1 (through its SDK patch) re-initializes transparently; opencode v2
  treats a 404, or a 400 "Bad Request: Server not initialized", on a session-carrying connection as an expired session,
  reconnects and retries once, and applies this only to legacy connections.
- **mcpx @ 05c78b2.** A 404 is an ordinary error (`internal/mcpclient/http.go:136-140`); nothing re-initializes. The
  pool would have to reap the instance, and an HTTP transport never dies on its own.
- **Value to mcpx.** + med: transparent recovery after a remote server restarts.
- **Effort.** M — the pool must reconnect the instance.
- **Risk.** A restarted remote server leaves its mcpx instance failing every call.
- **Detail.** None.
- **Sources.** `2025-11-25/basic/transports.mdx:213-215` "it **MUST** start a new session";
  `internal/mcpclient/http.go:139`; `v1:patches/@modelcontextprotocol%2Fsdk@1.29.0.patch:210`;
  `v2:packages/core/src/mcp/client.ts:173`; `v2:packages/core/src/mcp/index.ts:336`.

## TR-30 Client sends a session id to modern servers

- **What.** 2026 has no sessions, so a modern client has no session id to send.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The client captures an `Mcp-Session-Id` from any response (`internal/mcpclient/http.go:126-130`)
  and sends it on every later request, modern ones included (`internal/mcpclient/http.go:89-93`).
- **Value to mcpx.** + low.
- **Effort.** S — capture only on a legacy `initialize`.
- **Risk.** Low; a modern server should ignore it.
- **Detail.** A modern server that wrongly mints one (as mcpx's own server does, TR-26) gets it echoed back.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:685`; `internal/mcpclient/http.go:89-93`.

## TR-31 `DELETE` handled by the server

- **What.** Legacy clients SHOULD DELETE the endpoint with their session id to end a session; the server MAY answer
  405. 2026 modern-only servers SHOULD answer DELETE with 405.
- **Where.** 2025-03-26..2025-11-25.
- **mcpx @ 05c78b2.** Drops the named session and stops its listener; an unknown id still gets 204
  (`internal/mcpserver/server.go:1119-1123`, `:1247-1258`; wire W28). After a DELETE the same id is served statelessly
  (TR-28).
- **Value to mcpx.** + low: right for a dual-era server.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** None.
- **Sources.** `2025-03-26/basic/transports.mdx:204` "the client application) **SHOULD** send an HTTP DELETE to the MCP
  endpoint with the"; `2026-07-28/basic/transports/streamable-http.mdx:683`; `internal/mcpserver/server.go:1120`;
  wire W28.

## TR-32 `DELETE` sent by the client on close

- **What.** The client side of TR-31.
- **Where.** 2025-03-26..2025-11-25.
- **mcpx @ 05c78b2.** On close, if a session id is held, the client sends DELETE with a 5 s timeout
  (`internal/mcpclient/http.go:236-247`).
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The 5 s is an inline constant. Because of TR-30, a modern server that minted an id would also be sent a
  DELETE.
- **Sources.** `2025-03-26/basic/transports.mdx:204`; `internal/mcpclient/http.go:236-247`.

## TR-33 `MCP-Protocol-Version` header requirement timeline

- **What.** 2025-03-26: clients SHOULD send it during OAuth server-metadata discovery only. 2025-06-18 and 2025-11-25:
  clients MUST send it on every request after `initialize`; if it is absent the server SHOULD assume 2025-03-26; an
  invalid or unsupported value gets 400. 2026: every POST MUST carry it and it MUST equal `_meta` `protocolVersion`,
  else 400 and `-32020`; a server supporting pre-2025-06-18 clients MAY treat absence as 2025-03-26, otherwise MUST
  reject.
- **Where.** 2025-03-26 (auth only); 2025-06-18..2026-07-28.
- **mcpx @ 05c78b2.** The client sends it on every POST, following the frame's `_meta` version or else 2025-11-25
  (`internal/mcpclient/http.go:78-85`; TR-34). The server compares it only with `_meta` when both are present
  (`internal/mcpserver/server.go:1159-1168`; `errors.md` ERR-07), never uses it as a version (TR-35) and never validates
  a legacy value (TR-36).
- **Value to mcpx.** + med: needed for modern interop and by intermediaries.
- **Effort.** S.
- **Risk.** See TR-34..TR-36.
- **Detail.** A legacy `initialize` carries no header (only "subsequent requests" must), so a 2025-06-18 server cannot
  demand it on `initialize`. 2026 says versions before 2025-06-18 "did not define" the header, which contradicts
  2025-03-26's authorization section (whose example value is `2024-11-05`).
- **Sources.** `2025-03-26/basic/authorization.mdx:135` "MCP clients _SHOULD_ include the header
  `MCP-Protocol-Version: <protocol-version>` during"; `2025-06-18/basic/transports.mdx:242`;
  `2025-06-18/basic/transports.mdx:256`; `2026-07-28/basic/transports/streamable-http.mdx:252` "Every POST request to
  the MCP endpoint **MUST** include an"; `2026-07-28/basic/transports/streamable-http.mdx:277`;
  `internal/mcpclient/http.go:85`.

## TR-34 Client header value after a legacy handshake

- **What.** After a legacy handshake the header SHOULD be the version negotiated in `initialize`, and a server MUST
  reject an unsupported value with 400.
- **Where.** 2025-06-18, 2025-11-25.
- **mcpx @ 05c78b2.** `setHeadersFor` uses the 2025-11-25 constant unless the frame carries `_meta`
  (`internal/mcpclient/http.go:81-85`); `Negotiated` is never consulted. The header is also sent on `initialize`
  itself, which the spec does not require.
- **Value to mcpx.** + med: remote servers pinned to 2025-06-18 become reachable after the handshake.
- **Effort.** S.
- **Risk.** Every request after `initialize` fails against a server that negotiated 2025-06-18 or 2025-03-26 and
  validates the header.
- **Detail.** Sending a version the server did not agree to is a send-conservatively conflict.
- **Sources.** `2025-11-25/basic/transports.mdx:270-271`; `internal/mcpclient/http.go:81` "version := ProtocolVersion".

## TR-35 Server uses the legacy header as the version

- **What.** From 2025-06-18 the header carries the negotiated version on every legacy HTTP request; the server should
  assume 2025-03-26 only when it has no other way to identify the version.
- **Where.** 2025-06-18, 2025-11-25.
- **mcpx @ 05c78b2.** `peerFor` looks at `_meta`, then the handshake stored on the connection, then `Oldest`
  (`internal/mcpserver/conn.go:133-147`); the header is only compared with `_meta`
  (`internal/mcpserver/server.go:1161-1168`). A legacy client that stays stateless (never echoes the session id) but
  sends `MCP-Protocol-Version: 2025-11-25` is served as 2025-03-26. Wire L4: a reused session id with header 2025-06-18
  got `structuredContent` flattened to text.
- **Value to mcpx.** + med: correct shapes for stateless legacy clients.
- **Effort.** S.
- **Risk.** Such clients silently lose `structuredContent` and `resource_link`.
- **Detail.** Guessing downward is recoverable, which is `docs/protocol.md`'s argument for the floor, but the header is
  the one signal the spec provides for this case. Once it is used, TR-36 matters.
- **Sources.** `2025-11-25/basic/transports.mdx:274-277`; `internal/mcpserver/conn.go:139-145`; wire L4.

## TR-36 Invalid legacy header value → 400

- **What.** A legacy request with an invalid or unsupported `MCP-Protocol-Version` MUST get 400.
- **Where.** 2025-06-18, 2025-11-25.
- **mcpx @ 05c78b2.** The header is checked only when `_meta` carries a version
  (`internal/mcpserver/server.go:1161`). Wire W14: `MCP-Protocol-Version: 1999-01-01`, no `_meta`, no session, served
  with 200.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** None.
- **Sources.** `2025-11-25/basic/transports.mdx:279-280` "**MUST** respond with `400 Bad Request`";
  `internal/mcpserver/server.go:1161`; wire W14.

## TR-37 `Mcp-Method` header (2026)

- **What.** Every 2026 Streamable HTTP request MUST carry `Mcp-Method: <method>` mirroring the body; a missing or
  mismatched value gets 400 and `-32020` (`errors.md` ERR-07). The headers are "REQUIRED for compliance".
- **Where.** 2026-07-28 (SEP-2243).
- **mcpx @ 05c78b2.** The client never sends it (`internal/mcpclient/http.go:78-94`). The server never reads it: wire
  W11 sent `Mcp-Method: prompts/list` with a `tools/list` body and got the tool list with 200.
- **Value to mcpx.** + high: without it a conformant 2026 HTTP server rejects every modern request from mcpx; as a
  server, a policy proxy in front of mcpx that routes on the header can be fooled.
- **Effort.** S.
- **Risk.** High: the modern HTTP upstream path is closed, together with `lifecycle-versioning.md` LV-26.
- **Detail.** Header names are case-insensitive; values are case-sensitive. Validate only modern-era requests, since
  legacy ones carry no such header. 2026 leaves header requirements for notification POSTs undefined. A `-32020` body
  does not contain `-32022`, so the mcpx client then falls back to legacy, which a modern-only server also rejects.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:290` "| `Mcp-Method` | `method` | All requests |";
  `2026-07-28/basic/transports/streamable-http.mdx:293` "These headers are **REQUIRED** for compliance.";
  `internal/mcpclient/http.go:78-94`; wire W11.

## TR-38 `Mcp-Name` header (2026)

- **What.** `Mcp-Name` mirrors `params.name` for `tools/call` and `prompts/get` and `params.uri` for `resources/read`.
  Values that are not safely ASCII MUST use the sentinel form `=?base64?…?=`, and plain values that happen to look like
  the sentinel MUST be encoded too; servers MUST decode before comparing.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Neither sent by the client nor checked by the server (`internal/mcpclient/http.go:78-94`).
- **Value to mcpx.** + high: required for modern HTTP interop; also lets mcpx, as an intermediary, route or rate-limit
  by tool without parsing the body.
- **Effort.** S, including the sentinel encoding.
- **Risk.** As TR-37.
- **Detail.** The tasks extension also requires `Mcp-Name` to carry `params.taskId` on its task methods (tasks
  register).
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:291`; `2026-07-28/basic/transports/streamable-http.mdx:506`
  "To avoid ambiguity, clients **MUST** also Base64-encode any plain-ASCII"; `seps/2663-tasks-extension.md:515`.

## TR-39 Mirror `x-mcp-header` arguments into `Mcp-Param-{Name}`

- **What.** A server MAY annotate primitive `inputSchema` properties with `x-mcp-header: <Name>`; HTTP clients MUST
  then send the argument's value as `Mcp-Param-{Name}`. An absent or null argument means no header; on `-32020` the
  client SHOULD re-list tools and retry; stdio clients MAY ignore the annotation.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** No `x-mcp-header` or `Mcp-Param` anywhere in `internal/` (`internal/mcpclient/http.go:78-94`
  sets no such header).
- **Value to mcpx.** + med: calls to annotated tools on 2026 HTTP upstreams fail `-32020` without it.
- **Effort.** M — schema walk, value encoding, header emission.
- **Risk.** Annotated tools are uncallable through mcpx; intermediaries misroute.
- **Detail.** Values use the same sentinel encoding as `Mcp-Name`; servers SHOULD NOT mark secrets for mirroring.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:364` "While the use of `x-mcp-header` is optional for
  servers, clients **MUST**"; `2026-07-28/basic/transports/streamable-http.mdx:374`;
  `2026-07-28/basic/transports/streamable-http.mdx:408`.

## TR-40 Intermediaries and mirrored headers

- **What.** Intermediaries that do not recognise an `Mcp-Param-{Name}` header MUST forward it; intermediaries that
  route or rate-limit on mirrored headers SHOULD reject requests whose `MCP-Protocol-Version` is absent or older than
  2026-07-28, because older versions never required header and body to agree.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** mcpx terminates and re-originates MCP rather than forwarding HTTP, so this applies only if it
  ever makes policy decisions on headers (`internal/daemon/server.go:488`).
- **Value to mcpx.** + low: guidance for any future header-based policy.
- **Effort.** S.
- **Risk.** Trusting unvalidated legacy headers for policy.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:548` "Intermediate servers that do not recognize an
  `Mcp-Param-{Name}` header"; `2026-07-28/basic/transports/streamable-http.mdx:644`.

## TR-41 `Origin` validation against DNS rebinding

- **What.** Servers MUST validate the `Origin` header on all incoming connections to prevent DNS rebinding; from
  2025-11-25, a present and invalid `Origin` gets 403 (body MAY be an id-less JSON-RPC error). Requests without
  `Origin` (non-browser clients) are unaffected.
- **Where.** Every HTTP transport in every revision. lootbox's `/ws` endpoint on fixed port 9420 accepts WebSocket
  upgrades with no `Origin` check, so any web page can send it a script (cross-site WebSocket hijacking).
- **mcpx @ 05c78b2.** No `Origin` check anywhere on `/mcp`, which is mounted on the loopback TCP listener as well as
  the Unix socket (`internal/daemon/server.go:483-488`); the only middleware records activity
  (`internal/daemon/server.go:290-293`, `:344-353`). Wire W15: `Origin: https://evil.example` → 200. The same gap
  covers `/v1`, where `/v1/exec` decodes JSON whatever the `Content-Type`
  (`internal/daemon/routes_exec.go:212-213`), so a cross-origin `text/plain` "simple" request runs a script without a
  preflight. The TCP port defaults to ephemeral (`daemon.port` 0, `internal/settings/registry.go:343`), which raises the
  attacker's cost to a port scan.
- **Value to mcpx.** + high: mcpx runs tools and scripts as the user, and loopback is exactly what DNS rebinding
  reaches.
- **Effort.** S — reject a present `Origin` that is not on an allowlist (default none, or loopback), require
  `Content-Type: application/json` on POSTs, optionally check `Host`.
- **Risk.** High: a malicious page plus a found port is remote code execution on the workstation.
- **Detail.** Unix-socket callers send no `Origin` and are unaffected. Authentication of `/mcp` and `/v1` is in the auth
  register.
- **Sources.** `2024-11-05/basic/transports.mdx:55` "1. Servers **MUST** validate the `Origin` header on all incoming
  connections to prevent DNS rebinding attacks"; `2025-11-25/basic/transports.mdx:79` "- If the `Origin` header is
  present and invalid, servers **MUST** respond with HTTP 403 Forbidden."; 
  `2026-07-28/basic/transports/streamable-http.mdx:58`; `internal/daemon/server.go:488`;
  `internal/daemon/routes_exec.go:212-213`; `.lootbox/src/lib/rpc/websocket_server.ts:282-290`; wire W15.

## TR-42 Bind to localhost when running locally

- **What.** When running locally, servers SHOULD bind only to 127.0.0.1 rather than all interfaces.
- **Where.** Every HTTP transport in every revision.
- **mcpx @ 05c78b2.** The TCP listener binds loopback unless `daemon.address` says otherwise; the comment calls
  binding wider "somebody's decision rather than a default" (`internal/daemon/server.go:206-213`).
- **Value to mcpx.** + med: done.
- **Effort.** S — done.
- **Risk.** None, but it does not replace TR-41: DNS rebinding reaches loopback.
- **Detail.** None.
- **Sources.** `2024-11-05/basic/transports.mdx:56` "2. When running locally, servers **SHOULD** bind only to
  localhost (127.0.0.1)"; `2025-11-25/basic/transports.mdx:81`; `2026-07-28/basic/transports/streamable-http.mdx:63`;
  `internal/daemon/server.go:206-213`.

## TR-43 `/mcp` mounted on the daemon's own listeners

- **What.** MCP over HTTP is served by the daemon at `proto.mcpPath` (default `/mcp`) on both its Unix socket and its
  loopback TCP port; `mcpx serve --transport http` now fails and names the address to use.
- **Where.** mcpx design.
- **mcpx @ 05c78b2.** `internal/cli/root.go:190-195` (`proto.serveMCP`, a lazily built handler);
  `internal/daemon/server.go:483-489`; `internal/cli/serve.go:373-377`; `internal/defaults/defaults.json:77`.
- **Value to mcpx.** + med: one listener and one access decision, so `/mcp` and `/v1` cannot drift apart.
- **Effort.** S — done.
- **Risk.** The listener is unauthenticated and does not check `Origin` (TR-41).
- **Detail.** Every `/mcp` call goes back to the same daemon over its own `/v1` socket, and the MCP surface is built
  once on first use; both are in the process-model register.
- **Sources.** `internal/daemon/server.go:488` "mux.Handle(path, s.MCP)"; `internal/cli/root.go:190-195`;
  `docs/protocol.md:330`.

## TR-44 Client drops server requests with string ids

- **What.** JSON-RPC ids may be strings or numbers in every revision. A server-initiated request such as
  `elicitation/create` or `roots/list` may therefore arrive with `"id": "abc"`.
- **Where.** `RequestId = string | number` from 2024-11-05 on.
- **mcpx @ 05c78b2.** The client decodes incoming frames with `ID *int64` (`internal/mcpclient/client.go:351-368`), so
  a string id fails to decode and the frame is silently skipped; `handleServerRequest` takes an `int64`
  (`internal/mcpclient/client.go:401`). The server then waits out its own timeout.
- **Value to mcpx.** + med: correctness against servers whose SDKs use string ids.
- **Effort.** S — keep ids as `json.RawMessage`.
- **Risk.** A silent hang, the same class of bug the comment at `internal/mcpclient/client.go:346-350` says was fixed
  once already.
- **Detail.** The server side also parses client replies as `*int64` (`internal/mcpserver/conn.go:306-320`), which is
  fine only because mcpx chooses its own numeric ids.
- **Sources.** `schema/2024-11-05/schema.ts:53` "export type RequestId = string | number;";
  `schema/2025-11-25/schema.ts:122`; `internal/mcpclient/client.go:352`; `internal/mcpclient/client.go:401`.

## TR-45 Client HTTP timeout caps whole SSE responses

- **What.** The upstream HTTP client's timeout also bounds reading an SSE response body, so any stream longer than the
  timeout is cut.
- **Where.** Implementation behaviour.
- **mcpx @ 05c78b2.** `http.Client{Timeout: plumbing.httpRequestTimeout}` (10 min) (`internal/mcpclient/http.go:54-57`,
  `:61`). A legacy call that streams for longer than 10 min, such as a slow elicitation, is cut off.
- **Value to mcpx.** + low: long elicitations over remote HTTP survive.
- **Effort.** S — rely on the context deadline, which `pool.callTimeout` already sets.
- **Risk.** An answer that arrives after 10 min on a remote server is lost.
- **Detail.** None.
- **Sources.** `internal/mcpclient/http.go:61` "hc:       &http.Client{Timeout: timeout},".
