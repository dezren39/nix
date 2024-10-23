# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin
# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin/dock/default.nix
#
{ config, pkgs, inputs, ... }: {
  # List packages installed in system profile. To search by name, run:
  # $ nix-env -qaP | grep wget

  nixpkgs = {
    config = {
      allowUnfree = true;
      #cudaSupport = true;
      #cudaCapabilities = ["8.0"];
      allowBroken = true;
      allowInsecure = false;
      allowUnsupportedSystem = true;
    };
    # overlays = ...
  };

  environment.systemPackages = with pkgs; [ # possibly not darwin
    _1password
    act
    age
    age-plugin-yubikey
    alacritty
    ansible
    arduino-cli
    aspell
    aspellDicts.en
    awscli
    bandwhich
    bash-completion
    bat
    # bitwarden-cli
    # whalebrew?
    black
    btop
    buf
    caddy
    certstrap
    cfssl
    cocoapods
    coreutils
    curl
    dbmate
    dejavu_fonts
    deno
    devenv
    difftastic
    dive
    du-dust
    emacs
    # emacs-unstable
    emacs-all-the-icons-fonts
    fastlane
    fd
    ffmpeg
    flyctl
    font-awesome
    fzf
    gcc
    gh
    git
    git-filter-repo
    glow
    gnupg
    gnused
    go
    # golangci # ???
    gomplate
    google-cloud-sdk
    gopls
    goreleaser
    graphviz
    gum
    hack-font
    hcloud
    htop
    httpie
    hugo
    hunspell
    iftop
    imagemagick
    inetutils
    ipcalc
    # jdk17
    jetbrains-mono
    jetbrains.phpstorm
    jpegoptim
    jq
    jwt-cli
    k3d
    k9s
    killall
    kubectl
    libfido2
    lima
    lnav
    mas
    meslo-lgs-nf
    micro
    mitmproxy
    mutagen
    mutagen-compose
    nats-server
    natscli
    ncdu
    neofetch
    neovim
    ngrok
    nmap
    nodejs
    noto-fonts
    noto-fonts-emoji
    openfortivpn
    openjdk
    openssh
    p7zip
    pandoc
    pgcli
    php81
    platformio
    pngquant
    protobuf
    protoc-gen-go
    protoc-gen-go-grpc
    python39
    python39Packages.virtualenv
    redis
    ripgrep
    slack
    sops
    sqlite
    ssh-to-age
    ssm-session-manager-plugin
    symfony-cli
    tflint
    tmux
    tree
    trivy
    unrar
    unzip
    upx
    # vector
    vim
    watchman
    wget
    yarn
    zip
    zsh-powerlevel10k
  ] ++
  [ # darwin
    aerospace
    dockutil
    fswatch
    rectangle
  ];

  services.nix-daemon.enable = true;
  # nix.package = pkgs.nix # disabled because using determinate nix

  programs.zsh.enable = true;  # default shell on catalina # https://github.com/dustinlyons/nixos-config/blob/main/modules/shared/config/p10k.zsh
  programs.fish.enable = true;
  programs.bash.enable = true;

  system.configurationRevision = inputs.self.rev or inputs.self.dirtyRev or null; # Set Git commit hash for darwin-version.

  # Used for backwards compatibility, please read the changelog before changing.
  # $ darwin-rebuild changelog
  system.stateVersion = 5;

  # The platform the configuration will be used on.
  nixpkgs.hostPlatform = "aarch64-darwin";

  users.users."drewry.pope" = {
      name = "drewry.pope";
      home = "/Users/drewry.pope";
  };
  home-manager = {
    useGlobalPkgs = true;
    useUserPackages = true;
    users = {
      "drewry.pope" = import ./home.nix;
    };
    sharedModules = [
      inputs.mac-app-util.homeManagerModules.default
    ];
    extraSpecialArgs = {
      inherit inputs;
    };
  };
  nix-homebrew = {
    enable = true;
    enableRosetta = true;
    user = "drewry.pope";
    taps = {
      "homebrew/homebrew-core" = inputs.homebrew-core;
      "homebrew/homebrew-cask" = inputs.homebrew-cask;
      "homebrew/homebrew-bundle" = inputs.homebrew-bundle;
      "null-dev/homebrew-firefox-profile-switcher" = inputs.homebrew-firefox-profile-switcher;
    };
    mutableTaps = false;
    autoMigrate = true;
  };
  homebrew = {
    enable = true;
    # global = {
      # lockfiles
      # brewFile
      # autoUpdate
    # };
    # brewOptions
    # caskArgsOptions
    # tapOptions
    onActivation = {
      autoUpdate = true;
      cleanup = "uninstall";
      # cleanup = "zap";
      upgrade = true;
      extraFlags = [ "--verbose" ];
    };
    # caskArgs
    taps = builtins.attrNames config.nix-homebrew.taps;
    # brewfile
    # extraConfig
    # whalebrews
    brews = [
      "cowsay"
      "fastfetch"
      "ffmpeg"
      "firefox-profile-switcher-connector"
      "telnet"
    ];
    casks = [
      "1password"
      "1password-cli"
      "amethyst"
      "angry-ip-scanner"
      "audacity"
      "balenaetcher"
      "bartender"
      "element"
      "firefox"
      "font-hack-nerd-font"
      "font-inconsolata-g-for-powerline"
      "font-source-code-pro-for-powerline"
      # "gitkraken"
      # "gitkraken-cli"
      "handbrake"
      "imageoptim"
      "keepingyouawake"
      "libreoffice"
      "logseq"
      "makemkv"
      "meld"
      "MKVToolNix"
      # "nextcloud"
      # "onlyoffice"
      "raycast"
      "signal"
      "slack"
      # "sonos"
      # "tailscale"
      # "vivaldi"
      "zoom"
      "safari-technology-preview" # "safari"
    ] ++ [ # adopted custom
      "1password"
      "alt-tab" # "alttab"
      "bartender" # "bartender-5"
      "github@beta" # "github-desktop"
      "google-chrome" # VERSION MISMATCH
      "mkvtoolnix" # "mkvtoolnix-88-0"
      "rectangle"
      "superkey"
      "windows-app"
      "zed"
      "zoom" # "zoom-us"
    ] ++[ # adopted standard
      "contexts"
      "displaylink" # "displaylink-manager"
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
    masApps = { # Mac App Store must be logged in & apps must be purchased first
      "1Password for Safari" = 1569813296;
      "GarageBand" = 682658836;
      "iMovie" = 408981434;
      "Keynote" = 409183694;
      "Numbers" = 409203825;
      "Pages" = 409201541;
    };
    # preInstalledAndNotFoundInNixOrBrewOrAppStore = [ # apps that are pre-installed on a macOS but not found in nixpkgs, homebrew, or the Mac App Store
    #   "arctic-wolf-agent-manager"
    #   "arctic-wolf-agent-notifier"
    #   "company-portal"
    #   "jamf-connect"
    #   "thousandeyes-endpoint-agent"
    #   "vertex-inc--self-service"
    #   "workday"
    # ];
  };
  # nix = {
  #   configureBuildUsers = true;
  #   extraOptions = ''
  #     extra-nix-path = nixpkgs=flake:nixpkgs
  #     upgrade-nix-store-path-url = https://install.determinate.systems/nix-upgrade/stable/universal
  #   '';
  #   gc = {
  #     user = "root";
  #     automatic = true;
  #     interval = { Weekday = 0; Hour = 2; Minute = 0; };
  #     options = "--delete-older-than 30d";
  #   };
  # };
  # system.checks.verifyNixPath = false;
  launchd.user.agents = {
    naturalScrollingToggle = {
      path = [ config.environment.systemPath ];
      serviceConfig = {
        KeepAlive = false;
        RunAtLoad = true;
        ProgramArguments = [
          "/bin/sh"
          "-c"
          "if system_profiler SPUSBDataType | grep -i \"Mouse\"; then defaults write NSGlobalDomain com.apple.swipescrolldirection -bool false; else defaults write NSGlobalDomain com.apple.swipescrolldirection -bool true; fi && killall Finder"
        ];
        StandardErrorPath = "/tmp/natural_scrolling.err.log";
        StandardOutPath = "/tmp/natural_scrolling.out.log";
      };
    };
  };
  system = {
    defaults = {
      LaunchServices = {
        LSQuarantine = false;
      };

      NSGlobalDomain = {
        AppleShowAllExtensions = true;
        ApplePressAndHoldEnabled = false;

        # 120, 90, 60, 30, 12, 6, 2
        KeyRepeat = 2;

        # 120, 94, 68, 35, 25, 15
        InitialKeyRepeat = 15;

        "com.apple.mouse.tapBehavior" = 1;
        "com.apple.sound.beep.volume" = 0.0;
        "com.apple.sound.beep.feedback" = 0;
      };

      # dock = {
      # https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin/home-manager.nix#L70
      #   autohide = true;
      #   show-recents = true;
      #   launchanim = true;
      #   mouse-over-hilite-stack = true;
      #   orientation = "bottom";
      #   tilesize = 48;
      # };

      finder = {
        _FXShowPosixPathInTitle = false;
      };

      trackpad = {
        Clicking = true;
        TrackpadThreeFingerDrag = true;
      };
    };

    # keyboard = {
    #   enableKeyMapping = true;
    #   remapCapsLockToControl = true;
    # };
  };
} // import ./nix.settings.nix
