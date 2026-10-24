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
