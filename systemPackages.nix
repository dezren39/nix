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
      docker
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
      emacs
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
      mitmproxy
      mutagen
      mutagen-compose
      nats-server
      natscli
      #ncdu
      neofetch
      neovim
      ngrok
      nmap
      nodejs
      # noto-fonts
      # noto-fonts-emoji
      #openfortivpn
      opencode
      openjdk
      openssh
      p7zip
      pandoc
      pgcli
      # php81
      platformio
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
    ]
    ++ [
      # darwin
      aerospace
      dockutil
      fswatch
      # rectangle
    ];
}
