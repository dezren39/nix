{
  pkgs,
}:
pkgs.writeShellApplication {
  name = "flake-tidy";
  runtimeInputs = with pkgs; [
    python3
    nixfmt
    nix
  ];
  text = ''
    exec python3 ${./flake_tidy.py} "$@"
  '';
}
