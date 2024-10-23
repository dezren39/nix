# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin
# https://github.com/dustinlyons/nixos-config/blob/main/modules/darwin/dock/default.nix
#
{ pkgs, inputs, ... }: {
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

      environment.systemPackages =
        with pkgs;
        [
            vim
            p7zip
            #zed-editor
            #oks,micro
            micro

        ] ++
        [
          # General packages for development and system management
          act
          alacritty
          aspell
          aspellDicts.en
          bash-completion
          bat
          btop
          coreutils
          difftastic
          du-dust
          gcc
          git-filter-repo
          killall
          neofetch
          openssh
          pandoc
          sqlite
          wget
          zip

          # Encryption and security tools
          _1password
          age
          age-plugin-yubikey
          gnupg
          libfido2

          # Cloud-related tools and SDKs
          # docker
          # docker-compose
          # awscli2 - marked broken Mar 22
          flyctl
          google-cloud-sdk
          go
          gopls
          ngrok
          ssm-session-manager-plugin
          # terraform # fails?
          # terraform-ls # fails?
          tflint

          # Media-related packages
          emacs-all-the-icons-fonts
          imagemagick
          dejavu_fonts
          ffmpeg
          fd
          font-awesome
          glow
          hack-font
          jpegoptim
          meslo-lgs-nf
          noto-fonts
          noto-fonts-emoji
          pngquant

          # PHP
          # php82
          # php82Packages.composer
          # php82Packages.php-cs-fixer
          # php82Extensions.xdebug
          # php82Packages.deployer
          # phpunit

          # Node.js development tools
          fzf
          # nodePackages.live-server
          # nodePackages.nodemon
          # nodePackages.prettier
          # nodePackages.npm
          # nodejs

          # Source code management, Git, GitHub tools
          gh

          # Text and terminal utilities
          htop
          hunspell
          iftop
          jetbrains-mono
          jetbrains.phpstorm
          jq
          ripgrep
          slack
          tree
          tmux
          unrar
          unzip
          zsh-powerlevel10k

          # Python packages
          black
          python39
          python39Packages.virtualenv
        ] ++
        # darwin
        [
        aerospace
        fswatch
        dockutil
        rectangle
        ];

      # Auto upgrade nix package and the daemon service.
      services.nix-daemon.enable = true;
      # nix.package = pkgs.nix;

      # Necessary for using flakes on this system.
      nix.settings.experimental-features = "nix-command flakes";

      # Create /etc/zshrc that loads the nix-darwin environment.
      # https://github.com/dustinlyons/nixos-config/blob/main/modules/shared/config/p10k.zsh
      programs.zsh.enable = true;  # default shell on catalina
      # programs.fish.enable = true;
      programs.fish.enable = true;
      programs.bash.enable = true;

      # Set Git commit hash for darwin-version.
      system.configurationRevision = inputs.self.rev or inputs.self.dirtyRev or null;

      # Used for backwards compatibility, please read the changelog before changing.
      # $ darwin-rebuild changelog
      system.stateVersion = 5;


      # The platform the configuration will be used on.
      nixpkgs.hostPlatform = "aarch64-darwin";

    users.users."drewry.pope" = {
        name = "drewry.pope";
        home = "/Users/drewry.pope";
    };
    homebrew = {
    enable = true;
    # onActivation.cleanup = "uninstall";

    taps = [];
    brews = [ "cowsay" ];
    casks = [];
};
    }

    /*
    { agenix, config, pkgs, ... }:

    let user = "dustin"; in
    {

      imports = [
        ../../modules/darwin/secrets.nix
        ../../modules/darwin/home-manager.nix
        ../../modules/shared
        agenix.darwinModules.default
      ];

      # Auto upgrade nix package and the daemon service.
      services.nix-daemon.enable = true;

      # Setup user, packages, programs
      nix = {
        package = pkgs.nix;
        configureBuildUsers = true;

        settings = {
          trusted-users = [ "@admin" "${user}" ];
          substituters = [ "https://nix-community.cachix.org" "https://cache.nixos.org" ];
          trusted-public-keys = [ "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=" ];
        };

        gc = {
          user = "root";
          automatic = true;
          interval = { Weekday = 0; Hour = 2; Minute = 0; };
          options = "--delete-older-than 30d";
        };

        # Turn this on to make command line easier
        extraOptions = ''
          experimental-features = nix-command flakes
        '';
      };

      # Turn off NIX_PATH warnings now that we're using flakes
      system.checks.verifyNixPath = false;

      # Load configuration that is shared across systems
      environment.systemPackages = with pkgs; [
        emacs-unstable
        agenix.packages."${pkgs.system}".default
      ] ++ (import ../../modules/shared/packages.nix { inherit pkgs; });

      launchd.user.agents = {
        emacs = {
          path = [ config.environment.systemPath ];
          serviceConfig = {
            KeepAlive = true;
            ProgramArguments = [
              "/bin/sh"
              "-c"
              "{ osascript -e 'display notification \"Attempting to start Emacs...\" with title \"Emacs Launch\"'; /bin/wait4path ${pkgs.emacs}/bin/emacs && { ${pkgs.emacs}/bin/emacs --fg-daemon; if [ $? -eq 0 ]; then osascript -e 'display notification \"Emacs has started.\" with title \"Emacs Launch\"'; else osascript -e 'display notification \"Failed to start Emacs.\" with title \"Emacs Launch\"' >&2; fi; } } &> /tmp/emacs_launch.log"
            ];
            StandardErrorPath = "/tmp/emacs.err.log";
            StandardOutPath = "/tmp/emacs.out.log";
          };
        };

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
        stateVersion = 4;

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

          dock = {
            autohide = false;
            show-recents = false;
            launchanim = true;
            mouse-over-hilite-stack = true;
            orientation = "bottom";
            tilesize = 48;
          };

          finder = {
            _FXShowPosixPathInTitle = false;
          };

          trackpad = {
            Clicking = true;
            TrackpadThreeFingerDrag = true;
          };
        };

        keyboard = {
          enableKeyMapping = true;
          remapCapsLockToControl = true;
        };
      };
    }
    */
