# Upstream era probe

How mcpx decides whether a server it fronts speaks the modern (2026-07-28,
per-request `_meta`) protocol or a legacy (`initialize`) one, and how it
remembers the answer.

Sources: [versioning](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions),
[stdio](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#backward-compatibility),
[Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#backward-compatibility).

## Order

The default is modern first (`upstream.protocol=modern`; a server's own
`protocol` key overrides it). Legacy first used to be the default on the
argument that nearly every server is legacy and the probe costs each of them a
round trip. The spec prescribes the opposite order for a dual-era client, and
the era cache turns the probe into a one-time cost per server configuration,
so the argument no longer holds.

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
