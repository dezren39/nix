// Package mcpclient is a minimal Model Context Protocol client.
//
// It implements only what a code-mode host needs: initialize, tools/list,
// tools/call, resources/list, resources/read and ping. Requests are
// id-multiplexed, so a single connection serves many concurrent callers.
package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/mcpheaders"
	"github.com/dezren39/mcpx/internal/spec"
)

// ProtocolVersion is the MCP revision mcpx negotiates.
const ProtocolVersion = "2025-11-25"

// ModernVersions are the per-request-metadata revisions mcpx can speak,
// newest first.
var ModernVersions = []string{"2026-07-28"}

// Transport moves JSON-RPC frames to and from a server.
type Transport interface {
	// Send writes one JSON-RPC message.
	Send(ctx context.Context, msg []byte) error
	// Recv returns the next JSON-RPC message, blocking until one arrives.
	Recv() ([]byte, error)
	// Close shuts the transport down.
	Close() error
	// Info describes the transport for diagnostics.
	Info() string
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("mcp error %d: %s (%s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message)
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
	// sendErr is set, instead of a reply, when the request never reached
	// the server; see begin.
	sendErr error
}

// Client is a connected MCP session.
type Client struct {
	t      Transport
	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan *rpcResponse
	closed  bool
	closeCh chan struct{}
	recvErr error

	ServerInfo   ServerInfo
	Capabilities map[string]json.RawMessage
	// Era is which protocol generation this connection settled on.
	Era Era
	// modern mirrors Era == EraModern for the read loop, which runs while
	// connect is still deciding and so cannot read Era without a race.
	modern atomic.Bool
	// Negotiated is the version actually in use.
	Negotiated string
	// onElicit answers server-initiated requests.
	onElicit ElicitHandler
	// notif holds notification handlers.
	notif Notifications
	// roots are the directories servers may work within.
	roots []Root
	// clientName and clientVersion identify mcpx on every modern request.
	clientName, clientVersion string
	// metaVersion is the version stamped into each request's _meta. Empty
	// for a legacy connection, whose version was settled by the handshake.
	metaVersion string
	// Source says how Era was settled: SourceProbe, SourceCache or
	// SourceForced.
	Source string
	// CachedEraWrong is set when a cached era was tried first and the server
	// turned out to speak the other one.
	CachedEraWrong bool
	// modernVersions are the modern revisions this client offers, newest
	// first; ModernVersions unless a caller narrowed them.
	modernVersions []string
	// probeTimeout is how long server/discover may go unanswered on stdio
	// before initialize is sent alongside it.
	probeTimeout time.Duration
	// logLevel is the level stamped into each modern request's _meta; empty
	// means none, and a modern server then sends no log messages.
	logLevel string
	// relays are the calls in flight whose host asked for progress or log
	// messages, each with the progress token mcpx sent upstream for it.
	relays map[*Relay]*relayState
	// asked are the server's requests to us still being answered, by id
	// (compact JSON), so an inbound notifications/cancelled can stop one.
	asked    map[string]*askedReq
	relaySeq atomic.Int64
	// toolHeaders are each tool's x-mcp-header annotations, learned from
	// tools/list, for a modern connection over HTTP.
	toolHeaders map[string][]mcpheaders.Param
	// invalidTools are tools excluded for invalid annotations, with why.
	invalidTools map[string]string
	// listen is the subscriptions/listen stream of a modern connection.
	listen *listener
	// Instructions is the free-text guidance a server returns from
	// initialize. Servers use it to explain conventions their schemas cannot:
	// chrome-devtools-mcp, for instance, describes how page ids are obtained.
	Instructions string
}

// ServerInfo is the identity a server reports during initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Title   string `json:"title,omitempty"`
}

type initResult struct {
	ProtocolVersion string                     `json:"protocolVersion"`
	ServerInfo      ServerInfo                 `json:"serverInfo"`
	Capabilities    map[string]json.RawMessage `json:"capabilities"`
	Instructions    string                     `json:"instructions,omitempty"`
}

// Tool is one entry from tools/list.
type Tool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	// Annotations are the behaviour hints a server attaches to a tool:
	// readOnlyHint, destructiveHint, idempotentHint, openWorldHint. They are
	// kept raw because mcpx forwards them unchanged and reads only the one
	// it acts on. Without them a client has nothing but the description to
	// decide whether a call is worth confirming.
	Annotations json.RawMessage `json:"annotations,omitempty"`
	// Execution carries taskSupport ("forbidden", "optional", "required"):
	// whether the tool may be run as a task. Kept raw and forwarded, so a
	// pass-through client sees what the upstream declared.
	Execution json.RawMessage `json:"execution,omitempty"`
	// Icons and Meta are carried, not read, so a host listing an upstream
	// through mcpx sees what the server published (#207).
	Icons json.RawMessage `json:"icons,omitempty"`
	Meta  json.RawMessage `json:"_meta,omitempty"`
}

type toolsListResult struct {
	Tools      []Tool  `json:"tools"`
	NextCursor *string `json:"nextCursor,omitempty"`
}

// Resource is one entry from resources/list.
type Resource struct {
	URI         string `json:"uri,omitempty"`
	URITemplate string `json:"uriTemplate,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	// The optional fields, carried rather than read: they were dropped on
	// parse, so no later layer could pass them on (#207).
	Title       string          `json:"title,omitempty"`
	Size        *int64          `json:"size,omitempty"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
	Icons       json.RawMessage `json:"icons,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

type resourcesListResult struct {
	Resources  []Resource `json:"resources"`
	NextCursor *string    `json:"nextCursor,omitempty"`
}

type resourceTemplatesListResult struct {
	ResourceTemplates []Resource `json:"resourceTemplates"`
	NextCursor        string     `json:"nextCursor,omitempty"`
}

// Era is which protocol generation a server speaks.
type Era string

const (
	// EraLegacy establishes a session with an initialize handshake.
	// Everything published today.
	EraLegacy Era = "legacy"
	// EraModern carries the version on every request and has no handshake.
	EraModern Era = "modern"
)

// Preference controls which era to try first. The values are the canonical
// names of the upstream.protocol setting, which normalises its aliases
// (modern, legacy, force-legacy, ...) before a Preference is ever built.
type Preference string

const (
	// PreferLegacy tries initialize first and probes server/discover only
	// if that fails. Kept for a server known to be legacy but not worth
	// forcing: it saves the probe's round trip on every start.
	PreferLegacy Preference = "prefer-initialize"
	// PreferModern probes server/discover first and falls back to
	// initialize. The default, because it is what the 2026-07-28 transport
	// pages prescribe for a dual-era client, and because the era cache makes
	// its cost a one-time one per server configuration.
	PreferModern Preference = "prefer-discover"
	// ForceLegacy and ForceModern skip the fallback, for a server known to
	// be one or the other, or to diagnose which it is.
	ForceLegacy Preference = "force-initialize"
	ForceModern Preference = "force-discover"
	// PreferFollow is PreferModern for the server's own session, plus a
	// separate legacy session for callers that speak a legacy revision, so
	// a server that can only push requests to a legacy client still can.
	// The pool implements it; to NewWithOptions it means PreferModern.
	PreferFollow Preference = "follow"
)

// Where an era determination came from, as reported on Client.Source.
const (
	SourceProbe  = "probe"
	SourceCache  = "cache"
	SourceForced = "forced"
)

// New connects, discovering which era the server speaks.
func New(ctx context.Context, t Transport, clientName, clientVersion string) (*Client, error) {
	return NewWithPreference(ctx, t, clientName, clientVersion, PreferModern)
}

// NewWithPreference connects, trying the given era first.
//
// The fallback is what makes mcpx dual-era. A modern client against a legacy
// server fails, and a legacy client against a modern server fails; only
// something that can do both reaches the whole ecosystem.
func NewWithPreference(ctx context.Context, t Transport, clientName, clientVersion string, pref Preference) (*Client, error) {
	return newClient(ctx, t, Options{ClientName: clientName, ClientVersion: clientVersion, Preference: pref})
}

func newClient(ctx context.Context, t Transport, o Options) (*Client, error) {
	c := &Client{
		t:              t,
		pending:        map[int64]chan *rpcResponse{},
		closeCh:        make(chan struct{}),
		clientName:     o.ClientName,
		clientVersion:  o.ClientVersion,
		onElicit:       o.OnServerRequest,
		roots:          append([]Root(nil), o.Roots...),
		modernVersions: o.ModernVersions,
		probeTimeout:   o.ProbeTimeout,
	}
	if len(c.modernVersions) == 0 {
		c.modernVersions = ModernVersions
	}
	if c.probeTimeout <= 0 {
		c.probeTimeout = defaults.UpstreamProbeTimeout
	}
	go c.recvLoop()

	pref := o.Preference
	if pref == "" {
		pref = PreferModern
	}
	cached := o.Cached
	if pref == ForceLegacy || pref == ForceModern {
		// A forced era is an instruction, not a guess, so nothing learned
		// earlier may override it.
		cached = ""
	}
	// A cached era replaces the preference's order, not its fallback: the
	// cache is only ever a better first guess.
	switch cached {
	case EraLegacy:
		pref = PreferLegacy
	case EraModern:
		pref = PreferModern
	}

	var err error
	switch pref {
	case ForceLegacy:
		err = c.initializeLegacy(ctx, c.clientName, c.clientVersion)
		if err == nil {
			c.Era = EraLegacy
		}
	case ForceModern:
		err = c.probe(ctx, false)
	case PreferLegacy:
		err = c.legacyFirst(ctx)
	default:
		err = c.probe(ctx, true)
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	if c.Era == EraModern {
		c.startListen()
	}
	switch {
	case o.Preference == ForceLegacy || o.Preference == ForceModern:
		c.Source = SourceForced
	case cached != "" && cached == c.Era:
		c.Source = SourceCache
	default:
		c.Source = SourceProbe
		c.CachedEraWrong = cached != ""
	}
	return c, nil
}

func (c *Client) initializeLegacy(ctx context.Context, clientName, clientVersion string) error {
	var ir initResult
	if err := c.call(ctx, "initialize", c.initializeParams(), &ir); err != nil {
		return fmt.Errorf("initialize: %w", legacyHTTPRefusal(err))
	}
	return c.finishLegacy(ctx, ir)
}

func (c *Client) initializeParams() json.RawMessage {
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": ProtocolVersion,
		// Declared only where mcpx can actually deliver; see capabilities.
		"capabilities": c.capabilities(false),
		"clientInfo":   map[string]any{"name": c.clientName, "version": c.clientVersion},
	})
	return params
}

// finishLegacy records an initialize result and completes the handshake.
func (c *Client) finishLegacy(ctx context.Context, ir initResult) error {
	// Every legacy revision: if the client does not support the version
	// the server answered with, it SHOULD disconnect. Carrying on would mean
	// speaking a revision nobody agreed to.
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#version-negotiation
	if !legacyVersion(ir.ProtocolVersion) {
		return &UnsupportedVersionError{Version: ir.ProtocolVersion, Supported: LegacyVersions}
	}
	c.metaVersion = ""
	c.ServerInfo = ir.ServerInfo
	c.Capabilities = ir.Capabilities
	c.Instructions = ir.Instructions
	c.Negotiated = ir.ProtocolVersion
	c.Era = EraLegacy

	if h, ok := c.t.(interface{ setNegotiated(string) }); ok {
		h.setNegotiated(ir.ProtocolVersion)
	}
	if err := c.notify(ctx, "notifications/initialized", json.RawMessage(`{}`)); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}
	// The standalone GET stream arrived with Streamable HTTP (2025-03-26).
	if h, ok := c.t.(interface{ listen() }); ok && ir.ProtocolVersion >= "2025-03-26" {
		h.listen()
	}
	return nil
}

// LegacyVersions are the initialize-era revisions mcpx speaks as a client,
// oldest first.
var LegacyVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}

func legacyVersion(v string) bool {
	for _, s := range LegacyVersions {
		if s == v {
			return true
		}
	}
	return false
}

// UnsupportedVersionError is an initialize answered with a version mcpx
// does not speak.
type UnsupportedVersionError struct {
	Version   string
	Supported []string
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("the server chose protocol version %q, which mcpx does not speak (it speaks %v); disconnecting",
		e.Version, e.Supported)
}

// Supports reports whether the server advertised a capability.
func (c *Client) Supports(cap string) bool {
	_, ok := c.Capabilities[cap]
	return ok
}

// contextReceiver is a transport that knows which request a frame arrived
// in answer to; see HTTPTransport.RecvContext.
type contextReceiver interface {
	RecvContext() ([]byte, context.Context, error)
}

func (c *Client) recvLoop() {
	cr, _ := c.t.(contextReceiver)
	for {
		var (
			raw    []byte
			origin context.Context
			err    error
		)
		if cr != nil {
			raw, origin, err = cr.RecvContext()
		} else {
			raw, err = c.t.Recv()
		}
		if err != nil {
			c.fail(err)
			return
		}
		// A 2025-03-26 server may batch; stdio hands the array over as one
		// line, and a line starting with '[' used to be dropped whole.
		if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
			var batch []json.RawMessage
			if json.Unmarshal(trimmed, &batch) == nil {
				for _, m := range batch {
					c.dispatch(origin, m)
				}
			}
			continue
		}
		c.dispatch(origin, raw)
	}
}

// dispatch routes one received message.
func (c *Client) dispatch(origin context.Context, raw []byte) {
	// A server-initiated request has an id AND a method. Matching only on
	// the id made such a frame look like a reply to nothing and dropped it,
	// so the server waited until the call timed out. The id is kept raw:
	// a server's ids are its own, and a string id is as valid as a number.
	var probe struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.Method != "" {
		if len(probe.ID) > 0 && string(probe.ID) != "null" {
			// 2026-07-28 forbids answering a server's request; the earlier
			// revisions require it. Dropped only when 2026-07-28's rule
			// governs (see spec.Governs).
			if c.modern.Load() && spec.Current().Governs("2026-07-28") {
				// 2026-07-28 has no server-to-client requests: a server
				// asks through input_required results instead (SEP-2260,
				// SEP-2322). The stdio transport page says the client
				// MUST NOT answer one, so it is dropped unanswered --
				// while 2026-07-28 is held strictly (spec.lenient, #307).
				// Lenient, it is answered like any legacy server's, for
				// a server that has not caught up with its own revision.
				return
			}
			c.handleServerRequest(origin, probe.ID, probe.Method, probe.Params)
		} else {
			c.handleNotification(probe.Method, probe.Params)
		}
		return
	}

	var resp rpcResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return // ignore malformed frames rather than killing the session
	}
	if resp.ID == nil {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[*resp.ID]
	if ok {
		delete(c.pending, *resp.ID)
	}
	c.mu.Unlock()
	if ok {
		ch <- &resp
	}
}

// OnElicit is called when a server asks a question. Nil means mcpx answers
// on the server's behalf, which it must do rather than ignore: a server that
// asks into silence waits until the call times out.
type ElicitHandler func(ctx context.Context, method string, params json.RawMessage) (any, error)

// SetElicitHandler installs the handler for server-initiated requests.
func (c *Client) SetElicitHandler(h ElicitHandler) {
	c.mu.Lock()
	c.onElicit = h
	c.mu.Unlock()
}

// handleServerRequest answers a request the server sent to us.
//
// Always answers a legacy server; a 2026-07-28 one has no business sending
// requests and is never answered (see dispatch). The alternative -- dropping what we do not understand --
// is what the old code did by accident, and it is indistinguishable from a
// hung server.
//
// origin is the context of the request the server asked in the course of,
// when the transport knows it; the handler sees its values (which call this
// is), not its deadline -- the answer has its own.
func (c *Client) handleServerRequest(origin context.Context, id json.RawMessage, method string, params json.RawMessage) {
	base := context.Background()
	if origin != nil {
		base = context.WithoutCancel(origin)
	}
	key := idKey(id)
	ctx, cancel := context.WithTimeout(base, defaults.ElicitHandlerTimeout)
	q := &askedReq{cancel: cancel}
	c.mu.Lock()
	if c.asked == nil {
		c.asked = map[string]*askedReq{}
	}
	c.asked[key] = q
	c.mu.Unlock()
	go func() {
		defer cancel()

		result, rpcErr := c.answer(ctx, method, params)

		c.mu.Lock()
		if c.asked[key] == q {
			delete(c.asked, key)
		}
		cancelled := q.cancelled
		c.mu.Unlock()
		if cancelled {
			// The server withdrew the question: it expects no answer.
			return
		}

		reply := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			reply["error"] = rpcErr
		} else {
			reply["result"] = result
		}
		b, err := json.Marshal(reply)
		if err != nil {
			return
		}
		sctx, scancel := context.WithTimeout(context.Background(), defaults.UpstreamElicitReplyTimeout)
		defer scancel()
		_ = c.t.Send(sctx, b)
	}()
}

// askedReq is one server request being answered.
type askedReq struct {
	cancel    context.CancelFunc
	cancelled bool
}

// idKey is a JSON-RPC id in a form two spellings of the same id share.
func idKey(id json.RawMessage) string {
	var v any
	if json.Unmarshal(id, &v) != nil {
		return string(id)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// cancelAsked stops the answer to a server request the server cancelled:
// the handler's context ends, and no response is sent. A cancellation for
// no request being answered -- unknown, already answered, or malformed -- is
// ignored, as the spec allows. The reason, if any, goes to OnWarning so it
// is logged.
func (c *Client) cancelAsked(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
		Reason    string          `json:"reason"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	key := idKey(p.RequestID)
	c.mu.Lock()
	q := c.asked[key]
	if q != nil {
		q.cancelled = true
		delete(c.asked, key)
	}
	c.mu.Unlock()
	if q == nil {
		return
	}
	q.cancel()
	reason := "the server cancelled its request " + key
	if p.Reason != "" {
		reason += ": " + p.Reason
	}
	c.warn(Warning{Reason: reason})
}

// ServerMessage is a log line a server sent us.
//
// Servers emit these to explain what they are doing, and mcpx dropped every
// one. A server that logs "retrying against the replica" is telling you
// exactly why a call was slow, and losing it means diagnosing from the
// outside what was explained from the inside.
type ServerMessage struct {
	Level  string          `json:"level"`
	Logger string          `json:"logger,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Progress is an update on a long operation.
type Progress struct {
	Token    any     `json:"progressToken"`
	Progress float64 `json:"progress"`
	Total    float64 `json:"total,omitempty"`
	Message  string  `json:"message,omitempty"`
}

// Notifications a caller may subscribe to.
type Notifications struct {
	// OnMessage receives a server's log lines.
	OnMessage func(ServerMessage)
	// OnProgress receives progress on a long call.
	OnProgress func(Progress)
	// OnListChanged fires when the server says its tools, resources or
	// prompts have changed. The kind is "tools", "resources" or "prompts".
	OnListChanged func(kind string)
	// OnResourceUpdated fires when a subscribed resource changes.
	OnResourceUpdated func(uri string)
	// OnElicitationComplete fires when a url-mode elicitation finishes out
	// of band -- the person came back from the browser. Without it the
	// caller waits for the deadline to find out something already happened.
	OnElicitationComplete func(id string)
	// OnWarning receives what mcpx noticed about a server that is not an
	// error: a tool excluded for invalid annotations, a listen stream the
	// server narrowed.
	OnWarning func(Warning)
}

// Subscribe installs notification handlers.
func (c *Client) Subscribe(n Notifications) {
	c.mu.Lock()
	c.notif = n
	c.mu.Unlock()
}

// handleNotification routes a server-initiated notification.
//
// Every one of these was previously discarded. They are the server
// explaining itself, and throwing that away means every diagnosis starts
// from the outside.
func (c *Client) handleNotification(method string, params json.RawMessage) {
	c.mu.Lock()
	n := c.notif
	l := c.listen
	c.mu.Unlock()
	if l != nil && !l.accepts(method, params) {
		return
	}
	if !c.relayNotification(method, params) {
		// Progress for no request in flight, or out of order, or too soon.
		return
	}

	switch method {
	case "notifications/cancelled":
		c.cancelAsked(params)
	case "notifications/message":
		if n.OnMessage == nil {
			return
		}
		var m ServerMessage
		if json.Unmarshal(params, &m) == nil {
			n.OnMessage(m)
		}
	case "notifications/progress":
		if n.OnProgress == nil {
			return
		}
		var p Progress
		if json.Unmarshal(params, &p) == nil {
			n.OnProgress(p)
		}
	case "notifications/tools/list_changed":
		c.invalidate("tools", n)
	case "notifications/resources/list_changed":
		c.invalidate("resources", n)
	case "notifications/prompts/list_changed":
		c.invalidate("prompts", n)
	case "notifications/elicitation/complete":
		if n.OnElicitationComplete == nil {
			return
		}
		var d struct {
			ElicitationID string `json:"elicitationId"`
		}
		if json.Unmarshal(params, &d) == nil && d.ElicitationID != "" {
			n.OnElicitationComplete(d.ElicitationID)
		}
	case "notifications/resources/updated":
		if n.OnResourceUpdated == nil {
			return
		}
		var u struct {
			URI string `json:"uri"`
		}
		if json.Unmarshal(params, &u) == nil && u.URI != "" {
			n.OnResourceUpdated(u.URI)
		}
	}
}

func (c *Client) invalidate(kind string, n Notifications) {
	if n.OnListChanged != nil {
		n.OnListChanged(kind)
	}
}

// CanSubscribeResources reports whether the server declared
// resources.subscribe. Without it neither era's mechanism does anything: a
// legacy server has no resources/subscribe to answer, and a modern one is
// never sent resourceSubscriptions it did not declare (see listener.filter).
func (c *Client) CanSubscribeResources() bool {
	return declares(c.Capabilities, "resources", "subscribe")
}

// declares reports whether a capability object sets a boolean flag.
func declares(caps map[string]json.RawMessage, capability, flag string) bool {
	var v map[string]json.RawMessage
	if json.Unmarshal(caps[capability], &v) != nil {
		return false
	}
	var b bool
	_ = json.Unmarshal(v[flag], &b)
	return b
}

// SubscribeResource asks for notifications when a resource changes.
//
// The legacy revisions do this with resources/subscribe per URI. The modern
// one replaced it with the resourceSubscriptions filter of
// subscriptions/listen, so against a modern server this adds the URI to the
// connection's listen stream, which is reopened with the new filter.
func (c *Client) SubscribeResource(ctx context.Context, uri string) error {
	if c.Era == EraModern {
		c.mu.Lock()
		l := c.listen
		c.mu.Unlock()
		if l != nil {
			l.set(uri, true)
		}
		return nil
	}
	params, _ := json.Marshal(map[string]string{"uri": uri})
	var out json.RawMessage
	return c.call(ctx, "resources/subscribe", params, &out)
}

// UnsubscribeResource stops notifications for a resource.
func (c *Client) UnsubscribeResource(ctx context.Context, uri string) error {
	if c.Era == EraModern {
		c.mu.Lock()
		l := c.listen
		c.mu.Unlock()
		if l != nil {
			l.set(uri, false)
		}
		return nil
	}
	params, _ := json.Marshal(map[string]string{"uri": uri})
	var out json.RawMessage
	return c.call(ctx, "resources/unsubscribe", params, &out)
}

// ListResourceTemplates returns the server's parameterised resources.
func (c *Client) ListResourceTemplates(ctx context.Context) ([]Resource, error) {
	var tres resourceTemplatesListResult
	if err := c.call(ctx, "resources/templates/list", json.RawMessage(`{}`), &tres); err != nil {
		return nil, err
	}
	return tres.ResourceTemplates, nil
}

// SetLogLevel asks the server to send messages at or above a level.
//
// Servers send nothing until asked, so a client that never calls this sees
// no log messages and concludes the server does not emit any.
//
// 2026-07-28 removed logging/setLevel: the level travels in each request's
// _meta, and a server MUST NOT log for a request that did not ask. So against
// a modern server this only records the level, and every later request
// carries it.
func (c *Client) SetLogLevel(ctx context.Context, level string) error {
	if c.Era == EraModern {
		c.mu.Lock()
		c.logLevel = level
		c.mu.Unlock()
		return nil
	}
	if !c.Supports("logging") {
		return nil
	}
	params, _ := json.Marshal(map[string]string{"level": level})
	var out json.RawMessage
	return c.call(ctx, "logging/setLevel", params, &out)
}

// Root is a directory a server may work within.
type Root struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

// SetRoots declares the directories servers may operate on.
//
// Without this a filesystem server has no idea what it is allowed to touch
// and must be told through its own configuration, separately, in a second
// place that drifts from the first.
func (c *Client) SetRoots(roots []Root) {
	c.mu.Lock()
	c.roots = roots
	c.mu.Unlock()
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.recvErr = err
	pending := c.pending
	c.pending = map[int64]chan *rpcResponse{}
	close(c.closeCh)
	c.mu.Unlock()

	for _, ch := range pending {
		ch <- &rpcResponse{Error: &rpcError{Code: -32000, Message: "connection closed: " + err.Error()}}
	}
}

// Done is closed when the session dies.
func (c *Client) Done() <-chan struct{} { return c.closeCh }

// Err returns the error that terminated the session, if any.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recvErr
}

// Alive reports whether the session is still usable.
func (c *Client) Alive() bool {
	select {
	case <-c.closeCh:
		return false
	default:
		return true
	}
}

func (c *Client) notify(ctx context.Context, method string, params json.RawMessage) error {
	b, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	return c.t.Send(ctx, b)
}

func (c *Client) call(ctx context.Context, method string, params json.RawMessage, out any) error {
	version := c.metaVersion
	if version == "" {
		raw, err := c.roundTrip(ctx, method, params)
		if err != nil || out == nil {
			return err
		}
		return json.Unmarshal(raw, out)
	}

	// Modern: every request carries its own version and capabilities, and a
	// result may come back input_required -- answered here and retried, so
	// callers see only the final result, exactly as they would from a
	// legacy server that asked its questions on the wire.
	for round := 0; ; round++ {
		withMeta, err := c.withMeta(ctx, params, version)
		if err != nil {
			return err
		}
		raw, err := c.roundTrip(ctx, method, withMeta)
		if err != nil {
			return err
		}
		var ir inputRequired
		_ = json.Unmarshal(raw, &ir)
		switch ir.ResultType {
		case "", "complete":
			// Absent means complete: an earlier revision's result, or a
			// server that left it out.
			if out == nil {
				return nil
			}
			return json.Unmarshal(raw, out)
		case "input_required":
		default:
			// "A resultType of any value unrecognized by the client MUST
			// be considered invalid." Treating it as complete would hand a
			// caller a result that means something mcpx does not know.
			// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#resulttype
			return &InvalidResultError{Method: method, ResultType: ir.ResultType}
		}
		if round+1 >= maxInputRounds {
			return fmt.Errorf("%s: the server was still asking for input after %d rounds", method, maxInputRounds)
		}
		if params, err = c.resolveInput(ctx, params, ir); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
	}
}

// roundTrip sends one request and returns its raw result.
func (c *Client) roundTrip(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan *rpcResponse, 1)

	c.mu.Lock()
	if c.closed {
		err := c.recvErr
		c.mu.Unlock()
		if err == nil {
			err = errors.New("client closed")
		}
		return nil, err
	}
	c.pending[id] = ch
	c.mu.Unlock()

	b, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if err := c.t.Send(ctx, b); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		if ctx.Err() != nil {
			// The HTTP transport's Send lasts until the response headers
			// arrive, so a server that is slow to start answering is timed
			// out here, not below -- and was never told.
			c.cancelled(method, id)
		}
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		c.cancelled(method, id)
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// Ping issues an MCP ping, used as a liveness probe.
//
// 2026-07-28 removed ping. Against a modern server the probe is
// server/discover instead, which every modern server MUST implement and
// which, like ping, does nothing but answer.
func (c *Client) Ping(ctx context.Context) error {
	if c.Era == EraModern {
		return c.call(ctx, "server/discover", json.RawMessage(`{}`), nil)
	}
	return c.call(ctx, "ping", json.RawMessage(`{}`), nil)
}

// nextPage decides whether a list continues, and with which cursor.
//
// Every revision says a missing nextCursor is the end. 2026-07-28 adds that
// an empty string is a valid cursor -- "don't make any determination based
// on cursor value other than whether a non-null value was provided" -- so a
// modern server's "" means "ask again with ”". The legacy pages never said
// so, and a legacy server that serialises an unset cursor as "" means the
// end; asking again would loop until the page bound. A cursor that repeats
// the one just sent is also the end: following it is a loop by definition.
//
// https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/pagination#implementation-guidelines
func (c *Client) nextPage(next *string, sent *string) (string, bool) {
	if next == nil {
		return "", false
	}
	if *next == "" && c.Era != EraModern {
		return "", false
	}
	if sent != nil && *sent == *next {
		return "", false
	}
	return *next, true
}

func pageParams(cursor *string) json.RawMessage {
	if cursor == nil {
		return json.RawMessage(`{}`)
	}
	b, _ := json.Marshal(map[string]string{"cursor": *cursor})
	return b
}

// ListTools returns every tool, following pagination cursors.
//
// On a modern connection over HTTP, a tool whose x-mcp-header annotations
// are invalid is left out: the transport page says a client MUST exclude it
// and SHOULD warn, so one bad definition cannot make its headers -- and so
// every call to it -- wrong.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	var cursor *string
	for i := 0; i < defaults.ListPageLimit; i++ {
		var res toolsListResult
		if err := c.call(ctx, "tools/list", pageParams(cursor), &res); err != nil {
			return all, err
		}
		all = append(all, res.Tools...)
		next, more := c.nextPage(res.NextCursor, cursor)
		if !more {
			break
		}
		cursor = &next
	}
	return c.checkToolHeaders(all), nil
}

// ListResources returns static resources plus resource templates.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var all []Resource
	var cursor *string
	for i := 0; i < defaults.ListPageLimit; i++ {
		var res resourcesListResult
		if err := c.call(ctx, "resources/list", pageParams(cursor), &res); err != nil {
			return all, err
		}
		all = append(all, res.Resources...)
		next, more := c.nextPage(res.NextCursor, cursor)
		if !more {
			break
		}
		cursor = &next
	}
	var tres resourceTemplatesListResult
	if err := c.call(ctx, "resources/templates/list", json.RawMessage(`{}`), &tres); err == nil {
		all = append(all, tres.ResourceTemplates...)
	}
	return all, nil
}

// Prompt is a reusable template a server offers.
//
// Prompts are the part of MCP that is not tools: a server saying "here is the
// wording that works for this" rather than "here is a function". A server
// that publishes a good one has encoded expertise that would otherwise have
// to be rediscovered by whoever writes the request.
type Prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
	Icons       json.RawMessage  `json:"icons,omitempty"`
	Meta        json.RawMessage  `json:"_meta,omitempty"`
}

// PromptArgument is one substitution a prompt takes.
type PromptArgument struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type promptsListResult struct {
	Prompts    []Prompt `json:"prompts"`
	NextCursor *string  `json:"nextCursor"`
}

// ListPrompts returns every prompt a server offers.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var all []Prompt
	var cursor *string
	for i := 0; i < defaults.ListPageLimit; i++ {
		var res promptsListResult
		if err := c.call(ctx, "prompts/list", pageParams(cursor), &res); err != nil {
			// A server without prompts answers method-not-found, which is an
			// absence rather than a failure. Treating it as an error would
			// make every listing fail on the majority of servers.
			return all, nil
		}
		all = append(all, res.Prompts...)
		next, more := c.nextPage(res.NextCursor, cursor)
		if !more {
			break
		}
		cursor = &next
	}
	return all, nil
}

// GetPrompt renders one prompt with its arguments filled in.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (json.RawMessage, error) {
	if args == nil {
		args = map[string]string{}
	}
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.call(ctx, "prompts/get", params, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// CallTool invokes a tool and returns the raw CallToolResult.
//
// On a modern HTTP connection the tool's x-mcp-header parameters are
// mirrored into Mcp-Param-* headers. A -32020 HeaderMismatch then most
// likely means the tool's schema changed since it was listed, so the list is
// read again and the call retried once -- the transport page's SHOULD.
func (c *Client) CallTool(ctx context.Context, name string, args any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	params, done, err := c.beginRelay(relayFrom(ctx), params)
	if err != nil {
		return nil, err
	}
	defer done()
	var raw json.RawMessage
	for attempt := 0; ; attempt++ {
		hctx, err := c.toolCallHeaders(ctx, name, args, attempt > 0)
		if err != nil {
			return nil, err
		}
		err = c.call(hctx, "tools/call", params, &raw)
		var re *rpcError
		if attempt == 0 && errors.As(err, &re) && re.Code == codeHeaderMismatch && c.mirrorsHeaders() {
			continue
		}
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
}

// ReadResource reads a resource URI.
//
// A missing resource is -32002 in the legacy revisions and -32602 in
// 2026-07-28, where clients SHOULD also accept -32002. Either is returned as
// a ResourceNotFoundError so a caller can tell "no such thing"
// from "the server failed".
func (c *Client) ReadResource(ctx context.Context, uri string) (json.RawMessage, error) {
	params, _ := json.Marshal(map[string]string{"uri": uri})
	var raw json.RawMessage
	if err := c.call(ctx, "resources/read", params, &raw); err != nil {
		var re *rpcError
		if errors.As(err, &re) && (re.Code == codeResourceNotFound || (re.Code == codeInvalidParams && c.Era == EraModern)) {
			return nil, &ResourceNotFoundError{URI: uri, err: re}
		}
		return nil, err
	}
	return raw, nil
}

const (
	codeResourceNotFound = -32002
	codeInvalidParams    = -32602
)

// ResourceNotFoundError is a resources/read of a URI the server does not
// have.
type ResourceNotFoundError struct {
	URI string
	err *rpcError
}

func (e *ResourceNotFoundError) Error() string {
	return fmt.Sprintf("resource not found: %s (%v)", e.URI, e.err)
}

func (e *ResourceNotFoundError) Unwrap() error { return e.err }

// InvalidResultError is a modern result whose resultType mcpx does not
// recognise.
type InvalidResultError struct {
	Method, ResultType string
}

func (e *InvalidResultError) Error() string {
	return fmt.Sprintf("%s: the server returned resultType %q, which mcpx does not recognise; the result is invalid",
		e.Method, e.ResultType)
}

// Close terminates the session.
func (c *Client) Close() error {
	// Through fail, so what is in flight is answered. Close used to mark the
	// client closed on its own, and fail -- the only thing that answers
	// pending requests -- then saw it closed and returned early: every call
	// waiting on a server being shut down sat until its own deadline.
	c.fail(errors.New("closed by client"))
	return c.t.Close()
}

// CallTimeout is a convenience wrapper applying a deadline.
func (c *Client) CallTimeout(parent context.Context, d time.Duration, name string, args any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	return c.CallTool(ctx, name, args)
}
