{
  lib,
  stdenvNoCC,
  fetchurl,
}:

let
  versionData = builtins.fromJSON (builtins.readFile ./hashes.json);
  inherit (versionData) version hashes;

  # Upstream asset naming: codedb-<os>-<arch> (no version suffix, no
  # extension except on Windows, which this package does not ship).
  assetName =
    {
      "aarch64-darwin" = "codedb-darwin-arm64";
      "x86_64-darwin" = "codedb-darwin-x86_64";
      "aarch64-linux" = "codedb-linux-arm64";
      "x86_64-linux" = "codedb-linux-x86_64";
    }
    .${stdenvNoCC.hostPlatform.system}
      or (throw "Unsupported platform for codedb: ${stdenvNoCC.hostPlatform.system}");
in
stdenvNoCC.mkDerivation {
  pname = "codedb";
  inherit version;

  src = fetchurl {
    url = "https://github.com/justrach/codedb/releases/download/v${version}/${assetName}";
    hash =
      hashes.${stdenvNoCC.hostPlatform.system}
        or (throw "Missing codedb hash for ${stdenvNoCC.hostPlatform.system}");
  };

  dontUnpack = true;
  dontStrip = true;

  installPhase = ''
    runHook preInstall
    install -Dm755 "$src" "$out/bin/codedb"
    runHook postInstall
  '';

  meta = {
    description = "codedb — prebuilt binary release of justrach/codedb";
    homepage = "https://github.com/justrach/codedb";
    sourceProvenance = with lib.sourceTypes; [ binaryNativeCode ];
    mainProgram = "codedb";
    platforms = builtins.attrNames hashes;
  };
}
