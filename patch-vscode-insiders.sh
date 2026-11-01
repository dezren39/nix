#!/bin/bash
# Patch VSCode Insiders workbench CSS after updates
# Run this after any VSCode Insiders update to re-apply UI patches
#
# Usage: bash ~/nix/patch-vscode-insiders.sh

usage() {
    echo "Usage: bash ~/nix/patch-vscode-insiders.sh"
}

# This script takes no arguments (#1177): any argument was silently
# ignored and the full patch sequence ran against the VS Code Insiders
# app bundle with exit 0 -- so --help rewrote the app bundle instead of
# printing usage. Reject the first argument by name, with usage on
# stderr and exit 2, before any patch step -- the house argument
# contract (#1167 / #1145 rules 1 and 8). --help prints usage and
# exits 0 with no side effects.
if [[ $# -gt 0 ]]; then
    case "$1" in
        -h|--help) usage; exit 0 ;;
        *) echo "patch-vscode-insiders: unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
fi

# VSCODE_INSIDERS_APP_DIR overrides the app location for tests (#969);
# the default is exactly the historical hardcoded path.
APP_DIR="${VSCODE_INSIDERS_APP_DIR:-/Applications/Visual Studio Code - Insiders.app}"
CSS_FILE="$APP_DIR/Contents/Resources/app/out/vs/workbench/workbench.desktop.main.css"

if [ ! -f "$CSS_FILE" ]; then
  echo "ERROR: VSCode Insiders CSS file not found at:"
  echo "  $CSS_FILE"
  exit 1
fi

echo "Patching VSCode Insiders workbench CSS..."

# #909: propagate Python failures — a missing patch target must make the
# script exit non-zero (the launchd agent has no exit-code consumer, so the
# log line is the only failure signal), and files are written back only
# when their content actually changed (the agent's WatchPaths are these
# very files, so a no-change rewrite touches what it watches).
status=0

# #1288: the workbench paths reach the embedded Python via the
# PATCH_TARGET_FILE environment variable, never by interpolation into
# the Python source -- a path containing an apostrophe (or any other
# quote) spliced into a single-quoted Python literal broke both patch
# blocks with a SyntaxError and no patch applied.

PATCH_TARGET_FILE="$CSS_FILE" python3 -c "
import os, sys
path = os.environ['PATCH_TARGET_FILE']
with open(path, 'r') as f:
    content = f.read()
original = content
missing = []

patches_applied = 0

# Patch 1: Hide composite title bar (terminal/panel header)
old = '.monaco-workbench .part>.composite.header-or-footer,.monaco-workbench .part>.composite.title{display:flex}'
new = '.monaco-workbench .part>.composite.header-or-footer,.monaco-workbench .part>.composite.title{display:none!important;height:0!important;overflow:hidden!important;padding:0!important;margin:0!important;border:0!important}'
if old in content:
    content = content.replace(old, new)
    patches_applied += 1
    print('  ✓ Patch 1: Hide composite title bar')
elif new in content:
    print('  ~ Patch 1: Already applied')
else:
    missing.append('Patch 1')
    print('  ✗ Patch 1: Target not found (CSS structure may have changed)')

# Patch 2: Hide .part>.title height (top black bar) - use height:0 not display:none so JS layout reclaims space
old2 = '.monaco-workbench .part>.title,.monaco-workbench .part>.header-or-footer{height:35px;display:flex;box-sizing:border-box;overflow:hidden}'
new2 = '.monaco-workbench .part>.title,.monaco-workbench .part>.header-or-footer{height:0px!important;min-height:0px!important;box-sizing:border-box;overflow:hidden!important;padding:0!important;margin:0!important;border:0!important}'
if old2 in content:
    content = content.replace(old2, new2)
    patches_applied += 1
    print('  ✓ Patch 2: Hide .part>.title height')
elif new2 in content:
    print('  ~ Patch 2: Already applied')
else:
    missing.append('Patch 2')
    print('  ✗ Patch 2: Target not found')

# Patch 3: Hide terminal tabs sidebar (nth-child selector)
patch3 = '.terminal-outer-container .split-view-container > .split-view-view:nth-child(2){display:none!important}'
if patch3 not in content:
    if '/* Custom UI Style End */' in content:
        content = content.replace('/* Custom UI Style End */', patch3 + '\n/* Custom UI Style End */')
    else:
        content += '\n/* Manual Patches Start */\n' + patch3 + '\n/* Manual Patches End */\n'
    patches_applied += 1
    print('  ✓ Patch 3: Hide terminal tabs sidebar (nth-child)')
else:
    print('  ~ Patch 3: Already applied')

# Patch 4: Hide terminal tabs sidebar (:has selector for broader coverage)
patch4 = '.split-view-view:has(> .tabs-container){display:none!important;width:0!important}'
if patch4 not in content:
    if '/* Custom UI Style End */' in content:
        content = content.replace('/* Custom UI Style End */', patch4 + '\n/* Custom UI Style End */')
    else:
        content += '\n' + patch4 + '\n'
    patches_applied += 1
    print('  ✓ Patch 4: Hide terminal tabs sidebar (:has selector)')
else:
    print('  ~ Patch 4: Already applied')

if content != original:
    with open(path, 'w') as f:
        f.write(content)

print(f'\nDone. {patches_applied} new patch(es) applied.')
if patches_applied > 0:
    print('Restart VSCode Insiders (Cmd+Q then reopen) for changes to take effect.')
    print('Dismiss the \"corrupt installation\" warning — this is expected.')
if missing:
    print('ERROR: target not found for: ' + ', '.join(missing) + ' (CSS structure may have changed)')
    sys.exit(1)
" || status=1

# Patch JS file to set title/header/footer height to 0
JS_FILE="$APP_DIR/Contents/Resources/app/out/vs/workbench/workbench.desktop.main.js"

if [ -f "$JS_FILE" ]; then
  echo "Patching VSCode Insiders workbench JS..."
  PATCH_TARGET_FILE="$JS_FILE" python3 -c "
import os, sys
path = os.environ['PATCH_TARGET_FILE']
with open(path, 'r') as f:
    content = f.read()
original = content

old = '.HEADER_HEIGHT=35}static{this.TITLE_HEIGHT=35}static{this.Footer_HEIGHT=35}'
new = '.HEADER_HEIGHT=0 }static{this.TITLE_HEIGHT=0 }static{this.Footer_HEIGHT=0 }'
if old in content:
    content = content.replace(old, new)
    print('  ✓ Patch 5: Set HEADER/TITLE/Footer HEIGHT to 0 in JS')
elif new in content:
    print('  ~ Patch 5: Already applied')
else:
    print('  ✗ Patch 5: Target not found')
    print('ERROR: target not found for: Patch 5 (JS structure may have changed)')
    sys.exit(1)

if content != original:
    with open(path, 'w') as f:
        f.write(content)
" || status=1
else
  # #931: a missing JS file is Patch 5's target missing in its most
  # complete form — name it and fail like a missing target does, rather
  # than silently skipping Patch 5 and exiting with the CSS status.
  echo "ERROR: VSCode Insiders JS file not found at:"
  echo "  $JS_FILE"
  echo "  ✗ Patch 5: Target not found (JS workbench file missing)"
  status=1
fi

exit $status
