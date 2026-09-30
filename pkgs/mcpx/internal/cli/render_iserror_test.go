package cli

import (
	"encoding/json"
	"testing"
)

// Conflict #3 (WP7): the ask path rendered a failed tool as plain text.
// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#error-handling
func TestRenderKeepsIsError(t *testing.T) {
	t.Run("2025-11-25/tools/ask-path-upstream-isError-preserved", func(t *testing.T) {
		result := map[string]json.RawMessage{
			"kind":   json.RawMessage(`"tools/call"`),
			"result": json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"boom"}]}`),
		}
		text, _, failed := renderAsk(result)
		if !failed || text != "boom" {
			t.Fatalf("renderAsk = %q, failed=%v", text, failed)
		}
	})
}
