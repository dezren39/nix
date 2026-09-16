{
  stdenvNoCC,
  fetchurl,
}:
stdenvNoCC.mkDerivation {
  pname = "codedb";
  version = "0.2.5855";

  src = fetchurl {
    url = "https://github.com/justrach/codedb/releases/download/v0.2.5855/codedb-darwin-arm64";
    hash = "sha256-/EUhPRFXgCvUaldRoPYYGoc7C9jcRPmf81lYl6oOk40=";
  };

  dontUnpack = true;
  dontStrip = true;

  installPhase = ''
    runHook preInstall
    install -Dm755 "$src" "$out/bin/codedb"
    runHook postInstall
  '';
}
