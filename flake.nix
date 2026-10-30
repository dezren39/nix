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
    # developing-today's OpenCode fork (v2, tracks upstream dev plus model
    # routing, mcpx and serve handover). We own it: if it breaks, fix it there
    # and re-pin here -- see .agents/skills/update-opencode. Its own nixpkgs is
    # kept, not followed: the fork's node_modules hashes are computed against
    # the bun in that nixpkgs, and a different bun can lay node_modules out
    # differently and miss the fixed-output hash.
    opencode.url = "github:developing-today/opencode";
    # developing-today's OpenChamber fork, built against the opencode above.
    openchamber = {
      url = "github:developing-today/openchamber";
      inputs.opencode.follows = "opencode";
    };
    # mcpx's single source: developing-today/code, pkgs/mcpx. Source-only
    # (flake = false) so none of that repo's flake input graph lands in this
    # lock; the derivation is the package.nix that lives beside the source
    # there, called from mkPackages below. We own it: if it breaks, fix it
    # there and re-pin here. The vendored copy this replaces was deleted in
    # #468; the port audit is in that issue.
    mcpx-src = {
      url = "github:developing-today/code";
      flake = false;
    };
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
    flakeTidy.exclude.follows = [
      "opencode.nixpkgs"
      "openchamber.nixpkgs"
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

    # Shared package definitions — used by packages, apps, devShells, and checks
    mkPackages =
      pkgs:
      let
        system = pkgs.stdenv.hostPlatform.system;
        bun-bin = pkgs.callPackage ./pkgs/bun-bin/package.nix { };
        # mcpx is built from the pinned mcpx-src input (developing-today/code,
        # pkgs/mcpx — the only copy; the vendored one was deleted in #468),
        # using the package.nix that lives beside that source; adapt here,
        # never by patching the source. Its takeover/upgrade/handoff code is
        # Linux-only, not just its tests: the successor takes over systemd's
        # MAINPID, and mcpclient/handoff.go reads /proc/<pid>/stat. Run on
        # darwin with the SOCK_CLOEXEC test helper patched, all 12 skipped
        # tests fail (7 on the missing /proc, 5 "takeover: EOF"; checked
        # 2026-10-19 at mcpx-src 7274889). launchd cannot hand over its pid
        # either, so this config never upgrades in place: a new mcpx
        # restarts the agent (configuration.nix). Everything else runs.
        mcpx =
          (pkgs.callPackage (inputs.mcpx-src + "/pkgs/mcpx/package.nix") { inherit bun-bin; }).overrideAttrs
            (
              old:
              pkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isDarwin {
                # SOCK_CLOEXEC is a Linux socket flag; the helper only needs a pair.
                postPatch = (old.postPatch or "") + ''
                  substituteInPlace internal/daemon/takeover_test.go \
                    --replace-fail 'syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC' 'syscall.SOCK_STREAM'
                '';
                # The package's own checkPhase runs a bare
                # `go test -timeout 30m $pkgs`, so checkFlags would be
                # ignored; put -skip on that line instead.
                # developing-today/code#418: builtins.replaceStrings is a
                # silent no-op when its pattern is absent, so a canonical
                # checkPhase change in code's pkgs/mcpx/package.nix arriving
                # via an mcpx-src pin bump would silently drop this skip and
                # break the darwin build on a Mac. Fail evaluation loudly
                # instead; the mcpx-darwin-skip flake check forces this
                # attribute on every system, so Linux CI catches drift too.
                checkPhase =
                  let
                    rewritten =
                      builtins.replaceStrings
                        [ "go test -timeout 30m $pkgs" ]
                        [ "go test -timeout 30m -skip='${mcpxDarwinSkip}' $pkgs" ]
                        old.checkPhase;
                  in
                  if rewritten == old.checkPhase then
                    throw "mcpx darwin skip: the canonical checkPhase line `go test -timeout 30m $pkgs` was not found in the mcpx-src package.nix checkPhase, so the darwin -skip rewrite would silently do nothing (developing-today/code#418). Update the replaceStrings pattern in flake.nix to match the new canonical line."
                  else
                    rewritten;
              }
            );
        mcpxDarwinSkip = "^(${
          pkgs.lib.concatStringsSep "|" [
            "TestACallInFlightAtATakeoverIsAnsweredAndItsConnectionMovesOn"
            "TestAnIdleKeepAliveConnectionSurvivesATakeover"
            "TestClientHandoffCommitsOnlyWhenActivated"
            "TestClientLeaseSurvivesDaemonTakeover"
            "TestDetachedChildKeepsItsPipesAndAdoptionResumesThem"
            "TestDetachRefusesWhileACallIsInFlight"
            "TestHandoffDoesNotSplitAFrameAcrossTheTwoProcesses"
            "TestKeepAliveCallersLoseNoCallsAcrossARealTakeover"
            "TestTakeoverKeepsTheChildAndTheListener"
            "TestTakeoverLosesNoCallsWhileAClientKeepsCalling"
            "TestTakeoverRestartsAChangedServerAndKeepsTheRest"
            "TestUpgradeKeepsTheChildAndHandsMainPIDToTheSuccessor"
            "TestACallInFlightAtTheHandoverIsAnsweredByTheOldDaemon"
            "TestAClientLeaseSurvivesADaemonTakeover"
            "TestAnIdleConnectionSurvivesATakeoverInProcess"
            "TestCallsIssuedDuringATakeoverNeverFail"
            "TestHandoffRecordsTheIdentityAPoolWasMadeWith"
            "TestKeepAliveCallersLoseNoCallsAcrossATakeover"
            "TestRegistryHandoffKeepsTheChildAcrossTheSuccessor"
            "TestRegistryHandoffThatIsNotCommittedRestoresTheChild"
            # Not a takeover test: the restart it checks happens, but it
            # verifies the new executable through /proc/<pid>/exe.
            "TestExecChangeRestartsThatPoolAndNoOther"
          ]
        })$";
        # The fork's own build, with this repo's mcpx on its PATH so the CLI it
        # talks to is the same build as the launchd daemon.
        opencode-unwrapped = inputs.opencode.packages.${system}.opencode.override { inherit mcpx; };
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
        # developing-today/opencode#13 asks for this in core.
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
          mcpx
          opencode
          opencode-unwrapped
          ;

        # OpenChamber's server, running the wrapped opencode above. Its own
        # opencode input already follows ours (flake.nix inputs); the override
        # swaps in the wrapper so the database and scratch pins hold for the
        # OpenCode it starts too.
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
        # Cloudflare Wrangler 4.147.0 — custom package (copied from
        # developing-today/code pkgs/wrangler) because nixpkgs lags npm
        # by 50+ minor versions and Wrangler tracks the Workers runtime.
        wrangler = pkgs.callPackage ./pkgs/wrangler { };
        flake-tidy = import ./pkgs/flake-tidy { inherit pkgs; };
        twg-cli = pkgs.callPackage ./pkgs/twg-cli/package.nix { };
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
              chmod u+w ./opencode-db-vacuum.sh ./opencode-db-prune.sh ./opencode-db-pids.test.sh
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bash ./opencode-db-pids.test.sh
              touch $out
            '';
        # #941: opencode-db-args.test.sh pins the argument parsers of
        # opencode-db-vacuum.sh / opencode-db-prune.sh — every
        # value-taking option must reject an empty or flag-shaped value
        # naming the option, so "--into --dry-run" cannot swallow the
        # flag and run a real vacuum (the #930 pattern). Self-contained
        # apart from sqlite3 for the vacuum happy-path control.
        opencode-db-args-tests =
          pkgs.runCommandLocal "opencode-db-args-tests"
            {
              nativeBuildInputs = [ pkgs.bash pkgs.sqlite ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-vacuum.sh $src/opencode-db-prune.sh $src/opencode-db-args.test.sh ./
              chmod u+w ./opencode-db-vacuum.sh ./opencode-db-prune.sh ./opencode-db-args.test.sh
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
              nativeBuildInputs = [ pkgs.bash pkgs.sqlite pkgs.gzip pkgs.coreutils ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-vacuum.sh $src/opencode-db-prune.sh $src/opencode-db-prune.test.sh ./
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
              nativeBuildInputs = [ pkgs.bash pkgs.sqlite pkgs.coreutils ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-vacuum.sh $src/opencode-db-vacuum.test.sh ./
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
              nativeBuildInputs = [ pkgs.bash pkgs.coreutils ];
              src = inputs.self;
            }
            ''
              cp $src/opencode-db-backup-prune.sh $src/opencode-db-backup-prune.test.sh ./
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
              nativeBuildInputs = [ pkgs.bash pkgs.git ];
              src = inputs.self;
            }
            ''
              cp $src/git-discover-repos $src/git-discover-repos.test.sh ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
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
              cp $src/clean.sh $src/clean.test.sh $src/git-discover-repos ./
              mkdir -p lib
              cp $src/lib/common.sh lib/
              chmod u+w ./clean.sh ./clean.test.sh ./git-discover-repos
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
        # discover/clean/spotlight checks): consumers execute the sibling
        # git-discover-repos directly, and the nix build sandbox has no
        # /usr/bin/env, so the unpatched `#!/usr/bin/env bash` copy fails
        # with "bad interpreter", its stderr is swallowed by the consumer,
        # and discovery silently reports 0 repos.
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
              chmod u+w ./git-maintain-repos ./git-maintain-repos.test.sh ./git-discover-repos
              patchShebangs ./git-discover-repos
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
        # #478: archive/skills/plannotator-setup-goal/SKILL.test.ts (the
        # repo's only *.test.ts suite) was run by nothing — no check,
        # workflow, or recipe referenced it, unlike the flake-tidy,
        # warp-import, and opencode-db-pids suites (#397/#414/#472). It is
        # self-contained (bun:test content pins on the sibling SKILL.md),
        # so run every archived skill suite here.
        skill-tests =
          pkgs.runCommandLocal "skill-tests"
            {
              nativeBuildInputs = [ pkgs.bun ];
              src = inputs.self;
            }
            ''
              cp -r $src/archive ./archive
              chmod -R u+w ./archive
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              found=0
              for t in archive/skills/*/SKILL.test.ts; do
                [ -e "$t" ] || continue
                found=1
                echo "== $t"
                bun test "$t"
              done
              if [ "$found" -eq 0 ]; then
                echo "no archive/skills/*/SKILL.test.ts suites found" >&2
                exit 1
              fi
              touch $out
            '';
        # developing-today/code#418: the darwin mcpx package rewrites the
        # canonical checkPhase line from developing-today/code's
        # pkgs/mcpx/package.nix to add the takeover/handoff -skip list.
        # Force the aarch64-darwin package's checkPhase here, on every
        # system: the override now throws when its replaceStrings pattern
        # no longer matches, and this check additionally asserts the
        # rewritten phase really carries the skip, so an mcpx-src pin bump
        # (flake.lock) or a flake.nix edit that breaks the coupling fails
        # `nix flake check` on Linux instead of a Mac build later.
        mcpx-darwin-skip =
          let
            darwinCheckPhase = packages.aarch64-darwin.mcpx.checkPhase;
          in
          pkgs.runCommandLocal "mcpx-darwin-skip-check" { inherit darwinCheckPhase; } ''
            case "$darwinCheckPhase" in
            *"-skip="*) ;;
            *)
              echo "darwin mcpx checkPhase lost its -skip rewrite:" >&2
              printf '%s\n' "$darwinCheckPhase" >&2
              exit 1
              ;;
            esac
            touch $out
          '';
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
            selfPkgs.wrangler
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
      }
    );
  };
}
