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
      (buf.overrideAttrs { doCheck = false; }) # WASM plugin timeout in TestRunBreakingPolicyLocal under Nix sandbox
      stow
      caddy
      certstrap
      cfssl
      # chromium # driverLink not supported on darwin - 2026-03-29
      # helium
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
      bun
      nodejs
      pnpm
      # noto-fonts
      # noto-fonts-emoji
      #openfortivpn
      # Patched opencode CLI and desktop — defined in flake.nix packages output
      # Patches: PR #11197, #18879, #20758, #20848
      inputs.self.packages.${system}.opencode
      inputs.self.packages.${system}.opencode-desktop
      inputs.self.packages.${system}.opencode-share
      openjdk
      openssh
      p7zip
      pandoc
      pgcli
      # php81
      platformio
      # playwright-driver # driverLink not supported on darwin
      # playwright-test  # depends on playwright-driver
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
      cargo
      dotnet-sdk_9
      inputs.just.packages.${system}.default
      # linker/binary diagnostic and fixing tools
      patchelf # fix ELF binaries (rpath, interpreter) - essential for Linux containers/cross-compile
      binutils # readelf, objdump, nm, strings - inspect binaries
      file # identify binary types (ELF vs Mach-O, architecture)
    ]
    ++ [
      # darwin
      cctools # install_name_tool, otool, lipo - inspect/fix Mach-O binaries
      aerospace
      dockutil
      fswatch
      noTunes
      # rectangle
    ];
}
