import { tool, type Plugin } from "@opencode-ai/plugin"
// In a subdirectory on purpose: opencode loads every plugin/*.ts file and
// calls each of its exports as a plugin, and the glob is not recursive. For
// the same reason this file exports nothing but its default.
import {
  DaemonClient,
  TUNING,
  goDurationMs,
  ago,
  discover,
  forgetRemembered,
  label,
  rank,
  readRemembered,
  runBin,
  splitArgs,
  stateDir,
  summarise,
  targetKey,
  writeRemembered,
  type Candidate,
  type Discovery,
  type DiscoverOptions,
  type Target,
} from "./mcpx/daemon.ts"
// The only reliable way to tell, in v1, whether anyone is looking at a
// screen. See `headless` below.
import { isMainThread } from "node:worker_threads"

/**
 * Tell mcpx which opencode session it is working for, and find the daemon.
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
 * ## No binary required
 *
 * Nothing here needs `mcpx` on PATH. The daemon is found by reading the info
 * files it publishes and health-checking the sockets they name; every feature
 * speaks the daemon's own `/v1` API. A binary, when present, is one more rung
 * on the ladder rather than its foundation -- which matters now, because
 * opencode v2 removes Bun's `$` from the plugin API entirely.
 *
 * ## Configuration
 *
 * Every setting below can be given three ways, and they are read in this
 * order: a plugin option, an environment variable, then the default.
 *
 * Plugin options arrive as the second argument, which opencode fills in from
 * a config entry of the form `"plugin": [["./mcpx-session.ts", { ... }]]`. A
 * plugin discovered by being dropped into `plugin/` gets no options, so the
 * environment is the way to configure that installation. See the README.
 */

/** Everything this plugin can be told, from config or from the environment. */
type Options = {
  /** A daemon named outright. `unix:///path` or `http://host:port`. */
  endpoint?: string
  /** A socket named outright. */
  socket?: string
  /** The mcpx binary, or "" to forbid spawning one. */
  bin?: string
  /** Arguments to put before mcpx's own, as a vector or a string. */
  binArgs?: string[] | string
  /** Where the opt-in tools get their answers: auto, v1 or cli. */
  backend?: "auto" | "v1" | "cli"
  /** Default scope for a daemon choice made this session. */
  remember?: "session" | "until-gone" | "indefinite"
  /** How much to inject into shell environments. */
  env?: "minimal" | "standard" | "full"
  instructions?: boolean
  toolTiming?: boolean
  tools?: boolean
  /** Offer mcpx_daemon_status and mcpx_daemon_select. Default: when unclear. */
  daemonTools?: boolean
  /** Report ambiguity on tool results. Default on. */
  annotate?: boolean
  /** Override the "is anyone looking at this" guess. */
  headless?: boolean
}

type Level = "minimal" | "standard" | "full"

const truthy = (v: unknown): boolean | undefined => {
  if (v === undefined || v === null || v === "") return undefined
  if (typeof v === "boolean") return v
  const s = String(v).toLowerCase()
  if (s === "1" || s === "true" || s === "yes" || s === "on") return true
  if (s === "0" || s === "false" || s === "no" || s === "off") return false
  return undefined
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

/**
 * Whether anyone can answer a question.
 *
 * v1 gives no direct answer -- `client.tui.showToast` publishes an event and
 * returns true whether or not a TUI is listening -- so this reads the process
 * shape instead. `opencode tui` runs the server in a Worker and the interface
 * on the main thread; `opencode run`, `opencode serve` and every CI
 * invocation run the server on the main thread with nothing attached. So
 * "this code is not on the main thread" is exactly "a TUI is attached", in
 * v1. It is checked once, because it cannot change.
 *
 * Getting it wrong in the safe direction costs a toast nobody sees. Getting
 * it wrong in the other direction would hang a headless run on a question,
 * which is why the permission prompt is never raised on this path -- only
 * `mcpx_daemon_select`, which the agent calls deliberately, asks anything.
 */
/** The remembered choice as a comparable string, or undefined. */
const remembered = (
  env: Record<string, string | undefined>,
  directory: string,
): string | undefined => {
  const t = readRemembered(env, directory)
  return t ? targetKey(t) : undefined
}

const headlessByDefault = (): boolean => {
  try {
    return isMainThread
  } catch {
    return true
  }
}

export default (async ({ directory, worktree, project, client }, options) => {
  const raw = (options ?? {}) as Options
  const env = process.env as Record<string, string | undefined>

  /** option, then environment, then default. */
  const str = (o: string | undefined, e: string | undefined, d?: string) => o ?? e ?? d
  const bool = (o: boolean | undefined, e: string | undefined, d: boolean) =>
    o ?? truthy(e) ?? d

  const settings = {
    endpoint: str(raw.endpoint, env.MCPX_DAEMON_ENDPOINT ?? env.MCPX_ENDPOINT),
    socket: str(raw.socket, env.MCPX_SOCKET),
    bin: str(raw.bin, env.MCPX_PLUGIN_BIN, TUNING.bin)!,
    binArgs: Array.isArray(raw.binArgs)
      ? raw.binArgs
      : splitArgs(typeof raw.binArgs === "string" ? raw.binArgs : env.MCPX_PLUGIN_BIN_ARGS),
    backend: (str(raw.backend, env.MCPX_PLUGIN_BACKEND, "auto") as Options["backend"])!,
    remember: (str(raw.remember, env.MCPX_PLUGIN_REMEMBER, "session") as "session" | "until-gone" | "indefinite")!,
    level: ((str(raw.env, env.MCPX_PLUGIN_ENV, "full") as Level) ?? "full") as Level,
    instructions: bool(raw.instructions, env.MCPX_PLUGIN_INSTRUCTIONS, false),
    toolTiming: bool(raw.toolTiming, env.MCPX_PLUGIN_TOOL_TIMING, false),
    tools: bool(raw.tools, env.MCPX_PLUGIN_TOOLS, false),
    annotate: bool(raw.annotate, env.MCPX_PLUGIN_ANNOTATE, true),
    headless: raw.headless ?? truthy(env.MCPX_PLUGIN_HEADLESS) ?? headlessByDefault(),
    // plugin.discoveryRetry. The registry declared it and nothing read it,
    // so an editor without mcpx installed paid the whole ladder again after
    // whatever TUNING happened to say.
    discoveryRetryMs: goDurationMs(env.MCPX_PLUGIN_DISCOVERY_RETRY) ?? TUNING.missCooldownMs,
  }
  const level: Level =
    settings.level === "minimal" || settings.level === "standard" ? settings.level : "full"

  const ladderOptions = (): DiscoverOptions => ({
    directory,
    endpoint: settings.endpoint,
    socket: settings.socket,
    bin: settings.bin,
    binArgs: settings.binArgs,
    remembered: chosen,
    env,
  })

  /**
   * The daemon, found once and reused.
   *
   * A miss is remembered for a while rather than retried on every tool call:
   * the whole point of the ladder is that it is cheap, but it is not free,
   * and a machine with no daemon at all would otherwise pay for the scan on
   * every single tool result.
   */
  let chosen: Target | undefined
  let found: Promise<Discovery> | undefined
  let missedAt = 0

  /**
   * Notice a choice made somewhere else.
   *
   * The optional TUI picker lives in the other realm and shares nothing with
   * this one but the filesystem, so the remembered choice is the only channel
   * between them. Re-read rarely rather than never: never would make the
   * picker take effect only in the next session, and always would put a file
   * read in front of every tool call.
   */
  let rememberedKey = remembered(env, directory)
  let rememberedAt = Date.now()
  const rememberMoved = (): boolean => {
    if (Date.now() - rememberedAt < TUNING.rememberRecheckMs) return false
    rememberedAt = Date.now()
    const now = remembered(env, directory)
    if (now === rememberedKey) return false
    rememberedKey = now
    return true
  }

  const look = async (): Promise<Discovery> => {
    if (found && rememberMoved()) {
      found = undefined
      chosen = undefined
    }
    if (!found) {
      if (Date.now() - missedAt < settings.discoveryRetryMs) {
        return { candidates: [], rung: 6, reason: "none", ambiguous: false }
      }
      found = discover(ladderOptions())
    }
    const d = await found
    if (!d.target) {
      found = undefined
      missedAt = Date.now()
    }
    return d
  }
  const daemon = async (): Promise<DaemonClient | undefined> => {
    const d = await look()
    return d.target ? new DaemonClient(d.target) : undefined
  }
  const forget = () => {
    found = undefined
    missedAt = Date.now()
  }

  // One discovery at boot, awaited.
  //
  // It costs a readdir and a parallel health check -- a few milliseconds --
  // and it buys two things worth more than that: the boot toast can be
  // accurate, and the daemon-selection tools can be offered only when they
  // would actually help, since opencode fixes the tool list at load time.
  const boot = await look().catch(
    (): Discovery => ({ candidates: [], rung: 6, reason: "none", ambiguous: false }),
  )

  /**
   * Say something, at most once per reason per session.
   *
   * Never when headless: `showToast` succeeds whether or not anyone is
   * watching, so an unbounded toast in CI is a silent leak rather than a
   * visible one. The reason, not the message, is the key -- a message that
   * differs only in a timestamp is still the same thing being said twice.
   */
  const said = new Set<string>()
  const toast = async (
    reason: string,
    message: string,
    variant: "info" | "warning" | "error" = "warning",
  ): Promise<void> => {
    if (settings.headless) return
    if (said.has(reason)) return
    said.add(reason)
    try {
      await client.tui.showToast({ body: { title: "mcpx", message, variant } })
    } catch {
      /* no TUI attached after all, which is not a failure */
    }
  }

  if (boot.ambiguous && boot.warning) {
    // Fire and forget: a plugin that waits on the interface during load
    // delays every session for a message.
    void toast("ambiguous", boot.warning + " Ask for mcpx_daemon_status to change it.")
  }

  /**
   * One line on every mcpx result, naming the daemon that answered.
   *
   * Cheaper than a toast and impossible to miss in a transcript, which is the
   * point: the user should never have to wonder which daemon a result came
   * from, and should never have to go looking for the answer.
   */
  const annotation = async (): Promise<string> => {
    const d = await look()
    if (!settings.annotate || !d.ambiguous || !d.chosen) return ""
    return (
      `\n\n---\nmcpx: answered by ${label(d.chosen)} (${targetKey(d.chosen.target)}); ` +
      `${d.candidates.length} daemons match this directory. ` +
      `Call mcpx_daemon_status to see them, mcpx_daemon_select to change.`
    )
  }

  /** The toast that fires the first time mcpx is actually used. */
  const announceOnUse = async (): Promise<void> => {
    const d = await look()
    if (d.ambiguous && d.warning) await toast("ambiguous", d.warning)
  }

  /**
   * Run something against the daemon, falling back to the binary.
   *
   * `backend` decides how hard to try: `v1` never spawns, `cli` never uses
   * the socket, and `auto` prefers the socket and spawns only when there is
   * no daemon or the daemon is too old to have the route.
   */
  const viaDaemon = async <T>(fn: (c: DaemonClient) => Promise<T>): Promise<T | undefined> => {
    if (settings.backend === "cli") return undefined
    const c = await daemon()
    if (!c) return undefined
    try {
      return await fn(c)
    } catch (err) {
      // Stopped, restarted, or its configuration changed and the socket
      // moved. Forget it; the next call asks again.
      forget()
      throw err
    }
  }
  const viaCLI = async (argv: string[]): Promise<string> => {
    if (settings.backend === "v1") {
      return "mcpx: no daemon is reachable, and backend=v1 forbids running the binary."
    }
    const out = await runBin(ladderOptions(), argv)
    if (out === undefined) {
      return (
        "mcpx: no daemon is reachable and no mcpx binary could be run. " +
        "Start one with `mcpx daemon`, or point the plugin at one with MCPX_DAEMON_ENDPOINT."
      )
    }
    return out || "(no output)"
  }

  const pretty = (v: unknown): string => JSON.stringify(v, null, 2)

  // Read once. These cannot change for the life of the process, and doing
  // them per command would put a subprocess in the path of every shell
  // invocation.
  const startedAt = new Date().toISOString()
  const version = process.env.OPENCODE_VERSION ?? ""

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

  /** One candidate as a line in the status table. */
  const describeCandidate = (c: Candidate, i: number, current?: Target): string => {
    const mark = current && targetKey(current) === targetKey(c.target) ? "*" : " "
    const bits = [
      `${mark} [${i}] ${label(c)}`,
      `      config:   ${c.configPath ?? "(unknown)"}`,
      `      socket:   ${targetKey(c.target)}`,
    ]
    if (c.startedAt) bits.push(`      started:  ${c.startedAt} (${ago(c.startedAt)})`)
    if (c.version) bits.push(`      version:  ${c.version}`)
    if (c.servers !== undefined) bits.push(`      servers:  ${c.servers}`)
    if (c.sessions !== undefined) bits.push(`      sessions: ${c.sessions}`)
    if (c.instances !== undefined) bits.push(`      live:     ${c.instances}`)
    return bits.join("\n")
  }

  /** Apply a choice, at the scope asked for. */
  const applyChoice = async (
    target: Target,
    scope: "session" | "until-gone" | "indefinite" | "permanent",
  ): Promise<string> => {
    chosen = target
    found = Promise.resolve(await discover({ ...ladderOptions(), remembered: target }))
    if (scope === "session") return "Remembered for this session only."
    if (scope === "permanent") {
      const res = await new DaemonClient(target).putSetting("daemon.endpoint", targetKey(target))
      if (res === "ok") return "Written to your mcpx configuration as daemon.endpoint."
      if (res === "absent") {
        // Being added concurrently. Say what to do rather than failing, and
        // leave a durable choice behind so the user is not stuck.
        await writeRemembered(env, directory, target, "indefinite")
        return (
          "This daemon has no settings API (PUT /v1/settings returned 404), so the choice " +
          "was remembered indefinitely for this directory instead. To make it permanent, " +
          `set "daemon": { "endpoint": "${targetKey(target)}" } in your mcpx config.`
        )
      }
      return "Writing the setting failed; the choice holds for this session."
    }
    await writeRemembered(env, directory, target, scope)
    return scope === "until-gone"
      ? "Remembered until this daemon stops answering."
      : "Remembered indefinitely for this directory."
  }

  // The daemon tools are offered when they can help: when the user asked for
  // them, when the full tool set is on, or when discovery found more than one
  // daemon and somebody may want to choose. They are never a gate -- the
  // plugin works whether or not anyone ever calls them, which is the whole
  // point of rung 6.
  const wantDaemonTools =
    raw.daemonTools ?? truthy(env.MCPX_PLUGIN_DAEMON_TOOLS) ?? (settings.tools || boot.ambiguous)

  const daemonTools = {
    mcpx_daemon_status: tool({
      description:
        "List every mcpx daemon running on this machine, best match first, and say " +
        "which one this session is using. Call this before mcpx_daemon_select: the " +
        "index it prints is what select takes.",
      args: {},
      async execute() {
        const d = await discover({ ...ladderOptions(), useRememberFile: true })
        const list = rank(d.candidates, directory)
        if (list.length === 0) {
          if (d.target) {
            return `Using ${targetKey(d.target)} (named explicitly; no scan was done).`
          }
          return "No mcpx daemon is running. Start one with `mcpx daemon`, or set MCPX_DAEMON_ENDPOINT."
        }
        const head =
          `${list.length} daemon${list.length === 1 ? "" : "s"} running. ` +
          (d.target ? `This session is using ${targetKey(d.target)} (${d.reason}).` : "") +
          "\n\n"
        return head + list.map((c, i) => describeCandidate(c, i, d.target)).join("\n\n")
      },
    }),

    mcpx_daemon_select: tool({
      description:
        "Point this session at one of the daemons mcpx_daemon_status listed, by its " +
        "index. The user is asked to confirm, and the tool sets the socket itself -- " +
        "pass the index, never a path.",
      args: {
        candidate: tool.schema
          .number()
          .int()
          .describe("the [index] from mcpx_daemon_status"),
        remember: tool.schema
          .enum(["session", "until-gone", "indefinite", "permanent"])
          .optional()
          .describe(
            "session: this session only (default). until-gone: until that daemon stops " +
              "answering. indefinite: for this directory, until changed. permanent: " +
              "written into the mcpx config.",
          ),
        stopOthers: tool.schema
          .boolean()
          .optional()
          .describe("stop every other daemon after selecting. Destructive: their sessions end."),
      },
      async execute(args, ctx) {
        const d = await discover({ ...ladderOptions(), useRememberFile: true })
        const list = rank(d.candidates, directory)
        if (list.length === 0) return "No daemon to select. Run mcpx_daemon_status first."
        const pick = list[args.candidate]
        if (!pick) {
          return `No candidate [${args.candidate}]. mcpx_daemon_status lists 0..${list.length - 1}.`
        }
        const scope = args.remember ?? settings.remember

        // The user confirms, and sees what they are confirming. This is the
        // only interactive channel a v1 server plugin has, and "always" is
        // most of what "remember this" means.
        await ctx.ask({
          permission: "mcpx_daemon_select",
          patterns: [targetKey(pick.target)],
          always: [targetKey(pick.target)],
          metadata: {
            daemon: label(pick),
            socket: targetKey(pick.target),
            config: pick.configPath,
            started: pick.startedAt,
            version: pick.version,
            servers: pick.servers,
            sessions: pick.sessions,
            remember: scope,
            stopOthers: args.stopOthers === true,
            others: list.filter((c) => c !== pick).map((c) => targetKey(c.target)),
          },
        })

        const note = await applyChoice(pick.target, scope)
        let stopped = ""
        if (args.stopOthers) {
          const others = list.filter((c) => targetKey(c.target) !== targetKey(pick.target))
          const results = await Promise.all(
            others.map(async (c) => ({ c, ok: await new DaemonClient(c.target).shutdown() })),
          )
          stopped =
            "\nStopped: " +
            (results.length
              ? results.map((r) => `${label(r.c)}${r.ok ? "" : " (failed)"}`).join(", ")
              : "none")
        }
        return `Now using ${label(pick)} (${targetKey(pick.target)}). ${note}${stopped}`
      },
    }),

    mcpx_daemon_forget: tool({
      description:
        "Drop the remembered daemon choice for this directory, so discovery decides again.",
      args: {},
      async execute() {
        await forgetRemembered(env, directory)
        chosen = undefined
        found = undefined
        const d = await look()
        return d.target
          ? `Forgotten. Discovery now chooses ${targetKey(d.target)} (${d.reason}).`
          : `Forgotten. No daemon is reachable. Remembered choices live in ${stateDir(env)}.`
      },
    }),
  }

  const mcpxTools = {
    mcpx_discover: tool({
      description:
        "List the MCP servers mcpx knows about, or show signatures for one. " +
        "Call with no arguments first: the answer is small and tells you " +
        "what else is worth asking for.",
      args: {
        namespace: tool.schema
          .string()
          .optional()
          .describe("a namespace to show signatures for; omit to list all"),
      },
      async execute(args) {
        await announceOnUse()
        const out = await viaDaemon(async (c) => {
          if (args.namespace) return await c.types(args.namespace)
          const ns = await c.namespaces()
          return ns
            .map((n) => `${n.namespace}  (${n.tools} tools)${n.description ? "  " + n.description : ""}`)
            .join("\n")
        })
        const body = out ?? (await viaCLI(args.namespace ? ["types", args.namespace] : ["ls"]))
        return body + (await annotation())
      },
    }),

    mcpx_exec: tool({
      description:
        "Run TypeScript against every MCP server at once. Tools are bound as " +
        "await tools.<namespace>.<tool>({...}) and only what you print comes " +
        "back, so filter and summarise here rather than reading a megabyte " +
        "of JSON into your context. This is the one that saves tokens.",
      args: {
        source: tool.schema.string().describe("TypeScript; top-level await works"),
      },
      async execute(args) {
        await announceOnUse()
        // undefined from the daemon means the route is absent -- an older
        // daemon -- which is a reason to fall back, not a failure to report.
        const out = await viaDaemon((c) => c.exec(args.source))
        const body =
          out === undefined
            ? await viaCLI(["exec", args.source])
            : (out.output ?? pretty(out.result ?? out))
        return body + (await annotation())
      },
    }),

    mcpx_observe: tool({
      description:
        "Query what mcpx has been doing: the durable log, or aggregate " +
        "statistics. Use it when a call failed and you want to know why " +
        "without running it again.",
      args: {
        what: tool.schema
          .enum(["log", "calls", "errors", "servers", "slowest"])
          .describe("log for records, the rest are aggregates"),
        since: tool.schema.string().optional().describe("15m, 2h, or an RFC3339 time"),
      },
      async execute(args) {
        await announceOnUse()
        const out = await viaDaemon(async (c) => {
          if (args.what === "log") {
            const res = await c.logQuery({ limit: TUNING.observeLimit, since: args.since })
            return pretty(res.records)
          }
          return pretty(await c.stats({ by: args.what, since: args.since }))
        })
        if (out !== undefined) return out + (await annotation())
        const argv =
          args.what === "log"
            ? ["log", "--limit", String(TUNING.observeLimit), ...(args.since ? ["--since", args.since] : [])]
            : ["stats", args.what, ...(args.since ? ["--since", args.since] : [])]
        return (await viaCLI(argv)) + (await annotation())
      },
    }),
  }

  const tools = {
    ...(settings.tools ? mcpxTools : {}),
    ...(wantDaemonTools ? daemonTools : {}),
  }

  return {
    "shell.env": async (input, output) => {
      const e: Record<string, string> = (output.env ??= {})

      // The one thing always injected. Without it session-scoped leasing
      // silently degrades to a single shared instance, which is the failure
      // this whole file exists to prevent.
      put(e, "MCPX_SESSION_ID", input.sessionID)
      if (level === "minimal") return

      // Both of the other fields the hook is given. cwd is the directory the
      // command will actually run in, which is not always the project root,
      // and callID distinguishes two commands issued in the same turn.
      put(e, "MCPX_OPENCODE_CWD", input.cwd)
      put(e, "MCPX_CALL_ID", input.callID)

      put(e, "MCPX_OPENCODE_DIRECTORY", directory)
      put(e, "MCPX_OPENCODE_WORKTREE", worktree)
      if (worktree) put(e, "MCPX_WORKTREE_NAME", String(worktree).split("/").pop())
      put(e, "MCPX_PROJECT_ID", (project as any)?.id)

      put(e, "MCPX_HARNESS", "opencode")
      put(e, "MCPX_HARNESS_VERSION", version)
      put(e, "MCPX_HARNESS_PID", process.env.OPENCODE_PID)
      put(e, "MCPX_HARNESS_STARTED", startedAt)
      put(e, "MCPX_SHELL_SEQ", String(++commands))

      // The daemon this session settled on, so a command that runs the mcpx
      // binary talks to the same one the plugin does. Without it a shell
      // command would walk its own ladder from a different directory and
      // could land on a different daemon -- which is exactly the confusion
      // the whole ladder exists to end.
      //
      // MCPX_DAEMON_ENDPOINT rather than MCPX_SOCKET: the endpoint setting is
      // only ever read when *connecting*, while MCPX_SOCKET also decides
      // where `mcpx daemon` binds -- so injecting that one would stop a user
      // starting a second daemon from a shell inside opencode.
      const active = chosen ?? boot.target
      if (active) put(e, "MCPX_DAEMON_ENDPOINT", targetKey(active))

      const trace: Array<[string, ...string[]]> = []
      if (input.sessionID) trace.push(["session_id", input.sessionID])
      if (input.callID) trace.push(["call_id", input.callID])
      if (worktree) trace.push(["worktree", String(worktree)])

      if (level === "full" && input.sessionID) {
        // Only at full, because the first command of a session pays for the
        // fetch. Cached thereafter, so the cost is per session rather than
        // per command.
        const s = await describe(input.sessionID)
        put(e, "MCPX_PARENT_SESSION_ID", s.parentID)
        put(e, "MCPX_SESSION_TITLE", s.title)
        put(e, "MCPX_SESSION_DIRECTORY", s.directory)
        put(e, "MCPX_SESSION_VERSION", s.version)
        put(e, "MCPX_SESSION_DEPTH", String(s.depth))
        if (s.created) {
          put(e, "MCPX_SESSION_CREATED", new Date(s.created).toISOString())
          put(e, "MCPX_SESSION_AGE_MS", String(Date.now() - s.created))
        }
        if (s.parentID) trace.push(["parent_session_id", s.parentID])
        if (s.ancestry.length) trace.push(["ancestry", ...s.ancestry])
      }

      if (trace.length) put(e, "MCPX_TRACE_IDS", JSON.stringify(trace))
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
      if (!settings.instructions) return
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
     * mcpx as opencode tools, off by default.
     *
     * The default is off because an agent that can run shell commands can
     * already run mcpx, and a tool definition costs context on every request
     * whether or not it is used. Worth turning on when the agent has no
     * shell, when mcpx calls should appear in the transcript, or when a model
     * keeps forgetting mcpx exists.
     *
     * The two daemon tools are the exception: they appear when discovery
     * found something worth choosing between, because a user who has to ask
     * "which daemon?" has no other way to answer it.
     */
    tool: Object.keys(tools).length ? tools : undefined,

    /**
     * Tool outcomes into mcpx's own log, off by default.
     *
     * When on, opencode's tool calls land in the same store as mcpx's, so one
     * `mcpx stats` covers both. It goes over the daemon's socket, which is a
     * fraction of a millisecond. With no daemon the record is dropped rather
     * than spawning a process per tool call: a timing is not worth 23 ms of
     * every tool call, and certainly not worth failing one.
     */
    "tool.execute.after": async (input, output) => {
      if (!settings.toolTiming) return
      const record = {
        event: "harness.tool",
        tool: input.tool,
        session: input.sessionID,
        call: input.callID,
        title: (output as any)?.title,
      }
      const mcpx = await daemon()
      if (!mcpx) return
      try {
        await mcpx.record(record)
      } catch {
        forget()
      }
    },
  }
}) satisfies Plugin
