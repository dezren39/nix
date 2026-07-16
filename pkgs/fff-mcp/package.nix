{
  stdenvNoCC,
  fetchurl,
}:
stdenvNoCC.mkDerivation {
  pname = "fff-mcp";
  version = "0.9.6";

  src = fetchurl {
    url = "https://github.com/dmtrKovalenko/fff/releases/download/v0.9.6/fff-mcp-aarch64-apple-darwin";
    hash = "sha256-Kaf63q+wYvPllUsauMaeFNyiT14GHNjTseobqzhaN1Q=";
  };

  dontUnpack = true;
  dontStrip = true;

  installPhase = ''
    runHook preInstall
    install -Dm755 "$src" "$out/bin/fff-mcp"
    runHook postInstall
  '';
}
