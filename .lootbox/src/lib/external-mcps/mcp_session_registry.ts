/**
 * McpSessionRegistry
 *
 * Tracks active MCP server sessions across lootbox instances.
 * Uses the XDG-compliant data directory (same as db.ts) with
 * a JSON registry file for cross-process coordination.
 *
 * Sessions include the assigned port, PID, lootbox instance port,
 * and a heartbeat timestamp updated by the health monitor.
 *
 * Stale entries (dead PID) are automatically cleaned on read.
 * File safety: atomic write via temp-file + rename.
 */

import { join } from "https://deno.land/std@0.208.0/path/mod.ts";
import {
  DEFAULT_MCP_SESSIONS_DIR,
} from "../constants.ts";

// ── Types ────────────────────────────────────────────────────────────

export interface McpSessionEntry {
  /** Sanitised MCP server name. */
  serverName: string;
  /** Unique session ID: `${serverName}-${pid}-${timestamp}`. */
  sessionId: string;
  /** PID of the lootbox process that owns this session. */
  pid: number;
  /** Port the MCP server is connected to (e.g. 9222). */
  port: number;
  /** Originally configured port (before auto-port). */
  originalPort: number;
  /** ISO timestamp when the session was created. */
  startedAt: string;
  /** ISO timestamp of last heartbeat (updated by health monitor). */
  lastHeartbeat: string;
  /** Which lootbox HTTP port this instance is running on. */
  lootboxPort: number;
  /** Working directory of the lootbox instance. */
  workdir: string;
}

export interface McpSessionRegistryData {
  version: 1;
  sessions: McpSessionEntry[];
}

// ── Helpers ──────────────────────────────────────────────────────────

/**
 * H1 fix: Platform-safe process-alive check.
 *
 * On Unix (macOS/Linux), we read /proc/{pid} or use `kill -0` semantics.
 * Deno doesn't expose signal 0, but on macOS /proc doesn't exist either.
 * Strategy:
 *   - Windows: Use Deno.Command("tasklist") to check if PID exists.
 *   - macOS:   Use Deno.Command("kill", ["-0", pid]) which is a no-op probe.
 *   - Linux:   Check /proc/{pid}/status existence (fast, no signals).
 *
 * Falls back to assuming alive if the check itself errors (safe default
 * — stale entries will accumulate but won't be incorrectly removed).
 */
function isProcessAlive(pid: number): boolean {
  try {
    const os = Deno.build.os;
    if (os === "linux") {
      // /proc is always available on Linux
      try {
        Deno.statSync(`/proc/${pid}`);
        return true;
      } catch {
        return false;
      }
    } else if (os === "darwin") {
      // macOS: use kill -0 via Deno.Command (synchronous check)
      const result = new Deno.Command("kill", {
        args: ["-0", String(pid)],
        stdout: "null",
        stderr: "null",
      }).outputSync();
      return result.code === 0;
    } else {
      // Windows: use tasklist to check for the PID
      const result = new Deno.Command("tasklist", {
        args: ["/FI", `PID eq ${pid}`, "/NH"],
        stdout: "piped",
        stderr: "null",
      }).outputSync();
      const output = new TextDecoder().decode(result.stdout);
      return output.includes(String(pid));
    }
  } catch {
    // If we can't determine, assume alive (safe default — avoids data loss)
    return true;
  }
}

/** Get platform-specific data directory (mirrors db.ts logic). */
function getDefaultDataDir(): string {
  const platform = Deno.build.os;
  if (platform === "windows") {
    const appData = Deno.env.get("APPDATA") || Deno.env.get("USERPROFILE");
    return appData ? join(appData, "lootbox") : join(Deno.cwd(), "lootbox-data");
  } else if (platform === "darwin") {
    const home = Deno.env.get("HOME");
    return home
      ? join(home, "Library", "Application Support", "lootbox")
      : join(Deno.cwd(), "lootbox-data");
  } else {
    const xdgDataHome = Deno.env.get("XDG_DATA_HOME");
    const home = Deno.env.get("HOME");
    if (xdgDataHome) return join(xdgDataHome, "lootbox");
    if (home) return join(home, ".local", "share", "lootbox");
    return join(Deno.cwd(), "lootbox-data");
  }
}

// ── McpSessionRegistry ──────────────────────────────────────────────

export class McpSessionRegistry {
  private sessionsDir: string;
  private registryPath: string;

  constructor(dataDir?: string) {
    const baseDir = dataDir || getDefaultDataDir();
    this.sessionsDir = join(baseDir, DEFAULT_MCP_SESSIONS_DIR);
    this.registryPath = join(this.sessionsDir, "registry.json");
  }

  /** Ensure the sessions directory exists. */
  private async ensureDir(): Promise<void> {
    try {
      await Deno.mkdir(this.sessionsDir, { recursive: true });
    } catch (error) {
      if (!(error instanceof Deno.errors.AlreadyExists)) throw error;
    }
  }

  /**
   * Read the registry from disk, cleaning stale entries (dead PIDs).
   * Returns an empty registry if the file doesn't exist.
   */
  async read(): Promise<McpSessionRegistryData> {
    await this.ensureDir();

    let data: McpSessionRegistryData;
    try {
      const text = await Deno.readTextFile(this.registryPath);
      data = JSON.parse(text) as McpSessionRegistryData;
    } catch {
      data = { version: 1, sessions: [] };
    }

    // Clean stale entries
    const before = data.sessions.length;
    data.sessions = data.sessions.filter((s) => isProcessAlive(s.pid));
    if (data.sessions.length < before) {
      const removed = before - data.sessions.length;
      console.error(
        `[McpSessionRegistry] Cleaned ${removed} stale session(s)`,
      );
      await this.write(data);
    }

    return data;
  }

  /**
   * Write the registry atomically (write temp → rename).
   */
  private async write(data: McpSessionRegistryData): Promise<void> {
    await this.ensureDir();
    const tmp = this.registryPath + `.tmp.${Deno.pid}`;
    await Deno.writeTextFile(tmp, JSON.stringify(data, null, 2));
    await Deno.rename(tmp, this.registryPath);
  }

  /**
   * Register a new session.
   */
  async register(entry: McpSessionEntry): Promise<void> {
    const data = await this.read();

    // Remove any existing entry for same sessionId (in case of restart)
    data.sessions = data.sessions.filter(
      (s) => s.sessionId !== entry.sessionId,
    );

    data.sessions.push(entry);
    await this.write(data);

    console.error(
      `[McpSessionRegistry] Registered session ${entry.sessionId} ` +
        `(${entry.serverName} on port ${entry.port})`,
    );
  }

  /**
   * Remove a session by sessionId.
   */
  async deregister(sessionId: string): Promise<void> {
    const data = await this.read();
    const before = data.sessions.length;
    data.sessions = data.sessions.filter((s) => s.sessionId !== sessionId);

    if (data.sessions.length < before) {
      await this.write(data);
      console.error(
        `[McpSessionRegistry] Deregistered session ${sessionId}`,
      );
    }
  }

  /**
   * Remove all sessions owned by the current process.
   */
  async deregisterByPid(pid?: number): Promise<void> {
    const targetPid = pid ?? Deno.pid;
    const data = await this.read();
    const before = data.sessions.length;
    data.sessions = data.sessions.filter((s) => s.pid !== targetPid);

    if (data.sessions.length < before) {
      const removed = before - data.sessions.length;
      await this.write(data);
      console.error(
        `[McpSessionRegistry] Deregistered ${removed} session(s) for PID ${targetPid}`,
      );
    }
  }

  /**
   * Update the heartbeat timestamp for a session.
   */
  async updateHeartbeat(sessionId: string): Promise<void> {
    const data = await this.read();
    const entry = data.sessions.find((s) => s.sessionId === sessionId);
    if (entry) {
      entry.lastHeartbeat = new Date().toISOString();
      await this.write(data);
    }
  }

  /**
   * Find all active sessions for a given server name.
   */
  async findByServerName(serverName: string): Promise<McpSessionEntry[]> {
    const data = await this.read();
    return data.sessions.filter((s) => s.serverName === serverName);
  }

  /**
   * Find all active sessions using a specific port.
   */
  async findByPort(port: number): Promise<McpSessionEntry[]> {
    const data = await this.read();
    return data.sessions.filter((s) => s.port === port);
  }

  /**
   * Find a session by its unique session ID.
   */
  async findBySessionId(
    sessionId: string,
  ): Promise<McpSessionEntry | undefined> {
    const data = await this.read();
    return data.sessions.find((s) => s.sessionId === sessionId);
  }

  /**
   * Generate a unique session ID for a server.
   * M9 fix: Includes a random suffix to prevent collisions within the same ms.
   */
  static generateSessionId(serverName: string): string {
    const rand = Math.random().toString(36).substring(2, 8);
    return `${serverName}-${Deno.pid}-${Date.now()}-${rand}`;
  }

  /** Get the path to the registry file (for testing). */
  getRegistryPath(): string {
    return this.registryPath;
  }
}
