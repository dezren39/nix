// Vendors opencode's built-in tool descriptions into this repo so they can be
// edited, reviewed, and diffed like any other prompt asset.
//
// registry.ts:318 fires `tool.definition` for every tool before the definition
// reaches the model, and `output.description` is mutable. Any .txt file in
// tool-definitions/ whose basename matches a tool id replaces that tool's
// description wholesale.
//
// Two limits, both from the upstream hook signature (plugin/src/index.ts:334):
//   - the hook receives only { toolID }, never the agent, so an override is
//     global; it cannot vary per agent the way `permission` can.
//   - it runs at registry.ts:318 while describeTask is appended at :326, so
//     overriding "task" replaces the base text but not the agent list.
//
// A file that exactly matches upstream is a no-op by definition, so leaving
// unmodified copies here is harmless and keeps the full set reviewable. Delete
// a file to fall back to whatever the installed opencode ships.

import type { Plugin } from "@opencode-ai/plugin"
import { readdirSync, readFileSync } from "node:fs"
import { homedir } from "node:os"
import { join } from "node:path"

// Resolved from $HOME rather than import.meta.dir: this file is symlinked into
// ~/.config/opencode/plugin/, and symlink resolution differs by runtime.
const DIR = join(homedir(), ".config", "nix", "tool-definitions")

export default (async () => {
  const overrides = new Map<string, string>()
  try {
    for (const file of readdirSync(DIR)) {
      if (!file.endsWith(".txt")) continue
      // Deliberately NOT trimmed. Upstream imports these files verbatim, so they
      // carry a trailing newline. registry.ts joins the task description with
      // describeTask using "\n", and that trailing newline is what produces the
      // blank line between them. Trimming here would silently reformat.
      const body = readFileSync(join(DIR, file), "utf8")
      if (body.trim()) overrides.set(file.slice(0, -4), body)
    }
  } catch {
    // No directory, no overrides. Never break startup over a prompt asset.
  }

  return {
    "tool.definition": async ({ toolID }, output) => {
      const replacement = overrides.get(toolID)
      if (replacement) output.description = replacement
    },
  }
}) satisfies Plugin
