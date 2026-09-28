# mcpx session plugin for opencode

Tells mcpx which opencode session it is working for.

## Why

mcpx leases MCP servers per session, so two agents running at once get separate
processes instead of corrupting one shared browser. That only works if mcpx can
tell the sessions apart, and nothing in a shell command carries that: the agent
does not know its own session id, and asking it to pass one would spend tokens
on plumbing and be forgotten half the time.

So the harness supplies it. Every shell command gets a few environment
variables, mcpx reads them, and the agent never learns any of it happened.

## Install

```sh
mkdir -p ~/.config/opencode/plugin
cp mcpx-session.ts ~/.config/opencode/plugin/
```

## Configuration

Everything past the session id is opt-in.

| Variable | Default | Effect |
| --- | --- | --- |
| `MCPX_PLUGIN_ENV` | `standard` | `minimal`, `standard` or `full` |
| `MCPX_PLUGIN_INSTRUCTIONS` | off | add mcpx usage to the system prompt |
| `MCPX_PLUGIN_TOOL_TIMING` | off | record opencode's tool timings into mcpx's log |

`minimal` is the session id alone — the only part leasing strictly requires.
`standard` adds parent session, directories, harness version and trace ids.
`full` adds agent, model and a per-process command counter.

## What gets injected

`standard`:

```
MCPX_SESSION_ID, MCPX_PARENT_SESSION_ID
MCPX_OPENCODE_DIRECTORY, MCPX_OPENCODE_WORKTREE, MCPX_WORKTREE_NAME
MCPX_TRACE_IDS          [["session_id","abc"],["worktree","/path"]]
MCPX_HARNESS, MCPX_HARNESS_VERSION, MCPX_HARNESS_PID, MCPX_HARNESS_STARTED
```

`full` adds `MCPX_AGENT`, `MCPX_MODEL`, `MCPX_MODEL_PROVIDER`,
`MCPX_MODEL_VARIANT`, `MCPX_SHELL_SEQ`.

### Why so many

Environment variables are not context. The model never sees them, and an unread
one costs a few bytes. So anything cheap goes in, on the reasoning that a
variable nobody reads is cheaper than a variable that is missing.

### Why not more

Transcript lengths, token counts and cost would each mean a database query per
shell command — a real cost paid on every invocation for a number almost nobody
reads. Those come from `mcpx stats --opencode`, which asks once.

## Trace ids

`MCPX_TRACE_IDS` is an array of pairs rather than a flat id, because a flat one
cannot say "this session, whose parent is that one, in this worktree". A reader
looks up the keys it knows and ignores the rest, so the list can grow without
any consumer changing.
