package e2e_test

import (
	"strings"
	"testing"
)

// excludeTools by pattern, end to end: the hidden tool is gone from the
// listing and refused when called by name; the rest still work. A
// top-level pool exclusion applies to the server too.
func TestExcludedToolsAreNeitherListedNorCallable(t *testing.T) {
	e := newEnv(t, `{
  "pool": { "excludeTools": ["/^struct/"] },
  "mcpServers": {
    "demo": { "command": "FAKE", "mcpx": { "excludeTools": ["wi*"] } }
  }
}`)
	e.run("ls")
	listed := e.run("tools", "--ns", "demo")
	for _, gone := range []string{"wipe", "structured"} {
		if strings.Contains(listed, gone) {
			t.Errorf("%s should not be listed:\n%s", gone, listed)
		}
	}
	if !strings.Contains(listed, "echo") {
		t.Errorf("echo should be listed:\n%s", listed)
	}
	for _, tool := range []string{"demo.wipe", "demo.structured"} {
		out, err := e.try("call", tool, "{}")
		if err == nil || !strings.Contains(out, "hidden") {
			t.Errorf("%s should be refused as hidden: err=%v\n%s", tool, err, out)
		}
	}
	if out := e.run("call", "demo.echo", `{"message":"fine"}`); !strings.Contains(out, "fine") {
		t.Fatalf("a visible tool should still work:\n%s", out)
	}
}
