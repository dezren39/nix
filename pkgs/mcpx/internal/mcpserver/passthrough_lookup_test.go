package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// passBackend is fakeBackend plus one upstream served under its own names.
type passBackend struct {
	*fakeBackend
	mu      sync.Mutex
	lookups int
	down    error
	lastNS  string
}

func (p *passBackend) UpstreamTools(context.Context, string) ([]mcpserver.Tool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lookups++
	if p.down != nil {
		return nil, p.down
	}
	return []mcpserver.Tool{{Name: "greet", Description: "says hello",
		InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
}

func (p *passBackend) Call(ctx context.Context, ns, tool string, args json.RawMessage) (string, error) {
	p.mu.Lock()
	p.lastNS = ns
	p.mu.Unlock()
	return p.fakeBackend.Call(ctx, ns, tool, args)
}

func (p *passBackend) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lookups
}

func passServer(b *passBackend) *mcpserver.Server {
	s := mcpserver.New(b, "mcpx", "test")
	s.Passthrough = []string{"up"}
	return s
}

// An upstream that is not answering must not look like an upstream with no
// tools. Before, tools/list quietly listed only mcpx's own tools, and calling
// the upstream's was answered "no tool named", which is false: the name is
// right and the server that knows it is down.
func TestAPassThroughUpstreamThatIsDownIsAnErrorNotAnEmptyList(t *testing.T) {
	b := &passBackend{fakeBackend: newBackend(), down: errors.New("connection refused")}
	s := passServer(b)

	m := handle(t, s, "tools/list", map[string]any{})
	if errCode(m) != -32603 {
		t.Fatalf("tools/list with the upstream down: want -32603, got %v", m)
	}
	if msg := m["error"].(map[string]any)["message"].(string); !strings.Contains(msg, `"up"`) {
		t.Errorf("error should name the upstream: %q", msg)
	}

	m = handle(t, s, "tools/call", map[string]any{"name": "greet", "arguments": map[string]any{}})
	if errCode(m) == -32602 {
		t.Fatalf("a call to the upstream's tool while it is down was answered 'no such tool': %v", m)
	}
	if errCode(m) != -32603 {
		t.Fatalf("want -32603, got %v", m)
	}
}

// One request, one lookup. tools/call asks both "does this name exist" and
// "is it the upstream's", and each lookup is several daemon round trips.
func TestAPassThroughCallLooksTheUpstreamUpOnce(t *testing.T) {
	b := &passBackend{fakeBackend: newBackend()}
	s := passServer(b)

	m := handle(t, s, "tools/call", map[string]any{"name": "greet", "arguments": map[string]any{}})
	if errCode(m) != 0 {
		t.Fatalf("tools/call greet: %v", m)
	}
	if b.lastNS != "up" {
		t.Errorf("greet went to %q, want the pass-through upstream", b.lastNS)
	}
	if n := b.count(); n != 1 {
		t.Errorf("one tools/call looked the upstream's tools up %d times, want 1", n)
	}
}

// The plain-POST projection runs tools through InvokeTool. It went straight to
// the gateway's dispatch, so a pass-through tool callable over JSON-RPC was
// unknown over POST.
func TestInvokeToolReachesThePassThroughUpstream(t *testing.T) {
	b := &passBackend{fakeBackend: newBackend()}
	s := passServer(b)
	out, err := s.InvokeTool(context.Background(), "greet", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("InvokeTool greet: %v", err)
	}
	if b.lastNS != "up" || !strings.Contains(out, "up.greet") {
		t.Errorf("InvokeTool greet = %q via %q, want the upstream", out, b.lastNS)
	}
}
