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

        # opencode: shell completions (yargs-based)
        _opencode_yargs_completions() {
          local cur_word args type_list
          cur_word="''${COMP_WORDS[COMP_CWORD]}"
          args=("''${COMP_WORDS[@]}")
          type_list=$(opencode --get-yargs-completions "''${args[@]}" 2>/dev/null)
          COMPREPLY=($(compgen -W "''${type_list}" -- "''${cur_word}"))
          return 0
        }
        complete -o bashdefault -o default -F _opencode_yargs_completions opencode
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

        # opencode: shell completions (yargs-based)
        eval "$(opencode completion 2>/dev/null)"
      '';
    };
    fish = {
      enable = true;
      functions = {
        ff = ''
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
        '';
        # opencode: completion helper (yargs-based)
        __fish_opencode_completions = ''
          set -l tokens (commandline -opc)
          set -l current (commandline -ct)
          set -lx COMP_LINE (commandline -p)
          set -lx COMP_POINT (commandline -C)
          set -lx COMP_CWORD (math (count $tokens) - 1)
          opencode --get-yargs-completions $tokens $current 2>/dev/null
        '';
      };
      shellInit = ''
        # lootbox: ensure deno and lootbox are on PATH
        fish_add_path $HOME/.deno/bin

        # opencode: register completions
        complete -c opencode -f -a '(__fish_opencode_completions)'
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
