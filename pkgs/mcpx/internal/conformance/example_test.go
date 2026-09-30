package conformance_test

import (
	"testing"
)

// The harness's own worked example, and the first requirement of the
// catalogue: every frame is JSON-RPC 2.0, in every revision, both ways.
// Validation happens inside the harness; the test only has to drive a
// representative exchange.
func TestJSONRPCFramesEveryRevision(t *testing.T) {
	for _, rev := range allRevs {
		// https://modelcontextprotocol.io/specification/2025-11-25/basic#messages
		t.Run(rev+"/messages/all-messages-follow-jsonrpc-2-server-stdio", func(t *testing.T) {
			srv, _ := newServer(t)
			ss := stdioServer(t, srv)
			ss.initialize(t, rev)
			for _, m := range methodsOf(rev, "ping", "tools/list", "resources/list", "prompts/list") {
				r := ss.request(t, rev, m, nil)
				if r["jsonrpc"] != "2.0" || r["error"] != nil {
					t.Errorf("%s: %v", m, r)
				}
			}
		})
		t.Run(rev+"/messages/all-messages-follow-jsonrpc-2-server-http", func(t *testing.T) {
			srv, _ := newServer(t)
			hs := httpServer(t, srv)
			sess := hs.initialize(t, rev)
			for _, m := range methodsOf(rev, "ping", "tools/list") {
				res, r := hs.request(t, rev, sess, m, nil)
				if r == nil || r["jsonrpc"] != "2.0" || r["error"] != nil {
					t.Errorf("%s: %d %s", m, res.Status, res.Body)
				}
			}
		})
		t.Run(rev+"/messages/all-messages-follow-jsonrpc-2-client", func(t *testing.T) {
			p := newPeer(t, rev)
			c := dialClient(t, p, clientOpts())
			if _, err := c.ListTools(ctxT(t)); err != nil {
				t.Fatal(err)
			}
			if !isModern(rev) {
				// 2026-07-28 has no ping.
				if err := c.Ping(ctxT(t)); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range p.sent() {
				if f["jsonrpc"] != "2.0" {
					t.Errorf("not JSON-RPC 2.0: %v", f)
				}
			}
		})
	}
}
