# nix config — architecture & features

Single-user **nix-darwin + home-manager + nix-homebrew** flake for an Apple
Silicon (`aarch64-darwin`) Mac, plus a substantial local AI-tooling
(OpenCode / Copilot / lootbox) setup. This document catches a human or agent up
on the whole repo.

SPDX-License-Identifier: MIT OR Apache-2.0

---

## 1. Top-level layout

```
flake.nix              Flake entry: inputs, darwinConfigurations, packages, apps, devShell, checks
configuration.nix      Main nix-darwin module (system defaults, homebrew, launchd, activation)
homeUser.nix           home-manager per-user module (imports homePackages + homePrograms)
homePackages.nix       home-manager packages (stub: just `hello`)
homePrograms.nix       home-manager programs (shells, git, ghostty, atuin, starship, setup-opencode)
systemPackages.nix     environment.systemPackages (~180 pkgs — the real package list)
nix.settings.nix       nix daemon settings (experimental-features, substituters)
services.nix           nix-darwin services (aerospace, skhd, jankyborders)
brews.nix casks.nix masApps.nix   homebrew formulae / casks / mas apps
treefmt.nix            formatter config (nixfmt + rustfmt) — used by flake `formatter`/`checks`

pkgs/                  Custom packages (see §4)
patches/               sidepulse + lootbox patch stacks (OpenCode is not patched; see §5)
*.patch (root)         homebrew-services fix (unused)
openchamber.nix        OpenChamber server launchd agent + desktop client (§5)
instructions/          OpenCode instruction files (lootbox.md, subagents.md)
commands/              OpenCode slash commands (/gha-fix, /gha-why)
.agents/skills/        Repo-local agent skills (see §7)
opencode.jsonc         OpenCode v2 config (Copilot model pools, agents, permissions)
tui.jsonc              v1 TUI config; v2 ignores it (v2 reads ~/.config/opencode/cli.json)
sounds/                Star Trek notification sounds (opencode-notifier)
themes/ vibrant-ink.json   Active TUI theme
justfile               Task runner (rebuild, tidy, share, link-git, lootbox)
rebuild.sh r / simple-rebuild.sh s / clean(.sh) / path.sh   Rebuild + cleanup scripts
symlinker.sh           Large bash symlink manager (source for pkgs/symlinker)
.aerospace.toml        AeroSpace tiling WM config
lootbox.config.json    lootbox MCP server definitions
config.toml            VSCode extension source-of-truth list (used by code-install-extensions.sh)
.github/workflows/opencode.yml   Comment-triggered OpenCode bot (only CI)
```

### Wiring (module composition)

The two entry modules compose sub-files with `lib.recursiveUpdate` (deep merge),
**not** the standard `imports` list:

```
flake.nix → ./configuration.nix
  ├─ imports: systemPackages, brews, casks, masApps, services
  ├─ recursiveUpdate with nix.settings.nix
  └─ home-manager.users."drewry.pope" = ./homeUser.nix
        ├─ recursiveUpdate homePackages.nix
        └─ recursiveUpdate homePrograms.nix
```

---

## 2. The flake (`flake.nix`)

- **Target:** `aarch64-darwin`, single default host keyed by `scutil --get
  LocalHostName` (`hosts = { MGM9JJ4V3R = { ... }; }`). `darwinSystemConfigurations`
  merges `defaultDarwinSystem` with per-host overrides.
- **Stack inputs:** determinate (Determinate Nix), nix-darwin, home-manager,
  mac-app-util, nix-homebrew (+ brew-src, homebrew-core/cask/bundle/services and
  third-party taps), NUR, fenix (Rust), treefmt-nix, just, nixpkgs-terraform,
  `opencode` (github:developing-today/opencode, our fork) and `openchamber`
  (github:developing-today/openchamber, our fork; its `opencode` follows ours).
- **Input hygiene:** a heavy `follows` graph plus `nixpkgs-hoisted*` inputs keeps
  transitive nixpkgs deduplicated. This is maintained by the custom **flake-tidy**
  tool (§4).
- **`mkPackages`** (single source of package defs, reused by `packages`, `apps`,
  `devShells`, `checks`): builds `bun-bin` and `mcpx`; `opencode-unwrapped`
  (the fork's package with our mcpx) and `opencode` (a wrapper pinning
  `OPENCODE_DB` and `TMPDIR`); `openchamber` (the fork's server built against
  that `opencode`); plus `brew-repair`, `flake-tidy`, `opencode-share`,
  `symlinker`.
- **checks:** `formatting` (treefmt) and `tidy` (flake-tidy `--check`) on every
  supported system; the Sidepulse package build/test and setup smoke test run
  on Darwin only.
- **apps:** `flake-tidy`, `opencode`, `openchamber`, `opencode-share`, `symlinker`;
  Sidepulse apps are Darwin-only.
- **devShell:** opencode, flake-tidy, symlinker, just, nixfmt, git, gh (with a
  version-printing shellHook); Sidepulse tools are added on Darwin only.

---

## 3. System configuration highlights

**`configuration.nix`**
- System-wide git ignore/config written to `/etc/gitignore` + `/etc/gitconfig`,
  forced onto Nix's git via `GIT_CONFIG_SYSTEM` (blocks `codedb.snapshot`,
  `.lootbox/cache|tmp`).
- `nixpkgs`: `allowUnfree/allowBroken/allowUnsupportedSystem`; overlays add fenix
  and the custom `noTunes` app.
- `system.defaults`: fast key repeat, disabled press-and-hold, silenced beep,
  auto-hide menu bar, trackpad tap-to-click, and a curated Spotlight preference
  block.
- **nix-homebrew:** Rosetta on, `mutableTaps=false`, declarative `trust.formulae`,
  custom taps (firefox-profile-switcher, autoraise, fuse).
- **`nix.enable = false`** — Determinate Nix owns the daemon; package forced to
  `nixVersions.git`.
- **activationScripts:** menu-bar spacing, disable font smoothing, kickstart skhd,
  exclude `/nix` from Spotlight, xcode-select + license accept.
- **launchd agents:** `naturalScrollingToggle` (flip scroll dir by mouse presence),
  `lootbox` (installs + runs the lootbox MCP server on port 9420).

**`nix.settings.nix`** — broad `experimental-features` (flakes, ca-derivations,
dynamic-derivations, recursive-nix, pipe-operators, …); extra substituters for
nixpkgs-terraform + fenix; perf tuning (`http-connections=100`,
`max-substitution-jobs=64`, `keep-outputs/derivations`).

**`systemPackages.nix`** — containers (colima/docker/lima/k3d/k9s/kubectl),
cloud/IaC (pinned `terraform-1.5.7`, awscli, gcloud, sops+age), languages
(go/node/bun/python/dotnet_9/zig/swift + fenix Rust toolchain + many `cargo-*`),
plus the repo's own flake packages. `buf` is overridden with `doCheck=false`.

**`services.nix`** — `aerospace` (config from `.aerospace.toml` via
`fromTOML`), `skhd` (remaps `cmd-q`, binds toggle-menubar), `jankyborders`.
(yabai/sketchybar present but commented.)

**`homeUser.nix`** — after `writeBoundary`, activation runs the packaged
`sidepulse setup` for the `sidepulseProviders` list (OpenCode by default). Edit
that list to change which providers are managed.

**`homePrograms.nix`** — bash/zsh/fish sharing aliases + init (`rm=trash`, cargo
linker pinned to `/usr/bin/cc`, credential helpers, an aerospace+fzf window
switcher, and a `setup-opencode()` helper that symlinks a central `.opencode`
into projects). Also atuin (Ctrl-R history), starship, direnv+nix-direnv, git
(work identity, `safe.directory=*`), ghostty (ghostty-bin, synthwave theme).

---

## 4. Custom packages (`pkgs/`)

| Package | Lang | Wrapper | What it does |
| --- | --- | --- | --- |
| **flake-tidy** ⭐ | Python 3 (~3300 lines) | `writeShellApplication` | Deduplicates/merges/flattens flake inputs by adding `follows`; edits `flake.nix`, reruns `nix flake lock` with backout, reformats. `dedup`/`merge`/`flatten`/`all` subcommands, `--check`/`--dry-run`. **~134 pytest tests** + fixtures. |
| bun-bin | Nix | `mkDerivation` | Prebuilt Bun (currently 1.3.14); Darwin re-signs via `install_name_tool`+`rcodesign`. Hashes in `hashes.json`. |
| mcpx | Go | `buildGo127Module` | MCP gateway / Code Mode runner, vendored verbatim from developing-today/code; its daemon runs as a launchd agent. Darwin skips the Linux-only takeover tests (flake.nix). |
| brew-repair | Bash (+py) | `writeShellScriptBin` | Detects casks brew thinks are installed but whose `.app` is missing; selectively reinstalls. `--dry-run/--quiet/--skip`. |
| opencode-share | Bash | `writeShellApplication` | Shares one `.opencode` across projects via nested **bindfs** mounts while keeping per-project `plans/`. mount/unmount/status/dry-run. |
| symlinker | Bash (~1250 lines, source at repo root `symlinker.sh`) | `writeShellApplication` | Bulk symlink manager with backup, undo (v2 RELINK), filters, dry-run. |
| buffer-backup | TypeScript (own flake) | own flake | VS Code extension: auto-backs-up unsaved buffers with SHA-256 dedup + retention. |
| noTunes.nix | Nix | `mkDerivation` | macOS app that stops Apple Music auto-launch. |
| codedb | Zig binary | `fetchurl` | Pinned signed/notarized AST-aware code search and MCP server. |
| fff-mcp | Rust binary | `fetchurl` | Pinned frecency-ranked file/content search MCP server. |
| lootbox-update | Bash/Nix | `writeShellApplication` | Builds pinned Lootbox source with compatibility patches, installs its UI and pinned npm MCP CLIs, then verifies launchd readiness. |
| sidepulse | Python/PyObjC | `buildPythonApplication` | Darwin-only SidePulse CLI/status-bar app. Individually documented, an ordered stack of eleven patch files applies upstream PRs #14, #17, #26, #28, #29, #30, #31 and #32. PRs #33, #34 and #35 are local-only: #33 holds the LED completion signal until the user is visibly back at a terminal (`src/sidepulse/presence.py`) instead of timing out after 4s; #34 stops failures and permission prompts expiring on a clock (they persist while the owning process is alive, and are released when the session is deleted or its terminal is closed), keeps a failure from being overwritten by the idle that follows it, and widens OpenCode error capture to `message.updated` and the `session.next.*` runner; #35 adds multi-signal composition (`src/sidepulse/compose.py`): ten display modes over a ranked signal registry with demand/ambient tiers, eight static (`priority`, `round-robin`, `split`, `paired`, `orbit`, `depth`, `beacon`, `tide`) and two host-driven (`drift`, `live`), plus per-signal arrival announcements, a `tempo` pace dial and per-LED colour correction for the Dot's opposed LEDs. Selected by `multi_signal.mode`, defaulting to `beacon`. See `docs/sidepulse-modes.md` for every display mode, `docs/sidepulse-customisations.md` for what each local patch adds and the live LED configuration, and `docs/sidepulse-leds.md` for the verified hardware limits they work within. |
| sidepulse-setup | Bash/Nix | `writeShellApplication` | Darwin-only wrapper providing `sidepulse --setup opencode` and `sidepulse setup opencode`; setup defaults to all upstream-supported providers. Home Manager invokes the packaged `sidepulse setup <provider>` for the configured provider list on every switch. |

---

## 5. OpenCode integration

- **`opencode.jsonc`** — v2-native config, `github-copilot` only. Every model is
  a pool (`experimental.model_routes`, shown as `opencode-route/<id>`):
  capability pools `frontier`, `fast-strong`, `balanced`, `very-fast`
  (round-robin, sticky per session, grouped by Artificial Analysis
  Intelligence/Coding Agent index and task time), and family pools `opus`,
  `sonnet`, `sol`, `luna`, `terra` (newest first, previous generation as
  failover; pick effort as the variant, e.g. `opencode-route/sonnet#high`).
  Agents: `build` (fast-strong); subagents `general` (fast-strong), `explore`
  and `quick` (very-fast), `balanced`, `deep` (frontier); `compaction`
  balanced, `summary` very-fast, `title` unset (Copilot's free utility model),
  `plan` disabled. The old `agents/*.md` prompts and tiers are gone.
  Instructions: `instructions/{lootbox,worktrees,recordings,work-strategies}.md`.
  Plugins: opencode-notifier only (the v1 plugins fail v2's loader; listed in
  the file for when they are ported). `compaction.buffer = 8192`.
- **`tui.jsonc`** — v1 TUI config; v2 does not read it (`~/.config/opencode/cli.json`
  instead). Kept only until it is clear nothing else reads it.
- **`instructions/`** — `lootbox.md` (all MCP tooling via local lootbox server on
  :9420; write reusable `.ts` to `.lootbox/scripts/`; namespaces `mcp_codedb`,
  `mcp_fff`, `mcp_chrome_devtools`, `mcp_context7`); `subagents.md` (subagent-first
  workflow; native subagents `explore` and `general`, plus configured custom
  capability tiers).
- **`commands/`** — `/gha-fix` (diagnose+fix a failed GitHub Actions job, with
  production-safety gating) and `/gha-why` (analyze a *successful* job).
  `./symlink-commands` installs missing global command symlinks safely.
- **`lootbox.config.json`** — the MCP server list, shared by lootbox and mcpx
  (`mcpx.config.nix` derives mcpx's config from it).

### OpenCode and OpenChamber: our forks, no local patches

OpenCode and OpenChamber are built from forks we own
(developing-today/opencode and developing-today/openchamber, both `main`). Fixes
go into the fork, then the pins move up: opencode fork -> openchamber's lock ->
this repo's lock. `.agents/skills/update-opencode` is the procedure. The old
local patch stack was retired (TUI-only changes do not reach OpenChamber, the
rest is covered by v2); remaining server-side gaps are developing-today/opencode#13
and developing-today/openchamber#28.

- **One database.** `~/.local/share/opencode/opencode.db` holds every session.
  The `opencode` wrapper forces `OPENCODE_DB` to it, and `OPENCODE_DB` is also
  set in `environment.variables`, `launchd.user.envVariables` and home-manager
  `sessionVariables` (OpenChamber's server reads it directly). `just
  opencode-db-backup` (APFS clone), `opencode-db-check`, `opencode-db-pending`.
- **Standing server** (`openchamber.nix`, ported from developing-today/code's
  systemd units): launchd agent `org.nixos.openchamber` runs `openchamber serve
  --foreground` on 127.0.0.1:3000, managing one `opencode serve` reached through
  `~/.local/state/openchamber/bin/opencode`. A switch restarts the agent only
  when OpenChamber changed; a new OpenCode re-points the link and SIGHUPs for an
  in-place handover. The desktop app is a client of that server
  (`OPENCHAMBER_SERVER_URL`, `OPENCHAMBER_SKIP_LOCAL_SERVER`). Logs:
  `~/Library/Logs/openchamber.log`.

### Other patch files

- **homebrew-services.patch** — fixes a Homebrew `require_relative` path; wired via
  a **commented-out** `applyPatches` block for the homebrew-services tap in
  `configuration.nix` (currently disabled).

---

## 6. Dev workflow

- **Rebuild:** `just rebuild` / `./rebuild.sh` / `./r` (flake update + switch);
  `just switch` / `./simple-rebuild.sh` / `./s` (switch only). Both `git add .`,
  run `nix-darwin switch --flake .`, fix repo ownership, auto-commit an empty
  commit tagged with hostname + generation.
- **Format:** `just fmt` (nixfmt + flake-tidy); treefmt via `nix fmt`.
- **Flake input hygiene:** `just tidy` / `tidy-dry` / `tidy-check` (flake-tidy);
  tests via `just tidy-test`.
- **Cleanup:** `./clean` (nix GC, docker prune, cache wipe, trash).
- **Symlinks:** `just link-git` (symlinker.sh). **.opencode sharing:** `just share`
  / `unshare` / `share-status`.
- **lootbox:** launchd runs the pinned, locally built Lootbox on loopback port 9420.
  Nix supplies Deno, codedb, fff-mcp, and the updater; Chrome DevTools MCP and
  mcp-remote are pinned by the controlled update step. Use `just lootbox-server`
  / `-kill` / `-restart` / `-check` / `update-lootbox`.
- **CI:** only `.github/workflows/opencode.yml` — a `/oc` comment-triggered
  OpenCode bot. No build/test pipeline in CI (flake-tidy tests run locally).

---

## 7. Repo-local agent skills (`.agents/skills/`)

Repo-local skills live in `.agents/skills/<name>/SKILL.md` — a plain git-tracked
directory using the same layout as the global `~/.agents/skills` that opencode
auto-scans on boot (not a symlink). opencode picks them up automatically; no
config registration needed. This repo ships `nix-config` for repo orientation
and `update-opencode` for updating and repairing the OpenCode/OpenChamber fork
stack from the forks up through this repo's lock.
