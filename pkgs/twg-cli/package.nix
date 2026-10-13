{
  lib,
  stdenvNoCC,
  fetchurl,
  autoPatchelfHook,
}:

let
  versionData = builtins.fromJSON (builtins.readFile ./hashes.json);
  inherit (versionData) version hashes;

  # Matches the asset naming in the official installer
  # (https://teamwork-graph.atlassian.com/cli/install): twg-<platform>-<arch>-v<version>.
  assetName =
    {
      "aarch64-darwin" = "twg-darwin-arm64";
      "x86_64-darwin" = "twg-darwin-x64";
      "aarch64-linux" = "twg-linux-arm64";
      "x86_64-linux" = "twg-linux-x64";
    }
    .${stdenvNoCC.hostPlatform.system}
      or (throw "Unsupported platform for twg-cli: ${stdenvNoCC.hostPlatform.system}");
in
stdenvNoCC.mkDerivation {
  pname = "twg-cli";
  inherit version;

  src = fetchurl {
    url = "https://teamwork-graph.atlassian.com/cli/${assetName}-v${version}";
    sha256 =
      hashes.${stdenvNoCC.hostPlatform.system}
        or (throw "Missing twg-cli hash for ${stdenvNoCC.hostPlatform.system}");
  };

  dontUnpack = true;
  dontConfigure = true;
  dontBuild = true;

  nativeBuildInputs = lib.optionals stdenvNoCC.hostPlatform.isLinux [ autoPatchelfHook ];

  installPhase = ''
    runHook preInstall
    install -Dm755 $src $out/bin/twg
    runHook postInstall
  '';

  meta = {
    description = "Atlassian Teamwork Graph CLI (twg) — prebuilt binary, pulls Jira/Confluence/Bitbucket/Teamwork Graph context into an AI agent's terminal";
    homepage = "https://teamworkgraph.com/cli";
    changelog = "https://developer.atlassian.com/cloud/twg-cli/changelog/";
    # Proprietary binary under Atlassian's terms, no SPDX id. Deliberately
    # left out of meta.license rather than lib.licenses.unfree: this package
    # is pulled in by inputs.self.packages.${system} (systemPackages.nix),
    # which nix-darwin's rebuild evaluates through plain
    # inputs.nixpkgs.legacyPackages (flake.nix's eachSystem), not the
    # allowUnfree = true pkgs instance configuration.nix builds for the
    # system closure. Setting it would make every rebuild need
    # NIXPKGS_ALLOW_UNFREE=1.
    sourceProvenance = with lib.sourceTypes; [ binaryNativeCode ];
    mainProgram = "twg";
    platforms = builtins.attrNames hashes;
  };
}
