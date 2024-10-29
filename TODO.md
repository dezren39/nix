security.pam.enableSudoTouchIdAuth = true;
system.defaults = {
  dock.autohide = true;
  dock.mru-spaces = false;
  finder.AppleShowAllExtensions = true;
  finder.FXPreferredViewStyle = "clmv";
  loginwindow.LoginwindowText = "nixcademy.com";
  screencapture.location = "~/Pictures/screenshots";
  screensaver.askForPasswordDelay = 10;
};
nix.extraOptions = ''
  extra-platforms = x86_64-darwin aarch64-darwin
'';
nix.linux-builder.enable = true;
{ config, pkgs, lib, ... }:

{
  nixpkgs.config.allowUnfreePredicate = pkg: builtins.elem (lib.getName pkg) [
    "unrar"
  ];

  environment.systemPackages = with pkgs; [
    vim ffmpeg-full coreutils gnugrep gnused gawk htop mtr
    smartmontools neofetch rsync p7zip hugo ncdu
    ipmitool iperf3 wireguard-tools jq p7zip unrar
  ];

  services.nix-daemon.enable = true;
  programs.zsh.enable = true;
  system.stateVersion = 4;
}
  system.defaults = {
    # minimal dock
    dock = {
      autohide = true;
      orientation = "left";
      show-process-indicators = false;
      show-recents = false;
      static-only = true;
    };
    # a finder that tells me what I want to know and lets me work
    finder = {
      AppleShowAllExtensions = true;
      ShowPathbar = true;
      FXEnableExtensionChangeWarning = false;
    };
    # Tab between form controls and F-row that behaves as F1-F12
    NSGlobalDomain = {
      AppleKeyboardUIMode = 3;
      "com.apple.keyboard.fnState" = true;
    };
  };
# I'd rather not have telemetry on my package manager.
environment.variables.HOMEBREW_NO_ANALYTICS = "1";

homebrew = {
  enable = true;

  onActivation = {
    autoUpdate = true;
    cleanup = "zap";
    upgrade = true;
  };

  brews = [
    "coreutils"
    "direnv"
    "fd"
    "gcc"
    "git"
    "grep"
    "ripgrep"
    "trash"
  ];

  # Update these applicatons manually.
  # As brew would update them by unninstalling and installing the newest
  # version, it could lead to data loss.
  casks = [
    "docker"
    "emacs-mac" # Emacs fork with better macOS support
    "firefox"
    "iterm2"
    "monitorcontrol" # Brightness and volume controls for external monitors.
    "ukelele"
    "unnaturalscrollwheels" # Enable natural scrolling in the trackpad but regular scroll on an external mouse
    "utm" # Virtual Machine Manager
    "visual-studio-code"
  ];

  taps = [
    "railwaycat/emacsmacport" # emacs-mac
  ];

  masApps = {
    Tailscale = 1475387142; # App Store URL id
  };
};
# Auto upgrade nix package and the daemon service.
services.nix-daemon.enable = true;

nix = {
  package = pkgs.nix;
  gc.automatic = true;
  optimise.automatic = true;
  settings = {
    auto-optimise-store = true;
    experimental-features = [ "nix-command" "flakes" ];
  };
};
system.keyboard.enableKeyMapping = true;
system.keyboard.remapCapsLockToControl = true;

# Disable press and hold for diacritics.
# I want to be able to press and hold j and k
# in VSCode with vim keys to move around.
system.defaults.NSGlobalDomain.ApplePressAndHoldEnabled = false;

  homebrew = {
    enable = true;
    onActivation = {
      autoUpdate = true;
      upgrade = true;
    };
    brews = [ "qemu" "runit" "gforth" ];
    casks = [ "1password-cli" "docker" "inkscape" "ngrok" "typora" ];
    # masApps = { OneTab = 1540160809; };
  };inputs = {
    nix-homebrew = {
      url = "github:zhaofengli-wip/nix-homebrew";
      inputs.nixpkgs.follows = "nixpkgs-unstable";
    };

    homebrew-core = {
      url = "github:homebrew/homebrew-core";
      flake = false;
    };
    homebrew-cask = {
      url = "github:homebrew/homebrew-cask";
      flake = false;
    };
    homebrew-bundle = {
      url = "github:homebrew/homebrew-bundle";
      flake = false;
    };


https://github.com/jwiegley/nix-config/blob/master/config/darwin.nix
https://github.com/dustinlyons/nixos-config


Mas apps?

Macports?
Homebred
