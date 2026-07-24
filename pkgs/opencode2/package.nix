{
  lib,
  stdenvNoCC,
  bun-bin,
  nodejs,
  sysctl,
  makeBinaryWrapper,
  models-dev,
  ripgrep,
  writableTmpDirAsHomeHook,
  opencode2Src,
  rev,
}:
let
  platform = stdenvNoCC.hostPlatform;
  bunCpu = if platform.isAarch64 then "arm64" else "x64";
  bunOs = if platform.isLinux then "linux" else "darwin";

  node_modules = stdenvNoCC.mkDerivation {
    pname = "opencode2-node-modules";
    version = "0.0.0+${lib.substring 0 7 rev}";
    src = opencode2Src;

    impureEnvVars = lib.fetchers.proxyImpureEnvVars ++ [
      "GIT_PROXY_COMMAND"
      "SOCKS_SERVER"
    ];
    nativeBuildInputs = [ bun-bin ];
    dontConfigure = true;

    buildPhase = ''
      runHook preBuild
      export BUN_INSTALL_CACHE_DIR=$(mktemp -d)
      bun install \
        --cpu="${bunCpu}" \
        --os="${bunOs}" \
        --filter '!./' \
        --filter './packages/cli' \
        --frozen-lockfile \
        --ignore-scripts \
        --no-progress
      bun --bun nix/scripts/canonicalize-node-modules.ts
      bun --bun nix/scripts/normalize-bun-binaries.ts
      runHook postBuild
    '';

    installPhase = ''
      runHook preInstall
      mkdir -p $out
      find . -type d -name node_modules -exec cp -R --parents {} $out \;
      runHook postInstall
    '';

    dontFixup = true;
    outputHashAlgo = "sha256";
    outputHashMode = "recursive";
    outputHash = "sha256-akqeQv3XdfTOeSkRibt+2jLBSaIBULV6WN0n9gY2XbQ=";
  };
in
stdenvNoCC.mkDerivation (finalAttrs: {
  pname = "opencode2";
  version = "0.0.0+${lib.substring 0 7 rev}";
  src = opencode2Src;

  nativeBuildInputs = [
    bun-bin
    nodejs
    makeBinaryWrapper
    models-dev
    writableTmpDirAsHomeHook
  ];

  postPatch = ''
    substituteInPlace packages/script/src/index.ts \
      --replace-fail 'throw new Error(`This script requires bun@''${expectedBunVersionRange}' \
                     'console.warn(`Warning: This script requires bun@''${expectedBunVersionRange}'
  '';

  configurePhase = ''
    runHook preConfigure
    cp -R ${node_modules}/. .
    patchShebangs node_modules
    patchShebangs packages/*/node_modules
    runHook postConfigure
  '';

  env.MODELS_DEV_API_JSON = "${models-dev}/dist/_api.json";
  env.OPENCODE_DISABLE_MODELS_FETCH = true;
  env.OPENCODE_VERSION = finalAttrs.version;
  env.OPENCODE_CHANNEL = "next";

  buildPhase = ''
    runHook preBuild
    bun --bun packages/cli/script/build.ts --single --skip-install
    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall
    mkdir -p $out
    cp -R packages/cli/dist/cli-*/bin $out/
    wrapProgram $out/bin/opencode2 \
      --prefix PATH : ${lib.makeBinPath ([ ripgrep ] ++ lib.optional platform.isDarwin sysctl)}
    runHook postInstall
  '';

  doInstallCheck = platform.canExecute platform;
  installCheckPhase = ''
    runHook preInstallCheck
    $out/bin/opencode2 --version
    runHook postInstallCheck
  '';

  passthru = { inherit node_modules; };

  meta = {
    description = "OpenCode 2.0 beta coding agent";
    homepage = "https://v2.opencode.ai";
    license = lib.licenses.mit;
    mainProgram = "opencode2";
    platforms = [
      "aarch64-linux"
      "x86_64-linux"
      "aarch64-darwin"
      "x86_64-darwin"
    ];
  };
})
