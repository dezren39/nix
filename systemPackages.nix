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
      cargo-cache # `cargo cache -r all` in ./clean; ~/.cargo has no built-in pruner
      duti # sets LaunchServices URL/UTI handlers; used to pin the default browser
      doggo
      docker-client
      uv
      ruff
      docker-compose
      inputs.nixpkgs-terraform.packages.${system}."terraform-1.5.7"
      # possibly not darwin
      powershell
      oils-for-unix
      # zen-browser
      _1password-cli
      act
      # github-copilot-cli # Gen-1 npm CLI deprecated; replaced by native `copilot-cli` cask
      # copilot-cli
      age
      # dbeaver-bin # removed 2026-09-15: reclaiming disk (use DBeaver.app)
      yq-go
      # nixd # 2025-10-15 fixed in staging
      age-plugin-yubikey
      alacritty
      # ansible # DISABLED 2026-08: ~2.1 GB closure, ~640 MB of it dependencies
      # almost nothing else uses. Re-enable if you need it.
      # arduino-cli # removed 2026-09-15: reclaiming disk (embedded; platformio also removed)
      # terraform
      powershell
      aspell
      aspellDicts.en
      nixfmt
      # awscli # removed 2026-09-15: reclaiming disk; use AWS SDK / CloudShell
      azure-cli
      direnv
      nix-direnv
      jujutsu
      lazyjj
      bandwhich
      bash-completion
      bat
      # bitwarden-cli
      # whalebrew?
      # kitty # removed 2026-09-15: reclaiming disk
      zellij
      tmux
      jankyborders
      sketchybar
      # sketchybar-app-font
      screen
      black
      btop
      # (buf.overrideAttrs { doCheck = false; }) # removed 2026-09-15: reclaiming disk (WASM plugin timeout in TestRunBreakingPolicyLocal under Nix sandbox)
      stow
      caddy
      certstrap
      # cfssl # removed 2026-09-15: reclaiming disk
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
      # dive # removed 2026-09-15: reclaiming disk
      dust
      # emacs
      # emacs-unstable
      # emacs-all-the-icons-fonts
      # fastlane # removed 2026-09-15: reclaiming disk (iOS/Android release automation)
      fd
      ffmpeg
      # flyctl # removed 2026-09-15: reclaiming disk
      # font-awesome
      fzf
      gcc
      gh
      git
      git-filter-repo
      git-lfs
      glow
      gnupg
      gnused
      gnumake
      go
      # golangci # ???
      # gomplate # removed 2026-09-15: reclaiming disk
      # google-cloud-sdk # removed 2026-09-15: reclaiming disk
      gopls
      # goreleaser # removed 2026-09-15: reclaiming disk
      graphviz
      gum
      # hack-font
      # hcloud # removed 2026-09-15: reclaiming disk
      htop
      httpie
      # hugo # removed 2026-09-15: reclaiming disk
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
      # k3d # removed 2026-09-15: reclaiming disk
      # k9s # removed 2026-09-15: reclaiming disk (helm-unittest/k3d cover local testing)
      # killall # removed 2026-09-15: reclaiming disk (macOS /usr/bin/killall remains)
      # argo-workflows # removed 2026-09-15: reclaiming disk (Argo Workflows engine, NOT Argo CD)
      kubernetes-helm
      kubernetes-helmPlugins.helm-unittest
      kubectl
      libfido2
      lima
      lnav
      mas
      meslo-lgs-nf
      micro
      # mitmproxy # broken on master 2026-02-26
      # mutagen # removed 2026-09-15: reclaiming disk
      # mutagen-compose # removed 2026-09-15: reclaiming disk
      # nats-server # removed 2026-09-15: reclaiming disk
      # natscli # removed 2026-09-15: reclaiming disk
      #ncdu
      # fastfetch # removed 2026-09-15: reclaiming disk (also disabled programs.fastfetch below)
      neovim
      # ngrok # removed 2026-09-15: reclaiming disk
      nmap
      bun
      nodejs
      pnpm
      # noto-fonts
      # noto-fonts-emoji
      #openfortivpn
      # OpenCode v1 and v2 CLIs — defined in flake.nix packages output
      # Patches: PR #11197, #18879, #20758, #20848
      inputs.self.packages.${system}.opencode
      inputs.self.packages.${system}.opencode2
      # inputs.self.packages.${system}.opencode-desktop # disabled: upstream build broken
      inputs.self.packages.${system}.lootbox-link
      inputs.self.packages.${system}.opencode-share
      inputs.self.packages.${system}.sidepulse
      inputs.self.packages.${system}.sidepulse-setup
      # openjdk # removed 2026-09-15: reclaiming disk (Azul Zulu JDK)
      openssh
      p7zip
      # pandoc # removed 2026-09-15: reclaiming disk
      pgcli
      # php81
      # platformio # removed 2026-09-15: reclaiming disk
      playwright-driver.browsers
      # playwright-test  # depends on playwright-driver
      pngquant
      protobuf
      protoc-gen-go
      protoc-gen-go-grpc
      python3
      bash
      # python39
      # python39Packages.virtualenv
      # redis # removed 2026-09-15: reclaiming disk
      ripgrep
      treefmt
      shfmt
      yamlfmt
      taplo
      prettier
      shellcheck
      # hadolint # removed 2026-09-15: reclaiming disk (Dockerfile linter)
      dockerfmt
      # slack
      sops
      sqlite
      ssh-to-age
      ssm-session-manager-plugin
      # symfony-cli # removed 2026-09-15: reclaiming disk (PHP tooling; php already commented)
      tflint
      tmux
      tree
      # trivy # removed 2026-09-15: reclaiming disk
      unrar
      unzip
      upx
      # vector
      vim
      # watchman
      wget
      yarn
      zip
      zstd
      # zsh-powerlevel10k — replaced by programs.starship (cross-shell, Rust, faster)
      # Rust toolchain via fenix (replaces bare cargo) — stable with all dev components
      # Rust toolchain removed 2026-09-15: reclaiming disk (~880 MB). Re-enable if you need cargo/rustc/rust-analyzer.
      # (pkgs.fenix.stable.withComponents [
      #   "cargo"
      #   "clippy"
      #   "rustc"
      #   "rustfmt"
      #   "rust-src" # needed by rust-analyzer for goto-definition into std
      #   "rust-analyzer" # LSP server
      #   "llvm-tools" # code coverage, llvm-profdata, llvm-cov
      # ])
      # Nightly-only: miri (undefined behavior detector) — use fenix.complete for nightly components
      # pkgs.fenix.complete.miri
      # Extra cargo tools removed 2026-09-15: orphaned after Rust toolchain removal (need cargo/rustc to function)
      # cargo-watch # auto-rebuild on file changes
      # cargo-expand # macro expansion viewer
      # cargo-edit # `cargo add`, `cargo rm`, `cargo upgrade`
      # cargo-nextest # faster test runner
      # cargo-udeps # find unused dependencies
      # cargo-deny # lint deps for licenses, bans, advisories
      # cargo-flamegraph # profiling flamegraphs
      # cargo-audit # security vulnerability scanner
      # cargo-insta # snapshot testing
      # zig # removed 2026-09-15: reclaiming disk
      # swiftPackages.swift # removed 2026-09-15: reclaiming disk (~880 MB; drops swiftc, nothing else depends on it)
      # dotnet-sdk_9 # DISABLED 2026-08: ~1.2 GB closure, ~590 MB of near-private
      # dependencies. Re-enable if you need .NET.
      inputs.just.packages.${system}.default
      # linker/binary diagnostic and fixing tools
      patchelf # fix ELF binaries (rpath, interpreter) - essential for Linux containers/cross-compile
      binutils # readelf, objdump, nm, strings - inspect binaries
      file # identify binary types (ELF vs Mach-O, architecture)
      darwin.trash # `trash` command — moves files to macOS Trash instead of permanent deletion
      inputs.self.packages.${system}.brew-repair
      inputs.self.packages.${system}.codedb
      pkgs.codebase-memory-mcp
      pkgs.fff-mcp
      inputs.self.packages.${system}.lootbox-update
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
