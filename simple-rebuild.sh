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
# Get the directory of the script
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Git add for the script's directory
cd "${script_dir}" || exit 1
echo "entered: $script_dir"
echo "git add ."

sudo chown -R "${USER:-$(id -un)}" .
git add .

echo "softwareupdate --install-rosetta --agree-to-license"
softwareupdate --install-rosetta --agree-to-license

# Build as the invoking user, activate as root.
#
# `sudo nix run nix-darwin -- switch` did both halves as root, which is why the
# repo needed `sudo chown -R "$USER" .` above: every eval/build artifact nix
# touched in this directory came back root-owned. Building unprivileged keeps
# ~/.cache/nix, the flake eval cache and any result symlinks owned by the user,
# and leaves root doing only what genuinely needs privilege -- activation.
#
# Falls back to the old combined path if the build fails, so a broken build
# never leaves the machine un-switched.
echo "nix build .#darwinConfigurations.$(hostname -s).system  (as $USER)"
if nix --extra-experimental-features 'nix-command flakes' \
     build ".#darwinConfigurations.$(hostname -s).system" --keep-going --out-link ./result; then
  echo "darwin-rebuild switch (activate as root)"
  sudo ./result/sw/bin/darwin-rebuild switch --flake . --keep-going
else
  echo "user build failed; falling back to combined root build+switch"
  sudo nix --extra-experimental-features 'nix-command flakes' run nix-darwin -- switch --flake . --keep-going
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

current=$(sudo darwin-rebuild --list-generations | grep current)
echo "current: $current"
hostname=$(hostname)
echo "hostname: $hostname"
sudo chown -R "${USER:-$(id -un)}" .
git commit --no-verify --allow-empty -m "$hostname $current"
