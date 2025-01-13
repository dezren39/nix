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
      # karabiner-elements = {
      #   enable = true;
      #   package = pkgs.karabiner-elements;
      # };
      # chunkwm
      skhd = {
        enable = true;
        package = pkgs.skhd;

        skhdConfig = ''
      #     # Terminal (default: SUPER + Return)
      #     cmd - return : kitty

      #     # Window Focus
      #     alt - h : yabai -m window --focus west
      #     alt - j : yabai -m window --focus south
      #     alt - k : yabai -m window --focus north
      #     alt - l : yabai -m window --focus eastj

      #     # Window Movement (like Hyprland's SUPER + SHIFT + arrow)
      #     shift + alt - h : yabai -m window --swap west
      #     shift + alt - j : yabai -m window --swap south
      #     shift + alt - k : yabai -m window --swap north
      #     shift + alt - l : yabai -m window --swap east

      #     # Resize windows (like Hyprland's SUPER + RIGHT MOUSE)
      #     ctrl + alt - h : yabai -m window --resize left:-50:0; \
      #                     yabai -m window --resize right:-50:0
      #     ctrl + alt - j : yabai -m window --resize bottom:0:50; \
      #                     yabai -m window --resize top:0:50
      #     ctrl + alt - k : yabai -m window --resize top:0:-50; \
      #                     yabai -m window --resize bottom:0:-50
      #     ctrl + alt - l : yabai -m window --resize right:50:0; \
      #                     yabai -m window --resize left:50:0

      #     # Toggle floating (like Hyprland's SUPER + SPACE)
      #     alt - space : yabai -m window --toggle float; \
      #                  yabai -m window --grid 4:4:1:1:2:2

      #     # Toggle fullscreen (like Hyprland's SUPER + F)
      #     alt - f : yabai -m window --toggle zoom-fullscreen

      #     # Workspaces (like Hyprland's SUPER + [1-9])
      #     cmd - 1 : yabai -m space --focus 1
      #     cmd - 2 : yabai -m space --focus 2
      #     cmd - 3 : yabai -m space --focus 3
      #     cmd - 4 : yabai -m space --focus 4
      #     cmd - 5 : yabai -m space --focus 5
      #     cmd - 6 : yabai -m space --focus 6
      #     cmd - 7 : yabai -m space --focus 7
      #     cmd - 8 : yabai -m space --focus 8
      #     cmd - 9 : yabai -m space --focus 9

      #     # Move window to workspace (like Hyprland's SUPER + SHIFT + [1-9])
      #     shift + cmd - 1 : yabai -m window --space 1
      #     shift + cmd - 2 : yabai -m window --space 2
      #     shift + cmd - 3 : yabai -m window --space 3
      #     shift + cmd - 4 : yabai -m window --space 4
      #     shift + cmd - 5 : yabai -m window --space 5
      #     shift + cmd - 6 : yabai -m window --space 6
      #     shift + cmd - 7 : yabai -m window --space 7
      #     shift + cmd - 8 : yabai -m window --space 8
      #     shift + cmd - 9 : yabai -m window --space 9

      #     # Rotate windows (like Hyprland's SUPER + R)
      #     alt - r : yabai -m space --rotate 90

      #     # Mirror/Flip windows
      #     alt - y : yabai -m space --mirror y-axis
      #     alt - x : yabai -m space --mirror x-axis

      #     # Toggle window split type
      #     alt - e : yabai -m window --toggle split

      #     # Stack Management (similar to Hyprland's master layout)
      #     alt - s : yabai -m window --stack next
      #     shift + alt - s : yabai -m window --stack recent

      #     # Focus window through stack
      #     alt - p : yabai -m window --focus stack.prev
      #     alt - n : yabai -m window --focus stack.next

      #     # Toggle gaps
      #     alt - g : yabai -m space --toggle padding; yabai -m space --toggle gap

      #     # Restart yabai (like Hyprland's SUPER + SHIFT + C)
      #     shift + alt - r : yabai --restart-service
        '';
      };
      aerospace = {
        enable = true;
        settings = builtins.fromTOML (builtins.readFile ./.aerospace.toml);
        package = pkgs.aerospace;
      };
      # yabai = {
      #   enable = true;
      #   package = pkgs.yabai;
      #   enableScriptingAddition = true;
      # };
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
