package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"sync"
	"syscall"
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
	// blockDelete, when non-nil, holds a DELETE in the handler until it is
	// closed (or the request goes away); deleteStarted is signalled when a
	// blocked DELETE arrives. They simulate a blackholed upstream (#402).
	blockDelete   chan struct{}
	deleteStarted chan struct{}
}

func newOrphanUpstream(t *testing.T) *orphanUpstream {
	t.Helper()
	u := &orphanUpstream{live: map[string]bool{}}
	u.Server = httptest.NewServer(http.HandlerFunc(u.serve))
	t.Cleanup(u.Close)
	return u
}

func (u *orphanUpstream) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete && u.blockDelete != nil {
		if u.deleteStarted != nil {
			select {
			case u.deleteStarted <- struct{}{}:
			default:
			}
		}
		select {
		case <-u.blockDelete:
		case <-r.Context().Done():
			return
		}
	}
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
	ad.WaitOrphanTerminations()

	deletes, deleted := up.counts()
	if deletes != 1 {
		t.Fatalf("upstream saw %d DELETE(s), want exactly 1 for the removed server's session", deletes)
	}
	if len(deleted) != 1 || deleted[0] != "sess-1" {
		t.Fatalf("DELETE carried session ids %v, want [sess-1]", deleted)
	}
}

// #402: ending an orphaned session must not block Attach — and therefore
// the takeover's serving path — when the upstream never answers. The
// DELETE still happens exactly once, in the background, after commit.
func TestTakeoverOrphanTerminationDoesNotBlockAttach(t *testing.T) {
	up := newOrphanUpstream(t)
	up.blockDelete = make(chan struct{})
	up.deleteStarted = make(chan struct{}, 1)
	_, ho := detachRemote(t, up.URL)
	next := emptyRegistry(t)

	ad, err := next.Adopt(ho.Pools)
	if err != nil {
		t.Fatal(err)
	}
	ho.Commit()
	start := time.Now()
	ad.Attach()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Attach blocked %v on an unanswering orphan upstream; orphan termination must run off the serving path", elapsed)
	}
	select {
	case <-up.deleteStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("no orphan DELETE was started in the background after Attach")
	}
	close(up.blockDelete)
	ad.WaitOrphanTerminations()
	if deletes, _ := up.counts(); deletes != 1 {
		t.Fatalf("upstream saw %d DELETE(s), want exactly 1", deletes)
	}
}

// #409: a successor that shuts down while its background orphan DELETE is
// still in flight must not exit first — process exit would kill the DELETE
// and leak the session upstream. shutdown() waits for the termination the
// committed takeover started; the upstream still sees exactly one DELETE.
func TestSuccessorShutdownWaitsForABlockedOrphanTermination(t *testing.T) {
	up := newOrphanUpstream(t)
	up.blockDelete = make(chan struct{})
	up.deleteStarted = make(chan struct{}, 1)
	paths := handoffPaths(t)
	oldCfg := &config.Config{MCPServers: map[string]*config.Server{
		"remote": {Name: "remote", URL: up.URL + "/mcp"},
	}}
	old := startOldServer(t, paths, oldCfg)
	p, ok := old.Registry().Pool("remote")
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

	// The successor's configuration no longer has the server, so the
	// session is orphaned by the takeover.
	successor, err := NewServer(handoffOptions(paths, &config.Config{MCPServers: map[string]*config.Server{}}, "new"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(successor.reg.Close)
	h, err := successor.TakeOver()
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	defer func() { _ = h.Done() }()

	select {
	case <-up.deleteStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("no orphan DELETE was started in the background after the takeover")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- successor.shutdown() }()
	select {
	case err := <-stopped:
		t.Fatalf("shutdown returned (%v) while the orphan DELETE was still blocked; the session would leak", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(up.blockDelete)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not return after the orphan DELETE completed")
	}
	if deletes, _ := up.counts(); deletes != 1 {
		t.Fatalf("upstream saw %d DELETE(s), want exactly 1", deletes)
	}
}

// #416: a daemon restart re-executes in place, and syscall.Exec would
// kill a background orphan DELETE still in flight — the same leak #409
// closed for shutdown, on the restart path. handleDaemonRestart waits
// for the termination before exec; the upstream still sees exactly one
// DELETE.
func TestDaemonRestartWaitsForABlockedOrphanTermination(t *testing.T) {
	up := newOrphanUpstream(t)
	up.blockDelete = make(chan struct{})
	up.deleteStarted = make(chan struct{}, 1)
	paths := handoffPaths(t)
	oldCfg := &config.Config{MCPServers: map[string]*config.Server{
		"remote": {Name: "remote", URL: up.URL + "/mcp"},
	}}
	old := startOldServer(t, paths, oldCfg)
	p, ok := old.Registry().Pool("remote")
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

	// The successor's configuration no longer has the server, so the
	// session is orphaned by the takeover.
	successor, err := NewServer(handoffOptions(paths, &config.Config{MCPServers: map[string]*config.Server{}}, "new"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(successor.reg.Close)
	h, err := successor.TakeOver()
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	defer func() { _ = h.Done() }()

	select {
	case <-up.deleteStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("no orphan DELETE was started in the background after the takeover")
	}

	execCalled := make(chan struct{}, 1)
	oldExec := execSelf
	execSelf = func(string, []string, []string) error {
		select {
		case execCalled <- struct{}{}:
		default:
		}
		return errors.New("exec is disabled in tests")
	}
	t.Cleanup(func() { execSelf = oldExec })

	// The handler SIGTERMs this process when the (stubbed) exec fails;
	// intercept it like TestHandleDaemonRestart does.
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	defer signal.Stop(term)

	w := httptest.NewRecorder()
	successor.handleDaemonRestart(w, httptest.NewRequest(http.MethodPost, "/v1/daemon/restart", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	select {
	case <-execCalled:
		t.Fatal("restart re-executed while the orphan DELETE was still blocked; the session would leak")
	case <-time.After(400 * time.Millisecond):
	}
	close(up.blockDelete)
	select {
	case <-execCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("restart did not re-execute after the orphan DELETE completed")
	}
	// Drain the fallback SIGTERM so it cannot land in a later test.
	select {
	case <-term:
	case <-time.After(2 * time.Second):
		t.Fatal("the restart handler never finished")
	}
	if deletes, _ := up.counts(); deletes != 1 {
		t.Fatalf("upstream saw %d DELETE(s), want exactly 1", deletes)
	}
}

// #409: the orphan wait is bounded. A termination that outlives the bound
// is logged and the exit proceeds; a daemon that never took over waits not
// at all.
func TestWaitOrphanTerminationsIsBoundedAndSkipsNonSuccessors(t *testing.T) {
	old := shutdownOrphanWait
	shutdownOrphanWait = 100 * time.Millisecond
	t.Cleanup(func() { shutdownOrphanWait = old })

	srv, err := NewServer(handoffOptions(handoffPaths(t), &config.Config{MCPServers: map[string]*config.Server{}}, "v"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.reg.Close)

	start := time.Now()
	srv.waitOrphanTerminations()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("a daemon that never took over waited %v for orphan terminations", elapsed)
	}

	ad := &Adoption{}
	ad.orphanWG.Add(1) // a termination that never finishes
	srv.adoption.Store(ad)
	start = time.Now()
	srv.waitOrphanTerminations()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the orphan wait was not bounded: still blocked after %v", elapsed)
	}
	ad.orphanWG.Done()
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
