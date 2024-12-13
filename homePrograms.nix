_: {
  programs = {
    bash = {
      enable = true;
      initExtra = ''
        ff() {
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
        }
      '';
    };

    zsh = { # https://github.com/dustinlyons/nixos-config/blob/main/modules/shared/config/p10k.zsh
      enable = true;
      initExtra = ''
        ff() {
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
        }
      '';
    };

    fish = {
      enable = true;
      functions = {
        ff = ''
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
        '';
      };
    };
    # pwsh.enable = true;
    # osh.enable = true;
    # ysh.enable = true;
    git = {
      enable = true;
      userName = "Drewry Pope";
      userEmail = "drewry.pope@vertexinc.com"; # TODO: move work email out of default
      extraConfig = {
        init.defaultBranch = "main";
        pull.rebase = true;
        core = {
          editor = "zed";
          autocrlf = "input";
        };
      };
      ignores = [
        ".DS_Store"
        "*.swp"
        ".direnv"
      ];
    };
  };
}
