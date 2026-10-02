package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/mcpheaders"
)

// The reserved _meta keys a 2026-07-28 request carries.
const (
	MetaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaClientInfo         = "io.modelcontextprotocol/clientInfo"
	MetaLogLevel           = "io.modelcontextprotocol/logLevel"
	MetaSubscriptionID     = "io.modelcontextprotocol/subscriptionId"
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
// Elicitation form mode it always answers -- with cancel when nobody is
// listening, which is the truthful answer and better than silence. URL mode
// needs someone to open the URL, so it is declared only with a handler
// installed: the broker stores the URL, shows it, and takes the answer and
// the server's completion notification. Sampling it can only pass on to
// something with a model, so it too needs a handler.
//
// The shapes differ by era: 2026-07-28 dropped roots.listChanged along with
// notifications/roots/list_changed, so a modern request declares roots as {}.
// A legacy initialize is sent before any version is agreed, so it declares
// in the shape of the version it offers (ProtocolVersion); form and url are
// what that revision defines, and an older server ignores keys it does not
// know.
func (c *Client) capabilities(modern bool) map[string]any {
	c.mu.Lock()
	h := c.onElicit
	c.mu.Unlock()
	elicitation := map[string]any{"form": map[string]any{}}
	if h != nil {
		elicitation["url"] = map[string]any{}
	}
	caps := map[string]any{
		"elicitation": elicitation,
		"roots":       map[string]any{"listChanged": false},
	}
	if modern {
		caps["roots"] = map[string]any{}
	}
	if h != nil {
		caps["sampling"] = map[string]any{}
	}
	return caps
}

// elicitModeDeclared reports whether a mode was declared for the revision in
// use. A 2025-06-18 server never heard of url mode: mcpx's initialize named
// it, but the server answered with a revision that has no such thing.
func (c *Client) elicitModeDeclared(mode string) bool {
	c.mu.Lock()
	h := c.onElicit
	c.mu.Unlock()
	switch mode {
	case "", "form":
		return true
	case "url":
		return h != nil && (c.Era == EraModern || c.Negotiated >= "2025-11-25")
	}
	return false
}

// withMeta adds the per-request metadata 2026-07-28 requires.
//
// Version and capabilities are required on every request -- the server MUST
// NOT infer capabilities from earlier ones -- and client info is a SHOULD.
// Keys the caller already set are kept, so a progress token or a deliberate
// override survives.
func (c *Client) withMeta(ctx context.Context, params json.RawMessage, version string) (json.RawMessage, error) {
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
	set(MetaClientCapabilities, narrowCapabilities(c.capabilities(true), ClientCapabilitiesFrom(ctx)))
	set(MetaClientInfo, map[string]any{"name": c.clientName, "version": c.clientVersion})
	c.mu.Lock()
	level := c.logLevel
	c.mu.Unlock()
	if level != "" {
		set(MetaLogLevel, level)
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	m["_meta"] = b
	return json.Marshal(m)
}

type capsKey struct{}

// WithClientCapabilities says whose capabilities a request is made on behalf
// of: the client mcpx is relaying for, as it declared them on its own
// 2026-07-28 request.
//
// A modern request declares capabilities per request, and the server decides
// from that one declaration whether it may proceed. When mcpx fronts an
// upstream as a pass-through, the declaration that matters is its client's:
// mcpx declaring sampling on behalf of a client that declared nothing made
// the upstream run a tool that requires sampling, where it should have
// answered -32021 MissingRequiredClientCapability for the client to see.
func WithClientCapabilities(ctx context.Context, caps json.RawMessage) context.Context {
	if len(caps) == 0 {
		return ctx
	}
	return context.WithValue(ctx, capsKey{}, caps)
}

// ClientCapabilitiesFrom is what WithClientCapabilities stored, or nil.
func ClientCapabilitiesFrom(ctx context.Context) json.RawMessage {
	if ctx == nil {
		return nil
	}
	caps, _ := ctx.Value(capsKey{}).(json.RawMessage)
	return caps
}

type callerVersionKey struct{}

// WithCallerVersion says which protocol revision the client mcpx is relaying
// for speaks. The pool reads it under protocol: follow, to pick an upstream
// session of the same era; see pool.Acquire.
func WithCallerVersion(ctx context.Context, version string) context.Context {
	if version == "" {
		return ctx
	}
	return context.WithValue(ctx, callerVersionKey{}, version)
}

// CallerVersionFrom is what WithCallerVersion stored, or "".
func CallerVersionFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(callerVersionKey{}).(string)
	return v
}

// CallerLegacy reports whether ctx carries a caller revision from before
// 2026-07-28 -- one that has a session a server can send requests on.
func CallerLegacy(ctx context.Context) bool {
	v := CallerVersionFrom(ctx)
	return v != "" && v < "2026-07-28"
}

// ErrNotRelayed is a handler declining a request it could not pass on, so
// the client answers it itself.
var ErrNotRelayed = errors.New("not relayed")

// declaresCapability reports whether a clientCapabilities object names capability.
func declaresCapability(caps json.RawMessage, capability string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(caps, &m) != nil {
		return false
	}
	_, ok := m[capability]
	return ok
}

// narrowCapabilities is mcpx's own declaration less every capability the
// relayed client did not declare. Never wider than mcpx's own: mcpx must not
// declare something it cannot answer just because its client can.
func narrowCapabilities(own map[string]any, relayed json.RawMessage) map[string]any {
	if len(relayed) == 0 {
		return own
	}
	var theirs map[string]json.RawMessage
	if json.Unmarshal(relayed, &theirs) != nil {
		return own
	}
	out := map[string]any{}
	for k, v := range own {
		if _, ok := theirs[k]; ok {
			out[k] = v
		}
	}
	return out
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
		if h != nil && declaresCapability(ClientCapabilitiesFrom(ctx), "roots") {
			// The client mcpx relays for has roots of its own, and they are
			// what the server means. The handler relays the question when
			// it can tie it to that client's call; otherwise mcpx answers
			// with its own.
			out, err := h(ctx, method, params)
			if err == nil {
				return out, nil
			}
			if !errors.Is(err, ErrNotRelayed) {
				return nil, &rpcError{Code: -32603, Message: err.Error()}
			}
		}
		if roots == nil {
			roots = []Root{}
		}
		return map[string]any{"roots": roots}, nil
	case "elicitation/create", "sampling/createMessage":
		if method == "elicitation/create" {
			var p struct {
				Mode string `json:"mode"`
			}
			_ = json.Unmarshal(params, &p)
			if !c.elicitModeDeclared(p.Mode) {
				// "Server sends an elicitation/create request with a mode
				// not declared in client capabilities: -32602." Answering
				// anyway would teach the server that undeclared works.
				return nil, &rpcError{Code: codeInvalidParams,
					Message: fmt.Sprintf("elicitation mode %q was not declared by this client", p.Mode)}
			}
		}
		if h != nil {
			out, err := h(ctx, method, params)
			if err != nil {
				return nil, &rpcError{Code: -32603, Message: err.Error()}
			}
			if method == "elicitation/create" {
				out = applyDefaults(out, params)
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
	// Answered concurrently, each under its own key. One at a time, a
	// client of mcpx that answers questions inline saw only the first of
	// several the server asked together, and had to come back once per
	// question for what the server meant as a single round.
	responses := map[string]any{}
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		first error
	)
	for key, req := range ir.InputRequests {
		wg.Add(1)
		go func(key, method string, params json.RawMessage) {
			defer wg.Done()
			out, rerr := c.answer(withInputRound(withInputKey(ctx, key), len(ir.InputRequests)), method, params)
			mu.Lock()
			defer mu.Unlock()
			if rerr != nil {
				if first == nil {
					first = fmt.Errorf("server asked for %s (%s): %w", method, key, rerr)
				}
				return
			}
			responses[key] = out
		}(key, req.Method, req.Params)
	}
	wg.Wait()
	if first != nil {
		return nil, first
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

// applyDefaults fills an accepted form answer's missing fields from the
// requested schema's defaults.
//
// 2025-11-25: "Clients that support defaults SHOULD pre-populate form fields
// with these values." mcpx has no form; whoever answers through the broker
// sends only what they chose to set. Pre-populating means the same thing
// here as in a form nobody edited: what was left out takes its default. A
// field the answerer did set is never overwritten.
//
// https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation#requested-schema
func applyDefaults(out any, params json.RawMessage) any {
	var p struct {
		Mode            string `json:"mode"`
		RequestedSchema struct {
			Properties map[string]struct {
				Default json.RawMessage `json:"default"`
			} `json:"properties"`
		} `json:"requestedSchema"`
	}
	b, err := json.Marshal(out)
	if err != nil {
		return out
	}
	var res map[string]json.RawMessage
	if json.Unmarshal(b, &res) != nil {
		return out
	}
	// ElicitResult.content is an object when present; a handler's nil map
	// marshals as null, which no revision allows.
	if raw, ok := res["content"]; ok && string(raw) == "null" {
		delete(res, "content")
		out = res
	}
	if json.Unmarshal(params, &p) != nil || (p.Mode != "" && p.Mode != "form") {
		return out
	}
	var action string
	_ = json.Unmarshal(res["action"], &action)
	if action != "accept" {
		return out
	}
	content := map[string]json.RawMessage{}
	if raw, ok := res["content"]; ok && string(raw) != "null" {
		if json.Unmarshal(raw, &content) != nil {
			return out
		}
	}
	changed := false
	for name, prop := range p.RequestedSchema.Properties {
		if _, set := content[name]; set || len(prop.Default) == 0 {
			continue
		}
		content[name] = prop.Default
		changed = true
	}
	if !changed {
		return out
	}
	cb, err := json.Marshal(content)
	if err != nil {
		return out
	}
	res["content"] = cb
	return res
}

// cancelled tells the server a request is no longer wanted, where the
// revision in use has the client say so.
//
//   - initialize is never cancelled: every legacy revision says the client
//     MUST NOT, since a half-finished handshake has no defined state.
//   - Over modern Streamable HTTP, closing the response stream IS the
//     cancellation, and the caller's context closing it has already done
//     that; 2026-07-28 expects no notifications/cancelled there.
//   - Everywhere else -- legacy, and modern stdio, where it is a MUST -- the
//     notification is sent, on its own context so it outlives the request.
//
// https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/cancellation
func (c *Client) cancelled(method string, id int64) {
	if method == "initialize" {
		return
	}
	if _, isHTTP := c.t.(*HTTPTransport); isHTTP && c.metaVersion != "" {
		return
	}
	cp, _ := json.Marshal(map[string]any{"requestId": id, "reason": "timeout"})
	ctx, cancel := context.WithTimeout(context.Background(), defaults.UpstreamCancelSendTimeout)
	defer cancel()
	_ = c.notify(ctx, "notifications/cancelled", cp)
}

// mirrorsHeaders reports whether this connection must mirror x-mcp-header
// parameters: modern, over HTTP. stdio clients MAY ignore the annotations,
// and a legacy revision never defined them.
func (c *Client) mirrorsHeaders() bool {
	_, isHTTP := c.t.(*HTTPTransport)
	return isHTTP && c.Era == EraModern
}

// Warning is something mcpx noticed about a server that is not an error.
type Warning struct {
	Tool   string `json:"tool,omitempty"`
	Reason string `json:"reason"`
}

// checkToolHeaders records each tool's x-mcp-header parameters and drops the
// tools whose annotations are invalid, warning about each.
func (c *Client) checkToolHeaders(tools []Tool) []Tool {
	if !c.mirrorsHeaders() {
		return tools
	}
	headers := map[string][]mcpheaders.Param{}
	invalid := map[string]string{}
	kept := tools[:0:0]
	for _, t := range tools {
		hp, err := mcpheaders.ToolParams(t.InputSchema)
		if err != nil {
			invalid[t.Name] = err.Error()
			c.warn(Warning{Tool: t.Name, Reason: "excluded: invalid x-mcp-header: " + err.Error()})
			continue
		}
		headers[t.Name] = hp
		kept = append(kept, t)
	}
	c.mu.Lock()
	c.toolHeaders, c.invalidTools = headers, invalid
	c.mu.Unlock()
	return kept
}

// toolCallHeaders puts a call's Mcp-Param-* headers on its context. The
// tool list is read first if this connection has not read it, or again when
// refresh is set, since the annotations come from it.
func (c *Client) toolCallHeaders(ctx context.Context, name string, args any, refresh bool) (context.Context, error) {
	if !c.mirrorsHeaders() {
		return ctx, nil
	}
	c.mu.Lock()
	known := c.toolHeaders != nil
	c.mu.Unlock()
	if !known || refresh {
		if _, err := c.ListTools(ctx); err != nil {
			return nil, err
		}
	}
	c.mu.Lock()
	why, bad := c.invalidTools[name]
	hp := c.toolHeaders[name]
	c.mu.Unlock()
	if bad {
		return nil, fmt.Errorf("tool %q was excluded because its x-mcp-header annotations are invalid: %s", name, why)
	}
	h, err := mcpheaders.Values(hp, args)
	if err != nil {
		return nil, fmt.Errorf("tool %q: %w", name, err)
	}
	return withExtraHeaders(ctx, h), nil
}

func (c *Client) warn(w Warning) {
	c.mu.Lock()
	f := c.notif.OnWarning
	c.mu.Unlock()
	if f != nil {
		f(w)
	}
}

type inputKeyCtx struct{}

func withInputKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, inputKeyCtx{}, key)
}

// InputKey is the key a 2026-07-28 server gave the question being answered
// on ctx -- its inputRequests key -- or "" for a question that arrived as a
// request of its own. A gateway that relays the question keeps the key, so
// the client it asks sees the server's own name for it.
func InputKey(ctx context.Context) string {
	k, _ := ctx.Value(inputKeyCtx{}).(string)
	return k
}

type inputRoundCtx struct{}

func withInputRound(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, inputRoundCtx{}, n)
}

// InputRound is how many questions the 2026-07-28 result being answered on
// ctx asked at once -- its inputRequests count -- or 0 when the question did
// not come from such a result.
//
// A gateway relaying the round needs it: the questions are answered
// concurrently and arrive one at a time, so without knowing how many are
// coming it can only guess when the round is complete, and a guess is a
// timeout that is too short on a loaded machine and wasted on a quick one.
func InputRound(ctx context.Context) int {
	n, _ := ctx.Value(inputRoundCtx{}).(int)
	return n
}
