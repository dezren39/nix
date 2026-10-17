package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HTTPTransport speaks MCP Streamable HTTP. Each Send performs a POST; the
// reply is either a single JSON object or an SSE stream, and every JSON-RPC
// message found is queued for Recv.
//
// This covers remote MCP servers directly, which removes the need for an
// mcp-remote node shim in front of them.
type HTTPTransport struct {
	url     string
	headers map[string]string
	hc      *http.Client

	sessionMu sync.RWMutex
	sessionID string
	// negotiated is the legacy version initialize settled on; it is what
	// MCP-Protocol-Version carries on every later legacy request.
	negotiated string
	// getStream is set once the standalone GET stream has been started.
	getStream bool

	incoming chan inbound
	errOnce  sync.Once
	errCh    chan error
	err      error

	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup
}

// HTTPOptions configure a remote MCP server.
type HTTPOptions struct {
	URL     string
	Headers map[string]string
	Timeout time.Duration
}

// NewHTTP creates a Streamable HTTP transport.
func NewHTTP(opts HTTPOptions) (*HTTPTransport, error) {
	if opts.URL == "" {
		return nil, errors.New("http transport: empty url")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaults.HTTPRequestTimeout
	}
	return &HTTPTransport{
		url:      opts.URL,
		headers:  opts.Headers,
		hc:       &http.Client{Timeout: timeout},
		incoming: make(chan inbound, 64),
		errCh:    make(chan error, 1),
		closed:   make(chan struct{}),
	}, nil
}

func (t *HTTPTransport) setHeaders(req *http.Request) {
	t.setHeadersFor(req, nil)
}

// setNegotiated records the version a legacy initialize settled on.
func (t *HTTPTransport) setNegotiated(v string) {
	t.sessionMu.Lock()
	t.negotiated = v
	t.sessionMu.Unlock()
}

// headerVersionFloor is the first revision with an MCP-Protocol-Version
// header. A server that negotiated 2025-03-26 or earlier never defined it,
// and a client sends only what was negotiated.
const headerVersionFloor = "2025-06-18"

// setHeadersFor sets headers for one outgoing frame.
//
// The version header follows the frame. A modern frame carries its version
// in _meta and the header MUST match it. A legacy frame carries the version
// initialize negotiated -- not mcpx's own newest -- because a server that
// negotiated down reads the header as the version in use and rejects one it
// never agreed to. Before initialize has answered there is nothing to send.
//
// Modern frames also carry Mcp-Method and Mcp-Name, and never
// Mcp-Session-Id: 2026-07-28 has no sessions, and a session id minted for an
// earlier legacy exchange means nothing to a modern request.
func (t *HTTPTransport) setHeadersFor(req *http.Request, msg []byte) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	t.sessionMu.RLock()
	sid, negotiated := t.sessionID, t.negotiated
	t.sessionMu.RUnlock()
	if v := frameVersion(msg); v != "" {
		req.Header.Set("MCP-Protocol-Version", v)
		for k, hv := range standardHeaders(msg) {
			req.Header.Set(k, hv)
		}
	} else {
		if negotiated >= headerVersionFloor {
			req.Header.Set("MCP-Protocol-Version", negotiated)
		}
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	extraHeaders(req.Context(), req)
}

// Send POSTs a frame and dispatches the reply asynchronously.
func (t *HTTPTransport) Send(ctx context.Context, msg []byte) error {
	select {
	case <-t.closed:
		return errors.New("http transport closed")
	default:
	}

	// The caller's context governs the request.
	//
	// Send is synchronous, so a server that accepts the connection and never
	// answers blocks here -- before Call reaches the select that watches for
	// a timeout. Detaching the request from the context therefore did not
	// make cancellation best-effort, it removed it: pool.callTimeout could
	// not interrupt a hung server at all.
	//
	// Protocol-level cancellation is unaffected. The notifications/cancelled
	// message is sent on its own background context precisely so that it
	// outlives the request it is cancelling.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	t.setHeadersFor(req, msg)

	resp, err := t.hc.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", t.url, err)
	}

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionMu.Lock()
		t.sessionID = sid
		t.sessionMu.Unlock()
	}

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		resp.Body.Close()
		return nil // notification acknowledged, no body
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.HTTPErrorBodyLimit))
		resp.Body.Close()
		he := &HTTPStatusError{URL: t.url, Status: resp.StatusCode, Body: bytes.TrimSpace(b)}
		he.rpc, he.id = parseRPCError(he.Body)
		// A modern server answers a bad request with 400 and a JSON-RPC
		// error naming the request. That is the reply, not a transport
		// failure, and the caller waiting on that id should receive it as
		// one -- which is how an UnsupportedProtocolVersionError reaches the
		// code that knows to retry.
		if he.rpc != nil && he.id != nil && sameID(he.id, msg) {
			t.push(he.Body)
			return nil
		}
		return he
	}

	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		reqID := frameID(msg)
		modern := frameVersion(msg) != ""
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			st := t.readSSE(ctx, resp.Body, reqID)
			resp.Body.Close()
			if reqID == nil || st.answered || ctx.Err() != nil {
				return
			}
			if modern {
				// 2026-07-28 has no resumption: "a broken response stream
				// loses the in-flight request". The caller hears so now,
				// and may re-issue it as a new request.
				t.lost(reqID)
				return
			}
			t.resume(ctx, reqID, st)
		}()
	default:
		// Read value by value rather than to EOF. A body that carries more
		// than one message -- newline-delimited JSON, which the official
		// conformance suite's reference server streams on
		// subscriptions/listen -- was read until the server closed it,
		// which for a subscription is never: the acknowledgement and every
		// list_changed after it sat unread, and the stream's caller hung.
		head := &firstValue{}
		dec := json.NewDecoder(io.TeeReader(resp.Body, head))
		var first json.RawMessage
		if err := dec.Decode(&first); err != nil {
			// Empty, or not JSON: handed on whole as before, for push to
			// judge.
			rest, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return readErr
			}
			b := append(head.buf.Bytes(), rest...)
			if len(bytes.TrimSpace(b)) > 0 {
				t.push(b)
			}
			return nil
		}
		head.done = true
		t.push(first)
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			defer resp.Body.Close()
			for {
				var next json.RawMessage
				if dec.Decode(&next) != nil {
					return
				}
				t.push(next)
			}
		}()
	}
	return nil
}

// firstValue keeps the bytes read while decoding a body's first JSON value,
// so a body that turns out not to be JSON can still be handed on whole, and
// stops keeping them once that value is decoded.
type firstValue struct {
	buf  bytes.Buffer
	done bool
}

func (f *firstValue) Write(p []byte) (int, error) {
	if !f.done {
		f.buf.Write(p)
	}
	return len(p), nil
}

// sseState is what one SSE stream said about itself: the last event id,
// for resuming it, the retry interval the server asked for, and whether the
// response the stream was opened for arrived on it.
type sseState struct {
	lastID   string
	retry    time.Duration
	answered bool
}

// readSSE delivers every message on a stream. reqID, when set, is the
// request whose response the stream is expected to carry.
//
// origin is the context of the request whose POST opened the stream, or nil
// for the standalone GET stream. A server request on a POST's stream is
// related to that request (2025-11-25 transports), and RecvContext hands
// origin on with it so the answer can be attributed to the call that
// provoked it rather than inferred from whoever else shares the session.
func (t *HTTPTransport) readSSE(origin context.Context, r io.Reader, reqID json.RawMessage) sseState {
	var st sseState
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var data strings.Builder
	flush := func() {
		if data.Len() == 0 {
			return
		}
		payload := data.String()
		data.Reset()
		if strings.TrimSpace(payload) == "" {
			return // a priming event: an id and an empty data field
		}
		if reqID != nil && answers([]byte(payload), reqID) {
			st.answered = true
		}
		t.pushFrom(origin, []byte(payload))
	}
	for sc.Scan() {
		line := sc.Text()
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"):
			// comment / keepalive
		case field == "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		case field == "id":
			// An id containing NUL is ignored, per the SSE specification.
			if !strings.ContainsRune(value, 0) {
				st.lastID = value
			}
		case field == "retry":
			if ms, err := strconv.Atoi(value); err == nil && ms >= 0 {
				st.retry = time.Duration(ms) * time.Millisecond
			}
		}
	}
	flush()
	return st
}

// resume reconnects a legacy stream the server closed before the response
// it was opened for.
//
// 2025-11-25 lets a server close a POST's stream early -- to shed a
// connection it holds for a slow call -- and expects the client to come back
// with GET and Last-Event-ID after the retry interval, where the rest of the
// stream, response included, is replayed. A stream that carried no event id
// cannot be resumed; the request is then lost, and the caller is told so at
// once rather than at its deadline.
//
// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#resumability-and-redelivery
func (t *HTTPTransport) resume(ctx context.Context, reqID json.RawMessage, st sseState) {
	for attempt := 0; attempt < defaults.UpstreamSSEReconnectAttempts; attempt++ {
		if st.lastID == "" {
			break
		}
		wait := st.retry
		if wait <= 0 {
			wait = defaults.UpstreamSSEReconnectDelay
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		case <-t.closed:
			return
		}
		body, err := t.get(ctx, st.lastID)
		if err != nil {
			break
		}
		next := t.readSSE(ctx, body, reqID)
		body.Close()
		if next.answered {
			return
		}
		if next.lastID == "" {
			next.lastID = st.lastID
		}
		if next.retry <= 0 {
			next.retry = st.retry
		}
		st = next
	}
	if ctx.Err() != nil {
		return
	}
	t.lost(reqID)
}

// lost answers a request whose response stream ended without its response.
func (t *HTTPTransport) lost(reqID json.RawMessage) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": reqID, "error": map[string]any{
		"code": codeStreamLost, "message": "the server closed the response stream before answering",
	}})
	t.push(b)
}

// codeStreamLost is the JSON-RPC error mcpx reports for a request whose
// response stream closed first. -32000 is the start of the range JSON-RPC
// reserves for implementation-defined server errors.
const codeStreamLost = -32000

// get opens a GET stream, resuming after lastID when it is set.
func (t *HTTPTransport) get(ctx context.Context, lastID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return nil, err
	}
	t.setHeadersFor(req, nil)
	req.Header.Del("Content-Type")
	req.Header.Set("Accept", "text/event-stream")
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	// Not t.hc: its timeout bounds a whole exchange, and a stream is open
	// for as long as the server keeps it.
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		resp.Body.Close()
		return nil, &HTTPStatusError{URL: t.url, Status: resp.StatusCode}
	}
	return resp.Body, nil
}

var streamClient = &http.Client{}

// listen opens the standalone GET stream a legacy Streamable HTTP server
// uses for messages that belong to no request: list changes, resource
// updates, and requests of its own. A server that has none answers 405 and
// that is the end of it. Without this stream, a server that asks a question
// outside a request's own stream -- which the TypeScript SDK does for a
// server.request() made inside a tool handler -- asks into silence, and the
// tool call waits until it times out.
//
// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#listening-for-messages-from-the-server
func (t *HTTPTransport) listen() {
	t.sessionMu.Lock()
	if t.getStream {
		t.sessionMu.Unlock()
		return
	}
	t.getStream = true
	t.sessionMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-t.closed
		cancel()
	}()
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		var st sseState
		failures := 0
		for ctx.Err() == nil {
			body, err := t.get(ctx, st.lastID)
			if err != nil {
				var he *HTTPStatusError
				if errors.As(err, &he) || failures >= defaults.UpstreamSSEReconnectAttempts {
					return // 405, or anything else that says "no stream here"
				}
				failures++
			} else {
				failures = 0
				next := t.readSSE(nil, body, nil)
				body.Close()
				if next.lastID != "" {
					st.lastID = next.lastID
				}
				if next.retry > 0 {
					st.retry = next.retry
				}
			}
			wait := st.retry
			if wait <= 0 {
				wait = defaults.UpstreamSSEReconnectDelay
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
			}
		}
	}()
}

// frameID is the id of a request frame, or nil for anything else.
func frameID(msg []byte) json.RawMessage {
	var f struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(msg, &f) != nil || f.Method == "" || len(f.ID) == 0 || string(f.ID) == "null" {
		return nil
	}
	return f.ID
}

// answers reports whether a payload (object or batch) contains the response
// to id.
func answers(payload []byte, id json.RawMessage) bool {
	var one struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []json.RawMessage
		if json.Unmarshal(trimmed, &batch) != nil {
			return false
		}
		for _, m := range batch {
			if answers(m, id) {
				return true
			}
		}
		return false
	}
	return json.Unmarshal(trimmed, &one) == nil && one.Method == "" &&
		bytes.Equal(bytes.TrimSpace(one.ID), bytes.TrimSpace(id))
}

// inbound is one received frame and the context of the request it arrived
// in answer to, when there is one.
type inbound struct {
	b      []byte
	origin context.Context
}

func (t *HTTPTransport) push(b []byte) { t.pushFrom(nil, b) }

func (t *HTTPTransport) pushFrom(origin context.Context, b []byte) {
	// A frame may be a single object or a batch array.
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(trimmed, &batch); err == nil {
			for _, m := range batch {
				t.deliver(origin, m)
			}
			return
		}
	}
	t.deliver(origin, trimmed)
}

func (t *HTTPTransport) deliver(origin context.Context, b []byte) {
	select {
	case t.incoming <- inbound{b: b, origin: origin}:
	case <-t.closed:
	}
}

// Recv returns the next queued frame.
func (t *HTTPTransport) Recv() ([]byte, error) {
	b, _, err := t.RecvContext()
	return b, err
}

// RecvContext is Recv, plus the context of the request on whose response
// stream the frame arrived (nil when none).
func (t *HTTPTransport) RecvContext() ([]byte, context.Context, error) {
	select {
	case in := <-t.incoming:
		return in.b, in.origin, nil
	case err := <-t.errCh:
		return nil, nil, err
	case <-t.closed:
		if t.err != nil {
			return nil, nil, t.err
		}
		return nil, nil, io.EOF
	}
}

// Close releases the session.
func (t *HTTPTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closed)
		t.sessionMu.RLock()
		sid := t.sessionID
		t.sessionMu.RUnlock()
		if sid != "" {
			ctx, cancel := context.WithTimeout(context.Background(), defaults.UpstreamSessionDeleteTimeout)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil)
			if err == nil {
				t.setHeaders(req)
				if resp, err := t.hc.Do(req); err == nil {
					resp.Body.Close()
				}
			}
		}
	})
	return nil
}

// HTTPHandoff is an upstream session as a successor process resumes it.
type HTTPHandoff struct {
	SessionID  string `json:"sessionId"`
	Negotiated string `json:"negotiated,omitempty"`
}

// Handoff reports the session this transport holds, if the upstream issued one.
func (t *HTTPTransport) Handoff() HTTPHandoff {
	t.sessionMu.RLock()
	defer t.sessionMu.RUnlock()
	return HTTPHandoff{SessionID: t.sessionID, Negotiated: t.negotiated}
}

// Resume continues a session a predecessor process established. Nothing is
// sent until the next request.
func (t *HTTPTransport) Resume(h HTTPHandoff) {
	t.sessionMu.Lock()
	t.sessionID, t.negotiated = h.SessionID, h.Negotiated
	t.sessionMu.Unlock()
}

// Abandon stops the transport without ending the upstream session, for a
// session a successor process now holds.
func (t *HTTPTransport) Abandon() {
	t.closeOnce.Do(func() { close(t.closed) })
	t.hc.CloseIdleConnections()
}

// Info describes the endpoint.
func (t *HTTPTransport) Info() string { return t.url }

// frameVersion reads the per-request protocol version from a frame's _meta,
// or "" for a legacy frame that carries none.
func frameVersion(msg []byte) string {
	if len(msg) == 0 {
		return ""
	}
	var f struct {
		Params struct {
			Meta map[string]any `json:"_meta"`
		} `json:"params"`
	}
	if json.Unmarshal(msg, &f) != nil {
		return ""
	}
	v, _ := f.Params.Meta[MetaProtocolVersion].(string)
	return v
}

// HTTPStatusError is a POST answered with a status outside 2xx.
//
// Structured rather than a string because the status and the body are what
// the era probe decides on: a 4xx with a recognised modern JSON-RPC error is
// a modern server, a 4xx with anything else is a legacy one, and a failure
// to connect is neither.
type HTTPStatusError struct {
	URL    string
	Status int
	Body   []byte
	// rpc is the JSON-RPC error in Body, if Body is one; id is its id.
	rpc *rpcError
	id  json.RawMessage
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("post %s: http %d: %s", e.URL, e.Status, string(e.Body))
}

// RPCCode returns the JSON-RPC error code carried in the body, if any.
func (e *HTTPStatusError) RPCCode() (int, bool) {
	if e.rpc == nil {
		return 0, false
	}
	return e.rpc.Code, true
}

// parseRPCError reads a JSON-RPC error response out of a body.
func parseRPCError(body []byte) (*rpcError, json.RawMessage) {
	var r struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *rpcError       `json:"error"`
	}
	if json.Unmarshal(body, &r) != nil || r.JSONRPC != "2.0" || r.Error == nil {
		return nil, nil
	}
	if len(r.ID) == 0 || string(r.ID) == "null" {
		return r.Error, nil
	}
	return r.Error, r.ID
}

// sameID reports whether a response id matches the id of the frame sent.
func sameID(id json.RawMessage, msg []byte) bool {
	var f struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(msg, &f) != nil || len(f.ID) == 0 {
		return false
	}
	return bytes.Equal(bytes.TrimSpace(f.ID), bytes.TrimSpace(id))
}
