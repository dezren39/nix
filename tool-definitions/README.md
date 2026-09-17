# tool-definitions

Verbatim copies of opencode's built-in tool descriptions, applied by
`../plugins/tool-definitions.ts` through the `tool.definition` plugin hook.
A filename matching a tool id replaces that tool's description wholesale.

**Sourced from opencode `1.18.31+e03db9b`** — the rev pinned in `flake.lock`.
Verify against the running binary after any flake update; these descriptions do
drift between releases (todowrite changed 9 lines between 1.18.21 and 1.18.31).

    just opencode-tooldefs-check

Only tools this config actually sends are tracked. Excluded on purpose:

- `read`, `lsp`, `plan-enter`, `plan-exit` — denied in opencode.jsonc
- `apply_patch` — registry.ts only sends it to `gpt-*` models; Claude models get
  `edit` and `write` instead

A file byte-identical to upstream is a no-op. Delete one to fall back to
whatever the installed opencode ships.
