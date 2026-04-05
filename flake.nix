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
      follows = "nixpkgs";
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
      follows = "nixpkgs";
    };
    systems.url = "github:nix-systems/default";
    determinate = {
      url = "https://flakehub.com/f/DeterminateSystems/determinate/*";
      # inputs.nixpkgs.follows = "nixpkgs";
      inputs.nix.inputs.git-hooks-nix.inputs.flake-compat.follows = "flake-compat";
      inputs.nix.inputs.flake-parts.follows = "flake-parts-hoisted";
      inputs.nix.inputs.git-hooks-nix.follows = "git-hooks-nix";
      inputs.nix.follows = "nix";
      inputs.nix.inputs.nixpkgs-23-11.follows = "nixpkgs-23-11";
      inputs.nix.inputs.nixpkgs-regression.follows = "nixpkgs-regression";
      inputs.nix.inputs.nixpkgs.follows = "nixpkgs-hoisted-2";
      inputs.nixpkgs.follows = "nixpkgs-hoisted-3";
    };
    darwin = {
      url = "github:lnl7/nix-darwin";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager/master";
      # inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs.follows = "nixpkgs-hoisted-4";
    };
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      # inputs.nixpkgs.follows = "nixpkgs";
      inputs.treefmt-nix.follows = "treefmt-nix";
      inputs.cl-nix-lite.follows = "cl-nix-lite";
      inputs.flake-utils.follows = "flake-utils";
      inputs.flake-compat.follows = "flake-compat-hoisted";
      inputs.nixpkgs.follows = "nixpkgs-hoisted-5";
      inputs.systems.follows = "systems-hoisted";
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
    nixpkgs-terraform.inputs.systems.follows = "systems";
    nixpkgs-terraform.inputs.nixpkgs-23_05.follows = "nixpkgs-23_05";
    nixpkgs-terraform.inputs.nixpkgs-24_05.follows = "nixpkgs-24_05";
    nixpkgs-terraform.inputs.nixpkgs.follows = "nixpkgs-hoisted-hoisted";
    flake-utils.url = "github:numtide/flake-utils";
    flake-utils.inputs.systems.follows = "systems";
    opencode = {
      url = "github:anomalyco/opencode/dev";
      # inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs.follows = "nixpkgs-hoisted-hoisted";
    };
    # nixpkgs-helium = {
    #   # https://github.com/NixOS/nixpkgs/pull/498572
    #   url = "github:Nytelife26/nixpkgs/helium/init";
    # };
    cl-nix-lite = {
      url = "github:hraban/cl-nix-lite";
      inputs.systems.follows = "systems";
      inputs.treefmt-nix.follows = "treefmt-nix";
      inputs.treefmt-nix.inputs.nixpkgs.follows = "nixpkgs-hoisted-hoisted";
      inputs.flake-parts.follows = "flake-parts";
      inputs.nixpkgs.follows = "nixpkgs-hoisted";
      inputs.flake-parts.inputs.nixpkgs-lib.follows = "nixpkgs-lib";
    };
    flake-compat.url = "github:edolstra/flake-compat";
    flake-compat-hoisted.url = "github:hraban/flake-compat/fixed-output";
    flake-parts.url = "github:hercules-ci/flake-parts";
    flake-parts-hoisted.url = "https://flakehub.com/f/hercules-ci/flake-parts/0.1";
    git-hooks-nix.url = "https://flakehub.com/f/cachix/git-hooks.nix/0.1.941";
    nix.url = "https://flakehub.com/f/DeterminateSystems/nix-src/%2A";
    nixpkgs-hoisted.url = "github:nixos/nixpkgs/nixos-25.11";
    nixpkgs-23-11.url = "github:NixOS/nixpkgs";
    nixpkgs-23_05.url = "github:nixos/nixpkgs/nixos-23.05-small";
    nixpkgs-24_05.url = "github:nixos/nixpkgs/nixos-24.05-small";
    nixpkgs-lib.url = "github:nix-community/nixpkgs.lib";
    nixpkgs-regression.url = "github:NixOS/nixpkgs";
    nixpkgs-hoisted-hoisted.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    nixpkgs-hoisted-2.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2505";
    nixpkgs-hoisted-3.url = "https://flakehub.com/f/DeterminateSystems/nixpkgs-weekly/0.1";
    nixpkgs-hoisted-4.url = "github:NixOS/nixpkgs/nixos-unstable";
    nixpkgs-hoisted-5.url = "github:NixOS/nixpkgs";
    systems-hoisted.url = "github:nix-systems/default-darwin";
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
            inputs.determinate.darwinModules.default
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
        tidy =
          pkgs.runCommandLocal "flake-tidy-check"
            {
              nativeBuildInputs = [ (import ./pkgs/flake-tidy { inherit pkgs; }) ];
              src = inputs.self;
            }
            ''
              flake-tidy all --check --flake-dir $src
              touch $out
            '';
      });
      apps = eachSystem (pkgs: {
        flake-tidy = {
          type = "app";
          program = "${import ./pkgs/flake-tidy { inherit pkgs; }}/bin/flake-tidy";
        };
      });
    };
}
