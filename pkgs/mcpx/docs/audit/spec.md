# Audit: the MCP specification (area `spec`)

Audited at `19237a0` (origin/main). Authorization excluded (#253). Entries
already in `docs/in-name-only.md` are not repeated.

Commands used (from `pkgs/mcpx`):

- `go test ./internal/conformance -run 'Test.*/2024-11-05/' -v -count=1` → exit 0, 175 PASS, 5 SKIP, 0 FAIL.
- `go test -count=1 -v -run 'Test(Lifecycle|Messages|Utilities|Features|ClientFeatures|Stateful|Transport)(Server|Client)' ./internal/conformance` → exit 0, 28 SKIP (the gapped cells).
- Same with `MCPX_CONFORMANCE_RUN_GAPS=1` → exit 1, 1315 PASS, 27 FAIL. Comparing the two runs: 5 cells skipped as gaps **pass**.
- A scratch test (not committed) against the conformance harness's `httpServer`: a 2024-11-05-style `GET` with `Accept: text/event-stream` returned
  `status=405 body={"jsonrpc":"2.0","error":{"code":-32600,"message":"a GET stream belongs to a session; send initialize first"}}`.

How the matrix works matters for every row below: `internal/conformance/*_test.go`
mostly registers **one test body for a list of requirement ids**
(`for _, id := range []string{...}`; 102 such loops). Each id then reads
"tested" in `docs/spec/conformance-matrix.md`, whether or not the body touches
that requirement.

## Matrix says "tested", the cited test does not exercise it

| what | state | evidence | issue |
| --- | --- | --- | --- |
| Server `progress-must-increase`, `progress-stop-after-completion`, `progress-only-active-tokens`, `progress-rate-limit`, `progress-task-*` (MUST/SHOULD, every revision) | untested | **Fixed since:** the cells now drive a real mcpclient relay against an upstream that reports badly (`serverRelayedProgress`), and `mcpclient.acceptProgress` enforces the rules. At the time of the audit: `internal/conformance/utilities_test.go:169-188` calls only `mcpx_status` and asserts no frame arrives ("mcpx sends none"). But mcpx **does** send progress: relayed upstream progress on `tools/call` (`internal/mcpclient/relay.go:158-172`). That path forwards the upstream's value verbatim with no check that it increases or is rate-limited, so a decreasing upstream reaches the host as a decreasing mcpx notification. The behaviour that binds mcpx is never looked at. | none (#212 closed) |
| Server `logging-setlevel-at-or-above` (2024-11-05 – 2025-11-25) | untested via cited test | Cited `utilities_test.go:239-262` sets `debug` and checks that `mcpx_status` emits nothing; no message is ever filtered. Filtering is real (`mcpclient/relay.go:181`) and is exercised by an uncited test, `internal/mcpclient/relay_test.go:80-86` (`LogLevel: "warning"`). Matrix points at the wrong test. | — |
| `cancellation-max-timeout-despite-progress` (both sides, 2026-07-28) and `lifecycle-timeouts-max-regardless-of-progress` (both sides, 2025-03-26 – 2025-11-25) | untested | Client: `utilities_test.go:338-360` and `lifecycle_test.go:751-` use a peer that never answers and **sends no progress**. Server: `utilities_test.go:91-104` checks only that `input_required` returns fast; `lifecycle_test.go:217` uses `askUnanswered`, no progress. Holds today only because progress never resets any timeout; a change that let progress extend it without a cap passes every cited test. | — |
| `cancellation-task-use-tasks-cancel` (MUST, both, 2025-11-25) | minimal | Client cover is the shared timeout body (`utilities_test.go:338-345`), which asserts the call was *not* task-augmented. Server cover (`utilities_test.go:55-57`) uses an un-tasked question. Neither sends a task-augmented request, so the "use `tasks/cancel`" rule is never reached. | #209 |
| `lifecycle-http-shutdown-closing-connection` for **2024-11-05** (both sides) | minimal | `lifecycle_test.go:153-170` initializes, `DELETE`s with `Mcp-Session-Id`, expects 404. Its own comment says "(2025-03-26 on)". 2024-11-05 has no session id, no DELETE and no Streamable HTTP; the requirement is "shutdown = closing the connection". | — |
| `sampling-deprecated-new-impls-should-not-adopt` (SHOULD NOT, 2026-07-28) | partial | Catalogue (`docs/spec/requirements.md:727`) says "mcpx forwards; pass-through, not adoption". But the daemon **originates** sampling: `internal/daemon/consumer.go:589-603` ("generation by sampling"), `generate` → `elicit.SampleAsk` → broker. And the pool declares `sampling` to every upstream whenever `Hooks.Elicit` is set (`internal/pool/pool.go:487-494`). The cited client test (`clientfeatures_test.go:360-375`) asserts sampling **is** declared when a handler exists. So the cited test passes on the behaviour the requirement discourages. | #110, #210, #256 |

## Recorded gaps that are already closed (docs contradict code)

| what | state | evidence | issue |
| --- | --- | --- | --- |
| `lifecycle-stdio-client-shutdown-sequence` gap (2024-11-05, 2025-03-26, 2025-06-18, 2025-11-25) and `stdio-client-shutdown-sequence` (2026-07-28) | fixed; gap record removed since, the 5 cells run | Gap text (`internal/conformance/coverage_lifecycle.go:98-100`, matrix line for every revision): "StdioTransport.Close sends SIGTERM at the same moment it closes stdin". Code: `internal/mcpclient/stdio.go:260-275` closes stdin, waits `stdinGrace`, then SIGTERM, then SIGKILL after `termGrace`. With `MCPX_CONFORMANCE_RUN_GAPS=1` all 5 skipped cells **pass** (`TestLifecycleClient/{2024-11-05,2025-03-26,2025-06-18,2025-11-25}/lifecycle/lifecycle-stdio-client-shutdown-sequence`, `TestTransportClient/2026-07-28/transport-stdio/stdio-client-shutdown-sequence`). `internal/mcpclient/stdio_shutdown_test.go` also covers it. Matrix under-reports 5 client cells as gaps; the passing tests are skipped so they guard nothing. | #203 (open) |

## Revisions and transports the suite covers in name only

| what | state | evidence | issue |
| --- | --- | --- | --- |
| mcpx as a **2024-11-05 server over HTTP** | partial | No HTTP+SSE server transport. A 2024-11-05 HTTP client's opening `GET` (scratch test above) gets **405** with a JSON-RPC error, no `endpoint` event. `docs/protocol.md:51` and `docs/spec/transport.md:9` say "2024-11-05 over stdio and Streamable HTTP": that is a hybrid no 2024-11-05 client speaks, because Streamable HTTP is 2025-03-26. `docs/protocol.md:78` says the same thing ("2024-11-05 has no Streamable HTTP, so nothing arriving on /mcp can be a 2024-11-05 request"). The `http-sse-server-*` MUSTs are marked n/a ("optional feature"), so the matrix shows 2024-11-05 server MUSTs at 30/32 tested with no transport gap. In practice 2024-11-05 is served over stdio only. | #220 is client-side only |
| 2024-11-05 / 2025-03-26 in mcpx's own suite | partial (not absent) | Unlike the official suite, mcpx's own suite does run per revision: the 2024-11-05 filter above gives 175 passing cells. Most bodies are shared across revisions, though, and the 2024-11-05 HTTP cells exercise 2025-03-26 mechanics (rows above). | — |

## Capabilities declared and not honoured

| what | state | evidence | issue |
| --- | --- | --- | --- |
| `resources.subscribe` (legacy) | partial | Declared whenever the connection can push (`internal/mcpserver/server.go:2153-2157`). `resources/subscribe` always succeeds, including for URIs no upstream owns or whose upstream lacks `resources.subscribe`. Nothing is delivered for those, and the client is not told (`server.go:871-882`; the daemon only logs a warning). Deliberate (#251), but the declared capability does not hold for those URIs. | #251 (closed) |
| Relayed progress as mcpx's own notifications | fixed since | See the first table. mcpx is the notifier of record towards the host and passes progress through unvalidated (`mcpclient/relay.go:158-172`). | — |

## Skipped

- Did not check each of the ~1300 cells one by one. I read the shared-body loops in `utilities_test.go`, `lifecycle_test.go` and `clientfeatures_test.go:360-440`. Other loops in `messages_test.go`, `features_test.go`, `stateful_test.go` and `transport_test.go` I only read by header. Many of those are "mcpx emits none, so it cannot emit one wrongly" covers (e.g. `features_test.go:180` outputSchema, `:358` prompt content, `clientfeatures_test.go:296` URL-required errors). They are vacuous but defensible as n/a, so I list none of them individually.
- Did not diff the schemas in `internal/mcpspec/schema/` against what the server emits beyond what the harness already validates.
- Did not check `revision-conflicts.md` entry by entry.
- 27 cells fail under `MCPX_CONFORMANCE_RUN_GAPS=1`. Those are recorded gaps that are still open, so they are consistent and not listed.

## Most consequential five

1. **mcpx cannot serve a real 2024-11-05 HTTP client** (405 on the opening GET). The docs call this "2024-11-05 over Streamable HTTP", a transport that revision does not have, and the matrix shows no gap for it.
2. **The progress MUSTs (increase, stop after completion, active tokens only) are "tested" on a path that sends no progress.** The relay path that does send progress forwards upstream values unchecked.
3. **Sampling is adopted, not just forwarded, in 2026-07-28.** The daemon originates `sampling` requests for script generation and declares the capability to every upstream. The matrix marks the SHOULD NOT as tested, using a test that asserts the declaration.
4. **The stdio shutdown gap (#203) is stale** (since fixed: the record is removed and the cells run). The fix is in, but 5 conformance cells are still skipped as gaps, so the passing tests guard nothing and the matrix under-reports.
5. **Timeout-despite-progress and tasks/cancel requirements are covered by shared test bodies that never send progress or a task.** They would not notice a regression.
