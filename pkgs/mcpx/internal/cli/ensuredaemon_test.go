package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// ensureDaemonClient builds a local client whose daemon socket does not
// exist, and whose autostart binary is a script that records its own
// invocation in marker before exiting without ever serving the socket.
func ensureDaemonClient(t *testing.T, marker, connectTimeout string) *Client {
	t.Helper()
	root := testsupport.TempDir(t)
	script := filepath.Join(root, "fake-daemon.sh")
	body := "#!/bin/sh\ntouch " + marker + "\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	paths := daemon.Paths{
		State:  filepath.Join(root, "state"),
		Cache:  filepath.Join(root, "cache"),
		Socket: filepath.Join(root, "state", "daemon.sock"),
	}
	c := NewClient(paths, "")
	if err := c.set.SetRuntime("autostart.bin", script); err != nil {
		t.Fatal(err)
	}
	if err := c.set.SetRuntime("autostart.connectTimeout", connectTimeout); err != nil {
		t.Fatal(err)
	}
	if err := c.set.SetRuntime("autostart.pollInterval", "10ms"); err != nil {
		t.Fatal(err)
	}
	return c
}

// A context that is already cancelled must not be read as "no daemon":
// EnsureDaemon used to spawn a daemon anyway and then poll for the full
// autostart.connectTimeout (20s by default) before unwinding (#233).
func TestEnsureDaemonAlreadyCancelledContext(t *testing.T) {
	root := testsupport.TempDir(t)
	marker := filepath.Join(root, "spawned")
	c := ensureDaemonClient(t, marker, "3s")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := c.EnsureDaemon(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureDaemon(cancelled ctx) = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("EnsureDaemon(cancelled ctx) took %v, want an immediate return", elapsed)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("EnsureDaemon spawned a daemon despite the cancelled context")
	}
}

// Cancellation that lands while EnsureDaemon is polling for the daemon it
// started must unwind at once, not after the connect timeout.
func TestEnsureDaemonCancelWhilePolling(t *testing.T) {
	root := testsupport.TempDir(t)
	marker := filepath.Join(root, "spawned")
	c := ensureDaemonClient(t, marker, "30s")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := c.EnsureDaemon(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureDaemon(cancelled while polling) = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("EnsureDaemon took %v after cancellation, want a prompt return", elapsed)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatal("expected the daemon spawn to have happened before cancellation")
	}
}

// A cancelled context against a remote endpoint reports the cancellation,
// not the generic "no daemon there" error.
func TestEnsureDaemonRemoteCancelledContext(t *testing.T) {
	c := NewClientAt(daemon.Paths{}, "", "http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.EnsureDaemon(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureDaemon(remote, cancelled ctx) = %v, want context.Canceled", err)
	}
}
