/**
 * Talk to the mcpx daemon directly, and find it without the mcpx binary.
 *
 * Measured on this machine, same request, mean of thirty:
 *
 *   spawn `mcpx status`            23.12 ms
 *   unix socket, new connection     0.27 ms
 *   unix socket, keep-alive         0.17 ms
 *   tcp loopback, new connection    1.59 ms
 *   tcp loopback, keep-alive        0.65 ms
 *
 * So the socket is roughly 135x faster than spawning, and almost all of the
 * difference is process startup rather than transport. That does not matter
 * for something called once a session; it matters a great deal for anything
 * on the path of every tool call, which is exactly where a plugin sits.
 *
 * ## Why there is a ladder
 *
 * This module used to answer "which daemon?" by spawning `mcpx --json
 * status`. That made the binary a precondition for the plugin, which is
 * wrong in two directions: a user may have a daemon (launchd, a container, a
 * remote host) with no `mcpx` on the plugin's PATH, and opencode v2 removes
 * Bun's `$` from the plugin API altogether, so the runtime no longer offers a
 * way to run a command at all.
 *
 * Everything discovery needs is already on disk. The daemon writes
 * `daemon-<key>.json` into its state directory with the socket, the endpoint,
 * the config it loaded, its version and when it started. Reading that needs
 * `node:fs`, which every runtime has. So the ladder reads files first, asks a
 * daemon second, and spawns only as a late fallback.
 *
 * ## Portability
 *
 * Nothing here imports from opencode. The only runtime affordances used are
 * `fetch` (with Bun's `unix` option), `node:fs`, `node:os`, `node:path` and
 * `node:child_process` -- deliberately, because the same core is wanted for
 * codex, claude, pi and manus, each of which will bring its own thin adapter.
 * `mcpx-session.ts` is opencode's adapter; this file is the part that ports.
 */

import { execFile } from "node:child_process"
import { readdirSync, readFileSync, statSync } from "node:fs"
import { homedir, tmpdir } from "node:os"
import { join } from "node:path"

import { DaemonOps } from "./ops.gen.ts"

/**
 * Every tunable in one place.
 *
 * The Go side keeps its constants in `internal/defaults/defaults.json` so
 * that no number hides beside the code that happens to need it. The plugin
 * ships as loose files and cannot read that, so this is the same idea in the
 * only form available here: one table, and each entry overridable through the
 * options a harness passes in.
 */
export const TUNING = {
  /** A health check is a local connect; anything slower is a dead daemon. */
  healthTimeoutMs: 1_500,
  /** One daemon answering for another directory. Still a local request. */
  resolveTimeoutMs: 2_000,
  /** A spawn of `mcpx --json status`, measured at ~23 ms. */
  cliTimeoutMs: 5_000,
  /** How long a failed lookup is remembered before the ladder runs again. */
  missCooldownMs: 60_000,
  /** The file the remembered choice is kept in, under the state directory. */
  rememberFile: "opencode-daemons.json",
  /** Default binary for rung 4, when a harness passes no `bin`. */
  bin: "mcpx",
  /** Output a spawned mcpx may print before it is truncated. */
  cliMaxOutput: 8 << 20,
  /** Records mcpx_observe pulls back. More than this is a query, not a look. */
  observeLimit: 40,
  /**
   * How stale the remembered choice may be before it is re-read.
   *
   * The TUI picker runs in a different realm and can only tell the server
   * plugin anything through this file, so a choice made there has to be
   * noticed. One small read every few seconds, on mcpx paths only.
   */
  rememberRecheckMs: 5_000,
} as const

/**
 * Parse a Go duration string ("60s", "1m30s", "500ms") into milliseconds.
 *
 * The settings registry hands every duration out in the syntax a person types
 * into MCPX_*, and that syntax is Go's. The plugin cannot call into Go to
 * resolve one, so it parses the same spelling here; anything it does not
 * understand is treated as absent, because a cooldown of NaN would retry
 * forever.
 */
export const goDurationMs = (v: string | undefined): number | undefined => {
  if (!v) return undefined
  const unit: Record<string, number> = { ns: 1e-6, us: 1e-3, µs: 1e-3, ms: 1, s: 1000, m: 60_000, h: 3_600_000 }
  const re = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g
  let total = 0
  let matched = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(v.trim())) !== null) {
    total += Number(m[1]) * unit[m[2]]
    matched += m[0].length
  }
  const body = v.trim().replace(/^[+-]/, "")
  if (matched !== body.length || matched === 0) return undefined
  return v.trim().startsWith("-") ? undefined : total
}

/** Where a daemon is. */
export type Target =
  | { kind: "socket"; path: string }
  | { kind: "url"; base: string }

/** Two targets are the same daemon when they name the same thing. */
export const sameTarget = (a?: Target, b?: Target): boolean => {
  if (!a || !b) return false
  if (a.kind !== b.kind) return false
  return a.kind === "socket" ? a.path === (b as any).path : a.base === (b as any).base
}

/** A target as one string, for logs, annotations and the remember file. */
export const targetKey = (t: Target): string =>
  t.kind === "socket" ? "unix://" + t.path : t.base

/** Parse the string form back. Anything that is not a URL is a socket path. */
export const parseTarget = (s: string): Target => {
  const v = s.trim()
  if (v.startsWith("unix://")) return { kind: "socket", path: v.slice("unix://".length) }
  if (/^https?:\/\//.test(v)) return { kind: "url", base: v.replace(/\/$/, "") }
  return { kind: "socket", path: v }
}

/** What a daemon publishes about itself, as written to `daemon-<key>.json`. */
export type DaemonInfo = {
  pid?: number
  socket?: string
  endpoint?: string
  configPath?: string
  configHash?: string
  version?: string
  startedAt?: string
}

/** What GET /v1/resolve answers. */
export type Resolution = {
  socket: string
  endpoint: string
  configPath: string
  configHash: string
  running: boolean
  sources?: string[]
}

/** Which rung produced a candidate, kept so a report can explain itself. */
export type CandidateSource =
  | "explicit"
  | "remembered"
  | "info"
  | "socket"
  | "resolve"
  | "cli"

/** One daemon that might be the right one. */
export type Candidate = {
  target: Target
  source: CandidateSource
  /** Where the info file was, when there was one. */
  infoPath?: string
  pid?: number
  endpoint?: string
  configPath?: string
  /** The daemon key: the fingerprint that names the socket and info file. */
  configHash?: string
  version?: string
  startedAt?: string
  /** Configured servers, from /v1/status. */
  servers?: number
  /** Distinct pooled instance keys, which is how many sessions hold one. */
  sessions?: number
  /** Live instances across every pool. */
  instances?: number
}

/** Why the ladder stopped where it did. */
export type Reason =
  | "explicit"
  | "remembered"
  | "only"
  | "resolved"
  | "cli"
  | "guess"
  | "none"

/** The whole answer, including the parts a harness may want to report. */
export type Discovery = {
  target?: Target
  chosen?: Candidate
  /** Every live daemon seen, best first. */
  candidates: Candidate[]
  /** The rung number from the design, 0..6. */
  rung: number
  reason: Reason
  /**
   * True when more than one daemon was live and nothing authoritative chose
   * between them. This is what a toast and a tool-result annotation are for;
   * it is never a reason to fail.
   */
  ambiguous: boolean
  /** One line, present exactly when something should be said out loud. */
  warning?: string
}

/** Everything the ladder can be told. Each has an environment equivalent. */
export type DiscoverOptions = {
  /** The directory whose daemon is wanted. */
  directory?: string
  /** Rung 0: a named endpoint. `unix://path` or `http://host:port`. */
  endpoint?: string
  /** Rung 0: a named socket path. */
  socket?: string
  /** Rung 4: the binary to spawn, or "" to forbid spawning. */
  bin?: string
  /** Rung 4: arguments to put before mcpx's own. */
  binArgs?: string[]
  /** Rung 1: a choice already made this session. */
  remembered?: Target
  /** Read the remembered choice from the state directory too. Default true. */
  useRememberFile?: boolean
  /** Overridable for tests; defaults to process.env. */
  env?: Record<string, string | undefined>
}

const envOf = (o: DiscoverOptions): Record<string, string | undefined> =>
  o.env ?? (process.env as Record<string, string | undefined>)

/**
 * Where the daemon keeps state, mirroring `internal/daemon/paths.go`.
 *
 * Kept in step by hand, because the alternative -- asking a daemon -- is the
 * thing that cannot be done before a daemon has been found.
 */
export const stateDir = (env: Record<string, string | undefined>): string => {
  if (env.MCPX_STATE_DIR) return env.MCPX_STATE_DIR
  if (env.XDG_STATE_HOME) return join(env.XDG_STATE_HOME, "mcpx")
  return join(env.HOME ?? homedir(), ".local", "state", "mcpx")
}

/**
 * Every directory a daemon can leave a socket in.
 *
 * The info file is always in the state directory, but the *socket* is not: a
 * state path longer than sun_path (100 bytes, portably) sends it to
 * `$XDG_RUNTIME_DIR/mcpx` and, failing that, to a private per-user directory
 * under the temp directory. A discovery that only listed the state directory
 * found nothing at all on those machines, which is the bug that made the old
 * "newest .sock in the state directory" guess look like "no daemon".
 */
export const socketDirs = (env: Record<string, string | undefined>): string[] => {
  const out = [stateDir(env)]
  if (env.XDG_RUNTIME_DIR) out.push(join(env.XDG_RUNTIME_DIR, "mcpx"))
  const uid = typeof process.getuid === "function" ? process.getuid() : undefined
  if (uid !== undefined) out.push(join(env.TMPDIR ?? tmpdir(), `mcpx-${uid}`))
  return [...new Set(out)]
}

const readDir = (dir: string): string[] => {
  try {
    return readdirSync(dir)
  } catch {
    return []
  }
}

/** Read every `daemon-*.json` a daemon has published. */
export const readInfoFiles = (env: Record<string, string | undefined>): Array<DaemonInfo & { infoPath: string }> => {
  const dir = stateDir(env)
  const out: Array<DaemonInfo & { infoPath: string }> = []
  for (const name of readDir(dir)) {
    if (!name.startsWith("daemon") || !name.endsWith(".json")) continue
    const infoPath = join(dir, name)
    try {
      const info = JSON.parse(readFileSync(infoPath, "utf8")) as DaemonInfo
      if (info && typeof info.socket === "string" && info.socket) out.push({ ...info, infoPath })
    } catch {
      // A half-written or corrupt file is not a daemon. The daemon writes
      // through a rename, so this should only ever be someone else's file.
    }
  }
  return out
}

/**
 * Sockets with no info file beside them.
 *
 * A daemon whose info file was deleted, or which put its socket somewhere the
 * info file does not point at, is still a daemon. Health-checking a socket
 * costs a connect, so listing them is cheap insurance against the info files
 * being the only source of truth.
 */
export const orphanSockets = (
  env: Record<string, string | undefined>,
  known: Set<string>,
): string[] => {
  const out: string[] = []
  for (const dir of socketDirs(env)) {
    for (const name of readDir(dir)) {
      if (!name.endsWith(".sock")) continue
      const path = join(dir, name)
      if (known.has(path)) continue
      try {
        // Fail closed on anything that is not ours. The socket's permissions
        // are the access control for the daemon API, so a socket owned by
        // another user must never become a candidate -- dialing it would
        // send this session's calls to somebody else's servers.
        const st = statSync(path)
        if (typeof process.getuid === "function" && st.uid !== process.getuid()) continue
      } catch {
        continue
      }
      out.push(path)
    }
  }
  return out
}

const withTimeout = (ms: number): RequestInit => {
  try {
    return { signal: AbortSignal.timeout(ms) }
  } catch {
    return {}
  }
}

/**
 * A client that reuses its connection.
 *
 * Bun dials a unix socket by passing `unix` to fetch, so there is one code
 * path for both transports and no hand-rolled HTTP.
 */
export class DaemonClient {
  constructor(readonly target: Target) {}

  /**
   * Every /v1 operation as a typed method, generated from the table the
   * routes are declared in (`ops.gen.ts`). The named methods below are
   * conveniences over some of them, with typed answers; these are the whole
   * surface, so nothing the daemon serves is reachable only through a
   * hand-built URL.
   */
  readonly ops: DaemonOps = new DaemonOps((path, init) => this.raw(path, init))

  private url(path: string): string {
    // Any host works over a socket; the dispatcher ignores it.
    return this.target.kind === "socket" ? `http://mcpx${path}` : this.target.base + path
  }

  private init(extra: RequestInit = {}): RequestInit {
    if (this.target.kind !== "socket") return extra
    // Bun's fetch takes `unix`. opencode runs plugins under Bun, so that is
    // the one that matters here. Node's fetch ignores the option and would
    // dial the placeholder host instead -- a URL target is the way to reach
    // a daemon from Node.
    return { ...extra, unix: this.target.path } as RequestInit
  }

  /** The raw response, for the few callers that need the status code. */
  async raw(path: string, init: RequestInit = {}): Promise<Response> {
    return fetch(this.url(path), this.init(init))
  }

  async get<T>(path: string, timeoutMs?: number): Promise<T> {
    const res = await this.raw(path, timeoutMs ? withTimeout(timeoutMs) : {})
    if (!res.ok) throw new Error(`mcpx ${path}: ${res.status} ${res.statusText}`)
    return (await res.json()) as T
  }

  /** A text/plain endpoint: /v1/types, /v1/catalog, /v1/client.ts. */
  async text(path: string, timeoutMs?: number): Promise<string> {
    const res = await this.raw(path, timeoutMs ? withTimeout(timeoutMs) : {})
    if (!res.ok) throw new Error(`mcpx ${path}: ${res.status} ${res.statusText}`)
    return await res.text()
  }

  async post<T>(path: string, body: unknown, timeoutMs?: number): Promise<T> {
    const res = await this.postRaw(path, body, timeoutMs)
    if (!res.ok) throw new Error(`mcpx ${path}: ${res.status} ${res.statusText}`)
    return (await res.json()) as T
  }

  /** POST without throwing on status, so a 404 can be handled as a fact. */
  async postRaw(path: string, body: unknown, timeoutMs?: number): Promise<Response> {
    return this.raw(path, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body ?? {}),
      ...(timeoutMs ? withTimeout(timeoutMs) : {}),
    })
  }

  /** PUT, for the settings API. */
  async putRaw(path: string, body: unknown, timeoutMs?: number): Promise<Response> {
    return this.raw(path, {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body ?? {}),
      ...(timeoutMs ? withTimeout(timeoutMs) : {}),
    })
  }

  /**
   * Whether the daemon is answering, and what it says about itself.
   *
   * @op health
   */
  async health(timeoutMs = TUNING.healthTimeoutMs): Promise<
    { status: string; version?: string; pid?: number; uptime?: string; endpoint?: string } | undefined
  > {
    try {
      return await this.get("/v1/health", timeoutMs)
    } catch {
      return undefined
    }
  }

  /** Whether the daemon is answering. */
  async alive(): Promise<boolean> {
    return (await this.health()) !== undefined
  }

  /**
   * The daemon, its pools and live instances.
   *
   * @op status
   */
  status(): Promise<DaemonStatus> {
    return this.get("/v1/status")
  }

  /**
   * Which daemon serves a directory. Undefined when the route is absent.
   *
   * @op resolve
   */
  async resolve(dir: string): Promise<Resolution | undefined> {
    try {
      const res = await this.raw(
        `/v1/resolve?dir=${encodeURIComponent(dir)}`,
        withTimeout(TUNING.resolveTimeoutMs),
      )
      if (!res.ok) return undefined
      return (await res.json()) as Resolution
    } catch {
      return undefined
    }
  }

  /**
   * Every namespace, with tool counts.
   *
   * @op namespaces
   */
  namespaces(): Promise<Array<{ namespace: string; tools: number; description?: string }>> {
    return this.get("/v1/namespaces?_")
  }

  /**
   * TypeScript signatures for namespaces -- `mcpx types` without a spawn.
   *
   * @op types
   */
  types(ns?: string): Promise<string> {
    return this.text(`/v1/types${ns ? `?ns=${encodeURIComponent(ns)}` : ""}`)
  }

  /**
   * Run a script against every server at once.
   *
   * Returns undefined when the daemon has no /v1/exec, which is how a caller
   * knows to fall back rather than reporting a failure that is really a
   * version difference.
   *
   * @op exec
   */
  async exec(source: string): Promise<{ output?: string; result?: unknown } | undefined> {
    const res = await this.postRaw("/v1/exec", {
      source,
      options: { output: "structured" },
    })
    if (res.status === 404) return undefined
    if (!res.ok) throw new Error(`mcpx /v1/exec: ${res.status} ${res.statusText}`)
    return (await res.json()) as { output?: string; result?: unknown }
  }

  /**
   * Call one tool.
   *
   * Through the generated method, so the body's keys are the route's. This
   * was hand-built and sent the arguments as `arguments` and the session as
   * `sessionId`; /v1/call reads `args` and `session`, ignored both, and
   * called every tool with no arguments at all.
   *
   * @op call
   */
  call(
    namespace: string,
    tool: string,
    args: Record<string, unknown>,
    session?: string,
  ): Promise<{ result: unknown }> {
    return this.ops.call({ server: namespace, tool, args, session }) as Promise<{ result: unknown }>
  }

  /**
   * Questions a server is waiting on an answer for.
   *
   * @op elicit_list
   */
  pendingElicitations(session?: string): Promise<unknown[]> {
    const q = session ? `?session=${encodeURIComponent(session)}` : ""
    return this.get(`/v1/elicit${q}`)
  }

  /**
   * Answer a question.
   *
   * The action is in the path and the body is only ever content, so there is
   * no way to send an accept with the wrong shape or a decline with content
   * that will be ignored.
   *
   * @op elicit_answer
   */
  answer(id: string, action: "accept" | "decline" | "cancel", content?: unknown): Promise<unknown> {
    return this.post(`/v1/elicit/${encodeURIComponent(id)}/${action}`, content ?? {})
  }

  /**
   * Query the durable log -- `mcpx log` without the process.
   *
   * The filters are the ones the command takes, and the daemon runs them
   * through the same query builder, so a plugin and a prompt select the same
   * records.
   *
   * @op log_query
   */
  logQuery(filter: LogFilter = {}): Promise<{ records: LogRecord[]; chain?: LogChainLevel[] }> {
    return this.get(`/v1/log${query(filter)}`)
  }

  /**
   * Aggregate the log: `mcpx stats` as JSON.
   *
   * @op stats_query
   */
  stats(opts: StatsQuery = {}): Promise<{ dimension: string; rows: unknown }> {
    return this.get(`/v1/stats${query(opts)}`)
  }

  /**
   * Search the public registry for servers that are not configured here.
   * `truncated` means the registry had more than came back.
   *
   * @op registry_search
   */
  registrySearch(q: string, limit?: number): Promise<{ servers: RegistryEntry[]; truncated: boolean }> {
    return this.get(`/v1/registry/search${query({ q, limit })}`)
  }

  /**
   * Argument autocomplete from an upstream server.
   *
   * `upstream: false` means the answer came from what mcpx already knows
   * rather than from the server itself -- worth showing differently, because
   * an empty list from a server and an empty list from a guess mean
   * different things.
   *
   * @op complete
   */
  complete(req: CompleteRequest): Promise<{ completion: Completion; upstream: boolean }> {
    return this.post("/v1/complete", req)
  }

  /**
   * Call a tool as a task: a handle now, the result later.
   *
   * For anything slow enough that holding a request open would invite an
   * intermediary to time it out.
   *
   * @op call
   */
  callAsTask(
    namespace: string,
    tool: string,
    args: Record<string, unknown>,
    opts: { ttl?: number; session?: string } = {},
  ): Promise<{ task: Task }> {
    return this.post("/v1/call", {
      server: namespace,
      tool,
      args,
      session: opts.session,
      task: { ttl: opts.ttl },
    })
  }

  /**
   * Every task this daemon holds.
   *
   * @op tasks_list
   */
  tasks(): Promise<{ tasks: Task[] }> {
    return this.get("/v1/tasks")
  }

  /**
   * One task's status.
   *
   * @op task_get
   */
  task(id: string): Promise<Task> {
    return this.get(`/v1/tasks/${encodeURIComponent(id)}`)
  }

  /**
   * Wait for a task and collect its result.
   *
   * @op task_result
   */
  taskResult(id: string, waitMs?: number): Promise<{ result: unknown }> {
    return this.get(`/v1/tasks/${encodeURIComponent(id)}/result${query({ waitMs })}`)
  }

  /**
   * Stop a running task.
   *
   * @op task_cancel
   */
  cancelTask(id: string): Promise<Task> {
    return this.post(`/v1/tasks/${encodeURIComponent(id)}/cancel`, {})
  }

  /**
   * Stop this daemon. Used when the user picks one and wants the rest gone.
   *
   * @op shutdown
   */
  async shutdown(): Promise<boolean> {
    try {
      const res = await this.postRaw("/v1/shutdown", {})
      return res.ok
    } catch {
      // The daemon may close the connection as it exits, which is a success
      // that looks like a transport error.
      return true
    }
  }

  /**
   * Write a setting into the user's mcpx configuration.
   *
   * Returns "absent" when the daemon has no settings API, so the caller can
   * explain that rather than reporting a failure the user cannot act on.
   *
   * @op settings_set
   */
  async putSetting(key: string, value: unknown, persist = true): Promise<"ok" | "absent" | "failed"> {
    try {
      const res = await this.putRaw(`/v1/settings/${encodeURIComponent(key)}`, { value, persist })
      if (res.status === 404) return "absent"
      return res.ok ? "ok" : "failed"
    } catch {
      return "failed"
    }
  }

  /**
   * Append a record to mcpx's durable log -- `mcpx log record` without the
   * process. Same parser on the daemon side, so the record is identical to
   * one sent by spawning the binary.
   *
   * @op log_record
   */
  record(record: Record<string, unknown>, level?: "debug" | "info" | "warn" | "error"): Promise<unknown> {
    const q = level ? `?level=${level}` : ""
    return this.post(`/v1/log${q}`, record)
  }
}

/** What /v1/status answers, as much of it as discovery cares about. */
export type DaemonStatus = {
  version?: string
  pid?: number
  uptime?: string
  endpoint?: string
  socket?: string
  config?: string
  servers?: Array<{
    name?: string
    namespace?: string
    live?: number
    instances?: Array<{ key?: string }>
  }>
}

/** A filter over the durable log, matching the flags `mcpx log` takes. */
export type LogFilter = {
  since?: string
  until?: string
  level?: "debug" | "info" | "warn" | "error"
  event?: string
  server?: string
  tool?: string
  session?: string
  trace?: string
  /** A trace and every ancestor, returned as a tree rather than a list. */
  chain?: string
  grep?: string
  limit?: number
  reverse?: "0" | "1"
}

/** One indexed log line. */
export type LogRecord = {
  id: number
  time: string
  level: string
  msg?: string
  template?: string
  attrs?: Record<string, unknown>
}

/** One trace in an ancestry, with the records that belong to it. */
export type LogChainLevel = {
  trace: string
  parent?: string
  depth: number
  records: LogRecord[]
}

/** What to aggregate the log by. */
export type StatsQuery = {
  by?: "calls" | "servers" | "instances" | "errors" | "sessions" | "volume" | "slowest"
  since?: string
  until?: string
  server?: string
  tool?: string
  session?: string
  top?: number
}

/** One server the registry knows about. */
export type RegistryEntry = {
  name: string
  namespace: string
  description?: string
  version?: string
  install?: string
  addWith: string
}

/** An autocomplete request for one argument of a prompt or a resource. */
export type CompleteRequest = {
  server: string
  ref: { type: "ref/prompt" | "ref/resource"; name?: string; uri?: string }
  argument: { name: string; value?: string }
  session?: string
  context?: Record<string, unknown>
}

/** What a completion reply carries; the specification caps values at 100. */
export type Completion = { values: string[]; total: number; hasMore: boolean }

/** A call running in the background. */
export type Task = {
  taskId: string
  status: "working" | "input_required" | "completed" | "failed" | "cancelled"
  statusMessage?: string
  createdAt: string
  lastUpdatedAt: string
  ttl: number
  pollInterval?: number
}

/**
 * Build a query string, dropping anything absent.
 *
 * Sending `limit=undefined` is worse than sending nothing: the daemon reads
 * it as a literal and answers with an empty page.
 */
const query = (params: Record<string, unknown>): string => {
  const parts = Object.entries(params)
    .filter(([, v]) => v !== undefined && v !== null && v !== "")
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
  return parts.length ? `?${parts.join("&")}` : ""
}

// ---------------------------------------------------------------------------
// The ladder
// ---------------------------------------------------------------------------

/** Rung 0: a target named outright, by option or by environment. */
export const explicitTarget = (opts: DiscoverOptions): Target | undefined => {
  const env = envOf(opts)
  const endpoint = opts.endpoint ?? env.MCPX_DAEMON_ENDPOINT ?? env.MCPX_ENDPOINT
  if (endpoint) return parseTarget(endpoint)
  const socket = opts.socket ?? env.MCPX_SOCKET
  if (socket) return { kind: "socket", path: socket }
  return undefined
}

/** Fill in what /v1/status knows that the info file does not. */
const describe = async (c: Candidate): Promise<Candidate> => {
  try {
    const st = await new DaemonClient(c.target).status()
    const keys = new Set<string>()
    let instances = 0
    for (const s of st.servers ?? []) {
      instances += s.live ?? 0
      for (const i of s.instances ?? []) if (i.key) keys.add(i.key)
    }
    return {
      ...c,
      version: st.version ?? c.version,
      pid: st.pid ?? c.pid,
      endpoint: st.endpoint ?? c.endpoint,
      configPath: st.config ?? c.configPath,
      servers: st.servers?.length ?? c.servers,
      sessions: keys.size,
      instances,
    }
  } catch {
    return c
  }
}

/** Rung 2: everything on disk, filtered to what is actually listening. */
export const liveCandidates = async (opts: DiscoverOptions = {}): Promise<Candidate[]> => {
  const env = envOf(opts)
  const seen = new Set<string>()
  const pending: Candidate[] = []

  for (const info of readInfoFiles(env)) {
    const path = info.socket as string
    if (seen.has(path)) continue
    seen.add(path)
    pending.push({
      target: { kind: "socket", path },
      source: "info",
      infoPath: info.infoPath,
      pid: info.pid,
      endpoint: info.endpoint,
      configPath: info.configPath,
      // The info file's own "configHash" is the digest of the server
      // definitions, used to invalidate the schema cache. The daemon *key*
      // -- the thing that names this file and the socket -- is in the file
      // name, and that is the one discovery compares against /v1/resolve.
      configHash: keyFromInfoPath(info.infoPath),
      version: info.version,
      startedAt: info.startedAt,
    })
  }
  for (const path of orphanSockets(env, seen)) {
    pending.push({ target: { kind: "socket", path }, source: "socket" })
  }

  // In parallel: a dead daemon costs the same as a live one, and the whole
  // scan has to fit inside the budget of a plugin boot.
  const checked = await Promise.all(
    pending.map(async (c): Promise<Candidate | undefined> => {
      const health = await new DaemonClient(c.target).health()
      if (!health) return undefined
      return { ...c, version: c.version ?? health.version, pid: c.pid ?? health.pid }
    }),
  )
  return checked.filter((c): c is Candidate => c !== undefined)
}

const keyFromInfoPath = (p: string): string | undefined => {
  const m = /daemon-([^/\\]+)\.json$/.exec(p)
  return m ? m[1] : undefined
}

/**
 * The best candidate, in the order the design sets out.
 *
 * A config path that is an ancestor of the session directory is the only
 * signal that is about *this* project rather than about the machine, so it
 * wins outright. After that, the most recently started daemon is the one the
 * user most likely started for this work, and the one with the most servers
 * is the one most likely to be able to answer.
 */
export const rank = (candidates: Candidate[], directory?: string): Candidate[] => {
  const dir = directory ? directory.replace(/\/+$/, "") : undefined
  const ancestor = (c: Candidate): number => {
    if (!dir || !c.configPath) return 0
    const root = c.configPath.replace(/\/[^/]*$/, "")
    return dir === root || dir.startsWith(root + "/") ? 1 : 0
  }
  const started = (c: Candidate): number => {
    const t = c.startedAt ? Date.parse(c.startedAt) : NaN
    return Number.isNaN(t) ? 0 : t
  }
  return [...candidates].sort(
    (a, b) =>
      ancestor(b) - ancestor(a) ||
      started(b) - started(a) ||
      (b.servers ?? 0) - (a.servers ?? 0) ||
      targetKey(a.target).localeCompare(targetKey(b.target)),
  )
}

/**
 * Rung 4: ask the binary, in the session's directory.
 *
 * `node:child_process` rather than Bun's `$`, because v2's plugin runtime no
 * longer hands over a shell and this file is meant to survive that. execFile
 * also takes an argument vector, so nothing here is shell-quoted.
 */
export const askCLI = async (opts: DiscoverOptions): Promise<Candidate | undefined> => {
  const out = await runBin(opts, ["--json", "status"])
  if (out === undefined) return undefined
  try {
    const start = out.indexOf("{")
    if (start < 0) return undefined
    const st = JSON.parse(out.slice(start))
    if (st?.running === true && typeof st.socket === "string" && st.socket) {
      return {
        target: { kind: "socket", path: st.socket },
        source: "cli",
        configPath: st.config,
        version: st.version,
        pid: st.pid,
      }
    }
  } catch {
    /* not JSON, or a status that says nothing is running */
  }
  return undefined
}

/**
 * Run the mcpx binary and return what it printed, or undefined if it could
 * not be run at all.
 *
 * Undefined distinguishes "there is no binary" from "the binary said
 * something unhelpful", which are different situations for a caller: the
 * first means fall back, the second means report.
 */
export const runBin = (
  opts: DiscoverOptions,
  argv: string[],
  timeoutMs = TUNING.cliTimeoutMs,
): Promise<string | undefined> => {
  const env = envOf(opts)
  const bin = opts.bin ?? env.MCPX_PLUGIN_BIN ?? TUNING.bin
  if (!bin) return Promise.resolve(undefined)
  const extra = opts.binArgs ?? splitArgs(env.MCPX_PLUGIN_BIN_ARGS)
  return new Promise((resolve) => {
    try {
      execFile(
        bin,
        [...extra, ...argv],
        { cwd: opts.directory, timeout: timeoutMs, encoding: "utf8", maxBuffer: TUNING.cliMaxOutput },
        (err, stdout, stderr) => {
          const out = String(stdout ?? "").trim()
          const bad = String(stderr ?? "").trim()
          // A non-zero exit with output is still an answer -- `mcpx status`
          // exits non-zero when no daemon is running and prints why.
          if (err && !out && !bad) return resolve(undefined)
          resolve(out || bad)
        },
      )
    } catch {
      // No such binary, or a runtime with no child_process at all.
      resolve(undefined)
    }
  })
}

/**
 * Split `MCPX_PLUGIN_BIN_ARGS` into an argument vector.
 *
 * JSON first, so an argument containing a space can be expressed exactly;
 * whitespace otherwise, because `--config /x` is what people will type.
 */
export const splitArgs = (raw?: string): string[] => {
  const v = (raw ?? "").trim()
  if (!v) return []
  if (v.startsWith("[")) {
    try {
      const parsed = JSON.parse(v)
      if (Array.isArray(parsed)) return parsed.map(String)
    } catch {
      /* fall through to whitespace */
    }
  }
  return v.split(/\s+/)
}

/**
 * Walk the ladder.
 *
 * It never throws and never returns a failure that a caller has to handle as
 * an error: the worst case is `reason: "none"`, meaning no daemon is running
 * at all, which is a fact about the machine rather than a fault.
 *
 * The one hard rule is rung 0: a named endpoint is used and the ladder stops.
 * Falling back to a local daemon because a remote one did not answer would
 * silently answer from the wrong machine, with the wrong servers and the
 * wrong credentials.
 */
export const discover = async (opts: DiscoverOptions = {}): Promise<Discovery> => {
  const env = envOf(opts)

  const explicit = explicitTarget(opts)
  if (explicit) {
    const health = await new DaemonClient(explicit).health()
    return {
      target: explicit,
      chosen: {
        target: explicit,
        source: "explicit",
        version: health?.version,
        pid: health?.pid,
        endpoint: health?.endpoint,
      },
      candidates: [],
      rung: 0,
      reason: "explicit",
      ambiguous: false,
      warning: health
        ? undefined
        : `mcpx: ${targetKey(explicit)} was named explicitly but is not answering.`,
    }
  }

  // Rung 1: a choice already made. Checked before the scan because it is one
  // request against many, and because re-deciding every session would make
  // "remember this" mean nothing.
  const remembered =
    opts.remembered ?? (opts.useRememberFile === false ? undefined : readRemembered(env, opts.directory))
  if (remembered && (await new DaemonClient(remembered).health())) {
    return {
      target: remembered,
      chosen: { target: remembered, source: "remembered" },
      candidates: [],
      rung: 1,
      reason: "remembered",
      ambiguous: false,
    }
  }

  // Rung 2: the info files. No binary, no spawn, one readdir and a health
  // check each.
  const live = await liveCandidates(opts)
  if (live.length === 0) {
    // Nothing on disk is answering. A binary may still know something the
    // filesystem does not -- a daemon started with MCPX_SOCKET pointing
    // somewhere unusual, for instance.
    const cli = await askCLI(opts)
    if (cli) {
      return { target: cli.target, chosen: cli, candidates: [cli], rung: 4, reason: "cli", ambiguous: false }
    }
    return { candidates: [], rung: 6, reason: "none", ambiguous: false }
  }

  const detailed = await Promise.all(live.map(describe))
  const ranked = rank(detailed, opts.directory)

  if (ranked.length === 1) {
    return {
      target: ranked[0].target,
      chosen: ranked[0],
      candidates: ranked,
      rung: 2,
      reason: "only",
      ambiguous: false,
    }
  }

  // Rung 3: any of them can answer for the directory, including for a
  // directory it does not serve itself. One request turns an ambiguous scan
  // into a definite answer.
  if (opts.directory) {
    for (const c of ranked) {
      const res = await new DaemonClient(c.target).resolve(opts.directory)
      if (!res) continue
      const named = ranked.find(
        (x) => x.target.kind === "socket" && x.target.path === res.socket,
      )
      if (res.running && named) {
        return {
          target: named.target,
          chosen: { ...named, source: "resolve", configPath: res.configPath || named.configPath },
          candidates: ranked,
          rung: 3,
          reason: "resolved",
          ambiguous: false,
        }
      }
      // A definite answer that nothing is serving this directory. The other
      // daemons are for other projects; say so rather than pretending one of
      // them is the right one.
      break
    }
  }

  // Rung 4: the binary, if there is one. It resolves configuration exactly
  // as mcpx does, which is the same answer /v1/resolve gives -- so this only
  // runs when the daemons are too old to have that route.
  const cli = await askCLI(opts)
  if (cli) {
    const named = ranked.find((x) => sameTarget(x.target, cli.target))
    return {
      target: cli.target,
      chosen: named ? { ...named, source: "cli" } : cli,
      candidates: ranked,
      rung: 4,
      reason: "cli",
      ambiguous: false,
    }
  }

  // Rung 6. Rung 5 -- asking -- belongs to the harness adapter, which knows
  // whether anyone is there to answer; the core returns the facts it needs
  // to ask with. Never a failure: guessing with a visible warning beats
  // refusing to work.
  const best = ranked[0]
  return {
    target: best.target,
    chosen: best,
    candidates: ranked,
    rung: 6,
    reason: "guess",
    ambiguous: true,
    warning: summarise(ranked, best),
  }
}

/** The one line a toast or an annotation shows. */
export const summarise = (candidates: Candidate[], chosen: Candidate): string => {
  const name = label(chosen)
  const bits = [name]
  if (chosen.startedAt) bits.push(`started ${ago(chosen.startedAt)}`)
  if (chosen.servers !== undefined) bits.push(`${chosen.servers} server${chosen.servers === 1 ? "" : "s"}`)
  return `mcpx: ${candidates.length} daemons match this directory. Using ${bits.join(", ")}.`
}

/** A short human name for a daemon: its project, else its socket. */
export const label = (c: Candidate): string => {
  if (c.configPath) {
    const dir = c.configPath.replace(/\/[^/]*$/, "")
    const base = dir.split("/").filter(Boolean).pop()
    if (base) return base
  }
  return targetKey(c.target)
}

/** "3h ago", from an RFC 3339 timestamp. */
export const ago = (iso: string): string => {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return "unknown"
  const s = Math.max(0, Math.round((Date.now() - t) / 1000))
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86400)}d ago`
}

// ---------------------------------------------------------------------------
// Remembering, rung 1's other half
// ---------------------------------------------------------------------------

export type RememberScope = "session" | "until-gone" | "indefinite"

/** One remembered choice, keyed by the directory it was made for. */
export type Remembered = {
  target: string
  scope: Exclude<RememberScope, "session">
  at: string
}

/**
 * Where the remembered choice lives.
 *
 * v1 server plugins have no storage API at all -- `ctx.storage` arrives in
 * v2 -- so a file under mcpx's own state directory is the only durable place
 * a v1 plugin has. Keying by directory rather than by project id keeps it
 * usable from a harness that has no notion of a project.
 */
export const rememberPath = (env: Record<string, string | undefined>): string =>
  join(stateDir(env), TUNING.rememberFile)

const readRememberFile = (env: Record<string, string | undefined>): Record<string, Remembered> => {
  try {
    const parsed = JSON.parse(readFileSync(rememberPath(env), "utf8"))
    return parsed && typeof parsed === "object" ? parsed : {}
  } catch {
    return {}
  }
}

/** The choice remembered for a directory, if any. */
export const readRemembered = (
  env: Record<string, string | undefined>,
  directory?: string,
): Target | undefined => {
  if (!directory) return undefined
  const hit = readRememberFile(env)[directory]
  if (!hit?.target) return undefined
  return parseTarget(hit.target)
}

/**
 * Remember a choice on disk.
 *
 * "until-gone" and "indefinite" are the same record; they differ in what
 * happens when the socket stops answering. Both fall back to the ladder at
 * that point -- rung 1 health-checks before it trusts the memory -- but
 * "until-gone" also erases itself, so a daemon that has died stops being
 * offered as the remembered answer on the next boot.
 */
export const writeRemembered = async (
  env: Record<string, string | undefined>,
  directory: string,
  target: Target,
  scope: Exclude<RememberScope, "session">,
): Promise<void> => {
  const fs = await import("node:fs/promises")
  const dir = stateDir(env)
  const all = readRememberFile(env)
  all[directory] = { target: targetKey(target), scope, at: new Date().toISOString() }
  await fs.mkdir(dir, { recursive: true })
  const path = rememberPath(env)
  const tmp = path + ".tmp"
  // Rename rather than write in place: a half-written file read by another
  // opencode window would be indistinguishable from no memory at all.
  await fs.writeFile(tmp, JSON.stringify(all, null, 2), { mode: 0o600 })
  await fs.rename(tmp, path)
}

/** Forget a directory's choice. */
export const forgetRemembered = async (
  env: Record<string, string | undefined>,
  directory: string,
): Promise<void> => {
  const fs = await import("node:fs/promises")
  const all = readRememberFile(env)
  if (!(directory in all)) return
  delete all[directory]
  const path = rememberPath(env)
  const tmp = path + ".tmp"
  await fs.writeFile(tmp, JSON.stringify(all, null, 2), { mode: 0o600 })
  await fs.rename(tmp, path)
}

/**
 * Open a client, or return undefined when no daemon is reachable.
 *
 * Undefined rather than throwing: a plugin that fails to load because mcpx
 * is not running has broken the editor for a tool the user may not even be
 * using. Degrading to no-op is the only acceptable failure here.
 */
export const connect = async (
  opts: DiscoverOptions = {},
): Promise<{ client?: DaemonClient; discovery: Discovery }> => {
  const discovery = await discover(opts)
  if (!discovery.target) return { discovery }
  return { client: new DaemonClient(discovery.target), discovery }
}
