# flake-tidy TODO

## High Priority

- [ ] Find the newest of all places sharing the same URL and use that revision
  - When multiple inputs share the same URL but have different locked revisions,
    prefer the newest (highest `lastModified`) and update the others to follow
- [ ] Smart same-URL detection beyond source_key grouping
  - Handle cases where URLs differ only in trailing slash, ref format, etc.

## Medium Priority

- [ ] `flatten` mode: interactive confirmation before adding new root inputs
- [ ] `flatten` mode: smarter name selection (prefer shorter, more common names)
- [ ] `flatten` mode: detect and skip inputs that would cause circular follows
- [ ] Config file support (`.flake-tidy.json` or `flake-tidy.toml` at repo root)
- [ ] `--format` flag to choose output format (text, json, github-actions)
- [ ] Detect and report conflicting follows declarations
- [ ] Support for workspace/multi-flake repositories

## Low Priority

- [ ] Web UI / TUI for interactive follows management
- [ ] `prune` mode: remove unused root inputs that nothing depends on
- [ ] `upgrade` mode: suggest version bumps for pinned inputs
- [ ] Generate a dependency graph visualization (mermaid or dot format)
- [ ] Pre-commit hook integration
- [ ] GitHub Action for automated PRs when tidy changes are available

## Done

- [x] Core dedup logic (follows for duplicate inputs)
- [x] Root same-URL consolidation
- [x] Transitive dedup with depth batching
- [x] `--override-input` fallback for registry resolution failures
- [x] Intermediate follows edge guard
- [x] Block and dotted style detection + insertion
- [x] `flatten` mode (hoist transitive inputs to root)
- [x] Granular excludes (global, dedup-specific, flatten-specific)
- [x] Includes system with wildcard default
- [x] CLI config overrides (--include, --exclude-*, --max-depth)
- [x] `--check` flag for CI integration
- [x] `--dry-run` mode
- [x] `--verbose` flag
- [x] Config validation
- [x] Default max-depth: 6
- [x] Nix flake check integration
- [x] Nix fmt integration
- [x] Unit tests with pytest
- [x] Integration tests with fixture flake
- [x] uv project setup
