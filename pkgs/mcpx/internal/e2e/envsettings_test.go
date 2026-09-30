package e2e_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The variables #172 found read by hand and declared nowhere. Each is now a
// setting, and a setting is only real if every surface it claims changes
// what happens -- the #179 lesson, where a guard proved a path was read and
// nothing proved that reading it did anything. So each case below turns the
// knob one way and observes the outcome, and one case leaves it alone and
// observes the opposite.

// withEnv is the same installation with extra variables, sharing its daemon.
func (e *env) withEnv(kv ...string) *env {
	c := *e
	c.envVars = append(append([]string{}, e.envVars...), kv...)
	return &c
}

func (e *env) writeConfig(t *testing.T, body string) {
	t.Helper()
	cfg := strings.ReplaceAll(body, "FAKE", e.fake)
	if err := os.WriteFile(filepath.Join(e.dir, ".mcpx.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoggingTraceIsReachableFromEverySurface(t *testing.T) {
	for _, c := range []struct {
		name  string
		on    bool
		setup func(t *testing.T, e *env) *env
	}{
		{"unset", false, func(_ *testing.T, e *env) *env { return e }},
		{"MCPX_TRACE", true, func(_ *testing.T, e *env) *env { return e.withEnv("MCPX_TRACE=1") }},
		{"config key", true, func(t *testing.T, e *env) *env {
			e.writeConfig(t, `{"logging": {"trace": true}, "mcpServers": {
  "demo": { "command": "FAKE", "mcpx": { "sharing": "shared", "scope": "global" } } }}`)
			return e
		}},
		// The flag belongs to `mcpx daemon`; the arguments an auto-started
		// daemon is given are how a test reaches it without owning a
		// foreground process.
		{"flag", true, func(_ *testing.T, e *env) *env {
			return e.withEnv("MCPX_AUTOSTART_ARGS=daemon,--detached,--logging-trace")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := c.setup(t, newEnv(t, oneServer))
			e.run("call", "demo.echo", `{"message":"traced"}`)
			out := e.run("log", "--grep", "call demo.echo", "--limit", "5")
			traced := strings.Contains(out, "instance=demo#")
			if traced != c.on {
				t.Errorf("trace on=%v, but the daemon log says otherwise:\n%s", c.on, out)
			}
		})
	}
}

// TestScriptPermissionsIsReachableFromEverySurface. Before #172 only the
// hand-read MCPX_PERMISSIONS and --permissions narrowed a local run; the
// registry's own MCPX_SCRIPT_PERMISSIONS, --script-permissions and the
// script.permissions key were parsed, reported by `mcpx settings`, and
// ignored.
func TestScriptPermissionsIsReachableFromEverySurface(t *testing.T) {
	const probe = `try { await Deno.readTextFile("/etc/hosts"); console.log("ALLOWED"); }
		catch (err) { console.log("denied:", (err as Error).name); }`
	base := newEnv(t, oneServer)

	check := func(t *testing.T, e *env, wantDenied bool, args ...string) {
		t.Helper()
		out := e.run(append(append([]string{"exec"}, args...), probe)...)
		denied := strings.Contains(out, "denied") && !strings.Contains(out, "ALLOWED")
		if denied != wantDenied {
			t.Errorf("denied=%v, want %v:\n%s", denied, wantDenied, out)
		}
	}

	t.Run("unset", func(t *testing.T) { check(t, base, false) })
	t.Run("MCPX_PERMISSIONS", func(t *testing.T) {
		check(t, base.withEnv("MCPX_PERMISSIONS=strict"), true)
	})
	t.Run("MCPX_SCRIPT_PERMISSIONS", func(t *testing.T) {
		check(t, base.withEnv("MCPX_SCRIPT_PERMISSIONS=strict"), true)
	})
	t.Run("flag", func(t *testing.T) { check(t, base, true, "--script-permissions", "strict") })
	t.Run("config key", func(t *testing.T) {
		e := newEnv(t, oneServer)
		e.writeConfig(t, `{"script": {"permissions": "strict"}, "mcpServers": {
  "demo": { "command": "FAKE", "mcpx": { "sharing": "shared", "scope": "global" } } }}`)
		check(t, e, true)
	})
}

// TestABootstrapSettingCannotBeSetWhereItWouldDoNothing. paths.configFile
// names the file the settings come from, so a file cannot set it and a
// running daemon cannot change it. Each of those used to be the shape of a
// silently inert setting; here each is a refusal that names the way that
// works.
func TestABootstrapSettingCannotBeSetWhereItWouldDoNothing(t *testing.T) {
	e := newEnv(t, oneServer)

	// The environment works: MCPX_CONFIG is how the harness points at the
	// config at all, and the setting reports where its value came from.
	got := e.run("settings", "get", "paths.configFile")
	if !strings.Contains(got, "env:MCPX_CONFIG") {
		t.Errorf("paths.configFile should come from MCPX_CONFIG:\n%s", got)
	}

	e.run("call", "demo.echo", `{"message":"warm"}`)
	client := e.socketClient(t)
	req, _ := http.NewRequest(http.MethodPut, "http://mcpx/v1/settings/paths.configFile",
		strings.NewReader(`{"value":"/elsewhere.json","persist":"project"}`))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "MCPX_CONFIG") {
		t.Errorf("the daemon should refuse, naming the variable: %d %s", resp.StatusCode, body)
	}

	before, _ := os.ReadFile(filepath.Join(e.dir, ".mcpx.json"))
	out, err := e.try("settings", "set", "paths.configFile", "/elsewhere.json", "--persist", "project")
	if err == nil || !strings.Contains(out, "MCPX_CONFIG") {
		t.Errorf("the CLI should refuse, naming the variable: %v\n%s", err, out)
	}
	after, _ := os.ReadFile(filepath.Join(e.dir, ".mcpx.json"))
	if string(before) != string(after) {
		t.Errorf("a refused set still wrote the file:\n%s", after)
	}

	// Put a readable config back before newEnv's cleanup runs `stop --all`,
	// which has to resolve settings to find the daemon it is stopping.
	t.Cleanup(func() { e.writeConfig(t, oneServer) })
	e.writeConfig(t, `{"paths": {"configFile": "/elsewhere.json"}, "mcpServers": {
  "demo": { "command": "FAKE", "mcpx": { "sharing": "shared", "scope": "global" } } }}`)
	out, err = e.try("settings", "get", "paths.configFile")
	if err == nil || !strings.Contains(out, "MCPX_CONFIG") {
		t.Errorf("a config file naming it should be refused, not obeyed or ignored: %v\n%s", err, out)
	}
}
