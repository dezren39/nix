# CI policy — main-only tier

Design: developing-today/code#1779 (`ci-fast-pr-full-main.md` §3.3–§3.4,
§5), as amended by #1172 (2026-10-30): the `pull_request` tier was
removed — CI is main-only. Every build workflow runs its full build
on push to `main`, on a nightly schedule, and on `workflow_dispatch`;
nothing runs on a pull request. This file is the adjustable policy
surface for this repo: tuning the policy means editing this table by
PR, never rewriting the pipeline. The highest class in a change wins.

| class | trigger paths (examples) | main tier |
|---|---|---|
| 0 — prose | `**.md`, docs/, comments-only changes | nothing, or markdown lint only |
| 1 — config/data | `**.json/yaml/toml` non-build, `.github/**` (non-workflow) | syntax parse + lint of changed files |
| 2 — package source (default) | one package's src + tests | lint/format + typecheck/syntax of affected packages + affected unit tests |
| 3 — elevated | lockfiles, `go.mod`/`go.sum` roots, `flake.nix`/`flake.lock`, build definitions, `.github/workflows/**`, shared/core libraries | class 2 + compile check of affected packages |
| 4 — nix expressions | `**.nix` | full flake eval/build of the affected outputs on `main` (no PR tier exists) |

Per-workflow application in this repo:

- `mcpx nix build` — class 4. The full `nix build` runs on push to
  `main` (restricted to `branches: [main]` and to `flake.nix`,
  `flake.lock`, and the workflow file itself), on a nightly schedule
  backstop, and on `workflow_dispatch`. Nothing the old PR build
  covered vanishes: main + nightly run it.
- `flake packages build` — #49: class 4 like `mcpx nix build`.
  The full per-package `nix build` matrix runs on push to `main`
  (nix/flake/package paths), nightly, and `workflow_dispatch`.
  mcpx is excluded — it stays owned by `mcpx nix build` above.
- `flake checks build` — #1134: class 4 like `flake packages build`.
  The full per-check `nix build` matrix (fail-fast off) runs on push
  to `main`, nightly, and `workflow_dispatch`, covering every
  `checks` derivation — the 23 `*-tests` suites plus `formatting`,
  `tidy`, and `mcpx-darwin-skip`. The check list is derived from the
  flake (`nix eval --json .#checks.x86_64-linux`), never hardcoded.
  No path filters: check sources include root-level scripts and
  test files, and a filter that misses one silently excludes that
  check (#1108).
- `buffer-backup lock sync` — already light (plain python3/node, no
  build); unchanged.
- `opencode` — a comment-triggered (`/oc`) bot, not a build check.
- Scheduled singleton workflows keep `cancel-in-progress: false` by
  design (design §2.2: deliberately left alone): `branch-cleanup`
  holds that value (restored by #1178 after #1172 flipped it to
  `true` without recording the change).
- `buffer-backup renovate bun sync` — Renovate automation, not a
  build check: since #1480 it follows the main-only trigger policy
  (no `pull_request` trigger; #1479 removed it and the old
  #1176 exception is retired). An hourly `schedule` scan (plus
  `workflow_dispatch`, optionally narrowed to one PR) finds open
  Renovate PRs whose changed files include the buffer-backup
  lockfile inputs and syncs each one as a matrix job, taking the
  branch from the PR object — never from a pull_request event.
  It likewise keeps `cancel-in-progress: false` by design — a
  run is never cancelled mid-push.

Main tier failures are owned by the remediation chain
(`main-broken-watcher.yml`, design §5): a failure of any of the
three watched main-tier workflows (`mcpx nix build`,
`flake packages build`, or `flake checks build`) on
`main` files (or appends to) a `main-broken` issue, keyed separately
per workflow (the workflow name is part of the Breakage-ID). This repo has
no Jules door, so the watcher enqueues the fix task itself at filing
time in an inline workflow step: it mints a GitHub Actions OIDC token
and posts the task to pm-dispatch's `/v1/tasks` endpoint at critical
priority, attached to the issue it just filed. If that enqueue step
fails, the issue carries the `queue/queued` label and the fleet monitor
enqueues it as the fallback.

## Release gate — Phase 3 (developing-today/code#1782, step 7)

Audit outcome (2026-11-02, queue task 6541): **no publish/release path
exists in this repo.** All nine workflows under
`.github/workflows/` are build-only or automation — `mcpx nix build`,
`flake packages build`, and `flake checks build` run `nix build` with
no push step; `main-broken watcher` files/appends issues;
`branch-cleanup` deletes stale branches; `buffer-backup lock sync`
only checks lockfiles; `buffer-backup renovate bun sync`
pushes a regenerated `bun.lock` back to the Renovate PR branch it
syncs (PR-scoped lockfile commit, not an artifact/cache/release
publish); `opencode` is a comment-triggered bot; `agents` inlines the
scheduled pm-dispatch behavior in `.github/workflows/agents.yml` rather
than calling the reusable `pm-scheduled` workflow. No step pushes
artifacts, caches, container images, or GitHub Releases, so there is
nothing to gate today — the release-gate acceptance is vacuously
satisfied for this repo.

Gate rule for any future publish path: a workflow that publishes
(artifacts, caches, images, releases) MUST first verify that the
publishing SHA has a `success` conclusion on all three watched
main-tier workflows (`mcpx nix build`, `flake packages build`,
`flake checks build`) via the Actions API, and refuse to publish
otherwise. The check is fail-closed: a failed **or missing**
main-gate conclusion blocks the publish.

Override path (hotfix): if the gate run is missing for a SHA that
must ship, a repo owner may re-run the three watched workflows for
that SHA (or `workflow_dispatch` the publish with an explicit
`override_release_gate=true` input plus a recorded reason), and the
override reason MUST be posted to the release PR/issue so the
exception is auditable. Without that explicit override, red or
unknown main-gate means no publish.
