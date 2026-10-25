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
| [CI and housekeeping](#ci-and-housekeeping)                                   | Concurrency groups, cleanup scripts                       |

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
| ROADMAP Done records the #617 completion                       | 2026-10-21 | #617, #618 |
| ROADMAP Done records the #619 completion                       | 2026-10-21 | #619, #620 |
| ROADMAP Done records the #621 completion                       | 2026-10-21 | #621, #622 |
| flake-tidy line scanners ignore braces in comments/strings     | 2026-10-21 | #623, #624 |
| ROADMAP Done records the #621 and #623 completions             | 2026-10-21 | #625, #626 |
| flake-tidy `_block_context_for_lines` closes one-line blocks   | 2026-10-22 | #628, #629 |
| ROADMAP Done records the #625 and #628 completions             | 2026-10-22 | #630, #631 |
| flake-tidy nested same-name block must not shadow root input   | 2026-10-22 | #634, #635 |
| ROADMAP Done records the #630 completion                       | 2026-10-22 | #632, #633 |
| ROADMAP Done records the #636 completion                       | 2026-10-22 | #638, #639 |
| flake-tidy nested inputs override must not reset scanner depth | 2026-10-22 | #640, #641 |
| flake-tidy `check_url_follows_conflicts` judges own-level only | 2026-10-22 | #642, #643 |
| ROADMAP Done records the #640 and #642 completions             | 2026-10-22 | #644, #645 |
| flake-tidy nested-inputs follows visible to existence checkers | 2026-10-22 | #647, #648 |
| ROADMAP Done records the #644 and #647 completions             | 2026-10-22 | #650, #651 |
| flake-tidy conflict judge uses own-level structural text       | 2026-10-22 | #646, #654 |
| ROADMAP Done records the #657 completion                       | 2026-10-22 | #657, #658 |
| ROADMAP Done records the #673 completion                       | 2026-10-22 | #673, #674 |
| flake-tidy judges every declaration in one-line nested block   | 2026-10-22 | #649, #675 |
| flake-tidy dotted-style parent blocks are not root scope       | 2026-10-22 | #655, #678 |
| ROADMAP Done records the #676 completion                       | 2026-10-22 | #676, #677 |
| flake-tidy `get_input_file_order` skips dotted override child  | 2026-10-22 | #652, #653, #681 |
| ROADMAP Done records the #679 completion                       | 2026-10-22 | #679, #680 |
| flake-tidy primed input names visible to block scanners        | 2026-10-22 | #683, #684 |
| flake-tidy primed names visible to deep-follows overrides      | 2026-10-22 | #685, #688 |
| ROADMAP Done records the #652, #679 and #683 completions       | 2026-10-22 | #686, #687 |
| ROADMAP Done records the #685 completion                       | 2026-10-22 | #689, #690 |
| flake-tidy `extract_failed_input` keeps primed names intact    | 2026-10-22 | #691, #692 |
| zsh init no longer runs `opencode completion` at shell start   | 2026-10-22 | #666, #695 |
| ROADMAP Done records the #686, #689 and #691 completions       | 2026-10-22 | #693, #694 |
| flake-tidy root-input inserts anchor outside dotted parents    | 2026-10-22 | #696, #697 |
| ROADMAP Done records the #666, #693 and #696 completions       | 2026-10-22 | #698, #699 |
| flake-tidy `remove_input_url_line` excises the url only        | 2026-10-22 | #700, #701 |
| ROADMAP Done records the #698 and #700 completions             | 2026-10-22 | #702, #703 |
| flake-tidy quoted input names visible to root-input scanners   | 2026-10-23 | #705, #706 |
| ROADMAP Done records the #702 and #705 completions             | 2026-10-23 | #707, #708 |
| flake-tidy `extract_failed_input` parses dotted input names    | 2026-10-23 | #710, #711 |
| flake-tidy `extract_failed_input` parses names structurally    | 2026-10-23 | #712, #713 |
| ROADMAP Done records the #715 completion                       | 2026-10-23 | #715, #716 |
| flake-tidy generators quote quote-requiring names              | 2026-10-23 | #709, #719 |
| flake-tidy `extract_failed_input` structural closing quote     | 2026-10-23 | #714, #720 |
| flake-tidy `_name_ref` matches `_quote_name` escapes           | 2026-10-23 | #721, #723 |
| flake-tidy `extract_failed_input` handles multi-primed paths   | 2026-10-23 | #722, #726 |
| flake-tidy `extract_failed_input` word-final possessives       | 2026-10-23 | #727, #728 |
| ROADMAP Done records the #709, #714 and #721 completions       | 2026-10-23 | #724, #725 |
| ROADMAP Done records the #722 and #727 completions             | 2026-10-23 | #729, #730 |
| flake-tidy `extract_failed_input` handles multi-word spans     | 2026-10-23 | #731, #732 |
| ROADMAP Done records the #724, #729 and #731 completions       | 2026-10-23 | #734, #735 |
| ROADMAP Done records the #734 completion                       | 2026-10-23 | #736, #737 |
| flake-tidy: a span's closing quote run is one delimiter      | 2026-10-23 | #733, #740 |
| ROADMAP Done records the #736 completion                     | 2026-10-23 | #738, #739 |
| ROADMAP Done records the #733 and #738 completions              | 2026-10-23 | #741, #742 |
| flake-tidy closing-run rule keeps adjacent quoted spans separate | 2026-10-23 | #743, #744 |
| flake-tidy closing run judged by whether the next span closes   | 2026-10-24 | #745, #746 |
| ROADMAP Done records the #741, #743 and #745 completions     | 2026-10-24 | #748, #749 |
| flake-tidy: separator before independent span is not a close | 2026-10-24 | #747, #753 |
| ROADMAP Done records the #748 completion                     | 2026-10-24 | #750, #751 |
| ROADMAP Done records the #747 and #750 completions           | 2026-10-24 | #754, #755 |
| flake-tidy closing-run: glued suffix is not an opening       | 2026-10-24 | #756, #757 |
| ROADMAP Done records the #754 and #756 completions           | 2026-10-24 | #758, #759 |
| ROADMAP Done records the #758 completion                     | 2026-10-24 | #761, #762 |
| ROADMAP Done records the #761 completion                     | 2026-10-24 | #765, #766 |
| flake-tidy separator-prefixed span no longer swallows prose  | 2026-10-24 | #760, #767 |
| ROADMAP Done records the #765 and #760 completions           | 2026-10-24 | #768, #769 |
| ROADMAP Done records the #768 completion                     | 2026-10-24 | #770, #771 |
| flake-tidy unclosed span no longer truncates the name        | 2026-10-24 | #772, #773 |
| flake-tidy `_brace_counts` fast paths (Bolt ledger)           | 2026-10-24 | #774, #752 |
| ROADMAP Done records the #770 and #772 completions            | 2026-10-24 | #779, #780 |
| flake-tidy: punctuation-start span is not a close             | 2026-10-24 | #778, #782 |
| buffer-backup lockfiles synced; lock-sync check added         | 2026-10-24 | #781, #783 |
| Stale dependency PRs triaged and landed                       | 2026-10-24 | #424, #775, #776, #777 |
| clean.sh var/folders sweep factored; its test is real         | 2026-10-24 | #557, #784 |
| ROADMAP Done records the #785 completion                     | 2026-10-24 | #785, #786 |
| ROADMAP Done records the #787 completion                     | 2026-10-24 | #787, #789 |
| ROADMAP Done rows match the completions their PRs record     | 2026-10-24 | #788, #791 |
| ROADMAP Done records the #793 completion                     | 2026-10-24 | #793, #794 |
| ROADMAP Done records the #795 completion                     | 2026-10-24 | #795, #796 |
| ROADMAP Done records the #797 completion                     | 2026-10-24 | #797, #798 |
| ROADMAP Done records the #799 completion                     | 2026-10-24 | #799, #800 |
| ROADMAP Done records the #801 completion                     | 2026-10-24 | #801, #802 |
| ROADMAP Done records the #803 completion                     | 2026-10-24 | #803, #804 |
| ROADMAP Done records the #806 completion                     | 2026-10-25 | #806, #807 |
| flake-tidy follows checks fast paths (Bolt ledger)           | 2026-10-25 | #808, #805 |
