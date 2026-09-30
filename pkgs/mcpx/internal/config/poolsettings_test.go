package config_test

import (
	"testing"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/settings"
)

func resolvedSet(t *testing.T, environ ...string) *settings.Set {
	t.Helper()
	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	set := settings.NewSet(sch)
	if err := sch.ApplyEnv(set, environ); err != nil {
		t.Fatal(err)
	}
	return set
}

func TestPoolSettingsReachTheResolvedServer(t *testing.T) {
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"demo": {Command: "true", Name: "demo"},
	}}
	config.ApplyPoolSettings(cfg, resolvedSet(t,
		"MCPX_POOL_SHARING=exclusive",
		"MCPX_POOL_SCOPE=session",
		"MCPX_POOL_MAX=7",
		"MCPX_POOL_MIN=2",
		"MCPX_POOL_IDLE_TIMEOUT=90s"))

	r, err := cfg.Resolve("demo")
	if err != nil {
		t.Fatal(err)
	}
	if r.Sharing != config.SharingExclusive {
		t.Errorf("sharing: got %q", r.Sharing)
	}
	if r.Scope != config.ScopeSession {
		t.Errorf("scope: got %q", r.Scope)
	}
	if r.Max != 7 || r.Min != 2 {
		t.Errorf("max/min: got %d/%d", r.Max, r.Min)
	}
	if r.IdleTimeout.String() != "1m30s" {
		t.Errorf("idleTimeout: got %s", r.IdleTimeout)
	}
}

func TestAServerStillOverridesThePoolSettings(t *testing.T) {
	// The fold writes the layer a config file's `pool` block writes. Writing
	// any higher would let a variable meant as a default silently beat a
	// deliberate per-server choice.
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"demo": {Command: "true", Name: "demo",
			Mcpx: &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeSession, Max: 3}},
	}}
	config.ApplyPoolSettings(cfg, resolvedSet(t,
		"MCPX_POOL_SHARING=exclusive", "MCPX_POOL_MAX=7"))

	r, err := cfg.Resolve("demo")
	if err != nil {
		t.Fatal(err)
	}
	if r.Sharing != config.SharingShared || r.Max != 3 {
		t.Errorf("the server's own values should win: %q max=%d", r.Sharing, r.Max)
	}
}

func TestAnUnsetPoolSettingLeavesTheFileAlone(t *testing.T) {
	// Copying the declared default in would turn "nobody said" into "somebody
	// said four", which is a different thing to every layer above.
	cfg := &config.Config{
		MCPServers: map[string]*config.Server{"demo": {Command: "true", Name: "demo"}},
		Pool:       config.Extras{Max: 9, Scope: config.ScopeCwd},
	}
	config.ApplyPoolSettings(cfg, resolvedSet(t))
	if cfg.Pool.Max != 9 || cfg.Pool.Scope != config.ScopeCwd {
		t.Errorf("the file's pool block was overwritten by defaults: %+v", cfg.Pool)
	}
}
