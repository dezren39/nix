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

# Opens a browser for the server's OAuth login and keeps the tokens in
# ~/.mcp-auth, where lootbox and mcpx find them. Press Ctrl-C once it reports
# it is connected. Needed once per server, and again if its URL changes -- the
# URL is part of what mcp-remote stores the tokens under.
#
# Log in to an mcp-remote MCP server (datadog, atlassian, context7)
[group('lootbox')]
mcp-login name:
    #!/usr/bin/env bash
    set -euo pipefail
    url=$(jq -r --arg n "{{name}}" '.mcpServers[$n] | select(.command == "mcp-remote") | .args[0] // empty' lootbox.config.json)
    if [ -z "$url" ]; then
        echo "{{name}} is not an mcp-remote server in lootbox.config.json; those are:" >&2
        jq -r '.mcpServers | to_entries[] | select(.value.command == "mcp-remote") | "  " + .key' lootbox.config.json >&2
        exit 1
    fi
    exec "$HOME/.local/share/lootbox/npm/node_modules/.bin/mcp-remote" "$url"

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

# Three entry points into one implementation (simple-rebuild.sh), split by
# privilege: build needs no sudo and runs unattended, activate needs root.
# Each commits --allow-empty to mark that it happened -- a build names the
# store path it produced (no sudo), an activation names the generation.

# Build the darwin system without activating it (no sudo)
[group('nix')]
build:
    ./simple-rebuild.sh --build-only

# Same build, but stage only and skip the marker commit -- for iterating
# until it passes; finish with `just build` to record it.

# Build without committing (no sudo); `build-only` is an alias
[group('nix')]
build-no-commit:
    ./simple-rebuild.sh --build-only --no-commit

alias build-only := build-no-commit

# Activate ./result, which `just build` produced. The nix build inside this is
# a cache hit in that case, so it is the "switch without building again" path.
#
# It uses `darwin-rebuild switch`, not `darwin-rebuild activate`. Reading
# darwin-rebuild: `nix-env -p /nix/var/nix/profiles/system --set` runs under
# `if [ "$action" = switch ]` only, so `activate` applies the configuration
# without registering a generation -- it would not appear in
# --list-generations and could not be rolled back to. The saving is one flake
# evaluation; the cost is the undo button.
#
# With a generation number -- `just activate 283` -- it rolls back to that
# generation instead, via -G, and skips the post-activation steps, which all
# run things from the *current* flake.

# Activate what `just build` built, or roll back: `just activate [GENERATION]`
[group('nix')]
activate *generation:
    ./simple-rebuild.sh --activate-only {{ generation }}

# List system generations you could `just activate N` back to
# (from the system profile, not ./result -- listing rollback targets must
# work on a tree with no build result; see simple-rebuild.sh / #458)
[group('nix')]
generations:
    sudo /nix/var/nix/profiles/system/sw/bin/darwin-rebuild --list-generations

# Simple rebuild: build + activate (no flake update)
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
#
# `uv sync` then the venv's own interpreter, not `uv run`: `uv run` holds a
# shared lock on ~/.cache/uv for as long as its command runs, which blocks
# `uv cache prune` (clean.sh). `&&` so a failed sync stops before testing a
# stale environment; `.venv/bin/python` because a bare `python` is whatever is
# first on PATH.
[group('tidy')]
tidy-test:
    cd pkgs/flake-tidy && uv sync && .venv/bin/python -m pytest tests/ -v

# Run every plugins/*.test.mjs suite (also a flake check: plugins-tests, #414)
[group('plugins')]
plugins-test:
    for t in plugins/*.test.mjs; do echo "== $t"; node "$t" || exit 1; done

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

# clean.sh has existed unwired to any recipe. Its own --help documents the
# scopes: --auto (caches + logs + repositories), --system (caches, logs,
# package managers, colima; no repositories), --no-system (repository half
# only), --path DIR, --depth N. Repo discovery is shared with
# git-maintain-repos and spotlight-exclude-artifacts via ./git-discover-repos.

# Reclaim disk: caches, logs, build artifacts, then git maintenance
[group('maintenance')]
clean *args:
    ./clean.sh {{ args }}

# Reclaim disk: caches, logs and package managers only, no repository walk
[group('maintenance')]
clean-system *args:
    ./clean.sh --system {{ args }}

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

# Play the multi-signal LED layouts on a connected device (all nine by default, ~160s each).
# Stops the status-bar app for the duration and restarts it afterwards.
# See docs/sidepulse-leds.md for what each layout does and why.
[group('sidepulse')]
sidepulse-led-demo *args:
    #!/usr/bin/env bash
    set -euo pipefail
    # Run against the built package rather than whatever is installed, so the
    # demo shows the layouts as patched here even before a `just switch`.
    pkg="$(nix build --no-link --print-out-paths .#sidepulse-unwrapped)"
    site="$(echo "$pkg"/lib/python*/site-packages)"
    python="$(head -1 "$pkg/bin/.sidepulse-wrapped" | sed 's|^#!||')"
    PYTHONPATH="$site" "$python" scripts/sidepulse-led-demo.py {{args}}

# Trigger the one-time macOS permission prompt for SidePulse LED writes.
[group('sidepulse')]
sidepulse-grant-leds:
    #!/usr/bin/env bash
    set -euo pipefail
    # LED writes go to /Volumes/PulseDot, which macOS gates behind "Removable
    # Volumes". TCC charges that access to the *responsible process*, which by
    # default is whoever spawned the helper rather than the helper itself. Run
    # from a terminal that already holds the grant, the write simply inherits
    # it and no grant is recorded for the helper at all -- which is how this
    # recipe used to print "Granted" while having changed precisely nothing,
    # leaving the launchd jobs denied and the LEDs dark.
    #
    # The helper now re-spawns itself with the TCC disclaim flag when a write
    # is denied, so the consent prompt names "SidePulse LED Writer" and the
    # grant is keyed to its fixed path, surviving rebuilds that move python's
    # nix store hash. This recipe verifies that row exists rather than
    # trusting an exit status that cannot distinguish the two cases.
    helper="${SIDEPULSE_LED_WRITER:-$HOME/.local/share/sidepulse/led-writer/SidePulse LED Writer}"
    target="${SIDEPULSE_LED_TARGET:-/Volumes/PulseDot/LEDS.LED}"
    tcc_db="$HOME/Library/Application Support/com.apple.TCC/TCC.db"
    service="kTCCServiceSystemPolicyRemovableVolumes"

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
    write_status=0
    printf '%s' "$current" | "$helper" "$target" || write_status=$?

    # The recorded grant is what matters, not the write succeeding: a write can
    # succeed purely by inheriting this terminal's access while every launchd
    # job stays denied. Only a row keyed to the helper's own path proves the
    # grant will outlive this shell.
    if [ ! -r "$tcc_db" ]; then
      echo "Could not read TCC.db to confirm the grant:" >&2
      echo "  $tcc_db" >&2
      echo "Give this terminal Full Disk Access and re-run to verify." >&2
      exit 1
    fi

    granted="$(sqlite3 "$tcc_db" \
      "select auth_value from access where service='$service' and client='$helper';" \
      2>/dev/null || true)"

    case "$granted" in
      2)
        echo
        echo "Granted -- 'SidePulse LED Writer' holds Removable Volumes by path."
        echo "The sidepulse launchd jobs will now work without further prompts."
        ;;
      "")
        echo
        echo "No grant is recorded for the helper." >&2
        if [ "$write_status" -eq 0 ]; then
          echo "The write succeeded by inheriting this terminal's own access," >&2
          echo "which the launchd jobs do not share. Nothing was granted." >&2
        else
          echo "The write also failed (exit $write_status)." >&2
        fi
        echo "Re-run; if no prompt appears, check System Settings >" >&2
        echo "Privacy & Security > Files and Folders." >&2
        exit 1
        ;;
      *)
        echo
        echo "Denied (auth_value=$granted):" >&2
        echo "  $helper" >&2
        echo "macOS will not prompt again while a denial is recorded. Clear it in" >&2
        echo "System Settings > Privacy & Security > Files and Folders" >&2
        echo "  -> 'SidePulse LED Writer' -> enable Removable Volumes" >&2
        exit 1
        ;;
    esac

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

# =============================================================================
# OpenCode database (~/.local/share/opencode/opencode.db: every session)
# =============================================================================

# Back up the OpenCode database (APFS clone; instant, safe while it is in use)
[group('opencode')]
opencode-db-backup:
    #!/usr/bin/env bash
    set -euo pipefail
    db="$HOME/.local/share/opencode/opencode.db"
    dest="$HOME/.local/share/opencode/backup/$(date +%Y%m%dT%H%M%S)"
    mkdir -p "$dest"
    # BEGIN IMMEDIATE takes SQLite's write lock: no commit lands and the WAL
    # cannot be reset while it is held. A checkpoint can still copy WAL frames
    # into the main file between the two clones, which is harmless: the WAL
    # clone still holds every frame and replays them on open. cp -c is
    # clonefile(2): atomic, and no extra disk until the files diverge.
    /usr/bin/sqlite3 "$db" <<SQL
    .timeout 120000
    BEGIN IMMEDIATE;
    .shell /bin/cp -c "$db" "$dest/opencode.db"
    .shell if [ -f "$db-wal" ]; then /bin/cp -c "$db-wal" "$dest/opencode.db-wal"; fi
    ROLLBACK;
    SQL
    chmod 600 "$dest"/opencode.db*
    echo "backup: $dest"
    just --justfile {{ justfile() }} opencode-db-check "$dest/opencode.db"

# Session/migration counts of an OpenCode database (default: the live one)
[group('opencode')]
opencode-db-check db="":
    #!/usr/bin/env bash
    set -euo pipefail
    db="{{ db }}"
    db="${db:-$HOME/.local/share/opencode/opencode.db}"
    # Read-only; for a backup that still has its WAL, sqlite needs to create
    # the -shm beside it, which mode=ro allows in a writable directory.
    /usr/bin/sqlite3 "file:$db?mode=ro" \
      "select 'sessions', count(*) from session;" \
      "select 'migrations', count(*), max(id) from migration;"

# Migrations the locked OpenCode would apply to the live database (dry check)
[group('opencode')]
opencode-db-pending:
    #!/usr/bin/env bash
    set -euo pipefail
    src=$(nix eval --raw .#inputs.opencode.outPath 2>/dev/null || \
      nix flake prefetch --json "github:developing-today/opencode/$(jq -r .nodes.opencode.locked.rev flake.lock)" | jq -r .storePath)
    tmp=$(mktemp -d)
    grep -ho 'id: "[^"]*"' "$src"/packages/core/src/database/migration/*.ts \
      | sed 's/id: "\(.*\)"/\1/' | LC_ALL=C sort -u > "$tmp/build"
    /usr/bin/sqlite3 "file:$HOME/.local/share/opencode/opencode.db?mode=ro" \
      "select id from migration" | LC_ALL=C sort -u > "$tmp/db"
    echo "pending (the build will apply these):"
    LC_ALL=C comm -23 "$tmp/build" "$tmp/db" | sed 's/^/  /'
    unknown=$(LC_ALL=C comm -13 "$tmp/build" "$tmp/db")
    rm -rf "$tmp"
    if [ -n "$unknown" ]; then
      echo "APPLIED BUT UNKNOWN TO THIS BUILD (build is older than the database):"
      echo "$unknown" | sed 's/^/  /'
      exit 1
    fi
    echo "ok: every applied migration is known to this build"

# OpenChamber agent, its OpenCode, and every process holding an OpenCode DB
[group('opencode')]
openchamber-status:
    #!/usr/bin/env bash
    set -uo pipefail
    job="gui/$(id -u)/org.nixos.openchamber"
    echo "== agent $job"
    launchctl print "$job" 2>/dev/null | grep -E '^\s+(state|pid|last exit code|runs) =' || echo "  not loaded"
    echo "== link: $(readlink ~/.local/state/openchamber/bin/opencode 2>/dev/null || echo missing)"
    echo "== deployed: $(cat ~/.local/state/openchamber/deployed 2>/dev/null || echo none)"
    echo "== http://127.0.0.1:3000"
    code=$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:3000/ 2>/dev/null) || code=""
    echo "  ${code:+HTTP $code}${code:-not answering}"
    echo "== processes with an OpenCode database open (must all be the same file)"
    lsof -nP -c opencode -c .opencode -c node -c OpenChamber 2>/dev/null \
      | awk '{print "  " $1, $2, $NF}' | grep -E '/opencode[^/]*\.db$' | sort -u
    echo "== database"
    just --justfile {{ justfile() }} opencode-db-check | sed 's/^/  /'

# After the first switch to the fork build: point the live OpenCode config
# back at this repo's opencode-global.jsonc (temporarily a plain copy of the old v1
# config so the old OpenChamber.app's upstream OpenCode kept working)
#
# Refuses before that switch: this repo's config routes every agent through
# opencode-route/* pools (experimental.model_routes), which only the fork
# implements, and the live server re-reads the file as soon as it changes --
# linking early would leave it with models it cannot resolve. The deploy link
# below exists only once the switch has installed the fork. FORCE=1 overrides.
#
# The live file it replaces is a hand-edited plain copy with no other home, so
# it is kept as opencode.jsonc.bak.<timestamp> next to it, never overwritten.

# Point the live OpenCode config at this repo's opencode-global.jsonc (after the fork switch)
[group('opencode')]
opencode-config-link:
    #!/usr/bin/env bash
    set -euo pipefail
    target="$HOME/.config/opencode/opencode.jsonc"
    deploy="$HOME/.local/state/openchamber/bin/opencode"
    if [ ! -e "$deploy" ] && [ "${FORCE:-0}" != "1" ]; then
        echo "refusing: $deploy does not exist, so the fork OpenCode is not deployed yet." >&2
        echo "Run the darwin-rebuild switch first (FORCE=1 to link anyway)." >&2
        exit 1
    fi
    if [ -e "$target" ] && [ ! -L "$target" ]; then
        backup="$target.bak.$(date -u +%Y-%m-%dT%H-%M-%SZ)"
        cp -p "$target" "$backup"
        echo "backed up the live config to $backup"
    fi
    ln -sfn "{{ justfile_directory() }}/opencode-global.jsonc" "$target"
    ls -la "$target"
