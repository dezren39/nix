package e2e_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setenv adds variables to every later invocation in this environment.
func (e *env) setenv(pairs ...string) { e.envVars = append(e.envVars, pairs...) }

func TestDiagnoseNamesTheArgumentAndTheMinimumThatWorks(t *testing.T) {
	// The whole claim of the deterministic diagnostic: not "the arguments
	// are wrong" but which one, and what to write instead.
	e := newEnv(t, oneServer)
	e.run("ls")
	out, err := e.try("diagnose", "await demo.echo()")
	if err == nil {
		t.Fatalf("a call missing a required argument should fail:\n%s", out)
	}
	for _, want := range []string{"message is required", "you wrote", "minimum",
		`await demo.echo({ message: "" })`} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnostic is missing %q:\n%s", want, out)
		}
	}
}

func TestDiagnoseSuggestsTheToolYouMeant(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	out, _ := e.try("diagnose", `await demo.eco({message:"x"})`)
	if !strings.Contains(out, "echo") {
		t.Fatalf("the closest name should be offered:\n%s", out)
	}
}

func TestDiagnoseIsQuietWhenEverythingMatches(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	out := e.run("diagnose", `await demo.echo({message:"x"})`)
	if !strings.Contains(out, "Nothing to report") {
		t.Fatalf("a correct script should pass:\n%s", out)
	}
}

func TestRunStopsBeforeTheScriptWhenItDoesNotMatchTheSchemas(t *testing.T) {
	// The reason this is on the run path and not only on demand: otherwise
	// the failure arrives from the server, halfway through, after the side
	// effects of every call before it.
	e := newEnv(t, oneServer)
	e.run("ls")
	script := filepath.Join(e.dir, "bad.ts")
	body := `import tools from "./mcpx-client.ts";
await tools.demo.echo();
`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := e.try("run", script)
	if err == nil {
		t.Fatalf("expected a refusal:\n%s", out)
	}
	if !strings.Contains(out, "message is required") {
		t.Fatalf("the refusal should say what is wrong:\n%s", out)
	}
}

// writeRecipe puts a saved script on the project's scripts path.
func writeRecipe(t *testing.T, e *env, name, body string) {
	t.Helper()
	dir := filepath.Join(e.dir, ".config", "mcpx", "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".ts"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sayRecipe = `// Echo a message back through the demo server.
// @param message:string   what to say
// @param loud:boolean = false   shout it
const text = @loud ? String(@message).toUpperCase() : @message;
console.log(String(await demo.echo({ message: text })));
`

func TestRecipesAreListedWithTheirPlaceholders(t *testing.T) {
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	e.run("ls")
	out := e.run("recipes")
	for _, want := range []string{"say", "message", "loud", "demo.echo"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing is missing %q:\n%s", want, out)
		}
	}
	// console.log is not a tool, and counting it as one would make every
	// recipe match the word "log".
	if strings.Contains(out, "console.log") {
		t.Errorf("a JavaScript built-in is not a tool:\n%s", out)
	}
}

func TestARecipeRendersWithoutRunning(t *testing.T) {
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	e.run("ls")
	out := e.run("recipes", "run", "say", "--script", "message=hi", "loud=true")
	if !strings.Contains(out, `String("hi").toUpperCase()`) {
		t.Fatalf("the values should be substituted as JSON:\n%s", out)
	}
	if strings.Contains(out, "@message") {
		t.Fatalf("a placeholder survived:\n%s", out)
	}
}

func TestARecipeThatIsMissingAValueSaysSoRatherThanRunning(t *testing.T) {
	// Nobody is watching in a test, which is exactly the headless case: the
	// question is opened, the deadline passes, and the caller is told what
	// it needed instead of hanging.
	e := newEnv(t, oneServer)
	e.setenv("MCPX_ELICIT_ASK_TIMEOUT=2s")
	writeRecipe(t, e, "say", sayRecipe)
	e.run("ls")
	out, err := e.try("recipes", "run", "say")
	if err == nil {
		t.Fatalf("an unfilled recipe must not look like a successful no-op:\n%s", out)
	}
	if !strings.Contains(out, "message") || !strings.Contains(out, "elc-") {
		t.Fatalf("it should name what was missing and the question it raised:\n%s", out)
	}
}

func TestRecipesAreMatchedDeterministically(t *testing.T) {
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	e.run("ls")
	first := e.run("recipes", "match", "echo a message")
	if !strings.Contains(first, "say") {
		t.Fatalf("expected a match:\n%s", first)
	}
	for i := 0; i < 3; i++ {
		if again := e.run("recipes", "match", "echo a message"); again != first {
			t.Fatalf("a deterministic match changed between runs:\n%s\nvs\n%s", first, again)
		}
	}
}

func TestSavingAScriptMakesItARecipe(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	out := e.run("recipes", "save", "greet", sayRecipe)
	if !strings.Contains(out, "greet") {
		t.Fatalf("save should say where it went:\n%s", out)
	}
	if !strings.Contains(e.run("recipes"), "greet") {
		t.Fatal("a saved recipe should appear in the listing")
	}
	shown := e.run("recipes", "show", "greet")
	if !strings.Contains(shown, "message: string") {
		t.Fatalf("show should describe the placeholders:\n%s", shown)
	}
}

func TestPromptSaysPlainlyThatThereIsNoModel(t *testing.T) {
	// The default. mcpx has no model, most callers cannot answer sampling,
	// and pretending otherwise costs the whole deadline to discover.
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	e.run("ls")
	out := e.run("prompt", "rotate the database credentials")
	if !strings.Contains(out, "no recipe matched") {
		t.Fatalf("it should say nothing matched:\n%s", out)
	}
	if !strings.Contains(out, "prompt.sample") {
		t.Fatalf("it should say why no script was written:\n%s", out)
	}
}

func TestPromptUsesARecipeWhenOneClearlyMatches(t *testing.T) {
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	e.run("ls")
	out := e.run("prompt", "--set", "message=hello", "echo a message back")
	if !strings.Contains(out, "recipe say") {
		t.Fatalf("a matched recipe should be used rather than generated:\n%s", out)
	}
	if !strings.Contains(out, `"hello"`) {
		t.Fatalf("the placeholder should be filled in:\n%s", out)
	}
}

func TestCatalogHistoryIsServed(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)
	resp, err := c.Get("http://mcpx/v1/catalog/history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var doc struct {
		Tools   []string       `json:"tools"`
		Changes map[string]any `json:"changes"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("history should be JSON: %v\n%s", err, b)
	}
	if doc.Changes == nil {
		t.Errorf("expected a changes object, got %s", b)
	}
}

func TestDiagnoseOverTheSocket(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)
	resp, err := c.Post("http://mcpx/v1/diagnose", "application/json",
		strings.NewReader(`{"source":"await demo.echo()"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var doc struct {
		Fatal       bool `json:"fatal"`
		Diagnostics []struct {
			Tool  string `json:"tool"`
			Field string `json:"field"`
			Fix   string `json:"fix"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if !doc.Fatal || len(doc.Diagnostics) != 1 ||
		doc.Diagnostics[0].Field != "message" || doc.Diagnostics[0].Fix == "" {
		t.Fatalf("unexpected diagnostics: %s", b)
	}
}

func TestADestructiveCallIsRefusedWhenNobodyConfirms(t *testing.T) {
	// Refusing is the opposite of what disambiguation does on a timeout, and
	// for the opposite reason: there, doing nothing is safe; here, doing
	// nothing is the destructive thing.
	e := newEnv(t, oneServer)
	e.setenv("MCPX_ELICIT_CONFIRM_DESTRUCTIVE=true", "MCPX_ELICIT_ASK_TIMEOUT=2s")
	e.run("ls")
	out, err := e.try("call", "demo.wipe", "{}")
	if err == nil {
		t.Fatalf("expected a refusal:\n%s", out)
	}
	if !strings.Contains(out, "destructive") {
		t.Fatalf("the refusal should say why:\n%s", out)
	}
	// A tool annotated read-only is untouched by the policy.
	if out := e.run("call", "demo.echo", `{"message":"fine"}`); !strings.Contains(out, "fine") {
		t.Fatalf("a read-only call should not be confirmed:\n%s", out)
	}
	// An unannotated one is not: the specification's destructiveHint
	// defaults to true, and an absent hint was read as false (#207).
	if out, err := e.try("call", "demo.structured", "{}"); err == nil || !strings.Contains(out, "destructive") {
		t.Fatalf("an unannotated tool may be destructive and should be confirmed: err=%v\n%s", err, out)
	}
}

func TestADestructiveCallProceedsWhenSomebodyConfirms(t *testing.T) {
	e := newEnv(t, oneServer)
	e.setenv("MCPX_ELICIT_CONFIRM_DESTRUCTIVE=true", "MCPX_ELICIT_ASK_TIMEOUT=60s")
	e.run("ls")

	done := make(chan string, 1)
	go func() {
		out, _ := e.try("call", "demo.wipe", "{}")
		done <- out
	}()

	id := waitForQuestion(t, e)
	e.run("elicit", "answer", id, `{"confirm":true}`)
	select {
	case out := <-done:
		if !strings.Contains(out, "wiped") {
			t.Fatalf("a confirmed call should go through:\n%s", out)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the call did not finish after the question was answered")
	}
}

const exclusiveAsking = `{
  "mcpServers": {
    "demo": { "command": "FAKE", "mcpx": { "sharing": "exclusive", "scope": "session", "max": 4 } }
  },
  "elicit": { "disambiguate": "ask", "askTimeout": "60s" }
}`

func TestDisambiguationRoutesTheCallToTheChosenInstance(t *testing.T) {
	// The case from the issue: three browsers, and a script that named none
	// of them. The answer must decide which process serves the call, not
	// merely be recorded.
	e := newEnv(t, exclusiveAsking)
	e.run("call", "--session", "work", "demo.open", `{"value":"work"}`)
	e.run("call", "--session", "scratch", "demo.open", `{"value":"scratch"}`)

	done := make(chan string, 1)
	go func() {
		out, _ := e.try("call", "--session", "third", "demo.state", "{}")
		done <- out
	}()

	id := waitForQuestion(t, e)
	shown := e.run("elicit", "show", id)
	for _, want := range []string{"session:work", "session:scratch", "new"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the question should offer %q with its details:\n%s", want, shown)
		}
	}
	e.run("elicit", "answer", id, `{"instance":"session:work"}`)

	select {
	case out := <-done:
		if !strings.Contains(out, "work") {
			t.Fatalf("the call should have gone to the chosen instance:\n%s", out)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the call did not finish after the question was answered")
	}
}

func TestDisambiguationFallsBackWhenNobodyAnswers(t *testing.T) {
	e := newEnv(t, exclusiveAsking)
	e.setenv("MCPX_ELICIT_ASK_TIMEOUT=2s")
	e.run("call", "--session", "work", "demo.open", `{"value":"work"}`)
	e.run("call", "--session", "scratch", "demo.open", `{"value":"scratch"}`)
	// The default is a fresh instance: a caller that wanted a particular one
	// and did not say so must not silently get somebody else's.
	out := e.run("call", "--session", "third", "demo.state", "{}")
	if strings.Contains(out, "work") || strings.Contains(out, "scratch") {
		t.Fatalf("an unanswered question should not hand over another caller's instance:\n%s", out)
	}
}

// waitForQuestion polls until something is waiting for an answer.
func waitForQuestion(t *testing.T, e *env) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, err := e.try("--json", "elicit", "list")
		if err == nil {
			var pending []struct {
				ID string `json:"id"`
			}
			if json.Unmarshal([]byte(out), &pending) == nil && len(pending) > 0 {
				return pending[0].ID
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("no question was raised")
	return ""
}
