{ inputs, system }:
{
  programs = {
    # pwsh.enable = true;
    # osh.enable = true;
    # ysh.enable = true;
    # ghostty = {
    #   enable = true;
    #   # package = inputs.nixpkgs.legacyPackages.${system}.ghostty;
    #   # package = inputs.nur.legacyPackages."${system}".repos.DimitarNestorov.ghostty;
    #   settings = {
    #     # ghostty +list-themes
    #     theme = "synthwave";
    #     # window-decoration = false;
    #     # TODO: hide tabs or make smaller or both
    #   };
    # };
    vscode = {
      enable = true;
    };
    bash = {
      enable = true;
      initExtra = ''
        ff() {
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
        }
        # lootbox: ensure deno and lootbox are on PATH
        export PATH="$HOME/.deno/bin:$PATH"
      '';
    };
    zsh = {
      # https://github.com/dustinlyons/nixos-config/blob/main/modules/shared/config/p10k.zsh
      enable = true;
      initContent = ''
        ff() {
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
        }
        # lootbox: ensure deno and lootbox are on PATH
        export PATH="$HOME/.deno/bin:$PATH"
      '';
    };
    fish = {
      enable = true;
      functions = {
        ff = ''
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
        '';
      };
      shellInit = ''
        # lootbox: ensure deno and lootbox are on PATH
        fish_add_path $HOME/.deno/bin
      '';
    };
    direnv = {
      enable = true;
      nix-direnv.enable = true;
    };
    git = {
      enable = true;
      settings = {
        user = {
          name = "Drewry Pope";
          email = "drewry.pope@vertexinc.com"; # TODO: move work email out of default
        };
        extraConfig = {
          init.defaultBranch = "main";
          pull.rebase = true;
          core = {
            editor = "code-insiders";
            autocrlf = "input";
            bigFileThreshold = "50m";
          };
          safe.directory = "*";
        };
      };
      ignores = [
        ".DS_Store"
        "*.swp"
        ".direnv"
        # codedb snapshots — never commit anywhere
        "codedb.snapshot"
        # lootbox ephemeral dirs — committed scripts (.lootbox/scripts/) are fine
        ".lootbox/cache/"
        ".lootbox/tmp/"
      ];
    };
  };
}
