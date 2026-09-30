package pool

import (
	"context"
	"testing"
	"time"
)

// Conflict #6 (WP7): the call budget must not run while a question is
// pending, and must still run out when none is.
func TestBudget(t *testing.T) {
	const d = 50 * time.Millisecond
	t.Run("mcpx/timeouts/budget-expires-with-no-question", func(t *testing.T) {
		var q questions
		ctx, cancel := budget(context.Background(), d, &q)
		defer cancel()
		select {
		case <-ctx.Done():
			if context.Cause(ctx) != errCallTimeout {
				t.Fatalf("cause = %v", context.Cause(ctx))
			}
		case <-time.After(20 * d):
			t.Fatal("the budget never ran out")
		}
	})
	t.Run("mcpx/timeouts/budget-paused-while-a-question-is-pending", func(t *testing.T) {
		var q questions
		ctx, cancel := budget(context.Background(), d, &q)
		defer cancel()
		done := q.asking()
		select {
		case <-ctx.Done():
			t.Fatal("the budget ran out while a question was pending")
		case <-time.After(10 * d):
		}
		done()
		select {
		case <-ctx.Done():
		case <-time.After(20 * d):
			t.Fatal("the budget did not resume after the question closed")
		}
	})
}
