// Worker Manager - Manages lifecycle of RPC worker processes

import type { RpcFile } from "./load_rpc_files.ts";

// ── Inline worker fallback ───────────────────────────────────────────
// Used only when the source rpc_worker.ts cannot be located at runtime
// (e.g. inside a `deno compile`d binary).  Keep in sync with rpc_worker.ts.
const RPC_WORKER_CODE = `
// RPC Worker - Long-running process that executes RPC functions
// One worker per RPC file, communicates with main server via WebSocket

interface CallMessage {
  type: "call";
  id: string;
  functionName: string;
  args: unknown;
}

interface ShutdownMessage {
  type: "shutdown";
}

type WorkerMessage = CallMessage | ShutdownMessage;

async function main() {
  // Parse CLI arguments
  const rpcFilePath = Deno.args[0];
  const workerWsUrl = Deno.args[1];
  const namespace = Deno.args[2];
  const rpcTimeoutMs = parseInt(Deno.args[3] || "30000", 10) || 30000;

  if (!rpcFilePath || !workerWsUrl || !namespace) {
    console.error("Usage: rpc_worker.ts <rpcFilePath> <workerWsUrl> <namespace> [rpcTimeoutMs]");
    Deno.exit(1);
  }

  // Import all functions from RPC file
  const functions = await import(rpcFilePath);

  // Connect to main server
  const ws = new WebSocket(workerWsUrl);

  ws.onopen = () => {
    // Identify ourselves
    ws.send(JSON.stringify({
      type: "identify",
      workerId: namespace,
    }));

    // Signal ready
    ws.send(JSON.stringify({
      type: "ready",
      workerId: namespace,
    }));
  };

  ws.onmessage = async (event) => {
    try {
      const msg: WorkerMessage = JSON.parse(
        typeof event.data === "string"
          ? event.data
          : new TextDecoder().decode(new Uint8Array(event.data as ArrayBuffer))
      );

      if (msg.type === "call") {
        const { id, functionName, args } = msg;

        try {
          // Get the function
          const fn = functions[functionName];

          if (typeof fn !== "function") {
            throw new Error(\`Function '\${functionName}' not found or not exported\`);
          }

          // Execute with timeout
          const timeoutPromise = new Promise((_, reject) => {
            setTimeout(() => reject(new Error(\`Function execution timeout (\${rpcTimeoutMs / 1000}s)\`)), rpcTimeoutMs);
          });

          const result = await Promise.race([
            fn(args),
            timeoutPromise,
          ]);

          // Send result back
          ws.send(JSON.stringify({
            type: "result",
            id,
            data: result,
          }));
        } catch (error) {
          // Send error back
          ws.send(JSON.stringify({
            type: "error",
            id,
            error: error instanceof Error ? error.message : String(error),
          }));
        }
      } else if (msg.type === "shutdown") {
        console.error(\`[Worker \${namespace}] Received shutdown signal\`);
        ws.close();
        Deno.exit(0);
      }
    } catch (error) {
      console.error(\`[Worker \${namespace}] Error handling message:\`, error);
    }
  };

  ws.onerror = (error) => {
    // Suppress "Unexpected EOF" errors on shutdown
    const errorEvent = error as ErrorEvent;
    if (!errorEvent.message?.includes("Unexpected EOF")) {
      console.error(\`[Worker \${namespace}] WebSocket error:\`, error);
    }
  };

  ws.onclose = () => {
    // Silent exit on close
    Deno.exit(0);
  };

  // Handle uncaught errors
  globalThis.addEventListener("error", (event) => {
    console.error(\`[Worker \${namespace}] Uncaught error:\`, event.error);

    try {
      ws.send(JSON.stringify({
        type: "crash",
        error: event.error?.message || String(event.error),
      }));
    } catch {
      // Best effort
    }

    ws.close();
    Deno.exit(1);
  });

  // Handle unhandled promise rejections
  globalThis.addEventListener("unhandledrejection", (event) => {
    console.error(\`[Worker \${namespace}] Unhandled rejection:\`, event.reason);

    try {
      ws.send(JSON.stringify({
        type: "crash",
        error: event.reason?.message || String(event.reason),
      }));
    } catch {
      // Best effort
    }

    ws.close();
    Deno.exit(1);
  });
}

main();
`;

interface WorkerState {
  process: Deno.ChildProcess;
  sendMessage?: (message: string) => void; // Callback to send messages to worker
  workerId: string;
  filePath: string;
  status: "starting" | "ready" | "crashed" | "failed";
  pendingCalls: Map<
    string,
    {
      resolve: (value: unknown) => void;
      reject: (error: Error) => void;
      timeoutId: number;
    }
  >;
  restartCount: number;
  lastRestart: number;
  everReady: boolean; // Track if worker ever successfully started
}

interface IdentifyMessage {
  type: "identify";
  workerId: string;
}

interface ReadyMessage {
  type: "ready";
  workerId: string;
}

interface ResultMessage {
  type: "result";
  id: string;
  data: unknown;
}

interface ErrorMessage {
  type: "error";
  id: string;
  error: string;
}

interface CrashMessage {
  type: "crash";
  error: string;
}

type WorkerIncomingMessage =
  | IdentifyMessage
  | ReadyMessage
  | ResultMessage
  | ErrorMessage
  | CrashMessage;

export interface WorkerManagerConfig {
  port: number;
  rpcTimeout: number;
  workerShutdownGrace: number;
  maxWorkerBackoff: number;
  maxWorkerRestarts: number;
  workerBackoffBase: number;
  workerPollInterval: number;
  workerWsPath: string;
}

export class WorkerManager {
  private workers = new Map<string, WorkerState>();
  private port: number;
  private rpcTimeout: number;
  private workerShutdownGrace: number;
  private maxWorkerBackoff: number;
  private maxWorkerRestarts: number;
  private workerBackoffBase: number;
  private workerPollInterval: number;
  private workerWsPath: string;

  constructor(config: WorkerManagerConfig) {
    this.port = config.port;
    this.rpcTimeout = config.rpcTimeout;
    this.workerShutdownGrace = config.workerShutdownGrace;
    this.maxWorkerBackoff = config.maxWorkerBackoff;
    this.maxWorkerRestarts = config.maxWorkerRestarts;
    this.workerBackoffBase = config.workerBackoffBase;
    this.workerPollInterval = config.workerPollInterval;
    this.workerWsPath = config.workerWsPath;
  }

  /** Cached path to the worker script (resolved once per process). */
  private _workerScriptPath: string | null = null;

  /**
   * Resolve the path to the worker script.
   * Prefers the canonical rpc_worker.ts; falls back to writing the
   * embedded RPC_WORKER_CODE to a temp file for compiled-binary mode.
   */
  private async resolveWorkerScript(): Promise<string> {
    if (this._workerScriptPath) return this._workerScriptPath;

    // Try to find rpc_worker.ts relative to this module
    const candidates = [
      new URL("./rpc_worker.ts", import.meta.url),
    ];
    for (const candidate of candidates) {
      try {
        // file:// URLs work with Deno.stat
        const path = candidate.protocol === "file:"
          ? candidate.pathname
          : candidate.href;
        await Deno.stat(path);
        this._workerScriptPath = path;
        return path;
      } catch {
        // Not found, try next
      }
    }

    // Fallback: write embedded code to a temp file
    const tempFile = await Deno.makeTempFile({
      prefix: "lootbox_worker_",
      suffix: ".ts",
    });
    await Deno.writeTextFile(tempFile, RPC_WORKER_CODE);
    this._workerScriptPath = tempFile;
    return tempFile;
  }

  /**
   * Start a worker process for an RPC file
   */
  async startWorker(file: RpcFile): Promise<void> {
    const workerId = file.name;

    // Prefer the canonical rpc_worker.ts source file; fall back to the
    // embedded string when the file can't be found (compiled binary).
    const workerScript = await this.resolveWorkerScript();

    // Spawn worker process
    const workerWsUrl = `ws://localhost:${this.port}${this.workerWsPath}`;
    const command = new Deno.Command("deno", {
      args: [
        "run",
        "--allow-all",
        workerScript,
        file.path,
        workerWsUrl,
        workerId,
        String(this.rpcTimeout),
      ],
      stdout: "piped",
      stderr: "inherit", // Show worker logs in main process
    });

    const process = command.spawn();

    // Create worker state
    const worker: WorkerState = {
      process,
      workerId,
      filePath: file.path,
      status: "starting",
      pendingCalls: new Map(),
      restartCount: 0,
      lastRestart: Date.now(),
      everReady: false,
    };

    this.workers.set(workerId, worker);

    // Monitor process exit
    process.status.then((status) => {
      if (!status.success) {
        console.error(
          `[WorkerManager] Worker ${workerId} exited with code ${status.code}`
        );
        this.handleWorkerCrash(workerId);
      }
    });
  }

  /**
   * Register a send callback for a worker
   */
  registerWorkerSender(
    workerId: string,
    sendMessage: (message: string) => void
  ): void {
    const worker = this.workers.get(workerId);
    if (worker) {
      worker.sendMessage = sendMessage;
    }
  }

  /**
   * Handle incoming message from worker
   */
  handleMessage(data: string): void {
    try {
      const msg: WorkerIncomingMessage = JSON.parse(data);

      if (msg.type === "identify") {
        const workerId = msg.workerId;
        const worker = this.workers.get(workerId);

        if (!worker) {
          console.error(
            `[WorkerManager] Unknown worker identified: ${workerId}`
          );
          return;
        }

      } else if (msg.type === "ready") {
        const workerId = msg.workerId;
        const worker = this.workers.get(workerId);

        if (worker) {
          worker.status = "ready";
          worker.everReady = true;
        }
      } else if (msg.type === "result") {
        // Find worker by searching for the pending call
        for (const worker of this.workers.values()) {
          const pending = worker.pendingCalls.get(msg.id);
          if (pending) {
            clearTimeout(pending.timeoutId);
            pending.resolve(msg.data);
            worker.pendingCalls.delete(msg.id);
            return;
          }
        }
      } else if (msg.type === "error") {
        // Find worker by searching for the pending call
        for (const worker of this.workers.values()) {
          const pending = worker.pendingCalls.get(msg.id);
          if (pending) {
            clearTimeout(pending.timeoutId);
            pending.reject(new Error(msg.error));
            worker.pendingCalls.delete(msg.id);
            return;
          }
        }
      } else if (msg.type === "crash") {
        // Find worker - we need to track which worker sent this
        // For now, log and let process monitoring handle it
        console.error(`[WorkerManager] Worker crashed: ${msg.error}`);
      }
    } catch (error) {
      console.error(`[WorkerManager] Error handling message:`, error);
    }
  }

  /**
   * Handle worker disconnection
   */
  handleDisconnect(workerId: string): void {
    const worker = this.workers.get(workerId);
    if (worker) {
      worker.sendMessage = undefined;
    }
  }

  /**
   * Call a function on a worker
   */
  async callFunction(
    namespace: string,
    functionName: string,
    args: unknown
  ): Promise<unknown> {
    const worker = this.workers.get(namespace);

    if (!worker) {
      throw new Error(`Worker for namespace '${namespace}' not found`);
    }

    if (worker.status === "failed") {
      throw new Error(
        `Worker for namespace '${namespace}' failed to start. Check the tool file for errors.`
      );
    }

    if (worker.status !== "ready") {
      throw new Error(
        `Worker for namespace '${namespace}' is not ready (status: ${worker.status})`
      );
    }

    if (!worker.sendMessage) {
      throw new Error(
        `Worker for namespace '${namespace}' has no send callback`
      );
    }

    // Generate unique call ID
    const callId = `call_${Date.now()}_${Math.random()
      .toString(36)
      .substring(2)}`;

    // Create promise for response
    const resultPromise = new Promise<unknown>((resolve, reject) => {
      // Timeout for RPC call
      const timeoutId = setTimeout(() => {
        worker.pendingCalls.delete(callId);
        reject(
          new Error(
            `RPC call timeout: ${namespace}.${functionName} (${this.rpcTimeout / 1000} seconds)`
          )
        );
      }, this.rpcTimeout);

      worker.pendingCalls.set(callId, { resolve, reject, timeoutId });
    });

    // Send call message
    worker.sendMessage(
      JSON.stringify({
        type: "call",
        id: callId,
        functionName,
        args,
      })
    );

    return resultPromise;
  }

  /**
   * Handle worker crash
   */
  private handleWorkerCrash(workerId: string): void {
    const worker = this.workers.get(workerId);
    if (!worker) return;

    // Reject all pending calls
    for (const [callId, pending] of worker.pendingCalls) {
      clearTimeout(pending.timeoutId);
      pending.reject(new Error(`Worker ${workerId} crashed`));
    }
    worker.pendingCalls.clear();

    // If worker never successfully started, mark as permanently failed
    if (!worker.everReady) {
      worker.status = "failed";
      console.error(
        `[WorkerManager] Worker ${workerId} failed to start - not retrying. Check the tool file for errors.`
      );
      return;
    }

    // Worker was previously healthy, attempt restart with backoff
    worker.status = "crashed";

    // Circuit breaker: stop restarting after max attempts
    if (this.maxWorkerRestarts > 0 && worker.restartCount >= this.maxWorkerRestarts) {
      worker.status = "failed";
      console.error(
        `[WorkerManager] Worker ${workerId} exceeded max restarts (${this.maxWorkerRestarts}) - not retrying.`
      );
      return;
    }

    const backoffMs = Math.min(this.workerBackoffBase * Math.pow(2, worker.restartCount), this.maxWorkerBackoff);
    worker.restartCount++;

    console.error(
      `[WorkerManager] Will restart worker ${workerId} in ${backoffMs}ms (attempt ${worker.restartCount})`
    );

    // Schedule restart
    setTimeout(() => {
      const file: RpcFile = {
        name: workerId,
        path: worker.filePath,
      };
      this.startWorker(file);
    }, backoffMs);
  }

  /**
   * Restart a worker (for hot reload)
   */
  async restartWorker(workerId: string, file: RpcFile): Promise<void> {
    const worker = this.workers.get(workerId);

    if (worker?.sendMessage) {
      console.error(
        `[WorkerManager] Shutting down worker ${workerId} for reload`
      );

      // Send shutdown signal
      try {
        worker.sendMessage(JSON.stringify({ type: "shutdown" }));
      } catch {
        // Best effort
      }

      // Wait a bit for graceful shutdown
      await new Promise((resolve) => setTimeout(resolve, this.workerShutdownGrace));

      // Force kill if still alive
      try {
        worker.process.kill("SIGKILL");
      } catch {
        // Already dead
      }

      // Clean up
      worker.sendMessage = undefined;
      this.workers.delete(workerId);
    }

    // Start new worker
    await this.startWorker(file);
  }

  /**
   * Wait for all workers to be ready
   */
  async waitForReady(timeoutMs: number): Promise<void> {
    const startTime = Date.now();

    while (Date.now() - startTime < timeoutMs) {
      const allSettled = Array.from(this.workers.values()).every(
        (w) => w.status === "ready" || w.status === "failed"
      );

      if (allSettled) {
        const ready = Array.from(this.workers.values()).filter(
          (w) => w.status === "ready"
        );
        const failed = Array.from(this.workers.values()).filter(
          (w) => w.status === "failed"
        );

        return;
      }

      await new Promise((resolve) => setTimeout(resolve, this.workerPollInterval));
    }

    const notReady = Array.from(this.workers.values()).filter(
      (w) => w.status !== "ready" && w.status !== "failed"
    );

    console.error(
      `[WorkerManager] Timeout waiting for workers. Not ready: ${notReady
        .map((w) => w.workerId)
        .join(", ")}`
    );
  }

  /**
   * Stop all workers
   */
  async stopAllWorkers(): Promise<void> {
    for (const worker of this.workers.values()) {
      if (worker.sendMessage) {
        try {
          worker.sendMessage(JSON.stringify({ type: "shutdown" }));
        } catch {
          // Best effort
        }
      }

      try {
        worker.process.kill("SIGTERM");
      } catch {
        // Already dead
      }
    }

    this.workers.clear();
  }

  /**
   * Get aggregate worker health status for the deep health endpoint.
   */
  getHealthStatus(): {
    status: "ok" | "degraded" | "unhealthy";
    total: number;
    ready: number;
    failed: number;
    crashed: number;
    starting: number;
  } {
    let ready = 0;
    let failed = 0;
    let crashed = 0;
    let starting = 0;

    for (const worker of this.workers.values()) {
      switch (worker.status) {
        case "ready":
          ready++;
          break;
        case "failed":
          failed++;
          break;
        case "crashed":
          crashed++;
          break;
        case "starting":
          starting++;
          break;
      }
    }

    const total = this.workers.size;

    let status: "ok" | "degraded" | "unhealthy";
    if (total === 0 || ready === total) {
      status = "ok";
    } else if (ready > 0) {
      status = "degraded";
    } else {
      status = "unhealthy";
    }

    return { status, total, ready, failed, crashed, starting };
  }
}
