---
name: mcpx-agy
description: >-
  Use mcpx from Google Antigravity (AGY). Covers CLI execution, daemon socket
  interaction, TypeScript Code Mode for minimal transcript bloat, and accessing
  consolidated MCP servers across background tasks and subagents.
---

# mcpx in Antigravity (AGY)

In Antigravity, agents can communicate with MCP servers through the centralized `mcpx` daemon. This keeps the agent's MCP tool count low while providing full access to all developer tools (Codedb, FFF, Chrome DevTools, Context7, Jules, Neon, Desktop Commander, etc.).

---

## 1. Context Efficiency in AGY

Calling individual MCP tools that return large JSON responses causes transcripts (`transcript.jsonl` / `transcript_full.jsonl`) to grow rapidly.

Instead of calling verbose MCP tools directly:
- Use `run_command` with `mcpx exec` to execute TypeScript that transforms and aggregates data on the daemon side before returning output.

---

## 2. Using mcpx via `run_command`

### Discovery
```bash
# Check running servers
mcpx ls

# Check TypeScript signatures
mcpx types codedb
mcpx types context7
```

### Executing Code Mode Scripts
```bash
mcpx exec '
  const status = await tools.codedb.status({});
  console.log("Indexed files:", status.fileCount);
'
```

### Chaining Across Multiple Servers
```bash
mcpx exec '
  const doc = await tools.context7.query_docs({
    libraryId: "react",
    query: "useCallback dependencies"
  });
  console.log(doc.results?.[0]?.snippet ?? "Not found");
'
```

---

## 3. Daemon REST / Socket Access

The `mcpx` daemon publishes an HTTP API on its local unix socket or localhost port:
- `GET /v1/namespaces`: List active namespaces
- `POST /v1/exec`: Run TypeScript against tools
- `GET /v1/log`: Inspect durable execution log

When debugging a failing tool without re-running destructive operations:
```bash
mcpx log --since 10m
```
