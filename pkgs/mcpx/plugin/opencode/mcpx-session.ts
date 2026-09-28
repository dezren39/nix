import type { Plugin } from "@opencode-ai/plugin"

/**
 * Tell mcpx which opencode session it is working for.
 *
 * mcpx leases MCP servers per session: two agents running at once get separate
 * processes rather than corrupting one shared browser. That only works if mcpx
 * can tell the sessions apart, and nothing in a shell command carries that --
 * the agent does not know its own session id, and asking it to pass one would
 * burn tokens on plumbing and be forgotten half the time.
 *
 * So the harness supplies it. Every shell command gets a handful of environment
 * variables; mcpx reads them and the agent never learns any of this happened.
 *
 * ## What the hook is actually given
 *
 * `shell.env` receives exactly `{ cwd, sessionID?, callID? }`. Everything
 * richer -- the parent session, the title, when it started -- has to be
 * fetched, and it is fetched once per session and cached, because those values
 * cannot change for the life of a session. A per-command request would be a
 * real cost paid on every invocation for something that never differs.
 *
 * ## Why so many variables
 *
 * Environment variables are not context. The model never sees them, and an
 * unread one costs a few bytes. So anything already in hand goes in, on the
 * reasoning that a variable nobody reads is cheaper than a variable that is
 * missing and needs another release to add.
 *
 * What stays out is anything that would need work per command: token counts,
 * transcript lengths, cost. Those come from `mcpx stats --opencode`, which
 * asks once over the whole database instead of once per shell invocation.
 *
 * ## Configuration
 *
 *   MCPX_PLUGIN_ENV=minimal|standard|full   how much to inject (default standard)
 *   MCPX_PLUGIN_INSTRUCTIONS=1              add mcpx usage to the system prompt
 *   MCPX_PLUGIN_TOOL_TIMING=1               record tool outcomes into mcpx's log
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

/** Set a variable only when it is actually something. */
const put = (env: Record<string, string>, key: string, value: unknown): void => {
  if (value === undefined || value === null) return
  const s = String(value)
  if (s === "" || s === "undefined" || s === "null") return
  env[key] = s
}

/** What is worth remembering about a session, fetched once. */
type Cached = {
  parentID?: string
  title?: string
  directory?: string
  projectID?: string
  version?: string
  created?: number
  /** Depth in the parent chain: 0 for a root session. */
  depth: number
  /** Every ancestor, nearest first. */
  ancestry: string[]
}

export default (async ({ directory, worktree, project, client, $ }) => {
  // Read once. These cannot change for the life of the process, and doing
  // them per command would put a subprocess in the path of every shell
  // invocation.
  const startedAt = new Date().toISOString()
  let version = ""
  try {
    version = (await $`opencode --version`.quiet().nothrow().text()).trim()
  } catch {
    /* absent rather than fatal */
  }

  const sessions = new Map<string, Cached>()
  let commands = 0

  /**
   * Fetch a session and its ancestry, once.
   *
   * The walk is bounded and every failure is swallowed: this runs in the path
   * of every shell command, and a plugin that can break the shell is worse
   * than a plugin that sometimes omits a variable.
   */
  const describe = async (id: string): Promise<Cached> => {
    const hit = sessions.get(id)
    if (hit) return hit

    const out: Cached = { depth: 0, ancestry: [] }
    try {
      let cur: string | undefined = id
      for (let i = 0; cur && i < 16; i++) {
        const res: any = await client.session.get({ path: { id: cur } })
        const s = res?.data ?? res
        if (!s?.id) break
        if (i === 0) {
          out.parentID = s.parentID
          out.title = s.title
          out.directory = s.directory
          out.projectID = s.projectID
          out.version = s.version
          out.created = s.time?.created
        } else {
          out.ancestry.push(s.id)
        }
        cur = s.parentID
        if (cur) out.depth = i + 1
      }
    } catch {
      /* a session we cannot describe still gets its id injected */
    }
    sessions.set(id, out)
    return out
  }

  return {
    "shell.env": async (input, output) => {
      const env: Record<string, string> = (output.env ??= {})
      const lvl = level()

      // The one thing always injected. Without it session-scoped leasing
      // silently degrades to a single shared instance, which is the failure
      // this whole file exists to prevent.
      put(env, "MCPX_SESSION_ID", input.sessionID)
      if (lvl === "minimal") return

      // Both of the other fields the hook is given. cwd is the directory the
      // command will actually run in, which is not always the project root,
      // and callID distinguishes two commands issued in the same turn.
      put(env, "MCPX_OPENCODE_CWD", input.cwd)
      put(env, "MCPX_CALL_ID", input.callID)

      put(env, "MCPX_OPENCODE_DIRECTORY", directory)
      put(env, "MCPX_OPENCODE_WORKTREE", worktree)
      if (worktree) put(env, "MCPX_WORKTREE_NAME", String(worktree).split("/").pop())
      put(env, "MCPX_PROJECT_ID", (project as any)?.id)

      put(env, "MCPX_HARNESS", "opencode")
      put(env, "MCPX_HARNESS_VERSION", version)
      put(env, "MCPX_HARNESS_PID", process.env.OPENCODE_PID)
      put(env, "MCPX_HARNESS_STARTED", startedAt)
      put(env, "MCPX_SHELL_SEQ", String(++commands))

      const trace: Array<[string, ...string[]]> = []
      if (input.sessionID) trace.push(["session_id", input.sessionID])
      if (input.callID) trace.push(["call_id", input.callID])
      if (worktree) trace.push(["worktree", String(worktree)])

      if (lvl === "full" && input.sessionID) {
        // Only at full, because the first command of a session pays for the
        // fetch. Cached thereafter, so the cost is per session rather than
        // per command.
        const s = await describe(input.sessionID)
        put(env, "MCPX_PARENT_SESSION_ID", s.parentID)
        put(env, "MCPX_SESSION_TITLE", s.title)
        put(env, "MCPX_SESSION_DIRECTORY", s.directory)
        put(env, "MCPX_SESSION_VERSION", s.version)
        put(env, "MCPX_SESSION_DEPTH", String(s.depth))
        if (s.created) {
          put(env, "MCPX_SESSION_CREATED", new Date(s.created).toISOString())
          put(env, "MCPX_SESSION_AGE_MS", String(Date.now() - s.created))
        }
        if (s.parentID) trace.push(["parent_session_id", s.parentID])
        if (s.ancestry.length) trace.push(["ancestry", ...s.ancestry])
      }

      if (trace.length) put(env, "MCPX_TRACE_IDS", JSON.stringify(trace))
    },

    /**
     * Usage guidance in the system prompt, off by default.
     *
     * Off because it is the expensive kind of help: every token is paid on
     * every request for the life of the session, whether or not an MCP tool
     * is ever reached for. Worth switching on in a project that leans on
     * mcpx, wasteful everywhere else.
     */
    "experimental.chat.system.transform": async (_input, output) => {
      if (!on("MCPX_PLUGIN_INSTRUCTIONS")) return
      const text = [
        "MCP servers are reached through `mcpx`, not through tool calls.",
        "`mcpx ls` lists namespaces, `mcpx types <ns>` prints signatures, and",
        "`mcpx exec '<typescript>'` runs code against them. Only what the",
        "script prints returns to you, so filter before you print.",
      ].join(" ")
      const parts = (output as any)?.parts
      if (Array.isArray(parts)) parts.push({ type: "text", text })
    },

    /**
     * Tool outcomes into mcpx's own log, off by default.
     *
     * When on, opencode's tool calls land in the same store as mcpx's, so one
     * `mcpx stats` covers both. It shells out per tool call, which is exactly
     * why it is not the default.
     */
    "tool.execute.after": async (input, output) => {
      if (!on("MCPX_PLUGIN_TOOL_TIMING")) return
      const record = {
        event: "harness.tool",
        tool: input.tool,
        session: input.sessionID,
        call: input.callID,
        title: (output as any)?.title,
      }
      await $`mcpx log record ${JSON.stringify(record)}`.quiet().nothrow()
    },
  }
}) satisfies Plugin
