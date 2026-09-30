package mcpclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// seen is one POST as the server received it.
type seen struct {
	method string
	id     json.RawMessage
	header http.Header
	body   map[string]any
}

type recorder struct {
	mu    sync.Mutex
	posts []seen
	gets  []http.Header
}

func (r *recorder) add(s seen) {
	r.mu.Lock()
	r.posts = append(r.posts, s)
	r.mu.Unlock()
}

func (r *recorder) all(method string) []seen {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []seen
	for _, p := range r.posts {
		if p.method == method {
			out = append(out, p)
		}
	}
	return out
}

func decodePost(r *http.Request) seen {
	b, _ := io.ReadAll(r.Body)
	var f map[string]any
	_ = json.Unmarshal(b, &f)
	var raw struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(b, &raw)
	m, _ := f["method"].(string)
	return seen{method: m, id: raw.ID, header: r.Header.Clone(), body: f}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func result(id json.RawMessage, v any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": v}
}

// modernHTTP is a 2026-07-28 Streamable HTTP server. tools is its current
// tools/list; mismatchOnce makes the first tools/call fail with -32020.
func modernHTTP(t *testing.T, rec *recorder, tools func() []any, call func(s seen) (int, any)) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s := decodePost(r)
		rec.add(s)
		if len(s.id) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		switch s.method {
		case "server/discover":
			writeJSON(w, result(s.id, discoverWith(map[string]any{"tools": map[string]any{}})))
		case "tools/list":
			writeJSON(w, result(s.id, map[string]any{"tools": tools(), "ttlMs": 0, "cacheScope": "private"}))
		case "tools/call":
			status, v := call(s)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(v)
		default:
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": s.id, "error": map[string]any{"code": -32601, "message": "no"}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dialURL(t *testing.T, url string, pref mcpclient.Preference, o mcpclient.Options) *mcpclient.Client {
	t.Helper()
	tr, err := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.ClientName, o.ClientVersion, o.Preference = "mcpx", "test", pref
	c, err := mcpclient.NewWithOptions(ctx, tr, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

var headerTool = map[string]any{"name": "sql", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
	"region": map[string]any{"type": "string", "x-mcp-header": "Region"},
	"n":      map[string]any{"type": "integer", "x-mcp-header": "N"},
	"query":  map[string]any{"type": "string"},
}}}

var badTool = map[string]any{"name": "bad", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
	"v": map[string]any{"type": "number", "x-mcp-header": "V"},
}}}

func TestModernHTTPHeaders(t *testing.T) {
	rec := &recorder{}
	var mu sync.Mutex
	tools := []any{headerTool, badTool}
	mismatch := 0
	srv := modernHTTP(t, rec, func() []any { mu.Lock(); defer mu.Unlock(); return tools }, func(s seen) (int, any) {
		mu.Lock()
		defer mu.Unlock()
		if mismatch > 0 {
			mismatch--
			// The schema changed under the client: a new annotation.
			tools = []any{map[string]any{"name": "sql", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"region": map[string]any{"type": "string", "x-mcp-header": "Region"},
				"query":  map[string]any{"type": "string", "x-mcp-header": "Query"},
			}}}}
			return 400, map[string]any{"jsonrpc": "2.0", "id": s.id, "error": map[string]any{"code": -32020, "message": "Header mismatch"}}
		}
		return 200, result(s.id, map[string]any{"content": []any{}})
	})
	warned := make(chan mcpclient.Warning, 4)
	c := dialURL(t, srv.URL, mcpclient.ForceModern, mcpclient.Options{})
	c.Subscribe(mcpclient.Notifications{OnWarning: func(w mcpclient.Warning) { warned <- w }})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#schema-extension
	t.Run("2026-07-28/transport/client-excludes-tool-with-invalid-x-mcp-header", func(t *testing.T) {
		got, err := c.ListTools(ctx5())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "sql" {
			t.Fatalf("tools %v: the invalid tool must be excluded, the valid one kept", got)
		}
		select {
		case w := <-warned:
			if w.Tool != "bad" {
				t.Fatalf("warning for %q", w.Tool)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no warning for the excluded tool")
		}
		if _, err := c.CallTool(ctx5(), "bad", map[string]any{"v": 1}); err == nil {
			t.Fatal("an excluded tool was called")
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#client-behavior
	t.Run("2026-07-28/transport/client-sends-standard-and-param-headers", func(t *testing.T) {
		if _, err := c.CallTool(ctx5(), "sql", map[string]any{"region": "Hello, 世界", "n": 42, "query": "q"}); err != nil {
			t.Fatal(err)
		}
		calls := rec.all("tools/call")
		h := calls[len(calls)-1].header
		for k, want := range map[string]string{
			"Mcp-Method": "tools/call", "Mcp-Name": "sql", "MCP-Protocol-Version": "2026-07-28",
			"Mcp-Param-Region": "=?base64?SGVsbG8sIOS4lueVjA==?=", "Mcp-Param-N": "42",
		} {
			if got := h.Get(k); got != want {
				t.Errorf("%s = %q, want %q", k, got, want)
			}
		}
		if h.Get("Mcp-Param-Query") != "" {
			t.Error("an unannotated parameter was mirrored")
		}
		for _, p := range rec.all("tools/list") {
			if p.header.Get("Mcp-Method") != "tools/list" {
				t.Errorf("tools/list Mcp-Method %q", p.header.Get("Mcp-Method"))
			}
		}
	})

	t.Run("2026-07-28/transport/client-relists-and-retries-on-header-mismatch", func(t *testing.T) {
		mu.Lock()
		mismatch = 1
		mu.Unlock()
		lists := len(rec.all("tools/list"))
		if _, err := c.CallTool(ctx5(), "sql", map[string]any{"region": "r", "query": "q"}); err != nil {
			t.Fatalf("the retry should succeed: %v", err)
		}
		if len(rec.all("tools/list")) != lists+1 {
			t.Error("tools/list was not read again after -32020")
		}
		calls := rec.all("tools/call")
		if got := calls[len(calls)-1].header.Get("Mcp-Param-Query"); got != "q" {
			t.Errorf("the retry did not carry the new annotation: %q", got)
		}
	})

	t.Run("2026-07-28/transport/no-session-id-on-modern-requests", func(t *testing.T) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, p := range rec.posts {
			if p.header.Get("Mcp-Session-Id") != "" {
				t.Fatalf("%s carried Mcp-Session-Id", p.method)
			}
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#cancellation
func TestModernHTTPCancellationClosesTheStream(t *testing.T) {
	t.Run("2026-07-28/transport/cancel-by-closing-stream-no-notification", func(t *testing.T) {
		rec := &recorder{}
		closed := make(chan struct{}, 1)
		srv := modernHTTP(t, rec, func() []any { return []any{} }, func(s seen) (int, any) {
			return 0, nil
		})
		// Replace tools/call with a handler that waits for the client to go.
		srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := decodePost(r)
			rec.add(s)
			switch {
			case len(s.id) == 0:
				w.WriteHeader(http.StatusAccepted)
			case s.method == "server/discover":
				writeJSON(w, result(s.id, discoverWith(nil)))
			case s.method == "tools/list":
				writeJSON(w, result(s.id, map[string]any{"tools": []any{}, "ttlMs": 0, "cacheScope": "private"}))
			case s.method == "tools/call":
				// A stream opened and left waiting, which is what a slow
				// call looks like: the client's Send has returned.
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				closed <- struct{}{}
			}
		})
		c := dialURL(t, srv.URL, mcpclient.ForceModern, mcpclient.Options{})
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, _ = c.CallTool(ctx, "slow", nil)
		select {
		case <-closed:
		case <-time.After(3 * time.Second):
			t.Fatal("the request stream was not closed")
		}
		time.Sleep(100 * time.Millisecond)
		if n := len(rec.all("notifications/cancelled")); n != 0 {
			t.Fatalf("%d notifications/cancelled POSTed to a modern server", n)
		}
	})
}

// legacyHTTP is a 2025-11-25-style Streamable HTTP server, parameterised by
// what tools/call does and what the GET stream carries.
type legacyHTTP struct {
	rec        *recorder
	version    string
	onCall     func(w http.ResponseWriter, s seen)
	onGet      func(w http.ResponseWriter, r *http.Request)
	onResponse func(s seen)
}

func (l *legacyHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Mcp-Session-Id", "sess-1")
	if r.Method == http.MethodGet {
		l.rec.mu.Lock()
		l.rec.gets = append(l.rec.gets, r.Header.Clone())
		l.rec.mu.Unlock()
		if l.onGet == nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		l.onGet(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s := decodePost(r)
	l.rec.add(s)
	if s.method == "" {
		if l.onResponse != nil {
			l.onResponse(s)
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if len(s.id) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch s.method {
	case "initialize":
		writeJSON(w, result(s.id, initResult(l.version, map[string]any{"tools": map[string]any{}})))
	case "tools/call":
		l.onCall(w, s)
	default:
		writeJSON(w, result(s.id, map[string]any{"tools": []any{}}))
	}
}

func sse(w http.ResponseWriter) func(format string, a ...any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	f := w.(http.Flusher)
	f.Flush()
	return func(format string, a ...any) {
		fmt.Fprintf(w, format, a...)
		f.Flush()
	}
}

// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#protocol-version-header
func TestLegacyHTTPNegotiatedVersionHeader(t *testing.T) {
	t.Run("2025-11-25/transport/protocol-version-header-is-negotiated", func(t *testing.T) {
		rec := &recorder{}
		srv := httptest.NewServer(&legacyHTTP{rec: rec, version: "2025-06-18", onCall: func(w http.ResponseWriter, s seen) {
			writeJSON(w, result(s.id, map[string]any{"content": []any{}}))
		}})
		t.Cleanup(srv.Close)
		c := dialURL(t, srv.URL, mcpclient.ForceLegacy, mcpclient.Options{})
		if _, err := c.CallTool(ctx5(), "x", nil); err != nil {
			t.Fatal(err)
		}
		if h := rec.all("initialize")[0].header.Get("MCP-Protocol-Version"); h != "" {
			t.Errorf("initialize carried MCP-Protocol-Version %q before anything was negotiated", h)
		}
		for _, m := range []string{"notifications/initialized", "tools/call"} {
			if h := rec.all(m)[0].header.Get("MCP-Protocol-Version"); h != "2025-06-18" {
				t.Errorf("%s: MCP-Protocol-Version %q, want the negotiated 2025-06-18", m, h)
			}
		}
	})
}

// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#resumability-and-redelivery
func TestLegacyHTTPStreamResumption(t *testing.T) {
	t.Run("2025-11-25/transport/client-resumes-closed-stream-with-last-event-id-after-retry", func(t *testing.T) {
		rec := &recorder{}
		var mu sync.Mutex
		var pending json.RawMessage
		var closedAt, resumedAt time.Time
		l := &legacyHTTP{rec: rec, version: "2025-11-25"}
		l.onCall = func(w http.ResponseWriter, s seen) {
			mu.Lock()
			pending = s.id
			mu.Unlock()
			send := sse(w)
			send("id: ev-1\nretry: 300\ndata: \n\n")
			mu.Lock()
			closedAt = time.Now()
			mu.Unlock()
		}
		l.onGet = func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			id := pending
			if r.Header.Get("Last-Event-ID") != "" {
				resumedAt = time.Now()
			}
			mu.Unlock()
			send := sse(w)
			if r.Header.Get("Last-Event-ID") == "ev-1" && id != nil {
				b, _ := json.Marshal(result(id, map[string]any{"content": []any{map[string]any{"type": "text", "text": "resumed"}}}))
				send("id: ev-2\ndata: %s\n\n", b)
				return
			}
			<-r.Context().Done()
		}
		srv := httptest.NewServer(l)
		t.Cleanup(srv.Close)
		c := dialURL(t, srv.URL, mcpclient.ForceLegacy, mcpclient.Options{})
		out, err := c.CallTool(ctx5(), "slow", nil)
		if err != nil || !strings.Contains(string(out), "resumed") {
			t.Fatalf("out %s err %v", out, err)
		}
		mu.Lock()
		defer mu.Unlock()
		if d := resumedAt.Sub(closedAt); d < 250*time.Millisecond {
			t.Fatalf("resumed %v after the close; the server asked for 300ms", d)
		}
	})
}

// A server that asks outside the POST's own stream -- the TypeScript SDK's
// server.request() inside a tool handler goes to the standalone GET stream --
// was never heard, and the tool call hung. The official suite's
// elicitation-sep1034-client-defaults scenario is exactly this.
// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#listening-for-messages-from-the-server
func TestLegacyHTTPStandaloneStream(t *testing.T) {
	t.Run("2025-11-25/transport/client-hears-server-requests-on-get-stream", func(t *testing.T) {
		rec := &recorder{}
		getStream := make(chan func(string, ...any), 1)
		answered := make(chan map[string]any, 1)
		l := &legacyHTTP{rec: rec, version: "2025-11-25"}
		l.onGet = func(w http.ResponseWriter, r *http.Request) {
			getStream <- sse(w)
			<-r.Context().Done()
		}
		l.onResponse = func(s seen) {
			if jsonKey(s.body["id"]) == `"el-1"` {
				answered <- s.body
			}
		}
		l.onCall = func(w http.ResponseWriter, s seen) {
			var send func(string, ...any)
			select {
			case send = <-getStream:
			case <-time.After(3 * time.Second):
				writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": s.id, "error": map[string]any{"code": -1, "message": "no GET stream"}})
				return
			}
			req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "el-1", "method": "elicitation/create",
				"params": map[string]any{"message": "name?", "requestedSchema": map[string]any{"type": "object",
					"properties": map[string]any{"name": map[string]any{"type": "string", "default": "Ann"}}}}})
			send("data: %s\n\n", req)
			select {
			case a := <-answered:
				b, _ := json.Marshal(a)
				writeJSON(w, result(s.id, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}}))
			case <-time.After(3 * time.Second):
				writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": s.id, "error": map[string]any{"code": -1, "message": "no answer"}})
			}
		}
		srv := httptest.NewServer(l)
		t.Cleanup(srv.Close)
		c := dialURL(t, srv.URL, mcpclient.ForceLegacy, mcpclient.Options{
			OnServerRequest: func(context.Context, string, json.RawMessage) (any, error) {
				return map[string]any{"action": "accept", "content": map[string]any{}}, nil
			},
		})
		out, err := c.CallTool(ctx5(), "ask", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "Ann") {
			t.Fatalf("the elicitation answer (with its default) did not reach the server: %s", out)
		}
		rec.mu.Lock()
		get := rec.gets[0]
		rec.mu.Unlock()
		if get.Get("Accept") != "text/event-stream" || get.Get("Mcp-Session-Id") != "sess-1" {
			t.Errorf("GET headers %v", get)
		}
	})
}

// httpSSEServer is a 2024-11-05 HTTP+SSE server: GET /sse streams, POST
// /message?s=1 takes messages and answers on the stream.
func httpSSEServer(t *testing.T, endpoint string) (*httptest.Server, *recorder) {
	rec := &recorder{}
	out := make(chan []byte, 16)
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		send := sse(w)
		send("event: endpoint\ndata: %s\n\n", endpoint)
		for {
			select {
			case b := <-out:
				send("event: message\ndata: %s\n\n", b)
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		s := decodePost(r)
		rec.add(s)
		w.WriteHeader(http.StatusAccepted)
		if len(s.id) == 0 || s.method == "" {
			return
		}
		var v any
		switch s.method {
		case "initialize":
			v = result(s.id, initResult("2024-11-05", map[string]any{"tools": map[string]any{}}))
		case "tools/list":
			v = result(s.id, map[string]any{"tools": []any{map[string]any{"name": "old", "inputSchema": map[string]any{"type": "object"}}}})
		default:
			v = map[string]any{"jsonrpc": "2.0", "id": s.id, "error": map[string]any{"code": -32601, "message": "no"}}
		}
		b, _ := json.Marshal(v)
		out <- b
	})
	// Anything else -- the Streamable HTTP probe's POST -- is refused the
	// way an HTTP+SSE-only server refuses it.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rec.add(seen{method: "refused " + r.Method})
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

// https://modelcontextprotocol.io/specification/2024-11-05/basic/transports#http-with-sse
func TestHTTPSSETransport(t *testing.T) {
	t.Run("2024-11-05/transport-http-sse/client-posts-to-endpoint-and-reads-stream", func(t *testing.T) {
		srv, rec := httpSSEServer(t, "/message?s=1")
		tr, err := mcpclient.NewLegacySSE(ctx5(), mcpclient.HTTPOptions{URL: srv.URL + "/sse"})
		if err != nil {
			t.Fatal(err)
		}
		c, err := mcpclient.NewWithOptions(ctx5(), tr, mcpclient.Options{ClientName: "mcpx", ClientVersion: "t",
			Preference: mcpclient.ForceLegacy})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		tools, err := c.ListTools(ctx5())
		if err != nil || len(tools) != 1 || tools[0].Name != "old" {
			t.Fatalf("tools %v err %v", tools, err)
		}
		if c.Negotiated != "2024-11-05" {
			t.Errorf("negotiated %q", c.Negotiated)
		}
		if len(rec.all("tools/list")) != 1 {
			t.Error("tools/list did not reach the endpoint")
		}
	})
	t.Run("2024-11-05/transport-http-sse/endpoint-on-another-origin-refused", func(t *testing.T) {
		srv, _ := httpSSEServer(t, "http://attacker.invalid/steal")
		_, err := mcpclient.NewLegacySSE(ctx5(), mcpclient.HTTPOptions{URL: srv.URL + "/sse"})
		if err == nil || !strings.Contains(err.Error(), "origin") {
			t.Fatalf("a foreign endpoint was accepted: %v", err)
		}
	})
	// The Streamable HTTP probe against this server ends in the error that
	// tells the pool to try HTTP+SSE.
	t.Run("2025-11-25/transport-http-sse/backcompat-probe-refusal-is-recognised", func(t *testing.T) {
		srv, _ := httpSSEServer(t, "/message")
		tr, _ := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: srv.URL + "/sse"})
		_, err := mcpclient.NewWithOptions(ctx5(), tr, mcpclient.Options{ClientName: "mcpx", ClientVersion: "t"})
		var refused *mcpclient.LegacyHTTPRefusedError
		if !errors.As(err, &refused) {
			t.Fatalf("want LegacyHTTPRefusedError, got %v", err)
		}
	})
}

// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#sending-messages-to-the-server
func TestLegacyHTTPCancellation(t *testing.T) {
	t.Run("2025-11-25/transport/client-cancel-by-notification-even-before-response-headers", func(t *testing.T) {
		rec := &recorder{}
		srv := httptest.NewServer(&legacyHTTP{rec: rec, version: "2025-11-25", onCall: func(w http.ResponseWriter, s seen) {
			time.Sleep(300 * time.Millisecond) // no headers until after the client gave up
		}})
		t.Cleanup(srv.Close)
		c := dialURL(t, srv.URL, mcpclient.ForceLegacy, mcpclient.Options{})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, _ = c.CallTool(ctx, "slow", nil)
		deadline := time.Now().Add(3 * time.Second)
		for len(rec.all("notifications/cancelled")) == 0 {
			if time.Now().After(deadline) {
				t.Fatal("a timed-out legacy request was never cancelled")
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}
