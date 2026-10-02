// Package events is a publish/subscribe bus for things the daemon notices.
//
// The MCP specification's own subscription mechanism, subscriptions/listen,
// carries exactly four kinds of notification: three list-changed events and
// resource updates. That is the right set for what it is -- a server telling
// a client its catalogue moved -- and nowhere near enough for everything a
// client of mcpx wants to hear about: a question waiting for an answer, a
// server that crashed, a call that finished, a record that was logged.
//
// So there are two layers. This bus carries every event mcpx produces. The
// MCP subscription is a filtered view of it, restricted to what the
// specification allows; /v1/events is an unfiltered one for clients that
// speak mcpx's own API. Both are the same stream underneath, so they cannot
// disagree about what happened.
package events

import (
	"encoding/json"
	"github.com/dezren39/mcpx/internal/defaults"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Kind names an event. Dotted, so a subscriber can match a family.
type Kind string

const (
	ToolsChanged     Kind = "tools.changed"
	ResourcesChanged Kind = "resources.changed"
	PromptsChanged   Kind = "prompts.changed"
	ResourceUpdated  Kind = "resource.updated"
	// ResourceWatching opens a /v1/events stream that named resources: what
	// it subscribed upstream and what it could not. Never on the bus.
	ResourceWatching Kind = "resource.watching"

	ElicitOpened    Kind = "elicit.opened"
	ElicitAnswered  Kind = "elicit.answered"
	ElicitCompleted Kind = "elicit.completed"

	SampleOpened   Kind = "sample.opened"
	SampleAnswered Kind = "sample.answered"

	ServerStarted Kind = "server.started"
	ServerStopped Kind = "server.stopped"

	// CallFinished is published by the daemon's hooks. There is no
	// call.started: it was declared here and published by nothing, so a
	// subscriber waiting for it waited forever (docs/decisions/0001).
	CallFinished Kind = "call.finished"

	ServerLog Kind = "server.log"
	Progress  Kind = "progress"
)

// Event is one thing that happened.
type Event struct {
	// Seq is monotonic per daemon. A subscriber that reconnects sends the
	// last one it saw and receives everything after it, which is what makes
	// a dropped connection lossless rather than merely resumable.
	Seq     uint64          `json:"seq"`
	Kind    Kind            `json:"kind"`
	At      time.Time       `json:"at"`
	Server  string          `json:"server,omitempty"`
	Session string          `json:"session,omitempty"`
	Trace   string          `json:"trace,omitempty"`
	URI     string          `json:"uri,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Filter selects events.
type Filter struct {
	// Kinds are prefixes: "elicit" matches every elicitation event.
	Kinds []string
	// Session restricts to one session's events, and is how the plugin
	// hears about its own questions and nobody else's.
	Session string
	// URIs restricts resource.updated to these resources.
	URIs []string
	// Server restricts to one server.
	Server string
}

// Matches reports whether an event passes.
func (f Filter) Matches(e Event) bool {
	if len(f.Kinds) > 0 {
		ok := false
		for _, k := range f.Kinds {
			if k == string(e.Kind) || strings.HasPrefix(string(e.Kind), k+".") || k == "*" {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.Session != "" && e.Session != "" && e.Session != f.Session {
		return false
	}
	if f.Server != "" && e.Server != "" && e.Server != f.Server {
		return false
	}
	if e.Kind == ResourceUpdated && len(f.URIs) > 0 {
		// Compared without a leading "/": mcpx's listings drop it (see the
		// daemon's upstreamResourceURI), so a subscriber may name /abs as abs.
		for _, u := range f.URIs {
			if strings.TrimPrefix(u, "/") == strings.TrimPrefix(e.URI, "/") {
				return true
			}
		}
		return false
	}
	return true
}

// Bus fans events out to subscribers and keeps a short history.
type Bus struct {
	seq atomic.Uint64

	mu      sync.Mutex
	subs    map[int]*Subscription
	next    int
	history []Event
	keep    int
}

// New creates a bus that remembers the last keep events.
//
// The history is what lets a reconnecting subscriber miss nothing. It is
// bounded, because an unbounded one is a memory leak with extra steps; a
// subscriber gone longer than the history covers is told so rather than
// silently given a gap.
func New(keep int) *Bus {
	if keep <= 0 {
		keep = defaults.EventHistory
	}
	return &Bus{subs: map[int]*Subscription{}, keep: keep}
}

// Subscription is one listener.
type Subscription struct {
	C      <-chan Event
	ch     chan Event
	filter Filter
	id     int
	bus    *Bus
	// Dropped counts events that could not be delivered because the
	// subscriber was not reading. Reported rather than blocking the
	// publisher, since one slow reader must never stall the daemon.
	Dropped atomic.Uint64

	// mu guards closed and the send on ch. The receiver closes this
	// channel, so without it a Close between Publish's snapshot of the
	// subscribers and its send panics the daemon: "send on closed channel".
	// A read lock, so publishers do not serialise with each other; the send
	// never blocks, so Close waits only for the select below.
	mu     sync.RWMutex
	closed bool
}

// send delivers one event unless the subscription has been closed.
func (s *Subscription) send(e Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- e:
	default:
		s.Dropped.Add(1)
	}
}

// Close stops delivery.
func (s *Subscription) Close() {
	s.bus.mu.Lock()
	delete(s.bus.subs, s.id)
	s.bus.mu.Unlock()
	// After the bus lock, never under it: a publisher holds no bus lock by
	// the time it sends, and taking them in one order everywhere is what
	// keeps that true.
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
	s.mu.Unlock()
}

// Subscribe starts listening, live only.
func (b *Bus) Subscribe(f Filter, since uint64) (sub *Subscription, gap bool) {
	return b.SubscribeFrom(f, since, since > 0)
}

// SubscribeFrom starts listening. since is the last sequence number already
// seen, and replay says whether to send what came after it.
//
// "No position given" and "position zero" are different requests. The first
// means live only; the second means everything still retained. Conflating
// them -- treating zero as absent -- made "replay from the start" silently
// return nothing, which is indistinguishable from nothing having happened.
//
// gap is true when since predates the retained history, so the subscriber
// knows it missed something and can resynchronise instead of trusting a
// stream with a hole in it.
func (b *Bus) SubscribeFrom(f Filter, since uint64, replay bool) (sub *Subscription, gap bool) {
	ch := make(chan Event, defaults.EventSubscriberBuf)
	b.mu.Lock()
	defer b.mu.Unlock()

	sub = &Subscription{C: ch, ch: ch, filter: f, id: b.next, bus: b}
	b.next++

	if replay {
		h := b.retained()
		if since > 0 && len(h) > 0 && h[0].Seq > since+1 {
			gap = true
		}
		for _, e := range h {
			if e.Seq > since && f.Matches(e) {
				select {
				case ch <- e:
				default:
					sub.Dropped.Add(1)
				}
			}
		}
	}
	b.subs[sub.id] = sub
	return sub, gap
}

// Publish sends an event to everyone who wants it.
//
// Never blocks. A subscriber whose buffer is full misses the event and has
// its drop counter raised -- stalling the daemon for one slow reader would
// turn a client problem into everyone's problem.
func (b *Bus) Publish(e Event) Event {
	e.Seq = b.seq.Add(1)
	if e.At.IsZero() {
		e.At = time.Now()
	}
	b.mu.Lock()
	b.history = append(b.history, e)
	// Trim in place, and only when twice the limit has built up: trimming on
	// every event allocated a fresh slice and copied the whole history each
	// time -- 214 us and 344 KB per event at the default 1024, paid by every
	// call the daemon reports. Compacting once per keep events amortises to
	// nothing, and retained() hides the slack from subscribers.
	if len(b.history) >= 2*b.keep {
		b.history = b.history[:copy(b.history, b.history[len(b.history)-b.keep:])]
	}
	subs := make([]*Subscription, 0, len(b.subs))
	for _, s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()

	for _, s := range subs {
		if s.filter.Matches(e) {
			s.send(e)
		}
	}
	return e
}

// retained is the history a subscriber may see: the last keep events. The
// slice itself may hold up to twice that between compactions, which is an
// allocation strategy and not a longer memory. Callers hold b.mu.
func (b *Bus) retained() []Event {
	if len(b.history) > b.keep {
		return b.history[len(b.history)-b.keep:]
	}
	return b.history
}

// Latest is the most recent sequence number, for a subscriber that wants to
// start "from now" and still be able to resume later.
func (b *Bus) Latest() uint64 { return b.seq.Load() }

// Subscribers reports how many listeners there are, for status output.
func (b *Bus) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// MCPNotification translates an event into the notification the MCP
// specification defines for it, if there is one.
//
// Most events have no MCP equivalent; that is exactly why this bus exists
// beside subscriptions/listen rather than being replaced by it.
func MCPNotification(e Event) (method string, params any, ok bool) {
	switch e.Kind {
	case ToolsChanged:
		return "notifications/tools/list_changed", map[string]any{}, true
	case ResourcesChanged:
		return "notifications/resources/list_changed", map[string]any{}, true
	case PromptsChanged:
		return "notifications/prompts/list_changed", map[string]any{}, true
	case ResourceUpdated:
		return "notifications/resources/updated", map[string]any{"uri": e.URI}, true
	case ElicitCompleted:
		var d struct {
			ElicitationID string `json:"elicitationId"`
		}
		_ = json.Unmarshal(e.Data, &d)
		return "notifications/elicitation/complete",
			map[string]any{"elicitationId": d.ElicitationID}, true
	}
	return "", nil, false
}
