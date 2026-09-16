---
description: Bring an existing session's history into the current directory, e.g. a worktree you just created with /warp.
---

# Warp Import

Move an existing session's conversation history into the **current working
directory**. Intended for use right after `/warp` has put you in a fresh
worktree: you get the sandbox from `/warp`, and this brings the old context in.

## Prefer the plugin when it is installed

If the `warp_import` tool is available, use it instead of the manual steps
below. It does the whole thing in one call — creates the worktree workspace,
relocates the session into it, and switches the TUI to that session — so the
`/new` + `/warp` + `/warp-import` + `/sessions` dance is not needed.

    warp_import(session: "<id or search term>", branch: null, use_current_directory: false)

Source lives at `plugins/warp-import.js` in this repo and is installed with
`just symlink-plugins`. It falls back to the plain export+import flow described
below if the workspace API is unavailable, and it reports when it did so.

The same confirmation duties apply: the tool still performs a **move**, not a
copy, and the result is still un-warpable afterwards. Confirm with the user
before calling it.

If the tool is not present, follow the manual instructions below.

## Arguments

$ARGUMENTS - Optional. A search term matched against session titles, or a full
session id (`ses_...`).

Examples:
- `sidepulse install`
- `the nixos inputs one`
- `ses_f96329702ffeNTPw1sbIAyv884`

If no arguments are given, list recent sessions and ask the user to choose.

## Before you start: what this actually does

Read this and make sure the user understands it, because it is not a copy.

`opencode import` performs an upsert keyed on the session id
(`onConflictDoUpdate` on `SessionTable.id`). Verified behaviour:

- The session is **relocated**, not duplicated. Its `directory` is rewritten to
  the cwd where `opencode import` runs. The original location loses it.
- Only `project_id`, `directory`, and `path` are updated. `workspace_id` is
  never set, so the session sits in the right directory but is not formally
  attached to the workspace `/warp` created. It may not group under that
  workspace in the UI.
- The imported session has **zero event rows** (import writes `session`,
  `message`, and `part` only). Warp reads events and fails with
  `SessionEventsNotFoundError` when there are none, so **an imported session can
  never itself be warped afterwards.**

None of this loses conversation content: messages and parts transfer in full.

## Instructions

1. **Confirm where you are.** Run `pwd`. If it is the same directory the target
   session already lives in, importing is a no-op with extra steps — stop and
   say so. Normally you should be inside a worktree created by `/warp`.

2. **Find the session.**

   ```bash
   opencode session list --format json
   ```

   Returns `[{id, title, updated, created, projectId, directory}]`, scoped to the
   current project. Note `updated`/`created` are epoch **milliseconds**.

   - If `$ARGUMENTS` looks like a session id (`^ses_`), use it directly, but
     still look it up so you can show the user its title before proceeding.
   - If `$ARGUMENTS` is a search term, filter on `title` case-insensitively.
     - Exactly one match: use it, but state which one you picked.
     - Several matches: show them with title + human-readable `updated`, and ask
       the user which one.
     - No matches: show the 10 most recent and ask.
   - If `$ARGUMENTS` is empty: show the 10 most recent and ask.

   Do not run bare `opencode export` with no id — it opens an interactive
   picker that you cannot drive from here. Always resolve an explicit id first.

3. **Confirm with the user before writing anything.** State plainly:
   - the session title and id,
   - the directory it will move **from** and **to**,
   - that the move is not a copy, and that the session will no longer be
     warpable afterwards.

   Wait for confirmation.

4. **Export, then import.**

   ```bash
   opencode export <SESSION_ID> > /tmp/warp-import-<SESSION_ID>.json
   opencode import /tmp/warp-import-<SESSION_ID>.json
   ```

   `export` writes JSON to stdout and progress to stderr, so the redirect is
   clean. `import` takes a **file path**, so the intermediate file is required —
   you cannot pipe one into the other.

   Run `import` from the directory you want the session to end up in. If you are
   unsure the shell is in the right place, use an explicit
   `cd <dir> && opencode import ...`.

   Add `--sanitize` to the export if the user asks to redact transcript and file
   data.

5. **Verify and report.**

   ```bash
   opencode session list --format json
   ```

   Confirm the session's `directory` is now the cwd. Report the new location and
   the message count. Then delete the temp JSON file — these can be large
   (hundreds of KB to several MB).

## Notes

- Pruned sessions import fine. `export` reads the `message`/`part` projections,
  never the `event` table, so `opencode-db-prune.sh` has no effect on this flow.
- If the user's real goal is a *copy* rather than a move, this command cannot do
  it — the id-keyed upsert means a second copy needs the ids rewritten in the
  JSON first. Say so rather than improvising, since message/part id collisions
  are involved.
