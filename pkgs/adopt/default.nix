{
  pkgs,
  lib,
  ...
}:
pkgs.writeShellScriptBin "adopt" (builtins.readFile ./adopt.sh)
