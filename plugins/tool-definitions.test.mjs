// SPDX-License-Identifier: MIT OR Apache-2.0
// Harness for plugins/tool-definitions.ts. Run: node plugins/tool-definitions.test.mjs
// Uses only fixture definition files in a temp dir (via the plugin's own
// TOOL_DEFINITIONS_DIR isolation hook) and a mock ctx. It never touches the
// real ~/.config/nix/tool-definitions and never spawns the CLI.

import assert from "node:assert/strict"
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"

const ROOT = await mkdtemp(join(tmpdir(), "tool-definitions-test-"))

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

// The plugin resolves its definitions dir from the environment at import
// time, so each scenario sets TOOL_DEFINITIONS_DIR and imports a fresh copy
// of the module (query string busts the ESM cache).
let importSeq = 0
const loadPlugin = async (dir) => {
  if (dir === null) delete process.env.TOOL_DEFINITIONS_DIR
  else process.env.TOOL_DEFINITIONS_DIR = dir
  const mod = await import(`./tool-definitions.ts?case=${++importSeq}`)
  return mod.default
}

// A mock v2 ctx recording exactly what setup() registers: the tool.transform
// callback (applied to a recording editor) and the session hooks.
const makeCtx = () => {
  const rec = { updates: new Map(), hooks: new Map(), transforms: 0 }
  const ctx = {
    tool: {
      transform: async (fn) => {
        rec.transforms++
        const editor = {
          update: (id, mutate) => {
            const tool = { description: "<original>" }
            mutate(tool)
            rec.updates.set(id, tool.description)
          },
        }
        await fn(editor)
      },
    },
    session: {
      hook: async (name, fn) => rec.hooks.set(name, fn),
    },
  }
  return { ctx, rec }
}

const fixtureDir = async (name, files) => {
  const dir = join(ROOT, name)
  await mkdir(dir, { recursive: true })
  for (const [file, body] of files) await writeFile(join(dir, file), body)
  return dir
}

console.log("\n# tool-definitions: loading and aliases")

await test("v1 alias files map to v2 ids; plain files keep their id; bodies untrimmed", async () => {
  const dir = await fixtureDir("basic", [
    ["bash.txt", "SHELL BODY\n"],
    ["task.txt", "SUBAGENT BODY"],
    ["edit.txt", "EDIT BODY\n\n"],
  ])
  const plugin = await loadPlugin(dir)
  assert.equal(plugin.id, "tool-definitions")
  const { ctx, rec } = makeCtx()
  await plugin.setup(ctx)
  assert.equal(rec.updates.get("shell"), "SHELL BODY\n")
  assert.equal(rec.updates.get("subagent"), "SUBAGENT BODY")
  assert.equal(rec.updates.get("edit"), "EDIT BODY\n\n") // deliberately NOT trimmed
  assert.equal(rec.updates.has("bash"), false)
  assert.equal(rec.updates.has("task"), false)
  assert.deepEqual([...rec.hooks.keys()].sort(), ["compaction", "context", "generate"])
})

await test("v2 file beats its v1 alias — alias written first", async () => {
  const dir = await fixtureDir("v2wins-a", [
    ["bash.txt", "V1 ALIAS BODY"],
    ["shell.txt", "V2 BODY"],
  ])
  const plugin = await loadPlugin(dir)
  const { ctx, rec } = makeCtx()
  await plugin.setup(ctx)
  assert.equal(rec.updates.get("shell"), "V2 BODY")
  assert.equal(rec.updates.size, 1)
})

await test("v2 file beats its v1 alias — v2 written first", async () => {
  const dir = await fixtureDir("v2wins-b", [
    ["shell.txt", "V2 BODY"],
    ["bash.txt", "V1 ALIAS BODY"],
  ])
  const plugin = await loadPlugin(dir)
  const { ctx, rec } = makeCtx()
  await plugin.setup(ctx)
  assert.equal(rec.updates.get("shell"), "V2 BODY")
  assert.equal(rec.updates.size, 1)
})

await test("whitespace-only files and non-.txt files are skipped", async () => {
  const dir = await fixtureDir("skips", [
    ["shell.txt", "  \n\t\n"],
    ["notes.md", "NOT A DEFINITION"],
    ["edit.txt", "EDIT BODY"],
  ])
  const plugin = await loadPlugin(dir)
  const { ctx, rec } = makeCtx()
  await plugin.setup(ctx)
  assert.deepEqual([...rec.updates.keys()], ["edit"])
})

await test("a dir with no usable definitions registers nothing and does not throw", async () => {
  const dir = await fixtureDir("empty", [["shell.txt", "\n"]])
  const plugin = await loadPlugin(dir)
  const { ctx, rec } = makeCtx()
  await plugin.setup(ctx)
  assert.equal(rec.transforms, 0)
  assert.equal(rec.hooks.size, 0)
})

await test("missing definitions dir registers nothing and does not throw", async () => {
  const plugin = await loadPlugin(join(ROOT, "does-not-exist"))
  const { ctx, rec } = makeCtx()
  await plugin.setup(ctx)
  assert.equal(rec.transforms, 0)
  assert.equal(rec.hooks.size, 0)
  assert.equal(rec.updates.size, 0)
})

console.log("\n# tool-definitions: session reapply hooks")

await test("reapply restores a clobbered description and leaves an appended list intact", async () => {
  const dir = await fixtureDir("reapply", [
    ["shell.txt", "SHELL BODY\n"],
    ["task.txt", "SUBAGENT BODY"],
  ])
  const plugin = await loadPlugin(dir)
  const { ctx, rec } = makeCtx()
  await plugin.setup(ctx)
  for (const name of ["context", "compaction", "generate"]) {
    const hook = rec.hooks.get(name)
    assert.equal(typeof hook, "function", `${name} hook registered`)
    const event = {
      tools: {
        // shell's built-in rebuilds its description wholesale per request.
        shell: { description: "zsh" },
        // subagent's built-in appends its agent list after the base text.
        subagent: { description: "SUBAGENT BODYAvailable subagents: a, b" },
      },
    }
    hook(event)
    assert.equal(event.tools.shell.description, "SHELL BODY\n", `${name}: shell restored`)
    assert.equal(
      event.tools.subagent.description,
      "SUBAGENT BODYAvailable subagents: a, b",
      `${name}: subagent's appended list kept`,
    )
  }
})

await test("reapply ignores tools that are absent from the event", async () => {
  const dir = await fixtureDir("reapply-absent", [["shell.txt", "SHELL BODY"]])
  const plugin = await loadPlugin(dir)
  const { ctx, rec } = makeCtx()
  await plugin.setup(ctx)
  const hook = rec.hooks.get("context")
  hook({ tools: {} })
  hook({})
  hook({ tools: { other: { description: "untouched" } } })
})

await rm(ROOT, { recursive: true, force: true })
console.log(`\ntool-definitions: ${passed} tests passed`)
if (process.exitCode) console.error("tool-definitions: FAILURES above")
