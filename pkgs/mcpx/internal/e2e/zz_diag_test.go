package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestZZDiagStreaming is a temporary diagnostic, not a test to keep. It
// reproduces the pre-#248 shape of TestAStreamingCommandWritesToTheFileItWasGiven
// and always fails, so that `go test` without -v still prints what it found.
func TestZZDiagStreaming(t *testing.T) {
	var log strings.Builder
	say := func(f string, a ...any) { fmt.Fprintf(&log, f+"\n", a...) }

	e := newEnv(t, oneServer)
	e.run("ls")
	out := filepath.Join(e.dir, "diag-events.ndjson")
	stateDir := filepath.Join(e.dir, "state")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.mcpx, "events", "--kinds", "server", "-o", out)
	cmd.Dir, cmd.Env = e.dir, e.envVars
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill() }()

	deadline := time.Now().Add(30 * time.Second)
	start := time.Now()
	var got, exited string
	n := 0
	for time.Now().Before(deadline) {
		o, err := e.try("restart", "demo")
		n++
		if n <= 3 || err != nil {
			say("restart #%d at %s: err=%v out=%q", n, time.Since(start).Round(time.Millisecond), err, o)
		}
		select {
		case werr := <-done:
			exited = fmt.Sprintf("child exited after %s: %v", time.Since(start), werr)
		default:
		}
		if exited != "" {
			break
		}
		if b, rerr := os.ReadFile(out); rerr == nil && len(b) > 0 {
			got = string(b)
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	// Absent means the child never reached streamOp -- it was still inside
	// ensure(). Present-but-empty means it is streaming and nothing arrived.
	if st, err := os.Stat(out); err != nil {
		say("OUT FILE: MISSING (%v) -- the child never reached the stream", err)
	} else {
		say("OUT FILE: exists, %d bytes", st.Size())
	}
	say("iterations=%d elapsed=%s exited=%q", n, time.Since(start).Round(time.Millisecond), exited)
	say("child stderr=%q stdout=%q", stderr.String(), stdout.String())
	say("got=%q", got)

	if ents, err := os.ReadDir(stateDir); err == nil {
		for _, en := range ents {
			say("state entry: %s", en.Name())
		}
	} else {
		say("state dir unreadable: %v", err)
	}

	// What the daemon the test itself can reach actually published. A replay
	// from zero shows everything retained, so "no server events at all" and
	// "server events the subscriber never saw" are distinguishable.
	func() {
		defer func() {
			if r := recover(); r != nil {
				say("replay panicked: %v", r)
			}
		}()
		hc := e.socketClient(t)
		rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rcancel()
		req, _ := http.NewRequestWithContext(rctx, "GET", "http://unix/v1/events?since=0", nil)
		req.Header.Set("Accept", "text/event-stream")
		resp, err := hc.Do(req)
		if err != nil {
			say("replay request failed: %v", err)
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 3000))
		say("replay status=%d body:\n%s", resp.StatusCode, b)
	}()

	// And the same request with the filter the failing command used, to see
	// whether kinds= is what excludes everything.
	func() {
		defer func() {
			if r := recover(); r != nil {
				say("filtered replay panicked: %v", r)
			}
		}()
		hc := e.socketClient(t)
		rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rcancel()
		req, _ := http.NewRequestWithContext(rctx, "GET", "http://unix/v1/events?since=0&kinds=server", nil)
		req.Header.Set("Accept", "text/event-stream")
		resp, err := hc.Do(req)
		if err != nil {
			say("filtered replay request failed: %v", err)
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		say("filtered replay status=%d body:\n%s", resp.StatusCode, b)
	}()

	t.Fatalf("DIAGNOSTIC (always fails on purpose):\n%s", log.String())
}
