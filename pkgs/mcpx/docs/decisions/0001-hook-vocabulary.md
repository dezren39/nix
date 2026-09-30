# 0001 — One hook vocabulary, and where a hook's output goes in the exec stream

```
number:   0001
status:   accepted
date:     2026-09-30
issues:   #81 #82 #112 #111 · #181 rows 2, 7 and 13 · touches #5 #6 #7 #80 #172 #177
code:     05c78b2
```

> Four proposals for "run something at a moment mcpx notices", written by
> people who could not see each other's work, and one of them already ships.
> This record keeps the shipped words, says what a hook may be and what it may
> do, and fixes where its output goes in the one stream mcpx documents as a
> contract.

---

## Context

### What ships

A file script runs inside a generated launcher, a template with named holes
(`internal/launcher/template.go:17-45`). Five of the holes are phases the user
fills: `before`, `prefix`, `onSuccess`, `onError`, `suffix`
(`template.go:35-39`). They are declared once, as repeatable `KindSource`
settings `script.before` … `script.suffix`
(`internal/settings/registry.go:212-242`), so each is a config key, an
`MCPX_SCRIPT_*` variable, a generated flag and a `/v1/settings` row. They have
short spellings on `run` and `exec` (`--before`, `--prefix`, `--on-success`,
`--on-error`, `--suffix`, `internal/cli/commands.go:446-455`), layer across the
config chain with the null-splice rule (`internal/config/config.go:208`,
`ResolveLines` at `:264`), and are listed in the man page's LAUNCHER section
(`internal/cli/manual.go:427-448`).

Their meanings are precise, and the order is tested
(`TestLauncherPhasesRunInOrder`, `internal/e2e/e2e_test.go:1437`):

| phase | runs | where it is written |
| --- | --- | --- |
| `before` | first, before mcpx's globals are installed | `internal/runner/runner.go:548` |
| `prefix` | after the globals, before the script's module is imported | `runner.go:554` |
| `onSuccess` | when the entry point returns, with `result.value` set | `runner.go:581` |
| `onError` | when it throws, with `result.error` set; the error is rethrown — "a hook, not a handler" | `runner.go:587`, `registry.go:233-236` |
| `suffix` | in a `finally`, on both paths | `runner.go:593` |

That is a good vocabulary. `onSuccess` and `onError` are separate because they
see different things; `suffix` is the one that runs either way. It already
answers #112's own open question — "does `onSuccess` earn its own phase when
`post` exists?" — with a yes, and says what `post` would have to mean.

### Three holes in its delivery

Found while writing this, each checked against a binary built from `05c78b2`.
All three are the #177 class: accepted, advertised, and not done.

1. **`mcpx exec` accepts `--before`, `--on-success` and `--on-error`, and drops
   them.** The inline branch splices only prefix and suffix into the snippet
   (`commands.go:786-799`); `opts.Phases` is set only for a file
   (`commands.go:805-811`).
   `mcpx exec --before 'console.error("B")' --on-success 'console.error("S")' 'console.log(1)'`
   prints `1`, exits 0, and runs neither hook. The same flags on
   `mcpx run file.ts` run both.
2. **A module whose top-level code throws skips `onError` and `suffix`.** The
   launcher's `await import(...)` sits above its `try` (`runner.go:557` against
   `:569`), so an error during import is never caught. `suffix` is documented
   as "runs last on both paths, like a finally" (`registry.go:240`). A throwing
   `exec` snippet loses its suffix the same way, since the suffix is spliced
   after the body.
3. **None of the five reaches `/v1/exec` or the MCP `mcpx_exec` tool.**
   `execsvc.Options` has no field for them (`internal/execsvc/execsvc.go:101`),
   `Service.Run` never sets `runner.Options.Phases` (`execsvc.go:324-343`), and
   `mcpx run --remote` returns into `execOnDaemon` before the phases are read
   (`commands.go:556-559`, `internal/cli/exec.go:472-485`). The phases are a
   local-CLI feature that the settings registry advertises on every surface.
   `RunWith`'s comment says this was deliberate — "flattening all of that into
   the wire Options would make the wire type a mirror of one command's flag
   list" (`execsvc.go:358-370`) — and this record reverses it for the phases,
   below.

### The daemon's internal hooks, and the bus

`pool.Hooks` (`internal/daemon/hooks.go:24`) is a second hook set —
`OnMessage`, `OnProgress`, `OnListChanged`, `OnResourceUpdated`,
`OnElicitationComplete`, `Elicit` — and every one of them turns into a bus
event. It is where #81's observers get their events, not a vocabulary a user
sees.

The bus (`internal/events/events.go`) declares **16** kinds (`events.go:29-52`),
not the 17 that #81 and #173 count, and two of them — `call.started` and
`task.updated` — are published by nothing: the only reference to either outside
the declaration is `events_test.go:67`. A hook on them would never fire.

#181 row 13 is right that `events.history` is 1024 (`defaults.json:52`, wired at
`internal/daemon/server.go:165`), and it is not the whole answer. Each
subscriber has its own 256-event channel (`events.go:177`, an inline constant
the defaults guard does not see), and `Publish` drops rather than blocks when it
is full (`events.go:207-234`), counting `Subscription.Dropped`. A hook runner
that stalls starts losing events after 256, not 1024. It recovers only by
resubscribing from its last delivered `seq` (`SubscribeFrom`, `events.go:176`),
and only while that `seq` is still inside the 1024 retained.

### What the issues propose

| source | points | phases | what runs | what it may do | declared as |
| --- | --- | --- | --- | --- | --- |
| #81, approach C | `call`, `exec`, `server.start`, or any event kind | `observe` (default), `before`, `after` | a command, a JS/TS module, a Python module, a plugin tool | observe: nothing; before: deny or replace; after: rewrite the result | `hooks: [{on, phase, run \| module \| plugin, when, async}]` |
| #82, approach C | as #81 | as #81 | the plugin's own MCP tool | as #81 | registered at runtime over `/v1`, owned by a plugin session |
| #112, approach C | exec only | `pre`, `main`, `post`, `onError`, `onSuccess` | a script or a prompt | `advise`, `transform`, `script`, `retry` | `options.hooks` per call, `script.hooks` in config |
| the notes, quoted in #112 | exec | `prompt`, `prePrompt`, `postPrompt`, `onErrorPrompt`, `onSuccessPrompt` | a prompt | — | five options |

---

## Options

### A — adopt #81's table, and re-express `script.*` as entries in it

One structured `hooks` list for everything, including inline source; the five
`script.*` settings and their flags removed.

It renames five shipped settings and five flags, and it pushes the cheapest hook
there is — a line of TypeScript that runs inside the script, costs no process
and can see `result` and `script` — through a structure built for
out-of-process actions with timeouts, exit codes and stdin. #81 itself says "do
not do that migration in this issue". And #81's `after` merges success and
failure into one phase, which is exactly the distinction the shipped set got
right.

### B — keep all four and document a mapping

Cheapest today. Every later issue picks whichever vocabulary its author read
first, and the user learns four. This is the default outcome, and it is why #181
row 2 exists.

### C — the shipped phase names become the vocabulary; one structured list for everything that is not inline source

Keep `before`, `prefix`, `onSuccess`, `onError`, `suffix` with their meanings,
add `observe`, keep `script.<phase>` as the spelling of inline hooks, and add one
setting for every other kind of hook using the same phase names.

It costs: `prefix` only means something inside a script; #81's `exec` point is
renamed; #112's `pre`/`post`/`main` go; and the registry needs a structured
setting kind it does not have.

---

## Decision

**C.** A hook is four things: **where** (`on`), **when** (`phase`), **what runs**
(the action), and **what its output may do** (`effect`).

### Phases

Six, and no others. The five shipped ones keep their names and their meaning,
extended from the launcher to any point. One is new.

| phase | when | may hold the operation up |
| --- | --- | --- |
| `before` | before the operation starts | yes, bounded by a timeout |
| `prefix` | the `script` point only, inline source only: after mcpx's surface is installed, before the body | — (it runs inside the script) |
| `onSuccess` | the operation succeeded, before its result is returned | yes, bounded |
| `onError` | the operation failed, before its error is returned; the error still propagates unless an effect replaces it | yes, bounded |
| `suffix` | after either, like a `finally`; it cannot change the outcome | yes, bounded |
| `observe` | after the fact, asynchronously, from the event bus | never |

`observe` is the default, because it is the only phase that cannot slow or
change anything.

### Points

With any phase but `observe`, `on` names a **point**: a moment the daemon
passes through, enumerated in one Go table the way `api.Ops()` enumerates
routes, each entry with the schema of what a hook receives and may return. The
documentation and the validation are generated from the table, and a guard test
fires every point and asserts a hook on it ran (#129's enumerate → completeness
→ probe), so a point cannot be declared and dead the way `call.started` is.

| point | fires on | phases it has |
| --- | --- | --- |
| `script` | one script run: `mcpx run`/`exec`, local or remote, `/v1/exec`, a recipe or intent run | all five |
| `call` | one `tools/call` going upstream, from any caller | `before`, `onSuccess`, `onError`, `suffix` |
| `server.start` | a pooled instance starting | `before`, `onSuccess`, `onError` |

#81 called the first point `exec`. It is `script` here so that `script.onError`
and `{"on": "script", "phase": "onError"}` are visibly the same place.

With `phase: "observe"`, `on` is an `events.Filter` kind prefix, verbatim — the
same vocabulary as `/v1/events?kinds=`, so `"elicit"` matches every elicitation
event and `"*"` matches all. `when` carries the filter's other fields
(`server`, `session`, `uri`), plus `tool` for the `call` point.

### Actions

Exactly one per hook.

| action | what runs | where |
| --- | --- | --- |
| `source` | inline TypeScript or a file, spliced into the launcher | inside the script; `script` point only; declared only as `script.<phase>` |
| `run` | an argv array — never a shell string; write `["sh", "-c", "…"]` to ask for a shell | a child process of whichever process owns the point |
| `module` | a script run by mcpx's own runner, with the generated client | as `run` |
| `prompt` | text with `@placeholders`, sent to the provider from #110 | the daemon; skipped with a warning on the run when no model is configured |
| `plugin` | `"namespace.tool"` — an MCP tool call | whichever server or plugin serves that namespace |

Python is a `run` (`["uv", "run", "hook.py"]`) until #105 makes it a runtime
`module` knows. A shell string is refused rather than guessed at, because
`sh -c` is an injection site and #81 asked for the choice to be loud.

**Input**: JSON on stdin for `run` and `module`, the arguments object for
`plugin`, `@placeholders` for `prompt` (JSON-encoded, never text-substituted,
as recipes already do), and flat `MCPX_HOOK_*` variables for the one-line case.
Those names go in #172's outbound table, not beside the code that sets them.

**Output**: one JSON object whose keys are the levels of the dial in
[0002](0002-autonomy-dial.md) — `advise`, `ask` or `refuse`, `propose`, `apply`,
`run`. A key above the hook's effect is dropped and reported in its hook frame,
never honoured. A non-zero exit is a result, not an error, following the
adapter's precedent (`internal/adapter`): the hook failed, `hook.failed` is
published, and the operation carries on — unless the hook says
`"failure": "closed"`, which is legal only when its effect may refuse (`ask` or
above), because failing closed *is* a refusal.

### Effect

The dial from [0002](0002-autonomy-dial.md), per hook, default `advise`. It is
clamped three times: by the phase (`suffix` at most `advise`, `observe` always
`off`), by the caller's `hooks.autonomy`, and by the daemon's `autonomy.max`.

### Declaration: settings, not endpoints

- **`script.before` … `script.suffix` stay exactly as shipped.** They are
  `{on: script, phase, source}`, and the only way to declare a `source` hook.
  Nothing about them is renamed.
- **One new daemon-scoped, repeatable setting, `hooks`**: a list of hook objects
  for every other action, layered across the config chain with the same
  null-splice rule as `script.*`. It needs a structured setting kind — a JSON
  value validated against a schema declared on the setting — which the
  registry does not have. #110's `model.agents` needs the same kind; it is one
  addition, not two.
- **Per call, `ExecOptions` gains two fields.** `script` carries the resolved
  `source` phases — `{before, prefix, onSuccess, onError, suffix}`, each a list
  of lines, the settings' own shape — so a remote run and a local one run the
  same launcher. That closes hole 3. `hooks` appends `script`-point hooks for
  that run and may only lower effects. This is #112's `options.hooks`, with its
  phase names replaced.
- **A plugin's hooks come from its manifest** (#80's `mcpx-plugin.json`), are
  resolved when its plugin session starts and dropped when it ends (#82's
  approach C). There is no hook-registration route.
  `GET /v1/settings/hooks` lists what is in force, and the registry already
  records where each value came from, which answers #81's "list them" and
  #82's "what is extending this daemon" without a new operation.

### The exec frame order (#181 row 7)

One order, replacing the block in `docs/exec.md` §"The frame order" and the
comment on `execsvc.Frame` (`execsvc.go:138-144`):

```
start{runId}
hook{phase:before}                           out-of-script before hooks, in declaration order
log | emit | stdout | call | hook{on:call}   interleaved, as they happen
artifact{id,name,mime,size,sha256,uri}
hook{phase:onSuccess|onError}                in declaration order
result | error                               exactly one, and final
hook{phase:suffix}
end{exitCode,durationMs}
---- only for delivery:"stream" ----
artifact.chunk{id,seq,data}
artifact.end{id}
```

With four rules:

1. **`result` or `error` is sent once, and it is final.** Every `onSuccess` and
   `onError` hook finishes before it. #112 put those frames after `result`
   while also saying the final result reflects a transform; both cannot be
   true, and a consumer that reads the answer and disconnects — the reason the
   order exists — has to be reading the real answer.
2. **What a hook changed is visible.** A hook frame carries
   `{on, phase, action, effect, durationMs, changed}`, plus `original` when an
   `apply` replaced the value. A caller can always see what mcpx changed.
3. **#111's `call` frame is interleaved**, beside `log`, `emit` and `stdout`,
   because calls happen while the script runs. It carries server, tool, a hash
   of the arguments, duration, ok or error, and `destructiveHint`.
4. **Frame types are an open set; the order is the contract.** A consumer
   ignores a type it does not know. That is already what the CLI does —
   `applyFrame` has no default case (`internal/cli/exec.go:199-264`) — and the
   plugin uses the structured shape, not frames. New frame types therefore need
   no capability flag, which answers #111's open question: `call` frames are on
   by default.

Inline `source` hooks produce no hook frames. What they log or emit is already
part of the run and arrives as `log`, `emit` or `stdout`.

---

## Consequences

**#81** adopts this vocabulary. `exec` becomes `script`; `after` becomes
`onSuccess` and `onError`, or `suffix` where it must run either way; `async` is
`observe`; string commands are refused. Slice 1, the observer table, is
otherwise unchanged. Its delivery section should use 256 and 1024 as above, and
drop `call.started` and `task.updated` from the hookable list until something
publishes them.

**#82** does not add a registration route. Runtime hooks are manifest hooks
owned by the plugin session, and teardown is the session ending. A
runtime-mutable hook table stays in #82 and waits for a concrete need.

**#112** keeps its structure and loses its words: `pre` → `before`, `post` →
`onSuccess`, `onSuccess` → `onSuccess`, `onError` → `onError`; `main` is not a
hook at all — a prompt that *is* the run is `/v1/intent` (#113). Its effect
names are replaced by 0002's, its stream placement by the order above, and
`script.hooks` by `hooks` entries with `"on": "script"`. The deterministic half
it wants to ship first is `module` and `run` hooks at the `script` point.

**#111** gets its `call` frame placed.

**#7** is unchanged: placeholders stay the launcher's. A prompt hook's
`@placeholders` use the same spelling and the same JSON encoding.

**#5** decides one question for both: hooks are declarable wherever `script.*`
is, since a project config can already run code on every `mcpx run` through
`script.before`. What a project-level config may declare is #5's call, made once
for `servers`, `script.*` and `hooks`.

**#6**: an observer is a live trigger over the same event vocabulary. Replaying
hooks over history needs the store.

**#172**: `MCPX_HOOK_*` names are declared in the outbound table.

**#177** gets the three delivery holes, independent of everything else here.
The fixes are in the report that accompanies this record, not in this change.

**Forbidden from here:** a hook-registration endpoint; a hook action that is a
shell string; a phase outside the six; a `source` action anywhere but
`script.<phase>`; any frame after `result` or `error` that changes it; a point
that nothing fires.

**Deferred:** a runtime-mutable hook table → #82. `mcpx hook test <name>
--event <file>` → #81, as a local CLI command. Hooks registering artifacts →
not in the first version; #81 decides. How a `run`-effect `onError` retry
streams its second attempt → #111 rung 5, bound by rule 1. `script.started` and
`script.finished` bus kinds, so the `script` point can be observed → #81.

---

## Mapping

| term | from | becomes |
| --- | --- | --- |
| `script.before` | shipped | `on: script`, `phase: before`, action `source` — unchanged |
| `script.prefix` | shipped | `on: script`, `phase: prefix`, action `source` — unchanged |
| `script.onSuccess` | shipped | `on: script`, `phase: onSuccess`, action `source` — unchanged |
| `script.onError` | shipped | `on: script`, `phase: onError`, action `source` — unchanged |
| `script.suffix` | shipped | `on: script`, `phase: suffix`, action `source` — unchanged |
| `@before` … `@suffix` | launcher placeholders | unchanged; the launcher's fill for the phases |
| `pool.Hooks` | `internal/daemon/hooks.go:24` | not a user vocabulary; the source of bus events |
| `on: <event kind>` | #81 | `on: <event kind>`, `phase: observe` |
| `phase: observe` | #81 | `observe` |
| `phase: before` | #81 | `before` |
| `phase: after` | #81 | `onSuccess` and `onError`, split by outcome; `suffix` if it must run on both |
| point `exec` | #81 | `on: script` |
| points `call`, `server.start` | #81 | unchanged |
| `run: "cmd …"` (a string) | #81 | `run: ["cmd", …]`; a string is refused |
| `module: x.ts` | #81 | `module` |
| `module: x.py` | #81 | `run: ["uv", "run", "x.py"]` until #105 |
| `plugin` + `tool` | #81 | `plugin: "namespace.tool"` |
| `async: true` | #81 | `phase: observe` |
| runtime hook registration | #82 | manifest `hooks`, owned by the plugin session |
| `phase: pre` | #112 | `before` |
| `phase: main` | #112 | not a hook: `/v1/intent` (#113) |
| `phase: post` | #112 | `onSuccess` |
| `phase: onSuccess`, `phase: onError` | #112 | unchanged |
| `script: x.ts` | #112 | `module` (a file), or `source` through `script.<phase>` |
| `prompt` | #112 | `prompt` |
| `effect` values | #112 | [0002](0002-autonomy-dial.md)'s levels |
| `options.hooks` | #112 | `ExecOptions.hooks`, `script` point only |
| `script.hooks` | #112 | `hooks` entries with `on: script` |
| `prePrompt` | notes | `prompt` action, `phase: before` |
| `postPrompt`, `onSuccessPrompt` | notes | `prompt` action, `phase: onSuccess` |
| `onErrorPrompt` | notes | `prompt` action, `phase: onError` |
| `prompt` | notes | `/v1/intent`, not a hook |
| `hook` frame placed after `result` | #112 | before `result` for `onSuccess`/`onError`; after it only for `suffix` |
| `call` frame | #111 | interleaved with `log`/`emit`/`stdout` |
