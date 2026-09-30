/**
 * A picker for "which mcpx daemon?", in the TUI.
 *
 * ## Why this is a second file
 *
 * A *server* plugin has almost no interface. `client.tui.showToast` and the
 * fixed dialogs are the whole surface; there is no generic dialog and no
 * select over HTTP, in either opencode version. The only interactive channel
 * a v1 server plugin has is a tool whose execution raises a permission
 * prompt, which is a yes/no -- which is why `mcpx_daemon_select` takes an
 * index the agent read out of `mcpx_daemon_status` rather than a path.
 *
 * A real list you arrow through lives in the TUI realm, which is a different
 * plugin with a different API. So: two artifacts, installed separately, and
 * this one is optional. Everything works without it.
 *
 * ## How a choice made here reaches the server plugin
 *
 * It does not, directly: the two run in different realms and share nothing
 * but the filesystem. So this writes the same remember file the server
 * plugin reads at rung 1, under mcpx's state directory, and the server plugin
 * re-reads it when it has gone stale. That is also why the scopes offered
 * here start at "until it stops answering": a session-only choice lives in
 * the server plugin's memory, which this realm cannot touch.
 *
 * ## Install
 *
 *   "plugin": ["./mcpx-tui.tsx"]
 *
 * v1 reads a TUI plugin from a default export of `{ id, tui }`, and a module
 * may export `tui` or `server` but never both -- so this cannot be merged
 * into mcpx-session.ts even if the realms allowed it.
 *
 * ## Typechecking
 *
 * Needs `@opencode-ai/plugin` and its three optional peers, `@opentui/core`,
 * `@opentui/keymap` and `@opentui/solid`. They are peer dependencies of the
 * plugin package, so a checkout that wants to typecheck this file installs
 * them explicitly. At runtime opencode supplies all of it.
 */

import type { TuiPlugin, TuiPluginApi, TuiPluginModule } from "@opencode-ai/plugin/tui"

import {
  DaemonClient,
  ago,
  discover,
  label,
  rank,
  targetKey,
  writeRemembered,
  type Candidate,
} from "./mcpx/daemon.ts"

const id = "mcpx-daemon-picker"

/** What the second dialog offers once a daemon has been picked. */
type Scope = "until-gone" | "indefinite" | "permanent" | "stop-others"

const env = () => process.env as Record<string, string | undefined>

const detail = (c: Candidate): string => {
  const bits: string[] = []
  if (c.startedAt) bits.push(`started ${ago(c.startedAt)}`)
  if (c.servers !== undefined) bits.push(`${c.servers} servers`)
  if (c.sessions) bits.push(`${c.sessions} sessions`)
  if (c.version) bits.push(c.version)
  return bits.join(" · ")
}

/**
 * Apply a choice, and say what happened.
 *
 * Every branch ends in a toast, because a picker that closes silently leaves
 * the user unsure whether anything was set -- and the effect is invisible
 * until the next mcpx call.
 */
const apply = async (api: TuiPluginApi, directory: string, pick: Candidate, scope: Scope): Promise<void> => {
  if (scope === "stop-others") {
    const all = await discover({ directory, env: env() })
    const others = all.candidates.filter((c) => targetKey(c.target) !== targetKey(pick.target))
    const results = await Promise.all(others.map((c) => new DaemonClient(c.target).shutdown()))
    await writeRemembered(env(), directory, pick.target, "indefinite")
    api.ui.toast({
      variant: "success",
      title: "mcpx",
      message: `Using ${label(pick)}. Stopped ${results.filter(Boolean).length} of ${others.length} others.`,
    })
    return
  }
  if (scope === "permanent") {
    const res = await new DaemonClient(pick.target).putSetting("daemon.endpoint", targetKey(pick.target))
    if (res === "ok") {
      api.ui.toast({ variant: "success", title: "mcpx", message: `daemon.endpoint = ${targetKey(pick.target)}` })
      return
    }
    // The settings API may not exist on this daemon. Remember it durably
    // anyway and say what to edit, rather than failing at the last step.
    await writeRemembered(env(), directory, pick.target, "indefinite")
    api.ui.toast({
      variant: "warning",
      title: "mcpx",
      message:
        res === "absent"
          ? "This daemon has no settings API; remembered for this directory instead."
          : "Could not write the setting; remembered for this directory instead.",
    })
    return
  }
  await writeRemembered(env(), directory, pick.target, scope)
  api.ui.toast({
    variant: "success",
    title: "mcpx",
    message:
      scope === "until-gone"
        ? `Using ${label(pick)} until it stops answering.`
        : `Using ${label(pick)} for ${directory}.`,
  })
}

const askScope = (api: TuiPluginApi, directory: string, pick: Candidate): void => {
  api.ui.dialog.replace(() => (
    <api.ui.DialogSelect<Scope>
      title={`Remember ${label(pick)}?`}
      options={[
        {
          title: "until it stops answering",
          value: "until-gone",
          description: "forgotten as soon as that daemon is gone",
        },
        {
          title: "for this directory",
          value: "indefinite",
          description: directory,
        },
        {
          title: "permanently, in the mcpx config",
          value: "permanent",
          description: "writes daemon.endpoint",
        },
        {
          title: "…and stop the other daemons",
          value: "stop-others",
          description: "their sessions end; this is usually what you wanted",
        },
      ]}
      onSelect={(option) => {
        api.ui.dialog.clear()
        void apply(api, directory, pick, option.value).catch((err) =>
          api.ui.toast({ variant: "error", title: "mcpx", message: String(err) }),
        )
      }}
    />
  ))
}

const show = async (api: TuiPluginApi): Promise<void> => {
  const directory = api.state.path.directory
  const found = await discover({ directory, env: env() })
  const list = rank(found.candidates, directory)
  if (list.length === 0) {
    api.ui.toast({
      variant: "info",
      title: "mcpx",
      message: found.target
        ? `Using ${targetKey(found.target)}, which was named explicitly.`
        : "No mcpx daemon is running.",
    })
    return
  }
  api.ui.dialog.replace(() => (
    <api.ui.DialogSelect<Candidate>
      title="mcpx daemon"
      placeholder="filter by project or socket"
      current={list.find((c) => found.target && targetKey(c.target) === targetKey(found.target))}
      options={list.map((c) => ({
        title: label(c),
        value: c,
        // The config path is the thing that actually distinguishes two
        // daemons; everything else is a tie-breaker.
        description: `${c.configPath ?? targetKey(c.target)}${detail(c) ? "  —  " + detail(c) : ""}`,
      }))}
      onSelect={(option) => askScope(api, directory, option.value)}
    />
  ))
}

const tui: TuiPlugin = async (api) => {
  api.keymap.registerLayer({
    commands: [
      {
        name: "mcpx.daemon",
        title: "mcpx: choose daemon",
        category: "mcpx",
        namespace: "palette",
        run() {
          void show(api).catch((err) =>
            api.ui.toast({ variant: "error", title: "mcpx", message: String(err) }),
          )
        },
      },
    ],
  })
}

const plugin: TuiPluginModule = { id, tui }

export default plugin
