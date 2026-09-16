#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
#
# clean.sh — reclaim disk: caches, logs, build artifacts, then git maintenance.
#
# STRUCTURE
#   1. system/user caches (brew, nix, docker, package managers, macOS)
#   2. logs
#   3. per-repo work: .trash, build artifacts, Spotlight markers
#   4. git maintenance, last, because it is the slowest part
#
# Discovery for step 3/4 is shared with git-maintain-repos and
# spotlight-exclude-artifacts via ./git-discover-repos, and the directory-name
# lists come from lib/common.sh. None of the three keeps its own copy.

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Flags. Vocabulary matches git-maintain-repos and spotlight-exclude-artifacts.
# ---------------------------------------------------------------------------
SCOPE="auto"          # auto | system | no-system
AUTO_SET=0
SCOPE_SET=0
TARGET_PATH=""
DEPTH_LIMIT=""
RECURSIVE_SET=0
FOLLOW=""
DRY_RUN=0
DO_MAINTENANCE=1
MAINT_DEPTH="--deep"
REPO_FLAGS_USED=""
USE_SUDO=""          # empty = decide from --dry-run

usage() {
  cat <<'EOF'
clean.sh — reclaim disk space

USAGE
  ./clean                          everything (default; same as --auto)
  ./clean --path DIR               per-repo work for DIR only
  ./clean --system                 caches/logs only, touch no repositories
  ./clean --no-system              repositories only, skip caches/logs
  ./clean --dry-run                report, change nothing

SCOPE (mutually exclusive; --auto names the default)
  --auto            Caches + logs + repositories. Passing it with --system or
                    --no-system is an error.
  --system          Caches, logs, package managers, colima. No repositories.
  --no-system       Repository half only: .trash, artifacts, markers, git
                    maintenance across the full discovered set.

DISCOVERY (shared with git-maintain-repos / spotlight-exclude-artifacts)
  --path DIR        Restrict repository work to DIR. Skips the cache/log half
                    and the global maintenance --system pass. Implies
                    --no-follow.
  --recursive       Descend without limit. Default.
  --no-recursive    Equivalent to --depth 1 (path + direct children).
  --depth N         Explicit find -maxdepth.
  --follow          Hop worktree -> real repo -> its other worktrees.
                    Default under --auto.
  --no-follow       Stay put. Default under --path.

MAINTENANCE
  --no-git-maintenance
                    Skip the git-maintain-repos pass entirely, leaving clean.sh
                    doing exactly what it did before maintenance was wired in.
                    Synonyms: --no-maintenance, --no-git.
  --quick           Run maintenance at --quick instead of --deep.
  --dry-run         Print what would happen; deletes nothing.

TUNABLES (environment)
  ARTIFACT_STALE_DAYS  default 7   repo idle period before artifacts are removed
  CACHE_STALE_DAYS     default 3   age gate for nix caches / flake GC roots
  LOG_STALE_DAYS       default 0   0 = drop all logs, N = keep last N days
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --auto)           AUTO_SET=1 ;;
    --system)         SCOPE="system"; SCOPE_SET=1 ;;
    --no-system)      SCOPE="no-system"; SCOPE_SET=1 ;;
    --path)           TARGET_PATH="${2:-}"; REPO_FLAGS_USED="$REPO_FLAGS_USED --path"; shift ;;
    --recursive|--walk) REPO_FLAGS_USED="$REPO_FLAGS_USED --recursive"; DEPTH_LIMIT=""; RECURSIVE_SET=1 ;;
    --no-recursive|--no-walk) DEPTH_LIMIT=1; RECURSIVE_SET=1 ;;
    --depth)          DEPTH_LIMIT="${2:-1}"; RECURSIVE_SET=1; REPO_FLAGS_USED="$REPO_FLAGS_USED --depth"; shift ;;
    --follow)         FOLLOW=1; REPO_FLAGS_USED="$REPO_FLAGS_USED --follow" ;;
    --no-follow)      FOLLOW=0; REPO_FLAGS_USED="$REPO_FLAGS_USED --no-follow" ;;
    --sudo)           USE_SUDO=1 ;;
    --no-sudo)        USE_SUDO=0 ;;
    --no-git-maintenance|--no-maintenance|--no-git) DO_MAINTENANCE=0 ;;
    --quick)          MAINT_DEPTH="--quick" ;;
    --dry-run)        DRY_RUN=1 ;;
    -h|--help)        usage; exit 0 ;;
    -*)               echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    *)                TARGET_PATH="$1" ;;
  esac
  shift
done

# --system touches no repositories, so repository-scoped flags are meaningless
# alongside it. Erroring beats silently ignoring them.
if [ "$SCOPE" = "system" ] && [ -n "$REPO_FLAGS_USED" ]; then
  echo "clean.sh: --system conflicts with repository-scoped flags:$REPO_FLAGS_USED" >&2
  echo "          (--system does caches/logs only and opens no repository)" >&2
  exit 2
fi

# --dry-run writes nothing, so it should not demand a password. --sudo forces
# escalation anyway; combined with --dry-run that is a no-op and says so.
if [ -z "$USE_SUDO" ]; then
  if [ "$DRY_RUN" = "1" ]; then USE_SUDO=0; else USE_SUDO=1; fi
elif [ "$USE_SUDO" = "1" ] && [ "$DRY_RUN" = "1" ]; then
  echo "clean.sh: --sudo with --dry-run is a no-op (nothing is written either way)" >&2
fi

if [ "$AUTO_SET" = "1" ] && [ "$SCOPE_SET" = "1" ]; then
  echo "clean.sh: --auto conflicts with --system/--no-system" >&2
  echo "          (--auto just names the default: caches + logs + repositories)" >&2
  exit 2
fi
# --path is inherently repository-scoped.
[ -n "$TARGET_PATH" ] && [ "$SCOPE" = "auto" ] && SCOPE="no-system"

DO_CACHES=1; DO_REPOS=1
case "$SCOPE" in
  system)    DO_REPOS=0 ;;
  no-system) DO_CACHES=0 ;;
esac

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
C_WARN=$'\033[1;33m'; C_INFO=$'\033[0;36m'; C_OFF=$'\033[0m'
warn() { printf '%s⚠️  %s%s\n' "$C_WARN" "$*" "$C_OFF" >&2; }
info() { printf '%sℹ️  %s%s\n' "$C_INFO" "$*" "$C_OFF"; }

get_free_bytes() { df -k / | awk 'NR==2 {print $4 * 1024}'; }

# On Apple Silicon the real prefix is /opt/homebrew. A stale /usr/local/bin/brew
# can shadow it on PATH -- it did here, so `brew cleanup` was running against an
# almost-empty prefix (1 cask) while the actual install had 71. Always resolve
# the prefix with the most casks rather than trusting PATH.
real_brew() {
  local best="" bestn=-1 p n
  for p in /opt/homebrew/bin/brew /usr/local/bin/brew "$(command -v brew 2>/dev/null)"; do
    [ -x "$p" ] || continue
    n=$(ls "$("$p" --prefix 2>/dev/null)/Caskroom" 2>/dev/null | wc -l | tr -d ' ')
    [ "${n:-0}" -gt "$bestn" ] && { bestn="${n:-0}"; best="$p"; }
  done
  printf '%s' "$best"
}
brew_cleanup() {
  local b; b="$(real_brew)"
  if [ -z "$b" ]; then info "brew not installed, skipping"; return 0; fi
  echo "🍺 brew cleanup ($b, prefix $("$b" --prefix 2>/dev/null))"
  "$b" cleanup --prune=all || warn "brew cleanup failed"
  # Casks whose app bundle is gone leave Caskroom metadata and broken symlinks.
  "$b" cleanup --prune=all --quiet 2>/dev/null || true
}

human_size() {
  local bytes=$1
  if (( bytes >= 1073741824 )); then printf "%.2f GB" "$(echo "scale=2; $bytes / 1073741824" | bc)"
  elif (( bytes >= 1048576 )); then printf "%.2f MB" "$(echo "scale=2; $bytes / 1048576" | bc)"
  else printf "%d KB" "$(( bytes / 1024 ))"; fi
}

# Every destructive action funnels through these so --dry-run is honoured
# uniformly and a missing tool warns instead of aborting the run.
run() {
  if [ "$DRY_RUN" = "1" ]; then echo "  would run: $*"; return 0; fi
  "$@"
}
del() {
  local p
  for p in "$@"; do
    [ -e "$p" ] || continue
    if [ "$DRY_RUN" = "1" ]; then echo "  would delete: ${p/#$HOME/\~}"; continue; fi
    chmod -R u+w "$p" 2>/dev/null
    rm -rf "$p" 2>/dev/null || warn "could not fully remove $p"
  done
}
# have CMD -- true if runnable. Missing tools are informational, never fatal.
have() { command -v "$1" >/dev/null 2>&1; }

# ---------------------------------------------------------------------------
# Privilege handling
# ---------------------------------------------------------------------------
if [ "$EUID" -ne 0 ]; then
  DISK_FREE_START="$(get_free_bytes)"
  echo "📏 Free disk before clean: $(human_size "$DISK_FREE_START")"
  # Root-only paths simply report as inaccessible when unprivileged.
  if [ "$USE_SUDO" = "0" ]; then
    info "no-sudo: staying unprivileged; root-owned paths will be skipped"
    export CALLER_USER_HOME="$(realpath ~)"
  else
    if [ "$DO_CACHES" = "1" ]; then brew_cleanup; fi
    CALLER_USER_HOME="$(realpath ~)" DISK_FREE_START="$DISK_FREE_START" \
      ARTIFACT_STALE_DAYS="${ARTIFACT_STALE_DAYS:-7}" LOG_STALE_DAYS="${LOG_STALE_DAYS:-0}" \
      CACHE_STALE_DAYS="${CACHE_STALE_DAYS:-3}" \
      sudo --preserve-env=CALLER_USER_HOME,DISK_FREE_START,ARTIFACT_STALE_DAYS,LOG_STALE_DAYS,CACHE_STALE_DAYS \
        "$0" "$@"
    exit $?
  fi
fi

ROOT_HOME="$(realpath ~)"
if [ -z "${CALLER_USER_HOME:-}" ]; then
  if [ -n "${SUDO_USER:-}" ]; then CALLER_USER_HOME="$(eval echo "~$SUDO_USER")"
  else CALLER_USER_HOME="$ROOT_HOME"; fi
fi
CALLER_USER="${SUDO_USER:-$(basename "$CALLER_USER_HOME")}"

# Anything that CREATES files must run as the caller. A previous root run left
# ~330 MB of root-owned files in ~/.cache that the user could not delete.
as_user() { sudo -u "$CALLER_USER" env HOME="$CALLER_USER_HOME" "$@"; }
as_user_have() { sudo -u "$CALLER_USER" env HOME="$CALLER_USER_HOME" sh -c "command -v $1 >/dev/null 2>&1"; }

ARTIFACT_STALE_DAYS="${ARTIFACT_STALE_DAYS:-7}"
CACHE_STALE_DAYS="${CACHE_STALE_DAYS:-3}"
LOG_STALE_DAYS="${LOG_STALE_DAYS:-0}"

# shellcheck source=lib/common.sh
. "$SELF_DIR/lib/common.sh"
build_name_expr CACHE_EXPR "${CACHE_NAMES[@]}"
build_name_expr RISK_EXPR  "${RISKY_NAMES[@]}"
build_name_expr TRASH_EXPR "${TRASH_NAMES[@]}"

#############################################################################
# HALF 1 — caches, logs, package managers
#############################################################################
if [ "$DO_CACHES" = "1" ]; then

echo "🐳 docker"
if have docker; then
  run docker container prune --force || warn "docker container prune failed"
  run docker builder prune --force   || warn "docker builder prune failed"
  run docker system prune --force --all --volumes || warn "docker system prune failed"
else
  info "docker not installed, skipping"
fi

# --- colima -----------------------------------------------------------------
# The VM's sparse disks never shrink: `docker system prune -a` frees blocks
# inside the guest, but the host-side file keeps them allocated. Measured 4.5 GB
# of disk images against 0 images / 0 containers / 0 volumes. There is no
# compaction command (`colima prune` only drops cached downloaded assets), so
# recreating the VM is the only way to reclaim it. Guarded on the VM being
# genuinely empty.
if have colima && have docker; then
  running_containers="$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')"
  live_volumes="$(docker volume ls -q 2>/dev/null | wc -l | tr -d ' ')"
  colima_size_kb="$(du -sk "$CALLER_USER_HOME/.colima" 2>/dev/null | cut -f1)"
  colima_size_kb="${colima_size_kb:-0}"
  if [ "${running_containers:-1}" = "0" ] && [ "${live_volumes:-1}" = "0" ] && [ "$colima_size_kb" -gt 1048576 ]; then
    echo "🐳 colima: $(human_size $((colima_size_kb * 1024))) of disk images, 0 containers, 0 volumes — recreating"
    run as_user colima stop        || warn "colima stop failed"
    run as_user colima delete -f   || warn "colima delete failed"
    # setsid + nohup so the fresh VM outlives this script.
    if [ "$DRY_RUN" = "1" ]; then echo "  would run: colima start (detached)"
    else
      as_user nohup colima start >/tmp/colima-clean-start.log 2>&1 &
      disown 2>/dev/null || true
      info "colima start running detached (log: /tmp/colima-clean-start.log)"
    fi
  else
    info "colima: ${running_containers} containers, ${live_volumes} volumes — leaving VM alone"
  fi
else
  info "colima/docker not both present, skipping VM recreate"
fi

# --- Tier 1: purely derived, no network state -------------------------------
echo "🧽 Tier 1 caches"
del "$CALLER_USER_HOME/Library/Developer/Xcode/DerivedData"
del "$CALLER_USER_HOME/.cache/gh" "$CALLER_USER_HOME/.cache/zig" \
    "$CALLER_USER_HOME/.cache/cmux" "$CALLER_USER_HOME/.cache/huggingface" \
    "$CALLER_USER_HOME/.cache/opencode.bak"
for _l in "$CALLER_USER_HOME"/.cache/fff_mcp*.log; do [ -e "$_l" ] && del "$_l"; done
del "$CALLER_USER_HOME/Library/Application Support/Code/Cache" \
    "$CALLER_USER_HOME/Library/Application Support/Code/CachedData" \
    "$CALLER_USER_HOME/Library/Application Support/Cursor/Cache" \
    "$CALLER_USER_HOME/Library/Application Support/Cursor/CachedData"
# .m2 may hold internal Artifactory artifacts that are gone upstream. Included
# deliberately: if the registry dropped them, they are not wanted either.
del "$CALLER_USER_HOME/.m2"
del "$CALLER_USER_HOME/.copilot/pkg" "$CALLER_USER_HOME/.copilot/logs"
del "$CALLER_USER_HOME/.codedb/projects"
del "$CALLER_USER_HOME/Library/Application Support/rancher-desktop"
del "$CALLER_USER_HOME/Library/Caches" "$CALLER_USER_HOME/Library/Logs"
del "$ROOT_HOME/Library/Caches" "$ROOT_HOME/Library/Logs"
del /Library/Caches/Homebrew
del /var/vm/sleepimage

# --- Tier 2: native prune, never rm -rf --------------------------------------
# These know which entries are still referenced. Blanket removal would force a
# full redownload and, for pnpm, break hardlinked node_modules already on disk.
echo "🧽 Tier 2 caches (native prune)"
prune_with() {
  local label="$1"; shift
  local bin="$1"
  if as_user_have "$bin"; then
    run as_user "$@" || warn "$label prune failed"
  else
    info "$label not installed, skipping"
  fi
}
prune_with npm   npm cache verify
prune_with uv    uv cache prune
prune_with pnpm  pnpm store prune      # NEVER rm -rf: breaks hardlinked node_modules in place
prune_with yarn  yarn cache clean
prune_with go    go clean -modcache
prune_with cargo-cache cargo cache -r all   # consider --autoclean-expensive for a gentler pass
prune_with pip   pip cache purge
prune_with pip3  pip3 cache purge
prune_with pipx  pipx uninstall-all --help  # probe only; destructive variants left out
prune_with deno  deno clean
prune_with composer composer clear-cache
prune_with gem   gem cleanup
prune_with nuget dotnet nuget locals all --clear
prune_with gradle_stop gradle --stop

# Both homebrew prefixes: nix-homebrew with enableRosetta=true provisions an
# arm64 prefix (/opt/homebrew) AND an x86_64 Rosetta prefix (/usr/local). Only
# cleaning whichever one PATH happens to resolve leaves the other untouched.
for _bp in /opt/homebrew/bin/brew /usr/local/bin/brew; do
  [ -x "$_bp" ] || continue
  echo "🍺 brew cleanup: $("$_bp" --prefix 2>/dev/null)"
  run as_user "$_bp" cleanup --prune=all || warn "brew cleanup failed for $_bp"
done
# bun refuses to run outside a project, so hand it a throwaway one.
if as_user_have bun; then
  if [ "$DRY_RUN" = "1" ]; then echo "  would run: bun pm cache rm"
  else
    BUN_TMP="$(mktemp -d)"; chown "$CALLER_USER" "$BUN_TMP"
    as_user sh -c "cd '$BUN_TMP' && printf '{\"name\":\"t\",\"version\":\"1.0.0\"}' > package.json && bun pm cache rm" \
      || warn "bun cache prune failed"
    rm -rf "$BUN_TMP"
  fi
else
  info "bun not installed, skipping"
fi

# --- VS Code extensions -----------------------------------------------------
# There is no `code --prune`: the CLI only offers --list/--install/--uninstall,
# so superseded versions accumulate forever. Measured here: azureterraform had
# 0.7.0 AND 0.9.0 (1.7 GB), sonarlint two copies (739 MB) -- 1.62 GB total.
#
# Superseded versions are always removed: VS Code never re-creates an old
# version, so this is a one-way win.
#
# VSCODE_PRUNE_HEAVY additionally deletes the big extensions outright. It is
# OFF by default on purpose: VS Code re-downloads them on next launch, so a
# clean run would delete them again next time -- a perpetual redownload loop
# rather than a saving. Set VSCODE_PRUNE_HEAVY=1 for a one-off reclaim.
VSCODE_EXT_DIRS=(
  "$CALLER_USER_HOME/.vscode/extensions"
  "$CALLER_USER_HOME/.vscode-insiders/extensions"
  "$CALLER_USER_HOME/.cursor/extensions"
)
VSCODE_HEAVY_GLOBS=(
  'ms-*terraform-*' 'ms-dotnettools.*' 'ms-azuretools.*'
  'codestream.codestream-*' 'sonarsource.*' 'asf.*' 'redhat.*'
)
prune_vscode_extensions() {
  local dir="$1" base old freed=0 k
  [ -d "$dir" ] || return 0
  # Superseded versions: strip the trailing -X.Y.Z[-platform] to group, keep
  # the highest by version sort.
  while IFS= read -r base; do
    local vers=(); while IFS= read -r v; do vers+=("$v"); done \
      < <(ls -1d "$dir/$base"-* 2>/dev/null | sort -V)
    [ ${#vers[@]} -lt 2 ] && continue
    for old in "${vers[@]:0:$((${#vers[@]}-1))}"; do
      k=$(du -sk "$old" 2>/dev/null | cut -f1); freed=$((freed + ${k:-0}))
      del "$old"
    done
  done < <(ls -1 "$dir" 2>/dev/null | sed -E 's/-[0-9]+\.[0-9]+\.[0-9]+(-.*)?$//' | sort | uniq -d)
  info "$(basename "$(dirname "$dir")"): superseded versions freed $((freed / 1024)) MB"

  if [ "${VSCODE_PRUNE_HEAVY:-0}" = "1" ]; then
    local g
    for g in "${VSCODE_HEAVY_GLOBS[@]}"; do
      for old in "$dir"/$g; do [ -d "$old" ] && del "$old"; done
    done
    info "$(basename "$(dirname "$dir")"): heavy globs removed (will redownload)"
  fi
}
echo "🧩 VS Code extensions"
for _d in "${VSCODE_EXT_DIRS[@]}"; do prune_vscode_extensions "$_d"; done

# --- orphaned brew casks / mas apps -----------------------------------------
# Casks present on disk but absent from casks.nix are drift: usually a rename
# (handbrake vs handbrake-app) leaving BOTH installed, or something commented
# out that was never uninstalled because homebrew.onActivation.cleanup="none".
# Reported, never auto-removed: deleting an app is not a cache operation.
if [ -n "$(real_brew)" ] && [ -f "$SELF_DIR/casks.nix" ]; then
  _b="$(real_brew)"
  _orphans=$(comm -23 \
    <(as_user "$_b" list --cask 2>/dev/null | sort) \
    <(grep -oE '^[[:space:]]*"[^"]+"' "$SELF_DIR/casks.nix" | tr -d ' "' | sort) 2>/dev/null)
  if [ -n "$_orphans" ]; then
    warn "casks installed but not in casks.nix (uninstall with: $_b uninstall --cask <name>)"
    printf '     %s\n' $_orphans
  fi
fi
if have mas && [ -f "$SELF_DIR/masApps.nix" ]; then
  _masorph=$(comm -23 \
    <(as_user mas list 2>/dev/null | awk '{$1=""; sub(/ *\([^)]*\)$/,""); sub(/^ /,""); print}' | sort) \
    <(grep -oE '"[^"]+" = [0-9]+' "$SELF_DIR/masApps.nix" | sed -E 's/" = .*//; s/^"//' | sort) 2>/dev/null)
  [ -n "$_masorph" ] && { warn "App Store apps not in masApps.nix:"; printf '     %s\n' $_masorph; }
fi

# --- Other language/tool caches, user + system ------------------------------
# Present-but-unlisted tools get cleaned; absent ones are informational only.
echo "🧽 Language/tool caches"
for _rel in \
  .gradle/caches .nuget/packages .dotnet .gem .bundle .cache/pip .ivy2/cache \
  .sbt .stack-work .cocoapods/repos .pub-cache .deno .cache/yarn .cache/Homebrew \
  .rustup/toolchains/.tmp .cache/ms-playwright .cache/puppeteer .cache/electron \
  .cache/node-gyp .cache/typescript .cache/deno .cache/prisma .cache/JetBrains
do
  for _base in "$CALLER_USER_HOME" "$ROOT_HOME"; do
    [ -d "$_base/$_rel" ] && del "$_base/$_rel"
  done
done
for _sys in /Library/Caches/com.apple.dt.Xcode /System/Volumes/Data/Library/Caches; do
  [ -d "$_sys" ] && del "$_sys"
done

# --- Tier 3: stale-gated nix caches -----------------------------------------
# nix-collect-garbage manages /nix/store only; it never touches ~/.cache/nix,
# so this grows without bound.
echo "🧽 Tier 3 caches (older than ${CACHE_STALE_DAYS}d)"
for _c in "$CALLER_USER_HOME"/.cache/nix/eval-cache-*; do
  [ -d "$_c" ] || continue
  if [ -z "$(find "$_c" -type f -mtime "-${CACHE_STALE_DAYS}" -print -quit 2>/dev/null)" ]; then
    del "$_c"
  fi
done

# tarball-cache is a BARE GIT REPO of fetched flake inputs. `gc --prune` alone
# reclaims almost nothing, because nix writes a ref per input and refs keep
# objects reachable forever -- regardless of whether any flake.lock still points
# at them. To actually age it out the stale REFS must go first; then
# git-maintain-repos --deep does the repack and prune.
# Safe: if a flake.lock still pins an expired input, nix simply refetches it.
TARBALL_CACHE="$CALLER_USER_HOME/.cache/nix/tarball-cache"
if [ -d "$TARBALL_CACHE" ]; then
  cutoff=$(( $(date +%s) - CACHE_STALE_DAYS * 86400 ))
  expired=0
  while IFS=' ' read -r ref ts; do
    [ -n "$ref" ] || continue
    if [ "${ts:-0}" -lt "$cutoff" ]; then
      if [ "$DRY_RUN" = "1" ]; then echo "  would expire ref: $ref"
      else as_user git -C "$TARBALL_CACHE" update-ref -d "$ref" 2>/dev/null && expired=$((expired + 1)); fi
    fi
  done < <(as_user git -C "$TARBALL_CACHE" for-each-ref \
             --format='%(refname) %(committerdate:unix)' 2>/dev/null)
  info "tarball-cache: expired $expired refs older than ${CACHE_STALE_DAYS}d"
  run "$SELF_DIR/git-maintain-repos" --path "$TARBALL_CACHE" --no-recursive --no-system --deep --no-register \
    || warn "tarball-cache maintenance failed"
fi

# Stale flake GC roots: a `nix build` result symlink or direnv profile in a
# directory nobody has touched pins its whole closure in /nix/store. Dropping
# the root lets the next nix-collect-garbage reclaim it.
echo "🧽 Stale flake GC roots (>${CACHE_STALE_DAYS}d)"
gcroot_dir=/nix/var/nix/gcroots/auto
if [ -d "$gcroot_dir" ]; then
  freed=0
  while IFS= read -r link; do
    target="$(readlink "$link" 2>/dev/null)" || continue
    [ -n "$target" ] || continue
    # The root points at a result symlink / .direnv profile inside a project.
    if [ ! -e "$target" ]; then
      del "$link"; freed=$((freed + 1)); continue
    fi
    owner_dir="$(dirname "$target")"
    case "$target" in
      */.direnv/*|*/result|*/result-*)
        if [ -z "$(find "$owner_dir" -maxdepth 2 -type f -mtime "-${CACHE_STALE_DAYS}" -print -quit 2>/dev/null)" ]; then
          del "$link" "$target"; freed=$((freed + 1))
        fi
        ;;
    esac
  done < <(find "$gcroot_dir" -maxdepth 1 -type l 2>/dev/null)
  info "flake GC roots released: $freed"
fi

# nix garbage collection runs LAST of the nix work, deliberately: expiring
# tarball-cache refs and releasing stale flake GC roots above is what makes
# paths collectable, and doing this first would defer every one of those
# reclaims to the NEXT run.
#
# nix-collect-garbage vs `nix store gc`: same collector underneath. The old CLI
# additionally understands -d (delete old profile generations), which is where
# most reclaimable space actually comes from; `nix store gc` is still flagged
# experimental and has no generation handling. Use both -- -d for generations,
# then a plain sweep for anything left unreferenced.
# -d deletes EVERY old generation, so the only rollback target left is the one
# currently active -- a bad switch then has nowhere to go back to. Keeping a
# window costs little (generations share almost all their store paths) and
# preserves rollback. NIX_KEEP_DAYS=0 restores the old scorched-earth -d.
# nix has no single flag for "newer than N days AND at least M generations":
# --delete-older-than is purely age-based, so a quiet fortnight would leave the
# current generation as the only rollback target. Widen the age window until at
# least NIX_KEEP_GENS old generations survive, then hand that to nix.
# NIX_KEEP_DAYS=0 restores the old scorched-earth -d.
NIX_KEEP_DAYS="${NIX_KEEP_DAYS:-7}"
NIX_KEEP_GENS="${NIX_KEEP_GENS:-5}"

nix_keep_window() {
  local profile="$1" days="$NIX_KEEP_DAYS" kept
  local now cutoff
  now=$(date +%s)
  while [ "$days" -lt 3650 ]; do
    cutoff=$(( now - days * 86400 ))
    kept=0
    while IFS= read -r line; do
      # "  42   2026-08-01 12:00:00   (current)"
      local gdate; gdate=$(echo "$line" | awk '{print $2" "$3}')
      local gts; gts=$(date -j -f "%Y-%m-%d %H:%M:%S" "$gdate" +%s 2>/dev/null) || continue
      [ "$gts" -ge "$cutoff" ] && kept=$((kept + 1))
    done < <(nix-env --list-generations --profile "$profile" 2>/dev/null)
    # kept includes the current generation, so require KEEP_GENS + 1
    [ "$kept" -ge $(( NIX_KEEP_GENS + 1 )) ] && break
    days=$(( days * 2 ))
  done
  printf '%s' "$days"
}

if [ "$NIX_KEEP_DAYS" = "0" ]; then
  echo "🧊 nix store garbage collection (deleting ALL old generations)"
  run nix-collect-garbage -d || warn "nix-collect-garbage failed"
else
  _win="$(nix_keep_window /nix/var/nix/profiles/system)"
  if [ "$_win" != "$NIX_KEEP_DAYS" ]; then
    info "widened generation window ${NIX_KEEP_DAYS}d -> ${_win}d to keep >=${NIX_KEEP_GENS} generations"
  fi
  echo "🧊 nix store garbage collection (keeping last ${_win}d / >=${NIX_KEEP_GENS} generations)"
  run nix-collect-garbage --delete-older-than "${_win}d" \
    || warn "nix-collect-garbage --delete-older-than failed"
fi
run nix-store --gc || warn "nix-store --gc failed"

# --- Deliberately left alone ------------------------------------------------
# ~/.cache/opencode  - live runtime package cache (~420 MB) for the editor in
#                      daily use; removing it mid-session is not worth it.
# ~/.whisper/models  - static ASR weights (~6.2 GB). Artifacts, not a cache:
#                      mtime never advances on load, so staleness cannot tell
#                      "unused" from "loaded right now". If space is ever
#                      needed, gate on a long window (180d+) or delete a
#                      specific model by name; they refetch from HuggingFace at
#                      multi-GB cost.
# ~/.m2 corporate    - see note above; included by explicit request.
# ~/.cargo/git       - a force-pushed or deleted git dependency cannot come back.
# ~/.local/share/opencode/opencode.db - session database, not a cache.

#############################################################################
# HALF 1b — logs
#############################################################################
echo "🧻 logs"
del "$CALLER_USER_HOME/git/oseries"
OPS_LOG_DIR="$CALLER_USER_HOME/.cache/operations-portal/logs"
if [ -d "$OPS_LOG_DIR" ]; then
  # operations-portal rotates daily but never prunes: retention_days is defined
  # in config yet never read by app/utils/logging.py, so rotated logs accumulate
  # forever. With a DEBUG default level this reached 60 GB.
  if [ "$LOG_STALE_DAYS" -eq 0 ]; then
    del "$OPS_LOG_DIR"
  else
    if [ "$DRY_RUN" = "1" ]; then echo "  would delete logs older than ${LOG_STALE_DAYS}d in $OPS_LOG_DIR"
    else
      find "$OPS_LOG_DIR" -type f -mtime "+${LOG_STALE_DAYS}" -delete 2>/dev/null
      find "$OPS_LOG_DIR" -mindepth 1 -type d -empty -delete 2>/dev/null
    fi
  fi
fi

echo "🧻 /private/var/folders"
if [ "$DRY_RUN" = "1" ]; then echo "  would clear /private/var/folders (keeping zz)"
else
  folders=/private/var/folders
  for i in "$folders"/*; do
    [ "$(basename "$i")" != "zz" ] && rm -rf "$i" 2>/dev/null
  done
  rm -rf "$folders/zz/"* 2>/dev/null
fi

del "$CALLER_USER_HOME/.Trash/"* "$ROOT_HOME/.Trash/"*

fi  # DO_CACHES

#############################################################################
# HALF 2 — per-repo work, then maintenance
#############################################################################
if [ "$DO_REPOS" = "1" ]; then

DARGS=()
if [ -n "$TARGET_PATH" ]; then DARGS+=(--path "$TARGET_PATH"); else DARGS+=(--auto --user "$CALLER_USER"); fi
[ -n "$DEPTH_LIMIT" ] && DARGS+=(--depth "$DEPTH_LIMIT")
[ -n "$FOLLOW" ] && { [ "$FOLLOW" = "1" ] && DARGS+=(--follow) || DARGS+=(--no-follow); }

# In auto/full mode, assert the global git scheduler first. Basically a no-op
# when already configured. Skipped for --path, which is single-repo scoped.
if [ "$DO_MAINTENANCE" = "1" ] && [ -z "$TARGET_PATH" ]; then
  echo "🔧 git maintenance: system"
  run "$SELF_DIR/git-maintain-repos" --system || warn "maintenance --system failed"
fi

DISCOVERY_FILE="$(mktemp)"
REPOS_ONLY_FILE="$(mktemp)"
trap 'rm -f "$DISCOVERY_FILE" "$REPOS_ONLY_FILE"' EXIT

# ONE discovery pass feeds everything below: .trash removal, artifact pruning,
# Spotlight markers and the maintenance repo list.
echo "🔎 discovering repositories and artifacts"
"$SELF_DIR/git-discover-repos" "${DARGS[@]}" --find all --status > "$DISCOVERY_FILE" 2>/dev/null \
  || warn "discovery failed"
awk -F'\t' '$1=="repo"{print $2}' "$DISCOVERY_FILE" > "$REPOS_ONLY_FILE"
n_repos=$(wc -l < "$REPOS_ONLY_FILE" | tr -d ' ')
n_wt=$(awk -F'\t' '$1=="worktree"' "$DISCOVERY_FILE" | wc -l | tr -d ' ')
n_found=$(awk -F'\t' '$1=="FOUND"' "$DISCOVERY_FILE" | wc -l | tr -d ' ')
echo "   $n_repos repos, $n_wt worktrees, $n_found artifact dirs"

# Is a repo idle? Excludes artifact dirs, .git and .worktrees; nested repos are
# evaluated as their own units.
repo_is_idle() {
  [ -z "$(find "$1" \
      \( "${CACHE_EXPR[@]}" -o "${RISK_EXPR[@]}" -o -name .git -o -name .worktrees \) -prune -o \
      -type f -mtime "-${ARTIFACT_STALE_DAYS}" -print -quit 2>/dev/null)" ]
}

# Spotlight markers are non-destructive and reversible, so they are applied
# regardless of staleness -- and re-applied to the empty stub left behind after
# a delete, so the directory is already excluded when a build regenerates it.
mark_dir() {
  local d="$1"
  [ -d "$d" ] || return 0
  # App static trees may be committed or feed flat-static manifests. Never add
  # an untracked Spotlight marker to them.
  [ "${d##*/}" = "static" ] && return 0
  [ -e "$d/$SPOTLIGHT_MARKER" ] && return 0
  if [ "$DRY_RUN" = "1" ]; then echo "  would mark: ${d/#$HOME/\~}"; MARKED=$((MARKED + 1)); return 0; fi
  spotlight_mark "$d" "$CALLER_USER" && MARKED=$((MARKED + 1))
}
# rm -rf then recreate is FASTER than emptying in place: `find -delete` stats and
# traverses every entry, while rm -rf uses the bulk unlink path. The stub costs
# two syscalls.
del_and_stub() {
  del "$1"
  if [ "$DRY_RUN" != "1" ]; then
    mkdir -p "$1" 2>/dev/null && chown "$CALLER_USER" "$1" 2>/dev/null
  fi
  mark_dir "$1"
}

MARKED=0; TRASHED=0; PRUNED=0; SKIPPED_TRACKED=0
declare -A IDLE_CACHE=()

echo "🗑  .trash, artifacts, Spotlight markers"
while IFS=$'\t' read -r tag reason status path; do
  [ "$tag" = "FOUND" ] || continue
  case "$reason" in
    trash)
      # Discard piles go unconditionally, staleness irrelevant.
      del "$path"; TRASHED=$((TRASHED + 1))
      ;;
    cache)
      owner="${path}"; owner_repo=""
      # Find which discovered repo this sits under, to reuse the idle check.
      while IFS= read -r r; do
        case "$path" in "$r"/*) [ ${#r} -gt ${#owner_repo} ] && owner_repo="$r" ;; esac
      done < "$REPOS_ONLY_FILE"
      if [ -n "$owner_repo" ]; then
        if [ -z "${IDLE_CACHE[$owner_repo]:-}" ]; then
          if repo_is_idle "$owner_repo"; then IDLE_CACHE[$owner_repo]=idle; else IDLE_CACHE[$owner_repo]=active; fi
        fi
        if [ "${IDLE_CACHE[$owner_repo]}" = "idle" ]; then
          del_and_stub "$path"; PRUNED=$((PRUNED + 1))
        else
          mark_dir "$path"       # active repo: mark but never delete
        fi
      else
        mark_dir "$path"
      fi
      ;;
    risky)
      # Only removable when git proves the whole dir is ignored. Tracked dirs
      # are not even marked: an untracked marker inside a committed vendor/
      # would show up in every `git status`.
      if [ "$status" = "tracked" ] || [ "$status" = "staged" ]; then
        SKIPPED_TRACKED=$((SKIPPED_TRACKED + 1))
      elif [ "$status" = "ignored" ]; then
        del_and_stub "$path"; PRUNED=$((PRUNED + 1))
      else
        mark_dir "$path"
      fi
      ;;
  esac
done < "$DISCOVERY_FILE"

# Mark every discovered object store too.
while IFS=$'\t' read -r kind path; do
  case "$kind" in repo|worktree) mark_dir "$path/.git" ;; esac
done < <(awk -F'\t' '$1=="repo"||$1=="worktree"{print $1"\t"$2}' "$DISCOVERY_FILE")

echo "   $TRASHED .trash removed, $PRUNED artifact dirs pruned, $MARKED marked, $SKIPPED_TRACKED tracked skipped"

# --- Artifact dirs directly in ~, outside any repo --------------------------
# ~/.cache is deliberately skipped: it is the shared XDG cache (uv, nix, gh,
# huggingface, opencode) with its own policy above, not a build artifact.
if [ -z "$TARGET_PATH" ]; then
  echo "🏠 artifact dirs directly in ~"
  HOME_ARTIFACT_SKIP=(.cache)
  for _n in "${CACHE_NAMES[@]}"; do
    _d="$CALLER_USER_HOME/$_n"
    [ -d "$_d" ] || continue
    _skip=0
    for _s in "${HOME_ARTIFACT_SKIP[@]}"; do [ "$_n" = "$_s" ] && _skip=1; done
    [ "$_skip" = "1" ] && { info "~/$_n skipped by policy"; continue; }
    if [ -z "$(find "$_d" -type f -mtime "-${ARTIFACT_STALE_DAYS}" -print -quit 2>/dev/null)" ]; then
      del_and_stub "$_d"
    else
      mark_dir "$_d"
    fi
  done
fi

# Recovery only: caller-repository maintenance runs as CALLER_USER, so this
# should be redundant. Limit any repair to Git metadata, never source trees.
git_metadata_needs_repair() {
  [ -n "$(find "$1" ! -user "$CALLER_USER" -print -quit 2>/dev/null)" ]
}

repair_git_ownership() {
  local kind path git_dir pointer caller_uid repaired=0
  caller_uid="$(id -u "$CALLER_USER")"
  while IFS=$'\t' read -r kind path; do
    [ -d "$path" ] || continue
    if [ -f "$path/HEAD" ] && [ -d "$path/objects" ] && [ -d "$path/refs" ]; then
      git_dir="$path"
    elif [ -f "$path/.git" ]; then
      pointer="$path/.git"
      git_dir="$(git -c safe.directory='*' -C "$path" rev-parse --absolute-git-dir 2>/dev/null)"
      [ -n "$git_dir" ] || { warn "could not resolve gitdir for $path"; continue; }
    elif [ -d "$path/.git" ]; then
      git_dir="$path/.git"
    else
      continue
    fi
    [ -d "$git_dir" ] || continue
    if { [ -n "${pointer:-}" ] && [ "$(stat -f '%u' "$pointer" 2>/dev/null)" != "$caller_uid" ]; } || git_metadata_needs_repair "$git_dir"; then
      if [ "$DRY_RUN" = "1" ]; then
        echo "  would repair Git ownership: ${git_dir/#$CALLER_USER_HOME/\~}"
      else
        [ -z "${pointer:-}" ] || chown -h "$CALLER_USER" "$pointer" 2>/dev/null || warn "could not repair $pointer"
        chown -R -h -P "$CALLER_USER" "$git_dir" 2>/dev/null || warn "could not repair $git_dir"
      fi
      repaired=$((repaired + 1))
    fi
    pointer=""
  done < <(awk -F'\t' '$1=="repo"||$1=="worktree"{print $1"\t"$2}' "$DISCOVERY_FILE")
  [ "$repaired" -gt 0 ] && echo "   $repaired Git metadata directories ownership repaired"
}

# --- git maintenance, last: it is the slowest step --------------------------
if [ "$DO_MAINTENANCE" = "1" ] && [ "$n_repos" -gt 0 ]; then
  repair_git_ownership
  echo "🔧 git maintenance ($MAINT_DEPTH) across $n_repos repos"
  while IFS= read -r repo; do
    [ -d "$repo" ] || continue
    run as_user "$SELF_DIR/git-maintain-repos" --path "$repo" --no-recursive --no-system \
      "$MAINT_DEPTH" --clean-orphans || warn "maintenance failed for $repo"
  done < "$REPOS_ONLY_FILE"
fi

# --- root's own repositories ------------------------------------------------
# Root cannot have the git scheduler installed (launchd *agents* need a GUI
# session), so this is repo cleanup and config only.
if [ -z "$TARGET_PATH" ] && [ -d "$ROOT_HOME" ] && [ "$ROOT_HOME" != "$CALLER_USER_HOME" ]; then
  echo "🔧 root-owned repositories"
  root_repos=$("$SELF_DIR/git-discover-repos" --auto --repos-only --user root 2>/dev/null | cut -f2)
  if [ -n "$root_repos" ]; then
    while IFS= read -r repo; do
      [ -d "$repo" ] || continue
      run "$SELF_DIR/git-maintain-repos" --path "$repo" --no-recursive --no-system "$MAINT_DEPTH" \
        || warn "root maintenance failed for $repo"
    done <<< "$root_repos"
  else
    info "no repositories under root's home"
  fi
fi

fi  # DO_REPOS

# ---------------------------------------------------------------------------
DISK_FREE_END="$(get_free_bytes)"
echo ""
echo "📏 Free disk before clean: $(human_size "${DISK_FREE_START:-0}")"
echo "📏 Free disk after clean:  $(human_size "$DISK_FREE_END")"
RECLAIMED=$(( DISK_FREE_END - ${DISK_FREE_START:-0} ))
if (( RECLAIMED > 0 )); then echo "✅ Reclaimed: $(human_size "$RECLAIMED")"
else echo "⚠️  No net disk reclaimed ($(human_size $(( -RECLAIMED ))) more used)"; fi
