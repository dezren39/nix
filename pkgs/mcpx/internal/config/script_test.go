package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

func lines(v ...any) []any { return v }

func TestLayerWithoutNullReplaces(t *testing.T) {
	got := config.ResolveLines([][]any{
		lines("near"),
		lines("far"),
	})
	if len(got) != 1 || got[0] != "near" {
		t.Fatalf("a layer with no null should replace: %v", got)
	}
}

func TestNullSplicesInheritedLines(t *testing.T) {
	got := config.ResolveLines([][]any{
		lines("near", nil),
		lines("far"),
	})
	if strings.Join(got, "|") != "near|far" {
		t.Fatalf("null should splice what was inherited: %v", got)
	}
}

func TestNullPositionDecidesOrder(t *testing.T) {
	got := config.ResolveLines([][]any{
		lines(nil, "near"),
		lines("far"),
	})
	if strings.Join(got, "|") != "far|near" {
		t.Fatalf("inherited lines should land where the null is: %v", got)
	}
}

func TestEmptyLayersAreTransparent(t *testing.T) {
	got := config.ResolveLines([][]any{nil, lines("far"), nil})
	if strings.Join(got, "|") != "far" {
		t.Fatalf("a layer that says nothing should change nothing: %v", got)
	}
}

func TestSeveralLayersCompose(t *testing.T) {
	// three files, each adding to what it inherited
	got := config.ResolveLines([][]any{
		lines("c", nil),
		lines("b", nil),
		lines("a"),
	})
	if strings.Join(got, "|") != "c|b|a" {
		t.Fatalf("layers should compose nearest-first: %v", got)
	}
}

func TestALayerCanDropEverythingInherited(t *testing.T) {
	got := config.ResolveLines([][]any{
		lines("only"),
		lines("b", nil),
		lines("a"),
	})
	if strings.Join(got, "|") != "only" {
		t.Fatalf("omitting null should discard the chain: %v", got)
	}
}

func TestNonStringElementsAreIgnored(t *testing.T) {
	got := config.ResolveLines([][]any{lines("keep", 42, "", true)})
	if strings.Join(got, "|") != "keep" {
		t.Fatalf("only strings and null are meaningful: %v", got)
	}
}

func TestScriptPrefixLayersAcrossConfigFiles(t *testing.T) {
	root := tree(t, map[string]string{
		".config/mcpx/config.json": `{"script":{"prefix":["outer"]},
		  "mcpServers":{"a":{"command":"x"}}}`,
		"proj/.config/mcpx/config.json": `{"script":{"prefix":["inner",null]}}`,
	})
	chdirTo(t, filepath.Join(root, "proj"))

	c, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	got := c.ScriptPrefix(nil)
	if strings.Join(got, "|") != "inner|outer" {
		t.Fatalf("a project should be able to add to a parent's prefix: %v", got)
	}
}

func TestCommandLineIsTheNearestLayer(t *testing.T) {
	root := tree(t, map[string]string{
		".config/mcpx/config.json": `{"script":{"prefix":["configured"]},
		  "mcpServers":{"a":{"command":"x"}}}`,
	})
	chdirTo(t, root)
	c, _ := config.Load("")

	if got := c.ScriptPrefix(lines("flag")); strings.Join(got, "|") != "flag" {
		t.Fatalf("a flag with no null should replace config: %v", got)
	}
	if got := c.ScriptPrefix(lines(nil, "flag")); strings.Join(got, "|") != "configured|flag" {
		t.Fatalf("a flag with null should keep config: %v", got)
	}
}

func TestMissingScriptBlockYieldsNothing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.json")
	os.WriteFile(p, []byte(`{"mcpServers":{"a":{"command":"x"}}}`), 0o644)
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ScriptPrefix(nil); len(got) != 0 {
		t.Fatalf("no script block should mean no lines: %v", got)
	}
}
