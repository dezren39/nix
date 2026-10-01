# Completion

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  completion/complete per revision (capability, references, context, limits), what mcpx answers and forwards, and what opencode sends
```

`completion/complete` suggests values for a prompt argument or a resource-template variable as a user types. The
method is in every revision, while the `completions` capability (2025-03-26; CAP-13 in the capabilities register),
`context.arguments` (2025-06-18) and a schema-enforced 100-value limit (2026) came later. What matters most at
`05c78b2`: mcpx's MCP server ignores the reference and answers with its own tool names although the daemon can already
forward the request to the upstream that owns the prompt, and neither opencode version ever sends the request.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| CMP-01 | method exists in 2024-11-05 with no gating capability | `2024-11-05 has` `mcpx missing` | `24-11 ✓ (ungated) · 25-03 ✓ (gated) · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | partial — served in every era; a 2024-11-05 upstream is never asked (#213) | + low | S | low |
| CMP-02 | calling without the capability → `-32601` | `2025-03-26 has` | `25-03 ✓ onward (prose)` | ✓ — client does not call undeclared upstreams | + low | S | low |
| CMP-03 | `ref/prompt` and `ref/resource`; `ResourceReference` renamed, wire unchanged | `2024-11-05 has` `2025-06-18 has` | `all five (TS rename 25-06; 26-07 prose: uri may be a template)` | ✓ — wire unchanged; `/v1/complete` accepts both | + low | S | low |
| CMP-04 | `PromptReference.title` | `2025-06-18 has` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — tolerated; dropped when forwarded | + low | S | low |
| CMP-05 | `context.arguments`: previously resolved argument values | `2025-06-18 has` `mcpx missing` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✗ — ignored by the MCP server, not forwarded by `/v1` (#213) | + med | S | med |
| CMP-06 | at most 100 values; schema-enforced from 2026 | `2024-11-05 has` `2026-07-28 has` | `24-11..25-11 prose · 26-07 @maxItems 100` | ✓ — own replies capped at 100 by default | + low | S | low |
| CMP-07 | `total` and `hasMore` (no cursor) | `2024-11-05 has` | `all five ✓` | ✓ — computed correctly for what mcpx returns | + low | S | low |
| CMP-08 | completion is neither MRTR-capable nor cacheable | `2026-07-28 has` | `26-07 ✓` | ✓ — n/a in practice | + low | S | low |
| CMP-09 | mcpx ignores `ref` and completes from its own tool names | `2024-11-05 has` `mcpx missing` | `mcpx only` | ✗ — prompt argument got tool names (wire W24) (#213) | + med | S | med |
| CMP-10 | MCP surface never forwards upstream, though the daemon can | `mcpx missing` | `mcpx only` | ✗ — `Pool.Complete` unused by `/mcp` (#213) | + med | S | low |
| CMP-11 | neither opencode version sends `completion/complete` | `mcpx has, others don't` | `opencode v1 — · v2 —` | n/a — mcpx's completion serves other hosts | + low | S | low |

## CMP-01 `completion/complete` without a capability in 2024-11-05

- **What.** The method, its `ref`, `argument {name, value}` and result `completion {values, total?, hasMore?}` are all in
  2024-11-05; no capability gates them. 2025-03-26 adds `completions`, which servers supporting the method MUST declare.
- **Where.** Method in all five; capability from 2025-03-26.
- **mcpx @ 05c78b2.** As a server it answers in every era (`internal/mcpserver/server.go:558-563`) and declares
  `completions` in every era it serves; it does not serve 2024-11-05 (`internal/mcpserver/server.go:778`). As a client it
  calls `completion/complete` only when the upstream declared `completions` (`internal/mcpclient/request.go:50`), so a
  2024-11-05 upstream — which cannot declare it — is never asked, even if it implements the method.
- **Value to mcpx.** + low: accept liberally by trying the method on a 2024-11-05 upstream and treating `-32601` as
  "unsupported".
- **Effort.** S.
- **Risk.** A 2024-11-05 server that implements completion looks like one that does not.
- **Detail.** The gate's comment explains why it exists: a well-behaved server answers method-not-found and "the rest"
  answer unpredictably. For 2024-11-05 that reasoning inverts, because declaring was impossible.
- **Sources.** `schema/2024-11-05/schema.ts:954` "export interface CompleteRequest extends Request {";
  `2025-03-26/changelog.mdx:26` "Added `completions` capability to explicitly indicate support for argument";
  `internal/mcpclient/request.go:50` "if !c.Supports("; `internal/mcpserver/server.go:558-563`

## CMP-02 capability-less call → `-32601`

- **What.** From 2025-03-26 the prose error list includes "Method not found: `-32601` (Capability not supported)".
- **Where.** 2025-03-26 onward.
- **mcpx @ 05c78b2.** As a server mcpx always declares and always answers, so it never needs this code. As a client it
  refuses to call an upstream that did not declare `completions` and reports `ok=false` so the caller can fall back
  (`internal/mcpclient/request.go:50`).
- **Value to mcpx.** + low; nothing to change.
- **Effort.** None.
- **Risk.** None.
- **Detail.** mcpx deliberately answers rather than returning `-32601`, because "a client offering completion and
  receiving method-not-found simply shows nothing, and the user concludes the feature is broken"
  (`internal/mcpserver/server.go:559-562`). The cost of that choice is CMP-09.
- **Sources.** `2025-03-26/server/utilities/completion.mdx:130` "- Method not found: `-32601` (Capability not
  supported)"; `internal/mcpclient/request.go:50`; `internal/mcpserver/server.go:559-562`

## CMP-03 reference types

- **What.** `ref` is `{type: "ref/prompt", name}` or `{type: "ref/resource", uri}`. The TypeScript name of the resource
  variant changes from `ResourceReference` to `ResourceTemplateReference` in 2025-06-18 — `type` and `uri` unchanged.
  2026 prose says the `uri` may be a URI template.
- **Where.** All five; the rename is in no changelog.
- **mcpx @ 05c78b2.** The MCP server ignores `ref` entirely (CMP-09). `/v1/complete` accepts both types and rejects any
  other with 400 (`internal/daemon/ops.go:339-343`).
- **Value to mcpx.** + none on the wire.
- **Effort.** None.
- **Risk.** None.
- **Detail.** The rename signals that `ref/resource` names a template, which is what `uri` held in practice.
- **Sources.** `schema/2025-03-26/schema.ts:1138` "export interface ResourceReference {";
  `schema/2025-06-18/schema.ts:1369` "export interface ResourceTemplateReference {";
  `2026-07-28/server/utilities/completion.mdx:140` "References a resource URI or URI template";
  `internal/daemon/ops.go:339-343`

## CMP-04 `PromptReference.title`

- **What.** `PromptReference extends BaseMetadata`, so a reference may carry `title` beside `name`.
- **Where.** 2025-06-18 onward.
- **mcpx @ 05c78b2.** Tolerated. `/v1/complete` re-marshals `ref` from `type`, `name` and `uri` only
  (`internal/daemon/ops.go:314-318`, `internal/daemon/ops.go:355`), so a `title` is dropped before an upstream sees it.
- **Value to mcpx.** + none.
- **Effort.** None.
- **Risk.** None.
- **Detail.** A display title on a request reference carries no meaning for the server; ignoring it is right.
- **Sources.** `schema/2025-06-18/schema.ts:1384` "export interface PromptReference extends BaseMetadata {";
  `internal/daemon/ops.go:355`

## CMP-05 `context.arguments`

- **What.** An optional `context: { arguments?: {[name]: string} }` on the request carries the values already chosen for
  other arguments, so a multi-argument prompt or template can complete dependent values (the branch after the repo).
- **Where.** 2025-06-18 onward. Values are strings only.
- **mcpx @ 05c78b2.** The MCP server parses only `argument` (`internal/mcpserver/server.go:1556-1562`). `/v1/complete`
  builds the upstream request from `ref` and `argument` alone (`internal/daemon/ops.go:355`); its own `context` field is
  mcpx's call context (session, cwd), a different thing that happens to share the name
  (`internal/daemon/ops.go:323`).
- **Value to mcpx.** + correct dependent completion once requests are forwarded (CMP-10).
- **Effort.** S — accept `context.arguments` and pass it through.
- **Risk.** If not done: upstreams suggest values that ignore earlier choices. The name clash on `/v1` invites a caller to
  put `arguments` in the wrong `context`.
- **Detail.** None beyond the above.
- **Sources.** `schema/2025-06-18/schema.ts:1333` "context?: {"; `2025-06-18/server/utilities/completion.mdx:79` "clients
  should include previous completions in the `context.arguments` object"; `internal/daemon/ops.go:355`
  "params, _ := json.Marshal(map[string]any{"; `internal/daemon/ops.go:323` "Context config.CallContext `"

## CMP-06 the 100-value limit

- **What.** A reply may carry at most 100 values. Through 2025-11-25 that is prose and a doc comment; 2026 adds a
  machine-checkable `@maxItems 100`.
- **Where.** Limit in all five; schema constraint in 2026.
- **mcpx @ 05c78b2.** Its own replies are cut to `completion.maxValues` (default 100;
  `internal/mcpserver/server.go:1583-1586`). The setting's help says "higher is not" legal, but nothing clamps it
  (`internal/settings/wire.go:179-183`). `/v1/complete` passes an upstream's `completion` through without applying the
  cap (`internal/daemon/ops.go:362-374`).
- **Value to mcpx.** + truncate upstream replies before they reach a 2026 host, once CMP-10 forwards them.
- **Effort.** S.
- **Risk.** A value above 100, from the setting or a sloppy upstream, fails 2026 schema validation.
- **Detail.** The MCP server and `/v1` read the same setting; a comment records that the MCP side used to hard-code 100
  (`internal/mcpserver/server.go:1579-1582`).
- **Sources.** `2024-11-05/server/utilities/completion.mdx:78` "- Maximum 100 items per response";
  `schema/2026-07-28/schema.ts:2649` "@maxItems 100"; `internal/settings/wire.go:179-183`;
  `internal/mcpserver/server.go:1583-1586`; `internal/daemon/ops.go:362-374`

## CMP-07 `total` and `hasMore`

- **What.** `total` may exceed the number of values sent; `hasMore` says more exist even when the total is unknown.
  There is no cursor: the client narrows by typing more.
- **Where.** All five, unchanged.
- **mcpx @ 05c78b2.** Both the MCP server and the `/v1` cache fallback set `total` to the full match count and `hasMore`
  to whether values were cut (`internal/mcpserver/server.go:1587-1598`, `internal/daemon/ops.go:417-421`).
- **Value to mcpx.** + low.
- **Effort.** None.
- **Risk.** None.
- **Detail.** Correct for what is computed; what is computed is the problem (CMP-09).
- **Sources.** `schema/2024-11-05/schema.ts:986` "total?: number;"; `schema/2024-11-05/schema.ts:990` "hasMore?:
  boolean;"; `internal/mcpserver/server.go:1587-1598`

## CMP-08 neither MRTR-capable nor cacheable

- **What.** In 2026 `CompleteRequestParams` extends plain `RequestParams` (not `InputResponseRequestParams`) and
  `CompleteResult` extends `Result` (not `CacheableResult`).
- **Where.** 2026.
- **mcpx @ 05c78b2.** Nothing to do: mcpx never answers completion with `input_required` and adds no cache hints.
- **Value to mcpx.** + no work.
- **Effort.** None.
- **Risk.** None.
- **Detail.** An upstream that would need user input to complete has no protocol path; it can only return nothing.
- **Sources.** `schema/2026-07-28/schema.ts:2593` "export interface CompleteRequestParams extends RequestParams {";
  `schema/2026-07-28/schema.ts:2644` "export interface CompleteResult extends Result {"

## CMP-09 mcpx ignores `ref` and completes tool names

- **What.** Whatever the reference — a prompt argument, a resource-template variable — mcpx's MCP server answers with its
  own tool and namespace names that contain the typed text.
- **Where.** mcpx only. `docs/protocol.md:86` documents it: "from mcpx's own names".
- **mcpx @ 05c78b2.** `complete` parses only `argument` and matches against `s.Tools()`
  (`internal/mcpserver/server.go:1555-1599`). Wire W24: completing prompt `fake_summarise`, argument `style`, value `b`,
  returned `["mcpx_ask_abandon","mcpx_ask_begin","mcpx_globals"]`.
- **Value to mcpx.** + real suggestions; − today's output is noise for every real use.
- **Effort.** S — forward to the owning upstream when `ref` names one (CMP-10).
- **Risk.** Hosts show nonsense suggestions, and users conclude completion is broken — the outcome the design meant to
  avoid.
- **Detail.** The `completions` declaration is honest in that mcpx answers; it is not honest about what the answers are.
- **Sources.** `internal/mcpserver/server.go:1556-1562`; `internal/mcpserver/server.go:1555-1599`; `docs/protocol.md:86`

## CMP-10 the MCP surface never forwards upstream

- **What.** `/v1/complete` forwards `completion/complete` to the server that owns the prompt or template through
  `Pool.Complete` when that server declared `completions`, and falls back to cached names otherwise, saying which in a
  `source` field. The MCP `completion/complete` branch never uses it.
- **Where.** mcpx internal parity (CLI = /v1 = MCP).
- **mcpx @ 05c78b2.** `internal/pool/request.go:60-70`; `internal/daemon/ops.go:357`; the MCP branch
  (`internal/mcpserver/server.go:558-563`) answers locally.
- **Value to mcpx.** + real argument values from upstream prompts and templates, through the protocol hosts already speak.
- **Effort.** S — split the namespaced prompt name back into `<ns>` and `<name>`, as `GetPrompt` already does
  (`internal/cli/serve.go:593-594`), and strip `mcpx://<ns>/` from template URIs.
- **Risk.** None.
- **Detail.** The `/v1` cache fallback has its own bug: for `ref/resource` it matches `t.URI`
  (`internal/daemon/ops.go:405-409`), but cached templates carry their template in `URITemplate` and leave `URI` empty
  (`internal/pool/pool.go:562-571`), so a non-empty prefix matches nothing and an empty one returns empty strings. For
  `ref/prompt` the fallback suggests prompt names, not argument values.
- **Sources.** `internal/pool/request.go:60` "func (p *Pool) Complete(ctx context.Context, sessionKey string, params
  json.RawMessage) (json.RawMessage, bool, error) {"; `internal/daemon/ops.go:357`; `internal/daemon/ops.go:405-409`;
  `internal/pool/pool.go:562-571`

## CMP-11 opencode never sends `completion/complete`

- **What.** Neither opencode client calls `completion/complete` (nor `resources/subscribe` or `logging/setLevel`).
- **Where.** opencode v1 and v2 lack it; their MCP client interfaces have no completion method.
- **mcpx @ 05c78b2.** n/a — mcpx's completion (CMP-09, CMP-10) serves other hosts.
- **Value to mcpx.** + low: fixing CMP-09 improves mcpx for other hosts, not opencode.
- **Effort.** None.
- **Risk.** None.
- **Detail.** v2 on a modern connection can still open `subscriptions/listen` through its SDK for list changes.
- **Sources.** `v1:packages/opencode/src/mcp/index.ts:164`; `v2:packages/core/src/mcp/client.ts:91`
