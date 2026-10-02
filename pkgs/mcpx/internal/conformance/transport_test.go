package conformance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/settings"
	"github.com/dezren39/mcpx/internal/spec"
)

// callParams is a tools/call that reaches the (possibly blocking) backend.
func callParams() map[string]any {
	return map[string]any{"name": "mcpx_call", "arguments": map[string]any{"namespace": "a", "tool": "b"}}
}

func waitSignal(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(wait):
		t.Fatalf("%s did not happen", what)
	}
}

// Transports: mcpx as a server.
func TestTransportServer(t *testing.T) {
	srvSide := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "server", id, fn) }

	// ---- stdio ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#stdio
	for _, id := range []string{"stdio-no-embedded-newlines", "stdio-server-stdout-only-mcp", "stdio-server-may-log-to-stderr"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			srv = srv.WithExtras([]mcpserver.Extra{{Tool: mcpserver.Tool{Name: "multiline", Description: "m",
				InputSchema: json.RawMessage(`{"type":"object"}`)},
				Call: func(context.Context, json.RawMessage) (string, error) { return "line one\nline two\r\n", nil }}})
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			r := ss.request(t, rev, "tools/call", map[string]any{"name": "multiline", "arguments": map[string]any{}})
			b, _ := json.Marshal(r)
			if !strings.Contains(string(b), `line one\nline two`) {
				t.Errorf("the text did not survive: %s", b)
			}
			// Every line stdout carried was one JSON-RPC message: the
			// harness fails on any line that is not one.
			ss.request(t, rev, "tools/list", nil)
		})
	}
	// https://modelcontextprotocol.io/specification/2025-06-18/basic/transports#stdio
	srvSide("stdio-message-is-single", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		ss.send(t, append(append([]byte("["), frame(1, "tools/list", params(rev, nil))...), ']'))
		b := ss.next(t)
		if strings.HasPrefix(string(b), "[") {
			t.Fatalf("a batch was answered as one: %s", b)
		}
		if errorCode(decode(t, b)) == 0 {
			t.Errorf("a batch was not refused: %s", b)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#stdio
	srvSide("stdio-message-may-be-batch", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		ss := stdioServer(t, srv)
		ss.initialize(t, rev)
		ss.send(t, []byte(`[{"jsonrpc":"2.0","id":1,"method":"tools/list"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`))
		var arr []map[string]any
		if err := json.Unmarshal(ss.next(t), &arr); err != nil || len(arr) != 2 {
			t.Errorf("%v %v", arr, err)
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#shutdown
	for _, id := range []string{"stdio-server-exit-on-eof", "stdio-server-may-close-stdout"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.request(t, rev, "tools/list", nil)
			ss.in.Close()
			select {
			case <-ss.done:
				ss.done <- nil
			case <-time.After(wait):
				t.Fatal("did not exit at EOF")
			}
			if _, quiet := ss.quiet(wait); !quiet {
				t.Error("stdout still open")
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#cancellation
	for _, id := range []string{"stdio-server-stop-cancelled-work", "stdio-server-silent-after-cancel"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, b := newServer(t)
			b.blocking(t)
			ss := stdioServer(t, srv)
			ss.send(t, frame(5, "tools/call", params(rev, callParams())))
			waitSignal(t, b.started, "the call")
			ss.send(t, frame(nil, "notifications/cancelled", params(rev, map[string]any{"requestId": 5})))
			waitSignal(t, b.ended, "the backend seeing the cancellation")
			if f, quiet := ss.quiet(300 * time.Millisecond); !quiet {
				t.Errorf("answered a cancelled request: %s", f)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#server-initiated-messages
	for _, id := range []string{"stdio-server-no-requests", "stdio-server-cancelled-only-for-listen"} {
		srvSide(id, func(t *testing.T, rev string) {
			// A question the client never answers: a modern client is handed
			// it in a result, never sent a request, and so never sent the
			// cancellation of one either.
			srv, _ := newServer(t)
			srv.Ask = newAsker("done", elicitQ("q"))
			ss := stdioServer(t, srv)
			r := ss.request(t, rev, "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks()))
			if resultOf(t, r)["resultType"] != "input_required" {
				t.Fatalf("%v", r)
			}
			if f, quiet := ss.quiet(300 * time.Millisecond); !quiet {
				t.Errorf("unexpected frame to a modern client: %s", f)
			}
		})
	}

	// ---- Streamable HTTP: endpoint, origin, binding ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#streamable-http
	srvSide("streamable-http-server-single-endpoint-post-get", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.Notify = newNotifier()
		hs := httpServer(t, srv)
		sess := hs.initialize(t, rev)
		if res, r := hs.request(t, rev, sess, "tools/list", nil); res.Status != http.StatusOK || errorCode(r) != 0 {
			t.Fatalf("POST: %d", res.Status)
		}
		req, _ := http.NewRequest(http.MethodGet, hs.ts.URL, nil)
		req.Header.Set("Accept", "text/event-stream")
		for k, v := range headersFor(rev, sess, "", "") {
			req.Header.Set(k, v)
		}
		st := openStream(t, hs.ts.Client(), req)
		if st.Status != http.StatusOK && st.Status != http.StatusMethodNotAllowed {
			t.Errorf("GET on the endpoint: %d", st.Status)
		}
	})
	srvSide("streamable-http-server-single-endpoint-post", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		for _, m := range []string{"server/discover", "tools/list", "prompts/list"} {
			if res, _ := hs.request(t, rev, "", m, nil); res.Status != http.StatusOK {
				t.Errorf("%s: %d", m, res.Status)
			}
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#security-warning
	for _, id := range []string{"streamable-http-server-validate-origin", "streamable-http-server-403-invalid-origin"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			hs := httpServer(t, srv)
			h := headersFor(rev, "", "tools/list", "")
			h["Origin"] = "https://attacker.example"
			if r := hs.post(t, frame(1, "tools/list", params(rev, nil)), h); r.Status != http.StatusForbidden {
				t.Errorf("foreign origin: %d", r.Status)
			}
			h["Origin"] = "http://localhost:1234"
			if r := hs.post(t, frame(1, "tools/list", params(rev, nil)), h); r.Status == http.StatusForbidden {
				t.Errorf("loopback origin refused")
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#security-warning
	srvSide("streamable-http-server-bind-localhost", func(t *testing.T, rev string) {
		for _, s := range settings.Registry() {
			if s.Path == "daemon.address" {
				if fmt.Sprint(s.Default) != "127.0.0.1" {
					t.Errorf("daemon.address defaults to %v", s.Default)
				}
				return
			}
		}
		t.Fatal("no daemon.address setting")
	})

	// ---- Streamable HTTP: POST outcomes ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#sending-messages-to-the-server
	srvSide("streamable-http-server-202-for-non-request", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		sess := hs.initialize(t, rev)
		r := hs.post(t, frame(nil, "notifications/roots/list_changed", params(rev, nil)),
			headersFor(rev, sess, "notifications/roots/list_changed", ""))
		if r.Status != http.StatusAccepted || len(r.Body) != 0 {
			t.Errorf("%d %q", r.Status, r.Body)
		}
	})
	srvSide("streamable-http-server-error-status-if-rejected", func(t *testing.T, rev string) {
		// A response to a request mcpx never sent cannot be accepted.
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		sess := hs.initialize(t, rev)
		r := hs.post(t, []byte(`{"jsonrpc":"2.0","id":424242,"result":{}}`), headersFor(rev, sess, "", ""))
		if r.Status < 400 || r.Status > 499 {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	})
	srvSide("streamable-http-server-json-or-sse-for-request", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		sess := hs.initialize(t, rev)
		res, _ := hs.request(t, rev, sess, "tools/list", nil)
		ct := res.Header.Get("Content-Type")
		if res.Status != http.StatusOK || !(strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/event-stream")) {
			t.Errorf("%d %q", res.Status, ct)
		}
	})

	// A call that asks its legacy client a question over the POST's own
	// stream, which nobody answers: the stream carries the question, then
	// the response, then ends.
	askedOverPost := func(t *testing.T, rev string) (httpResult, []map[string]any) {
		srv, _ := newServer(t)
		q, caps := elicitQ("q"), map[string]any{"elicitation": map[string]any{}}
		if rev < rev20250618 {
			q, caps = sampleQ("q"), map[string]any{"sampling": map[string]any{}}
		}
		srv.Ask = newAsker("done", q)
		srv.Timing = mcpserver.Timing{AskTimeout: 300 * time.Millisecond, AskPoll: 20 * time.Millisecond}
		hs := httpServer(t, srv)
		r := hs.post(t, frame(0, "initialize", initParams(rev, caps)), headersFor(rev20250326, "", "", ""))
		sess := r.Header.Get("Mcp-Session-Id")
		hs.post(t, frame(nil, "notifications/initialized", nil), headersFor(rev, sess, "", ""))
		res := hs.post(t, frame(9, "tools/call", callThatAsks()), headersFor(rev, sess, "tools/call", "mcpx_call"))
		var frames []map[string]any
		for _, f := range res.Frames {
			frames = append(frames, decode(t, f))
		}
		return res, frames
	}
	for _, id := range []string{"streamable-http-sse-includes-response", "streamable-http-post-sse-may-carry-requests",
		"streamable-http-sse-not-closed-before-response", "streamable-http-server-terminates-sse-after-response"} {
		srvSide(id, func(t *testing.T, rev string) {
			if isModern(rev) {
				// 2026-07-28: a question is a result, so the response is
				// JSON and the exchange ends with it.
				srv, _ := newServer(t)
				srv.Ask = newAsker("done", elicitQ("q"))
				hs := httpServer(t, srv)
				res, r := hs.request(t, rev, "", "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks()))
				if len(res.Frames) != 1 || resultOf(t, r)["resultType"] != "input_required" {
					t.Errorf("%d %s", res.Status, res.Body)
				}
				return
			}
			res, frames := askedOverPost(t, rev)
			if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
				t.Fatalf("not a stream: %q %s", res.Header.Get("Content-Type"), res.Body)
			}
			if len(frames) < 2 || frames[0]["method"] == nil || frames[len(frames)-1]["id"] != float64(9) {
				t.Fatalf("want the question, then the response last: %v", frames)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#multiple-connections
	srvSide("streamable-http-no-broadcast", func(t *testing.T, rev string) {
		// With the session's GET stream open, a question for a POST goes
		// on that POST's stream only.
		srv, _ := newServer(t)
		srv.Notify = newNotifier()
		q, caps := elicitQ("q"), map[string]any{"elicitation": map[string]any{}}
		if rev < rev20250618 {
			q, caps = sampleQ("q"), map[string]any{"sampling": map[string]any{}}
		}
		srv.Ask = newAsker("done", q)
		srv.Timing = mcpserver.Timing{AskTimeout: 300 * time.Millisecond, AskPoll: 20 * time.Millisecond}
		hs := httpServer(t, srv)
		r := hs.post(t, frame(0, "initialize", initParams(rev, caps)), headersFor(rev20250326, "", "", ""))
		sess := r.Header.Get("Mcp-Session-Id")
		hs.post(t, frame(nil, "notifications/initialized", nil), headersFor(rev, sess, "", ""))
		get, _ := http.NewRequest(http.MethodGet, hs.ts.URL, nil)
		for k, v := range headersFor(rev, sess, "", "") {
			get.Header.Set(k, v)
		}
		gs := openStream(t, hs.ts.Client(), get)
		res := hs.post(t, frame(9, "tools/call", callThatAsks()), headersFor(rev, sess, "tools/call", "mcpx_call"))
		if !strings.Contains(string(res.Body), q.Method) {
			t.Fatalf("the question was not on the POST stream: %s", res.Body)
		}
		lines, _ := gs.rest(100 * time.Millisecond)
		for _, f := range dataFrames(t, lines) {
			if f["method"] == q.Method {
				t.Errorf("the question was also on the GET stream")
			}
		}
	})

	// ---- cancellation over HTTP ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#sending-messages-to-the-server
	srvSide("streamable-http-disconnect-not-cancel", func(t *testing.T, rev string) {
		srv, b := newServer(t)
		b.blocking(t)
		hs := httpServer(t, srv)
		sess := hs.initialize(t, rev)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			_, _ = hs.ts.Client().Do(hs.postReq(t, frame(5, "tools/call", callParams()),
				headersFor(rev, sess, "tools/call", "mcpx_call")).WithContext(ctx))
		}()
		waitSignal(t, b.started, "the call")
		cancel()
		select {
		case <-b.ended:
			t.Fatal("a disconnect cancelled the call")
		case <-time.After(300 * time.Millisecond):
		}
		hs.post(t, frame(nil, "notifications/cancelled", map[string]any{"requestId": 5}),
			headersFor(rev, sess, "notifications/cancelled", ""))
		waitSignal(t, b.ended, "cancellation by notification")
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#cancellation
	for _, id := range []string{"streamable-http-close-is-cancel", "streamable-http-stop-cancelled-work",
		"streamable-http-silent-after-cancel"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, b := newServer(t)
			b.blocking(t)
			hs := httpServer(t, srv)
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				_, _ = hs.ts.Client().Do(hs.postReq(t, frame(5, "tools/call", params(rev, callParams())),
					headersFor(rev, "", "tools/call", "mcpx_call")).WithContext(ctx))
			}()
			waitSignal(t, b.started, "the call")
			cancel()
			waitSignal(t, b.ended, "the backend seeing the closed stream")
		})
	}

	// ---- GET streams ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#listening-for-messages-from-the-server
	for _, id := range []string{"streamable-http-get-sse-or-405", "streamable-http-get-close-any-time"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			srv.Notify = newNotifier()
			hs := httpServer(t, srv)
			noSess, _ := http.NewRequest(http.MethodGet, hs.ts.URL, nil)
			if r := hs.do(t, noSess); r.Status != http.StatusMethodNotAllowed {
				t.Errorf("GET without a session: %d", r.Status)
			}
			sess := hs.initialize(t, rev)
			get, _ := http.NewRequest(http.MethodGet, hs.ts.URL, nil)
			get.Header.Set("Accept", "text/event-stream")
			for k, v := range headersFor(rev, sess, "", "") {
				get.Header.Set(k, v)
			}
			st := openStream(t, hs.ts.Client(), get)
			if st.Status != http.StatusOK || !strings.HasPrefix(st.Header.Get("Content-Type"), "text/event-stream") {
				t.Fatalf("GET: %d %q", st.Status, st.Header.Get("Content-Type"))
			}
			del, _ := http.NewRequest(http.MethodDelete, hs.ts.URL, nil)
			del.Header.Set("Mcp-Session-Id", sess)
			hs.do(t, del)
			if _, ended := st.rest(wait); !ended {
				t.Error("the server did not close the GET stream when the session ended")
			}
		})
	}

	// ---- sessions ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#session-management
	for _, id := range []string{"streamable-http-server-may-assign-session", "streamable-http-session-id-secure",
		"streamable-http-session-id-visible-ascii"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			hs := httpServer(t, srv)
			a, b := hs.initialize(t, rev), hs.initialize(t, rev)
			if a == "" || a == b {
				t.Fatalf("sessions %q %q", a, b)
			}
			if !regexp.MustCompile(`^[\x21-\x7E]+$`).MatchString(a) {
				t.Errorf("not visible ASCII: %q", a)
			}
			if hex := regexp.MustCompile(`[0-9a-f]{32}`).FindString(a); hex == "" {
				t.Errorf("%q does not carry 128 random bits", a)
			}
		})
	}
	for _, id := range []string{"streamable-http-server-404-terminated-session", "streamable-http-server-may-405-delete"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			hs := httpServer(t, srv)
			sess := hs.initialize(t, rev)
			del, _ := http.NewRequest(http.MethodDelete, hs.ts.URL, nil)
			del.Header.Set("Mcp-Session-Id", sess)
			if r := hs.do(t, del); r.Status/100 != 2 && r.Status != http.StatusMethodNotAllowed {
				t.Fatalf("DELETE: %d", r.Status)
			}
			if r, _ := hs.request(t, rev, sess, "tools/list", nil); r.Status != http.StatusNotFound {
				t.Errorf("request on the ended session: %d", r.Status)
			}
			if r, _ := hs.request(t, rev, "no-such-session", "tools/list", nil); r.Status != http.StatusNotFound {
				t.Errorf("request on an unknown session: %d", r.Status)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#session-management
	srvSide("streamable-http-server-may-terminate-on-session-expiry", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.Timing = mcpserver.Timing{SessionIdle: 100 * time.Millisecond}
		hs := httpServer(t, srv)
		sess := hs.initialize(t, rev)
		time.Sleep(300 * time.Millisecond)
		hs.initialize(t, rev) // the reaper runs as sessions are made
		if r, _ := hs.request(t, rev, sess, "tools/list", nil); r.Status != http.StatusNotFound {
			t.Errorf("an idle session was kept: %d", r.Status)
		}
	})

	// ---- version header ----

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#protocol-version-header
	srvSide("streamable-http-server-assumes-2025-03-26", func(t *testing.T, rev string) {
		// No header, no session: 2025-03-26, which is the one revision
		// that answers a batch.
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		r := hs.post(t, []byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`), nil)
		var arr []any
		if json.Unmarshal(r.Body, &arr) != nil || len(arr) != 1 {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	})
	srvSide("streamable-http-server-400-unsupported-version-header", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		sess := hs.initialize(t, rev)
		for _, bad := range []string{"garbage", "2024-10-07"} {
			h := headersFor(rev, sess, "tools/list", "")
			h["MCP-Protocol-Version"] = bad
			if r := hs.post(t, frame(1, "tools/list", nil), h); r.Status != http.StatusBadRequest {
				t.Errorf("%s: %d", bad, r.Status)
			}
		}
	})
	srvSide("streamable-http-missing-version-header-legacy", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		r := hs.post(t, frame(1, "tools/list", nil), nil)
		m := decode(t, r.Body)
		if r.Status != http.StatusOK || resultOf(t, m)["resultType"] != nil {
			t.Errorf("not served as legacy: %d %s", r.Status, r.Body)
		}
	})

	// ---- 2026-07-28 headers and statuses ----

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#server-validation
	mismatch := func(t *testing.T, r httpResult, id float64) {
		t.Helper()
		m := decode(t, r.Body)
		if r.Status != http.StatusBadRequest || errorCode(m) != -32020 || m["id"] != id {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	}
	for _, id := range []string{"streamable-http-header-mismatch-400-jsonrpc", "streamable-http-server-rejects-header-body-mismatch",
		"streamable-http-protocol-version-header-matches-meta"} {
		srvSide(id, func(t *testing.T, rev string) {
			srv, _ := newServer(t)
			hs := httpServer(t, srv)
			body := frame(7, "tools/call", params(rev, map[string]any{"name": "mcpx_status", "arguments": map[string]any{}}))
			h := headersFor(rev, "", "tools/call", "mcpx_status")
			h["MCP-Protocol-Version"] = rev20251125
			mismatch(t, hs.post(t, body, h), 7)
			h = headersFor(rev, "", "tools/list", "mcpx_status")
			mismatch(t, hs.post(t, body, h), 7)
			h = headersFor(rev, "", "tools/call", "mcpx_other")
			mismatch(t, hs.post(t, body, h), 7)
		})
	}
	srvSide("streamable-http-server-decodes-before-compare", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		body := frame(7, "tools/call", params(rev, map[string]any{"name": "mcpx_status", "arguments": map[string]any{}}))
		h := headersFor(rev, "", "tools/call", "=?base64?bWNweF9zdGF0dXM=?=")
		if r := hs.post(t, body, h); r.Status != http.StatusOK {
			t.Errorf("an encoded name that matches was refused: %d %s", r.Status, r.Body)
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#server-behavior-for-custom-headers
	// mcpx re-lists upstream tools verbatim, so an upstream's x-mcp-header
	// annotation is one mcpx's own server must check.
	srvSide("streamable-http-server-validates-param-headers", func(t *testing.T, rev string) {
		b := newBackend()
		srv := mcpserver.New(b, "mcpx", "test").WithExtras([]mcpserver.Extra{{
			Tool: mcpserver.Tool{Name: "echo_region", InputSchema: json.RawMessage(
				`{"type":"object","properties":{"region":{"type":"string","x-mcp-header":"Region"}}}`)},
			Call: func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
		}})
		hs := httpServer(t, srv)
		body := frame(7, "tools/call", params(rev, map[string]any{"name": "echo_region",
			"arguments": map[string]any{"region": "Hello"}}))
		h := headersFor(rev, "", "tools/call", "echo_region")
		mismatch(t, hs.post(t, body, h), 7) // omitted while the body has a value
		h["Mcp-Param-Region"] = "Goodbye"
		mismatch(t, hs.post(t, body, h), 7)
		h["Mcp-Param-Region"] = "=?base64?SGVsbG8?=" // unpadded
		mismatch(t, hs.post(t, body, h), 7)
		h["Mcp-Param-Region"] = "=?base64?SGVsbG8=?="
		if r := hs.post(t, body, h); r.Status != http.StatusOK {
			t.Errorf("a matching encoded header was refused: %d %s", r.Status, r.Body)
		}
	})
	srvSide("streamable-http-header-names-case-insensitive", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		req := hs.postReq(t, frame(1, "tools/list", params(rev, nil)), nil)
		req.Header["mcp-protocol-version"] = []string{rev}
		req.Header["MCP-METHOD"] = []string{"tools/list"}
		if r := hs.do(t, req); r.Status != http.StatusOK {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	})
	srvSide("streamable-http-unknown-method-404", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		r := hs.post(t, frame(1, "x/nope", params(rev, nil)), headersFor(rev, "", "x/nope", ""))
		if r.Status != http.StatusNotFound || errorCode(decode(t, r.Body)) != -32601 {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	})
	srvSide("streamable-http-unsupported-version-400", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		p := params(rev, nil)
		p["_meta"].(map[string]any)[mcpserver.MetaProtocolVersion] = "2099-01-01"
		h := headersFor(rev, "", "tools/list", "")
		h["MCP-Protocol-Version"] = "2099-01-01"
		if r := hs.post(t, frame(1, "tools/list", p), h); r.Status != http.StatusBadRequest || errorCode(decode(t, r.Body)) != -32022 {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	})
	srvSide("streamable-http-missing-capability-400", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.Notify = newNotifier()
		hs := httpServer(t, srv)
		p := params(rev, map[string]any{"notifications": map[string]any{"taskIds": []string{"t1"}}})
		r := hs.post(t, frame(1, "subscriptions/listen", p), headersFor(rev, "", "subscriptions/listen", ""))
		if r.Status != http.StatusBadRequest || errorCode(decode(t, r.Body)) != -32021 {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	})

	// ---- 2026-07-28 streams ----

	listenStream := func(t *testing.T, rev string) (*stream, *notifier) {
		srv, _ := newServer(t)
		n := newNotifier()
		srv.Notify = n
		srv.Timing = mcpserver.Timing{SSEKeepAlive: 50 * time.Millisecond}
		hs := httpServer(t, srv)
		st := openStream(t, hs.ts.Client(), hs.postReq(t, frame(5, "subscriptions/listen",
			params(rev, map[string]any{"notifications": map[string]any{"promptsListChanged": true}})),
			headersFor(rev, "", "subscriptions/listen", "")))
		st.until(t, func(l string) bool { return strings.HasPrefix(l, "data:") }) // the acknowledgement
		time.Sleep(50 * time.Millisecond)
		return st, n
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#server-sent-events
	srvSide("streamable-http-x-accel-buffering", func(t *testing.T, rev string) {
		st, _ := listenStream(t, rev)
		if st.Header.Get("X-Accel-Buffering") != "no" {
			t.Errorf("X-Accel-Buffering %q", st.Header.Get("X-Accel-Buffering"))
		}
	})
	srvSide("streamable-http-sse-comment-keepalive", func(t *testing.T, rev string) {
		st, _ := listenStream(t, rev)
		st.until(t, func(l string) bool { return strings.HasPrefix(l, ":") })
	})
	for _, id := range []string{"streamable-http-sse-notifications-must-relate", "streamable-http-no-resumability"} {
		srvSide(id, func(t *testing.T, rev string) {
			st, n := listenStream(t, rev)
			n.ch <- [2]any{"notifications/prompts/list_changed", map[string]any{}}
			lines := st.until(t, func(l string) bool { return strings.Contains(l, "list_changed") })
			for _, l := range lines {
				if strings.HasPrefix(l, "id:") {
					t.Errorf("an event id invites resumption: %q", l)
				}
			}
			f := dataFrames(t, lines[len(lines)-1:])[0]
			if asMap(asMap(f["params"])["_meta"])["io.modelcontextprotocol/subscriptionId"] != float64(5) {
				t.Errorf("not tied to the listen request: %v", f)
			}
		})
	}
	srvSide("streamable-http-sse-no-requests", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		srv.Ask = newAsker("done", elicitQ("q"))
		hs := httpServer(t, srv)
		res, _ := hs.request(t, rev, "", "tools/call", paramsWith(rev, `{"elicitation":{}}`, callThatAsks()))
		for _, f := range res.Frames {
			if m := decode(t, f); m["method"] != nil && m["id"] != nil {
				t.Errorf("a request on the response stream: %s", f)
			}
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/deprecated
	srvSide("http-sse-deprecated-no-new-adoption", func(t *testing.T, rev string) {
		srv, _ := newServer(t)
		hs := httpServer(t, srv)
		req, _ := http.NewRequest(http.MethodGet, hs.ts.URL, nil)
		req.Header.Set("Accept", "text/event-stream")
		if r := hs.do(t, req); strings.Contains(string(r.Body), "event: endpoint") {
			t.Errorf("serves HTTP+SSE")
		}
	})
}

// scriptedChild is a stdio "server" written in sh: it logs to stderr, reads
// each request line into a file, and answers initialize for rev. Enough to
// see what mcpx writes to a child and how it treats the child's stderr.
func scriptedChild(t *testing.T, rev string) (mcpclient.StdioOptions, string) {
	dir := t.TempDir()
	in := filepath.Join(dir, "stdin")
	init := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"%s","capabilities":{"tools":{}},"serverInfo":{"name":"sh","version":"1"}}}`, rev)
	tools := `{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`
	script := `echo "starting up; this is not an error" >&2
IFS= read -r l; printf '%s\n' "$l" >> "$1"; printf '%s\n' "$2"
while IFS= read -r l; do printf '%s\n' "$l" >> "$1"; case "$l" in *'"id":2'*) printf '%s\n' "$3";; esac; done`
	return mcpclient.StdioOptions{Command: "/bin/sh", Args: []string{"-c", script, "sh", in, init, tools}, InheritEnv: true}, in
}

// Transports: mcpx as a client.
func TestTransportClient(t *testing.T) {
	cli := func(id string, fn func(t *testing.T, rev string)) { forReq(t, "client", id, fn) }

	// ---- stdio ----

	stdioSession := func(t *testing.T, rev string) (*mcpclient.StdioTransport, []string) {
		opts, in := scriptedChild(t, rev)
		tr, err := mcpclient.NewStdio(opts)
		if err != nil {
			t.Fatal(err)
		}
		o := clientOpts()
		o.Preference = mcpclient.ForceLegacy
		c, err := mcpclient.NewWithOptions(ctxT(t), tr, o)
		if err != nil {
			t.Fatalf("%v (stderr: %s)", err, tr.Stderr(5))
		}
		if _, err := c.Request(ctxT(t), "tools/list", nil); err != nil {
			t.Fatal(err)
		}
		_, _ = c.CallTimeout(ctxT(t), 50*time.Millisecond, "t", map[string]any{"text": "a\nb"})
		time.Sleep(50 * time.Millisecond)
		b, _ := os.ReadFile(in)
		c.Close()
		return tr, strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#stdio
	for _, id := range []string{"transport-client-should-support-stdio", "stdio-no-embedded-newlines",
		"stdio-client-stdin-only-mcp", "stdio-message-is-single"} {
		cli(id, func(t *testing.T, rev string) {
			lr := rev
			if isModern(rev) {
				lr = rev20251125 // the child speaks the legacy handshake; the framing rules are the same
			}
			_, lines := stdioSession(t, lr)
			if len(lines) < 3 {
				t.Fatalf("child saw %q", lines)
			}
			for _, l := range lines {
				var m map[string]any
				if err := json.Unmarshal([]byte(l), &m); err != nil || m["jsonrpc"] != "2.0" {
					t.Errorf("stdin line is not one JSON-RPC message: %q", l)
				}
			}
		})
	}
	for _, id := range []string{"stdio-client-may-capture-stderr", "stdio-client-stderr-not-error"} {
		cli(id, func(t *testing.T, rev string) {
			lr := rev
			if isModern(rev) {
				lr = rev20251125
			}
			tr, _ := stdioSession(t, lr) // connecting succeeded despite stderr output
			if !strings.Contains(tr.Stderr(4096), "starting up") {
				t.Errorf("stderr not captured: %q", tr.Stderr(4096))
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#cancellation
	for _, id := range []string{"stdio-client-cancel-by-notification"} {
		cli(id, func(t *testing.T, rev string) {
			p := newPeer(t, rev)
			p.on("tools/call", func(map[string]any) (any, *rpcError) { return nil, nil })
			c := dialClient(t, p, clientOpts())
			_, _ = c.CallTimeout(ctxT(t), 50*time.Millisecond, "echo", map[string]any{})
			time.Sleep(100 * time.Millisecond)
			calls, cancels := p.sentMethod("tools/call"), p.sentMethod("notifications/cancelled")
			if len(cancels) != 1 || asMap(cancels[0]["params"])["requestId"] != calls[0]["id"] {
				t.Errorf("cancels %v for %v", cancels, calls)
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio
	cli("stdio-client-no-responses", func(t *testing.T, rev string) {
		// A 2026-07-28 server may not send requests; if one arrives the
		// client must not answer it.
		p := newPeer(t, rev)
		dialClient(t, p, clientOpts())
		before := len(p.sent())
		p.push(frame(77, "roots/list", nil))
		time.Sleep(100 * time.Millisecond)
		p.mu.Lock()
		after := p.frames[before:]
		p.mu.Unlock()
		for _, b := range after {
			t.Errorf("answered a modern server's request: %s", b)
		}
	})
	cli("stdio-modern-only-client-probe", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		o := clientOpts()
		o.Preference = mcpclient.ForceModern
		dialClient(t, p, o)
		if f := p.sent(); f[0]["method"] != "server/discover" || len(p.sentMethod("initialize")) != 0 {
			t.Errorf("%v", f)
		}
	})
	cli("stdio-no-fallback-on-modern-error", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("server/discover", func(map[string]any) (any, *rpcError) {
			return nil, &rpcError{Code: -32022, Message: "unsupported", Data: map[string]any{"supported": []string{"2099-01-01"}}}
		})
		o := clientOpts()
		o.Preference = mcpclient.PreferModern
		if _, err := dialClientErr(t, p, o); err == nil {
			t.Error("connected without a shared version")
		}
		if len(p.sentMethod("initialize")) != 0 {
			t.Error("a modern error was read as a legacy server")
		}
	})
	cli("stdio-client-shutdown-sequence", func(t *testing.T, rev string) {
		dir := t.TempDir()
		mark := filepath.Join(dir, "mark")
		script := `trap 'echo term >> "$1"; exit 0' TERM; while IFS= read -r l; do :; done; echo eof >> "$1"`
		tr, err := mcpclient.NewStdio(mcpclient.StdioOptions{Command: "/bin/sh", Args: []string{"-c", script, "sh", mark}, InheritEnv: true})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
		_ = tr.Close()
		if b, _ := os.ReadFile(mark); strings.TrimSpace(string(b)) != "eof" {
			t.Errorf("the child saw %q", b)
		}
	})

	// ---- Streamable HTTP ----

	httpSession := func(t *testing.T, rev string, sse bool) (*httpPeerSrv, *mcpclient.Client) {
		p := newPeer(t, rev)
		hp := httpPeer(t, p)
		hp.sse = sse
		if !isModern(rev) {
			hp.session = "sess-abc"
		}
		c, err := dialHTTPClient(t, hp, clientOpts())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Fatal(err)
		}
		return hp, c
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#sending-messages-to-the-server
	for _, id := range []string{"streamable-http-client-post-every-message", "streamable-http-client-accept-both",
		"streamable-http-post-body-may-batch", "streamable-http-post-body-single",
		"streamable-http-post-body-request-or-notification", "http-sse-deprecated-no-new-adoption",
		"streamable-http-no-resumability"} {
		cli(id, func(t *testing.T, rev string) {
			hp, c := httpSession(t, rev, false)
			c.Close()
			for _, r := range hp.requests() {
				// DELETE ends the session; GET opens the listening stream
				// the transport allows. Every message goes by POST.
				if r.Method == http.MethodDelete || r.Method == http.MethodGet {
					continue
				}
				if r.Method != http.MethodPost {
					t.Errorf("%s request", r.Method)
				}
				a := r.Header.Get("Accept")
				if !strings.Contains(a, "application/json") || !strings.Contains(a, "text/event-stream") {
					t.Errorf("Accept %q", a)
				}
				if !strings.HasPrefix(string(r.Body), "{") || !strings.Contains(string(r.Body), `"method"`) {
					t.Errorf("body is not one request or notification: %s", r.Body)
				}
				if r.Header.Get("Last-Event-ID") != "" {
					t.Error("Last-Event-ID sent")
				}
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#session-management
	for _, id := range []string{"streamable-http-client-echoes-session", "streamable-http-client-delete-session",
		"streamable-http-client-handles-session-securely"} {
		cli(id, func(t *testing.T, rev string) {
			hp, c := httpSession(t, rev, false)
			c.Close()
			reqs := hp.requests()
			var deleted bool
			for i, r := range reqs {
				if i > 0 && r.Header.Get("Mcp-Session-Id") != "sess-abc" {
					t.Errorf("request %d lacks the session: %s", i, r.Body)
				}
				if r.Method == http.MethodDelete {
					deleted = true
				}
			}
			if !deleted {
				t.Error("no DELETE at close")
			}
		})
	}
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#sending-messages-to-the-server
	for _, id := range []string{"streamable-http-client-supports-json-and-sse", "streamable-http-sse-comment-keepalive"} {
		cli(id, func(t *testing.T, rev string) {
			httpSession(t, rev, true)  // SSE answers, with a comment before the event
			httpSession(t, rev, false) // JSON answers
		})
	}
	cli("streamable-http-client-cancel-by-notification", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		p.on("tools/call", func(map[string]any) (any, *rpcError) { return nil, nil })
		hp := httpPeer(t, p)
		hp.session = "s"
		c, err := dialHTTPClient(t, hp, clientOpts())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = c.CallTimeout(ctxT(t), 50*time.Millisecond, "echo", map[string]any{})
		time.Sleep(200 * time.Millisecond)
		if len(p.sentMethod("notifications/cancelled")) != 1 {
			t.Errorf("no cancellation POSTed: %v", p.sent())
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#listening-for-messages-from-the-server
	cli("streamable-http-get-close-any-time", func(t *testing.T, rev string) {
		_, c := httpSession(t, rev, false)
		done := make(chan struct{})
		go func() { c.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(wait):
			t.Error("closing the client hung on its streams")
		}
	})
	cli("streamable-http-multiple-streams", func(t *testing.T, rev string) {
		hp, c := httpSession(t, rev, true)
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := c.ListTools(ctxT(t)); errs <- err }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Error(err)
			}
		}
		_ = hp
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http
	for _, id := range []string{"streamable-http-protocol-version-header-every-post",
		"streamable-http-protocol-version-header-matches-meta", "streamable-http-protocol-version-header-is-negotiated"} {
		cli(id, func(t *testing.T, rev string) {
			hp, _ := httpSession(t, rev, false)
			for i, r := range hp.requests() {
				if i == 0 && !isModern(rev) {
					continue // initialize, before anything is negotiated
				}
				if got := r.Header.Get("MCP-Protocol-Version"); got != rev {
					t.Errorf("MCP-Protocol-Version %q on %s", got, r.Body)
				}
			}
		})
	}
	cli("streamable-http-header-names-case-insensitive", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		ts := httptestServer(t, func(w http.ResponseWriter, r *http.Request) {
			var f map[string]any
			_ = json.NewDecoder(r.Body).Decode(&f)
			_ = p.Send(r.Context(), mustJSON(f))
			reply := <-p.in
			w.Header()["content-type"] = []string{"application/json"}
			_, _ = w.Write(reply)
		})
		tr, _ := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: ts})
		o := clientOpts()
		o.Preference = mcpclient.ForceModern
		c, err := mcpclient.NewWithOptions(ctxT(t), tr, o)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Error(err)
		}
	})
	cli("streamable-http-intermediary-http-error", func(t *testing.T, rev string) {
		p := newPeer(t, rev)
		hp := httpPeer(t, p)
		c, err := dialHTTPClient(t, hp, clientOpts())
		if err != nil {
			t.Fatal(err)
		}
		gw := httptestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>bad gateway</html>"))
		})
		tr, _ := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: gw})
		o := clientOpts()
		o.Preference = mcpclient.ForceModern
		if _, err := mcpclient.NewWithOptions(ctxT(t), tr, o); err == nil || !strings.Contains(err.Error(), "502") {
			t.Errorf("a gateway error was not reported as one: %v", err)
		}
		if _, err := c.ListTools(ctxT(t)); err != nil {
			t.Error(err)
		}
	})
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func httptestServer(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL
}

// With 2026-07-28 not held strictly (spec.lenient), a request a 2026-07-28
// server sends is answered rather than dropped: the opposite of
// stdio-client-no-responses, which runs under the default (#307).
func TestLenient2026AnswersServerRequests(t *testing.T) {
	t.Cleanup(spec.Set(spec.Must(nil, "", []string{"2026-07-28"})))
	p := newPeer(t, "2026-07-28")
	dialClient(t, p, clientOpts())
	before := len(p.sent())
	p.push(frame(77, "roots/list", nil))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range p.sent()[before:] {
			if id, _ := f["id"].(float64); id == 77 && f["method"] == nil {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("a lenient client did not answer: %v", p.sent()[before:])
}
