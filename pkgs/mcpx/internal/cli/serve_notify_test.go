package cli

import (
	"reflect"
	"testing"

	"github.com/dezren39/mcpx/internal/events"
)

// A client matches notifications/resources/updated to its subscriptions by
// URI. It subscribed with the mcpx:// URI mcpx listed; the daemon's event
// carries the upstream one, which the client never heard of.
// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#subscriptions
func TestResourceUpdatesCarryTheSubscribedURI(t *testing.T) {
	t.Run("2025-11-25/resources/updated-uri-is-the-subscribed-uri", func(t *testing.T) {
		asked := map[string][]string{"file:///a": {"mcpx://docs/file:///a"}}
		e := events.Event{Kind: events.ResourceUpdated, Server: "docs", URI: "file:///a"}
		_, params, _ := events.MCPNotification(e)
		got := subscribedURIs(e, params, asked)
		want := []any{map[string]any{"uri": "mcpx://docs/file:///a"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		other := events.Event{Kind: events.ToolsChanged}
		_, p2, _ := events.MCPNotification(other)
		if got := subscribedURIs(other, p2, asked); !reflect.DeepEqual(got, []any{p2}) {
			t.Fatalf("a non-resource notification changed: %v", got)
		}
	})
}
