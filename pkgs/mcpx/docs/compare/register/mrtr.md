# Multi round-trip requests (the 2026 `input_required` mechanism)

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  InputRequiredResult, inputRequests, requestState and retries in 2026-07-28, and how mcpx and opencode v2 implement them
```

In 2026-07-28 a server can no longer send the client a request: when it needs the user's answer, a model completion or
the client's roots, it answers the client's own `tools/call`, `prompts/get` or `resources/read` early with an
`InputRequiredResult`, and the client sends the same request again with the answers and an opaque `requestState`. This
page covers that mechanism for elicitation, sampling and roots alike; the embedded request shapes are in
[elicitation.md](elicitation.md), [sampling.md](sampling.md) and [roots.md](roots.md), and the `resultType` field every 2026
result carries is registered with lifecycle. For mcpx the point is that its `requestState` is a signed token bound to the
`Mcp-Session-Id` that 2026 removed, so the mechanism works only for 2026 hosts that behave like legacy ones (MRTR-08), and
that results routed through it come back flattened to text (MRTR-19).

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| MRTR-01 | `InputRequiredResult` with `resultType: "input_required"` | `2026-07-28 has` | `26-07 only` | ✓ — produced for 2026 hosts and consumed from upstreams | + high | S | low |
| MRTR-02 | `inputRequests`: server-assigned keys, bare `{method, params}` values | `2026-07-28 has` | `26-07 only` | ✓ — keys are broker question ids | + med | S | low |
| MRTR-03 | Only `tools/call`, `prompts/get`, `resources/read` may answer `input_required` | `2026-07-28 has` `specs conflict` | `26-07 only` | ✓ — exactly these three are interruptible | + low | S | low |
| MRTR-04 | Server-initiated requests removed (`ServerRequest` union gone) | `2026-07-28 removes` `has better replacement` | `24-11..25-11 ✓ · 26-07 —` | ✓ — wire requests for legacy, MRTR for 2026 | + high | S | low |
| MRTR-05 | No input request of a kind the client did not declare | `2026-07-28 has` | `26-07 only` | ✓ — only sendable questions included | + med | S | low |
| MRTR-06 | `requestState` opaque; client echoes it exactly, never invents one | `2026-07-28 has` | `26-07 only` | ✓ — echoed verbatim; stale fields removed per retry | + med | S | low |
| MRTR-07 | Server MUST integrity-protect `requestState` | `2026-07-28 has` | `26-07 only` | ✓ — `base64url(payload).HMAC-SHA256`, per-process key | + high | S | low |
| MRTR-08 | `requestState` SHOULD bind the authenticated principal | `2026-07-28 has` `mcpx missing` | `26-07 only` | ✗ — bound to `Mcp-Session-Id`, which 2026 removed (#201) | + high | M | high |
| MRTR-09 | `requestState` SHOULD carry a short expiry | `2026-07-28 has` | `26-07 only` | ✓ — `e` expiry, `proto.stateTTL` 30 m | + low | S | low |
| MRTR-10 | `requestState` SHOULD identify method and parameters digest | `2026-07-28 has` `mcpx missing` | `26-07 only` | ✗ — call id only; retry params ignored (#201) | + med | S | med |
| MRTR-11 | Single use needs server-side enforcement | `2026-07-28 has` | `26-07 only` | n/a — not enforced; replay re-polls the same call | − moot | S | low |
| MRTR-12 | Retry carries `inputResponses` keyed like `inputRequests` | `2026-07-28 has` | `26-07 only` | ✓ — built per key; relayed per key | + med | S | low |
| MRTR-13 | Retry uses a new JSON-RPC id; state only for that retry | `2026-07-28 has` | `26-07 only` | ✓ — each round takes a fresh id | + low | S | low |
| MRTR-14 | `requestState`-only result: client MAY retry at once | `2026-07-28 has` | `26-07 only` | partial — retries immediately, no backoff | + low | S | med |
| MRTR-15 | Rounds are unbounded on the server side | `2026-07-28 has` | `26-07 only` | partial — mcpx as server abandons after 8 rounds | + low | S | low |
| MRTR-16 | Client-side cap on rounds | `2026-07-28 has` `opencode v2 has` | spec: none; mcpx 8; opencode v2 10 | ✓ — `elicit.inputRounds` 8, then an error | + low | S | low |
| MRTR-17 | No per-input error channel | `2026-07-28 has` `specs conflict` `has no replacement` | `26-07 only` | partial — one failed input aborts the whole call | + low | M | low |
| MRTR-18 | Client fulfils `input_required` and retries | `2026-07-28 has` `opencode v2 has` | mcpx ✓; opencode v2 SDK ✓; opencode v1, lootbox ✗ | partial — implemented; unreachable until discovery is fixed | + high | S | med |
| MRTR-19 | Results returned through MRTR keep their full shape | `mcpx missing` | spec: result is the method's own; mcpx: text | ✗ — flattened to one text block (#201) | + med | M | med |
| MRTR-20 | Where the interrupted call's state lives | `2026-07-28 has` | spec: in `requestState`; mcpx: daemon task | partial — call runs as a daemon task; token names it | + med | L | med |

## MRTR-01 `InputRequiredResult` with `resultType: "input_required"`

- **What.** An interim result carrying optional `inputRequests` and optional `requestState`; at least one MUST be present.
  It terminates the original request; the retry is a new, independent request.
- **Where.** 2026-07-28 only.
- **mcpx @ 05c78b2.** Produced for 2026 hosts by `inputRequired` (`internal/mcpserver/ask.go:256-266`) and detected on
  upstream results by the client (`internal/mcpclient/client.go:663-676`).
- **Value to mcpx.** + high: the only way a 2026 host can be asked anything.
- **Effort.** S — done.
- **Risk.** None beyond the rows below.
- **Detail.** The server builds `{"resultType":"input_required","inputRequests":{…},"requestState":"…"}`; wire M2 shows
  one. Everything a server needs to resume should be in the retry itself (MRTR-20 on how far mcpx is from that).
- **Sources.** `schema/2026-07-28/schema.ts:584` "export interface InputRequiredResult extends Result {";
  `schema/2026-07-28/schema.ts:575` "At least one of"; `internal/mcpserver/ask.go:256-266`;
  `internal/mcpclient/client.go:663-676`; wire M2;
  <https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/mrtr>.

## MRTR-02 `inputRequests`: server-assigned keys, bare `{method, params}` values

- **What.** A map from server-chosen keys, unique within the request, to `ElicitRequest`, `CreateMessageRequest` or
  `ListRootsRequest` objects. There is no `jsonrpc` and no `id`; the key does the id's job.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Keys are the broker's question ids (`elc-…`) and values are the upstream's method and raw params
  (`internal/mcpserver/ask.go:258-259`).
- **Value to mcpx.** + med: done.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Because the params are copied raw, the originator prefix and the `elicitationId` strip that the legacy path
  applies are missing here (see [elicitation.md](elicitation.md), ELI-10 and ELI-18). Keys need only be unique per
  request; the tasks extension requires uniqueness over a task's lifetime ([tasks.md](tasks.md)).
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:228` "keys are server assigned identifiers";
  `schema/2026-07-28/schema.ts:538` "CreateMessageRequest | ListRootsRequest | ElicitRequest;";
  `internal/mcpserver/ask.go:259`.

## MRTR-03 Only `tools/call`, `prompts/get`, `resources/read` may answer `input_required`

- **What.** The prose lists three methods and says servers MUST NOT send `InputRequiredResult` on any other request. A
  schema comment on `InputResponseRequestParams` says the params "may be included in any client-initiated request".
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Exactly these three can block on the client (`internal/mcpserver/ask.go:318-325`).
- **Value to mcpx.** + low: aligned.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The schema's actual `extends` wire `InputResponseRequestParams` into those three param types only, agreeing
  with the prose; the comment is the outlier. Completion is not on the list, so `completion/complete` can never ask.
- **Also recorded from the resources register.** `resources/list` and `resources/templates/list` cannot. The server
  routes `resources/read` through the ask path (`internal/mcpserver/server.go:701-705`), and renders the final answer
  as one text item (`internal/mcpserver/ask.go:276-287`). An `InputRequiredResult` answer is not a cacheable result
  (pagination-caching register). The mechanism and its gaps are in the MRTR register.
- **Also recorded from the prompts register.** The server routes `prompts/get` through the ask path
  (`internal/mcpserver/server.go:742-746`); the finished call is rendered as one user text message
  (`internal/mcpserver/ask.go:271-275`). `GetPromptResult` is not a cacheable result, unlike `ReadResourceResult`
  (pagination-caching register). The mechanism is in the MRTR register.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:192` "Servers **MUST NOT** send";
  `schema/2026-07-28/schema.ts:598` "These parameters may be included in any client-initiated request.";
  `internal/mcpserver/ask.go:318-325`.; `schema/2026-07-28/schema.ts:1205` "export interface ReadResourceRequestParams"; `schema/2026-07-28/schema.ts:1245` "result: ReadResourceResult | InputRequiredResult;"; `internal/mcpserver/server.go:701-705`; `internal/mcpserver/ask.go:276-287`; `schema/2026-07-28/schema.ts:1602` "export interface GetPromptRequestParams extends InputResponseRequestParams {"; `2026-07-28/server/prompts.mdx:165` "Servers **MAY** also respond to `prompts/get` with an"; `internal/mcpserver/server.go:742-746`; `internal/mcpserver/ask.go:271-275`

## MRTR-04 Server-initiated requests removed (`ServerRequest` union gone)

- **What.** 2026 servers cannot send `ping`, `sampling/createMessage`, `roots/list` or `elicitation/create` as JSON-RPC
  requests; MRTR is the only way. `ClientResult` shrinks to `EmptyResult`.
- **Where.** `ServerRequest` in 2024-11-05..2025-11-25; none in 2026-07-28.
- **mcpx @ 05c78b2.** Asks a legacy host on the wire and a 2026 host through `input_required`; the call does not know
  which happened (`internal/mcpserver/ask.go:117-121`).
- **Value to mcpx.** + high: done per era.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The changelog calls this a breaking change but never says the `ServerRequest` union and the non-empty
  `ClientResult` members disappeared. `ClientResult = EmptyResult` is vestigial, since a 2026 client answers nothing.
- **Sources.** `schema/2025-11-25/schema.ts:2545` "export type ServerRequest =";
  `schema/2026-07-28/schema.ts:3169` "export type ClientResult = EmptyResult;";
  `2026-07-28/basic/patterns/mrtr.mdx:12` "The previous pattern of server-initiated requests is no longer";
  `internal/mcpserver/ask.go:117-121`.

## MRTR-05 No input request of a kind the client did not declare

- **What.** A server MUST NOT put an `elicitation/create` (or sampling, or roots) request into `inputRequests` for a client
  that did not declare that capability on this request.
- **Where.** 2026-07-28; capabilities are per request there.
- **mcpx @ 05c78b2.** `sendableTo` keeps only questions the host can take (`internal/mcpserver/ask.go:241-249`, via
  `Question.Sendable` at `internal/mcpserver/conn.go:367`). A question the host cannot take (a URL flow to a form-only
  host, say) stays in the broker for another audience, and mcpx waits (`internal/mcpserver/ask.go:177-193`).
- **Value to mcpx.** + med: correct, and the broker makes the fallback useful.
- **Effort.** S.
- **Risk.** low.
- **Detail.** When the request truly cannot finish without the capability, the spec's answer is `-32021`, which mcpx never
  sends because a broker audience can still answer; see [elicitation.md](elicitation.md), ERR-08.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:246` "that the client has not declared support for in its capabilities";
  `internal/mcpserver/ask.go:241-249`; `internal/mcpserver/ask.go:179` "Either nothing was asked yet".

## MRTR-06 `requestState` opaque; client echoes it exactly, never invents one

- **What.** Clients MUST NOT inspect, parse or modify `requestState`, MUST send back the exact value, and MUST NOT include
  one if none was given.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** As a client, echoed as received (`internal/mcpclient/modern.go:180-187`), with stale
  `requestState` and `inputResponses` removed before each retry (`internal/mcpclient/modern.go:171-172`).
- **Value to mcpx.** + med: done.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The spec lets servers encode anything ("base64-encoded JSON, encrypted JWT, serialized binary"), so a client
  cannot assume a format; mcpx's own token happens to be readable base64url JSON (MRTR-07).
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:254` "the client **MUST** echo back the exact value of that field";
  `2026-07-28/basic/patterns/mrtr.mdx:255` "Clients **MUST NOT** inspect, parse, modify";
  `internal/mcpclient/modern.go:181` "Opaque: passed back exactly as received".

## MRTR-07 Server MUST integrity-protect `requestState`

- **What.** Servers MUST treat `requestState` as attacker-controlled; when it influences authorization, resource access or
  business logic they MUST protect its integrity (HMAC or AEAD) and reject anything that fails verification.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The token is `base64url(json{c, b, e}) + "." + base64url(HMAC-SHA256)` with a random 32-byte key per
  process that is never persisted (`internal/mcpserver/state.go:47-51`, `:56-74`); verification is constant-time
  (`internal/mcpserver/state.go:82-112`). The state comment says the spec "says nothing about what a server should put in
  it" (`internal/mcpserver/state.go:20-21`), which is stale against mrtr.mdx.
- **Value to mcpx.** + high: a bare call id would let anyone resume, and read, another host's call.
- **Effort.** S — done; fix the comment.
- **Risk.** low.
- **Detail.** Observed on the wire (M2, decode of the base64url part): `{"c":"tsk-685a4a42635a4122",
  "b":"sess-e4af48671de84810a1c018d965371751","e":1790768865}`, that is a daemon task id, the session id, and a Unix
  expiry. A daemon restart invalidates every token, as intended, because the calls they name do not survive either.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:232` "servers **MUST** treat `requestState` as an attacker-controlled input.";
  `internal/mcpserver/state.go:73` "return body + \".\" + s.sign(body), nil";
  `internal/mcpserver/state.go:21` "nothing about what a server should put in it"; wire M2.

## MRTR-08 `requestState` SHOULD bind the authenticated principal

- **What.** To prevent replay, the integrity-protected payload SHOULD include the authenticated principal, and state
  presented by a different principal SHOULD be rejected.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Binds to the connection instead: the binding is the connection id (`internal/mcpserver/conn.go:67`),
  and minting refuses an empty one (`internal/mcpserver/state.go:64-66`). Over HTTP a connection has an id only when the
  client echoes an `Mcp-Session-Id` minted on `initialize` or `server/discover`; any other request gets an anonymous
  connection with an empty id (`internal/mcpserver/server.go:1209-1218`). 2026 removed that header. Wire M4: replaying a
  valid state without the session header gets `-32602 "this requestState belongs to another session"`.
- **Value to mcpx.** + high: this is why a correct stateless 2026 host cannot be asked anything (the resulting `-32603`
  is [elicitation.md](elicitation.md) ELI-17).
- **Effort.** M — mcpx has no authenticated principal on `/mcp` (an auth-register item), so the honest binding today is
  nothing beyond the HMAC plus the method and params digest (MRTR-10); bind to a principal once one exists.
- **Risk.** high if not done: every stateless 2026 host is locked out of MRTR. If done carelessly (no binding, no digest),
  a leaked token resumes someone else's call.
- **Detail.** The spec binds to the principal, not the connection, and 2026 statelessness says servers SHOULD NOT depend on
  connection reuse. mcpx's doc chose the session deliberately ("A token bound to nothing is a token anyone may present"),
  which was reasonable before the HMAC-plus-digest option was considered.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:235` "the authenticated principal, rejecting state presented by a different principal.";
  `internal/mcpserver/state.go:103` "if p.Binding != binding {"; `internal/mcpserver/conn.go:67`;
  `internal/mcpserver/server.go:1209-1218`; `2026-07-28/changelog.mdx:12` "Remove protocol-level sessions"; wire M4.

## MRTR-09 `requestState` SHOULD carry a short expiry

- **What.** The payload SHOULD include a short TTL, and expired state SHOULD be rejected.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The `e` field is the Unix expiry, `proto.stateTTL` (30 m) after minting, and verification refuses
  it afterwards with "this requestState expired; the call it named is gone" (`internal/mcpserver/state.go:109`).
- **Value to mcpx.** + low: done.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The setting lives in `internal/defaults/defaults.json` under `proto`, so it is configurable rather than
  inline.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:236` "a short expiry (TTL), rejecting state presented after it lapses;";
  `internal/mcpserver/state.go:109` "if time.Now().Unix() > p.Expires {";
  `internal/defaults/defaults.json:73` "\"stateTTL\": \"30m\",".

## MRTR-10 `requestState` SHOULD identify method and parameters digest

- **What.** The payload SHOULD name the originating request (method and a digest of its salient parameters), and state
  presented on a different request SHOULD be rejected.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** The payload has no method and no digest (`internal/mcpserver/state.go:47-51`); a retry is resumed by
  call id alone and its own params are ignored (`internal/mcpserver/ask.go:131-141`).
- **Value to mcpx.** + med: closes cross-request replay, and is what would let mcpx drop the session binding (MRTR-08).
- **Effort.** S — hash method plus name plus arguments into the payload and compare on resume.
- **Risk.** med: within one session, a state replayed on a different `tools/call` returns the first call's result.
- **Detail.** `forAsk` already strips `_meta`, `inputResponses` and `requestState` from params
  (`internal/mcpserver/ask.go:95-108`), so the digest input is readily available.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:237` "an identifier for the originating request, e.g. the method name and a digest of its salient parameters";
  `internal/mcpserver/state.go:47-51`; `internal/mcpserver/ask.go:131-141`.

## MRTR-11 Single use needs server-side enforcement

- **What.** Principal, TTL and request binding bound the replay window but do not make a state single-use; a server for
  which a state must be consumed at most once MUST enforce that itself.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Not enforced: `verify` checks signature, binding and expiry only (`internal/mcpserver/state.go:82-112`).
  Replaying a state within its TTL re-polls the same daemon call.
- **Value to mcpx.** − moot: an mcpx call is not a one-time redemption, so the MUST does not bind it.
- **Effort.** S.
- **Risk.** low.
- **Detail.** Replaying with fresh answers would feed them to an already-answered question; what the broker does then was
  not examined.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:240` "do not by themselves guarantee single-use.";
  `2026-07-28/basic/patterns/mrtr.mdx:242` "enforce that invariant server-side."; `internal/mcpserver/state.go:82-112`.

## MRTR-12 Retry carries `inputResponses` keyed like `inputRequests`

- **What.** `InputResponseRequestParams{inputResponses?, requestState?}`: each key matches a request key, each value is an
  `ElicitResult`, `CreateMessageResult` or `ListRootsResult`. Servers SHOULD ignore unexpected keys.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** As a client it answers every key and builds the map (`internal/mcpclient/modern.go:163-179`). As a
  server it reads the map as raw JSON and hands it to the broker (`internal/mcpserver/ask.go:131-141`).
- **Value to mcpx.** + med: done.
- **Effort.** S.
- **Risk.** None known; how the broker treats an unknown key was not examined.
- **Detail.** If required answers are missing, the server SHOULD ask again rather than fail (MRTR-15).
- **Sources.** `schema/2026-07-28/schema.ts:600` "export interface InputResponseRequestParams extends RequestParams {";
  `schema/2026-07-28/schema.ts:542` "CreateMessageResult | ListRootsResult | ElicitResult;";
  `2026-07-28/basic/patterns/mrtr.mdx:264` "the server **SHOULD** ignore any information it does not recognize or need.";
  `internal/mcpclient/modern.go:163-179`.

## MRTR-13 Retry uses a new JSON-RPC id; state only for that retry

- **What.** The initial request and the retry are independent, so the id MUST differ; `inputRequests` and `requestState`
  MUST NOT be used for any other request the client sends in parallel.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Every round goes through `roundTrip`, which takes a fresh id (`internal/mcpclient/client.go:681-682`).
- **Value to mcpx.** + low: done.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Unspecified: whether a retry may reuse the original request's `progressToken`. Neither mrtr.mdx nor the
  progress page says (see [progress-cancellation.md](progress-cancellation.md)).
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:256` "The JSON-RPC `id` **MUST** be different";
  `2026-07-28/basic/patterns/mrtr.mdx:257` "They **MUST NOT** be used for any other request";
  `internal/mcpclient/client.go:681-682`.

## MRTR-14 `requestState`-only result: client MAY retry at once

- **What.** An `InputRequiredResult` with no `inputRequests` asks the client only to come back; it MAY retry immediately.
  The schema's example calls this load shedding; the prose never does.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Handled: empty responses, state echoed (`internal/mcpclient/modern.go:173-187`). The retry is
  immediate, with no backoff (`internal/mcpclient/client.go:654-677`).
- **Value to mcpx.** + low: a server-directed continuation that mcpx could also use itself.
- **Effort.** S — add a backoff.
- **Risk.** med: a hot loop against a server that is shedding load, until the round cap (MRTR-16).
- **Detail.** "MAY retry immediately" permits waiting; a load-shedding server presumably wants the client to.
- **Sources.** `schema/2026-07-28/schema.ts:579` "@example InputRequiredResult with request state only (load shedding)";
  `2026-07-28/basic/patterns/mrtr.mdx:253` "the client **MAY** retry the original request immediately.";
  `internal/mcpclient/client.go:654-677`.

## MRTR-15 Rounds are unbounded on the server side

- **What.** A server MAY answer `input_required` repeatedly, SHOULD ask again when required information is missing rather
  than fail, and MUST NOT assume the client will fulfil or retry. No maximum is specified.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** As a server mcpx counts rounds per request and abandons the upstream call past `proto.askRounds` (8)
  with `-32603` (`internal/mcpserver/ask.go:195-199`). The cap is also what turns a failed mint into an immediate error
  (ELI-17 in [elicitation.md](elicitation.md)).
- **Value to mcpx.** + low: a guard the spec omits, and configurable.
- **Effort.** S.
- **Risk.** low: an upstream wizard asking more than eight questions fails through mcpx.
- **Detail.** mcpx also bounds the whole hold by `proto.askTimeout` (10 m). Abandoning after a cap is allowed; the spec only
  asks servers not to error for *missing* answers.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:247` "Servers **MAY** choose to return an `InputRequiredResult` on multiple attempts";
  `2026-07-28/basic/patterns/mrtr.mdx:267` "rather than returning an error.";
  `internal/mcpserver/ask.go:195-199`; `internal/defaults/defaults.json:72` "\"askRounds\": 8,".

## MRTR-16 Client-side cap on rounds

- **What.** Nothing caps rounds, so a client needs its own limit.
- **Where.** mcpx: `elicit.inputRounds` (8). opencode v2: its SDK auto-fulfils up to 10 rounds by default. The spec sets
  none.
- **mcpx @ 05c78b2.** Errors after `defaults.InputRounds` rounds (`internal/mcpclient/client.go:671-673`;
  `internal/defaults/defaults.go:356`).
- **Value to mcpx.** + low: sensible.
- **Effort.** S — done.
- **Risk.** low: a legitimate multi-step server longer than the cap fails.
- **Detail.** mcpx has two separate caps of the same value: one as a client of upstreams (this row), one as a server to
  hosts (MRTR-15).
- **Sources.** `internal/mcpclient/client.go:671-673`; `internal/defaults/defaults.json:66` "\"inputRounds\": 8";
  `v2:packages/core/src/mcp/client.ts:157`; <https://unpkg.com/@modelcontextprotocol/client@2.0.0/dist/src-D_zzAWoS.mjs>
  (L5049, `DEFAULT_INPUT_REQUIRED_MAX_ROUNDS = 10`).

## MRTR-17 No per-input error channel

- **What.** `InputResponse` is a union of results only. A client that cannot or will not fulfil one input request
  (malformed sampling, an unsupported mode) cannot say so for that key: it can omit it, answer `decline`/`cancel`
  (elicitation only), or give up. Yet the 2026 schema still documents client-side `-32602` cases for sampling and
  elicitation that MRTR has no way to carry.
- **Where.** 2026-07-28. Legacy clients returned JSON-RPC errors to the server's request.
- **mcpx @ 05c78b2.** As a client, any failed answer aborts the whole call with a local error, naming the key
  (`internal/mcpclient/modern.go:164-168`), rather than answering the others.
- **Value to mcpx.** + low: answering what it can and omitting the rest would let the server re-ask or fail cleanly.
- **Effort.** M.
- **Risk.** low.
- **Detail.** Omitting a key is the only in-band signal, and the server SHOULD then ask again (MRTR-15), which can loop
  until a cap.
- **Sources.** `schema/2026-07-28/schema.ts:541` "export type InputResponse =";
  `schema/2026-07-28/schema.ts:371` "**Sampling**: Missing tool result or tool results mixed with other content";
  `internal/mcpclient/modern.go:164-168`.

## MRTR-18 Client fulfils `input_required` and retries

- **What.** The client side of MRTR: answer each input request with the matching handler and retry the original request.
- **Where.** mcpx (client of modern upstreams). opencode v2: the SDK fulfils automatically from registered handlers, which
  are only `elicitation/create` and `roots/list`. opencode v1 speaks no 2026. lootbox uses SDK 1.22, whose newest revision
  is 2025-06-18.
- **mcpx @ 05c78b2.** Modern results with `resultType: "input_required"` are answered by `answer()`, the same function that
  serves legacy server-initiated requests, so questions go through the same broker; the call is retried with
  `inputResponses` and the echoed state (`internal/mcpclient/client.go:654-677`;
  `internal/mcpclient/modern.go:103-135`, `:156-189`).
- **Value to mcpx.** + high: parity across eras for upstream questions.
- **Effort.** S — done.
- **Risk.** med: unreachable in practice until mcpx's modern discovery and headers work against conformant servers (a
  lifecycle and transports issue), so this path is untested against real 2026 upstreams.
- **Detail.** opencode v2 fails an input request it has no handler for (sampling) after the SDK refuses, which is the
  missing error channel of MRTR-17 seen from the client.
- **Sources.** `internal/mcpclient/client.go:654-677`; `internal/mcpclient/modern.go:103-135`;
  `v2:packages/core/src/mcp/client.ts:155`; `v2:packages/core/src/mcp/client.ts:157`;
  `.lootbox/deno.json:44` "@modelcontextprotocol/sdk";
  <https://unpkg.com/@modelcontextprotocol/client@2.0.0/dist/src-D_zzAWoS.mjs> (L5044,
  `DEFAULT_INPUT_REQUIRED_AUTO_FULFILL = true`).

## MRTR-19 Results returned through MRTR keep their full shape

- **What.** After the rounds, the final result is the method's own result: a full `CallToolResult`, `GetPromptResult` or
  `ReadResourceResult`.
- **Where.** 2026-07-28 (and the same holds for legacy interruptible calls).
- **mcpx @ 05c78b2.** `askResult` renders a finished interruptible call as one text block: `tools/call` gets one text
  content (plus `isError`), `prompts/get` one user text message, `resources/read` one text entry
  (`internal/mcpserver/ask.go:269-296`). Images, `structuredContent`, `resource_link`, multi-message prompts and blobs are
  lost on this path, though the direct path keeps more.
- **Value to mcpx.** + med: a call should not return less because it happened to ask a question.
- **Effort.** M — carry the raw upstream result through the daemon task instead of `Outcome.Text`.
- **Risk.** med: MRTR-routed calls silently degrade compared with the same call made without a question.
- **Detail.** The same renderer serves legacy hosts whose calls were interrupted on the wire, so the loss is not 2026-only.
- **Sources.** `internal/mcpserver/ask.go:269-296`; `internal/mcpserver/ask.go:269` "func askResult(req request, out Outcome) map[string]any {".

## MRTR-20 Where the interrupted call's state lives

- **What.** MRTR lets a server ask "without maintaining any server-side state": everything needed to resume can travel in
  `requestState`. mcpx instead keeps the upstream call running and puts only its handle in the token.
- **Where.** 2026-07-28 describes the stateless option; it is not a MUST.
- **mcpx @ 05c78b2.** An interruptible call is a daemon task tracked in an `askTable` (`internal/daemon/routes_proto.go:53-65`);
  the token's `c` is its task id (wire M2). If the host disconnects, the call "keeps running as a task"
  (`internal/mcpserver/ask.go:157-162`), and its result is kept for `proto.askTTL` (15 m).
- **Value to mcpx.** + med: a live upstream call cannot be serialised into a token, so this is the server-minted-handle
  pattern, and it is right for a proxy. What it costs is that a resume must reach the same daemon.
- **Effort.** L to change, and not worth changing.
- **Risk.** med: a disconnect does not cancel the upstream call on this path (unlike ordinary calls; see
  [progress-cancellation.md](progress-cancellation.md)), and these tasks appear in `mcpx_tasks_list` but not in MCP
  `tasks/list`, because the two task stores differ ([tasks.md](tasks.md)).
- **Detail.** Resuming therefore needs both a valid token and the task still alive: after a daemon restart the key and the
  call are both gone, which the state comment calls intended.
- **Sources.** `2026-07-28/basic/patterns/mrtr.mdx:199` "without maintaining any server-side state";
  `internal/mcpserver/ask.go:159` "keeps running as a task, and its questions keep their";
  `internal/daemon/routes_proto.go:53-65`; `internal/defaults/defaults.json:75` "\"askTTL\": \"15m\",";
  `internal/mcpserver/state.go:27-28` "The key is per process and never persisted."; wire M2.
