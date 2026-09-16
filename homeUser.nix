# home.nix
{
  config,
  pkgs,
  lib,
  inputs,
  system,
  ...
}:
let
  # The activation helper defaults to all providers; this config uses OpenCode only.
  sidepulseProviders = [ "opencode" ];
  mkSidePulseActivation =
    {
      providers ? [ "all" ],
    }:
    lib.hm.dag.entryAfter [ "writeBoundary" ] ''
      for provider in ${lib.escapeShellArgs providers}; do
        run ${lib.getExe inputs.self.packages.${system}.sidepulse} setup "$provider" \
          --sd-eject-guard-scope user
      done
    '';
in
lib.recursiveUpdate {
  home = lib.recursiveUpdate {
    stateVersion = "23.05";
    file = {
      ".aerospace.toml".source = ./.aerospace.toml;
      ".config/lootbox/lootbox.config.json".source = ./lootbox.config.json;
      # Deliberately-inert ~/.gitconfig. Git reads ~/.config/git/config first and
      # ~/.gitconfig second, so a stray file here silently overrides everything
      # home-manager writes — which is exactly what happened: an unmanaged 555-byte
      # ~/.gitconfig was overriding core.editor, safe.directory and pull.rebase.
      # Owning it as a read-only store symlink keeps that from recurring.
      # Side effect: `git config --global ...` now fails (read-only). That is
      # intentional — edit gitSettings.nix instead.
      ".gitconfig".text = ''
        # Managed by nix — intentionally contains no settings.
        #
        # Real git configuration lives in:
        #   ~/.config/nix/gitSettings.nix   (single source of truth)
        #     -> ~/.config/git/config       (home-manager, per-user)
        #     -> /etc/gitconfig             (nix-darwin, system-wide)
        #
        # This file exists only so a stray ~/.gitconfig cannot shadow those.
      '';
      # npm: set global install prefix to a writable user directory
      ".npmrc".text = "prefix=~/.npm-global\n";
      # Toggle menu bar visibility (bind to skhd shortcut)
      ".local/bin/toggle-menubar".executable = true;
      ".local/bin/toggle-menubar".text = ''
        #!/bin/bash
        # Toggle macOS menu bar auto-hide (reclaims notch space when hidden)
        current=$(defaults read NSGlobalDomain _HIHideMenuBar 2>/dev/null || echo 0)
        if [ "$current" = "1" ]; then
          defaults write NSGlobalDomain _HIHideMenuBar -bool false
          echo "Menu bar: visible (notch space used by menu bar)"
        else
          defaults write NSGlobalDomain _HIHideMenuBar -bool true
          echo "Menu bar: hidden (notch space reclaimed, shows on hover)"
        fi
        killall Dock 2>/dev/null || true
      '';
    };
    activation = {
      # reloadAerospace = lib.hm.dag.entryAfter ["writeBoundary"] ''
      #   $DRY_RUN_CMD ${pkgs.aerospace}/bin/aerospace reload-config
      # '';
      installPlaywright = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        run ${pkgs.nodejs}/bin/npm install -g playwright@latest 2>/dev/null || true
      '';
      # installGhCopilot: gh copilot is now built into gh (>=2.65) and executes
      # the native `copilot` binary from PATH (managed via copilot-cli cask).
      installPlannotator = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        run ${pkgs.curl}/bin/curl -fsSL https://plannotator.ai/install.sh | run ${pkgs.bash}/bin/bash 2>/dev/null || true
      '';
      setupSidePulse = mkSidePulseActivation { providers = sidepulseProviders; };
      installAltTab = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        app="$HOME/Applications/AltTab.app"
        staging="$HOME/Applications/.AltTab.staging.app"
        was_running=0
        /usr/bin/pgrep -x AltTab >/dev/null && was_running=1 || true

        run /usr/bin/killall AltTab 2>/dev/null || true
        run /bin/rm -rf "$staging"
        run /usr/bin/ditto "${pkgs.alt-tab-debug}/Applications/AltTab.app" "$staging"
        run /bin/chmod -R u+w "$staging"
        run /usr/bin/codesign --force --deep \
          --sign DB9A68C7370300622592A710E5EA85C5EFA52604 \
          --options runtime \
          --preserve-metadata=identifier,entitlements,flags \
          "$staging"
        run /usr/bin/codesign --verify --deep --strict "$staging"
        run /bin/rm -rf "$app"
        run /bin/mv "$staging" "$app"
        run /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$app"

        if [ "$was_running" = 1 ]; then
          run /usr/bin/open "$app"
        fi
      '';
    };
    sessionVariables = {
      EDITOR = "zed";
      LANG = "en_US.UTF-8";
      COPILOT_MODEL = "claude-opus-4.5";
      OPENCODE_EXPERIMENTAL = "1";

      # Go: user-local install directory
      GOPATH = "$HOME/go";
      GOBIN = "$HOME/go/bin";

      # Shell history — most precise timestamp format (ISO date + HH:MM:SS + timezone)
      HISTTIMEFORMAT = "%F %T %Z ";
      HISTSIZE = "100000";
      HISTFILESIZE = "200000";
      HISTCONTROL = "ignoreboth:erasedups";
      HISTIGNORE = "ls:cd:pwd:exit:clear:history";
    };
  } (import ./homePackages.nix { inherit config pkgs; });
} (import ./homePrograms.nix { inherit inputs system pkgs; })
/*
  { config, pkgs, lib, home-manager, ... }:

  let
    user = "dustin";
    # Define the content of your file as a derivation
    myEmacsLauncher = pkgs.writeScript "emacs-launcher.command" ''
      #!/bin/sh
        emacsclient -c -n &
    '';
    sharedFiles = import ../shared/files.nix { inherit config pkgs; };
    additionalFiles = import ./files.nix { inherit user config pkgs; };
  in
  {
    imports = [
     ./dock
    ];

    users.users.${user} = {
      name = "${user}";
      home = "/Users/${user}";
      isHidden = false;
      shell = pkgs.zsh;
    };

    homebrew = {
      # This is a module from nix-darwin
      # Homebrew is *installed* via the flake input nix-homebrew
      enable = true;
      casks = pkgs.callPackage ./casks.nix {};

      # These app IDs are from using the mas CLI app
      # mas = mac app store
      # https://github.com/mas-cli/mas
      #
      # $ nix shell nixpkgs#mas
      # $ mas search <app name>
      #
      masApps = {
        "1password" = 1333542190;
        "hidden-bar" = 1452453066;
        "wireguard" = 1451685025;
      };
    };

    # Enable home-manager
    home-manager = {
      useGlobalPkgs = true;
      users.${user} = { pkgs, config, lib, ... }:{
        home = {
          enableNixpkgsReleaseCheck = false;
          packages = pkgs.callPackage ./packages.nix {};
          file = lib.mkMerge [
            sharedFiles
            additionalFiles
            { "emacs-launcher.command".source = myEmacsLauncher; }
          ];

          stateVersion = "23.11";
        };

        programs = {} // import ../shared/home-manager.nix { inherit config pkgs lib; };

        # Marked broken Oct 20, 2022 check later to remove this
        # https://github.com/nix-community/home-manager/issues/3344
        manual.manpages.enable = false;
      };
    };

    # Fully declarative dock using the latest from Nix Store
    local = {
      dock.enable = true;
      dock.entries = [
        { path = "/Applications/Slack.app/"; }
        { path = "/System/Applications/Messages.app/"; }
        { path = "/System/Applications/Facetime.app/"; }
        { path = "/Applications/Telegram.app/"; }
        { path = "${pkgs.alacritty}/Applications/Alacritty.app/"; }
        { path = "/System/Applications/Music.app/"; }
        { path = "/System/Applications/News.app/"; }
        { path = "/System/Applications/Photos.app/"; }
        { path = "/System/Applications/Photo Booth.app/"; }
        { path = "/System/Applications/TV.app/"; }
        { path = "${pkgs.jetbrains.phpstorm}/Applications/PhpStorm.app/"; }
        { path = "/Applications/TablePlus.app/"; }
        { path = "/Applications/Asana.app/"; }
        { path = "/Applications/Drafts.app/"; }
        { path = "/System/Applications/Home.app/"; }
        { path = "/Applications/iPhone Mirroring.app/"; }
        {
          path = toString myEmacsLauncher;
          section = "others";
        }
        {
          path = "${config.users.users.${user}.home}/.local/share/";
          section = "others";
          options = "--sort name --view grid --display folder";
        }
        {
          path = "${config.users.users.${user}.home}/.local/share/downloads";
          section = "others";
          options = "--sort name --view grid --display stack";
        }
      ];
    };
  }

  git = {
     enable = true;
     ignores = [ "*.swp" ];
     userName = name;
     userEmail = email;
     lfs = {
       enable = true;
     };
     # Single source of truth: ./gitSettings.nix, imported by homePrograms.nix
     # as programs.git.settings. Do not re-declare keys here — this whole block
     # sits inside the reference comment below and is inert, and duplicating
     # settings is what produced the earlier extraConfig breakage.
     settings = import ./gitSettings.nix;
   };

   alacritty = {
     enable = true;
     settings = {
       cursor = {
         style = "Block";
       };

       window = {
         opacity = 1.0;
         padding = {
           x = 24;
           y = 24;
         };
       };

       font = {
         normal = {
           family = "MesloLGS NF";
           style = "Regular";
         };
         size = lib.mkMerge [
           (lib.mkIf pkgs.stdenv.hostPlatform.isLinux 10)
           (lib.mkIf pkgs.stdenv.hostPlatform.isDarwin 14)
         ];
       };

       colors = {
         primary = {
           background = "0x1f2528";
           foreground = "0xc0c5ce";
         };

         normal = {
           black = "0x1f2528";
           red = "0xec5f67";
           green = "0x99c794";
           yellow = "0xfac863";
           blue = "0x6699cc";
           magenta = "0xc594c5";
           cyan = "0x5fb3b3";
           white = "0xc0c5ce";
         };

         bright = {
           black = "0x65737e";
           red = "0xec5f67";
           green = "0x99c794";
           yellow = "0xfac863";
           blue = "0x6699cc";
           magenta = "0xc594c5";
           cyan = "0x5fb3b3";
           white = "0xd8dee9";
         };
       };
     };
   };

   ssh = {
     enable = true;
     includes = [
       (lib.mkIf pkgs.stdenv.hostPlatform.isLinux
         "/home/${user}/.ssh/config_external"
       )
       (lib.mkIf pkgs.stdenv.hostPlatform.isDarwin
         "/Users/${user}/.ssh/config_external"
       )
     ];
     matchBlocks = {
       "github.com" = {
         identitiesOnly = true;
         identityFile = [
           (lib.mkIf pkgs.stdenv.hostPlatform.isLinux
             "/home/${user}/.ssh/id_github"
           )
           (lib.mkIf pkgs.stdenv.hostPlatform.isDarwin
             "/Users/${user}/.ssh/id_github"
           )
         ];
       };
     };
   };

   tmux = {
     enable = true;
     plugins = with pkgs.tmuxPlugins; [
       vim-tmux-navigator
       sensible
       yank
       prefix-highlight
       {
         plugin = power-theme;
         extraConfig = ''
            set -g @tmux_power_theme 'gold'
         '';
       }
       {
         plugin = resurrect; # Used by tmux-continuum

         # Use XDG data directory
         # https://github.com/tmux-plugins/tmux-resurrect/issues/348
         extraConfig = ''
           set -g @resurrect-dir '/Users/dustin/.cache/tmux/resurrect'
           set -g @resurrect-capture-pane-contents 'on'
           set -g @resurrect-pane-contents-area 'visible'
         '';
       }
       {
         plugin = continuum;
         extraConfig = ''
           set -g @continuum-restore 'on'
           set -g @continuum-save-interval '5' # minutes
         '';
       }
     ];
     terminal = "screen-256color";
     prefix = "C-x";
     escapeTime = 10;
     historyLimit = 50000;
     extraConfig = ''
       # Remove Vim mode delays
       set -g focus-events on

       # Enable full mouse support
       set -g mouse on

       # -----------------------------------------------------------------------------
       # Key bindings
       # -----------------------------------------------------------------------------

       # Unbind default keys
       unbind C-b
       unbind '"'
       unbind %

       # Split panes, vertical or horizontal
       bind-key x split-window -v
       bind-key v split-window -h

       # Move around panes with vim-like bindings (h,j,k,l)
       bind-key -n M-k select-pane -U
       bind-key -n M-h select-pane -L
       bind-key -n M-j select-pane -D
       bind-key -n M-l select-pane -R

       # Smart pane switching with awareness of Vim splits.
       # This is copy paste from https://github.com/christoomey/vim-tmux-navigator
       is_vim="ps -o state= -o comm= -t '#{pane_tty}' \
         | grep -iqE '^[^TXZ ]+ +(\\S+\\/)?g?(view|n?vim?x?)(diff)?$'"
       bind-key -n 'C-h' if-shell "$is_vim" 'send-keys C-h'  'select-pane -L'
       bind-key -n 'C-j' if-shell "$is_vim" 'send-keys C-j'  'select-pane -D'
       bind-key -n 'C-k' if-shell "$is_vim" 'send-keys C-k'  'select-pane -U'
       bind-key -n 'C-l' if-shell "$is_vim" 'send-keys C-l'  'select-pane -R'
       tmux_version='$(tmux -V | sed -En "s/^tmux ([0-9]+(.[0-9]+)?).*TODO_REMOVE_THIS_BLOCK/\1/p")'
       if-shell -b '[ "$(echo "$tmux_version < 3.0" | bc)" = 1 ]' \
         "bind-key -n 'C-\\' if-shell \"$is_vim\" 'send-keys C-\\'  'select-pane -l'"
       if-shell -b '[ "$(echo "$tmux_version >= 3.0" | bc)" = 1 ]' \
         "bind-key -n 'C-\\' if-shell \"$is_vim\" 'send-keys C-\\\\'  'select-pane -l'"

       bind-key -T copy-mode-vi 'C-h' select-pane -L
       bind-key -T copy-mode-vi 'C-j' select-pane -D
       bind-key -T copy-mode-vi 'C-k' select-pane -U
       bind-key -T copy-mode-vi 'C-l' select-pane -R
       bind-key -T copy-mode-vi 'C-\' select-pane -l
       '';
     };
*/
