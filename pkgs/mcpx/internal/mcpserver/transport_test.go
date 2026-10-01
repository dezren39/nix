package mcpserver_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// Transport conformance for mcpx as a server. Each case is named
// <revision>/<area>/<requirement> and cites the page it comes from.

const modern = "2026-07-28"

var modernMeta = map[string]any{
	"io.modelcontextprotocol/protocolVersion":    modern,
	"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "t", "version": "1"},
}

// frame builds a JSON-RPC message. A nil id makes a notification.
func frame(id any, method string, params map[string]any) []byte {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	b, _ := json.Marshal(m)
	return b
}

// post answers one POST in process.
func post(s *mcpserver.Server, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func modernHeaders(method, name string) map[string]string {
	h := map[string]string{"MCP-Protocol-Version": modern, "Mcp-Method": method}
	if name != "" {
		h["Mcp-Name"] = name
	}
	return h
}

func rpcErr(t *testing.T, body []byte) (int, json.RawMessage) {
	t.Helper()
	var f struct {
		ID    json.RawMessage `json:"id"`
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatalf("not a JSON-RPC message: %v\n%s", err, body)
	}
	if f.Error == nil {
		return 0, f.ID
	}
	return f.Error.Code, f.ID
}

func TestModernStreamableHTTPHeaders(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	call := frame(7, "tools/call", modernParams(map[string]any{"name": "mcpx_status"}))

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#server-validation
	mismatch := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400:\n%s", w.Code, w.Body)
		}
		code, id := rpcErr(t, w.Body.Bytes())
		if code != -32020 {
			t.Fatalf("code %d, want -32020 HeaderMismatch:\n%s", code, w.Body)
		}
		if string(id) != "7" {
			t.Errorf("the request id is readable and should be echoed, got %s", id)
		}
	}
	// A body that carries no _meta at all is a different fault from a header
	// that contradicts one, and the specification gives it a different code:
	// basic/index, "A request missing any required field is malformed; the
	// server MUST reject it with JSON-RPC error code -32602 (Invalid params).
	// On HTTP, the response status MUST be 400 Bad Request." The official
	// conformance suite scores it the same way -- its server-stateless
	// scenario asserts -32602 for both sep-2575-request-meta-invalid-missing-meta
	// and -missing-protocol-version. This subtest previously asserted -32020,
	// which made the bug look correct.
	invalidParams := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400:\n%s", w.Code, w.Body)
		}
		code, id := rpcErr(t, w.Body.Bytes())
		if code != -32602 {
			t.Fatalf("code %d, want -32602 InvalidParams:\n%s", code, w.Body)
		}
		if string(id) != "7" {
			t.Errorf("the request id is readable and should be echoed, got %s", id)
		}
	}
	t.Run("2026-07-28/streamable-http/protocol-version-header-required", func(t *testing.T) {
		h := modernHeaders("tools/call", "mcpx_status")
		delete(h, "MCP-Protocol-Version")
		mismatch(t, post(s, call, h))
	})
	t.Run("2026-07-28/streamable-http/protocol-version-header-matches-meta", func(t *testing.T) {
		h := modernHeaders("tools/call", "mcpx_status")
		h["MCP-Protocol-Version"] = "2025-11-25"
		mismatch(t, post(s, call, h))
	})
	t.Run("2026-07-28/streamable-http/body-without-meta-is-invalid-params", func(t *testing.T) {
		invalidParams(t, post(s, frame(7, "tools/list", nil), modernHeaders("tools/list", "")))
	})
	t.Run("2026-07-28/streamable-http/mcp-method-header-required", func(t *testing.T) {
		h := modernHeaders("tools/call", "mcpx_status")
		delete(h, "Mcp-Method")
		mismatch(t, post(s, call, h))
	})
	t.Run("2026-07-28/streamable-http/mcp-method-header-matches-body", func(t *testing.T) {
		mismatch(t, post(s, call, modernHeaders("tools/list", "mcpx_status")))
	})
	t.Run("2026-07-28/streamable-http/header-values-are-case-sensitive", func(t *testing.T) {
		mismatch(t, post(s, call, modernHeaders("TOOLS/CALL", "mcpx_status")))
	})
	for _, c := range []struct {
		method string
		params map[string]any
		name   string
	}{
		{"tools/call", map[string]any{"name": "mcpx_status"}, "mcpx_status"},
		{"prompts/get", map[string]any{"name": "summarise"}, "summarise"},
		{"resources/read", map[string]any{"uri": "demo://a"}, "demo://a"},
	} {
		body := frame(7, c.method, modernParams(c.params))
		t.Run("2026-07-28/streamable-http/mcp-name-required-for-"+strings.ReplaceAll(c.method, "/", "-"), func(t *testing.T) {
			mismatch(t, post(s, body, modernHeaders(c.method, "")))
		})
		t.Run("2026-07-28/streamable-http/mcp-name-matches-body-for-"+strings.ReplaceAll(c.method, "/", "-"), func(t *testing.T) {
			mismatch(t, post(s, body, modernHeaders(c.method, "other")))
		})
		t.Run("2026-07-28/streamable-http/mcp-name-accepted-when-it-matches-for-"+strings.ReplaceAll(c.method, "/", "-"), func(t *testing.T) {
			w := post(s, body, modernHeaders(c.method, c.name))
			if w.Code != http.StatusOK {
				t.Fatalf("status %d:\n%s", w.Code, w.Body)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#value-encoding
	t.Run("2026-07-28/streamable-http/mcp-name-base64-sentinel-decoded-before-compare", func(t *testing.T) {
		enc := "=?base64?" + base64.StdEncoding.EncodeToString([]byte("mcpx_status")) + "?="
		if w := post(s, call, modernHeaders("tools/call", enc)); w.Code != http.StatusOK {
			t.Fatalf("an encoded name equal to the body should be accepted, got %d:\n%s", w.Code, w.Body)
		}
		wrong := "=?base64?" + base64.StdEncoding.EncodeToString([]byte("mcpx_other")) + "?="
		mismatch(t, post(s, call, modernHeaders("tools/call", wrong)))
		mismatch(t, post(s, call, modernHeaders("tools/call", "=?base64?!!!?=")))
	})
	t.Run("2026-07-28/streamable-http/header-with-invalid-characters-rejected", func(t *testing.T) {
		mismatch(t, post(s, call, modernHeaders("tools/call", "mcpx_st\xe4tus")))
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#protocol-version-header
	t.Run("2026-07-28/streamable-http/unsupported-version-is-400-with-32022", func(t *testing.T) {
		p := map[string]any{"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion":    "2099-01-01",
			"io.modelcontextprotocol/clientCapabilities": map[string]any{}}}
		h := modernHeaders("tools/list", "")
		h["MCP-Protocol-Version"] = "2099-01-01"
		w := post(s, frame(7, "tools/list", p), h)
		if code, _ := rpcErr(t, w.Body.Bytes()); w.Code != http.StatusBadRequest || code != -32022 {
			t.Fatalf("status %d code %d, want 400 -32022:\n%s", w.Code, code, w.Body)
		}
	})
	t.Run("2026-07-28/streamable-http/unknown-method-is-404-with-32601", func(t *testing.T) {
		w := post(s, frame(7, "no/such", modernParams(nil)), modernHeaders("no/such", ""))
		if code, _ := rpcErr(t, w.Body.Bytes()); w.Code != http.StatusNotFound || code != -32601 {
			t.Fatalf("status %d code %d, want 404 -32601:\n%s", w.Code, code, w.Body)
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#meta
	t.Run("2026-07-28/streamable-http/invalid-params-is-400", func(t *testing.T) {
		body := frame(7, "prompts/get", modernParams(map[string]any{
			"name": "summarise", "arguments": map[string]any{"n": 1}}))
		w := post(s, body, modernHeaders("prompts/get", "summarise"))
		if code, _ := rpcErr(t, w.Body.Bytes()); w.Code != http.StatusBadRequest || code != -32602 {
			t.Fatalf("status %d code %d, want 400 -32602:\n%s", w.Code, code, w.Body)
		}
	})
	t.Run("2026-07-28/streamable-http/success-is-200-json", func(t *testing.T) {
		w := post(s, frame(7, "tools/list", modernParams(nil)), modernHeaders("tools/list", ""))
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("status %d type %q", w.Code, w.Header().Get("Content-Type"))
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#earlier-streamable-http-revisions
	t.Run("2026-07-28/streamable-http/session-id-ignored-and-not-echoed", func(t *testing.T) {
		h := modernHeaders("tools/list", "")
		h["Mcp-Session-Id"] = "sess-nobody-issued-this"
		w := post(s, frame(7, "tools/list", modernParams(nil)), h)
		if w.Code != http.StatusOK {
			t.Fatalf("an unknown session id on a modern request should be ignored, got %d:\n%s", w.Code, w.Body)
		}
		if got := w.Header().Get("Mcp-Session-Id"); got != "" {
			t.Errorf("a modern request must not be given a session id, got %q", got)
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#sending-messages
	t.Run("2026-07-28/streamable-http/batch-is-400", func(t *testing.T) {
		body := []byte("[" + string(frame(1, "ping", modernParams(nil))) + "]")
		w := post(s, body, modernHeaders("ping", ""))
		if code, _ := rpcErr(t, w.Body.Bytes()); w.Code != http.StatusBadRequest || code != -32600 {
			t.Fatalf("status %d code %d, want 400 -32600:\n%s", w.Code, code, w.Body)
		}
	})
	t.Run("2026-07-28/streamable-http/accepted-notification-is-202-no-body", func(t *testing.T) {
		w := post(s, frame(nil, "notifications/cancelled", modernParams(map[string]any{"requestId": 1})),
			modernHeaders("notifications/cancelled", ""))
		if w.Code != http.StatusAccepted || w.Body.Len() != 0 {
			t.Fatalf("status %d body %q, want 202 and nothing", w.Code, w.Body)
		}
	})
	t.Run("2026-07-28/streamable-http/unaccepted-notification-is-4xx", func(t *testing.T) {
		w := post(s, frame(nil, "notifications/cancelled", modernParams(map[string]any{"requestId": 1})),
			modernHeaders("notifications/other", ""))
		if w.Code < 400 || w.Code > 499 {
			t.Fatalf("status %d, want 4xx", w.Code)
		}
		if _, id := rpcErr(t, w.Body.Bytes()); len(id) != 0 && string(id) != "null" {
			t.Errorf("the error for a notification has no id, got %s", id)
		}
	})
}

// Header names are case-insensitive; this needs a real listener, because
// the in-process request never passes through Go's header canonicalisation.
func TestModernHeaderNamesAreCaseInsensitive(t *testing.T) {
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#case-sensitivity
	t.Run("2026-07-28/streamable-http/header-names-case-insensitive", func(t *testing.T) {
		ts := httptest.NewServer(mcpserver.New(newBackend(), "mcpx", "test"))
		defer ts.Close()
		req, _ := http.NewRequest(http.MethodPost, ts.URL,
			strings.NewReader(string(frame(1, "tools/call", modernParams(map[string]any{"name": "mcpx_status"})))))
		req.Header["mcp-protocol-version"] = []string{modern}
		req.Header["MCP-METHOD"] = []string{"tools/call"}
		req.Header["mcp-name"] = []string{"mcpx_status"}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("status %d:\n%s", resp.StatusCode, b)
		}
	})
}

// blockingBackend holds every upstream call until its context ends, and
// reports how it ended: what proves a cancellation reached the backend and
// not only the transport.
type blockingBackend struct {
	*fakeBackend
	started chan struct{}
	ended   chan error
}

func newBlocking() *blockingBackend {
	return &blockingBackend{fakeBackend: newBackend(),
		started: make(chan struct{}, 4), ended: make(chan error, 4)}
}

func (b *blockingBackend) Call(ctx context.Context, _, _ string, _ json.RawMessage) (string, error) {
	b.started <- struct{}{}
	<-ctx.Done()
	b.ended <- ctx.Err()
	return "", ctx.Err()
}

func (b *blockingBackend) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the upstream call never started")
	}
}

func (b *blockingBackend) waitEnded(t *testing.T) {
	t.Helper()
	select {
	case <-b.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancellation never reached the upstream call")
	}
}

func callParams(extra map[string]any) map[string]any {
	p := map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "ns", "tool": "slow"}}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func TestModernClosingTheStreamCancels(t *testing.T) {
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#cancellation
	t.Run("2026-07-28/streamable-http/closing-response-stream-cancels-backend-call", func(t *testing.T) {
		b := newBlocking()
		ts := httptest.NewServer(mcpserver.New(b, "mcpx", "test"))
		defer ts.Close()
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL,
			strings.NewReader(string(frame(1, "tools/call", callParams(map[string]any{"_meta": modernMeta})))))
		for k, v := range modernHeaders("tools/call", "mcpx_call") {
			req.Header.Set(k, v)
		}
		errc := make(chan error, 1)
		go func() {
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
			}
			errc <- err
		}()
		b.waitStarted(t)
		cancel()
		b.waitEnded(t)
		<-errc
	})
}

// legacySession runs initialize and returns the session id.
func legacySession(t *testing.T, s http.Handler, version string) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(
		frame(1, "initialize", map[string]any{"protocolVersion": version, "capabilities": map[string]any{}}))))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	id := w.Header().Get("Mcp-Session-Id")
	if id == "" {
		t.Fatalf("initialize should mint a session:\n%s", w.Body)
	}
	return id
}

func TestLegacyStreamableHTTPSessions(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#session-management
	t.Run("2025-11-25/streamable-http/unknown-session-is-404", func(t *testing.T) {
		w := post(s, frame(2, "tools/list", nil), map[string]string{"Mcp-Session-Id": "sess-unknown"})
		if w.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404:\n%s", w.Code, w.Body)
		}
	})
	t.Run("2025-11-25/streamable-http/deleted-session-is-404", func(t *testing.T) {
		id := legacySession(t, s, "2025-11-25")
		if w := post(s, frame(2, "ping", nil), map[string]string{"Mcp-Session-Id": id}); w.Code != http.StatusOK {
			t.Fatalf("a live session should answer, got %d", w.Code)
		}
		r := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
		r.Header.Set("Mcp-Session-Id", id)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code/100 != 2 {
			t.Fatalf("DELETE status %d", w.Code)
		}
		if w := post(s, frame(3, "ping", nil), map[string]string{"Mcp-Session-Id": id}); w.Code != http.StatusNotFound {
			t.Fatalf("status %d after DELETE, want 404:\n%s", w.Code, w.Body)
		}
	})
	t.Run("2025-11-25/streamable-http/delete-without-session-is-400", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/mcp", nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", w.Code)
		}
	})
	t.Run("2025-11-25/streamable-http/delete-of-unknown-session-is-404", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
		r.Header.Set("Mcp-Session-Id", "sess-unknown")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404", w.Code)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#protocol-version-header
	for _, bad := range []string{"garbage", "2024-10-07", "2025-01-01"} {
		t.Run("2025-11-25/streamable-http/unsupported-protocol-version-header-is-400/"+bad, func(t *testing.T) {
			w := post(s, frame(2, "tools/list", nil), map[string]string{"MCP-Protocol-Version": bad})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400:\n%s", w.Code, w.Body)
			}
		})
	}
	t.Run("2025-11-25/streamable-http/supported-protocol-version-header-accepted", func(t *testing.T) {
		w := post(s, frame(2, "tools/list", nil), map[string]string{"MCP-Protocol-Version": "2025-06-18"})
		if w.Code != http.StatusOK {
			t.Fatalf("status %d:\n%s", w.Code, w.Body)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#sending-messages-to-the-server
	t.Run("2025-11-25/streamable-http/accepted-notification-is-202-no-body", func(t *testing.T) {
		id := legacySession(t, s, "2025-11-25")
		w := post(s, frame(nil, "notifications/initialized", nil), map[string]string{"Mcp-Session-Id": id})
		if w.Code != http.StatusAccepted || w.Body.Len() != 0 {
			t.Fatalf("status %d body %q", w.Code, w.Body)
		}
	})
	t.Run("2025-11-25/streamable-http/unknown-notification-is-ignored-202", func(t *testing.T) {
		w := post(s, frame(nil, "notifications/whatever", nil), nil)
		if w.Code != http.StatusAccepted || w.Body.Len() != 0 {
			t.Fatalf("status %d body %q", w.Code, w.Body)
		}
	})
	t.Run("2025-11-25/streamable-http/response-nobody-asked-for-is-400", func(t *testing.T) {
		id := legacySession(t, s, "2025-11-25")
		w := post(s, []byte(`{"jsonrpc":"2.0","id":-99,"result":{}}`), map[string]string{"Mcp-Session-Id": id})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", w.Code)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#listening-for-messages-from-the-server
	t.Run("2025-11-25/streamable-http/get-without-session-is-405", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/mcp", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status %d, want 405", w.Code)
		}
	})
	t.Run("2025-11-25/streamable-http/get-with-unknown-session-is-404", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		r.Header.Set("Mcp-Session-Id", "sess-unknown")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404", w.Code)
		}
	})
}

func TestLegacyBatches(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	batch := func(parts ...[]byte) []byte {
		var ss []string
		for _, p := range parts {
			ss = append(ss, string(p))
		}
		return []byte("[" + strings.Join(ss, ",") + "]")
	}
	replies := func(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
		t.Helper()
		var out []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("status %d, not a batch reply: %v\n%s", w.Code, err, w.Body)
		}
		return out
	}

	// https://modelcontextprotocol.io/specification/2025-03-26/basic#batching
	t.Run("2025-03-26/streamable-http/batch-accepted-in-a-2025-03-26-session", func(t *testing.T) {
		id := legacySession(t, s, "2025-03-26")
		w := post(s, batch(frame(1, "ping", nil), frame(2, "tools/list", nil),
			frame(nil, "notifications/initialized", nil)), map[string]string{"Mcp-Session-Id": id})
		got := replies(t, w)
		if w.Code != http.StatusOK || len(got) != 2 {
			t.Fatalf("status %d, want one reply per request:\n%s", w.Code, w.Body)
		}
	})
	t.Run("2025-03-26/streamable-http/header-absent-assumes-2025-03-26", func(t *testing.T) {
		w := post(s, batch(frame(1, "ping", nil)), nil)
		if got := replies(t, w); len(got) != 1 {
			t.Fatalf("without a header the revision is 2025-03-26, which batches:\n%s", w.Body)
		}
	})
	t.Run("2025-03-26/streamable-http/batch-of-notifications-only-is-202", func(t *testing.T) {
		w := post(s, batch(frame(nil, "notifications/initialized", nil)), nil)
		if w.Code != http.StatusAccepted || w.Body.Len() != 0 {
			t.Fatalf("status %d body %q", w.Code, w.Body)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-03-26/basic/lifecycle#initialization
	t.Run("2025-03-26/lifecycle/initialize-not-in-a-batch", func(t *testing.T) {
		w := post(s, batch(frame(1, "initialize", map[string]any{"protocolVersion": "2025-03-26"})), nil)
		got := replies(t, w)
		if len(got) != 1 || got[0]["error"] == nil {
			t.Fatalf("initialize in a batch should be refused:\n%s", w.Body)
		}
		if w.Header().Get("Mcp-Session-Id") != "" {
			t.Error("a refused initialize must not mint a session")
		}
	})
	// https://modelcontextprotocol.io/specification/2025-06-18/changelog
	t.Run("2025-06-18/streamable-http/batch-rejected-after-2025-06-18", func(t *testing.T) {
		id := legacySession(t, s, "2025-06-18")
		w := post(s, batch(frame(1, "ping", nil)), map[string]string{"Mcp-Session-Id": id})
		if code, _ := rpcErr(t, w.Body.Bytes()); w.Code != http.StatusBadRequest || code != -32600 {
			t.Fatalf("status %d code %d, want 400 -32600:\n%s", w.Code, code, w.Body)
		}
	})
	t.Run("2025-11-25/streamable-http/batch-rejected-by-header", func(t *testing.T) {
		w := post(s, batch(frame(1, "ping", nil)), map[string]string{"MCP-Protocol-Version": "2025-11-25"})
		if code, _ := rpcErr(t, w.Body.Bytes()); w.Code != http.StatusBadRequest || code != -32600 {
			t.Fatalf("status %d code %d, want 400 -32600:\n%s", w.Code, code, w.Body)
		}
	})
}

// sseReader reads a GET stream line by line.
type sseReader struct {
	lines chan string
}

func openGET(t *testing.T, url, session string, ctx context.Context) (*http.Response, *sseReader) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r := &sseReader{lines: make(chan string, 64)}
	go func() {
		defer close(r.lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			r.lines <- sc.Text()
		}
	}()
	return resp, r
}

func (r *sseReader) waitFor(t *testing.T, pred func(string) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case l, ok := <-r.lines:
			if !ok {
				t.Fatal("the stream ended first")
			}
			if pred(l) {
				return
			}
		case <-deadline:
			t.Fatal("never arrived")
		}
	}
}

func (r *sseReader) waitClosed(t *testing.T) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-r.lines:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the stream stayed open")
		}
	}
}

func TestLegacyGETStream(t *testing.T) {
	// A server apiece, so a notifier left over from one case cannot take
	// the notification another case fires.
	var (
		n  *fakeNotifier
		s  *mcpserver.Server
		ts *httptest.Server
	)
	fresh := func(t *testing.T) {
		n = &fakeNotifier{got: make(chan mcpserver.ListenFilter, 4), fire: make(chan [2]any, 4)}
		s = mcpserver.New(newBackend(), "mcpx", "test")
		s.Notify = n
		s.Timing.SSEKeepAlive = 20 * time.Millisecond
		ts = httptest.NewServer(s)
		t.Cleanup(ts.Close)
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#listening-for-messages-from-the-server
	t.Run("2025-11-25/streamable-http/get-returns-event-stream-carrying-list-changed", func(t *testing.T) {
		fresh(t)
		id := legacySession(t, s, "2025-11-25")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		resp, r := openGET(t, ts.URL, id, ctx)
		defer resp.Body.Close()
		if ct := resp.Header.Get("Content-Type"); resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "text/event-stream") {
			t.Fatalf("status %d type %q", resp.StatusCode, ct)
		}
		if resp.Header.Get("X-Accel-Buffering") != "no" {
			t.Error("an event stream should disable proxy buffering")
		}
		select {
		case lf := <-n.got:
			if lf.ToolsListChanged || !lf.PromptsListChanged || !lf.ResourcesListChanged {
				t.Errorf("a legacy client is sent every list_changed it was promised: %+v", lf)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("opening the stream should start forwarding list_changed")
		}
		n.fire <- [2]any{"notifications/prompts/list_changed", map[string]any{}}
		r.waitFor(t, func(l string) bool { return strings.Contains(l, "notifications/prompts/list_changed") })
	})
	t.Run("2025-11-25/streamable-http/session-declares-what-the-get-stream-delivers", func(t *testing.T) {
		fresh(t)
		w := post(s, frame(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"}), nil)
		if !strings.Contains(w.Body.String(), `"listChanged":true`) ||
			!strings.Contains(w.Body.String(), `"subscribe":true`) {
			t.Fatalf("a session can open a GET stream, so it can be pushed to:\n%s", w.Body)
		}
	})
	t.Run("2025-11-25/streamable-http/get-stream-keep-alive-comments", func(t *testing.T) {
		fresh(t)
		id := legacySession(t, s, "2025-11-25")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		resp, r := openGET(t, ts.URL, id, ctx)
		defer resp.Body.Close()
		listened(t, n)
		r.waitFor(t, func(l string) bool { return strings.HasPrefix(l, ":") })
	})
	t.Run("2025-11-25/streamable-http/one-get-stream-per-session", func(t *testing.T) {
		fresh(t)
		id := legacySession(t, s, "2025-11-25")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		first, _ := openGET(t, ts.URL, id, ctx)
		defer first.Body.Close()
		listened(t, n)
		second, _ := openGET(t, ts.URL, id, ctx)
		defer second.Body.Close()
		if second.StatusCode != http.StatusConflict {
			t.Fatalf("status %d, want 409", second.StatusCode)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#session-management
	t.Run("2025-11-25/streamable-http/delete-ends-the-get-stream", func(t *testing.T) {
		fresh(t)
		id := legacySession(t, s, "2025-11-25")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		resp, r := openGET(t, ts.URL, id, ctx)
		defer resp.Body.Close()
		listened(t, n)
		req, _ := http.NewRequest(http.MethodDelete, ts.URL, nil)
		req.Header.Set("Mcp-Session-Id", id)
		dr, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		dr.Body.Close()
		r.waitClosed(t)
	})
	t.Run("2025-11-25/streamable-http/resource-subscription-delivered-on-get-stream", func(t *testing.T) {
		fresh(t)
		id := legacySession(t, s, "2025-11-25")
		w := post(s, frame(2, "resources/subscribe", map[string]any{"uri": "demo://a"}),
			map[string]string{"Mcp-Session-Id": id})
		if w.Code != http.StatusOK {
			t.Fatalf("subscribe: %d %s", w.Code, w.Body)
		}
		listened(t, n) // the subscription's own listen
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		resp, r := openGET(t, ts.URL, id, ctx)
		defer resp.Body.Close()
		listened(t, n) // the list_changed forwarding
		n.fire <- [2]any{"notifications/resources/updated", map[string]any{"uri": "demo://a"}}
		r.waitFor(t, func(l string) bool { return strings.Contains(l, "notifications/resources/updated") })
	})
}

// listened waits for the notifier to be started, and fails rather than hangs.
func listened(t *testing.T, n *fakeNotifier) mcpserver.ListenFilter {
	t.Helper()
	select {
	case lf := <-n.got:
		return lf
	case <-time.After(5 * time.Second):
		t.Fatal("no notification stream was started")
	}
	return mcpserver.ListenFilter{}
}

func TestLegacyHTTPCancellation(t *testing.T) {
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#sending-messages-to-the-server
	// "Disconnection SHOULD NOT be interpreted as the client cancelling its
	// request. To cancel, the client SHOULD explicitly send an MCP
	// CancelledNotification."
	t.Run("2025-11-25/streamable-http/disconnect-is-not-cancellation-cancelled-notification-is", func(t *testing.T) {
		b := newBlocking()
		s := mcpserver.New(b, "mcpx", "test")
		ts := httptest.NewServer(s)
		defer ts.Close()
		id := legacySession(t, s, "2025-11-25")

		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL,
			strings.NewReader(string(frame(9, "tools/call", callParams(nil)))))
		req.Header.Set("Mcp-Session-Id", id)
		go func() {
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}()
		b.waitStarted(t)
		cancel()
		select {
		case <-b.ended:
			t.Fatal("a dropped connection cancelled the call")
		case <-time.After(300 * time.Millisecond):
		}
		w := post(s, frame(nil, "notifications/cancelled", map[string]any{"requestId": 9}),
			map[string]string{"Mcp-Session-Id": id})
		if w.Code != http.StatusAccepted {
			t.Fatalf("status %d", w.Code)
		}
		b.waitEnded(t)
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#earlier-streamable-http-revisions
	// "An Mcp-Session-Id header on a request: ignore it."
	t.Run("2026-07-28/streamable-http/modern-request-not-bound-to-a-legacy-session", func(t *testing.T) {
		b := newBlocking()
		s := mcpserver.New(b, "mcpx", "test")
		ts := httptest.NewServer(s)
		defer ts.Close()
		id := legacySession(t, s, "2025-11-25")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL,
			strings.NewReader(string(frame(9, "tools/call", callParams(nil)))))
		req.Header.Set("Mcp-Session-Id", id)
		go func() {
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}()
		b.waitStarted(t)
		// A modern notification naming the legacy session reaches nothing
		// in it: were it bound, this would cancel the legacy call.
		h := modernHeaders("notifications/cancelled", "")
		h["Mcp-Session-Id"] = id
		post(s, frame(nil, "notifications/cancelled", modernParams(map[string]any{"requestId": 9})), h)
		select {
		case <-b.ended:
			t.Fatal("a modern request was bound to a legacy session's connection")
		case <-time.After(300 * time.Millisecond):
		}
		post(s, frame(nil, "notifications/cancelled", map[string]any{"requestId": 9}),
			map[string]string{"Mcp-Session-Id": id})
		b.waitEnded(t)
	})
}

func TestOriginValidation(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Origins = mcpserver.OriginPolicy{Hosts: []string{"localhost", "127.0.0.1", "::1", "10.1.2.3"},
		Origins: []string{"https://app.example.com"}}
	for _, rev := range []string{"2025-03-26", "2025-11-25", "2026-07-28"} {
		// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#security-warning
		// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#security-%26-endpoint
		// tools/list: ping is gone from 2026-07-28, and this is a test about
		// Origin, not about which methods survive.
		body, headers := frame(1, "tools/list", nil), map[string]string{}
		if rev == modern {
			body, headers = frame(1, "tools/list", modernParams(nil)), modernHeaders("tools/list", "")
		}
		with := func(origin string) *httptest.ResponseRecorder {
			h := map[string]string{"Origin": origin}
			for k, v := range headers {
				h[k] = v
			}
			if origin == "" {
				delete(h, "Origin")
			}
			return post(s, body, h)
		}
		t.Run(rev+"/streamable-http/invalid-origin-is-403", func(t *testing.T) {
			for _, o := range []string{"https://evil.example", "http://localhost.evil.example", "null", "file://"} {
				w := with(o)
				if w.Code != http.StatusForbidden {
					t.Errorf("Origin %q: status %d, want 403", o, w.Code)
				}
				if _, id := rpcErr(t, w.Body.Bytes()); len(id) != 0 {
					t.Errorf("the 403 body is an error with no id, got %s", id)
				}
			}
		})
		t.Run(rev+"/streamable-http/allowed-origins-served", func(t *testing.T) {
			for _, o := range []string{"", "http://localhost:3000", "https://127.0.0.1",
				"http://[::1]:9", "http://10.1.2.3:4444", "https://app.example.com"} {
				if w := with(o); w.Code != http.StatusOK {
					t.Errorf("Origin %q: status %d:\n%s", o, w.Code, w.Body)
				}
			}
		})
	}
	t.Run("2025-11-25/streamable-http/invalid-origin-is-403-on-get-and-delete", func(t *testing.T) {
		for _, m := range []string{http.MethodGet, http.MethodDelete} {
			r := httptest.NewRequest(m, "/mcp", nil)
			r.Header.Set("Origin", "https://evil.example")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Errorf("%s: status %d, want 403", m, w.Code)
			}
		}
	})
	t.Run("2025-11-25/streamable-http/default-policy-is-loopback", func(t *testing.T) {
		d := mcpserver.New(newBackend(), "mcpx", "test")
		if w := post(d, frame(1, "ping", nil), map[string]string{"Origin": "http://localhost:1"}); w.Code != http.StatusOK {
			t.Errorf("loopback: %d", w.Code)
		}
		if w := post(d, frame(1, "ping", nil), map[string]string{"Origin": "http://10.1.2.3"}); w.Code != http.StatusForbidden {
			t.Errorf("non-loopback: %d", w.Code)
		}
	})
}

// stdioSession drives ServeStdio over pipes and collects its output by id.
type stdioSession struct {
	in     *io.PipeWriter
	frames chan map[string]any
	done   chan error
	raw    []string
	mu     sync.Mutex
}

func startStdio(t *testing.T, s *mcpserver.Server) *stdioSession {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ss := &stdioSession{in: inW, frames: make(chan map[string]any, 64), done: make(chan error, 1)}
	go func() {
		err := s.ServeStdio(context.Background(), inR, outW)
		outW.Close()
		ss.done <- err
	}()
	go func() {
		defer close(ss.frames)
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			ss.mu.Lock()
			ss.raw = append(ss.raw, sc.Text())
			ss.mu.Unlock()
			var anyFrame any
			if err := json.Unmarshal(sc.Bytes(), &anyFrame); err != nil {
				t.Errorf("stdout carried a line that is not JSON: %q", sc.Text())
				continue
			}
			if m, ok := anyFrame.(map[string]any); ok {
				ss.frames <- m
			} else {
				ss.frames <- map[string]any{"batch": anyFrame}
			}
		}
	}()
	t.Cleanup(func() { inW.Close() })
	return ss
}

// send writes one line, and fails rather than hangs when the server has
// stopped reading -- which is what a server stuck on one request does.
func (ss *stdioSession) send(t *testing.T, b []byte) {
	t.Helper()
	errc := make(chan error, 1)
	go func() {
		_, err := ss.in.Write(append(b, '\n'))
		errc <- err
	}()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server stopped reading its input")
	}
}

func (ss *stdioSession) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case f, ok := <-ss.frames:
		if !ok {
			t.Fatal("stdout closed")
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame")
	}
	return nil
}

// closeAndWait closes stdin and returns every frame still to come.
func (ss *stdioSession) closeAndWait(t *testing.T, within time.Duration) []map[string]any {
	t.Helper()
	ss.in.Close()
	var rest []map[string]any
	deadline := time.After(within)
	for {
		select {
		case f, ok := <-ss.frames:
			if !ok {
				select {
				case <-ss.done:
				case <-deadline:
					t.Fatal("ServeStdio did not return")
				}
				return rest
			}
			rest = append(rest, f)
		case <-deadline:
			t.Fatalf("ServeStdio did not exit within %s of EOF", within)
		}
	}
}

func TestStdioTransport(t *testing.T) {
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#cancellation
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation
	t.Run("2026-07-28/stdio/cancelled-request-stops-and-gets-no-response", func(t *testing.T) {
		b := newBlocking()
		ss := startStdio(t, mcpserver.New(b, "mcpx", "test"))
		ss.send(t, frame(5, "tools/call", callParams(map[string]any{"_meta": modernMeta})))
		b.waitStarted(t)
		ss.send(t, frame(nil, "notifications/cancelled", modernParams(map[string]any{"requestId": 5})))
		b.waitEnded(t)
		ss.send(t, frame(6, "ping", nil))
		if f := ss.next(t); f["id"] != float64(6) {
			t.Fatalf("the cancelled request must not be answered; got %v", f)
		}
		for _, f := range ss.closeAndWait(t, 5*time.Second) {
			if f["id"] == float64(5) {
				t.Fatalf("the cancelled request was answered: %v", f)
			}
		}
	})
	t.Run("2025-11-25/stdio/cancellation-reaches-backend-call", func(t *testing.T) {
		b := newBlocking()
		ss := startStdio(t, mcpserver.New(b, "mcpx", "test"))
		ss.send(t, frame(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"}))
		ss.next(t)
		ss.send(t, frame("abc", "tools/call", callParams(nil)))
		b.waitStarted(t)
		ss.send(t, frame(nil, "notifications/cancelled", map[string]any{"requestId": "abc", "reason": "user"}))
		b.waitEnded(t)
		for _, f := range ss.closeAndWait(t, 5*time.Second) {
			if f["id"] == "abc" {
				t.Fatalf("the cancelled request was answered: %v", f)
			}
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#shutdown
	t.Run("2026-07-28/stdio/eof-exits-promptly-cancelling-in-flight-work", func(t *testing.T) {
		b := newBlocking()
		s := mcpserver.New(b, "mcpx", "test")
		s.Timing.StdioDrain = 50 * time.Millisecond
		ss := startStdio(t, s)
		ss.send(t, frame(1, "tools/call", callParams(nil)))
		b.waitStarted(t)
		ss.closeAndWait(t, 3*time.Second)
		b.waitEnded(t)
	})
	t.Run("2026-07-28/stdio/eof-still-answers-finished-work", func(t *testing.T) {
		ss := startStdio(t, mcpserver.New(newBackend(), "mcpx", "test"))
		ss.send(t, frame(1, "tools/list", nil))
		ss.send(t, frame(2, "tools/call", map[string]any{"name": "mcpx_status"}))
		rest := ss.closeAndWait(t, 5*time.Second)
		if len(rest) != 2 {
			t.Fatalf("requests sent before EOF should still be answered, got %v", rest)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#operation
	t.Run("2025-11-25/stdio/ping-before-initialize-is-answered", func(t *testing.T) {
		ss := startStdio(t, mcpserver.New(newBackend(), "mcpx", "test"))
		ss.send(t, frame(1, "ping", nil))
		if f := ss.next(t); f["id"] != float64(1) || f["error"] != nil {
			t.Fatalf("got %v", f)
		}
	})
	// https://www.jsonrpc.org/specification#notification
	t.Run("2025-11-25/stdio/notification-is-never-answered", func(t *testing.T) {
		ss := startStdio(t, mcpserver.New(newBackend(), "mcpx", "test"))
		ss.send(t, frame(nil, "notifications/nonsense", nil))
		ss.send(t, frame(2, "ping", nil))
		if f := ss.next(t); f["id"] != float64(2) {
			t.Fatalf("a notification got a reply: %v", f)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-03-26/basic#batching
	t.Run("2025-03-26/stdio/batch-answered-with-an-array", func(t *testing.T) {
		ss := startStdio(t, mcpserver.New(newBackend(), "mcpx", "test"))
		ss.send(t, frame(1, "initialize", map[string]any{"protocolVersion": "2025-03-26"}))
		ss.next(t)
		ss.send(t, []byte("["+string(frame(2, "ping", nil))+","+string(frame(3, "tools/list", nil))+
			","+string(frame(nil, "notifications/initialized", nil))+"]"))
		f := ss.next(t)
		arr, _ := f["batch"].([]any)
		if len(arr) != 2 {
			t.Fatalf("a batch of two requests and a notification gets two replies in one array: %v", f)
		}
	})
	t.Run("2025-03-26/stdio/batch-of-notifications-gets-nothing", func(t *testing.T) {
		ss := startStdio(t, mcpserver.New(newBackend(), "mcpx", "test"))
		ss.send(t, []byte("["+string(frame(nil, "notifications/initialized", nil))+"]"))
		ss.send(t, frame(2, "ping", nil))
		if f := ss.next(t); f["id"] != float64(2) {
			t.Fatalf("a batch of notifications got a reply: %v", f)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-03-26/basic/lifecycle#initialization
	t.Run("2025-03-26/lifecycle/initialize-in-a-batch-refused-on-stdio", func(t *testing.T) {
		ss := startStdio(t, mcpserver.New(newBackend(), "mcpx", "test"))
		ss.send(t, []byte("["+string(frame(1, "initialize", map[string]any{"protocolVersion": "2025-03-26"}))+"]"))
		arr, _ := ss.next(t)["batch"].([]any)
		if len(arr) != 1 {
			t.Fatalf("got %v", arr)
		}
		if e, _ := arr[0].(map[string]any); e["error"] == nil {
			t.Fatalf("initialize in a batch should be an error: %v", e)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-06-18/changelog
	t.Run("2025-06-18/stdio/batch-rejected-after-2025-06-18", func(t *testing.T) {
		ss := startStdio(t, mcpserver.New(newBackend(), "mcpx", "test"))
		ss.send(t, frame(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
		ss.next(t)
		ss.send(t, []byte("["+string(frame(2, "ping", nil))+"]"))
		f := ss.next(t)
		e, _ := f["error"].(map[string]any)
		if e == nil || e["code"] != float64(-32600) {
			t.Fatalf("want one -32600 error, got %v", f)
		}
	})
	// Every revision's stdio page: messages are newline-delimited and MUST
	// NOT contain embedded newlines.
	t.Run("2025-03-26/stdio/every-frame-is-one-line", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test").WithExtras([]mcpserver.Extra{{
			Tool: mcpserver.Tool{Name: "lines", InputSchema: json.RawMessage(`{"type":"object"}`)},
			Call: func(context.Context, json.RawMessage) (string, error) { return "a\nb\r\nc", nil },
		}})
		ss := startStdio(t, s)
		ss.send(t, frame(1, "tools/call", map[string]any{"name": "lines"}))
		f := ss.next(t)
		if !strings.Contains(dump(f), `a\nb`) {
			t.Fatalf("the newline should survive, escaped: %v", f)
		}
		ss.mu.Lock()
		n := len(ss.raw)
		ss.mu.Unlock()
		if n != 1 {
			t.Fatalf("one message should be one line, got %d", n)
		}
	})
	t.Run("2025-11-25/stdio/initialize-starts-list-changed-forwarding", func(t *testing.T) {
		n := &fakeNotifier{got: make(chan mcpserver.ListenFilter, 1), fire: make(chan [2]any, 1)}
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Notify = n
		ss := startStdio(t, s)
		ss.send(t, frame(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"}))
		ss.next(t)
		select {
		case lf := <-n.got:
			if !lf.PromptsListChanged || lf.ToolsListChanged {
				t.Fatalf("filter %+v", lf)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a legacy stdio client declared listChanged and is never sent one")
		}
		n.fire <- [2]any{"notifications/prompts/list_changed", map[string]any{}}
		if f := ss.next(t); f["method"] != "notifications/prompts/list_changed" {
			t.Fatalf("got %v", f)
		}
	})
}

func dump(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#server-behavior-for-custom-headers
//
// The cases are the official suite's http-custom-header-server-validation
// scenario, plus the integer and null rows of the specification's table.
func TestModernParamHeaders(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test").WithExtras([]mcpserver.Extra{{
		Tool: mcpserver.Tool{Name: "echo_region", InputSchema: json.RawMessage(`{"type":"object","properties":{
			"region":{"type":"string","x-mcp-header":"Region"},
			"n":{"type":"integer","x-mcp-header":"N"}}}`)},
		Call: func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
	}})
	send := func(args map[string]any, params map[string]string) *httptest.ResponseRecorder {
		h := modernHeaders("tools/call", "echo_region")
		for k, v := range params {
			h[k] = v
		}
		return post(s, frame(7, "tools/call", modernParams(map[string]any{"name": "echo_region", "arguments": args})), h)
	}
	accept := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		if code, _ := rpcErr(t, w.Body.Bytes()); w.Code != http.StatusOK || code != 0 {
			t.Fatalf("status %d code %d, want 200 and a result:\n%s", w.Code, code, w.Body)
		}
	}
	reject := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		if code, _ := rpcErr(t, w.Body.Bytes()); w.Code != http.StatusBadRequest || code != -32020 {
			t.Fatalf("status %d code %d, want 400 -32020:\n%s", w.Code, code, w.Body)
		}
	}
	hello := map[string]any{"region": "Hello"}
	t.Run("2026-07-28/streamable-http/param-header-matching-body-accepted", func(t *testing.T) {
		accept(t, send(hello, map[string]string{"Mcp-Param-Region": "Hello"}))
	})
	t.Run("2026-07-28/streamable-http/param-header-base64-decoded", func(t *testing.T) {
		accept(t, send(hello, map[string]string{"Mcp-Param-Region": "=?base64?SGVsbG8=?="}))
	})
	t.Run("2026-07-28/streamable-http/param-header-invalid-base64-padding-rejected", func(t *testing.T) {
		reject(t, send(hello, map[string]string{"Mcp-Param-Region": "=?base64?SGVsbG8?="}))
	})
	t.Run("2026-07-28/streamable-http/param-header-invalid-base64-chars-rejected", func(t *testing.T) {
		reject(t, send(hello, map[string]string{"Mcp-Param-Region": "=?base64?SGVs!!!bG8=?="}))
	})
	t.Run("2026-07-28/streamable-http/param-header-without-sentinel-is-literal", func(t *testing.T) {
		accept(t, send(map[string]any{"region": "SGVsbG8="}, map[string]string{"Mcp-Param-Region": "SGVsbG8="}))
		accept(t, send(map[string]any{"region": "=?base64?SGVsbG8="}, map[string]string{"Mcp-Param-Region": "=?base64?SGVsbG8="}))
	})
	t.Run("2026-07-28/streamable-http/param-header-mismatch-rejected", func(t *testing.T) {
		reject(t, send(hello, map[string]string{"Mcp-Param-Region": "Goodbye"}))
	})
	t.Run("2026-07-28/streamable-http/param-header-missing-with-body-value-rejected", func(t *testing.T) {
		reject(t, send(hello, nil))
	})
	t.Run("2026-07-28/streamable-http/param-header-invalid-characters-rejected", func(t *testing.T) {
		reject(t, send(map[string]any{"region": "H\xe4llo"}, map[string]string{"Mcp-Param-Region": "H\xe4llo"}))
	})
	t.Run("2026-07-28/streamable-http/param-header-not-expected-for-null-or-absent", func(t *testing.T) {
		accept(t, send(map[string]any{"region": nil}, nil))
		accept(t, send(map[string]any{}, nil))
		reject(t, send(map[string]any{}, map[string]string{"Mcp-Param-Region": "Hello"}))
	})
	t.Run("2026-07-28/streamable-http/param-header-integer-compared-numerically", func(t *testing.T) {
		accept(t, send(map[string]any{"n": 42}, map[string]string{"Mcp-Param-N": "42.0"}))
		reject(t, send(map[string]any{"n": 42}, map[string]string{"Mcp-Param-N": "43"}))
	})
	t.Run("2026-07-28/streamable-http/param-header-name-case-insensitive", func(t *testing.T) {
		accept(t, send(hello, map[string]string{"mcp-param-region": "Hello"}))
	})
}
