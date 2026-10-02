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
      # Upstream's bunfig.toml refuses any version published in the last 3
      # days, and bun 1.3.14 applies that even to versions already pinned in
      # bun.lock. A v2 commit that bumps a dependency the same day it ships
      # (@agentclientprotocol/sdk 1.6.0 on 2026-10-01) then fails to install
      # until the version ages in. The lockfile plus outputHash already pin the
      # exact contents, so the gate adds nothing here.
      sed -i '/^minimumReleaseAge[[:space:]]*=/d' bunfig.toml
      bun install \
        --cpu="${bunCpu}" \
        --os="${bunOs}" \
        --filter '!./' \
        --filter './packages/cli' \
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
    outputHash = "sha256-NAQA5Dh7t5/GPrRr9k/vuWAUSrBnTIxkEwyUez/xCrU=";
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
    # Upstream v2 renamed the compiled binary to `opencode` (commit bb564f96a,
    # "feat(cli): rename command to opencode"). Keep exposing it as `opencode2`
    # so it does not collide with the `opencode` package on PATH.
    if [ -e $out/bin/opencode ] && [ ! -e $out/bin/opencode2 ]; then
      mv $out/bin/opencode $out/bin/opencode2
    fi
    wrapProgram $out/bin/opencode2 \
      --prefix PATH : ${lib.makeBinPath ([ ripgrep ] ++ lib.optional platform.isDarwin sysctl)} \
      `# v2 defaults to the same opencode.db as v1 and runs v2-only migrations` \
      `# against it, which drop tables v1 still uses (workspace, session_input,` \
      `# session_context_epoch, data_migration). Give v2 its own database.` \
      --set-default OPENCODE_DB opencode-v2.db \
      --set-default OPENCODE_EXPERIMENTAL 1 \
      --set-default OPENCODE_EXPERIMENTAL_OUTPUT_TOKEN_MAX 999999999 \
      --set-default OPENCODE_EXPERIMENTAL_BASH_DEFAULT_TIMEOUT_MS 300000 \
      --set-default OPENCODE_EXPERIMENTAL_LSP_TY 1
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
