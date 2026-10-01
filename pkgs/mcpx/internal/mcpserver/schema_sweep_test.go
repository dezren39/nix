package mcpserver_test

// Schema sweep: drive mcpx's MCP server through every method it serves, at every revision, and validate every
// frame it sends against the official schema, in strict mode (a property the revision does not define is a
// violation). Run with
//   go test ./internal/mcpserver -run TestServerFramesMatchSchema -v

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/mcpspec"
)

type sweepNotifier struct{}

func (sweepNotifier) Listen(ctx context.Context, f mcpserver.ListenFilter, send func(string, any)) {
	if f.ToolsListChanged {
		send("notifications/tools/list_changed", map[string]any{})
	}
	for _, u := range f.ResourceSubscriptions {
		send("notifications/resources/updated", map[string]any{"uri": u})
	}
	<-ctx.Done()
}

type sweepStep struct {
	method string
	params map[string]any
	notify bool
}

func sweepSteps(rev string) []sweepStep {
	modern := rev >= "2026-07-28"
	var steps []sweepStep
	if !modern {
		steps = append(steps,
			sweepStep{method: "initialize", params: map[string]any{"protocolVersion": rev,
				"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "sweep", "version": "1"}}},
			sweepStep{method: "notifications/initialized", notify: true},
			sweepStep{method: "ping"},
			sweepStep{method: "logging/setLevel", params: map[string]any{"level": "info"}},
			sweepStep{method: "resources/subscribe", params: map[string]any{"uri": "demo://a"}},
			sweepStep{method: "resources/unsubscribe", params: map[string]any{"uri": "demo://a"}},
		)
	} else {
		steps = append(steps,
			sweepStep{method: "server/discover", params: map[string]any{}},
			sweepStep{method: "subscriptions/listen", params: map[string]any{"notifications": map[string]any{
				"toolsListChanged": true, "resourceSubscriptions": []any{"demo://a"}}}},
		)
	}
	steps = append(steps,
		sweepStep{method: "tools/list"},
		sweepStep{method: "tools/call", params: map[string]any{"name": "mcpx_namespaces", "arguments": map[string]any{}}},
		sweepStep{method: "tools/call", params: map[string]any{"name": "mcpx_call", "arguments": map[string]any{"namespace": "alpha", "tool": "t", "args": map[string]any{}}}},
		sweepStep{method: "tools/call", params: map[string]any{"name": "no_such_tool", "arguments": map[string]any{}}},
		sweepStep{method: "completion/complete", params: map[string]any{"ref": map[string]any{"type": "ref/prompt", "name": "summarise"}, "argument": map[string]any{"name": "x", "value": "mcpx"}}},
		sweepStep{method: "resources/list"},
		sweepStep{method: "resources/templates/list"},
		sweepStep{method: "resources/read", params: map[string]any{"uri": "demo://a"}},
		sweepStep{method: "prompts/list"},
		sweepStep{method: "prompts/get", params: map[string]any{"name": "summarise"}},
	)
	if rev >= "2025-11-25" {
		steps = append(steps,
			sweepStep{method: "tools/call", params: map[string]any{"name": "mcpx_namespaces", "arguments": map[string]any{}, "task": map[string]any{}}},
			sweepStep{method: "tasks/list"},
		)
	}
	if modern {
		for i := range steps {
			if steps[i].params == nil {
				steps[i].params = map[string]any{}
			}
			steps[i].params["_meta"] = map[string]any{
				"io.modelcontextprotocol/protocolVersion":    rev,
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
				"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "sweep", "version": "1"},
			}
		}
	}
	return steps
}

type sweepFinding struct{ rev, method, frame, err string }

func TestServerFramesMatchSchema(t *testing.T) {
	var findings []sweepFinding
	for _, rev := range append(mcpspec.Revisions(), "1999-01-01") {
		findings = append(findings, sweepRev(t, rev)...)
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].rev < findings[j].rev })
	for _, f := range findings {
		t.Errorf("VIOLATION %s %s: %s\n    frame: %s", f.rev, f.method, f.err, f.frame)
	}
}

func sweepRev(t *testing.T, rev string) []sweepFinding {
	srv := mcpserver.New(newBackend(), "mcpx", "test")
	srv.Notify = sweepNotifier{}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = srv.ServeStdio(ctx, inR, outW); outW.Close(); close(done) }()

	var mu sync.Mutex
	methodOf := map[string]string{}
	var findings []sweepFinding
	schemaRev := rev
	if rev == "1999-01-01" {
		schemaRev = "2025-11-25" // an unsupported initialize: the answer must be a valid latest-revision result
	}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			var head struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			_ = json.Unmarshal(line, &head)
			mu.Lock()
			m := head.Method
			if m == "" {
				m = methodOf[string(head.ID)]
			}
			mu.Unlock()
			label := m
			if head.Method == "" {
				label = m + " (response)"
			}
			t.Logf("%s <- %s", rev, line)
			if err := mcpspec.ValidateServerMessageStrict(schemaRev, line, m); err != nil {
				if _, ext := err.(*mcpspec.ErrExtension); ext {
					t.Logf("%s %s: %v", rev, label, err)
					continue
				}
				mu.Lock()
				findings = append(findings, sweepFinding{rev, label, string(line), err.Error()})
				mu.Unlock()
			}
		}
	}()

	send := func(id int, st sweepStep) {
		frame := map[string]any{"jsonrpc": "2.0", "method": st.method}
		if st.params != nil {
			frame["params"] = st.params
		}
		if !st.notify {
			frame["id"] = id
			mu.Lock()
			methodOf[fmt.Sprint(id)] = st.method
			mu.Unlock()
		}
		b, _ := json.Marshal(frame)
		// The sweep's own frames must be valid, or a server complaint could be the test's fault.
		if err := mcpspec.ValidateClientMessage(schemaRev, b); err != nil && !strings.HasPrefix(st.method, "tasks/") {
			t.Logf("%s: sweep sent a non-conforming client frame %s: %v", rev, b, err)
		}
		_, _ = inW.Write(append(b, '\n'))
	}
	for i, st := range sweepSteps(schemaRev) {
		if rev == "1999-01-01" {
			if st.method != "initialize" {
				continue
			}
			st.params["protocolVersion"] = rev
		}
		send(i+1, st)
	}
	time.Sleep(300 * time.Millisecond) // let background tasks and pushes land
	_ = inW.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Errorf("%s: server did not stop", rev)
	}
	<-readDone
	mu.Lock()
	defer mu.Unlock()
	return findings
}
