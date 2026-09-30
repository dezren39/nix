package mcpclient_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// fakeTransport speaks whichever era it was built for, so the client's
// probing can be tested without a subprocess.
type fakeTransport struct {
	era    string // legacy | modern | modern-only-strict
	in     chan []byte
	seen   []string
	closed bool
	onSend func([]byte)
}

func newFake(era string) *fakeTransport {
	return &fakeTransport{era: era, in: make(chan []byte, 16)}
}

func (f *fakeTransport) Send(_ context.Context, msg []byte) error {
	if f.onSend != nil {
		f.onSend(msg)
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(msg, &req)
	// Only requests get answered. A frame with no method is the client
	// replying to something we asked, and treating that as a request meant
	// answering it with an error -- onto a channel the test had already
	// closed, which panicked whenever the reply lost the race with Close.
	if req.Method == "" {
		return nil
	}
	f.seen = append(f.seen, req.Method)

	send := func(frame map[string]any) {
		b, _ := json.Marshal(frame)
		if !f.closed {
			f.in <- b
		}
	}
	reply := func(result any) {
		send(map[string]any{
			"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": result,
		})
	}
	fail := func(code int, message string, data any) {
		send(map[string]any{
			"jsonrpc": "2.0", "id": json.RawMessage(req.ID),
			"error": map[string]any{"code": code, "message": message, "data": data},
		})
	}

	switch req.Method {
	case "notifications/initialized":
		return nil
	case "initialize":
		if f.era != "legacy" {
			fail(-32601, "no method initialize", nil)
			return nil
		}
		reply(map[string]any{
			"protocolVersion": "2025-11-25",
			"serverInfo":      map[string]any{"name": "fake", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		})
	case "server/discover":
		if f.era == "legacy" {
			fail(-32601, "no method server/discover", nil)
			return nil
		}
		if f.era == "modern-only-strict" {
			fail(-32022, "Unsupported protocol version",
				map[string]any{"supported": []string{"2099-01-01"}})
			return nil
		}
		reply(map[string]any{
			"supportedVersions": []string{"2026-07-28"},
			"_meta": map[string]any{
				"io.modelcontextprotocol/serverInfo": map[string]any{"name": "fake", "version": "1"},
			},
			"capabilities": map[string]any{"tools": map[string]any{}},
		})
	default:
		fail(-32601, "no method "+req.Method, nil)
	}
	return nil
}

func (f *fakeTransport) Recv() ([]byte, error) {
	msg, ok := <-f.in
	if !ok {
		return nil, context.Canceled
	}
	return msg, nil
}

func (f *fakeTransport) Info() string { return "fake:" + f.era }

func (f *fakeTransport) Close() error {
	if !f.closed {
		f.closed = true
		close(f.in)
	}
	return nil
}

func connect(t *testing.T, era string, pref mcpclient.Preference) (*mcpclient.Client, *fakeTransport, error) {
	t.Helper()
	f := newFake(era)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := mcpclient.NewWithPreference(ctx, f, "mcpx", "test", pref)
	return c, f, err
}

func TestALegacyServerIsReachedOnTheFirstTry(t *testing.T) {
	// Nearly every server in existence is legacy, so the default must not
	// cost them a wasted probe.
	c, f, err := connect(t, "legacy", mcpclient.PreferLegacy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Era != mcpclient.EraLegacy {
		t.Errorf("era = %q", c.Era)
	}
	if strings.Join(f.seen, ",") != "initialize,notifications/initialized" {
		t.Errorf("no probe should have been wasted: %v", f.seen)
	}
}

func TestAModernServerIsReachedByFallingForward(t *testing.T) {
	c, f, err := connect(t, "modern", mcpclient.PreferLegacy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Era != mcpclient.EraModern {
		t.Errorf("era = %q", c.Era)
	}
	if c.Negotiated != "2026-07-28" {
		t.Errorf("negotiated = %q", c.Negotiated)
	}
	if !strings.Contains(strings.Join(f.seen, ","), "server/discover") {
		t.Errorf("it should have probed: %v", f.seen)
	}
}

func TestAModernServerIsReachedDirectlyWhenPreferred(t *testing.T) {
	c, f, err := connect(t, "modern", mcpclient.PreferModern)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if f.seen[0] != "server/discover" {
		t.Errorf("modern should have been tried first: %v", f.seen)
	}
}

func TestALegacyServerIsStillReachedWhenModernIsPreferred(t *testing.T) {
	// The fallback has to work in both directions or the preference becomes
	// a way to break half the ecosystem.
	c, _, err := connect(t, "legacy", mcpclient.PreferModern)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Era != mcpclient.EraLegacy {
		t.Errorf("era = %q", c.Era)
	}
}

func TestAVersionErrorDoesNotTriggerAFallback(t *testing.T) {
	// A recognised modern error identifies a modern server. The version is
	// wrong, not the era, and falling back would report the wrong problem.
	_, f, err := connect(t, "modern-only-strict", mcpclient.PreferModern)
	if err == nil {
		t.Fatal("expected a version failure")
	}
	for _, m := range f.seen {
		if m == "initialize" {
			t.Errorf("it should not have fallen back: %v", f.seen)
		}
	}
	if !strings.Contains(err.Error(), "2099-01-01") {
		t.Errorf("the supported list should reach the caller: %v", err)
	}
}

func TestForcingAnEraSkipsTheFallback(t *testing.T) {
	_, f, err := connect(t, "legacy", mcpclient.ForceModern)
	if err == nil {
		t.Fatal("forcing modern against a legacy server should fail")
	}
	for _, m := range f.seen {
		if m == "initialize" {
			t.Errorf("force should mean force: %v", f.seen)
		}
	}
}

// TestAServerQuestionIsAnsweredRatherThanDropped is the regression for a live
// bug: recvLoop matched inbound frames on id alone, so a server-initiated
// request looked like a reply to nothing and was discarded. The server then
// waited until the call timed out, and mcpx reported a timeout -- true,
// useless, and pointing at the wrong thing.
func TestAServerQuestionIsAnsweredRatherThanDropped(t *testing.T) {
	f := newFake("legacy")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := mcpclient.NewWithPreference(ctx, f, "mcpx", "test", mcpclient.PreferLegacy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	answered := make(chan json.RawMessage, 1)
	f.onSend = func(msg []byte) {
		var m struct {
			Result json.RawMessage `json:"result"`
			ID     *int64          `json:"id"`
		}
		if json.Unmarshal(msg, &m) == nil && m.Result != nil && m.ID != nil && *m.ID == 99 {
			answered <- m.Result
		}
	}

	c.SetElicitHandler(func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "elicitation/create" {
			return nil, nil
		}
		return map[string]any{"action": "accept", "content": map[string]any{"repo": "me/thing"}}, nil
	})

	// The server asks.
	f.in <- []byte(`{"jsonrpc":"2.0","id":99,"method":"elicitation/create",
		"params":{"message":"which repo?","requestedSchema":{"type":"object"}}}`)

	select {
	case got := <-answered:
		if !strings.Contains(string(got), "me/thing") {
			t.Errorf("the handler's answer should have been sent: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server's question was never answered")
	}
}

func TestWithNoHandlerAServerQuestionIsCancelledNotIgnored(t *testing.T) {
	// Silence is indistinguishable from a hung server. Cancel is honest:
	// nobody was asked, so nobody chose.
	f := newFake("legacy")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := mcpclient.NewWithPreference(ctx, f, "mcpx", "test", mcpclient.PreferLegacy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	answered := make(chan json.RawMessage, 1)
	f.onSend = func(msg []byte) {
		var m struct {
			Result json.RawMessage `json:"result"`
			ID     *int64          `json:"id"`
		}
		if json.Unmarshal(msg, &m) == nil && m.Result != nil && m.ID != nil && *m.ID == 7 {
			answered <- m.Result
		}
	}
	f.in <- []byte(`{"jsonrpc":"2.0","id":7,"method":"elicitation/create","params":{}}`)

	select {
	case got := <-answered:
		if !strings.Contains(string(got), "cancel") {
			t.Errorf("expected a cancel, got %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was sent back")
	}
}
