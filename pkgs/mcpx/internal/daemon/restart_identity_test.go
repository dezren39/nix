package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// An operator's restart of one server replaces its child even though the
// program at its path is byte-identical, so the pid changes. The server beside
// it is not touched. This is the forced path; the automatic check keeps the
// child (see TestReloadFollowsAForRePointedCommandByWhatItRuns).
func TestRestartOfAServerWhoseBinaryIsUnchangedGetsANewChild(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-server")
	copyProgram(t, fake, bin, nil)
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: fake},
		"b": {Name: "b", Command: bin},
	}}
	r, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pa, _ := r.Pool("a")
	pb, _ := r.Pool("b")
	pidA, pidB := childPID(t, pa), childPID(t, pb)
	identity := pb.ExecIdentity()

	if _, err := r.Restart(context.Background(), "b", false); err != nil {
		t.Fatal(err)
	}
	pb2, _ := r.Pool("b")
	if got := childPID(t, pb2); got == pidB {
		t.Fatalf("the restart of an identical binary kept child pid %d", pidB)
	}
	if got := pb2.ExecIdentity(); got != identity {
		t.Fatalf("the restart moved the identity of an unchanged binary: %q -> %q", identity, got)
	}
	waitGone(t, pidB)

	if pa2, _ := r.Pool("a"); pa2 != pa || childPID(t, pa2) != pidA {
		t.Fatal("restarting one server restarted its neighbour")
	}
}
