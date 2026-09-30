package e2e_test

import (
	"strings"
	"testing"
)

// TestConfirmDestructiveHoldsOnAColdDaemon: elicit.confirmDestructive read
// the tool's annotations from the schema cache, found nothing because the
// daemon had only just started and was still reading schemas in the
// background, and let a destructive call through unconfirmed. A safety check
// that fails open whenever the cache is cold is not a safety check. Found as
// a flake in TestADestructiveCallIsRefusedWhenNobodyConfirms on a loaded
// machine; a server slow to start makes it deterministic.
func TestConfirmDestructiveHoldsOnAColdDaemon(t *testing.T) {
	e := newEnv(t, oneServer)
	// Set in the environment the server inherits rather than in the config:
	// newEnv replaces FAKE in the config body with the binary's path, which
	// would rename the variable too.
	e.setenv("FAKEMCP_START_DELAY=1500ms",
		"MCPX_ELICIT_CONFIRM_DESTRUCTIVE=true", "MCPX_ELICIT_ASK_TIMEOUT=2s")
	out, err := e.try("call", "demo.wipe", "{}")
	if err == nil || strings.Contains(out, "wiped") {
		t.Fatalf("a destructive call on a cold daemon ran without confirmation:\n%s", out)
	}
	if !strings.Contains(out, "destructive") {
		t.Fatalf("the refusal should say why:\n%s", out)
	}
}

// TestARecipeRunOnAColdDaemonSeesItsServers: the daemon generated the
// client a recipe runs against from the schema cache, which a daemon that
// had just started had not filled yet, so the recipe died on "demo.echo is
// not a function". Found by a flaky run of an autonomy test.
func TestARecipeRunOnAColdDaemonSeesItsServers(t *testing.T) {
	e := newEnv(t, oneServer)
	writeRecipe(t, e, "say", sayRecipe)
	e.setenv("FAKEMCP_START_DELAY=1500ms")
	out, err := e.try("recipes", "run", "say", "message=cold-start")
	if err != nil || !strings.Contains(out, "cold-start") {
		t.Fatalf("a recipe run as the first command should reach its server: %v\n%s", err, out)
	}
}

// TestDiagnoseOverV1OnAColdDaemonSeesItsServers: the CLI waits for the
// schemas before diagnosing; the plugin and MCP reach /v1/diagnose directly,
// and got a false all-clear from the empty catalog.
func TestDiagnoseOverV1OnAColdDaemonSeesItsServers(t *testing.T) {
	e := newEnv(t, oneServer)
	e.setenv("FAKEMCP_START_DELAY=1500ms")
	e.run("ls")
	code, doc := postJSON(t, e.socketClient(t), "/v1/diagnose", `{"source":"await demo.echo()"}`)
	if code != 200 || doc["fatal"] != true {
		t.Fatalf("/v1/diagnose on a cold daemon should find the missing argument: %d %v", code, doc)
	}
}
