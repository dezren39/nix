package launcher_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/launcher"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAFileCanDeclareItsPlaceholderInAComment(t *testing.T) {
	// A comment works in any language and cannot affect what the file does
	// at runtime, which makes it the safest of the three spellings.
	dir := t.TempDir()
	p := write(t, dir, "thing.ts", "// @mcpx:placeholder banner\nlog.info(\"hi\");\n")
	defs, err := launcher.LoadDefinitions([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "banner" {
		t.Fatalf("got %+v", defs)
	}
	if strings.Contains(defs[0].Body, "@mcpx:placeholder") {
		t.Error("the declaration should not be part of the body")
	}
	if !strings.Contains(defs[0].Body, `log.info("hi")`) {
		t.Errorf("the body should survive: %q", defs[0].Body)
	}
}

func TestADeclarationCanNameItsParameters(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "wrap.ts", "// @mcpx:placeholder wrap(inner, label)\nrun(@label, () => { @inner });\n")
	defs, err := launcher.LoadDefinitions([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(defs[0].Params, ",") != "inner,label" {
		t.Errorf("got %v", defs[0].Params)
	}
}

func TestAnExportedConstantAlsoDeclares(t *testing.T) {
	// Visible to tooling, unlike a comment.
	dir := t.TempDir()
	p := write(t, dir, "x.ts", "export const MCPX_PLACEHOLDER = \"metrics\";\nsend();\n")
	defs, err := launcher.LoadDefinitions([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if defs[0].Name != "metrics" {
		t.Errorf("got %q", defs[0].Name)
	}
}

func TestTheFilenameDeclaresWhenNothingElseDoes(t *testing.T) {
	// Least ceremony for a directory of small fragments, and unambiguous
	// because a filename is already unique within its directory.
	dir := t.TempDir()
	p := write(t, dir, "report.ts", "emit(summary());\n")
	defs, err := launcher.LoadDefinitions([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if defs[0].Name != "report" {
		t.Errorf("got %q", defs[0].Name)
	}
}

func TestADeclaredPlaceholderIsUsableFromATemplate(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "banner.ts", "// @mcpx:placeholder banner\nBANNER;\n")
	defs, _ := launcher.LoadDefinitions([]string{p})

	fill, err := launcher.Bind(launcher.Fill{launcher.Entry: "E"}, defs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := launcher.Expand(
		launcher.Template{Text: "@banner\n@entry", Name: "t"}, fill,
		launcher.Options{Defs: defs})
	if err != nil {
		t.Fatal(err)
	}
	if got != "BANNER;\nE" {
		t.Errorf("got %q", got)
	}
}

func TestADeclaredPlaceholderReceivesArgumentsByName(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "wrap.ts", "// @mcpx:placeholder wrap(inner)\ntry { @inner } catch (e) { log.error(\"caught\", e as Error); }\n")
	defs, _ := launcher.LoadDefinitions([]string{p})
	fill, _ := launcher.Bind(launcher.Fill{launcher.Entry: "ENTRY"}, defs)

	got, err := launcher.Expand(
		launcher.Template{Text: "@wrap(entry)", Name: "t"}, fill,
		launcher.Options{Defs: defs})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "try { ENTRY }") {
		t.Errorf("the named parameter should bind: %q", got)
	}
}

func TestShadowingABuiltInIsRefused(t *testing.T) {
	// A template that reads correctly and means something else is the worst
	// kind of surprise.
	dir := t.TempDir()
	p := write(t, dir, "x.ts", "// @mcpx:placeholder entry\nNOPE;\n")
	defs, _ := launcher.LoadDefinitions([]string{p})
	if _, err := launcher.Bind(launcher.Fill{}, defs); err == nil {
		t.Fatal("redefining @entry should be refused")
	} else if !strings.Contains(err.Error(), "built in") {
		t.Errorf("the reason should be stated: %v", err)
	}
}

func TestTwoFilesClaimingOneNameIsRefused(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.ts", "// @mcpx:placeholder same\nA;\n")
	b := write(t, dir, "b.ts", "// @mcpx:placeholder same\nB;\n")
	defs, _ := launcher.LoadDefinitions([]string{a, b})
	if _, err := launcher.Bind(launcher.Fill{}, defs); err == nil {
		t.Fatal("two declarations of one name cannot be ordered")
	} else if !strings.Contains(err.Error(), "a.ts") || !strings.Contains(err.Error(), "b.ts") {
		t.Errorf("both files should be named: %v", err)
	}
}
