package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

type fakeBackend struct{ calls map[string]int }

func newBackend() *fakeBackend { return &fakeBackend{calls: map[string]int{}} }

func (f *fakeBackend) hit(n string) { f.calls[n]++ }

func (f *fakeBackend) Namespaces(context.Context) (string, error) {
	f.hit("namespaces")
	return "alpha  2 tools", nil
}
func (f *fakeBackend) Catalog(_ context.Context, budget int, bias string) (string, error) {
	f.hit("catalog")
	return "catalog budget=" + itoa(budget) + " bias=" + bias, nil
}
func (f *fakeBackend) Types(_ context.Context, ns []string) (string, error) {
	f.hit("types")
	return "types " + strings.Join(ns, ","), nil
}
func (f *fakeBackend) Search(_ context.Context, q string, limit int) (string, error) {
	f.hit("search")
	return "search " + q, nil
}
func (f *fakeBackend) Call(_ context.Context, ns, tool string, _ json.RawMessage) (string, error) {
	f.hit("call")
	return "called " + ns + "." + tool, nil
}
func (f *fakeBackend) Exec(_ context.Context, src string, _ int) (string, error) {
	f.hit("exec")
	return "ran " + src, nil
}
func (f *fakeBackend) Log(context.Context, string, string, string, int) (string, error) {
	f.hit("log")
	return "records", nil
}
func (f *fakeBackend) Stats(_ context.Context, d string) (string, error) {
	f.hit("stats")
	return "stats " + d, nil
}
func (f *fakeBackend) Status(context.Context) (string, error) { f.hit("status"); return "{}", nil }
func (f *fakeBackend) RegistrySearch(_ context.Context, q string, _ int) (string, error) {
	f.hit("registry")
	return "registry " + q, nil
}

func (f *fakeBackend) Resources(context.Context) ([]mcpserver.ResourceRef, error) {
	f.hit("resources")
	return []mcpserver.ResourceRef{{URI: "demo://a", Name: "a"}}, nil
}
func (f *fakeBackend) Prompts(context.Context) ([]mcpserver.PromptRef, error) {
	f.hit("prompts")
	return []mcpserver.PromptRef{{Name: "summarise", Description: "d"}}, nil
}
func (f *fakeBackend) ReadResource(_ context.Context, uri string) (string, string, error) {
	f.hit("readResource")
	return "contents of " + uri, "text/plain", nil
}
func (f *fakeBackend) GetPrompt(_ context.Context, name string, _ map[string]string) (string, error) {
	f.hit("getPrompt")
	return "rendered " + name, nil
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func call(t *testing.T, s *mcpserver.Server, name string, args any) (string, bool) {
	t.Helper()
	resp := s.Handle(context.Background(), mcpserver.Request(1, "tools/call", map[string]any{
		"name": name, "arguments": args,
	}))
	text, isErr, err := mcpserver.ResultOf(resp)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return text, isErr
}

func TestInitializeReportsWhatTheServerIs(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "initialize",
		map[string]any{"protocolVersion": "2024-11-05"}))
	b, _ := json.Marshal(resp)
	var doc struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			Instructions    string `json:"instructions"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	// A client speaking an older revision is better served than refused.
	if doc.Result.ProtocolVersion != "2024-11-05" {
		t.Errorf("the client's version should be echoed: %q", doc.Result.ProtocolVersion)
	}
	if doc.Result.ServerInfo.Name != "mcpx" {
		t.Errorf("got %q", doc.Result.ServerInfo.Name)
	}
	if !strings.Contains(doc.Result.Instructions, "mcpx_exec") {
		t.Error("the instructions should point at the tool that saves context")
	}
}

func TestTheToolSurfaceIsSmall(t *testing.T) {
	// The whole reason mcpx exists is that many schemas crowd out the work.
	// Exposing many again over MCP would rebuild the problem with extra steps.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	if n := len(s.Tools()); n > 12 {
		t.Errorf("the surface has grown to %d tools; that defeats the point", n)
	}
	for _, tool := range s.Tools() {
		if tool.Description == "" {
			t.Errorf("%s is undocumented", tool.Name)
		}
		var doc map[string]any
		if err := json.Unmarshal(tool.InputSchema, &doc); err != nil {
			t.Errorf("%s has an invalid schema: %v", tool.Name, err)
		}
		if doc["type"] != "object" {
			t.Errorf("%s: MCP arguments are an object", tool.Name)
		}
	}
}

func TestEveryToolReachesItsBackend(t *testing.T) {
	f := newBackend()
	s := mcpserver.New(f, "mcpx", "test")
	for _, c := range []struct {
		tool string
		args any
		want string
	}{
		{"mcpx_namespaces", map[string]any{}, "namespaces"},
		{"mcpx_catalog", map[string]any{"budget": 500}, "catalog"},
		{"mcpx_types", map[string]any{"namespaces": []string{"a"}}, "types"},
		{"mcpx_search", map[string]any{"query": "x"}, "search"},
		{"mcpx_call", map[string]any{"namespace": "a", "tool": "b"}, "call"},
		{"mcpx_exec", map[string]any{"source": "1"}, "exec"},
		{"mcpx_log", map[string]any{}, "log"},
		{"mcpx_stats", map[string]any{}, "stats"},
		{"mcpx_status", map[string]any{}, "status"},
		{"mcpx_registry", map[string]any{"query": "weather"}, "registry"},
	} {
		if _, isErr := call(t, s, c.tool, c.args); isErr {
			t.Errorf("%s reported an error", c.tool)
		}
		if f.calls[c.want] == 0 {
			t.Errorf("%s never reached the backend", c.tool)
		}
	}
}

func TestADottedNameInTheNamespaceFieldIsAccepted(t *testing.T) {
	// It is how the tools are written everywhere else, so it is what somebody
	// will send.
	f := newBackend()
	s := mcpserver.New(f, "mcpx", "test")
	text, isErr := call(t, s, "mcpx_call", map[string]any{"namespace": "alpha.read"})
	if isErr {
		t.Fatalf("should have been accepted: %s", text)
	}
	if !strings.Contains(text, "alpha.read") {
		t.Errorf("got %q", text)
	}
}

func TestAToolFailureIsAResultNotAProtocolError(t *testing.T) {
	// A protocol error means the client did something wrong, and a client
	// that retries the wrong thing on a tool failure never converges.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	text, isErr := call(t, s, "mcpx_types", map[string]any{})
	if !isErr {
		t.Fatal("a missing required argument should be flagged")
	}
	if !strings.Contains(text, "required") {
		t.Errorf("the reason should be in the result: %q", text)
	}
	resp := s.Handle(context.Background(), mcpserver.Request(1, "tools/call",
		map[string]any{"name": "mcpx_types", "arguments": map[string]any{}}))
	b, _ := json.Marshal(resp)
	if strings.Contains(string(b), `"error"`) {
		t.Errorf("it should not be a JSON-RPC error: %s", b)
	}
}

func TestAnUnknownMethodIsAProtocolError(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "nonsense", nil))
	b, _ := json.Marshal(resp)
	if !strings.Contains(string(b), "-32601") {
		t.Errorf("expected method-not-found: %s", b)
	}
}

func TestANotificationGetsNoReply(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	if resp := s.Handle(context.Background(),
		mcpserver.Request(0, "notifications/initialized", nil)); resp != nil {
		t.Error("a notification has no reply by definition")
	}
}

func TestExtraToolsAppearAndAreCallable(t *testing.T) {
	// An adapted program should be a tool in its own right, not something
	// reachable only through a script.
	f := newBackend()
	s := mcpserver.New(f, "mcpx", "test").WithExtras([]mcpserver.Extra{{
		Tool: mcpserver.Tool{
			Name: "gitx_log", Description: "commits",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
		Call: func(context.Context, json.RawMessage) (string, error) { return "a commit", nil },
	}})
	found := false
	for _, tool := range s.Tools() {
		if tool.Name == "gitx_log" {
			found = true
		}
	}
	if !found {
		t.Fatal("the extra tool should be listed")
	}
	if text, isErr := call(t, s, "gitx_log", map[string]any{}); isErr || text != "a commit" {
		t.Errorf("got %q err=%v", text, isErr)
	}
}

func TestStdioAnswersFramesInOrder(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out strings.Builder
	if err := s.ServeStdio(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one reply per request, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[1], "mcpx_exec") {
		t.Errorf("the second reply should be the tool list: %s", lines[1])
	}
}

func TestAMalformedFrameIsReportedAndTheStreamContinues(t *testing.T) {
	// A host that sends one bad frame should not have to reconnect.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	in := strings.NewReader("not json\n" + `{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n")
	var out strings.Builder
	if err := s.ServeStdio(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-32700") {
		t.Errorf("the parse error should be reported: %s", out.String())
	}
	if strings.Count(out.String(), "\n") != 2 {
		t.Errorf("the stream should have continued: %s", out.String())
	}
}

func TestResourcesAndPromptsArePassedThroughNotFakedEmpty(t *testing.T) {
	// Declaring the capability and then returning nothing is a lie a client
	// cannot detect: it asks once, gets an empty list, and never asks again.
	f := newBackend()
	s := mcpserver.New(f, "mcpx", "test")

	for _, c := range []struct{ method, want string }{
		{"resources/list", "demo://a"},
		{"prompts/list", "summarise"},
	} {
		resp := s.Handle(context.Background(), mcpserver.Request(1, c.method, nil))
		b, _ := json.Marshal(resp)
		if !strings.Contains(string(b), c.want) {
			t.Errorf("%s should pass through: %s", c.method, b)
		}
	}
}

func TestReadingAResourceAndRenderingAPromptReachTheBackend(t *testing.T) {
	f := newBackend()
	s := mcpserver.New(f, "mcpx", "test")

	resp := s.Handle(context.Background(), mcpserver.Request(1, "resources/read",
		map[string]any{"uri": "demo://a"}))
	b, _ := json.Marshal(resp)
	if !strings.Contains(string(b), "contents of demo://a") {
		t.Errorf("got %s", b)
	}

	resp = s.Handle(context.Background(), mcpserver.Request(2, "prompts/get",
		map[string]any{"name": "summarise", "arguments": map[string]string{"x": "y"}}))
	b, _ = json.Marshal(resp)
	if !strings.Contains(string(b), "rendered summarise") {
		t.Errorf("got %s", b)
	}
	// A prompt result is messages, not content: a client that expects the
	// tool shape will not find the text.
	if !strings.Contains(string(b), `"messages"`) {
		t.Errorf("a prompt reply should carry messages: %s", b)
	}
}

func TestCapabilitiesMatchWhatIsAnswered(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "initialize", nil))
	b, _ := json.Marshal(resp)
	for _, want := range []string{`"tools"`, `"resources"`, `"prompts"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s should be advertised: %s", want, b)
		}
	}
}
