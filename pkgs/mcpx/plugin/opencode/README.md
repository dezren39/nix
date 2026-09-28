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
| `MCPX_PLUGIN_ENV` | `full` | `minimal`, `standard` or `full` |
| `MCPX_PLUGIN_INSTRUCTIONS` | off | add mcpx usage to the system prompt |
| `MCPX_PLUGIN_TOOL_TIMING` | off | record opencode's tool timings into mcpx's log |

`full` is the default: everything already in hand, plus a session lookup done
once and cached.

`standard` drops the lookup, for anyone who would rather not have that request
at all. `minimal` is the session id alone — the only part leasing strictly
requires.

## What gets injected

`minimal`:

```
MCPX_SESSION_ID
```

`standard` adds everything the hook is handed directly, plus what the plugin
knew at startup:

```
MCPX_OPENCODE_CWD, MCPX_CALL_ID, MCPX_PROJECT_ID
MCPX_OPENCODE_DIRECTORY, MCPX_OPENCODE_WORKTREE, MCPX_WORKTREE_NAME
MCPX_HARNESS, MCPX_HARNESS_VERSION, MCPX_HARNESS_PID, MCPX_HARNESS_STARTED
MCPX_SHELL_SEQ
MCPX_TRACE_IDS          [["session_id","abc"],["call_id","x"],["worktree","/p"]]
```

`full` adds what needs a lookup, done once per session and cached:

```
MCPX_PARENT_SESSION_ID, MCPX_SESSION_TITLE, MCPX_SESSION_DIRECTORY
MCPX_SESSION_VERSION, MCPX_SESSION_DEPTH
MCPX_SESSION_CREATED, MCPX_SESSION_AGE_MS
```

and extends `MCPX_TRACE_IDS` with `parent_session_id` and the full `ancestry`.

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
