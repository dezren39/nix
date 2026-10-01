# In name only

```
created:      2026-10-01T09:00:00-05:00
last-updated: 2026-10-01T09:00:00-05:00
increment:    1
status:       working
tags:         area:audit
description:  features, settings and tests that exist on paper and do
              little or nothing in practice. A running list; the full audit
              is still to come.
```

Each entry is something a reader could reasonably believe works, and mostly
does not. It is a list to fix from, not a list of excuses. Check an entry against the code before adding it -- two seeded here from
`docs/spec/revision-conflicts.md` were already fixed. Add to it when you
find one; remove an entry only in the change that makes it true, and say so in
the commit.

**Status of this file:** seeded from what the 2026-09-30/10-01 fix rounds
found while doing other work. The full audit -- every command, flag, setting,
environment variable and surface checked for whether it changes behaviour --
has not been done yet.

Legend: **absent** -- declared, nothing behind it. **minimal** -- does the
least that lets a test or a doc say it exists. **partial** -- works on some
paths and silently not on others.

## Authorization

| what | state | detail |
| --- | --- | --- |
| OAuth as a client (to remote servers) | absent | `type: oauth` is refused before connecting. Every client-side conformance failure is this: token endpoint, metadata discovery, DPoP, client credentials. #253 |
| Authorization on mcpx's own `/mcp` and `/v1` | absent | Anyone who can reach the socket or port is trusted. The `Origin`/`Host` check stops browsers, not callers. #253 |
| The permissive auth interface | minimal | Authorizes everyone; exists so the shape is in place. #300 |
| `autonomy.max` against a determined caller | partial | Holds per request, but a socket caller can `PUT` a higher ceiling with `persist:user` and restart the daemon. Decision 0002 says so. |

## Questions from servers (elicitation, sampling)

| what | state | detail |
| --- | --- | --- |
| Elicitation storage | partial | Readable by any local user and never pruned. #255 |
| Elicitation answering UI | minimal | Does not show which server asks; answers are not checked against the requested schema. #255 |
| Sampling | minimal | Declared to every upstream; answering needs an agent watching the question queue. No human review, no result validation, no error mapping. Deprecated in 2026-07-28; kept working only. #256, #210 |
| Server-to-client requests across eras | absent | A caller on 2025-11-25 reaching an upstream mcpx talks to in 2026-07-28 never sees the upstream's elicitation or sampling. Four `server-all` conformance failures. |

## Tasks

| what | state | detail |
| --- | --- | --- |
| Task persistence | absent | The store is in memory; tasks do not survive a daemon restart. #209 |
| `tasks/cancel` | minimal | Forces the cancelled status instead of letting the work reach its own end. #209 |
| mcpx asking an upstream to run a call as a task | absent | An upstream that requires a task fails. #209 |
| `taskSupport` default for 2025-11-25 clients | partial | A tool with none declared can still be called as a task. #209 |

## Relay between hosts and upstreams

| what | state | detail |
| --- | --- | --- |
| Log messages per call | partial | A log message does not say which call it belongs to; with several calls on one upstream connection, each gets it. |
| Upstream log level on legacy revisions | partial | Pinned to `info` at connect, so an upstream's debug messages never exist to relay. |
| Progress and mcpx's own timeout | partial | Progress reaches the host but does not extend mcpx's per-server call timeout. |
| Relay on `prompts/get` and `resources/read` | absent | Only `tools/call` relays progress and logs. |
| Client capabilities to 2025-11-25 upstreams | partial | Declared once at connection; per-request narrowing reaches 2026-07-28 upstreams only. |

## Commands and settings

| what | state | detail |
| --- | --- | --- |
| Restarting the daemon itself | absent | `restart` replaces server processes; nothing restarts the daemon. #302 |
| Natural language to script | minimal | `mcpx prompt` matches a recipe or drafts a script; no declared model source, no run path through the ceiling end to end. #294 |
| `mcpx.namespace` validation | partial | Checked for reserved words only, not for being a valid identifier. |
| Settings being read vs mattering | guard gap | `internal/settings/consumed_test.go` proves a setting is read, not that reading it changes anything. |
