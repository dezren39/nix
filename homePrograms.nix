{ inputs, system }:
{
  programs = {
    # pwsh.enable = true;
    # osh.enable = true;
    # ysh.enable = true;
    # ghostty: terminal emulator — installed via brew cask, config managed by home-manager
    # NOTE: ghostty flake (github:ghostty-org/ghostty) does NOT build on darwin (missing Swift 6 / xcodebuild in nix)
    ghostty = {
      enable = true;
      package = null; # installed via homebrew cask — don't build the nix package (broken on darwin: wuffs + gtk4-layer-shell)
      # installVimSyntax = true; # requires nix package — install vim syntax manually if needed
      settings = {
        # ghostty +list-themes
        theme = "synthwave";
        font-size = 14;
        window-padding-x = 8;
        window-padding-y = 8;
        copy-on-select = "clipboard";
        confirm-close-surface = false;
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
    # atuin: SQLite-backed shell history with fuzzy search, cross-shell, exit code/duration tracking
    atuin = {
      enable = true;
      enableZshIntegration = true;
      enableBashIntegration = true;
      enableFishIntegration = true;
      settings = {
        auto_sync = false;
        search_mode = "fuzzy";
        filter_mode = "global";
        style = "compact";
        show_preview = true;
        show_help = true;
        history_filter = [
          "^ls"
          "^cd"
          "^pwd"
          "^exit"
          "^clear"
        ];
      };
    };
    # starship: cross-shell prompt — git status, language versions, nix shell indicator, cmd duration
    starship = {
      enable = true;
      enableZshIntegration = true;
      enableBashIntegration = true;
      enableFishIntegration = true;
      settings = {
        add_newline = false;
        character = {
          success_symbol = "[❯](bold green)";
          error_symbol = "[❯](bold red)";
        };
        git_status.disabled = false;
        nix_shell = {
          symbol = " ";
          format = "via [$symbol$state( \\($name\\))]($style) ";
        };
        directory.truncation_length = 3;
        cmd_duration = {
          min_time = 2000;
          format = "took [$duration]($style) ";
        };
      };
    };
    # fastfetch: fast system info display (neofetch replacement)
    fastfetch = {
      enable = true;
    };
    # fzf: fuzzy finder with shell integration (file finder, directory jumper, Ctrl+T/Alt+C)
    fzf = {
      enable = true;
      enableZshIntegration = true;
      enableBashIntegration = true;
      enableFishIntegration = true;
      defaultOptions = [
        "--height 40%"
        "--border"
        "--reverse"
      ];
    };
    direnv = {
      enable = true;
      nix-direnv.enable = true;
    };
    git = {
      enable = true;
      signing.format = null; # silence home-manager 25.05 deprecation warning (was defaulting to "openpgp")
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
