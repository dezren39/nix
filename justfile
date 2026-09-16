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

# Log in to Context7 (free tier: 1000 calls/month vs a lower anonymous limit)
[group('lootbox')]
context7-login:
    #!/usr/bin/env bash
    set -euo pipefail
    bin="$HOME/.local/share/lootbox/npm/node_modules/.bin/mcp-remote"
    [ -x "$bin" ] || { echo "mcp-remote missing; run 'just update-lootbox' first" >&2; exit 1; }
    auth="$HOME/.mcp-auth"
    echo "Starting mcp-remote; a browser window will open for Context7 (Clerk)."
    echo "Complete the sign-in, then this returns automatically."
    # mcp-remote is a stdio MCP server: it exits the moment stdin closes, which
    # would abort the OAuth callback. Hold stdin open with a FIFO instead.
    fifo=$(mktemp -u); mkfifo "$fifo"
    exec 3<>"$fifo"; rm -f "$fifo"
    "$bin" https://mcp.context7.com/mcp <&3 >/dev/null 2>&1 &
    pid=$!
    trap 'kill "$pid" 2>/dev/null || true; exec 3>&-' EXIT
    for _ in $(seq 1 120); do
        if find "$auth" -name '*token*' -newermt '-10 minutes' 2>/dev/null | grep -q .; then
            echo "Authenticated. Credentials cached under $auth."
            echo "Restart lootbox to pick it up: just lootbox-restart"
            exit 0
        fi
        kill -0 "$pid" 2>/dev/null || { echo "mcp-remote exited early" >&2; exit 1; }
        sleep 1
    done
    echo "Timed out after 120s without seeing cached credentials." >&2
    echo "Context7 still works anonymously; this only raises the rate limit." >&2
    exit 1

# Verify server health, configured namespaces, and Deno script execution
[group('lootbox')]
lootbox-check:
    #!/usr/bin/env bash
    set -euo pipefail
    curl -fsS http://127.0.0.1:9420/health
    namespaces=$("$HOME/.local/bin/lootbox" tools)
    printf '%s\n' "$namespaces"
    for namespace in mcp_codedb mcp_codebase_memory mcp_fff mcp_fff_worktree \
                     mcp_fff_nix mcp_chrome_devtools mcp_context7; do
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

# Spotlight: full per-repo artifact walk (not run on switch -- see simple-rebuild.sh)
[group('maintenance')]
spotlight-walk *args:
    ./spotlight-exclude-artifacts --no-system {{ args }}

# Spotlight: re-assert static markers only (user + root)
[group('maintenance')]
spotlight-system:
    ./spotlight-exclude-artifacts --system
    sudo ./spotlight-exclude-artifacts --system

# Git: deep maintenance across ~/git (reflog expiry + prune + repack)
[group('maintenance')]
git-maintain *args:
    ./git-maintain-repos --auto --deep {{ args }}

# Git: routine pass, never prunes
[group('maintenance')]
git-maintain-quick *args:
    ./git-maintain-repos --auto --quick {{ args }}

# Symlink repository commands into the global OpenCode command directory
[group('symlinks')]
symlink-commands *args:
    ./symlink-commands {{ args }}

# Preview OpenCode command symlink changes
[group('symlinks')]
symlink-commands-dry *args:
    ./symlink-commands --dry-run {{ args }}

# Symlink repository plugins into the global OpenCode plugin directory
[group('symlinks')]
symlink-plugins *args:
    ./symlink-plugins {{ args }}

# Preview OpenCode plugin symlink changes
[group('symlinks')]
symlink-plugins-dry *args:
    ./symlink-plugins --dry-run {{ args }}

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

# =============================================================================
# SidePulse
# =============================================================================

# Manually configure all SidePulse providers or one provider. `just switch` runs
# the providers selected in homeUser.nix during Home Manager activation.
[group('sidepulse')]
sidepulse-setup *args:
    nix run .#sidepulse-setup -- {{args}}

# Trigger the one-time macOS permission prompt for SidePulse LED writes.
[group('sidepulse')]
sidepulse-grant-leds:
    #!/usr/bin/env bash
    set -euo pipefail
    # LED writes go to /Volumes/PulseDot, which macOS gates behind "Removable
    # Volumes". A launchd job has no responsible app, so it cannot prompt and is
    # auto-denied -- which is why the LEDs silently never worked. Running the
    # helper from YOUR terminal gives TCC a GUI-attributable parent, so the
    # prompt actually appears. Click Allow once; the launchd jobs then inherit
    # the grant by path.
    #
    # The helper lives outside the nix store on purpose, so the grant survives
    # rebuilds instead of breaking every time python's store hash moves.
    helper="${SIDEPULSE_LED_WRITER:-$HOME/.local/share/sidepulse/led-writer/SidePulse LED Writer}"
    target="${SIDEPULSE_LED_TARGET:-/Volumes/PulseDot/LEDS.LED}"

    if [ ! -x "$helper" ]; then
      echo "LED writer not built: $helper" >&2
      echo "Run 'just sidepulse-setup opencode' first (it needs Xcode CLT clang)." >&2
      exit 1
    fi
    if [ ! -e "$target" ]; then
      echo "LED device not mounted: $target" >&2
      exit 1
    fi

    echo "Helper : $helper"
    echo "Target : $target"
    echo
    echo "Writing the file's CURRENT contents back to it, unchanged -- this only"
    echo "exists to make macOS show the permission prompt. Click Allow."
    echo

    # Round-trip the existing bytes so the LED state is not disturbed.
    current="$(cat "$target" 2>/dev/null || true)"
    if printf '%s' "$current" | "$helper" "$target"; then
      echo
      echo "Granted -- the helper can write the LED device."
      echo "The sidepulse launchd jobs will now work without further prompts."
    else
      echo
      echo "Still denied." >&2
      echo "If no prompt appeared, a Deny was already recorded. Fix it in:" >&2
      echo "  System Settings > Privacy & Security > Files and Folders" >&2
      echo "  -> 'SidePulse LED Writer' -> enable Removable Volumes" >&2
      exit 1
    fi

# =============================================================================
# OpenCode storage
# =============================================================================

# Report how much VACUUM alone would reclaim (usually ~0 until you prune). Read-only.
[group('maintenance')]
opencode-db-vacuum-report:
    ./opencode-db-vacuum.sh --dry-run

# Compact opencode.db via VACUUM INTO. Requires every opencode process stopped.
[group('maintenance')]
opencode-db-vacuum *args:
    ./opencode-db-vacuum.sh {{ args }}

# Report how much of opencode.db is superseded event snapshots. Read-only.
[group('maintenance')]
opencode-db-prune-report *args:
    ./opencode-db-prune.sh {{ args }}

# Drop superseded event snapshots, then reclaim the pages. opencode must be stopped.
[group('maintenance')]
opencode-db-prune *args:
    ./opencode-db-prune.sh --apply {{ args }}
    ./opencode-db-vacuum.sh --auto-vacuum
