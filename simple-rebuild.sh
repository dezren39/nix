#!/usr/bin/env zsh
path=("$HOME/.nix-profile/bin" "/nix/var/nix/profiles/default/bin" "/run/current-system/sw/bin" "$path[@]")

# Trace every command with the file and line it came from.
#
# zsh's default PS4 is '+%N:%i> ', where %N is the *script or function* name
# and %i the line within it. At top level that reads as simple-rebuild.sh:97,
# which is what you want; inside a function it collapses to phase_build:2,
# which cannot be looked up in the file. That became the common case when this
# script was split into phases.
#
# %x is the file containing the source, %I the line number in that file, so
# both cases report a real location you can open.
PS4='+%x:%I> '
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
generation=""
case "${1:-}" in
  --build-only)    mode="build" ;;
  --activate-only) mode="activate"; generation="${2:-}" ;;
  "")              ;;
  *) echo "usage: ${0:t} [--build-only|--activate-only [GENERATION]]" >&2; exit 2 ;;
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
  # Rolling back to an existing generation is a different operation from
  # activating a freshly built one, and deliberately does less: no rosetta
  # check, no ./result, and none of the post-activation steps below. Those all
  # run things *from the current flake* (lootbox-update, the spotlight and git
  # helpers), which is precisely what you do not want when the point is to get
  # back to how the system was. `-G` sets action=rollback inside darwin-rebuild,
  # which reads $profile/systemConfig rather than building anything.
  if [[ -n "$generation" ]]; then
    echo "darwin-rebuild --switch-generation $generation (rollback, as root)"
    sudo ./result/sw/bin/darwin-rebuild --switch-generation "$generation"
    record_activation "rollback"
    return
  fi

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

  # The spotlight markers and the git system setup used to be re-run here, as
  # the user and again under sudo. All four calls were redundant:
  # configuration.nix's postActivation already runs both helpers, for root and
  # for the primary user, and `darwin-rebuild switch` above has just executed
  # it. See configuration.nix, `spotlight_exclude` / `git_maintain`.
  #
  # Removing them also closes a real hole rather than papering over it. Those
  # two helpers live in this repo, user-writable (-rwxr-xr-x drewry.pope) in a
  # user-writable directory, so `sudo ./git-maintain-repos` runs a file
  # anything with your uid can rewrite first. Activation instead invokes them
  # at their *store* paths -- ${./git-maintain-repos} -- which are root-owned
  # and read-only. That is also why neither is a candidate for a NOPASSWD
  # sudoers rule: a passwordless sudo on a user-writable script is a complete
  # root escalation, strictly worse than the password prompt it removes.

  record_activation "switch"
}

# What the system actually is now, read from the profile symlink rather than
# from what we intended to do -- so a partial activation cannot be recorded as
# a successful one.
#
# Both reads are unprivileged, unlike `darwin-rebuild --list-generations`,
# which takes the profile lock and needs root:
#   readlink    /nix/var/nix/profiles/system  -> system-283-link  (the number)
#   readlink -f /nix/var/nix/profiles/system  -> /nix/store/...   (the closure)
record_activation() {
  local how="$1" gen store
  gen=$(readlink /nix/var/nix/profiles/system 2>/dev/null || echo "system-?-link")
  gen="${${gen#system-}%-link}"
  store=$(basename "$(readlink -f /nix/var/nix/profiles/system 2>/dev/null || echo unknown)")
  echo "now: generation $gen -> $store"
  reclaim_root_owned
  git add .
  git commit --no-verify --allow-empty -m "$(hostname) $how gen $gen $store"
}

case "$mode" in
  build)    phase_build ;;
  activate) phase_activate ;;
  all)      phase_build && phase_activate ;;
esac
