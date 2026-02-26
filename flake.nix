{
  description = "Darwin configuration";

  inputs = {
    nixpkgs = {
      # url = "github:developing-today-forks/nixpkgs";
      # url = "github:nixos/nixpkgs/nixos-unstable";
      # url = "github:nixos/nixpkgs/staging-next";
      # url = "github:ofalvai/nixpkgs/push-nqwkpkkyqxzv"; # 72a5334
      # url = "github:nixos/nixpkgs/nixos-unstable";
      # url = "github:nixos/nixpkgs/70801e0"; # swift on darwin https://github.com/nixos/nixpkgs/issues/483584
      # url = "github:ofalvai/nixpkgs/72a5334";
      url = "github:nixos/nixpkgs";
    };
    nixpkgs-git = {
      # url = "github:nixos/nixpkgs/nixos-unstable";
      # url = "github:nixos/nixpkgs/70801e0"; # swift on darwin https://github.com/nixos/nixpkgs/issues/483584
      # url = "github:nixos/nixpkgs/staging-next";
      # url = "github:ofalvai/nixpkgs/push-nqwkpkkyqxzv"; # 72a5334
      # url = "github:ofalvai/nixpkgs/72a5334";
      url = "github:nixos/nixpkgs";
    };
    stable = {
      # url = "github:developing-today-forks/nixpkgs";
      # url = "github:nixos/nixpkgs/nixos-unstable";
      url = "github:nixos/nixpkgs";
      # url = "github:nixos/nixpkgs/nixos-unstable";
      # url = "github:nixos/nixpkgs/staging-next";
      # url = "github:ofalvai/nixpkgs/push-nqwkpkkyqxzv"; # 72a5334
      # url = "github:nixos/nixpkgs/nixos-unstable";
      # url = "github:nixos/nixpkgs/70801e0"; # swift on darwin https://github.com/nixos/nixpkgs/issues/483584
      # url = "github:ofalvai/nixpkgs/72a5334";
    };
    systems.url = "github:nix-systems/default";
    determinate = {
      url = "https://flakehub.com/f/DeterminateSystems/determinate/*";
      # inputs.nixpkgs.follows = "nixpkgs";
    };
    darwin = {
      url = "github:lnl7/nix-darwin";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager/master";
      # inputs.nixpkgs.follows = "nixpkgs";
    };
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      # inputs.nixpkgs.follows = "nixpkgs";
    };
    brew-src = {
      # must keep this at least as new as https://github.com/zhaofengli/nix-homebrew/blob/main/flake.nix#L6
      # find latest version here https://github.com/Homebrew/brew/releases
      url = "github:Homebrew/brew/5.0.15";
      flake = false;
    };
    nix-homebrew = {
      url = "github:zhaofengli/nix-homebrew";
      inputs.brew-src.follows = "brew-src";
    };
    homebrew-core = {
      url = "github:homebrew/homebrew-core";
      flake = false;
    };
    homebrew-cask = {
      url = "github:homebrew/homebrew-cask";
      flake = false;
    };
    homebrew-bundle = {
      url = "github:homebrew/homebrew-bundle";
      flake = false;
    };
    homebrew-firefox-profile-switcher = {
      url = "github:null-dev/homebrew-firefox-profile-switcher";
      flake = false;
    };
    homebrew-autoraise = {
      url = "github:Dimentium/homebrew-autoraise";
      flake = false;
    };
    homebrew-services = {
      url = "github:homebrew/homebrew-services";
      flake = false;
    };

    nur = {
      url = "github:nix-community/NUR";
      # inputs.nixpkgs.follows = "nixpkgs";
    }; # what is https://github.com/nix-community/nur-combined ?
    # rust, see https://github.com/nix-community/fenix#usage
    treefmt-nix.url = "github:numtide/treefmt-nix";
    nixpkgs-terraform.url = "github:stackbuilders/nixpkgs-terraform";
    flake-utils.url = "github:numtide/flake-utils";
  };
  outputs =
    inputs:
    let
      eachSystem =
        f:
        inputs.nixpkgs.lib.genAttrs (import inputs.systems) (
          system: f inputs.nixpkgs.legacyPackages.${system}
        );
      treefmtEval = eachSystem (pkgs: inputs.treefmt-nix.lib.evalModule pkgs ./treefmt.nix);
    in
    {
      darwinConfigurations = {
        MGM9JJ4V3R = inputs.darwin.lib.darwinSystem rec {
          system = "aarch64-darwin";
          modules = [
            #inputs.determinate.darwinModules.default
            inputs.nix-homebrew.darwinModules.nix-homebrew
            inputs.mac-app-util.darwinModules.default
            inputs.home-manager.darwinModules.home-manager
            ./configuration.nix
          ];
          specialArgs = { inherit inputs system; };
        };
      };
      formatter = eachSystem (pkgs: treefmtEval.${pkgs.system}.config.build.wrapper);
      checks = eachSystem (pkgs: {
        formatting = treefmtEval.${pkgs.system}.config.build.check inputs.self;
      });
    };
}
