package recipes_test

import (
	"testing"

	"github.com/dezren39/mcpx/internal/recipes"
)

func corpus() []recipes.Recipe {
	return []recipes.Recipe{
		recipes.Parse("close-stale", "/a/close-stale.ts",
			"// Close stale issues in a repository.\n"+
				"// @param repo:string which repository\n"+
				"await demo.close_issue({ repo: @repo });\n"),
		recipes.Parse("screenshot", "/a/screenshot.ts",
			"// Take a screenshot of a page.\n"+
				"// @param url:string the address\n"+
				"await chrome.navigate({ url: @url }); await chrome.screenshot({});\n"),
	}
}

func TestAMatchIsOverNameDescriptionParametersAndTools(t *testing.T) {
	got := recipes.Match(corpus(), "take a screenshot of the checkout page", 5)
	if len(got) == 0 || got[0].Recipe.Name != "screenshot" {
		t.Fatalf("expected screenshot first, got %+v", got)
	}
	if len(got[0].Why) == 0 {
		t.Error("a ranking nobody can inspect is a ranking nobody can correct")
	}
}

func TestTheSameRequestAlwaysGivesTheSameAnswer(t *testing.T) {
	// The point of matching deterministically: a repeated request costs
	// nothing and cannot drift.
	first := recipes.Match(corpus(), "close stale issues", 5)
	for i := 0; i < 5; i++ {
		again := recipes.Match(corpus(), "close stale issues", 5)
		if len(again) != len(first) || again[0].Recipe.Name != first[0].Recipe.Name ||
			again[0].Score != first[0].Score {
			t.Fatalf("run %d disagreed: %+v vs %+v", i, again, first)
		}
	}
}

func TestNothingRelevantScoresNothing(t *testing.T) {
	if got := recipes.Match(corpus(), "rotate the database credentials", 5); len(got) != 0 {
		t.Fatalf("expected no matches, got %+v", got)
	}
}

func TestDecideRefusesWhenTheLeadIsTooNarrow(t *testing.T) {
	// Running the wrong saved script is a side effect, not a wrong answer.
	cands := []recipes.Candidate{
		{Recipe: recipes.Recipe{Name: "a"}, Score: 50},
		{Recipe: recipes.Recipe{Name: "b"}, Score: 40},
	}
	if _, ok := recipes.Decide(cands, 30, 150); ok {
		t.Fatal("50 against 40 is not a clear winner")
	}
	cands[0].Score = 100
	if _, ok := recipes.Decide(cands, 30, 150); !ok {
		t.Fatal("100 against 40 is")
	}
}

func TestDecideRefusesBelowTheFloor(t *testing.T) {
	cands := []recipes.Candidate{{Recipe: recipes.Recipe{Name: "a"}, Score: 8}}
	if _, ok := recipes.Decide(cands, 30, 150); ok {
		t.Fatal("a weak single candidate is not a match")
	}
}
