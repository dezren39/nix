#!/usr/bin/env zsh
path=("$HOME/.nix-profile/bin" "/nix/var/nix/profiles/default/bin" "/run/current-system/sw/bin" "$path[@]")
set -exuo pipefail
#ulimit -n $(ulimit -Hn)
#sudo prlimit --pid $$ --nofile=1000000:1000000
#nix-shell -p nixVersions.nix_2_18 git cachix jq
#cat /mnt/c/wsl/cachix.key | cachix authtoken --stdin
# Get the directory of the script
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Git add for the script's directory
cd "${script_dir}" || exit 1
echo "entered: $script_dir"

cp -f "$HOME/.config/zed/settings.json" ./.zed/settings.json
cp -f "$HOME/Library/Application Support/Zed/extensions/index.json" ./.zed/index.json
find "$HOME/Library/Application Support/Zed/extensions/installed" -depth 1  -exec basename {} \; > ./.zed/installed.txt
