package conformance_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// taskSession is a 2025-11-25 stdio session that started one task-augmented
// tools/call and holds its CreateTaskResult.
func taskSession(t *testing.T, rev string, blocking bool, ttl any) (*stdioSrv, *backend, map[string]any) {
	t.Helper()
	srv, b := newServer(t)
	if blocking {
		b.blocking(t)
	}
	ss := stdioServer(t, srv)
	ss.initialize(t, rev)
	task := map[string]any{}
	if ttl != nil {
		task["ttl"] = ttl
	}
	r := resultOf(t, ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "a", "tool": "b"}, "task": task}))
	tk := asMap(r["task"])
	if tk["taskId"] == nil {
		t.Fatalf("no task: %v", r)
	}
	return ss, b, tk
}

// Tasks, caching, subscriptions, MRTR: mcpx as a server.
func TestStatefulServer(t *testing.T) {
	srvSide := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "server", id, fn) }

	// ---- tasks (2025-11-25 core) ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks
	for _, id := range []string{"tasks-begin-working", "tasks-timestamps", "tasks-poll-interval", "tasks-return-createtaskresult",
		"tasks-id-string-receiver-unique", "tasks-unbound-secure-ids", "tasks-ttl-override-report", "tasks-requestor-may-ttl"} {
		srvSide(id, func(t *testing.T, rev string) {
			_, _, a := taskSession(t, rev, true, 60000)
			_, _, b := taskSession(t, rev, true, nil)
			if a["status"] != "working" {
				t.Errorf("status %v", a["status"])
			}
			for _, k := range []string{"createdAt", "lastUpdatedAt"} {
				if _, err := time.Parse(time.RFC3339, fmt.Sprint(a[k])); err != nil {
					t.Errorf("%s %v", k, a[k])
				}
			}
			if a["pollInterval"] == nil {
				t.Error("no pollInterval")
			}
			if a["ttl"] != float64(60000) || b["ttl"] == nil || b["ttl"].(float64) <= 0 {
				t.Errorf("ttl requested 60000 -> %v; unrequested -> %v", a["ttl"], b["ttl"])
			}
			id := fmt.Sprint(a["taskId"])
			if id == fmt.Sprint(b["taskId"]) || !regexp.MustCompile(`[0-9a-f]{16}`).MatchString(id) {
				t.Errorf("ids %v %v are not unique random handles", a["taskId"], b["taskId"])
			}
		})
	}
	for _, id := range []string{"tasks-result-terminal-final", "tasks-result-blocks", "tasks-result-exact",
		"tasks-sse-no-upgrade-get", "tasks-no-related-task-in-get-list-cancel-results", "tasks-taskid-param-authoritative",
		"tasks-terminal-immutable", "tasks-valid-transitions"} {
		srvSide(id, func(t *testing.T, rev string) {
			ss, b, tk := taskSession(t, rev, true, nil)
			get := resultOf(t, ss.request(t, rev, "tasks/get", map[string]any{"taskId": tk["taskId"],
				"_meta": map[string]any{"io.modelcontextprotocol/related-task": map[string]any{"taskId": "tsk-other"}}}))
			if get["status"] != "working" {
				t.Fatalf("%v", get)
			}
			// tasks/result blocks until the call ends, then gives the
			// tool's own result.
			ss.send(t, frame(50, "tasks/result", map[string]any{"taskId": tk["taskId"]}))
			if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
				t.Fatalf("tasks/result did not wait: %s", f)
			}
			b.block <- struct{}{}
			res := resultOf(t, ss.response(t, rev, 50))
			if !strings.Contains(fmt.Sprint(res["content"]), "called a.b") {
				t.Errorf("not the tool's result: %v", res)
			}
			for i := 0; i < 2; i++ {
				g := resultOf(t, ss.request(t, rev, "tasks/get", map[string]any{"taskId": tk["taskId"]}))
				if g["status"] != "completed" {
					t.Errorf("status %v", g["status"])
				}
				if _, has := asMap(g["_meta"])["io.modelcontextprotocol/related-task"]; has {
					t.Errorf("related-task on a tasks/get result: %v", g)
				}
			}
		})
	}
	for _, id := range []string{"tasks-cancel-transition-before-response", "tasks-cancelled-sticky", "tasks-cancelled-may-delete"} {
		srvSide(id, func(t *testing.T, rev string) {
			ss, _, tk := taskSession(t, rev, true, nil)
			c := resultOf(t, ss.request(t, rev, "tasks/cancel", map[string]any{"taskId": tk["taskId"]}))
			if c["status"] != "cancelled" {
				t.Errorf("cancel answered %v", c)
			}
			time.Sleep(50 * time.Millisecond)
			if g := resultOf(t, ss.request(t, rev, "tasks/get", map[string]any{"taskId": tk["taskId"]})); g["status"] != "cancelled" {
				t.Errorf("after cancel: %v", g["status"])
			}
		})
	}
	for _, id := range []string{"tasks-informative-errors", "tasks-result-same-error"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			r := ss.request(t, rev, "tasks/get", map[string]any{"taskId": "tsk-nope"})
			if errorCode(r) != -32602 || !strings.Contains(fmt.Sprint(r["error"]), "tsk-nope") {
				t.Errorf("%v", r)
			}
			// A task whose request fails reports the same error.
			// (mcpx_call without its arguments fails as a tool.)
			tr := resultOf(t, ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_call",
				"arguments": map[string]any{}, "task": map[string]any{}}))
			res := ss.request(t, rev, "tasks/result", map[string]any{"taskId": asMap(tr["task"])["taskId"]})
			direct := ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_call", "arguments": map[string]any{}})
			// tasks/result alone MUST add the related-task _meta; the rest
			// is the same result.
			if m := asMap(asMap(res["result"])["_meta"]); m != nil {
				delete(m, "io.modelcontextprotocol/related-task")
				if len(m) == 0 {
					delete(asMap(res["result"]), "_meta")
				}
			}
			a, _ := json.Marshal(res["result"])
			d, _ := json.Marshal(direct["result"])
			if errorCode(res) != errorCode(direct) || string(a) != string(d) {
				t.Errorf("task %s / %v vs direct %s / %v", a, res["error"], d, direct["error"])
			}
		})
	}
	srvSide("tasks-list-paginate", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.PageSize = 2
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		for i := 0; i < 5; i++ {
			ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_call",
				"arguments": map[string]any{"namespace": "a", "tool": "b"}, "task": map[string]any{}})
		}
		seen, cursor := 0, any(nil)
		for i := 0; i < 10; i++ {
			p := map[string]any{}
			if cursor != nil {
				p["cursor"] = cursor
			}
			r := resultOf(t, ss.request(t, rev, "tasks/list", p))
			seen += len(r["tasks"].([]any))
			if cursor = r["nextCursor"]; cursor == nil {
				break
			}
		}
		if seen != 5 {
			t.Errorf("listed %d of 5", seen)
		}
	})
	srvSide("tasks-protocol-errors", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		for _, c := range []struct {
			method string
			p      map[string]any
		}{{"tasks/get", map[string]any{"taskId": "tsk-nope"}}, {"tasks/result", map[string]any{"taskId": "tsk-nope"}},
			{"tasks/cancel", map[string]any{"taskId": "tsk-nope"}}, {"tasks/list", map[string]any{"cursor": "not-a-cursor"}}} {
			if r := ss.request(t, rev, c.method, c.p); errorCode(r) != -32602 {
				t.Errorf("%s: %v", c.method, r)
			}
		}
	})
	srvSide("tasks-declare-capability", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		caps := asMap(resultOf(t, ss.initialize(t, rev))["capabilities"])
		if asMap(asMap(asMap(caps["tasks"])["requests"])["tools"])["call"] == nil {
			t.Errorf("%v", caps)
		}
	})
	srvSide("tasks-receiver-without-cap-ignore", func(t *testing.T, rev string) {
		// Task augmentation on a method that does not support it is ignored:
		// the ordinary result comes back.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		r := resultOf(t, ss.request(t, rev, "tools/list", map[string]any{"task": map[string]any{}}))
		if r["tools"] == nil {
			t.Errorf("%v", r)
		}
	})
	for _, id := range []string{"tasks-augment-only-if-declared", "tasks-no-capability-no-tasks",
		"tasks-status-notification-no-related-task"} {
		srvSide(id, func(t *testing.T, rev string) {
			// mcpx's own requests (questions) carry no task, and it sends
			// no task status notifications.
			q, after, _ := unanswered(t, rev)
			if _, has := asMap(q["params"])["task"]; has {
				t.Errorf("%v", q)
			}
			for _, f := range after {
				if f["method"] == "notifications/tasks/status" {
					t.Errorf("%v", f)
				}
			}
		})
	}
	srvSide("tasks-cancel-terminal-reject", func(t *testing.T, rev string) {
		ss, b, tk := taskSession(t, rev, true, nil)
		b.block <- struct{}{}
		ss.request(t, rev, "tasks/result", map[string]any{"taskId": tk["taskId"]})
		if r := ss.request(t, rev, "tasks/cancel", map[string]any{"taskId": tk["taskId"]}); errorCode(r) != -32602 {
			t.Errorf("cancelling a completed task: %v", r)
		}
	})
	srvSide("tasks-tool-forbidden-default", func(t *testing.T, rev string) {
		// No mcpx tool declares execution.taskSupport, so each is
		// "forbidden": a task-augmented call must be refused.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		r := ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}, "task": map[string]any{}})
		if errorCode(r) != -32601 {
			t.Errorf("%v", r)
		}
	})

	srvSide("tasks-delete-after-ttl", func(t *testing.T, rev string) {
		ss, b, tk := taskSession(t, rev, true, 100)
		b.block <- struct{}{}
		time.Sleep(400 * time.Millisecond)
		if r := ss.request(t, rev, "tasks/get", map[string]any{"taskId": tk["taskId"]}); errorCode(r) == 0 {
			t.Errorf("kept past its ttl: %v", r)
		}
	})

	// ---- subscriptions (legacy) ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#list-changed-notification
	srvSide("subscriptions-list-changed-should-notify", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		n := newNotifier()
		srv.Notify = n
		ss := stdioServer(t, srv)
		caps := asMap(resultOf(t, ss.initialize(t, rev))["capabilities"])
		if asMap(caps["prompts"])["listChanged"] != true {
			t.Fatalf("%v", caps)
		}
		time.Sleep(50 * time.Millisecond)
		// (mcpx's own tool list is fixed, so it has no tools list_changed.)
		for _, m := range []string{"prompts", "resources"} {
			n.ch <- [2]any{"notifications/" + m + "/list_changed", map[string]any{}}
			if f := decode(t, ss.next(t)); f["method"] != "notifications/"+m+"/list_changed" {
				t.Errorf("%v", f)
			}
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#subscriptions
	srvSide("subscriptions-resources-updated-only-if-subscribed", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		n := newNotifier()
		srv.Notify = n
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		time.Sleep(50 * time.Millisecond)
		n.ch <- [2]any{"notifications/resources/updated", map[string]any{"uri": "mem://alpha/one"}}
		if f, quiet := ss.quiet(150 * time.Millisecond); !quiet {
			t.Fatalf("sent before any subscription: %s", f)
		}
		ss.request(t, rev, "resources/subscribe", map[string]any{"uri": "mem://alpha/one"})
		time.Sleep(50 * time.Millisecond)
		n.ch <- [2]any{"notifications/resources/updated", map[string]any{"uri": "mem://alpha/two"}}
		n.ch <- [2]any{"notifications/resources/updated", map[string]any{"uri": "mem://alpha/one"}}
		f := decode(t, ss.next(t))
		if asMap(f["params"])["uri"] != "mem://alpha/one" {
			t.Errorf("%v", f)
		}
		if g, quiet := ss.quiet(150 * time.Millisecond); !quiet {
			t.Errorf("unsubscribed update sent: %s", g)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks#input-required-status
	taskAsks := func(t *testing.T, rev string) (question, status map[string]any, task string, ss *stdioSrv) {
		srv, _ := newServer(t)
		srv.Ask = newAsker("done", elicitQ("q"))
		srv.Timing = mcpserver.Timing{AskTimeout: 2 * time.Second, AskPoll: 20 * time.Millisecond}
		ss = stdioServer(t, srv)
		ss.request(t, rev, "initialize", initParams(rev, map[string]any{"elicitation": map[string]any{}}))
		ss.send(t, frame(nil, "notifications/initialized", nil))
		r := resultOf(t, ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_call",
			"arguments": map[string]any{"namespace": "up", "tool": "t"}, "task": map[string]any{}}))
		task = fmt.Sprint(asMap(r["task"])["taskId"])
		for question == nil {
			m := decode(t, ss.next(t))
			if m["method"] == "elicitation/create" {
				question = m
			}
		}
		status = resultOf(t, ss.request(t, rev, "tasks/get", map[string]any{"taskId": task}))
		return question, status, task, ss
	}
	for _, id := range []string{"tasks-input-request-related-task", "tasks-input-required-move", "elicitation-task-related-id-shared"} {
		srvSide(id, func(t *testing.T, rev string) {
			q, st, task, _ := taskAsks(t, rev)
			rt := asMap(asMap(asMap(q["params"])["_meta"])["io.modelcontextprotocol/related-task"])
			if fmt.Sprint(rt["taskId"]) != task {
				t.Errorf("the question does not name its task: %v", q)
			}
			if st["status"] != "input_required" {
				t.Errorf("status while asking: %v", st["status"])
			}
		})
	}
	for _, id := range []string{"tasks-result-response-related-task", "tasks-related-task-all-messages"} {
		srvSide(id, func(t *testing.T, rev string) {
			q, _, task, ss := taskAsks(t, rev)
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": map[string]any{"action": "accept", "content": map[string]any{"repo": "x"}}})
			ss.send(t, b)
			r := resultOf(t, ss.request(t, rev, "tasks/result", map[string]any{"taskId": task}))
			if fmt.Sprint(asMap(asMap(r["_meta"])["io.modelcontextprotocol/related-task"])["taskId"]) != task {
				t.Errorf("tasks/result does not name its task: %v", r)
			}
		})
	}
	srvSide("tasks-leave-input-required", func(t *testing.T, rev string) {
		q, _, task, ss := taskAsks(t, rev)
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": map[string]any{"action": "accept", "content": map[string]any{"repo": "x"}}})
		ss.send(t, b)
		time.Sleep(200 * time.Millisecond)
		if st := resultOf(t, ss.request(t, rev, "tasks/get", map[string]any{"taskId": task})); st["status"] == "input_required" {
			t.Errorf("still input_required after the answer")
		}
	})

	// ---- tasks extension (2026-07-28) ----

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/utilities/tasks
	srvSide("ext-tasks-get-input-requests", func(t *testing.T, rev string) {
		// A client that can answer is never handed a task that then waits
		// on input: it gets the question in the result.
		srv, _ := newServer(t)
		srv.Ask = newAsker("done", elicitQ("q"))
		ss := stdioServer(t, srv)
		r := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev,
			`{"elicitation":{},"extensions":{"io.modelcontextprotocol/tasks":{}}}`, callThatAsks())))
		if r["resultType"] != "input_required" {
			t.Errorf("%v", r)
		}
	})

	// ---- caching ----

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/utilities/caching
	srvSide("caching-ttl-non-negative", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.Cache = mcpserver.Cache{List: -5 * time.Second, Read: -time.Second}
		ss := stdioServer(t, srv)
		for _, m := range []string{"tools/list", "server/discover"} {
			if r := resultOf(t, ss.request(t, rev, m, nil)); r["ttlMs"] != float64(0) {
				t.Errorf("%s ttlMs %v", m, r["ttlMs"])
			}
		}
	})

	// ---- MRTR ----

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/utilities/mrtr
	for _, id := range []string{"mrtr-at-least-one-field", "mrtr-inputrequests-keys-values", "mrtr-input-request-type-restricted",
		"mrtr-only-declared-capabilities", "mrtr-supported-requests-only"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			srv.Ask = newAsker("done", elicitQ("e1"), sampleQ("s1"))
			ss := stdioServer(t, srv)
			r := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks())))
			ir := asMap(r["inputRequests"])
			if r["requestState"] == nil && ir == nil {
				t.Fatalf("%v", r)
			}
			if len(ir) != 1 || asMap(ir["e1"])["method"] != "elicitation/create" {
				t.Errorf("only the elicitation the client declared, keyed by its id: %v", ir)
			}
		})
	}
	srvSide("mrtr-server-validate-responses", func(t *testing.T, rev string) {
		// Answers keyed to questions never asked are not taken as answers:
		// the question is asked again.
		srv, _ := newServer(t)
		srv.Ask = newAsker("done", elicitQ("e1"))
		ss := stdioServer(t, srv)
		first := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks())))
		p := callThatAsks()
		p["requestState"] = first["requestState"]
		p["inputResponses"] = map[string]any{"bogus": map[string]any{"action": "accept", "content": map[string]any{}}}
		again := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, p)))
		if again["resultType"] != "input_required" {
			t.Errorf("%v", again)
		}
	})
}

// Tasks, caching, subscriptions, MRTR: mcpx as a client.
func TestStatefulClient(t *testing.T) {
	cli := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "client", id, fn) }

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks
	for _, id := range []string{"tasks-augment-only-if-declared", "tasks-no-capability-no-tasks",
		"tasks-tool-no-cap-no-augment", "tasks-status-notification-optional"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			p.caps["tasks"] = map[string]any{"requests": map[string]any{"tools": map[string]any{"call": map[string]any{}}}}
			c := dialClient(t, p, clientOpts())
			if _, err := c.CallTool(ctxT(t), "echo", map[string]any{}); err != nil {
				t.Fatal(err)
			}
			for _, f := range p.sent() {
				if _, has := asMap(f["params"])["task"]; has {
					t.Errorf("task-augmented: %v", f)
				}
			}
		})
	}

	// ---- MRTR ----

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/utilities/mrtr
	mrtr := func(t *testing.T, rev string) []map[string]any {
		p := newPeer(t, rev)
		var calls []map[string]any
		p.on("tools/call", func(pm map[string]any) (any, *rpcError) {
			calls = append(calls, pm)
			if len(calls) == 1 {
				return map[string]any{"resultType": "input_required", "requestState": "opaque/ÿ==",
					"inputRequests": map[string]any{"r1": map[string]any{"method": "roots/list", "params": map[string]any{}}}}, nil
			}
			return map[string]any{"resultType": "complete", "content": []any{}, "ttlMs": 0, "cacheScope": "private"}, nil
		})
		c := dialClient(t, p, clientOpts())
		if _, err := c.CallTool(ctxT(t), "echo", map[string]any{"x": 1}); err != nil {
			t.Fatal(err)
		}
		// Another, unrelated call: the state must not travel to it.
		_, _ = c.CallTool(ctxT(t), "other", map[string]any{})
		var ids []any
		for _, f := range p.sentMethod("tools/call") {
			ids = append(ids, f["id"])
		}
		if len(ids) != 3 || ids[0] == ids[1] {
			t.Errorf("request ids %v", ids)
		}
		return calls
	}
	for _, id := range []string{"mrtr-client-echo-state", "mrtr-client-fulfil-before-retry", "mrtr-new-id-on-retry",
		"mrtr-no-cross-request-use", "mrtr-requeststate-opaque-client", "caching-no-cache-mrtr-retries"} {
		cli(id, func(t *testing.T, rev string) {
			calls := mrtr(t, rev)
			if len(calls) != 3 {
				t.Fatalf("%d calls reached the server", len(calls))
			}
			if calls[1]["requestState"] != "opaque/ÿ==" {
				t.Errorf("state not echoed verbatim: %v", calls[1]["requestState"])
			}
			if _, ok := asMap(asMap(calls[1]["inputResponses"])["r1"])["roots"]; !ok {
				t.Errorf("retry without the answer: %v", calls[1])
			}
			if calls[2]["requestState"] != nil || calls[2]["inputResponses"] != nil {
				t.Errorf("state carried into another request: %v", calls[2])
			}
		})
	}

	// ---- caching ----

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/utilities/caching
	cli("caching-ttl-not-poll", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("tools/list", func(map[string]any) (any, *rpcError) {
			return map[string]any{"resultType": "complete", "tools": []any{}, "ttlMs": 20, "cacheScope": "public"}, nil
		})
		c := dialClient(t, p, clientOpts())
		_, _ = c.ListTools(ctxT(t))
		n := len(p.sent())
		time.Sleep(200 * time.Millisecond)
		if len(p.sent()) != n {
			t.Errorf("the client polled when the ttl ran out: %v", p.sent()[n:])
		}
	})
	_ = mcpclient.ProtocolVersion
}
