Tools are not in your context. They live behind lootbox, a local gateway on
`http://127.0.0.1:9420`. Write `.ts` scripts to `.lootbox/scripts/` and run them
with `lootbox <script>.ts`; inside a script call `tools.<namespace>.<fn>({args})`.
Use `lootbox exec 'code'` only for one-line checks.

Lootbox is not required for every task. When the answer is in the directory you
are working in, the built-in `grep`/`glob` are the right default.

| Namespace             | Covers                                                    |
| --------------------- | --------------------------------------------------------- |
| `mcp_fff`             | ranked search, `~/git` (26 repos)                         |
| `mcp_fff_worktree`    | ranked search, `~/.local/share/opencode/worktree`         |
| `mcp_fff_nix`         | ranked search, `~/.config/nix` (nix-darwin config)        |
| `mcp_codedb`          | where a symbol is defined, who calls it                   |
| `mcp_codebase_memory` | how one symbol reaches another, blast radius, call graph  |
| `mcp_context7`        | third-party library and CLI documentation                 |
| `mcp_chrome_devtools` | load a page, click, screenshot, inspect network           |

**These tools fail silently.** Each fff process indexes exactly one root, so
outside the three listed above it returns `0 matches` rather than an error.
codedb and codebase-memory answer only for paths they have already indexed. An
empty result from any of them means "not indexed", never "not present" — confirm
with the built-in `grep` before concluding something does not exist.

Load the **`lootbox` skill** before using any of these: it carries the query
syntax and per-tool guidance that lootbox does not forward from the servers
themselves.
