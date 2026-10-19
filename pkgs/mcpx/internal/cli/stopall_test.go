package cli

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// writeDaemonRecord plants a daemon record for a live process (the test's
// own, so PruneStale keeps it) in the given state directory.
func writeDaemonRecord(t *testing.T, dir, socket, configPath string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(daemon.Info{
		PID: os.Getpid(), Socket: socket, ConfigPath: configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// customStateApp returns an App whose own state directory is a scratch dir,
// with the default state directory redirected under a scratch XDG home, so
// the only daemon record on the machine is the one the test plants in the
// default directory.
func customStateApp(t *testing.T) (a *App, defaultDir string) {
	t.Helper()
	// The default dir holds the daemon socket the stop-all test binds, so
	// it must come from a directory short enough for a unix socket path
	// however deep TMPDIR is (#434) -- t.TempDir() under a nix sandbox
	// build's TMPDIR exceeds Darwin's 104-byte sockaddr_un limit.
	xdg := testsupport.SocketDir(t)
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(xdg, "cache"))
	t.Setenv("MCPX_STATE_DIR", "")
	t.Setenv("MCPX_CACHE_DIR", "")
	defaultDir = filepath.Join(xdg, "mcpx")
	a = &App{Paths: daemon.PathsAt(filepath.Join(t.TempDir(), "custom"), "")}
	return a, defaultDir
}

// A daemon found in another state directory must be listed with a complete
// Paths rooted in that directory -- before the fix allDaemons built
// Paths{State: dir} with Info/Cache empty, and stopAll ignored even that
// and used the caller's paths (developing-today/code#316).
func TestAllDaemonsCarriesCompletePathsForFoundDir(t *testing.T) {
	a, defaultDir := customStateApp(t)
	writeDaemonRecord(t, defaultDir, filepath.Join(defaultDir, "daemon.sock"), "/cfg/a.json")

	listings := a.allDaemons()
	if len(listings) != 1 {
		t.Fatalf("allDaemons returned %d listings, want 1", len(listings))
	}
	p := listings[0].paths
	if p.State != defaultDir {
		t.Errorf("listing paths.State = %q, want %q", p.State, defaultDir)
	}
	if want := filepath.Join(defaultDir, "daemon.json"); p.Info != want {
		t.Errorf("listing paths.Info = %q, want %q", p.Info, want)
	}
	if p.Cache == "" {
		t.Error("listing paths.Cache is empty, want a complete Paths")
	}
}

// stop --all must actually stop a daemon whose record lives only in the
// default state directory, not the caller's own.
func TestStopAllStopsDaemonFoundInDefaultDir(t *testing.T) {
	a, defaultDir := customStateApp(t)
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(defaultDir, "daemon.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var shutdowns atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/shutdown", func(w http.ResponseWriter, r *http.Request) {
		shutdowns.Add(1)
		_, _ = w.Write([]byte(`{}`))
		// Stop answering shortly after replying, like a daemon going down,
		// so waitUntilStopped sees the socket refuse.
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = ln.Close()
		}()
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	writeDaemonRecord(t, defaultDir, socket, "/cfg/a.json")

	if err := a.stopAll(context.Background()); err != nil {
		t.Fatalf("stopAll: %v", err)
	}
	if got := shutdowns.Load(); got != 1 {
		t.Fatalf("fake daemon received %d shutdowns, want 1", got)
	}
}
