package pool_test

import (
	"context"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// A daemon stop drains its pools for the window it is given. A call that
// never returns is cut when the window passes, and WaitDrained returns then:
// the stop never waits on it longer.
func TestCloseDrainedWithinCutsACallThatNeverReturnsAtTheWindow(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2}))
	lease, err := p.Acquire(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	const window = 300 * time.Millisecond
	start := time.Now()
	p.CloseDrainedWithin(window)
	p.WaitDrained()
	elapsed := time.Since(start)
	if elapsed < window {
		t.Fatalf("the held call was cut after %v, before its %v window", elapsed, window)
	}
	if elapsed > window+5*time.Second {
		t.Fatalf("the stop waited %v for a call that never returns; the window is %v", elapsed, window)
	}
}

// A call that finishes inside the window is not cut: the stop returns as soon
// as it does, not at the end of the window.
func TestCloseDrainedWithinLetsACallFinishInsideTheWindow(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2}))
	lease, err := p.Acquire(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	const window = 5 * time.Second
	go func() {
		time.Sleep(100 * time.Millisecond)
		lease.Release()
	}()

	start := time.Now()
	p.CloseDrainedWithin(window)
	p.WaitDrained()
	if elapsed := time.Since(start); elapsed >= window {
		t.Fatalf("the stop took %v; it should return once the call finished, well inside %v", elapsed, window)
	}
}

// A stop whose window is already spent cuts at once rather than waiting.
func TestCloseDrainedWithinZeroCutsAtOnce(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2}))
	lease, err := p.Acquire(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	start := time.Now()
	p.CloseDrainedWithin(0)
	p.WaitDrained()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("a spent window still waited %v", elapsed)
	}
}
