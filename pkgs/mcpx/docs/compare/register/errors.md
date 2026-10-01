# Errors

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  JSON-RPC and MCP error codes across revisions, their HTTP statuses, tool errors versus protocol errors, and what mcpx sends.
```

MCP reports failures two ways: JSON-RPC error responses (protocol errors) and, for tools, successful results with
`isError: true` (execution errors). The code set barely moved until 2026-07-28, which added an allocation policy, three
codes (`-32020`..`-32022`) that must travel with HTTP 400, moved resource-not-found to `-32602`, and gave unknown
methods HTTP 404. For mcpx, a dual-era proxy, the two things that matter most are choosing codes by the peer's revision
(it sends the 2026-only `-32022` to legacy clients and `-32602` for a missing resource to everyone) and the HTTP status
(every JSON-RPC error goes out as 200, which defeats the status-based era detection 2026 clients use). mcpx answering
unknown notifications with an error is in the notifications register.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| ERR-01 | `-32700` Parse error | `2024-11-05 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (typed)` | ✓ — sent without id; HTTP 400 | + low | S | low |
| ERR-02 | `-32600` Invalid Request | `2024-11-05 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (typed)` | partial — stdio checks `jsonrpc`; HTTP never does | + low | S | low |
| ERR-03 | `-32601` Method not found, and what it now covers | `2024-11-05 has` `2026-07-28 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ + 404` | partial — used for unknown methods; always HTTP 200 | + med | S | low |
| ERR-04 | `-32602` Invalid params, meanings widening | `2024-11-05 has` `2026-07-28 has` `mcpx missing` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (wider)` | partial — also used for internal failures (#202) | + med | S | med |
| ERR-05 | `-32603` Internal error | `2024-11-05 has` `2026-07-28 has` `mcpx missing` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | partial — MRTR path uses it; resources and prompts use `-32602` (#202) | + med | S | low |
| ERR-06 | `-32002` Resource not found → `-32602` | `2024-11-05 has` `2026-07-28 removes` `mcpx missing` `2026-07-28 has` | `24-11 ✓ · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 MUST NOT emit` | partial — `-32602` to every era; legacy expects `-32002` (#202) | + low | S | low |
| ERR-07 | `-32020` HeaderMismatch | `2026-07-28 has` `mcpx missing` | `26-07 ✓` (HTTP 400) | ✗ — plain-JSON 400 for one case; others unchecked (#199) | + high | S | high |
| ERR-08 | `-32021` MissingRequiredClientCapability | `2026-07-28 has` | `26-07 ✓` (HTTP 400) | ✗ — never used; the broker answers instead | + low | S | low |
| ERR-09 | `-32022` UnsupportedProtocolVersion | `2026-07-28 has` `mcpx missing` | `26-07 ✓` (HTTP 400) | partial — right shape; `requested` empty; HTTP 200 (#199) | + med | S | med |
| ERR-10 | Code for refusing a legacy `initialize`: `-32602` vs `-32022` | `2024-11-05 has` `specs conflict` `mcpx missing` | `24-11..25-11` example `-32602` · `26-07` `-32022` | ✗ — 2026-only `-32022` sent to legacy peers (#202) | + med | S | med |
| ERR-11 | Draft codes `-32001` / `-32003` / `-32004` | `2026-07-28 has` | pre-release 2026 drafts only | n/a — never used by mcpx | + low | S | low |
| ERR-12 | Error-code allocation policy (2026) | `2026-07-28 has` `specs conflict` | `26-07 ✓` | ✓ — standard codes plus `-32022` only | + low | S | low |
| ERR-13 | HTTP status for modern JSON-RPC errors (400 / 404) | `2026-07-28 has` `mcpx missing` | `26-07 ✓` | ✗ — every error goes out as 200 (#199) | + med | S | med |
| ERR-14 | Check order for a malformed 2026 HTTP request | `2026-07-28 has` `specs conflict` | `26-07` (unspecified) | partial — header-vs-`_meta` first; missing fields never rejected | + low | S | low |
| ERR-15 | Error response `id` optional | `2025-11-25 has` | `24-11 — · 25-03 — · 25-06 — · 25-11 ✓ · 26-07 ✓` | partial — sends id-less parse errors; client drops id-less errors | + low | S | low |
| ERR-16 | Local errors versus peer errors through a proxy | `2026-07-28 has` `mcpx missing` | `26-07` guidance | partial — upstream code and data survive only as text (#202) | + med | M | med |
| ERR-17 | Tool input validation: protocol error → `isError` | `2025-11-25 has` | `24-11..25-06` protocol error · `25-11 ✓ · 26-07 ✓` `isError` | ✓ — tool failures are `isError` results | + med | S | low |
| ERR-18 | 2026 schema still shows `-32602` for bad tool arguments | `2026-07-28 has` `specs conflict` | `26-07` prose vs schema | ✓ — mcpx follows the prose | + low | S | low |

## ERR-01 `-32700` Parse error

- **What.** Invalid JSON received. Standard JSON-RPC; 2026 adds a typed `ParseError` interface.
- **Where.** Every revision.
- **mcpx @ 05c78b2.** stdio answers `-32700` with no id (`internal/mcpserver/server.go:1040-1044`); HTTP answers 400
  with `-32700` (`internal/mcpserver/server.go:1153-1157`). Batch arrays land here (wire W16, S3;
  `transports.md` TR-09), and so does an HTTP body over 64 MiB, which is truncated before parsing
  (`internal/mcpserver/server.go:1132`).
- **Value to mcpx.** + low.
- **Effort.** S — done.
- **Risk.** None.
- **Detail.** Legacy HTTP allows an error status whose body MAY be an id-less JSON-RPC error, which is what mcpx sends.
  The mcpx client drops id-less errors it receives (ERR-15).
- **Sources.** `schema/2024-11-05/schema.ts:80` "export const PARSE_ERROR = -32700;"; `schema/2026-07-28/schema.ts:328`;
  `internal/mcpserver/server.go:1042-1043`; wire S3.

## ERR-02 `-32600` Invalid Request

- **What.** The message is not a valid request object; the 2026 doc names missing required fields such as `jsonrpc` or
  `method`, or wrong types for them. 2025-11-25 also uses it for misuse of task-required execution.
- **Where.** Every revision.
- **mcpx @ 05c78b2.** stdio answers `-32600` when `jsonrpc` is present and not `"2.0"`, but accepts a frame with no
  `jsonrpc` at all (`internal/mcpserver/server.go:1046-1049`). The HTTP path has no such check; line 1046 is the only
  one.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low; accepting a missing `jsonrpc` is accept-liberally.
- **Detail.** None.
- **Sources.** `schema/2024-11-05/schema.ts:81`; `schema/2026-07-28/schema.ts:339`;
  `2025-11-25/basic/utilities/tasks.mdx:775`; `internal/mcpserver/server.go:1046` "if req.JSONRPC != \"\" &&
  req.JSONRPC != \"2.0\" {".

## ERR-03 `-32601` Method not found, and what it now covers

- **What.** Unknown method, in every revision. It accumulates meanings: from 2025-03-26 the completion page uses it for
  `completion/complete` without the capability; 2025-11-25 for a `taskSupport: "required"` tool called without a task;
  the 2026 schema for any method "gated behind a server capability the server did not advertise" (a missing *client*
  capability is `-32021` instead); and 2026 HTTP pairs it with status 404. The tasks extension also requires it for
  the removed `tasks/result`.
- **Where.** Every revision; 404 pairing in 2026-07-28.
- **mcpx @ 05c78b2.** Unrecognised methods fall through to `fail(-32601, "no method …")`
  (`internal/mcpserver/server.go:766`), sent with HTTP 200 (wire W12; ERR-13). The same fallthrough answers unknown
  notifications, which is covered in the notifications register.
- **Value to mcpx.** + med: the 404 lets 2026 clients tell "unknown method" from "old server".
- **Effort.** S.
- **Risk.** Low.
- **Detail.** None.
- **Sources.** `schema/2026-07-28/schema.ts:346` "In MCP, a server returns this error when a client invokes a method the
  server does not implement"; `2025-03-26/server/utilities/completion.mdx:130`;
  `2026-07-28/basic/transports/streamable-http.mdx:271`; `internal/mcpserver/server.go:766`; wire W12.

## ERR-04 `-32602` Invalid params, meanings widening

- **What.** Invalid or malformed parameters. The documented uses grow: unknown tool or prompt, missing prompt
  arguments, invalid or expired cursor, invalid log level, the legacy `initialize` version example (ERR-10), an
  elicitation mode the client did not declare, malformed sampling tool results; 2026 adds a request missing required
  `_meta` fields (HTTP 400) and a missing resource (ERR-06).
- **Where.** Every revision; widest in 2026-07-28.
- **mcpx @ 05c78b2.** `tools/call` params that do not decode get `-32602` (`internal/mcpserver/server.go:662-664`).
  Every `resources/read` and `prompts/get` failure also gets it, internal ones included (ERR-05), and a missing
  resource gets it in every era (ERR-06). A request missing required `_meta` is not rejected at all
  (`lifecycle-versioning.md` LV-24).
- **Value to mcpx.** + med: meaning "fix your parameters", it should not be used for server faults.
- **Effort.** S.
- **Risk.** Clients retry with "fixed" parameters against a broken server.
- **Detail.** In 2026 an invalid per-request `logLevel` SHOULD reject the whole request with `-32602`; see the logging
  register. The elicitation and sampling cases are errors a *client* returns, and in 2026 a client answers those
  requests only inside MRTR, where `InputResponse` has no error variant, so the documented `-32602` has no channel
  (MRTR register).
- **Also recorded from the resources register.** JSON-RPC semantics generally. A daemon or upstream failure (process
  crash, timeout, unknown namespace) in `resources/read` or `prompts/get` comes back as `-32602`
  (`internal/mcpserver/server.go:714-716`, `internal/mcpserver/server.go:755-758`). The MRTR path uses `-32603` for
  the same failures (`internal/mcpserver/ask.go:147-149`), so the two paths disagree. Distinguishing "no such
  resource" (`-32602`) from "upstream down" (`-32603`) needs the backend to return a typed error. Overlaps the errors
  register.
- **Sources.** `schema/2026-07-28/schema.ts:389`; `schema/2026-07-28/schema.ts:366`;
  `schema/2026-07-28/schema.ts:370-371`; `schema/2026-07-28/schema.ts:541-542`; `2026-07-28/basic/index.mdx:381`;
  `2024-11-05/server/utilities/logging.mdx:110`; `internal/mcpserver/server.go:715`.; `2026-07-28/server/resources.mdx:405` "Servers **SHOULD** return `-32603` for internal errors."; `internal/mcpserver/server.go:755-758`; `internal/mcpserver/ask.go:147-149`

## ERR-05 `-32603` Internal error

- **What.** The receiver hit an unexpected condition. 2026's resources page says servers SHOULD use `-32603` for
  internal errors.
- **Where.** Every revision.
- **mcpx @ 05c78b2.** A daemon or upstream failure during `resources/read` or `prompts/get` (process crash, timeout,
  unknown namespace) is reported as `-32602` (`internal/mcpserver/server.go:714-716`, `:755-758`). The MRTR path reports
  the same failures as `-32603` (`internal/mcpserver/ask.go:147-149`, `:170-172`), so the two paths disagree. The
  `-32603` a stateless 2026 client gets when MRTR gives up is in the elicitation register.
- **Value to mcpx.** + med: clients stop retrying corrected parameters against a broken server.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** None.
- **Sources.** `schema/2024-11-05/schema.ts:84`; `schema/2026-07-28/schema.ts:403`;
  `2026-07-28/server/resources.mdx:405` "Servers **SHOULD** return `-32603` for internal errors.";
  `internal/mcpserver/server.go:714-716`; `internal/mcpserver/ask.go:147-149`.

## ERR-06 `-32002` Resource not found → `-32602`

- **What.** 2024-11-05..2025-11-25 answer a missing resource with `-32002`. 2026 servers MUST use `-32602` and MUST NOT
  emit `-32002`; clients SHOULD still accept `-32002`; a missing resource MUST NOT be answered with empty `contents`.
- **Where.** `-32002` through 2025-11-25; `-32602` in 2026-07-28.
- **mcpx @ 05c78b2.** Any backend error becomes `-32602` for every era (`internal/mcpserver/server.go:713-716`). Wire
  W8: `{"code":-32602,"message":"mcp error -32602: no such resource: nope"}`; the upstream's own code survives only as
  text in the message and its `data` is lost. `-32002` appears nowhere in `internal/mcpserver` or
  `internal/mcpclient`.
- **Value to mcpx.** + low: legacy hosts that special-case `-32002` show "not found" properly; as a proxy mcpx should
  translate in both directions by peer revision.
- **Effort.** S — choose the code by `peer.Version`.
- **Risk.** Low.
- **Detail.** `-32002` sits in the legacy implementation-defined band; it is the one code there that 2026 lets
  receivers interpret, and it is reserved, never reused.
- **Also recorded from the resources register.** `-32002` in 2024-11-05 through 2025-11-25 (SHOULD); Any backend error
  from a read becomes `-32602` in every era (`internal/mcpserver/server.go:713-716`); wire W8:
  `{"code":-32602,"message":"mcp error -32602: no such resource: nope"}`. Correct for 2026; a legacy host that
  special-cases `-32002` does not recognise it. The upstream's own code survives only as text inside mcpx's message;
  its `data` is lost. `-32002` sat in JSON-RPC's implementation-defined band; 2026 declares `-32000..-32019`
  legacy/implementation-defined with `-32002` the only one receivers may interpret. Overlaps the errors register.
- **Sources.** `2025-11-25/server/resources.mdx:388` "- Resource not found: `-32002`";
  `2026-07-28/server/resources.mdx:404`; `2026-07-28/server/resources.mdx:407`; `2026-07-28/basic/index.mdx:140`;
  `internal/mcpserver/server.go:715`; wire W8.; `2024-11-05/server/resources.mdx:337` "- Resource not found: `-32002`"; `internal/mcpserver/server.go:713-716`

## ERR-07 `-32020` HeaderMismatch

- **What.** A server that processes the body MUST reject a request whose required standard header
  (`MCP-Protocol-Version`, `Mcp-Method`, `Mcp-Name`) is missing, disagrees with the body, or contains invalid
  characters, with HTTP 400 **and** a JSON-RPC `-32020` error; values using the Base64 sentinel are decoded before
  comparing. Intermediaries must return an HTTP error but need not use JSON-RPC.
- **Where.** 2026-07-28 (numbered `-32001` in the pre-release draft).
- **mcpx @ 05c78b2.** Only a mismatch between `MCP-Protocol-Version` and `_meta` is caught, and it is answered 400 with
  `{"error": "MCP-Protocol-Version … does not match …"}`, not JSON-RPC (`internal/mcpserver/server.go:1159-1168`; wire
  W10). `Mcp-Method` and `Mcp-Name` are never checked (wire W11), a 2026 request with no version header is not
  rejected, and `-32020` appears nowhere in `internal/`. As a client, a `-32020` from an upstream is not recognised as
  modern (`lifecycle-versioning.md` LV-28).
- **Value to mcpx.** + high: a dual-era client decides "modern server" from a recognised JSON-RPC error in a 400 body;
  mcpx's plain body makes it fall back to `initialize`, which mcpx answers, so the exchange works but in the wrong era.
- **Effort.** S.
- **Risk.** Dual-era clients misclassify mcpx; a policy proxy trusting the headers can be fooled.
- **Detail.** Apply it only to modern-era requests; legacy requests carry no `Mcp-Method`. The schema's own note on
  `_meta` `protocolVersion` says a mismatch returns "400 Bad Request" without naming a code, while the transport page
  requires `-32020`. A client seeing `-32020` for an `x-mcp-header` tool SHOULD re-list tools and retry
  (`transports.md` TR-39).
- **Sources.** `schema/2026-07-28/schema.ts:434` "export const HEADER_MISMATCH = -32020;";
  `2026-07-28/basic/transports/streamable-http.mdx:597` "When rejecting a request due to header validation failure,
  servers **MUST**"; `2026-07-28/basic/transports/streamable-http.mdx:624`; `schema/2026-07-28/schema.ts:71-72`;
  `internal/mcpserver/server.go:1163`; wire W10.

## ERR-08 `-32021` MissingRequiredClientCapability

- **What.** If processing needs a capability absent from this request's `clientCapabilities`, the server MUST return
  `-32021` with `data.requiredCapabilities`, a full `ClientCapabilities` object; HTTP 400.
- **Where.** 2026-07-28 (numbered `-32003` in the pre-release draft).
- **mcpx @ 05c78b2.** Never used, and no constant exists. When a 2026 client did not declare elicitation, `canAsk` is
  false and the question goes to mcpx's broker, where another audience can answer it
  (`internal/mcpserver/ask.go:66-76`; `internal/mcpserver/server.go:649-657`).
- **Value to mcpx.** + low: a host that could re-send with the capability never learns it was needed; the broker is a
  deliberate alternative.
- **Effort.** S.
- **Risk.** Low; not using it is conformant while mcpx can finish without the capability.
- **Detail.** MRTR forbids input requests for undeclared capabilities, so when input is truly required this is the
  only compliant answer (MRTR register). The tasks extension uses it for clients that did not declare the extension
  (tasks register).
- **Also recorded from the elicitation register.** 2026-07-28 only (earlier drafts numbered it `-32003`). Never
  emitted, and no constant exists. A 2026 host that did not declare elicitation gets the direct path: `canAsk` is
  false (`internal/mcpserver/ask.go:66-76`) and the question waits in the broker for another audience. The findings
  disagree on the label: the base-protocol axis calls `-32021` the only compliant answer when input is truly required;
  the mcpx axis calls the broker fallback a design choice. Both hold: mcpx never *requires* the capability, because a
  broker audience can answer. `data.requiredCapabilities` is a full `ClientCapabilities` object. The rule that mcpx
  never puts an undeclared kind of question into `inputRequests` is in [mrtr.md](mrtr.md).
- **Sources.** `schema/2026-07-28/schema.ts:442` "export const MISSING_REQUIRED_CLIENT_CAPABILITY = -32021;";
  `schema/2026-07-28/schema.ts:523` "requiredCapabilities: ClientCapabilities;"; `2026-07-28/basic/index.mdx:387`;
  `internal/mcpserver/ask.go:67`.; `schema/2026-07-28/examples/MissingRequiredClientCapabilityError/missing-elicitation-capability.json:6`

## ERR-09 `-32022` UnsupportedProtocolVersion

- **What.** If the requested version is unknown, or known but deliberately unsupported (an experimental draft, say),
  the server MUST return `-32022` with `data.supported: string[]` and `data.requested: string`; the client SHOULD retry
  with a mutually supported version. HTTP 400.
- **Where.** 2026-07-28 (numbered `-32004` in the pre-release draft).
- **mcpx @ 05c78b2.** The pre-dispatch check (`internal/mcpserver/server.go:469-471`) calls `unsupportedVersion`, which
  reads `params.protocolVersion`, the `initialize` field, instead of `_meta` (`internal/mcpserver/server.go:245-248`),
  so `requested` is always `""` on modern requests. It goes out with 200 (ERR-13). Wire W13:
  `HTTP/1.1 200 OK … "data":{"requested":"","supported":[…]}`. The `supported` list is right and includes 2026-07-28.
- **Value to mcpx.** + med: a correct error lets a modern client pick a version and retry, and the 400 is how dual-era
  clients recognise a modern server.
- **Effort.** S — read the `_meta` key; map the status.
- **Risk.** Clients that inspect `requested` or key on the 400 misbehave.
- **Detail.** mcpx's own client recognises modern servers by this code alone, via a substring test
  (`lifecycle-versioning.md` LV-28).
- **Sources.** `2026-07-28/basic/versioning.mdx:50` "support), it **MUST** respond with an";
  `schema/2026-07-28/schema.ts:483` "export interface UnsupportedProtocolVersionError extends Omit<";
  `2026-07-28/basic/transports/streamable-http.mdx:265`; `internal/mcpserver/server.go:248`; wire W13.

## ERR-10 Code for refusing a legacy `initialize`: `-32602` vs `-32022`

- **What.** Every legacy lifecycle page shows an "initialization error" example: `-32602 "Unsupported protocol
  version"` with `data.supported` and `data.requested`, the same data shape 2026 standardises under `-32022`. mcpx
  answers legacy `initialize` refusals with `-32022`, a code no legacy revision defines.
- **Where.** Example in 2024-11-05..2025-11-25; `-32022` in 2026-07-28 only.
- **mcpx @ 05c78b2.** `codeUnsupportedVersion = -32022` (`internal/mcpserver/server.go:230`) is used for every
  `initialize` refusal (`internal/mcpserver/server.go:506-509`); wire W1, W3, W4, S1.
- **Value to mcpx.** + med. A dual-era client may read `-32022` as "modern server", which happens to reach mcpx's
  discover path; a legacy client sees an unknown code. For any well-formed version the legacy answer is a counter-offer,
  not an error (`lifecycle-versioning.md` LV-05..LV-07); an error remains right only for garbage, where the legacy
  example uses `-32602`.
- **Effort.** S.
- **Risk.** Legacy clients that switch on `-32602` treat the refusal as unknown; mcpx's own client aborts on it
  (`lifecycle-versioning.md` LV-29).
- **Detail.** The legacy example requests `"1.0.0"`, not a date. 2026's versioning page tells a modern-only server to
  name its versions in any error to `initialize` without assigning a code. Sending a 2026 code to a legacy peer is a
  send-conservatively conflict.
- **Sources.** `2024-11-05/basic/lifecycle.mdx:210` "\"code\": -32602,"; `2025-11-25/basic/lifecycle.mdx:278`;
  `2026-07-28/basic/versioning.mdx:59` "\"code\": -32022,"; `schema/2026-07-28/schema.ts:450`;
  `internal/mcpserver/server.go:230`; wire W1.

## ERR-11 Draft codes `-32001` / `-32003` / `-32004`

- **What.** Before 2026-07-28 was released its draft numbered HeaderMismatch `-32001`,
  MissingRequiredClientCapability `-32003` and UnsupportedProtocolVersion `-32004`; they were renumbered to
  `-32020`..`-32022`. Pre-release SDKs may still send the old numbers, which now fall in the band where receivers must
  assume nothing.
- **Where.** Pre-release 2026 drafts only.
- **mcpx @ 05c78b2.** Never used. A pre-release server sending `-32004` would not be recognised as modern by mcpx's
  `-32022` substring test (`internal/mcpclient/client.go:330`), which is also what the policy requires.
- **Value to mcpx.** + low.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** None.
- **Sources.** `2026-07-28/changelog.mdx:65` "`HeaderMismatch` `-32001` → `-32020`, `MissingRequiredClientCapability`";
  `internal/mcpclient/client.go:330`.

## ERR-12 Error-code allocation policy (2026)

- **What.** JSON-RPC's `-32000..-32099` is split. `-32000..-32019` is legacy: no new allocations, new implementations
  SHOULD NOT use it, and receivers MUST NOT assume a meaning, except for `-32002`. `-32020..-32099` belongs to the
  specification: implementations MUST NOT emit undefined codes there and MUST use defined ones only with their meaning;
  codes are allocated sequentially from `-32020`, and earlier codes (`-32002`, `-32042`) stay reserved. New
  application errors SHOULD be allocated outside `-32768..-32000`.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Uses only the JSON-RPC standard codes and `-32022` (`internal/mcpserver/server.go:222-231`).
- **Value to mcpx.** + low: nothing to change today; it constrains any code mcpx adds.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The schema's comment describes `-32000..-32019` as implementation-defined, used by existing SDKs "for
  their own purposes", without the prose's SHOULD NOT for new implementations.
- **Sources.** `2026-07-28/basic/index.mdx:117` "- **`-32000` to `-32019` — legacy.** Codes in this sub-range were
  allocated by"; `2026-07-28/basic/index.mdx:122`; `2026-07-28/basic/index.mdx:153-154`;
  `schema/2026-07-28/schema.ts:413-416`; `internal/mcpserver/server.go:222-231`.

## ERR-13 HTTP status for modern JSON-RPC errors (400 / 404)

- **What.** For 2026 requests over HTTP: `-32602` for missing `_meta`, `-32020`, `-32021` and `-32022` MUST go out with
  400; an unimplemented method MUST go out as 404 with `-32601`, whose body distinguishes it from an old HTTP+SSE
  server's bare 404. Legacy Streamable HTTP ties no JSON-RPC code to a status: its 400 is for a bad version header or a
  missing required session, and its 404 for an expired session.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Every non-streaming response, errors included, is written with 200
  (`internal/mcpserver/server.go:1192`). Wire W12 (`-32601`, 200), W13 (`-32022`, 200), W8 (`-32602`, 200).
- **Value to mcpx.** + med: dual-era clients classify servers by these statuses (`lifecycle-versioning.md` LV-30,
  `transports.md` TR-11).
- **Effort.** S — map by code when the request is modern; keep 200 for legacy requests.
- **Risk.** A dual-era client probing mcpx with a 2026 request gets a 200 error and may misclassify it.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:265` "not to support), it **MUST** respond with `400 Bad
  Request` and an"; `2026-07-28/basic/transports/streamable-http.mdx:271` "If the server does not implement the
  requested RPC method, it **MUST** respond"; `2026-07-28/basic/index.mdx:381`; `internal/mcpserver/server.go:1192`
  "writeJSON(w, http.StatusOK, resp)"; wire W12.

## ERR-14 Check order for a malformed 2026 HTTP request

- **What.** A missing `MCP-Protocol-Version` header is a HeaderMismatch (`-32020`); a missing `_meta`
  `protocolVersion` is `-32602`; an unsupported version is `-32022`. All three are 400, and the spec does not order
  the checks, for example when the header is present with an unsupported version and `_meta` is absent. Only the
  JSON-RPC code differs, and dual-era clients classify by that code.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Order: parse (`-32700`), then header against `_meta` only when both are present (plain 400,
  `internal/mcpserver/server.go:1161`), then session, then the version check before dispatch (`-32022`,
  `internal/mcpserver/server.go:469`); missing header or `_meta` fields are never rejected.
- **Value to mcpx.** + low: pick one order and document it.
- **Effort.** S.
- **Risk.** Clients may classify the same malformed request differently across servers.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/transports/streamable-http.mdx:624` "- A required standard header
  (`MCP-Protocol-Version`, `Mcp-Method`,"; `2026-07-28/basic/index.mdx:380-381`; `internal/mcpserver/server.go:1161`;
  `internal/mcpserver/server.go:469`.

## ERR-15 Error response `id` optional

- **What.** 2024-11-05..2025-06-18 schemas require `id` on `JSONRPCError`; 2025-11-25 makes it optional for errors
  where the id could not be read, and splits `JSONRPCResultResponse` from `JSONRPCErrorResponse`. Earlier HTTP prose
  already allowed id-less error bodies on 4xx.
- **Where.** 2025-11-25, 2026-07-28.
- **mcpx @ 05c78b2.** The server sends parse errors without an id (`internal/mcpserver/server.go:1042-1043`,
  `:1155-1156`). The client skips any frame without an id as a notification
  (`internal/mcpclient/client.go:369-370`), so an id-less error from an upstream, such as its parse error for one of
  mcpx's frames, is dropped and the pending call waits out its timeout.
- **Value to mcpx.** + low.
- **Effort.** S — fail the in-flight call, or log, on an id-less error.
- **Risk.** A strict 2025-06-18 client may reject id-less errors; that is unavoidable for parse errors.
- **Detail.** None.
- **Sources.** `schema/2024-11-05/schema.ts:91` "id: RequestId;"; `schema/2025-11-25/schema.ts:161` "id?: RequestId;";
  `2025-11-25/basic/index.mdx:89`; `internal/mcpclient/client.go:369-370`.

## ERR-16 Local errors versus peer errors through a proxy

- **What.** 2026: errors purely local to an implementation, such as an SDK timeout, have no codes, and implementations
  that surface them in JSON-RPC shape should make sure they cannot be mistaken for errors from the peer. A proxy has
  three sources to keep apart: the upstream's error, its own failures, and local timeouts.
- **Where.** 2026-07-28 guidance.
- **mcpx @ 05c78b2.** An upstream error arrives as Go error text (`mcp error -32602: no such resource: nope`) and is
  re-wrapped in a fresh `-32602` (`internal/mcpserver/server.go:713-716`; wire W8), so the upstream's `data` is lost and
  a daemon or transport failure looks like an upstream invalid-params error (ERR-05). Whether other upstream codes,
  such as `-32002` or `-32042`, are ever forwarded unchanged was not verified.
- **Value to mcpx.** + med: an agent can tell "the upstream said no" from "mcpx broke".
- **Effort.** M — carry upstream code and data through the daemon, and mark local failures.
- **Risk.** Agents retry the wrong thing.
- **Detail.** None.
- **Sources.** `2026-07-28/basic/index.mdx:146` "Errors that are purely local to an implementation (for example, a
  request"; `2026-07-28/basic/index.mdx:149`; `internal/mcpserver/server.go:715`; wire W8.

## ERR-17 Tool input validation: protocol error → `isError`

- **What.** 2024-11-05..2025-06-18 list unknown tools and invalid arguments as protocol errors and execution failures as
  `isError: true` results. 2025-11-25 (SEP-1303) moves input-validation failures into tool execution errors so the
  model can correct itself; protocol errors are left for unknown tools, malformed requests and server errors, and
  clients SHOULD pass execution errors to the model.
- **Where.** 2025-11-25 and 2026-07-28 prose.
- **mcpx @ 05c78b2.** A failing tool is an `isError: true` result (`internal/mcpserver/server.go:666-674`); params that
  do not decode get `-32602` (`internal/mcpserver/server.go:662-664`). In the proxy direction `mcpx_call` loses an
  upstream's `isError` (tools register).
- **Value to mcpx.** + med: matches mcpx's design; as a proxy, pass upstream errors through unchanged.
- **Effort.** S — done on the server side.
- **Risk.** Low.
- **Detail.** None.
- **Sources.** `2024-11-05/server/tools.mdx:231` "1. **Protocol Errors**: Standard JSON-RPC errors for issues like:";
  `2024-11-05/server/tools.mdx:233` "- Invalid arguments"; `2025-11-25/changelog.mdx:28`;
  `2025-11-25/server/tools.mdx:471`; `internal/mcpserver/server.go:666-674`.

## ERR-18 2026 schema still shows `-32602` for bad tool arguments

- **What.** 2026 prose makes input-validation failures (a date in the wrong format, a value out of range) `isError`
  results. The 2026 schema's `InvalidParamsError` doc lists "Tools: Unknown tool name or invalid tool arguments" and
  ships an example `invalid-tool-arguments.json`: `-32602 "Invalid arguments for tool calculate: Missing required
  property 'expression'"`.
- **Where.** 2026-07-28, prose against schema.
- **mcpx @ 05c78b2.** Follows the prose: tool failures are `isError` (`internal/mcpserver/server.go:666-674`).
- **Value to mcpx.** + low: clients, mcpx included, must accept both forms from upstreams.
- **Effort.** S.
- **Risk.** Low.
- **Detail.** The reading that reconciles them draws the line between failing the `CallToolRequest` schema (a
  malformed request, `-32602`) and failing the tool's `inputSchema` (`isError`). The prose never states that line, and
  "missing required property" is an `inputSchema` failure, so the example contradicts the prose either way.
- **Sources.** `2026-07-28/server/tools.mdx:762` "- Input validation errors (e.g., date in wrong format, value out of
  range)"; `schema/2026-07-28/schema.ts:366` "- **Tools**: Unknown tool name or invalid tool arguments";
  `schema/2026-07-28/examples/InvalidParamsError/invalid-tool-arguments.json:3`.
