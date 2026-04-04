All tools are accessed through lootbox. The lootbox server runs on `http://localhost:9420` (auto-started by launchd on macOS).

## Always write scripts

Write `.ts` scripts to `.lootbox/scripts/` for any tool usage. Only use `lootbox exec 'code'` for one-line checks. Scripts are reusable, testable, and composable.

```bash
# Write a script (preferred)
cat > .lootbox/scripts/find-todos.ts << 'EOF'
const results = await tools.mcp_fff.grep({ query: "TODO" });
console.log(JSON.stringify(results, null, 2));
EOF
lootbox find-todos.ts

# Inline only for quick checks
lootbox exec 'console.log(await tools.mcp_codedb.status({}))'
```

## Available MCP namespaces

| Namespace           | Tools                                                                                                                                                                                                                                       | What it does                                                    |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| `mcp_codedb`          | tree, outline, symbol, search, word, hot, deps, read, edit, changes, status, snapshot, bundle, projects, index, remote                                                                                                                      | Codebase exploration, symbol lookup, AST-aware search           |
| `mcp_fff`             | grep, find_files, multi_grep                                                                                                                                                                                                                | Frecency-ranked file search and content grep                    |
| `mcp_chrome_devtools` | navigate_page, take_screenshot, take_snapshot, click, fill, press_key, hover, type_text, evaluate_script, wait_for, upload_file, handle_dialog, list_console_messages, get_console_message, list_network_requests, get_network_request, ... | Browser automation, UI verification, screenshots                |
| `mcp_context7`        | resolve_library_id, query_docs                                                                                                                                                                                                              | Library documentation lookup (API refs, usage guides, examples) |

## Script patterns

```typescript
// .lootbox/scripts/search-code.ts — find definitions
const sym = await tools.mcp_codedb.symbol({ name: "handleAuth" });
console.log(sym);

// .lootbox/scripts/check-ui.ts — browser verification
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:3000" });
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(snap);

// .lootbox/scripts/lookup-docs.ts — library docs
const lib = await tools.mcp_context7.resolve_library_id({ query: "how to use React hooks", libraryName: "react" });
const docs = await tools.mcp_context7.query_docs({ libraryId: lib.libraryId, query: "useEffect cleanup" });
console.log(docs);

// .lootbox/scripts/multi-search.ts — chain tools
const files = await tools.mcp_fff.grep({ query: "deprecated" });
for (const f of files.matches || []) {
  const outline = await tools.mcp_codedb.outline({ path: f.path });
  console.log(f.path, outline);
}
```

## Commands

| Command                  | Description                           |
| ------------------------ | ------------------------------------- |
| `lootbox <script>.ts`      | Run a script from `.lootbox/scripts/`   |
| `lootbox exec 'code'`      | Inline one-liner (use sparingly)      |
| `lootbox tools`            | List namespaces                       |
| `lootbox tools types <ns>` | TypeScript signatures for a namespace |
| `lootbox scripts`          | List available scripts                |
| `just lootbox-server`      | Start server                          |
| `just lootbox-kill`        | Kill server                           |
| `just update-lootbox`      | Update binary                         |

## Key notes

- Config: `lootbox.config.json` at repo root
- Scripts: `.lootbox/scripts/` (committed to git)
- `CONTEXT7_API_KEY` is loaded from `.env.local` via direnv — never put secrets in config
- `codedb_remote` (via mcp_codedb.remote) searches actual library source code; `mcp_context7` searches documentation
- For codedb worktrees: call `mcp_codedb.index` with the worktree path first
