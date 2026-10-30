# CI policy — fast PR tier, full main tier

Design: developing-today/code#1779 (`ci-fast-pr-full-main.md` §3.3–§3.4,
§5). This file is the adjustable policy surface for this repo: tuning
the PR tier means editing this table by PR, never rewriting the
pipeline. The highest class in a change wins.

| class | trigger paths (examples) | PR tier |
|---|---|---|
| 0 — prose | `**.md`, docs/, comments-only changes | nothing, or markdown lint only |
| 1 — config/data | `**.json/yaml/toml` non-build, `.github/**` (non-workflow) | syntax parse + lint of changed files |
| 2 — package source (default) | one package's src + tests | lint/format + typecheck/syntax of affected packages + affected unit tests |
| 3 — elevated | lockfiles, `go.mod`/`go.sum` roots, `flake.nix`/`flake.lock`, build definitions, `.github/workflows/**`, shared/core libraries | class 2 + compile check of affected packages |
| 4 — nix expressions | `**.nix` | class 1 + parse/eval of the changed files only (never full flake check on a PR) |

Per-workflow application in this repo:

- `mcpx nix build` — class 4: on `pull_request` it is eval-only
  (parse the changed `.nix` files + the darwin mcpx skip-guard eval).
  The full `nix build` runs on push to `main` (restricted to
  `branches: [main]`, so a commit never fires the suite twice via
  push + pull_request) and on a nightly schedule backstop. Nothing
  the old PR build covered vanishes: main + nightly run it.
- `flake packages build` — #49: class 4 like `mcpx nix build`.
  On `pull_request` it instantiates every `x86_64-linux` package's
  drvPath (eval only); the full per-package `nix build` matrix runs on
  push to `main` and nightly. mcpx is excluded — it stays owned by
  `mcpx nix build` above.
- `flake checks build` — #1134: class 4 like `flake packages build`.
  On `pull_request` it instantiates every `x86_64-linux` check's
  drvPath (eval only); the full per-check `nix build` matrix
  (fail-fast off) runs on push to `main` and nightly, covering every
  `checks` derivation — the 23 `*-tests` suites plus `formatting`,
  `tidy`, and `mcpx-darwin-skip`. The check list is derived from the
  flake (`nix eval --json .#checks.x86_64-linux`), never hardcoded.
  No path filters on any tier: check sources include root-level
  scripts and test files, and a filter that misses one silently
  excludes that check (#1108).
- `buffer-backup lock sync` — already light (plain python3/node, no
  build); unchanged by the two-tier split.
- `opencode` — a comment-triggered (`/oc`) bot, not a build check;
  out of scope for the PR tier.
- Scheduled singleton workflows (`branch-cleanup`,
  `buffer-backup renovate bun sync`) keep `cancel-in-progress: false`
  by design (design §2.2: deliberately left alone).

Main tier failures are owned by the remediation chain
(`main-broken-watcher.yml`, design §5): a failure of `mcpx nix build`
on `main` files (or appends to) a `main-broken` issue. This repo has
no Jules door, so the issue is released to the fleet queue directly
(`queue/queued` label; priority-90 packet enqueue is fleet-side via
that label + the fleet monitor).
