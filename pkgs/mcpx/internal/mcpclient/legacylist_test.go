package mcpclient_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// A server that knows server/discover but offers only initialize-era
// versions. Datadog's MCP server does this: it answers discover with -32022
// and supported [2024-11-05 .. 2025-11-25]. mcpx called that "modern, no
// shared version" and failed, while speaking 2025-11-25 itself.
func TestALegacyOnlySupportedListFallsBackToInitialize(t *testing.T) {
	legacyList := []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}
	refuse := func() (any, map[string]any, bool) {
		return nil, rpcErr(-32022, "Unsupported protocol version",
			map[string]any{"supported": legacyList, "requested": "2026-07-28"}), false
	}
	for _, tc := range []struct {
		id     string
		answer func() (any, map[string]any, bool)
	}{
		{"unsupported-version-error", refuse},
		{"discover-result", func() (any, map[string]any, bool) { return discoverResult(legacyList...), nil, false }},
	} {
		t.Run("2026-07-28/stdio-compat/legacy-only-"+tc.id+"-falls-back", func(t *testing.T) {
			s := newProbeServer(legacyAnswering(tc.answer))
			// Long enough that the timer-driven initialize cannot be what
			// rescues the connection: the verdict on discover must.
			c, err := probeDial(t, s, mcpclient.Options{ProbeTimeout: 4 * time.Second})
			if err != nil {
				t.Fatalf("%v; frames %v", err, s.methods())
			}
			if c.Era != mcpclient.EraLegacy || !sent(s, "initialize") {
				t.Errorf("era=%q frames=%v", c.Era, s.methods())
			}
		})
	}

	t.Run("2026-07-28/stdio-compat/legacy-only-list-is-an-error-when-forced-modern", func(t *testing.T) {
		s := newProbeServer(legacyAnswering(refuse))
		if _, err := probeDial(t, s, mcpclient.Options{Preference: mcpclient.ForceModern}); err == nil || sent(s, "initialize") {
			t.Errorf("force-discover must not fall back: err=%v frames=%v", err, s.methods())
		}
	})

	t.Run("2026-07-28/http-compat/400-with-legacy-only-list-falls-back", func(t *testing.T) {
		srv, seen := httpServer(t, func(m string, id json.RawMessage, _ *http.Request) (int, string) {
			if m == "initialize" {
				return 200, legacyInit(id)
			}
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": nil,
				"error": rpcErr(-32022, "Unsupported protocol version", map[string]any{"supported": legacyList})})
			return 400, string(b)
		})
		c, err := dialHTTP(t, srv.URL, mcpclient.Options{})
		if err != nil || c.Era != mcpclient.EraLegacy || !has(*seen, "initialize") {
			t.Errorf("err=%v seen=%v", err, *seen)
		}
	})
}
