package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are the settings that were declared and never read. Each test here
// sets one the way a user would -- a variable or a config key, not a Go field
// -- and asserts that something observable changed. A unit test on the wiring
// would have passed for most of these before they were wired, because the
// wiring was the part that did not exist.

// plainServer has no mcpx block, so the pool defaults are what decide it.
// oneServer states sharing and scope per server, which would mask the very
// layer under test.
const plainServer = `{
  "mcpServers": {
    "demo": { "command": "FAKE" }
  }
}`

type configRow struct {
	Name    string `json:"name"`
	Sharing string `json:"sharing"`
	Scope   string `json:"scope"`
	Max     int    `json:"max"`
}

func (e *env) configRows(t *testing.T, extraEnv ...string) []configRow {
	t.Helper()
	saved := e.envVars
	e.envVars = append(append([]string{}, e.envVars...), extraEnv...)
	defer func() { e.envVars = saved }()
	out := e.run("--json", "config")
	var doc struct {
		Servers []configRow `json:"servers"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("config --json should be a document: %v\n%s", err, out)
	}
	return doc.Servers
}

func TestPoolDefaultsAreReachableFromTheEnvironment(t *testing.T) {
	e := newEnv(t, plainServer)

	base := e.configRows(t)
	if len(base) != 1 || base[0].Sharing != "shared" {
		t.Fatalf("expected one shared server by default, got %+v", base)
	}

	// Scope stays global so that max is not clamped to one by the
	// single-key rule in config.Resolve.
	got := e.configRows(t,
		"MCPX_POOL_SHARING=exclusive",
		"MCPX_POOL_SCOPE=session",
		"MCPX_POOL_MAX=7")
	if len(got) != 1 {
		t.Fatalf("expected one server, got %+v", got)
	}
	if got[0].Sharing != "exclusive" || got[0].Scope != "session" || got[0].Max != 7 {
		t.Fatalf("the pool variables did not reach the resolved server: %+v", got[0])
	}
}

func TestPerServerPoolSettingsStillBeatTheEnvironment(t *testing.T) {
	// The fold writes the layer a config file's `pool` block writes, which
	// is beneath a server's own mcpx block. If it wrote any higher, a
	// variable meant as a default would override a deliberate per-server
	// choice, which is the opposite of what a default is.
	e := newEnv(t, oneServer)
	got := e.configRows(t, "MCPX_POOL_SHARING=exclusive")
	if len(got) != 1 || got[0].Sharing != "shared" {
		t.Fatalf("mcpx.sharing on the server should win: %+v", got)
	}
}

func TestJSONIsReachableAfterTheSubcommand(t *testing.T) {
	// --json was consumed by main before the subcommand, so `mcpx ls --json`
	// set output.json in the registry and printed a table anyway.
	e := newEnv(t, oneServer)
	out := e.run("ls", "--json")
	var any []map[string]any
	if err := json.Unmarshal([]byte(out), &any); err != nil {
		t.Fatalf("`ls --json` should print one document: %v\n%s", err, out)
	}
}

func TestOutputJSONIsReachableFromTheEnvironment(t *testing.T) {
	e := newEnv(t, oneServer)
	saved := e.envVars
	e.envVars = append(append([]string{}, saved...), "MCPX_OUTPUT_JSON=true")
	defer func() { e.envVars = saved }()
	out := e.run("ls")
	var any []map[string]any
	if err := json.Unmarshal([]byte(out), &any); err != nil {
		t.Fatalf("MCPX_OUTPUT_JSON should produce a document: %v\n%s", err, out)
	}
}

func TestScriptPrefixIsReachableFromTheEnvironment(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "s.ts")
	if err := os.WriteFile(script,
		[]byte("export default function () { return \"done\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := e.envVars
	e.envVars = append(append([]string{}, saved...),
		`MCPX_SCRIPT_PREFIX=console.info("the prefix ran")`)
	defer func() { e.envVars = saved }()

	out := e.run("run", script)
	if !strings.Contains(out, "the prefix ran") {
		t.Fatalf("MCPX_SCRIPT_PREFIX should reach the launcher:\n%s", out)
	}
}

func TestScriptEnvIsReachableFromTheEnvironment(t *testing.T) {
	e := newEnv(t, oneServer)
	saved := e.envVars
	e.envVars = append(append([]string{}, saved...), "MCPX_SCRIPT_ENV=FROM_SETTING=yes")
	defer func() { e.envVars = saved }()

	out := e.run("exec", `console.log(Deno.env.get("FROM_SETTING") ?? "missing")`)
	if !strings.Contains(out, "yes") {
		t.Fatalf("MCPX_SCRIPT_ENV should reach the script's environment:\n%s", out)
	}
}

func TestLoggingFileOffWritesNoDurableLog(t *testing.T) {
	e := newEnv(t, oneServer)
	saved := e.envVars
	e.envVars = append(append([]string{}, saved...), "MCPX_LOGGING_FILE=false")
	defer func() { e.envVars = saved }()

	e.run("exec", `console.log("hello")`)

	logs := filepath.Join(e.dir, "state", "logs")
	entries, err := os.ReadDir(logs)
	if err != nil {
		return // no directory at all is the strongest form of the answer
	}
	for _, en := range entries {
		if strings.HasSuffix(en.Name(), ".jsonl") {
			t.Fatalf("logging.file was off and %s was written anyway", en.Name())
		}
	}
}

func TestStrictUnknownKeysRefusesATypo(t *testing.T) {
	e := newEnv(t, oneServer)
	cfg := filepath.Join(e.dir, ".mcpx.json")
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	withTypo := strings.Replace(string(body), "{\n", "{\n  \"pooll\": {},\n", 1)
	if err := os.WriteFile(cfg, []byte(withTypo), 0o644); err != nil {
		t.Fatal(err)
	}

	// Off by default: a key mcpx does not claim is somebody else's section.
	if out, err := e.try("ls"); err != nil {
		t.Fatalf("an unknown key should be tolerated by default: %v\n%s", err, out)
	}

	saved := e.envVars
	e.envVars = append(append([]string{}, saved...), "MCPX_PLUMBING_STRICT_UNKNOWN_KEYS=true")
	defer func() { e.envVars = saved }()
	out, err := e.try("ls")
	if err == nil {
		t.Fatalf("strictUnknownKeys should refuse an unclaimed key:\n%s", out)
	}
	if !strings.Contains(out, "pooll") {
		t.Fatalf("the refusal should name the key:\n%s", out)
	}
}

func TestStateDirectoryIsReachableAsASetting(t *testing.T) {
	// paths.state was readable from MCPX_STATE_DIR and from nowhere else,
	// so the config key and the flag that the registry promises did nothing.
	e := newEnv(t, oneServer)
	elsewhere := filepath.Join(e.dir, "elsewhere")

	// MCPX_STATE_DIR is the same setting under its alias, and two names for
	// one setting in an environment that has no order is refused on purpose.
	saved := e.envVars
	var kept []string
	for _, kv := range saved {
		if !strings.HasPrefix(kv, "MCPX_STATE_DIR=") {
			kept = append(kept, kv)
		}
	}
	e.envVars = append(kept, "MCPX_PATHS_STATE="+elsewhere)
	defer func() { e.envVars = saved }()

	e.run("ls")
	if _, err := os.Stat(elsewhere); err != nil {
		t.Fatalf("paths.state should have decided where the daemon lives: %v", err)
	}
	// Stop the daemon this test started; the cleanup in newEnv runs without
	// the override and would look in the wrong directory for it.
	_, _ = e.try("stop")
}
