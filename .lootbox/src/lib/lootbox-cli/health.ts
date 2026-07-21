/**
 * lootbox health — CLI command to query the running server's health endpoint.
 *
 * Usage:
 *   lootbox health            Pretty-print health status
 *   lootbox health --json     Machine-readable JSON output
 *
 * Exit codes:
 *   0 = ok
 *   1 = degraded
 *   2 = unhealthy
 *   3 = unreachable (server not running)
 */

import { get_config } from "../get_config.ts";

/** ANSI helpers (no-op when stdout is not a terminal). */
const isTTY = Deno.stdout.isTerminal();
const green = (s: string) => (isTTY ? `\x1b[32m${s}\x1b[0m` : s);
const yellow = (s: string) => (isTTY ? `\x1b[33m${s}\x1b[0m` : s);
const red = (s: string) => (isTTY ? `\x1b[31m${s}\x1b[0m` : s);
const dim = (s: string) => (isTTY ? `\x1b[2m${s}\x1b[0m` : s);

function statusColor(status: string): string {
  switch (status) {
    case "ok":
    case "connected":
      return green(status);
    case "degraded":
    case "reconnecting":
      return yellow(status);
    case "unhealthy":
    case "failed":
    case "disconnected":
      return red(status);
    default:
      return status;
  }
}

function statusIcon(status: string): string {
  switch (status) {
    case "ok":
    case "connected":
      return green("✅");
    case "degraded":
    case "reconnecting":
      return yellow("⚠️");
    case "unhealthy":
    case "failed":
    case "disconnected":
      return red("❌");
    default:
      return "❓";
  }
}

function formatUptime(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (days > 0) return `${days}d ${hours % 24}h ${minutes % 60}m`;
  if (hours > 0) return `${hours}h ${minutes % 60}m`;
  if (minutes > 0) return `${minutes}m ${seconds % 60}s`;
  return `${seconds}s`;
}

function formatTimeSince(iso: string | null): string {
  if (!iso) return dim("never");
  const ms = Date.now() - new Date(iso).getTime();
  if (ms < 1000) return "just now";
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s ago`;
  return `${Math.floor(ms / 60_000)}m ago`;
}

export async function healthCommand(jsonOutput: boolean): Promise<void> {
  const config = await get_config();

  // Build HTTP health URL from server config
  const wsUrl = config.server_url; // e.g. ws://localhost:3000/ws
  const httpUrl = wsUrl
    .replace(/^ws:/, "http:")
    .replace(/^wss:/, "https:")
    .replace(/\/ws$/, config.health_path);

  try {
    const response = await fetch(httpUrl, {
      signal: AbortSignal.timeout(5_000),
    });

    if (!response.ok) {
      console.error(`Error: Health endpoint returned HTTP ${response.status}`);
      Deno.exit(3);
    }

    const data = await response.json();

    if (jsonOutput) {
      console.log(JSON.stringify(data, null, 2));
    } else {
      prettyPrint(data);
    }

    // Exit code based on status
    switch (data.status) {
      case "ok":
        Deno.exit(0);
        break;
      case "degraded":
        Deno.exit(1);
        break;
      case "unhealthy":
        Deno.exit(2);
        break;
      default:
        Deno.exit(0);
    }
  } catch (error) {
    if (jsonOutput) {
      console.log(JSON.stringify({ status: "unreachable", error: String(error) }));
    } else {
      console.error(
        `${red("❌")} Cannot reach lootbox server at ${httpUrl}`,
      );
      console.error(
        dim(
          error instanceof Error ? error.message : String(error),
        ),
      );
      console.error(dim("\nIs the server running? Start it with: lootbox server"));
    }
    Deno.exit(3);
  }
}

function prettyPrint(data: Record<string, unknown>): void {
  const status = data.status as string;
  const uptimeMs = data.uptime_ms as number;
  const subsystems = data.subsystems as Record<string, unknown>;

  console.log(
    `\n${statusIcon(status)} Lootbox Server Health: ${statusColor(status.toUpperCase())}` +
      dim(` (uptime: ${formatUptime(uptimeMs)})`),
  );
  console.log();

  // Workers
  const workers = subsystems.workers as Record<string, unknown>;
  const wTotal = workers.total as number;
  const wReady = workers.ready as number;
  console.log(
    `  Workers: ${wReady}/${wTotal} ready ${statusIcon(workers.status as string)}`,
  );
  if ((workers.failed as number) > 0) {
    console.log(`    ${red(`${workers.failed} failed`)}`);
  }
  if ((workers.crashed as number) > 0) {
    console.log(`    ${yellow(`${workers.crashed} crashed (restarting)`)}`);
  }
  if ((workers.starting as number) > 0) {
    console.log(`    ${dim(`${workers.starting} starting...`)}`);
  }

  // MCP Servers
  const mcpServers = subsystems.mcp_servers as Record<string, unknown>;
  const servers = mcpServers.servers as Record<string, Record<string, unknown>>;
  const serverNames = Object.keys(servers);

  if (serverNames.length > 0) {
    console.log(`\n  MCP Servers:`);
    // Find longest name for alignment
    const maxLen = Math.max(...serverNames.map((n) => n.length));
    for (const [name, info] of Object.entries(servers)) {
      const padded = name.padEnd(maxLen);
      const lastCheck = formatTimeSince(info.last_health_check as string | null);
      const reconnects = info.reconnect_attempts as number;

      let line = `    ${padded}: ${statusColor(info.status as string)} ${statusIcon(info.status as string)}`;
      line += dim(` (last check: ${lastCheck})`);
      if (reconnects > 0) {
        line += yellow(` [${reconnects} reconnect attempts]`);
      }
      console.log(line);
    }
  } else {
    console.log(dim("\n  No MCP servers configured"));
  }

  console.log();
}
