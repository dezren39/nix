_: {
  programs = {
    bash = {
      enable = true;
      interactiveShellInit = ''
        ff() {
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "aerospace focus --window-id {1}")+abort'
        }
      '';
    };

    zsh = {
      enable = true;
      interactiveShellInit = ''
        ff() {
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "aerospace focus --window-id {1}")+abort'
        }
      '';
    };

    fish = {
      enable = true;
      interactiveShellInit = ''
        function ff
          aerospace list-windows --all | fzf --bind 'enter:execute(bash -c "aerospace focus --window-id {1}")+abort'
        end
      '';
    };
      # pwsh.enable = true;
      # osh.enable = true;
      # ysh.enable = true;
    };
    home-manager.enable = true;
    git = {
      enable = true;
      config = {
        "safe.directory" = "*";
        "core.bigFileThreshold" = "50m";
      };
    };
  };
}
