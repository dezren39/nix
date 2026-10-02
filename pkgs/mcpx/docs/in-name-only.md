# In name only

```
created:      2026-10-01T09:00:00-05:00
last-updated: 2026-10-01T12:30:00-05:00
increment:    2
status:       working
tags:         area:audit
description:  features, settings and tests that exist on paper and do
              little or nothing in practice. The curated list; the
              per-area audits carry the full detail.
```

Each entry is something a reader could reasonably believe works, and mostly
does not. It is a list to fix from, not a list of excuses. Check an entry against the code before adding it -- two seeded here from
`docs/spec/revision-conflicts.md` were already fixed. Add to it when you
find one; remove an entry only in the change that makes it true, and say so in
the commit.

**Status of this file:** seeded from what the 2026-09-30/10-01 fix rounds found
while doing other work, then extended by the 2026-10-01 audit of `origin/main`
at `19237a0`. This page stays curated: it carries the entries worth acting on,
and the three area files carry every row, with the command and output or the
file:line behind it.

- [audit/surface.md](audit/surface.md) -- commands, 173 settings, `MCPX_*`
  variables, flags, exit codes, `/v1` routes against the CLI.
- [audit/spec.md](audit/spec.md) -- each MCP revision's requirements against the
  code and against the conformance matrix, both directions.
- [audit/features.md](audit/features.md) -- opencode plugin, `exec`/`run`,
  daemon pools, recipes/`prompt`/autonomy, adapters and OpenAPI, diagnostics,
  elicitation.

**What the audit did not cover.** Authorization was excluded throughout (known
absent, #253). The settings sweep read all 173 statically but ran only 31 at two
values, so most `proto.*`, `upstream.*`, `transport.*`, `prompt.*` and
`recipes.*` settings are unverified either way. The twelve `plugin.*` settings
are read by the TypeScript plugin and were not exercised from a live opencode.
The spec pass read the shared-body loops in `utilities_test.go`,
`lifecycle_test.go` and `clientfeatures_test.go` and the rest only by header, so
it is a sample of ~1300 conformance cells, not a census. Each area file ends
with its own "skipped" list; read it before concluding something is clean.

**Read the specifications, not only the conformance suite.** The suite tests
what it has scenarios for: nothing for 2024-11-05, three scenarios for
2025-03-26, and it measures zero for features no fixture exposes. Passing it is
not conformance. The per-revision requirements are catalogued in
`docs/spec/requirements.md` and `docs/compare/matrix-revisions.md`; check each
against the code, and check each conformance pass against what the spec actually
asks. The audit found the matrix itself is the weak point: most conformance
bodies register one test for a list of requirement ids, so an id reads "tested"
whether or not the body touches it.

Legend: **absent** -- declared, nothing behind it. **minimal** -- does the
least that lets a test or a doc say it exists. **partial** -- works on some
paths and silently not on others. **untested** -- may work; no test would
notice if it stopped.

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
| Answers checked against the requested schema | absent | Nothing validates an answer. Answering a question whose schema requires `repo` with `{"totally_not_in_schema":123}` is accepted silently, rc 0. (The other half of this entry -- "does not show which server asks" -- is **fixed**: `elicit list` has a SERVER column and `elicit show` prints `from <server>`.) #255 |
| `elicit answer key=value` types | partial | Every value reaches the server as a string, so `age=3` arrives as `"3"` for an integer field (`cli/elicit.go:119-126`). Pass JSON to avoid it. #255 |
| Exit 75 when a call stops for input | partial | Holds for the first pending question on a server. While an earlier question for that same (shared) server is unanswered, later callers **block** instead of exiting 75 -- `exec` waited the full two-minute expiry, then printed a cancel result and exited **0**, which is the symptom #286 was closed for. The e2e tests all start from an empty question store, so none of them reach this. #286 (closed) |
| Sampling | minimal | Declared to every upstream; answering needs an agent watching the question queue. No human review, no result validation, no error mapping. Deprecated in 2026-07-28; kept working only. #256, #210 |
| Server-to-client requests across eras | partial | Works when the upstream is configured `protocol: follow`: a 2025-11-25 caller gets its own legacy session of a dual-era upstream, whose requests reach it as requests ([spec/era-probe.md](spec/era-probe.md#follow)). Under the default `modern` a legacy caller still shares the modern session, where a dual-era server's legacy-only push (the official suite's `test_elicitation`, `test_sampling`) answers -32601 -- the four `server-all` failures, until `scripts/conformance.sh` configures `follow`. The reverse direction -- a 2026-07-28 upstream's `input_required` reaching a legacy caller as `elicitation/create` -- already worked, and is now tested (`internal/e2e/follow_test.go`). |

## Tasks

| what | state | detail |
| --- | --- | --- |
| Task persistence | absent | The store is in memory; tasks do not survive a daemon restart. #209 |
| `tasks/cancel` | minimal | Forces the cancelled status instead of letting the work reach its own end. #209 |
| mcpx asking an upstream to run a call as a task | absent | An upstream that requires a task fails. #209 |
| `taskSupport` default for 2025-11-25 clients | partial | A tool with none declared can still be called as a task. #209 |

## Skills extension

mcpx declares `io.modelcontextprotocol/skills` (SEP-2640, 2026-07-28 only) and
serves its own four skills from `plugin/opencode/skills` -- each a `SKILL.md`
plus whatever supporting files and subfolders sit beside it (`mcpx-basics` has
a getting-started guide and an `examples/` folder of scripts) -- through
`skills/list`, `skills/get`, `resources/read` and `resources/directory/read`.
One part of the extension is not built and is not declared.

| what | state | detail |
| --- | --- | --- |
| An upstream's skills through `skills/list` | absent | mcpx answers `skills/list` with its own skills only. An upstream that declares the extension has its `skill://` files passed through as ordinary resources (under their own URIs in pass-through mode, as `mcpx://<ns>/...` otherwise), but its `skills/list` entries -- the digests and frontmatter a host verifies against -- are not relayed, so a host cannot load them as skills through mcpx. Relaying needs a new daemon route and a pool and client method for a request the upstream may or may not support, plus URI handling for the gateway's `mcpx://` rewrite; no fixture in the conformance suite serves skills, so nothing would test it today. |
| `resources/directory/read` (`directoryRead`) | built | Declared as `directoryRead: true` wherever the skills extension is (2026-07-28, and not under `ExtrasOnly`, which serves no skills). Lists the direct children of any directory inside a served skill -- files with the metadata `resources/list` gives them, subdirectories as `inode/directory` -- paginated like `resources/list`; a file, an unknown URI or anything outside the skills is `-32602`. It follows the files, not a fixed layout: `TestDirectoryReadFollowsAnyLayout` covers nested, empty and binary content, and `TestDirectoryReadListsEveryDirectoryOnDisk` checks every shipped directory against the disk. All six `sep-2640-skills-directory` checks in the `server-all` leg pass. Upstreams' skill directories are not listed, for the reason in the row above. |

## Relay between hosts and upstreams

| what | state | detail |
| --- | --- | --- |
| Log messages per call | partial | A log message does not say which call it belongs to; with several calls on one upstream connection, each gets it. |
| Upstream log level on legacy revisions | partial | Pinned to `info` at connect, so an upstream's debug messages never exist to relay. |
| Progress and mcpx's own timeout | partial | Progress reaches the host but does not extend mcpx's per-server call timeout. |
| Relay on `prompts/get` and `resources/read` | absent | Only `tools/call` relays progress and logs. |
| Client capabilities to 2025-11-25 upstreams | partial | Declared once at connection; per-request narrowing reaches 2026-07-28 upstreams only. |

## Commands and settings

Full table: [audit/surface.md](audit/surface.md).

| what | state | detail |
| --- | --- | --- |
| `exec.timeout` (documented default 120s, "kill a script after this long") | absent | Local `exec` never reads it. `MCPX_EXEC_TIMEOUT=1s mcpx exec 'await new Promise(r=>setTimeout(r,3000)); console.log("late")'` printed `late` after 3.8s, rc 0; the flag and `--remote` are no different. Only `--timeout` (default 0 = no limit) kills, and it prints nothing -- rc 124, empty output. Scripts have no default limit. |
| Call-scoped settings on `POST /v1/exec` | absent | `ExecService` builds its settings once under a `sync.Once` from the config file and environment (`internal/daemon/routes_exec.go:170-185`), so the `X-Mcpx-Settings` header is ignored, `mcpx daemon --<flag>` is ignored, and nothing marked `hot: true` is re-read. The scope model in `configuration.md` does not hold on the exec path. |
| Daemon-scoped flags given to a client command | absent | `mcpx call --pool-call-timeout 1s …` is accepted, validated, printed back and ignored -- including when that command auto-starts the daemon. `configuration.md` names this as the failure the scope field exists to prevent, and `help call` advertises ~40 such settings. Nothing warns. #185 |
| `mcpx help <command>` listing "usage, flags and settings" | absent (flags) | No command's help has a flags section. `help exec` never mentions `--timeout`, `--keep`, `--session`, `--export`, `--ns`, `--runtime`. The settings it does list are the same 71 for `init`, `man` and `call` alike, with nothing saying which command acts on which. #144, #147 |
| `script.permissions: none` | minimal | `none`, `off` and `unsandboxed` all map to deno `--allow-all` (`internal/defaults/defaults.json:43-45`). A reader picking "none" for no permissions gets all of them. |
| `logstore.queryLimit`, `stats.top` | partial | `mcpx log` and `mcpx stats` read the store in-process with their own hard-coded defaults (100, 20); the settings are read only by `GET /v1/log` and `GET /v1/stats`. The flag is listed in `help log`. |
| `tasks.ttl` | partial | Applied to exec tasks only; a `/v1/call` task still gets 600000ms. |
| Global flags after the command | partial | `--config`, `--profile`, `--skip-default` and `--all-profiles` are documented as global but only parse before the command: `mcpx ls --profile nope` → `flag provided but not defined: -profile`. An unknown profile before the command is accepted silently and returns the default set. |
| Restarting the daemon itself | absent | `restart` replaces server processes; nothing restarts the daemon. #302 |
| Natural language to script | minimal | `mcpx prompt` matches a recipe or drafts a script; no declared model source, no run path through the ceiling end to end. When nothing is produced it still exits 0. #294 |
| `prompt --mode` | absent | Renamed to `--autonomy`; `--mode` is `flag provided but not defined`, recorded only in decision 0002's "Landed" note. Settings flags on `prompt` also only parse for `autonomy`, `session` and `set`. |
| `mcpx.namespace` validation | partial | Checked for reserved words only. `{"namespace":"1 bad-ns"}` is accepted and listed. |
| Settings being read vs mattering | guard gap | `internal/settings/consumed_test.go` proves a setting is read, not that reading it changes anything. |

## Scripts and their sandbox

Full table: [audit/features.md](audit/features.md).

| what | state | detail |
| --- | --- | --- |
| Permission profiles as a boundary | partial | A script that can reach the daemon can ask the daemon for more than its own profile allows: under `--permissions strict`, posting to `/v1/exec` with `options:{permissions:"all"}` returns 200 and writes the file. `/v1/exec` does not cap requested permissions against the caller's. |
| `artifact({path})` against a denied file | partial | Under `strict`, `Deno.readTextFile` is refused but `artifact("leak.txt",{path:"…/secret.txt"})` succeeds, because the daemon does the read via `x-mcpx-path`. No test covers that header. |
| `--suffix` "runs last on both paths, like a finally" | partial | On `exec` with a snippet that throws, the suffix never runs; on `run` with a file it does. Tests cover the file path only. |
| `--remote` parity with local | partial | Drops the caller's environment except `--env`, drops `res.Logs` so `log.info` output disappears, resolves profiles and runtimes from the daemon's settings rather than the caller's, and skips `diagnose.preflight`. |
| Generated result types from `outputSchema` | absent | `structured()` is typed `Promise<ToolResult>` with `type ToolResult = any`; codegen never reads `outputSchema`. #216 |

## Diagnostics and the log

Full table: [audit/features.md](audit/features.md).

| what | state | detail |
| --- | --- | --- |
| Failed tool calls counted as failures | absent | `"ok": err == nil` (`internal/pool/pool.go:959`) and a tool returning `isError` is not an error there. `mcpx call fake.boom` exits 1 and prints "reported an error (isError)", while the log record says `"ok":true`, `stats calls` shows ERRORS 0 / 0.0%, and `stats errors` says "No errors in the log yet." Every error rate built on the log reports zero. |
| `stats servers` call counts | partial | Summed from `server.stop` records only, so they disagree with `stats calls` (5 vs 12 observed). |
| Attribution by server | partial | Servers that share a pool id are all logged under one name, so calls to one appear against another. |
| `diagnose` | partial | Does not check argument types (a number for a string reports "Nothing to report"), nor unknown namespaces, nor a script name that does not exist -- each exits 0 saying there is nothing to report. #111 |
| `doctor` server health | partial | Never probes a server. Before `refresh`, only a server with a missing binary fails; a crashing one and an unreachable one both pass. |

## Adapters and OpenAPI

Full table: [audit/features.md](audit/features.md).

| what | state | detail |
| --- | --- | --- |
| Adapter `timeout` | absent | `"timeout":"1s"` on a 5s program returned after 5.05s. The context error is never consulted and there is no process-group kill (`internal/adapter/adapter.go:255-261`). No test. |
| Adapter and API argument checking | absent | Declared parameter `type` is not enforced at call time -- `"abc"` for a number is passed through as argv. Unknown arguments are dropped silently, and a positional value starting with `-` is placed before the program's own flags with no `--` separator, so a caller can inject options. |
| OpenAPI cookie parameters, required query params and bodies | absent | Cookie parameters are dropped from the schema and sent as query values; only required *path* parameters are checked, so a missing required `q` is sent anyway and an empty body goes out as `Content-Length: 0`. |
| `securitySchemes` / `security` | absent | Not referenced anywhere in `internal/openapi` or `internal/cli`; authentication is possible only through hand-written `headers`, and `mcpx api --spec` has no way to set them at all. |
| APIs as namespaces in scripts | partial | Only `mcpx serve` exposes them; in `exec`, `typeof pets` is `undefined`, though adapters do appear. |

## The opencode plugin

Full table: [audit/features.md](audit/features.md). Checked against opencode
1.18.31 with plugin types 1.15.5, by invoking the hooks under bun rather than
inside a running opencode.

| what | state | detail |
| --- | --- | --- |
| `instructions` option | absent | Pushes onto `output.parts`; the v1 hook type is `output: { system: string[] }`, so the system prompt comes back empty. #217 |
| `remember: "permanent"` | absent | Sends `persist: true` where the daemon's field is a string, so the write fails with `cannot unmarshal bool into … .persist of type string`. |
| `until-gone` remembered scope | absent | Written to the file and never read; behaves exactly like `indefinite`. |
| `MCPX_HARNESS_VERSION` | absent | Read from `OPENCODE_VERSION`, which opencode 1.18.31 does not set. |
| Documented discovery rung 5 ("ask the user") and the headless warning line | absent | `discover` skips rung 5; `toast()` returns early when headless and writes nothing, so the documented log line never appears. |
| Nothing loads `mcpx-session.ts` | untested | `internal/e2e/plugin_test.go:32` runs `bun test plugin/opencode/mcpx/` only. That is the gap the `persist` type mismatch went through. #141 |

## Specification and the conformance matrix

Full table: [audit/spec.md](audit/spec.md).

| what | state | detail |
| --- | --- | --- |
| mcpx as a 2024-11-05 server over HTTP | partial | There is no HTTP+SSE server transport. A 2024-11-05 client's opening `GET` gets 405 and a JSON-RPC error, never an `endpoint` event. `docs/protocol.md:51` and `docs/spec/transport.md:9` advertise "2024-11-05 over stdio and Streamable HTTP", a combination that revision does not have; in practice it is stdio only, and the matrix shows no gap. |
| The progress MUSTs (must increase, stop after completion, active tokens only, rate limit) | **fixed** | Relayed progress is now checked before it reaches the host (`mcpclient.acceptProgress`), and the server-side cells run a real upstream that reports out of order, after completion, in a burst and after the response (`serverRelayedProgress` in `internal/conformance/utilities_test.go`). |
| `sampling-deprecated-new-impls-should-not-adopt` (2026-07-28) | partial | Catalogued as "mcpx forwards; pass-through, not adoption", but the daemon originates sampling for script generation (`internal/daemon/consumer.go:589-603`) and declares the capability to every upstream that could answer. The cited test asserts the declaration exists, so it passes on the behaviour the requirement discourages. #210, #256 |
| Timeout-despite-progress and `tasks/cancel` requirements | untested | The shared bodies covering them never send progress and never send a task-augmented request, so they hold only because progress currently resets nothing. A change that let progress extend a timeout uncapped would pass every cited test. #209 |
| The matrix's "tested" column generally | guard gap | 102 loops register one test body for a list of requirement ids; every id in the list then reads "tested". At least two rows point at the wrong test, one of which is genuinely covered by an uncited test elsewhere. |
| The `lifecycle-stdio-client-shutdown-sequence` gap (#203) | **fixed; record removed, the five cells run** | The gap says `StdioTransport.Close` "sends SIGTERM at the same moment it closes stdin". It does not: `internal/mcpclient/stdio.go:260-275` closes stdin, waits `stdinGrace`, then SIGTERM, then SIGKILL after `termGrace`. All five cells (`TestLifecycleClient` for 2024-11-05, 2025-03-26, 2025-06-18, 2025-11-25 and `TestTransportClient/2026-07-28/transport-stdio`) SKIP by default and PASS under `MCPX_CONFORMANCE_RUN_GAPS=1`. So five passing tests guard nothing and the matrix under-reports five client cells. Remove the gap, do not fix the code. |
