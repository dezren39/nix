---
name: mcpx-basics
description: Use when reaching an MCP server through mcpx - listing what exists, reading tool signatures, or writing a script that calls tools. Covers why to filter in the script rather than in your context.
---

# Reaching MCP servers through mcpx

Tools are not in your context. They are behind `mcpx`, and you get at them by
writing a script.

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

Tools are bound as async functions under `tools`:

```ts
const hits = await tools.fff.grep({ query: "parseConfig" });
const files = JSON.parse(hits.content[0].text).map((h: any) => h.path);
console.log(files.slice(0, 10).join("\n"));
```

**Only what you print comes back.** That is the whole point. A search that
returns 200KB of JSON costs you nothing if the script prints ten paths.

So: filter, count, join and summarise inside the script. Reading a large
result into your context and then picking through it is the one mistake this
tool exists to prevent.

## Useful built-ins

- `emit(value)` streams a result as the script runs
- `log.info("msg", { k: v })` writes a structured record
- `console.log` is stdout, which is the script's answer
- top-level `await` works

## When something fails

`mcpx log --since 5m` shows what actually happened. `mcpx log --chain <trace>`
walks a call back through the server that served it to the daemon that started
it. Neither re-runs anything.

## Do not

- Do not ask for `mcpx types` on every namespace "to see what is there".
  That is what `mcpx catalog --budget 2000` is for.
- Do not print a whole result to inspect it. Print `JSON.stringify(r).length`
  first, then decide.
