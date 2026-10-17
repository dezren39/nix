// Vendors opencode's built-in tool descriptions into this repo so they can be
// edited, reviewed, and diffed like any other prompt asset.
//
// OpenCode v2 plugin shape: `export default { id, setup(ctx) }`. Two v2
// mechanisms carry the override:
//
//   1. ctx.tool.transform(editor => editor.update(id, ...)) replaces the base
//      description stored in the tool registry. This is what GET tool listings
//      and every later hook start from.
//   2. Some built-ins rebuild their description per request in a session
//      `context` / `compaction` / `generate` hook (core/src/tool/plugin/shell.ts
//      overwrites `shell` wholesale with the detected shell name;
//      subagent.ts appends "Available subagents:" to whatever the base is). So
//      a registry edit alone is clobbered for `shell`. The session hook below
//      re-applies the override only when the final text no longer starts with
//      it, which restores `shell` and leaves subagent's appended agent list
//      intact — the same "replace the base, keep the agent list" result the v1
//      `tool.definition` hook gave for `task`.
//
// Limits, re-checked against v2:
//   - v1's hook saw only { toolID }, so overrides were global. v2's session
//     `context` event carries `agent`, so per-agent overrides are now possible;
//     this port deliberately stays global to preserve behavior.
//   - v1 ran before describeTask appended the agent list. v2 has the same
//     ordering effect (subagent appends in its context hook after the base), so
//     overriding the subagent tool still replaces the base text but not the
//     agent list.
//   - v2 renamed tools: `bash` is `shell`, `task` is `subagent`. Files named for
//     the v1 IDs are mapped through ALIASES so existing files keep working; a
//     file named for the v2 ID wins. `todowrite` has no v2 built-in, so its
//     file is a no-op (update() ignores missing IDs).
//
// A file that exactly matches upstream is a no-op by definition, so leaving
// unmodified copies here is harmless and keeps the full set reviewable. Delete
// a file to fall back to whatever the installed opencode ships.

import { readdirSync, readFileSync } from "node:fs"
import { homedir } from "node:os"
import { join } from "node:path"

// Resolved from $HOME rather than import.meta.dir: this file is symlinked into
// ~/.config/opencode/plugin/, and symlink resolution differs by runtime.
// TOOL_DEFINITIONS_DIR exists only so the port can be tested in isolation.
const DIR = process.env.TOOL_DEFINITIONS_DIR ?? join(homedir(), ".config", "nix", "tool-definitions")

const ALIASES: Record<string, string> = { bash: "shell", task: "subagent" }

function load() {
  const overrides = new Map<string, string>()
  try {
    for (const file of readdirSync(DIR)) {
      if (!file.endsWith(".txt")) continue
      // Deliberately NOT trimmed. Upstream imports these files verbatim, so they
      // carry a trailing newline. Trimming here would silently reformat.
      const body = readFileSync(join(DIR, file), "utf8")
      if (!body.trim()) continue
      const name = file.slice(0, -4)
      const id = ALIASES[name] ?? name
      // A file named for the v2 ID beats one named for its v1 alias.
      if (id !== name && overrides.has(id)) continue
      overrides.set(id, body)
    }
  } catch {
    // No directory, no overrides. Never break startup over a prompt asset.
  }
  return overrides
}

export default {
  id: "tool-definitions",
  async setup(ctx: any) {
    const overrides = load()
    if (overrides.size === 0) return

    await ctx.tool.transform((editor: any) => {
      for (const [id, body] of overrides) {
        editor.update(id, (tool: any) => {
          tool.description = body
        })
      }
    })

    const reapply = (event: any) => {
      for (const [id, body] of overrides) {
        const tool = event.tools?.[id]
        if (tool && !tool.description.startsWith(body)) tool.description = body
      }
    }
    await ctx.session.hook("context", reapply)
    await ctx.session.hook("compaction", reapply)
    await ctx.session.hook("generate", reapply)
  },
}
