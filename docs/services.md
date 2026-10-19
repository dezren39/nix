# The long-running services: OpenChamber, OpenCode, mcpx

How the three services this flake keeps running are started, replaced on a
switch, checked and debugged on this Mac. This page describes mechanisms, which
change only when the code does. Point-in-time findings, with their dates, are in
[`2026-10-19-reliability-review.md`](2026-10-19-reliability-review.md); the MCP
servers behind mcpx are in [`mcp.md`](mcp.md).

Written 2026-10-19. Upstream file:line references are to the revisions pinned in
`flake.lock` then: developing-today/openchamber `94b7e56f9`, developing-today/opencode
`f9aa962`, developing-today/code (`mcpx-src`) `7274889`.

## What runs

| Service | launchd label | Program | Listens | Logs |
|---|---|---|---|---|
| OpenChamber | `org.nixos.openchamber` (`openchamber.nix`) | `~/.local/state/openchamber/start`, a link to the start script in the store | `127.0.0.1:3000` | `~/Library/Logs/openchamber.log` |
| OpenCode (the one OpenChamber manages) | none: a child of OpenChamber, left running across OpenChamber restarts (`AbandonProcessGroup = true`) | `~/.local/state/openchamber/bin/opencode`, a link to the wrapped fork build | a random `127.0.0.1` port | `~/.local/share/opencode/log/` |
| mcpx | `org.nixos.mcpx` (`configuration.nix`) | a store wrapper (`mcpx-agent`) that runs `mcpx --config ~/.config/mcpx/config.json stop`, then `exec mcpx daemon --config …` | a unix socket `~/.local/state/mcpx/daemon-<hash>.sock`, plus a loopback HTTP port | `/tmp/mcpx.err.log`, `~/.local/state/mcpx/logs/mcpx-<date>.jsonl` |

The desktop app (`/Applications/Nix Apps/OpenChamber.app`) is only a window onto
the server at port 3000 (`OPENCHAMBER_SERVER_URL`, `OPENCHAMBER_SKIP_LOCAL_SERVER=1`
in `launchd.user.envVariables`). A second app at `/Applications/OpenChamber.app`
(version 2.1.0, installed by hand) is not managed by nix and goes stale.

The terminal UI runs its own shared OpenCode service (`opencode serve --service`,
state in `~/.local/state/opencode/service.json`), separate from OpenChamber's.
Both open the one database `~/.local/share/opencode/opencode.db` (`OPENCODE_DB`,
forced by the wrapper in `flake.nix`).

## How a switch reaches each one

`just switch` builds as you, activates as root, then checks the services
(`simple-rebuild.sh`). Inside activation, order matters:

1. **nix-darwin's launchd step** (about line 862 of the activate script):
   for each user agent whose generated plist differs from the installed one, it
   runs `launchctl unload`, copies the new plist and `launchctl load -w`
   (nix-darwin `modules/system/launchd.nix`). Agents that are no longer defined
   are unloaded and their plists deleted.
2. **home-manager** (about line 1100) re-links the files it manages, then runs
   its activation steps in DAG order.

So an agent whose plist changed is restarted *before* home-manager updates its
files. Anything an agent reads from a home-manager link is still the previous
generation's at that moment.

### OpenChamber

Its plist names the stable link `~/.local/state/openchamber/start`, so nix-darwin
almost never restarts it. A plist change (as when `ExitTimeOut` was added) does,
once. `home.activation.openchamberDeploy` decides instead:

- The **deploy key** is `<start script store path>:<sha256 of the UI password
  file>`, compared with `~/.local/state/openchamber/deployed`.
- Key changed (new OpenChamber, new start script, or new password): `launchctl
  kickstart -k`. OpenChamber restarts and adopts the running OpenCode.
- Key unchanged but the OpenCode link target changed: `launchctl kill SIGHUP`,
  and OpenChamber hands its OpenCode over to the new binary in place.
- After linking, it checks that `start` resolves to an executable and fails the
  activation otherwise; launchd's only signal for a bad link is exit 78.

The start script puts mcpx's store path on OpenChamber's PATH, so **every mcpx
update also changes the start script and restarts OpenChamber**.

### OpenCode (handover)

When OpenChamber starts or reloads, it decides between adopting the recorded
OpenCode and handing over to a new one, using
`~/.config/openchamber/managed-opencode/state.json` (pid, port, binary, args,
launch fingerprint, password, agent tool token). On a handover it starts the
new OpenCode on its own port, sends new work there once it is healthy, and lets
the old one finish running turns and event streams for up to 30 minutes
(`DEFAULT_OPENCODE_DRAIN_GRACE_MS`, `draining-opencode.js`). If the new one
fails its health check, the old one keeps serving.

`openchamber.log` records why: `managed launch: handoff (binary-changed)`,
`handoff (config-refresh)` (OpenChamber rewrote OpenCode's config, for example
after model routes were saved) and `start (no-state)`.

### mcpx

- **A new mcpx build** changes the wrapper's store path, so the plist changes and
  nix-darwin restarts the agent. There is no in-place upgrade on macOS (see below).
- **A config change** is picked up by the running daemon in place: it polls its
  config files on each reap tick (`daemon.watchConfig`, `daemon.reapInterval`
  30 s) and reloads without restarting servers whose definition did not change.
- **The config file** `~/.config/mcpx/config.json` is a real file written by
  `home.activation.mcpxConfig` (`homeUser.nix`), not a home-manager link, because
  of how mcpx names its socket:

  The socket is `<state dir>/daemon-<hash>.sock`, the hash taken over the state
  directory and the **symlink-resolved** path of every config file in use
  (`FingerprintConfig`, `internal/daemon/paths.go`). A home-manager link resolves
  to a store path that changes with the content, so each config change gave a new
  socket name while a daemon started earlier stayed on the old one. At a fixed
  real path the name never changes. Upstream: developing-today/code#368.

  The step rewrites the file only when its content differs, and restarts the
  agent only when the file was absent or still a link (a first switch onto this
  layout, or a fresh machine), the only cases where the socket name changes.

## Only one of each

- **mcpx.** Only the launchd agent should run a daemon for the user config.
  - `daemon.autostart = false` in `mcpx.config.nix`. With it on, any command
    that found no daemon started a detached one (`daemon --detached --idle-exit
    4h`) that could hold the socket for hours. Now such a command fails with
    "disabled by daemon.autostart" and works again once the agent is up. It is a
    client setting, read by every CLI from the user config. A project
    `.mcpx.json` could turn it back on; there are none today.
  - The OpenCode fork's mcpx plugin starts `mcpx daemon` itself when
    `/v1/health` does not answer and ignores that setting
    (`packages/core/src/tool/plugin/mcpx.ts`, `ensureDaemon`;
    developing-today/opencode#134).
  - So the agent's wrapper stops whatever answers on its socket before starting.
    launchd never runs two copies of one job, so that process is a stray.
  - A daemon that finds its socket held exits 1, "a daemon is already
    listening", and launchd respawns it every 10 s. That is the loop of
    2026-10-19.
- **OpenChamber** refuses to start when port 3000 is taken.
- **OpenCode.** OpenChamber manages exactly one OpenCode and adopts it across
  restarts. A stray OpenCode is not cleaned up by anything.

## Shutdown and launchd's exit timeout

launchd gives these agents 5 s between SIGTERM and SIGKILL by default
(`launchctl print` shows `exit timeout = 5`). That was shorter than both stops,
so both are now set explicitly:

| Agent | Stop takes up to | `ExitTimeOut` |
|---|---|---|
| mcpx | 10 s: HTTP drain (`http.shutdownGrace`, 5 s) and MCP server shutdown, both inside `limits.execDrainTimeout` (10 s; `stopServing`, `internal/daemon/server.go`) | 20 |
| OpenChamber | 10 s (`SHUTDOWN_TIMEOUT`, `packages/web/server/index.js:185`); OpenCode is left running | 15 |

## Graceful replacement, and how developing-today/code does it with systemd

| Service | When a switch changes it | Graceful here? |
|---|---|---|
| OpenCode | Handover through OpenChamber (above) | Yes, running turns finish |
| OpenChamber | Restarted; it adopts the running OpenCode | Partly. Sessions survive. Browser, desktop and terminal connections drop, and port 3000 is closed for about 10 s |
| mcpx | Restarted; every MCP server process restarts | No. Stateful servers, such as browser sessions, are lost. Config-only changes are reloaded in place |

developing-today/code's home-manager config (`home/common/default.nix`) runs the
same services as systemd user units. A review of it on 2026-10-19 (not
re-checked line by line since) found these differences:

| Mechanism there | What it buys | Here |
|---|---|---|
| `openchamber.socket` (socket activation; OpenChamber reads `LISTEN_FDS`) | Port 3000 stays open across restarts; connections wait instead of being refused | Absent. launchd's `Sockets` would need a small shim that calls `launch_activate_socket` and passes the fd as `LISTEN_FDS`. Not built |
| mcpx `Type=notify`, `NotifyAccess=all`; `ExecReload` runs `mcpx upgrade` | New binary with no dropped call: sockets, connections and MCP server processes are passed to a successor that takes over systemd's main PID | Not possible as is. launchd cannot adopt a new main PID, and the handover code reads Linux's `/proc/<pid>/stat` (`mcpclient/handoff.go`). On macOS all 12 takeover tests fail (7 on `/proc`, 5 "takeover: EOF"). See `flake.nix` |
| `X-RestartIfChanged=false` plus a deploy key for mcpx | A switch touches mcpx only when its binary or environment changed | Partly: a plist change restarts it; config changes reload in place |
| OpenChamber self-handoff (SIGURG) | OpenChamber restart with no gap | Off (`OPENCHAMBER_SELF_HANDOFF=0`): the successor would outlive the pid launchd tracks, and KeepAlive would start a second server |
| `KillMode=process` | Stopping OpenChamber leaves OpenCode running | Same, via `AbandonProcessGroup = true` |
| Deploy key for OpenChamber (restart vs reload vs nothing) | — | Same pattern; here it also covers the UI password |

A truly graceful mcpx upgrade on macOS needs work in developing-today/code. Replace
the `/proc` start-time check (for example with `sysctl kern.proc.pid`), then make
an exec-in-place upgrade keep the sockets and server pipes so launchd's pid
survives.

## The post-switch health check

`check_services` in `simple-rebuild.sh` runs last in `phase_activate`, after the
generation is recorded. For each entry in `health_services`, the agent must keep
the same pid across two samples 3 s apart, and its check must pass:

- `org.nixos.openchamber`: `http://127.0.0.1:3000/` answers with any status. 401
  counts as up, since the UI password is required.
- `org.nixos.mcpx`: `cd ~ && mcpx ls` exits 0. With autostart off this succeeds
  only if a daemon is on the socket the CLI computes, so a daemon on a stale
  socket fails it.

All agents share `HEALTH_TIMEOUT` seconds (default 60). On failure it prints
launchd's state, program and last exit code plus the tail of the agent's stderr
log, and `just switch` exits non-zero. Rollbacks skip it. One-shot agents
(`KeepAlive = false`, such as colima) do not belong in the list.

A passing `just build` tests none of this. Activation scripts, launchd agents and
this check only run on `just switch`, which needs sudo and the user.

## Debugging

- **Read launchd's view first:** `launchctl print gui/$(id -u)/<label>` (state,
  program, runs, last exit code), then the agent's stderr log, then the app's log.
- **Exit 78 (`EX_CONFIG`) and nothing new in the app log:** launchd could not run
  the program, usually because the path in `program =` does not exist. launchd then
  parks the job until the file appears (an `event triggers … Executable` block in
  the print), and `launchctl kickstart` hangs. Fix the path, then `launchctl
  bootout gui/$(id -u)/<label>` and `launchctl bootstrap gui/$(id -u)
  ~/Library/LaunchAgents/<label>.plist`.
- **mcpx exiting 1 every ~10 s with "a daemon is already listening on …sock":**
  another process holds the socket. `lsof -t <socket>` shows it. `cd ~ && mcpx
  stop`, then `launchctl kickstart gui/$(id -u)/org.nixos.mcpx`. After the
  wrapper change the next agent start clears it by itself.
- **The CLI says no daemon while the agent is running:** the agent is on a
  different socket than the CLI computes. Compare `lsof -p <agent pid> | grep
  sock` with the name the CLI reports (`mcpx status`). `mcpx status` exits 0 even
  with no daemon; `mcpx ls` does not.
- **OpenChamber "Unreachable" / connection refused on 3000:** the agent is not
  running; start with `launchctl print`. 401s are the UI password, not a fault.

## Credentials these services hold

- **OpenChamber UI password:** sops (`secrets/openchamber.yaml`), decrypted by
  sops-nix to `~/.config/openchamber/ui-password`. It is required before
  OpenChamber will serve a public tunnel, and every client signs in once. It has
  no trailing newline, so zsh prints a `%` after it; copy it with `sops -d
  --extract '["openchamber_ui_password"]' secrets/openchamber.yaml | pbcopy`.
  Changing it restarts OpenChamber (deploy key).
- **Managed OpenCode password and OpenChamber's agent tool token:** in
  `~/.config/openchamber/managed-opencode/state.json`. The server only listens on
  loopback, and the token is only accepted from same-machine addresses
  (`agent-tool/runtime.js`). Both are **reused** across OpenChamber restarts on
  purpose, so the running OpenCode can be adopted
  (`ensureLocalOpenCodeServerPassword({ reusePassword })` in
  `auth-state-runtime.js`; `createChildEnv({ reuseToken })` in
  `agent-tool/runtime.js`). A restart does not rotate them.
  - To rotate (worked out from the code, not yet tested):
    1. `launchctl bootout gui/$(id -u)/org.nixos.openchamber`.
    2. Stop the OpenCode whose pid is in that file. This interrupts running turns.
    3. Delete the file.
    4. `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/org.nixos.openchamber.plist`;
       OpenChamber starts a new OpenCode with new values.
  - Never print that file. Read only its key names.
