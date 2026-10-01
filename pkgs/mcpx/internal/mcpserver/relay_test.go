package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// relayBackend is a pass-through upstream whose call does what the test
// says, and remembers what it was given.
type relayBackend struct {
	*passBackend
	mu    sync.Mutex
	caps  json.RawMessage
	tools []string
	err   error
	delay time.Duration
}

func (r *relayBackend) Call(ctx context.Context, ns, tool string, args json.RawMessage) (string, error) {
	r.mu.Lock()
	r.caps = mcpserver.DeclaredCapabilities(ctx)
	r.tools = append(r.tools, tool)
	err, delay := r.err, r.delay
	r.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if err != nil {
		return "", err
	}
	return r.passBackend.Call(ctx, ns, tool, args)
}

func newRelay() *relayBackend {
	return &relayBackend{passBackend: &passBackend{fakeBackend: newBackend()}}
}

// An upstream's -32021 reaches the client as -32021, with HTTP 400 and the
// upstream's requiredCapabilities -- not as a tool result saying the call
// failed, which tells the client the tool ran. And the capabilities the
// client declared are what the Backend is handed to relay: mcpx declaring
// its own made the upstream run a tool the client could not support.
//
// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#meta
func TestAPassThroughUpstreamsMissingCapabilityIsRelayed(t *testing.T) {
	b := newRelay()
	b.err = &mcpserver.UpstreamError{Code: -32021, Message: "MissingRequiredClientCapabilityError",
		Data: json.RawMessage(`{"requiredCapabilities":{"sampling":{}}}`)}
	s := mcpserver.New(b, "mcpx", "test")
	s.Passthrough = []string{"up"}

	w := post(s, frame(9, "tools/call", modernWith(`{"elicitation":{}}`,
		map[string]any{"name": "greet", "arguments": map[string]any{}})), modernHeaders("tools/call", "greet"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400:\n%s", w.Code, w.Body)
	}
	var m struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Required map[string]any `json:"requiredCapabilities"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Error.Code != -32021 || m.Error.Data.Required["sampling"] == nil {
		t.Fatalf("want -32021 naming sampling, got:\n%s", w.Body)
	}
	b.mu.Lock()
	caps := string(b.caps)
	b.mu.Unlock()
	if caps != `{"elicitation":{}}` {
		t.Errorf("the backend should be handed the client's own declaration, got %q", caps)
	}
}

// In pass-through mode the upstream decides which names it answers to. The
// official suite's reference server answers test_trigger_prompt_change
// without listing it; mcpx answered "no tool named", so the prompt list
// never changed and no list_changed could be relayed.
func TestAPassThroughCallToAnUnlistedNameReachesTheUpstream(t *testing.T) {
	b := newRelay()
	s := mcpserver.New(b, "mcpx", "test")
	s.Passthrough = []string{"up"}

	m := handle(t, s, "tools/call", map[string]any{"name": "hidden_hook", "arguments": map[string]any{}})
	if errCode(m) != 0 {
		t.Fatalf("an unlisted name should go to the upstream, got %v", m)
	}
	b.mu.Lock()
	tools := b.tools
	b.mu.Unlock()
	if len(tools) != 1 || tools[0] != "hidden_hook" {
		t.Fatalf("upstream was called with %v", tools)
	}

	// mcpx's own tools are still mcpx's.
	m = handle(t, s, "tools/call", map[string]any{"name": "mcpx_namespaces", "arguments": map[string]any{}})
	if errCode(m) != 0 || len(b.tools) != 1 {
		t.Fatalf("a gateway tool went to the upstream: %v %v", m, b.tools)
	}

	// Without pass-through an unknown name is still -32602.
	plain := mcpserver.New(newBackend(), "mcpx", "test")
	if c := errCode(handle(t, plain, "tools/call", map[string]any{"name": "hidden_hook"})); c != -32602 {
		t.Fatalf("unknown tool without pass-through: code %d, want -32602", c)
	}
}

// Answers sent with the first request, before anything was asked, answer the
// question they fit when the upstream asks it; keys that fit nothing are
// ignored (SEP-2322: servers SHOULD ignore what they do not recognise). They
// used to be stripped with the protocol fields, and the client was asked
// again for what it had already said.
func TestInputResponsesSentUpFrontAnswerTheQuestionTheyFit(t *testing.T) {
	asker := &scriptedAsker{text: "done", questions: []mcpserver.Question{{
		ID: "elc-1", Key: "user_name", Method: "elicitation/create", Mode: "form", Server: "up",
		Params: json.RawMessage(`{"message":"name?","requestedSchema":{"type":"object"}}`),
	}}}
	srv := mcpserver.New(newBackend(), "mcpx", "test")
	srv.Ask = asker
	srv.Timing = mcpserver.Timing{AskPoll: 5 * time.Millisecond}

	resp := srv.Handle(context.Background(), mcpserver.Request(1, "tools/call", modernCall(1, map[string]any{
		"inputResponses": map[string]any{
			"user_name":          map[string]any{"action": "accept", "content": map[string]any{"name": "Alice"}},
			"unknown_extra_key":  map[string]any{"action": "accept"},
			"another_unexpected": map[string]any{"action": "accept"},
		},
	})))
	body := protoJSON(t, resp)
	if strings.Contains(body, "input_required") || !strings.Contains(body, "done") {
		t.Fatalf("answered up front, the call should complete:\n%s", body)
	}
	if len(asker.answers) != 1 || asker.answers["elc-1"] == nil {
		t.Errorf("only the fitting answer should be relayed, by question id: %v", asker.answers)
	}
}

// A roots/list from the upstream may be put to a client that declared roots,
// and only to one that did.
func TestRootsQuestionsGoOnlyToAClientThatDeclaredRoots(t *testing.T) {
	q := mcpserver.Question{ID: "r-1", Method: "roots/list"}
	with := mcpserver.Peer{Version: modern, Modern: true, Caps: map[string]json.RawMessage{"roots": json.RawMessage(`{}`)}}
	without := mcpserver.Peer{Version: modern, Modern: true, Caps: map[string]json.RawMessage{"elicitation": json.RawMessage(`{}`)}}
	if !q.Sendable(with) {
		t.Error("a client that declared roots can be asked for them")
	}
	if q.Sendable(without) {
		t.Error("a client that did not declare roots must not be asked for them")
	}
}

// A legacy POST whose answer is slow opens its event stream and keeps it
// warm, rather than sending nothing at all until the upstream answers.
// Silence for pool.callTimeout (two minutes) outlasted the official suite's
// 30-second scenario, and any idle-timeout proxy in between.
func TestASlowLegacyAnswerIsKeptAlive(t *testing.T) {
	b := newRelay()
	b.delay = 150 * time.Millisecond
	s := mcpserver.New(b, "mcpx", "test")
	s.Passthrough = []string{"up"}
	s.Timing.SSEKeepAlive = 20 * time.Millisecond
	id := legacySession(t, s, "2025-11-25")
	headers := map[string]string{"Mcp-Session-Id": id, "MCP-Protocol-Version": "2025-11-25",
		"Accept": "application/json, text/event-stream"}

	w := post(s, frame(2, "tools/call", map[string]any{"name": "greet", "arguments": map[string]any{}}), headers)
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type %q, want an event stream:\n%s", ct, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, ": keep-alive") {
		t.Errorf("a quiet exchange should carry keep-alives:\n%s", body)
	}
	if !strings.Contains(body, `"id":2`) || !strings.Contains(body, "called up.greet") {
		t.Errorf("the answer should end the stream:\n%s", body)
	}

	// A client that offered only JSON keeps getting JSON.
	headers["Accept"] = "application/json"
	w = post(s, frame(3, "tools/call", map[string]any{"name": "greet", "arguments": map[string]any{}}), headers)
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type %q for a JSON-only client", ct)
	}
}
