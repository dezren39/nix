---
name: mcpx-opencode2
description: >-
  Use mcpx with OpenCode 2.x and OpenCode forks. Covers native tools (mcpx_exec,
  mcpx_discover, mcpx_observe), session leasing, Code Mode token optimization,
  and daemon management. Use when running in OpenCode to interact with MCP
  servers, browser automation, codebase search, and multi-server workflows.
---

# mcpx in OpenCode 2

OpenCode 2 connects to `mcpx` either natively through the session plugin (`.opencode/plugin/mcpx-session.ts`) or via the `mcpx` CLI in bash.

The session plugin exposes native agent tools and automatically injects session identity into every subshell command so servers like browsers are isolated per session.

---

## 1. Native Plugin Tools

When configured in `.opencode/opencode.jsonc`:
```jsonc
{
  "plugin": [
    ["./.opencode/plugin/mcpx-session.ts", { "tools": true, "instructions": true }]
  ]
}
```

OpenCode receives the following native tools:

### `mcpx_discover`
Lists registered MCP server namespaces or shows typed signatures for a namespace.
- No args: `mcpx_discover({})` lists all active namespaces and tool counts.
- With namespace: `mcpx_discover({ namespace: "chrome-devtools" })` returns the TypeScript function signatures.

### `mcpx_exec`
Executes TypeScript directly in `mcpx` against all active MCP servers simultaneously.
- **Top-level `await` is supported.**
- Tools are available on the global `tools.<namespace>.<toolName>(args)` object.
- **Context Token Saver**: Only what you output (`console.log(...)` or returned value) enters your conversation context. Instead of pulling a 500 KB JSON payload into the prompt, filter and aggregate in TypeScript.

### `mcpx_observe`
Queries the daemon's durable log and runtime metrics.
- `mcpx_observe({ what: "log", since: "15m" })`: Inspect recent tool execution logs, parameters, and errors.
- `mcpx_observe({ what: "slowest" })` or `mcpx_observe({ what: "errors" })`: Get aggregate performance and failure statistics.

### `mcpx_daemon_status` & `mcpx_daemon_select`
Discover and choose between multiple running mcpx daemons on the host or in containers.

---

## 2. Code Mode: Maximizing Context Efficiency

Instead of invoking multiple individual MCP tool calls across multiple turns:
1. Discover the tool signatures with `mcpx_discover({ namespace: "<server>" })`.
2. Write a single TypeScript script with `mcpx_exec` to call, filter, and extract only the needed fields.

### Example: Searching and Filtering with Codedb & FFF
```typescript
// Call via mcpx_exec:
const matches = await tools.fff.grep({ query: "interface User" });
const files = (matches.matches || []).slice(0, 5);

const outlines = [];
for (const file of files) {
  const outline = await tools.codedb.outline({ path: file.path });
  outlines.push({ path: file.path, outline });
}

console.log(JSON.stringify(outlines, null, 2));
```

### Example: Isolated Browser Automation
`chrome-devtools` is leased exclusively per session. Your browser state (cookies, open tabs, navigations) will not collide with other agent sessions running simultaneously.

```typescript
// Navigate and capture error logs
await tools["chrome-devtools"].navigate_page({ url: "http://localhost:3000" });
const errors = await tools["chrome-devtools"].list_console_messages({ types: ["error"] });
console.log(JSON.stringify(errors, null, 2));
```

---

## 3. Terminal / CLI Fallback

If you execute commands in the terminal, the plugin has already injected `MCPX_SESSION_ID` into your environment. You can use standard CLI commands:

```bash
# List all MCP namespaces
mcpx ls

# Check signatures
mcpx types codedb

# Run an inline script
mcpx exec 'console.log(await tools.codedb.status({}))'

# Run a script file
mcpx exec path/to/script.ts
```
