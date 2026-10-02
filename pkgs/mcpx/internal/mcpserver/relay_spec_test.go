package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/spec"
)

// progress.message to a 2024-11-05 client: stripped only when 2024-11-05 is
// first in spec.precedence and strict (#307).
func TestProgressMessagePrecedence(t *testing.T) {
	in := json.RawMessage(`{"progressToken":"p","progress":1,"message":"step"}`)
	for _, c := range []struct {
		name  string
		pol   spec.Policy
		strip bool
	}{
		{"default", spec.Default(), false},
		{"2024-11-05 first, strict", spec.Must(nil, "2024-11-05", nil), true},
		{"2024-11-05 first, lenient", spec.Must(nil, "2024-11-05", []string{"2024-11-05"}), false},
		{"2025-03-26 first", spec.Must(nil, "2025-03-26", nil), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer spec.Set(c.pol)()
			out := string(downgradeNotification("notifications/progress", in, "2024-11-05"))
			if got := !strings.Contains(out, "message"); got != c.strip {
				t.Errorf("stripped=%v, want %v: %s", got, c.strip, out)
			}
			// A revision that defines the field is never touched.
			if out := string(downgradeNotification("notifications/progress", in, "2025-03-26")); !strings.Contains(out, "message") {
				t.Errorf("stripped for 2025-03-26: %s", out)
			}
		})
	}
}
