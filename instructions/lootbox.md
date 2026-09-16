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

## Available MCP namespaces

| Namespace             | What it does                                                    |
| --------------------- | --------------------------------------------------------------- |
| `mcp_codedb`          | Codebase exploration, symbol lookup, AST-aware search           |
| `mcp_fff`             | Frecency-ranked file search and content grep                    |
| `mcp_chrome_devtools` | Browser automation, UI verification, screenshots                |
| `mcp_context7`        | Library documentation lookup (API refs, usage guides, examples) |

Run `lootbox tools types <ns>` for the TypeScript signatures of a namespace, and
`lootbox tools` to list namespaces. Prefer CodeDB when AST-aware exploration fits.

## Key notes

- `mcp_codedb.codedb_remote` searches actual library source code; `mcp_context7` searches documentation
- For codedb worktrees: call `mcp_codedb.index` with the worktree path first
- Context7 authenticates through `mcp-remote`; never put secrets in config
- `just lootbox-check` verifies health, tools, and Deno scripts
