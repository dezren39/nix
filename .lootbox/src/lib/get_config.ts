import { parseArgs } from "@std/cli";
import { exists } from "https://deno.land/std@0.208.0/fs/mod.ts";
import type {
  ClientConfig,
  Config,
  GlobalConfig,
  HazmatClientExtras,
  HazmatGlobalExtras,
  HazmatServerExtras,
  McpMultiClientStrategy,
  McpServerConfig,
  PermissionsConfig,
  ResolvedConfig,
  ServerConfig,
} from "./lootbox-cli/types.ts";
import {
  getUserLootboxToolsDir,
  getUserLootboxWorkflowsDir,
  getUserLootboxScriptsDir,
  getHomeDir,
} from "./paths.ts";
import { dirname, join } from "https://deno.land/std@0.208.0/path/mod.ts";
import {
  DEFAULT_PORT,
  DEFAULT_TIMEOUT_MS,
  DEFAULT_RPC_TIMEOUT_MS,
  DEFAULT_WORKER_READY_TIMEOUT_MS,
  DEFAULT_WORKER_SHUTDOWN_GRACE_MS,
  DEFAULT_FILE_WATCH_DEBOUNCE_MS,
  DEFAULT_MAX_WORKER_BACKOFF_MS,
  DEFAULT_MAX_WORKER_RESTARTS,
  DEFAULT_WORKER_BACKOFF_BASE_MS,
  DEFAULT_SERVER_START_DELAY_MS,
  DEFAULT_WORKER_POLL_INTERVAL_MS,
  DEFAULT_CLIENT_TIMEOUT_BUFFER_MS,
  CLIENT_TIMEOUT_FLOOR_MS,
  DEFAULT_AUTO_DISCONNECT_DELAY_MS,
  DEFAULT_RECONNECT_DELAY_MS,
  DEFAULT_PERMISSION_FLAGS,
  DEFAULT_CONFIG_FILENAME,
  DEFAULT_DB_FILENAME,
  DEFAULT_WS_PATH,
  DEFAULT_WORKER_WS_PATH,
  DEFAULT_HEALTH_PATH,
  DEFAULT_TOOL_FILE_EXTENSION,
  DEFAULT_OPENAPI_TITLE,
  DEFAULT_WORKFLOW_STATE_FILE,
  DEFAULT_MCP_CLIENT_NAME,
  DEFAULT_MCP_HEALTH_CHECK_INTERVAL_MS,
  DEFAULT_MCP_MAX_RECONNECT_ATTEMPTS,
  DEFAULT_MCP_RECONNECT_BACKOFF_BASE_MS,
  DEFAULT_MCP_MAX_RECONNECT_BACKOFF_MS,
  DEFAULT_MCP_HEALTH_CHECK_TIMEOUT_MS,
  DEFAULT_MCP_MULTI_CLIENT_STRATEGY,
} from "./constants.ts";

// ── Config file loading ──────────────────────────────────────────────

/**
 * Config file search chain (highest precedence first):
 *
 *   1A  ./lootbox.config.json              Project – legacy flat file in CWD
 *   1B  ./.lootbox/config.json             Project – inside project lootbox dir
 *   2A  ~/.lootbox/config.json             User preferred – best global spot
 *   2B  $XDG_CONFIG_HOME/lootbox/config.json  User preferred – XDG-correct
 *   2C  ~/.config/lootbox/config.json      User preferred – XDG fallback
 *   3A  $XDG_DATA_HOME/lootbox/config.json User fallback – data dir (not ideal)
 *   3B  ~/.local/share/lootbox/config.json User fallback – XDG data fallback
 *   3C  ~/Library/Application Support/lootbox/config.json  macOS user fallback
 *   4A  /usr/local/etc/lootbox/config.json System preferred – always writable
 *   4B  /etc/lootbox/config.json           System fallback – may be read-only
 *
 * When --config is given explicitly, ONLY that path is used (error if missing).
 * Otherwise we walk the chain and use the first file that exists.
 * If nothing is found, return {} (all defaults).
 */
export async function discoverConfigFile(): Promise<string | null> {
  const home = (() => {
    try { return getHomeDir(); } catch { return null; }
  })();

  const candidates: string[] = [
    // 1A – project: legacy flat file
    DEFAULT_CONFIG_FILENAME,
    // 1B – project: inside .lootbox dir
    join(".lootbox", "config.json"),
  ];

  if (home) {
    // 2A – user preferred: ~/.lootbox/
    candidates.push(join(home, ".lootbox", "config.json"));

    // 2B – user preferred: $XDG_CONFIG_HOME/lootbox/
    const xdgConfigHome = Deno.env.get("XDG_CONFIG_HOME");
    if (xdgConfigHome) {
      candidates.push(join(xdgConfigHome, "lootbox", "config.json"));
    }

    // 2C – user preferred: ~/.config/lootbox/ (XDG fallback)
    candidates.push(join(home, ".config", "lootbox", "config.json"));

    // 3A – user fallback: $XDG_DATA_HOME/lootbox/
    const xdgDataHome = Deno.env.get("XDG_DATA_HOME");
    if (xdgDataHome) {
      candidates.push(join(xdgDataHome, "lootbox", "config.json"));
    }

    if (Deno.build.os === "darwin") {
      // 3C – macOS user fallback: ~/Library/Application Support/lootbox/
      candidates.push(
        join(home, "Library", "Application Support", "lootbox", "config.json"),
      );
    } else {
      // 3B – user fallback: ~/.local/share/lootbox/ (Linux/Unix)
      candidates.push(
        join(home, ".local", "share", "lootbox", "config.json"),
      );
    }
  }

  // 4A – system preferred: /usr/local/etc/lootbox/
  candidates.push(join("/usr", "local", "etc", "lootbox", "config.json"));

  // 4B – system fallback: /etc/lootbox/
  candidates.push(join("/etc", "lootbox", "config.json"));

  for (const candidate of candidates) {
    if (await exists(candidate)) {
      return candidate;
    }
  }

  return null;
}

async function loadConfigFile(path?: string): Promise<Config> {
  // Explicit --config: use only that path, error if it fails.
  if (path) {
    try {
      const text = await Deno.readTextFile(path);
      return JSON.parse(text) as Config;
    } catch {
      console.error(`Error: could not read config file: ${path}`);
      Deno.exit(1);
    }
  }

  // Auto-discover: walk the search chain.
  const discovered = await discoverConfigFile();
  if (discovered) {
    try {
      const text = await Deno.readTextFile(discovered);
      return JSON.parse(text) as Config;
    } catch {
      // File exists but is unreadable/invalid — warn but don't crash.
      console.error(`Warning: found config at ${discovered} but could not parse it`);
      return {};
    }
  }

  return {};
}

// ── Permission parsing ───────────────────────────────────────────────

/**
 * Normalise a single permission token into a proper Deno CLI flag.
 *
 *   "net"              -> "--allow-net"
 *   "--allow-net"      -> "--allow-net"
 *   "--deny-write=/x"  -> "--deny-write=/x"
 *   "read=/tmp"        -> "--allow-read=/tmp"
 *   "allow-read"       -> "--allow-read"
 *   "deny-env"         -> "--deny-env"
 */
function normalisePermissionToken(token: string): string {
  const t = token.trim();
  if (!t) return "";
  // Already a full flag
  if (t.startsWith("--")) return t;
  // Starts with allow- or deny- but missing --
  if (t.startsWith("allow-") || t.startsWith("deny-")) return `--${t}`;
  // Bare name, possibly with =value  e.g. "read=/tmp"
  return `--allow-${t}`;
}

/**
 * Parse a PermissionsConfig value into an ordered list of Deno CLI flags.
 * Returns an empty array for "no permissions".
 */
function parsePermissions(
  perm: PermissionsConfig | undefined,
): string[] {
  if (perm === undefined || perm === true) {
    return [...DEFAULT_PERMISSION_FLAGS];
  }
  if (perm === false || perm === null) {
    return [];
  }
  if (perm === "all") {
    return ["--allow-all"];
  }
  if (typeof perm === "string") {
    return perm
      .split(",")
      .map(normalisePermissionToken)
      .filter(Boolean);
  }
  if (Array.isArray(perm)) {
    return perm.map(normalisePermissionToken).filter(Boolean);
  }
  // Object form
  const flags: string[] = [];
  if (perm.defaults) {
    flags.push(...DEFAULT_PERMISSION_FLAGS);
  }
  if (perm.allow) {
    for (const a of perm.allow) {
      const n = normalisePermissionToken(a);
      if (n) flags.push(n);
    }
  }
  if (perm.deny) {
    for (const d of perm.deny) {
      const t = d.trim();
      if (!t) continue;
      if (t.startsWith("--")) {
        flags.push(t);
      } else if (t.startsWith("deny-")) {
        flags.push(`--${t}`);
      } else {
        flags.push(`--deny-${t}`);
      }
    }
  }
  return flags;
}

/**
 * Parse CLI permission flags (--allow-*, --deny-*, --no-sandbox) from
 * raw Deno.args and append them to the base permission list.
 *
 * --no-sandbox replaces everything with ["--allow-all"].
 */
function applyCLIPermissions(
  base: string[],
  rawArgs: string[],
): string[] {
  // Check for --no-sandbox first – it wins over everything
  if (rawArgs.includes("--no-sandbox")) {
    return ["--allow-all"];
  }

  const extra: string[] = [];
  for (const arg of rawArgs) {
    if (
      arg.startsWith("--allow-") ||
      arg.startsWith("--deny-")
    ) {
      extra.push(arg);
    }
  }
  if (extra.length === 0) return base;
  return [...base, ...extra];
}

// ── Helpers ──────────────────────────────────────────────────────────

function resolveNumber(
  cliStr: string | undefined,
  ...configValues: (number | undefined)[]
): number | undefined {
  if (cliStr !== undefined) {
    const n = parseInt(cliStr, 10);
    if (!isNaN(n)) return n;
  }
  for (const v of configValues) {
    if (v !== undefined) return v;
  }
  return undefined;
}

// ── Singleton cache ──────────────────────────────────────────────────

let _cachedConfig: ResolvedConfig | null = null;

/**
 * Return the cached ResolvedConfig, or resolve it on first call.
 *
 * The config is determined by CLI args + config file and does not change
 * at runtime, so caching after the first resolution is safe.  Call
 * `resetConfigCache()` in tests to force a fresh resolution.
 */
export const get_config = async (): Promise<ResolvedConfig> => {
  if (_cachedConfig) return _cachedConfig;
  const resolved = await _resolve_config();
  _cachedConfig = resolved;
  return resolved;
};

/** Clear the cached config (for tests). */
export function resetConfigCache(): void {
  _cachedConfig = null;
}

// ── Config resolution (internal) ─────────────────────────────────────

const _resolve_config = async (): Promise<ResolvedConfig> => {
  // --- Parse CLI args -------------------------------------------------
  const args = parseArgs(Deno.args, {
    string: [
      "lootbox-root",
      "port",
      "lootbox-data-dir",
      "timeout",
      "rpc-timeout",
      "client-timeout",
      "client-timeout-buffer",
      "config",
      "server-url",
    ],
    boolean: ["no-sandbox"],
    alias: {
      "lootbox-root": "r",
      port: "p",
      "lootbox-data-dir": "d",
    },
  });

  // --- Load config file -----------------------------------------------
  const config = await loadConfigFile(args.config as string | undefined);

  // --- Merge structured + legacy + hazmat ---------------------------------
  //   Priority (highest first):
  //   CLI flag > hazmat.server/client/global > server/client/global > legacy flat keys > defaults
  //
  //   Type boundaries enforce which keys belong where at compile time;
  //   unknown JSON keys are simply ignored at runtime (no manual key lists).
  const srv = config.server ?? {};
  const cli_ = config.client ?? {};
  const glb = config.global ?? {};
  const haz = config.hazmat ?? {};
  const hazSrv = (haz.server ?? {}) as Partial<ServerConfig & HazmatServerExtras>;
  const hazCli = (haz.client ?? {}) as Partial<ClientConfig & HazmatClientExtras>;
  const hazGlb = (haz.global ?? {}) as Partial<GlobalConfig & HazmatGlobalExtras>;

  // --- Port -----------------------------------------------------------
  const port = (() => {
    const n = resolveNumber(
      args.port as string | undefined,
      hazGlb.port ?? hazSrv.port,
      glb.port ?? srv.port,
      config.port,
    );
    if (n !== undefined) return n;
    return DEFAULT_PORT;
  })();

  if (isNaN(port)) {
    console.error("Error: --port must be a valid number");
    Deno.exit(1);
  }

  // --- Lootbox root / dirs -------------------------------------------
  let lootboxRoot: string;
  let toolsDir: string;
  let workflowsDir: string;
  let scriptsDir: string;

  const explicitRoot =
    (args["lootbox-root"] as string) ||
    hazSrv.lootboxRoot ||
    srv.lootboxRoot ||
    config.lootboxRoot;

  if (explicitRoot) {
    lootboxRoot = explicitRoot;
    toolsDir = `${lootboxRoot}/tools`;
    workflowsDir = `${lootboxRoot}/workflows`;
    scriptsDir = `${lootboxRoot}/scripts`;
  } else {
    const localLootboxDir = ".lootbox";
    if (await exists(localLootboxDir)) {
      lootboxRoot = localLootboxDir;
      toolsDir = `${lootboxRoot}/tools`;
      workflowsDir = `${lootboxRoot}/workflows`;
      scriptsDir = `${lootboxRoot}/scripts`;

      // Auto-create missing subdirectories so the server doesn't crash
      for (const dir of [toolsDir, workflowsDir, scriptsDir]) {
        if (!(await exists(dir))) {
          try {
            await Deno.mkdir(dir, { recursive: true });
            console.error(`Auto-created missing directory: ${dir}`);
          } catch {
            // Best effort — will fail later if actually needed
          }
        }
      }
    } else {
      const homeToolsDir = getUserLootboxToolsDir();
      if (await exists(homeToolsDir)) {
        lootboxRoot = dirname(homeToolsDir);
        toolsDir = homeToolsDir;
        workflowsDir = getUserLootboxWorkflowsDir();
        scriptsDir = getUserLootboxScriptsDir();
      } else {
        console.error("\n\u274C No lootbox directory found!");
        console.error("\nLooked in:");
        console.error(`  \u2022 ${localLootboxDir}`);
        console.error(`  \u2022 ${homeToolsDir}`);
        console.error(
          "\n\uD83D\uDCA1 Run 'lootbox init' to create a new lootbox project.\n",
        );
        Deno.exit(1);
      }
    }
  }

  // --- Data dir -------------------------------------------------------
  const lootboxDataDir =
    (args["lootbox-data-dir"] as string) ||
    hazSrv.lootboxDataDir ||
    srv.lootboxDataDir ||
    config.lootboxDataDir ||
    null;

  // --- MCP servers ----------------------------------------------------
  const mcpServers: Record<string, McpServerConfig> | null =
    hazSrv.mcpServers ?? srv.mcpServers ?? config.mcpServers ?? null;

  // --- Timeout --------------------------------------------------------
  const timeout = (() => {
    const n = resolveNumber(
      args.timeout as string | undefined,
      hazSrv.timeout,
      srv.timeout,
      config.timeout,
    );
    if (n !== undefined) {
      if (n <= 0) {
        console.error(
          "Error: --timeout must be a positive number (milliseconds)",
        );
        Deno.exit(1);
      }
      return n;
    }
    return DEFAULT_TIMEOUT_MS;
  })();

  // --- RPC timeout ----------------------------------------------------
  const rpcTimeout = (() => {
    const n = resolveNumber(
      args["rpc-timeout"] as string | undefined,
      hazSrv.rpcTimeout,
      srv.rpcTimeout,
    );
    return n ?? DEFAULT_RPC_TIMEOUT_MS;
  })();

  // --- Worker ready timeout -------------------------------------------
  const workerReadyTimeout = (() => {
    const n = resolveNumber(
      undefined, // no CLI flag for this – hazmat only
      hazSrv.workerReadyTimeout,
    );
    return n ?? DEFAULT_WORKER_READY_TIMEOUT_MS;
  })();

  // --- Permissions ----------------------------------------------------
  // Resolve from config (structured > legacy sandbox)
  let permConfig: PermissionsConfig | undefined =
    hazSrv.permissions ?? srv.permissions;

  if (permConfig === undefined && config.sandbox !== undefined) {
    // Legacy: sandbox:true => default permissions, sandbox:false => "all"
    permConfig = config.sandbox ? true : "all";
  }

  let permissionFlags = parsePermissions(permConfig);

  // Append dynamic --allow-import for the server port
  // (always needed so user scripts can import the generated client)
  const allowImport = `--allow-import=localhost:${port}`;
  if (
    !permissionFlags.includes("--allow-all") &&
    !permissionFlags.some((f) => f.startsWith("--allow-import"))
  ) {
    permissionFlags.push(allowImport);
  }

  // Apply CLI overrides (--no-sandbox, --allow-*, --deny-*)
  permissionFlags = applyCLIPermissions(permissionFlags, Deno.args);

  // Re-add --allow-import if --no-sandbox / --allow-all replaced everything
  // (--allow-all already covers imports, but keep it explicit for clarity)
  if (
    !permissionFlags.includes("--allow-all") &&
    !permissionFlags.some((f) => f.startsWith("--allow-import"))
  ) {
    permissionFlags.push(allowImport);
  }

  // --- Client timeout -------------------------------------------------
  const clientTimeoutBuffer = (() => {
    const n = resolveNumber(
      args["client-timeout-buffer"] as string | undefined,
      hazCli.clientTimeoutBuffer,
      cli_.clientTimeoutBuffer,
    );
    return n ?? DEFAULT_CLIENT_TIMEOUT_BUFFER_MS;
  })();

  const clientTimeout = (() => {
    const explicit = resolveNumber(
      args["client-timeout"] as string | undefined,
      hazCli.clientTimeout,
      cli_.clientTimeout,
    );
    if (explicit !== undefined) return explicit;
    return Math.max(timeout + clientTimeoutBuffer, CLIENT_TIMEOUT_FLOOR_MS);
  })();

  // --- Server URL (client-side) ---------------------------------------
  const serverUrl = (() => {
    if (args["server-url"]) return args["server-url"] as string;
    if (hazCli.serverUrl) return hazCli.serverUrl;
    if (cli_.serverUrl) return cli_.serverUrl;
    if (config.serverUrl) return config.serverUrl;
    return `ws://localhost:${port}${hazGlb.wsPath ?? DEFAULT_WS_PATH}`;
  })();

  // --- Hazmat: server internals (no CLI flags) ------------------------
  const workerShutdownGrace =
    hazSrv.workerShutdownGrace ?? DEFAULT_WORKER_SHUTDOWN_GRACE_MS;
  const fileWatchDebounce =
    hazSrv.fileWatchDebounce ?? DEFAULT_FILE_WATCH_DEBOUNCE_MS;
  const maxWorkerBackoff =
    hazSrv.maxWorkerBackoff ?? DEFAULT_MAX_WORKER_BACKOFF_MS;
  const maxWorkerRestarts =
    hazSrv.maxWorkerRestarts ?? DEFAULT_MAX_WORKER_RESTARTS;
  const workerBackoffBase =
    hazSrv.workerBackoffBase ?? DEFAULT_WORKER_BACKOFF_BASE_MS;
  const serverStartDelay =
    hazSrv.serverStartDelay ?? DEFAULT_SERVER_START_DELAY_MS;
  const workerPollInterval =
    hazSrv.workerPollInterval ?? DEFAULT_WORKER_POLL_INTERVAL_MS;
  const dbFilename =
    hazSrv.dbFilename ?? DEFAULT_DB_FILENAME;
  const workerWsPath =
    hazSrv.workerWsPath ?? DEFAULT_WORKER_WS_PATH;
  const healthPath =
    hazSrv.healthPath ?? DEFAULT_HEALTH_PATH;
  const toolFileExtension =
    hazSrv.toolFileExtension ?? DEFAULT_TOOL_FILE_EXTENSION;
  const openApiTitle =
    hazSrv.openApiTitle ?? DEFAULT_OPENAPI_TITLE;
  const mcpClientName =
    hazSrv.mcpClientName ?? DEFAULT_MCP_CLIENT_NAME;

  // --- Hazmat: MCP health monitoring (global defaults) ----------------
  const mcpHealthCheckInterval =
    hazSrv.mcpHealthCheckInterval ?? DEFAULT_MCP_HEALTH_CHECK_INTERVAL_MS;
  const mcpMaxReconnectAttempts =
    hazSrv.mcpMaxReconnectAttempts ?? DEFAULT_MCP_MAX_RECONNECT_ATTEMPTS;
  const mcpReconnectBackoffBase =
    hazSrv.mcpReconnectBackoffBase ?? DEFAULT_MCP_RECONNECT_BACKOFF_BASE_MS;
  const mcpMaxReconnectBackoff =
    hazSrv.mcpMaxReconnectBackoff ?? DEFAULT_MCP_MAX_RECONNECT_BACKOFF_MS;
  const mcpHealthCheckTimeout =
    hazSrv.mcpHealthCheckTimeout ?? DEFAULT_MCP_HEALTH_CHECK_TIMEOUT_MS;

  // --- Hazmat: MCP multi-client (global default) ----------------------
  const mcpDefaultMultiClientStrategy: McpMultiClientStrategy =
    hazSrv.mcpDefaultMultiClientStrategy ?? DEFAULT_MCP_MULTI_CLIENT_STRATEGY;

  // --- Hazmat: client internals ---------------------------------------
  const autoDisconnectDelay = (() => {
    const n = resolveNumber(
      undefined, // no CLI flag
      hazCli.autoDisconnectDelay,
      cli_.autoDisconnectDelay,
    );
    return n ?? DEFAULT_AUTO_DISCONNECT_DELAY_MS;
  })();
  const workflowStateFile =
    hazCli.workflowStateFile ?? DEFAULT_WORKFLOW_STATE_FILE;
  const reconnectDelay =
    hazCli.reconnectDelay ?? DEFAULT_RECONNECT_DELAY_MS;

  // --- Hazmat: global internals ---------------------------------------
  const wsPath =
    hazGlb.wsPath ?? DEFAULT_WS_PATH;

  // --- Return ---------------------------------------------------------
  return {
    tools_dir: toolsDir,
    workflows_dir: workflowsDir,
    scripts_dir: scriptsDir,
    port,
    lootbox_data_dir: lootboxDataDir,
    mcp_servers: mcpServers,
    timeout,
    rpc_timeout: rpcTimeout,
    worker_ready_timeout: workerReadyTimeout,
    permission_flags: permissionFlags,

    // Server – hazmat (resolved flat)
    worker_shutdown_grace: workerShutdownGrace,
    file_watch_debounce: fileWatchDebounce,
    max_worker_backoff: maxWorkerBackoff,
    max_worker_restarts: maxWorkerRestarts,
    worker_backoff_base: workerBackoffBase,
    server_start_delay: serverStartDelay,
    worker_poll_interval: workerPollInterval,
    db_filename: dbFilename,
    worker_ws_path: workerWsPath,
    health_path: healthPath,
    tool_file_extension: toolFileExtension,
    openapi_title: openApiTitle,
    mcp_client_name: mcpClientName,

    // MCP health monitoring (global defaults, resolved flat)
    mcp_health_check_interval: mcpHealthCheckInterval,
    mcp_max_reconnect_attempts: mcpMaxReconnectAttempts,
    mcp_reconnect_backoff_base: mcpReconnectBackoffBase,
    mcp_max_reconnect_backoff: mcpMaxReconnectBackoff,
    mcp_health_check_timeout: mcpHealthCheckTimeout,

    // MCP multi-client (global default, resolved flat)
    mcp_default_multi_client_strategy: mcpDefaultMultiClientStrategy,

    // Client
    server_url: serverUrl,
    client_timeout: clientTimeout,
    auto_disconnect_delay: autoDisconnectDelay,

    // Client – hazmat (resolved flat)
    workflow_state_file: workflowStateFile,
    reconnect_delay: reconnectDelay,

    // Global – hazmat (resolved flat)
    ws_path: wsPath,
  };
};
