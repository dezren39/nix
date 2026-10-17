package pool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// stopEvent is one server.stop lifecycle record: which process was closed,
// why, and when.
type stopEvent struct {
	pid    int
	reason string
	at     time.Time
}

// recordStops collects the server.stop events a test's pool emits, and puts
// the lifecycle hook back when the test ends.
func recordStops(t *testing.T) <-chan stopEvent {
	t.Helper()
	stops := make(chan stopEvent, 32)
	old := pool.Lifecycle
	pool.Lifecycle = func(event string, attrs map[string]any) {
		if event != "server.stop" {
			return
		}
		pid, _ := attrs["pid"].(int)
		reason, _ := attrs["reason"].(string)
		select {
		case stops <- stopEvent{pid: pid, reason: reason, at: time.Now()}:
		default:
		}
	}
	t.Cleanup(func() { pool.Lifecycle = old })
	return stops
}

// awaitStop waits until the process pid has been closed, and returns the
// event that says so. Waiting here also keeps the retiring goroutine from
// outliving the test's settings.
func awaitStop(t *testing.T, stops <-chan stopEvent, pid int) stopEvent {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-stops:
			if ev.pid == pid {
				return ev
			}
		case <-deadline:
			t.Fatalf("pid %d was never closed", pid)
		}
	}
}

// A lazy restart is not a cut-off: a call in flight when it starts finishes on
// the old child, and the restart does not wait for that call. The old child is
// closed only after the call ends, so the call succeeds.
func TestLazyRestartDoesNotFailAnInFlightCall(t *testing.T) {
	setExecWatch(t, execTick, 10*time.Second)
	stops := recordStops(t)
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	oldPID := servedPID(t, p)

	done := inFlight(t, p, 3000)
	start := time.Now()
	res := p.Restart(context.Background(), true)
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("a lazy restart waited %v for a call that runs for 3s", waited)
	}
	if res.Stopped != 1 || len(res.Started) != 0 || len(res.Failed) != 0 {
		t.Fatalf("lazy restart = %+v", res)
	}
	if res.Note == "" {
		t.Fatalf("a lazy restart with a call running should say so: %+v", res)
	}
	r := <-done
	if r.err != nil {
		t.Fatalf("the call in flight during a lazy restart failed: %v", r.err)
	}
	if want := fmt.Sprintf("on pid %d", oldPID); !strings.Contains(textOf(t, r.raw), want) {
		t.Fatalf("the call did not finish on the old child (want %q): %s", want, textOf(t, r.raw))
	}
	if ev := awaitStop(t, stops, oldPID); ev.reason != "restart" {
		t.Fatalf("the old child closed for %q, want restart", ev.reason)
	}
}

// A lazy restart sends new calls to a new child at once. The new call does not
// wait for the old child's call, and it is served by a process other than the
// old one.
func TestLazyRestartSendsNewCallsToANewChild(t *testing.T) {
	setExecWatch(t, execTick, 10*time.Second)
	stops := recordStops(t)
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	oldPID := servedPID(t, p)

	done := inFlight(t, p, 3000)
	p.Restart(context.Background(), true)
	newPID := servedPID(t, p)
	if newPID == 0 || newPID == oldPID {
		t.Fatalf("the call after a lazy restart was served by pid %d (old child %d)", newPID, oldPID)
	}
	select {
	case <-done:
		t.Fatal("a new call waited for the old child's call to finish")
	default:
	}
	if r := <-done; r.err != nil {
		t.Fatalf("the call in flight during a lazy restart failed: %v", r.err)
	}
	awaitStop(t, stops, oldPID)
	if got := servedPID(t, p); got != newPID {
		t.Fatalf("the replacement was not kept: pid %d became %d", newPID, got)
	}
}

// A lazy restart of an idle child closes it at once, and starts nothing.
func TestLazyRestartClosesAnIdleChildAtOnce(t *testing.T) {
	setExecWatch(t, execTick, 10*time.Second)
	stops := recordStops(t)
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	oldPID := servedPID(t, p)

	res := p.Restart(context.Background(), true)
	if res.Stopped != 1 || len(res.Started) != 0 || res.Note != "" {
		t.Fatalf("lazy restart of an idle child = %+v", res)
	}
	if st := p.Status(); st.Live != 0 {
		t.Fatalf("%d instances live after a lazy restart, want 0", st.Live)
	}
	if ev := awaitStop(t, stops, oldPID); ev.reason != "restart" {
		t.Fatalf("the idle child closed for %q, want restart", ev.reason)
	}
}

// A lazy restart's call that runs past the drain window is cut off, and the
// error says the server was restarted. The old child is closed at the window,
// not when the call would have ended.
func TestLazyRestartCutsOffACallPastTheDrainWindow(t *testing.T) {
	setExecWatch(t, execTick, 200*time.Millisecond)
	stops := recordStops(t)
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	oldPID := servedPID(t, p)

	done := inFlight(t, p, 3000)
	start := time.Now()
	p.Restart(context.Background(), true)
	var r struct {
		raw json.RawMessage
		err error
	}
	select {
	case r = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the call in flight during a lazy restart hung")
	}
	if r.err == nil {
		t.Fatalf("a call that outlived the drain window completed: %s", textOf(t, r.raw))
	}
	if !strings.Contains(r.err.Error(), "restarted on request") {
		t.Fatalf("the error does not say why the call failed: %v", r.err)
	}
	ev := awaitStop(t, stops, oldPID)
	if ev.reason != "restart" {
		t.Fatalf("the old child closed for %q, want restart", ev.reason)
	}
	if after := ev.at.Sub(start); after > 2500*time.Millisecond {
		t.Fatalf("the old child closed %v after the restart; the drain window is 200ms", after)
	}
	if got := servedPID(t, p); got == oldPID || got == 0 {
		t.Fatalf("after a lazy restart the call was served by pid %d (old child %d)", got, oldPID)
	}
}
