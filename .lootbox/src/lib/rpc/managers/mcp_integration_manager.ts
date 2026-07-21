/**
 * McpIntegrationManager
 *
 * Manages MCP (Model Context Protocol) integration.
 * Handles:
 * - MCP client lifecycle via McpClientManager
 * - Health monitoring via McpHealthMonitor
 * - Schema fetching via McpSchemaFetcher
 * - MCP tool and resource calls
 * - Providing schemas to type generation
 */

import { McpClientManager, type McpServerHealth } from "../../external-mcps/mcp_client_manager.ts";
import type { McpConfigFile, McpServerConfig } from "../../external-mcps/mcp_config.ts";
import {
  McpHealthMonitor,
  type McpHealthGlobalDefaults,
} from "../../external-mcps/mcp_health_monitor.ts";
import { McpSchemaFetcher } from "../../external-mcps/mcp_schema_fetcher.ts";
import type { McpServerSchemas } from "../../external-mcps/mcp_schema_fetcher.ts";
import { executeMcpResource, executeMcpTool } from "../execute_mcp.ts";
import {
  McpSessionRegistry,
  type McpSessionEntry,
} from "../../external-mcps/mcp_session_registry.ts";
import { McpAutoPortAssigner } from "../../external-mcps/mcp_auto_port.ts";
import type { McpMultiClientStrategy } from "../../lootbox-cli/types.ts";
import {
  DEFAULT_MCP_MULTI_CLIENT_STRATEGY,
  DEFAULT_MCP_AUTO_PORT_RANGE,
} from "../../constants.ts";

/** Overall MCP subsystem health status for the deep health endpoint. */
export interface McpHealthStatus {
  status: "ok" | "degraded" | "unhealthy";
  servers: Record<string, McpServerHealth>;
}

export class McpIntegrationManager {
  private state: {
    clientManager: McpClientManager;
    schemaFetcher: McpSchemaFetcher;
    healthMonitor: McpHealthMonitor;
    mcpConfig: McpConfigFile;
    sessionRegistry: McpSessionRegistry;
    /** Session IDs registered by this instance, for cleanup. */
    registeredSessionIds: string[];
  } | null = null;

  /**
   * Initialize MCP integration with provided configuration.
   *
   * @param mcpConfig        Parsed MCP server configuration.
   * @param mcpClientName    Identity string sent to MCP servers.
   * @param healthDefaults   Global health-check defaults from resolved config.
   * @param lootboxPort      Port this lootbox instance is serving on (for registry).
   * @param multiClientStrategy  Global default multi-client strategy.
   */
  async initialize(
    mcpConfig: McpConfigFile,
    mcpClientName?: string,
    healthDefaults?: McpHealthGlobalDefaults,
    lootboxPort?: number,
    multiClientStrategy?: McpMultiClientStrategy,
  ): Promise<void> {
    console.error("Initializing MCP integration...");

    const sessionRegistry = new McpSessionRegistry();
    const globalStrategy = multiClientStrategy ?? DEFAULT_MCP_MULTI_CLIENT_STRATEGY;

    // ── Apply multi-client strategy & auto-port ──────────────────────
    const effectiveConfigs: Record<string, McpServerConfig> = {};
    const registeredSessionIds: string[] = [];
    /** H4 fix: Map serverName → sessionId for targeted heartbeat updates. */
    const serverSessionMap = new Map<string, string>();
    const portAssigner = new McpAutoPortAssigner(sessionRegistry);

    for (const [serverName, config] of Object.entries(mcpConfig.mcpServers)) {
      const serverStrategy = config.multiClient?.strategy ?? globalStrategy;
      let effectiveConfig = { ...config };

      if (serverStrategy === "auto-port" && config.args) {
        // Find the originally configured port from the args
        const originalPort = this.extractPortFromArgs(
          config.args,
          config.multiClient?.portArgPattern,
        );

        if (originalPort !== null) {
          const portRange = config.multiClient?.portRange ?? DEFAULT_MCP_AUTO_PORT_RANGE;
          try {
            const result = await portAssigner.assignPort(
              serverName, originalPort, portRange,
            );
            if (result.wasReassigned) {
              effectiveConfig = {
                ...config,
                args: McpAutoPortAssigner.rewriteArgs(
                  config.args,
                  originalPort,
                  result.port,
                  config.multiClient?.portArgPattern,
                ),
              };
              console.error(
                `[McpIntegrationManager] Auto-port for ${serverName}: ${originalPort} → ${result.port}`,
              );
            }

            // Register session in the registry
            const sessionId = McpSessionRegistry.generateSessionId(serverName);
            await sessionRegistry.register({
              serverName,
              sessionId,
              pid: Deno.pid,
              port: result.port,
              originalPort,
              startedAt: new Date().toISOString(),
              lastHeartbeat: new Date().toISOString(),
              lootboxPort: lootboxPort ?? 3000,
              workdir: Deno.cwd(),
            });
            registeredSessionIds.push(sessionId);
            serverSessionMap.set(serverName, sessionId);
          } catch (err) {
            console.error(
              `[McpIntegrationManager] Auto-port failed for ${serverName}:`,
              err instanceof Error ? err.message : String(err),
            );
            // C1 fix: Do NOT fall through to effectiveConfigs — the original
            // port is conflicting, so skip this server entirely.
            continue;
          }
        } else {
          // No port found in args — just register with port 0
          const sessionId = McpSessionRegistry.generateSessionId(serverName);
          await sessionRegistry.register({
            serverName,
            sessionId,
            pid: Deno.pid,
            port: 0,
            originalPort: 0,
            startedAt: new Date().toISOString(),
            lastHeartbeat: new Date().toISOString(),
            lootboxPort: lootboxPort ?? 3000,
            workdir: Deno.cwd(),
          });
          registeredSessionIds.push(sessionId);
          serverSessionMap.set(serverName, sessionId);
        }
      } else if (serverStrategy === "fail") {
        // Check registry for conflicts before connecting
        const existing = await sessionRegistry.findByServerName(serverName);
        if (existing.length > 0) {
          console.error(
            `[McpIntegrationManager] CONFLICT: server '${serverName}' already has ` +
              `${existing.length} active session(s). Strategy is "fail" — skipping.`,
          );
          continue; // don't add to effectiveConfigs
        }
        // Register this session
        const sessionId = McpSessionRegistry.generateSessionId(serverName);
        await sessionRegistry.register({
          serverName,
          sessionId,
          pid: Deno.pid,
          port: 0,
          originalPort: 0,
          startedAt: new Date().toISOString(),
          lastHeartbeat: new Date().toISOString(),
          lootboxPort: lootboxPort ?? 3000,
          workdir: Deno.cwd(),
        });
        registeredSessionIds.push(sessionId);
        serverSessionMap.set(serverName, sessionId);
      } else if (serverStrategy === "warn") {
        // Warn if conflict exists but proceed
        const existing = await sessionRegistry.findByServerName(serverName);
        if (existing.length > 0) {
          console.error(
            `[McpIntegrationManager] WARNING: server '${serverName}' already has ` +
              `${existing.length} active session(s). Proceeding anyway (strategy: warn).`,
          );
        }
        // Register session
        const sessionId = McpSessionRegistry.generateSessionId(serverName);
        await sessionRegistry.register({
          serverName,
          sessionId,
          pid: Deno.pid,
          port: 0,
          originalPort: 0,
          startedAt: new Date().toISOString(),
          lastHeartbeat: new Date().toISOString(),
          lootboxPort: lootboxPort ?? 3000,
          workdir: Deno.cwd(),
        });
        registeredSessionIds.push(sessionId);
        serverSessionMap.set(serverName, sessionId);
      } else {
        // "per-session" — each session gets its own server process (default stdio behavior)
        const sessionId = McpSessionRegistry.generateSessionId(serverName);
        await sessionRegistry.register({
          serverName,
          sessionId,
          pid: Deno.pid,
          port: 0,
          originalPort: 0,
          startedAt: new Date().toISOString(),
          lastHeartbeat: new Date().toISOString(),
          lootboxPort: lootboxPort ?? 3000,
          workdir: Deno.cwd(),
        });
        registeredSessionIds.push(sessionId);
        serverSessionMap.set(serverName, sessionId);
      }

      effectiveConfigs[serverName] = effectiveConfig;
    }

    const clientManager = new McpClientManager(mcpClientName ?? "lootbox");
    await clientManager.initializeClients(effectiveConfigs);

    const schemaFetcher = new McpSchemaFetcher();
    for (const serverName of clientManager.getConnectedServerNames()) {
      const client = clientManager.getClient(serverName);
      if (client) {
        await schemaFetcher.fetchSchemas(client, serverName);
      }
    }

    // Set up health monitoring
    const defaults: McpHealthGlobalDefaults = healthDefaults ?? {
      checkInterval: 30_000,
      maxReconnectAttempts: 5,
      reconnectBackoffBase: 2_000,
      maxReconnectBackoff: 60_000,
      checkTimeout: 5_000,
    };
    const healthMonitor = new McpHealthMonitor(clientManager, defaults);

    // When a server reconnects, automatically re-fetch its schemas.
    // On healthy pings, update session registry heartbeats.
    healthMonitor.onEvent(async (event) => {
      if (event.type === "server:reconnected") {
        const client = clientManager.getClient(event.serverName);
        if (client) {
          try {
            await schemaFetcher.fetchSchemas(client, event.serverName);
            console.error(
              `[McpIntegrationManager] Re-fetched schemas for ${event.serverName} after reconnect`,
            );
          } catch (err) {
            console.error(
              `[McpIntegrationManager] Failed to re-fetch schemas for ${event.serverName}:`,
              err,
            );
          }
        }
      }

      // H4 fix: Update heartbeat only for the specific healthy server's session,
      // not all sessions. This reduces disk I/O from O(N) to O(1) per ping.
      if (event.type === "server:healthy") {
        const sessionId = serverSessionMap.get(event.serverName);
        if (sessionId) {
          try {
            await sessionRegistry.updateHeartbeat(sessionId);
          } catch {
            // Best effort
          }
        }
      }
    });

    // C3 fix: Only monitor servers that passed strategy checks (effectiveConfigs),
    // not all servers from the original config (which may include skipped ones).
    healthMonitor.start(effectiveConfigs);

    this.state = {
      clientManager,
      schemaFetcher,
      healthMonitor,
      mcpConfig,
      sessionRegistry,
      registeredSessionIds,
    };
    console.error("MCP integration initialized successfully");
  }

  /**
   * Shutdown MCP integration and disconnect all clients.
   * Deregisters all sessions owned by this process from the registry.
   */
  async shutdown(): Promise<void> {
    if (this.state) {
      this.state.healthMonitor.stop();
      await this.state.clientManager.disconnectAll();

      // Deregister our sessions from the registry
      for (const sessionId of this.state.registeredSessionIds) {
        try {
          await this.state.sessionRegistry.deregister(sessionId);
        } catch {
          // Best effort cleanup
        }
      }

      this.state = null;
      console.error("MCP integration shut down");
    }
  }

  /**
   * Handle MCP tool or resource call
   * Method format: mcp_ServerName.operationName
   * Resource operations start with "resource_"
   */
  async handleMcpCall(
    method: string,
    args: unknown,
    rpcTimeout?: number,
  ): Promise<{ success: boolean; data?: unknown; error?: string }> {
    if (!this.state) {
      return {
        success: false,
        error: "MCP is not initialized",
      };
    }

    // Parse method: mcp_ServerName.operationName
    const parts = method.split(".");
    if (parts.length !== 2) {
      return {
        success: false,
        error: `Invalid MCP method format: ${method}`,
      };
    }

    const serverNameWithPrefix = parts[0]; // mcp_ServerName
    const operationName = parts[1];

    // Remove mcp_ prefix to get actual server name
    if (!serverNameWithPrefix.startsWith("mcp_")) {
      return {
        success: false,
        error: `Invalid MCP method format: ${method}`,
      };
    }

    const serverName = serverNameWithPrefix.substring(4); // Remove "mcp_"

    // Check if it's a resource call (starts with resource_)
    if (operationName.startsWith("resource_")) {
      const resourceName = operationName.substring(9); // Remove "resource_"
      return await executeMcpResource(
        this.state.clientManager,
        this.state.schemaFetcher,
        serverName,
        resourceName,
        args,
        rpcTimeout,
      );
    } else {
      // It's a tool call
      return await executeMcpTool(
        this.state.clientManager,
        this.state.schemaFetcher,
        serverName,
        operationName,
        args,
        rpcTimeout,
      );
    }
  }

  /**
   * Get all MCP schemas for type generation
   */
  async getSchemas(): Promise<McpServerSchemas[]> {
    if (!this.state) {
      return [];
    }
    const schemas: McpServerSchemas[] = [];
    for (
      const serverName of this.state.clientManager.getConnectedServerNames()
    ) {
      const client = this.state.clientManager.getClient(serverName);
      if (client) {
        schemas.push(
          await this.state.schemaFetcher.fetchSchemas(client, serverName),
        );
      }
    }
    return schemas;
  }

  /**
   * Get list of connected MCP server names
   */
  getConnectedServers(): string[] {
    if (!this.state) {
      return [];
    }
    return this.state.clientManager.getConnectedServerNames();
  }

  /**
   * Check if MCP integration is enabled
   */
  isEnabled(): boolean {
    return this.state !== null;
  }

  /**
   * Get the aggregate MCP health status for the deep health endpoint.
   *
   * Returns per-server health snapshots and an overall status:
   *   "ok"        — all servers connected
   *   "degraded"  — some servers unhealthy/reconnecting but at least one is connected
   *   "unhealthy" — all servers are down
   */
  getHealthStatus(): McpHealthStatus {
    if (!this.state) {
      return { status: "ok", servers: {} };
    }

    const servers = this.state.clientManager.getServerHealth();
    const names = Object.keys(servers);

    if (names.length === 0) {
      return { status: "ok", servers };
    }

    const connectedCount = names.filter(
      (n) => servers[n].status === "connected",
    ).length;

    let status: "ok" | "degraded" | "unhealthy";
    if (connectedCount === names.length) {
      status = "ok";
    } else if (connectedCount > 0) {
      status = "degraded";
    } else {
      status = "unhealthy";
    }

    return { status, servers };
  }

  /**
   * Update heartbeats for all registered sessions in the registry.
   * Called periodically by the health monitor tick.
   */
  async updateRegistryHeartbeats(): Promise<void> {
    if (!this.state) return;
    for (const sessionId of this.state.registeredSessionIds) {
      try {
        await this.state.sessionRegistry.updateHeartbeat(sessionId);
      } catch {
        // Best effort — don't break the health loop
      }
    }
  }

  /**
   * Get the session registry (for testing or external queries).
   */
  getSessionRegistry(): McpSessionRegistry | null {
    return this.state?.sessionRegistry ?? null;
  }

  /**
   * Get registered session IDs for this instance (for testing).
   */
  getRegisteredSessionIds(): string[] {
    return this.state?.registeredSessionIds ?? [];
  }

  // ── Private helpers ──────────────────────────────────────────────────

  /**
   * Extract a port number from the server's args array.
   * Looks for:
   *   1. A matching --flag=PORT or --flag PORT (using portArgPattern)
   *   2. A bare number that looks like a port
   *   3. A port embedded in a URL (e.g. http://localhost:9222)
   */
  private extractPortFromArgs(
    args: string[],
    portArgPattern?: string,
  ): number | null {
    for (let i = 0; i < args.length; i++) {
      const arg = args[i];

      // Pattern-based extraction: --flag=PORT
      if (portArgPattern && arg.startsWith(`${portArgPattern}=`)) {
        const val = parseInt(arg.split("=")[1], 10);
        if (!isNaN(val) && val > 0 && val <= 65535) return val;
      }

      // Pattern-based extraction: --flag PORT
      if (portArgPattern && arg === portArgPattern && i + 1 < args.length) {
        const val = parseInt(args[i + 1], 10);
        if (!isNaN(val) && val > 0 && val <= 65535) return val;
      }

      // M3 fix: Bare port number — only use this fallback when portArgPattern
      // is set (i.e. the user told us to look for a port). Without a pattern,
      // any numeric arg like "--timeout 5000" would be mis-detected as a port.
      if (portArgPattern && /^\d+$/.test(arg)) {
        const val = parseInt(arg, 10);
        if (val > 0 && val <= 65535) return val;
      }

      // URL-embedded port (e.g. http://localhost:9222)
      const urlMatch = arg.match(/:(\d+)(?:\/|$)/);
      if (urlMatch) {
        const val = parseInt(urlMatch[1], 10);
        if (val > 0 && val <= 65535) return val;
      }
    }

    return null;
  }
}
