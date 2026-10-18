package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
)

// orphanUpstream is a legacy streamable HTTP server that issues a session id
// on initialize and counts the DELETEs that end sessions.
type orphanUpstream struct {
	*httptest.Server
	mu      sync.Mutex
	live    map[string]bool
	issued  int
	deletes int
	deleted []string
}

func newOrphanUpstream(t *testing.T) *orphanUpstream {
	t.Helper()
	u := &orphanUpstream{live: map[string]bool{}}
	u.Server = httptest.NewServer(http.HandlerFunc(u.serve))
	t.Cleanup(u.Close)
	return u
}

func (u *orphanUpstream) serve(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	sid := r.Header.Get("Mcp-Session-Id")
	switch r.Method {
	case http.MethodDelete:
		u.deletes++
		u.deleted = append(u.deleted, sid)
		if u.live[sid] {
			delete(u.live, sid)
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
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "upstream", "version": "1"},
			}})
		case req.Method == "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case req.Method == "server/discover":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
		case !u.live[sid]:
			w.WriteHeader(http.StatusNotFound)
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (u *orphanUpstream) counts() (deletes int, deleted []string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.deletes, append([]string(nil), u.deleted...)
}

// detachRemote starts a session on a registry whose only server is the
// upstream, then detaches it for a successor.
func detachRemote(t *testing.T, url string) (*Registry, *RegistryHandoff) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"remote": {Name: "remote", URL: url + "/mcp"},
	}}
	old, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(old.Close)
	p, ok := old.Pool("remote")
	if !ok {
		t.Fatal("no pool for remote")
	}
	lease, err := p.Acquire(context.Background(), "global")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Client().Ping(context.Background()); err != nil {
		lease.Release()
		t.Fatalf("ping: %v", err)
	}
	lease.Release()
	ho, err := old.Detach(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(ho.Pools) != 1 || len(ho.Pools[0].Instances) != 1 || ho.Pools[0].Instances[0].HTTP == nil {
		t.Fatalf("want the one upstream session handed on, got %+v", ho.Pools)
	}
	return old, ho
}

func emptyRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	next, err := NewRegistry(&config.Config{MCPServers: map[string]*config.Server{}}, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(next.Close)
	return next
}

// A server removed from the configuration during a takeover has no pool to
// adopt its upstream session. The successor must end that session upstream
// once the handoff commits: exactly one DELETE, carrying the session id.
func TestTakeoverTerminatesARemovedServersUpstreamSession(t *testing.T) {
	up := newOrphanUpstream(t)
	_, ho := detachRemote(t, up.URL)
	next := emptyRegistry(t)

	ad, err := next.Adopt(ho.Pools)
	if err != nil {
		t.Fatal(err)
	}
	ho.Commit()
	ad.Attach()

	deletes, deleted := up.counts()
	if deletes != 1 {
		t.Fatalf("upstream saw %d DELETE(s), want exactly 1 for the removed server's session", deletes)
	}
	if len(deleted) != 1 || deleted[0] != "sess-1" {
		t.Fatalf("DELETE carried session ids %v, want [sess-1]", deleted)
	}
}

// On the abort path the predecessor still owns the session: no DELETE, and
// the predecessor goes on serving it.
func TestAbortedTakeoverLeavesARemovedServersSessionAlone(t *testing.T) {
	up := newOrphanUpstream(t)
	old, ho := detachRemote(t, up.URL)
	next := emptyRegistry(t)

	ad, err := next.Adopt(ho.Pools)
	if err != nil {
		t.Fatal(err)
	}
	ad.Abort()
	ho.Restore()

	if deletes, _ := up.counts(); deletes != 0 {
		t.Fatalf("an aborted handoff ended the upstream session with %d DELETE(s)", deletes)
	}
	p, _ := old.Pool("remote")
	lease, err := p.Acquire(context.Background(), "global")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := lease.Client().Ping(context.Background()); err != nil {
		t.Fatalf("the predecessor no longer serves the session after an abort: %v", err)
	}
	if deletes, _ := up.counts(); deletes != 0 {
		t.Fatalf("serving after an abort ended the upstream session with %d DELETE(s)", deletes)
	}
}
