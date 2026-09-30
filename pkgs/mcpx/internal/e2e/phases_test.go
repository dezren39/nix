package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The launcher phases are the hook vocabulary docs/decisions/0001 chose, and
// two of the holes it found in their delivery are closed here.

// TestASnippetRunsItsBeforeAndOnSuccessPhases: `mcpx exec` accepted
// --before, --on-success and --on-error and dropped them; only a file got
// them.
func TestASnippetRunsItsBeforeAndOnSuccessPhases(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("exec", "--before", `console.log("phase-before")`,
		"--on-success", `console.log("phase-ok")`, `console.log("the-body")`)
	b, body, ok := strings.Index(out, "phase-before"), strings.Index(out, "the-body"), strings.Index(out, "phase-ok")
	if b < 0 || body < 0 || ok < 0 || !(b < body && body < ok) {
		t.Fatalf("want before, body, onSuccess in that order:\n%s", out)
	}
}

// TestAThrowingSnippetRunsOnError: a snippet fails by throwing at top level,
// which happened during the launcher's import -- outside its try -- so the
// phase that exists for failures never ran for the commonest one.
func TestAThrowingSnippetRunsOnError(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("exec", "--on-error", `console.log("phase-error:" + String(result.error))`,
		`throw new Error("snippet-boom")`)
	if err == nil {
		t.Fatalf("a throwing snippet should still fail:\n%s", out)
	}
	if !strings.Contains(out, "phase-error:Error: snippet-boom") {
		t.Fatalf("onError did not run, or did not see the error:\n%s", out)
	}
}

// TestAFileThatThrowsAtTopLevelRunsOnErrorAndSuffix: the same hole for a
// file with no entry point, where suffix -- the launcher's finally -- was
// skipped too.
func TestAFileThatThrowsAtTopLevelRunsOnErrorAndSuffix(t *testing.T) {
	e := newEnv(t, oneServer)
	script := filepath.Join(e.dir, "fails.ts")
	if err := os.WriteFile(script, []byte(`throw new Error("file-boom");`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := e.try("run", "--on-error", `console.log("phase-error")`,
		"--suffix", `console.log("phase-suffix")`, script)
	if err == nil {
		t.Fatalf("a throwing file should still fail:\n%s", out)
	}
	for _, want := range []string{"phase-error", "phase-suffix"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s did not run for a top-level throw:\n%s", want, out)
		}
	}
}

// TestARemoteExecPrintsItsOutput: `mcpx exec --remote` asked the daemon for
// its text answer, the script's bare stdout, and then decoded it as a JSON
// Result -- so plain output failed with "invalid character" and output that
// happened to be JSON printed nothing and exited 0.
func TestARemoteExecPrintsItsOutput(t *testing.T) {
	e := newEnv(t, oneServer)
	if out := e.run("exec", "--remote", `console.log("remote-plain")`); !strings.Contains(out, "remote-plain") {
		t.Errorf("plain stdout was lost:\n%s", out)
	}
	if out := e.run("exec", "--remote", `console.log(JSON.stringify({remote: "json"}))`); !strings.Contains(out, `{"remote":"json"}`) {
		t.Errorf("JSON-looking stdout was swallowed:\n%s", out)
	}
	if _, err := e.try("exec", "--remote", `throw new Error("remote-boom")`); err == nil {
		t.Errorf("a failing remote script should fail the command")
	}
}
