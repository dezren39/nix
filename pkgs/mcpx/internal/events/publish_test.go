package events_test

import (
	"sync"
	"testing"

	"github.com/dezren39/mcpx/internal/events"
)

// Publish snapshots the subscribers, drops the bus lock, then sends. A Close
// in that window closed the channel it was about to send on, and "send on
// closed channel" is a panic that takes the daemon with it -- reachable by
// any subscriber disconnecting while an event goes out. Seen for real: the
// nix build's events test panicked here.
func TestClosingASubscriptionDuringPublishDoesNotPanic(t *testing.T) {
	for i := 0; i < 200; i++ {
		b := events.New(8)
		subs := make([]*events.Subscription, 8)
		for j := range subs {
			subs[j], _ = b.Subscribe(events.Filter{}, 0)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for k := 0; k < 200; k++ {
				b.Publish(events.Event{Kind: events.CallFinished})
			}
		}()
		go func() {
			defer wg.Done()
			for _, s := range subs {
				s.Close()
				s.Close() // idempotent: a second close must not panic either
			}
		}()
		wg.Wait()
	}
}

// Trimming the history allocated a new slice and copied every retained event
// on each publish: 214 us and 344 KB per event at the default 1024, on every
// call the daemon reports.
func TestPublishDoesNotCopyTheHistoryEveryTime(t *testing.T) {
	const keep = 1024
	b := events.New(keep)
	for i := 0; i < 4*keep; i++ {
		b.Publish(events.Event{Kind: events.CallFinished})
	}
	got := testing.AllocsPerRun(4*keep, func() {
		b.Publish(events.Event{Kind: events.CallFinished})
	})
	// Amortised: one compaction per keep events, allocating nothing.
	if got > 0.1 {
		t.Errorf("%.2f allocations per publish; the history should be compacted in place", got)
	}
}

// The slack the amortised trim leaves must not reach subscribers: a replay
// still carries at most keep events, and a gap is still reported.
func TestReplayStillStopsAtTheRetentionLimit(t *testing.T) {
	const keep = 4
	b := events.New(keep)
	for i := 0; i < 10*keep; i++ {
		b.Publish(events.Event{Kind: events.CallFinished})
	}
	s, gap := b.SubscribeFrom(events.Filter{}, 1, true)
	defer s.Close()
	if !gap {
		t.Error("a subscriber resuming from the first event has missed some; that is a gap")
	}
	n := 0
	for {
		select {
		case <-s.C:
			n++
			continue
		default:
		}
		break
	}
	if n != keep {
		t.Errorf("replayed %d events, want at most keep=%d", n, keep)
	}
}
