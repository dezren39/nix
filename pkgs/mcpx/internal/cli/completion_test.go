package cli

import (
	"strings"
	"testing"
)

// TestZshCompletionCommandsAreQuoted is the regression test for issue #365:
// `mcpx completion zsh | zsh -n` failed with "parse error near `;;'".
// Summaries carry apostrophes, parens and glob brackets, so every entry in
// the _describe commands array must be single-quoted; an unquoted '\'' escape
// is only valid inside quotes.
func TestZshCompletionCommandsAreQuoted(t *testing.T) {
	out, err := Completion("zsh")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(out, "  commands=(\n")
	if start < 0 {
		t.Fatal("commands array not found in zsh completion")
	}
	rest := out[start+len("  commands=(\n"):]
	end := strings.Index(rest, "\n  )\n")
	if end < 0 {
		t.Fatal("commands array not terminated in zsh completion")
	}
	lines := strings.Split(rest[:end], "\n")
	if len(lines) == 0 {
		t.Fatal("commands array is empty")
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "    '") || !strings.HasSuffix(l, "'") {
			t.Errorf("unquoted commands entry: %q", l)
		}
	}
	// At least one summary really does contain an apostrophe, so this test
	// guards the case that broke, not a vacuous shape.
	if !strings.Contains(out, `server'\''s prompts`) {
		t.Error("expected an apostrophe-bearing entry (server's prompts) in the commands array")
	}
}

// TestZshDescribeEscapesColonsAndQuotes pins zshDescribe: the entry is
// single-quoted for the shell, and _describe's own colon separator inside the
// description is backslash-escaped.
func TestZshDescribeEscapesColonsAndQuotes(t *testing.T) {
	got := zshDescribe("list a server's prompts: verbosely")
	want := `list a server'\''s prompts\: verbosely`
	if got != want {
		t.Errorf("zshDescribe = %q, want %q", got, want)
	}
}
