#!/usr/bin/env zsh
path=("$HOME/.nix-profile/bin" "/nix/var/nix/profiles/default/bin" "/run/current-system/sw/bin" "$path[@]")
set -exuo pipefail

# error: opening Git repository "/Users/drewry.pope/.config/nix": repository path '/Users/drewry.pope/.config/nix' is not owned by current user
# # if not root rerun as root
# if [[ $EUID -ne 0 ]]; then
#     echo "Rerunning as root..."
#     sudo "$0"
#     exit $?
# else
#     echo "Running as root..."
# fi
#ulimit -n $(ulimit -Hn)
#sudo prlimit --pid $$ --nofile=1000000:1000000
#nix-shell -p nixVersions.nix_2_18 git cachix jq
#cat /mnt/c/wsl/cachix.key | cachix authtoken --stdin
# Get the directory of the script.
#
# This was `$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)`, which is bash. The
# shebang is zsh, where BASH_SOURCE does not exist, so under `set -u` the line
# errored with "BASH_SOURCE[0]: parameter not set", script_dir came out empty,
# and the `cd` below became `cd ''` -- a no-op that silently left the script in
# whatever directory the caller happened to be in. It only ever worked because
# callers were already here. `${0:A:h}` is the zsh spelling: :A absolutises and
# resolves symlinks (so ~/nix resolves to ~/.config/nix), :h takes the dirname.
script_dir="${0:A:h}"
# Git add for the script's directory
cd "${script_dir}" || exit 1
echo "entered: $script_dir"
echo "git add ."

# Reclaim any root-owned files, but only if there are any.
#
# This was an unconditional `sudo chown -R`, which prompted for a password
# before anything else happened even when there was nothing to fix. It dates
# from when `sudo nix run nix-darwin -- switch` did the build *and* the
# activation as root, leaving every eval/build artifact in this directory
# root-owned (see the comment above the build below). Building unprivileged
# fixed the cause; measured on 2026-10-01 after two unprivileged builds, the
# repo had 0 files not owned by the invoking user.
#
# Kept as a guard rather than deleted because the activation below still runs
# nix as root against this flake, so it is not proven that nothing can slip
# through. The difference is that it now costs a password only when it finds
# something, and says what it found.
reclaim_root_owned() {
  local owner="${USER:-$(id -un)}" strays
  strays=$(find . -not -user "$owner" -not -path './.git/*' -print -quit 2>/dev/null)
  if [[ -n "$strays" ]]; then
    echo "reclaiming root-owned files (first: $strays)"
    sudo chown -R "$owner" .
  else
    echo "no root-owned files; skipping chown"
  fi
}

reclaim_root_owned
git add .

# Phases.
#
# `just build`, `just activate` and `just switch` are three entry points into
# one implementation, so there is nothing to drift. The split is by privilege:
# phase_build needs no sudo and can run unattended, phase_activate needs root
# and therefore a human (or a fingerprint) at the machine.
#
#   --build-only     prepare + build            no sudo
#   --activate-only  activate + post + commit   sudo
#   (no flag)        all of it
mode="all"
case "${1:-}" in
  --build-only)    mode="build" ;;
  --activate-only) mode="activate" ;;
  "")              ;;
  *) echo "usage: ${0:t} [--build-only|--activate-only]" >&2; exit 2 ;;
esac

# Every commit here is --allow-empty: the point is to mark *when* a build or an
# activation happened, which is information even when no file changed. A build
# names the store path it produced, because that is the only identity a build
# has -- and reading it needs no sudo, unlike --list-generations, which takes
# the system profile lock. An activation names the generation, which is the
# identity the system has.
phase_build() {
  echo "nix build .#darwinConfigurations.$(hostname -s).system  (as $USER)"
  if nix --extra-experimental-features 'nix-command flakes' \
       build ".#darwinConfigurations.$(hostname -s).system" --keep-going --out-link ./result; then
    built=$(basename "$(readlink -f ./result)")
  elif [[ "$mode" == "build" ]]; then
    # No root fallback here: this phase's whole contract is that it never asks
    # for a password, so a failed build fails rather than escalating.
    echo "build failed" >&2
    return 1
  else
    # Falls back to the old combined path, so a broken unprivileged build never
    # leaves the machine un-switched.
    echo "user build failed; falling back to combined root build+switch"
    sudo nix --extra-experimental-features 'nix-command flakes' run nix-darwin -- switch --flake . --keep-going
    built="root-fallback"
  fi
  git commit --no-verify --allow-empty -m "$(hostname) build ${built}"
}

phase_activate() {
  echo "softwareupdate --install-rosetta --agree-to-license"
  softwareupdate --install-rosetta --agree-to-license

  # Activate as root. Build already happened (phase_build, or a previous
  # `just build`), so the nix build inside this is a cache hit.
  if [[ -x ./result/sw/bin/darwin-rebuild ]]; then
    echo "darwin-rebuild switch (activate as root)"
    sudo ./result/sw/bin/darwin-rebuild switch --flake . --keep-going
  else
    echo "no ./result to activate; run 'just build' first" >&2
    return 1
  fi

  echo "install/update pinned lootbox"
  nix run .#lootbox-update -- --if-needed

  # Spotlight: mark .git and regenerable build-artifact dirs under ~/git as
  # never-index. Runs as the invoking user (not root) so the markers are
  # user-owned. Static paths are handled by configuration.nix activation.
  # --system only: re-assert the static marker list for both accounts. The
  # per-repo walk is deliberately NOT run here -- it rescans every repo under
  # ~/git, which is wasted work on a switch. Use `just spotlight-walk` or ./clean.
  echo "spotlight: static exclude markers (user + root)"
  ./spotlight-exclude-artifacts --system || true       # ~40ms. Consider dropping --system for default --auto: only +~3s, marks new repos
  sudo ./spotlight-exclude-artifacts --system || true  # ~40ms. Consider dropping --system for default --auto: only +~3s, marks new repos

  # System-wide git setup only (scheduler + drift report). Touches no repos, so it
  # stays fast. Per-repo maintenance is the scheduler's job, or ./git-maintain-repos.
  # Run for both accounts: the scheduler is per-user, and root has its own global
  # config via /var/root/.gitconfig.
  echo "git: system-wide maintenance setup (user)"
  ./git-maintain-repos --system || true                # ~80ms. Do NOT drop --system: default --auto walks every repo, ~3min
  echo "git: system-wide maintenance setup (root)"
  sudo ./git-maintain-repos --system || true           # ~80ms. Root: config only. No scheduler: macOS has only launchd, and git installs launchd *agents*, which need a GUI Aqua session root lacks

  # --list-generations needs root despite not matching darwin-rebuild's
  # root-required action regex: it takes the /nix/var/nix/profiles/system lock.
  current=$(sudo darwin-rebuild --list-generations | grep current)
  echo "current: $current"
  echo "hostname: $(hostname)"
  reclaim_root_owned
  git add .
  git commit --no-verify --allow-empty -m "$(hostname) $current"
}

case "$mode" in
  build)    phase_build ;;
  activate) phase_activate ;;
  all)      phase_build && phase_activate ;;
esac
