package pool

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
)

// waitLocked is entered holding p.mu and waits on p.cond. Its helper
// goroutine, on ctx.Done, takes p.mu to broadcast. If a broadcast from
// elsewhere wakes the waiter at the same moment its ctx ends, the waiter
// reacquires p.mu and then waits for the helper -- which is blocked on p.mu.
// Neither moves again, p.mu is held for good, and every later call into the
// pool hangs. Any caller cancelled while an instance is being released can
// do it.
func TestWaitLockedSurvivesAWakeupRacingCancellation(t *testing.T) {
	for i := 0; i < 300; i++ {
		p := &Pool{cfg: &config.Resolved{Server: &config.Server{Name: "waiter"}, Max: 1}}
		p.cond = sync.NewCond(&p.mu)
		ctx, cancel := context.WithCancel(context.Background())

		waiting := make(chan struct{})
		done := make(chan struct{})
		go func() {
			p.mu.Lock()
			close(waiting)
			_ = p.waitLocked(ctx)
			p.mu.Unlock()
			close(done)
		}()
		<-waiting
		// The waiter is in, or about to enter, cond.Wait; taking p.mu
		// guarantees it is (Wait releases it). Then wake it and end its
		// context together, as a release racing a cancellation does.
		p.mu.Lock()
		p.cond.Broadcast()
		cancel()
		p.mu.Unlock()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: waitLocked deadlocked with the pool lock held", i)
		}
	}
}
