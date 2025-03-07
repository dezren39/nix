_: {
  homebrew.casks =
    [
      # found
      "audio-hijack"
      "zen-browser"
      "1password"
      "1password-cli"
      "amethyst"
      "angry-ip-scanner"
      "audacity"
      "balenaetcher"
      "bartender"
      "element"
      "firefox"
      "hammerspoon"
      "font-hack-nerd-font"
      "font-inconsolata-g-for-powerline"
      "font-source-code-pro-for-powerline"
      # "gitkraken"
      # "gitkraken-cli"
      "handbrake"
      "imageoptim"
      "keepingyouawake"
      # "libreoffice"
      "logseq"
      "makemkv"
      "meld"
      "MKVToolNix"
      # "nextcloud"
      # "onlyoffice"
      "raycast"
      "signal"
      # "slack"
      # "sonos"
      # "tailscale"
      # "vivaldi"
      "zoom"
      "safari-technology-preview" # "safari"
      "usb-overdrive"
      "homebrew/cask/docker"
      "insomnia"
      "tableplus"
      # "ngrok"
      "postico"
      "visual-studio-code"
      "visual-studio-code@insiders"
      "wireshark"
      "cursor"
      "discord"
      "loom"
      "notion"
      "slack"
      "telegram"
      "zoom"
      "appcleaner"
      "syncthing"
      # "steam"
      "vlc"
      "raycast"
      "asana"
      "google-chrome"
    ]
    ++ [
      # adopted custom
      "1password"
      "alt-tab" # "alttab"
      "bartender" # "bartender-5"
      "github@beta" # "github-desktop"
      "google-chrome" # VERSION MISMATCH
      "mkvtoolnix" # "mkvtoolnix-88-0"
      "rectangle"
      # "superkey"
      "windows-app"
      "zed"
      "zoom" # "zoom-us"
    ]
    ++ [
      # adopted standard
      "contexts"
      "displaylink" # "displaylink-manager"
      # TODO: macOS App LoginExtension-EXE
      "drawio" # "draw-io"
      "iterm2@nightly" # iterm2 # iterm2@beta # iterm2@nightly # "iterm"
      "microsoft-edge"
      "microsoft-excel"
      "microsoft-onenote"
      "microsoft-outlook"
      "microsoft-powerpoint"
      "microsoft-teams"
      "microsoft-word"
      "onedrive" # VERSION MISMATCH
      "snagit" # "snagit-2024"
      "webex" # VERSION
    ];
}
