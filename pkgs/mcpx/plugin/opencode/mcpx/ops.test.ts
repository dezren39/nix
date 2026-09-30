/**
 * The generated operation methods, against a fake daemon on a real unix
 * socket and -- when the Go suite hands one over in OPS_LIVE_SOCKET -- against
 * a real daemon.
 *
 *   bun test plugin/opencode/mcpx/ops.test.ts
 *
 * The fake half proves the request shapes: that a path parameter is escaped,
 * a raw body arrives as bytes, a bare-document route receives the document.
 * The live half proves the shapes are the ones the routes actually read,
 * which is the half a fake cannot: DaemonClient.call() passed every fake
 * anyone wrote for it while the daemon ignored its arguments.
 */

import { afterEach, describe, expect, test } from "bun:test"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"

import { DaemonClient } from "./daemon.ts"
import { buildRequest, OpError, OPS } from "./ops.gen.ts"

type Seen = { method: string; path: string; type: string | null; body: Uint8Array }

const cleanup: Array<() => void> = []
afterEach(() => {
  while (cleanup.length) cleanup.pop()?.()
})

/** A daemon that records every request and answers with `reply`. */
const fake = (reply: (s: Seen) => Response = () => Response.json({ ok: true })) => {
  const dir = mkdtempSync(join(tmpdir(), "mcpxo-"))
  const socket = join(dir, "d.sock")
  const seen: Seen[] = []
  const server = Bun.serve({
    unix: socket,
    async fetch(req) {
      const url = new URL(req.url)
      const s: Seen = {
        method: req.method,
        path: url.pathname + url.search,
        type: req.headers.get("content-type"),
        body: new Uint8Array(await req.arrayBuffer()),
      }
      seen.push(s)
      return reply(s)
    },
  })
  cleanup.push(() => {
    server.stop(true)
    rmSync(dir, { recursive: true, force: true })
  })
  return { client: new DaemonClient({ kind: "socket", path: socket }), seen }
}

const text = (b: Uint8Array) => new TextDecoder().decode(b)

describe("buildRequest", () => {
  test("escapes path parameters rather than letting them split the path", () => {
    const { path } = buildRequest("task_get", { id: "a b/c" })
    expect(path).toBe("/v1/tasks/a%20b%2Fc")
  })

  test("sends query parameters as scalars, and a switch as 1 or 0", () => {
    const { path, init } = buildRequest("types", { ns: "fs,git", instructions: "0" })
    expect(path).toBe("/v1/types?ns=fs%2Cgit&instructions=0")
    expect(init.body).toBeUndefined()
    expect(buildRequest("task_result", { id: "t", waitMs: 5 }).path).toBe("/v1/tasks/t/result?waitMs=5")
  })

  test("a bare-document route receives the document, not an object around it", () => {
    const { init } = buildRequest("tool_invoke", { tool: "mcpx_health", arguments: { a: 1 } })
    expect(JSON.parse(init.body as string)).toEqual({ a: 1 })
    const rec = buildRequest("log_record", { record: { msg: "hi" }, level: "warn" })
    expect(rec.path).toBe("/v1/log?level=warn")
    expect(JSON.parse(rec.init.body as string)).toEqual({ msg: "hi" })
  })

  test("a POST with nothing to send still sends an object", () => {
    const { init } = buildRequest("refresh")
    expect(init.method).toBe("POST")
    expect(init.body).toBe("{}")
  })

  test("refuses an argument the route does not take, and a missing required one", () => {
    expect(() => buildRequest("task_get", { id: "x", nope: 1 })).toThrow("takes no argument nope")
    expect(() => buildRequest("task_get", {})).toThrow("id is required")
  })

  test("declares every operation the table has", () => {
    // A spot check that the table is the whole table and not a subset.
    for (const name of ["health", "resolve", "protocol", "artifact_put", "ask_begin", "events", "settings_set"]) {
      expect(OPS[name]).toBeDefined()
    }
  })
})

describe("DaemonOps against a fake daemon", () => {
  test("an artifact is sent as its bytes", async () => {
    const { client, seen } = fake(() => Response.json({ id: "art-1" }))
    const bytes = new Uint8Array([0, 255, 1, 254, 10, 13])
    await client.ops.artifactPut({ name: "b.bin", content: bytes })
    expect(seen[0].method).toBe("POST")
    expect(seen[0].path).toBe("/v1/artifacts?name=b.bin")
    expect(seen[0].type).toBe("application/octet-stream")
    expect([...seen[0].body]).toEqual([...bytes])
  })

  test("a text or byte route hands back the response to read as either", async () => {
    const { client } = fake(() => new Response("declare const x: 1\n"))
    const res = await client.ops.types({ ns: "fs" })
    expect(await res.text()).toBe("declare const x: 1\n")
  })

  test("a refusal throws with the daemon's own message", async () => {
    const { client } = fake(() => Response.json({ error: "no task t; it may have expired" }, { status: 404 }))
    const err = await client.ops.taskGet({ id: "t" }).catch((e) => e)
    expect(err).toBeInstanceOf(OpError)
    expect((err as OpError).status).toBe(404)
    expect(String(err)).toContain("no task t; it may have expired")
  })

  test("call() sends the arguments under the key /v1/call reads", async () => {
    // It sent `arguments` and `sessionId`; the route reads `args` and
    // `session`, so every tool it called got nothing.
    const { client, seen } = fake(() => Response.json({ result: "ok" }))
    await client.call("demo", "echo", { message: "hi" }, "s-1")
    const body = JSON.parse(text(seen[0].body))
    expect(body).toEqual({ server: "demo", tool: "echo", args: { message: "hi" }, session: "s-1" })
  })
})

// The live half. The Go test TestThePluginSuiteRunsAgainstARealDaemon starts
// a daemon with one fake server and passes its socket.
const live = process.env.OPS_LIVE_SOCKET
describe.skipIf(!live)("DaemonOps against a real daemon", () => {
  const client = () => new DaemonClient({ kind: "socket", path: live as string })

  test("every read that needs no arguments answers", async () => {
    const c = client()
    for (const [name, spec] of Object.entries(OPS)) {
      const needs = Object.values(spec.params).some((p) => p.required)
      if (spec.method !== "GET" || needs || spec.response === "response") continue
      const out = await c.ops.invoke(name)
      expect(out).toBeDefined()
    }
  })

  test("call() reaches the tool with its arguments", async () => {
    const res = await client().call("demo", "echo", { message: "through the plugin" })
    expect(JSON.stringify(res)).toContain("through the plugin")
  })

  test("an artifact round-trips byte for byte", async () => {
    const c = client()
    const bytes = new Uint8Array(512).map((_, i) => (i * 37) % 256)
    const put = (await c.ops.artifactPut({ name: "p.bin", content: bytes })) as { id: string; size: number }
    expect(put.size).toBe(bytes.length)
    const got = new Uint8Array(await (await c.ops.artifactGet({ id: put.id })).arrayBuffer())
    expect([...got]).toEqual([...bytes])
    await c.ops.artifactDelete({ id: put.id })
    const gone = await c.ops.artifactGet({ id: put.id }).catch((e) => e)
    expect(gone).toBeInstanceOf(OpError)
  })

  test("resolve answers for the daemon's own directory", async () => {
    const dir = process.env.OPS_LIVE_DIR as string
    const res = (await client().ops.resolve({ dir })) as { running?: boolean }
    expect(res.running).toBe(true)
  })
})
