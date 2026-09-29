package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These drive the real binary against a real daemon, because everything here
// is about two processes disagreeing and a single-process test cannot show
// that. The bug this whole area exists to prevent -- a flag accepted by one
// process and ignored by another -- is invisible unless both are real.

func TestSettingsListShowsWhereEachValueCameFrom(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("settings", "list", "--changed")
	// The environment this test runs in sets the state directory, so that is
	// the value whose provenance should be reported.
	if !strings.Contains(out, "paths.state") {
		t.Fatalf("a setting the environment overrode should be listed:\n%s", out)
	}
	if !strings.Contains(out, "MCPX_STATE_DIR") {
		t.Fatalf("the listing should name the variable that decided it:\n%s", out)
	}
}

func TestSettingsGetExplainsOneSetting(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("settings", "get", "pool.idleTimeout")
	for _, want := range []string{"pool.idleTimeout", "default", "--pool-idle-timeout",
		"MCPX_POOL_IDLE_TIMEOUT", "daemon"} {
		if !strings.Contains(out, want) {
			t.Errorf("`settings get` should mention %q:\n%s", want, out)
		}
	}
}

func TestARuntimeChangeReachesTheRunningDaemon(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls") // make sure a daemon exists before changing it

	out := e.run("settings", "set", "logging.level", "debug")
	if !strings.Contains(out, "applied") {
		t.Fatalf("a hot setting should be applied at once:\n%s", out)
	}
	// Asked of the daemon, not resolved locally: the whole point is that the
	// two can differ and that the difference is visible.
	got := e.run("settings", "get", "logging.level", "--daemon")
	if !strings.Contains(got, "debug") || !strings.Contains(got, "runtime") {
		t.Fatalf("the daemon should report the runtime override:\n%s", got)
	}
	e.run("settings", "unset", "logging.level")
	got = e.run("settings", "get", "logging.level", "--daemon")
	if !strings.Contains(got, "from      default") {
		t.Fatalf("dropping the override should fall back to the default:\n%s", got)
	}
}

func TestAColdSettingSaysARestartIsNeededRatherThanPretending(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	out := e.run("settings", "set", "daemon.port", "9931")
	if !strings.Contains(out, "read once when the process starts") {
		t.Fatalf("a setting consumed at startup should say so rather than "+
			"accepting a value that does nothing:\n%s", out)
	}
}

func TestPersistingASettingWritesTheConfigFileAndApplies(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	e.run("settings", "set", "pool.max", "7", "--persist", "project")

	b, err := os.ReadFile(filepath.Join(e.dir, ".mcpx.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("the rewritten config should still be valid JSON: %v\n%s", err, b)
	}
	pool, _ := doc["pool"].(map[string]any)
	if pool == nil || pool["max"] != float64(7) {
		t.Fatalf("pool.max should be written as a number, not a string:\n%s", b)
	}
	if _, kept := doc["mcpServers"]; !kept {
		t.Fatalf("the rest of the file must survive the edit:\n%s", b)
	}
	if got := e.run("settings", "get", "pool.max"); !strings.Contains(got, "7") {
		t.Fatalf("the persisted value should be what resolves now:\n%s", got)
	}
}

func TestAnInvalidValueIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	e := newEnv(t, oneServer)
	before, _ := os.ReadFile(filepath.Join(e.dir, ".mcpx.json"))
	out, err := e.try("settings", "set", "pool.idleTimeout", "forever", "--persist", "project")
	if err == nil {
		t.Fatalf("a value that is not a duration should be refused:\n%s", out)
	}
	after, _ := os.ReadFile(filepath.Join(e.dir, ".mcpx.json"))
	if string(before) != string(after) {
		t.Fatal("a refused value must not have been written")
	}
}

func TestAddingAServerIsLiveWithoutARestart(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")

	out := e.run("servers", "add", "second", "--description", "added live", "--", e.fake)
	if !strings.Contains(out, "live") {
		t.Fatalf("adding should report that it took effect:\n%s", out)
	}
	if got := e.run("servers", "list"); !strings.Contains(got, "second") {
		t.Fatalf("the new server should be listed:\n%s", got)
	}
	// Reached through the pool, not merely present in a file: a server that
	// is configured but not callable is the failure this is meant to rule
	// out.
	if got := e.run("ls"); !strings.Contains(got, "second") {
		t.Fatalf("the daemon should serve the new namespace:\n%s", got)
	}
	// The one that was already running must not have been disturbed.
	if got := e.run("ls"); !strings.Contains(got, "demo") {
		t.Fatalf("the existing server should survive the reload:\n%s", got)
	}

	e.run("servers", "remove", "second")
	if got := e.run("servers", "list"); strings.Contains(got, "second") {
		t.Fatalf("the server should be gone:\n%s", got)
	}
}

func TestAServerAddedOverTheSocketIsTheSameAsOneAddedFromTheCLI(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)

	body := strings.NewReader(`{"name":"viasocket","command":"` + e.fake +
		`","mcpx":{"description":"added over /v1"}}`)
	resp, err := c.Post("http://mcpx/v1/servers", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/servers: %d %s", resp.StatusCode, raw)
	}
	if got := e.run("ls"); !strings.Contains(got, "viasocket") {
		t.Fatalf("a server added over /v1 should be callable:\n%s", got)
	}

	req, _ := http.NewRequest(http.MethodDelete, "http://mcpx/v1/servers/viasocket", nil)
	resp2, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /v1/servers: %d", resp2.StatusCode)
	}
	if got := e.run("servers", "list"); strings.Contains(got, "viasocket") {
		t.Fatalf("it should be gone:\n%s", got)
	}
}

func TestEverySettingIsReachableOverV1(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)

	resp, err := c.Get("http://mcpx/v1/settings?plumbing=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Settings []struct {
			Path   string `json:"path"`
			Env    string `json:"env"`
			Flag   string `json:"flag"`
			Scope  string `json:"scope"`
			Source string `json:"source"`
		} `json:"settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Settings) < 50 {
		t.Fatalf("the whole registry should be there, got %d", len(out.Settings))
	}
	for _, s := range out.Settings {
		if s.Env == "" || s.Flag == "" || s.Scope == "" || s.Source == "" {
			t.Errorf("%s is missing part of its record: %+v", s.Path, s)
		}
	}
}

// TestACallScopedFlagReachesADaemonStartedWithoutIt is the failure the whole
// scope mechanism exists for. The daemon here was started in an environment
// that says nothing about search limits; the client's flag has to travel with
// the request or it is silently lost.
func TestACallScopedFlagReachesADaemonStartedWithoutIt(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")

	all := jsonOf(t, e.run("--json", "search", "e"))
	var everything []map[string]any
	if err := json.Unmarshal([]byte(all), &everything); err != nil {
		t.Skipf("the fake server offers nothing searchable: %v", err)
	}
	if len(everything) < 2 {
		t.Skipf("need at least two matching tools to show a limit working, got %d",
			len(everything))
	}

	// Through the environment rather than the flag, because that proves the
	// value travelled: the daemon's own environment has no such variable.
	e.envVars = append(e.envVars, "MCPX_SEARCH_LIMIT=1")
	limited := jsonOf(t, e.run("--json", "search", "e"))
	var few []map[string]any
	if err := json.Unmarshal([]byte(limited), &few); err != nil {
		t.Fatal(err)
	}
	if len(few) != 1 {
		t.Fatalf("the client's call-scoped setting should have reached the daemon; "+
			"got %d results, wanted 1", len(few))
	}
}
