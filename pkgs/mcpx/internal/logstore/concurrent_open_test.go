package logstore

import (
	"sync"
	"testing"
)

// Two processes -- or two concurrent MCP requests in one -- open a fresh
// index at the same moment. The WAL switch needs an exclusive lock, and it
// was requested before busy_timeout was set, so the loser failed at once with
// "database is locked" instead of waiting its turn.
func TestConcurrentOpenOfAFreshIndexWaitsRatherThanFailing(t *testing.T) {
	for round := 0; round < 20; round++ {
		dir := t.TempDir()
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, err := Open(dir)
				if err != nil {
					errs <- err
					return
				}
				s.Close()
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d: %v", round, err)
		}
	}
}
