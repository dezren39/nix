# Roadmap

This is the roadmap for the repo. It is grouped by area, and each row has a horizon: Now, Next, Later or Ideas. How the roadmap works, and how it connects to designs and plans, is in [`planning/README.md`](planning/README.md).

A row records intent. It does not settle a decision and it is not a go-ahead to build.

Sources: open issues, [`TODO.md`](TODO.md), [`ARCHITECTURE.md`](ARCHITECTURE.md) and the merged PRs of 2026-10-18 through 2026-10-21. About 60 of the 167 open issues were read, so the mcpx design backlog is only partly listed here.

## Areas

| Area                                                                          | Current focus                                             |
| ----------------------------------------------------------------------------- | --------------------------------------------------------- |
| [mcpx moves to developing-today/code](#mcpx-moves-to-developing-todaycode)    | Remove the vendored copy and build from the code repo     |
| [Keeping agent work safe on disk](#keeping-agent-work-safe-on-disk)           | The temp-dir guard, and finding out what deleted files    |
| [OpenCode and OpenChamber integration](#opencode-and-openchamber-integration) | Pins, the mcpx config, MCP sign-ins                       |
| [Sidepulse patches](#sidepulse-patches)                                       | Port a 12-patch stack to upstream                         |
| [The darwin system](#the-darwin-system)                                       | Old notes only, no open work                              |
| [CI and housekeeping](#ci-and-housekeeping)                                   | Stale dependency PRs, concurrency groups, cleanup scripts |

## mcpx moves to developing-today/code

`developing-today/code` is the only place the mcpx source lives (`pkgs/mcpx` there). This repo builds it from the pinned `mcpx-src` flake input (#468); the vendored copy that used to sit at `pkgs/mcpx` here is deleted. The plan was [`planning/plans/mcpx-extraction.md`](planning/plans/mcpx-extraction.md).

The OpenCode fork does not bundle mcpx. It links one only if the consumer passes it in, and this repo passes the build from the pinned input.

| Title                                    | Horizon | Status   | Intent                                                                                                             | Links                                                                  |
| ---------------------------------------- | ------- | -------- | ------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| Make mcpx consumable from the code repo  | Now     | planned  | Choose how to consume it and make it build on aarch64-darwin.                                                      | `developing-today/code#308`, `developing-today/code#237`               |
| Port the useful differences into code    | Now     | planned  | The `EnsureDaemon` check, `MCPX_STATE_DIR` and any tests that exist only here.                                     | [plan](planning/plans/mcpx-extraction.md), `developing-today/code#306` |
| Move the mcpx backlog                    | Now     | planned  | About 100 open mcpx issues here need new homes. GitHub cannot transfer across owners.                              | `developing-today/code#309`                                            |
| Build mcpx from the code repo            | Next    | done     | Added the `mcpx-src` input, kept passing `mcpx` to the fork, deleted `pkgs/mcpx`.                                   | [plan](planning/plans/mcpx-extraction.md), #468                        |
| Fork flake decides where mcpx comes from | Next    | proposed | Whether the OpenCode flake keeps mcpx optional or defaults to the code repo's build.                               | `developing-today/opencode#82`                                         |
| mcpx config for the github server        | Next    | proposed | The config lets the github server use PR and issue write tools. This stays here, since the config file lives here. | #371                                                                   |

## Keeping agent work safe on disk

macOS `dirhelper` deletes files in the per-user temp dir that were not accessed for three days. On 2026-10-17 that damaged 9 clones and 2 worktrees under `$TMPDIR/opencode`.

| Title                                     | Horizon | Status      | Intent                                                                                                                                                         | Links         |
| ----------------------------------------- | ------- | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------- |
| Temp-dir guard                            | Now     | in-progress | A launchd agent keeps `$TMPDIR/opencode` a symlink into `~/.local/share/opencode` and refreshes access times. Runs as an interim job until merged and rebuilt. | PR #392, #377 |
| Confirm what deleted the files            | Now     | proposed    | `log show` returned nothing for `dirhelper`, so the cause is a strong guess.                                                                                   | #377          |
| Uncommitted work in old worktrees         | Now     | proposed    | Staged deletions in two clones are backed up. Check the rest.                                                                                                  | #376          |
| Rebuild to activate the bootstrap changes | Now     | proposed    | The `.bootstrap-done` gitignore and the fork OpenCode and OpenChamber are not active until a rebuild.                                                          | #374          |
| Free disk space keeps dropping            | Next    | proposed    | Investigate between cleans.                                                                                                                                    | #373          |
| Finish the `clean.sh` recommendations     | Next    | proposed    |                                                                                                                                                                | #375          |

## OpenCode and OpenChamber integration

| Title                          | Horizon | Status      | Intent                                                                                                | Links                                   |
| ------------------------------ | ------- | ----------- | ----------------------------------------------------------------------------------------------------- | --------------------------------------- |
| Keep the fork pins current     | Now     | in-progress | `flake.nix` pins the OpenCode and OpenChamber forks. The `update-opencode` skill describes the steps. | [skill](.agents/skills/update-opencode) |
| Finish the Datadog MCP sign-in | Next    | proposed    | The OAuth sign-in is not finished.                                                                    | #372                                    |

## Sidepulse patches

Sidepulse drives an LED strip from OpenCode state. This repo carries a patch stack on top of upstream.

| Title                                               | Horizon | Status   | Intent                                                                                                                       | Links                        |
| --------------------------------------------------- | ------- | -------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| Update to upstream main and port the 12-patch stack | Next    | proposed |                                                                                                                              | #327                         |
| Generate the OpenCode v2 plugin                     | Next    | proposed |                                                                                                                              | #378                         |
| Fixed-path LED writer helper                        | Next    | proposed | Lets the macOS TCC grant survive Nix rebuilds.                                                                               | #328                         |
| Patch series: signals                               | Later   | proposed | Error signal, held completion signal, failures that stop expiring, several signals at once, signals that mean what they say. | #329, #330, #331, #332, #333 |

## The darwin system

`configuration.nix`, `homeUser.nix`, `systemPackages.nix`, homebrew and the window manager config. No open issue covers it.

| Title                                  | Horizon | Status | Intent                                                                                                   | Links              |
| -------------------------------------- | ------- | ------ | -------------------------------------------------------------------------------------------------------- | ------------------ |
| Window manager notes                   | Ideas   | idea   | Auto reload for AeroSpace, centering the mouse on focus, auto-start for AutoRaise, other tiling options. | [TODO.md](TODO.md) |
| Sudo with Touch ID and system defaults | Ideas   | idea   | Notes on `pam` Touch ID, dock and finder defaults. Check which are already set.                          | [TODO.md](TODO.md) |
| Linux builder for nix-darwin           | Ideas   | idea   | A note enables `nix.linux-builder`.                                                                      | [TODO.md](TODO.md) |

## CI and housekeeping

| Title                                          | Horizon | Status   | Intent                                                                                                                                                                                                                                                            | Links                                                                  |
| ---------------------------------------------- | ------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| Stale dependency PRs                           | Next    | proposed | Renovate PRs for `pkgs/buffer-backup` (TypeScript 7, @types/node, @types/vscode) and two Bolt drafts, #364 and #388, have sat open since 2026-10-01 or later. Merge or close the rest and pin them in `renovate.json`. | #317, #320, #324, #364, #388 |
| macOS Go test failures                         | Next    | proposed | Moot once mcpx is built from the code repo — that build has landed (#469), so this is moot as predicted.                                                                                                                                               | #366                                                                   |

## Done

| Title                                                        | Date       | Result     |
| ------------------------------------------------------------ | ---------- | ---------- |
| Concurrency groups that keep queued `/oc` runs               | 2026-10-18 | #425       |
| Restart path waits for orphan DELETEs                        | 2026-10-18 | #421       |
| Orphaned upstream sessions end off the takeover serving path | 2026-10-18 | #404       |
| `EnsureDaemon` honours a cancelled context                   | 2026-10-18 | #400       |
| `MCPX_STATE_DIR` in daemon discovery                         | 2026-10-18 | #410       |
| node scripts run with `--experimental-transform-types`       | 2026-10-18 | #387       |
| `Group.Messages` filled in statistics                        | 2026-10-18 | #394       |
| flake-tidy tests as a flake check                            | 2026-10-18 | #401       |
| Plugin tests as a flake check                                | 2026-10-18 | #415       |
| `timeout-minutes` on every job                               | 2026-10-18 | #406, #417 |
| Concurrency groups on workflows                              | 2026-10-18 | #399       |
| `clean.sh` prunes `.worktrees` in the postgres sweep         | 2026-10-18 | #412       |
| mcpx built from the code repo; vendored copy deleted         | 2026-10-19 | #469       |
| `.pytest_cache` ignored; tracked cache marker dropped        | 2026-10-19 | #456       |
| `.ruff_cache` / `.mypy_cache` ignored; marker dropped        | 2026-10-19 | #464       |
| opencode-db guard counts a bare `opencode` argv[0]           | 2026-10-19 | #470       |
| opencode-db-pids test as a flake check                       | 2026-10-19 | #477       |
| Archived skill tests as a flake check                        | 2026-10-19 | #479       |
| Formatting flake check green (nixfmt)                        | 2026-10-19 | #481       |
| LoginExtension `.dmg` untracked; `lootbox.db` ignored        | 2026-10-19 | #482       |
| Checkout and opencode actions SHA-pinned                     | 2026-10-20 | #485       |
| flake-tidy loads its config; tidy check green                | 2026-10-20 | #486       |
| flake-tidy import probe quotes the flake path                | 2026-10-20 | #490       |
| ARCHITECTURE.md flake summary brought up to date             | 2026-10-20 | #493       |
| flake-tidy parse-result isolation gate (for #454)            | 2026-10-20 | #494       |
| mcpx-nixbuild push runs get their own group (code#392 half)  | 2026-10-20 | #495       |
| DeterminateSystems action pinned to a v23 SHA (code#375 set) | 2026-10-20 | #483       |
| ROADMAP Done records the 2026-10-19/20 merges                | 2026-10-20 | #489       |
| ROADMAP Done records the #487 and #492 merges                | 2026-10-20 | #498       |
| ROADMAP Done records the #494 and #495 merges                | 2026-10-20 | #500       |
| flake-tidy dedups same-repo pins at different revs           | 2026-10-20 | #511       |
| mcpx-src bumped for the 30m check timeout (code#510)         | 2026-10-20 | #513       |
| ROADMAP Done records six 2026-10-20 merges                   | 2026-10-20 | #515       |
| flake-tidy: rev-pinned inputs never deduped against unpinned | 2026-10-20 | #516       |
| ROADMAP Done records the #512 and #514 merges                | 2026-10-20 | #518       |
| flake-tidy rev-dedup covers git-type inputs                  | 2026-10-20 | #519, #520 |
| flake-tidy returns real URLs for git/sourcehut inputs        | 2026-10-20 | #521, #522 |
| flake-tidy git-locked fallback for indirect overrides        | 2026-10-20 | #523       |
| flake-tidy returns a path: URL for path inputs               | 2026-10-20 | #524, #525 |
| flake-tidy cross-type dedup ranks git above indirect         | 2026-10-20 | #526, #527 |
| ROADMAP Done records the #518–#527 merges                    | 2026-10-20 | #531, #532 |
| flake-tidy `_locked_url` handles tarball/file/path nodes     | 2026-10-20 | #533, #534 |
| flake-tidy forge handling keeps host and dir                 | 2026-10-20 | #535, #536 |
| flake-tidy URL rebuild keeps submodules/shallow/allRefs/dir  | 2026-10-20 | #528, #537, #538, #541 |
| flake-tidy handles hg (mercurial) inputs                     | 2026-10-20 | #539, #542 |
| Darwin mcpx `-skip` rewrite fails loudly on drift (code#418) | 2026-10-20 | #543       |
| flake-tidy `node_url`/`_append_query` dedupe query params    | 2026-10-20 | #544, #545 |
| ROADMAP Done records the #533–#544 merges                    | 2026-10-21 | #547, #548 |
| flake-tidy `node_url` ref segment precedes embedded query    | 2026-10-21 | #546, #552 |
| flake-tidy groups same repo across URL spellings             | 2026-10-21 | #555, #559 |
| flake-tidy `resolve_follows` returns None on cyclic follows  | 2026-10-21 | #565, #566 |
| flake-tidy override builder drops unresolvable `indirect:` ref | 2026-10-21 | #568, #569 |
| flake-tidy `_normalize_vcs_url` splits embedded query first    | 2026-10-21 | #560, #561 |
| flake-tidy `compute_indirect_overrides` cycle-guarded          | 2026-10-21 | #563, #564 |
| flake-tidy `_normalize_vcs_url` case rules cover scp/`.GIT`    | 2026-10-21 | #562, #572 |
| flake-tidy `_normalize_vcs_url` strips at most one `.git`      | 2026-10-21 | #575, #576 |
| flake-tidy `_normalize_vcs_url` drops explicit default ports   | 2026-10-21 | #579, #580 |
| flake-tidy `_normalize_vcs_url` lowercases IPv6 literal hosts  | 2026-10-21 | #581, #582 |
| flake-tidy `_normalize_vcs_url` strips one trailing host dot   | 2026-10-21 | #584, #585 |
| flake-tidy canonicalizes IPv6 and port spellings               | 2026-10-21 | #583, #588 |
| flake-tidy keys unconvertible ports verbatim, never raises     | 2026-10-21 | #589, #592 |
| flake-tidy `_normalize_vcs_url` drops compound default ports   | 2026-10-21 | #595, #596 |
| flake-tidy `source_key` canonicalizes the forge host           | 2026-10-21 | #599, #600 |
| flake-tidy rebuilt URLs percent-encode lock values             | 2026-10-21 | #601, #602 |
| flake-tidy `source_key` drops the explicit default forge host  | 2026-10-21 | #605, #606 |
| flake-tidy `_locked_url` keeps the locked rev for forge types  | 2026-10-21 | #609, #610 |
| flake-tidy `source_key` canonicalizes tarball/file hosts       | 2026-10-21 | #611, #612 |
| flake-tidy `_canonicalize_url_authority` ends at `?` and `#`   | 2026-10-21 | #613, #614 |
| ROADMAP Done records the #599 and #601 completions             | 2026-10-21 | #603, #604 |
| ROADMAP Done records the #605 completion                       | 2026-10-21 | #607, #608 |
| ROADMAP Done records the #609, #611 and #613 completions       | 2026-10-21 | #615, #616 |
