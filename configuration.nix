# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin
# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin/dock/default.nix
#
{
  config,
  pkgs,
  lib,
  inputs,
  system,
  ...
}:
lib.recursiveUpdate {
  imports = [
    ./systemPackages.nix
    ./brews.nix
    ./casks.nix
    ./masApps.nix
    ./services.nix
    # ./home.nix
  ];

  # =========================================================================
  # System-wide git safety — applies to ALL users, ALL git binaries
  # =========================================================================

  # Patterns file at /etc/gitignore
  environment.etc."gitignore".text = ''
    # codedb snapshots — never commit anywhere
    codedb.snapshot

    # lootbox ephemeral dirs (scripts are fine)
    .lootbox/cache/
    .lootbox/tmp/
  '';

  # System gitconfig at /etc/gitconfig — tells git to use the patterns file
  # Apple/brew git reads /etc/gitconfig by default; nix git needs the env var below
  environment.etc."gitconfig".text = ''
    [core]
    	excludesFile = /etc/gitignore
  '';

  # Force nix-packaged git to read /etc/gitconfig (it normally reads $nixStore/etc/gitconfig)
  environment.variables.GIT_CONFIG_SYSTEM = "/etc/gitconfig";
  # List packages installed in system profile. To search by name, run:
  # $ nix-env -qaP | grep wget

  nixpkgs = {
    # TODO: module nixpkgs
    hostPlatform = "aarch64-darwin";
    config = {
      allowUnfree = true;
      #cudaSupport = true;
      #cudaCapabilities = ["8.0"];
      allowBroken = true;
      allowInsecure = false;
      allowUnsupportedSystem = true;
    };
    overlays = [
      inputs.fenix.overlays.default
      (final: prev: {
        noTunes = final.callPackage ./pkgs/noTunes.nix { };
      })
      # (final: prev: {
      #   helium =
      #     (import inputs.nixpkgs-helium {
      #       inherit (prev) system;
      #       config = prev.config;
      #     }).helium;
      # })
    ];
  };

  # nix.package = pkgs.nix # disabled because using determinate nix

  # TODO: module system
  system = {
    configurationRevision = inputs.self.rev or inputs.self.dirtyRev or null; # Set Git commit hash for darwin-version.
    stateVersion = 5;
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

      # =====================================================================
      # Spotlight — trim search categories and disable noisy result types
      # =====================================================================
      CustomUserPreferences = {
        "com.apple.Spotlight" = {
          orderedItems = [
            {
              enabled = true;
              name = "APPLICATIONS";
            }
            {
              enabled = true;
              name = "SYSTEM_PREFS";
            }
            {
              enabled = true;
              name = "MENU_EXPRESSION";
            } # Calculator
            {
              enabled = true;
              name = "MENU_CONVERSION";
            } # Unit conversion
            {
              enabled = true;
              name = "MENU_DEFINITION";
            } # Dictionary
            {
              enabled = false;
              name = "DIRECTORIES";
            } # Folders
            {
              enabled = false;
              name = "PDF";
            }
            {
              enabled = false;
              name = "DOCUMENTS";
            }
            {
              enabled = false;
              name = "FONTS";
            }
            {
              enabled = false;
              name = "MESSAGES";
            }
            {
              enabled = false;
              name = "CONTACT";
            }
            {
              enabled = false;
              name = "EVENT_TODO";
            }
            {
              enabled = false;
              name = "IMAGES";
            }
            {
              enabled = false;
              name = "BOOKMARKS";
            }
            {
              enabled = false;
              name = "MUSIC";
            }
            {
              enabled = false;
              name = "MOVIES";
            }
            {
              enabled = false;
              name = "PRESENTATIONS";
            }
            {
              enabled = false;
              name = "SPREADSHEETS";
            }
            {
              enabled = false;
              name = "SOURCE";
            }
            {
              enabled = false;
              name = "MENU_OTHER";
            }
            {
              enabled = false;
              name = "MENU_WEBSEARCH";
            } # Siri suggestions
            {
              enabled = false;
              name = "MENU_SPOTLIGHT_SUGGESTIONS";
            }
          ];
        };
      };
    };

    # keyboard = {
    #   enableKeyMapping = true;
    #   remapCapsLockToControl = true;
    # };
  };

  # TODO: module users
  users.users."drewry.pope" = {
    name = "drewry.pope";
    home = "/Users/drewry.pope";
  };
  # TODO: module home-manager
  home-manager = {

    useGlobalPkgs = true;
    useUserPackages = true;
    users = {
      # TODO: module per-user home manager
      "drewry.pope" = import ./homeUser.nix;
    };
    sharedModules = [
      inputs.mac-app-util.homeManagerModules.default
    ];
    extraSpecialArgs = {
      inherit inputs system;
    };
    backupFileExtension = ".backup";
  };
  # TODO: module nix-homebrew
  nix-homebrew = {
    enable = true;
    enableRosetta = true;
    user = "drewry.pope";
    # Workaround: nix-homebrew uses Ruby 4.0 but brew vendors gems under ruby/3.4.0.
    # bundler/setup.rb resolves to ruby/4.0.0/ which doesn't exist, so gems like
    # sorbet-runtime fail to load. Also, install_bundler_gems! tries to write into the
    # read-only Nix store (gems.rb mkpath). Fix both by:
    # 1. Symlinking ruby/4.0.0 → 3.4.0 so bundler/setup.rb finds vendored gems
    # 2. Setting HOMEBREW_SKIP_INITIAL_GEM_INSTALL to prevent writes to Nix store
    # ref: https://github.com/zhaofengli/nix-homebrew/issues/35
    package = pkgs.runCommandLocal "brew-src-ruby40-compat" { } ''
      cp -r "${inputs.brew-src}" "$out"
      chmod u+w "$out/Library/Homebrew/vendor/bundle/ruby"
      ln -s 3.4.0 "$out/Library/Homebrew/vendor/bundle/ruby/4.0.0"
    '';
    extraEnv.HOMEBREW_SKIP_INITIAL_GEM_INSTALL = "1";
    taps = {
      "homebrew/homebrew-core" = inputs.homebrew-core;
      "homebrew/homebrew-cask" = inputs.homebrew-cask;
      "homebrew/homebrew-bundle" = inputs.homebrew-bundle;
      # "homebrew/homebrew-services" = inputs.nixpkgs.legacyPackages."${pkgs.system}".applyPatches
      # {
      #   name = "homebrew-services-patched"; # https://github.com/zhaofengli/nix-homebrew/issues/13#issuecomment-2156223912
      #   src = inputs.homebrew-services;
      #   patches = [ ./homebrew-services.patch ];
      # };
      "null-dev/homebrew-firefox-profile-switcher" = inputs.homebrew-firefox-profile-switcher;
      "Dimentium/homebrew-autoraise" = inputs.homebrew-autoraise;
      "gromgit/homebrew-fuse" = inputs.homebrew-fuse;

    };
    mutableTaps = false;
    autoMigrate = true;
  };
  # TODO: module homebrew
  homebrew = {
    # https://github.com/BatteredBunny/brew-nix
    # https://github.com/jcszymansk/nixcasks
    enable = true;
    global = {
      # lockfiles
      # brewFile
      autoUpdate = true;
    };
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
  nix = {
    enable = false;
    # package = pkgs.nixVersions.nix_2_24;
    package = lib.mkForce pkgs.nixVersions.git; # forcing because determinate nix wants an older version, if problems try commenting the above line and reverting to the determinate nix version, probably 2.24.10 or something
    # package = pkgs.nixVersions.nix_2_25;
    # package = pkgs.nixVersions.nix_2_26;
    # package = pkgs.nixVersions.nix_2_42;
  };
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
  system.primaryUser = "drewry.pope";

  # Restart skhd after rebuild so config changes take effect
  # Fix Spotlight indexing and exclude noisy directories
  system.activationScripts.postActivation.text = ''
    if /bin/launchctl list | grep -q org.nixos.skhd; then
      /bin/launchctl kickstart -k "gui/$(id -u)/org.nixos.skhd" || true
    fi

    # =====================================================================
    # Spotlight indexing fixes
    # =====================================================================

    # Disable Spotlight indexing on /nix (huge read-only store, never useful)
    if [ -d /nix ]; then
      /usr/bin/mdutil -i off /nix 2>/dev/null || true
      # Marker file tells Spotlight to never index this volume/directory
      /usr/bin/touch /nix/.metadata_never_index 2>/dev/null || true
    fi

    # If Spotlight is stuck in transitioning state, rebuild the index
    if /usr/bin/mdutil -s / 2>&1 | grep -q "kMDConfigSearchLevelTransitioning"; then
      echo "Spotlight stuck in transitioning state — rebuilding index..."
      /usr/bin/mdutil -E / 2>/dev/null || true
    fi

    # Add .metadata_never_index to common dev/cache directories in $HOME
    HOME_DIR="/Users/drewry.pope"
    for dir in \
      "$HOME_DIR/.nix-defexpr" \
      "$HOME_DIR/.nix-profile" \
      "$HOME_DIR/.local/state/nix" \
      "$HOME_DIR/.cache" \
      "$HOME_DIR/Library/Caches"; do
      if [ -d "$dir" ]; then
        /usr/bin/touch "$dir/.metadata_never_index" 2>/dev/null || true
      fi
    done

    # Point xcode-select at full Xcode.app if installed (idempotent, instant)
    if [ -d "/Applications/Xcode.app/Contents/Developer" ]; then
      /usr/bin/xcode-select -s /Applications/Xcode.app/Contents/Developer 2>/dev/null || true
      # Accept Xcode license (no-op if already accepted)
      /usr/bin/xcodebuild -license accept 2>/dev/null || true
      # Install additional components on first launch (no-op if already done)
      /usr/bin/xcodebuild -runFirstLaunch 2>/dev/null || true
    fi
  '';

  # TODO: module launchd
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
    lootbox = {
      path = [
        config.environment.systemPath
        "/Users/drewry.pope/.deno/bin"
      ];
      serviceConfig = {
        KeepAlive = true;
        RunAtLoad = true;
        WorkingDirectory = "/Users/drewry.pope/.config/nix";
        ProgramArguments = [
          "/bin/sh"
          "-c"
          ''
            export PATH="/Users/drewry.pope/.deno/bin:$PATH"
            # Install lootbox if not present
            if ! command -v lootbox &>/dev/null; then
              curl -fsSL https://raw.githubusercontent.com/jx-codes/lootbox/main/install.sh | bash
            fi
            # Run lootbox server (foreground so launchd can manage it)
            exec lootbox server --port 9420
          ''
        ];
        StandardErrorPath = "/tmp/lootbox.err.log";
        StandardOutPath = "/tmp/lootbox.out.log";
      };
    };
  };
} (import ./nix.settings.nix) # TODO: module nix-settings
