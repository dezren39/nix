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
	if r.Sharing != config.SharingShared {
		t.Fatalf("default sharing should be shared, got %q", r.Sharing)
	}
	if r.Scope != config.ScopeGlobal {
		t.Fatalf("default scope should be global, got %q", r.Scope)
	}
	if r.Max != 1 {
		t.Fatalf("a single-key scope is always max 1, got %d", r.Max)
	}
	if r.IdleTimeout != config.DefaultIdleTimeout {
		t.Fatalf("idle timeout default wrong: %s", r.IdleTimeout)
	}
	if r.Namespace != "a" {
		t.Fatalf("namespace should default to the server name, got %q", r.Namespace)
	}
}

func TestGlobalScopeForcesMaxToOne(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Mcpx: &config.Extras{Scope: config.ScopeGlobal, Max: 9}},
	}}
	r, _ := c.Resolve("a")
	if r.Max != 1 {
		t.Fatalf("one key can only need one process, got max=%d", r.Max)
	}
}

func TestNonGlobalScopeKeepsMax(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Mcpx: &config.Extras{Scope: config.ScopeSession, Max: 9}},
	}}
	r, _ := c.Resolve("a")
	if r.Max != 9 {
		t.Fatalf("a multi-key scope must keep its max, got %d", r.Max)
	}
}

func TestUnknownKeysAreIgnored(t *testing.T) {
	// Go's json decoder ignores unknown fields. There are no users of the
	// retired `mode` key, so it needs no special handling -- but a config
	// carrying one must still load rather than fail.
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{"mcpServers":{"a":{"command":"x","mcpx":{"scope":"session"}}}}`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Scope != config.ScopeSession {
		t.Fatalf("got %q", r.Scope)
	}
}

func TestFileDefaultsApplyToEveryServer(t *testing.T) {
	c := &config.Config{
		Defaults: config.Extras{Scope: config.ScopeSession, Max: 7, IdleTimeout: "1m"},
		MCPServers: map[string]*config.Server{
			"a": {Name: "a", Command: "x"},
			"b": {Name: "b", Command: "y", Mcpx: &config.Extras{Max: 2}},
		},
	}
	a, _ := c.Resolve("a")
	b, _ := c.Resolve("b")
	if a.Max != 7 || a.Scope != config.ScopeSession || a.IdleTimeout != time.Minute {
		t.Fatalf("defaults not applied: %+v", a)
	}
	if b.Max != 2 {
		t.Fatalf("per-server value should win, got %d", b.Max)
	}
}

func TestInvalidSharingRejected(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Mcpx: &config.Extras{Sharing: "nonsense"}},
	}}
	if _, err := c.Resolve("a"); err == nil {
		t.Fatal("an unknown sharing must be rejected")
	}
}

func TestInvalidScopeRejectedAndListsValidOnes(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Mcpx: &config.Extras{Scope: "nonsense"}},
	}}
	_, err := c.Resolve("a")
	if err == nil {
		t.Fatal("an unknown scope must be rejected")
	}
	if !strings.Contains(err.Error(), "worktree") {
		t.Errorf("error should list valid scopes: %v", err)
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

func TestLoggingConfigIsParsed(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{
	  "logging": { "format": "logfmt", "level": "debug", "source": "debug" },
	  "mcpServers": { "a": { "command": "x", "mcpx": { "logging": { "level": "warn" } } } }
	}`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Logging.Format != "logfmt" || c.Logging.Level != "debug" || c.Logging.Source != "debug" {
		t.Fatalf("logging block not parsed: %+v", c.Logging)
	}
	r, err := c.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.LogLevel != "warn" {
		t.Errorf("per-server level should resolve, got %q", r.LogLevel)
	}
}

func TestPoolBlockAppliesLikeDefaults(t *testing.T) {
	// "pool" is what `mcpx config --schema` calls these knobs and "defaults"
	// is what reads naturally beside mcpServers. Both exist because having
	// one of them silently do nothing would be worse than having two.
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcpx.json")
	if err := os.WriteFile(path, []byte(`{
		"mcpServers": { "a": { "command": "true" } },
		"pool": { "max": 7, "sharing": "exclusive", "scope": "session", "idleTimeout": "90s" }
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cfg.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Max != 7 {
		t.Errorf("pool.max = %d, want 7", r.Max)
	}
	if r.Sharing != config.SharingExclusive {
		t.Errorf("pool.sharing = %v", r.Sharing)
	}
	if r.IdleTimeout != 90*time.Second {
		t.Errorf("pool.idleTimeout = %v", r.IdleTimeout)
	}
}

func TestDefaultsBlockWinsOverPoolBlock(t *testing.T) {
	// The older spelling wins, because an existing config already uses it
	// and should not change meaning by the addition of a synonym.
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcpx.json")
	if err := os.WriteFile(path, []byte(`{
		"mcpServers": { "a": { "command": "true" } },
		"pool":     { "max": 7, "scope": "session" },
		"defaults": { "max": 3 }
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(path)
	r, err := cfg.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Max != 3 {
		t.Errorf("defaults.max should win: got %d", r.Max)
	}
	if r.Scope != config.ScopeSession {
		t.Errorf("pool.scope should still apply where defaults is silent: %v", r.Scope)
	}
}
