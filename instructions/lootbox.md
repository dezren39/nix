All tools are accessed through lootbox. The launchd-managed server listens only on `http://127.0.0.1:9420`.

Lootbox is not required for every task; use it when an available MCP capability fits.

## Always write scripts

Write `.ts` scripts to `.lootbox/scripts/` and run them with `lootbox <script>.ts`.
Scripts are reusable, testable, and composable. Use `lootbox exec 'code'` only for
one-line checks.

```typescript
// .lootbox/scripts/find-todos.ts
const results = await tools.mcp_fff.grep({ query: "TODO" });
console.log(JSON.stringify(results, null, 2));
```

## Choosing a namespace

| Need                                       | Use                                             |
| ------------------------------------------ | ----------------------------------------------- |
| Find a string in the directory you are in  | built-in `grep`/`glob` — not lootbox                |
| Ranked search over the 26 repos in `~/git`   | `mcp_fff`                                         |
| Ranked search over opencode worktrees      | `mcp_fff_worktree`                                |
| Ranked search over this flake              | `mcp_fff_nix`                                     |
| Where a symbol is defined, or who calls it | `mcp_codedb`                                      |
| How one symbol reaches another; blast radius | `mcp_codebase_memory`                           |
| How to use a third-party library           | `mcp_context7`                                    |
| Load a page, click, screenshot, inspect    | `mcp_chrome_devtools`                             |

Each fff process indexes exactly one root, fixed at daemon start:

| namespace          | root                               |
| ------------------ | ---------------------------------- |
| `mcp_fff`          | `~/git` — 26 repos                 |
| `mcp_fff_worktree` | `~/.local/share/opencode/worktree` |
| `mcp_fff_nix`      | `~/.config/nix`                    |

Outside all three, **fff returns 0 matches rather than an error** — use the built-in
`grep`. Constraints filter a search and must be shaped like `*.ts`, `src/` (trailing
slash), `schema.rs`, or `!test/`; bare words are treated as search text, not filters.
Load the `fff` skill for the full syntax, which lootbox does not forward.

`mcp_codedb` and `mcp_codebase_memory` both index per project. codedb is a flat
symbol and trigram index; codebase-memory is a call graph, so prefer it only when
the question is about relationships rather than locations. Its `search_graph` is
BM25 — literal terms, no semantic matching — and its `semantic_query` is not worth
using. It runs in the analysis profile: read-only, so it cannot index a new
project — index from the CLI instead. Treat `check_index_coverage` as required
before any claim that something does not exist.

With codedb, reach for the structural tools first — `codedb_symbol` for a
definition, `codedb_callers` for usages, `codedb_outline` before `codedb_read`.
`codedb_search` is a substring fallback for when the symbol name is unknown, not
the default. Edit with native tools, never `codedb_edit`.

Use `mcp_context7` for any library, framework, SDK or CLI question even when the
answer seems known, since training data lags releases; prefer it over web search
for library docs. It has itself indexed, but not lootbox.

Run `lootbox tools types <ns>` for the TypeScript signatures of a namespace, and
`lootbox tools` to list namespaces.

## Key notes

- `mcp_codedb.codedb_remote` searches actual library source code; `mcp_context7` searches documentation
- For codedb worktrees: call `mcp_codedb.index` with the worktree path first
- Context7 authenticates through `mcp-remote`; never put secrets in config
- `just lootbox-check` verifies health, tools, and Deno scripts
