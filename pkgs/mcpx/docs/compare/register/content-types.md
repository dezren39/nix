# Content types

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  Every MCP content type and result-content field per revision, and what mcpx, opencode, lootbox and Cloudflare do with each.
```

A tool result, a prompt message and a sampling message carry typed content blocks: `text`, `image`, `audio`, embedded
`resource`, `resource_link`, and, inside sampling only, `tool_use` and `tool_result`; each may carry `annotations` and
`_meta`, and a tool result may add `structuredContent`. mcpx has a per-revision `downgrade()` that rewrites these for
older clients, but in production only two of its five branches can fire; what decides what a host actually sees is that
`mcpx_call` renders every upstream result to one text block (dropping images, audio, resources, `_meta` and `isError`),
so the only non-text block mcpx ever sends is the `resource_link` for an `mcpx_exec` artifact.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| CT-01 | `text` content block | `2024-11-05 has` `opencode v1 has` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ (+_meta) · 25-11 ✓ · 26-07 ✓` | ✓ — the only block `mcpx_call` emits | + high | S | low |
| CT-02 | `image` content block | `2024-11-05 has` `opencode v1 has` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ · opencode v1 attachment · v2 media` | partial — `mcpx_call` drops it; scripts keep it | + med | M | med |
| CT-03 | `audio` content block | `2025-03-26 has` `opencode v2 has` | `24-11 — · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ · opencode v1 dropped · v2 media` | partial — dropped by `mcpx_call`; downgrade branch dead | + low | S | low |
| CT-04 | Embedded `resource` block (tool results and prompts only) | `2024-11-05 has` `opencode v1 has` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ · never in sampling` | partial — dropped by `mcpx_call`; downgrade target | + med | S | low |
| CT-05 | `resource_link` block | `2025-06-18 has` `opencode v2 has` | `25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ · opencode v1 direct dropped · v2 uri text` | ✓ — emitted for `mcpx_exec` artifacts only | + high | S | med |
| CT-06 | `resource_link` rewritten as embedded resource below 2025-06-18 | `2025-06-18 has` `mcpx missing` | mcpx only (downgrade for 2025-03-26 clients) | partial — broken mime: link type over text label (#206) | + low | S | med |
| CT-07 | `ContentBlock` union and where each type is allowed | `2025-06-18 has` `specs conflict` | `24-11 inline unions · 25-06 ContentBlock · prompts prose lags to 26-07` | ✓ — gated per revision in `downgrade()` | + low | S | low |
| CT-08 | `tool_use` / `tool_result` blocks, sampling only | `2025-11-25 has` `2026-07-28 deprecates` | `25-11 ✓ · 26-07 dep` | n/a — not examined | − niche | M | low |
| CT-09 | `annotations` on content (`audience`, `priority`, `lastModified`) | `2024-11-05 has` `2025-06-18 has` | `24-11 Annotated · 25-03 Annotations · 25-06 +lastModified · 26-07 ✓` | partial — passed by `downgrade()`, dropped by `mcpx_call` | + med | S | low |
| CT-10 | `_meta` on content blocks | `2025-06-18 has` | `25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | partial — dropped by `mcpx_call`, link rewrite, MRTR render | + low | S | low |
| CT-11 | `ToolResultContent.structuredContent` becomes any JSON | `2026-07-28 has` | `25-11 object · 26-07 any JSON (sampling)` | n/a — not examined | + low | S | low |
| CT-12 | What `downgrade()` touches, and which branches can fire | `mcpx has, others don't` | mcpx only | partial — only link and `resultType` branches live | + med | S | med |
| CT-13 | `mcpx_call` renders every result to one text block | `2025-06-18 has` `mcpx missing` | mcpx `/mcp` and stdio `mcpx serve` | partial — images, audio, resources, `_meta` dropped (#206) | + med | M | high |
| CT-14 | Unknown content block type fails the whole call | `opencode v1 has` `opencode v2 has` | `opencode v1 verified · v2 same union` | ✓ — never forwards an unknown block | + med | S | high |
| CT-15 | Script clients unwrap tool results | `has better replacement` `opencode v1 has` `opencode v2 has` | `mcpx · opencode v1/v2 · Cloudflare unwrap · lootbox raw envelope` | ✓ — `structuredContent`, then text, then blocks | + high | S | low |
| CT-16 | JSON arriving as text parsed into a value | `opencode v2 has` | `mcpx always by shape · opencode v2 without outputSchema · v1 never` | ✓ — parses `{…}`/`[…]` text | + low | S | low |
| CT-17 | Full envelope kept on `.raw` | `mcpx has, others don't` | mcpx only | ✓ — non-enumerable `.raw` on every value | + med | S | low |
| CT-18 | Media produced by a tool inside a script | `opencode v1 has` `opencode v2 has` | `opencode attaches to model · mcpx artifact link · lootbox base64 · CF mixed` | partial — plugin never declares `artifacts` | + med | S | med |
| CT-19 | `isError: true` becomes an exception in scripts | `has better replacement` `opencode v1 has` `opencode v2 has` | `mcpx · opencode v1/v2 · Cloudflare throw · lootbox returns it` | ✓ — `ToolError` with `.raw` | + high | S | low |

## CT-01 `text` content block

- **What.** `{type: "text", text, annotations?, _meta?}`.
- **Where.** All five revisions; allowed in tool results, prompt messages and sampling messages. `_meta` from 2025-06-18.
  opencode v1 and v2 pass it to the model as text.
- **mcpx @ 05c78b2.** The one block `mcpx_call` ever emits (CT-13); also what `downgrade()` renders `structuredContent`
  and audio into for older clients (`internal/mcpserver/revisions.go:146-156`, `:224-226`).
- **Value to mcpx.** + high: the baseline every host reads.
- **Effort.** S (nothing to do).
- **Risk.** None.
- **Detail.** Everything mcpx cannot express for a client ends up here, so text is also where information is lost
  silently: a text block has no mime type and no URI.
- **Sources.** `schema/2024-11-05/schema.ts:846` "type: \"text\";"; `schema/2026-07-28/schema.ts:2317` "type: \"text\";"; `v2:packages/core/src/mcp/client.ts:324`

## CT-02 `image` content block

- **What.** `{type: "image", data (base64), mimeType, annotations?, _meta?}`.
- **Where.** All five revisions; tool results, prompts and sampling. opencode v1 turns it into a data-URL file
  attachment; v2 into a `media` part. Both deliver it to a vision model.
- **mcpx @ 05c78b2.** `mcpx_call` drops it (`renderResult` keeps only `text`, `internal/cli/commands.go:385-415`); if
  a result has no text block at all, the entire raw result JSON, base64 included, becomes the text
  (`internal/cli/commands.go:414`). Scripts keep images reachable through the value and `.raw` (CT-15, CT-17).
  The MRTR result render also drops images (see the MRTR area).
- **Value to mcpx.** + med: a screenshot tool behind `mcpx_call` either loses the picture or floods the context with
  base64. − text keeps context small, which is mcpx's goal; `mcpx_exec` artifacts (CT-05) are the designed answer.
- **Effort.** M — pass image blocks through, or turn them into artifacts the way `mcpx_exec` does.
- **Risk.** Med: megabytes of base64 in a model's context when the upstream returns only an image.
- **Detail.** The raw-JSON fallback is the worst case: an image-only result produces no text, so `renderResult` returns
  `string(raw)`.
- **Sources.** `schema/2024-11-05/schema.ts:857` "type: \"image\";"; `internal/cli/commands.go:414` "return string(raw)"; `v1:packages/opencode/src/session/tools.ts:430`; `v2:packages/core/src/mcp/client.ts:325-326`

## CT-03 `audio` content block

- **What.** `{type: "audio", data (base64), mimeType}`.
- **Where.** 2025-03-26 onward; tool results, prompts and sampling. opencode v2 treats it as `media`; opencode v1's
  direct path drops it silently (its converter handles only text, image and resource), while v1 code mode attaches it
  as a file.
- **mcpx @ 05c78b2.** `downgrade()` has an audio→text branch ("(audio mime, base64, elided)"), but `FeatAudio`'s floor
  equals `Oldest` (2025-03-26), and every version mcpx agrees to is at or above it, so the branch cannot fire; a
  2024-11-05 client is refused at `initialize` before it could need it. mcpx emits no audio anyway, and `mcpx_call`
  drops upstream audio.
- **Value to mcpx.** − dead code today. + becomes live if mcpx ever serves 2024-11-05.
- **Effort.** S.
- **Risk.** Serving 2024-11-05 without extending the floors table would send 2025-03-26 shapes (audio, tool
  `annotations`, the `completions` capability) to 2024-11-05 clients.
- **Detail.** The comment at `internal/mcpserver/revisions.go:45-48` says the branch is kept deliberately ("always true
  today" is a fact about the floor). `describeBlob` appends "base64, elided" only when `data` was non-empty.
- **Sources.** `schema/2025-03-26/schema.ts:991` "type: \"audio\";"; `2025-03-26/changelog.mdx:25` "Added support for audio data, joining the existing text and image content types"; `internal/mcpserver/revisions.go:87` "FeatAudio:               \"2025-03-26\","; `internal/mcpserver/revisions.go:14`; `v1:packages/opencode/src/session/tools.ts:429`

## CT-04 Embedded `resource` block (tool results and prompts only)

- **What.** `{type: "resource", resource: TextResourceContents | BlobResourceContents, annotations?, _meta?}`: the
  resource's contents inline.
- **Where.** All five revisions in tool results and prompt messages. Never a top-level sampling block in any revision
  (from 2025-11-25 only nested inside `tool_result.content`). opencode v1 turns text into text and a blob into an
  attachment only for PDF, GIF, JPEG, PNG and WebP up to 10 MiB, else a "[Binary MCP resource omitted …]" line; v2
  turns text into text and a blob into `media`, or into its URI when the blob has no `mimeType`.
- **mcpx @ 05c78b2.** The target of the `resource_link` downgrade (CT-06). `mcpx_call` drops upstream embedded
  resources.
- **Value to mcpx.** + med: the only way before 2025-06-18 to hand a client something with a URI.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** In 2024-11-05 the sampling message union is `TextContent | ImageContent` only, so a resource can never be
  sampled directly.
- **Sources.** `schema/2024-11-05/schema.ts:617` "type: \"resource\";"; `schema/2024-11-05/schema.ts:812` "content: TextContent | ImageContent;"; `v1:packages/opencode/src/session/tools.ts:32` "const MAX_MCP_RESOURCE_BLOB_BYTES = 10 * 1024 * 1024"; `v1:packages/opencode/src/session/tools.ts:436`; `v2:packages/core/src/mcp/client.ts:328`

## CT-05 `resource_link` block

- **What.** `ResourceLink extends Resource` plus `type: "resource_link"`: a URI reference with name, mime type and
  description instead of inline contents. A link returned by a tool is not guaranteed to appear in `resources/list`.
- **Where.** 2025-06-18 onward in tool results and prompts (and inside sampling `tool_result.content` from 2025-11-25).
  opencode v2 renders it as its bare `uri` in a text part (the name is lost). opencode v1 drops it silently on the
  direct path and renders `"<name>: <uri>"` in code mode.
- **mcpx @ 05c78b2.** The only non-text block mcpx emits: `mcpx_exec` artifacts become `resource_link` blocks
  (`uri` `mcpx://artifacts/{id}`, `name`, `mimeType`, `description` "N bytes, produced by this script",
  `internal/cli/serve.go:174-187`). They travel through the backend's string result behind a U+001E marker and are
  re-encoded in `tools/call` (`internal/mcpserver/toolresult.go:21`, `:34-56`; `internal/mcpserver/server.go:676-681`).
- **Value to mcpx.** + high: keeps binaries out of context, the reason `mcpx_exec` exists.
- **Effort.** S (done).
- **Risk.** Med: opencode v1 without code mode never shows the model an `mcpx_exec` artifact; the text part of the
  result should mention the URI for such hosts.
- **Detail.** `description` on a link is valid because `ResourceLink` extends `Resource`. The prompts prose documents
  links only from 2026-07-28 although the 2025-06-18 schema allows them (CT-07).
- **Sources.** `schema/2025-06-18/schema.ts:761` "type: \"resource_link\";"; `schema/2026-07-28/schema.ts:1712` "Note: resource links returned by tools are not guaranteed to appear in the results of"; `internal/cli/serve.go:180` "\"type\":        \"resource_link\","; `v2:packages/core/src/mcp/client.ts:327` "if (part.type === \"resource_link\") return [{ type: \"text\", text: part.uri }]"; `v1:packages/opencode/src/tool/code-mode.ts:104`

## CT-06 `resource_link` rewritten as embedded resource below 2025-06-18

- **What.** For a 2025-03-26 client, `linkAsResource` turns a link into
  `{type: "resource", resource: {uri, mimeType: <link's mimeType>, text: "name — uri"}}`, in tool results and inside a
  prompt message's single block.
- **Where.** mcpx only.
- **mcpx @ 05c78b2.** `internal/mcpserver/revisions.go:220-223` (dispatch), `:248-253` (prompt messages), `:260-273`
  (the rewrite). For a PNG artifact the client receives `mimeType: "image/png"` over a text body that is a label.
- **Value to mcpx.** + low: `text/plain` for the label, with the original type in the text or `_meta`, would be honest.
- **Effort.** S.
- **Risk.** Med: a 2025-03-26 host that trusts `mimeType` tries to render a label as an image.
- **Detail.** `text/plain` is used only when the link has no `mimeType` (`internal/mcpserver/revisions.go:264-266`), so
  the label is typed honestly only for untyped links; every typed artifact is mislabelled. The rewrite keeps `uri`
  machine-readable (its purpose) but drops the link's `description`, `title`, `size`, `annotations` and `_meta`. It
  is one of the two `downgrade()` branches that can fire in production (CT-12).
- **Sources.** `internal/mcpserver/revisions.go:271-272` "\"uri\": uri, \"mimeType\": mime, \"text\": text}}"; `schema/2025-03-26/schema.ts:654`

## CT-07 `ContentBlock` union and where each type is allowed

- **What.** 2025-06-18 names the union `TextContent | ImageContent | AudioContent | ResourceLink | EmbeddedResource` and
  uses it for `CallToolResult.content[]` and `PromptMessage.content`; earlier revisions spell inline unions.
- **Where.** Allowed blocks per location (T text, I image, A audio, R embedded resource, L link, U `tool_use`,
  X `tool_result`):
  `CallToolResult.content[]` — 24-11 T I R; 25-03 T I A R; 25-06 onward T I A R L.
  `PromptMessage.content` (one block, never an array) — same progression.
  `SamplingMessage.content` — 24-11 T I; 25-03/25-06 T I A; 25-11 T I A U X, single or array; 26-07 same, deprecated.
  `ToolResultContent.content[]` (nested in sampling) — 25-11 onward T I A R L.
  `ReadResourceResult.contents[]` is `Text|BlobResourceContents`, never content blocks.
- **mcpx @ 05c78b2.** `downgrade()` gates `resource_link` and `structuredContent` by revision
  (`internal/mcpserver/revisions.go:146`); audio is gated but never fires (CT-03).
- **Value to mcpx.** + low: the table is what `downgrade()` must encode.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** Prose and schema disagree for prompts: the 2025-06-18 schema allows `resource_link` in prompt messages,
  but the 2025-06-18 and 2025-11-25 prompts pages document only text, image, audio and embedded resource; a "Resource
  Links" section first appears in 2026-07-28, and the 2025-06-18 changelog advertises links only "in tool call results"
  (prompts area).
- **Sources.** `schema/2025-06-18/schema.ts:1133` "export type ContentBlock ="; `schema/2024-11-05/schema.ts:656` "content: (TextContent | ImageContent | EmbeddedResource)[];"; `schema/2025-03-26/schema.ts:699` "content: (TextContent | ImageContent | AudioContent | EmbeddedResource)[];"; `schema/2025-06-18/schema.ts:750` "content: ContentBlock;"; `schema/2025-11-25/schema.ts:1683` "content: SamplingMessageContentBlock | SamplingMessageContentBlock[];"; `schema/2025-11-25/schema.ts:1884` "content: ContentBlock[];"; `2026-07-28/server/prompts.mdx:274` "#### Resource Links"

## CT-08 `tool_use` / `tool_result` blocks, sampling only

- **What.** `ToolUseContent {id, name, input, _meta?}` and
  `ToolResultContent {toolUseId, content: ContentBlock[], structuredContent?, isError?, _meta?}`, allowed only in
  `SamplingMessageContentBlock`.
- **Where.** 2025-11-25; 2026-07-28 keeps them but deprecates sampling.
- **mcpx @ 05c78b2.** Not examined (the sampling area covers what mcpx relays).
- **Value to mcpx.** − matters only for a sampling relay.
- **Effort.** M.
- **Risk.** Low.
- **Detail.** Neither carries `annotations`; clients SHOULD preserve their `_meta` (it is used for caching).
- **Sources.** `schema/2025-11-25/schema.ts:1693` "export type SamplingMessageContentBlock ="; `schema/2025-11-25/schema.ts:1835` "type: \"tool_use\";"; `schema/2025-11-25/schema.ts:1869` "type: \"tool_result\";"

## CT-09 `annotations` on content (`audience`, `priority`, `lastModified`)

- **What.** `annotations: {audience?, priority? (0..1), lastModified? (ISO 8601)}` on text, image, audio, embedded
  resource and link blocks.
- **Where.** 2024-11-05 through a mixin (`extends Annotated`); 2025-03-26 renames it to an explicit
  `annotations?: Annotations` field (same wire shape, not in the changelog); 2025-06-18 adds `lastModified`
  (changelog-silent). Never on `tool_use`/`tool_result`.
- **mcpx @ 05c78b2.** `downgrade()` never touches annotations, so they pass through for any revision; `mcpx_call` drops
  them with everything else that is not text (CT-13); the link rewrite drops the link's annotations (CT-06).
- **Value to mcpx.** + med: `audience: ["user"]` is the spec's own way to keep a block out of model context, which is
  mcpx's goal.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** `lastModified` would be sent unchanged to a 2025-03-26 client, which does not define it; harmless to a
  client that ignores unknown fields, and one more field `downgrade()` does not police.
- **Sources.** `schema/2024-11-05/schema.ts:818` "export interface Annotated {"; `schema/2025-03-26/schema.ts:924` "export interface Annotations {"; `schema/2025-06-18/schema.ts:1127` "lastModified?: string;"

## CT-10 `_meta` on content blocks

- **What.** `TextContent`, `ImageContent`, `AudioContent`, `EmbeddedResource` and `ResourceLink` (through `Resource`)
  gain `_meta`.
- **Where.** 2025-06-18 onward.
- **mcpx @ 05c78b2.** Dropped by `mcpx_call` (CT-13), by the link rewrite (CT-06) and by the MRTR result render (MRTR
  area). `downgrade()` leaves it in place for 2025-03-26 clients, which do not define it.
- **Value to mcpx.** + low: extensions (MCP Apps UI pointers, for example) ride on `_meta`.
- **Effort.** S.
- **Risk.** Low today.
- **Detail.** Result-level `_meta` is recorded in the `_meta` area; this row is the per-block field.
- **Sources.** `schema/2025-06-18/schema.ts:1157` "_meta?: { [key: string]: unknown };"

## CT-11 `ToolResultContent.structuredContent` becomes any JSON

- **What.** The sampling `tool_result` block's `structuredContent` is an object in 2025-11-25 and `unknown` in
  2026-07-28, mirroring TOOL-27.
- **Where.** 2025-11-25 object; 2026-07-28 any JSON. The 2026 changelog mentions only the `CallToolResult` field.
- **mcpx @ 05c78b2.** Not examined.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** A sampling relay forwarding a 2026 host's tool results to a 2025-11-25 server would hit the same
  non-object gap as TOOL-27.
- **Sources.** `schema/2025-11-25/schema.ts:1891` "structuredContent?: { [key: string]: unknown };"; `schema/2026-07-28/schema.ts:2469` "structuredContent?: unknown;"

## CT-12 What `downgrade()` touches, and which branches can fire

- **What.** `downgrade(result, version)` runs on every successful result. At 2026-07-28 it returns the result
  untouched. Otherwise, on a JSON copy: below 2025-06-18 it moves `structuredContent` into a text block (TOOL-27) and
  rewrites `resource_link` (CT-06); below 2025-03-26 it rewrites `audio` (CT-03); below 2026-07-28 it deletes
  `resultType` (LV-36). The same content rewrite applies to each prompt message's single block.
- **Where.** mcpx only; opencode and lootbox never rewrite for a peer.
- **mcpx @ 05c78b2.** `internal/mcpserver/revisions.go:132-170`, `206-232`, `236-258`, `260-289`. mcpx emits no
  `structuredContent` and no audio, so only the link and `resultType` branches can fire.
- **Value to mcpx.** + med: the single edge is sound design; it is also where every other send-conservatively fix
  belongs.
- **Effort.** S.
- **Risk.** Med: it never looks at `text`, `image`, `resource`, `annotations`, `_meta`, icons, titles, or any list result
  (`tools/list`, `prompts/list`, …). When mcpx emits richer results, fields a client's revision lacks will leak; titles
  in `prompts/list` to 2025-03-26 already do (prompts area).
- **Detail.** The fast path's condition is "the version defines structuredContent, resource_link, audio and resultType",
  that is 2026-07-28 only; 2025-06-18 and 2025-11-25 results are still copied, although at most `resultType` changes.
- **Sources.** `internal/mcpserver/revisions.go:133-138`; `internal/mcpserver/server.go:439` "resp.Result = downgrade(resp.Result, peer.Version)"

## CT-13 `mcpx_call` renders every result to one text block

- **What.** The core proxy tool returns one text block. `renderResult` decodes only `content[].type/text`,
  `structuredContent` and `isError`: if `structuredContent` is present it wins, pretty-printed; else text blocks are
  joined with newlines; else the whole raw result JSON is the text. Images, audio, embedded resources, links,
  annotations and upstream `_meta` are dropped.
- **Where.** mcpx's MCP surface (`/mcp` and stdio `mcpx serve`). `/v1/call` and scripts get the raw result.
- **mcpx @ 05c78b2.** `internal/cli/commands.go:385-415`; called from `mcpBackend.Call`
  (`internal/cli/serve.go:103-117`). Wire W21: an upstream `structuredContent {"n": 42}` reached the host as the text
  `"{\n  \"n\": 42\n}"`.
- **Value to mcpx.** + med: images to multimodal hosts (as blocks, or as artifacts as `mcpx_exec` does) and `_meta` for
  extensions. − text-only is cheap, which is mcpx's goal.
- **Effort.** M.
- **Risk.** High: an image-only upstream result becomes megabytes of base64 text (CT-02); a failure becomes a success
  (TOOL-26).
- **Detail.** The function's comment says it unwraps "the same way the script client does, so CLI and script output
  agree"; the script client throws on `isError` and keeps images on `.raw`, so the two do not agree.
  `downgradeMessages`/`downgradeContent` therefore only ever see one text block from `mcpx_call`.
- **Also recorded from the tools register.** `renderResult` (`internal/cli/commands.go:385-415`), falling back to
  `string(raw)` (`internal/cli/commands.go:414`). Wire W21: upstream `structuredContent {n: 42}` arrived as the text
  `"{\n \"n\": 42\n}"` with no `structuredContent`. `renderResult`'s comment says it unwraps "the same way the script
  client does", but the script client throws on `isError` and keeps the untouched envelope on `.raw` (code-mode
  register); `mcpx_call` does neither. A JSON `null` `structuredContent` is rendered as the text `null`.
- **Sources.** `internal/cli/commands.go:383-384` "// renderResult unwraps a CallToolResult the same way the script client does,"; `internal/cli/commands.go:414` "return string(raw)"; `internal/cli/serve.go:116` "return renderResult(res.Result), nil"; wire W21; `internal/cli/commands.go:383` "renderResult unwraps a CallToolResult the same way the script client does,"; `internal/cli/commands.go:385-415`

## CT-14 Unknown content block type fails the whole call

- **What.** Both opencode SDKs define `ContentBlock` as a closed union (`text | image | audio | resource_link |
  resource`). In v1 the result is parsed against `CallToolResultSchema`, so any other `type` rejects the call as a zod
  error, not an MCP error.
- **Where.** opencode v1 (verified); v2's SDK has the same union (its validation path was not traced).
- **mcpx @ 05c78b2.** `downgrade()` gates `resource_link` and `structuredContent` by revision
  (`internal/mcpserver/revisions.go:146`), and `mcpx_call` emits text only, so no unknown block reaches opencode today.
- **Value to mcpx.** + med: mcpx must never pass a new or extension block type through verbatim to a host that did not
  negotiate it.
- **Effort.** S.
- **Risk.** High if broken: one unknown block from an upstream loses the whole result, text included.
- **Detail.** This is the practical reason send-conservatively applies to content: a strict client does not skip what
  it cannot parse. `downgrade()`'s own comment makes the same point ("a content block it cannot parse is not a degraded
  result, it is an unreadable one", `internal/mcpserver/revisions.go:130-131`).
- **Sources.** `v1:packages/opencode/src/mcp/catalog.ts:59` "CallToolResultSchema,"; <https://unpkg.com/@modelcontextprotocol/sdk@1.29.0/dist/esm/types.js> (L1131–L1137, L1296); <https://unpkg.com/@modelcontextprotocol/core@2.0.0/dist/auth-CUe6YdwF.mjs> (L678–L684)

## CT-15 Script clients unwrap tool results

- **What.** What a script gets back from a tool call. mcpx: throws on `isError`; else `structuredContent` if present;
  else all-text results joined (and JSON-parsed if they look like JSON, CT-16); else the block array. opencode v1 code
  mode: `structuredContent` (unless `null`), else joined text (links as `"<name>: <uri>"`), else a placeholder such as
  `[2 images attached to the result]`. opencode v2: `structuredContent`, else text (parsed when there is no
  `outputSchema`). Cloudflare `codeMcpServer`: compatibility `toolResult`, MCP errors, `structuredContent`, all-text,
  then the mixed result. lootbox: the raw `CallToolResult`; the script digs out `content[0].text` itself.
- **Where.** mcpx, opencode v1/v2 and Cloudflare unwrap; lootbox does not.
- **mcpx @ 05c78b2.** `unwrap` in the generated client (`internal/codegen/emit.go:493-519`).
- **Value to mcpx.** + high: this is the good path for agents, and it is done.
- **Effort.** S (done).
- **Risk.** Low.
- **Detail.** mcpx does not handle the pre-2024-11-05 compatibility `toolResult` field that Cloudflare still honours (no
  `toolResult` in `internal`). `mcpx_call` (CT-13) does not share this logic although its comment says it does.
- **Sources.** `internal/codegen/emit.go:501` "if (raw && raw.structuredContent !== undefined) {"; `.lootbox/src/lib/rpc/execute_mcp.ts:68-72`; `.lootbox/src/lib/external-mcps/parse_mcp_schemas.ts:283-293`; `v1:packages/opencode/src/tool/code-mode.ts:109-115`; `v2:packages/core/src/tool/mcp.ts:96-106`; <https://developers.cloudflare.com/agents/tools/codemode/api-reference/#codemcpserver> "Returned MCP values are unwrapped in this order: compatibility `toolResult`, MCP errors, `structuredContent`, all-text content, then the original mixed-content result."

## CT-16 JSON arriving as text parsed into a value

- **What.** mcpx parses all-text results that look like a JSON object or array, even when the tool declares an
  `outputSchema`. opencode v2 parses only when the tool has no `outputSchema` and the text starts with `{` or `[`.
  opencode v1 never parses.
- **Where.** mcpx (always, by shape); opencode v2 (only without a schema); opencode v1 (never); Cloudflare
  (undocumented); lootbox (n/a, raw envelope).
- **mcpx @ 05c78b2.** `internal/codegen/emit.go:505-516`.
- **Value to mcpx.** + low: v2's extra condition avoids returning a parsed object where `structuredContent` was
  expected; mcpx could adopt it once it generates return types from `outputSchema`.
- **Effort.** S.
- **Risk.** Low: prose that happens to be `[…]` becomes an array; `.raw` keeps the original (CT-17).
- **Detail.** mcpx checks the first and last characters (`{…}` or `[…]`); v2 checks only the first and falls back to
  text on a parse error.
- **Sources.** `internal/codegen/emit.go:505-516`; `v2:packages/core/src/tool/mcp.ts:99-104` "Agents assume JSON returned as text is already an object, so parse it when the server declares no schema."; `v1:packages/opencode/src/tool/code-mode.ts:109-110`

## CT-17 Full envelope kept on `.raw`

- **What.** mcpx attaches the untouched `CallToolResult` to every unwrapped value as a non-enumerable `.raw` property;
  strings are boxed so they can carry it. Image bytes, annotations and `_meta` stay reachable there.
- **Where.** mcpx only. opencode projects the envelope away and moves media to attachments; Cloudflare returns the mixed
  result only when content is not all text; lootbox returns nothing but the envelope.
- **mcpx @ 05c78b2.** `attachRaw` (`internal/codegen/emit.go:477-491`).
- **Value to mcpx.** + med: ergonomic values without losing the protocol detail.
- **Effort.** S (done).
- **Risk.** Low: `typeof` a boxed string is `"object"`, which surprises a model that tests `typeof r === "string"`; a
  frozen object cannot take the property and the error is swallowed (`internal/codegen/emit.go:481`).
- **Detail.** `JSON.stringify` of a boxed string gives the string, so logging is unaffected.
- **Sources.** `internal/codegen/emit.go:486-488`

## CT-18 Media produced by a tool inside a script

- **What.** When a call inside an opencode script returns image, audio or blob content, opencode attaches it to the
  `execute` result as data-URL files, so the model sees the image. mcpx leaves the bytes to the script and, when the
  caller declares the `artifacts` capability, replaces media at the output boundary with a `resource_link` to a stored
  artifact; `artifact()` stores one explicitly.
- **Where.** opencode v1/v2 (attach); mcpx (store and link); lootbox (base64 in the envelope, seen only if printed);
  Cloudflare (mixed result object, no attachment channel documented).
- **mcpx @ 05c78b2.** `docs/exec.md:259-275`; `interceptMedia` (`internal/execsvc/execsvc.go:543-545`); `artifact()`
  (`internal/codegen/emit.go:1275`).
- **Value to mcpx.** − opencode's approach is the context cost mcpx avoids ("one screenshot costs more than the rest of
  the task"). + where a vision model must see the image, opencode's channel is the only one that renders it; mcpx needs
  a `resources/read`.
- **Effort.** S.
- **Risk.** Med: the plugin's `mcpx_exec` never declares `artifacts`, so its images are not intercepted and must be
  printed as base64 to be seen (inferred from `plugin/opencode/mcpx/daemon.ts:452-455`).
- **Detail.** Artifacts are served by a random 128-bit id, not the content hash, because a hash is guessable
  (`docs/exec.md:200-207`).
- **Sources.** `v1:packages/opencode/src/tool/code-mode.ts:89-100`; `v2:packages/core/src/codemode/tool.ts:139-147`; `docs/exec.md:259-275`

## CT-19 `isError: true` becomes an exception in scripts

- **What.** mcpx, opencode and Cloudflare throw when a tool result carries `isError: true`. lootbox returns it as a
  successful value, so the script must check `.isError` itself and usually does not.
- **Where.** mcpx (`ToolError` with server, tool and `.raw`); opencode v1 (throws, surfaced as a `ToolFailure`
  diagnostic); opencode v2 (`ToolFailure`); Cloudflare ("MCP error results become thrown connector errors"); lootbox
  passes it through.
- **mcpx @ 05c78b2.** `internal/codegen/emit.go:494-499`.
- **Value to mcpx.** + high: done, and the reason scripts do not share TOOL-26's bug.
- **Effort.** S (done).
- **Risk.** Low for mcpx; a lootbox script can report success after every call failed.
- **Detail.** Every thrower builds the message from the text blocks joined with newlines, with a default when there are
  none: mcpx "`<server>.<tool> returned isError`", opencode "MCP tool returned an error".
- **Sources.** `internal/codegen/emit.go:494` "if (raw && raw.isError) {"; `.lootbox/src/lib/rpc/execute_mcp.ts:60-72`; `v2:packages/core/src/tool/mcp.ts:78-85`; <https://developers.cloudflare.com/agents/tools/codemode/api-reference/#mcpconnector> "MCP error results become thrown connector errors."
