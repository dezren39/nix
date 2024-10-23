#!/usr/bin/env bash

# Find applications that weren't installed through Homebrew
# Compares /Applications and ~/Applications against brew list
# Outputs applications that don't match any Homebrew installation

set -euo pipefail

# Get list of all Homebrew-installed apps
mapfile -t BREW_APPS < <(brew list --cask 2>/dev/null | tr '[:upper:]' '[:lower:]')
mapfile -t BREW_NAMES < <(brew list --cask 2>/dev/null | tr '[:upper:]' '[:lower:]' | tr ' .' '-')

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

echo "Checking /Applications..."
find /Applications -maxdepth 1 -name "*.app" -print0 | while IFS= read -r -d '' app; do
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
