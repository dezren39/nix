# MCP revisions × features

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare, area:protocol
description:  every method, notification, capability field, content type,
              error code and HTTP header, against the five published
              revisions of the Model Context Protocol.
```

What the specification defines, revision by revision. This page is about the
specification only; what mcpx does with each row is in
[matrix-mcpx.md](matrix-mcpx.md) and, per difference, in the
[register](README.md#the-register). Every row names one source; sources are
at `modelcontextprotocol/modelcontextprotocol` commit `046fa30` (conventions in
[README.md](README.md#citations)).

Columns: `24-11` = 2024-11-05, `25-03` = 2025-03-26, `25-06` = 2025-06-18,
`25-11` = 2025-11-25, `26-07` = 2026-07-28.

Legend: ✓ defined · — absent · **dep** defined but deprecated (still works;
scheduled for removal under the 2026 feature-lifecycle policy) · **rem**
removed (was defined earlier) · **ext** only through the
`io.modelcontextprotocol/tasks` extension · **MRTR** exists only as an input
request embedded in an `InputRequiredResult`, never as a JSON-RPC request of
its own · (s) schema only · (p) prose only.

Three things the tables make visible that no single changelog says:

- **2026-07-28 is not an increment.** It removes the handshake, `ping`,
  sessions, the GET stream, resumability, `logging/setLevel`,
  `resources/subscribe`, every server-to-client request and every unsolicited
  notification (list changes now arrive only on an opted-in
  `subscriptions/listen` stream), and it adds required things to every
  exchange: `_meta` version and capabilities on every request, `resultType` on
  every result, `ttlMs`/`cacheScope` on every cacheable result, and on HTTP
  the `Mcp-Method` header (plus `Mcp-Name` for `tools/call`, `resources/read`
  and `prompts/get`).
- **Deprecated is not removed.** Roots, Sampling and Logging are deprecated in
  2026-07-28 but still defined and still usable, now only through
  `input_required` (roots, sampling) or per-request `logLevel` (logging).
- **Batching lived for one revision.** Added in 2025-03-26, removed in
  2025-06-18. A server that claims 2025-03-26 is on the hook for it.

## 1. Base protocol

| Feature | 2024-11-05 | 2025-03-26 | 2025-06-18 | 2025-11-25 | 2026-07-28 | Source |
|---|---|---|---|---|---|---|
| `initialize` / `notifications/initialized` | ✓ | ✓ | ✓ | ✓ | rem | `schema/2026-07-28/schema.ts:3153` |
| Legacy counter-offer version negotiation | ✓ | ✓ | ✓ | ✓ | rem | `2025-11-25/basic/lifecycle.mdx:170` |
| `InitializeResult.instructions` | ✓(s) | ✓ | ✓ | ✓ | moved to `DiscoverResult` | `schema/2024-11-05/schema.ts:172` |
| `Implementation.title` | — | — | ✓ | ✓ | ✓ | `schema/2025-06-18/schema.ts:331` |
| `Implementation.icons/description/websiteUrl` | — | — | — | ✓ | ✓ | `schema/2025-11-25/schema.ts:550` |
| `server/discover` (MUST implement) | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:665` |
| Per-request `_meta` protocolVersion + clientCapabilities (required) | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:76` |
| `clientInfo` / `serverInfo` in `_meta` (SHOULD) | — | — | — | — | ✓ | `2026-07-28/basic/index.mdx:384` |
| `UnsupportedProtocolVersionError` (-32022) | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:483` |
| `ping` | ✓ | ✓ | ✓ | ✓ | rem | `seps/2575-stateless-mcp.md:569` |
| JSON-RPC batching | — | ✓ | rem | — | — | `2025-06-18/changelog.mdx:12` |
| `resultType` on every result | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:234` |
| `_meta` on requests/notifications/results | ✓(s) | ✓(s) | ✓ (+rules) | ✓ | ✓ | `schema/2024-11-05/schema.ts:21` |
| Reserved-prefix rule | — | — | forward-DNS | reverse-DNS | reverse-DNS | `2025-11-25/basic/index.mdx:208` |
| OTel `traceparent`/`tracestate`/`baggage` reserved | — | — | — | — | ✓ | `2026-07-28/basic/index.mdx:421` |
| `notifications/progress` | ✓ both ways | ✓ +`message` | ✓ | ✓ (+tasks) | ✓ server→client only | `schema/2025-03-26/schema.ts:316` |
| `notifications/cancelled` | ✓ both ways | ✓ | ✓ | ✓ `requestId?` | ✓ `requestId` req'd; transport-specific | `schema/2025-11-25/schema.ts:223` |
| Server-initiated requests | ✓ | ✓ | ✓ | ✓ | rem (MRTR) | `2026-07-28/basic/patterns/index.mdx:20` |
| `subscriptions/listen` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:1314` |
| `logging/setLevel` | ✓ | ✓ | ✓ | ✓ | rem (per-request `logLevel`; Logging dep) | `2026-07-28/changelog.mdx:20` |
| Tasks | — | — | — | ✓ core (experimental) | extension | `2026-07-28/changelog.mdx:22` |
| `extensions` capability | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:785` |
| Cursor pagination | ✓ | ✓ | ✓ | ✓ | ✓ (+ `""` valid; cacheable) | `2026-07-28/server/utilities/pagination.mdx:106` |
| `CacheableResult` `ttlMs`/`cacheScope` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:1081` |
| Error `id` optional | — | — | — | ✓ | ✓ | `schema/2025-11-25/schema.ts:161` |
| Error-code allocation policy | — | — | — | — | ✓ | `2026-07-28/basic/index.mdx:117` |
| JSON Schema 2020-12 default dialect | — | — | — | ✓ | ✓ (+`$ref` rules) | `2025-11-25/basic/index.mdx:144` |
| stdio transport | ✓ | ✓ (+batch) | ✓ | ✓ | ✓ | `2026-07-28/basic/transports/stdio.mdx:13` |
| HTTP+SSE transport | ✓ | dep | dep | dep | dep (registry) | `2026-07-28/deprecated.mdx:31` |
| Streamable HTTP | — | ✓ | ✓ | ✓ (+polling) | ✓ (reshaped, POST-only) | `2026-07-28/basic/transports/streamable-http.mdx:9` |
| `Mcp-Session-Id` sessions | — | ✓ | ✓ | ✓ | rem | `2026-07-28/basic/transports/streamable-http.mdx:685` |
| GET standalone SSE stream | (SSE endpoint) | ✓ | ✓ | ✓ | rem | `2026-07-28/basic/transports/streamable-http.mdx:19` |
| `Last-Event-ID` resumability | — | ✓ | ✓ | ✓ | rem | `2026-07-28/basic/transports/streamable-http.mdx:157` |
| Closing stream = cancel | — | no (SHOULD NOT) | no | no | ✓ MUST | `2026-07-28/basic/transports/streamable-http.mdx:235` |
| OAuth 2.1 authorization | — | ✓ | ✓ (RS + RFC 9728/8707) | ✓ (+OIDC, CIMD, scopes) | ✓ (+RFC 9207, DCR dep) | `2025-06-18/changelog.mdx:16` |
| Dynamic Client Registration | — | SHOULD | SHOULD | MAY | dep | `2026-07-28/deprecated.mdx:29` |

## 2. Methods

`initialize` and `ping` are in §1.

| method | 24-11 | 25-03 | 25-06 | 25-11 | 26-07 | source |
| --- | --- | --- | --- | --- | --- | --- |
| `server/discover` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:666` |
| `tools/list` | ✓ | ✓ | ✓ | ✓ | ✓ (+cacheable) | `schema/2026-07-28/schema.ts:1768` |
| `tools/call` | ✓ | ✓ | ✓ | ✓ (+`task`) | ✓ (+MRTR) | `schema/2026-07-28/schema.ts:1883` |
| `prompts/list` | ✓ | ✓ | ✓ | ✓ | ✓ (+cacheable) | `schema/2026-07-28/schema.ts:1567` |
| `prompts/get` | ✓ | ✓ | ✓ | ✓ | ✓ (+MRTR) | `schema/2026-07-28/schema.ts:1622` |
| `resources/list` | ✓ | ✓ | ✓ | ✓ | ✓ (+cacheable) | `schema/2026-07-28/schema.ts:1122` |
| `resources/templates/list` | ✓ | ✓ | ✓ | ✓ | ✓ (+cacheable) | `schema/2026-07-28/schema.ts:1158` |
| `resources/read` | ✓ | ✓ | ✓ | ✓ | ✓ (+MRTR, +cacheable) | `schema/2026-07-28/schema.ts:1217` |
| `resources/subscribe` | ✓ | ✓ | ✓ | ✓ | rem | `schema/2025-11-25/schema.ts:751` |
| `resources/unsubscribe` | ✓ | ✓ | ✓ | ✓ | rem | `schema/2025-11-25/schema.ts:769` |
| `subscriptions/listen` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:1315` |
| `completion/complete` | ✓ (ungated) | ✓ | ✓ (+context) | ✓ | ✓ | `schema/2024-11-05/schema.ts:955` |
| `logging/setLevel` | ✓ | ✓ | ✓ | ✓ | rem | `schema/2025-11-25/schema.ts:1520` |
| `sampling/createMessage` | ✓ | ✓ | ✓ | ✓ (+tools, +`task`) | dep, MRTR | `schema/2026-07-28/schema.ts:2186` |
| `roots/list` | ✓ | ✓ | ✓ | ✓ | dep, MRTR | `schema/2026-07-28/schema.ts:2719` |
| `elicitation/create` | — | — | ✓ (form) | ✓ (form+url, +`task`) | ✓, MRTR | `schema/2026-07-28/schema.ts:2857` |
| `tasks/get` | — | — | — | ✓ | ext | `schema/2025-11-25/schema.ts:1401` |
| `tasks/result` | — | — | — | ✓ | rem (ext: `-32601`) | `schema/2025-11-25/schema.ts:1423` |
| `tasks/list` | — | — | — | ✓ | rem (not in ext) | `schema/2025-11-25/schema.ts:1471` |
| `tasks/cancel` | — | — | — | ✓ | ext (ack-only) | `schema/2025-11-25/schema.ts:1449` |
| `tasks/update` | — | — | — | — | ext | `seps/2663-tasks-extension.md:358` |

## 3. Notifications

| notification | 24-11 | 25-03 | 25-06 | 25-11 | 26-07 | source |
| --- | --- | --- | --- | --- | --- | --- |
| `notifications/initialized` | ✓ | ✓ | ✓ | ✓ | rem | `schema/2025-11-25/schema.ts:301` |
| `notifications/cancelled` | ✓ both ways | ✓ | ✓ | ✓ (`requestId` optional) | ✓ client→server; server→client only to end a listen on stdio | `schema/2026-07-28/schema.ts:637` |
| `notifications/progress` | ✓ both ways | ✓ (+`message`) | ✓ | ✓ | ✓ server→client only, request-scoped | `schema/2026-07-28/schema.ts:1041` |
| `notifications/message` | ✓ | ✓ | ✓ | ✓ | dep; only with per-request `logLevel`; request-scoped | `schema/2026-07-28/schema.ts:2059` |
| `notifications/tools/list_changed` | ✓ unsolicited | ✓ | ✓ | ✓ | ✓ listen-only (`toolsListChanged`) | `schema/2026-07-28/schema.ts:1896` |
| `notifications/prompts/list_changed` | ✓ unsolicited | ✓ | ✓ | ✓ | ✓ listen-only (`promptsListChanged`) | `schema/2026-07-28/schema.ts:1754` |
| `notifications/resources/list_changed` | ✓ unsolicited | ✓ | ✓ | ✓ | ✓ listen-only (`resourcesListChanged`) | `schema/2026-07-28/schema.ts:1257` |
| `notifications/resources/updated` | ✓ after subscribe | ✓ | ✓ | ✓ | ✓ listen-only (`resourceSubscriptions`) | `schema/2026-07-28/schema.ts:1429` |
| `notifications/roots/list_changed` | ✓ | ✓ | ✓ | ✓ | rem | `schema/2025-11-25/schema.ts:2146` |
| `notifications/elicitation/complete` | — | — | — | ✓ | rem | `schema/2025-11-25/schema.ts:2494` |
| `notifications/tasks/status` | — | — | — | ✓ | rem | `schema/2025-11-25/schema.ts:1496` |
| `notifications/tasks` | — | — | — | — | ext (listen, `taskIds`) | `seps/2663-tasks-extension.md:422` |
| `notifications/subscriptions/acknowledged` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:1399` |

## 4. Capability fields

| capability | 24-11 | 25-03 | 25-06 | 25-11 | 26-07 | source |
| --- | --- | --- | --- | --- | --- | --- |
| client `experimental` | ✓ | ✓ | ✓ | ✓ | ✓ (`JSONObject`) | `schema/2026-07-28/schema.ts:720` |
| client `roots` | ✓ | ✓ | ✓ | ✓ | dep (`{}`) | `schema/2026-07-28/schema.ts:732` |
| client `roots.listChanged` | ✓ | ✓ | ✓ | ✓ | rem | `schema/2025-11-25/schema.ts:322` |
| client `sampling` | ✓ | ✓ | ✓ | ✓ | dep | `schema/2026-07-28/schema.ts:749` |
| client `sampling.context` | — | — | — | ✓ (soft-dep gate) | dep | `schema/2026-07-28/schema.ts:754` |
| client `sampling.tools` | — | — | — | ✓ | dep | `schema/2026-07-28/schema.ts:758` |
| client `elicitation` | — | — | ✓ | ✓ | ✓ | `schema/2026-07-28/schema.ts:769` |
| client `elicitation.form` | — | — | — | ✓ | ✓ | `schema/2026-07-28/schema.ts:770` |
| client `elicitation.url` | — | — | — | ✓ | ✓ | `schema/2026-07-28/schema.ts:771` |
| client `tasks` (list/cancel/requests.sampling.createMessage/requests.elicitation.create) | — | — | — | ✓ | rem | `schema/2025-11-25/schema.ts:346` |
| client `extensions` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:785` |
| server `experimental` | ✓ | ✓ | ✓ | ✓ | ✓ | `schema/2026-07-28/schema.ts:797` |
| server `logging` | ✓ | ✓ | ✓ | ✓ | dep | `schema/2026-07-28/schema.ts:808` |
| server `completions` | — | ✓ | ✓ | ✓ | ✓ | `schema/2026-07-28/schema.ts:815` |
| server `prompts.listChanged` | ✓ | ✓ | ✓ | ✓ | ✓ (listen) | `schema/2026-07-28/schema.ts:829` |
| server `resources.subscribe` | ✓ | ✓ | ✓ | ✓ | ✓ (now = `resourceSubscriptions`) | `schema/2026-07-28/schema.ts:850` |
| server `resources.listChanged` | ✓ | ✓ | ✓ | ✓ | ✓ (listen) | `schema/2026-07-28/schema.ts:854` |
| server `tools.listChanged` | ✓ | ✓ | ✓ | ✓ | ✓ (listen) | `schema/2026-07-28/schema.ts:869` |
| server `tasks.list` | — | — | — | ✓ | rem | `schema/2025-11-25/schema.ts:437` |
| server `tasks.cancel` | — | — | — | ✓ | rem (ext implies cancel) | `schema/2025-11-25/schema.ts:441` |
| server `tasks.requests.tools.call` | — | — | — | ✓ | rem | `schema/2025-11-25/schema.ts:453` |
| server `extensions` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:882` |
| `extensions["io.modelcontextprotocol/tasks"]` (both sides) | — | — | — | — | ext | `seps/2663-tasks-extension.md:47` |

## 5. Content types

| content type | 24-11 | 25-03 | 25-06 | 25-11 | 26-07 | source |
| --- | --- | --- | --- | --- | --- | --- |
| `text` | ✓ | ✓ | ✓ (+`_meta`) | ✓ | ✓ | `schema/2024-11-05/schema.ts:846` |
| `image` | ✓ | ✓ | ✓ (+`_meta`) | ✓ | ✓ | `schema/2024-11-05/schema.ts:857` |
| `audio` | — | ✓ | ✓ | ✓ | ✓ | `schema/2025-03-26/schema.ts:991` |
| `resource` (embedded) | ✓ | ✓ | ✓ | ✓ | ✓ | `schema/2024-11-05/schema.ts:617` |
| `resource_link` | — | — | ✓ | ✓ | ✓ | `schema/2025-06-18/schema.ts:761` |
| `tool_use` (sampling only) | — | — | — | ✓ | dep | `schema/2025-11-25/schema.ts:1835` |
| `tool_result` (sampling only) | — | — | — | ✓ | dep | `schema/2025-11-25/schema.ts:1869` |

### 5.1 Where each content type may appear

T = text, I = image, A = audio, R = embedded `resource`, L = `resource_link`, U = `tool_use`, X = `tool_result`.

| location | 24-11 | 25-03 | 25-06 | 25-11 | 26-07 | source (latest rev with that shape) |
| --- | --- | --- | --- | --- | --- | --- |
| `CallToolResult.content[]` | T I R | T I A R | T I A R L | T I A R L | T I A R L | `schema/2026-07-28/schema.ts:1813` |
| `PromptMessage.content` (single block) | T I R | T I A R | T I A R L | T I A R L | T I A R L | `schema/2026-07-28/schema.ts:1706` |
| `SamplingMessage.content` (request messages and `CreateMessageResult`) | T I (single) | T I A (single) | T I A (single) | T I A U X (single or array) | T I A U X (dep) | `schema/2026-07-28/schema.ts:2258` |
| `ToolResultContent.content[]` (nested in sampling) | — | — | — | T I A R L | T I A R L (dep) | `schema/2026-07-28/schema.ts:2461` |
| `ReadResourceResult.contents[]` | Text/Blob*ResourceContents* (not content blocks) | same | same (+`_meta`) | same | same (+cacheable) | `schema/2026-07-28/schema.ts:1230` |

Per-revision sources for the non-2026 cells: `schema/2024-11-05/schema.ts:656` “content: (TextContent | ImageContent | EmbeddedResource)[];”; `schema/2025-03-26/schema.ts:699` “content: (TextContent | ImageContent | AudioContent | EmbeddedResource)[];”; `schema/2025-06-18/schema.ts:823` “content: ContentBlock[];”; `schema/2024-11-05/schema.ts:607` “content: TextContent | ImageContent | EmbeddedResource;”; `schema/2025-03-26/schema.ts:645` “content: TextContent | ImageContent | AudioContent | EmbeddedResource;”; `schema/2025-06-18/schema.ts:750` “content: ContentBlock;”; `schema/2024-11-05/schema.ts:812` “content: TextContent | ImageContent;”; `schema/2025-03-26/schema.ts:918` “content: TextContent | ImageContent | AudioContent;”; `schema/2025-11-25/schema.ts:1683` “content: SamplingMessageContentBlock | SamplingMessageContentBlock[];”; `schema/2025-11-25/schema.ts:1884` “content: ContentBlock[];”

Annotations (`audience`, `priority`, and from 2025-06-18 `lastModified`) are allowed on T, I, A, R, L (L via `Resource`) in every revision the type exists; never on U or X. `_meta` is on T, I, A, R, L from 2025-06-18 and on U, X from their introduction.

## 6. Selected fields and types

| field / type | 24-11 | 25-03 | 25-06 | 25-11 | 26-07 | source |
| --- | --- | --- | --- | --- | --- | --- |
| `Tool.annotations` (4 hints + title) | — | ✓ | ✓ | ✓ | ✓ | `schema/2025-03-26/schema.ts:809` |
| `Tool.title` / `BaseMetadata.title` | — | — | ✓ | ✓ | ✓ | `schema/2025-06-18/schema.ts:930` |
| `Tool.outputSchema` | — | — | ✓ (object root) | ✓ (object root) | ✓ (any schema) | `schema/2025-06-18/schema.ts:951` |
| `CallToolResult.structuredContent` | — | — | ✓ object | ✓ object | ✓ any JSON | `schema/2026-07-28/schema.ts:1821` |
| `icons` (Tool, Resource, ResourceTemplate, Prompt, Implementation) | — | — | — | ✓ | ✓ | `schema/2025-11-25/schema.ts:1249` |
| `Tool.execution.taskSupport` | — | — | — | ✓ | rem | `schema/2025-11-25/schema.ts:1270` |
| `Annotations.lastModified` | — | — | ✓ | ✓ | ✓ | `schema/2025-06-18/schema.ts:1127` |
| `CompleteRequest…context.arguments` | — | — | ✓ | ✓ | ✓ | `schema/2025-06-18/schema.ts:1333` |
| `Result.resultType` (required) | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:234` |
| `InputRequiredResult` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:584` |
| `CacheableResult` (`ttlMs`, `cacheScope`) | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:1081` |
| `_meta` `io.modelcontextprotocol/logLevel` | — | — | — | — | dep | `schema/2026-07-28/schema.ts:110` |
| `_meta` `io.modelcontextprotocol/related-task` | — | — | — | ✓ | rem | `schema/2025-11-25/schema.ts:1332` |
| `URLElicitationRequiredError` `-32042` | — | — | — | ✓ | rem (reserved) | `schema/2025-11-25/schema.ts:181` |
| `MissingRequiredClientCapabilityError` `-32021` | — | — | — | — | ✓ | `schema/2026-07-28/schema.ts:442` |

## 7. Error codes

| Code | Name / meaning | 2024-11-05 | 2025-03-26 | 2025-06-18 | 2025-11-25 | 2026-07-28 | Source |
|---|---|---|---|---|---|---|---|
| -32700 | Parse error | ✓ | ✓ | ✓ | ✓ | ✓ (`ParseError`) | `schema/2026-07-28/schema.ts:312` |
| -32600 | Invalid Request | ✓ | ✓ | ✓ | ✓ (+task-required misuse) | ✓ | `2025-11-25/basic/utilities/tasks.mdx:775` |
| -32601 | Method not found (also: server capability not advertised) | ✓ | ✓ | ✓ | ✓ (+task-required tool not run as task) | ✓ + HTTP 404 | `2026-07-28/basic/transports/streamable-http.mdx:271` |
| -32602 | Invalid params: unknown tool/prompt, bad cursor, bad log level; legacy `initialize` version error example; 26-07 also missing `_meta` fields and resource-not-found | ✓ | ✓ | ✓ | ✓ | ✓ (expanded) | `2026-07-28/basic/index.mdx:381` |
| -32603 | Internal error | ✓ | ✓ | ✓ | ✓ | ✓ | `schema/2024-11-05/schema.ts:84` |
| -32002 | Resource not found | ✓ | ✓ | ✓ | ✓ | MUST NOT emit; clients SHOULD accept | `2026-07-28/basic/index.mdx:140` |
| -32042 | URL elicitation required | — | — | — | ✓ | MUST NOT emit | `schema/2025-11-25/schema.ts:181` |
| -32020 | HeaderMismatch (headers ≠ body, or missing/malformed) | — | — | — | — | ✓ (HTTP 400) | `schema/2026-07-28/schema.ts:434` |
| -32021 | MissingRequiredClientCapability (`data.requiredCapabilities`) | — | — | — | — | ✓ (HTTP 400) | `schema/2026-07-28/schema.ts:442` |
| -32022 | UnsupportedProtocolVersion (`data.supported`, `data.requested`) | — | — | — | — | ✓ (HTTP 400) | `schema/2026-07-28/schema.ts:450` |
| -32000..-32019 | Legacy implementation-defined; receivers assume nothing (except -32002) | (unreserved) | (unreserved) | (unreserved) | (unreserved) | policy | `2026-07-28/basic/index.mdx:117` |
| -32020..-32099 | Reserved for the MCP spec | — | — | — | — | policy | `2026-07-28/basic/index.mdx:122` |
| -32001 / -32003 / -32004 | Pre-release draft numbers for HeaderMismatch / MissingRequiredClientCapability / UnsupportedProtocolVersion; never in a release | — | — | — | — | renumbered | `2026-07-28/changelog.mdx:65` |
| (HTTP) 400 | legacy: invalid/unsupported `MCP-Protocol-Version`, missing required session; 26-07: -32602 meta / -32020 / -32021 / -32022 | — | ✓ (session) | ✓ | ✓ | ✓ | `2025-06-18/basic/transports.mdx:256` |
| (HTTP) 403 | invalid `Origin` (explicit from 25-11); auth insufficient scope | — | ✓ (auth) | ✓ (auth) | ✓ | ✓ | `2025-11-25/basic/transports.mdx:79` |
| (HTTP) 404 | legacy: expired/unknown session ⇒ re-initialize; 26-07: unknown method + -32601 | — | ✓ | ✓ | ✓ | ✓ (changed meaning) | `2025-03-26/basic/transports.mdx:201` |
| (HTTP) 405 | GET/DELETE not offered | — | ✓ | ✓ | ✓ | ✓ (SHOULD for GET/DELETE) | `2026-07-28/basic/transports/streamable-http.mdx:683` |

## 8. HTTP headers

| Header | Dir | 2024-11-05 | 2025-03-26 | 2025-06-18 | 2025-11-25 | 2026-07-28 | Source |
|---|---|---|---|---|---|---|---|
| `Origin` (validated) | C→S | MUST validate | MUST | MUST | MUST; invalid ⇒ 403 | MUST; invalid ⇒ 403 | `2026-07-28/basic/transports/streamable-http.mdx:58` |
| `Accept: application/json, text/event-stream` | C→S | — | MUST (POST); `text/event-stream` (GET) | MUST | MUST | MUST (POST) | `2026-07-28/basic/transports/streamable-http.mdx:76` |
| `Content-Type` (response `application/json` \| `text/event-stream`) | S→C | SSE only | MUST pick one | ✓ | ✓ | ✓ | `2026-07-28/basic/transports/streamable-http.mdx:89` |
| `Mcp-Session-Id` | both | — | MAY mint; MUST echo | same | same (`MCP-Session-Id`) | removed; ignore, don't mint | `2025-03-26/basic/transports.mdx:187` |
| `MCP-Protocol-Version` | C→S | — | SHOULD during OAuth metadata discovery only | MUST after init; absent ⇒ assume 2025-03-26 | same | MUST every POST; = `_meta` else -32020 | `2026-07-28/basic/transports/streamable-http.mdx:252` |
| `Last-Event-ID` | C→S | — | resume via GET | same | same; always via GET | removed; ignore | `2025-11-25/basic/transports.mdx:179` |
| `Mcp-Method` | C→S | — | — | — | — | REQUIRED all requests | `2026-07-28/basic/transports/streamable-http.mdx:290` |
| `Mcp-Name` | C→S | — | — | — | — | REQUIRED tools/call, resources/read, prompts/get | `2026-07-28/basic/transports/streamable-http.mdx:291` |
| `Mcp-Param-{Name}` (from `x-mcp-header`) | C→S | — | — | — | — | clients MUST mirror | `2026-07-28/basic/transports/streamable-http.mdx:374` |
| `X-Accel-Buffering: no` | S→C | — | — | — | — | SHOULD on SSE | `2026-07-28/basic/transports/streamable-http.mdx:136` |
| `Authorization: Bearer` | C→S | — | MUST (when auth) | MUST | MUST | MUST | `2026-07-28/basic/authorization/index.mdx:262` |
| `WWW-Authenticate` (`resource_metadata`, `scope`, `error="insufficient_scope"`) | S→C | — | (401 only) | MUST on 401 | header or well-known; `scope` SHOULD | same | `2025-11-25/basic/authorization.mdx:95` |
| SSE `id:` / `retry:` fields (not headers) | S→C | — | `id` MAY | same | priming event SHOULD; `retry` MUST be honoured | removed | `2025-11-25/basic/transports.mdx:115` |
