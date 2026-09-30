package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitWritesAConfigMcpxHonours runs `mcpx init` and reads the result
// back through the binary: the starter file's browser server must come out
// session-scoped and exclusive, which is what its comment promises.
func TestInitWritesAConfigMcpxHonours(t *testing.T) {
	e := newEnv(t, oneServer)
	fresh := filepath.Join(e.dir, "fresh")
	if err := os.Mkdir(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(e.mcpx, "init")
	cmd.Dir, cmd.Env = fresh, e.envVars
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mcpx init: %v\n%s", err, out)
	}
	written := filepath.Join(fresh, ".mcpx.json")
	out := e.run("--config", written, "--json", "config")
	var doc struct {
		Servers []struct {
			Name, Sharing, Scope string
			Max                  int
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	found := false
	for _, s := range doc.Servers {
		if s.Name == "chrome-devtools" {
			found = true
			if s.Scope != "session" || s.Sharing != "exclusive" || s.Max != 4 {
				t.Errorf("the starter browser server resolved to sharing=%s scope=%s max=%d; "+
					"its comment promises one browser per session, one caller at a time, up to 4",
					s.Sharing, s.Scope, s.Max)
			}
		}
	}
	if !found {
		t.Fatalf("the starter config has no chrome-devtools server:\n%s", out)
	}
}

// TestAStaleMcpxKeyIsRefusedByName: a key the "mcpx" block does not have
// stops the command with the key's name, instead of being dropped.
func TestAStaleMcpxKeyIsRefusedByName(t *testing.T) {
	e := newEnv(t, `{"mcpServers":{"demo":{"command":"FAKE","mcpx":{"mode":"session"}}}}`)
	out, err := e.try("config")
	if err == nil {
		t.Fatalf("a config with mcpx.mode was accepted:\n%s", out)
	}
	if !strings.Contains(out, `"mode"`) || !strings.Contains(out, "scope") {
		t.Fatalf("the refusal should name the key and what the block takes:\n%s", out)
	}
}

// TestAnotherHostsServerKeysAreReportedAndStrictRefusesThem: "type" is what
// Claude Code writes on every server. mcpx does not read it; by default that
// is a note in doctor, and under plumbing.strictUnknownKeys it is a refusal.
func TestAnotherHostsServerKeysAreReportedAndStrictRefusesThem(t *testing.T) {
	e := newEnv(t, `{"mcpServers":{"demo":{"type":"stdio","command":"FAKE"}}}`)
	if out, err := e.try("config"); err != nil {
		t.Fatalf("another host's key should be tolerated by default: %v\n%s", err, out)
	}
	doctor, _ := e.try("doctor")
	if !strings.Contains(doctor, "demo.type") {
		t.Errorf("doctor should report the key mcpx ignores:\n%s", doctor)
	}
	saved := e.envVars
	e.envVars = append(append([]string{}, saved...), "MCPX_PLUMBING_STRICT_UNKNOWN_KEYS=true")
	defer func() { e.envVars = saved }()
	out, err := e.try("config")
	if err == nil {
		t.Fatalf("strictUnknownKeys should refuse a server key mcpx does not read:\n%s", out)
	}
	if !strings.Contains(out, "demo.type") {
		t.Fatalf("the refusal should name the key:\n%s", out)
	}
}
