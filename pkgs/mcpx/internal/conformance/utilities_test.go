package conformance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/spec"
)

// unanswered is a legacy stdio session in which mcpx asks its client a
// question nobody answers until the ask deadline: the question, and every
// frame after it up to the call's own response.
func unanswered(t *testing.T, rev string) (question map[string]any, after []map[string]any, ss *stdioSrv) {
	t.Helper()
	srv, _ := newServer(t)
	q, caps := elicitQ("q"), map[string]any{"elicitation": map[string]any{}}
	if rev < rev20250618 {
		q, caps = sampleQ("q"), map[string]any{"sampling": map[string]any{}}
	}
	srv.Ask = newAsker("done", q)
	srv.Timing = mcpserver.Timing{AskTimeout: 300 * time.Millisecond, AskPoll: 20 * time.Millisecond}
	ss = stdioServer(t, srv)
	ss.request(t, rev, "initialize", initParams(rev, caps))
	ss.send(t, frame(nil, "notifications/initialized", nil))
	ss.send(t, frame(9, "tools/call", callThatAsks()))
	for {
		f := ss.next(t)
		m := decode(t, f)
		if m["method"] != nil && m["id"] != nil && question == nil {
			checkServerFrame(t, rev, f, "")
			question = m
			continue
		}
		checkServerFrame(t, rev, f, "")
		after = append(after, m)
		if m["id"] == float64(9) {
			return question, after, ss
		}
	}
}

// relayedProgress makes one call that asked for progress against a peer
// that reports badly -- a stray token, a repeat, a decrease, a burst, a
// report after completion and one after the response -- and returns the
// progress values mcpx passed on to the call's relay, and how many reached
// a Subscribe-d OnProgress.
func relayedProgress(t *testing.T, rev string) (got []float64, subscribed int) {
	t.Helper()
	p := newPeer(t, rev)
	var tok any
	p.on("tools/call", func(pm map[string]any) (any, *rpcError) {
		tok = asMap(pm["_meta"])["progressToken"]
		if tok == nil {
			t.Error("the call asked for no progress")
		}
		send := func(v float64, total ...float64) {
			n := map[string]any{"progressToken": tok, "progress": v}
			if len(total) > 0 {
				n["total"] = total[0]
			}
			p.notify("notifications/progress", n)
		}
		p.notify("notifications/progress", map[string]any{"progressToken": "stray", "progress": 0.25})
		send(1)
		time.Sleep(3 * mcpclient.ProgressMinInterval / 2)
		send(1)
		send(0.5)
		send(2)
		time.Sleep(3 * mcpclient.ProgressMinInterval / 2)
		for i := 3; i < 53; i++ {
			send(float64(i))
		}
		send(100, 100)
		send(101, 100)
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}, nil
	})
	var mu sync.Mutex
	c := dialClient(t, p, clientOpts())
	c.Subscribe(mcpclient.Notifications{OnProgress: func(mcpclient.Progress) { mu.Lock(); subscribed++; mu.Unlock() }})
	ctx := mcpclient.WithRelay(ctxT(t), &mcpclient.Relay{ProgressToken: json.RawMessage(`"host-tok"`),
		OnProgress: func(raw json.RawMessage) {
			var n struct {
				Token    any     `json:"progressToken"`
				Progress float64 `json:"progress"`
			}
			_ = json.Unmarshal(raw, &n)
			if n.Token != "host-tok" {
				t.Errorf("progress under token %v, not the host's", n.Token)
			}
			mu.Lock()
			got = append(got, n.Progress)
			mu.Unlock()
		}})
	if _, err := c.CallTool(ctx, "echo", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	// A second call reports once and never completes; progress for its
	// token after its response must still be dropped -- the first call's
	// final report cannot be what stops it.
	p.on("tools/call", func(pm map[string]any) (any, *rpcError) {
		tok = asMap(pm["_meta"])["progressToken"]
		p.notify("notifications/progress", map[string]any{"progressToken": tok, "progress": 1000})
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}, nil
	})
	if _, err := c.CallTool(ctx, "echo", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * mcpclient.ProgressMinInterval / 2)
	p.notify("notifications/progress", map[string]any{"progressToken": tok, "progress": 2000})
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return append([]float64(nil), got...), subscribed
}

// relayBackend answers mcpx_call by calling an upstream through a real
// mcpclient, relaying what the client asked for -- what the daemon does.
type relayBackend struct {
	*backend
	up *mcpclient.Client
}

func (b relayBackend) Call(ctx context.Context, _, tool string, _ json.RawMessage) (string, error) {
	if r := mcpserver.RelayFrom(ctx); r != nil && r.Notify != nil {
		ctx = mcpclient.WithRelay(ctx, &mcpclient.Relay{ProgressToken: r.ProgressToken, LogLevel: r.LogLevel, Meta: r.Meta,
			OnProgress: func(p json.RawMessage) { r.Notify("notifications/progress", p) }})
	}
	if _, err := b.up.CallTool(ctx, tool, map[string]any{}); err != nil {
		return "", err
	}
	return "called", nil
}

// serverRelayedProgress has a client call mcpx with a progressToken, mcpx
// call an upstream that reports badly (as relayedProgress's does), and
// returns the progress values mcpx sent the client, each frame validated
// against rev and checked to carry the client's own token.
func serverRelayedProgress(t *testing.T, rev string) []float64 {
	t.Helper()
	// The client's revision first and strict, so a 2024-11-05 client is held
	// to its own schema: under the default precedence (2026-07-28 first) mcpx
	// sends progress.message to it, which that schema does not define (#307).
	t.Cleanup(spec.Set(spec.Must(nil, rev, nil)))
	p := newPeer(t, rev)
	var tok any
	p.on("tools/call", func(pm map[string]any) (any, *rpcError) {
		tok = asMap(pm["_meta"])["progressToken"]
		send := func(v float64, total ...float64) {
			n := map[string]any{"progressToken": tok, "progress": v, "message": fmt.Sprintf("step %v", v)}
			if len(total) > 0 {
				n["total"] = total[0]
			}
			p.notify("notifications/progress", n)
		}
		p.notify("notifications/progress", map[string]any{"progressToken": "stray", "progress": 0.25})
		send(1)
		time.Sleep(3 * mcpclient.ProgressMinInterval / 2)
		send(1)
		send(0.5)
		send(2)
		time.Sleep(3 * mcpclient.ProgressMinInterval / 2)
		for i := 3; i < 53; i++ {
			send(float64(i))
		}
		send(100, 100)
		send(101, 100)
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}, nil
	})
	b := newBackend()
	srv := mcpserver.New(relayBackend{b, dialClient(t, p, clientOpts())}, "mcpx", "test")
	ss := stdioServer(t, srv)
	ss.initialize(t, rev)
	r := ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "up", "tool": "echo"}, "_meta": map[string]any{"progressToken": "p1"}})
	if errorCode(r) != 0 || asMap(r["result"])["isError"] == true {
		t.Fatalf("call failed: %v", r)
	}
	// After the response: the upstream reports again; nothing may follow.
	p.notify("notifications/progress", map[string]any{"progressToken": tok, "progress": 200})
	var got []float64
	frames := append([][]byte(nil), ss.pending...)
	ss.pending = nil
	for {
		f, quiet := ss.quiet(200 * time.Millisecond)
		if quiet {
			break
		}
		ss.pending = nil
		frames = append(frames, f)
	}
	for _, f := range frames {
		m := decode(t, f)
		if m["method"] != "notifications/progress" {
			t.Errorf("unexpected: %s", f)
			continue
		}
		checkServerFrame(t, rev, f, "")
		pm := asMap(m["params"])
		if pm["progressToken"] != "p1" {
			t.Errorf("progress for %v, not the client's token", pm["progressToken"])
		}
		// The message is the upstream's, carried where the client's
		// revision defines it and dropped where it does not.
		if msg, _ := pm["message"].(string); (msg == "") == mcpserver.Defines(rev, mcpserver.FeatProgressMessage) {
			t.Errorf("progress message %q for a %s client", msg, rev)
		}
		v, _ := pm["progress"].(float64)
		got = append(got, v)
	}
	return got
}

// Cancellation, progress, pagination, logging, completion: mcpx as a server.
func TestUtilitiesServer(t *testing.T) {
	srvSide := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "server", id, fn) }

	// ---- cancellation mcpx sends ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation
	for _, id := range []string{"cancellation-ref-same-direction-in-progress", "cancellation-requestid-required-non-task-forbidden-task",
		"cancellation-sender-ignore-late-response", "cancellation-task-use-tasks-cancel"} {
		srvSide(id, func(t *testing.T, rev string) {
			q, after, ss := unanswered(t, rev)
			if q == nil {
				t.Fatal("no question")
			}
			if _, has := asMap(q["params"])["task"]; has {
				t.Errorf("a task-augmented request: %v", q)
			}
			var cancelled bool
			for _, f := range after {
				if f["method"] == "notifications/cancelled" {
					p := asMap(f["params"])
					if p["requestId"] != q["id"] {
						t.Errorf("cancels %v, asked %v", p["requestId"], q["id"])
					}
					if _, has := p["taskId"]; has {
						t.Errorf("taskId on a non-task cancellation: %v", p)
					}
					cancelled = true
				}
			}
			if !cancelled {
				t.Fatalf("no cancellation of the expired question: %v", after)
			}
			// The client's answer arrives after all: ignored, and the
			// session carries on.
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": map[string]any{"action": "decline"}})
			ss.send(t, b)
			if r := ss.request(t, rev, "tools/list", nil); errorCode(r) != 0 {
				t.Errorf("after a late answer: %v", r)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/utilities/cancellation
	for _, id := range []string{"cancellation-timeout-then-cancel", "cancellation-timeouts-establish",
		"cancellation-max-timeout-despite-progress"} {
		srvSide(id, func(t *testing.T, rev string) {
			// A 2026-07-28 server sends no requests of its own: a question is
			// a result, returned at once, so no request of mcpx's can hang.
			srv, _ := newServer(t)
			srv.Ask = newAsker("done", elicitQ("q"))
			ss := stdioServer(t, srv)
			start := time.Now()
			r := ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks()))
			if resultOf(t, r)["resultType"] != "input_required" || time.Since(start) > 2*time.Second {
				t.Errorf("%v after %s", r, time.Since(start))
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/lifecycle#timeouts
	srvSide("cancellation-timeouts-per-request-configurable", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		r := ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_exec",
			"arguments": map[string]any{"source": "1", "timeout": 3}})
		if errorCode(r) != 0 {
			t.Fatalf("%v", r)
		}
	})

	// ---- cancellation mcpx receives ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation#behavior-requirements
	for _, id := range []string{"cancellation-receiver-should-stop-free-no-response", "cancellation-server-should-stop-free-no-response",
		"cancellation-processing-should-cease"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, b := newServer(t)
			b.blocking(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			ss.send(t, frame(5, "tools/call", params(rev, callParams())))
			waitSignal(t, b.started, "the call")
			ss.send(t, frame(nil, "notifications/cancelled", params(rev, map[string]any{"requestId": 5, "reason": "user"})))
			waitSignal(t, b.ended, "the backend seeing the cancellation")
			if f, quiet := ss.quiet(300 * time.Millisecond); !quiet {
				t.Errorf("answered a cancelled request: %s", f)
			}
		})
	}
	for _, id := range []string{"cancellation-receiver-may-ignore", "cancellation-server-may-ignore",
		"cancellation-ignore-invalid", "cancellation-handle-races"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			// Unknown, already answered, and malformed cancellations.
			r := ss.request(t, rev, "tools/list", nil)
			_ = r
			for _, p := range []map[string]any{{"requestId": 12345}, {"requestId": 1}, {"requestId": map[string]any{}}, {}} {
				ss.send(t, frame(nil, "notifications/cancelled", params(rev, p)))
			}
			// And a cancellation racing its own request's answer.
			for i := 100; i < 110; i++ {
				ss.send(t, frame(i, "tools/list", params(rev, nil)))
				ss.send(t, frame(nil, "notifications/cancelled", params(rev, map[string]any{"requestId": i})))
			}
			if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
				m := decode(t, f)
				if m["id"] == nil || errorCode(m) != 0 {
					t.Errorf("a cancellation provoked %s", f)
				}
			}
			ss.pending = nil
			if r := ss.request(t, rev, "tools/list", nil); errorCode(r) != 0 {
				t.Errorf("the session did not survive: %v", r)
			}
		})
	}

	// ---- progress: what mcpx relays from upstream ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/progress
	for _, id := range []string{"progress-must-increase", "progress-message-human-readable", "progress-only-active-tokens",
		"progress-rate-limit", "progress-stop-after-completion", "progress-receiver-may-skip", "progress-server-may-skip",
		"progress-task-token-lives-with-task", "progress-task-same-token", "progress-task-stop-at-terminal",
		"progress-http-notifications-relate-to-request"} {
		srvSide(id, func(t *testing.T, rev string) {
			// The only progress mcpx sends is an upstream's, relayed. The
			// upstream here reports badly; what reaches the client must
			// still be in order, for its own token, throttled, and over
			// by the call's response.
			got := serverRelayedProgress(t, rev)
			if len(got) < 4 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[len(got)-1] != 100 {
				t.Fatalf("sent %v", got)
			}
			for i := 1; i < len(got); i++ {
				if got[i] <= got[i-1] {
					t.Errorf("sent out of order: %v", got)
				}
			}
			if len(got) > 10 {
				t.Errorf("a burst of 50 was not throttled: %d sent", len(got))
			}
		})
	}
	srvSide("progress-token-unique-sender", func(t *testing.T, rev string) {
		// mcpx's own requests to a client ask for no progress.
		q, _, _ := unanswered(t, rev)
		if _, has := asMap(asMap(q["params"])["_meta"])["progressToken"]; has {
			t.Errorf("%v", q)
		}
	})

	// ---- pagination ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/pagination
	pagedServer := func(t *testing.T, rev string) *stdioSrv {
		srv, _ := newServer(t)
		var extras []mcpserver.Extra
		for i := 0; i < 25; i++ {
			extras = append(extras, mcpserver.Extra{Tool: mcpserver.Tool{Name: fmt.Sprintf("x_%02d", i), Description: "x",
				InputSchema: json.RawMessage(`{"type":"object"}`)}})
		}
		srv = srv.WithExtras(extras)
		srv.PageSize = 10
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		return ss
	}
	srvSide("pagination-stable-cursors", func(t *testing.T, rev string) {
		ss := pagedServer(t, rev)
		cur := resultOf(t, ss.request(t, rev, "tools/list", nil))["nextCursor"]
		a, _ := json.Marshal(resultOf(t, ss.request(t, rev, "tools/list", map[string]any{"cursor": cur}))["tools"])
		b, _ := json.Marshal(resultOf(t, ss.request(t, rev, "tools/list", map[string]any{"cursor": cur}))["tools"])
		if cur == nil || string(a) != string(b) {
			t.Errorf("the same cursor gave different pages")
		}
	})
	srvSide("pagination-invalid-cursor-graceful", func(t *testing.T, rev string) {
		ss := pagedServer(t, rev)
		r := ss.request(t, rev, "tools/list", map[string]any{"cursor": "not-a-cursor"})
		if r["result"] == nil && errorCode(r) != -32602 {
			t.Errorf("%v", r)
		}
		if r := ss.request(t, rev, "tools/list", nil); errorCode(r) != 0 {
			t.Error("the session did not survive")
		}
	})
	srvSide("pagination-invalid-cursor-32602", func(t *testing.T, rev string) {
		ss := pagedServer(t, rev)
		if r := ss.request(t, rev, "tools/list", map[string]any{"cursor": "not-a-cursor"}); errorCode(r) != -32602 {
			t.Errorf("%v", r)
		}
	})

	// ---- logging: mcpx's own tools send none; upstream messages are relayed (e2e relay_test) ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/logging
	for _, id := range []string{"logging-declare-capability", "logging-setlevel-at-or-above", "logging-unset-server-chooses",
		"logging-server-hygiene", "logging-no-secrets-pii", "logging-impl-validate-control", "logging-deprecated",
		"logging-no-message-without-loglevel", "logging-request-scoped-only"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			var caps map[string]any
			if isModern(rev) {
				caps = asMap(resultOf(t, ss.request(t, rev, "server/discover", nil))["capabilities"])
			} else {
				caps = asMap(resultOf(t, ss.initialize(t, rev))["capabilities"])
				ss.request(t, rev, "logging/setLevel", map[string]any{"level": "debug"})
			}
			// Legacy: declared, because upstream log messages are relayed to
			// a client that set a level. 2026-07-28 has no such capability.
			if _, ok := caps["logging"]; ok == isModern(rev) {
				t.Errorf("logging declared=%v under %s", ok, rev)
			}
			ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}})
			if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
				t.Errorf("unexpected: %s", f)
			}
		})
	}
	srvSide("logging-errors-setlevel", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		if r := ss.requestInvalid(t, rev, "logging/setLevel", map[string]any{"level": "loud"}); errorCode(r) != -32602 {
			t.Errorf("an invalid level: %v", r)
		}
		if r := ss.request(t, rev, "logging/setLevel", map[string]any{"level": "warning"}); errorCode(r) != 0 {
			t.Errorf("a valid level: %v", r)
		}
	})
	srvSide("logging-invalid-loglevel-reject-request", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		p := params(rev, nil)
		p["_meta"].(map[string]any)["io.modelcontextprotocol/logLevel"] = "loud"
		ss.send(t, frame(1, "tools/list", p))
		if r := decode(t, ss.next(t)); errorCode(r) != -32602 {
			t.Errorf("%v", r)
		}
	})

	// ---- completion ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/completion
	srvSide("completion-declare-capability", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		var caps map[string]any
		if isModern(rev) {
			caps = asMap(resultOf(t, ss.request(t, rev, "server/discover", nil))["capabilities"])
		} else {
			caps = asMap(resultOf(t, ss.initialize(t, rev))["capabilities"])
		}
		if _, ok := caps["completions"]; !ok {
			t.Fatal("completions not declared")
		}
		r := ss.request(t, rev, "completion/complete", map[string]any{"ref": map[string]any{"type": "ref/prompt", "name": "greet"},
			"argument": map[string]any{"name": "who", "value": ""}})
		if errorCode(r) != 0 {
			t.Errorf("%v", r)
		}
	})
	srvSide("completion-max-100", func(t *testing.T, rev string) {
		srv, b := newServer(t)
		b.completions = 150
		srv.MaxCompletions = func() int { return 500 }
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		r := resultOf(t, ss.request(t, rev, "completion/complete", map[string]any{"ref": map[string]any{"type": "ref/prompt", "name": "greet"},
			"argument": map[string]any{"name": "who", "value": ""}}))
		c := asMap(r["completion"])
		if n := len(c["values"].([]any)); n != 100 || c["hasMore"] != true {
			t.Errorf("%d values, hasMore %v", n, c["hasMore"])
		}
	})
	srvSide("completion-errors", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		r := ss.requestInvalid(t, rev, "completion/complete", map[string]any{"ref": map[string]any{"type": "ref/prompt", "name": "no-such-prompt"},
			"argument": map[string]any{"name": "x", "value": ""}})
		if errorCode(r) != -32602 {
			t.Errorf("an unknown prompt: %v", r)
		}
	})
}

// Cancellation, progress, pagination, logging, completion: mcpx as a client.
func TestUtilitiesClient(t *testing.T) {
	cli := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "client", id, fn) }

	// ---- cancellation mcpx sends ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation
	for _, id := range []string{"cancellation-client-should-send", "cancellation-stdio-client-must-send",
		"cancellation-timeout-then-cancel", "cancellation-timeouts-establish", "cancellation-max-timeout-despite-progress",
		"cancellation-ref-same-direction-in-progress", "cancellation-ref-client-issued-in-progress",
		"cancellation-requestid-required-non-task-forbidden-task", "cancellation-sender-ignore-late-response",
		"cancellation-client-ignore-late-response", "cancellation-task-use-tasks-cancel",
		"cancellation-timeouts-per-request-configurable"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			p.on("tools/call", func(map[string]any) (any, *rpcError) { return nil, nil })
			c := dialClient(t, p, clientOpts())
			start := time.Now()
			if _, err := c.CallTimeout(ctxT(t), 100*time.Millisecond, "echo", map[string]any{}); err == nil {
				t.Fatal("no timeout")
			}
			if time.Since(start) > 2*time.Second {
				t.Errorf("took %s", time.Since(start))
			}
			time.Sleep(50 * time.Millisecond)
			calls, cancels := p.sentMethod("tools/call"), p.sentMethod("notifications/cancelled")
			if len(cancels) != 1 {
				t.Fatalf("cancellations %v", cancels)
			}
			cp := asMap(cancels[0]["params"])
			if cp["requestId"] != calls[0]["id"] {
				t.Errorf("cancelled %v, sent %v", cp["requestId"], calls[0]["id"])
			}
			if _, has := cp["taskId"]; has {
				t.Error("taskId on a non-task cancellation")
			}
			if _, has := asMap(calls[0]["params"])["task"]; has {
				t.Error("a task-augmented request")
			}
			// The answer arrives late: ignored.
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": calls[0]["id"], "result": map[string]any{"content": []any{}}})
			p.push(b)
			if _, err := c.ListTools(ctxT(t)); err != nil {
				t.Errorf("after a late answer: %v", err)
			}
		})
	}
	cli("cancellation-client-must-not-cancel-initialize", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("initialize", func(map[string]any) (any, *rpcError) { return nil, nil })
		ctx, cancel := contextWithTimeout(100 * time.Millisecond)
		defer cancel()
		o := clientOpts()
		o.Preference = mcpclient.ForceLegacy
		if c, _ := mcpclient.NewWithOptions(ctx, p, o); c != nil {
			c.Close()
		}
		if n := len(p.sentMethod("notifications/cancelled")); n != 0 {
			t.Errorf("cancelled its initialize")
		}
	})

	// ---- cancellation mcpx receives ----

	// A server question the client is still working on, then cancelled.
	cancelledQuestion := func(t *testing.T, rev string) (answered bool, handlerCtxEnded bool) {
		p := newPeer(t, rev)
		ended := make(chan struct{}, 1)
		o := clientOpts()
		o.OnServerRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			select {
			case <-ctx.Done():
				ended <- struct{}{}
				return nil, ctx.Err()
			case <-time.After(time.Second):
				return map[string]any{"action": "decline"}, nil
			}
		}
		dialClient(t, p, o)
		before := len(p.sent())
		p.push(frame(4242, "elicitation/create", map[string]any{"message": "?", "requestedSchema": map[string]any{"type": "object"}}))
		time.Sleep(50 * time.Millisecond)
		p.notify("notifications/cancelled", map[string]any{"requestId": 4242})
		select {
		case <-ended:
			handlerCtxEnded = true
		case <-time.After(1500 * time.Millisecond):
		}
		time.Sleep(100 * time.Millisecond)
		for _, f := range p.sent()[before:] {
			if f["id"] == float64(4242) {
				answered = true
			}
		}
		return answered, handlerCtxEnded
	}
	for _, id := range []string{"cancellation-receiver-should-stop-free-no-response", "cancellation-processing-should-cease"} {
		cli(id, func(t *testing.T, rev string) {
			if isModern(rev) {
				// A 2026-07-28 server has no request of its own to cancel;
				// a cancellation naming none is simply dropped.
				p := newPeer(t, rev)
				c := dialClient(t, p, clientOpts())
				before := len(p.sent())
				p.notify("notifications/cancelled", map[string]any{"requestId": 4242})
				time.Sleep(50 * time.Millisecond)
				if len(p.sent()) != before {
					t.Errorf("answered a cancellation: %v", p.sent()[before:])
				}
				if _, err := c.ListTools(ctxT(t)); err != nil {
					t.Error(err)
				}
				return
			}
			answered, stopped := cancelledQuestion(t, rev)
			if answered || !stopped {
				t.Errorf("answered=%v stopped=%v", answered, stopped)
			}
		})
	}
	cli("cancellation-log-reasons", func(t *testing.T, rev string) {
		// The reason a server gives for withdrawing its question reaches
		// OnWarning, which the pool writes to the lifecycle log.
		p := newPeer(t, rev)
		o := clientOpts()
		o.OnServerRequest = func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		c := dialClient(t, p, o)
		warned := make(chan string, 4)
		c.Subscribe(mcpclient.Notifications{OnWarning: func(w mcpclient.Warning) { warned <- w.Reason }})
		if isModern(rev) {
			// A 2026-07-28 server asks nothing it could cancel; a
			// cancellation naming nothing is dropped without a word.
			p.notify("notifications/cancelled", map[string]any{"requestId": 4242, "reason": "user gave up"})
			select {
			case w := <-warned:
				t.Errorf("warned about a cancellation of nothing: %s", w)
			case <-time.After(100 * time.Millisecond):
			}
			return
		}
		p.push(frame(4242, "elicitation/create", map[string]any{"message": "?", "requestedSchema": map[string]any{"type": "object"}}))
		time.Sleep(50 * time.Millisecond)
		p.notify("notifications/cancelled", map[string]any{"requestId": 4242, "reason": "user gave up"})
		select {
		case w := <-warned:
			if !strings.Contains(w, "user gave up") || !strings.Contains(w, "4242") {
				t.Errorf("warning %q lacks the reason or the request", w)
			}
		case <-time.After(time.Second):
			t.Error("the cancellation reason was not reported")
		}
	})
	for _, id := range []string{"cancellation-receiver-may-ignore", "cancellation-ignore-invalid", "cancellation-handle-races"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			c := dialClient(t, p, clientOpts())
			for _, pm := range []map[string]any{{"requestId": 999}, {"requestId": 1}, {"requestId": map[string]any{}}, {}} {
				p.notify("notifications/cancelled", pm)
			}
			var wg sync.WaitGroup
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); _, _ = c.ListTools(ctxT(t)) }()
			}
			wg.Wait()
			if _, err := c.ListTools(ctxT(t)); err != nil {
				t.Error(err)
			}
		})
	}

	// ---- progress mcpx asks for: none ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/progress
	for _, id := range []string{"progress-token-string-or-int", "progress-token-unique-sender", "progress-token-unique-client",
		"progress-receiver-may-skip"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			c := dialClient(t, p, clientOpts())
			_, _ = c.CallTool(ctxT(t), "echo", map[string]any{})
			p.notify("notifications/progress", map[string]any{"progressToken": "x", "progress": 1})
			for _, f := range p.sent() {
				if tok, has := asMap(asMap(f["params"])["_meta"])["progressToken"]; has {
					t.Errorf("asked for progress (%v) it does not track", tok)
				}
			}
		})
	}
	for _, id := range []string{"progress-track-tokens", "progress-rate-limit"} {
		cli(id, func(t *testing.T, rev string) {
			got, subscribed := relayedProgress(t, rev)
			// Passed on: 1, 2 and the first of the burst, each after a
			// pause; then the final report, which is never held back; then
			// the second call's one report (1000).
			// Dropped: the stray token, a repeat and a decrease, most of a
			// burst inside the rate limit, anything after completion, and
			// anything after the response.
			if len(got) < 5 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[len(got)-2] != 100 || got[len(got)-1] != 1000 {
				t.Fatalf("relayed %v", got)
			}
			for i := 1; i < len(got)-1; i++ {
				if got[i] <= got[i-1] {
					t.Errorf("relayed out of order: %v", got)
				}
			}
			if len(got) > 10 {
				t.Errorf("a burst of 50 was not throttled: %d relayed", len(got))
			}
			if subscribed != len(got) {
				t.Errorf("the subscriber saw %d progress notifications, the relay %d", subscribed, len(got))
			}
		})
	}

	// ---- pagination ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/pagination
	for _, id := range []string{"pagination-cursor-opaque", "pagination-missing-nextcursor-end", "pagination-no-fixed-page-size",
		"pagination-no-persist-across-sessions", "pagination-support-both-flows"} {
		cli(id, func(t *testing.T, rev string) {
			for _, pages := range [][]int{{1}, {3, 1, 2}} {
				p := newPeer(t, rev)
				var cursors []any
				p.on("tools/list", func(pm map[string]any) (any, *rpcError) {
					i := len(cursors)
					cursors = append(cursors, pm["cursor"])
					var tools []any
					for j := 0; j < pages[i]; j++ {
						tools = append(tools, map[string]any{"name": fmt.Sprintf("t%d_%d", i, j), "inputSchema": map[string]any{"type": "object"}})
					}
					r := map[string]any{"tools": tools}
					if i+1 < len(pages) {
						r["nextCursor"] = fmt.Sprintf("opaque/%d?==", i+1)
					}
					return r, nil
				})
				c := dialClient(t, p, clientOpts())
				tools, err := c.ListTools(ctxT(t))
				want := 0
				for _, n := range pages {
					want += n
				}
				if err != nil || len(tools) != want {
					t.Errorf("pages %v: %d tools, %v", pages, len(tools), err)
				}
				for i, cu := range cursors {
					if i == 0 && cu != nil {
						t.Errorf("first page asked with cursor %v", cu)
					}
					if i > 0 && cu != fmt.Sprintf("opaque/%d?==", i) {
						t.Errorf("cursor %v was not passed back verbatim", cu)
					}
				}
			}
		})
	}
	cli("pagination-empty-cursor-not-end", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		n := 0
		p.on("tools/list", func(pm map[string]any) (any, *rpcError) {
			n++
			if n == 1 {
				return map[string]any{"tools": []any{map[string]any{"name": "a", "inputSchema": map[string]any{"type": "object"}}},
					"nextCursor": "", "resultType": "complete"}, nil
			}
			return map[string]any{"tools": []any{map[string]any{"name": "b", "inputSchema": map[string]any{"type": "object"}}},
				"resultType": "complete"}, nil
		})
		c := dialClient(t, p, clientOpts())
		if tools, _ := c.ListTools(ctxT(t)); len(tools) != 2 {
			t.Errorf("an empty nextCursor ended the list: %d tools", len(tools))
		}
	})

	// ---- logging ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/logging
	cli("logging-client-may-setlevel", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.caps["logging"] = map[string]any{}
		p.on("logging/setLevel", func(map[string]any) (any, *rpcError) { return map[string]any{}, nil })
		c := dialClient(t, p, clientOpts())
		if err := c.SetLogLevel(ctxT(t), "warning"); err != nil {
			t.Fatal(err)
		}
		if len(p.sentMethod("logging/setLevel")) != 1 {
			t.Error("not sent")
		}
		q := newPeer(t, rev)
		c2 := dialClient(t, q, clientOpts())
		_ = c2.SetLogLevel(ctxT(t), "warning")
		if len(q.sentMethod("logging/setLevel")) != 0 {
			t.Error("sent to a server that declared no logging")
		}
	})
	for _, id := range []string{"logging-impl-validate-control", "logging-no-secrets-pii"} {
		cli(id, func(t *testing.T, rev string) {
			// Upstream log messages -- malformed, or carrying a secret --
			// are taken without failing, and never answered.
			p := newPeer(t, rev)
			c := dialClient(t, p, clientOpts())
			for _, pm := range []any{map[string]any{"level": "nonsense", "data": 1}, map[string]any{"level": "info", "data": "token=hunter2"}, "garbage"} {
				p.notify("notifications/message", pm)
			}
			if _, err := c.ListTools(ctxT(t)); err != nil {
				t.Error(err)
			}
		})
	}

	// ---- completion ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/completion
	cli("completion-context-arguments", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.caps["completions"] = map[string]any{}
		p.on("completion/complete", func(map[string]any) (any, *rpcError) {
			return map[string]any{"completion": map[string]any{"values": []string{"x"}}}, nil
		})
		c := dialClient(t, p, clientOpts())
		_, ok, err := c.Complete(ctxT(t), json.RawMessage(`{"ref":{"type":"ref/prompt","name":"p"},"argument":{"name":"b","value":""},"context":{"arguments":{"a":"1"}}}`))
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		got := asMap(asMap(p.sentMethod("completion/complete")[0]["params"])["context"])
		if asMap(got["arguments"])["a"] != "1" {
			t.Errorf("context not carried: %v", got)
		}
	})
	cli("completion-client-should", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		// A server without completions is not asked: the client degrades
		// rather than failing.
		if _, ok, err := c.Complete(ctxT(t), json.RawMessage(`{"ref":{"type":"ref/prompt","name":"p"},"argument":{"name":"b","value":""}}`)); ok || err != nil {
			t.Errorf("ok=%v err=%v", ok, err)
		}
		// 2024-11-05 has the method and no capability to declare it, so
		// there the server is asked and its method-not-found is the same
		// absence (#213, CMP-01).
		asked := strings.Contains(fmt.Sprint(p.sent()), "completion/complete")
		if asked != (rev == "2024-11-05") {
			t.Errorf("asked=%v on %s", asked, rev)
		}
	})
}
