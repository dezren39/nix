package source_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/source"
)

func opts(dir string) source.Options {
	return source.Options{Dir: dir, AllowDir: true, Probe: true}
}

func TestInlineTextIsUsedAsIs(t *testing.T) {
	got, err := source.Resolve(`console.log("hi")`, opts(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != source.KindText || got.Text != `console.log("hi")` {
		t.Errorf("got %+v", got)
	}
}

func TestAValueNamingAnExistingFileIsReadAsOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hook.ts")
	if err := os.WriteFile(path, []byte("export const x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := source.Resolve("hook.ts", opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != source.KindFile {
		t.Fatalf("should have been read as a file: %+v", got)
	}
	if !strings.Contains(got.Text, "export const x") {
		t.Errorf("contents not read: %q", got.Text)
	}
}

func TestProbingCanBeTurnedOff(t *testing.T) {
	// Worth having for anyone whose snippets collide with filenames.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("FILE"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := opts(dir)
	o.Probe = false
	got, _ := source.Resolve("x", o)
	if got.Kind != source.KindText || got.Text != "x" {
		t.Errorf("with probing off the value is literal: %+v", got)
	}
}

func TestExplicitPrefixesOverrideTheProbe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "amb")
	if err := os.WriteFile(path, []byte("FROM FILE"), 0o644); err != nil {
		t.Fatal(err)
	}
	asText, err := source.Resolve("@text:amb", opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if asText.Text != "amb" || !asText.Explicit {
		t.Errorf("@text: should win over the probe: %+v", asText)
	}
	asFile, err := source.Resolve("@file:amb", opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if asFile.Text != "FROM FILE" {
		t.Errorf("@file: should read the file: %+v", asFile)
	}
}

func TestAMissingExplicitFileIsAnErrorRatherThanSilentText(t *testing.T) {
	_, err := source.Resolve("@file:/nope/missing.ts", opts(t.TempDir()))
	if err == nil {
		t.Fatal("naming a file that is not there should fail, not fall back to text")
	}
}

func TestADirectoryIsConcatenatedInNaturalOrder(t *testing.T) {
	// Byte order puts 10 before 9, which is wrong for precisely the case
	// directory concatenation exists to serve.
	dir := t.TempDir()
	for _, n := range []string{"2-b.ts", "10-c.ts", "1-a.ts"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("// "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := source.Resolve(dir, opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != source.KindDir {
		t.Fatalf("should be a directory read: %+v", got)
	}
	var order []string
	for _, f := range got.Files {
		order = append(order, filepath.Base(f))
	}
	want := "1-a.ts,2-b.ts,10-c.ts"
	if strings.Join(order, ",") != want {
		t.Errorf("natural order: got %v, want %s", order, want)
	}
}

func TestEachConcatenatedFileIsNamedInTheOutput(t *testing.T) {
	// A stack trace into a concatenation is otherwise unattributable.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.ts"), []byte("const a = 1;"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.ts"), []byte("const b = 2;"), 0o644)
	got, err := source.Resolve(dir, opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got.Text, "// --- ") != 2 {
		t.Errorf("both files should be attributed:\n%s", got.Text)
	}
}

func TestADirectoryIsRefusedWhereTheSettingDoesNotTakeOne(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.ts"), []byte("x"), 0o644)
	o := opts(dir)
	o.AllowDir = false
	_, err := source.Resolve(dir, o)
	if err == nil {
		t.Fatal("a directory should be refused when the setting does not take one")
	}
	if !strings.Contains(err.Error(), "sourceDirAllowed") {
		t.Errorf("the error should name the switch that changes it: %v", err)
	}
}

func TestShallowByDefaultAndRecursiveWhenAsked(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "top.ts"), []byte("t"), 0o644)
	sub := filepath.Join(dir, "sub")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "deep.ts"), []byte("d"), 0o644)

	shallow, err := source.Resolve(dir, opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(shallow.Files) != 1 {
		t.Errorf("shallow should see only the top file: %v", shallow.Files)
	}
	o := opts(dir)
	o.Recursive = true
	deep, err := source.Resolve(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(deep.Files) != 2 {
		t.Errorf("recursive should see both: %v", deep.Files)
	}
}

func TestResolveAllJoinsInOrder(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "second.ts"), []byte("SECOND"), 0o644)
	got, err := source.ResolveAll([]string{"FIRST", "second.ts", "THIRD"}, opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "FIRST\nSECOND\nTHIRD" {
		t.Errorf("order not preserved: %q", got.Text)
	}
}

func TestNoneMeansNothingAtAll(t *testing.T) {
	got, err := source.Resolve("none", opts(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != source.KindNone || got.Text != "" {
		t.Errorf("none should resolve to nothing: %+v", got)
	}
}

func TestTextAndFileAtTheSameLevelIsAConflict(t *testing.T) {
	if err := source.Conflict("script.prefix", "inline", "/a/file.ts"); err == nil {
		t.Fatal("both forms at one level cannot be ordered, so it should be refused")
	}
	if err := source.Conflict("script.prefix", "inline", ""); err != nil {
		t.Errorf("one form alone is fine: %v", err)
	}
}
