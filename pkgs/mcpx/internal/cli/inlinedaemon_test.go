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

// inlineDaemonApp builds an App whose configuration declares no servers,
// so its in-process daemon has nothing to spawn and should answer at once.
func inlineDaemonApp(t *testing.T) *App {
	t.Helper()
	root := testsupport.TempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	cfg := filepath.Join(root, "mcpx.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &App{ConfigPath: cfg, Paths: daemon.PathsAt(root, "")}
}

// A context that is already cancelled must not start an in-process daemon
// and then wait out the full inline start timeout (5s by default): Ping is
// ctx-bound, so it could never succeed, and the old code reported the
// generic "did not answer" error instead of the cancellation (#430, the
// inline counterpart of the #233 EnsureDaemon fix).
func TestInlineDaemonAlreadyCancelledContext(t *testing.T) {
	a := inlineDaemonApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := a.inlineDaemon(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("inlineDaemon(cancelled ctx) = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("inlineDaemon(cancelled ctx) took %v, want an immediate return", elapsed)
	}
	if a.inlineStop != nil {
		t.Fatal("inlineDaemon started an in-process daemon despite the cancelled context")
	}
}

// The uncancelled path is unchanged: the in-process daemon starts and the
// returned client can ping it.
func TestInlineDaemonStartsWithLiveContext(t *testing.T) {
	a := inlineDaemonApp(t)
	ctx := context.Background()
	c, err := a.inlineDaemon(ctx)
	if err != nil {
		t.Fatalf("inlineDaemon(live ctx): %v", err)
	}
	defer a.CloseInline()
	if !c.Ping(ctx) {
		t.Fatal("the client returned by inlineDaemon does not ping its daemon")
	}
}
