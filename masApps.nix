_: {
  homebrew.masApps = {
    # Mac App Store must be logged in & apps must be purchased first
    "1Password for Safari" = 1569813296;
    # "GarageBand" = 682658836;
    # "iMovie" = 408981434; # removed 2026-08: 3.7 GB, unused
    # "Keynote" = 409183694; # errors during brew bundle
    # "Numbers" = 409203825; # errors during brew bundle
    # "Pages" = 409201541; # errors during brew bundle
    # "Xcode" = 497799835; # NOT here (#1069): brew bundle during activation
    # needs Mac App Store sign-in + a prior "purchase" and a multi-GB
    # download, and a bundle failure can fail the switch. Xcode is still
    # managed -- by simple-rebuild.sh instead: its xcode_mas step runs
    # `mas install` / `mas upgrade` for this id after every switch, then
    # selects it (xcode-select -s, license accept, -runFirstLaunch).
  };
}
