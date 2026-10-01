package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A call a server refuses because its schema moved is explained where every
// surface meets it: the daemon's call boundary. These drive the real binary
// against a fake server that is upgraded underneath a running daemon, which
// is the only way catalog history comes to hold a change.

// schemaServer points the fake at a file naming its schema version. The
// variable is FAKEMCP_SCHEMA_FILE, spelled with a JSON escape because newEnv
// replaces every "FAKE" in a config with the binary's path, key names
// included.
const schemaServer = `{
  "mcpServers": {
    "demo": {
      "command": "FAKE",
      "env": { "\u0046AKEMCP_SCHEMA_FILE": "VERSIONFILE" },
      "mcpx": { "sharing": "shared", "scope": "global" }
    }
  }
}`

// callDiagnostic is the part of a diagnostic these tests read.
type callDiagnostic struct {
	Kind    string `json:"kind"`
	Tool    string `json:"tool"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Changed *struct {
		When time.Time `json:"when"`
		What string    `json:"what"`
	} `json:"changed"`
	Fix   string `json:"fix"`
	Fatal bool   `json:"fatal"`
}

type callErrorDoc struct {
	Error       string           `json:"error"`
	Diagnostics []callDiagnostic `json:"diagnostics"`
}

// hasRepoChange is the finding every surface should carry: the tool, the
// argument it now requires, and the recorded change that made it required.
func hasRepoChange(ds []callDiagnostic) bool {
	for _, d := range ds {
		if d.Tool == "demo.create_issue" && d.Field == "repo" && d.Changed != nil &&
			d.Changed.What != "" && !d.Changed.When.IsZero() {
			return true
		}
	}
	return false
}

// upgradedServer is a daemon that has seen demo.create_issue take only a
// title, and then seen it require a repo as well.
func upgradedServer(t *testing.T) *env {
	t.Helper()
	schema := filepath.Join(t.TempDir(), "schema")
	mustWrite(t, schema, "v1")
	e := newEnv(t, strings.ReplaceAll(schemaServer, "VERSIONFILE", schema))
	e.run("refresh")
	mustWrite(t, schema, "v2")
	e.run("refresh")

	// Precondition: history holds the change. Without it every assertion
	// below would be about a diagnostic with nothing to say about "when".
	resp, err := e.socketClient(t).Get("http://mcpx/v1/catalog/history?tool=demo.create_issue")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "repo") {
		t.Fatalf("catalog history should record repo becoming required:\n%s", b)
	}
	return e
}

// split runs mcpx with stdout and stderr apart, for assertions about which
// stream something belongs on.
func (e *env) split(args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.mcpx, args...)
	cmd.Dir = e.dir
	cmd.Env = e.envVars
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), harnessTimeout(ctx, err)
}

func postCall(t *testing.T, c *http.Client, path, body string) (int, []byte) {
	t.Helper()
	resp, err := c.Post("http://mcpx"+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestARefusedCallCarriesTheSchemaChangeOverV1(t *testing.T) {
	e := upgradedServer(t)
	c := e.socketClient(t)

	status, b := postCall(t, c, "/v1/call",
		`{"server":"demo","tool":"create_issue","args":{"title":"x"}}`)
	var doc callErrorDoc
	if err := json.Unmarshal(b, &doc); err != nil || status != http.StatusBadGateway {
		t.Fatalf("status %d: %v\n%s", status, err, b)
	}
	if !hasRepoChange(doc.Diagnostics) {
		t.Errorf("/v1/call should name the tool, the field and what changed:\n%s", b)
	}
	// Added to, never replaced.
	if !strings.HasPrefix(doc.Error, "mcp error -32602: Invalid params\n") ||
		!strings.Contains(doc.Error, "repo is required") {
		t.Errorf("the error text should be the server's, then the rendered diagnostic:\n%s", doc.Error)
	}

	// The same call collected later, as a task.
	status, b = postCall(t, c, "/v1/call",
		`{"server":"demo","tool":"create_issue","args":{"title":"x"},"task":{"ttl":60000}}`)
	var started struct {
		Task struct {
			TaskID string `json:"taskId"`
		} `json:"task"`
	}
	if err := json.Unmarshal(b, &started); err != nil || started.Task.TaskID == "" {
		t.Fatalf("status %d: %v\n%s", status, err, b)
	}
	resp, err := c.Get("http://mcpx/v1/tasks/" + started.Task.TaskID + "/result?waitMs=2000")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var taskDoc callErrorDoc
	if err := json.Unmarshal(b, &taskDoc); err != nil || !hasRepoChange(taskDoc.Diagnostics) {
		t.Errorf("a task's failure should carry the same diagnostics (%v):\n%s", err, b)
	}

	// The path-per-tool route writes the error text itself, so it carries
	// the rendered form. The structured field waits on routes_proto.go.
	status, b = postCall(t, c, "/v1/call/demo/create_issue", `{"title":"x"}`)
	if status != http.StatusBadGateway || !strings.Contains(string(b), "repo is required") {
		t.Errorf("/v1/call/{server}/{tool} should carry the rendered diagnostic (status %d):\n%s", status, b)
	}
}

func TestMcpxCallPrintsTheDiagnostic(t *testing.T) {
	e := upgradedServer(t)
	_, stderr, err := e.split("call", "demo.create_issue", "{}")
	if err == nil {
		t.Fatalf("a refused call should fail:\n%s", stderr)
	}
	for _, want := range []string{
		"Invalid params", // the server's own words, kept
		"demo.create_issue: the call was rejected and repo is required", // the field
		"schema changed", "`repo` was added and is required", // and when
		`minimum:    await demo.create_issue({ repo: "", ... })`,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr is missing %q:\n%s", want, stderr)
		}
	}
}

func TestMcpxCallJSONCarriesTheDiagnostics(t *testing.T) {
	e := upgradedServer(t)
	stdout, stderr, err := e.split("--json", "call", "demo.create_issue", "{}")
	if err == nil {
		t.Fatalf("a refused call should still exit non-zero:\n%s", stdout)
	}
	var doc callErrorDoc
	if jerr := json.Unmarshal([]byte(stdout), &doc); jerr != nil {
		t.Fatalf("--json should put the error document on stdout: %v\nstdout:\n%s\nstderr:\n%s",
			jerr, stdout, stderr)
	}
	if !hasRepoChange(doc.Diagnostics) {
		t.Errorf("--json should carry the diagnostics as data:\n%s", stdout)
	}
}

func TestAFailureThatIsNotAboutTheSchemaHasNoDiagnostics(t *testing.T) {
	e := upgradedServer(t)
	c := e.socketClient(t)

	// A protocol error with nothing to do with the arguments.
	status, b := postCall(t, c, "/v1/call", `{"server":"demo","tool":"outage","args":{}}`)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil || status != http.StatusBadGateway {
		t.Fatalf("status %d: %v\n%s", status, err, b)
	}
	if _, has := doc["diagnostics"]; has {
		t.Errorf("an outage is not a schema problem:\n%s", b)
	}
	if doc["error"] != "mcp error -32603: internal error: the backend is unavailable" {
		t.Errorf("the server's error should come through untouched:\n%s", b)
	}

	// A tool that reports its own failure succeeds as a call.
	status, b = postCall(t, c, "/v1/call", `{"server":"demo","tool":"boom","args":{}}`)
	if status != http.StatusOK || strings.Contains(string(b), "diagnostics") {
		t.Errorf("a tool's own error is a result, not something to diagnose (status %d):\n%s", status, b)
	}

	stdout, stderr, err := e.split("--json", "call", "demo.outage", "{}")
	if err == nil {
		t.Fatalf("an outage should fail:\n%s", stdout)
	}
	if strings.Contains(stdout, "diagnostics") || strings.Contains(stderr, "schema changed") {
		t.Errorf("no diagnostic expected:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestAScriptSeesTheDiagnosticsOnToolError(t *testing.T) {
	e := upgradedServer(t)
	// Preflight would stop this script before it ran, which is its job; off
	// here so the failure reaches the call, as a dynamic call's would.
	out := e.run("exec", "--diagnose-preflight=false", `
try {
  await demo.create_issue({ title: "x" });
  console.log("no error");
} catch (err) {
  console.log(JSON.stringify({ name: err.name, diagnostics: err.diagnostics }));
}`)
	var doc struct {
		Name        string           `json:"name"`
		Diagnostics []callDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if doc.Name != "ToolError" || !hasRepoChange(doc.Diagnostics) {
		t.Errorf("ToolError should expose .diagnostics:\n%s", out)
	}
}
