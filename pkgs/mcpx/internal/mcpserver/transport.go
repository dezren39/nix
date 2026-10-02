package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/mcpheaders"
)

// The transport rules of both eras, kept out of server.go so that the wire
// and the protocol can change independently.
//
// Spec pages this file implements:
//   https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http
//   https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio
//   https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
//   https://modelcontextprotocol.io/specification/2025-03-26/basic (batching)

// codeHeaderMismatch is 2026-07-28's HeaderMismatchError: the HTTP headers
// that mirror the body are missing, malformed, or disagree with it.
const codeHeaderMismatch = -32020

// ---- cancellation ----

// inflightReq is one request a connection is still answering.
type inflightReq struct {
	cancel context.CancelFunc
}

// idKey is a request id in a form a notifications/cancelled requestId can be
// compared with. Both sides go through a decode and re-encode, so 7 and 7.0
// agree and "7" and 7 do not -- JSON-RPC treats a string id and a number id
// as different ids.
func idKey(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// track registers a request so a cancellation can reach its context.
//
// The returned cancelled reports whether the work was cancelled -- by the
// client, or by the transport shutting down -- which is when its response
// must not be sent: 2026-07-28 says MUST NOT send further messages for a
// cancelled request, the legacy revisions SHOULD NOT send a response.
func (c *Conn) track(parent context.Context, id json.RawMessage) (ctx context.Context, done func(), cancelled func() bool) {
	ctx, cancel := context.WithCancel(parent)
	key := idKey(id)
	r := &inflightReq{cancel: cancel}
	c.mu.Lock()
	if c.inflight == nil {
		c.inflight = map[string]*inflightReq{}
	}
	c.inflight[key] = r
	c.mu.Unlock()
	done = func() {
		c.mu.Lock()
		if c.inflight[key] == r {
			delete(c.inflight, key)
		}
		c.mu.Unlock()
		cancel()
	}
	return ctx, done, func() bool { return ctx.Err() != nil }
}

// cancelInflight cancels the request a notifications/cancelled names. An id
// nothing is running under is ignored, as the specification says it may be:
// the request may simply have finished first.
func (c *Conn) cancelInflight(requestID any) {
	b, err := json.Marshal(requestID)
	if err != nil {
		return
	}
	key := idKey(b)
	c.mu.Lock()
	r := c.inflight[key]
	c.mu.Unlock()
	if r != nil {
		r.cancel()
	}
}

// ---- batching ----

// isBatch reports whether a frame is a JSON-RPC batch.
func isBatch(body []byte) bool {
	t := bytes.TrimLeft(body, " \t\r\n")
	return len(t) > 0 && t[0] == '['
}

// batchRefused is the answer to a batch the governing revision does not
// define. 2025-03-26 requires a server to accept batches; 2025-06-18 removed
// them, and the modern revisions define a POST body and a stdio line as one
// message. An id cannot be named because there are several.
func batchRefused(version string) *response {
	return &response{JSONRPC: "2.0", Error: &rpcError{Code: codeInvalidRequest,
		Message: "JSON-RPC batches exist only in 2025-03-26, not in protocol version " + version +
			", so send one message at a time"}}
}

// runBatch answers every element of a 2025-03-26 batch.
//
// Elements run concurrently, because JSON-RPC does not order a batch's
// replies and one element may be a request that has to wait for the client
// to answer a question another element carries. Responses in the batch are
// delivered; notifications are handled and get nothing back; a batch that
// held only those produces no reply at all.
func (s *Server) runBatch(ctx context.Context, c *Conn, elems []json.RawMessage, tracked bool) []*response {
	out := make([]*response, len(elems))
	var wg sync.WaitGroup
	for i, raw := range elems {
		if id, result, rerr, ok := replyOf(raw); ok {
			c.deliver(id, result, rerr)
			continue
		}
		var req request
		if err := json.Unmarshal(raw, &req); err != nil {
			out[i] = &response{JSONRPC: "2.0",
				Error: &rpcError{Code: codeInvalidRequest, Message: err.Error()}}
			continue
		}
		if req.Method == "initialize" {
			// 2025-03-26 lifecycle: "The initialization phase MUST be the
			// first interaction ... the initialize request MUST NOT be part
			// of a JSON-RPC batch."
			out[i] = &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{
				Code: codeInvalidRequest, Message: "initialize must not be sent in a batch"}}
			continue
		}
		if requestVersion(req.Params) != "" {
			out[i] = &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{
				Code: codeInvalidRequest, Message: "a per-request-metadata message cannot be batched"}}
			continue
		}
		wg.Add(1)
		go func(i int, req request) {
			defer wg.Done()
			if len(req.ID) == 0 {
				_ = s.HandleOn(ctx, c, req)
				return
			}
			rctx, done, cancelled := ctx, func() {}, func() bool { return false }
			if tracked {
				rctx, done, cancelled = c.track(ctx, req.ID)
			}
			resp := s.HandleOn(rctx, c, req)
			if !cancelled() {
				out[i] = resp
			}
			done()
		}(i, req)
	}
	wg.Wait()
	var replies []*response
	for _, r := range out {
		if r != nil {
			replies = append(replies, r)
		}
	}
	return replies
}

// ---- Origin ----

// OriginPolicy decides which browser origins may reach the HTTP transport.
//
// Every Streamable HTTP revision says a server MUST validate Origin, and
// 2025-11-25 and 2026-07-28 say an invalid one gets 403. Without it any web
// page can reach a server on loopback through DNS rebinding and run tools as
// the user. A request with no Origin is not from a browser page and is not
// refused: every CLI and SDK client sends none.
type OriginPolicy struct {
	// Hosts are hostnames allowed at any port, over http or https.
	// Nil means the loopback names in defaults.
	Hosts []string
	// Origins are further origins allowed exactly (scheme://host[:port]).
	Origins []string
}

func (s *Server) originAllowed(origin string) bool { return s.Origins.Allows(origin) }

// Allows reports whether a request carrying this Origin may be served.
// Exported so the daemon's /v1 routes, which share its listeners, apply the
// same rule rather than a second copy of it.
func (p OriginPolicy) Allows(origin string) bool {
	if origin == "" {
		return true
	}
	hosts := p.Hosts
	if hosts == nil {
		hosts = defaults.TransportLoopbackHosts
	}
	trimmed := strings.TrimSuffix(origin, "/")
	for _, o := range p.Origins {
		if strings.EqualFold(strings.TrimSuffix(o, "/"), trimmed) {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		// Includes the literal "null" a sandboxed or file: page sends,
		// which names nobody and so cannot be allowed by name.
		return false
	}
	h := u.Hostname()
	for _, allowed := range hosts {
		if strings.EqualFold(strings.Trim(allowed, "[]"), h) {
			return true
		}
	}
	return false
}

// ---- modern request headers ----

// nameOf is the body value Mcp-Name mirrors, and whether the method has one.
func nameOf(req request) (string, bool) {
	var p struct {
		Name   string `json:"name"`
		URI    string `json:"uri"`
		TaskID string `json:"taskId"`
	}
	_ = json.Unmarshal(req.Params, &p)
	switch req.Method {
	case "tools/call", "prompts/get":
		return p.Name, true
	case "resources/read":
		return p.URI, true
	case "tasks/get", "tasks/update", "tasks/cancel":
		// The tasks extension's routing header (SEP-2663): Mcp-Name is
		// the taskId, so a load balancer can route a poll to the node
		// holding the task.
		return p.TaskID, true
	}
	return "", false
}

// nameRequired reports whether a request of this method must carry
// Mcp-Name. SEP-2243 requires it for the three methods it names. The tasks
// extension puts the same obligation on its clients, but a client built to
// SEP-2243 alone does not know it; a missing header is tolerated there, and
// only one that contradicts the body is refused.
func nameRequired(method string) bool {
	switch method {
	case "tools/call", "prompts/get", "resources/read":
		return true
	}
	return false
}

func headerMismatch(id json.RawMessage, format string, a ...any) *response {
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{
		Code: codeHeaderMismatch, Message: "Header mismatch: " + fmt.Sprintf(format, a...)}}
}

// missingMeta is the answer to a modern request that left out a _meta field
// the revision requires. Invalid params rather than header mismatch: the
// field is absent from the body, not contradicted by a header.
func missingMeta(id json.RawMessage, key string) *response {
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{
		Code: codeInvalidParams, Message: "Invalid params: _meta is missing the required " + key}}
}

// checkModernHeaders validates the headers a 2026-07-28 POST mirrors from
// its body. nil means they are present and agree.
//
// The rule exists because a gateway routes on the header and the server acts
// on the body; if they can disagree, what was authorised and what ran are
// different requests.
func checkModernHeaders(r *http.Request, req request) *response {
	isRequest := len(req.ID) > 0
	for _, h := range []string{"MCP-Protocol-Version", "Mcp-Method", "Mcp-Name"} {
		if v := r.Header.Get(h); v != "" && !mcpheaders.Safe(v) {
			return headerMismatch(req.ID, "%s contains characters a header may not carry", h)
		}
	}
	hv := r.Header.Get("MCP-Protocol-Version")
	bv := requestVersion(req.Params)
	switch {
	case hv == "":
		return headerMismatch(req.ID, "MCP-Protocol-Version header is required")
	case bv == "":
		// A missing body field is not a header mismatch. The headers are
		// mirrors of the body, and -32020 says the mirror disagrees with what
		// it reflects; here there is nothing to reflect. basic/index makes
		// this case invalid params: "A request missing any required field is
		// malformed; the server MUST reject it with JSON-RPC error code
		// -32602 (Invalid params)." modernStatus already maps -32602 to 400,
		// and its own comment already called this the missing-_meta case --
		// so the two halves of the rule disagreed, and the half a client
		// reads was the wrong one.
		return missingMeta(req.ID, MetaProtocolVersion)
	case hv != bv:
		return headerMismatch(req.ID, "MCP-Protocol-Version header value '%s' does not "+
			"match body value '%s'", hv, bv)
	}
	// Notification POSTs have no header requirements in 2026-07-28; what a
	// client did send must still agree.
	m := r.Header.Get("Mcp-Method")
	if m == "" && isRequest {
		return headerMismatch(req.ID, "Mcp-Method header is required")
	}
	if m != "" && m != req.Method {
		return headerMismatch(req.ID, "Mcp-Method header value '%s' does not match body value '%s'",
			m, req.Method)
	}
	body, named := nameOf(req)
	if !named {
		return nil
	}
	raw := r.Header.Get("Mcp-Name")
	if raw == "" {
		if isRequest && nameRequired(req.Method) {
			return headerMismatch(req.ID, "Mcp-Name header is required for %s", req.Method)
		}
		return nil
	}
	got, err := mcpheaders.Decode(raw)
	if err != nil {
		return headerMismatch(req.ID, "Mcp-Name header is not valid Base64: %v", err)
	}
	if got != body {
		return headerMismatch(req.ID, "Mcp-Name header value '%s' does not match body value '%s'",
			got, body)
	}
	return nil
}

// checkParamHeaders validates the Mcp-Param-* headers of a 2026-07-28
// tools/call against the x-mcp-header annotations of the tool it names. It
// shares mcpheaders with the client side, which is what sends these headers
// upstream, so a value mcpx would send is a value mcpx accepts.
//
// A tool mcpx cannot find, or whose annotations are invalid, is left to the
// dispatcher: there is nothing to check a header against.
func (s *Server) checkParamHeaders(r *http.Request, req request) *response {
	if req.Method != "tools/call" {
		return nil
	}
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(req.Params, &p) != nil || p.Name == "" {
		return nil
	}
	all, err := s.surface(s.withPass(r.Context()))
	if err != nil {
		return nil
	}
	for _, t := range all {
		if t.Name != p.Name {
			continue
		}
		params, err := mcpheaders.ToolParams(t.InputSchema)
		if err != nil {
			return nil
		}
		err = mcpheaders.Check(params, p.Arguments, func(name string) (string, bool) {
			v := r.Header.Values(name)
			if len(v) == 0 {
				return "", false
			}
			// Repeated headers join, as HTTP combines them, and so cannot match.
			return strings.Join(v, ","), true
		})
		if err != nil {
			return headerMismatch(req.ID, "%v", err)
		}
		return nil
	}
	return nil
}

// modernStatus is the HTTP status for a 2026-07-28 response. The one place
// the mapping lives, so every error the dispatcher produces gets the status
// the specification ties to its code:
//
//   - -32022 UnsupportedProtocolVersion, -32021 MissingRequiredClientCapability,
//     -32020 HeaderMismatch and -32602 (which is what a request missing a
//     required _meta field gets) are 400;
//   - -32601 is 404, and the JSON-RPC body is what tells a dual-era client
//     that 404 from a legacy server with no such endpoint;
//   - a malformed message (-32700, -32600) is 400;
//   - everything else is 200 with the JSON-RPC body.
//
// Every -32602 is 400, not only the missing-_meta case: invalid params is a
// client error whatever the parameter, and a status that depended on which
// handler produced it would not be a function of the response.
func modernStatus(resp *response) int {
	if resp == nil || resp.Error == nil {
		return http.StatusOK
	}
	switch resp.Error.Code {
	case codeUnsupportedVersion, codeMissingCapability, codeHeaderMismatch,
		codeInvalidParams, codeParse, codeInvalidRequest:
		return http.StatusBadRequest
	case codeMethodNotFound:
		return http.StatusNotFound
	}
	return http.StatusOK
}

// modernHeaderVersion reports whether an MCP-Protocol-Version header names a
// per-request-metadata revision -- including one mcpx does not implement,
// which is still a modern request to be answered with -32022.
func modernHeaderVersion(h string) bool {
	if len(h) != len("2006-01-02") {
		return false
	}
	if _, err := time.Parse("2006-01-02", h); err != nil {
		return false
	}
	return Modern(h)
}

// ---- the legacy GET stream ----

// eventStream is a legacy session's GET stream: where a 2025-03-26 ..
// 2025-11-25 client receives what no request of its own is waiting for.
type eventStream struct {
	mu sync.Mutex
	w  http.ResponseWriter
	f  http.Flusher
	// closed is set when the handler returns; the ResponseWriter is dead
	// after that, and a notifier holding this stream must not touch it.
	closed bool
}

func (e *eventStream) write(frame any) error {
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return http.ErrHandlerTimeout
	}
	if _, err := fmt.Fprintf(e.w, "data: %s\n\n", b); err != nil {
		return err
	}
	e.f.Flush()
	return nil
}

// comment is an SSE comment line: no event, but traffic.
func (e *eventStream) comment() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := fmt.Fprint(e.w, ":\n\n"); err != nil {
		return err
	}
	e.f.Flush()
	return nil
}

// streamPush sends a notification on the session's GET stream, if one is
// open. Without one it is dropped: the legacy transport lets a client choose
// not to listen, and mcpx keeps no redelivery buffer.
func (c *Conn) streamPush(method string, params any) {
	c.mu.Lock()
	st := c.stream
	c.mu.Unlock()
	if st != nil {
		_ = st.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}
}

// serveGET opens a legacy session's notification stream.
func (s *Server) serveGET(w http.ResponseWriter, r *http.Request) {
	notAllowed := func(why string) {
		w.Header().Set("Allow", "POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, response{JSONRPC: "2.0",
			Error: &rpcError{Code: codeInvalidRequest, Message: why}})
	}
	if modernHeaderVersion(r.Header.Get("MCP-Protocol-Version")) {
		notAllowed("2026-07-28 has no GET stream; open one with subscriptions/listen")
		return
	}
	id := r.Header.Get(sessionHeader)
	if id == "" {
		// Nothing to deliver to a client mcpx cannot identify.
		notAllowed("a GET stream belongs to a session; send initialize first")
		return
	}
	c, ok := s.session(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{JSONRPC: "2.0",
			Error: &rpcError{Code: codeInvalidRequest, Message: "no such session"}})
		return
	}
	f := asFlusher(w)
	if f == nil {
		notAllowed("this listener cannot stream")
		return
	}
	st := &eventStream{w: w, f: f}
	c.mu.Lock()
	if c.stream != nil {
		c.mu.Unlock()
		// One per session. The specification allows several but forbids
		// broadcasting a message across them, and a second stream a client
		// opened by accident would silently take half its notifications.
		writeJSON(w, http.StatusConflict, response{JSONRPC: "2.0",
			Error: &rpcError{Code: codeInvalidRequest, Message: "this session already has a GET stream open"}})
		return
	}
	c.stream = st
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.stream == st {
			c.stream = nil
		}
		c.mu.Unlock()
		s.stopListChanged(c)
		st.mu.Lock()
		st.closed = true
		st.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	s.startListChanged(c)

	tick := time.NewTicker(s.Timing.resolved().SSEKeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.ended:
			return
		case <-tick.C:
			if st.comment() != nil {
				return
			}
		}
	}
}

// startListChanged forwards list_changed to a legacy client.
//
// A legacy client has no subscriptions/listen to opt in with: declaring
// listChanged at initialize is the promise, and a notification stream the
// client can receive -- stdio, or an open GET stream -- is the delivery.
func (s *Server) startListChanged(c *Conn) {
	if s.Notify == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	if c.listChanged != nil {
		c.listChanged()
	}
	c.listChanged = cancel
	c.mu.Unlock()
	go s.Notify.Listen(ctx, ListenFilter{ToolsListChanged: s.toolsVary(),
		PromptsListChanged: true, ResourcesListChanged: true}, c.push)
}

func (s *Server) stopListChanged(c *Conn) {
	c.mu.Lock()
	if c.listChanged != nil {
		c.listChanged()
		c.listChanged = nil
	}
	c.mu.Unlock()
}

// end terminates a session: its GET stream closes and nothing more is sent.
func (c *Conn) end() {
	c.endedOnce.Do(func() { close(c.ended) })
	c.stopListen()
	c.s.stopListChanged(c)
}
