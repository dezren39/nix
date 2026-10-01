# What each says a server should be

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare, area:protocol
description:  the character of an MCP server as each revision and each
              product imagines it, in a paragraph each, and what mcpx has
              to be to satisfy all of them at once.
```

This page is the philosophy, one short account per revision and per product.
The exhaustive list of what a server MUST and SHOULD do, revision by revision,
belongs to the protocol work (`docs/spec/server-obligations.md`, which that work
is writing; it is not on `main` at `05c78b2`). The feature-by-feature table is
[matrix-revisions.md](matrix-revisions.md).

## The revisions

**2024-11-05: a stateful peer over a session.** "MCP provides a stateful
session protocol" (`2024-11-05/architecture/index.mdx:8`). One client and one
server open a session with `initialize`, and version and capabilities are
fixed for its life. Either side may send the other a request at any time: the
server samples and lists roots, both ping. Transports are stdio or a
two-endpoint HTTP+SSE with a persistent event stream. There is no
authorization in the core. The server is a long-lived companion process that
remembers its client.

**2025-03-26: the same session, made deployable on the web.** Streamable HTTP
puts everything on one endpoint, adds optional `Mcp-Session-Id` sessions and
resumable SSE, and treats a dropped connection as *not* a cancellation
(`2025-03-26/basic/transports.mdx:120-122`). OAuth 2.1 arrives with the server
as its own authorization server, along with batching (for one revision), audio,
tool annotations, and a `completions` capability (`2025-03-26/changelog.mdx:9-27`).
The server is still a session. It is now also something you can put behind a
URL.

**2025-06-18: a session that is an OAuth resource server.** Batching goes. Every
HTTP request after initialization carries `MCP-Protocol-Version`. The server
discovers its authorization server through RFC 9728 and validates token
audiences (RFC 8707). Respecting negotiated capabilities becomes a MUST
(`2025-06-18/changelog.mdx:12-26`). Structured tool output, resource links,
`title`, formal `_meta` rules and elicitation all arrive. The server can now
ask the *user* something, and it describes its output with a schema.

**2025-11-25: the most elaborate stateful server.** Sessions, resumable and
now pollable SSE (the server may drop a stream and let the client reconnect),
experimental durable tasks with their own cancel and progress rules, URL-mode
elicitation with its own error code, OIDC discovery, Client ID Metadata
Documents, step-up scopes and icons (`2025-11-25/changelog.mdx:9-40`). The
server is a long-running service with jobs, still one session per connection.

**2026-07-28: a stateless request/response service.** "MCP is a stateless
protocol: every request is self-contained and carries its own protocol version
and capabilities" (`2026-07-28/architecture/index.mdx:8-9`). No handshake, no
session, no GET stream, no resumption, no `ping`, no server-initiated request.
The server asks by returning `input_required` and waiting for the client to
retry. Lists must not vary per connection. Results say how long they may be
cached, and a single opted-in `subscriptions/listen` stream is the only
long-lived construct (`2026-07-28/changelog.mdx:12-34`). Roots, sampling and
logging are deprecated. The server is a function of the request, and anything
that spans requests is a handle the client carries. A server may still serve
legacy clients alongside, as a dual-era server
(`2026-07-28/basic/versioning.mdx:128-130`).

The arc across the five: from *a companion process that remembers its client*
to *a service that remembers nothing it was not handed*. mcpx has to sit at
both ends of that arc at once.

## The products

**opencode v1 (1.18.31)** consumes a *tools-first, text-first* server over one
long-lived connection per server, per directory, per opencode process. It
connects every configured server when MCP is first touched and waits for all
of them (`v1:packages/opencode/src/mcp/index.ts:505`). It never restarts one
that dies (`:448`), and it gives every session in that directory the same
client with nothing identifying the caller
(`v1:packages/opencode/src/mcp/catalog.ts:54`). It declares only `roots`
(`v1:packages/opencode/src/mcp/index.ts:46`), so a server must not need
elicitation, sampling or tasks, and it speaks 2025-11-25 at most. Text, images
and embedded resources reach the model; `audio` and `resource_link` do not
(`v1:packages/opencode/src/session/tools.ts:429`). Server `instructions` and a
declared `resources` capability cost context on every request
(`v1:packages/opencode/src/session/system.ts:129`,
`v1:packages/opencode/src/session/tools.ts:136`). With `OPENCODE_EXPERIMENTAL`
set, as it is on this machine, the tools move behind code mode's `execute`
(`v1:packages/opencode/src/session/tools.ts:388`).

**opencode v2 (2.0.3)** consumes servers *as libraries for its own code mode*.
It holds one connection per server per Location, lives as long as the
background daemon, and shares that connection with every session in every
attached window (`v2:packages/core/src/mcp/index.ts:152`,
`v2:packages/core/src/location-services.ts:52`). It expects typed tools with
names matching `^[A-Za-z0-9_-]{1,64}$` (`v2:packages/core/src/tool.ts:297`). It
prefers results as `structuredContent` or JSON text, which it turns into program
values (`v2:packages/core/src/tool/mcp.ts:96`). It answers form and URL
elicitations, attributed to the Location rather than the session
(`v2:packages/core/src/mcp/index.ts:80`). It does not ask the server for
isolation; it *tells* the server which session is calling, in
`_meta["ai.opencode/sessionID"]`, and leaves partitioning to the server
(`v2:packages/core/src/mcp/client.ts:278`). It speaks 2025-11-25 by default and
2026-07-28 when configured (`v2:packages/schema/src/mcp.ts:23`), allows 12-hour
calls (`v2:packages/core/src/mcp/client.ts:34`), and expects a server that is
itself code mode to be configured `codemode: false`
(`v2:packages/core/src/plugin/mcp-codemode-exclusion.ts:16`).

**lootbox** treats a server as a long-lived process it connects to once, at
daemon start, and shares among all callers, pinging it to decide whether it is
alive (`.lootbox/src/lib/rpc/managers/mcp_integration_manager.ts:221-222`). It
declares `capabilities: {}`, so a server that elicits, samples or asks for
roots is refused (`.lootbox/src/lib/external-mcps/mcp_client_manager.ts:150-153`).
It assumes one page of tools and one page of resources, and names that reduce
harmlessly to `[A-Za-z0-9_]`
(`.lootbox/src/lib/external-mcps/mcp_schema_fetcher.ts:69`). It assumes a
server is stateless enough to share. Its one concession to stateful servers is
rewriting a port it finds in their command line
(`.lootbox/src/lib/rpc/managers/mcp_integration_manager.ts:82-133`). Results
reach the script as raw envelopes.

**Cloudflare code mode** starts from "Almost every MCP server is just a wrapper
around an existing traditional API" (<https://blog.cloudflare.com/code-mode/>,
"But MCP is still useful, because it is uniform"). A server is a remote,
OAuth-authorised HTTP endpoint that an Agent connects to once. The Agents MCP
client offers `auto`, `streamable-http` and `sse`, and no stdio
(<https://developers.cloudflare.com/agents/model-context-protocol/apis/client-api/>,
"Transport options"). The server enforces its own authorization. It describes
its tools well enough to generate typed methods, with names unique after
sanitisation or the connector throws. A large API should itself be a code-mode
server with two tools, `search` and `execute`, at about 1,000 tokens
(<https://blog.cloudflare.com/code-mode-mcp/>, "Server-side Code Mode").
Side-effecting tools should be safe to pause, replay in order and, ideally,
revert (<https://developers.cloudflare.com/agents/tools/codemode/how-it-works/>,
"Deterministic replay", "Rollback").

**mcpx (as a server)** describes itself as a server that "runs MCP servers and
exposes them through a few tools rather than many"
(`internal/mcpserver/server.go:397`): a stable, small surface (`mcpx_call`,
`mcpx_exec`, discovery tools) whose real work happens next to pooled upstream
processes. Its rules for itself are in [`../protocol.md`](../protocol.md):
speak every revision, accept liberally, send conservatively, carry questions
across the era boundary.

## What mcpx has to be to satisfy all of them

Reading the accounts side by side gives mcpx's job:

- **To 2026-07-28 and to opencode v2, a function of the request.** Version,
  capabilities, and now *identity*: v2 hands over the session in `_meta`, and
  2026 forbids inferring it from the connection. Anything spanning requests is
  a handle.
- **To the legacy revisions and to opencode v1, a patient session.** It holds
  the `initialize` agreement, sends questions on the wire, and never replies to
  a notification.
- **To lootbox-style and Cloudflare-style callers, a catalogue.** Typed names
  that sanitise safely and do not collide, with results a program can use
  (structured when possible).
- **To every upstream, the host they cannot see.** It answers `ping`, roots,
  elicitation and sampling on the host's behalf, or carries them to the host
  when the host can answer.

The places where mcpx does not yet meet one of these are the register's
`mcpx missing` rows ([README](README.md#the-register)).
