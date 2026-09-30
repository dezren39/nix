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
