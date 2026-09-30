package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// The reserved _meta keys a 2026-07-28 request carries.
const (
	MetaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaClientInfo         = "io.modelcontextprotocol/clientInfo"
)

// Options configure a connection.
type Options struct {
	ClientName    string
	ClientVersion string
	Preference    Preference
	// OnServerRequest answers elicitation and sampling. Given here rather
	// than installed afterwards because capabilities are declared during the
	// handshake: a handler added later cannot be declared, so a server
	// would never learn it exists.
	OnServerRequest ElicitHandler
	// Roots are the directories servers may work within, served from the
	// first request rather than from whenever they happen to be set.
	Roots []Root
	// Cached is the era this server configuration was last found to speak,
	// tried first in place of the preference's order. Ignored when the
	// preference forces an era.
	Cached Era
	// ProbeTimeout bounds how long a stdio server/discover may go unanswered
	// before initialize is sent alongside it. Zero means the default.
	ProbeTimeout time.Duration
	// ModernVersions narrows the modern revisions offered, newest first.
	// Nil means all of ModernVersions.
	ModernVersions []string
}

// NewWithOptions connects with everything known up front.
func NewWithOptions(ctx context.Context, t Transport, o Options) (*Client, error) {
	return newClient(ctx, t, o)
}

// capabilities are what mcpx declares as a client, in either era.
//
// Declared only where mcpx can actually deliver. Roots it always serves.
// Elicitation it always answers -- with cancel when nobody is listening,
// which is the truthful answer and better than silence. Sampling it can only
// pass on to something with a model, so it is declared only when a handler
// is installed to do that; a server told sampling works when it does not
// waits out a deadline for nothing.
func (c *Client) capabilities() map[string]any {
	c.mu.Lock()
	h := c.onElicit
	c.mu.Unlock()
	caps := map[string]any{
		"elicitation": map[string]any{},
		"roots":       map[string]any{"listChanged": false},
	}
	if h != nil {
		caps["sampling"] = map[string]any{}
	}
	return caps
}

// withMeta adds the per-request metadata 2026-07-28 requires.
//
// Version and capabilities are required on every request -- the server MUST
// NOT infer capabilities from earlier ones -- and client info is a SHOULD.
// Keys the caller already set are kept, so a progress token or a deliberate
// override survives.
func (c *Client) withMeta(params json.RawMessage, version string) (json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &m); err != nil {
			return nil, fmt.Errorf("params must be an object to carry _meta: %w", err)
		}
	}
	meta := map[string]any{}
	if raw, ok := m["_meta"]; ok {
		_ = json.Unmarshal(raw, &meta)
	}
	set := func(k string, v any) {
		if _, ok := meta[k]; !ok {
			meta[k] = v
		}
	}
	set(MetaProtocolVersion, version)
	set(MetaClientCapabilities, c.capabilities())
	set(MetaClientInfo, map[string]any{"name": c.clientName, "version": c.clientVersion})
	b, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	m["_meta"] = b
	return json.Marshal(m)
}

// answer resolves one server-initiated request.
//
// Shared by both eras: a legacy server sends the request on the wire, a
// modern one returns it inside an input_required result. One function means
// the two cannot answer differently.
//
// Roots are answered here whatever handler is installed. Before this, an
// installed handler received every request, and the daemon's handler knew
// elicitation and sampling only -- so with the broker on, which is always,
// every roots/list got "not implemented" while the roots sat configured.
func (c *Client) answer(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	c.mu.Lock()
	h := c.onElicit
	roots := append([]Root(nil), c.roots...)
	c.mu.Unlock()

	switch method {
	case "ping":
		// A server is allowed to ping its client in every revision. mcpx
		// answered method-not-found, which a server reasonably reads as a
		// dead connection -- so the liveness probe reported the opposite of
		// the truth.
		return map[string]any{}, nil
	case "roots/list":
		if roots == nil {
			roots = []Root{}
		}
		return map[string]any{"roots": roots}, nil
	case "elicitation/create", "sampling/createMessage":
		if h != nil {
			out, err := h(ctx, method, params)
			if err != nil {
				return nil, &rpcError{Code: -32603, Message: err.Error()}
			}
			return out, nil
		}
		if method == "elicitation/create" {
			// Cancel, not decline. Nobody was asked, so nobody said no.
			return map[string]any{"action": "cancel"}, nil
		}
	}
	return nil, &rpcError{Code: -32601, Message: "mcpx does not implement " + method}
}

// inputRequired is the part of a 2026-07-28 result that asks for more.
type inputRequired struct {
	ResultType    string `json:"resultType"`
	InputRequests map[string]struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	} `json:"inputRequests"`
	RequestState *string `json:"requestState"`
}

// resolveInput answers what an input_required result asked for and returns
// the params to retry the original request with.
//
// This is how a 2026-07-28 server elicits, samples or asks for roots: not by
// sending a request of its own -- there is no connection to send it on --
// but by answering "not yet, first tell me these" and expecting the same
// request again with the answers attached. A client that does not do this
// sees what looks like an empty result and never learns a question was
// asked.
func (c *Client) resolveInput(ctx context.Context, params json.RawMessage, ir inputRequired) (json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &m); err != nil {
			return nil, err
		}
	}
	responses := map[string]any{}
	for key, req := range ir.InputRequests {
		out, rerr := c.answer(ctx, req.Method, req.Params)
		if rerr != nil {
			return nil, fmt.Errorf("server asked for %s (%s): %w", req.Method, key, rerr)
		}
		responses[key] = out
	}
	delete(m, "inputResponses")
	delete(m, "requestState")
	if len(responses) > 0 {
		b, err := json.Marshal(responses)
		if err != nil {
			return nil, err
		}
		m["inputResponses"] = b
	}
	if ir.RequestState != nil {
		// Opaque: passed back exactly as received, never interpreted.
		b, err := json.Marshal(*ir.RequestState)
		if err != nil {
			return nil, err
		}
		m["requestState"] = b
	}
	return json.Marshal(m)
}

// maxInputRounds is read once so a test can see the bound it is testing.
var maxInputRounds = defaults.InputRounds
