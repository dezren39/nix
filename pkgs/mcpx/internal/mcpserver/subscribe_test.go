package mcpserver_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// resNotifier arranges updates for URIs under mcpx://yes/ and refuses the
// rest, as the daemon does for a server that does not declare
// resources.subscribe. Every update it is asked to deliver is sent for
// every requested URI, refused or not, so the server's own narrowing is
// what is under test.
type resNotifier struct {
	mu     sync.Mutex
	active int
	fire   chan struct{}
}

func (n *resNotifier) Listen(ctx context.Context, f mcpserver.ListenFilter, send func(string, any)) {
	n.ListenResources(ctx, f, nil, send)
}

func (n *resNotifier) ListenResources(ctx context.Context, f mcpserver.ListenFilter,
	ready func([]string, map[string]string), send func(string, any)) {
	n.mu.Lock()
	n.active++
	n.mu.Unlock()
	defer func() { n.mu.Lock(); n.active--; n.mu.Unlock() }()
	var ok []string
	refused := map[string]string{}
	for _, u := range f.ResourceSubscriptions {
		if strings.HasPrefix(u, "mcpx://yes/") {
			ok = append(ok, u)
		} else {
			refused[u] = "the server does not declare resources.subscribe"
		}
	}
	if ready != nil {
		ready(ok, refused)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.fire:
			for _, u := range f.ResourceSubscriptions {
				send("notifications/resources/updated", map[string]any{"uri": u})
			}
		}
	}
}

func (n *resNotifier) streams() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.active
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/subscriptions#acknowledgment
// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#subscriptions
func TestResourceSubscriptionsAgreeOnlyToWhatCanBeDelivered(t *testing.T) {
	setup := func() (*mcpserver.Server, *resNotifier, *recorder, *mcpserver.Conn) {
		n := &resNotifier{fire: make(chan struct{}, 4)}
		s := mcpserver.New(newBackend(), "mcpx", "test")
		s.Notify = n
		rec := &recorder{}
		return s, n, rec, s.ConnWithSend("sess-res", rec.send)
	}

	t.Run("2026-07-28/subscriptions/ack-omits-resources-whose-updates-cannot-be-delivered", func(t *testing.T) {
		s, n, rec, c := setup()
		s.HandleOn(context.Background(), c, mcpserver.Request(3, "subscriptions/listen",
			modernParams(map[string]any{"notifications": map[string]any{
				"resourceSubscriptions": []string{"mcpx://yes/a", "mcpx://no/b"}}})))
		ack := rec.wait(t, 1)[0]
		agreed, _ := ack["params"].(map[string]any)["notifications"].(map[string]any)
		if got := protoJSON(t, agreed["resourceSubscriptions"]); got != `["mcpx://yes/a"]` {
			t.Fatalf("agreed %s", got)
		}
		n.fire <- struct{}{}
		rec.wait(t, 2)
		time.Sleep(50 * time.Millisecond)
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, f := range rec.frames[1:] {
			if strings.Contains(protoJSON(t, f), "mcpx://no/b") {
				t.Errorf("delivered an update that was not agreed: %v", f)
			}
		}
	})

	// A subscription is a standing interest: the legacy revisions neither
	// require the resource to exist nor give a way to say "agreed, but
	// nothing will come", so subscribe succeeds -- and delivers nothing (#251).
	t.Run("2025-11-25/resources/subscribe-succeeds-and-delivers-nothing-when-updates-cannot-be-arranged", func(t *testing.T) {
		s, n, rec, c := setup()
		for i, u := range []string{"mcpx://no/b", "test://unowned", "mcpx://yes/a"} {
			resp := protoJSON(t, s.HandleOn(context.Background(), c,
				mcpserver.Request(i+1, "resources/subscribe", map[string]any{"uri": u})))
			if strings.Contains(resp, "error") {
				t.Errorf("subscribe %s: %s", u, resp)
			}
		}
		// Replaced streams end asynchronously; only the last may hear fire.
		deadline := time.Now().Add(2 * time.Second)
		for n.streams() != 1 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		n.fire <- struct{}{}
		rec.wait(t, 1)
		time.Sleep(50 * time.Millisecond)
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, f := range rec.frames {
			if got := protoJSON(t, f); strings.Contains(got, "mcpx://no/b") || strings.Contains(got, "test://unowned") {
				t.Errorf("delivered an update that cannot have been arranged: %s", got)
			}
		}
	})

	t.Run("2025-11-25/resources/legacy-subscription-ends-with-the-connection", func(t *testing.T) {
		s, n, _, c := setup()
		s.HandleOn(context.Background(), c,
			mcpserver.Request(1, "resources/subscribe", map[string]any{"uri": "mcpx://yes/a"}))
		s.HandleOn(context.Background(), c,
			mcpserver.Request(2, "resources/subscribe", map[string]any{"uri": "mcpx://yes/b"}))
		// The replaced stream ends asynchronously.
		deadline := time.Now().Add(2 * time.Second)
		for n.streams() != 1 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if got := n.streams(); got != 1 {
			t.Fatalf("%d notifier streams for one connection's subscriptions, want 1", got)
		}
		c.StopListenForTest()
		for n.streams() != 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if got := n.streams(); got != 0 {
			t.Errorf("%d notifier streams outlived the connection", got)
		}
	})
}
