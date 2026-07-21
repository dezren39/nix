/**
 * WebSocketRpcServer - Thin Orchestrator
 *
 * Composes and coordinates all manager classes to provide
 * a complete WebSocket RPC server.
 *
 * Responsibilities:
 * - Manager composition and initialization
 * - Lifecycle orchestration (start/stop)
 * - Wiring managers together with callbacks
 */

import { OpenAPIHono } from "@hono/zod-openapi";
import type { Hono } from "@hono/hono";
import { upgradeWebSocket } from "@hono/hono/deno";
import type { Spinner } from "@std/cli/unstable-spinner";
import { get_client, set_client } from "../client_cache.ts";
import {
  DEFAULT_WORKER_WS_PATH,
  DEFAULT_WS_PATH,
  DEFAULT_HEALTH_PATH,
  DEFAULT_OPENAPI_TITLE,
} from "../constants.ts";
import type { McpConfigFile } from "../external-mcps/mcp_config.ts";
import { WorkerManager, type WorkerManagerConfig } from "./worker_manager.ts";
import { RpcCacheManager } from "./managers/rpc_cache_manager.ts";
import { FileWatcherManager } from "./managers/file_watcher_manager.ts";
import { TypeGeneratorManager } from "./managers/type_generator_manager.ts";
import { McpIntegrationManager } from "./managers/mcp_integration_manager.ts";
import { MessageRouter } from "./managers/message_router.ts";
import { ConnectionManager } from "./managers/connection_manager.ts";
import { OpenApiRouteHandler } from "./managers/openapi_route_handler.ts";
import { setupUIRoutes } from "../ui_server.ts";
import { showBootup } from "../lootbox-cli/bootup.ts";

export class WebSocketRpcServer {
  private app = new OpenAPIHono();

  // Manager composition
  private rpcCacheManager: RpcCacheManager;
  private fileWatcherManager: FileWatcherManager;
  private typeGeneratorManager: TypeGeneratorManager;
  private mcpIntegrationManager: McpIntegrationManager;
  private connectionManager: ConnectionManager;
  private messageRouter!: MessageRouter; // Initialized in start()
  private workerManager: WorkerManager | null = null;

  private currentPort = 0;
  private workerWsPath = DEFAULT_WORKER_WS_PATH;
  private wsPath = DEFAULT_WS_PATH;
  private healthPath = DEFAULT_HEALTH_PATH;
  private openApiTitle = DEFAULT_OPENAPI_TITLE;
  private clientConfigValues: import("./managers/connection_manager.ts").ClientConfigPayload | null = null;

  constructor() {
    // Initialize independent managers
    this.rpcCacheManager = new RpcCacheManager();
    this.fileWatcherManager = new FileWatcherManager();
    this.typeGeneratorManager = new TypeGeneratorManager(this.rpcCacheManager);
    this.mcpIntegrationManager = new McpIntegrationManager();
    this.connectionManager = new ConnectionManager();
  }

  /**
   * Wire managers together with event callbacks
   */
  private wireManagers(): void {
    // RPC cache refresh triggers:
    // 1. Type cache invalidation
    this.rpcCacheManager.onCacheRefreshed(() => {
      this.typeGeneratorManager.invalidateCache();
    });

    // 2. Client notifications
    this.rpcCacheManager.onCacheRefreshed((functions) => {
      this.connectionManager.broadcastToClients({
        type: "functions_updated",
        functions,
      });
    });

    // 3. Client code regeneration and caching
    this.rpcCacheManager.onCacheRefreshed(async () => {
      try {
        const schemas = this.mcpIntegrationManager.isEnabled()
          ? await this.mcpIntegrationManager.getSchemas()
          : undefined;
        const clientCode = await this.typeGeneratorManager.generateClientCode(
          this.currentPort,
          schemas,
          this.clientConfigValues?.client_timeout,
          this.clientConfigValues?.auto_disconnect_delay,
        );
        set_client(clientCode);
      } catch (err) {
        console.error("Failed to regenerate client code:", err);
      }
    });

    // 4. Worker restarts
    this.rpcCacheManager.onCacheRefreshed(async () => {
      if (this.workerManager) {
        const uniqueFiles = this.rpcCacheManager.getUniqueFiles();
        await Promise.all(
          Array.from(uniqueFiles.values()).map((file) =>
            this.workerManager!.restartWorker(file.name, file)
          )
        );
      }
    });
  }

  /**
   * Start the RPC server
   */
  async start(port: number, mcpConfig: McpConfigFile | null, spinner?: Spinner): Promise<void> {
    this.currentPort = port;

    // Get config early
    const { get_config } = await import("../get_config.ts");
    const config = await get_config();

    // Store client config values for WebSocket welcome messages
    this.clientConfigValues = {
      client_timeout: config.client_timeout,
      auto_disconnect_delay: config.auto_disconnect_delay,
      reconnect_delay: config.reconnect_delay,
      ws_path: config.ws_path,
      server_url: config.server_url,
    };

    // Phase 1 & 2: Load RPC cache and initialize MCP in parallel
    await Promise.all([
      this.rpcCacheManager.refreshCache(),
      mcpConfig ? this.mcpIntegrationManager.initialize(mcpConfig, config.mcp_client_name, {
        checkInterval: config.mcp_health_check_interval,
        maxReconnectAttempts: config.mcp_max_reconnect_attempts,
        reconnectBackoffBase: config.mcp_reconnect_backoff_base,
        maxReconnectBackoff: config.mcp_max_reconnect_backoff,
        checkTimeout: config.mcp_health_check_timeout,
      }, port, config.mcp_default_multi_client_strategy) : Promise.resolve(),
    ]);

    // Phase 2.5: Generate initial client code
    const schemas = this.mcpIntegrationManager.isEnabled()
      ? await this.mcpIntegrationManager.getSchemas()
      : undefined;
    const clientCode = await this.typeGeneratorManager.generateClientCode(
      port,
      schemas,
      config.timeout,
      config.auto_disconnect_delay,
    );
    set_client(clientCode);

    // Phase 3: Wire managers together
    this.wireManagers();

    // Phase 4: Setup message routing
    this.workerManager = new WorkerManager({
      port,
      rpcTimeout: config.rpc_timeout,
      workerShutdownGrace: config.worker_shutdown_grace,
      maxWorkerBackoff: config.max_worker_backoff,
      maxWorkerRestarts: config.max_worker_restarts,
      workerBackoffBase: config.worker_backoff_base,
      workerPollInterval: config.worker_poll_interval,
      workerWsPath: config.worker_ws_path,
    });
    this.messageRouter = new MessageRouter(
      this.workerManager,
      this.mcpIntegrationManager,
      config.rpc_timeout
    );

    // Phase 5: Setup HTTP routes with OpenAPI documentation
    this.workerWsPath = config.worker_ws_path;
    this.wsPath = config.ws_path;
    this.healthPath = config.health_path;
    this.openApiTitle = config.openapi_title;
    this.setupRoutes();

    // Phase 7: Start file watcher
    this.fileWatcherManager.startWatching(config.tools_dir, async () => {
      await this.rpcCacheManager.refreshCache();
    }, config.file_watch_debounce, config.tool_file_extension);

    // Phase 8: Start HTTP server
    // TODO: make hostname configurable (e.g. config.hostname ?? "localhost")
    Deno.serve({ port, hostname: "localhost", onListen: () => {} }, this.app.fetch);

    // Give server time to start
    await new Promise((resolve) => setTimeout(resolve, config.server_start_delay));

    // Phase 9: Initialize workers
    const uniqueFiles = this.rpcCacheManager.getUniqueFiles();
    await Promise.all(
      Array.from(uniqueFiles.values()).map((file) =>
        this.workerManager!.startWorker(file)
      )
    );

    // Wait for workers to be ready
    await this.workerManager.waitForReady(config.worker_ready_timeout);

    // Show bootup display
    showBootup({
      port,
      toolsDir: config.tools_dir,
      mcpServers: this.mcpIntegrationManager.getConnectedServers(),
      rpcFunctions: this.rpcCacheManager.getFunctionNames(),
      spinner,
    });
  }

  /**
   * Stop the RPC server
   */
  async stop(): Promise<void> {
    console.error("Stopping RPC server...");

    // Stop workers
    if (this.workerManager) {
      await this.workerManager.stopAllWorkers();
      this.workerManager = null;
    }

    // Close all client connections
    await this.connectionManager.closeAllClients();

    // Shutdown MCP
    await this.mcpIntegrationManager.shutdown();

    // Stop file watcher
    this.fileWatcherManager.stopWatching();

    console.error("RPC server stopped");
  }

  /**
   * Setup all HTTP and WebSocket routes
   */
  private setupRoutes(): void {
    // Setup OpenAPI-documented REST routes
    const openApiHandler = new OpenApiRouteHandler(
      this.app,
      this.rpcCacheManager,
      this.typeGeneratorManager,
      this.mcpIntegrationManager,
      get_client,
      this.currentPort,
      this.healthPath,
      this.openApiTitle,
      this.workerManager
    );
    openApiHandler.setupRoutes();

    // Setup UI routes
    setupUIRoutes(this.app);

    // Setup WebSocket routes (cannot be documented via OpenAPI)
    this.setupWebSocketRoutes();
  }

  /**
   * Setup WebSocket routes
   * Note: WebSocket routes cannot be documented via OpenAPI specification
   */
  private setupWebSocketRoutes(): void {
    // Cast to Hono for WebSocket routes (OpenAPIHono extends Hono but upgradeWebSocket has strict typing)
    const honoApp = this.app as unknown as Hono;

    honoApp.get(
      this.workerWsPath,
      upgradeWebSocket(() => {
        return this.connectionManager.createWorkerWebSocketHandler(
          this.workerManager!
        );
      })
    );

    honoApp.get(
      this.wsPath,
      upgradeWebSocket(() => {
        return this.connectionManager.createClientWebSocketHandler(
          this.messageRouter,
          () => this.rpcCacheManager.getFunctionNames(),
          () => this.clientConfigValues!
        );
      })
    );
  }
}
