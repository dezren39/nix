package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #108: node was invoked with --experimental-strip-types, whose strip-only
// mode rejects non-erasable TypeScript (enum, value namespaces) that Node
// runs fine under --experimental-transform-types. On machines without deno
// or bun, auto-resolution lands on node, so every such script failed with a
// raw SyntaxError that looked like the author's mistake.
func TestNodeArgsUseTransformTypesNotStripTypes(t *testing.T) {
	fakeRuntimes(t, "node")
	got, err := Resolve("node", "all", Setup{})
	if err != nil {
		t.Fatal(err)
	}
	args := got.Args("s.ts")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--experimental-transform-types") {
		t.Errorf("node argv %q does not contain --experimental-transform-types", args)
	}
	if strings.Contains(joined, "--experimental-strip-types") {
		t.Errorf("node argv %q still contains --experimental-strip-types (#108)", args)
	}
}

// End-to-end guard for #108: a non-erasable TypeScript file must actually
// run under the resolved node runtime. Skips when no suitable node is on
// PATH (transform-types needs Node >= 22.6; older nodes reject the flag
// with "bad option").
func TestNodeRunsNonErasableTypeScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node on PATH")
	}
	got, err := Resolve("node", "all", Setup{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "en.ts")
	src := "enum E { A }\nconsole.log(\"enum ok\", E.A);\n"
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(got.Bin, got.Args(script)...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "bad option") {
			t.Skipf("this node does not support --experimental-transform-types: %s", out)
		}
		t.Fatalf("node failed on enum script (#108): %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "enum ok 0") {
		t.Errorf("unexpected output %q, want %q", out, "enum ok 0")
	}
}
