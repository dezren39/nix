package daemon

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
)

func TestLeasesSurviveARestartInA0600File(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{State: dir, Cache: dir, Socket: filepath.Join(dir, "d.sock"), Info: filepath.Join(dir, "d.json")}
	cfg := &config.Config{MCPServers: map[string]*config.Server{}}
	newReg := func() *Registry {
		r, err := NewRegistry(cfg, paths, log.New(io.Discard, "", 0).Printf)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(r.Close)
		return r
	}

	first := newReg()
	seen := time.Now().Truncate(time.Second)
	first.adoptLeases(map[string]leaseRecord{
		"caller-1": {LastSeen: seen, Owned: map[string]string{"demo": "session:caller-1"}},
	})
	if err := first.SaveSessions(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("sessions.json is mode %o, want 0600", mode)
	}

	restarted := newReg()
	got, ok := restarted.snapshotLeases()["caller-1"]
	if !ok {
		t.Fatal("the lease did not survive a restart")
	}
	if got.Owned["demo"] != "session:caller-1" || !got.LastSeen.Equal(seen) {
		t.Fatalf("the restored lease is %+v", got)
	}
}
