import { z } from "zod";

// Health endpoint schemas
export const HealthResponseSchema = z.object({
  status: z.enum(["ok", "degraded", "unhealthy"]).openapi({ example: "ok" }),
  uptime_ms: z.number().openapi({ example: 123456 }),
  subsystems: z.object({
    workers: z.object({
      status: z.enum(["ok", "degraded", "unhealthy"]).openapi({ example: "ok" }),
      total: z.number().openapi({ example: 5 }),
      ready: z.number().openapi({ example: 5 }),
      failed: z.number().openapi({ example: 0 }),
      crashed: z.number().openapi({ example: 0 }),
      starting: z.number().openapi({ example: 0 }),
    }),
    mcp_servers: z.object({
      status: z.enum(["ok", "degraded", "unhealthy"]).openapi({ example: "ok" }),
      servers: z.record(
        z.string(),
        z.object({
          status: z.enum(["connected", "disconnected", "reconnecting", "failed"]).openapi({ example: "connected" }),
          last_health_check: z.string().nullable().openapi({ example: "2026-04-07T12:00:00.000Z" }),
          reconnect_attempts: z.number().openapi({ example: 0 }),
        }),
      ).openapi({ example: {} }),
    }),
  }),
});

// Namespaces endpoint schemas
export const NamespacesResponseSchema = z.string().openapi({
  description: "List of available namespaces with function counts",
  example: "<namespaces>\n- fs (12 functions)\n- db (8 functions)\n- mcp_bestpractices (5 functions)\n</namespaces>",
});

// RPC namespace metadata schemas
export const RpcNamespaceMetadataResponseSchema = z.string().openapi({
  description: "Human-readable namespace metadata with function signatures",
  example: "Available Namespaces:\n\n<namespaces>\nfiledb:\n  - createTable\n  - query\n</namespaces>",
});

// Types endpoint schemas
export const TypesResponseSchema = z.string().openapi({
  description: "TypeScript type definitions for all RPC functions",
  example: "export interface CreateTableArgs { tableName: string; columns: Column[]; }",
});

// Namespace-specific types endpoint schemas
export const NamespaceTypesParamSchema = z.object({
  namespaces: z.string().openapi({
    param: { name: "namespaces", in: "path" },
    description: "Comma-separated list of namespace names",
    example: "filedb,zendesk",
  }),
});

export const NamespaceTypesResponseSchema = z.string().openapi({
  description: "TypeScript type definitions for specific namespaces",
  example: "export interface FiledbCreateTableArgs { tableName: string; }",
});

// Client code endpoint schemas
export const ClientCodeResponseSchema = z.string().openapi({
  description: "Generated RPC client code for browser/Deno usage",
  example: "export class RpcClient {\n  async call(method: string, args: any) { ... }\n}",
});
