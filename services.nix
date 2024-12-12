_: {
    services = {
      nix-daemon.enable = true;
      jankyborders = {
        enable = true;
        # hidpi = true;
        # width = 1.0;
        # style = "square";
        # order = "above";
      };
      # aerospace.enable = true; # TODO: switch to this
      sketchybar = {
        enable = true;
        config = ''
          sketchybar --bar height=24
          sketchybar --update
          echo "sketchybar configuration loaded.."
        '';
      };
      # yabai.enable = true;
      # chunkwm.enable = true;
      # autossh
      # eternalterminal
      # https://github.com/omerxx/dotfiles
    };
}
