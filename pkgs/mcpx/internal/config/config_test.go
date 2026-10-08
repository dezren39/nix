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

func TestAnUnknownKeyInTheMcpxBlockIsRefused(t *testing.T) {
	// The "mcpx" block is mcpx's alone, so a key it does not read is a typo
	// or a name that no longer exists. encoding/json dropped it silently,
	// which is how `mcpx init` shipped "mode": "session" -- split into
	// sharing and scope long before -- and every browser it configured ran
	// as one shared process.
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{"mcpServers":{"browser":{"command":"x","mcpx":{"mode":"session","max":4}}}}`)
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("a config whose mcpx block says \"mode\" loaded; the key would have been dropped")
	}
	for _, want := range []string{p, `"browser"`, `"mode"`, "sharing", "scope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal should name %s:\n%v", want, err)
		}
	}

	// Nested too: a typo inside the block's logging object is the same
	// mistake one level down.
	p = write(t, dir, "d.json", `{"mcpServers":{"a":{"command":"x","mcpx":{"logging":{"levle":"debug"}}}}}`)
	if _, err := config.Load(p); err == nil || !strings.Contains(err.Error(), "levle") {
		t.Errorf("a typo inside mcpx.logging should be refused by name, got %v", err)
	}

	// And every key the block does read still loads.
	p = write(t, dir, "e.json", `{"mcpServers":{"a":{"command":"x","mcpx":{"sharing":"exclusive","scope":"session","max":2,"min":0,
		"idleTimeout":"1m","callTimeout":"1m","startTimeout":"1m","namespace":"aa","disabled":false,
		"logging":{"level":"debug"},"prelude":"p","profiles":["w"],"default":true,"description":"d",
		"tools":["t"],"excludeTools":["u"]}}}}`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("a block using only known keys was refused: %v", err)
	}
	r, err := c.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Scope != config.ScopeSession || r.Sharing != config.SharingExclusive {
		t.Fatalf("got sharing=%q scope=%q", r.Sharing, r.Scope)
	}
}

func TestServerKeysAnotherHostWritesAreRecordedNotRefused(t *testing.T) {
	// A server entry is shared with every other MCP host. Claude Code
	// writes "type" and Cline writes "alwaysAllow"; refusing them would
	// make a config that works everywhere else fail here.
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{"mcpServers":{"a":{"type":"stdio","command":"x","alwaysAllow":["t"]}}}`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("another host's keys should load: %v", err)
	}
	var got []string
	for _, k := range c.Ignored {
		if k.File != p {
			t.Errorf("ignored key %s.%s names file %q, want %q", k.Server, k.Key, k.File, p)
		}
		got = append(got, k.Server+"."+k.Key)
	}
	if strings.Join(got, ",") != "a.alwaysAllow,a.type" {
		t.Errorf("ignored = %v, want a.alwaysAllow and a.type", got)
	}

	// The same through the search path, which merges several files.
	//
	// SearchPathFrom walks up to / and then consults $XDG_CONFIG_HOME, $HOME
	// and /etc, so without this the test reads whatever the developer running
	// it happens to have configured. That was harmless while unknown keys were
	// ignored; now that an unknown key in an "mcpx" block is an error, one
	// stale key in a real ~/.mcpx.json would fail this test on that machine
	// and nowhere else.
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	sub := filepath.Join(dir, "proj")
	write(t, sub, ".mcpx.json", `{"mcpServers":{"b":{"command":"y","trust":true}}}`)
	merged, err := config.LoadFrom("", sub)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range merged.Ignored {
		if k.Server == "b" && k.Key == "trust" && strings.HasSuffix(k.File, filepath.Join("proj", ".mcpx.json")) {
			found = true
		}
	}
	if !found {
		t.Errorf("a merged load lost the ignored key: %+v", merged.Ignored)
	}
}

func TestFileDefaultsApplyToEveryServer(t *testing.T) {
	c := &config.Config{
		Pool: config.Extras{Scope: config.ScopeSession, Max: 7, IdleTimeout: "1m"},
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

func TestPoolBlockAppliesToEveryServer(t *testing.T) {
	// The block is named for the dotted path the registry uses, so that
	// `mcpx config --schema` and a configuration file agree on what these
	// knobs are called.
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

func TestAServerOverridesThePoolBlock(t *testing.T) {
	// The block is a default, not a mandate: a server that states a value
	// keeps it.
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcpx.json")
	if err := os.WriteFile(path, []byte(`{
		"mcpServers": { "a": { "command": "true", "mcpx": { "max": 3 } } },
		"pool": { "max": 7, "scope": "session" }
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(path)
	r, err := cfg.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Max != 3 {
		t.Errorf("the server's own value should win: got %d", r.Max)
	}
	if r.Scope != config.ScopeSession {
		t.Errorf("pool.scope should still apply where the server is silent: %v", r.Scope)
	}
}

func TestSearchPathDedupesUserConfig(t *testing.T) {
	t.Setenv("MCPX_CONFIG", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	userCfg := write(t, home, filepath.Join(".config", "mcpx", "config.json"), `{}`)

	// The upward walk passes through $HOME, so without dedupe the user
	// config appeared twice: once from the walk, once from the home
	// fallback. The duplicated source changed the daemon key, making a
	// daemon started from one cwd invisible to a CLI run from another
	// (e.g. `mcpx status` said "not running" for a live systemd daemon).
	proj := filepath.Join(home, "some", "project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, wd := range []string{home, proj} {
		paths := config.SearchPathFrom(wd)
		var count int
		seen := map[string]bool{}
		for _, p := range paths {
			if seen[p] {
				t.Fatalf("SearchPathFrom(%q) has duplicate %q in %v", wd, p, paths)
			}
			seen[p] = true
			if p == userCfg {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("SearchPathFrom(%q) lists the user config %d times, want 1: %v", wd, count, paths)
		}
	}

	// Nearest-first precedence is preserved: a project config still wins.
	projCfg := write(t, proj, filepath.Join(".config", "mcpx", "config.json"), `{}`)
	if paths := config.SearchPathFrom(proj); len(paths) == 0 || paths[0] != projCfg {
		t.Fatalf("project config should come first, got %v", paths)
	}
}

// A key encoding/json reads is never reported as one mcpx ignored.
//
// json prefers an exact field match and otherwise accepts a case-insensitive
// one, so {"Command": "echo"} populates Server.Command. Checking document keys
// against the tag spellings alone put those in Config.Ignored anyway: doctor
// warned about a config that works, and plumbing.strictUnknownKeys refused it.
func TestAKeyJSONReadsIsNotReportedIgnored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := write(t, dir, "c.json", `{"mcpServers":{"s":{"Command":"echo","Args":["hi"]}}}`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := c.MCPServers["s"].Command; got != "echo" {
		t.Fatalf(`"Command" did not decode (got %q); the premise of this test is gone`, got)
	}
	if len(c.Ignored) != 0 {
		t.Errorf("keys json decoded were reported ignored: %+v", c.Ignored)
	}
}

// A server's own protocol key overrides upstream.protocol, so it accepts the
// same aliases and Resolve hands the pool only the canonical name.
func TestResolveNormalisesAServersProtocolAlias(t *testing.T) {
	for given, want := range map[string]string{
		"modern": "prefer-discover", "prefer-newest": "prefer-discover",
		"legacy": "prefer-initialize", "prefer-session": "prefer-initialize",
		"force-modern": "force-discover", "force-stateless": "force-discover",
		"force-legacy": "force-initialize", "Force-Session": "force-initialize",
		"follow": "follow", "prefer-discover": "prefer-discover",
	} {
		c := &config.Config{MCPServers: map[string]*config.Server{
			"a": {Name: "a", Command: "x", Protocol: given},
		}}
		r, err := c.Resolve("a")
		if err != nil {
			t.Errorf("%q: %v", given, err)
			continue
		}
		if r.Protocol != want {
			t.Errorf("%q resolved to %q, want %q", given, r.Protocol, want)
		}
	}
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Protocol: "newest"},
	}}
	_, err := c.Resolve("a")
	if err == nil || !strings.Contains(err.Error(), "prefer-discover") || !strings.Contains(err.Error(), "force-legacy") {
		t.Errorf("an unknown protocol should be refused, listing names and aliases: %v", err)
	}
}

func TestPreconditionsGating(t *testing.T) {
	// 1. Server-level missing secret
	r := &config.Resolved{
		Server:          &config.Server{Name: "test-server"},
		RequiresSecrets: []string{"NON_EXISTENT_TEST_ENV_VAR_XYZ"},
	}
	ok, reason := r.PassesPreconditions("foo", nil)
	if ok || !strings.Contains(reason, "NON_EXISTENT_TEST_ENV_VAR_XYZ") {
		t.Fatalf("expected failure due to missing secret, got ok=%v, reason=%q", ok, reason)
	}
	if r.VisibleTool("foo") {
		t.Fatal("expected foo to not be visible when secret precondition fails")
	}

	// 2. Server-level satisfied secret
	t.Setenv("TEST_EXISTING_ENV_VAR", "secret-value")
	r.RequiresSecrets = []string{"TEST_EXISTING_ENV_VAR"}
	ok, reason = r.PassesPreconditions("foo", nil)
	if !ok {
		t.Fatalf("expected preconditions to pass, got reason: %s", reason)
	}
	if !r.VisibleTool("foo") {
		t.Fatal("expected foo to be visible when preconditions pass")
	}

	// 3. Server-level required server
	r.RequiresServers = []string{"auth-server"}
	isActive := false
	serverChecker := func(srv string) bool {
		return srv == "auth-server" && isActive
	}
	ok, reason = r.PassesPreconditions("foo", serverChecker)
	if ok || !strings.Contains(reason, "auth-server") {
		t.Fatalf("expected failure when dependent server inactive, got ok=%v, reason=%q", ok, reason)
	}

	isActive = true
	ok, reason = r.PassesPreconditions("foo", serverChecker)
	if !ok {
		t.Fatalf("expected preconditions to pass when dependent server active, got reason: %s", reason)
	}

	// 4. Tool-level precondition
	r.RequiresSecrets = nil
	r.RequiresServers = nil
	r.Preconditions = map[string]config.Precondition{
		"export_csv": {RequiresSecret: "CSV_EXPORT_TOKEN"},
	}

	// normal tool passes
	ok, _ = r.PassesPreconditions("read_data", nil)
	if !ok {
		t.Fatal("read_data should pass preconditions")
	}
	if !r.VisibleTool("read_data") {
		t.Fatal("read_data should be visible")
	}

	// gated tool fails initially
	ok, reason = r.PassesPreconditions("export_csv", nil)
	if ok || !strings.Contains(reason, "CSV_EXPORT_TOKEN") {
		t.Fatalf("export_csv should fail without CSV_EXPORT_TOKEN, got ok=%v, reason=%q", ok, reason)
	}
	if r.VisibleTool("export_csv") {
		t.Fatal("export_csv should not be visible when missing secret")
	}

	// gated tool passes once secret is set
	t.Setenv("CSV_EXPORT_TOKEN", "12345")
	ok, _ = r.PassesPreconditions("export_csv", nil)
	if !ok {
		t.Fatal("export_csv should pass once secret is set")
	}
	if !r.VisibleTool("export_csv") {
		t.Fatal("export_csv should be visible once secret is set")
	}
}

