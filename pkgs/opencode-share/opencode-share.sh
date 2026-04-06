#!/usr/bin/env bash
# opencode-share: Share .opencode across projects via bindfs, with per-project plans/
set -euo pipefail

PROG="opencode-share"
VERSION="1.0.0"

# Defaults
SOURCE="${OPENCODE_SHARE_SOURCE:-$HOME/.config/nix/.opencode}"
DRY_RUN=false
ACTION="mount" # mount | unmount | status

# Colors (disabled if not a terminal)
if [[ -t 1 ]]; then
  RED='\033[0;31m'
  GREEN='\033[0;32m'
  YELLOW='\033[1;33m'
  BLUE='\033[0;34m'
  BOLD='\033[1m'
  NC='\033[0m'
else
  RED='' GREEN='' YELLOW='' BLUE='' BOLD='' NC=''
fi

usage() {
  cat <<EOF
Usage: $PROG [OPTIONS] <dir> [dir...]

Share .opencode across git projects via bindfs mounts.
Each project gets the shared .opencode but keeps its own plans/ directory.

HOW IT WORKS:
  For each target directory, two bindfs mounts are created:

    1. SOURCE/.opencode  →  target/.opencode
       Shares all config, commands, instructions, plugins, themes, etc.

    2. target/plans/  →  target/.opencode/plans
       Shadows the shared plans/ with a project-local directory.

  This means target/.opencode/plans resolves to target/plans/ (the project's
  own plans), while everything else in .opencode is shared from the source.
  No absolute paths are baked into symlinks — bindfs mounts resolve correctly.

OPTIONS:
  --source DIR      Source .opencode directory
                    (default: \$OPENCODE_SHARE_SOURCE or ~/.config/nix/.opencode)
  --unmount, -u     Unmount and clean up (keeps plans/ safe)
  --status, -s      Show mount status for given directories
  --dry-run, -n     Show what would be done without doing it
                    (works even without macFUSE/bindfs installed)
  -h, --help        Show this help
  --version         Show version

UNMOUNT BEHAVIOR:
  Unmounting is safe and non-destructive:
    - Inner mount (plans/) is unmounted first, then outer (.opencode)
    - target/plans/ is NEVER deleted — your project plans are preserved
    - target/.opencode/ is removed only if empty after unmount
    - The source .opencode directory is never modified

REQUIREMENTS:
  macFUSE           macOS kernel extension providing FUSE support.
                    Installed as a homebrew cask. Requires a one-time reboot
                    and security approval after first install.
                    https://osxfuse.github.io/

  bindfs            FUSE filesystem that mirrors a directory at another location.
                    Installed via homebrew from the gromgit/fuse tap.
                    Depends on macFUSE being installed and loaded.
                    https://bindfs.org/

  Both are managed by nix-darwin in this repository.

FIRST-TIME SETUP:
  1. just switch                  # installs macfuse cask + bindfs brew
  2. Reboot your Mac              # required for macFUSE kernel extension
  3. System Settings              # Privacy & Security → Allow macFUSE
  4. just share ~/my-project      # start sharing

  After the one-time reboot, no further restarts are needed.

EXAMPLES:
  $PROG ~/operations-portal
  $PROG ~/operations-portal ~/other-project
  $PROG --unmount ~/operations-portal
  $PROG --dry-run ~/operations-portal ~/other-project
  $PROG --status ~/operations-portal
  $PROG --source /path/to/.opencode ~/project

JUST RECIPES (from the nix config repo):
  just share ~/project            # mount shared .opencode
  just unshare ~/project          # unmount (keeps plans/ safe)
  just share-status ~/project     # show what's mounted
  just share-dry ~/project        # preview without doing anything

NIX:
  nix run .#opencode-share -- ~/project           # run directly via flake app
  nix run .#opencode-share -- --help              # this help
  nix build .#packages.\$(nix eval --expr builtins.currentSystem --raw).opencode-share

ENVIRONMENT:
  OPENCODE_SHARE_SOURCE   Override default source .opencode directory
                          (default: ~/.config/nix/.opencode)
EOF
}

log()  { echo -e "${GREEN}[ok]${NC} $*"; }
warn() { echo -e "${YELLOW}[!!]${NC} $*"; }
err()  { echo -e "${RED}[er]${NC} $*" >&2; }
info() { echo -e "${BLUE}[..]${NC} $*"; }
dry()  { echo -e "${BLUE}[dry-run]${NC} $*"; }

run() {
  if $DRY_RUN; then
    dry "$*"
  else
    "$@"
  fi
}

is_mounted() {
  mount | grep -qF " on $1 (" 2>/dev/null
}

check_deps() {
  local missing=()

  if ! command -v bindfs &>/dev/null; then
    missing+=("bindfs — install via: brew install gromgit/fuse/bindfs-mac")
  fi

  # Check macFUSE is installed (kext or system extension)
  local macfuse_found=false
  [[ -d "/Library/Filesystems/macfuse.fs" ]] && macfuse_found=true
  [[ -f "/usr/local/lib/libfuse.dylib" ]] && macfuse_found=true
  [[ -f "/opt/homebrew/lib/libfuse.dylib" ]] && macfuse_found=true
  if ! $macfuse_found; then
    missing+=("macFUSE — install via: brew install --cask macfuse (reboot required)")
  fi

  if [[ ${#missing[@]} -gt 0 ]]; then
    err "Missing dependencies:"
    for dep in "${missing[@]}"; do
      err "  - $dep"
    done
    err ""
    err "If using nix-darwin, run: just switch"
    exit 1
  fi
}

do_mount() {
  local target="$1"
  local target_opencode="$target/.opencode"
  local target_plans="$target/plans"

  echo ""
  info "${BOLD}Mounting:${NC} $target"

  # Validate source
  if [[ ! -d "$SOURCE" ]]; then
    err "Source .opencode not found: $SOURCE"
    return 1
  fi

  # Validate target
  if [[ ! -d "$target" ]]; then
    err "Target directory not found: $target"
    return 1
  fi

  # If .opencode is a symlink, unlink it first
  if [[ -L "$target_opencode" ]]; then
    warn "Unlinking existing symlink: $target_opencode"
    run unlink "$target_opencode"
  fi

  # If already fully mounted, skip
  if is_mounted "$target_opencode" && is_mounted "$target_opencode/plans"; then
    log "Already fully mounted: $target_opencode (skipping)"
    return 0
  fi

  # Mount the base .opencode if not already mounted
  if ! is_mounted "$target_opencode"; then
    # Create mount point
    if [[ ! -d "$target_opencode" ]]; then
      info "Creating mount point: $target_opencode"
      run mkdir -p "$target_opencode"
    fi

    # Mount shared .opencode via bindfs
    info "bindfs $SOURCE $target_opencode"
    run bindfs "$SOURCE" "$target_opencode"

    if ! $DRY_RUN && ! is_mounted "$target_opencode"; then
      err "Failed to mount $target_opencode"
      return 1
    fi
  else
    info "Base mount already exists, checking plans..."
  fi

  # Ensure plans/ exists in source (needed as mount point inside bindfs)
  if ! $DRY_RUN && [[ ! -d "$target_opencode/plans" ]]; then
    info "Creating plans/ in source .opencode"
    mkdir -p "$SOURCE/plans"
  fi

  # Create project-local plans directory
  if [[ ! -d "$target_plans" ]]; then
    info "Creating project-local plans: $target_plans"
    run mkdir -p "$target_plans"
  fi

  # Shadow .opencode/plans with project-local plans via bindfs
  if ! is_mounted "$target_opencode/plans"; then
    info "bindfs $target_plans $target_opencode/plans"
    run bindfs "$target_plans" "$target_opencode/plans"

    if ! $DRY_RUN && ! is_mounted "$target_opencode/plans"; then
      err "Failed to mount plans overlay at $target_opencode/plans"
      return 1
    fi
  fi

  log "Shared .opencode mounted at $target_opencode"
  log "Project plans: $target_plans <-> $target_opencode/plans"
}

do_unmount() {
  local target="$1"
  local target_opencode="$target/.opencode"
  local target_plans="$target/plans"

  echo ""
  info "${BOLD}Unmounting:${NC} $target"

  # Unmount plans overlay first (inner mount must go before outer)
  if is_mounted "$target_opencode/plans"; then
    info "Unmounting plans overlay: $target_opencode/plans"
    run umount "$target_opencode/plans"
  else
    info "Plans overlay not mounted (skipping)"
  fi

  # Unmount .opencode (outer mount)
  if is_mounted "$target_opencode"; then
    info "Unmounting .opencode: $target_opencode"
    run umount "$target_opencode"
  else
    info ".opencode not mounted (skipping)"
  fi

  # Clean up empty mount point directory (only if empty)
  if [[ -d "$target_opencode" ]]; then
    if [[ -z "$(ls -A "$target_opencode" 2>/dev/null)" ]]; then
      info "Removing empty mount point: $target_opencode"
      run rmdir "$target_opencode"
    else
      warn "Mount point not empty, leaving: $target_opencode"
    fi
  fi

  # Plans dir is always preserved
  if [[ -d "$target_plans" ]]; then
    local plan_count
    plan_count=$(find "$target_plans" -maxdepth 1 -type f 2>/dev/null | wc -l | tr -d ' ')
    log "Project plans preserved: $target_plans ($plan_count files)"
  fi

  log "Unmounted .opencode from $target"
}

do_status() {
  local target="$1"
  local target_opencode="$target/.opencode"

  echo ""
  echo -e "${BOLD}$target${NC}"

  if [[ -L "$target_opencode" ]]; then
    local link_target
    link_target=$(readlink "$target_opencode")
    warn ".opencode is a symlink -> $link_target (not a bindfs mount)"
  elif is_mounted "$target_opencode"; then
    local mount_src
    mount_src=$(mount | grep " on $target_opencode (" | awk '{print $1}')
    log ".opencode: mounted (source: $mount_src)"

    if is_mounted "$target_opencode/plans"; then
      mount_src=$(mount | grep " on $target_opencode/plans (" | awk '{print $1}')
      log "plans/:   mounted (source: $mount_src)"
    else
      warn "plans/:   NOT mounted (using shared plans from source)"
    fi
  elif [[ -d "$target_opencode" ]]; then
    info ".opencode: directory exists but not mounted"
  else
    info ".opencode: not present"
  fi

  if [[ -d "$target/plans" ]]; then
    local plan_count
    plan_count=$(find "$target/plans" -maxdepth 1 -type f 2>/dev/null | wc -l | tr -d ' ')
    log "plans/:   $plan_count project-local plan files"
  else
    info "plans/:   no project-local plans directory"
  fi
}

# ─── Parse arguments ─────────────────────────────────────────────────────────

TARGETS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --source)
      SOURCE="$2"
      shift 2
      ;;
    --unmount | --umount | -u)
      ACTION="unmount"
      shift
      ;;
    --status | -s)
      ACTION="status"
      shift
      ;;
    --dry-run | -n)
      DRY_RUN=true
      shift
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    --version)
      echo "$PROG $VERSION"
      exit 0
      ;;
    --)
      shift
      # Everything after -- is a target
      for arg in "$@"; do
        TARGETS+=("$(cd "$arg" 2>/dev/null && pwd -P || echo "$arg")")
      done
      break
      ;;
    -*)
      err "Unknown option: $1"
      echo ""
      usage
      exit 1
      ;;
    *)
      # Resolve to absolute physical path
      TARGETS+=("$(cd "$1" 2>/dev/null && pwd -P || echo "$1")")
      shift
      ;;
  esac
done

if [[ ${#TARGETS[@]} -eq 0 ]]; then
  err "No target directories specified"
  echo ""
  usage
  exit 1
fi

# Check dependencies (skip for status and dry-run)
if [[ "$ACTION" != "status" ]] && ! $DRY_RUN; then
  check_deps
elif $DRY_RUN; then
  # Warn but don't fail on dry-run
  if ! command -v bindfs &>/dev/null; then
    warn "bindfs not installed (dry-run will proceed anyway)"
  fi
fi

# Resolve source to absolute physical path
SOURCE="$(cd "$SOURCE" 2>/dev/null && pwd -P || echo "$SOURCE")"

if [[ "$ACTION" == "mount" ]]; then
  info "Source: $SOURCE"
fi

# ─── Process each target ─────────────────────────────────────────────────────

ERRORS=0
for target in "${TARGETS[@]}"; do
  case "$ACTION" in
    mount) do_mount "$target" || ((ERRORS++)) ;;
    unmount) do_unmount "$target" || ((ERRORS++)) ;;
    status) do_status "$target" || ((ERRORS++)) ;;
  esac
done

echo ""
if [[ $ERRORS -gt 0 ]]; then
  err "$ERRORS target(s) had errors"
  exit 1
else
  log "Done ($ACTION ${#TARGETS[@]} target(s))"
fi
