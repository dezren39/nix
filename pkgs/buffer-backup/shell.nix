# shell.nix — for nix-shell compatibility
# Prefer `nix develop` (uses flake.nix) but this works with legacy nix-shell
let
  pkgs = import <nixpkgs> { };
in
pkgs.mkShell {
  packages = [
    pkgs.bun
    pkgs.nodejs_22
    pkgs.typescript
  ];

  shellHook = ''
    echo "buffer-backup dev shell (nix-shell)"
    echo "  bun install    — install deps"
    echo "  bun x tsc -p . — compile"
  '';
}
