# Roots

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  roots/list, the roots capability and its list_changed notification across revisions, and what mcpx, opencode and lootbox answer
```

Roots let a server ask the client which filesystem locations it should work in. The request is in every revision; 2026-07-28
drops the change notification and the `listChanged` flag, carries `roots/list` only as an embedded input request, and
deprecates the feature in favour of tool parameters or configuration. For mcpx the point is that it declares `roots` to
every upstream and always answers an empty list, while its doc says "the configured roots" (ROOT-01); and that as a server
it never asks its own host for roots, so the host's workspace never reaches an upstream (ROOT-04). opencode declares roots
in both versions and answers with one root, its directory.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| ROOT-01 | `roots/list` and what a client answers | `2024-11-05 has` `2026-07-28 deprecates` `opencode v1 has` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 dep. (MRTR)`; opencode one root; lootbox ✗ | partial — declared to every upstream; always `[]` | + med | S | med |
| ROOT-02 | `notifications/roots/list_changed` removed | `2024-11-05 has` `2026-07-28 removes` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | ✗ — answers the notification with a `-32601` error | + low | S | low |
| ROOT-03 | Roots deprecated; `ListRootsRequest` embedded in `InputRequiredResult` | `2026-07-28 deprecates` `has better replacement` | `26-07 only` | ✓ — embedded `roots/list` answered like legacy | − deprecated | S | low |
| ROOT-04 | Server asks its host for roots | `mcpx missing` `2026-07-28 deprecates` | spec: any server may; mcpx never does | ✗ — never sends `roots/list` to a host (#210) | + med | M | low |
| ROOT-05 | Per-request `roots` `_meta` key | `specs conflict` | SEP-2575 lists it; 2026-07-28 defines no such key | n/a — neither sent nor read | − moot | S | low |

## ROOT-01 `roots/list` and what a client answers

- **What.** A server asks the client for `Root{uri, name?}` entries; `uri` must be a `file://` URI. A client that supports
  it declares the `roots` capability. `_meta` on `Root` arrives in 2025-06-18.
- **Where.** All five revisions; in 2026-07-28 only as an MRTR input request, and deprecated. opencode v1 and v2 both
  declare `roots` and answer one root, the opencode directory (v1 the instance directory, v2 the Location directory).
  lootbox declares no capabilities, so its SDK would answer `-32601`.
- **mcpx @ 05c78b2.** The client declares `roots` to every upstream and answers from configured roots
  (`internal/mcpclient/modern.go:116-120`), but the daemon installs nil roots (`internal/daemon/server.go:176`) and no
  setting feeds them, so the answer is always `{"roots": []}`. `docs/protocol.md:278` says "the configured roots".
- **Value to mcpx.** + med: filesystem servers could scope themselves to the caller's cwd or repository, which the pool
  already knows as the scope key. The alternative is to stop declaring.
- **Effort.** S either way: derive roots from the call's cwd or repo, or drop the capability.
- **Risk.** med: servers that refuse to work without a root fail with a confusing "no roots" message. opencode v2's own
  comment cites such servers ("Some legacy servers refuse to run without one").
- **Detail.** The natural roots vary per scope key (cwd, repo, worktree), so a pooled instance shared across keys has no
  single right answer; per-key instances do. opencode's root is the opencode directory, not the git worktree. 2026 prose
  spells out that roots are "informational guidance rather than an access-control mechanism".
- **Sources.** `schema/2024-11-05/schema.ts:1029` "roots/list"; `schema/2025-06-18/schema.ts:1439`
  "_meta?: { [key: string]: unknown };"; `schema/2026-07-28/schema.ts:2760` "This *must* start with";
  `internal/mcpclient/modern.go:116-120`; `internal/daemon/server.go:176` "reg.InstallHooks(srv.Events, broker, nil)";
  `docs/protocol.md:278` "the configured roots, before consulting any handler";
  `v1:packages/opencode/src/mcp/index.ts:78` "Promise.resolve({ roots: [{ uri: pathToFileURL(directory).href }] }),";
  `v2:packages/core/src/mcp/client.ts:155`; `v2:packages/core/src/mcp/client.ts:145` "Some legacy servers refuse to run without one";
  `.lootbox/src/lib/external-mcps/mcp_client_manager.ts:150-153`; `2026-07-28/client/roots.mdx:22` "are informational guidance rather than an access-control mechanism.".

## ROOT-02 `notifications/roots/list_changed` removed

- **What.** The client-to-server "my roots changed" notification. Legacy clients that declared `listChanged: true` MUST send
  it on change. 2026 removes it; the only client notification left is `notifications/cancelled`.
- **Where.** 2024-11-05..2025-11-25.
- **mcpx @ 05c78b2.** As a client it declares `listChanged: false`, so it never has to send one. As a server it has no
  handler, and the default branch answers with a `-32601` error (`internal/mcpserver/server.go:766`). Wire S5:
  `{"jsonrpc":"2.0","error":{"code":-32601,"message":"no method notifications/roots/list_changed"}}`, a response to a
  notification, which MUST NOT get one.
- **Value to mcpx.** + low: stop replying; better still, refresh roots from the host (ROOT-04).
- **Effort.** S — ignore notifications (the general fix is in [notifications.md](notifications.md)).
- **Risk.** low: strict hosts log a protocol error on every roots change.
- **Detail.** opencode declares `roots` without `listChanged`, so it never sends this notification to mcpx.
- **Sources.** `schema/2025-11-25/schema.ts:2146` "notifications/roots/list_changed";
  `2025-11-25/client/roots.mdx:78` "When roots change, clients that support";
  `schema/2026-07-28/schema.ts:3166` "export type ClientNotification = CancelledNotification;";
  `internal/mcpserver/server.go:766`; wire S5.

## ROOT-03 Roots deprecated; `ListRootsRequest` embedded in `InputRequiredResult`

- **What.** 2026 deprecates Roots (SEP-2577): migrate to tool parameters, resource URIs or server configuration.
  `ListRootsRequest` becomes a bare `{method, params?: {_meta?}}` inside `inputRequests`, and `ListRootsResult` no longer
  extends `Result`.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The client answers an embedded `roots/list` with the same `answer()` as a legacy request
  (`internal/mcpclient/modern.go:116-120`), so a 2026 upstream gets the same empty list (ROOT-01).
- **Value to mcpx.** − deprecated.
- **Effort.** S — done.
- **Risk.** low.
- **Detail.** The embedded request's `_meta` is a plain `MetaObject`, not `RequestMetaObject`, so no protocol version or
  capabilities are required inside it. opencode v2's code says roots are "Legacy era only: ... modern servers cannot
  request them", yet the same client registers a `roots/list` handler and its SDK auto-fulfils MRTR input requests from
  registered handlers ([mrtr.md](mrtr.md), MRTR-18); which of the two holds on a modern v2 connection was not verified.
- **Sources.** `2026-07-28/client/roots.mdx:8` "The Roots feature is deprecated as of protocol version";
  `schema/2026-07-28/schema.ts:2718` "export interface ListRootsRequest {";
  `schema/2026-07-28/schema.ts:2742` "export interface ListRootsResult {"; `internal/mcpclient/modern.go:116-120`;
  `v2:packages/core/src/mcp/client.ts:144`; `v2:packages/core/src/mcp/client.ts:155`.

## ROOT-04 Server asks its host for roots

- **What.** Any server may ask its client for roots (a `roots/list` request in legacy, a `ListRootsRequest` input request in
  2026). A proxy can use that to learn the host's workspace and pass it on to upstreams.
- **Where.** Every revision (deprecated in 2026).
- **mcpx @ 05c78b2.** Never asks: there is no `roots/list` sender in `internal/mcpserver`. The host's workspace never
  becomes an upstream root, which is half of why mcpx's upstream answer is empty (ROOT-01).
- **Value to mcpx.** + med: a roots bridge would give upstreams the directory the host is actually working in. opencode
  would answer with its directory in both versions.
- **Effort.** M — ask per host, map to the pool's scope keys, answer upstream `roots/list` from that.
- **Risk.** low: without it, roots-aware upstreams see nothing.
- **Detail.** mcpx already scopes instances by cwd, repo or session (the pool scope key), which covers most of what roots
  would say; a bridge matters for servers that insist on roots rather than their own cwd.
- **Sources.** `internal/mcpserver/server.go:766`; `internal/mcpclient/modern.go:116-120`;
  `v1:packages/opencode/src/mcp/index.ts:78`.

## ROOT-05 Per-request `roots` `_meta` key

- **What.** SEP-2575 (stateless MCP) lists `roots` among the fields carried per request in `_meta`, next to
  `protocolVersion`, `clientInfo`, `logLevel` and `clientCapabilities`. The 2026-07-28 specification defines no
  `io.modelcontextprotocol/roots` key; roots arrive only as MRTR `roots/list` input requests.
- **Where.** SEP text only.
- **mcpx @ 05c78b2.** Neither sends nor reads such a key.
- **Value to mcpx.** − moot: follow the specification, not the SEP.
- **Effort.** S.
- **Risk.** low: a server built from the SEP text might look for a key no client sends.
- **Detail.** The reserved `_meta` key table in 2026 has `protocolVersion`, `clientInfo`, `clientCapabilities`, `logLevel`
  and `subscriptionId`, no `roots`.
- **Sources.** `seps/2575-stateless-mcp.md:760` "(`protocolVersion`, `clientInfo`, `roots`, `logLevel`, `clientCapabilities`)";
  `2026-07-28/basic/index.mdx:356`.
