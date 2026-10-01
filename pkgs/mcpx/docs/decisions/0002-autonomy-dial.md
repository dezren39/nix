# 0002 — One autonomy dial, and a ceiling the daemon sets

```
number:   0002
status:   accepted
date:     2026-09-30
issues:   #111 #112 #113 #114 #110 · #181 rows 3 and 5 · touches #81 #172 #176
code:     05c78b2
```

> Three issues invented the same dial, and the code already has a fourth
> spelling of it that any caller can turn up. This record names one dial,
> says what each stop on it permits, and makes the daemon's limit a property
> of the settings registry rather than of whichever feature reaches it first.

---

## Context

### The question underneath

Every one of these proposals is answering the same question: **when mcpx is
about to act on something the caller did not send in the request itself — a
script a model wrote, a recipe it picked by score, a repair it worked out, a
hook's output, an operation it routed a sentence to — how far may it go on its
own?**

That sentence also draws the edge. The script in a `/v1/exec` body is the
caller's: it sent the code. A tool call is the caller's. A recipe run by name is
not — the caller sent a name, and the code is on the daemon's search path. A
generated script is not. The dial governs the second kind and never the first.

### What ships

| knob | where | values | what it actually does |
| --- | --- | --- | --- |
| `prompt.mode` | `internal/settings/consumer.go:97`, call scope | `script` (default), `run` | the default for `/v1/intent`; a body `mode` overrides it unconditionally (`internal/daemon/routes_consumer.go:344-347`) |
| `mcpx prompt --run` | `internal/cli/consumer.go:402`, `:423-426` | flag | sends `mode: run` |
| recipe run `mode` | `routes_consumer.go:268-271`; `mcpx recipes run --script` | `script`, `run` (default) | render and diagnose only, or run |
| `prompt.sample` | `consumer.go:105`, daemon scope | `never` (default), `ask` | whether mcpx may ask a model at all |
| `elicit.confirmDestructive` | `consumer.go:47`, daemon scope | bool, off | ask before a `destructiveHint` call; unanswered refuses |
| `elicit.disambiguate` | `consumer.go:23`, daemon scope | `never`, `ask` | ask which live instance to use |
| `diagnose.preflight` | `consumer.go:65`, call scope | bool, on | refuse a script whose calls no longer match the schemas |
| `script.typecheck` | `internal/settings/registry.go:244` | `off`, `on`, `strict` | refuse a program that does not type-check |
| `elicit.mode` | `docs/elicitation.md` §4, *status: proposed* | `ask`, `auto`, `decline`, `error` | not implemented; who answers an upstream server's question |

Three things were found reading this, the third by running it:

- **There is no ceiling.** `prompt.mode` is a default, not a limit. Any caller
  that can reach the socket sends `"mode": "run"` and the daemon runs a script a
  model wrote (`routes_consumer.go:393`). An operator cannot say "this daemon
  proposes and never runs", which is the thing #111's last paragraph asks for.
- **An unknown mode is silently `script`.** Both routes test `mode != "run"` /
  `mode == "run"` (`routes_consumer.go:315`, `:393`), so #113's `plan` would be
  accepted today and quietly mean something nobody chose.
- **A caller's `prompt.mode` is accepted and ignored, and so is the runtime
  API's.** It is declared call-scoped, which promises that "the CLI sends its
  effective value with the request and the daemon honours it for that request
  only" (`internal/settings/schema.go:87-88`). The daemon instead resolves a
  second, private settings set from its config files and environment once at
  start (`internal/daemon/consumer.go:70-80`, `settings.Resolve`) and reads the
  policy from that (`:128`) — never from the request's `callSettings`, and
  never from its own live set. `mcpx prompt` reads only its own `--mode` and
  `--run` (`internal/cli/consumer.go:423-426`). Against a matching recipe,
  `MCPX_PROMPT_MODE=run mcpx prompt …` and `mcpx prompt --prompt-mode run …`
  both answer `"mode": "script"`; only `--run` runs it. And
  `PUT /v1/settings/prompt.mode {"value": "run"}` answers `"applied": true`,
  `GET` then reports `run` from `runtime:api`, and the next prompt still
  answers `"mode": "script"` — the API reports a change that did not happen.
  Half-read, in #177's terms: read, so `TestEverySettingIsReadSomewhere`
  passes, and honoured only from a config file present when the daemon
  started. The other six call-scoped settings in the same policy
  (`recipes.minScore`, `recipes.matchMargin`, `recipes.limit`,
  `prompt.catalogBudget`, `prompt.maxTokens`, `prompt.runTimeout`) go through
  the same path (`consumer.go:117-135`) and share the fault.

The settings registry has scopes (`internal/settings/schema.go:63-107`), and the
daemon honours only call-scoped values from a caller's `X-Mcpx-Settings` header
(`internal/daemon/routes_settings.go:50-76`). That is the only line between
"the daemon's policy" and "the caller's preference" in the code today, and it
has no way to say "a caller may lower this but not raise it".

### What the issues propose

| source | name | values | default |
| --- | --- | --- | --- |
| #111 | `repair.level` | `off`, `explain`, `confirm`, `propose`, `retry`, `rewrite` | `explain` |
| #112 | hook `effect` | `advise`, `transform`, `script`, `retry` | `advise` |
| #113 | `prompt.mode` | adds `plan` | — |
| #114 | generators | "a generated artefact is a file a human reviews"; never a runtime behaviour | — |
| #110 | `sampling.answerLocally` | a model answering an upstream question itself | off |
| #111, last paragraph | `ClampedBy` | "the daemon sets the maximum autonomy it will ever exercise, a caller may lower it but never raise it" | — |

#176 asked for #111's `repair.level` to be the one dial.

---

## Options

### A — one dial per feature, each with its own words

What happens by default. Four vocabularies for one idea, and a ceiling
implemented three times (#181 row 5).

### B — #111's `repair.level` everywhere, as #176 proposed

It is the earliest and the most worked-out. But the name says *repair* on a
setting that governs prompts and hooks, and `level` already means log level
(`logging.level`). Its top two stops, `retry` and `rewrite`, differ in *what*
runs, not in *how far* mcpx goes — both execute code the caller did not send,
and a ceiling cannot tell them apart. And it has no stop for #112's `transform`,
replacing the value the caller receives without running anything, so #112 would
still need words of its own.

### C — one ordered vocabulary named for the concept, instantiated per feature, with one daemon ceiling enforced by the registry

A single set of levels called `autonomy`; each feature that needs the dial gets
a `<feature>.autonomy` setting; one daemon-scoped `autonomy.max` bounds them
all through a new `ClampedBy` field on a setting.

Costs: renames the shipped `prompt.mode` and its body field; adds a registry
feature.

---

## Decision

**C.**

### The levels

Ordered from least to most. Each permits everything below it.

| level | may | may not |
| --- | --- | --- |
| `off` | nothing — the caller receives exactly what it would have without mcpx's opinion | attach anything |
| `advise` | attach text or diagnostics to the result or the error | stop, replace, or run anything |
| `ask` | stop the operation: raise a question through the broker, where an unanswered one refuses, as `confirmDestructive` already does; or refuse with a structured reason | replace or run anything |
| `propose` | return a replacement beside the original, unapplied: a script, a plan, a transformed value, a declaration | apply it |
| `apply` | replace the value the caller receives, with the original kept visible | execute anything the caller did not send |
| `run` | execute code or an operation the caller did not send, with the caller's own permissions and nothing more | exceed the caller's permissions, or skip a consent gate |

Two rules hold at every level. **No level skips a consent gate**:
`elicit.confirmDestructive` still asks before a destructive call, whoever wrote
the script. **No level makes mcpx exceed what the caller could have done** —
#112's "a hook must never be able to do what the script it wraps could not",
made general.

### The settings

| setting | scope | default | replaces |
| --- | --- | --- | --- |
| `autonomy.max` | daemon, not hot | `run` | nothing; new |
| `prompt.autonomy` | call, `ClampedBy: autonomy.max` | `propose` | `prompt.mode` (`script` → `propose`, `run` → `run`) |
| `repair.autonomy` | call, `ClampedBy: autonomy.max` | `advise` | #111's `repair.level` |
| `hooks.autonomy` | call, `ClampedBy: autonomy.max` | `run` — each hook's own `effect` is the real limit | nothing; new |
| hook `effect` | per hook ([0001](0001-hook-vocabulary.md)) | `advise` | #112's `effect` |

The `mode` field on `/v1/intent` and `/v1/recipes/{name}/run` becomes
`autonomy`, and an unknown value is a 400 rather than a silent `propose`.
An empty field means `prompt.autonomy` on the intent route and `run` on the
recipe route, which keeps that route's own rule — "a route named run runs"
(`routes_consumer.go:265-271`); both are clamped by `autonomy.max`.
`mcpx prompt --run` and `mcpx recipes run --script` stay as readable spellings,
the way `--remote` and `--local` spell `exec.where`.

`autonomy.max` defaults to `run` because that is today's effective ceiling: no
behaviour changes until an operator lowers it. A recipe run by name is governed
— the caller sent a name, not the code — so `autonomy.max: propose` makes the
recipe route render instead of run. `/v1/exec` is not governed, and that is the
point rather than a gap: a caller clamped to `propose` who takes the proposed
script and sends it to `/v1/exec` has run it deliberately, with the code in
hand, which is exactly what `docs/consumer.md`'s trust rule asks for. The
ceiling bounds what mcpx does on its own; it does not claim to stop a caller
running code the caller sends. [0003](0003-declared-vs-enforced-capabilities.md)
holds it to that claim.

### Lowered, never refused

A request above the ceiling is **lowered to it and reported**, not refused. The
answer carries `{autonomy, requested, clampedBy}`, and the CLI prints one line
to stderr. Lowering is always the safe direction, so delivering the lower
level is delivering a safe subset of what was asked; refusing would cost the
caller a round trip to arrive at the same place. It is not silent — the
response says what was asked, what was used, and which setting and which layer
decided, which is the provenance the registry already keeps for every value
(`internal/settings/value.go:47-72`).

### `ClampedBy` — an `internal/settings` capability (#181 row 5)

The API, as it should land:

```go
// internal/settings/schema.go, a new field on Setting:

	// ClampedBy names the setting whose value is the most this one may
	// resolve to. The ceiling is daemon-scoped and not hot, this setting
	// is call-scoped, and both are KindEnum with the same Enum, ordered
	// from least to most permissive: that order is what "most" means.
	//
	// A value above the ceiling is lowered to it, from any layer, and the
	// lowering is recorded on the Value. It is never refused: the lower
	// value is always a safe answer to the request.
	ClampedBy string
```

```go
// internal/settings/clamp.go, new:

// Clamp is what bounding a requested value by its ceiling produced.
type Clamp struct {
	Value     string // what will be used
	Requested string // what was asked for
	By        *Value // the ceiling and its origin, when it lowered the request
}

// Clamp bounds requested by the ClampedBy ceiling of path, as resolved in s.
// A handler that takes the dial from a request body calls this; the call
// settings header gets it through WithOverrides.
func (s *Set) Clamp(path, requested string) (Clamp, error)

// Rank is the position of raw in an ordered setting.
func (d Setting) Rank(raw string) (int, bool)
```

Four hooks into existing code, each small:

1. `New` (`schema.go`) refuses a registry where `ClampedBy` names a missing
   setting, a ceiling that is not daemon-scoped or is hot, a clamped setting
   that is not call-scoped, a different `Kind` or `Enum`, or a clamped default
   above the ceiling's.
2. `Set.raw` (`value.go:262`) returns the clamped value for a setting with a
   ceiling, so no reader anywhere — `consumerPolicyFrom`, a handler, a
   call-scoped view from `WithOverrides` — can observe a value above it.
3. `Value` gains `Requested` and `ClampedBy`, and `daemon.SettingRecord` renders
   them, so `mcpx settings get prompt.autonomy` says it was lowered and by what.
4. The two routes that take the dial in a body call
   `s.callSettings(r).Clamp("prompt.autonomy", body.Autonomy)` and put the
   `Clamp` in the response. An empty body falls back to the request's
   call-scoped value, and the policy is read per request from
   `s.callSettings(r)` — the daemon's live set plus the caller's header —
   instead of from the private set `initConsumer` resolved at start. That
   closes the half-read above for all seven settings at once, and makes a
   runtime `PUT` mean what its `"applied": true` says.

Version one supports `KindEnum` only. A floor — a daemon requiring *more*
checking than a caller asks for, as `diagnose.preflight` might want — is the same
mechanism with the order reversed; nothing needs one yet, and
`elicit.confirmDestructive` gets the same effect today by being daemon-scoped
so a caller cannot touch it at all.

**Landed** with `autonomy.max`, `repair.autonomy` and `hooks.autonomy`
(`internal/settings/clamp.go`). Two deviations from the sketch above: a
clamped setting's `Enum` is an ordered *subset* of the ceiling's rather than
equal to it, so `prompt.autonomy` keeps its two levels; and a prompt route
whose level is lowered below `propose` returns neither the script nor a
generation, only the candidates and diagnostics. `repair.autonomy` governs
the diagnostics a refused call carries (`off` drops them); `hooks.autonomy`
governs launcher phases from a configuration file or the environment, never
those given as flags on the command itself. The `autonomy.max` row in
`internal/api/boundaries.go` names the refusal tests.

**Originally not implemented**, for two reasons. The `Setting` struct lives
in `schema.go`, which another change is editing now. And a `ClampedBy` field
that clamps nothing would be a declared capability with no behaviour — the
#177 bug, and the thing [0003](0003-declared-vs-enforced-capabilities.md) forbids.
It lands with its first consumer: the `prompt.mode` → `prompt.autonomy` rename
and `autonomy.max`, which touch `internal/daemon/routes_consumer.go`,
`internal/daemon/consumer.go`, `internal/defaults` and `docs/configuration.md`.

The tests it lands with:

- `New` refuses each malformed `ClampedBy` above.
- `Clamp` lowers and never raises; the origin of the ceiling is recorded; a
  call-scoped override above the ceiling reads as the ceiling; a file layer
  above the ceiling in the daemon's own config reads as the ceiling.
- End to end, with `autonomy.max: propose`: `mcpx prompt --run` on a prompt
  that matches a recipe whose body writes a file returns the script with
  `clampedBy: autonomy.max`, and the file does not exist. Falsified by removing
  the `Clamp` call: the file appears.
- End to end, with the default ceiling: `MCPX_PROMPT_AUTONOMY=run mcpx prompt …`
  and `mcpx prompt --prompt-autonomy run …` both run the recipe. Falsified by
  reading the start-time policy instead of the request's: both answer
  `propose`, which is today's behaviour for `prompt.mode`.
- `PUT /v1/settings/autonomy.max` answers `applied: false,
  restartRequired: true` (`routes_settings.go:216-227`), because the ceiling
  is not hot.

What it does not defend against, stated so nobody reads more into it: a caller
with socket access can write a higher ceiling into a config file with
`PUT /v1/settings/autonomy.max {"persist": "user"}` and then shut the daemon
down. Every daemon setting has that property until the admin operations are
access-controlled — the advisory `op.admin` row in
[0003](0003-declared-vs-enforced-capabilities.md).

---

## Consequences

**#111**: `repair.level` becomes `repair.autonomy`, and each rung has a level
(the mapping below). `ClampedBy` belongs to `internal/settings`, not to repair;
#111 consumes it. "Whose policy wins when the daemon and the caller disagree" is
answered: the daemon's ceiling, with the lowering reported.

**#112**: `effect` takes these levels. `transform` is `apply`; `script` is `run`,
or `propose` to hand the generated script back instead; `retry` is `run`. A `pre`
hook that refuses is `ask`. #112's "cached, reviewable, promotable" generated
script is `propose` by another name.

**#113**: `prompt.mode: plan` is `prompt.autonomy: propose`. Routing to an
operation at `run` still passes every consent gate, so a destructive operation
still asks.

**#114**: generators are `propose` by construction — they print a declaration,
and a human writes it into config. That is not a setting and cannot be raised;
`--write <path>` is the human's own act.

**#110**: `prompt.sample` and `model.provider` answer whether a model is
consulted at all, which is a different question from how far its output may go.
A model answering an upstream server's question on the user's behalf is `apply`,
bounded by `autonomy.max`, whatever #110 names the setting; a question `Route`
sends to a human (`internal/elicit/elicit.go:491`) is never answered by a model
at any level.

**Shipped code**: `prompt.mode` is renamed, with no alias — there are no users.

**Forbidden from here**: a feature-specific autonomy vocabulary; a clamped
setting that refuses instead of lowering; any level that skips a consent gate;
the dial on anything the caller sent in the request (an exec body, a tool call).

**Deferred**: floors → the first setting that needs one. Clamping kinds other
than `KindEnum` → the same. Per-feature ceilings (`prompt.autonomyMax` and so on)
→ only if one global ceiling proves too coarse; `ClampedBy` already allows it
without an API change.

---

## Mapping

| term | from | level |
| --- | --- | --- |
| `prompt.mode: script` | shipped | `propose` |
| `prompt.mode: run`, `mcpx prompt --run` | shipped | `run` |
| an unknown `mode`, e.g. `plan` | shipped behaviour | `propose`, silently — becomes a 400 |
| recipe run `mode: script`, `--script` | shipped | `propose` |
| recipe run `mode: run` (the route's default) | shipped | `run` |
| `off` | #111 | `off` |
| `explain` | #111 | `advise` |
| `confirm` | #111 | `ask` |
| `propose` | #111 | `propose` |
| `retry` | #111 | `run` |
| `rewrite` | #111 | `run` |
| rung 0, deterministic diagnostic | #111 | `advise` |
| rung 1, deterministic near-miss | #111 | `propose`; `run` when the fix is provably the intended one and the caller allows `run` |
| rung 2, explain | #111 | `advise` |
| rung 3, warn / confirm | #111 | `ask` |
| rung 4, propose as an artifact | #111 | `propose` |
| rungs 5 and 6, retry and reinterpret | #111 | `run` |
| refusal with a reason (approach B, item 7) | #111 | `ask` |
| pass-through | #111 | `off` |
| `advise` | #112 | `advise` |
| `transform` | #112 | `apply` |
| `script` | #112 | `run`, or `propose` |
| `retry` | #112 | `run` |
| a `pre` hook that refuses | #112 | `ask` |
| `observe` | #81 | `off` |
| a `before` hook that denies | #81 | `ask` |
| a `before` hook that returns a replacement result | #81 | `apply` |
| an `after` hook that rewrites the result | #81 | `apply` |
| `prompt.mode: plan` | #113 | `propose` |
| `mode: run` | #113 | `run` |
| generators | #114 | `propose`, fixed |
| `sampling.answerLocally` | #110 | `apply` |
| `ClampedBy` | #111 | `Setting.ClampedBy` in `internal/settings` |
| a caller lowering the level | #111 | a call-scoped `<feature>.autonomy` |
| the daemon's maximum | #111 | `autonomy.max` |
| `dryRun` | #111 | not the dial: a flag on the caller's own script |
| `elicit.confirmDestructive`, `elicit.disambiguate` | shipped | not the dial: consent and clarification gates, unchanged |
| `diagnose.preflight`, `script.typecheck` | shipped | not the dial: checks on the caller's own code |
| `prompt.sample`, `model.provider` | shipped, #110 | not the dial: whether a model is consulted |
| `elicit.mode` | `docs/elicitation.md`, proposed | not the dial: who answers an upstream question, by rules the user wrote |
