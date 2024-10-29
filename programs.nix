_: {
  programs = {
    zsh.enable = true; # https://github.com/dustinlyons/nixos-config/blob/main/modules/shared/config/p10k.zsh
    fish.enable = true;
    bash.enable = true;
    # pwsh.enable = true;
    # osh.enable = true;
    # ysh.enable = true;
    git = {
      enable = true;
      config = {
        "safe.directory" = "*";
        "core.bigFileThreshold" = "50m";
      };
    };
  };
}
