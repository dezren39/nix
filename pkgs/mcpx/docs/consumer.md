# mcpx as a consumer

> Sampling, elicitation and completion are things a server asks of its client.
> mcpx is a client to its upstream servers and a server to whatever drives it,
> so today it only passes them through. Are there cases where mcpx should
> initiate them itself?
>
> — issue #41

Four, and they are not equally good. This is the record of which, why, and
what was deliberately not built.

## 1. Deterministic schema-change diagnostics

The most valuable thing here needs no model at all, and that was the central
finding of the issue. mcpx already knows every tool's input schema. What it
did not know was what those schemas *used to be* — `internal/codegen` could
fingerprint a catalog and diff two of them, but nothing ever stored one, so
every diff was against nothing and the question "when did this change" had no
answer anywhere in the program.

So `internal/diagnose` keeps one. Each time the daemon reads schemas, it
records every tool's flattened shape and appends whatever differs to a per-tool
change list in `<state>/catalog-history.json`. That file is the missing half.
With it, a script that calls a tool with no arguments after that tool gained a
required one is not a mystery; it is two facts mcpx is holding, put next to
each other:

```
demo.echo: the argument object is no longer optional: message is required
           (schema changed 2026-09-20: `message` became required)
  you wrote:  await demo.echo()   (line 2)
  minimum:    await demo.echo({ message: "" })
```

Three decisions inside that are worth stating.

**The scanner does not parse TypeScript.** It looks for two identifiers, a dot
and a parenthesis, and skips every construct that could hide one: strings,
template literals, both comment forms. A call assembled dynamically is
invisible to it, and that is the correct failure — a diagnostic about a call
nobody wrote is worse than no diagnostic. It also stops at the first sign of
not knowing: a spread in an argument object silences the missing-argument
check entirely, because a spread can supply anything.

**Only configured namespaces are diagnosed.** `console.log(x)` and
`JSON.parse(s)` are indistinguishable from tool calls at this level. Having an
opinion about somebody else's code is not this package's business.

**The issue's example needed correcting.** It reads:

```
demo.create_issue: argument 1 is now required (`options` became required).
  you wrote:  await demo.create_issue("title")
  minimum:    await demo.create_issue("title", {})
```

mcpx's generated client does not take positional arguments. `codegen` binds
every tool as `fn(args?: {...})` — one object, optional exactly when the schema
has no required properties. So the diagnostic mcpx can actually produce is
"the argument object is no longer optional", which is the same fact in the
shape the generated code has. Anything else would be advice that does not
compile.

The upstream half, `diagnose.CallError`, runs in `Registry.Call`
(`internal/daemon/callerr.go`), the one place a failed call passes on its way
to every surface. The server's error stays first and whole; the rendered
diagnostic follows it, and `/v1/call` carries the same finding as data:

```json
{"error": "mcp error -32602: Invalid params\ndemo.create_issue: the call was rejected and repo is required but was not sent (schema changed 2026-09-30: `repo` was added and is required)\n  minimum:    await demo.create_issue({ repo: \"\", ... })",
 "diagnostics": [{"kind": "invalid-params", "tool": "demo.create_issue", "field": "repo",
   "changed": {"when": "2026-09-30T…", "kind": "argument-added", "what": "`repo` was added and is required"},
   "fix": "await demo.create_issue({ repo: \"\", ... })", "fatal": true}]}
```

So `mcpx call` prints it, `mcpx --json call` puts the document on stdout, a
task's result carries it, a script's `ToolError` has `.diagnostics`, and
`mcpx_call` shows the text. Data beside the prose, because an agent can act on
`field` where it would have to parse a sentence.

Three conditions decide whether it runs, and each is there because of what it
said without them. Only an answer from a server is explained: a timeout or a
server that would not start never answered, so the arguments were not at
fault. Not a code in `-32000..-32099`: mcpx's own client reports a dropped
connection as `-32000 connection closed: unexpected EOF`, and the text match
read "unexpected" as "expected" — a crash was explained as a missing argument.
And not a namespace whose schemas have not been read, where `CallError` said
"no server is configured under this namespace" about a configured server; it
used to answer that for any failure, before it looked at the code at all.

Not covered: `mcpx_call` from a client that can answer questions goes through
`CallAsk`, which leases the pool itself; `/v1/call/{server}/{tool}` carries the
text but not the field; and a server that reports bad arguments as a result
with `isError` has made a successful call, so there is no failure to explain.

`--diagnose-preflight` is on by default on `run` and `exec`. The failure it
catches surfaces otherwise as an error from a server, halfway through, after
the side effects of every call before it.

## 2. Elicitation mcpx raises for itself

Two policies, both off by default, both landing on `Registry.Call` between
resolving an instance and using it.

**Disambiguation** (`elicit.disambiguate`, `never | ask`). It asks only when
the answer is genuinely open: the server is exclusive, so its instances are
distinct things rather than copies; several are live; and the caller's own
scope key names none of them. When the caller already has an instance, the
scope key *is* the answer and a question would be theatre. The form is an enum
of the live keys plus "new", each with what it is doing — holders, calls,
idle time, pid — because a choice without that is not a choice.

**Destructive confirmation** (`elicit.confirmDestructive`, off). Before a call
to a tool that may be destructive: annotated `destructiveHint: true`, or (the specification's default) not annotated `readOnlyHint: true` or `destructiveHint: false`. This is the step a
human-driven client has and a script does not.

The two disagree about what a silent deadline means, deliberately. An
unanswered disambiguation takes `elicit.disambiguateDefault`, which is `new` —
a caller that wanted a particular browser and did not say so gets a fresh one
rather than somebody else's. An unanswered confirmation refuses the call.
There, doing nothing is safe; here, doing nothing is the destructive thing.

Routing needed no change in `internal/elicit`. A confirmation is a lone
boolean called `confirm`, which `Route` already sends to a human because
consent is not the agent's to give; a disambiguation is an enum, which goes to
the agent that raised the call. `internal/elicit/policy.go` adds only the
constructors, whose job is to make it impossible to raise one of mcpx's own
questions without a default and a deadline.

The daemon had no access to settings before this — `daemon.Options` carries a
`config.Config` and nothing else — so `settings.Resolve` folds the same
configuration files and environment the CLI reads. Whether a destructive call
is confirmed is not a question with two correct answers depending on which
process asked.

**Several daemons**, the other disambiguation case in the issue, is not here.
It belongs to the plugin: the question is which daemon serves a directory, and
by the time mcpx is answering, that has been decided.

## 3. Recipes

A recipe is not a new artefact. It is a script already on the search path that
declares its own holes:

```ts
// Close stale issues in a repository.
// @param repo:string        which repository, as owner/name
// @param days:number = 30   how old counts as stale

const found = await demo.search({ repo: @repo, olderThanDays: @days });
```

Declaration is a header comment because a comment survives every runtime,
cannot affect execution, and is visible in the file somebody edits. References
are `@name`, the spelling the launcher already uses for its own holes.

**Values are substituted as JSON, never as text.** A recipe runs with the
user's credentials; raw substitution would make every placeholder an injection
site. A string arrives quoted, an object arrives as an object, and a recipe
with an unfilled required hole is an invalid script — which is the right way
round, because it cannot then be run by accident.

**Matching is a score over four things the recipe already carries**: its name,
its summary, its placeholder names and the tools it calls. No model, no
embedding, no index. The same request gives the same recipe every time, at no
cost. `Decide` refuses to choose when the leader is not ahead of the runner-up
by `recipes.matchMargin` (150%, half as much again), because running the wrong
saved script is a side effect rather than a wrong answer.

JavaScript built-ins are excluded from the tool list. `console.log` counted as
a tool makes every recipe match the word "log".

Missing placeholders become one form with one deadline, not one question each:
a person filling in four fields is doing one thing, and four deadlines are four
chances for a run to die half-configured.

## 4. `/prompt`, which is `POST /v1/intent`

**The name.** `POST /v1/prompt` was taken, by the operation that renders an
upstream server's MCP prompt, and Go's mux keys on method and path. Renaming
somebody else's operation to free a word was not worth it. The CLI command is
`mcpx prompt`, which is what the issue asked for; the route is `/v1/intent`.

The order is recipes first, generation second, and generation is **off by
default** (`prompt.sample: never`). The issue sequences sampling-backed
generation last and conditions it on there being a caller that can answer
sampling; most cannot, and a caller that cannot pays the entire deadline to
find out. With it off, an unmatched request returns the ranked recipes and
says so in one sentence. That is the honest default, and it is also the one
that does not make `mcpx prompt` hang for ninety seconds on a machine with no
answerer.

With it on, the request goes out as an ordinary MCP sampling request through
the same broker that carries every other question — so the plugin, an agent
watching `mcpx elicit`, and an MCP client that declared sampling all answer it
without learning anything new. It is given the slice of the catalog the
existing search ranked for this request, inside `prompt.catalogBudget` tokens,
and nothing else. The whole reason mcpx is worth having is that tool schemas
stay out of a model's context; a generation request has to send some, so it
sends as few as it can.

The generated script is validated with the step-1 diagnostics before anything
runs, and returned rather than run unless `autonomy: run` is asked for.

Whatever is asked for, the daemon's `autonomy.max` is the most it does: a
request above it — in a body, the `X-Mcpx-Settings` header, the CLI's
`--run`, an MCP tool call or the plugin — is lowered to it, not refused, and
the answer carries `requested` and `clampedBy` beside `autonomy`. The CLI
prints `requested run, clamped to propose by autonomy.max (...)` on stderr.
The same ceiling bounds `repair.autonomy` (whether a failed call carries
diagnostics) and `hooks.autonomy` (whether configured script hooks run).

## Running a script from the daemon

`recipe_run` and `intent` in run mode need to execute something, and the
daemon has never run scripts. `Server.execScript` calls `internal/runner`
directly, against the daemon's own endpoint. **This is a seam, not a design.**
`POST /v1/exec` has since landed, with its own options and permission model
(`internal/api/ops_exec.go`, `internal/daemon/routes_exec.go`), but
`execScript` (`internal/daemon/consumer.go:517`) has not yet been rerouted
through it. It is one function, marked, for exactly that reason.

## The concerns from the issue, and what was decided

**Scope — "anything that makes mcpx call a model has to justify that it isn't
rebuilding the harness."** Three of the four things built here never call one.
The fourth has no model of its own and never will: option 3 from the issue, a
configured provider with an API key, was not built and should not be. It is
the one that makes mcpx an agent. Sampling through the caller is the opposite
— it is mcpx asking the harness it already sits under, which is what the
specification is for.

**Trust — "generated code runs with the user's credentials."** Return the
script by default (`prompt.autonomy: propose`, one level of the dial in
[decisions/0002](decisions/0002-autonomy-dial.md)); run only on explicit request.
Recipe values are JSON-encoded, so a recipe cannot be turned into an injection
site by its arguments. A recipe name that would become a path is refused.

**Latency and cost — "never sample on a path that doesn't need it."** The
deterministic diagnostics come first and answer the case the issue identified
as the common one. Recipe matching is free and deterministic. Sampling happens
on exactly one path, only when nothing matched, and only when it has been
turned on.

**Headless callers.** Every question mcpx raises for itself carries a default
and a deadline, and `elicit.Ask` exists so that forgetting either is not
possible. The defaults differ by question, for the reasons above. A caller
that gets no answer is told which question it raised, so it can look up what
was asked rather than guessing.

## Not built

- **A language service.** The issue is right that suggesting a fix to a script
  is LSP work rather than MCP completion, and right to keep the two apart.
  `internal/diagnose` is the part an LSP would need; the server itself is a
  separate piece of work and nothing here presumes its shape.
- **Sampled rewrites of a diagnostic.** When the caller is an agent it reads
  the error either way; the value is in the error being specific, not in a
  model rewording it. If a human-facing front end ever wants a proposed
  rewrite, `diagnose.Diagnostic` is already the structured input for one.
- **Daemon disambiguation**, as above: the plugin's question, not mcpx's.

## Bugs found on the way

The schema cache (`cacheVersion`) is not invalidated when a cached entry gains
a field. Adding `Annotations` to `mcpclient.Tool` parsed cleanly against an
existing cache file and left it empty, which presented as the destructive-call
policy silently never firing — the annotations were there upstream and absent
from the cache nobody had reason to reread. The version is bumped, with a
comment saying why; the shape of the trap is still there for the next field.
