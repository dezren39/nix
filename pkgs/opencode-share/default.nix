{ pkgs }:
pkgs.writeShellApplication {
  name = "opencode-share";
  runtimeInputs = with pkgs; [
    coreutils
    findutils
  ];
  text = builtins.readFile ./opencode-share.sh;
}
