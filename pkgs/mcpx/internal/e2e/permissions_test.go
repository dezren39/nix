package e2e_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestAStrictScriptCannotReadFilesOnARuntimeWithoutPermissions: node and bun
// have no permission model, and mcpx ran a strict script on them with the
// user's full authority -- it read /etc/hosts. A restriction that is not
// enforced must be refused, not ignored (docs/decisions/0003).
func TestAStrictScriptCannotReadFilesOnARuntimeWithoutPermissions(t *testing.T) {
	const probe = `import { readFileSync } from "node:fs";
try { readFileSync("/etc/hosts"); console.log("ALLOWED"); } catch { console.log("denied"); }`
	e := newEnv(t, oneServer)
	for _, rt := range []string{"node", "bun"} {
		if _, err := exec.LookPath(rt); err != nil {
			t.Logf("%s not installed", rt)
			continue
		}
		out, err := e.try("exec", "--runtime", rt, "--script-permissions", "strict", probe)
		if strings.Contains(out, "ALLOWED") {
			t.Errorf("%s ran a strict script with full authority:\n%s", rt, out)
		}
		if err == nil || !strings.Contains(out, "no permission model") {
			t.Errorf("%s with strict permissions should be refused, saying why: %v\n%s", rt, err, out)
		}
	}
}
