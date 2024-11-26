# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin
# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin/dock/default.nix
#
{ config, pkgs, lib, inputs, ... }: lib.recursiveUpdate {
  imports = [
    ./systemPackages.nix
    ./brews.nix
    ./casks.nix
    ./masApps.nix
    # ./home.nix
  ];
  # List packages installed in system profile. To search by name, run:
  # $ nix-env -qaP | grep wget

  nixpkgs = { # TODO: module nixpkgs
    hostPlatform = "aarch64-darwin";
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


   # TODO: module services
  services.nix-daemon.enable = true;
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
      inherit inputs;
    };
  };
  # TODO: module nix-homebrew
  nix-homebrew = {
    enable = true;
    enableRosetta = true;
    user = "drewry.pope";
    taps = {
      "homebrew/homebrew-core" = inputs.homebrew-core;
      "homebrew/homebrew-cask" = inputs.homebrew-cask;
      "homebrew/homebrew-bundle" = inputs.homebrew-bundle;
      "homebrew/homebrew-services" = inputs.nixpkgs.legacyPackages."${pkgs.system}".applyPatches {
        name = "homebrew-services-patched"; # https://github.com/zhaofengli/nix-homebrew/issues/13#issuecomment-2156223912
        src = inputs.homebrew-services;
        patches = [./homebrew-services.patch];
      };
      "null-dev/homebrew-firefox-profile-switcher" = inputs.homebrew-firefox-profile-switcher;
      "Dimentium/homebrew-autoraise" = inputs.homebrew-autoraise;
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
  };
} (import ./nix.settings.nix) # TODO: module nix-settings
