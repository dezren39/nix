# 0003 — What mcpx declares, and what it enforces

```
number:   0003
status:   accepted
date:     2026-09-30
issues:   #82 #177 · #181 row 10 · touches #80 #81 #111 #129 #172 and every issue whose footer says "Security/auth deferred; see tracker"
code:     05c78b2
```

> Every issue in the backlog defers security to the tracker, and #82 proposes
> declaring capabilities now and enforcing them later. Nobody owned the line
> between the two. Looking for it turned up a daemon whose documented access
> control is not its access control, a sandbox that holds on one runtime of
> three, and a credentials block that does nothing at all.

---

## Context

### What the issues say

- #82, approach C: "Capabilities become declarable. The plugin manifest says
  what it intends to use … and the daemon can report and later enforce that.
  Declaring is useful even before enforcing, because it is documentation the
  daemon can print." And: "capability *enforcement* is security work and is out
  of scope here".
- #181 row 10: "Nobody owns the declared-vs-enforced capability boundary …
  The deferral has nowhere to land."
- #177: a declaration that does not do what it says is a bug, and "it is
  indistinguishable from success".
- `docs/protocol.md:18-32`: "Accept liberally, send conservatively … a
  notification or a server-initiated request that the peer did not negotiate
  *and* did not declare is a lie about what was agreed". Capabilities that
  depend on a push are "declared only when a notifier is behind them"
  (`:108-110`).
- `internal/api/ops.go:59-67`: `Admin` is "carried here so a later OAuth pass
  has one place to read scopes from, and so the MCP tool can say so in its
  description". `Mutating` and `Destructive` feed the MCP annotations.

### What exists, and what each declaration actually does

Everything below was checked against a binary built from `05c78b2`, in an
isolated state directory, and the daemon stopped afterwards.

**1. The daemon's access control is not what it is documented to be.** The
OpenAPI document says: "Every endpoint is unauthenticated. The socket's file
permissions are the access control" (`internal/api/openapi.go:38-39`);
`daemon.endpoint` says the local socket has "filesystem permissions as the
access control" (`internal/settings/registry.go:354-355`). The socket is
indeed `0600` (`internal/daemon/server.go:202`). But every daemon also listens
on loopback TCP, unconditionally, on an ephemeral port
(`server.go:209-213`, `daemon.port` default `0`), and that listener serves the
same unauthenticated API with no check of any kind — no `Origin`, no content
type, no token. A request shaped exactly like a browser's no-preflight "simple
request" runs code:

```
curl -X POST http://127.0.0.1:$PORT/v1/exec \
  -H 'Content-Type: text/plain;charset=UTF-8' -H 'Origin: https://evil.example' \
  -d '{"source":"console.log(\"ran from a foreign origin\")","options":{"output":"structured"}}'
→ "stdout":"ran from a foreign origin\n"
```

and `/mcp` answers `initialize` for the same foreign `Origin`. The MCP
Streamable HTTP transport (2025-11-25, §"Security Warning") says servers
"**MUST** validate the `Origin` header on all incoming connections to prevent
DNS rebinding attacks. If the `Origin` header is present and invalid, servers
**MUST** respond with HTTP 403 Forbidden." The OpenAPI text does mention the
TCP listener, but only to say that binding it *wider* than loopback is a
deliberate act — it treats loopback as private, and loopback is not private:
every other user on a shared host can reach it, and so can a web page, subject
only to whatever the browser does about pages reaching the local network,
which varies by browser and version. The port is ephemeral, not secret; the
range is 16,384 ports on macOS and about 28,000 on Linux, and a caller that
fires the same request at all of them does not need to know which one
answered. Scripts run with `--allow-all` by default
(`internal/runner/runner.go:52-53`). The declared boundary is the socket's
mode bits; the enforced boundary is "can send an HTTP request to localhost".

**2. `script.permissions` holds on Deno and nowhere else.** The setting offers
`all`, `net`, `read`, `read-net` and `strict` (`registry.go:183-189`), and its
help does not mention runtimes. The profiles become Deno flags
(`runner.go:41-67`), which are appended for Deno and never for Bun or Node
(`runner.go:85-101`). The code's own comment says "bun and node run with the
user's own authority whatever is asked, which is stated here rather than
pretended otherwise" (`runner.go:43-45`) — stated in a Go comment, to nobody
who sets the value. With a script that reads a file outside its working
directory: `--runtime deno --permissions strict` fails with `NotCapable`;
`--runtime node` and `--runtime bun` print the file and exit 0. Under
`script.runtime: auto`, whether `strict` holds depends on whether Deno happens
to be installed, which on a Nix install it always is (#181 row 11), so nobody
testing through the flake sees it.

**3. `servers.<name>.auth` does nothing.** `config.Server.Auth`
(`internal/config/config.go:141`) is the only non-test reference to package
`mcpauth` in the tree; nothing calls `Auth.Resolve` (`mcpauth.go:93`) or
`Auth.Describe` (`mcpauth.go:190`). A bearer token in `auth` is parsed and
never sent. `docs/story.md:2013-2027` documents it as working, including "an
unset variable is **named up front**" and "a server requiring [OAuth] says so
before the first request"; `Resolved.NeedsOAuth` (`mcpauth.go:84-86`) is read
by nothing. #115 lists it as an existing capability. `docs/protocol.md:378`
("OAuth for remote servers. Declared and not performed") is honest about
OAuth, and says nothing about the rest of the block.

**4. One MCP annotation is wrong, and one label reads like a permission.**
The annotations are hints by the specification's own definition — "all
properties in `ToolAnnotations` are **hints** … Clients should never make tool
use decisions based on `ToolAnnotations` received from untrusted servers"
(2026-07-28 schema, `ToolAnnotations`). But `idempotentHint` is computed as
`!Mutating || Destructive` (`internal/api/ops.go:664`), which calls `restart`
idempotent; a second restart kills in-flight calls a second time. And an admin
operation's tool description begins "Privileged:" (`ops.go:675-677`), an
mcpx-invented label, while nothing checks who calls it. The OpenAPI document
states that plainly; the tool description, which is what a model reads, did
not. The design behind it is sound and written down —
"withholding the tool buys no safety" (`docs/parity.md:65-69`,
`internal/mcpserver/ops.go:25-29`) — only the label was ambiguous.

**5. Routing is not permission.** A question whose single boolean field is
named `confirm` is routed to a human, because "consent is the one thing an
agent cannot give on somebody's behalf" (`internal/elicit/elicit.go:508-512`).
`Broker.Respond` (`elicit.go:292`) accepts an answer from anyone, and `By` is
self-reported. That is correct for what it is — mcpx cannot know who is
answering — and it is a hint, not a guarantee.

**6. What is enforced, and says so.** The daemon honours only call-scoped
settings from a caller's `X-Mcpx-Settings` header (`routes_settings.go:64-72`).
A non-hot setting written at runtime is not applied and the answer says
`restartRequired` (`routes_settings.go:216-227`). The socket and its directory
are `0600`/`0700` (`server.go:202`, `paths.go:144-160`). Capabilities to peers
follow the promise rule in `docs/protocol.md` §2.2 and §4. `ExecOptions`'
`capabilities: ["artifacts"]` is the *caller's* declaration and is trusted by
design, because remoteness cannot be read off a transport
(`internal/execsvc/execsvc.go:65-72`). None of the restrictions in this
paragraph — the header filter, the socket's mode, the directory's mode — has a
test that tries to get round it.

The pattern: promises get delivered and guarded, because the protocol and
settings work built guards for them. Restrictions are enforced where
enforcement was cheap and silently absent where it was not. Hints are a mix of
true, false, and phrased like rules.

---

## Options

### A — declare nothing until it is enforced

Strip the "Privileged" label, the audience reasons and #82's manifest
capabilities until something enforces them. It throws away real value: the
MCP annotations are hints *by specification*, a model is better for reading
them, and #82 is right that a declaration the daemon can print is
documentation. It also would not have caught findings 1–3, which are not
early declarations but broken ones.

### B — declare freely; each feature enforces what it can

The status quo, and it produced every finding above.

### C — three kinds of declaration with one rule each, one owner, one table, two guard tests

Distinguish promises, restrictions and hints; give restrictions exactly two
states; put every restriction in one table owned by the surfaces that render
it; guard both states.

---

## Decision

**C.**

### Three kinds of declaration

| kind | says | rule |
| --- | --- | --- |
| **promise** | mcpx will do X: a capability to a peer, a setting's effect, a config field's effect, a documented behaviour | delivered. There is no advisory promise; an undelivered one is a bug (#177), not a state. |
| **restriction** | X will be refused: a sandbox profile, a scope filter, a ceiling, a plugin's capability list, an auth scope, an access control | **enforced** — mcpx, or something it drives, refuses X, and a test tries X and sees the refusal — or **advisory** — nothing refuses X, and every surface that shows the restriction says `advisory` in the same text. No third state. |
| **hint** | X is probably true, act accordingly: MCP annotations, the admin label, an elicitation's audience | true, and recognisable as a hint: a field the specification defines as one (MCP's `*Hint`), or the word `advisory` beside it. A false hint is a bug. |

**"Declarable now, enforceable later" is `advisory`**, and it may be declared
today — on the condition that the word is on every surface that shows it. It
moves to `enforced` only in the change that adds its refusal test. Nothing may
be shown as enforced before that test exists.

### Ownership

**The daemon is the only enforcer.** A plugin, a hook, a manifest and a caller
only declare.

**SURFACES owns the declarations**, in one table in `internal/api`, beside the
ops table: `api.Boundaries()`. The ops table already names itself "the one
place a later OAuth pass reads scopes from" (`ops.go:59-61`), and a restriction
on a setting, an operation or a plugin reaches a caller only through surfaces
SURFACES owns — tool descriptions, OpenAPI, `/v1/settings`, the CLI. Every
restriction any issue wants to declare is a row: #82's manifest capabilities,
0002's `autonomy.max`, 0001's fail-closed hooks, a future OAuth scope. The
footer "Security/auth deferred; see tracker" on every issue now has somewhere
to land: a deferred restriction is an `advisory` row naming the issue that will
enforce it.

```go
// internal/api/boundaries.go

type BoundaryState string

const (
	Enforced BoundaryState = "enforced"
	Advisory BoundaryState = "advisory"
)

// Boundary is one restriction mcpx declares.
type Boundary struct {
	Name     string // "daemon.access", "script.permissions", "op.admin"
	Declares string // the restriction, in one sentence
	State    BoundaryState
	// Refusal names the case in TestEveryEnforcedBoundaryRefuses that tries
	// to get round it. Required when enforced; a claim with no probe is the
	// thing this table exists to stop.
	Refusal string
	// Until names the issue that will enforce it. Required when advisory.
	Until string
}

func Boundaries() []Boundary
```

### The guard tests

Following #129's enumerate → completeness → probe, and #177's second warning
that acceptance is not effect:

1. **`TestEveryEnforcedBoundaryRefuses`**, end to end, against the real binary:
   one case per enforced row, each doing the forbidden thing and asserting the
   refusal *and* that the same action without the restriction succeeds (#75's
   with-and-without). Its first cases: a daemon-scoped setting in
   `X-Mcpx-Settings` leaves the daemon's value unchanged; a read outside the
   working directory under `script.permissions: strict`, **for every runtime
   found on the machine**; a TCP request without the daemon's credential, and a
   request with a foreign `Origin`, are refused.
2. **`TestEveryBoundaryIsEnforcedOrSaysAdvisory`**, a unit test over
   `api.Boundaries()`: an enforced row must name a case that exists in test 1
   (the completeness half, so the table cannot claim what the probe does not
   try); an advisory row must name an issue, and every surface that renders it
   must contain `advisory`.

The two together mean a boundary cannot be declared enforced without a probe,
and cannot be declared at all without saying which it is.

### The rows it starts with

With the state each has when the table and its tests land together — which
they must, since an enforced row without its case fails test 2.

| row | declares | state | refusal case, or until |
| --- | --- | --- | --- |
| `daemon.access` | only a process that can open the socket or read the daemon record — or was given its token — can use the API | **enforced after the fix below** | TCP without the token; foreign `Origin` |
| `settings.callScope` | a caller's header changes only call-scoped settings | enforced | daemon-scoped key in the header |
| `script.permissions` | a script cannot exceed its profile | **enforced after the fix below** | outside read, per runtime |
| `op.admin` | admin operations would need a separate grant | advisory | the OAuth pass — a new issue under #181 |
| `elicit.audience` | a question routed to a human is answered by one | advisory | #82, once answerers have an identity |
| `plugin.capabilities` | a plugin uses only what its manifest lists | advisory | #82, one capability at a time |
| `autonomy.max` | mcpx does not act above the ceiling on its own | enforced, from the day it lands | 0002's end-to-end case |
| `hooks.failClosed` | a failing fail-closed hook refuses its operation | enforced, from the day it lands | 0001's hook dispatcher |

### What changes now

- **`daemon.access`** is the most urgent thing in this record, and it is a
  security fix rather than a documentation one. Two steps. First, the TCP
  listener refuses, with 403, any request whose `Origin` header is present and
  not allowed: cheap, the specification's MUST, and it closes the browser
  path, since browsers always send `Origin` on a cross-origin POST and the CLI
  and the plugin never do. Second, it requires a credential that only a
  reader of the `0600` daemon record can have: a random token generated at
  start, written into `daemon-<key>.json` beside the endpoint, and sent as
  `Authorization: Bearer` by every client that discovers the endpoint there. A
  client of a daemon on another machine gets the token the way it gets the
  endpoint, from its own configuration — a setting beside `daemon.endpoint`.
  The unix socket stays as it is. Then "file permissions are the access
  control" is true of both listeners. The fix is in
  `internal/daemon/server.go` and the clients that read the record
  (`internal/cli/client.go`, `plugin/opencode/mcpx/daemon.ts`,
  `docs/plugin-discovery.md`).
- **`script.permissions`**: a profile other than `all` on a runtime with no
  permission model is refused before anything runs, naming the runtime and the
  profile. Refusal rather than an advisory label, because a sandbox that
  silently does not apply is the worst version of this bug. The fix is in
  `runner.Detect`/`runner.Permissions`, and #105 inherits the rule for Python.
- **`servers.<name>.auth`**: wired, or the field removed. A config block that
  parses and does nothing is a false promise; until it is fixed, `mcpx config`
  and `mcpx doctor` should say it is ignored. The `docs/story.md` section waits
  for the final docs pass, which owns that file.
- **`idempotentHint`** comes from an explicit per-operation field rather than
  a formula over two unrelated ones; `restart` is not idempotent.
- **The admin label** says `advisory` in the tool description. Done in this
  change: `internal/api/ops.go` and `TestAPrivilegedOperationSaysItIsAdvisory`.

---

## Consequences

**#82**: manifest `capabilities` ship as advisory rows, and every listing of
them says "advisory". Each becomes enforced separately, with its own refusal
case, once the plugin session identifies callers. The plugin session must carry
an identity the daemon can check, or no capability can ever leave `advisory`.

**#181 row 10** closes: the boundary has an owner, a table and two tests.

**#177** gains four instances: the TCP listener, `script.permissions` on Bun and
Node, the inert `auth` block, and `restart`'s `idempotentHint`. The first needs
to be treated as a security issue; there are no users yet, which is the only
reason it is not an emergency.

**#111, #112, #81**: `ClampedBy` and fail-closed hooks are restrictions, so they
land enforced, with their refusal cases, or not at all.

**#129**: the two tests are guards of its kind; `api.Boundaries()` is the
registry they walk.

**#80, #83**: a plugin or tool source that declares anything restrictive adds a
row.

**Peer capabilities** — what mcpx declares in `initialize` or per request — are
promises, and stay under `docs/protocol.md`'s rule and the PROTOCOL area's
guards. "Send conservatively" is the promise rule applied to the wire: do not
send what the peer did not declare, and do not declare what mcpx will not
deliver.

**Forbidden from here**: a restriction shown without its state; an enforced
row without a refusal case; an advisory row without the issue that will
enforce it; any surface describing an access control that a test has not tried
to get round.

**Deferred**: authenticating callers — who is calling, not just whether they
can reach the socket — to the OAuth pass the ops table already anticipates.
Until then every row about *who* may do something is advisory, and says so.

---

## Mapping

| term | from | kind | state |
| --- | --- | --- | --- |
| "the socket's file permissions are the access control" | `openapi.go:38`, `registry.go:354` | restriction | not true of the TCP listener → `daemon.access`, enforced after the fix |
| loopback TCP listener | `server.go:209-213` | — | the unguarded half of `daemon.access` |
| `Origin` validation | MCP Streamable HTTP, 2025-11-25 | restriction | missing; a MUST |
| capabilities declared to a client | `docs/protocol.md` §2.2 | promise | delivered |
| `sampling` declared to servers | `docs/protocol.md:271`, #39 | promise | delivered only with a handler |
| "send conservatively" | `docs/protocol.md:18-32` | promise, on the wire | PROTOCOL's guards |
| `readOnlyHint`, `destructiveHint`, `openWorldHint` | `ops.go:659-667` | hint | specification-defined; true |
| `idempotentHint` | `ops.go:664` | hint | false for `restart` → explicit field |
| `Admin`, `x-mcpx-admin` | `ops.go:59-62`, `openapi.go:58` | hint | the OpenAPI text says so |
| "Privileged:" in a tool description | `ops.go:676` | hint | now says `advisory` |
| `Mutating`, `Destructive` | `ops.go:63-67` | hint | through the annotations |
| call-scope filter on `X-Mcpx-Settings` | `routes_settings.go:64-72` | restriction | enforced; gains its refusal case |
| `Hot` | `schema.go:183-188`, `routes_settings.go:216-227` | promise | delivered: not applied, and the answer says so. The field's comment says the API "refuses it" (`schema.go:187`); it answers 200 with `applied: false` |
| socket mode | `server.go:202`, `paths.go:156-160` | restriction | enforced by the OS |
| `script.permissions` | `registry.go:183`, `runner.go:41-101` | restriction | Deno only → refuse elsewhere |
| elicitation audience | `elicit.go:74-84`, `:491-514` | hint | advisory until #82 |
| `servers.<name>.auth` | `config.go:141`, `docs/story.md:2013` | promise | undelivered → wire or remove |
| `NeedsOAuth`, "says so before the first request" | `mcpauth.go:59-64`, `:84-86` | promise | undelivered |
| OAuth "declared and not performed" | `docs/protocol.md:378` | a documented gap | honest; a gap written down is not a promise |
| `ExecOptions.capabilities` | `execsvc.go:65-72` | the caller's promise | trusted by design |
| manifest `capabilities` | #82, approach C | restriction | advisory until #82 enforces each |
| "declarable now, enforceable later" | #82 | — | `advisory` |
| `ClampedBy`, `autonomy.max` | [0002](0002-autonomy-dial.md) | restriction | enforced from the day it lands |
| `failure: closed` | [0001](0001-hook-vocabulary.md) | restriction | enforced from the day it lands |
| "Security/auth deferred; see tracker" | every issue's footer | — | an advisory row naming its issue |
