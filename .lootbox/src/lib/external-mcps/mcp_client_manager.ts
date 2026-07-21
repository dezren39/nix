// MCP client lifecycle management

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { SSEClientTransport } from "@modelcontextprotocol/sdk/client/sse.js";
import type { McpServerConfig } from "./mcp_config.ts";
import { VERSION } from "../../version.ts";

// ── Connection state ─────────────────────────────────────────────────

/** Granular connection state for each MCP server. */
export type McpConnectionState =
  | "connected"
  | "disconnected"
  | "reconnecting"
  | "failed";

/** Per-server health snapshot exposed to health endpoint / CLI. */
export interface McpServerHealth {
  status: McpConnectionState;
  lastHealthCheck: string | null; // ISO timestamp
  reconnectAttempts: number;
}

/** Internal tracking for a single MCP server connection. */
interface McpServerEntry {
  client: Client | null;
  config: McpServerConfig;
  state: McpConnectionState;
  lastHealthCheck: Date | null;
  reconnectAttempts: number;
}

// ── Event types ──────────────────────────────────────────────────────

export type McpClientEvent =
  | { type: "connected"; serverName: string }
  | { type: "disconnected"; serverName: string }
  | { type: "reconnecting"; serverName: string; attempt: number }
  | { type: "reconnected"; serverName: string }
  | { type: "failed"; serverName: string; attempts: number };

export type McpClientEventListener = (event: McpClientEvent) => void;

// ── McpClientManager ─────────────────────────────────────────────────

export class McpClientManager {
  private servers = new Map<string, McpServerEntry>();
  private clientName: string;
  private eventListeners: McpClientEventListener[] = [];

  constructor(clientName: string) {
    this.clientName = clientName;
  }

  // ── Event system ───────────────────────────────────────────────────

  /** Register a listener for connection lifecycle events. */
  onEvent(listener: McpClientEventListener): void {
    this.eventListeners.push(listener);
  }

  private emit(event: McpClientEvent): void {
    for (const listener of this.eventListeners) {
      try {
        listener(event);
      } catch {
        // Best effort — don't let listener errors break lifecycle
      }
    }
  }

  // ── Initialisation ─────────────────────────────────────────────────

  /**
   * Initialize all MCP clients from configuration.
   * Stores each server's config so reconnection is possible later.
   */
  async initializeClients(
    configs: Record<string, McpServerConfig>,
  ): Promise<void> {
    const promises = Object.entries(configs).map(([serverName, config]) =>
      this.connectClient(serverName, config)
    );

    await Promise.allSettled(promises);

    const connected = this.getConnectedServerNames();
    const failed = Array.from(this.servers.entries())
      .filter(([_, entry]) => entry.state === "failed")
      .map(([name]) => name);

    console.error(
      `MCP Clients initialized: ${connected.length} connected, ${failed.length} failed`,
    );
    if (connected.length > 0) {
      console.error(`Connected servers: ${connected.join(", ")}`);
    }
    if (failed.length > 0) {
      console.error(`Failed servers: ${failed.join(", ")}`);
    }
  }

  // ── Connection ─────────────────────────────────────────────────────

  /** Create a transport from server config. */
  private createTransport(config: McpServerConfig) {
    if (config.transport === "stdio" || !config.transport) {
      return new StdioClientTransport({
        command: config.command,
        args: config.args,
        env: { ...Deno.env.toObject(), ...config.env },
      });
    } else if (config.transport === "streamable_http") {
      return new StreamableHTTPClientTransport(new URL(config.url));
    } else if (config.transport === "sse") {
      return new SSEClientTransport(new URL(config.url));
    }
    throw new Error(`Invalid transport: ${(config as unknown as Record<string, unknown>).transport}`);
  }

  /**
   * Connect to a single MCP server.
   * Stores the config for future reconnection.
   */
  async connectClient(
    serverName: string,
    config: McpServerConfig,
  ): Promise<void> {
    // Initialise entry (or update config on an existing one)
    const existing = this.servers.get(serverName);
    const entry: McpServerEntry = existing ?? {
      client: null,
      config,
      state: "disconnected",
      lastHealthCheck: null,
      reconnectAttempts: 0,
    };
    if (!existing) {
      entry.config = config;
      this.servers.set(serverName, entry);
    }

    try {
      console.error(`Connecting to MCP server: ${serverName}...`);

      const transport = this.createTransport(config);

      const client = new Client(
        { name: this.clientName, version: VERSION },
        { capabilities: {} },
      );

      await client.connect(transport);

      entry.client = client;
      entry.state = "connected";
      entry.reconnectAttempts = 0;

      this.emit({ type: "connected", serverName });
      console.error(`Successfully connected to MCP server: ${serverName}`);
    } catch (error) {
      entry.state = "failed";
      entry.client = null;

      // L5 fix: Emit "failed" event so listeners can react to initial connection failures.
      this.emit({ type: "failed", serverName, attempts: 0 });

      console.error(
        `Failed to connect to MCP server '${serverName}':`,
        error instanceof Error ? error.message : String(error),
      );
    }
  }

  /**
   * Reconnect to a server that was previously configured.
   * Used by the health monitor after detecting a dropped connection.
   */
  async reconnectClient(serverName: string): Promise<boolean> {
    const entry = this.servers.get(serverName);
    if (!entry) {
      console.error(`[McpClientManager] Cannot reconnect unknown server: ${serverName}`);
      return false;
    }

    entry.state = "reconnecting";
    entry.reconnectAttempts++;
    this.emit({ type: "reconnecting", serverName, attempt: entry.reconnectAttempts });

    // Close old client if still lingering
    if (entry.client) {
      try {
        await entry.client.close();
      } catch {
        // Best effort
      }
      entry.client = null;
    }

    try {
      console.error(
        `[McpClientManager] Reconnecting to ${serverName} (attempt ${entry.reconnectAttempts})...`,
      );

      const transport = this.createTransport(entry.config);
      const client = new Client(
        { name: this.clientName, version: VERSION },
        { capabilities: {} },
      );

      await client.connect(transport);

      entry.client = client;
      entry.state = "connected";
      entry.reconnectAttempts = 0;

      this.emit({ type: "reconnected", serverName });
      console.error(`[McpClientManager] Reconnected to ${serverName}`);
      return true;
    } catch (error) {
      entry.state = "disconnected";
      console.error(
        `[McpClientManager] Reconnect to ${serverName} failed:`,
        error instanceof Error ? error.message : String(error),
      );
      return false;
    }
  }

  /**
   * Mark a server as permanently failed (circuit breaker tripped).
   */
  markFailed(serverName: string): void {
    const entry = this.servers.get(serverName);
    if (entry) {
      entry.state = "failed";
      this.emit({ type: "failed", serverName, attempts: entry.reconnectAttempts });
    }
  }

  /**
   * Mark a server as disconnected (health check detected failure).
   */
  markDisconnected(serverName: string): void {
    const entry = this.servers.get(serverName);
    if (entry && entry.state === "connected") {
      entry.state = "disconnected";
      this.emit({ type: "disconnected", serverName });
    }
  }

  /**
   * Update the last health-check timestamp for a server.
   */
  updateHealthCheck(serverName: string): void {
    const entry = this.servers.get(serverName);
    if (entry) {
      entry.lastHealthCheck = new Date();
    }
  }

  // ── Queries ────────────────────────────────────────────────────────

  /**
   * Get a connected client by server name.
   * Returns undefined if server failed to connect or doesn't exist.
   */
  getClient(serverName: string): Client | undefined {
    const entry = this.servers.get(serverName);
    return entry?.state === "connected" ? entry.client ?? undefined : undefined;
  }

  /** Get the stored config for a server (for health monitor). */
  getServerConfig(serverName: string): McpServerConfig | undefined {
    return this.servers.get(serverName)?.config;
  }

  /** Get list of successfully connected server names. */
  getConnectedServerNames(): string[] {
    return Array.from(this.servers.entries())
      .filter(([_, entry]) => entry.state === "connected" && entry.client)
      .map(([name]) => name);
  }

  /** Get all server names (connected + failed + disconnected). */
  getAllServerNames(): string[] {
    return Array.from(this.servers.keys());
  }

  /** Get connection state for a specific server. */
  getConnectionState(serverName: string): McpConnectionState | "unknown" {
    return this.servers.get(serverName)?.state ?? "unknown";
  }

  /** Get the reconnect attempt count for a server. */
  getReconnectAttempts(serverName: string): number {
    return this.servers.get(serverName)?.reconnectAttempts ?? 0;
  }

  /**
   * Get connection status for a specific server.
   * @deprecated Use getConnectionState() instead.
   */
  getConnectionStatus(serverName: string): "connected" | "failed" | "unknown" {
    const state = this.servers.get(serverName)?.state;
    if (state === "connected") return "connected";
    if (state === "failed") return "failed";
    return "unknown";
  }

  /**
   * Get all connection statuses.
   * @deprecated Use getServerHealth() or getConnectionState() instead.
   */
  getAllConnectionStatuses(): Record<
    string,
    "connected" | "failed" | "unknown"
  > {
    const statuses: Record<string, "connected" | "failed" | "unknown"> = {};
    for (const [name, entry] of this.servers.entries()) {
      if (entry.state === "connected") statuses[name] = "connected";
      else if (entry.state === "failed") statuses[name] = "failed";
      else statuses[name] = "unknown";
    }
    return statuses;
  }

  /**
   * Get per-server health snapshots for the deep health endpoint.
   */
  getServerHealth(): Record<string, McpServerHealth> {
    const result: Record<string, McpServerHealth> = {};
    for (const [name, entry] of this.servers.entries()) {
      result[name] = {
        status: entry.state,
        lastHealthCheck: entry.lastHealthCheck?.toISOString() ?? null,
        reconnectAttempts: entry.reconnectAttempts,
      };
    }
    return result;
  }

  // ── Teardown ───────────────────────────────────────────────────────

  /** Disconnect all MCP clients. */
  async disconnectAll(): Promise<void> {
    console.error(`Disconnecting ${this.servers.size} MCP clients...`);

    const promises = Array.from(this.servers.entries()).map(
      async ([serverName, entry]) => {
        try {
          if (entry.client) {
            await entry.client.close();
          }
          console.error(`Disconnected from MCP server: ${serverName}`);
        } catch (error) {
          console.error(
            `Error disconnecting from MCP server '${serverName}':`,
            error instanceof Error ? error.message : String(error),
          );
        }
      },
    );

    await Promise.allSettled(promises);

    this.servers.clear();
    this.eventListeners = [];
  }
}
