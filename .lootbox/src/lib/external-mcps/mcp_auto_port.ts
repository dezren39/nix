/**
 * McpAutoPortAssigner
 *
 * Finds an available port for an MCP server by:
 *   1. Checking the session registry for ports already claimed
 *   2. Trying each port in the configured range
 *   3. Verifying the port is not bound by another process (TCP probe)
 *
 * Used when multiClient.strategy is "auto-port".
 */

import {
  DEFAULT_MCP_AUTO_PORT_RANGE,
} from "../constants.ts";
import type { McpSessionRegistry } from "./mcp_session_registry.ts";

// ── Types ────────────────────────────────────────────────────────────

export interface AutoPortResult {
  /** The port that was assigned. */
  port: number;
  /** Whether the port differs from the originally configured one. */
  wasReassigned: boolean;
  /** Human-readable reason for the assignment. */
  reason: string;
}

// ── Helpers ──────────────────────────────────────────────────────────

/**
 * Check if a TCP port is available by attempting to listen on it briefly.
 * Returns true if the port is free, false if it's already bound.
 */
async function isTcpPortAvailable(port: number): Promise<boolean> {
  try {
    const listener = Deno.listen({ port, transport: "tcp" });
    listener.close();
    return true;
  } catch (error) {
    if (error instanceof Deno.errors.AddrInUse) {
      return false;
    }
    // Permission errors etc. — treat as unavailable
    return false;
  }
}

// ── McpAutoPortAssigner ─────────────────────────────────────────────

export class McpAutoPortAssigner {
  private registry: McpSessionRegistry;
  private portRange: readonly [number, number];

  constructor(
    registry: McpSessionRegistry,
    portRange?: readonly [number, number],
  ) {
    this.registry = registry;
    this.portRange = portRange ?? DEFAULT_MCP_AUTO_PORT_RANGE;
  }

  /**
   * Try to assign the preferred port first.
   * If it's taken (by registry or OS), scan the range for a free one.
   *
   * @param serverName  The MCP server name (for logging/registry lookup)
   * @param preferredPort  The originally configured port
   * @param customRange  Optional per-server port range override
   */
  async assignPort(
    serverName: string,
    preferredPort: number,
    customRange?: readonly [number, number],
  ): Promise<AutoPortResult> {
    const range = customRange ?? this.portRange;
    const [rangeStart, rangeEnd] = range;

    // Collect ports already claimed in the registry for this server
    const registeredSessions = await this.registry.findByServerName(serverName);
    const registeredPorts = new Set(registeredSessions.map((s) => s.port));

    // Try preferred port first
    if (!registeredPorts.has(preferredPort)) {
      if (await isTcpPortAvailable(preferredPort)) {
        return {
          port: preferredPort,
          wasReassigned: false,
          reason: `preferred port ${preferredPort} is available`,
        };
      }
    }

    console.error(
      `[McpAutoPortAssigner] Port ${preferredPort} unavailable for ${serverName}, ` +
        `scanning range ${rangeStart}-${rangeEnd}...`,
    );

    // Scan range for a free port
    for (let port = rangeStart; port <= rangeEnd; port++) {
      if (port === preferredPort) continue; // already tried
      if (registeredPorts.has(port)) continue; // claimed in registry

      if (await isTcpPortAvailable(port)) {
        console.error(
          `[McpAutoPortAssigner] Assigned port ${port} for ${serverName} ` +
            `(original: ${preferredPort})`,
        );
        return {
          port,
          wasReassigned: true,
          reason: `auto-assigned port ${port} (original ${preferredPort} was unavailable)`,
        };
      }
    }

    // Exhausted range — return error-like result
    throw new Error(
      `[McpAutoPortAssigner] No available port in range ${rangeStart}-${rangeEnd} ` +
        `for server '${serverName}'. All ports are in use.`,
    );
  }

  /**
   * Rewrite a server's args array to use the assigned port.
   *
   * Supports two patterns:
   *   1. `--argName=PORT`  or  `--argName PORT` (next arg)
   *   2. Bare port number as a standalone arg
   *
   * @param args            Original args array
   * @param originalPort    The port value to find and replace
   * @param newPort         The new port to substitute
   * @param portArgPattern  Optional arg flag pattern (e.g. "--remote-debugging-port")
   * @returns New args array with the port replaced
   */
  static rewriteArgs(
    args: string[],
    originalPort: number,
    newPort: number,
    portArgPattern?: string,
  ): string[] {
    if (originalPort === newPort) return [...args];

    const portStr = String(originalPort);
    const newPortStr = String(newPort);
    const result: string[] = [];

    for (let i = 0; i < args.length; i++) {
      const arg = args[i];

      // Pattern-based rewrite (e.g. --remote-debugging-port=9222)
      if (portArgPattern) {
        // --flag=PORT
        if (arg.startsWith(`${portArgPattern}=`)) {
          result.push(`${portArgPattern}=${newPortStr}`);
          continue;
        }
        // --flag PORT (next arg is the port)
        if (arg === portArgPattern && i + 1 < args.length && args[i + 1] === portStr) {
          result.push(arg);
          result.push(newPortStr);
          i++; // skip the next arg
          continue;
        }
      }

      // Bare port replacement as standalone arg
      if (arg === portStr) {
        result.push(newPortStr);
        continue;
      }

      // URL-embedded port replacement (e.g. http://localhost:9222)
      if (arg.includes(`:${portStr}`)) {
        result.push(arg.replace(`:${portStr}`, `:${newPortStr}`));
        continue;
      }

      result.push(arg);
    }

    return result;
  }
}

// Re-export the helper for testing
export { isTcpPortAvailable };
