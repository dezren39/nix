package e2e_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// exitCode is the status a finished command exited with.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

var (
	elcRe = regexp.MustCompile(`elc-[0-9a-f]+`)
	tskRe = regexp.MustCompile(`tsk-[0-9a-f]+`)
)

// settle declines the question a reported call left open and waits for that
// call to finish. askmcp serves one request at a time, so a call still
// waiting on its question would hold the next one in the queue.
func settle(t *testing.T, e *env, out string) {
	t.Helper()
	q, c := elcRe.FindString(out), tskRe.FindString(out)
	if q == "" || c == "" {
		t.Fatalf("no question or call id to settle in:\n%s", out)
	}
	e.run("elicit", "decline", q)
	e.run("task", "result", c)
}

// TestACallWaitingForInputExits75 is #286: a server that stops to ask
// something, under a command with nobody to answer it, exits
// ExitInputRequired at once with the question and how to answer it -- and
// the question stays answerable, with the call's result collectable after.
// Before, the call held for elicit.ttl, the server was told "cancel", and
// the command exited 0.
func TestACallWaitingForInputExits75(t *testing.T) {
	e := askEnv(t)

	out, err := e.try("call", "ask.need_repo")
	if got := exitCode(err); got != 75 {
		t.Fatalf("call: exit %d, want 75:\n%s", got, out)
	}
	for _, want := range []string{"Which repository should this go in?",
		"mcpx elicit answer elc-", `"repo":"..."`, "mcpx task result tsk-"} {
		if !strings.Contains(out, want) {
			t.Errorf("call: the message should contain %q:\n%s", want, out)
		}
	}
	settle(t, e, out)

	// --json: the same document as data, then answer it and collect.
	jout, err := e.try("--json", "call", "ask.need_repo")
	if got := exitCode(err); got != 75 {
		t.Fatalf("--json call: exit %d, want 75:\n%s", got, jout)
	}
	var doc struct {
		InputRequired struct {
			CallID    string `json:"callId"`
			Questions []struct {
				ID string `json:"id"`
			} `json:"questions"`
		} `json:"inputRequired"`
	}
	// The document first on stdout; the same message follows on stderr.
	if err := json.NewDecoder(strings.NewReader(jout[strings.Index(jout, "{"):])).Decode(&doc); err != nil || len(doc.InputRequired.Questions) == 0 {
		t.Fatalf("--json call: no inputRequired document (%v):\n%s", err, jout)
	}
	e.run("elicit", "answer", doc.InputRequired.Questions[0].ID, `{"repo":"me/thing"}`)
	res := e.run("task", "result", doc.InputRequired.CallID)
	if !strings.Contains(res, "using me/thing") {
		t.Errorf("the answered call's result should be collectable:\n%s", res)
	}
}

func TestAScriptWaitingForInputExits75(t *testing.T) {
	e := askEnv(t)
	script := filepath.Join(e.dir, "s.ts")
	if err := os.WriteFile(script, []byte(
		"export default async () => { await ask.need_repo({}); console.log(\"AFTER\"); };\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		argv []string
	}{
		// The catch is the point: a script that swallows ToolError must not
		// be able to hide that a question is waiting.
		{"exec", []string{"exec", `try { await ask.need_repo({}); } catch { console.log("CAUGHT"); } console.log("AFTER");`}},
		{"exec --remote", []string{"exec", "--remote", `await ask.need_repo({}); console.log("AFTER");`}},
		{"run", []string{"run", script}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := e.try(c.argv...)
			if got := exitCode(err); got != 75 {
				t.Fatalf("exit %d, want 75:\n%s", got, out)
			}
			if !strings.Contains(out, "Which repository should this go in?") ||
				!strings.Contains(out, "mcpx elicit answer elc-") {
				t.Errorf("the message should say what is asked and how to answer:\n%s", out)
			}
			if strings.Contains(out, "AFTER") || strings.Contains(out, "CAUGHT") {
				t.Errorf("the script should stop at the question:\n%s", out)
			}
			settle(t, e, out)
		})
	}
}
