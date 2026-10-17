---
name: update-opencode
description: Updates the OpenCode / OpenChamber stack this nix-darwin config builds from developing-today's own forks, and repairs it when it breaks, from the forks up through this repo's flake.lock. Use when asked to update or upgrade OpenCode or OpenChamber, refresh the opencode/openchamber flake inputs, fix a failing opencode or openchamber build, or when OpenChamber/OpenCode misbehaves after an update.
---

# Update the OpenCode / OpenChamber stack

This repo does not patch OpenCode or OpenChamber. It builds both from forks we
own, and every fix goes into the fork it belongs to:

```
anomalyco/opencode (dev, v2)  ->  dezren39/opencode  ->  developing-today/opencode   (main)
openchamber/openchamber       ->  dezren39/openchamber -> developing-today/openchamber (main)
                                                              |
                                  inputs.opencode.follows = "opencode"   (this repo)
```

- `inputs.opencode` = `github:developing-today/opencode` (default branch `main`).
  Its own `nixpkgs` is deliberately NOT followed: the fork's
  `nix/hashes.json` node_modules hashes are computed with the bun in the
  fork's locked nixpkgs.
- `inputs.openchamber` = `github:developing-today/openchamber`, with
  `inputs.opencode.follows = "opencode"`, so there is one OpenCode.
- `flake.nix` `mkPackages`:
  - `opencode-unwrapped` = the fork's `packages.<sys>.opencode`, overridden
    with this repo's `mcpx`.
  - `opencode` = a shell wrapper that FORCES
    `OPENCODE_DB=$HOME/.local/share/opencode/opencode.db` and points `TMPDIR`
    at `~/.local/share/opencode/tmp` (`OPENCODE_TMPDIR` overrides). Never
    weaken the DB pin: every opencode and OpenChamber must open that one
    database (the user's sessions; ~26 GB). OpenChamber's server hard-codes
    `~/.local/share/opencode` and also honors `OPENCODE_DB`, which is set the
    same way in `environment.variables`, `launchd.user.envVariables` and
    home-manager `sessionVariables`.
  - `openchamber` = the fork's package overridden with that wrapped opencode.
- `openchamber.nix` runs `openchamber serve --foreground` on 127.0.0.1:3000 as
  launchd agent `org.nixos.openchamber`, with OpenCode reached through the
  link `~/.local/state/openchamber/bin/opencode`. Its activation restarts the
  agent when OpenChamber changes and only re-points the link + SIGHUPs when
  just OpenCode changed (in-place handover). The desktop app is a thin client
  (`OPENCHAMBER_SERVER_URL`, `OPENCHAMBER_SKIP_LOCAL_SERVER=1`). Read the
  header of `openchamber.nix` before changing any of it.

We may push to `main` of either fork at any time without asking. Prefer small,
conventional-commit commits; rebase on `origin/main` before pushing. Never
force-push `main`.

## Update procedure

1. Protect current work: `git status --short`. Never revert or commit unrelated
   changes (the build scripts `git add .`, so mention anything unrelated that
   would be swept into a build commit).
2. Optionally bring upstream into the forks first (each fork has a daily
   upstream-sync workflow; check whether it ran or do the merge yourself):
   - opencode: merge `anomalyco/opencode` `dev` into fork `main`.
   - openchamber: merge `openchamber/openchamber` `main` into fork `main`.
3. Verify the OpenCode fork builds here: in a clone of developing-today/opencode
   at the commit you want, `nix build .#opencode -L` (detached, poll the log).
4. Update OpenChamber's pin and verify it builds against that OpenCode:
   in developing-today/openchamber, `nix flake update opencode`, then
   `nix build .#openchamber .#openchamber-desktop -L`. Commit the lock and push.
5. In this repo: `nix flake update opencode openchamber`, then
   `just build-no-commit` until it passes, then one `just build` to commit.
   Activation (`just switch`) needs sudo and the user.

Builds are slow (opencode alone is several minutes; node_modules downloads are
fixed-output). Run them detached (`nohup ... > log 2>&1 &`) and check the log
every few minutes; never block on one long timeout.

## When something breaks: fix it where it lives, then re-pin upward

- **OpenCode build or behavior** -> fix in developing-today/opencode, push to
  `main`, then step 4 (OpenChamber lock), then step 5 (this repo's lock).
- **OpenChamber build or behavior** -> fix in developing-today/openchamber,
  push, then step 5.
- **Only this repo's wiring** (wrapper, launchd agent, env, activation) -> fix
  here.

Do not reintroduce a local patch stack here. A temporary
`--override-input opencode path:/some/checkout` is fine for testing, never as
the committed state.

## Recurring failure modes

- **`opencode-node_modules` hash mismatch.** The fork's `nix/hashes.json` holds
  one hash per system and must be the fork's own (its lockfile differs from
  upstream's; upstream's bot updates only upstream's). Take the `got:` value
  for the system you built and commit it to the fork. You can only build the
  aarch64-darwin one here; leave the linux ones unless you can build them.
- **`Cannot find module '../../../script/native-arch'`.** `nix/node_modules.nix`
  is the `src` for the opencode build; its fileset must include the repo-root
  `script/` directory (fixed in fork 43e5af3c17, may regress on upstream merges).
- **OpenChamber node_modules hash.** Same idea; bun installs platform-specific
  optional packages, so the hash is per system.
- **Darwin code signing.** `codesign --verify --strict` the built opencode
  binary and the OpenChamber app; an invalid signature gets the process killed
  on Apple Silicon. Ad-hoc re-sign in the fork's packaging if needed.
- **Database.** Before activating a build whose OpenCode adds migrations,
  compare the fork's migration ids
  (`packages/core/src/database/migration/*.ts`, `id: "..."`) with the
  `migration` table of `~/.local/share/opencode/opencode.db`
  (`sqlite3 "file:...?mode=ro" "select id from migration"`, use `LC_ALL=C sort`
  before `comm`). Applied-but-unknown ids mean the build is older than the
  database: stop. Pending ones should be additive; read any that `DROP` or
  `UPDATE` existing tables. Take an APFS-clone backup first (`just
  opencode-db-backup`).
- **Unrelated sidepulse lock drift**: an unrestricted `nix flake update` moves
  `sidepulse-src`; its patch stack lives in `pkgs/sidepulse` (zero-fuzz GNU
  patch; see that package's comments).

## Completion report

Report the old and new revisions of opencode and openchamber (fork commits and
this repo's lock), every fork commit you pushed and why, build commands and
results, whether the database migration check passed, and anything verified
only statically.
