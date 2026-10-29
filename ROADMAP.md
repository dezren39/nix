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

## mcpx moves to developing-today/code

`developing-today/code` is the only place the mcpx source lives (`pkgs/mcpx` there). This repo builds it from the pinned `mcpx-src` flake input (#468); the vendored copy that used to sit at `pkgs/mcpx` here is deleted. The plan was [`planning/plans/mcpx-extraction.md`](planning/plans/mcpx-extraction.md).

The OpenCode fork does not bundle mcpx. It links one only if the consumer passes it in, and this repo passes the build from the pinned input.

| Title                                    | Horizon | Status   | Intent                                                                                                             | Links                                                                  |
| ---------------------------------------- | ------- | -------- | ------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| Make mcpx consumable from the code repo  | Now     | planned  | Choose how to consume it and make it build on aarch64-darwin.                                                      | `developing-today/code#308`, `developing-today/code#237`               |
| Port the useful differences into code    | Now     | planned  | The `EnsureDaemon` check, `MCPX_STATE_DIR` and any tests that exist only here.                                     | [plan](planning/plans/mcpx-extraction.md), `developing-today/code#306` |
| Move the mcpx backlog                    | Now     | planned  | About 100 open mcpx issues here need new homes. GitHub cannot transfer across owners.                              | `developing-today/code#309`                                            |

## Keeping agent work safe on disk

macOS `dirhelper` deletes files in the per-user temp dir that were not accessed for three days. On 2026-10-17 that damaged 9 clones and 2 worktrees under `$TMPDIR/opencode`.

| Title                                     | Horizon | Status      | Intent                                                                                                                                                         | Links         |
| ----------------------------------------- | ------- | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------- |
| Temp-dir guard                            | Now     | in-progress | A launchd agent keeps `$TMPDIR/opencode` a symlink into `~/.local/share/opencode` and refreshes access times. Runs as an interim job until merged and rebuilt. | PR #392 |
| Uncommitted work in old worktrees         | Now     | proposed    | Staged deletions in two clones are backed up. Check the rest.                                                                                                  | #376          |
| Free disk space keeps dropping            | Next    | proposed    | Investigate between cleans.                                                                                                                                    | #373          |

## OpenCode and OpenChamber integration

| Title                          | Horizon | Status      | Intent                                                                                                | Links                                   |
| ------------------------------ | ------- | ----------- | ----------------------------------------------------------------------------------------------------- | --------------------------------------- |
| Keep the fork pins current     | Now     | in-progress | `flake.nix` pins the OpenCode and OpenChamber forks. The `update-opencode` skill describes the steps. | [skill](.agents/skills/update-opencode) |
| Finish the Datadog MCP sign-in | Next    | proposed    | The OAuth sign-in is not finished.                                                                    | #372                                    |

## Sidepulse patches

Sidepulse drives an LED strip from OpenCode state. This repo carries a patch stack on top of upstream.

| Title                                               | Horizon | Status   | Intent                                                                                                                       | Links                        |
| --------------------------------------------------- | ------- | -------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| Patch series: signals                               | Later   | proposed | Error signal, held completion signal, failures that stop expiring, several signals at once, signals that mean what they say. | #329, #332 |

## The darwin system

`configuration.nix`, `homeUser.nix`, `systemPackages.nix`, homebrew and the window manager config. No open issue covers it.

| Title                                  | Horizon | Status | Intent                                                                                                   | Links              |
| -------------------------------------- | ------- | ------ | -------------------------------------------------------------------------------------------------------- | ------------------ |
| Window manager notes                   | Ideas   | idea   | Auto reload for AeroSpace, centering the mouse on focus, auto-start for AutoRaise, other tiling options. | [TODO.md](TODO.md) |
| Sudo with Touch ID and system defaults | Ideas   | idea   | Notes on `pam` Touch ID, dock and finder defaults. Check which are already set.                          | [TODO.md](TODO.md) |
| Linux builder for nix-darwin           | Ideas   | idea   | A note enables `nix.linux-builder`.                                                                      | [TODO.md](TODO.md) |

## Done

Completed work is recorded in GitHub — see closed issues and merged PRs.
