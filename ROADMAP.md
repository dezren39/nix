# Roadmap

This is the roadmap for the repo. It is grouped by area, and each row has a horizon: Now, Next, Later or Ideas. How the roadmap works, and how it connects to designs and plans, is in [`planning/README.md`](planning/README.md).

A row records intent. It does not settle a decision and it is not a go-ahead to build.

Sources: open issues, [`TODO.md`](TODO.md), [`ARCHITECTURE.md`](ARCHITECTURE.md) and the merged PRs of 2026-10-18 through 2026-10-23. About 60 of the 167 open issues were read, so the mcpx design backlog is only partly listed here.

## Areas

| Area                                                                          | Current focus                                             |
| ----------------------------------------------------------------------------- | --------------------------------------------------------- |
| [mcpx moves to developing-today/code](#mcpx-moves-to-developing-todaycode)    | Remove the vendored copy and build from the code repo     |
| [Keeping agent work safe on disk](#keeping-agent-work-safe-on-disk)           | The temp-dir guard, and finding out what deleted files    |
| [OpenCode and OpenChamber integration](#opencode-and-openchamber-integration) | Pins, the mcpx config, MCP sign-ins                       |
| [Sidepulse patches](#sidepulse-patches)                                       | Port a 12-patch stack to upstream                         |
| [The darwin system](#the-darwin-system)                                       | Old notes only, no open work                              |
| [CI and housekeeping](#ci-and-housekeeping)                                   | One argument contract for the repo's scripts              |

## mcpx moves to developing-today/code

The mcpx plans are canonical in `developing-today/code`: [`doc/fleet/mcpx-extraction.md`](https://github.com/developing-today/code/blob/main/doc/fleet/mcpx-extraction.md), [`doc/fleet/mcpx-identity-transport.md`](https://github.com/developing-today/code/blob/main/doc/fleet/mcpx-identity-transport.md) and [`doc/fleet/mcpx-oauth-authorization-server.md`](https://github.com/developing-today/code/blob/main/doc/fleet/mcpx-oauth-authorization-server.md) (historical copies remain under `planning/plans/` here). mcpx source lives only in code's `pkgs/mcpx`; since #1388 (2026-11-03) this repo no longer builds mcpx at all - the fork's build arrives through the openchamber -> opencode chain and flake.nix only re-exposes it.

How the services run day to day is canonical at [`doc/fleet/services.md`](https://github.com/developing-today/code/blob/main/doc/fleet/services.md) and [`doc/fleet/mcp.md`](https://github.com/developing-today/code/blob/main/doc/fleet/mcp.md) in `developing-today/code`.

| Title                                    | Horizon | Status   | Intent                                                                                                             | Links                                                                  |
| ---------------------------------------- | ------- | -------- | ------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| mcpx takeover and handoff on darwin      | Now     | planned  | Takeover/upgrade/handoff is unsupported on darwin, and Linux-only tests break the darwin build.                        | `developing-today/code#237`                                            |

## Keeping agent work safe on disk

macOS `dirhelper` deletes files in the per-user temp dir that were not accessed for three days. On 2026-10-17 that damaged 9 clones and 2 worktrees under `$TMPDIR/opencode`.

| Title                                     | Horizon | Status      | Intent                                                                                                                                                         | Links         |
| ----------------------------------------- | ------- | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------- |
| Uncommitted work in old worktrees         | Now     | proposed    | Staged deletions in two clones are backed up. Check the rest.                                                                                                  | #376          |
| Free disk space keeps dropping            | Next    | proposed    | Investigate between cleans.                                                                                                                                    | #373          |

## OpenCode and OpenChamber integration

| Title                          | Horizon | Status      | Intent                                                                                                | Links                                   |
| ------------------------------ | ------- | ----------- | ----------------------------------------------------------------------------------------------------- | --------------------------------------- |
| Keep the fork pins current     | Now     | in-progress | `flake.nix` pins the OpenCode and OpenChamber forks. The `update-opencode` skill describes the steps. | [skill](.agents/skills/update-opencode) |
| Finish the Datadog MCP sign-in | Next    | proposed    | The OAuth sign-in is not finished.                                                                    | #372                                    |

## Sidepulse patches

Sidepulse drives an LED strip from OpenCode state. This repo carries a patch stack on top of upstream.

## The darwin system

`configuration.nix`, `homeUser.nix`, `systemPackages.nix`, homebrew and the window manager config. No open issue covers it.

| Title                                  | Horizon | Status | Intent                                                                                                   | Links              |
| -------------------------------------- | ------- | ------ | -------------------------------------------------------------------------------------------------------- | ------------------ |
| Window manager notes                   | Ideas   | idea   | Auto reload for AeroSpace, centering the mouse on focus, auto-start for AutoRaise, other tiling options. | [TODO.md](TODO.md) |
| Sudo with Touch ID and system defaults | Ideas   | idea   | Notes on `pam` Touch ID, dock and finder defaults. Check which are already set.                          | [TODO.md](TODO.md) |
| Linux builder for nix-darwin           | Ideas   | idea   | A note enables `nix.linux-builder`.                                                                      | [TODO.md](TODO.md) |

## CI and housekeeping

| Title                                    | Horizon | Status      | Intent                                                                                                                                 | Links       |
| ---------------------------------------- | ------- | ----------- | -------------------------------------------------------------------------------------------------------------------------------------- | ----------- |
| One argument contract for the scripts    | Now     | in-progress | Guard helpers in `lib/common.sh` and one shared conformance probe matrix; the discovery-family phase 1 landed (#1146); the opencode-db and packaged families follow. | #1145 |

## Done

Completed work is recorded in GitHub — see closed issues and merged PRs.
