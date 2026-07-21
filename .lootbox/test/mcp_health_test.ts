/**
 * Unit tests for MCP health monitoring, McpClientManager state tracking,
 * McpHealthMonitor config resolution, and mcp_config.ts validation of
 * health/multiClient fields.
 */

import {
  assertEquals,
  assertExists,
  assertStrictEquals,
} from "jsr:@std/assert";

import {
  DEFAULT_MCP_HEALTH_CHECK_INTERVAL_MS,
  DEFAULT_MCP_MAX_RECONNECT_ATTEMPTS,
  DEFAULT_MCP_RECONNECT_BACKOFF_BASE_MS,
  DEFAULT_MCP_MAX_RECONNECT_BACKOFF_MS,
  DEFAULT_MCP_HEALTH_CHECK_TIMEOUT_MS,
  DEFAULT_MCP_MULTI_CLIENT_STRATEGY,
  DEFAULT_MCP_AUTO_PORT_RANGE,
  DEFAULT_MCP_SESSIONS_DIR,
} from "../src/lib/constants.ts";

import {
  McpHealthMonitor,
  type McpHealthGlobalDefaults,
} from "../src/lib/external-mcps/mcp_health_monitor.ts";

import { validateMcpConfig } from "../src/lib/external-mcps/mcp_config.ts";

// ── Constants tests ──────────────────────────────────────────────────

Deno.test("MCP health constants have expected default values", () => {
  assertStrictEquals(DEFAULT_MCP_HEALTH_CHECK_INTERVAL_MS, 30_000);
  assertStrictEquals(DEFAULT_MCP_MAX_RECONNECT_ATTEMPTS, 5);
  assertStrictEquals(DEFAULT_MCP_RECONNECT_BACKOFF_BASE_MS, 2_000);
  assertStrictEquals(DEFAULT_MCP_MAX_RECONNECT_BACKOFF_MS, 60_000);
  assertStrictEquals(DEFAULT_MCP_HEALTH_CHECK_TIMEOUT_MS, 5_000);
});

Deno.test("MCP multi-client constants have expected default values", () => {
  assertStrictEquals(DEFAULT_MCP_MULTI_CLIENT_STRATEGY, "warn");
  assertEquals(DEFAULT_MCP_AUTO_PORT_RANGE, [9222, 9299]);
  assertStrictEquals(DEFAULT_MCP_SESSIONS_DIR, "mcp-sessions");
});

// ── McpHealthMonitor config resolution tests ─────────────────────────

Deno.test("resolveHealthConfig returns global defaults when no per-server config", () => {
  const globals: McpHealthGlobalDefaults = {
    checkInterval: 15_000,
    maxReconnectAttempts: 3,
    reconnectBackoffBase: 1_000,
    maxReconnectBackoff: 30_000,
    checkTimeout: 2_000,
  };

  // We need a minimal McpClientManager mock for the constructor
  const mockClientManager = {
    getClient: () => undefined,
    getConnectedServerNames: () => [],
    getAllServerNames: () => [],
    getConnectionState: () => "unknown",
    getReconnectAttempts: () => 0,
    markDisconnected: () => {},
    markFailed: () => {},
    updateHealthCheck: () => {},
    getServerHealth: () => ({}),
    onEvent: () => {},
  } as any;

  const monitor = new McpHealthMonitor(mockClientManager, globals);

  const resolved = monitor.resolveHealthConfig(undefined);
  assertEquals(resolved, globals);
});

Deno.test("resolveHealthConfig merges per-server overrides with globals", () => {
  const globals: McpHealthGlobalDefaults = {
    checkInterval: 30_000,
    maxReconnectAttempts: 5,
    reconnectBackoffBase: 2_000,
    maxReconnectBackoff: 60_000,
    checkTimeout: 5_000,
  };

  const mockClientManager = {
    getClient: () => undefined,
    getConnectedServerNames: () => [],
    getAllServerNames: () => [],
    getConnectionState: () => "unknown",
    getReconnectAttempts: () => 0,
    markDisconnected: () => {},
    markFailed: () => {},
    updateHealthCheck: () => {},
    getServerHealth: () => ({}),
    onEvent: () => {},
  } as any;

  const monitor = new McpHealthMonitor(mockClientManager, globals);

  // Override only checkInterval and maxReconnectAttempts
  const resolved = monitor.resolveHealthConfig({
    checkInterval: 10_000,
    maxReconnectAttempts: 10,
  });

  assertEquals(resolved, {
    checkInterval: 10_000,
    maxReconnectAttempts: 10,
    reconnectBackoffBase: 2_000,   // from global
    maxReconnectBackoff: 60_000,   // from global
    checkTimeout: 5_000,           // from global
  });
});

Deno.test("resolveHealthConfig per-server overrides all fields", () => {
  const globals: McpHealthGlobalDefaults = {
    checkInterval: 30_000,
    maxReconnectAttempts: 5,
    reconnectBackoffBase: 2_000,
    maxReconnectBackoff: 60_000,
    checkTimeout: 5_000,
  };

  const mockClientManager = { onEvent: () => {} } as any;
  const monitor = new McpHealthMonitor(mockClientManager, globals);

  const perServer = {
    checkInterval: 1_000,
    maxReconnectAttempts: 99,
    reconnectBackoffBase: 500,
    maxReconnectBackoff: 10_000,
    checkTimeout: 1_000,
  };

  const resolved = monitor.resolveHealthConfig(perServer);
  assertEquals(resolved, perServer);
});

// ── McpHealthMonitor lifecycle tests ─────────────────────────────────

Deno.test("McpHealthMonitor starts and stops cleanly", () => {
  const mockClientManager = {
    getClient: () => undefined,
    getConnectedServerNames: () => [],
    getAllServerNames: () => [],
    getConnectionState: () => "unknown",
    getReconnectAttempts: () => 0,
    markDisconnected: () => {},
    markFailed: () => {},
    updateHealthCheck: () => {},
    getServerHealth: () => ({}),
    onEvent: () => {},
  } as any;

  const globals: McpHealthGlobalDefaults = {
    checkInterval: 60_000, // long interval so no actual checks fire
    maxReconnectAttempts: 3,
    reconnectBackoffBase: 1_000,
    maxReconnectBackoff: 30_000,
    checkTimeout: 5_000,
  };

  const monitor = new McpHealthMonitor(mockClientManager, globals);

  assertStrictEquals(monitor.isRunning(), false);

  monitor.start({
    test_server: {
      command: "echo",
      args: ["test"],
    },
  });

  assertStrictEquals(monitor.isRunning(), true);

  monitor.stop();

  assertStrictEquals(monitor.isRunning(), false);
});

Deno.test("McpHealthMonitor emits events via onEvent listener", () => {
  const events: string[] = [];

  const mockClientManager = { onEvent: () => {} } as any;
  const globals: McpHealthGlobalDefaults = {
    checkInterval: 60_000,
    maxReconnectAttempts: 3,
    reconnectBackoffBase: 1_000,
    maxReconnectBackoff: 30_000,
    checkTimeout: 5_000,
  };

  const monitor = new McpHealthMonitor(mockClientManager, globals);
  monitor.onEvent((event) => events.push(event.type));

  // Manually test the event system is wired
  // (We can't easily trigger real health checks without MCP servers)
  assertExists(monitor.onEvent);
  assertEquals(events.length, 0);

  monitor.stop();
});

// ── mcp_config.ts validation tests ───────────────────────────────────

Deno.test("validateMcpConfig accepts health config", () => {
  const config = {
    mcpServers: {
      "test-server": {
        command: "echo",
        args: ["test"],
        health: {
          checkInterval: 15000,
          maxReconnectAttempts: 10,
        },
      },
    },
  };

  const result = validateMcpConfig(config);
  assertExists(result.mcpServers.test_server);
  assertExists(result.mcpServers.test_server.health);
  assertStrictEquals(result.mcpServers.test_server.health?.checkInterval, 15000);
  assertStrictEquals(result.mcpServers.test_server.health?.maxReconnectAttempts, 10);
});

Deno.test("validateMcpConfig accepts multiClient config", () => {
  const config = {
    mcpServers: {
      "chrome-devtools": {
        command: "npx",
        args: ["chrome-devtools-mcp@latest"],
        multiClient: {
          strategy: "auto-port",
          portRange: [9222, 9299],
          portArgPattern: "--browserUrl",
        },
      },
    },
  };

  const result = validateMcpConfig(config);
  const server = result.mcpServers.chrome_devtools;
  assertExists(server);
  assertExists(server.multiClient);
  assertStrictEquals(server.multiClient?.strategy, "auto-port");
  assertEquals(server.multiClient?.portRange, [9222, 9299]);
  assertStrictEquals(server.multiClient?.portArgPattern, "--browserUrl");
});

Deno.test("validateMcpConfig rejects invalid health.checkInterval", () => {
  const config = {
    mcpServers: {
      test: {
        command: "echo",
        args: ["test"],
        health: { checkInterval: -1 },
      },
    },
  };

  let threw = false;
  try {
    validateMcpConfig(config);
  } catch (e) {
    threw = true;
    assertEquals(
      (e as Error).message.includes("health.checkInterval must be a positive number"),
      true,
    );
  }
  assertEquals(threw, true);
});

Deno.test("validateMcpConfig rejects invalid multiClient.strategy", () => {
  const config = {
    mcpServers: {
      test: {
        command: "echo",
        args: ["test"],
        multiClient: { strategy: "invalid" },
      },
    },
  };

  let threw = false;
  try {
    validateMcpConfig(config);
  } catch (e) {
    threw = true;
    assertEquals(
      (e as Error).message.includes("multiClient.strategy must be one of"),
      true,
    );
  }
  assertEquals(threw, true);
});

Deno.test("validateMcpConfig rejects invalid multiClient.portRange", () => {
  const config = {
    mcpServers: {
      test: {
        command: "echo",
        args: ["test"],
        multiClient: { portRange: [9300, 9200] }, // start > end
      },
    },
  };

  let threw = false;
  try {
    validateMcpConfig(config);
  } catch (e) {
    threw = true;
    assertEquals(
      (e as Error).message.includes("multiClient.portRange must be [start, end]"),
      true,
    );
  }
  assertEquals(threw, true);
});

Deno.test("validateMcpConfig rejects non-number health.maxReconnectAttempts", () => {
  const config = {
    mcpServers: {
      test: {
        command: "echo",
        args: ["test"],
        health: { maxReconnectAttempts: "five" },
      },
    },
  };

  let threw = false;
  try {
    validateMcpConfig(config);
  } catch (e) {
    threw = true;
    assertEquals(
      (e as Error).message.includes("health.maxReconnectAttempts must be >= 0"),
      true,
    );
  }
  assertEquals(threw, true);
});

Deno.test("validateMcpConfig passes through servers without health/multiClient", () => {
  const config = {
    mcpServers: {
      simple: {
        command: "echo",
        args: ["hello"],
      },
    },
  };

  const result = validateMcpConfig(config);
  assertExists(result.mcpServers.simple);
  assertEquals(result.mcpServers.simple.health, undefined);
  assertEquals(result.mcpServers.simple.multiClient, undefined);
});

// ── ResolvedConfig MCP health fields test ────────────────────────────

Deno.test("ResolvedConfig includes MCP health and multi-client defaults", async () => {
  const { get_config, resetConfigCache } = await import("../src/lib/get_config.ts");
  resetConfigCache();

  const config = await get_config();

  // These should exist and have default values from constants.ts
  assertStrictEquals(config.mcp_health_check_interval, DEFAULT_MCP_HEALTH_CHECK_INTERVAL_MS);
  assertStrictEquals(config.mcp_max_reconnect_attempts, DEFAULT_MCP_MAX_RECONNECT_ATTEMPTS);
  assertStrictEquals(config.mcp_reconnect_backoff_base, DEFAULT_MCP_RECONNECT_BACKOFF_BASE_MS);
  assertStrictEquals(config.mcp_max_reconnect_backoff, DEFAULT_MCP_MAX_RECONNECT_BACKOFF_MS);
  assertStrictEquals(config.mcp_health_check_timeout, DEFAULT_MCP_HEALTH_CHECK_TIMEOUT_MS);
  assertStrictEquals(config.mcp_default_multi_client_strategy, "warn");

  resetConfigCache();
});
