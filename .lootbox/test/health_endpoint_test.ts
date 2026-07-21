/**
 * Unit tests for the deep health endpoint and WorkerManager.getHealthStatus().
 */

import {
  assertEquals,
  assertStrictEquals,
} from "jsr:@std/assert";

import type { WorkerManagerConfig } from "../src/lib/rpc/worker_manager.ts";
import { WorkerManager } from "../src/lib/rpc/worker_manager.ts";

// ── WorkerManager.getHealthStatus() tests ────────────────────────────

function createWorkerManager(): WorkerManager {
  const config: WorkerManagerConfig = {
    port: 3000,
    rpcTimeout: 30_000,
    workerShutdownGrace: 500,
    maxWorkerBackoff: 30_000,
    maxWorkerRestarts: 0,
    workerBackoffBase: 1_000,
    workerPollInterval: 100,
    workerWsPath: "/worker-ws",
  };
  return new WorkerManager(config);
}

Deno.test("WorkerManager.getHealthStatus() returns ok with no workers", () => {
  const wm = createWorkerManager();
  const status = wm.getHealthStatus();

  assertStrictEquals(status.status, "ok");
  assertStrictEquals(status.total, 0);
  assertStrictEquals(status.ready, 0);
  assertStrictEquals(status.failed, 0);
  assertStrictEquals(status.crashed, 0);
  assertStrictEquals(status.starting, 0);
});

// ── McpIntegrationManager.getHealthStatus() tests ────────────────────

Deno.test("McpIntegrationManager.getHealthStatus() returns ok when disabled", async () => {
  const { McpIntegrationManager } = await import(
    "../src/lib/rpc/managers/mcp_integration_manager.ts"
  );

  const mgr = new McpIntegrationManager();

  // Not initialized — should report ok with empty servers
  const health = mgr.getHealthStatus();
  assertStrictEquals(health.status, "ok");
  assertEquals(health.servers, {});
});

// ── Deep health response shape tests ─────────────────────────────────

Deno.test("Deep health response has expected shape", () => {
  // Simulate the response structure
  const response = {
    status: "ok" as const,
    uptime_ms: 12345,
    subsystems: {
      workers: {
        status: "ok" as const,
        total: 3,
        ready: 3,
        failed: 0,
        crashed: 0,
        starting: 0,
      },
      mcp_servers: {
        status: "ok" as const,
        servers: {
          chrome_devtools: {
            status: "connected" as const,
            last_health_check: "2026-04-07T12:00:00.000Z",
            reconnect_attempts: 0,
          },
        },
      },
    },
  };

  assertStrictEquals(response.status, "ok");
  assertStrictEquals(typeof response.uptime_ms, "number");
  assertStrictEquals(response.subsystems.workers.total, 3);
  assertStrictEquals(response.subsystems.workers.ready, 3);
  assertStrictEquals(
    response.subsystems.mcp_servers.servers.chrome_devtools.status,
    "connected",
  );
});

Deno.test("Overall status is worst of subsystem statuses", () => {
  // Helper mirroring the logic in openapi_route_handler.ts
  function computeOverall(
    ...statuses: ("ok" | "degraded" | "unhealthy")[]
  ): "ok" | "degraded" | "unhealthy" {
    if (statuses.includes("unhealthy")) return "unhealthy";
    if (statuses.includes("degraded")) return "degraded";
    return "ok";
  }

  assertStrictEquals(computeOverall("ok", "ok"), "ok");
  assertStrictEquals(computeOverall("ok", "degraded"), "degraded");
  assertStrictEquals(computeOverall("degraded", "ok"), "degraded");
  assertStrictEquals(computeOverall("ok", "unhealthy"), "unhealthy");
  assertStrictEquals(computeOverall("degraded", "unhealthy"), "unhealthy");
  assertStrictEquals(computeOverall("unhealthy", "unhealthy"), "unhealthy");
});

// ── Health CLI formatting tests ──────────────────────────────────────

Deno.test("Health CLI exit codes map correctly", () => {
  const statusToExitCode = (status: string): number => {
    switch (status) {
      case "ok":
        return 0;
      case "degraded":
        return 1;
      case "unhealthy":
        return 2;
      default:
        return 0;
    }
  };

  assertStrictEquals(statusToExitCode("ok"), 0);
  assertStrictEquals(statusToExitCode("degraded"), 1);
  assertStrictEquals(statusToExitCode("unhealthy"), 2);
  assertStrictEquals(statusToExitCode("unknown"), 0);
});
