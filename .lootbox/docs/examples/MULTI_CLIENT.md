# MCP Multi-Client Strategies

When multiple lootbox instances run simultaneously and configure the
same MCP server, port conflicts can occur (especially for servers that
listen on a fixed TCP port, like `chrome-devtools-mcp`).

The **multi-client strategy** controls how lootbox handles this.

## Strategies

### `warn` (default)

Log a warning if another instance is already using the same MCP server,
but proceed anyway. The connection may fail if the server's port is
already bound.

```json
{
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": ["chrome-devtools-mcp@latest"],
      "multiClient": {
        "strategy": "warn"
      }
    }
  }
}
```

### `fail`

Refuse to connect if another instance is already using the same server.
This is useful when running duplicate servers would cause corruption or
confusion.

```json
{
  "multiClient": {
    "strategy": "fail"
  }
}
```

### `auto-port`

Automatically assign a different port from a configured range. Lootbox
checks a session registry and TCP port availability to find the next
free port, then rewrites the server's command-line arguments to use it.

```json
{
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": ["chrome-devtools-mcp@latest", "--remote-debugging-port=9222"],
      "multiClient": {
        "strategy": "auto-port",
        "portRange": [9222, 9299],
        "portArgPattern": "--remote-debugging-port"
      }
    }
  }
}
```

**How auto-port works:**

1. Check the session registry for ports already claimed by other
   instances.
2. For each candidate port (starting from the preferred port in args),
   verify it's not in the registry AND not bound at the OS level.
3. Rewrite the server's args to use the free port.
4. Register the port in the session registry.

**Arg rewriting patterns supported:**

| Pattern             | Example                                       |
|---------------------|-----------------------------------------------|
| `--flag=PORT`       | `--remote-debugging-port=9222` → `=9223`      |
| `--flag PORT`       | `--port 9222` → `--port 9223`                 |
| URL-embedded port   | `http://localhost:9222` → `http://localhost:9223` |
| Bare number (only with `portArgPattern`) | `9222` → `9223`        |

### `per-session`

Spawn an independent MCP server process for each lootbox session.
Each process is isolated and does not share state with other instances.

```json
{
  "multiClient": {
    "strategy": "per-session"
  }
}
```

## Configuration

### Per-Server

```json
{
  "mcpServers": {
    "server-name": {
      "command": "...",
      "args": ["..."],
      "multiClient": {
        "strategy": "warn | fail | auto-port | per-session",
        "portRange": [9222, 9299],
        "portArgPattern": "--browserUrl"
      }
    }
  }
}
```

| Field           | Type              | Default        | Description                              |
|-----------------|-------------------|----------------|------------------------------------------|
| `strategy`      | string            | `"warn"`       | One of: warn, fail, auto-port, per-session |
| `portRange`     | [number, number]  | [9222, 9299]   | Port range for auto-port scanning        |
| `portArgPattern`| string            | —              | CLI flag pattern to match for port rewriting |

### Global Default Strategy

```json
{
  "hazmat": {
    "server": {
      "mcpDefaultMultiClientStrategy": "warn"
    }
  }
}
```

## Session Registry

The session registry (`mcp-sessions/` directory) tracks which MCP
servers are running, which ports they use, and which lootbox process
owns them.

**Registry entries are automatically cleaned up:**
- On graceful shutdown, lootbox deregisters its sessions.
- On startup, stale entries from dead PIDs are pruned.
- Heartbeat updates keep entries fresh while the server is healthy.

**Registry location:** `{lootboxDataDir}/mcp-sessions/`

See [KNOWN_LIMITATIONS.md](../../src/lib/external-mcps/KNOWN_LIMITATIONS.md)
for documented trade-offs in the registry implementation.

## Transport Types

MCP servers can use three transport types:

### stdio (default)

The server is launched as a child process communicating over stdin/stdout.

```json
{
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path"]
}
```

### streamable_http

Connect to a remote MCP server over HTTP.

```json
{
  "transport": "streamable_http",
  "url": "https://mcp.example.com/api"
}
```

### sse (Server-Sent Events)

Connect to a legacy MCP server using SSE.

```json
{
  "transport": "sse",
  "url": "https://mcp-legacy.example.com/events"
}
```
