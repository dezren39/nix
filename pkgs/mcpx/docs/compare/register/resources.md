# Resources

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  Resource and ResourceTemplate fields, resources/read contents, subscriptions and resource errors per revision, and how mcpx, opencode and lootbox expose resources
```

Resources are read-only data a server publishes by URI, listed directly or through URI templates; mcpx re-exposes every
upstream's resources under `mcpx://<ns>/…` and publishes script artifacts as a template of its own. What mcpx sends back
at `05c78b2` is lossy — templates carry `uri` instead of the required `uriTemplate`, binary and multi-part contents
collapse into one text item, optional fields are dropped on parse — and `resources/subscribe` is accepted and declared on
stdio but never reaches an upstream. List-change and subscribe capabilities are in the capabilities register, cache hints
in the pagination-caching register, `resource_link` and embedded resources in the content-types register; ERR-06 and
ERR-04 overlap the errors register.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| RES-01 | `Resource.title` / `ResourceTemplate.title` | `2025-06-18 has` `mcpx missing` | `24-11 — · 25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✗ — dropped on parse (#207) | + low | S | low |
| RES-02 | `Resource.size`: in the 2024-11-05 schema, in prose from 2025-03-26 | `2024-11-05 has` `specs conflict` `mcpx missing` | `all five ✓ (schema) · prose from 25-03 · templates never` | ✗ — dropped on parse (#207) | + med | S | low |
| RES-03 | resource `annotations` (`audience`, `priority`) | `2024-11-05 has` `mcpx missing` | `all five ✓ (mixin until 25-03, same wire)` | ✗ — dropped on parse (#207) | + med | S | low |
| RES-04 | `Annotations.lastModified` | `2025-06-18 has` `mcpx missing` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✗ — dropped with `annotations` (#207) | + low | S | low |
| RES-05 | `Resource.icons` / `ResourceTemplate.icons` | `2025-11-25 has` `mcpx missing` | `25-11 ✓ · 26-07 ✓` | ✗ — dropped on parse (#207) | + low | S | low |
| RES-06 | `_meta` on `Resource`, `ResourceTemplate`, read contents | `2025-06-18 has` `mcpx missing` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✗ — dropped on parse and on read (#207) | + med | S | med |
| RES-07 | `ResourceTemplate.uriTemplate` (required) | `2024-11-05 has` `mcpx missing` | `all five ✓` | ✗ — broken: mcpx sends `uri` (wire W18) (#207) | + high | S | high |
| RES-08 | upstream URIs re-exposed as `mcpx://<ns>/<uri>` | `mcpx has, others don't` | `mcpx only` | partial — disambiguates; leading `/` trimmed so URIs can collide | + med | S | low |
| RES-09 | text vs base64 `blob` contents, unchanged since 2024-11-05 | `2024-11-05 has` `mcpx missing` | `all five ✓ (+ _meta 25-06)` | ✗ — always `text`; binary as base64 text or a placeholder (#206) | + med | S | med |
| RES-10 | `contents[]` may hold several items | `2024-11-05 has` `mcpx missing` | `all five ✓` | ✗ — merged into one text item under the requested URI (#206) | + low | S | low |
| RES-11 | a missing resource MUST NOT read as empty `contents` | `2026-07-28 has` | `26-07 ✓ only` | ✓ — failures are errors, never `contents: []` | + low | S | low |
| RES-12 | `resources/subscribe` removed in 2026 | `2024-11-05 has` `2026-07-28 removes` `has better replacement` | `24-11..25-11 ✓ · 26-07 rem (listen filter) · opencode never sends` | acc. — accepted from every era | + med | S | low |
| RES-13 | `resources/unsubscribe` removed in 2026 | `2024-11-05 has` `2026-07-28 removes` `has better replacement` | `24-11..25-11 ✓ · 26-07 rem` | acc. — accepted from every era | + low | S | low |
| RES-14 | `notifications/resources/updated` listen-only and tagged in 2026 | `2024-11-05 has` `2026-07-28 has` `mcpx missing` | `24-11..25-11 after subscribe · 26-07 listen stream + subscriptionId` | partial — sent untagged (#208) | + med | S | med |
| RES-15 | `resources/updated` carries the upstream URI, not `mcpx://` | `2024-11-05 has` `mcpx missing` | `all five (the uri param)` | ✗ — the host cannot correlate the update (#208) | + med | S | med |
| RES-16 | how resources reach the model in each host | `opencode v1 has` `opencode v2 has` | `opencode v1 3 model tools · v2 TUI @ list only · mcpx tool + script API` | ✓ — `mcpx_resource_read` and `readResource()` | + med | S | med |

## RES-01 `Resource.title` / `ResourceTemplate.title`

- **What.** A human display name, separate from the programmatic `name` (which stays required), via `BaseMetadata`.
- **Where.** 2025-06-18 onward.
- **mcpx @ 05c78b2.** The client's `Resource` struct has only `uri`, `uriTemplate`, `name`, `description`, `mimeType`
  (`internal/mcpclient/client.go:137-143`), so `title` is gone before mcpx re-exposes anything; mcpx's own `ResourceRef`
  has no `title` either (`internal/mcpserver/server.go:84-89`).
- **Value to mcpx.** + display in hosts and in `mcpx_resources`.
- **Effort.** S — but re-exposing it needs revision filtering, since lists are not downgraded (tools register).
- **Risk.** Display data lost.
- **Detail.** Same loss for `Prompt`-side titles is in the prompts register.
- **Sources.** `schema/2025-06-18/schema.ts:526` "export interface Resource extends BaseMetadata {";
  `2025-06-18/server/resources.mdx:279` "- `title`: Optional human-readable name of the resource for display
  purposes."; `internal/mcpclient/client.go:137-143`; `internal/mcpserver/server.go:84-89`

## RES-02 `Resource.size`

- **What.** Optional raw size in bytes (before base64), so a client can estimate context use before reading.
- **Where.** In the schema of all five revisions; the 2024-11-05 prose field list omits it, 2025-03-26 prose adds it.
  `ResourceTemplate` never has `size`.
- **mcpx @ 05c78b2.** Dropped on parse (`internal/mcpclient/client.go:137-143`) and absent from `ResourceRef`
  (`internal/mcpserver/server.go:84-89`).
- **Value to mcpx.** + a size before a read is exactly what an agent needs to decide whether to pull a resource into
  context — mcpx's own goal.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** mcpx's artifacts already know their size (it goes into the `resource_link` description), so mcpx could
  emit `size` for its own resources too.
- **Sources.** `schema/2024-11-05/schema.ts:449` "size?: number;"; `2024-11-05/server/resources.mdx:276` "- `mimeType`:
  Optional MIME type"; `2025-03-26/server/resources.mdx:277` "- `size`: Optional size in bytes";
  `internal/mcpclient/client.go:137-143`

## RES-03 resource `annotations`

- **What.** `annotations: {audience?, priority?}` on resources and templates. 2024-11-05 gets it through the `Annotated`
  mixin; 2025-03-26 names the type `Annotations` and adds an explicit field — same JSON.
- **Where.** All five. The rename is not in the 2025-03-26 changelog, and the prose "Annotations" section first appears in
  2025-06-18.
- **mcpx @ 05c78b2.** Dropped on parse (`internal/mcpclient/client.go:137-143`).
- **Value to mcpx.** + `audience: ["user"]` is the upstream saying "keep this out of the model's context"; `priority`
  (0..1) orders what to read first.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** The same `Annotations` type sits on content blocks (content-types register).
- **Sources.** `schema/2024-11-05/schema.ts:417` "export interface Resource extends Annotated {";
  `schema/2025-03-26/schema.ts:475` "annotations?: Annotations;"; `internal/mcpclient/client.go:137-143`

## RES-04 `Annotations.lastModified`

- **What.** Optional ISO 8601 last-modified timestamp in `Annotations`, on resources, templates and content blocks.
- **Where.** 2025-06-18 onward. The 2025-06-18 changelog does not list it.
- **mcpx @ 05c78b2.** Dropped along with `annotations` (RES-03).
- **Value to mcpx.** + a freshness hint for cached reads.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** None beyond RES-03.
- **Sources.** `schema/2025-06-18/schema.ts:1127` "lastModified?: string;"; `2025-06-18/server/resources.mdx:314`
  "**`lastModified`**: An ISO 8601 formatted timestamp"

## RES-05 `Resource.icons` / `ResourceTemplate.icons`

- **What.** `Resource` and `ResourceTemplate` extend `Icons`.
- **Where.** 2025-11-25 and 2026.
- **mcpx @ 05c78b2.** Dropped on parse (`internal/mcpclient/client.go:137-143`).
- **Value to mcpx.** + display.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** Same `Icon` shape as tools (tools register).
- **Sources.** `schema/2025-11-25/schema.ts:802` "export interface Resource extends BaseMetadata, Icons {";
  `schema/2025-11-25/schema.ts:845` "export interface ResourceTemplate extends BaseMetadata, Icons {"

## RES-06 `_meta` on resources and read contents

- **What.** Arbitrary metadata on each `Resource`, `ResourceTemplate` and each item of `resources/read` `contents`.
- **Where.** 2025-06-18 onward.
- **mcpx @ 05c78b2.** Dropped on parse for listings (`internal/mcpclient/client.go:137-143`) and when reads are flattened
  to text (`internal/cli/serve.go:608-637`).
- **Value to mcpx.** + extensions (MCP Apps UI resources among them) carry their data here.
- **Effort.** S.
- **Risk.** Extension metadata silently lost.
- **Detail.** See the tools register for `_meta` on `Tool`.
- **Sources.** `schema/2025-06-18/schema.ts:561` "_meta?: { [key: string]: unknown };";
  `schema/2025-06-18/schema.ts:620`; `internal/cli/serve.go:608-637`

## RES-07 `ResourceTemplate.uriTemplate`

- **What.** A template carries its RFC 6570 URI template in the required field `uriTemplate`; there is no `uri` on a
  template.
- **Where.** All five.
- **mcpx @ 05c78b2.** Broken. `resources/templates/list` returns `[]ResourceRef`, whose only URI field is `json:"uri"`
  (`internal/mcpserver/server.go:85`, `internal/mcpserver/server.go:574-593`, `internal/cli/serve.go:684-698`). Wire W18:
  `{"uri":"mcpx://artifacts/{id}","name":"mcpx artifact",…}`. To a schema-validating host these are malformed; a lax
  one finds no `uriTemplate` to expand.
- **Value to mcpx.** + mcpx's artifact template and every upstream template become usable by hosts.
- **Effort.** S — a separate template type with `uriTemplate`.
- **Risk.** If not fixed: every host ignores or rejects mcpx's templates.
- **Detail.** The client side reads `uriTemplate` correctly (`internal/mcpclient/client.go:139`). lootbox reads
  `uriTemplate` off `resources/list` entries, where it never appears (CODE-46).
- **Sources.** `schema/2025-11-25/schema.ts:851` "uriTemplate: string;"; `internal/mcpserver/server.go:85` "URI
  string `; `internal/cli/serve.go:684-698`; `internal/mcpclient/client.go:139`

## RES-08 `mcpx://<ns>/<uri>` namespacing

- **What.** mcpx re-exposes each upstream resource as `mcpx://<namespace>/<original-uri>`, appends " (ns)" to its
  description, and keeps only `name`, `description` and `mimeType`.
- **Where.** mcpx only; it is how a proxy keeps two servers' identical URIs apart (tools register, TOOL-02, for the same
  rule on names).
- **mcpx @ 05c78b2.** `internal/cli/serve.go:486-495`. A leading `/` is trimmed from the upstream URI
  (`internal/cli/serve.go:490`), so an upstream URI `/abs` and `abs` map to the same `mcpx://ns/abs`.
- **Value to mcpx.** + disambiguation is necessary; − the optional fields dropped with it (RES-01..RES-06).
- **Effort.** S.
- **Risk.** Low; the collision needs an upstream that publishes bare paths.
- **Detail.** The daemon's `/v1/resources` and the `mcpx_resources` tool use the same listing. Reads strip the prefix
  back off before calling the upstream.
- **Sources.** `internal/cli/serve.go:490` "URI:         \"mcpx://\" + r.Namespace + \"/\" + strings.TrimPrefix(r.URI,
  \"/\"),"; `internal/cli/serve.go:486-495`

## RES-09 text versus `blob` contents

- **What.** `resources/read` returns `contents: (TextResourceContents | BlobResourceContents)[]`: `uri`, optional
  `mimeType`, and either `text` or base64 `blob`. Only `_meta` was added (2025-06-18).
- **Where.** All five, otherwise unchanged.
- **mcpx @ 05c78b2.** Every read answers one `TextResourceContents` (`internal/mcpserver/server.go:720-722`). An mcpx
  artifact with a binary type becomes base64 in `text` under e.g. `image/png` (`internal/cli/exec.go:548-552`). An
  upstream `blob` becomes the placeholder `(N bytes of mime, base64)` (`internal/cli/serve.go:608-637`), where N is the
  length of the base64 string, not the byte count.
- **Value to mcpx.** + hosts that can render images get them; − blobs cost context, but `resources/read` is the
  explicit "I want the bytes" call.
- **Effort.** S — emit `blob` for binary.
- **Risk.** A host that trusts `mimeType` tries to decode text as an image; an upstream binary resource is unreadable
  through mcpx's MCP surface.
- **Detail.** The first item's `mimeType` becomes the whole result's (`internal/cli/serve.go:621-624`); an empty type
  becomes `text/plain`.
- **Also recorded from the content types register.** `BlobResourceContents` in every revision. Result shape at
  `internal/mcpserver/server.go:720-722`; artifacts at `internal/cli/exec.go:548-552`; upstream flattening in
  `renderResource` (`internal/cli/serve.go:608-637`). Several upstream `contents[]` items, possibly with different
  URIs and types, are joined with newlines into one item under the requested URI, and the first item's `mimeType`
  becomes the whole result's (`internal/cli/serve.go:621-624`, `:636`); per-part URIs (a directory listing, say) are
  lost.
- **Sources.** `schema/2024-11-05/schema.ts:359` "contents: (TextResourceContents | BlobResourceContents)[];";
  `schema/2026-07-28/schema.ts:1230`; `internal/cli/exec.go:552` "return
  base64.StdEncoding.EncodeToString(body), mime, nil"; `internal/mcpserver/server.go:720-722`; `schema/2024-11-05/schema.ts:506`; `schema/2025-03-26/schema.ts:537-548`; `internal/cli/serve.go:636` "return strings.Join(parts, \"\n\"), mime, nil"

## RES-10 several `contents` items merged into one

- **What.** A read may return several items, with different URIs and types (a directory listing, a multi-part
  document).
- **Where.** All five.
- **mcpx @ 05c78b2.** The upstream `contents[]` is joined with newlines into one text item under the requested URI
  (`internal/cli/serve.go:619-636`); the ask path uses the same renderer (`internal/cli/serve_ask.go:196-201`).
- **Value to mcpx.** + low.
- **Effort.** S — pass the array through with rewritten URIs.
- **Risk.** Per-part URIs and types are lost.
- **Detail.** Blob parts become placeholders inside the joined text (RES-09).
- **Sources.** `internal/cli/serve.go:636` "return strings.Join(parts, "; `internal/cli/serve_ask.go:196-201`

## RES-11 no empty `contents` for a missing resource

- **What.** 2026 states outright that "not found" must be an error, never `contents: []`; an empty array stays legal for
  a resource that exists but is empty.
- **Where.** 2026; not stated in earlier revisions.
- **mcpx @ 05c78b2.** Read failures are errors (`internal/mcpserver/server.go:713-716`), and a success always carries
  exactly one item (`internal/mcpserver/server.go:720-722`).
- **Value to mcpx.** + low: already conforms.
- **Effort.** None.
- **Risk.** Low. The one-item shape has its own ambiguity: an upstream that returns `contents: []` for an existing empty
  resource reaches the host as one item with empty `text`.
- **Detail.** None beyond the above.
- **Sources.** `2026-07-28/server/resources.mdx:410` "Servers **MUST NOT** return an empty `contents` array for a
  non-existent resource."; `internal/mcpserver/server.go:720-722`

## RES-12 `resources/subscribe`

- **What.** A legacy per-URI subscribe RPC. 2026 removes it; a client lists the URIs in `resourceSubscriptions` on
  `subscriptions/listen` instead.
- **Where.** 2024-11-05 through 2025-11-25. Neither opencode version ever sends it.
- **mcpx @ 05c78b2.** Accepted from any era (`internal/mcpserver/server.go:595-620`) — accept liberally. The per-URI set is
  kept on the connection and mapped onto `restartListen` with only the URI list (`internal/mcpserver/server.go:619`),
  which replaces any list-changed filter the connection had open.
- **Value to mcpx.** + accepting from every era is right.
- **Effort.** S.
- **Risk.** A host mixing `subscriptions/listen` for list changes and `resources/subscribe` on stdio loses its
  list-changed filter on the first subscribe. As a client, calling it against a pure-2026 upstream would get `-32601`
  (mcpx's client makes it a no-op for modern upstreams).
- **Detail.** Over HTTP the call answers `{}` and nothing can ever be pushed (capabilities register, CAP-17).
- **Sources.** `schema/2025-11-25/schema.ts:751` "method: \"resources/subscribe\";"; `2026-07-28/changelog.mdx:18`
  "Replace the HTTP GET endpoint and `resources/subscribe`/`resources/unsubscribe` with `subscriptions/listen`";
  `internal/mcpserver/server.go:619` "s.restartListen(c, ListenFilter{ResourceSubscriptions: uris})"

## RES-13 `resources/unsubscribe`

- **What.** The legacy counterpart of RES-12. 2026 has no per-URI unsubscribe: the client cancels the listen stream and
  opens a new one with a different filter.
- **Where.** 2024-11-05 through 2025-11-25.
- **mcpx @ 05c78b2.** Accepted from any era (`internal/mcpserver/server.go:595`); on stdio it answered `{}` on the wire
  (stdio run 2).
- **Value to mcpx.** + fine as is.
- **Effort.** Done.
- **Risk.** None.
- **Detail.** Unsubscribing the last URI restarts the listener with an empty filter.
- **Sources.** `schema/2025-11-25/schema.ts:769` "method: \"resources/unsubscribe\";";
  `2026-07-28/basic/patterns/subscriptions.mdx:9` "the client cancels it. It replaces the former
  `resources/subscribe` RPC and the HTTP GET"; `internal/mcpserver/server.go:595`

## RES-14 `notifications/resources/updated` in 2026

- **What.** The notification and its `uri` param are unchanged, but in 2026 it is sent only on a `subscriptions/listen`
  stream for URIs in `resourceSubscriptions`, and every listen-stream notification MUST carry
  `_meta["io.modelcontextprotocol/subscriptionId"]`.
- **Where.** All five; delivery changes in 2026.
- **mcpx @ 05c78b2.** Emitted with params `{uri}` and no `_meta` (`internal/events/events.go:260-261`), to legacy and
  modern listeners alike.
- **Value to mcpx.** + 2026 stdio hosts can attribute updates to a subscription.
- **Effort.** S — tag on listen streams only; legacy subscribers must not get the key.
- **Risk.** 2026 hosts multiplexing several subscriptions cannot correlate untagged notifications.
- **Detail.** In every revision the `uri` "might be a sub-resource of the one that the client actually subscribed to".
  The tagging rule applies to all listen-stream notifications (notifications register).
- **Sources.** `schema/2026-07-28/schema.ts:1421` "This is only sent for resources the client opted in to via the
  `resourceSubscriptions` field"; `schema/2024-11-05/schema.ts:403` "method: \"notifications/resources/updated\";";
  `internal/events/events.go:261`

## RES-15 `resources/updated` carries the upstream URI

- **What.** A host subscribes to the URI it was given; the update has to name that URI.
- **Where.** Every revision.
- **mcpx @ 05c78b2.** The listener strips `mcpx://<ns>/` from the host's URIs to match upstream events
  (`internal/cli/serve.go:747-758`) but the outgoing notification carries the upstream URI (`internal/events/events.go:261`),
  which the host never saw. The namespace is discarded in the strip, so the same URI from two servers matches both.
- **Value to mcpx.** + updates become usable.
- **Effort.** S — re-prefix on the way out and keep the namespace in the filter.
- **Risk.** If not fixed: every update is uncorrelatable for the host.
- **Detail.** Only matters once SUB-15 makes updates arrive at all.
- **Sources.** `internal/events/events.go:261` "return \"notifications/resources/updated\", map[string]any{\"uri\":
  e.URI}, true"; `internal/cli/serve.go:747-758`

## RES-16 how resources reach the model

- **What.** Every revision calls resources "application-driven": the host decides how they reach the model. Hosts
  differ sharply.
- **Where.** opencode v1 adds three model-facing tools — `list_mcp_resources`, `list_mcp_resource_templates`,
  `read_mcp_resource` — to every request once any connected server declares `resources`, even in code mode; reads go
  through a `read` permission on `mcp:<server>:<uri>`. opencode v2 lists resources and templates only for the TUI `@`
  autocomplete and `GET /api/mcp/resource`; `Mcp.readResource` exists with no caller. lootbox generates functions
  (CODE-46).
- **mcpx @ 05c78b2.** Resources are reachable through the `mcpx_resources`, `mcpx_resource_templates` and
  `mcpx_resource_read` op tools (wire W6) and from scripts through one `readResource(uri)` export
  (`internal/execsvc/execsvc.go:610-611`).
- **Value to mcpx.** + opencode v1 pays three schemas per request just because mcpx declares `resources`
  (capabilities register, CAP-16); v2 never reads them, so mcpx's own tools are the only route for a v2 model.
- **Effort.** S.
- **Risk.** Context cost in v1.
- **Detail.** v1's three tools duplicate mcpx's own resource tools when mcpx is the server.
- **Sources.** `2026-07-28/server/resources.mdx:24` "Resources in MCP are designed to be **application-driven**, with
  host applications"; `v1:packages/opencode/src/session/tools.ts:27`; `v1:packages/opencode/src/session/tools.ts:136`;
  `v1:packages/opencode/src/session/tools.ts:388`; `v2:packages/tui/src/component/prompt/autocomplete.tsx:413`;
  `v2:packages/core/src/mcp/index.ts:713`; `internal/execsvc/execsvc.go:610-611`
