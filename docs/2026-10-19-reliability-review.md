# Reliability review, 2026-10-19

A dated record of one long working session: what broke, why, what was changed,
what was found and left open, and what turned out to be wrong along the way.
**It describes the state on 2026-10-19 at commit `e509884`, before the next
`just switch` applied it (generation 304 was active).** It is a snapshot, not a
status page. The durable mechanisms are in [`services.md`](services.md) and
[`mcp.md`](mcp.md).

Facts marked *(audit)* come from three read-only investigations run that day.
They covered OpenChamber, OpenCode and mcpx reliability; the MCP servers and their
credentials; and how developing-today/code replaces services under systemd.
Everything else was checked directly while writing this page.

## Changes made

| Commit | What |
|---|---|
| `365ac99` | The desktop app bundles the real OpenCode, not the deploy link. Its dangling link made nix-darwin's rsync into `/Applications/Nix Apps` fail ("symlink has no referent", exit 23) |
| developing-today/openchamber `94b7e56f9`, pinned here in `3402586` | The desktop app's bundle links `packages/` next to `node_modules/`, so `@openchamber/web` resolves. The app crashed at launch with `ERR_MODULE_NOT_FOUND` |
| `a0c0cdf`, `8f05b36`, `c90cd1b`, `e2d8f39` | Temporary darwin skips for mcpx tests that only failed in the nix sandbox, each removed once fixed upstream (#428, #434, #435, #445) |
| `50d22f1` | `simple-rebuild.sh`: fail the build phase when `nix build` exits 0 but produces no `./result` |
| `5a856f6` | `openchamber.nix`: link the start script, not the deploy key, and fail activation if the link is not executable |
| `f940ba9` | `just switch` ends with a health check of the long-running agents; the nix-config skill documents activation testing and launchd debugging |
| `d65df61` | Removed lootbox, the second MCP gateway. `mcpx.config.nix` became the only server list; `mcp-remote` and `chrome-devtools-mcp` are now built from a lockfile |
| `e220322` | Documented the stray `%` when reading the sops UI password |
| `ecca0a1` | mcpx: config as a real file at a fixed path, `daemon.autostart = false`, an agent wrapper that clears strays, a CLI-based health check, `ExitTimeOut` on mcpx and OpenChamber |
| `e509884` | Docs: the live OpenCode config is not the repo's `opencode-global.jsonc` |

Upstream issues filed: developing-today/code#368 (mcpx socket name follows the
symlink-resolved config path) and developing-today/opencode#134 (OpenCode's mcpx
plugin starts its own daemon regardless of `daemon.autostart`).

Outside the repo, the same day:
- lootbox was unloaded and its binary, UI, logs and leftover folders deleted.
  `~/.lootbox` (14 browser-automation scripts written by hand) was moved to the
  Trash.
- The `lootbox.md` line was removed from the live OpenCode config.
- Six Renovate PRs that only touched lootbox's UI were closed.
- `nix store gc` freed 22.9 GiB (10,438 store paths).

## Incidents and their causes

### mcpx restarted 841 times (launchd: 842 runs)

**Symptom.** From 11:47 to 14:09, launchd's mcpx daemon exited 1 about every
10 s: "a daemon is already listening on ~/.local/state/mcpx/daemon-541cdd605dd1.sock".

**Cause.** mcpx names its socket from the symlink-resolved path of its config, and
the config was a home-manager link into the nix store.

- Socket name per generation *(audit)*: generations 290-295 gave `3489ca4d2ea0`,
  296 gave `539ba2d32a06`, 297-304 gave `541cdd605dd1`.
- nix-darwin restarts launchd agents before home-manager re-links files (activate
  script lines ~862 and ~1100).

Timeline, from `/tmp/mcpx.err.log` and `~/.local/state/mcpx/logs/`:

| Time | Event |
|---|---|
| 2026-10-18 20:58 | Generation 298's activation restarts the agent. It reads the still-old link and listens on `daemon-539ba2d32a06.sock` |
| 04:48:10 | A command computes the current name, finds no daemon and autostarts a detached one (`daemon --detached --idle-exit 4h`, mcpx build `hnqq7r7w…`) on `daemon-541cdd605dd1.sock`. Being used, it never idles out |
| 11:47 | Generation 302 brings a new mcpx (`p8wcs9ml…`); launchd's daemon restarts on the current name and finds it held |
| 14:09 | `mcpx stop` ends the stray and launchd's daemon (pid 73031) takes the socket |

Meanwhile, clients ran on the stray's older build.

**Fix** (`ecca0a1`):
- the config is a real file at a fixed path;
- commands no longer start daemons;
- the agent's wrapper stops anything on its socket before starting;
- the health check fails when the CLI cannot reach the daemon.

Each part was reproduced and tested with real mcpx binaries on scratch state
directories. A symlinked config re-pointed to another file left the CLI unable to
reach the running daemon (exit 1). A real file rewritten in place kept the socket
and was hot-reloaded.

### OpenChamber unreachable

**Symptom.** The desktop app showed the local instance as Unreachable, and every
request to `127.0.0.1:3000` was refused.

**Cause.** `493be7d` added the UI password's hash to the deploy key
(`<script>:<sha256>`), and the same string was used as the target of the `start`
link. The link named a file that did not exist. launchd failed every spawn with
exit 78 (`EX_CONFIG`) and wrote nothing to `openchamber.log`. It then parked the
job waiting for the file, so `launchctl kickstart` hung.

**Fix.** `5a856f6`. Recovery: re-point the link, then `launchctl bootout` and
`bootstrap`.

### Desktop app: activation failure, then crash at launch

- **rsync failure.** nix-darwin copies apps into `/Applications/Nix Apps` with
  `rsync --copy-unsafe-links`. The app bundled `opencode-cli/opencode` as a link to
  `~/.local/state/openchamber/bin/opencode`, which home-manager creates only later
  in the same activation, so rsync failed. Fixed in `365ac99`.
- **Crash at launch.** `early-startup.mjs` imports `@openchamber/web`, a workspace
  link to `../../packages/web`. The darwin packaging linked only `node_modules`
  into the app, so the link dangled. The fork never updated its packaging when the
  import was added (`e601f1627`). Fixed in the fork.

### "Am I running the right binary?"

The routing, plans, cost-tier and usage features were present on the server. They
live under Settings → Routing and Settings → Usage, with live data such as routing
attempts per model. Two things made them look missing:

- the nix-built desktop app was crashing at launch (above);
- a second, hand-installed `/Applications/OpenChamber.app` (2.1.0) exists outside nix.

### `nix build` exited 0 without producing a system

nix logged "error (ignored): SQLite database …/eval-cache-v6/….sqlite is busy",
exited 0, and did not create `./result`. `simple-rebuild.sh` then committed a
marker naming the string `result` as the built store path, and failed three steps
later with an unrelated-looking "no ./result to activate". `50d22f1` checks for
`./result/sw/bin/darwin-rebuild` right after the build.

### mcpx tests failing only in the nix sandbox

These failures were sandbox artefacts, not product bugs:

- **Timing.** Tests asserted a child process's marker or pid file the instant a
  call returned (#428, #435, #445).
- **Socket path length.** A test bound a unix socket under the sandbox's long
  `TMPDIR`, past Darwin's 104-byte limit (#434).

All four were fixed upstream within hours of being filed.

### The UI password's trailing `%`

The secret has no trailing newline, so zsh prints a reverse-video `%` after it,
which got copied with it. The stored value ends in neither `%` nor a newline. The
start script's `$(< file)` keeps a `%` but drops a trailing newline. Documented in
`openchamber.nix` and `.sops.yaml` (`e220322`).

### Two credentials printed into the session

While inventorying MCP servers, one investigation printed the managed OpenCode
server's password and OpenChamber's agent tool token from
`~/.config/openchamber/managed-opencode/state.json`. Its filter hid only strings
longer than 60 characters.

- **Exposure.** The server listens on loopback only, and the token is only accepted
  from same-machine addresses. But both values are in the session history and were
  sent to the model provider.
- **Not rotated as of this writing.** OpenChamber reuses both across restarts on
  purpose. The rotation steps are in `services.md`.

## Corrections made during the session

1. **The stray daemon's cause.** The user was first told it came from a command
   run during a restart. The cause was the socket name changing with the config
   (above).
2. **The wrong check of that cause.** The socket-name mechanism was then wrongly
   ruled out, by comparing the config store path each generation *contained*
   (identical for 297-302). What matters is what the link pointed to when the
   daemon started; the daemon's own log shows `539ba2d32a06`.
3. **Credential rotation.** One investigation said restarting OpenChamber would
   rotate the leaked credentials. The code reuses them.
4. **mcpx's stop time.** One investigation, and the first version of the
   `ExitTimeOut` comment, said mcpx stops in "5 s of requests, then up to 10 s of
   servers". The whole stop is bounded by `limits.execDrainTimeout` (10 s), with
   the 5 s HTTP grace inside it. `ExitTimeOut = 20` covers it either way.
5. **Copilot models.** One investigation reported that OpenChamber's OpenCode had
   no Copilot models. That was transient, during OpenChamber's rewrites of the
   OpenCode config (15:09-15:13); `/api/model` later listed 38 `github-copilot`
   models.
6. **mcpx autostart setting.** One investigation reported mcpx had no setting to
   disable autostart. `daemon.autostart` exists; it is in `mcpx config --schema`
   and `docs/configuration.md` upstream.

## Open problems (2026-10-19)

Most urgent first. Each has its evidence and a suggested fix.

1. **Disk space.** 8.2 GiB free (99% used) before garbage collection; about 39 GiB
   after. The biggest remaining items, all user data and not deleted:
   - `~/.local/share/opencode/tmp`: 43 GB. Nothing prunes it, because the opencode
     wrapper points `TMPDIR` there (see #377 and #392).
   - The OpenCode database: 26,231,259,136 bytes. About 5.5 GB of free pages
     *(audit)*; auto-vacuum is off.
   - A 2026-10-16 backup of the database. It is an APFS clone, so deleting it frees
     only the blocks that have changed since, not 26 GB.
   - `~/.local/share/opencode/worktree`: 14 GB.
2. **Leaked credentials.** Rotate them (above).
3. **The live OpenCode config is not the repo's.**
   - `~/.config/opencode` → `~/nix/.opencode` → `~/.opencode/opencode.jsonc`, a
     plain v1-format file that OpenChamber rewrites. `opencode-global.jsonc` and
     its `model_routes` pools are unused.
   - In it, `small_model` is `github-copilot/claude-opus-5.5`, so every session
     title is written by the premium model.
   - Decide whether nix or OpenChamber owns this file.
4. **OpenChamber restarts OpenCode whenever it rewrites OpenCode's config.**
   `openchamber.log` shows 21 `handoff (config-refresh)`, 1 `handoff
   (binary-changed)` and 1 `start (no-state)`. At 13:29 a previous OpenCode was
   force-stopped when its handover grace ran out *(audit)*.
5. **Every mcpx update restarts OpenChamber,** because the start script puts mcpx's
   store path on its PATH. Port 3000 is closed for about 10 s each time. A stable
   link to mcpx on that PATH would avoid it.
6. **mcpx.**
   - `aws` has no credentials and fails on every start (`aws configure sso`, then
     `aws sso login`, or remove it).
   - GitHub and Datadog expose write tools (#371).
   - `elicit.confirmDestructive` is off.
   - OpenCode's mcpx plugin can still start a stray (developing-today/opencode#134);
     the agent wrapper clears it at the next agent start.
7. **OpenCode sessions and snapshots** *(audit)*:
   - Some sessions repeat "tool_use.input: Input should be an object" every turn
     (4 times across 3 sessions).
   - Warm-up retried one session about every 4 minutes after "invalid request body".
   - Undo snapshots fail in operations-portal: 480 "pathspec … is beyond a symbolic
     link" errors, because its `features` directory is a symlink.
8. **Shell start-up.** `homePrograms.nix` runs `eval "$(opencode completion)"`.
   OpenCode v2 treats `completion` as a directory name: it starts the 190 MB
   binary in every new shell and logs errors.
9. **Leftover processes,** not part of the three services and still running at
   this writing:
   - pid 40073, a fake OpenCode test server from Oct 14 on port 53489;
   - pid 88342, a Node test process from Oct 17 that ignores SIGTERM;
   - pids 32670 and 80975, Python workers from an operations-portal worktree;
   - three headless Chrome groups from OpenChamber performance probes, about 1 GB *(audit)*.
10. **Log noise and growth.**
    - 3,182 of `openchamber.log`'s 4,447 lines are "[PushWatcher] connected".
    - `opencode.log` grew about 45 MB in 27 hours *(audit)*.
    - `/tmp/mcpx.err.log` (605 KB) is never rotated.
11. **The terminal's shared OpenCode service** (`opencode serve --service`) runs as
    the child of a terminal client, not of launchd. Closing that terminal tab
    probably stops it for other terminal clients too *(audit, inferred)*.

## Model-backed features (2026-10-19)

- **OpenCode.** The default model and the title agent are both
  `github-copilot/claude-opus-5.5` (from the live config's `small_model`). Summary
  and compaction use the default model. All 11 Copilot models referenced in the
  configs exist *(audit)*.
- **OpenChamber's small model** (session titles, goal writer, session assist) is
  `github-copilot/claude-sonnet-5`, and it worked that day *(audit)*.
- **OpenChamber's automatic routing ("Jev").**
  - It is off: `classification.json` has `"source":"off"` and `routing.json` has
    `enabled:false` *(audit)*. That is why the UI says "Needs Jev, and no
    classification provider is set up".
  - Options listed: Zen's free promotion (sends conversation excerpts to
    opencode.ai), a Zen key, OpenRouter or Vercel AI Gateway, TypeSafe's API, or a
    custom endpoint.
- **Dictation.** A local sherpa-onnx Parakeet 0.6B int8 model (631 MB) is
  installed. There are no errors and no sign it has been used *(audit)*.
  Text-to-speech uses macOS `say`.
- **`@xenova/transformers`** ships with OpenChamber, with no sign it is used *(audit)*.
- **mcpx embeddings.** The backend is `auto` and `search.semantic` is false; the
  embeddings table is empty, and the test vectors look like a hash fallback, not a
  model *(audit, inferred)*. Off, not failing.

## Left for the user at the end of the session

1. `just switch`. It should end with `up: org.nixos.openchamber` and
   `up: org.nixos.mcpx`; mcpx restarts once onto its fixed socket.
2. Then `rm -rf ~/.local/share/lootbox`. Today's mcpx still runs two servers from
   it, so it must wait for the switch. The switch removes `~/.config/lootbox` and
   the old launchd plist itself.
3. Rotate the two leaked credentials when convenient.
4. Decide on the disk items, the AWS server and who owns the live OpenCode config.
