{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    nixpkgs-unstable = {
      url = "github:nixos/nixpkgs";
      # Same as nixpkgs — candidate for same-URL consolidation
    };
    systems.url = "github:nix-systems/default";
    flake-utils = {
      url = "github:numtide/flake-utils";
      # transitive dep on systems — candidate for dedup
    };
    some-tool = {
      url = "github:example/some-tool";
      # transitive deps on nixpkgs and some-lib
      # some-lib has no root equivalent — candidate for flatten
    };
  };
  outputs = _: { };
}
