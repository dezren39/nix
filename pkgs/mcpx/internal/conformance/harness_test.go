package conformance_test

// The shared harness for requirement tests. Every area's tests use these
// pieces rather than inventing their own, so a frame is built, sent, read
// and validated the same way everywhere.
//
// mcpx as a server:
//   srv := newServer(t)                     // mcpserver.Server over a fake backend
//   sio := stdioServer(t, srv)              // ServeStdio over pipes
//   sio.initialize(t, rev)                  // legacy handshake (no-op for modern)
//   resp := sio.request(t, rev, "tools/list", nil) // validated strictly against rev
//   hs := httpServer(t, srv)                // Streamable HTTP via httptest
//   sess := hs.initialize(t, rev)
//   res := hs.request(t, rev, sess, "tools/list", nil)  // status, headers, frames
//
// mcpx as a client:
//   peer := newPeer(rev)                    // scripted server speaking rev
//   peer.on("tools/list", func(p map[string]any) (any, *rpcError) {...})
//   c := dialClient(t, peer, rev)           // mcpclient over the peer
//   peer.sent()                             // every frame the client sent, validated
//   hp := httpPeer(t, peer)                 // the same peer behind Streamable HTTP
//
// Every frame either side emits is validated with mcpspec (strict for
// mcpx's server frames) unless the test says otherwise.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/conformance"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/mcpspec"
)

const (
	rev20241105 = "2024-11-05"
	rev20250326 = "2025-03-26"
	rev20250618 = "2025-06-18"
	rev20251125 = "2025-11-25"
	rev20260728 = "2026-07-28"

	specURL = "https://modelcontextprotocol.io/specification/"

	// wait bounds every blocking read in the harness. A test that needs a
	// frame and gets none fails instead of hanging the suite.
	wait = 5 * time.Second
)

// allRevs, legacyRevs and httpRevs are the revisions a table-driven test
// loops over.
var (
	allRevs    = []string{rev20241105, rev20250326, rev20250618, rev20251125, rev20260728}
	legacyRevs = []string{rev20241105, rev20250326, rev20250618, rev20251125}
)

func isModern(rev string) bool { return rev >= rev20260728 }

// ---------------------------------------------------------------------------
// Frames

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// frame is a JSON-RPC message. A nil id makes a notification.
func frame(id any, method string, params any) []byte {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if pm, ok := params.(map[string]any); params != nil && (!ok || pm != nil) {
		m["params"] = params
	}
	b, _ := json.Marshal(m)
	return b
}

// params adds what rev requires on every request: nothing for legacy, the
// per-request _meta for 2026-07-28. caps is the client capabilities JSON
// declared on a modern request ("" = {}).
func params(rev string, extra map[string]any) map[string]any {
	return paramsWith(rev, "", extra)
}

func paramsWith(rev, caps string, extra map[string]any) map[string]any {
	p := map[string]any{}
	for k, v := range extra {
		p[k] = v
	}
	if !isModern(rev) {
		if len(p) == 0 {
			return nil
		}
		return p
	}
	if caps == "" {
		caps = "{}"
	}
	meta, _ := p["_meta"].(map[string]any)
	m := map[string]any{
		mcpserver.MetaProtocolVersion:    rev,
		mcpserver.MetaClientCapabilities: json.RawMessage(caps),
		"io.modelcontextprotocol/clientInfo": map[string]any{
			"name": "conformance", "version": "1",
		},
	}
	for k, v := range meta {
		m[k] = v
	}
	p["_meta"] = m
	return p
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, b)
	}
	return m
}

func resultOf(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	r, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", m)
	}
	return r
}

func errorCode(m map[string]any) int {
	e, _ := m["error"].(map[string]any)
	if e == nil {
		return 0
	}
	c, _ := e["code"].(float64)
	return int(c)
}

// checkServerFrame validates a frame mcpx-as-server sent, strictly: mcpx
// must not send a revision keys it does not define. method is the request
// a response answers.
func checkServerFrame(t *testing.T, rev string, b []byte, method string) {
	t.Helper()
	if err := mcpspec.ValidateServerMessageStrict(rev, b, method); err != nil {
		var ext *mcpspec.ErrExtension
		if asExt(err, &ext) {
			return
		}
		t.Errorf("%s frame for %s does not match the schema: %v\n%s", rev, method, err, b)
	}
}

// checkClientFrame validates a frame mcpx-as-client sent.
func checkClientFrame(t *testing.T, rev string, b []byte, method string) {
	t.Helper()
	if err := mcpspec.ValidateClientMessage(rev, b, method); err != nil {
		var ext *mcpspec.ErrExtension
		if asExt(err, &ext) {
			return
		}
		t.Errorf("%s client frame (%s) does not match the schema: %v\n%s", rev, method, err, b)
	}
}

func asExt(err error, target **mcpspec.ErrExtension) bool {
	e, ok := err.(*mcpspec.ErrExtension)
	if ok {
		*target = e
	}
	return ok
}

// ---------------------------------------------------------------------------
// mcpx as a server

// backend is a complete fake Backend: tools, resources, templates and
// prompts all non-empty, so every list has something in it.
type backend struct {
	mu    sync.Mutex
	calls map[string]int
	// block, when set, makes Call wait until the context ends or it closes.
	block   chan struct{}
	started chan struct{}
	// ended receives once per blocked call whose context ended: the
	// backend saw the cancellation.
	ended chan struct{}
	// completions is how many values Complete offers (0: three).
	completions int
}

func newBackend() *backend {
	return &backend{calls: map[string]int{}, started: make(chan struct{}, 16), ended: make(chan struct{}, 16)}
}

func (b *backend) hit(n string) { b.mu.Lock(); b.calls[n]++; b.mu.Unlock() }

func (b *backend) count(n string) int { b.mu.Lock(); defer b.mu.Unlock(); return b.calls[n] }

func (b *backend) Namespaces(context.Context) (string, error) {
	b.hit("namespaces")
	return "alpha", nil
}
func (b *backend) Catalog(context.Context, int, string) (string, error) {
	b.hit("catalog")
	return "catalog", nil
}
func (b *backend) Types(context.Context, []string) (string, error) {
	b.hit("types")
	return "types", nil
}
func (b *backend) Search(_ context.Context, q string, _ int) (string, error) {
	b.hit("search")
	return "search " + q, nil
}
func (b *backend) Call(ctx context.Context, ns, tool string, _ json.RawMessage) (string, error) {
	b.hit("call")
	if b.block != nil {
		b.started <- struct{}{}
		select {
		case <-ctx.Done():
			b.ended <- struct{}{}
			return "", ctx.Err()
		case <-b.block:
		}
	}
	return "called " + ns + "." + tool, nil
}
func (b *backend) Exec(_ context.Context, src string, _ int) (string, error) {
	b.hit("exec")
	return "ran " + src, nil
}
func (b *backend) Log(context.Context, string, string, string, int) (string, error) {
	return "records", nil
}
func (b *backend) Stats(context.Context, string) (string, error) { return "{}", nil }
func (b *backend) Status(context.Context) (string, error)        { b.hit("status"); return "{}", nil }
func (b *backend) RegistrySearch(context.Context, string, int) (string, error) {
	return "[]", nil
}
func (b *backend) Resources(context.Context) ([]mcpserver.ResourceRef, error) {
	return []mcpserver.ResourceRef{{URI: "mem://alpha/one", Name: "one", MimeType: "text/plain"}}, nil
}
func (b *backend) Prompts(context.Context) ([]mcpserver.PromptRef, error) {
	return []mcpserver.PromptRef{{Name: "greet", Description: "say hi",
		Arguments: []mcpserver.PromptArg{{Name: "who", Required: true}}}}, nil
}
func (b *backend) ReadResource(_ context.Context, uri string) ([]mcpserver.ResourceContents, error) {
	switch uri {
	case "mem://alpha/one":
		return []mcpserver.ResourceContents{{MimeType: "text/plain", Text: "hello"}}, nil
	case "mem://alpha/bin":
		return []mcpserver.ResourceContents{{MimeType: "image/png", Blob: "iVBORw0KGgo="}}, nil
	}
	return nil, fmt.Errorf("resource %s: %w", uri, mcpserver.ErrResourceNotFound)
}

// Complete offers completions values for the one prompt there is.
func (b *backend) Complete(_ context.Context, params json.RawMessage) ([]string, error) {
	var p struct {
		Ref struct {
			Name string `json:"name"`
		} `json:"ref"`
	}
	_ = json.Unmarshal(params, &p)
	if p.Ref.Name != "greet" {
		return nil, fmt.Errorf("%w: no prompt named %q", mcpserver.ErrInvalidParams, p.Ref.Name)
	}
	n := b.completions
	if n == 0 {
		n = 3
	}
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("v%03d", i)
	}
	return out, nil
}
func (b *backend) GetPrompt(_ context.Context, name string, args map[string]string) (string, error) {
	if name != "greet" {
		return "", fmt.Errorf("%w: no prompt named %q", mcpserver.ErrInvalidParams, name)
	}
	return "hi " + args["who"], nil
}
func (b *backend) ResourceTemplates(context.Context) ([]mcpserver.ResourceRef, error) {
	return []mcpserver.ResourceRef{{URI: "mem://alpha/{name}", Name: "by-name"}}, nil
}

// notifier pushes whatever the test sends on ch to every listener whose
// filter allows it.
type notifier struct{ ch chan [2]any }

func newNotifier() *notifier { return &notifier{ch: make(chan [2]any, 16)} }

// Listen filters the way the daemon's event bus does: only the kinds and
// resource URIs the server asked for.
func (n *notifier) Listen(ctx context.Context, f mcpserver.ListenFilter, send func(string, any)) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-n.ch:
			if n.allows(f, m[0].(string), m[1]) {
				send(m[0].(string), m[1])
			}
		}
	}
}

func (n *notifier) allows(f mcpserver.ListenFilter, method string, params any) bool {
	switch method {
	case "notifications/tools/list_changed":
		return f.ToolsListChanged
	case "notifications/prompts/list_changed":
		return f.PromptsListChanged
	case "notifications/resources/list_changed":
		return f.ResourcesListChanged
	case "notifications/resources/updated":
		uri, _ := asMap(params)["uri"].(string)
		for _, u := range f.ResourceSubscriptions {
			if u == uri {
				return true
			}
		}
	}
	return false
}

// blocking makes every backend call wait for the context to end, and
// releases them all at cleanup so no server goroutine outlives the test
// (an httptest server's Close waits for its handlers).
func (b *backend) blocking(t *testing.T) {
	b.block = make(chan struct{})
	t.Cleanup(func() { close(b.block) })
}

// newServer is mcpx's MCP server over a fake backend.
func newServer(t *testing.T) (*mcpserver.Server, *backend) {
	t.Helper()
	b := newBackend()
	return mcpserver.New(b, "mcpx", "test"), b
}

// stdioSrv drives ServeStdio over pipes.
type stdioSrv struct {
	in      *io.PipeWriter
	frames  chan []byte
	pending [][]byte
	done    chan error
	nextID  atomic.Int64
	methods map[string]string // request id -> method, for validation
	mu      sync.Mutex
}

func stdioServer(t *testing.T, s *mcpserver.Server) *stdioSrv {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ss := &stdioSrv{in: inW, frames: make(chan []byte, 256), done: make(chan error, 1), methods: map[string]string{}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err := s.ServeStdio(ctx, inR, outW)
		outW.Close()
		ss.done <- err
	}()
	go func() {
		defer close(ss.frames)
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			ss.frames <- append([]byte(nil), sc.Bytes()...)
		}
	}()
	t.Cleanup(func() {
		inW.Close()
		cancel()
		select {
		case <-ss.done:
		case <-time.After(wait):
			t.Error("ServeStdio did not return after stdin closed")
		}
	})
	return ss
}

// send writes one line.
func (ss *stdioSrv) send(t *testing.T, b []byte) {
	t.Helper()
	errc := make(chan error, 1)
	go func() { _, err := ss.in.Write(append(b, '\n')); errc <- err }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(wait):
		t.Fatal("the server stopped reading its input")
	}
}

// next is the next frame the server writes, raw.
func (ss *stdioSrv) next(t *testing.T) []byte {
	t.Helper()
	if len(ss.pending) > 0 {
		f := ss.pending[0]
		ss.pending = ss.pending[1:]
		return f
	}
	select {
	case f, ok := <-ss.frames:
		if !ok {
			t.Fatal("stdout closed")
		}
		return f
	case <-time.After(wait):
		t.Fatal("no frame from the server")
	}
	return nil
}

// quiet reports whether nothing arrives within d.
func (ss *stdioSrv) quiet(d time.Duration) ([]byte, bool) {
	if len(ss.pending) > 0 {
		return ss.pending[0], false
	}
	select {
	case f, ok := <-ss.frames:
		if !ok {
			return nil, true
		}
		ss.pending = append(ss.pending, f)
		return f, false
	case <-time.After(d):
		return nil, true
	}
}

// response reads frames until the response to id, keeping the rest (in
// order) for next. Every frame read is validated against rev.
func (ss *stdioSrv) response(t *testing.T, rev string, id any) map[string]any {
	t.Helper()
	want, _ := json.Marshal(id)
	var kept [][]byte
	defer func() { ss.pending = append(kept, ss.pending...) }()
	deadline := time.After(wait)
	for {
		var f []byte
		if len(ss.pending) > 0 {
			f, ss.pending = ss.pending[0], ss.pending[1:]
		} else {
			select {
			case g, ok := <-ss.frames:
				if !ok {
					t.Fatalf("stdout closed before the response to %s", want)
				}
				f = g
			case <-deadline:
				t.Fatalf("no response to %s", want)
			}
		}
		m := decode(t, f)
		if raw, ok := m["id"]; ok && m["method"] == nil {
			got, _ := json.Marshal(raw)
			if bytes.Equal(got, want) {
				ss.mu.Lock()
				method := ss.methods[string(want)]
				ss.mu.Unlock()
				checkServerFrame(t, rev, f, method)
				return m
			}
		}
		kept = append(kept, f)
	}
}

// initialize runs the legacy handshake for rev. For a modern revision it
// does nothing: there is no handshake.
func (ss *stdioSrv) initialize(t *testing.T, rev string) map[string]any {
	t.Helper()
	if isModern(rev) {
		return nil
	}
	r := ss.request(t, rev, "initialize", initParams(rev, nil))
	ss.send(t, frame(nil, "notifications/initialized", nil))
	return r
}

// request sends a request and returns its response, validated.
func (ss *stdioSrv) request(t *testing.T, rev, method string, p map[string]any) map[string]any {
	t.Helper()
	return ss.requestOpt(t, rev, method, p, true)
}

// requestInvalid sends a request the test knows is malformed; only the
// response is validated.
func (ss *stdioSrv) requestInvalid(t *testing.T, rev, method string, p map[string]any) map[string]any {
	t.Helper()
	return ss.requestOpt(t, rev, method, p, false)
}

func (ss *stdioSrv) requestOpt(t *testing.T, rev, method string, p map[string]any, check bool) map[string]any {
	t.Helper()
	id := ss.nextID.Add(1)
	ss.mu.Lock()
	ss.methods[fmt.Sprint(id)] = method
	ss.mu.Unlock()
	if method != "initialize" {
		p = params(rev, p)
	}
	b := frame(id, method, p)
	if check && len(methodsOf(rev, method)) == 1 {
		checkClientFrame(t, rev, b, "")
	}
	ss.send(t, b)
	return ss.response(t, rev, id)
}

func initParams(rev string, caps map[string]any) map[string]any {
	if caps == nil {
		caps = map[string]any{}
	}
	return map[string]any{
		"protocolVersion": rev,
		"capabilities":    caps,
		"clientInfo":      map[string]any{"name": "conformance", "version": "1"},
	}
}

// httpSrv is mcpx's Streamable HTTP endpoint on a real listener.
type httpSrv struct {
	ts     *httptest.Server
	nextID atomic.Int64
}

func httpServer(t *testing.T, s *mcpserver.Server) *httpSrv {
	t.Helper()
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return &httpSrv{ts: ts}
}

// httpResult is one POST's outcome: the status, headers, and the JSON-RPC
// frames in the body (one for application/json, each event's data for
// text/event-stream).
type httpResult struct {
	Status int
	Header http.Header
	Body   []byte
	Frames [][]byte
}

// post sends raw bytes with the given headers (Content-Type and Accept are
// filled in if absent).
func (h *httpSrv) post(t *testing.T, body []byte, hdr map[string]string) httpResult {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.ts.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return h.do(t, req)
}

func (h *httpSrv) do(t *testing.T, req *http.Request) httpResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	resp, err := h.ts.Client().Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	r := httpResult{Status: resp.StatusCode, Header: resp.Header, Body: b}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(string(b), "\n") {
			if d, ok := strings.CutPrefix(line, "data:"); ok && strings.TrimSpace(d) != "" {
				r.Frames = append(r.Frames, []byte(strings.TrimSpace(d)))
			}
		}
	} else if len(bytes.TrimSpace(b)) > 0 {
		r.Frames = [][]byte{b}
	}
	return r
}

// headersFor are the headers a conformant client of rev sends with a
// request of method (name is the tool/resource/prompt name for Mcp-Name).
func headersFor(rev, session, method, name string) map[string]string {
	h := map[string]string{}
	if rev >= rev20250618 {
		h["MCP-Protocol-Version"] = rev
	}
	if session != "" && !isModern(rev) {
		h["Mcp-Session-Id"] = session
	}
	if isModern(rev) {
		h["Mcp-Method"] = method
		if name != "" {
			h["Mcp-Name"] = name
		}
	}
	return h
}

// initialize runs the legacy handshake over HTTP and returns the session id
// (possibly empty). Modern: no-op.
func (h *httpSrv) initialize(t *testing.T, rev string) string {
	t.Helper()
	if isModern(rev) {
		return ""
	}
	r := h.post(t, frame(0, "initialize", initParams(rev, nil)), headersFor(rev20250326, "", "", ""))
	if r.Status != http.StatusOK || len(r.Frames) != 1 {
		t.Fatalf("initialize: %d %s", r.Status, r.Body)
	}
	checkServerFrame(t, rev, r.Frames[0], "initialize")
	sess := r.Header.Get("Mcp-Session-Id")
	n := h.post(t, frame(nil, "notifications/initialized", nil), headersFor(rev, sess, "notifications/initialized", ""))
	if n.Status != http.StatusAccepted {
		t.Fatalf("initialized: %d %s", n.Status, n.Body)
	}
	return sess
}

// request POSTs one request with the headers rev requires and validates
// every frame that comes back.
func (h *httpSrv) request(t *testing.T, rev, session, method string, p map[string]any) (httpResult, map[string]any) {
	t.Helper()
	name, _ := p["name"].(string)
	if u, ok := p["uri"].(string); ok {
		name = u
	}
	id := h.nextID.Add(1)
	b := frame(id, method, params(rev, p))
	r := h.post(t, b, headersFor(rev, session, method, name))
	var resp map[string]any
	for _, f := range r.Frames {
		m := decode(t, f)
		if m["method"] == nil {
			checkServerFrame(t, rev, f, method)
			resp = m
		} else {
			checkServerFrame(t, rev, f, "")
		}
	}
	return r, resp
}

// ---------------------------------------------------------------------------
// mcpx as a client

// handler answers one request from the client: a result, or an error.
type handler func(p map[string]any) (any, *rpcError)

// peer is a scripted MCP server of one revision, as an mcpclient.Transport.
// Defaults answer the handshake for rev (initialize for legacy,
// server/discover for modern -- and refuse the other era's opener the way a
// real server of rev does), ping, and the list methods.
type peer struct {
	rev  string
	caps map[string]any

	mu       sync.Mutex
	handlers map[string]handler
	frames   [][]byte
	in       chan []byte
	closed   bool
	// replies are the client's responses to server-initiated requests, by id.
	replies map[string]chan map[string]any
	nextID  int
	t       *testing.T
	// validate, when false, skips schema checks of what the client sent
	// (for tests that feed the client malformed input and expect a
	// malformed-looking reaction).
	validate bool
}

func newPeer(t *testing.T, rev string) *peer {
	p := &peer{rev: rev, t: t, caps: map[string]any{"tools": map[string]any{}, "resources": map[string]any{},
		"prompts": map[string]any{}}, handlers: map[string]handler{}, in: make(chan []byte, 256),
		replies: map[string]chan map[string]any{}, validate: true}
	p.on("ping", func(map[string]any) (any, *rpcError) { return map[string]any{}, nil })
	p.on("tools/list", func(map[string]any) (any, *rpcError) {
		return map[string]any{"tools": []any{map[string]any{"name": "echo",
			"inputSchema": map[string]any{"type": "object"}}}}, nil
	})
	p.on("resources/list", func(map[string]any) (any, *rpcError) { return map[string]any{"resources": []any{}}, nil })
	p.on("prompts/list", func(map[string]any) (any, *rpcError) { return map[string]any{"prompts": []any{}}, nil })
	p.on("tools/call", func(map[string]any) (any, *rpcError) {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}, nil
	})
	if isModern(rev) {
		p.on("server/discover", func(map[string]any) (any, *rpcError) {
			return map[string]any{"resultType": "complete", "supportedVersions": []string{rev},
				"capabilities": p.caps, "_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": "peer", "version": "1"}}}, nil
		})
		p.on("initialize", func(map[string]any) (any, *rpcError) {
			return nil, &rpcError{Code: -32022, Message: "unsupported protocol version",
				Data: map[string]any{"supported": []string{rev}}}
		})
	} else {
		p.on("initialize", func(map[string]any) (any, *rpcError) {
			return map[string]any{"protocolVersion": rev, "capabilities": p.caps,
				"serverInfo": map[string]any{"name": "peer", "version": "1"}}, nil
		})
		p.on("server/discover", func(map[string]any) (any, *rpcError) {
			return nil, &rpcError{Code: -32601, Message: "method not found"}
		})
	}
	return p
}

// on sets (or replaces) the answer to a method.
func (p *peer) on(method string, h handler) {
	p.mu.Lock()
	p.handlers[method] = h
	p.mu.Unlock()
}

func (p *peer) Send(_ context.Context, msg []byte) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return io.ErrClosedPipe
	}
	p.frames = append(p.frames, append([]byte(nil), msg...))
	p.mu.Unlock()
	var f map[string]any
	if err := json.Unmarshal(msg, &f); err != nil {
		p.t.Errorf("client sent a frame that is not JSON: %s", msg)
		return nil
	}
	method, _ := f["method"].(string)
	id, hasID := f["id"]
	if method == "" && hasID {
		// A response to a request this peer sent.
		key, _ := json.Marshal(id)
		p.mu.Lock()
		ch := p.replies[string(key)]
		p.mu.Unlock()
		if ch != nil {
			ch <- f
		}
		return nil
	}
	// A vendor method is an extension, not a schema violation.
	if p.validate && len(methodsOf(p.rev, method)) == 1 {
		checkClientFrame(p.t, p.rev, msg, "")
	}
	if !hasID {
		return nil
	}
	p.mu.Lock()
	h := p.handlers[method]
	p.mu.Unlock()
	reply := map[string]any{"jsonrpc": "2.0", "id": id}
	if h == nil {
		reply["error"] = rpcError{Code: -32601, Message: "method not found: " + method}
	} else if res, e := h(asMap(f["params"])); e != nil {
		reply["error"] = e
	} else if res == nil {
		return nil // silence: the test wants no answer
	} else {
		reply["result"] = res
	}
	b, _ := json.Marshal(reply)
	p.push(b)
	return nil
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func (p *peer) push(b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.in <- b
	}
}

func (p *peer) Recv() ([]byte, error) {
	b, ok := <-p.in
	if !ok {
		return nil, io.EOF
	}
	return b, nil
}

func (p *peer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.in)
	}
	return nil
}

func (p *peer) Info() string { return "peer:" + p.rev }

// notify sends the client a notification.
func (p *peer) notify(method string, params any) { p.push(frame(nil, method, params)) }

// ask sends the client a request and returns its response (validated
// against rev as a response to method).
func (p *peer) ask(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	p.mu.Lock()
	p.nextID++
	// Numeric: string ids are a requirement of their own (messages), and
	// the harness should not make every question depend on it.
	id := 1000 + p.nextID
	ch := make(chan map[string]any, 1)
	key, _ := json.Marshal(id)
	p.replies[string(key)] = ch
	p.mu.Unlock()
	p.push(frame(id, method, params))
	select {
	case f := <-ch:
		b, _ := json.Marshal(f)
		if p.validate {
			checkClientFrame(t, p.rev, b, method)
		}
		return f
	case <-time.After(wait):
		t.Fatalf("the client did not answer %s", method)
	}
	return nil
}

// sent is every frame the client sent, decoded, in order.
func (p *peer) sent() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]map[string]any, 0, len(p.frames))
	for _, b := range p.frames {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		out = append(out, m)
	}
	return out
}

// sentMethod is the frames with the given method.
func (p *peer) sentMethod(method string) []map[string]any {
	var out []map[string]any
	for _, f := range p.sent() {
		if f["method"] == method {
			out = append(out, f)
		}
	}
	return out
}

// dialClient connects mcpclient to the peer, forcing the peer's era so the
// test sees the revision it asked for rather than a probe.
func dialClient(t *testing.T, p *peer, o mcpclient.Options) *mcpclient.Client {
	t.Helper()
	c, err := dialClientErr(t, p, o)
	if err != nil {
		t.Fatalf("connect to a %s peer: %v", p.rev, err)
	}
	return c
}

func dialClientErr(t *testing.T, p *peer, o mcpclient.Options) (*mcpclient.Client, error) {
	t.Helper()
	if o.ClientName == "" {
		o.ClientName, o.ClientVersion = "mcpx", "test"
	}
	if o.Preference == "" {
		o.Preference = mcpclient.ForceLegacy
		if isModern(p.rev) {
			o.Preference = mcpclient.ForceModern
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	c, err := mcpclient.NewWithOptions(ctx, p, o)
	if c != nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, err
}

// httpPeerSrv puts a peer behind Streamable HTTP and records every request
// the client made, headers included.
type httpPeerSrv struct {
	*httptest.Server
	peer *peer
	mu   sync.Mutex
	reqs []recorded
	// session, when set, is issued at initialize and required afterwards.
	session string
	// sse answers requests as a text/event-stream (with a keep-alive
	// comment first) instead of application/json.
	sse    bool
	serial sync.Mutex
}

type recorded struct {
	Method string // HTTP method
	Header http.Header
	Body   []byte
}

func httpPeer(t *testing.T, p *peer) *httpPeerSrv {
	t.Helper()
	hp := &httpPeerSrv{peer: p}
	hp.Server = httptest.NewServer(http.HandlerFunc(hp.serve))
	t.Cleanup(hp.Close)
	return hp
}

func (hp *httpPeerSrv) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	hp.mu.Lock()
	hp.reqs = append(hp.reqs, recorded{r.Method, r.Header.Clone(), b})
	hp.mu.Unlock()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var f map[string]any
	_ = json.Unmarshal(b, &f)
	if _, ok := f["id"]; !ok || f["method"] == nil {
		_ = hp.peer.Send(r.Context(), b)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	// One exchange at a time: the peer has a single reply channel, and two
	// concurrent POSTs must not take each other's answers.
	hp.serial.Lock()
	defer hp.serial.Unlock()
	_ = hp.peer.Send(r.Context(), b)
	var reply []byte
	select {
	case reply = <-hp.peer.in:
	case <-time.After(wait):
		w.WriteHeader(http.StatusGatewayTimeout)
		return
	}
	if f["method"] == "initialize" && hp.session != "" {
		w.Header().Set("Mcp-Session-Id", hp.session)
	}
	if hp.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, ": keep-alive\n\nevent: message\ndata: %s\n\n", reply)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK
	if m := decode(hp.peer.t, reply); errorCode(m) != 0 && isModern(hp.peer.rev) {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(reply)
}

// requests is every HTTP request the client made.
func (hp *httpPeerSrv) requests() []recorded {
	hp.mu.Lock()
	defer hp.mu.Unlock()
	return append([]recorded(nil), hp.reqs...)
}

// dialHTTPClient connects mcpclient to the peer over Streamable HTTP.
func dialHTTPClient(t *testing.T, hp *httpPeerSrv, o mcpclient.Options) (*mcpclient.Client, error) {
	t.Helper()
	tr, err := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: hp.URL})
	if err != nil {
		t.Fatal(err)
	}
	if o.ClientName == "" {
		o.ClientName, o.ClientVersion = "mcpx", "test"
	}
	if o.Preference == "" {
		o.Preference = mcpclient.ForceLegacy
		if isModern(hp.peer.rev) {
			o.Preference = mcpclient.ForceModern
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	c, err := mcpclient.NewWithOptions(ctx, tr, o)
	if c != nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, err
}

func ctxT(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	t.Cleanup(cancel)
	return ctx
}

// clientOpts is the zero Options with a name; tests set what they need.
func clientOpts() mcpclient.Options {
	return mcpclient.Options{ClientName: "mcpx", ClientVersion: "test"}
}

// methodsOf keeps the methods rev defines (2026-07-28 removed ping, for one).
func methodsOf(rev string, methods ...string) []string {
	s, err := mcpspec.Get(rev)
	if err != nil {
		return methods
	}
	var out []string
	for _, m := range methods {
		if _, ok := s.MethodDef(m); ok {
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Requirements

var (
	catOnce sync.Once
	catByID map[string]conformance.Requirement
	catErr  error
)

func requirement(t *testing.T, id string) conformance.Requirement {
	t.Helper()
	catOnce.Do(func() {
		root, err := conformance.ModuleRoot(".")
		if err != nil {
			catErr = err
			return
		}
		reqs, err := conformance.Load(filepath.Join(root, conformance.CataloguePath))
		catErr = err
		catByID = map[string]conformance.Requirement{}
		for _, r := range reqs {
			catByID[r.ID] = r
		}
	})
	if catErr != nil {
		t.Fatal(catErr)
	}
	r, ok := catByID[id]
	if !ok {
		t.Fatalf("no requirement %q in the catalogue", id)
	}
	return r
}

// forReq runs fn once per revision the requirement has, as the subtest
// "<rev>/<area>/<id>" -- the name conformance.SReq/CReq cover. Taking the
// revisions from the catalogue is what makes the cover honest: the test
// cannot claim a revision it does not run. Where a gap is recorded for the
// cell, the subtest skips with "gap: <issue>" instead of failing.
func forReq(t *testing.T, side, id string, fn func(t *testing.T, rev string)) {
	t.Helper()
	r := requirement(t, id)
	for _, rev := range r.Revs {
		t.Run(rev+"/"+conformance.AreaGroup(r.Area)+"/"+id, func(t *testing.T) {
			// MCPX_CONFORMANCE_RUN_GAPS=1 runs the gapped cells too, to see
			// which gaps a change has closed.
			if g := conformance.GapAt(id, side, rev); g != nil && os.Getenv("MCPX_CONFORMANCE_RUN_GAPS") != "1" {
				t.Skip("gap: " + g.Issue + " " + g.Why)
			}
			fn(t, rev)
		})
	}
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// asker is an Asker whose call raises the given questions until each is
// answered, then finishes with text.
type asker struct {
	mu        sync.Mutex
	questions []mcpserver.Question
	answers   map[string]json.RawMessage
	text      string
	began     int
	abandoned int
}

func newAsker(text string, qs ...mcpserver.Question) *asker {
	return &asker{questions: qs, answers: map[string]json.RawMessage{}, text: text}
}

func (a *asker) Begin(context.Context, string, json.RawMessage) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.began++
	return fmt.Sprintf("call-%d", a.began), nil
}

func (a *asker) Poll(ctx context.Context, _ string, wait time.Duration) (mcpserver.Outcome, error) {
	a.mu.Lock()
	var open []mcpserver.Question
	for _, q := range a.questions {
		if _, ok := a.answers[q.ID]; !ok {
			open = append(open, q)
		}
	}
	a.mu.Unlock()
	if len(open) == 0 {
		return mcpserver.Outcome{Done: true, Text: a.text}, nil
	}
	return mcpserver.Outcome{Questions: open}, nil
}

func (a *asker) Reply(_ context.Context, _ string, answers map[string]json.RawMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range answers {
		a.answers[k] = v
	}
	return nil
}

func (a *asker) Abandon(string) { a.mu.Lock(); a.abandoned++; a.mu.Unlock() }

// elicitQ is a form elicitation an upstream server raises.
func elicitQ(id string) mcpserver.Question {
	return mcpserver.Question{ID: id, Method: "elicitation/create", Mode: "form", Server: "up",
		Params: json.RawMessage(`{"message":"which repo?","requestedSchema":{"type":"object","properties":{"repo":{"type":"string"}}}}`)}
}

// callThatAsks is the tools/call params that go through the Asker.
func callThatAsks() map[string]any {
	return map[string]any{"name": "mcpx_call", "arguments": map[string]any{"namespace": "up", "tool": "t"}}
}

// sampleQ is a sampling request an upstream server raises.
func sampleQ(id string) mcpserver.Question {
	return mcpserver.Question{ID: id, Method: "sampling/createMessage", Server: "up",
		Params: json.RawMessage(`{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}],"maxTokens":10}`)}
}

// stream is an open HTTP response read line by line as it arrives.
type stream struct {
	Status int
	Header http.Header
	lines  chan string
	cancel context.CancelFunc
}

// openStream sends req and returns as soon as the headers arrive; the body
// is read in the background. The stream is closed at cleanup.
func openStream(t *testing.T, client *http.Client, req *http.Request) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	st := &stream{Status: resp.StatusCode, Header: resp.Header, lines: make(chan string, 256), cancel: cancel}
	go func() {
		defer close(st.lines)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
		for sc.Scan() {
			st.lines <- sc.Text()
		}
	}()
	t.Cleanup(cancel)
	return st
}

// until reads lines until one satisfies ok, failing after wait.
func (st *stream) until(t *testing.T, ok func(string) bool) (seen []string) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case l, open := <-st.lines:
			if !open {
				t.Fatalf("stream ended; saw %q", seen)
			}
			seen = append(seen, l)
			if ok(l) {
				return seen
			}
		case <-deadline:
			t.Fatalf("not seen; saw %q", seen)
		}
	}
}

// rest drains lines until the stream ends or d passes.
func (st *stream) rest(d time.Duration) (lines []string, ended bool) {
	deadline := time.After(d)
	for {
		select {
		case l, open := <-st.lines:
			if !open {
				return lines, true
			}
			lines = append(lines, l)
		case <-deadline:
			return lines, false
		}
	}
}

// dataFrames picks the JSON of each "data:" line.
func dataFrames(t *testing.T, lines []string) []map[string]any {
	var out []map[string]any
	for _, l := range lines {
		if d, ok := strings.CutPrefix(l, "data:"); ok {
			out = append(out, decode(t, []byte(strings.TrimSpace(d))))
		}
	}
	return out
}

// postReq builds a POST to the server with rev's headers.
func (h *httpSrv) postReq(t *testing.T, body []byte, hdr map[string]string) *http.Request {
	req, err := http.NewRequest(http.MethodPost, h.ts.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return req
}
