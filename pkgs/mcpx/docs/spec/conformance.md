# The conformance matrix

`docs/spec/conformance-matrix.md` says, for every normative requirement of every MCP revision from 2024-11-05 to
2026-07-28, and for each side mcpx plays, whether a named test holds mcpx to it, why it does not bind mcpx, or which
issue tracks mcpx not meeting it. `go test ./internal/conformance` fails when any cell has none of the three. This is
how it is put together, and what building it found.

## Pieces

- **Catalogue.** `docs/spec/requirements.md` is the prose catalogue (788 rows, from the five area sweeps). `go run
  ./internal/conformance/cmd/genreq` turns it into `internal/conformance/requirements.json`: revision ranges expanded,
  the side column read as server/client applicability. Anything the catalogue does not plainly mark n/a is taken to
  apply -- a row wrongly marked as applying shows up as a missing test and gets looked at; one wrongly marked n/a is
  never looked at again. Re-judgements live in `overrides/*.json`, each with its reason. Authorization rows are
  `not-implemented:#253`: mcpx implements no OAuth.
- **Coverage.** `coverage_*.go`, one file per area. A cover names a test as `<pkg>.<TestName>/<subtest>`; the subtest's
  leading revision is the revision it covers, or an explicit `@rev,...` for a test not named per revision. `SReq`/`CReq`
  cover the requirement-named subtests `forReq` produces.
- **Gaps.** A `Gap` names a requirement, side, optional revisions, an issue, and why. The tests in this package consult
  the gap list at run time: a gapped cell's subtest skips with `gap: <issue>`, so the failing check is kept and runs
  again the moment the entry is removed. `MCPX_CONFORMANCE_RUN_GAPS=1` runs them all anyway -- which is how the gaps
  closed by #239 were found and removed.
- **The check.** `TestMatrix` regenerates `requirements.json` and the matrix and compares both with the committed
  files (`MCPX_UPDATE_MATRIX=1` rewrites them), parses the Go test sources to refuse a cover that names a test which
  does not exist or which skips as a gap, and fails listing every cell with no cover and no gap.
  It also fails when a tested cell's catalogue note claims a gap (any `gap`/`GAP` in the note, recorded as `claims`
  in `requirements.json`): the note and the test cannot both be right, so neither is allowed to silently outrank
  the other. The matrix names the sha256 of its inputs (the catalogue, overrides and `coverage*.go`) rather than a
  commit, since a file cannot name the commit that contains it; the comparison is what keeps it current.
  `MCPX_MATRIX_AREAS=tools,resources` narrows the list. `go run ./internal/conformance/cmd/covering` runs exactly the
  covering tests, one `go test -run` per package (`-n` prints the commands).

## Why `forReq` takes its revisions from the catalogue

A test that loops over its own list of revisions can claim one it never runs. `forReq(t, side, id, fn)` reads the
requirement's revisions from `requirements.json` and runs `fn` once per revision as `<rev>/<area>/<id>`, which is
exactly the name the cover expands to. The cover and the run cannot drift.

## The harness

`harness_test.go` is shared by every area: mcpx's server over stdio pipes (`stdioServer`) and over a real listener
(`httpServer`), with a complete fake backend, a filtering notifier and a scriptable `Asker`; mcpx's client against a
scripted peer of any revision (`newPeer`), in process or behind Streamable HTTP (`httpPeer`). Every frame either side
emits is validated with `mcpspec` -- strictly for mcpx's server frames, so a key a revision does not define fails the
test that provoked it.

## What building it found and fixed (internal/mcpserver)

Each fix has a test in this package that fails without it.

- A question mcpx put to a legacy client was not bounded by the ask deadline: a client that never answered held the
  call for good, and the question was never cancelled. It is now asked under the deadline, and its expiry sends
  `notifications/cancelled`.
- A batch of 2026-07-28 requests on a stdio process that had not been initialized was answered as a 2025-03-26 batch.
  An element that names its own revision now decides.
- `logging/setLevel` with a level that is not one of the eight answered `{}`; it is `-32602` now. A valid level is
  still accepted though mcpx sends no log messages.
- `completion.maxValues` could raise a completion past the 100 values every revision allows.
- The batch refusal was two sentences; error messages are one.
- An invalid list cursor restarted at the first page; it is `-32602` now, in every list including `tasks/list`.
- (`internal/cli/serve.go`) `mcpx://<ns>/<uri>` concatenated the upstream URI unencoded, so a space, `#` or `?` in it
  made something that is not a URI, or not the one meant. Characters a path may not hold are percent-encoded, `%`
  included, and decoded again on read, completion and subscribe.
- `tasks/cancel` of a finished task succeeded; 2025-11-25 makes it `-32602`.
- An unknown tool name was an `isError` result; it is `-32602`, before any task or question starts.
- `prompts/get` did not check required arguments; a missing one is now `-32602` before anything runs or asks. (The
  `-32602`/`-32603` classification of backend failures landed at the same time in #245; the two are merged.) The
  missing `backend == nil` guard is added.

## Surprises

- Through 2025-06-18 the schema requires an error response's `id` to be a string or integer, and JSON-RPC requires
  `null` when the request could not be read. No frame satisfies both; mcpx omits the id (C4 in
  `revision-conflicts.md`).
- 2026-07-28 removed `ping`. mcpx still answers one (accept liberally) and its client no longer sends one.
- Two `ServeStdio` loops on one `Server` shared its default connection, so a question could reach the wrong one. No
  process does this -- one stdio server per process -- but a test that tried it did; a second loop now gets its own.
