{
  inputs,
  system,
  pkgs,
  ...
}:
{
  environment.systemPackages =
    with pkgs;
    [
      colima
      doggo
      docker
      uv
      docker-compose
      inputs.nixpkgs-terraform.packages.${system}."terraform-1.5.7"
      # possibly not darwin
      powershell
      oils-for-unix
      # zen-browser
      _1password-cli
      act
      github-copilot-cli
      copilot-cli
      age
      dbeaver-bin
      yq-go
      # nixd # 2025-10-15 fixed in staging
      age-plugin-yubikey
      alacritty
      ansible
      arduino-cli
      # terraform
      powershell
      aspell
      aspellDicts.en
      nixfmt
      awscli
      direnv
      jujutsu
      lazyjj
      bandwhich
      bash-completion
      bat
      # bitwarden-cli
      # whalebrew?
      kitty
      zellij
      tmux
      jankyborders
      sketchybar
      # sketchybar-app-font
      screen
      black
      btop
      buf
      stow
      caddy
      certstrap
      cfssl
      chromium
      cocoapods
      coreutils
      curl
      dbmate
      # dejavu_fonts
      deno
      # devenv
      difftastic
      dive
      dust
      # emacs
      # emacs-unstable
      # emacs-all-the-icons-fonts
      fastlane
      fd
      ffmpeg
      flyctl
      # font-awesome
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
      # hack-font
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
      # jetbrains.phpstorm
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
      # mitmproxy # broken on master 2026-02-26
      mutagen
      mutagen-compose
      nats-server
      natscli
      #ncdu
      fastfetch
      neovim
      ngrok
      nmap
      nodejs
      # noto-fonts
      # noto-fonts-emoji
      #openfortivpn
      inputs.opencode.packages.${system}.opencode
      # Fix opencode-desktop: upstream flake is missing outputHashes for git dependencies
      # as-of: 2026-03-27
      # ref: https://github.com/anomalyco/opencode/issues/18273
      # ref: https://github.com/Vishal2002/opencode/tree/fix/auth-to-body-provider-dialogs
      # NOTE: auth->body sed patches removed; SDK types expect `auth` and tsgo -b fails with `body`
      (inputs.opencode.packages.${system}.desktop.overrideAttrs (old: {
        cargoDeps = pkgs.rustPlatform.importCargoLock {
          lockFile = inputs.opencode + "/packages/desktop/src-tauri/Cargo.lock";
          outputHashes = {
            "specta-2.0.0-rc.22" = "sha256-YsyOAnXELLKzhNlJ35dHA6KGbs0wTAX/nlQoW8wWyJQ=";
            "tauri-2.9.5" = "sha256-dv5E/+A49ZBvnUQUkCGGJ21iHrVvrhHKNcpUctivJ8M=";
            "tauri-specta-2.0.0-rc.21" = "sha256-n2VJ+B1nVrh6zQoZyfMoctqP+Csh7eVHRXwUQuiQjaQ=";
          };
        };
      }))
      openjdk
      openssh
      p7zip
      pandoc
      pgcli
      # php81
      platformio
      playwright-driver
      playwright-test
      pngquant
      protobuf
      protoc-gen-go
      protoc-gen-go-grpc
      python3
      # python39
      # python39Packages.virtualenv
      redis
      ripgrep
      # slack
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
      dotnet-sdk_9
      just
    ]
    ++ [
      # darwin
      aerospace
      dockutil
      fswatch
      noTunes
      # rectangle
    ];
}
