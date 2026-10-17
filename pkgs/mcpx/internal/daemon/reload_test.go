package daemon

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// childPID starts (or finds) the global instance of p and reports its pid.
func childPID(t *testing.T, p *pool.Pool) int {
	t.Helper()
	lease, err := p.Acquire(context.Background(), "global")
	if err != nil {
		t.Fatalf("acquire %s: %v", p.Name(), err)
	}
	lease.Release()
	for _, in := range p.Status().Instances {
		if in.Key == "global" && in.PID > 0 {
			return in.PID
		}
	}
	t.Fatalf("no global instance for %s", p.Name())
	return 0
}

func nsExtras(ns string) *config.Extras {
	if ns == "" {
		return nil
	}
	return &config.Extras{Namespace: ns}
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	// stdio shutdown is stdinGrace + termGrace + killWait in the worst case
	deadline := time.Now().Add(15 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still running after reload", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReloadKeepsUnchangedChildAndReplacesChangedOne(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	// b starts with args that differ from a: identical definitions share a pool.
	cfgOf := func(bArgs []string) *config.Config {
		return &config.Config{MCPServers: map[string]*config.Server{
			"a": {Name: "a", Command: fake},
			"b": {Name: "b", Command: fake, Args: bArgs},
		}}
	}
	r, err := NewRegistry(cfgOf([]string{"--initial"}), Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	pa, _ := r.Pool("a")
	pb, _ := r.Pool("b")
	pidA, pidB := childPID(t, pa), childPID(t, pb)

	if _, _, err := r.Reload(cfgOf([]string{"--changed"})); err != nil {
		t.Fatal(err)
	}
	pa2, _ := r.Pool("a")
	pb2, _ := r.Pool("b")
	if pa2 != pa {
		t.Fatalf("unchanged server a got a new pool")
	}
	if got := childPID(t, pa2); got != pidA {
		t.Fatalf("unchanged server a restarted: pid %d -> %d", pidA, got)
	}
	if pb2 == pb {
		t.Fatalf("changed server b kept its old pool")
	}
	if got := childPID(t, pb2); got == pidB {
		t.Fatalf("changed server b kept its old child pid %d", pidB)
	}
	waitGone(t, pidB)
}

func TestReloadRenamedNamespaceKeepsChildAndResolvesNewName(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	cfgOf := func(ns string) *config.Config {
		return &config.Config{Path: filepath.Join(dir, "mcp.json"), MCPServers: map[string]*config.Server{
			"a": {Name: "a", Command: fake, Mcpx: nsExtras(ns)},
		}}
	}
	r, err := NewRegistry(cfgOf("alpha"), Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	p, _ := r.Pool("alpha")
	pid := childPID(t, p)

	if _, _, err := r.Reload(cfgOf("beta")); err != nil {
		t.Fatal(err)
	}
	got, ok := r.Pool("beta")
	if !ok {
		t.Fatal(`namespace "beta" does not resolve after reload`)
	}
	if got != p {
		t.Fatal("renaming a namespace replaced an unchanged server's pool")
	}
	if childPID(t, got) != pid {
		t.Fatalf("renaming a namespace restarted the child")
	}
}
