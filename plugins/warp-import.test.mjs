// SPDX-License-Identifier: MIT OR Apache-2.0
// Harness for plugins/warp-import.js. Run: node plugins/warp-import.test.mjs
// Uses only fake data. It never touches opencode.db and never spawns the CLI.

import assert from "node:assert/strict"
import { mkdir, mkdtemp, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import plugin, { buildTool, describeSession, resolveSession } from "./warp-import.js"

// Real directories: spawn() fails with ENOENT on a non-existent cwd, so the
// "current directory" and the fake workspace directory must actually exist.
const ROOT = await mkdtemp(join(tmpdir(), "warp-import-test-"))
const CWD = join(ROOT, "repo")
const WT = join(ROOT, "worktree")
await mkdir(CWD, { recursive: true })
await mkdir(WT, { recursive: true })

let passed = 0
const test = async (name, fn) => {
  try {
    await fn()
    passed++
    console.log(`  ok  ${name}`)
  } catch (err) {
    console.error(`FAIL  ${name}\n      ${err.message}`)
    process.exitCode = 1
  }
}

const SESSIONS = [
  { id: "ses_aaa", title: "SidePulse install fixes", updated: 1_700_000_003_000, directory: "/repo" },
  { id: "ses_bbb", title: "nixos inputs bump", updated: 1_700_000_002_000, directory: "/repo" },
  { id: "ses_ccc", title: "SidePulse LED writer", updated: 1_700_000_001_000, directory: "/repo" },
]
console.log("\n# resolveSession")

await test("full id match", () => {
  const r = resolveSession(SESSIONS, "ses_bbb")
  assert.equal(r.status, "ok")
  assert.equal(r.matchedBy, "id")
  assert.equal(r.session.id, "ses_bbb")
})

await test("unique title match, case-insensitive", () => {
  const r = resolveSession(SESSIONS, "NIXOS INPUTS")
  assert.equal(r.status, "ok")
  assert.equal(r.matchedBy, "title")
  assert.equal(r.session.id, "ses_bbb")
})

await test("ambiguous title returns every candidate and does not guess", () => {
  const r = resolveSession(SESSIONS, "sidepulse")
  assert.equal(r.status, "ambiguous")
  assert.equal(r.session, undefined)
  assert.deepEqual(
    r.candidates.map((s) => s.id),
    ["ses_aaa", "ses_ccc"],
  )
})

await test("no match returns recent candidates + a reason", () => {
  const r = resolveSession(SESSIONS, "nothing matches this")
  assert.equal(r.status, "none")
  assert.equal(r.candidates.length, 3)
  assert.match(r.reason, /No session title matches/)
})

await test("unknown ses_ id does not fall through to title search", () => {
  const r = resolveSession(SESSIONS, "ses_zzz")
  assert.equal(r.status, "none")
  assert.match(r.reason, /No session has id/)
})

await test("empty list", () => {
  const r = resolveSession([], "x")
  assert.equal(r.status, "empty")
})

await test("missing / junk selector and junk input never throw", () => {
  assert.equal(resolveSession(SESSIONS, "").status, "none")
  assert.equal(resolveSession(SESSIONS, undefined).status, "none")
  assert.equal(resolveSession(null, "x").status, "empty")
  assert.equal(resolveSession([null, undefined], "x").status, "empty")
})

await test("candidates are sorted most-recent-first and rendered readably", () => {
  const r = resolveSession(SESSIONS, "")
  assert.equal(r.candidates[0].id, "ses_aaa")
  assert.match(describeSession(SESSIONS[0]), /ses_aaa — SidePulse install fixes — 2023-11-14T22:13:23\.000Z \[\/repo\]/)
})

console.log("\n# plugin entrypoint robustness")

await test("default export is the v1 plugin module shape with a required id", async () => {
  assert.equal(typeof plugin, "object")
  assert.equal(plugin.id, "warp-import")
  assert.equal(typeof plugin.server, "function")
})

await test("server() never throws on hostile input", async () => {
  assert.deepEqual(await plugin.server(undefined), {})
  assert.deepEqual(await plugin.server({}), {})
  assert.deepEqual(await plugin.server({ client: {} }), {})
})

await test("server() registers exactly one tool named warp_import", async () => {
  const hooks = await plugin.server({ client: { session: { list: async () => ({ data: [] }) } } })
  assert.deepEqual(Object.keys(hooks.tool), ["warp_import"])
})

await test("description leads with the user-initiated-only rule", async () => {
  const hooks = await plugin.server({ client: { session: { list: async () => ({ data: [] }) } } })
  assert.ok(hooks.tool.warp_import.description.startsWith("USER-INITIATED ONLY."))
})

console.log("\n# behaviour")

// A fake hey-api low-level client reachable the same way the real one is
// (client.tui._client), so postJson() takes its primary code path.
function makeEnv({ sessions = SESSIONS, onPost } = {}) {
  const calls = []
  const inner = {
    async post({ url, body }) {
      calls.push({ url, body })
      return onPost ? onPost({ url, body }) : { data: undefined }
    },
  }
  return {
    calls,
    env: {
      client: {
        session: { list: async () => ({ data: sessions }), _client: inner },
        tui: { _client: inner },
      },
      serverUrl: new URL("http://127.0.0.1:1/"),
      directory: CWD,
      worktree: CWD,
    },
  }
}

const ctx = { directory: CWD, worktree: CWD, ask: async () => {} }

await test("ambiguous selector short-circuits before any side effect", async () => {
  const { env, calls } = makeEnv()
  const out = await buildTool(env).execute({ session: "sidepulse" }, ctx)
  assert.match(out, /matches 2 sessions/)
  assert.match(out, /ses_aaa/)
  assert.match(out, /ses_ccc/)
  assert.equal(calls.length, 0, "must not create a workspace or select a session")
})

await test("denied permission aborts with no side effects", async () => {
  const { env, calls } = makeEnv()
  const denied = { ...ctx, ask: async () => { throw new Error("denied by user") } }
  const out = await buildTool(env).execute({ session: "ses_aaa" }, denied)
  assert.match(out, /was not permitted: denied by user/)
  assert.equal(calls.length, 0)
})

await test("FALLBACK: warp API throwing degrades to export+import and says so", async () => {
  const { env, calls } = makeEnv({
    onPost: ({ url }) => {
      if (url === "/experimental/workspace") throw new Error("experimental workspace API unavailable")
      return { data: undefined }
    },
  })

  // Stub the CLI so nothing is actually exported or imported.
  process.env.OPENCODE_BIN = "/usr/bin/true"
  const out = await buildTool(env).execute({ session: "ses_aaa" }, ctx)
  delete process.env.OPENCODE_BIN

  assert.match(out, /FALLBACK: workspace creation failed/)
  assert.match(out, /experimental workspace API unavailable/)
  assert.match(out, /No new worktree was made/)
  assert.ok(out.includes(`to: ${CWD}`), "falls back to the current directory")
  // It still moved the session and still switched the TUI.
  assert.ok(calls.some((c) => c.url === "/tui/select-session" && c.body.sessionID === "ses_aaa"))
  assert.match(out, /can never be warped again/)
})

await test("FALLBACK: workspace returning no directory is treated as a failure", async () => {
  const { env } = makeEnv({
    onPost: ({ url }) => (url === "/experimental/workspace" ? { data: { id: "w1" } } : { data: undefined }),
  })
  process.env.OPENCODE_BIN = "/usr/bin/true"
  const out = await buildTool(env).execute({ session: "ses_aaa" }, ctx)
  delete process.env.OPENCODE_BIN
  assert.match(out, /FALLBACK/)
  assert.match(out, /returned no directory/)
})

await test("happy path reports workspace, move warning and warp warning", async () => {
  const { env, calls } = makeEnv({
    onPost: ({ url }) =>
      url === "/experimental/workspace"
        ? { data: { id: "ws_1", directory: WT, branch: "feat/x" } }
        : { data: undefined },
  })
  process.env.OPENCODE_BIN = "/usr/bin/true"
  const out = await buildTool(env).execute({ session: "ses_aaa", branch: "feat/x" }, ctx)
  delete process.env.OPENCODE_BIN

  assert.match(out, /Moved session ses_aaa/)
  assert.match(out, /from: \/repo/)
  assert.ok(out.includes(`to: ${WT}`))
  assert.match(out, /workspace: ws_1 \(worktree, branch feat\/x\)/)
  assert.match(out, /TUI switched to this session/)
  assert.match(out, /MOVE, not a copy/)
  assert.match(out, /never be warped again/)
  assert.equal(calls[0].url, "/experimental/workspace")
  assert.deepEqual(calls[0].body, { type: "worktree", branch: "feat/x" })
  assert.equal(calls[1].url, "/tui/select-session")
})

await test("use_current_directory skips the workspace call entirely", async () => {
  const { env, calls } = makeEnv()
  process.env.OPENCODE_BIN = "/usr/bin/true"
  const out = await buildTool(env).execute({ session: "ses_aaa", use_current_directory: true }, ctx)
  delete process.env.OPENCODE_BIN
  assert.ok(!calls.some((c) => c.url === "/experimental/workspace"))
  assert.match(out, /Skipped workspace creation/)
})

await test("CLI failure is reported, not thrown, and does not select a session", async () => {
  const { env, calls } = makeEnv({
    onPost: ({ url }) =>
      url === "/experimental/workspace" ? { data: { id: "ws_1", directory: WT } } : { data: undefined },
  })
  process.env.OPENCODE_BIN = "/usr/bin/false"
  const out = await buildTool(env).execute({ session: "ses_aaa" }, ctx)
  delete process.env.OPENCODE_BIN
  assert.match(out, /failed while moving the session/)
  assert.match(out, /The session was not moved/)
  assert.ok(!calls.some((c) => c.url === "/tui/select-session"))
})

await test("session list failure is reported, not thrown", async () => {
  const env = {
    client: { session: { list: async () => { throw new Error("db locked") } } },
  }
  const out = await buildTool(env).execute({ session: "ses_aaa" }, ctx)
  assert.match(out, /could not list sessions: db locked/)
})

await test("TUI select failure is non-fatal and reported", async () => {
  const { env } = makeEnv({
    onPost: ({ url }) => {
      if (url === "/experimental/workspace") return { data: { id: "ws_1", directory: WT } }
      return { error: { data: { message: "no tui attached" } } }
    },
  })
  process.env.OPENCODE_BIN = "/usr/bin/true"
  const out = await buildTool(env).execute({ session: "ses_aaa" }, ctx)
  delete process.env.OPENCODE_BIN
  assert.match(out, /Moved session ses_aaa/)
  assert.match(out, /Could not switch the TUI automatically \(no tui attached\)/)
})

await test("execute never rejects, even with a hostile context", async () => {
  const { env } = makeEnv()
  const out = await buildTool(env).execute(undefined, undefined)
  assert.equal(typeof out, "string")
})

await test("no low-level client falls back to serverUrl fetch, and its failure degrades", async () => {
  // client.session exists but exposes no `_client`, so postJson() must take the
  // raw-fetch branch. Port 1 is closed, so the workspace call fails and the tool
  // must degrade to plain export+import rather than blowing up.
  const env = {
    client: { session: { list: async () => ({ data: SESSIONS }) } },
    serverUrl: new URL("http://127.0.0.1:1/"),
    directory: CWD,
    worktree: CWD,
  }
  process.env.OPENCODE_BIN = "/usr/bin/true"
  const out = await buildTool(env).execute({ session: "ses_aaa" }, ctx)
  delete process.env.OPENCODE_BIN
  assert.match(out, /FALLBACK: workspace creation failed/)
  assert.ok(out.includes(`to: ${CWD}`))
  assert.match(out, /Could not switch the TUI automatically/)
})

await test("no client and no serverUrl still degrades cleanly", async () => {
  const env = { client: { session: { list: async () => ({ data: SESSIONS }) } }, directory: CWD, worktree: CWD }
  process.env.OPENCODE_BIN = "/usr/bin/true"
  const out = await buildTool(env).execute({ session: "ses_aaa" }, ctx)
  delete process.env.OPENCODE_BIN
  assert.match(out, /FALLBACK/)
  assert.match(out, /no SDK client and no serverUrl available/)
})

await test("the plugin leaves no temp files behind", async () => {
  const { readdir } = await import("node:fs/promises")
  const leaked = (await readdir(tmpdir())).filter((n) => n.startsWith("warp-import-") && !n.startsWith("warp-import-test-"))
  assert.deepEqual(leaked, [], `leaked temp dirs: ${leaked.join(", ")}`)
})

await rm(ROOT, { recursive: true, force: true })

console.log(`\n${passed} passed${process.exitCode ? ", SOME FAILED" : ""}\n`)
