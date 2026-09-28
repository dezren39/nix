import type { Plugin } from "@opencode-ai/plugin"

/**
 * Tell mcpx which opencode session it is working for.
 *
 * mcpx leases MCP servers per session: two agents running at once get separate
 * processes rather than corrupting one shared browser. That only works if mcpx
 * can tell the sessions apart, and nothing in a shell command carries that
 * information -- the agent does not know its own session id, and asking it to
 * pass one would burn tokens on plumbing.
 *
 * So the harness supplies it. Every shell command gets a handful of environment
 * variables; mcpx reads them and the agent never learns any of this happened.
 *
 * ## What is injected, and why so much
 *
 * Environment variables cost nothing. They are not context, the model never
 * sees them, and an unread one is free. So anything cheap and possibly useful
 * goes in, on the reasoning that a variable nobody reads costs a few bytes
 * while a variable that is missing costs a round trip through this file.
 *
 * The expensive ones are deliberately absent. Transcript lengths and token
 * counts would mean a database query per shell command, which is a real cost
 * paid on every invocation for a number almost nobody reads. Those are
 * available through `mcpx stats --opencode` instead, which asks once.
 *
 * ## Configuration
 *
 * Everything beyond the session id is opt-in, through MCPX_PLUGIN_*:
 *
 *   MCPX_PLUGIN_ENV=minimal|standard|full   how much to inject (default standard)
 *   MCPX_PLUGIN_INSTRUCTIONS=1              add mcpx usage to the system prompt
 *   MCPX_PLUGIN_TOOL_TIMING=1               record tool durations into mcpx's log
 *
 * minimal is the session id alone. It exists because that is the only part
 * strictly required for leasing to work, and somebody will want the floor.
 */

type Level = "minimal" | "standard" | "full"

const level = (): Level => {
  const v = (process.env.MCPX_PLUGIN_ENV ?? "standard").toLowerCase()
  return v === "minimal" || v === "full" ? v : "standard"
}

const on = (name: string): boolean => {
  const v = process.env[name]
  return v === "1" || v === "true" || v === "yes"
}

/** A value only if it is actually something. */
const put = (env: Record<string, string>, key: string, value: unknown): void => {
  if (value === undefined || value === null) return
  const s = String(value)
  if (s === "" || s === "undefined" || s === "null") return
  env[key] = s
}

/**
 * Trace identifiers as an array of pairs.
 *
 * A flat MCPX_SESSION_ID cannot express "this session, whose parent is that
 * one, running in this worktree". A list of pairs can, is trivially parseable,
 * and grows without any consumer needing to change: a reader looks up the keys
 * it knows and ignores the rest.
 */
type TraceIds = Array<[string, string]>

const encodeTrace = (ids: TraceIds): string => JSON.stringify(ids)

export default (async ({ directory, worktree, $ }) => {
  // Read once at startup. These cannot change for the life of the process,
  // and doing it per command would put a subprocess in the path of every
  // shell invocation.
  const startedAt = new Date().toISOString()
  let version = ""
  try {
    version = (await $`opencode --version`.quiet().nothrow().text()).trim()
  } catch {
    /* not fatal; the variable is simply absent */
  }

  let counter = 0

  return {
    "shell.env": async (input, output) => {
      const env: Record<string, string> = (output.env ??= {})
      const lvl = level()

      // The one thing that is always injected. Without it, session-scoped
      // leasing silently degrades to a single shared instance, which is the
      // failure this whole file exists to prevent.
      const sessionID = (input as any)?.sessionID ?? (input as any)?.session?.id
      put(env, "MCPX_SESSION_ID", sessionID)
      if (lvl === "minimal") return

      const parentID =
        (input as any)?.session?.parentID ?? (input as any)?.parentID
      put(env, "MCPX_PARENT_SESSION_ID", parentID)

      // Directories. mcpx's repo and worktree scopes key on these, and
      // deriving them from the shell's cwd is wrong whenever a command runs
      // somewhere else.
      put(env, "MCPX_OPENCODE_DIRECTORY", directory)
      put(env, "MCPX_OPENCODE_WORKTREE", worktree)
      if (worktree) put(env, "MCPX_WORKTREE_NAME", String(worktree).split("/").pop())

      // Identifiers as pairs, so a consumer can look up what it knows and
      // ignore what it does not.
      const trace: TraceIds = []
      if (sessionID) trace.push(["session_id", String(sessionID)])
      if (parentID) trace.push(["parent_session_id", String(parentID)])
      if (worktree) trace.push(["worktree", String(worktree)])
      if (trace.length) put(env, "MCPX_TRACE_IDS", encodeTrace(trace))

      put(env, "MCPX_HARNESS", "opencode")
      put(env, "MCPX_HARNESS_VERSION", version)
      put(env, "MCPX_HARNESS_PID", process.env.OPENCODE_PID)
      put(env, "MCPX_HARNESS_STARTED", startedAt)

      if (lvl !== "full") return

      // full adds things that are nice to have and nobody should depend on.
      const agent = (input as any)?.agent ?? (input as any)?.session?.agent
      put(env, "MCPX_AGENT", typeof agent === "string" ? agent : agent?.name)

      const model = (input as any)?.model ?? (input as any)?.session?.model
      if (model) {
        put(env, "MCPX_MODEL", typeof model === "string" ? model : model?.id)
        put(env, "MCPX_MODEL_PROVIDER", model?.providerID)
        put(env, "MCPX_MODEL_VARIANT", model?.variant)
      }

      // A per-process counter, so a log can tell the third shell command of a
      // session from the thirtieth without joining anything.
      put(env, "MCPX_SHELL_SEQ", String(++counter))
    },

    /**
     * Usage guidance in the system prompt, off by default.
     *
     * It is off because it is the expensive kind of help: every token here is
     * paid on every request for the life of the session, whether or not any
     * MCP tool is ever reached for. Worth switching on in a project that
     * leans on mcpx, wasteful everywhere else.
     */
    "experimental.chat.system.transform": async (_input, output) => {
      if (!on("MCPX_PLUGIN_INSTRUCTIONS")) return
      const text = [
        "MCP servers are reached through `mcpx`, not through tool calls.",
        "`mcpx ls` lists namespaces, `mcpx types <ns>` prints signatures, and",
        "`mcpx exec '<typescript>'` runs code against them. Only what the",
        "script prints returns to you, so filter before you print.",
      ].join(" ")
      if (Array.isArray(output.parts)) {
        output.parts.push({ type: "text", text })
      } else if (Array.isArray((output as any).system)) {
        ;(output as any).system.push(text)
      }
    },

    /**
     * Tool timings into mcpx's own log, off by default.
     *
     * When on, opencode's tool calls land in the same store as mcpx's, so one
     * `mcpx stats` covers both. It shells out per tool call, which is why it
     * is not the default.
     */
    "tool.execute.after": async (input, output) => {
      if (!on("MCPX_PLUGIN_TOOL_TIMING")) return
      const record = {
        event: "harness.tool",
        tool: (input as any)?.tool,
        sessionID: (input as any)?.sessionID,
        ok: !(output as any)?.error,
      }
      await $`mcpx log record ${JSON.stringify(record)}`.quiet().nothrow()
    },
  }
}) satisfies Plugin
