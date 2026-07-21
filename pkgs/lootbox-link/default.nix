{ pkgs }:
pkgs.writeShellApplication {
  name = "lootbox-link";
  runtimeInputs = with pkgs; [
    coreutils
    findutils
    git
    gnugrep
    rsync
  ];
  text = builtins.readFile ./lootbox-link.sh;
}
