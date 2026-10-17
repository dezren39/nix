import { mkdirSync } from "node:fs"

/**
 * Move the shell's temp directory off the macOS per-user temp path.
 *
 * On macOS `TMPDIR` is `/var/folders/<...>/T/`, which the OS sweeps
 * periodically. opencode advertises `$TMPDIR/opencode` to agents as the
 * pre-approved scratch location, and agents reasonably read that as "a place to
 * put things" — including git worktrees. A sweep then deletes tracked files out
 * from under active work: ~3000 files across several worktrees in one session,
 * silently, while `git status` still reported clean from a stale fsmonitor
 * index.
 *
 * This hook repoints `TMPDIR` (and the `TMP`/`TEMP` aliases some tools read) at
 * a durable path under the opencode data directory. Ordinary scratch files keep
 * working exactly as before; they just survive.
 *
 * OpenCode v2: the v1 `shell.env` hook is now `ctx.shell.hook("create.before")`,
 * which receives the command, cwd, timeout, shell and a mutable `env` for every
 * shell process the shell tool spawns. v2 plugins no longer get Bun's `$`, so the
 * directory is created with mkdirSync.
 *
 * Note this changes what `$TMPDIR` resolves to inside shell commands. The
 * literal path printed in the shell tool description is computed by opencode
 * from the parent process environment, so to change that text as well, export
 * TMPDIR before launching opencode.
 */

const DURABLE_TMP = `${process.env.HOME}/.local/share/opencode/tmp`

export default {
  id: "durable-tmpdir",
  async setup(ctx: any) {
    try {
      mkdirSync(DURABLE_TMP, { recursive: true })
    } catch {}

    await ctx.shell.hook("create.before", (input: any) => {
      input.env.TMPDIR = DURABLE_TMP
      input.env.TMP = DURABLE_TMP
      input.env.TEMP = DURABLE_TMP
    })
  },
}
