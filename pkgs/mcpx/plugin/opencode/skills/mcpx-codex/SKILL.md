---
name: mcpx-codex
description: >-
  Use mcpx from OpenAI Codex / Codex CLI agents. Covers executing scripts via
  the mcpx CLI, TypeScript filtering to save context tokens, session management,
  and executing across pooled and exclusively leased MCP servers.
---

# mcpx in Codex

Codex agents interact with the unified MCP server ecosystem via the `mcpx` CLI tool. All MCP servers (Codedb, FFF, Chrome DevTools, Context7, Jules, Neon, etc.) are managed by the `mcpx` daemon.

---

## 1. Why mcpx for Codex?

Standard MCP exposes dozens of complex tool schemas directly into the prompt context, burning tens of thousands of tokens before execution even begins.

With `mcpx`:
1. Schemas are queried on demand via `mcpx types <server>`.
2. Multiple tools are chained and filtered inside TypeScript using `mcpx exec`.
3. Only the final formatted result enters the Codex conversation context.

---

## 2. CLI Workflow

### Discover Available Tools
```bash
# List all active MCP server namespaces
mcpx ls

# View TypeScript interfaces and methods for a specific server
mcpx types codedb
mcpx types fff
mcpx types chrome-devtools
```

### Running Scripts (Code Mode)
Write scripts that call `tools.<server>.<method>()`:

```bash
# Inline execution
mcpx exec 'console.log(await tools.codedb.status({}))'

# Multi-step script with filtering
cat << 'EOF' | mcpx exec -
const res = await tools.fff.grep({ query: "TODO" });
const topFiles = (res.matches || []).slice(0, 10).map(m => m.path);
console.log(JSON.stringify(topFiles));
EOF
```

### Direct Tool Calling
To invoke a single tool without writing TypeScript:
```bash
mcpx call codedb.symbol '{"name": "handleAuth"}'
```

---

## 3. Session Isolation & Browser Control

Servers like `chrome-devtools` are pooled exclusively per session. By default, `mcpx` assigns a session based on the environment. If managing a distinct long-running task:

```bash
export MCPX_SESSION_ID="codex-task-$(date +%s)"
mcpx exec 'await tools["chrome-devtools"].navigate_page({ url: "http://localhost:3000" })'
```

---

## 4. Diagnostics & Debugging

If a tool call hangs or returns an unexpected error:
```bash
# View last 15 minutes of mcpx execution logs
mcpx log --since 15m

# Check slowest operations
mcpx stats slowest
```
