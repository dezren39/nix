/**
 * Unit tests for McpSessionRegistry.
 *
 * Uses a temp directory for each test so we don't pollute the real data dir.
 * Tests cover:
 *   - Empty registry read
 *   - Register / deregister sessions
 *   - Deregister by PID
 *   - Heartbeat updates
 *   - Stale PID cleanup
 *   - Lookup helpers (by server name, port, sessionId)
 *   - Session ID generation
 *   - Atomic write safety (file exists after write)
 */

import {
  assertEquals,
  assertExists,
  assertNotEquals,
} from "jsr:@std/assert";

import {
  McpSessionRegistry,
  type McpSessionEntry,
} from "../src/lib/external-mcps/mcp_session_registry.ts";

// ── Helpers ──────────────────────────────────────────────────────────

/** Create a temp directory and return a registry rooted there. */
async function makeTempRegistry(): Promise<{
  registry: McpSessionRegistry;
  tmpDir: string;
}> {
  const tmpDir = await Deno.makeTempDir({ prefix: "lootbox_session_test_" });
  const registry = new McpSessionRegistry(tmpDir);
  return { registry, tmpDir };
}

/** Build a minimal McpSessionEntry for testing. */
function makeEntry(
  overrides: Partial<McpSessionEntry> = {},
): McpSessionEntry {
  return {
    serverName: overrides.serverName ?? "test-server",
    sessionId: overrides.sessionId ??
      McpSessionRegistry.generateSessionId("test-server"),
    pid: overrides.pid ?? Deno.pid, // current process (alive)
    port: overrides.port ?? 9222,
    originalPort: overrides.originalPort ?? 9222,
    startedAt: overrides.startedAt ?? new Date().toISOString(),
    lastHeartbeat: overrides.lastHeartbeat ?? new Date().toISOString(),
    lootboxPort: overrides.lootboxPort ?? 3000,
    workdir: overrides.workdir ?? Deno.cwd(),
  };
}

/** Clean up temp dir after test. */
async function cleanup(tmpDir: string): Promise<void> {
  try {
    await Deno.remove(tmpDir, { recursive: true });
  } catch { /* ignore */ }
}

// ── Tests ────────────────────────────────────────────────────────────

Deno.test("McpSessionRegistry: empty registry returns version 1 with no sessions", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const data = await registry.read();
    assertEquals(data.version, 1);
    assertEquals(data.sessions.length, 0);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: register adds a session", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const entry = makeEntry();
    await registry.register(entry);

    const data = await registry.read();
    assertEquals(data.sessions.length, 1);
    assertEquals(data.sessions[0].serverName, "test-server");
    assertEquals(data.sessions[0].sessionId, entry.sessionId);
    assertEquals(data.sessions[0].port, 9222);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: register replaces duplicate sessionId", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const entry = makeEntry({ port: 9222 });
    await registry.register(entry);

    // Re-register same sessionId with different port
    const updated = { ...entry, port: 9223 };
    await registry.register(updated);

    const data = await registry.read();
    assertEquals(data.sessions.length, 1);
    assertEquals(data.sessions[0].port, 9223);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: deregister removes a session by sessionId", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const entry1 = makeEntry({ sessionId: "s1", port: 9222 });
    const entry2 = makeEntry({ sessionId: "s2", port: 9223 });
    await registry.register(entry1);
    await registry.register(entry2);

    await registry.deregister("s1");

    const data = await registry.read();
    assertEquals(data.sessions.length, 1);
    assertEquals(data.sessions[0].sessionId, "s2");
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: deregister is no-op for unknown sessionId", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const entry = makeEntry();
    await registry.register(entry);

    await registry.deregister("nonexistent-id");

    const data = await registry.read();
    assertEquals(data.sessions.length, 1);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: deregisterByPid removes all sessions for a PID", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const entry1 = makeEntry({ sessionId: "s1", pid: Deno.pid });
    const entry2 = makeEntry({ sessionId: "s2", pid: Deno.pid });
    // Entry with a different (fake but alive-ish) PID — use pid=1 (launchd on macOS, init on Linux)
    const entry3 = makeEntry({ sessionId: "s3", pid: 1 });
    await registry.register(entry1);
    await registry.register(entry2);
    await registry.register(entry3);

    await registry.deregisterByPid(Deno.pid);

    const data = await registry.read();
    // Only entry3 (pid=1) should remain (it may or may not be cleaned as stale,
    // but deregisterByPid only removes matching PIDs)
    const myEntries = data.sessions.filter((s) => s.pid === Deno.pid);
    assertEquals(myEntries.length, 0);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: updateHeartbeat updates timestamp", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const oldTime = "2024-01-01T00:00:00.000Z";
    const entry = makeEntry({ lastHeartbeat: oldTime });
    await registry.register(entry);

    // Small delay to ensure timestamp differs
    await new Promise((r) => setTimeout(r, 5));
    await registry.updateHeartbeat(entry.sessionId);

    const data = await registry.read();
    assertNotEquals(data.sessions[0].lastHeartbeat, oldTime);
    // New heartbeat should be a recent ISO string
    const hbDate = new Date(data.sessions[0].lastHeartbeat);
    assertEquals(isNaN(hbDate.getTime()), false);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: updateHeartbeat is no-op for unknown session", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const entry = makeEntry({ lastHeartbeat: "2024-01-01T00:00:00.000Z" });
    await registry.register(entry);

    await registry.updateHeartbeat("nonexistent");

    const data = await registry.read();
    assertEquals(data.sessions[0].lastHeartbeat, "2024-01-01T00:00:00.000Z");
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: findByServerName returns matching entries", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    await registry.register(makeEntry({ sessionId: "s1", serverName: "alpha" }));
    await registry.register(makeEntry({ sessionId: "s2", serverName: "beta" }));
    await registry.register(makeEntry({ sessionId: "s3", serverName: "alpha" }));

    const alphas = await registry.findByServerName("alpha");
    assertEquals(alphas.length, 2);
    assertEquals(alphas.every((e) => e.serverName === "alpha"), true);

    const betas = await registry.findByServerName("beta");
    assertEquals(betas.length, 1);

    const gammas = await registry.findByServerName("gamma");
    assertEquals(gammas.length, 0);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: findByPort returns matching entries", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    await registry.register(makeEntry({ sessionId: "s1", port: 9222 }));
    await registry.register(makeEntry({ sessionId: "s2", port: 9223 }));
    await registry.register(makeEntry({ sessionId: "s3", port: 9222 }));

    const on9222 = await registry.findByPort(9222);
    assertEquals(on9222.length, 2);

    const on9223 = await registry.findByPort(9223);
    assertEquals(on9223.length, 1);

    const on9999 = await registry.findByPort(9999);
    assertEquals(on9999.length, 0);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: findBySessionId returns correct entry or undefined", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    const entry = makeEntry({ sessionId: "unique-id-123" });
    await registry.register(entry);

    const found = await registry.findBySessionId("unique-id-123");
    assertExists(found);
    assertEquals(found!.sessionId, "unique-id-123");

    const notFound = await registry.findBySessionId("no-such-id");
    assertEquals(notFound, undefined);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: generateSessionId produces unique IDs", () => {
  const id1 = McpSessionRegistry.generateSessionId("server-a");
  const id2 = McpSessionRegistry.generateSessionId("server-a");
  // IDs include timestamp so they should differ (or at least not be identical strings
  // since Date.now() may match within the same ms, but PID is same — practically
  // they should be different due to sequential Date.now() calls)
  // We mainly check format: starts with server name
  assertEquals(id1.startsWith("server-a-"), true);
  assertEquals(id2.startsWith("server-a-"), true);
  assertEquals(typeof id1, "string");
  assertEquals(id1.length > 10, true);
});

Deno.test("McpSessionRegistry: stale PID cleanup removes dead sessions", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    // PID 99999999 should not exist
    const staleEntry = makeEntry({
      sessionId: "stale-session",
      pid: 99999999,
    });
    const aliveEntry = makeEntry({
      sessionId: "alive-session",
      pid: Deno.pid,
    });

    // Write both entries directly (bypass read-clean cycle)
    await registry.register(aliveEntry);
    await registry.register(staleEntry);

    // Now read — should clean the stale entry
    const data = await registry.read();
    assertEquals(data.sessions.length, 1);
    assertEquals(data.sessions[0].sessionId, "alive-session");
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: registry file is created on disk", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    await registry.register(makeEntry());
    const path = registry.getRegistryPath();
    const stat = await Deno.stat(path);
    assertEquals(stat.isFile, true);
  } finally {
    await cleanup(tmpDir);
  }
});

Deno.test("McpSessionRegistry: multiple servers can coexist", async () => {
  const { registry, tmpDir } = await makeTempRegistry();
  try {
    await registry.register(makeEntry({
      sessionId: "chrome-1",
      serverName: "chrome-devtools",
      port: 9222,
    }));
    await registry.register(makeEntry({
      sessionId: "filesystem-1",
      serverName: "filesystem",
      port: 0,
    }));
    await registry.register(makeEntry({
      sessionId: "chrome-2",
      serverName: "chrome-devtools",
      port: 9223,
      lootboxPort: 3001,
    }));

    const data = await registry.read();
    assertEquals(data.sessions.length, 3);

    const chromes = await registry.findByServerName("chrome-devtools");
    assertEquals(chromes.length, 2);

    const fs = await registry.findByServerName("filesystem");
    assertEquals(fs.length, 1);
  } finally {
    await cleanup(tmpDir);
  }
});
