package recipes_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/recipes"
)

const stale = `// Close stale issues in a repository.
// @param repo:string        which repository, as owner/name
// @param days:number = 30   how old counts as stale
// @param dryRun:boolean?    report without closing

const found = await demo.search({ repo: @repo, olderThanDays: @days });
if (!@dryRun) await demo.close({ ids: found });
`

func parse(t *testing.T) recipes.Recipe {
	t.Helper()
	return recipes.Parse("close-stale", "/tmp/close-stale.ts", stale)
}

func TestADeclarationBecomesATypedPlaceholder(t *testing.T) {
	r := parse(t)
	if r.Summary != "Close stale issues in a repository." {
		t.Errorf("summary = %q", r.Summary)
	}
	byName := map[string]recipes.Placeholder{}
	for _, p := range r.Placeholders {
		byName[p.Name] = p
	}
	if len(byName) != 3 {
		t.Fatalf("expected three placeholders, got %+v", r.Placeholders)
	}
	if p := byName["repo"]; p.Type != recipes.TypeString || !p.Required ||
		p.Description != "which repository, as owner/name" {
		t.Errorf("repo = %+v", p)
	}
	if p := byName["days"]; p.Type != recipes.TypeNumber || p.Required ||
		p.Default == nil || *p.Default != "30" {
		t.Errorf("days = %+v", p)
	}
	if p := byName["dryRun"]; p.Type != recipes.TypeBoolean || p.Required {
		t.Errorf("dryRun = %+v", p)
	}
}

func TestTheToolsARecipeCallsAreRecorded(t *testing.T) {
	// Matching free text to a recipe leans on this: somebody describing a
	// task uses the words the tools are named with.
	r := parse(t)
	if strings.Join(r.Tools, ",") != "demo.close,demo.search" {
		t.Errorf("tools = %v", r.Tools)
	}
}

func TestValuesAreSubstitutedAsJSONNotAsText(t *testing.T) {
	// A recipe runs with the user's credentials. Pasting raw text in would
	// make every placeholder an injection site.
	r := parse(t)
	out, err := r.Render(map[string]any{"repo": `me/thing"); evil(("`, "dryRun": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `evil((`) && !strings.Contains(out, `\"`) {
		t.Fatalf("the value was not encoded:\n%s", out)
	}
	if !strings.Contains(out, "olderThanDays: 30") {
		t.Errorf("the default should be used when nothing was given:\n%s", out)
	}
	if !strings.Contains(out, "if (!true)") {
		t.Errorf("the boolean should be a boolean:\n%s", out)
	}
	if strings.Contains(out, "@repo") {
		t.Errorf("a placeholder survived:\n%s", out)
	}
}

func TestRenderRefusesWhenSomethingRequiredIsMissing(t *testing.T) {
	if _, err := parse(t).Render(nil); err == nil {
		t.Fatal("a recipe with an unfilled required hole must not render")
	}
}

func TestMissingListsOnlyWhatCannotBeDefaulted(t *testing.T) {
	missing := parse(t).Missing(nil)
	if len(missing) != 1 || missing[0].Name != "repo" {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestTheFormCarriesDescriptionsAndDefaults(t *testing.T) {
	// Every question needs an answer for the case where nobody answers, and
	// the schema is where an answerer can see what that would be.
	schema := recipes.FormSchema(parse(t).Placeholders)
	var doc struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Properties["days"]["default"] != float64(30) {
		t.Errorf("days default = %v", doc.Properties["days"]["default"])
	}
	if doc.Properties["repo"]["description"] == nil {
		t.Error("the description should reach whoever answers")
	}
	if len(doc.Required) != 1 || doc.Required[0] != "repo" {
		t.Errorf("required = %v", doc.Required)
	}
}

func TestAnUndeclaredReferenceIsStillAPlaceholder(t *testing.T) {
	r := recipes.Parse("x", "/tmp/x.ts", "await demo.echo({ message: @who });")
	if len(r.Placeholders) != 1 || r.Placeholders[0].Name != "who" {
		t.Fatalf("placeholders = %+v", r.Placeholders)
	}
}

func TestAnEmailAddressIsNotAPlaceholder(t *testing.T) {
	r := recipes.Parse("x", "/tmp/x.ts", `const to = "someone@example.com";`)
	if len(r.Placeholders) != 0 {
		t.Fatalf("placeholders = %+v", r.Placeholders)
	}
}
