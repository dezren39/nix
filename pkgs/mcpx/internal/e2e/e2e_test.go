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
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
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
	if jerr := json.Unmarshal([]byte(out), &doc); jerr != nil {
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
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
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
	var doc map[string]any
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[0])
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
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
	var doc map[string]any
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[0])
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
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

func TestFilteredLogCallsAreCheap(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "cost.ts")
	os.WriteFile(script, []byte(`import { log } from "./mcpx-client.ts";
export default function main() {
  const N = 50_000;
  const t = performance.now();
  for (let i = 0; i < N; i++) log.debug("filtered {i}", { i });
  return { perCallUs: (performance.now() - t) * 1000 / N, enabled: log.enabled("debug") };
}
`), 0o644)
	out := e.run("run", "--log-level", "info", "--format", "bare", script)
	start := strings.Index(out, "{")
	var doc struct {
		PerCallUs float64 `json:"perCallUs"`
		Enabled   bool    `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(out[start:]), &doc); err != nil {
		t.Fatalf("bad result: %v\n%s", err, out)
	}
	if doc.Enabled {
		t.Error("debug should report disabled at an info threshold")
	}
	// Serialising and writing would be ~0.1us; a stack trace ~5us. A
	// short-circuited call should be far below either.
	if doc.PerCallUs > 0.5 {
		t.Errorf("a filtered call cost %.3fus; the level check is not short-circuiting", doc.PerCallUs)
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

func TestPrefixIsRejectedForFileScripts(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "p.ts")
	os.WriteFile(script, []byte(`console.log("hi");`), 0o644)
	out, err := e.try("run", "--prefix", "const X = 1;", script)
	if err == nil {
		t.Fatalf("expected rejection:\n%s", out)
	}
	if !strings.Contains(out, "exec") {
		t.Errorf("the error should say where prefixes do apply:\n%s", out)
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
