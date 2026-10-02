# Upstream era probe

How mcpx decides whether a server it fronts speaks the modern (2026-07-28,
per-request `_meta`) protocol or a legacy (`initialize`) one, and how it
remembers the answer.

Sources: [versioning](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions),
[stdio](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#backward-compatibility),
[Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#backward-compatibility).

## Values

`upstream.protocol`, and a server's own `protocol` key, which overrides it,
take one of the values below. Each is named for the first request mcpx sends,
not for an era's relative age; the older names are still accepted wherever the
value is read (config file, `MCPX_UPSTREAM_PROTOCOL`, `--upstream-protocol`,
`PUT /v1/settings`) and are normalised to the canonical one, which is what
`mcpx settings`, `mcpx config` and `GET /v1/protocol` report. Case does not
matter. Any other value is refused with this list.

| canonical | meaning | aliases (accepted, normalised to the canonical) |
| --- | --- | --- |
| `prefer-discover` (default) | `server/discover` first, fall back to `initialize` | `modern`, `prefer-modern`, `prefer-stateless`, `prefer-newest` |
| `prefer-initialize` | `initialize` first, fall back to `server/discover` | `legacy`, `prefer-legacy`, `prefer-session`, `prefer-oldest` |
| `force-discover` | `server/discover` only | `force-modern`, `force-stateless` |
| `force-initialize` | `initialize` only | `force-legacy`, `force-session` |
| `follow` | unchanged | — |

What each value does against a server that speaks only the old
(`initialize`, 2025-11-25 and earlier) protocol, both, or only the new
(`server/discover`, 2026-07-28) one. "Legacy session" means an `initialize`
handshake; "modern session" means per-request `_meta` with no handshake. With
the era cache on, the `prefer-*` and `follow` values try the cached era first
on later starts; the result is the same.

| value | old-only server | dual-version server | new-only server |
| --- | --- | --- | --- |
| `prefer-discover` | discover is refused (or unanswered within `upstream.probeTimeout` on stdio), then `initialize`: legacy session | discover answers: modern session | modern session |
| `prefer-initialize` | legacy session, no discover sent | `initialize` answers: legacy session, so the server's 2026-07-28 features are not used | `initialize` is refused, then discover: modern session (fails if a stdio server exits on the refused `initialize`) |
| `force-discover` | fails to connect; no fallback | modern session | modern session |
| `force-initialize` | legacy session | legacy session | fails to connect; no fallback |
| `follow` | as `prefer-discover`: one legacy session, shared by every caller | modern session for 2026-07-28 callers plus a second, legacy session for callers on an older revision ([Follow](#follow)) | modern session; the legacy session is refused, so older callers share the modern one |

## Order

The default is discover first (`upstream.protocol=prefer-discover`; a server's
own `protocol` key overrides it). Legacy first used to be the default on the
argument that nearly every server is legacy and the probe costs each of them a
round trip. The spec prescribes the opposite order for a dual-era client, and
the era cache turns the probe into a one-time cost per server configuration,
so the argument no longer holds.

## Follow

`protocol: follow` (per server, or `upstream.protocol=follow`) is `prefer-discover`
for the server's own session, plus a second, legacy-only session (`force-initialize`
-- `initialize`, no probe) used for any call whose caller reached mcpx in a
revision before 2026-07-28. The point is server-to-client requests: a dual-era
server can push `elicitation/create` or `sampling/createMessage` only on a
legacy session, and a legacy caller can only be asked by a request. Over the
modern session such a server answers its legacy-only tools -32601 (the
official suite's `test_elicitation`, `test_sampling`).

- **Who is legacy.** The caller's revision travels with the call: mcpx's MCP
  server records it per request, `mcpx serve` sends it to the daemon as
  `X-Mcpx-Caller-Protocol` on `/v1/call` and `/v1/ask`, and the pool reads it.
  A call with no caller revision (the CLI, `/v1` direct, schema refresh) is
  modern.
- **Pool.** The two sessions are two instances under the same scope key; the
  key is still the caller's identity, and the era is a second axis. Each lane
  has its own `max`, so a global scope (max 1) holds one of each, and a legacy
  call the server never finishes cannot stall modern callers. A legacy caller
  also takes a modern-lane instance that settled legacy (a legacy-only server
  needs no second session). Eviction stays within a lane.
- **Fallback.** If the legacy-only start fails (a modern-only server refuses
  `initialize`), the pool remembers it for its lifetime and legacy callers
  share the modern session, as under `prefer-discover`.
- **Era cache.** The legacy session is forced, so it neither reads nor writes
  the cache; the cache keeps describing the server's own era.
- **Attribution.** A legacy HTTP server's request on a POST's response stream
  is handed to the answer handler with that request's context, so the daemon
  attributes it to the call that provoked it rather than inferring it from who
  else shares the session (it could not when two calls were in flight).
- **Why not the default.** For a stdio server the second session is a second
  process for the same key -- a second browser, a second index -- which the
  pool otherwise never does. Opt in where the server is cheap to run twice or
  where legacy callers need its questions.

## stdio

`server/discover` is sent with the full `_meta` (newest version mcpx offers,
client capabilities, client info). Then:

| answer | era | next |
| --- | --- | --- |
| a result with a `supportedVersions` array | modern | newest mutual version; none mutual is an error |
| `-32020`, `-32021`, `-32022` | modern | `-32022` retries with the newest mutual version from `data.supported`, each version at most once; otherwise an error. Never `initialize`. |
| any other error | legacy | `initialize` + `notifications/initialized` |
| a success that is not a DiscoverResult | legacy | a catch-all handler answered under legacy semantics |
| nothing within `upstream.probeTimeout` | undecided | `initialize` is sent *alongside* the pending discover; whichever settles the era first wins |
| the connection closes | legacy | `ErrClosedDuringProbe`; the pool starts a new process and connects legacy-only |

The timeout does not abandon the discover because the commonest slow server is
a modern one being downloaded by `npx`. Giving up on its answer at 2s would
classify it legacy and then cache that.

## Streamable HTTP

The probe is a POST of `server/discover` with `MCP-Protocol-Version` equal to
the `_meta` version and no session id. A non-2xx body that is a JSON-RPC
response whose id matches the request is delivered as that response, so a
`-32022` reaches the retry logic. Otherwise the transport returns
`*HTTPStatusError` (status, body, parsed JSON-RPC error). A 4xx with a
recognised modern error is modern; any other 4xx falls back to `initialize`;
connection failures and 5xx are errors and nothing is cached. When
`initialize` is itself refused with a bare 400/404/405 the error is a
`*LegacyHTTPRefusedError`, which is the hook for falling back to the
deprecated HTTP+SSE transport (not implemented here). There is no probe
timeout on HTTP: status codes decide, and a slow server is only slow.

## Era cache

Key: a SHA-256 of the process definition only -- command, args, env, cwd,
transport, url, headers (JSON-encoded, so map keys are sorted). Not
`PoolID()`: that includes leasing knobs and hashes headers in map iteration
order, so it differs between runs.

Value: era, negotiated version, when, and how it was learned. Stored in memory
and in `<state>/upstream-eras.json` (`upstream.eraCache`, default on), written
atomically (temp file + rename, mode 0600) with a version field. A missing,
truncated, non-JSON or other-version file is treated as empty and rewritten on
the next start.

A cached legacy server gets `initialize` first and no discover at all. A
cached modern server gets the normal probe. If the cached era turns out wrong
the probe continues in the normal order, the cache is rewritten, and the pool
emits `server.era.stale`. `force-*` never reads or writes the cache.

`GET /v1/protocol` reports per upstream server the effective preference, the
live era and negotiated version, `eraSource` (`probe`, `cache`, `forced`), and
the cached record.
