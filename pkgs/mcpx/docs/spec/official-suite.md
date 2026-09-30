# The official conformance suite, run against mcpx

```
created:      2026-09-30T12:00:00-05:00
last-updated: 2026-09-30T12:00:00-05:00
increment:    1
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

Run on `main` at `408bc2b`. Raw output is per leg in the out-dir
(`<leg>/out.txt`, `<leg>/results/**/checks.json`), and `failures.txt` has one
line per failed check.

| leg | at `05c78b2` (first run) | now |
| --- | --- | --- |
| server `--requirements 2025-11-25` | 47 passed, 19 failed | **45 / 21** |
| server `--requirements 2026-07-28` | 60 / 104 | **117 / 54** |
| server `--suite all` | 84 / 106 | **136 / 61** |
| client `--requirements 2025-11-25` | 5 / 64 | **20 / 56** |
| client `--requirements 2026-07-28` | 23 / 84 | **63 / 68** (62 / 69 in two of three runs; see below) |

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
fixture surface -- tools called `slow_compute`, `confirm_delete`,
`test_missing_capability`, `json_schema_2020_12_tool`; prompts called
`test_simple_prompt`; resources under `test://` -- and fail before they reach
a protocol assertion. Those failures say nothing about conformance. Reading
them as defects would be weeks of work for two fixes.

### Server, 2026-07-28 (54)

| class | checks | scenarios |
| --- | --- | --- |
| fixture absent | 52 | `tools-call-{image,audio,embedded-resource,mixed-content,with-progress}`, `prompts-get-*`, `resources-read-{text,binary}`, `resources-templates-read`, `completion-complete` (no such prompt: `-32602`, which is the correct answer), `json-schema-2020-12`, every `input-required-result-*` that needs a fixture tool to elicit, `http-custom-header-server-validation` (no tool carries `x-mcp-header`), and most `tasks-*` ("slow_compute did not create a task") |
| **defect** | 2, and one found reading them | below |

#252 names four. Read against the code and the scenario sources, two hold:

1. **`-32021` is never raised.** mcpx never refuses a call for a capability
   the request did not declare: a question the client cannot receive goes to
   the broker instead (the `32021-broker-fallback` row of #257). Whether that
   fallback discharges the MUST is the open question; the suite's
   `sep-2575-server-rejects-undeclared-capability` cannot settle it, because
   it calls a fixture tool mcpx does not have.
2. **The tasks extension does not gate on its own capability.**
   `sep-2663-tasks-methods-non-declaring`: `tasks/get`, `tasks/update` and
   `tasks/cancel` from a client that did not declare
   `io.modelcontextprotocol/tasks` get `-32602` (no such task) where the SEP
   says `-32021`. This one needs no fixture.

The other two are fixture-bound. "No server-directed task creation" is not
missing: `maybeTask` in `internal/mcpserver/tasks.go` answers a declaring
client's `tools/call` with `resultType: "task"` once it has run longer than
`protoMessages.taskAfter`, and the scenarios name `slow_compute`, which is not
there to run long. "`Mcp-Method` changes the result content"
(`tasks-headers-tolerate-mcp-method-on-tools-call`) compares the text against
`Hello, sep-2243!` from the fixture tool `greet`
(`src/scenarios/server/tasks/headers.ts` in the suite); the header is not
involved.

**Found while checking them: an unknown tool is a result, not an error.**
`tools/call` naming a tool mcpx does not have is answered
`{isError: true, content: [{text: "no tool named …"}]}`
(`internal/mcpserver/server.go`, the `tools/call` case, via `dispatch`). Every
revision's tools page lists "Unknown tools" among the *protocol* errors, with a
`-32602` example
([2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25/server/tools#error-handling),
[2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/server/tools#error-handling)).
It is also why several fixture scenarios fail as "the server executed it"
rather than "the server refused it".

One warning is fixture-bound too: the suite mutates its own prompt list and
waits for `notifications/prompts/list_changed` on a listen stream, and mcpx's
prompt list is its upstreams'.

### Server, 2025-11-25 (21)

19 are the fixture tools, prompts and resources above, and two carry a second,
real cause behind the fixture one: `tools-call-with-logging` and
`tools-call-with-progress` could not pass with the fixture either, because
mcpx relays neither `notifications/message` nor `notifications/progress` to a
host (#212).

The other two are `resources-subscribe` and `resources-unsubscribe`, which
passed before #247 and fail since: the scenario subscribes to
`test://watched-resource`, which no configured upstream owns, and mcpx now
refuses a subscription it cannot deliver instead of acknowledging it and
staying silent. That is the honest answer replacing a dishonest pass, and
whether an unowned URI should be refused or accepted is the open decision in
#251.

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
warm-up happens to list before the adapter stops it -- one run in three here.
The fix is for the adapter to force a listing (`mcpx search ""`, as it does
for every other scenario).

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
  most of those 52 into the passed column without changing a line of mcpx;
  it has not been built.

The per-requirement status, with the test that verifies each row, is
[conformance-matrix.md](conformance-matrix.md).
