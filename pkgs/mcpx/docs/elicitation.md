# Elicitation: a design

```
created:      2026-09-28T18:00:00-05:00
last-updated: 2026-09-28T18:00:00-05:00
increment:    1
status:       partial -- largely shipped, and differently in places
tags:         area:protocol, area:integration
description:  how a server asks a question, and how the answer gets back
              through a CLI, a REST call, a script, an MCP host or a plugin.
```

**This was written before the feature existed, and the feature has since
shipped -- not always in the shape proposed here.** It is kept as the design
record, because the reasoning is still the reasoning. What actually ships is in
`docs/parity.md` (the `elicit_*` rows) and `docs/consumer.md`; the authority is
`internal/elicit/`, `internal/cli/elicit.go` and the three `/v1/elicit` routes
in `internal/daemon/server.go:424-426`.

Where this document and the code disagree, the code is right. The disagreements
worth knowing before you read on:

| this document says | what shipped |
| --- | --- |
| §2's three "live bugs" | all three fixed: `dispatch` routes server-initiated requests (`internal/mcpclient/client.go:404-421`), `negotiate` no longer echoes a version it does not serve (`internal/mcpserver/server.go:1092-1095`), and the client declares `elicitation` (`internal/mcpclient/modern.go:71-84`) |
| `mcpx call --elicit=<mode>` | not built. There is no `--elicit` flag on any command |
| a waiting call exits `75` | built (#286), with a different document: `mcpx call`, `mcpx exec` and `mcpx run` exit `75` with the question, its schema, the `mcpx elicit answer` command and the task id that collects the result. The call keeps running on the daemon as a task. The wire is `POST /v1/call` with `X-Mcpx-Input: report`, answered `202 {"inputRequired": {...}}` -- see §5.1 |
| `POST /v1/elicit/<id>/answer` | `POST /v1/elicit/{id}/{action}` -- the action is in the path, the body is content only |
| `GET /v1/elicit/subscribe` | not built. `mcpx elicit watch` polls; the SSE stream is `GET /v1/events` |
| `mcpx_elicit_pending` | the tool is `mcpx_elicit_list` |
| `plumbing.elicitTTL`, `elicit.mode`, `elicit.rules` | none exist. The TTL is `elicit.ttl` in `defaults.json` and is not a setting; the shipped policy is `elicit.Route` (`internal/elicit/elicit.go:491`), which is not rule-based |
| `MCPX_PLUGIN_ELICIT` | does not exist |
| `onElicit()` / `ask()` script globals, the TUI view, the `mcpx-elicitation` skill | none built |
| a `202` with `Location` and `Retry-After`, an `Elicitation` OpenAPI component | not built. The retry shape that shipped is MCP-side `input_required` |

---

## 1. What elicitation actually is

Every other MCP method runs client → server. Elicitation runs the other way: a
**server asks the client a question** in the middle of handling a tool call,
and waits for an answer before finishing.

```
mcpx ──── tools/call ────────────────▶ server
mcpx ◀─── elicitation/create ──────── server     "which account?"
mcpx ──── {action:"accept", ...} ────▶ server
mcpx ◀─── the tool result ──────────── server
```

Two modes:

| mode | carries | for |
| --- | --- | --- |
| `form` | `requestedSchema`, a flat object of primitives | a value the caller can type |
| `url` | `url` + `elicitationId` | anything that must not pass through a model: credentials, OAuth, payment |

Three answers, and the distinction matters:

- **`accept`** — approved, with `content` matching the schema
- **`decline`** — explicitly refused. The server should offer an alternative.
- **`cancel`** — dismissed without choosing. The server may ask again later.

Collapsing decline and cancel is the obvious mistake. A server that treats
"not now" as "never" stops offering something the user wanted.

### Two eras, and mcpx is in the older one

This is bigger than elicitation, and correcting it is a prerequisite.

The spec divides implementations into two **eras**:

| | versions | how it works |
| --- | --- | --- |
| **legacy** | `2025-11-25` and earlier | `initialize` handshake establishes a session; version negotiated once |
| **modern** | `2026-07-28` (current) and later | no handshake; every request carries its version in `_meta`; `server/discover` is mandatory |

The current version is **`2026-07-28`**. It is not a draft.

The compatibility matrix is unforgiving. A modern client against a legacy
server **fails**. A legacy client against a modern server **fails**. Only a
*dual-era* implementation bridges them, and it must implement both.

mcpx was legacy on both sides when this was written: its client sent
`initialize` with `ProtocolVersion = "2025-06-18"`, and its server answered
`initialize`. It is dual-era now -- the client offers `2025-11-25`
(`internal/mcpclient/client.go:22`) and probes modern with `server/discover`
first, and the server serves both eras.

**And its server lies.** `negotiate()` echoes whatever version the client
asked for:

```console
$ printf '{"jsonrpc":"2.0","id":1,"method":"initialize",
           "params":{"protocolVersion":"2026-07-28"}}\n' | mcpx serve
  we claimed: 2026-07-28
$ ... server/discover
  -32601 no method server/discover
```

A modern client is told "yes, I speak 2026-07-28", and then finds no
`server/discover` and no `_meta` handling. The spec says an unsupported
version **MUST** get an `UnsupportedProtocolVersionError` (`-32022`) listing
what is actually supported. This was a second live bug, independent of
elicitation. It is fixed: `negotiate` echoes a version only when it is both
supported and non-modern, and otherwise answers `Latest`
(`internal/mcpserver/server.go:1092-1095`); `server/discover` is implemented
(`server.go:535`) and `-32022` exists (`server.go:327`).

### Elicitation in each era

The delivery mechanism differs, which is why the era question comes first.

- **Legacy**: a genuine mid-flight request. The server sends
  `elicitation/create` while `tools/call` is still open, and waits.
- **Modern**: a **retry**. The call returns an `InputRequiredResult` carrying
  `inputRequests`, the call *ends*, and the client re-sends the original
  request with `inputResponses` attached.

The modern shape is dramatically easier for mcpx, because nothing stays open:
an invocation can return and a person can answer an hour later. The legacy
shape is what every server that exists today actually speaks.

So the broker below is designed around the modern semantics -- a question is
state with a deadline -- and the legacy path is implemented by holding the
call open and feeding it from the same broker. **Both are needed.** One shape
of storage, two shapes of wire.

---

## 2. What is in the way today

### `recvLoop` silently drops server-initiated requests

`internal/mcpclient/client.go`:

```go
var resp rpcResponse
if err := json.Unmarshal(raw, &resp); err != nil { continue }
if resp.ID == nil { continue }       // notification
ch, ok := c.pending[*resp.ID]        // ← a request has an ID too
if ok { ch <- &resp }                // ← no match, frame discarded
```

A server→client request has both an `id` and a `method`. It unmarshals
cleanly, finds no pending entry, and is dropped. The server then waits
forever, the call hits `pool.callTimeout`, and the error says "timed out" —
which is true and useless.

**This was a live bug, not only a missing feature**, and it has been fixed:
`recvLoop` delegates to `dispatch`, which looks for a `method` as well as an
`id` and routes the frame to `handleServerRequest`
(`internal/mcpclient/client.go:404-421`).

### `initialize` does not declare the capability

It did not, when this was written. mcpx advertised `tools`, `resources`,
`prompts`, and a well-behaved server checking for `elicitation` would not send
one -- protection by silence, which is luck rather than design. The client now
declares `elicitation` (with `form`, and `url` when a handler is set), `roots`
and `sampling` (`internal/mcpclient/modern.go:71-84`, wired in at
`client.go:315`).

### Nothing owns "a question awaiting an answer"

Every existing mcpx state is either in-process (a lease) or durable and
append-only (the log). A pending elicitation is neither: it outlives a process
and it is mutated once.

---

## 3. The shape: a broker

One new component. `internal/elicit`.

```go
type Request struct {
    ID        string            // elc-<16 hex>
    Trace     string            // the call that triggered it
    Parent    string            // that call's own parent
    Session   string            // who owns it
    Server    string            // which server is asking
    Tool      string            // during which tool call
    Mode      string            // form | url
    Message   string            // why, in words
    Schema    json.RawMessage   // form mode
    URL       string            // url mode
    Created   time.Time
    ExpiresAt time.Time         // Created + TTL
    State     string            // pending | answered | expired | abandoned
}

type Answer struct {
    ID      string
    Action  string            // accept | decline | cancel
    Content map[string]any    // accept, form mode only
    By      string            // which surface answered
    At      time.Time
}
```

Stored in the existing SQLite index beside `records`, in two tables. It is
already there, already rotated, already queryable, and a pending question is
exactly the kind of thing somebody will later want to ask `mcpx log sql`
about.

The broker is one object with four operations:

Proposed as four operations; what shipped is seven, in
`internal/elicit/elicit.go`:

```go
OpenRequest(r Request) (Request, error)        // register; fills id, mode, TTL
Await(ctx, id) (Answer, error)                 // block until answered or expired
Respond(a Answer) error                        // from any surface
Get(id) (Request, bool, error)                 // the question
Lookup(id) (Answer, bool, error)               // its answer, once it has one
Pending(f Filter) ([]Request, error)           // what is outstanding
Close() error
```

`Await` takes no timeout: the deadline rides on the `Request`, so one
question cannot be awaited with two different lifetimes.

`Await` blocks on a channel when the answerer is in-process, and polls the
table when it is not. Polling is acceptable here because the interval that
matters is human — a 250 ms poll against a question a person answers in twenty
seconds is not a cost anyone can measure.

### Time to live

Every request carries one. The default is `elicit.ttl`, 120 s, in
`internal/defaults/defaults.json` (`defaults.ElicitTTL`). It is **not** a
registry setting and **not** per-server overridable: `OpenRequest`
(`internal/elicit/elicit.go:206`) and the daemon's hooks use the constant
unconditionally. Making it a setting is unfinished work, not a decision.

On expiry the broker answers `cancel` on the requester's behalf, because that
is what expiry *means*: dismissed without an explicit choice. Answering
`decline` would tell the server the user said no, which is a different and
wrong thing.

The TTL is also the backpressure. Without it a stuck server accumulates
questions nobody will ever see, and the table becomes a graveyard.

---

## 4. The policy layer: answering without a human

A CLI invocation frequently has no human attached. Guessing is unacceptable;
failing every time is unusable. So the broker consults a policy first, and
only asks a human if the policy abstains.

**Not built.** None of `elicit.mode`, `elicit.ttl` as a setting, or
`elicit.rules` is in the registry, and `MCPX_ASSUME_YES` does not exist. The
policy that shipped is `elicit.Route` (`internal/elicit/elicit.go:491`), which
chooses an audience rather than matching rules, together with the settings
`elicit.disambiguate`, `elicit.disambiguateDefault`, `elicit.confirmDestructive`
and `elicit.askTimeout`. The block below is the design, kept for the reasoning.

```jsonc
{
  "elicit": {
    "mode": "ask",              // ask | auto | decline | error
    "ttl": "120s",
    "rules": [
      { "match": { "server": "github", "field": "username" },
        "answer": "octocat" },
      { "match": { "mode": "url" }, "action": "decline",
        "because": "no browser in CI" },
      { "match": { "field": "confirm" }, "action": "accept",
        "when": "MCPX_ASSUME_YES=1" }
    ]
  }
}
```

The four modes:

- **`ask`** (default, interactive) — a human is expected
- **`auto`** — rules only; anything unmatched is `cancel`. This is the CI mode.
- **`decline`** — refuse everything, politely. For a sandbox that should never
  be asked.
- **`error`** — fail the call outright, so a script that did not expect a
  question does not silently get a cancelled one

Rules are matched before a human is ever asked, and every match is logged with
the rule that fired. A policy nobody can audit is a policy nobody should
trust.

---

## 5. Every surface

The same broker, six front doors. The rule throughout: **an unanswered
question is a first-class result, not an error.**

### 5.1 CLI — the exchange you described

A call that elicits does not hang. It returns a document and a non-zero exit
code reserved for this case (`75`, `EX_TEMPFAIL`, which is what it is).

```console
$ mcpx call github.create_issue '{"title":"x"}'
{
  "status": "input-required",
  "elicitation": {
    "id": "elc-9f2c1a84bb0e7d31",
    "trace": "cal-d13e4c64102e9113",
    "session": "s87630-989154000",
    "server": "github",
    "mode": "form",
    "message": "Which repository should this issue go in?",
    "requestedSchema": {
      "type": "object",
      "properties": { "repo": { "type": "string", "description": "owner/name" } },
      "required": ["repo"]
    },
    "expiresAt": "2026-09-28T18:02:00Z",
    "ttlSeconds": 120,
    "respondWith": "mcpx elicit answer elc-9f2c1a84bb0e7d31 '{\"repo\":\"me/thing\"}'"
  }
}
$ echo $?
75
```

What shipped (#286) carries the same information in a different shape:

```console
$ mcpx call ask.need_repo; echo $?
mcpx: ask.need_repo is waiting for input and nothing here can answer it.

  asks     Which repository should this go in?
  schema   {"properties":{"repo":{"description":"owner/name","type":"string"}},"required":["repo"],"type":"object"}
  expires  2026-09-30T22:31:37-05:00 (2m0s from now)
  answer   mcpx elicit answer elc-93e18b28a5a3c1f5 '{"repo":"..."}'
           mcpx elicit decline elc-93e18b28a5a3c1f5 | mcpx elicit cancel elc-93e18b28a5a3c1f5

The call keeps running until it is answered or expires; collect its result with: mcpx task result tsk-06654ccc9f064a5c
75
$ mcpx elicit answer elc-93e18b28a5a3c1f5 '{"repo":"me/thing"}'
$ mcpx task result tsk-06654ccc9f064a5c      # the call's real result
```

`--json` puts `{"inputRequired": {callId, server, tool, questions[], text}}` on
stdout. A script under `mcpx exec` or `mcpx run` stops at the call that asked
and exits `75` with the same message, even if it catches `ToolError`: the
generated client sees the daemon's `202` and exits rather than throwing.

Two limits. A daemon running inside the command (`daemon.inline`) stops with
it, so the call cannot outlive the exit and the message says so. And when other
calls share the server's instance a question cannot be tied to one call for
certain; it is still reported, marked `ambiguous`, because waiting out the TTL
and exiting 0 was the bug.

`respondWith` is there because the alternative is the caller assembling it
from three fields, and they will get it wrong once and then copy it forever.

```console
$ mcpx elicit answer elc-9f2c1a84bb0e7d31 '{"repo":"me/thing"}'
$ mcpx elicit decline elc-9f2c...      # explicitly no
$ mcpx elicit cancel  elc-9f2c...      # dismissed
$ mcpx elicit list                     # what is outstanding, with time left
$ mcpx elicit watch                    # follow, for a terminal left open
```

Under the retry model, answering re-drives the original call automatically and
the result lands in the log; `mcpx elicit result <id>` fetches it. Under the
bidirectional model the original invocation is still blocked and simply
proceeds.

A flag for the common case, where the caller is a person who will wait.
**Not built** -- `mcpx call` takes only `-session` and `-raw`, and
`--elicit=prompt` is rejected as an undefined flag:

```console
$ mcpx call --elicit=prompt github.create_issue '{...}'   # NOT BUILT
Which repository should this issue go in?
  repo (owner/name): me/thing
```

`--elicit` takes `prompt | return | auto | decline | error`, defaulting to
`prompt` on a terminal and `return` otherwise. That default is the whole
ergonomic story: interactive use just works, scripted use gets a document.

### 5.2 Scripts

A script is the one place where blocking is natural, because a script already
awaits.

```ts
const issue = await tools.github.create_issue({ title: "x" });
```

By default the runtime answers from policy or cancels, and the call returns
whatever the server does with that. To participate -- **not built**: no
`onElicit` and no `ask` are emitted into the script's globals
(`internal/codegen/emit.go`):

```ts
onElicit(async (q) => {      // NOT BUILT
  if (q.mode === "url") return { action: "decline" };
  if (q.schema.properties.repo) return { action: "accept", content: { repo: "me/thing" } };
  return { action: "cancel" };
});
```

And for a script run on a terminal, `ask(q)` would prompt. The handler is
registered rather than passed per call, because the question arrives from
inside a call the script did not know would ask.

### 5.3 REST and OpenAPI

HTTP has a status code for exactly this, and it is not 400.

```
POST /v1/call/github/create_issue
→ 202 Accepted          # built only with X-Mcpx-Input: report, as {"inputRequired": {...}}; no Location/Retry-After
  Location: /v1/elicit/elc-9f2c1a84bb0e7d31
  Retry-After: 120

  { "status": "input-required", "elicitation": { ... } }
```

```
POST /v1/elicit/elc-9f2c.../accept   {"content":{...}}   # also /decline, /cancel
GET  /v1/elicit                       list
GET  /v1/elicit/elc-9f2c...           one, with time remaining
```

`202` is right: the request was understood and accepted, and completion is
pending. A `400` would say the caller made a mistake, which they did not.

The OpenAPI document gains a `202` response on every tool path and an
`Elicitation` schema component. Both are generated, so they cannot drift.
**Neither is built**: no operation declares a `202`, and there is no
`Elicitation` component in `internal/api/openapi.go`. What shipped instead is
the MCP-side `input_required` result (`internal/mcpserver/ask.go`).

### 5.4 mcpx as an MCP server — the pass-through

This is the interesting one, and the reason the whole design is worth
building. A question from a server three layers down has to reach the agent
at the top.

```
agent ──▶ mcpx (MCP server) ──▶ mcpx daemon ──▶ github server
                                                      │
agent ◀── elicitation/create ◀── broker ◀────── "which repo?"
```

mcpx declares `elicitation` in its own `initialize` capabilities **only if its
client did**. Capability negotiation has to be honest in both directions, or
mcpx promises something it cannot deliver and the failure surfaces at the
worst moment.

Then:

- **Client supports elicitation** → forward it, rewriting the message to name
  the real originator: *"github (via mcpx) asks: which repository?"*. The
  agent must know which server is asking; that is a spec requirement and also
  just decent.
- **Client does not** → fall back to policy, then to returning
  `input-required` in the tool result text so the agent can call
  `mcpx_elicit_answer` itself.

That fallback means **an agent can answer a question with no client support at
all**, which matters because most MCP hosts do not implement elicitation yet.

Two new tools on the MCP surface:

- `mcpx_elicit_list` — what is waiting, with schemas and deadlines
- `mcpx_elicit_answer` — answer one

### 5.4a Who is the question for: the agent or the person?

The spec does not have a field for this, and deliberately so. From the Python
SDK's own words: *"If the client is an agent, it might decide how to handle
the elicitation -- either by asking the user or automatically generating a
response."* **The decision is the client's, not the server's.**

So mcpx needs a routing policy, and the useful thing is that the elicitation
itself carries enough to route on:

| signal | route to | why |
| --- | --- | --- |
| `mode: "url"` | **human** | the point is a browser and a person's consent |
| `format: "password"`, or a name matching `token\|secret\|key\|password` | **human** | a model should not be handed a credential, and cannot know one |
| a `confirm`/`approve` boolean on a destructive call | **human** | consent is the whole content of the question |
| an enum the agent can evaluate from context | **agent** | "which of these three repos" is answerable from what it just read |
| free text with no constraint | **agent first, human on decline** | the agent may know; if it does not it should decline, not invent |

The last row is the one that matters. A model asked "which repository?" with
no way to know will produce a plausible name. **The default for anything the
agent cannot verify must be decline, not guess**, and the skill in §5.7 exists
mostly to say so.

A server may *hint* through `_meta` -- `io.mcpx/audience: "user"` -- and mcpx
should honour a hint toward the human and ignore one toward the agent, because
a server pushing a question past a person is exactly the direction that needs
resisting.

In the plugin this becomes concrete: `notify` puts it to the agent, `ask` puts
it to the person, and the routing policy decides which when the mode is
`auto`. A question routed to the agent that the agent declines is *re-raised*
to the person rather than failing -- that escalation is the behaviour somebody
would expect and would otherwise have to build by hand.

### 5.5 The opencode plugin

The plugin is where this becomes genuinely good, because opencode has a real
user and a real UI.

```ts
"tool.execute.after": async (input, output) => {
  const pending = await pendingFor(input.sessionID)
  if (!pending.length) return
  output.output += "\n\n" + renderElicitation(pending[0])
}
```

Three levels, opt-in as everything else in that plugin is. **Not built**:
`MCPX_PLUGIN_ELICIT` is not a variable the registry derives or the plugin
reads, and there is no `--plugin-elicit`.

1. **`MCPX_PLUGIN_ELICIT=notify`** — a pending question is appended to the
   tool result, so the agent sees it and can answer via a tool. No UI needed.
2. **`MCPX_PLUGIN_ELICIT=ask`** — the plugin uses opencode's own permission
   mechanism (`ask({ permission, metadata })`) to put the question to the
   *user*. This is the real prize: a genuine dialog, in the harness the person
   is already looking at.
3. **`MCPX_PLUGIN_ELICIT=auto`** — policy only.

The session id the plugin already injects is what makes this work. A question
raised by a call from session X is offered to session X and to nobody else. No
new plumbing; the existing injection is exactly the binding the spec requires
between an elicitation and a user identity.

### 5.6 TUI

**Not built** -- nothing in `internal/tui/` mentions elicitation.

A seventh view, and a badge in the header when something is pending —
because the one thing worse than a question nobody answers is a question
nobody knows was asked.

Form mode renders the schema as fields. URL mode shows the URL and offers to
open it. `a` accepts, `d` declines, `c` cancels, `enter` opens the detail.

The header badge is deliberately loud: `● 1 question` in the accent colour.

### 5.7 Skills

**Not built** -- there is no `mcpx-elicitation` skill in `plugin/opencode/skills/`.

`mcpx-elicitation`, loaded on demand, would cover: what `input-required` means,
that `decline` and `cancel` are different, how to answer from a tool, and the
one thing an agent gets wrong — **inventing a value rather than declining**.
A model asked "which repository?" with no way to know will happily guess.
The skill's job is to say: if you do not know, `decline` and explain why.

---

## 6. What it unlocks

Elicitation is not only a feature. It is the missing half of four things mcpx
already has.

### Third-party authorisation flows

I overstated this in the first draft. Two different things share the word
OAuth:

1. **MCP's own authorization spec** -- how a *client authenticates to a remote
   MCP server* over HTTP. That is a separate document, separate machinery
   (OAuth 2.1, resource metadata, token handling), and elicitation does not
   provide it. It remains a genuine gap.
2. **A server sending its user through someone else's authorisation flow** --
   "connect your GitHub account". *That* is url-mode elicitation, and it does
   fall out of this design.

The second is common and currently impossible. The first is unaffected. The
line in `OPENCODE-V2.md` should be split accordingly rather than crossed off.

### Write operations in OpenAPI and CLI adapters

Both currently default to read-only, because a specification describes what a
service *can* do rather than what you meant to allow. With elicitation there is
a middle setting: expose writes, and elicit a confirmation before each one.

```jsonc
{ "methods": ["all"], "confirm": ["POST", "DELETE"] }
```

That is strictly better than the two options available now, which are "no
writes" and "all writes, no questions asked".

### Credentials at the moment of need

`mcpx registry add` currently reports which variables a server requires and
leaves them empty, because inventing a secret is wrong. With elicitation the
server can ask for it on first use, and the answer can go to a keychain rather
than a config file. The current behaviour is the best available without
elicitation; this is better.

### Disambiguation instead of guessing

"Three repositories match, which did you mean?" is the single most common
thing a tool cannot currently do. Today it either guesses or fails. Neither is
what a person would do.

### And two things it unlocks that are not about servers at all

**mcpx can elicit on its own behalf.** `mcpx doctor` finding an unconfigured
server could ask rather than report. `mcpx registry add` could ask for the
secrets it currently lists. The broker does not care whether the asker is an
upstream server or mcpx itself, and building it only for pass-through would
leave that on the table.

**Long-running approvals become possible.** With a TTL measured in minutes and
a durable store, "this deploy needs a second pair of eyes" is expressible: a
question raised by CI, answered by a person from a terminal or the TUI, and
the call resumes. That is a different product feature that falls out of the
same machinery.

---

## 6a. How the answer travels: four mechanisms, not one

The re-exec shape in §5.1 is right for one case and wrong as the only option.
There are four ways an answer can reach the broker, and each is best somewhere.

| mechanism | latency | survives exit | best for |
| --- | --- | --- | --- |
| **stdin/stdout prompt** | immediate | no | a person at a terminal |
| **re-exec** (`mcpx elicit answer`) | seconds to hours | yes | scripts, CI, anything that returned |
| **daemon socket subscription** | immediate | while connected | the TUI, the plugin, any long-lived client |
| **HTTP** (`POST /v1/elicit/<id>/<action>`) | immediate | yes | remote callers, webhooks, another machine |

The daemon already owns a unix socket and a loopback port. A long-lived
client -- the TUI, the plugin, a `mcpx elicit watch` left running -- should
**subscribe** rather than poll:

```
GET /v1/events                    → server-sent events, carrying elicit.* kinds
                                    (GET /v1/elicit/subscribe was proposed and not built;
                                     mcpx elicit watch polls, internal/cli/elicit.go:164)
```

That removes the polling in §3 for every case that matters, and polling
survives only as the fallback for a process that cannot hold a connection.

The defaults follow from the table:

- terminal → prompt, no re-exec, no state to reconcile
- non-terminal → return the document, expect `mcpx elicit answer`
- TUI, plugin → subscribe
- REST caller → the HTTP endpoints, which they are already speaking

Re-exec is the *conventional* shape because it is the only one that works when
the asking process is gone, and that is the case a CLI has to handle. It is
not the best shape when the process is still there.

## 6b. Continuity across the re-exec

A re-exec means a new process, and the question is whether anything is lost.

Nothing needs to be, because session identity already lives in the
environment rather than the process. `MCPX_SESSION_ID` is injected by the
harness; a second invocation in the same shell carries the same value and
therefore leases the same pooled server.

What the audit trail needs, and what the design should record:

```
elicit.open      trace=cal-d13e… elicit=elc-9f2c… session=s876… pid=41201
elicit.answered  elicit=elc-9f2c… action=accept by=cli
                 session=s876… pid=41533 answeredAfterMs=18420
elicit.resumed   trace=cal-a77b… parent=cal-d13e… elicit=elc-9f2c…
```

Three things fall out of that. The **pid changes and is recorded on both
sides**, so "who answered this" is answerable. The **session is the same**, so
`mcpx log --chain` walks from the resumed call back through the elicitation to
the original call. And `answeredAfterMs` is the metric worth having: it is the
human latency, and it is the number that tells you whether a TTL is set
sensibly.

If the session id is *absent* -- a bare shell, no harness -- the broker binds
the question to the config fingerprint and the working directory instead, and
says so in the record. Refusing to work without a session would make the
feature unusable from a plain terminal, which is where people debug.

## 7. Cost, and what could go wrong

Roughly 1,200 lines, in five pieces. The broker and its two tables. The
`recvLoop` fix plus a bidirectional path. Policy evaluation. Six surface
adaptations, most of them under fifty lines each. The TUI view.

Three things I would expect to go wrong:

**Deadlock under the bidirectional model.** A call holds a lease while waiting
for an answer; the answer arrives through a surface that needs the same pool.
Mitigated by the TTL, but the retry model avoids it entirely — which is the
strongest argument for preferring it.

**Questions nobody sees.** A CLI invocation that returns `input-required` into
a script that ignores exit codes produces a question that expires unanswered,
and the user sees only a mysterious cancellation. The TUI badge and
`mcpx elicit list` are the mitigation; so would be a `--elicit=error`, which
would make not expecting a question a loud failure rather than a quiet one.
Neither the badge nor the flag is built.

**Rule-based auto-answers drifting into dishonesty.** A rule that answers
"yes" to everything is a policy that lies to a server about a user's consent.
Logging every rule that fires is the minimum; I would also make `auto` mode
refuse to match a rule whose `match` is empty, so there is no "match
everything" shorthand.

---

## 8. The explainer

Everything mcpx does today assumes the conversation goes one way. You ask a
server to do something; it does it; you read the answer. Every part of
mcpx — the CLI, the scripts, the REST endpoints, the MCP server, the TUI — is
shaped by that assumption.

Elicitation breaks it. It lets a server stop halfway through a job and ask a
question: *which repository did you mean? are you sure you want to delete
this? I need you to log in — here is the link.* The server cannot continue
until somebody answers.

That is awkward for mcpx specifically, because mcpx is usually invoked by
something that cannot answer. A shell script has no opinion about which
repository. A cron job cannot log in. A model can answer, but it will happily
invent a repository name it has no way to know, which is worse than not
answering at all.

So the design turns a blocking question into a **piece of state that anything
can answer, later.** The question gets an identity, a deadline and a home in
the existing database. The tool call stops and says "I need input, here is
what and here is the ticket". Anyone holding that ticket — a person at a
terminal, a script that knew this might happen, an agent through a tool, a
dialog in the editor — can supply the answer, and the call resumes.

Three consequences are worth stating plainly.

**The answer does not have to come from whoever asked.** CI can raise a
question a human answers from a laptop twenty minutes later. That is not a
special feature; it falls out of the question being state rather than a
blocked function call.

**"No" and "not now" stay different.** The spec separates *decline* from
*cancel*, and it matters: a server told "no" should offer an alternative, and
a server told "not now" should ask again later. Collapsing them — which is the
tempting simplification — makes tools worse in a way nobody traces back to
this decision.

**Not answering is a legitimate outcome.** A deadline expires, the question
becomes a cancellation, the server is told honestly that nobody chose. The
failure mode to avoid is a system that waits forever, or one that invents an
answer to avoid waiting.

The reason to build it is not the feature itself. It is that four things mcpx
already does are currently half-finished for want of a way to ask a question.
OAuth is elicitation. Safe write access is elicitation plus a confirmation.
Credentials-on-first-use is elicitation. Disambiguation is elicitation. Each
of those is presently either missing or solved by refusing to do the risky
thing, and they all become available from one piece of machinery.

The catch worth knowing when this was written -- **since fixed**: mcpx's
receive loop treated every inbound frame as a reply to something it sent. A
server that asks a question gets silence, waits, and eventually times out —
and the error mcpx reports is "the call timed out", which is true, useless,
and points at entirely the wrong thing. That is worth fixing whether or not
the rest of this is ever built.
