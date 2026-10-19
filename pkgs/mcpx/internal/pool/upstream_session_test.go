package pool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/pool"
)

// sessionUpstream is a legacy streamable HTTP server: it issues Mcp-Session-Id
// on initialize and answers 404 to a session it no longer knows.
type sessionUpstream struct {
	*httptest.Server
	mu      sync.Mutex
	live    map[string]bool
	issued  int
	deletes int
	// deleteAttempts and deleted record every DELETE that arrives, including
	// ones answered 404, so a failure can tell "no DELETE arrived" apart from
	// "a DELETE arrived for a session the upstream no longer knew" (#290).
	deleteAttempts int
	deleted        []string
	served         []string
}

func newSessionUpstream(t *testing.T) *sessionUpstream {
	t.Helper()
	u := &sessionUpstream{live: map[string]bool{}}
	u.Server = httptest.NewServer(http.HandlerFunc(u.serve))
	t.Cleanup(u.Close)
	return u
}

func (u *sessionUpstream) expireAll() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.live = map[string]bool{}
}

func (u *sessionUpstream) serve(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	sid := r.Header.Get("Mcp-Session-Id")
	switch r.Method {
	case http.MethodDelete:
		u.deleteAttempts++
		u.deleted = append(u.deleted, sid)
		if u.live[sid] {
			delete(u.live, sid)
			u.deletes++
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case http.MethodPost:
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch {
		case req.Method == "initialize":
			u.issued++
			sid = fmt.Sprintf("sess-%d", u.issued)
			u.live[sid] = true
			w.Header().Set("Mcp-Session-Id", sid)
			rpcReply(w, req.ID, map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "upstream", "version": "1"},
			})
		case req.Method == "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case req.Method == "server/discover":
			rpcError(w, req.ID, -32601, "method not found")
		case !u.live[sid]:
			w.WriteHeader(http.StatusNotFound)
		default:
			u.served = append(u.served, sid)
			rpcReply(w, req.ID, map[string]any{})
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func rpcReply(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func rpcError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

func upstreamPool(t *testing.T, url string) *pool.Pool {
	t.Helper()
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"remote": {Name: "remote", URL: url + "/mcp"},
	}}
	r, err := cfg.Resolve("remote")
	if err != nil {
		t.Fatal(err)
	}
	p := pool.New(r)
	t.Cleanup(p.Close)
	return p
}

func pingThrough(t *testing.T, p *pool.Pool, key string) {
	t.Helper()
	lease, err := p.Acquire(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := lease.Client().Ping(context.Background()); err != nil {
		t.Fatalf("ping through %s: %v", key, err)
	}
}

// handOff moves the old pool's live upstream sessions to the successor and
// commits, as the takeover does.
func handOff(t *testing.T, from, to *pool.Pool) {
	t.Helper()
	ph, err := from.Detach()
	if err != nil {
		t.Fatal(err)
	}
	if len(ph.Instances) != 1 || ph.Instances[0].HTTP == nil {
		t.Fatalf("want the one upstream session handed on, got %+v", ph.Instances)
	}
	ads, err := to.AdoptSessions(ph.Instances)
	if err != nil {
		t.Fatal(err)
	}
	from.Commit()
	to.Attach(ads, ph.Seq)
}

func TestAnUpstreamSessionSurvivesATakeover(t *testing.T) {
	up := newSessionUpstream(t)
	old := upstreamPool(t, up.URL)
	pingThrough(t, old, "k")
	successor := upstreamPool(t, up.URL)
	handOff(t, old, successor)

	pingThrough(t, successor, "k")
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.issued != 1 {
		t.Fatalf("the upstream issued %d sessions; the handoff should reuse the first", up.issued)
	}
	if up.deletes != 0 {
		t.Fatalf("the handoff ended the upstream session with %d DELETE(s)", up.deletes)
	}
	for _, sid := range up.served {
		if sid != "sess-1" {
			t.Fatalf("a request after the takeover carried session %q, want sess-1", sid)
		}
	}
	if len(up.served) == 0 {
		t.Fatal("no request after the takeover reached the upstream session")
	}
}

// lifecycleCapture records pool lifecycle events of several kinds at once.
// lifecycleEvents installs a single-kind hook, and TerminateOrphanedSession's
// failure path (#290) is only visible in the kind the test is not asserting
// on, so this test needs both the terminated and the orphaned events.
func lifecycleCapture(t *testing.T, kinds ...string) func(string) []map[string]any {
	t.Helper()
	want := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	var mu sync.Mutex
	got := map[string][]map[string]any{}
	prev := pool.Lifecycle
	pool.Lifecycle = func(event string, attrs map[string]any) {
		if want[event] {
			mu.Lock()
			got[event] = append(got[event], attrs)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { pool.Lifecycle = prev })
	return func(kind string) []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), got[kind]...)
	}
}

// A handed session nobody adopted is ended upstream by
// TerminateOrphanedSession, once the predecessor has committed.
func TestTerminateOrphanedSessionEndsTheUpstreamSession(t *testing.T) {
	events := lifecycleCapture(t, "upstream.session.terminated", "upstream.session.orphaned")
	up := newSessionUpstream(t)
	old := upstreamPool(t, up.URL)
	pingThrough(t, old, "k")
	ph, err := old.Detach()
	if err != nil {
		t.Fatal(err)
	}
	old.Commit()
	pool.TerminateOrphanedSession("remote", ph.Instances[0])

	up.mu.Lock()
	defer up.mu.Unlock()
	if up.deletes != 1 {
		// #290: say why termination did not land — the orphaned events carry
		// the reason TerminateOrphanedSession reported, and the mock's
		// attempt log separates "no DELETE arrived" from "the DELETE named
		// a session the upstream no longer knew".
		t.Fatalf("the orphaned session saw %d DELETE(s), want exactly 1: %d DELETE attempt(s) for sessions %v; orphaned events = %v",
			up.deletes, up.deleteAttempts, up.deleted, events("upstream.session.orphaned"))
	}
	terminated := events("upstream.session.terminated")
	if len(terminated) != 1 || terminated[0]["server"] != "remote" || terminated[0]["session"] != "sess-1" {
		t.Fatalf("terminated events = %v, want one naming remote/sess-1; orphaned events = %v",
			terminated, events("upstream.session.orphaned"))
	}
}

// A session that cannot be terminated — the endpoint is already gone — is
// reported, not dropped silently.
func TestTerminateOrphanedSessionReportsAnUnreachableEndpoint(t *testing.T) {
	orphaned := lifecycleEvents(t, "upstream.session.orphaned")
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := dead.URL
	dead.Close()

	pool.TerminateOrphanedSession("remote", pool.InstanceHandoff{
		ID:   "i1",
		HTTP: &mcpclient.HTTPHandoff{SessionID: "sess-9", Negotiated: "2025-03-26", URL: url + "/mcp"},
	})

	events := orphaned()
	if len(events) != 1 {
		t.Fatalf("orphaned events = %v, want one", events)
	}
	if events[0]["server"] != "remote" || events[0]["session"] != "sess-9" || events[0]["reason"] == "" {
		t.Fatalf("orphaned event = %v, want server, session and a reason", events[0])
	}
}

func TestAnExpiredUpstreamSessionIsReinitialised(t *testing.T) {
	up := newSessionUpstream(t)
	old := upstreamPool(t, up.URL)
	pingThrough(t, old, "k")
	successor := upstreamPool(t, up.URL)
	up.expireAll()
	handOff(t, old, successor)

	if st := successor.Status(); st.Live != 0 {
		t.Fatalf("an expired session must not be served, %d live", st.Live)
	}
	pingThrough(t, successor, "k")
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.issued != 2 {
		t.Fatalf("the upstream issued %d sessions; an expired one should be replaced once", up.issued)
	}
}
