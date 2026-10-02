package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/tasks"
)

// scripted is an MCP server in memory. It completes the handshake and then
// answers tools/call with the error it was given, or hangs up mid-call.
//
// A real mcpclient.Client talks to it, so the error these tests read is the
// one the daemon reads: if mcpclient changes how it reports a server's error,
// upstreamFault stops recognising it and these tests say so.
type scripted struct {
	code    int
	message string
	data    json.RawMessage
	hangUp  bool

	in   chan []byte
	once sync.Once
}

func newScripted(code int, message string) *scripted {
	return &scripted{code: code, message: message, in: make(chan []byte, 8)}
}

func (s *scripted) Send(_ context.Context, msg []byte) error {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(msg, &req)
	frame := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	switch req.Method {
	case "initialize":
		frame["result"] = map[string]any{
			"protocolVersion": mcpclient.ProtocolVersion,
			"serverInfo":      map[string]any{"name": "scripted", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}
	case "tools/call", "resources/read":
		if s.hangUp {
			_ = s.Close()
			return nil
		}
		e := map[string]any{"code": s.code, "message": s.message}
		if len(s.data) > 0 {
			e["data"] = s.data
		}
		frame["error"] = e
	default:
		return nil
	}
	b, _ := json.Marshal(frame)
	s.in <- b
	return nil
}

func (s *scripted) Recv() ([]byte, error) {
	msg, ok := <-s.in
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return msg, nil
}

func (s *scripted) Close() error {
	s.once.Do(func() { close(s.in) })
	return nil
}

func (s *scripted) Info() string { return "scripted" }

// callThrough makes one tools/call against a scripted server and returns the
// error exactly as mcpclient hands it to the pool.
func callThrough(t *testing.T, s *scripted) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mcpclient.NewWithPreference(ctx, s, "test", "0", mcpclient.ForceLegacy)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer c.Close()
	_, err = c.CallTool(ctx, "create_issue", map[string]any{"title": "x"})
	if err == nil {
		t.Fatal("the scripted server should have refused the call")
	}
	return err
}

func TestUpstreamFaultReadsTheServersAnswer(t *testing.T) {
	err := callThrough(t, newScripted(diagnose.InvalidParams, "Invalid params"))
	for _, e := range []error{err, fmt.Errorf("demo.create_issue: %w", err)} {
		code, msg, ok := upstreamFault(e)
		if !ok || code != diagnose.InvalidParams || msg != "Invalid params" {
			t.Errorf("upstreamFault(%v) = %d, %q, %v", e, code, msg, ok)
		}
	}
}

func TestUpstreamFaultIgnoresWhatNoServerSaid(t *testing.T) {
	for _, e := range []error{
		context.DeadlineExceeded,
		// The text of a server's error, with no server behind it.
		errors.New("mcp error -32602: Invalid params"),
		// A daemon-side error with the same two fields.
		&tasks.Fault{Code: diagnose.InvalidParams, Message: "Invalid params"},
	} {
		if code, msg, ok := upstreamFault(e); ok {
			t.Errorf("upstreamFault(%T %v) = %d, %q; this is not a server's answer", e, e, code, msg)
		}
	}
}

// schemaRegistry is a registry whose demo.create_issue gained a required
// repo between two observations, as catalog history records it after an
// upgrade. cold is configured and has never been read.
func schemaRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		// Never started: every schema here is seeded, as the disk cache does.
		"demo": {Command: filepath.Join(dir, "never-run")},
		"cold": {Command: filepath.Join(dir, "never-run-either")},
	}}
	r, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.history = diagnose.OpenHistory(filepath.Join(dir, CatalogHistoryFile), 8)
	p, _ := r.Pool("demo")
	schema := func(required ...string) json.RawMessage {
		props := map[string]any{}
		for _, n := range required {
			props[n] = map[string]any{"type": "string"}
		}
		b, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required})
		return b
	}
	p.SetSchemas([]mcpclient.Tool{{Name: "create_issue", InputSchema: schema("title")}}, nil, nil, "", time.Now())
	r.ObserveCatalog()
	p.SetSchemas([]mcpclient.Tool{{Name: "create_issue", InputSchema: schema("title", "repo")}}, nil, nil, "", time.Now())
	r.ObserveCatalog()
	return r
}

func TestARefusedCallIsExplainedByWhatChanged(t *testing.T) {
	r := schemaRegistry(t)
	upstream := callThrough(t, newScripted(diagnose.InvalidParams, "Invalid params"))

	got := r.explainCall(context.Background(), "demo", "create_issue", map[string]any{"title": "x"}, upstream)
	ds := callDiagnostics(got)
	if len(ds) != 1 {
		t.Fatalf("expected one diagnostic, got %+v", ds)
	}
	d := ds[0]
	if d.Tool != "demo.create_issue" || d.Field != "repo" || d.Changed == nil || d.Changed.Field != "repo" {
		t.Errorf("the diagnostic should name the tool, the field and the change: %+v", d)
	}
	// Added to, never replaced: the server's text comes first and whole.
	if !strings.HasPrefix(got.Error(), upstream.Error()+"\n") {
		t.Errorf("the upstream message must lead the error:\n%s", got.Error())
	}
	if !strings.Contains(got.Error(), diagnose.Render(ds)) {
		t.Errorf("the rendered diagnostic should follow it:\n%s", got.Error())
	}
	if !errors.Is(got, upstream) {
		t.Error("the upstream error should still be reachable with errors.Is")
	}
	body := callErrorBody(got)
	if body.Error != got.Error() || len(body.Diagnostics) != 1 {
		t.Errorf("body = %+v", body)
	}
}

func TestAFailureTheCatalogCannotExplainIsLeftAlone(t *testing.T) {
	r := schemaRegistry(t)
	hungUp := newScripted(0, "")
	hungUp.hangUp = true
	dropped := callThrough(t, hungUp)
	// The trap this case exists for: mcpclient reports a server that died
	// mid-call as a JSON-RPC error, -32000 "connection closed: unexpected
	// EOF", so it does reach CallError, and "unexpected" contains
	// "expected". If this stops holding, the case below is testing the
	// transport branch instead and wants rewriting rather than deleting.
	if code, msg, ok := upstreamFault(dropped); !ok || !strings.Contains(msg, "expected") {
		t.Fatalf("precondition: a dropped connection read as %d %q %v", code, msg, ok)
	}
	for name, c := range map[string]struct {
		server string
		err    error
	}{
		"a timeout":            {"demo", context.DeadlineExceeded},
		"a dropped connection": {"demo", dropped},
		"an internal error": {"demo", callThrough(t,
			newScripted(-32603, "internal error: the backend is unavailable"))},
		// Configured, never read: against an empty catalog every tool would be
		// "unknown", and that would be false.
		"a server not yet read": {"cold", callThrough(t,
			newScripted(diagnose.InvalidParams, "Invalid params"))},
	} {
		got := r.explainCall(context.Background(), c.server, "create_issue", map[string]any{}, c.err)
		if got != c.err {
			t.Errorf("%s: the error should come back unchanged, got:\n%v", name, got)
		}
		if body := callErrorBody(got); body.Diagnostics != nil {
			t.Errorf("%s: no diagnostics expected, got %+v", name, body.Diagnostics)
		}
	}
}

// /v1's error body carries the server's JSON-RPC error whole, so a relay can
// answer with it: a pass-through's -32021 reaches its client as -32021 with
// the server's requiredCapabilities, not as a sentence.
func TestAFailedCallsBodyCarriesTheServersError(t *testing.T) {
	s := newScripted(-32021, "MissingRequiredClientCapabilityError")
	s.data = json.RawMessage(`{"requiredCapabilities":{"sampling":{}}}`)
	err := callThrough(t, s)
	body := callErrorBody(fmt.Errorf("demo.create_issue: %w", err))
	if body.Upstream == nil || body.Upstream.Code != -32021 ||
		body.Upstream.Message != "MissingRequiredClientCapabilityError" ||
		!strings.Contains(string(body.Upstream.Data), `"sampling"`) {
		t.Fatalf("upstream = %+v", body.Upstream)
	}
	// A read of a missing resource is mcpclient's ResourceNotFoundError
	// wrapping the server's error. Its own struct has no Data field, and
	// reading one off it panicked the daemon on the first resources/read
	// the official suite made.
	nf := newScripted(-32002, "Resource not found")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mcpclient.NewWithPreference(ctx, nf, "test", "0", mcpclient.ForceLegacy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, rerr := c.ReadResource(ctx, "file:///nope")
	var missing *mcpclient.ResourceNotFoundError
	if !errors.As(rerr, &missing) {
		t.Fatalf("premise: want a ResourceNotFoundError, got %v", rerr)
	}
	if b := callErrorBody(rerr); b.Upstream == nil || b.Upstream.Code != -32002 {
		t.Errorf("upstream = %+v", b.Upstream)
	}
}

// A roots answer is the client's ListRootsResult, accepted as given.
func TestARootsAnswerIsTheClientsResult(t *testing.T) {
	ans, err := answerFromResult("r-1", "roots/list", json.RawMessage(`{"roots":[{"uri":"file:///c"}]}`))
	if err != nil || ans.Action != "accept" || !strings.Contains(string(ans.Content), "file:///c") {
		t.Fatalf("answer = %+v, %v", ans, err)
	}
	if _, err := answerFromResult("r-1", "roots/list", json.RawMessage(`null`)); err == nil {
		t.Error("an empty roots answer should be refused")
	}
}

// The relayed client's capabilities ride on a header and reach the call's
// context; a malformed header is ignored rather than declared.
func TestClientCapsHeaderReachesTheCall(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/call", nil)
	r.Header.Set(ClientCapsHeader, `{"roots":{}}`)
	if got := mcpclient.ClientCapabilitiesFrom(withClientCaps(context.Background(), r)); string(got) != `{"roots":{}}` {
		t.Errorf("caps = %s", got)
	}
	r.Header.Set(ClientCapsHeader, `{not json`)
	if got := mcpclient.ClientCapabilitiesFrom(withClientCaps(context.Background(), r)); got != nil {
		t.Errorf("malformed header declared %s", got)
	}
}

// A roots/list no call of a relaying client raised has nobody to put it to,
// and is handed back for mcpx to answer with its own roots.
func TestARootsQuestionWithNoCallIsNotRelayed(t *testing.T) {
	r := &Registry{}
	if _, err := r.rootsViaBroker(context.Background(), "demo", "global", nil); !errors.Is(err, mcpclient.ErrNotRelayed) {
		t.Fatalf("err = %v, want ErrNotRelayed", err)
	}
}
