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
		text, _, failed := daemonAsker{app: &App{}}.renderAsk(result)
		if !failed || text != "boom" {
			t.Fatalf("renderAsk = %q, failed=%v", text, failed)
		}
	})
}

// A resource read answered through the ask path names its entries the way
// the listing does. It passed "mcpx://<server>/" as the namespace, so every
// entry came back as mcpx://mcpx://<server>//<uri>, which reads nothing.
func TestAskPathResourceURIsAreNamespacedOnce(t *testing.T) {
	_, contents, _ := daemonAsker{app: &App{}}.renderAsk(map[string]json.RawMessage{
		"kind":   json.RawMessage(`"resources/read"`),
		"server": json.RawMessage(`"demo"`),
		"result": json.RawMessage(`{"contents":[{"uri":"demo://greeting","text":"hi"}]}`),
	})
	if len(contents) != 1 || contents[0].URI != "mcpx://demo/demo://greeting" {
		t.Fatalf("contents = %+v", contents)
	}
}
