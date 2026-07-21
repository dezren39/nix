# MCP Health Monitoring

Lootbox automatically monitors the health of connected MCP servers and
reconnects them when they become unreachable.

## How It Works

1. **Periodic health checks** — After connecting to MCP servers, lootbox
   sends `ping()` probes at a configurable interval (default: every 30s).

2. **Failure detection** — If a ping times out or throws, the server is
   marked "unhealthy" and a reconnection is scheduled.

3. **Exponential backoff** — Reconnection attempts use exponential
   backoff: `base * 2^attempt`, capped at `maxReconnectBackoff`.

4. **Circuit breaker** — After `maxReconnectAttempts` consecutive
   failures, the server is marked "failed" and no further reconnection
   is attempted.

## Configuration

Health monitoring is automatic for all connected MCP servers. You can
tune the behavior globally (via `hazmat.server`) or per-server (via the
`health` block on each MCP server config).

### Per-Server Health Config

```json
{
  "mcpServers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "health": {
        "checkInterval": 15000,
        "maxReconnectAttempts": 10,
        "reconnectBackoffBase": 1000,
        "maxReconnectBackoff": 30000,
        "checkTimeout": 3000
      }
    }
  }
}
```

### Per-Server Health Fields

| Field                | Type   | Default | Description                             |
|----------------------|--------|---------|-----------------------------------------|
| `checkInterval`      | number | 30000   | Milliseconds between health checks      |
| `maxReconnectAttempts`| number | 5       | Max reconnect attempts (0 = unlimited)  |
| `reconnectBackoffBase`| number | 2000    | Base for exponential backoff (ms)       |
| `maxReconnectBackoff` | number | 60000   | Maximum backoff cap (ms)                |
| `checkTimeout`       | number | 5000    | Timeout for a single ping probe (ms)    |

Per-server values override the global defaults. Fields you omit fall
through to the global setting.

### Global Defaults (hazmat.server)

```json
{
  "hazmat": {
    "server": {
      "mcpHealthCheckInterval": 30000,
      "mcpMaxReconnectAttempts": 5,
      "mcpReconnectBackoffBase": 2000,
      "mcpMaxReconnectBackoff": 60000,
      "mcpHealthCheckTimeout": 5000
    }
  }
}
```

## Connection States

Each MCP server tracks one of four states:

| State          | Description                                    |
|----------------|------------------------------------------------|
| `connected`    | Healthy and responding to pings                |
| `disconnected` | Was connected but health check failed          |
| `reconnecting` | Actively attempting to reconnect               |
| `failed`       | Circuit breaker tripped, no more retries       |

## Deep Health Endpoint

The `GET /health` endpoint returns subsystem-level health including MCP
server status:

```bash
curl http://localhost:3000/health
```

```json
{
  "status": "degraded",
  "timestamp": "2026-04-07T12:00:00.000Z",
  "subsystems": {
    "workers": {
      "status": "ok",
      "total": 1,
      "ready": 1,
      "crashed": 0,
      "failed": 0,
      "starting": 0
    },
    "mcp": {
      "status": "degraded",
      "servers": {
        "filesystem": {
          "status": "connected",
          "lastHealthCheck": "2026-04-07T12:00:00.000Z",
          "reconnectAttempts": 0
        },
        "github": {
          "status": "disconnected",
          "lastHealthCheck": "2026-04-07T11:59:30.000Z",
          "reconnectAttempts": 2
        }
      }
    }
  }
}
```

The overall `status` is the worst of all subsystem statuses:
- `ok` — all subsystems healthy
- `degraded` — at least one server disconnected or reconnecting
- `unhealthy` — at least one server permanently failed

## CLI Health Command

```bash
# Pretty-print health status
lootbox health

# JSON output (for scripting)
lootbox health --json
```

**Exit codes:**
- `0` — ok
- `1` — degraded
- `2` — unhealthy
- `3` — unreachable (server not running)

## Events

The health monitor emits events that the integration manager uses for
session heartbeat updates:

| Event                  | When                                      |
|------------------------|-------------------------------------------|
| `server:healthy`       | Ping succeeded                            |
| `server:unhealthy`     | Ping failed or timed out                  |
| `server:reconnecting`  | Reconnection attempt started              |
| `server:reconnected`   | Reconnection succeeded                    |
| `server:failed`        | Circuit breaker tripped                   |
