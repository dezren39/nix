---
name: mcpx-basics
description: Use when reaching an MCP server through mcpx - installing and configuring it for the first time, listing what exists, reading tool signatures, or writing a script that calls tools. Has a getting-started guide and runnable example scripts, and covers why to filter in the script rather than in your context.
---

# Reaching MCP servers through mcpx

Tools are not in your context. They are behind `mcpx`, and you get at them by
writing a script.

New to mcpx -- nothing installed, no config yet? Read `getting-started.md`
beside this file first. `examples/` holds working scripts; `examples/README.md`
says what each one shows.

## The loop

```sh
mcpx ls                    # what exists, cheap, starts nothing
mcpx types <namespace>     # signatures for one server
mcpx exec '<typescript>'   # run something
```

`mcpx ls` first, always. It is small and it tells you what else is worth
asking for. `mcpx types` on a namespace you have not looked at is thousands of
characters; on one you have chosen it is the thing you needed.

## Writing the script

Tools are bound as async functions under `tools`, and each namespace is also
a global:

```ts
const r = await tools.fs.list_directory({ path: "/abs/path" });
const ts = r.content.split("\n").filter((l: string) => l.endsWith(".ts"));
console.log(ts.length, "TypeScript files\n" + ts.slice(0, 10).join("\n"));
```

A result arrives **unwrapped**: structured content and JSON-in-text are
already parsed, plain text is a string. Do not reach into `.content[0].text`;
the MCP envelope, where image bytes live, is on `.raw`. A tool answering
`isError` throws `ToolError` (`examples/handle-errors.ts`). Not sure of the
shape? Print `typeof r` and `Object.keys(r)` before anything else.

**Only what you print comes back.** That is the whole point. A search that
returns 200KB of JSON costs you nothing if the script prints ten paths.

So: filter, count, join and summarise inside the script. Reading a large
result into your context and then picking through it is the one mistake this
tool exists to prevent.

## Useful built-ins

- `search(words)` and `describe("ns.tool")` find and explain tools without
  leaving the script (`examples/find-tool.ts`)
- `call(ns, tool, args)` calls by name, for a tool chosen at run time
- `artifact(name, data)` keeps a file and returns a small reference
  (`examples/save-artifact.ts`)
- `emit(value)` streams a result as the script runs
- `log.info("msg", { k: v })` writes a structured record
- `console.log` is stdout, which is the script's answer
- top-level `await` works
- `Deno.args` holds `mcpx run` arguments on deno, bun and node alike

## When something fails

`mcpx log --since 5m` shows what actually happened. `mcpx log --chain <trace>`
walks a call back through the server that served it to the daemon that started
it. Neither re-runs anything.

## Do not

- Do not ask for `mcpx types` on every namespace "to see what is there".
  That is what `mcpx catalog --budget 2000` is for.
- Do not print a whole result to inspect it. Print `JSON.stringify(r).length`
  first, then decide.
