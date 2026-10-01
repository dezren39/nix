package e2e_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
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
		if err == nil || !strings.Contains(out, `profile "strict"`) || !strings.Contains(out, "script.profiles.strict."+rt) {
			t.Errorf("%s with strict permissions should be refused, saying why: %v\n%s", rt, err, out)
		}
	}
}

// A profile that defines node flags has to reach node: readnet runs node
// under --permission with reads allowed, so a write is refused by node
// itself. Without the flags the same write succeeds, which is the control.
func TestNodeReceivesTheFlagsAProfileDefinesForIt(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	e := newEnv(t, oneServer)
	target := filepath.Join(t.TempDir(), "w")
	probe := `import { writeFileSync } from "node:fs";
try { writeFileSync(` + jsonQuote(target) + `, "1"); console.log("WROTE"); } catch (e) { console.log("DENIED " + e.code); }`
	out, err := e.try("exec", "--runtime", "node", "--permissions", "readnet", probe)
	if err != nil || !strings.Contains(out, "DENIED ERR_ACCESS_DENIED") {
		t.Errorf("node under readnet should refuse a write: %v\n%s", err, out)
	}
	out, err = e.try("exec", "--runtime", "node", "--permissions", "all", probe)
	if err != nil || !strings.Contains(out, "WROTE") {
		t.Errorf("node under all should write: %v\n%s", err, out)
	}
}

// A runtime declared by name, with a binary whose name says nothing, runs
// with the argv of its kind.
func TestADeclaredRuntimeRunsByName(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	e := newEnv(t, oneServer).withEnv(
		`MCPX_SCRIPT_RUNTIMES={"engine":{"kind":"node","bin":` + jsonQuote(node) + `}}`)
	out, err := e.try("exec", "--runtime", "engine", `console.log("kind=" + process.env.MCPX_RUNTIME)`)
	if err != nil || !strings.Contains(out, "kind=node") {
		t.Errorf("declared runtime: %v\n%s", err, out)
	}
	out, err = e.try("exec", "--runtime", "sh", `console.log(1)`)
	if err == nil || !strings.Contains(out, "not a known JavaScript runtime") {
		t.Errorf("--runtime sh: %v\n%s", err, out)
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
