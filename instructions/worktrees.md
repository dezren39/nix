# Git worktrees: never under /var/

`/var/folders/.../T/` is the macOS per-user temp directory. **It is swept
periodically by the OS.** A sweep has already destroyed ~3000 tracked files
across several active worktrees in a single session, and it does so silently —
`git status` keeps reporting a clean tree from a stale fsmonitor index while the
files are gone.

The environment block advertises a `/var/folders/.../T/opencode` path as
"pre-approved for external directory access". That approval is about
*permissions*, not *durability*. Use it for scratch output you are willing to
lose. **Never create a git worktree there.**

## Where worktrees go

Create them under the durable opencode worktree root:

```
~/.local/share/opencode/worktree/<descriptive-name>
```

```bash
git -C <main-clone> worktree add \
  ~/.local/share/opencode/worktree/<descriptive-name> \
  -b <branch> origin/main
```

Pick a name nobody else is using. One worktree per agent per task — do not
create several for the same job, and do not work in another agent's.

## Never work in the session's default worktree

The user and other agents share it. Edits there have been silently clobbered,
including a regression test that was only recovered because a reviewer noticed
it missing. Create your own and stay in it.

## Worktrees share one remote

All worktrees of a clone share the parent's `origin`. If that URL is wrong,
**every** worktree is wrong at once, and a single `git remote set-url` on the
parent fixes all of them.

Verify immediately before every push:

```bash
git remote get-url origin
```

If it does not name the repository you are working on, **do not push**. Report
it — a repoint is global corruption, not a local mistake.

## Staging

Never `git add .`. Other sessions may have left unrelated edits in a shared
tree, and unrelated in-flight work has been swept into commits this way. Stage
only your own files by explicit path.

## Forbidden

`git reset --hard`, `git restore`, `git checkout -- <path>`, and `git push
--force` are forbidden unless the user explicitly asks for them by name. When a
worktree is damaged, recover non-destructively — `git checkout-index -a` or
`git archive` from a known-good ref — rather than resetting.
