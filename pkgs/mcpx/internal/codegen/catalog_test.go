package codegen_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/codegen"
)

func ns(name string, n int, argBytes int) codegen.Namespace {
	out := codegen.Namespace{Name: name, Server: name, Description: name + " desc"}
	for i := 0; i < n; i++ {
		props := map[string]any{}
		for j := 0; j*8 < argBytes; j++ {
			props[fmt.Sprintf("p%d", j)] = map[string]any{"type": "string"}
		}
		schema, _ := json.Marshal(map[string]any{"type": "object", "properties": props})
		out.Tools = append(out.Tools, codegen.Tool{
			Name:        fmt.Sprintf("%s_tool%02d", name, i),
			Description: "does a thing",
			InputSchema: schema,
		})
	}
	return out
}

func shownCounts(out string) map[string]int {
	counts := map[string]int{}
	var current string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "- ") {
			current = strings.Fields(strings.TrimPrefix(line, "- "))[0]
			counts[current] = 0
		} else if strings.HasPrefix(line, "  - ") && current != "" {
			counts[current]++
		}
	}
	return counts
}

func TestEveryNamespaceIsListedEvenAtAnImpossibleBudget(t *testing.T) {
	out := codegen.Catalog([]codegen.Namespace{ns("alpha", 20, 200), ns("beta", 3, 40)},
		codegen.CatalogOptions{Budget: 1})
	counts := shownCounts(out)
	if len(counts) != 2 {
		t.Fatalf("both namespaces must appear, got %v\n%s", counts, out)
	}
	for name, n := range counts {
		if n != 0 {
			t.Errorf("%s showed %d signatures at budget 1", name, n)
		}
	}
	if !strings.Contains(out, "none shown") {
		t.Errorf("header should say none shown:\n%s", out)
	}
}

func TestLargeNamespaceCannotStarveSmallOnes(t *testing.T) {
	// Without rotation, alpha's twenty tools would consume the budget before
	// beta was reached. This is the property the round-robin exists for.
	out := codegen.Catalog([]codegen.Namespace{ns("alpha", 20, 400), ns("beta", 3, 40)},
		codegen.CatalogOptions{Budget: 400})
	counts := shownCounts(out)
	if counts["beta"] == 0 {
		t.Fatalf("the small namespace was starved: %v\n%s", counts, out)
	}
}

func TestSmallNamespacesAreShownCompletely(t *testing.T) {
	out := codegen.Catalog([]codegen.Namespace{ns("alpha", 30, 300), ns("beta", 2, 30)},
		codegen.CatalogOptions{Budget: 2000})
	counts := shownCounts(out)
	if counts["beta"] != 2 {
		t.Fatalf("beta should be complete, got %d\n%s", counts["beta"], out)
	}
	if !strings.Contains(out, "- beta (2 tools)") {
		t.Errorf("a complete namespace should not report a shown count:\n%s", out)
	}
}

func TestBudgetIsApproximatelyRespected(t *testing.T) {
	for _, budget := range []int{200, 600, 2000} {
		out := codegen.Catalog([]codegen.Namespace{ns("alpha", 40, 300), ns("beta", 40, 120)},
			codegen.CatalogOptions{Budget: budget})
		got := len(out) / 4
		// The header preamble is not budgeted, so allow a fixed slack.
		if got > budget+60 {
			t.Errorf("budget %d produced ~%d tokens", budget, got)
		}
	}
}

func TestMoreBudgetNeverShowsFewerTools(t *testing.T) {
	prev := -1
	for _, budget := range []int{100, 300, 800, 2000, 8000} {
		out := codegen.Catalog([]codegen.Namespace{ns("alpha", 25, 250), ns("beta", 8, 80)},
			codegen.CatalogOptions{Budget: budget})
		total := 0
		for _, n := range shownCounts(out) {
			total += n
		}
		if total < prev {
			t.Fatalf("budget %d showed %d tools, fewer than the previous %d", budget, total, prev)
		}
		prev = total
	}
}

func TestCheaperSignaturesArePreferred(t *testing.T) {
	n := codegen.Namespace{Name: "x", Server: "x"}
	big, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{
		"aaaaaaaaaaaaaaaaaaaa": map[string]any{"type": "string"},
		"bbbbbbbbbbbbbbbbbbbb": map[string]any{"type": "string"},
		"cccccccccccccccccccc": map[string]any{"type": "string"},
	}})
	small, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{}})
	n.Tools = []codegen.Tool{
		{Name: "expensive", InputSchema: big},
		{Name: "cheap", InputSchema: small},
	}
	out := codegen.Catalog([]codegen.Namespace{n}, codegen.CatalogOptions{Budget: 40})
	if !strings.Contains(out, "x.cheap(") {
		t.Fatalf("the cheap signature should win a tight budget:\n%s", out)
	}
}

func TestBiasPromotesMatchingTools(t *testing.T) {
	n := codegen.Namespace{Name: "web", Server: "web"}
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{
		"a": map[string]any{"type": "string"}, "b": map[string]any{"type": "string"},
	}})
	tiny, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{}})
	n.Tools = []codegen.Tool{
		{Name: "aaa_cheap", InputSchema: tiny},
		{Name: "bbb_cheap", InputSchema: tiny},
		{Name: "take_screenshot", InputSchema: schema, Description: "capture the page"},
	}
	// Without bias the two cheap tools win a tight budget.
	plain := codegen.Catalog([]codegen.Namespace{n}, codegen.CatalogOptions{Budget: 30})
	if strings.Contains(plain, "take_screenshot") {
		t.Skip("budget too generous to demonstrate the difference")
	}
	biased := codegen.Catalog([]codegen.Namespace{n},
		codegen.CatalogOptions{Budget: 30, Bias: []string{"screenshot"}})
	if !strings.Contains(biased, "take_screenshot") {
		t.Fatalf("bias should promote the matching tool:\n%s", biased)
	}
}

func TestSignaturesAreSingleLine(t *testing.T) {
	out := codegen.Catalog([]codegen.Namespace{ns("alpha", 5, 200)},
		codegen.CatalogOptions{Budget: 4000})
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  - ") && strings.Contains(line, "\n") {
			t.Fatalf("catalogue entries must be one line: %q", line)
		}
	}
	if strings.Contains(out, "\n    ") {
		t.Errorf("found an indented continuation, signatures are not compact:\n%s", out)
	}
}

func TestEmptyNamespaceIsStillListed(t *testing.T) {
	out := codegen.Catalog([]codegen.Namespace{{Name: "empty", Server: "empty"}},
		codegen.CatalogOptions{Budget: 500})
	if !strings.Contains(out, "- empty (0 tools, none shown)") {
		t.Fatalf("an empty namespace should still be visible:\n%s", out)
	}
}
