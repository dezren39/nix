# home.nix

{ config, pkgs, ... }:

{
  # Home Manager needs a bit of information about you and the paths it should
  # manage.

  # This value determines the Home Manager release that your configuration is
  # compatible with. This helps avoid breakage when a new Home Manager release
  # introduces backwards incompatible changes.
  #
  # You should not change this value, even if you update Home Manager. If you do
  # want to update the value, then make sure to first check the Home Manager
  # release notes.
  home.stateVersion = "23.05"; # Please read the comment before changing.

  # The home.packages option allows you to install Nix packages into your
  # environment.

  # Home Manager is pretty good at managing dotfiles. The primary way to manage
  # plain files is through 'home.file'.
  home.file = {
    # # Building this configuration will create a copy of 'dotfiles/screenrc' in
    # # the Nix store. Activating the configuration will then make '~/.screenrc' a
    # # symlink to the Nix store copy.
    # ".screenrc".source = dotfiles/screenrc;

    # # You can also set the file content immediately.
    # ".gradle/gradle.properties".text = ''
    #   org.gradle.console=verbose
    #   org.gradle.daemon.idletimeout=3600000
    # '';
  };

  # You can also manage environment variables but you will have to manually
  # source
  #
  #  ~/.nix-profile/etc/profile.d/hm-session-vars.sh
  #
  # or
  #
  #  /etc/profiles/per-user/davish/etc/profile.d/hm-session-vars.sh
  #
  # if you don't want to manage your shell through Home Manager.
  home.sessionVariables = {
    # EDITOR = "emacs";
  };
}


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
   extraConfig = {
     init.defaultBranch = "main";
     core = {
	    editor = "vim";
       autocrlf = "input";
     };
     commit.gpgsign = true;
     pull.rebase = true;
     rebase.autoStash = true;
   };
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
