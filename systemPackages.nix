{ pkgs, ... } : {
  environment.systemPackages = with pkgs; [
    powershell
    oils-for-unix
    # zen-browser
  ] ++ [ # possibly not darwin
    _1password-cli
    act
    age
    age-plugin-yubikey
    alacritty
    ansible
    arduino-cli
    aspell
    aspellDicts.en
    awscli
    jujutsu
    lazyjj
    bandwhich
    bash-completion
    bat
    # bitwarden-cli
    # whalebrew?
    zellij
    tmux
    screen
    black
    btop
    buf
    caddy
    certstrap
    cfssl
    cocoapods
    coreutils
    curl
    dbmate
    dejavu_fonts
    deno
    devenv
    difftastic
    dive
    du-dust
    emacs
    # emacs-unstable
    emacs-all-the-icons-fonts
    fastlane
    fd
    ffmpeg
    flyctl
    font-awesome
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
    hack-font
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
    jetbrains.phpstorm
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
    noto-fonts
    noto-fonts-emoji
    #openfortivpn
    openjdk
    openssh
    p7zip
    pandoc
    pgcli
    php81
    platformio
    pngquant
    protobuf
    protoc-gen-go
    protoc-gen-go-grpc
    python39
    # python39Packages.virtualenv
    redis
    ripgrep
    slack
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
  ] ++
  [ # darwin
    aerospace
    dockutil
    fswatch
    # rectangle
  ];
}
