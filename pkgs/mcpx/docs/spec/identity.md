# Who an MCP request is from

mcpx's own MCP server passes every call on to an upstream server, and which
*instance* of that server answers depends on its `scope`: one per session, per
cwd, per pid, per repository, or per call. So every request has to answer the
question "who is asking?", and the answer has to come from the transport, not
from the process.

## What went wrong

The backend used to ask the process. `mcpSession()` returned
`$MCPX_SESSION_ID` or `mcp-<pid>`, and `callContext()` filled in the process's
cwd and pid. That is right for `mcpx serve`, where one process serves one
client over stdio. It is wrong inside the daemon, which serves `/mcp` for
every HTTP client from one process: every client, every `Mcp-Session-Id`,
resolved to `session:mcp-<daemonpid>`, `cwd:<daemon's cwd>` and
`pid:<daemonpid>`. Two agents talking to one daemon shared one browser, one
REPL, one set of cookies, and saw each other's `mcpx_exec` artifacts in
`resources/list` (conflict #2 in the WP7 record). Over stdio the same test
gave separate instances, so the two transports disagreed about what `scope:
session` means.

## What mcpx does now

`mcpserver` resolves an `Identity` for each request from the connection it
arrived on, and puts it in the request's context
(`internal/mcpserver/identity.go`). The backend builds the daemon call context
from that (`mcpCaller` in `internal/cli/serve.go`):

| request arrives on | identity | session | cwd / pid |
| --- | --- | --- | --- |
| stdio (`mcpx serve`) | the process | `$MCPX_SESSION_ID`, else `mcp-<pid>` | the process's |
| a legacy Streamable HTTP session | its `Mcp-Session-Id` | `mcp-<session id>` | unknown |
| any request carrying `_meta["dev.mcpx/session"]` | that name | the name, verbatim | unknown |
| a 2026-07-28 request without it, or a legacy POST outside a session | none | none: a fresh call id | unknown |

The rows are in precedence order except that an explicit
`_meta["dev.mcpx/session"]` wins over the connection's own identity.

### Why cwd and pid are unknown over HTTP

The daemon's cwd and pid are not the client's, and the transport carries
neither. A scope that needs one (`cwd`, `repo`, `worktree`, `pid`) degrades to
per-call and logs the degradation once, which is the direction that can't
leak one client's state into another. A client's `roots` would be a better
source for cwd. That needs a `roots/list` round trip that mcpx doesn't make
yet.

### Why a modern request gets no identity

2026-07-28 has no sessions. The streamable-http page tells a server to ignore
`Mcp-Session-Id` and not to mint one, so no connection-level identity exists.
mcpx doesn't invent one: an unnamed request is its own scope, which for a
`session`-scoped upstream means a new instance per request.

### `dev.mcpx/session`

For a client that wants continuity, such as an agent host that keeps one
browser across its calls, mcpx reads `_meta["dev.mcpx/session"]` (a string)
from any request of any revision. The key uses mcpx's own prefix because the
specification reserves `io.modelcontextprotocol/` and defines no session key
of its own. The value is used verbatim as the session id, so a client can join
the same session a CLI user names with `--session` or `$MCPX_SESSION_ID`.
That is deliberate. Anyone who can reach the daemon can already run any tool
as its user (see the warning the daemon prints for a non-loopback address), so
a session name is a label and grants no authority.

## What it touches

Identity reaches every backend path that picks an instance: `mcpx_call`,
`mcpx_exec` (its artifact session), `resources/read`, `prompts/get`,
`completion/complete`, the artifact listing in `resources/list`, and the ask
path (`/v1/ask` with the same context). A caller with no identity lists no
artifacts, because the daemon reads an empty session as "everyone's".

The tests are
`2025-11-25/transport/http-mcp-clients-do-not-share-scoped-instances`,
`2026-07-28/transport/client-named-session-is-kept-across-requests`,
`2026-07-28/transport/unnamed-requests-are-their-own-scope` (e2e, against a
session-scoped fakemcp), and the `TestEachConnectionHasItsOwnIdentity` cases
in `internal/mcpserver/wp11_test.go`.
