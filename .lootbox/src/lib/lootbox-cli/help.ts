import { DEFAULT_PORT } from "../constants.ts";

export function showLlmHelp() {
  console.log(`lootbox - Script Execution Reference

Sandboxed TypeScript runtime for executing scripts with network access and
discoverable tool functions.

DISCOVERY:
  lootbox tools                     List available function namespaces
  lootbox tools types <ns1,ns2>     Get TypeScript signatures
  lootbox scripts                   List available scripts with examples

EXECUTION:
  lootbox script.ts                 Execute TypeScript file
  lootbox exec 'code'               Execute inline code
  cat file.ts | lootbox             Execute from stdin

AVAILABLE APIS:
  tools.<namespace>.<function>({ arg: value })
  console.log() / console.error()
  fetch(url, options)               HTTP requests
  Promise.all([...])                Parallel execution
  stdin(default = "")               Access piped stdin data
    .text()                         Returns trimmed text
    .json()                         Returns parsed JSON or null
    .lines()                        Returns array of non-empty lines
    .raw()                          Returns raw input

CONSTRAINTS:
  \u2022 Configurable execution timeout (default: 10 seconds)
  \u2022 Permission-controlled execution for safety (configurable)

EXAMPLES:
  # Discover and use tools
  lootbox tools
  lootbox tools types namespace1
  lootbox exec 'console.log(await tools.namespace1.func({arg: "value"}))'

  # Parallel execution
  lootbox exec 'const [r1, r2] = await Promise.all([tools.ns1.f1({}), tools.ns2.f2({})])'

  # Process piped data
  cat data.json | lootbox exec 'console.log(stdin().json())'

  # Composability
  lootbox script1.ts | jq '.data' | lootbox script2.ts

WORKFLOW EXECUTION:
  workflow step                             Execute current workflow step
  workflow step --end-loop="reason"         Advance from loop (after min iterations)
  workflow abort --abort="reason"           Abort workflow with reason
  workflow status                           Check workflow position

COMMAND-SPECIFIC HELP:
  lootbox tools --llm       Detailed tool discovery help
  lootbox scripts --llm     Detailed script management help
  lootbox workflow --llm    Detailed workflow execution help
  lootbox health            Server health status (--json for machine-readable)
`);
}

export function showHumanHelp() {
  console.log(`lootbox - Sandboxed TypeScript runtime with network access

Write scripts with fetch() for web requests and the 'tools' object for
additional capabilities. Permission-controlled execution keeps your system
safe while you orchestrate, fetch, and transform data.

Usage:
  lootbox [OPTIONS] [FILE]
  lootbox exec <code>
  lootbox tools [subcommand]
  lootbox scripts [subcommand]
  lootbox workflow <command> [args]
  lootbox server [OPTIONS]
  lootbox health [--json]

Execution Environment:
  \u2022 Runtime: Deno with TypeScript support
  \u2022 Network: fetch() available for HTTP requests
  \u2022 Permissions: Configurable Deno permissions (default: --allow-net only)
  \u2022 Timeout: Configurable execution limit (default: 10 seconds)
  \u2022 Global APIs: console, fetch, Promise, standard JavaScript/TypeScript APIs

Function Library (tools object):
  The 'tools' object provides access to functions organized by namespace.
  Available namespaces depend on your configuration.
  Syntax: tools.<namespace>.<function>({ args })

  Examples:
    tools.namespace1.functionName({ arg1: value1, arg2: value2 })
    tools.namespace2.anotherFunction({ param: "value" })

  Discovery:
    lootbox tools              List all available namespaces
    lootbox tools types <ns>   See TypeScript signatures for namespace

Options:
  -s, --server <url>          WebSocket server URL (default: ws://localhost:${DEFAULT_PORT}/ws)
  --config <path>             Path to config file (default: lootbox.config.json)
  --timeout <ms>              Script execution timeout in milliseconds
  --rpc-timeout <ms>          RPC function call timeout in milliseconds
  --client-timeout <ms>       Client-side response timeout in milliseconds
  --no-sandbox                Grant full Deno permissions (--allow-all)
  --allow-<perm>[=value]      Add a Deno --allow-* permission flag
  --deny-<perm>[=value]       Add a Deno --deny-* permission flag
  --config-help               Show configuration file information
  --llm-help                  Show LLM-focused help (command index)
  -h, --help                  Show this help message
  -v, --version               Show version

Workflow Commands:
  workflow start <file>                   Start a new workflow from a YAML file
  workflow step                           Show/repeat current step
  workflow step --end-loop="reason"       End loop early and advance with reason (only after min iterations)
  workflow abort --abort="reason"         Abort workflow with reason
  workflow reset                          Reset workflow to the beginning
  workflow status                         Show current workflow status

Script Management:
  scripts                                 List all available scripts with descriptions
  scripts init <filename>                 Create new script from template (auto-adds .ts extension)

Server Commands:
  server                      Start the WebSocket RPC server
    --port <port>             Server port (default: ${DEFAULT_PORT})
    --lootbox-root <path>     Lootbox root directory (default: .lootbox)
    --lootbox-data-dir <path> Data directory (optional, defaults to ~/.local/share/lootbox)
    --timeout <ms>            Script execution timeout (default: 10000)
    --rpc-timeout <ms>        RPC call timeout (default: 30000)
    --config <path>           Path to config file
    --no-sandbox              Grant full permissions to user scripts
    --allow-<perm>            Add Deno permission
    --deny-<perm>             Add Deno deny permission

Health Commands:
  health                      Pretty-print server health status
  health --json               JSON output (for scripting)
                              Exit codes: 0=ok, 1=degraded, 2=unhealthy, 3=unreachable

Tool Discovery:
  lootbox tools                            # List all available namespaces
  lootbox tools types sqlite,fetch         # Get TypeScript types
  lootbox tools --llm                      # LLM-focused help

Execution:
  lootbox script.ts                        # Execute script file
  lootbox exec 'console.log("Hello")'      # Execute inline code
  cat data.json | lootbox script.ts        # Pipe data to script

Examples:
  # Using tools in inline code
  lootbox exec 'console.log(await tools.sqlite.query({ sql: "SELECT 1" }))'

  # Parallel execution
  lootbox exec 'const [r1, r2] = await Promise.all([tools.ns1.f1({}), tools.ns2.f2({})]); console.log(r1, r2)'

  # Using fetch
  lootbox exec 'const data = await fetch("https://api.example.com").then(r => r.json()); console.log(data)'

  # Workflow execution
  lootbox workflow start tutorial.yaml
  lootbox workflow step                                    # Show/repeat current step
  lootbox workflow step --end-loop="completed the task"   # End loop early with reason
  lootbox workflow abort --abort="switching approach"     # Abort workflow
  lootbox workflow status                                 # Check progress

  # Script management
  lootbox scripts                       # List all available scripts
  lootbox scripts init fetch-data       # Create new script (auto-adds .ts)
  lootbox fetch-data.ts                 # Run the script

  # Server mode
  lootbox server                        # Uses defaults (port ${DEFAULT_PORT}, ./lootbox/tools)
  lootbox server --port 9000            # Custom port
  lootbox server --timeout 60000        # 60-second script timeout
  lootbox server --no-sandbox           # Full permissions for scripts

  # Workflow file format (YAML):
  # steps:
  #   - title: Step name
  #     prompt: |
  #       Instructions for this step
  #   - title: Loop example
  #     loop: { min: 2, max: 5 }
  #     prompt: |
  #       This step repeats 2-5 times. Use 'workflow step' to repeat,
  #       or 'workflow step --end-loop="reason"' to advance (after min 2 iterations)
`);
}

export function showConfigHelp() {
  console.log(`lootbox - Configuration

Create a lootbox.config.json file in your project directory (or specify
--config <path>) to configure server, client, and execution settings.
All settings are optional with sensible defaults.

Configuration File:
  Default: lootbox.config.json (in current directory)
  Override: --config <path>
  Format: JSON

Example:
  {
    "server": {
      "port": ${DEFAULT_PORT},
      "lootboxRoot": ".lootbox",
      "timeout": 30000,
      "rpcTimeout": 60000,
      "permissions": true,
      "mcpServers": {
        "filesystem": {
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path"]
        }
      }
    },
    "client": {
      "clientTimeout": 35000,
      "clientTimeoutBuffer": 5000
    },
    "global": {
      "port": ${DEFAULT_PORT}
    }
  }

Server Settings (server.*):
  port              Server port (default: ${DEFAULT_PORT})
  lootboxRoot       Root directory for lootbox files (default: .lootbox)
                    Contains: tools/, workflows/, scripts/
  lootboxDataDir    Internal data directory (default: ~/.local/share/lootbox)
  mcpServers        MCP server definitions (see MCP Config below)
  timeout           Script execution timeout in ms (default: 10000)
                    CLI: --timeout <ms>
  rpcTimeout        RPC function call timeout in ms (default: 30000)
                    CLI: --rpc-timeout <ms>
  permissions       Deno permissions for user scripts (see below)

Client Settings (client.*):
  serverUrl           Override WebSocket URL (e.g., wss://remote:${DEFAULT_PORT}/ws)
                      CLI: --server-url <url> or -s <url>
  clientTimeout       Client response timeout in ms
                      Default: max(timeout + clientTimeoutBuffer, 30000)
                      CLI: --client-timeout <ms>
  clientTimeoutBuffer Extra ms added to server timeout for client timeout
                      May be negative. Default: 5000.
                      CLI: --client-timeout-buffer <ms>

Global Settings (global.*):
  port              Default port used by both server and client

MCP Server Config (server.mcpServers.{name}.*):
  Each MCP server supports these fields:

  Stdio transport (default):
    command           Command to launch the server (required)
    args              Array of command-line arguments
    env               Environment variables (key-value object)

  HTTP/SSE transport:
    transport         "streamable_http" or "sse"
    url               Server URL (required for HTTP/SSE)

  Per-server health monitoring (health.*):
    checkInterval         Health check interval in ms (default: 30000)
    maxReconnectAttempts  Max reconnect attempts, 0=unlimited (default: 5)
    reconnectBackoffBase  Backoff base in ms (default: 2000)
    maxReconnectBackoff   Max backoff cap in ms (default: 60000)
    checkTimeout          Single probe timeout in ms (default: 5000)

  Multi-client conflict resolution (multiClient.*):
    strategy          "warn" | "fail" | "auto-port" | "per-session" (default: "warn")
    portRange         [start, end] port range for auto-port (default: [9222, 9299])
    portArgPattern    CLI flag pattern for port rewriting (e.g., "--browserUrl")

Permissions:
  Controls Deno permissions for user-script execution.

  true              Apply defaults (--allow-net only) [default]
  false / null      No extra permissions (fully sandboxed)
  "all"             Grant --allow-all (full access)
  "net,read=/tmp"   Comma-separated permission tokens
  ["--allow-net", "--allow-read=/tmp"]   Array of flags
  {                 Object form:
    "defaults": true,            Prepend default flags
    "allow": ["net", "read"],    --allow-net, --allow-read
    "deny": ["write"]            --deny-write
  }

  CLI flags --allow-* and --deny-* append to config permissions.
  --no-sandbox overrides everything with --allow-all.

Hazmat Overrides (hazmat.server.*):
  Advanced settings for MCP health monitoring defaults:
    mcpHealthCheckInterval      Global health check interval (default: 30000)
    mcpMaxReconnectAttempts     Global max reconnect attempts (default: 5)
    mcpReconnectBackoffBase     Global backoff base (default: 2000)
    mcpMaxReconnectBackoff      Global max backoff cap (default: 60000)
    mcpHealthCheckTimeout       Global probe timeout (default: 5000)
    mcpDefaultMultiClientStrategy  Default strategy (default: "warn")

  Other hazmat overrides:
    workerReadyTimeout, workerShutdownGrace, fileWatchDebounce,
    maxWorkerBackoff, maxWorkerRestarts, workerBackoffBase

Health Command:
  lootbox health              Pretty-print server health
  lootbox health --json       JSON output for scripting
  Exit codes: 0=ok, 1=degraded, 2=unhealthy, 3=unreachable

Priority (for all settings):
  CLI flags > config file > defaults

  Full priority chain:
    CLI flag > hazmat.{server,client,global} > {server,client,global}
    > legacy flat keys > built-in defaults (constants.ts)

  Type boundaries (ServerConfig, ClientConfig, HazmatServerExtras, etc.)
  enforce which keys belong where at compile time. Unknown JSON keys are
  silently ignored at runtime.

Settings That Can ONLY Be Set in Config (no CLI flag):
  server.mcpServers, server.permissions (object form),
  all hazmat.* overrides

Settings That Can ONLY Be Set via CLI (not in config):
  --no-sandbox (applies --allow-all; config equivalent: permissions: "all")
  --eval / -e (inline code to execute)
  --help, --version, --llm-help, --config-help

Legacy flat keys (port, timeout, sandbox, etc.) are still read for
backward compatibility but the structured form is preferred.

The config file is optional. If not found, defaults will be used.
`);
}
