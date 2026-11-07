rec {
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
      url = "github:nixos/nixpkgs/nixpkgs-unstable";
    };
    nixpkgs-git = {
      # url = "github:nixos/nixpkgs/nixos-unstable";
      # url = "github:nixos/nixpkgs/70801e0"; # swift on darwin https://github.com/nixos/nixpkgs/issues/483584
      # url = "github:nixos/nixpkgs/staging-next";
      # url = "github:ofalvai/nixpkgs/push-nqwkpkkyqxzv"; # 72a5334
      # url = "github:ofalvai/nixpkgs/72a5334";
      # url = "github:nixos/nixpkgs";
      follows = "nixpkgs";
    };
    stable = {
      # url = "github:developing-today-forks/nixpkgs";
      # url = "github:nixos/nixpkgs/nixos-unstable";
      # url = "github:nixos/nixpkgs";
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
      inputs.determinate-nixd-aarch64-darwin.follows = "determinate-nixd-aarch64-darwin";
      inputs.determinate-nixd-aarch64-linux.follows = "determinate-nixd-aarch64-linux";
      inputs.determinate-nixd-x86_64-linux.follows = "determinate-nixd-x86_64-linux";
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
      # nixpkgs not followed — mac-app-util uses its own tested nixpkgs for SBCL/CL compat
      inputs.treefmt-nix.follows = "treefmt-nix";
      inputs.cl-nix-lite.follows = "cl-nix-lite";
      inputs.flake-utils.follows = "flake-utils";
      inputs.flake-compat.follows = "flake-compat-hoisted";
      inputs.systems.follows = "systems-hoisted";
      inputs.nixpkgs.follows = "nixpkgs-hoisted";
    };
    brew-src = {
      # Track upstream so ./r's flake update advances Homebrew automatically.
      url = "github:Homebrew/brew";
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
    homebrew-fuse = {
      url = "github:gromgit/homebrew-fuse";
      flake = false;
    };
    homebrew-local-whisper = {
      url = "github:gabrimatic/homebrew-local-whisper";
      flake = false;
    };

    nur = {
      url = "github:nix-community/NUR";
      # inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-parts.follows = "flake-parts";
      inputs.nixpkgs.follows = "nixpkgs-hoisted-4";
    }; # what is https://github.com/nix-community/nur-combined ?
    # rust, see https://github.com/nix-community/fenix#usage
    fenix = {
      url = "github:nix-community/fenix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.rust-analyzer-src.follows = "rust-analyzer-src";
    };
    treefmt-nix.url = "github:numtide/treefmt-nix";
    treefmt-nix.inputs.nixpkgs.follows = "nixpkgs-hoisted-hoisted";
    just = {
      url = "github:casey/just";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
    sops-nix = {
      url = "github:Mic92/sops-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nixpkgs-terraform.url = "github:stackbuilders/nixpkgs-terraform";
    nixpkgs-terraform.inputs.systems.follows = "systems";
    nixpkgs-terraform.inputs.nixpkgs-23_05.follows = "nixpkgs-23_05";
    nixpkgs-terraform.inputs.nixpkgs-24_05.follows = "nixpkgs-24_05";
    nixpkgs-terraform.inputs.nixpkgs.follows = "nixpkgs-hoisted-hoisted";
    flake-utils.url = "github:numtide/flake-utils";
    flake-utils.inputs.systems.follows = "systems";
    # developing-today's OpenChamber fork — this flake's ONLY
    # developing-today input (#1384). OpenChamber's own opencode input
    # (developing-today's OpenCode fork: v2, tracks upstream dev plus
    # model routing, mcpx and serve handover) is the opencode this
    # flake consumes, reached as inputs.openchamber.inputs.opencode;
    # the fork's mcpx (origin: developing-today/code, pkgs/mcpx) and
    # the mcpx daemon modules arrive through the same chain. We own
    # them all: if one breaks, fix it in its home repo and re-pin
    # here. The fork's own nixpkgs stays unfollowed (see flakeTidy
    # below): its node_modules hashes are computed against the bun
    # in that nixpkgs, and a different bun can lay node_modules out
    # differently and miss the fixed-output hash.
    openchamber.url = "github:developing-today-forks/openchamber";
    sidepulse-src = {
      url = "github:inteliwear/sidepulse/main";
      flake = false;
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
      # nixpkgs not followed — cl-nix-lite uses its own tested nixpkgs for SBCL/CL compat
      inputs.flake-parts.inputs.nixpkgs-lib.follows = "nixpkgs-lib";
      inputs.nixpkgs.follows = "nixpkgs-hoisted";
    };
    flake-compat.url = "github:edolstra/flake-compat";
    flake-compat-hoisted.url = "github:hraban/flake-compat/fixed-output";
    flake-parts.url = "github:hercules-ci/flake-parts";
    flake-parts.inputs.nixpkgs-lib.follows = "nixpkgs-lib";
    flake-parts-hoisted.url = "https://flakehub.com/f/hercules-ci/flake-parts/0.1";
    flake-parts-hoisted.inputs.nixpkgs-lib.follows = "nixpkgs-lib";
    git-hooks-nix.url = "https://flakehub.com/f/cachix/git-hooks.nix/0.1.941";
    git-hooks-nix.inputs.nixpkgs.follows = "nixpkgs-hoisted-hoisted";
    git-hooks-nix.inputs.flake-compat.follows = "flake-compat";
    nix.url = "https://flakehub.com/f/DeterminateSystems/nix-src/%2A";
    nix.inputs.flake-parts.follows = "flake-parts-hoisted";
    nix.inputs.git-hooks-nix.follows = "git-hooks-nix";
    nix.inputs.nixpkgs.follows = "nixpkgs-hoisted-2";
    nix.inputs.nixpkgs-23-11.follows = "nixpkgs-23-11";
    nix.inputs.nixpkgs-regression.follows = "nixpkgs-23-11";
    nix.inputs.git-hooks-nix.inputs.nixpkgs.follows = "nixpkgs-hoisted-hoisted";
    nix.inputs.flake-parts.inputs.nixpkgs-lib.follows = "nixpkgs-lib";
    nix.inputs.bdwgc.follows = "bdwgc";
    nixpkgs-hoisted.url = "github:nixos/nixpkgs/nixos-25.11";
    nixpkgs-23-11.url = "github:NixOS/nixpkgs";
    nixpkgs-23_05.url = "github:nixos/nixpkgs/nixos-23.05-small";
    nixpkgs-24_05.url = "github:nixos/nixpkgs/nixos-24.05-small";
    nixpkgs-lib.url = "github:nix-community/nixpkgs.lib";
    nixpkgs-regression.follows = "nixpkgs-23-11";
    nixpkgs-hoisted-hoisted.follows = "nixpkgs";
    nixpkgs-hoisted-2.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2505";
    nixpkgs-hoisted-3.url = "https://flakehub.com/f/DeterminateSystems/nixpkgs-weekly/0.1";
    nixpkgs-hoisted-4.url = "github:NixOS/nixpkgs/nixos-unstable";
    nixpkgs-hoisted-5.follows = "nixpkgs-23-11";
    systems-hoisted.url = "github:nix-systems/default-darwin";
    determinate-nixd-aarch64-darwin.url = "file+https://install.determinate.systems/determinate-nixd/tag/v3.23.2/macOS";
    determinate-nixd-aarch64-darwin.flake = false;
    determinate-nixd-aarch64-linux.url = "file+https://install.determinate.systems/determinate-nixd/tag/v3.23.2/aarch64-linux";
    determinate-nixd-aarch64-linux.flake = false;
    determinate-nixd-x86_64-linux.url = "file+https://install.determinate.systems/determinate-nixd/tag/v3.23.2/x86_64-linux";
    determinate-nixd-x86_64-linux.flake = false;
    nixpkgs-23-11-hoisted.follows = "nixpkgs-23-11";
    nixpkgs-regression-hoisted.follows = "nixpkgs-23-11";
    rust-analyzer-src.url = "github:rust-lang/rust-analyzer/nightly";
    rust-analyzer-src.flake = false;
    bdwgc.url = "github:DeterminateSystems/bdwgc/user-defined-stacks";
    bdwgc.flake = false;
  };
  outputs = inputs: rec {
    # flake-tidy config (pkgs/flake-tidy reads it via `nix eval .#flakeTidy`,
    # its outputs import probe, or a --config file — the tidy check passes
    # this evaluated value as a file; see #476). The
    # forks' own nixpkgs stay unfollowed: their node_modules fixed-output
    # hashes are computed with the bun in that nixpkgs.
    # #1384: openchamber is this flake's only developing-today input, and
    # its own transitive inputs (fleet, opencode, mcpx-src) are pinned to
    # versions that match its node_modules hashes and its mcpx/Go pairing.
    # Hoisting them to root would drag them onto this flake's nixpkgs (the
    # flatten pass keeps the newest duplicate), which breaks mcpx's
    # buildGo127Module lookup. Keep the whole subtree unpinned/unflattened,
    # like the forks' nixpkgs above.
    flakeTidy.exclude.follows = [
      "openchamber/opencode.nixpkgs"
      "openchamber.nixpkgs"
    ];
    flakeTidy.exclude.flatten = [
      "fleet"
      "mcpx-src"
      "nixpkgs"
      "opencode"
    ];
    eachSystem =
      f:
      inputs.nixpkgs.lib.genAttrs (builtins.filter (system: system != "x86_64-darwin") (
        import inputs.systems
      )) (system: f inputs.nixpkgs.legacyPackages.${system});

    # Per-host configuration; darwin-rebuild matches via scutil --get LocalHostName
    hosts = {
      MGM9JJ4V3R = {
        # inherits default system "aarch64-darwin"
        # extraModules = [ ];
        # extraSpecialArgs = { };
      };
    };
    # Default darwin system configuration shared by all hosts
    defaultDarwinSystem = {
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
    # Per-host darwinSystem args — merged defaults + host overrides, filtered to allowed keys
    darwinSystemConfigurations = builtins.mapAttrs (
      hostName: hostConfiguration:
      let
        merged = defaultDarwinSystem // { inherit hostName; } // hostConfiguration;
        extra = merged // {
          modules = merged.modules ++ (merged.extraModules or [ ]);
          specialArgs =
            merged.specialArgs
            // (merged.extraSpecialArgs or { })
            // {
              inherit (merged) hostName system;
            };
        };
        final = {
          inherit (extra) modules specialArgs;
        };
      in
      final
    ) hosts;
    darwinConfigurations = builtins.mapAttrs (
      _hostName: hostConfiguration: inputs.darwin.lib.darwinSystem hostConfiguration
    ) darwinSystemConfigurations;

    # The mcpx daemon modules (#343's option set, completed by the
    # consolidation #1384): they originate in developing-today/code
    # (pkgs/mcpx/modules), the opencode fork re-exports them with the
    # package defaulted to its own mcpx build, and openchamber passes
    # them through; this flake exposes the same outputs so consumers
    # of this flake keep one import shape. darwinModules.mcpx — the
    # slice #343 left remaining — now exists via the chain. This
    # repo's own machine configs do not enable services.mcpx yet:
    # configuration.nix still runs its hand-rolled agent, whose
    # fixed-path pattern is what the darwin module encodes.
    nixosModules.mcpx = inputs.openchamber.nixosModules.mcpx;
    homeManagerModules.mcpx = inputs.openchamber.homeManagerModules.mcpx;
    darwinModules.mcpx = inputs.openchamber.darwinModules.mcpx;
    homeManagerModules.fleet-brokers = import ./pkgs/fleet-brokers/home-module.nix;

    # Shared package definitions — used by packages, apps, devShells, and checks
    mkPackages =
      pkgs:
      let
        system = pkgs.stdenv.hostPlatform.system;
        bun-bin = pkgs.callPackage ./pkgs/bun-bin/package.nix { };
        # The fork's build, as pinned through the openchamber input,
        # unmodified: it already bundles the fork's mcpx — the same
        # build the daemon modules default to — so the CLI talks to
        # the same mcpx the daemon runs. The coherence this repo used
        # to maintain with a local override over a private mcpx build
        # is now true by construction (#1384).
        opencode-unwrapped = inputs.openchamber.inputs.opencode.packages.${system}.opencode;
        # Two things the fork cannot know, set by an outer shell wrapper (only a
        # shell wrapper can expand $HOME at exec time; the fork wraps with
        # makeBinaryWrapper, whose C wrapper rejects --run):
        #
        # OPENCODE_DB, forced, not defaulted. Every opencode and OpenChamber on
        # this machine must open the one database that holds the sessions:
        # ~/.local/share/opencode/opencode.db, which is where OpenChamber's own
        # server reads it (it hard-codes ~/.local/share/opencode and ignores
        # XDG_DATA_HOME) and where the fork's prod channel puts it. Absolute, so
        # neither XDG_DATA_HOME nor a stray OPENCODE_DB in some environment can
        # send a process to a fresh, empty database.
        #
        # TMPDIR: macOS's /var/folders/<...>/T/ is swept by the OS, and opencode
        # advertises $TMPDIR/opencode to agents as approved scratch space; they
        # have put git worktrees there and lost thousands of tracked files to a
        # sweep. OPENCODE_TMPDIR still overrides. Scoped to this wrapper rather
        # than the whole machine, which would retarget nix builds too.
        # developing-today-forks/opencode#13 asks for this in core.
        opencode =
          pkgs.runCommandLocal "opencode-${opencode-unwrapped.version}"
            {
              nativeBuildInputs = [ pkgs.makeShellWrapper ];
              passthru.unwrapped = opencode-unwrapped;
              inherit (opencode-unwrapped) version;
              meta = opencode-unwrapped.meta // {
                mainProgram = "opencode";
              };
            }
            ''
              mkdir -p $out/bin
              ln -s ${opencode-unwrapped}/share $out/share
              makeShellWrapper ${opencode-unwrapped}/bin/opencode $out/bin/opencode \
                --run 'export OPENCODE_DB="$HOME/.local/share/opencode/opencode.db"' \
                --run 'export TMPDIR="''${OPENCODE_TMPDIR:-$HOME/.local/share/opencode/tmp}"' \
                --run '${pkgs.coreutils}/bin/mkdir -p "$TMPDIR"'
            '';
      in
      ({
        inherit
          bun-bin
          opencode
          opencode-unwrapped
          ;

        # Not built here: the fork's mcpx, re-exposed so this flake's
        # package surface (systemPackages installs it) keeps its
        # shape while the build itself lives with the fork (#1384).
        mcpx = inputs.openchamber.inputs.opencode.packages.${system}.mcpx;

        # OpenChamber's server, running the wrapped opencode above:
        # the override swaps in the wrapper so the database and
        # scratch pins hold for the OpenCode it starts too.
        openchamber = inputs.openchamber.packages.${system}.openchamber.override { inherit opencode; };

        brew-repair = import ./pkgs/brew-repair {
          inherit pkgs;
          lib = pkgs.lib;
        };
        # `nix run .#adopt`: add this Mac to the hosts map (see pkgs/adopt).
        adopt = import ./pkgs/adopt {
          inherit pkgs;
          lib = pkgs.lib;
        };
        codedb = pkgs.callPackage ./pkgs/codedb/package.nix { };
        flake-tidy = import ./pkgs/flake-tidy { inherit pkgs; };
        twg-cli = pkgs.callPackage ./pkgs/twg-cli/package.nix { };
        # code#5362: the laptop fleet brokers (secrets + queue) and
        # the repo-queue client, as one workspace package.
        fleet-brokers = pkgs.callPackage ./pkgs/fleet-brokers/package.nix { };
        # code#5343: the resident laptop execution agent daemon.
        laptop-agent = pkgs.callPackage ./pkgs/laptop-agent/package.nix { };
        # mcp-remote and chrome-devtools-mcp, which mcpx.config.nix runs.
        npm-mcp-servers = pkgs.callPackage ./pkgs/npm-mcp-servers { };
        opencode-share = import ./pkgs/opencode-share { inherit pkgs; };
        # The service lifecycle event log (org.nixos.service-events).
        service-events = pkgs.callPackage ./pkgs/service-events { };
        symlinker = import ./pkgs/symlinker {
          inherit pkgs;
          symlinkerSrc = ./symlinker.sh;
        };
      })
      // pkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isDarwin (rec {
        sidepulse-unwrapped = pkgs.callPackage ./pkgs/sidepulse {
          sidepulseSrc = inputs.sidepulse-src;
        };
        sidepulse = pkgs.callPackage ./pkgs/sidepulse-setup {
          sidepulseUnwrapped = sidepulse-unwrapped;
          name = "sidepulse";
          defaultToSetup = false;
        };
        sidepulse-setup = pkgs.callPackage ./pkgs/sidepulse-setup {
          sidepulseUnwrapped = sidepulse-unwrapped;
        };
      });

    treefmtEval = eachSystem (pkgs: inputs.treefmt-nix.lib.evalModule pkgs ./treefmt.nix);

    packages = eachSystem mkPackages;
    formatter = eachSystem (pkgs: treefmtEval.${pkgs.stdenv.hostPlatform.system}.config.build.wrapper);
    checks = eachSystem (
      pkgs:
      {
        formatting = treefmtEval.${pkgs.stdenv.hostPlatform.system}.config.build.check inputs.self;
        tidy =
          let
            selfPkgs = mkPackages pkgs;
            # #476: inside the build sandbox a `nix eval` subprocess has no
            # store/daemon access, so the tool can never load this config
            # itself. Evaluate it here, where nix IS the evaluator, and
            # hand it to the tool as a file.
            flakeTidyConfig = pkgs.writeText "flake-tidy-config.json" (builtins.toJSON flakeTidy);
          in
          pkgs.runCommandLocal "flake-tidy-check"
            {
              nativeBuildInputs = [ selfPkgs.flake-tidy ];
              src = inputs.self;
            }
            ''
              flake-tidy all --check --flake-dir $src --config ${flakeTidyConfig}
              touch $out
            '';
        # #397: pkgs/flake-tidy has a 134-test pytest suite that nothing ran
        # (the package is a writeShellApplication with no checkPhase, and the
        # `tidy` check above only runs the tool against this repo). Run the
        # suite here so `nix flake check` — and any CI that runs it — fails
        # when the dedup/flatten logic regresses.
        flake-tidy-tests =
          pkgs.runCommandLocal "flake-tidy-tests"
            {
              nativeBuildInputs = [ pkgs.python3Packages.pytest ];
              src = ./pkgs/flake-tidy;
            }
            ''
              cp -r $src ./flake-tidy-src
              chmod -R u+w ./flake-tidy-src
              cd ./flake-tidy-src
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              pytest -q -p no:cacheprovider
              touch $out
            '';
        # #1149: simple-rebuild.test.sh pins simple-rebuild.sh's
        # mode-flag parser -- --build-only with --activate-only in
        # either order and a repeated --activate-only exit 2 naming
        # the flags instead of silently last-winning across the
        # no-sudo/sudo privilege split, and valid invocations parse
        # exactly as before. The suite extracts the real parser
        # segment from the script and runs it under zsh (the script's
        # interpreter), so it needs zsh but no build/activation.
        simple-rebuild-tests =
          pkgs.runCommandLocal "simple-rebuild-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.zsh
                pkgs.coreutils
                pkgs.gnugrep
                pkgs.gawk
              ];
            }
            ''
              cp ${./simple-rebuild.sh} simple-rebuild.sh
              cp ${./simple-rebuild.test.sh} simple-rebuild.test.sh
              chmod u+w simple-rebuild.sh simple-rebuild.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./simple-rebuild.test.sh
              touch $out
            '';
        # #1148: check-pin-sync.test.py — the probes for
        # pkgs/bun-bin/check-pin-sync.py (missing workflow / missing
        # hashes / malformed hashes must exit 2, never traceback at
        # the exit-1 "out of sync" signal; in-sync/out-of-sync
        # baselines unchanged). The test locates the script beside
        # itself and its no-arg baseline resolves the repo root two
        # levels up from the script, so copy both pin files into the
        # check tree in that layout.
        bun-pin-sync-tests =
          pkgs.runCommandLocal "bun-pin-sync-tests"
            {
              nativeBuildInputs = [ pkgs.python3 ];
            }
            ''
              mkdir -p pkgs/bun-bin .github/workflows
              cp ${./pkgs/bun-bin/check-pin-sync.py} pkgs/bun-bin/check-pin-sync.py
              cp ${./pkgs/bun-bin/check-pin-sync.test.py} pkgs/bun-bin/check-pin-sync.test.py
              cp ${./pkgs/bun-bin/hashes.json} pkgs/bun-bin/hashes.json
              cp ${./.github/workflows/buffer-backup-renovate-bun-sync.yml} .github/workflows/buffer-backup-renovate-bun-sync.yml
              python3 pkgs/bun-bin/check-pin-sync.test.py
              touch $out
            '';
        # #414: plugins/warp-import.test.mjs (25 tests) was run by nothing —
        # no workflow, recipe, or check referenced it. Run every
        # plugins/*.test.mjs here so `nix flake check` fails when a plugin
        # regresses; the glob picks up future suites (e.g. sidepulse) too.
        plugins-tests =
          pkgs.runCommandLocal "plugins-tests"
            {
              nativeBuildInputs = [ pkgs.nodejs ];
              src = ./plugins;
            }
            ''
              cp -r $src ./plugins-src
              chmod -R u+w ./plugins-src
              cd ./plugins-src
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              found=0
              for t in *.test.mjs; do
                [ -e "$t" ] || continue
                found=1
                echo "== $t"
                node "$t"
              done
              if [ "$found" -eq 0 ]; then
                echo "no plugins/*.test.mjs suites found" >&2
                exit 1
              fi
              touch $out
            '';
        # #472: opencode-db-pids.test.sh (the #467 regression test for the
        # opencode_pids() guard in opencode-db-vacuum.sh /
        # opencode-db-prune.sh) was run by nothing — no recipe, check, or
        # workflow referenced it, so the guard that regressed in
        # #461/#465/#467 could regress again silently. The test is
        # self-contained (it shadows ps with synthetic rows; no opencode
        # binary, lsof, or database needed), so run it here.
        opencode-db-pids-tests =
          pkgs.runCommandLocal "opencode-db-pids-tests"
            {
              nativeBuildInputs = [ pkgs.bash ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-vacuum.sh $src/opencode-db-prune.sh $src/opencode-db-pids.test.sh ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              chmod u+w ./opencode-db-vacuum.sh ./opencode-db-prune.sh ./opencode-db-pids.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-db-pids.test.sh
              touch $out
            '';
        # #941: opencode-db-args.test.sh pins the argument parsers of (extended to backup-prune, folded into the shared probe matrix, #1147)
        # opencode-db-vacuum.sh / opencode-db-prune.sh — every
        # value-taking option must reject an empty or flag-shaped value
        # naming the option, so "--into --dry-run" cannot swallow the
        # flag and run a real vacuum (the #930 pattern). Self-contained
        # apart from sqlite3 for the vacuum happy-path control.
        opencode-db-args-tests =
          pkgs.runCommandLocal "opencode-db-args-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.sqlite
              ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-vacuum.sh $src/opencode-db-prune.sh $src/opencode-db-backup-prune.sh $src/opencode-db-args.test.sh ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              chmod u+w ./opencode-db-vacuum.sh ./opencode-db-prune.sh ./opencode-db-backup-prune.sh ./opencode-db-args.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-db-args.test.sh
              touch $out
            '';
        # #1050: opencode-db-prune.test.sh drives the real prune script
        # against fixture SQLite DBs -- dry-run eligibility, the batched
        # delete itself (newest/non-snapshot/sync-owned rows survive),
        # --archive + --restore-session round-trip, integrity asserted.
        # The destructive core had no executable test while every
        # peripheral property (args #941, pids #472) was pinned. Needs
        # gzip for the archive compressor fallback (zstd preferred when
        # present) and coreutils for date/mktemp.
        opencode-db-prune-tests =
          pkgs.runCommandLocal "opencode-db-prune-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.sqlite
                pkgs.gzip
                pkgs.coreutils
              ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-vacuum.sh $src/opencode-db-prune.sh $src/opencode-db-prune.test.sh ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              chmod u+w ./opencode-db-vacuum.sh ./opencode-db-prune.sh ./opencode-db-prune.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-db-prune.test.sh
              touch $out
            '';
        # #1088: opencode-db-vacuum.test.sh drives the real vacuum script
        # against fixture SQLite DBs -- the VACUUM INTO + verify + swap
        # core (shrink, 500/500/500/10 row counts, .old backup, integrity),
        # the existing-.old/.compact refusals, --no-backup, a custom
        # --into, and --auto-vacuum reading back INCREMENTAL. The args and
        # pids suites pin only the parser and the pid guard; the
        # destructive swap had no executable test.
        opencode-db-vacuum-tests =
          pkgs.runCommandLocal "opencode-db-vacuum-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.sqlite
                pkgs.coreutils
              ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-vacuum.sh $src/opencode-db-vacuum.test.sh ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              chmod u+w ./opencode-db-vacuum.sh ./opencode-db-vacuum.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-db-vacuum.test.sh
              touch $out
            '';
        # #659: opencode-db-backup-prune.test.sh pins the retention for
        # `just opencode-db-backup`'s timestamped backup dirs -- dry-run
        # lists exactly the beyond-keep set and deletes nothing, --apply
        # deletes exactly that set, the newest backup is never listed,
        # and non-timestamp entries are never touched. Self-contained:
        # fixture dirs only, no database needed.
        opencode-db-backup-prune-tests =
          pkgs.runCommandLocal "opencode-db-backup-prune-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.coreutils
              ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-backup-prune.sh $src/opencode-db-backup-prune.test.sh ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              chmod u+w ./opencode-db-backup-prune.sh ./opencode-db-backup-prune.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-db-backup-prune.test.sh
              touch $out
            '';
        # #557: var-folders-clean.test.sh (the #553 test for clean.sh's
        # /private/var/folders sweep) was run by nothing, tested a pasted
        # copy of the loops instead of the shipped logic, and exited 0 on
        # failure. The sweep now lives in var-folders-clean.sh (which
        # clean.sh calls), the test drives that script, and failures exit
        # non-zero — run it here, by name, like opencode-db-pids above.
        var-folders-clean-tests =
          pkgs.runCommandLocal "var-folders-clean-tests"
            {
              nativeBuildInputs = [ pkgs.bash ];
              src = inputs.self;
            }
            ''
              cp $src/var-folders-clean.sh $src/var-folders-clean.test.sh ./
              chmod u+w ./var-folders-clean.sh ./var-folders-clean.test.sh
              bash ./var-folders-clean.test.sh
              touch $out
            '';
        # #883/#886: symlinker.test.sh drives the real symlinker.sh through
        # run -> undo round-trips — colon-named conflicts (BACKUP records),
        # legacy v2 colon-format undo files, the irrecoverable-undo exit
        # code, and same-second run-id collisions (via a stubbed date) — so
        # the undo safety net cannot regress silently.
        symlinker-tests =
          pkgs.runCommandLocal "symlinker-tests"
            {
              nativeBuildInputs = [ pkgs.bash ];
              src = inputs.self;
            }
            ''
              cp $src/symlinker.sh $src/symlinker.test.sh ./
              chmod u+w ./symlinker.sh ./symlinker.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./symlinker.test.sh
              touch $out
            '';
        # #967: symlink-scripts.test.sh drives the real symlink-commands
        # and symlink-plugins — in place against the repo's real
        # commands/ and plugins/, and as verbatim copies in a fixture
        # tree for the edge cases — pinning fresh link, already-linked
        # no-op, identical-file replacement (not a warning), conflict /
        # elsewhere / broken-symlink preservation with exit 1, --dry-run
        # changing nothing, unknown option exit 2, and the glob
        # divergence (commands links every regular file; plugins only
        # *.{js,ts}, so *.test.mjs is never linked). Neither script had
        # a suite or a check before this.
        symlink-scripts-tests =
          pkgs.runCommandLocal "symlink-scripts-tests"
            {
              nativeBuildInputs = [ pkgs.bash ];
              src = inputs.self;
            }
            ''
              cp $src/symlink-commands $src/symlink-plugins $src/symlink-scripts.test.sh ./
              cp -r $src/commands $src/plugins ./
              chmod u+w ./symlink-commands ./symlink-plugins ./symlink-scripts.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./symlink-scripts.test.sh
              touch $out
            '';
        # #983: search.test.sh drives the real search.sh with a stub
        # brew — PR #959's #957 fix (a failed `brew list --cask` exits
        # 1 naming the step instead of reporting every app) shipped
        # with no test and no check. The suite pins failing/missing
        # brew (exit 1, step named, no findings), working brew (only
        # non-cask apps reported), empty list (all apps, exit 0), and
        # `brew list` running exactly once per invocation.
        search-tests =
          pkgs.runCommandLocal "search-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.coreutils
                pkgs.findutils
                pkgs.gnugrep
                pkgs.gnused
              ];
              src = inputs.self;
            }
            ''
              cp $src/search.sh $src/search.test.sh ./
              chmod u+w ./search.sh ./search.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./search.test.sh
              touch $out
            '';
        # #1167: full-clean.test.sh drives the real full-clean.sh
        # with its steps (./s, prune, vacuum) and PATH commands
        # (sudo, nix-collect-garbage, nix, df) stubbed to log every
        # call -- the script has no argument parser, so --help,
        # --bogus and --dry-run used to run the full destructive
        # sequence and exit 0. The suite pins those three (no stub
        # called, correct exits, argument named) plus a no-argument
        # control that runs the whole stubbed sequence.
        full-clean-tests =
          pkgs.runCommandLocal "full-clean-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.coreutils
                pkgs.gnugrep
              ];
              src = inputs.self;
            }
            ''
              cp $src/full-clean.sh $src/full-clean.test.sh ./
              chmod u+w ./full-clean.sh ./full-clean.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./full-clean.test.sh
              touch $out
            '';
        # #1190: rebuild-backup-zed.test.sh drives the real
        # rebuild.sh and backup-zed.sh under zsh (their interpreter)
        # -- PR #1169 added their no-argument guards alongside
        # full-clean.sh's, but only the bash script got a test. The
        # suite stubs git/nix/simple-rebuild.sh for rebuild.sh and
        # uses a fixture HOME for backup-zed.sh, pinning --help/-h
        # (exit 0, usage, no side effects), unknown arguments
        # (exit 2, argument named, no side effects), and no-argument
        # controls for both scripts.
        rebuild-backup-zed-tests =
          pkgs.runCommandLocal "rebuild-backup-zed-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.zsh
                pkgs.coreutils
                pkgs.gnugrep
                pkgs.findutils
              ];
              src = inputs.self;
            }
            ''
              cp $src/rebuild.sh $src/backup-zed.sh $src/rebuild-backup-zed.test.sh ./
              chmod u+w ./rebuild.sh ./backup-zed.sh ./rebuild-backup-zed.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./rebuild-backup-zed.test.sh
              touch $out
            '';
        # #940: git-discover-repos.test.sh drives the real
        # git-discover-repos through bad, mixed, regular-file, all-valid,
        # --count and --dry-run --path runs — a bad --path used to exit 0,
        # so exit-code-checking callers (clean.sh) saw a successful empty
        # scan instead of a bad argument. It also pins PR #939's
        # flag-shaped-value guards (#951): --path/--depth/--find/--user
        # with a flag-shaped next token exit 2 naming the option.
        git-discover-repos-tests =
          pkgs.runCommandLocal "git-discover-repos-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.git
              ];
              src = inputs.self;
            }
            ''
              cp $src/git-discover-repos $src/git-discover-repos.test.sh ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              chmod u+w ./git-discover-repos ./git-discover-repos.test.sh
              patchShebangs ./git-discover-repos
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./git-discover-repos.test.sh
              touch $out
            '';
        # #951: clean.test.sh drives the real clean.sh parser -- PR #939
        # made its value-taking options reject a flag-shaped next token
        # ("--path --dry-run" used to swallow --dry-run and run the
        # cleanup for real), but added no test for clean.sh. The suite
        # pins --path/--depth/--docker-age rejection (exit 2, option
        # named, no side effects) plus a valid-value control that runs
        # only the unknown-scratch sweep over an empty isolated HOME.
        clean-tests =
          pkgs.runCommandLocal "clean-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.git
              ];
              src = inputs.self;
            }
            ''
              cp $src/clean.sh $src/clean.test.sh $src/git-discover-repos $src/var-folders-clean.sh ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              cp $src/lib/outcome-probe.sh lib/
              chmod u+w ./clean.sh ./clean.test.sh ./git-discover-repos ./var-folders-clean.sh
              patchShebangs ./git-discover-repos
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./clean.test.sh
              touch $out
            '';
        # #1077: pkgs/adopt/adopt.test.sh was run by nothing — no flake
        # check, workflow, recipe, or package referenced it, so a
        # regression in adopt.sh (which edits flake.nix) landed silently.
        # The suite drives the real adopt.sh against a scratch flake,
        # including the BWK-strict awk shim cases for #1074. Run it here
        # (checks are eachSystem, so aarch64-darwin gets it too).
        adopt-tests =
          pkgs.runCommandLocal "adopt-tests"
            {
              nativeBuildInputs = [ pkgs.bash ];
              src = ./pkgs/adopt;
            }
            ''
              cp -r $src ./adopt-src
              chmod -R u+w ./adopt-src
              cd ./adopt-src
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./adopt.test.sh
              touch $out
            '';
        # #951: git-maintain-repos.test.sh drives the real
        # git-maintain-repos parser -- PR #939's flag-shaped guards for
        # --path/--depth/--jobs/--pack-threads/--reflog-expire had no
        # test. The suite pins rejection (exit 2, option named, no side
        # effects) plus a --dry-run valid-value control over a fixture
        # repo.
        # #978: the check copies must be shebang-patched (here and in the
        # discover/clean/spotlight checks): git-maintain-repos starts worker
        # copies of itself via xargs, and its consumers execute the sibling
        # git-discover-repos directly. The nix build sandbox has no
        # /usr/bin/env, so an unpatched `#!/usr/bin/env bash` copy cannot run.
        git-maintain-repos-tests =
          pkgs.runCommandLocal "git-maintain-repos-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.git
              ];
              src = inputs.self;
            }
            ''
              cp $src/git-maintain-repos $src/git-maintain-repos.test.sh $src/git-discover-repos ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              chmod u+w ./git-maintain-repos ./git-maintain-repos.test.sh ./git-discover-repos
              patchShebangs ./git-discover-repos ./git-maintain-repos
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./git-maintain-repos.test.sh
              touch $out
            '';
        # #951: spotlight-exclude-artifacts.test.sh drives the real
        # spotlight-exclude-artifacts parser -- PR #939's flag-shaped
        # guards for --path/--depth had no test ("--path --dry-run" used
        # to run the real marking pass). The suite pins rejection
        # (exit 2, option named, no markers written) plus a --dry-run
        # valid-value control over a fixture repo.
        spotlight-exclude-artifacts-tests =
          pkgs.runCommandLocal "spotlight-exclude-artifacts-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.git
              ];
              src = inputs.self;
            }
            ''
              cp $src/spotlight-exclude-artifacts $src/spotlight-exclude-artifacts.test.sh $src/git-discover-repos ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              cp $src/lib/arg-probe.sh lib/
              chmod u+w ./spotlight-exclude-artifacts ./spotlight-exclude-artifacts.test.sh ./git-discover-repos
              patchShebangs ./git-discover-repos
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./spotlight-exclude-artifacts.test.sh
              touch $out
            '';
        # #453: opencode-tmp-guard.test.sh extracts the real guard script
        # from opencode-tmp-guard.nix and drives it against a fixture temp
        # tree -- the depth-4 scan used to miss worktree .git pointer files
        # and repos at depth >= 5 (the metadata dirhelper sweeps first),
        # node_modules .git dirs were walked, and a regular file at
        # $TMPDIR/opencode made every run a silent no-op.
        opencode-tmp-guard-tests =
          pkgs.runCommandLocal "opencode-tmp-guard-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.python3
              ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-tmp-guard.nix $src/opencode-tmp-guard.test.sh ./
              chmod u+w ./opencode-tmp-guard.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-tmp-guard.test.sh
              touch $out
            '';
        # #1432: exercise the real OpenChamber deploy activation body with a
        # missing-after-poll password, unchanged-password control, and real
        # password-rotation fixture. The missing path must fail before key
        # computation or any service/marker mutation.
        openchamber-deploy-tests =
          pkgs.runCommandLocal "openchamber-deploy-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.coreutils
                pkgs.gawk
                pkgs.gnugrep
                pkgs.python3
              ];
              src = inputs.self;
            }
            ''
              cp $src/openchamber.nix $src/openchamber-deploy.test.sh ./
              chmod u+w ./openchamber-deploy.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./openchamber-deploy.test.sh
              touch $out
            '';
        # #659: opencode-prune-report.test.sh extracts the real report
        # script from opencode-prune-report.nix and drives it against stub
        # pruners -- both invoked, clean.sh gets --dry-run and
        # --no-git-maintenance, nothing ever gets --apply or
        # --prune-unknown-scratch, and one pruner failing does not hide the
        # other's report. #1436: it also rebuilds the tools derivation's
        # store layout from the derivation's own cp/chmod lines and drives
        # the REAL store-copied clean.sh against a scratch HOME, so an
        # incomplete derivation (a missing git-discover-repos cost the
        # scheduled report a fatal 127 before the scratch section) fails
        # here instead of on the Mac.
        opencode-prune-report-tests =
          pkgs.runCommandLocal "opencode-prune-report-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.bc
                pkgs.git
                pkgs.python3
              ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-prune-report.nix $src/opencode-prune-report.test.sh ./
              cp $src/clean.sh $src/opencode-db-backup-prune.sh $src/git-discover-repos $src/git-maintain-repos $src/var-folders-clean.sh $src/casks.nix $src/masApps.nix ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              chmod u+w ./opencode-prune-report.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-prune-report.test.sh
              touch $out
            '';
        # #945: install-vscode-patch-agent.test.sh drives the real
        # installer with a scratch HOME and a stub launchctl -- a missing
        # LaunchAgents dir, a failing launchctl load, and a blocked dir
        # all used to print "Installed and loaded" and exit 0, so the
        # patch agent could silently never be installed.
        install-vscode-patch-agent-tests =
          pkgs.runCommandLocal "install-vscode-patch-agent-tests"
            {
              nativeBuildInputs = [ pkgs.bash ];
              src = inputs.self;
            }
            ''
              cp $src/install-vscode-patch-agent.sh $src/install-vscode-patch-agent.test.sh ./
              chmod u+w ./install-vscode-patch-agent.sh ./install-vscode-patch-agent.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./install-vscode-patch-agent.test.sh
              touch $out
            '';
        # #969: patch-vscode-insiders.test.sh drives the real
        # patch-vscode-insiders.sh against a fixture app tree via the
        # VSCODE_INSIDERS_APP_DIR seam -- the #925 write-only-when-changed
        # rule and the #925/#938 missing-target / missing-JS exit-1
        # contracts were verified only by hand (with the /Applications
        # prefix retargeted in scratch copies), so they could regress
        # silently.
        patch-vscode-insiders-tests =
          pkgs.runCommandLocal "patch-vscode-insiders-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.python3
              ];
              src = inputs.self;
            }
            ''
              cp $src/patch-vscode-insiders.sh $src/patch-vscode-insiders.test.sh ./
              chmod u+w ./patch-vscode-insiders.sh ./patch-vscode-insiders.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./patch-vscode-insiders.test.sh
              touch $out
            '';
        # #965: mcp-clients.test.py -- the regression suite for
        # mcp-clients.py, the every-switch enforcement point that keeps
        # Claude/Codex/Zed MCP config mcpx-only -- was run by nothing:
        # no flake check, workflow, or justfile recipe referenced it,
        # unlike every neighbouring suite. Run the real suite here so
        # `nix flake check` fails when an enforcement spelling/parsing
        # fix (#905/#906/#929/#944/#950/#956/#990) regresses.
        mcp-clients-tests =
          pkgs.runCommandLocal "mcp-clients-tests"
            {
              nativeBuildInputs = [ pkgs.python3 ];
              src = inputs.self;
            }
            ''
              cp $src/mcp-clients.py $src/mcp-clients.test.py ./
              chmod u+w ./mcp-clients.py ./mcp-clients.test.py
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              python3 ./mcp-clients.test.py
              touch $out
            '';
        # #1449: exercise the actual home-manager activation wrapper and
        # mcp-clients.py with a scratch HOME and a service-event stub. A
        # partial enforcement must be durably reported without blocking the
        # rest of activation; failure to record it must still fail activation.
        mcp-clients-activation-tests =
          pkgs.runCommandLocal "mcp-clients-activation-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.coreutils
                pkgs.gnugrep
                pkgs.gnused
                pkgs.python3
              ];
              src = inputs.self;
            }
            ''
              cp $src/mcp-clients-activation.sh $src/mcp-clients-activation.test.sh $src/mcp-clients.py ./
              chmod u+w ./mcp-clients-activation.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./mcp-clients-activation.test.sh
              touch $out
            '';
        # #1454: activation must durably record failures from the root and
        # primary-user helper runs without hiding helper output or blocking
        # later activation entries when the event write succeeds.
        system-activation-helper-tests =
          pkgs.runCommandLocal "system-activation-helper-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.coreutils
                pkgs.gnugrep
              ];
              src = inputs.self;
            }
            ''
              cp $src/system-activation-helper.sh $src/system-activation-helper.test.sh ./
              chmod u+w ./system-activation-helper.test.sh
              bash ./system-activation-helper.test.sh
              touch $out
            '';
        # #907: opencode-share.test.sh drives the real opencode-share.sh
        # in --dry-run mode through [good, missing, good], all-good, and
        # all-fail target lists — the post-increment error counter used
        # to abort the run at the first failing target under `set -e`,
        # so multi-target runs cannot silently skip later targets again.
        opencode-share-tests =
          pkgs.runCommandLocal "opencode-share-tests"
            {
              nativeBuildInputs = [ pkgs.bash ];
              src = ./pkgs/opencode-share;
            }
            ''
              cp $src/opencode-share.sh $src/opencode-share.test.sh ./
              chmod u+w ./opencode-share.sh ./opencode-share.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-share.test.sh
              touch $out
            '';
        # #927/#926: brew-repair.test.sh drives the real brew-repair.sh
        # against a call-logging stub brew — '-s --dry-run' / '-s -n' used
        # to swallow the option as the skip value and run real reinstalls,
        # and a failed/unparseable `brew info` scan used to exit 0 as
        # "All cask apps are healthy!", so the parse errors, the scan
        # failures, and the valid-value/healthy controls cannot regress
        # silently.
        brew-repair-tests =
          pkgs.runCommandLocal "brew-repair-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.python3
              ];
              src = ./pkgs/brew-repair;
            }
            ''
              cp $src/brew-repair.sh $src/brew-repair.test.sh ./
              chmod u+w ./brew-repair.sh ./brew-repair.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./brew-repair.test.sh
              touch $out
            '';
        # #1287: code-install-extensions.test.sh drives the real
        # code-install-extensions.sh with a stub `code` — a failed
        # install used to exit 0 unless it was the script's last line,
        # so the all-succeed, middle-failure, last-line-failure, and
        # two-failure shapes (exit status, failure summary naming and
        # counting the failures, and every extension still attempted
        # in order) cannot regress silently.
        code-install-extensions-tests =
          pkgs.runCommandLocal "code-install-extensions-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.coreutils
                pkgs.gnugrep
                pkgs.gnused
                pkgs.gawk
              ];
            }
            ''
              cp ${./code-install-extensions.sh} code-install-extensions.sh
              cp ${./code-install-extensions.test.sh} code-install-extensions.test.sh
              cp ${./config.toml} config.toml
              cp ${./vsc-extensions.txt} vsc-extensions.txt
              chmod u+w code-install-extensions.sh code-install-extensions.test.sh config.toml vsc-extensions.txt
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./code-install-extensions.test.sh
              touch $out
            '';
        # #1435: pm-dispatch-launch.test.sh drives the real launch
        # script extracted from agents.yml with a stub curl and the
        # real jq -- payload construction, the four GITHUB_OUTPUT keys
        # the Summarize step consumes, and the prompt-fetch / OIDC /
        # non-2xx failure messages and exit codes cannot regress or
        # drift between the three dispatch tasks (there is now one
        # implementation, but its contract is pinned here).
        pm-dispatch-launch-tests =
          pkgs.runCommandLocal "pm-dispatch-launch-tests"
            {
              nativeBuildInputs = [
                pkgs.bash
                pkgs.coreutils
                pkgs.jq
              ];
              src = inputs.self;
            }
            ''
              cp $src/.github/scripts/pm-dispatch-launch.sh $src/.github/scripts/pm-dispatch-launch.test.sh ./
              chmod u+w ./pm-dispatch-launch.sh ./pm-dispatch-launch.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./pm-dispatch-launch.test.sh
              touch $out
            '';
        # skill-tests (the archive/skills/*/SKILL.test.ts suites, #478)
        # retired 2026-11-07: the vendored skills moved to dezren39/cairn
        # (.agents/skills-examples/) per developing-today/code#5982, and
        # their suites moved with them.
      }
      // pkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isDarwin {
        sidepulse = (mkPackages pkgs).sidepulse;
        sidepulse-wrapper =
          let
            selfPkgs = mkPackages pkgs;
          in
          pkgs.runCommandLocal "sidepulse-wrapper-check"
            {
              nativeBuildInputs = [
                pkgs.coreutils
                pkgs.diffutils
                pkgs.gnugrep
              ];
            }
            ''
              export HOME="$TMPDIR/home"

              ${selfPkgs.sidepulse}/bin/sidepulse --setup opencode --dry-run --no-status-bar > opencode.log
              grep -F 'opencode: would update' opencode.log

              ${selfPkgs.sidepulse}/bin/sidepulse setup opencode --dry-run --no-status-bar > opencode-command.log
              grep -F 'opencode: would update' opencode-command.log

              ${selfPkgs.sidepulse-unwrapped}/bin/sidepulse agent-monitor install opencode
              cp "$HOME/.config/opencode/plugins/sidepulse.js" first-opencode.js
              ${selfPkgs.sidepulse-unwrapped}/bin/sidepulse agent-monitor install opencode
              cmp first-opencode.js "$HOME/.config/opencode/plugins/sidepulse.js"
              # The OpenCode v2 loader rejects v1's bare exported function.
              grep -F 'export default {' first-opencode.js

              touch "$out"
            '';
      }
      // pkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
        # #343: the mcpx modules, evaluated end to end on Linux. Full
        # module-system evaluation is the equivalence proof available
        # here: the NixOS unit must carry the launchd agent's
        # semantics (start at login, keep running, 20 s stop window,
        # $HOME working directory, explicit --config) and the
        # rendered config.json must carry settings verbatim plus
        # servers as mcpServers — for both the NixOS and the
        # home-manager module. A module edit that breaks evaluation
        # or drops a field fails here instead of at switch time.
        mcpx-nixos-module =
          let
            eval = inputs.nixpkgs.lib.nixosSystem {
              system = pkgs.stdenv.hostPlatform.system;
              modules = [
                inputs.self.nixosModules.mcpx
                {
                  services.mcpx = {
                    enable = true;
                    settings.daemon.autostart = false;
                    # #1244: a settings-written mcpServers must survive
                    # (merged per-server; servers wins for `example`).
                    settings.mcpServers.from-settings = {
                      command = "/bin/settings-only";
                    };
                    settings.mcpServers.example = {
                      command = "/bin/should-lose";
                      args = [ "settings" ];
                    };
                    servers.example = {
                      command = "/bin/true";
                      args = [ "mcp" ];
                    };
                    environment.MCPX_TEST_MARKER = "present";
                    environmentFile = "/home/mcpx-test/mcpx.env";
                  };
                  # The minimum a nixosSystem evaluation asks for.
                  boot.loader.grub.enable = false;
                  fileSystems."/" = {
                    device = "/dev/null";
                    fsType = "ext4";
                  };
                  system.stateVersion = "25.11";
                }
              ];
            };
            unit = eval.config.systemd.user.services.mcpx;
          in
          pkgs.runCommandLocal "mcpx-nixos-module-check"
            {
              execStart = unit.serviceConfig.ExecStart;
              restart = unit.serviceConfig.Restart;
              timeoutStopSec = toString unit.serviceConfig.TimeoutStopSec;
              workingDirectory = unit.serviceConfig.WorkingDirectory;
              marker = unit.environment.MCPX_TEST_MARKER;
              wantedBy = pkgs.lib.concatStringsSep " " unit.wantedBy;
              environmentFile = unit.serviceConfig.EnvironmentFile;
              mcpxConfig = eval.config.environment.sessionVariables.MCPX_CONFIG;
              configFile = eval.config.services.mcpx.configFile;
              nativeBuildInputs = [ pkgs.jq ];
            }
            ''
              case "$execStart" in
              *"/bin/mcpx daemon --config "*) ;;
              *)
                echo "ExecStart lost the daemon --config form: $execStart" >&2
                exit 1
                ;;
              esac
              [ "$restart" = "always" ] || { echo "Restart=$restart, want always" >&2; exit 1; }
              [ "$timeoutStopSec" = "20" ] || { echo "TimeoutStopSec=$timeoutStopSec, want 20" >&2; exit 1; }
              [ "$workingDirectory" = "%h" ] || { echo "WorkingDirectory=$workingDirectory, want %h" >&2; exit 1; }
              [ "$wantedBy" = "default.target" ] || { echo "wantedBy=$wantedBy, want default.target" >&2; exit 1; }
              [ "$marker" = "present" ] || { echo "environment did not reach the unit" >&2; exit 1; }
              [ "$environmentFile" = "/home/mcpx-test/mcpx.env" ] || { echo "EnvironmentFile=$environmentFile, want the runtime file path" >&2; exit 1; }
              [ "$mcpxConfig" = "$configFile" ] || { echo "MCPX_CONFIG=$mcpxConfig does not name the rendered config $configFile" >&2; exit 1; }
              jq -e '.daemon.autostart == false and .mcpServers.example.command == "/bin/true" and .mcpServers.example.args == ["mcp"] and .mcpServers."from-settings".command == "/bin/settings-only"' "$configFile" > /dev/null \
                || { echo "rendered config.json mismatch:" >&2; cat "$configFile" >&2; exit 1; }
              touch $out
            '';
        # #1244: settings.mcpServers with servers unset must not be
        # erased by the empty servers default.
        mcpx-nixos-settings-mcpservers =
          let
            eval = inputs.nixpkgs.lib.nixosSystem {
              system = pkgs.stdenv.hostPlatform.system;
              modules = [
                inputs.self.nixosModules.mcpx
                {
                  services.mcpx = {
                    enable = true;
                    settings.mcpServers.only = {
                      command = "/bin/only-from-settings";
                      args = [ "mcp" ];
                    };
                  };
                  boot.loader.grub.enable = false;
                  fileSystems."/" = {
                    device = "/dev/null";
                    fsType = "ext4";
                  };
                  system.stateVersion = "25.11";
                }
              ];
            };
          in
          pkgs.runCommandLocal "mcpx-nixos-settings-mcpservers-check"
            {
              configFile = eval.config.services.mcpx.configFile;
              nativeBuildInputs = [ pkgs.jq ];
            }
            ''
              jq -e '.mcpServers.only.command == "/bin/only-from-settings" and .mcpServers.only.args == ["mcp"] and (.mcpServers | length) == 1' "$configFile" > /dev/null \
                || { echo "settings.mcpServers was not preserved:" >&2; cat "$configFile" >&2; exit 1; }
              touch $out
            '';
        mcpx-home-manager-module =
          let
            eval = inputs.home-manager.lib.homeManagerConfiguration {
              inherit pkgs;
              modules = [
                inputs.self.homeManagerModules.mcpx
                {
                  services.mcpx = {
                    enable = true;
                    settings.daemon.autostart = false;
                    servers.example = {
                      command = "/bin/true";
                      args = [ "mcp" ];
                    };
                    environment.MCPX_TEST_MARKER = "present";
                    environmentFile = "/home/mcpx-test/mcpx.env";
                  };
                  home = {
                    username = "mcpx-test";
                    homeDirectory = "/home/mcpx-test";
                    stateVersion = "25.11";
                  };
                }
              ];
            };
            service = eval.config.systemd.user.services.mcpx.Service;
          in
          pkgs.runCommandLocal "mcpx-home-manager-module-check"
            {
              execStart = service.ExecStart;
              restart = service.Restart;
              timeoutStopSec = toString service.TimeoutStopSec;
              wantedBy = pkgs.lib.concatStringsSep " " eval.config.systemd.user.services.mcpx.Install.WantedBy;
              marker =
                if builtins.elem "MCPX_TEST_MARKER=present" service.Environment then "present" else "missing";
              environmentFile = service.EnvironmentFile;
              mcpxConfig = eval.config.home.sessionVariables.MCPX_CONFIG;
              xdgSource = eval.config.xdg.configFile."mcpx/config.json".source;
              configFile = eval.config.services.mcpx.configFile;
              nativeBuildInputs = [ pkgs.jq ];
            }
            ''
              case "$execStart" in
              *"/bin/mcpx daemon --config "*) ;;
              *)
                echo "ExecStart lost the daemon --config form: $execStart" >&2
                exit 1
                ;;
              esac
              [ "$restart" = "always" ] || { echo "Restart=$restart, want always" >&2; exit 1; }
              [ "$timeoutStopSec" = "20" ] || { echo "TimeoutStopSec=$timeoutStopSec, want 20" >&2; exit 1; }
              [ "$wantedBy" = "default.target" ] || { echo "WantedBy=$wantedBy, want default.target" >&2; exit 1; }
              [ "$marker" = "present" ] || { echo "environment did not reach the unit" >&2; exit 1; }
              [ "$environmentFile" = "/home/mcpx-test/mcpx.env" ] || { echo "EnvironmentFile=$environmentFile, want the runtime file path" >&2; exit 1; }
              [ "$mcpxConfig" = "$configFile" ] || { echo "MCPX_CONFIG=$mcpxConfig does not name the rendered config $configFile" >&2; exit 1; }
              [ "$xdgSource" = "$configFile" ] || { echo "xdg config source=$xdgSource is not the rendered config $configFile" >&2; exit 1; }
              jq -e '.daemon.autostart == false and .mcpServers.example.command == "/bin/true" and .mcpServers.example.args == ["mcp"]' "$configFile" > /dev/null \
                || { echo "rendered config.json mismatch:" >&2; cat "$configFile" >&2; exit 1; }
              touch $out
            '';
        # code#5362: the fleet-brokers home-manager module must render
        # all three fleet service units pointing at the flake's own packages.
        fleet-brokers-home-manager-module =
          # The module's service config is guarded by hostPlatform.isLinux,
          # so the check must not evaluate it on Darwin (codex P1 on #1459).
          if !pkgs.stdenv.hostPlatform.isLinux then
            pkgs.runCommandLocal "fleet-brokers-home-manager-module-check" { } "touch $out"
          else
            let
              selfPkgs = mkPackages pkgs;
              eval = inputs.home-manager.lib.homeManagerConfiguration {
                inherit pkgs;
                modules = [
                  inputs.self.homeManagerModules.fleet-brokers
                  {
                    services.fleet-brokers = {
                      enable = true;
                      package = selfPkgs.fleet-brokers;
                      laptopAgentPackage = selfPkgs.laptop-agent;
                      # nixpkgs' bws is unfree; the check only needs a
                      # package on the unit PATH, so a stub stands in.
                      bwsPackage = pkgs.coreutils;
                    };
                    home = {
                      username = "fleet-test";
                      homeDirectory = "/home/fleet-test";
                      stateVersion = "25.11";
                    };
                  }
                ];
              };
              qb = eval.config.systemd.user.services.queue-broker.Service;
              sb = eval.config.systemd.user.services.secrets-broker.Service;
              la = eval.config.systemd.user.services.laptop-agent.Service;
            in
            pkgs.runCommandLocal "fleet-brokers-home-manager-module-check"
              {
                qbExec = qb.ExecStart;
                sbExec = sb.ExecStart;
                sbPath = pkgs.lib.concatStringsSep ":" sb.Environment;
                bwsBin = "${pkgs.coreutils}/bin";
                laExec = la.ExecStart;
                wantedBy = pkgs.lib.concatStringsSep " " eval.config.systemd.user.services.queue-broker.Install.WantedBy;
              }
              ''
                case "$qbExec" in
                *"/bin/queue-broker") ;;
                *)
                  echo "queue-broker ExecStart wrong: $qbExec" >&2
                  exit 1
                  ;;
                esac
                case "$sbExec" in
                *"/bin/secrets-broker") ;;
                *)
                  echo "secrets-broker ExecStart wrong: $sbExec" >&2
                  exit 1
                  ;;
                esac
                case "$sbPath" in
                *"$bwsBin"*) ;;
                *)
                  echo "secrets-broker PATH lacks bwsPackage: $sbPath" >&2
                  exit 1
                  ;;
                esac
                case "$laExec" in
                *"/bin/laptop-agent") ;;
                *)
                  echo "laptop-agent ExecStart wrong: $laExec" >&2
                  exit 1
                  ;;
                esac
                [ "$wantedBy" = "default.target" ] || { echo "WantedBy=$wantedBy, want default.target" >&2; exit 1; }
                touch $out
              '';
        # code#5343: the laptop-agent must reject an empty token at startup
        # and authenticate requests by header (PR #1459 codex P1).
        laptop-agent-tests =
          pkgs.runCommandLocal "laptop-agent-tests"
            {
              nativeBuildInputs = [ pkgs.python3 ];
            }
            ''
              cp -r ${./pkgs/laptop-agent/src} src
              cp -r ${./pkgs/laptop-agent/tests} tests
              python3 -m unittest discover -s tests -v
              touch $out
            '';
      }
    );
    apps = eachSystem (
      pkgs:
      let
        selfPkgs = mkPackages pkgs;
      in
      ({
        flake-tidy = {
          type = "app";
          program = "${selfPkgs.flake-tidy}/bin/flake-tidy";
        };
        adopt = {
          type = "app";
          program = "${selfPkgs.adopt}/bin/adopt";
        };
        mcpx = {
          type = "app";
          program = "${selfPkgs.mcpx}/bin/mcpx";
        };
        opencode = {
          type = "app";
          program = "${selfPkgs.opencode}/bin/opencode";
        };
        openchamber = {
          type = "app";
          program = "${selfPkgs.openchamber}/bin/openchamber";
        };
        opencode-share = {
          type = "app";
          program = "${selfPkgs.opencode-share}/bin/opencode-share";
        };
        symlinker = {
          type = "app";
          program = "${selfPkgs.symlinker}/bin/symlinker";
        };
      })
      // pkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isDarwin {
        sidepulse = {
          type = "app";
          program = "${selfPkgs.sidepulse}/bin/sidepulse";
        };
        sidepulse-setup = {
          type = "app";
          program = "${selfPkgs.sidepulse-setup}/bin/sidepulse-setup";
        };
      }
    );
    devShells = eachSystem (
      pkgs:
      let
        selfPkgs = mkPackages pkgs;
      in
      {
        default = pkgs.mkShell {
          packages = [
            selfPkgs.opencode
            selfPkgs.flake-tidy
            selfPkgs.symlinker
            inputs.just.packages.${pkgs.stdenv.hostPlatform.system}.default
            pkgs.nixfmt
            pkgs.git
            pkgs.gh
          ]
          ++ pkgs.lib.optionals pkgs.stdenv.hostPlatform.isDarwin [
            selfPkgs.sidepulse
            selfPkgs.sidepulse-setup
          ];
          shellHook = ''
            echo "devshell (${pkgs.stdenv.hostPlatform.system})"
            echo "  opencode   $(opencode --version 2>/dev/null || echo 'not found')"
            echo "  flake-tidy $(command -v flake-tidy >/dev/null 2>&1 && echo 'ok' || echo 'not found')"
            echo "  symlinker  $(command -v symlinker >/dev/null 2>&1 && echo 'ok' || echo 'not found')"
            echo "  just       $(just --version 2>/dev/null || echo 'not found')"
            echo "  nix        $(nix --version 2>/dev/null || echo 'not found')"
            echo "  git        $(git --version 2>/dev/null || echo 'not found')"
            echo "  gh         $(gh --version 2>/dev/null | head -1 || echo 'not found')"
          '';
        };
        # Lean shell for CI warming (fleet-build image + binary caches).
        # The default shell pulls this repo's own from-source builds
        # (selfPkgs.opencode), which a CI export job has to
        # compile from scratch — that realisation ran 2h+ and died
        # (dezren39/nix#1381). Everything below substitutes from
        # cache.nixos.org instead: same tools, no local builds.
        fleet = pkgs.mkShell {
          packages = [
            pkgs.opencode
            pkgs.just
            pkgs.nixfmt
            pkgs.git
            pkgs.gh
            pkgs.jq
            pkgs.ripgrep
          ];
        };
      }
    );
  };
}
