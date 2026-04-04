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

# Format all nix files
[group('format')]
fmt:
    nixfmt *.nix pkgs/*.nix
