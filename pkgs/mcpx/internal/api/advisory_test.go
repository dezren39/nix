package api_test

import (
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
)

// TestAPrivilegedOperationSaysItIsAdvisory holds the admin label to
// docs/decisions/0003: a restriction nothing enforces must say so on the
// surface that shows it.
//
// "Privileged" is mcpx's own word, not one of the specification's *Hint
// fields, so a model reading a tool list cannot tell from the word alone
// whether something checks it. Nothing does -- every endpoint is
// unauthenticated -- and the OpenAPI document already says that. The tool
// description is the surface a model actually reads, so it has to say it too.
func TestAPrivilegedOperationSaysItIsAdvisory(t *testing.T) {
	admin := 0
	for _, op := range api.Ops() {
		if !op.Admin {
			continue
		}
		admin++
		desc := op.ToolDescription()
		if !strings.Contains(desc, "Privileged") {
			t.Errorf("%s: an admin operation should still be labelled privileged:\n%s", op.Name, desc)
		}
		if !strings.Contains(desc, "Advisory:") {
			t.Errorf("%s: the privileged label must say it is advisory, "+
				"because nothing checks who calls it:\n%s", op.Name, desc)
		}
	}
	if admin == 0 {
		t.Fatal("no admin operations found; the table changed shape and this test checks nothing")
	}

	info, _ := api.OpenAPI("test")["info"].(map[string]any)
	if d, _ := info["description"].(string); !strings.Contains(d, "unauthenticated") {
		t.Errorf("the OpenAPI document is the other surface that shows x-mcpx-admin, "+
			"and it must keep saying the API is unauthenticated:\n%s", d)
	}
}
