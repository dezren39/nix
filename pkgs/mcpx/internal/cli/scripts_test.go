package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/testsupport"
)

// chdir moves into dir and isolates the search from the real user.
//
// Script and recipe discovery has two roots: the walk up from the working
// directory, and $HOME. Setting XDG_CONFIG_HOME closes neither -- recipes.Dirs
// takes home from os.UserHomeDir -- so the developer's own
// ~/.config/mcpx/scripts was listed alongside each test's fixtures and these
// tests failed on a machine that had any. HOME is set here rather than in each
// test because a test that forgets it fails only for whoever has scripts.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	t.Setenv("HOME", filepath.Join(dir, "home-empty"))
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPathShapedArgumentsAreUsedVerbatim(t *testing.T) {
	for _, arg := range []string{"./a.ts", "/abs/b.ts", "sub/c.ts", "d.ts", "e.js"} {
		got, err := resolveScript(arg)
		if err != nil {
			t.Fatalf("%q: %v", arg, err)
		}
		if got != arg {
			t.Errorf("%q was rewritten to %q; path-shaped arguments must pass through", arg, got)
		}
	}
}

func TestBareNameResolvesFromProjectScripts(t *testing.T) {
	root := testsupport.TempDir(t)
	want := writeScript(t, filepath.Join(root, ScriptsDirName), "report.ts", "// r\n")
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	got, err := resolveScript("report")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBareNameFoundInAParentDirectory(t *testing.T) {
	root := testsupport.TempDir(t)
	want := writeScript(t, filepath.Join(root, ScriptsDirName), "shared.ts", "// s\n")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, deep)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	got, err := resolveScript("shared")
	if err != nil {
		t.Fatalf("a script in an ancestor .mcpx/scripts should be found: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNearerScriptShadowsFurtherOne(t *testing.T) {
	root := testsupport.TempDir(t)
	writeScript(t, filepath.Join(root, ScriptsDirName), "dup.ts", "// outer\n")
	inner := filepath.Join(root, "inner")
	want := writeScript(t, filepath.Join(inner, ScriptsDirName), "dup.ts", "// inner\n")
	chdir(t, inner)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	got, err := resolveScript("dup")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("nearest .mcpx/scripts must win: got %q, want %q", got, want)
	}
}

func TestUserScriptsAreFoundWhenNoProjectScriptExists(t *testing.T) {
	root := testsupport.TempDir(t)
	xdg := filepath.Join(root, "xdg")
	want := writeScript(t, filepath.Join(xdg, "mcpx", "scripts"), "global.ts", "// g\n")
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, work)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	got, err := resolveScript("global")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMissingScriptErrorListsSearchPathAndHowToCreateIt(t *testing.T) {
	root := testsupport.TempDir(t)
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	_, err := resolveScript("nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"nope", ScriptsDirName, "looked in", "mcpx run"} {
		if !strings.Contains(msg, want) && want != "mcpx run" {
			t.Errorf("error should mention %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, filepath.Join(root, ScriptsDirName)) {
		t.Errorf("error should list the project scripts dir:\n%s", msg)
	}
}

func TestDiscoverScriptsMarksShadowedEntries(t *testing.T) {
	root := testsupport.TempDir(t)
	writeScript(t, filepath.Join(root, ScriptsDirName), "dup.ts", "// outer one\n")
	inner := filepath.Join(root, "inner")
	writeScript(t, filepath.Join(inner, ScriptsDirName), "dup.ts", "// inner one\n")
	writeScript(t, filepath.Join(inner, ScriptsDirName), "only.ts", "// only here\n")
	chdir(t, inner)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	entries, err := discoverScripts()
	if err != nil {
		t.Fatal(err)
	}
	var shadowed, first int
	for _, e := range entries {
		if e.Name == "dup" {
			if e.Shadowed {
				shadowed++
			} else {
				first++
			}
		}
	}
	if first != 1 || shadowed != 1 {
		t.Fatalf("expected one winning and one shadowed dup, got %d/%d in %+v", first, shadowed, entries)
	}
}

func TestScriptSummaryReadsLeadingComment(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"// plain line\nimport x\n":     "plain line",
		"/** block form */\nimport x\n": "block form",
		"/* c style */\n":               "c style",
		"import x from \"y\"\n":         "",
	}
	i := 0
	for body, want := range cases {
		i++
		p := writeScript(t, dir, "s"+string(rune('a'+i))+".ts", body)
		if got := scriptSummary(p); got != want {
			t.Errorf("summary of %q = %q, want %q", body, got, want)
		}
	}
}

func TestConfigMcpxSpellingWinsOverDotMcpx(t *testing.T) {
	root := testsupport.TempDir(t)
	writeScript(t, filepath.Join(root, ".mcpx", "scripts"), "dup.ts", "// legacy\n")
	want := writeScript(t, filepath.Join(root, ".config", "mcpx", "scripts"), "dup.ts", "// preferred\n")
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	got, err := resolveScript("dup")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf(".config/mcpx must win over .mcpx: got %q, want %q", got, want)
	}
}

func TestBothSpellingsAreBothSearched(t *testing.T) {
	root := testsupport.TempDir(t)
	legacy := writeScript(t, filepath.Join(root, ".mcpx", "scripts"), "only-legacy.ts", "// l\n")
	writeScript(t, filepath.Join(root, ".config", "mcpx", "scripts"), "other.ts", "// o\n")
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	got, err := resolveScript("only-legacy")
	if err != nil {
		t.Fatalf("a script under .mcpx must still resolve: %v", err)
	}
	if got != legacy {
		t.Fatalf("got %q, want %q", got, legacy)
	}
}

func TestGeneratedClientIsNotListedAsAScript(t *testing.T) {
	root := testsupport.TempDir(t)
	dir := filepath.Join(root, ".config", "mcpx", "scripts")
	writeScript(t, dir, "real.ts", "// a real script\n")
	writeScript(t, dir, "mcpx-client.ts", "// Generated by mcpx.\n")
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	entries, err := discoverScripts()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "mcpx-client" {
			t.Fatal("the generated client must not appear as a runnable script")
		}
	}
	if len(entries) != 1 || entries[0].Name != "real" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}
