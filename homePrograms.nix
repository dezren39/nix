_: {
  programs = {
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
