#!/bin/bash
# Patch VSCode Insiders workbench CSS after updates
# Run this after any VSCode Insiders update to re-apply UI patches
#
# Usage: bash ~/nix/patch-vscode-insiders.sh

CSS_FILE="/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/out/vs/workbench/workbench.desktop.main.css"

if [ ! -f "$CSS_FILE" ]; then
  echo "ERROR: VSCode Insiders CSS file not found at:"
  echo "  $CSS_FILE"
  exit 1
fi

echo "Patching VSCode Insiders workbench CSS..."

python3 -c "
path = '$CSS_FILE'
with open(path, 'r') as f:
    content = f.read()

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

with open(path, 'w') as f:
    f.write(content)

print(f'\nDone. {patches_applied} new patch(es) applied.')
if patches_applied > 0:
    print('Restart VSCode Insiders (Cmd+Q then reopen) for changes to take effect.')
    print('Dismiss the \"corrupt installation\" warning — this is expected.')
"

# Patch JS file to set title/header/footer height to 0
JS_FILE="/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/out/vs/workbench/workbench.desktop.main.js"

if [ -f "$JS_FILE" ]; then
  echo "Patching VSCode Insiders workbench JS..."
  python3 -c "
path = '$JS_FILE'
with open(path, 'r') as f:
    content = f.read()

old = '.HEADER_HEIGHT=35}static{this.TITLE_HEIGHT=35}static{this.Footer_HEIGHT=35}'
new = '.HEADER_HEIGHT=0 }static{this.TITLE_HEIGHT=0 }static{this.Footer_HEIGHT=0 }'
if old in content:
    content = content.replace(old, new)
    print('  ✓ Patch 5: Set HEADER/TITLE/Footer HEIGHT to 0 in JS')
elif new in content:
    print('  ~ Patch 5: Already applied')
else:
    print('  ✗ Patch 5: Target not found')

with open(path, 'w') as f:
    f.write(content)
"
fi
