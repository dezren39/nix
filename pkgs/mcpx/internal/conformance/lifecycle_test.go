package conformance_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// Lifecycle, versioning, discover and ping: mcpx as a server.
func TestLifecycleServer(t *testing.T) {
	srvSide := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "server", id, fn) }

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/index
	srvSide("lifecycle-implementations-support-base-and-lifecycle", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		r := ss.initialize(t, rev)
		if resultOf(t, r)["protocolVersion"] != rev {
			t.Errorf("initialize %s: %v", rev, r)
		}
		if tl := ss.request(t, rev, "tools/list", nil); errorCode(tl) != 0 {
			t.Errorf("tools/list after the handshake: %v", tl)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index
	srvSide("versioning-implementations-support-base-versioning-patterns", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		d := ss.request(t, rev, "server/discover", nil)
		if vs, _ := resultOf(t, d)["supportedVersions"].([]any); len(vs) == 0 {
			t.Errorf("discover: %v", d)
		}
		if tl := ss.request(t, rev, "tools/list", nil); errorCode(tl) != 0 {
			t.Errorf("stateless tools/list: %v", tl)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#initialization
	srvSide("lifecycle-initialization-first-interaction", func(t *testing.T, rev string) {
		// The server's part: it says nothing until the client opens.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
			t.Fatalf("the server spoke before initialize: %s", f)
		}
		ss.initialize(t, rev)
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#initialization
	srvSide("lifecycle-server-responds-caps-and-info", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		r := resultOf(t, ss.initialize(t, rev))
		if _, ok := r["capabilities"].(map[string]any); !ok {
			t.Errorf("no capabilities: %v", r)
		}
		info, _ := r["serverInfo"].(map[string]any)
		if info["name"] != "mcpx" || info["version"] != "test" {
			t.Errorf("serverInfo: %v", r["serverInfo"])
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#initialization
	srvSide("lifecycle-server-no-requests-before-initialized", func(t *testing.T, rev string) {
		// initialize, but no initialized: a call now must not provoke a
		// server request (mcpx asks only within a call, and only of a
		// client that declared it can answer).
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.request(t, rev, "initialize", initParams(rev, map[string]any{"elicitation": map[string]any{}}))
		ss.request(t, rev, "tools/list", nil)
		if f, quiet := ss.quiet(200 * time.Millisecond); !quiet {
			t.Fatalf("server sent before initialized: %s", f)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#version-negotiation
	srvSide("lifecycle-server-echoes-supported-version", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		if got := resultOf(t, ss.initialize(t, rev))["protocolVersion"]; got != rev {
			t.Errorf("asked %s, got %v", rev, got)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#version-negotiation
	for _, id := range []string{"lifecycle-server-counteroffers-version", "lifecycle-server-counteroffers-latest"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.send(t, frame(1, "initialize", initParams("1999-01-01", nil)))
			raw := ss.next(t)
			checkServerFrame(t, mcpclient.ProtocolVersion, raw, "initialize")
			got := resultOf(t, decode(t, raw))["protocolVersion"]
			if got != mcpclient.ProtocolVersion {
				t.Errorf("counter-offer %v, want the latest legacy version %s", got, mcpclient.ProtocolVersion)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#capability-negotiation
	srvSide("lifecycle-capabilities-establish-session-features", func(t *testing.T, rev string) {
		// What mcpx declares is what it answers: completions only where the
		// revision defines them, and completion/complete answered then.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		caps := resultOf(t, ss.initialize(t, rev))["capabilities"].(map[string]any)
		_, declared := caps["completions"]
		if want := rev >= rev20250326; declared != want {
			t.Errorf("%s: completions declared=%v", rev, declared)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#operation
	for _, id := range []string{"lifecycle-respect-negotiated-version-and-capabilities-should",
		"lifecycle-respect-negotiated-version-and-capabilities-must"} {
		srvSide(id, func(t *testing.T, rev string) {
			// Respecting the version: every frame validated strictly
			// against the negotiated revision (done by the harness) across
			// the listing methods, whose shapes changed between revisions.
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			for _, m := range []string{"tools/list", "resources/list", "prompts/list", "resources/templates/list"} {
				ss.request(t, rev, m, nil)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#stdio
	srvSide("lifecycle-stdio-server-may-exit", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		ss.in.Close()
		select {
		case <-ss.done:
			ss.done <- nil // for the cleanup
		case <-time.After(wait):
			t.Fatal("ServeStdio did not return at EOF")
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#http
	srvSide("lifecycle-http-shutdown-closing-connection", func(t *testing.T, rev string) {
		// A session ends with the client's DELETE (2025-03-26 on); after
		// that the session is gone.
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		sess := hs.initialize(t, rev)
		if sess == "" {
			t.Fatal("no session issued")
		}
		req, _ := http.NewRequest(http.MethodDelete, hs.ts.URL, nil)
		req.Header.Set("Mcp-Session-Id", sess)
		if r := hs.do(t, req); r.Status/100 != 2 {
			t.Fatalf("DELETE: %d", r.Status)
		}
		if r, _ := hs.request(t, rev, sess, "tools/list", nil); r.Status != http.StatusNotFound {
			t.Errorf("request on a closed session: %d", r.Status)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#error-handling
	srvSide("lifecycle-prepared-for-error-cases", func(t *testing.T, rev string) {
		// Version mismatch answered, unknown method refused, and the
		// session survives both.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		if r := ss.request(t, rev, "no/such/method", nil); errorCode(r) != -32601 {
			t.Errorf("unknown method: %v", r)
		}
		if r := ss.request(t, rev, "tools/list", nil); errorCode(r) != 0 {
			t.Errorf("after an error: %v", r)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#timeouts
	// A question mcpx puts to its client and nobody answers: the call is
	// released at the ask timeout, and the question is cancelled.
	askUnanswered := func(t *testing.T, rev string) (elicit map[string]any, rest []map[string]any, took time.Duration) {
		srv, _ := newServer(t)
		q, caps := elicitQ("q1"), map[string]any{"elicitation": map[string]any{}}
		if rev < rev20250618 { // no elicitation yet: sampling is the question these revisions have
			q, caps = sampleQ("q1"), map[string]any{"sampling": map[string]any{}}
		}
		srv.Ask = newAsker("done", q)
		srv.Timing = mcpserver.Timing{AskTimeout: 300 * time.Millisecond, AskPoll: 20 * time.Millisecond}
		ss := stdioServer(t, srv)
		ss.request(t, rev, "initialize", initParams(rev, caps))
		ss.send(t, frame(nil, "notifications/initialized", nil))
		start := time.Now()
		ss.send(t, frame(77, "tools/call", callThatAsks()))
		for time.Since(start) < wait {
			f := decode(t, ss.next(t))
			if f["method"] == "elicitation/create" || f["method"] == "sampling/createMessage" {
				elicit = f
				continue
			}
			rest = append(rest, f)
			if f["id"] == float64(77) {
				return elicit, rest, time.Since(start)
			}
		}
		t.Fatal("the call was never released")
		return
	}
	for _, id := range []string{"lifecycle-timeouts-all-requests-2024", "lifecycle-timeouts-establish", "lifecycle-timeouts-max-regardless-of-progress"} {
		srvSide(id, func(t *testing.T, rev string) {
			el, _, took := askUnanswered(t, rev)
			if el == nil {
				t.Fatal("no question was asked")
			}
			if took > 3*time.Second {
				t.Errorf("released after %s", took)
			}
		})
	}
	srvSide("lifecycle-timeouts-cancel-on-expiry", func(t *testing.T, rev string) {
		el, rest, _ := askUnanswered(t, rev)
		if el == nil {
			t.Fatal("no question was asked")
		}
		for _, f := range rest {
			if f["method"] == "notifications/cancelled" && asMap(f["params"])["requestId"] == el["id"] {
				return
			}
		}
		t.Errorf("the unanswered question %v was not cancelled; saw %v", el["id"], rest)
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#timeouts
	srvSide("lifecycle-timeouts-per-request-configurable", func(t *testing.T, rev string) {
		// mcpx_exec takes a per-call timeout; the server passes it through.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		r := ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_exec",
			"arguments": map[string]any{"source": "1", "timeout": 3}})
		if errorCode(r) != 0 {
			t.Fatalf("%v", r)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/schema#clientcapabilities
	srvSide("lifecycle-capability-sets-open", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		caps := map[string]any{"x-vendor/extra": map[string]any{"on": true}}
		if isModern(rev) {
			b, _ := json.Marshal(caps)
			r := ss.request(t, rev, "tools/list", paramsWith(rev, string(b), nil))
			if errorCode(r) != 0 {
				t.Errorf("unknown capability refused: %v", r)
			}
			return
		}
		r := ss.request(t, rev, "initialize", initParams(rev, caps))
		if errorCode(r) != 0 {
			t.Errorf("unknown capability refused: %v", r)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/schema#implementation
	srvSide("lifecycle-implementation-name-version-required", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		var info map[string]any
		if isModern(rev) {
			m, _ := resultOf(t, ss.request(t, rev, "server/discover", nil))["_meta"].(map[string]any)
			info, _ = m["io.modelcontextprotocol/serverInfo"].(map[string]any)
		} else {
			info, _ = resultOf(t, ss.initialize(t, rev))["serverInfo"].(map[string]any)
		}
		if s, _ := info["name"].(string); s == "" {
			t.Errorf("name: %v", info)
		}
		if s, _ := info["version"].(string); s == "" {
			t.Errorf("version: %v", info)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/ping#behavior-requirements
	for _, id := range []string{"ping-receiver-responds-promptly-empty", "ping-schema-receiver-must-respond"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			start := time.Now()
			r := resultOf(t, ss.request(t, rev, "ping", nil))
			if len(r) != 0 {
				t.Errorf("ping result not empty: %v", r)
			}
			if d := time.Since(start); d > time.Second {
				t.Errorf("ping took %s", d)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/ping
	for _, id := range []string{"ping-either-party-may-initiate", "ping-avoid-excessive"} {
		srvSide(id, func(t *testing.T, rev string) {
			// mcpx accepts a ping from its client and never pings back
			// unprompted: nothing arrives on an idle session.
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			ss.request(t, rev, "ping", nil)
			if f, quiet := ss.quiet(300 * time.Millisecond); !quiet {
				t.Errorf("idle session carried %s", f)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#extension-negotiation
	srvSide("meta-extension-ids-follow-meta-rules", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		caps := resultOf(t, ss.request(t, rev, "server/discover", nil))["capabilities"].(map[string]any)
		ext, _ := caps["extensions"].(map[string]any)
		for k := range ext {
			if !validMetaKey(k) || !strings.Contains(k, "/") {
				t.Errorf("extension id %q is not a prefixed _meta key", k)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#protocol-version-negotiation
	srvSide("versioning-server-unsupported-version-error", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		p := params(rev, nil)
		p["_meta"].(map[string]any)[mcpserver.MetaProtocolVersion] = "2099-01-01"
		ss.send(t, frame(9, "tools/list", p))
		r := decode(t, ss.next(t))
		if errorCode(r) != -32022 {
			t.Fatalf("%v", r)
		}
		d, _ := r["error"].(map[string]any)["data"].(map[string]any)
		if vs, _ := d["supported"].([]any); len(vs) == 0 {
			t.Errorf("no supported list: %v", r)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#protocol-version-negotiation
	srvSide("versioning-server-must-implement-discover", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		r := resultOf(t, ss.request(t, rev, "server/discover", nil))
		if vs, _ := r["supportedVersions"].([]any); len(vs) == 0 || vs[0] != rev {
			t.Errorf("supportedVersions: %v", r["supportedVersions"])
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#extension-negotiation
	srvSide("versioning-extension-fallback-or-reject", func(t *testing.T, rev string) {
		// A tool call without the task field gets the core result, not a
		// task handle.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		r := resultOf(t, ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}}))
		if _, ok := r["content"]; !ok {
			t.Errorf("core result expected: %v", r)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#compatibility-matrix
	for _, id := range []string{"versioning-dual-era-server-may", "versioning-dual-era-server-selects-by-opening",
		"versioning-dual-era-both-concurrently"} {
		srvSide(id, func(t *testing.T, rev string) {
			// One server, one HTTP endpoint: a legacy session and a
			// stateless modern request side by side, each answered in its
			// own era.
			srv, _ := newServer(t)
			hs := httpServer(t, srv)
			sess := hs.initialize(t, rev20251125)
			_, legacy := hs.request(t, rev20251125, sess, "tools/list", nil)
			_, modern := hs.request(t, rev, "", "tools/list", nil)
			if _, ok := resultOf(t, legacy)["resultType"]; ok {
				t.Errorf("legacy session got a modern result: %v", legacy)
			}
			if resultOf(t, modern)["resultType"] != "complete" {
				t.Errorf("modern request got a legacy result: %v", modern)
			}
			// And on stdio, the opening selects the era for the process.
			ss := stdioServer(t, srv)
			ss.initialize(t, rev20251125)
			if _, ok := resultOf(t, ss.request(t, rev20251125, "tools/list", nil))["resultType"]; ok {
				t.Error("stdio legacy session answered in the modern shape")
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#statelessness
	for _, id := range []string{"lifecycle-stateless-no-reliance-on-prior-requests", "lifecycle-stateless-multiple-conversations"} {
		srvSide(id, func(t *testing.T, rev string) {
			// Each request stands alone: a fresh connection with no
			// discover and no prior request is served.
			srv, _ := newServer(t)
			hs := httpServer(t, srv)
			for i := 0; i < 3; i++ {
				res, r := hs.request(t, rev, "", "tools/list", nil)
				if res.Status != http.StatusOK || errorCode(r) != 0 {
					t.Fatalf("request %d: %d %s", i, res.Status, res.Body)
				}
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#statelessness
	srvSide("lifecycle-stateless-no-connection-reuse-required", func(t *testing.T, rev string) {
		// A question handed out on one HTTP request is answered on another:
		// each POST is its own connection, and the requestState is enough.
		srv, _ := newServer(t)
		srv.Ask = newAsker("done", elicitQ("e"))
		hs := httpServer(t, srv)
		_, first := hs.request(t, rev, "", "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks()))
		r := resultOf(t, first)
		p := callThatAsks()
		p["requestState"] = r["requestState"]
		p["inputResponses"] = map[string]any{"e": map[string]any{"action": "accept", "content": map[string]any{"repo": "x"}}}
		_, second := hs.request(t, rev, "", "tools/call", paramsWith(rev, `{"elicitation":{}}`, p))
		if resultOf(t, second)["resultType"] != "complete" {
			t.Errorf("resumed on another connection: %v", second)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#statelessness
	srvSide("lifecycle-stateless-explicit-handles", func(t *testing.T, rev string) {
		// State that spans requests is an explicit handle; the modern HTTP
		// path issues no session.
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		for _, m := range []string{"server/discover", "tools/list"} {
			res, _ := hs.request(t, rev, "", m, nil)
			if s := res.Header.Get("Mcp-Session-Id"); s != "" {
				t.Errorf("%s issued a session %q", m, s)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/deprecated#deprecated
	srvSide("deprecated-should-not-adopt", func(t *testing.T, rev string) {
		// mcpx does not serve the deprecated HTTP+SSE transport: a GET
		// without a session is not an SSE endpoint handshake.
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		req, _ := http.NewRequest(http.MethodGet, hs.ts.URL, nil)
		req.Header.Set("Accept", "text/event-stream")
		if r := hs.do(t, req); r.Status == http.StatusOK && strings.Contains(string(r.Body), "event: endpoint") {
			t.Errorf("serves HTTP+SSE: %s", r.Body)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/server/discover#discoverresult
	discover := func(t *testing.T, rev string) map[string]any {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		return resultOf(t, ss.request(t, rev, "server/discover", nil))
	}
	srvSide("discover-supportedversions", func(t *testing.T, rev string) {
		r := discover(t, rev)
		if vs, _ := r["supportedVersions"].([]any); len(vs) == 0 {
			t.Errorf("%v", r)
		}
		if _, ok := r["protocolVersions"]; ok {
			t.Error("protocolVersions is not a DiscoverResult field")
		}
	})
	srvSide("discover-capabilities", func(t *testing.T, rev string) {
		if _, ok := discover(t, rev)["capabilities"].(map[string]any); !ok {
			t.Error("no capabilities")
		}
	})
	srvSide("discover-serverinfo-in-meta", func(t *testing.T, rev string) {
		r := discover(t, rev)
		m, _ := r["_meta"].(map[string]any)
		if _, ok := m["io.modelcontextprotocol/serverInfo"].(map[string]any); !ok {
			t.Errorf("_meta serverInfo: %v", r)
		}
		if _, ok := r["serverInfo"]; ok {
			t.Error("top-level serverInfo")
		}
	})
	srvSide("discover-instructions-optional", func(t *testing.T, rev string) {
		if s, ok := discover(t, rev)["instructions"]; ok {
			if _, isStr := s.(string); !isStr {
				t.Errorf("instructions: %v", s)
			}
		}
	})
	srvSide("discover-cacheable", func(t *testing.T, rev string) {
		r := discover(t, rev)
		if _, ok := r["ttlMs"].(float64); !ok {
			t.Errorf("ttlMs: %v", r)
		}
		if s, _ := r["cacheScope"].(string); s == "" {
			t.Errorf("cacheScope: %v", r)
		}
	})
	srvSide("discover-resulttype", func(t *testing.T, rev string) {
		if r := discover(t, rev); r["resultType"] != "complete" {
			t.Errorf("resultType: %v", r["resultType"])
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/schema#clientcapabilities
	srvSide("lifecycle-include-context-only-if-declared", func(t *testing.T, rev string) {
		// mcpx originates no sampling request of its own; it never sets
		// includeContext. Nothing it sends a client carries the field.
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		ss.request(t, rev, "tools/call", map[string]any{"name": "mcpx_status", "arguments": map[string]any{}})
		if f, quiet := ss.quiet(100 * time.Millisecond); !quiet && strings.Contains(string(f), "includeContext") {
			t.Errorf("sent includeContext: %s", f)
		}
	})
}

// validMetaKey is the _meta key grammar: an optional prefix of dot-separated
// labels ending in "/", then a name that starts and ends alphanumeric.
func validMetaKey(k string) bool {
	name := k
	if i := strings.LastIndex(k, "/"); i >= 0 {
		prefix := k[:i]
		name = k[i+1:]
		for _, label := range strings.Split(prefix, ".") {
			if label == "" || !alnum(label[0]) || !alnum(label[len(label)-1]) {
				return false
			}
			for _, c := range []byte(label) {
				if !alnum(c) && c != '-' {
					return false
				}
			}
		}
	}
	if name == "" {
		return true
	}
	if !alnum(name[0]) || !alnum(name[len(name)-1]) {
		return false
	}
	for _, c := range []byte(name) {
		if !alnum(c) && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}

func alnum(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }

// Lifecycle, versioning, discover and ping: mcpx as a client.
func TestLifecycleClient(t *testing.T) {
	cli := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "client", id, fn) }

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/index
	cli("lifecycle-implementations-support-base-and-lifecycle", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		if c.Negotiated != rev {
			t.Errorf("negotiated %q with a %s server", c.Negotiated, rev)
		}
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Error(err)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index
	cli("versioning-implementations-support-base-versioning-patterns", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		if c.Era != mcpclient.EraModern {
			t.Fatalf("era %s", c.Era)
		}
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Error(err)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-03-26/basic/lifecycle#initialization
	cli("lifecycle-initialize-not-in-batch", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		dialClient(t, p, clientOpts())
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, b := range p.frames {
			if strings.HasPrefix(strings.TrimSpace(string(b)), "[") {
				t.Errorf("batch sent: %s", b)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#initialization
	for _, id := range []string{"lifecycle-initialization-first-interaction", "lifecycle-client-sends-initialize",
		"lifecycle-client-sends-initialized-after-init", "lifecycle-client-no-requests-before-init-response"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			c := dialClient(t, p, clientOpts())
			if _, err := c.ListTools(ctxT(t)); err != nil {
				t.Fatal(err)
			}
			f := p.sent()
			if len(f) < 3 || f[0]["method"] != "initialize" || f[1]["method"] != "notifications/initialized" {
				t.Fatalf("opening sequence: %v", f)
			}
			ip := asMap(f[0]["params"])
			for _, k := range []string{"protocolVersion", "capabilities", "clientInfo"} {
				if _, ok := ip[k]; !ok {
					t.Errorf("initialize lacks %s: %v", k, ip)
				}
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#version-negotiation
	for _, id := range []string{"lifecycle-client-sends-supported-version", "lifecycle-client-sends-latest-version"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			dialClient(t, p, clientOpts())
			if v := asMap(p.sentMethod("initialize")[0]["params"])["protocolVersion"]; v != mcpclient.ProtocolVersion {
				t.Errorf("offered %v, want the latest legacy version %s", v, mcpclient.ProtocolVersion)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/schema#initializerequestparams
	cli("lifecycle-client-may-support-older", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		if c.Negotiated != rev {
			t.Errorf("%s server: negotiated %q", rev, c.Negotiated)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#version-negotiation
	cli("lifecycle-client-disconnects-on-unsupported-version", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("initialize", func(map[string]any) (any, *rpcError) {
			return map[string]any{"protocolVersion": "1999-01-01", "capabilities": map[string]any{},
				"serverInfo": map[string]any{"name": "x", "version": "1"}}, nil
		})
		if _, err := dialClientErr(t, p, clientOpts()); err == nil {
			t.Error("accepted a version it does not support")
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#version-negotiation
	cli("lifecycle-http-protocol-version-header", func(t *testing.T, rev string) {
		hp := httpPeer(t, newPeer(t, rev))
		c, err := dialHTTPClient(t, hp, clientOpts())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Fatal(err)
		}
		for _, r := range hp.requests()[1:] {
			if got := r.Header.Get("MCP-Protocol-Version"); got != rev {
				t.Errorf("after negotiating %s, a request carried MCP-Protocol-Version %q: %s", rev, got, r.Body)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#capability-negotiation
	for _, id := range []string{"lifecycle-capabilities-establish-session-features",
		"lifecycle-respect-negotiated-version-and-capabilities-should",
		"lifecycle-respect-negotiated-version-and-capabilities-must"} {
		cli(id, func(t *testing.T, rev string) {
			// A server that declared no completions is not asked for one.
			p := newPeer(t, rev)
			c := dialClient(t, p, clientOpts())
			_, ok, err := c.Complete(ctxT(t), json.RawMessage(`{"ref":{"type":"ref/prompt","name":"x"},"argument":{"name":"a","value":""}}`))
			if err != nil || ok {
				t.Errorf("ok=%v err=%v", ok, err)
			}
			// Except on 2024-11-05, which has no completions capability
			// to declare: there the method is asked and method-not-found
			// read as the same absence (#213, CMP-01).
			if asked := len(p.sentMethod("completion/complete")) != 0; asked != (rev == "2024-11-05") {
				t.Errorf("asked=%v on %s", asked, rev)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#stdio
	cli("lifecycle-stdio-client-shutdown-sequence", func(t *testing.T, rev string) {
		// A child that exits cleanly on EOF must see EOF, not a signal.
		dir := t.TempDir()
		mark := filepath.Join(dir, "mark")
		script := `trap 'echo term >> "$1"; exit 0' TERM; while IFS= read -r l; do :; done; echo eof >> "$1"`
		tr, err := mcpclient.NewStdio(mcpclient.StdioOptions{Command: "/bin/sh",
			Args: []string{"-c", script, "sh", mark}, InheritEnv: true})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
		_ = tr.Close()
		b, _ := os.ReadFile(mark)
		if strings.TrimSpace(string(b)) != "eof" {
			t.Errorf("the child saw %q; closing stdin should come first and be enough", b)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#http
	cli("lifecycle-http-shutdown-closing-connection", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		hp := httpPeer(t, p)
		hp.session = "sess-1"
		c, err := dialHTTPClient(t, hp, clientOpts())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
		var deleted bool
		for _, r := range hp.requests() {
			if r.Method == http.MethodDelete && r.Header.Get("Mcp-Session-Id") == "sess-1" {
				deleted = true
			}
		}
		if !deleted && rev >= rev20250326 {
			t.Error("closing the client did not end the session")
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#error-handling
	cli("lifecycle-prepared-for-error-cases", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("tools/call", func(map[string]any) (any, *rpcError) { return nil, &rpcError{Code: -32603, Message: "boom"} })
		c := dialClient(t, p, clientOpts())
		if _, err := c.CallTool(ctxT(t), "echo", map[string]any{}); err == nil {
			t.Error("error answer not reported")
		}
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Errorf("connection lost after an error answer: %v", err)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#timeouts
	for _, id := range []string{"lifecycle-timeouts-all-requests-2024", "lifecycle-timeouts-establish",
		"lifecycle-timeouts-per-request-configurable", "lifecycle-timeouts-max-regardless-of-progress"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			p.on("tools/call", func(map[string]any) (any, *rpcError) { return nil, nil }) // never answers
			c := dialClient(t, p, clientOpts())
			start := time.Now()
			_, err := c.CallTimeout(ctxT(t), 100*time.Millisecond, "echo", map[string]any{})
			if err == nil {
				t.Fatal("a call nobody answers returned no error")
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Errorf("timed out after %s", d)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#timeouts
	cli("lifecycle-timeouts-cancel-on-expiry", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("tools/call", func(map[string]any) (any, *rpcError) { return nil, nil })
		c := dialClient(t, p, clientOpts())
		_, _ = c.CallTimeout(ctxT(t), 50*time.Millisecond, "echo", map[string]any{})
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && len(p.sentMethod("notifications/cancelled")) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		if len(p.sentMethod("notifications/cancelled")) == 0 {
			t.Error("no notifications/cancelled after the timeout")
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#timeouts
	cli("lifecycle-timeouts-may-reset-on-progress", func(t *testing.T, rev string) {
		// mcpx does not reset on progress (the MAY is not taken); its
		// deadline is absolute, which the max-timeout rows hold.
		p := newPeer(t, rev)
		p.on("tools/call", func(map[string]any) (any, *rpcError) { return nil, nil })
		c := dialClient(t, p, clientOpts())
		start := time.Now()
		_, _ = c.CallTimeout(ctxT(t), 100*time.Millisecond, "echo", map[string]any{})
		if time.Since(start) > 2*time.Second {
			t.Error("deadline not absolute")
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/schema#cancellednotification
	cli("lifecycle-cancel-not-initialize", func(t *testing.T, rev string) {
		// An initialize that times out is abandoned, not cancelled.
		p := newPeer(t, rev)
		p.on("initialize", func(map[string]any) (any, *rpcError) { return nil, nil })
		ctx, cancel := contextWithTimeout(100 * time.Millisecond)
		defer cancel()
		o := clientOpts()
		o.Preference = mcpclient.ForceLegacy
		c, _ := mcpclient.NewWithOptions(ctx, p, o)
		if c != nil {
			c.Close()
		}
		for _, f := range p.sentMethod("notifications/cancelled") {
			t.Errorf("cancelled initialize: %v", f)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/schema#initializeresult
	cli("lifecycle-instructions-may-go-in-system-prompt", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("initialize", func(map[string]any) (any, *rpcError) {
			return map[string]any{"protocolVersion": rev, "capabilities": map[string]any{},
				"serverInfo": map[string]any{"name": "x", "version": "1"}, "instructions": "use page ids"}, nil
		})
		if c := dialClient(t, p, clientOpts()); c.Instructions != "use page ids" {
			t.Errorf("instructions dropped: %q", c.Instructions)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/schema#clientcapabilities
	cli("lifecycle-capability-sets-open", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.caps["x-vendor/extra"] = map[string]any{"on": true}
		if _, err := dialClientErr(t, p, clientOpts()); err != nil {
			t.Errorf("unknown server capability refused: %v", err)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/schema#implementation
	cli("lifecycle-implementation-name-version-required", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Fatal(err)
		}
		for _, f := range p.sent() {
			var info map[string]any
			if f["method"] == "initialize" {
				info = asMap(asMap(f["params"])["clientInfo"])
			} else if m := asMap(asMap(f["params"])["_meta"]); m != nil && isModern(rev) && f["id"] != nil {
				info = asMap(m["io.modelcontextprotocol/clientInfo"])
			} else {
				continue
			}
			if info["name"] == nil || info["version"] == nil {
				t.Errorf("clientInfo incomplete in %v", f)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/ping#behavior-requirements
	for _, id := range []string{"ping-receiver-responds-promptly-empty", "ping-schema-receiver-must-respond",
		"ping-either-party-may-initiate"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			dialClient(t, p, clientOpts())
			r := p.ask(t, "ping", nil)
			if res, ok := r["result"].(map[string]any); !ok || len(res) != 0 {
				t.Errorf("ping answer: %v", r)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/ping#implementation-considerations
	cli("ping-avoid-excessive", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		dialClient(t, p, clientOpts())
		time.Sleep(300 * time.Millisecond)
		if n := len(p.sentMethod("ping")); n != 0 {
			t.Errorf("idle client pinged %d times", n)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#protocol-version-negotiation
	cli("versioning-client-retry-with-supported", func(t *testing.T, rev string) {
		// No mutual version in data.supported: the error reaches the
		// caller, once, rather than a retry loop.
		p := newPeer(t, rev)
		p.on("tools/list", func(pm map[string]any) (any, *rpcError) {
			return nil, &rpcError{Code: -32022, Message: "unsupported", Data: map[string]any{"supported": []string{"2099-01-01"}}}
		})
		c := dialClient(t, p, clientOpts())
		if _, err := c.ListTools(ctxT(t)); err == nil || !strings.Contains(err.Error(), "32022") {
			t.Errorf("%v", err)
		}
		if n := len(p.sentMethod("tools/list")); n > 2 {
			t.Errorf("%d attempts", n)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#protocol-version-negotiation
	for _, id := range []string{"versioning-client-may-discover-first", "discover-client-may-call"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			dialClient(t, p, clientOpts())
			if f := p.sent(); len(f) == 0 || f[0]["method"] != "server/discover" {
				t.Errorf("first frame: %v", f)
			}
		})
	}

	// https://modelcontextprotocol.io/specification/2026-07-28/server/discover#discoverresult
	cli("discover-client-choose-from-list", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("server/discover", func(map[string]any) (any, *rpcError) {
			return map[string]any{"resultType": "complete", "supportedVersions": []string{"2099-01-01", rev},
				"capabilities": map[string]any{"tools": map[string]any{}}}, nil
		})
		c := dialClient(t, p, clientOpts())
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Fatal(err)
		}
		for _, f := range p.sentMethod("tools/list") {
			if v := asMap(asMap(f["params"])["_meta"])[mcpserver.MetaProtocolVersion]; v != rev {
				t.Errorf("used %v, not a version from the list it supports", v)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/server/discover#discoverresult
	cli("discover-serverinfo-not-for-behavior", func(t *testing.T, rev string) {
		// Two servers differing only in serverInfo get identical traffic.
		var seqs [2][]string
		for i, name := range []string{"a", "evil-admin"} {
			p := newPeer(t, rev)
			p.on("server/discover", func(map[string]any) (any, *rpcError) {
				return map[string]any{"resultType": "complete", "supportedVersions": []string{rev},
					"capabilities": p.caps, "_meta": map[string]any{
						"io.modelcontextprotocol/serverInfo": map[string]any{"name": name, "version": "1"}}}, nil
			})
			c := dialClient(t, p, clientOpts())
			_, _ = c.ListTools(ctxT(t))
			for _, f := range p.sent() {
				m, _ := f["method"].(string)
				seqs[i] = append(seqs[i], m)
			}
		}
		if strings.Join(seqs[0], ",") != strings.Join(seqs[1], ",") {
			t.Errorf("behaviour depends on serverInfo: %v vs %v", seqs[0], seqs[1])
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/server/discover#request
	cli("discover-request-meta-required", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		dialClient(t, p, clientOpts())
		d := p.sentMethod("server/discover")
		if len(d) == 0 {
			t.Fatal("no discover")
		}
		m := asMap(asMap(d[0]["params"])["_meta"])
		for _, k := range []string{mcpserver.MetaProtocolVersion, mcpserver.MetaClientCapabilities} {
			if _, ok := m[k]; !ok {
				t.Errorf("discover _meta lacks %s: %v", k, m)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions
	cli("versioning-era-cache", func(t *testing.T, rev string) {
		// A cached era is tried first, skipping the probe.
		p := newPeer(t, rev)
		o := clientOpts()
		o.Preference = mcpclient.PreferModern
		o.Cached = mcpclient.EraModern
		c, err := dialClientErr(t, p, o)
		if err != nil {
			t.Fatal(err)
		}
		if c.Source != mcpclient.SourceCache {
			t.Errorf("source %q", c.Source)
		}
		if len(p.sentMethod("initialize")) != 0 {
			t.Error("probed legacy despite the cache")
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#backward-compatibility
	cli("versioning-dual-era-client-probe-discover-first-stdio", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		o := clientOpts()
		o.Preference = mcpclient.PreferModern
		if _, err := dialClientErr(t, p, o); err != nil {
			t.Fatal(err)
		}
		if f := p.sent(); len(f) == 0 || f[0]["method"] != "server/discover" {
			t.Errorf("first frame %v", f)
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#backward-compatibility
	cli("versioning-fallback-not-keyed-to-one-code", func(t *testing.T, rev string) {
		// A legacy server answering discover with an unusual error code
		// still gets the initialize fallback.
		for _, code := range []int{-32601, -32602, -32000, -32603} {
			p := newPeer(t, rev20251125)
			p.validate = false // the probe is a 2026 frame sent to a 2025 server, by design
			p.on("server/discover", func(map[string]any) (any, *rpcError) {
				return nil, &rpcError{Code: code, Message: "nope"}
			})
			o := clientOpts()
			o.Preference = mcpclient.PreferModern
			c, err := dialClientErr(t, p, o)
			if err != nil || c.Era != mcpclient.EraLegacy {
				t.Errorf("code %d: no fallback (%v)", code, err)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/index#statelessness
	cli("lifecycle-stateless-client-process-lifetime", func(t *testing.T, rev string) {
		// One client carries many calls; nothing about a call ends it.
		p := newPeer(t, rev)
		c := dialClient(t, p, clientOpts())
		for i := 0; i < 3; i++ {
			if _, err := c.CallTool(ctxT(t), "echo", map[string]any{}); err != nil {
				t.Fatal(err)
			}
		}
		if !c.Alive() {
			t.Error("client closed after calls")
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/deprecated#deprecated
	cli("deprecated-should-not-adopt", func(t *testing.T, rev string) {
		// mcpx does not adopt sampling unless something can answer it.
		p := newPeer(t, rev)
		dialClient(t, p, clientOpts())
		f := p.sent()[0]
		var caps map[string]any
		if isModern(rev) {
			caps = asMap(asMap(asMap(f["params"])["_meta"])[mcpserver.MetaClientCapabilities])
		} else {
			caps = asMap(asMap(f["params"])["capabilities"])
		}
		if _, ok := caps["sampling"]; ok {
			t.Errorf("declared sampling with no handler: %v", caps)
		}
	})
}
