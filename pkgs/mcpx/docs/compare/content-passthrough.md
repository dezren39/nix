# Content types through mcpx: did it need them?

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare, area:protocol
description:  what mcpx does with each MCP content type on each path a
              result can take, and whether supporting audio, resource_link,
              structuredContent and the rest was ever necessary.
```

## The short answer

**Mostly no, and the reason is not the one the code assumes.**

`internal/mcpserver/revisions.go` is built for a proxy that passes upstream
content through and spells it down for older clients. It turns
`structuredContent` into text for 2025-03-26, turns `resource_link` into an
embedded resource, and turns `audio` into a placeholder
(`internal/mcpserver/revisions.go:132-170`). That design is sound, and at
`05c78b2` very little of it runs. mcpx does not pass upstream content through
on its main path at all. `mcpx_call`, the tool that reaches one upstream tool,
**renders every result to text** before `downgrade()` ever sees it
(`internal/cli/serve.go:103-117`, `internal/cli/commands.go:384-415`).
The only non-text block mcpx itself emits is `resource_link`, for a script's
artifacts (`internal/cli/serve.go:174-186`). So:

- **`audio`**: never emitted. The audio→text branch can never fire, because
  mcpx's floor is 2025-03-26, the revision that introduced audio
  (`internal/mcpserver/revisions.go:14`, `:87`).
- **`structuredContent`**: never emitted. `mcpx_call` pretty-prints it into a
  text block for every revision, 2026-07-28 included (wire W21).
- **`resource_link`**: emitted for script artifacts; downgraded to an embedded
  `resource` for 2025-03-26 hosts. This is the one live branch.
- **`resultType`**: stamped for 2026-07-28, stripped for everyone else. Live.
- **`image`**, **embedded `resource`**, annotations, `_meta` on content:
  never touched by `downgrade()`, and never passed through by `mcpx_call`.

mcpx did not *need* the audio, `structuredContent` and `resource_link` branches
for what it does today. It will need them when `mcpx_call` stops flattening,
and it should stop, for two reasons unrelated to revisions. It drops
`isError`, so a failed upstream call looks like a success (wire W20). And it
drops images and embedded resources entirely, which a host such as opencode v2
would have rendered (`v2:packages/core/src/mcp/client.ts:325-328`).

---

## Per content type, per path

A result can reach a host by six paths. What each does with each type at
`05c78b2`:

| content | `mcpx_call` (MCP tool; also `mcpx call` on the CLI) | `mcpx_exec` (MCP tool) | a call that stopped to ask a question | `prompts/get` | `resources/read` | generated script client (`tools.ns.tool()`) |
| --- | --- | --- | --- | --- | --- | --- |
| `text` | kept; blocks joined with `\n`, unless `structuredContent` is present, in which case **the text is dropped** and only the structured value is shown | whatever the script prints | kept, joined | kept; non-user roles prefixed `[role]`; all merged into **one user message** | kept; multi-part contents merged into one | joined; parsed as JSON when it looks like JSON |
| `image` | **dropped**; if no text block exists the whole result is dumped as JSON text, base64 and all | stored as an artifact and replaced by a `resource_link` | dropped | dropped | n/a | kept in the returned block array and on `.raw` |
| `audio` | **dropped** (same as image) | stored as an artifact, replaced by a `resource_link` | dropped | dropped | n/a | kept in the block array and on `.raw` |
| embedded `resource` (text) | dropped | passed as the script returns it | dropped | dropped | n/a | kept in the block array |
| embedded `resource` (blob) | dropped | stored as an artifact, replaced by a `resource_link` | dropped | dropped | n/a | kept in the block array |
| `resource_link` | dropped | **emitted**, one per artifact, with size in the description | dropped | dropped | n/a | kept in the block array |
| `structuredContent` | pretty-printed JSON as the only text | n/a | dropped | n/a | n/a | **returned as the value** (preferred over text) |
| upstream result with `isError: true` | **dropped**: the host sees a normal result (W20). An upstream JSON-RPC error or transport failure, by contrast, *is* reported as `isError: true` (`internal/mcpserver/server.go:665-675`) | a script failure becomes the tool's `isError` result | **dropped** the same way; a failed task is reported as `isError: true` (`internal/cli/serve_ask.go:174-181`) | n/a | n/a | thrown as `ToolError`, envelope on `.raw` |
| resource `text` contents | n/a | n/a | n/a | n/a | kept | n/a |
| resource `blob` contents | n/a | n/a | n/a | n/a | an upstream blob is **described**, "(N bytes of mime, base64)"; an mcpx artifact comes back as base64 in `text` with its real mime type | n/a |

Sources, in column order: `internal/cli/commands.go:384-415` (`renderResult`);
`internal/cli/serve.go:125-187` and `internal/execsvc/media.go:27-126`;
`internal/mcpserver/ask.go:268-296` (`askResult`);
`internal/cli/serve.go:638-668` (`renderPrompt`) and
`internal/mcpserver/server.go:755-764`; `internal/cli/serve.go:607-636`
(`renderResource`) and `internal/cli/serve.go:703-714` (`readArtifact`);
`internal/codegen/emit.go:493-518` (`unwrap`).

Three things in that table are worth saying out loud:

- **A tool that fails politely is reported as a success.** The protocol has
  two ways to fail. A JSON-RPC error means the request was wrong or the server
  broke. `isError: true` means the tool ran and failed, which is the one the
  model is meant to see and correct. mcpx reports the first as `isError`
  and turns the second into a normal result, on both the ordinary path and the
  ask path, because both render through `renderResult`, which parses
  `isError` and never reads it (`internal/cli/commands.go:392`,
  `internal/cli/serve_ask.go:189-205`).
- **Scripts see more than hosts do.** A script receives the upstream's blocks
  and `structuredContent` intact and gets `isError` as an exception. The model
  calling `mcpx_call` gets a text rendering. Moving to `mcpx_exec` is the
  workaround today.
- **`resources/read` could carry bytes, and mcpx says it cannot.** The comment
  on `readArtifact` reads "resources/read carries text, so a binary body comes
  back base64" (`internal/cli/serve.go:703-707`). But `BlobResourceContents`
  with a base64 `blob` field is in every revision
  (`schema/2024-11-05/schema.ts:506-512`,
  `schema/2026-07-28/schema.ts:1230`). mcpx puts base64 in `text` under an
  `image/png` mime type, which a client following the schema will treat as
  text.

---

## `downgrade()`, per type and revision

What the edge function would do if a block reached it, by client revision.
"Untouched" means the block is sent as is.

| carried | 2025-03-26 client | 2025-06-18 | 2025-11-25 | 2026-07-28 | live at `05c78b2`? |
| --- | --- | --- | --- | --- | --- |
| `structuredContent` | removed, appended to `content` as indented JSON text | untouched | untouched | untouched | no: nothing emits it |
| `resource_link` | becomes `{type:"resource", resource:{uri, mimeType: <the link's mime>, text: "name — uri"}}` | untouched | untouched | untouched | **yes**, for `mcpx_exec` artifacts |
| `audio` | untouched (2025-03-26 defines it) | untouched | untouched | untouched | no: the branch is for revisions below 2025-03-26, which mcpx refuses |
| `image`, `text`, embedded `resource` | untouched in every revision | | | | n/a |
| `resultType` | removed | removed | removed | stamped `"complete"` unless set | **yes** |
| `inputRequests` / `requestState` | never built for a legacy client | | | built | yes, modern only |
| annotations, `_meta` on blocks, `title`, `icons` | not examined by `downgrade()` | | | | leaks are possible: `prompts/list` sends `title` to 2025-03-26 |

Sources: `internal/mcpserver/revisions.go:132-289`,
`internal/mcpserver/server.go:432-440`.

One defect in the live branch: a PNG artifact downgraded for a 2025-03-26 host
becomes an embedded text resource that says `mimeType: "image/png"` and whose
`text` is a label (`internal/mcpserver/revisions.go:260-273`). The URI stays
machine-readable, which was the goal, but the mime type describes bytes that
are not there. `text/plain` for the label, with the real type in the text,
would be honest.

---

## What the hosts do with each type

Passing a block through is only useful if the host keeps it. The two hosts
this repository pins:

| content | opencode v1 (direct tools) | opencode v1 (code mode) | opencode v2 |
| --- | --- | --- | --- |
| `text` | text | text | text |
| `image` | attachment | not traced | media |
| `audio` | **dropped** | not traced | media |
| embedded `resource` | text, or an allow-listed blob | not traced | text or media |
| `resource_link` | **dropped** | `name: uri` text | `uri` text |
| `structuredContent` | used only when `content` is empty | preferred | becomes the tool's `output` |
| `isError` | thrown with its text | thrown | `ToolFailure` with its text |

Sources: `v1:packages/opencode/src/session/tools.ts:429-436`,
`v1:packages/opencode/src/tool/code-mode.ts:104-109`,
`v1:packages/opencode/src/mcp/catalog.ts:68-75`,
`v2:packages/core/src/mcp/client.ts:324-328`,
`v2:packages/core/src/tool/mcp.ts:78-97`.

So under v1 with code mode off, an `mcpx_exec` artifact link never reaches the
model. The artifact is still readable, but only by something that knows to
ask. Under v2 a pass-through `mcpx_call` would deliver images, which it
throws away today.

## What mcpx should do

1. Pass `mcpx_call` results through as the upstream sent them, `isError`
   included, and let `downgrade()` do the job it was written for. The text
   rendering belongs to the CLI, where a person is reading.
2. Keep the artifact interception (`internal/execsvc/media.go`) for large media.
   It is the context-economy win, and it is orthogonal to revisions.
3. Put binary resource bodies in `blob`, not base64 in `text`.
4. Extend `downgrade()` to lists before anything starts leaking there:
   `title`, `icons`, `outputSchema` and `annotations` on `tools/list` and
   `prompts/list` for revisions that do not define them.
5. If 2024-11-05 is ever served, add its floors. Then `audio`, tool
   `annotations` and the `completions` capability start needing the branches
   that are dead today.

The individual rows, with value, effort and risk, are in
[register/content-types.md](register/content-types.md).
