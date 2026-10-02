package e2e_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The example scripts shipped in the mcpx-basics skill, run as a reader
// would run them: copied out, `mcpx run`, against a real daemon and the test
// server. An example that does not run teaches the wrong thing with
// authority, so every file in the folder must have a case here.
const examplesDir = "../../plugin/opencode/skills/mcpx-basics/examples"

func TestSkillExamplesRun(t *testing.T) {
	type tc struct {
		args []string
		code int
		want []string
	}
	cases := map[string][]tc{
		"find-tool.ts": {
			{[]string{"echo"}, 0, []string{"demo.echo({ message })", "Echo a message back."}},
			{[]string{"nothingmatchesthis"}, 0, []string{"no tool matches", "namespaces: demo"}},
		},
		"call-any.ts": {
			{[]string{"demo.echo", `{"message":"hello"}`}, 0, []string{"5 chars", "hello"}},
		},
		"filter-in-script.ts": {
			{[]string{"demo.structured", "{}", "n"}, 0, []string{"object with keys n", "42"}},
			{[]string{"demo.structured", "{}", "missing"}, 0, []string{`no field "missing"`}},
		},
		"handle-errors.ts": {
			{[]string{"demo.boom"}, 1, []string{"tool error from demo.boom: boom: deliberate failure"}},
			{[]string{"demo.echo", `{"message":"fine"}`}, 0, []string{`ok: "fine"`}},
		},
		"parallel.ts": {
			{[]string{"demo.echo", `{"message":"a"}`, `{"message":"b"}`}, 0, []string{`ok    "a"`, `ok    "b"`, "2 calls in"}},
			{[]string{"demo.boom", `{}`}, 0, []string{"error boom: deliberate failure", "1 calls in"}},
		},
		"save-artifact.ts": {
			// structured answers with a caption and a PNG: the image is kept.
			{[]string{"demo.structured", "{}", "shot.png"}, 0, []string{"shot.png: 8 bytes, image/png, mcpx://artifacts/"}},
			{[]string{"demo.echo", `{"message":"x"}`, "r.json"}, 0, []string{"r.json:", "application/json"}},
		},
		"read-resource.ts": {
			{[]string{"demo", "demo://greeting"}, 0, []string{"hello from a resource"}},
		},
	}

	ents, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatal(err)
	}
	scripts := 0
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".ts") && !strings.HasPrefix(e.Name(), "mcpx-") {
			scripts++
			if _, ok := cases[e.Name()]; !ok {
				t.Errorf("example %s has no case here, so nothing checks that it runs", e.Name())
			}
		}
	}
	if scripts == 0 || scripts != len(cases) {
		t.Fatalf("%d example scripts on disk, %d cases", scripts, len(cases))
	}

	for _, rt := range []string{"deno", "bun", "node"} {
		t.Run(rt, func(t *testing.T) {
			if _, err := exec.LookPath(rt); err != nil {
				t.Skipf("%s not installed", rt)
			}
			e := newEnv(t, oneServer)
			dir := filepath.Join(e.dir, "examples")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, runs := range cases {
				body, err := os.ReadFile(filepath.Join(examplesDir, name))
				if err != nil {
					t.Fatal(err)
				}
				script := filepath.Join(dir, name)
				if err := os.WriteFile(script, body, 0o644); err != nil {
					t.Fatal(err)
				}
				// Every example explains itself when given nothing.
				runs = append(runs, tc{nil, 2, []string{"usage: mcpx run " + name}})
				for i, c := range runs {
					flags := []string{"run", "--runtime", rt}
					if rt == "deno" && i == len(runs)-1 {
						// A reader may turn type checking on; the
						// examples must survive it.
						flags = append(flags, "--typecheck=on")
					}
					out, err := e.try(append(append(flags, script), c.args...)...)
					code := 0
					var ee *exec.ExitError
					if errors.As(err, &ee) {
						code = ee.ExitCode()
					} else if err != nil {
						t.Fatalf("%s %v: %v", name, c.args, err)
					}
					if code != c.code {
						t.Errorf("%s %v: exit %d, want %d\n%s", name, c.args, code, c.code, out)
					}
					for _, w := range c.want {
						if !strings.Contains(out, w) {
							t.Errorf("%s %v: output lacks %q\n%s", name, c.args, w, out)
						}
					}
				}
			}
		})
	}
}
