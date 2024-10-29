# flake.nix

{
  description = "Darwin configuration";

  inputs = {
    #nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    # nixpkgs.url = "github:nixos/nixpkgs";
    nixpkgs = {
      url = "github:nixos/nixpkgs";
    };
    stable = {
      url = "github:nixos/nixpkgs/nixos-unstable";
    };
    determinate.url = "https://flakehub.com/f/DeterminateSystems/determinate/0.1";
    darwin = {
      url = "github:lnl7/nix-darwin";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nix-homebrew = {
      # url = "github:zhaofengli/nix-homebrew";
      url = "github:dezren39/nix-homebrew/shellIntegration";
      inputs.nixpkgs.follows = "nixpkgs";
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
    # rust, see https://github.com/nix-community/fenix#usage
  };

  outputs = inputs: {
    darwinConfigurations = {
      MGM9JJ4V3R = inputs.darwin.lib.darwinSystem {
        system = "aarch64-darwin";
        modules = [
          inputs.determinate.darwinModules.default
          inputs.nix-homebrew.darwinModules.nix-homebrew
          inputs.mac-app-util.darwinModules.default
          inputs.home-manager.darwinModules.home-manager
          ./configuration.nix
        ];
        specialArgs = { inherit inputs; };
      };
    };
  };
}
