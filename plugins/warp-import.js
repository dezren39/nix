// SPDX-License-Identifier: MIT OR Apache-2.0
//
// warp-import — collapse "/new + /warp + /warp-import + /sessions" into one call.
//
// WHAT IT DOES
//   1. Resolves a target session by id or case-insensitive title search.
//   2. Creates a workspace (git worktree) and takes its directory.
//   3. Runs `opencode export <id>` then `opencode import <file>` with cwd set to
//      that directory, which RELOCATES the session there.
//   4. Calls POST /tui/select-session to move the TUI into the session.
//
// USER-INITIATED ONLY
//   This tool moves a session and repoints the user's TUI. It must never be
//   called speculatively. The tool description repeats this, but a description
//   is only a hint to the model. For a rule the model cannot ignore, add this to
//   opencode.jsonc (this plugin is NOT listed in the `plugin` array — it is
//   auto-discovered from ~/.config/opencode/plugins/*.js):
//
//     "permission": { "warp_import": "ask" }
//
//   `"ask"` => prompt on every call. Config string values become a rule
//   { permission: "warp_import", action: "ask", pattern: "*" } via
//   Permission.fromConfig (permission/index.ts:186-198), matched by
//   Permission.evaluate (permission/index.ts:28-36) and enforced in
//   Permission.ask (permission/index.ts:67-107).
//
//   IMPORTANT: unlike built-in tools, plugin tools are NOT auto-gated by the
//   host. The registry loop at session/tools.ts:92-106 calls item.execute
//   without a `permission: item.id` check, and registry.ts fromPlugin
//   (registry.ts:125-181) only forwards `ask` into the plugin context. So this
//   plugin calls ctx.ask({ permission: "warp_import", ... }) itself — that call
//   is what makes the "ask" rule real.
//
//     "permission": { "warp_import": "deny" }
//
//   `"deny"` with the implicit "*" pattern removes the tool from the model's
//   tool list entirely (Permission.disabled permission/index.ts:204-214, applied
//   by resolveTools in session/llm/request.ts:208-214).
//
// CAVEAT THIS TOOL ALWAYS REPORTS
//   `opencode import` is an upsert on the session id. The session is MOVED, not
//   copied, and the imported row has zero `event` rows — so the session can
//   never be warped again afterwards.
//
// SAFETY
//   No network calls, no telemetry. The only file written is a temp export JSON,
//   removed in a finally block. Everything is wrapped so the plugin can never
//   throw at load time and never emits an unhandled rejection.

import { spawn } from "node:child_process"
import { createWriteStream } from "node:fs"
import { mkdtemp, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"

const TOOL_ID = "warp_import"
const RECENT_LIMIT = 10

// ---------------------------------------------------------------------------
// Pure helpers (exported for tests; safe because the default export below uses
// the v1 plugin module shape, so the loader reads only `default.server` and
// never treats these as plugin factories — plugin/shared.ts:272-304 and
// plugin/index.ts:113-125).
// ---------------------------------------------------------------------------

function title(session) {
  const value = session && session.title
  return typeof value === "string" ? value : ""
}

function stamp(session) {
  const value = session && (session.updated ?? session.created)
  return typeof value === "number" && Number.isFinite(value) ? value : 0
}

function byRecent(a, b) {
  return stamp(b) - stamp(a)
}

/** `updated`/`created` are epoch milliseconds. */
export function describeSession(session) {
  const when = stamp(session)
  const iso = when > 0 ? new Date(when).toISOString() : "unknown"
  const dir = session && session.directory ? ` [${session.directory}]` : ""
  return `${session && session.id} — ${title(session) || "(untitled)"} — ${iso}${dir}`
}

/**
 * Resolve a session from `opencode session list --format json` shaped data.
 * Never guesses: multiple title hits return "ambiguous" with the candidates.
 */
export function resolveSession(sessions, selector) {
  const list = Array.isArray(sessions) ? sessions.filter(Boolean) : []
  const recent = list.slice().sort(byRecent).slice(0, RECENT_LIMIT)
  if (list.length === 0) return { status: "empty", candidates: [] }

  const term = typeof selector === "string" ? selector.trim() : ""
  if (!term) {
    return { status: "none", candidates: recent, reason: "No session selector was provided." }
  }

  const exact = list.find((item) => item && item.id === term)
  if (exact) return { status: "ok", session: exact, matchedBy: "id" }

  if (/^ses_/i.test(term)) {
    return { status: "none", candidates: recent, reason: `No session has id "${term}".` }
  }

  const needle = term.toLowerCase()
  const matches = list.filter((item) => title(item).toLowerCase().includes(needle)).sort(byRecent)
  if (matches.length === 1) return { status: "ok", session: matches[0], matchedBy: "title" }
  if (matches.length > 1) return { status: "ambiguous", candidates: matches.slice(0, RECENT_LIMIT) }
  return { status: "none", candidates: recent, reason: `No session title matches "${term}".` }
}

function errorText(value) {
  if (!value) return "unknown error"
  if (typeof value === "string") return value
  if (value instanceof Error) return value.message || String(value)
  const data = value.data
  if (data && typeof data.message === "string") return data.message
  if (typeof value.message === "string") return value.message
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

// The generated SDK (createOpencodeClient -> OpencodeClient, sdk.gen.ts:1157)
// exposes `tui` and `session` but has NO selectSession and NO workspace group at
// this rev. Both routes exist on the server (TuiApi/WorkspaceApi are mounted
// unconditionally in InstanceHttpApi, httpapi/api.ts:75-76), so we post to them
// through the SDK's underlying hey-api client, which already carries baseUrl,
// auth headers and interceptors. `_client` is only `protected` in TypeScript; at
// runtime it is a plain property (sdk.gen.ts:223-231).
function lowLevel(client) {
  if (!client || typeof client !== "object") return undefined
  for (const key of ["tui", "session", "app", "project", "config"]) {
    const group = client[key]
    const inner = group && group._client
    if (inner && typeof inner.post === "function") return inner
  }
  return undefined
}

async function postJson(env, path, body) {
  const inner = lowLevel(env.client)
  if (inner) {
    const res = await inner.post({ url: path, body, headers: { "Content-Type": "application/json" } })
    if (res && res.error) throw new Error(errorText(res.error))
    return res ? res.data : undefined
  }

  if (!env.serverUrl) throw new Error("no SDK client and no serverUrl available")
  const res = await fetch(new URL(path, String(env.serverUrl)), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${path} responded ${res.status} ${res.statusText}`)
  // `warp` succeeds with 204 No Content.
  return await res.json().catch(() => undefined)
}

async function listSessions(env) {
  const res = await env.client.session.list() // sdk.gen.ts:435 -> GET /session
  if (res && res.error) throw new Error(errorText(res.error))
  const data = res && "data" in res ? res.data : res
  return Array.isArray(data) ? data : []
}

// POST /experimental/workspace (httpapi/groups/workspace.ts:73-84). Payload is
// Workspace.CreateInput minus projectID (control-plane/workspace.ts:62-69).
// Returns Workspace.Info, whose `directory` the worktree adapter fills in
// (control-plane/adapters/worktree.ts:31-43).
async function createWorkspace(env, branch) {
  const info = await postJson(env, "/experimental/workspace", {
    type: "worktree",
    branch: branch ? String(branch) : null,
  })
  const dir = info && info.directory
  if (!dir || typeof dir !== "string") {
    throw new Error("workspace was created but returned no directory")
  }
  return info
}

// POST /tui/select-session (httpapi/groups/tui.ts:47,163-174);
// payload { sessionID } per sdk v2 types.gen.ts:2648-2656.
async function selectSession(env, sessionID) {
  await postJson(env, "/tui/select-session", { sessionID })
}

// ---------------------------------------------------------------------------
// CLI (export writes JSON to stdout; import takes a FILE PATH, so a temp file is
// mandatory — you cannot pipe one into the other)
// ---------------------------------------------------------------------------

function bin() {
  return process.env.OPENCODE_BIN || "opencode"
}

function run(args, options) {
  const opts = options || {}
  return new Promise((resolve, reject) => {
    let child
    try {
      child = spawn(bin(), args, { cwd: opts.cwd, stdio: ["ignore", opts.stdout ? "pipe" : "ignore", "pipe"] })
    } catch (err) {
      reject(err)
      return
    }

    let stderr = ""
    let settled = false
    // When stdout is redirected we must wait for BOTH the process to exit and
    // the file stream to flush, otherwise a fast process can close its stream
    // before the exit code is known and nothing would ever settle.
    let pendingSink = Boolean(opts.stdout)
    let exit

    const fail = (err) => {
      if (settled) return
      settled = true
      reject(err)
    }
    const maybeResolve = () => {
      if (settled || pendingSink || exit === undefined) return
      settled = true
      if (exit !== 0) {
        reject(new Error(`\`${bin()} ${args.join(" ")}\` exited ${exit}${stderr ? `: ${stderr.trim()}` : ""}`))
        return
      }
      resolve(stderr)
    }

    if (child.stderr) {
      child.stderr.setEncoding("utf8")
      child.stderr.on("data", (chunk) => {
        if (stderr.length < 8192) stderr += chunk
      })
    }

    if (opts.stdout && child.stdout) {
      const sink = createWriteStream(opts.stdout)
      sink.on("error", fail)
      child.stdout.on("error", fail)
      sink.on("close", () => {
        pendingSink = false
        maybeResolve()
      })
      child.stdout.pipe(sink)
    }

    child.on("error", fail)
    child.on("close", (code) => {
      exit = code === null ? 1 : code
      maybeResolve()
    })
  })
}

async function relocate(sessionID, targetDir, sanitize) {
  const dir = await mkdtemp(join(tmpdir(), "warp-import-"))
  const file = join(dir, `${sessionID}.json`)
  try {
    const args = ["export", sessionID]
    if (sanitize) args.push("--sanitize")
    await run(args, { stdout: file })
    await run(["import", file], { cwd: targetDir })
  } finally {
    await rm(dir, { recursive: true, force: true }).catch(() => {})
  }
}

// ---------------------------------------------------------------------------
// Tool
// ---------------------------------------------------------------------------

const DESCRIPTION = [
  "USER-INITIATED ONLY. Never call this tool unless the user explicitly asked for it in this turn,",
  "by running /warp-import or by directly asking to move a session into a worktree. Do not call it to",
  "explore, to recover context, or because it seems helpful. If you are not certain the user asked, do not call it.",
  "",
  "Creates a git-worktree workspace, MOVES an existing session into it (export + import), and switches the TUI to it.",
  "This replaces the manual /new -> /warp -> /warp-import -> /sessions sequence.",
  "",
  "Destructive caveats you must relay to the user: the session is relocated, not copied (its old directory loses it),",
  "and the imported session has no event history, so it can never be warped again.",
].join(" ")

export function buildTool(env) {
  return {
    description: DESCRIPTION,
    // NOTE: these are plain JSON Schema fragments, not Zod. The host converts
    // them with legacyJsonSchema (registry.ts:363-372), which marks EVERY
    // property as required. So the optional inputs are typed nullable and
    // documented with an explicit "pass null/false" instruction instead of
    // being omitted.
    args: {
      session: {
        type: "string",
        description:
          "A full session id (ses_...) or a term matched case-insensitively against session titles. Ambiguous terms are rejected with a candidate list rather than guessed.",
      },
      branch: {
        type: ["string", "null"],
        description: "Git branch name for the new worktree. Pass null to let opencode generate one.",
      },
      use_current_directory: {
        type: "boolean",
        description:
          "Pass false normally. Pass true only when the user is already in the worktree they want, to skip creating a workspace and import into the current directory instead.",
      },
    },
    async execute(args, ctx) {
      try {
        return await execute(env, args || {}, ctx || {})
      } catch (err) {
        // Never let anything escape as an unhandled rejection or a host defect.
        return `warp_import failed: ${errorText(err)}`
      }
    },
  }
}

async function execute(env, args, ctx) {
  // Make the documented `"permission": { "warp_import": "ask" }` rule actually
  // bite. Without this the host does not gate plugin tools at all.
  if (typeof ctx.ask === "function") {
    try {
      await ctx.ask({
        permission: TOOL_ID,
        patterns: ["*"],
        always: ["*"],
        metadata: { session: args.session ?? null, branch: args.branch ?? null },
      })
    } catch (err) {
      return `warp_import was not permitted: ${errorText(err)}`
    }
  }

  // 1. Resolve the target session.
  let sessions
  try {
    sessions = await listSessions(env)
  } catch (err) {
    return `warp_import could not list sessions: ${errorText(err)}`
  }

  const found = resolveSession(sessions, args.session)
  if (found.status !== "ok") {
    const lines = found.candidates.map((item) => `  - ${describeSession(item)}`)
    const head =
      found.status === "ambiguous"
        ? `"${args.session}" matches ${found.candidates.length} sessions. Ask the user which one, then call again with the exact id.`
        : found.status === "empty"
          ? "No sessions exist in this project."
          : `${found.reason} Ask the user which session they meant, then call again with the exact id.`
    return [head, lines.length ? "\nCandidates (most recent first):" : "", ...lines].filter(Boolean).join("\n")
  }

  const session = found.session
  const fallbackDir = ctx.directory || ctx.worktree || env.directory
  const notes = []

  // 2. Get a target directory.
  let targetDir
  let workspace
  if (args.use_current_directory) {
    targetDir = fallbackDir
    notes.push("Skipped workspace creation (use_current_directory was set); imported into the current directory.")
  } else {
    try {
      workspace = await createWorkspace(env, args.branch)
      targetDir = workspace.directory
    } catch (err) {
      // Degrade to today's behaviour rather than failing outright.
      targetDir = fallbackDir
      notes.push(
        `FALLBACK: workspace creation failed (${errorText(err)}). No new worktree was made. ` +
          `Fell back to a plain export+import into the current directory, which is exactly what ` +
          `the manual /warp-import command does.`,
      )
    }
  }

  if (!targetDir) {
    return "warp_import aborted: could not determine a target directory, and no workspace was created. Nothing was changed."
  }

  // 3. Move the session.
  try {
    await relocate(session.id, targetDir, args.sanitize === true)
  } catch (err) {
    return [
      `warp_import failed while moving the session: ${errorText(err)}`,
      workspace ? `A workspace was created at ${targetDir} and was left in place.` : "",
      "The session was not moved.",
    ]
      .filter(Boolean)
      .join("\n")
  }

  // 4. Switch the TUI. A failure here is cosmetic — the move already happened.
  let switched = true
  try {
    await selectSession(env, session.id)
  } catch (err) {
    switched = false
    notes.push(`Could not switch the TUI automatically (${errorText(err)}). Use /sessions to open it manually.`)
  }

  const from = session.directory && session.directory !== targetDir ? session.directory : null
  return [
    `Moved session ${session.id} — "${title(session) || "(untitled)"}"`,
    from ? `  from: ${from}` : null,
    `    to: ${targetDir}`,
    workspace ? `  workspace: ${workspace.id} (worktree${workspace.branch ? `, branch ${workspace.branch}` : ""})` : null,
    switched ? "  TUI switched to this session." : null,
    "",
    "Warning: this was a MOVE, not a copy — the old location no longer has this session.",
    "Warning: the imported session has no event history, so it can never be warped again.",
    ...(notes.length ? ["", ...notes] : []),
  ]
    .filter((line) => line !== null)
    .join("\n")
}

// ---------------------------------------------------------------------------
// Plugin entrypoint
// ---------------------------------------------------------------------------
//
// v1 plugin module shape: a default-exported object with `id` and `server`.
// `id` is mandatory for file-sourced plugins (plugin/shared.ts:306-317). Using
// this shape means the loader reads only `default.server` and ignores the named
// exports above (plugin/index.ts:113-118) — with the legacy shape every export
// would be invoked as a plugin factory.
export default {
  id: "warp-import",
  async server(input) {
    try {
      const env = {
        client: input && input.client,
        serverUrl: input && input.serverUrl,
        directory: input && input.directory,
        worktree: input && input.worktree,
      }
      if (!env.client || !env.client.session) return {}
      return { tool: { [TOOL_ID]: buildTool(env) } }
    } catch {
      // A broken plugin must never break session startup.
      return {}
    }
  },
}
