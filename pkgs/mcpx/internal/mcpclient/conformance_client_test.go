package mcpclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpspec"
)

// wire is a scripted server that checks every frame the client sends against
// the schema of the revision in use, in strict mode: "send conservatively"
// means a client sends only what that revision defines, and strict mode is
// the only check that can see a key borrowed from another revision.
type wire struct {
	t *testing.T
	// rev is the revision legacy frames are checked against; a modern frame
	// is checked against the version in its own _meta.
	rev string

	mu     sync.Mutex
	sent   []map[string]any
	in     chan []byte
	closed bool
	// asked maps the ids of requests pushed to the client to their methods,
	// so the client's replies can be validated as the right result.
	asked map[string]string
	// handle answers one request: a result, an error, or (nil, nil) for
	// silence.
	handle func(method string, params map[string]any) (any, map[string]any)
}

func newWire(t *testing.T, rev string, handle func(string, map[string]any) (any, map[string]any)) *wire {
	return &wire{t: t, rev: rev, in: make(chan []byte, 256), asked: map[string]string{}, handle: handle}
}

func (w *wire) Send(_ context.Context, msg []byte) error {
	var f map[string]any
	if err := json.Unmarshal(msg, &f); err != nil {
		w.t.Errorf("client sent invalid JSON: %s", msg)
		return nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return io.ErrClosedPipe
	}
	w.sent = append(w.sent, f)
	rev := w.rev
	params, _ := f["params"].(map[string]any)
	meta, _ := params["_meta"].(map[string]any)
	if v, _ := meta[mcpclient.MetaProtocolVersion].(string); v != "" {
		rev = v
	}
	if f["method"] == "initialize" {
		// initialize is written in the revision it offers, before any
		// other is agreed.
		rev, _ = params["protocolVersion"].(string)
	}
	reqMethod := ""
	if f["method"] == nil && f["id"] != nil {
		reqMethod = w.asked[jsonKey(f["id"])]
	}
	w.mu.Unlock()
	if err := mcpspec.ValidateClientMessageStrict(rev, msg, reqMethod); err != nil {
		var ext *mcpspec.ErrExtension
		if !errors.As(err, &ext) {
			w.t.Errorf("client frame is not valid %s: %v\n%s", rev, err, msg)
		}
	}
	// _meta is an open object in every schema, so strict mode does not look
	// inside it; the capabilities a modern request declares there are
	// checked against the revision's ClientCapabilities on their own.
	if caps, ok := meta[mcpclient.MetaClientCapabilities]; ok {
		b, _ := json.Marshal(caps)
		if err := mcpspec.ValidateStrict(rev, "ClientCapabilities", b); err != nil {
			w.t.Errorf("declared capabilities are not valid %s: %v\n%s", rev, err, b)
		}
	}
	method, _ := f["method"].(string)
	if method == "" || f["id"] == nil {
		return nil
	}
	res, rpcErr := w.handle(method, params)
	if res == nil && rpcErr == nil {
		return nil
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": f["id"]}
	if rpcErr != nil {
		reply["error"] = rpcErr
	} else {
		reply["result"] = res
	}
	w.push(reply)
	return nil
}

func (w *wire) push(v any) {
	b, _ := json.Marshal(v)
	if m, ok := v.(map[string]any); ok && m["method"] != nil && m["id"] != nil {
		w.mu.Lock()
		w.asked[jsonKey(m["id"])] = m["method"].(string)
		w.mu.Unlock()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.in <- b
	}
}

func (w *wire) pushRaw(b string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.in <- []byte(b)
	}
}

func (w *wire) Recv() ([]byte, error) {
	b, ok := <-w.in
	if !ok {
		return nil, io.EOF
	}
	return b, nil
}

func (w *wire) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		close(w.in)
	}
	return nil
}

func (w *wire) Info() string { return "wire" }

func jsonKey(v any) string { b, _ := json.Marshal(v); return string(b) }

// frames returns what the client sent with the given method ("" for replies).
func (w *wire) frames(method string) []map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []map[string]any
	for _, f := range w.sent {
		m, _ := f["method"].(string)
		if m == method {
			out = append(out, f)
		}
	}
	return out
}

// waitFor polls until cond holds or fails the test.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func reply(w *wire, id any) map[string]any {
	for _, f := range w.frames("") {
		if jsonKey(f["id"]) == jsonKey(id) {
			return f
		}
	}
	return nil
}

func initResult(version string, caps map[string]any) map[string]any {
	if caps == nil {
		caps = map[string]any{}
	}
	return map[string]any{"protocolVersion": version, "capabilities": caps,
		"serverInfo": map[string]any{"name": "wire", "version": "1"}}
}

func discoverWith(caps map[string]any) map[string]any {
	if caps == nil {
		caps = map[string]any{}
	}
	return map[string]any{"resultType": "complete", "supportedVersions": []string{"2026-07-28"},
		"capabilities": caps, "ttlMs": 0, "cacheScope": "private",
		"_meta": map[string]any{"io.modelcontextprotocol/serverInfo": map[string]any{"name": "wire", "version": "1"}}}
}

func dialWire(t *testing.T, w *wire, pref mcpclient.Preference, o mcpclient.Options) *mcpclient.Client {
	t.Helper()
	c, err := dialWireErr(w, pref, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func dialWireErr(w *wire, pref mcpclient.Preference, o mcpclient.Options) (*mcpclient.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.ClientName, o.ClientVersion, o.Preference = "mcpx", "test", pref
	return mcpclient.NewWithOptions(ctx, w, o)
}

func ctx5() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = cancel
	return ctx
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#resulttype
func TestResultType(t *testing.T) {
	answer := ""
	w := newWire(t, "2026-07-28", func(m string, p map[string]any) (any, map[string]any) {
		switch m {
		case "server/discover":
			return discoverWith(map[string]any{"tools": map[string]any{}}), nil
		case "tools/call":
			r := map[string]any{"content": []any{}}
			if answer != "" {
				r["resultType"] = answer
			}
			return r, nil
		}
		return nil, nil
	})
	c := dialWire(t, w, mcpclient.ForceModern, mcpclient.Options{})
	t.Run("2026-07-28/messages/resulttype-absent-means-complete", func(t *testing.T) {
		answer = ""
		if _, err := c.CallTool(ctx5(), "x", nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("2026-07-28/messages/resulttype-unknown-invalid", func(t *testing.T) {
		answer = "partial"
		_, err := c.CallTool(ctx5(), "x", nil)
		var ie *mcpclient.InvalidResultError
		if !errors.As(err, &ie) || ie.ResultType != "partial" {
			t.Fatalf("an unrecognised resultType must be invalid, got %v", err)
		}
	})
}

// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#version-negotiation
func TestLegacyLifecycle(t *testing.T) {
	t.Run("2025-11-25/lifecycle/client-disconnects-on-unsupported-version", func(t *testing.T) {
		w := newWire(t, "2025-11-25", func(m string, p map[string]any) (any, map[string]any) {
			if m == "initialize" {
				return initResult("2099-01-01", nil), nil
			}
			return nil, nil
		})
		_, err := dialWireErr(w, mcpclient.ForceLegacy, mcpclient.Options{})
		var ue *mcpclient.UnsupportedVersionError
		if !errors.As(err, &ue) || ue.Version != "2099-01-01" {
			t.Fatalf("want UnsupportedVersionError, got %v", err)
		}
		if len(w.frames("notifications/initialized")) != 0 {
			t.Error("initialized was sent for a version mcpx does not speak")
		}
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if !closed {
			t.Error("the connection was left open")
		}
	})
	t.Run("2025-06-18/lifecycle/client-adopts-an-older-negotiated-version", func(t *testing.T) {
		for _, v := range mcpclient.LegacyVersions {
			w := newWire(t, v, func(m string, p map[string]any) (any, map[string]any) {
				if m == "initialize" {
					return initResult(v, nil), nil
				}
				return nil, nil
			})
			c := dialWire(t, w, mcpclient.ForceLegacy, mcpclient.Options{})
			if c.Negotiated != v {
				t.Errorf("negotiated %q, want %q", c.Negotiated, v)
			}
		}
	})
	// "The client MUST NOT send a cancellation for the initialize request"
	// is in every legacy cancellation page.
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation#behavior-requirements
	t.Run("2025-11-25/cancellation/initialize-never-cancelled", func(t *testing.T) {
		w := newWire(t, "2025-11-25", func(string, map[string]any) (any, map[string]any) { return nil, nil })
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := mcpclient.NewWithOptions(ctx, w, mcpclient.Options{ClientName: "mcpx", ClientVersion: "t",
			Preference: mcpclient.ForceLegacy})
		if err == nil {
			t.Fatal("an unanswered initialize succeeded")
		}
		time.Sleep(50 * time.Millisecond)
		if n := len(w.frames("notifications/cancelled")); n != 0 {
			t.Fatalf("sent %d notifications/cancelled for initialize", n)
		}
	})
	t.Run("2025-11-25/cancellation/timed-out-request-is-cancelled", func(t *testing.T) {
		w := newWire(t, "2025-11-25", func(m string, p map[string]any) (any, map[string]any) {
			if m == "initialize" {
				return initResult("2025-11-25", nil), nil
			}
			return nil, nil
		})
		c := dialWire(t, w, mcpclient.ForceLegacy, mcpclient.Options{})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, _ = c.CallTool(ctx, "slow", nil)
		waitFor(t, "notifications/cancelled", func() bool { return len(w.frames("notifications/cancelled")) == 1 })
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#cancellation
func TestModernStdioCancellation(t *testing.T) {
	t.Run("2026-07-28/transport-stdio/client-cancel-by-notification", func(t *testing.T) {
		w := newWire(t, "2026-07-28", func(m string, p map[string]any) (any, map[string]any) {
			if m == "server/discover" {
				return discoverWith(nil), nil
			}
			return nil, nil
		})
		c := dialWire(t, w, mcpclient.ForceModern, mcpclient.Options{})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, _ = c.CallTool(ctx, "slow", nil)
		waitFor(t, "notifications/cancelled", func() bool { return len(w.frames("notifications/cancelled")) == 1 })
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/changelog
func TestRemovedMethodsAreNotSentToModernServers(t *testing.T) {
	w := newWire(t, "2026-07-28", func(m string, p map[string]any) (any, map[string]any) {
		switch m {
		case "server/discover":
			return discoverWith(map[string]any{"logging": map[string]any{}, "tools": map[string]any{}}), nil
		case "tools/list":
			return map[string]any{"tools": []any{}, "ttlMs": 0, "cacheScope": "private"}, nil
		}
		return nil, map[string]any{"code": -32601, "message": "no " + m}
	})
	c := dialWire(t, w, mcpclient.ForceModern, mcpclient.Options{})
	t.Run("2026-07-28/ping/no-ping-to-modern-server", func(t *testing.T) {
		if err := c.Ping(ctx5()); err != nil {
			t.Fatal(err)
		}
		if len(w.frames("ping")) != 0 {
			t.Fatal("ping was sent to a modern server")
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/logging#per-request-log-level
	t.Run("2026-07-28/logging/level-per-request-in-meta", func(t *testing.T) {
		if err := c.SetLogLevel(ctx5(), "info"); err != nil {
			t.Fatal(err)
		}
		if len(w.frames("logging/setLevel")) != 0 {
			t.Fatal("logging/setLevel was sent to a modern server")
		}
		if _, err := c.ListTools(ctx5()); err != nil {
			t.Fatal(err)
		}
		lists := w.frames("tools/list")
		meta := lists[len(lists)-1]["params"].(map[string]any)["_meta"].(map[string]any)
		if meta[mcpclient.MetaLogLevel] != "info" {
			t.Fatalf("tools/list _meta lacks the log level: %v", meta)
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/pagination#implementation-guidelines
func TestPaginationCursors(t *testing.T) {
	pages := func(rev string, pref mcpclient.Preference) (*wire, *mcpclient.Client) {
		w := newWire(t, rev, func(m string, p map[string]any) (any, map[string]any) {
			switch m {
			case "server/discover":
				return discoverWith(map[string]any{"tools": map[string]any{}}), nil
			case "initialize":
				return initResult(rev, map[string]any{"tools": map[string]any{}}), nil
			case "tools/list":
				r := map[string]any{"ttlMs": 0, "cacheScope": "private"}
				if pref == mcpclient.ForceLegacy {
					r = map[string]any{}
				}
				if _, ok := p["cursor"]; !ok {
					r["tools"] = []any{map[string]any{"name": "a", "inputSchema": map[string]any{"type": "object"}}}
					r["nextCursor"] = ""
				} else {
					r["tools"] = []any{map[string]any{"name": "b", "inputSchema": map[string]any{"type": "object"}}}
				}
				return r, nil
			}
			return nil, nil
		})
		return w, dialWire(t, w, pref, mcpclient.Options{})
	}
	t.Run("2026-07-28/pagination/empty-string-cursor-is-a-cursor", func(t *testing.T) {
		w, c := pages("2026-07-28", mcpclient.ForceModern)
		tools, err := c.ListTools(ctx5())
		if err != nil || len(tools) != 2 {
			t.Fatalf("tools %v err %v; the empty cursor was not followed", tools, err)
		}
		if n := len(w.frames("tools/list")); n != 2 {
			t.Fatalf("%d tools/list requests, want 2", n)
		}
	})
	t.Run("2025-11-25/pagination/empty-string-cursor-ends-a-legacy-list", func(t *testing.T) {
		w, c := pages("2025-11-25", mcpclient.ForceLegacy)
		tools, err := c.ListTools(ctx5())
		if err != nil || len(tools) != 1 {
			t.Fatalf("tools %v err %v", tools, err)
		}
		if n := len(w.frames("tools/list")); n != 1 {
			t.Fatalf("%d tools/list requests, want 1", n)
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/server/resources#error-handling
func TestResourceNotFound(t *testing.T) {
	code := 0
	handler := func(rev string) func(string, map[string]any) (any, map[string]any) {
		return func(m string, p map[string]any) (any, map[string]any) {
			switch m {
			case "server/discover":
				return discoverWith(map[string]any{"resources": map[string]any{}}), nil
			case "initialize":
				return initResult(rev, map[string]any{"resources": map[string]any{}}), nil
			case "resources/read":
				return nil, map[string]any{"code": code, "message": "nope", "data": map[string]any{"uri": p["uri"]}}
			}
			return nil, nil
		}
	}
	modern := dialWire(t, newWire(t, "2026-07-28", handler("2026-07-28")), mcpclient.ForceModern, mcpclient.Options{})
	legacy := dialWire(t, newWire(t, "2025-11-25", handler("2025-11-25")), mcpclient.ForceLegacy, mcpclient.Options{})
	for _, tc := range []struct {
		name     string
		c        *mcpclient.Client
		code     int
		notFound bool
	}{
		{"2026-07-28/resources/not-found-32602", modern, -32602, true},
		{"2026-07-28/resources/not-found-accepts-32002", modern, -32002, true},
		{"2025-11-25/resources/not-found-32002", legacy, -32002, true},
		{"2025-11-25/resources/32602-is-invalid-params-not-not-found", legacy, -32602, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code = tc.code
			_, err := tc.c.ReadResource(ctx5(), "file:///missing")
			var nf *mcpclient.ResourceNotFoundError
			if errors.As(err, &nf) != tc.notFound {
				t.Fatalf("code %d: not-found=%v, want %v (%v)", tc.code, !tc.notFound, tc.notFound, err)
			}
		})
	}
}

// https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#stdio
func TestReceivedBatchesAreSplit(t *testing.T) {
	t.Run("2025-03-26/messages/client-accepts-batched-server-messages", func(t *testing.T) {
		w := newWire(t, "2025-03-26", func(m string, p map[string]any) (any, map[string]any) {
			if m == "initialize" {
				return initResult("2025-03-26", nil), nil
			}
			return nil, nil
		})
		c := dialWire(t, w, mcpclient.ForceLegacy, mcpclient.Options{})
		changed := make(chan string, 1)
		c.Subscribe(mcpclient.Notifications{OnListChanged: func(k string) { changed <- k }})
		w.pushRaw(`[{"jsonrpc":"2.0","method":"notifications/tools/list_changed"},{"jsonrpc":"2.0","id":"p1","method":"ping"}]`)
		select {
		case k := <-changed:
			if k != "tools" {
				t.Fatalf("kind %q", k)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the batched notification was dropped")
		}
		waitFor(t, "the batched ping answered", func() bool { return reply(w, "p1") != nil })
	})
}

// https://www.jsonrpc.org/specification#request_object -- an id is a string or a number.
func TestServerRequestWithStringID(t *testing.T) {
	t.Run("2025-11-25/messages/server-request-string-id-answered", func(t *testing.T) {
		w := newWire(t, "2025-11-25", func(m string, p map[string]any) (any, map[string]any) {
			if m == "initialize" {
				return initResult("2025-11-25", nil), nil
			}
			return nil, nil
		})
		dialWire(t, w, mcpclient.ForceLegacy, mcpclient.Options{})
		w.push(map[string]any{"jsonrpc": "2.0", "id": "req-7", "method": "ping"})
		waitFor(t, "a reply to the string id", func() bool { return reply(w, "req-7") != nil })
	})
}

// https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation
func TestElicitationDeclarations(t *testing.T) {
	accept := func(content map[string]any) mcpclient.ElicitHandler {
		return func(context.Context, string, json.RawMessage) (any, error) {
			return map[string]any{"action": "accept", "content": content}, nil
		}
	}
	legacyAt := func(rev string, h mcpclient.ElicitHandler) (*wire, *mcpclient.Client) {
		w := newWire(t, rev, func(m string, p map[string]any) (any, map[string]any) {
			if m == "initialize" {
				return initResult(rev, nil), nil
			}
			return nil, nil
		})
		return w, dialWire(t, w, mcpclient.ForceLegacy, mcpclient.Options{OnServerRequest: h})
	}
	elicit := func(w *wire, id string, mode string) map[string]any {
		p := map[string]any{"message": "?"}
		if mode == "url" {
			p["mode"], p["url"], p["elicitationId"] = "url", "https://example.invalid/x", "e1"
		} else {
			p["requestedSchema"] = map[string]any{"type": "object", "properties": map[string]any{
				"name": map[string]any{"type": "string", "default": "John Doe"},
				"age":  map[string]any{"type": "integer", "default": 30},
				"set":  map[string]any{"type": "boolean", "default": true},
			}}
		}
		w.push(map[string]any{"jsonrpc": "2.0", "id": id, "method": "elicitation/create", "params": p})
		var r map[string]any
		waitFor(t, "a reply to "+id, func() bool { r = reply(w, id); return r != nil })
		return r
	}

	t.Run("2025-11-25/elicitation/declares-form-and-url-only-when-answerable", func(t *testing.T) {
		for _, withHandler := range []bool{false, true} {
			var h mcpclient.ElicitHandler
			if withHandler {
				h = accept(nil)
			}
			w, _ := legacyAt("2025-11-25", h)
			caps := w.frames("initialize")[0]["params"].(map[string]any)["capabilities"].(map[string]any)
			el := caps["elicitation"].(map[string]any)
			if _, ok := el["form"]; !ok {
				t.Errorf("handler=%v: form not declared", withHandler)
			}
			if _, ok := el["url"]; ok != withHandler {
				t.Errorf("handler=%v: url declared=%v", withHandler, ok)
			}
		}
	})
	t.Run("2025-11-25/elicitation/undeclared-mode-is-32602", func(t *testing.T) {
		w, _ := legacyAt("2025-11-25", nil)
		r := elicit(w, "u1", "url")
		if e, _ := r["error"].(map[string]any); e == nil || e["code"] != float64(-32602) {
			t.Fatalf("url mode without a handler: %v", r)
		}
		// 2025-06-18 has no url mode, whatever initialize offered.
		w, _ = legacyAt("2025-06-18", accept(nil))
		r = elicit(w, "u2", "url")
		if e, _ := r["error"].(map[string]any); e == nil || e["code"] != float64(-32602) {
			t.Fatalf("url mode under 2025-06-18: %v", r)
		}
		w, _ = legacyAt("2025-11-25", accept(nil))
		r = elicit(w, "u3", "url")
		if r["error"] != nil {
			t.Fatalf("declared url mode refused: %v", r)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation#requested-schema
	t.Run("2025-11-25/elicitation/client-applies-schema-defaults", func(t *testing.T) {
		w, _ := legacyAt("2025-11-25", accept(map[string]any{"age": 41}))
		r := elicit(w, "f1", "form")
		content, _ := r["result"].(map[string]any)["content"].(map[string]any)
		if content["name"] != "John Doe" || content["set"] != true {
			t.Errorf("defaults not applied: %v", content)
		}
		if content["age"] != float64(41) {
			t.Errorf("an answered field was overwritten by its default: %v", content["age"])
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/subscriptions
func TestModernListenStream(t *testing.T) {
	var ackNarrow bool
	w := newWire(t, "2026-07-28", nil)
	w.handle = func(m string, p map[string]any) (any, map[string]any) {
		switch m {
		case "server/discover":
			return discoverWith(map[string]any{
				"tools":     map[string]any{"listChanged": true},
				"resources": map[string]any{"subscribe": true},
			}), nil
		}
		return nil, nil
	}
	listens := func() []map[string]any { return w.frames("subscriptions/listen") }
	c := dialWire(t, w, mcpclient.ForceModern, mcpclient.Options{})
	changed := make(chan string, 8)
	updated := make(chan string, 8)
	warned := make(chan mcpclient.Warning, 8)
	c.Subscribe(mcpclient.Notifications{
		OnListChanged:     func(k string) { changed <- k },
		OnResourceUpdated: func(u string) { updated <- u },
		OnWarning:         func(x mcpclient.Warning) { warned <- x },
	})
	tag := func(method string, id any, extra map[string]any) map[string]any {
		p := map[string]any{"_meta": map[string]any{mcpclient.MetaSubscriptionID: id}}
		for k, v := range extra {
			p[k] = v
		}
		return map[string]any{"jsonrpc": "2.0", "method": method, "params": p}
	}

	t.Run("2026-07-28/subscriptions/client-opens-listen-for-declared-capabilities", func(t *testing.T) {
		waitFor(t, "subscriptions/listen", func() bool { return len(listens()) == 1 })
		f := listens()[0]["params"].(map[string]any)["notifications"].(map[string]any)
		if f["toolsListChanged"] != true || len(f) != 1 {
			t.Fatalf("filter %v: want exactly toolsListChanged (prompts and resources lists were not declared)", f)
		}
	})
	id := listens()[0]["id"]

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#receiving-messages
	t.Run("2026-07-28/transport-stdio/client-correlates-subscription-id", func(t *testing.T) {
		w.push(tag("notifications/subscriptions/acknowledged", id, map[string]any{
			"notifications": map[string]any{"toolsListChanged": true}}))
		w.push(tag("notifications/tools/list_changed", 999, nil)) // no such stream
		w.push(tag("notifications/tools/list_changed", id, nil))
		select {
		case <-changed:
		case <-time.After(3 * time.Second):
			t.Fatal("a tagged list_changed was not delivered")
		}
		select {
		case <-changed:
			t.Fatal("a notification for another subscription id was delivered")
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("2026-07-28/subscriptions/resource-subscription-reopens-listen", func(t *testing.T) {
		if err := c.SubscribeResource(ctx5(), "file:///a"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "a second listen", func() bool { return len(listens()) == 2 })
		second := listens()[1]
		f := second["params"].(map[string]any)["notifications"].(map[string]any)
		subs, _ := f["resourceSubscriptions"].([]any)
		if len(subs) != 1 || subs[0] != "file:///a" {
			t.Fatalf("filter %v", f)
		}
		// On stdio the old stream is cancelled by notification.
		waitFor(t, "the first listen cancelled", func() bool {
			for _, n := range w.frames("notifications/cancelled") {
				if jsonKey(n["params"].(map[string]any)["requestId"]) == jsonKey(id) {
					return true
				}
			}
			return false
		})
		ackNarrow = true
		w.push(tag("notifications/subscriptions/acknowledged", second["id"], map[string]any{
			"notifications": map[string]any{"toolsListChanged": true}}))
		w.push(tag("notifications/resources/updated", second["id"], map[string]any{"uri": "file:///a"}))
		select {
		case u := <-updated:
			if u != "file:///a" {
				t.Fatalf("uri %q", u)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("resources/updated not delivered")
		}
	})

	t.Run("2026-07-28/subscriptions/client-checks-acknowledged-subset", func(t *testing.T) {
		if !ackNarrow {
			t.Skip("depends on the previous case")
		}
		select {
		case x := <-warned:
			if !strings.Contains(x.Reason, "file:///a") {
				t.Fatalf("warning %q", x.Reason)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a narrowed acknowledgement was not reported")
		}
	})

	t.Run("2026-07-28/subscriptions/client-reopens-after-server-ends-stream", func(t *testing.T) {
		last := listens()[len(listens())-1]
		w.push(map[string]any{"jsonrpc": "2.0", "id": last["id"], "result": map[string]any{
			"resultType": "complete", "_meta": map[string]any{mcpclient.MetaSubscriptionID: last["id"]}}})
		n := len(listens())
		waitFor(t, "the listen reopened", func() bool { return len(listens()) == n+1 })
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#protocol-version-negotiation
func TestVersionRetry(t *testing.T) {
	reject := func(n *int, always bool) func(string, map[string]any) (any, map[string]any) {
		return func(m string, p map[string]any) (any, map[string]any) {
			if m != "server/discover" {
				return nil, nil
			}
			*n++
			if always || *n == 1 {
				return nil, map[string]any{"code": -32022, "message": "Unsupported protocol version",
					"data": map[string]any{"supported": []string{"2026-07-28"}, "requested": "2026-07-28"}}
			}
			return discoverWith(nil), nil
		}
	}
	t.Run("2026-07-28/versioning/client-retries-with-supported-version", func(t *testing.T) {
		n := 0
		w := newWire(t, "2026-07-28", reject(&n, false))
		c := dialWire(t, w, mcpclient.ForceModern, mcpclient.Options{})
		if c.Era != mcpclient.EraModern || n != 2 {
			t.Fatalf("era %q after %d discovers", c.Era, n)
		}
	})
	t.Run("2026-07-28/versioning/retry-is-bounded", func(t *testing.T) {
		n := 0
		w := newWire(t, "2026-07-28", reject(&n, true))
		if _, err := dialWireErr(w, mcpclient.ForceModern, mcpclient.Options{}); err == nil {
			t.Fatal("a server that always rejects was accepted")
		}
		if n != 2 {
			t.Fatalf("%d discovers, want 2", n)
		}
	})
}
