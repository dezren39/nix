// SPDX-License-Identifier: MIT OR Apache-2.0
// Harness for plugins/durable-tmpdir.ts. Run: node plugins/durable-tmpdir.test.mjs
// HOME is redirected to a temp dir before import (the plugin derives its
// durable path from $HOME at import time), so the real opencode data dir is
// never touched.

import assert from "node:assert/strict"
import { existsSync } from "node:fs"
import { mkdtemp, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"

const FAKE_HOME = await mkdtemp(join(tmpdir(), "durable-tmpdir-test-"))
process.env.HOME = FAKE_HOME
const DURABLE = join(FAKE_HOME, ".local", "share", "opencode", "tmp")

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

const { default: plugin } = await import("./durable-tmpdir.ts")

console.log("\n# durable-tmpdir")

await test("default export is the v2 plugin shape", async () => {
  assert.equal(plugin.id, "durable-tmpdir")
  assert.equal(typeof plugin.setup, "function")
})

await test("setup creates the durable dir and registers create.before", async () => {
  assert.equal(existsSync(DURABLE), false)
  const hooks = new Map()
  const ctx = { shell: { hook: async (name, fn) => hooks.set(name, fn) } }
  await plugin.setup(ctx)
  assert.equal(existsSync(DURABLE), true)
  assert.deepEqual([...hooks.keys()], ["create.before"])

  const input = { env: {} }
  hooks.get("create.before")(input)
  assert.equal(input.env.TMPDIR, DURABLE)
  assert.equal(input.env.TMP, DURABLE)
  assert.equal(input.env.TEMP, DURABLE)
})

await test("create.before overwrites inherited temp vars and leaves others alone", async () => {
  const hooks = new Map()
  const ctx = { shell: { hook: async (name, fn) => hooks.set(name, fn) } }
  await plugin.setup(ctx)
  const input = { env: { TMPDIR: "/var/folders/xx/T/", PATH: "/usr/bin" } }
  hooks.get("create.before")(input)
  assert.equal(input.env.TMPDIR, DURABLE)
  assert.equal(input.env.PATH, "/usr/bin")
})

await rm(FAKE_HOME, { recursive: true, force: true })
console.log(`\ndurable-tmpdir: ${passed} tests passed`)
if (process.exitCode) console.error("durable-tmpdir: FAILURES above")
