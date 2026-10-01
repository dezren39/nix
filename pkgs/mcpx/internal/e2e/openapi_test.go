package e2e_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The published document does not depend on what this machine runs.
//
// `mcpx openapi` is what a person publishes, so two people with different
// servers configured must get the same bytes. The unit test that claimed
// this called cli.OpenAPI twice in one process and compared the results --
// a pure function against itself, true whatever the function did. The
// property only shows up across configurations, which needs the binary.
//
// The mechanism is that an upstream tool is covered by the declared
// /v1/call/{server}/{tool} template rather than enumerated. The daemon's own
// /v1/openapi.json does enumerate, deliberately -- configuring one server
// with eight tools takes it from 53 paths to 61 -- so this asserts the
// property of the command's document, which is the one that gets published.
func TestTheOpenAPIDocumentIsTheSameWhateverIsConfigured(t *testing.T) {
	bare := newEnv(t, `{"mcpServers":{}}`)
	loaded := newEnv(t, oneServer)
	// Not just configured: started, with its catalogue read and a call made,
	// which is what fills the catalogue the daemon's document enumerates
	// from. Without this the two documents could agree only because the
	// second daemon never learned any tools.
	loaded.run("ls")
	loaded.run("call", "demo.echo", `{"message":"warm"}`)

	a, b := bare.run("openapi"), loaded.run("openapi")
	if a != b {
		t.Errorf("`mcpx openapi` differs between an empty configuration and one with "+
			"a running server, so the document is not publishable\n%s",
			firstDifference(a, b))
	}
	// The premise. If the template were gone the documents could agree by
	// both describing nothing, and if the catalogue were empty there would
	// have been nothing for the second one to add.
	if !strings.Contains(a, "/v1/call/{server}/{tool}") {
		t.Error("the upstream-tool template is absent, so there was nothing for a " +
			"configured server to add and this comparison proves nothing")
	}
	if tools := loaded.run("--json", "tools"); !strings.Contains(tools, `"echo"`) {
		t.Errorf("the configured daemon knows no tools, so its document had nothing "+
			"machine-specific to include and this comparison proves nothing:\n%s", tools)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(a), &doc); err != nil {
		t.Fatalf("`mcpx openapi` is not JSON: %v", err)
	}
	if paths, _ := doc["paths"].(map[string]any); len(paths) == 0 {
		t.Error("the document has no paths")
	}
}

// firstDifference names the line two documents first disagree on, because a
// diff of two 200KB JSON documents in a test log is unreadable.
func firstDifference(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] != lb[i] {
			return fmt.Sprintf("  line %d:\n    empty config: %s\n    with a server: %s", i+1, la[i], lb[i])
		}
	}
	return fmt.Sprintf("  same %d lines, then %d against %d lines", min(len(la), len(lb)), len(la), len(lb))
}
