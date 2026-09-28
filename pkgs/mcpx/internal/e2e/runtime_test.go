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
			// MCPX_RUNTIME is what the runner actually sets; read it the way
			// a script would, from the process environment rather than from a
			// global that was never defined.
			out, err := e.try("exec", "--runtime", rt,
				`const name = (globalThis as any).Deno?.env?.get?.("MCPX_RUNTIME") `+
					`?? (globalThis as any).process?.env?.MCPX_RUNTIME ?? "unset";`+
					`const r = await demo.echo({ message: "hello from " + name }); console.log(String(r));`)
			if err != nil {
				t.Fatalf("%s: %v\n%s", rt, err, out)
			}
			// Asserting the prefix alone let "hello from undefined" pass,
			// which is the one outcome this test exists to catch: the
			// round trip working while the runtime identity is lost.
			want := "hello from " + rt
			if !strings.Contains(out, want) {
				t.Fatalf("%s should have echoed %q:\n%s", rt, want, out)
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
