{
  lib,
  buildGoModule,
  # Runtimes mcpx can use for `mcpx run` / `mcpx exec`. It picks the first one
  # present on PATH in this order, so the wrapper below pins a known-good set
  # rather than depending on whatever the calling shell happens to have.
  deno,
  bun-bin,
  nodejs,
  # git backs the `repo` and `worktree` scopes. Without it those silently
  # degrade to per-directory keys, so it is a hard runtime dependency rather
  # than a nicety.
  git,
  makeBinaryWrapper,
}:
buildGoModule {
  pname = "mcpx";
  version = "0.1.0";

  src = lib.cleanSourceWith {
    src = ./.;
    filter =
      path: type:
      let
        base = baseNameOf path;
      in
      !(
        base == "bin"
        || base == "result"
        || lib.hasPrefix "result-" base
        || base == "mcpx-client.ts"
      );
  };

  # One third-party Go dependency: modernc.org/sqlite, which backs the log
  # index. Everything else -- the MCP client, the process pools, the JSON
  # Schema to TypeScript compiler, the CLI -- is standard library.
  #
  # modernc.org/sqlite specifically, rather than the usual mattn driver,
  # because it is a pure-Go translation of SQLite. A cgo driver would make this
  # derivation need a C toolchain and would break cross-compilation, for a
  # database that is only ever an index over the JSONL logs.
  vendorHash = "sha256-lWVua+XHU33Fc9GzRPrHN82OHlGTPxQvkV8BD+tbx4U=";

  subPackages = [ "cmd/mcpx" ];

  ldflags = [
    "-s"
    "-w"
    "-X main.version=0.1.0"
  ];

  nativeBuildInputs = [ makeBinaryWrapper ];

  # subPackages confines `go test` to cmd/mcpx, which has none. The real suites
  # live in internal/; the unit ones run in the sandbox, the end-to-end ones
  # spawn JavaScript runtimes and bind unix sockets and do not.
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    export MCPX_STATE_DIR=$TMPDIR/state MCPX_CACHE_DIR=$TMPDIR/cache
    go test ./internal/config/ ./internal/codegen/ ./internal/daemon/ ./internal/pool/ ./internal/logging/ ./internal/logstore/
    runHook postCheck
  '';

  postInstall = ''
    wrapProgram $out/bin/mcpx \
      --suffix PATH : ${
        lib.makeBinPath [
          git
          deno
          bun-bin
          nodejs
        ]
      }
  '';

  meta = {
    description = "Run TypeScript against your MCP servers from the command line";
    longDescription = ''
      mcpx keeps a local daemon that owns every MCP server process. Scripts
      import a generated, fully typed client and call tools as ordinary async
      functions, so only what a script prints reaches the model's context.

      Stateful servers such as chrome-devtools-mcp are leased per session, so
      concurrent agents get separate processes instead of corrupting one
      shared browser.
    '';
    homepage = "https://github.com/dezren39/mcpx";
    license = lib.licenses.mit;
    mainProgram = "mcpx";
    platforms = lib.platforms.unix;
  };
}
