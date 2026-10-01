package cli

import (
	"encoding/json"
	"testing"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// Conflict #3 (WP7): the ask path rendered a failed tool as plain text.
// https://modelcontextprotocol.io/specification/2025-11-25/server/tools#error-handling
func TestRenderKeepsIsError(t *testing.T) {
	t.Run("2025-11-25/tools/ask-path-upstream-isError-preserved", func(t *testing.T) {
		result := map[string]json.RawMessage{
			"kind":   json.RawMessage(`"tools/call"`),
			"result": json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"boom"}]}`),
		}
		// Carried verbatim since #206, so isError is in the result itself
		// rather than reconstructed from a flag.
		text, _, _ := daemonAsker{app: &App{}}.renderAsk(result)
		raw, ok := mcpserver.DecodeRaw(text)
		if !ok {
			t.Fatalf("renderAsk should carry the upstream result verbatim: %q", text)
		}
		var got struct {
			IsError bool `json:"isError"`
		}
		if json.Unmarshal(raw, &got) != nil || !got.IsError {
			t.Fatalf("isError lost: %s", raw)
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
