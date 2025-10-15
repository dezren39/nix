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

git add .

echo "softwareupdate --install-rosetta --agree-to-license"
softwareupdate --install-rosetta --agree-to-license

echo "darwin-rebuild switch --flake ."
sudo nix --extra-experimental-features 'nix-command flakes' run nix-darwin -- switch --flake . --keep-going

current=$(sudo darwin-rebuild --list-generations | grep current)
echo "current: $current"
hostname=$(hostname)
echo "hostname: $hostname"
git commit --no-verify --allow-empty -m "$hostname $current"
