package codegen_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/codegen"
)

func diffNS(name string, tools ...codegen.Tool) codegen.Namespace {
	return codegen.Namespace{Name: name, Server: name, Tools: tools}
}

func diffTool(name, desc, schema string) codegen.Tool {
	return codegen.Tool{Name: name, Description: desc, InputSchema: json.RawMessage(schema)}
}

func TestADiffNoticesAdditionsRemovalsAndChanges(t *testing.T) {
	before := codegen.Fingerprint([]codegen.Namespace{
		diffNS("alpha", diffTool("read", "reads", `{"type":"object"}`),
			diffTool("write", "writes", `{"type":"object"}`)),
	})
	after := codegen.Fingerprint([]codegen.Namespace{
		diffNS("alpha", diffTool("read", "reads", `{"type":"object"}`),
			// A changed schema matters as much as a new tool: a caller with
			// the old signature will send the wrong arguments.
			diffTool("list", "lists", `{"type":"object"}`)),
		diffNS("beta", diffTool("go", "goes", `{"type":"object"}`)),
	})
	d := codegen.Diff(before, after)

	if strings.Join(d.Added, ",") != "alpha.list,beta.go" {
		t.Errorf("added = %v", d.Added)
	}
	if strings.Join(d.Removed, ",") != "alpha.write" {
		t.Errorf("removed = %v", d.Removed)
	}
	if strings.Join(d.NewServers, ",") != "beta" {
		t.Errorf("newServers = %v", d.NewServers)
	}
}

func TestAChangedSchemaIsNoticed(t *testing.T) {
	before := codegen.Fingerprint([]codegen.Namespace{
		diffNS("a", diffTool("x", "same", `{"type":"object","properties":{"p":{}}}`)),
	})
	after := codegen.Fingerprint([]codegen.Namespace{
		diffNS("a", diffTool("x", "same", `{"type":"object","properties":{"q":{}}}`)),
	})
	d := codegen.Diff(before, after)
	if strings.Join(d.Changed, ",") != "a.x" {
		t.Errorf("a changed signature should be reported: %+v", d)
	}
}

func TestNoChangeSaysSo(t *testing.T) {
	state := codegen.Fingerprint([]codegen.Namespace{diffNS("a", diffTool("x", "d", `{}`))})
	d := codegen.Diff(state, state)
	if !d.Empty() {
		t.Errorf("expected no change: %+v", d)
	}
	if !strings.Contains(d.Render(), "No change") {
		t.Errorf("got %q", d.Render())
	}
}

func TestTheShorterFormWins(t *testing.T) {
	// The point of a diff is to cost less. When almost everything changed it
	// does not, and the reader would have to rebuild the whole from a list.
	full := strings.Repeat("x", 100)
	if got := codegen.ShorterOf(full, "short diff"); got != "short diff" {
		t.Errorf("got %q", got)
	}
	longDiff := strings.Repeat("y", 200)
	if got := codegen.ShorterOf(full, longDiff); got != full {
		t.Error("a diff longer than the catalog should not be used")
	}
}
