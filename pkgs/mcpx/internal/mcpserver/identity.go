package mcpserver

import (
	"context"
	"encoding/json"
)

// MetaSession is the _meta key a client may name its own session with.
//
// Under mcpx's prefix, not the specification's: 2026-07-28 has no sessions
// at all, so there is no standard key to read, and the reserved
// io.modelcontextprotocol/ prefix is not ours to extend. See
// docs/spec/identity.md.
const MetaSession = "dev.mcpx/session"

// IdentitySource says where a request's identity came from, which decides
// what the backend may build from it.
type IdentitySource string

const (
	// IdentityNone: the transport names no client. A modern HTTP request
	// without MetaSession, or a legacy HTTP request outside a session. The
	// only honest scope is the request itself.
	IdentityNone IdentitySource = ""
	// IdentityProcess: the connection is this process's own -- stdio,
	// where one process serves exactly one client -- so the process is the
	// client, and its cwd and pid are the client's too.
	IdentityProcess IdentitySource = "process"
	// IdentitySession: a legacy Streamable HTTP session, keyed by the
	// Mcp-Session-Id mcpx minted at initialize.
	IdentitySession IdentitySource = "session"
	// IdentityClient: the client named itself with MetaSession.
	IdentityClient IdentitySource = "client"
)

// Identity is who one request is from, as far as its transport can tell.
//
// It exists because every HTTP client of the daemon's /mcp used to be
// resolved to the daemon's own pid and cwd: the backend asked the process
// who it was, and inside the daemon the process is the daemon. So every
// client shared one "session", and every session-scoped upstream instance
// with it (conflict #2).
type Identity struct {
	Source IdentitySource
	// Key is stable for the life of the client; empty for IdentityNone.
	Key string
}

type identityKey struct{}

func withIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom is the identity of the request ctx belongs to. A context that
// never passed through a connection -- a test, the REST projection of a
// tool -- has none, which is the per-request answer.
func IdentityFrom(ctx context.Context) Identity {
	id, _ := ctx.Value(identityKey{}).(Identity)
	return id
}

// identityFor resolves one request's identity on this connection.
//
// A name the client gives itself wins: it is an explicit statement, and the
// only one a sessionless revision can make. Otherwise the connection's own
// identity, if the transport gave it one.
func (c *Conn) identityFor(params json.RawMessage) Identity {
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(params, &p) == nil {
		var named string
		if json.Unmarshal(p.Meta[MetaSession], &named) == nil && named != "" {
			return Identity{Source: IdentityClient, Key: named}
		}
	}
	switch {
	case c.process:
		return Identity{Source: IdentityProcess, Key: c.id}
	case c.legacy && c.id != "":
		return Identity{Source: IdentitySession, Key: c.id}
	}
	return Identity{}
}
