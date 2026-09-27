# Dependencies

Everything mcpx needs, and why. Nothing here is incidental — if it is listed,
something breaks without it.

## Go libraries

**None.** `go.mod` has no `require` block. The MCP client, the JSON-RPC
framing, the process pools, the JSON Schema to TypeScript compiler, the HTTP
API and the CLI are all standard library.

This is deliberate and worth keeping:

- `vendorHash = null` in the Nix package, so there is no dependency tree to
  pin, audit or update.
- Nothing to review when a transitive package changes hands.
- The build is `go build` with no network.

The standard library packages doing real work: `net/http` (daemon and client),
`encoding/json` (everything), `os/exec` + `syscall` (child processes and
process groups), `sync` (pools), `path/filepath` (path identity),
`text/tabwriter` (human output).

If a dependency is ever added, it should be for something genuinely hard —
a tokeniser, a SQLite driver — and the trade should be stated here.

## External programs

| Program | Needed for | If missing |
| --- | --- | --- |
| `git` | `repo` and `worktree` scopes | those scopes degrade to `cwd`, with a log line saying git is absent |
| `deno`, `bun` or `node` | `mcpx run` and `mcpx exec` | those commands fail; `mcpx call`, `ls`, `types`, `catalog`, `search` still work |
| the configured MCP servers | their own namespaces | that namespace reports `error` with the server's stderr |

**git** is invoked as `git rev-parse [--path-format=absolute] --git-common-dir`
and `--show-toplevel`, with results cached per directory for the daemon's
lifetime. `--path-format=absolute` needs git 2.31 (March 2021); there is a
fallback that absolutises the relative answer by hand, so older git still
works. Nothing else about git is used — no fetching, no writing, no config.

**A JavaScript runtime** is chosen at first use: deno, then bun, then node.
Override with `--runtime` or the `runtime` config key. Deno runs with
`--no-check`; the generated client is machine-written and a type error in the
agent's own script surfaces at runtime anyway.

**MCP servers** are whatever the config names. mcpx spawns them with the given
`command`, `args`, `env` and `cwd`, or connects over Streamable HTTP for `url`
servers. Their own configuration is exactly those argv and environment
entries — MCP has no separate configuration channel.

## Network

The daemon binds two listeners, both local: a unix socket for the CLI, and
`127.0.0.1` on an OS-assigned ephemeral port for generated script clients,
because `fetch()` over a unix socket is not portable across the three
runtimes. Neither is reachable off the machine.

Outbound traffic happens only when a configured server is a `url` server, or
when a script makes its own requests.

## Nix packaging

`pkgs/mcpx/package.nix` wraps the binary with `git`, `deno`, `bun-bin` and
`nodejs` suffixed onto `PATH`, so behaviour does not depend on the calling
shell. `git` is in that list because `repo` and `worktree` scopes are
silently weaker without it — that was a real gap, found by asking this
question.

The unit suites run in the build sandbox. The end-to-end suite spawns
JavaScript runtimes and binds unix sockets, so it runs outside with
`go test ./...`.
