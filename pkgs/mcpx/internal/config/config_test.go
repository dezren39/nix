package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadsPlainMCPServersFormat(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{
	  "mcpServers": {
	    "a": { "command": "x", "args": ["1"] },
	    "b": { "url": "https://example.com/mcp" }
	  }
	}`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.MCPServers) != 2 {
		t.Fatalf("want 2 servers, got %d", len(c.MCPServers))
	}
	if c.MCPServers["b"].Transport != "http" {
		t.Fatalf("a url-only server should default to http, got %q", c.MCPServers["b"].Transport)
	}
}

func TestJSONCCommentsAreStripped(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{
	  // a line comment
	  "mcpServers": {
	    /* block */
	    "a": { "command": "x" } // trailing
	  }
	}`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("jsonc should parse: %v", err)
	}
	if _, ok := c.MCPServers["a"]; !ok {
		t.Fatal("server a missing")
	}
}

func TestCommentStrippingIgnoresStringContents(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{
	  "mcpServers": {
	    "a": { "command": "x", "args": ["https://example.com//path", "a/*b*/c"] }
	  }
	}`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	args := c.MCPServers["a"].Args
	if len(args) != 2 || args[0] != "https://example.com//path" || args[1] != "a/*b*/c" {
		t.Fatalf("string contents were mangled: %q", args)
	}
}

func TestResolveAppliesDefaults(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x"},
	}}
	r, err := c.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != config.ModeShared {
		t.Fatalf("default mode should be shared, got %q", r.Mode)
	}
	if r.Max != 1 {
		t.Fatalf("shared mode is always max 1, got %d", r.Max)
	}
	if r.IdleTimeout != config.DefaultIdleTimeout {
		t.Fatalf("idle timeout default wrong: %s", r.IdleTimeout)
	}
	if r.Namespace != "a" {
		t.Fatalf("namespace should default to the server name, got %q", r.Namespace)
	}
}

func TestSharedModeForcesMaxToOne(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Mcpx: &config.Extras{Mode: config.ModeShared, Max: 9}},
	}}
	r, _ := c.Resolve("a")
	if r.Max != 1 {
		t.Fatalf("shared mode must collapse to one process, got max=%d", r.Max)
	}
}

func TestFileDefaultsApplyToEveryServer(t *testing.T) {
	c := &config.Config{
		Defaults: config.Extras{Mode: config.ModeSession, Max: 7, IdleTimeout: "1m"},
		MCPServers: map[string]*config.Server{
			"a": {Name: "a", Command: "x"},
			"b": {Name: "b", Command: "y", Mcpx: &config.Extras{Max: 2}},
		},
	}
	a, _ := c.Resolve("a")
	b, _ := c.Resolve("b")
	if a.Max != 7 || a.Mode != config.ModeSession || a.IdleTimeout != time.Minute {
		t.Fatalf("defaults not applied: %+v", a)
	}
	if b.Max != 2 {
		t.Fatalf("per-server value should win, got %d", b.Max)
	}
}

func TestInvalidModeRejected(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Mcpx: &config.Extras{Mode: "nonsense"}},
	}}
	if _, err := c.Resolve("a"); err == nil {
		t.Fatal("an unknown mode must be rejected")
	}
}

func TestDisabledServersAreSkipped(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x"},
		"b": {Name: "b", Command: "y", Mcpx: &config.Extras{Disabled: true}},
	}}
	all, err := c.ResolveAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Name != "a" {
		t.Fatalf("disabled server leaked: %+v", all)
	}
}

func TestServerNeedsCommandOrURL(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{"mcpServers": {"a": {}}}`)
	if _, err := config.Load(p); err == nil {
		t.Fatal("a server with neither command nor url must be rejected")
	}
}

func TestSanitizeNamespace(t *testing.T) {
	cases := map[string]string{
		"fff-nix":         "fff_nix",
		"chrome-devtools": "chrome_devtools",
		"7up":             "_7up",
		"ok_name":         "ok_name",
		"a.b c":           "a_b_c",
	}
	for in, want := range cases {
		if got := config.SanitizeNamespace(in); got != want {
			t.Errorf("SanitizeNamespace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchPathPrefersProjectThenUser(t *testing.T) {
	t.Setenv("MCPX_CONFIG", "")
	paths := config.SearchPath()
	if len(paths) == 0 {
		t.Fatal("search path is empty")
	}
	// `.config/mcpx/config.json` is the most specific project spelling and must
	// be tried before the flat `.mcpx.json`.
	if got := paths[0]; !strings.HasSuffix(got, filepath.Join(".config", "mcpx", "config.json")) {
		t.Fatalf("most specific project config should come first, got %q", got)
	}
	var sawFlat bool
	for _, p := range paths {
		if filepath.Base(p) == ".mcpx.json" {
			sawFlat = true
		}
	}
	if !sawFlat {
		t.Fatal(".mcpx.json must still be on the search path")
	}
}

func TestMCPXConfigEnvOverridesEverything(t *testing.T) {
	t.Setenv("MCPX_CONFIG", "/tmp/explicit.json")
	paths := config.SearchPath()
	if len(paths) != 1 || paths[0] != "/tmp/explicit.json" {
		t.Fatalf("MCPX_CONFIG should be the only candidate, got %v", paths)
	}
}

func TestMissingConfigIsNotAnError(t *testing.T) {
	t.Setenv("MCPX_CONFIG", filepath.Join(t.TempDir(), "definitely-missing.json"))
	if _, err := config.Load(""); err != nil {
		t.Fatalf("a missing config should yield empty defaults, got %v", err)
	}
}
