# SPDX-License-Identifier: MIT OR Apache-2.0
#
# The MCP servers that are only published to npm, pinned by package-lock.json:
# every tarball is fetched by its integrity hash at build time, so nothing is
# npm-installed on the machine. Exposes bin/mcp-remote (the OAuth proxy for
# the remote servers in mcpx.config.nix) and bin/chrome-devtools-mcp.
#
# These are the same versions the removed lootbox updater npm-installed into
# ~/.local/share/lootbox/npm, so mcp-remote keeps finding its existing OAuth
# tokens in ~/.mcp-auth without a new login.
#
# To update: change the versions in package.json, then in this directory run
#   npm install --package-lock-only --ignore-scripts
# and rebuild.
{
  stdenvNoCC,
  importNpmLock,
  nodejs,
}:
let
  nodeModules = importNpmLock.buildNodeModules {
    npmRoot = ./.;
    inherit nodejs;
    # No package in the lockfile has an install script today; keep it that
    # way if a bump adds one, as the old `npm install --ignore-scripts` did.
    derivationArgs.npmRebuildFlags = [ "--ignore-scripts" ];
  };
in
stdenvNoCC.mkDerivation {
  pname = "npm-mcp-servers";
  version = "1";
  dontUnpack = true;
  installPhase = ''
    runHook preInstall
    mkdir -p $out/bin
    for bin in mcp-remote chrome-devtools-mcp; do
      ln -s ${nodeModules}/node_modules/.bin/$bin $out/bin/$bin
    done
    runHook postInstall
  '';
  passthru = { inherit nodeModules; };
}
