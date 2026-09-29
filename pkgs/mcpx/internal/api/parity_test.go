package api_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// routePattern finds every route the daemon registers.
//
// Read out of the source rather than out of a mux, because http.ServeMux
// will not say what has been registered on it and the alternative -- a
// parallel list of patterns kept by hand -- is exactly the drift this test
// exists to catch.
var routePattern = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) (/v1[^"]*)"`)

func daemonRoutes(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "daemon")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range routePattern.FindAllStringSubmatch(string(b), -1) {
			out[m[1]+" "+m[2]] = e.Name()
		}
	}
	if len(out) == 0 {
		t.Fatal("no routes found; the pattern that reads them has stopped matching")
	}
	return out
}

// TestEveryDaemonRouteIsDeclared is the whole point of the table.
//
// It must be impossible to add a /v1 route that MCP cannot reach, because
// the failure mode is silent: the endpoint works, the CLI uses it, and an
// agent speaking MCP simply cannot get at the thing everyone else can.
func TestEveryDaemonRouteIsDeclared(t *testing.T) {
	routes := daemonRoutes(t)
	declared := map[string]bool{}
	for _, op := range api.Ops() {
		declared[op.Route()] = true
	}
	var missing []string
	for route, file := range routes {
		if !declared[route] {
			missing = append(missing, route+" (registered in daemon/"+file+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("routes with no entry in internal/api/ops.go:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

func TestEveryDeclaredOpHasARoute(t *testing.T) {
	routes := daemonRoutes(t)
	var missing []string
	for _, op := range api.Ops() {
		if _, ok := routes[op.Route()]; !ok {
			missing = append(missing, op.Name+": "+op.Route())
		}
	}
	if len(missing) > 0 {
		t.Errorf("declared operations the daemon does not serve:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

func TestEveryNonStreamingOpIsReachableAsAnMCPTool(t *testing.T) {
	// Built with a nil caller: only the names and schemas matter here, and
	// nothing is invoked.
	srv := mcpserver.New(nil, "mcpx", "test").WithExtras(mcpserver.OpTools(nil))
	tools := map[string]bool{}
	for _, tool := range srv.Tools() {
		tools[tool.Name] = true
	}
	for _, op := range api.Ops() {
		if op.Streams {
			if tools["mcpx_"+op.Name] {
				t.Errorf("%s streams and must not be a tool: a tool result is one "+
					"value and an event stream does not end", op.Name)
			}
			continue
		}
		if !tools[op.ToolName()] {
			t.Errorf("%s (%s) has no MCP tool; expected %s", op.Name, op.Route(), op.ToolName())
		}
	}
}

func TestOpNamesAndToolNamesAreUnique(t *testing.T) {
	names := map[string]bool{}
	for _, op := range api.Ops() {
		if names[op.Name] {
			t.Errorf("two operations are called %q", op.Name)
		}
		names[op.Name] = true
	}
	seen := map[string]string{}
	srv := mcpserver.New(nil, "mcpx", "test").WithExtras(mcpserver.OpTools(nil))
	for _, tool := range srv.Tools() {
		if prev, dup := seen[tool.Name]; dup {
			t.Errorf("duplicate tool %q (also from %s)", tool.Name, prev)
		}
		seen[tool.Name] = "tools"
	}
}

func TestAdminOperationsAreAnnotatedAsSuch(t *testing.T) {
	// A client scoping on annotations is the only mechanism the protocol
	// offers, so an operation that stops the daemon while claiming to be
	// read-only is actively misleading.
	for _, op := range api.Ops() {
		ann := op.Annotations()
		if ro, _ := ann["readOnlyHint"].(bool); ro && op.Mutating {
			t.Errorf("%s mutates but is annotated read-only", op.Name)
		}
		if op.Destructive && !op.Mutating {
			t.Errorf("%s is destructive but not marked mutating", op.Name)
		}
		if op.Admin && !op.Mutating {
			t.Errorf("%s is privileged but not marked mutating", op.Name)
		}
		if op.Destructive {
			if d, _ := ann["destructiveHint"].(bool); !d {
				t.Errorf("%s is destructive but does not say so in its annotations", op.Name)
			}
		}
		if op.Admin && !strings.Contains(op.ToolDescription(), "Privileged") {
			t.Errorf("%s does not tell a caller that it is privileged", op.Name)
		}
	}
}
