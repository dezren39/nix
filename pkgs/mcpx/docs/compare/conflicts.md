# Communication conflicts

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare, area:protocol
description:  every place two parties cannot both be satisfied: revision
              against revision, prose against schema, product against
              product, one mcpx surface against another, and mcpx's
              documents against its code.
```

A conflict here is a place where doing what one side expects breaks another.
Some are in the specification itself. Some are between the products mcpx sits
between. Some are inside mcpx. mcpx's design answer to the protocol-level ones
(accept liberally, send conservatively, one `downgrade()` at the edge, two
mechanisms for one question) is in [`../protocol.md`](../protocol.md) and is
not repeated here. This page records the collisions, not the design. mcpx
status is as of `05c78b2`.

Each entry names both sides, the source for each, and what mcpx does or should
do. IDs are stable so issues and register rows can point at them.

---

## A. Revision against revision

A dual-era server has to serve both sides of each of these, per request.

<a id="c-01"></a>
**C-01 A request with neither `_meta` nor a session.** 2026-07-28 calls it
malformed and requires `-32602` with HTTP 400 (`2026-07-28/basic/index.mdx:380-382`).
The legacy HTTP rules say a server that receives no `MCP-Protocol-Version`
"SHOULD assume protocol version `2025-03-26`"
(`2025-06-18/basic/transports.mdx:250-254`). The specification never says what
a dual-era server should do. mcpx serves it as 2025-03-26
(`internal/mcpserver/conn.go:137-146`). That is right for a dual-era server,
and it should stay. See [stateless.md §4](stateless.md#4-the-conflict-at-the-centre).

**C-02 Version negotiation.** Every legacy revision says the server MUST answer
`initialize` with a version it supports, a counter-offer
(`2025-03-26/basic/lifecycle.mdx:130-133`). 2026-07-28 answers an unsupported
version with an error, `-32022` (`2026-07-28/basic/versioning.mdx:48-67`).
mcpx applies the 2026 behaviour to legacy `initialize`: 2024-11-05 and unknown
versions get `-32022`, a code no legacy client knows
(`internal/mcpserver/server.go:844-859`, W1). It should counter-offer on
`initialize` and keep `-32022` for `_meta` requests.

**C-03 Resource not found.** `-32002` through 2025-11-25; `-32602` in
2026-07-28, which also forbids emitting `-32002` and asks clients to still
accept it (`2026-07-28/basic/index.mdx:136-142`). mcpx sends `-32602` to every
revision, so legacy clients get the wrong code
(`internal/mcpserver/server.go:713-716`).

**C-04 Logging's default flipped.** 2025-11-25: with no `logging/setLevel` "the
server MAY decide which messages to send automatically"
(`schema/2025-11-25/schema.ts:1545`). 2026-07-28: with no per-request
`logLevel` the server "MUST NOT emit `notifications/message`"
(`2026-07-28/server/utilities/logging.mdx:62-64`). mcpx emits no log messages
to hosts at all, so it satisfies both by silence. Its own comment states the
2026 rule backwards (`internal/mcpserver/revisions.go:72-73`).

**C-05 How a server asks.** Legacy: a server-initiated request on the open
connection. 2026-07-28: `input_required` and a retried request
(`2026-07-28/basic/patterns/mrtr.mdx:7-14`). Two mechanisms, one question.
mcpx bridges them both ways ([input-required.md](input-required.md)).

**C-06 Resource subscriptions.** `resources/subscribe` and `unsubscribe`
through 2025-11-25; `subscriptions/listen` with a `resourceSubscriptions` filter
in 2026-07-28 (`2026-07-28/changelog.mdx:18`). The capability flag
`resources.subscribe` survives in the 2026 schema with a new meaning, "supports
`resourceSubscriptions` on listen" (`schema/2026-07-28/schema.ts:850`,
`2026-07-28/server/resources.mdx:58`). mcpx accepts both from either era.

**C-07 Tasks, core against extension.** The 2025-11-25 experimental tasks and
the 2026 `io.modelcontextprotocol/tasks` extension are "**not
wire-compatible**" (`seps/2663-tasks-extension.md:944`). `tasks/result` and
`tasks/list` are removed and `tasks/update` added. `failed` changes meaning:
in core it includes tool results with `isError`; in the extension those are
`completed` (`schema/2025-11-25/schema.ts:1310`,
`seps/2663-tasks-extension.md:835`). mcpx serves the core shape to every era
and advertises the extension (`internal/mcpserver/server.go:1404-1417`).

**C-08 `_meta` key prefixes.** 2025-06-18 recommends forward DNS
(`example.com/`); 2025-11-25 recommends reverse DNS (`com.example/`)
(`2025-11-25/basic/index.mdx:207-210`). Neither changelog mentions it. mcpx
reserves nothing of its own in `_meta`. opencode's `ai.opencode/sessionID`
follows the reverse rule.

**C-09 Batching lived for one revision.** 2025-03-26 servers "**MUST** support
receiving JSON-RPC batches" (`2025-03-26/basic/index.mdx:97`); 2025-06-18
removes batching (`2025-06-18/changelog.mdx:12`). mcpx lists 2025-03-26 as
supported and rejects batches with `-32700` (W16).

**C-10 Is a dropped connection a cancellation?** 2025-03-26 through
2025-11-25: "Disconnection **SHOULD NOT** be interpreted as the client
cancelling its request" (`2025-11-25/basic/transports.mdx:128`). 2026-07-28:
closing the response stream "**MUST** be treated by the server as
cancellation" (`2026-07-28/basic/transports/streamable-http.mdx:235-236`).
mcpx propagates the HTTP request context to the upstream call for every era.
It is right for 2026 and against the legacy SHOULD NOT. This is inferred from
the context chain and was not driven.

**C-11 `notifications/cancelled.requestId`.** Optional in 2025-11-25 (so a task
can be cancelled without one), required again in 2026-07-28
(`schema/2025-11-25/schema.ts:223`). mcpx reads it when present.

**C-12 URL elicitation's plumbing.** 2025-11-25 added `elicitationId`,
`notifications/elicitation/complete` and `-32042`; 2026-07-28 removed all three
(`2026-07-28/changelog.mdx:54-60`, `2026-07-28/basic/index.mdx:144`). mcpx passes
an upstream's elicitation `params` through to a 2026 host verbatim, so a 2026
host can receive an `elicitationId` it has no use for.

**C-13 Sessions.** Legacy: a client "**MUST** include" the `Mcp-Session-Id` it
was given (`2025-11-25/basic/transports.mdx:206-208`). 2026-07-28: a server
supporting only that revision should "ignore it, and do not mint or echo
session IDs" (`2026-07-28/basic/transports/streamable-http.mdx:680-686`). mcpx
mints one on `server/discover`, a modern request, and its modern `input_required`
path needs it back ([stateless.md](stateless.md#the-answer)).

**C-14 `MCP-Protocol-Version`.** Legacy (2025-06-18+): required after
initialization; an invalid value is a 400
(`2025-06-18/basic/transports.mdx:256-257`). 2026-07-28: required on every POST
and must equal the `_meta` version, else `-32020`
(`2026-07-28/basic/transports/streamable-http.mdx:252`). The 2026 page also
says revisions before 2025-06-18 "did not define" the header
(`2026-07-28/basic/transports/streamable-http.mdx:278`). 2025-03-26 did define
it, for OAuth metadata discovery (`2025-03-26/basic/authorization.mdx:135-139`).
mcpx ignores an invalid legacy header (W14) and answers a 2026 mismatch with a
non-JSON-RPC 400 (W10).

**C-15 `structuredContent` and `outputSchema`.** An object, described by an
object-rooted schema, in 2025-06-18 and 2025-11-25; any JSON value, any schema
in 2026-07-28 (`schema/2026-07-28/schema.ts:1821`). A server that returns an
array to a 2026 client must not return it to a 2025 one. mcpx emits no
`structuredContent` today.

**C-16 Client registration.** Dynamic Client Registration is a SHOULD in
2025-03-26 (`2025-03-26/basic/authorization.mdx:42`), a MAY in 2025-11-25
(behind Client ID Metadata Documents), and deprecated in 2026-07-28
(`2026-07-28/deprecated.mdx:29`). mcpx performs no OAuth.

**C-17 `includeContext`.** `thisServer` and `allServers` are soft-deprecated in
2025-11-25 and deprecated in 2026-07-28 (`2026-07-28/changelog.mdx:84-88`),
gated by `sampling.context` in between. mcpx forwards sampling requests to hosts
without checking it.

## B. One revision against itself

<a id="c-20"></a>
**C-20 Bad tool arguments: protocol error or tool error?** 2026 prose makes
input-validation failures `isError` results
(`2026-07-28/server/tools.mdx:760-765`). So does the 2025-11-25 changelog
(`2025-11-25/changelog.mdx:30`). The 2026 schema's `InvalidParamsError` still
lists "invalid tool arguments" as `-32602` (`schema/2026-07-28/schema.ts:366`)
and ships a `-32602` example for it. mcpx reports its own tools' argument
errors as `isError` results (`internal/mcpserver/server.go:665-675`), which
follows the prose.

**C-21 Ending a subscription (2026).** The cancellation page says the server
MUST send `notifications/cancelled` for the listen id
(`2026-07-28/basic/patterns/cancellation.mdx:11`). The subscriptions page says
it SHOULD send a successful `subscriptions/listen` result, then close
(`2026-07-28/basic/patterns/subscriptions.mdx:122-124`). The schema limits the
server-sent cancel to stdio (`schema/2026-07-28/schema.ts:637`).

**C-22 Client cancellation (2026).** "A client **SHOULD** send a cancellation
notification" (`2026-07-28/basic/patterns/cancellation.mdx:8`), but on HTTP the
transport "defines no client-to-server" notification. Closing the stream is
the cancellation (`2026-07-28/basic/transports/streamable-http.mdx:95-101`).
Read the SHOULD as stdio-only.

**C-23 Legacy lifecycle against its own example.** A counter-offer is a MUST
(`2024-11-05/basic/lifecycle.mdx:126`), yet the same page's error example
answers an unsupported version with `-32602` (`2024-11-05/basic/lifecycle.mdx:210`).
The client's disconnect is a SHOULD in prose (`:130`) and a MUST in the schema
(`schema/2024-11-05/schema.ts:162`).

**C-24 Which results are cacheable (2026).** The changelog names five methods
(`2026-07-28/changelog.mdx:36`); the caching page and the schema add
`server/discover` (`2026-07-28/server/utilities/caching.mdx:16`,
`schema/2026-07-28/schema.ts:678`).

**C-25 Where `inputResponses` may go (2026).** The schema says "any
client-initiated request" (`schema/2026-07-28/schema.ts:598`); the prose allows
`input_required` on three methods only (`2026-07-28/basic/patterns/mrtr.mdx:192`).

**C-26 Is `resultType` open (2026)?** Typed as `"complete" | "input_required" |
string` (`schema/2026-07-28/schema.ts:216`), but "a `resultType` of any value
unrecognized by the client **MUST** be considered invalid"
(`2026-07-28/basic/index.mdx:84`). An extension value (`"task"`) is valid only
where the extension was negotiated. mcpx stamps `"complete"` on its task
handle, where the extension's value would be `"task"`.

**C-27 The examples break their own rule (2026).** The `list_changed` examples
carry no `_meta` (`2026-07-28/server/tools.mdx:246`), though every notification
on a listen stream MUST carry `io.modelcontextprotocol/subscriptionId`
(`2026-07-28/basic/index.mdx:413`). An implementer copying the example (as mcpx's
stdio listen path effectively does) is non-conformant.

**C-28 The tasks SEP against itself.** `tasks/get` responses MUST use
`resultType: "complete"` (`seps/2663-tasks-extension.md:340`), but its own error
examples show `"task"` (`:844`, `:868`). It names the protocol version
`2026-06-30` (`:17`). It reserves `notifications/tasks/` but names its
notification `notifications/tasks` (`:894`, `:422`).

**C-29 "Unsolicited" tasks (2026).** The changelog says servers may return task
handles "without per-request opt-in" (`2026-07-28/changelog.mdx:22`). The
extension still requires the client to list it in each request's capabilities
(`docs/extensions/tasks/overview.mdx:231-233`). Both hold once "opt-in" is read
as "the removed `task` parameter".

**C-30 Answers cannot be errors (2026).** `InputResponse` has no error variant
(`schema/2026-07-28/schema.ts:541-542`). Yet the schema still documents
client-side `-32602` for elicitation modes and sampling tool results
(`schema/2026-07-28/schema.ts:370-371`), errors a client can no longer return
inside MRTR.

**C-31 Deprecation windows (2026).** Twelve months minimum
(`2026-07-28/changelog.mdx:110`), except HTTP+SSE: three months after SEP-2596
reaches Final (`2026-07-28/deprecated.mdx:31`).

## C. Product against product

<a id="c-40"></a>
**C-40 Neither opencode speaks 2026 by default.** v1's SDK negotiates
2025-11-25 and has no modern path
(`v1:patches/@modelcontextprotocol%2Fsdk@1.29.0.patch:36`). v2 speaks
2026-07-28 only when a server is configured `protocol: "auto"` or
`"2026-07-28"` (`v2:packages/schema/src/mcp.ts:20-23`). So mcpx's modern serving
path is not exercised by either host out of the box. `docs/opencode-plugin.md`
says v2 will exercise it; that holds only with the option set.

**C-41 Session identity arrives two ways, and mcpx reads neither on `/mcp`.**
The v1 plugin injects `MCPX_SESSION_ID` into shell commands through `shell.env`
(`plugin/opencode/mcpx-session.ts:664`). That reaches the CLI, not `/mcp`. v2
has no session id in its shell hook, and instead sends
`_meta["ai.opencode/sessionID"]` on every `tools/call`
(`v2:packages/core/src/mcp/client.ts:278`). mcpx's MCP server keys every call by
process id (`internal/cli/serve.go:292-297`).

**C-42 Code mode inside code mode.** v2 wraps every MCP server in its own code
mode unless the server is configured `codemode: false`
(`v2:packages/schema/src/mcp.ts:34-35`), and the built-in exclusion matches only
`executor` (`v2:packages/core/src/plugin/mcp-codemode-exclusion.ts:16`). mcpx's
`mcpx_exec` then runs as a tool called from a program the model wrote for
opencode's interpreter. v2 also appends `?codemode=false` to remote MCP URLs so
servers that bundle code mode can expose raw tools
(`v2:packages/core/src/mcp/client.ts:214-218`). mcpx ignores the parameter.

**C-43 Tool names.** opencode names MCP tools `<server>_<tool>`
(`v1:packages/opencode/src/mcp/catalog.ts:119`), so mcpx configured as `mcpx`
becomes `mcpx_mcpx_namespaces`. v2 rejects any name part over 64 characters
(`v2:packages/core/src/tool.ts:297`).

**C-44 v1 drops what mcpx's artifacts use.** v1's direct tool path keeps text,
images and embedded resources and drops `audio` and `resource_link`
(`v1:packages/opencode/src/session/tools.ts:429-436`). `mcpx_exec` artifacts are
`resource_link` blocks, so under v1 without code mode the model never sees
them.

**C-45 Declaring `resources` costs every v1 request context.** v1 adds three
resource tools to every request when any server declares `resources`
(`v1:packages/opencode/src/session/tools.ts:136`). mcpx always declares it.

**C-46 Nobody answers mcpx's questions on v1.** v1 has no `elicitation/create`
handler and declares no `elicitation` (`v1:packages/opencode/src/mcp/index.ts:42-48`,
`:77`). mcpx correctly never sends it one, so a question raised under v1 goes
to the broker. v2 declares form and url and answers them, but attributes them
to the Location, not the session (`v2:packages/core/src/mcp/index.ts:80-82`).

**C-47 Progress that keeps nothing alive.** v2's comment says requesting
progress "keeps long calls alive under the SDK's timeout", but
`resetTimeoutOnProgress` is never set (`v2:packages/core/src/mcp/client.ts:279-281`).
mcpx never forwards progress, so in either host a long `mcpx_call` is bounded
by the host's timeout: 60 s on v1 unless configured, 12 h on v2.

**C-48 lootbox declares nothing.** lootbox's MCP client (SDK 1.22.0, latest
2025-06-18) sends `capabilities: {}`
(`.lootbox/src/lib/external-mcps/mcp_client_manager.ts:150-153`). An upstream
that elicits gets method-not-found. mcpx declares `elicitation` and answers
through its broker.

**C-49 Sandbox or no sandbox.** The MCP client best-practices guide's
programmatic tool calling "requires clients to implement a sandbox". Cloudflare
argues a shell-based CLI is "a much broader attack surface than a sandboxed
isolate". opencode runs only the language features it implements. mcpx runs
scripts on a real runtime with full authority, by choice
([register/code-mode.md](register/code-mode.md)). Nobody is wrong on the facts.
The disagreement is about who the caller is and what it may do.

## D. One mcpx surface against another

<a id="c-60"></a>
**C-60 `mcpx_call` against the script client.** The same upstream result is
seen differently. `mcpx_call` drops `isError`, pretty-prints `structuredContent`
as text and drops images (`internal/cli/commands.go:384-415`). A script gets
`ToolError`, the structured value, and every block on `.raw`
(`internal/codegen/emit.go:493-518`). See [content-passthrough.md](content-passthrough.md).

**C-61 Two task stores.** The comment on the task store says one store serves
both the MCP server and `/v1` so a task started on one is visible on the other
(`internal/tasks/tasks.go:9-12`). There are two: the MCP server's
(`internal/mcpserver/tasks.go:31-38`) and the daemon's
(`internal/daemon/ops.go:428-429`).

**C-62 stdio and HTTP declare different capabilities.** The same server
declares `listChanged: true` and `subscribe: true` over stdio and `false` over
HTTP (W2, S2). Neither delivers: nothing starts a notifier for a legacy stdio
client (`internal/mcpserver/server.go:1389-1402`).

**C-63 mcpx's server and client agree with each other and not with the
schema.** Both use `protocolVersions` for `server/discover`
(`internal/mcpserver/server.go:534`, `internal/mcpclient/client.go:284`), and
three tests pin it (`internal/mcpserver/server_test.go:393`,
`internal/mcpclient/era_test.go:87`, `internal/mcpclient/modern_test.go:88`).
Self-consistent tests are how an interoperability bug passes CI.

**C-64 The plugin's `mcpx_exec` and the daemon's `/v1/exec`.** The plugin reads
an `output` field the daemon's result does not have, and sends no session,
working directory or abort signal (`plugin/opencode/mcpx/`; see
[register/plugin-apis.md](register/plugin-apis.md)).

**C-65 The CLI and `/v1/exec` release sessions differently.** The CLI sets
`MCPX_EPHEMERAL` (`internal/cli/commands.go:694`); `/v1/exec` does not
(`internal/execsvc/execsvc.go:573-593`). So session-scoped instances, a browser
for instance, are released at the end of a CLI run and left for idle reaping
after a `/v1/exec` run. Found by reading code; not driven.

**C-66 `/v1/protocol` reports the default, not the setting.** `nativeElicit` is
the compiled default rather than the effective configuration
(`internal/daemon/routes_proto.go:530`), on the one endpoint whose comment
says it cannot disagree with the code (`internal/daemon/routes_proto.go:495-497`).

**C-67 A script name locally, a path remotely.** Already tracked as #106: the
CLI resolves a script name against a search path, and `/v1/exec` treats `file`
as a path.

## E. mcpx's documents against its code

`docs/protocol.md` at `05c78b2` against the code at `05c78b2`. The PROTOCOL work
under way is rewriting that document; these are recorded so a reviewer can
check each is gone.

| # | `docs/protocol.md` says | the code does | where |
| --- | --- | --- | --- |
| E-1 | `initialize` accepts "any legacy version" (§2.1) | 2024-11-05 and unknown versions are refused `-32022` | `internal/mcpserver/server.go:844-859` |
| E-2 | `logging` declared for 2025-03-26 to 2025-11-25, "removed in 2026-07-28" (§2.2) | never declared; 2026 deprecated it rather than removing it | `internal/mcpserver/server.go:1398-1403`; `schema/2026-07-28/schema.ts:808` |
| E-3 | `listChanged` and `subscribe` ✓ in every revision (§2.2) | false over HTTP; true over stdio and never delivered | `internal/mcpserver/server.go:1389-1402` |
| E-4 | "declares none of them outside the revision that defines them" (§2.1) and `extensions[tasks]` ✓ for 2025-11-25 (§2.2) | `extensions` does not exist in 2025-11-25 | `internal/mcpserver/server.go:1404-1417` |
| E-5 | a stateless modern connection's questions "stay with the broker" (§3.3) | the call is abandoned with `-32603` after eight immediate rounds | `internal/mcpserver/ask.go:195-209` |
| E-6 | `sampling` declared upstream "only when a handler exists" (§4) | always declared; the daemon always installs a handler | `internal/daemon/hooks.go:69` |
| E-7 | `roots/list` answers "the configured roots" (§4.1) | always `[]` | `internal/daemon/server.go:176` |
| E-8 | older upstream revisions "on request (`protocol: force-legacy`)" (§1) | `force-legacy` only disables fallback; 2025-11-25 is always offered | `internal/mcpclient/client.go:261-267` |
| E-9 | `notifications/tasks/status` is dropped because "mcpx polls task state" (§4.2) | mcpx never creates upstream tasks, so nothing is polled | `internal/mcpclient/client.go:845-858` |
| E-10 | OAuth "declared and not performed" (§6) | the whole per-server `auth` block (bearer, basic, header, query, env) is never applied; `mcpauth.Auth.Resolve` has no caller | `internal/mcpauth/mcpauth.go:93`; `internal/config/config.go:141` |
| E-11 | `resources/subscribe` "forwards from the same bus" (§2.1) | over HTTP it cannot push, and mcpx never subscribes upstream | `internal/mcpserver/server.go:595-620` |
| E-12 | `subscriptions/listen` sends "only the four notification kinds" (§2.1) | over HTTP: 202, empty body, no stream (W17) | `internal/mcpserver/server.go:622-640` |
| E-13 | inbound cancellation "does not reach an upstream call" (§6) | true of the notification; an HTTP disconnect does propagate by context | `internal/mcpserver/server.go:1521-1538` |
| E-14 | the 2025-03-26 schema "was not among" those read (§1) | it is available now, and it confirms the claims made about it | `schema/2025-03-26/schema.ts` |
| E-15 | `mcpSession` comment: a host has "no way to tell us" its session | opencode v2 sends `_meta["ai.opencode/sessionID"]` | `internal/cli/serve.go:289-290` |
| E-16 | server.go header: "Ten tools instead of three hundred" | 61 tools (W6) | `internal/mcpserver/server.go:12-13` |
| E-17 | `readArtifact`: "resources/read carries text" | `BlobResourceContents.blob` exists in every revision | `internal/cli/serve.go:704-707`; `schema/2024-11-05/schema.ts:506-512` |
| E-18 | `revisions.go`: 2026 servers emit logs "at a level of their own choosing" | 2026: no `logLevel`, no log messages | `internal/mcpserver/revisions.go:72-73`; `2026-07-28/server/utilities/logging.mdx:62-64` |

The existing comparison documents also say things the pinned sources do not:

| # | document says | pinned source says | where |
| --- | --- | --- | --- |
| E-20 | `OPENCODE-V2.md`: opencode code mode provides `fetch` as an extension | 2.0.3's execute description says "Do not use `fetch`", and core registers no extension | `OPENCODE-V2.md:51`; `v2:packages/core/src/codemode/tool.ts:63` |
| E-21 | `OPENCODE-V2.md`: "If you stay on opencode v1 … none of v2's Code Mode is available to you today" | v1 enables code mode whenever `OPENCODE_EXPERIMENTAL` is set, and this machine sets it | `OPENCODE-V2.md:222`; `v1:packages/opencode/src/effect/runtime-flags.ts:48`; `nix:flake.nix:297` |
| E-22 | `OPENCODE-V2.md`: tagged templates present; "251 checked items, 17 gaps"; "10,764 lines" | 2.0.3: tagged templates a gap; 211 checked and 30 open; 9,456 lines | `OPENCODE-V2.md:104-105`, `:19`; `v2:packages/codemode/interpreter-support.md:60` |
| E-23 | `OPENCODE-V2.md`: the exclusion plugin is `mcp-codemode-defaults.ts` and covers code-mode servers generally | it is `mcp-codemode-exclusion.ts` and matches `executor` only | `OPENCODE-V2.md:53`; `v2:packages/core/src/plugin/mcp-codemode-exclusion.ts:7-16` |
| E-24 | `docs/opencode-plugin.md`: a server plugin has `client.tui.*`, "identical in v1 and v2" | v2's server plugin context has no `client`; toasts are TUI-plugin only | `docs/opencode-plugin.md:111`, `:361`; `v2:packages/plugin/src/promise/plugin.ts:26` |
| E-25 | `docs/opencode-plugin.md`: a tool with a permission prompt works in v1 and v2 | v2's tool context has no `ask` | `docs/opencode-plugin.md:272`; `v2:packages/schema/src/tool.ts:14` |
| E-26 | `docs/opencode-plugin.md`: v2's `PUT /api/experimental/session/:id/environment` is the API for session env | it replaces the whole environment, the TUI sets it itself on every session view, and plugins have no HTTP client | `docs/opencode-plugin.md:86`, `:412`; `v2:packages/tui/src/app.tsx:511` |
| E-27 | `docs/opencode-plugin.md`: v1 execution timeout "one value, 30 s" | 30 s is connect and list; `tools/call` defaults to the SDK's 60 s | `docs/opencode-plugin.md:178`; `v1:packages/opencode/src/mcp/index.ts:672` |
| E-28 | `docs/opencode-plugin.md`: v1 "will get `cancel`" for an elicitation | a v1 client answers `-32601`; mcpx never sends it one | `docs/opencode-plugin.md:184`; `v1:packages/opencode/src/mcp/index.ts:77` |
| E-29 | `ASSESSMENT.md`: lootbox runs scripts `--allow-all` | `--allow-net` | `ASSESSMENT.md:73`; `.lootbox/src/lib/constants.ts:78` |

## F. mcpx sends what the peer did not negotiate

"Send conservatively" is mcpx's own rule ([`../protocol.md`](../protocol.md)).
These are the places where it breaks it at `05c78b2`:

| # | sent | to | where |
| --- | --- | --- | --- |
| F-1 | `-32022`, a 2026 code | a legacy client's `initialize` | `internal/mcpserver/server.go:506-509` |
| F-2 | a top-level `serverInfo` in `DiscoverResult` (2026 puts it in `_meta`) | 2026 clients | `internal/mcpserver/server.go:535` |
| F-3 | `Mcp-Session-Id` | 2026 clients (`server/discover`) and failed legacy handshakes | `internal/mcpserver/server.go:1214-1229` |
| F-4 | the core `tasks` capability | 2026 clients | `internal/mcpserver/server.go:1404-1417` |
| F-5 | `capabilities.extensions` | 2025-11-25 clients | `internal/mcpserver/server.go:1404-1417` |
| F-6 | `listChanged: true`, `subscribe: true` with nothing behind them | legacy stdio clients | `internal/mcpserver/server.go:1389-1402` |
| F-7 | `title` on prompts | 2025-03-26 clients | `internal/mcpserver/server.go:724-740` |
| F-8 | the upstream's raw elicitation `params`, `elicitationId` included | 2026 clients | `internal/mcpserver/ask.go:256-265` |
| F-9 | sampling requests with `tools` or `includeContext`, unchecked against `sampling.tools` or `sampling.context` | hosts | `internal/mcpserver/conn.go:211` |
| F-10 | responses to notifications (an error for an unknown one) | any client | `internal/mcpserver/server.go:766` |
| F-11 | `sampling` declared with no answerer behind it | every upstream | `internal/daemon/hooks.go:69` |
| F-12 | legacy-shaped capabilities in 2026 `_meta`, and no mention of url elicitation or tasks | modern upstreams | `internal/mcpclient/modern.go:46-58` |
| F-13 | `MCP-Protocol-Version: 2025-11-25` regardless of what was negotiated | legacy HTTP upstreams that negotiated lower | `internal/mcpclient/http.go:81-85` |
| F-14 | `logging/setLevel`, a method 2026 removed | modern upstreams | `internal/daemon/hooks.go:29-33` |
| F-15 | `resultType: "complete"` on a task handle whose extension value is `"task"` | 2026 clients | `internal/mcpserver/server.go:452-453` |
