/**
 * Talk to the mcpx daemon directly, without spawning the binary.
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
 * The socket also beats loopback TCP by about 4x, and keep-alive is worth
 * another 2.4x on TCP. Both are the same HTTP API underneath -- the daemon
 * serves one API on two listeners -- so this module picks whichever is
 * reachable and speaks the same requests either way.
 */

/** Where a daemon is. */
export type Target =
  | { kind: "socket"; path: string }
  | { kind: "url"; base: string }

/**
 * Find the daemon.
 *
 * The socket is preferred when it exists, because it is faster and because
 * its filesystem permissions are the access control. A URL is for a daemon
 * somewhere else -- another machine, a shared one for a team -- and is only
 * used when named, since guessing at a network address would be worse than
 * failing.
 */
export const findDaemon = async (
  $: any,
  opts: { endpoint?: string; directory?: string } = {},
): Promise<Target | undefined> => {
  const endpoint = opts.endpoint ?? process.env.MCPX_DAEMON_ENDPOINT ?? process.env.MCPX_ENDPOINT
  if (endpoint) {
    if (endpoint.startsWith("unix://")) return { kind: "socket", path: endpoint.slice(7) }
    return { kind: "url", base: endpoint.replace(/\/$/, "") }
  }
  if (process.env.MCPX_SOCKET) return { kind: "socket", path: process.env.MCPX_SOCKET }

  // Asked, not guessed. The socket's name carries a hash of the resolved
  // configuration, and a long state path moves it to a private runtime
  // directory entirely, so neither "the newest .sock in the state directory"
  // nor any other listing finds the right one reliably. mcpx itself knows.
  //
  // This is one spawn, and callers cache the result: the cost that matters
  // is the one on every tool call, not the one at the start of a session.
  try {
    const cmd = opts.directory
      ? $`mcpx --json status`.cwd(opts.directory)
      : $`mcpx --json status`
    const st = JSON.parse(String(await cmd.quiet().nothrow().text()))
    if (st?.running === true && typeof st.socket === "string" && st.socket) {
      return { kind: "socket", path: st.socket }
    }
  } catch {
    /* mcpx not installed, or printed something that is not JSON */
  }
  return undefined
}

/**
 * A client that reuses its connection.
 *
 * Bun and Node both dial a unix socket through the same fetch/undici path by
 * passing the socket path, so there is one code path for both transports and
 * no hand-rolled HTTP.
 */
export class DaemonClient {
  constructor(private target: Target) {}

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

  async get<T>(path: string): Promise<T> {
    const res = await fetch(this.url(path), this.init())
    if (!res.ok) throw new Error(`mcpx ${path}: ${res.status} ${res.statusText}`)
    return (await res.json()) as T
  }

  async post<T>(path: string, body: unknown): Promise<T> {
    const res = await fetch(
      this.url(path),
      this.init({
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body ?? {}),
      }),
    )
    if (!res.ok) throw new Error(`mcpx ${path}: ${res.status} ${res.statusText}`)
    return (await res.json()) as T
  }

  /** Whether the daemon is answering. */
  async alive(): Promise<boolean> {
    try {
      await this.get("/v1/health")
      return true
    } catch {
      return false
    }
  }

  /** Every namespace, with tool counts. */
  namespaces(): Promise<Array<{ namespace: string; tools: number; description?: string }>> {
    return this.get("/v1/namespaces?_")
  }

  /** Call one tool. */
  call(
    namespace: string,
    tool: string,
    args: Record<string, unknown>,
    session?: string,
  ): Promise<{ result: unknown }> {
    return this.post("/v1/call", {
      server: namespace,
      tool,
      arguments: args,
      sessionId: session,
      callId: session,
    })
  }

  /** Questions a server is waiting on an answer for. */
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
   */
  answer(id: string, action: "accept" | "decline" | "cancel", content?: unknown): Promise<unknown> {
    return this.post(`/v1/elicit/${encodeURIComponent(id)}/${action}`, content ?? {})
  }

  /**
   * Append a record to mcpx's durable log -- `mcpx log record` without the
   * process. Same parser on the daemon side, so the record is identical to
   * one sent by spawning the binary.
   */
  record(record: Record<string, unknown>, level?: "debug" | "info" | "warn" | "error"): Promise<unknown> {
    const q = level ? `?level=${level}` : ""
    return this.post(`/v1/log${q}`, record)
  }
}

/**
 * Open a client, or return undefined when no daemon is reachable.
 *
 * Undefined rather than throwing: a plugin that fails to load because mcpx
 * is not running has broken the editor for a tool the user may not even be
 * using. Degrading to no-op is the only acceptable failure here.
 */
export const connect = async (
  $: any,
  opts: { endpoint?: string; directory?: string } = {},
): Promise<DaemonClient | undefined> => {
  const target = await findDaemon($, opts)
  if (!target) return undefined
  const client = new DaemonClient(target)
  return (await client.alive()) ? client : undefined
}
