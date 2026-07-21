/**
 * Default constants for lootbox configuration.
 *
 * All hardcoded values that were previously scattered across the codebase
 * are centralized here. These serve as the base defaults; they can be
 * overridden via lootbox.config.json (or --config <path>) and CLI flags.
 *
 * "hazmat" overrides in the config can replace any of these at the user's
 * own risk.
 */

// ── Server defaults ──────────────────────────────────────────────────
/** TCP port the WebSocket RPC server listens on. */
export const DEFAULT_PORT = 3000;

/** Script-execution timeout in milliseconds (server-side). */
export const DEFAULT_TIMEOUT_MS = 10_000;

/** RPC function-call timeout inside worker processes, in milliseconds. */
export const DEFAULT_RPC_TIMEOUT_MS = 30_000;

/** Maximum time to wait for all workers to become ready, in milliseconds. */
export const DEFAULT_WORKER_READY_TIMEOUT_MS = 30_000;

/** Grace period in ms before force-killing a worker on reload. */
export const DEFAULT_WORKER_SHUTDOWN_GRACE_MS = 500;

/** Debounce delay in ms for file-watcher events. */
export const DEFAULT_FILE_WATCH_DEBOUNCE_MS = 100;

/** Maximum backoff cap in ms for worker restart attempts. */
export const DEFAULT_MAX_WORKER_BACKOFF_MS = 30_000;

/** Circuit breaker: max worker restart attempts (0 = unlimited). */
export const DEFAULT_MAX_WORKER_RESTARTS = 0;

/** Base value in ms for exponential worker-restart backoff. */
export const DEFAULT_WORKER_BACKOFF_BASE_MS = 1_000;

/** Delay in ms after HTTP server starts before spawning workers. */
export const DEFAULT_SERVER_START_DELAY_MS = 100;

/** Polling interval in ms while waiting for worker readiness. */
export const DEFAULT_WORKER_POLL_INTERVAL_MS = 100;

// ── Client defaults ──────────────────────────────────────────────────
/**
 * Additional milliseconds added on top of `timeout` for the client-side
 * WebSocket response timeout.  The effective client timeout is:
 *
 *   max(timeout + clientTimeoutBuffer, CLIENT_TIMEOUT_FLOOR_MS)
 *
 * May be negative to shrink the client timeout below the server timeout
 * (not recommended).
 */
export const DEFAULT_CLIENT_TIMEOUT_BUFFER_MS = 5_000;

/**
 * Absolute floor for the client timeout so that even a very small
 * server timeout does not make the client give up too early.
 */
export const CLIENT_TIMEOUT_FLOOR_MS = 30_000;

/** Delay in ms before client auto-disconnects after all calls finish. */
export const DEFAULT_AUTO_DISCONNECT_DELAY_MS = 100;

/** Delay in ms before client attempts WebSocket reconnection. */
export const DEFAULT_RECONNECT_DELAY_MS = 1_000;

// ── Permissions defaults ─────────────────────────────────────────────
/**
 * Default Deno permission flags applied to user-script execution when
 * `permissions` is unset or `true` in the config.
 *
 * These mirror the original "sandbox" behaviour: network access only,
 * plus the dynamic `--allow-import=localhost:<port>`.
 */
export const DEFAULT_PERMISSION_FLAGS: readonly string[] = ["--allow-net"];

// ── Path / route defaults ────────────────────────────────────────────
/**
 * Legacy config file name in the current working directory.
 * First candidate in the config search chain (see get_config.ts discoverConfigFile).
 */
export const DEFAULT_CONFIG_FILENAME = "lootbox.config.json";

/** SQLite database filename inside the data directory. */
export const DEFAULT_DB_FILENAME = "lootbox.db";

/** WebSocket endpoint path for client connections. */
export const DEFAULT_WS_PATH = "/ws";

/** WebSocket endpoint path for worker connections. */
export const DEFAULT_WORKER_WS_PATH = "/worker-ws";

/** Health-check HTTP endpoint path. */
export const DEFAULT_HEALTH_PATH = "/health";

/** File extension used for tool/RPC file discovery. */
export const DEFAULT_TOOL_FILE_EXTENSION = ".ts";

/** Title used in the generated OpenAPI specification. */
export const DEFAULT_OPENAPI_TITLE = "Lootbox API";

/** Filename for persisted workflow state (client-side). */
export const DEFAULT_WORKFLOW_STATE_FILE = ".lootbox-workflow.json";

/** Identity string sent as MCP client name. */
export const DEFAULT_MCP_CLIENT_NAME = "lootbox";

// ── MCP health monitoring defaults ──────────────────────────────────
/** Interval in ms between MCP server health checks. */
export const DEFAULT_MCP_HEALTH_CHECK_INTERVAL_MS = 30_000;

/** Circuit breaker: max MCP reconnection attempts (0 = unlimited). */
export const DEFAULT_MCP_MAX_RECONNECT_ATTEMPTS = 5;

/** Base value in ms for exponential MCP reconnection backoff. */
export const DEFAULT_MCP_RECONNECT_BACKOFF_BASE_MS = 2_000;

/** Maximum backoff cap in ms for MCP reconnection attempts. */
export const DEFAULT_MCP_MAX_RECONNECT_BACKOFF_MS = 60_000;

/** Timeout in ms for a single MCP health-check probe. */
export const DEFAULT_MCP_HEALTH_CHECK_TIMEOUT_MS = 5_000;

// ── MCP multi-client defaults ───────────────────────────────────────
/**
 * Default strategy when multiple lootbox instances configure the same
 * MCP server. "warn" logs a warning but proceeds; "fail" refuses to
 * connect; "auto-port" auto-increments the port; "per-session" spawns
 * an independent server process per session.
 */
export const DEFAULT_MCP_MULTI_CLIENT_STRATEGY = "warn" as const;

/**
 * Default port range for auto-port assignment.
 * Only used when multiClient.strategy is "auto-port".
 */
export const DEFAULT_MCP_AUTO_PORT_RANGE: readonly [number, number] = [9222, 9299];

/** Directory name (relative to data dir) for MCP session registry files. */
export const DEFAULT_MCP_SESSIONS_DIR = "mcp-sessions";

