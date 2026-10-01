# Logging

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  logging/setLevel, notifications/message, log levels and the 2026 per-request logLevel, across revisions and products
```

Logging is the server-to-client `notifications/message` stream and the knobs that control it. 2026-07-28 changes it more than
its size suggests: it removes `logging/setLevel`, moves the level into each request's `_meta`, flips the default from "the
server decides" to "send nothing", and deprecates the whole feature in the same revision. For mcpx the two things that
matter are that its client still sends the removed `setLevel` to modern upstreams, and that its own code comment and
protocol doc describe the 2026 rule backwards. Request-scoped delivery of `notifications/message` (never on a listen
stream) is one rule shared with progress and is registered once, in [notifications.md](notifications.md).

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| LOG-01 | `logging/setLevel` request removed | `2024-11-05 has` `2026-07-28 removes` `has better replacement` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 —` | acc. — answers `{}` in every era, does nothing | + low | S | low |
| LOG-02 | Per-request `_meta` `io.modelcontextprotocol/logLevel` | `2026-07-28 has` `2026-07-28 deprecates` `mcpx missing` `specs conflict` | `26-07 ✓ (dep.)` | ✗ — never sent upstream, never read from hosts (#200) | + low | S | low |
| LOG-03 | No `logLevel` means no log messages at all | `2026-07-28 has` `specs conflict` | `24-11..25-11: server decides · 26-07: MUST NOT emit` | ✓ — emits none; code comment states the opposite | + low | S | med |
| LOG-04 | Invalid log level rejects the whole request | `2026-07-28 has` | `24-11..25-11: setLevel -32602 · 26-07: any request -32602` | partial — `setLevel` never validates; `_meta` level ignored | + low | S | low |
| LOG-05 | RFC 5424 log levels unchanged | `2024-11-05 has` `2026-07-28 deprecates` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 dep.` | n/a — emits none; client only ever asks `info` | − moot | S | low |
| LOG-06 | Logging feature deprecated (SEP-2577) | `2026-07-28 deprecates` | `26-07 dep.; removable from 2027-07-28` | ✓ — nothing declared or emitted | − deprecated | S | low |
| LOG-07 | mcpx client sends `setLevel info` to every logging upstream | `2026-07-28 removes` `mcpx missing` | mcpx only; opencode v1/v2 never send `setLevel` | ✗ — removed method sent to modern upstreams; level hard-coded (#200) | + med | S | low |
| LOG-08 | Relaying upstream log messages to an MCP host | `mcpx missing` `2026-07-28 deprecates` | spec: any server may emit; mcpx: event bus only | ✓ — relayed to a host that set a level, during its calls (#212; `docs/protocol.md` §4.4) | + low | M | low |
| LOG-09 | Client handling of `notifications/message` | `opencode v1 has` `has no replacement` | `opencode v1 ✓ · opencode v2 ✗` | n/a — mcpx sends none to hosts | − moot | S | low |

## LOG-01 `logging/setLevel` request removed

- **What.** The session-wide "send me logs at this level and above" request is gone in 2026-07-28; the level now travels
  on each request (LOG-02).
- **Where.** 2024-11-05 through 2025-11-25; removed in 2026-07-28. opencode v1 and v2 never send it
  (`v1:packages/opencode/src/mcp/index.ts:164`; `v2:packages/core/src/mcp/client.ts:91`).
- **mcpx @ 05c78b2.** Accepted from every era and answered `{}` (plus `resultType` for a 2026 caller), with no effect and
  nothing forwarded upstream (`internal/mcpserver/server.go:565-572`). Wire W25: a 2026 `logging/setLevel` gets
  `{"resultType":"complete"}`.
- **Value to mcpx.** + low: accept-liberally is the right call; the handler's comment says refusing "would make a
  well-behaved client that asked anyway treat the whole connection as degraded".
- **Effort.** S — done.
- **Risk.** None as is.
- **Detail.** Params are `{level: LoggingLevel}`. The legacy rule was "all logs at this level and higher". mcpx answers
  without parsing the params, so an invalid level is not rejected (LOG-04). The `FeatLoggingSetLevel` comment that
  justifies the removal is wrong about the replacement (LOG-03).
- **Sources.** `schema/2025-11-25/schema.ts:1520` "logging/setLevel";
  `2026-07-28/changelog.mdx:20` "Remove";
  `schema/2024-11-05/schema.ts:714` "The server should send all logs at this level and higher";
  `internal/mcpserver/server.go:572` "return reply(map[string]any{})"; wire W25.

## LOG-02 Per-request `_meta` `io.modelcontextprotocol/logLevel`

- **What.** An optional `LoggingLevel` in a request's `_meta` opts that one request into `notifications/message` at or
  above the level. It is the replacement for `setLevel`, and it is itself deprecated along with the rest of Logging.
- **Where.** 2026-07-28 only; listed in the reserved `_meta` key table.
- **mcpx @ 05c78b2.** Never sent: no `logLevel` anywhere in `internal/mcpclient`, so a 2026 upstream can never send mcpx a
  log message. As a server mcpx ignores it on host requests (it emits nothing anyway).
- **Value to mcpx.** + low: if upstream logs are wanted in the event bus from modern servers, the level has to ride on
  every forwarded request. Deprecated, so not worth more than that.
- **Effort.** S — one `_meta` key in `withMeta`, fed from the per-server `logLevel` setting that already exists (LOG-07).
- **Risk.** Without it, upstream diagnostics from 2026 servers are lost.
- **Detail.** There is no way to set a minimum once; a proxy that wants logs must add the key to each forwarded request.
  A proxy relaying a host's own level should forward exactly what the host sent, because adding a level the host did not
  ask for makes the upstream emit what the host did not request (LOG-03). The key is simultaneously the replacement for
  `setLevel` and `@deprecated` in the same schema.
- **Also recorded from the meta register.** The server emits no `notifications/message` at all, so it is compliant by
  omission (`internal/mcpserver/server.go:565-572`), but the comment at `internal/mcpserver/revisions.go:72-73` says
  `setLevel` was removed "in favour of servers emitting at a level of their own choosing", the opposite of the spec.
  The client never sets the key upstream, so a 2026 upstream never sends mcpx logs; what it sends instead is in the
  logging register. Being the replacement and deprecated at once is why this row carries `specs conflict`. Logging
  behaviour is in the logging register.
- **Sources.** `schema/2026-07-28/schema.ts:110` "\"io.modelcontextprotocol/logLevel\"?: LoggingLevel;";
  `schema/2026-07-28/schema.ts:106` "@deprecated Deprecated as of protocol version 2026-07-28 (SEP-2577).";
  `2026-07-28/basic/index.mdx:356` "Minimum log level the server should emit for a request";
  <https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/logging>.; `2026-07-28/server/utilities/logging.mdx:63`; `2026-07-28/server/utilities/logging.mdx:100`; `internal/mcpserver/revisions.go:73` "// favour of servers emitting at a level of their own choosing.".

## LOG-03 No `logLevel` means no log messages at all

- **What.** Legacy: if the client never sent `setLevel`, the server MAY decide which messages to send. 2026: if a request
  carries no `logLevel`, the server MUST NOT send `notifications/message` for it. The default flips from "server decides"
  to "none".
- **Where.** Legacy default in 2024-11-05..2025-11-25; the inverse in 2026-07-28.
- **mcpx @ 05c78b2.** Compliant by omission: it emits no `notifications/message` (`internal/mcpserver/server.go:566-568`).
  But the feature table comment says 2026 removed `setLevel` "in favour of servers emitting at a level of their own
  choosing" (`internal/mcpserver/revisions.go:72-73`), which is the legacy default presented as the 2026 one.
- **Value to mcpx.** + low: fix the comment before someone builds on it.
- **Effort.** S — two comment lines.
- **Risk.** med: the wrong model leads straight to forwarding unsolicited upstream logs to 2026 hosts, which violates a
  MUST.
- **Detail.** This is a behaviour inversion, not just a method removal. A dual-era proxy must drop legacy upstream log
  messages for a 2026 host unless that host's request carried `logLevel`. The schema says it twice: the `_meta` doc
  ("If absent, the server MUST NOT send any") and the logging page.
- **Sources.** `schema/2025-11-25/schema.ts:1545` "the server MAY decide which messages to send automatically";
  `2026-07-28/server/utilities/logging.mdx:63` "The server **MUST NOT**";
  `schema/2026-07-28/schema.ts:102` "If absent, the server MUST NOT send any";
  `internal/mcpserver/revisions.go:73` "favour of servers emitting at a level of their own choosing.".

## LOG-04 Invalid log level rejects the whole request

- **What.** Legacy: an unknown level in `logging/setLevel` gets `-32602`. 2026: an unknown `logLevel` in a request's
  `_meta` SHOULD fail that whole request with `-32602`, whatever the request was.
- **Where.** `-32602` on `setLevel` in 2024-11-05..2025-11-25; on any request in 2026-07-28.
- **mcpx @ 05c78b2.** `logging/setLevel` returns `{}` without reading its params (`internal/mcpserver/server.go:565-572`),
  so a bad level is accepted. A 2026 `_meta` level is not read at all, so it cannot fail a request.
- **Value to mcpx.** + low: as a proxy, the useful rule is the reverse one: never forward a level the upstream will reject,
  because that fails an unrelated tool call.
- **Effort.** S.
- **Risk.** A typo in a logging hint failing a tool call, if mcpx ever forwards levels unchecked.
- **Detail.** SHOULD, so a server may also ignore the bad value. Validation belongs where mcpx adds or forwards the key
  (LOG-02), against the eight RFC 5424 names (LOG-05).
- **Sources.** `2024-11-05/server/utilities/logging.mdx:110` "Invalid log level";
  `2026-07-28/server/utilities/logging.mdx:100` "value carried in a request's";
  `2026-07-28/server/utilities/logging.mdx:101` "the server **SHOULD** reject that".

## LOG-05 RFC 5424 log levels unchanged

- **What.** `debug`, `info`, `notice`, `warning`, `error`, `critical`, `alert`, `emergency`, identical in all five
  revisions. The `LoggingLevel` type is marked deprecated in 2026.
- **Where.** All five.
- **mcpx @ 05c78b2.** Emits none. As a client it only ever asks for `info` (`internal/daemon/hooks.go:29`, LOG-07).
- **Value to mcpx.** − moot.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The `notifications/message` params are `level`, optional `logger`, and `data` (any JSON), unchanged in
  every revision.
- **Sources.** `schema/2024-11-05/schema.ts:747` "export type LoggingLevel ="; `schema/2026-07-28/schema.ts:2075`
  "export type LoggingLevel ="; `schema/2024-11-05/schema.ts:733` "logger?: string;";
  `schema/2026-07-28/schema.ts:2043` "data: unknown;".

## LOG-06 Logging feature deprecated (SEP-2577)

- **What.** The whole feature (capability, notification, level type, `_meta` key) is deprecated in 2026-07-28. New
  implementations SHOULD NOT adopt it; existing ones SHOULD move to `stderr` (stdio) or OpenTelemetry.
- **Where.** 2026-07-28; eligible for removal no earlier than twelve months after the revision.
- **mcpx @ 05c78b2.** Declares and emits nothing (CAP-12), so there is nothing to retire.
- **Value to mcpx.** − deprecated: nothing to build.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Deprecated is not removed: a 2026 server may still emit when asked (LOG-02). Roots and Sampling are
  deprecated by the same SEP; Elicitation is not.
- **Sources.** `2026-07-28/server/utilities/logging.mdx:8` "The Logging feature is deprecated as of protocol version";
  `2026-07-28/server/utilities/logging.mdx:13` "New implementations";
  `2026-07-28/deprecated.mdx:28` "[Logging](https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/logging)".

## LOG-07 mcpx client sends `setLevel info` to every logging upstream

- **What.** After connecting, the pool sends `logging/setLevel {"level":"info"}` to any upstream that declares `logging`,
  including a modern one, for which the method no longer exists. It should use the per-request `_meta` key for modern
  upstreams.
- **Where.** mcpx only. opencode v1 and v2 never send `setLevel` (`v1:packages/opencode/src/mcp/index.ts:164`;
  `v2:packages/core/src/mcp/client.ts:91`).
- **mcpx @ 05c78b2.** `LogLevel: "info"` is an inline constant (`internal/daemon/hooks.go:29`); the pool sends it
  (`internal/pool/pool.go:410-417`); the client checks only `Supports("logging")`, not the era
  (`internal/mcpclient/client.go:568-575`). A per-server `logLevel` is resolved in config and read by nothing
  (`internal/config/config.go:483`).
- **Value to mcpx.** + med: correct 2026 behaviour, and a configurable level instead of a hard-coded one.
- **Effort.** S — era switch plus wiring the existing setting.
- **Risk.** low: a modern upstream answers `-32601`, which is swallowed (`_ =`); the cost is that logs from modern
  upstreams never flow.
- **Detail.** This is a send-conservatively conflict (a method sent to a peer whose revision does not define it). The
  hard-coded `"info"` breaks the project's "nothing hardcoded" rule. Legacy upstreams that do not declare `logging` are
  correctly left alone.
- **Sources.** `internal/daemon/hooks.go:29`; `internal/mcpclient/client.go:568-575`; `internal/config/config.go:483`;
  `schema/2026-07-28/schema.ts:110` "\"io.modelcontextprotocol/logLevel\"?: LoggingLevel;".

## LOG-08 Relaying upstream log messages to an MCP host

- **What.** Upstream `notifications/message` is published on mcpx's event bus as a `ServerLog` event (visible on
  `/v1/events` and in the TUI). The MCP notifier forwards only list_changed and resources/updated, so no MCP host ever
  sees an upstream log.
- **Where.** Every revision defines `notifications/message` (deprecated in 2026). mcpx: event bus only.
- **mcpx @ 05c78b2.** `internal/daemon/hooks.go:31-33` publishes the event; the listen filter kinds are only the four list
  and resource kinds (`internal/cli/serve.go:726-742`); `MCPNotification` has no `ServerLog` case
  (`internal/events/events.go:252-271`).
- **Value to mcpx.** + low: hosts that show server logs could; for an agent host, logs in context are mostly noise, and
  the event bus is the better home.
- **Effort.** M — request-scoped forwarding on the call's own stream, only when the host asked (legacy `setLevel`, 2026
  `_meta` `logLevel`).
- **Risk.** low. Blanket forwarding would itself break LOG-03's MUST and the request-scoping rule in
  [notifications.md](notifications.md).
- **Detail.** A relayed message would have to be attributed to the request that caused it, which on a shared upstream
  instance mcpx cannot always do (the same limit as elicitation on shared instances, see
  [elicitation.md](elicitation.md)). The 2026 page also says log messages MUST NOT contain credentials or secrets; a relay
  inherits that obligation.
- **Sources.** `internal/daemon/hooks.go:32` "r.publish(events.Event{Kind: events.ServerLog, Server: server, Data: mustJSON(m)})";
  `internal/cli/serve.go:726-742`; `internal/events/events.go:252-271`;
  `2026-07-28/server/utilities/logging.mdx:123` "Log messages **MUST NOT** contain:".

## LOG-09 Client handling of `notifications/message`

- **What.** opencode v1 maps server log messages onto its own logger ("MCP server log", by level). opencode v2 registers
  no logging handler, so server logs are dropped.
- **Where.** opencode v1 only; v2 removed it with no replacement. Neither version sends `setLevel`.
- **mcpx @ 05c78b2.** Sends no `notifications/message` to hosts and no longer declares `logging`
  (`internal/mcpserver/server.go:565`), so neither opencode version loses anything from mcpx.
- **Value to mcpx.** − moot.
- **Effort.** S.
- **Risk.** None.
- **Detail.** v1 maps `logger`, `level` and `data` into structured fields. Because neither version sends `setLevel`, a
  legacy server talking to v1 sends whatever it decides (LOG-03's legacy default).
- **Sources.** `v1:packages/opencode/src/mcp/index.ts:457`; `v1:packages/opencode/src/mcp/index.ts:474`;
  `v2:packages/core/src/mcp/client.ts:140`.
