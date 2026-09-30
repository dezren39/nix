# Process model and statelessness

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  What each MCP revision expects of server state and connection lifetime, and how mcpx, opencode, lootbox and Cloudflare arrange processes.
```

Until 2025-11-25 an MCP connection is a stateful session; 2026-07-28 makes the protocol stateless by default, so a
server may not use earlier requests, the connection, or the process as context, and anything that spans requests
travels as an explicit identifier. For mcpx the question is sharper than for a plain server, because it is a daemon
that pools upstream processes for many callers: what identifies a caller decides which browser a call reaches, and
today every HTTP host of the daemon is one caller. The full list of state mcpx keeps is in
[../stateless.md](../stateless.md); this file records the items that are differences.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| PM-01 | One stateful session per client–server connection | `2026-07-28 removes` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | ✓ — legacy caps and version held per `Conn` | + low | S | low |
| PM-02 | Server must not use earlier requests on a connection as context | `2026-07-28 has` | `25-11 — · 26-07 MUST` | ✓ — modern path reads `_meta` first | + med | S | low |
| PM-03 | Cross-request state named by an explicit identifier each request | `2026-07-28 has` | `25-11 — · 26-07 MUST (handles are a pattern)` | partial — tasks are handles; pool key is not | + high | S | med |
| PM-04 | A connection or process is not a conversation | `2026-07-28 has` | `25-11 — · 26-07 SHOULD/SHOULD NOT` | partial — pool scope keyed on a process id | + high | S | high |
| PM-05 | Server state carried through the client in `requestState` | `2026-07-28 has` `has better replacement` | `25-11 — · 26-07 ✓` | partial — token names daemon-side call; bound to session | + med | M | med |
| PM-06 | A broken stream loses the request; client re-issues | `2026-07-28 has` | `25-03..25-11 resumable · 26-07 lost` | n/a — upstream retry policy not examined | + low | S | med |
| PM-07 | State that 2026 still permits | `2026-07-28 has` | `26-07 listen, tasks, requestState, handles, legacy sessions` | ✓ — tasks and legacy sessions allowed | + low | S | low |
| PM-08 | Request with neither modern `_meta` nor a prior `initialize` | `specs conflict` | `26-07 silent for dual-era stdio; HTTP MAY assume 2025-03-26` | acc. — served as 2025-03-26 | + low | S | low |
| PM-09 | What "supports 2026-07-28" minimally means | `2026-07-28 has` | `24-11..25-11 base + lifecycle · 26-07 base + versioning + patterns` | partial — discover and listen shapes wrong | + high | M | high |
| PM-10 | Every HTTP MCP host shares one pool session key | `opencode v2 has` `mcpx missing` | mcpx `/mcp` | ✗ — key is the daemon's pid (#211) | + high | S | high |
| PM-11 | Stateful server isolation per caller | `mcpx has, others don't` `opencode v1 has` `opencode v2 has` | `mcpx per session · opencode v1 per directory · v2 per Location · lootbox shared · CF per execution id` | ✓ — session-scoped process leases | + high | S | med |
| PM-12 | `/v1/exec` session-scoped instances outlive the run | `mcpx missing` | mcpx only | ✗ — never marked ephemeral; waits for idle reap (#219) | + med | S | med |
| PM-13 | MCP surface built once per daemon, never rebuilt | `mcpx missing` | mcpx only | ✗ — `sync.Once`; new adapters invisible (#219) | + med | S | med |
| PM-14 | Daemon's MCP server calls the daemon over its own socket | `mcpx has, others don't` | mcpx only | ✓ — one path for stdio and HTTP | − latency | M | low |
| PM-15 | `/v1/protocol` reports `nativeElicit` from the default | `mcpx missing` | mcpx only | ✗ — compiled default, not the setting (#219) | + low | S | low |
| PM-16 | opencode process topology: one process vs shared daemon | `opencode v1 has` `opencode v2 has` | `opencode v1 TUI + Worker per window · v2 background daemon` | n/a — mcpx is already a daemon | + med | S | med |
| PM-17 | MCP connection scope: per directory vs per Location | `opencode v1 has` `opencode v2 has` | `opencode v1 per directory per process · v2 per Location per daemon` | n/a — mcpx must tell sessions apart | + med | S | med |
| PM-18 | When MCP servers start: awaited all vs forked each | `opencode v1 has` `opencode v2 has` | `opencode v1 all at first use · v2 async per server` | n/a — mcpx connect time matters on v1 | + low | S | low |
| PM-19 | MCP server lifetime: instance vs daemon, never idle | `opencode v1 has` `opencode v2 has` | `opencode v1 until dispose/exit · v2 no idle TTL` | n/a — `mcpx serve` may live for days | + low | S | med |
| PM-20 | No automatic restart when an MCP server dies | `opencode v1 has` `opencode v2 has` | `opencode v1 failed · v2 failed` | n/a — a dead `mcpx serve` stays dead | + med | S | med |
| PM-21 | opencode database filename depends on release channel | `opencode v1 has` `opencode v2 has` `mcpx missing` | `opencode.db: v1 latest/beta/prod · v2 also dev/next` | ✗ — `mcpx stats opencode` reads `opencode.db` only (#219) | + low | S | med |
| PM-22 | lootbox connects every server at startup and pings | `lootbox has` | `lootbox fork (monitor) · deployed upstream (no monitor)` | n/a — lazy pools, min 0 | + low | S | low |
| PM-23 | lootbox re-fetches every schema on every discovery | `lootbox has` `has better replacement` | lootbox | ✓ — persisted cache invalidated by list_changed | − none | S | low |
| PM-24 | lootbox multi-instance strategies and port rewriting | `lootbox has` `has better replacement` | lootbox fork only | n/a — isolates callers, not daemons | − misplaced | S | low |
| PM-25 | Per-run script and client setup | `lootbox has` `has better replacement` | `lootbox HTTP import + --reload · mcpx content-addressed on disk` | ✓ — compile cache stays warm | − none | S | low |

## PM-01 One stateful session per client–server connection

- **What.** 2024-11-05 through 2025-11-25 describe MCP as a stateful session protocol: each client holds one session per
  server, opened by `initialize`, and capabilities and version hold for the whole session.
- **Where.** 2024-11-05 to 2025-11-25. 2026-07-28 removes it (PM-02); a dual-era server may still run legacy sessions
  for clients that open with `initialize`.
- **mcpx @ 05c78b2.** Legacy capabilities and version are stored per `Conn` (`internal/mcpserver/conn.go:101-112`); on
  HTTP a legacy `Conn` lives behind `Mcp-Session-Id`, on stdio it is the process.
- **Value to mcpx.** + low: kept for legacy clients; nothing to change.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** The session lifetime and reaping rules (30-minute idle, reaped only when another session is issued) are
  recorded in the lifecycle and transports areas and in [../stateless.md](../stateless.md).
- **Sources.** `2024-11-05/architecture/index.mdx:8` "isolating concerns. Built on JSON-RPC, MCP provides a stateful session protocol focused"; `2024-11-05/architecture/index.mdx:61` "- Establishes one stateful session per server"; `internal/mcpserver/conn.go:101-112`

## PM-02 Server must not use earlier requests on a connection as context

- **What.** Servers MUST NOT rely on prior requests over the same connection to establish context (capabilities,
  protocol version, client identity); every request supplies it in `_meta`.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The modern path reads `_meta` before any connection state (`internal/mcpserver/conn.go:133-136`).
  The legacy HTTP question-and-answer path relies on sessions by design, for legacy peers only.
- **Value to mcpx.** + med: a stateless front lets many agents share one daemon without cross-talk.
- **Effort.** S (done for modern peers).
- **Risk.** Low.
- **Detail.** The same page adds that no state "should be inferred from previous requests, even those on the same
  connection or stream"; capabilities that arrive per request mean mcpx may not remember an earlier request's
  `elicitation` declaration either.
- **Sources.** `2026-07-28/basic/index.mdx:191` "- Servers **MUST NOT** rely on prior requests over the same connection to"; `internal/mcpserver/conn.go:133-136`

## PM-03 Cross-request state named by an explicit identifier each request

- **What.** State that spans requests (long-running tasks, application handles) MUST be referenced by an explicit
  identifier the client passes on each request. With sessions gone, a server that needs cross-call state mints a
  handle (`create_basket()` → `basket_id`) and takes it as an ordinary tool argument; there is no protocol type for
  handles.
- **Where.** 2026-07-28 (SEP-2567).
- **mcpx @ 05c78b2.** Task ids (`tsk-<16 hex>`) are such handles. The pool scope key is not: for `/mcp` hosts it comes
  from the serving process (PM-10), not from anything in the request.
- **Value to mcpx.** + high: the 2026 way to give a host its own browser is a handle or a per-request key, which is
  exactly what mcpx's pool needs as input.
- **Effort.** S.
- **Risk.** Med: without it the pool key stays process-derived.
- **Detail.** SEP guidance for handles: opaque, at least 128 random bits when unauthenticated, validate
  `(handle, principal)` when authenticated, clear expiry errors. mcpx's task store has no principal binding (tasks
  area).
- **Sources.** `2026-07-28/basic/index.mdx:200` "- State that needs to span multiple requests (e.g., long-running tasks,"; `2026-07-28/changelog.mdx:12` "Servers that need cross-call state use explicit, server-minted handles passed as ordinary tool arguments"; `seps/2567-sessionless-mcp.md:98` "That third point is **not a protocol change**. There is no `handles/*` method"

## PM-04 A connection or process is not a conversation

- **What.** Servers SHOULD handle requests from many tasks, threads or conversations; SHOULD NOT require a client to
  reuse the same connection or process for related operations; clients SHOULD NOT make a conversation the lifetime of
  a stdio process. The note spells it out: a server must not treat connection or process identity as a proxy for
  conversation or session continuity.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** As a server: `mcpx serve` keys pool scope on its own pid (`mcp-<pid>`), which is exactly process
  identity standing in for a conversation; over HTTP it is worse (PM-10). As a client: the `session` scope gives each
  session key its own upstream process (PM-11), tying a stdio child's lifetime to a conversation.
- **Value to mcpx.** + high: the per-request identity the spec wants is what would fix PM-10.
- **Effort.** S.
- **Risk.** High: sharing a stateful upstream across callers is the failure mcpx exists to prevent.
- **Detail.** The client-side SHOULD NOT is aimed at modern servers that keep state in handles. A legacy stateful server
  (a browser MCP) has nowhere else to keep state, so per-session processes remain the only isolation for it; the
  tension is real but only for servers that could have used handles.
- **Sources.** `2026-07-28/basic/index.mdx:194` "- Servers **SHOULD** be prepared to handle requests associated with multiple"; `2026-07-28/basic/index.mdx:196` "- Servers **SHOULD NOT** require that a client reuse the same connection or process to"; `2026-07-28/basic/index.mdx:198` "- Clients **SHOULD NOT** use an individual task, thread, or conversation as the"; `2026-07-28/basic/index.mdx:207` "transport, and a server must not treat connection or process identity as a"; `internal/cli/serve.go:296` "return fmt.Sprintf(\"mcp-%d\", os.Getpid())"

## PM-05 Server state carried through the client in `requestState`

- **What.** In 2026 a server that needs input returns `input_required` with an opaque `requestState`; the client echoes
  it on the retry. The server MUST treat it as attacker-controlled and integrity-protect it if it matters, and SHOULD
  bind principal, TTL and request digest. The state lives in the client, not in a session.
- **Where.** 2026-07-28. The mechanics (which methods, retry ids, `inputResponses`) are in the MRTR area.
- **mcpx @ 05c78b2.** The token is HMAC-signed with a per-process key and time-bound (`proto.stateTTL` 30m), but it
  names a call id whose live upstream call and question table stay in the daemon (`askTable`, result kept
  `proto.askTTL` 15m), and it is bound to the `Mcp-Session-Id` that 2026 removed. So a stateless 2026 client cannot
  complete it (elicitation area), and the daemon holds state behind the token.
- **Value to mcpx.** + med: binding to a principal (or nothing, since the HMAC already authenticates) plus a
  method/params digest would make it work for session-less clients.
- **Effort.** M.
- **Risk.** Med: a live upstream call cannot be serialised into a token, so mcpx will always hold some state here; it
  is the server-minted-handle pattern carried in `requestState`, which 2026 permits (PM-07).
- **Detail.** The signing key is per `mcpserver.Server`, so a daemon restart invalidates every outstanding token.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:232` "1. If a client request contains a `requestState` field, servers **MUST** treat `requestState` as an attacker-controlled input."; `internal/mcpserver/state.go:29-51`; `internal/mcpserver/server.go:161-163`; `internal/daemon/routes_proto.go:53-65`

## PM-06 A broken stream loses the request; client re-issues

- **What.** No redelivery: if a response stream breaks, the in-flight request is lost and the client MUST re-issue it
  with a new id; on stdio a crash means restart and retry.
- **Where.** 2026-07-28. 2025-03-26 through 2025-11-25 had SSE resumability instead (transports area).
- **mcpx @ 05c78b2.** Not examined for upstream calls (pool retry policy). Restart backoff settings exist in
  `plumbing` (`internal/defaults/defaults.json:49-50`).
- **Value to mcpx.** + low: retry-on-crash is spec-aligned for modern upstreams.
- **Effort.** S.
- **Risk.** Med: re-issuing a non-idempotent tool executes it twice; nothing makes the server deduplicate, and
  `idempotentHint`/`destructiveHint` are the only signal (an unwritten hazard).
- **Detail.** The MUST applies to the client's intent to get a result; mcpx as a proxy has to decide whether a lost
  upstream call is retried or surfaced to the host.
- **Sources.** `2026-07-28/changelog.mdx:28` "A broken response stream loses the in-flight request; clients **MUST** re-issue it as a new request with a new request ID"; `2026-07-28/basic/transports/stdio.mdx:112` "Because the protocol is stateless, any in-flight requests are simply lost and"

## PM-07 State that 2026 still permits

- **What.** Stateless "by default", not entirely: open `subscriptions/listen` streams (request-scoped); durable tasks
  in the tasks extension, polled by id; client-held `requestState`; application handles in tool arguments;
  per-request auth context; process-lifetime state on stdio (SEP: SHOULD NOT rely on it); legacy sessions of a
  dual-era server.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Tasks (legacy shape) and legacy HTTP sessions both exist (`internal/mcpserver/server.go:475-502`,
  `1208-1230`); both are permitted. What is not permitted is minting a session for a modern peer, which `server/discover`
  does (lifecycle and transports areas).
- **Value to mcpx.** + low: tells mcpx which of its stores need no change.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** The inventory in [../stateless.md](../stateless.md) classifies each mcpx store against this list.
- **Sources.** `2026-07-28/basic/index.mdx:211` "Long-lived requests like"; `docs/extensions/tasks/overview.mdx:56` "initial status, TTL, and suggested polling interval. The task is durably"; `seps/2567-sessionless-mcp.md:237` "such servers SHOULD NOT rely on process-lifetime state and SHOULD migrate to explicit handles"; `seps/2575-stateless-mcp.md:726` "Not entirely (hence 'by default')."

## PM-08 Request with neither modern `_meta` nor a prior `initialize`

- **What.** 2026-07-28 defines a modern request (with `_meta`) and a legacy one (after `initialize`), and says
  modern-only servers reject a missing `_meta` with `-32602`. It is silent on a dual-era server receiving a bare
  request with no handshake; on HTTP a dual-era server MAY treat a missing `MCP-Protocol-Version` as 2025-03-26.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Serves it as `Oldest` = 2025-03-26 (`internal/mcpserver/conn.go:140-145`), consistent with the HTTP
  MAY, on stdio as well.
- **Value to mcpx.** + low: accept liberally; keep.
- **Effort.** S.
- **Risk.** Low: a modern client that forgot `_meta` silently gets legacy shapes. The stdio probe advice exists because
  lenient legacy servers do exactly this.
- **Detail.** mcpx also ignores the legacy HTTP version header on such a request (lifecycle area).
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:278` "`2025-06-18` (which did not define the `MCP-Protocol-Version` header) **MAY**"; `2026-07-28/basic/versioning.mdx:176` "- A request carrying modern per-request `_meta` is served statelessly"; `internal/mcpserver/conn.go:140-145`

## PM-09 What "supports 2026-07-28" minimally means

- **What.** Legacy revisions: all implementations MUST support the base protocol and lifecycle management. 2026-07-28:
  base protocol, versioning and the message patterns (request/response, MRTR, subscribe/notify) are MUST; everything
  else is optional.
- **Where.** 2024-11-05..2025-11-25 vs 2026-07-28.
- **mcpx @ 05c78b2.** Advertises 2026-07-28, but `server/discover` and `subscriptions/listen` shapes are wrong
  (lifecycle and subscriptions areas).
- **Value to mcpx.** + high: the minimum is discover, per-request `_meta`, MRTR and listen, and nothing more.
- **Effort.** M.
- **Risk.** High: advertising 2026-07-28 while its mandatory patterns are malformed is "declared but not delivered".
- **Detail.** Tools, resources, prompts and the rest are optional in 2026, so a correct minimal 2026 surface is small.
- **Sources.** `2024-11-05/basic/index.mdx:30` "All implementations **MUST** support the base protocol and lifecycle management"; `2026-07-28/basic/index.mdx:17` "All implementations **MUST** support the base protocol, versioning,"

## PM-10 Every HTTP MCP host shares one pool session key

- **What.** Calls from `/mcp` run with `CallContext{SessionID: mcpSession()}`, which is `$MCPX_SESSION_ID` or
  `mcp-<pid>` of the process running the MCP server. Over HTTP that process is the daemon, so every remote host lands on
  the same session-, pid- or cwd-scoped instance.
- **Where.** mcpx. opencode v2 sends `_meta["ai.opencode/sessionID"]` on every `tools/call`, which mcpx drops (`_meta`
  area).
- **mcpx @ 05c78b2.** `internal/cli/serve.go:287-297`; used by `Call`, `Exec`, `ReadResource`, `GetPrompt` and the asker
  (`internal/cli/serve.go:111-112`, `139`, `575-576`, `597-598`; `internal/cli/serve_ask.go:34-37`).
- **Value to mcpx.** + high: per-host isolation over HTTP, from `ai.opencode/sessionID`, a server-minted handle
  (PM-03), or client identity.
- **Effort.** S.
- **Risk.** High: two opencode sessions (or two opencode windows, PM-16) on one daemon drive one browser.
- **Detail.** `mcpx serve` over stdio gets per-host isolation for free, since each host spawns its own process; the
  comment "one per process is the honest answer" is true there and false for the daemon. `OPENCODE-V2.md:92-94` says
  nobody provides per-session isolation for opencode v2; reading the `_meta` key would.
- **Sources.** `internal/cli/serve.go:296` "return fmt.Sprintf(\"mcp-%d\", os.Getpid())"; `internal/cli/serve_ask.go:31-33` "one per process is the honest answer"; `v2:packages/core/src/mcp/client.ts:278`

## PM-11 Stateful server isolation per caller

- **What.** Who shares one instance of a stateful upstream (a browser). mcpx gives each session key its own process and
  pins the lease for a whole script run. opencode v1 has one connection per server per directory per opencode process,
  shared by every session in that window; v2 one per server per Location, shared by every session in every window
  attached to the daemon. lootbox holds one client per server for everyone. Cloudflare's connections belong to one
  Agent, and a connector can key resources by the execution id.
- **Where.** As listed; opencode v2's only mitigation is the advisory `_meta` session id.
- **mcpx @ 05c78b2.** Scopes global, repo, worktree, cwd, session, parent-session, pid and call
  (`internal/config/config.go:50-71`), each key its own process; sharing shared or exclusive (the package comment still
  names the stateful modes "pooled" and "session", `internal/pool/pool.go:3-16`; key derivation
  `internal/config/scope.go:46-64`). A `session` scope with no session id degrades to `call`. Instances live until
  `IdleTimeout` with no holders, with a `Min` floor and a `Max` cap evicted LRU (`internal/pool/pool.go:238-244`,
  `697-723`).
- **Value to mcpx.** + high: concurrent Chrome sessions are the problem mcpx exists to solve; v2 makes the problem worse,
  which strengthens the case.
- **Effort.** S (done; the key is the open question, PM-10).
- **Risk.** Med: see PM-12 for instances that are not released.
- **Detail.** The pool is invisible to MCP hosts and compatible with 2026 statelessness; only its key matters, and the
  key could come from request `_meta` or a handle (PM-03). Cloudflare's equivalent is resource-level, not process-level:
  `ctx.executionId` passed to connector methods, plus `onPassEnd`/`disposeExecution` hooks.
- **Sources.** `internal/pool/pool.go:14` "\"session\" mode is the important one: a lease is pinned to a session key for"; `v1:packages/opencode/src/cli/cmd/tui.ts:210`; `v2:packages/core/src/mcp/index.ts:152`; `.lootbox/src/lib/external-mcps/mcp_client_manager.ts:49`; <https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#resource-lifetimes> "Connector methods receive the stable execution ID, which can key durable resource metadata across passes."

## PM-12 `/v1/exec` session-scoped instances outlive the run

- **What.** After every run `execsvc.RunWith` calls `Release(session)`, which reaches `Registry.ReleaseCaller`; that
  stops only keys where `CallerOwned` is true (`Ephemeral`, or a `call:` prefix). `scriptEnv` never sets
  `MCPX_EPHEMERAL`, so for `/v1/exec` runs, including the plugin's `mcpx_exec` with its generated `exec-<runID>`
  session, session-scoped instances such as Chrome are not released; they wait for idle reaping.
- **Where.** mcpx only. The CLI path sets `MCPX_EPHEMERAL` and is unaffected.
- **mcpx @ 05c78b2.** `internal/execsvc/execsvc.go:481-485`; `internal/config/scope.go:39-41`;
  `internal/execsvc/execsvc.go:573-593`; the generated client forwards `x-mcpx-ephemeral` only from that variable
  (`internal/codegen/emit.go:533`); CLI sets it at `internal/cli/commands.go:694`.
- **Value to mcpx.** + med: pooled browsers return promptly.
- **Effort.** S.
- **Risk.** Med: combined with the plugin sending no session (plugin-APIs area), a Chrome script run through the plugin
  gets a fresh browser every call, and each one lingers until idle timeout.
- **Detail.** Inferred from code; not run.
- **Sources.** `internal/execsvc/execsvc.go:481-485`; `internal/config/scope.go:39-41`; `internal/codegen/emit.go:533`; `internal/cli/commands.go:694`

## PM-13 MCP surface built once per daemon, never rebuilt

- **What.** `lazyMCP` builds the MCP server on the first `/mcp` request (adapters, OpenAPI documents, op tools) under a
  `sync.Once` and never rebuilds it. A config reload that adds an adapter or an API does not change `tools/list`, and
  `tools.listChanged` could never fire for mcpx's own list.
- **Where.** mcpx only.
- **mcpx @ 05c78b2.** `internal/cli/serve_ask.go:234-244`; `internal/cli/serve.go:332-347`.
- **Value to mcpx.** + med: adapters added at runtime appear without a restart.
- **Effort.** S.
- **Risk.** Med: users restart the daemon to see new adapter tools.
- **Detail.** The build uses the first request's context, so a cancelled first request would cache a context error for
  the daemon's life (inferred from code, not triggered).
- **Sources.** `internal/cli/serve_ask.go:242` "l.once.Do(func() { l.srv, l.err = l.app.MCPServer(ctx) })"

## PM-14 Daemon's MCP server calls the daemon over its own socket

- **What.** Every `/mcp` tool call goes back to the same daemon over its own `/v1` socket (`b.app.ensure(ctx)`, then
  `c.Call`), adding a hop and a second HTTP request per call. The same code serves stdio `mcpx serve`, where it is the
  right design.
- **Where.** mcpx only.
- **mcpx @ 05c78b2.** `internal/cli/serve.go:30-117`; daemon mount at `internal/cli/root.go:190-195`; rationale at
  `docs/protocol.md:341-344`.
- **Value to mcpx.** − latency and a failure mode (a daemon that cannot reach its own socket). + one code path, and it
  is what makes HTTP disconnects cancel upstream calls through context propagation.
- **Effort.** M to short-circuit.
- **Risk.** Low.
- **Detail.** Short-circuiting would have to keep the cancellation chain intact.
- **Sources.** `internal/cli/serve.go:104` "c, err := b.app.ensure(ctx)"; `docs/protocol.md:341-344`

## PM-15 `/v1/protocol` reports `nativeElicit` from the default

- **What.** The status endpoint says `"nativeElicit": true` from `defaults.ProtoNative` even when `proto.native=false`
  is configured, which does disable native asking.
- **Where.** mcpx only.
- **mcpx @ 05c78b2.** `internal/daemon/routes_proto.go:530`; the setting is honoured at `internal/cli/serve.go:320-326`.
- **Value to mcpx.** + low: the endpoint's stated reason to exist is that it "cannot disagree" with the code.
- **Effort.** S.
- **Risk.** Low: a declared-versus-delivered report.
- **Detail.** The rest of `asServer` comes from the same tables the code consults (`internal/daemon/routes_proto.go:495-497`).
- **Sources.** `internal/daemon/routes_proto.go:530` "\"nativeElicit\": defaults.ProtoNative,"; `internal/cli/serve.go:320` "if a.Settings().Bool(\"proto.native\") {"

## PM-16 opencode process topology: one process vs shared daemon

- **What.** opencode v1 is one process per window: the TUI on the main thread and the server in a `Worker`, reached by
  an in-process fetch shim (`http://opencode.internal`); a TCP port opens only with `--port`, `--hostname` or mDNS.
  opencode v2's default command resolves or starts a shared background service ("Starting background server…") on
  127.0.0.1:0xc0de (49374) for the latest/dev/beta/next channels, and every TUI connects to it over HTTP; `--standalone`
  opts out.
- **Where.** v1 and v2. v2 also deletes `packages/opencode`: the binary is `packages/cli`, running `@opencode/server`
  (`/api/*`, typed by `@opencode/protocol`) over `@opencode/core` Effect services built per Location.
- **mcpx @ 05c78b2.** Not affected directly; the plugin infers "headless" from `isMainThread`
  (`plugin/opencode/mcpx-session.ts:161`), which is a v1 assumption.
- **Value to mcpx.** + med: v2 is architecturally close to mcpx (daemon plus thin clients) and offers a typed HTTP API
  mcpx could call.
- **Effort.** S.
- **Risk.** Med: under v2 one server, one plugin instance per Location and one MCP connection per server per Location
  serve every window (PM-11).
- **Detail.** v2's `Mode` is `"default" | "service" | "stdio"`, and the service may be registered with the OS. `opencode
  serve`/`run` in v1 put the server on the main thread.
- **Sources.** `v1:packages/opencode/src/cli/cmd/tui.ts:210` "const worker = new Worker(file, {"; `v1:packages/opencode/src/cli/cmd/tui.ts:246`; `v2:packages/cli/src/commands/handlers/default.ts:31`; `v2:packages/cli/src/services/service-config.ts:35` "return 0xc0de"; `v2:packages/cli/src/services/server-connection.ts:41`; `v2:packages/cli/package.json:7`

## PM-17 MCP connection scope: per directory vs per Location

- **What.** v1 keeps MCP clients in `InstanceState`, a cache keyed by directory; v2 keeps a `ServerEntry {client?,
  tools?, prompts?}` map in a Location-scoped service (directory plus workspace id). All sessions of that directory or
  Location share each server's single connection.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** n/a; opencode will never give mcpx more than one connection per server entry per place, so mcpx
  must tell sessions apart itself (PM-10).
- **Value to mcpx.** + med.
- **Effort.** S.
- **Risk.** Med.
- **Detail.** v2 canonicalises the Location key before use. v2 spawns stdio servers in the Location's `Environment`,
  which may be a remote workspace (`v2:packages/core/src/mcp/client.ts:198`); v1 spawns them on the host.
- **Sources.** `v1:packages/opencode/src/effect/instance-state.ts:49`; `v1:packages/opencode/src/mcp/index.ts:492`; `v2:packages/core/src/mcp/index.ts:67`; `v2:packages/core/src/mcp/index.ts:152`; `v2:packages/core/src/location-service-map.ts:21`

## PM-18 When MCP servers start: awaited all vs forked each

- **What.** v1 connects every configured server concurrently when MCP state is first touched, so the first MCP access
  in a directory waits for the slowest server (up to its timeout). v2 forks each `startServer`, publishes
  `ToolsChanged` as each lands, and only `callTool`/`prompt`/`readResource` wait on that server's startup latch.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** n/a.
- **Value to mcpx.** + low: `mcpx serve` start time delays every v1 session's first request.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** v1's MCP state is not part of instance bootstrap; it materialises lazily on the first `InstanceState.get`.
- **Sources.** `v1:packages/opencode/src/mcp/index.ts:505`; `v1:packages/opencode/src/mcp/index.ts:528`; `v2:packages/core/src/mcp/index.ts:499`; `v2:packages/core/src/mcp/index.ts:507`

## PM-19 MCP server lifetime: instance vs daemon, never idle

- **What.** v1 closes clients (and kills descendants) when the directory instance is disposed or the process exits. v2
  retains Location service graphs with `idleTimeToLive: Duration.infinity`, so MCP servers live as long as the daemon.
- **Where.** opencode v1 and v2.
- **mcpx @ 05c78b2.** n/a; an `mcpx serve` child of the v2 daemon can live for days.
- **Value to mcpx.** + low: mcpx must not assume a short-lived parent.
- **Effort.** S.
- **Risk.** Med: resource leaks in `mcpx serve` surface under v2 first.
- **Detail.** v2 invalidates a Location only on boot failure or explicit invalidation. Teardown differs too: v1
  SIGTERMs descendants; v2 signals the process group, SIGKILL after 2 s (lifecycle area).
- **Sources.** `v1:packages/opencode/src/mcp/index.ts:531`; `v2:packages/core/src/location-services.ts:52` "{ idleTimeToLive: Duration.infinity },"

## PM-20 No automatic restart when an MCP server dies

- **What.** On transport close both versions mark the server `failed: "Connection closed"`, drop its tools and publish
  `ToolsChanged`; neither respawns it. Recovery is a manual `connect`.
- **Where.** opencode v1 and v2. v2 does reconnect when a credential for that server's integration changes.
- **mcpx @ 05c78b2.** n/a; if `mcpx serve` exits, opencode loses mcpx until the user reconnects.
- **Value to mcpx.** + med: the stdio side should survive daemon restarts and upgrades.
- **Effort.** S.
- **Risk.** Med: a daemon upgrade that kills `mcpx serve` silently removes mcpx from every open opencode session.
- **Detail.** 2026-07-28 expects a client to restart a crashed stdio server and retry (PM-06); neither opencode version
  does.
- **Sources.** `v1:packages/opencode/src/mcp/index.ts:448`; `v2:packages/core/src/mcp/index.ts:358`; `v2:packages/core/src/mcp/index.ts:543`

## PM-21 opencode database filename depends on release channel

- **What.** v1 uses `opencode.db` only for the latest, beta and prod channels, else `opencode-<channel>.db`; v2 uses
  `opencode.db` for latest, dev, beta, next and prod. Both honour `OPENCODE_DB` and `OPENCODE_DISABLE_CHANNEL_DB`.
- **Where.** opencode v1 and v2; they disagree on whether `next` gets its own file.
- **mcpx @ 05c78b2.** `mcpx stats opencode` looks only for `opencode.db` (`internal/opencode/opencode.go:37`, `:46`);
  this machine has both `opencode.db` and `opencode-next.db`.
- **Value to mcpx.** + low: enumerate `opencode*.db` or read the channel.
- **Effort.** S.
- **Risk.** Med: stats silently read the wrong opencode's database.
- **Detail.** A v1 `next` build writes `opencode-next.db`; a v2 `next` build writes `opencode.db`.
- **Sources.** `v1:packages/core/src/database/database.ts:49`; `v2:packages/cli/src/database-path.ts:7`; `internal/opencode/opencode.go:37`

## PM-22 lootbox connects every server at startup and pings

- **What.** lootbox connects to every configured server before its HTTP listener opens, keeps one client per server for
  the daemon's lifetime, and (in the fork) runs a health monitor that pings each server, reconnects with exponential
  backoff and gives up after a set number of failures.
- **Where.** lootbox. The deployed daemon on :9420 is not the fork: it is upstream `jx-codes/lootbox@587a5a1` plus four
  patches (`~/.config/nix/flake.nix`, lines 386–398), and has neither the monitor nor a deep `/health` (the live
  `/health` returned `{"status":"ok"}`). The fork-only set also includes the multi-instance strategies (PM-24) and the
  configurable timeout and permissions.
- **mcpx @ 05c78b2.** Lazy pools (`pool.min` 0, `max` 4, idle 5 m, `internal/defaults/defaults.json:2-10`); `Client.Ping`
  exists (`internal/mcpclient/client.go:726-727`).
- **Value to mcpx.** + low: a ping could spot a wedged stateful server before a script blocks on it.
- **Effort.** S.
- **Risk.** Low: pinging an idle lazy server keeps it alive and defeats `idleTimeout`.
- **Detail.** Startup waits for every server (`await clientManager.initializeClients(...)` before `Deno.serve`);
  `ASSESSMENT.md:100` measured 43 s before the port opened. opencode v2 starts asynchronously (PM-18); Cloudflare
  restores an Agent's connections after hibernation.
- **Sources.** `.lootbox/README.md:385-388` "Lootbox automatically monitors MCP server health with periodic `ping()` probes."; `.lootbox/src/lib/rpc/managers/mcp_integration_manager.ts:221-222`; `.lootbox/src/lib/rpc/websocket_server.ts:190`

## PM-23 lootbox re-fetches every schema on every discovery

- **What.** `GET /namespaces`, `/rpc-namespaces` and `/types/:ns` all call `getSchemas()`, which runs `tools/list` and
  `resources/list` against every connected server in turn; the cache accessor `getAllSchemas()` is stubbed to return
  `[]`.
- **Where.** lootbox (fork and deployed; the path is identical in both).
- **mcpx @ 05c78b2.** A persisted schema cache (`internal/daemon/registry.go:87-100`) invalidated by `list_changed`
  (`internal/mcpclient/client.go:495-500`).
- **Value to mcpx.** − already solved.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** lootbox calls `listTools()` with no cursor, so only the first page is ever read.
- **Sources.** `.lootbox/src/lib/rpc/managers/mcp_integration_manager.ts:380-396`; `.lootbox/src/lib/external-mcps/mcp_schema_fetcher.ts:137-140` "// return Array.from(this.cache.values()); return [];"; `.lootbox/src/lib/external-mcps/mcp_schema_fetcher.ts:69`

## PM-24 lootbox multi-instance strategies and port rewriting

- **What.** When two lootbox daemons configure the same server, the `warn`/`fail`/`auto-port`/`per-session`
  strategies coordinate them through a file registry; `auto-port` finds a port in the server's arguments (for example
  `--remote-debugging-port`) and rewrites it to a free one.
- **Where.** lootbox fork only.
- **mcpx @ 05c78b2.** n/a: mcpx isolates callers within one daemon (PM-11).
- **Value to mcpx.** − `ASSESSMENT.md` puts it plainly: "the thing that needs isolating is the caller, not the process";
  there is only ever one lootbox instance, a launchd singleton.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** `per-session` does what `warn` does without the warning: it registers a session and spawns one shared
  client, although the README says "Spawn independent server per session".
- **Sources.** `.lootbox/src/lib/rpc/managers/mcp_integration_manager.ts:200-216`; `ASSESSMENT.md:95-96` "The abstraction is at the wrong level: the thing that needs isolating is the caller, not the process."; `.lootbox/README.md:417` "| `per-session`  | Spawn independent server per session                |"

## PM-25 Per-run script and client setup

- **What.** lootbox writes a temp file whose first line imports the client over HTTP from the daemon itself, then runs
  `deno run --reload=http://localhost:<port>/client.ts`, re-fetching and re-compiling the client every time. mcpx
  writes the client to a directory named after its content hash only when it changes, and names inline scripts after
  their own hash, so the runtime's compile cache stays warm.
- **Where.** lootbox; mcpx. opencode runs in-process; Cloudflare uses a new isolate (CODE-04).
- **mcpx @ 05c78b2.** `internal/runner/runner.go:4-9` (the rationale, naming lootbox), `:245-251`, `:276-291`,
  `:455-475`.
- **Value to mcpx.** − already solved; `ASSESSMENT.md` measured `lootbox exec 'console.log(1)'` at 0.74–10.3 s.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** lootbox's client URL already carries `?v=<version>`, so `--reload` is redundant. A lootbox script runs in
  the daemon's working directory, not the caller's (code-mode area).
- **Sources.** `.lootbox/src/lib/execute_llm_script.ts:18`; `.lootbox/src/lib/execute_llm_script.ts:33`; `.lootbox/src/lib/execute_llm_script.ts:13`; `internal/runner/runner.go:4-9`
