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

	// 1. Provide retrieval-targeted feedback (1.0 = highly relevant tool recommendation)
	retScore := 1.0
	if err := fs.ProvideFeedback(FeedbackParams{
		TraceID:        "trc-12345",
		Score:          1.0,
		Target:         TargetRetrieval,
		RetrievalScore: &retScore,
		Scores:         map[string]float64{"relevance": 1.0},
		Notes:          "exact tool needed for query",
	}); err != nil {
		t.Fatalf("ProvideFeedback retrieval: %v", err)
	}

	// Verify updated feedback
	updated, err := fs.Get("trc-12345")
	if err != nil {
		t.Fatalf("Get after feedback: %v", err)
	}
	if updated.Score == nil || *updated.Score != 1.0 {
		t.Fatalf("expected score 1.0, got %v", updated.Score)
	}
	if updated.Target != TargetRetrieval {
		t.Fatalf("expected target %q, got %q", TargetRetrieval, updated.Target)
	}
	if updated.RetrievalScore == nil || *updated.RetrievalScore != 1.0 {
		t.Fatalf("expected retrieval score 1.0, got %v", updated.RetrievalScore)
	}
	if updated.FeedbackNotes != "exact tool needed for query" {
		t.Fatalf("unexpected feedback notes: %s", updated.FeedbackNotes)
	}

	// Check tool multipliers: retrieval should be boosted (>1.4), execution should be neutral (1.0)
	execMult, retrMult := fs.ToolFeedbackAdjustment("browser:screenshot")
	if retrMult < 1.4 {
		t.Fatalf("expected boosted retrMult > 1.4 for score 1.0, got %f", retrMult)
	}
	if execMult != 1.0 {
		t.Fatalf("expected neutral execMult 1.0 when only retrieval feedback recorded, got %f", execMult)
	}

	// Verify query correlation
	correlated := fs.FindCorrelatedQueries("browser:screenshot", 0.8)
	if len(correlated) != 1 || correlated[0] != "take a screenshot of google.com" {
		t.Fatalf("unexpected correlated queries: %v", correlated)
	}

	// 2. Add second interaction for same tool that failed at execution time (runtime crash)
	rec2 := InteractionRecord{
		TraceID:   "trc-67890",
		Timestamp: time.Now(),
		Source:    "proxy",
		Input:     "take screenshot of page",
		Output:    "error: chrome crashed with SIGSEGV",
		ToolsUsed: []string{"browser:screenshot"},
	}
	if err := fs.RecordInteraction(rec2); err != nil {
		t.Fatalf("RecordInteraction: %v", err)
	}

	// Provide execution-targeted feedback (0.0 = bad runtime crash)
	execScore := 0.0
	if err := fs.ProvideFeedback(FeedbackParams{
		TraceID:        "trc-67890",
		Score:          0.0,
		Target:         TargetExecution,
		ExecutionScore: &execScore,
		Notes:          "tool crashed during execution",
	}); err != nil {
		t.Fatalf("ProvideFeedback execution: %v", err)
	}

	// Verify distinct multipliers:
	// retrMult should still be boosted (~1.5) because semantic match was right
	// execMult should be penalized (0.5) because execution failed
	execMult2, retrMult2 := fs.ToolFeedbackAdjustment("browser:screenshot")
	if retrMult2 < 1.4 {
		t.Fatalf("retrMult should remain high despite execution failure, got %f", retrMult2)
	}
	if execMult2 > 0.6 {
		t.Fatalf("execMult should be penalized (<0.6) for execution crash, got %f", execMult2)
	}

	// Filter by target
	retrievalList, err := fs.List(InteractionFilter{Target: TargetRetrieval})
	if err != nil || len(retrievalList) != 1 {
		t.Fatalf("expected 1 retrieval interaction, got %v (err: %v)", len(retrievalList), err)
	}
	execList, err := fs.List(InteractionFilter{Target: TargetExecution})
	if err != nil || len(execList) != 1 {
		t.Fatalf("expected 1 execution interaction, got %v (err: %v)", len(execList), err)
	}
}
