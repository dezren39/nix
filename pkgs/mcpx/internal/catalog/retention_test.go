package catalog

import (
	"fmt"
	"testing"
	"time"
)

func TestWorkingSetRetention(t *testing.T) {
	ws := NewWorkingSet(3, 1*time.Hour, []string{"pinned.always"})

	// Initially, pinned.always is present
	if !ws.Contains("pinned", "always") && !ws.Contains("", "pinned.always") {
		t.Fatalf("expected pinned.always to be present")
	}

	// Add sticky tool 1
	ws.Add("serverA", "tool1", OriginSticky)
	// Add injected tool 2
	ws.Add("serverA", "tool2", OriginInjected)

	// We now have 3 tools: pinned.always, serverA.tool1 (sticky), serverA.tool2 (injected)
	if ws.Count() != 3 {
		t.Fatalf("expected 3 tools, got %d", ws.Count())
	}

	// Add another injected tool 3; ceiling is 3, so an unpinned non-sticky tool must be evicted.
	// serverA.tool2 was added before tool3, so tool2 is evicted!
	// serverA.tool1 is sticky, so it must survive!
	ws.Add("serverA", "tool3", OriginInjected)

	if ws.Count() != 3 {
		t.Fatalf("expected 3 tools, got %d", ws.Count())
	}
	if ws.Contains("serverA", "tool2") {
		t.Fatalf("expected serverA.tool2 to be evicted")
	}
	if !ws.Contains("serverA", "tool1") {
		t.Fatalf("expected sticky tool1 to survive")
	}
	if !ws.Contains("serverA", "tool3") {
		t.Fatalf("expected injected tool3 to be present")
	}

	// Ingest 5 more injected tools
	for i := 4; i < 9; i++ {
		ws.Add("serverA", fmt.Sprintf("tool%d", i), OriginInjected)
	}

	// tool1 is sticky so it should still survive as long as there are injected tools to evict!
	if !ws.Contains("serverA", "tool1") {
		t.Fatalf("expected sticky tool1 to survive against injected tools")
	}
	if !ws.Contains("", "pinned.always") {
		t.Fatalf("expected pinned tool to survive")
	}
}
