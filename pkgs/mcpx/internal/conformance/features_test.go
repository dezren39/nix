package conformance_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// capsFor is what mcpx declares to a client of rev.
func capsFor(t *testing.T, ss *stdioSrv, rev string) map[string]any {
	t.Helper()
	if isModern(rev) {
		return asMap(resultOf(t, ss.request(t, rev, "server/discover", nil))["capabilities"])
	}
	return asMap(resultOf(t, ss.initialize(t, rev))["capabilities"])
}

// linking is an extra tool whose result carries a resource link.
func linking() mcpserver.Extra {
	return mcpserver.Extra{Tool: mcpserver.Tool{Name: "linker", Description: "l", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Call: func(context.Context, json.RawMessage) (string, error) {
			return mcpserver.EncodeResult("see the link", []map[string]any{
				{"type": "resource_link", "uri": "mem://alpha/unlisted", "name": "unlisted", "mimeType": "text/plain"}}), nil
		}}
}

// Tools, resources, prompts: mcpx as a server.
func TestFeaturesServer(t *testing.T) {
	srvSide := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "server", id, fn) }

	// ---- capabilities and lists ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#capabilities
	for _, id := range []string{"tools-declare-capability", "resources-declare-capability", "prompts-declare-capability",
		"prompts-declare-capability-discover", "resources-cap-features-independent", "tools-list-must-respond-when-declared",
		"resources-list-must-respond-when-declared", "prompts-list-must-respond-when-declared", "tools-list-may-be-empty-change",
		"tools-embedded-implies-resources-cap"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			caps := capsFor(t, ss, rev)
			for _, k := range []string{"tools", "resources", "prompts"} {
				if _, ok := caps[k]; !ok {
					t.Errorf("%s not declared: %v", k, caps)
				}
			}
			r := asMap(caps["resources"])
			if _, ok := r["subscribe"]; !ok {
				t.Errorf("resources.subscribe not stated: %v", r)
			}
			for _, m := range []string{"tools/list", "resources/list", "prompts/list"} {
				if res := ss.request(t, rev, m, nil); errorCode(res) != 0 {
					t.Errorf("%s: %v", m, res)
				}
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/server/tools#listing-tools
	for _, id := range []string{"tools-list-not-per-connection", "resources-list-not-per-connection",
		"prompts-list-not-per-connection", "tools-list-deterministic-order"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			srv = srv.WithExtras([]mcpserver.Extra{linking()})
			var got []string
			for i := 0; i < 2; i++ {
				ss := stdioServer(t, srv)
				var out []any
				for _, m := range []string{"tools/list", "resources/list", "prompts/list"} {
					r := resultOf(t, ss.request(t, rev, m, nil))
					delete(r, "_meta")
					out = append(out, r)
				}
				b, _ := json.Marshal(out)
				got = append(got, string(b))
			}
			if got[0] != got[1] {
				t.Error("two connections were given different lists")
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#list-changed-notification
	listChanged := func(t *testing.T, rev, kind string) {
		srv, _ := newServer(t)
		n := newNotifier()
		srv.Notify = n
		ss := stdioServer(t, srv)
		declared := true
		if isModern(rev) {
			ss.send(t, frame(5, "subscriptions/listen", params(rev, map[string]any{
				"notifications": map[string]any{kind + "ListChanged": true}})))
			ack := decode(t, ss.next(t))
			_, declared = asMap(asMap(ack["params"])["notifications"])[kind+"ListChanged"]
		} else {
			declared = asMap(capsFor(t, ss, rev)[kind])["listChanged"] == true
		}
		time.Sleep(50 * time.Millisecond)
		n.ch <- [2]any{"notifications/" + kind + "/list_changed", map[string]any{}}
		if kind == "tools" {
			// mcpx's tools are a fixed set: an upstream's change does not
			// change them, so mcpx neither promises nor sends this.
			if declared {
				t.Errorf("tools listChanged promised")
			}
			if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
				t.Errorf("sent %s", f)
			}
			return
		}
		if !declared {
			t.Fatalf("%s listChanged not declared though mcpx can push", kind)
		}
		b := ss.next(t)
		checkServerFrame(t, rev, b, "")
		if decode(t, b)["method"] != "notifications/"+kind+"/list_changed" {
			t.Errorf("%s", b)
		}
	}
	for id, kind := range map[string]string{"tools-list-changed-notify": "tools", "tools-list-changed-notify-to-listeners": "tools",
		"prompts-list-changed-notify": "prompts", "prompts-list-changed-notify-to-listeners": "prompts",
		"resources-list-changed-notify": "resources"} {
		srvSide(id, func(t *testing.T, rev string) { listChanged(t, rev, kind) })
	}
	srvSide("tools-list-changed-means-emit", func(t *testing.T, rev string) {
		listChanged(t, rev, "tools")
		listChanged(t, rev, "prompts")
	})

	// ---- tool definitions ----

	toolsOf := func(t *testing.T, rev string) []map[string]any {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		var out []map[string]any
		for _, x := range resultOf(t, ss.request(t, rev, "tools/list", nil))["tools"].([]any) {
			out = append(out, asMap(x))
		}
		return out
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#tool
	for _, id := range []string{"tools-inputschema-type-object-root", "tools-inputschema-valid-not-null",
		"tools-inputschema-no-params-recommended"} {
		srvSide(id, func(t *testing.T, rev string) {
			for _, tl := range toolsOf(t, rev) {
				s := asMap(tl["inputSchema"])
				if s == nil || s["type"] != "object" {
					t.Errorf("%v: inputSchema %v", tl["name"], tl["inputSchema"])
				}
				if len(asMap(s["properties"])) == 0 && s["additionalProperties"] != false {
					t.Errorf("%v takes nothing but does not say so", tl["name"])
				}
			}
		})
	}
	nameRe := regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	for _, id := range []string{"tools-name-length", "tools-name-charset", "tools-name-no-special", "tools-name-unique-in-server"} {
		srvSide(id, func(t *testing.T, rev string) {
			seen := map[string]bool{}
			for _, tl := range toolsOf(t, rev) {
				n := fmt.Sprint(tl["name"])
				if !nameRe.MatchString(n) {
					t.Errorf("tool name %q", n)
				}
				if seen[n] {
					t.Errorf("%q twice", n)
				}
				seen[n] = true
			}
		})
	}
	for _, id := range []string{"tools-output-schema-conform", "tools-output-schema-object-root", "tools-structured-also-text",
		"tools-structured-any-json", "tools-structured-object-only"} {
		srvSide(id, func(t *testing.T, rev string) {
			// mcpx promises no output shape, so it has none to keep.
			for _, tl := range toolsOf(t, rev) {
				if _, ok := tl["outputSchema"]; ok {
					t.Errorf("%v declares an outputSchema", tl["name"])
				}
			}
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			r := resultOf(t, ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}}))
			if _, ok := r["structuredContent"]; ok {
				t.Errorf("%v", r)
			}
		})
	}

	// ---- tools/call ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#error-handling
	for _, id := range []string{"tools-error-unknown-tool-protocol", "tools-result-lookup-errors-protocol", "tools-name-case-sensitive"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			for _, n := range []string{"no_such_tool", "MCPX_STATUS"} {
				if r := ss.request(t, rev, "tools/call", map[string]any{"name": n, "arguments": map[string]any{}}); errorCode(r) != -32602 {
					t.Errorf("%s: %v", n, r)
				}
			}
			if r := ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}}); errorCode(r) != 0 {
				t.Errorf("%v", r)
			}
		})
	}
	for _, id := range []string{"tools-result-iserror-for-tool-errors", "tools-error-input-validation-execution"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			r := resultOf(t, ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_call", "arguments": map[string]any{}}))
			if r["isError"] != true || len(r["content"].([]any)) == 0 {
				t.Errorf("a failed tool must be an isError result with its reason: %v", r)
			}
		})
	}
	srvSide("tools-error-invalid-args-protocol", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		if r := ss.requestInvalid(t, rev, "tools/call", map[string]any{"name": 7}); errorCode(r) != -32602 {
			t.Errorf("%v", r)
		}
	})
	for _, id := range []string{"tools-embedded-resource-may", "tools-resource-link-may", "tools-resource-link-may-not-listed"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			srv = srv.WithExtras([]mcpserver.Extra{linking()})
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			r := resultOf(t, ss.request(t, rev, "tools/call", map[string]any{"name": "linker", "arguments": map[string]any{}}))
			want := "resource_link"
			if rev < rev20250618 {
				want = "resource" // no links before 2025-06-18: embedded instead
			}
			found := false
			for _, c := range r["content"].([]any) {
				if asMap(c)["type"] == want {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s block: %v", want, r)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/server/tools#calling-tools
	for _, id := range []string{"tools-call-input-required-may", "tools-call-inputrequests-declared-only",
		"resources-read-input-required-may", "prompts-get-input-required-may"} {
		srvSide(id, func(t *testing.T, rev string) {
			for _, c := range []struct {
				method string
				p      map[string]any
			}{{"tools/call", callThatAsks()}, {"resources/read", map[string]any{"uri": "mem://alpha/one"}},
				{"prompts/get", map[string]any{"name": "greet", "arguments": map[string]any{"who": "x"}}}} {
				srv, _ := newServer(t)
				srv.Ask = newAsker("done", elicitQ("e1"), sampleQ("s1"))
				ss := stdioServer(t, srv)
				r := resultOf(t, ss.request(t, rev, c.method, paramsWith(rev, `{"elicitation":{}}`, c.p)))
				if r["resultType"] != "input_required" {
					t.Errorf("%s: %v", c.method, r)
					continue
				}
				if ir := asMap(r["inputRequests"]); len(ir) != 1 || ir["s1"] != nil {
					t.Errorf("%s asked what the client did not declare: %v", c.method, ir)
				}
			}
		})
	}

	// ---- resources ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#error-handling
	missingCode := func(t *testing.T, rev string) (int, map[string]any) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		r := ss.request(t, rev, "resources/read", map[string]any{"uri": "mem://alpha/missing"})
		return errorCode(r), r
	}
	for _, id := range []string{"resources-not-found-32002", "resources-not-found-32602", "resources-no-empty-contents-for-missing"} {
		srvSide(id, func(t *testing.T, rev string) {
			code, r := missingCode(t, rev)
			want := -32002
			if isModern(rev) {
				want = -32602
			}
			if code != want || r["result"] != nil {
				t.Errorf("want %d: %v", want, r)
			}
			if asMap(asMap(r["error"])["data"])["uri"] != "mem://alpha/missing" {
				t.Errorf("no uri in data: %v", r)
			}
		})
	}
	srvSide("resources-internal-32603", func(t *testing.T, rev string) {
		srv := mcpserver.New(failingReads{newBackend()}, "mcpx", "test")
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		if r := ss.request(t, rev, "resources/read", map[string]any{"uri": "mem://alpha/one"}); errorCode(r) != -32603 {
			t.Errorf("%v", r)
		}
	})
	for _, id := range []string{"resources-read-multiple-may", "resources-file-xdg-mime-may", "resources-https-only-if-client-fetchable",
		"resources-https-else-other-scheme", "resources-annotations-defs"} {
		srvSide(id, func(t *testing.T, rev string) {
			// mcpx passes a backend's URIs and types through as given: it
			// mints no https:// URI and invents no annotation.
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			backend := 0
			for _, x := range resultOf(t, ss.request(t, rev, "resources/list", nil))["resources"].([]any) {
				r := asMap(x)
				if u, _ := r["uri"].(string); strings.HasPrefix(u, "skill://mcpx/") {
					continue // mcpx's own skills, not the backend's
				}
				backend++
				if r["uri"] != "mem://alpha/one" || r["mimeType"] != "text/plain" {
					t.Errorf("%v", r)
				}
			}
			if backend == 0 {
				t.Fatal("no backend resource listed")
			}
			c := resultOf(t, ss.request(t, rev, "resources/read", map[string]any{"uri": "mem://alpha/one"}))["contents"].([]any)
			if len(c) < 1 || asMap(c[0])["mimeType"] != "text/plain" {
				t.Errorf("%v", c)
			}
		})
	}
	srvSide("resources-sec-binary-encoded", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		c := resultOf(t, ss.request(t, rev, "resources/read", map[string]any{"uri": "mem://alpha/bin"}))["contents"].([]any)
		if len(c) != 1 || asMap(c[0])["blob"] != "iVBORw0KGgo=" || asMap(c[0])["text"] != nil {
			t.Errorf("binary contents not a base64 blob: %v", c)
		}
	})
	srvSide("resources-sec-validate-uris", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		for _, u := range []string{"", "not a uri", "../../etc/passwd", "mem://alpha/../../x"} {
			if r := ss.requestInvalid(t, rev, "resources/read", map[string]any{"uri": u}); r["result"] != nil {
				t.Errorf("%q was read: %v", u, r)
			}
		}
	})

	// ---- prompts ----

	// https://modelcontextprotocol.io/specification/2025-11-25/server/prompts
	for _, id := range []string{"prompts-image-base64-mime", "prompts-audio-base64-mime", "prompts-embedded-must-include",
		"prompts-resource-link-may"} {
		srvSide(id, func(t *testing.T, rev string) {
			// mcpx renders a prompt as text, so it sends no binary or
			// embedded content to get wrong.
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			r := resultOf(t, ss.request(t, rev, "prompts/get", map[string]any{"name": "greet", "arguments": map[string]any{"who": "x"}}))
			for _, m := range r["messages"].([]any) {
				if ty := asMap(asMap(m)["content"])["type"]; ty != "text" {
					t.Errorf("content %v", ty)
				}
			}
		})
	}
	srvSide("prompts-errors-32602-32603", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		if r := ss.request(t, rev, "prompts/get", map[string]any{"name": "no-such"}); errorCode(r) != -32602 {
			t.Errorf("unknown prompt: %v", r)
		}
		srv2 := mcpserver.New(failingReads{newBackend()}, "mcpx", "test")
		ss2 := stdioServer(t, srv2)
		ss2.initialize(t, rev)
		if r := ss2.request(t, rev, "prompts/get", map[string]any{"name": "greet", "arguments": map[string]any{"who": "x"}}); errorCode(r) != -32603 {
			t.Errorf("internal failure: %v", r)
		}
	})
	for _, id := range []string{"prompts-validate-args", "prompts-validate-io"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			if r := ss.request(t, rev, "prompts/get", map[string]any{"name": "greet", "arguments": map[string]any{}}); errorCode(r) != -32602 {
				t.Errorf("a required argument was not checked: %v", r)
			}
		})
	}
	srvSide("prompts-respect-capability-negotiation", func(t *testing.T, rev string) {
		// Nothing is sent a client that did not declare it: a call that
		// raises a question is not asked of a client that declared nothing.
		srv, _ := newServer(t)
		srv.Ask = newAsker("done", elicitQ("q"))
		srv.Timing = mcpserver.Timing{AskTimeout: 150 * time.Millisecond, AskPoll: 20 * time.Millisecond}
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		r := ss.request(t, rev, "prompts/get", map[string]any{"name": "greet", "arguments": map[string]any{"who": "x"}})
		if resultOf(t, r)["resultType"] == "input_required" {
			t.Errorf("%v", r)
		}
	})
}

// failingReads is a backend whose reads fail for a reason that is not
// "not found".
type failingReads struct{ *backend }

func (failingReads) ReadResource(context.Context, string) ([]mcpserver.ResourceContents, error) {
	return nil, errors.New("disk on fire")
}
func (failingReads) GetPrompt(context.Context, string, map[string]string) (string, error) {
	return "", errors.New("disk on fire")
}

// Tools, resources, prompts: mcpx as a client.
func TestFeaturesClient(t *testing.T) {
	cli := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "client", id, fn) }

	// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#error-handling
	for _, id := range []string{"tools-client-exec-errors-to-llm", "tools-client-protocol-errors-to-llm-may",
		"tools-name-case-sensitive", "tools-resource-link-may-not-listed"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			var names []string
			p.on("tools/call", func(pm map[string]any) (any, *rpcError) {
				n := fmt.Sprint(pm["name"])
				names = append(names, n)
				switch n {
				case "Fails":
					return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "quota exceeded"}}}, nil
				case "gone":
					return nil, &rpcError{Code: -32602, Message: "no tool named gone"}
				}
				return map[string]any{"content": []any{map[string]any{"type": "resource_link", "uri": "file:///never-listed", "name": "x"}}}, nil
			})
			c := dialClient(t, p, clientOpts())
			raw, err := c.CallTool(ctxT(t), "Fails", map[string]any{})
			if err != nil || !strings.Contains(string(raw), `"isError":true`) || !strings.Contains(string(raw), "quota exceeded") {
				t.Errorf("a tool failure was not kept for the model: %s %v", raw, err)
			}
			if _, err := c.CallTool(ctxT(t), "gone", map[string]any{}); err == nil || !strings.Contains(err.Error(), "-32602") {
				t.Errorf("a protocol error lost its code: %v", err)
			}
			if raw, err := c.CallTool(ctxT(t), "link", map[string]any{}); err != nil || !strings.Contains(string(raw), "never-listed") {
				t.Errorf("%s %v", raw, err)
			}
			if names[0] != "Fails" {
				t.Errorf("the name was changed: %v", names)
			}
		})
	}
	cli("tools-x-mcp-header-stdio-may-ignore", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("tools/list", func(map[string]any) (any, *rpcError) {
			return map[string]any{"resultType": "complete", "tools": []any{map[string]any{"name": "h", "inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{"n": map[string]any{"type": "number", "x-mcp-header": "bad header"}}}}}}, nil
		})
		c := dialClient(t, p, clientOpts())
		if tools, err := c.ListTools(ctxT(t)); err != nil || len(tools) != 1 {
			t.Errorf("%v %v", tools, err)
		}
	})
	cli("resources-client-accept-32002", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("resources/read", func(map[string]any) (any, *rpcError) {
			return nil, &rpcError{Code: -32002, Message: "Resource not found", Data: map[string]any{"uri": "file:///x"}}
		})
		c := dialClient(t, p, clientOpts())
		if _, err := c.ReadResource(ctxT(t), "file:///x"); err == nil || !strings.Contains(err.Error(), "-32002") {
			t.Errorf("%v", err)
		}
	})
	for _, id := range []string{"resources-subscribe-rpc", "resources-unsubscribe-follows-subscribe"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			p.caps["resources"] = map[string]any{"subscribe": true}
			for _, m := range []string{"resources/subscribe", "resources/unsubscribe"} {
				p.on(m, func(map[string]any) (any, *rpcError) { return map[string]any{}, nil })
			}
			c := dialClient(t, p, clientOpts())
			if err := c.SubscribeResource(ctxT(t), "file:///a"); err != nil {
				t.Fatal(err)
			}
			if err := c.UnsubscribeResource(ctxT(t), "file:///a"); err != nil {
				t.Fatal(err)
			}
			s, u := p.sentMethod("resources/subscribe"), p.sentMethod("resources/unsubscribe")
			if len(s) != 1 || len(u) != 1 || asMap(u[0]["params"])["uri"] != "file:///a" {
				t.Errorf("%v %v", s, u)
			}
		})
	}
	cli("resources-updated-may-be-subresource", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		var mu sync.Mutex
		var got []string
		c.Subscribe(mcpclient.Notifications{OnResourceUpdated: func(u string) { mu.Lock(); got = append(got, u); mu.Unlock() }})
		p.notify("notifications/resources/updated", map[string]any{"uri": "file:///dir/sub/file.txt"})
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if len(got) != 1 || got[0] != "file:///dir/sub/file.txt" {
			t.Errorf("%v", got)
		}
	})
	for _, id := range []string{"prompts-client-paginate", "prompts-respect-capability-negotiation"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			n := 0
			p.on("prompts/list", func(pm map[string]any) (any, *rpcError) {
				n++
				r := map[string]any{"prompts": []any{map[string]any{"name": fmt.Sprintf("p%d", n)}}}
				if n == 1 {
					r["nextCursor"] = "c2"
				}
				return r, nil
			})
			c := dialClient(t, p, clientOpts())
			if ps, err := c.ListPrompts(ctxT(t)); err != nil || len(ps) != 2 {
				t.Errorf("%v %v", ps, err)
			}
			q := newPeer(t, rev)
			delete(q.caps, "prompts")
			// The pool asks for prompts only where Supports says the server
			// declared them (pool.go); this is that gate.
			if c2 := dialClient(t, q, clientOpts()); c2.Supports("prompts") || !c.Supports("prompts") {
				t.Error("the capability gate does not follow the declaration")
			}
		})
	}
}
