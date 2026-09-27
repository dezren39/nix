package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/testsupport"
)

// env is a fully isolated mcpx installation: its own binary, config, state
// directory and daemon.
type env struct {
	t       *testing.T
	dir     string
	mcpx    string
	fake    string
	envVars []string
}

func newEnv(t *testing.T, cfgBody string) *env {
	t.Helper()
	dir := t.TempDir()
	fake := testsupport.FakeMCPBinary(t)

	mcpx := filepath.Join(dir, "mcpx")
	build := exec.Command("go", "build", "-o", mcpx, "./cmd/mcpx")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build mcpx: %v\n%s", err, out)
	}

	cfg := strings.ReplaceAll(cfgBody, "FAKE", fake)
	if err := os.WriteFile(filepath.Join(dir, ".mcpx.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &env{
		t:    t,
		dir:  dir,
		mcpx: mcpx,
		fake: fake,
		envVars: append(os.Environ(),
			"MCPX_STATE_DIR="+filepath.Join(dir, "state"),
			"MCPX_CACHE_DIR="+filepath.Join(dir, "cache"),
			"MCPX_CONFIG="+filepath.Join(dir, ".mcpx.json"),
		),
	}
	t.Cleanup(func() {
		out, _ := e.try("stop")
		_ = out
	})
	return e
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func (e *env) try(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.mcpx, args...)
	cmd.Dir = e.dir
	cmd.Env = e.envVars
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *env) run(args ...string) string {
	e.t.Helper()
	out, err := e.try(args...)
	if err != nil {
		e.t.Fatalf("mcpx %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

const oneServer = `{
  "mcpServers": {
    "demo": { "command": "FAKE", "mcpx": { "mode": "shared", "description": "a fake server" } }
  }
}`

const statefulServer = `{
  "mcpServers": {
    "demo": { "command": "FAKE", "mcpx": { "mode": "session", "max": 4 } }
  }
}`

func TestLsStartsDaemonAndListsNamespaces(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("ls")
	if !strings.Contains(out, "demo") {
		t.Fatalf("namespace missing:\n%s", out)
	}
	if !strings.Contains(out, "a fake server") {
		t.Fatalf("description missing:\n%s", out)
	}
}

func TestLsIsFastOnceCached(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	// Give the background warm a moment, then time a steady-state call.
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	e.run("ls")
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("cached `ls` took %s; discovery must not touch MCP servers", d)
	}
}

func TestTypesRendersSignatures(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("types", "demo")
	for _, want := range []string{
		"declare namespace demo {",
		"function echo(args: {",
		"message: string;",
		"function fancy_name(",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("types output missing %q:\n%s", want, out)
		}
	}
}

func TestTypesRejectsUnknownNamespace(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("types", "nope")
	if err == nil {
		t.Fatalf("expected failure, got:\n%s", out)
	}
	if !strings.Contains(out, "unknown namespace") {
		t.Fatalf("error should name the problem:\n%s", out)
	}
}

func TestSearchFindsTools(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("search", "echo")
	if !strings.Contains(out, "demo.echo") {
		t.Fatalf("search missed the tool:\n%s", out)
	}
}

func TestCallWithoutJavaScript(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("call", "demo.echo", `{"message":"hello there"}`)
	if !strings.Contains(out, "hello there") {
		t.Fatalf("call output wrong:\n%s", out)
	}
}

func TestCallRejectsNonJSONArgs(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("call", "demo.echo", "not json")
	if err == nil {
		t.Fatalf("expected failure:\n%s", out)
	}
	if !strings.Contains(out, "JSON") {
		t.Fatalf("error should mention JSON:\n%s", out)
	}
}

func TestExecRunsTypeScript(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", `const r = await demo.echo({ message: "from script" }); console.log(String(r));`)
	if !strings.Contains(out, "from script") {
		t.Fatalf("exec output wrong:\n%s", out)
	}
}

func TestExecUnwrapsStructuredContent(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", `const r = await demo.structured(); console.log(JSON.stringify(r));`)
	if !strings.Contains(out, `"n":42`) {
		t.Fatalf("structuredContent should arrive parsed:\n%s", out)
	}
}

func TestExecSurfacesToolErrorsAsExceptions(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("exec", `
	  try {
	    await demo.boom();
	    console.log("NO ERROR");
	  } catch (e) {
	    console.log("CAUGHT:", (e as Error).message);
	  }
	`)
	if err != nil {
		t.Fatalf("script itself should succeed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "CAUGHT: boom: deliberate failure") {
		t.Fatalf("isError should become a thrown ToolError:\n%s", out)
	}
}

func TestExecPropagatesNonZeroExit(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("exec", `await demo.boom();`)
	if err == nil {
		t.Fatalf("an unhandled tool error must fail the command:\n%s", out)
	}
}

func TestExecCanUseTheGenericCallHelper(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", `const r = await call("demo", "echo", { message: "generic" }); console.log(String(r));`)
	if !strings.Contains(out, "generic") {
		t.Fatalf("generic call helper failed:\n%s", out)
	}
}

func TestRunExecutesAFileAndPassesArgs(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "s.ts")
	body := `import tools from "./mcpx-client.ts";
const args = (globalThis as any).Deno?.args ?? (globalThis as any).process.argv.slice(2);
const r = await tools.demo.echo({ message: args[0] });
console.log("GOT:" + String(r));
`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.run("run", script, "argument-one")
	if !strings.Contains(out, "GOT:argument-one") {
		t.Fatalf("file run failed:\n%s", out)
	}
}

func TestRunWritesTypedClientBesideTheScript(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "s.ts")
	os.WriteFile(script, []byte(`console.log("ok");`), 0o644)
	e.run("run", script)
	client := filepath.Join(e.dir, "mcpx-client.ts")
	b, err := os.ReadFile(client)
	if err != nil {
		t.Fatalf("client not written beside the script: %v", err)
	}
	if !strings.Contains(string(b), "export const demo") {
		t.Fatalf("client is missing the namespace:\n%s", b)
	}
}

// TestConcurrentRunsGetIsolatedInstances is the regression test for the
// problem that motivated mcpx: several agents driving one stateful MCP server
// at the same time. Each run must see only its own writes.
func TestConcurrentRunsGetIsolatedInstances(t *testing.T) {
	e := newEnv(t, statefulServer)
	e.run("ls")

	script := filepath.Join(e.dir, "iso.ts")
	body := `import tools from "./mcpx-client.ts";
const args = (globalThis as any).Deno?.args ?? (globalThis as any).process.argv.slice(2);
await tools.demo.open({ value: args[0] });
const state = await tools.demo.state();
console.log("RESULT " + JSON.stringify(state));
`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 4
	outs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i], errs[i] = e.try("run", script, fmt.Sprintf("value-%d", i))
		}(i)
	}
	wg.Wait()

	pids := map[float64]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("run %d failed: %v\n%s", i, errs[i], outs[i])
		}
		line := ""
		for _, l := range strings.Split(outs[i], "\n") {
			if strings.HasPrefix(l, "RESULT ") {
				line = strings.TrimPrefix(l, "RESULT ")
			}
		}
		if line == "" {
			t.Fatalf("run %d produced no RESULT line:\n%s", i, outs[i])
		}
		var got struct {
			PID  float64  `json:"pid"`
			Seen []string `json:"seen"`
		}
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("run %d: %v (%s)", i, err, line)
		}
		want := fmt.Sprintf("value-%d", i)
		if len(got.Seen) != 1 || got.Seen[0] != want {
			t.Fatalf("run %d saw %v, want exactly [%s]: state leaked between concurrent runs",
				i, got.Seen, want)
		}
		pids[got.PID] = true
	}
	if len(pids) != n {
		t.Fatalf("expected %d distinct server processes, got %d", n, len(pids))
	}
}

func TestSessionIsReleasedWhenAScriptEnds(t *testing.T) {
	e := newEnv(t, statefulServer)
	e.run("exec", `await demo.echo({ message: "x" });`)
	// The instance is stopped on release, so nothing should be left pinned.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out := e.run("--json", "status")
		var st struct {
			Servers []struct {
				Live int `json:"live"`
			} `json:"servers"`
		}
		if json.Unmarshal([]byte(out), &st) == nil && len(st.Servers) > 0 && st.Servers[0].Live == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("session instance was not released after the script finished")
}

func TestStatusReportsPoolState(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	out := e.run("status")
	if !strings.Contains(out, "daemon:   running") {
		t.Fatalf("status missing daemon line:\n%s", out)
	}
	if !strings.Contains(out, "demo") {
		t.Fatalf("status missing server:\n%s", out)
	}
}

func TestJSONOutputIsValid(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("--json", "ls")
	var v []map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("--json ls is not valid JSON: %v\n%s", err, out)
	}
	if len(v) != 1 {
		t.Fatalf("want 1 namespace, got %d", len(v))
	}
}

func TestSchemaCacheSurvivesDaemonRestart(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	time.Sleep(500 * time.Millisecond)
	e.run("stop")

	// With the server binary removed, a cold daemon can only answer from the
	// on-disk cache.
	moved := e.fake + ".moved"
	if err := os.Rename(e.fake, moved); err != nil {
		t.Skipf("cannot move fake binary: %v", err)
	}
	defer os.Rename(moved, e.fake)

	out := e.run("types", "demo")
	if !strings.Contains(out, "function echo(") {
		t.Fatalf("cache did not survive a restart:\n%s", out)
	}
}

func TestRestartClearsServerState(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("call", "demo.open", `{"value":"before"}`)
	out := e.run("call", "demo.state", "{}")
	if !strings.Contains(out, "before") {
		t.Fatalf("state was not recorded:\n%s", out)
	}
	e.run("restart", "demo")
	out = e.run("call", "demo.state", "{}")
	if strings.Contains(out, "before") {
		t.Fatalf("state survived a restart:\n%s", out)
	}
}

func TestUnknownServerIsReportedClearly(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("call", "nope.echo", "{}")
	if err == nil {
		t.Fatalf("expected failure:\n%s", out)
	}
	if !strings.Contains(out, "unknown server or namespace") {
		t.Fatalf("unhelpful error:\n%s", out)
	}
}

func TestFailingServerDoesNotBlockHealthyOnes(t *testing.T) {
	cfg := `{
	  "mcpServers": {
	    "good": { "command": "FAKE" },
	    "bad":  { "command": "/nonexistent/definitely-not-a-binary" }
	  }
	}`
	e := newEnv(t, cfg)
	out := e.run("ls")
	if !strings.Contains(out, "good") {
		t.Fatalf("healthy server missing:\n%s", out)
	}
	// The healthy server still works.
	out = e.run("call", "good.echo", `{"message":"still fine"}`)
	if !strings.Contains(out, "still fine") {
		t.Fatalf("a broken server broke a healthy one:\n%s", out)
	}
}

func TestDuplicateNamespaceIsRejected(t *testing.T) {
	cfg := `{
	  "mcpServers": {
	    "a-b": { "command": "FAKE" },
	    "a.b": { "command": "FAKE" }
	  }
	}`
	e := newEnv(t, cfg)
	out, err := e.try("ls")
	if err == nil {
		t.Fatalf("two servers mapping to one namespace must be rejected:\n%s", out)
	}
}

func TestClientCommandWritesAModule(t *testing.T) {
	e := newEnv(t, oneServer)
	dest := filepath.Join(e.dir, "generated", "client.ts")
	e.run("client", "-o", dest)
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "export const demo") {
		t.Fatalf("generated client is wrong:\n%s", b)
	}
}

func TestConfigCommandShowsResolvedSettings(t *testing.T) {
	e := newEnv(t, statefulServer)
	out := e.run("config")
	var cfg struct {
		Servers []struct {
			Mode string `json:"mode"`
			Max  int    `json:"max"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("config output invalid: %v\n%s", err, out)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Mode != "session" || cfg.Servers[0].Max != 4 {
		t.Fatalf("resolved config wrong: %+v", cfg.Servers)
	}
}

func TestNoOrphanProcessesAfterStop(t *testing.T) {
	e := newEnv(t, statefulServer)
	e.run("call", "demo.echo", `{"message":"x"}`)

	// Ask the daemon exactly which processes it owns, so the assertion is not
	// confused by unrelated MCP servers belonging to other tests or to the
	// user's own mcpx installations.
	var st struct {
		Servers []struct {
			Instances []struct {
				PID int `json:"pid"`
			} `json:"instances"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(e.run("--json", "status")), &st); err != nil {
		t.Fatalf("status: %v", err)
	}
	var owned []int
	for _, s := range st.Servers {
		for _, in := range s.Instances {
			if in.PID > 0 {
				owned = append(owned, in.PID)
			}
		}
	}
	if len(owned) == 0 {
		t.Fatal("daemon reported no child processes; the test proves nothing")
	}

	e.run("stop")

	deadline := time.Now().Add(5 * time.Second)
	for {
		var alive []int
		for _, pid := range owned {
			if processExists(pid) {
				alive = append(alive, pid)
			}
		}
		if len(alive) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("MCP server processes survived `mcpx stop`: %v", alive)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// processExists reports whether a pid is still running, without signalling it.
func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 performs error checking only.
	return p.Signal(syscall.Signal(0)) == nil
}
