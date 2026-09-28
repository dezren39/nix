package config_test

import (
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

func res(t *testing.T, c *config.Config, name string) *config.Resolved {
	t.Helper()
	r, err := c.Resolve(name)
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	return r
}

func boolp(b bool) *bool { return &b }

func profileConfig() *config.Config {
	return &config.Config{MCPServers: map[string]*config.Server{
		"always": {Name: "always", Command: "a"},
		"web": {Name: "web", Command: "w", Mcpx: &config.Extras{
			Profiles: []string{"web"}, Default: boolp(false)}},
		"code": {Name: "code", Command: "c", Mcpx: &config.Extras{
			Profiles: []string{"code"}}},
	}}
}

func TestDefaultProfileIncludesEverythingNotOptedOut(t *testing.T) {
	c := profileConfig()
	p := config.Profile{}
	if !p.Includes(res(t, c, "always")) {
		t.Error("a server with no profile settings should be default-on")
	}
	if !p.Includes(res(t, c, "code")) {
		t.Error("belonging to a profile should not remove a server from the default set")
	}
	if p.Includes(res(t, c, "web")) {
		t.Error("default:false should exclude a server when no profile is asked for")
	}
}

func TestNamedProfileAddsToTheDefaultSet(t *testing.T) {
	c := profileConfig()
	p := config.Profile{Names: []string{"web"}}
	if !p.Includes(res(t, c, "web")) {
		t.Error("--profile web should include the web server")
	}
	if !p.Includes(res(t, c, "always")) {
		t.Error("--profile should add to, not replace, the default set")
	}
}

func TestSkipDefaultNarrowsToExactlyTheProfile(t *testing.T) {
	c := profileConfig()
	p := config.Profile{Names: []string{"web"}, SkipDefault: true}
	if !p.Includes(res(t, c, "web")) {
		t.Error("the requested profile must survive")
	}
	if p.Includes(res(t, c, "always")) {
		t.Error("--skip-default should drop the default set")
	}
}

func TestAllProfilesIgnoresEveryExclusion(t *testing.T) {
	c := profileConfig()
	p := config.Profile{All: true}
	for _, n := range []string{"always", "web", "code"} {
		if !p.Includes(res(t, c, n)) {
			t.Errorf("--all-profiles should include %s", n)
		}
	}
}

func TestProfileMatchingIsCaseInsensitive(t *testing.T) {
	c := profileConfig()
	if !(config.Profile{Names: []string{"WEB"}}).Includes(res(t, c, "web")) {
		t.Error("profile names should match case-insensitively")
	}
}

func TestGlobalDefaultOffFlipsTheBaseline(t *testing.T) {
	c := profileConfig()
	c.Pool = config.Extras{Default: boolp(false)}
	if (config.Profile{}).Includes(res(t, c, "code")) {
		t.Error("defaults.default=false should exclude servers that do not opt in")
	}
	// A server can still opt back in.
	c.MCPServers["code"].Mcpx.Default = boolp(true)
	if !(config.Profile{}).Includes(res(t, c, "code")) {
		t.Error("a per-server default should override the global one")
	}
}

func TestAliasSharesAPoolWhenLeasingMatches(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"base":  {Name: "base", Command: "x", Args: []string{"-a"}},
		"view":  {Name: "view", AliasOf: "base", Mcpx: &config.Extras{Tools: []string{"one"}}},
		"other": {Name: "other", AliasOf: "base", Mcpx: &config.Extras{Scope: config.ScopeSession}},
	}}
	base, view, other := res(t, c, "base"), res(t, c, "view"), res(t, c, "other")

	if base.PoolID() != view.PoolID() {
		t.Error("an alias differing only in its tool subset must share the pool")
	}
	if base.PoolID() == other.PoolID() {
		t.Error("an alias with different leasing must not share the pool")
	}
	if view.Command != "base command" && view.Command != base.Command {
		t.Fatalf("an alias should inherit the process definition, got %q", view.Command)
	}
}

func TestAliasKeepsItsOwnView(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"base": {Name: "base", Command: "x", Mcpx: &config.Extras{Description: "full"}},
		"view": {Name: "view", AliasOf: "base", Mcpx: &config.Extras{
			Description: "restricted", Tools: []string{"one"}}},
	}}
	v := res(t, c, "view")
	if v.Description != "restricted" {
		t.Errorf("alias description should be its own, got %q", v.Description)
	}
	if v.Namespace != "view" {
		t.Errorf("alias namespace should be its own, got %q", v.Namespace)
	}
	if !v.VisibleTool("one") || v.VisibleTool("two") {
		t.Error("alias tool filtering is not applied")
	}
}

func TestAliasChainIsRejected(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"base": {Name: "base", Command: "x"},
		"a":    {Name: "a", AliasOf: "base"},
		"b":    {Name: "b", AliasOf: "a"},
	}}
	if _, err := c.Resolve("b"); err == nil {
		t.Fatal("an alias of an alias should be rejected, not silently followed")
	}
}

func TestAliasOfUnknownServerIsRejected(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", AliasOf: "nope"},
	}}
	if _, err := c.Resolve("a"); err == nil {
		t.Fatal("aliasing a server that does not exist must fail")
	}
}

func TestVisibleToolAppliesDenyOverAllow(t *testing.T) {
	c := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: "x", Mcpx: &config.Extras{
			Tools: []string{"one", "two"}, ExcludeTools: []string{"two"}}},
	}}
	r := res(t, c, "a")
	if !r.VisibleTool("one") {
		t.Error("allowlisted tool should be visible")
	}
	if r.VisibleTool("two") {
		t.Error("excludeTools must win over tools")
	}
	if r.VisibleTool("three") {
		t.Error("a tool outside the allowlist should be hidden")
	}
}
