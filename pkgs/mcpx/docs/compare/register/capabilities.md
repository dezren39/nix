# Capabilities

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  every ClientCapabilities/ServerCapabilities field per revision; what mcpx, opencode and lootbox declare
```

A capability is a promise about the revision in force, and mcpx makes it twice: as a server to each host and as a
client to each upstream. At `05c78b2` the largest gaps are declarations without delivery (stdio `listChanged` and
`subscribe`, `tasks.requests.tools.call`, upstream `sampling`) and shapes sent to revisions that do not define them (core
`tasks` to 2026 hosts, `extensions` to 2025-11-25 hosts, `roots.listChanged` to 2026 upstreams). How capabilities travel
(`initialize`, `server/discover`, the 2026 per-request `_meta` key, `Implementation` fields) is in the lifecycle and
`_meta` registers, and the `-32021` missing-capability error is in the errors register.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| CAP-01 | `experimental` map (client and server); 2026 narrows values to JSON objects | `2024-11-05 has` `2026-07-28 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (JSONObject)` | n/a — declared in neither direction | + low | S | low |
| CAP-02 | client `roots`; deprecated to a bare `{}` in 2026 | `2024-11-05 has` `2026-07-28 deprecates` `mcpx missing` `opencode v1 has` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 dep · opencode v1 ✓ v2 ✓ · lootbox —` | partial — declared to every upstream; always answers an empty list (#210) | + med | S | med |
| CAP-03 | client `roots.listChanged` removed in 2026 | `2024-11-05 has` `2026-07-28 removes` `mcpx missing` `2026-07-28 deprecates` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 rem · opencode —` | ✗ — `listChanged:false` sent to 2026 upstreams too (#200) | + low | S | low |
| CAP-04 | client `sampling`; structured in 2025-11-25, deprecated in 2026 | `2024-11-05 has` `2025-11-25 has` `2026-07-28 deprecates` `mcpx missing` `mcpx has, others don't` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 dep · opencode v1 — v2 — · lootbox —` | partial — declared to every upstream; only a broker agent answers (#210) | + med | S | med |
| CAP-05 | client `sampling.context` gates `includeContext` | `2025-11-25 has` `2026-07-28 deprecates` | `24-11 — · 25-03 — · 25-06 — · 25-11 ✓ · 26-07 dep` | ✓ — never declared, correct for a proxy | + low | S | low |
| CAP-06 | client `sampling.tools` gates tool use inside sampling | `2025-11-25 has` `2026-07-28 deprecates` | `24-11 — · 25-03 — · 25-06 — · 25-11 ✓ · 26-07 dep` | ✓ — never declared upstream | − deprecated | M | low |
| CAP-07 | client `elicitation` | `2025-06-18 has` `opencode v2 has` | `24-11 — · 25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ · opencode v1 — v2 ✓ · lootbox —` | ✓ — always declared upstream; host gate checks revision | + high | S | low |
| CAP-08 | client `elicitation.form`; `{}` means form only | `2025-11-25 has` `mcpx missing` `opencode v2 has` | `24-11 — · 25-03 — · 25-06 — · 25-11 ✓ · 26-07 ✓ · opencode v2 ✓ (applyDefaults)` | partial — form gate ignores a url-only declaration (#210) | + low | S | low |
| CAP-09 | client `elicitation.url` | `2025-11-25 has` `mcpx missing` `opencode v2 has` | `24-11 — · 25-03 — · 25-06 — · 25-11 ✓ · 26-07 ✓ · opencode v1 — v2 ✓` | partial — host gate correct; never declared upstream (#200) | + med | M | med |
| CAP-10 | client `tasks` (client-hosted tasks), 2025-11-25 only | `2025-11-25 has` `2026-07-28 removes` `has better replacement` | `24-11 — · 25-03 — · 25-06 — · 25-11 ✓ · 26-07 rem · opencode — · lootbox —` | ✓ — never declared | − one-revision | S | low |
| CAP-11 | client `extensions` map | `2026-07-28 has` `mcpx missing` | `26-07 ✓ only` | ✗ — never declared, so no upstream extension is reachable (#200) | + med | S | low |
| CAP-12 | server `logging`; deprecated, not removed, in 2026 | `2024-11-05 has` `2026-07-28 deprecates` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 dep` | ✓ — never declared; `docs/protocol.md` says otherwise | + low | S | low |
| CAP-13 | server `completions` | `2025-03-26 has` | `24-11 — (method ungated) · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — declared in every era mcpx serves | + low | S | low |
| CAP-14 | server `prompts` | `2024-11-05 has` `opencode v1 has` `opencode v2 has` | `all five ✓ · opencode v1 ✓ v2 ✓ (slash commands)` | ✓ — always declared | + low | S | low |
| CAP-15 | server `prompts.listChanged`; 2026 delivers it only on a listen stream | `2024-11-05 has` `2026-07-28 has` `mcpx missing` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (listen) · opencode v1 — v2 ✓` | ✗ — stdio declares true, legacy hosts never receive it (#208) | + med | S | med |
| CAP-16 | server `resources` | `2024-11-05 has` `opencode v1 has` `opencode v2 has` | `all five ✓ · opencode v1 ✓ (3 model tools) v2 ✓ (TUI only)` | ✓ — always declared, even with no upstream resources | + med | S | med |
| CAP-17 | server `resources.subscribe`; kept in 2026 with a new meaning | `2024-11-05 has` `2026-07-28 has` `specs conflict` `mcpx missing` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (= resourceSubscriptions) · opencode never subscribes` | ✗ — stdio declares true; nothing is subscribed upstream (#208) | + med | M | med |
| CAP-18 | server `resources.listChanged`; 2026 delivers it only on a listen stream | `2024-11-05 has` `2026-07-28 has` `mcpx missing` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (listen) · opencode v1 — v2 ✓` | ✗ — stdio declares true, legacy hosts never receive it (#208) | + med | S | med |
| CAP-19 | server `tools` | `2024-11-05 has` `opencode v1 has` `opencode v2 has` | `all five ✓ · opencode v1 ✓ v2 ✓` | ✓ — always declared | + low | S | low |
| CAP-20 | server `tools.listChanged`; 2026 delivers it only on a listen stream | `2024-11-05 has` `2026-07-28 has` `mcpx missing` `opencode v1 has` `opencode v2 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (listen) · opencode v1 ✓ v2 ✓` | ✗ — stdio declares true; mcpx's own list never changes (#208) | + med | S | med |
| CAP-21 | server core `tasks` object, 2025-11-25 only | `2025-11-25 has` `2026-07-28 removes` `has better replacement` `mcpx missing` | `24-11 — · 25-03 — · 25-06 — · 25-11 ✓ · 26-07 rem (extension)` | ✗ — also sent to 2026 hosts (wire W5) (#209) | + med | S | low |
| CAP-22 | server `tasks.list` | `2025-11-25 has` `2026-07-28 removes` `mcpx missing` | `25-11 ✓ only` | ✗ — declared though tasks are bound to no requestor (#209) | + med | S | med |
| CAP-23 | server `tasks.cancel` | `2025-11-25 has` `2026-07-28 removes` | `25-11 ✓ · 26-07 rem (extension implies cancel)` | partial — declared; terminal-task cancel not rejected (tasks register) | + low | S | low |
| CAP-24 | server `tasks.requests.tools.call` | `2025-11-25 has` `2026-07-28 removes` `mcpx missing` | `25-11 ✓ only` | ✗ — declared, but no tool allows task execution (#209) | + med | S | low |
| CAP-25 | server `extensions` map | `2026-07-28 has` `mcpx missing` | `26-07 ✓ only` | ✗ — also sent to 2025-11-25 hosts (#209) | + low | S | low |
| CAP-26 | `extensions["io.modelcontextprotocol/tasks"]` key, both directions | `2026-07-28 has` `specs conflict` `mcpx missing` | `26-07 ext only (SEP-2663 names 2026-06-30)` | ✗ — advertised to hosts, not implemented; never declared upstream (#209) | + med | M | med |

## CAP-01 `experimental` map (client and server)

- **What.** Both `ClientCapabilities` and `ServerCapabilities` carry `experimental`, a map of non-standard capability
  name to settings object. 2026 retypes the values from TypeScript `object` to `JSONObject`.
- **Where.** Every revision, both sides. Only 2026 constrains the value type.
- **mcpx @ 05c78b2.** Declares no `experimental` key as a server (`internal/mcpserver/server.go:1398-1403`) or as a client
  (`internal/mcpclient/modern.go:50-56`).
- **Value to mcpx.** + none on the wire today; any JSON object already satisfied `object`.
- **Effort.** S — type-level only.
- **Risk.** None practical either way.
- **Detail.** The same `JSONObject` narrowing applies in 2026 to `sampling.context`/`.tools`, `elicitation.form`/`.url`,
  `logging` and `completions`. mcpx forwards nothing from `experimental` in either direction; a host's experimental
  capabilities never reach an upstream.
- **Sources.** `schema/2024-11-05/schema.ts:189` "experimental?: { [key: string]: object };";
  `schema/2026-07-28/schema.ts:720` "experimental?: { [key: string]: JSONObject };"; `schema/2026-07-28/schema.ts:797`
  "experimental?: { [key: string]: JSONObject };"; `internal/mcpclient/modern.go:50-56`

## CAP-02 client `roots`

- **What.** A client that can answer `roots/list` declares `roots`. 2026 keeps the key as a bare `{}` and deprecates it
  (SEP-2577); roots then reach a server only as an input request inside an `InputRequiredResult`.
- **Where.** All five revisions; deprecated in 2026. opencode v1 and v2 declare `roots: {}` and answer one root, the
  instance or Location directory (v2 notes it is legacy-era only). lootbox declares no capabilities at all, so an
  upstream `roots/list` gets the SDK's method-not-found.
- **mcpx @ 05c78b2.** As a client it declares `roots` to every upstream (`internal/mcpclient/modern.go:52`) and answers
  `roots/list` with `{"roots": []}` (`internal/mcpclient/modern.go:116-120`), because the daemon installs nil roots
  (`internal/daemon/server.go:176`) and no setting feeds them. `docs/protocol.md:278` says it answers "the configured
  roots". As a server it never asks a host for roots (roots register).
- **Value to mcpx.** + filesystem servers could scope themselves to the caller's cwd or repo, which the pool's scope key
  already knows; or − stop declaring.
- **Effort.** S either way (derive roots from the call's scope, or drop the capability).
- **Risk.** If not done: servers that refuse to run without a root fail with a confusing message — opencode v2's own
  comment says some legacy servers do exactly that. If done per scope: roots differ per pooled instance, which is the
  intended behaviour.
- **Detail.** An empty list is a legal answer, so this is a doc/intent bug rather than a wire violation. opencode's root is
  the opencode directory, not the worktree. lootbox's `capabilities: {}` comes with no request handler at all, so an
  upstream `roots/list` or `elicitation/create` gets the SDK's default method-not-found.
- **Sources.** `schema/2026-07-28/schema.ts:732` "roots?: {};"; `schema/2026-07-28/schema.ts:724` "@deprecated
  Deprecated as of protocol version 2026-07-28 (SEP-2577)."; `internal/mcpclient/modern.go:52`;
  `internal/daemon/server.go:176`; `docs/protocol.md:278` "the configured roots, before consulting any handler";
  `v1:packages/opencode/src/mcp/index.ts:46` "roots: {},"; `v2:packages/core/src/mcp/client.ts:146`;
  `v2:packages/core/src/mcp/client.ts:144` "Legacy era only"; `.lootbox/src/lib/external-mcps/mcp_client_manager.ts:150-153`

## CAP-03 client `roots.listChanged`

- **What.** 2024-11-05 through 2025-11-25 declare `roots: { listChanged?: boolean }`. 2026 removes the sub-field along
  with `notifications/roots/list_changed`; with per-request roots there is nothing to invalidate.
- **Where.** Four legacy revisions; absent in 2026. opencode v1 and v2 declare `roots` without it.
- **mcpx @ 05c78b2.** Declares `"roots": {"listChanged": false}` in every era, including the 2026
  `io.modelcontextprotocol/clientCapabilities` `_meta` on every modern request (`internal/mcpclient/modern.go:52`,
  `internal/mcpclient/modern.go:83`). A send-conservatively conflict: a key the revision does not define.
- **Value to mcpx.** + dropping it for 2026 is the conservative send; nothing is lost.
- **Effort.** S — one map per era.
- **Risk.** Low: capabilities are an open set, but a strict 2026 server validating `roots` as `{}` could reject it.
- **Detail.** The same object is sent in the legacy `initialize` regardless of the negotiated version. Declaring
  `listChanged: false` in legacy eras is legal but pointless (mcpx never sends `notifications/roots/list_changed`). The
  wire capture of what mcpx declared to an upstream reads
  `{"elicitation":{},"roots":{"listChanged":false},"sampling":{}}` (wire, "mcpx as client").
- **Also recorded from the roots register.** `listChanged` in the four legacy revisions; absent in 2026-07-28.
  Declares `"roots": {"listChanged": false}` in every era, including in 2026 per-request capabilities
  (`internal/mcpclient/modern.go:52`), a key 2026 does not define. opencode v1 and v2 declare `roots: {}`. The removal
  follows from ROOT-02: with MRTR the server asks for roots on every request that needs them, so there is nothing to
  invalidate. The changelog names the notification's removal, not the field's. The wider 2026 per-request capability
  shape is a capabilities-register item.
- **Sources.** `schema/2025-11-25/schema.ts:322` "listChanged?: boolean;"; `schema/2026-07-28/schema.ts:732` "roots?:
  {};"; `internal/mcpclient/modern.go:52`; `internal/mcpclient/modern.go:83` "set(MetaClientCapabilities,
  c.capabilities())"; `schema/2026-07-28/schema.ts:724` "@deprecated Deprecated as of protocol version 2026-07-28 (SEP-2577)."; `v1:packages/opencode/src/mcp/index.ts:46` "roots: {},".

## CAP-04 client `sampling`

- **What.** A client that can run `sampling/createMessage` declares `sampling`. It is an opaque object through 2025-06-18;
  2025-11-25 gives it `context` and `tools` sub-capabilities; 2026 deprecates the whole capability (SEP-2577).
- **Where.** All five; structured from 2025-11-25; deprecated 2026. Neither opencode version declares it or registers a
  handler; lootbox declares nothing.
- **mcpx @ 05c78b2.** As a client, `sampling: {}` is added whenever an `onElicit` handler exists
  (`internal/mcpclient/modern.go:54-56`); the pool always installs one when hooks exist (`internal/pool/pool.go:393-399`)
  and the daemon always installs `Elicit: r.answerServer` (`internal/daemon/hooks.go:69`), so every upstream is told
  sampling works. The only answerer is an agent watching the broker; without a broker `answerServer` returns an error for
  sampling (`internal/daemon/hooks.go:104-111`). `docs/protocol.md:271` says sampling is declared "only when a handler
  exists". As a server, mcpx's gate for asking a host is only "declared `sampling`" (`internal/mcpserver/conn.go:211`).
- **Value to mcpx.** + honesty: servers that have a no-sampling fallback would use it.
- **Effort.** S — declare only when an agent answerer is configured.
- **Risk.** If not done: a sampling upstream waits out `elicit.ttl` (120 s) and then gets an error.
- **Detail.** An empty `sampling: {}` in 2025-11-25+ means "basic sampling, no tools, no context inclusion". A proxy that
  relays sampling should copy the downstream host's sub-capabilities or it silently narrows them. mcpx is the only one of
  mcpx/opencode/lootbox that declares sampling at all. Forwarding sampling to a host without checking its sub-capabilities
  or revision is in the sampling register.
- **Also recorded from the sampling register.** Bare in 2024-11-05..2025-06-18; structured in 2025-11-25 and
  2026-07-28. Declares `sampling: {}` upstream when a handler is installed (`internal/mcpclient/modern.go:54-56`),
  never `tools` or `context`, regardless of what the host behind the call declared. Whether mcpx should declare
  sampling at all is SMP-09.
- **Sources.** `schema/2024-11-05/schema.ts:202` "sampling?: object;"; `schema/2025-11-25/schema.ts:327` "sampling?:
  {"; `schema/2026-07-28/schema.ts:736` "@deprecated Deprecated as of protocol version 2026-07-28 (SEP-2577).";
  `internal/mcpclient/modern.go:54-56`; `internal/daemon/hooks.go:69` "Elicit: r.answerServer,";
  `docs/protocol.md:271` "`sampling` only when a handler exists to answer it"; `v1:packages/opencode/src/mcp/index.ts:42`
  "// sampling: {},"; `v2:packages/core/src/mcp/client.ts:142`; `schema/2025-06-18/schema.ts:246` "sampling?: object;"

## CAP-05 client `sampling.context`

- **What.** Sub-capability that gates `includeContext`: without it, servers SHOULD use only `"none"` or omit the field.
- **Where.** 2025-11-25 and 2026 (deprecated with the values it gates). The 2025-11-25 changelog does not mention it.
- **mcpx @ 05c78b2.** Never declared upstream (`internal/mcpclient/modern.go:55`).
- **Value to mcpx.** + not declaring it is correct: a proxy has no "context from other servers" to inject.
- **Effort.** S.
- **Risk.** None if left undeclared.
- **Detail.** SHOULD-level only: a server may still send `thisServer`/`allServers` and the client MAY ignore it.
- **Sources.** `schema/2025-11-25/schema.ts:332` "context?: object;"; `2025-11-25/client/sampling.mdx:69` "**With
  context inclusion support (soft-deprecated):**"; `internal/mcpclient/modern.go:55`

## CAP-06 client `sampling.tools`

- **What.** Declares that the client accepts `tools`/`toolChoice` in `sampling/createMessage`. Servers MUST NOT send
  tool-enabled sampling without it; the schema also says the client MUST error if it gets one anyway.
- **Where.** 2025-11-25 and 2026 (deprecated).
- **mcpx @ 05c78b2.** Never declared upstream (`internal/mcpclient/modern.go:55`). Downstream, the gate that decides
  whether a host may be asked checks only `sampling` presence (`internal/mcpserver/conn.go:211`); that forwarding gap is
  recorded in the sampling register.
- **Value to mcpx.** − relaying agentic sampling is work for a deprecated feature.
- **Effort.** M — the tool loop (`tool_use`/`tool_result` blocks) has to pass through unmodified.
- **Risk.** Declaring it without a tool loop invites requests mcpx must then refuse.
- **Detail.** Prose and schema put the obligation on different sides: the server MUST NOT send, the client MUST error.
- **Sources.** `schema/2025-11-25/schema.ts:336` "tools?: object;"; `2025-11-25/client/sampling.mdx:40` "Clients
  **MUST** declare support for tool use via the `sampling.tools` capability"; `internal/mcpserver/conn.go:211`
  "func (p Peer) CanSample() bool { return p.Declared("

## CAP-07 client `elicitation`

- **What.** A client that can answer `elicitation/create` declares `elicitation`. Opaque in 2025-06-18; structured into
  modes from 2025-11-25 (CAP-08, CAP-09).
- **Where.** 2025-06-18, 2025-11-25, 2026. opencode v1 has it commented out (and answers `elicitation/create` with
  `-32601`); opencode v2 declares it with both modes; lootbox declares nothing.
- **mcpx @ 05c78b2.** Always declares `"elicitation": {}` upstream (`internal/mcpclient/modern.go:51`); when nobody is
  listening it answers `cancel` (`internal/mcpclient/modern.go:129-131`), which keeps the always-on declaration honest.
  Asking a host requires the host to be at 2025-06-18 or later and to have declared it
  (`internal/mcpserver/conn.go:192`), so mcpx never sends `elicitation/create` to opencode v1.
- **Value to mcpx.** + the broker is built on it.
- **Effort.** S.
- **Risk.** None.
- **Detail.** For opencode v1 users the broker (`mcpx elicit`) is the only way to answer mcpx's questions. opencode v2's
  handler turns the request into a Location-global form (elicitation register).
- **Sources.** `schema/2025-06-18/schema.ts:250` "elicitation?: object;"; `2025-06-18/basic/lifecycle.mdx:155` "|
  Client   | `elicitation`  |"; `internal/mcpclient/modern.go:51`; `internal/mcpclient/modern.go:129-131`;
  `internal/mcpserver/conn.go:192`; `v1:packages/opencode/src/mcp/index.ts:44` "// elicitation: {},";
  `v2:packages/core/src/mcp/client.ts:143`

## CAP-08 client `elicitation.form`

- **What.** 2025-11-25 splits elicitation into `form` and `url`. An empty `elicitation: {}` is equivalent to
  `{ form: {} }`; a declaring client MUST support at least one mode; servers MUST NOT send an undeclared mode.
- **Where.** 2025-11-25 and 2026. opencode v2 declares `form: { applyDefaults: true }`.
- **mcpx @ 05c78b2.** As a client it declares bare `{}`, i.e. form only (`internal/mcpclient/modern.go:51`). As a server,
  `CanElicit` returns true for any non-url mode once `elicitation` is declared (`internal/mcpserver/conn.go:192`,
  `internal/mcpserver/conn.go:195`), without looking for a `form` key — so a 2025-11-25+ host that declared
  `{ url: {} }` alone would still be sent a form request.
- **Value to mcpx.** + low: url-only hosts are rare, but the rule is a MUST NOT.
- **Effort.** S — treat "`form` present, or neither key present" as form support.
- **Risk.** A url-only host receives a `requestedSchema` it cannot render.
- **Detail.** `applyDefaults` is not a schema field (`form` is an open object) — it is the SDK's instruction to fill
  schema defaults into accepted content. Because `{}` means form-only, mcpx's upstream declaration never admits url mode
  (CAP-09).
- **Also recorded from the elicitation register.** 2025-11-25 and 2026-07-28. opencode v2 declares `{ form: {
  applyDefaults: true }, url: {} }`. Downstream, url mode needs ≥2025-11-25 and an explicit `url` key
  (`internal/mcpserver/conn.go:198-207`). Upstream, mcpx declares bare `{}` (`internal/mcpclient/modern.go:51`), which
  is form-only, so no upstream may send it a URL elicitation even when the host behind the call could open one. The
  2026 per-request capabilities carry the same legacy-shaped object (see the capabilities register). Found while
  drafting: `CanElicit` treats any declared `elicitation` as form-capable (`internal/mcpserver/conn.go:192-197`), so a
  host that declared only `{ url: {} }` would still be sent form questions, which the MUST NOT forbids. No host in
  this study declares url-only.
- **Sources.** `schema/2025-11-25/schema.ts:341` "elicitation?: { form?: object; url?: object };";
  `2025-11-25/client/elicitation.mdx:65` "an empty capabilities object is equivalent to declaring support for `form`
  mode only"; `2025-11-25/client/elicitation.mdx:75`; `2025-11-25/client/elicitation.mdx:77` "Servers **MUST NOT**
  send elicitation requests with modes that are not supported by the client."; `internal/mcpserver/conn.go:195`
  "if mode != "; `v2:packages/core/src/mcp/client.ts:143`; `internal/mcpserver/conn.go:198-207`; `internal/mcpclient/modern.go:51`

## CAP-09 client `elicitation.url`

- **What.** Declares support for URL-mode elicitation (the server hands the user a URL to visit; used for OAuth and
  payment flows).
- **Where.** 2025-11-25 and 2026. opencode v2 declares `url: {}` and also handles `notifications/elicitation/complete`;
  opencode v1 has no elicitation at all.
- **mcpx @ 05c78b2.** As a server the gate is right: url mode needs a 2025-11-25+ host that declared an explicit `url`
  key (`internal/mcpserver/conn.go:198-207`). As a client mcpx declares bare `{}` upstream
  (`internal/mcpclient/modern.go:51`), yet its broker accepts `mode: "url"` requests anyway (elicitation register), so a
  conforming upstream never sends one through mcpx even when the attached host (opencode v2) could open it.
- **Value to mcpx.** + OAuth-style and payment flows from remote upstreams would work through mcpx.
- **Effort.** M — the declaration has to reflect whether a URL-capable host is attached; 2026's per-request
  capabilities allow exactly that, legacy `initialize` does not.
- **Risk.** If declared unconditionally: a URL request with no host able to open it strands the user until the
  deadline. If not declared: remote servers needing URL flows cannot be used through mcpx.
- **Detail.** In 2026 mcpx could declare `url` only on requests forwarded on behalf of a URL-capable host; in legacy eras
  the declaration is fixed at `initialize` for the life of the upstream instance, which may be shared by several hosts.
- **Sources.** `schema/2026-07-28/schema.ts:771`; `internal/mcpserver/conn.go:198-207`; `internal/mcpclient/modern.go:51`
  "\"elicitation\": map[string]any{},"; `v2:packages/core/src/mcp/client.ts:143`;
  `v2:packages/core/src/mcp/client.ts:160`

## CAP-10 client `tasks` (client-hosted tasks)

- **What.** 2025-11-25 lets a client host tasks for server→client `sampling/createMessage` and `elicitation/create`,
  and declare `tasks.list` / `tasks.cancel`. 2026 deletes the field: client-hosted tasks cannot be expressed once
  unsolicited server→client requests are forbidden.
- **Where.** 2025-11-25 only. Neither opencode version declares it (v1 has it commented out, with an issue link);
  lootbox declares nothing.
- **mcpx @ 05c78b2.** Not declared upstream (`internal/mcpclient/modern.go:46-58`).
- **Value to mcpx.** − work for a one-revision feature.
- **Effort.** S — leave undeclared.
- **Risk.** None.
- **Detail.** Sub-fields: `list`, `cancel`, `requests.sampling.createMessage`, `requests.elicitation.create`. SEP-2663
  removes client-hosted tasks because polling a client-hosted task would itself be an unsolicited server→client request.
  The server-side replacement is the tasks extension (CAP-26).
- **Sources.** `schema/2025-11-25/schema.ts:346` "tasks?: {"; `seps/2663-tasks-extension.md:35` "**Client-hosted tasks
  are no longer expressible.**"; `v1:packages/opencode/src/mcp/index.ts:48` "// tasks: {},"

## CAP-11 client `extensions` map

- **What.** Map of extension identifier to settings object; keys follow the `_meta` key-naming rules with a mandatory
  prefix; `{}` means supported with no settings.
- **Where.** 2026 only.
- **mcpx @ 05c78b2.** Never declared upstream (`internal/mcpclient/modern.go:46-58`). In particular mcpx never declares
  `io.modelcontextprotocol/tasks`, so a 2026 upstream will never hand it a task, and `CallTool` sends only
  `{name, arguments}` (`internal/mcpclient/client.go:845-858`).
- **Value to mcpx.** + the only way to opt into tasks, apps and auth extensions from 2026 servers.
- **Effort.** S to declare; each extension then costs its own implementation.
- **Risk.** None for declaring nothing; declaring an extension mcpx cannot handle would be the #177 class.
- **Detail.** If one side supports an extension and the other does not, the supporting side MUST revert to core
  behaviour or reject. The 2026 extensions map has no counterpart in `initialize`.
- **Sources.** `schema/2026-07-28/schema.ts:785` "extensions?: { [key: string]: JSONObject };";
  `2026-07-28/basic/versioning.mdx:121` "If one party supports an extension but the other does not, the supporting";
  `internal/mcpclient/modern.go:46-58`

## CAP-12 server `logging`

- **What.** A server that emits `notifications/message` declares `logging`. 2026 deprecates it (SEP-2577) but keeps the
  field; logging moves to a per-request `_meta` level (logging register).
- **Where.** All five; deprecated in 2026.
- **mcpx @ 05c78b2.** Never declared in any revision (`internal/mcpserver/server.go:1398`; rationale at
  `internal/mcpserver/server.go:566-571`; wire W2 shows no `logging`). That is truthful — mcpx emits no
  `notifications/message`. `docs/protocol.md:104` claims it is declared for 2025-03-26..2025-11-25 and "removed" in
  2026; both halves are wrong against the code and the schema.
- **Value to mcpx.** + fix the doc; the code is right.
- **Effort.** S.
- **Risk.** If the doc stays: operators expect log notifications that never come.
- **Detail.** opencode v1 forwards server log messages into its own log; v2 drops them (logging register).
- **Also recorded from the logging register.** All five revisions; deprecated (SEP-2577) in 2026-07-28. Never declared
  in any revision: `capabilities()` builds only tools, resources, prompts, completions and the task objects
  (`internal/mcpserver/server.go:1398-1403`). The `logging/setLevel` handler explains why: mcpx sends no log messages,
  so declaring the capability "was a promise of a stream that does not exist"
  (`internal/mcpserver/server.go:566-571`). Wire W2 shows no `logging`. `docs/protocol.md:104` still marks `logging` ✓
  for 2025-03-26..2025-11-25 and "removed in 2026-07-28"; both halves are wrong against the code and the schema. The
  2026 prose repeats the MUST ("Servers that emit log message notifications **MUST** declare"), so even a deprecated
  feature keeps its gate. Capability value is `JSONObject` in 2026 (was `object`).
- **Sources.** `schema/2024-11-05/schema.ts:216` "logging?: object;"; `schema/2026-07-28/schema.ts:801` "@deprecated
  Deprecated as of protocol version 2026-07-28 (SEP-2577)."; `schema/2026-07-28/schema.ts:808` "logging?:
  JSONObject;"; `internal/mcpserver/server.go:1398`; `docs/protocol.md:104`; `2025-11-25/server/utilities/logging.mdx:19` "Servers that emit log message notifications"; `2026-07-28/server/utilities/logging.mdx:32` "Servers that emit log message notifications"; wire W2.

## CAP-13 server `completions`

- **What.** 2025-03-26 adds the `completions` capability; servers that support `completion/complete` MUST declare it.
  2024-11-05 already had the method, with no capability.
- **Where.** Capability in 2025-03-26 and later; method in all five (completion register).
- **mcpx @ 05c78b2.** Declares `completions: {}` in every era it serves (`internal/mcpserver/server.go:1402`; wire W2),
  and answers the method in every era. As a client it calls `completion/complete` only if the upstream declared
  `completions` (`internal/mcpclient/request.go:50`).
- **Value to mcpx.** + low; the declaration is correct. What the answers contain is the completion register's concern.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Unsupported → `-32601` from 2025-03-26 prose. mcpx does not serve 2024-11-05, so the missing capability in
  that revision matters only on the client side (a 2024-11-05 upstream's completions are unreachable).
- **Sources.** `schema/2025-03-26/schema.ts:240` "completions?: object;";
  `2025-03-26/server/utilities/completion.mdx:24` "Servers that support completions **MUST** declare the `completions`
  capability:"; `internal/mcpserver/server.go:1402`; `internal/mcpclient/request.go:50`

## CAP-14 server `prompts`

- **What.** A server that offers prompts declares `prompts` (with optional `listChanged`, CAP-15).
- **Where.** All five. opencode v1 and v2 list prompts only from servers that declare `prompts` and turn each into a
  slash command.
- **mcpx @ 05c78b2.** Always declared (`internal/mcpserver/server.go:1401`), whether or not any upstream has prompts.
- **Value to mcpx.** + low: an empty prompt list costs a host one cheap request.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Unlike `resources` (CAP-16), declaring `prompts` costs opencode no model context: prompts become
  commands, not tools.
- **Sources.** `schema/2024-11-05/schema.ts:220` "prompts?: {"; `internal/mcpserver/server.go:1401`;
  `v1:packages/opencode/src/command/index.ts:105`; `v2:packages/core/src/plugin/command.ts:55`

## CAP-15 server `prompts.listChanged`

- **What.** Until 2025-11-25, `listChanged: true` means the server may push `notifications/prompts/list_changed`
  unsolicited. In 2026 it means the server SHOULD send it to clients that opened `subscriptions/listen` with
  `promptsListChanged`.
- **Where.** All five; delivery model changes in 2026. opencode v2 refetches prompts (and republishes the commands) on
  the notification; v1 ignores it but fetches prompts fresh on each command listing.
- **mcpx @ 05c78b2.** Declared as "this connection can push" (`internal/mcpserver/server.go:1390`): true on stdio (wire
  S2), false over HTTP (wire W2), because HTTP `Conn`s have no push function (`internal/mcpserver/conn.go:87-92`). On a
  legacy stdio connection nothing starts the notifier — `restartListen` is called only from `resources/subscribe` and
  `subscriptions/listen` (`internal/mcpserver/server.go:619`, `internal/mcpserver/server.go:636`) — so the true flag is
  never delivered. The comment at `internal/mcpserver/server.go:1393-1396` claims the opposite.
- **Value to mcpx.** + legacy hosts would see upstream prompts appear and disappear; the daemon's event bus already sees
  those changes (`internal/daemon/hooks.go:37-51`).
- **Effort.** S — start a listener for the three list kinds on a legacy stdio connection after `initialized`.
- **Risk.** If not done: a declared-but-not-delivered capability (#177 class). Over HTTP the flag is honest but no host
  ever learns of a change: mcpx offers no legacy GET stream and answers 2026 `subscriptions/listen` over HTTP with an
  empty 202 (transports register).
- **Detail.** The 2025-11-25 schema says list_changed "may be issued by servers without any previous subscription";
  2026 ties it to an opted-in stream and requires `_meta["io.modelcontextprotocol/subscriptionId"]` on every such
  notification (notifications register). What changes is the upstreams' lists surfaced through mcpx's own
  `prompts/list`.
- **Sources.** `schema/2026-07-28/schema.ts:829` "listChanged?: boolean;"; `schema/2025-11-25/schema.ts:1159` "This may
  be issued by servers without any previous subscription from the client."; `2026-07-28/server/prompts.mdx:169`;
  `internal/mcpserver/server.go:1390` "push := s.Notify != nil && c != nil && c.canPush()";
  `internal/mcpserver/server.go:1393-1396` "are defined in every revision mcpx serves and arrive unsolicited on a";
  `v2:packages/core/src/mcp/index.ts:374`

## CAP-16 server `resources`

- **What.** A server that offers resources declares `resources` (with optional `subscribe` and `listChanged`).
- **Where.** All five. opencode v1 adds three model-facing tools (`list_mcp_resources`, `list_mcp_resource_templates`,
  `read_mcp_resource`) to every request as soon as any connected server declares `resources`, even in code mode; v2 uses
  the capability only for the TUI `@` list.
- **mcpx @ 05c78b2.** Always declared (`internal/mcpserver/server.go:1400`), even when no upstream has resources.
- **Value to mcpx.** + three tool schemas on every opencode v1 request is exactly the context cost mcpx exists to
  avoid; − mcpx always has at least its own artifact template (`mcpx://artifacts/{id}`, wire W18), so dropping the
  capability also drops artifact reads for that host. A per-host choice, not a simple removal.
- **Effort.** S.
- **Risk.** If not done: every opencode v1 session pays the three schemas. If done unconditionally: hosts lose
  `resources/read` of script artifacts.
- **Detail.** v1 gates resource reads behind a `read` permission with pattern `mcp:<server>:<uri>`. How resources reach
  the model in each product is RES-16.
- **Sources.** `schema/2024-11-05/schema.ts:229` "resources?: {"; `internal/mcpserver/server.go:1400`;
  `v1:packages/opencode/src/session/tools.ts:136`; `v2:packages/tui/src/component/prompt/autocomplete.tsx:413`

## CAP-17 server `resources.subscribe`

- **What.** Legacy: the server supports `resources/subscribe`. 2026 removes that RPC but keeps the flag, redefined as
  "supports `resourceSubscriptions` on `subscriptions/listen`".
- **Where.** All five; meaning changes in 2026. Neither opencode version ever subscribes.
- **mcpx @ 05c78b2.** `subscribe: push` (`internal/mcpserver/server.go:1400`): true on stdio (wire S2), false over HTTP
  (wire W2). `resources/subscribe` is accepted in every era but nothing subscribes upstream (SUB-15), so on stdio the
  flag is declared and not delivered.
- **Value to mcpx.** + real resource watches (files, tickets) through mcpx.
- **Effort.** M — needs upstream subscription on legacy servers and a listen stream on modern ones.
- **Risk.** If not done: a host subscribes and waits for updates that arrive only if an upstream sends them unasked.
  A client reading the 2026 flag as "call `resources/subscribe`" gets `-32601` from a pure-2026 server.
- **Detail.** The 2026 changelog says the RPC was replaced but never says the flag was kept and redefined — the prose
  (resources.mdx) and schema do. On stdio, mcpx maps a subscribe onto `restartListen` with only the URI list, which
  replaces any active list-changed filter on that connection (`internal/mcpserver/server.go:619`).
- **Also recorded from the notifications register.** The flag is in all five revisions; its meaning changes in
  2026-07-28, which the changelog does not say. Declares `subscribe: push` in every era
  (`internal/mcpserver/server.go:1400`), true on stdio and false over HTTP. Updates reach a subscriber only if an
  upstream sends them unasked, because mcpx never subscribes upstream (SUB-15; the resources register has the
  details).
- **Sources.** `schema/2026-07-28/schema.ts:850` "subscribe?: boolean;"; `2026-07-28/server/resources.mdx:58` "-
  `subscribe` : whether the server supports resource-specific update notifications";
  `2026-07-28/changelog.mdx:18`; `internal/mcpserver/server.go:1400`; `internal/mcpserver/server.go:619`

## CAP-18 server `resources.listChanged`

- **What.** As CAP-15, for `notifications/resources/list_changed`; in 2026 opted into with `resourcesListChanged`.
- **Where.** All five; listen-only in 2026. opencode v2 publishes a `ResourcesChanged` event on it; v1 ignores it.
- **mcpx @ 05c78b2.** Same as CAP-15: true on stdio and never delivered to legacy hosts, false over HTTP
  (`internal/mcpserver/server.go:1400`, `internal/mcpserver/server.go:1390`).
- **Value to mcpx.** + hosts learn when an upstream adds resources.
- **Effort.** S (shares CAP-15's fix).
- **Risk.** As CAP-15.
- **Detail.** mcpx's pool does drop its cached schema on an upstream `list_changed` and publishes an event
  (`internal/daemon/hooks.go:37-51`); only the last hop to the host is missing.
- **Sources.** `schema/2026-07-28/schema.ts:854` "listChanged?: boolean;"; `2026-07-28/server/resources.mdx:234`;
  `internal/mcpserver/server.go:1400` "\"resources\":   map[string]any{\"subscribe\": push, \"listChanged\":
  streamed},"; `v2:packages/core/src/mcp/index.ts:375`

## CAP-19 server `tools`

- **What.** A server that offers tools declares `tools`.
- **Where.** All five. opencode v1 registers its `tools/list_changed` handler only when the server declares `tools`.
- **mcpx @ 05c78b2.** Always declared (`internal/mcpserver/server.go:1399`).
- **Value to mcpx.** + low; nothing to change.
- **Effort.** S.
- **Risk.** None.
- **Detail.** mcpx's tool list is its own fixed surface plus generated op tools (tools register), not the upstreams'
  tools.
- **Sources.** `schema/2024-11-05/schema.ts:242` "tools?: {"; `internal/mcpserver/server.go:1399`;
  `v1:packages/opencode/src/mcp/index.ts:462`

## CAP-20 server `tools.listChanged`

- **What.** As CAP-15, for `notifications/tools/list_changed`; in 2026 opted into with `toolsListChanged`.
- **Where.** All five; listen-only in 2026. Both opencode versions refetch tools on it (v2 debounces the registry
  reconcile by 100 ms; on a modern connection v2's SDK opens `subscriptions/listen` itself).
- **mcpx @ 05c78b2.** Same declaration rule as CAP-15 (`internal/mcpserver/server.go:1399`). Independently of delivery,
  mcpx's own tool list cannot change: the MCP surface is built once per daemon under a `sync.Once`
  (`internal/cli/serve_ask.go:242`), so adapters or APIs added by a config reload never appear.
- **Value to mcpx.** + adapters added at runtime would reach hosts without a restart.
- **Effort.** S for delivery; S to rebuild the surface on reload.
- **Risk.** If not done: users restart the daemon to see new adapter tools; the declared flag promises something that
  cannot happen.
- **Detail.** `tools/list_changed` is the only list-changed kind both opencode versions honour, so it is the one that
  matters most for mcpx's primary host.
- **Also recorded from the notifications register.** Legacy revisions (2024-11-05..2025-11-25) over stdio.
  Capabilities are true when the connection can push (`internal/mcpserver/server.go:1390`, `:1397-1402`; wire S2).
  `restartListen` is called only from `resources/subscribe` and `subscriptions/listen`
  (`internal/mcpserver/server.go:619`, `:636`), and `ServeStdio` starts nothing
  (`internal/mcpserver/server.go:1000-1073`). The capability comment claims list_changed notifications "arrive
  unsolicited on a connection that can push" (`internal/mcpserver/server.go:1393-1396`). Over HTTP the flags are
  truthfully `false` (no push), and `docs/protocol.md:100-102` claims ✓ everywhere. A send-conservatively conflict: a
  capability promising a stream that does not exist. Legacy HTTP hosts could have had these on the GET stream, which
  mcpx answers with 405 (a transports-register item).
- **Sources.** `schema/2026-07-28/schema.ts:869` "listChanged?: boolean;"; `2026-07-28/server/tools.mdx:238`
  "When the list of available tools changes, servers that declared the `listChanged`";
  `internal/cli/serve_ask.go:242` "l.once.Do(func() { l.srv, l.err = l.app.MCPServer(ctx) })";
  `v1:packages/opencode/src/mcp/index.ts:462`; `v2:packages/core/src/tool/mcp.ts:140`; `internal/mcpserver/server.go:1390` "push := s.Notify != nil && c != nil && c.canPush()"; `internal/mcpserver/server.go:1393-1396` "arrive unsolicited on a"; `internal/mcpserver/server.go:619`; `docs/protocol.md:100`; wire S2.

## CAP-21 server core `tasks` object

- **What.** 2025-11-25 servers declare task support in `capabilities.tasks` (`list`, `cancel`,
  `requests.tools.call`). 2026 moves tasks into the `io.modelcontextprotocol/tasks` extension, and SEP-2663 says servers
  MUST NOT keep advertising the legacy capabilities under a protocol version that includes the extension.
- **Where.** 2025-11-25 only; replaced by the extension in 2026.
- **mcpx @ 05c78b2.** `capabilities()` emits the core shape whenever `Defines(version, FeatTasks)`, and `FeatTasks` has
  no ceiling (`internal/mcpserver/server.go:1404-1418`; `internal/mcpserver/revisions.go:90`), so every 2026 host gets
  `tasks {list, cancel, requests.tools.call}` from `server/discover` (wire W5). A send-conservatively conflict.
- **Value to mcpx.** + keeps the capability object honest to modern hosts.
- **Effort.** S — add a ceiling for the core shape.
- **Risk.** Low for parsing (open set), but the declaration promises `tasks/list` and `tasks/result`, which the extension
  removed.
- **Detail.** The comment above the branch says the capability is "declared both ways and a client of either era finds
  it where it looks"; SEP-2663 forbids exactly that. The shape of what mcpx actually returns for tasks is in the tasks
  register.
- **Sources.** `internal/mcpserver/server.go:1411`; `internal/mcpserver/server.go:1406` "declared both ways and a client
  of either era finds it where it"; `seps/2663-tasks-extension.md:949` "**MUST NOT** continue to
  advertise the legacy capabilities under any protocol version that includes this extension";
  `2026-07-28/changelog.mdx:22`; `schema/2025-11-25/schema.ts:433` "tasks?: {"

## CAP-22 server `tasks.list`

- **What.** Declares `tasks/list`. A server that cannot bind tasks to requestors SHOULD NOT declare it.
- **Where.** 2025-11-25; removed with the extension (which has no `tasks/list`).
- **mcpx @ 05c78b2.** Declared (`internal/mcpserver/server.go:1412`), and `tasks/list` returns every task from every
  connection (`internal/mcpserver/tasks.go:89-95`); an unrelated client listed another's task on the wire (W23).
- **Value to mcpx.** + removes cross-host metadata exposure on a shared daemon.
- **Effort.** S — stop declaring, or bind tasks to a requestor.
- **Risk.** If not done: any host sees every host's task ids and can collect their results.
- **Detail.** Task ids are 64 random bits (`internal/tasks/tasks.go:86-90`), "cryptographically secure" only weakly.
- **Sources.** `schema/2025-11-25/schema.ts:437` "list?: object;"; `2025-11-25/basic/utilities/tasks.mdx:876`
  "**SHOULD NOT** declare the `tasks.list` capability"; `internal/mcpserver/server.go:1412`

## CAP-23 server `tasks.cancel`

- **What.** Declares `tasks/cancel`. In 2025-11-25 cancelling a terminal task MUST be rejected with `-32602`; the 2026
  extension implies cancel and answers with an empty acknowledgement.
- **Where.** 2025-11-25; implied by the extension in 2026.
- **mcpx @ 05c78b2.** Declared (`internal/mcpserver/server.go:1412`); cancelling a finished task returns the task
  snapshot instead of `-32602` (tasks register).
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** The declaration itself is fine for 2025-11-25; the behaviour behind it is the gap.
- **Sources.** `schema/2025-11-25/schema.ts:441` "cancel?: object;"; `2025-11-25/basic/utilities/tasks.mdx:494`;
  `internal/mcpserver/tasks.go:124` "return reply(snap)"

## CAP-24 server `tasks.requests.tools.call`

- **What.** Declares that `tools/call` may be task-augmented. Each tool must also opt in with
  `execution.taskSupport`; absent means `"forbidden"`, and a conforming client then MUST NOT request a task.
- **Where.** 2025-11-25 only.
- **mcpx @ 05c78b2.** Declared (`internal/mcpserver/server.go:1413`), but mcpx's `Tool` struct has no `execution` field
  (`internal/mcpserver/server.go:260-268`), so every mcpx tool is task-forbidden to a conforming client. mcpx still
  accepts a `task` param from anyone (`internal/mcpserver/server.go:477-501`).
- **Value to mcpx.** + long `mcpx_exec`/`mcpx_call` runs could survive disconnects for conforming hosts.
- **Effort.** S — add `execution: {taskSupport: "optional"}` to long-running tools, at 2025-11-25 only.
- **Risk.** If not done: declared but unreachable (#177 class). If done carelessly: `taskSupport: "required"` would make
  the tool uncallable from opencode v1, whose SDK throws on it (TOOL-21).
- **Detail.** `execution` must not be sent below 2025-11-25, and `tools/list` is not revision-filtered today (TOOL-25).
- **Also recorded from the tasks register.** 2025-11-25 only; the extension has no per-tool gate. Declares
  `tasks.requests.tools.call` (`internal/mcpserver/server.go:1413`), but its `Tool` type has no `execution` field
  (`internal/mcpserver/server.go:260-268`), so every tool is `forbidden` and a conforming client never asks for a
  task. mcpx still accepts a `task` param from anyone and creates the task (`internal/mcpserver/server.go:477-501`;
  wire W22) rather than `-32601`. The field must not be sent below 2025-11-25 (send conservatively). The tools
  register owns `Tool.execution` itself.
- **Sources.** `schema/2025-11-25/schema.ts:453` "call?: object;"; `2025-11-25/basic/utilities/tasks.mdx:115`
  "clients **MUST NOT** attempt to invoke the tool as a task"; `internal/mcpserver/server.go:1413`;
  `internal/mcpserver/server.go:260-268`; `schema/2025-11-25/schema.ts:1270`; wire W22.

## CAP-25 server `extensions` map

- **What.** Servers advertise extensions in an `extensions` map returned by `server/discover`.
- **Where.** 2026 only; `ServerCapabilities` in 2025-11-25 has no such member.
- **mcpx @ 05c78b2.** Emitted inside the same `FeatTasks` branch whose floor is 2025-11-25
  (`internal/mcpserver/server.go:1415-1417`), so a 2025-11-25 `initialize` returns `extensions` too. This contradicts
  mcpx's own rule that it "**declares** none of them outside the revision that defines them" (`docs/protocol.md:93-94`),
  while the capability table at `docs/protocol.md:106` agrees with the code.
- **Value to mcpx.** + low; a conservative send.
- **Effort.** S — gate on 2026.
- **Risk.** Low (open set).
- **Detail.** The only key mcpx puts in it is the tasks extension (CAP-26).
- **Sources.** `schema/2026-07-28/schema.ts:882` "extensions?: { [key: string]: JSONObject };";
  `2026-07-28/changelog.mdx:32`; `internal/mcpserver/server.go:1415`; `docs/protocol.md:93-94`; `docs/protocol.md:106`

## CAP-26 `extensions["io.modelcontextprotocol/tasks"]` key

- **What.** The 2026 way to negotiate tasks: the server advertises the key via `server/discover`, the client declares it
  in each request's capabilities. It is not wire-compatible with 2025-11-25 tasks.
- **Where.** 2026, as an official extension (SEP-2663). Under 2025-11-25, servers MUST NOT treat the capability as
  enabling tasks.
- **mcpx @ 05c78b2.** As a server it advertises the key to 2025-11-25 and 2026 hosts
  (`internal/mcpserver/server.go:1415-1417`) while implementing only the 2025-11-25 task wire shape (tasks register). As
  a client it never declares the key (`internal/mcpclient/modern.go:46-58`).
- **Value to mcpx.** + long-running upstream jobs through modern servers; honest advertising to modern hosts.
- **Effort.** M — the extension's shapes (`resultType: "task"`, `tasks/update`, `DetailedTask`) differ from 2025-11-25.
- **Risk.** If not done: 2026 hosts are promised an extension whose wire format mcpx does not speak.
- **Detail.** SEP-2663 names the protocol version `2026-06-30` in its compatibility table, not `2026-07-28`; the local
  clone has no other source for the extension (the normative text lives in an external `ext-tasks` repo). Its
  backward-compatibility table is the only place that says servers MUST migrate from the legacy capabilities.
- **Also recorded from the tasks register.** 2025-11-25 core; 2026-07-28 extension. Declares both, to both eras
  (`internal/mcpserver/server.go:1404-1417`); the code comment says so on purpose: "declared both ways and a client of
  either era finds it where it looks" (`internal/mcpserver/server.go:1405-1407`). As a client of modern upstreams it
  never declares the extension. No extension-specific settings are defined yet; The doc repeats the table
  (`docs/protocol.md:106`) against its own rule that nothing is declared outside the revision that defines it.
- **Sources.** `seps/2663-tasks-extension.md:47`; `seps/2663-tasks-extension.md:948` "This extension is not defined
  under the `2025-11-25` protocol version."; `seps/2663-tasks-extension.md:949`; `internal/mcpserver/server.go:1415`; `2025-11-25/basic/utilities/tasks.mdx:45`; `schema/2025-11-25/schema.ts:433`; `seps/2663-tasks-extension.md:83` "No extension-specific settings are currently defined; an empty object indicates support."; `internal/mcpserver/server.go:1404-1417`; `docs/protocol.md:106`.
