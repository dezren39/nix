package conformance_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// loopingAsker raises a fresh question every time the last is answered:
// an upstream that never stops asking.
type loopingAsker struct {
	mu sync.Mutex
	n  int
	q  func(id string) mcpserver.Question
}

func (a *loopingAsker) Begin(context.Context, string, json.RawMessage) (string, error) {
	return "c", nil
}
func (a *loopingAsker) Poll(context.Context, string, time.Duration) (mcpserver.Outcome, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return mcpserver.Outcome{Questions: []mcpserver.Question{a.q(fmt.Sprintf("q%d", a.n))}}, nil
}
func (a *loopingAsker) Reply(context.Context, string, map[string]json.RawMessage) error {
	a.mu.Lock()
	a.n++
	a.mu.Unlock()
	return nil
}
func (a *loopingAsker) Abandon(string) {}

// relayed is what a legacy client that declared caps is sent when an
// upstream raises q during a tools/call: the request frame.
func relayed(t *testing.T, rev string, caps map[string]any, q mcpserver.Question) map[string]any {
	t.Helper()
	srv, _ := newServer(t)
	srv.Ask = newAsker("done", q)
	srv.Timing = mcpserver.Timing{AskTimeout: 300 * time.Millisecond, AskPoll: 20 * time.Millisecond}
	ss := stdioServer(t, srv)
	ss.request(t, rev, "initialize", initParams(rev, caps))
	ss.send(t, frame(nil, "notifications/initialized", nil))
	ss.send(t, frame(9, "tools/call", callThatAsks()))
	for {
		b := ss.next(t)
		m := decode(t, b)
		if m["method"] != nil && m["id"] != nil {
			checkServerFrame(t, rev, b, "")
			return m
		}
		if m["id"] == float64(9) {
			return nil
		}
	}
}

// Roots, sampling, elicitation: mcpx as a server (relaying an upstream's
// questions to its own client).
func TestClientFeaturesServer(t *testing.T) {
	srvSide := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "server", id, fn) }

	// ---- sampling relayed ----

	toolLoop := `{"messages":[{"role":"user","content":{"type":"text","text":"weather?"}},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"get","input":{},"_meta":{"k":1}}]},
		{"role":"user","content":[{"type":"tool_result","toolUseId":"tu1","content":[{"type":"text","text":"sunny"}],"structuredContent":{"t":20}}]}],
		"maxTokens":50,"stopSequences":["x"]}`
	// https://modelcontextprotocol.io/specification/2025-11-25/client/sampling
	for _, id := range []string{"sampling-tool-result-message-only-tool-results", "sampling-tool-use-result-balance",
		"sampling-toolresult-id-must-match-tooluse", "sampling-toolresult-structured-conform-outputschema",
		"sampling-message-must-have-role-and-content", "sampling-server-answer-every-tooluse"} {
		srvSide(id, func(t *testing.T, rev string) {
			// mcpx does not author sampling messages; it relays an
			// upstream's unchanged, so their structure is the upstream's.
			q := mcpserver.Question{ID: "s", Method: "sampling/createMessage", Server: "up", Params: json.RawMessage(toolLoop)}
			var sent map[string]any
			if isModern(rev) {
				srv, _ := newServer(t)
				srv.Ask = newAsker("done", q)
				ss := stdioServer(t, srv)
				r := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"sampling":{"tools":{}}}`, callThatAsks())))
				sent = asMap(asMap(asMap(r["inputRequests"])["s"])["params"])
			} else {
				sent = asMap(relayed(t, rev, map[string]any{"sampling": map[string]any{"tools": map[string]any{}}}, q)["params"])
			}
			var want map[string]any
			_ = json.Unmarshal([]byte(toolLoop), &want)
			a, _ := json.Marshal(sent["messages"])
			b, _ := json.Marshal(want["messages"])
			if string(a) != string(b) {
				t.Errorf("messages changed in transit:\n%s\n%s", a, b)
			}
		})
	}
	srvSide("sampling-deprecated-new-impls-should-not-adopt", func(t *testing.T, rev string) {
		// mcpx originates no sampling of its own: a modern call with
		// nothing upstream asking is answered without any request.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		r := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"sampling":{}}`,
			map[string]any{"name": "mcpx_status", "arguments": map[string]any{}})))
		if r["resultType"] != "complete" {
			t.Errorf("%v", r)
		}
	})
	srvSide("sampling-both-iteration-limits-tool-loops", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.Ask = &loopingAsker{q: func(id string) mcpserver.Question {
			return mcpserver.Question{ID: id, Method: "sampling/createMessage", Server: "up",
				Params: json.RawMessage(`{"messages":[{"role":"user","content":{"type":"text","text":"again"}}],"maxTokens":5}`)}
		}}
		srv.Timing = mcpserver.Timing{AskRounds: 3, AskTimeout: wait, AskPoll: 10 * time.Millisecond}
		ss := stdioServer(t, srv)
		p := callThatAsks()
		var r map[string]any
		for round := 0; round < 10; round++ {
			r = ss.request(t, rev, "tools/call", paramsWith(rev, `{"sampling":{}}`, p))
			res := asMap(r["result"])
			if res["resultType"] != "input_required" {
				break
			}
			p = callThatAsks()
			p["requestState"] = res["requestState"]
			resp := map[string]any{}
			for k := range asMap(res["inputRequests"]) {
				resp[k] = map[string]any{"role": "assistant", "model": "m", "content": map[string]any{"type": "text", "text": "ok"}}
			}
			p["inputResponses"] = resp
		}
		if errorCode(r) == 0 && asMap(r["result"])["resultType"] == "input_required" {
			t.Errorf("an upstream that never stops asking was never cut off")
		}
	})

	// ---- elicitation relayed ----

	// https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation
	for _, id := range []string{"elicitation-request-must-include-mode-and-message", "elicitation-form-request-shape",
		"elicitation-server-may-omit-mode-for-form", "elicitation-no-clickable-urls-in-form",
		"elicitation-server-no-sensitive-info-in-form-mode", "elicitation-server-no-sensitive-info",
		"elicitation-form-schema-flat-primitives", "elicitation-server-may-request-during-client-request",
		"elicitation-empty-capability-means-form-only", "elicitation-server-no-undeclared-modes"} {
		srvSide(id, func(t *testing.T, rev string) {
			var p map[string]any
			if isModern(rev) {
				srv, _ := newServer(t)
				srv.Ask = newAsker("done", elicitQ("e"), urlQ("u"))
				ss := stdioServer(t, srv)
				r := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks())))
				ir := asMap(r["inputRequests"])
				if ir["u"] != nil {
					t.Errorf("url mode sent to a form-only client: %v", ir)
				}
				p = asMap(asMap(ir["e"])["params"])
			} else {
				srv, _ := newServer(t)
				srv.Ask = newAsker("done", urlQ("u"), elicitQ("e"))
				srv.Timing = mcpserver.Timing{AskTimeout: 300 * time.Millisecond, AskPoll: 20 * time.Millisecond}
				ss := stdioServer(t, srv)
				ss.request(t, rev, "initialize", initParams(rev, map[string]any{"elicitation": map[string]any{}}))
				ss.send(t, frame(nil, "notifications/initialized", nil))
				ss.send(t, frame(9, "tools/call", callThatAsks()))
				for {
					m := decode(t, ss.next(t))
					if m["method"] == "elicitation/create" {
						p = asMap(m["params"])
						if p["mode"] == "url" || p["url"] != nil {
							t.Errorf("url mode sent to a form-only client: %v", p)
						}
						continue
					}
					if m["id"] == float64(9) {
						break
					}
				}
			}
			if p == nil {
				t.Fatal("no form question")
			}
			if msg, _ := p["message"].(string); msg == "" || strings.Contains(msg, "http") {
				t.Errorf("message %q", p["message"])
			}
			if p["requestedSchema"] == nil {
				t.Errorf("no requestedSchema: %v", p)
			}
			if rev < rev20251125 && p["mode"] != nil {
				t.Errorf("mode sent to %s: %v", rev, p)
			}
		})
	}
	for _, id := range []string{"elicitation-url-request-shape-with-id", "elicitation-url-request-shape", "elicitation-url-must-be-valid",
		"elicitation-url-no-sensitive-user-info", "elicitation-url-not-pre-authenticated", "elicitation-url-https-outside-dev",
		"elicitation-server-sensitive-via-url-mode", "elicitation-server-must-not-self-authorize-via-url",
		"elicitation-third-party-creds-not-via-client", "elicitation-user-authorizes-server-directly",
		"elicitation-server-not-transmit-url-creds-to-client"} {
		srvSide(id, func(t *testing.T, rev string) {
			// mcpx carries an upstream's URL unchanged -- adds nothing to it,
			// mints none of its own -- to a client that declared url mode.
			var p map[string]any
			caps := map[string]any{"elicitation": map[string]any{"url": map[string]any{}, "form": map[string]any{}}}
			if isModern(rev) {
				srv, _ := newServer(t)
				srv.Ask = newAsker("done", urlQ("u"))
				ss := stdioServer(t, srv)
				r := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{"url":{},"form":{}}}`, callThatAsks())))
				p = asMap(asMap(asMap(r["inputRequests"])["u"])["params"])
				if _, has := p["elicitationId"]; has {
					t.Errorf("2026-07-28 has no elicitationId: %v", p)
				}
			} else {
				p = asMap(relayed(t, rev, caps, urlQ("u"))["params"])
				if p["elicitationId"] == nil {
					t.Errorf("url mode without an elicitationId: %v", p)
				}
			}
			if p["url"] != "https://auth.example/connect?state=abc" || p["mode"] != "url" {
				t.Errorf("the URL was not carried as given: %v", p)
			}
		})
	}
	srvSide("elicitation-no-32042-in-2026", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.Ask = newAsker("done", urlQ("u"))
		srv.Timing = mcpserver.Timing{AskTimeout: 200 * time.Millisecond, AskPoll: 20 * time.Millisecond}
		ss := stdioServer(t, srv)
		// A url question for a form-only client stays with the broker:
		// never -32042.
		if r := ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks())); errorCode(r) == -32042 {
			t.Errorf("%v", r)
		}
	})
	for _, id := range []string{"elicitation-server-handle-decline-cancel-failure", "elicitation-server-bind-to-client-and-user"} {
		srvSide(id, func(t *testing.T, rev string) {
			// A decline is taken as an answer and the call goes on; the
			// requestState only resumes the request it was minted for.
			if !isModern(rev) {
				// The question goes to the session whose call raised it,
				// and to no other.
				srv, _ := newServer(t)
				srv.Notify = newNotifier()
				srv.Ask = newAsker("done", elicitQ("e"))
				srv.Timing = mcpserver.Timing{AskTimeout: 300 * time.Millisecond, AskPoll: 20 * time.Millisecond}
				hs := httpServer(t, srv)
				var sess [2]string
				for i := range sess {
					r := hs.post(t, frame(0, "initialize", initParams(rev, map[string]any{"elicitation": map[string]any{}})), headersFor(rev20250326, "", "", ""))
					sess[i] = r.Header.Get("Mcp-Session-Id")
					hs.post(t, frame(nil, "notifications/initialized", nil), headersFor(rev, sess[i], "", ""))
				}
				get, _ := http.NewRequest(http.MethodGet, hs.ts.URL, nil)
				for k, v := range headersFor(rev, sess[1], "", "") {
					get.Header.Set(k, v)
				}
				other := openStream(t, hs.ts.Client(), get)
				res := hs.post(t, frame(9, "tools/call", callThatAsks()), headersFor(rev, sess[0], "tools/call", "mcpx_call"))
				if !strings.Contains(string(res.Body), "elicitation/create") {
					t.Fatalf("the asking session was not asked: %s", res.Body)
				}
				lines, _ := other.rest(100 * time.Millisecond)
				for _, f := range dataFrames(t, lines) {
					if f["method"] == "elicitation/create" {
						t.Error("another session was asked")
					}
				}
				return
			}
			srv, _ := newServer(t)
			a := newAsker("done", elicitQ("e"))
			srv.Ask = a
			ss := stdioServer(t, srv)
			first := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks())))
			other := map[string]any{"name": "mcpx_call", "arguments": map[string]any{"namespace": "other", "tool": "t"},
				"requestState": first["requestState"], "inputResponses": map[string]any{"e": map[string]any{"action": "decline"}}}
			if r := ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, other)); errorCode(r) == 0 {
				t.Errorf("a requestState resumed another request: %v", r)
			}
			p := callThatAsks()
			p["requestState"] = first["requestState"]
			p["inputResponses"] = map[string]any{"e": map[string]any{"action": "decline"}}
			if r := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, p))); r["resultType"] != "complete" {
				t.Errorf("a decline did not end the question: %v", r)
			}
		})
	}
	for _, id := range []string{"elicitation-url-required-error-only-when-required", "elicitation-url-required-error-lists-elicitations",
		"elicitation-url-required-error-url-mode-with-id", "elicitation-server-may-return-url-required-error",
		"elicitation-complete-only-to-initiating-client", "elicitation-complete-must-carry-id",
		"elicitation-server-may-send-complete-notification"} {
		srvSide(id, func(t *testing.T, rev string) {
			// mcpx sends neither -32042 nor notifications/elicitation/complete,
			// so neither can be sent wrongly.
			_, after, _ := unanswered(t, rev)
			for _, f := range after {
				if f["method"] == "notifications/elicitation/complete" || errorCode(f) == -32042 {
					t.Errorf("%v", f)
				}
			}
		})
	}
}

func urlQ(id string) mcpserver.Question {
	return mcpserver.Question{ID: id, Method: "elicitation/create", Mode: "url", Server: "up",
		Params: json.RawMessage(`{"mode":"url","message":"connect your account","url":"https://auth.example/connect?state=abc","elicitationId":"up-1"}`)}
}

// Roots, sampling, elicitation: mcpx as a client (answering an upstream).
func TestClientFeaturesClient(t *testing.T) {
	cli := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "client", id, fn) }

	declared := func(t *testing.T, rev string, o mcpclient.Options) map[string]any {
		p := newPeer(t, rev)
		dialClient(t, p, o)
		f := p.sent()[0]
		if isModern(rev) {
			return asMap(asMap(asMap(f["params"])["_meta"])[mcpserver.MetaClientCapabilities])
		}
		return asMap(asMap(f["params"])["capabilities"])
	}
	handler := func(context.Context, string, json.RawMessage) (any, error) { return map[string]any{}, nil }

	// https://modelcontextprotocol.io/specification/2025-11-25/client/roots
	for _, id := range []string{"roots-client-declare-capability-at-initialize", "roots-client-declare-capability-per-request-meta",
		"roots-client-listchanged-must-notify", "roots-deprecated-new-impls-should-not-adopt", "roots-deprecated-existing-should-migrate"} {
		cli(id, func(t *testing.T, rev string) {
			c := declared(t, rev, clientOpts())
			r, ok := c["roots"]
			if !ok {
				t.Fatalf("roots not declared: %v", c)
			}
			// listChanged is not claimed, because the set never changes.
			if asMap(r)["listChanged"] == true {
				t.Errorf("listChanged claimed: %v", r)
			}
		})
	}
	cli("roots-client-return-jsonrpc-errors", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		dialClient(t, p, clientOpts())
		if r := p.ask(t, "roots/list", nil); r["result"] == nil {
			t.Errorf("%v", r)
		}
		if r := p.ask(t, "x/unknown", nil); errorCode(r) != -32601 {
			t.Errorf("%v", r)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/client/sampling
	for _, id := range []string{"sampling-client-declare-capability-at-initialize", "sampling-client-declare-capability-per-request-meta",
		"sampling-client-declare-tools-capability", "sampling-deprecated-new-impls-should-not-adopt"} {
		cli(id, func(t *testing.T, rev string) {
			if _, ok := declared(t, rev, clientOpts())["sampling"]; ok {
				t.Error("sampling declared with nothing to answer it")
			}
			o := clientOpts()
			o.OnServerRequest = handler
			c := declared(t, rev, o)
			if _, ok := c["sampling"]; !ok {
				t.Errorf("sampling not declared with a handler: %v", c)
			}
			if asMap(c["sampling"])["tools"] != nil {
				t.Errorf("sampling.tools claimed: %v", c)
			}
		})
	}
	// A server's question is handed to the handler as sent, and the
	// handler's answer goes back as given.
	passThrough := func(t *testing.T, rev, method, params string, answer any) (got json.RawMessage, reply map[string]any) {
		var mu sync.Mutex
		o := clientOpts()
		o.OnServerRequest = func(_ context.Context, m string, p json.RawMessage) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			if m == method {
				got = append(json.RawMessage(nil), p...)
			}
			return answer, nil
		}
		if isModern(rev) {
			pr := newPeer(t, rev)
			n := 0
			pr.on("tools/call", func(pm map[string]any) (any, *rpcError) {
				if n++; n == 1 {
					var pp any
					_ = json.Unmarshal([]byte(params), &pp)
					return map[string]any{"resultType": "input_required", "requestState": "s",
						"inputRequests": map[string]any{"q": map[string]any{"method": method, "params": pp}}}, nil
				}
				reply = asMap(asMap(pm["inputResponses"])["q"])
				return map[string]any{"resultType": "complete", "content": []any{}}, nil
			})
			c := dialClient(t, pr, o)
			if _, err := c.CallTool(ctxT(t), "echo", map[string]any{}); err != nil {
				t.Fatal(err)
			}
			return got, map[string]any{"result": reply}
		}
		pr := newPeer(t, rev)
		dialClient(t, pr, o)
		var pp any
		_ = json.Unmarshal([]byte(params), &pp)
		reply = pr.ask(t, method, pp)
		return got, reply
	}
	sampleParams := `{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}],"maxTokens":7,"temperature":0.2,
		"modelPreferences":{"hints":[{"name":"claude"},{"name":"gpt"}],"speedPriority":0.9},"includeContext":"none","systemPrompt":"be brief","metadata":{"k":"v"}}`
	sampleAnswer := map[string]any{"role": "assistant", "model": "m1", "stopReason": "x-vendor-stop",
		"content": map[string]any{"type": "text", "text": "ok", "_meta": map[string]any{"k": 1}}}
	for _, id := range []string{"sampling-stopreason-custom-values", "sampling-tooluse-meta-preserve",
		"sampling-toolresult-id-must-match-tooluse", "sampling-tool-result-message-only-tool-results",
		"sampling-tool-use-result-balance", "sampling-toolresult-structured-conform-outputschema",
		"sampling-message-must-have-role-and-content"} {
		cli(id, func(t *testing.T, rev string) {
			got, reply := passThrough(t, rev, "sampling/createMessage", sampleParams, sampleAnswer)
			var a, b any
			_ = json.Unmarshal(got, &a)
			_ = json.Unmarshal([]byte(sampleParams), &b)
			if fmt.Sprint(a) != fmt.Sprint(b) {
				t.Errorf("the request changed on the way to the answerer: %s", got)
			}
			res := asMap(reply["result"])
			if res["stopReason"] != "x-vendor-stop" || asMap(asMap(res["content"])["_meta"])["k"] != float64(1) {
				t.Errorf("the answer changed on the way back: %v", reply)
			}
		})
	}
	for _, id := range []string{"sampling-client-errors-user-rejected", "sampling-human-in-loop-can-deny"} {
		cli(id, func(t *testing.T, rev string) {
			o := clientOpts()
			o.OnServerRequest = func(context.Context, string, json.RawMessage) (any, error) {
				return nil, errors.New("the user declined")
			}
			if isModern(rev) {
				// The refusal must not become an answer in the retry.
				p := newPeer(t, rev)
				n := 0
				var retry map[string]any
				p.on("tools/call", func(pm map[string]any) (any, *rpcError) {
					if n++; n == 1 {
						var sp any
						_ = json.Unmarshal([]byte(sampleParams), &sp)
						return map[string]any{"resultType": "input_required", "requestState": "s",
							"inputRequests": map[string]any{"q": map[string]any{"method": "sampling/createMessage", "params": sp}}}, nil
					}
					retry = pm
					return map[string]any{"resultType": "complete", "content": []any{}}, nil
				})
				c := dialClient(t, p, o)
				_, err := c.CallTool(ctxT(t), "echo", map[string]any{})
				if err == nil && asMap(asMap(retry["inputResponses"])["q"])["content"] != nil {
					t.Errorf("a refusal was answered as a sample: %v", retry)
				}
				return
			}
			p := newPeer(t, rev)
			dialClient(t, p, o)
			r := p.ask(t, "sampling/createMessage", json.RawMessage(sampleParams))
			if r["error"] == nil {
				t.Errorf("a refusal was not an error: %v", r)
			}
		})
	}
	cli("sampling-both-iteration-limits-tool-loops", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("tools/call", func(map[string]any) (any, *rpcError) {
			if isModern(rev) {
				return map[string]any{"resultType": "input_required", "requestState": "s",
					"inputRequests": map[string]any{"q": map[string]any{"method": "roots/list", "params": map[string]any{}}}}, nil
			}
			return nil, nil
		})
		c := dialClient(t, p, clientOpts())
		start := time.Now()
		if _, err := c.CallTimeout(ctxT(t), 300*time.Millisecond, "loop", map[string]any{}); err == nil {
			t.Fatal("an endless exchange ended in success")
		}
		if time.Since(start) > 3*time.Second {
			t.Errorf("took %s", time.Since(start))
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation
	for _, id := range []string{"elicitation-client-declare-capability-at-initialize", "elicitation-client-declare-capability-per-request-meta",
		"elicitation-client-support-at-least-one-mode", "elicitation-empty-capability-means-form-only"} {
		cli(id, func(t *testing.T, rev string) {
			c := declared(t, rev, clientOpts())
			e, ok := c["elicitation"]
			if !ok {
				t.Fatalf("%v", c)
			}
			if asMap(e)["url"] != nil {
				t.Errorf("url mode claimed: %v", e)
			}
		})
	}
	formParams := `{"message":"which repo?","requestedSchema":{"type":"object","properties":{"repo":{"type":"string"}}}}`
	for _, id := range []string{"elicitation-client-missing-mode-is-form", "elicitation-client-elicitationid-opaque"} {
		cli(id, func(t *testing.T, rev string) {
			ps := formParams
			if id == "elicitation-client-elicitationid-opaque" {
				ps = `{"mode":"url","message":"m","url":"https://a.example/x","elicitationId":"Opaque/ID==?"}`
			}
			got, _ := passThrough(t, rev, "elicitation/create", ps, map[string]any{"action": "decline"})
			var a, b any
			_ = json.Unmarshal(got, &a)
			_ = json.Unmarshal([]byte(ps), &b)
			if fmt.Sprint(a) != fmt.Sprint(b) {
				t.Errorf("%s", got)
			}
		})
	}
	for _, id := range []string{"elicitation-client-no-url-prefetch", "elicitation-client-no-open-without-consent",
		"elicitation-client-show-full-url", "elicitation-client-no-clickable-except-url-field"} {
		cli(id, func(t *testing.T, rev string) {
			var hits atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
			defer ts.Close()
			ps := fmt.Sprintf(`{"mode":"url","message":"m","url":"%s/connect?very=long&state=abc","elicitationId":"e1"}`, ts.URL)
			got, _ := passThrough(t, rev, "elicitation/create", ps, map[string]any{"action": "decline"})
			if !strings.Contains(string(got), ts.URL+`/connect?very=long\u0026state=abc`) && !strings.Contains(string(got), ts.URL+"/connect?very=long&state=abc") {
				t.Errorf("the URL did not reach whoever answers, whole: %s", got)
			}
			time.Sleep(50 * time.Millisecond)
			if hits.Load() != 0 {
				t.Error("the URL was fetched")
			}
		})
	}
	cli("elicitation-no-32042-in-2026", func(t *testing.T, rev string) {
		o := clientOpts()
		o.OnServerRequest = func(context.Context, string, json.RawMessage) (any, error) { return nil, errors.New("no") }
		p := newPeer(t, rev)
		n := 0
		var got any
		p.on("tools/call", func(pm map[string]any) (any, *rpcError) {
			if n++; n == 1 {
				return map[string]any{"resultType": "input_required", "requestState": "s",
					"inputRequests": map[string]any{"q": map[string]any{"method": "elicitation/create", "params": json.RawMessage(formParams)}}}, nil
			}
			got = pm
			return map[string]any{"resultType": "complete", "content": []any{}}, nil
		})
		c := dialClient(t, p, o)
		_, err := c.CallTool(ctxT(t), "echo", map[string]any{})
		if strings.Contains(fmt.Sprint(err, got), "32042") {
			t.Errorf("%v %v", err, got)
		}
	})
	// Which server is asking reaches the person: mcpx relays an upstream's
	// question to its own host with the server named in the message.
	for _, id := range []string{"elicitation-app-show-requesting-server", "elicitation-client-show-requesting-server",
		"elicitation-client-clear-what-and-why"} {
		cli(id, func(t *testing.T, rev string) {
			var msg string
			if isModern(rev) {
				srv, _ := newServer(t)
				srv.Ask = newAsker("done", elicitQ("e"))
				ss := stdioServer(t, srv)
				r := resultOf(t, ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks())))
				msg, _ = asMap(asMap(asMap(r["inputRequests"])["e"])["params"])["message"].(string)
			} else {
				msg, _ = asMap(relayed(t, rev, map[string]any{"elicitation": map[string]any{}}, elicitQ("e"))["params"])["message"].(string)
			}
			if !strings.Contains(msg, "up") || !strings.Contains(msg, "which repo?") {
				t.Errorf("the message does not say who asks, and what: %q", msg)
			}
		})
	}
	for _, id := range []string{"elicitation-client-ignore-unknown-complete", "elicitation-client-may-auto-retry-on-complete"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			c := dialClient(t, p, clientOpts())
			var mu sync.Mutex
			var ids []string
			c.Subscribe(mcpclient.Notifications{OnElicitationComplete: func(id string) { mu.Lock(); ids = append(ids, id); mu.Unlock() }})
			p.notify("notifications/elicitation/complete", map[string]any{"elicitationId": "nobody-asked"})
			time.Sleep(100 * time.Millisecond)
			if _, err := c.ListTools(ctxT(t)); err != nil {
				t.Error(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(ids) != 1 {
				t.Errorf("completion not dispatched: %v", ids)
			}
		})
	}
}
