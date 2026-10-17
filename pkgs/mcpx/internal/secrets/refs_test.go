package secrets

import (
	"reflect"
	"testing"
)

func none(string) bool { return false }

func only(names ...string) func(string) bool {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return func(ns string) bool { return set[ns] }
}

func TestReferencesFindsTheNamespacesAScriptReaches(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		configured func(string) bool
		want       []string
	}{
		{"dotted call", `const r = await tools.fs.read({path: "/x"});`, none, []string{"fs"}},
		{"bracket form", `await tools["fs"]["read"]({})`, none, []string{"fs"}},
		{"single quotes in brackets", `await tools['chrome']['list']()`, none, []string{"chrome"}},
		{"a reference without a call", `const f = tools.gh;`, none, []string{"gh"}},
		{"across lines", "await tools\n  .fs\n  .read({})", none, []string{"fs"}},
		{"nested calls both count", `console.log(await tools.a.b({ x: tools.c.d() }))`, none, []string{"a", "c"}},
		{"a namespace that is not configured still counts", `tools.nope.x()`, none, []string{"nope"}},
		{"names are sorted and unique", `tools.b.x(); tools.a.y(); tools.b.z();`, none, []string{"a", "b"}},

		{"a string is not code", `const s = "tools.fs.read()";`, none, nil},
		{"a single-quoted string is not code", `const s = 'tools.fs.read()';`, none, nil},
		{"an escaped quote does not end the string", `const s = "it\"s tools.fs.read()";`, none, nil},
		{"a line comment is not code", "// tools.fs.read()\nx();", none, nil},
		{"a block comment is not code", "/* tools.gh.x() */ y();", none, nil},
		{"plain template text is not code", "const s = `tools.fs.read()`;", none, nil},
		{"an unterminated string ends at its line", "const s = \"abc\ntools.fs.read()", none, []string{"fs"}},

		{"a template interpolation is code", "console.log(`${await tools.chrome.list()}`)", none, []string{"chrome"}},
		{"a call inside an object in an interpolation", "`${({x: tools.gh.get()}).x}`", none, []string{"gh"}},
		{"plain text after an interpolation is still text", "`${1} tools.fs.read()`", none, nil},

		{"a member of something else is not a namespace", `client.tools.fs.read()`, none, nil},
		{"optional chaining is a member too", `x?.tools.fs.read()`, none, nil},
		{"a computed key is invisible", `tools[name].read()`, none, nil},
		{"a key in an object is not a reference", `const o = { demo: 1 }`, only("demo"), nil},

		{"a bare configured namespace is a reference", `await demo.echo({})`, only("demo"), []string{"demo"}},
		{"a bare unconfigured name is not", `await other.echo({})`, only("demo"), nil},
		{"a bare name the script declares is a local", "const demo = {x: 1};\ndemo.echo();", only("demo"), nil},
		{"an imported name is declared too", `import { demo } from "./lib.ts"; demo.echo();`, only("demo"), nil},
		{"an aliased import declares the alias", `import { x as demo } from "./lib.ts"; demo.echo();`, only("demo"), nil},
		{"a bare name inside a string is not", `const s = "demo.echo()";`, only("demo"), nil},
		{"both spellings in one script", `await demo.echo(); await tools.fs.read();`, only("demo"), []string{"demo", "fs"}},

		{"an empty script reaches nothing", ``, none, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := References(c.src, c.configured)
			if got == nil {
				t.Fatal("References must never return nil: it serialises as null")
			}
			// Empty and nil are the same answer here: nothing was reached.
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("References(%q) = %v; want %v", c.src, got, c.want)
			}
		})
	}
}
