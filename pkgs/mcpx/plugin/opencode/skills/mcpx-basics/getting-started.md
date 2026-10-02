# Getting started with mcpx

From nothing to a script calling a tool. Every command here was run as
written; the outputs are what came back.

## 1. Install

mcpx is one Go binary. `mcpx exec` and `mcpx run` also need a TypeScript
runtime on `PATH`: deno, bun or node (tried in that order). `call`, `ls`,
`types`, `search` and `catalog` work without one.

```sh
# with nix
nix run github:dezren39/nix#mcpx -- --version

# from a checkout of github.com/dezren39/nix
cd pkgs/mcpx && go build -o ~/.local/bin/mcpx ./cmd/mcpx
```

`mcpx doctor` says what is missing, if anything.

## 2. First configuration

mcpx reads the `mcpServers` object every MCP host uses, from `.mcpx.json` in
the current directory or any parent. An existing config pastes in unchanged.
The quickest way to write one is to add a server:

```sh
mcpx servers add fs -- npx -y @modelcontextprotocol/server-filesystem "$PWD"
# added fs to .mcpx.json; it is live now
```

`mcpx init` writes a commented starter file instead, with examples of the
optional per-server `"mcpx"` block (`sharing`, `scope`, `description`).
Remove the example servers it contains before use: they name commands that
are probably not installed, and `mcpx ls` reports them as failed.

There is no daemon to start. The first command that needs one starts it.

## 3. Look before calling

```sh
mcpx ls                 # namespaces, tool counts, sharing; starts no server
mcpx search read file   # find a tool by words
mcpx types fs           # TypeScript signatures for one namespace
```

A namespace shows `0 TOOLS` and `unread` until something reads its schemas;
the first `types`, `search` or call does.

## 4. One call

```sh
mcpx call fs.read_text_file '{"path":"/abs/path/to/note.txt"}'
```

Arguments are one JSON object. `mcpx call` is for a single call you want to
see the whole answer to. Anything more is a script.

## 5. A script

```sh
mcpx exec 'const r = await fs.list_directory({ path: "/abs/path" });
  const names = r.content.split("\n").filter((l) => l.endsWith(".ts"));
  console.log(names.length, "TypeScript files:", names.join(", "))'
# 2 TypeScript files: [FILE] a.ts, [FILE] b.ts
```

Every namespace is a global inside `exec` (`fs` here; also `tools.fs`).
Results arrive unwrapped: this server returns structured content, so `r` is an
object and `r.content` the listing. Another tool may return a plain string. If
you do not know which, print `typeof r` and `Object.keys(r)` first -- or run
`examples/filter-in-script.ts`, which does exactly that.

For anything longer than a line, write a file and `mcpx run` it:

```sh
mcpx run ./report.ts arg1 arg2      # args arrive in Deno.args, on every runtime
```

A named script in `.mcpx/scripts/<name>.ts` (searched upward, then
`~/.config/mcpx/scripts/`) runs as `mcpx run <name>`; `mcpx scripts` lists
them. The `examples/` folder beside this file holds working scripts to copy
there.

## 6. When it does not work

| symptom | look at |
| --- | --- |
| a namespace shows `error` in `mcpx ls` | `mcpx status`, which has the full start error |
| a call failed or hung | `mcpx log --since 5m` |
| `exec` says no runtime | install deno, bun or node; `mcpx doctor` |
| servers from another project appear | more than one daemon; see the `mcpx-daemon` skill |

## 7. Giving an MCP host mcpx itself

A host that only speaks MCP can use mcpx as one server:

```json
{ "mcpServers": { "mcpx": { "command": "mcpx", "args": ["serve"] } } }
```

It gets mcpx's own tools, and these skills over the MCP skills extension
(`skill://mcpx/<name>/SKILL.md`), supporting files included.
