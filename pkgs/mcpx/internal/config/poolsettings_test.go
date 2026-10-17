package config_test

import (
	"encoding/json"
	"strings"
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

func TestWithNothingSetEveryPoolGetsFour(t *testing.T) {
	// No configuration and no variable: the built-in layer in
	// defaults.json decides, and it says four. Scope is session so that the
	// single-key rule in Resolve does not clamp the answer to one.
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Command: "true", Name: "a", Mcpx: &config.Extras{Scope: config.ScopeSession}},
		"b": {Command: "true", Name: "b"},
	}}
	cfg.Pool.Scope = config.ScopeSession
	config.ApplyPoolSettings(cfg, resolvedSet(t))
	for _, name := range []string{"a", "b"} {
		r, err := cfg.Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		if r.Max != 4 {
			t.Errorf("%s: max=%d, want the built-in 4", name, r.Max)
		}
	}
}

func TestTheDaemonPoolMaxReachesEveryPool(t *testing.T) {
	// One daemon-wide value, set once, is the ceiling of every pool that
	// does not state its own. Neither server here has an mcpx block.
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Command: "true", Name: "a"},
		"b": {Command: "true", Name: "b"},
	}}
	config.ApplyPoolSettings(cfg, resolvedSet(t,
		"MCPX_POOL_SCOPE=session", "MCPX_POOL_MAX=5"))
	for _, name := range []string{"a", "b"} {
		r, err := cfg.Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		if r.Max != 5 {
			t.Errorf("%s: max=%d, want the daemon's 5", name, r.Max)
		}
	}
}

func TestTheDaemonPoolMaxFromAConfigurationFileReachesEveryPool(t *testing.T) {
	// The same key written in a configuration file reaches the daemon
	// through the settings layer, not only through the environment.
	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	set := settings.NewSet(sch)
	var doc map[string]any
	if err := json.Unmarshal([]byte(`{"pool":{"max":6,"scope":"session"}}`), &doc); err != nil {
		t.Fatal(err)
	}
	if err := sch.ApplyFile(set, doc, "mcpx.json", 0); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Command: "true", Name: "a"},
		"b": {Command: "true", Name: "b"},
	}}
	config.ApplyPoolSettings(cfg, set)
	for _, name := range []string{"a", "b"} {
		r, err := cfg.Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		if r.Max != 6 {
			t.Errorf("%s: max=%d, want the file's 6", name, r.Max)
		}
	}
}

func TestAPerServerMaxBeatsTheDaemonValueAndOnlyForItsOwnServer(t *testing.T) {
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"own":     {Command: "true", Name: "own", Mcpx: &config.Extras{Max: 2}},
		"inherit": {Command: "true", Name: "inherit"},
	}}
	config.ApplyPoolSettings(cfg, resolvedSet(t,
		"MCPX_POOL_SCOPE=session", "MCPX_POOL_MAX=5"))

	own, err := cfg.Resolve("own")
	if err != nil {
		t.Fatal(err)
	}
	inherit, err := cfg.Resolve("inherit")
	if err != nil {
		t.Fatal(err)
	}
	if own.Max != 2 {
		t.Errorf("own: max=%d, want the server's own 2", own.Max)
	}
	if inherit.Max != 5 {
		t.Errorf("inherit: max=%d, want the daemon's 5", inherit.Max)
	}
}

func TestAnInvalidDaemonPoolMaxIsRefused(t *testing.T) {
	// Each spelling of a bad value is refused by every layer that accepts
	// the setting, and the refusal names the setting. A value that got past
	// one layer would be the silent fallback this exists to prevent: a zero
	// that resolves to four, or a negative that resolves to one.
	for _, raw := range []string{"0", "-3", "abc", "1.5"} {
		sch, err := settings.New(settings.Registry())
		if err != nil {
			t.Fatal(err)
		}
		set := settings.NewSet(sch)

		err = sch.ApplyEnv(set, []string{"MCPX_POOL_MAX=" + raw})
		if err == nil || !strings.Contains(err.Error(), "pool.max") {
			t.Errorf("MCPX_POOL_MAX=%s: want a refusal naming pool.max, got %v", raw, err)
		}
		if err := set.SetRuntime("pool.max", raw); err == nil {
			t.Errorf("runtime pool.max=%s: want a refusal", raw)
		}
	}
}

func TestTheSmallestDaemonPoolMaxIsOne(t *testing.T) {
	// The boundary of the refusal above: one is the least a pool can be.
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Command: "true", Name: "a"},
	}}
	config.ApplyPoolSettings(cfg, resolvedSet(t,
		"MCPX_POOL_SCOPE=session", "MCPX_POOL_MAX=1"))
	r, err := cfg.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Max != 1 {
		t.Errorf("max=%d, want 1", r.Max)
	}
}
