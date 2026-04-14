#!/usr/bin/env bash

# Get free disk space in bytes (portable macOS)
get_free_bytes() {
  df -k / | awk 'NR==2 {print $4 * 1024}'
}

# Human-readable size from bytes
human_size() {
  local bytes=$1
  if (( bytes >= 1073741824 )); then
    printf "%.2f GB" "$(echo "scale=2; $bytes / 1073741824" | bc)"
  elif (( bytes >= 1048576 )); then
    printf "%.2f MB" "$(echo "scale=2; $bytes / 1048576" | bc)"
  else
    printf "%d KB" "$(( bytes / 1024 ))"
  fi
}

# When not root: run brew cleanup, then re-invoke as root, passing caller's home + start disk
if [ "$EUID" -ne 0 ]; then
  DISK_FREE_START="$(get_free_bytes)"
  echo "📏 Free disk before clean: $(human_size "$DISK_FREE_START")"
  brew cleanup --prune=all
  CALLER_USER_HOME="$(realpath ~)" DISK_FREE_START="$DISK_FREE_START" \
    sudo --preserve-env=CALLER_USER_HOME,DISK_FREE_START "$0" "$@"
  exit $?
fi

# Now running as root — set both homes
ROOT_HOME="$(realpath ~)"
if [ -z "$CALLER_USER_HOME" ]; then
  # Ran directly as root (e.g. `sudo ./clean.sh`) — fall back to SUDO_USER's home
  if [ -n "$SUDO_USER" ]; then
    CALLER_USER_HOME="$(eval echo "~$SUDO_USER")"
  else
    CALLER_USER_HOME="$ROOT_HOME"
  fi
fi

nix-collect-garbage -d
nix-store --gc

# Caller's home cleanup
rm -rf "$CALLER_USER_HOME/.cache/nix/eval-cache-v2"

docker container prune --force
docker builder prune --force
docker system prune --force --all --volumes

rm -rf "$CALLER_USER_HOME/git/oseries"
find "$CALLER_USER_HOME/git" -type d -name .terraform -exec rm -rf {} +

# sudo pmset -a hibernatemode 0
rm -f /var/vm/sleepimage

rm -rf "$CALLER_USER_HOME/Library/Caches"
rm -rf "$CALLER_USER_HOME/Library/Logs"

folders=/private/var/folders
for i in "$folders"/*; do
  if [ "$(basename "$i")" != "zz" ]; then
    rm -rf "$i"
  fi
done
rm -rf "$folders/zz/"*

# Trash for both caller and root
rm -rf "$CALLER_USER_HOME/.Trash/"*
rm -rf "$ROOT_HOME/.Trash/"*

# Report disk reclaimed
DISK_FREE_END="$(get_free_bytes)"
echo ""
echo "📏 Free disk before clean: $(human_size "$DISK_FREE_START")"
echo "📏 Free disk after clean:  $(human_size "$DISK_FREE_END")"
RECLAIMED=$(( DISK_FREE_END - DISK_FREE_START ))
if (( RECLAIMED > 0 )); then
  echo "✅ Reclaimed: $(human_size "$RECLAIMED")"
else
  echo "⚠️  No net disk reclaimed ($(human_size $(( -RECLAIMED ))) more used)"
fi
