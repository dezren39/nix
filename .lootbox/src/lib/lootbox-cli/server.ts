import { parseArgs } from "@std/cli";
import { Spinner } from "@std/cli/unstable-spinner";
import { get_config } from "../get_config.ts";
import { WebSocketRpcServer } from "../rpc/websocket_server.ts";

/**
 * Sanitize server name to be a valid identifier.
 * Replaces hyphens and other invalid characters with underscores.
 */
function sanitizeServerName(name: string): string {
  return name.replace(/[^a-zA-Z0-9_]/g, "_");
}

export async function startServer(args: string[]): Promise<void> {
  const parsedArgs = parseArgs(args, {
    string: [
      "port",
      "lootbox-root",
      "lootbox-data-dir",
      "timeout",
      "rpc-timeout",
      "config",
    ],
    boolean: ["no-sandbox"],
    alias: {
      p: "port",
      r: "lootbox-root",
      d: "lootbox-data-dir",
    },
  });

  // Build a synthetic Deno.args array so get_config() picks up the flags.
  const originalArgs = Deno.args;
  const customArgs: string[] = [];

  if (parsedArgs.port)
    customArgs.push("--port", String(parsedArgs.port));
  if (parsedArgs["lootbox-root"])
    customArgs.push("--lootbox-root", parsedArgs["lootbox-root"] as string);
  if (parsedArgs["lootbox-data-dir"])
    customArgs.push("--lootbox-data-dir", parsedArgs["lootbox-data-dir"] as string);
  if (parsedArgs.timeout)
    customArgs.push("--timeout", parsedArgs.timeout as string);
  if (parsedArgs["rpc-timeout"])
    customArgs.push("--rpc-timeout", parsedArgs["rpc-timeout"] as string);
  if (parsedArgs.config)
    customArgs.push("--config", parsedArgs.config as string);
  if (parsedArgs["no-sandbox"])
    customArgs.push("--no-sandbox");

  // Forward any --allow-* / --deny-* flags that were passed through
  for (const arg of args) {
    if (arg.startsWith("--allow-") || arg.startsWith("--deny-")) {
      customArgs.push(arg);
    }
  }

  if (customArgs.length > 0) {
    Object.defineProperty(Deno, "args", { value: customArgs, writable: true });
  }

  try {
    const spinner = new Spinner({
      message: "Starting lootbox \uD83C\uDF81",
      color: "cyan",
    });
    spinner.start();

    const config = await get_config();

    // Process MCP servers from config
    let mcpConfig = null;
    if (config.mcp_servers && Object.keys(config.mcp_servers).length > 0) {
      const sanitizedServers: Record<
        string,
        (typeof config.mcp_servers)[string]
      > = {};

      for (const [serverName, serverConfig] of Object.entries(
        config.mcp_servers,
      )) {
        const sanitizedName = sanitizeServerName(serverName);
        sanitizedServers[sanitizedName] = serverConfig;
      }
      mcpConfig = { mcpServers: sanitizedServers };
    }

    const server = new WebSocketRpcServer();
    await server.start(config.port, mcpConfig, spinner);
  } catch (error) {
    console.error("Failed to start server:", error);
    Deno.exit(1);
  } finally {
    Object.defineProperty(Deno, "args", {
      value: originalArgs,
      writable: true,
    });
  }
}
