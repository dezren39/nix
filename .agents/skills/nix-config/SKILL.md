---
name: nix-config
description: Use when working in the ~/.config/nix nix-darwin flake repo (rebuilding the Mac system, editing configuration.nix/homePrograms.nix/systemPackages.nix, homebrew brews/casks/masApps, custom pkgs/ like flake-tidy or opencode-share, the OpenCode/OpenChamber fork inputs and their launchd server, or the justfile workflow). Gets an agent caught up on this repo's structure, standards, tooling, and available skills before making changes.
---

# nix-config: repo orientation for agents

This is a single-user **nix-darwin + home-manager + nix-homebrew** flake for an
Apple Silicon Mac (`aarch64-darwin`), plus heavy local AI tooling (OpenCode /
GitHub Copilot / lootbox). Read `ARCHITECTURE.md` at the repo root for the full
breakdown; this skill is the fast path.

## First moves

1. Read `ARCHITECTURE.md` (repo root) — complete map of every file.
2. Run `just` (or `just --list`) to see the task runner recipes.
3. Never hand-run `nix-darwin switch`; use the scripts/recipes below.

## Rebuild & workflow (how changes go live)

- `just rebuild` / `./rebuild.sh` / `./r` — flake update + switch.
- `just switch` / `./simple-rebuild.sh` / `./s` — switch only (no flake update).
- `just build` — build only, no sudo, commits a build marker. While iterating on a
  failing build use `just build-no-commit` (alias `build-only`; passes
  `--no-commit`), then finish with one `just build` to commit.
- `just switch` builds as you (`nix build`; it fails if nix exits 0 without
  producing `./result`), activates as root (`sudo ./result/sw/bin/darwin-rebuild
  switch`), fixes repo ownership, commits empty markers (hostname + build store
  path, then generation), and ends by checking that every long-running launchd
  agent listed in `health_services` in `simple-rebuild.sh` keeps one pid for 3 s
  and answers its URL; otherwise the switch exits non-zero. Add new long-running
  agents to that list.
- **A passing build tests nothing that runs at activation.** `just build` never
  runs `home.activation.*`, `system.activationScripts` / `postActivation`, or
  (re)starts launchd agents; only `just switch` does, and that needs sudo and the
  user. When you change any of them, run the shell fragment yourself against a
  scratch directory where you can, and say in the commit and your reply that it
  is untested until a switch and what the user should check afterwards.
- `just fmt` — nixfmt + flake-tidy (run before finishing nix edits).
- `nix fmt` — treefmt (nixfmt + rustfmt).
- `./clean` — aggressive disk cleanup (nix GC, docker prune, caches). Destructive.

## Module wiring (important — non-standard)

Entry modules compose sub-files with `lib.recursiveUpdate` (deep merge), **not**
an `imports` list. Do not assume standard `imports` semantics.

```
flake.nix → configuration.nix
  ├─ imports: systemPackages, brews, casks, masApps, services
  ├─ recursiveUpdate nix.settings.nix
  └─ home-manager user → homeUser.nix → recursiveUpdate homePackages + homePrograms
```

Where things go:
- New system package → `systemPackages.nix` (the real list; `homePackages.nix` is a stub).
- Shell/git/program config → `homePrograms.nix`.
- macOS defaults / launchd / activation / homebrew meta → `configuration.nix`.
- Homebrew apps → `brews.nix` / `casks.nix` / `masApps.nix`.
- Nix daemon settings → `nix.settings.nix` (keep in sync with flake.nix notes).

## Standards & conventions

- Format Nix with **nixfmt**, Rust with **rustfmt** (see `treefmt.nix`). shellcheck
  is intentionally disabled.
- Determinate Nix owns the daemon (`nix.enable = false`). Assume flakes + a broad
  `experimental-features` set are available.
- Flake input hygiene is maintained by **flake-tidy** — after touching `flake.nix`
  inputs run `just tidy` (or `just tidy-check` in CI). Prefer `follows` over new
  duplicate nixpkgs; the `nixpkgs-hoisted*` inputs exist for this.
- Commits: rebuild scripts auto-commit; only commit manually when asked.
- License headers: `SPDX-License-Identifier: MIT OR Apache-2.0`.

## Custom packages (`pkgs/`)

- **flake-tidy** (Python, ~3300 lines, **~134 pytest tests**) — flake input
  dedup/merge/flatten. `nix run .#flake-tidy -- all|dedup|merge|flatten [--check|--dry-run]`.
  Tests: `just tidy-test` (`cd pkgs/flake-tidy && uv sync && .venv/bin/python -m pytest tests/ -v`;
  not `uv run`, which holds uv's cache lock and blocks `clean`).
- **opencode-share** — share `.opencode` across projects via bindfs. `just share|unshare|share-status`.
- **symlinker** — bulk symlink manager with undo. Source is `symlinker.sh` at repo
  root (not in the package dir). `just link-git`.
- **bun-bin** — prebuilt Bun (update version + hashes in `pkgs/bun-bin/hashes.json`).
- **brew-repair** — reinstall casks whose `.app` went missing.
- **buffer-backup** — a VS Code extension with its own flake.
- **noTunes.nix** — stops Apple Music auto-launch.

## OpenCode / Copilot / lootbox

- Config: `opencode-global.jsonc` (linked as ~/.config/opencode/opencode.jsonc;
  not named opencode.jsonc so the project walk does not load it twice; v2-native; github-copilot only; every model is a
  pool in `experimental.model_routes`: capability pools frontier / fast-strong /
  balanced / very-fast and family pools opus / sonnet / sol / luna / terra with
  the effort as variant; agents build, general, explore, quick, balanced, deep).
  The fork's built-in `routing-config` skill documents every routing key. v2
  ignores `tui.jsonc` (it reads `~/.config/opencode/cli.json`).
- **Tooling is subagent-first** and routed through the local **lootbox** MCP server
  (port 9420, launchd-managed). Namespaces: `mcp_codedb`, `mcp_fff`,
  `mcp_chrome_devtools`, `mcp_context7`. Write reusable `.ts` scripts to
  `.lootbox/scripts/`. Nix provides Deno/codedb/fff-mcp; a pinned updater builds
  Lootbox and npm MCP CLIs. `just lootbox-server|-kill|-restart|-check|update-lootbox`.
- **OpenCode and OpenChamber come from forks we own** (developing-today/opencode,
  developing-today/openchamber; `main`; openchamber's `opencode` input follows
  ours). Nothing is patched here: fix the fork, push to its `main`, then re-pin
  upward (opencode -> openchamber lock -> this repo's lock). Procedure: the
  `update-opencode` skill. `flake.nix` wraps the fork's opencode only to force
  `OPENCODE_DB=~/.local/share/opencode/opencode.db` (the one database every
  opencode/OpenChamber must use) and a durable `TMPDIR`.
- `openchamber.nix`: launchd agent `org.nixos.openchamber` keeps `openchamber
  serve` up on 127.0.0.1:3000 (port of developing-today/code's systemd model);
  the desktop app is its client. mcpx (source: developing-today/code's
  `pkgs/mcpx`, built here from the pinned `mcpx-src` flake input) runs as
  launchd agent `org.nixos.mcpx`; servers come
  from `lootbox.config.json` via `mcpx.config.nix`.
- Database tools: `just opencode-db-backup` (instant APFS clone),
  `opencode-db-check`, `opencode-db-pending` (migrations a new build would
  apply; refuses if the DB is ahead of the build).
- Slash commands live in `commands/` (`/gha-fix`, `/gha-why`). Run
  `./symlink-commands` to install them globally; use `--dry-run` to preview.

## Skills in this repo

Repo-local agent skills live in `.agents/skills/<name>/SKILL.md` (a plain
git-tracked directory, same layout as the global `~/.agents/skills` that opencode
auto-scans on boot — not a symlink). Add new skills there; opencode picks them up
automatically. Currently shipped: **nix-config** (this skill),
**update-opencode** (updates and repairs the OpenCode/OpenChamber fork stack, forks first),
**lootbox** (query syntax and per-server rules for the lootbox MCP tools) and
**merge-first** (merge PRs without waiting for checks; only when the user asks for that mode).

## Gotchas

- `.opencode` at repo root is a **symlink** into OneDrive that loops back — real
  tracked content lives at the repo root (`instructions/`, `opencode-global.jsonc`, etc.).
- CI: `opencode.yml` (the `/oc` comment bot) and `mcpx-nixbuild.yml`, which
  builds only the mcpx package, on Linux, when `flake.nix` or `flake.lock`
  changes. Nothing in CI builds or activates the darwin system: run `just fmt`,
  `just tidy-check`, `just tidy-test` and `just build-no-commit` locally.
- **A launchd agent is not up** (`org.nixos.openchamber`, `.mcpx`, `.lootbox`;
  `just switch` names it). Read `launchctl print gui/$(id -u)/<label>` (state,
  program, last exit code) and the agent's stderr log before the app's own log.
  - Exit 78 (`EX_CONFIG`) and nothing new in the app log: launchd could not run
    the program, usually because the path in `program =` does not exist (a
    dangling link). launchd then parks the job until that file appears (an
    `event triggers … Executable` block in the print) and `launchctl kickstart`
    hangs. After fixing the path, reload it: `launchctl bootout
    gui/$(id -u)/<label>`, then `launchctl bootstrap gui/$(id -u)
    ~/Library/LaunchAgents/<label>.plist`.
  - mcpx exiting 1 about every 10 s with "a daemon is already listening on
    …sock" in `/tmp/mcpx.err.log`: a daemon the CLI started itself (`daemon
    --detached --idle-exit 4h`, possibly an older build) holds the socket and
    never goes idle. Run `cd ~ && mcpx stop`, then `launchctl kickstart
    gui/$(id -u)/org.nixos.mcpx`; `lsof -t` on the socket should then show
    launchd's `mcpx daemon --config …`.
