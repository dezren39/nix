{
  inputs,
  system,
  pkgs,
  ...
}:
let
  homeDir = "/Users/drewry.pope";

  sharedShellAliases = {
    rm = "trash";
    ll = "ls -lah --group-directories-first --color=auto";
  };

  # Shared POSIX shell init — sourced by bash, zsh, and fish (fish tolerates POSIX via implicit translation)
  sharedShellInit = ''
    dd-creds() { sudo -v && source ~/Documents/dd-creds.sh && echo "DD_APP_KEY and DD_API_KEY exported"; }
    gh-token() { sudo -v && source ~/Documents/gh-token.sh && echo "GH_TOKEN exported"; }

    ff() {
      aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "setsid sh -c \"aerospace focus --window-id {1}\" >/dev/null 2>&1 < /dev/null &")+abort'
    }

    # Tool install bin directories — ensure globally-installed binaries are on PATH
    export PATH="$HOME/bin:$PATH"              # personal scripts
    export PATH="$HOME/.deno/bin:$PATH"       # deno install
    export PATH="${homeDir}/.local/bin:$PATH"  # uv tool install, pip install --user
    export PATH="$HOME/.bun/bin:$PATH"        # bun install -g
    export PATH="$HOME/.cargo/bin:$PATH"      # cargo install
    export PATH="$HOME/go/bin:$PATH"          # go install
    export PATH="$HOME/.npm-global/bin:$PATH" # npm install -g (prefix set via npmrc)

    # ez-stack
    export PATH="${homeDir}/.local/share/uv/tools/ez-stack/lib/python3.12/site-packages/ez_stack/bin:$PATH"
    eval "$(ez shell-init)"

    # setup-opencode: link the central .opencode directory into the current project
    setup-opencode() {
      # Resolve the central .opencode dir (always use drewry.pope's, even as root)
      local central
      central="$(realpath ${homeDir}/.config/nix/.opencode 2>/dev/null)"
      if [ -z "$central" ] || [ ! -d "$central" ]; then
        echo "ERROR: Central .opencode not found at ${homeDir}/.config/nix/.opencode" >&2
        return 1
      fi

      # Trash dir (always drewry.pope's ~/git/.trash, even as root)
      local trash_dir="${homeDir}/git/.trash"
      mkdir -p "$trash_dir"

      local target=".opencode"

      # Case 1: Already a symlink
      # FUTURE IMPROVEMENT: Could handle migration — if .opencode is a symlink to
      # a different .opencode directory (not central), we could:
      #   1. Merge files from the symlink target into central
      #   2. Replace the symlink at the old target location to point to central
      #   3. Then re-link .opencode here to central
      # CAUTION: Must compare realpath of both the symlink target and central,
      # since the symlink may use a different path (e.g. ../foo, ~/foo, or an
      # intermediate symlink) but still resolve to the same final directory.
      if [ -L "$target" ]; then
        local link_dest
        link_dest="$(realpath "$target" 2>/dev/null)"
        if [ "$link_dest" = "$central" ]; then
          echo "✓ .opencode already linked to $central"
          return 0
        else
          echo "WARNING: .opencode is a symlink to $link_dest (expected $central)" >&2
          echo "  Remove it manually if you want to re-link: rm .opencode" >&2
          return 1
        fi
      fi

      # Case 2: Existing directory — merge new files, report conflicts, then trash
      if [ -d "$target" ]; then
        echo "Merging new files from local .opencode into central..."
        local had_conflicts=0
        while IFS= read -r rel; do
          if [ -e "$central/$rel" ]; then
            if ! diff -q "$target/$rel" "$central/$rel" >/dev/null 2>&1; then
              echo "  CONFLICT (kept central): $rel"
              had_conflicts=1
            fi
          else
            mkdir -p "$central/$(dirname "$rel")"
            cp -a "$target/$rel" "$central/$rel"
            echo "  COPIED: $rel"
          fi
        done < <(cd "$target" && find . -type f | sed 's|^\./||')

        # Trash the local directory with RFC 3339 timestamp (colons replaced with dashes)
        local stamp
        stamp="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
        local base
        base="$(basename "$(pwd)")"
        local trash_dest="$trash_dir/.opencode-''${base}-''${stamp}"
        mv "$target" "$trash_dest"
        echo "Trashed local .opencode → $trash_dest"
        echo ""
        echo "  Files moved to: $trash_dest"
        echo ""

        if [ "$had_conflicts" -eq 1 ]; then
          echo "Some files had conflicts (central version was kept)."
          echo "You can examine the previous .opencode directory to compare:"
          echo ""
          echo "  cd $trash_dest"
          echo ""
        fi
      fi

      # Case 3 (or after Case 2 cleanup): Create symlink
      ln -s "$central" "$target"
      echo "✓ Linked .opencode → $central"
    }
  '';
in
{
  programs = {
    # pwsh.enable = true;
    # osh.enable = true;
    # ysh.enable = true;
    # ghostty: terminal emulator — use ghostty-bin (pre-built binary) on darwin
    # NOTE: ghostty flake (github:ghostty-org/ghostty) does NOT build on darwin (missing Swift 6 / xcodebuild in nix)
    # NOTE: ghostty (source build) fails on darwin (wuffs + gtk4-layer-shell). ghostty-bin avoids this.
    ghostty = {
      enable = true;
      package = pkgs.ghostty-bin; # pre-built binary — avoids broken source build on darwin
      installVimSyntax = true;
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
      shellAliases = sharedShellAliases;
      initExtra = ''
        ${sharedShellInit}

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
      shellAliases = sharedShellAliases;
      initContent = ''
        ${sharedShellInit}

        # opencode: shell completions (yargs-based)
        eval "$(opencode completion 2>/dev/null)"
      '';
    };
    fish = {
      enable = true;
      shellAbbrs = sharedShellAliases;
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
        ${sharedShellInit}

        # opencode: register completions
        complete -c opencode -f -a '(__fish_opencode_completions)'
      '';
    };
    # atuin: SQLite-backed shell history — Ctrl+R only (up arrow = normal shell behavior)
    atuin = {
      enable = true;
      enableZshIntegration = true;
      enableBashIntegration = true;
      enableFishIntegration = true;
      flags = [
        "--disable-up-arrow" # up arrow = normal previous command, Ctrl+R = atuin search
      ];
      settings = {
        auto_sync = false;
        update_check = false;
        search_mode = "fuzzy";
        filter_mode = "global"; # default: search everything. Ctrl+R cycles through filters
        workspaces = true; # enable "workspace" filter — scopes to current git repo
        style = "compact";
        inline_height = 20; # smaller inline UI instead of fullscreen
        show_preview = true;
        show_help = true;
        enter_accept = false; # tab-like: lets you edit before running
        store_failed = true; # keep commands that failed too
        secrets_filter = true; # auto-hide AWS keys, tokens, etc.
        history_filter = [
          "^ls"
          "^cd"
          "^pwd"
          "^exit"
          "^clear"
        ];
        # filter modes available when cycling with Ctrl+R during search:
        # global → host → session → directory → workspace → global ...
        search.filters = [
          "global"
          "host"
          "session"
          "directory"
          "workspace"
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
      config = {
        global = {
          hide_env_diff = true;
          warn_timeout = "30s";
          load_dotenv = true;
        };
        whitelist = {
          prefix = [ "${homeDir}" ];
        };
      };
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
          # pull.rebase = true; # commented out — prefer merge default
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
