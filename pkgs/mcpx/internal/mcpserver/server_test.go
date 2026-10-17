package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// fakeBackend counts calls under a lock: stdio requests run concurrently, so
// an unguarded map here is a fatal "concurrent map writes" waiting for a run
// where two requests land at once.
type fakeBackend struct {
	mu    sync.Mutex
	calls map[string]int
}

func newBackend() *fakeBackend { return &fakeBackend{calls: map[string]int{}} }

func (f *fakeBackend) hit(n string) {
	f.mu.Lock()
	f.calls[n]++
	f.mu.Unlock()
}

func (f *fakeBackend) count(n string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[n]
}

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
func (f *fakeBackend) ReadResource(_ context.Context, uri string) ([]mcpserver.ResourceContents, error) {
	f.hit("readResource")
	if uri == "demo://logo" {
		return []mcpserver.ResourceContents{{MimeType: "image/png", Blob: "iVBORw0KGgo="}}, nil
	}
	return []mcpserver.ResourceContents{{MimeType: "text/plain", Text: "contents of " + uri}}, nil
}
func (f *fakeBackend) GetPrompt(_ context.Context, name string, _ map[string]string) (string, error) {
	f.hit("getPrompt")
	switch name {
	case "nosuch":
		return "", fmt.Errorf("%w: no prompt named %q", mcpserver.ErrInvalidParams, name)
	case "broken":
		return "", errors.New("upstream timed out")
	}
	return "rendered " + name, nil
}

// Complete stands in for the upstream server that owns a ref: values only it
// could know, so a test can tell who answered.
func (f *fakeBackend) Complete(_ context.Context, params json.RawMessage) ([]string, error) {
	f.hit("complete")
	var p struct {
		Ref struct {
			Name string `json:"name"`
		} `json:"ref"`
	}
	_ = json.Unmarshal(params, &p)
	switch p.Ref.Name {
	case "nosuch":
		return nil, fmt.Errorf("%w: no prompt named %q", mcpserver.ErrInvalidParams, p.Ref.Name)
	case "broken":
		return nil, errors.New("upstream timed out")
	}
	return []string{"from-upstream-1", "from-upstream-2", "from-upstream-3"}, nil
}

func (f *fakeBackend) ResourceTemplates(context.Context) ([]mcpserver.ResourceRef, error) {
	f.hit("templates")
	return []mcpserver.ResourceRef{{URI: "demo://item/{id}", Name: "item"}}, nil
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
		map[string]any{"protocolVersion": "2025-06-18"}))
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
	// An unsupported version is refused with the list that would work, not
	// agreed to. Echoing it was the bug: a client asking for something mcpx
	// cannot serve was told yes.
	// A supported legacy version is agreed to as asked.
	if doc.Result.ProtocolVersion != "2025-06-18" {
		t.Errorf("a supported version should be agreed: %q", doc.Result.ProtocolVersion)
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
		if f.count(c.want) == 0 {
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

// Every request gets one reply, matched by id. Not by position: requests run
// concurrently so a cancellation can reach one, and JSON-RPC does not order
// replies.
func TestStdioAnswersEveryRequestByID(t *testing.T) {
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
	byID := map[float64]string{}
	for _, l := range lines {
		var f struct {
			ID float64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(l), &f); err != nil {
			t.Fatal(err)
		}
		byID[f.ID] = l
	}
	if !strings.Contains(byID[2], "mcpx_exec") {
		t.Errorf("the reply to id 2 should be the tool list: %s", byID[2])
	}
	if _, ok := byID[1]; !ok {
		t.Errorf("ping was not answered:\n%s", out.String())
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

func TestAModernVersionCannotBeAgreedOverInitialize(t *testing.T) {
	// A client sending initialize is legacy by definition; the modern
	// revisions have no handshake. Agreeing would promise a protocol neither
	// side is speaking, so it is answered with the latest legacy revision,
	// as for any other version mcpx cannot agree to.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "initialize",
		map[string]any{"protocolVersion": "2026-07-28"}))
	if resp.Error != nil {
		t.Fatalf("initialize is answered with a version, not refused: %v", resp.Error)
	}
	if got := protoJSON(t, resp.Result); !strings.Contains(got, `"protocolVersion":"`+mcpserver.Latest+`"`) {
		t.Errorf("want %s:\n%s", mcpserver.Latest, got)
	}
}

func TestServerDiscoverAnswersForModernClients(t *testing.T) {
	// Mandatory in the modern revisions, and the probe a dual-era client
	// uses to decide which era it is talking to.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "server/discover", nil))
	b, _ := json.Marshal(resp)
	// https://modelcontextprotocol.io/specification/2026-07-28/schema#discoverresult
	t.Run("2026-07-28/discover/result-has-supportedVersions-and-serverInfo-in-meta", func(t *testing.T) {
		var doc struct {
			Result map[string]json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		var versions []string
		if err := json.Unmarshal(doc.Result["supportedVersions"], &versions); err != nil || len(versions) == 0 {
			t.Errorf("supportedVersions must be a non-empty array: %s", b)
		}
		var meta struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"io.modelcontextprotocol/serverInfo"`
		}
		_ = json.Unmarshal(doc.Result["_meta"], &meta)
		if meta.ServerInfo.Name != "mcpx" {
			t.Errorf("serverInfo belongs in _meta: %s", b)
		}
		for _, stale := range []string{"protocolVersions", "serverInfo"} {
			if _, ok := doc.Result[stale]; ok {
				t.Errorf("%q is not a DiscoverResult field: %s", stale, b)
			}
		}
		if _, ok := doc.Result["capabilities"]; !ok {
			t.Errorf("capabilities missing: %s", b)
		}
	})
}

func TestAPerRequestVersionIsHonouredAndChecked(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")

	ok := s.Handle(context.Background(), mcpserver.Request(1, "tools/list", modernParams(nil)))
	b, _ := json.Marshal(ok)
	if !strings.Contains(string(b), "mcpx_exec") {
		t.Errorf("a modern request should be served without a handshake: %s", b)
	}

	bad := s.Handle(context.Background(), mcpserver.Request(2, "tools/list",
		map[string]any{"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion": "1999-01-01"}}))
	b, _ = json.Marshal(bad)
	if !strings.Contains(string(b), "-32022") {
		t.Errorf("an unsupported per-request version should be refused: %s", b)
	}
}

func TestListsArePaginatedSoALargeInstallationIsReadable(t *testing.T) {
	// mcpx fronts every tool of every configured server. A client with a
	// frame limit has no way to read that list except by paging, and
	// ignoring the cursor made a large installation simply unreadable.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.PageSize = 3

	first := s.Handle(context.Background(), mcpserver.Request(1, "tools/list", nil))
	b, _ := json.Marshal(first)
	var page struct {
		Result struct {
			Tools      []map[string]any `json:"tools"`
			NextCursor string           `json:"nextCursor"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Result.Tools) != 3 {
		t.Fatalf("expected a page of 3, got %d", len(page.Result.Tools))
	}
	if page.Result.NextCursor == "" {
		t.Fatal("more remains, so a cursor should have been offered")
	}
	// Opaque, because the specification says so and a client that parses one
	// is relying on something it was told not to.
	if strings.Contains(page.Result.NextCursor, "3") {
		t.Errorf("the cursor should not be a readable offset: %q", page.Result.NextCursor)
	}

	second := s.Handle(context.Background(), mcpserver.Request(2, "tools/list",
		map[string]any{"cursor": page.Result.NextCursor}))
	b, _ = json.Marshal(second)
	var next struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal(b, &next)
	if len(next.Result.Tools) == 0 {
		t.Fatal("the second page should have content")
	}
	if next.Result.Tools[0]["name"] == page.Result.Tools[0]["name"] {
		t.Error("the second page should not repeat the first")
	}
}

func TestAnInvalidCursorIsInvalidParams(t *testing.T) {
	// Every revision's pagination page: an invalid cursor is -32602. The
	// old answer -- page one again -- turned a client that follows
	// nextCursor into one that re-reads the first page forever.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "tools/list",
		map[string]any{"cursor": "not-a-cursor"}))
	if errOf(t, resp) == "" {
		t.Fatal("an invalid cursor was answered with a page")
	}
	b, _ := json.Marshal(resp)
	if !strings.Contains(string(b), `"code":-32602`) {
		t.Errorf("%s", b)
	}
}

// errOf returns a response's JSON-RPC error message, or empty.
func errOf(t *testing.T, resp any) string {
	t.Helper()
	b, _ := json.Marshal(resp)
	var doc struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &doc) != nil || doc.Error == nil {
		return ""
	}
	return doc.Error.Message
}

func TestCancellationIsRecordedRatherThanDropped(t *testing.T) {
	// A client that cancels and sees work continue cannot tell whether the
	// message arrived.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	var gotID, gotReason string
	s.OnCancel = func(id, reason string) { gotID, gotReason = id, reason }

	if resp := s.Handle(context.Background(), mcpserver.Request(0,
		"notifications/cancelled",
		map[string]any{"requestId": 42, "reason": "user pressed escape"})); resp != nil {
		t.Error("a notification has no reply")
	}
	if gotID != "42" || gotReason != "user pressed escape" {
		t.Errorf("the hook should have fired: %q %q", gotID, gotReason)
	}
	if reason, ok := s.Cancelled("42"); !ok || reason == "" {
		t.Errorf("it should be queryable: %q %v", reason, ok)
	}
}

func TestLoggingSetLevelIsAcceptedRatherThanRefused(t *testing.T) {
	// Refusing would make a well-behaved client treat the whole connection
	// as degraded.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "logging/setLevel",
		map[string]any{"level": "info"}))
	if msg := errOf(t, resp); msg != "" {
		t.Errorf("got %s", msg)
	}
}

func TestCapabilitiesMatchWhatIsActuallyAnswered(t *testing.T) {
	// Declaring a capability mcpx cannot serve invites a client to use it
	// and get silence, which is worse than not offering it.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "initialize",
		map[string]any{"protocolVersion": "2025-11-25"}))
	b, _ := json.Marshal(resp)
	for _, want := range []string{"completions", "tools", "resources", "prompts"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%q is answered and should be declared: %s", want, b)
		}
	}
	// Not declared, because mcpx cannot do it. Checked against the
	// capabilities object rather than the whole body, which also carries
	// instructions.
	var doc struct {
		Result struct {
			Capabilities map[string]any `json:"capabilities"`
		} `json:"result"`
	}
	_ = json.Unmarshal(b, &doc)
	if _, declared := doc.Result.Capabilities["sampling"]; declared {
		t.Errorf("sampling is not implemented and must not be declared: %v", doc.Result.Capabilities)
	}
	// logging is: upstream log messages are relayed to a client that set a
	// level, during its calls.
	if _, declared := doc.Result.Capabilities["logging"]; !declared {
		t.Errorf("mcpx relays upstream log messages, so logging must be declared: %v", doc.Result.Capabilities)
	}
}

// fakeNotifier lets a test push notifications at will.
type fakeNotifier struct {
	got  chan mcpserver.ListenFilter
	fire chan [2]any
}

func (f *fakeNotifier) Listen(ctx context.Context, lf mcpserver.ListenFilter, send func(string, any)) {
	f.got <- lf
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-f.fire:
			send(n[0].(string), n[1])
		}
	}
}

func TestSubscriptionsListenStreamsOnlyWhatWasRequested(t *testing.T) {
	// The specification says a server MUST NOT send notification types the
	// client did not request. The filter reaching the notifier is exactly
	// what the client asked for.
	n := &fakeNotifier{got: make(chan mcpserver.ListenFilter, 1), fire: make(chan [2]any, 4)}
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Notify = n

	var out strings.Builder
	var mu sync.Mutex
	s.SetPush(func(method string, params any) {
		mu.Lock()
		out.WriteString(method + "\n")
		mu.Unlock()
	})

	resp := s.Handle(context.Background(), mcpserver.Request(7, "subscriptions/listen",
		map[string]any{"notifications": map[string]any{"resourcesListChanged": true}}))
	if resp != nil {
		t.Errorf("a listen stream is long-lived; its result is withheld: %+v", resp)
	}

	select {
	case lf := <-n.got:
		if !lf.ResourcesListChanged || lf.PromptsListChanged {
			t.Errorf("the filter should be exactly what was asked: %+v", lf)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the notifier was never started")
	}

	n.fire <- [2]any{"notifications/resources/list_changed", map[string]any{}}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := out.String()
		mu.Unlock()
		if strings.Contains(got, "notifications/resources/list_changed") {
			if !strings.Contains(got, "notifications/subscriptions/acknowledged") {
				t.Errorf("the stream should be acknowledged first: %s", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the notification never arrived: %s", out.String())
}

func TestPushCapabilitiesAreDeclaredOnlyWhenSomethingCanPush(t *testing.T) {
	// Claiming listChanged without a notifier invites a client to wait for
	// notifications that will never come.
	quiet := mcpserver.New(newBackend(), "mcpx", "test")
	b, _ := json.Marshal(quiet.Handle(context.Background(),
		mcpserver.Request(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"})))
	if strings.Contains(string(b), `"listChanged":true`) {
		t.Errorf("nothing can push, so nothing should be promised: %s", b)
	}

	// A notifier is not enough. The daemon builds every HTTP connection with
	// no send function, so s.Notify != nil was true there and nothing could
	// ever reach the client: it declared subscribe, accepted
	// subscriptions/listen, acknowledged it and then delivered nothing.
	mute := mcpserver.New(newBackend(), "mcpx", "test")
	mute.Notify = &fakeNotifier{got: make(chan mcpserver.ListenFilter, 1)}
	b, _ = json.Marshal(mute.Handle(context.Background(),
		mcpserver.Request(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"})))
	if strings.Contains(string(b), `"subscribe":true`) {
		t.Errorf("this connection cannot carry a push, so nothing should be promised: %s", b)
	}

	loud := mcpserver.New(newBackend(), "mcpx", "test")
	loud.Notify = &fakeNotifier{got: make(chan mcpserver.ListenFilter, 1)}
	loud.SetPush(func(string, any) {})
	b, _ = json.Marshal(loud.Handle(context.Background(),
		mcpserver.Request(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"})))
	if !strings.Contains(string(b), `"subscribe":true`) {
		t.Errorf("with a notifier and a pushable connection, subscription should be declared: %s", b)
	}

	// list_changed is only ever delivered on a stream the client opened, and
	// only 2026-07-28 has a way to open one. Telling an older client it will
	// receive them is telling it to wait for something with no mechanism.
	b, _ = json.Marshal(loud.Handle(context.Background(),
		mcpserver.Request(2, "initialize", map[string]any{"protocolVersion": "2025-06-18"})))
	// 2025-06-18 has no subscriptions/listen, but it does define
	// notifications/resources/list_changed, which arrives unsolicited. A
	// pushable connection can deliver it, so it must be declared.
	if !strings.Contains(string(b), `"listChanged":true`) {
		t.Errorf("legacy revisions define list_changed and mcpx sends it: %s", b)
	}
	b, _ = json.Marshal(loud.Handle(context.Background(),
		mcpserver.Request(3, "server/discover", nil)))
	if !strings.Contains(string(b), `"listChanged":true`) {
		t.Errorf("2026-07-28 can open a stream, so list_changed should be declared: %s", b)
	}
}

func TestLegacyResourceSubscribeIsAccepted(t *testing.T) {
	n := &fakeNotifier{got: make(chan mcpserver.ListenFilter, 1), fire: make(chan [2]any, 1)}
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Notify = n
	s.SetPush(func(string, any) {})

	resp := s.Handle(context.Background(), mcpserver.Request(1, "resources/subscribe",
		map[string]any{"uri": "demo://a"}))
	if msg := errOf(t, resp); msg != "" {
		t.Fatalf("got %s", msg)
	}
	select {
	case lf := <-n.got:
		if len(lf.ResourceSubscriptions) != 1 || lf.ResourceSubscriptions[0] != "demo://a" {
			t.Errorf("the URI should reach the stream: %+v", lf)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscribing should have started a stream")
	}
}

func TestResourceTemplatesAreServedRatherThanSwallowed(t *testing.T) {
	// mcpx consumed templates and never offered them onward, so a templated
	// resource became invisible one hop down.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	b, _ := json.Marshal(s.Handle(context.Background(),
		mcpserver.Request(1, "resources/templates/list", nil)))
	if !strings.Contains(string(b), "demo://item/{id}") {
		t.Errorf("got %s", b)
	}
}

func TestAToolCallCanRunAsATask(t *testing.T) {
	// For a genuinely slow tool, holding a request open for minutes invites
	// every intermediary to time it out. A task hands back a handle at once.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	resp := s.Handle(context.Background(), mcpserver.Request(1, "tools/call", map[string]any{
		// mcpx_call: a tool that declares taskSupport (2025-11-25).
		"name": "mcpx_call", "arguments": map[string]any{"namespace": "alpha", "tool": "t"},
		"task": map[string]any{"ttl": 60000},
	}))
	b, _ := json.Marshal(resp)
	var got struct {
		Result struct {
			Task struct {
				TaskID string `json:"taskId"`
				Status string `json:"status"`
			} `json:"task"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &got); err != nil || got.Result.Task.TaskID == "" {
		t.Fatalf("a task handle should come back at once: %s", b)
	}
	id := got.Result.Task.TaskID

	// tasks/result waits for it and returns the tool's own result.
	res := s.Handle(context.Background(), mcpserver.Request(2, "tasks/result",
		map[string]any{"taskId": id}))
	text, isErr, err := mcpserver.ResultOf(res)
	if err != nil || isErr {
		t.Fatalf("got %q %v %v", text, isErr, err)
	}
	if !strings.Contains(text, "alpha") {
		t.Errorf("the tool's result should arrive: %q", text)
	}

	// tasks/get reports it completed.
	get := s.Handle(context.Background(), mcpserver.Request(3, "tasks/get",
		map[string]any{"taskId": id}))
	b, _ = json.Marshal(get)
	if !strings.Contains(string(b), `"completed"`) {
		t.Errorf("the task should be completed: %s", b)
	}
}

func TestATaskCanBeListedAndCancelled(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Handle(context.Background(), mcpserver.Request(1, "tools/call", map[string]any{
		"name": "mcpx_call", "arguments": map[string]any{"namespace": "alpha", "tool": "t"}, "task": map[string]any{},
	}))
	list := s.Handle(context.Background(), mcpserver.Request(2, "tasks/list", nil))
	b, _ := json.Marshal(list)
	if !strings.Contains(string(b), "tsk-") {
		t.Fatalf("the task should be listed: %s", b)
	}
	missing := s.Handle(context.Background(), mcpserver.Request(3, "tasks/get",
		map[string]any{"taskId": "tsk-nope"}))
	if errOf(t, missing) == "" {
		t.Error("an unknown task should be an error")
	}
}

func TestTasksAreDeclaredForBothEras(t *testing.T) {
	// Core in 2025-11-25, an extension in 2026-07-28 -- each where its own
	// revision defines it, and never the other's way.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	b, _ := json.Marshal(s.Handle(context.Background(),
		mcpserver.Request(1, "server/discover", nil)))
	if !strings.Contains(string(b), "io.modelcontextprotocol/tasks") {
		t.Errorf("the extension should be declared to a modern client: %s", b)
	}
	if strings.Contains(string(b), `"tasks":{`) {
		t.Errorf("core tasks must not be declared under 2026-07-28: %s", b)
	}
	b, _ = json.Marshal(s.Handle(context.Background(),
		mcpserver.Request(2, "initialize", map[string]any{"protocolVersion": "2025-11-25"})))
	if !strings.Contains(string(b), `"tasks":{`) {
		t.Errorf("core tasks should be declared to 2025-11-25: %s", b)
	}
	if strings.Contains(string(b), "extensions") {
		t.Errorf("2025-11-25 has no extensions capability: %s", b)
	}
}

func TestModernResultsSayTheyAreComplete(t *testing.T) {
	// 2026-07-28 makes resultType mandatory on every result; it is how a
	// client tells a finished result from an input_required one. mcpx sent
	// none, so a strict modern client could not parse any of its replies.
	s := mcpserver.New(newBackend(), "mcpx", "test")
	modern := modernParams(nil)
	for i, method := range []string{"server/discover", "tools/list", "prompts/list"} {
		b, _ := json.Marshal(s.Handle(context.Background(), mcpserver.Request(i+1, method, modern)))
		if !strings.Contains(string(b), `"resultType":"complete"`) {
			t.Errorf("%s: no resultType: %s", method, b)
		}
	}
	// A legacy request is answered exactly as before.
	b, _ := json.Marshal(s.Handle(context.Background(), mcpserver.Request(9, "tools/list", map[string]any{})))
	if strings.Contains(string(b), "resultType") {
		t.Errorf("a legacy result should not grow a field it never asked for: %s", b)
	}
}

// modernParams is a well-formed 2026-07-28 request's params: the two
// per-request fields the specification makes mandatory, plus whatever the
// method needs.
func modernParams(extra map[string]any) map[string]any {
	return modernWith(`{}`, extra)
}

// modernWith is modernParams with declared client capabilities.
func modernWith(caps string, extra map[string]any) map[string]any {
	p := map[string]any{"_meta": map[string]any{
		mcpserver.MetaProtocolVersion:    "2026-07-28",
		mcpserver.MetaClientCapabilities: json.RawMessage(caps),
	}}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func TestBatchExecution(t *testing.T) {
	b := newBackend()
	s := mcpserver.New(b, "mcpx", "test")

	// 1. Successful batch call
	req := mcpserver.Request(1, "tools/call", map[string]any{
		"name": "mcpx_batch",
		"arguments": map[string]any{
			"calls": []map[string]any{
				{"namespace": "alpha", "tool": "one"},
				{"tool": "alpha.two"},
			},
		},
	})
	res := s.Handle(context.Background(), req)
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
	resBytes, _ := json.Marshal(res.Result)
	if !strings.Contains(string(resBytes), "called alpha.one") || !strings.Contains(string(resBytes), "called alpha.two") {
		t.Fatalf("expected both calls executed: %s", string(resBytes))
	}

	// 2. Alias batch_call works
	req2 := mcpserver.Request(2, "tools/call", map[string]any{
		"name": "batch_call",
		"arguments": map[string]any{
			"calls": []map[string]any{
				{"namespace": "alpha", "tool": "three"},
			},
		},
	})
	res2 := s.Handle(context.Background(), req2)
	if res2.Error != nil {
		t.Fatalf("unexpected error: %v", res2.Error)
	}

	// 3. Parallel batch
	req3 := mcpserver.Request(3, "tools/call", map[string]any{
		"name": "batch_call",
		"arguments": map[string]any{
			"parallel": true,
			"calls": []map[string]any{
				{"namespace": "alpha", "tool": "p1"},
				{"namespace": "alpha", "tool": "p2"},
			},
		},
	})
	res3 := s.Handle(context.Background(), req3)
	if res3.Error != nil {
		t.Fatalf("unexpected error: %v", res3.Error)
	}
}
