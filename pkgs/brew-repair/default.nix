{
  pkgs,
  lib,
  ...
}:
pkgs.writeShellScriptBin "brew-repair" (builtins.readFile ./brew-repair.sh)
