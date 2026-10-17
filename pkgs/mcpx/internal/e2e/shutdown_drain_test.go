package e2e_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// Stopping the daemon, or removing a server, takes its pools out of service
// while a call may still be running on them. That call is a caller's work and
// gets the same chance a call gets when a pool setting changes: it finishes on
// the old child and returns its real result, up to the drain window
// (limits.execDrainTimeout). A call that runs past the window is cut at it.

// A session-scoped server, so the slow call is not shared with the warm-up.
func TestAShutdownDoesNotCutACallInFlight(t *testing.T) {
	cases := []struct {
		name string
		ms   int
	}{
		{"a short call", 3000},
		// Past the HTTP grace (5 s), which the old shutdown waited before it
		// began closing pools.
		{"a call past the HTTP grace", 7000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, twoSessionPools)
			e.run("call", "demo.echo", `{"message":"warm"}`)
			slow := e.callInBackground("call", "demo.slow", fmt.Sprintf(`{"ms":%d}`, tc.ms))
			e.awaitInFlight(t)

			e.run("stop")
			r := awaitResult(t, slow)
			want := fmt.Sprintf("slept %dms", tc.ms)
			if r.err != nil || !strings.Contains(r.out, want) {
				t.Fatalf("the call in flight was cut by `mcpx stop`: err=%v\n%s", r.err, r.out)
			}
		})
	}
}

// Removing a server is a reload, and a reload drains the pool it replaces.
// Pinned here so the removal path cannot drift back to a closing one.
func TestARemovedServerDoesNotCutACallInFlight(t *testing.T) {
	e := newEnv(t, twoSessionPools)
	e.run("call", "demo.echo", `{"message":"warm"}`)
	slow := e.callInBackground("call", "demo.slow", `{"ms":7000}`)
	e.awaitInFlight(t)

	e.run("servers", "remove", "demo")
	r := awaitResult(t, slow)
	if r.err != nil || !strings.Contains(r.out, "slept 7000ms") {
		t.Fatalf("the call in flight was cut by `servers remove`: err=%v\n%s", r.err, r.out)
	}
}

// A call that never returns is not waited for. It is held for the drain
// window, cut when the window passes with the reason said, and the shutdown
// completes with its child gone.
func TestAShutdownCutsACallThatNeverReturnsAtTheDrainWindow(t *testing.T) {
	e := newEnv(t, twoSessionPools)
	e.run("call", "demo.echo", `{"message":"warm"}`)
	slow := e.callInBackground("call", "demo.slow", `{"ms":60000}`)
	e.awaitInFlight(t)
	pid := e.inFlightPID(t)

	start := time.Now()
	e.run("stop")
	r := awaitResult(t, slow)
	elapsed := time.Since(start)

	if r.err == nil || strings.Contains(r.out, "slept") {
		t.Fatalf("a call that never returns should be cut, not answered: err=%v\n%s", r.err, r.out)
	}
	if !strings.Contains(r.out, "stopped because the daemon is shutting down") {
		t.Errorf("the cut call should say the daemon stopped under it:\n%s", r.out)
	}
	// It had the whole window: cut early, the drain did not run.
	if elapsed < defaults.ExecDrainTimeout-time.Second {
		t.Errorf("the call was cut after %v, before the %v drain window", elapsed, defaults.ExecDrainTimeout)
	}
	// The window bounds the stop. The slack is for process teardown.
	if limit := defaults.ExecDrainTimeout + 5*time.Second; elapsed > limit {
		t.Errorf("the shutdown held a call for %v, past the %v drain window", elapsed, defaults.ExecDrainTimeout)
	}
	deadline := time.Now().Add(15 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("child %d still running after the daemon stopped", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// inFlightPID is the pid of the child a caller is holding, as the daemon
// reports it.
func (e *env) inFlightPID(t *testing.T) int {
	t.Helper()
	var st struct {
		Servers []struct {
			Instances []struct {
				PID     int `json:"pid"`
				Holders int `json:"holders"`
			} `json:"instances"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatal(err)
	}
	for _, s := range st.Servers {
		for _, in := range s.Instances {
			if in.Holders > 0 && in.PID > 0 {
				return in.PID
			}
		}
	}
	t.Fatal("no in-flight child with a pid")
	return 0
}
