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
4. `tui.jsonc` and the user's `~/.config/opencode/cli.json`, because active
   patches can add configuration consumed there. Since `dev` became the v2 CLI
   (07bbd0f), the TUI reads `cli.json` (schema `packages/tui/src/config/index.tsx`,
   migrated once from a legacy `tui.json` if `cli.json` is absent); `tui.jsonc`
   is no longer read by `.#opencode`.
5. `ARCHITECTURE.md`, for the current package and patch layout.

The configured input currently tracks `github:anomalyco/opencode/dev`. "Newest"
means the head resolved by the input URL and ref that are actually present in
`flake.nix`; do not silently switch repository, branch, or source.

Only patches listed in `opencodePatches` are active. Retired patch files may stay
in `patches/` for reference; see "Retired patches" below. Root-level OpenCode patch
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
- The sidebar defaults to hidden rather than automatic. Since v2 this is the
  `session.sidebar` config default (`component/session-frame.tsx` and the
  `dialog-config.tsx` default), not a kv signal.
- Permission and sidebar vertical scrollbars remain hidden.

v2 layout: message rendering is split across `routes/session/index.tsx`,
`message-parts.tsx` (text, reasoning, `InlineToolRow`) and `group-view.tsx`;
per-row spacing lives on `SessionRowView`'s `marginTop`. The prompt metadata
row is `component/prompt/metadata.tsx`. Horizontal `gap`s between tokens in a
single row, the turn-token debug block, the queued-prompt dock and the
composer panel are intentionally left at upstream spacing.

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
  Since v2 it is a top-level key of `~/.config/opencode/cli.json`.
- An omitted value defaults to `0`, meaning exactly at the bottom.
- The current viewport position is sampled while rendering.
- Auto-follow disengages after the user scrolls above the configured tolerance.
- Auto-follow re-engages when the viewport returns within tolerance.
- Frame callbacks or equivalent observers are cleaned up with the component.
- The session scrollbox uses dynamic sticky behavior, not unconditional
  `stickyScroll={true}`. v2 already gates it on message navigation
  (`!navigationMessage() && !navigationSlack()`); the patch ANDs
  `stickToBottom()` into that expression. OpenTUI's own manual-scroll tracking
  re-engages at one line from the bottom, which is why the configurable
  tolerance is still patched.
- `tui.jsonc` remains valid and its value retains the documented semantics.

Likely upstream-equivalence signals that require the human gate:

- upstream conditional sticky scrolling with the same disengage/re-engage rules;
- an upstream auto-follow setting that covers the same tolerance semantics;
- a viewport API that directly exposes equivalent "user is following" state.

### `patches/opencode-plan-permissions-reminder.patch`

Intent: Plan remains read-only by default through its resolved permission rules,
while configured Plan permission overrides are not contradicted by injected
reminder text.

Since v2 there is a single reminder: the `enter` template in
`packages/core/src/plugin/plan.ts` (the v1 `plan.txt`/`plan-mode.txt` are gone).
The patch also updates `packages/core/test/plugin/plan.test.ts`, which asserts
on the reminder text.

Required behavior:

- The Plan reminder states that resolved Plan permissions are authoritative.
- Allowed paths and actions may be used, including configured overrides.
- Denied paths and actions remain prohibited.
- Neither reminder claims its text supersedes resolved permissions.
- The reminder does not claim the plan directory is the only writable path, but
  keeps upstream's plan-file workflow (discuss in conversation; write plan files
  under the plan directory only when asked).
- Plan's built-in permission defaults and lifecycle orchestration remain
  unchanged.

Likely upstream-equivalence signals that require the human gate:

- upstream reminder text explicitly defers to resolved Plan permissions;
- Plan workflow generation derives its writable-path guidance from effective
  permission rules;
- the hardcoded read-only reminder is replaced by permission-aware instructions.

### `patches/opencode-run-descendant-permissions.patch`

Intent: port anomalyco/opencode#36898 so headless `opencode run` applies its
existing `--auto` approve or non-auto reject policy to permission requests from
any descendant Task session, rather than ignoring the request and hanging.

Since v2 the code is `packages/cli/src/run/noninteractive.ts`. Upstream already
walks `parentID` (`ownsSession`) for permissions and forms, so the patch is a
reduced hardening of that walk: cycle-safe, 10 s bounded lookups, settled
results cached, `session.created` events seed descendants, and a failed lookup
aborts admission, makes a 5 s bounded `session.interrupt`, and rethrows (the
upstream walk swallowed failures as "unrelated" and hung). v2 `run` has no
separate command request, so only prompt admission observes the abort.

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

Since v2 upstream covers most of this: the TUI collects prompts over
`data.session.family(root)` (cycle-safe, root-keyed) and the mini footer
(`packages/tui/src/mini/stream-v2.subagent.ts`) discovers descendants
recursively. The remaining patch is one effect in `routes/session/index.tsx`
that also syncs each descendant's children, because the root sync lists only
direct children and deeper subagents created before the view attached were
otherwise never registered.

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

### `patches/opencode-question-submit-exit.patch`

Intent: make the `question` tool prompt escapable and multi-select questions
submittable. Upstream `dev` leaves multi-select impossible to submit from the
keyboard and swallows ctrl+c while the custom-answer textarea is focused.

Since v2 the question prompt is the form prompt,
`packages/tui/src/routes/session/form.tsx` (anomalyco/opencode#42493 landed).
Upstream now clears `editing` in `selectTab`, lets `tab` advance multi-select,
binds `space` to toggle, and its editing layer has no `confirm()` gate. The
reduced patch keeps the remaining behavior: the editing layer's first command
is `app.exit` → dismiss (so ctrl+c dismisses rather than clears, and ctrl+d
works while editing), multi-select `return` advances to the next field except
on the "Type your own answer" row, and the footer shows `space toggle` and
`enter next`. Names below that predate v2 (`reject()`, `useBindings`,
`tuiConfig.keybinds.get`) map to `cancel`, `Keymap.createLayer` and command ids.

Required behavior:

- `selectTab` clears `editing`, so changing tabs (including by mouse) can never
  leave `editing` set on a tab whose editing layer is inactive. Without this both
  binding layers are disabled at once and every key, including ctrl+c, is dead.
- The editing layer registers an `app.exit` command bound to `reject()` and
  includes `tuiConfig.keybinds.get("app.exit")`, so ctrl+c/ctrl+d dismiss the
  question instead of being swallowed. Question mode replaces the base keymap
  layer, so the global `app.exit` binding is otherwise inactive.
- The editing layer is enabled on `store.editing` alone, not
  `store.editing && !confirm()`.
- Multi-select binds `space` to toggle, matching the `dialog.mcp.toggle` and
  `plugins.toggle` convention.
- Multi-select binds `return` to advance to the next tab, whose last tab is
  "Confirm", where `return` submits. `return` still opens the editor when the
  "Type your own answer" row is selected.
- Single-select `return` behavior is unchanged.
- `escape` continues to reject from the non-editing layer and to cancel the edit
  from the editing layer.
- The footer advertises `space toggle` for multi-select and reports `enter` as
  `next`/`edit`/`submit`/`confirm` as appropriate.

Likely upstream-equivalence signals that require the human gate:

- anomalyco/opencode#43376, #33848, #30393, or #28112 are fixed on the tracked
  branch;
- anomalyco/opencode#42493 (`packages/tui/src/routes/session/form.tsx`, currently
  `v2`-only) or an equivalent question form rewrite lands on `dev`;
- question mode stops replacing the base keymap layer, restoring the global
  `app.exit` binding.

### `patches/opencode-question-enter-submit.patch`

Applied immediately after `opencode-question-submit-exit.patch` and generated
against its post-image. Repairs a separate Enter-handling defect in the same
component. Since v2 it targets `form.tsx`: `commitEdit()` is the editing
layer's former inline `return` body, and both the textual-field and
custom-answer textareas pass `onSubmit={() => commitEdit()}`. v2 routes
focused-textarea keys through a keymap-managed textarea layer, which may make
this redundant; it is kept because the `onSubmit` path is cheap and was not
verified interactively.

Intent: a focused `@opentui` textarea consumes Enter internally before the key
reaches the question-mode keymap, so the declarative `key: "return"` binding in
the editing layer never fires and a typed custom answer cannot be submitted.
This is most visible on terminals that negotiate the Kitty keyboard protocol
(Ghostty, kitty, WezTerm), where Enter arrives as an enhanced CSI-u sequence and
only ever inserts newlines.

Required behavior:

- The commit logic is factored into a single `commitEdit()` helper rather than
  living inline in the `return` binding.
- The custom-answer `<textarea>` passes `onSubmit={() => commitEdit()}`. This is
  the primary submit path and mirrors `packages/opencode/src/cli/cmd/run/
  footer.question.tsx`, which already binds `onSubmit` to its `saveCustom`.
- The editing layer's `key: "return"` binding is retained as a fallback for
  terminals where the textarea lets Enter propagate, and delegates to the same
  `commitEdit()`.
- Empty-input, multi-select, and single-select commit semantics are unchanged
  from the pre-patch inline implementation.

Likely upstream-equivalence signals that require the human gate:

- `question.tsx` adopts `onSubmit` on the custom-answer textarea;
- the question prompt is rebuilt on the run-mode footer component, which already
  handles this correctly;
- `@opentui` stops consuming Enter in focused textareas, or the keymap gains
  priority over focused-renderable key handling.

### `patches/opencode-permission-enter-submit.patch`

Intent: the same focused-textarea Enter defect as above, in the permission
reject-reason prompt. `RejectPrompt`'s `<textarea>` in
`packages/tui/src/routes/session/permission.tsx` passes
`onSubmit={() => props.onConfirm(input.plainText)}`; the `return` binding stays.
Upstream-equivalence signal: that textarea gains `onSubmit` upstream.

### `patches/opencode-numpad-enter.patch`

Intent: keypad Enter (`kpenter`) acts like `return` everywhere Enter confirms.
Since v2 this is one binding expander in `packages/tui/src/context/keymap.tsx`
that gives every bare `return` binding a `kpenter` twin (covering all keymap
layers, config keybinds and the managed textarea layer), plus the raw
`event.name === "return"` checks in the mini footers (`footer.command.tsx`,
`footer.permission.tsx`, `footer.form.tsx`). Upstream-equivalence signal: the
keymap or OpenTUI treats `kpenter` as `return` globally.

## Retired patches

These files remain in `patches/` but are not in `opencodePatches`. They were
retired at 07bbd0f (dev became the v2 CLI) because upstream implements the
same behavior for every configuration v2 can express:

- `opencode-hidden-agent-variants.patch`: v2 agents carry the variant inside
  their model ref (`model#variant`, or legacy `model` + `variant` migrated by
  `core/src/config/plugin/agent.ts`). Compaction resolves the compaction
  agent's own model ref (`core/src/session/compaction.ts`) and title uses
  `agent.model?.variant` (`core/src/session/context.ts`), so a configured
  hidden-agent variant already wins; agents without one inherit as before. v2
  drops a `variant` with no `model` ("omitted unsupported legacy model
  variant"), so the v1 variant-without-model case can no longer be configured.
- `opencode-agent-variant-defaults.patch`: `preferredSelection` in
  `packages/tui/src/context/local.tsx` already orders saved per-model variant
  (including explicit `default`), then the agent's configured variant when its
  model matches, then the configured default model's variant.

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
rg 'auto_scroll|tolerance|scroll: Schema' packages/tui/src/config
rg 'paddingLeft|paddingRight|paddingTop|paddingBottom|marginTop|gap=' packages/tui/src
rg 'session\?\.sidebar' packages/tui/src
rg 'const enter =|resolved Plan permissions' packages/core/src/plugin/plan.ts
rg 'agent\.model|variant' packages/core/src/session/compaction.ts packages/core/src/session/context.ts
rg 'ownsSession|parentOf' packages/cli/src/run/noninteractive.ts
rg 'family\(|descendantSessionIDs' packages/tui/src/routes/session/index.tsx packages/client/src/solid/data.ts
rg 'kpenter' packages/tui/src
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

Then build the whole system, which also covers `opencode2` and everything else
the lock update moved:

```bash
just build-no-commit   # alias: just build-only — iterate with this
just build             # once it passes — records the build commit
```

Both run `simple-rebuild.sh --build-only` (no sudo). `build-no-commit` adds
`--no-commit`: it still runs `git add .` (flakes only see tracked files) but
skips the `<host> build <store path>` marker commit, so failed attempts leave no
commits. Use `just build` exactly once, after the build passes, to commit the
change. Neither activates; `just activate`/`just switch` need sudo and the user.

Run `git diff --check` and inspect the final diff. Do not run a full system switch
unless the user asks. Do not commit or push unless explicitly requested — a
user request to "use just build to commit" is that request.

## Recurring failure modes

- **`dev` replaced by the v2 CLI (1ddb087 → 07bbd0f).** `packages/opencode`
  was deleted; the build is `packages/cli`, server logic is `packages/core`,
  run mode is `packages/cli/src/run` plus `packages/tui/src/mini`, and
  question prompts are `routes/session/form.tsx`. Nix reports only the first
  failing file; apply every patch with GNU patch in a scratch tree to see the
  real extent. Each patch was ported or retired by behavior, not by hunk.
  `.#opencode` and `.#opencode2` now build the same CLI from different
  branches.

- **`opencode-node_modules` hash mismatch on aarch64-darwin.** `flake.nix`
  overrides upstream's `nix/hashes.json` hash for `aarch64-darwin` because
  `pkgs/bun-bin` may differ from upstream's bun. Take the `got:` value; compare
  with upstream `nix/hashes.json` at the new rev (they matched at `a79ecfe`).
- **`bun: command not found: codesign`.** Since anomalyco/opencode#52183,
  `packages/opencode/script/build.ts` ad-hoc re-signs the darwin binary.
  Upstream `nix/opencode.nix` lacks codesign; the override adds
  `pkgs.darwin.sigtool` (codesign) and `pkgs.cctools` (codesign_allocate,
  else "Failed to spawn codesign_allocate"). Verify with
  `codesign --verify --strict` on the built binary.
- **`opencode2` `bun install`: "blocked by minimum-release-age".** Upstream
  `bunfig.toml` sets `minimumReleaseAge = 259200` and bun 1.3.14 enforces it
  even for lockfile-pinned versions, so a same-day dependency bump fails.
  `pkgs/opencode2/package.nix` deletes that line inside the fixed-output
  node_modules build; then update `outputHash` from `got:`.
- **Unrestricted `nix flake update` moves `sidepulse-src`.** Its lock pin is the
  revision the sidepulse patches target (`patches/sidepulse-pr-*` for
  upstream PRs, `patches/sidepulse-dezren39-nix-*` for local ones; list in
  `pkgs/sidepulse/default.nix`). If they stop applying, restore the previous
  `sidepulse-src` lock node rather than porting the stack unasked. When asked
  to port: check with GNU patch (`nixpkgs#gnupatch`), not macOS
  `/usr/bin/patch`, which prints "failed" in lowercase and fuzzes differently.
  Regenerate every patch at zero fuzz from one commit per patch, and read
  every hunk that needed fuzz — at 6d55225 a fuzz-2 hunk silently reordered
  the LED writer fallbacks. Porting history: dezren39/nix#327.

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
