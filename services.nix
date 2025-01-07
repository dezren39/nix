{pkgs,...}: {
    services = {
      nix-daemon.enable = true;
      jankyborders = {
        enable = true;
        # hidpi = true;
        # width = 1.0;
        # style = "square";
        # order = "above";
      };
      # aerospace = {
      #   enable = false;
      #   settings = builtins.fromTOML (builtins.readFile ./.aerospace.toml);
      #   package = pkgs.aerospace;
      # };
      yabai = {
        enable = true;
        package = pkgs.yabai;
        enableScriptingAddition = true;
      };
      sketchybar = {
        enable = true;
        config = ''
          sketchybar --bar height=24
          sketchybar --update
          echo "sketchybar configuration loaded.."
        '';
      };
      # chunkwm.enable = true;
      # autossh
      # eternalterminal
      # https://github.com/omerxx/dotfiles
    };
}
