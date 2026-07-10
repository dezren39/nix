---
name: nix-config
description: Use when working in the ~/.config/nix nix-darwin flake repo (rebuilding the Mac system, editing configuration.nix/homePrograms.nix/systemPackages.nix, homebrew brews/casks/masApps, custom pkgs/ like flake-tidy or opencode-share, opencode/Copilot patches, or the justfile workflow). Gets an agent caught up on this repo's structure, standards, tooling, and available skills before making changes.
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
- Both `git add .`, run `sudo nix run nix-darwin -- switch --flake .`, fix repo
  ownership, and auto-commit an empty commit tagged with hostname + generation.
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
  Tests: `just tidy-test` (`cd pkgs/flake-tidy && uv run pytest tests/ -v`).
- **opencode-share** — share `.opencode` across projects via bindfs. `just share|unshare|share-status`.
- **symlinker** — bulk symlink manager with undo. Source is `symlinker.sh` at repo
  root (not in the package dir). `just link-git`.
- **bun-bin** — prebuilt Bun (update version + hashes in `pkgs/bun-bin/hashes.json`).
- **brew-repair** — reinstall casks whose `.app` went missing.
- **buffer-backup** — a VS Code extension with its own flake.
- **noTunes.nix** — stops Apple Music auto-launch.

## OpenCode / Copilot / lootbox

- Config: `opencode.jsonc` (github-copilot only; model
  `github-copilot/claude-opus-4.8`), TUI `tui.jsonc` (`auto_scroll_tolerance` is a
  custom patched key). Instructions: `instructions/lootbox.md`, `instructions/subagents.md`.
- **Tooling is subagent-first** and routed through the local **lootbox** MCP server
  (port 9420, launchd-managed). Namespaces: `mcp_codedb`, `mcp_fff`,
  `mcp_chrome_devtools`, `mcp_context7`. Write reusable `.ts` scripts to
  `.lootbox/scripts/`. `just lootbox-server|-kill|-restart`.
- The `opencode` derivation is patched in `flake.nix` (`opencodePatches`) with ONLY
  two patches: `patches/opencode-compact-tui.patch` and
  `patches/opencode-scroll-autofollow.patch`. The root `*.patch` files (Copilot
  Business/Enterprise, compaction, OpenAI response-id, edit-read) are NOT applied —
  they're local reference snapshots (already carried by the `anomalyco/opencode/dev`
  input). Read them before touching opencode auth/session/TUI behavior.
- Slash commands live in `commands/` (`/fix`, `/why`).

## Skills in this repo

Repo-local agent skills live in `.agents/skills/<name>/SKILL.md` (a plain
git-tracked directory, same layout as the global `~/.agents/skills` that opencode
auto-scans on boot — not a symlink). Add new skills there; opencode picks them up
automatically. Currently shipped: **nix-config** (this skill).

## Gotchas

- `.opencode` at repo root is a **symlink** into OneDrive that loops back — real
  tracked content lives at the repo root (`instructions/`, `opencode.jsonc`, etc.).
- CI is only a `/oc` comment-triggered OpenCode bot (`.github/workflows/opencode.yml`);
  there is no build/test pipeline — run `just fmt`, `just tidy-check`, and
  `just tidy-test` locally.
