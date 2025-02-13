{ pkgs, ... }:
{
  projectRootFile = "flake.nix";
  programs.nixfmt.enable = true;
  # programs.shellcheck.enable = true; # failures
}
