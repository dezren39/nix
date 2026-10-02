package config_test

import (
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

func TestToolPatternsMatchExactGlobAndRegexp(t *testing.T) {
	for _, c := range []struct {
		pattern string
		yes     []string
		no      []string
	}{
		{"delete_datadog_workflow", []string{"delete_datadog_workflow"}, []string{"delete_datadog_workflows", "x_delete_datadog_workflow"}},
		// Anchored: a glob covers the whole name, unlike a regexp.
		{"delete_*", []string{"delete_", "delete_x", "delete_a/b"}, []string{"undelete_x", "Delete_x"}},
		{"*_monitor", []string{"create_datadog_monitor"}, []string{"create_datadog_monitors"}},
		{"*delete*", []string{"delete_x", "bulk_delete", "undelete"}, []string{"remove"}},
		{"get_?", []string{"get_a"}, []string{"get_", "get_ab"}},
		{"[cu]pdate_*", []string{"update_x", "cpdate_x"}, []string{"xpdate_x"}},
		{"[!u]pdate_*", []string{"xpdate_x"}, []string{"update_x"}},
		{"[]]x", []string{"]x"}, []string{"x"}},
		{"a.b*", []string{"a.b", "a.bc"}, []string{"axb"}},
		// Unanchored, as regexps are.
		{"/delete/", []string{"delete_x", "bulk_delete"}, []string{"remove"}},
		{"/^(create|update)_.*_monitor$/", []string{"create_datadog_monitor"}, []string{"search_datadog_monitor", "create_datadog_monitors"}},
	} {
		p, err := config.CompileToolPatterns([]string{c.pattern})
		if err != nil {
			t.Fatalf("%q: %v", c.pattern, err)
		}
		for _, n := range c.yes {
			if !p.Match(n) {
				t.Errorf("%q should match %q", c.pattern, n)
			}
		}
		for _, n := range c.no {
			if p.Match(n) {
				t.Errorf("%q should not match %q", c.pattern, n)
			}
		}
	}
}

func TestABadToolPatternIsAConfigError(t *testing.T) {
	for _, bad := range []string{"/(/", "delete_[", ""} {
		if _, err := config.CompileToolPatterns([]string{bad}); err == nil {
			t.Errorf("%q: a pattern that cannot match as written must be refused", bad)
		}
	}
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Mcpx: &config.Extras{ExcludeTools: []string{"/(/"}}},
	}}
	if _, err := c.Resolve("a"); err == nil || !strings.Contains(err.Error(), "excludeTools") {
		t.Fatalf("Resolve should name the field: %v", err)
	}
}

// The README says a top-level pool block applies tools and excludeTools to
// every server; Resolve read them from the server alone.
func TestPoolToolListsReachEveryServer(t *testing.T) {
	c := &config.Config{
		Pool: config.Extras{Tools: []string{"get_*", "delete_*"}, ExcludeTools: []string{"delete_*"}},
		MCPServers: map[string]*config.Server{
			"plain": {Name: "plain", Command: "x"},
			"own": {Name: "own", Command: "x", Mcpx: &config.Extras{
				Tools: []string{"*"}, ExcludeTools: []string{"execute_*"}}},
		},
	}
	plain := res(t, c, "plain")
	if !plain.VisibleTool("get_x") || plain.VisibleTool("delete_x") || plain.VisibleTool("other") {
		t.Error("a server with no lists of its own should take the pool's")
	}
	own := res(t, c, "own")
	if !own.VisibleTool("other") {
		t.Error("a server's own tools list replaces the pool's")
	}
	if own.VisibleTool("execute_x") {
		t.Error("a server's own exclusion should apply")
	}
	if own.VisibleTool("delete_x") {
		t.Error("the pool's exclusion must still apply when a server adds its own")
	}
}
