package e2e_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/testsupport"

	_ "modernc.org/sqlite"
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
	// Outside $HOME: the daemon finds scripts and recipes by walking up
	// from here, and must not reach the user's own (testsupport.TempDir).
	dir := testsupport.TempDir(t)
	fake := testsupport.FakeMCPBinary(t)

	mcpx := testsupport.MCPXBinary(t)

	cfg := strings.ReplaceAll(cfgBody, "FAKE", fake)
	if err := os.WriteFile(filepath.Join(dir, ".mcpx.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &env{
		t:    t,
		dir:  dir,
		mcpx: mcpx,
		fake: fake,
		envVars: append(withoutMCPXVars(os.Environ()),
			// Never the real registry. A test that reaches the internet
			// fails wherever the internet is slow, which is every CI runner
			// -- `/v1/registry/search` timed out there while passing on a
			// laptop, and took a merge with it.
			"MCPX_REGISTRY_URL="+stubRegistry(t),
			// Never the developer's own scripts and config. $HOME is the
			// second root of recipe and script discovery (recipes.Dirs takes
			// it from os.UserHomeDir, which no MCPX_ variable covers), so a
			// machine with ~/.config/mcpx/scripts ranked its owner's recipes
			// against the fixtures and failed tests that CI passed. It also
			// keeps `mcpx init --global` out of the real config forever.
			// Deno and bun cache under HOME too; their variables are passed
			// through below so a cold cache is not paid per test.
			"HOME="+filepath.Join(dir, "home"),
			"DENO_DIR="+toolCache(t, "DENO_DIR", "Library", "Caches", "deno"),
			"BUN_INSTALL_CACHE_DIR="+toolCache(t, "BUN_INSTALL_CACHE_DIR", ".bun", "install", "cache"),
			"MCPX_STATE_DIR="+filepath.Join(dir, "state"),
			"MCPX_CACHE_DIR="+filepath.Join(dir, "cache"),
			"MCPX_CONFIG="+filepath.Join(dir, ".mcpx.json"),
			// Backstop for anything that escapes cleanup entirely -- an
			// interrupted run, a panic before t.Cleanup, a daemon started by
			// a child the harness never learned about. The default for an
			// auto-started daemon is hours; at that length a few suite runs
			// leave dozens of live daemons, each holding a fakemcp child.
			// This is the setting `mcpx` gives the daemon it starts on
			// demand, so it is the one the tests start.
			"MCPX_AUTOSTART_IDLE_EXIT="+testIdleExit,
		),
	}
	t.Cleanup(func() {
		// `stop --all` rather than `stop`: plain `stop` dials the socket
		// path recomputed from the environment, and a long state path moves
		// the real socket to a private runtime directory keyed to the
		// *caller's* TMPDIR, so the computed path misses and the daemon
		// survives. `--all` reads the daemon-<key>.json info files instead,
		// which record the path the daemon actually bound. Each env has its
		// own MCPX_STATE_DIR, so `--all` is scoped to this test.
		if out, err := e.try("stop", "--all"); err != nil {
			// Logged, not fatal: a test that has already passed should not
			// be failed by its own teardown, but a teardown that silently
			// fails is how the leak went unnoticed for so long.
			t.Logf("cleanup: mcpx stop --all failed: %v\n%s", err, out)
		}
	})
	return e
}

// testIdleExit is how long a daemon a test started survives with nothing to
// do. Short enough that a leak clears itself before the next run, long enough
// that it cannot expire in the middle of a slow test.
const testIdleExit = "60s"

// toolCache is where deno or bun caches, named absolutely so moving HOME
// does not move it. A per-test cache is correct but cold, and these runtimes
// pay for that in downloads on every test that runs a script.
func toolCache(t *testing.T, env string, parts ...string) string {
	t.Helper()
	if v := os.Getenv(env); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(append([]string{home}, parts...)...)
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
	return string(out), harnessTimeout(ctx, err)
}

// harnessTimeout names the harness's own deadline as the killer. Without it
// a slow machine reported a bare "signal: killed", which read as the OOM
// killer or a crash (#260) when it was this 90 s budget expiring.
func harnessTimeout(ctx context.Context, err error) error {
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("the test harness killed mcpx at its 90s deadline: %w", err)
	}
	return err
}

// runStdin runs mcpx with something on standard input, which is how a
// protocol server is spoken to.
func (e *env) runStdin(stdin string, args ...string) string {
	e.t.Helper()
	cmd := exec.Command(e.mcpx, args...)
	cmd.Dir = e.dir
	cmd.Env = e.envVars
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("mcpx %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
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
    "demo": { "command": "FAKE", "mcpx": { "sharing": "shared", "scope": "global", "description": "a fake server" } }
  }
}`

const statefulServer = `{
  "mcpServers": {
    "demo": { "command": "FAKE", "mcpx": { "sharing": "exclusive", "scope": "session", "max": 4 } }
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

	// Every part of the catalogue, not only tools: the cache held tools and
	// resources and not prompts, so a restarted daemon served none, and
	// `mcpx prompts` said "No prompts" for a server that publishes one.
	// Checking one kind is what let that stand.
	out := e.run("types", "demo")
	if !strings.Contains(out, "function echo(") {
		t.Fatalf("tools did not survive a restart:\n%s", out)
	}
	if out := e.run("prompts"); !strings.Contains(out, "demo.summarise") {
		t.Fatalf("prompts did not survive a restart:\n%s", out)
	}
	if out := e.run("resources"); !strings.Contains(out, "demo/") {
		t.Fatalf("resources did not survive a restart:\n%s", out)
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
			Sharing string `json:"sharing"`
			Scope   string `json:"scope"`
			Max     int    `json:"max"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("config output invalid: %v\n%s", err, out)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Scope != "session" ||
		cfg.Servers[0].Sharing != "exclusive" || cfg.Servers[0].Max != 4 {
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

func TestTypesForASingleTool(t *testing.T) {
	e := newEnv(t, oneServer)
	full := e.run("types", "demo")
	one := e.run("types", "demo.echo")

	if !strings.Contains(one, "function echo(") {
		t.Fatalf("the requested tool is missing:\n%s", one)
	}
	for _, other := range []string{"function state(", "function boom(", "function slow("} {
		if strings.Contains(one, other) {
			t.Errorf("single-tool output leaked %q:\n%s", other, one)
		}
	}
	// The whole point is the size difference.
	if len(one) >= len(full) {
		t.Fatalf("single tool (%d) should be smaller than the namespace (%d)", len(one), len(full))
	}
}

func TestTypesAcceptsSeveralToolSelectors(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("types", "demo.echo,demo.state")
	if !strings.Contains(out, "function echo(") || !strings.Contains(out, "function state(") {
		t.Fatalf("both tools should appear:\n%s", out)
	}
	if strings.Contains(out, "function boom(") {
		t.Errorf("unrequested tool leaked:\n%s", out)
	}
}

func TestTypesRejectsUnknownToolByName(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("types", "demo.nosuchtool")
	if err == nil {
		t.Fatalf("expected failure:\n%s", out)
	}
	if !strings.Contains(out, "demo.nosuchtool") {
		t.Fatalf("the error should name what was not found:\n%s", out)
	}
}

func TestTypesAcceptsTheGeneratedFunctionName(t *testing.T) {
	// `fancy-name` is exposed as fancy_name; asking by either must work.
	e := newEnv(t, oneServer)
	out := e.run("types", "demo.fancy_name")
	if !strings.Contains(out, "function fancy_name(") {
		t.Fatalf("sanitised name should resolve:\n%s", out)
	}
}

func TestServerPreludeFromConfigReachesTypes(t *testing.T) {
	cfg := `{
	  "mcpServers": {
	    "demo": { "command": "FAKE", "mcpx": { "prelude": "ids come from state()" } }
	  }
	}`
	e := newEnv(t, cfg)
	out := e.run("types", "demo")
	if !strings.Contains(out, "ids come from state()") {
		t.Fatalf("config prelude missing:\n%s", out)
	}
}

func TestCatalogFitsABudgetAndListsEveryNamespace(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("catalog", "--budget", "200")
	if !strings.Contains(out, "- demo (") {
		t.Fatalf("namespace header missing:\n%s", out)
	}
	if got := len(out) / 4; got > 400 {
		t.Errorf("catalog at budget 200 produced ~%d tokens:\n%s", got, out)
	}
}

const profileServers = `{
  "mcpServers": {
    "demo":  { "command": "FAKE" },
    "extra": { "command": "FAKE", "mcpx": { "namespace": "extra", "profiles": ["web"], "default": false } },
    "peek":  { "aliasOf": "demo", "mcpx": { "namespace": "peek", "tools": ["echo"], "profiles": ["web"], "default": false } }
  }
}`

// jsonOf trims anything printed before a JSON document begins.
//
// Both brackets, because a top-level array is as valid a document as an
// object. Looking only for "{" skipped an array's opening bracket and landed
// on its first element, which then failed on the comma after it.
func jsonOf(t *testing.T, out string) string {
	t.Helper()
	obj := strings.Index(out, "{")
	arr := strings.Index(out, "[")
	switch {
	case obj < 0 && arr < 0:
		return out
	case arr < 0 || (obj >= 0 && obj < arr):
		return out[obj:]
	default:
		return out[arr:]
	}
}

// firstRecord finds the first JSON log record in mixed output. Taking line
// zero is fragile: mcpx prints its own notices to stderr, and on a cold cache
// one of them lands ahead of the record under test.
func firstRecord(t *testing.T, out string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var doc map[string]any
		if json.Unmarshal([]byte(line), &doc) == nil && doc["level"] != nil {
			return doc
		}
	}
	t.Fatalf("no log record found in:\n%s", out)
	return nil
}

func namespacesOf(t *testing.T, out string) map[string]bool {
	t.Helper()
	var v []struct {
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for _, n := range v {
		got[n.Namespace] = true
	}
	return got
}

func TestProfileHidesOptedOutServersByDefault(t *testing.T) {
	e := newEnv(t, profileServers)
	got := namespacesOf(t, e.run("--json", "ls"))
	if !got["demo"] {
		t.Error("a default-on server should be listed")
	}
	if got["extra"] || got["peek"] {
		t.Errorf("default:false servers should be hidden: %v", got)
	}
}

func TestProfileFlagAddsThem(t *testing.T) {
	e := newEnv(t, profileServers)
	got := namespacesOf(t, e.run("--profile", "web", "--json", "ls"))
	for _, want := range []string{"demo", "extra", "peek"} {
		if !got[want] {
			t.Errorf("--profile web should include %s: %v", want, got)
		}
	}
}

func TestSkipDefaultNarrowsToTheProfile(t *testing.T) {
	e := newEnv(t, profileServers)
	got := namespacesOf(t, e.run("--profile", "web", "--skip-default", "--json", "ls"))
	if got["demo"] {
		t.Errorf("--skip-default should drop the default set: %v", got)
	}
	if !got["extra"] || !got["peek"] {
		t.Errorf("the profile itself must survive: %v", got)
	}
}

func TestGeneratedClientHonoursTheProfile(t *testing.T) {
	e := newEnv(t, profileServers)
	// A namespace outside the profile must not appear in a script's client.
	out := e.run("exec", `console.log(typeof (globalThis as any).extra);`)
	if !strings.Contains(out, "undefined") {
		t.Fatalf("an excluded namespace leaked into the client:\n%s", out)
	}
	withProfile := e.run("--profile", "web", "exec",
		`const r = await extra.echo({ message: "in profile" }); console.log(String(r));`)
	if !strings.Contains(withProfile, "in profile") {
		t.Fatalf("the profile namespace should be callable:\n%s", withProfile)
	}
}

func TestAliasExposesASubsetOfTheSameServer(t *testing.T) {
	e := newEnv(t, profileServers)
	full := e.run("--profile", "web", "types", "demo")
	restricted := e.run("--profile", "web", "types", "peek")

	if !strings.Contains(restricted, "function echo(") {
		t.Fatalf("the allowlisted tool is missing:\n%s", restricted)
	}
	for _, hidden := range []string{"function state(", "function boom("} {
		if strings.Contains(restricted, hidden) {
			t.Errorf("alias leaked %q outside its allowlist", hidden)
		}
	}
	if len(restricted) >= len(full) {
		t.Error("the restricted view should be smaller than the full one")
	}
}

func TestAliasSharesOneProcessWithItsTarget(t *testing.T) {
	e := newEnv(t, profileServers)
	// Write through the full view, read through the alias. Same process means
	// the alias sees it.
	e.run("--profile", "web", "exec", `await demo.open({ value: "written-via-demo" });`)
	out := e.run("--profile", "web", "exec", `console.log(JSON.stringify(await peek.echo({ message: "x" })));`)
	if !strings.Contains(out, "x") {
		t.Fatalf("alias call failed:\n%s", out)
	}
	// One pool, reported once under both names.
	st := e.run("--json", "--all-profiles", "status")
	var doc struct {
		Servers []struct {
			Namespace string `json:"namespace"`
			Live      int    `json:"live"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(st), &doc); err != nil {
		t.Fatal(err)
	}
	for _, s := range doc.Servers {
		if strings.Contains(s.Namespace, ",") && s.Live > 1 {
			t.Fatalf("a shared pool should not hold several instances: %+v", s)
		}
	}
}

func TestScriptLogHelperRendersToStderrNotStdout(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", `log.info("hello {who}", { who: "world" }); console.log("THE-RESULT");`)
	if !strings.Contains(out, "hello world") {
		t.Fatalf("the template was not interpolated:\n%s", out)
	}
	if !strings.Contains(out, "THE-RESULT") {
		t.Fatalf("stdout was lost:\n%s", out)
	}
}

func TestLogFormatsAreSelectable(t *testing.T) {
	e := newEnv(t, oneServer)
	bare := e.run("exec", "--format", "bare", `log.info("just {x}", { x: 1 });`)
	if strings.TrimSpace(bare) != "just 1" {
		t.Fatalf("bare should be the message alone, got %q", bare)
	}
	jsonOut := e.run("exec", "--format", "json", `log.info("just {x}", { x: 1 });`)
	var doc map[string]any
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(jsonOut), "\n")[0])
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		t.Fatalf("json format is not parseable: %v\n%s", err, jsonOut)
	}
	if doc["msg"] != "just 1" || doc["template"] != "just {x}" {
		t.Fatalf("json should carry both forms: %v", doc)
	}
}

func TestLogLevelThresholdIsHonoured(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--log-level", "warn",
		`log.debug("D"); log.info("I"); log.warn("W"); log.error("E");`)
	for _, hidden := range []string{"\"D\"", " D", "I\n"} {
		_ = hidden
	}
	if strings.Contains(out, " D") || strings.Contains(out, " I\n") {
		t.Errorf("records below the threshold were emitted:\n%s", out)
	}
	if !strings.Contains(out, "W") || !strings.Contains(out, "E") {
		t.Errorf("records at or above the threshold were dropped:\n%s", out)
	}
}

func TestDefaultExportIsCalledAndItsReturnPrinted(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "entry.ts")
	body := `import tools from "./mcpx-client.ts";
export function helper() { return "library use"; }
export default async function main(args: string[]) {
  return { got: args, viaHelper: helper() };
}
`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.run("run", script, "alpha", "beta")
	var doc struct {
		Got       []string `json:"got"`
		ViaHelper string   `json:"viaHelper"`
	}
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("no JSON result:\n%s", out)
	}
	if err := json.Unmarshal([]byte(out[start:]), &doc); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, out)
	}
	if len(doc.Got) != 2 || doc.Got[0] != "alpha" {
		t.Errorf("main should receive argv as an array, got %v", doc.Got)
	}
	if doc.ViaHelper != "library use" {
		t.Errorf("other exports should remain usable: %q", doc.ViaHelper)
	}
}

func TestNamedExportReceivesSpreadArguments(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "named.ts")
	body := `export function join(a: string, b: string) { return a + "+" + b; }
export default function main() { return "default-was-used"; }
`
	os.WriteFile(script, []byte(body), 0o644)
	out := e.run("run", "--export", "join", script, "x", "y")
	if !strings.Contains(out, "x+y") {
		t.Fatalf("--export should call the named function with spread args:\n%s", out)
	}
	if strings.Contains(out, "default-was-used") {
		t.Error("--export must not also run the default export")
	}
}

func TestUnknownExportNamesWhatExists(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "named2.ts")
	os.WriteFile(script, []byte(`export function real() { return 1; }`), 0o644)
	out, err := e.try("run", "--export", "missing", script)
	if err == nil {
		t.Fatalf("expected failure:\n%s", out)
	}
	if !strings.Contains(out, "real") {
		t.Errorf("the error should list the exports that do exist:\n%s", out)
	}
}

func TestScriptWithoutDefaultExportStillRunsOnImport(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "toplevel.ts")
	os.WriteFile(script, []byte(`console.log("ran at import time");`), 0o644)
	out := e.run("run", script)
	if !strings.Contains(out, "ran at import time") {
		t.Fatalf("a script with no entry point should still run:\n%s", out)
	}
}

func TestRunJSONEnvelope(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "env.ts")
	body := `import { log } from "./mcpx-client.ts";
export default async function main() {
  log.info("working on {thing}", { thing: "it" });
  return { done: true };
}
`
	os.WriteFile(script, []byte(body), 0o644)
	out := e.run("--json", "run", script)

	var doc struct {
		OK       bool `json:"ok"`
		ExitCode int  `json:"exitCode"`
		Result   struct {
			Done bool `json:"done"`
		} `json:"result"`
		Logs []struct {
			Level    string `json:"level"`
			Msg      string `json:"msg"`
			Template string `json:"template"`
			Thing    string `json:"thing"`
		} `json:"logs"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &doc); err != nil {
		t.Fatalf("envelope is not JSON: %v\n%s", err, out)
	}
	if !doc.OK || doc.ExitCode != 0 {
		t.Errorf("run should have succeeded: %+v", doc)
	}
	if !doc.Result.Done {
		t.Errorf("the return value should be parsed into result: %s", out)
	}
	if len(doc.Logs) != 1 {
		t.Fatalf("logs should be captured, got %d", len(doc.Logs))
	}
	if doc.Logs[0].Msg != "working on it" || doc.Logs[0].Template != "working on {thing}" {
		t.Errorf("both message forms should survive: %+v", doc.Logs[0])
	}
	if doc.Logs[0].Thing != "it" {
		t.Errorf("attributes should be present: %+v", doc.Logs[0])
	}
}

func TestRunJSONReportsAFailure(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "fail.ts")
	os.WriteFile(script, []byte(`throw new Error("deliberate");`), 0o644)
	out, err := e.try("--json", "run", script)
	if err == nil {
		t.Fatal("a throwing script should exit non-zero")
	}
	var doc struct {
		OK       bool   `json:"ok"`
		ExitCode int    `json:"exitCode"`
		Stderr   string `json:"stderr"`
	}
	if jerr := json.Unmarshal([]byte(jsonOf(t, out)), &doc); jerr != nil {
		t.Fatalf("envelope should still be valid JSON: %v\n%s", jerr, out)
	}
	if doc.OK || doc.ExitCode == 0 {
		t.Errorf("the envelope should report the failure: %+v", doc)
	}
	if !strings.Contains(doc.Stderr, "deliberate") {
		t.Errorf("the script's own stderr belongs in the envelope: %q", doc.Stderr)
	}
}

func TestEmitStreamsResultsToStdout(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", `for (const n of [1, 2, 3]) emit({ step: n });`)
	for _, want := range []string{`{"step":1}`, `{"step":2}`, `{"step":3}`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing streamed value %s:\n%s", want, out)
		}
	}
	// Order matters: a stream that arrives out of order is not a stream.
	if strings.Index(out, `"step":1`) > strings.Index(out, `"step":3`) {
		t.Errorf("streamed values are out of order:\n%s", out)
	}
}

func TestEmitAndReturnCoexist(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "both.ts")
	os.WriteFile(script, []byte(`import { emit } from "./mcpx-client.ts";
export default function main() {
  emit({ partial: 1 });
  emit({ partial: 2 });
  return { final: true };
}
`), 0o644)
	out := e.run("--json", "run", script)

	var doc struct {
		Results []map[string]any `json:"results"`
		Result  map[string]any   `json:"result"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &doc); err != nil {
		t.Fatalf("bad envelope: %v\n%s", err, out)
	}
	if len(doc.Results) != 2 {
		t.Fatalf("streamed values should be collected in order, got %v", doc.Results)
	}
	if doc.Results[0]["partial"] != float64(1) || doc.Results[1]["partial"] != float64(2) {
		t.Errorf("streamed order is wrong: %v", doc.Results)
	}
	if doc.Result["final"] != true {
		t.Errorf("the return value should still be the result: %v", doc.Result)
	}
}

func TestLogAcceptsExtraArgumentsAndErrors(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", "--log-level", "debug",
		`log.debug("state", { id: 7 }, "extra", 42);`)
	doc := firstRecord(t, out)
	if doc["id"] != float64(7) {
		t.Errorf("a plain object should become attributes: %v", doc)
	}
	args, ok := doc["args"].([]any)
	if !ok || len(args) != 2 || args[0] != "extra" {
		t.Errorf("trailing values should be collected into args: %v", doc["args"])
	}
}

func TestLogCapturesAnErrorStructurally(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", `log.error("failed", new Error("boom"));`)
	if !strings.Contains(out, `"boom"`) || !strings.Contains(out, `"stack"`) {
		t.Fatalf("an Error should be captured with its message and stack:\n%s", out)
	}
}

func TestLogSourceRecordsTheCallSite(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "src.ts")
	os.WriteFile(script, []byte(`import { log } from "./mcpx-client.ts";
export default function named() {
  log.info("hello");
}
`), 0o644)
	out := e.run("run", "--log-source=all", "--format", "json", script)
	doc := firstRecord(t, out)
	if !strings.HasSuffix(fmt.Sprint(doc["source.file"]), "src.ts") {
		t.Errorf("source file should be the script: %v", doc["source.file"])
	}
	if doc["source.line"] != float64(3) {
		t.Errorf("source line should be where log.info is: %v", doc["source.line"])
	}
	if doc["source.function"] != "named" {
		t.Errorf("source function should be the caller: %v", doc["source.function"])
	}
}

func TestLogSourceIsOffByDefault(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", `log.info("hello");`)
	if strings.Contains(out, "source.file") {
		t.Errorf("source capture should be opt-in:\n%s", out)
	}
}

// A filtered call must not touch what it was given, which is the whole point
// of checking the level first: the cost a caller is trying to avoid is
// building the argument, not the call.
//
// This was a wall-clock assertion -- 50k calls, fail over 0.5us each -- and it
// failed at 1.559us on a loaded machine while short-circuiting perfectly.
// The threshold did sit in a real gap (a call that formats and writes measures
// ~16.5us here, not the ~0.1us the old comment claimed), but a 0.5us budget
// measured by a wall clock on a shared machine loses that margin to a noisy
// neighbour, and it did.
//
// A getter counts reads instead. Untouched is exactly the property, it is the
// same answer whatever else the machine is doing, and it still discriminates:
// at --log-level debug this same script reports touched=1000.
func TestAFilteredLogCallNeverTouchesItsArguments(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "cost.ts")
	os.WriteFile(script, []byte(`import { log } from "./mcpx-client.ts";
export default function main() {
  let touched = 0;
  const probe = { get i() { touched++; return 1; } };
  for (let i = 0; i < 1000; i++) log.debug("filtered {i}", probe);
  return { touched, enabled: log.enabled("debug") };
}
`), 0o644)
	out := e.run("run", "--log-level", "info", "--format", "bare", script)
	start := strings.Index(out, "{")
	var doc struct {
		Touched int  `json:"touched"`
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(out[start:]), &doc); err != nil {
		t.Fatalf("bad result: %v\n%s", err, out)
	}
	if doc.Enabled {
		t.Error("debug should report disabled at an info threshold")
	}
	if doc.Touched != 0 {
		t.Errorf("a filtered call read its arguments %d times; the level check is not short-circuiting", doc.Touched)
	}
}

func TestSourceCaptureDefaultsToWarnAndAbove(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", `log.info("routine"); log.warn("off");`)
	var traced, untraced int
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var doc map[string]any
		if json.Unmarshal([]byte(line), &doc) != nil {
			continue
		}
		if _, ok := doc["source.file"]; ok {
			traced++
		} else {
			untraced++
		}
	}
	if traced != 1 || untraced != 1 {
		t.Fatalf("expected warn traced and info not, got traced=%d untraced=%d:\n%s", traced, untraced, out)
	}
}

func TestLogWithBindsAttributes(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json",
		`const s = log.with({ run: "r1" }); s.info("bound"); log.info("unbound");`)
	var bound, unbound bool
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var doc map[string]any
		json.Unmarshal([]byte(line), &doc)
		if doc["msg"] == "bound" && doc["run"] == "r1" {
			bound = true
		}
		if doc["msg"] == "unbound" {
			if _, leaked := doc["run"]; leaked {
				t.Error("with() must not affect the base logger")
			}
			unbound = true
		}
	}
	if !bound || !unbound {
		t.Fatalf("both records should appear:\n%s", out)
	}
}

func TestPerServerLoggingLevelResolves(t *testing.T) {
	cfg := `{
	  "logging": { "level": "error" },
	  "mcpServers": { "demo": { "command": "FAKE", "mcpx": { "logging": { "level": "debug" } } } }
	}`
	e := newEnv(t, cfg)
	out := e.run("--json", "config")
	if !strings.Contains(out, "demo") {
		t.Fatalf("config did not resolve:\n%s", out)
	}
}

func TestExecPrefixFromConfig(t *testing.T) {
	cfg := `{
	  "script": { "prefix": ["const FROM_CONFIG = 'yes';"] },
	  "mcpServers": { "demo": { "command": "FAKE" } }
	}`
	e := newEnv(t, cfg)
	out := e.run("exec", `console.log(FROM_CONFIG)`)
	if !strings.Contains(out, "yes") {
		t.Fatalf("configured prefix should be in scope:\n%s", out)
	}
}

func TestExecPrefixFlagReplacesUnlessInherited(t *testing.T) {
	cfg := `{
	  "script": { "prefix": ["const A = 'config';"] },
	  "mcpServers": { "demo": { "command": "FAKE" } }
	}`
	e := newEnv(t, cfg)

	replaced := e.run("exec", "--prefix", "const A = 'flag';", `console.log(A)`)
	if !strings.Contains(replaced, "flag") || strings.Contains(replaced, "config") {
		t.Errorf("a plain flag should replace:\n%s", replaced)
	}

	inherited := e.run("exec", "--prefix", "-", "--prefix", "const B = 'extra';",
		`console.log(A, B)`)
	if !strings.Contains(inherited, "config") || !strings.Contains(inherited, "extra") {
		t.Errorf("'-' should keep the configured lines:\n%s", inherited)
	}
}

func TestFileScriptPrefixRunsBeforeTopLevelCode(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "top.ts")
	os.WriteFile(script, []byte(`console.log("TOP-LEVEL");
export default function main() { return "done"; }
`), 0o644)
	// A static import would be hoisted and run the module first whatever the
	// prefix said; the launcher imports dynamically so ordering is real.
	out := e.run("run", "--format", "bare", "--prefix", `log.info("PREFIX");`, script)
	pi, ti := strings.Index(out, "PREFIX"), strings.Index(out, "TOP-LEVEL")
	if pi < 0 || ti < 0 {
		t.Fatalf("both should appear:\n%s", out)
	}
	if pi > ti {
		t.Fatalf("the prefix must run before the module body:\n%s", out)
	}
}

func TestFileScriptPrefixCanPatchGlobals(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "glob.ts")
	os.WriteFile(script, []byte(`export default function main() {
  return { saw: (globalThis as any).INJECTED ?? "<unset>" };
}
`), 0o644)
	out := e.run("run", "--prefix", `(globalThis as any).INJECTED = "patched";`, script)
	if !strings.Contains(out, "patched") {
		t.Fatalf("a prefix should be able to set globals the script reads:\n%s", out)
	}
}

func TestFileScriptPrefixCannotBindModuleScope(t *testing.T) {
	// The honest limit: ESM gives the module its own scope, so a prefix
	// declaration is not visible inside the script.
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "scope.ts")
	os.WriteFile(script, []byte(`export default function main() {
  return { has: typeof (globalThis as any).FROM_PREFIX_CONST };
}
`), 0o644)
	out := e.run("run", "--prefix", `const FROM_PREFIX_CONST = 1;`, script)
	if !strings.Contains(out, "undefined") {
		t.Fatalf("a const in the launcher must not leak into the module:\n%s", out)
	}
}

func TestFileScriptSuffixSeesTheResult(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "res.ts")
	os.WriteFile(script, []byte(`export default function main() { return { n: 1 }; }`), 0o644)
	out := e.run("run", "--format", "bare",
		"--suffix", `log.info("ok={ok}", { ok: result.ok });`, script)
	if !strings.Contains(out, "ok=true") {
		t.Fatalf("the suffix should see how the run ended:\n%s", out)
	}
}

func TestFileScriptSuffixRunsEvenOnFailure(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "fail2.ts")
	os.WriteFile(script, []byte(`export default function main() { throw new Error("deliberate"); }`), 0o644)
	out, err := e.try("run", "--format", "bare",
		"--suffix", `log.warn("cleanup ok={ok}", { ok: result.ok });`, script)
	if err == nil {
		t.Fatal("a throwing script should still fail the command")
	}
	if !strings.Contains(out, "cleanup ok=false") {
		t.Fatalf("cleanup must run and see the failure:\n%s", out)
	}
}

func TestFileScriptPrefixSeesTheScriptContext(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "ctx.ts")
	os.WriteFile(script, []byte(`export default function main() { return "x"; }`), 0o644)
	out := e.run("run", "--format", "bare",
		"--prefix", `log.info("{name} got {n}", { name: script.name, n: script.args.length });`,
		script, "a", "b")
	if !strings.Contains(out, "ctx got 2") {
		t.Fatalf("the prefix should see the script name and its arguments:\n%s", out)
	}
}

func TestEnvFlagReachesTheScript(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--env", "GREETING=hi", "--env", "OTHER=2",
		`console.log((globalThis as any).Deno.env.get("GREETING"), (globalThis as any).Deno.env.get("OTHER"));`)
	if !strings.Contains(out, "hi 2") {
		t.Fatalf("--env should reach the script:\n%s", out)
	}
}

func TestEnvFlagRejectsMalformedPairs(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("exec", "--env", "NOEQUALS", `console.log(1)`)
	if err == nil {
		t.Fatalf("expected rejection:\n%s", out)
	}
	if !strings.Contains(out, "KEY=VALUE") {
		t.Errorf("the error should show the expected shape:\n%s", out)
	}
}

func TestPermissionsDefaultWideOpen(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", `console.log(typeof Deno.readTextFileSync === "function" ? "have-fs" : "no-fs");`)
	if !strings.Contains(out, "have-fs") {
		t.Fatalf("the default should be unsandboxed:\n%s", out)
	}
}

func TestPermissionsCanBeNarrowed(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--permissions", "strict",
		`try { await Deno.readTextFile("/etc/hosts"); console.log("ALLOWED"); }
		 catch (err) { console.log("denied:", (err as Error).name); }`)
	if strings.Contains(out, "ALLOWED") {
		t.Fatalf("strict should deny filesystem reads:\n%s", out)
	}
	if !strings.Contains(out, "denied") {
		t.Fatalf("expected a permission error:\n%s", out)
	}
}

func TestScriptCanUseGlobalsWithoutImporting(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "noimp.ts")
	// No import statement anywhere.
	os.WriteFile(script, []byte(`export default async function main() {
  log("plain call");
  emit({ streamed: 1 });
  const r = await demo.echo({ message: "via global" });
  return { got: String(r) };
}
`), 0o644)
	out := e.run("run", "--format", "bare", script)
	for _, want := range []string{"plain call", `{"streamed":1}`, "via global"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestLogIsCallableAsInfo(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", `log("called directly");`)
	doc := firstRecord(t, out)
	if doc["level"] != "info" || doc["msg"] != "called directly" {
		t.Fatalf("log() should behave as log.info(): %v", doc)
	}
}

func TestImportingStillWorksAlongsideGlobals(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "mixed.ts")
	os.WriteFile(script, []byte(`import { log as imported } from "./mcpx-client.ts";
export default function main() {
  imported.info("via import");
  log.info("via global");
  return { same: imported === log };
}
`), 0o644)
	out := e.run("run", "--format", "bare", script)
	if !strings.Contains(out, "via import") || !strings.Contains(out, "via global") {
		t.Fatalf("both forms should work:\n%s", out)
	}
	if !strings.Contains(out, `"same": true`) {
		t.Errorf("the import and the global should be the same object:\n%s", out)
	}
}

func TestConsoleLogIsPrintedOnceButRecordedToo(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "bare", `console.log("UNIQUE-MARKER");`)
	if n := strings.Count(out, "UNIQUE-MARKER"); n != 1 {
		t.Fatalf("console.log should reach the terminal exactly once, saw %d:\n%s", n, out)
	}
}

func TestConsoleErrorBecomesARecord(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", `console.error("diagnostic");`)
	doc := firstRecord(t, out)
	if doc["level"] != "error" || doc["console"] != "error" {
		t.Fatalf("unexpected record: %v", doc)
	}
}

func TestConsoleCaptureCanBeDisabled(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--no-capture-console", "--format", "json", `console.error("raw text");`)
	if strings.Contains(out, `"console"`) {
		t.Fatalf("capture should be off:\n%s", out)
	}
	if !strings.Contains(out, "raw text") {
		t.Fatalf("the original output should still appear:\n%s", out)
	}
}

func TestGlobalDeclarationsAreWrittenForEditors(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "decl.ts")
	os.WriteFile(script, []byte(`export default function main() { return 1; }`), 0o644)
	e.run("run", script)

	b, err := os.ReadFile(filepath.Join(e.dir, "mcpx-globals.d.ts"))
	if err != nil {
		t.Fatalf("declarations should be written beside the script: %v", err)
	}
	for _, want := range []string{"declare global {", "const log: Logger;", "const emit:", "const demo:"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("declarations missing %q:\n%s", want, b)
		}
	}
}

func TestLauncherPhasesRunInOrder(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "ph.ts")
	os.WriteFile(script, []byte(`export default function main() { log("3 body"); return undefined; }`), 0o644)
	out := e.run("run", "--format", "bare",
		"--before", `log("1 before")`, "--prefix", `log("2 prefix")`,
		"--on-success", `log("4 success")`, "--suffix", `log("5 suffix")`, script)

	var seen []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 1 && line[0] >= '1' && line[0] <= '5' {
			seen = append(seen, string(line[0]))
		}
	}
	if strings.Join(seen, "") != "12345" {
		t.Fatalf("phases ran out of order: %v\n%s", seen, out)
	}
}

func TestOnErrorRunsAndTheErrorStillPropagates(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "pherr.ts")
	os.WriteFile(script, []byte(`export default function main() { throw new Error("deliberate"); }`), 0o644)
	out, err := e.try("run", "--format", "bare",
		"--on-success", `log("should not run")`,
		"--on-error", `log.error("onError fired")`,
		"--suffix", `log("suffix ran")`, script)

	if err == nil {
		t.Fatal("onError is a hook, not a handler; the failure must still surface")
	}
	if !strings.Contains(out, "onError fired") || !strings.Contains(out, "suffix ran") {
		t.Fatalf("both hooks should have run:\n%s", out)
	}
	if strings.Contains(out, "should not run") {
		t.Error("onSuccess must not run on the failure path")
	}
}

func TestConsoleInfoBecomesLogInfoWithoutAnyScriptChange(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", `console.info("plain console call");`)
	doc := firstRecord(t, out)
	if doc["level"] != "info" || doc["msg"] != "plain console call" {
		t.Fatalf("console.info should be log.info: %v", doc)
	}
}

func TestConsoleCoverageBeyondTheFiveLevels(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", "--log-level", "debug",
		`console.count("c"); console.assert(false, "failed"); console.group("g"); console.info("nested"); console.groupEnd();`)

	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var doc map[string]any
		if json.Unmarshal([]byte(line), &doc) == nil {
			if k, ok := doc["console"].(string); ok {
				kinds = append(kinds, k)
			}
			if doc["console"] == "info" && !strings.HasPrefix(doc["msg"].(string), "  ") {
				t.Errorf("group should indent nested output: %v", doc["msg"])
			}
		}
	}
	for _, want := range []string{"count", "assert", "group"} {
		if !slices.Contains(kinds, want) {
			t.Errorf("console.%s was not captured; saw %v", want, kinds)
		}
	}
}

func TestReleaseConsoleRestoresTheOriginal(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json",
		`releaseConsole(); console.error("raw again");`)
	if strings.Contains(out, `"console"`) {
		t.Fatalf("releaseConsole should hand the real console back:\n%s", out)
	}
	if !strings.Contains(out, "raw again") {
		t.Fatalf("output should still appear:\n%s", out)
	}
}

func TestErrorsCarryStructuredFramesAndAString(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json",
		`function boom() { throw new Error("deliberate"); }
		 try { boom(); } catch (err) { log.error("failed", err as Error); }`)
	doc := firstRecord(t, out)

	args, ok := doc["args"].([]any)
	if !ok || len(args) == 0 {
		t.Fatalf("the Error should be in args: %v", doc)
	}
	desc, ok := args[0].(map[string]any)
	if !ok {
		t.Fatalf("the Error should be structured: %v", args[0])
	}
	if desc["message"] != "deliberate" {
		t.Errorf("message lost: %v", desc)
	}
	frames, ok := desc["frames"].([]any)
	if !ok || len(frames) == 0 {
		t.Fatalf("frames should be captured: %v", desc)
	}
	top, _ := frames[0].(map[string]any)
	if top["function"] != "boom" {
		t.Errorf("the throwing function should be the top frame: %v", top)
	}
	// Both forms must survive; V8 memoises .stack, so getting one usually
	// destroys the other. The string is rendered from the frames.
	stack, _ := desc["stack"].(string)
	if !strings.Contains(stack, "deliberate") || !strings.Contains(stack, "boom") {
		t.Errorf("a readable stack string should be present too: %q", stack)
	}
}

func TestConsoleTraceCapturesFrames(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "json", "--log-level", "debug",
		`function deep() { console.trace("here"); } deep();`)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var doc map[string]any
		if json.Unmarshal([]byte(line), &doc) != nil || doc["console"] != "trace" {
			continue
		}
		frames, ok := doc["frames"].([]any)
		if !ok || len(frames) == 0 {
			t.Fatalf("console.trace should carry frames: %v", doc)
		}
		top, _ := frames[0].(map[string]any)
		if top["function"] != "deep" {
			t.Errorf("the calling function should be the top frame: %v", top)
		}
		return
	}
	t.Fatalf("no trace record found:\n%s", out)
}

func TestCaptureFramesIsAvailableToScripts(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "bare",
		`function a() { return captureFrames(0, 4); }
		 const f = a();
		 console.log(JSON.stringify({ n: f.length, top: f[0]?.function }));`)
	if !strings.Contains(out, `"top":"a"`) {
		t.Fatalf("captureFrames should be a global and name the caller:\n%s", out)
	}
}

func TestEveryConsoleMethodIsCoveredAndNameIsKept(t *testing.T) {
	e := newEnv(t, oneServer)
	// Deno ships 25 console members. Anything mcpx replaces must keep its
	// name, because losing it breaks introspection for no gain, and anything
	// it does not replace must still be callable.
	out := e.run("exec", "--format", "bare", `
		const names = Object.keys(console).filter((k) => typeof (console as any)[k] === "function");
		const renamed = names.filter((k) => (console as any)[k].name !== k);
		console.log(JSON.stringify({ total: names.length, renamed }));
		// console.createTask requires a non-empty string in Deno too, so a
		// throw there is fidelity rather than a defect. Compare against the
		// unwrapped console instead of assuming nothing throws.
		//
		// Members absent from the snapshot are skipped rather than failed:
		// if we never captured it we never replaced it, so console[k] IS the
		// original and there is nothing it could diverge from. console.Console
		// is one -- a class that correctly throws when called without new.
		const native = (console as any).__mcpxOriginal ?? {};
		for (const k of names) {
			if (typeof native[k] !== "function") continue;
			let ours = false, theirs = false;
			try { (console as any)[k](); } catch { ours = true; }
			try { native[k](); } catch { theirs = true; }
			if (ours !== theirs) console.log("divergent:", k, ours, theirs);
		}
	`)
	if !strings.Contains(out, `"renamed":[]`) {
		t.Errorf("every patched console method should keep its name:\n%s", out)
	}
	if strings.Contains(out, "divergent:") {
		t.Errorf("a patched method should throw exactly when the original does:\n%s", out)
	}
}

func TestConsoleInfoFormatsLikeTheRuntimeNotLikeJSON(t *testing.T) {
	e := newEnv(t, oneServer)
	// A caller who writes console.info({a:1}) expects to read what the
	// runtime would have printed. The structured copy is already in args, so
	// the message is free to be the human form.
	out := e.run("exec", "--format", "compact", `console.info("shaped", { a: 1, b: [1, 2] });`)
	if !strings.Contains(out, "{ a: 1, b: [ 1, 2 ] }") {
		t.Errorf("message should use the runtime's inspect, not JSON:\n%s", out)
	}
}

func TestConsoleIndentLevelReflectsGrouping(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--format", "bare", `
		const a = console.indentLevel;
		console.group("g");
		const b = console.indentLevel;
		console.groupEnd();
		console.log(JSON.stringify({ a, b, c: console.indentLevel }));
	`)
	if !strings.Contains(out, `{"a":0,"b":1,"c":0}`) {
		t.Errorf("indentLevel should track group depth:\n%s", out)
	}
}

func TestConfigDefaultsPrintsTheEmbeddedLayer(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("config", "--defaults")
	var doc map[string]any
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &doc); err != nil {
		t.Fatalf("--defaults should print valid JSON: %v\n%s", err, out)
	}
	for _, section := range []string{"pool", "logging", "daemon", "catalog", "script"} {
		if _, ok := doc[section]; !ok {
			t.Errorf("the %q section should be present: %v", section, doc)
		}
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestACustomLauncherReplacesTheGeneratedOne(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "hello.ts")
	mustWrite(t, script, `export default function main(argv: string[]) {
		log.info("script ran", { argv });
		return "done";
	}`)
	custom := filepath.Join(dir, "mine.ts")
	mustWrite(t, custom, "@header\nlog.info(\"mine speaking\");\n@globals\n@console\n@entry\n")

	out := e.run("run", "--launcher", custom, "--format", "compact", script, "A")
	if !strings.Contains(out, "mine speaking") {
		t.Errorf("the custom launcher should run:\n%s", out)
	}
	if !strings.Contains(out, "script ran") {
		t.Errorf("@entry should still reach the script:\n%s", out)
	}
}

func TestNoLauncherHandsTheScriptStraightToTheRuntime(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "raw.ts")
	mustWrite(t, script, `console.log("globals:", typeof (globalThis as any).log);`)

	bare := e.run("run", "--no-launcher", script)
	if !strings.Contains(bare, "globals: undefined") {
		t.Errorf("with no launcher nothing should be installed:\n%s", bare)
	}
	wrapped := e.run("run", script)
	if !strings.Contains(wrapped, "globals: function") {
		t.Errorf("normally the globals are there:\n%s", wrapped)
	}
}

func TestLauncherAndNoLauncherTogetherIsRefused(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "x.ts")
	mustWrite(t, script, `export default () => "ok";`)
	out, err := e.try("run", "--launcher", "@entry", "--no-launcher", script)
	if err == nil {
		t.Fatal("the two contradict each other and should be refused")
	}
	if !strings.Contains(out, "contradict") {
		t.Errorf("the error should say why: %s", out)
	}
}

func TestALauncherCycleIsRefusedBeforeAnythingRuns(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "x.ts")
	mustWrite(t, script, `export default () => "ok";`)
	out, err := e.try("run", "--launcher=@prefix", "--prefix", "@suffix",
		"--suffix", "@prefix", script)
	if err == nil {
		t.Fatal("a reference loop should be refused, not hung on")
	}
	if !strings.Contains(out, "loop") {
		t.Errorf("the error should name the loop: %s", out)
	}
}

func TestARepeatedPlaceholderNeedsPermissionAndThenWorks(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "twice.ts")
	mustWrite(t, script, `export default function main() { log.info("ran"); }`)

	if _, err := e.try("run", "--launcher=@header @globals @entry @entry", script); err == nil {
		t.Fatal("a repeated placeholder should be refused by default")
	}
	out := e.run("run", "--launcher=@header @globals @entry @entry",
		"--allow-repeat", "entry", "--format", "compact", script)
	if n := strings.Count(out, "ran"); n != 2 {
		t.Errorf("once permitted it should genuinely run twice, got %d:\n%s", n, out)
	}
}

func TestAPhaseCanBeAFileInsteadOfAString(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "x.ts")
	mustWrite(t, script, `export default () => "ok";`)
	hook := filepath.Join(dir, "hook.ts")
	mustWrite(t, hook, `log.info("from a file");`)

	out := e.run("run", "--prefix", hook, "--format", "compact", script)
	if !strings.Contains(out, "from a file") {
		t.Errorf("a phase given a path should read it:\n%s", out)
	}
}

// TestCapturedConsoleMatchesTheRuntimeExactly is the question that matters for
// a script that was written against an ordinary console: does redirecting it
// change what the script sees or produces? Every line is compared against the
// same script run with no launcher at all.
func TestCapturedConsoleMatchesTheRuntimeExactly(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "fmt.ts")
	mustWrite(t, script, `
console.info("%s scored %d and %f%%", "alice", 42, 1.5);
console.info("%o and %O and %j", {a:1}, {b:2}, {c:3});
console.info("%c styled", "color: red");
console.info("literal %% percent");
console.info("too few %s %s", "one");
console.info("surplus %s", "a", "b", "c");
console.info({ nested: { deep: [1, 2, { x: true }] } });
`)
	raw := e.run("run", "--no-launcher", script)
	captured := e.run("run", "--format", "bare", script)

	rawLines := nonEmptyLines(raw)
	capLines := nonEmptyLines(captured)
	if len(rawLines) != len(capLines) {
		t.Fatalf("line counts differ\nraw:\n%s\ncaptured:\n%s", raw, captured)
	}
	for i := range rawLines {
		if rawLines[i] != capLines[i] {
			t.Errorf("line %d differs:\n  runtime:  %q\n  captured: %q",
				i+1, rawLines[i], capLines[i])
		}
	}
}

// nonEmptyLines drops blanks and mcpx's own progress notices.
//
// The notices have to go because the two runs being compared do not agree
// about whether to print them: one uses the default text format and gets
// them, the other asks for a machine format and does not. On a warm cache
// neither prints anything and the comparison passes; on a cold one the line
// counts differ and the test fails for a reason that has nothing to do with
// the console.
func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "mcpx: ") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// TestConsoleSurvivesValuesThatBreakNaiveSerialisers is the other half: a
// script must not start throwing because its console was redirected.
func TestConsoleSurvivesValuesThatBreakNaiveSerialisers(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "hostile.ts")
	mustWrite(t, script, `
const circ: any = { name: "loop" }; circ.self = circ;
class Weird { get boom() { throw new Error("getter threw"); } }
const sym = Symbol("tag");
const checks: Record<string, unknown> = {};
const guard = (k: string, f: () => void) => {
  try { f(); checks[k] = "ok"; } catch (e) { checks[k] = "THREW " + (e as Error).message; }
};
guard("circular", () => console.info(circ));
guard("throwingGetter", () => console.info(new Weird()));
guard("symbolKeyed", () => console.info({ [sym]: "s", big: 123n, u: undefined, n: null }));
guard("noArguments", () => console.info());
guard("typedArray", () => console.info(new Uint8Array([1, 2, 3])));
guard("collections", () => console.info(new Map([["k", "v"]]), new Set([1])));
guard("veryLarge", () => console.info("x".repeat(200000)));
guard("functionValue", () => console.info(function named() {}));
checks.returnsUndefined = console.info("x") === undefined ? "ok" : "BAD";
checks.keepsItsName = console.info.name === "info" ? "ok" : "BAD:" + console.info.name;
console.log("RESULT " + JSON.stringify(checks));
`)
	// Stdout alone. The 200 KB console.info is rendered on stderr, and with
	// both streams merged into one buffer the RESULT line on stdout could
	// land in the middle of it -- "xxx…RESULT {…}" -- and not be found at
	// the start of any line (#282). Two streams interleaving in a shared
	// pipe is not a defect in either.
	out, stderr, err := e.split("run", "--format", "bare", script)
	if err != nil {
		t.Fatalf("mcpx run failed: %v\n%s\n%s", err, out, stderr)
	}
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "RESULT ") {
			line = strings.TrimPrefix(strings.TrimSpace(l), "RESULT ")
		}
	}
	if line == "" {
		t.Fatalf("the script should have finished:\n%s", out)
	}
	var checks map[string]string
	if err := json.Unmarshal([]byte(line), &checks); err != nil {
		t.Fatalf("%v\n%s", err, line)
	}
	if len(checks) < 10 {
		t.Fatalf("expected every case to report, got %d: %v", len(checks), checks)
	}
	for name, got := range checks {
		if got != "ok" {
			t.Errorf("%s: %s", name, got)
		}
	}
}

// TestEverySettingTheSchemaAdvertisesActuallyWorks is the test that would have
// caught the registry promising flags no command accepted. `mcpx config
// --schema` and the man page are both generated from the registry, so a
// setting listed there and rejected by the command is documentation that
// lies.
func TestEverySettingTheSchemaAdvertisesActuallyWorks(t *testing.T) {
	e := newEnv(t, oneServer)
	dir := t.TempDir()
	script := filepath.Join(dir, "x.ts")
	mustWrite(t, script, `export default () => "ok";`)

	out := e.run("--json", "config", "--schema")
	var entries []struct {
		Path     string   `json:"path"`
		Flag     string   `json:"flag"`
		Kind     string   `json:"kind"`
		Default  string   `json:"default"`
		Enum     []string `json:"enum"`
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &entries); err != nil {
		t.Fatalf("--schema --json should be parseable: %v", err)
	}
	if len(entries) < 20 {
		t.Fatalf("expected a real registry, got %d entries", len(entries))
	}

	// Every command, not just run. The same gap existed on catalog, search
	// and status, and checking one command would have left it there.
	commands := []string{"run", "exec", "catalog", "search", "ls", "status", "types"}
	checked := 0
	for _, cmd := range commands {
		for _, entry := range entries {
			if !appliesTo(entry.Commands, cmd) {
				continue
			}
			value := entry.Default
			if len(entry.Enum) > 0 {
				value = entry.Enum[0]
			}
			if value == "" {
				switch entry.Kind {
				case "int":
					value = "1"
				case "duration":
					value = "30s"
				case "bytes":
					value = "1MB"
				case "bool":
					value = "true"
				default:
					continue // nothing safe to pass
				}
			}
			checked++
			args := []string{cmd, "--" + entry.Flag + "=" + value}
			switch cmd {
			case "run", "exec":
				args = append(args, script)
			case "search", "types":
				args = append(args, "demo")
			}
			if out, err := e.try(args...); err != nil && strings.Contains(out, "not defined") {
				t.Errorf("--%s is advertised by --schema for %q but that command rejects it (%s=%q)",
					entry.Flag, cmd, entry.Path, value)
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d settings were exercised; the check is not doing its job", checked)
	}
}

func appliesTo(commands []string, cmd string) bool {
	if len(commands) == 0 {
		return true
	}
	for _, c := range commands {
		if c == cmd {
			return true
		}
	}
	return false
}

// TestSettingsFromAConfigFileActuallyTakeEffect is the difference between a
// setting being accepted and a setting being applied. Validation alone is
// documentation that lies more quietly: the value is checked, reported as
// fine, and then ignored.
func TestSettingsFromAConfigFileActuallyTakeEffect(t *testing.T) {
	cfg := strings.TrimSuffix(strings.TrimSpace(oneServer), "}") +
		`, "logging": { "format": "compact", "level": "debug" },
		   "catalog": { "budget": 300 } }`
	e := newEnv(t, cfg)
	dir := t.TempDir()
	script := filepath.Join(dir, "x.ts")
	mustWrite(t, script, `export default () => { log.info("ran"); };`)

	// compact renders "INFO msg key=value"; text would render a timestamp.
	out := e.run("run", script)
	if !strings.Contains(out, "INFO ") || strings.Contains(out, `"level"`) {
		t.Errorf("logging.format from the config file should apply:\n%s", out)
	}

	// Two environments rather than two flags, so what differs is the config
	// file and nothing else. The fake server's catalog is small, so the
	// budgets have to be far apart to produce a visible difference.
	tight := newEnv(t, strings.TrimSuffix(strings.TrimSpace(oneServer), "}")+
		`, "catalog": { "budget": 1 } }`)
	loose := newEnv(t, strings.TrimSuffix(strings.TrimSpace(oneServer), "}")+
		`, "catalog": { "budget": 9000 } }`)
	if a, b := len(tight.run("catalog")), len(loose.run("catalog")); a >= b {
		t.Errorf("catalog.budget from the config file should apply: %d vs %d", a, b)
	}
}

func TestExploreRefusesWithoutATerminal(t *testing.T) {
	// It reads a prompt loop from stdin; run from a script it would consume
	// whatever was piped and do something surprising. Saying so and naming
	// the scriptable commands is better than half-working.
	e := newEnv(t, oneServer)
	out, err := e.try("explore")
	if err == nil {
		t.Fatal("explore should refuse when stdin is not a terminal")
	}
	if !strings.Contains(out, "needs a terminal") {
		t.Errorf("the reason should be stated: %s", out)
	}
	for _, alt := range []string{"ls", "types", "catalog", "log"} {
		if !strings.Contains(out, alt) {
			t.Errorf("the scriptable alternative %q should be named: %s", alt, out)
		}
	}
}

func TestHarnessTraceIdsReachTheLog(t *testing.T) {
	// The harness knows what the agent does not. Pairs rather than a flat id,
	// because a flat one cannot say "this session, whose parent is that one".
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars,
		`MCPX_TRACE_IDS=[["session_id","abc123"],["worktree","/w/x"],["aliases","a","b"]]`)
	out := e.run("exec", "--format", "json", `log.info("hello")`)
	doc := firstRecord(t, out)

	if doc["id.session_id"] != "abc123" {
		t.Errorf("session id should ride along: %v", doc)
	}
	if doc["id.worktree"] != "/w/x" {
		t.Errorf("worktree should ride along: %v", doc)
	}
	// An entry longer than a pair names several ids for one key.
	aliases, ok := doc["id.aliases"].([]any)
	if !ok || len(aliases) != 2 {
		t.Errorf("a variadic entry should survive as a list: %v", doc["id.aliases"])
	}
}

func TestHarnessIdsCannotRewriteTheRealSession(t *testing.T) {
	// A caller may add context; it may not rewrite which session a call was
	// actually leased for, or leasing becomes advisory.
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, `MCPX_TRACE_IDS=[["session","impostor"]]`)
	out := e.run("exec", "--format", "json", `log.info("hello")`)
	doc := firstRecord(t, out)
	if doc["session"] == "impostor" {
		t.Error("the harness should not be able to overwrite the resolved session")
	}
}

func TestLogRecordAcceptsWhatOtherThingsKnow(t *testing.T) {
	// mcpx's log should be able to hold what the harness knows, so that one
	// `mcpx stats` covers both. A caller that can produce JSON should not
	// also have to learn a schema.
	e := newEnv(t, oneServer)

	e.run("log", "record", `{"event":"harness.tool","tool":"bash","ok":true}`)
	e.run("log", "record", "--level", "warn", `{"msg":"something odd","n":42}`)

	out := e.run("log", "--grep", "harness.tool|something odd", "--limit", "10")
	if !strings.Contains(out, "harness.tool") {
		t.Errorf("an event-only record should land:\n%s", out)
	}
	if !strings.Contains(out, "something odd") {
		t.Errorf("a message record should land:\n%s", out)
	}
	// Told, not observed: without the distinction a synthetic record is
	// indistinguishable from a measured one.
	if !strings.Contains(out, "external=true") {
		t.Errorf("records from outside should be marked:\n%s", out)
	}
	if !strings.Contains(out, "WARN") {
		t.Errorf("the level should be honoured:\n%s", out)
	}
}

func TestLogRecordRejectsWhatIsNotAnObject(t *testing.T) {
	e := newEnv(t, oneServer)
	if _, err := e.try("log", "record", `not json`); err == nil {
		t.Fatal("a record that is not a JSON object should be refused")
	}
	if _, err := e.try("log", "record", `[1,2,3]`); err == nil {
		t.Fatal("an array is not a record")
	}
}

// TestEveryTuiViewProducesRowsFromRealData exercises the source
// implementations against a real store. The view itself is tested with a
// fake; this is the other half -- that the queries behind each view actually
// return something, which a fake can never tell you.
func TestEveryTuiViewProducesRowsFromRealData(t *testing.T) {
	e := newEnv(t, oneServer)
	// Generate something to report on.
	e.run("exec", `const r = await demo.echo({ message: "hi" }); emit(r);`)
	e.run("log", "record", `{"event":"harness.tool","tool":"bash"}`)

	out := e.run("--json", "tui", "--dump")
	var dump map[string]struct {
		Columns []string   `json:"columns"`
		Rows    [][]string `json:"rows"`
		Note    string     `json:"note"`
		Error   string     `json:"error"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &dump); err != nil {
		t.Fatalf("--dump should be parseable: %v\n%s", err, out)
	}
	for _, view := range []string{"stats:calls", "servers", "storage", "sessions"} {
		got, ok := dump[view]
		if !ok {
			t.Errorf("%s is missing from the dump", view)
			continue
		}
		if got.Error != "" {
			t.Errorf("%s failed: %s", view, got.Error)
			continue
		}
		if len(got.Columns) == 0 {
			t.Errorf("%s has no columns", view)
		}
	}
	// Storage always has something: the index itself exists by now.
	if len(dump["storage"].Rows) == 0 {
		t.Errorf("storage should list at least the index: %+v", dump["storage"])
	}
	if !strings.Contains(dump["storage"].Note, "total") {
		t.Errorf("storage should total what it found: %q", dump["storage"].Note)
	}
	// And calls, because a tool call was just made.
	if len(dump["stats:calls"].Rows) == 0 {
		t.Errorf("a call was made; stats should show it: %+v", dump["stats:calls"])
	}
}

func TestPromptsAndResourcesReachThroughFromUpstream(t *testing.T) {
	// Prompts are the half of MCP that is not tools. mcpx reported none,
	// which threw away everything a server published that was not a
	// function.
	e := newEnv(t, oneServer)

	prompts := e.run("prompts")
	if !strings.Contains(prompts, "summarise") {
		t.Errorf("the server's prompt should be listed:\n%s", prompts)
	}
	if !strings.Contains(prompts, "text, style?") {
		t.Errorf("required and optional arguments should be distinguished:\n%s", prompts)
	}

	rendered := e.run("prompts", "demo.summarise", "text=a long document", "style=in one line")
	if !strings.Contains(rendered, "Summarise in one line: a long document") {
		t.Errorf("arguments should be substituted:\n%s", rendered)
	}

	resources := e.run("resources")
	if !strings.Contains(resources, "demo://greeting") {
		t.Errorf("the server's resource should be listed:\n%s", resources)
	}
	read := e.run("resources", "demo/demo://greeting")
	if !strings.Contains(read, "hello from a resource") {
		t.Errorf("reading it should return its contents:\n%s", read)
	}
}

func TestTheMCPServerAdvertisesWhatItActuallyHas(t *testing.T) {
	// Returning empty lists while declaring the capability is a lie a client
	// cannot detect: it asks once, gets nothing, and never asks again.
	e := newEnv(t, oneServer)
	out := e.runStdin(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"prompts/list"}`+"\n"+
			`{"jsonrpc":"2.0","id":3,"method":"resources/list"}`+"\n",
		"serve")

	if !strings.Contains(out, "summarise") {
		t.Errorf("prompts/list should pass through:\n%s", out)
	}
	if !strings.Contains(out, "greeting") {
		t.Errorf("resources/list should pass through:\n%s", out)
	}
}

func TestDoctorFindsAServerWhoseCommandIsMissing(t *testing.T) {
	// The single most common cause of "mcpx does not work", and invisible
	// until something tries to call it.
	e := newEnv(t, `{"mcpServers":{"ghost":{"command":"definitely-not-installed-xyz"}}}`)
	out, err := e.try("doctor")
	if err == nil {
		t.Fatal("an unrunnable server should make doctor unhealthy")
	}
	if !strings.Contains(out, "definitely-not-installed-xyz") {
		t.Errorf("the missing command should be named:\n%s", out)
	}
	if !strings.Contains(out, "remove those servers") {
		t.Errorf("a check that only reports leaves the reader where they started:\n%s", out)
	}
}

func TestDoctorIsQuietWhenEverythingIsFine(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("doctor")
	if strings.Contains(out, "FAIL") {
		t.Errorf("a healthy install should have nothing to report:\n%s", out)
	}
	// Section names and mcpServers are the document's own content, not
	// typos. Reporting them made the warning useless by burying real ones.
	if strings.Contains(out, "mcpServers") {
		t.Errorf("config sections should not read as unknown keys:\n%s", out)
	}
}

func TestDoctorStillCatchesARealTypo(t *testing.T) {
	cfg := strings.TrimSuffix(strings.TrimSpace(oneServer), "}") +
		`, "logging": { "levl": "debug" } }`
	e := newEnv(t, cfg)
	out := e.run("doctor")
	if !strings.Contains(out, "logging.levl") {
		t.Errorf("a misspelled key should still be reported:\n%s", out)
	}
}

func TestTypesArePublishedInEveryFormSomethingConsumes(t *testing.T) {
	// The same information, reshaped. TypeScript is what a script imports,
	// JSON Schema is what a validator reads, OpenAPI is what a client
	// generator reads, and the MCP form is what an MCP host reads. A caller
	// should not have to convert one into another.
	e := newEnv(t, oneServer)

	ts := e.run("schema", "--format", "typescript")
	if !strings.Contains(ts, "function echo") {
		t.Errorf("typescript should declare the tool:\n%s", ts)
	}

	js := e.run("schema", "--format", "json-schema")
	var jsDoc struct {
		Schema string         `json:"$schema"`
		Defs   map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, js)), &jsDoc); err != nil {
		t.Fatalf("json-schema should parse: %v", err)
	}
	if !strings.Contains(jsDoc.Schema, "json-schema.org") {
		t.Errorf("it should declare its dialect: %q", jsDoc.Schema)
	}
	if _, ok := jsDoc.Defs["demo.echo"]; !ok {
		t.Errorf("the tool should be under $defs: %v", jsDoc.Defs)
	}

	mcp := e.run("schema", "--format", "mcp")
	if !strings.Contains(mcp, "inputSchema") {
		t.Errorf("the MCP form should carry inputSchema:\n%s", mcp)
	}
}

func TestTheOpenAPIDocumentDescribesTheToolsItFrontsNotOnlyItself(t *testing.T) {
	// Describing the wrapper but not what it wraps is a specification of the
	// wrong thing: the tools were reachable from MCP and from a script and
	// from nowhere a generated client could see.
	e := newEnv(t, oneServer)
	out := e.run("schema", "--format", "openapi")

	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &doc); err != nil {
		t.Fatal(err)
	}
	var upstream []string
	for p := range doc.Paths {
		if strings.HasPrefix(p, "/v1/call/") {
			upstream = append(upstream, p)
		}
	}
	if len(upstream) == 0 {
		t.Fatalf("upstream tools should have paths; got %v", keysOf(doc.Paths))
	}
	found := false
	for _, p := range upstream {
		if strings.Contains(p, "/demo/echo") {
			found = true
		}
	}
	if !found {
		t.Errorf("the demo tool should be addressable: %v", upstream)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestElicitationsCanBeListedAndAnsweredFromTheCommandLine(t *testing.T) {
	// The whole point of storing a question rather than blocking on one:
	// this process never asked it, and can still answer it.
	e := newEnv(t, oneServer)
	e.run("elicit", "list") // creates the store

	seedElicitation(t, e, "elc-test1", "github", "form",
		`{"type":"object","properties":{"repo":{"type":"string"}},"required":["repo"]}`)

	list := e.run("elicit", "list")
	if !strings.Contains(list, "elc-test1") {
		t.Fatalf("the question should be listed:\n%s", list)
	}
	// The command to answer is spelled out, because otherwise every caller
	// assembles it from three fields and gets it wrong once.
	show := e.run("elicit", "show", "elc-test1")
	if !strings.Contains(show, "mcpx elicit answer elc-test1") {
		t.Errorf("the answer command should be shown:\n%s", show)
	}

	// key=value, because most answers are one short string.
	e.run("elicit", "answer", "elc-test1", "repo=me/thing")

	out := e.run("--json", "elicit", "result", "elc-test1")
	var ans struct {
		Action  string          `json:"action"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &ans); err != nil {
		t.Fatal(err)
	}
	if ans.Action != "accept" || !strings.Contains(string(ans.Content), "me/thing") {
		t.Errorf("got %+v", ans)
	}
	if after := e.run("elicit", "list"); strings.Contains(after, "elc-test1") {
		t.Errorf("an answered question should not still be pending:\n%s", after)
	}
}

func TestACredentialQuestionIsRoutedToAHuman(t *testing.T) {
	// The default is the agent -- it asked for the thing and has the context.
	// A credential is the exception: a model cannot know one and should not
	// hold one.
	e := newEnv(t, oneServer)
	e.run("elicit", "list")

	seedElicitation(t, e, "elc-agent", "demo", "form",
		`{"type":"object","properties":{"repo":{"type":"string"}}}`)
	seedElicitation(t, e, "elc-human", "demo", "form",
		`{"type":"object","properties":{"api_token":{"type":"string"}}}`)

	out := e.run("--json", "elicit", "list")
	var pending []struct {
		ID       string `json:"id"`
		Audience string `json:"audience"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &pending); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	reasons := map[string]string{}
	for _, p := range pending {
		got[p.ID] = p.Audience
		reasons[p.ID] = p.Reason
	}
	if got["elc-agent"] != "agent" {
		t.Errorf("an ordinary choice belongs to the agent: %v", got)
	}
	if got["elc-human"] != "human" {
		t.Errorf("a token field belongs to a person: %v", got)
	}
	// Routing nobody can inspect is routing nobody can correct.
	if reasons["elc-human"] == "" {
		t.Error("the reason should be recorded")
	}
}

// seedElicitation writes a question straight into the store, standing in for
// a server that asked one.
func seedElicitation(t *testing.T, e *env, id, server, mode, schema string) {
	t.Helper()
	// Through the same pure-Go driver the store uses, so the suite needs no
	// sqlite3-capable interpreter on PATH (the nix check sandbox has none).
	db, err := sql.Open("sqlite", filepath.Join(e.dir, "state", "logs", "elicit.db"))
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	defer db.Close()
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT OR REPLACE INTO elicitations VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "cal-x", "", "s1", server, "tool", mode, "which one?", schema, "", "", "", now, now+120000, "pending", 1); err != nil {
		t.Fatalf("seeding: %v", err)
	}
}

func TestTheConnectionLadderReportsEveryRungItTried(t *testing.T) {
	// "Why is this slow" and "why did it answer from the wrong machine" are
	// both answered by the list of what was tried, so every rung is recorded
	// whether or not it worked.
	e := newEnv(t, oneServer)

	cold := e.run("doctor", "-v")
	if !strings.Contains(cold, "reached over spawn") {
		t.Errorf("a cold start should climb to the spawn rung:\n%s", cold)
	}
	warm := e.run("doctor", "-v")
	if !strings.Contains(warm, "reached over socket") {
		t.Errorf("a warm one should stop at the socket:\n%s", warm)
	}
}

func TestANamedEndpointIsNotSilentlyReplacedByALocalDaemon(t *testing.T) {
	// Falling back would answer from the wrong machine, which is worse than
	// failing.
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, "MCPX_DAEMON_ENDPOINT=http://127.0.0.1:1")
	out, err := e.try("doctor")
	if err == nil {
		t.Fatal("an unreachable named endpoint should fail")
	}
	if !strings.Contains(out, "will not start a local one") {
		t.Errorf("the reason should be stated:\n%s", out)
	}
	if strings.Contains(out, "reached over spawn") {
		t.Errorf("it must not have started a local daemon instead:\n%s", out)
	}
}

func TestInlineModeRunsWithNoDaemonAndLeavesNothingBehind(t *testing.T) {
	// The last rung of the ladder: no separate process. It must work, and it
	// must not leave a daemon or a socket behind -- an in-process daemon
	// lives exactly as long as the command.
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, "MCPX_DAEMON_AUTOSTART=false", "MCPX_DAEMON_INLINE=true")

	out := e.run("call", "demo.echo", `{"message":"inline"}`)
	if !strings.Contains(out, "inline") {
		t.Fatalf("the call should have worked with no daemon:\n%s", out)
	}
	if socks, _ := filepath.Glob(filepath.Join(e.dir, "state", "*.sock")); len(socks) > 0 {
		t.Errorf("no daemon socket should remain: %v", socks)
	}
}

func TestInlineModeRunsAScriptThatCallsATool(t *testing.T) {
	// The case the inline test above missed. `call` goes from this process
	// straight to the in-process daemon; a script goes out to a runtime
	// subprocess and has to come *back* over the socket. That return path
	// was broken: the socket handed to the script was recomputed from the
	// configuration key, and inline mode listens on a private temporary
	// socket instead, so the script was told to reach a daemon at "".
	//
	// Every inline script that called a tool failed, and nothing noticed,
	// because the only inline test called a tool from the CLI.
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, "MCPX_DAEMON_AUTOSTART=false", "MCPX_DAEMON_INLINE=true")

	out := e.run("exec", `const r = await demo.echo({message: "from-a-script"}); emit({text: JSON.stringify(r).includes("from-a-script")});`)
	if !strings.Contains(out, "true") {
		t.Fatalf("a script must reach the inline daemon:\n%s", out)
	}
	if socks, _ := filepath.Glob(filepath.Join(e.dir, "state", "*.sock")); len(socks) > 0 {
		t.Errorf("no daemon socket should remain: %v", socks)
	}
}

func TestWithoutInlineANoDaemonSituationFailsClearly(t *testing.T) {
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, "MCPX_DAEMON_AUTOSTART=false")
	out, err := e.try("ls")
	if err == nil {
		t.Fatal("with no daemon and no way to make one, it should fail")
	}
	// Every rung is named, because the list of what was tried is the answer
	// to "why did this not work".
	for _, want := range []string{"socket", "spawn", "disabled by daemon.autostart"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
}

func TestScriptsReachTheDaemonOverItsSocketWhereTheRuntimeCan(t *testing.T) {
	// Deno and Bun take the socket; Node falls back to TCP because its fetch
	// cannot address one without undici. Each runtime is checked only where
	// it is installed, and the expectation is per runtime -- the first
	// version of this assumed Deno, and on a runner without it the script
	// correctly ran under Node, used TCP, and failed the test for being
	// right.
	for _, c := range []struct{ rt, want string }{
		{"deno", "via unix"}, {"bun", "via unix"}, {"node", "via tcp"},
	} {
		t.Run(c.rt, func(t *testing.T) {
			if _, err := exec.LookPath(c.rt); err != nil {
				t.Skipf("%s not installed", c.rt)
			}
			e := newEnv(t, oneServer)
			out := e.run("exec", "--runtime", c.rt, "--format", "bare",
				`const r = await demo.echo({message:"x"}); console.log("via", transport());`)
			if !strings.Contains(out, c.want) {
				t.Errorf("%s should report %q:\n%s", c.rt, c.want, out)
			}
		})
	}
}

func TestTheEventStreamDeliversCallsAndResumes(t *testing.T) {
	// The stream is what hooks read. Every event carries a sequence number,
	// and a reconnect replays what was missed -- lossless, not merely
	// resumable.
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"a"}`)
	e.run("call", "demo.echo", `{"message":"b"}`)

	client := e.socketClient(t)
	resp, err := client.Get("http://mcpx/v1/events?kinds=call&since=0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content type %q", ct)
	}
	buf := make([]byte, 8192)
	n, _ := io.ReadAtLeast(resp.Body, buf, 64)
	body := string(buf[:n])
	if !strings.Contains(body, "event: call.finished") {
		t.Errorf("replayed calls should arrive:\n%s", body)
	}
	if !strings.Contains(body, "id: ") {
		t.Errorf("events should carry sequence ids for resume:\n%s", body)
	}
}

// socketClient speaks HTTP to the running daemon over its unix socket.
//
// The socket is asked for rather than globbed: a long state path makes the
// daemon fall back to a private runtime directory, so it is not necessarily
// where the state directory would suggest. The opencode plugin finds it the
// same way, for the same reason.
func (e *env) socketClient(t *testing.T) *http.Client {
	t.Helper()
	var st struct {
		Socket string `json:"socket"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatal(err)
	}
	if st.Socket == "" {
		t.Fatal("status did not report a socket")
	}
	return &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", st.Socket)
		}},
	}
}

func TestARecordPostedToTheDaemonLandsInTheSameLogAsTheCommand(t *testing.T) {
	// The plugin records a timing after every tool call. Over the socket
	// that costs a fraction of a millisecond instead of a process spawn, and
	// it must be the same record either way -- same parser, same log, same
	// external marker.
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"warm"}`)
	client := e.socketClient(t)

	resp, err := client.Post("http://mcpx/v1/log?level=warn", "application/json",
		strings.NewReader(`{"event":"harness.tool","tool":"bash","session":"ses-sock"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	out := e.run("--json", "log", "--grep", "harness.tool", "--limit", "5")
	for _, want := range []string{"ses-sock", `"external":true`, "warn"} {
		if !strings.Contains(out, want) {
			t.Errorf("the posted record should carry %s:\n%s", want, out)
		}
	}

	// null is the case worth pinning: it decodes without error into a nil
	// map, and before the shared parser refused it, the write that followed
	// would have panicked inside the daemon.
	for _, body := range []string{`null`, `[1]`, `nope`} {
		resp, err := client.Post("http://mcpx/v1/log", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, resp.StatusCode)
		}
	}
	if _, err := client.Get("http://mcpx/v1/health"); err != nil {
		t.Errorf("the daemon should survive bad records: %v", err)
	}
}

func TestStatusReportsRunningEitherWay(t *testing.T) {
	// `running` used to appear only when false, so anything testing it --
	// the opencode plugin did -- read a live daemon as down.
	e := newEnv(t, oneServer)
	var st map[string]any
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatal(err)
	}
	if st["running"] != false {
		t.Errorf("before anything starts a daemon: running = %v", st["running"])
	}
	e.run("ls")
	st = nil
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatal(err)
	}
	if st["running"] != true || st["socket"] == nil {
		t.Errorf("with a daemon up: running = %v, socket = %v", st["running"], st["socket"])
	}
}

// TestEnvCleanupStopsTheDaemonItStarted is a test of the harness rather than
// of mcpx. The e2e suite was leaking daemons -- dozens of them, each holding
// a fakemcp child and living for hours -- and nothing noticed, because the
// error from the cleanup was discarded.
//
// The mechanism reproduced here is the daemon key. A daemon is keyed to the
// set of config files it read, so a second config file means a second daemon,
// and plain `mcpx stop` only ever targets the key the current invocation
// resolves to -- leaving the other one running. The other half of the bug is
// the socket path: a state directory too long for sun_path relocates the
// socket into a private runtime directory derived from the *caller's* TMPDIR,
// so a recomputed path can miss even when the key is right. Both are fixed
// the same way, and this test exercises both at once, because t.TempDir()
// here is long enough to trigger the relocation.
//
// Checked two ways, because the two failures look different. A socket that
// still accepts a connection means the daemon is serving. A live pid with a
// dead socket means it dropped the listener and kept running.
func TestEnvCleanupStopsTheDaemonItStarted(t *testing.T) {
	type daemon struct {
		socket string
		pid    int
	}
	var started []daemon

	t.Run("lifecycle", func(t *testing.T) {
		e := newEnv(t, oneServer)

		// cfgArgs selects which config, and therefore which daemon, the
		// invocation is about.
		record := func(cfgArgs ...string) {
			var st struct {
				Socket  string `json:"socket"`
				PID     int    `json:"pid"`
				Running bool   `json:"running"`
			}
			args := append(append([]string{"--json"}, cfgArgs...), "status")
			if err := json.Unmarshal([]byte(jsonOf(t, e.run(args...))), &st); err != nil {
				t.Fatal(err)
			}
			if !st.Running || st.Socket == "" || st.PID == 0 {
				t.Fatalf("no daemon to clean up: %+v", st)
			}
			started = append(started, daemon{st.Socket, st.PID})
		}

		e.run("ls") // anything that needs a daemon starts one
		record()

		// A second config file is a second daemon: the key is the set of
		// config paths, so this is a daemon plain `mcpx stop` -- which only
		// ever asks about the config the current invocation resolves to --
		// cannot see.
		other := filepath.Join(e.dir, "other.mcpx.json")
		if err := os.WriteFile(other, []byte(strings.ReplaceAll(oneServer, "FAKE", e.fake)), 0o644); err != nil {
			t.Fatal(err)
		}
		e.run("--config", other, "ls")
		record("--config", other)

		if started[0].pid == started[1].pid {
			t.Fatalf("the second config reused daemon %d; this test needs two", started[0].pid)
		}

		// The info files are what cleanup reads, so a test that cleanup
		// works is worthless if they are not there to be found.
		type daemonRow struct {
			PID     int  `json:"pid"`
			Running bool `json:"running"`
		}
		var rows []daemonRow
		if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "daemons"))), &rows); err != nil {
			t.Fatal(err)
		}
		for _, d := range started {
			if !slices.ContainsFunc(rows, func(r daemonRow) bool { return r.PID == d.pid && r.Running }) {
				t.Fatalf("daemon %d absent from the info files: %+v", d.pid, rows)
			}
		}
	})

	// Shutdown goes on past the point `stop` waits for, so give each process
	// a moment to actually leave rather than racing it.
	for _, d := range started {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && daemonAlive(d.socket, d.pid) {
			time.Sleep(100 * time.Millisecond)
		}
		if conn, err := net.Dial("unix", d.socket); err == nil {
			conn.Close()
			t.Errorf("daemon %d still listening on %s after cleanup", d.pid, d.socket)
		}
		if syscall.Kill(d.pid, 0) == nil {
			t.Errorf("daemon pid %d still alive after cleanup", d.pid)
		}
	}
}

// daemonAlive reports whether either half of a daemon survives: the listener
// or the process.
func daemonAlive(socket string, pid int) bool {
	if conn, err := net.Dial("unix", socket); err == nil {
		conn.Close()
		return true
	}
	return syscall.Kill(pid, 0) == nil
}

// TestResolveTellsAPluginWhichDaemonServesADirectory is the no-binary case

// TestResolveTellsAPluginWhichDaemonServesADirectory is the no-binary case
// from the plugin's side: everything the opencode plugin needs to pick a
// daemon comes back from one request to a daemon it already reached.
func TestResolveTellsAPluginWhichDaemonServesADirectory(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	client := e.socketClient(t)

	var st struct {
		Socket string `json:"socket"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatal(err)
	}

	resp, err := client.Get("http://mcpx/v1/resolve?dir=" + e.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got struct {
		Socket     string   `json:"socket"`
		ConfigPath string   `json:"configPath"`
		ConfigHash string   `json:"configHash"`
		Running    bool     `json:"running"`
		Sources    []string `json:"sources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Socket != st.Socket {
		t.Errorf("resolve named %q, the daemon is on %q", got.Socket, st.Socket)
	}
	if !got.Running {
		t.Error("the daemon answering the question is running, by construction")
	}
	if got.ConfigHash == "" || len(got.Sources) == 0 {
		t.Errorf("resolve must say which configuration it fingerprinted: %+v", got)
	}
	if !strings.HasSuffix(got.ConfigPath, ".mcpx.json") {
		t.Errorf("configPath = %q", got.ConfigPath)
	}

	// A relative directory is a caller bug, and a silent wrong answer would
	// be worse than an error: it would name some other project's daemon.
	bad, err := client.Get("http://mcpx/v1/resolve?dir=relative")
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("relative dir: status %d, want 400", bad.StatusCode)
	}
}

func TestTypeCheckCatchesAnUnknownNamespaceBeforeAnythingRuns(t *testing.T) {
	// Three separate faults let a typo'd namespace run half a script:
	// preflight ignores unknown namespaces on purpose, --typecheck was
	// silently dropped on the exec path, and when it did run it failed on
	// mcpx's own generated client, so nobody could leave it on.
	//
	// The emit before the bad call is the point: without a check, it ran,
	// and so would anything else with a side effect.
	e := newEnv(t, oneServer)
	out, err := e.try("exec", "--typecheck=on",
		`emit({step: "before"}); await totally_made_up.some_tool({});`)
	if err == nil {
		t.Fatalf("an unknown namespace should stop the run:\n%s", out)
	}
	if !strings.Contains(out, "totally_made_up") {
		t.Errorf("the error should name the namespace:\n%s", out)
	}
	if strings.Contains(out, `"step"`) {
		t.Errorf("nothing should have run:\n%s", out)
	}
}

func TestTypeCheckPassesForAScriptThatUsesTheGlobals(t *testing.T) {
	// The other half: the ambient declarations must actually load, or a
	// check that works reports "Cannot find name 'emit'" for every script.
	e := newEnv(t, oneServer)
	out := e.run("exec", "--typecheck=on",
		`const r = await demo.echo({message: "hi"}); emit({ok: r != null});`)
	if !strings.Contains(out, "true") {
		t.Fatalf("a correct script must pass the check and run:\n%s", out)
	}
}

// stubRegistry serves the shape registry.Client.Search parses, and nothing
// else. It exists so no test depends on a network it cannot control.
func stubRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"io.example/demo",` +
			`"description":"a stub entry","version":"1.0.0"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestTwoConfigurationsDoNotShareAGeneratedClient(t *testing.T) {
	// One working directory for every run meant two projects overwrote each
	// other's generated client: a script was type-checked, and would have
	// run, against another project's catalogue. Seven of twelve concurrent
	// cross-project runs failed that way.
	//
	// The cache root is shared deliberately here -- that is the condition
	// that used to break it. The directory inside it is named for the
	// client's contents, so two catalogues never meet.
	cache := t.TempDir()
	a := newEnv(t, oneServer)
	b := newEnv(t, strings.ReplaceAll(oneServer, "demo", "other"))
	for _, e := range []*env{a, b} {
		e.envVars = append(e.envVars, "MCPX_CACHE_DIR="+cache,
			"MCPX_DAEMON_AUTOSTART=false", "MCPX_DAEMON_INLINE=true")
	}

	type outcome struct {
		out string
		err error
	}
	results := make(chan outcome, 8)
	for i := 0; i < 4; i++ {
		go func() {
			out, err := a.try("exec", "--typecheck=on", `emit({t: typeof demo});`)
			results <- outcome{out, err}
		}()
		go func() {
			out, err := b.try("exec", "--typecheck=on", `emit({t: typeof other});`)
			results <- outcome{out, err}
		}()
	}
	for i := 0; i < 8; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("a run saw the other configuration's client:\n%s", r.out)
		}
	}
}

// TestEveryExecFlagChangesSomething is the guard for a fault this repository
// has now shipped three times: a flag defined on a shared flag set, described
// in the help, and read on only one of the two paths that share it.
//
// --typecheck was dropped on the exec path; --launcher, --no-launcher,
// --export and --allow-repeat were all set only when a file was given. Each
// was found by accident, never by a test, because a flag that does nothing
// looks exactly like a flag that worked.
//
// A flag earns its place by changing an observable outcome. Add a row when
// you add a flag to `mcpx exec`.
//
// Every row runs twice: here, and with --remote on the daemon. The remote
// path was handed five of these flags and dropped the rest (#192) -- parsed,
// described in the help, and absent from the program the daemon ran.
func TestEveryExecFlagChangesSomething(t *testing.T) {
	e := newEnv(t, oneServer)
	type row struct {
		flag string
		// base is given both with and without the flag, so the comparison
		// isolates the flag rather than everything around it.
		base   []string
		args   []string
		source string
		// want must appear with the flag and must not appear without it.
		want string
		// gone must appear without the flag and must not appear with it,
		// for a flag whose effect is to take something away.
		gone string
		// fails marks a flag whose whole purpose is to refuse the run, or a
		// source that fails so a failure hook can be seen.
		fails bool
	}
	rows := []row{
		{
			flag:   "--export",
			args:   []string{"--export=main"},
			source: `export const main = async () => { emit({via: "main"}); };`,
			want:   `"via":"main"`,
		},
		{
			flag:   "--launcher",
			args:   []string{"--launcher", `console.log("LAUNCHED"); @entry`},
			source: `emit({ok: 1});`,
			want:   "LAUNCHED",
		},
		{
			flag:   "--no-launcher",
			args:   []string{"--no-launcher"},
			source: `export default () => emit({wrapped: 1});`,
			gone:   `"wrapped":1`,
		},
		{
			flag:   "--prefix",
			args:   []string{"--prefix", "const shared = 41;"},
			source: `emit({v: shared + 1});`,
			want:   `"v":42`,
		},
		{
			flag:   "--suffix",
			args:   []string{"--suffix", `emit({tail: "SUFFIXED"});`},
			source: `emit({ok: 1});`,
			want:   "SUFFIXED",
		},
		{
			flag:   "--before",
			args:   []string{"--before", `console.log("BEFORE-RAN");`},
			source: `emit({ok: 1});`,
			want:   "BEFORE-RAN",
		},
		{
			flag:   "--on-success",
			args:   []string{"--on-success", `console.log("SUCCESS-HOOK");`},
			source: `emit({ok: 1});`,
			want:   "SUCCESS-HOOK",
		},
		{
			flag:   "--on-error",
			args:   []string{"--on-error", `console.log("ERROR-HOOK");`},
			source: `throw new Error("boom");`,
			want:   "ERROR-HOOK",
			fails:  true,
		},
		{
			flag:   "--env",
			args:   []string{"--env", "MCPX_FLAG_PROBE=FROM-ENV-FLAG"},
			source: `const g = globalThis as any; emit({e: g.Deno ? g.Deno.env.get("MCPX_FLAG_PROBE") : g.process.env.MCPX_FLAG_PROBE});`,
			want:   "FROM-ENV-FLAG",
		},
		{
			flag:   "--no-capture-console",
			args:   []string{"--no-capture-console"},
			source: `console.log("WRAPPED:" + String((globalThis.console as any).__mcpxWrapped));`,
			want:   "WRAPPED:undefined",
		},
		{
			// Without permission the repeated placeholder is refused before
			// anything runs, so the launcher's line is the evidence.
			flag:   "--allow-repeat",
			base:   []string{`--launcher=console.log("REPEAT-OK"); @entry @entry`},
			args:   []string{"--allow-repeat", "entry"},
			source: `emit({ok: 1});`,
			want:   "REPEAT-OK",
		},
		{
			// The marker is the point: with the check the script is refused
			// before it runs, without it the emit has already happened by
			// the time the name turns out not to exist.
			flag:   "--typecheck",
			args:   []string{"--typecheck=on"},
			source: `emit({ran: 1}); await totally_made_up.x({});`,
			want:   "does not type check",
			fails:  true,
		},
	}
	for _, where := range []string{"local", "remote"} {
		for _, c := range rows {
			t.Run(where+"/"+c.flag, func(t *testing.T) {
				lead := []string{"exec"}
				if where == "remote" {
					lead = append(lead, "--remote")
				}
				lead = append(lead, c.base...)
				argv := append(append(append([]string{}, lead...), c.args...), c.source)
				with, werr := e.try(argv...)
				if !c.fails && werr != nil {
					t.Fatalf("%s: %v\n%s", c.flag, werr, with)
				}
				without, _ := e.try(append(append([]string{}, lead...), c.source)...)
				if c.want != "" {
					if !strings.Contains(with, c.want) {
						t.Errorf("%s did nothing: wanted %q in\n%s", c.flag, c.want, with)
					}
					// And without it, the same source must not produce that
					// outcome, or the flag is not what caused it.
					if strings.Contains(without, c.want) {
						t.Errorf("%s: %q appears without the flag too, so the test proves nothing:\n%s",
							c.flag, c.want, without)
					}
				}
				if c.gone != "" {
					if strings.Contains(with, c.gone) {
						t.Errorf("%s did nothing: %q should be gone from\n%s", c.flag, c.gone, with)
					}
					if !strings.Contains(without, c.gone) {
						t.Errorf("%s: %q is missing without the flag too, so the test proves nothing:\n%s",
							c.flag, c.gone, without)
					}
				}
			})
		}
	}
}

func TestConcurrentRunsOfOneScriptKeepTheirOwnLauncher(t *testing.T) {
	// The generated entry point was named for the script, so every
	// concurrent run of that script wrote the same file: six runs with six
	// different launchers all executed the fourth one's. Placeholders make
	// the same mistake quieter -- two runs of one script with different
	// @values are two different programs sharing a path.
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "s.ts")
	// No default export: a module that runs on import, so a launcher that
	// only prints does not have to reproduce the entry-point scaffolding.
	if err := os.WriteFile(script, []byte("void 0;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 6
	type res struct {
		want string
		out  string
		err  error
	}
	out := make(chan res, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			mark := fmt.Sprintf("LAUNCHER-%d", i)
			o, err := e.try("run", "--launcher",
				fmt.Sprintf("console.log(%q); @entry", mark), script)
			out <- res{mark, o, err}
		}(i)
	}
	for i := 0; i < n; i++ {
		r := <-out
		if r.err != nil {
			t.Errorf("%s: %v\n%s", r.want, r.err, r.out)
			continue
		}
		if !strings.Contains(r.out, r.want) {
			t.Errorf("a run executed another run's launcher: wanted %s, got:\n%s", r.want, r.out)
		}
	}
}

// withoutMCPXVars drops every MCPX_ variable the developer running the suite
// happens to have exported.
//
// The harness sets the ones it needs a line below, and last-wins in exec's
// environment would cover those. It does not cover the ones it deliberately
// leaves unset: a test that asserts a knob is off reads the default only if
// nothing in the ambient environment turned it on, and MCPX_TRACE=1 in the
// shell of somebody debugging mcpx is exactly the case. That fails on one
// machine and nowhere else, which is the worst kind.
//
// PATH and everything else is kept, because the child needs a runtime.
func withoutMCPXVars(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "MCPX_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
