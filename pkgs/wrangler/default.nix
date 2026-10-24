{
  lib,
  buildNpmPackage,
  fetchurl,
  nodejs,
}:

# Cloudflare Wrangler.
#   https://developers.cloudflare.com/workers/wrangler/
#   Source: https://github.com/cloudflare/workers-sdk (packages/wrangler)
#
# Why this exists rather than using pkgs.wrangler:
#   nixpkgs carries 4.94.0 while npm latest is 4.147.0 -- fifty-three minor
#   versions behind, the largest gap of anything in this config. Wrangler tracks
#   the Workers runtime closely, so that gap is not cosmetic.
#
# Packaging notes:
#   * npm is the only distribution channel. cloudflare/workers-sdk is a
#     monorepo whose GitHub releases are per-package tags (the "latest release"
#     is whatever subpackage shipped most recently, e.g.
#     @cloudflare/deploy-helpers), with no wrangler binary attached. Resolve the
#     version from the npm registry, not from GitHub releases.
#   * Unlike the Vercel CLI (../vercel-cli), wrangler is NOT self-contained: it
#     needs its 8 runtime dependencies, most importantly `workerd`, which is the
#     actual Workers runtime binary. So buildNpmPackage, not a bare unpack.
#   * The npm tarball ships no package-lock.json, so one is vendored here,
#     generated from the published package.json with devDependencies/scripts
#     stripped and `npm install --package-lock-only`. The same strip is
#     reapplied in postPatch so the lock matches package.json.
#   * fsevents is an optionalDependency and macOS-only; it resolves to nothing
#     on Linux.
#   * Upstream publishes three bin names -- wrangler, wrangler2 and cf-wrangler
#     -- all pointing at the same entrypoint. npmInstallHook creates all three.
#   * To update: bump version, update `hash`, regenerate package-lock.json as
#     above, then set npmDepsHash from the build failure message.

let
  version = "4.147.0";
in
buildNpmPackage {
  pname = "wrangler";
  inherit version nodejs;

  src = fetchurl {
    url = "https://registry.npmjs.org/wrangler/-/wrangler-${version}.tgz";
    hash = "sha256-xC0hD6GfbkC2Oo3zsSd0ATbf/nUl7p7YK4qxg9LNg5U=";
  };

  postPatch = ''
    cp ${./package-lock.json} package-lock.json
    chmod +w package.json package-lock.json

    ${lib.getExe nodejs} -e '
      const fs = require("fs");
      const p = JSON.parse(fs.readFileSync("package.json", "utf8"));
      delete p.devDependencies;
      delete p.scripts;
      delete p.workspaces;
      fs.writeFileSync("package.json", JSON.stringify(p, null, 2));
    '
  '';

  npmDepsHash = "sha256-jRqQr6X4Z8cchDhDDVGOszCavEjSY6GSGkPyL4TMfB4=";

  npmFlags = [
    "--ignore-scripts"
    "--no-audit"
    "--no-fund"
  ];

  dontNpmBuild = true;

  # Wrangler phones home for update checks and telemetry on every invocation;
  # neither can write to /nix/store, and both are noise.
  makeWrapperArgs = [
    "--set"
    "WRANGLER_SEND_METRICS"
    "false"
    "--set"
    "NO_UPDATE_NOTIFIER"
    "1"
  ];

  meta = {
    description = "Command-line interface for all things Cloudflare Workers";
    homepage = "https://developers.cloudflare.com/workers/wrangler/";
    downloadPage = "https://www.npmjs.com/package/wrangler";
    license = with lib.licenses; [
      mit
      asl20
    ];
    mainProgram = "wrangler";
    platforms = lib.platforms.all;
  };
}
