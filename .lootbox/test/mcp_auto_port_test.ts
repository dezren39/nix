/**
 * Unit tests for McpAutoPortAssigner.
 *
 * Tests cover:
 *   - Preferred port available → no reassignment
 *   - Preferred port in registry → scans range
 *   - Preferred port bound by OS → scans range
 *   - All ports exhausted → throws
 *   - Custom port range override
 *   - rewriteArgs: --flag=PORT, --flag PORT, bare PORT, URL-embedded PORT
 *   - isTcpPortAvailable: basic probe
 */

import {
  assertEquals,
  assertRejects,
} from "jsr:@std/assert";

import {
  McpAutoPortAssigner,
  isTcpPortAvailable,
} from "../src/lib/external-mcps/mcp_auto_port.ts";

import {
  McpSessionRegistry,
  type McpSessionEntry,
} from "../src/lib/external-mcps/mcp_session_registry.ts";

// ── Helpers ──────────────────────────────────────────────────────────

async function makeTempRegistry(): Promise<{
  registry: McpSessionRegistry;
  tmpDir: string;
}> {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_autoport_test_" });
  const registry = new McpSessionRegistry(tmpDir);
  return { registry, tmpDir };
}

function makeEntry(
  overrides: Partial<McpSessionEntry> = {},
): McpSessionEntry {
  return {
    serverName: overrides.serverName ?? "test-server",
    sessionId: overrides.sessionId ??
      McpSessionRegistry.generateSessionId("test-server"),
    pid: overrides.pid ?? Deno.pid,
    port: overrides.port ?? 9222,
    originalPort: overrides.originalPort ?? 9222,
    startedAt: overrides.startedAt ?? new Date().toISOString(),
    lastHeartbeat: overrides.lastHeartbeat ?? new Date().toISOString(),
    lootboxPort: overrides.lootboxPort ?? 3000,
    workdir: overrides.workdir ?? Deno.cwd(),
  };
}

async function cleanup(tmpDir: string): Promise<void> {
  try {
    await Deno.remove(tmpDir, { recursive: true });
  } catch { /* ignore */ }
}

// ── Tests: assignPort ────────────────────────────────────────────────

Deno.test("McpAutoPortAssigner: preferred port available → no reassignment", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    // Use a high port that's almost certainly free
    const assigner = new McpAutoPortAssigner(registry, [19222, 19299]);
    const result = await assigner.assignPort("chrome-devtools", 19222);
    assertEquals(result.port, 19222);
    assertEquals(result.wasReassigned, false);
    assertEquals(result.reason.includes("available"), true);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpAutoPortAssigner: preferred port in registry → scans range", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    // Register the preferred port as already claimed
    await registry.register(makeEntry({
      sessionId: "existing",
      serverName: "chrome-devtools",
      port: 19222,
    }));

    const assigner = new McpAutoPortAssigner(registry, [19222, 19225]);
    const result = await assigner.assignPort("chrome-devtools", 19222);
    assertEquals(result.wasReassigned, true);
    assertEquals(result.port >= 19223, true);
    assertEquals(result.port <= 19225, true);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpAutoPortAssigner: preferred port bound by OS → scans range", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    // Bind a port to simulate it being in use
    const listener = Deno.listen({ port: 19250, transport: "tcp" });
    try {
      const assigner = new McpAutoPortAssigner(registry, [19250, 19255]);
      const result = await assigner.assignPort("chrome-devtools", 19250);
      assertEquals(result.wasReassigned, true);
      assertEquals(result.port >= 19251, true);
      assertEquals(result.port <= 19255, true);
    } finally {
      listener.close();
    }
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpAutoPortAssigner: all ports exhausted → throws", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    // Register all ports in a tiny range
    for (let port = 19260; port <= 19262; port++) {
      await registry.register(makeEntry({
        sessionId: `s-${port}`,
        serverName: "chrome-devtools",
        port,
      }));
    }

    const assigner = new McpAutoPortAssigner(registry, [19260, 19262]);
    await assertRejects(
      () => assigner.assignPort("chrome-devtools", 19260),
      Error,
      "No available port",
    );
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpAutoPortAssigner: custom per-server range override", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    // Default range is [19300,19310] but we override to [19400,19405]
    const assigner = new McpAutoPortAssigner(registry, [19300, 19310]);
    const result = await assigner.assignPort("chrome-devtools", 19400, [19400, 19405]);
    assertEquals(result.port, 19400);
    assertEquals(result.wasReassigned, false);
  } finally {
    await cleanup(tmpDir);
  }
});

// ── Tests: rewriteArgs ──────────────────────────────────────────────

Deno.test("McpAutoPortAssigner.rewriteArgs: same port → no change", () => {
  const args = ["--remote-debugging-port=9222", "--headless"];
  const result = McpAutoPortAssigner.rewriteArgs(args, 9222, 9222);
  assertEquals(result, ["--remote-debugging-port=9222", "--headless"]);
});

Deno.test("McpAutoPortAssigner.rewriteArgs: --flag=PORT pattern", () => {
  const args = ["--headless", "--remote-debugging-port=9222", "--no-first-run"];
  const result = McpAutoPortAssigner.rewriteArgs(
    args, 9222, 9223, "--remote-debugging-port",
  );
  assertEquals(result, ["--headless", "--remote-debugging-port=9223", "--no-first-run"]);
});

Deno.test("McpAutoPortAssigner.rewriteArgs: --flag PORT (space-separated)", () => {
  const args = ["--headless", "--remote-debugging-port", "9222", "--no-first-run"];
  const result = McpAutoPortAssigner.rewriteArgs(
    args, 9222, 9223, "--remote-debugging-port",
  );
  assertEquals(result, ["--headless", "--remote-debugging-port", "9223", "--no-first-run"]);
});

Deno.test("McpAutoPortAssigner.rewriteArgs: bare port number", () => {
  const args = ["--headless", "9222"];
  const result = McpAutoPortAssigner.rewriteArgs(args, 9222, 9223);
  assertEquals(result, ["--headless", "9223"]);
});

Deno.test("McpAutoPortAssigner.rewriteArgs: URL-embedded port", () => {
  const args = ["--browserUrl", "http://localhost:9222"];
  const result = McpAutoPortAssigner.rewriteArgs(args, 9222, 9223);
  assertEquals(result, ["--browserUrl", "http://localhost:9223"]);
});

Deno.test("McpAutoPortAssigner.rewriteArgs: no match → args unchanged", () => {
  const args = ["--headless", "--no-first-run"];
  const result = McpAutoPortAssigner.rewriteArgs(args, 9222, 9223);
  assertEquals(result, ["--headless", "--no-first-run"]);
});

// ── Tests: isTcpPortAvailable ───────────────────────────────────────

Deno.test("isTcpPortAvailable: free port returns true", async () => {
  // Use a high port unlikely to be in use
  const available = await isTcpPortAvailable(19270);
  assertEquals(available, true);
});

Deno.test("isTcpPortAvailable: bound port returns false", async () => {
  const listener = Deno.listen({ port: 19271, transport: "tcp" });
  try {
    const available = await isTcpPortAvailable(19271);
    assertEquals(available, false);
  } finally {
    listener.close();
  }
});
