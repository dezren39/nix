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

// capableTransport is a legacy server that declares what the test tells it
// to and answers whatever it is asked.
type capableTransport struct {
	caps  map[string]any
	in    chan []byte
	mu    sync.Mutex
	seen  []string
	close bool
	// asked is a request the server sends the client, once, on the first
	// tools/call.
	asked  string
	answer chan json.RawMessage
	// version is the protocolVersion initialize answers; 2025-11-25 when
	// empty.
	version string
	// noComplete makes completion/complete method-not-found.
	noComplete bool
}

func newCapable(caps map[string]any) *capableTransport {
	return &capableTransport{caps: caps, in: make(chan []byte, 16),
		answer: make(chan json.RawMessage, 1)}
}

func (f *capableTransport) Send(_ context.Context, msg []byte) error {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
	}
	_ = json.Unmarshal(msg, &req)
	if req.Method == "" {
		// The client answering something we asked it.
		select {
		case f.answer <- req.Result:
		default:
		}
		return nil
	}
	f.mu.Lock()
	f.seen = append(f.seen, req.Method)
	f.mu.Unlock()

	send := func(frame map[string]any) {
		b, _ := json.Marshal(frame)
		f.mu.Lock()
		closed := f.close
		f.mu.Unlock()
		if !closed {
			f.in <- b
		}
	}
	reply := func(result any) {
		send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": result})
	}

	switch req.Method {
	case "notifications/initialized":
		return nil
	case "initialize":
		v := f.version
		if v == "" {
			v = "2025-11-25"
		}
		reply(map[string]any{
			"protocolVersion": v,
			"serverInfo":      map[string]any{"name": "fake", "version": "1"},
			"capabilities":    f.caps,
		})
	case "completion/complete":
		if f.noComplete {
			send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID),
				"error": map[string]any{"code": -32601, "message": "no method"}})
			return nil
		}
		reply(map[string]any{"completion": map[string]any{
			"values": []string{"upstream-only"}, "total": 1, "hasMore": false}})
	case "some/extension":
		reply(map[string]any{"ok": true})
	default:
		send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID),
			"error": map[string]any{"code": -32601, "message": "no method " + req.Method}})
	}
	return nil
}

func (f *capableTransport) Recv() ([]byte, error) {
	msg, ok := <-f.in
	if !ok {
		return nil, context.Canceled
	}
	return msg, nil
}

func (f *capableTransport) Info() string { return "capable" }

func (f *capableTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.close {
		f.close = true
		close(f.in)
	}
	return nil
}

// ping the client from the server, which every revision allows and which
// mcpx answered with method-not-found -- read by a server as a dead
// connection, so the liveness probe reported the opposite of the truth.
func (f *capableTransport) pingClient() {
	f.in <- []byte(`{"jsonrpc":"2.0","id":9001,"method":"ping","params":{}}`)
}

func dial(t *testing.T, f mcpclient.Transport) *mcpclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := mcpclient.NewWithPreference(ctx, f, "mcpx", "test", mcpclient.ForceLegacy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// The typed helpers are what a code-mode host needs, which is not the set
// the protocol defines. Anything outside it had no way through at all, and
// the absence surfaced as "mcpx cannot do that" rather than "mcpx never
// asked".
func TestAnyMethodCanBeSent(t *testing.T) {
	c := dial(t, newCapable(map[string]any{"tools": map[string]any{}}))
	raw, err := c.Request(context.Background(), "some/extension", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "true") {
		t.Errorf("result = %s", raw)
	}
}

func TestCompletionIsNotAskedOfAServerThatDidNotDeclareIt(t *testing.T) {
	// A well-behaved server answers method-not-found and the rest answer
	// something unpredictable, so the declaration is the only thing worth
	// trusting. The caller needs to know which of the two it is showing.
	plain := newCapable(map[string]any{"tools": map[string]any{}})
	c := dial(t, plain)
	raw, ok, err := c.Complete(context.Background(), json.RawMessage(`{}`))
	if err != nil || ok || raw != nil {
		t.Fatalf("expected a clean absence, got %v %v %s", ok, err, raw)
	}
	plain.mu.Lock()
	seen := strings.Join(plain.seen, ",")
	plain.mu.Unlock()
	if strings.Contains(seen, "completion/complete") {
		t.Errorf("a server that declared nothing should not be asked: %s", seen)
	}

	declared := newCapable(map[string]any{
		"tools": map[string]any{}, "completions": map[string]any{}})
	c2 := dial(t, declared)
	raw, ok, err = c2.Complete(context.Background(), json.RawMessage(`{}`))
	if err != nil || !ok {
		t.Fatalf("a declared capability should be used: %v %v", ok, err)
	}
	if !strings.Contains(string(raw), "upstream-only") {
		t.Errorf("result = %s", raw)
	}
}

// 2024-11-05 had completion/complete and no capability to declare it, so
// gating on the declaration meant a server of that era was never asked.
func TestCompletionIsAskedOfA20241105Server(t *testing.T) {
	old := newCapable(map[string]any{"tools": map[string]any{}})
	old.version = "2024-11-05"
	c := dial(t, old)
	raw, ok, err := c.Complete(context.Background(), json.RawMessage(`{}`))
	if err != nil || !ok || !strings.Contains(string(raw), "upstream-only") {
		t.Fatalf("a 2024-11-05 server should be asked: ok=%v err=%v raw=%s", ok, err, raw)
	}

	// One that does not implement it is the same absence as an undeclared
	// capability, not an error.
	none := newCapable(map[string]any{"tools": map[string]any{}})
	none.version, none.noComplete = "2024-11-05", true
	c2 := dial(t, none)
	raw, ok, err = c2.Complete(context.Background(), json.RawMessage(`{}`))
	if err != nil || ok || raw != nil {
		t.Fatalf("method-not-found from a 2024-11-05 server is an absence: ok=%v err=%v raw=%s", ok, err, raw)
	}
	none.mu.Lock()
	seen := strings.Join(none.seen, ",")
	none.mu.Unlock()
	if !strings.Contains(seen, "completion/complete") {
		t.Fatalf("premise: server was never asked: %s", seen)
	}
}

func TestAServerMayPingItsClient(t *testing.T) {
	f := newCapable(map[string]any{"tools": map[string]any{}})
	dial(t, f)
	f.pingClient()
	select {
	case res := <-f.answer:
		if res == nil {
			t.Fatal("a ping must be answered with a result, not an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("mcpx never answered a ping; a server reads that as a dead connection")
	}
}
