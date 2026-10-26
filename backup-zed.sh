#!/usr/bin/env zsh
path=("$HOME/.nix-profile/bin" "/nix/var/nix/profiles/default/bin" "/run/current-system/sw/bin" "$path[@]")
set -exuo pipefail
#ulimit -n $(ulimit -Hn)
#sudo prlimit --pid $$ --nofile=1000000:1000000
#nix-shell -p nixVersions.nix_2_18 git cachix jq
#cat /mnt/c/wsl/cachix.key | cachix authtoken --stdin
# Get the directory of the script. `${BASH_SOURCE[0]}` does not exist under
# this zsh shebang: with `set -u` it errored inside the command substitution,
# script_dir came out empty and `cd ''` silently stayed in the caller's
# directory. Same fix as rebuild.sh/simple-rebuild.sh: :A resolves symlinks,
# :h takes the dirname.
script_dir="${0:A:h}"
# Git add for the script's directory
cd "${script_dir}" || exit 1
echo "entered: $script_dir"

cp -f "$HOME/.config/zed/settings.json" ./.zed/settings.json
cp -f "$HOME/Library/Application Support/Zed/extensions/index.json" ./.zed/index.json
# Write to a temp file and mv on success (#860): a direct "> ./.zed/installed.txt"
# truncates the committed list before find runs, so a missing extensions/installed
# dir (or any other find failure) would leave it emptied when set -e exits.
installed_tmp=$(mktemp ./.zed/.installed.txt.XXXXXX)
trap 'rm -f "$installed_tmp"' EXIT
find "$HOME/Library/Application Support/Zed/extensions/installed" -mindepth 1 -maxdepth 1 -exec basename {} \; > "$installed_tmp"
mv -f "$installed_tmp" ./.zed/installed.txt
trap - EXIT
