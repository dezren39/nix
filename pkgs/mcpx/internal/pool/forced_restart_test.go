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

// The operator's restart replaces every child, whatever the executable holds.
// An identical binary gets a new process: the identity is not consulted.
func TestForcedRestartOfAnIdenticalBinaryChangesTheChild(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	oldPID := servedPID(t, p)
	identity := p.ExecIdentity()

	res := p.Restart(context.Background(), false)
	if res.Stopped != 1 || len(res.Started) != 1 || len(res.Failed) != 0 {
		t.Fatalf("forced restart = %+v", res)
	}
	if got := p.ExecIdentity(); got != identity {
		t.Fatalf("a restart of an unchanged binary moved its identity: %q -> %q", identity, got)
	}
	if got := servedPID(t, p); got == oldPID || got == 0 {
		t.Fatalf("the forced restart of an identical binary kept pid %d", oldPID)
	}
}

// A forced restart drains like an executable change does: a call running when
// it starts finishes on the child it started on, and the next call is served
// by the replacement.
func TestForcedRestartLetsAnInFlightCallFinish(t *testing.T) {
	setExecWatch(t, execTick, 10*time.Second)
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	oldPID := servedPID(t, p)

	done := inFlight(t, p, 1000)
	res := p.Restart(context.Background(), false)
	r := <-done
	if r.err != nil {
		t.Fatalf("the call in flight during the forced restart failed: %v", r.err)
	}
	if want := fmt.Sprintf("on pid %d", oldPID); !strings.Contains(textOf(t, r.raw), want) {
		t.Fatalf("the call did not complete on the old child (want %q): %s", want, textOf(t, r.raw))
	}
	if len(res.Started) != 1 {
		t.Fatalf("forced restart = %+v", res)
	}
	if got := servedPID(t, p); got == oldPID {
		t.Fatalf("the next call was served by the old child pid %d", got)
	}
}

// A call still running when the drain window ends is cut off, and the error
// says why: the server was restarted on request, not that the tool failed.
func TestForcedRestartCutsOffACallPastTheDrainWindow(t *testing.T) {
	setExecWatch(t, execTick, 200*time.Millisecond)
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	servedPID(t, p)

	done := inFlight(t, p, 3000)
	res := p.Restart(context.Background(), false)
	var r struct {
		raw json.RawMessage
		err error
	}
	select {
	case r = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the call in flight during the forced restart hung")
	}
	if r.err == nil {
		t.Fatalf("a call that outlived the drain window completed: %s", textOf(t, r.raw))
	}
	if !strings.Contains(r.err.Error(), "restarted on request") {
		t.Fatalf("the error does not say why the call failed: %v", r.err)
	}
	if len(res.Started) != 1 {
		t.Fatalf("forced restart = %+v", res)
	}
}
