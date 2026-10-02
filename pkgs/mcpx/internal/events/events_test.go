package events_test

import (
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/events"
)

func recv(t *testing.T, s *events.Subscription) events.Event {
	t.Helper()
	select {
	case e := <-s.C:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
	}
	return events.Event{}
}

func none(t *testing.T, s *events.Subscription) {
	t.Helper()
	select {
	case e := <-s.C:
		t.Fatalf("expected nothing, got %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAKindPrefixSelectsAFamily(t *testing.T) {
	b := events.New(0)
	s, _ := b.Subscribe(events.Filter{Kinds: []string{"elicit"}}, 0)
	defer s.Close()

	b.Publish(events.Event{Kind: events.ElicitOpened})
	b.Publish(events.Event{Kind: events.CallFinished})
	b.Publish(events.Event{Kind: events.ElicitAnswered})

	if e := recv(t, s); e.Kind != events.ElicitOpened {
		t.Errorf("got %s", e.Kind)
	}
	if e := recv(t, s); e.Kind != events.ElicitAnswered {
		t.Errorf("the call event should have been filtered out; got %s", e.Kind)
	}
}

func TestASessionOnlyHearsItsOwnQuestions(t *testing.T) {
	// How the plugin hears about its own elicitations and nobody else's.
	b := events.New(0)
	mine, _ := b.Subscribe(events.Filter{Session: "s1"}, 0)
	defer mine.Close()

	b.Publish(events.Event{Kind: events.ElicitOpened, Session: "s2"})
	b.Publish(events.Event{Kind: events.ElicitOpened, Session: "s1"})

	if e := recv(t, mine); e.Session != "s1" {
		t.Errorf("got %s", e.Session)
	}
	none(t, mine)
}

func TestAReconnectingSubscriberMissesNothing(t *testing.T) {
	// The sequence number is what makes a dropped connection lossless
	// rather than merely resumable.
	b := events.New(0)
	first, _ := b.Subscribe(events.Filter{}, 0)
	e1 := b.Publish(events.Event{Kind: events.ServerStarted})
	recv(t, first)
	first.Close() // the connection drops

	b.Publish(events.Event{Kind: events.CallFinished})
	b.Publish(events.Event{Kind: events.ServerStopped})

	again, gap := b.Subscribe(events.Filter{}, e1.Seq)
	defer again.Close()
	if gap {
		t.Error("nothing was lost, so there should be no gap")
	}
	if e := recv(t, again); e.Kind != events.CallFinished {
		t.Errorf("replay should start after what was seen: %s", e.Kind)
	}
	if e := recv(t, again); e.Kind != events.ServerStopped {
		t.Errorf("got %s", e.Kind)
	}
}

func TestAGapBeyondHistoryIsReportedRatherThanHidden(t *testing.T) {
	// A subscriber gone longer than the history covers is told so, so it can
	// resynchronise rather than trust a stream with a hole in it.
	b := events.New(3)
	for i := 0; i < 10; i++ {
		b.Publish(events.Event{Kind: events.CallFinished})
	}
	s, gap := b.Subscribe(events.Filter{}, 1)
	defer s.Close()
	if !gap {
		t.Error("seven events fell out of history; that should be reported")
	}
}

func TestASlowReaderCannotStallThePublisher(t *testing.T) {
	// One slow reader must never become everyone's problem.
	b := events.New(0)
	slow, _ := b.Subscribe(events.Filter{}, 0)
	defer slow.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			b.Publish(events.Event{Kind: events.CallFinished})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing blocked on a reader that was not reading")
	}
	if slow.Dropped.Load() == 0 {
		t.Error("undelivered events should be counted")
	}
}

// A subscriber that disconnects while an event is being published must not
// take the publisher down. Publish used to copy the subscriber list, release
// the lock, and only then send; a Close in that window closed the channel
// under it and the daemon died with "send on closed channel". Seen in the nix
// build of 2026-10-02, where TestASlowReaderCannotStallThePublisher timed out
// under load and its deferred Close raced the still-running publisher.
func TestClosingDuringPublishDoesNotPanic(t *testing.T) {
	b := events.New(0)
	stop := make(chan struct{})
	published := make(chan struct{})
	go func() {
		defer close(published)
		for {
			select {
			case <-stop:
				return
			default:
				b.Publish(events.Event{Kind: events.CallFinished})
			}
		}
	}()
	for i := 0; i < 20000; i++ {
		s, _ := b.Subscribe(events.Filter{}, 0)
		s.Close()
	}
	close(stop)
	<-published
}

func TestResourceUpdatesAreFilteredByURI(t *testing.T) {
	b := events.New(0)
	s, _ := b.Subscribe(events.Filter{URIs: []string{"file:///a"}}, 0)
	defer s.Close()
	b.Publish(events.Event{Kind: events.ResourceUpdated, URI: "file:///b"})
	b.Publish(events.Event{Kind: events.ResourceUpdated, URI: "file:///a"})
	if e := recv(t, s); e.URI != "file:///a" {
		t.Errorf("got %s", e.URI)
	}
}

func TestOnlySpecDefinedEventsBecomeMCPNotifications(t *testing.T) {
	// Most events have no MCP equivalent -- exactly why this bus exists
	// beside subscriptions/listen rather than being replaced by it.
	if _, _, ok := events.MCPNotification(events.Event{Kind: events.ToolsChanged}); !ok {
		t.Error("tools.changed has an MCP form")
	}
	if _, _, ok := events.MCPNotification(events.Event{Kind: events.CallFinished}); ok {
		t.Error("call.finished has no MCP form and must not invent one")
	}
	m, _, _ := events.MCPNotification(events.Event{Kind: events.ResourceUpdated, URI: "x"})
	if m != "notifications/resources/updated" {
		t.Errorf("got %s", m)
	}
}

func TestPositionZeroReplaysEverythingButAbsentIsLiveOnly(t *testing.T) {
	// Conflating them made "replay from the start" silently return nothing,
	// which is indistinguishable from nothing having happened.
	b := events.New(0)
	b.Publish(events.Event{Kind: events.CallFinished})
	b.Publish(events.Event{Kind: events.CallFinished})

	all, _ := b.SubscribeFrom(events.Filter{}, 0, true)
	defer all.Close()
	recv(t, all)
	recv(t, all)

	live, _ := b.SubscribeFrom(events.Filter{}, 0, false)
	defer live.Close()
	none(t, live)
}

// mcpx's listings drop a resource URI's leading "/" when they namespace it,
// so a subscriber naming mcpx://ns/abs/doc asks for "abs/doc" and the
// upstream reports "/abs/doc". Compared literally, they never matched (#241).
func TestResourceURIMatchingIgnoresALeadingSlash(t *testing.T) {
	b := events.New(0)
	s, _ := b.Subscribe(events.Filter{URIs: []string{"abs/doc"}}, 0)
	defer s.Close()
	b.Publish(events.Event{Kind: events.ResourceUpdated, URI: "/abs/other"})
	b.Publish(events.Event{Kind: events.ResourceUpdated, URI: "/abs/doc"})
	if e := recv(t, s); e.URI != "/abs/doc" {
		t.Errorf("got %s", e.URI)
	}
}
