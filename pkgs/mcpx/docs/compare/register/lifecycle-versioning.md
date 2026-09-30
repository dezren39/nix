# Lifecycle and versioning

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  How peers agree on a revision: the legacy initialize handshake, 2026 per-request versioning, server/discover, dual-era rules.
```

Revisions 2024-11-05 through 2025-11-25 open every connection with an `initialize` handshake that fixes the version
and capabilities for the connection's life; 2026-07-28 removes the handshake, puts version, capabilities and identity
in every request's `_meta`, and adds `server/discover`. mcpx has to speak both eras in both directions, and two things
matter most: its modern path agrees with itself but not with the schema (`protocolVersions` where the field is
`supportedVersions`), so it cannot complete discovery with a conformant 2026 peer in either direction; and it refuses
legacy clients that the legacy rules say it must answer with a counter-offer.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| LV-01 | `initialize` / `notifications/initialized` handshake, removed in 2026 | `2024-11-05 has` `2026-07-28 removes` `has better replacement` `opencode v1 has` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` · opencode v1 ✓ · opencode v2 ✓ (default) | ✓ — served and spoken; legacy tried first as a client | + high | S | low |
| LV-02 | `InitializeRequest.params`: `protocolVersion`, `capabilities`, `clientInfo` | `2024-11-05 has` `2025-11-25 has` `2026-07-28 removes` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ (+`_meta`) · 26-07 —` | ✓ — all three sent; missing `protocolVersion` accepted | + low | S | low |
| LV-03 | 2024-11-05 is not served; the floor is 2025-03-26 | `impl deferred` `2024-11-05 has` | `24-11 ✓` hosts exist | ✗ — deliberate floor at 2025-03-26 | + med | M | med |
| LV-04 | Server echoes a supported requested version | `2024-11-05 has` `2026-07-28 removes` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | ✓ — 2025-03-26 and 2025-06-18 agreed as asked | + high | S | low |
| LV-05 | Counter-offer for an older legacy version (2024-11-05) | `mcpx missing` `2024-11-05 has` `specs conflict` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | ✗ — refused with `-32022` instead of a counter-offer (#202) | + high | S | high |
| LV-06 | Counter-offer for an unknown newer version | `mcpx missing` `2024-11-05 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | ✗ — `9999-01-01` refused; future dates count as modern (#202) | + med | S | med |
| LV-07 | `initialize` asking for `2026-07-28` | `2026-07-28 has` `mcpx missing` | `26-07 ✓` (dual-era rule) | ✗ — refused `-32022`, legacy list only; rule says counter-offer (#202) | + low | S | low |
| LV-08 | Client checks the version the server answered | `specs conflict` `mcpx missing` `opencode v1 has` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` · opencode v1 ✓ · opencode v2 ✓ | ✗ — stores any version the server returns (#203) | + med | S | med |
| LV-09 | Version each client offers in `initialize` | `2025-11-25 has` `opencode v1 has` `opencode v2 has` `lootbox has` | mcpx 2025-11-25 · opencode v1/v2 2025-11-25 · lootbox 2025-06-18 | partial — always 2025-11-25; documented override does not exist | + low | S | low |
| LV-10 | Pre-initialization restrictions (only `ping`, logging) | `2024-11-05 has` `2026-07-28 removes` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | acc. — any method served before `initialize`, as 2025-03-26 | + low | S | low |
| LV-11 | "Respect negotiated version and capabilities": SHOULD → MUST | `2025-06-18 has` | `24-11 SHOULD · 25-03 SHOULD · 25-06 MUST · 25-11 MUST · 26-07 per request` | partial — results downgraded; some declarations leak | + high | M | med |
| LV-12 | `initialize` MUST NOT be batched | `2025-03-26 has` `2025-06-18 removes` | `25-03 ✓` only | n/a — mcpx accepts no batches at all | + low | S | low |
| LV-13 | `notifications/initialized` arriving at a 2026 server | `2026-07-28 removes` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | ✓ — swallowed silently, bare `initialized` too | + low | S | low |
| LV-14 | `instructions`: `InitializeResult` → `DiscoverResult` | `2024-11-05 has` `2026-07-28 has` `specs conflict` `opencode v1 has` `opencode v2 has` | `24-11 ✓(schema) · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (discover)` · opencode v1/v2 system prompt | ✓ — sent on both; client stores both | + high | S | low |
| LV-15 | `Implementation.title` | `2025-06-18 has` | `24-11 — · 25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✗ — not sent (optional) | + low | S | low |
| LV-16 | `Implementation.description` | `2025-11-25 has` | `25-11 ✓ · 26-07 ✓` | ✗ — not sent (optional) | + low | S | low |
| LV-17 | `Implementation.icons` | `2025-11-25 has` | `25-11 ✓ · 26-07 ✓` | ✗ — not sent, not read | + low | S | low |
| LV-18 | `Implementation.websiteUrl` | `2025-11-25 has` | `25-11 ✓ · 26-07 ✓` | ✗ — not sent (optional) | + low | S | low |
| LV-19 | Per-request `_meta` `io.modelcontextprotocol/protocolVersion` | `2026-07-28 has` `opencode v2 has` | `26-07 ✓` · opencode v2 (modern era) | ✓ — read and checked per request; client sends it | + high | S | low |
| LV-20 | Per-request `_meta` `io.modelcontextprotocol/clientCapabilities` | `2026-07-28 has` `opencode v2 has` | `26-07 ✓` · opencode v2 (modern era) | partial — read per request; a missing key means "none" | + high | S | med |
| LV-21 | Per-request `_meta` `io.modelcontextprotocol/clientInfo` | `2026-07-28 has` `opencode v2 has` | `26-07 ✓` (SHOULD) · opencode v2 (modern era) | ✓ — client sends it; server does not read it | + low | S | low |
| LV-22 | `io.modelcontextprotocol/serverInfo` in every result's `_meta` | `2026-07-28 has` `mcpx missing` | `26-07 ✓` (SHOULD) | ✗ — never emitted; client never reads it (#199) | + med | S | low |
| LV-23 | `DiscoverResult` has no top-level `serverInfo` | `2026-07-28 has` `mcpx missing` | `26-07` (field absent) | ✗ — sends undefined field; client reads it there (#199) | + low | S | low |
| LV-24 | A 2026 request missing required `_meta` is malformed | `2026-07-28 has` `mcpx missing` | `26-07 ✓` | partial — no version ⇒ served as 2025-03-26; no caps ⇒ none (#199) | + med | S | med |
| LV-25 | `server/discover` MUST be implemented | `2026-07-28 has` `opencode v2 has` | `26-07 ✓` · opencode v2 (`protocol` auto/pinned) | partial — answered in any era; field names wrong | + high | S | high |
| LV-26 | `DiscoverResult.supportedVersions` | `2026-07-28 has` `mcpx missing` | `26-07 ✓` | ✗ — `protocolVersions` on both server and client (#199) | + high | S | high |
| LV-27 | stdio probe order: `server/discover` before `initialize` | `2026-07-28 has` `impl deferred` `opencode v2 has` | `26-07` SHOULD · opencode v2 `auto` | partial — legacy first by default; modern first is opt-in | + med | S | med |
| LV-28 | Which probe errors mean "modern server" | `2026-07-28 has` `mcpx missing` | `26-07 ✓` | ✗ — only a `-32022` substring counts (#200) | + med | S | med |
| LV-29 | Modern-only server refusing `initialize`; mcpx client aborts | `2026-07-28 has` `mcpx missing` | `26-07` SHOULD name versions | ✗ — client aborts on `-32022`; never tries discover (#200) | + high | S | high |
| LV-30 | HTTP era detection from the 400 body | `2026-07-28 has` | `26-07` MAY/SHOULD | partial — status ignored; body matched by substring | + med | S | med |
| LV-31 | Era is a property of the server; cache it | `2026-07-28 has` | `26-07` SHOULD cache, MAY persist | partial — per connection; not persisted across restarts | + low | S | low |
| LV-32 | Per-server era setting | `opencode v2 has` | mcpx four values · opencode v2 three values | partial — typos silently fall back to `legacy` | + low | S | low |
| LV-33 | Dual-era server: era chosen per opening message | `2026-07-28 has` | `26-07` MAY | ✓ — per-request `_meta` first, else connection handshake | + high | S | low |
| LV-34 | `ping` received from a modern client | `2026-07-28 removes` `has better replacement` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | acc. — answered `{resultType:"complete"}` | + low | S | low |
| LV-35 | `ping` sent by a client | `2024-11-05 has` `2026-07-28 removes` `lootbox has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` · lootbox (fork) | partial — `Ping()` era-blind but unused; answers server pings | + low | S | low |
| LV-36 | `resultType` required on every 2026 result | `2026-07-28 has` | `26-07 ✓` | ✓ — stamped for modern peers, stripped for legacy | + high | S | low |
| LV-37 | Request-id uniqueness: per session → among in-flight | `2026-07-28 has` | `24-11..25-11` session · `26-07` in flight | ✓ — server-initiated ids are negative | + low | S | low |
| LV-38 | stdio server exits promptly on stdin EOF | `2026-07-28 has` | `26-07` SHOULD | ✓ — `mcpx serve` exits at EOF | + med | S | low |
| LV-39 | Client stdio shutdown: close stdin, wait, SIGTERM, SIGKILL | `2024-11-05 has` `mcpx missing` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` · opencode v2 ✓ | partial — SIGTERM immediately after closing stdin (#203) | + low | S | low |
| LV-40 | Restart a stdio server that exited unexpectedly | `2026-07-28 has` `lootbox has` | `26-07` SHOULD · lootbox (fork) · opencode v1/v2 — | ✓ — lazy respawn on next call, backoff after failures | + med | S | low |

## LV-01 `initialize` / `notifications/initialized` handshake, removed in 2026

- **What.** In the four legacy revisions every connection opens with an `initialize` request, its `InitializeResult`,
  and a `notifications/initialized` notification, and that exchange MUST be the first interaction. 2026-07-28 deletes
  both message types; per-request `_meta` (LV-19..LV-21) and the optional `server/discover` (LV-25) replace them.
- **Where.** 2024-11-05, 2025-03-26, 2025-06-18, 2025-11-25. Not in 2026-07-28: `ClientRequest` lists
  `DiscoverRequest` and no `InitializeRequest`, and `ClientNotification` is `CancelledNotification` alone. opencode v1
  always initializes; opencode v2 does unless its per-server `protocol` option says otherwise (LV-32).
- **mcpx @ 05c78b2.** The server answers `initialize` (`internal/mcpserver/server.go:505-527`) and swallows
  `notifications/initialized` (`internal/mcpserver/server.go:540-541`). The client initializes first by default
  (`internal/mcpclient/client.go:231-232`) and sends `notifications/initialized` after the result
  (`internal/mcpclient/client.go:277`).
- **Value to mcpx.** + high: every published legacy client and server uses it, and 2026 explicitly allows a server
  to keep answering it (LV-33).
- **Effort.** S — done in both directions.
- **Risk.** Removing it would cut off the whole legacy ecosystem; keeping it costs nothing.
- **Detail.** mcpx also accepts the non-standard bare method name `initialized`, an accept-liberally extra. What a
  modern-only server does with a stray `notifications/initialized` is LV-13.
- **Sources.** `2024-11-05/basic/lifecycle.mdx:40` "The initialization phase **MUST** be the first interaction";
  `2026-07-28/changelog.mdx:14` "remove the `initialize`/`notifications/initialized` handshake";
  `schema/2026-07-28/schema.ts:3153` "export type ClientRequest ="; `schema/2026-07-28/schema.ts:3166` "export type
  ClientNotification = CancelledNotification;"; `v1:patches/@modelcontextprotocol%2Fsdk@1.29.0.patch:36`;
  `v2:packages/core/src/mcp/client.ts:312`; `internal/mcpserver/server.go:540`.

## LV-02 `InitializeRequest.params`: `protocolVersion`, `capabilities`, `clientInfo`

- **What.** `protocolVersion: string`, `capabilities: ClientCapabilities` and `clientInfo: Implementation` are required
  and unchanged in every legacy revision. 2025-11-25 moves them into `InitializeRequestParams extends RequestParams`,
  which formally allows `_meta` on `initialize`.
- **Where.** 2024-11-05..2025-11-25. 2026-07-28 moves each into a per-request `_meta` key (LV-19, LV-20, LV-21).
- **mcpx @ 05c78b2.** The client sends all three (`internal/mcpclient/client.go:262-267`). The server reads
  `protocolVersion` and `capabilities` (`internal/mcpserver/server.go:506-517`) and ignores `clientInfo`. An
  `initialize` with no `protocolVersion`, or with params that do not parse, is accepted and answered with 2025-11-25
  (`internal/mcpserver/server.go:848`).
- **Value to mcpx.** + low: nothing to add.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The schema describes `protocolVersion` as the latest version the client supports and adds "The client
  MAY decide to support older versions as well." The 2026 mapping is `protocolVersion` →
  `io.modelcontextprotocol/protocolVersion` (required), `capabilities` → `io.modelcontextprotocol/clientCapabilities`
  (required), `clientInfo` → `io.modelcontextprotocol/clientInfo` (optional, SHOULD). Accepting a missing
  `protocolVersion` is accept-liberally: the field is required in every legacy schema.
- **Sources.** `schema/2024-11-05/schema.ts:151` "protocolVersion: string;"; `schema/2025-11-25/schema.ts:255` "export
  interface InitializeRequestParams extends RequestParams {"; `2026-07-28/basic/index.mdx:375`;
  `internal/mcpserver/server.go:848`.

## LV-03 2024-11-05 is not served; the floor is 2025-03-26

- **What.** mcpx serves 2025-03-26, 2025-06-18, 2025-11-25 and 2026-07-28. A 2024-11-05 host cannot connect, and a
  request that declares nothing is assumed to be 2025-03-26.
- **Where.** 2024-11-05 hosts exist (older desktop builds, early SDKs); the spec repo still carries its schema and its
  HTTP+SSE transport.
- **mcpx @ 05c78b2.** `Supported` lists four revisions (`internal/mcpserver/server.go:778`) and `Oldest` is
  2025-03-26 (`internal/mcpserver/revisions.go:14`). `docs/protocol.md:51-59` records the floor as a deliberate choice.
  As a client mcpx still accepts a 2024-11-05 answer from an upstream (LV-08).
- **Value to mcpx.** + med: reach to the oldest hosts, and "every revision" is the stated goal. − another
  `downgrade()` column to maintain.
- **Effort.** M — the version, a downgrade column (no audio, no tool annotations, no `completions` capability), and
  the HTTP+SSE transport if those hosts are to be reached remotely (`transports.md` TR-10).
- **Risk.** Not done: the "speak every revision" goal stays unmet for 2024-11-05.
- **Detail.** `docs/protocol.md:51-53` says the 2025-03-26 schema "was not among" those read; it is available now and
  confirms the doc's assumptions (audio, annotations and `completions` present; no `structuredContent`,
  `resource_link`, elicitation or tasks). Serving 2024-11-05 is a separate decision from counter-offering to a
  2024-11-05 client (LV-05), which needs no new column.
- **Sources.** `internal/mcpserver/revisions.go:14` "const Oldest = \"2025-03-26\""; `docs/protocol.md:56` "as the
  floor"; `internal/mcpserver/server.go:778`.

## LV-04 Server echoes a supported requested version

- **What.** In every legacy revision, if the server supports the version the client asked for, it MUST answer with that
  same version.
- **Where.** 2024-11-05..2025-11-25; no negotiation exists in 2026-07-28 (LV-19).
- **mcpx @ 05c78b2.** `negotiate` returns the requested version when it is a supported legacy one
  (`internal/mcpserver/server.go:855`) and the reply echoes it (`internal/mcpserver/server.go:523`), with capabilities
  shaped for that revision. Wire W2 agreed 2025-03-26; the stdio run S1 agreed 2025-06-18.
- **Value to mcpx.** + high: this is the common path for every legacy host.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The agreed version is stored on the connection and governs everything sent to that peer afterwards
  (`internal/mcpserver/conn.go:101-105`).
- **Sources.** `2024-11-05/basic/lifecycle.mdx:126` "If the server supports the requested protocol version, it
  **MUST** respond with the same"; `internal/mcpserver/server.go:855`; wire W2.

## LV-05 Counter-offer for an older legacy version (2024-11-05)

- **What.** Every legacy revision says that when the server does not support the requested version it MUST still
  answer `initialize` successfully, with another version it supports (SHOULD be its latest); the client then decides
  whether to disconnect. Negotiation is a counter-offer, not an error.
- **Where.** 2024-11-05..2025-11-25, identical text.
- **mcpx @ 05c78b2.** `negotiate()` returns `""` for any version not in `Supported`
  (`internal/mcpserver/server.go:844-859`) and the `initialize` branch answers
  `unsupportedVersion(..., legacyOnly=true)` (`internal/mcpserver/server.go:506-509`). Wire W1 (HTTP) and S1 (stdio):
  `-32022` with `data.requested: "2024-11-05"` and `supported` = the three legacy versions. The same failed handshake
  also mints an `Mcp-Session-Id` (`transports.md` TR-27). `docs/protocol.md:76` claims `initialize` accepts "any
  legacy version".
- **Value to mcpx.** + high: a 2024-11-05 host would be offered 2025-11-25 and could proceed if it tolerates it;
  today it gets a code it has never heard of.
- **Effort.** S — return `Latest` instead of `""` for a well-formed version mcpx does not implement.
- **Risk.** Not done: every 2024-11-05-only host fails the handshake. Done: such a host may disconnect (spec-intended),
  or proceed and meet 2025-era shapes it did not ask for, which is its decision to make.
- **Detail.** The same lifecycle pages show an "initialization error" example with `-32602` and
  `data: {supported, requested}` (`errors.md` ERR-10), which is why the texts conflict. The example's `requested` value
  is `"1.0.0"`, not a date, which is one way to reconcile them: counter-offer for a well-formed version, error for
  garbage.
- **Sources.** `2024-11-05/basic/lifecycle.mdx:126`; `2025-03-26/basic/lifecycle.mdx:132` "Otherwise, the server
  **MUST** respond with another protocol version it"; `2025-11-25/basic/lifecycle.mdx:170`;
  `internal/mcpserver/server.go:858` `return ""`; `docs/protocol.md:76`; wire W1, S1.

## LV-06 Counter-offer for an unknown newer version

- **What.** A legacy handshake from a client newer than mcpx (for example `9999-01-01`) gets the same counter-offer
  rule as LV-05: answer with mcpx's own latest legacy version.
- **Where.** Required by every legacy revision.
- **mcpx @ 05c78b2.** Same refusal path as LV-05. Wire W4: HTTP 200, `-32022`, `supported` = the legacy list, and a new
  `Mcp-Session-Id` header.
- **Value to mcpx.** + med: forward compatibility with the next client that still starts with `initialize`.
- **Effort.** S — the same change as LV-05, plus the `Modern()` check below.
- **Risk.** Not done: a future dual-era client that probes with `initialize` first falls over at the handshake.
- **Detail.** `Modern()` is a string comparison, `version >= "2026-07-28"` (`internal/mcpserver/server.go:793`), so
  every future date counts as "modern" and would still be refused over `initialize` after LV-05 is fixed, unless that
  check changes too.
- **Sources.** `2025-11-25/basic/lifecycle.mdx:170`; `internal/mcpserver/server.go:793` "func Modern(version string)
  bool { return version >= \"2026-07-28\" }"; wire W4.

## LV-07 `initialize` asking for `2026-07-28`

- **What.** 2026 has no `initialize`. A dual-era server treats an `initialize` request as selecting legacy semantics,
  so an `initialize` carrying `protocolVersion: "2026-07-28"` falls under the legacy counter-offer rule and should be
  answered with 2025-11-25.
- **Where.** 2026-07-28 versioning page (dual-era rules).
- **mcpx @ 05c78b2.** The `initialize` branch passes `legacyOnly=true` (`internal/mcpserver/server.go:237-242`, `:508`).
  Wire W3: `-32022`, `supported: ["2025-11-25","2025-06-18","2025-03-26"]`. `docs/protocol.md:76` records the refusal
  as intended.
- **Value to mcpx.** + low: either answer is consistent with "`initialize` means legacy". A counter-offer lets a
  confused client proceed; the refusal's data omits 2026-07-28, so it gives no hint to try `server/discover`.
- **Effort.** S — the same change as LV-05.
- **Risk.** Low.
- **Detail.** The refusal code is itself 2026-only (`errors.md` ERR-10). A dual-era client that receives it concludes
  "modern server" and retries with `server/discover`, which mcpx answers, so the exchange still ends in the modern era
  after one wasted round trip.
- **Sources.** `2026-07-28/basic/versioning.mdx:178` "- An `initialize` request selects legacy semantics, scoped to the
  stdio"; `internal/mcpserver/server.go:508`; `docs/protocol.md:76`; wire W3.

## LV-08 Client checks the version the server answered

- **What.** Legacy prose says the client SHOULD disconnect if it does not support the version in the server's
  `InitializeResult`; the schema doc comment on `InitializeResult.protocolVersion` says MUST.
- **Where.** Both texts in all four legacy revisions. opencode v1 and v2 accept only a known list (2025-11-25,
  2025-06-18, 2025-03-26, 2024-11-05, 2024-10-07) and fail the connection on anything else.
- **mcpx @ 05c78b2.** The client stores `ir.ProtocolVersion` as `Negotiated` with no check
  (`internal/mcpclient/client.go:275`). `/v1/protocol` showed the test upstream at `negotiated: 2025-06-18` (wire,
  "mcpx as client"). A 2024-11-05 answer would be accepted although mcpx implements neither that revision's shapes
  nor its HTTP+SSE transport.
- **Value to mcpx.** + med: prevents silently misparsing a server that picked a revision mcpx cannot speak, while
  accepting known lower versions stays accept-liberally.
- **Effort.** S — check against `Supported` and disconnect otherwise.
- **Risk.** Not done: silent misparse against an old or unknown revision.
- **Detail.** The schema is the source of truth for a revision, so treat it as MUST. Nothing below the handshake adapts
  to the accepted version either: the HTTP version header stays 2025-11-25 (`transports.md` TR-34).
- **Sources.** `2024-11-05/basic/lifecycle.mdx:130` "If the client does not support the version in the server's
  response, it **SHOULD**"; `schema/2024-11-05/schema.ts:162` "If the client cannot support this version, it MUST
  disconnect."; `schema/2025-11-25/schema.ts:281`; `internal/mcpclient/client.go:275`;
  https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/types.js L4;
  https://unpkg.com/@modelcontextprotocol/core@2.0.0/dist/auth-CUe6YdwF.mjs L4–L12.

## LV-09 Version each client offers in `initialize`

- **What.** Which revision a client proposes when it opens a legacy connection.
- **Where.** mcpx: 2025-11-25. opencode v1 (`@modelcontextprotocol/sdk` 1.29.0 plus a 647-line patch) and opencode v2
  (`@modelcontextprotocol/client` 2.0.0, unless `protocol` is set): 2025-11-25. lootbox (`@modelcontextprotocol/sdk`
  1.22.0): 2025-06-18 as its latest, with client capabilities `{}`.
- **mcpx @ 05c78b2.** `ProtocolVersion = "2025-11-25"` (`internal/mcpclient/client.go:22`), sent at
  `internal/mcpclient/client.go:263`; the modern list is `["2026-07-28"]` (`internal/mcpclient/client.go:26`).
  `docs/protocol.md:39-40` says 2025-03-26 and 2025-06-18 are spoken upstream "on request (`protocol: force-legacy`)",
  but `force-legacy` only removes the fallback; it never chooses a version.
- **Value to mcpx.** + low: offering the newest legacy revision is right. A per-server pin would matter only for
  servers that mishandle counter-offers.
- **Effort.** S — correct the doc, or add the pin it describes.
- **Risk.** A documented knob that does not exist is a declared-but-not-delivered bug.
- **Detail.** lootbox's `capabilities: {}` means an upstream `elicitation/create` or `roots/list` gets the SDK's
  default method-not-found. opencode v1's patch moves the handshake into `_initialize` so it can be re-run on session
  expiry (`transports.md` TR-29).
- **Sources.** `internal/mcpclient/client.go:22` "const ProtocolVersion = \"2025-11-25\""; `docs/protocol.md:39` "on
  request (`protocol: force-legacy`)"; `v1:packages/opencode/package.json:83`; `v2:packages/core/package.json:113`;
  `v2:packages/core/src/mcp/client.ts:312`; `.lootbox/deno.json:44`;
  `.lootbox/src/lib/external-mcps/mcp_client_manager.ts:150-153`;
  `@modelcontextprotocol/sdk@1.22.0/dist/esm/types.js:2` "export const LATEST_PROTOCOL_VERSION = '2025-06-18';" (the
  copy lootbox resolves from the local Deno npm cache).

## LV-10 Pre-initialization restrictions (only `ping`, logging)

- **What.** Legacy: the client SHOULD NOT send requests other than `ping` before the `initialize` response, and the
  server SHOULD NOT send requests other than `ping` and logging before `notifications/initialized`.
- **Where.** 2024-11-05..2025-11-25; meaningless in 2026-07-28.
- **mcpx @ 05c78b2.** The server processes any method on a connection that never initialized and treats it as
  `Oldest`, 2025-03-26 (`internal/mcpserver/conn.go:140-146`).
- **Value to mcpx.** + low: SHOULD NOT, so serving is permitted.
- **Effort.** S — nothing to do.
- **Risk.** A modern client that forgot `_meta` is silently served legacy shapes (LV-24).
- **Detail.** 2026's stdio page notes that legacy servers which do not validate ordering answer an era-ambiguous
  method under legacy semantics, which is why the modern probe treats any non-modern answer as "legacy" (LV-27,
  LV-28). The dual-era case of a request with neither `_meta` nor a prior handshake is covered in the process-model
  register.
- **Sources.** `2024-11-05/basic/lifecycle.mdx:113` "The client **SHOULD NOT** send requests other than";
  `2024-11-05/basic/lifecycle.mdx:116`; `2026-07-28/basic/transports/stdio.mdx:144` "some legacy servers do not
  validate that a"; `internal/mcpserver/conn.go:144` "v = Oldest".

## LV-11 "Respect negotiated version and capabilities": SHOULD → MUST

- **What.** 2024-11-05 and 2025-03-26 say both parties SHOULD respect the negotiated version and use only negotiated
  capabilities; 2025-06-18 makes it MUST. 2026-07-28 restates it per request: a server MUST NOT rely on capabilities
  the client has not declared on that request.
- **Where.** MUST from 2025-06-18.
- **mcpx @ 05c78b2.** `Defines()` and `downgrade()` gate result shapes per revision
  (`internal/mcpserver/revisions.go:115-123`, `:132-170`). Declarations are not covered by that machinery; the
  capability leaks to 2025-11-25 and 2026 peers are in the capabilities register.
- **Value to mcpx.** + high: this is mcpx's "send conservatively" rule.
- **Effort.** M — the remaining leaks are spread across capabilities, lists and requests.
- **Risk.** Strict peers reject or misread fields they never negotiated.
- **Detail.** The 2025-06-18 changelog records the change as item 9.
- **Sources.** `2024-11-05/basic/lifecycle.mdx:162` "Both parties **SHOULD**:"; `2025-06-18/basic/lifecycle.mdx:175`
  "Both parties **MUST**:"; `2025-06-18/changelog.mdx:31`; `internal/mcpserver/revisions.go:132-170`.

## LV-12 `initialize` MUST NOT be batched

- **What.** With JSON-RPC batching introduced, 2025-03-26 forbids putting `initialize` in a batch; the sentence goes
  away with batching in 2025-06-18.
- **Where.** 2025-03-26 only.
- **mcpx @ 05c78b2.** mcpx does not accept batches at all (`transports.md` TR-09).
- **Value to mcpx.** + low: only relevant if 2025-03-26 batching is implemented.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The stated reason was compatibility with 2024-11-05 peers that cannot read batches.
- **Sources.** `2025-03-26/basic/lifecycle.mdx:72` "The initialize request **MUST NOT** be part of a JSON-RPC".

## LV-13 `notifications/initialized` arriving at a 2026 server

- **What.** 2026 defines no such notification, so a modern-only receiver sees an unknown notification. Notifications
  never get a response; on HTTP the server returns 202 if it accepts the POST or a 4xx status.
- **Where.** Required after `initialize` in 2024-11-05..2025-11-25; absent in 2026-07-28.
- **mcpx @ 05c78b2.** Accepted silently, as are bare `initialized` notifications
  (`internal/mcpserver/server.go:540-541`). The mcpx client sends it only after a legacy `initialize` succeeded, so it
  never sends it to a modern-only server.
- **Value to mcpx.** + low: correct for a dual-era server.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** 2026 HTTP says header requirements for notification POSTs are "not defined by this revision", so a
  modern-only HTTP server might 400 it for a missing `Mcp-Method`; a legacy client would already have failed at
  `initialize`.
- **Sources.** `schema/2026-07-28/schema.ts:3166`; `2026-07-28/basic/index.mdx:160` "The receiver **MUST NOT** send a
  response."; `2026-07-28/basic/transports/streamable-http.mdx:102`; `internal/mcpserver/server.go:540`.

## LV-14 `instructions`: `InitializeResult` → `DiscoverResult`

- **What.** Optional natural-language guidance from the server about how to use it. Legacy servers put it in
  `InitializeResult.instructions`; 2026 servers in `DiscoverResult.instructions`.
- **Where.** Present in every legacy schema including 2024-11-05, although the lifecycle prose first shows it in
  2025-03-26. 2026-07-28 moves it to `DiscoverResult`. opencode v1 and v2 render server instructions inside
  `<mcp_instructions>` in the system prompt; v2 also reads them from `server/discover` and adds a code-mode hint.
- **mcpx @ 05c78b2.** Sent in both handshakes (`internal/mcpserver/server.go:526`, `:537`); the client stores either
  (`internal/mcpclient/client.go:274`, `:322`).
- **Value to mcpx.** + high: mcpx's whole pitch (use `mcpx_exec`, not a hundred tools) reaches the model this way, and
  opencode puts it in front of the model on every turn.
- **Effort.** S — done.
- **Risk.** Long instructions cost tokens on every opencode turn. A 2026 client that never calls `server/discover`
  (it is optional for clients) never sees them.
- **Detail.** 2026 tightens the wording: instructions "should not duplicate information already in tool
  descriptions". opencode v1 drops servers whose tools are all permission-disabled; v2 delivers instruction changes as
  diffs. The schema-vs-prose gap for 2024-11-05 matters only to someone implementing from prose.
- **Sources.** `schema/2024-11-05/schema.ts:172` "instructions?: string;"; `2025-03-26/basic/lifecycle.mdx:103`;
  `schema/2026-07-28/schema.ts:696`; `internal/mcpserver/server.go:526`; `internal/mcpclient/client.go:322`;
  `v1:packages/opencode/src/session/system.ts:129`; `v2:packages/core/src/mcp/instructions.ts:23`;
  `v2:packages/core/src/mcp/client.ts:249`.

## LV-15 `Implementation.title`

- **What.** `Implementation` (the type of `clientInfo` and `serverInfo`) gains an optional display `title` beside the
  programmatic `name`, through `BaseMetadata`.
- **Where.** `{name, version}` only in 2024-11-05 and 2025-03-26; `{name, title?, version}` from 2025-06-18.
- **mcpx @ 05c78b2.** Sends `{name, version}` as server and client (`internal/mcpserver/server.go:525`,
  `internal/mcpclient/modern.go:84`).
- **Value to mcpx.** + low: a UI nicety.
- **Effort.** S.
- **Risk.** None; optional.
- **Detail.** Send conservatively: no `title` to a 2024-11-05 or 2025-03-26 peer (harmless in practice, since the
  object is open).
- **Sources.** `schema/2025-06-18/schema.ts:331` "export interface Implementation extends BaseMetadata {";
  `schema/2025-03-26/schema.ts:277` "export interface Implementation {"; `2025-06-18/basic/lifecycle.mdx:69`.

## LV-16 `Implementation.description`

- **What.** Optional free-text `description` on `Implementation`.
- **Where.** 2025-11-25 and 2026-07-28.
- **mcpx @ 05c78b2.** Not sent (`internal/mcpserver/server.go:525`); the client does not surface an upstream's.
- **Value to mcpx.** + low: an upstream's description would improve mcpx's catalog output.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Of the three `Implementation` fields added in 2025-11-25, this is the only one the changelog names.
- **Sources.** `2025-11-25/changelog.mdx:25` "Add optional `description` field to `Implementation`";
  `schema/2025-11-25/schema.ts:550`.

## LV-17 `Implementation.icons`

- **What.** `Implementation` extends the `Icons` mixin, so a client or server can publish icons for itself.
- **Where.** 2025-11-25 and 2026-07-28.
- **mcpx @ 05c78b2.** Not sent; not read from upstreams.
- **Value to mcpx.** + low.
- **Effort.** S to send; more to render safely.
- **Risk.** Rendering upstream icons without the icon rules below.
- **Detail.** Not named in the changelog; it arrives through the mixin. Consumers MUST reject icon URIs with unsafe
  schemes (`javascript:`, `file:`, `ftp:`, `ws:`, local app schemes), should fetch without credentials, and should
  check that icons come from the server's origin.
- **Sources.** `schema/2025-11-25/schema.ts:550` "export interface Implementation extends BaseMetadata, Icons {";
  `2025-11-25/basic/index.mdx:247`; `2025-11-25/basic/index.mdx:251`; `2025-11-25/basic/index.mdx:252`.

## LV-18 `Implementation.websiteUrl`

- **What.** Optional `websiteUrl` (`@format uri`) on `Implementation`.
- **Where.** 2025-11-25 and 2026-07-28.
- **mcpx @ 05c78b2.** Not sent (`internal/mcpserver/server.go:525`).
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The 2025-11-25 changelog never mentions it.
- **Sources.** `schema/2025-11-25/schema.ts:567` "websiteUrl?: string;".

## LV-19 Per-request `_meta` `io.modelcontextprotocol/protocolVersion`

- **What.** Every 2026 request MUST carry its protocol version in
  `params._meta["io.modelcontextprotocol/protocolVersion"]`. There is no negotiation step: the server accepts or
  rejects each request on its own, and the version of the request is the version of the response.
- **Where.** 2026-07-28. opencode v2's SDK stamps it on every request of a modern connection.
- **mcpx @ 05c78b2.** The server reads it (`requestVersion`, `internal/mcpserver/server.go:813-822`) and refuses an
  unsupported one before dispatch (`internal/mcpserver/server.go:469-471`; the code is `errors.md` ERR-09). The client
  adds it to every modern request (`internal/mcpclient/modern.go:82`).
- **Value to mcpx.** + high: the core of the modern era.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The key is non-optional in the schema. On HTTP it MUST equal the `MCP-Protocol-Version` header: the
  schema doc says "400 Bad Request", the transport page says 400 with `-32020` (`errors.md` ERR-07). Absent means
  malformed (LV-24).
- **Sources.** `2026-07-28/basic/versioning.mdx:12` "There is no negotiation handshake. Every request carries its
  protocol"; `schema/2026-07-28/schema.ts:76`; `schema/2026-07-28/schema.ts:71-72`; `internal/mcpserver/server.go:469`;
  `internal/mcpclient/modern.go:82`; https://unpkg.com/@modelcontextprotocol/core@2.0.0/dist/auth-CUe6YdwF.mjs L24;
  `v2:packages/core/src/mcp/client.ts:248`.

## LV-20 Per-request `_meta` `io.modelcontextprotocol/clientCapabilities`

- **What.** Client capabilities are declared on every 2026 request; `{}` means "no optional capabilities", and the
  server MUST NOT carry them over from earlier requests.
- **Where.** 2026-07-28 (legacy declares once, in `initialize`). opencode v2's SDK sends it in the modern era.
- **mcpx @ 05c78b2.** The server builds a per-request `Peer` from `_meta` (`internal/mcpserver/conn.go:133-136`,
  `:150-166`); if the key is missing the capabilities are nil and the request is served rather than rejected (LV-24).
  The client always sends its own (`internal/mcpclient/modern.go:83`).
- **Value to mcpx.** + high: one connection can carry many agents with different capabilities.
- **Effort.** S.
- **Risk.** A proxy that caches capabilities per connection breaks the MUST NOT.
- **Detail.** The mcpx client declares `roots: {listChanged: false}` in the modern era
  (`internal/mcpclient/modern.go:52`) although 2026 `roots` is `{}` and deprecated; the shape of what mcpx declares is
  in the capabilities register. Because capabilities are per request, mcpx could declare upstream only what the
  originating host declared (for example url-mode elicitation); today its declaration is fixed per client.
- **Sources.** `schema/2026-07-28/schema.ts:96` "Servers MUST NOT infer capabilities from prior requests.";
  `2026-07-28/basic/index.mdx:377`; `internal/mcpserver/conn.go:133`; `internal/mcpclient/modern.go:83`.

## LV-21 Per-request `_meta` `io.modelcontextprotocol/clientInfo`

- **What.** Client identity moves to optional per-request `_meta`; clients SHOULD send it on every request unless
  configured not to. It is self-reported and display-only (`meta.md` META-08).
- **Where.** 2026-07-28. opencode v2's SDK sends it in the modern era.
- **mcpx @ 05c78b2.** The client sends it on every modern request (`internal/mcpclient/modern.go:84`). The server
  defines the key (`internal/mcpserver/conn.go:174`) but never reads it.
- **Value to mcpx.** + low: attributing upstream calls per agent in logs is allowed; changing behaviour on it is not.
- **Effort.** S.
- **Risk.** Using it for authorization or isolation would break the SHOULD NOT.
- **Detail.** SEP-2575 had it required; PR #3002 made it optional after the SEP reached Final.
- **Sources.** `2026-07-28/basic/index.mdx:384` "Clients **SHOULD** include `io.modelcontextprotocol/clientInfo` on
  every request"; `seps/2575-stateless-mcp.md:791`; `internal/mcpclient/modern.go:84`;
  `internal/mcpserver/conn.go:174`.

## LV-22 `io.modelcontextprotocol/serverInfo` in every result's `_meta`

- **What.** A 2026 server SHOULD identify itself on every result under
  `result._meta["io.modelcontextprotocol/serverInfo"]`, `DiscoverResult` included.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Never emitted: `HandleOn` adds only `resultType` (`internal/mcpserver/server.go:432-436`; wire
  W6, W7, W8). The client reads identity only from a top-level field (LV-23).
- **Value to mcpx.** + med: a modern client that wants to show which server answered finds nothing, and with LV-23 it
  has no identity from mcpx at all.
- **Effort.** S — stamp it where `resultType` is stamped, for modern peers only.
- **Risk.** Low (SHOULD).
- **Detail.** Legacy results must not get it. PR #3002 removed `DiscoverResult.serverInfo` "to avoid duplicate
  representations". The 2026 reserved-key table omits this key although the schema defines it (`meta.md` META-06).
- **Sources.** `2026-07-28/basic/index.mdx:396` "Servers **SHOULD** include the following `io.modelcontextprotocol/*`
  field in"; `schema/2026-07-28/schema.ts:157`; `seps/2575-stateless-mcp.md:795`; `2026-07-28/changelog.mdx:14`;
  `internal/mcpserver/server.go:432-436`; wire W6.

## LV-23 `DiscoverResult` has no top-level `serverInfo`

- **What.** 2026's `DiscoverResult` defines `supportedVersions`, `capabilities`, `instructions` and the cache fields.
  Server identity belongs in result `_meta` (LV-22), not in a `serverInfo` field.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The server sends a top-level `serverInfo` (`internal/mcpserver/server.go:535`; wire W5, W9) and
  the client reads it from there (`internal/mcpclient/client.go:285`, `:320`).
- **Value to mcpx.** + low on its own; it is the other half of LV-22.
- **Effort.** S.
- **Risk.** A validator that treats the result as closed rejects it; an open one ignores it and finds no identity.
- **Detail.** Sending a field the peer's revision does not define is a send-conservatively conflict.
- **Sources.** `schema/2026-07-28/schema.ts:678-697`; `internal/mcpserver/server.go:535`;
  `internal/mcpclient/client.go:285`; wire W5.

## LV-24 A 2026 request missing required `_meta` is malformed

- **What.** A 2026 request without `protocolVersion` or `clientCapabilities` in `_meta` is malformed; the server MUST
  reject it with `-32602`, and on HTTP with status 400.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Not rejected. No version: treated as legacy at 2025-03-26
  (`internal/mcpserver/conn.go:134-146`). Version present but no capabilities: capabilities nil, request served
  (`internal/mcpserver/conn.go:150-166`). Wire W9: a bare `server/discover` with no `_meta` and no headers was answered
  like W5 and issued a session.
- **Value to mcpx.** + med: conformance for modern peers. − rejecting every request without `_meta` would break lenient
  legacy clients that skip `initialize`, so reject only when `protocolVersion` is present and `clientCapabilities` is
  not.
- **Effort.** S.
- **Risk.** As is, a modern client with a bug silently gets legacy-shaped results.
- **Detail.** 2026 defines behaviour for "a request carrying modern `_meta`" and for "an `initialize` request"; a
  dual-era server receiving neither is not covered (see the process-model register). On HTTP, a server supporting
  pre-2025-06-18 clients MAY treat a missing version header as 2025-03-26, which matches mcpx's default. The same
  malformed request can also draw `-32020` for the missing header; the order is unspecified (`errors.md` ERR-14).
- **Sources.** `2026-07-28/basic/index.mdx:380` "A request missing any required field is malformed; the server
  **MUST** reject it with"; `2026-07-28/basic/index.mdx:381`; `internal/mcpserver/conn.go:144`; wire W9.

## LV-25 `server/discover` MUST be implemented

- **What.** Every 2026 server MUST answer `server/discover`, which returns supported versions, capabilities, optional
  instructions and identity. Clients MAY call it; it is the recommended stdio probe (LV-27). The request carries
  nothing but `_meta`, yet `params` itself is required.
- **Where.** 2026-07-28. opencode v2 calls it when a server's `protocol` is `auto` or pinned to `2026-07-28`.
- **mcpx @ 05c78b2.** Answered in any era (`internal/mcpserver/server.go:529-538`; wire W5, W9), with capabilities in
  the 2026 shape. The client probes it (`internal/mcpclient/client.go:296`).
- **Value to mcpx.** + high: without a conformant answer no modern client can learn what mcpx speaks.
- **Effort.** S — the field fixes are LV-23 and LV-26.
- **Risk.** High while the field names are wrong: modern interop is broken in both directions.
- **Detail.** `DiscoverResult extends CacheableResult`, so `ttlMs` and `cacheScope` are required; mcpx sends neither,
  and the 2026 changelog's cacheable-result item forgets to list `server/discover` (both covered in the caching
  register). The capability object mcpx puts in the answer, including a core `tasks` object 2026 does not define, is
  covered in the capabilities register. Answering it on a legacy connection is accept-liberally.
- **Sources.** `2026-07-28/server/discover.mdx:8` "capabilities, and identity before sending any other requests.
  Servers **MUST**"; `schema/2026-07-28/schema.ts:665-668`; `schema/2026-07-28/schema.ts:678` "export interface
  DiscoverResult extends CacheableResult {"; `2026-07-28/changelog.mdx:36`; `internal/mcpserver/server.go:529-538`;
  `v2:packages/core/src/mcp/client.ts:313`.

## LV-26 `DiscoverResult.supportedVersions`

- **What.** The version list in `DiscoverResult` is `supportedVersions: string[]`; the client picks one for its
  subsequent requests.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Both halves use the wrong name. The server answers `"protocolVersions": Supported`
  (`internal/mcpserver/server.go:534`); the client parses `protocolVersions` (`internal/mcpclient/client.go:284`), so
  against a conformant server its list is empty and it fails with "no shared protocol version; the server offers []"
  (`internal/mcpclient/client.go:314-318`). Tests pin the wrong name on both sides
  (`internal/mcpserver/server_test.go:393`, `internal/mcpclient/era_test.go:87`,
  `internal/mcpclient/modern_test.go:88`). Wire W5, W9.
- **Value to mcpx.** + high: this is the whole "mcpx as a modern server" and "mcpx as a modern client" path.
- **Effort.** S, plus the three tests.
- **Risk.** High: mcpx↔mcpx works only because both halves are wrong the same way; every conformant modern peer fails.
- **Detail.** The client's selection logic is right (newest mutually supported version, not the first listed); only
  the key is wrong. The stdio probe text says to "Select a mutually supported version from `supportedVersions`". The
  name `protocolVersions` may come from an early SEP draft. Over HTTP a modern upstream fails even earlier, on the
  missing `Mcp-Method` header (`transports.md` TR-37).
- **Sources.** `schema/2026-07-28/schema.ts:683` "supportedVersions: string[];"; `internal/mcpserver/server.go:534`
  "\"protocolVersions\": Supported,"; `internal/mcpclient/client.go:284`; `internal/mcpclient/client.go:314-318`;
  wire W5.

## LV-27 stdio probe order: `server/discover` before `initialize`

- **What.** A dual-era stdio client SHOULD send `server/discover`, with its preferred modern version, before anything
  else: a `DiscoverResult` means modern; a recognised modern error means modern (retry with a version from
  `supported`, do not fall back); any other error or a timeout means legacy (fall back to `initialize`). Modern-only
  clients are also recommended to probe.
- **Where.** 2026-07-28. opencode v2 with `protocol: "auto"` probes `server/discover` and falls back to legacy; its
  default is plain legacy.
- **mcpx @ 05c78b2.** The default is the reverse, `initialize` first (`internal/mcpclient/client.go:184`, `:231-232`);
  modern-first is opt-in per server (LV-32). The comment at `internal/mcpclient/client.go:193-197` defers the flip:
  "Legacy first is correct today and will stop being correct, which is why it is configurable".
  `docs/protocol.md:267` gives the same reason.
- **Value to mcpx.** + med: the spec's SHOULD. − legacy first saves a round trip on today's mostly legacy servers.
- **Effort.** S to flip the default, once LV-26 and LV-29 are fixed.
- **Risk.** A modern-only stdio server that treats a bad `initialize` as fatal and exits makes legacy-first fail where
  probe-first works; with LV-29 unfixed, legacy-first is exactly what makes spec-following modern servers unreachable.
- **Detail.** Legacy servers answer an unknown pre-`initialize` request with `-32601`, `-32602` or nothing, hence "or
  times out". The HTTP equivalent is LV-30. Cache the result (LV-31).
- **Sources.** `2026-07-28/basic/transports/stdio.mdx:124` "legacy version that requires an `initialize` handshake
  **SHOULD** probe with"; `internal/mcpclient/client.go:193-197`; `internal/mcpclient/client.go:232`;
  `docs/protocol.md:267`; `v2:packages/schema/src/mcp.ts:20`; `v2:packages/core/src/mcp/client.ts:313`.

## LV-28 Which probe errors mean "modern server"

- **What.** A recognised modern error (for example `-32022`) identifies a modern server; everything else is treated as
  legacy, and the legacy fallback MUST NOT be keyed to one specific error code.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** `isVersionError` is `strings.Contains(err.Error(), "-32022")`
  (`internal/mcpclient/client.go:327-331`). A modern server's `-32020` or `-32021` therefore triggers a legacy
  fallback, which a modern-only server also rejects. When `server/discover` itself draws `-32022`, the client aborts
  rather than retrying with a version from `data.supported`; that is moot today because mcpx knows one modern version
  (`internal/mcpclient/client.go:26`).
- **Value to mcpx.** + med: correct era decisions against modern servers that reject for reasons other than the
  version.
- **Effort.** S — parse the error code and treat `-32020..-32022` as modern.
- **Risk.** Wrong-era fallback and a misleading reported error.
- **Detail.** A substring test also matches `-32022` quoted anywhere in an error message. Pre-release SDKs that still
  send `-32004` are not recognised, which is the reading the 2026 allocation policy requires anyway (`errors.md`
  ERR-11).
- **Sources.** `2026-07-28/basic/transports/stdio.mdx:139` "The fallback **MUST NOT** be keyed to one specific error
  code: legacy servers"; `internal/mcpclient/client.go:330` "strings.Contains(err.Error(), \"-32022\")";
  `internal/mcpclient/client.go:26`.

## LV-29 Modern-only server refusing `initialize`; mcpx client aborts

- **What.** A modern-only server rejects `initialize` with an implementation-defined JSON-RPC error and SHOULD name the
  versions it supports, since a legacy client has no way to fall forward. A dual-era client that tried legacy first
  should fall forward to `server/discover`.
- **Where.** 2026-07-28 versioning page and its compatibility matrix.
- **mcpx @ 05c78b2.** `newClient` checks `isVersionError` after the first attempt regardless of which era it was
  (`internal/mcpclient/client.go:244-249`); the comment there assumes the first attempt was modern. So a modern server
  that answers `initialize` with `-32022`, as mcpx's own server does, makes the client give up. The era tests use a
  fake that answers `initialize` with `-32601`, so the case is untested (`internal/mcpclient/era_test.go:66-69`).
- **Value to mcpx.** + high: reachability of every modern-only server that follows the SHOULD.
- **Effort.** S — short-circuit only when the first attempt was modern.
- **Risk.** Not done: mcpx cannot connect to spec-following modern-only servers with its default settings.
- **Detail.** Over HTTP the mcpx client's `initialize` carries `MCP-Protocol-Version` but no `Mcp-Method`, so a
  modern-only HTTP server rejects it with 400 before method dispatch. If its header check runs first the body is
  `-32020`, which does not contain `-32022`, so the client does fall forward, and then fails again on the missing
  `Mcp-Method` (`transports.md` TR-37); if its version check runs first the body is `-32022` and the client aborts as
  above. The spec does not order the two checks (`errors.md` ERR-14).
- **Sources.** `2026-07-28/basic/versioning.mdx:154` "A server that supports only [modern](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#terminology) versions
  **SHOULD** name"; `2026-07-28/basic/versioning.mdx:170`; `internal/mcpclient/client.go:246` "if isVersionError(err)
  {"; `internal/mcpclient/era_test.go:66-69`.

## LV-30 HTTP era detection from the 400 body

- **What.** On Streamable HTTP a dual-era client MAY send a modern request first. On a 400 it SHOULD inspect the body:
  a recognised modern JSON-RPC error means modern (fix and retry); an empty or unrecognised body means fall back to
  `initialize`, and possibly further to HTTP+SSE (`transports.md` TR-11).
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The client uses the stdio logic (legacy first) and never consults the status for era
  (`internal/mcpclient/client.go:235-258`). A non-2xx answer becomes an error carrying the status and up to 4 KiB of
  body (`internal/mcpclient/http.go:136-140`), which `isVersionError` then greps.
- **Value to mcpx.** + med: as a server, mcpx must return 400 with a JSON-RPC body for modern errors for this to work
  in its favour; it returns 200 (`errors.md` ERR-13) and a plain JSON body for header mismatches (`errors.md` ERR-07).
- **Effort.** S.
- **Risk.** Dual-era clients misclassify mcpx, and mcpx misclassifies servers that reject for header reasons.
- **Detail.** Modern servers use 400 for `-32602` (missing `_meta`), `-32020`, `-32021` and `-32022`, and 404 with
  `-32601` for unknown methods.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:654` "era the server implements by attempting a modern
  request first. On"; `2026-07-28/basic/transports/streamable-http.mdx:660`; `internal/mcpclient/http.go:136-140`.

## LV-31 Era is a property of the server; cache it

- **What.** Era belongs to the server, not the request. Clients SHOULD cache it for the life of a stdio process or per
  HTTP origin, MAY persist it across restarts, and re-probe if the assumption fails.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** `Client.Era` is set once per connection (`internal/mcpclient/client.go:237`, `:257`); each new
  client, one per pool instance, probes again, and nothing persists across daemon restarts. `/v1/protocol` reports
  each server's era, negotiated version and preference (`internal/daemon/routes_proto.go:515`).
- **Value to mcpx.** + low: mcpx spawns many instances of one server, and each repeats the probe.
- **Effort.** S.
- **Risk.** None.
- **Detail.** For HTTP the key is the origin, not the URL path.
- **Sources.** `2026-07-28/basic/versioning.mdx:148` "The era determination is a property of the server, not of an
  individual"; `internal/mcpclient/client.go:237`; `internal/daemon/routes_proto.go:515`.

## LV-32 Per-server era setting

- **What.** A per-upstream setting that chooses or forces the era.
- **Where.** mcpx: `protocol: legacy | modern | force-legacy | force-modern`. opencode v2:
  `protocol: "legacy" | "auto" | "2026-07-28"`, default legacy; a pinned version the server does not support fails
  with a message naming the versions it does, and a pin must be a modern revision. opencode v1 has no such option.
- **mcpx @ 05c78b2.** Parsed at `internal/config/config.go:142-146` and mapped at `internal/pool/pool.go:636-646`, whose
  `default:` branch turns any unknown value, typos included, into `legacy`; config loading validates only command and
  URL (`internal/config/config.go:701-708`).
- **Value to mcpx.** + low: lets a user pin an era; typos should fail loudly.
- **Effort.** S — validate at load.
- **Risk.** Low: a misspelt `protocol` silently probes legacy first.
- **Detail.** `docs/opencode-plugin.md:187` says opencode v2 "negotiating `2026-07-28` per server" will exercise mcpx's
  modern path; that happens only when the user sets `protocol` for mcpx, because v2 defaults to legacy.
- **Sources.** `internal/pool/pool.go:645` "return mcpclient.PreferLegacy"; `internal/config/config.go:142-146`;
  `v2:packages/schema/src/mcp.ts:20` "Schema.Literals([\"legacy\", \"auto\", \"2026-07-28\"])";
  `v2:packages/schema/src/mcp.ts:23`; `docs/opencode-plugin.md:187`.

## LV-33 Dual-era server: era chosen per opening message

- **What.** A request carrying modern `_meta` is served statelessly under 2026; an `initialize` selects legacy
  semantics scoped to that stdio process or HTTP session; a dual-era server MAY serve both eras concurrently on one
  endpoint or process.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The peer is resolved per request, from `_meta` first and the connection's handshake otherwise
  (`internal/mcpserver/conn.go:133-147`); legacy capabilities are stored per connection
  (`internal/mcpserver/conn.go:101-105`).
- **Value to mcpx.** + high: this is the "speak every revision" goal.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** The rule is per request, so on stdio a request with modern `_meta` that follows an `initialize` on the
  same process is still served statelessly; mcpx does this because `_meta` is checked first. The gap for a request
  with neither is covered in the process-model register.
- **Sources.** `2026-07-28/basic/versioning.mdx:176` "- A request carrying modern per-request `_meta` is served
  statelessly"; `2026-07-28/basic/versioning.mdx:182`; `2026-07-28/basic/versioning.mdx:130`;
  `internal/mcpserver/conn.go:133`.

## LV-34 `ping` received from a modern client

- **What.** Legacy `ping` (either side; the receiver MUST answer promptly with `{}`) is removed in 2026 in both
  directions: servers cannot send requests, any request proves liveness client to server, and transport keep-alives do
  the rest.
- **Where.** 2024-11-05..2025-11-25. Absent from the 2026 schema altogether: removed, not deprecated.
- **mcpx @ 05c78b2.** The server answers `ping` in any era (`internal/mcpserver/server.go:543-544`); a modern one gets
  `{resultType: "complete"}` (wire W7).
- **Value to mcpx.** + low: harmless accept-liberally.
- **Effort.** S — done.
- **Risk.** None; `docs/protocol.md:78` is the statement of intent.
- **Detail.** A modern-only server answers `ping` with `-32601` (404 on HTTP). 2026 recommends SSE comment lines as
  keep-alive on long streams (`transports.md` TR-24).
- **Sources.** `2024-11-05/basic/utilities/ping.mdx:28` "1. The receiver **MUST** respond promptly with an empty
  response:"; `2026-07-28/changelog.mdx:20` "Remove `ping`, `logging/setLevel`, and
  `notifications/roots/list_changed`."; `seps/2575-stateless-mcp.md:569`; `internal/mcpserver/server.go:543`; wire W7.

## LV-35 `ping` sent by a client

- **What.** A client may ping a legacy server as a liveness check; against a 2026 server the method does not exist.
- **Where.** 2024-11-05..2025-11-25. lootbox's health monitor pings every server periodically and reconnects with
  backoff (fork only; the deployed upstream build has no monitor).
- **mcpx @ 05c78b2.** `Client.Ping` sends `ping` regardless of era (`internal/mcpclient/client.go:726-728`), but
  nothing in the pool or daemon calls it. The client does answer a server's `ping` in any era
  (`internal/mcpclient/modern.go:110-115`), which it once answered with method-not-found.
- **Value to mcpx.** + low: a probe could catch a wedged stateful server. − pinging an idle, lazily started server
  keeps it alive and defeats `idleTimeout`; pinging a modern upstream gets `-32601`, a false "dead".
- **Effort.** S — gate `Ping()` on the legacy era if it is ever used.
- **Risk.** Low while unused.
- **Detail.** For a modern upstream the equivalent check is `server/discover` or nothing.
- **Sources.** `internal/mcpclient/client.go:726-728`; `internal/mcpclient/modern.go:110-115`;
  `.lootbox/README.md:385-388` "Lootbox automatically monitors MCP server health with periodic `ping()`".

## LV-36 `resultType` required on every 2026 result

- **What.** Every 2026 result MUST carry `resultType`: `"complete"`, `"input_required"`, or an extension value
  advertised through capabilities. An unrecognised value MUST be treated as invalid; an absent one (legacy servers)
  MUST be treated as `"complete"`.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Stamped `"complete"` on modern results that lack one (`internal/mcpserver/server.go:432-436`,
  `:444-456`), deleted below 2026 (`internal/mcpserver/revisions.go:163-168`); wire W6, W7. The client reads it to spot
  `input_required` (`internal/mcpclient/client.go:663-665`).
- **Value to mcpx.** + high: done.
- **Effort.** S — done.
- **Risk.** None for ordinary results.
- **Detail.** The TypeScript type is open (`"complete" | "input_required" | string`) while the prose makes unknown
  values invalid. Error responses carry no `resultType`, correctly. The task-creation result stamped `"complete"` where
  the tasks extension says `"task"` is in the tasks register.
- **Also recorded from the content types register.** Clients treat an absent value as `"complete"` and an unrecognised
  one as invalid. The server stamps `complete` for modern peers (`internal/mcpserver/server.go:432-436`) and
  `downgrade()` deletes the field for anyone below 2026-07-28 (`internal/mcpserver/revisions.go:163-168`). One of the
  two branches that fire in production (CT-12). The deletion happens on a copy
  (`internal/mcpserver/revisions.go:139-145`), so a stored task result handed out twice is not mutated.
- **Sources.** `2026-07-28/basic/index.mdx:73` "The `result` **MUST** include a `resultType` field";
  `2026-07-28/basic/index.mdx:84`; `schema/2026-07-28/schema.ts:234` "resultType: ResultType;";
  `schema/2026-07-28/schema.ts:216`; `internal/mcpserver/server.go:452-454`.; `2026-07-28/basic/index.mdx:85` "clients **MUST** treat an absent `resultType` as `\"complete\"`."; `internal/mcpserver/server.go:439` "resp.Result = downgrade(resp.Result, peer.Version)"

## LV-37 Request-id uniqueness: per session → among in-flight

- **What.** Legacy: a request id MUST NOT have been used before by the requestor within the same session. 2026: an id
  MUST NOT match any other request the sender has issued and not yet had answered.
- **Where.** Session-scoped in 2024-11-05..2025-11-25; in-flight-scoped in 2026-07-28.
- **mcpx @ 05c78b2.** Server-initiated ids are negative so they cannot collide with a client's
  (`internal/mcpserver/conn.go:239-244`).
- **Value to mcpx.** + low: a pooled proxy may reuse ids after completion under 2026, never under legacy.
- **Effort.** S.
- **Risk.** None.
- **Detail.** `subscriptionId` is the listen request's id, so reusing an id while a listen stream is open is forbidden
  anyway (it is still in flight). MRTR retries MUST use a new id. Ids MUST NOT be `null` in every revision.
- **Sources.** `2024-11-05/basic/messages.mdx:28` "- The request ID **MUST NOT** have been previously used by the
  requestor within the same"; `2026-07-28/basic/index.mdx:48`; `seps/2567-sessionless-mcp.md:168`;
  `internal/mcpserver/conn.go:240`.

## LV-38 stdio server exits promptly on stdin EOF

- **What.** 2026 makes stdin EOF the primary portable shutdown signal: a server SHOULD exit promptly when its standard
  input is closed.
- **Where.** 2026-07-28 stdio page (legacy revisions describe only the client's side, LV-39).
- **mcpx @ 05c78b2.** `mcpx serve` exited at stdin EOF in every recorded stdio run; the read loop ends when the scanner
  does (`internal/mcpserver/server.go:1000-1073`).
- **Value to mcpx.** + med: hosts that only close the pipe get a clean exit.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** 2026 also gives Windows equivalents for the signals.
- **Sources.** `2026-07-28/basic/transports/stdio.mdx:102` "Servers **SHOULD** exit promptly when their standard input
  is closed or reads"; `internal/mcpserver/server.go:1000-1073`; wire S1–S5.

## LV-39 Client stdio shutdown: close stdin, wait, SIGTERM, SIGKILL

- **What.** The client SHOULD close the server's stdin, wait for it to exit, send SIGTERM if it does not exit in
  reasonable time, then SIGKILL.
- **Where.** Legacy lifecycle pages; the 2026 stdio page keeps the sequence. opencode v2 follows it (close stdin,
  wait 2 s, SIGTERM, SIGKILL after 2 s, whole process group). opencode v1 instead SIGTERMs descendants found with
  `pgrep -P` and closes the client, and only on instance disposal.
- **mcpx @ 05c78b2.** Closes stdin and immediately SIGTERMs the process group, then SIGKILLs after
  `plumbing.stdioDrainGrace` (3 s) (`internal/mcpclient/stdio.go:197-209`; `internal/defaults/defaults.json:40`).
- **Value to mcpx.** + low: servers that flush state on EOF get their chance.
- **Effort.** S — wait for exit (bounded by the grace setting) before SIGTERM.
- **Risk.** A server that saves on stdin EOF may be killed mid-write.
- **Detail.** Using the process group means grandchildren die too, which matters for servers that fork a browser
  (`internal/mcpclient/stdio.go:69`).
- **Sources.** `2024-11-05/basic/lifecycle.mdx:178` "1. First, closing the input stream to the child process (the
  server)"; `2025-11-25/basic/lifecycle.mdx:233-236`; `internal/mcpclient/stdio.go:202`;
  `v2:packages/core/src/mcp/stdio.ts:10`; `v2:packages/core/src/mcp/stdio.ts:57`;
  `v1:packages/opencode/src/mcp/index.ts:541`; `v1:packages/opencode/src/mcp/index.ts:546`.

## LV-40 Restart a stdio server that exited unexpectedly

- **What.** 2026: if the server process exits unexpectedly the client SHOULD restart it; in-flight requests are lost
  and listen streams are re-established. Statelessness makes the restart free; a legacy server needs a fresh
  `initialize`.
- **Where.** 2026-07-28. lootbox's fork reconnects with exponential backoff behind a circuit breaker. opencode v1 and v2
  never respawn: the server is marked failed ("Connection closed"), its tools disappear, and recovery is a manual
  reconnect (v2 also reconnects when that server's integration credential changes).
- **mcpx @ 05c78b2.** Dead instances are reaped (`internal/pool/pool.go:344-356`) and the next call starts a fresh
  one, which probes and initializes again; a failed start sets a cooldown that grows by `plumbing.restartBackoffStep`
  (2 s) up to `plumbing.restartBackoffMax` (30 s) (`internal/pool/pool.go:205-208`;
  `internal/defaults/defaults.json:49-50`).
- **Value to mcpx.** + med: as a client this works. As a server it matters the other way round: if `mcpx serve` dies,
  opencode loses mcpx until the user reconnects, so the stdio side should outlive daemon restarts.
- **Effort.** S.
- **Risk.** A daemon upgrade that kills `mcpx serve` silently removes mcpx from every open opencode session.
- **Detail.** The cap is triggered by an inline `30*time.Second` rather than by the setting
  (`internal/pool/pool.go:206`): the computed backoff is replaced by `restartBackoffMax` only once it exceeds 30 s, so
  a `restartBackoffMax` below 30 s is not a cap.
- **Sources.** `2026-07-28/basic/transports/stdio.mdx:111` "If the server process exits unexpectedly, the client
  **SHOULD** restart it."; `internal/pool/pool.go:344-356`; `internal/pool/pool.go:205-208`;
  `v1:packages/opencode/src/mcp/index.ts:448`; `v2:packages/core/src/mcp/index.ts:355`;
  `.lootbox/README.md:385-388`.
