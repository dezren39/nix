{
  stdenvNoCC,
  fetchurl,
}:
stdenvNoCC.mkDerivation {
  pname = "codedb";
  version = "0.2.5830";

  src = fetchurl {
    url = "https://github.com/justrach/codedb/releases/download/v0.2.5830/codedb-darwin-arm64";
    hash = "sha256-K3htdATPjqeYV9zToGfTVJPmfT61I+oetYEBA97ASM0=";
  };

  dontUnpack = true;
  dontStrip = true;

  installPhase = ''
    runHook preInstall
    install -Dm755 "$src" "$out/bin/codedb"
    runHook postInstall
  '';
}
