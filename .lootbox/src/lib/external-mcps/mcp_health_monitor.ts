/**
 * McpHealthMonitor
 *
 * Periodically probes each MCP server's connection health and triggers
 * automatic reconnection with exponential backoff when a server
 * becomes unreachable.  A circuit breaker stops retries after a
 * configurable maximum number of attempts.
 *
 * Per-server settings (from the server's `health` config block) take
 * precedence over global defaults (from resolved config).
 */

import type { McpClientManager } from "./mcp_client_manager.ts";
import type { McpServerConfig } from "./mcp_config.ts";
import type { McpHealthConfig } from "../lootbox-cli/types.ts";

// ── Resolved health config for a single server ──────────────────────

export interface ResolvedMcpHealthConfig {
  checkInterval: number;
  maxReconnectAttempts: number;
  reconnectBackoffBase: number;
  maxReconnectBackoff: number;
  checkTimeout: number;
}

/** Global defaults passed by the integration manager. */
export interface McpHealthGlobalDefaults {
  checkInterval: number;
  maxReconnectAttempts: number;
  reconnectBackoffBase: number;
  maxReconnectBackoff: number;
  checkTimeout: number;
}

// ── Per-server monitoring state ─────────────────────────────────────

interface ServerMonitorState {
  resolved: ResolvedMcpHealthConfig;
  reconnectTimer: number | null; // setTimeout id
  isReconnecting: boolean;
}

// ── Event types ─────────────────────────────────────────────────────

export type HealthMonitorEvent =
  | { type: "server:healthy"; serverName: string }
  | { type: "server:unhealthy"; serverName: string; error: string }
  | { type: "server:reconnecting"; serverName: string; attempt: number }
  | { type: "server:reconnected"; serverName: string }
  | { type: "server:failed"; serverName: string; attempts: number };

export type HealthMonitorEventListener = (event: HealthMonitorEvent) => void;

// ── McpHealthMonitor ────────────────────────────────────────────────

export class McpHealthMonitor {
  private clientManager: McpClientManager;
  private globalDefaults: McpHealthGlobalDefaults;
  private serverStates = new Map<string, ServerMonitorState>();
  private intervalId: number | null = null;
  private eventListeners: HealthMonitorEventListener[] = [];
  private running = false;

  constructor(
    clientManager: McpClientManager,
    globalDefaults: McpHealthGlobalDefaults,
  ) {
    this.clientManager = clientManager;
    this.globalDefaults = globalDefaults;
  }

  // ── Event system ───────────────────────────────────────────────────

  onEvent(listener: HealthMonitorEventListener): void {
    this.eventListeners.push(listener);
  }

  private emit(event: HealthMonitorEvent): void {
    for (const listener of this.eventListeners) {
      try {
        listener(event);
      } catch {
        // Best effort
      }
    }
  }

  // ── Config resolution ──────────────────────────────────────────────

  /**
   * Merge per-server health config with global defaults.
   * Per-server values win; anything unset falls through to global.
   */
  resolveHealthConfig(perServer?: McpHealthConfig): ResolvedMcpHealthConfig {
    return {
      checkInterval:
        perServer?.checkInterval ?? this.globalDefaults.checkInterval,
      maxReconnectAttempts:
        perServer?.maxReconnectAttempts ??
        this.globalDefaults.maxReconnectAttempts,
      reconnectBackoffBase:
        perServer?.reconnectBackoffBase ??
        this.globalDefaults.reconnectBackoffBase,
      maxReconnectBackoff:
        perServer?.maxReconnectBackoff ??
        this.globalDefaults.maxReconnectBackoff,
      checkTimeout:
        perServer?.checkTimeout ?? this.globalDefaults.checkTimeout,
    };
  }

  // ── Lifecycle ──────────────────────────────────────────────────────

  /**
   * Start monitoring all configured MCP servers.
   * Call this after `McpClientManager.initializeClients()`.
   */
  start(serverConfigs: Record<string, McpServerConfig>): void {
    if (this.running) return;
    this.running = true;

    // Build per-server monitoring state
    for (const [serverName, config] of Object.entries(serverConfigs)) {
      const resolved = this.resolveHealthConfig(config.health);
      this.serverStates.set(serverName, {
        resolved,
        reconnectTimer: null,
        isReconnecting: false,
      });
    }

    // C2 fix: If no servers to monitor, mark running but don't start a timer.
    if (this.serverStates.size === 0) {
      console.error("[McpHealthMonitor] No servers to monitor — idle");
      return;
    }

    // Use the shortest check interval across all servers as the tick rate.
    // Each tick, only servers whose interval has elapsed are actually probed.
    const minInterval = Math.min(
      ...Array.from(this.serverStates.values()).map(
        (s) => s.resolved.checkInterval,
      ),
    );

    // Track last-check time per server for per-server interval support
    const lastCheck = new Map<string, number>();
    for (const name of this.serverStates.keys()) {
      lastCheck.set(name, 0); // force immediate first check
    }

    this.intervalId = setInterval(() => {
      const now = Date.now();
      for (const [serverName, state] of this.serverStates.entries()) {
        const last = lastCheck.get(serverName) ?? 0;
        if (now - last >= state.resolved.checkInterval) {
          lastCheck.set(serverName, now);
          this.checkServer(serverName).catch((err) => {
            console.error(
              `[McpHealthMonitor] Unexpected error checking ${serverName}:`,
              err,
            );
          });
        }
      }
    }, minInterval);

    console.error(
      `[McpHealthMonitor] Started monitoring ${this.serverStates.size} server(s) ` +
        `(tick every ${minInterval}ms)`,
    );
  }

  /** Stop monitoring and cancel all pending reconnection timers. */
  stop(): void {
    this.running = false;

    if (this.intervalId !== null) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }

    for (const state of this.serverStates.values()) {
      if (state.reconnectTimer !== null) {
        clearTimeout(state.reconnectTimer);
        state.reconnectTimer = null;
      }
    }

    this.serverStates.clear();
    this.eventListeners = [];
    console.error("[McpHealthMonitor] Stopped");
  }

  /** Whether the monitor is currently running. */
  isRunning(): boolean {
    return this.running;
  }

  // ── Health check ───────────────────────────────────────────────────

  /**
   * Probe a single server's health.
   * Uses the MCP SDK `ping()` method with a timeout.
   */
  private async checkServer(serverName: string): Promise<void> {
    const state = this.serverStates.get(serverName);
    if (!state || state.isReconnecting) return;

    const client = this.clientManager.getClient(serverName);
    if (!client) {
      // Server is not connected — it's either failed or disconnected.
      // If it's in "connected" state somehow (stale), correct it.
      const connState = this.clientManager.getConnectionState(serverName);
      if (connState === "connected") {
        this.clientManager.markDisconnected(serverName);
        this.emit({ type: "server:unhealthy", serverName, error: "client missing" });
        this.scheduleReconnect(serverName);
      }
      return;
    }

    try {
      // C4 fix: Use a clearable timeout to prevent timer leaks.
      // When ping succeeds, the timeout is cleared immediately.
      let timeoutId: number | undefined;
      const pingPromise = client.ping();
      const timeoutPromise = new Promise<never>((_, reject) => {
        timeoutId = setTimeout(
          () => reject(new Error("Health check timeout")),
          state.resolved.checkTimeout,
        );
      });

      try {
        await Promise.race([pingPromise, timeoutPromise]);
      } finally {
        if (timeoutId !== undefined) clearTimeout(timeoutId);
      }

      // Success — mark healthy
      this.clientManager.updateHealthCheck(serverName);
      this.emit({ type: "server:healthy", serverName });
    } catch (error) {
      const msg = error instanceof Error ? error.message : String(error);
      console.error(
        `[McpHealthMonitor] Health check failed for ${serverName}: ${msg}`,
      );

      this.clientManager.markDisconnected(serverName);
      this.emit({ type: "server:unhealthy", serverName, error: msg });
      this.scheduleReconnect(serverName);
    }
  }

  // ── Reconnection ──────────────────────────────────────────────────

  /**
   * Schedule a reconnection attempt with exponential backoff.
   * Respects the circuit breaker (maxReconnectAttempts).
   */
  private scheduleReconnect(serverName: string): void {
    const state = this.serverStates.get(serverName);
    if (!state || state.isReconnecting) return;

    const attempts = this.clientManager.getReconnectAttempts(serverName);
    const max = state.resolved.maxReconnectAttempts;

    // Circuit breaker
    if (max > 0 && attempts >= max) {
      console.error(
        `[McpHealthMonitor] Circuit breaker tripped for ${serverName} ` +
          `after ${attempts} attempts — marking as failed`,
      );
      this.clientManager.markFailed(serverName);
      this.emit({ type: "server:failed", serverName, attempts });
      return;
    }

    // Exponential backoff: base * 2^attempts, capped at max
    const backoff = Math.min(
      state.resolved.reconnectBackoffBase * Math.pow(2, attempts),
      state.resolved.maxReconnectBackoff,
    );

    console.error(
      `[McpHealthMonitor] Scheduling reconnect for ${serverName} ` +
        `in ${backoff}ms (attempt ${attempts + 1})`,
    );

    state.isReconnecting = true;
    state.reconnectTimer = setTimeout(async () => {
      try {
        this.emit({
          type: "server:reconnecting",
          serverName,
          attempt: attempts + 1,
        });

        const success = await this.clientManager.reconnectClient(serverName);

        if (success) {
          this.emit({ type: "server:reconnected", serverName });
          state.isReconnecting = false;
        } else {
          // Failed — schedule another attempt
          state.isReconnecting = false;
          this.scheduleReconnect(serverName);
        }
      } catch (err) {
        console.error(
          `[McpHealthMonitor] Reconnect error for ${serverName}:`,
          err,
        );
        state.isReconnecting = false;
        this.scheduleReconnect(serverName);
      }
    }, backoff);
  }
}
