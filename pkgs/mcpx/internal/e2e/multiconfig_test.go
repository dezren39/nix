package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeparateConfigsGetSeparateDaemons is the multi-repo case: two projects
// with different .mcpx.json files must not end up sharing one daemon, or the
// first project to run a command would decide which servers exist for both.
func TestSeparateConfigsGetSeparateDaemons(t *testing.T) {
	shared := t.TempDir()
	fake := newEnv(t, oneServer).fake

	mk := func(name, serverName string) *env {
		dir := filepath.Join(shared, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := strings.ReplaceAll(`{"mcpServers":{"`+serverName+`":{"command":"FAKE"}}}`, "FAKE", fake)
		if err := os.WriteFile(filepath.Join(dir, ".mcpx.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		e := newEnv(t, oneServer) // reuse the built binary and state layout
		e.dir = dir
		e.envVars = append(e.envVars, "MCPX_CONFIG="+filepath.Join(dir, ".mcpx.json"))
		return e
	}

	a := mk("proj-a", "alpha")
	b := mk("proj-b", "beta")

	outA := a.run("--json", "ls")
	outB := b.run("--json", "ls")

	nsOf := func(out string) []string {
		var v []struct {
			Namespace string `json:"namespace"`
		}
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatalf("bad json: %v\n%s", err, out)
		}
		var names []string
		for _, n := range v {
			names = append(names, n.Namespace)
		}
		return names
	}

	na, nb := nsOf(outA), nsOf(outB)
	if len(na) != 1 || na[0] != "alpha" {
		t.Fatalf("project a saw %v, want [alpha]", na)
	}
	if len(nb) != 1 || nb[0] != "beta" {
		t.Fatalf("project b saw %v, want [beta]: the two projects shared a daemon", nb)
	}
}

// TestEditingConfigTakesEffect guards against the stale-daemon trap: after
// changing .mcpx.json the next command must reflect the change.
func TestEditingConfigTakesEffect(t *testing.T) {
	e := newEnv(t, oneServer)
	if out := e.run("--json", "ls"); !strings.Contains(out, `"demo"`) {
		t.Fatalf("initial namespace missing:\n%s", out)
	}

	updated := strings.ReplaceAll(
		`{"mcpServers":{"renamed":{"command":"FAKE"}}}`, "FAKE", e.fake)
	if err := os.WriteFile(filepath.Join(e.dir, ".mcpx.json"), []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	out := e.run("--json", "ls")
	if !strings.Contains(out, `"renamed"`) {
		t.Fatalf("config edit was not picked up:\n%s", out)
	}
	if strings.Contains(out, `"demo"`) {
		t.Fatalf("stale daemon still serving the old config:\n%s", out)
	}
}

func TestDaemonsCommandListsRunningDaemons(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	out := e.run("--json", "daemons")
	var rows []struct {
		Running bool   `json:"running"`
		Config  string `json:"config"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	found := false
	for _, r := range rows {
		if r.Running && strings.HasSuffix(r.Config, ".mcpx.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("running daemon not listed:\n%s", out)
	}
}

func TestStopAllShutsEverythingDown(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	out := e.run("stop", "--all")
	if !strings.Contains(out, "stopped daemon") {
		t.Fatalf("stop --all reported nothing:\n%s", out)
	}
	if got := e.run("status"); !strings.Contains(got, "not running") {
		t.Fatalf("daemon survived stop --all:\n%s", got)
	}
}
