package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// execTick is the exec-watch interval these tests run at. The production
// default is 30s, which would make each case take minutes.
const execTick = 20 * time.Millisecond

// setExecWatch runs the exec watch at interval d for the pools a test makes,
// and puts the default back when the test ends.
func setExecWatch(t *testing.T, d time.Duration) {
	t.Helper()
	old := pool.ExecCheckInterval
	pool.ExecCheckInterval = d
	t.Cleanup(func() { pool.ExecCheckInterval = old })
}

// lifecycleLog records the events the pools emit for the length of a test.
type lifecycleLog struct {
	mu     sync.Mutex
	events []lifecycleEvent
}

type lifecycleEvent struct {
	name  string
	attrs map[string]any
}

func recordLifecycle(t *testing.T) *lifecycleLog {
	t.Helper()
	log := &lifecycleLog{}
	old := pool.Lifecycle
	pool.Lifecycle = func(name string, attrs map[string]any) {
		log.mu.Lock()
		defer log.mu.Unlock()
		log.events = append(log.events, lifecycleEvent{name, attrs})
	}
	t.Cleanup(func() { pool.Lifecycle = old })
	return log
}

// count reports how many events named name for server carry every attribute
// in want. A string attribute matches by prefix, so a long reason can be
// named by its opening words.
func (l *lifecycleLog) count(name, server string, want map[string]string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if e.name != name || fmt.Sprint(e.attrs["server"]) != server {
			continue
		}
		match := true
		for k, prefix := range want {
			if !strings.HasPrefix(fmt.Sprint(e.attrs[k]), prefix) {
				match = false
				break
			}
		}
		if match {
			n++
		}
	}
	return n
}

// livePID is the pid of the child a pool serves the global key with, or 0. It
// starts nothing, so it sees what the exec watch left running.
func livePID(p *pool.Pool) int {
	for _, in := range p.Status().Instances {
		if in.Key == "global" && in.PID > 0 {
			return in.PID
		}
	}
	return 0
}

// servedPID asks the pool's child which process answers. Unlike livePID this
// goes through a real call, so it shows the child is serving.
func servedPID(t *testing.T, p *pool.Pool) int {
	t.Helper()
	raw, err := p.Call(context.Background(), "global", "state", map[string]any{})
	if err != nil {
		t.Fatalf("state call on %s: %v", p.Name(), err)
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || len(res.Content) == 0 {
		t.Fatalf("decode state result %s: %v", raw, err)
	}
	var st struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &st); err != nil {
		t.Fatalf("decode state text %q: %v", res.Content[0].Text, err)
	}
	return st.PID
}

// waitFor polls cond until it holds, and fails the test if d passes first.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s (no change within %s)", what, d)
		}
		time.Sleep(time.Millisecond)
	}
}

// exeBytes is the program a running child is executing, read through /proc. A
// child keeps the file it was started from even after that path is replaced,
// so this is what tells the old program from the new one.
func exeBytes(t *testing.T, pid int) []byte {
	t.Helper()
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		t.Fatalf("read /proc/%d/exe: %v", pid, err)
	}
	return b
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// Replacing a live pool's executable at its path restarts that pool within
// a few checks, and the new child runs the new program. The pool beside it
// keeps its child.
func TestExecChangeRestartsThatPoolAndNoOther(t *testing.T) {
	setExecWatch(t, execTick)
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-server")
	copyProgram(t, fake, bin, nil)
	log := recordLifecycle(t)

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

	replaceProgram(t, fake, bin, []byte("new build"))
	changed := time.Now()
	var pidB2 int
	waitFor(t, 2*time.Second, "pool b kept its child after its executable was replaced", func() bool {
		pidB2 = livePID(pb)
		return pidB2 != 0 && pidB2 != pidB
	})
	t.Logf("pool b restarted %s after its executable was replaced (pid %d -> %d, check every %s)",
		time.Since(changed).Round(time.Millisecond), pidB, pidB2, execTick)

	if !bytes.Equal(exeBytes(t, pidB2), mustRead(t, bin)) {
		t.Fatal("the restarted child is not running the replaced program")
	}
	if got := servedPID(t, pb); got != pidB2 {
		t.Fatalf("the call was served by pid %d, not the restarted child %d", got, pidB2)
	}
	if got := livePID(pa); got != pidA {
		t.Fatalf("the neighbour pool was restarted: pid %d -> %d", pidA, got)
	}
	if n := log.count("server.restart", "a", nil); n != 0 {
		t.Fatalf("the neighbour pool logged %d restarts", n)
	}
	if n := log.count("server.restart", "b", map[string]string{"reason": "executable changed"}); n != 1 {
		t.Fatalf("pool b logged %d restarts for executable changed, want 1", n)
	}
	waitGone(t, pidB)
}

// Negative control for the test above: with the check off the same change is
// never noticed, and this is the failure the check exists to prevent. It is
// kept as a test so the switch is known to turn the check off.
func TestExecWatchOffWhenTheIntervalIsZero(t *testing.T) {
	setExecWatch(t, 0)
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-server")
	copyProgram(t, fake, bin, nil)
	cfg := &config.Config{MCPServers: map[string]*config.Server{"b": {Name: "b", Command: bin}}}
	r, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pb, _ := r.Pool("b")
	pid := childPID(t, pb)

	replaceProgram(t, fake, bin, []byte("new build"))
	time.Sleep(10 * execTick)
	if got := livePID(pb); got != pid {
		t.Fatalf("with the check off, a changed executable restarted pid %d -> %d", pid, got)
	}
}

// A symlink re-pointed at a byte-identical file keeps its child. Re-pointed at
// different bytes, it restarts. Both are seen while the pool is live.
func TestExecWatchFollowsARepointedLinkByWhatItRuns(t *testing.T) {
	setExecWatch(t, execTick)
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	other := filepath.Join(dir, "other")
	link := filepath.Join(dir, "mcp-server")
	writeWrapper(t, fake, first, "same program")
	writeWrapper(t, fake, second, "same program")
	writeWrapper(t, fake, other, "a different program")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{MCPServers: map[string]*config.Server{"b": {Name: "b", Command: link}}}
	r, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pb, _ := r.Pool("b")
	pid := childPID(t, pb)

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
	time.Sleep(10 * execTick)
	if got := livePID(pb); got != pid {
		t.Fatalf("a link re-pointed at byte-identical bytes restarted its pool: pid %d -> %d", pid, got)
	}

	repoint(other)
	waitFor(t, 2*time.Second, "a link re-pointed at different bytes kept its child", func() bool {
		got := livePID(pb)
		return got != 0 && got != pid
	})
	waitGone(t, pid)
}

// A child whose executable is deleted keeps serving, with one warning however
// many checks see the gap. When the file returns, the child is kept if the
// bytes are the same and replaced if they are not.
func TestExecWatchKeepsAChildWhoseExecutableIsGone(t *testing.T) {
	setExecWatch(t, execTick)
	for _, tc := range []struct {
		name string
		// returnTag is the tag of the file that comes back at the path.
		returnTag string
		restart   bool
	}{
		{"same bytes come back", "original", false},
		{"different bytes come back", "a different program", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := testsupport.FakeMCPBinary(t)
			dir := t.TempDir()
			bin := filepath.Join(dir, "mcp-server")
			writeWrapper(t, fake, bin, "original")
			log := recordLifecycle(t)
			cfg := &config.Config{MCPServers: map[string]*config.Server{"b": {Name: "b", Command: bin}}}
			r, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			pb, _ := r.Pool("b")
			pid := childPID(t, pb)

			if err := os.Remove(bin); err != nil {
				t.Fatal(err)
			}
			time.Sleep(10 * execTick)
			if !alive(pid) || servedPID(t, pb) != pid {
				t.Fatalf("the child stopped serving while its executable was gone")
			}
			if n := log.count("server.warning", "b", map[string]string{"reason": "executable missing"}); n != 1 {
				t.Fatalf("%d warnings while the executable was gone, want exactly 1", n)
			}

			writeWrapper(t, fake, bin, tc.returnTag)
			if tc.restart {
				waitFor(t, 2*time.Second, "different bytes came back but the child was kept", func() bool {
					got := livePID(pb)
					return got != 0 && got != pid
				})
			} else {
				waitFor(t, 2*time.Second, "the same bytes came back but nothing noticed", func() bool {
					return log.count("server.exec", "b", map[string]string{"state": "restored"}) > 0
				})
				if got := livePID(pb); got != pid {
					t.Fatalf("the same bytes came back but the child was replaced: pid %d -> %d", pid, got)
				}
				if got := servedPID(t, pb); got != pid {
					t.Fatalf("after the file returned, the call was served by pid %d, not %d", got, pid)
				}
			}
			if n := log.count("server.warning", "b", map[string]string{"reason": "executable missing"}); n != 1 {
				t.Fatalf("%d warnings in all, want exactly 1", n)
			}
		})
	}
}
