# Show available recipes
default: list

list:
  @just --list

# =============================================================================
# Utilities - Lootbox
# =============================================================================

# Build and install the pinned Lootbox source, then restart launchd
[group('lootbox')]
update-lootbox:
    nix run .#lootbox-update -- --force

# Start the launchd-managed Lootbox server
[group('lootbox')]
lootbox-server:
    #!/usr/bin/env bash
    set -euo pipefail
    label="gui/$(id -u)/org.nixos.lootbox"
    if ! launchctl print "$label" >/dev/null 2>&1; then
        launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/org.nixos.lootbox.plist"
    fi
    launchctl kickstart -k "$label"
    for _ in $(seq 1 90); do
        namespaces=$("$HOME/.local/bin/lootbox" tools 2>/dev/null || true)
        if grep -q "mcp_codedb" <<<"$namespaces" \
          && grep -q "mcp_fff" <<<"$namespaces" \
          && grep -q "mcp_chrome_devtools" <<<"$namespaces" \
          && grep -q "mcp_context7" <<<"$namespaces"; then
            echo "Lootbox server is ready with all configured namespaces"
            exit 0
        fi
        sleep 1
    done
    echo "Lootbox server did not become healthy" >&2
    exit 1

# Stop the launchd-managed Lootbox server
[group('lootbox')]
lootbox-kill:
    #!/usr/bin/env bash
    set -euo pipefail
    launchctl bootout "gui/$(id -u)/org.nixos.lootbox" 2>/dev/null || true

# Restart lootbox server
[group('lootbox')]
lootbox-restart: lootbox-kill lootbox-server

# Verify server health, configured namespaces, and Deno script execution
[group('lootbox')]
lootbox-check:
    #!/usr/bin/env bash
    set -euo pipefail
    curl -fsS http://127.0.0.1:9420/health
    namespaces=$("$HOME/.local/bin/lootbox" tools)
    printf '%s\n' "$namespaces"
    for namespace in mcp_codedb mcp_fff mcp_chrome_devtools mcp_context7; do
        grep -q "$namespace" <<<"$namespaces"
    done
    "$HOME/.local/bin/lootbox" exec 'console.log("lootbox script execution ok")'
    "$HOME/.local/bin/lootbox" exec 'const r = await tools.mcp_codedb.codedb_status({}); if (r.isError) throw new Error(JSON.stringify(r)); console.log("codedb ok")'
    "$HOME/.local/bin/lootbox" exec 'const r = await tools.mcp_fff.grep({query:"lootbox"}); if (r.isError) throw new Error(JSON.stringify(r)); console.log("fff ok")'
    "$HOME/.local/bin/lootbox" exec 'const r = await tools.mcp_chrome_devtools.list_pages({}); if (r.isError) throw new Error(JSON.stringify(r)); console.log("chrome devtools ok")'
    "$HOME/.local/bin/lootbox" exec 'const r = await tools.mcp_context7.resolve_library_id({libraryName:"react",query:"React documentation"}); if (r.isError) throw new Error(JSON.stringify(r)); console.log("context7 ok")'
    curl -fsS http://127.0.0.1:9420/ui >/dev/null

# =============================================================================
# Nix Rebuild
# =============================================================================

# Full rebuild: update flake inputs + switch
[group('nix')]
rebuild:
    ./rebuild.sh

# Simple rebuild: switch only (no flake update)
[group('nix')]
switch:
    ./simple-rebuild.sh

# =============================================================================
# Format
# =============================================================================

# Format all nix files and run tidy
[group('format')]
fmt:
    nixfmt *.nix pkgs/*.nix
    nix run .#flake-tidy -- all || true

# =============================================================================
# Flake Tidy
# =============================================================================

# Run all tidy operations: merge -> dedup -> flatten -> dedup
[group('tidy')]
tidy *args:
    nix run .#flake-tidy -- all {{args}}

# Run all tidy operations (dry run)
[group('tidy')]
tidy-dry *args:
    nix run .#flake-tidy -- all --dry-run {{args}}

# Deduplicate flake inputs
[group('tidy')]
tidy-dedup *args:
    nix run .#flake-tidy -- dedup {{args}}

alias dedup := tidy-dedup

# Deduplicate flake inputs (dry run)
[group('tidy')]
tidy-dedup-dry *args:
    nix run .#flake-tidy -- dedup --dry-run {{args}}

alias dedup-dry := tidy-dedup-dry

# Flatten/hoist transitive inputs to root
[group('tidy')]
tidy-flatten *args:
    nix run .#flake-tidy -- flatten {{args}}

alias flatten := tidy-flatten

# Flatten/hoist transitive inputs to root (dry run)
[group('tidy')]
tidy-flatten-dry *args:
    nix run .#flake-tidy -- flatten --dry-run {{args}}

alias flatten-dry := tidy-flatten-dry

# Check if tidy changes are needed (for CI)
[group('tidy')]
tidy-check *args:
    nix run .#flake-tidy -- all --check {{args}}

# Run flake-tidy tests
[group('tidy')]
tidy-test:
    cd pkgs/flake-tidy && uv run pytest tests/ -v

# =============================================================================
# Symlinks
# =============================================================================

# Overridable via environment variables
LINK_GIT_INPUT_DIR  := env_var_or_default("LINK_GIT_INPUT_DIR",  home_directory() / "git")
LINK_GIT_OUTPUT_DIR := env_var_or_default("LINK_GIT_OUTPUT_DIR", home_directory())

# Symlink ~/git/* into ~/ (add-only unless --force)
[group('symlinks')]
link-git-dirs *args:
    ./symlinker.sh --input-dir "{{LINK_GIT_INPUT_DIR}}" --output-dir "{{LINK_GIT_OUTPUT_DIR}}" {{args}}

alias link-git     := link-git-dirs
alias link-home    := link-git-dirs
alias link-git-dir := link-git-dirs

# Symlink repository commands into the global OpenCode command directory
[group('symlinks')]
symlink-commands *args:
    ./symlink-commands {{ args }}

# Preview OpenCode command symlink changes
[group('symlinks')]
symlink-commands-dry *args:
    ./symlink-commands --dry-run {{ args }}

# =============================================================================
# OpenCode Share — bindfs-based .opencode sharing with per-project plans/
# =============================================================================

# Share .opencode into target directories via bindfs mount
[group('opencode')]
share *args:
    nix run .#opencode-share -- {{args}}

# Unmount shared .opencode from target directories
[group('opencode')]
unshare *args:
    nix run .#opencode-share -- --unmount {{args}}

# Show mount status for target directories
[group('opencode')]
share-status *args:
    nix run .#opencode-share -- --status {{args}}

# Dry-run: show what share would do
[group('opencode')]
share-dry *args:
    nix run .#opencode-share -- --dry-run {{args}}
