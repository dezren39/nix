package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/tasks"
)

// Conn is one client's connection.
//
// Everything here used to sit on Server, which was correct while the only
// transport was stdio -- one process, one client, one connection. Over HTTP
// a single Server answers every client, so a subscription filter, a push
// function and a set of declared capabilities kept there belonged to
// whichever client spoke last. That is not a style preference: with
// server-initiated requests it decides *whose* client gets asked a question.
type Conn struct {
	s *Server

	// caps is what a legacy client declared at initialize. A modern request
	// carries its own capabilities in _meta and never consults this, because
	// the specification says a server MUST NOT infer them from an earlier
	// request.
	caps map[string]json.RawMessage
	// version is what the initialize handshake settled on. Empty until a
	// legacy client has shaken hands, and empty forever for a modern one.
	version string
	// id is the Mcp-Session-Id this connection is reachable by, for the
	// transports that need to correlate a later POST with this connection.
	// It is also who owns a legacy task started here: empty for a
	// connection that does not outlive one request.
	id string

	// send writes one frame to this client, or is nil when the transport
	// cannot carry an unsolicited frame.
	send func(frame any) error
	// pushFn sends a notification. Separate from send because a transport
	// may be able to push and not to ask: a notification needs no reply, a
	// request needs a route back for one.
	pushFn func(method string, params any)

	mu        sync.Mutex
	pending   map[int64]chan *clientReply
	nextID    atomic.Int64
	subs      map[string]bool
	subMu     sync.Mutex
	listens   map[string]*listenStream
	cancelled map[string]string
	lastUsed  time.Time

	// inflight are the requests this connection is still answering, keyed
	// by canonical id, so notifications/cancelled can reach the context the
	// work runs under rather than only being written down.
	inflight map[string]*inflightReq
	// legacy marks a Streamable HTTP session minted by initialize. A modern
	// request presenting its id is not bound to it: that revision has no
	// sessions, and a legacy client's pending questions are not its business.
	legacy bool
	// process marks the connection that is this process's own client --
	// stdio's -- whose identity is the process itself. See Identity.
	process bool
	// stream is that GET stream while one is open.
	stream *eventStream
	// ended is closed when the session is terminated.
	ended     chan struct{}
	endedOnce sync.Once
	// listChanged forwards list_changed to a legacy client, which declared
	// nothing to opt in with: the capability mcpx declared is the promise.
	listChanged context.CancelFunc
	// logLevel is what a legacy client set with logging/setLevel: the least
	// severe upstream log message relayed to it during a call. Empty until
	// it asks, and nothing is relayed until then.
	logLevel string
}

// clientReply is a JSON-RPC response from the client to a request we sent it.
type clientReply struct {
	Result json.RawMessage
	Error  *rpcError
}

func (s *Server) newConn(id string, send func(any) error) *Conn {
	c := &Conn{s: s, id: id, send: send,
		pending: map[int64]chan *clientReply{}, lastUsed: time.Now(),
		ended: make(chan struct{})}
	if send != nil {
		c.pushFn = func(method string, params any) {
			_ = send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
		}
	}
	return c
}

// push sends a notification, if this transport can carry one.
func (c *Conn) push(method string, params any) {
	c.mu.Lock()
	fn := c.pushFn
	c.mu.Unlock()
	if fn != nil {
		fn(method, params)
	}
}

// canPush reports whether anything can reach this connection's client.
func (c *Conn) canPush() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pushFn != nil || c.send != nil
}

// Detached is a connection with no client behind it.
//
// Used by the in-process paths -- a test, the REST projection of a tool --
// where there is a request to answer and nothing to push back to.
func (s *Server) Detached() *Conn { return s.newConn("", nil) }

// SetCapabilities records what a legacy client declared.
func (c *Conn) SetCapabilities(caps map[string]json.RawMessage, version string) {
	c.mu.Lock()
	c.caps, c.version = caps, version
	c.mu.Unlock()
}

// Version is the revision this connection settled on, for a legacy client.
func (c *Conn) Version() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

// ---- what the client said it can do ----

// Peer is what mcpx may send one client, on one request.
//
// Built per request rather than per connection because the two eras disagree
// about where the answer comes from: legacy settles it once at initialize,
// modern restates it on every request and forbids a server from remembering
// the last one. Collapsing them into one value here is what lets everything
// downstream ask "may I send this?" without knowing which era it is in.
type Peer struct {
	// Version is the revision governing this request.
	Version string
	// Modern reports whether it is a per-request-metadata revision.
	Modern bool
	// Caps is what the client declared, in the modern shape.
	Caps map[string]json.RawMessage
}

// peerFor resolves the capabilities governing one request.
func (c *Conn) peerFor(params json.RawMessage) Peer {
	if v := requestVersion(params); v != "" {
		return Peer{Version: v, Modern: Modern(v), Caps: requestCapabilities(params)}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.version
	if v == "" {
		// No handshake and no _meta. The client told us nothing, so the
		// safest reading is the oldest revision mcpx serves: every shape it
		// defines is understood by everything newer.
		v = Oldest
	}
	return Peer{Version: v, Caps: c.caps}
}

// requestCapabilities reads the clientCapabilities a modern request carries.
func requestCapabilities(params json.RawMessage) map[string]json.RawMessage {
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	raw, ok := p.Meta[MetaClientCapabilities]
	if !ok {
		return nil
	}
	var caps map[string]json.RawMessage
	if json.Unmarshal(raw, &caps) != nil {
		return nil
	}
	return caps
}

// The reserved _meta keys a 2026-07-28 request carries. Repeated here rather
// than imported from mcpclient so the two halves of mcpx do not depend on
// each other; a test asserts they agree.
const (
	MetaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaClientInfo         = "io.modelcontextprotocol/clientInfo"
)

// DeclaredExtension reports whether the client declared an extension in its
// capabilities -- which only a 2026-07-28 client can do, because no earlier
// ClientCapabilities has an extensions field. The tasks SEP says so
// directly: under 2025-11-25 the extension must be treated as undeclared.
func (p Peer) DeclaredExtension(name string) bool {
	if !Defines(p.Version, FeatExtensions) {
		return false
	}
	var ext map[string]json.RawMessage
	if json.Unmarshal(p.Caps["extensions"], &ext) != nil {
		return false
	}
	_, ok := ext[name]
	return ok
}

// Declared reports whether the client declared a capability.
func (p Peer) Declared(name string) bool {
	_, ok := p.Caps[name]
	return ok
}

// CanElicit reports whether mcpx may send this client an elicitation in the
// given mode.
//
// Three gates, and all three are needed. The revision has to define
// elicitation at all -- 2025-03-26 does not. The client has to have declared
// it. And url mode has to be separately declared, because it arrived in
// 2025-11-25 and a client that knows only form mode receives a request with
// no `requestedSchema` and no way to render it.
func (p Peer) CanElicit(mode string) bool {
	if !AtLeast(p.Version, "2025-06-18") || !p.Declared("elicitation") {
		return false
	}
	if mode != "url" {
		return true
	}
	if !AtLeast(p.Version, "2025-11-25") {
		return false
	}
	var e struct {
		URL json.RawMessage `json:"url"`
	}
	if json.Unmarshal(p.Caps["elicitation"], &e) != nil {
		return false
	}
	return len(e.URL) > 0
}

// CanSample reports whether mcpx may ask this client for a model completion.
func (p Peer) CanSample() bool { return p.Declared("sampling") }

// AnswersInline reports whether this client can answer a question raised
// mid-call, by either mechanism.
func (p Peer) AnswersInline() bool { return p.CanElicit("form") || p.CanSample() }

// ---- server-initiated requests, the legacy mechanism ----

// ErrNoPush means this connection cannot carry a request to its client.
var ErrNoPush = errors.New("this transport cannot send the client a request")

// Request sends the client a request and waits for its answer.
//
// This is the legacy mechanism: a genuine mid-flight request, sent while the
// client's own call is still open. A modern client is never sent one --
// there is no connection to send it on -- and gets an input_required result
// instead.
func (c *Conn) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	send := senderFrom(ctx)
	if send == nil {
		c.mu.Lock()
		send = c.send
		c.mu.Unlock()
	}
	if send == nil {
		return nil, ErrNoPush
	}

	id := c.nextID.Add(1)
	// Negative, so a server-initiated id can never collide with a client's
	// own. JSON-RPC only requires uniqueness within a direction, but a
	// client that keys one table by id -- and several do -- would otherwise
	// see our request answer its own.
	wire := -id
	ch := make(chan *clientReply, 1)
	c.mu.Lock()
	c.pending[wire] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, wire)
		c.mu.Unlock()
	}()

	if err := send(map[string]any{
		"jsonrpc": "2.0", "id": wire, "method": method, "params": params,
	}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		// Tell the client to stop rather than leaving it rendering a dialog
		// for an answer nobody will read.
		_ = send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled",
			"params": map[string]any{"requestId": wire, "reason": "timed out"}})
		return nil, ctx.Err()
	case reply := <-ch:
		if reply.Error != nil {
			return nil, reply.Error
		}
		return reply.Result, nil
	}
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message)
}

// deliver routes a JSON-RPC response the client sent us.
//
// Returns false when nothing was waiting for it, which is how a caller tells
// a reply to our request from a frame that only looks like one.
func (c *Conn) deliver(id int64, result json.RawMessage, rerr *rpcError) bool {
	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if !ok {
		return false
	}
	ch <- &clientReply{Result: result, Error: rerr}
	return true
}

// isReply reports whether a frame is a response rather than a request: an id
// and no method. The client's own recvLoop had this bug in reverse, and it
// cost a release of silent elicitation failures.
func isReply(req request) bool {
	return len(req.ID) > 0 && req.Method == ""
}

// replyOf reads the result or error from a response frame.
func replyOf(raw []byte) (int64, json.RawMessage, *rpcError, bool) {
	var f struct {
		ID     *int64          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(raw, &f) != nil || f.ID == nil || f.Method != "" {
		return 0, nil, nil, false
	}
	if f.Result == nil && f.Error == nil {
		return 0, nil, nil, false
	}
	return *f.ID, f.Result, f.Error, true
}

// ---- asking this connection's client ----

// sender is how a request in flight reaches this client, when the transport
// binds that to the exchange rather than to the connection.
//
// Streamable HTTP is why: the frame has to go out on the response stream of
// the request that is waiting for it, and a connection serving several
// requests at once has several of those.
type senderKey struct{}

func withSender(ctx context.Context, fn func(any) error) context.Context {
	return context.WithValue(ctx, senderKey{}, fn)
}

func senderFrom(ctx context.Context) func(any) error {
	fn, _ := ctx.Value(senderKey{}).(func(any) error)
	return fn
}

// askClient puts one question to the client over the wire and converts the
// answer into the shape the daemon's broker records.
func (c *Conn) askClient(ctx context.Context, p Peer, q Question) (json.RawMessage, error) {
	params, err := q.paramsFor(p)
	if err != nil {
		return nil, err
	}
	if taskID := tasks.IDFrom(ctx); taskID != "" && !p.Modern {
		// A 2025-11-25 task that needs its requestor's input shows it:
		// input_required while the question is out, working again once it
		// is answered, and the question itself names the task it belongs
		// to. The status is a SHOULD and the metadata a MUST, and neither
		// happened -- the task sat at working while its client was being
		// asked something in its name.
		// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks#input-required-status
		if params, err = withRelatedTask(params, taskID); err != nil {
			return nil, err
		}
		st := c.s.tasks()
		st.SetStatus(taskID, tasks.InputRequired, "waiting on "+q.Method)
		defer st.SetStatus(taskID, tasks.Working, "")
	}
	ctx, cancel := context.WithTimeout(ctx, defaults.ElicitHandlerTimeout)
	defer cancel()
	return c.Request(ctx, q.Method, params)
}

// MetaRelatedTask ties a message to the 2025-11-25 task it serves.
const MetaRelatedTask = "io.modelcontextprotocol/related-task"

func withRelatedTask(params json.RawMessage, taskID string) (json.RawMessage, error) {
	m := map[string]any{}
	if err := json.Unmarshal(params, &m); err != nil {
		return nil, err
	}
	meta, _ := m["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta[MetaRelatedTask] = map[string]any{"taskId": taskID}
	m["_meta"] = meta
	return json.Marshal(m)
}

// Question is one thing a server asked mid-call.
type Question struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	// Mode distinguishes form from url, which decides whether a given
	// client may be sent it at all.
	Mode string `json:"mode,omitempty"`
	// Server is which upstream server is asking. A client has to be told,
	// because "are you sure?" means different things from different servers.
	Server string `json:"server,omitempty"`
	// Key is the upstream's own name for the question, its inputRequests
	// key, when it asked in a 2026-07-28 result. Relayed as the key a modern
	// client sees, so a server's "user_name" is not renamed on the way.
	Key string `json:"key,omitempty"`
	// Round is how many questions the upstream asked in this result, when it
	// asked in a 2026-07-28 input_required. A relayed round is complete once
	// that many are open; see viaAsk.
	Round int `json:"round,omitempty"`
}

// Sendable reports whether this client may be asked this question.
func (q Question) Sendable(p Peer) bool {
	switch q.Method {
	case "elicitation/create":
		return p.CanElicit(q.Mode)
	case "sampling/createMessage":
		return p.CanSample()
	case "roots/list":
		return p.Declared("roots")
	}
	return false
}

// paramsFor renders a question into the shape the client's revision defines.
//
// Downgrading matters here more than anywhere: a 2025-06-18 client that
// receives `mode` has been sent a field its schema does not have, and a
// strict one rejects the whole request over it.
func (q Question) paramsFor(p Peer) (json.RawMessage, error) {
	m := map[string]any{}
	if len(q.Params) > 0 {
		if err := json.Unmarshal(q.Params, &m); err != nil {
			return nil, err
		}
	}
	if q.Method == "elicitation/create" {
		if msg, _ := m["message"].(string); q.Server != "" {
			// Name the originator. The client is being asked something by a
			// server three layers down, and "are you sure?" with no idea who
			// is asking is not a question anybody can answer.
			if !strings.Contains(msg, q.Server) {
				m["message"] = q.Server + " (via mcpx) asks: " + msg
			}
		}
		switch {
		case !AtLeast(p.Version, "2025-11-25"):
			// Neither field exists before 2025-11-25, and url mode itself
			// does not; a url question never reaches here, because Sendable
			// refused it.
			delete(m, "mode")
			delete(m, "elicitationId")
		case p.Modern:
			// 2026-07-28 dropped elicitationId along with the
			// notifications/elicitation/complete it correlated: a url
			// question is complete when the client retries.
			delete(m, "elicitationId")
		default:
			// 2025-11-25 makes elicitationId REQUIRED on a url-mode
			// request. An upstream server of another revision may not have
			// sent one, and relaying the question without it is sending a
			// request the client's schema rejects. The question's own id is
			// unique per call, which is what the field needs to be.
			if mode, _ := m["mode"].(string); mode == "url" {
				if id, _ := m["elicitationId"].(string); id == "" {
					m["elicitationId"] = "mcpx-" + q.ID
				}
			}
		}
	}
	return json.Marshal(m)
}
