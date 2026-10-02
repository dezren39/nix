# mcpx

> A user types a prompt. An agent that has never heard of mcpx works out what
> it is, writes one script, and hands back two screenshots. This is that walk,
> with the exact bytes it saw.

---

At 09:14 a user opens a blank session and types:

    Take a screenshot of example.com, click the link on it,
    wait for the new page to load, and screenshot that too.

The agent has no MCP servers configured. Its tool list is the usual built-ins —
read, write, edit, bash, glob, grep. Nothing named "browser", nothing named
"chrome". There are 27 Chrome tools live on this machine and not one byte of
their schemas is in the agent's context, which is the entire point, but the
agent does not know that yet.

What it does have is one instruction file, [`AGENTS.md`](../AGENTS.md),
injected at session start. Abridged -- the full file is 109 lines and adds
profiles, per-tool `types`, `catalog --budget` and the `globalThis` bindings,
which this walk does not need and #264 is about -- it reads:

```markdown
# mcpx

Every MCP server on this machine is reachable from the shell through `mcpx`.
None of their tool schemas are in your context. You pull in only what you need.

## The loop

1. `mcpx ls` — which namespaces exist. Cheap; run it whenever.
2. `mcpx search <words>` — find a tool by name or description.
3. `mcpx types <namespace>` — load signatures for that namespace only.
4. `mcpx run <name>` or `mcpx exec '<code>'` — do the work.

Do not run `mcpx types` for every namespace. Loading one costs a few hundred
tokens; loading all of them costs thousands and defeats the point.

## Scripts

A named script lives at `.mcpx/scripts/<name>.ts`, searched upward from the
working directory, then `~/.config/mcpx/scripts/`. Nearest wins.

    mcpx scripts                 list what exists
    mcpx run <name> [args...]    run one; args arrive in Deno.args
    mcpx run ./path/to/file.ts   a path is used verbatim

Scripts are TypeScript on a real runtime (deno, else bun, else node). They get
the full runtime: filesystem, network, subprocesses. Relative paths resolve
against *your* working directory, not the script's.

Import the generated client, which mcpx writes next to the script:

    import tools from "./mcpx-client.ts";
    const pages = await tools.chrome_devtools.list_pages({});

Every namespace is also a bare identifier inside `mcpx exec`.

## Results

A result arrives unwrapped: structured output already parsed, JSON-in-text
already parsed, plain text as a string. `.raw` holds the full MCP envelope —
that is where image bytes live. A tool returning `isError` throws `ToolError`.

## Stateful servers

`mcpx ls` has SHARING and SCOPE columns. SCOPE says what a server process is
keyed by; SHARING says whether one process serves several callers at once.

A namespace scoped to `session` gives each session its own process. If the host
set `MCPX_SESSION_ID`, successive runs in that session reach the same process,
so a browser persists between invocations. If it did not, each run is isolated
and anything that must share state — navigate, snapshot, click — has to happen
inside **one** invocation.

`mcpx status` shows the KEY each live process is serving, which is the quickest
way to see whether you are sharing or not.

## When something is wrong

- `error` in `mcpx ls` — `mcpx status` has the server's stderr.
- Arguments rejected — `mcpx types <ns>` is generated from the server's own
  schema and is authoritative.
- Wedged server — `mcpx restart <namespace>`.

## Do not

- Do not add these servers to your own MCP config. The point is that they are
  not in your context.
- Do not parse human output; use `mcpx --json <command>`.
- Do not assume a tool name is a valid identifier: `fancy-name` becomes
  `fancy_name`. `mcpx types` shows the real function name.
```

The first three lines do all the framing. *Reachable from the shell through
`mcpx`* means this is a command, not a tool call and not a plugin — it belongs
in bash. *None of their tool schemas are in your context* explains the absence
the agent just noticed. *You pull in only what you need* says the absence is
deliberate and reversible.

From those three lines the agent now knows:

- capability it cannot see exists, and `mcpx` is the door to it;
- discovery is a separate, cheap step from use;
- loading everything is the failure mode the file is warning against.

That last point is what stops it reflexively fanning out an explorer subagent
across `~/.config`, `/usr/local/bin` and the repo looking for browser tooling.
The file already said where to look. Twelve lines of guideline follow, and they
are all shaped the same way — a command, then the cost of misusing it.

So the agent makes one pinned memory:

> **mcpx** — MCP via shell, not tools. `ls` → `search` → `types <ns>` → `run`.
> Scripts: `.mcpx/scripts/<name>.ts`, import `./mcpx-client.ts`. Stateful
> namespaces need one invocation. Never `types` everything.

and three todos:

1. `mcpx ls` — confirm a browser namespace exists and note its mode
2. `mcpx types` for that one namespace only
3. write `.mcpx/scripts/shot-flow.ts`, run it, report paths

---

Todo one. The agent runs `mcpx ls` and gets back, in 10 milliseconds, without
a single MCP server process being started:

```
NAMESPACE        TOOLS  SHARING    SCOPE    LIVE  STATE  DESCRIPTION
chrome_devtools  27     exclusive  session  0     ready  drive a headless Chrome
codebase_memory  0      shared     global   0     error  server "codebase-memory": initialize: mcp error -32000: con…
codedb           5      shared     global   0     ready  where a symbol is defined, who calls it
context7         2      shared     global   0     ready  third-party library docs
fff              3      shared     global   0     ready  ranked search over ~/git
fff_nix          3      shared     global   0     ready  ranked search over ~/.config/nix
fff_worktree     3      shared     global   0     ready  ranked search over opencode worktrees

Next: `mcpx types <namespace>` for signatures, `mcpx search <query>` to find a tool.
```

Seven namespaces, 43 tools, 171 tokens. `chrome_devtools` is the one. Its
`SCOPE` is `session` and its `SHARING` is `exclusive`, which the instructions
already told it means *one browser per session, one caller at a time* — so
unless the host set a session id, do it all in one invocation. `LIVE 0` means
nothing is running yet. One row
says `error`, and it is honest about why — that server is genuinely broken today
and mcpx says so instead of quietly omitting it.

The agent does not yet know which of the 27 tools it needs, so it narrows
first:

```
$ mcpx search screenshot click
FUNCTION                         DESCRIPTION
chrome_devtools.click            Clicks on the provided element
chrome_devtools.take_screenshot  Take a screenshot of the page or element.
chrome_devtools.fill_form        Fill out multiple form elements (inputs, selects, checkboxes, radios) at once. ALWAYS pre…
chrome_devtools.take_snapshot    Take a text snapshot of the target page based on the a11y tree. The snapshot lists page e…
```

Sixty-one tokens. Four names, enough to know the shape of the job.

Todo two. `mcpx types chrome_devtools` — 4,402 tokens, the single largest thing
the agent will load all session, and it loads it once and deliberately:

```typescript
// Generated by mcpx. Call these from a script run with `mcpx run <file.ts>`.
// Every function is async and returns the tool's result.

/** drive a headless Chrome */
declare namespace chrome_devtools {
  function click(args: {
    /** Set to true for double clicks. Default is false. */
    dblClick?: boolean;
    /** Whether to include a snapshot in the response. Default is false. */
    includeSnapshot?: boolean;
    /** Targets a specific page by ID. */
    pageId: number;
    /** The uid of an element on the page from the page content snapshot */
    uid: string;
  }): Promise<ToolResult>;
```

This is generated from Chrome's own JSON Schema, so it is authoritative rather
than remembered. Two facts land that the agent could not have guessed: `pageId`
is **required** on nearly every call, and `click` takes a `uid` "from the page
content snapshot" — not a CSS selector. Those two constraints determine the
whole shape of the script. An agent working from memory of how browser
automation usually looks would have written `click({ selector: "a" })` and
failed twice before finding out.

Todo three. The script. Convention says `.mcpx/scripts/<name>.ts`, so:

```typescript
// Screenshot a page, click its first link, wait for load, screenshot again.
//
// Usage: mcpx run shot-flow [url] [outDir]
import tools from "./mcpx-client.ts";

const cd = tools.chrome_devtools;
const url = Deno.args[0] ?? "https://example.com";
const outDir = Deno.args[1] ?? "./shots";
await Deno.mkdir(outDir, { recursive: true });

async function shot(pageId: number, name: string): Promise<string> {
  const result = await cd.take_screenshot({ pageId });
  const image = ((result.raw as any)?.content ?? []).find((c: any) => c.type === "image");
  const path = `${outDir}/${name}.png`;
  await Deno.writeFile(path, Uint8Array.from(atob(image.data), (c) => c.charCodeAt(0)));
  return path;
}

await cd.new_page({ url });

// list_pages prints "<id>: <title> (<url>)"; find the one we just opened.
const pages = String(await cd.list_pages({}));
const host = new URL(url).host;
const row = pages.split("\n").find((line) => line.includes(host) && /^\d+:/.test(line));
if (!row) throw new Error(`no page for ${host} in:\n${pages}`);
const pageId = Number(row.split(":")[0]);

const before = await shot(pageId, "1-before");

// The a11y snapshot gives every element a uid; click needs that, not a selector.
const snapshot = String(await cd.take_snapshot({ pageId }));
const uid = snapshot.match(/uid=(\S+) link "/)?.[1];
if (!uid) throw new Error(`no link in snapshot:\n${snapshot}`);

await cd.click({ pageId, uid });
await cd.wait_for({ pageId, text: ["IANA", "Example"] });

const after = await shot(pageId, "2-after");
console.log(JSON.stringify({ url, clicked: uid, before, after }, null, 2));
```

Three things in there are only possible because this is a real runtime and not
a sandbox. `Deno.mkdir` and `Deno.writeFile` do actual filesystem work. `atob`
decodes the PNG out of `.raw`, because the image bytes never pass through the
model. And `new URL(url).host` is just the standard library, present because
this is ordinary TypeScript.

The whole browser conversation — `new_page`, `list_pages`, `take_screenshot`,
`take_snapshot`, `click`, `wait_for`, `take_screenshot` — is seven tool calls
and a multi-kilobyte a11y tree. None of it reaches the agent. Only the last
`console.log` does.

```
$ mcpx run shot-flow
{
  "url": "https://example.com",
  "clicked": "1_3",
  "before": "./shots/1-before.png",
  "after": "./shots/2-after.png"
}
```

Under a second of agent-visible work, and a browser that started, did its job,
and was released when the process exited. Had a second agent run the same
script at the same moment, it would have gotten its own browser and its own
`1_3`, because `session` mode leases a process per run.

The agent replies:

> Both screenshots are saved.
>
> - `./shots/1-before.png` (25 KB) — example.com as loaded
> - `./shots/2-after.png` (100 KB) — iana.org, after clicking the "Learn more"
>   link (`uid=1_3`)
>
> Reusable as `mcpx run shot-flow <url> <outDir>`.

The user opens both, sees the plain example.com page and then the IANA page,
and notices the interesting part: the agent spent about 4,600 tokens total,
nearly all of it on one deliberate `types` call, and left behind a script that
costs nothing to run again.

---

What the user turns over next is not whether it worked but where the edges are.
If a script can return primitive values, can a plain one skip the browser
entirely and just print a number? Sessions are leased per run — could one be
named, pinned, listed, searched, and re-attached later, read-only for watching
or read-write for driving? Could a still-cached session be resumed instead of
rebuilt? Could the daemon's log answer questions about what ran and how long it
took? A wedged browser can be restarted, but can it be restarted for one
session rather than the whole namespace? Can the daemon be upgraded without
dropping the servers it holds? And how should this be wired up at all — a
launchd job, a systemd unit, a shell function, a completion script? What about
driving it interactively, or from CI with the script on stdin, or with
everything in environment variables?

Those are the questions the reference pages exist to answer, so the user opens
them, and over time learns about:

<!-- table-of-contents-marker -->

## Zero-context discovery

```
created:      2026-09-25T18:10:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    3
status:       core
tags:         area:discovery, cost:context
description:  ls, search and types answer from an on-disk cache without
              starting a single MCP server process.
```

`mcpx ls`, `mcpx search` and `mcpx types` never touch a server. They read a
schema cache keyed to the config file's fingerprint, so a cold daemon answers
in about 10 ms and a warm one in about 1 ms.

The cache is written to `$XDG_CACHE_HOME/mcpx/schemas-<hash>.json` and survives
daemon restarts, machine reboots, and the MCP server binary being deleted —
there is a regression test that moves the binary away and checks `mcpx types`
still answers.

The practical effect is that an agent can run `mcpx ls` as often as it likes.
The cost model is: `ls` about 171 tokens, `search` about 61, `types` per
namespace from 675 (codedb) to 4,402 (chrome_devtools). Nothing is loaded that
was not asked for.

`mcpx refresh` re-reads every server. `mcpx --json ls` is the machine-readable
form; never parse the table.

2026-09-27T05:05:00-05:00

## Sharing and scope

```
created:      2026-09-25T18:30:00-05:00
last-updated: 2026-09-27T06:20:00-05:00
increment:    5
status:       core
tags:         area:pools, area:concurrency
description:  two independent axes -- how many callers share a process, and
              what decides which process you get.
```

A single `mode` setting used to answer both questions at once, which meant
neither could be chosen freely. They are now separate.

**`sharing`** — how many callers may use one process at a time.

| value | meaning |
| --- | --- |
| `shared` (default) | any number of concurrent callers; MCP multiplexes by JSON-RPC id |
| `exclusive` | one caller at a time, others queue |

**`scope`** — what a process is keyed by. One live process per distinct key.

| scope | key | resolved by |
| --- | --- | --- |
| `global` (default) | constant | mcpx |
| `repo` | `git rev-parse --git-common-dir` | mcpx |
| `worktree` | `git rev-parse --show-toplevel` | mcpx |
| `cwd` | working directory | mcpx |
| `session` | `MCPX_SESSION_ID` or `--session` | caller |
| `parent-session` | `MCPX_PARENT_SESSION_ID` | caller |
| `pid` | calling process id | caller |
| `call` | unique per invocation | mcpx |

```jsonc
"chrome-devtools": {
  "command": "chrome-devtools-mcp",
  "args": ["--headless", "--isolated"],
  "mcpx": { "sharing": "exclusive", "scope": "session", "max": 4, "idleTimeout": "5m" }
}
```

The defaults describe a stateless server, which most are, so a server with no
`mcpx` block gets one shared process for everything.

### What mcpx cannot work out for itself

`repo`, `worktree`, `cwd` and `call` are computed from the call itself.
`session` and `parent-session` cannot be: a subagent and its parent share a
working directory and differ only by an identifier their host assigns. The
caller supplies those through `MCPX_SESSION_ID` and `MCPX_PARENT_SESSION_ID`.

A scope that cannot resolve **degrades to per-call isolation and says so**
once, in the daemon log, naming the variable that would fix it. Degrading
toward isolation is deliberate: accidentally sharing a stateful process
corrupts results, while over-isolating only costs a process.

Verified end to end. Two invocations under one `MCPX_SESSION_ID` reach the same
browser, so the second sees the page the first opened. Two subagents under one
`MCPX_PARENT_SESSION_ID` share; a third under a different parent does not:

```
live= 2 keys= ['psession:root', 'psession:other']
```

### Lifetime

`pid`-scoped processes are stopped as soon as the process they belong to
exits, rather than waiting out an idle timer — there is no possible future
caller. Everything else is reaped on the idle timer, except keys a caller
minted for itself, which are stopped the moment that caller finishes.

2026-09-27T06:20:00-05:00

## Profiles and aliases

```
created:      2026-09-27T13:00:00-05:00
last-updated: 2026-09-27T13:00:00-05:00
increment:    1
status:       standard
tags:         area:config, cost:context
description:  select subsets of servers by profile, and expose one server
              several times under different namespaces and tool subsets.
```

**Profiles** decide which servers a command sees at all. A browser nobody is
using should not occupy a namespace, a row of `mcpx ls`, or a share of a
catalogue budget.

```jsonc
"chrome-devtools": { "mcpx": { "profiles": ["web"], "default": false } }
```

```
mcpx ls                                 the default set
mcpx --profile web ls                   default set plus the web servers
mcpx --profile web --skip-default ls    exactly the web servers
mcpx --all-profiles ls                  everything, ignoring profiles
```

A server is default-on unless it says otherwise; `"pool": { "default":
false }` flips the baseline so servers opt in instead. The selection applies
to everything derived from the server list — `ls`, `types`, `catalog`,
`search`, and the generated client — so a script written under one profile
cannot reach a namespace outside it.

**Aliases** expose one server under a second namespace with its own tool
subset, description and prelude.

```jsonc
"chrome-peek": {
  "aliasOf": "chrome-devtools",
  "mcpx": {
    "sharing": "exclusive", "scope": "session",
    "tools": ["list_pages", "take_snapshot", "take_screenshot"],
    "description": "read-only view of the same browser"
  }
}
```

Whether an alias shares a *process* with its target depends on whether its
leasing matches. Pool identity covers the command, arguments, environment,
working directory, transport, sharing, scope, maxima and timeouts — everything
that changes the process or how it is handed out. Tool filtering is not in it,
because filtering is presentation.

So the example above shares one browser with `chrome_devtools`: opening a page
through the full view and listing pages through the restricted one shows the
same page, from the same pid. Change the alias's `scope` and it becomes a
separate process instead. `mcpx status` reports a shared pool once, under
every name that reaches it.

Chains are rejected. An alias of an alias is a puzzle, and the error says to
point at the original.

2026-09-27T13:00:00-05:00

## The script contract

```
created:      2026-09-27T14:30:00-05:00
last-updated: 2026-09-27T14:30:00-05:00
increment:    1
status:       standard
tags:         area:scripts, area:logging
description:  stdout is the result, stderr is logs, a default export is the
              entry point, and --json wraps the lot.
```

**stdout is the result. stderr is diagnostics.** A script that prints nothing
to stdout returns nothing.

**A default export is an entry point.** If a module has one, mcpx calls it and
prints whatever it returns; if it does not, top-level code runs on import as
before. One file is therefore both importable and runnable without ceremony:

```typescript
import tools, { log } from "./mcpx-client.ts";

export function countFiles(query: string) { /* importable */ }

export default async function main(args: string[]) {
  log.info("searching for {query}", { query: args[0] });
  return { count: await countFiles(args[0]) };     // printed as JSON
}
```

`main` receives argv as an array, the way every other main does.
`mcpx run --export countFiles script alpha` calls a named export instead, with
arguments **spread** — `--export f a b` reads as `f(a, b)`. Naming an export
that does not exist lists the ones that do.

**Streaming results.** A return value is one answer at the end; `emit()` is
many answers as they are found:

```typescript
for (const file of files) emit({ file, findings: await scan(file) });
```

Each value is written immediately — one JSON line on stdout in ordinary use,
or collected in order into the envelope's `results` array under `--json`. A
script can stream *and* return: the streamed values are the progress, the
return value is the conclusion.

**Logging** is available to scripts and shares the daemon's renderer:

```typescript
log.info("fetched {count} pages in {ms}ms", { count: 3, ms: 412, url });
```

```
19:44:45.911 INFO  fetched 3 pages in 412ms url=https://example.com
```

A message may carry `{placeholders}` filled from the attributes; `{{x}}` writes
a literal `{x}`. Both forms are kept: the interpolated message for reading, the
template for grouping records that differ only in their values. An attribute
consumed by the template is not repeated in the trailing key/value list. A
missing placeholder is left visible rather than blanked, because a hole in a
sentence is a bug worth seeing.

The first argument after the message becomes attributes when it is a plain
object. Anything else, and anything after it, is collected into an `args`
array, so console-style calls keep their values instead of dropping them. An
`Error` is captured as name, message and stack rather than stringified:

```typescript
log.error("upload failed", err);            // err becomes args[0], structured
log.debug("state", { id }, "extra", 42);    // id is an attribute
```

**Call sites** are recorded for `warn` and above by default. Capture costs
about 5 microseconds in a script -- measured, and roughly fifty times the cost
of the record it decorates -- because building the stack trace is the expensive
half. That is worth paying where something went wrong and wasteful on routine
progress, so the default traces the levels you would actually investigate.
`--log-source` alone widens it to everything, `--log-source=error` narrows it,
`--log-source=false` turns it off.

**A filtered call costs nothing.** The script knows the active threshold, so
`log.debug()` below it returns after a comparison: 0.026 microseconds measured,
against 0.10 for the encode-and-write it used to do and 5 for a traced one.
Debug logging can be left in.

**Binding context** works as it does in slog:

```typescript
const scoped = log.with({ run: runId, phase: "scan" });
await scan(scoped, file);
```

The returned logger has to be passed where it is needed. JavaScript has no
ambient context, so bindings do not follow the call stack by themselves; the
alternative would be a hidden global, which is worse than an explicit argument.

Records travel on stderr behind a `U+001E` marker rather than through the
daemon, so logging works with no daemon reachable, costs no round trip, and
cannot reorder against the script's own output. Anything else on stderr passes
through untouched.

**Formats**, on `--format`, for both scripts and the daemon:

| | |
| --- | --- |
| `text` (default) | `19:44:45.911 INFO  fetched 3 pages url=x` |
| `logfmt` | `ts=… level=info msg="fetched 3 pages" count=3` |
| `json` | one object per line, with `msg` and `template` |
| `json-pretty` | indented |
| `compact` | `INFO fetched 3 pages` |
| `bare` | `fetched 3 pages` |

`--log-level` sets the threshold; `MCPX_FORMAT` and `MCPX_LOG_LEVEL` set
defaults. The daemon renders through the same writer, so one choice governs
everything.

**Imports are optional.** The launcher installs the standard surface on
`globalThis` before importing a script, so a one-liner needs no imports:

```typescript
export default async function main() {
  log("starting");                       // log() is log.info()
  emit({ partial: 1 });
  return await fff.grep({ query: "TODO" });
}
```

Importing still works and yields the same objects, which is what an editor
wants. mcpx also writes `mcpx-globals.d.ts` beside the script so a language
server knows the globals exist without an import.

**console is captured.** `console.error`, `warn`, `info` and `debug` become
records, so they get enrichment, formatting and the durable log instead of
being bare text on stderr. `console.log` is left on stdout -- it is the
script's result and redirecting it would change what a caller reads -- but is
*also* recorded, marked file-only so the terminal does not show it twice.
`--no-capture-console` turns all of this off.

**Wrapping a run.** `--prefix` and `--suffix` add lines around a script without
the script knowing. For a file they run in the generated launcher — before the
module is imported and after its entry returns, in a `finally` so cleanup
survives a throw:

```
mcpx run --prefix 'log.info("starting {name}", { name: script.name });' \
         --suffix 'log.info("took {ms}ms", { ms: Math.round(result.ms) });' report
```

Prefix lines see `script` (path, name, args, export); suffix lines also see
`result` (ok, value, error, ms). They can set globals the script reads, but
cannot declare bindings inside its module scope — ESM does not allow it.

Lines layer across configuration: a list element of `null` (or `-` on the
command line) splices in whatever was inherited, so a nearer config can extend
a farther one instead of only replacing it.

`--env KEY=VALUE` sets variables for the run.

**`mcpx --json run`** wraps a whole run in one document — stdout, the parsed
result, captured logs, the script's own stderr, exit code, duration and
runtime. Nothing leaks to the terminal alongside it:

```json
{ "ok": true, "exitCode": 0, "durationMs": 97, "runtime": "deno",
  "result": { "count": 24 },
  "logs": [ { "level": "info", "msg": "searching for flake.nix",
              "template": "searching for {query}", "query": "flake.nix" } ] }
```

2026-09-27T14:30:00-05:00

## Named scripts

```
created:      2026-09-27T04:45:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    1
status:       standard
tags:         area:scripts, area:cli
description:  .mcpx/scripts/<name>.ts, searched upward from cwd then the user
              config directory; nearest wins.
```

`mcpx run <name>` resolves a bare name against, in order:

1. `./.mcpx/scripts/<name>.ts`, then the same path in each parent directory up
   to the filesystem root — so a repo's scripts override a parent's;
2. `$XDG_CONFIG_HOME/mcpx/scripts/<name>.ts`;
3. `~/.config/mcpx/scripts/<name>.ts`.

Anything path-shaped — containing a separator, or ending `.ts`, `.js`, `.mts` —
is used verbatim, so absolute paths and `./local.ts` keep working unchanged.

`mcpx scripts` lists what is reachable, using each file's first comment line as
its description and marking entries shadowed by a nearer copy:

```
NAME       DESCRIPTION                                             PATH
shot-flow  Screenshot a page, click a link, wait for load, scree…  /tmp/demo/.mcpx/scripts/shot-flow.ts
```

Arguments after the name reach the script as `Deno.args`. The working directory
is **yours**, not the script's, so a relative output path means what it says on
the command line.

2026-09-27T05:05:00-05:00

## The generated client

```
created:      2026-09-25T18:10:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    2
status:       core
tags:         area:codegen, area:types
description:  JSON Schema compiled to a typed TypeScript module written beside
              the script, so the runtime's own module cache applies.
```

mcpx compiles each tool's JSON Schema into a TypeScript function and writes the
module to `mcpx-client.ts` next to the script. Enums become literal unions,
nullable types become unions with `null`, `$ref` is resolved, cycles bottom out
in `unknown`, and descriptions become JSDoc.

Writing a real file is the reason scripts start in tens of milliseconds. The
predecessor imported its client over HTTP with `--reload`, which re-downloaded
and re-typechecked the module graph on every run and cost seconds.

Results arrive unwrapped: `structuredContent` parsed, JSON-in-text parsed,
plain text as a string, everything else with `.raw` carrying the full envelope.
`isError` throws a `ToolError` carrying `server`, `tool` and `raw`.

`mcpx client -o <path>` writes the module for a checked-in script so an editor
can type-check it.

2026-09-27T05:05:00-05:00

## Calls without a JavaScript runtime

```
created:      2026-09-25T18:10:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    1
status:       standard
tags:         area:cli
description:  mcpx call runs one tool with no script and no runtime, in ~47ms.
```

```
$ mcpx call fff_nix.find_files '{"query":"flake.nix","maxResults":3}'
$ mcpx --json call codedb.definition '{"symbol":"Registry"}' | jq -r '.content[0].text'
```

About 47 ms, no JS runtime involved. `--raw` prints the full MCP envelope;
`--session <key>` joins an existing lease on a stateful server.

2026-09-27T05:05:00-05:00

## One daemon per config

```
created:      2026-09-26T00:10:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    2
status:       standard
tags:         area:daemon, area:multirepo
description:  the daemon is keyed to the config file it loaded, so repos do not
              share servers and a config edit takes effect immediately.
```

Without this, the first project to run a command would decide which MCP servers
exist for every other project on the machine, and editing a config would leave
a stale daemon serving the old one.

The key is a fingerprint of the config path plus its bytes. Different config,
different daemon. Edited config, new daemon. Auto-started daemons exit after
four idle hours; one started deliberately with `mcpx daemon` or under launchd
stays up.

`mcpx daemons` lists them all, `mcpx stop --all` clears them.

On macOS the unix socket falls back to a short hashed path under `$TMPDIR` when
the state directory would exceed the 104-byte `sun_path` limit, because the
failure mode is otherwise an opaque `bind: invalid argument`.

2026-09-27T05:05:00-05:00

## Runtime portability

```
created:      2026-09-25T19:40:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    2
status:       standard
tags:         area:runtime
description:  deno, then bun, then node; the generated client avoids syntax any
              of them reject.
```

`--runtime deno|bun|node` overrides detection; `"runtime"` in the config sets a
default. Deno runs with `--no-check`, since the generated client is
machine-written and a type error in the agent's own script surfaces at runtime
anyway.

The client is written to avoid TypeScript that Node's strip-only mode rejects.
A cross-runtime test caught constructor parameter properties failing on Node
only, so the client uses plain fields. A second test asserts session isolation
holds on all three, because a runtime that cannot read `MCPX_SESSION` would
silently re-share every stateful server.

2026-09-27T05:05:00-05:00

## Remote servers without a shim

```
created:      2026-09-25T18:10:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    1
status:       standard
tags:         area:transport
description:  Streamable HTTP and SSE are spoken directly; no mcp-remote
              process in between.
```

```jsonc
"context7": { "url": "https://mcp.context7.com/mcp", "transport": "http" }
```

Connects in about 800 ms with no Node shim process. Batched frames and SSE
streams are both handled; `Mcp-Session-Id` is tracked and released with a
`DELETE` on shutdown. OAuth is not implemented — see below.

2026-09-27T05:05:00-05:00

## Honest failure reporting

```
created:      2026-09-26T04:30:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    2
status:       standard
tags:         area:diagnostics
description:  a server that will not start is reported with its stderr instead
              of vanishing from the namespace list.
```

```
codebase_memory  0  shared  0  error  server "codebase-memory": initialize: mcp error -32000: con…
```

`mcpx status` has the full text, including the child's stderr tail. Start
failures enter an exponential cooldown capped at 30 s so a broken server cannot
spin, and a failed schema fetch deliberately does **not** record a cache
timestamp — otherwise a server that never started would persist as "cached,
0 tools" and the error would be lost across restarts.

Child processes are killed by process group, so browsers do not outlive the
daemon.

2026-09-27T05:05:00-05:00

## Search

```
created:      2026-09-25T18:10:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    2
status:       standard
tags:         area:discovery
description:  ranked tool search, all-terms first with an any-term fallback.
```

Exact name match scores 100, prefix 60, substring 40, function path 30,
namespace 20, description 10. Every term must match by default, which narrows
well; when that returns nothing the query is retried as any-term, so
`mcpx search screenshot click` returns both tools rather than neither.

`-n <count>` caps results, default 20.

2026-09-27T05:05:00-05:00

## Lifecycle and operations

```
created:      2026-09-25T18:10:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    2
status:       standard
tags:         area:ops
description:  status, restart, refresh, stop, daemons, and the trace switch.
```

| Command | Effect |
| --- | --- |
| `mcpx status [-v]` | daemon, pools, live instances, pids, call counts, sessions |
| `mcpx restart [<ns>]` | stop instances; the next call starts fresh ones |
| `mcpx refresh` | re-read every server's schemas |
| `mcpx daemons` | every daemon for this user |
| `mcpx stop [--all]` | shut down this config's daemon, or all |
| `mcpx daemon --port N` | run in the foreground |
| `mcpx config [--path]` | resolved configuration |
| `mcpx init [--global]` | starter config |

Idle instances are reaped on a 30-second timer against each server's
`idleTimeout`. `MCPX_TRACE=1` logs one line per tool call naming the instance
that served it. `mcpx stop` flushes the schema cache **before** closing its
listeners, so a stop followed immediately by a directory removal cannot race.

2026-09-27T05:05:00-05:00

## Reading the log

```
created:      2026-09-27T21:00:00-05:00
last-updated: 2026-09-27T21:00:00-05:00
increment:    1
status:       standard
tags:         area:diagnostics
description:  mcpx log and mcpx stats, over a SQLite index of the JSONL files.
```

Every record the daemon and every script produce is already written as JSON
lines under the state directory. That file is the record of truth and nothing
else writes to it. Beside it sits `index.db`, a SQLite index built from it, and
the only reason it exists is that questions like "which tool is slow" are
queries, not greps.

The index is rebuilt by reading, not by a background thread. `mcpx log` and
`mcpx stats` ingest before they answer, and ingest is incremental: a file whose
size and mtime have not moved is not opened at all. A background indexer would
be faster in the rare case and quietly wrong in the bad one, and an index
nobody trusts is worse than no index. Deleting `index.db` costs a rescan and no
data.

```
mcpx log --since 1h --level warn          recent trouble
mcpx log --event server.* --server fff    one server's lifecycle
mcpx log --grep 'timed out' --limit 50    regular expression over msg and attrs
mcpx log -f                               tail as records arrive
mcpx log --chain cal-9f3c...              a call and everything that led to it
```

`--chain` is the one worth knowing. A record carries the id of the thing it
happened inside, and only the record that *creates* something carries its
parent, so walking backwards from a tool call reaches the server instance that
served it and then the daemon that started that. It prints as an indented tree,
oldest first.

`mcpx stats` aggregates the same index. `calls` groups by server and tool with
exact p50/p95/p99 — exact, not sketched, because these are thousands of rows
and an approximation would trade the one property that matters, that the number
printed is a call which really took that long. `slowest` prints individual
calls with their trace ids, which is where `--chain` comes from. `servers` and
`instances` cover process lifecycle, aggregate and one row per process
respectively; `errors` groups on the message *template* rather than the
interpolated text, so one recurring failure is one row; `sessions` says what
each script run did; `volume` is records per level per hour plus what the logs
cost on disk.

`mcpx log sql '<select ...>'` is the escape hatch, with `--schema` for the DDL
and `--path` for the file. Anything that is not a read is refused. That is a
guard rail rather than a security boundary: the index can be deleted and
rebuilt at will, so it is protecting someone who typed DELETE meaning SELECT,
not defending against anyone.

The driver is `modernc.org/sqlite`, a pure-Go translation of SQLite. A cgo
driver would be faster and would also make the Nix build need a C toolchain and
stop cross-compiling, which is a poor trade for an index.

2026-09-27T21:00:00-05:00

## Configuration

```
created:      2026-09-25T18:10:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    1
status:       standard
tags:         area:config
description:  the same mcpServers object other MCP hosts use, plus an optional
              per-server mcpx block.
```

Searched upward from the working directory: `.mcpx.json`, `.mcpx/config.json`,
`.mcp.json`; then `$XDG_CONFIG_HOME/mcpx/config.json`, `~/.config/mcpx/config.json`,
`~/.mcpx.json`, `/etc/mcpx/config.json`. `MCPX_CONFIG` overrides everything.
JSONC comments are stripped, string-literal aware.

Per server: `mode`, `max`, `min`, `idleTimeout`, `callTimeout`, `startTimeout`,
`namespace`, `description`, `tools`, `extraIncludeTools`, `excludeTools`, `disabled`. A top-level `pool`
block applies any of them to every server. The three tool lists take exact
names, globs (`delete_*`) or `/regexps/`, and hide a tool from listings and calls
alike; see [Hiding tools](configuration.md#hiding-tools).

2026-09-27T05:05:00-05:00

## One declaration per setting

```
created:      2026-09-28T02:30:00-05:00
last-updated: 2026-09-28T02:30:00-05:00
increment:    1
status:       standard
tags:         area:config
description:  a setting is described once and is then readable from a file, a
              variable and a flag.
```

`internal/settings/registry.go` holds every knob mcpx has. Each entry states a
type, a default, a name, a sentence and a paragraph. From that one description
the setting becomes readable from a configuration file at its dotted path,
from a generated `MCPX_` variable, and from a generated flag. There is no
wiring, so there is nothing to forget to wire.

What this replaces had a flag in one file, a default in another, an
environment lookup in a third and a struct field in a fourth. That has one
failure mode and it happened every time: the four drifted, and "what is this
set to" became "read all four and guess". It also meant settings that were
documented but unreachable -- the plumbing switches could be set from the
environment and nowhere else, so a configuration file could describe a value
it could not apply.

Defaults are written as strings in the syntax a user would type and go through
the same validator as an override, so a default cannot be invalid.

Precedence runs defaults, then configuration files with the nearest last, then
the environment, then flags. Every value remembers what it overrode, because
"why is this not what my config says" is the most common configuration
question and the answer is now in the value itself.

Two spellings of one setting at the same level is an error. On a command line
the same spelling twice is not -- that is a person editing their own command,
and the last one is what they meant. In the environment two variables
disagreeing genuinely cannot be ordered, so it is refused rather than guessed.

`mcpx config --schema` prints the lot. `--plumbing` adds the internals.

2026-09-28T02:30:00-05:00

## Plumbing

```
created:      2026-09-28T02:30:00-05:00
last-updated: 2026-09-28T02:30:00-05:00
increment:    1
status:       standard
tags:         area:config
description:  internal switches for decisions that could reasonably go either
              way.
```

Settings under `plumbing.` control internals. They work, they are documented,
and there is no ordinary reason to change one.

They exist because the code has guards that could defensibly go the other way.
Whether a directory may stand in for a source string. Whether a bare argument
naming a file is read as one. Whether `foo.ts` beside `foo.js` is an error.
Rather than decide permanently and leave the other half of the world stuck,
each guard reads a switch.

The cost is a longer list. The benefit is that nobody has to patch the binary
to get past a decision that was never meant to be final.

They are hidden from ordinary help and left out of shell completion, because
offering an internal in a tab list is how somebody sets one by accident.

2026-09-28T02:30:00-05:00

## Source that can be a file

```
created:      2026-09-28T02:30:00-05:00
last-updated: 2026-09-28T02:30:00-05:00
increment:    1
status:       standard
tags:         area:scripts
description:  every string-shaped input also accepts a path, or a directory.
```

`--prefix`, `--before`, `--on-error`, `--launcher` and the rest accept inline
source, a path to a file, or a directory whose files are concatenated.

The reason is that a snippet does not stay one line. The moment it grows,
keeping it in a command is unpleasant and keeping it in JSON is unreadable.
Making each setting accept either form costs one resolver; forcing the choice
up front costs a rewrite at exactly the point the answer becomes obvious.

Detection is by probe: a value naming something on disk is read as it. `@text:`
and `@file:` force the interpretation for the case where a snippet genuinely
collides with a filename, and `plumbing.sourceProbePaths` turns the probe off.

A directory is concatenated in natural order, so `9` comes before `10`. Byte
order gets that backwards, which is wrong for precisely the case the feature
serves -- fragments numbered to control their order. Each file is named in a
comment above its contents, because a stack trace into a concatenation is
otherwise unattributable.

2026-09-28T02:30:00-05:00

## The launcher is replaceable

```
created:      2026-09-28T02:30:00-05:00
last-updated: 2026-09-28T02:30:00-05:00
increment:    1
status:       standard
tags:         area:scripts
description:  a template with named holes, replaceable wholesale or removable
              entirely.
```

A script runs inside a generated launcher. That launcher is a template with
named holes:

```
@header @globals @console @before @prefix @import @entry @onSuccess @onError @suffix
```

`--launcher` replaces it with source or a file. `--no-launcher` removes it
entirely: the script is handed to the runtime with nothing installed, nothing
captured and nothing wrapping the error.

Fills may reference each other, which is what makes rearrangement possible
rather than just substitution -- a suffix ending in `@prefix` runs the prefix
again on the way out. That also makes cycles possible, so the reference graph
is checked before anything is substituted and a loop is reported by naming the
path that closed it. Left unchecked it would be a hang, found by waiting.

A placeholder resolving twice is an error unless named in
`plumbing.launcherPlaceholderRepeat`, because the usual cause is a mistake that
silently doubles an effect. The entry block is braced so a permitted repeat
genuinely runs twice -- allowing a repeat and then emitting code that cannot
parse would be worse than refusing it.

A misspelled placeholder is caught by near-miss comparison, including
transpositions, because `@entyr` for `@entry` is a swap and plain edit distance
scores that as two changes. Left in place it becomes a syntax error from the
runtime pointing at a line the user did not write.

`--launcher` takes a required value and `--no-launcher` is separate. An
optional-value flag reads better, but Go implements that only by treating the
flag as boolean, and then `--launcher mine.ts` silently runs `mine.ts` as the
script with no launcher at all. That was found by testing rather than reading.

2026-09-28T02:30:00-05:00

## Search paths

```
created:      2026-09-28T02:30:00-05:00
last-updated: 2026-09-28T02:30:00-05:00
increment:    1
status:       standard
tags:         area:config
description:  lists with a splice point, accepting files as well as
              directories.
```

`paths.scripts` and `paths.config` are lists. A null entry stands for the
built-in locations, so `["./mine", null]` searches yours first and then the
usual places without restating them. A list without a null replaces outright,
which is what most people mean most of the time.

An entry may name a file rather than a directory. Somebody with one script in
an odd place should be able to point at it without inventing a directory to
hold it.

A name matching both `foo.ts` and `foo.js` is refused. Which one runs was a
coin flip, and preferring one quietly means an edit to the other does nothing
with no indication why. `plumbing.allowTsJsOverlap` permits it, and then `.ts`
wins, because a project holding both is almost always compiling one into the
other.

When a script is not found, the error prints the path that was actually
searched, marking what was missing and what was a file. That is the question
being asked.

2026-09-28T02:30:00-05:00

## Checking before running

```
created:      2026-09-28T02:30:00-05:00
last-updated: 2026-09-28T02:30:00-05:00
increment:    1
status:       standard
tags:         area:scripts
description:  everything knowable is checked before a server starts.
```

A malformed `--env` pair, an unreadable hook, a log directory that is a file --
all knowable before any work begins, all previously found later and more
expensively. A bad `--env` pair was silently ignored, so the script ran without
the variable and behaved as though it had never been asked for.

Everything is checked in one pass, so a run with three bad paths reports three
problems rather than the first and then two more runs. A missing writable
directory is created rather than refused, because the intent is unambiguous. A
missing optional path is a warning, because refusing to start over an empty
script directory would make every fresh checkout noisy.

`--typecheck` resolves and checks the generated program without running it,
which answers "will every import resolve" before the servers are up. Off by
default because it costs a second or two on a cold module cache; worth turning
on for anything scheduled, where the cost is irrelevant and a broken import at
three in the morning is not. Under Node and Bun it reports that it cannot run
rather than pretending it did, because those strip types instead of checking
them.

2026-09-28T02:30:00-05:00

## What the console does and does not capture

```
created:      2026-09-28T02:30:00-05:00
last-updated: 2026-09-28T02:30:00-05:00
increment:    1
status:       standard
tags:         area:logging
description:  console is redirected, faithfully; raw byte writes are not.
```

All twenty-five console members are handled. Those carrying a level become
that level. `console.log` still reaches stdout, because stdout is the script's
result. `trace` carries structured frames, `clear` resets indentation without
erasing a durable log, and the devtools markers become file-only records rather
than vanishing.

The test that matters compares a script's output under capture against the same
script run with `--no-launcher`, line for line. Not "does it log" but "is it
the same". That formulation is the only one that cannot drift, and it caught
format specifiers: `console.info("%s scored %d", name, n)` was rendering as the
template beside its arguments instead of substituting them.

Names are preserved, so `console.info.name` is still `"info"`. Messages use the
runtime's own inspect, so `console.info({a:1})` reads `{ a: 1 }` exactly as it
would unwrapped -- the structured value is already in `args`, so the message is
free to be the human form.

`console.createTask()` throws when called bare. So does Deno's. The test
compares against the unwrapped console rather than asserting nothing throws,
because matching the original includes matching its failures.

**Raw writes are not captured.** `Deno.stdout.write` and `Deno.stderr.write`
go straight out. A script reaching for bytes has asked for bytes, and wrapping
them in records would be the wrong answer. Use `console` or `log` for anything
meant to be recorded.

2026-09-28T02:30:00-05:00

## Structured stack traces

```
created:      2026-09-28T02:30:00-05:00
last-updated: 2026-09-28T02:30:00-05:00
increment:    1
status:       standard
tags:         area:logging
description:  frames as data, from V8, in all three runtimes.
```

`Error.prepareStackTrace` is V8-specific and in no standard, and works in Deno,
Node and Bun alike. It hands back call sites instead of a formatted string, so
frames arrive as data: function, file, line, column, and the async and native
flags. Parsing the string form loses those flags and breaks on any path
containing the characters the format uses as delimiters.

One trap is worth recording. **V8 memoises whatever `prepareStackTrace`
returned the first time `.stack` is read.** Asking for frames destroys the
string; asking for the string destroys the frames. Both are wanted, so frames
are captured and the string is rendered from them. Two attempts at this each
lost one form before a test asserted both survive.

`captureFrames(skip, limit)` and `errorFrames(err)` are available to scripts.

2026-09-28T02:30:00-05:00

## Cancellation reaches the transport

```
created:      2026-09-28T03:40:00-05:00
last-updated: 2026-09-28T03:40:00-05:00
increment:    1
status:       standard
tags:         area:transport
description:  a call timeout can interrupt a hung HTTP server.
```

The Streamable HTTP transport detached its POST from the caller's context.
The intent was presumably that a request in flight should finish rather than
be abandoned half-processed, which sounds reasonable and was wrong.

`Send` is synchronous. A server that accepts the connection and never answers
blocks inside it, *before* the caller reaches the select that watches for a
timeout. Detaching the request did not make cancellation best-effort, it
removed it: `pool.callTimeout` could not interrupt a hung server at all, and
the only bound left was the transport's own ten-minute ceiling.

The request now carries the caller's context. Protocol-level cancellation is
unaffected, because `notifications/cancelled` is deliberately sent on its own
background context so that it outlives the request it cancels.

The test blocks a server and asserts `Send` returns. It fails on the previous
code after five seconds and passes in under one.

2026-09-28T03:40:00-05:00

## Where a setting is read

```
created:      2026-09-28T04:30:00-05:00
last-updated: 2026-09-28T04:30:00-05:00
increment:    1
status:       standard
tags:         area:config
description:  what the registry governs, and what still has its own path.
```

Declaring a setting and validating it is not the same as applying it. A value
that is accepted, reported as fine, and then ignored is documentation that
lies more quietly than a missing flag does.

These come from the resolved set, and therefore work identically from a file,
a variable or a flag:

- `logging.format`, `logging.level`, `logging.source`, `logging.dir`
- `catalog.budget`, `catalog.bias`
- `script.typecheck`
- `paths.scripts`
- `plumbing.allowTsJsOverlap`

Pool settings are read by the configuration loader rather than the registry,
because they are per-server and the server's own block has to win. `pool` and
`pool` are two spellings of one block: `pool` reads naturally beside
`mcpServers`, `pool` is what `mcpx config --schema` calls it. Both exist
because having one of them silently do nothing would be worse than having two.
Where both appear, `pool` wins -- it is the older spelling, and adding a
synonym should not change what an existing file means.

Still on their own paths, and honestly so: `output.json` (the global `--json`
flag is read before any command), `paths.state` and `paths.cache` (needed
before settings can be resolved, since they say where to look), and most of
`daemon.*`.

2026-09-28T04:30:00-05:00

## Placeholders that take arguments

```
created:      2026-09-28T07:30:00-05:00
last-updated: 2026-09-28T07:30:00-05:00
increment:    1
status:       standard
tags:         area:scripts
description:  @name(a, b), and @names a user declared.
```

A placeholder may take arguments:

```
@console(header, prefix)
```

An argument naming a placeholder expands to it; anything else is literal.
They bind inside the body only, so two uses of one placeholder cannot
contaminate each other. Inside, they are addressable by the names the
declaration gave them, and always as `@1`, `@2` and `@args`.

This is the difference between a template that can be rearranged and one that
can only be filled in.

A file may declare the `@name` it provides:

```ts
// @mcpx:placeholder timed(body, label)
{
  const __t = performance.now();
  @body
  log.info("timing", { label: @label, ms: Math.round(performance.now() - __t) });
}
```

Three spellings, because the right one depends on the file. A comment works in
any language and cannot affect runtime. An exported `MCPX_PLACEHOLDER`
constant is visible to tooling. The filename is the least ceremony for a
directory of one-line fragments.

Shadowing a built-in is refused: a template that reads correctly and means
something else is the worst kind of surprise. Two files claiming one name is
refused for the reason two config keys are -- there is no order between them.

`paths.placeholders` is empty by default, because scanning every script
directory for declarations would make an ordinary script's filename quietly
meaningful.

The single-resolution rule composes with this. If `@timed` uses `@entry` and
the template also uses `@entry` directly, that is a repeat and is refused
unless named in `plumbing.launcherPlaceholderRepeat`.

2026-09-28T07:30:00-05:00

## The harness knows what the agent does not

```
created:      2026-09-28T07:30:00-05:00
last-updated: 2026-09-28T07:30:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  session identity arrives through the environment, not the model.
```

mcpx leases servers per session. That only works if it can tell sessions
apart, and nothing in a shell command carries that: the agent does not know
its own session id, and asking it to pass one would spend tokens on plumbing
and be forgotten half the time.

So the harness supplies it. `plugin/opencode/mcpx-session.ts` puts a handful
of variables into every shell command; mcpx reads them and the agent never
learns any of it happened.

`MCPX_TRACE_IDS` is a list of pairs rather than a flat id, because a flat one
cannot say "this session, whose parent is that one, in this worktree". An
entry longer than a pair names several ids for one key. Unknown keys are kept,
since the list exists to grow, and they land on every record as `id.<key>`.

They cannot overwrite what mcpx established. A caller may add context; it may
not rewrite which session a call was actually leased for, or leasing becomes
advisory.

Everything past the session id is opt-in. Environment variables are not
context -- the model never sees them -- so anything cheap goes in on the
reasoning that an unread variable is cheaper than a missing one. Transcript
lengths and token counts stay out: each would be a database query on every
shell command, for a number almost nobody reads. Those come from
`mcpx stats --opencode`, which asks once.

2026-09-28T07:30:00-05:00

## Statistics from opencode

```
created:      2026-09-28T07:30:00-05:00
last-updated: 2026-09-28T07:30:00-05:00
increment:    1
status:       standard
tags:         area:inspection
description:  read-only reporting over opencode's own database.
```

`mcpx stats --opencode` reports overview, agents, models, projects, busiest
sessions and activity. What a run cost, which model answered, how much was a
subagent's -- those live in opencode's database and are pruned over time, so
folding them in beside mcpx's own log means one place to ask.

The database is opened read-only and belongs to a program that may be running.
`mode=ro` rather than `immutable=1`, deliberately: immutable would permit
reading a torn page from a live writer. Every statistic checks for the columns
it needs, because that schema is not mcpx's to depend on -- one that has moved
should cost a number, not the command.

The model column holds JSON rather than a name, and it is decoded in Go rather
than with `json_extract`, which is present in most SQLite builds and absent in
enough of them to be worth avoiding.

2026-09-28T07:30:00-05:00

## The explorer

```
created:      2026-09-28T07:30:00-05:00
last-updated: 2026-09-28T07:30:00-05:00
increment:    1
status:       standard
tags:         area:ux
description:  a prompt loop over the discovery commands, for a human.
```

Everything `mcpx explore` shows is available elsewhere. It exists because
discovery is a loop -- list namespaces, look at one, read a signature, try it,
see what the log said -- and running four commands with different flags to go
round it once is enough friction that people stop and guess instead.

Deliberately not full-screen. A pane interface needs a terminal library, raw
mode, resize handling and a redraw loop, and loses the two things a plain
prompt gives for free: output stays in scrollback where it can be copied, and
every screen corresponds to a command that can be scripted. The footer prints
that command, so the tool teaches its own non-interactive form rather than
being a place where knowledge stops.

A bare word naming a namespace shows its signatures, because that is what
somebody means nine times in ten. `chain` with no argument lists recent
traces, because needing another command to find this one's argument is exactly
the friction being removed.

The terminal check asks the kernel for terminal attributes rather than testing
`ModeCharDevice`. That test is wrong in the direction that matters:
`/dev/null` is a character device, so redirecting stdin from it read as
interactive and blocked on a prompt nobody was there to answer.

2026-09-28T07:30:00-05:00

## The browser

```
created:      2026-09-28T09:00:00-05:00
last-updated: 2026-09-28T09:00:00-05:00
increment:    1
status:       standard
tags:         area:ux
description:  three panes, so comparing two tools is a keystroke.
```

`mcpx tui`, `mcpx --tui`, or just `mcpx` when there is a terminal to draw on.

Three panes -- namespaces, the selected namespace's tools, one signature --
so the question "which of these two tools do I want" is a cursor move rather
than two commands and a scrollback hunt. Moving the cursor loads the next
pane; nothing needs pressing.

`L` switches to the log. `r` refreshes. `/` filters either list.

### Why both this and the prompt

`mcpx explore` and `mcpx tui` answer differently shaped questions. The prompt
is better when you know what you want and will paste the result somewhere: its
output stays in scrollback and every screen names the command that produced
it. The browser is better when you do not know yet, because three panes hold
more context than a scrolling transcript.

The prompt also works where the browser cannot run -- over a pipe, in CI,
inside another program -- so neither is a worse version of the other.

### Six views

`]` and `[` move between them; the header names them all with the current one
marked, because a view nobody knows exists is a view nobody uses.

- **tools** -- namespaces, their tools, one signature
- **log** -- every record, newest last
- **stats** -- calls, servers, errors, sessions, slowest, volume; `d` cycles
- **servers** -- individual processes, with pid, uptime and why each stopped
- **sessions** -- what mcpx saw merged with what opencode recorded
- **storage** -- what all of it costs on disk

Five of the six are grids, rendered through one table type. A new view is
therefore a query rather than a widget, and every view gets selection,
truncation and drill-down without restating any of it.

**sessions** is the one that needed both halves. mcpx knows which servers a
session used; opencode knows what it cost. Neither alone is the row anybody
wants, and a machine without opencode still gets mcpx's half rather than an
error.

**storage** exists because it answers a question that arrives suddenly --
something is large and it is not obvious what -- and answering it otherwise
means knowing where four different things live.

### Opening a row

Enter, or a click. A log line is truncated to fit a terminal, and the part cut
off is usually the part being looked for: a stack, a full path, a nested
result. Expanding it is the difference between the log being browsable and
being a place to notice that something exists before going to another command
to read it.

Multi-line strings are indented rather than escaped, since escaping them is
what made them unreadable in the row.

### The dependency

bubbletea, bubbles and lipgloss. They cost 1.2 MB in the stripped binary,
taking it from 11.2 to 12.4 MB, and replace what would otherwise be hand
written escape sequences, resize handling and a redraw loop. That is code
which is never finished and never correct on every terminal.

Mouse support is on, in cell-motion mode: clicks and the wheel, without an
event per pixel of movement.

Capturing the mouse does take over text selection. Every terminal worth using
restores it on shift-drag, which is the convention, and being unable to click
a log line open is a worse trade than learning one modifier.

### Testing a full-screen program

The view reads from an interface, not from the daemon client, so a fake drives
it in tests. A terminal program that cannot be tested is one that breaks
quietly, and the failures worth catching are not visual: that moving the
cursor loads the right namespace, that a late reply for a namespace the cursor
has already left is dropped rather than shown, and that keys go to the filter
while it is open -- without that last one, typing a namespace containing "q"
quits.

2026-09-28T09:00:00-05:00

## mcpx speaks MCP

```
created:      2026-09-28T11:00:00-05:00
last-updated: 2026-09-28T11:00:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  the inversion: mcpx as a server, not only a client.
```

mcpx exists so tool schemas never reach a model's context. A host that speaks
only MCP -- a different editor, a hosted agent, anything that is not opencode
-- could not use any of that.

So `mcpx serve` offers mcpx itself as an MCP server: ten tools reaching every
server it knows about, with the schemas still on this side of the wire.

The surface is small on purpose. Exposing three hundred tools over MCP would
rebuild the problem mcpx was built to solve, with extra steps. `mcpx_exec` is
the one that matters: it runs TypeScript next to the servers and only what it
prints comes back.

stdio and Streamable HTTP, plus a plain POST per tool at `/v1/tools/<name>`.
Offering only JSON-RPC would make mcpx reachable from MCP hosts and from
nothing else, which is the opposite of the point -- a shell script with curl
should ask the same questions an agent does.

2026-09-28T11:00:00-05:00

## Command-line programs as servers

```
created:      2026-09-28T11:00:00-05:00
last-updated: 2026-09-28T11:00:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  a declaration, not a wrapper process.
```

An enormous amount of capability already exists as command-line programs.
Writing an MCP server to wrap one is a day's work producing a process whose
only job is to shell out, and there are hundreds of such programs.

`paths.adapters` points at declarations instead:

```jsonc
{ "adapters": [{
  "name": "gitx", "command": "git",
  "tools": [
    { "name": "log", "args": ["log", "--oneline"],
      "params": [{ "name": "limit", "flag": "-n", "type": "integer", "default": "10" }] }
  ] }] }
```

That is git as an MCP server. It appears in `tools/list`, it is callable from
a script as `await gitx.log({ limit: 5 })`, and `mcpx adapter check` says
whether the binary is even installed. The daemon runs each adapter as a server
of its own -- `mcpx adapter serve gitx`, spawned like any upstream -- so it is
a namespace in `mcpx ls`, `types`, `search`, `catalog`, `/v1/call` and the
generated client, with a signature typed from the declared `params`. An
adapter cannot share a name with a configured server.
`internal/e2e/toolparity_test.go` holds every tool in `tools/list` to being in
the generated client, and calls one from a script.

**Not a shell escape.** A tool is a named subcommand with declared parameters,
so a model cannot invent a command line and what is reachable is exactly what
somebody wrote down.

Two details that are wrong in the obvious implementation. A boolean parameter
becomes the flag's presence, because `--verbose true` is wrong for almost
every program ever written. And a non-zero exit is a result rather than an
error: the program ran and said something, and deciding that is a failure
belongs to whoever asked.

2026-09-28T11:00:00-05:00

## Finding servers that are not configured yet

```
created:      2026-09-28T11:00:00-05:00
last-updated: 2026-09-28T11:00:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  the official registry, and why not HAPI.
```

`mcpx registry search weather` asks a registry what exists. `mcpx registry add
<name> --write` puts it in the configuration file.

### Which registry

The official MCP Registry, at `registry.modelcontextprotocol.io`. It is
unauthenticated, returns real data, and -- the part that matters -- publishes
an OpenAPI specification that other registries implement. Writing against the
specification rather than against one host means the same code reaches the
official registry, a vendor's subregistry, and whatever an organisation runs
internally to control what its agents may install. `registry.url` points it
anywhere.

HAPI was the other candidate and is not usable: the framework is not open
source and its public API returns 404.

### Translating an entry

A registry entry says what a server is, not how this machine should run it.

Remotes are preferred when offered, because nothing is installed and nothing
runs locally. Otherwise the package becomes an ephemeral runner -- `npx -y`,
`uvx`, `docker run --rm` -- since a server pinned in a config file should not
also require the machine to have been prepared. A publisher's own runtime hint
wins over that table, because they know something about their package that a
table does not.

npm versions are pinned. A configuration that silently upgrades is one that
breaks on a morning nobody changed anything.

**Secrets are never invented.** The registry says which variables a server
requires; those are reported so the caller can set them, because a server that
exits immediately for a missing key looks broken rather than unconfigured.

The reverse-DNS name is stripped to its last component, so
`io.github.microsoft/playwright-mcp` becomes `playwright_mcp` rather than
making every call read `io_github_microsoft_playwright_mcp_navigate`.

2026-09-28T11:00:00-05:00

## Specifications as tools

```
created:      2026-09-28T13:00:00-05:00
last-updated: 2026-09-28T13:00:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  OpenAPI documents become callable tools.
```

```sh
mcpx api tools --spec https://petstore3.swagger.io/api/v3/openapi.json
mcpx api call petstore_findpetsbystatus '{"status":"available"}' --spec ...
```

An enormous amount of capability is already described by a specification
somebody else maintains. A specification is a better source than a
hand-written wrapper for the same reason a generated client is better than a
hand-written one: it is already correct, already complete, and it changes when
the service does.

`paths.apis` declares them permanently, and the operations appear over MCP
beside everything else.

### Two things easy to get wrong

**Relative server URLs.** The Swagger petstore declares `/api/v3`, which must
resolve against wherever the document was fetched. Unresolved it produces a
request with no scheme, and the failure reads as a network problem rather than
an unresolved reference.

**Only reads, by default.** A specification describes what a service *can* do,
not what you meant to allow. `--methods all` exposes the rest; the difference
between listing orders and cancelling them should be a deliberate keystroke.

Deprecated operations are skipped unless asked for, and labelled when included.
Path parameters are escaped, because a slash in one silently changes which
endpoint is called.

2026-09-28T13:00:00-05:00

## Finding a tool without leaving the script

```
created:      2026-09-28T13:00:00-05:00
last-updated: 2026-09-28T13:00:00-05:00
increment:    1
status:       standard
tags:         area:scripts
description:  search() and describe(), inside the program.
```

```ts
for (const hit of search("grep")) {
  console.log(hit.namespace + "." + hit.tool, hit.required);
}
console.log(describe("fff_nix.grep"));
```

Without these, discovering a tool means ending the script, running
`mcpx search`, reading the result and writing a new script. That round trip is
the expensive part: for a model it is a whole turn, and the intermediate
result passes through its context on the way.

Synchronous, because everything they search is already in the generated
client. A promise would only be a promise of work already done.

A word matching a tool's name scores higher than one matching its prose, so a
tool called `grep` beats one whose description merely mentions grepping.

2026-09-28T13:00:00-05:00

## The half of MCP that is not tools

```
created:      2026-09-28T15:00:00-05:00
last-updated: 2026-09-28T15:00:00-05:00
increment:    1
status:       standard
tags:         area:protocol
description:  prompts and resources, which mcpx reported as empty.
```

A server publishes three things: tools, resources and prompts. mcpx handled
tools, read resources only when asked by URI, and reported **no prompts at
all** -- while its MCP server declared the capability and returned an empty
list.

That last part is the worst kind of wrong, because a client cannot detect it.
It asks once, receives nothing, and never asks again.

```sh
mcpx prompts                              # what servers publish
mcpx prompts demo.summarise text="..."    # render one
mcpx resources                            # what exists
mcpx resources demo/demo://greeting       # read one
```

Prompts are a server saying "here is the wording that works for this" rather
than "here is a function". A server publishing a good one has encoded
expertise that would otherwise be rediscovered by whoever writes the request.

Both pass through the MCP server, namespaced -- two servers may publish the
same URI, and a caller otherwise has no way to say which it meant.

A server that does not support prompts answers method-not-found. That is an
absence, not a failure, so it is treated as an empty list; the alternative
makes every listing fail on the majority of servers.

Binary resources are described rather than inlined. A megabyte of base64 in a
model's context is the failure this whole tool exists to prevent.

2026-09-28T15:00:00-05:00

## mcpx doctor

```
created:      2026-09-28T15:00:00-05:00
last-updated: 2026-09-28T15:00:00-05:00
increment:    1
status:       standard
tags:         area:ux
description:  one command for "it does not work".
```

mcpx has a daemon, a runtime, a configuration chain, a log index, adapters,
registries, API specifications and a plugin. "It does not work" stopped being
a question with one answer some time ago.

`mcpx doctor` checks each in the order somebody would have to know to check
them by hand: the runtime, git, whether the settings contradict each other,
whether the configuration parses, **whether every server's command is actually
installed**, the directories, the daemon, the log index, and the optional
integrations.

That server-command check is the important one. It is the most common cause of
"mcpx does not work" and it is invisible until something tries to call the
server, at which point the error arrives from three layers down.

Every check says what to do about it. One that only reports a problem leaves
the reader exactly where they started.

### It found a bug immediately

The unknown-key warning was reporting `mcpServers`, `logging`, `paths` and
every server name as unrecognised. They are not: a section is the path to a
setting, and `mcpServers` is the document's own content. The warning listed
everything, which made a real typo invisible among the noise -- so it had been
useless since the day it was written, and nothing displayed it until now.

2026-09-28T15:00:00-05:00

## Types, in whichever form a consumer reads

```
created:      2026-09-28T16:30:00-05:00
last-updated: 2026-09-28T16:30:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  four renderings of one fact.
```

```sh
mcpx schema --format typescript    # what a script imports
mcpx schema --format json-schema   # what a validator reads
mcpx schema --format openapi       # what a client generator reads
mcpx schema --format mcp           # what an MCP host reads
```

The same information every time. A caller should not have to reshape one into
another, and each of those four consumers is real.

JSON Schema goes under `$defs`, which is where a reader looks for named
subschemas, so the result is itself a valid schema document rather than a bag
of them.

### The specification described the wrong thing

`mcpx openapi` described mcpx's own ten tools and nothing else. The three
hundred it actually fronts were reachable from MCP, and from a script, and
from nowhere a generated client could see.

A specification that describes the wrapper but not what it wraps is a
specification of the wrong thing. Every upstream tool now has a path:

```
POST /v1/call/<namespace>/<tool>
```

Served as well as described -- `curl -X POST .../v1/call/fff/grep -d '{"query":"x"}'`
reaches the same server a script does. The document is generated per request
rather than at startup, so a server that appears later is described without a
restart.

2026-09-28T16:30:00-05:00

## Two protocol eras

```
created:      2026-09-28T20:00:00-05:00
last-updated: 2026-09-28T20:00:00-05:00
increment:    1
status:       standard
tags:         area:protocol
description:  legacy and modern, both directions.
```

The specification splits implementations in two. Legacy revisions
(`2025-11-25` and earlier) negotiate once through an `initialize` handshake.
`2026-07-28`, the current one, carries the version in `_meta` on every
request, has no handshake at all, and requires `server/discover`.

The matrix is unforgiving: modern against legacy fails, legacy against modern
fails, only a dual-era implementation bridges. mcpx is dual-era on both sides.

**As a client** it probes and falls back. Legacy first by default, because
nearly every server in existence is legacy and probing modern first costs a
wasted round trip on all of them. That is correct today and will stop being
correct, which is why `PreferModern`, `ForceLegacy` and `ForceModern` exist.
A recognised `UnsupportedProtocolVersionError` stops the fallback: it
identifies a modern server, so the version is wrong rather than the era, and
falling back would report the wrong problem.

**As a server** it answers both, and no longer lies. `negotiate()` used to
echo whatever version was asked for, so a client requesting `2026-07-28` was
told yes and then found no `server/discover`. It now answers
`UnsupportedProtocolVersionError` (`-32022`) with the list that would work,
and refuses a modern version over `initialize` at all -- a client sending
`initialize` is legacy by definition, so agreeing would promise a protocol
neither side is speaking.

2026-09-28T20:00:00-05:00

## When a server asks a question

```
created:      2026-09-28T20:00:00-05:00
last-updated: 2026-09-28T20:00:00-05:00
increment:    1
status:       standard
tags:         area:protocol
description:  elicitation, stored rather than blocked on.
```

Every other MCP method runs client to server. Elicitation runs the other way:
a server stops mid-call and asks something -- which repository, are you sure,
log in here.

### The bug that came first

`recvLoop` matched inbound frames on their id alone. A server-initiated
request has an id *and* a method, so it looked like a reply to nothing and was
discarded. The server waited, the call hit its timeout, and mcpx reported a
timeout: true, useless, and pointing at the wrong thing.

Frames are now routed by shape, and a question is **always** answered -- with
`cancel` when nobody was asked, because silence is indistinguishable from a
hung server while cancel is honest about nobody having chosen.

### A question is state, not a blocked call

That is the whole design. A question gets an identity, a deadline and a row
in the database beside the log index. The call stops and says "I need input,
here is the ticket".

So **whoever answers need not be whoever asked.** CI raises a question a
person answers from a laptop twenty minutes later; the reattach works because
nothing was ever held in memory. A call that is waiting exits `75`
(`EX_TEMPFAIL`), which is what it is: not a failure, a "try again when you
have an answer".

Expiry answers `cancel`, never `decline`. Expiry means dismissed without
choosing; telling a server the user declined would say something different and
untrue. Overdue questions expire when something reads them rather than on a
timer, so there is no sweeper to go wrong and nothing is reported as pending
when it is not.

### Who answers

The specification leaves this to the client, deliberately: *"If the client is
an agent, it might decide how to handle the elicitation."*

The default is **the agent**, and that is usually right -- it asked for the
thing, the question is part of that request, and it has the context. A human
is pulled in only for what an agent cannot know or should not hold: a
credential, a browser flow, or a bare confirmation, since consent is not the
agent's to give.

Every routing decision records its reason. Routing nobody can inspect is
routing nobody can correct.

### Answering

```sh
mcpx elicit list
mcpx elicit answer elc-9f2c1a84 repo=me/thing    # key=value, or JSON
mcpx elicit decline elc-9f2c1a84
```

`key=value` is accepted because most answers are one short string, and making
somebody quote JSON for that is ceremony. `mcpx elicit show` prints the
command that answers a question, because the alternative is assembling it from
three fields and getting it wrong once.

2026-09-28T20:00:00-05:00

## Credentials

```
created:      2026-09-28T20:00:00-05:00
last-updated: 2026-09-28T20:00:00-05:00
increment:    1
status:       standard
tags:         area:config
description:  what the specification actually requires, which is little.
```

The specification's position is short and the opposite of what people assume:

- Authorization is **optional**.
- stdio transports **SHOULD NOT** use OAuth. They take credentials from the
  environment -- a child process is trusted because you started it.
- HTTP transports **SHOULD** use OAuth 2.1 when the server is protected.

So most servers need nothing.

```jsonc
{ "mcpServers": { "api": {
    "url": "https://example.com/mcp",
    "auth": { "type": "bearer", "token": "${API_TOKEN}" } } } }
```

`bearer`, `basic`, `header`, `query` and `env`, all with `${VAR}` expansion so
the secret is never in the file. An unset variable is **named up front**
rather than surfacing as a 401 three layers away.

`Describe()` prints a reference but never a literal, because its output is
what people paste into issues.

`oauth` is declared but not performed. A server requiring it says so before
the first request rather than failing with a 401 nobody can interpret.

2026-09-28T20:00:00-05:00

## The rest of the protocol

```
created:      2026-09-28T22:00:00-05:00
last-updated: 2026-09-30T14:30:00-05:00
increment:    2
status:       standard
tags:         area:protocol
description:  the features that are not tools, and which of them mcpx has.
```

Tools, resources and prompts are the famous three. The specification defines
several more, and mcpx was silently dropping most of them.

### Now implemented

**Roots.** A client tells a server which directories it may work within.
Without it a filesystem server has to be told through its own configuration,
separately, in a second place that drifts from the first.

**Server log messages.** `notifications/message` carries a server explaining
itself -- "retrying against the replica" tells you exactly why a call was
slow. Every one was discarded. Note that servers send none until asked, via
`logging/setLevel`, so a client that never asks concludes servers do not
emit any.

**Progress.** `notifications/progress` on a long call, previously dropped.

**List-changed notifications.** A server saying its tools have changed means
the cached schema is stale. mcpx already computes catalog diffs; it just was
not listening for the event that should trigger one.

**Pagination when serving.** mcpx fronts every tool of every configured
server, and a client with a frame limit had no way to read that list. The
cursor is opaque -- base64 of an offset -- because the specification says so
and a client that parses one is relying on something it was told not to. An
*invalid* cursor starts from the beginning rather than failing: a client
cannot validate an opaque value before sending it, so refusing would strand
one that has nothing better to send. That is a deliberate departure from a
SHOULD -- the specification asks for `-32602`, and
[`docs/spec/server-obligations.md`](./spec/server-obligations.md) §4.3 records
the obligation being departed from. #257 is where it gets revisited.

**Completion.** `completion/complete` answers from what mcpx already holds.
A client that offers autocomplete and receives method-not-found shows
nothing, and the user concludes the feature is broken rather than absent.

**Cancellation, inbound.** Recorded rather than dropped. mcpx cannot yet
interrupt an in-flight upstream call -- that needs the request id plumbed
through the pool -- but a cancellation silently discarded leaves a client
unable to tell whether the message arrived.

**Sampling.** Shipped since this entry was written; see
[Sampling](#sampling) below for what it does and the ordering bug that made
it unreachable at first.

### Still not implemented, and declared as such

Capabilities are declared only where mcpx can actually deliver. Claiming one
it cannot serve invites a server to use it and get silence, which is worse
than not offering it at all. What mcpx declares, per revision and per
direction, is [`docs/protocol.md`](./protocol.md) §2.2; what the
specification would require of a server that declared more is
[`docs/spec/server-obligations.md`](./spec/server-obligations.md).

2026-09-30T14:30:00-05:00

## One daemon, many clients

```
created:      2026-09-28T23:30:00-05:00
last-updated: 2026-09-28T23:30:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  a socket, a port, or a machine somewhere else.
```

The daemon has always served its whole API over HTTP. Two things stopped that
being useful beyond one machine: the client hardcoded a unix-socket dialler,
and the listener hardcoded loopback. Both were one line.

```sh
MCPX_DAEMON_ENDPOINT=http://mcpx.internal:8899    # a daemon on a VPN
MCPX_DAEMON_ENDPOINT=unix:///run/mcpx/other.sock  # a different local socket
mcpx daemon --address 0.0.0.0 --port 8899         # serve a network
```

Works for the CLI, the plugin, and generated scripts -- which could already
do it, since they were given `MCPX_ENDPOINT` from the start.

A configured endpoint also means mcpx **will not try to start** that daemon.
Falling back to a local one when a remote is unreachable would silently
answer from the wrong machine, which is worse than failing.

Binding beyond loopback prints a warning, once, because the API is
unauthenticated: whatever can route to the port can run tools as you. That
should be a sentence somebody read rather than a default they inherited.

### What each transport costs

Same request, mean of thirty:

| | |
| --- | --- |
| spawn `mcpx status` | 23.12 ms |
| unix socket, new connection | 0.27 ms |
| unix socket, keep-alive | **0.17 ms** |
| tcp loopback, new connection | 1.59 ms |
| tcp loopback, keep-alive | 0.65 ms |

Spawning is **135× slower** than a warm socket, and almost all of it is
process startup rather than transport. Irrelevant for something run once;
decisive for anything on the path of every tool call.

`plugin/opencode/mcpx/daemon.ts` therefore talks to the socket directly rather
than shelling out. It returns `undefined` when no daemon is reachable rather
than throwing, because a plugin that fails to load has broken the editor for
a tool the user may not even be using.

It sat unused for a day: written, documented, benchmarked, and imported by
nothing. The plugin still spawned `mcpx log record` after every tool call.
Wiring it in found three more things, each invisible until something real
used the path:

- **The socket was guessed.** The client took the newest `.sock` in the state
  directory. Sockets are keyed by configuration, so that is the wrong daemon
  whenever two exist, and none at all when a long state path moved the socket
  elsewhere. It now asks `mcpx --json status`, once.
- **`status` said `running` only when false.** Testing it read a live daemon
  as down.
- **One file, two daemons.** The key hashed the configuration's path as
  spelled. `/tmp` is a symlink to `/private/tmp` on macOS, so a process whose
  `$PWD` held the short form computed a different key, saw no daemon, and
  started a second. Paths are now resolved through symlinks first.

With those fixed, a timing record costs 0.09 ms over the socket instead of a
23 ms spawn.

2026-09-28T23:30:00-05:00

## Hearing what the daemon is doing

```
created:      2026-09-29T11:00:00-05:00
last-updated: 2026-09-29T11:00:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  one event bus, two views of it.
```

MCP's own subscription mechanism, `subscriptions/listen`, carries four kinds of
notification: tools, prompts and resources changing, and a subscribed resource
updating. That is the right set for a server telling a client its catalogue
moved, and nowhere near enough for what a client of mcpx wants to hear -- a
question waiting for an answer, a server that crashed, a call that finished.

So there is one bus and two views of it:

| | carries | for |
| --- | --- | --- |
| `subscriptions/listen` | the four the specification defines | any MCP client |
| `GET /v1/events` | everything mcpx notices | the plugin, the TUI, a script, curl |

Both read the same stream, so they cannot disagree about what happened. The
MCP view is deliberately narrow: the specification says a server MUST NOT send
a notification type the client did not request, so nothing mcpx-specific leaks
into it.

### Server-sent events, not WebSockets

SSE is plain HTTP. It goes through every proxy HTTP does, a browser's
`EventSource` reconnects it without any code knowing, and consuming it takes a
GET. WebSockets would buy bidirectionality nothing here needs -- answers go
back as ordinary POSTs -- at the cost of an upgrade that half of all
middleboxes mishandle.

### Lossless reconnection

Every event carries a sequence number. Reconnect with `Last-Event-ID` (which
`EventSource` sends for you) or `?since=N` and everything after N is replayed.

`?since=0` replays everything retained; *no* position means live only. Those
are different requests, and conflating them -- which an early version did --
made "replay from the start" silently return nothing, indistinguishable from
nothing having happened.

A reconnect older than the retained history gets an in-band `gap` event, so
the subscriber can resynchronise rather than trust a stream with a hole in it.

Publishing never blocks. One slow reader cannot stall the daemon; it misses
events and has a counter to say so.

2026-09-29T11:00:00-05:00

## Sampling

```
created:      2026-09-29T11:00:00-05:00
last-updated: 2026-09-29T11:00:00-05:00
increment:    1
status:       standard
tags:         area:protocol
description:  a server asking for a model's help, passed to whoever has one.
```

Sampling is part of the MCP specification, in every revision -- not an
opencode feature. It lets a **server ask its client for a model completion**:
"summarise this", "classify that", "draft a reply". The server gets an
intelligence it does not have to host or pay for.

It is the mirror of elicitation:

| | asks | for |
| --- | --- | --- |
| elicitation | a person or agent | a value -- which repository, are you sure |
| sampling | a model | text -- a completion the server will use |

mcpx has no model, so it cannot answer either itself. What it can do is carry
the request to something that can. A sampling request becomes a stored
question -- same table, same deadline, same routing as elicitation -- and
whatever drives mcpx answers it. The opencode plugin is the natural answerer:
it holds a session with a real model already.

Declining a sampling request becomes an error, because the specification has
no decline result for it. A result missing `role` or `model` has them filled,
since strict servers reject one without them for a reason unrelated to its
content.

2026-09-29T11:00:00-05:00

### It did not work, and nothing said so

The passthrough above shipped unreachable. A server only sends a sampling
request to a client that declared `sampling`, and mcpx never did: capabilities
are declared in the handshake, and the handler that made sampling possible was
installed on the connection *after* it. Every test of the passthrough called
it directly, so every test passed.

The same ordering broke roots. Once the daemon's handler was installed -- which
is always -- it received every server request, `roots/list` included, and knew
only elicitation and sampling. Roots were configured, and every server that
asked for them was told "not implemented".

Both are fixed by giving the connection its handler and roots at construction,
and by answering roots before consulting the handler at all.

2026-09-29T15:00:00-05:00

## What 2026-07-28 actually asks of a client

```
created:      2026-09-29T15:00:00-05:00
last-updated: 2026-09-29T15:00:00-05:00
increment:    1
status:       standard
tags:         area:protocol
description:  three MUSTs the first modern implementation missed.
```

The first modern-era client probed with `server/discover`, chose a version,
and then spoke exactly as a legacy client would. Reading the schema rather than
the prose found three requirements it did not meet:

| requirement | was | now |
| --- | --- | --- |
| `_meta` carries `protocolVersion` and `clientCapabilities` on **every** request -- a server MUST NOT infer capabilities from an earlier one | sent on none, not even `server/discover` | stamped on every modern request, with `clientInfo` |
| the HTTP `MCP-Protocol-Version` header MUST match that `_meta` version, or 400 | a constant: the legacy version | read from the frame being sent |
| every result MUST carry `resultType` | mcpx's server sent none | `"complete"` on every reply to a modern request |

And one mechanism it did not know existed. A modern server cannot send the
client a request of its own -- there is no connection to send it on -- so it
elicits, samples and asks for roots by *answering*: `resultType:
"input_required"`, a map of `inputRequests`, an opaque `requestState`. The
client answers each, and sends the original request again with
`inputResponses` and the state, exactly as received. mcpx returned the "not
yet" as though it were the result, so against a modern server, elicitation,
sampling and roots silently did nothing.

Both eras now answer through one function, so a question is answered the same
way whether it arrived on the wire or inside a result. The retry loop is
bounded (`elicit.inputRounds`, 8): a server that never stops asking is broken
or adversarial, and without a bound the client would answer it forever.

Still missing, and honest about it: mcpx's *server* does not yet turn an
upstream question into `input_required` for a modern client of its own. It
answers through the broker instead, which works, but a modern client cannot
answer inline.

2026-09-29T15:00:00-05:00

## Running without a daemon

```
created:      2026-09-29T11:00:00-05:00
last-updated: 2026-09-29T11:00:00-05:00
increment:    1
status:       standard
tags:         area:integration
description:  the last rung, and what it costs.
```

Every command walks the same ladder, cheapest first:

```
1. socket    an existing local daemon          0.17 ms
2. url       a named remote one                0.65 ms
3. spawn     start a daemon, then use it       ~23 ms, once
4. inline    run the servers in this process   every time
```

A named endpoint stops the ladder at step 2: falling back to a local daemon
would answer from the wrong machine.

**Inline** hosts the daemon's own API inside the command, on a private socket,
for as long as the command runs. It is the identical handler, so it cannot
drift from daemon mode.

What changes is lifetime, and it changes completely:

| | with a daemon | inline |
| --- | --- | --- |
| a server starts | once, reused | every command |
| between commands | kept warm | gone |
| a browser session | survives across commands | dies with the command |
| schema cache | shared | read from disk each time |

That is why inline is the last rung and off by default. It is for a sandbox
with no fork, a read-only filesystem, or a container whose init will not reap
-- places where the alternative is not working at all. A pool that silently
stops pooling is a performance bug nobody can see, so it has to be asked for.

2026-09-29T11:00:00-05:00

## Tasks

```
created:      2026-09-29T11:00:00-05:00
last-updated: 2026-09-29T11:00:00-05:00
increment:    1
status:       standard
tags:         area:protocol
description:  a slow tool call as a handle rather than a held request.
```

Add `task` to a `tools/call` and the reply is a handle, immediately. Poll it
with `tasks/get`, wait on it with `tasks/result`, stop it with `tasks/cancel`.

For a genuinely slow tool -- a build, a crawl, a browser session -- holding a
request open for minutes invites every proxy, load balancer and client timeout
in between to cut it off. A handle does not.

Core in `2025-11-25`, an extension in `2026-07-28`, so mcpx declares it both
ways and a client of either era finds it where it looks.

Every task has a TTL. The specification allows an unbounded one; mcpx never
offers it, because a result nobody collects is memory nobody frees.

2026-09-29T11:00:00-05:00

## Nix packaging

```
created:      2026-09-27T04:20:00-05:00
last-updated: 2026-09-30T16:10:00-05:00
increment:    3
status:       standard
tags:         area:packaging, platform:nix
description:  buildGoModule with a pinned vendorHash; unit tests run in the
              sandbox; deno, bun and node are pinned on the wrapper's PATH.
```

mcpx has **five** direct third-party Go dependencies. `modernc.org/sqlite`
backs the log index, and it is a pure-Go translation rather than the usual cgo
driver, because a cgo driver would make this derivation need a C toolchain and
would break cross-compilation, for a database that is only ever an index over
files that remain the source of truth. The other four are one decision each:
`bubbletea`, `bubbles` and `lipgloss` for the browser, and `gopkg.in/yaml.v3`
for OpenAPI documents. Each is argued in
[`package.nix`](../package.nix) and listed with its reason in
[`docs/dependencies.md`](./dependencies.md).

This entry and `docs/dependencies.md` both said "one" until the browser and the
OpenAPI work landed, while the browser entry above already described three of
the four by name — the file disagreed with itself, which is how a count that
nothing checks decays.

That reasoning was written down and not enforced: `buildGoModule` leaves cgo
enabled on a native build, so the derivation was free to link against a C
toolchain the comment said it must not need. `CGO_ENABLED=0` is now set in the
derivation itself. It surfaced on a machine with a symlinked `/nix`, where the
cc-wrapper's purity check trips on gcc canonicalising the path -- a build that
should never have consulted a C compiler failing because of one.

The wrapper suffixes `git`, `deno`, `bun-bin` and `nodejs` onto `PATH`, so
script execution does not depend on the calling shell and the two repository
layouts native discovery cannot read still resolve. `git` is a *suffix*, not a
prefix, and since #223 nothing on the default path needs it -- see
[Finding the repository without git](#finding-the-repository-without-git).

The man page and the shell completions are generated by the binary the build
just produced, so they describe the commands and settings this build actually
has. A man page maintained separately is wrong within two releases.

`nix build .#mcpx` runs the unit suites in the sandbox. The end-to-end suite
spawns JavaScript runtimes and binds unix sockets, so it runs outside with
`go test ./...`. The sandbox list is hand-written (`package.nix`), and of the
32 packages under `internal/` that have tests it names 18 — `internal/e2e` is
excluded on purpose, and the other fourteen, `internal/api`, `internal/cli`
and `internal/mcpclient` among them, by drift. A list maintained beside a
generated one, which is the failure mode this document keeps finding (#285).

2026-09-30T16:10:00-05:00

## The comparison register

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T14:30:00-05:00
increment:    2
status:       standard
tags:         area:compare, area:protocol
description:  511 rows, one per difference between mcpx and the five MCP
              revisions, opencode v1 and v2, lootbox and Cloudflare code mode.
```

mcpx sits between things that disagree: five revisions of the protocol, two of
them eras apart; two opencodes with different plugin APIs; three other ways to
run code against MCP tools. Every argument about what mcpx should do next was
being had from memory.

[`docs/compare/`](./compare/) is the register that replaced the memory. One
row per difference -- one field, one method, one error code, one header, one
behaviour -- with where it exists, what mcpx did at commit `05c78b2`, what
adopting it is worth, what it would cost, and a citation for every claim. 511
rows; 171 of them labelled `mcpx missing`.

**The point of a register is that it is specific enough to be wrong.** A
prose comparison says "mcpx has partial 2026 support"; a register says
`server/discover` answers `protocolVersions` where the schema says
`supportedVersions`, at `internal/mcpserver/server.go:534`, against
`schema/2026-07-28/schema.ts:678`. The second can be checked, and checking it
produced twenty-four issues (#199 through #220 and their siblings), each
grouping the rows one fix would close. Several were fixed in the same week
the register was written.

It is dated on purpose. The status column is `mcpx @ 05c78b2` and says so in
every table header, because a status column with no commit behind it is a
column nobody can falsify. Twenty-seven pull requests have landed since; the
README lists them and says the column is stale rather than pretending
otherwise.

2026-09-30T14:30:00-05:00

## Finding the repository without git

```
created:      2026-09-30T07:30:00-05:00
last-updated: 2026-09-30T14:30:00-05:00
increment:    2
status:       standard
tags:         area:config, area:packaging
description:  the repo and worktree scopes read .git themselves, so git is no
              longer a runtime dependency and the image lost 105 MB.
```

A server scoped `repo` gets one process per clone; one scoped `worktree` gets
one per checkout. Both keys used to come from forking git twice per directory
-- `rev-parse --git-common-dir` and `--show-toplevel` -- and caching the
answers, negative ones included, for the daemon's lifetime.

That cost four things, and three of them were only found by building the
replacement:

- **105 MB of the container image.** Debian's git depends on perl outright.
- **It did not work in the container anyway.** A checkout mounted into the
  image is owned by the host's uid and the image runs as uid 1000, so git
  refuses it: `fatal: detected dubious ownership in repository at
  '/work/proj'`. git was in the image *so that these scopes would work*, and
  in the ordinary way of using the image they silently fell back to
  per-directory keys.
- **The daemon's environment leaked into every caller's key.** An autostarted
  daemon inherits the environment of whoever ran mcpx first. A git hook
  exports `GIT_DIR`; a hook that runs mcpx leaves a daemon carrying it for
  hours, and every later caller from any directory was then keyed to that one
  repository.
- **The cache made the first answer permanent.** A directory that became a
  repository after the first call there stayed "not a repository" until the
  daemon exited.

mcpx now reads `.git` itself -- a port of git's own discovery rules, written
against them and recorded in [`docs/git-discovery.md`](./git-discovery.md)
layout by layout. git is still consulted, once, for the two layouts the
native reader deliberately does not implement, and if it is absent those
layouts degrade to `cwd` with a log line and a `doctor` warning saying which
layout and why. That is the whole of the dependency: no fetching, no writing,
no network.

The environment leak is pinned by
`TestTheDaemonsOwnGitDirDoesNotPinEveryCaller`, which fails against the old
code. The image went from 413 MB to 288 MB.

`doctor` keeps its git row. An optional dependency whose absence changes
behaviour is exactly the thing a diagnostic should report -- dropping the row
because git became optional would have hidden the one case where its absence
still matters.

2026-09-30T14:30:00-05:00

## Measured against the official suite

```
created:      2026-09-30T09:00:00-05:00
last-updated: 2026-09-30T14:30:00-05:00
increment:    3
status:       standard
tags:         area:protocol, area:conformance
description:  every frame validated against the real schema, and the official
              MCP conformance suite run against a live daemon in both
              directions.
```

mcpx claims five revisions in each direction. Until this, all five claims were
tested against mcpx's idea of them.

Two things changed that. First, `internal/mcpspec` validates real traffic
against the official schemas for all five revisions, in strict mode when mcpx
is the sender: a key some *other* revision defines is a failure, because
"send conservatively" is precisely the rule a lenient check cannot see. That
is what makes a passing test mean something -- the previous tests pinned the
wrong `server/discover` field name on both sides at once, which is how it
passed CI for weeks.

Second, there is now an **official** suite: `modelcontextprotocol/conformance`.
It is not optional folklore. SEP-2484, Final, makes a merged conformance
scenario a condition of a Standards Track SEP reaching Final, and SEP-1730
ties SDK tiering to a score from it. Its `requirements/<revision>.yaml` files
are frozen, so an implementation is measured against the suite as it stood
when it was expected to conform. `scripts/conformance.sh` runs both legs: mcpx
as a server at the daemon's `/mcp`, and -- through
`internal/conformance/officialclient` -- mcpx as the client under test.

It found real defects, including two that no amount of reading found: a
modern request missing `_meta` was answered `-32020` when the specification
and mcpx's own doc comment both say `-32602`, and the five methods 2026-07-28
*removed* were still being answered to 2026-07-28 peers. The second is the
limit of "accept liberally": offering a method a revision never had withholds
nothing, but answering one it removed contradicts the specification naming the
replacement.

The numbers -- passed and failed per requirement set, the commit they were
measured at, and which failures are defects -- live in exactly one place,
[`docs/spec/official-suite.md`](./spec/official-suite.md), so they are not
repeated here to go stale.

**Read the failure counts carefully; most are not defects.** The suite's
scenarios assume a server implementing its own fixture surface -- tools named
`slow_compute`, prompts named `test_simple_prompt`, resources under `test://`.
mcpx is a proxy: it publishes its own small tool set and namespaces every
upstream resource as `mcpx://<namespace>/<uri>`. A scenario that cannot find
its fixture fails without ever reaching a protocol assertion. An honest
refusal can score worse than a dishonest pass: once #247 made mcpx arrange a
real upstream subscription, subscribing to `test://watched-resource`, which no
upstream owns, became a failure (#251).

What the specification asks of a server, revision by revision -- including the
obligations the prose implies but never states -- is
[`docs/spec/server-obligations.md`](./spec/server-obligations.md).

2026-09-30T14:30:00-05:00

## Decision records

```
created:      2026-09-30T10:00:00-05:00
last-updated: 2026-09-30T14:30:00-05:00
increment:    1
status:       standard
tags:         area:process
description:  when two issues answer one question differently, the answer is
              written down before either is built.
```

An audit of the backlog found eighteen collisions between themes: pairs of
issues proposing different answers to the same question, written by people who
could not see each other's work, none with an owner. Each would have been a
breaking change whenever the second one was built.

[`docs/decisions/`](./decisions/) holds the answer to each. A record says what
the code already does, what each issue proposed, which option was taken and
why, and exactly what every affected issue has to change as a result. Three
exist: the hook vocabulary and where a hook's output goes in the exec stream;
one autonomy dial with a ceiling the daemon sets; and what mcpx *declares*
against what it *enforces*.

Two rules keep them honest. **A record that lists options without choosing one
does not belong here** -- that is a design document, and it is the shape a
decision takes when nobody wants to decide. And **a change that contradicts an
accepted record changes the record in the same pull request, or it does not
merge**: a record nobody is obliged to update is a record that describes last
month.

2026-09-30T14:30:00-05:00

## A command for every operation

```
created:      2026-09-30T13:00:00-05:00
last-updated: 2026-09-30T14:30:00-05:00
increment:    2
status:       standard
tags:         area:cli, area:parity
description:  the CLI, /v1, the MCP tools and the plugin are four renderings
              of one declaration, and tests hold each direction.
```

`config = env = cli = /v1 = mcp` had been the rule from the start, and the
CLI was the surface that kept falling behind: an operation was easy to add to
the ops table and easy to forget on the command line, and nothing noticed.

A command is now *generated* from its operation's declaration -- flags from
its parameters, positionals for its path -- so it exists the moment the
operation does. Sixty operations, each with a route and a plugin method;
fifty-nine with an MCP tool, because one of them streams and a tool call
cannot. Fifty-two commands, because some cover several operations.
[`docs/parity.md`](./parity.md) is generated from the same four declarations,
which is why it cannot describe a surface that does not exist or leave one
out, and ten tests hold the seven directions between them -- including that
every command the binary runs is declared, and every declared one runs.

`mcpx openapi` was the same failure one level up. It printed a hand-written
map maintained beside the generated one, and the map had drifted to describing
a command that was removed: its only declared server was `mcpx serve
--transport http`, which refuses to run, and it listed none of the `/v1`
operations. Thirteen paths against sixty-four. There is now one document --
the command adds to the bytes the daemon serves rather than competing with
them -- and it stays publishable, because the daemon templates upstream tools
as `/v1/tools/{tool}` instead of enumerating a particular machine's:

```
$ mcpx openapi | jq '.paths | length'
64
$ mcpx openapi | jq -c .servers
[{"description":"over the daemon's unix socket","url":"http://mcpx"},{"description":"the loopback endpoint reported by /v1/health","url":"http://127.0.0.1:0"}]
```


**Privileged operations are offered, not hidden.** A caller that can reach the
socket can already stop the daemon, so withholding the tool buys no safety and
costs an agent the ability to restart a server that has wedged. What they
carry instead is a description saying so and the MCP annotations
(`readOnlyHint`, `destructiveHint`) a client can scope on.

2026-09-30T14:30:00-05:00

---

## Proposed

Everything below is wanted and not yet built. Status fields say so.

### Plain scripts returning primitives

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:scripts
```

A script's stdout is already its result, so `mcpx run count-todos` printing
`47` works today. What is missing is a declared contract — an exit-code
convention, and `--json` on `run` that wraps stdout, stderr, exit status and
duration in one envelope for programmatic callers.

2026-09-27T05:05:00-05:00

### Named, pinned and resumable sessions

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:pools, area:sessions
```

Leases are anonymous and per-run. Wanted: `mcpx session new --name login-flow`
to create a named lease that outlives the process, `mcpx session ls` and
`mcpx session search` to find them, `--session login-flow` to rejoin one, and
a TTL so a browser mid-login can be resumed rather than rebuilt.

`--session <key>` already exists on `run`, `exec` and `call`; the missing parts
are naming, persistence across daemon restarts, and listing.

2026-09-27T05:05:00-05:00

### Attaching to another session

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:sessions, area:sharing
```

Read-only attach for watching what another agent's browser is doing, and
read-write attach for taking over. Needs a lease model that admits more than
one holder, plus a permission story for who may attach to whose.

2026-09-27T05:05:00-05:00

### Per-session restart

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:pools, area:ops
```

`mcpx restart <ns>` stops every instance in a namespace. Wanted:
`mcpx restart --session <key>`, so one wedged browser can be recycled without
disturbing the other three.

2026-09-27T05:05:00-05:00

### Zero-downtime upgrade

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:daemon, area:ops
```

Upgrading means stopping the daemon, which kills every MCP server it holds.
Wanted: socket handoff to a new binary with child processes preserved — the
same problem opencode v2 solves for its PTY daemon with a handoff ticket.

2026-09-27T05:05:00-05:00

### Service installation

```
created:      2026-09-27T05:05:00-05:00
last-updated: 2026-09-30T14:30:00-05:00
increment:    2
status:       partial
tags:         area:ops, area:packaging
```

A nix-darwin module existed and was dropped when the package moved into the
nix repo. Wanted: `mcpx service install` emitting a launchd plist or systemd
unit.

**The systemd half now exists as a template rather than a command.**
[`daemon/mcpx.service`](../daemon/mcpx.service) is a user unit --
`default.target`, `Restart=on-failure` -- with
[`daemon/README.md`](../daemon/README.md) giving the install, verify and log
commands. A *user* unit rather than a system one because config and state
belong in the real `$HOME`, and a system unit running as root would need a
`HOME` override to find either.

A file you copy is not the same as a command that writes it, so this stays
`partial`: there is still no `mcpx service install`, nothing emits a launchd
plist, and macOS gets nothing. The completions half of the original want is
done -- `mcpx completion bash|zsh|fish` exists, and the Nix build already
installs what it prints.

Until then the daemon starts on demand from any command, which is the
supported path.

2026-09-30T14:30:00-05:00

### Alternative input modes

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:cli
```

Today: `mcpx exec '<code>'` and `mcpx run <name|file>`. Scripts inherit stdin,
so a script can read it. Missing: `mcpx exec -` to take the program itself from
stdin, `--env-file`, and an interactive REPL holding one session across
statements.

2026-09-27T05:05:00-05:00

### OAuth for remote servers

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:transport, area:auth
```

Streamable HTTP works; bearer headers can be set in config. Missing: the OAuth
authorization-code flow, credential storage, and refresh. Servers needing
interactive auth must be reached another way for now.

2026-09-27T05:05:00-05:00

### Context-budgeted catalog

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:discovery, cost:context
```

`mcpx types <ns>` is all-or-nothing: 4,402 tokens for chrome_devtools whether
three tools are needed or all 27. opencode v2's code mode instead fits a
catalog to a fixed token budget, showing every namespace and as many full
signatures as fit, round-robin, shortest first.

Wanted: `mcpx catalog --budget 2000` emitting one paste-ready block whose size
is bounded no matter how many servers are configured.

2026-09-27T05:05:00-05:00

### OpenAPI documents as tools

```
created:      2026-09-27T05:05:00-05:00
status:       proposed
tags:         area:transport
```

An OpenAPI 3.x document is already a machine-readable description of callable
operations. Turning one into a namespace would compose with pools exactly as
MCP servers do, and is a large amount of reach for a contained amount of code.

2026-09-27T05:05:00-05:00

---

# Summary

mcpx puts every MCP server behind one command so their schemas stay out of the
model's context until something asks for them, and leases a separate server
process to each script run so that concurrent agents driving stateful servers —
browsers, above all — cannot corrupt one another.

The walk above is the whole interface: `ls` to see what exists, `search` to
find a tool, `types` to load one namespace, `run` to do the work. Everything
else in this document is detail underneath those four verbs.

# Notes and links

- [`AGENTS.md`](../AGENTS.md) — the instruction file abridged above. This is
  the text to inject into an agent's session; it is deliberately short, and
  109 lines today. The quote above is not the whole of it (#264).
- [`ASSESSMENT.md`](../ASSESSMENT.md) — why this exists, with the measurements
  against the predecessor it replaced.
- [`OPENCODE-V2.md`](../OPENCODE-V2.md) — how this compares to opencode v2's
  in-process code mode, including where v2 is the better answer.
- [`docs/release-runbook.md`](./release-runbook.md) — how to keep this document
  current. Read it before editing anything above.
- [`docs/proposals.md`](./proposals.md) — open design questions, and what is
  true today versus what is merely wanted.
- [`docs/ideas.md`](./ideas.md) — unvetted brainstorm intake, before anything
  has a shape, with answers recorded inline.
- [`docs/dependencies.md`](./dependencies.md) — every library and external
  program, and why each is needed.
- [`docs/opencode-plugin.md`](./opencode-plugin.md) — what the plugin does and
  why, opencode v1 against v2, and which parts are opencode's rather than
  general enough to port to another harness.
- [`scripts/stress.sh`](../scripts/stress.sh) — concurrency and leak checks
  against real servers. [`scripts/bench.sh`](../scripts/bench.sh) — latency
  comparison.
- [Model Context Protocol](https://modelcontextprotocol.io) — the spec. Its
  [client best-practices guide](https://modelcontextprotocol.io/docs/2026-07-28/develop/clients/client-best-practices)
  documents this pattern as "programmatic tool calling".
- [Cloudflare, *Code Mode*](https://blog.cloudflare.com/code-mode/) — where the
  idea was first argued publicly.
