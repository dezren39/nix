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

Namespaces in `session` mode (see the MODE column of `mcpx ls`) give each run
its own server process. Anything that must share state — navigate, snapshot,
click — has to happen inside **one** invocation. Two `mcpx exec` calls get two
browsers.

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
