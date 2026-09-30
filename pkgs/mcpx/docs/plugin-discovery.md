# Finding the daemon without the binary

What the opencode plugin does when there is no `mcpx` on `PATH`, why each rung
of the ladder exists, and what was found building it.

```
created:      2026-09-29T21:00:00-05:00
last-updated: 2026-09-29T21:00:00-05:00
status:       implemented
```

Companion to `docs/opencode-plugin.md`, which records what opencode v1 and v2
offer a plugin. This one records the decisions that followed, and the things
that turned out not to be true.

---

## 1. The requirement, restated

The binary must not be a precondition. Two reasons, one of them forced:

- a daemon may be running under launchd, in a container, or on another host,
  with no `mcpx` on the plugin's `PATH`;
- opencode v2 removes Bun's `$` from the plugin API. `node:child_process`
  still works, but the runtime no longer *offers* a way to run a command, and
  building on one it does not offer is building on sand.

And the harder half: **never fail because there are two.** A plugin that
refuses to work until the user disambiguates has converted "we guessed" into
"nothing works", which is worse than the guess.

---

## 2. `GET /v1/resolve`

The one new daemon endpoint. It answers "which daemon serves this directory?"
for *any* directory, from *any* live daemon, because the answer is a function
of mcpx's configuration search path and not of what the asked daemon happens
to have loaded.

```
GET /v1/resolve?dir=/abs/path
  → { socket, endpoint, configPath, configHash, running, sources }
```

Three things were needed to make it honest.

**It must not chdir.** `config.SearchPath()` walked up from `os.Getwd()`, which
a server cannot change without racing every request in flight. So
`SearchPathFrom(dir)` and `LoadFrom(explicit, dir)` were added and the existing
functions now delegate to them. That is the only edit outside this feature's
own files.

**It must resolve symlinks first.** `FingerprintConfig` already resolves the
files it hashes, for exactly this reason: `/tmp` is a symlink to `/private/tmp`
on macOS, so a caller whose `$PWD` holds the unresolved spelling computed a
different key for the same project and was told nothing was running. The
directory has to get the same treatment before the walk, or the bug comes back
one level up.

**It must prefer the info file's socket.** The daemon key names the socket only
when the state path fits in `sun_path`; beyond 100 bytes the socket moves to a
private runtime directory and only the info file knows where. A resolver that
trusted the key would hand out a path with nothing behind it.

### A name collision worth recording

`daemon-<key>.json` contains a field called `configHash`, and it is **not** the
key in the file name. The field is `HashConfig(raw)` — a digest of the server
definitions, used to invalidate the schema cache. The file name is
`FingerprintConfig(sources)` — the daemon key, over every path and every byte
that contributed. Two different numbers for the same daemon, under one name.

`/v1/resolve` returns the *key*, because that is what identifies a daemon.
Renaming the field in the info file would be the real fix; it is out of scope
here and is reported rather than done.

---

## 3. The ladder

| # | rung | cost | applies when |
| --- | --- | --- | --- |
| 0 | explicit endpoint or socket | free | configured, or remote |
| 1 | the remembered choice, still answering | one health check | every session after the first |
| 2 | the `daemon-*.json` info files | readdir + parallel health | the usual case |
| 3 | `/v1/resolve` against any live daemon | one request | two or more candidates |
| 4 | `mcpx --json status` | one spawn, ~23 ms | a binary exists and rung 3 was not available |
| 5 | ask | a toast, a prompt, or the picker | still ambiguous, and someone is there |
| 6 | best candidate, warn, carry on | free | headless, or unanswered |

Rung 5 is deliberately **not** in the core. The core returns the candidates and
a warning; whether anyone can be asked, and how, is the adapter's business, and
it differs per harness. That keeps `mcpx/daemon.ts` free of opencode.

### Rung 2 looks in three directories, not one

The info file is always in the state directory. The socket is not: too long a
state path sends it to `$XDG_RUNTIME_DIR/mcpx`, and failing that to
`$TMPDIR/mcpx-<uid>`. Sockets are therefore scanned in all three, and any that
has no info file is still health-checked — a daemon with a deleted info file is
still a daemon.

A socket that is not owned by this user is skipped without dialing. The
socket's permissions are the access control for the whole API; connecting to
another user's would send this session's calls to somebody else's servers.

### Rung 3 stops at the first definite answer

If a daemon answers and names a socket that is live and among the candidates,
that is the answer. If it answers and says nothing is serving this directory,
the loop stops rather than asking the next daemon: they all compute the same
function, so a second opinion is the same opinion.

### Rung 0 does not fall through

A named endpoint that is not answering returns *that endpoint* with a warning,
never a local daemon. Answering from the wrong machine — wrong servers, wrong
credentials, possibly wrong data — is worse than answering not at all. This
matches what the CLI already does (`will not start a local one`).

---

## 4. Asking, in a harness with almost no interface

A v1 server plugin has `client.tui.showToast` and a few fixed dialogs. There is
no generic dialog and no select over HTTP, in either version. So:

- **A toast**, on boot and on first use, once per session per reason.
- **One line on every mcpx tool result**, naming the daemon that answered.
  Cheaper than a toast and impossible to miss in a transcript.
- **`mcpx_daemon_select`**, whose execution raises `ctx.ask(...)`. That is the
  only interactive channel a v1 server plugin has, and it is a good one: the
  user sees the daemon's config path, start time, version and session count in
  the permission metadata, and `always` is exactly "remember this".
- **`mcpx-tui.tsx`**, optional, for a real list you arrow through.

The tool takes an **index**, not a path. The agent reads `mcpx_daemon_status`,
reasons about it, explains its recommendation, and names a number; the tool
turns that into a socket. The agent cannot mistype a path it never types. This
is why the guided skill beats a coded walkthrough: it degrades correctly when
the agent is confused.

⚠️ v1's `permission.ask` *hook* is dead code — declared, documented, never
invoked. Nothing here is built on it.

### Detecting headless

`showToast` publishes an event and returns true whether or not anyone is
listening, so it cannot be asked. The process shape can: `opencode tui` runs the
server in a `Worker` and the interface on the main thread, while `opencode run`,
`opencode serve` and every CI invocation run the server on the main thread with
nothing attached. So `isMainThread` from `node:worker_threads` is, in v1,
exactly "no TUI attached". `MCPX_PLUGIN_HEADLESS` overrides it.

Being wrong in the safe direction costs a toast nobody sees. Being wrong the
other way would hang a headless run on a question — which is why no automatic
path ever raises a permission prompt. Only `mcpx_daemon_select`, which an agent
calls deliberately, asks anything.

---

## 5. Remembering

| scope | where | why there |
| --- | --- | --- |
| session | plugin memory | it should not outlive the session |
| until-gone, indefinite | `$MCPX_STATE_DIR/opencode-daemons.json` | **v1 server plugins have no storage API** |
| permanent | `PUT /v1/settings/daemon.endpoint` | it is a user preference, and mcpx already owns those |

The file is keyed by directory, written through a rename, and read at rung 1
before the scan. `ctx.storage` in v2 replaces it exactly.

The TUI picker writes the same file — it is the only channel between the two
realms — so the server plugin re-reads it when its copy has gone stale. Five
seconds: never would make the picker take effect only in the next session, and
always would put a file read in front of every tool call.

---

## 6. What else had to stop spawning

Discovery was half the problem. The rest:

| feature | before | now |
| --- | --- | --- |
| tool timing | `POST /v1/log`, spawn fallback | `POST /v1/log`, and **drop** the record with no daemon |
| `mcpx_discover` | `mcpx ls` / `mcpx types` | `GET /v1/namespaces`, `GET /v1/types` |
| `mcpx_observe` | `mcpx log` / `mcpx stats` | `GET /v1/log`, `GET /v1/stats` |
| `mcpx_exec` | `mcpx exec` | `POST /v1/exec`, falling back on 404 |

Dropping the timing record is the right failure. A timing is not worth 23 ms on
every tool call, and certainly not worth failing one.

The `backend` setting (`auto`, `v1`, `cli`) makes the fallback explicit.
`v1` is the honest setting for a machine with no binary: a missing daemon then
reports a missing daemon, rather than a missing command.

---

## 7. What is general and what is opencode

`mcpx/daemon.ts` imports nothing from opencode. It uses `fetch`, `node:fs`,
`node:os`, `node:path` and `node:child_process`, and nothing else. The ladder,
the info-file format, the health filtering, the ranking and the remember file
are all about mcpx; `mcpx-session.ts` is the part that is about opencode.

What a second harness has to provide: a directory, a session id, somewhere to
put environment variables, and — optionally — a hook after a tool call, a way
to show a line of text, and a way to ask a yes/no. Only the first two are
required; everything else degrades.

---

## 8. Constants

The Go side puts every constant in `internal/defaults/defaults.json`; this
feature adds `resolve.dialTimeout`.

The plugin cannot read that file — it ships as loose files copied into an
opencode config directory, with no mcpx installation implied. So it has the
same idea in the only form available: one `TUNING` table at the top of
`mcpx/daemon.ts`, with every timeout, limit and file name in it, and each
value overridable through the options a harness passes in.
