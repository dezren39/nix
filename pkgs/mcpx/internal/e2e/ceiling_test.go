package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// autonomy.max, the daemon's ceiling on the autonomy dial
// (docs/decisions/0002-autonomy-dial.md). Before it, any caller that could
// reach the socket sent "autonomy": "run" and the daemon ran a script it had
// not been sent. Each test here is a caller trying to get round the ceiling
// from one surface; each must find the request lowered, told so, and the
// side effect absent.

// markRecipe leaves a file behind when it runs, so "did it run" is answered
// by the filesystem rather than by the response that is under test.
const markRecipe = `// Leave a mark on the filesystem.
// @param path:string   where
(await import("node:fs")).writeFileSync(@path, "ran");
console.log("marked");
`

// ceilingEnv starts a daemon whose owner set autonomy.max to max, then
// drops the variable, so every later command is a caller that has no
// ceiling of its own and is limited only by the daemon's.
func ceilingEnv(t *testing.T, max string) (*env, string) {
	t.Helper()
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "mark", markRecipe)
	e.setenv("MCPX_AUTONOMY_MAX=" + max)
	e.run("ls")
	e.unsetenv("MCPX_AUTONOMY_MAX")
	return e, filepath.Join(e.dir, "mark.out")
}

func (e *env) unsetenv(name string) {
	var kept []string
	for _, kv := range e.envVars {
		if !strings.HasPrefix(kv, name+"=") {
			kept = append(kept, kv)
		}
	}
	e.envVars = kept
}

func assertNoMark(t *testing.T, mark, surface string) {
	t.Helper()
	if _, err := os.Stat(mark); err == nil {
		t.Errorf("%s: the recipe ran past autonomy.max propose -- %s exists", surface, mark)
		_ = os.Remove(mark)
	}
}

func assertClamped(t *testing.T, surface string, doc map[string]any) {
	t.Helper()
	by, _ := doc["clampedBy"].(string)
	if doc["autonomy"] != "propose" || doc["requested"] != "run" ||
		!strings.HasPrefix(by, "autonomy.max (env:MCPX_AUTONOMY_MAX)") {
		t.Errorf("%s: want autonomy propose, requested run, clampedBy autonomy.max: %v", surface, doc)
	}
	if doc["result"] != nil {
		t.Errorf("%s: a lowered request carried a result: %v", surface, doc["result"])
	}
}

func TestV1CannotRaiseAutonomyAboveTheCeiling(t *testing.T) {
	e, mark := ceilingEnv(t, "propose")
	c := e.socketClient(t)
	place := fmt.Sprintf(`{"path":%q}`, mark)

	for _, tc := range []struct{ name, path, body, header string }{
		{"intent body run", "/v1/intent",
			`{"prompt":"leave a mark on the filesystem","autonomy":"run","placeholders":` + place + `}`, ""},
		{"recipe run, empty autonomy", "/v1/recipes/mark/run", `{"placeholders":` + place + `}`, ""},
		{"recipe run body run", "/v1/recipes/mark/run", `{"autonomy":"run","placeholders":` + place + `}`, ""},
		// The call-settings header: prompt.autonomy is call-scoped, so a
		// caller may send it, and autonomy.max is daemon-scoped, so a
		// caller's copy of it is dropped.
		{"intent header", "/v1/intent",
			`{"prompt":"leave a mark on the filesystem","placeholders":` + place + `}`,
			`{"prompt.autonomy":"run","autonomy.max":"run"}`},
	} {
		req, _ := http.NewRequest(http.MethodPost, "http://mcpx"+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		if tc.header != "" {
			req.Header.Set("X-Mcpx-Settings", tc.header)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var doc map[string]any
		_ = json.Unmarshal(b, &doc)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: lowered, not refused -- want 200, got %d: %s", tc.name, resp.StatusCode, b)
		}
		assertClamped(t, tc.name, doc)
		if src, _ := doc["source"].(string); !strings.Contains(src, "writeFileSync") {
			t.Errorf("%s: propose should still hand back the script: %v", tc.name, doc)
		}
		assertNoMark(t, mark, tc.name)
	}

	// The settings view says the same, with the layer that decided.
	resp, err := c.Get("http://mcpx/v1/settings/autonomy.max")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"value":"propose"`) {
		t.Errorf("GET autonomy.max should report propose: %s", b)
	}
}

func TestTheCeilingCannotBeRaisedAtRuntime(t *testing.T) {
	e, mark := ceilingEnv(t, "propose")
	c := e.socketClient(t)
	req, _ := http.NewRequest(http.MethodPut, "http://mcpx/v1/settings/autonomy.max",
		strings.NewReader(`{"value":"run"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var put map[string]any
	_ = json.Unmarshal(b, &put)
	if put["applied"] != false || put["restartRequired"] != true {
		t.Errorf("PUT autonomy.max should answer applied false, restartRequired true: %s", b)
	}
	code, doc := postJSON(t, c, "/v1/recipes/mark/run",
		fmt.Sprintf(`{"autonomy":"run","placeholders":{"path":%q}}`, mark))
	if code != http.StatusOK {
		t.Fatalf("recipe run answered %d: %v", code, doc)
	}
	assertClamped(t, "after PUT", doc)
	assertNoMark(t, mark, "after PUT")
}

func TestTheCLICannotRaiseAutonomyAboveTheCeiling(t *testing.T) {
	e, mark := ceilingEnv(t, "propose")
	for _, args := range [][]string{
		{"--json", "prompt", "--run", "--set", "path=" + mark, "leave a mark on the filesystem"},
		{"--json", "prompt", "--prompt-autonomy=run", "--set", "path=" + mark, "leave a mark on the filesystem"},
		{"--json", "recipes", "run", "mark", "path=" + mark},
		{"--json", "recipes", "run", "mark", "--autonomy", "run", "path=" + mark},
	} {
		name := strings.Join(args[1:3], " ")
		out, errOut, err := e.split(args...)
		if err != nil {
			t.Errorf("%s: %v\n%s\n%s", name, err, out, errOut)
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
			continue
		}
		assertClamped(t, name, doc)
		if !strings.Contains(errOut, "requested run, clamped to propose by autonomy.max") {
			t.Errorf("%s: the CLI should say on stderr that it was lowered:\n%s", name, errOut)
		}
		assertNoMark(t, mark, name)
	}
}

func TestMCPCannotRaiseAutonomyAboveTheCeiling(t *testing.T) {
	e, mark := ceilingEnv(t, "propose")
	var in strings.Builder
	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcpx_intent","arguments":{"prompt":"leave a mark on the filesystem","autonomy":"run","placeholders":{"path":%q}}}}`+"\n", mark)
	fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcpx_recipe_run","arguments":{"name":"mark","autonomy":"run","placeholders":{"path":%q}}}}`+"\n", mark)
	out := e.runStdin(in.String(), "serve")
	if n := strings.Count(out, `clampedBy\": \"autonomy.max (env:MCPX_AUTONOMY_MAX)`); n != 2 {
		t.Errorf("both MCP tools should report the clamp, %d did:\n%s", n, out)
	}
	assertNoMark(t, mark, "MCP")
}

func TestThePluginCannotRaiseAutonomyAboveTheCeiling(t *testing.T) {
	bun := lookBun(t)
	e, mark := ceilingEnv(t, "propose")
	var where struct {
		Socket string `json:"socket"`
	}
	if err := json.Unmarshal([]byte(e.run("--json", "resolve")), &where); err != nil || where.Socket == "" {
		t.Fatalf("could not find the daemon's socket: %v", err)
	}
	out := runBun(t, bun, e, "plugin/opencode/mcpx/ceiling.test.ts",
		"CEILING_SOCKET="+where.Socket, "CEILING_MARK="+mark)
	if !strings.Contains(out, " 0 fail") || !strings.Contains(out, " 1 pass") {
		t.Fatalf("the plugin's ceiling test did not run clean:\n%s", out)
	}
	assertNoMark(t, mark, "plugin")
}

func TestHooksFromConfigDoNotRunAboveTheCeiling(t *testing.T) {
	e := newEnv(t, oneServer)
	cfg := filepath.Join(e.dir, ".mcpx.json")
	b, _ := os.ReadFile(cfg)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	doc["script"] = map[string]any{"onSuccess": []any{`console.log("configured-hook")`}}
	nb, _ := json.Marshal(doc)
	mustWrite(t, cfg, string(nb))

	// Precondition: at the default ceiling the configured hook runs.
	if out := e.run("exec", `console.log("body")`); !strings.Contains(out, "configured-hook") {
		t.Fatalf("the configured hook should run at the default ceiling:\n%s", out)
	}

	e.setenv("MCPX_AUTONOMY_MAX=propose", "MCPX_HOOKS_AUTONOMY=run")
	out, errOut, err := e.split("exec", `console.log("body")`)
	if err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, errOut)
	}
	if strings.Contains(out, "configured-hook") || !strings.Contains(out, "body") {
		t.Errorf("a configured hook ran with autonomy.max propose, or the body did not:\n%s", out)
	}
	if !strings.Contains(errOut, "hooks.autonomy requested run, clamped to propose by autonomy.max") ||
		!strings.Contains(errOut, "onSuccess") {
		t.Errorf("stderr should say which hook was skipped and why:\n%s", errOut)
	}

	// A hook given on the command line is the caller's own code, which the
	// dial never governs.
	if out := e.run("exec", "--on-success", `console.log("flagged-hook")`, `console.log("body")`); !strings.Contains(out, "flagged-hook") {
		t.Errorf("the command's own --on-success should run under the ceiling:\n%s", out)
	}
}

func TestRepairCannotRaiseAutonomyAboveTheCeiling(t *testing.T) {
	schema := filepath.Join(t.TempDir(), "schema")
	mustWrite(t, schema, "v1")
	e := newEnv(t, strings.ReplaceAll(schemaServer, "VERSIONFILE", schema))
	e.setenv("MCPX_AUTONOMY_MAX=off")
	e.run("refresh")
	e.unsetenv("MCPX_AUTONOMY_MAX")
	mustWrite(t, schema, "v2")
	e.run("refresh")
	c := e.socketClient(t)

	req, _ := http.NewRequest(http.MethodPost, "http://mcpx/v1/call",
		strings.NewReader(`{"server":"demo","tool":"create_issue","args":{"title":"x"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mcpx-Settings", `{"repair.autonomy":"run"}`)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var got callErrorDoc
	if err := json.Unmarshal(b, &got); err != nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d: %v\n%s", resp.StatusCode, err, b)
	}
	if len(got.Diagnostics) != 0 {
		t.Errorf("repair.autonomy run past autonomy.max off still attached diagnostics:\n%s", b)
	}
	if !strings.HasPrefix(got.Error, "mcp error -32602") {
		t.Errorf("the server's own error should still come back:\n%s", b)
	}
}

func lookBun(t *testing.T) string {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun is not installed")
	}
	return bun
}

func runBun(t *testing.T, bun string, e *env, file string, extra ...string) string {
	t.Helper()
	cmd := exec.Command(bun, "test", file)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(append([]string{}, e.envVars...), extra...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun test %s: %v\n%s", file, err, out)
	}
	return string(out)
}
