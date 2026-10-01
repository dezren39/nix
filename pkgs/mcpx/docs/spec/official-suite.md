# The official conformance suite, run against mcpx

```
created:      2026-09-30T12:00:00-05:00
last-updated: 2026-09-30T23:00:00-05:00
increment:    3
status:       standard
tags:         area:protocol, area:spec
description:  what modelcontextprotocol/conformance says about mcpx as a
              server and as a client, measured against the suite's own
              fixture server behind mcpx in pass-through mode, and what each
              remaining failure is.
```

## In CI

`.github/workflows/mcpx-conformance.yml` runs every leg on each pull request and push to `main`
that touches mcpx -- one job per leg, about a minute each. A leg is one side of the protocol (mcpx
as a server, or as a client) checked against one rule set; the suite has rule sets for 2025-11-25
and 2026-07-28 only, plus `server-all`, every server scenario it has. A leg's job fails when any
check fails, lists the failures on the run's summary page, and uploads them as an artifact.
**It is not a merge block**: no branch protection requires it.

On `main` each leg's counts are compared with two records kept in the repository variable
`MCPX_CONFORMANCE_<LEG>` -- the best failed/total ratio and the largest total -- and a run that
goes backwards on either gets a large warning on its summary. The rules are in
`scripts/conformance-record.py`. Saving the records needs the secret
`MCPX_CONFORMANCE_VARS_TOKEN`, a fine-grained token with *Variables: read and write* on this
repository; GITHUB_TOKEN cannot write variables.

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

- **Server leg.** The suite's own reference fixture,
  `examples/servers/typescript/everything-server.ts` in the clone, started the
  way its `package.json` starts it (`tsx everything-server.ts`, `PORT` set),
  sits behind `mcpx daemon --port … --passthrough demo`. Pass-through
  (`mcp.passthrough`, [`../protocol.md`](../protocol.md) §2.5) offers the
  fixture's tools, prompts and resources under their own names, so a scenario
  asking for `test_simple_text` or `test://static-text` gets them through mcpx
  and its checks reach mcpx's protocol handling. The 2025-11-25 leg has its own
  daemon that talks 2025-11-25 to the fixture as well: the fixture's legacy
  tools push requests to their client, which only a legacy session carries.
  `--suite all` and 2026-07-28 use a daemon that talks 2026-07-28 upstream. The
  suite has no stdio server mode, so `mcpx serve` is not covered here;
  `schema_sweep_test.go` covers it instead.
- **Client leg.** `internal/conformance/officialclient` writes an isolated
  config naming the scenario server as an HTTP upstream and drives the real
  `mcpx` binary against it, so everything the scenario sees on the wire came
  from mcpx's own client.

Every daemon and the fixture are stopped on exit.

**Where the tasks fixtures come from.** The tasks-extension scenarios
(`src/scenarios/server/tasks/*.ts`) ask for `greet`, `slow_compute`,
`failing_job`, `protocol_error_job`, `confirm_delete`, `multi_input` and
`test_tool_with_task`. Nothing in the suite defines them: `everything-server.ts`
does not, and the suite's `known-sdks.ts` points each SDK at its *own*
conformance server (`test/conformance/src/everythingServer.ts` in
typescript-sdk, `conformance/everything-server` in go-sdk). They are SDK
fixtures. mcpx forwards a tool call to its upstream and does not invent tools,
so those checks fail here as `no tool named "…"` and are listed as such below,
not counted as passes by any substitute.

## Results

Every column is a run of `scripts/conformance.sh` against a daemon built from
that commit. Raw output is per leg in the out-dir (`<leg>/out.txt`,
`<leg>/results/**/checks.json`), and `failures.txt` has one line per failed
check. Columns before `64f3471` put mcpx's test fake (one tool, `echo`) behind
the daemon; `64f3471` is that setup re-measured as the "before" of this
change; the last column is the fixture in pass-through.

| leg | `05c78b2` (first run) | `408bc2b` | `a93be43` | `60068c6` | `64f3471` (fake) | **this change (fixture, pass-through)** |
| --- | --- | --- | --- | --- | --- | --- |
| server `--requirements 2025-11-25` | 47 passed, 19 failed | 45 / 21 | 45 / 21 | 45 / 21 | 45 / 21 | **78 / 3** |
| server `--requirements 2026-07-28` | 60 / 104 | 117 / 54 | 110 / 62 | 109 / 62 | 110 / 62 | **146 / 39** |
| server `--suite all` | 84 / 106 | 136 / 61 | 131 / 67 | 128 / 67 | 131 / 67 | **167 / 44** |
| client `--requirements 2025-11-25` | 5 / 64 | 20 / 56 | 20 / 56 | 20 / 48 | 20 / 56 | **20 / 56** (3 runs, identical) |
| client `--requirements 2026-07-28` | 23 / 84 | 62–63 / 68–69 | 63 / 68 | 63 / 59 | 63 / 68 | **63 / 68** (3 runs, identical) |

Totals moved by more than the failures did, because the fixture makes the
suite run checks it skipped before (a scenario stops at its first missing
fixture). Read the failure lists, not the totals.

### What running against a real fixture found in mcpx

Each is fixed, with a test that fails without the fix:

- **An upstream's question never reached the client once two calls shared an
  instance.** A question raised by a 2026-07-28 upstream was attributed to the
  waiting call by `(server, instance)` alone; with a second call in flight on
  the instance -- one earlier client that never resumed was enough -- it was
  left to the broker, and the request hung until the suite's timeout ("This
  operation was aborted", 12 `input-required-result-*` checks). It is now
  attributed through the call's own context
  (`internal/daemon/routes_proto.go` `askFor`;
  `TestAQuestionOnACallsContextIsThatCalls`).
- **Several questions in one round reached the client one round at a time**,
  renamed to mcpx's ids. An upstream's `inputRequests` are now answered
  concurrently and relayed together under the upstream's own keys
  (`mcpclient.resolveInput`, `mcpserver.wireKey`;
  `TestInputRequestsOfOneRoundAreAskedTogetherUnderTheirKeys`,
  `TestInputRequestKeysKeepTheUpstreamsNames`).
- **Two rounds of one call got the same `requestState`** when minted within a
  second (`sep-2322-multi-round-r2`); the state now carries a nonce
  (`TestRequestStateDiffersPerRound`).
- **`resources/list` carried every resource template as a resource with an
  empty URI** (`resources-list`: "Resource 0: missing uri"); before pass-through
  it read `mcpx://<ns>/`, which no check caught
  (`TestResourcesListOmitsTemplates`).
- **A template's `{variables}` were percent-encoded** by the `mcpx://` rewrite,
  turning `…/{id}` into a fixed URI (`TestResourcesListOmitsTemplates`).
- **`resources/templates/list` on a cold daemon listed no server's templates**:
  it read the schema cache without filling it, as `resources/list` does
  (`TestTemplatesListedColdIncludeTheServers`).
- **A resource read answered on the ask path came back as
  `mcpx://mcpx://<server>//<uri>`** (`TestAskPathResourceURIsAreNamespacedOnce`).

And the pass-through mode itself: `TestPassthroughExposesOneUpstreamUnrenamed`,
`TestPassthroughToolQuestionsReachTheClient`.

**Unknown tool is `-32602`** (#284) on every revision mcpx serves:
`TestUnknownToolIsAProtocolError` (in-process, 2025-11-25 and 2026-07-28) and
`TestUnknownToolIsAProtocolErrorOverHTTP` (over `/mcp`, each of 2024-11-05,
2025-03-26, 2025-06-18, 2025-11-25 through its own session, and 2026-07-28),
both in `internal/mcpserver`, which CI's `go test ./...` runs.

### Client-leg flake, fixed in the adapter

`elicitation-sep1034-client-defaults` passed alone and failed in the parallel
`--requirements` run: the adapter looked for pending questions by running
`mcpx elicit list` every 200 ms, a process start per tick, and under load a
tick outlived the scenario. It now reads the broker's store in process every
20 ms and answers there (`officialclient` `answerElicitations`). Three
consecutive client runs: 20/56 and 63/68 each time, every non-`auth/*`
scenario passing in all three.

## What the remaining failures are

### Server, 2025-11-25 (3)

| scenario / check | message | kind |
| --- | --- | --- |
| `tools-call-with-logging` | No log notifications received | mcpx defect: no relay of upstream `notifications/message` (#212) |
| `tools-call-with-progress` | No progress notifications received | mcpx defect: no relay of upstream `notifications/progress` (#212) |
| `server-sse-polling` / `scenario-timeout` | did not complete within 30000ms | open: the fixture closes the POST stream to mcpx (SEP-1699); mcpx reconnects with `Last-Event-ID` (the fixture logs it) and no replayed response arrives, so the call never finishes. Not scored for 2025-11-25 (`pending`); root cause not established |

### Server, 2026-07-28 (39 failed, 2 warnings)

| scenario / check | message | kind |
| --- | --- | --- |
| `tasks-*` (26 checks across 8 scenarios) | `no tool named "slow_compute"` / `"greet"` / `"failing_job"` / `"protocol_error_job"` / `"confirm_delete"` / `"multi_input"` / `"test_tool_with_task"`, and "Not testable: no task was created by the preceding step" downstream of them | fixture missing: SDK fixtures, see above |
| `tasks-required-task-error` / `sep-2663-server-returns-missing-capability-when-required` | `failing_job` returned -32602; spec requires -32021 | fixture missing (an unknown tool is -32602, correctly) |
| `http-custom-header-server-validation` (5) | Not testable: server exposes no tool with `x-mcp-header` annotations | fixture missing: `everything-server.ts` has none |
| `tools-call-with-progress` | No progress notifications received | mcpx defect (#212) |
| `server-stateless` / `sep-2575-http-server-no-independent-requests-on-stream`, `sep-2575-server-no-log-without-loglevel` | no frames from the streaming / logging tool | mcpx defect: same relay gap (#212) |
| `server-stateless` / `sep-2575-server-sends-prompts-list-changed-on-subscription` (WARNING) | no `notifications/prompts/list_changed` on the listen stream | mcpx gap: an upstream's list change on *its* listen stream is not subscribed to and relayed |
| `server-stateless` / `sep-2575-server-rejects-undeclared-capability`, `sep-2575-missing-capability-http-400` | executed `test_missing_capability` although the client did not declare `sampling` | mcpx defect: the upstream sees mcpx's own client capabilities, not the calling client's; a pass-through call should carry the caller's |
| `input-required-result-basic-list-roots` / `sep-2322-list-roots-incomplete`; `input-required-result-multiple-input-requests` / `sep-2322-multiple-inputs-incomplete` (2 of 3 asked) | no `roots/list` inputRequest | decision to revisit: mcpx answers an upstream's `roots/list` from its own configured roots and never relays it |
| `input-required-result-ignore-extra-params` / `sep-2322-ignore-unexpected-params` (WARNING) | no complete result | mcpx gap: a request carrying `inputResponses` without a `requestState` is treated as a new call; the fixture accepts such answers directly, mcpx does not forward them |

### Server, `--suite all` (44)

The 2026-07-28 list, plus five legacy-tool checks that only the 2025-11-25
daemon can pass: `tools-call-elicitation`, `tools-call-sampling`,
`elicitation-sep1034-defaults`, `elicitation-sep1330-enums` ("Server did not
request … from client") and `tools-call-with-logging`. `--suite all` runs
against the 2026-07-28 daemon, and over 2026-07-28 the fixture itself answers
its legacy tools' pushed requests `-32601`.

### Client, both sets

Every remaining failure is `auth/*`: mcpx has no OAuth client (#253), and its
per-server `auth` block is parsed and never applied (#240).

## What the suite does not see

- `mcpx serve` over stdio: no stdio server mode in the suite.
- Legacy revisions other than 2025-11-25: there are no requirement sets for
  them. `internal/mcpserver/requirements_test.go` and the schema sweep cover
  2024-11-05 through 2025-06-18.
- The tasks extension and `x-mcp-header` validation, whose fixtures exist only
  in the SDKs' own conformance servers. mcpx's own tests cover them
  (`internal/mcpserver/requirements_test.go`,
  `internal/mcpserver/transport_test.go`).

The per-requirement status, with the test that verifies each row, is
[conformance-matrix.md](conformance-matrix.md).
