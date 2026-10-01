# exec and artifacts

> A script produced a screenshot. Who gets it, and what does it cost them?

---

## The thing that was wrong

`mcpx exec` ran the script in the CLI process. The CLI generated the client,
spawned deno, and the script called back into the daemon over its socket. That
is the right arrangement for a person at a terminal — the script owns stdout,
reads real standard input, and a relative path in it means what it means on
the command line.

It is the only arrangement, and that was the problem. Two callers could not
use it at all:

- The **opencode plugin** holds a unix socket and nothing else. It has no mcpx
  binary to spawn. It could list tools, call one, read the log — and could not
  run a script, which is the thing mcpx exists for.
- A **daemon on another machine** could not be asked to run one. The servers
  are over there; the script ran over here and reached across the network for
  every single call.

So `POST /v1/exec`: the daemon runs the script. The generated client it writes
points at the daemon's own socket, so the calls that used to cross a network
now cross a socket, and a caller with no binary has one request to make.

The refactor that made that safe is `internal/execsvc`. Two implementations of
"run a script" would be two sets of bugs and a script that behaves differently
depending on who asked — which is exactly what this whole feature is trying to
stop. There is one.

### Why the CLI still builds its own runner options

`execsvc.Run` is the wire entry point: everything arrives as `ExecOptions` and
nothing is inherited from a terminal that is not there.

`execsvc.RunWith` takes runner options the caller assembled. The CLI uses it,
because `mcpx run` genuinely has more knobs than `/v1/exec` does — launcher
templates, five injection phases, `--keep`, a typecheck mode, placeholder
files — and flattening all of that into the wire type would make the wire type
a mirror of one command's flag list.

What must not be duplicated is everything *after* the process starts: which
frames are produced, how artifacts are reconciled, when media is intercepted,
how delivery is applied. That is all in `RunWith`, and it is the same code for
both. Hooks the caller already set are chained rather than replaced, so the
CLI keeps rendering logs to a terminal while the service collects the same
records for the result.

## Where a script runs

`exec.where` is `auto`, `local` or `remote`, spelled `--local` and `--remote`
at the command line.

Auto runs locally when the daemon is on this machine and on the daemon when it
is not. That is the rule that makes the common case right without anyone
choosing: a person gets a terminal-shaped run, and a configured remote
endpoint gets a remote one, because running locally against a remote daemon
means every call crosses the network and every file the script writes lands on
the wrong side.

`--local` against a remote endpoint is **refused** rather than attempted. It
does work, in the sense that the script runs and the calls succeed; it just
quietly produces a different artifact story, and saying so beats a subtly
different result.

A remote run sends the script's *text*, not its path. A path would mean the
daemon's filesystem, and somebody typing `mcpx run ./report.ts --remote` means
the file in front of them. The consequence is that relative imports do not
survive the trip: the daemon writes the text to its own disk, and a sibling
module is not beside it. `--export` does survive, because the launcher imports
the written file and calls the named export there.

Every flag that shapes the program travels with it: `--env`, `--typecheck`,
`--launcher`/`--no-launcher`, `--allow-repeat`, `--no-capture-console`,
`--export`, and the five phases (`--before`, `--prefix`, `--suffix`,
`--on-success`, `--on-error`). The CLI resolves them -- reading any file a
phase or launcher names on *its* side -- and sends source, so the daemon runs
the program that would have run locally. Until #192 the remote path sent five
of these and silently dropped the rest; `TestEveryExecFlagChangesSomething`
now runs every row both ways.

## ExecOptions

One flat object that grows, rather than a new endpoint per capability. This is
step 7 of the issue and it is the right shape: the caller declares what it can
receive, the script declares what it produced, mcpx handles delivery.

```ts
type ExecOptions = {
  runtime?: "auto" | "deno" | "bun" | "node"
  timeout?: string
  permissions?: string
  placeholders?: Record<string, unknown>
  session?: string
  cwd?: string; env?: Record<string, string>; stdin?: string
  output?: "text" | "structured" | "stream"
  capabilities?: string[]
  artifacts?: {
    delivery?: "reference" | "inline" | "stream"
    maxBytes?: number
    sharedFs?: string
    dir?: string
  }
  task?: { ttl: number }
  ns?: string[]; args?: string[]; export?: string
  typecheck?: "off" | "on" | "strict"
  launcher?: string; launcherName?: string   // template text, or "none"
  allowRepeat?: string[]
  captureConsole?: boolean
  phases?: { before?: string[]; prefix?: string[]; onSuccess?: string[];
             onError?: string[]; suffix?: string[] }
}
```

`cwd`, `env` and `stdin` are the **caller's** context, not the daemon's. A
remote daemon has a different working directory and a different environment,
and a script that reads either should see the one belonging to whoever asked.
A local caller passes its own, which is how the two cases stay identical.

Three fields are not in the issue's list. `ns` and `args` are there because
`mcpx exec --ns` and `mcpx run script a b` already exist, and an option the
CLI has and `/v1` does not is a parity hole — the one thing this repository
checks for automatically. `dir` sits beside `sharedFs` because they are
different statements: `sharedFs` is "I can see your disk", `dir` is "put them
here for me". They happen to be implemented the same way when the caller is
local, and they are not the same claim.

`task` reuses `internal/tasks` unchanged. A long exec with artifacts is a task
whose result holds references, which already fits `tasks/result`.

## Output shapes

Three renderings of one run.

- **text** — the CLI default. stdout is the script's answer and a person
  reading it wants it unadorned. It goes to the terminal *as it is produced*,
  not collected and printed at the end; a long run that printed as it went
  must keep doing so.
- **structured** — the `/v1` and MCP default. One document:
  `{result, emits[], logs[], stdout, artifacts[], exitCode, durationMs,
  error?}`. A caller over HTTP wants fields, not text it has to scrape.
- **stream** — frames as they happen. SSE by default because it is plain
  HTTP and a browser consumes it with no code; NDJSON on
  `Accept: application/x-ndjson`, because a program with a JSON decoder should
  not have to strip `data: ` off every line first.

`result` is the script's stdout parsed when stdout is JSON. That rule is
inherited from `mcpx --json run` rather than invented, so the two agree.

### The frame order, and why it is the order

```
start{runId}
log | emit | stdout            (interleaved, as they happen)
artifact{id,name,mime,size,sha256,uri}
result | error
end{exitCode,durationMs}
---- only for delivery:"stream" ----
artifact.chunk{id,seq,data}
artifact.end{id}
```

Artifact **metadata** comes before `end`; artifact **bodies** come strictly
after it. That is the whole point of the design, and it is step 5 of the
issue: a streaming consumer reads the emits and the return value, decides it
does not need the 4 MB video, and disconnects at a frame boundary having paid
for none of it.

A disconnect is not merely tolerated. The sink returning an error cancels the
run context, which kills the process group; and every file the CLI writes from
a stream goes to a temporary name and is renamed on `artifact.end`. A
cancelled stream must not leave something that looks like a complete file,
because the next thing to read that directory cannot tell the difference.

## Artifacts

### The script API is the same everywhere

```ts
const shot = await chrome_devtools.take_screenshot({ format: "png" })
await artifact("checkout.png", shot)
return { total }
```

`data` may be a string, a `Uint8Array`, an `ArrayBuffer`, `{ path }`, an MCP
`image`/`audio`/`resource` content item, or a tool result carrying one on
`.raw`. Accepting the content item directly is what makes the common case one
line: a screenshot *arrives* as `{type:"image",data,mimeType}` and that is
exactly what gets passed in.

The script never asks where it is running. The issue's step 1 — `if
(mcpx.session.isRemote)` — was rejected for that reason: an author who has to
know about remoteness writes two code paths and one of them is never tested.

What actually happens underneath differs, and only in a way nobody has to
think about. `artifact()` POSTs to `/v1/artifacts` over the transport the
generated client already has. When the runner set `MCPX_ARTIFACTS_LOCAL=1` —
which it does when a unix socket proves the daemon shares this filesystem —
a `{path}` is sent as a header and the store **hardlinks** it. Otherwise the
bytes travel once, to the daemon, and are then fetched by whoever wants them.

Remoteness is declared, never inferred. A unix socket can be ssh-forwarded and
loopback TCP can be a container with its own filesystem; neither transport
tells you whether the caller can see the daemon's disk. So the *caller* says
so, with `capabilities: ["artifacts"]` and optionally `sharedFs`.

### The store

Content-addressed by sha256 under `<state>/artifacts/blobs/ab/cdef...`, with a
SQLite index beside it. SQLite because it is already a dependency and because
"list the artifacts of this run" is a query rather than a directory walk.

The **handle is not the hash**. A content hash is guessable by anyone who can
guess the content — and for a screenshot of a public page, that is everyone.
So an artifact is served by a separate random 128-bit id mapped to the hash.
That id is the entire access control until OAuth privilege levels land, which
is why it is sized as a secret rather than as an identifier. When scopes
arrive, the natural shape is that the session that produced an artifact owns
it and a privileged scope can read any of them; the index already carries the
session for exactly that.

Two registrations of the same bytes share one blob and get two handles. The
quota counts distinct content, so a script that produces the same screenshot
twice pays once. Deleting a registration only removes the body when no other
registration refers to it.

Everything is written to a temporary name and renamed on completion — in the
store, in `--artifacts-dir`, and in the stream consumer. A cancelled upload
leaves nothing a later reader could mistake for a whole file.

### Names are untrusted

The name comes from the script, and a script is not trusted with a path.
`SanitizeName` takes the last component (so `../../etc/passwd` is `passwd`),
keeps `[A-Za-z0-9.-_+]`, turns spaces into hyphens and everything else into
underscores, strips leading and trailing dots, and truncates to
`artifacts.nameMaxLength` while keeping the extension. An empty result becomes
`artifact`, because a file has to be called something.

Removal rather than rejection: a caller whose artifact is refused over its
name has lost the file, and the name is decoration — the artifact is reached
by id.

Collisions get a numeric suffix **before** the extension — `shot-1.png`, not
`shot.png-1` — so the file still opens in whatever the extension implies.
Nothing is ever overwritten: two files called `shot.png` are two files, and
silently keeping one of them is data loss. The reservation uses
`O_CREAT|O_EXCL`, because testing with `Stat` and then creating is a race two
concurrent runs of the same script would lose.

### Delivery

| mode | what crosses | when |
| --- | --- | --- |
| `reference` (default) | an id and a URI | always correct; the only mode whose cost does not scale with what the script happened to produce |
| `inline` | base64 in the structured result, bounded by `artifacts.inlineMaxBytes` **and** the caller's `maxBytes` | a caller that cannot make a second request |
| `stream` | frames after `end` | one response, cancellable |

A `sharedFs` or `dir` beats all three: the caller can already read the bytes,
so hardlinking them into its directory costs an inode and no I/O.

Reference is the default because of the round-trip arithmetic. Over a unix
socket a second request costs about 0.2 ms, so separate GETs are free locally
and bring Range and parallel downloads with them. Remotely a single stream is
better, which is why `stream` exists — and artifacts remain fetchable by id
afterwards either way, so choosing the stream costs nothing.

`inline`'s ceiling is deliberately well under the per-artifact ceiling. Inline
delivery puts bytes in the caller's context, which is the cost this whole
feature exists to avoid.

### Intercepting upstream media

When a structured or streamed result — or an emit — contains an MCP `image`,
`audio` or blob `resource` block, **and** the caller declared `artifacts`, the
block is stored and replaced by a `resource_link` carrying the URI, the name,
the type and the size.

This is the local win, and it is worth having with no remote daemon anywhere.
chrome-devtools returns a screenshot as base64 image content; left alone that
lands in an agent's context, where one screenshot costs more than the rest of
the task and where nobody will ever look at it.

It happens at the **output boundary**, not inside the script. A script that
received a handle instead of the bytes could not inspect the image it just
took — measure it, crop it, compare two — which is a real thing scripts do. So
the script sees whatever the upstream server sent, and only what crosses back
to the caller is rewritten. `artifacts.interceptImages` turns it off.

## Reaching an artifact

| surface | how |
| --- | --- |
| MCP | `resource_link` in the `mcpx_exec` result, then `resources/read` on `mcpx://artifacts/{id}`; also `resources/list` and a resource template |
| `/v1` | `GET /v1/artifacts/{id}` with Range, `GET /v1/artifacts?run=…`, `DELETE /v1/artifacts/{id}` |
| CLI | `mcpx exec --artifacts-dir ./out` |

`GET /v1/artifacts/{id}` goes through `http.ServeContent`, which brings Range,
conditional requests and 206 for free — and Range is what makes reading the
tail of a large artifact cheap, one of the three things printing the file
could not do. `Content-Disposition` is `attachment` with the sanitised name,
because a browser deciding to render somebody else's bytes inline is how a
store of arbitrary files becomes an XSS surface.

`resources/read` carries text, so a binary body comes back base64 with its
real type stated. That is lossy in exactly one way — the client decodes — and
it is what the protocol offers. A client that wants raw bytes has the `/v1`
route.

### One edit outside this area

`mcpserver.dispatch` returns a `string`, which cannot carry a `resource_link`.
Threading a content array through a dozen cases to serve one of them would
make eleven signatures worse to serve the twelfth. So `mcpx_exec` encodes its
blocks into the string it already returns (`mcpserver.EncodeResult`), and
`tools/call` decodes it in exactly one place. `EncodeResult` returns the text
unchanged when there are no blocks, so every other tool is byte-for-byte what
it was.

## Settings

Everything is declared in `internal/settings/exec.go`. It is **not** declared
*once*: unlike `internal/settings/consumer.go` and `internal/settings/wire.go`,
which read `defaults.*`, every `Default:` in `exec.go` is an inline string
literal (`"120s"`, `"64MiB"`, `"10m"`, …), so each value that also lives in
`defaults.json` is written in two places with nothing holding them equal. Three
of them — `exec.where`, `artifacts.enabled` and `artifacts.dir` — have no
`defaults.json` key at all. The values below are what both say today.

| setting | default | what it decides |
| --- | --- | --- |
| `exec.timeout` | `120s` | how long a script may run, wherever it runs |
| `exec.output` | `text` | text, structured or stream |
| `exec.where` | `auto` | local, remote, or decided by whether the daemon is |
| `artifacts.enabled` | `true` | whether the store exists at all |
| `artifacts.dir` | — | `--artifacts-dir` |
| `artifacts.delivery` | `reference` | reference, inline or stream |
| `artifacts.ttl` | `24h` | retention before collection |
| `artifacts.maxBytes` | `64MiB` | the largest single artifact |
| `artifacts.quota` | `1GiB` | the total, over distinct content |
| `artifacts.inlineMaxBytes` | `1MiB` | the largest that may be base64'd into a result |
| `artifacts.chunkBytes` | `256KiB` | one streamed body frame |
| `artifacts.gcInterval` | `10m` | how often expired artifacts are swept |
| `artifacts.interceptImages` | `true` | rewrite inline media at the boundary |

Exceeding `maxBytes` is an error, not a truncation. A truncated screenshot is
worse than a refused one, because it looks like it worked.

`nameMaxLength`, `nameCollisionLimit`, `idBytes` and `listLimit` live in
`defaults.json` without a registry entry: they are read at package
initialisation and a configuration file arriving later could not change them,
and a setting that does nothing is worse than no setting. (This paragraph used
to offer `plumbing.eventHistory` as the comparison. That one *is* a setting —
`events.history`, `internal/settings/wire.go:239`, flag `--events-history` —
so it was the wrong example.)

## Not done

- **The other direction.** An inbox — uploading a file a script needs as
  input, with a script-side `input("name")` — is out of scope for this cut.
  Nothing here prevents it: `POST /v1/artifacts` already stores by content and
  the index already carries a session.
- **OAuth scoping.** Artifacts are served by an unguessable id on an
  unauthenticated API, which is the same access control every other `/v1`
  route has. The index carries the session so a scope check has something to
  check.
- **`multipart/mixed`.** The issue offers it as an alternative to base64 in
  the stream. NDJSON and SSE carry chunks base64'd, which costs 33% on the
  wire. Worth revisiting; not worth a second framing before the first one has
  a user.
- **Relative imports on a remote run.** They need sibling modules on the
  runtime's disk, and a remote run sends one file's text.
