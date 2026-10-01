package mcpclient_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// scriptedServer is a fake whose replies are chosen per test, recording every
// frame the client sends.
type scriptedServer struct {
	mu     sync.Mutex
	in     chan []byte
	frames []map[string]any
	// respond answers a request; nil means "no method".
	respond func(method string, params map[string]any) any
	closed  bool
}

func newScripted(respond func(string, map[string]any) any) *scriptedServer {
	return &scriptedServer{in: make(chan []byte, 64), respond: respond}
}

func (s *scriptedServer) Send(_ context.Context, msg []byte) error {
	var f map[string]any
	_ = json.Unmarshal(msg, &f)
	s.mu.Lock()
	s.frames = append(s.frames, f)
	s.mu.Unlock()
	method, _ := f["method"].(string)
	if method == "" || f["id"] == nil {
		return nil // a reply to us, or a notification
	}
	params, _ := f["params"].(map[string]any)
	out := s.respond(method, params)
	var reply map[string]any
	if out == nil {
		reply = map[string]any{"jsonrpc": "2.0", "id": f["id"],
			"error": map[string]any{"code": -32601, "message": "no method " + method}}
	} else {
		reply = map[string]any{"jsonrpc": "2.0", "id": f["id"], "result": out}
	}
	b, _ := json.Marshal(reply)
	s.in <- b
	return nil
}

func (s *scriptedServer) Recv() ([]byte, error) {
	msg, ok := <-s.in
	if !ok {
		return nil, context.Canceled
	}
	return msg, nil
}

func (s *scriptedServer) Info() string { return "scripted" }

func (s *scriptedServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.in)
	}
	return nil
}

func (s *scriptedServer) requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, f := range s.frames {
		if m, _ := f["method"].(string); m != "" && f["id"] != nil {
			out = append(out, f)
		}
	}
	return out
}

func modernDiscover(params map[string]any) any {
	return map[string]any{
		"resultType":        "complete",
		"supportedVersions": []string{"2026-07-28"},
		"_meta": map[string]any{
			"io.modelcontextprotocol/serverInfo": map[string]any{"name": "fake", "version": "1"},
		},
		"capabilities": map[string]any{"tools": map[string]any{}},
	}
}

func dialModern(t *testing.T, s *scriptedServer, o mcpclient.Options) *mcpclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.ClientName, o.ClientVersion, o.Preference = "mcpx", "test", mcpclient.ForceModern
	c, err := mcpclient.NewWithOptions(ctx, s, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestEveryModernRequestCarriesVersionAndCapabilities(t *testing.T) {
	// Both are required on every 2026-07-28 request, and a server MUST NOT
	// infer capabilities from an earlier one. The client used to send
	// neither -- not even on server/discover.
	s := newScripted(func(method string, params map[string]any) any {
		switch method {
		case "server/discover":
			return modernDiscover(params)
		case "tools/list":
			return map[string]any{"resultType": "complete", "tools": []any{}}
		}
		return nil
	})
	c := dialModern(t, s, mcpclient.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.ListTools(ctx); err != nil {
		t.Fatal(err)
	}

	reqs := s.requests()
	if len(reqs) < 2 {
		t.Fatalf("expected discover and tools/list, saw %d requests", len(reqs))
	}
	for _, r := range reqs {
		params, _ := r["params"].(map[string]any)
		meta, _ := params["_meta"].(map[string]any)
		if meta[mcpclient.MetaProtocolVersion] != "2026-07-28" {
			t.Errorf("%s: protocol version %v", r["method"], meta[mcpclient.MetaProtocolVersion])
		}
		if _, ok := meta[mcpclient.MetaClientCapabilities].(map[string]any); !ok {
			t.Errorf("%s: no client capabilities in _meta", r["method"])
		}
		info, _ := meta[mcpclient.MetaClientInfo].(map[string]any)
		if info["name"] != "mcpx" {
			t.Errorf("%s: client info %v", r["method"], info)
		}
	}
}

func TestAnInputRequiredResultIsAnsweredAndRetried(t *testing.T) {
	// How a 2026-07-28 server elicits: it answers "not yet", lists what it
	// needs, and expects the same request again with the answers and its
	// opaque state. Before this, the client returned the "not yet" as if it
	// were the result.
	var calls int
	s := newScripted(func(method string, params map[string]any) any {
		switch method {
		case "server/discover":
			return modernDiscover(params)
		case "tools/call":
			calls++
			resp, _ := params["inputResponses"].(map[string]any)
			if resp == nil {
				return map[string]any{
					"resultType": "input_required",
					"inputRequests": map[string]any{
						"repo": map[string]any{"method": "elicitation/create", "params": map[string]any{
							"mode": "form", "message": "Which repository?",
							"requestedSchema": map[string]any{"type": "object"}}},
						"where": map[string]any{"method": "roots/list", "params": map[string]any{}},
					},
					"requestState": "opaque-7",
				}
			}
			if params["requestState"] != "opaque-7" {
				return map[string]any{"resultType": "complete", "isError": true,
					"content": []any{map[string]any{"type": "text", "text": "state lost"}}}
			}
			b, _ := json.Marshal(resp)
			return map[string]any{"resultType": "complete",
				"content": []any{map[string]any{"type": "text", "text": string(b)}}}
		}
		return nil
	})
	asked := ""
	c := dialModern(t, s, mcpclient.Options{
		Roots: []mcpclient.Root{{URI: "file:///work", Name: "work"}},
		OnServerRequest: func(_ context.Context, method string, params json.RawMessage) (any, error) {
			asked = method
			return map[string]any{"action": "accept", "content": map[string]any{"repo": "me/x"}}, nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := c.CallTool(ctx, "make_issue", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("expected one retry, the server saw %d calls", calls)
	}
	if asked != "elicitation/create" {
		t.Errorf("the handler should have been asked, got %q", asked)
	}
	for _, want := range []string{"me/x", "file:///work", "accept"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the retry should carry %s:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "state lost") {
		t.Error("requestState must be passed back exactly")
	}
}

// https://modelcontextprotocol.io/specification/2026-07-28/client/elicitation#requested-schema
// The 2026-07-28 path asks through input_required, not a wire request, so
// the legacy defaults test does not reach it.
func TestModernElicitationAppliesSchemaDefaults(t *testing.T) {
	t.Run("2026-07-28/elicitation/client-applies-schema-defaults", func(t *testing.T) {
		var got map[string]any
		s := newScripted(func(method string, params map[string]any) any {
			switch method {
			case "server/discover":
				return modernDiscover(params)
			case "tools/call":
				resp, _ := params["inputResponses"].(map[string]any)
				if resp == nil {
					return map[string]any{
						"resultType": "input_required",
						"inputRequests": map[string]any{
							"q": map[string]any{"method": "elicitation/create", "params": map[string]any{
								"mode": "form", "message": "?",
								"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{
									"name": map[string]any{"type": "string", "default": "John Doe"},
									"age":  map[string]any{"type": "integer", "default": 30},
								}}}},
						},
					}
				}
				got, _ = resp["q"].(map[string]any)
				return map[string]any{"resultType": "complete", "content": []any{}}
			}
			return nil
		})
		c := dialModern(t, s, mcpclient.Options{
			OnServerRequest: func(context.Context, string, json.RawMessage) (any, error) {
				return map[string]any{"action": "accept", "content": map[string]any{"age": 41}}, nil
			},
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := c.CallTool(ctx, "ask", map[string]any{}); err != nil {
			t.Fatal(err)
		}
		content, _ := got["content"].(map[string]any)
		if content["name"] != "John Doe" {
			t.Errorf("default not applied: %v", got)
		}
		if content["age"] != float64(41) {
			t.Errorf("an answered field was overwritten by its default: %v", got)
		}
	})
}

func TestAServerThatNeverStopsAskingIsCutOff(t *testing.T) {
	s := newScripted(func(method string, params map[string]any) any {
		switch method {
		case "server/discover":
			return modernDiscover(params)
		case "tools/call":
			return map[string]any{"resultType": "input_required", "requestState": "again"}
		}
		return nil
	})
	c := dialModern(t, s, mcpclient.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.CallTool(ctx, "nag", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "rounds") {
		t.Fatalf("expected a bounded failure, got %v", err)
	}
}

func legacyServer(caps *map[string]any) *scriptedServer {
	return newScripted(func(method string, params map[string]any) any {
		if method == "initialize" {
			if caps != nil {
				*caps, _ = params["capabilities"].(map[string]any)
			}
			return map[string]any{
				"protocolVersion": "2025-11-25",
				"serverInfo":      map[string]any{"name": "fake", "version": "1"},
				"capabilities":    map[string]any{},
			}
		}
		return nil
	})
}

func TestSamplingIsDeclaredOnlyWhenSomethingCanAnswerIt(t *testing.T) {
	// Declared in the handshake, so the handler has to exist before it. It
	// used to be installed afterwards and sampling was never declared at
	// all -- the passthrough behind it was unreachable.
	for _, withHandler := range []bool{false, true} {
		var caps map[string]any
		s := legacyServer(&caps)
		o := mcpclient.Options{ClientName: "mcpx", ClientVersion: "t", Preference: mcpclient.ForceLegacy}
		if withHandler {
			o.OnServerRequest = func(context.Context, string, json.RawMessage) (any, error) { return nil, nil }
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, err := mcpclient.NewWithOptions(ctx, s, o)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		_, has := caps["sampling"]
		if has != withHandler {
			t.Errorf("handler=%v: sampling declared=%v", withHandler, has)
		}
		if _, ok := caps["elicitation"]; !ok {
			t.Errorf("handler=%v: elicitation should always be declared", withHandler)
		}
		c.Close()
	}
}

func TestRootsAreServedEvenWithAHandlerInstalled(t *testing.T) {
	// The daemon always installs a handler, and it used to receive every
	// server request -- roots/list included, which it did not implement.
	s := legacyServer(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := mcpclient.NewWithOptions(ctx, s, mcpclient.Options{
		ClientName: "mcpx", ClientVersion: "t", Preference: mcpclient.ForceLegacy,
		Roots: []mcpclient.Root{{URI: "file:///work"}},
		OnServerRequest: func(context.Context, string, json.RawMessage) (any, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// The server asks; the client's reply is the frame with our id and no
	// method.
	s.in <- []byte(`{"jsonrpc":"2.0","id":991,"method":"roots/list","params":{}}`)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, f := range s.frames {
			if id, _ := f["id"].(float64); id == 991 && f["method"] == nil {
				s.mu.Unlock()
				if f["error"] != nil {
					t.Fatalf("roots/list was refused: %v", f["error"])
				}
				b, _ := json.Marshal(f["result"])
				if !strings.Contains(string(b), "file:///work") {
					t.Fatalf("roots missing from the reply: %s", b)
				}
				return
			}
		}
		s.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the client never answered roots/list")
}

func TestInputRequestsOfOneRoundAreAskedTogetherUnderTheirKeys(t *testing.T) {
	// A server that asks two things in one round means one round. Asked
	// one at a time, a gateway relaying them could only show its own client
	// the first, and the client came back once per question.
	s := newScripted(func(method string, params map[string]any) any {
		switch method {
		case "server/discover":
			return modernDiscover(params)
		case "tools/call":
			if params["inputResponses"] == nil {
				q := func(msg string) map[string]any {
					return map[string]any{"method": "elicitation/create", "params": map[string]any{
						"mode": "form", "message": msg, "requestedSchema": map[string]any{"type": "object"}}}
				}
				return map[string]any{"resultType": "input_required", "requestState": "s",
					"inputRequests": map[string]any{"a": q("first?"), "b": q("second?")}}
			}
			return map[string]any{"resultType": "complete", "content": []any{}}
		}
		return nil
	})
	var mu sync.Mutex
	keys := map[string]bool{}
	both := make(chan struct{})
	c := dialModern(t, s, mcpclient.Options{
		OnServerRequest: func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
			mu.Lock()
			keys[mcpclient.InputKey(ctx)] = true
			if len(keys) == 2 {
				close(both)
			}
			mu.Unlock()
			select {
			case <-both:
			case <-time.After(2 * time.Second):
				return nil, context.DeadlineExceeded
			}
			return map[string]any{"action": "accept", "content": map[string]any{}}, nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.CallTool(ctx, "two", map[string]any{}); err != nil {
		t.Fatalf("both questions should have been open at once: %v", err)
	}
	if !keys["a"] || !keys["b"] {
		t.Errorf("each question should carry its server's key, saw %v", keys)
	}
}
