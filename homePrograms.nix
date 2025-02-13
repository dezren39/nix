{ inputs, system }:
{
  programs = {
    # pwsh.enable = true;
    # osh.enable = true;
    # ysh.enable = true;
    ghostty = {
      enable = true;
      # package = inputs.nixpkgs-master.legacyPackages.${system}.ghostty;
      package = inputs.nur.legacyPackages."${system}".repos.DimitarNestorov.ghostty;
      settings = {
        # ghostty +list-themes
        theme = "synthwave";
        # window-decoration = false;
        # TODO: hide tabs or make smaller or both
      };
    };
    vscode = {
      enable = true;
    };
    bash = {
      enable = true;
      initExtra = ''
        ff() {
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
        }
      '';
    };
    zsh = {
      # https://github.com/dustinlyons/nixos-config/blob/main/modules/shared/config/p10k.zsh
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
          bigFileThreshold = "50m";
        };
        safe.directory = "*";
      };
      ignores = [
        ".DS_Store"
        "*.swp"
        ".direnv"
      ];
    };
  };
}
