// MCP server configuration management

import type { McpHealthConfig, McpMultiClientConfig } from "../lootbox-cli/types.ts";

/**
  McpServerConfig: add supports to all transports
**/
interface McpServerConfigBase {
  transport?: "stdio" | "streamable_http" | "sse";
  /** Per-server health-check and reconnection overrides. */
  health?: McpHealthConfig;
  /** Per-server multi-client strategy overrides. */
  multiClient?: McpMultiClientConfig;
}

interface McpServerConfigStdio extends McpServerConfigBase {
  command: string;
  args: string[];
  env?: Record<string, string>;
  transport?: "stdio";
  url?: never;
  headers?: never;
}

interface McpServerConfigHttp extends McpServerConfigBase {
  url: string;
  transport: "streamable_http" | "sse";
  headers?: Record<string, string>[];
  command?: never;
  args?: never;
}

export type McpServerConfig = McpServerConfigStdio | McpServerConfigHttp;

/**
 * Sanitize server name to be a valid identifier
 * Replaces hyphens and other invalid characters with underscores
 */
function sanitizeServerName(name: string): string {
  return name.replace(/[^a-zA-Z0-9_]/g, "_");
}

export interface McpConfigFile {
  mcpServers: Record<string, McpServerConfig>;
}

/**
 * Load and parse MCP configuration from a .mcp.json file
 */
export async function loadMcpConfig(path: string): Promise<McpConfigFile> {
  try {
    const content = await Deno.readTextFile(path);
    const parsed = JSON.parse(content);
    return validateMcpConfig(parsed);
  } catch (error) {
    if (error instanceof Deno.errors.NotFound) {
      throw new Error(`MCP config file not found: ${path}`);
    }
    if (error instanceof SyntaxError) {
      throw new Error(`Invalid JSON in MCP config file: ${path}`);
    }
    throw error;
  }
}

/**
 * Validate MCP configuration structure and filter out mcp-rpc-bridge
 */
export function validateMcpConfig(config: unknown): McpConfigFile {
  if (typeof config !== "object" || config === null) {
    throw new Error("MCP config must be an object");
  }

  const configObj = config as Record<string, unknown>;

  if (!("mcpServers" in configObj)) {
    throw new Error("MCP config must have 'mcpServers' field");
  }

  if (
    typeof configObj.mcpServers !== "object" ||
    configObj.mcpServers === null
  ) {
    throw new Error("'mcpServers' must be an object");
  }

  const mcpServers = configObj.mcpServers as Record<string, unknown>;
  const validated: Record<string, McpServerConfig> = {};

  for (const [serverName, serverConfig] of Object.entries(mcpServers)) {
    // Skip our own mcp-rpc-bridge server
    if (typeof serverConfig === "object" && serverConfig !== null) {
      const cfg = serverConfig as Record<string, unknown>;
      if (cfg.command === "mcp-rpc-bridge") {
        console.error(`Skipping mcp-rpc-bridge server: ${serverName}`);
        continue;
      }
    }
    if (typeof serverConfig !== "object" || serverConfig === null) {
      throw new Error(`Server config for '${serverName}' must be an object`);
    }

    const cfg = serverConfig as Record<string, unknown>;

    // H5 fix: Detect transport type first, then validate appropriate fields.
    const transport = cfg.transport as string | undefined;
    let validatedConfig: McpServerConfig;

    if (transport === "streamable_http" || transport === "sse") {
      // HTTP/SSE transport — requires url, not command/args
      if (typeof cfg.url !== "string" || !cfg.url) {
        throw new Error(
          `Server '${serverName}' with transport '${transport}' must have 'url' string`
        );
      }
      validatedConfig = {
        transport,
        url: cfg.url,
      } as McpServerConfig;

      if (cfg.headers !== undefined) {
        if (!Array.isArray(cfg.headers)) {
          throw new Error(`Server '${serverName}' headers must be an array`);
        }
        (validatedConfig as unknown as Record<string, unknown>).headers = cfg.headers;
      }
    } else {
      // stdio transport (default) — requires command/args
      if (typeof cfg.command !== "string") {
        throw new Error(`Server '${serverName}' must have 'command' string`);
      }

      if (!Array.isArray(cfg.args)) {
        throw new Error(`Server '${serverName}' must have 'args' array`);
      }

      if (!cfg.args.every((arg) => typeof arg === "string")) {
        throw new Error(
          `Server '${serverName}' args must be array of strings`
        );
      }

      validatedConfig = {
        command: cfg.command,
        args: cfg.args as string[],
      } as McpServerConfig;

      if (transport === "stdio") {
        (validatedConfig as unknown as Record<string, unknown>).transport = "stdio";
      }

      if (cfg.env !== undefined) {
        if (typeof cfg.env !== "object" || cfg.env === null) {
          throw new Error(`Server '${serverName}' env must be an object`);
        }
        const env = cfg.env as Record<string, unknown>;
        if (!Object.values(env).every((val) => typeof val === "string")) {
          throw new Error(
            `Server '${serverName}' env values must be strings`
          );
        }
        (validatedConfig as unknown as Record<string, unknown>).env = env as Record<string, string>;
      }
    }

    // Parse optional per-server health config
    if (cfg.health !== undefined) {
      if (typeof cfg.health !== "object" || cfg.health === null) {
        throw new Error(`Server '${serverName}' health must be an object`);
      }
      const h = cfg.health as Record<string, unknown>;
      const healthConfig: McpHealthConfig = {};
      if (h.checkInterval !== undefined) {
        if (typeof h.checkInterval !== "number" || h.checkInterval <= 0) {
          throw new Error(`Server '${serverName}' health.checkInterval must be a positive number`);
        }
        healthConfig.checkInterval = h.checkInterval;
      }
      if (h.maxReconnectAttempts !== undefined) {
        if (typeof h.maxReconnectAttempts !== "number" || h.maxReconnectAttempts < 0) {
          throw new Error(`Server '${serverName}' health.maxReconnectAttempts must be >= 0`);
        }
        healthConfig.maxReconnectAttempts = h.maxReconnectAttempts;
      }
      if (h.reconnectBackoffBase !== undefined) {
        if (typeof h.reconnectBackoffBase !== "number" || h.reconnectBackoffBase <= 0) {
          throw new Error(`Server '${serverName}' health.reconnectBackoffBase must be a positive number`);
        }
        healthConfig.reconnectBackoffBase = h.reconnectBackoffBase;
      }
      if (h.maxReconnectBackoff !== undefined) {
        if (typeof h.maxReconnectBackoff !== "number" || h.maxReconnectBackoff <= 0) {
          throw new Error(`Server '${serverName}' health.maxReconnectBackoff must be a positive number`);
        }
        healthConfig.maxReconnectBackoff = h.maxReconnectBackoff;
      }
      if (h.checkTimeout !== undefined) {
        if (typeof h.checkTimeout !== "number" || h.checkTimeout <= 0) {
          throw new Error(`Server '${serverName}' health.checkTimeout must be a positive number`);
        }
        healthConfig.checkTimeout = h.checkTimeout;
      }
      validatedConfig.health = healthConfig;
    }

    // Parse optional per-server multi-client config
    if (cfg.multiClient !== undefined) {
      if (typeof cfg.multiClient !== "object" || cfg.multiClient === null) {
        throw new Error(`Server '${serverName}' multiClient must be an object`);
      }
      const mc = cfg.multiClient as Record<string, unknown>;
      const multiClientConfig: McpMultiClientConfig = {};
      if (mc.strategy !== undefined) {
        const validStrategies = ["warn", "fail", "auto-port", "per-session"];
        if (typeof mc.strategy !== "string" || !validStrategies.includes(mc.strategy)) {
          throw new Error(
            `Server '${serverName}' multiClient.strategy must be one of: ${validStrategies.join(", ")}`
          );
        }
        multiClientConfig.strategy = mc.strategy as McpMultiClientConfig["strategy"];
      }
      if (mc.portRange !== undefined) {
        if (
          !Array.isArray(mc.portRange) ||
          mc.portRange.length !== 2 ||
          typeof mc.portRange[0] !== "number" ||
          typeof mc.portRange[1] !== "number" ||
          mc.portRange[0] > mc.portRange[1]
        ) {
          throw new Error(
            `Server '${serverName}' multiClient.portRange must be [start, end] with start <= end`
          );
        }
        multiClientConfig.portRange = mc.portRange as [number, number];
      }
      if (mc.portArgPattern !== undefined) {
        if (typeof mc.portArgPattern !== "string") {
          throw new Error(`Server '${serverName}' multiClient.portArgPattern must be a string`);
        }
        multiClientConfig.portArgPattern = mc.portArgPattern;
      }
      validatedConfig.multiClient = multiClientConfig;
    }

    // Sanitize server name to ensure it's a valid identifier
    const sanitizedName = sanitizeServerName(serverName);
    if (sanitizedName !== serverName) {
      console.error(`Sanitized MCP server name: '${serverName}' -> '${sanitizedName}'`);
    }
    validated[sanitizedName] = validatedConfig;
  }

  return { mcpServers: validated };
}