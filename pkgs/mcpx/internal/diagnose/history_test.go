package diagnose_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/diagnose"
)

func tool(required ...string) diagnose.Tool {
	return diagnose.Tool{
		Namespace: "demo", Name: "create_issue", Func: "create_issue",
		Shape: diagnose.Shape{
			Props:    map[string]string{"title": "string", "options": "object"},
			Required: required,
		},
	}
}

func TestTheFirstSightingOfACatalogIsNotAChange(t *testing.T) {
	// A daemon starting for the first time would otherwise report every tool
	// it has as newly added, and a diagnostic quoting that would mislead.
	h := diagnose.OpenHistory(filepath.Join(t.TempDir(), "h.json"), 8)
	if ch := h.Observe(time.Now(), []diagnose.Tool{tool("title")}); len(ch) != 0 {
		t.Fatalf("expected nothing, got %+v", ch)
	}
}

func TestAnArgumentBecomingRequiredIsRecordedWithItsDate(t *testing.T) {
	h := diagnose.OpenHistory(filepath.Join(t.TempDir(), "h.json"), 8)
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	h.Observe(first, []diagnose.Tool{tool("title")})

	when := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	changes := h.Observe(when, []diagnose.Tool{tool("options", "title")})
	if len(changes) != 1 {
		t.Fatalf("expected one change, got %+v", changes)
	}
	if changes[0].Kind != diagnose.ChangeArgRequired || changes[0].Field != "options" {
		t.Errorf("change = %+v", changes[0])
	}
	if !changes[0].When.Equal(when) {
		t.Errorf("when = %v", changes[0].When)
	}
}

func TestHistorySurvivesAReopen(t *testing.T) {
	// The point of keeping it on disk: the diagnostic after a restart can
	// still say when the schema moved.
	path := filepath.Join(t.TempDir(), "h.json")
	h := diagnose.OpenHistory(path, 8)
	h.Observe(time.Now().Add(-time.Hour), []diagnose.Tool{tool("title")})
	h.Observe(time.Now(), []diagnose.Tool{tool("options", "title")})
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	again := diagnose.OpenHistory(path, 8)
	if got := again.Changes("demo.create_issue"); len(got) != 1 {
		t.Fatalf("expected the change to survive, got %+v", got)
	}
}

func TestTheChangeListIsBounded(t *testing.T) {
	h := diagnose.OpenHistory(filepath.Join(t.TempDir(), "h.json"), 2)
	now := time.Now()
	h.Observe(now, []diagnose.Tool{tool("title")})
	for i := 0; i < 5; i++ {
		req := []string{"title"}
		if i%2 == 0 {
			req = []string{"options", "title"}
		}
		h.Observe(now.Add(time.Duration(i)*time.Minute), []diagnose.Tool{tool(req...)})
	}
	if got := h.Changes("demo.create_issue"); len(got) > 2 {
		t.Fatalf("the list should be capped at the limit, got %d", len(got))
	}
}

func TestAServerThatFailedToStartIsNotAToolRemoval(t *testing.T) {
	// No tools from a namespace at all means the server did not answer, not
	// that it emptied itself. Recording removals then would produce a
	// diagnostic blaming a schema change for a start failure.
	h := diagnose.OpenHistory(filepath.Join(t.TempDir(), "h.json"), 8)
	h.Observe(time.Now().Add(-time.Hour), []diagnose.Tool{tool("title")})
	if ch := h.Observe(time.Now(), nil); len(ch) != 0 {
		t.Fatalf("expected nothing, got %+v", ch)
	}
}

func TestARemovedToolIsRecordedWhenItsNamespaceStillAnswers(t *testing.T) {
	h := diagnose.OpenHistory(filepath.Join(t.TempDir(), "h.json"), 8)
	other := diagnose.Tool{Namespace: "demo", Name: "list", Func: "list"}
	h.Observe(time.Now().Add(-time.Hour), []diagnose.Tool{tool("title"), other})
	ch := h.Observe(time.Now(), []diagnose.Tool{other})
	if len(ch) != 1 || ch[0].Kind != diagnose.ChangeToolRemoved {
		t.Fatalf("expected a removal, got %+v", ch)
	}
}
