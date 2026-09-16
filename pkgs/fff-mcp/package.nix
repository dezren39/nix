{
  stdenvNoCC,
  fetchurl,
}:
stdenvNoCC.mkDerivation {
  pname = "fff-mcp";
  version = "0.10.6";

  src = fetchurl {
    url = "https://github.com/dmtrKovalenko/fff/releases/download/v0.10.6/fff-mcp-aarch64-apple-darwin";
    hash = "sha256-AuD1f1uI+mmElPMQ2ABaDDTVvaWh/NBpUgs1+OIxmJI=";
  };

  dontUnpack = true;
  dontStrip = true;

  installPhase = ''
    runHook preInstall
    install -Dm755 "$src" "$out/bin/fff-mcp"
    runHook postInstall
  '';
}
