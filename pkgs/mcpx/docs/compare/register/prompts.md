# Prompts

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  Prompt, PromptArgument and PromptMessage per revision, content allowed in prompt messages, and how mcpx and opencode pass prompts on
```

Prompts are server-authored message templates a user picks — opencode turns each into a slash command — and the shape
has grown only by optional fields (`title`, `icons`, `_meta`) and by what a message may carry (audio in 2025-03-26,
`resource_link` in 2025-06-18). What matters most at `05c78b2` is that mcpx collapses every `prompts/get` into one user
text message, and sends the 2025-06-18 `title` to 2025-03-26 hosts because lists are never revision-filtered.
Content-block types are in the content-types register, `prompts.listChanged` in the capabilities register, cache hints in
the pagination-caching register, and `prompts/get` failures reported as `-32602` are ERR-04 in the resources register.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| PRM-01 | `Prompt.title` | `2025-06-18 has` `mcpx missing` | `24-11 — · 25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✗ — kept, but also sent to 2025-03-26 hosts (#207) | + low | S | low |
| PRM-02 | `PromptArgument.title`: in the schema, not in 2025-06-18 prose | `2025-06-18 has` `specs conflict` `mcpx missing` | `25-06 ✓ (schema) · 25-11 ✓ · 26-07 ✓` | ✗ — dropped on parse (#207) | + low | S | low |
| PRM-03 | `Prompt.icons` (not on arguments) | `2025-11-25 has` `mcpx missing` | `25-11 ✓ · 26-07 ✓` | ✗ — dropped on parse (#207) | + low | S | low |
| PRM-04 | `_meta` on `Prompt` (not on arguments) | `2025-06-18 has` `mcpx missing` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✗ — dropped on parse (#207) | + med | S | med |
| PRM-05 | audio allowed in `PromptMessage.content` | `2025-03-26 has` | `24-11 T I R · 25-03 +audio · later same` | ✗ — dropped by flattening (PRM-07) | + low | S | low |
| PRM-06 | `resource_link` allowed in prompt messages; prose lags until 2026 | `2025-06-18 has` `specs conflict` | `25-06 ✓ (schema) · 25-11 ✓ (schema) · 26-07 ✓ (schema + prose)` | ✗ — dropped by flattening (PRM-07) | + low | S | low |
| PRM-07 | mcpx flattens `prompts/get` into one user text message | `2024-11-05 has` `mcpx missing` | `every revision allows many messages, roles, content types` | ✗ — roles, order and non-text content lost (#206) | + med | S | med |
| PRM-08 | opencode turns MCP prompts into slash commands (text only) | `opencode v1 has` `opencode v2 has` | `opencode v1 ✓ ($1..$n) · v2 ✓ (server:prompt)` | ✓ — mcpx prompts appear as commands | + low | S | low |

## PRM-01 `Prompt.title`

- **What.** A human display name for a prompt, separate from `name`, via `BaseMetadata`.
- **Where.** 2025-06-18 onward; documented in 2025-06-18 prose.
- **mcpx @ 05c78b2.** The client keeps an upstream `title` (`internal/mcpclient/client.go:786`), and mcpx's `PromptRef`
  emits it (`internal/mcpserver/server.go:94`) to every host. `downgrade()` touches only content, messages,
  `structuredContent` and `resultType` (`internal/mcpserver/revisions.go:132-170`), so a 2025-03-26 host receives a field
  its revision does not define — a send-conservatively conflict.
- **Value to mcpx.** + low; the fix is the list filtering the tools register (TOOL-25) also needs.
- **Effort.** S.
- **Risk.** Low: unknown fields are usually ignored. The same gap applies to any future `icons` or `_meta`.
- **Detail.** The 2025-03-26 `Prompt` is `name`, `description`, `arguments` only.
- **Sources.** `schema/2025-06-18/schema.ts:701` "export interface Prompt extends BaseMetadata {";
  `2025-06-18/server/prompts.mdx:176` "- `title`: Optional human-readable name of the prompt for display purposes.";
  `schema/2025-03-26/schema.ts:599-612`; `internal/mcpserver/server.go:94` "Title       string      `;
  `internal/mcpserver/revisions.go:132-170`

## PRM-02 `PromptArgument.title`

- **What.** Each prompt argument may carry a display `title`.
- **Where.** In the 2025-06-18 schema onward (`PromptArgument extends BaseMetadata`); the 2025-06-18 prose documents
  `Prompt.title` but not the argument's.
- **mcpx @ 05c78b2.** Neither the client's `PromptArgument` (`internal/mcpclient/client.go:792-796`) nor mcpx's
  `PromptArg` (`internal/mcpserver/server.go:100-104`) has a `title`, so it is dropped.
- **Value to mcpx.** + low: argument labels in a host's prompt form.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** `PromptReference` in completion requests gains `title` the same way (completion register).
- **Sources.** `schema/2025-06-18/schema.ts:722` "export interface PromptArgument extends BaseMetadata {";
  `internal/mcpclient/client.go:792-796`; `internal/mcpserver/server.go:100-104`

## PRM-03 `Prompt.icons`

- **What.** `Prompt extends BaseMetadata, Icons`. `PromptArgument` gets no icons.
- **Where.** 2025-11-25 and 2026.
- **mcpx @ 05c78b2.** The client `Prompt` struct has `name`, `title`, `description`, `arguments` only
  (`internal/mcpclient/client.go:784-789`); icons are dropped.
- **Value to mcpx.** + display.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** Same `Icon` shape as tools and resources.
- **Sources.** `schema/2025-11-25/schema.ts:984` "export interface Prompt extends BaseMetadata, Icons {";
  `2025-11-25/server/prompts.mdx:185` "- `icons`: Optional array of icons for display in user interfaces";
  `internal/mcpclient/client.go:784-789`

## PRM-04 `_meta` on `Prompt`

- **What.** Arbitrary metadata on a prompt definition. `PromptArgument` has none.
- **Where.** 2025-06-18 onward.
- **mcpx @ 05c78b2.** Dropped on parse (`internal/mcpclient/client.go:784-789`).
- **Value to mcpx.** + extension data must survive the proxy.
- **Effort.** S.
- **Risk.** Extension metadata silently lost.
- **Detail.** See the tools register for `_meta` on `Tool`.
- **Sources.** `schema/2025-06-18/schema.ts:714` "_meta?: { [key: string]: unknown };";
  `internal/mcpclient/client.go:784-789`

## PRM-05 audio in prompt messages

- **What.** 2024-11-05 prompt messages carry text, image or an embedded resource; 2025-03-26 adds audio.
  `PromptMessage.content` is always one block, never an array.
- **Where.** 2025-03-26 onward.
- **mcpx @ 05c78b2.** `downgradeMessages` rewrites each message's single block per revision
  (`internal/mcpserver/revisions.go:236-258`), and audio's floor is 2025-03-26, which is mcpx's own floor, so no rewrite
  is ever needed. In practice audio never reaches a host at all: `prompts/get` keeps only text (PRM-07).
- **Value to mcpx.** + low.
- **Effort.** S (with PRM-07).
- **Risk.** Low.
- **Detail.** mcpx does not serve 2024-11-05, the one revision where audio in a prompt would be invalid.
- **Sources.** `schema/2024-11-05/schema.ts:607` "content: TextContent | ImageContent | EmbeddedResource;";
  `schema/2025-03-26/schema.ts:645` "content: TextContent | ImageContent | AudioContent | EmbeddedResource;";
  `internal/mcpserver/revisions.go:236-258`

## PRM-06 `resource_link` in prompt messages

- **What.** From 2025-06-18 `PromptMessage.content` is a `ContentBlock`, so a prompt may carry a `resource_link` (a URI to
  fetch) instead of embedding contents.
- **Where.** In the schema from 2025-06-18. The 2025-06-18 and 2025-11-25 prompts prose documents only text, image, audio
  and embedded resources, and the 2025-06-18 changelog advertises resource links only "in tool call results"; a
  "Resource Links" section first appears in 2026 prose.
- **mcpx @ 05c78b2.** `downgradeMessages` would turn a `resource_link` into an embedded resource for pre-2025-06-18 hosts
  (`internal/mcpserver/revisions.go:248-253`), but PRM-07 means no link ever reaches it.
- **Value to mcpx.** + low.
- **Effort.** S (with PRM-07).
- **Risk.** Low.
- **Detail.** A host implemented from the 2025-06-18 prose alone may not expect links in prompts.
- **Sources.** `schema/2025-06-18/schema.ts:750` "content: ContentBlock;"; `2026-07-28/server/prompts.mdx:274` "####
  Resource Links"; `2025-06-18/changelog.mdx:27` "Add support for **[resource
  links](https://modelcontextprotocol.io/specification/2025-06-18/server/tools#resource-links)** in"; `internal/mcpserver/revisions.go:248-253`

## PRM-07 mcpx flattens `prompts/get`

- **What.** A `GetPromptResult` is an optional `description` plus `messages[]`, each with a `role` (`user` or
  `assistant`) and one content block — enough for few-shot prompts that alternate roles and carry images or resources.
- **Where.** Every revision.
- **mcpx @ 05c78b2.** The upstream result is rendered to one string by `renderPrompt`
  (`internal/cli/serve.go:640-669`): the description is folded into the text, non-user roles become `[role] ` prefixes
  (`internal/cli/serve.go:662-664`), non-text content is dropped, and mcpx answers with a single
  `{role:"user", content:{type:"text"}}` message (`internal/mcpserver/server.go:759-764`).
- **Value to mcpx.** + few-shot prompts keep working through mcpx; images and resources survive.
- **Effort.** S — pass `messages` through and let `downgradeMessages` do its job.
- **Risk.** If not fixed: prompt semantics change silently — an assistant turn becomes user text.
- **Detail.** `downgradeMessages` exists (`internal/mcpserver/revisions.go:236-258`) but only ever sees one text block.
  opencode also keeps only the text of prompt messages (PRM-08), so for opencode hosts the loss is mostly the roles.
- **Also recorded from the content types register.** Multi-message, multi-role prompts exist in every revision.
  opencode v1 and v2 use only the text of prompt messages anyway (slash commands). `renderPrompt`
  (`internal/cli/serve.go:640-669`), used by `prompts/get` (`internal/mcpserver/server.go:759-764`) and the ask path
  (`internal/mcpserver/ask.go:271-275`). An embedded text resource in a prompt message is dropped too: `renderPrompt`
  reads only `content.text`, and a resource's text sits at `content.resource.text`.
- **Sources.** `internal/cli/serve.go:662-664` "fmt.Fprintf(&b, "; `internal/cli/serve.go:640-669`;
  `internal/mcpserver/server.go:759-764`; `internal/mcpserver/revisions.go:236-258`; `internal/mcpserver/server.go:760-763`; `schema/2024-11-05/schema.ts:607` "content: TextContent | ImageContent | EmbeddedResource;"

## PRM-08 opencode prompts as slash commands

- **What.** opencode lists prompts from servers that declare `prompts` and exposes each as a slash command, using only
  the `text` content of the resulting messages.
- **Where.** opencode v1: command key `sanitize(server):sanitize(prompt)`, positional arguments mapped to `$1..$n`,
  `getPrompt` fetched lazily when the template is read. opencode v2: `server:prompt`, parsed arguments, refreshed on
  `prompts/list_changed`.
- **mcpx @ 05c78b2.** mcpx re-exposes upstream prompts as `<ns>_<name>` (wire W19), so under an opencode server entry
  called `mcpx` a prompt appears as `mcpx:<ns>_<name>`.
- **Value to mcpx.** + mcpx-proxied prompts are usable in opencode with no extra work; − image and resource content in a
  prompt never reaches opencode either way.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** v1 fetches prompts fresh on each command listing (no cache), so it ignores `prompts/list_changed` without
  going stale for long.
- **Sources.** `v1:packages/opencode/src/command/index.ts:105`; `v1:packages/opencode/src/command/index.ts:124`;
  `v1:packages/opencode/src/mcp/catalog.ts:108`; `v2:packages/core/src/plugin/command.ts:55`;
  `v2:packages/core/src/plugin/command.ts:105`
