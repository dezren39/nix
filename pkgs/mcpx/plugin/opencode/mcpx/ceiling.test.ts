/**
 * The plugin against a daemon whose owner set autonomy.max to propose
 * (docs/decisions/0002-autonomy-dial.md). The Go test
 * TestThePluginCannotRaiseAutonomyAboveTheCeiling starts that daemon and
 * passes its socket; without one there is nothing to test, and the file
 * registers nothing rather than reporting a skip.
 */
import { existsSync } from "node:fs"
import { describe, expect, test } from "bun:test"

import { DaemonClient } from "./daemon.ts"

const socket = process.env.CEILING_SOCKET
const mark = process.env.CEILING_MARK as string

if (socket) {
  describe("the plugin under autonomy.max propose", () => {
    test("asking intent and recipe_run to run is lowered, reported, and does not run", async () => {
      const ops = new DaemonClient({ kind: "socket", path: socket }).ops
      const answers = [
        await ops.intent({ prompt: "leave a mark on the filesystem", autonomy: "run", placeholders: { path: mark } }),
        await ops.recipeRun({ name: "mark", autonomy: "run", placeholders: { path: mark } }),
      ] as Array<Record<string, unknown>>
      for (const a of answers) {
        expect(a.autonomy).toBe("propose")
        expect(a.requested).toBe("run")
        expect(String(a.clampedBy)).toStartWith("autonomy.max")
        expect(a.result).toBeUndefined()
      }
      expect(existsSync(mark)).toBe(false)
    })
  })
}
