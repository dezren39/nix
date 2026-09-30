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
// The mechanism is that upstream tools are reached through the
// /v1/tools/{tool} template rather than enumerated; if that ever changes to
// enumeration, the daemon with a server configured gains paths and this
// fails.
func TestTheOpenAPIDocumentIsTheSameWhateverIsConfigured(t *testing.T) {
	bare := newEnv(t, `{"mcpServers":{}}`)
	loaded := newEnv(t, oneServer)
	// Not just configured: started, with its catalogue read. A document
	// built from a live daemon's tools would differ from here on.
	loaded.run("ls")
	loaded.run("call", "demo.echo", `{"message":"warm"}`)

	a, b := bare.run("openapi"), loaded.run("openapi")
	if a != b {
		t.Errorf("`mcpx openapi` differs between an empty configuration and one with "+
			"a running server, so the document is not publishable\n%s",
			firstDifference(a, b))
	}
	// The premise: if neither daemon has any tools, the comparison is
	// between two empty documents and proves nothing.
	if !strings.Contains(a, "/v1/tools/{tool}") {
		t.Error("the upstream-tool template is absent, so there was nothing for a " +
			"configured server to add and this comparison proves nothing")
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
