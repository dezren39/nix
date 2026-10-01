package e2e_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The prompt routes' level on the autonomy dial (docs/decisions/0002). Two
// things were wrong with prompt.mode, which this replaced: an unknown value
// was read as the default without a word, and the caller's own setting never
// reached the daemon, which read the policy once from its config files at
// start -- so the environment, the flag and PUT /v1/settings were all ignored
// while the PUT answered "applied".

type resolutionDoc struct {
	Autonomy string `json:"autonomy"`
	Source   string `json:"source"`
	Result   *struct {
		Stdout string `json:"stdout"`
	} `json:"result"`
}

func TestAnUnknownAutonomyIsRefused(t *testing.T) {
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	e.run("ls")
	c := e.socketClient(t)
	for _, path := range []string{"/v1/intent", "/v1/recipes/say/run"} {
		code, doc := postJSON(t, c, path,
			`{"prompt":"echo a message back","autonomy":"plan","placeholders":{"message":"x"}}`)
		if code != http.StatusBadRequest {
			t.Errorf("%s with autonomy plan answered %d, want 400: %v", path, code, doc)
		}
		if msg, _ := doc["error"].(string); !strings.Contains(msg, "propose or run") {
			t.Errorf("%s: the refusal should name the levels: %v", path, doc)
		}
	}
	if out, err := e.try("recipes", "run", "say", "--autonomy", "plan", "message=x"); err == nil {
		t.Errorf("recipes run --autonomy plan was accepted:\n%s", out)
	}
}

func TestTheCallersPromptAutonomyReachesTheDaemon(t *testing.T) {
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	// The daemon starts without the variable; only the later command has it.
	e.run("ls")

	var proposed resolutionDoc
	if err := json.Unmarshal([]byte(e.run("--json", "prompt", "--set", "message=hello", "echo a message back")), &proposed); err != nil {
		t.Fatal(err)
	}
	if proposed.Autonomy != "propose" || proposed.Result != nil {
		t.Fatalf("the default should propose, not run: %+v", proposed)
	}

	e.setenv("MCPX_PROMPT_AUTONOMY=run")
	var ran resolutionDoc
	if err := json.Unmarshal([]byte(e.run("--json", "prompt", "--set", "message=hello", "echo a message back")), &ran); err != nil {
		t.Fatal(err)
	}
	if ran.Autonomy != "run" || ran.Result == nil || !strings.Contains(ran.Result.Stdout, "hello") {
		t.Fatalf("MCPX_PROMPT_AUTONOMY=run on the caller should run the recipe: %+v %+v", ran, ran.Result)
	}
}

func TestAPromptAutonomySetOverTheAPIIsHonoured(t *testing.T) {
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	e.run("ls")
	c := e.socketClient(t)

	req, _ := http.NewRequest(http.MethodPut, "http://mcpx/v1/settings/prompt.autonomy",
		strings.NewReader(`{"value":"run"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT prompt.autonomy answered %d", resp.StatusCode)
	}
	code, doc := postJSON(t, c, "/v1/intent",
		`{"prompt":"echo a message back","placeholders":{"message":"over the api"}}`)
	if code != http.StatusOK || doc["autonomy"] != "run" || doc["result"] == nil {
		t.Fatalf("a PUT that answered applied should change what the next request does: %d %v", code, doc)
	}
}
