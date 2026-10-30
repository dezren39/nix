#!/usr/bin/env bash

# Find applications that weren't installed through Homebrew
# Compares /Applications and ~/Applications against brew list
# Outputs applications that don't match any Homebrew installation

set -euo pipefail

usage() {
    echo "Usage: search.sh"
}

# This script takes no arguments (#1158): any argument was silently
# ignored and the full scan ran with exit 0. Reject the first argument
# by name, with usage on stderr and exit 2, before any brew call --
# the house argument contract. --help prints usage and exits 0.
if [[ $# -gt 0 ]]; then
    case "$1" in
        -h|--help) usage; exit 0 ;;
        *) echo "search: unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
fi

# Get list of all Homebrew-installed apps. Capture `brew list --cask`
# once, with its exit status checked (#957): the old process
# substitutions hid a failed `brew list` — its status was invisible
# and its stderr discarded by 2>/dev/null — so brew breakage (or a
# missing brew) looked exactly like "no casks installed" and every
# app was reported as not brew-installed, with exit 0. Name the
# failing step on stderr, surface brew's own stderr, and exit 1.
# A genuinely empty successful list keeps the normal path below.
BREW_ERR=$(mktemp)
trap 'rm -f "$BREW_ERR"' EXIT

if ! BREW_LIST=$(brew list --cask 2>"$BREW_ERR"); then
    echo "search: error: 'brew list --cask' failed — installed casks could not be enumerated" >&2
    if [[ -s "$BREW_ERR" ]]; then
        sed 's/^/  /' "$BREW_ERR" >&2
    fi
    exit 1
fi

BREW_APPS=()
BREW_NAMES=()
if [[ -n "$BREW_LIST" ]]; then
    mapfile -t BREW_APPS <<< "$(printf '%s' "$BREW_LIST" | tr '[:upper:]' '[:lower:]')"
    mapfile -t BREW_NAMES <<< "$(printf '%s' "$BREW_LIST" | tr '[:upper:]' '[:lower:]' | tr ' .' '-')"
fi

check_if_brew_installed() {
    local app_path="$1"
    local app_name
    local normalized_name

    # Skip if not an app
    if [[ ! "$app_path" =~ \.app$ ]]; then
        return
    fi

    # Get the app name without .app extension
    app_name=$(basename "$app_path" .app | tr '[:upper:]' '[:lower:]')
    normalized_name=$(echo "$app_name" | tr ' .' '-')

    # Check if app exists in brew list
    if [[ ! " ${BREW_APPS[*]} " =~ " ${app_name} " ]] && \
       [[ ! " ${BREW_NAMES[*]} " =~ " ${normalized_name} " ]]; then
        # echo "${app_path}"
        echo "${normalized_name}"

        # Try to find similar Homebrew packages
        local similar
        similar=$(brew search "$normalized_name" 2>/dev/null | grep -i "$normalized_name" || true)
        if [ -n "$similar" ]; then
            # echo "  Similar Homebrew packages found:"
            # echo "$similar" | sed 's/^/    /'
            true
        fi
        # echo
    fi
}

# First leg scans /Applications; SEARCH_APPLICATIONS_DIR overrides the
# directory for tests (#983) — the default behaviour is unchanged.
APPLICATIONS_DIR="${SEARCH_APPLICATIONS_DIR:-/Applications}"
echo "Checking $APPLICATIONS_DIR..."
find "$APPLICATIONS_DIR" -maxdepth 1 -name "*.app" -print0 | while IFS= read -r -d '' app; do
    check_if_brew_installed "$app"
done

echo "Checking ~/Applications..."
if [ -d ~/Applications ]; then
    find ~/Applications -maxdepth 1 -name "*.app" -print0 | while IFS= read -r -d '' app; do
        check_if_brew_installed "$app"
    done
else
    echo "~/Applications directory does not exist"
fi
