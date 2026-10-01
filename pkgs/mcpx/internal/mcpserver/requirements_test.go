package mcpserver_test

// Requirement-named tests: each subtest is "<revision>/<area>/<requirement>",
// with the specification URL beside it, so the conformance matrix can be
// assembled from `go test -v` output.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

const spec = "https://modelcontextprotocol.io/specification/"

func handle(t *testing.T, s *mcpserver.Server, method string, params any) map[string]any {
	t.Helper()
	resp := s.Handle(context.Background(), mcpserver.Request(1, method, params))
	if resp == nil {
		t.Fatalf("%s: no response", method)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(protoJSON(t, resp)), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func resultOf(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	r, _ := m["result"].(map[string]any)
	if r == nil {
		t.Fatalf("no result: %v", m)
	}
	return r
}

func errCode(m map[string]any) int {
	e, _ := m["error"].(map[string]any)
	if e == nil {
		return 0
	}
	c, _ := e["code"].(float64)
	return int(c)
}

// ---- 2026-07-28: discover, caching, per-request _meta ----

func TestModernEnvelope(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Cache = mcpserver.Cache{List: 7 * time.Second, Read: 3 * time.Second}

	// spec + "2026-07-28/server/discover"
	t.Run("2026-07-28/discover/result-shape-is-DiscoverResult", func(t *testing.T) {
		r := resultOf(t, handle(t, s, "server/discover", modernParams(nil)))
		vs, _ := r["supportedVersions"].([]any)
		if len(vs) == 0 || vs[0] != "2026-07-28" {
			t.Errorf("supportedVersions: %v", r["supportedVersions"])
		}
		for _, gone := range []string{"protocolVersions", "serverInfo"} {
			if _, ok := r[gone]; ok {
				t.Errorf("%s is not a DiscoverResult field: %v", gone, r)
			}
		}
		if r["resultType"] != "complete" {
			t.Errorf("resultType: %v", r["resultType"])
		}
	})

	// spec + "2026-07-28/basic/index#meta" (per-response fields)
	t.Run("2026-07-28/basic/serverInfo-in-meta-on-every-result", func(t *testing.T) {
		// tools/list rather than ping: 2026-07-28 removed ping, so a modern
		// peer gets -32601 for it and there is no result to carry _meta.
		for _, method := range []string{"server/discover", "tools/list", "prompts/list", "resources/read"} {
			params := modernParams(map[string]any{"uri": "demo://a"})
			r := resultOf(t, handle(t, s, method, params))
			meta, _ := r["_meta"].(map[string]any)
			info, _ := meta[mcpserver.MetaServerInfo].(map[string]any)
			if info["name"] != "mcpx" || info["version"] != "test" {
				t.Errorf("%s: _meta.serverInfo missing: %v", method, r["_meta"])
			}
		}
	})

	// spec + "2026-07-28/server/utilities/caching#cacheable-results"
	for method, want := range map[string]struct {
		scope string
		ttl   float64
	}{
		"server/discover":          {"public", 7000},
		"tools/list":               {"public", 7000},
		"prompts/list":             {"public", 7000},
		"resources/templates/list": {"public", 7000},
		"resources/list":           {"private", 7000},
		"resources/read":           {"private", 3000},
	} {
		t.Run("2026-07-28/caching/"+strings.ReplaceAll(method, "/", "-")+"-has-ttlMs-and-cacheScope", func(t *testing.T) {
			r := resultOf(t, handle(t, s, method, modernParams(map[string]any{"uri": "demo://a"})))
			if r["ttlMs"] != want.ttl || r["cacheScope"] != want.scope {
				t.Errorf("ttlMs=%v cacheScope=%v, want %v %v", r["ttlMs"], r["cacheScope"], want.ttl, want.scope)
			}
		})
	}

	// spec + "2026-07-28/server/utilities/caching#interaction-with-pagination"
	t.Run("2026-07-28/caching/every-page-has-the-same-cacheScope", func(t *testing.T) {
		paged := mcpserver.New(newBackend(), "mcpx", "test")
		paged.PageSize = 3
		first := resultOf(t, handle(t, paged, "tools/list", modernParams(nil)))
		cursor, _ := first["nextCursor"].(string)
		if cursor == "" {
			t.Fatal("expected a second page")
		}
		second := resultOf(t, handle(t, paged, "tools/list", modernParams(map[string]any{"cursor": cursor})))
		if first["cacheScope"] != second["cacheScope"] || second["ttlMs"] == nil {
			t.Errorf("pages disagree: %v / %v", first["cacheScope"], second["cacheScope"])
		}
	})

	t.Run("2026-07-28/caching/legacy-results-carry-no-hints", func(t *testing.T) {
		r := resultOf(t, handle(t, s, "tools/list", nil))
		for _, k := range []string{"ttlMs", "cacheScope", "resultType", "_meta"} {
			if _, ok := r[k]; ok {
				t.Errorf("%s sent to a legacy client: %v", k, r[k])
			}
		}
	})

	// spec + "2026-07-28/basic/index#meta" (per-request fields)
	t.Run("2026-07-28/basic/missing-clientCapabilities-is-32602", func(t *testing.T) {
		m := handle(t, s, "tools/list", map[string]any{"_meta": map[string]any{
			mcpserver.MetaProtocolVersion: "2026-07-28"}})
		if errCode(m) != -32602 {
			t.Errorf("want -32602: %v", m)
		}
	})

	// spec + "2026-07-28/server/utilities/logging#error-handling"
	t.Run("2026-07-28/logging/unrecognized-logLevel-is-32602", func(t *testing.T) {
		p := modernParams(nil)
		p["_meta"].(map[string]any)[mcpserver.MetaLogLevel] = "loud"
		if m := handle(t, s, "tools/list", p); errCode(m) != -32602 {
			t.Errorf("want -32602: %v", m)
		}
		p["_meta"].(map[string]any)[mcpserver.MetaLogLevel] = "warning"
		if m := handle(t, s, "tools/list", p); errCode(m) != 0 {
			t.Errorf("a real level is accepted: %v", m)
		}
	})

	// spec + "2026-07-28/basic/transports/streamable-http#sending-messages-to-the-server"
	// -- "If the server does not implement the requested RPC method, it MUST
	// respond with 404 Not Found and a JSON-RPC error with code -32601".
	// ping and logging/setLevel are gone from 2026-07-28, so a modern peer
	// gets method-not-found rather than an answer. This asserted the
	// opposite, under the "accept liberally" rule -- which is right for a
	// method a revision never had and wrong for one it removed, because the
	// removal is the specification naming a replacement.
	t.Run("2026-07-28/basic/removed-methods-are-method-not-found", func(t *testing.T) {
		for _, m := range []string{"ping", "logging/setLevel"} {
			if got := errCode(handle(t, s, m, modernParams(map[string]any{"level": "info"}))); got != -32601 {
				t.Errorf("%s: code %d, want -32601", m, got)
			}
		}
	})
}

// spec + "2026-07-28/server/utilities/logging#per-request-log-level"
func TestNoLogMessagesWithoutALogLevel(t *testing.T) {
	t.Run("2026-07-28/logging/no-notifications-message-without-logLevel", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		var in bytes.Buffer
		for i, method := range []string{"server/discover", "tools/list", "tools/call", "resources/read", "prompts/get"} {
			b, _ := json.Marshal(mcpserver.Request(i+1, method, modernParams(map[string]any{
				"name": "mcpx_namespaces", "uri": "demo://a"})))
			in.Write(append(b, '\n'))
		}
		var out bytes.Buffer
		if err := s.ServeStdio(context.Background(), &in, &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "notifications/message") {
			t.Errorf("a log message was sent to a request that set no level:\n%s", out.String())
		}
		if strings.Count(out.String(), "\n") != 5 {
			t.Errorf("expected five replies and nothing else:\n%s", out.String())
		}
	})
}

// ---- capabilities and shapes per revision ----

func TestCapabilitiesPerRevision(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	caps := func(version string) map[string]any {
		var r map[string]any
		if version == "2026-07-28" {
			r = resultOf(t, handle(t, s, "server/discover", modernParams(nil)))
		} else {
			r = resultOf(t, handle(t, s, "initialize", map[string]any{"protocolVersion": version}))
			if r["protocolVersion"] != version {
				t.Fatalf("%s not agreed: %v", version, r["protocolVersion"])
			}
		}
		c, _ := r["capabilities"].(map[string]any)
		return c
	}
	// spec + "2025-11-25/schema#servercapabilities" has no extensions field.
	t.Run("2025-11-25/capabilities/no-extensions-field", func(t *testing.T) {
		if _, ok := caps("2025-11-25")["extensions"]; ok {
			t.Error("extensions is not in 2025-11-25's ServerCapabilities")
		}
	})
	t.Run("2025-11-25/capabilities/core-tasks-declared", func(t *testing.T) {
		if _, ok := caps("2025-11-25")["tasks"]; !ok {
			t.Error("core tasks are 2025-11-25's")
		}
	})
	// spec + "2026-07-28/schema#servercapabilities" and the tasks SEP: core
	// tasks MUST NOT be advertised where the extension exists.
	t.Run("2026-07-28/capabilities/tasks-only-as-extension", func(t *testing.T) {
		c := caps("2026-07-28")
		if _, ok := c["tasks"]; ok {
			t.Error("core tasks declared to 2026-07-28")
		}
		ext, _ := c["extensions"].(map[string]any)
		if _, ok := ext[mcpserver.ExtTasks]; !ok {
			t.Errorf("extension missing: %v", c)
		}
	})
	// spec + "2024-11-05/basic/lifecycle": no completions capability yet.
	t.Run("2024-11-05/capabilities/no-completions", func(t *testing.T) {
		if _, ok := caps("2024-11-05")["completions"]; ok {
			t.Error("completions capability is 2025-03-26")
		}
		if _, ok := caps("2025-03-26")["completions"]; !ok {
			t.Error("completions capability should be declared from 2025-03-26")
		}
	})
}

// spec + "<rev>/basic/lifecycle#version-negotiation", every legacy revision:
// "If the server supports the requested protocol version, it MUST respond
// with the same version. Otherwise, the server MUST respond with another
// protocol version it supports."
func TestInitializeNegotiation(t *testing.T) {
	for _, rev := range mcpserver.LegacySupported() {
		t.Run(rev+"/lifecycle/initialize-agrees-a-supported-version", func(t *testing.T) {
			s := mcpserver.New(newBackend(), "mcpx", "test")
			r := resultOf(t, handle(t, s, "initialize", map[string]any{"protocolVersion": rev}))
			if r["protocolVersion"] != rev {
				t.Errorf("got %v", r["protocolVersion"])
			}
		})
		t.Run(rev+"/lifecycle/initialize-unsupported-version-answers-latest", func(t *testing.T) {
			s := mcpserver.New(newBackend(), "mcpx", "test")
			m := handle(t, s, "initialize", map[string]any{"protocolVersion": "1999-01-01"})
			if errCode(m) != 0 {
				t.Fatalf("refused instead of answered: %v", m)
			}
			if r := resultOf(t, m); r["protocolVersion"] != mcpserver.Latest {
				t.Errorf("got %v, want %s", r["protocolVersion"], mcpserver.Latest)
			}
		})
	}
}

type richBackend struct {
	*fakeBackend
	readErr error
}

func (r richBackend) ReadResource(ctx context.Context, uri string) ([]mcpserver.ResourceContents, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	return r.fakeBackend.ReadResource(ctx, uri)
}

func (r richBackend) Prompts(context.Context) ([]mcpserver.PromptRef, error) {
	return []mcpserver.PromptRef{{Name: "p", Title: "P",
		Arguments: []mcpserver.PromptArg{{Name: "a"}}}}, nil
}

// Every field mcpx sends, checked against each revision's schema.ts.
func TestShapesPerRevision(t *testing.T) {
	s := mcpserver.New(richBackend{fakeBackend: newBackend()}, "mcpx", "test").WithExtras([]mcpserver.Extra{{
		Tool: mcpserver.Tool{Name: "zz_tool", InputSchema: json.RawMessage(`{"type":"object"}`),
			Annotations: json.RawMessage(`{"readOnlyHint":true}`)},
		Call: func(context.Context, json.RawMessage) (string, error) { return "", nil },
	}})
	listFor := func(t *testing.T, rev, method, key string) []any {
		c := s.ConnForTest("sess-" + rev)
		s.HandleOn(context.Background(), c, mcpserver.Request(1, "initialize",
			map[string]any{"protocolVersion": rev}))
		b, _ := json.Marshal(s.HandleOn(context.Background(), c, mcpserver.Request(2, method, nil)))
		var m map[string]map[string][]any
		_ = json.Unmarshal(b, &m)
		return m["result"][key]
	}
	// spec + "2024-11-05/schema" Tool has name, description, inputSchema.
	t.Run("2024-11-05/tools/no-tool-annotations", func(t *testing.T) {
		if b := protoJSON(t, listFor(t, "2024-11-05", "tools/list", "tools")); strings.Contains(b, "annotations") {
			t.Errorf("Tool.annotations is 2025-03-26:\n%s", b)
		}
		if b := protoJSON(t, listFor(t, "2025-03-26", "tools/list", "tools")); !strings.Contains(b, "readOnlyHint") {
			t.Errorf("2025-03-26 has annotations and should keep them:\n%s", b)
		}
	})
	// spec + "2025-03-26/schema" Prompt has no title.
	t.Run("2025-03-26/prompts/no-title", func(t *testing.T) {
		if b := protoJSON(t, listFor(t, "2025-03-26", "prompts/list", "prompts")); strings.Contains(b, "title") {
			t.Errorf("Prompt.title is 2025-06-18:\n%s", b)
		}
		if b := protoJSON(t, listFor(t, "2025-06-18", "prompts/list", "prompts")); !strings.Contains(b, `"title":"P"`) {
			t.Errorf("2025-06-18 has title:\n%s", b)
		}
	})
	// spec + "<rev>/schema#resourcetemplate": uriTemplate, in every revision.
	for _, rev := range mcpserver.Supported {
		t.Run(rev+"/resources/templates-carry-uriTemplate", func(t *testing.T) {
			var r map[string]any
			if mcpserver.Modern(rev) {
				r = resultOf(t, handle(t, s, "resources/templates/list", modernParams(nil)))
			} else {
				r = resultOf(t, handle(t, s, "resources/templates/list", nil))
			}
			items, _ := r["resourceTemplates"].([]any)
			first, _ := items[0].(map[string]any)
			if first["uriTemplate"] != "demo://item/{id}" || first["uri"] != nil {
				t.Errorf("want uriTemplate and no uri: %v", first)
			}
		})
	}
	// spec + "2024-11-05/schema" has no AudioContent.
	t.Run("2024-11-05/content/audio-becomes-text", func(t *testing.T) {
		out := protoJSON(t, s.Downgrade(map[string]any{"content": []any{
			map[string]any{"type": "audio", "data": "AAA=", "mimeType": "audio/wav"}}}, "2024-11-05"))
		if strings.Contains(out, `"audio"`) {
			t.Errorf("audio sent to 2024-11-05:\n%s", out)
		}
	})
}

// ---- resources: not found ----

func TestResourceNotFound(t *testing.T) {
	notFound := fmt.Errorf("%w: mcpx://nope/x", mcpserver.ErrResourceNotFound)
	s := mcpserver.New(richBackend{fakeBackend: newBackend(), readErr: notFound}, "mcpx", "test")
	// spec + "2026-07-28/server/resources#error-handling"
	t.Run("2026-07-28/resources/not-found-is-32602-with-uri", func(t *testing.T) {
		m := handle(t, s, "resources/read", modernParams(map[string]any{"uri": "mcpx://nope/x"}))
		e, _ := m["error"].(map[string]any)
		data, _ := e["data"].(map[string]any)
		if errCode(m) != -32602 || data["uri"] != "mcpx://nope/x" {
			t.Errorf("got %v", m)
		}
	})
	// spec + "2025-11-25/server/resources#error-handling"
	for _, rev := range mcpserver.LegacySupported() {
		t.Run(rev+"/resources/not-found-is-32002-with-uri", func(t *testing.T) {
			c := s.ConnForTest("sess-" + rev)
			s.HandleOn(context.Background(), c, mcpserver.Request(1, "initialize",
				map[string]any{"protocolVersion": rev}))
			var m map[string]any
			_ = json.Unmarshal([]byte(protoJSON(t, s.HandleOn(context.Background(), c,
				mcpserver.Request(2, "resources/read", map[string]any{"uri": "mcpx://nope/x"})))), &m)
			e, _ := m["error"].(map[string]any)
			data, _ := e["data"].(map[string]any)
			if errCode(m) != -32002 || data["uri"] != "mcpx://nope/x" {
				t.Errorf("got %v", m)
			}
		})
	}
	// spec + "2026-07-28/server/resources#error-handling": -32603 for
	// internal errors.
	t.Run("2026-07-28/resources/read-failure-is-32603-not-32602", func(t *testing.T) {
		broken := mcpserver.New(richBackend{fakeBackend: newBackend(),
			readErr: errors.New("upstream timed out")}, "mcpx", "test")
		if m := handle(t, broken, "resources/read", modernParams(map[string]any{"uri": "mcpx://a/b"})); errCode(m) != -32603 {
			t.Errorf("got %v", m)
		}
	})
}

// spec + "2026-07-28/basic/index#error-codes": nothing in -32020..-32099
// except the three defined codes with their meaning, nothing new in
// -32000..-32019, and never -32002 or -32042 to a modern client.
func TestErrorCodeAllocation(t *testing.T) {
	notFound := fmt.Errorf("%w: x", mcpserver.ErrResourceNotFound)
	s := mcpserver.New(richBackend{fakeBackend: newBackend(), readErr: notFound}, "mcpx", "test")
	probes := []struct {
		method string
		params map[string]any
	}{
		{"nope/nope", nil}, {"resources/read", map[string]any{"uri": "x"}},
		{"resources/subscribe", map[string]any{}}, {"tasks/get", map[string]any{"taskId": "tsk-x"}},
		{"tasks/result", map[string]any{"taskId": "tsk-x"}}, {"tasks/list", nil},
		{"tasks/update", map[string]any{"taskId": "tsk-x"}}, {"tasks/cancel", map[string]any{"taskId": "tsk-x"}},
		{"prompts/get", map[string]any{"name": 1}}, {"tools/call", map[string]any{"name": 1}},
		{"subscriptions/listen", map[string]any{"notifications": map[string]any{"taskIds": []string{"a"}}}},
	}
	allowed := map[int]bool{-32020: true, -32021: true, -32022: true}
	for _, era := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(era+"/basic/error-codes-are-allocated-ones", func(t *testing.T) {
			for _, p := range probes {
				params := p.params
				if era == "2026-07-28" {
					params = modernParams(p.params)
				}
				code := errCode(handle(t, s, p.method, params))
				switch {
				case code == -32002 && era == "2026-07-28":
					t.Errorf("%s: -32002 sent to a modern client", p.method)
				case code == -32042 && era == "2026-07-28":
					t.Errorf("%s: -32042 sent to a modern client", p.method)
				case code <= -32000 && code >= -32019 && code != -32002:
					t.Errorf("%s: legacy-range code %d", p.method, code)
				case code <= -32020 && code >= -32099 && !allowed[code]:
					t.Errorf("%s: undefined reserved code %d", p.method, code)
				}
			}
		})
	}
	t.Run("2026-07-28/basic/unsupported-version-is-32022-with-supported-list", func(t *testing.T) {
		m := handle(t, s, "tools/list", map[string]any{"_meta": map[string]any{
			mcpserver.MetaProtocolVersion: "1999-01-01", mcpserver.MetaClientCapabilities: map[string]any{}}})
		e, _ := m["error"].(map[string]any)
		data, _ := e["data"].(map[string]any)
		if errCode(m) != -32022 || data["requested"] != "1999-01-01" {
			t.Errorf("got %v", m)
		}
	})
}

// ---- subscriptions/listen ----

// recorder collects frames sent on a connection.
type recorder struct {
	mu     sync.Mutex
	frames []map[string]any
}

func (r *recorder) send(frame any) error {
	var m map[string]any
	b, _ := json.Marshal(frame)
	_ = json.Unmarshal(b, &m)
	r.mu.Lock()
	r.frames = append(r.frames, m)
	r.mu.Unlock()
	return nil
}

func (r *recorder) wait(t *testing.T, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.frames) >= n {
			out := append([]map[string]any(nil), r.frames...)
			r.mu.Unlock()
			return out
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("wanted %d frames, got %d: %v", n, len(r.frames), r.frames)
	return nil
}

// multiNotifier hands each Listen its own fire channel, and ends a stream
// when told to.
type multiNotifier struct {
	mu      sync.Mutex
	streams []chan [2]any
	ended   chan struct{}
}

func (m *multiNotifier) Listen(ctx context.Context, f mcpserver.ListenFilter, send func(string, any)) {
	ch := make(chan [2]any, 8)
	m.mu.Lock()
	m.streams = append(m.streams, ch)
	m.mu.Unlock()
	defer func() {
		if m.ended != nil {
			select {
			case m.ended <- struct{}{}:
			default:
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-ch:
			if !ok {
				return // the source ended: a server-initiated teardown
			}
			send(n[0].(string), n[1])
		}
	}
}

func (m *multiNotifier) stream(t *testing.T, i int) chan [2]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		if len(m.streams) > i {
			ch := m.streams[i]
			m.mu.Unlock()
			return ch
		}
		m.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stream %d never opened", i)
	return nil
}

func listenReq(id int, filter map[string]any) any {
	return mcpserver.Request(id, "subscriptions/listen", modernParams(map[string]any{"notifications": filter}))
}

func subID(frame map[string]any) any {
	p, _ := frame["params"].(map[string]any)
	if p == nil {
		p, _ = frame["result"].(map[string]any)
	}
	meta, _ := p["_meta"].(map[string]any)
	return meta[mcpserver.MetaSubscriptionID]
}

func TestSubscriptionsListen(t *testing.T) {
	setup := func() (*mcpserver.Server, *multiNotifier, *recorder, *mcpserver.Conn) {
		n := &multiNotifier{ended: make(chan struct{}, 4)}
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Notify = n
		rec := &recorder{}
		return s, n, rec, s.ConnWithSend("sess-listen", rec.send)
	}

	// spec + "2026-07-28/basic/patterns/subscriptions#acknowledgment"
	t.Run("2026-07-28/subscriptions/acknowledged-first-with-subscriptionId-in-meta", func(t *testing.T) {
		s, n, rec, c := setup()
		if resp := s.HandleOn(context.Background(), c, mcpserver.Request(7, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"resourcesListChanged": true}}))); resp != nil {
			t.Fatalf("stdio withholds the result: %v", resp)
		}
		n.stream(t, 0) <- [2]any{"notifications/resources/list_changed", map[string]any{}}
		frames := rec.wait(t, 2)
		ack := frames[0]
		if ack["method"] != "notifications/subscriptions/acknowledged" {
			t.Fatalf("first frame: %v", ack)
		}
		p, _ := ack["params"].(map[string]any)
		if _, top := p["subscriptionId"]; top {
			t.Error("subscriptionId belongs in _meta")
		}
		if subID(ack) != float64(7) {
			t.Errorf("ack _meta: %v", p)
		}
		agreed, _ := p["notifications"].(map[string]any)
		if agreed["resourcesListChanged"] != true {
			t.Errorf("the agreed subset is missing: %v", p)
		}
	})

	// spec + "2026-07-28/basic/patterns/subscriptions#receiving-notifications"
	t.Run("2026-07-28/subscriptions/every-notification-carries-the-subscriptionId", func(t *testing.T) {
		s, n, rec, c := setup()
		s.HandleOn(context.Background(), c, mcpserver.Request(9, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{"demo://a"}}})))
		n.stream(t, 0) <- [2]any{"notifications/resources/updated", map[string]any{"uri": "demo://a"}}
		frames := rec.wait(t, 2)
		if frames[1]["method"] != "notifications/resources/updated" || subID(frames[1]) != float64(9) {
			t.Errorf("untagged: %v", frames[1])
		}
	})

	// spec + "2026-07-28/basic/patterns/subscriptions#multiple-concurrent-subscriptions"
	t.Run("2026-07-28/subscriptions/concurrent-listens-are-kept-apart", func(t *testing.T) {
		s, n, rec, c := setup()
		s.HandleOn(context.Background(), c, mcpserver.Request(1, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"resourcesListChanged": true}})))
		s.HandleOn(context.Background(), c, mcpserver.Request(2, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"promptsListChanged": true}})))
		if c.Listening() != 2 {
			t.Fatalf("the second listen replaced the first: %d open", c.Listening())
		}
		// Both kinds down both streams, since which goroutine opened which
		// is not ordered: each stream's own filter must keep one of them.
		for i := 0; i < 2; i++ {
			n.stream(t, i) <- [2]any{"notifications/resources/list_changed", map[string]any{}}
			n.stream(t, i) <- [2]any{"notifications/prompts/list_changed", map[string]any{}}
		}
		frames := rec.wait(t, 4)
		time.Sleep(20 * time.Millisecond)
		if n := len(rec.wait(t, 4)); n != 4 {
			t.Errorf("a stream delivered a kind it was not opened for: %d frames", n)
		}
		ids := map[string]any{}
		for _, f := range frames[2:] {
			ids[f["method"].(string)] = subID(f)
		}
		if ids["notifications/resources/list_changed"] != float64(1) || ids["notifications/prompts/list_changed"] != float64(2) {
			t.Errorf("tags: %v", ids)
		}
	})

	// spec + "2026-07-28/basic/patterns/subscriptions" -- MUST NOT send a
	// type the client did not request.
	t.Run("2026-07-28/subscriptions/unrequested-type-is-never-sent", func(t *testing.T) {
		s, n, rec, c := setup()
		s.HandleOn(context.Background(), c, mcpserver.Request(3, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"resourcesListChanged": true}})))
		ch := n.stream(t, 0)
		ch <- [2]any{"notifications/elicitation/complete", map[string]any{"elicitationId": "x"}}
		ch <- [2]any{"notifications/resources/list_changed", map[string]any{}}
		frames := rec.wait(t, 2)
		time.Sleep(20 * time.Millisecond)
		for _, f := range frames {
			if f["method"] == "notifications/elicitation/complete" {
				t.Errorf("sent an unrequested type: %v", f)
			}
		}
	})

	// spec + "2026-07-28/basic/patterns/cancellation#transport-specific-cancellation" (stdio)
	t.Run("2026-07-28/subscriptions/stdio-cancel-by-notifications-cancelled", func(t *testing.T) {
		s, n, _, c := setup()
		s.HandleOn(context.Background(), c, mcpserver.Request(4, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"resourcesListChanged": true}})))
		n.stream(t, 0)
		s.HandleOn(context.Background(), c, mcpserver.Request(0, "notifications/cancelled",
			map[string]any{"requestId": 4}))
		select {
		case <-n.ended:
		case <-time.After(2 * time.Second):
			t.Fatal("the stream kept running after the client cancelled it")
		}
		if c.Listening() != 0 {
			t.Errorf("still listening: %d", c.Listening())
		}
	})

	// The listen and the in-flight table key ids the same way, so a
	// cancellation naming the listen as 4.0 ends the listen opened as 4.
	t.Run("2026-07-28/subscriptions/cancel-matches-the-id-however-it-is-spelled", func(t *testing.T) {
		s, n, _, c := setup()
		s.HandleOn(context.Background(), c, mcpserver.Request(4, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"resourcesListChanged": true}})))
		n.stream(t, 0)
		s.HandleOn(context.Background(), c, mcpserver.Request(0, "notifications/cancelled",
			map[string]any{"requestId": json.RawMessage("4.0")}))
		select {
		case <-n.ended:
		case <-time.After(2 * time.Second):
			t.Fatal("4.0 did not cancel the listen opened as 4")
		}
	})

	// spec + "2026-07-28/basic/patterns/cancellation" (server MUST send
	// notifications/cancelled) and "#graceful-closure" (SHOULD send result).
	t.Run("2026-07-28/subscriptions/server-teardown-sends-cancelled-then-result", func(t *testing.T) {
		s, n, rec, c := setup()
		s.HandleOn(context.Background(), c, mcpserver.Request(5, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"resourcesListChanged": true}})))
		close(n.stream(t, 0))
		frames := rec.wait(t, 3)
		cancelled, final := frames[1], frames[2]
		cp, _ := cancelled["params"].(map[string]any)
		if cancelled["method"] != "notifications/cancelled" || cp["requestId"] != float64(5) {
			t.Errorf("want notifications/cancelled naming 5: %v", cancelled)
		}
		if final["id"] != float64(5) || subID(final) != float64(5) {
			t.Errorf("want the listen's result with its subscriptionId: %v", final)
		}
		if r, _ := final["result"].(map[string]any); r["resultType"] != "complete" {
			t.Errorf("graceful result: %v", final)
		}
	})

	t.Run("2026-07-28/subscriptions/taskIds-without-the-extension-is-32021", func(t *testing.T) {
		s, _, _, c := setup()
		b := protoJSON(t, s.HandleOn(context.Background(), c, mcpserver.Request(6, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{"taskIds": []string{"a"}}}))))
		if !strings.Contains(b, "-32021") {
			t.Errorf("got %s", b)
		}
	})

	t.Run("2025-11-25/resources/subscribe-notifications-are-untagged", func(t *testing.T) {
		s, n, _, _ := setup()
		var got []any
		var mu sync.Mutex
		s.SetPush(func(method string, params any) {
			mu.Lock()
			got = append(got, params)
			mu.Unlock()
		})
		s.Handle(context.Background(), mcpserver.Request(1, "resources/subscribe", map[string]any{"uri": "demo://a"}))
		n.stream(t, 0) <- [2]any{"notifications/resources/updated", map[string]any{"uri": "demo://a"}}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			l := len(got)
			mu.Unlock()
			if l > 0 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(got) != 1 || strings.Contains(protoJSON(t, got[0]), "subscriptionId") {
			t.Errorf("legacy notification: %v", got)
		}
	})
}

// spec + "2026-07-28/basic/transports/streamable-http" -- the listen POST's
// response is the stream.
func TestHTTPListenIsAStream(t *testing.T) {
	t.Run("2026-07-28/subscriptions/http-listen-response-is-an-open-sse-stream", func(t *testing.T) {
		n := &multiNotifier{ended: make(chan struct{}, 1)}
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Notify = n
		s.Timing.SSEKeepAlive = 20 * time.Millisecond
		srv := httptest.NewServer(s)
		defer srv.Close()

		body, _ := json.Marshal(listenReq(11, map[string]any{"resourcesListChanged": true}))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		// Required on every modern POST by the Streamable HTTP transport.
		req.Header.Set("MCP-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", "subscriptions/listen")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("content type %q, status %d", ct, resp.StatusCode)
		}
		lines := make(chan string, 64)
		go func() {
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				lines <- sc.Text()
			}
			close(lines)
		}()
		next := func(prefix string) string {
			t.Helper()
			timeout := time.After(2 * time.Second)
			for {
				select {
				case l, ok := <-lines:
					if !ok {
						t.Fatalf("stream ended waiting for %q", prefix)
					}
					if strings.HasPrefix(l, prefix) {
						return l
					}
				case <-timeout:
					t.Fatalf("nothing starting %q", prefix)
				}
			}
		}
		if ack := next("data:"); !strings.Contains(ack, "notifications/subscriptions/acknowledged") {
			t.Fatalf("first event: %s", ack)
		}
		n.stream(t, 0) <- [2]any{"notifications/resources/list_changed", map[string]any{}}
		if ev := next("data:"); !strings.Contains(ev, "list_changed") || !strings.Contains(ev, mcpserver.MetaSubscriptionID) {
			t.Errorf("notification: %s", ev)
		}
		next(": keep-alive")
		cancel() // closing the stream is the client's cancellation
		select {
		case <-n.ended:
		case <-time.After(2 * time.Second):
			t.Fatal("closing the HTTP stream did not end the subscription")
		}
		_, _ = io.Copy(io.Discard, resp.Body)
	})
}

// ---- tasks ----

type slowBackend struct {
	*fakeBackend
	delay time.Duration
	fail  bool
}

func (s slowBackend) Namespaces(ctx context.Context) (string, error) {
	time.Sleep(s.delay)
	if s.fail {
		return "", errors.New("tool failed")
	}
	return "slow", nil
}

func taskClient(extra map[string]any) map[string]any {
	return modernWith(`{"extensions":{"io.modelcontextprotocol/tasks":{}}}`,
		mergeMaps(map[string]any{"name": "mcpx_namespaces", "arguments": map[string]any{}}, extra))
}

// taskParams is a 2026-07-28 request from a client that declared the tasks
// extension, which the extension's own methods require.
func taskParams(extra map[string]any) map[string]any {
	return modernWith(`{"extensions":{"io.modelcontextprotocol/tasks":{}}}`, extra)
}

func mergeMaps(a, b map[string]any) map[string]any {
	for k, v := range b {
		a[k] = v
	}
	return a
}

func TestTasksPerEra(t *testing.T) {
	slow := func(delay time.Duration, fail bool) *mcpserver.Server {
		s := mcpserver.New(slowBackend{fakeBackend: newBackend(), delay: delay, fail: fail}, "mcpx", "test")
		s.Timing.TaskAfter = 10 * time.Millisecond
		return s
	}
	poll := func(t *testing.T, s *mcpserver.Server, id string) map[string]any {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			r := resultOf(t, handle(t, s, "tasks/get", taskParams(map[string]any{"taskId": id})))
			if st := r["status"]; st == "completed" || st == "failed" || st == "cancelled" {
				return r
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("never finished")
		return nil
	}

	// SEP-2663 (docs/extensions/tasks) "Task Creation"
	t.Run("2026-07-28/tasks/server-directed-CreateTaskResult-is-flat", func(t *testing.T) {
		s := slow(200*time.Millisecond, false)
		r := resultOf(t, handle(t, s, "tools/call", taskClient(nil)))
		if r["resultType"] != "task" || r["taskId"] == nil || r["task"] != nil {
			t.Fatalf("want a flat CreateTaskResult: %v", r)
		}
		for _, k := range []string{"ttlMs", "pollIntervalMs", "status", "createdAt", "lastUpdatedAt"} {
			if _, ok := r[k]; !ok {
				t.Errorf("%s missing: %v", k, r)
			}
		}
		for _, k := range []string{"ttl", "pollInterval", "cacheScope"} {
			if _, ok := r[k]; ok {
				t.Errorf("%s is not an extension field: %v", k, r)
			}
		}
		done := poll(t, s, r["taskId"].(string))
		if done["status"] != "completed" || done["resultType"] != "complete" {
			t.Errorf("tasks/get: %v", done)
		}
		res, _ := done["result"].(map[string]any)
		if !strings.Contains(protoJSON(t, res), "slow") {
			t.Errorf("the result is inlined on tasks/get: %v", done)
		}
	})

	t.Run("2026-07-28/tasks/fast-call-is-answered-directly", func(t *testing.T) {
		s := slow(0, false)
		s.Timing.TaskAfter = 2 * time.Second
		if r := resultOf(t, handle(t, s, "tools/call", taskClient(nil))); r["resultType"] != "complete" {
			t.Errorf("got %v", r)
		}
	})

	// SEP-2663: MUST NOT return CreateTaskResult to a client that did not
	// declare the extension; the task param MUST be ignored.
	t.Run("2026-07-28/tasks/no-task-without-the-extension-task-param-ignored", func(t *testing.T) {
		s := slow(50*time.Millisecond, false)
		r := resultOf(t, handle(t, s, "tools/call", modernParams(map[string]any{
			"name": "mcpx_namespaces", "task": map[string]any{"ttl": 1000}})))
		if r["resultType"] != "complete" || r["task"] != nil {
			t.Errorf("got %v", r)
		}
	})

	// SEP-2663 "failed ... MUST NOT be used for non-JSON-RPC errors"
	t.Run("2026-07-28/tasks/tool-isError-is-completed-not-failed", func(t *testing.T) {
		s := slow(100*time.Millisecond, true)
		r := resultOf(t, handle(t, s, "tools/call", taskClient(nil)))
		done := poll(t, s, r["taskId"].(string))
		res, _ := done["result"].(map[string]any)
		if done["status"] != "completed" || res["isError"] != true {
			t.Errorf("got %v", done)
		}
	})

	t.Run("2026-07-28/tasks/removed-methods-are-32601", func(t *testing.T) {
		s := slow(0, false)
		for _, m := range []string{"tasks/list", "tasks/result"} {
			if code := errCode(handle(t, s, m, modernParams(map[string]any{"taskId": "x"}))); code != -32601 {
				t.Errorf("%s: %d", m, code)
			}
		}
	})

	t.Run("2026-07-28/tasks/update-and-cancel-ack-empty-unknown-is-32602", func(t *testing.T) {
		s := slow(300*time.Millisecond, false)
		r := resultOf(t, handle(t, s, "tools/call", taskClient(nil)))
		id := r["taskId"].(string)
		for _, m := range []string{"tasks/update", "tasks/cancel"} {
			got := resultOf(t, handle(t, s, m, taskParams(map[string]any{"taskId": id, "inputResponses": map[string]any{"x": map[string]any{}}})))
			delete(got, "_meta")
			delete(got, "resultType")
			if len(got) != 0 {
				t.Errorf("%s: want an empty ack, got %v", m, got)
			}
			if code := errCode(handle(t, s, m, taskParams(map[string]any{"taskId": "tsk-none"}))); code != -32602 {
				t.Errorf("%s unknown: %d", m, code)
			}
		}
	})

	// https://modelcontextprotocol.io/extensions/tasks/overview (SEP-2663
	// "Servers MUST return this error for non-declaring clients issuing
	// tasks/get, tasks/update, and tasks/cancel requests") and
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index
	// (-32021 with data.requiredCapabilities). Asked about a live task, so
	// the answer cannot be the unknown-id -32602.
	t.Run("2026-07-28/tasks/methods-from-a-non-declaring-client-are-32021", func(t *testing.T) {
		s := slow(300*time.Millisecond, false)
		id := resultOf(t, handle(t, s, "tools/call", taskClient(nil)))["taskId"].(string)
		for _, m := range []string{"tasks/get", "tasks/update", "tasks/cancel"} {
			for _, id := range []string{id, "tsk-none"} {
				b := protoJSON(t, handle(t, s, m, modernParams(map[string]any{"taskId": id, "inputResponses": map[string]any{}})))
				if !strings.Contains(b, `"code":-32021`) ||
					!strings.Contains(b, `"requiredCapabilities":{"extensions":{"io.modelcontextprotocol/tasks":{}}}`) {
					t.Errorf("%s %s: %s", m, id, b)
				}
			}
		}
		// Removed methods keep -32601 even for a non-declaring client.
		if code := errCode(handle(t, s, "tasks/result", modernParams(map[string]any{"taskId": id}))); code != -32601 {
			t.Errorf("tasks/result: %d", code)
		}
	})

	// 2025-11-25 core tasks: a task is its connection's.
	t.Run("2025-11-25/tasks/another-connection-cannot-list-or-read-it", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		a, b := s.ConnForTest("sess-a"), s.ConnForTest("sess-b")
		for _, c := range []*mcpserver.Conn{a, b} {
			s.HandleOn(context.Background(), c, mcpserver.Request(1, "initialize",
				map[string]any{"protocolVersion": "2025-11-25"}))
		}
		created := protoJSON(t, s.HandleOn(context.Background(), a, mcpserver.Request(2, "tools/call",
			map[string]any{"name": "mcpx_call", "arguments": map[string]any{"namespace": "alpha", "tool": "t"}, "task": map[string]any{}})))
		var doc struct {
			Result struct {
				Task struct {
					TaskID string `json:"taskId"`
				} `json:"task"`
			} `json:"result"`
		}
		_ = json.Unmarshal([]byte(created), &doc)
		id := doc.Result.Task.TaskID
		if id == "" {
			t.Fatalf("no task: %s", created)
		}
		mine := protoJSON(t, s.HandleOn(context.Background(), a, mcpserver.Request(3, "tasks/list", nil)))
		theirs := protoJSON(t, s.HandleOn(context.Background(), b, mcpserver.Request(3, "tasks/list", nil)))
		if !strings.Contains(mine, id) || strings.Contains(theirs, id) {
			t.Errorf("list leaks across connections:\nmine %s\ntheirs %s", mine, theirs)
		}
		for _, m := range []string{"tasks/get", "tasks/result", "tasks/cancel"} {
			got := protoJSON(t, s.HandleOn(context.Background(), b, mcpserver.Request(4, m,
				map[string]any{"taskId": id})))
			if !strings.Contains(got, "-32602") {
				t.Errorf("%s from another connection: %s", m, got)
			}
		}
	})
}

// ---- elicitation per era ----

func TestElicitationPerEra(t *testing.T) {
	url := mcpserver.Question{ID: "elc-9", Method: "elicitation/create", Mode: "url",
		Params: json.RawMessage(`{"mode":"url","message":"sign in","url":"https://example.invalid/x"}`)}
	// spec + "2025-11-25/client/elicitation" -- elicitationId is required
	// on a url-mode request.
	t.Run("2025-11-25/elicitation/url-mode-gets-an-elicitationId", func(t *testing.T) {
		b, err := url.ParamsFor(peer("2025-11-25", `{"elicitation":{"url":{}}}`))
		if err != nil || !strings.Contains(string(b), `"elicitationId":"mcpx-elc-9"`) {
			t.Errorf("got %s %v", b, err)
		}
	})
	// spec + "2026-07-28/schema#elicitrequesturlparams" has no elicitationId.
	t.Run("2026-07-28/elicitation/no-elicitationId", func(t *testing.T) {
		withID := url
		withID.Params = json.RawMessage(`{"mode":"url","message":"m","url":"https://example.invalid","elicitationId":"up-1"}`)
		b, _ := withID.ParamsFor(peer("2026-07-28", `{"elicitation":{"url":{}}}`))
		if strings.Contains(string(b), "elicitationId") {
			t.Errorf("got %s", b)
		}
	})
	t.Run("2026-07-28/elicitation/inputRequests-carry-no-elicitationId", func(t *testing.T) {
		asker := &scriptedAsker{questions: []mcpserver.Question{{
			ID: "elc-1", Method: "elicitation/create", Mode: "form",
			Params: json.RawMessage(`{"message":"m","requestedSchema":{"type":"object"},"elicitationId":"up-1"}`),
		}}, text: "done"}
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Ask = asker
		b := protoJSON(t, s.Handle(context.Background(), mcpserver.Request(1, "tools/call", modernCall(1, nil))))
		if !strings.Contains(b, "input_required") || strings.Contains(b, "elicitationId") {
			t.Errorf("got %s", b)
		}
	})
}

// ---- MRTR ----

// spec + "2026-07-28/basic/patterns/mrtr#server-requirements-basic-workflow"
func TestMRTRServerRules(t *testing.T) {
	first := func(t *testing.T, s *mcpserver.Server) string {
		b := protoJSON(t, s.Handle(context.Background(), mcpserver.Request(1, "tools/call", modernCall(1, nil))))
		var d struct {
			Result struct {
				RequestState string `json:"requestState"`
				TTL          any    `json:"ttlMs"`
			} `json:"result"`
		}
		_ = json.Unmarshal([]byte(b), &d)
		if d.Result.TTL != nil {
			t.Errorf("input_required carries no cache hints: %s", b)
		}
		return d.Result.RequestState
	}
	t.Run("2026-07-28/mrtr/requestState-is-bound-to-the-originating-request", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Ask = &scriptedAsker{questions: oneQuestion(), text: "done"}
		state := first(t, s)
		other := modernCall(2, map[string]any{"requestState": state,
			"arguments": map[string]any{"namespace": "x", "tool": "other"}})
		if b := protoJSON(t, s.Handle(context.Background(), mcpserver.Request(2, "tools/call", other))); !strings.Contains(b, "-32602") {
			t.Errorf("state accepted on another request: %s", b)
		}
		// The same request, travelling differently, is the same request.
		same := modernCall(3, map[string]any{"requestState": state,
			"inputResponses": map[string]any{"elc-1": map[string]any{"action": "accept"}}}).(map[string]any)
		same["_meta"].(map[string]any)[mcpserver.MetaClientInfo] = map[string]any{"name": "other", "version": "2"}
		if b := protoJSON(t, s.Handle(context.Background(), mcpserver.Request(3, "tools/call", same))); !strings.Contains(b, "done") {
			t.Errorf("the retry was refused: %s", b)
		}
	})
	t.Run("2026-07-28/mrtr/missing-input-response-asks-again", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Ask = &scriptedAsker{questions: oneQuestion(), text: "done"}
		state := first(t, s)
		b := protoJSON(t, s.Handle(context.Background(), mcpserver.Request(2, "tools/call",
			modernCall(2, map[string]any{"requestState": state,
				"inputResponses": map[string]any{"unasked": map[string]any{"action": "accept"}}}))))
		if !strings.Contains(b, "input_required") || !strings.Contains(b, "elc-1") {
			t.Errorf("want the question again: %s", b)
		}
	})
	t.Run("2026-07-28/mrtr/tampered-requestState-is-rejected", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Ask = &scriptedAsker{questions: oneQuestion(), text: "done"}
		state := first(t, s)
		b := protoJSON(t, s.Handle(context.Background(), mcpserver.Request(2, "tools/call",
			modernCall(2, map[string]any{"requestState": "x" + state}))))
		if !strings.Contains(b, "-32602") {
			t.Errorf("got %s", b)
		}
	})
	// spec + "2026-07-28/basic/index" -- no sessions: discover mints none.
	t.Run("2026-07-28/mrtr/discover-issues-no-session", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		srv := httptest.NewServer(s)
		defer srv.Close()
		body, _ := json.Marshal(mcpserver.Request(1, "server/discover", modernParams(nil)))
		resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
			t.Errorf("session issued: %s", id)
		}
	})
}

// spec + "2026-07-28/server/tools#listing-tools" -- SHOULD be deterministic.
func TestToolsListIsDeterministic(t *testing.T) {
	t.Run("2026-07-28/tools/list-order-is-stable-whatever-order-extras-arrive-in", func(t *testing.T) {
		mk := func(names ...string) []mcpserver.Extra {
			var out []mcpserver.Extra
			for _, n := range names {
				out = append(out, mcpserver.Extra{Tool: mcpserver.Tool{Name: n, InputSchema: json.RawMessage(`{}`)}})
			}
			return out
		}
		a := mcpserver.New(newBackend(), "mcpx", "test").WithExtras(mk("b_x", "a_y", "c_z"))
		b := mcpserver.New(newBackend(), "mcpx", "test").WithExtras(mk("c_z", "b_x", "a_y"))
		if protoJSON(t, a.Tools()) != protoJSON(t, b.Tools()) {
			t.Errorf("order depends on arrival:\n%v\n%v", a.Tools(), b.Tools())
		}
	})
}

// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#error-handling
// https://modelcontextprotocol.io/specification/2026-07-28/server/tools#error-handling
// "Unknown tool" is listed under protocol errors, answered -32602; an
// isError result would say the tool ran.
func TestUnknownToolIsAProtocolError(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	for name, params := range map[string]map[string]any{
		"2025-11-25/tools/unknown-tool-is-32602": {"name": "no_such_tool"},
		"2026-07-28/tools/unknown-tool-is-32602": modernParams(map[string]any{"name": "no_such_tool"}),
	} {
		t.Run(name, func(t *testing.T) {
			m := handle(t, s, "tools/call", params)
			if errCode(m) != -32602 || m["result"] != nil {
				t.Errorf("got %v", m)
			}
		})
	}
	t.Run("2026-07-28/tools/known-tool-still-runs", func(t *testing.T) {
		if m := handle(t, s, "tools/call", modernParams(map[string]any{"name": "mcpx_namespaces"})); m["error"] != nil {
			t.Errorf("got %v", m)
		}
	})
}

// The same over /mcp, for every revision mcpx serves: each legacy one through
// its own initialize and session, the modern one statelessly. #284 made this
// -32602; before it, an isError result let the official suite's
// tools-call-simple-text "pass" against a tool that did not exist.
func TestUnknownToolIsAProtocolErrorOverHTTP(t *testing.T) {
	for _, v := range mcpserver.LegacySupported() {
		t.Run(v+"/tools/unknown-tool-is-32602-over-http", func(t *testing.T) {
			s := mcpserver.New(newBackend(), "mcpx", "test")
			w := post(s, frame(0, "initialize", map[string]any{"protocolVersion": v,
				"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}), nil)
			sess := w.Header().Get("Mcp-Session-Id")
			if sess == "" {
				t.Fatalf("no session from initialize: %d %s", w.Code, w.Body)
			}
			h := map[string]string{"Mcp-Session-Id": sess, "MCP-Protocol-Version": v}
			post(s, frame(nil, "notifications/initialized", nil), h)
			w = post(s, frame(1, "tools/call", map[string]any{"name": "no_such_tool"}), h)
			if code, _ := rpcErr(t, w.Body.Bytes()); code != -32602 {
				t.Errorf("code %d, want -32602: %s", code, w.Body)
			}
		})
	}
	t.Run("2026-07-28/tools/unknown-tool-is-32602-over-http", func(t *testing.T) {
		s := mcpserver.New(newBackend(), "mcpx", "test")
		w := post(s, frame(1, "tools/call", modernParams(map[string]any{"name": "no_such_tool"})),
			modernHeaders("tools/call", "no_such_tool"))
		if code, _ := rpcErr(t, w.Body.Bytes()); code != -32602 {
			t.Errorf("code %d, want -32602: %s", code, w.Body)
		}
	})
}
