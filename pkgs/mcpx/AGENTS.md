# mcpx

Every MCP server on this machine is reachable from the shell through `mcpx`.
None of their tool schemas are in your context. You pull in only what you need.

## The loop

1. `mcpx ls` — which namespaces exist. Cheap; run it whenever.
2. `mcpx search <words>` — find a tool by name or description.
3. `mcpx types <namespace>` — load signatures for that namespace only, or
   `mcpx types <namespace>.<tool>` for a single tool, which is far cheaper.
   `mcpx catalog [--budget N] [--bias words]` fits every namespace into a
   token budget when you are still deciding.
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

`log`, `emit`, `tools` and every namespace are already on `globalThis`, so a
script needs no imports at all. `log(msg)` is `log.info(msg)`. Importing works
too and gives the same objects:

    import tools from "./mcpx-client.ts";
    const pages = await tools.chrome_devtools.list_pages({});

Every namespace is also a bare identifier inside `mcpx exec`.

## Profiles

Some namespaces are hidden unless asked for. `mcpx ls` shows the default set;
`mcpx --profile <name> ls` adds a group, and `--skip-default` narrows to
exactly it. If a namespace you expect is missing, try `mcpx --all-profiles ls`
before concluding it does not exist.

The selection applies to the generated client too, so a namespace outside the
active profile is not callable from a script.

## Script contract

stdout is your result; stderr is diagnostics. Use the log helper rather than
`console.error`:

    import tools, { log } from "./mcpx-client.ts";
    log.info("fetched {count} pages", { count: 3 });

A message may carry {placeholders} filled from the attributes; {{x}} is a
literal {x}. An Error passed after the message is captured structurally.

To stream results rather than returning one at the end:

    for (const file of files) emit({ file, count: await scan(file) });

Each value is written as it is produced. Streaming and returning can be
combined: the streamed values are progress, the return value is the answer.

If a script has a default export, mcpx calls it with argv and prints what it
returns. Otherwise top-level code runs. `--export <name>` calls a named export
with spread arguments instead.

`mcpx --json run <script>` returns one document with the result, the logs, the
exit code and the duration. Use it when something else has to read the output.

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
