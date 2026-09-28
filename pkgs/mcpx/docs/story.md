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

What it does have is one instruction file, 62 lines, injected at session start.
It reads:

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
`namespace`, `description`, `tools`, `excludeTools`, `disabled`. A top-level `pool`
block applies any of them to every server.

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

## Nix packaging

```
created:      2026-09-27T04:20:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    1
status:       standard
tags:         area:packaging, platform:nix
description:  buildGoModule with vendorHash = null; unit tests run in the
              sandbox; deno, bun and node are pinned on the wrapper's PATH.
```

mcpx has one third-party Go dependency: `modernc.org/sqlite`, which backs the
log index. It is a pure-Go translation rather than the usual cgo driver,
because a cgo driver would make this derivation need a C toolchain and would
break cross-compilation, for a database that is only ever an index over files
that remain the source of truth.

The wrapper suffixes `deno`, `bun-bin` and `nodejs` onto `PATH` so script
execution does not depend on the calling shell.

The man page and the shell completions are generated by the binary the build
just produced, so they describe the commands and settings this build actually
has. A man page maintained separately is wrong within two releases.

`nix build .#mcpx` runs the unit suites in the sandbox. The end-to-end suite
spawns JavaScript runtimes and binds unix sockets, so it runs outside with
`go test ./...`.

2026-09-27T05:05:00-05:00

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
status:       proposed
tags:         area:ops, area:packaging
```

A nix-darwin module existed and was dropped when the package moved into the
nix repo. Wanted: `mcpx service install` emitting a launchd plist or systemd
unit, plus `mcpx completions bash|zsh|fish`.

Until then the daemon starts on demand from any command, which is the
supported path.

2026-09-27T05:05:00-05:00

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

- [`AGENTS.md`](../AGENTS.md) — the instruction file quoted above. This is the
  text to inject into an agent's session; it is deliberately 62 lines.
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
- [`scripts/stress.sh`](../scripts/stress.sh) — concurrency and leak checks
  against real servers. [`scripts/bench.sh`](../scripts/bench.sh) — latency
  comparison.
- [Model Context Protocol](https://modelcontextprotocol.io) — the spec. Its
  [client best-practices guide](https://modelcontextprotocol.io/docs/2026-07-28/develop/clients/client-best-practices)
  documents this pattern as "programmatic tool calling".
- [Cloudflare, *Code Mode*](https://blog.cloudflare.com/code-mode/) — where the
  idea was first argued publicly.
