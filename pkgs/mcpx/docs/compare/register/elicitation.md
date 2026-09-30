# Elicitation

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  elicitation/create per revision (form, url, 2026 via MRTR), its schema and capability rules, and how mcpx, opencode and lootbox carry it
```

Elicitation is how a server asks the user something through the client: a flat form (2025-06-18), a URL to open
(2025-11-25), and from 2026-07-28 a question embedded in an `input_required` result rather than a request of its own. It
is the one client feature 2026 does not deprecate, and for mcpx it is central: every upstream question lands in mcpx's
broker and is either relayed to the host that made the call or answered by another audience. What matters most is that
the relay works for legacy hosts and for 2026 hosts that keep an `Mcp-Session-Id`, but a correct stateless 2026 host gets
`-32603` from any eliciting upstream (ELI-17), and that mcpx never tells upstreams it can take URL mode although its
broker does. The 2026 mechanism itself (`InputRequiredResult`, `requestState`, retries, and mcpx as an MRTR client of a
modern upstream) is registered in [mrtr.md](mrtr.md).

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| ELI-01 | `elicitation/create` and the `elicitation` client capability | `2025-06-18 has` `opencode v2 has` | `24-11 — · 25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ (MRTR)`; opencode v2 ✓, v1 ✗, lootbox ✗ | ✓ — both directions through the broker | + high | S | low |
| ELI-02 | `requestedSchema` limited to flat primitive properties | `2025-06-18 has` | `25-06 ✓ · 25-11 ✓ (+ arrays for multi-select) · 26-07 ✓` | ✓ — forwarded verbatim; never validated | + low | S | low |
| ELI-03 | Three-way action `accept` / `decline` / `cancel` | `2025-06-18 has` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✓ — `cancel` when nobody was asked | + low | S | low |
| ELI-04 | Enum schemas: `enumNames` → titled/untitled single and multi-select | `2025-11-25 has` `mcpx missing` | `25-06 enum+enumNames · 25-11 rich · 26-07 rich` | partial — rich enums reach 2025-06-18 hosts undowngraded (#214) | + low | M | low |
| ELI-05 | `default` on every primitive schema | `2025-11-25 has` `opencode v2 has` | `25-06 boolean only · 25-11 all · 26-07 all`; opencode v2 applies defaults | ✓ — forwarded unchanged | + low | S | low |
| ELI-06 | `ElicitResult.content` values may be `string[]` | `2025-11-25 has` | `25-06 scalars · 25-11 + string[] · 26-07 + string[]` | ✓ — answers relayed as raw JSON | + low | S | low |
| ELI-07 | `requestedSchema.$schema` allowed | `2025-11-25 has` | `25-11 ✓ · 26-07 ✓` | ✓ — forwarded verbatim | − moot | S | low |
| ELI-08 | `mode` field; omitted means form | `2025-11-25 has` | `25-11 ✓ · 26-07 ✓` | ✓ — stripped for pre-2025-11-25 hosts | + low | S | low |
| ELI-09 | URL-mode elicitation | `2025-11-25 has` `opencode v2 has` `mcpx missing` | `25-11 ✓ · 26-07 ✓`; opencode v2 renders a link field | partial — relayed; broker accepts url without declaring it (#214) | + med | S | med |
| ELI-10 | `elicitationId` added, then removed | `2025-11-25 has` `2026-07-28 removes` `mcpx missing` | `25-11 only` | partial — stripped for old hosts, leaked to 2026 hosts (#201) | + low | S | low |
| ELI-11 | `notifications/elicitation/complete` added, then removed | `2025-11-25 has` `2026-07-28 removes` `opencode v2 has` | `25-11 only`; opencode v2 handles it | partial — consumed from upstreams, never sent to hosts | + low | S | low |
| ELI-12 | `-32042` `URLElicitationRequiredError` added, then removed | `2025-11-25 has` `2026-07-28 removes` `has better replacement` `mcpx missing` | `25-11 only`; 26-07 reserves the code | ✗ — neither emitted nor handled from upstreams (#214) | + med | M | med |
| ELI-13 | Sensitive data: never → only via URL mode | `2025-11-25 has` | `25-06 never · 25-11 URL only · 26-07 URL only` | n/a — relays; broker answers persist to SQLite | + low | S | med |
| ELI-14 | 2026 `ElicitRequest` embedded in `InputRequiredResult`, not a request | `2026-07-28 has` | `26-07 only` | ✓ — produced and answered | + med | S | low |
| ELI-15 | Legacy delivery over Streamable HTTP: request on the POST's SSE | `2025-06-18 has` `2026-07-28 removes` | `25-03..25-11 allowed · 26-07 forbidden` | ✓ — works with a session (wire L1–L3) | + high | S | low |
| ELI-16 | Legacy delivery over stdio: request on the pipe | `2025-06-18 has` | `25-06 ✓ · 25-11 ✓` | ✓ — interruptible calls run concurrently; not wire-driven | + high | S | low |
| ELI-17 | Stateless 2026 host plus eliciting upstream | `2026-07-28 has` `mcpx missing` | `26-07 only` | ✗ — `-32603` after 8 busy rounds (wire M1) (#201) | + high | M | high |
| ELI-18 | Naming the originating server in a relayed question | `mcpx missing` | spec: client MUST show who asks; mcpx: legacy path only | partial — legacy prefixed "(via mcpx)"; 2026 path raw (#201) | + med | S | med |
| ELI-19 | Question from a shared upstream instance | `impl deferred` | mcpx only | partial — goes to the broker, never inline | + low | L | low |
| ELI-20 | Questions raised inside `mcpx_exec` | `impl deferred` `opencode v2 has` `mcpx missing` | mcpx only | partial — always the broker, never the host (#77) | + low | L | low |
| ELI-21 | Answering an elicitation from inside a script | `opencode v2 has` `mcpx missing` | opencode v2 UI form; mcpx designed, not built | ✗ — no `onElicit`; broker and policy only (#214) | + med | M | low |
| ELI-22 | Client with no elicitation answerer | `opencode v1 has` `lootbox has` | opencode v1 and lootbox: `-32601`; mcpx: `cancel` | ✓ — declares, answers `cancel` when nobody is asked | + low | S | low |
| ELI-23 | Elicitation attributed to a session or a whole location | `opencode v2 has` | opencode v2: Location-global forms | n/a — mcpx is the server here | + low | S | low |

## ELI-01 `elicitation/create` and the `elicitation` client capability

- **What.** A server asks the user, through the client, for structured data: `message` plus `requestedSchema`. A client
  that can answer declares `elicitation` (an opaque object in 2025-06-18).
- **Where.** 2025-06-18 onward; absent in 2024-11-05 and 2025-03-26. In 2026-07-28 it survives as an embedded input
  request (ELI-14). opencode v2 declares it and answers; opencode v1 comments it out; lootbox declares no capabilities.
- **mcpx @ 05c78b2.** Both directions, through the broker. Downstream: sent only to hosts at ≥2025-06-18 that declared it
  (`internal/mcpserver/conn.go:192`). Upstream: always declares `elicitation: {}` (`internal/mcpclient/modern.go:51`) and
  answers `cancel` when nothing is installed to ask (`internal/mcpclient/modern.go:129-131`), which keeps the always-on
  declaration honest.
- **Value to mcpx.** + high: this is the feature that lets an upstream stop and ask the user, and mcpx exists partly to
  route those questions.
- **Effort.** S — done.
- **Risk.** None as is.
- **Detail.** 2025-06-18 `ElicitRequest` params are one inline object; 2025-11-25 splits them into
  `ElicitRequestFormParams | ElicitRequestURLParams`. Upstream questions become broker rows (`elc-…` ids), answered by a
  host inline (ELI-15, ELI-16, the 2026 path in [mrtr.md](mrtr.md)), by the CLI, TUI or plugin, or by policy.
- **Sources.** `schema/2025-06-18/schema.ts:1459` "elicitation/create"; `schema/2025-06-18/schema.ts:250`
  "elicitation?: object;"; `2025-06-18/changelog.mdx:24` "Add support for"; `internal/mcpclient/modern.go:51`;
  `internal/mcpserver/conn.go:192`; `v2:packages/core/src/mcp/client.ts:143`;
  `v1:packages/opencode/src/mcp/index.ts:44` "// elicitation: {},";
  `.lootbox/src/lib/external-mcps/mcp_client_manager.ts:150-153`.

## ELI-02 `requestedSchema` limited to flat primitive properties

- **What.** `type: "object"` with `properties` of string, number, boolean or enum schemas and an optional `required`
  list. No nesting and no arrays of objects, so any client can render it as a form.
- **Where.** 2025-06-18 onward. 2025-11-25 adds array-typed multi-select enums (ELI-04).
- **mcpx @ 05c78b2.** Forwards the schema verbatim in both eras: the modern path copies the upstream's params
  (`internal/mcpserver/ask.go:259`); the legacy path rewrites only `message`, `mode` and `elicitationId`
  (`internal/mcpserver/conn.go:382-407`). mcpx does not check that an upstream kept to the subset.
- **Value to mcpx.** + low: the restriction is what makes a CLI or TUI prompt feasible.
- **Effort.** S.
- **Risk.** low: an upstream sending a nested schema reaches the host unchanged, and the host decides.
- **Detail.** `StringSchema.format` is one of `email`, `uri`, `date`, `date-time`. opencode v2 maps each property to a
  typed form field and turns arrays into multi-select, replacing machine titles such as "string with format email" with
  the description or key.
- **Sources.** `schema/2025-06-18/schema.ts:1485` "export type PrimitiveSchemaDefinition =";
  `2025-06-18/client/elicitation.mdx:279` "Note that complex nested structures, arrays of objects";
  `internal/mcpserver/ask.go:259`; `v2:packages/core/src/mcp/index.ts:224`.

## ELI-03 Three-way action `accept` / `decline` / `cancel`

- **What.** `accept` (with `content` in form mode), `decline` (an explicit no), `cancel` (dismissed without answering).
- **Where.** 2025-06-18 onward, unchanged.
- **mcpx @ 05c78b2.** Keeps the distinction: with no answerer installed the client replies `cancel`, "Cancel, not decline.
  Nobody was asked, so nobody said no." (`internal/mcpclient/modern.go:129-131`).
- **Value to mcpx.** + low: correct already.
- **Effort.** S.
- **Risk.** None.
- **Detail.** In URL mode `accept` means the user consented to open the URL, not that the out-of-band flow finished
  (ELI-14). Sampling has no such three-way answer: mcpx turns a declined sampling question into an error (see
  [sampling.md](sampling.md)).
- **Sources.** `schema/2025-06-18/schema.ts:1550` "action: \"accept\" | \"decline\" | \"cancel\";";
  `internal/mcpclient/modern.go:130` "Cancel, not decline.".

## ELI-04 Enum schemas: `enumNames` → titled/untitled single and multi-select

- **What.** 2025-06-18 has `EnumSchema{enum, enumNames?}`. 2025-11-25 (SEP-1330) replaces it with
  `UntitledSingleSelect{enum}`, `TitledSingleSelect{oneOf[{const,title}]}`, `UntitledMultiSelect{type:"array",
  items.enum}`, `TitledMultiSelect{items.anyOf}`, plus a `LegacyTitledEnumSchema` (the old `enumNames`).
- **Where.** Simple enums in 2025-06-18; rich enums in 2025-11-25 and 2026-07-28.
- **mcpx @ 05c78b2.** When a 2025-11-25 upstream's question goes to a 2025-06-18 host, only `mode` and `elicitationId` are
  stripped (`internal/mcpserver/conn.go:398-404`); `oneOf` and array enums go through unchanged.
- **Value to mcpx.** + low: downgrade titled single-select to `enum` + `enumNames` for 2025-06-18 hosts; multi-select has
  no 2025-06-18 equivalent and would have to be refused or flattened.
- **Effort.** M — a schema rewrite per property, plus mapping multi-select answers back.
- **Risk.** low: a strict 2025-06-18 host rejects the whole request, the same risk the conn.go comment names for `mode`.
- **Detail.** Multi-select answers are `string[]` (ELI-06). `LegacyTitledEnumSchema` "will be removed in a future version"
  but is not in the 2026 deprecated-features registry.
- **Sources.** `schema/2025-06-18/schema.ts:1535` "enumNames?: string[]; // Display names for enum values";
  `schema/2025-11-25/schema.ts:2463` "export type EnumSchema =";
  `2025-11-25/changelog.mdx:16` "Update"; `schema/2026-07-28/schema.ts:3096`; `internal/mcpserver/conn.go:398-404`.

## ELI-05 `default` on every primitive schema

- **What.** 2025-06-18 allows `default` only on `BooleanSchema`. 2025-11-25 (SEP-1034) adds it to string, number and enum
  schemas; clients that support defaults SHOULD prefill.
- **Where.** 2025-11-25 and 2026-07-28. opencode v2 declares `form: { applyDefaults: true }`, so its SDK fills schema
  defaults into accepted content.
- **mcpx @ 05c78b2.** Forwarded unchanged to hosts of every revision (as ELI-04). Whether mcpx's own CLI prompt prefills
  was not examined.
- **Value to mcpx.** + low: prefill in `mcpx elicit` prompts.
- **Effort.** S.
- **Risk.** low: an extra field to a strict 2025-06-18 host.
- **Detail.** `applyDefaults` is an SDK option carried inside the `form` capability object, which the spec types as an
  open object.
- **Sources.** `schema/2025-06-18/schema.ts:1524` "default?: boolean;"; `schema/2025-11-25/schema.ts:2247`
  "default?: string;"; `2025-11-25/client/elicitation.mdx:233` "All primitive types support optional default values";
  `v2:packages/core/src/mcp/client.ts:143`.

## ELI-06 `ElicitResult.content` values may be `string[]`

- **What.** Accepted content values are `string | number | boolean` in 2025-06-18, and may also be `string[]` from
  2025-11-25, for multi-select enums.
- **Where.** 2025-11-25 and 2026-07-28.
- **mcpx @ 05c78b2.** On the server side answers stay raw JSON: a modern host's `inputResponses` are read as
  `map[string]json.RawMessage` (`internal/mcpserver/ask.go:79-89`) and a legacy host's reply is taken as raw
  (`internal/mcpserver/ask.go:214`). How the broker and CLI decode array answers was not examined.
- **Value to mcpx.** + low: needed only once multi-select is rendered.
- **Effort.** S.
- **Risk.** low: a decoder typed to the 2025-06-18 shape rejects arrays.
- **Detail.** Downgrading to a 2025-06-18 host would also have to map an array answer back (ELI-04).
- **Sources.** `schema/2025-06-18/schema.ts:1556` "content?: { [key: string]: string | number | boolean };";
  `schema/2025-11-25/schema.ts:2485` "content?: { [key: string]: string | number | boolean | string[] };";
  `internal/mcpserver/ask.go:82`.

## ELI-07 `requestedSchema.$schema` allowed

- **What.** An optional `$schema` on the form schema (JSON Schema 2020-12 is the default dialect).
- **Where.** 2025-11-25 and 2026-07-28.
- **mcpx @ 05c78b2.** Forwarded verbatim with the rest of the schema (ELI-02).
- **Value to mcpx.** − moot.
- **Effort.** S.
- **Risk.** None.
- **Detail.** Not mentioned in the 2025-11-25 changelog.
- **Sources.** `schema/2025-11-25/schema.ts:2171` "$schema?: string;".

## ELI-08 `mode` field; omitted means form

- **What.** `mode?: "form"` or `mode: "url"`. Servers MAY omit it for form mode, and clients MUST treat an absent `mode` as
  form.
- **Where.** 2025-11-25 and 2026-07-28.
- **mcpx @ 05c78b2.** Stripped for hosts below 2025-11-25 (`internal/mcpserver/conn.go:398-402`); a URL question never
  reaches such a host because `Sendable` refuses it.
- **Value to mcpx.** + low: done.
- **Effort.** S.
- **Risk.** None.
- **Detail.** The same strip removes `elicitationId` (ELI-10).
- **Sources.** `schema/2025-11-25/schema.ts:2159` "mode?: \"form\";";
  `2025-11-25/client/elicitation.mdx:97` "For backwards compatibility, servers"; `internal/mcpserver/conn.go:398-402`.

## ELI-09 URL-mode elicitation

- **What.** `mode: "url"`, `message`, `url`: the user completes something out of band (credentials, OAuth, payment) and
  nothing but the URL passes through the client.
- **Where.** 2025-11-25 and 2026-07-28. opencode v2 renders an external-link field and settles the form on the answer (or,
  in legacy, on the completion notification).
- **mcpx @ 05c78b2.** Relays URL questions only to hosts that declared `url` (CAP-08). As a client it never declares `url`,
  yet `elicitViaBroker` accepts `mode: "url"` and files it under the upstream's `elicitationId`
  (`internal/daemon/hooks.go:122-146`), and an upstream `notifications/elicitation/complete` marks the row accepted
  (`internal/daemon/hooks.go:55-68`). It handles what it never asked for.
- **Value to mcpx.** + med: declaring `url` would let OAuth-style servers use it legitimately rather than by accident.
- **Effort.** S for the declaration; the per-host gating is CAP-08.
- **Risk.** med: two upstreams choosing the same `elicitationId` collide in the broker, because the row id is theirs.
- **Detail.** Clients MUST NOT prefetch the URL, MUST show it in full and MUST get consent before opening it. `docs/protocol.md`
  also lists as a known gap that mcpx never starts a URL elicitation of its own (for example for upstream OAuth). The 2026
  URL params are only `mode`, `message`, `url`.
- **Sources.** `schema/2025-11-25/schema.ts:2189` "mode: \"url\";"; `2025-11-25/changelog.mdx:17` "Added support for";
  `internal/daemon/hooks.go:138` "ID:        p.ElicitationID,"; `internal/mcpclient/modern.go:51`;
  `docs/protocol.md:376` "url-mode elicitation raised by mcpx itself."; `schema/2026-07-28/schema.ts:2821-2838`;
  `v2:packages/core/src/mcp/index.ts:226`.

## ELI-10 `elicitationId` added, then removed

- **What.** A required opaque id on 2025-11-25 URL requests, used to correlate the completion notification and the
  `-32042` error. 2026 deletes it; a server encodes correlation in `requestState` instead.
- **Where.** 2025-11-25 only.
- **mcpx @ 05c78b2.** Stripped for hosts below 2025-11-25 (`internal/mcpserver/conn.go:398-404`). The 2026 path copies the
  upstream's params verbatim (`internal/mcpserver/ask.go:259`), so a 2025-11-25 upstream's `elicitationId` reaches a 2026
  host whose schema has no such field. As a client, mcpx uses it as the broker row id (ELI-09).
- **Value to mcpx.** + low: extend the strip to 2026 hosts.
- **Effort.** S.
- **Risk.** low: an undefined field sent to 2026 hosts (a send-conservatively conflict).
- **Detail.** opencode v2's code notes the same fact: "Legacy only: 2026-07-28 has no elicitationId and no completion
  notification".
- **Sources.** `schema/2025-11-25/schema.ts:2200` "elicitationId: string;"; `2026-07-28/changelog.mdx:53` "Remove the";
  `internal/mcpserver/ask.go:259`; `internal/mcpserver/conn.go:398-404`;
  `v2:packages/core/src/mcp/index.ts:226` "Legacy only: 2026-07-28 has no elicitationId".

## ELI-11 `notifications/elicitation/complete` added, then removed

- **What.** An optional server-to-client notice that an out-of-band URL interaction finished. Removed in 2026 because
  under MRTR the client learns the outcome by retrying.
- **Where.** 2025-11-25 only. opencode v2 handles it (legacy URL mode); v1 does not.
- **mcpx @ 05c78b2.** As a client it consumes it from upstreams (`internal/mcpclient/client.go:501`). As a server it has an
  emitter (`internal/events/events.go:262-268`) that nothing reaches: the listen filter has no elicitation kind
  (`internal/cli/serve.go:727-739`), so no host ever gets one and `FeatElicitationComplete` in the feature table is never
  exercised.
- **Value to mcpx.** + low: a 2025-11-25 host that opened a URL through mcpx would learn completion early.
- **Effort.** S — but it would have to be gated to 2025-11-25 hosts and to the one host that got the URL request.
- **Risk.** low: MAY-level; hosts learn the outcome from the call's result anyway.
- **Detail.** The notification MUST go only to the client that received the URL request and MUST carry its
  `elicitationId`. The emitter maps any `ElicitCompleted` event, with no revision gate at that point; the feature table
  gives it a 2026 ceiling (`internal/mcpserver/revisions.go:103`).
- **Sources.** `schema/2025-11-25/schema.ts:2494` "notifications/elicitation/complete";
  `2025-11-25/client/elicitation.mdx:398` "Servers **MAY** send a"; `internal/mcpclient/client.go:501`;
  `internal/events/events.go:262-268`; `v2:packages/core/src/mcp/client.ts:160`.

## ELI-12 `-32042` `URLElicitationRequiredError` added, then removed

- **What.** 2025-11-25: a tool may fail with `-32042` listing URL elicitations (`data.elicitations`) that must complete
  before a retry. 2026 removes the type; `-32042` is reserved "2025-11-25 only" and MUST NOT be emitted. An
  `InputRequiredResult` carrying a URL `elicitation/create` replaces it.
- **Where.** 2025-11-25 only.
- **mcpx @ 05c78b2.** `-32042` appears nowhere in `internal/mcpserver` or `internal/mcpclient`. Whether an upstream's
  `-32042` passes through to hosts unchanged was not verified.
- **Value to mcpx.** + med: as a client of 2025-11-25 upstreams, turn `-32042` into URL questions for the host (legacy
  `elicitation/create`, or `input_required` for a 2026 host) and retry the call.
- **Effort.** M.
- **Risk.** med: tools that rely on `-32042` fail through mcpx.
- **Detail.** The constant is `@internal` and sits in the implementation-defined range. The removal is changelog-silent:
  the 2026 changelog mentions `elicitationId` and the notification but not this error; only `basic/index.mdx` and a schema
  comment record it. The 2025-11-25 changelog never named the addition either.
- **Also recorded from the errors register.** Absent from `internal/mcpserver` and `internal/mcpclient`. As a client
  of 2025-11-25 upstreams, a tool that answers `-32042` fails through mcpx instead of becoming a URL elicitation and a
  retry; as a server, an upstream `-32042` would need translating for a 2026 host. The 2025-11-25 constant is marked
  `@internal` and commented as belonging to the `[-32000, -32099]` implementation-defined range; neither its addition
  nor its removal is in a changelog. Under the 2026 policy it lies in the specification's sub-range, reserved as a
  historical code.
- **Sources.** `schema/2025-11-25/schema.ts:181` "export const URL_ELICITATION_REQUIRED = -32042;";
  `2025-11-25/client/elicitation.mdx:426` "the server **MAY** return a";
  `2026-07-28/basic/index.mdx:144` "URL elicitation required (2025-11-25 only).".

## ELI-13 Sensitive data: never → only via URL mode

- **What.** 2025-06-18: servers MUST NOT request sensitive information by elicitation. 2025-11-25 narrows it to form mode
  and requires URL mode for sensitive interactions. 2026 keeps the 2025-11-25 rule.
- **Where.** As stated.
- **mcpx @ 05c78b2.** Relays whatever the upstream asks. Broker rows persist in SQLite (`state/logs/elicit.db`,
  `internal/daemon/hooks.go:235-241`), so a secret collected by a misbehaving upstream in form mode is stored.
- **Value to mcpx.** + low: mcpx's own prompts must never collect secrets by form.
- **Effort.** S.
- **Risk.** med: secrets in the broker database and the event bus if an upstream breaks the rule.
- **Detail.** The rule binds the asking server, not the relay; a relay can only limit what it keeps.
- **Sources.** `2025-06-18/client/elicitation.mdx:32` "Servers **MUST NOT** use elicitation to request sensitive information.";
  `2025-11-25/client/elicitation.mdx:30` "Servers **MUST NOT** use form mode elicitation to request sensitive information such as";
  `internal/daemon/hooks.go:235-241`.

## ELI-14 2026 `ElicitRequest` embedded in `InputRequiredResult`, not a request

- **What.** In 2026 `ElicitRequest` is a bare `{method, params}` (no `jsonrpc`, no `id`) inside `inputRequests`; its params
  have no `_meta` or `task`; `ElicitResult` no longer extends `Result`. A URL `accept` means consent; the outcome is learned
  on the retry.
- **Where.** 2026-07-28. Elicitation is not deprecated there, unlike Roots, Sampling and Logging (SEP-2577 names only
  those three).
- **mcpx @ 05c78b2.** Produces it for 2026 hosts (`internal/mcpserver/ask.go:256-266`) and answers it from modern upstreams
  with the same `answer()` used for legacy requests (`internal/mcpclient/modern.go:103-135`).
- **Value to mcpx.** + med: done.
- **Effort.** S.
- **Risk.** None beyond the mechanism's own gaps ([mrtr.md](mrtr.md)).
- **Detail.** A 2025-11-25 `ElicitRequestFormParams` extended `TaskAugmentedRequestParams`, so an elicitation could run as
  a client-hosted task; that path is gone (see [tasks.md](tasks.md)).
- **Sources.** `schema/2026-07-28/schema.ts:2856` "export interface ElicitRequest {";
  `schema/2026-07-28/schema.ts:3134` "export interface ElicitResult {";
  `2026-07-28/client/elicitation.mdx:376` "The response with"; `2026-07-28/changelog.mdx:73`;
  `internal/mcpclient/modern.go:103-135`.

## ELI-15 Legacy delivery over Streamable HTTP: request on the POST's SSE

- **What.** A legacy server may send its own requests on the SSE stream of a POST before the response. 2026 forbids
  independent requests on any stream.
- **Where.** 2025-03-26..2025-11-25 transport rule; elicitation itself from 2025-06-18; forbidden in 2026-07-28.
- **mcpx @ 05c78b2.** When an upstream elicits during `mcpx_call`, `prompts/get` or `resources/read`, a ≥2025-06-18 host
  that declared elicitation and holds a session gets `elicitation/create` (negative id, `-1`) as an SSE frame on its POST;
  it answers with a separate POST (202), and the result follows on the same stream (`internal/mcpserver/ask.go:212-229`;
  `internal/mcpserver/conn.go:228-274`). Wire L1–L3 shows exactly this, with the message prefixed
  "ask (via mcpx) asks:".
- **Value to mcpx.** + high: inline answers from the host that made the call.
- **Effort.** S — done.
- **Risk.** low: it needs an `Mcp-Session-Id`, because the answer POST finds the waiting request by session
  (`internal/mcpserver/server.go:1142-1151`).
- **Detail.** Only the three methods above are interruptible (`internal/mcpserver/ask.go:318-325`). A host's call is held
  up to `proto.askTimeout` (10 m). When a relayed request times out, mcpx sends the host `notifications/cancelled` for it
  (see [progress-cancellation.md](progress-cancellation.md)). The transports register owns the SSE rules themselves.
- **Sources.** `2025-03-26/basic/transports.mdx:111` "The server **MAY** send JSON-RPC _requests_ and _notifications_ before sending a";
  `2026-07-28/basic/transports/streamable-http.mdx:117` "The server **MUST NOT** send independent JSON-RPC _requests_ on this stream.";
  `internal/mcpserver/conn.go:395`; `internal/mcpserver/server.go:1142-1151`; wire L1–L3.

## ELI-16 Legacy delivery over stdio: request on the pipe

- **What.** On stdio the server writes `elicitation/create` to stdout and reads the client's reply from stdin while the
  original call is still open.
- **Where.** 2025-06-18 and 2025-11-25 (2026 forbids server-written requests on stdio).
- **mcpx @ 05c78b2.** An interruptible request runs on its own goroutine so the answer can arrive on the same read loop;
  replies are recognised by shape (an id and no method) and routed to the waiting request
  (`internal/mcpserver/server.go:1032-1039`, `:1055-1062`).
- **Value to mcpx.** + high: works for every spawning host that declares elicitation.
- **Effort.** S — done.
- **Risk.** low: verified by reading code only; the wire study drove HTTP elicitation, not a stdio host answering.
- **Detail.** Everything else on stdio is handled in line, so a long non-interruptible call still blocks the pipe (a
  transports-register item).
- **Sources.** `internal/mcpserver/server.go:1036` "if id, result, rerr, ok := replyOf([]byte(line)); ok {";
  `internal/mcpserver/server.go:1055-1062`.

## ELI-17 Stateless 2026 host plus eliciting upstream

- **What.** A 2026 host that declares elicitation and calls, over HTTP without an `Mcp-Session-Id` (2026 removed it), an
  upstream tool that elicits. The host should receive an `input_required` result; from mcpx it gets `-32603`.
- **Where.** 2026-07-28, server side.
- **mcpx @ 05c78b2.** `mint` refuses an empty connection binding (`internal/mcpserver/state.go:64`), and an HTTP request
  without a session id gets an anonymous connection with an empty id (`internal/mcpserver/server.go:1209-1218`). In
  `viaAsk` the round counter is incremented before the mint and the failed mint `continue`s without waiting, so rounds 1–8
  spin at once and the ninth exceeds `proto.askRounds`; the call is abandoned (`internal/mcpserver/ask.go:195-209`). Wire
  M1: `-32603 "tools/call was still asking for input after 8 rounds"` after about a second. With a session id taken from
  `server/discover` the same call works (wire M2–M3).
- **Value to mcpx.** + high: this is the whole 2026 server-side elicitation story for correct clients.
- **Effort.** M — bind `requestState` to something the request carries (or to nothing but the HMAC, plus a method and
  params digest; see [mrtr.md](mrtr.md)), and make a failed mint fall back to the broker as intended.
- **Risk.** high: every stateless 2026 host that declares elicitation gets an internal error from any eliciting upstream.
  A 2026 host that declares nothing takes the broker path and works.
- **Detail.** Both the code comment and the doc say the intended fallback is the broker ("the question goes back to the
  broker"; "its questions stay with the broker"); the round counter defeats it. The binding choice itself is registered in
  [mrtr.md](mrtr.md); that mcpx mints a session id on `server/discover` at all is a lifecycle-register item.
- **Sources.** `internal/mcpserver/state.go:64`; `internal/mcpserver/ask.go:195-209`;
  `internal/mcpserver/ask.go:204` "No verifiable state means no safe resume";
  `docs/protocol.md:225-228` "is issued **no** token at all, and its questions stay with the broker";
  `internal/mcpserver/server.go:1209-1218`; `2026-07-28/changelog.mdx:12` "Remove protocol-level sessions"; wire M1, M2–M3.

## ELI-18 Naming the originating server in a relayed question

- **What.** Clients MUST make clear which server is asking. Behind a proxy the client sees only the proxy, so the proxy has
  to put the originator into the question.
- **Where.** Client rule from the elicitation pages; mcpx applies it on the legacy path only.
- **mcpx @ 05c78b2.** Legacy: `paramsFor` prefixes `"<server> (via mcpx) asks: "` unless the message already names the
  server (`internal/mcpserver/conn.go:390-396`; wire L2). 2026: `inputRequired` copies the upstream's params verbatim, with
  no prefix (`internal/mcpserver/ask.go:259`; wire M2 shows the bare "Which repository should this go in?").
- **Value to mcpx.** + med: a 2026 host cannot tell which upstream is asking, which matters most for confirmations and
  credentials.
- **Effort.** S — run the same rewrite on the modern path (and drop `elicitationId` there, ELI-10).
- **Risk.** med: users answering questions without knowing who asked.
- **Detail.** The `inputRequests` keys are broker ids (`elc-…`), which is fine; only the params differ between the paths.
- **Sources.** `2025-11-25/client/elicitation.mdx:42` "Provide UI that makes it clear which server is requesting information";
  `internal/mcpserver/conn.go:395`; `internal/mcpserver/ask.go:259` "requests[q.ID] = map[string]any{"; wire L2, M2.

## ELI-19 Question from a shared upstream instance

- **What.** A question arrives on an upstream connection, not on a call. When two calls share one pooled instance
  (`sharing: shared`), mcpx cannot tell which call asked.
- **Where.** mcpx design; the spec is silent.
- **mcpx @ 05c78b2.** `forKey` attributes a question only when exactly one call is running on that (server, scope key)
  (`internal/daemon/routes_proto.go:151-160`); otherwise the question goes to the broker's default audience. The doc says
  so and why (`docs/protocol.md:241-244`).
- **Value to mcpx.** + low: safety over convenience; one host's credential prompt never reaches another host.
- **Effort.** L — needs request-level correlation from the upstream.
- **Risk.** low: hosts using shared servers never get inline questions.
- **Detail.** 2025-11-25's `io.modelcontextprotocol/related-task` `_meta` would correlate exactly for task-augmented
  upstream calls, but mcpx never task-augments upstream calls (see [tasks.md](tasks.md)) and 2026 drops the key.
- **Sources.** `docs/protocol.md:241-244` "mcpx makes none, and the question goes to the broker";
  `internal/daemon/routes_proto.go:156`.

## ELI-20 Questions raised inside `mcpx_exec`

- **What.** Upstream calls made by a script never interrupt the MCP host that ran the script; their questions go to the
  broker.
- **Where.** mcpx design.
- **mcpx @ 05c78b2.** `daemonAsker.Begin` treats only `mcpx_call` as interruptible (`internal/cli/serve_ask.go:44-58`); the
  gap is written down (`docs/protocol.md:379-382`): the correlation identifies a call, and a script is many calls.
- **Value to mcpx.** + low: scripts that elicit could run under the host's UI.
- **Effort.** L.
- **Risk.** low: nothing new breaks.
- **Detail.** The script-side answer to the same problem is ELI-21.
- **Also recorded from the code mode register.** opencode v2 (UI form, attributed to a `"global"` sentinel session
  because the server "cannot attribute them to a persisted session row"). Cloudflare (agent-level handlers). mcpx
  (broker; no script hook). opencode v1 (capability commented out; answers `-32601`) and lootbox (`capabilities: {}`;
  elicitation register, ELI-22) refuse. Broker routes `GET /v1/elicit`, `GET /v1/elicit/{id}`, `POST
  /v1/elicit/{id}/{action}` (`internal/daemon/server.go:422-424`). `daemonAsker.Begin` treats only `mcpx_call` as
  interruptible (`internal/cli/serve_ask.go:44-58`). The script-side `onElicit(q => …)` is designed in a proposal
  (`docs/elicitation.md:7` status `proposed`; `docs/elicitation.md:308-330`) and not built (rg finds no `onElicit` in
  `internal/codegen`). mcpx defers the inline case on purpose: "A script makes many upstream calls and the question
  belongs to one of them; the correlation in §3.4 identifies a *call*, and a script is not one"
  (`docs/protocol.md:379-382`). Protocol details live in the elicitation register (ELI-20, ELI-21).
- **Sources.** `docs/protocol.md:379` "A modern client cannot answer a question raised by";
  `internal/cli/serve_ask.go:57` "p.Name != \"mcpx_call\"".; `docs/elicitation.md:321` `onElicit(async (q) => {`; `v2:packages/core/src/mcp/client.ts:143`; `v2:packages/core/src/mcp/index.ts:80-82`; https://developers.cloudflare.com/agents/model-context-protocol/apis/client-api/#elicitation ("On the stateless path, elicitation returns `input_required` and completes through multi-round-trip requests (MRTR).")

## ELI-21 Answering an elicitation from inside a script

- **What.** When a tool call made by a script asks a question, the script itself could answer it (it often knows the
  repository or the choice). opencode v2 shows a form in its UI instead; Cloudflare's agent runs registered handlers.
- **Where.** opencode v2 (UI form, form and URL modes). mcpx: daemon broker only. opencode v1 and lootbox declare no
  elicitation capability.
- **mcpx @ 05c78b2.** The broker has `GET /v1/elicit`, `GET /v1/elicit/{id}` and `POST /v1/elicit/{id}/{action}`
  (`internal/daemon/server.go:422-424`), but the generated script client has no `onElicit`; rg finds none in
  `internal/codegen`. `docs/elicitation.md:308-330` describes `onElicit` as if a script could register it today.
- **Value to mcpx.** + med: a script that expects a question could answer from its own data instead of failing or waiting
  for a human.
- **Effort.** M — a long-poll or SSE from the generated client to `/v1/elicit?run=<id>`, plus handler registration.
- **Risk.** low: without it scripts depend on policy answers or a human at the CLI or TUI.
- **Detail.** The doc is ahead of the code, so the gap is a doc/code disagreement as well as a missing feature.
- **Sources.** `docs/elicitation.md:318-324` "onElicit(async (q) => {"; `internal/daemon/server.go:422-424`;
  `v2:packages/core/src/mcp/client.ts:143`.

## ELI-22 Client with no elicitation answerer

- **What.** What a client replies when it receives `elicitation/create` and has nothing to answer with.
- **Where.** opencode v1 registers only `roots/list`, so its SDK answers every other server request with `-32601`. lootbox
  declares `capabilities: {}` and registers no handler, so its SDK does the same. mcpx declares elicitation and replies
  `cancel`.
- **mcpx @ 05c78b2.** As a server it never sends `elicitation/create` to a host that did not declare it
  (`internal/mcpserver/conn.go:192`), so opencode v1 never sees one. As a client it answers `cancel` rather than an error
  (`internal/mcpclient/modern.go:129-131`).
- **Value to mcpx.** + low: confirms that capability gating is enough.
- **Effort.** S.
- **Risk.** low. `docs/opencode-plugin.md:184` says v1 "will get `cancel`"; on the wire v1 would answer `-32601`, not an
  `ElicitResult` (moot, because mcpx never sends it).
- **Detail.** The 1.x SDK also refuses to register an elicitation handler without the capability.
- **Sources.** `v1:packages/opencode/src/mcp/index.ts:77`; `v1:packages/opencode/src/mcp/index.ts:44` "// elicitation: {},";
  `.lootbox/src/lib/external-mcps/mcp_client_manager.ts:150-153`; `internal/mcpclient/modern.go:129-131`;
  `docs/opencode-plugin.md:184`.

## ELI-23 Elicitation attributed to a session or a whole location

- **What.** opencode v2 turns `elicitation/create` into a form owned by the sentinel session id `"global"`, because "the
  server cannot attribute them to a persisted session row". Every session in that Location sees it.
- **Where.** opencode v2 only (v1 has no elicitation).
- **mcpx @ 05c78b2.** mcpx is the server here: its questions reach v2 users as Location-global forms. On the legacy path
  the "(via mcpx) asks:" prefix at least names the upstream (ELI-18).
- **Value to mcpx.** + low: v2 users can answer mcpx questions at all.
- **Effort.** S.
- **Risk.** low: with several sessions open, it is unclear whose question it is.
- **Detail.** v2 maps form properties to typed fields and URL mode to an external-link field. mcpx's own analogue of the
  attribution problem is ELI-19.
- **Sources.** `v2:packages/core/src/mcp/index.ts:80` "MCP elicitations are Location-scoped, not Session-scoped";
  `v2:packages/core/src/mcp/index.ts:82`; `v2:packages/core/src/mcp/client.ts:160`.
