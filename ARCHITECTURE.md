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
patches/               OpenCode TUI patches applied to the opencode derivation
*.patch (root)         OpenCode Copilot/OpenAI patches + homebrew fix
instructions/          OpenCode instruction files (lootbox.md, subagents.md)
commands/              OpenCode slash commands (/gha-fix, /gha-why)
.agents/skills/        Repo-local agent skills (see §7)
opencode.jsonc         OpenCode config (Copilot-only, model policy, plugins)
tui.jsonc              OpenCode TUI theme + auto_scroll_tolerance
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
  `opencode` (github:anomalyco/opencode/dev), and `opencode2`
  (github:anomalyco/opencode/v2).
- **Input hygiene:** a heavy `follows` graph plus `nixpkgs-hoisted*` inputs keeps
  transitive nixpkgs deduplicated. This is maintained by the custom **flake-tidy**
  tool (§4).
- **`mkPackages`** (single source of package defs, reused by `packages`, `apps`,
  `devShells`, `checks`): builds `bun-bin`, patches `opencode` (applies
  `opencodePatches` + swaps in `bun-bin` as the build's bun), builds the v2 CLI
  from `packages/cli` as `opencode2`, and exposes `brew-repair`, `flake-tidy`,
  `opencode-share`, `symlinker`.
- **`opencodePatches`** = `patches/opencode-compact-tui.patch` +
  `patches/opencode-scroll-autofollow.patch` +
  `patches/opencode-plan-permissions-reminder.patch`.
- **checks:** `formatting` (treefmt) and `tidy` (flake-tidy `--check`).
- **apps:** `flake-tidy`, `opencode`, `opencode2`, `opencode-share`, `symlinker`.
- **devShell:** opencode, flake-tidy, symlinker, just, nixfmt, git, gh (with a
  version-printing shellHook).

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
| opencode2 | TypeScript/Bun | `mkDerivation` | Builds the OpenCode 2.0 beta CLI from the tracked upstream `v2` branch as the separate `opencode2` executable. |
| brew-repair | Bash (+py) | `writeShellScriptBin` | Detects casks brew thinks are installed but whose `.app` is missing; selectively reinstalls. `--dry-run/--quiet/--skip`. |
| opencode-share | Bash | `writeShellApplication` | Shares one `.opencode` across projects via nested **bindfs** mounts while keeping per-project `plans/`. mount/unmount/status/dry-run. |
| symlinker | Bash (~1250 lines, source at repo root `symlinker.sh`) | `writeShellApplication` | Bulk symlink manager with backup, undo (v2 RELINK), filters, dry-run. |
| buffer-backup | TypeScript (own flake) | own flake | VS Code extension: auto-backs-up unsaved buffers with SHA-256 dedup + retention. |
| noTunes.nix | Nix | `mkDerivation` | macOS app that stops Apple Music auto-launch. |
| codedb | Zig binary | `fetchurl` | Pinned signed/notarized AST-aware code search and MCP server. |
| fff-mcp | Rust binary | `fetchurl` | Pinned frecency-ranked file/content search MCP server. |
| lootbox-update | Bash/Nix | `writeShellApplication` | Builds pinned Lootbox source with compatibility patches, installs its UI and pinned npm MCP CLIs, then verifies launchd readiness. |

---

## 5. OpenCode integration

- **`opencode.jsonc`** — `github-copilot` only. Build uses GPT-5.6 Sol xhigh,
  Plan uses Claude Fable 5 high, native subagents and compaction use GPT-5.6 Sol
  medium, and title generation uses GPT-5.6 Luna medium. The top-level small
  model remains GPT-5.6 Terra. Uses
  upstream model metadata and blacklists GPT < 5.6, Claude < 4.8, and Gemini <
  3.1. Loads instructions
  `.opencode/instructions/{lootbox,subagents}.md`. Plugins (npm): opencode-notifier,
  plannotator, opencode-pty, md-table-formatter, opencode-auto-continue (GitHub
  tarball). `compaction.reserved = 8192`.
- **`tui.jsonc`** — theme `vibrant-ink`; `auto_scroll_tolerance: 0` (custom key
  consumed by the scroll-autofollow patch).
- **`instructions/`** — `lootbox.md` (all MCP tooling via local lootbox server on
  :9420; write reusable `.ts` to `.lootbox/scripts/`; namespaces `mcp_codedb`,
  `mcp_fff`, `mcp_chrome_devtools`, `mcp_context7`); `subagents.md` (subagent-first
  workflow; native subagents `explore` and `general`, plus configured custom
  capability tiers).
- **`commands/`** — `/gha-fix` (diagnose+fix a failed GitHub Actions job, with
  production-safety gating) and `/gha-why` (analyze a *successful* job).
  `./symlink-commands` installs missing global command symlinks safely.
- **`lootbox.config.json`** — MCP servers: codedb, fff (fff-mcp), chrome-devtools,
  context7.

### Patches

Applied to the opencode build (`patches/`, via `opencodePatches`):
- **opencode-compact-tui.patch** — zeroes padding/gaps across TUI for a dense layout.
- **opencode-scroll-autofollow.patch** — adds `auto_scroll_tolerance` config + sticky
  scroll (auto-follow only when near bottom).
- **opencode-plan-permissions-reminder.patch** — makes legacy and experimental Plan
  reminders honor resolved Plan permissions while preserving read-only defaults.
- **opencode-hidden-agent-variants.patch** — makes an explicitly configured hidden
  agent variant authoritative, allowing compaction and title generation to use
  independent reasoning effort.
- **opencode-agent-variant-defaults.patch** — makes a visible agent's configured
  variant the TUI default when no explicit per-model preference has been saved.
- **opencode-run-descendant-permissions.patch** — ports anomalyco/opencode#36898 so
  headless `opencode run` approves or rejects permission requests from any
  descendant Task session instead of hanging.
- **opencode-nested-subagent-prompts.patch** — ports the runtime portions of
  anomalyco/opencode#36046 so nested subagent prompts and status surface in the
  TUI and interactive CLI footer.

Root `*.patch` files are **NOT applied by the flake** (only the seven `patches/`
above are). They are local snapshots/reference for fixes that are already carried
by the `anomalyco/opencode/dev` input. Read before touching related behavior:
- **opencode-copilot-business-support.patch** — Copilot Business/Enterprise: `ghu_`
  vs `gho_` tokens, VS Code identity headers, `copilot_internal/v2/token` exchange.
- **opencode-copilot-compaction-fix.patch** — dummy `_noop` tool for Copilot
  Enterprise so compaction with prior tool calls succeeds.
- **opencode-edit-read-clarify.patch** — clarifies "Read before Edit" (new files with
  empty `oldString` need no prior read).
- **opencode-openai-response-id-caching.patch** — OpenAI Responses-API server-side
  prompt caching via `previous_response_id` (adds a DB migration).
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
and `update-opencode` for advancing the OpenCode input while preserving and
repairing active patches.
