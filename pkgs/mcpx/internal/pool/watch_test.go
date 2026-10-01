package pool_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// subscribing builds a pool over a fake that declares resources.subscribe
// and logs every subscribe and unsubscribe it receives.
func subscribing(t *testing.T) (*pool.Pool, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "subs.log")
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"fake": {Name: "fake", Command: testsupport.FakeMCPBinary(t),
			Env:  map[string]string{"FAKEMCP_SUBSCRIBE": "1", "FAKEMCP_SUB_LOG": log},
			Mcpx: &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}},
	}}
	r, err := cfg.Resolve("fake")
	if err != nil {
		t.Fatal(err)
	}
	p := pool.New(r)
	t.Cleanup(p.Close)
	return p, log
}

// logged returns the fake's log lines as "<pid> <line>".
func logged(t *testing.T, log string) []string {
	t.Helper()
	b, _ := os.ReadFile(log)
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func countSuffix(lines []string, suffix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasSuffix(l, " "+suffix) {
			n++
		}
	}
	return n
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

func TestWatchCountsSubscribersPerURI(t *testing.T) {
	p, log := subscribing(t)
	ctx := context.Background()
	a, err := p.Watch(ctx, "global", "/abs/doc")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Watch(ctx, "global", "/abs/doc")
	if err != nil {
		t.Fatal(err)
	}
	if n := countSuffix(logged(t, log), "subscribe /abs/doc"); n != 1 {
		t.Fatalf("two watchers, %d upstream subscribes: %v", n, logged(t, log))
	}
	a()
	a() // release is idempotent: a second call must not take b's count
	if got := p.Watching()["/abs/doc"]; got != 1 {
		t.Fatalf("after one release: %d watchers", got)
	}
	if n := countSuffix(logged(t, log), "unsubscribe /abs/doc"); n != 0 {
		t.Fatalf("unsubscribed with a watcher left: %v", logged(t, log))
	}
	b()
	if n := countSuffix(logged(t, log), "unsubscribe /abs/doc"); n != 1 {
		t.Fatalf("last release did not unsubscribe: %v", logged(t, log))
	}
}

func TestWatchRefusesAServerWithoutSubscribe(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	if _, err := p.Watch(context.Background(), "global", "demo://greeting"); !errors.Is(err, pool.ErrNotSubscribable) {
		t.Fatalf("got %v", err)
	}
	if len(p.Watching()) != 0 {
		t.Fatalf("a refused watch was counted: %v", p.Watching())
	}
}

func TestWatchResubscribesAReplacementInstance(t *testing.T) {
	p, log := subscribing(t)
	release, err := p.Watch(context.Background(), "global", "demo://greeting")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	p.Restart(context.Background(), false)
	eventually(t, "a second process subscribed after the restart", func() bool {
		pids := map[string]bool{}
		for _, l := range logged(t, log) {
			if pid, rest, _ := strings.Cut(l, " "); rest == "subscribe demo://greeting" {
				pids[pid] = true
			}
		}
		return len(pids) == 2
	})
}

func TestAWatchedInstanceIsNotIdle(t *testing.T) {
	p, _ := subscribing(t)
	release, err := p.Watch(context.Background(), "global", "demo://greeting")
	if err != nil {
		t.Fatal(err)
	}
	if n := p.ReapIdle(time.Now().Add(24 * time.Hour)); n != 0 {
		t.Fatalf("reaped %d instances holding a subscription", n)
	}
	release()
	if n := p.ReapIdle(time.Now().Add(24 * time.Hour)); n != 1 {
		t.Fatalf("reaped %d after the last release, want 1", n)
	}
}
