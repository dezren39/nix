---
name: mcpx-claude
description: >-
  Use mcpx from Claude Code and Anthropic agents. Covers CLI Code Mode execution
  via Bash, single-roundtrip multi-tool scripting, configuring mcpx as an MCP
  server, and session leasing for browsers and developer tools.
---

# mcpx in Claude Code

Claude Code agents connect to the `mcpx` ecosystem either via the `Bash` tool using `mcpx` CLI commands or as a unified MCP server proxy.

---

## 1. Why mcpx for Claude?

When dozens of MCP servers are configured directly in Claude (`claude mcp add` / `mcpServers`), every prompt pays the context cost of every tool's JSON schema definitions, which can easily take up 20k–50k tokens on every turn.

`mcpx` solves this:
- **Zero Schema Overhead**: Claude only accesses the tools it needs when it needs them.
- **Single-Roundtrip Multi-Tool Chaining**: Instead of 5 separate tool calls with 5 roundtrips, write a single TypeScript script that executes in `mcpx`.
- **Session Leasing**: Stateful tools (like Chrome browser automation) get dedicated processes that do not interfere with parallel Claude sessions.

---

## 2. Using mcpx via Bash Tool

### Step 1: Discover Tools
Run `mcpx ls` to see what servers are registered:
```bash
mcpx ls
```

To see the exact TypeScript types and function signatures for a namespace:
```bash
mcpx types codedb
mcpx types fff
mcpx types chrome-devtools
```

### Step 2: Run Scripts with `mcpx exec`
Use top-level `await` and `tools.<server>.<method>()`. Only printed output (`console.log`) is returned to Claude:

```bash
mcpx exec '
  const files = await tools.fff.grep({ query: "TODO: security" });
  console.log(files.matches?.map(m => m.path).join("\n"));
'
```

### Calling Single Tools
```bash
mcpx call codedb.symbol '{"name": "AuthService"}'
```

---

## 3. Configuring mcpx as a Direct MCP Server in Claude

If you prefer Claude to have standard MCP tools directly exposed via `mcpx`:
Add `mcpx` to Claude's config (`~/.claude.json`):
```json
{
  "mcpServers": {
    "mcpx": {
      "command": "mcpx",
      "args": ["serve"]
    }
  }
}
```
This runs `mcpx serve`, which acts as an MCP gateway presenting all pooled MCP servers to Claude through a unified client connection.
