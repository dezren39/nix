---
name: update-opencode
description: Updates the OpenCode flake input to the newest configured upstream commit and repairs active OpenCode Nix patches while preserving their behavior. Use when asked to update or upgrade OpenCode, refresh the opencode flake input, fix OpenCode patch failures after an update, or make .#opencode build. Ask the user only when upstream appears to implement equivalent behavior or preserving a patch requires a semantic refactor.
---

# Update OpenCode

Update the OpenCode flake input and leave `.#opencode` building at the newest
commit resolved by the source configured in `flake.nix`. Preserve the behavioral
intent of every active patch, not merely its old diff context.

## Scope and source of truth

Start by reading:

1. `flake.nix`, especially `inputs.opencode`, `opencodePatches`, and `mkPackages`.
2. `flake.lock`, to record the old OpenCode revision.
3. Every patch listed in `opencodePatches`, in application order.
4. `tui.jsonc`, because active patches can add configuration consumed there.
5. `ARCHITECTURE.md`, for the current package and patch layout.

The configured input currently tracks `github:anomalyco/opencode/dev`. "Newest"
means the head resolved by the input URL and ref that are actually present in
`flake.nix`; do not silently switch repository, branch, or source.

Only patches listed in `opencodePatches` are active. Root-level OpenCode patch
files are historical/reference snapshots and must not be activated, regenerated,
deleted, or treated as build inputs unless the user separately requests that.

The package override also replaces upstream Bun with `pkgs/bun-bin`. Preserve
that override while updating or repairing patches.

## Human decision policy

Proceed autonomously through update, diagnosis, patch repair, compilation, and
verification. Do not ask about ordinary maintenance such as:

- shifted hunk context or line numbers;
- moved or renamed files;
- reorganized imports or nearby code;
- local API/type changes with an obvious behavior-preserving adaptation;
- patch ordering conflicts that can be resolved without changing behavior;
- routine build, formatting, cache, or network retries.

Ask the user before changing course only in either situation below.

### 1. Upstream may have implemented the patch

Stop before removing, disabling, or materially shrinking the patch. Present:

- the upstream code, option, issue, or behavior that appears equivalent;
- a point-by-point comparison with this repo's patch invariants;
- any remaining difference, including defaults, configuration, or edge cases;
- options to remove the patch, retain a reduced patch, or keep the current
  behavior, with a recommendation.

Then ask which option to take. A patch applying cleanly is not evidence that it
is still necessary; conversely, similar upstream code is not enough to remove it
without comparing behavior.

### 2. Preserving intent requires a semantic refactor

This gate applies when upstream replaced the relevant architecture or lifecycle
and there is no local, behavior-preserving port. Routine renames and API updates
do not count. Present:

- how the old patch worked;
- what upstream changed and why a mechanical port is unsafe or impossible;
- two or more viable designs, including behavior, maintenance cost, and risks;
- the recommended design.

Ask the user before implementing that refactor.

If neither gate applies, do not stop at a failed patch or partial build.

## Active patch invariants

Always derive the active list from `flake.nix`. The current active patches and
their required behavior are documented below.

### `patches/opencode-compact-tui.patch`

Intent: retain the dense, zero-spacing TUI used by this configuration without
changing application semantics.

Required behavior:

- Prompt content, prompt metadata/status rows, and workspace notices use zero
  left/right/top padding and zero gaps where the patch currently targets them.
- Home prompt/footer containers have their patched padding removed.
- Sidebar content/footer has zero patched padding and gaps.
- The session's main message column has zero patched outer padding, bottom
  padding, and gaps.
- User, assistant, reasoning, text, task, tool, diff, question, and revert blocks
  retain the patch's compact spacing.
- The sidebar defaults to hidden rather than automatic.
- Permission and sidebar vertical scrollbars remain hidden.

This is a behavioral density policy, not a blind global replacement of every
spacing value in upstream. Port all existing patch targets and inspect nearby
new equivalents, but do not remove spacing from unrelated dialogs or controls
without evidence that they replaced a patched component.

Likely upstream-equivalence signals that require the human gate:

- a supported compact/density/spacing mode producing the same layout;
- theme-level spacing controls covering all patched targets;
- upstream defaults to the same hidden-sidebar and hidden-scrollbar behavior.

### `patches/opencode-scroll-autofollow.patch`

Intent: streamed output follows the bottom only while the user is already at or
near the bottom. Scrolling into history must not be overridden by each streamed
token.

Required behavior:

- TUI config accepts `auto_scroll_tolerance` as an integer greater than or equal
  to zero. It must be part of the schema because unknown fields are stripped.
- An omitted value defaults to `0`, meaning exactly at the bottom.
- The current viewport position is sampled while rendering.
- Auto-follow disengages after the user scrolls above the configured tolerance.
- Auto-follow re-engages when the viewport returns within tolerance.
- Frame callbacks or equivalent observers are cleaned up with the component.
- The session scrollbox uses dynamic sticky behavior, not unconditional
  `stickyScroll={true}`.
- `tui.jsonc` remains valid and its value retains the documented semantics.

Likely upstream-equivalence signals that require the human gate:

- upstream conditional sticky scrolling with the same disengage/re-engage rules;
- an upstream auto-follow setting that covers the same tolerance semantics;
- a viewport API that directly exposes equivalent "user is following" state.

### `patches/opencode-plan-permissions-reminder.patch`

Intent: Plan remains read-only by default through its resolved permission rules,
while configured Plan permission overrides are not contradicted by injected
legacy or experimental reminder text.

Required behavior:

- Both `plan.txt` and `plan-mode.txt` state that resolved Plan permissions are
  authoritative.
- Allowed paths and actions may be used, including configured overrides.
- Denied paths and actions remain prohibited.
- Neither reminder claims its text supersedes resolved permissions.
- The experimental reminder does not claim the generated plan file is the only
  writable path, but still directs the agent to maintain that plan file.
- Plan's built-in permission defaults and lifecycle orchestration remain
  unchanged.

Likely upstream-equivalence signals that require the human gate:

- upstream reminder text explicitly defers to resolved Plan permissions;
- Plan workflow generation derives its writable-path guidance from effective
  permission rules;
- the hardcoded read-only reminder is replaced by permission-aware instructions.

### `patches/opencode-hidden-agent-variants.patch`

Intent: hidden/internal agents with an explicitly configured variant use that
variant instead of inheriting the originating user turn's variant. This allows
compaction and title generation to select independent reasoning effort.

Required behavior:

- A hidden agent with an explicit variant resolves options from that variant.
- A hidden agent without an explicit variant preserves upstream inheritance.
- Non-hidden agents preserve upstream user-turn variant behavior.
- Small hidden calls, including title generation, may use an explicit variant.
- Compaction message metadata records its effective configured variant.

Likely upstream-equivalence signals that require the human gate:

- request preparation natively gives internal agent variants precedence;
- compaction and title expose another supported independent effort setting;
- compaction no longer derives its variant from the originating user message.

### `patches/opencode-agent-variant-defaults.patch`

Intent: a visible agent's configured variant is the initial TUI selection when
the user has not saved an explicit preference for that model.

Required behavior:

- A saved per-model variant, including explicit `default`, takes precedence.
- Without a saved preference, an agent using its configured model uses its
  configured variant.
- An agent-level variant does not apply after selecting a different model.
- Changing the TUI variant continues to persist the explicit per-model choice.

Likely upstream-equivalence signals that require the human gate:

- the TUI natively falls back to the active agent's configured variant;
- variant persistence becomes agent-aware while preserving explicit choices.

### `patches/opencode-run-descendant-permissions.patch`

Intent: port anomalyco/opencode#36898 so headless `opencode run` applies its
existing `--auto` approve or non-auto reject policy to permission requests from
any descendant Task session, rather than ignoring the request and hanging.

Required behavior:

- Session create/update events incrementally identify descendants of the root.
- Missed events are recovered by walking `parentID` with cycle protection.
- Unrelated session permission requests remain ignored.
- Descendant lineage lookups are bounded and cached.
- A failed lineage lookup aborts the pending root request and makes a bounded
  best-effort server abort instead of leaving the command blocked.
- Both prompt and command requests observe that abort signal.

Likely upstream-equivalence signals that require the human gate:

- anomalyco/opencode#36898 or equivalent descendant-aware headless permission
  handling is merged;
- permission requests are routed to a root session before the CLI receives them.

### `patches/opencode-nested-subagent-prompts.patch`

Intent: port the runtime portions of anomalyco/opencode#36046 so permission and
question prompts from nested subagent chains are visible in the root TUI and the
interactive CLI footer tracks the full descendant tree.

Required behavior:

- TUI permission/question collection traverses the full root-session subtree.
- Child session views do not duplicate prompts collected by the root view.
- Traversal is cycle-safe and does not cross unrelated session trees.
- Interactive CLI bootstrap discovers descendants recursively.
- Live nested Task events register grandchild and deeper subagent tabs.
- Existing direct-child tab navigation remains unchanged.

Likely upstream-equivalence signals that require the human gate:

- anomalyco/opencode#36046 or equivalent full-subtree prompt collection and CLI
  footer tracking is merged;
- nested permission prompts are centrally routed to the root session.

## Update workflow

### 1. Protect current work

Inspect `git status --short`, the unstaged diff, and the staged diff. The worktree
may already be dirty. Never revert, overwrite, stage, or commit unrelated user
changes. Record the old OpenCode revision from `flake.lock`.

### 2. Update only OpenCode

Run from the repository root:

```bash
nix flake update opencode
```

Do not run an unrestricted `nix flake update`. Inspect the lockfile diff and
confirm changes are limited to the OpenCode input and any unavoidable inputs
owned by its lock graph.

Verify that the new locked revision matches the configured branch head when
GitHub is reachable:

```bash
git ls-remote https://github.com/anomalyco/opencode.git refs/heads/dev
```

If `flake.nix` tracks a different source or ref, adapt this check to that source.
Do not hardcode the current repository when the configured input has changed.

### 3. Build before editing patches

```bash
nix build .#opencode --no-link --print-build-logs
```

If it succeeds, continue to the invariant audit and smoke checks. A clean patch
application can still miss newly introduced replacement UI components.

### 4. Diagnose against the exact locked source

Use a fresh disposable clone or source tree checked out at the new locked
revision. Do not diagnose against an arbitrary current checkout.

Validate active patches individually and then in their Nix application order.
Nix's patch phase uses strip level `-p1`, so patch paths must remain rooted as
`a/packages/...` and `b/packages/...`. The final Nix build is authoritative even
when `git apply --check` succeeds.

Classify failures:

- Missing path: locate the replacement by symbols and behavior, not filename
  alone.
- Failed hunk: compare old and new surrounding logic and port the intent.
- Compile/type failure: adapt to the new local API while retaining invariants.
- Runtime architecture replacement: evaluate whether the semantic-refactor gate
  applies.
- Similar upstream behavior: evaluate whether the upstream-equivalence gate
  applies.

Useful searches include:

```bash
rg 'stickyScroll|scrollTop|scrollHeight|setFrameCallback' packages/tui
rg 'scroll_acceleration|auto_scroll|tolerance' packages/tui
rg 'paddingLeft|paddingRight|paddingTop|paddingBottom|marginTop|gap=' packages/tui/src
rg 'kv\.signal<"auto" \| "hide">\("sidebar"' packages/tui
rg 'only file|supersedes|resolved Plan permissions' packages/opencode/src/session/prompt
rg 'variantName|agent\.variant|userMessage\.model\.variant' packages/opencode/src/session
rg 'isSessionInTree|permission\.sessionID !== sessionID' packages/opencode/src/cli/cmd/run.ts
rg 'collectDescendantSessions|collectSubtree|knownSession' packages/opencode/src/cli/cmd/run packages/tui/src/routes/session
```

### 5. Regenerate the smallest correct patches

Work from pristine upstream at the exact locked revision. Recreate each patch's
changes in `opencodePatches` order.

- Keep each patch focused on its documented intent.
- Preserve patch order and validate the whole sequence.
- If a later patch depends on an earlier patch's source state, stage the earlier
  changes in the disposable checkout as the baseline before generating the later
  diff; do not create workspace commits.
- Generate paths compatible with `-p1`.
- Avoid unrelated formatting or generated-file changes.
- Use `apply_patch` for manual workspace edits. A deterministic `git diff
  --binary` export is acceptable for regenerating a patch after its source edits
  have been reviewed.

After regeneration, inspect the full patch rather than trusting successful
application. Compare the old and new diffs against every invariant above.

### 6. Verify application and behavior

In a fresh exact-revision source tree, apply all active patches in order and
perform static checks for the invariants. Specifically confirm:

- compact prompt, home, session, sidebar, permission, and tool targets still
  contain the intended zero-spacing/visibility/default values;
- `auto_scroll_tolerance` is present in the effective schema;
- the bottom-distance calculation uses viewport height and scroll height;
- sticky scrolling is driven by state and callback/observer cleanup remains;
- `tui.jsonc` matches the implemented setting name and semantics.
- legacy and experimental Plan reminders defer to resolved permissions and do
  not claim the generated plan file is the only writable path.
- explicitly configured hidden-agent variants take precedence while ordinary
  agent and hidden-agent fallback behavior remains unchanged.
- headless descendant permission requests receive the root run's approval policy
  and ancestry failures abort rather than hang.
- nested permission/question prompts and live subagent state surface across the
  full descendant tree without changing direct-child navigation.

Do not claim runtime UI behavior was manually observed unless it actually was.
Static source verification plus a successful build should be reported as such.

### 7. Build and smoke-test the flake app

Run until successful:

```bash
nix build .#opencode --no-link --print-build-logs
nix run .#opencode -- --version
```

Also evaluate or inspect the app program if needed to confirm `.#opencode` points
to the patched package. The task is not complete with only `git apply --check` or
only a package evaluation.

Run `git diff --check` and inspect the final diff. Do not run a full system switch
unless the user asks. Do not commit or push unless explicitly requested.

## Completion report

Report:

- old and new locked OpenCode revisions;
- whether the new revision matched the configured remote head;
- which patches applied unchanged and which were regenerated;
- how each patch invariant was verified;
- the exact build and smoke commands and their results;
- any verification that remained static rather than interactive;
- unrelated existing worktree changes left untouched.

## Keep this skill current

Update this file whenever an active patch is added, removed after user approval,
or materially changes intent; when source structure or build commands change; or
when a new recurring failure mode is discovered.
