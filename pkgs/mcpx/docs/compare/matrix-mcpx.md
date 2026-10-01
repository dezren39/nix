# mcpx × revisions, both directions

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare, area:protocol
description:  what mcpx does for each protocol feature, serving each
              revision and consuming each era, at one named commit, in a
              shape whose status cells can be refreshed row by row.
```

**Status as of `05c78b2`** (`origin/main`, "mcpx: stop leaking daemons, restore
the CI cache, and add a Containerfile (#182)"). The protocol work in progress
at the time of writing changes several rows. Refresh a row by re-reading the
code cited in it and editing its cells, then change the commit above.
`GET /v1/protocol` reports which revision defines which feature and what
each upstream negotiated. It does not report whether mcpx implements a feature
correctly, and nothing generates that side yet.

Columns. **srv→<rev>**: a host of that revision connected to mcpx.
**cli→legacy**: mcpx connected to an upstream speaking 2025-03-26 to
2025-11-25 (mcpx offers 2025-11-25). **cli→modern**: mcpx connected to a
2026-07-28 upstream. The register column links the area file holding the
individual differences, with value, effort and risk.

Legend: ✓ works as the revision defines · partial (the cell says how) · ✗
missing or broken · n/a not defined for that peer · acc. accepted beyond the
revision (accept-liberally, [`../protocol.md`](../protocol.md)). Evidence marked
W*, M*, S* comes from the wire session described in
[stateless.md](stateless.md#evidence).

| # | feature | srv→2025-03-26 | srv→2025-06-18 | srv→2025-11-25 | srv→2026-07-28 | cli→legacy | cli→modern | code (row) | register |
|---|---|---|---|---|---|---|---|---|---|
| 1 | version handshake / negotiation | ✓ (2024-11-05 & future refused) | ✓ | ✓ | partial: `_meta` honoured; wrong `requested`, HTTP 200 | partial: always offers 2025-11-25, accepts anything | ✗ discover field name; -32022 on init aborts | `internal/mcpserver/server.go:844-859`; `internal/mcpclient/client.go:202-325` | [lifecycle-versioning](register/lifecycle-versioning.md) |
| 2 | `server/discover` | acc. | acc. | acc. | partial: `protocolVersions`, top-level `serverInfo`, no ttl | n/a | ✗ | `internal/mcpserver/server.go:529-538` | [lifecycle-versioning](register/lifecycle-versioning.md), [pagination-caching](register/pagination-caching.md) |
| 3 | `resultType` on results | stripped ✓ | stripped ✓ | stripped ✓ | ✓ (task handle wrong) | n/a | ✓ read (`input_required` detection) | `internal/mcpserver/server.go:432-436`; `internal/mcpserver/revisions.go:163-168`; `internal/mcpclient/client.go:663-665` | [lifecycle-versioning](register/lifecycle-versioning.md) |
| 4 | result `_meta` serverInfo | n/a | n/a | n/a | ✗ | n/a | ✗ not read | `internal/mcpserver/server.go:432-436` | [lifecycle-versioning](register/lifecycle-versioning.md) |
| 5 | stdio framing | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | `internal/mcpserver/server.go:1000-1073`; `internal/mcpclient/stdio.go:112-145` | [transports](register/transports.md) |
| 6 | stdio concurrency | partial (in-line) | partial | partial | partial | ✓ id-multiplexed | ✓ | `internal/mcpserver/server.go:1051-1067` | [transports](register/transports.md) |
| 7 | batching | ✗ | n/a | n/a | n/a | partial: HTTP split, stdio dropped | n/a | `internal/mcpserver/server.go:1041-1044`; `internal/mcpclient/http.go:197-210` | [transports](register/transports.md) |
| 8 | HTTP POST JSON/SSE | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | `internal/mcpserver/server.go:1118-1193`; `internal/mcpclient/http.go:97-162` | — |
| 9 | HTTP GET stream | ✓ 405 | ✓ 405 | ✓ 405 | ✓ 405 | ✗ never opened | n/a | `internal/mcpserver/server.go:1124-1131` | [transports](register/transports.md) |
| 10 | `Mcp-Session-Id` | partial: issued on failed init; no 404 | partial | partial | ✗ minted on discover | partial: no re-init on 404 | ✗ still sent | `internal/mcpserver/server.go:1208-1230`; `internal/mcpclient/http.go:89-93`, `internal/mcpclient/http.go:126-130` | [transports](register/transports.md), [lifecycle-versioning](register/lifecycle-versioning.md) |
| 11 | `MCP-Protocol-Version` header | n/a | partial: not validated or used | partial | partial: mismatch 400 but not -32020 | ✗ fixed 2025-11-25 | ✓ from `_meta` | `internal/mcpserver/server.go:1161-1168`; `internal/mcpclient/http.go:81-85` | [transports](register/transports.md), [lifecycle-versioning](register/lifecycle-versioning.md) |
| 12 | `Mcp-Method` / `Mcp-Name` | n/a | n/a | n/a | ✗ not checked | n/a | ✗ not sent | — | [transports](register/transports.md) |
| 13 | HTTP error statuses (400/404) | ✓ (200 is legacy-correct) | ✓ | ✓ | ✗ | n/a | partial: body inspected by substring | `internal/mcpserver/server.go:1192`; `internal/mcpclient/client.go:329-331` | [transports](register/transports.md) |
| 14 | Origin validation | ✗ | ✗ | ✗ | ✗ | n/a | n/a | — | [transports](register/transports.md) |
| 15 | legacy HTTP+SSE (2024-11-05) | n/a | n/a | n/a | n/a | ✗ | n/a | `internal/pool/pool.go:368-381` | [transports](register/transports.md) |
| 16 | capabilities declared | partial: stdio listChanged never delivered | partial | partial: + `extensions` leak, `tasks.list` | partial: + core `tasks` leak | partial: sampling always, roots empty | partial | `internal/mcpserver/server.go:1389-1420`; `internal/mcpclient/modern.go:46-58` | [capabilities](register/capabilities.md), [lifecycle-versioning](register/lifecycle-versioning.md) |
| 17 | `ping` | ✓ | ✓ | ✓ | acc. | ✓ answered | ✓ answered | `internal/mcpserver/server.go:543`; `internal/mcpclient/modern.go:110-115` | [lifecycle-versioning](register/lifecycle-versioning.md) |
| 18 | `tools/list` | ✓ (61 tools) | ✓ | ✓ | partial: no ttlMs/cacheScope | ✓ paginated | ✗ unreachable | `internal/mcpserver/server.go:546-556`; `internal/mcpclient/client.go:731-750` | [tools](register/tools.md), [pagination-caching](register/pagination-caching.md), [lifecycle-versioning](register/lifecycle-versioning.md) |
| 19 | `tools/call` via `mcpx_call` | partial: isError lost, text only | partial | partial | partial | ✓ raw result kept in /v1 | ✗ unreachable | `internal/cli/serve.go:103-117`; `internal/cli/commands.go:385-415` | [tools](register/tools.md) |
| 20 | `resources/list` | ✓ (namespaced URIs) | ✓ | ✓ | partial | ✓ | ✗ | `internal/mcpserver/server.go:683-699`; `internal/cli/serve.go:473-497` | [resources](register/resources.md), [pagination-caching](register/pagination-caching.md) |
| 21 | `resources/templates/list` | ✗ `uri` not `uriTemplate` | ✗ | ✗ | ✗ | ✓ reads `uriTemplate` | ✗ | `internal/mcpserver/server.go:574-593` | [resources](register/resources.md) |
| 22 | `resources/read` | partial: -32602 not -32002; blob as text | partial | partial | partial: code ✓, no ttl, internal→-32602 | ✓ | ✗ | `internal/mcpserver/server.go:701-722` | [resources](register/resources.md) |
| 23 | resources subscribe / updated | partial: stdio only, never upstream, wrong URI | partial | partial | partial (via listen) | ✗ never subscribes | ✗ no listen | `internal/mcpserver/server.go:595-620`; `internal/mcpclient/client.go:536-543` | [resources](register/resources.md), [notifications](register/notifications.md) |
| 24 | `prompts/list` | partial: `title` leak | ✓ | ✓ | partial | ✓ | ✗ | `internal/mcpserver/server.go:724-740` | [prompts](register/prompts.md), [pagination-caching](register/pagination-caching.md) |
| 25 | `prompts/get` | partial: flattened | partial | partial | partial | ✓ | ✗ | `internal/mcpserver/server.go:742-764`; `internal/cli/serve.go:640-669` | [prompts](register/prompts.md) |
| 26 | `completion/complete` | partial: ref ignored | partial | partial | partial | ✓ `Complete` if declared | ✗ | `internal/mcpserver/server.go:1555-1599`; `internal/pool/request.go:60-70` | [completion](register/completion.md) |
| 27 | logging (`setLevel`, `notifications/message`) | acc. no-op, never emits | acc. | acc. | acc. (removed); `logLevel` never read | partial: setLevel info; logs → event bus | ✗ uses removed method | `internal/mcpserver/server.go:565-572`; `internal/daemon/hooks.go:29-33` | [logging](register/logging.md) |
| 28 | elicitation (server→host) | n/a (broker) | ✓ form, SSE/stdio | ✓ form+url | partial: needs session; stateless → -32603 | n/a | n/a | `internal/mcpserver/ask.go:122-233`; `internal/mcpserver/conn.go:191-208` | [elicitation](register/elicitation.md) |
| 29 | elicitation (upstream→mcpx) | n/a | ✓ via broker | ✓ incl. url, complete notif | n/a | ✓ (string ids dropped) | ✓ `input_required` (unreachable until ) | `internal/daemon/hooks.go:122-170`; `internal/mcpclient/modern.go:156-189` | [transports](register/transports.md), [lifecycle-versioning](register/lifecycle-versioning.md) |
| 30 | sampling (server→host) | partial: no revision or tools gating | partial | partial | partial | n/a | n/a | `internal/mcpserver/conn.go:211`; `internal/mcpserver/ask.go:256-266` | [sampling](register/sampling.md) |
| 31 | sampling (upstream→mcpx) | n/a | n/a | n/a | n/a | partial: always declared, broker only | partial | `internal/daemon/hooks.go:180-232` | [capabilities](register/capabilities.md), [sampling](register/sampling.md) |
| 32 | roots | ✗ never asks host; notif gets error | ✗ | ✗ | ✗ | partial: declared, empty | partial | `internal/daemon/server.go:176`; `internal/mcpclient/modern.go:116-120` | [roots](register/roots.md), [capabilities](register/capabilities.md) |
| 33 | tasks (server) | acc. | acc. | partial: no taskSupport, cancel-terminal, no binding, no related-task | ✗ wrong shape, no `tasks/update` | n/a | n/a | `internal/mcpserver/server.go:474-501`; `internal/mcpserver/tasks.go` | [capabilities](register/capabilities.md), [tasks](register/tasks.md) |
| 34 | tasks (client) | n/a | n/a | n/a | n/a | ✗ never task-augments | ✗ extension not declared | `internal/mcpclient/client.go:845-858` | [tools](register/tools.md) |
| 35 | `subscriptions/listen` | acc. (stdio) | acc. | acc. | partial: stdio wrong ack, no subscriptionId; HTTP 202 | n/a | ✗ never opened | `internal/mcpserver/server.go:622-640` | [notifications](register/notifications.md), [transports](register/transports.md) |
| 36 | notifications (list_changed) to host | ✗ stdio declared, never sent | ✗ | ✗ | partial: stdio via listen only | — | — | `internal/mcpserver/server.go:1389-1402` | [capabilities](register/capabilities.md) |
| 37 | notifications from host | partial: unknown ones get an error reply | partial | partial | partial | ✓ all known handled | ✓ | `internal/mcpserver/server.go:766`; `internal/mcpclient/client.go:473-522` | [notifications](register/notifications.md) |
| 38 | cancellation (host→mcpx→upstream) | partial: recorded only on stdio; HTTP disconnect propagates | partial | partial | partial | ✓ sends `notifications/cancelled` ("timeout") | partial | `internal/mcpserver/server.go:1521-1538`; `internal/mcpclient/client.go:709-716` | [progress-cancellation](register/progress-cancellation.md) |
| 39 | progress | ✗ | ✗ | ✗ | ✗ | partial: relayed to the host under its token (#212); does not reset mcpx's own timeout | same | `internal/mcpserver/ask.go:95-108`; `internal/daemon/hooks.go:34-36` | [progress-cancellation](register/progress-cancellation.md) |
| 40 | `_meta` passthrough (trace, sessionID) | ✗ | ✗ | ✗ | ✗ | ✗ never sent | ✗ | `internal/mcpserver/ask.go:95-108` | [meta](register/meta.md) |
| 41 | pagination | partial: bad cursor→0 | partial | partial | partial | ✓ ≤100 pages | ✗ | `internal/mcpserver/server.go:1477-1513` | [pagination-caching](register/pagination-caching.md) |
| 42 | content downgrade | ✓ resource_link→resource ( mime) | ✓ untouched | ✓ | ✓ untouched | n/a | n/a | `internal/mcpserver/revisions.go:132-289` | [content-types](register/content-types.md) |
| 43 | auth to upstream | n/a | n/a | n/a | n/a | partial: `headers` only; `auth` inert; no OAuth | same | `internal/pool/pool.go:377-380` | [auth](register/auth.md) |
| 44 | auth on /mcp | ✗ | ✗ | ✗ | ✗ | n/a | n/a | `internal/cli/root.go:196-204` | [auth](register/auth.md) |

## Reading it

- **The legacy server columns are mostly ✓ or partial.** The partials are
  lossy rendering (`mcpx_call`, `prompts/get`, `resources/read`) and
  declarations with nothing behind them. None of them stops a legacy host
  from working.
- **The modern server column is partial almost everywhere.** mcpx's own client
  works against it, but a conformant 2026 client does not: see
  [stateless.md](stateless.md).
- **The modern client column is ✗ almost everywhere.** One field name in
  discovery stops everything behind it. The rows are ✗ because they are
  unreachable, not because each is broken on its own, and several (MRTR, `_meta`) are
  implemented and would work once discovery does.
