#!/usr/bin/env zsh
path=("$HOME/.nix-profile/bin" "/nix/var/nix/profiles/default/bin" "/run/current-system/sw/bin" "$path[@]")

# See simple-rebuild.sh: zsh's default PS4 reports the *function* name and a
# line within it, which cannot be looked up in the file. %x:%I reports the file
# and the line in it, in both cases.
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
# Get the directory of the script
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Git add for the script's directory
cd "${script_dir}" || exit 1
echo "entered: $script_dir"

echo "git add ."
git add .

echo "nix flake update"
sudo nix --extra-experimental-features 'nix-command flakes' flake update

echo "./simple-rebuild.sh"
./simple-rebuild.sh
