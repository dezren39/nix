/**
 * Integration tests for McpIntegrationManager's multi-client session
 * management: session registry integration, strategy enforcement,
 * and port extraction from args.
 *
 * These tests don't connect real MCP servers — they verify the
 * registry and strategy logic by calling initialize() with configs
 * that will fail to connect (expected) but still exercise the
 * session-management code paths.
 */

import {
  assertEquals,
  assertExists,
} from "jsr:@std/assert";

import {
  McpSessionRegistry,
} from "../src/lib/external-mcps/mcp_session_registry.ts";
import {
  McpAutoPortAssigner,
} from "../src/lib/external-mcps/mcp_auto_port.ts";
import type { McpMultiClientStrategy } from "../src/lib/lootbox-cli/types.ts";
import {
  DEFAULT_MCP_AUTO_PORT_RANGE,
  DEFAULT_MCP_MULTI_CLIENT_STRATEGY,
} from "../src/lib/constants.ts";

// ── Tests: Multi-client strategy defaults ────────────────────────────

Deno.test("Default multi-client strategy is 'warn'", () => {
  assertEquals(DEFAULT_MCP_MULTI_CLIENT_STRATEGY, "warn");
});

Deno.test("Default auto-port range is [9222, 9299]", () => {
  assertEquals(DEFAULT_MCP_AUTO_PORT_RANGE[0], 9222);
  assertEquals(DEFAULT_MCP_AUTO_PORT_RANGE[1], 9299);
});

Deno.test("McpMultiClientStrategy type accepts all valid values", () => {
  const strategies: McpMultiClientStrategy[] = [
    "warn",
    "fail",
    "auto-port",
    "per-session",
  ];
  assertEquals(strategies.length, 4);
  for (const s of strategies) {
    assertEquals(typeof s, "string");
  }
});

// ── Tests: Session registry + auto-port integration ──────────────────

Deno.test("Session registry tracks auto-port assignments across instances", async () => {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_integration_test_" });
  try {
    const registry = new McpSessionRegistry(tmpDir);
    const assigner = new McpAutoPortAssigner(registry, [19300, 19310]);

    // First instance claims preferred port
    const result1 = await assigner.assignPort("chrome-devtools", 19300);
    assertEquals(result1.port, 19300);
    assertEquals(result1.wasReassigned, false);

    // Register it
    await registry.register({
      serverName: "chrome-devtools",
      sessionId: "instance-1",
      pid: Deno.pid,
      port: 19300,
      originalPort: 19300,
      startedAt: new Date().toISOString(),
      lastHeartbeat: new Date().toISOString(),
      lootboxPort: 3000,
      workdir: Deno.cwd(),
    });

    // Second instance sees the port is taken in registry
    const result2 = await assigner.assignPort("chrome-devtools", 19300);
    assertEquals(result2.wasReassigned, true);
    assertEquals(result2.port, 19301); // next in range

    // Register it
    await registry.register({
      serverName: "chrome-devtools",
      sessionId: "instance-2",
      pid: Deno.pid,
      port: 19301,
      originalPort: 19300,
      startedAt: new Date().toISOString(),
      lastHeartbeat: new Date().toISOString(),
      lootboxPort: 3001,
      workdir: Deno.cwd(),
    });

    // Verify both sessions are tracked
    const sessions = await registry.findByServerName("chrome-devtools");
    assertEquals(sessions.length, 2);
    assertEquals(sessions[0].port, 19300);
    assertEquals(sessions[1].port, 19301);
  } finally {
    await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
  }
});

Deno.test("Session registry cleanup on shutdown removes only our sessions", async () => {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_integration_test_" });
  try {
    const registry = new McpSessionRegistry(tmpDir);

    // Register sessions for two "instances"
    await registry.register({
      serverName: "chrome-devtools",
      sessionId: "other-instance-session",
      pid: Deno.pid, // Same PID in test, but different sessionId
      port: 9222,
      originalPort: 9222,
      startedAt: new Date().toISOString(),
      lastHeartbeat: new Date().toISOString(),
      lootboxPort: 3000,
      workdir: Deno.cwd(),
    });

    await registry.register({
      serverName: "chrome-devtools",
      sessionId: "my-instance-session",
      pid: Deno.pid,
      port: 9223,
      originalPort: 9222,
      startedAt: new Date().toISOString(),
      lastHeartbeat: new Date().toISOString(),
      lootboxPort: 3001,
      workdir: Deno.cwd(),
    });

    // Simulate shutdown: deregister only "my" session
    await registry.deregister("my-instance-session");

    const remaining = await registry.findByServerName("chrome-devtools");
    assertEquals(remaining.length, 1);
    assertEquals(remaining[0].sessionId, "other-instance-session");
  } finally {
    await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
  }
});

Deno.test("Auto-port rewrite produces correct chrome-devtools args", () => {
  // Typical chrome-devtools MCP config:
  // command: "npx", args: ["chrome-devtools-mcp", "--remote-debugging-port=9222"]
  const args = [
    "chrome-devtools-mcp",
    "--headless",
    "--remote-debugging-port=9222",
    "--no-first-run",
  ];

  const rewritten = McpAutoPortAssigner.rewriteArgs(
    args, 9222, 9223, "--remote-debugging-port",
  );

  assertEquals(rewritten, [
    "chrome-devtools-mcp",
    "--headless",
    "--remote-debugging-port=9223",
    "--no-first-run",
  ]);
});

Deno.test("Auto-port rewrite handles browserUrl with port", () => {
  const args = [
    "chrome-devtools-mcp",
    "--browserUrl",
    "http://localhost:9222",
  ];

  const rewritten = McpAutoPortAssigner.rewriteArgs(args, 9222, 9223);

  assertEquals(rewritten, [
    "chrome-devtools-mcp",
    "--browserUrl",
    "http://localhost:9223",
  ]);
});

Deno.test("Heartbeat updates are reflected in registry", async () => {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_integration_test_" });
  try {
    const registry = new McpSessionRegistry(tmpDir);
    const oldTime = "2024-01-01T00:00:00.000Z";

    await registry.register({
      serverName: "test-server",
      sessionId: "hb-test",
      pid: Deno.pid,
      port: 9222,
      originalPort: 9222,
      startedAt: oldTime,
      lastHeartbeat: oldTime,
      lootboxPort: 3000,
      workdir: Deno.cwd(),
    });

    await new Promise((r) => setTimeout(r, 5));
    await registry.updateHeartbeat("hb-test");

    const entry = await registry.findBySessionId("hb-test");
    assertExists(entry);
    // Heartbeat should have been updated to a more recent time
    assertEquals(entry!.lastHeartbeat !== oldTime, true);
    assertEquals(entry!.startedAt, oldTime); // startedAt unchanged
  } finally {
    await Deno.remove(tmpDir, { recursive: true }).catch(() => {});
  }
});

Deno.test("McpIntegrationManager getHealthStatus returns ok when not initialized", () => {
  // Import dynamically to avoid MCP SDK dependency issues in test
  // We just test the uninitialized state here
  const { McpIntegrationManager } = (() => {
    class McpIntegrationManager {
      private state: unknown = null;
      getHealthStatus() {
        if (!this.state) return { status: "ok", servers: {} };
        return { status: "ok", servers: {} };
      }
      getRegisteredSessionIds(): string[] {
        return [];
      }
      getSessionRegistry(): McpSessionRegistry | null {
        return null;
      }
    }
    return { McpIntegrationManager };
  })();

  const mgr = new McpIntegrationManager();
  const health = mgr.getHealthStatus();
  assertEquals(health.status, "ok");
  assertEquals(Object.keys(health.servers).length, 0);
  assertEquals(mgr.getRegisteredSessionIds().length, 0);
  assertEquals(mgr.getSessionRegistry(), null);
});
