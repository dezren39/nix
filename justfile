# Show available recipes
default: list

list:
  @just --list

# =============================================================================
# Utilities - Lootbox
# =============================================================================

# Reinstall/update lootbox binary from latest source
[group('lootbox')]
update-lootbox:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Updating lootbox..."
    curl -fsSL https://raw.githubusercontent.com/jx-codes/lootbox/main/install.sh | bash
    echo "lootbox updated to $(lootbox --version)"

# Start lootbox server (if not already running)
[group('lootbox')]
lootbox-server:
    #!/usr/bin/env bash
    set -euo pipefail
    if lsof -iTCP:9420 -sTCP:LISTEN -t &>/dev/null; then
        echo "Lootbox server already running on port 9420"
    else
        echo "Starting lootbox server..."
        nohup lootbox server --port 9420 &>/dev/null &
        disown
        sleep 1
        if lsof -iTCP:9420 -sTCP:LISTEN -t &>/dev/null; then
            echo "Lootbox server started on port 9420"
        else
            echo "Lootbox server may still be starting up..."
        fi
    fi

# Kill running lootbox server
[group('lootbox')]
lootbox-kill:
    #!/usr/bin/env bash
    set -euo pipefail
    pid=$(lsof -iTCP:9420 -sTCP:LISTEN -t 2>/dev/null || true)
    if [ -n "$pid" ]; then
        kill "$pid"
        echo "Killed lootbox server (PID $pid)"
    else
        echo "No lootbox server running on port 9420"
    fi

# Restart lootbox server
[group('lootbox')]
lootbox-restart: lootbox-kill lootbox-server

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
