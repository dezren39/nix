package mcpclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// probeServer is an in-process server whose answers are a function, so each
// era behaviour the spec names can be written as one small handler.
type probeServer struct {
	mu     sync.Mutex
	in     chan []byte
	frames []map[string]any
	closed bool
	// answer returns the reply frame's result or error for a request, or
	// (nil, nil) for silence. close makes the server hang up instead.
	answer func(method string, params map[string]any) (result any, rpcErr map[string]any, close bool, delay time.Duration)
}

func newProbeServer(answer func(string, map[string]any) (any, map[string]any, bool, time.Duration)) *probeServer {
	return &probeServer{in: make(chan []byte, 64), answer: answer}
}

func (s *probeServer) Send(_ context.Context, msg []byte) error {
	var f map[string]any
	_ = json.Unmarshal(msg, &f)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return io.ErrClosedPipe
	}
	s.frames = append(s.frames, f)
	s.mu.Unlock()
	method, _ := f["method"].(string)
	if method == "" || f["id"] == nil {
		return nil
	}
	params, _ := f["params"].(map[string]any)
	result, rpcErr, hangUp, delay := s.answer(method, params)
	if hangUp {
		s.Close()
		return nil
	}
	if result == nil && rpcErr == nil {
		return nil
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": f["id"]}
	if rpcErr != nil {
		reply["error"] = rpcErr
	} else {
		reply["result"] = result
	}
	b, _ := json.Marshal(reply)
	go func() {
		time.Sleep(delay)
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.closed {
			s.in <- b
		}
	}()
	return nil
}

func (s *probeServer) Recv() ([]byte, error) {
	b, ok := <-s.in
	if !ok {
		return nil, io.EOF
	}
	return b, nil
}

func (s *probeServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.in)
	}
	return nil
}

func (s *probeServer) Info() string { return "probe" }

// methods lists the requests and notifications the server received, in order.
func (s *probeServer) methods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, f := range s.frames {
		if m, _ := f["method"].(string); m != "" {
			out = append(out, m)
		}
	}
	return out
}

func (s *probeServer) discovers() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, f := range s.frames {
		if f["method"] == "server/discover" {
			p, _ := f["params"].(map[string]any)
			out = append(out, p)
		}
	}
	return out
}

func discoverResult(versions ...string) map[string]any {
	return map[string]any{
		"resultType":        "complete",
		"supportedVersions": versions,
		"capabilities":      map[string]any{"tools": map[string]any{}},
		"_meta": map[string]any{
			"io.modelcontextprotocol/serverInfo": map[string]any{"name": "probe-modern", "version": "2"},
		},
	}
}

var initOK = map[string]any{
	"protocolVersion": "2025-06-18",
	"serverInfo":      map[string]any{"name": "probe-legacy", "version": "1"},
	"capabilities":    map[string]any{"tools": map[string]any{}},
}

func rpcErr(code int, msg string, data any) map[string]any {
	e := map[string]any{"code": code, "message": msg}
	if data != nil {
		e["data"] = data
	}
	return e
}

// legacyAnswering builds a legacy server that answers discover however
// unknownDiscover says.
func legacyAnswering(unknown func() (any, map[string]any, bool)) func(string, map[string]any) (any, map[string]any, bool, time.Duration) {
	return func(method string, _ map[string]any) (any, map[string]any, bool, time.Duration) {
		switch method {
		case "initialize":
			return initOK, nil, false, 0
		case "server/discover":
			r, e, c := unknown()
			return r, e, c, 0
		}
		return nil, rpcErr(-32601, "no "+method, nil), false, 0
	}
}

// modernOnly rejects initialize (naming its versions, as versioning.mdx
// says a modern-only server SHOULD) and answers discover for the versions it
// supports.
func modernOnly(supported []string, delay time.Duration) func(string, map[string]any) (any, map[string]any, bool, time.Duration) {
	return func(method string, params map[string]any) (any, map[string]any, bool, time.Duration) {
		switch method {
		case "initialize":
			return nil, rpcErr(-32601, "initialize unsupported; supported: "+strings.Join(supported, ","), nil), false, 0
		case "server/discover":
			meta, _ := params["_meta"].(map[string]any)
			asked, _ := meta[mcpclient.MetaProtocolVersion].(string)
			for _, v := range supported {
				if v == asked {
					return discoverResult(supported...), nil, false, delay
				}
			}
			return nil, rpcErr(-32022, "Unsupported protocol version",
				map[string]any{"supported": supported, "requested": asked}), false, delay
		}
		return map[string]any{}, nil, false, 0
	}
}

func probeDial(t *testing.T, s *probeServer, o mcpclient.Options) (*mcpclient.Client, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.ClientName, o.ClientVersion = "mcpx", "test"
	if o.ProbeTimeout == 0 {
		o.ProbeTimeout = 100 * time.Millisecond
	}
	c, err := mcpclient.NewWithOptions(ctx, s, o)
	if c != nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, err
}

func sent(s *probeServer, method string) bool {
	for _, m := range s.methods() {
		if m == method {
			return true
		}
	}
	return false
}

func TestStdioEraProbe(t *testing.T) {
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#backward-compatibility
	legacyCases := []struct {
		id     string
		answer func() (any, map[string]any, bool)
	}{
		{"legacy-answering-32601-falls-back-to-initialize", func() (any, map[string]any, bool) {
			return nil, rpcErr(-32601, "Method not found", nil), false
		}},
		{"legacy-answering-32602-falls-back-to-initialize", func() (any, map[string]any, bool) {
			return nil, rpcErr(-32602, "Invalid params", nil), false
		}},
		// A code no list would contain: the fallback MUST NOT be keyed to one
		// specific error code.
		{"fallback-not-keyed-to-one-error-code", func() (any, map[string]any, bool) {
			return nil, rpcErr(-32000, "server not initialized", nil), false
		}},
		// Processing an era-ambiguous method under legacy semantics.
		{"non-discover-success-is-legacy", func() (any, map[string]any, bool) {
			return map[string]any{}, nil, false
		}},
		{"legacy-silent-falls-back-after-probe-timeout", func() (any, map[string]any, bool) {
			return nil, nil, false
		}},
	}
	for _, tc := range legacyCases {
		t.Run("2026-07-28/stdio-compat/"+tc.id, func(t *testing.T) {
			s := newProbeServer(legacyAnswering(tc.answer))
			c, err := probeDial(t, s, mcpclient.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if c.Era != mcpclient.EraLegacy || c.Negotiated != "2025-06-18" {
				t.Errorf("era=%q negotiated=%q, want legacy 2025-06-18", c.Era, c.Negotiated)
			}
			got := strings.Join(s.methods(), ",")
			if got != "server/discover,initialize,notifications/initialized" {
				t.Errorf("frames = %s", got)
			}
			if c.Source != mcpclient.SourceProbe {
				t.Errorf("source = %q", c.Source)
			}
		})
	}

	t.Run("2026-07-28/stdio-compat/probe-carries-full-meta", func(t *testing.T) {
		s := newProbeServer(modernOnly([]string{"2026-07-28"}, 0))
		if _, err := probeDial(t, s, mcpclient.Options{}); err != nil {
			t.Fatal(err)
		}
		d := s.discovers()
		if len(d) != 1 {
			t.Fatalf("discovers = %d", len(d))
		}
		meta, _ := d[0]["_meta"].(map[string]any)
		if meta[mcpclient.MetaProtocolVersion] != mcpclient.ModernVersions[0] {
			t.Errorf("protocolVersion = %v", meta[mcpclient.MetaProtocolVersion])
		}
		for _, k := range []string{mcpclient.MetaClientCapabilities, mcpclient.MetaClientInfo} {
			if _, ok := meta[k]; !ok {
				t.Errorf("_meta lacks %s: %v", k, meta)
			}
		}
	})

	t.Run("2026-07-28/stdio-compat/discover-result-is-modern", func(t *testing.T) {
		s := newProbeServer(modernOnly([]string{"2026-07-28"}, 0))
		c, err := probeDial(t, s, mcpclient.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if c.Era != mcpclient.EraModern || c.Negotiated != "2026-07-28" {
			t.Errorf("era=%q negotiated=%q", c.Era, c.Negotiated)
		}
		// https://modelcontextprotocol.io/specification/2026-07-28/schema#discoverresult
		if c.ServerInfo.Name != "probe-modern" {
			t.Errorf("serverInfo should come from _meta: %+v", c.ServerInfo)
		}
		if sent(s, "initialize") {
			t.Errorf("a modern server must not be sent initialize: %v", s.methods())
		}
	})

	t.Run("2026-07-28/stdio-compat/dual-era-stays-modern", func(t *testing.T) {
		s := newProbeServer(func(m string, p map[string]any) (any, map[string]any, bool, time.Duration) {
			if m == "initialize" {
				return initOK, nil, false, 0
			}
			return modernOnly([]string{"2026-07-28", "2025-11-25"}, 0)(m, p)
		})
		c, err := probeDial(t, s, mcpclient.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if c.Era != mcpclient.EraModern || c.Negotiated != "2026-07-28" {
			t.Errorf("era=%q negotiated=%q", c.Era, c.Negotiated)
		}
		if sent(s, "initialize") {
			t.Errorf("frames = %v", s.methods())
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#protocol-version-negotiation
	t.Run("2026-07-28/versioning/unsupported-version-retries-with-supported", func(t *testing.T) {
		s := newProbeServer(modernOnly([]string{"2026-07-28"}, 0))
		c, err := probeDial(t, s, mcpclient.Options{ModernVersions: []string{"2099-01-01", "2026-07-28"}})
		if err != nil {
			t.Fatal(err)
		}
		if c.Era != mcpclient.EraModern || c.Negotiated != "2026-07-28" {
			t.Errorf("era=%q negotiated=%q", c.Era, c.Negotiated)
		}
		d := s.discovers()
		if len(d) != 2 {
			t.Fatalf("want a retry, got %d discovers", len(d))
		}
		if v := d[1]["_meta"].(map[string]any)[mcpclient.MetaProtocolVersion]; v != "2026-07-28" {
			t.Errorf("retry asked for %v", v)
		}
		if sent(s, "initialize") {
			t.Errorf("-32022 identifies a modern server; no fallback: %v", s.methods())
		}
	})

	t.Run("2026-07-28/stdio-compat/unsupported-version-without-mutual-is-an-error", func(t *testing.T) {
		s := newProbeServer(modernOnly([]string{"2099-01-01"}, 0))
		_, err := probeDial(t, s, mcpclient.Options{})
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "2099-01-01") {
			t.Errorf("the supported list should reach the caller: %v", err)
		}
		if sent(s, "initialize") {
			t.Errorf("do not fall back to initialize: %v", s.methods())
		}
	})

	t.Run("2026-07-28/stdio-compat/slow-modern-beyond-probe-timeout-stays-modern", func(t *testing.T) {
		s := newProbeServer(modernOnly([]string{"2026-07-28"}, 300*time.Millisecond))
		c, err := probeDial(t, s, mcpclient.Options{ProbeTimeout: 50 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if c.Era != mcpclient.EraModern {
			t.Errorf("a late DiscoverResult should still win: era=%q", c.Era)
		}
		// initialize was sent alongside -- that is the timeout path.
		if !sent(s, "initialize") {
			t.Errorf("the timeout should have sent initialize too: %v", s.methods())
		}
		if n := len(s.discovers()); n != 1 {
			t.Errorf("the first discover should have been kept, not re-sent: %d sent", n)
		}
	})

	t.Run("2026-07-28/stdio-compat/closed-before-discover-is-a-typed-error", func(t *testing.T) {
		s := newProbeServer(legacyAnswering(func() (any, map[string]any, bool) { return nil, nil, true }))
		_, err := probeDial(t, s, mcpclient.Options{})
		if !errors.Is(err, mcpclient.ErrClosedDuringProbe) {
			t.Fatalf("err = %v", err)
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions
// "MAY persist it across restarts ... re-probing if the cached assumption
// later fails."
func TestCachedEra(t *testing.T) {
	legacy := legacyAnswering(func() (any, map[string]any, bool) {
		return nil, rpcErr(-32601, "no", nil), false
	})
	t.Run("2026-07-28/era-cache/cached-legacy-sends-no-discover", func(t *testing.T) {
		s := newProbeServer(legacy)
		c, err := probeDial(t, s, mcpclient.Options{Cached: mcpclient.EraLegacy})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(s.methods(), ","); got != "initialize,notifications/initialized" {
			t.Errorf("frames = %s", got)
		}
		if c.Source != mcpclient.SourceCache || c.CachedEraWrong {
			t.Errorf("source=%q wrong=%v", c.Source, c.CachedEraWrong)
		}
	})
	t.Run("2026-07-28/era-cache/cached-legacy-wrong-reprobes-modern", func(t *testing.T) {
		s := newProbeServer(modernOnly([]string{"2026-07-28"}, 0))
		c, err := probeDial(t, s, mcpclient.Options{Cached: mcpclient.EraLegacy})
		if err != nil {
			t.Fatal(err)
		}
		if c.Era != mcpclient.EraModern || !c.CachedEraWrong || c.Source != mcpclient.SourceProbe {
			t.Errorf("era=%q wrong=%v source=%q", c.Era, c.CachedEraWrong, c.Source)
		}
	})
	t.Run("2026-07-28/era-cache/cached-modern-wrong-reprobes-legacy", func(t *testing.T) {
		s := newProbeServer(legacy)
		c, err := probeDial(t, s, mcpclient.Options{Cached: mcpclient.EraModern})
		if err != nil {
			t.Fatal(err)
		}
		if c.Era != mcpclient.EraLegacy || !c.CachedEraWrong {
			t.Errorf("era=%q wrong=%v", c.Era, c.CachedEraWrong)
		}
	})
	t.Run("2026-07-28/era-cache/force-ignores-cache", func(t *testing.T) {
		s := newProbeServer(modernOnly([]string{"2026-07-28"}, 0))
		c, err := probeDial(t, s, mcpclient.Options{Cached: mcpclient.EraLegacy, Preference: mcpclient.ForceModern})
		if err != nil {
			t.Fatal(err)
		}
		if sent(s, "initialize") || c.Source != mcpclient.SourceForced {
			t.Errorf("frames=%v source=%q", s.methods(), c.Source)
		}
	})
}

// httpServer is a Streamable HTTP endpoint whose answer to each JSON-RPC
// method is a status and a body.
func httpServer(t *testing.T, h func(method string, id json.RawMessage, r *http.Request) (int, string)) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var f struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &f)
		mu.Lock()
		seen = append(seen, f.Method)
		mu.Unlock()
		if len(f.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		status, body := h(f.Method, f.ID, r)
		if body != "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func dialHTTP(t *testing.T, url string, o mcpclient.Options) (*mcpclient.Client, error) {
	t.Helper()
	tr, err := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.ClientName, o.ClientVersion = "mcpx", "test"
	c, err := mcpclient.NewWithOptions(ctx, tr, o)
	if c != nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, err
}

func legacyInit(id json.RawMessage) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": initOK})
	return string(b)
}

func has(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestHTTPEraProbe(t *testing.T) {
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#backward-compatibility
	t.Run("2026-07-28/http-compat/probe-sends-modern-headers", func(t *testing.T) {
		var hdr http.Header
		srv, _ := httpServer(t, func(m string, id json.RawMessage, r *http.Request) (int, string) {
			if m == "server/discover" {
				hdr = r.Header.Clone()
			}
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": discoverResult("2026-07-28")})
			return 200, string(b)
		})
		c, err := dialHTTP(t, srv.URL, mcpclient.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if c.Era != mcpclient.EraModern {
			t.Errorf("era = %q", c.Era)
		}
		if hdr.Get("MCP-Protocol-Version") != "2026-07-28" || hdr.Get("Mcp-Session-Id") != "" {
			t.Errorf("headers = %v", hdr)
		}
	})
	t.Run("2026-07-28/http-compat/400-with-modern-error-body-retries-not-falls-back", func(t *testing.T) {
		srv, seen := httpServer(t, func(m string, id json.RawMessage, r *http.Request) (int, string) {
			if m == "initialize" {
				return 200, legacyInit(id)
			}
			if r.Header.Get("MCP-Protocol-Version") != "2026-07-28" {
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id,
					"error": rpcErr(-32022, "Unsupported protocol version", map[string]any{"supported": []string{"2026-07-28"}})})
				return 400, string(b)
			}
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": discoverResult("2026-07-28")})
			return 200, string(b)
		})
		c, err := dialHTTP(t, srv.URL, mcpclient.Options{ModernVersions: []string{"2099-01-01", "2026-07-28"}})
		if err != nil {
			t.Fatal(err)
		}
		if c.Era != mcpclient.EraModern || has(*seen, "initialize") {
			t.Errorf("era=%q seen=%v", c.Era, *seen)
		}
	})
	t.Run("2026-07-28/http-compat/400-with-modern-error-no-mutual-is-an-error", func(t *testing.T) {
		srv, seen := httpServer(t, func(m string, id json.RawMessage, _ *http.Request) (int, string) {
			if m == "initialize" {
				return 200, legacyInit(id)
			}
			// id null, as a header-validation failure is written before the
			// body is trusted.
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": nil,
				"error": rpcErr(-32022, "Unsupported protocol version", map[string]any{"supported": []string{"2099-01-01"}})})
			return 400, string(b)
		})
		_, err := dialHTTP(t, srv.URL, mcpclient.Options{})
		if err == nil || has(*seen, "initialize") {
			t.Errorf("err=%v seen=%v", err, *seen)
		}
	})
	for _, tc := range []struct {
		id     string
		status int
		body   string
	}{
		{"400-without-body-falls-back", 400, ""},
		{"400-with-non-modern-body-falls-back", 400, `{"error":"bad request"}`},
		{"404-without-body-falls-back", 404, ""},
	} {
		t.Run("2026-07-28/http-compat/"+tc.id, func(t *testing.T) {
			srv, seen := httpServer(t, func(m string, id json.RawMessage, _ *http.Request) (int, string) {
				if m == "initialize" {
					return 200, legacyInit(id)
				}
				return tc.status, tc.body
			})
			c, err := dialHTTP(t, srv.URL, mcpclient.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if c.Era != mcpclient.EraLegacy || !has(*seen, "initialize") {
				t.Errorf("era=%q seen=%v", c.Era, *seen)
			}
		})
	}
	// A 4xx whose body answers the request is that request's reply, not a
	// transport failure.
	t.Run("2026-07-28/http-compat/4xx-json-rpc-reply-is-delivered-as-the-reply", func(t *testing.T) {
		srv, _ := httpServer(t, func(m string, id json.RawMessage, _ *http.Request) (int, string) {
			if m == "server/discover" {
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": discoverResult("2026-07-28")})
				return 200, string(b)
			}
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcErr(-32602, "Unknown tool: nope", nil)})
			return 400, string(b)
		})
		c, err := dialHTTP(t, srv.URL, mcpclient.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.CallTool(context.Background(), "nope", nil)
		var he *mcpclient.HTTPStatusError
		if err == nil || errors.As(err, &he) || !strings.Contains(err.Error(), "mcp error -32602") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("2026-07-28/http-compat/legacy-refused-too-is-typed-for-http-sse", func(t *testing.T) {
		srv, _ := httpServer(t, func(string, json.RawMessage, *http.Request) (int, string) { return 405, "" })
		_, err := dialHTTP(t, srv.URL, mcpclient.Options{})
		var refused *mcpclient.LegacyHTTPRefusedError
		if !errors.As(err, &refused) || refused.Status != 405 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("2026-07-28/http-compat/connection-refused-is-not-a-fallback", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		ln.Close()
		_, err = dialHTTP(t, "http://"+addr+"/mcp", mcpclient.Options{})
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), "initialize") {
			t.Errorf("an unreachable server says nothing about its era: %v", err)
		}
	})
}
