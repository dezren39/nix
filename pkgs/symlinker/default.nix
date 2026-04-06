{
  pkgs,
  symlinkerSrc,
}:
pkgs.writeShellApplication {
  name = "symlinker";
  runtimeInputs = with pkgs; [
    coreutils
    findutils
  ];
  text = ''
    exec bash ${symlinkerSrc} "$@"
  '';
}
