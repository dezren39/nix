import assert from "node:assert/strict"
import test, { describe } from "node:test"
import sidepulsePlugin from "./sidepulse.js"

describe("sidepulse plugin", () => {
  test("plugin default export shape", () => {
    assert.equal(sidepulsePlugin.id, "sidepulse")
    assert.equal(typeof sidepulsePlugin.setup, "function")
  })

  test("setup registers expected hooks and cleans up cleanly", async () => {
    const hooksRegistered = []
    let subscribedSignal = null

    const mockCtx = {
      location: { directory: "/tmp/project" },
      event: {
        async *subscribe({ signal }) {
          subscribedSignal = signal
          yield {
            type: "session.created",
            data: { sessionID: "ses_001" },
            location: { directory: "/tmp/project" },
          }
          yield {
            type: "session.execution.started",
            data: { sessionID: "ses_001" },
            location: { directory: "/tmp/project" },
          }
        },
      },
      session: {
        get: async () => ({ parentID: null }),
        hook: async (name, _fn) => {
          hooksRegistered.push(`session:${name}`)
          return { dispose: async () => {} }
        },
      },
      tool: {
        hook: async (name, _fn) => {
          hooksRegistered.push(`tool:${name}`)
          return { dispose: async () => {} }
        },
      },
    }

    const cleanup = await sidepulsePlugin.setup(mockCtx)

    // Allow event stream processing loop to run
    await new Promise((resolve) => setTimeout(resolve, 50))

    assert.deepEqual(hooksRegistered, [
      "session:prompt",
      "tool:execute.before",
      "tool:execute.after",
    ])

    await cleanup()
    assert.equal(subscribedSignal?.aborted, true)
  })
})
