{
  lib,
  stdenvNoCC,
  fetchurl,
  autoPatchelfHook,
  unzip,
  installShellFiles,
  openssl,
  cctools,
  darwin,
  rcodesign,
}:

let
  versionData = builtins.fromJSON (builtins.readFile ./hashes.json);
  inherit (versionData) version hashes;

  archName =
    {
      "aarch64-darwin" = "bun-darwin-aarch64";
      "aarch64-linux" = "bun-linux-aarch64";
      "x86_64-darwin" = "bun-darwin-x64-baseline";
      "x86_64-linux" = "bun-linux-x64";
    }
    .${stdenvNoCC.hostPlatform.system}
      or (throw "Unsupported platform for bun-bin: ${stdenvNoCC.hostPlatform.system}");
in
stdenvNoCC.mkDerivation {
  pname = "bun-bin";
  inherit version;

  src = fetchurl {
    url = "https://github.com/oven-sh/bun/releases/download/bun-v${version}/${archName}.zip";
    hash =
      hashes.${stdenvNoCC.hostPlatform.system}
        or (throw "Missing bun hash for ${stdenvNoCC.hostPlatform.system}");
  };

  sourceRoot =
    {
      "aarch64-darwin" = archName;
      "x86_64-darwin" = archName;
    }
    .${stdenvNoCC.hostPlatform.system} or null;

  strictDeps = true;

  nativeBuildInputs = [
    unzip
    installShellFiles
  ]
  ++ lib.optionals stdenvNoCC.hostPlatform.isLinux [ autoPatchelfHook ];

  buildInputs = [ openssl ];

  dontConfigure = true;
  dontBuild = true;

  installPhase = ''
    runHook preInstall
    install -Dm 755 ./bun $out/bin/bun
    ln -s $out/bin/bun $out/bin/bunx
    runHook postInstall
  '';

  postPhases = [ "postPatchelf" ];
  postPatchelf =
    lib.optionalString stdenvNoCC.hostPlatform.isDarwin ''
      '${lib.getExe' cctools "${cctools.targetPrefix}install_name_tool"}' $out/bin/bun \
        -change /usr/lib/libicucore.A.dylib '${lib.getLib darwin.ICU}/lib/libicucore.A.dylib'
      '${lib.getExe rcodesign}' sign --code-signature-flags linker-signed $out/bin/bun
    ''
    +
      lib.optionalString
        (
          stdenvNoCC.buildPlatform.canExecute stdenvNoCC.hostPlatform
          && !(stdenvNoCC.hostPlatform.isDarwin && stdenvNoCC.hostPlatform.isx86_64)
        )
        ''
          installShellCompletion --cmd bun \
            --bash <(SHELL="bash" $out/bin/bun completions) \
            --zsh <(SHELL="zsh" $out/bin/bun completions) \
            --fish <(SHELL="fish" $out/bin/bun completions)
        '';

  meta = {
    description = "Latest Bun runtime (prebuilt binary)";
    homepage = "https://bun.sh";
    changelog = "https://bun.sh/blog/bun-v${version}";
    license = with lib.licenses; [ mit lgpl21Only ];
    sourceProvenance = with lib.sourceTypes; [ binaryNativeCode ];
    mainProgram = "bun";
    platforms = builtins.attrNames hashes;
  };
}
