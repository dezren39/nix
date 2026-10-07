package logstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStoreEconomics(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tokens.db")
	ts, err := NewTokenStore(dbPath)
	if err != nil {
		t.Fatalf("failed to open token store: %v", err)
	}
	defer ts.Close()

	rec1 := TokenRecord{
		ID:               "turn-1",
		Timestamp:        time.Now(),
		PromptTokens:     150,
		CompletionTokens: 50,
		ToolsInjected:    3,
		BaselineTokens:   4000,
		InjectedTokens:   600,
	}
	if err := ts.RecordTurn(rec1); err != nil {
		t.Fatalf("failed to record turn 1: %v", err)
	}

	rec2 := TokenRecord{
		ID:               "turn-2",
		Timestamp:        time.Now(),
		PromptTokens:     200,
		CompletionTokens: 80,
		ToolsInjected:    4,
		BaselineTokens:   4000,
		InjectedTokens:   800,
	}
	if err := ts.RecordTurn(rec2); err != nil {
		t.Fatalf("failed to record turn 2: %v", err)
	}

	sum, err := ts.Summary()
	if err != nil {
		t.Fatalf("failed to compute summary: %v", err)
	}

	if sum.TotalTurns != 2 {
		t.Fatalf("expected 2 turns, got %d", sum.TotalTurns)
	}
	// Expected tokens saved: (4000 - 600) + (4000 - 800) = 3400 + 3200 = 6600
	if sum.TotalTokensSaved != 6600 {
		t.Fatalf("expected 6600 tokens saved, got %d", sum.TotalTokensSaved)
	}
	if sum.SavingsPercentage < 80.0 {
		t.Fatalf("expected > 80%% savings, got %f", sum.SavingsPercentage)
	}

	recent, err := ts.Recent(5)
	if err != nil || len(recent) != 2 {
		t.Fatalf("expected 2 recent turns, got %d (err: %v)", len(recent), err)
	}
}
