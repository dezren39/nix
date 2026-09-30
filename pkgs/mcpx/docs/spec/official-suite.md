# The official conformance suite, run against mcpx

```
created:      2026-09-30T12:00:00-05:00
last-updated: 2026-09-30T16:00:00-05:00
increment:    2
status:       standard
tags:         area:protocol, area:spec
description:  what modelcontextprotocol/conformance says about mcpx as a
              server and as a client, which failures are defects and which
              are the suite asking a gateway for tools it does not have.
```

`modelcontextprotocol/conformance` (npm `@modelcontextprotocol/conformance`)
is the suite the specification's own process leans on: SEP-2484 makes a
merged scenario a condition of a Standards Track SEP reaching Final, and
SEP-1730 grades the official SDKs by it. Its `requirements/<revision>.yaml`
sets are frozen, so a number from one of them means the same thing next year.
The 2026-07-28 scenarios are on the alpha line (`0.2.0-alpha.11`, suite commit
`7169291`); `latest` is a 0.1.x that predates that revision.

`scripts/conformance.sh` runs it:

```
CONFORMANCE_DIR=/path/to/conformance scripts/conformance.sh [out-dir]
```

- **Server leg.** `mcpx daemon --port` with `internal/testsupport/fakemcp` as
  its one upstream, scenarios pointed at `/mcp`. The suite has no stdio server
  mode, so `mcpx serve` is not covered here; `schema_sweep_test.go` covers it
  instead, validating every frame mcpx sends against each revision's schema.
- **Client leg.** `internal/conformance/officialclient` writes an isolated
  config naming the scenario server as an HTTP upstream and drives the real
  `mcpx` binary against it, so everything the scenario sees on the wire came
  from mcpx's own client.

Every daemon the script starts is stopped on exit.

## Results

Run on `main` at `a93be43` (after #283 and #284), merged into this branch.
Raw output is per leg in the out-dir (`<leg>/out.txt`,
`<leg>/results/**/checks.json`), and `failures.txt` has one line per failed
check.

| leg | at `05c78b2` (first run) | at `408bc2b` | at `a93be43` |
| --- | --- | --- | --- |
| server `--requirements 2025-11-25` | 47 passed, 19 failed | 45 / 21 | **45 / 21** |
| server `--requirements 2026-07-28` | 60 / 104 | 117 / 54 | **110 / 62** |
| server `--suite all` | 84 / 106 | 136 / 61 | **131 / 67** |
| client `--requirements 2025-11-25` | 5 / 64 | 20 / 56 | **20 / 56** |
| client `--requirements 2026-07-28` | 23 / 84 | 62–63 / 68–69 | **63 / 68** |

**The server numbers went down because two passes were false.** The suite's
`tools-call-simple-text` and `tools-call-error` call fixture tools
(`test_simple_text`, `test_error_handling`) and accept any text or any
`isError` result. mcpx answered an unknown tool with an `isError` result, so
both "passed" against tools that do not exist. #284 made an unknown tool the
`-32602` every revision's tools page asks for, and both now fail honestly, as
fixture failures, in both sets. The same change turned the tasks scenarios'
"did not create a task" into "no tool named …", which splits some of them
into more failed checks. In the other direction, `resources-subscribe` and
`-unsubscribe` pass again (#251: a legacy subscription always succeeds), and
`tasks-methods-non-declaring` passes (`-32021`). The #284 PR body reports
2025-11-25 47/19 and 2026-07-28 118/53; this run, on the merged tree, does not
reproduce them, and the scenario lists above are why.

The first run found, among others: mcpx could not reach any real modern
server, because both halves read and wrote `protocolVersions` where the
schema says `supportedVersions` (#224); no `Origin` check (#198); a terminated
session still answered (#198); every 2026 list and read result without
`ttlMs`/`cacheScope` (#232); `-32022` for a legacy `initialize` it should have
counter-offered (#232); a `-32020` where `-32602` was right, and five methods
2026-07-28 removed still answered to modern peers (#250). Every one of those is
fixed, and the suite no longer reports it.

## What the remaining failures are

mcpx is a gateway. It publishes its own small tool set (`mcpx_call`,
`mcpx_exec`, discovery) and namespaces every upstream resource as
`mcpx://<namespace>/<uri>`. Most server scenarios assume the suite's own
fixture surface -- tools called `test_simple_text`, `slow_compute`, `greet`,
`confirm_delete`, `test_missing_capability`; prompts called
`test_simple_prompt`; resources under `test://` -- and fail before they reach
a protocol assertion. Those failures say nothing about conformance.

**Every remaining server failure, in both sets, is of that kind.** Each
message names the missing fixture: `no tool named "…"`, `no prompt named "…"`,
`a resource URI looks like mcpx://<namespace>/<uri>, got "test://…"`, or the
suite's own "Not testable: server does not list the diagnostic tool …". One
2026-07-28 warning is fixture-bound too: the suite mutates its own prompt list
and waits for `notifications/prompts/list_changed` on a listen stream, and
mcpx's prompt list is its upstreams'.

Of the four defects #252 named, one was fixed (the tasks extension's methods
answer `-32021` to a client that did not declare it, #284), one is a decision
(`-32021` for a tool that might ask: mcpx sends the question to the broker
instead, [`../protocol.md`](../protocol.md) §3.1), and two were fixture-bound
all along: "no server-directed task creation" is not missing -- `maybeTask` in
`internal/mcpserver/tasks.go` answers a declaring client's `tools/call` with
`resultType: "task"` once it has run longer than `protoMessages.taskAfter` --
and "`Mcp-Method` changes the result content"
(`tasks-headers-tolerate-mcp-method-on-tools-call`) compares the text against
`Hello, sep-2243!` from the fixture tool `greet`
(`src/scenarios/server/tasks/headers.ts` in the suite).

A few failures carry a second, real cause behind the fixture one:
`tools-call-with-logging` and `tools-call-with-progress` could not pass with
the fixture either, because mcpx relays neither `notifications/message` nor
`notifications/progress` to a host (#212).

### Client, both sets

Every remaining failure is `auth/*`: mcpx has no OAuth client (#253), and its
per-server `auth` block is parsed and never applied (#240).

One non-auth scenario is **flaky, and the flake is in the adapter, not in
mcpx**. `json-schema-ref-no-deref` only checks that the client listed tools
without dereferencing a network `$ref`. The adapter lists by running
`mcpx ls` and assumes that sends `tools/list` ("Listing forces the daemon up,
the upstream connected, and tools/list sent",
`internal/conformance/officialclient/main.go`). It does not: `CmdLs` is
answered from the schema cache and starts no server (`internal/cli/commands.go`,
`CmdLs` doc comment). The scenario passes only when the daemon's background
warm-up happens to list before the adapter stops it -- one run in three at
`408bc2b`, and this run. The fix is for the adapter to force a listing
(`mcpx search ""`, as it does for every other scenario).

## What the suite does not see

- `mcpx serve` over stdio: no stdio server mode in the suite.
- Legacy revisions other than 2025-11-25: there are no requirement sets for
  them. `internal/mcpserver/requirements_test.go` and the schema sweep cover
  2024-11-05 through 2025-06-18.
- Anything behind a fixture tool: the MRTR round trip, the tasks lifecycle and
  `x-mcp-header` validation are exercised by mcpx's own tests
  (`internal/e2e/execask_test.go`, `internal/mcpserver/requirements_test.go`,
  `internal/mcpserver/transport_test.go`) against real upstreams instead.
  A fixture upstream that exposes the suite's names through mcpx would move
  most of these into the passed column without changing a line of mcpx;
  it has not been built.

The per-requirement status, with the test that verifies each row, is
[conformance-matrix.md](conformance-matrix.md).
