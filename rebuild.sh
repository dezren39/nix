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
# Get the directory of the script. `${BASH_SOURCE[0]}` does not exist under
# this zsh shebang: with `set -u` it errored inside the command substitution,
# script_dir came out empty and `cd ''` silently stayed in the caller's
# directory. Same fix as simple-rebuild.sh: :A resolves symlinks (./r ->
# rebuild.sh), :h takes the dirname.
script_dir="${0:A:h}"
# Git add for the script's directory
cd "${script_dir}" || exit 1
echo "entered: $script_dir"

echo "git add ."
git add .

echo "nix flake update"
# As you, not root. Under sudo, Nix reads root's config instead of
# ~/.config/nix/nix.conf, so it fetched all github: inputs without the access
# token (60 requests/hour), and it left flake.lock root-owned for
# simple-rebuild.sh's reclaim_root_owned to chown back. Updating the lock
# needs no privilege.
nix --extra-experimental-features 'nix-command flakes' flake update

echo "./simple-rebuild.sh"
./simple-rebuild.sh
