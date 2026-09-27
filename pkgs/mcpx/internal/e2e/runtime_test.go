package e2e_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestGeneratedClientWorksOnEveryRuntime guards the portability of the
// generated module. It reads the session from the environment, and each
// runtime exposes the environment differently (Deno.env vs process.env), so a
// regression here would silently break isolation on one runtime only.
func TestGeneratedClientWorksOnEveryRuntime(t *testing.T) {
	for _, rt := range []string{"deno", "bun", "node"} {
		rt := rt
		t.Run(rt, func(t *testing.T) {
			if _, err := exec.LookPath(rt); err != nil {
				t.Skipf("%s not installed", rt)
			}
			e := newEnv(t, oneServer)
			out, err := e.try("exec", "--runtime", rt,
				`const r = await demo.echo({ message: "hello from "+(globalThis as any).MCPX_RT }); console.log(String(r));`)
			if err != nil {
				t.Fatalf("%s: %v\n%s", rt, err, out)
			}
			if !strings.Contains(out, "hello from") {
				t.Fatalf("%s produced no output:\n%s", rt, out)
			}
		})
	}
}

// TestSessionIsolationHoldsOnEveryRuntime checks the environment-variable path
// specifically: if a runtime cannot read MCPX_SESSION, every run would collapse
// onto one session and stateful servers would be shared again.
func TestSessionIsolationHoldsOnEveryRuntime(t *testing.T) {
	for _, rt := range []string{"deno", "bun", "node"} {
		rt := rt
		t.Run(rt, func(t *testing.T) {
			if _, err := exec.LookPath(rt); err != nil {
				t.Skipf("%s not installed", rt)
			}
			e := newEnv(t, statefulServer)
			// Two sequential runs with explicit, distinct sessions must not
			// see each other's writes.
			if out, err := e.try("exec", "--runtime", rt, "--session", "iso-a",
				`await demo.open({ value: "a" }); console.log(JSON.stringify(await demo.state()));`); err != nil {
				t.Fatalf("%s run a: %v\n%s", rt, err, out)
			}
			out, err := e.try("exec", "--runtime", rt, "--session", "iso-b",
				`await demo.open({ value: "b" }); console.log(JSON.stringify(await demo.state()));`)
			if err != nil {
				t.Fatalf("%s run b: %v\n%s", rt, err, out)
			}
			if strings.Contains(out, `"a"`) {
				t.Fatalf("%s: session b saw session a's state, so MCPX_SESSION is not reaching the client:\n%s", rt, out)
			}
			if !strings.Contains(out, `"b"`) {
				t.Fatalf("%s: session b lost its own state:\n%s", rt, out)
			}
		})
	}
}
