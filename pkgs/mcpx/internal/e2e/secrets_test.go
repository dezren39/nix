package e2e_test

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/codegen"
)

// secretServer is the fake server, requiring one secret. Its echo tool answers
// with what the child process sees in that variable, which is how the test
// learns a value was delivered to the child and not only accepted by the
// daemon.
//
// The report variable is named without FAKE, because newEnv rewrites every
// FAKE in the configuration to the binary's path.
const secretServer = `{"mcpServers": {"demo": {
	"command": "FAKE",
	"env": {"MCPX_REPORT_ENV": "MCPX_E2E_TOKEN"},
	"mcpx": {"requiresSecrets": ["MCPX_E2E_TOKEN"]}
}}}`

// stdinTry is runStdin for the cases that must fail: it reports the failure
// instead of stopping the test.
func stdinTry(e *env, stdin string, args ...string) (string, error) {
	cmd := exec.Command(e.mcpx, args...)
	cmd.Dir = e.dir
	cmd.Env = e.envVars
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// A value nothing else in the suite writes, so finding it anywhere means it
// was written there by this code path.
const e2eValue = "tok-e2e-7c1d-not-on-disk"

func TestAServerWaitsForItsSecretAndRunsOnceItHasIt(t *testing.T) {
	e := newEnv(t, secretServer)

	// Waiting: the call is refused, and the refusal names the secret and the
	// request to answer.
	out, err := e.try("call", "demo.echo", `{"message":"x"}`)
	if err == nil {
		t.Fatalf("a call to a server waiting for its secret must not run:\n%s", out)
	}
	for _, want := range []string{"MCPX_E2E_TOKEN", "mcpx secrets set", "request sr-"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal should say %q:\n%s", want, out)
		}
	}
	if reqs := e.run("secrets", "requests"); !strings.Contains(reqs, "pending") || !strings.Contains(reqs, "MCPX_E2E_TOKEN") {
		t.Errorf("the request is listed as pending:\n%s", reqs)
	}
	if srv := e.run("servers", "show", "demo"); !strings.Contains(srv, `"active": false`) || !strings.Contains(srv, "MCPX_E2E_TOKEN") {
		t.Errorf("servers show says what the server is waiting for:\n%s", srv)
	}

	// Given a value on standard input, the server can run at once, with no
	// restart of the daemon: the next call starts the child with the value.
	if out := e.runStdin(e2eValue+"\n", "secrets", "set", "MCPX_E2E_TOKEN"); !strings.Contains(out, "is set in the running daemon") {
		t.Fatalf("set should say the value is held in the running daemon:\n%s", out)
	}
	got := e.run("call", "demo.echo", `{"message":"x"}`)
	if !strings.Contains(got, e2eValue) {
		t.Fatalf("the child should see the value it was given, and the trailing newline is not part of it:\n%s", got)
	}

	// Unset: the server waits again, and a new call is refused.
	if out := e.run("secrets", "unset", "MCPX_E2E_TOKEN"); !strings.Contains(out, "no longer set") {
		t.Errorf("unset should say the name is no longer set:\n%s", out)
	}
	if out, err := e.try("call", "demo.echo", `{"message":"x"}`); err == nil {
		t.Fatalf("after unset the call is refused again:\n%s", out)
	}
}

// quietSecretServer is secretServer without the report: its echo answers with
// its message, as any server's answer would. A server that returns a secret in
// its own answer has put it on disk itself, and that is not what this test is
// about; the test is about what mcpx keeps.
const quietSecretServer = `{"mcpServers": {"demo": {
	"command": "FAKE",
	"mcpx": {"requiresSecrets": ["MCPX_E2E_TOKEN"]}
}}}`

func TestTheSecretValueNeverReachesDisk(t *testing.T) {
	e := newEnv(t, quietSecretServer)
	e.runStdin(e2eValue+"\n", "secrets", "set", "MCPX_E2E_TOKEN")
	e.run("call", "demo.echo", `{"message":"x"}`)
	e.try("call", "demo.nope", `{}`)
	e.run("secrets", "list")
	e.run("secrets", "requests")

	// Everything the daemon and the CLI write: state, cache, logs, the HOME
	// directory, the config. The value is in memory and in a child's
	// environment, and must not be in any file.
	var found []string
	scanned := 0
	err := filepath.WalkDir(e.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 64<<20 {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		scanned++
		if strings.Contains(string(b), e2eValue) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A scan that read nothing would pass for any value at all.
	if scanned < 3 {
		t.Fatalf("the scan read %d files; a daemon's state is more than that", scanned)
	}
	if len(found) > 0 {
		t.Fatalf("the secret's value was written to disk in: %v", found)
	}
}

func TestAScriptThatReachesAWaitingServerIsRefusedBeforeItsFirstStatement(t *testing.T) {
	e := newEnv(t, secretServer)
	// The first statement prints. If the script ran at all, it would print
	// before the call that cannot succeed.
	script := `console.log("first-statement-ran"); await tools.demo.echo({message: "x"});`

	out, err := e.try("exec", script)
	if err == nil {
		t.Fatalf("exec of a script that reaches a waiting server must be refused:\n%s", out)
	}
	if strings.Contains(out, "first-statement-ran") || strings.Contains(err.Error(), "first-statement-ran") {
		t.Fatalf("the script ran its first statement before it was refused:\n%s", out)
	}
	if !strings.Contains(out, "MCPX_E2E_TOKEN") {
		t.Errorf("the refusal names what is missing:\n%s", out)
	}

	e.runStdin(e2eValue+"\n", "secrets", "set", "MCPX_E2E_TOKEN")
	if out := e.run("exec", script); !strings.Contains(out, "first-statement-ran") {
		t.Errorf("with the value given, the same script runs:\n%s", out)
	}
}

func TestPreflightOverTheSocketReadsWithoutRunning(t *testing.T) {
	e := newEnv(t, secretServer)
	e.run("ls") // starts the daemon, which the socket belongs to
	c := e.socketClient(t)

	body := `{"source":"const r = await tools.demo.echo({message: 'x'}); await tools.nowhere.gone();"}`
	resp, err := c.Post("http://mcpx/v1/preflight", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preflight = %d: %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("a preflight answer is not cacheable: %q", resp.Header.Get("Cache-Control"))
	}
	var rep struct {
		Namespaces []string `json:"namespaces"`
		Unknown    []string `json:"unknown"`
		Ready      bool     `json:"ready"`
		Missing    []struct {
			Server  string   `json:"server"`
			Secrets []string `json:"secrets"`
		} `json:"missing"`
		Requests []string `json:"requests"`
	}
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Ready || len(rep.Missing) != 1 || rep.Missing[0].Server != "demo" || len(rep.Requests) != 1 {
		t.Fatalf("demo is waiting and has one request: %s", raw)
	}
	if strings.Join(rep.Unknown, ",") != "nowhere" {
		t.Errorf("nowhere has no server: %v", rep.Unknown)
	}

	// Nothing ran: no child of demo is live. The first call would start one.
	nsResp, err := c.Get("http://mcpx/v1/namespaces")
	if err != nil {
		t.Fatal(err)
	}
	defer nsResp.Body.Close()
	var namespaces []struct {
		Server string `json:"server"`
		Live   int    `json:"live"`
	}
	if err := json.NewDecoder(nsResp.Body).Decode(&namespaces); err != nil {
		t.Fatal(err)
	}
	for _, ns := range namespaces {
		if ns.Server == "demo" && ns.Live != 0 {
			t.Errorf("preflight must not start the server: %d live", ns.Live)
		}
	}
}

func TestSecretsSetNeverTakesTheValueFromTheCommandLine(t *testing.T) {
	e := newEnv(t, secretServer)
	out, err := e.try("secrets", "set", "MCPX_E2E_TOKEN", e2eValue)
	if err == nil {
		t.Fatalf("a value on the command line is refused:\n%s", out)
	}
	if strings.Contains(out, e2eValue) {
		t.Fatalf("the refusal repeats the value it refused:\n%s", out)
	}
	if !strings.Contains(out, "standard input") {
		t.Errorf("the refusal says where the value goes:\n%s", out)
	}
}

func TestSecretsSetNeedsAValueAndAGoodName(t *testing.T) {
	e := newEnv(t, secretServer)
	if out, err := stdinTry(e, "\n", "secrets", "set", "MCPX_E2E_TOKEN"); err == nil || !strings.Contains(out, "no value") {
		t.Errorf("an empty value is refused and says so:\n%s %v", out, err)
	}
	if out, err := stdinTry(e, e2eValue, "secrets", "set", "1NOT-A-NAME"); err == nil || !strings.Contains(out, "environment variable name") {
		t.Errorf("a name that is not an environment variable is refused:\n%s %v", out, err)
	}
}

func TestADeclinedRequestFailsTheScriptThatWaitedOnIt(t *testing.T) {
	e := newEnv(t, secretServer)
	if _, err := e.try("call", "demo.echo", `{"message":"x"}`); err == nil {
		t.Fatal("the call must be refused before anything is declined")
	}
	list := e.run("secrets", "requests")
	fields := strings.Fields(list)
	var id string
	for _, f := range fields {
		if strings.HasPrefix(f, "sr-") {
			id = f
			break
		}
	}
	if id == "" {
		t.Fatalf("the listing shows the request id:\n%s", list)
	}
	if out := e.run("secrets", "decline", id); !strings.Contains(out, "declined") {
		t.Fatalf("decline should say so:\n%s", out)
	}
	if out := e.run("secrets", "request", id); !strings.Contains(out, "declined") {
		t.Errorf("the request reads as declined afterwards:\n%s", out)
	}
}

func TestTypesAndSearchNameTheCallFormAndTheNativeName(t *testing.T) {
	e := newEnv(t, secretServer)
	e.runStdin(e2eValue+"\n", "secrets", "set", "MCPX_E2E_TOKEN")

	types := e.run("types", "demo")
	if !strings.Contains(types, codegen.CanonicalSentence) {
		t.Errorf("types carries the canonical sentence:\n%s", types)
	}
	if !strings.Contains(types, "Called as tools.demo.echo(args); native name demo_echo.") {
		t.Errorf("each tool says how a script calls it and its native name:\n%s", types)
	}

	search := e.run("search", "echo")
	if !strings.Contains(search, "tools.demo.echo(args)") || !strings.Contains(search, "demo_echo") {
		t.Errorf("search shows the call form and the native name:\n%s", search)
	}
	raw := e.run("--json", "search", "echo")
	if !strings.Contains(raw, `"call": "tools.demo.echo(args)"`) || !strings.Contains(raw, `"native": "demo_echo"`) {
		t.Errorf("the JSON answer carries call and native:\n%s", raw)
	}
}

func TestTheSecretsHelpCarriesTheCanonicalWording(t *testing.T) {
	e := newEnv(t, secretServer)
	for _, cmd := range []string{"types", "search", "catalog"} {
		// Help wraps its text, so the words are compared joined by single spaces.
		out := strings.Join(strings.Fields(e.run("help", cmd)), " ")
		if !strings.Contains(out, codegen.CanonicalSentence) {
			t.Errorf("mcpx help %s should carry the canonical sentence:\n%s", cmd, out)
		}
	}
	if out := e.run("help", "secrets"); !strings.Contains(out, "standard input") {
		t.Errorf("mcpx help secrets says where the value comes from:\n%s", out)
	}
}
