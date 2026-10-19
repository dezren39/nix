# Move mcpx to developing-today/code

- Status: draft
- Roadmap item: [mcpx leaves this repo](../../ROADMAP.md#mcpx-moves-to-developing-todaycode)
- Design: none needed, the decision is made
- Issues: `developing-today/code#308` (how to consume), `developing-today/code#309` (move the backlog), `developing-today/code#237` (takeover tests on darwin)

## Goal

`developing-today/code` is the only place the `mcpx` source lives. This repo builds `mcpx` from a pinned rev of that repo, and `opencode`, the launchd daemon and OpenChamber still get it.

## Why

This repo carries a vendored copy (`pkgs/mcpx`) that has drifted: 29 files differ between code@8fc1fb3 and nix@4f46bd5, and each side holds fixes the other lacks. Fixes land twice or get missed. Code is the primary source.

## Does the darwin flake keep mcpx without this copy?

Not on its own. The OpenCode fork does not bundle mcpx. Its flake takes an optional argument and links the binary only when a consumer passes one:

- `opencode/flake.nix`: `mcpx = pkgs.mcpx or null`
- `opencode/nix/opencode.nix`: `mcpx ? null`, then `ln -s ${mcpx}/bin/mcpx $out/bin/mcpx`
- `flake.nix` here, in `mkPackages`: `inputs.opencode.packages.${system}.opencode.override { inherit mcpx; }`, where `mcpx` is built from `./pkgs/mcpx/package.nix`.

The same package feeds the launchd daemon (`configuration.nix`) and OpenChamber's PATH (`openchamber.nix`). Deleting `pkgs/mcpx` with no replacement removes mcpx from the CLI, the daemon and OpenChamber. The replacement has to be a pinned build of the code repo.

## Differences found (code is `-`, this repo is `+`)

- `internal/cli/client.go`: `EnsureDaemon` here checks `ctx.Err()` again after the poll loop (since #233). Code's fix for its #284 originally did not; code closed that gap in its #328 (PR #329), and this repo pinned the deadline-exit path with a test in PR #440. Landed on both sides; the remaining differences in this file are comment wording and the issue numbers cited.
- `internal/daemon/paths.go`: `envStateDir()` and `MCPX_STATE_DIR` in `DaemonSearchDirs` and `mirrorInfoPath`. Fixed here by #410 and ported to code by its PR #306 (merged 2026-10-18). Landed on both sides.
- `internal/daemon/server.go`, `takeover.go`, `registry_handoff.go`: both repos now wait for background orphan-session DELETEs on shutdown, on the handed-off return, and on the restart path — code via its #298/#299 and PR #307 (its #301), this repo via PR #421 (#409, #416). Landed on both sides.
- About 20 more files, mostly tests, docs and the `mcpx-agy` example README. Many differ only in the issue number a comment cites, since each repo uses its own numbers (code `#278` is `#369` here). I did not read every test diff.
- `internal/cli/dispatch.go` differs only in map-literal alignment. Run `gofmt` in code and settle which is right.

## What only exists here and must move

- Darwin test handling in `flake.nix`: a `postPatch` for `takeover_test.go` (`SOCK_CLOEXEC`) and a `-skip` list of 20 takeover tests. This is `developing-today/code#237`.
- The `bun-bin` argument (`pkgs/bun-bin`).

## What stays here

`mcpx.config.nix`, the launchd agent in `configuration.nix` (it restarts the daemon, since launchd cannot hand it over), the `homeUser.nix` wiring, and the `openchamber.nix` and `opencode-unwrapped` overrides. Issue #371 (the github server's write tools in the mcpx config) stays too.

## Steps

1. **Code can be consumed.** Choose how (`developing-today/code#308`) and make `mcpx` build on aarch64-darwin (`#237`). Done when `nix build` of the chosen output succeeds on this Mac.
2. **Port the differences.** One PR per group in code: the gofmt question, tests that exist only here. (The `EnsureDaemon` check and the `MCPX_STATE_DIR` change have already landed on both sides — see Differences found.) Done when `diff -ru` between code and this repo shows only issue-number comments and the darwin adaptations.
3. **Re-home the backlog.** `developing-today/code#309`. GitHub cannot transfer issues across owners, so each live issue is re-filed there and closed here with a link.
4. **Switch the build.** Add the code input, build `mcpx` from it in `mkPackages`, and keep `{ inherit mcpx; }` going into the fork. Done when `opencode`, the launchd daemon and OpenChamber all report the same `mcpx` version, and it comes from the pinned rev.
5. **Delete the copy.** Remove `pkgs/mcpx/`, `.github/workflows/mcpx.yml` and `.github/workflows/mcpx-conformance.yml`. Update `ARCHITECTURE.md` and the `.agents/skills` that mention `pkgs/mcpx`. Done when `grep -r pkgs/mcpx` finds nothing.
6. **Close the mcpx source issues here.** Those that become moot: #418, #403, #366, #367, #362, #259, #235, #108 and the design backlog. (#409 and #416 are already closed — fixed by PR #421, not by this step.)

## Tests and evidence

- `nix build` of `mcpx` from the pinned rev on aarch64-darwin, with the output of `mcpx version`.
- `darwin-rebuild build` for this configuration, before and after step 4, with the closure diff.
- After switching: `mcpx status` against the launchd daemon, and a call through `opencode`.

## Rollout and rollback

Step 4 is the only step that changes what runs on the machine. Roll back by reverting that commit, which restores the vendored copy as long as step 5 has not landed. Do step 5 in a separate PR after a few days of use.

## Risks

- The launchd daemon restarts when the store path changes. Calls in flight at that moment fail. The agent restarts the daemon because launchd cannot do a takeover, as `configuration.nix` explains.
- Code's input graph is large. If it is consumed whole, this repo's lock file grows. `developing-today/code#308` decides how to avoid that.

## Progress

- 2026-10-18: plan written. No step started.
