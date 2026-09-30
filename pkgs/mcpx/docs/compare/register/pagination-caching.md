# Pagination and caching

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  cursor rules per revision, 2026 cacheable results (ttlMs, cacheScope), list stability, and how mcpx and opencode page and cache
```

Every list method pages with an opaque cursor in every revision; 2026 adds caching hints (`ttlMs`, `cacheScope`) that
six results MUST carry, and tightens what a cursor and a list may be. What matters most at `05c78b2` is that none of
mcpx's 2026 results carry the cache fields, and its client stops paging on an empty `nextCursor`, which 2026 says is a
valid cursor. Deterministic `tools/list` order is TOOL-23 in the tools register.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| PG-01 | opaque cursors; "don't persist across sessions" dropped in 2026 | `2024-11-05 has` `2026-07-28 has` | `24-11..25-11 ✓ (session-bound) · 26-07 ✓ (no sessions)` | ✓ — stateless encoded offsets | + low | S | low |
| PG-02 | `""` is a valid cursor and MUST NOT end the list | `2026-07-28 has` `mcpx missing` | `26-07 ✓ only · opencode v1 ✓ (only undefined ends)` | ✗ — client stops paging on `""` (#200) | + med | S | med |
| PG-03 | invalid cursor SHOULD be `-32602` | `2024-11-05 has` `mcpx missing` | `all five ✓ (SHOULD)` | ✗ — undecodable cursor silently restarts at zero (#202) | + low | S | low |
| PG-04 | servers SHOULD provide stable cursors | `2024-11-05 has` | `all five ✓ (SHOULD); 26-07 no cross-page guarantee` | partial — offsets shift if the list changes | + low | S | low |
| PG-05 | page size is the server's choice | `2024-11-05 has` | `all five ✓` | ✓ — `mcp.pageSize`, default 100 | + low | S | low |
| PG-06 | client page-following caps | `opencode v1 has` `opencode v2 has` | `opencode v1 1000 pages · v2 64 pages · spec silent` | partial — 100 pages, silent truncation, no loop check | + low | S | low |
| PG-07 | mcpx client swallows list errors and skips template pages | `2024-11-05 has` `mcpx missing` | `mcpx client` | ✗ — `prompts/list` and templates errors hidden (#203) | + low | S | med |
| PG-08 | six results MUST carry cache hints; changelog names five | `2026-07-28 has` `specs conflict` `mcpx missing` | `26-07 ✓ only` | ✗ — none of the six carry them (wire W5, W6, W18, W19) (#199) | + med | S | med |
| PG-09 | `ttlMs`: integer ms ≥ 0; absent means 0 | `2026-07-28 has` `mcpx missing` | `26-07 ✓ only` | ✗ — never emitted (#199) | + med | S | med |
| PG-10 | `cacheScope`: `"public"` or `"private"` | `2026-07-28 has` `mcpx missing` | `26-07 ✓ only` | ✗ — never emitted (#199) | + med | S | high |
| PG-11 | per-page caching: own TTL clock, same scope on every page | `2026-07-28 has` | `26-07 ✓ only` | n/a — until PG-08 | + low | S | low |
| PG-12 | MRTR retries and `input_required` results never cached | `2026-07-28 has` | `26-07 ✓ only` | n/a — mcpx caches no MRTR result | + low | S | low |
| PG-13 | mcpx's upstream schema cache ignores `ttlMs` | `2026-07-28 has` `mcpx missing` | `26-07 (client MAY cache per ttlMs)` | ✗ — cached until list_changed or refresh, persisted to disk (#200) | + med | S | med |
| PG-14 | lists MUST NOT vary per connection; MAY vary by authorization | `2026-07-28 has` | `26-07 ✓ only (tools, prompts, resources)` | ✓ — lists are per daemon | + med | S | med |

## PG-01 opaque cursors and persistence

- **What.** Cursors are opaque strings in every revision. 2024-11-05 through 2025-11-25 also tell clients not to persist
  cursors across sessions; 2026 drops that line (there are no sessions) and replaces it with PG-02's rule.
- **Where.** All five; the persistence rule is gone in 2026.
- **mcpx @ 05c78b2.** `nextCursor` is `base64url("o:<offset>")` (`internal/mcpserver/server.go:1477-1505`), encoded so
  that clients are not tempted to parse it (`internal/mcpserver/server.go:1472-1476`). It depends on no server state, so
  it stays valid across connections — what 2026 now permits.
- **Value to mcpx.** + already stateless.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** Being stateless, the cursor is also unsigned and unversioned (PG-04).
- **Sources.** `2024-11-05/server/utilities/pagination.mdx:17` "- The **cursor** is an opaque string token, representing
  a position in the result set"; `2025-11-25/server/utilities/pagination.mdx:93` "- Don't persist cursors across
  sessions"; `internal/mcpserver/server.go:1477-1505`

## PG-02 the empty-string cursor

- **What.** 2026: clients MUST NOT decide anything from a cursor's value except whether a non-null one was given; an
  empty string is a valid cursor and MUST NOT be treated as the end of results. Only an absent or null `nextCursor` ends
  the list.
- **Where.** 2026 only. opencode v1 already stops only on `undefined`.
- **mcpx @ 05c78b2.** As a server it never emits `""` (it adds `nextCursor` only when non-empty,
  `internal/mcpserver/server.go:551-555`). As a client it stops when `NextCursor == ""`
  (`internal/mcpclient/client.go:744`, `internal/mcpclient/client.go:766`, `internal/mcpclient/client.go:820`), and a Go
  `string` cannot tell `""` from absent, so a 2026 upstream that uses `""` as a real cursor is read as one page.
- **Value to mcpx.** + complete catalogues from every conformant 2026 server.
- **Effort.** S — decode `nextCursor` as `*string`.
- **Risk.** If not fixed: silent truncation of an upstream's tools, resources or prompts.
- **Detail.** mcpx's server treats an incoming `cursor: ""` as "start" (`internal/mcpserver/server.go:1483`) — harmless,
  since it never issues one.
- **Sources.** `2026-07-28/server/utilities/pagination.mdx:106` "non-null value was provided (e.g. an empty string is a
  valid cursor and"; `internal/mcpclient/client.go:744` "if res.NextCursor == "; `v1:packages/opencode/src/mcp/catalog.ts:29`

## PG-03 invalid cursors

- **What.** An invalid cursor SHOULD produce `-32602` (Invalid params). The 2026 schema's `InvalidParamsError` doc names
  "invalid or expired cursor values" and ships an example.
- **Where.** All five (SHOULD).
- **mcpx @ 05c78b2.** An undecodable cursor is ignored and the list restarts at offset 0
  (`internal/mcpserver/server.go:1483-1486`).
- **Value to mcpx.** + clients notice a stale cursor instead of looping or duplicating.
- **Effort.** S.
- **Risk.** Low. In 2026 an error is also the signal for a client to discard its cached pages (PG-11); a silent restart
  never sends it.
- **Detail.** None beyond the above.
- **Sources.** `2024-11-05/server/utilities/pagination.mdx:94` "Invalid cursors **SHOULD** result in an error with code
  -32602 (Invalid params)."; `2026-07-28/server/utilities/pagination.mdx:111`; `schema/2026-07-28/schema.ts:368`;
  `internal/mcpserver/server.go:1483-1486`

## PG-04 stable cursors

- **What.** Servers SHOULD provide stable cursors. 2026's caching page adds that there is no cross-page consistency
  guarantee: if data changes between pages, clients may see duplicates or gaps, and those wanting a snapshot SHOULD
  re-fetch from the start.
- **Where.** All five (SHOULD); the relaxation is 2026.
- **mcpx @ 05c78b2.** Cursors are plain offsets, not tied to a list version, so a list that changes between pages
  (an upstream reload, an adapter added) shifts silently (`internal/mcpserver/server.go:1477-1505`).
- **Value to mcpx.** + low: `tools/list` fits one page today (61 < 100).
- **Effort.** S — version or sign the cursor.
- **Risk.** Low, and within what 2026 explicitly tolerates.
- **Detail.** mcpx's own tool list never changes after it is built (TOOL-24, CAP-20), so only resource and prompt lists
  can shift.
- **Sources.** `2024-11-05/server/utilities/pagination.mdx:80` "- Provide stable cursors";
  `2026-07-28/server/utilities/caching.mdx:158` "- There is no cross-page consistency guarantee. If the underlying data
  changes between"

## PG-05 page size

- **What.** The server decides the page size; clients MUST NOT assume a fixed one.
- **Where.** All five.
- **mcpx @ 05c78b2.** `mcp.pageSize` (`internal/cli/serve.go:309`), default `"100"` written inline in the settings
  registry (`internal/settings/registry.go:417`), with a second inline `return 100` fallback
  (`internal/mcpserver/server.go:1469`). All four list methods use it.
- **Value to mcpx.** + low.
- **Effort.** S — move both defaults into `defaults.json`.
- **Risk.** Low. The page size is configurable; only its two defaults are inline constants, against mcpx's "nothing
  hardcoded" rule.
- **Detail.** With opencode v2's 64-page cap (PG-06), 100 per page allows 6,400 entries before v2 stops reading.
- **Sources.** `2024-11-05/server/utilities/pagination.mdx:18` "- **Page size** is determined by the server, and clients
  **MUST NOT** assume a fixed page"; `internal/cli/serve.go:309` "srv.PageSize = a.Settings().Int("; `internal/settings/registry.go:417`;
  `internal/mcpserver/server.go:1469` "return 100"

## PG-06 client page-following caps

- **What.** No revision caps how many pages a client follows; clients choose a cap to survive a server that never stops.
- **Where.** opencode v1 pages itself up to 1,000 pages and errors on a repeated cursor; opencode v2 relies on its SDK,
  which aggregates up to 64 pages.
- **mcpx @ 05c78b2.** `ListTools`, `ListResources` and `ListPrompts` follow at most 100 pages
  (`internal/mcpclient/client.go:734`, `internal/mcpclient/client.go:756`, `internal/mcpclient/client.go:807`), then
  return what they have without an error, and do not detect a repeated cursor.
- **Value to mcpx.** + low.
- **Effort.** S — report truncation, detect loops, move the literal into settings.
- **Risk.** A server that repeats a cursor costs 100 round trips; a very large one is truncated without notice.
- **Detail.** The cap is an inline `100` in three loops.
- **Sources.** `internal/mcpclient/client.go:734` "for i := 0; i < 100; i++ {"; `v1:packages/opencode/src/mcp/catalog.ts:12`
  "const MAX_LIST_PAGES = 1_000"; `v1:packages/opencode/src/mcp/catalog.ts:30`;
  https://unpkg.com/@modelcontextprotocol/client@2.0.0/dist/index.mjs L2904

## PG-07 mcpx client swallows list errors

- **What.** How a list reader treats an error mid-listing.
- **Where.** mcpx client.
- **mcpx @ 05c78b2.** `ListPrompts` returns whatever it has on any error, as if the server had no prompts
  (`internal/mcpclient/client.go:813-818`). `ListResources` requests `resources/templates/list` once, without a cursor,
  and ignores its error (`internal/mcpclient/client.go:772`); `ListResourceTemplates` does not page either
  (`internal/mcpclient/client.go:556-562`).
- **Value to mcpx.** + low.
- **Effort.** S — distinguish `-32601` from other errors; follow template cursors.
- **Risk.** A transient error hides a server's prompts until the next refresh, because the empty list is cached (PG-13);
  templates past the first page never appear.
- **Detail.** The pool calls these only when the server declared the capability, so "no prompts" is not the likely
  meaning of an error there.
- **Sources.** `internal/mcpclient/client.go:813-818` "A server without prompts answers method-not-found";
  `internal/mcpclient/client.go:772`; `internal/mcpclient/client.go:556-562`

## PG-08 which results must carry cache hints

- **What.** In 2026, complete results of `server/discover`, `tools/list`, `prompts/list`, `resources/list`,
  `resources/templates/list` and `resources/read` MUST carry `ttlMs` and `cacheScope`. `prompts/get`,
  `completion/complete`, `tools/call` and any `input_required` result do not. A cached response is keyed by the method
  plus the parameters that affect it (`uri`, `cursor`).
- **Where.** 2026 only. The changelog lists five methods and omits `server/discover`; the caching page and the schema
  (`DiscoverResult extends CacheableResult`) include it.
- **mcpx @ 05c78b2.** None of the six carry either field (`internal/mcpserver/server.go:529-538`,
  `internal/mcpserver/server.go:546-556`, `internal/mcpserver/server.go:574-593`,
  `internal/mcpserver/server.go:683-740`); wire W5, W6, W18 and W19 show the results without them.
- **Value to mcpx.** + schema-required; hosts could cache mcpx's small, fixed tool surface.
- **Effort.** S — stamp both fields for modern peers alongside `resultType`.
- **Risk.** Strict 2026 clients reject the results as schema-invalid.
- **Detail.** `resources/read` is cacheable while `prompts/get` is not — an asymmetry the schema makes explicit.
- **Sources.** `2026-07-28/server/utilities/caching.mdx:13` "Servers MUST include caching hints on results with";
  `2026-07-28/changelog.mdx:36` "Require `ttlMs` and `cacheScope` fields on results returned by";
  `schema/2026-07-28/schema.ts:678` "export interface DiscoverResult extends CacheableResult {";
  `schema/2026-07-28/schema.ts:1779` "export interface ListToolsResult extends PaginatedResult, CacheableResult {";
  `schema/2026-07-28/schema.ts:1229` "export interface ReadResourceResult extends CacheableResult {";
  `schema/2026-07-28/schema.ts:1634` "export interface GetPromptResult extends Result {";
  `2026-07-28/server/utilities/caching.mdx:31`

## PG-09 `ttlMs`

- **What.** An integer number of milliseconds the result may be considered fresh, like `Cache-Control: max-age`. Servers
  MUST send a value `>= 0`. `0` means immediately stale; absent means clients assume `0`; negative is treated as `0`.
  TTL is not a polling interval; clients that poll MUST add jitter and backoff. A `list_changed` notification invalidates
  a fresh cached list.
- **Where.** 2026 only.
- **mcpx @ 05c78b2.** Never emitted.
- **Value to mcpx.** + mcpx's own tool list is fixed for the daemon's life, so a long TTL is honest for `tools/list`;
  `ttlMs: 0` is always legal where freshness is unknown.
- **Effort.** S.
- **Risk.** As PG-08.
- **Detail.** A server MAY send `ttlMs` without `listChanged`, relying on TTL alone.
- **Sources.** `schema/2026-07-28/schema.ts:1094` "ttlMs: number;"; `2026-07-28/server/utilities/caching.mdx:56`
  "- If `ttlMs` is absent, clients **SHOULD** assume a default of `0` (immediately stale)";
  `2026-07-28/server/utilities/caching.mdx:60` "Servers **MUST** provide a `ttlMs` value that is `>= 0`.";
  `2026-07-28/server/utilities/caching.mdx:81`

## PG-10 `cacheScope`

- **What.** `"public"`: no user-specific data; any client, gateway or proxy MAY share it across users. `"private"`: reuse
  only within the same authorization context; caches MUST NOT be shared across contexts. Servers MUST NOT rely on
  `cacheScope` for access control.
- **Where.** 2026 only.
- **mcpx @ 05c78b2.** Never emitted.
- **Value to mcpx.** + `"private"` is the honest answer for a per-user daemon's resources; `"public"` fits mcpx's own
  tool list, which is identical for every caller. As a gateway in front of several hosts mcpx must honour `"private"` from
  upstreams by keying any cache on the upstream credential.
- **Effort.** S to emit; M to honour upstream scopes in a shared cache.
- **Risk.** High if gotten wrong: `"public"` on a per-user result lets a shared cache leak one user's resources to
  another.
- **Detail.** The spec's own guidance: `"public"` for lists identical for all users, `"private"` for user-dependent
  `resources/read` results and per-user filtered lists.
- **Sources.** `schema/2026-07-28/schema.ts:1109` "cacheScope: "; `2026-07-28/server/utilities/caching.mdx:101`;
  `2026-07-28/server/utilities/caching.mdx:178` "- MUST apply appropriate per-primitive access controls, and MUST NOT
  rely on"

## PG-11 caching paginated lists

- **What.** Each page is cached independently: its own `ttlMs` and freshness clock (servers MAY vary TTL per page), all
  pages of one list MUST share one `cacheScope`, an expired page is re-fetched by its cursor, and a cursor that becomes
  invalid means discarding every cached page and starting over.
- **Where.** 2026 only.
- **mcpx @ 05c78b2.** n/a until mcpx emits hints (PG-08). mcpx's silent restart on a bad cursor (PG-03) would deny a
  caching client the error that tells it to discard pages.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** None beyond the above.
- **Sources.** `2026-07-28/server/utilities/caching.mdx:153` "- Each page response carries its own `ttlMs` value. The
  freshness clock for each page"; `2026-07-28/server/utilities/caching.mdx:162`;
  `2026-07-28/server/utilities/caching.mdx:166` "Servers **MUST** apply the same `cacheScope` to all response pages for a
  given list"

## PG-12 MRTR retries are never cached

- **What.** `input_required` results carry no cache hints, and results of retried requests — those carrying
  `inputResponses` or `requestState` — MUST NOT be cached, because they depend on inputs outside the cache key.
- **Where.** 2026 only.
- **mcpx @ 05c78b2.** n/a — mcpx caches no MRTR result in either direction.
- **Value to mcpx.** + a guardrail for any future cache.
- **Effort.** S.
- **Risk.** None today.
- **Detail.** A `resources/read` that went through an MRTR round is cacheable by method but not by this rule.
- **Sources.** `2026-07-28/server/utilities/caching.mdx:23-25`; `2026-07-28/server/utilities/caching.mdx:36` "is,
  requests carrying `inputResponses` or `requestState`&mdash;**MUST NOT** be cached,"

## PG-13 mcpx's upstream schema cache ignores `ttlMs`

- **What.** A 2026 client MAY treat an upstream list as fresh for `ttlMs` and SHOULD re-fetch once stale.
- **Where.** 2026.
- **mcpx @ 05c78b2.** Upstream tools, resources and prompts are cached per pool until a `list_changed` arrives or someone
  refreshes, and the cache is persisted to disk across restarts (`internal/pool/pool.go:121-128`,
  `internal/pool/pool.go:446-455`, `internal/pool/pool.go:527-531`). The client's result types have no TTL field
  (`internal/mcpclient/client.go:131-134`).
- **Value to mcpx.** + fresher catalogues from modern upstreams, which today send mcpx no `list_changed` at all because it
  never opens a listen stream (notifications register).
- **Effort.** S.
- **Risk.** Stale catalogues until `mcpx refresh`.
- **Detail.** `cacheScope: "private"` would matter only if mcpx shared one cache across authorization contexts; the cache
  is per configured server, and each server carries one credential.
- **Sources.** `internal/pool/pool.go:446-455`; `internal/mcpclient/client.go:131-134`; `2026-07-28/changelog.mdx:36`

## PG-14 lists must not vary per connection

- **What.** `tools/list`, `prompts/list` and `resources/list` MAY change over time but MUST NOT vary per connection or as
  a side effect of other requests; they MAY vary by the authorization presented on the request.
- **Where.** 2026 only.
- **mcpx @ 05c78b2.** mcpx's tool list is built once per daemon (`internal/cli/serve_ask.go:242`) and resource and prompt
  listings come from per-server schema caches (`internal/pool/pool.go:121-128`), so no list depends on which connection
  asks.
- **Value to mcpx.** + keeps mcpx cacheable; rules out "load a namespace's tools into this session after the agent asks
  for it", a pattern mcpx should not adopt over MCP.
- **Effort.** S (policy).
- **Risk.** Per-connection lazy tool expansion would break 2026 caching.
- **Detail.** This is the list half of the 2026 statelessness change; the ordering half is TOOL-23 in the tools register.
- **Sources.** `2026-07-28/server/tools.mdx:65` "but **MUST NOT** vary"; `2026-07-28/server/tools.mdx:67` "**MAY** vary
  by the authorization presented on the request"; `2026-07-28/server/prompts.mdx:60`;
  `2026-07-28/server/resources.mdx:77`; `internal/cli/serve_ask.go:242`
