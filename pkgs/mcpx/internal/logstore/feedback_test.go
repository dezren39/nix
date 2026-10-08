package logstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFeedbackStoreOperations(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFeedbackStore(filepath.Join(dir, "feedback.db"))
	if err != nil {
		t.Fatalf("NewFeedbackStore: %v", err)
	}
	defer fs.Close()

	rec := InteractionRecord{
		TraceID:   "trc-12345",
		Timestamp: time.Now(),
		Source:    "proxy",
		Input:     "take a screenshot of google.com",
		Output:    "captured screenshot and saved to /tmp/screenshot.png",
		ToolsUsed: []string{"browser:screenshot"},
	}

	if err := fs.RecordInteraction(rec); err != nil {
		t.Fatalf("RecordInteraction: %v", err)
	}

	// Verify retrieval
	got, err := fs.Get("trc-12345")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Input != rec.Input || len(got.ToolsUsed) != 1 || got.ToolsUsed[0] != "browser:screenshot" {
		t.Fatalf("unexpected record: %+v", got)
	}
	if got.Score != nil {
		t.Fatalf("expected nil score before feedback, got %v", *got.Score)
	}

	// List without feedback
	hasFb := false
	unrated, err := fs.List(InteractionFilter{HasFeedback: &hasFb})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(unrated) != 1 {
		t.Fatalf("expected 1 unrated record, got %d", len(unrated))
	}

	// Provide feedback (1.0 = good)
	scores := map[string]float64{"relevance": 1.0, "quality": 0.9}
	if err := fs.ProvideFeedback("trc-12345", 1.0, scores, "perfect screenshot"); err != nil {
		t.Fatalf("ProvideFeedback: %v", err)
	}

	// Verify updated feedback
	updated, err := fs.Get("trc-12345")
	if err != nil {
		t.Fatalf("Get after feedback: %v", err)
	}
	if updated.Score == nil || *updated.Score != 1.0 {
		t.Fatalf("expected score 1.0, got %v", updated.Score)
	}
	if updated.FeedbackNotes != "perfect screenshot" {
		t.Fatalf("unexpected feedback notes: %s", updated.FeedbackNotes)
	}

	// Check tool multiplier
	mult := fs.ToolFeedbackAdjustment("browser:screenshot")
	if mult < 1.4 {
		t.Fatalf("expected boosted multiplier > 1.4 for score 1.0, got %f", mult)
	}
}
