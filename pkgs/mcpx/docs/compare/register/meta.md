# `_meta`

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  The _meta extension point across revisions: naming and reservation rules, reserved keys, and which keys mcpx reads, writes, forwards or drops.
```

`_meta` is the reserved metadata object on requests, notifications and results. It has existed since 2024-11-05,
acquired naming rules in 2025-06-18, had its reserved-prefix rule inverted in 2025-11-25, and in 2026-07-28 became the
carrier for protocol state itself: version, capabilities, identity, log level and subscription ids. For mcpx, which
terminates one MCP conversation and starts another, the question is which keys belong to a single hop and which must
travel end to end. It replaces the per-hop keys correctly, but drops everything else in both directions, including
trace context and opencode v2's per-session isolation hint. The per-request version, capability, client and server
identity keys are rows in [lifecycle-versioning.md](lifecycle-versioning.md) (LV-19..LV-22).

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| META-01 | `_meta` on requests, notifications and results, without rules | `2024-11-05 has` | `24-11 ✓(schema) · 25-03 ✓(schema) · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — client keeps caller keys when adding its own | + low | S | low |
| META-02 | Key-name format and forward-DNS prefix reservation | `2025-06-18 has` | `25-06 ✓` | n/a — mcpx defines no `_meta` keys | + low | S | low |
| META-03 | Reservation flips to reverse-DNS "second label" | `2025-11-25 has` `specs conflict` | `25-11 ✓ · 26-07 ✓` | n/a — no mcpx keys yet | + low | S | low |
| META-04 | Typed `_meta` objects per message kind | `2026-07-28 has` | `26-07 ✓` | partial — request keys read; notification and result keys never set | + med | S | low |
| META-05 | `RequestParams._meta` required, so `params` is required | `2026-07-28 has` | `26-07 ✓` | ✓ — client creates `params`; server treats absence as legacy | + low | S | low |
| META-06 | 2026 reserved-key table | `2026-07-28 has` `specs conflict` | `26-07 ✓` | partial — three request keys handled; the rest ignored | + med | S | med |
| META-07 | `traceparent` / `tracestate` / `baggage` | `2026-07-28 has` `mcpx missing` | `26-07 ✓` (prose only) | ✓ — passed upstream from a host's `tools/call` (#212; `docs/protocol.md` §4.4) | + med | S | low |
| META-08 | `clientInfo` / `serverInfo` are self-reported, display-only | `2026-07-28 has` | `26-07 ✓` (legacy silent) | ✓ — neither drives behaviour | + low | S | low |
| META-09 | `ai.opencode/sessionID` on every opencode v2 `tools/call` | `opencode v2 has` `mcpx missing` `has better replacement` | opencode v2 ✓ · opencode v1 — | ✗ — ignored; every host shares one scope (#211) | + high | M | high |
| META-10 | Per-hop versus end-to-end keys through a proxy | `2026-07-28 has` | `26-07` (implied) | partial — per-hop keys replaced; nothing end-to-end survives | + med | M | med |
| META-11 | Upstream result `_meta` reaching the host | `2024-11-05 has` `mcpx missing` | `24-11 ✓(schema) · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | partial — lost via `mcpx_call`; kept on `/v1` and in scripts (#206) | + med | M | low |

## META-01 `_meta` on requests, notifications and results, without rules

- **What.** 2024-11-05 already reserves `params._meta` on requests (typed with the single key `progressToken`),
  `params._meta` on notifications and `result._meta` on results (both open maps). No key-format or reservation rules
  exist until 2025-06-18.
- **Where.** Schema-only in 2024-11-05 and 2025-03-26; prose rules from 2025-06-18.
- **mcpx @ 05c78b2.** When the client adds its own 2026 keys it keeps any the caller already set
  (`internal/mcpclient/modern.go:73-81`).
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** Progress opt-in has always lived in `_meta`, never as a top-level parameter (PC-04).
- **Sources.** `schema/2024-11-05/schema.ts:21` "_meta?: {"; `schema/2024-11-05/schema.ts:25` "progressToken?:
  ProgressToken;"; `schema/2024-11-05/schema.ts:37`; `schema/2024-11-05/schema.ts:46` "_meta?: { [key: string]: unknown
  };"; `internal/mcpclient/modern.go:73-81`.

## META-02 Key-name format and forward-DNS prefix reservation

- **What.** 2025-06-18 defines a key as an optional prefix (dot-separated labels, then `/`) plus a name, and reserves
  any prefix of the form "zero or more labels, then `modelcontextprotocol` or `mcp`, then any label", for example
  `modelcontextprotocol.io/`, `mcp.dev/`, `api.modelcontextprotocol.org/`, `tools.mcp.com/`. Implementations MUST NOT
  make assumptions about values at reserved keys. The same revision adds `_meta` to more types (PR #710).
- **Where.** 2025-06-18; replaced one revision later (META-03).
- **mcpx @ 05c78b2.** Defines no `_meta` keys of its own (`internal/mcpclient/modern.go:12-16` lists only the three
  `io.modelcontextprotocol/` request keys).
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Names, unless empty, begin and end with an alphanumeric and may contain hyphens, underscores and dots.
  The `_meta` fields added to `Tool`, `Resource`, `ResourceTemplate`, `Prompt`, `Root` and content blocks are covered
  in the tools, resources, prompts, roots and content-types registers.
- **Sources.** `2025-06-18/basic/index.mdx:140` "- Any prefix beginning with zero or more valid labels, followed by
  `modelcontextprotocol` or `mcp`, followed by any valid label,"; `2025-06-18/basic/index.mdx:142`;
  `2025-06-18/changelog.mdx:35` "1. Add `_meta` field to additional interface types".

## META-03 Reservation flips to reverse-DNS "second label"

- **What.** 2025-11-25 says implementations SHOULD use reverse-DNS prefixes and reserves a prefix exactly when its
  **second** label is `modelcontextprotocol` or `mcp` (`io.modelcontextprotocol/`, `dev.mcp/`,
  `org.modelcontextprotocol.api/`, `com.mcp.tools/`); `com.example.mcp/` is not reserved. So `tools.mcp.com/` was
  reserved under 2025-06-18 and is not under 2025-11-25, while `com.mcp.tools/` now is.
- **Where.** 2025-11-25 and 2026-07-28 (same text). The 2025-11-25 changelog does not mention the change.
- **mcpx @ 05c78b2.** No mcpx keys yet (`internal/mcpclient/modern.go:12-16`).
- **Value to mcpx.** + low: any key mcpx adds should use a reverse-DNS prefix whose second label is not `mcp` or
  `modelcontextprotocol`; `dev.mcpx/` qualifies under both rules.
- **Effort.** S.
- **Risk.** None.
- **Detail.** opencode's `ai.opencode/` is reverse DNS for opencode.ai and unreserved under both rules.
- **Sources.** `2025-11-25/basic/index.mdx:207` "- Implementations SHOULD use reverse DNS notation";
  `2025-11-25/basic/index.mdx:208` "- Any prefix where the second label is `modelcontextprotocol` or `mcp` is
  **reserved** for MCP use."; `2026-07-28/basic/index.mdx:337`; `2026-07-28/basic/index.mdx:339`.

## META-04 Typed `_meta` objects per message kind

- **What.** The 2026 schema types `_meta` per kind: `MetaObject` (the naming rules), `RequestMetaObject`
  (`progressToken`, required `protocolVersion`, `clientInfo`, required `clientCapabilities`, `logLevel`),
  `NotificationMetaObject` (`subscriptionId`), `ResultMetaObject` (`serverInfo`), and
  `SubscriptionsListenResultMetaObject`, where `subscriptionId` is required. It fixes which key is legal where:
  version and capabilities only on requests, `subscriptionId` on notifications and the listen result, `serverInfo` on
  results. The changelog does not mention these types.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Reads the two required request keys (`internal/mcpserver/conn.go:171-175`); sets no notification
  key (SUB-05) and no result key (`lifecycle-versioning.md` LV-22).
- **Value to mcpx.** + med: the typed objects are the checklist for what a 2026 peer expects on each message.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** 2026's embedded input requests are not `JSONRPCRequest`s: `ListRootsRequest.params._meta` is a plain
  `MetaObject` without version or capabilities, and `CreateMessageRequest` has no `_meta`; see the roots and sampling
  registers.
- **Sources.** `schema/2026-07-28/schema.ts:54` "export type MetaObject = Record<string, unknown>;";
  `schema/2026-07-28/schema.ts:63`; `schema/2026-07-28/schema.ts:120`; `schema/2026-07-28/schema.ts:143`;
  `schema/2026-07-28/schema.ts:1326`; `internal/mcpserver/conn.go:171-175`.

## META-05 `RequestParams._meta` required, so `params` is required

- **What.** 2026 declares `RequestParams { _meta: RequestMetaObject }` with `_meta` non-optional, and concrete requests
  type `params` as required (`PaginatedRequest.params`, `DiscoverRequest.params`). In legacy revisions `params` was
  optional for list requests and `ping`.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The client's `withMeta` creates `params` when there is none (`internal/mcpclient/modern.go:66-91`).
  The server cannot see a version in a request without `params`, so it serves it as legacy
  (`lifecycle-versioning.md` LV-24).
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The base `Request.params` stays optional in the schema, so a request without `params` is valid as a
  `JSONRPCRequest` yet malformed by the prose.
- **Sources.** `schema/2026-07-28/schema.ts:180` "_meta: RequestMetaObject;"; `schema/2026-07-28/schema.ts:1064`
  "params: PaginatedRequestParams;"; `schema/2025-11-25/schema.ts:637` "params?: PaginatedRequestParams;";
  `schema/2026-07-28/schema.ts:188`; `internal/mcpclient/modern.go:66-91`.

## META-06 2026 reserved-key table

- **What.** 2026 lists the reserved keys: `progressToken`; `io.modelcontextprotocol/protocolVersion`, `/clientInfo`,
  `/clientCapabilities`, `/logLevel`, `/subscriptionId`; and `traceparent`, `tracestate`, `baggage`. Official
  extensions define more under `io.modelcontextprotocol/`, third-party extensions use their own vendor prefix, and
  implementations MUST NOT make assumptions about values at these keys.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Knows `protocolVersion`, `clientCapabilities` and `clientInfo`
  (`internal/mcpserver/conn.go:171-175`; `internal/mcpclient/modern.go:12-16`); nothing handles `logLevel`,
  `subscriptionId`, `serverInfo` or the trace keys.
- **Value to mcpx.** + med: each unhandled key is a row below or in `lifecycle-versioning.md`.
- **Effort.** S per key.
- **Risk.** As in the per-key rows.
- **Detail.** The table omits `io.modelcontextprotocol/serverInfo`, which the schema defines on `ResultMetaObject` and
  the same page describes in a separate result-fields table. SEP-2575 lists `roots` among the per-request fields, but
  the released revision defines no roots key; roots reach a server only through MRTR `roots/list` input requests.
  Per-key rows: `lifecycle-versioning.md` LV-19..LV-22 and PC-04..TASK-15 here.
- **Sources.** `2026-07-28/basic/index.mdx:348` "The following `_meta` keys are reserved by this specification:";
  `seps/2575-stateless-mcp.md:760`;
  `2026-07-28/basic/index.mdx:328`; `2026-07-28/basic/index.mdx:358`; `2026-07-28/basic/index.mdx:402`;
  `schema/2026-07-28/schema.ts:157`; `internal/mcpserver/conn.go:171-175`.

## META-07 `traceparent` / `tracestate` / `baggage`

- **What.** Unprefixed keys reserved for W3C Trace Context and W3C Baggage (SEP-414); when present, values MUST follow
  those formats. The spec says the exception exists for compatibility with existing implementations and the
  OpenTelemetry semantic conventions for MCP.
- **Where.** 2026-07-28 (SDKs used them before).
- **mcpx @ 05c78b2.** No `traceparent` in `internal/`; the host's `_meta` is dropped (`internal/mcpserver/server.go:658-665`)
  and never reaches the upstream request.
- **Value to mcpx.** + med: a proxy is where a trace continues from agent to mcpx to upstream.
- **Effort.** S to pass through; M to create spans.
- **Risk.** Traces break at mcpx.
- **Detail.** The keys are prose-only: the 2026 schema has no `traceparent` (its `MetaObject` is
  `Record<string, unknown>`). The prose calls them "an exception to the prefix requirement above", but the key-format
  rules above make the prefix optional and the same table lists an unprefixed `progressToken`; the requirement is only
  implied by "third-party extensions use their own vendor prefix".
- **Sources.** `2026-07-28/basic/index.mdx:421` "As an exception to the prefix requirement above, the keys
  `traceparent`, `tracestate`, and"; `2026-07-28/basic/index.mdx:358`; `2026-07-28/basic/index.mdx:330`;
  `2026-07-28/basic/index.mdx:362`; `internal/mcpserver/server.go:658-665`.

## META-08 `clientInfo` / `serverInfo` are self-reported, display-only

- **What.** Both are unverified. Implementations SHOULD NOT change behaviour or make security decisions based on them;
  display, logging and debugging are their purpose.
- **Where.** 2026-07-28 says so explicitly; legacy revisions are silent.
- **mcpx @ 05c78b2.** The server never reads `clientInfo` (the key is only declared,
  `internal/mcpserver/conn.go:174`), and pool scope does not key on it.
- **Value to mcpx.** + low: logging it for attribution is fine; special-casing agents by name is not.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Aggregating clients should not rely on `serverInfo.name` being unique; see the tools register on
  namespacing.
- **Sources.** `2026-07-28/basic/index.mdx:407` "intended for display, logging, and debugging. Implementations
  **SHOULD NOT**"; `internal/mcpserver/conn.go:174`.

## META-09 `ai.opencode/sessionID` on every opencode v2 `tools/call`

- **What.** opencode v2's MCP client attaches the calling opencode session id as the vendor key
  `_meta["ai.opencode/sessionID"]` on every tool call, direct and code-mode child calls alike. It is advisory: a server
  that chooses to can partition its state by it. opencode v1 sends nothing but `name` and `arguments`.
- **Where.** opencode v2 only, in exactly one place.
- **mcpx @ 05c78b2.** Ignored. `tools/call` reads `{name, arguments}` (`internal/mcpserver/server.go:658-665`), no
  `ai.opencode` string exists in `internal/`, and `Backend.Call(ctx, namespace, tool, args)` has no caller identity
  (`internal/mcpserver/server.go:45`). Pool scope for `/mcp` comes from a process-wide session key that every HTTP host
  shares (process-model register, finding M-3).
- **Value to mcpx.** + high: using it as the scope key gives opencode v2 real per-session isolation of stateful
  servers through `/mcp`, with no plugin and no shell environment; `OPENCODE-V2.md:92-94` notes that nothing else
  provides this.
- **Effort.** M — reading the key is small; threading a caller identity from `tools/call` into pool leasing is the work.
- **Risk.** Not done: two opencode sessions on one daemon share one browser.
- **Detail.** Other clients will not send it. v2 tells the server which session is calling and leaves partitioning to
  the server.
- **Also recorded from the code mode register.** opencode v2 only; used in exactly one place. Script to daemon:
  `x-mcpx-session` headers (`internal/codegen/emit.go:528-533`). As a server: no `ai.opencode` string in `internal/`,
  and `Backend.Call(ctx, namespace, tool, args)` has no caller identity (`internal/mcpserver/server.go:45`). As a
  client: rg finds no `_meta` in `internal/pool`. Child calls carry it because they run the same MCP executor with
  `sessionID: context.sessionID` (`v2:packages/core/src/tool/mcp.ts:69`). The key is prefixed as `_meta` requires. The
  mcpx side has three gaps held in other registers: the plugin's `mcpx_exec` sends no session and no `cwd`, so every
  call gets a fresh `exec-<runID>` session and the daemon's directory (plugin-API register, PLG-39); session-scoped
  instances started through `/v1/exec` are never released at run end (process-model register, PM-12); and every HTTP
  MCP host shares one pool session key (PM-10). What mcpx gets right is that a session lease is pinned for the whole
  script run, so navigate, snapshot and click reach the same browser (PM-11).
- **Sources.** `v2:packages/core/src/mcp/client.ts:278`; `v2:packages/core/src/tool/mcp.ts:69` "sessionID:
  context.sessionID,"; `v1:packages/opencode/src/mcp/catalog.ts:56`; `OPENCODE-V2.md:89`;
  `internal/mcpserver/server.go:45`.

## META-10 Per-hop versus end-to-end keys through a proxy

- **What.** A proxy that terminates one MCP conversation and starts another must not forward per-hop keys
  (`protocolVersion`, `clientCapabilities`, `clientInfo`) verbatim, because forwarding a host's capabilities upstream
  promises capabilities the proxy must then honour. End-to-end keys (trace context, vendor hints, a progress
  correlation) should cross. The spec implies this through "MUST NOT make assumptions about values at these keys"; it
  does not spell it out for proxies.
- **Where.** 2026-07-28 (implied).
- **mcpx @ 05c78b2.** The host's `_meta` is deleted before dispatch (`internal/mcpserver/ask.go:95-108`) and the
  upstream request gets mcpx's own keys (`internal/mcpclient/modern.go:60-91`), so per-hop keys are right. Nothing
  end-to-end survives (PC-04, META-07, META-09). `withMeta` keeps any key the caller already set
  (`internal/mcpclient/modern.go:73-81`), so passing a host's `_meta` through unfiltered would let the host's
  `clientCapabilities` override mcpx's.
- **Value to mcpx.** + med: one allowlist decides what crosses.
- **Effort.** M.
- **Risk.** Forwarding everything breaks send-conservatively; forwarding nothing breaks tracing and isolation.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/index.mdx:328` "implementations **MUST NOT** make assumptions about values at these
  keys."; `internal/mcpserver/ask.go:100`; `internal/mcpclient/modern.go:73-81`.

## META-11 Upstream result `_meta` reaching the host

- **What.** Results carry `_meta` (an open map since 2024-11-05, formalised in 2025-06-18); extensions such as MCP Apps
  put UI resource pointers there.
- **Where.** Every revision.
- **mcpx @ 05c78b2.** `mcpx_call` renders the upstream result as text, decoding only `content`, `structuredContent` and
  `isError` (`internal/cli/commands.go:385-415`), so result `_meta` is lost. `/v1/call` passes the raw result
  (`internal/daemon/server.go:703`), and the script client exposes it as `.raw` (`internal/codegen/emit.go:476-491`).
- **Value to mcpx.** + med: extensions that ride in `_meta` work through mcpx.
- **Effort.** M.
- **Risk.** Low today.
- **Detail.** None.
- **Sources.** `schema/2024-11-05/schema.ts:46`; `internal/cli/commands.go:386-393`; `internal/daemon/server.go:703`;
  `internal/codegen/emit.go:476-491`.
