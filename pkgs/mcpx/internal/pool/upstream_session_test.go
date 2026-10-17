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
	served  []string
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
