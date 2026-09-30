# Tools

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  Tool definition fields, JSON Schema rules, tools/list and CallToolResult per revision, and how mcpx and opencode handle each
```

Tools are the one MCP feature every host uses, and mcpx sits on both sides: it consumes upstream tool definitions to
catalogue, type and call them, and serves its own tool surface to hosts. What matters most at `05c78b2` is that
`mcpx_call`, the core proxy tool, drops the upstream `isError` and flattens every result to text (wire W20, W21), and that
mcpx's client drops `icons`, `execution` and `_meta` from upstream definitions while its own `tools/list` is 61 tools, not
the promised ten. Content block types are in the content-types register, the invalid-arguments conflict (`-32602` or
`isError`; 2026 prose and schema disagree) in the errors register, `ttlMs`/`cacheScope` and per-connection stability in
the pagination-caching register, and `Mcp-Param-*` header encoding in the transports register.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TOOL-01 | `Tool.name` format: 1–128 chars, `[A-Za-z0-9_.-]`, case-sensitive | `2025-11-25 has` | `24-11 ✓ (unique id only) · 25-03 same · 25-06 same · 25-11 ✓ (rules) · 26-07 ✓` | ✓ — own names comply | + low | S | low |
| TOOL-02 | aggregators SHOULD prefix a server id; `serverInfo.name` unreliable | `2026-07-28 has` | `26-07 ✓ only` | ✓ — namespaces by config key, not `serverInfo.name` | + med | S | low |
| TOOL-03 | host-side names `server_tool`; opencode v2 caps at 64, no dot | `opencode v1 has` `opencode v2 has` | `opencode v1 ✓ · v2 ✓ (≤64)` | partial — served as `mcpx`, names double to `mcpx_mcpx_*` | + low | S | low |
| TOOL-04 | `Tool.title` and display precedence title → annotations.title → name | `2025-06-18 has` | `24-11 — · 25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | partial — upstream title kept; own tools have none | + low | S | low |
| TOOL-05 | `ToolAnnotations.title` | `2025-03-26 has` | `24-11 — · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | partial — op tools only; the ten core tools lack it | + low | S | low |
| TOOL-06 | `Tool.description` optional; "hint to the model" wording from 2025-03-26 | `2024-11-05 has` | `all five ✓` | ✓ — always sent | + low | S | low |
| TOOL-07 | `inputSchema` opens to any 2020-12 keyword; root `type:"object"` stays | `2024-11-05 has` `2026-07-28 has` | `24-11..25-11 ✓ (restricted) · 26-07 ✓ (any keyword)` | ✓ — upstream schemas passed through raw | + med | M | med |
| TOOL-08 | no-parameter tools: `{type:"object", additionalProperties:false}` recommended | `2025-11-25 has` | `25-11 ✓ · 26-07 ✓` | ✓ — used by every no-arg mcpx tool | + low | S | low |
| TOOL-09 | `x-mcp-header` on inputSchema properties; HTTP clients drop invalid tools | `2026-07-28 has` `mcpx missing` | `26-07 ✓ only` | ✗ — neither validated nor mirrored (#200) | + med | M | med |
| TOOL-10 | JSON Schema 2020-12 is the default dialect | `2025-11-25 has` | `25-11 ✓ · 26-07 ✓ · earlier unspecified` | n/a — no validator; codegen reads 2020-12 and draft-07 keywords | + low | S | low |
| TOOL-11 | `$ref` MUST NOT dereference network URIs by default | `2026-07-28 has` | `26-07 ✓ only` | ✓ — codegen resolves only local `#/$defs/`, `#/definitions/` | + med | S | high |
| TOOL-12 | bound schema depth / subschemas / validation time | `2026-07-28 has` | `26-07 ✓ only (SHOULD)` | partial — codegen depth 12 and cycle guard; no validator | + low | S | low |
| TOOL-13 | `Tool.outputSchema`; object-rooted until 2026 | `2025-06-18 has` `2026-07-28 has` `opencode v1 has` `opencode v2 has` | `25-06 ✓ (object) · 25-11 ✓ (object) · 26-07 ✓ (any) · opencode v1 ✓ v2 ✓ (types)` | partial — parsed, unused by codegen; own tools declare none | + med | S | med |
| TOOL-14 | `outputSchema` ⇒ conforming `structuredContent` MUST; clients SHOULD validate | `2025-06-18 has` `opencode v1 has` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓ · opencode v1 enforces · v2 not traced` | n/a — validates nothing; declares no outputSchema | + low | S | med |
| TOOL-15 | `Tool.annotations` object; untrusted unless the server is trusted | `2025-03-26 has` | `24-11 — · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | partial — upstream kept raw; core ten tools carry none | + med | S | med |
| TOOL-16 | `readOnlyHint` (default false) | `2025-03-26 has` | `25-03 ✓ onward` | ✓ — op tools set it from `Mutating` | + med | S | low |
| TOOL-17 | `destructiveHint` (default true, only when not read-only) | `2025-03-26 has` `mcpx missing` | `25-03 ✓ onward` | ✗ — confirm gate reads absent as non-destructive (#207) | + med | S | med |
| TOOL-18 | `idempotentHint` (default false) | `2025-03-26 has` | `25-03 ✓ onward` | ✓ — op tools set it; not used for retries | + med | S | med |
| TOOL-19 | `openWorldHint` (default true) | `2025-03-26 has` | `25-03 ✓ onward` | partial — true on every op tool, including local ones | + low | S | low |
| TOOL-20 | `Tool.icons` | `2025-11-25 has` `mcpx missing` | `25-11 ✓ · 26-07 ✓` | ✗ — dropped on parse (#207) | + low | S | low |
| TOOL-21 | `Tool.execution.taskSupport`; `required` needs a task call | `2025-11-25 has` `2026-07-28 removes` `mcpx missing` | `25-11 ✓ only · opencode v1 SDK throws on required` | ✗ — not parsed; `required` upstream tools uncallable (#209) | + med | M | med |
| TOOL-22 | `_meta` on `Tool` | `2025-06-18 has` `specs conflict` `mcpx missing` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓ (schema; prose omits)` | ✗ — dropped on parse (#207) | + med | S | med |
| TOOL-23 | `tools/list` in deterministic order | `2026-07-28 has` | `26-07 ✓ only (SHOULD)` | ✓ — fixed slice; upstream cache sorted by name | + med | S | low |
| TOOL-24 | mcpx's `tools/list` is 61 tools, not the promised ten | `does not match mcpx's goal` | `mcpx only` | partial — ten core plus 51 generated op tools | + high | S | med |
| TOOL-25 | `tools/list` and other lists are not revision-filtered | `2025-06-18 has` `2025-11-25 has` | `mcpx design` | partial — safe today only because own tools carry few fields | + low | S | med |
| TOOL-26 | `CallToolResult.isError`; `mcpx_call` drops it | `2024-11-05 has` `mcpx missing` `opencode v1 has` `opencode v2 has` | `all five ✓ · opencode v1 ✓ v2 ✓ (thrown)` | ✗ — upstream failures reported as success (wire W20) (#206) | + high | S | high |
| TOOL-27 | `CallToolResult.structuredContent`; any JSON value in 2026 | `2025-06-18 has` `2026-07-28 has` `opencode v1 has` `opencode v2 has` | `25-06 ✓ (object) · 25-11 ✓ (object) · 26-07 ✓ (any)` | partial — downgraded below 2025-06-18; non-objects not downgraded | + med | S | med |
| TOOL-28 | text duplicate of `structuredContent`; which one each client reads | `2025-06-18 has` `opencode v1 has` `opencode v2 has` | `25-06..26-07 SHOULD · opencode v1 text first · v2 structured first` | partial — `mcpx_call` sends text only | + med | S | med |
| TOOL-29 | `mcpx_exec` returns artifacts as `resource_link` blocks | `2025-06-18 has` `mcpx has, others don't` | `25-06 ✓ onward · opencode v1 drops them` | ✓ — one link per artifact | + med | S | med |
| TOOL-30 | `tools/call` may answer `InputRequiredResult`; `task` param gone | `2026-07-28 has` | `26-07 ✓ only` | ✓ — both directions (gaps in the MRTR register) | + high | S | med |
| TOOL-31 | per-call permission prompts for MCP tools in the host | `opencode v1 has` `opencode v2 has` | `opencode v1 ✓ v2 ✓` | n/a — host-side | + low | S | low |

## TOOL-01 `Tool.name` format

- **What.** 2025-11-25 (SEP-986) adds SHOULD-level naming rules: 1–128 characters, case-sensitive, only ASCII letters,
  digits, `_`, `-` and `.`, no spaces or commas, unique within a server. Earlier revisions say only "Unique identifier".
- **Where.** Rules in 2025-11-25 and 2026; all revisions require a unique name.
- **mcpx @ 05c78b2.** Every name mcpx serves is `mcpx_` plus lower-case words and underscores (wire W6), well inside the
  rules. Upstream names are not re-exposed as MCP tools; they are reached through `mcpx_call {namespace, tool}` and
  through generated TypeScript, whose identifier rules are a code-mode concern.
- **Value to mcpx.** + low: nothing to change for its own names.
- **Effort.** S.
- **Risk.** Low. Accept liberally: these are SHOULDs, so upstream names outside them must still be callable.
- **Detail.** `.` is allowed by the spec but not by opencode v2's registry (TOOL-03), so a spec-conformant name like
  `admin.tools.list` is renamed by that host.
- **Sources.** `2024-11-05/server/tools.mdx:183` "- `name`: Unique identifier for the tool";
  `2025-11-25/server/tools.mdx:219` "Tool names **SHOULD** be between 1 and 128 characters in length (inclusive).";
  `2025-11-25/server/tools.mdx:221` "The following **SHOULD** be the only allowed characters";
  `internal/mcpserver/server.go:279`

## TOOL-02 aggregators SHOULD disambiguate colliding names

- **What.** 2026 adds a note: name uniqueness is per server; clients or proxies aggregating several servers SHOULD
  disambiguate (for example by prefixing a server identifier) and SHOULD NOT rely on `serverInfo.name`, which is not
  guaranteed unique.
- **Where.** 2026 only. It is the only place the specification speaks directly to proxies.
- **mcpx @ 05c78b2.** Namespaces come from the configuration key, sanitised, or an explicit `namespace`
  (`internal/config/config.go:480`), never from `serverInfo.name`; `mcpx_call` takes `namespace` and `tool` as separate
  arguments (wire W6), and prompts are re-exposed as `<ns>_<name>` (wire W19).
- **Value to mcpx.** + endorses the design mcpx already has.
- **Effort.** S.
- **Risk.** Using `serverInfo.name` would collide; mcpx does not.
- **Detail.** Sanitised namespaces can themselves collide (`fff-nix` and `fff_nix`); that and reserved-word identifiers
  are in the code-mode register.
- **Sources.** `2026-07-28/server/tools.mdx:324` "Tool name uniqueness is scoped to a single server. Clients or proxies
  that"; `2026-07-28/server/tools.mdx:330` "servers and **SHOULD NOT** be relied upon for disambiguation.";
  `internal/config/config.go:480` "Namespace:    pick(ex.Namespace, SanitizeNamespace(name)),"

## TOOL-03 host-side tool names in opencode

- **What.** opencode exposes each MCP tool as `sanitize(server)_sanitize(tool)`, replacing anything outside
  `[a-zA-Z0-9_-]` with `_`. v2 also registers the tool under namespace `server` (code-mode path
  `tools.<server>.<tool>`) and rejects a normalised tool name that does not match `^[A-Za-z0-9_-]{1,64}$`.
- **Where.** opencode v1 and v2. The spec allows 128 characters and `.`; opencode v2 allows 64 and no `.`.
- **mcpx @ 05c78b2.** mcpx's own tools are already prefixed `mcpx_` (`internal/mcpserver/server.go:279`), so an opencode
  server entry named `mcpx` yields `mcpx_mcpx_namespaces`. The longest mcpx tool name is far below 64 characters.
- **Value to mcpx.** + low: the doubled prefix costs tokens on every opencode v1 request outside code mode.
- **Effort.** S — document a short server key for opencode, or drop the `mcpx_` prefix when served over MCP.
- **Risk.** Low. v2 logs and skips an invalid registration rather than failing the server.
- **Detail.** Permission keys use the same `server_tool` form (TOOL-31), so users can allow `mcpx_*` wholesale.
- **Sources.** `v1:packages/opencode/src/mcp/catalog.ts:119` "export const toolName = (clientName: string, name:
  string) => sanitize(clientName) + "; `v2:packages/core/src/tool/mcp.ts:17`; `v2:packages/core/src/tool/mcp.ts:45`;
  `v2:packages/core/src/tool.ts:297`; `internal/mcpserver/server.go:279`

## TOOL-04 `Tool.title` and display precedence

- **What.** 2025-06-18 gives `Tool` a top-level `title` (via `BaseMetadata`) and fixes display precedence: `title`, then
  `annotations.title`, then `name`.
- **Where.** 2025-06-18 onward.
- **mcpx @ 05c78b2.** The client keeps an upstream `title` (`internal/mcpclient/client.go:119`). mcpx's own `Tool`
  struct has no `title` (`internal/mcpserver/server.go:260-268`), so hosts fall back to `annotations.title` (op tools) or
  the name (the ten core tools).
- **Value to mcpx.** + low: human-facing listings.
- **Effort.** S — but adding it needs revision filtering (TOOL-25).
- **Risk.** Sending `title` to a 2025-03-26 host would be a send-conservatively conflict, as P-2 is for prompts.
- **Detail.** The `BaseMetadata.title` doc singles out `Tool` as the one type where `annotations.title` outranks
  `name`.
- **Sources.** `schema/2025-06-18/schema.ts:930`; `schema/2025-11-25/schema.ts:1289` "Display name precedence order is:
  title, annotations.title, then name."; `internal/mcpclient/client.go:119`; `internal/mcpserver/server.go:260-268`

## TOOL-05 `ToolAnnotations.title`

- **What.** 2025-03-26's annotations object carries its own `title`, the only display name before `Tool.title`
  existed.
- **Where.** 2025-03-26 onward.
- **mcpx @ 05c78b2.** Every generated op tool carries `annotations.title` = the operation summary
  (`internal/api/ops.go:661`); the ten core tools (`mcpx_call`, `mcpx_exec`, …) carry no annotations at all (wire W6).
- **Value to mcpx.** + low: the tools a host most needs to label are the ten that lack one.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** Sending `annotations` to 2025-03-26 hosts is correct; it is the one tool field mcpx sends that is newer
  than 2024-11-05, which mcpx does not serve.
- **Sources.** `schema/2025-03-26/schema.ts:741` "title?: string;"; `internal/api/ops.go:661` "\"title\":
  o.Summary,"

## TOOL-06 `Tool.description`

- **What.** Optional human-readable description. 2025-03-26 adds that clients can use it to improve the model's
  understanding, "like a hint to the model".
- **Where.** All five; the field itself is unchanged.
- **mcpx @ 05c78b2.** Always sent for mcpx's own tools (`internal/mcpserver/server.go:261`, no `omitempty`); upstream
  descriptions are kept and rendered as JSDoc in generated types (code-mode register).
- **Value to mcpx.** + low; the descriptions are what a host's model pays for per tool (TOOL-24).
- **Effort.** S.
- **Risk.** None.
- **Detail.** None beyond the wording.
- **Sources.** `schema/2024-11-05/schema.ts:695` "description?: string;"; `schema/2025-03-26/schema.ts:793` "It can be
  thought of like a"; `internal/mcpserver/server.go:261`

## TOOL-07 `inputSchema` shape

- **What.** 2024-11-05 through 2025-11-25 type `inputSchema` as `{type:"object", properties?, required?}` (plus
  `$schema` from 2025-11-25). 2026 allows any JSON Schema 2020-12 keyword beside `type` (`oneOf`, `$ref`, `$defs`,
  `if/then/else`, …); the root must still be `type: "object"`.
- **Where.** Restricted through 2025-11-25; open in 2026.
- **mcpx @ 05c78b2.** Upstream schemas are kept as raw JSON (`internal/mcpclient/client.go:121`); codegen understands
  `anyOf`/`oneOf`/`allOf`, `prefixItems`, local `$ref` and `$defs`/`definitions` (`internal/codegen/ts.go:52-58`).
- **Value to mcpx.** + raw pass-through is already right; − typed codegen must keep up with composition keywords.
- **Effort.** M — codegen coverage, not the wire.
- **Risk.** Codegen that assumes flat `properties` produces wrong argument types.
- **Detail.** The older TypeScript type was narrower than what servers sent in practice; 2026 aligns the schema with
  reality (SEP-2106). opencode v1's SDK rejects a whole `tools/list` if any `inputSchema.type` is not the literal
  `"object"`, which every revision requires anyway.
- **Sources.** `schema/2024-11-05/schema.ts:699` "inputSchema: {"; `schema/2026-07-28/schema.ts:1997` "inputSchema: {
  $schema?: string; type: "; `2026-07-28/changelog.mdx:49` "Loosen `inputSchema` and `outputSchema` to allow any JSON
  Schema 2020-12 keywords"; `internal/mcpclient/client.go:121`; https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/types.js L1242

## TOOL-08 no-parameter tools

- **What.** `inputSchema` MUST be a valid JSON Schema object, never `null`; for tools with no parameters the recommended
  form is `{type:"object", additionalProperties:false}`; `{type:"object"}` is also valid.
- **Where.** 2025-11-25 and 2026.
- **mcpx @ 05c78b2.** Every no-argument mcpx tool uses exactly the recommended form
  (`internal/mcpserver/server.go:283`; wire W6).
- **Value to mcpx.** + already conforms.
- **Effort.** None.
- **Risk.** Upstreams sending `inputSchema: null` should still be tolerated when read (accept liberally).
- **Detail.** Guidance only.
- **Sources.** `2025-11-25/server/tools.mdx:201` "**MUST** be a valid JSON Schema object (not `null`)";
  `2025-11-25/server/tools.mdx:203`; `internal/mcpserver/server.go:283`

## TOOL-09 `x-mcp-header` parameter annotation

- **What.** A property in `inputSchema` may carry `x-mcp-header: "<Name>"`; a Streamable-HTTP client MUST mirror that
  argument into an `Mcp-Param-<Name>` header so intermediaries can route without parsing the body. Clients on HTTP MUST
  reject (drop from their `tools/list` view) any tool whose annotation breaks the constraints, and SHOULD log a warning.
- **Where.** 2026 only. stdio clients MAY ignore it.
- **mcpx @ 05c78b2.** Neither `x-mcp-header` nor `Mcp-Param` appears in `internal/`: annotated tools are neither
  validated nor mirrored when mcpx calls an HTTP upstream, and invalid ones are not filtered from mcpx's catalogue.
- **Value to mcpx.** + mcpx is an HTTP client of remote upstreams, where this is mandatory.
- **Effort.** M — validation plus header encoding (transports register).
- **Risk.** If not done: 2026 HTTP servers behind header-routing intermediaries reject or misroute mcpx's calls, and
  mcpx exposes tool definitions it was required to drop.
- **Detail.** Constraints on the value: non-empty; HTTP token syntax; no CR/LF; unique case-insensitively within the
  schema; only on `integer`, `string` or `boolean` properties (not `number`), integers within ±(2^53−1); only on
  properties statically reachable from the root. Servers SHOULD NOT mark secrets. A `null`/absent argument omits the
  header.
- **Also recorded from the transports register.** Not implemented; such tools would be listed in mcpx's catalog and
  callable through `mcpx_call`.
- **Sources.** `2026-07-28/server/tools.mdx:362` "Clients using the Streamable HTTP transport **MUST** reject tool
  definitions where any"; `2026-07-28/server/tools.mdx:346-359`;
  `2026-07-28/basic/transports/streamable-http.mdx:364` "While the use of `x-mcp-header` is optional for servers,
  clients **MUST**"; `2026-07-28/basic/transports/streamable-http.mdx:408`; `2026-07-28/basic/transports/streamable-http.mdx:402` "Clients using the Streamable HTTP transport **MUST** reject tool definitions"

## TOOL-10 JSON Schema 2020-12 as the default dialect

- **What.** Schemas without `$schema` are JSON Schema 2020-12; implementations MUST support 2020-12 and MUST handle
  unsupported dialects gracefully.
- **Where.** 2025-11-25 and 2026; earlier revisions leave the dialect unspecified.
- **mcpx @ 05c78b2.** mcpx has no JSON Schema validator (`go.mod` has no schema library). Codegen reads both 2020-12
  keywords (`$defs`, `prefixItems`) and draft-07 `definitions` (`internal/codegen/ts.go:52-58`).
- **Value to mcpx.** + low: matters only if mcpx starts validating arguments or results.
- **Effort.** S–M, depending on the validator chosen.
- **Risk.** A validator defaulting to draft-07 would misread `$defs`, `prefixItems` and friends.
- **Detail.** Applies to `inputSchema`, `outputSchema` and elicitation `requestedSchema`.
- **Sources.** `2025-11-25/basic/index.mdx:144` "**Default dialect**: When a schema does not include a `$schema` field,
  it defaults to"; `2025-11-25/changelog.mdx:33` "Establish JSON Schema 2020-12 as the default dialect";
  `internal/codegen/ts.go:52-58`

## TOOL-11 `$ref` network dereference forbidden by default

- **What.** Implementations MUST NOT automatically dereference `$ref`s that resolve to a network URI. An opt-in fetch
  mode MUST be off by default and SHOULD allowlist hosts, reject loopback/link-local/private addresses, apply timeouts
  and size limits, and log fetched URIs. A schema that fails on an unresolved external `$ref` SHOULD be rejected, not
  treated as permissive.
- **Where.** 2026 only.
- **mcpx @ 05c78b2.** Codegen resolves only `#/$defs/` and `#/definitions/` references
  (`internal/codegen/ts.go:263-276`) and renders anything else as `unknown` (`internal/codegen/ts.go:119`); nothing
  fetches.
- **Value to mcpx.** + keep it that way: a daemon on loopback that ingests untrusted upstream schemas is the SSRF case
  the rule exists for.
- **Effort.** S (none today).
- **Risk.** High if a future validator is added with default network loading.
- **Detail.** Rendering an unresolved external ref as `unknown` is permissive for types; that is codegen, not validation,
  so the SHOULD about rejecting such schemas does not bite yet.
- **Sources.** `2026-07-28/basic/index.mdx:301` "JSON Schema 2020-12 permits `$ref` to point at an absolute URI.
  Implementations **MUST NOT**"; `2026-07-28/server/tools.mdx:800`; `internal/codegen/ts.go:263-276`

## TOOL-12 bounds on composition keywords

- **What.** Implementations SHOULD bound schema depth, the number of subschemas, or validation time so a hostile schema
  cannot stall a validator.
- **Where.** 2026 only; SHOULD, no numbers given.
- **mcpx @ 05c78b2.** No validator. Codegen stops at depth 12 and breaks `$ref` cycles
  (`internal/codegen/ts.go:103-104`, `internal/codegen/ts.go:111-112`).
- **Value to mcpx.** + low: a pooled daemon reading many upstream schemas benefits if it ever validates.
- **Effort.** S–M.
- **Risk.** Low today.
- **Detail.** The depth 12 is an inline constant, against mcpx's "nothing hardcoded" rule.
- **Sources.** `2026-07-28/basic/index.mdx:315` "expressive schemas but can be expensive to validate. Implementations
  **SHOULD** apply"; `internal/codegen/ts.go:103` "if r.depth > 12 {"

## TOOL-13 `Tool.outputSchema`

- **What.** Optional JSON Schema for `structuredContent`. 2025-06-18 and 2025-11-25 require `type: "object"` at the root;
  2026 allows any 2020-12 schema (arrays, scalars).
- **Where.** 2025-06-18 onward; loosened in 2026. opencode v1 and v2 feed it into their code-mode type signatures.
- **mcpx @ 05c78b2.** The client parses it (`internal/mcpclient/client.go:122`), but codegen's `Tool` has no such field
  (`internal/codegen/ts.go:19-23`) and every generated function returns `ToolResult = any`
  (`internal/codegen/emit.go:458`). mcpx's own tools declare none.
- **Value to mcpx.** + typed results for `mcpx exec` scripts; codegen already unwraps `structuredContent`.
- **Effort.** S to type results; M to validate.
- **Risk.** If mcpx ever re-exposes upstream definitions over MCP, a 2026 upstream's array-rooted `outputSchema` is
  invalid for 2025-06-18/2025-11-25 hosts — a downgrade case `downgrade()` does not cover.
- **Detail.** The code-mode register has the typing comparison (opencode types results from it; lootbox and mcpx do
  not).
- **Sources.** `schema/2025-06-18/schema.ts:951` "outputSchema?: {"; `schema/2025-11-25/schema.ts:1277` "Currently
  restricted to type: "; `schema/2026-07-28/schema.ts:2005` "outputSchema?: { $schema?: string; [key: string]: unknown
  };"; `internal/mcpclient/client.go:122`; `internal/codegen/emit.go:458`;
  `v1:packages/opencode/src/tool/code-mode.ts:127`; `v2:packages/core/src/tool/mcp.ts:48`

## TOOL-14 `outputSchema` validation obligations

- **What.** If a tool declares `outputSchema`, servers MUST return conforming `structuredContent`; clients SHOULD validate
  it.
- **Where.** 2025-06-18 onward. opencode v1's SDK enforces it: a call to a tool with `outputSchema` that returns no
  `structuredContent` (and is not `isError`) throws, and mismatching content throws `InvalidParams`; if listing fails on
  an unresolvable `$ref` in some `outputSchema`, v1 re-lists with `outputSchema` stripped. v2 was not traced.
- **mcpx @ 05c78b2.** mcpx validates nothing upstream and declares no `outputSchema` on its own tools, so opencode v1's
  enforcement never trips on mcpx.
- **Value to mcpx.** + low today; a constraint on the future.
- **Effort.** S.
- **Risk.** If mcpx adds `outputSchema` to `mcpx_call` or `mcpx_exec`, every success path must emit `structuredContent`,
  or opencode v1 fails the call.
- **Detail.** v2 stops JSON-parsing text results once a tool declares `outputSchema` (TOOL-28).
- **Sources.** `2025-06-18/server/tools.mdx:314` "Servers **MUST** provide structured results that conform to this
  schema."; `2025-06-18/server/tools.mdx:315` "Clients **SHOULD** validate structured results against this schema.";
  `v1:packages/opencode/src/mcp/catalog.ts:14`; `v1:packages/opencode/src/mcp/catalog.ts:155`;
  https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/client/index.js L500–L501

## TOOL-15 `Tool.annotations`

- **What.** Optional behaviour hints on each tool (`title`, `readOnlyHint`, `destructiveHint`, `idempotentHint`,
  `openWorldHint`). Clients MUST treat them as untrusted unless they come from trusted servers.
- **Where.** 2025-03-26 onward; absent in 2024-11-05.
- **mcpx @ 05c78b2.** Upstream annotations are kept raw (`internal/mcpclient/client.go:128`). mcpx's generated op tools
  carry all five fields (`internal/api/ops.go:659-667`); the ten core tools carry none (wire W6), so by the defaults a
  host must assume `mcpx_call` and `mcpx_exec` are destructive, non-idempotent and open-world.
- **Value to mcpx.** + hosts drive confirm-before-run from these; `mcpx_namespaces` or `mcpx_types` marked read-only
  could be auto-approved.
- **Effort.** S.
- **Risk.** Trusting hints from an untrusted upstream; or, for mcpx's own tools, hosts asking more than they need to.
- **Detail.** For `mcpx_call` and `mcpx_exec` the honest answer depends on the upstream tool, so leaving them unannotated
  (defaults) is defensible; the discovery tools are unambiguously read-only.
- **Sources.** `schema/2025-03-26/schema.ts:809` "annotations?: ToolAnnotations;";
  `2025-03-26/server/tools.mdx:189` "tool annotations to be untrusted unless they come from trusted servers.";
  `internal/mcpclient/client.go:128`; `internal/api/ops.go:659-667`

## TOOL-16 `readOnlyHint`

- **What.** `true` means the tool does not modify its environment. Default `false`: absent is not read-only.
- **Where.** 2025-03-26 onward.
- **mcpx @ 05c78b2.** Op tools set `readOnlyHint: !o.Mutating` (`internal/api/ops.go:662`); mcpx does not read it on
  upstream tools.
- **Value to mcpx.** + the primary auto-approve signal for hosts.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** `destructiveHint` and `idempotentHint` are meaningful only when `readOnlyHint` is false.
- **Sources.** `schema/2025-03-26/schema.ts:748` "readOnlyHint?: boolean;"; `schema/2026-07-28/schema.ts:1921` "Default:
  false"; `internal/api/ops.go:662` "\"readOnlyHint\":    !o.Mutating,"

## TOOL-17 `destructiveHint`

- **What.** `true` means the tool may perform destructive updates; `false` means additive only. Meaningful only when
  `readOnlyHint` is false. Default **`true`**.
- **Where.** 2025-03-26 onward.
- **mcpx @ 05c78b2.** mcpx acts on it: with `elicit.confirmDestructive` on (default off), a call to an upstream tool
  annotated destructive asks through the broker first (`internal/settings/consumer.go:47-49`). But the check returns
  false when annotations or the field are absent (`internal/daemon/consumer.go:414-425`) — the opposite of the spec
  default — and ignores `readOnlyHint`. Op tools set it from `o.Destructive` (`internal/api/ops.go:663`).
- **Value to mcpx.** + the confirmation gate is a real safety feature for scripts; it should use the spec default.
- **Effort.** S — absent `destructiveHint` with `readOnlyHint` not true counts as destructive.
- **Risk.** If not fixed: an unannotated upstream tool that deletes things runs unconfirmed with the setting on. If
  fixed: more confirmations for unannotated tools, which is what the default means.
- **Detail.** Reading absent as non-destructive is exactly the misreading the spec default exists to prevent.
- **Sources.** `schema/2025-03-26/schema.ts:758` "destructiveHint?: boolean;"; `schema/2025-03-26/schema.ts:756`
  "Default: true"; `internal/daemon/consumer.go:414-425`; `internal/settings/consumer.go:49` "ask before a call to a
  tool annotated destructiveHint"

## TOOL-18 `idempotentHint`

- **What.** `true` means repeating a call with the same arguments has no further effect. Meaningful only when not
  read-only. Default `false`.
- **Where.** 2025-03-26 onward.
- **mcpx @ 05c78b2.** Op tools set `idempotentHint: !o.Mutating || o.Destructive` (`internal/api/ops.go:664`). mcpx does
  not consult it before retrying anything.
- **Value to mcpx.** + 2026 removed stream resumability, so after a dropped 2026 stream the only safe-retry signal the
  protocol offers is this hint.
- **Effort.** S.
- **Risk.** Retrying a non-idempotent tool after a dropped stream double-executes.
- **Detail.** 2026: a broken stream means the request is lost and clients MUST re-issue it as a new request.
- **Sources.** `schema/2025-03-26/schema.ts:768` "idempotentHint?: boolean;"; `2026-07-28/changelog.mdx:28` "clients
  **MUST** re-issue it as a new request with a new request ID"; `internal/api/ops.go:664`

## TOOL-19 `openWorldHint`

- **What.** `true` means the tool interacts with an open world of external entities (web search, say); `false` a closed
  domain. Default **`true`**.
- **Where.** 2025-03-26 onward.
- **mcpx @ 05c78b2.** Every op tool declares `openWorldHint: true` (`internal/api/ops.go:665`), including ones that only
  read the local daemon (`mcpx_health`, `mcpx_settings_get`; wire W6).
- **Value to mcpx.** + low: `false` for daemon-local operations is the accurate answer.
- **Effort.** S.
- **Risk.** Low — hosts may be more cautious than needed.
- **Detail.** Operations that reach a registry or an upstream (`mcpx_registry_search`, `mcpx_resource_read`) are
  genuinely open-world.
- **Sources.** `schema/2025-03-26/schema.ts:778` "openWorldHint?: boolean;"; `schema/2025-03-26/schema.ts:776`
  "Default: true"; `internal/api/ops.go:665` "\"openWorldHint\":   true,"

## TOOL-20 `Tool.icons`

- **What.** `Tool extends BaseMetadata, Icons`: optional `icons: Icon[]` with `src`, `mimeType`, `sizes`, `theme`.
- **Where.** 2025-11-25 and 2026.
- **mcpx @ 05c78b2.** The client `Tool` struct has no `icons` field, so they are dropped on parse
  (`internal/mcpclient/client.go:117-129`).
- **Value to mcpx.** + plugin/TUI display.
- **Effort.** S.
- **Risk.** Loss of display data only.
- **Detail.** Clients that render icons MUST support PNG and JPEG and SHOULD support SVG and WebP; SVG can carry script.
- **Sources.** `schema/2025-11-25/schema.ts:1249` "export interface Tool extends BaseMetadata, Icons {";
  `2025-11-25/changelog.mdx:13` "Allow servers to expose icons as additional metadata for tools, resources, resource
  templates, and prompts"; `internal/mcpclient/client.go:117-129`

## TOOL-21 `Tool.execution.taskSupport`

- **What.** Per-tool task negotiation: `"forbidden"` (default), `"optional"`, `"required"`. For `required`, clients MUST
  call the tool as a task or get `-32601`. The server must also declare `tasks.requests.tools.call`.
- **Where.** 2025-11-25 only; removed from the 2026 schema (the tasks extension has no per-tool negotiation), which the
  2026 changelog does not mention. opencode v1's SDK throws `InvalidRequest` when asked to call a tool cached as
  task-required; neither opencode version declares client tasks.
- **mcpx @ 05c78b2.** Not parsed (`internal/mcpclient/client.go:117-129`); `CallTool` sends only `{name, arguments}`
  (`internal/mcpclient/client.go:845-858`) and no `tasks/*` method exists in the client or pool. A 2025-11-25 upstream
  tool with `taskSupport: "required"` answers `-32601` and mcpx has no way around it. mcpx's own tools carry no
  `execution` (`internal/mcpserver/server.go:260-268`), which makes its declared `tasks.requests.tools.call` unreachable
  (capabilities register).
- **Value to mcpx.** + long-running upstream jobs (CI, batch) survive disconnects; mcpx already has a task store to bridge
  them.
- **Effort.** M.
- **Risk.** If not done: a growing class of servers is uncallable through mcpx. If mcpx ever marks its own tool
  `required`, opencode v1 cannot call it.
- **Detail.** `docs/protocol.md` says upstream task status notifications are dropped because "mcpx polls task state";
  mcpx polls nothing upstream (tasks register).
- **Also recorded from the tasks register.** 2025-11-25. opencode v1's SDK throws `InvalidRequest` before sending a
  call to a tool cached as task-required; neither opencode version can run tasks. The client sends `tools/call` with
  `{name, arguments}` only (`internal/mcpclient/client.go:845-858`), never `params.task`, and handles no
  `CreateTaskResult`; no `tasks/*` method exists in `internal/mcpclient` or `internal/pool`. Such a tool answers
  `-32601` and mcpx has no way around it. In 2026 the equivalent is declaring the extension per request and accepting
  server-chosen tasks, which mcpx's client does not do either. `docs/protocol.md:297-299` claims mcpx "polls task
  state"; with no upstream tasks there is nothing to poll (TASK-14).
- **Sources.** `schema/2025-11-25/schema.ts:1241` "taskSupport?: "; `2025-11-25/basic/utilities/tasks.mdx:117` "If
  `execution.taskSupport` is `"; `internal/mcpclient/client.go:849`; https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/client/index.js L490–L494;
  `v1:packages/opencode/src/mcp/index.ts:48` "// tasks: {},"; <https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/client/index.js> (L490–L494).

## TOOL-22 `_meta` on `Tool`

- **What.** 2025-06-18 adds `_meta` to `Tool` (and to resources, templates, prompts and content blocks). Keys whose second
  label is `modelcontextprotocol` or `mcp` are reserved.
- **Where.** 2025-06-18 onward. The 2026 schema has it; the 2026 Tool data-type prose list omits it.
- **mcpx @ 05c78b2.** The client `Tool` struct has no `_meta`, so it is dropped on parse
  (`internal/mcpclient/client.go:117-129`).
- **Value to mcpx.** + MCP Apps and other extensions hang per-tool data here; a proxy that drops it breaks them.
- **Effort.** S — keep it raw, like `annotations`.
- **Risk.** Extension metadata silently lost.
- **Detail.** The prose/schema gap in 2026 is an omission, not a removal: the schema is authoritative. Upstream
  *result* `_meta` loss is in the `_meta` register.
- **Sources.** `schema/2025-06-18/schema.ts:967` "_meta?: { [key: string]: unknown };";
  `2025-06-18/changelog.mdx:35` "Add `_meta` field to additional interface types"; `schema/2026-07-28/schema.ts:2014`;
  `2026-07-28/server/tools.mdx:286-302`; `internal/mcpclient/client.go:117-129`

## TOOL-23 deterministic `tools/list` order

- **What.** Servers SHOULD return tools in the same order across requests while the set is unchanged, for client
  caching and LLM prompt-cache hits.
- **Where.** 2026 only; SHOULD; `tools/list` alone (not prompts or resources).
- **mcpx @ 05c78b2.** The ten core tools come first, then adapter, OpenAPI and op tools in declaration order
  (`internal/mcpserver/server.go:376-378`; `internal/cli/serve.go:332-347`). The upstream schema cache is sorted by name
  (`internal/pool/pool.go:501-503`).
- **Value to mcpx.** + prompt-cache hits for every host.
- **Effort.** S (done).
- **Risk.** Go map iteration would randomise a listing built from a map; none is today.
- **Detail.** Merges the tools and caching findings on the same rule; the pagination-caching register refers here.
- **Sources.** `2026-07-28/server/tools.mdx:71` "Servers **SHOULD** return tools in a deterministic order";
  `2026-07-28/changelog.mdx:34` "Servers **SHOULD** return tools from `tools/list` in a deterministic order";
  `internal/mcpserver/server.go:376-378`; `internal/pool/pool.go:501-503`

## TOOL-24 mcpx's `tools/list` is 61 tools

- **What.** Besides the ten core tools, every generated `/v1` operation is exposed as its own MCP tool — 61 in total
  with one upstream configured. The package doc and comments say "Ten tools instead of three hundred".
- **Where.** mcpx only; the aim is to keep upstream schemas out of the host's context.
- **mcpx @ 05c78b2.** Base list `internal/mcpserver/server.go:276-380`; op tools from `api.Ops()`
  (`internal/mcpserver/ops.go:30-60`), appended in `internal/cli/serve.go:344`. Wire W6 returned 61 tools.
- **Value to mcpx.** − about 51 extra schemas in every host's context, the cost mcpx exists to avoid; + the parity
  principle (CLI = /v1 = MCP) is why they are there.
- **Effort.** S — gate op tools behind a setting, or expose one dispatcher tool.
- **Risk.** Token cost on every request of every host that sends tool schemas to its model directly.
- **Detail.** A product decision between two stated principles; the "ten" comment is stale either way.
- **Sources.** `internal/mcpserver/server.go:12-13` "Ten tools"; `internal/mcpserver/server.go:274-275` "These ten
  reach"; `internal/cli/serve.go:344` "extras = append(extras, a.v1OpTools()...)"; `internal/mcpserver/ops.go:30-60`

## TOOL-25 lists are not revision-filtered

- **What.** mcpx builds every result in the newest shape and spells it down in `downgrade()`, but `downgrade()` rewrites
  only content blocks, prompt messages, `structuredContent` and `resultType`; it never touches `tools/list` or any other
  list result.
- **Where.** mcpx design. Tool `title`/`outputSchema`/`_meta` are 2025-06-18 fields; `icons`/`execution` are 2025-11-25.
- **mcpx @ 05c78b2.** mcpx's tools carry only `name`, `description`, `inputSchema` and `annotations` (wire W6 key set;
  `internal/mcpserver/server.go:260-268`), all defined in 2025-03-26, so nothing leaks today.
  `internal/mcpserver/server.go:439` applies `downgrade` to every result; `internal/mcpserver/revisions.go:132-170`
  shows what it handles.
- **Value to mcpx.** + low now; + necessary before adding `title`, `outputSchema`, `icons` or `execution`.
- **Effort.** S.
- **Risk.** The first newer field added to a tool leaks to older hosts; prompts already leak `title` to 2025-03-26 (prompts
  register).
- **Detail.** `downgrade()` copies before editing, so stored task results are not mutated.
- **Sources.** `internal/mcpserver/server.go:439` "resp.Result = downgrade(resp.Result, peer.Version)";
  `internal/mcpserver/revisions.go:132-170`; `internal/mcpserver/server.go:260-268`

## TOOL-26 `CallToolResult.isError`, and `mcpx_call` dropping it

- **What.** Tool execution failures are reported in the result with `isError: true`, not as protocol errors, so the model
  can see and react to them.
- **Where.** All five. opencode v1 throws an error built from the text blocks (or "MCP tool returned an error") and
  discards non-text blocks; v2 raises a `ToolFailure` the same way. lootbox hands the envelope to the script as a
  successful value (code-mode register).
- **mcpx @ 05c78b2.** mcpx's own failures are correct: a failing tool becomes `isError: true`
  (`internal/mcpserver/server.go:666-674`). But `mcpx_call` launders upstream failures: `/v1/call` returns 200 with the
  raw result (`internal/daemon/server.go:694-703`), `mcpBackend.Call` returns `renderResult(res.Result), nil`
  (`internal/cli/serve.go:103-117`), and `renderResult` parses `IsError` and ignores it
  (`internal/cli/commands.go:385-415`). Wire W20: upstream `boom` with `isError: true` came back as
  `{"content":[{"text":"boom: deliberate failure","type":"text"}],"resultType":"complete"}`.
- **Value to mcpx.** + correctness of the core proxy tool.
- **Effort.** S — return an error from `mcpBackend.Call` when the upstream result has `isError`.
- **Risk.** If not fixed: agents act on error text as if it were data.
- **Detail.** Scripts are unaffected: the generated client throws `ToolError` on `isError`
  (`internal/codegen/emit.go:493-500`). A task created for such a call is marked `failed` only if mcpx's own result has
  `isError`, which here it never does. The ask path sets `isError` only for transport failures
  (`internal/cli/serve_ask.go:175-181`). Whether bad arguments are `isError` or `-32602` is in the errors register.
- **Also recorded from the content types register.** `isError` exists in every revision. opencode v1 and v2 would turn
  an `isError` result into a thrown error carrying the joined text blocks (non-text blocks of an error result are
  discarded), if mcpx passed it on. `/v1/call` returns 200 with the raw result (`internal/daemon/server.go:694-703`);
  `renderResult` parses `IsError` and ignores it (`internal/cli/commands.go:392`); the ask path sets `IsError` only
  for transport failures (`internal/cli/serve_ask.go:175-181`). Wire W20: fakemcp's `boom` came back as
  `{"content":[{"text":"boom: deliberate failure","type":"text"}],"resultType":"complete"}`. Scripts are unaffected:
  the generated client throws `ToolError` (CT-19). The errors area records the protocol-error-vs-`isError` rules this
  sits on.
- **Sources.** `schema/2024-11-05/schema.ts:663` "isError?: boolean;"; `internal/cli/commands.go:392`;
  `internal/cli/serve.go:116` "return renderResult(res.Result), nil"; `internal/mcpserver/server.go:666-674`;
  `v1:packages/opencode/src/mcp/catalog.ts:68`; `v2:packages/core/src/tool/mcp.ts:78`; wire W20

## TOOL-27 `CallToolResult.structuredContent`

- **What.** 2025-06-18 adds `structuredContent: {[key]: unknown}` (an object). 2026 retypes it `unknown`: any JSON value,
  including arrays, scalars and `null` (distinct from absent).
- **Where.** Absent in 2024-11-05 and 2025-03-26; object in 2025-06-18 and 2025-11-25; any JSON in 2026. The same change
  applies to sampling's `ToolResultContent.structuredContent`.
- **mcpx @ 05c78b2.** For hosts below 2025-06-18, `downgrade()` removes it and appends it as an indented JSON text block
  (`internal/mcpserver/revisions.go:146-156`). There is no downgrade of a non-object value for 2025-06-18/2025-11-25
  hosts. In practice mcpx never emits `structuredContent` (CT-13), so the branch is dead today.
- **Value to mcpx.** + needed as soon as mcpx relays structured results.
- **Effort.** S — wrap or textify non-object values for 2025-06-18/2025-11-25 hosts.
- **Risk.** A 2026 upstream returning `[...]` or `42` relayed verbatim is schema-invalid for 2025-06-18/2025-11-25 hosts.
- **Detail.** opencode v1 treats `structuredContent: null` like absent (TOOL-28), which erases the 2026 distinction.
- **Also recorded from the content types register.** 2025-06-18 onward. opencode v1's direct path uses it only when
  `content` is empty (then as `JSON.stringify` text); v1 code mode prefers it over text; v2 makes it the tool's
  `output` whenever present. Below 2025-06-18, `downgrade()` deletes it and appends it as an indented JSON text block
  (`internal/mcpserver/revisions.go:146-156`). For 2025-06-18 and 2025-11-25 clients a non-object value from a 2026
  upstream is passed unchanged, which their schema forbids. mcpx itself emits no `structuredContent` through
  `mcpx_call` (CT-13), so the branch is dead in production. `null` is a legal 2026 value distinct from absent.
  opencode v1's SDK also enforces "tool has `outputSchema` ⇒ result has `structuredContent`" (tools area).
- **Sources.** `schema/2025-06-18/schema.ts:828` "structuredContent?: { [key: string]: unknown };";
  `schema/2026-07-28/schema.ts:1821` "structuredContent?: unknown;"; `schema/2025-11-25/schema.ts:1891`;
  `schema/2026-07-28/schema.ts:2469`; `internal/mcpserver/revisions.go:146-156`; `2026-07-28/server/tools.mdx:500` "For backwards compatibility, a tool that returns structured content SHOULD also return the serialized JSON in a TextContent block."; `internal/mcpserver/revisions.go:200-203`; `v1:packages/opencode/src/mcp/catalog.ts:75`; `v1:packages/opencode/src/tool/code-mode.ts:109`; `v2:packages/core/src/tool/mcp.ts:97`

## TOOL-28 `structuredContent` versus its text duplicate

- **What.** For backwards compatibility a tool returning `structuredContent` SHOULD also return the serialized JSON in a
  text block. Clients then pick one.
- **Where.** 2025-06-18 onward. opencode v1 returns `content` as-is when non-empty and uses `structuredContent` only
  when `content` is empty (and not `null`); v2 makes `structuredContent` the tool's `output`, else parses text that starts
  with `{`/`[` as JSON when the tool has no `outputSchema`, else passes text.
- **mcpx @ 05c78b2.** `mcpx_call` sends one text block and never `structuredContent` (CT-13), so v2 code mode gets data
  only through its JSON-looking-text heuristic. Generated script clients return `structuredContent` when present
  (`internal/codegen/emit.go:501`).
- **Value to mcpx.** + returning upstream `structuredContent` alongside the text would give v2 real values and keep v1
  working.
- **Effort.** S.
- **Risk.** Without the text duplicate, opencode v1 shows the model nothing useful; without `structuredContent`, v2
  guesses.
- **Detail.** 2026 prose adds that `structuredContent` is unrelated to LLM "structured outputs".
- **Sources.** `2026-07-28/server/tools.mdx:500` "For backwards compatibility, a tool that returns structured content
  SHOULD also return the serialized JSON in a TextContent block."; `v1:packages/opencode/src/mcp/catalog.ts:75`;
  `v2:packages/core/src/tool/mcp.ts:96`; `v2:packages/core/src/tool/mcp.ts:100`

## TOOL-29 `mcpx_exec` artifacts as `resource_link`

- **What.** Files a script produces come back as `resource_link` blocks (`uri` `mcpx://artifacts/{id}`, `name`,
  `mimeType`, `description`), read later with `resources/read` if the host wants the bytes.
- **Where.** `resource_link` exists from 2025-06-18. Neither opencode nor lootbox returns artifacts this way; opencode v1
  silently drops `resource_link` from direct tool results, and v2 keeps only the `uri` as text.
- **mcpx @ 05c78b2.** Built in `internal/cli/serve.go:174-187`, carried through the string result with a U+001E marker
  (`internal/mcpserver/toolresult.go:21`), appended in `tools/call` (`internal/mcpserver/server.go:676-681`); for
  2025-03-26 hosts it becomes an embedded `resource` (content-types register).
- **Value to mcpx.** + keeps binaries out of context.
- **Effort.** S (done); + S to also name artifact URIs in the text block so opencode v1 users see them.
- **Risk.** Artifacts appear to vanish in opencode v1 outside code mode.
- **Detail.** `description` on a `resource_link` is valid (it extends `Resource`).
- **Sources.** `internal/cli/serve.go:180` "\"type\":        \"resource_link\","; `internal/mcpserver/toolresult.go:21`;
  `internal/mcpserver/server.go:676-681`; `v1:packages/opencode/src/session/tools.ts:429`;
  `v2:packages/core/src/mcp/client.ts:327`

## TOOL-30 `tools/call` may answer with `InputRequiredResult`

- **What.** In 2026, `CallToolResultResponse.result` is `CallToolResult | InputRequiredResult`, and
  `CallToolRequestParams` extends `InputResponseRequestParams` (`inputResponses`, `requestState`) instead of
  `TaskAugmentedRequestParams`, so the 2025-11-25 `task` field is gone.
- **Where.** 2026 only.
- **mcpx @ 05c78b2.** As a client it resolves `input_required` and retries (`internal/mcpclient/client.go:654-677`); as a
  server it emits it for `tools/call` (`internal/mcpserver/server.go:649-657`). Its gaps (session dependence, text-only
  final render, no method binding in `requestState`) are in the MRTR register.
- **Value to mcpx.** + implemented; it is how a 2026 upstream asks a question mid-call.
- **Effort.** Done.
- **Risk.** See the MRTR register.
- **Detail.** mcpx still accepts a `task` param on `tools/call` from any era (tasks register).
- **Sources.** `schema/2026-07-28/schema.ts:1849` "result: CallToolResult | InputRequiredResult;";
  `schema/2026-07-28/schema.ts:1863` "export interface CallToolRequestParams extends InputResponseRequestParams {";
  `internal/mcpclient/client.go:654-677`; `internal/mcpserver/server.go:649-657`

## TOOL-31 per-call permission prompts in the host

- **What.** Hosts gate each MCP tool call on a permission: opencode v1 asks with `permission: "<server>_<tool>"` and
  patterns `["*"]`; v2 calls `permission.assert({action: "<server>_<tool>", resources: ["*"]})`.
- **Where.** opencode v1 and v2. v1 code-mode child calls ask per child tool too.
- **mcpx @ 05c78b2.** n/a — host-side. Every upstream call made inside one `mcpx_exec` is a single permission from the
  host's point of view.
- **Value to mcpx.** + users can allow `mcpx_*` wholesale; − one approval then covers everything a script does.
- **Effort.** S (documentation).
- **Risk.** Low.
- **Detail.** The permission key is the doubled name from TOOL-03 when the server entry is called `mcpx`.
- **Sources.** `v1:packages/opencode/src/session/tools.ts:408`; `v2:packages/core/src/tool/mcp.ts:51`;
  `v1:packages/opencode/src/tool/code-mode.ts:147`
