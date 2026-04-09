#!/usr/bin/env bash
# brew-repair: Find and fix broken Homebrew cask installations
# Checks each installed cask's expected app artifacts against /Applications
# and selectively reinstalls only the broken ones.
set -uo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

DRY_RUN=false
QUIET=false
SKIP_LIST=()

usage() {
  cat <<EOF
Usage: brew-repair [OPTIONS]

Find and reinstall broken Homebrew cask apps.

A cask is "broken" when brew thinks it's installed but the .app bundle
is missing from /Applications (or ~/Applications).

Options:
  -n, --dry-run     Show what would be reinstalled without doing it
  -q, --quiet       Only show broken casks (no healthy ones)
  -s, --skip CASK   Skip a cask (can be repeated, e.g. -s 1password -s docker)
  -h, --help        Show this help

Examples:
  brew-repair                     # Find and fix all broken casks
  brew-repair -n                  # Preview what's broken (no changes)
  brew-repair -s 1password        # Fix everything except 1password
  brew-repair -q -n               # Quietly list only broken casks
EOF
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n|--dry-run) DRY_RUN=true; shift ;;
    -q|--quiet) QUIET=true; shift ;;
    -s|--skip) SKIP_LIST+=("$2"); shift 2 ;;
    -h|--help) usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

is_skipped() {
  local cask="$1"
  for s in "${SKIP_LIST[@]+"${SKIP_LIST[@]}"}"; do
    [[ "$cask" == "$s" ]] && return 0
  done
  return 1
}

echo -e "${BOLD}${BLUE}brew-repair${NC} — scanning installed casks..."
echo ""

# Get all installed casks
mapfile -t CASKS < <(brew list --cask 2>/dev/null)

if [[ ${#CASKS[@]} -eq 0 ]]; then
  echo "No casks installed."
  exit 0
fi

echo -e "  Found ${#CASKS[@]} installed casks, querying artifacts..."
echo ""

# Build cask→app mapping from brew JSON in one shot
# brew info may return different token names (e.g. docker→docker-desktop)
# so we also track the original installed name→actual token mapping
TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

brew info --json=v2 --cask "${CASKS[@]}" 2>/dev/null | python3 -c "
import json, sys
data = json.load(sys.stdin)
for c in data.get('casks', []):
    apps = []
    for a in c.get('artifacts', []):
        if isinstance(a, dict) and 'app' in a:
            apps.extend(a['app'])
    print(c['token'] + '|' + '|'.join(apps))
" > "$TMPFILE" 2>/dev/null || true

# Also build a map of original cask name → token returned by brew info
# (handles renames like docker→docker-desktop, handbrake→handbrake-app, etc.)
declare -A TOKEN_TO_ORIGINAL
for cask in "${CASKS[@]}"; do
  TOKEN_TO_ORIGINAL["$cask"]="$cask"
done

BROKEN=()
BROKEN_ORIGINAL=()
HEALTHY=0
SKIPPED=0
FONT_OR_NOAPP=0

while IFS='|' read -r cask rest; do
  [[ -z "$cask" ]] && continue

  # Find original cask name — may differ from token
  original="${TOKEN_TO_ORIGINAL[$cask]:-$cask}"

  if is_skipped "$cask" || is_skipped "$original"; then
    SKIPPED=$((SKIPPED + 1))
    [[ "$QUIET" == false ]] && echo -e "  ${YELLOW}⏭${NC}  $cask (skipped)"
    continue
  fi

  # No app artifacts = font, plugin, CLI-only, etc.
  if [[ -z "$rest" ]]; then
    FONT_OR_NOAPP=$((FONT_OR_NOAPP + 1))
    [[ "$QUIET" == false ]] && echo -e "  ${BLUE}—${NC}  $cask (no .app — font/plugin/CLI)"
    continue
  fi

  # Check if ALL expected apps exist
  ALL_FOUND=true
  MISSING_APP=""
  IFS='|' read -ra APP_LIST <<< "$rest"
  for app in "${APP_LIST[@]}"; do
    [[ -z "$app" ]] && continue
    if [[ ! -d "/Applications/$app" ]] && [[ ! -d "$HOME/Applications/$app" ]]; then
      ALL_FOUND=false
      MISSING_APP="$app"
      break
    fi
  done

  if [[ "$ALL_FOUND" == true ]]; then
    HEALTHY=$((HEALTHY + 1))
    [[ "$QUIET" == false ]] && echo -e "  ${GREEN}✅${NC} $cask"
  else
    BROKEN+=("$cask")
    BROKEN_ORIGINAL+=("$original")
    echo -e "  ${RED}❌${NC} $cask ${RED}→ missing ${MISSING_APP}${NC}"
  fi
done < "$TMPFILE"

# Check for installed casks that weren't in the JSON output (removed/renamed upstream)
declare -A SEEN_TOKENS
while IFS='|' read -r cask _rest; do
  SEEN_TOKENS["$cask"]=1
done < "$TMPFILE"

for cask in "${CASKS[@]}"; do
  if [[ -z "${SEEN_TOKENS[$cask]:-}" ]]; then
    # This cask wasn't in JSON output — it's been renamed or removed
    # Check if its renamed version was already seen
    found=false
    for token in "${!SEEN_TOKENS[@]}"; do
      if [[ "$token" == *"$cask"* ]] || [[ "$cask" == *"$token"* ]]; then
        found=true
        break
      fi
    done
    if [[ "$found" == false ]]; then
      echo -e "  ${YELLOW}⚠️${NC}  $cask (installed but unknown to brew — may be renamed/removed)"
    fi
  fi
done

echo ""
echo -e "${BOLD}Summary:${NC} ${GREEN}${HEALTHY} healthy${NC}, ${RED}${#BROKEN[@]} broken${NC}, ${SKIPPED} skipped, ${FONT_OR_NOAPP} non-app (fonts/plugins/CLIs)"

if [[ ${#BROKEN[@]} -eq 0 ]]; then
  echo -e "\n${GREEN}${BOLD}All cask apps are healthy!${NC}"
  exit 0
fi

echo ""
if [[ "$DRY_RUN" == true ]]; then
  echo -e "${YELLOW}${BOLD}Dry run — would reinstall:${NC}"
  for i in "${!BROKEN[@]}"; do
    cask="${BROKEN[$i]}"
    original="${BROKEN_ORIGINAL[$i]}"
    if [[ "$cask" != "$original" ]]; then
      echo "  brew reinstall --cask $original  (now: $cask)"
    else
      echo "  brew reinstall --cask $cask"
    fi
  done
  echo ""
  echo "Run without -n to fix these."
  exit 0
fi

echo -e "${BOLD}Reinstalling ${#BROKEN[@]} broken cask(s)...${NC}"
echo ""

FIXED=0
FAILED=0
FAILED_LIST=()

for i in "${!BROKEN[@]}"; do
  cask="${BROKEN[$i]}"
  original="${BROKEN_ORIGINAL[$i]}"
  echo -e "${BLUE}→${NC} Reinstalling ${BOLD}$original${NC}..."
  if brew reinstall --cask "$original" 2>&1; then
    echo -e "  ${GREEN}✅ $original reinstalled${NC}"
    FIXED=$((FIXED + 1))
  else
    echo -e "  ${RED}⚠️  $original failed (may need sudo)${NC}"
    FAILED=$((FAILED + 1))
    FAILED_LIST+=("$original")
  fi
  echo ""
done

echo -e "${BOLD}Results:${NC} ${GREEN}${FIXED} fixed${NC}, ${RED}${FAILED} failed${NC}"

if [[ ${#FAILED_LIST[@]} -gt 0 ]]; then
  echo ""
  echo -e "${YELLOW}These casks need manual intervention (probably sudo):${NC}"
  for cask in "${FAILED_LIST[@]}"; do
    echo "  brew reinstall --cask $cask"
  done
fi
