#!/bin/bash
# Install the launchd agent for auto-patching VSCode Insiders
# Usage: bash ~/nix/install-vscode-patch-agent.sh

PLIST_PATH="$HOME/Library/LaunchAgents/com.drewry.patch-vscode-insiders.plist"
PATCH_SCRIPT="$HOME/nix/patch-vscode-insiders.sh"

# Ensure patch script exists and is executable
if [ ! -f "$PATCH_SCRIPT" ]; then
  echo "ERROR: Patch script not found at $PATCH_SCRIPT"
  exit 1
fi
chmod +x "$PATCH_SCRIPT"

# Unload existing agent if present
launchctl unload "$PLIST_PATH" 2>/dev/null

# Write the plist
cat > "$PLIST_PATH" << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.drewry.patch-vscode-insiders</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>/Users/drewry.pope/nix/patch-vscode-insiders.sh</string>
    </array>
    <key>WatchPaths</key>
    <array>
        <string>/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/out/vs/workbench/workbench.desktop.main.js</string>
        <string>/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/out/vs/workbench/workbench.desktop.main.css</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/patch-vscode-insiders.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/patch-vscode-insiders.log</string>
</dict>
</plist>
EOF

# Load the agent
launchctl load "$PLIST_PATH"

echo "✓ Installed and loaded com.drewry.patch-vscode-insiders"
echo "  Patch script: $PATCH_SCRIPT"
echo "  Plist: $PLIST_PATH"
echo "  Log: /tmp/patch-vscode-insiders.log"
echo ""
echo "The agent will:"
echo "  - Run at login"
echo "  - Re-patch whenever VSCode Insiders updates its JS/CSS files"
