// ── Execution response ───────────────────────────────────────────────
export interface ExecResponse {
  result?: string;
  error?: string;
  id?: string;
}

// ── MCP server configuration ─────────────────────────────────────────
export interface McpServerConfig {
  command: string;
  args: string[];
  env?: Record<string, string>;
}

// ── MCP health monitoring configuration ──────────────────────────────
/**
 * Per-server health-check and reconnection settings.
 * All fields are optional; unset values fall through to global
 * hazmat.server defaults, then to constants.ts defaults.
 */
export interface McpHealthConfig {
  /** Interval in ms between health-check probes (default 30 000). */
  checkInterval?: number;
  /** Max reconnection attempts before marking as failed; 0 = unlimited (default 5). */
  maxReconnectAttempts?: number;
  /** Base value in ms for exponential reconnection backoff (default 2 000). */
  reconnectBackoffBase?: number;
  /** Maximum backoff cap in ms for reconnection attempts (default 60 000). */
  maxReconnectBackoff?: number;
  /** Timeout in ms for a single health-check probe (default 5 000). */
  checkTimeout?: number;
}

// ── MCP multi-client configuration ───────────────────────────────────
/**
 * Per-server multi-client strategy settings.
 *
 *   "warn"        – log a warning when a conflict is detected but proceed
 *   "fail"        – refuse to connect if a conflicting session exists
 *   "auto-port"   – auto-increment the port to avoid conflicts
 *   "per-session"  – each session spawns its own server process (stdio default)
 */
export type McpMultiClientStrategy = "warn" | "fail" | "auto-port" | "per-session";

export interface McpMultiClientConfig {
  /** Conflict-resolution strategy (default "warn"). */
  strategy?: McpMultiClientStrategy;
  /** Port range for auto-port assignment, e.g. [9222, 9299] (default [9222, 9299]). */
  portRange?: [number, number];
  /** The CLI arg pattern whose port value should be rewritten, e.g. "--browserUrl". */
  portArgPattern?: string;
}

// ── Permissions ──────────────────────────────────────────────────────
/**
 * Flexible permission specification for user-script execution.
 *
 * Accepted shapes:
 *
 *   true          – apply the default permission set (--allow-net)
 *   false | null  – no extra permissions (fully sandboxed)
 *   "all"         – grant --allow-all
 *   string        – comma-separated permission tokens, e.g.
 *                   "--allow-net,--allow-read=/tmp"
 *   string[]      – ordered list of permission tokens
 *   object        – fine-grained control (see PermissionsObject)
 */
export type PermissionsConfig =
  | boolean
  | null
  | "all"
  | string
  | string[]
  | PermissionsObject;

/**
 * Object form of permissions.
 *
 *   defaults  – if true, prepend the built-in default flags first
 *   allow     – list of --allow-* tokens (shorthand: "net" => "--allow-net")
 *   deny      – list of --deny-* tokens  (shorthand: "write" => "--deny-write")
 */
export interface PermissionsObject {
  defaults?: boolean;
  allow?: string[];
  deny?: string[];
}

// ── Config file shape ("lootbox.config.json") ────────────────────────
//
// Top-level keys:
//   server   – settings consumed only by the server process
//   client   – settings consumed only by the client / exec CLI
//   global   – settings consumed by both sides
//   hazmat   – internal overrides (not documented in help/examples)
//
// All keys are optional; sensible defaults live in constants.ts.

/** Server-specific configuration. */
export interface ServerConfig {
  port?: number;
  lootboxRoot?: string;
  lootboxDataDir?: string;
  mcpServers?: Record<string, McpServerConfig>;
  /** Script-execution timeout in ms (default 10 000). */
  timeout?: number;
  /** Deno permissions for user-script execution (default true = sandbox). */
  permissions?: PermissionsConfig;
  /** RPC function-call timeout inside worker subprocesses in ms (default 30 000). */
  rpcTimeout?: number;
}

/** Client-specific configuration. */
export interface ClientConfig {
  serverUrl?: string;
  /**
   * Client-side WebSocket response timeout in ms.
   * Default: max(timeout + clientTimeoutBuffer, 30 000).
   * When set explicitly this value is used as-is.
   */
  clientTimeout?: number;
  /**
   * Buffer added to the server timeout to derive the default client timeout.
   * May be negative. Default 5 000.
   */
  clientTimeoutBuffer?: number;
  /** Delay in ms before auto-disconnecting after all calls finish (default 100). */
  autoDisconnectDelay?: number;
}

/** Settings that apply to both server and client. */
export interface GlobalConfig {
  /** Default port (used to derive serverUrl on client side). */
  port?: number;
}

/** Hazmat (internal) overrides – mirrors the same structure. */
export interface HazmatConfig {
  server?: Partial<ServerConfig & HazmatServerExtras>;
  client?: Partial<ClientConfig & HazmatClientExtras>;
  global?: Partial<GlobalConfig & HazmatGlobalExtras>;
}

/**
 * Extra fields that are only valid inside hazmat.server.
 * Placing these in the normal `server` block will trigger
 * strict-validation errors/warnings.
 */
export interface HazmatServerExtras {
  /** Max time to wait for workers to become ready in ms (default 30 000). */
  workerReadyTimeout?: number;
  /** Grace period in ms before force-killing a worker on reload (default 500). */
  workerShutdownGrace?: number;
  /** Debounce delay in ms for file-watcher events (default 100). */
  fileWatchDebounce?: number;
  /** Maximum backoff cap in ms for worker restart (default 30 000). */
  maxWorkerBackoff?: number;
  /** Circuit breaker: max worker restart attempts; 0 = unlimited (default 0). */
  maxWorkerRestarts?: number;
  /** Base value in ms for exponential worker-restart backoff (default 1 000). */
  workerBackoffBase?: number;
  /** Delay in ms after HTTP start before spawning workers (default 100). */
  serverStartDelay?: number;
  /** Polling interval in ms while waiting for worker readiness (default 100). */
  workerPollInterval?: number;
  /** SQLite database filename (default "lootbox.db"). */
  dbFilename?: string;
  /** WebSocket path for worker connections (default "/worker-ws"). */
  workerWsPath?: string;
  /** Health-check endpoint path (default "/health"). */
  healthPath?: string;
  /** File extension for tool discovery (default ".ts"). */
  toolFileExtension?: string;
  /** Title for the OpenAPI spec (default "Lootbox API"). */
  openApiTitle?: string;
  /** MCP client identity string (default "lootbox"). */
  mcpClientName?: string;

  // ── MCP health monitoring (global defaults) ──────────────────────
  /** Default interval in ms between MCP health-check probes (default 30 000). */
  mcpHealthCheckInterval?: number;
  /** Default max MCP reconnection attempts; 0 = unlimited (default 5). */
  mcpMaxReconnectAttempts?: number;
  /** Default base value in ms for MCP reconnection backoff (default 2 000). */
  mcpReconnectBackoffBase?: number;
  /** Default max backoff cap in ms for MCP reconnection (default 60 000). */
  mcpMaxReconnectBackoff?: number;
  /** Default timeout in ms for a single MCP health-check probe (default 5 000). */
  mcpHealthCheckTimeout?: number;

  // ── MCP multi-client (global default) ────────────────────────────
  /** Default multi-client strategy for all MCP servers (default "warn"). */
  mcpDefaultMultiClientStrategy?: McpMultiClientStrategy;
}

/**
 * Extra fields that are only valid inside hazmat.client.
 */
export interface HazmatClientExtras {
  /** Filename for persisted workflow state (default ".lootbox-workflow.json"). */
  workflowStateFile?: string;
  /** Delay in ms before client attempts reconnection (default 1 000). */
  reconnectDelay?: number;
}

/**
 * Extra fields that are only valid inside hazmat.global.
 */
export interface HazmatGlobalExtras {
  /** WebSocket endpoint path for client connections (default "/ws"). */
  wsPath?: string;
  /** Config filename (default "lootbox.config.json"). */
  configFilename?: string;
}

/** Root config file schema. */
export interface Config {
  server?: ServerConfig;
  client?: ClientConfig;
  global?: GlobalConfig;
  hazmat?: HazmatConfig;

  // ── Legacy flat keys (still read for backward compat) ──────────────
  port?: number;
  serverUrl?: string;
  lootboxRoot?: string;
  lootboxDataDir?: string;
  mcpServers?: Record<string, McpServerConfig>;
  timeout?: number;
  /** @deprecated Use `server.permissions` instead. */
  sandbox?: boolean;
}

// ── Resolved config (output of get_config) ───────────────────────────
/** Fully resolved, validated configuration used at runtime. */
export interface ResolvedConfig {
  // Paths
  tools_dir: string;
  workflows_dir: string;
  scripts_dir: string;

  // Server
  port: number;
  lootbox_data_dir: string | null;
  mcp_servers: Record<string, McpServerConfig> | null;
  timeout: number;
  rpc_timeout: number;
  worker_ready_timeout: number;

  /**
   * Pre-computed Deno CLI flags for user-script permissions.
   * e.g. ["--allow-net", "--allow-import=localhost:3000"]
   * An empty array means no extra permissions.
   * Contains "--allow-all" when full access is requested.
   */
  permission_flags: string[];

  // Server – hazmat (resolved, not behind .hazmat)
  worker_shutdown_grace: number;
  file_watch_debounce: number;
  max_worker_backoff: number;
  max_worker_restarts: number;
  worker_backoff_base: number;
  server_start_delay: number;
  worker_poll_interval: number;
  db_filename: string;
  worker_ws_path: string;
  health_path: string;
  tool_file_extension: string;
  openapi_title: string;
  mcp_client_name: string;

  // MCP health monitoring (global defaults, resolved)
  mcp_health_check_interval: number;
  mcp_max_reconnect_attempts: number;
  mcp_reconnect_backoff_base: number;
  mcp_max_reconnect_backoff: number;
  mcp_health_check_timeout: number;

  // MCP multi-client (global default, resolved)
  mcp_default_multi_client_strategy: McpMultiClientStrategy;

  // Client
  server_url: string;
  client_timeout: number;
  auto_disconnect_delay: number;

  // Client – hazmat (resolved)
  workflow_state_file: string;
  reconnect_delay: number;

  // Global – hazmat (resolved)
  ws_path: string;
}

// ── Workflow state ───────────────────────────────────────────────────
export interface FlowState {
  file: string;
  section: number;
  loopIteration?: number;
  sessionId?: string;
}
