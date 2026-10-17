package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// The promise under test: a call issued while a takeover is under way either
// completes on the old daemon or succeeds on the new one. A transport error is
// the failure this guards against -- a refused connection, a reset, an EOF on
// a kept-alive connection the old daemon closed as it drained -- because the
// client has no way to retry a call it cannot tell was never received.

// handoffPaths is a state directory whose socket both daemons share, as they
// do in production: the successor takes the old daemon's listener, it does
// not bind a new one.
func handoffPaths(t *testing.T) Paths {
	t.Helper()
	dir, err := os.MkdirTemp("", "mxh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return Paths{State: dir, Cache: dir, Socket: filepath.Join(dir, "d.sock"), Info: filepath.Join(dir, "d.json")}
}

func handoffOptions(paths Paths, cfg *config.Config, version string) Options {
	return Options{
		Config:  cfg,
		Paths:   paths,
		Version: version,
		Logger:  log.New(io.Discard, "", 0),
	}
}

// cliStyleClient talks to the socket the way the CLI does: one transport,
// kept alive between calls, so the test exercises connection reuse across the
// handoff rather than a fresh connection per request.
func cliStyleClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		IdleConnTimeout: 90 * time.Second,
	}}
}

// hammer runs workers that issue calls until stop closes, and returns the
// counts and the first failure. A call counts as failed only when no HTTP
// response came back.
type hammerResult struct {
	ok, failed atomic.Int64
	mu         sync.Mutex
	firstErr   error
}

func (h *hammerResult) fail(err error) {
	h.failed.Add(1)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.firstErr == nil {
		h.firstErr = err
	}
}

func hammer(client *http.Client, workers int, stop <-chan struct{}, res *hammerResult) *sync.WaitGroup {
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := "load-" + string(rune('a'+i))
			for {
				select {
				case <-stop:
					return
				default:
				}
				body, _ := json.Marshal(map[string]any{
					"server": "a", "tool": "echo", "session": session,
					"args": map[string]any{"text": "during handoff"},
				})
				resp, err := client.Post("http://mcpx/v1/call", "application/json", bytes.NewReader(body))
				if err != nil {
					res.fail(err)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				res.ok.Add(1)
			}
		}(i)
	}
	return &wg
}

func TestCallsIssuedDuringATakeoverNeverFail(t *testing.T) {
	paths := handoffPaths(t)
	fake := testsupport.FakeMCPBinary(t)
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: fake},
	}}

	old, err := NewServer(handoffOptions(paths, cfg, "old"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(old.reg.Close)
	if err := old.Listen(0); err != nil {
		t.Fatal(err)
	}
	old.startHTTP()
	t.Cleanup(func() { _ = old.httpSrv.Close() })

	client := cliStyleClient(paths.Socket)
	res := &hammerResult{}
	stop := make(chan struct{})
	wg := hammer(client, 12, stop, res)

	// Let calls build up on kept-alive connections, then hand over while they
	// are still arriving.
	time.Sleep(200 * time.Millisecond)
	successor, err := NewServer(handoffOptions(paths, cfg, "new"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(successor.reg.Close)
	h, err := successor.TakeOver()
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	_ = h.Done()

	// Keep the load on after the handoff, so calls land on the successor.
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	if res.failed.Load() > 0 {
		t.Fatalf("%d of %d calls failed during the takeover; first: %v",
			res.failed.Load(), res.ok.Load()+res.failed.Load(), res.firstErr)
	}
	if res.ok.Load() == 0 {
		t.Fatal("no call was made")
	}
	t.Logf("%d calls, none failed", res.ok.Load())
}

// instancePID is the pid of the child a pool holds for the given lease key, or
// zero when it holds none.
func instancePID(p *pool.Pool, key string) int {
	for _, in := range p.Status().Instances {
		if in.Key == key && in.PID > 0 {
			return in.PID
		}
	}
	return 0
}

// A client's lease is the session it named: the child that session was given,
// and the record of it. Both must be the same after a takeover, and the record
// must be on disk, so a daemon started from the state directory keeps it too.
func TestAClientLeaseSurvivesADaemonTakeover(t *testing.T) {
	paths := handoffPaths(t)
	fake := testsupport.FakeMCPBinary(t)
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: fake, Mcpx: &config.Extras{Sharing: "exclusive", Scope: "session"}},
	}}

	old, err := NewServer(handoffOptions(paths, cfg, "old"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(old.reg.Close)
	if err := old.Listen(0); err != nil {
		t.Fatal(err)
	}
	old.startHTTP()
	t.Cleanup(func() { _ = old.httpSrv.Close() })

	client := cliStyleClient(paths.Socket)
	const session = "lease-x"
	body, _ := json.Marshal(map[string]any{
		"server": "a", "tool": "echo", "session": session,
		"args": map[string]any{"text": "before"},
	})
	resp, err := client.Post("http://mcpx/v1/call", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the call that makes the lease: %s", resp.Status)
	}

	before, ok := old.reg.snapshotLeases()[session]
	if !ok {
		t.Fatal("the call did not create a lease for its session")
	}
	pa, _ := old.reg.Pool("a")
	pid := instancePID(pa, "session:"+session)
	if pid == 0 {
		t.Fatalf("the session has no child before the takeover: %+v", pa.Status().Instances)
	}

	successor, err := NewServer(handoffOptions(paths, cfg, "new"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(successor.reg.Close)
	h, err := successor.TakeOver()
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	_ = h.Done()

	after, ok := successor.reg.snapshotLeases()[session]
	if !ok {
		t.Fatal("the lease did not survive the takeover")
	}
	if !after.LastSeen.Equal(before.LastSeen) {
		t.Fatalf("the lease was last seen %v before the takeover and %v after", before.LastSeen, after.LastSeen)
	}
	pa2, _ := successor.reg.Pool("a")
	if got := instancePID(pa2, "session:"+session); got != pid {
		t.Fatalf("the session's child changed across the takeover: pid %d -> %d", pid, got)
	}

	b, err := os.ReadFile(filepath.Join(paths.State, "sessions.json"))
	if err != nil {
		t.Fatalf("the lease was not written to disk: %v", err)
	}
	if !bytes.Contains(b, []byte(`"`+session+`"`)) {
		t.Fatalf("sessions.json does not hold the session: %s", b)
	}
}
