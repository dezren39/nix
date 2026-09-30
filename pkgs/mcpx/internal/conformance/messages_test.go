package conformance_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// Messages, errors, _meta, JSON Schema: mcpx as a server.
func TestMessagesServer(t *testing.T) {
	srvSide := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "server", id, fn) }

	// A representative exchange over stdio: requests with integer and
	// string ids, one that fails, one that cannot be read. Every frame
	// the harness reads is validated strictly against rev.
	type exchange struct {
		raw   map[any][]byte
		parse map[string]any
	}
	exchangeFor := func(t *testing.T, rev string) exchange {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		ex := exchange{raw: map[any][]byte{}}
		for _, id := range []any{41, "str-id"} {
			ss.send(t, frame(id, "tools/list", params(rev, nil)))
		}
		ss.send(t, frame(43, "no/such/method", params(rev, nil)))
		for len(ex.raw) < 3 {
			b := ss.next(t)
			m := decode(t, b)
			method := "tools/list"
			if m["id"] == float64(43) {
				method = ""
			}
			checkServerFrame(t, rev, b, method)
			ex.raw[m["id"]] = b
		}
		ss.send(t, []byte(`{"jsonrpc":"2.0","id":`))
		// Not schema-checked: through 2025-06-18 the schema requires an id
		// of type string|integer, and JSON-RPC requires null when the
		// request's could not be read. No frame satisfies both
		// (docs/spec/revision-conflicts.md, C4); mcpx omits it.
		ex.parse = decode(t, ss.next(t))
		return ex
	}
	checkResponses := func(t *testing.T, rev string, parse bool) {
		ex := exchangeFor(t, rev)
		for _, id := range []any{float64(41), "str-id", float64(43)} {
			b, ok := ex.raw[id]
			if !ok {
				t.Fatalf("no response with id %v; got %v", id, ex.raw)
			}
			m := decode(t, b)
			_, hasR := m["result"]
			_, hasE := m["error"]
			if hasR == hasE {
				t.Errorf("id %v: result and error both or neither: %s", id, b)
			}
			if hasR {
				if _, obj := m["result"].(map[string]any); !obj {
					t.Errorf("id %v: result is not an object: %s", id, b)
				}
			}
			if hasE {
				e := m["error"].(map[string]any)
				if c, ok := e["code"].(float64); !ok || c != float64(int(c)) {
					t.Errorf("error code not an integer: %s", b)
				}
				if msg, _ := e["message"].(string); msg == "" {
					t.Errorf("error without a message: %s", b)
				}
			}
			if !utf8.Valid(b) {
				t.Errorf("not UTF-8: %q", b)
			}
		}
		if !parse {
			return
		}
		if errorCode(ex.parse) != -32700 {
			t.Fatalf("parse error: %v", ex.parse)
		}
		if id, present := ex.parse["id"]; present && id != nil {
			t.Errorf("%s: an unreadable request's error invents an id: %v", rev, id)
		}
	}
	// https://modelcontextprotocol.io/specification/2025-06-18/basic#responses
	for _, id := range []string{"messages-response-same-id", "messages-error-response-same-id-unless-unreadable",
		"messages-all-messages-follow-jsonrpc-2"} {
		srvSide(id, func(t *testing.T, rev string) { checkResponses(t, rev, true) })
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic#responses
	for _, id := range []string{"messages-response-result-xor-error", "messages-result-response-same-id",
		"messages-result-response-has-result", "messages-result-any-object", "errors-error-has-code-and-message",
		"errors-codes-integers", "transport-jsonrpc-utf8", "transport-binding-message-directions",
		"messages-other-components-may"} {
		srvSide(id, func(t *testing.T, rev string) { checkResponses(t, rev, false) })
	}

	// Requests mcpx itself sends (questions to a legacy client).
	// https://modelcontextprotocol.io/specification/2025-11-25/basic#requests
	serverRequests := func(t *testing.T, rev string) []map[string]any {
		srv, _ := newServer(t)
		q1, q2, caps := elicitQ("a"), elicitQ("b"), map[string]any{"elicitation": map[string]any{}}
		if rev < rev20250618 {
			q1, q2, caps = sampleQ("a"), sampleQ("b"), map[string]any{"sampling": map[string]any{}}
		}
		srv.Ask = newAsker("done", q1, q2)
		ss := stdioServer(t, srv)
		if isModern(rev) {
			b, _ := json.Marshal(caps)
			r := ss.request(t, rev, "tools/call", paramsWith(rev, string(b), callThatAsks()))
			if resultOf(t, r)["resultType"] != "input_required" {
				t.Fatalf("modern client should be handed the question: %v", r)
			}
			if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
				t.Fatalf("a request was sent to a modern client: %s", f)
			}
			return nil
		}
		ss.request(t, rev, "initialize", initParams(rev, caps))
		ss.send(t, frame(nil, "notifications/initialized", nil))
		ss.send(t, frame(9, "tools/call", callThatAsks()))
		var reqs []map[string]any
		for {
			m := decode(t, ss.next(t))
			if m["method"] != nil && m["id"] != nil {
				reqs = append(reqs, m)
				answer := map[string]any{"action": "accept", "content": map[string]any{"repo": "x"}}
				if m["method"] == "sampling/createMessage" {
					answer = map[string]any{"role": "assistant", "model": "m",
						"content": map[string]any{"type": "text", "text": "ok"}}
				}
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": answer})
				ss.send(t, b)
				continue
			}
			if m["id"] == float64(9) {
				return reqs
			}
		}
	}
	for _, id := range []string{"messages-request-id-string-or-integer", "messages-request-id-not-null",
		"messages-request-id-unique-per-session", "messages-servers-must-not-initiate-requests"} {
		srvSide(id, func(t *testing.T, rev string) {
			reqs := serverRequests(t, rev)
			if !isModern(rev) && len(reqs) < 2 {
				t.Fatalf("expected two questions, got %v", reqs)
			}
			seen := map[string]bool{}
			for _, r := range reqs {
				switch r["id"].(type) {
				case string, float64:
				default:
					t.Errorf("id %v (%T)", r["id"], r["id"])
				}
				k, _ := json.Marshal(r["id"])
				if seen[string(k)] {
					t.Errorf("id %s reused", k)
				}
				seen[string(k)] = true
			}
		})
	}

	// Notifications: mcpx sends them without an id and answers none.
	// https://modelcontextprotocol.io/specification/2025-11-25/basic#notifications
	srvSide("messages-notification-no-response", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		for _, m := range []string{"notifications/cancelled", "notifications/roots/list_changed", "notifications/x-unknown"} {
			ss.send(t, frame(nil, m, params(rev, map[string]any{"requestId": 999})))
		}
		if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
			t.Errorf("a notification was answered: %s", f)
		}
	})
	pushed := func(t *testing.T, rev string) map[string]any {
		srv, _ := newServer(t)
		n := newNotifier()
		srv.Notify = n
		ss := stdioServer(t, srv)
		if isModern(rev) {
			ss.send(t, frame(5, "subscriptions/listen", params(rev, map[string]any{
				"notifications": map[string]any{"promptsListChanged": true}})))
			checkServerFrame(t, rev, ss.next(t), "") // the acknowledgement
		} else {
			ss.initialize(t, rev)
		}
		time.Sleep(50 * time.Millisecond) // the listener is registered asynchronously
		n.ch <- [2]any{"notifications/prompts/list_changed", map[string]any{}}
		b := ss.next(t)
		checkServerFrame(t, rev, b, "")
		return decode(t, b)
	}
	srvSide("messages-notification-no-id", func(t *testing.T, rev string) {
		m := pushed(t, rev)
		if m["method"] != "notifications/prompts/list_changed" {
			t.Fatalf("%v", m)
		}
		if _, ok := m["id"]; ok {
			t.Errorf("notification with an id: %v", m)
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/utilities/subscriptions
	srvSide("meta-subscription-id-on-listen-notifications", func(t *testing.T, rev string) {
		m := pushed(t, rev)
		if asMap(asMap(m["params"])["_meta"])["io.modelcontextprotocol/subscriptionId"] != float64(5) {
			t.Errorf("untagged: %v", m)
		}
	})

	// Errors in 2026-07-28: only the codes the specification allocates.
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#error-codes
	codesFor := func(t *testing.T, rev string) map[int]map[string]any {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		out := map[int]map[string]any{}
		for _, c := range []struct {
			method string
			p      map[string]any
		}{
			{"no/such/method", nil},
			{"resources/read", map[string]any{"uri": "mem://nope"}},
			{"tools/call", map[string]any{"name": 7}},
			{"prompts/get", map[string]any{"name": "no-such-prompt"}},
		} {
			r := ss.requestInvalid(t, rev, c.method, c.p)
			if e, ok := r["error"].(map[string]any); ok {
				out[errorCode(r)] = e
			}
		}
		if isModern(rev) {
			p := params(rev, nil)
			p["_meta"].(map[string]any)[mcpserver.MetaProtocolVersion] = "2099-01-01"
			ss.send(t, frame(99, "tools/list", p))
			r := decode(t, ss.next(t))
			out[errorCode(r)] = r["error"].(map[string]any)
		}
		ss.send(t, []byte(`{nope`))
		r := decode(t, ss.next(t))
		out[errorCode(r)] = r["error"].(map[string]any)
		if rev != rev20250326 {
			// A batch where the revision has none.
			ss.send(t, append(append([]byte("["), frame(98, "tools/list", params(rev, nil))...), ']'))
			if b := decode(t, ss.next(t)); errorCode(b) != 0 {
				out[errorCode(b)] = b["error"].(map[string]any)
			}
		}
		return out
	}
	allowedModern := map[int]bool{-32700: true, -32600: true, -32601: true, -32602: true, -32603: true,
		-32020: true, -32021: true, -32022: true}
	for _, id := range []string{"errors-standard-jsonrpc-codes", "errors-legacy-subrange-no-new-codes",
		"errors-reserved-subrange-only-spec-codes", "errors-do-not-emit-32002-and-32042",
		"errors-new-codes-outside-jsonrpc-range", "errors-receivers-no-meaning-for-legacy-subrange"} {
		srvSide(id, func(t *testing.T, rev string) {
			codes := codesFor(t, rev)
			for _, want := range []int{-32700, -32601, -32602} {
				if _, ok := codes[want]; !ok {
					t.Errorf("never produced %d; got %v", want, codes)
				}
			}
			for c := range codes {
				if !allowedModern[c] {
					t.Errorf("code %d is not one 2026-07-28 allocates", c)
				}
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#error-codes
	srvSide("errors-data-may", func(t *testing.T, rev string) {
		if d, _ := codesFor(t, rev)[-32022]["data"].(map[string]any); d["supported"] == nil {
			t.Errorf("-32022 without data.supported")
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/basic#error-codes
	srvSide("errors-message-concise-single-sentence", func(t *testing.T, rev string) {
		for code, e := range codesFor(t, rev) {
			msg, _ := e["message"].(string)
			if strings.ContainsAny(msg, "\n") || strings.Count(strings.TrimSuffix(msg, "."), ". ") > 0 {
				t.Errorf("%d: %q is not one short sentence", code, msg)
			}
		}
	})

	// _meta keys mcpx writes.
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/index#meta
	metaKeys := func(t *testing.T, rev string) []string {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		var keys []string
		var walk func(v any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				for k, vv := range x {
					if k == "_meta" {
						for mk := range asMap(vv) {
							keys = append(keys, mk)
						}
					}
					walk(vv)
				}
			case []any:
				for _, vv := range x {
					walk(vv)
				}
			}
		}
		for _, m := range methodsOf(rev, "server/discover", "tools/list", "resources/list", "prompts/list", "resources/templates/list") {
			walk(ss.request(t, rev, m, nil))
		}
		walk(ss.request(t, rev, "resources/read", map[string]any{"uri": "mem://alpha/one"}))
		return keys
	}
	reservedKeys := map[string]bool{"io.modelcontextprotocol/serverInfo": true,
		"io.modelcontextprotocol/subscriptionId": true, "io.modelcontextprotocol/related-task": true}
	for _, id := range []string{"meta-reserved-for-mcp", "meta-prefix-label-format", "meta-reverse-dns-should",
		"meta-reserved-prefix-second-label", "meta-name-format", "meta-schema-may-reserve-names"} {
		srvSide(id, func(t *testing.T, rev string) {
			for _, k := range metaKeys(t, rev) {
				if !validMetaKey(k) {
					t.Errorf("_meta key %q breaks the naming rules", k)
				}
				if strings.HasPrefix(k, "io.modelcontextprotocol/") && !reservedKeys[k] {
					t.Errorf("_meta key %q uses the reserved prefix for something the specification does not define", k)
				}
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/index#meta
	for _, id := range []string{"meta-no-assumptions-on-reserved-keys", "meta-progress-token-receiver-not-obligated"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			r := ss.request(t, rev, "tools/list", map[string]any{"_meta": map[string]any{
				"com.example/trace": "abc", "io.modelcontextprotocol/not-yet-defined": 1, "progressToken": "p1"}})
			if errorCode(r) != 0 {
				t.Errorf("unknown _meta refused: %v", r)
			}
			if f, quiet := ss.quiet(100 * time.Millisecond); !quiet && strings.Contains(string(f), "progress") {
				t.Errorf("unexpected: %s", f)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#meta
	srvSide("meta-server-should-send-serverinfo", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		r := resultOf(t, ss.request(t, rev, "tools/list", nil))
		if asMap(asMap(r["_meta"])["io.modelcontextprotocol/serverInfo"])["name"] != "mcpx" {
			t.Errorf("%v", r["_meta"])
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#meta
	srvSide("meta-info-not-for-behavior-or-security", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		var out []string
		for _, name := range []string{"claude", "evil"} {
			p := params(rev, nil)
			p["_meta"].(map[string]any)["io.modelcontextprotocol/clientInfo"] = map[string]any{"name": name, "version": "1"}
			ss.send(t, frame(name, "tools/list", p))
			r := ss.response(t, rev, name)
			b, _ := json.Marshal(r["result"])
			out = append(out, string(b))
		}
		if out[0] != out[1] {
			t.Error("the answer depends on clientInfo")
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#meta
	for _, id := range []string{"meta-server-no-undeclared-client-capabilities", "meta-server-no-infer-caps-from-prior"} {
		srvSide(id, func(t *testing.T, rev string) {
			// The same stdio process first initializes declaring
			// elicitation; a modern request that declares nothing must
			// still not be handed a question.
			srv, _ := newServer(t)
			srv.Ask = newAsker("done", elicitQ("q"))
			srv.Timing = mcpserver.Timing{AskTimeout: 200 * time.Millisecond, AskPoll: 20 * time.Millisecond}
			ss := stdioServer(t, srv)
			ss.request(t, rev20251125, "initialize", initParams(rev20251125, map[string]any{"elicitation": map[string]any{}}))
			ss.send(t, frame(nil, "notifications/initialized", nil))
			ss.send(t, frame(3, "tools/call", params(rev, callThatAsks())))
			for {
				m := decode(t, ss.next(t))
				if m["method"] != nil && m["id"] != nil {
					t.Fatalf("asked a client that declared nothing: %v", m)
				}
				if m["id"] == float64(3) {
					if resultOf(t, m)["resultType"] == "input_required" {
						t.Fatalf("handed a question it cannot answer: %v", m)
					}
					return
				}
			}
		})
	}

	// resultType.
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#result-type
	for _, id := range []string{"messages-resulttype-required", "messages-resulttype-server-must-include"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			for _, m := range []string{"server/discover", "tools/list", "resources/list", "prompts/list"} {
				if r := resultOf(t, ss.request(t, rev, m, nil)); r["resultType"] != "complete" {
					t.Errorf("%s: resultType %v", m, r["resultType"])
				}
			}
			r := resultOf(t, ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}}))
			if r["resultType"] != "complete" {
				t.Errorf("tools/call: %v", r["resultType"])
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#result-type
	srvSide("messages-resulttype-extension-values", func(t *testing.T, rev string) {
		// "task" is an extension value: never sent to a client that did
		// not declare the tasks extension, even for a slow call.
		srv, b := newServer(t)
		b.blocking(t)
		srv.Timing = mcpserver.Timing{TaskAfter: 20 * time.Millisecond}
		ss := stdioServer(t, srv)
		ss.send(t, frame(1, "tools/call", params(rev, map[string]any{"name": "mcpx_call",
			"arguments": map[string]any{"namespace": "a", "tool": "b"}})))
		<-b.started
		time.Sleep(100 * time.Millisecond)
		b.block <- struct{}{} // release the call without ending its context
		if r := resultOf(t, ss.response(t, rev, 1)); r["resultType"] != "complete" {
			t.Errorf("resultType %v without the extension", r["resultType"])
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#message-patterns
	srvSide("messages-patterns-notifications-scoped-to-request", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.Notify = newNotifier()
		ss := stdioServer(t, srv)
		ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}})
		if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
			t.Errorf("a notification outside any request's scope: %s", f)
		}
	})

	// HTTP status for protocol errors.
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http
	srvSide("versioning-unsupported-version-http-400", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		p := params(rev, nil)
		p["_meta"].(map[string]any)[mcpserver.MetaProtocolVersion] = "2099-01-01"
		h := headersFor(rev, "", "tools/list", "")
		h["MCP-Protocol-Version"] = "2099-01-01"
		r := hs.post(t, frame(1, "tools/list", p), h)
		if r.Status != http.StatusBadRequest || errorCode(decode(t, r.Body)) != -32022 {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	})
	srvSide("errors-header-mismatch-http-400", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		r := hs.post(t, frame(1, "tools/list", params(rev, nil)), headersFor(rev, "", "prompts/list", ""))
		if r.Status != http.StatusBadRequest || errorCode(decode(t, r.Body)) != -32020 {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports#request-metadata
	srvSide("transport-binding-may-mirror-metadata", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		res, r := hs.request(t, rev, "", "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}})
		if res.Status != http.StatusOK || errorCode(r) != 0 {
			t.Errorf("mirrored headers not accepted: %d %s", res.Status, res.Body)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices
	srvSide("security-implementors-guidelines", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		h := headersFor(rev, "", "tools/list", "")
		h["Origin"] = "https://evil.example"
		if r := hs.post(t, frame(1, "tools/list", params(rev, nil)), h); r.Status != http.StatusForbidden {
			t.Errorf("a foreign origin was served: %d", r.Status)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-03-26/basic#batching
	srvSide("messages-batch-may-send-must-receive", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		ss.send(t, []byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`))
		var arr []map[string]any
		if err := json.Unmarshal(ss.next(t), &arr); err != nil || len(arr) != 2 {
			t.Errorf("batch not answered with an array of two: %v %v", arr, err)
		}
	})

	// JSON Schema.
	// https://modelcontextprotocol.io/specification/2025-11-25/basic#json-schema-usage
	toolSchemas := func(t *testing.T, rev string, extras ...mcpserver.Extra) []map[string]any {
		srv, _ := newServer(t)
		srv = srv.WithExtras(extras)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		var out []map[string]any
		for _, tl := range resultOf(t, ss.request(t, rev, "tools/list", nil))["tools"].([]any) {
			out = append(out, asMap(asMap(tl)["inputSchema"]))
		}
		return out
	}
	for _, id := range []string{"json-schema-default-2020-12", "json-schema-recommend-2020-12", "json-schema-schemas-valid",
		"tools-schema-default-2020-12"} {
		srvSide(id, func(t *testing.T, rev string) {
			for _, s := range toolSchemas(t, rev) {
				if d, ok := s["$schema"]; ok && d != "https://json-schema.org/draft/2020-12/schema" {
					t.Errorf("mcpx's own schema declares %v", d)
				}
				if s["type"] != "object" {
					t.Errorf("inputSchema is not an object schema: %v", s)
				}
			}
		})
	}
	srvSide("json-schema-may-declare-dialect", func(t *testing.T, rev string) {
		ex := mcpserver.Extra{Tool: mcpserver.Tool{Name: "dialect", Description: "d",
			InputSchema: json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`)}}
		found := false
		for _, s := range toolSchemas(t, rev, ex) {
			if s["$schema"] == "http://json-schema.org/draft-07/schema#" {
				found = true
			}
		}
		if !found {
			t.Error("a declared dialect was not carried")
		}
	})
	srvSide("json-schema-no-network-ref", func(t *testing.T, rev string) {
		var hits atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
		defer ts.Close()
		srv, _ := newServer(t)
		srv = srv.WithExtras([]mcpserver.Extra{{Tool: mcpserver.Tool{Name: "remote", Description: "r",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"` + ts.URL + `/s.json"}}}`)},
			Call: func(context.Context, json.RawMessage) (string, error) { return "ok", nil }}})
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		ss.request(t, rev, "tools/list", nil)
		ss.request(t, rev, "tools/call", map[string]any{"name": "remote", "arguments": map[string]any{"x": 1}})
		if hits.Load() != 0 {
			t.Errorf("fetched a remote $ref %d times", hits.Load())
		}
	})
}

// Messages, errors, _meta, JSON Schema: mcpx as a client.
func TestMessagesClient(t *testing.T) {
	cli := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "client", id, fn) }

	// What the client sends: ids, notifications, _meta.
	session := func(t *testing.T, rev string) *peer {
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		for i := 0; i < 3; i++ {
			if _, err := c.ListTools(ctxT(t)); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = c.CallTool(ctxT(t), "echo", map[string]any{"s": "héllo ✓"})
		return p
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic#requests
	for _, id := range []string{"messages-request-id-string-or-integer", "messages-request-id-not-null",
		"messages-request-id-unique-per-session", "messages-request-id-unique-among-in-flight",
		"messages-notification-no-id", "transport-jsonrpc-utf8", "transport-binding-message-directions",
		"messages-requests-client-to-server-only"} {
		cli(id, func(t *testing.T, rev string) {
			p := session(t, rev)
			seen := map[string]bool{}
			p.mu.Lock()
			raws := append([][]byte(nil), p.frames...)
			p.mu.Unlock()
			for _, b := range raws {
				if !utf8.Valid(b) {
					t.Errorf("not UTF-8: %q", b)
				}
			}
			for _, f := range p.sent() {
				id, hasID := f["id"]
				if f["method"] == nil {
					t.Errorf("the client sent something that is not a request or notification: %v", f)
					continue
				}
				if strings.HasPrefix(f["method"].(string), "notifications/") {
					if hasID {
						t.Errorf("notification with id: %v", f)
					}
					continue
				}
				switch id.(type) {
				case string, float64:
				default:
					t.Errorf("request id %v (%T)", id, id)
				}
				k, _ := json.Marshal(id)
				if seen[string(k)] {
					t.Errorf("id %s reused", k)
				}
				seen[string(k)] = true
			}
		})
	}

	// The client's answers to what a server asks.
	// https://modelcontextprotocol.io/specification/2025-11-25/basic#responses
	for _, id := range []string{"messages-response-same-id", "messages-result-response-same-id",
		"messages-result-response-has-result", "messages-response-result-xor-error",
		"messages-error-response-same-id-unless-unreadable", "errors-error-has-code-and-message", "errors-codes-integers"} {
		cli(id, func(t *testing.T, rev string) {
			if isModern(rev) {
				// 2026-07-28: the client answers a server's questions in the
				// retry, each keyed by the id the server gave it.
				p := newPeer(t, rev)
				n := 0
				var retry map[string]any
				p.on("tools/call", func(pm map[string]any) (any, *rpcError) {
					if n++; n == 1 {
						return map[string]any{"resultType": "input_required", "requestState": "s1",
							"inputRequests": map[string]any{"r1": map[string]any{"method": "roots/list", "params": map[string]any{}}}}, nil
					}
					retry = pm
					return map[string]any{"resultType": "complete", "content": []any{}}, nil
				})
				c := dialClient(t, p, clientOpts())
				if _, err := c.CallTool(ctxT(t), "echo", map[string]any{}); err != nil {
					t.Fatal(err)
				}
				r1 := asMap(asMap(retry["inputResponses"])["r1"])
				if _, ok := r1["roots"]; !ok {
					t.Errorf("the answer is not keyed to the question: %v", retry["inputResponses"])
				}
				return
			}
			p := newPeer(t, rev)
			dialClient(t, p, clientOpts())
			ok := p.ask(t, "roots/list", nil)
			if _, has := ok["result"]; !has || ok["error"] != nil {
				t.Errorf("roots/list: %v", ok)
			}
			if ok["id"] != float64(1001) {
				t.Errorf("answered id %v", ok["id"])
			}
			bad := p.ask(t, "x-vendor/unknown", nil)
			e := asMap(bad["error"])
			if bad["result"] != nil || e == nil {
				t.Fatalf("unknown method: %v", bad)
			}
			if c, isNum := e["code"].(float64); !isNum || c != -32601 {
				t.Errorf("code %v", e["code"])
			}
			if s, _ := e["message"].(string); s == "" {
				t.Error("no message")
			}
			if bad["id"] != float64(1002) {
				t.Errorf("error answered id %v", bad["id"])
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic#notifications
	for _, id := range []string{"messages-notification-no-response", "meta-progress-token-receiver-not-obligated"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			c := dialClient(t, p, clientOpts())
			before := len(p.sent())
			p.notify("notifications/tools/list_changed", map[string]any{})
			p.notify("notifications/progress", map[string]any{"progressToken": "nobody", "progress": 1})
			p.notify("notifications/x-unknown", map[string]any{})
			time.Sleep(100 * time.Millisecond)
			if after := p.sent()[before:]; len(after) != 0 {
				t.Errorf("answered a notification: %v", after)
			}
			if _, err := c.ListTools(ctxT(t)); err != nil {
				t.Error(err)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic#responses
	for _, id := range []string{"messages-result-any-object", "meta-no-assumptions-on-reserved-keys",
		"messages-resulttype-absent-means-complete", "messages-other-components-may"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			p.on("tools/list", func(map[string]any) (any, *rpcError) {
				return map[string]any{"tools": []any{map[string]any{"name": "a", "inputSchema": map[string]any{"type": "object"}}},
					"x-extra": 1, "_meta": map[string]any{"com.example/k": 1, "io.modelcontextprotocol/future": true}}, nil
			})
			c := dialClient(t, p, clientOpts())
			tools, err := c.ListTools(ctxT(t))
			if err != nil || len(tools) != 1 {
				t.Errorf("%v %v", tools, err)
			}
			if _, err := c.Request(ctxT(t), "x-vendor/method", nil); err == nil {
				t.Log("vendor method answered")
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#error-codes
	for _, id := range []string{"errors-legacy-subrange-no-new-codes", "errors-receivers-no-meaning-for-legacy-subrange",
		"errors-client-accept-32002-from-legacy"} {
		cli(id, func(t *testing.T, rev string) {
			for _, code := range []int{-32002, -32001, -32010} {
				p := newPeer(t, rev)
				p.on("resources/read", func(map[string]any) (any, *rpcError) {
					return nil, &rpcError{Code: code, Message: "no"}
				})
				c := dialClient(t, p, clientOpts())
				if _, err := c.ReadResource(ctxT(t), "file:///x"); err == nil || !strings.Contains(err.Error(), "no") {
					t.Errorf("%d: %v", code, err)
				}
				if _, err := c.ListTools(ctxT(t)); err != nil {
					t.Errorf("%d: the connection did not survive: %v", code, err)
				}
				if n := len(p.sentMethod("server/discover")) + len(p.sentMethod("initialize")); n != 1 {
					t.Errorf("%d: the code was read as a reason to renegotiate", code)
				}
			}
		})
	}

	// _meta the client writes.
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#meta
	for _, id := range []string{"meta-request-params-meta-required", "meta-request-required-fields",
		"meta-client-should-send-clientinfo"} {
		cli(id, func(t *testing.T, rev string) {
			p := session(t, rev)
			for _, f := range p.sent() {
				if f["id"] == nil {
					continue
				}
				m := asMap(asMap(f["params"])["_meta"])
				for _, k := range []string{mcpserver.MetaProtocolVersion, mcpserver.MetaClientCapabilities,
					"io.modelcontextprotocol/clientInfo"} {
					if _, ok := m[k]; !ok {
						t.Errorf("%v lacks %s", f["method"], k)
					}
				}
			}
		})
	}
	for _, id := range []string{"meta-reserved-for-mcp", "meta-prefix-label-format", "meta-reverse-dns-should",
		"meta-reserved-prefix-second-label", "meta-name-format", "meta-schema-may-reserve-names"} {
		cli(id, func(t *testing.T, rev string) {
			allowed := map[string]bool{mcpserver.MetaProtocolVersion: true, mcpserver.MetaClientCapabilities: true,
				"io.modelcontextprotocol/clientInfo": true}
			for _, f := range session(t, rev).sent() {
				for k := range asMap(asMap(f["params"])["_meta"]) {
					if !validMetaKey(k) {
						t.Errorf("%q breaks the naming rules", k)
					}
					if strings.HasPrefix(k, "io.modelcontextprotocol/") && !allowed[k] {
						t.Errorf("%q uses the reserved prefix", k)
					}
				}
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#meta
	cli("meta-info-not-for-behavior-or-security", func(t *testing.T, rev string) {
		var seqs []string
		for _, name := range []string{"a", "root-admin"} {
			p := newPeer(t, rev)
			p.on("server/discover", func(map[string]any) (any, *rpcError) {
				return map[string]any{"resultType": "complete", "supportedVersions": []string{rev}, "capabilities": p.caps,
					"_meta": map[string]any{"io.modelcontextprotocol/serverInfo": map[string]any{"name": name, "version": "1"}}}, nil
			})
			c := dialClient(t, p, clientOpts())
			_, _ = c.ListTools(ctxT(t))
			var s []string
			for _, f := range p.sent() {
				s = append(s, f["method"].(string))
			}
			seqs = append(seqs, strings.Join(s, ","))
		}
		if seqs[0] != seqs[1] {
			t.Errorf("behaviour depends on serverInfo: %v", seqs)
		}
	})

	// resultType.
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#result-type
	cli("messages-resulttype-unknown-invalid", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("tools/list", func(map[string]any) (any, *rpcError) {
			return map[string]any{"resultType": "x-vendor-thing", "tools": []any{}}, nil
		})
		c := dialClient(t, p, clientOpts())
		if _, err := c.ListTools(ctxT(t)); err == nil {
			t.Error("an unknown resultType was taken as complete")
		}
	})
	cli("messages-resulttype-extension-values", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		dialClient(t, p, clientOpts())
		caps := asMap(asMap(asMap(p.sent()[0]["params"])["_meta"])[mcpserver.MetaClientCapabilities])
		if _, ok := caps["extensions"]; ok {
			t.Errorf("declares extensions it does not handle: %v", caps)
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports#request-metadata
	cli("transport-binding-may-mirror-metadata", func(t *testing.T, rev string) {
		hp := httpPeer(t, newPeer(t, rev))
		c, err := dialHTTPClient(t, hp, clientOpts())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = c.ListTools(ctxT(t))
		for _, r := range hp.requests() {
			if r.Header.Get("MCP-Protocol-Version") != rev {
				t.Errorf("header %q on %s", r.Header.Get("MCP-Protocol-Version"), r.Body)
			}
		}
	})
	// https://modelcontextprotocol.io/specification/2025-03-26/basic#batching
	cli("messages-batch-may-send-must-receive", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		// Answer the next tools/list with a batch holding its response.
		p.on("tools/list", func(pm map[string]any) (any, *rpcError) { return nil, nil })
		done := make(chan error, 1)
		go func() { _, err := c.ListTools(ctxT(t)); done <- err }()
		time.Sleep(50 * time.Millisecond)
		id := p.sentMethod("tools/list")[0]["id"]
		b, _ := json.Marshal([]any{map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"tools": []any{}}}})
		p.push(b)
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("a batched response was dropped")
		}
	})

	// JSON Schema.
	// https://modelcontextprotocol.io/specification/2025-11-25/basic#json-schema-usage
	for _, id := range []string{"json-schema-may-declare-dialect", "tools-schema-default-2020-12"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			p.on("tools/list", func(map[string]any) (any, *rpcError) {
				return map[string]any{"tools": []any{map[string]any{"name": "d", "inputSchema": map[string]any{
					"$schema": "http://json-schema.org/draft-07/schema#", "type": "object"}}}}, nil
			})
			c := dialClient(t, p, clientOpts())
			tools, err := c.ListTools(ctxT(t))
			if err != nil || len(tools) != 1 || !strings.Contains(string(tools[0].InputSchema), "draft-07") {
				t.Errorf("%v %v", tools, err)
			}
		})
	}
	cli("json-schema-no-network-ref", func(t *testing.T, rev string) {
		var hits atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
		defer ts.Close()
		p := newPeer(t, rev)
		p.on("tools/list", func(map[string]any) (any, *rpcError) {
			return map[string]any{"tools": []any{map[string]any{"name": "r", "inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{"x": map[string]any{"$ref": ts.URL + "/s.json"}}}}}}, nil
		})
		c := dialClient(t, p, clientOpts())
		_, _ = c.ListTools(ctxT(t))
		_, _ = c.CallTool(ctxT(t), "r", map[string]any{"x": 1})
		if hits.Load() != 0 {
			t.Errorf("fetched a remote $ref")
		}
	})
	_ = mcpclient.ProtocolVersion
}
