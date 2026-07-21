{
  description = "lootbox – Deno CLI with Hono server, MCP, and Vite UI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
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

        # ── helpers ──────────────────────────────────────────────────
        version = (builtins.fromJSON (builtins.readFile ./deno.json)).version;

        src = pkgs.lib.cleanSourceWith {
          src = ./.;
          filter =
            path: type:
            let
              baseName = builtins.baseNameOf path;
            in
            # exclude build artefacts, caches, and IDE dirs
            baseName != "node_modules"
            && baseName != ".git"
            && baseName != ".opencode"
            && baseName != ".lootbox"
            && baseName != "lootbox"; # compiled binary in repo root
        };

        # ── vendored chrome-devtools-mcp ─────────────────────────────
        #
        # The published npm package is a self-contained rollup bundle
        # (all deps are devDeps — zero runtime npm deps).  We just need
        # node + the tarball contents.
        chrome-devtools-mcp = pkgs.stdenv.mkDerivation rec {
          pname = "chrome-devtools-mcp";
          version = "0.21.0";

          src = pkgs.fetchurl {
            url = "https://registry.npmjs.org/${pname}/-/${pname}-${version}.tgz";
            hash = "sha256-KMHWSSctf6ihIT792yZeTqaYuHqmUanN2SJje5PI184=";
          };

          nativeBuildInputs = [ pkgs.makeWrapper ];

          unpackPhase = ''
            mkdir -p $TMPDIR/pkg
            tar xzf $src -C $TMPDIR/pkg --strip-components=1
          '';

          installPhase = ''
            mkdir -p $out/lib/chrome-devtools-mcp $out/bin

            cp -r $TMPDIR/pkg/build $out/lib/chrome-devtools-mcp/
            cp $TMPDIR/pkg/package.json $out/lib/chrome-devtools-mcp/

            # Wrapper script: runs the MCP server with node from the Nix store
            makeWrapper ${pkgs.nodejs_22}/bin/node $out/bin/chrome-devtools-mcp \
              --add-flags "$out/lib/chrome-devtools-mcp/build/src/bin/chrome-devtools-mcp.js"
          '';

          meta = with pkgs.lib; {
            description = "MCP server for Chrome DevTools (vendored)";
            homepage = "https://github.com/ChromeDevTools/chrome-devtools-mcp";
            license = licenses.asl20;
            mainProgram = "chrome-devtools-mcp";
          };
        };

        # ── package: compiled lootbox binary ─────────────────────────
        lootbox = pkgs.stdenv.mkDerivation {
          pname = "lootbox";
          inherit version src;

          nativeBuildInputs = [
            pkgs.deno
            pkgs.makeWrapper
          ];

          # Deno needs a writable home for its cache
          buildPhase = ''
            export DENO_DIR="$TMPDIR/deno"
            mkdir -p "$DENO_DIR"

            # Cache deps first (network allowed during build via FOD or --impure)
            deno install

            # Build the UI
            cd ui && deno install && deno run -A npm:vite build && cd ..

            # Compile the CLI binary
            deno compile --allow-all --include ui/dist -o lootbox src/lootbox-cli.ts
          '';

          # The compiled binary spawns `deno run` subprocesses at runtime
          # (worker_manager, execute_llm_script, execute_rpc) so deno must
          # be on PATH.
          installPhase = ''
            mkdir -p $out/bin
            cp lootbox $out/bin/.lootbox-unwrapped
            makeWrapper $out/bin/.lootbox-unwrapped $out/bin/lootbox \
              --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.deno ]}
          '';

          meta = with pkgs.lib; {
            description = "lootbox CLI – Hono server, MCP, Vite UI";
            license = licenses.mit;
            mainProgram = "lootbox";
          };
        };

        # ── lootbox-full: lootbox + chrome-devtools-mcp ───────────────
        # Convenience package that installs both to the same profile.
        # When installed via `nix profile install`, both land on PATH.
        lootbox-full = pkgs.symlinkJoin {
          name = "lootbox-full-${version}";
          paths = [
            lootbox
            chrome-devtools-mcp
          ];

          meta = with pkgs.lib; {
            description = "lootbox CLI with vendored chrome-devtools-mcp";
            license = licenses.mit;
            mainProgram = "lootbox";
          };
        };

      in
      {
        # ── packages ───────────────────────────────────────────────
        packages = {
          default = lootbox;
          lootbox = lootbox;
          lootbox-full = lootbox-full;
          inherit chrome-devtools-mcp;
        };

        # ── apps ───────────────────────────────────────────────────
        apps.default = {
          type = "app";
          program = "${lootbox}/bin/lootbox";
        };

        # Run the chrome-devtools-mcp server standalone
        apps.chrome-devtools-mcp = {
          type = "app";
          program = "${chrome-devtools-mcp}/bin/chrome-devtools-mcp";
        };

        # Dev convenience: cache deps + compile in working tree
        apps.update = {
          type = "app";
          program = toString (
            pkgs.writeShellScript "lootbox-update" ''
              set -euo pipefail
              export PATH="${pkgs.deno}/bin:$PATH"
              deno install
              cd ui && deno install && cd ..
              deno task compile
            ''
          );
        };

        # Create global ~/.lootbox dirs and default config if missing.
        # Flags:
        #   --no-autodetect     skip MCP binary detection entirely
        #   --include <bin>     force-add a binary as an MCP server
        #   --exclude <bin>     skip this binary even if autodetected
        apps.setup = {
          type = "app";
          program = toString (
            pkgs.writeShellScript "lootbox-setup" ''
              set -euo pipefail

              # ── parse flags ─────────────────────────────────────
              autodetect=true
              includes=()
              excludes=()

              while [ $# -gt 0 ]; do
                case "$1" in
                  --no-autodetect) autodetect=false; shift ;;
                  --include)
                    [ $# -lt 2 ] && { echo "error: --include requires an argument"; exit 1; }
                    includes+=("$2"); shift 2 ;;
                  --exclude)
                    [ $# -lt 2 ] && { echo "error: --exclude requires an argument"; exit 1; }
                    excludes+=("$2"); shift 2 ;;
                  -h|--help)
                    echo "Usage: lootbox-setup [FLAGS]"
                    echo ""
                    echo "Creates ~/.lootbox/ dirs and a default config.json."
                    echo ""
                    echo "Flags:"
                    echo "  --no-autodetect   skip MCP binary detection"
                    echo "  --include <bin>   force-add a binary as MCP server"
                    echo "  --exclude <bin>   skip a binary even if autodetected"
                    echo "  -h, --help        show this help"
                    exit 0 ;;
                  *) echo "unknown flag: $1 (try --help)"; exit 1 ;;
                esac
              done

              # ── detect an MCP binary on PATH ──────────────────────
              # Uses only `command -v` — bare binary names survive Nix
              # profile upgrades and garbage collection.  If the binary
              # is installed but not on PATH, the user must fix that.
              detect_mcp_binary() {
                local bin="$1"
                if command -v "$bin" >/dev/null 2>&1; then
                  echo "$bin"
                  return 0
                fi
                return 1
              }

              # ── check if a binary is excluded ─────────────────────
              is_excluded() {
                local bin="$1"
                local ex
                for ex in "''${excludes[@]+"''${excludes[@]}"}"; do
                  [ "$ex" = "$bin" ] && return 0
                done
                return 1
              }

              # ── config key from binary name ───────────────────────
              # "chrome-devtools-mcp" → "chrome-devtools" (strip -mcp, keep hyphens)
              config_name() {
                local bin="$1"
                echo "''${bin%-mcp}"
              }

              global_dir="$HOME/.lootbox"
              config_file="$global_dir/config.json"

              echo "lootbox setup"
              echo ""

              # ── create global dirs ──────────────────────────────
              created=""
              for dir in tools workflows scripts; do
                if [ ! -d "$global_dir/$dir" ]; then
                  mkdir -p "$global_dir/$dir"
                  created="''${created:+$created, }$dir"
                fi
              done

              if [ -n "$created" ]; then
                echo "created ~/.lootbox/{$created}"
              else
                echo "~/.lootbox/ dirs already exist"
              fi

              # ── collect MCP servers ─────────────────────────────
              # Parallel indexed arrays + a counter (avoids set -u issues
              # with empty-array length expansion).
              mcp_names=()
              mcp_cmds=()
              n_servers=0

              add_server() {
                local name="$1" cmd="$2"
                mcp_names+=("$name")
                mcp_cmds+=("$cmd")
                n_servers=$((n_servers + 1))
              }

              # Autodetect known MCP binaries
              if [ "$autodetect" = true ]; then
                for bin in chrome-devtools-mcp; do
                  if is_excluded "$bin"; then
                    echo "excluded: $bin"
                    continue
                  fi
                  cmd="$(detect_mcp_binary "$bin" || true)"
                  if [ -n "$cmd" ]; then
                    add_server "$(config_name "$bin")" "$cmd"
                    echo "detected: $bin"
                  fi
                done
              fi

              # Force-included binaries (always added, warn if not found)
              for bin in "''${includes[@]+"''${includes[@]}"}"; do
                if is_excluded "$bin"; then
                  echo "excluded (overrides --include): $bin"
                  continue
                fi
                cmd="$(detect_mcp_binary "$bin" || true)"
                if [ -n "$cmd" ]; then
                  add_server "$(config_name "$bin")" "$cmd"
                  echo "included: $bin"
                else
                  echo "warning: --include $bin not found on PATH"
                fi
              done

              # ── generate config ─────────────────────────────────
              # Only write a config file when there are MCP servers to
              # register — everything else uses lootbox built-in defaults.
              if [ -f "$config_file" ]; then
                echo ""
                echo "config already exists: $config_file"

                if [ "$n_servers" -gt 0 ]; then
                  for (( i=0; i<n_servers; i++ )); do
                    name="''${mcp_names[$i]}"
                    if ! grep -q "\"$name\"" "$config_file" 2>/dev/null; then
                      echo ""
                      echo "hint: $name is available but not in your config."
                      echo "  add to server.mcpServers in $config_file:"
                      echo "    \"$name\": { \"command\": \"''${mcp_cmds[$i]}\", \"args\": [] }"
                    fi
                  done
                fi
              elif [ "$n_servers" -gt 0 ]; then
                {
                  echo '{'
                  echo '  "server": {'
                  echo '    "mcpServers": {'
                  for (( i=0; i<n_servers; i++ )); do
                    comma=","
                    [ $((i + 1)) -eq "$n_servers" ] && comma=""
                    echo "      \"''${mcp_names[$i]}\": {"
                    echo "        \"command\": \"''${mcp_cmds[$i]}\","
                    echo '        "args": []'
                    echo "      }$comma"
                  done
                  echo '    }'
                  echo '  }'
                  echo '}'
                } > "$config_file"
                echo "wrote $config_file"
              fi

              echo ""
              if [ -f "$config_file" ]; then
                echo "done. start with: lootbox server --config $config_file"
              else
                echo "done. start with: lootbox server"
              fi
            ''
          );
        };

        # ── devShell ───────────────────────────────────────────────
        devShells.default = pkgs.mkShell {
          name = "lootbox-dev";

          packages = with pkgs; [
            deno
            nodejs_22 # for ui/vite tooling
          ];

          shellHook = ''
            echo "lootbox dev shell  (deno $(deno --version | head -1 | awk '{print $2}'))"
          '';
        };

        # ── formatter (nix fmt) ────────────────────────────────────
        formatter = pkgs.writeShellScriptBin "lootbox-fmt" ''
          set -euo pipefail
          export PATH="${pkgs.deno}/bin:$PATH"
          deno lint --fix "$@"
          deno fmt "$@"
        '';

        # ── checks (nix flake check) ──────────────────────────────
        checks = {
          # Run the test suite
          tests = pkgs.stdenv.mkDerivation {
            name = "lootbox-check-tests";
            inherit src;
            nativeBuildInputs = [ pkgs.deno ];
            buildPhase = ''
              export DENO_DIR="$TMPDIR/deno"
              mkdir -p "$DENO_DIR"
              deno test --allow-all test/
            '';
            installPhase = "mkdir -p $out && touch $out/ok";
          };

          # Formatting check (--check mode, no writes)
          formatting = pkgs.stdenv.mkDerivation {
            name = "lootbox-check-formatting";
            inherit src;
            nativeBuildInputs = [ pkgs.deno ];
            buildPhase = ''
              export DENO_DIR="$TMPDIR/deno"
              mkdir -p "$DENO_DIR"
              echo "Checking deno fmt..."
              deno fmt --check
              echo "Checking deno lint..."
              deno lint
            '';
            installPhase = "mkdir -p $out && touch $out/ok";
          };
        };
      }
    );
}
