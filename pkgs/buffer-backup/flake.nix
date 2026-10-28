{
  description = "VS Code buffer-backup extension";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.stdenv.mkDerivation {
          pname = "buffer-backup-vscode";
          version = "0.2.0";
          src = ./.;

          nativeBuildInputs = [
            pkgs.bun
            pkgs.nodejs_22
          ];

          buildPhase = ''
            export HOME=$(mktemp -d)
            # This build installs from bun.lock ONLY, while Renovate
            # updates package-lock.json ONLY (#781): the two lockfiles
            # drift unless bun.lock is regenerated (`bun install`)
            # when a Renovate PR lands. The lock-sync check below
            # (`nix flake check` here, or check-lock-sync.py) fails
            # when they resolve a dependency to different versions.
            bun install --frozen-lockfile 2>/dev/null || bun install
            bun x tsc -p ./
          '';

          installPhase = ''
            mkdir -p $out
            cp -r out $out/out
            cp package.json $out/
          '';
        };

        packages.install = pkgs.writeShellScriptBin "buffer-backup-install" ''
          set -euo pipefail
          EXT_SRC="${self.packages.${system}.default}"
          for dir in "$HOME/.vscode/extensions" "$HOME/.vscode-insiders/extensions"; do
            if [ -d "$(dirname "$dir")" ]; then
              mkdir -p "$dir"
              LINK="$dir/drewry-pope.buffer-backup-0.2.0"
              rm -rf "$LINK"
              ln -sfn "$EXT_SRC" "$LINK"
              echo "Installed buffer-backup -> $LINK"
            fi
          done
        '';

        # #781: fail when bun.lock and package-lock.json resolve the
        # same package.json dependency to different versions.
        checks.lock-sync =
          pkgs.runCommand "buffer-backup-lock-sync"
            {
              nativeBuildInputs = [ pkgs.python3 ];
            }
            ''
              python3 ${./check-lock-sync.py} ${./.}
              touch $out
            '';

        # #1013: cleanup-count.test.mjs -- the #1002 regression suite
        # for cleanupCount one-pass convergence (plus the cleanupSize
        # control) -- was run by nothing: no flake check, workflow, or
        # npm script referenced it, so an extension.ts change could
        # silently break the #1002 post-condition and merge green.
        # Run the real suite here, following the house pattern (#965):
        # the test reads src/extension.ts relative to its own
        # location, so copy both into the check tree in that layout.
        # The buffer-backup workflow also runs the suite directly.
        checks.cleanup-count =
          pkgs.runCommand "buffer-backup-cleanup-count"
            {
              nativeBuildInputs = [ pkgs.nodejs_22 ];
            }
            ''
              mkdir -p src
              cp ${./cleanup-count.test.mjs} cleanup-count.test.mjs
              cp ${./src/extension.ts} src/extension.ts
              node cleanup-count.test.mjs
              touch $out
            '';

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.bun
            pkgs.nodejs_22
            pkgs.typescript
          ];

          shellHook = ''
            echo "buffer-backup dev shell"
            echo "  bun install    — install deps"
            echo "  bun x tsc -p . — compile"
            echo "  nix build      — build extension"
          '';
        };
      }
    );
}
