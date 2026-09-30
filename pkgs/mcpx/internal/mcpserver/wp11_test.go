package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// Tests for the WP11 conflicts. Each case is named for the requirement it
// pins, <rev>/<area>/<requirement>, with the spec URL beside it.

// identityBackend records the identity each call was made under.
type identityBackend struct {
	*fakeBackend
	mu   sync.Mutex
	seen []mcpserver.Identity
}

func (b *identityBackend) Call(ctx context.Context, ns, tool string, args json.RawMessage) (string, error) {
	b.mu.Lock()
	b.seen = append(b.seen, mcpserver.IdentityFrom(ctx))
	b.mu.Unlock()
	return b.fakeBackend.Call(ctx, ns, tool, args)
}

func (b *identityBackend) last(t *testing.T) mcpserver.Identity {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.seen) == 0 {
		t.Fatal("the backend was never called")
	}
	return b.seen[len(b.seen)-1]
}

func echoCallParams(meta map[string]any) map[string]any {
	p := map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "demo", "tool": "echo"}}
	if meta != nil {
		p["_meta"] = meta
	}
	return p
}

func postMCP(t *testing.T, h http.Handler, session string, frame any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(frame)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	var f struct {
		Method string `json:"method"`
		Params struct {
			Name string         `json:"name"`
			Meta map[string]any `json:"_meta"`
		} `json:"params"`
	}
	_ = json.Unmarshal(b, &f)
	if v, _ := f.Params.Meta[mcpserver.MetaProtocolVersion].(string); v != "" {
		req.Header.Set("MCP-Protocol-Version", v)
		req.Header.Set("Mcp-Method", f.Method)
		if f.Params.Name != "" {
			req.Header.Set("Mcp-Name", f.Params.Name)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// Conflict #2. The identity a backend call carries is the connection's, not
// the process's: inside the daemon the process is the daemon, and every HTTP
// client resolved to it.
func TestEachConnectionHasItsOwnIdentity(t *testing.T) {
	b := &identityBackend{fakeBackend: newBackend()}
	s := mcpserver.New(b, "mcpx", "test")

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#session-management
	t.Run("2025-11-25/transport/each-http-session-is-its-own-identity", func(t *testing.T) {
		var keys []string
		for i := 0; i < 2; i++ {
			init := postMCP(t, s, "", mcpserver.Request(1, "initialize",
				map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}}))
			sess := init.Header().Get("Mcp-Session-Id")
			if sess == "" {
				t.Fatal("no session minted")
			}
			postMCP(t, s, sess, mcpserver.Request(2, "tools/call", echoCallParams(nil)))
			id := b.last(t)
			if id.Source != mcpserver.IdentitySession || id.Key != sess {
				t.Fatalf("session %s called as %+v", sess, id)
			}
			keys = append(keys, id.Key)
		}
		if keys[0] == keys[1] {
			t.Fatalf("two sessions, one identity: %v", keys)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports
	t.Run("2026-07-28/transport/sessionless-request-has-no-identity", func(t *testing.T) {
		postMCP(t, s, "", mcpserver.Request(3, "tools/call", echoCallParams(modernParams(nil)["_meta"].(map[string]any))))
		if id := b.last(t); id.Source != mcpserver.IdentityNone || id.Key != "" {
			t.Fatalf("a 2026-07-28 request names no client, got %+v", id)
		}
	})

	t.Run("2026-07-28/transport/client-may-name-itself-under-dev.mcpx/session", func(t *testing.T) {
		meta := modernParams(nil)["_meta"].(map[string]any)
		meta[mcpserver.MetaSession] = "agent-7"
		postMCP(t, s, "", mcpserver.Request(4, "tools/call", echoCallParams(meta)))
		if id := b.last(t); id.Source != mcpserver.IdentityClient || id.Key != "agent-7" {
			t.Fatalf("got %+v", id)
		}
	})

	t.Run("2025-11-25/transport/stdio-connection-is-the-process", func(t *testing.T) {
		s.Handle(context.Background(), mcpserver.Request(5, "tools/call", echoCallParams(nil)))
		if id := b.last(t); id.Source != mcpserver.IdentityProcess {
			t.Fatalf("got %+v", id)
		}
	})
}

// Conflict #9b: mcpx's own tool list is fixed for the life of the server, so
// declaring listChanged was a promise about upstream lists it does not show.
func TestToolsListChangedIsNeverDeclared(t *testing.T) {
	n := &fakeNotifier{got: make(chan mcpserver.ListenFilter, 4), fire: make(chan [2]any, 4)}
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Notify = n
	s.SetPush(func(string, any) {})

	// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#capabilities
	for _, rev := range []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"} {
		t.Run(rev+"/tools/listChanged-not-declared-for-a-fixed-list", func(t *testing.T) {
			resp := s.Handle(context.Background(), mcpserver.Request(1, "initialize",
				map[string]any{"protocolVersion": rev}))
			b, _ := json.Marshal(resp)
			if !strings.Contains(string(b), `"tools":{"listChanged":false}`) {
				t.Fatalf("%s", b)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/subscriptions#acknowledgment
	t.Run("2026-07-28/subscriptions/toolsListChanged-never-agreed", func(t *testing.T) {
		rec := &recorder{}
		c := s.ConnWithSend("sess-tools", rec.send)
		s.HandleOn(context.Background(), c, mcpserver.Request(7, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"toolsListChanged": true}})))
		ack := rec.wait(t, 1)[0]
		p, _ := ack["params"].(map[string]any)
		agreed, _ := p["notifications"].(map[string]any)
		if agreed["toolsListChanged"] == true {
			t.Fatalf("agreed to a notification mcpx has no occasion to send: %v", p)
		}
		b, _ := json.Marshal(s.Handle(context.Background(), mcpserver.Request(2, "server/discover", nil)))
		if !strings.Contains(string(b), `"tools":{"listChanged":false}`) {
			t.Fatalf("discover: %s", b)
		}
	})
}

func rpcCode(t *testing.T, resp any) int {
	t.Helper()
	b, _ := json.Marshal(resp)
	var doc struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(b, &doc)
	if doc.Error == nil {
		return 0
	}
	return doc.Error.Code
}

// Conflict #9c: completion/complete answered from mcpx's own tool names and
// ignored ref, while /v1/complete asked the upstream server.
func TestCompletionIsForwardedToTheOwningServer(t *testing.T) {
	complete := func(s *mcpserver.Server, ref map[string]any) any {
		return s.Handle(context.Background(), mcpserver.Request(1, "completion/complete",
			map[string]any{"ref": ref, "argument": map[string]any{"name": "text", "value": ""}}))
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/completion#requesting-completions
	t.Run("2025-11-25/completion/prompt-ref-answered-by-the-owning-server", func(t *testing.T) {
		b, _ := json.Marshal(complete(mcpserver.New(newBackend(), "mcpx", "test"),
			map[string]any{"type": "ref/prompt", "name": "demo_summarise"}))
		if !strings.Contains(string(b), "from-upstream-1") || strings.Contains(string(b), "mcpx_") {
			t.Fatalf("not the upstream's values: %s", b)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/completion#error-handling
	t.Run("2025-11-25/completion/unknown-prompt-is-invalid-params", func(t *testing.T) {
		if c := rpcCode(t, complete(mcpserver.New(newBackend(), "mcpx", "test"),
			map[string]any{"type": "ref/prompt", "name": "nosuch"})); c != -32602 {
			t.Fatalf("code %d", c)
		}
	})
	t.Run("2025-11-25/completion/malformed-ref-is-invalid-params", func(t *testing.T) {
		if c := rpcCode(t, complete(mcpserver.New(newBackend(), "mcpx", "test"),
			map[string]any{"type": "ref/tool", "name": "x"})); c != -32602 {
			t.Fatalf("code %d", c)
		}
	})
	t.Run("2025-11-25/completion/upstream-failure-is-internal-error", func(t *testing.T) {
		if c := rpcCode(t, complete(mcpserver.New(newBackend(), "mcpx", "test"),
			map[string]any{"type": "ref/prompt", "name": "broken"})); c != -32603 {
			t.Fatalf("code %d", c)
		}
	})
	// completion.maxValues is a hot setting; the server was built once, so
	// the cap has to be read per request.
	t.Run("2025-11-25/completion/cap-follows-a-changed-setting", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		max := 3
		s.MaxCompletions = func() int { return max }
		ref := map[string]any{"type": "ref/prompt", "name": "demo_summarise"}
		b, _ := json.Marshal(complete(s, ref))
		if !strings.Contains(string(b), `"hasMore":false`) {
			t.Fatalf("%s", b)
		}
		max = 1
		b, _ = json.Marshal(complete(s, ref))
		if !strings.Contains(string(b), `"values":["from-upstream-1"]`) || !strings.Contains(string(b), `"hasMore":true`) {
			t.Fatalf("the new cap was not applied: %s", b)
		}
	})
}

// Conflict #12: prompts/get answered every failure with -32602.
func TestPromptErrorsAreClassified(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	get := func(name string) any {
		return s.Handle(context.Background(), mcpserver.Request(1, "prompts/get",
			map[string]any{"name": name}))
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/server/prompts#error-handling
	t.Run("2025-11-25/prompts/unknown-prompt-is-invalid-params", func(t *testing.T) {
		if c := rpcCode(t, get("nosuch")); c != -32602 {
			t.Fatalf("code %d", c)
		}
	})
	t.Run("2025-11-25/prompts/upstream-failure-is-internal-error", func(t *testing.T) {
		if c := rpcCode(t, get("broken")); c != -32603 {
			t.Fatalf("code %d", c)
		}
	})
}

// resources-read-binary: a binary resource is a BlobResourceContents, not
// text. mcpx flattened every read into text.
func TestBinaryResourcesStayBlobs(t *testing.T) {
	blobOf := func(t *testing.T, resp any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(resp)
		var doc struct {
			Result struct {
				Contents []map[string]any `json:"contents"`
			} `json:"result"`
		}
		if err := json.Unmarshal(b, &doc); err != nil || len(doc.Result.Contents) != 1 {
			t.Fatalf("%s", b)
		}
		return doc.Result.Contents[0]
	}
	check := func(t *testing.T, c map[string]any) {
		t.Helper()
		if c["blob"] != "iVBORw0KGgo=" || c["mimeType"] != "image/png" || c["uri"] != "demo://logo" {
			t.Fatalf("not a blob: %v", c)
		}
		if _, has := c["text"]; has {
			t.Fatalf("a blob entry carries no text: %v", c)
		}
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#binary-content
	t.Run("2025-11-25/resources/binary-read-returns-blob-with-mimeType", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		check(t, blobOf(t, s.Handle(context.Background(), mcpserver.Request(1, "resources/read",
			map[string]any{"uri": "demo://logo"}))))
	})
	// The ask path renders its own result; it has to keep the blob too.
	t.Run("2026-07-28/resources/binary-read-via-ask-returns-blob", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Ask = blobAsker{}
		check(t, blobOf(t, s.Handle(context.Background(), mcpserver.Request(1, "resources/read",
			modernWith(`{"elicitation":{}}`, map[string]any{"uri": "demo://logo"})))))
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/server/resources#error-handling
	t.Run("2026-07-28/resources/not-found-via-ask-is-invalid-params-with-uri", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Ask = blobAsker{fail: true, err: fmt.Errorf("%w: gone", mcpserver.ErrResourceNotFound)}
		resp := s.Handle(context.Background(), mcpserver.Request(1, "resources/read",
			modernWith(`{"elicitation":{}}`, map[string]any{"uri": "demo://logo"})))
		if c := rpcCode(t, resp); c != -32602 || !strings.Contains(protoJSON(t, resp), `"uri":"demo://logo"`) {
			t.Fatalf("code %d: %s", c, protoJSON(t, resp))
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/server/prompts#error-handling
	t.Run("2026-07-28/prompts/invalid-via-ask-is-invalid-params", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Ask = blobAsker{fail: true, err: fmt.Errorf("%w: missing text", mcpserver.ErrInvalidParams)}
		if c := rpcCode(t, s.Handle(context.Background(), mcpserver.Request(1, "prompts/get",
			modernWith(`{"elicitation":{}}`, map[string]any{"name": "demo_summarise"})))); c != -32602 {
			t.Fatalf("code %d", c)
		}
	})
	t.Run("2026-07-28/resources/failed-read-via-ask-is-an-error-not-contents", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Ask = blobAsker{fail: true}
		if c := rpcCode(t, s.Handle(context.Background(), mcpserver.Request(1, "resources/read",
			modernWith(`{"elicitation":{}}`, map[string]any{"uri": "demo://logo"})))); c != -32603 {
			t.Fatalf("code %d", c)
		}
	})
}

type blobAsker struct {
	fail bool
	err  error
}

func (blobAsker) Begin(context.Context, string, json.RawMessage) (string, error) { return "c", nil }
func (a blobAsker) Poll(context.Context, string, time.Duration) (mcpserver.Outcome, error) {
	if a.fail {
		return mcpserver.Outcome{Done: true, IsError: true, Text: "upstream timed out", Err: a.err}, nil
	}
	return mcpserver.Outcome{Done: true, Contents: []mcpserver.ResourceContents{
		{MimeType: "image/png", Blob: "iVBORw0KGgo="}}}, nil
}
func (blobAsker) Reply(context.Context, string, map[string]json.RawMessage) error { return nil }
func (blobAsker) Abandon(string)                                                  {}

// A 2025-11-25 task whose call is asking its client something shows
// input_required, and the question names the task.
func TestATaskWaitingOnItsClientShowsInputRequired(t *testing.T) {
	asker := &heldAsker{scriptedAsker: &scriptedAsker{questions: oneQuestion(), text: "done"},
		release: make(chan struct{})}
	defer close(asker.release)
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Ask = asker
	s.Timing = mcpserver.Timing{TaskPoll: 250 * time.Millisecond, AskPoll: 20 * time.Millisecond}
	rec := &recorder{}
	c := s.ConnWithSend("sess-task", rec.send)
	s.HandleOn(context.Background(), c, mcpserver.Request(1, "initialize", map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]any{"elicitation": map[string]any{"form": map[string]any{}}}}))

	b, _ := json.Marshal(s.HandleOn(context.Background(), c, mcpserver.Request(2, "tools/call",
		map[string]any{"name": "mcpx_call", "arguments": map[string]any{"namespace": "x", "tool": "y"},
			"task": map[string]any{"ttl": 60000}})))
	var created struct {
		Result struct {
			Task struct {
				TaskID       string `json:"taskId"`
				PollInterval int64  `json:"pollInterval"`
			} `json:"task"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &created); err != nil || created.Result.Task.TaskID == "" {
		t.Fatalf("%s", b)
	}
	taskID := created.Result.Task.TaskID

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks#task-creation
	t.Run("2025-11-25/tasks/pollInterval-comes-from-the-setting", func(t *testing.T) {
		if created.Result.Task.PollInterval != 250 {
			t.Fatalf("pollInterval %d", created.Result.Task.PollInterval)
		}
	})

	frame := rec.wait(t, 1)[0]
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks#input-required-status
	t.Run("2025-11-25/tasks/question-carries-related-task-metadata", func(t *testing.T) {
		p, _ := frame["params"].(map[string]any)
		meta, _ := p["_meta"].(map[string]any)
		rt, _ := meta["io.modelcontextprotocol/related-task"].(map[string]any)
		if frame["method"] != "elicitation/create" || rt["taskId"] != taskID {
			t.Fatalf("%v", frame)
		}
	})
	status := func() string {
		b, _ := json.Marshal(s.HandleOn(context.Background(), c,
			mcpserver.Request(3, "tasks/get", map[string]any{"taskId": taskID})))
		var g struct {
			Result struct {
				Status string `json:"status"`
			} `json:"result"`
		}
		_ = json.Unmarshal(b, &g)
		return g.Result.Status
	}
	t.Run("2025-11-25/tasks/status-is-input-required-while-asking", func(t *testing.T) {
		if st := status(); st != "input_required" {
			t.Fatalf("status %q", st)
		}
	})
	id, _ := frame["id"].(float64)
	if !c.DeliverForTest(int64(id), json.RawMessage(`{"action":"accept","content":{"repo":"r"}}`)) {
		t.Fatal("nobody was waiting for the answer")
	}
	t.Run("2025-11-25/tasks/status-leaves-input-required-once-answered", func(t *testing.T) {
		// Strictly working: the call is still running (heldAsker has not
		// let it finish), so completed here would mean the status was never
		// restored and only the end of the task hid it.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if st := status(); st == "working" {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("status stuck at %q", status())
	})
}

// heldAsker is scriptedAsker whose call, once answered, keeps running until
// released.
type heldAsker struct {
	*scriptedAsker
	mu       sync.Mutex
	release  chan struct{}
	answered bool
}

func (a *heldAsker) Reply(ctx context.Context, id string, answers map[string]json.RawMessage) error {
	a.mu.Lock()
	a.answered = true
	a.mu.Unlock()
	return a.scriptedAsker.Reply(ctx, id, answers)
}

func (a *heldAsker) Poll(ctx context.Context, id string, wait time.Duration) (mcpserver.Outcome, error) {
	a.mu.Lock()
	answered := a.answered
	a.mu.Unlock()
	if answered {
		select {
		case <-a.release:
		default:
			return mcpserver.Outcome{}, nil
		}
	}
	return a.scriptedAsker.Poll(ctx, id, wait)
}
