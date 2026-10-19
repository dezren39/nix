package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	// The spawn is cmd.Start + Release (client.go): Start returns once the
	// process exists, before the child has run any code, and this test's
	// whole point is that EnsureDaemon returns the instant cancellation
	// lands — so the child's touch can legitimately land after the return.
	// Asserting the marker instantly races the child's scheduling (the
	// flake that got this test skipped in the darwin nix build, #428).
	// The child is Setsid and Released, so it still runs: wait for the
	// marker instead, and fail only if it never appears.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, statErr := os.Stat(marker); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected the daemon spawn to have happened before cancellation")
		}
		time.Sleep(10 * time.Millisecond)
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

// The uncancelled path is unchanged: a daemon that never becomes ready
// still fails with "did not become ready" after the connect timeout.
func TestEnsureDaemonNeverReadyStillTimesOut(t *testing.T) {
	root := testsupport.TempDir(t)
	marker := filepath.Join(root, "spawned")
	c := ensureDaemonClient(t, marker, "1s")
	start := time.Now()
	err := c.EnsureDaemon(context.Background())
	if err == nil || !strings.Contains(err.Error(), "daemon did not become ready") {
		t.Fatalf("err = %v, want the did-not-become-ready timeout error", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("gave up after %v, before the 1s connect timeout", elapsed)
	}
	// The spawn is cmd.Start + Release (client.go): Start returns once the
	// process exists, before the child has run any code, so the child's
	// touch can legitimately land after EnsureDaemon returns at the
	// connect timeout. Asserting the marker instantly races the child's
	// scheduling — the same flake as #428, in the test #437 added without
	// that pattern (#445). The child is Setsid and Released, so it still
	// runs: wait for the marker instead, and fail only if it never
	// appears.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, statErr := os.Stat(marker); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake daemon was never spawned")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// errAfterContext reports context.Canceled from Err once its deadline
// passes, but its Done channel never fires. It models a cancellation that
// only becomes visible as a poll loop exits on its own deadline: an
// in-loop select on Done cannot catch it, only a post-loop Err check can.
type errAfterContext struct {
	context.Context
	at time.Time
}

func (c errAfterContext) Done() <-chan struct{} { return nil }

func (c errAfterContext) Err() error {
	if time.Now().Before(c.at) {
		return nil
	}
	return context.Canceled
}

// The post-loop ctx.Err() check (in this copy since #233) had no test
// pinning it: a cancellation visible only at the poll loop's deadline
// exit must report context.Canceled, not "daemon did not become ready".
// Filed as developing-today/code#328, whose copy lacked the check itself.
func TestEnsureDaemonCancelVisibleOnlyAtDeadline(t *testing.T) {
	root := testsupport.TempDir(t)
	marker := filepath.Join(root, "spawned")
	c := ensureDaemonClient(t, marker, "300ms")
	ctx := errAfterContext{Context: context.Background(), at: time.Now().Add(100 * time.Millisecond)}
	err := c.EnsureDaemon(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureDaemon(cancel visible only at deadline) = %v, want context.Canceled", err)
	}
}
