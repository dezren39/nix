package pool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// execTick is the exec-watch interval these tests run at.
const execTick = 20 * time.Millisecond

// setExecWatch sets the check interval and the drain window for the pools a
// test makes, and puts the defaults back when it ends.
func setExecWatch(t *testing.T, every, drain time.Duration) {
	t.Helper()
	oldEvery, oldDrain := pool.ExecCheckInterval, pool.ExecDrainTimeout
	pool.ExecCheckInterval, pool.ExecDrainTimeout = every, drain
	t.Cleanup(func() {
		pool.ExecCheckInterval, pool.ExecDrainTimeout = oldEvery, oldDrain
	})
}

// copyFake puts a fresh copy of the fake MCP server at path, the way an
// installer does: a new inode, renamed into place.
func copyFake(t *testing.T, fake, path string, extra []byte) {
	t.Helper()
	b, err := os.ReadFile(fake)
	if err != nil {
		t.Fatal(err)
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(b, extra...), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// busy reports whether a call is running on one of the pool's children.
func busy(p *pool.Pool) bool {
	for _, in := range p.Status().Instances {
		if in.Holders > 0 {
			return true
		}
	}
	return false
}

// childPIDs lists the pids of the pool's live children.
func childPIDs(p *pool.Pool) []int {
	var out []int
	for _, in := range p.Status().Instances {
		if in.PID > 0 {
			out = append(out, in.PID)
		}
	}
	return out
}

// servedPID asks the pool which child answers, through a real call.
func servedPID(t *testing.T, p *pool.Pool) int {
	t.Helper()
	res, err := p.Call(context.Background(), "global", "state", map[string]any{})
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	var st struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &st); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return st.PID
}

// awaitReplacement waits until the pool has a child other than old.
func awaitReplacement(t *testing.T, p *pool.Pool, old int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, pid := range childPIDs(p) {
			if pid != old {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no replacement for pid %d within 10s", old)
		}
		time.Sleep(time.Millisecond)
	}
}

// inFlight starts a slow call on the pool and returns its result when it
// ends. It waits until the call is actually running on a child, so the
// executable is replaced under a call that is in progress.
func inFlight(t *testing.T, p *pool.Pool, ms int) <-chan struct {
	raw json.RawMessage
	err error
} {
	t.Helper()
	done := make(chan struct {
		raw json.RawMessage
		err error
	}, 1)
	go func() {
		raw, err := p.Call(context.Background(), "global", "slow", map[string]any{"ms": ms})
		done <- struct {
			raw json.RawMessage
			err error
		}{raw, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !busy(p) {
		if time.Now().After(deadline) {
			t.Fatal("the slow call never started on a child")
		}
		time.Sleep(time.Millisecond)
	}
	return done
}

// A call in flight when the executable is replaced completes on the child it
// started on, because the drain window is longer than the call. The next call
// is served by the replacement.
func TestCallInFlightAcrossAnExecRestartCompletesOnTheOldChild(t *testing.T) {
	setExecWatch(t, execTick, 10*time.Second)
	fake := testsupport.FakeMCPBinary(t)
	bin := filepath.Join(t.TempDir(), "mcp-server")
	copyFake(t, fake, bin, nil)

	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	oldPID := servedPID(t, p)

	done := inFlight(t, p, 1000)
	copyFake(t, fake, bin, []byte("new build"))
	awaitReplacement(t, p, oldPID)

	r := <-done
	if r.err != nil {
		t.Fatalf("the call in flight during the restart failed: %v", r.err)
	}
	if want := fmt.Sprintf("on pid %d", oldPID); !strings.Contains(textOf(t, r.raw), want) {
		t.Fatalf("the call did not complete on the old child (want %q): %s", want, textOf(t, r.raw))
	}
	if got := servedPID(t, p); got == oldPID {
		t.Fatalf("the next call was served by the old child pid %d", got)
	}
}

// A call still running when the drain window ends is not left hanging, and is
// not silently lost: it fails with an error that says the server was restarted
// for a changed executable. The next call is served by the replacement.
func TestCallInFlightPastTheDrainFailsWithAClearError(t *testing.T) {
	setExecWatch(t, execTick, 200*time.Millisecond)
	fake := testsupport.FakeMCPBinary(t)
	bin := filepath.Join(t.TempDir(), "mcp-server")
	copyFake(t, fake, bin, nil)

	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	oldPID := servedPID(t, p)

	done := inFlight(t, p, 3000)
	copyFake(t, fake, bin, []byte("new build"))

	var r struct {
		raw json.RawMessage
		err error
	}
	select {
	case r = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the call in flight during the restart hung")
	}
	if r.err == nil {
		t.Fatalf("a call that outlived the drain window completed: %s", textOf(t, r.raw))
	}
	if !strings.Contains(r.err.Error(), "restarted for a changed executable") {
		t.Fatalf("the error does not say why the call failed: %v", r.err)
	}
	awaitReplacement(t, p, oldPID)
	if got := servedPID(t, p); got == oldPID || got == 0 {
		t.Fatalf("after the restart the call was served by pid %d (old child %d)", got, oldPID)
	}
}

// writeExecWrapper writes a small shell script that execs the fake server. Its
// identity is its content, so two wrappers with the same tag are the same
// program, and a different tag is a different one.
func writeExecWrapper(t *testing.T, fake, path, tag string) {
	t.Helper()
	body := fmt.Sprintf("#!/bin/sh\n# %s\nexec '%s' \"$@\"\n", tag, fake)
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// The periodic check does not restart a child when its link is re-pointed at a
// byte-identical program: the identity is the content, and the content did not
// change. The check is shown to be running by the second half: a link
// re-pointed at different bytes is replaced within the same window.
func TestExecCheckKeepsAChildWhenALinkIsRePointedAtIdenticalBytes(t *testing.T) {
	setExecWatch(t, execTick, 10*time.Second)
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	other := filepath.Join(dir, "other")
	link := filepath.Join(dir, "mcp-server")
	writeExecWrapper(t, fake, first, "same program")
	writeExecWrapper(t, fake, second, "same program")
	writeExecWrapper(t, fake, other, "a different program")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}

	p := pool.New(resolved(t, link, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	pid := servedPID(t, p)

	repoint := func(to string) {
		t.Helper()
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(to, link); err != nil {
			t.Fatal(err)
		}
	}
	repoint(second)
	// Many checks go by with the stamp moved.
	time.Sleep(20 * execTick)
	if got := servedPID(t, p); got != pid {
		t.Fatalf("a link re-pointed at identical bytes restarted its child: pid %d -> %d", pid, got)
	}

	repoint(other)
	awaitReplacement(t, p, pid)
}
