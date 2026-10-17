// sidepulse-opencode-plugin (OpenCode v2 plugin shape)
import { appendFile, mkdir } from "node:fs/promises"
import { connect } from "node:net"
import { homedir } from "node:os"
import { dirname, join } from "node:path"

// SIDEPULSE_OPENCODE_LOG exists only so the plugin can be exercised in a scratch
// environment without appending to the real recovery log.
const SIDEPULSE_LOG =
  process.env.SIDEPULSE_OPENCODE_LOG || "/Users/drewry.pope/.local/state/sidepulse/agent-monitor/opencode.jsonl"
const SIDEPULSE_SOCKET_NAME = "events.sock"
const SIDEPULSE_SOCKET_TIMEOUT_MS = 200
const SIDEPULSE_MAX_EVENT_BYTES = 1048576

// Mirrors sidepulse.ipc.candidate_event_socket_paths: XDG_STATE_HOME wins when the
// shell exports it, and the plain home path is always tried too because the status
// bar LaunchAgent does not inherit XDG_STATE_HOME.
function socketCandidates() {
  const targets = []
  const add = (value) => {
    if (typeof value === "string" && value.length > 0 && !targets.includes(value)) {
      targets.push(value)
    }
  }
  const stateHome = process.env.XDG_STATE_HOME
  if (stateHome) add(join(stateHome, "sidepulse", "agent-monitor", SIDEPULSE_SOCKET_NAME))
  let home = process.env.HOME
  if (!home) {
    try {
      home = homedir()
    } catch {
      home = ""
    }
  }
  if (home) add(join(home, ".local", "state", "sidepulse", "agent-monitor", SIDEPULSE_SOCKET_NAME))
  add(join(dirname(SIDEPULSE_LOG), SIDEPULSE_SOCKET_NAME))
  return targets
}

// Resolves false instead of rejecting for every failure mode: no listener, a stale
// socket file, a wedged server, or a partial write.
function writeEventSocket(target, message) {
  return new Promise((resolve) => {
    let socket
    try {
      socket = connect(target)
    } catch {
      resolve(false)
      return
    }
    let flushed = false
    let settled = false
    let guard
    const settle = (ok) => {
      if (settled) return
      settled = true
      clearTimeout(guard)
      resolve(ok)
    }
    const abort = () => {
      try {
        socket.destroy()
      } catch {
        // Already torn down.
      }
      settle(false)
    }
    guard = setTimeout(abort, SIDEPULSE_SOCKET_TIMEOUT_MS)
    guard.unref?.()
    socket.on("error", abort)
    socket.on("close", () => settle(flushed))
    socket.on("connect", () => {
      try {
        socket.end(message, () => {
          flushed = true
        })
      } catch {
        abort()
      }
    })
  })
}

// Live push to the status bar. The JSONL log above stays the recovery source; this
// is what makes the status bar react without waiting for a reconcile.
async function notifySidePulse(payload) {
  try {
    const message = JSON.stringify({ provider: "opencode", line: payload })
    if (Buffer.byteLength(message, "utf8") > SIDEPULSE_MAX_EVENT_BYTES) return
    for (const target of socketCandidates()) {
      if (await writeEventSocket(target, message)) return
    }
  } catch {
    // Monitoring must never interrupt an OpenCode session.
  }
}

async function appendSidePulseLog(payload) {
  try {
    await mkdir(dirname(SIDEPULSE_LOG), { recursive: true })
    await appendFile(SIDEPULSE_LOG, JSON.stringify(payload) + "\n", "utf8")
  } catch {
    // Monitoring must never interrupt an OpenCode session.
  }
}
function firstString(...values) {
  return values.find((value) => typeof value === "string" && value.length > 0)
}

// v2 events are `{ id, created, type, location: { directory }, data }`. Every
// session-scoped event carries `data.sessionID`; the v1 `properties.info.id`
// shapes are gone.
function sessionId(event) {
  const d = event?.data ?? {}
  return firstString(d.sessionID, d.sessionId, d.session_id)
}

// Task-tool subagents are full OpenCode sessions carrying `parentID`, so
// without this they arrive indistinguishable from top-level work and inflate
// every "how much is running" signal. SidePulse already excludes rows keyed
// `<provider>:agent:<id>`, and it derives that key from `agent_id`, so tagging
// a child session is all it takes to have it treated as a subagent.
//
// v1 read lineage from `session.updated`, which v2 no longer publishes. In v2
// `session.created` (and `session.forked`) carry `parentID` directly. A subagent
// resumed by task_id publishes no `created` in this process, so an unknown
// session is looked up once through `ctx.session.get` and cached.
const parentBySession = new Map()
const lineageLookups = new Map()

function rememberLineage(event) {
  if (event?.type !== "session.created" && event?.type !== "session.forked") return
  const id = sessionId(event)
  if (!id) return
  const parentID = event.data?.parentID
  parentBySession.set(id, typeof parentID === "string" && parentID.length > 0 ? parentID : null)
}

function resolveLineage(ctx, sessionID) {
  if (!sessionID || parentBySession.has(sessionID)) return Promise.resolve()
  let pending = lineageLookups.get(sessionID)
  if (!pending) {
    pending = Promise.resolve()
      .then(() => ctx.session.get({ sessionID }))
      .then((info) => {
        const parentID = info?.parentID ?? info?.data?.parentID
        parentBySession.set(sessionID, typeof parentID === "string" && parentID.length > 0 ? parentID : null)
      })
      .catch(() => {
        // Unknown stays unknown (top-level); see agentIdFor.
      })
      .finally(() => lineageLookups.delete(sessionID))
    lineageLookups.set(sessionID, pending)
  }
  // Never let a slow lookup hold up a hook the session is waiting on.
  return Promise.race([pending, new Promise((resolve) => setTimeout(resolve, SIDEPULSE_SOCKET_TIMEOUT_MS).unref?.())])
}

function agentIdFor(sessionID) {
  // Unknown lineage is treated as top-level. Mislabelling a real session as a
  // subagent would silently drop it from the aggregate, which is the worse
  // failure of the two.
  return parentBySession.get(sessionID) ? sessionID : undefined
}

function parentOf(sessionID) {
  return parentBySession.get(sessionID) || undefined
}

// Errors that must not light the device red.
//
// v1 had to split this by carrier because `session.error` fired for both the
// recoverable and the fatal `ContextOverflowError`. In v2 the terminal outcome
// of a run is a single event: `session.execution.failed` only fires when the run
// actually failed (a recoverable overflow compacts and the run continues, so it
// never reaches it), and it also covers the v1 `StructuredOutputError` case that
// only showed up on the assistant message. So no overflow filter is needed.
//
// `aborted` is the user pressing escape (v1 `MessageAbortedError`). That is an
// instruction, not a failure, and a signal that persists until acknowledged
// must not fire every time somebody interrupts an agent. User interrupts mostly
// arrive as `session.execution.interrupted` anyway; this catches the rest.
const ERROR_IGNORED = new Set(["aborted"])

function isReportableError(error) {
  if (!error) return false
  // An untyped error is still an error; only known-benign types are dropped.
  return typeof error.type !== "string" || !ERROR_IGNORED.has(error.type)
}

// v2 event type -> the v1 event_name vocabulary SidePulse already maps, so the
// receiving side needs no change.
//
// v2 core no longer publishes `session.status` / `session.idle` (they remain in
// the schema for old TUIs only). The run lifecycle replaces them:
// execution.started is busy, retry.scheduled is still work in flight (v1 kept
// retry as busy), and succeeded / interrupted are idle.
//
// Not forwarded: `session.step.failed` (v1 `session.next.step.failed`). A failed
// step is either retried or ends the run, and the run's end already arrives as
// `session.execution.failed`; forwarding both would light red during retries.
// `message.updated` and `session.updated` no longer exist on the v2 stream, and
// the token stream (`session.text.delta` etc.) is never forwarded.
function eventName(event) {
  switch (event?.type) {
    case "session.created":
    case "session.deleted":
    case "permission.asked":
    case "permission.replied":
      return event.type
    case "session.execution.started":
    case "session.retry.scheduled":
      return "session.status"
    case "session.execution.succeeded":
    case "session.execution.interrupted":
      return "session.idle"
    case "session.execution.failed":
      return isReportableError(event.data?.error) ? "session.error" : "session.idle"
    case "session.tool.failed":
      return "session.next.tool.failed"
    default:
      return undefined
  }
}

// The pid of the process that owns these sessions.
//
// A prompt and a failure are questions put to a human, and SidePulse holds
// them on the device until they are answered rather than timing them out.
// That is only safe while somebody can still answer: closing the terminal
// kills this process mid-prompt and no further event is ever sent, so without
// a liveness handle the device would hold a question from a session that no
// longer exists. Read once - it cannot change for the life of the plugin.
const AGENT_PID = typeof process?.pid === "number" ? process.pid : undefined

const ORIGIN = process.env.T3CODE_HOME || process.env.T3_CODE || process.env.T3CODE ||
  process.env.T3_MCP_BEARER_TOKEN
  ? { agent_origin: "T3 Code", agent_origin_kind: "opencode_t3code" }
  : { agent_origin: "OpenCode", agent_origin_kind: "opencode_app" }

async function emit(payload) {
  if (!payload.session_id) return
  if (AGENT_PID !== undefined && payload.agent_pid === undefined) payload.agent_pid = AGENT_PID
  await Promise.all([appendSidePulseLog(payload), notifySidePulse(payload)])
}

function sessionPayload(event_name, session_id, fields) {
  return {
    event_name,
    session_id,
    agent_id: agentIdFor(session_id),
    parent_session_id: parentOf(session_id),
    ...fields,
    timestamp: new Date().toISOString(),
    ...ORIGIN,
  }
}

// v2 plugin shape: a default export with an `id` and a `setup(ctx)` that wires
// subscriptions and hooks and returns a cleanup. v1 exported a bare async
// function returning `{ event, "tool.execute.before", ... }`, which the v2
// loader rejects outright.
export default {
  id: "sidepulse",
  async setup(ctx) {
    // One v2 server serves many directories; prefer the event's own location
    // and fall back to the location the plugin was activated in.
    const fallbackCwd = firstString(ctx.location?.directory)
    const cwdOf = (event) => firstString(event?.location?.directory, fallbackCwd)
    const controller = new AbortController()

    async function handle(event) {
      // Lineage is recorded before the filter so that `session.forked`, which
      // is never forwarded itself, still teaches us who the parents are.
      rememberLineage(event)
      const name = eventName(event)
      if (!name) return
      const session_id = sessionId(event)
      if (event.type !== "session.created" && event.type !== "session.deleted") {
        await resolveLineage(ctx, session_id)
      }
      const d = event.data ?? {}
      await emit(
        sessionPayload(name, session_id, {
          cwd: cwdOf(event),
          tool_name: firstString(d.tool, d.toolName, d.name, d.action),
          message: firstString(d.message, d.error?.message),
        }),
      )
      if (event.type === "session.deleted") parentBySession.delete(session_id)
    }

    // The v2 stream fails (rather than blocking the bus) when a consumer falls
    // behind, so resubscribe until cleanup instead of going silent forever.
    ;(async () => {
      while (!controller.signal.aborted) {
        try {
          for await (const event of ctx.event.subscribe({ signal: controller.signal })) {
            try {
              await handle(event)
            } catch {
              // Monitoring must never interrupt an OpenCode session.
            }
          }
        } catch {
          // Stream ended or overflowed; fall through to resubscribe.
        }
        if (!controller.signal.aborted) await new Promise((resolve) => setTimeout(resolve, 1000))
      }
    })()

    const registrations = []

    // A human actually typing is a different thing from the session becoming
    // busy, which happens constantly and for many reasons. SidePulse uses this to
    // decide that a failure has been seen and dealt with, so it must only fire
    // for real input. v1 inferred it from `message.updated` with role user; v2
    // has a dedicated `session.prompt` hook that runs once per submitted prompt.
    // Reported as a busy-equivalent so the mode is unchanged, but flagged so
    // SidePulse can retire any latched error for this session and its subagents.
    registrations.push(
      await ctx.session.hook("prompt", async (input) => {
        try {
          await resolveLineage(ctx, input?.sessionID)
          await emit(
            sessionPayload("session.status", input?.sessionID, {
              sidepulse_user_prompt: true,
              cwd: fallbackCwd,
            }),
          )
        } catch {
          // Monitoring must never interrupt an OpenCode session.
        }
      }),
    )

    // These are the real tool signals. Without them nothing ever distinguishes
    // "the model is thinking" from "a tool is running", so SidePulse's
    // TOOL_RUNNING mode was unreachable for OpenCode. In v2 they are
    // `ctx.tool.hook("execute.before" | "execute.after")`, still emitted under
    // the v1 names.
    for (const [hook, name] of [
      ["execute.before", "tool.execute.before"],
      ["execute.after", "tool.execute.after"],
    ]) {
      registrations.push(
        await ctx.tool.hook(hook, async (input) => {
          try {
            await emit(sessionPayload(name, input?.sessionID, { cwd: fallbackCwd, tool_name: input?.tool }))
          } catch {
            // Monitoring must never interrupt an OpenCode session.
          }
        }),
      )
    }

    return async () => {
      controller.abort()
      await Promise.allSettled(registrations.map((registration) => registration.dispose()))
    }
  },
}
