_: {
  homebrew.casks = [
    "1password"
    "1password-cli"
    "amethyst"
    # "angry-ip-scanner" # DEPRECATED — find replacement
    "appcleaner"
    "asana"
    "audacity"
    "audio-hijack"
    "balenaetcher"
    "bartender"
    "betterdisplay"
    "cmux" # nixpkgs PR pending: https://github.com/NixOS/nixpkgs/pull/493537
    "contexts"
    "copilot-cli"
    # "cursor" # removed 2026-08: ~16.5 GB (13 GB extensions); using VS Code + opencode
    # "discord" # removed 2026-08: overlapping chat tools
    "display-pilot"
    "displaylink"
    "drawio"
    # "element" # removed 2026-08: overlapping chat tools
    "firefox"
    "font-hack-nerd-font"
    "font-inconsolata-g-for-powerline"
    "font-source-code-pro-for-powerline"
    # "ghostty" # switched to nixpkgs ghostty-bin (pre-built binary managed by home-manager)
    # "gitkraken"
    # "gitkraken-cli"
    "github@beta"
    "google-chrome"
    "hammerspoon"
    "handbrake-app"
    "homebrew/cask/docker-desktop"
    "imageoptim"
    "insomnia"
    "iterm2@nightly"
    "keepingyouawake"
    "keka"
    "keycastr"
    # "libreoffice"
    "logseq"
    "loom"
    "macfuse" # required for bindfs (opencode-share)
    # "makemkv"
    # "meld"
    "microsoft-edge"
    "microsoft-excel"
    "microsoft-onenote"
    "microsoft-outlook"
    "microsoft-powerpoint"
    "microsoft-teams"
    "microsoft-word"
    # "mkvtoolnix-app" # renamed from mkvtoolnix
    # "nextcloud"
    # "ngrok"
    "notion"
    "onedrive"
    "onyx" # system maintenance, cache cleaning, Spotlight reindexing
    # "onlyoffice"
    "postico"
    "raycast"
    "rectangle"
    "safari-technology-preview"
    # "signal" # removed 2026-08: overlapping chat tools
    # "slack" # removed 2026-08: overlapping chat tools
    "snagit"
    # "sonos"
    # "steam"
    # "superkey"
    "syncthing-app" # renamed from syncthing
    "tableplus"
    # "tailscale"
    "telegram"
    "usb-overdrive"
    # "vivaldi"
    # "visual-studio-code" # removed 2026-09: reclaiming disk (extensions ~9.7 GB); using opencode
    # "visual-studio-code@insiders" # removed 2026-08: 13 GB of duplicate extensions vs stable
    "vlc"
    "webex"
    "windows-app"
    "wireshark-app" # renamed from wireshark
    "zed"
    # Zen ships exactly two channels, and Homebrew mirrors them one-to-one.
    # There is no zen@beta -- the cask named "zen" IS the beta stream:
    #
    #   "zen"           1.21.13b   suffix b = beta      (Zen.app)
    #   "zen@twilight"  1.22t      suffix t = twilight  (Twilight.app)
    #
    # Twilight is the newer stream and owns the real profile
    # (Profiles/df40mpog.Default (alpha), ~2.4 GB). "zen" is kept installed
    # only as a fallback -- TODO: drop it once Twilight is proven.
    #
    # GOTCHA: both share bundle id app.zen-browser.zen AND profile dir
    # (application.ini Profile=zen), so they differ only by install path
    # (/Applications/Zen.app vs /Applications/Twilight.app). Each path gets its
    # own install hash in installs.ini, and a NEW hash silently creates a BLANK
    # profile -- which is exactly how the profile appeared "lost". If a channel
    # is ever reinstalled, re-point its install id at the alpha profile before
    # first launch. Never run both at once: Firefox-family profiles are locked
    # to a single process.
    "zen"
    "zen@twilight"
    "zoom"
  ];
}
