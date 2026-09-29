# Elicitation: a design

```
created:      2026-09-28T18:00:00-05:00
last-updated: 2026-09-28T18:00:00-05:00
increment:    1
status:       proposed
tags:         area:protocol, area:integration
description:  how a server asks a question, and how the answer gets back
              through a CLI, a REST call, a script, an MCP host or a plugin.
```

Not implemented. This is the shape it would take, what it costs, and what it
unlocks.

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

### The spec changed, in our favour

Revision `2025-06-18` makes this a genuine mid-flight bidirectional request:
the server sends `elicitation/create` while `tools/call` is still open.

Revision `2026-07-28` and the current draft replace it with a **retry**: the
tool call returns an `InputRequiredResult` carrying `inputRequests`, the call
*ends*, and the client re-sends the original request with `inputResponses`
attached.

That second shape is dramatically easier for mcpx, because nothing has to stay
open. A CLI invocation can return, a person can answer an hour later, and a
new invocation carries the answer. **The design below targets the retry model
as primary and treats the bidirectional model as a compatibility path**, which
inverts the usual preference for the newer spec — here the newer one is also
the simpler one.

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

**This is a live bug, not only a missing feature.** Any server that elicits
today fails opaquely against mcpx. That is worth fixing even if none of the
rest is built.

### `initialize` does not declare the capability

mcpx advertises `tools`, `resources`, `prompts`. A well-behaved server checks
for `elicitation` and will not send one without it — so most servers are
currently protected from the bug above by our own silence. That is luck, not
design, and it ends the moment we advertise anything.

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

```go
Open(ctx, Request) (string, error)     // register, return the id
Await(ctx, id, timeout) (*Answer, error)  // block until answered or expired
Answer(id string, a Answer) error      // from any surface
Pending(filter) ([]Request, error)     // what is outstanding
```

`Await` blocks on a channel when the answerer is in-process, and polls the
table when it is not. Polling is acceptable here because the interval that
matters is human — a 250 ms poll against a question a person answers in twenty
seconds is not a cost anyone can measure.

### Time to live

Every request carries one. Default `plumbing.elicitTTL = 120s`, per-server
overridable.

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

A flag for the common case, where the caller is a person who will wait:

```console
$ mcpx call --elicit=prompt github.create_issue '{...}'
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
whatever the server does with that. To participate:

```ts
onElicit(async (q) => {
  if (q.mode === "url") return { action: "decline" };
  if (q.schema.properties.repo) return { action: "accept", content: { repo: "me/thing" } };
  return { action: "cancel" };
});
```

And for a script run on a terminal, `ask(q)` prompts. The handler is
registered rather than passed per call, because the question arrives from
inside a call the script did not know would ask.

### 5.3 REST and OpenAPI

HTTP has a status code for exactly this, and it is not 400.

```
POST /v1/call/github/create_issue
→ 202 Accepted
  Location: /v1/elicit/elc-9f2c1a84bb0e7d31
  Retry-After: 120

  { "status": "input-required", "elicitation": { ... } }
```

```
POST /v1/elicit/elc-9f2c.../answer   {"action":"accept","content":{...}}
GET  /v1/elicit                       list
GET  /v1/elicit/elc-9f2c...           one, with time remaining
```

`202` is right: the request was understood and accepted, and completion is
pending. A `400` would say the caller made a mistake, which they did not.

The OpenAPI document gains a `202` response on every tool path and an
`Elicitation` schema component. Both are generated, so they cannot drift.

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

- `mcpx_elicit_pending` — what is waiting, with schemas and deadlines
- `mcpx_elicit_answer` — answer one

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

Three levels, opt-in as everything else in that plugin is:

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

A seventh view, and a badge in the header when something is pending —
because the one thing worse than a question nobody answers is a question
nobody knows was asked.

Form mode renders the schema as fields. URL mode shows the URL and offers to
open it. `a` accepts, `d` declines, `c` cancels, `enter` opens the detail.

The header badge is deliberately loud: `● 1 question` in the accent colour.

### 5.7 Skills

`mcpx-elicitation`, loaded on demand, covering: what `input-required` means,
that `decline` and `cancel` are different, how to answer from a tool, and the
one thing an agent gets wrong — **inventing a value rather than declining**.
A model asked "which repository?" with no way to know will happily guess.
The skill's job is to say: if you do not know, `decline` and explain why.

---

## 6. What it unlocks

Elicitation is not only a feature. It is the missing half of four things mcpx
already has.

### OAuth, which is a named gap

`url` mode **is** how MCP does OAuth. A server that needs authorisation elicits
a URL, the user visits it, the server notices out of band. The entire OAuth
story that `OPENCODE-V2.md` lists as missing is a consequence of this design
rather than separate work. Nothing else is needed.

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
`mcpx elicit list` are the mitigation; so is `--elicit=error`, which makes not
expecting a question a loud failure rather than a quiet one.

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

The catch worth knowing up front: **there is a real bug here today.** mcpx's
receive loop treats every inbound frame as a reply to something it sent. A
server that asks a question gets silence, waits, and eventually times out —
and the error mcpx reports is "the call timed out", which is true, useless,
and points at entirely the wrong thing. That is worth fixing whether or not
the rest of this is ever built.
