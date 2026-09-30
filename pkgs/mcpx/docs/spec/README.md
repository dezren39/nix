# docs/spec

```
created:      2026-09-30T12:00:00-05:00
last-updated: 2026-09-30T12:00:00-05:00
increment:    1
status:       standard
tags:         area:protocol, area:spec
description:  an index of the protocol records: what the specification asks,
              what mcpx does on the wire, and how that is measured.
```

[`../protocol.md`](../protocol.md) is the design, and where to start. These
are the records under it, each tested by name
(`<revision>/<area>/<requirement>`).

**What the specification asks**, independent of mcpx:

| file | one line |
| --- | --- |
| [server-obligations.md](server-obligations.md) | per revision, what a server MUST, SHOULD and MAY do, including what the prose implies and never states |
| [client-obligations.md](client-obligations.md) | the same for a client, and what a proxy owes on top |

**What mcpx does**, as a server and as a client:

| file | one line |
| --- | --- |
| [transport.md](transport.md) | mcpx as a server on the wire: stdio and Streamable HTTP, per revision, headers, statuses, sessions, `Origin` |
| [messages.md](messages.md) | what mcpx sends per revision: the 2026 envelope, per-revision fields, listen, not-found, tasks, `requestState` |
| [identity.md](identity.md) | who an MCP request is from, per connection, and what scope that gives it |
| [client.md](client.md) | mcpx as a client: headers, `x-mcp-header`, results, lifecycle, listen, elicitation, resumption, HTTP+SSE |
| [era-probe.md](era-probe.md) | how mcpx decides an upstream's era, and the cache that makes it a one-time cost |

**How it is measured**:

| file | one line |
| --- | --- |
| [requirements.md](requirements.md) | the catalogue: every normative requirement of every revision, one row each, with its quote and anchor |
| [revision-conflicts.md](revision-conflicts.md) | the rules that change between revisions, and where one revision contradicts itself |
| [conformance.md](conformance.md) | how the matrix is built from the catalogue, the test covers and the gaps, and what building it found |
| [conformance-matrix.md](conformance-matrix.md) | generated: every requirement, per revision and side, with the test that verifies it or the issue that tracks the gap |
| [official-suite.md](official-suite.md) | the official `modelcontextprotocol/conformance` suite against mcpx, and which failures are defects |

**Not here:** how mcpx compares with the revisions side by side, with opencode,
lootbox and Cloudflare code mode, and every place two of them disagree, is
[`../compare/`](../compare/README.md) — start with
[matrix-revisions.md](../compare/matrix-revisions.md) and
[conflicts.md](../compare/conflicts.md).
