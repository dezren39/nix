package logstore

import (
	"database/sql"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// TokenRecord represents token economics for a single turn or request.
type TokenRecord struct {
	ID               string    `json:"id"`
	Timestamp        time.Time `json:"timestamp"`
	PromptTokens     int       `json:"promptTokens"`
	CompletionTokens int       `json:"completionTokens"`
	ToolsInjected    int       `json:"toolsInjected"`
	BaselineTokens   int       `json:"baselineTokens"`
	InjectedTokens   int       `json:"injectedTokens"`
	TokensSaved      int       `json:"tokensSaved"`
}

// TokenSummary aggregates token savings across all recorded turns.
type TokenSummary struct {
	TotalTurns            int64   `json:"totalTurns"`
	TotalPromptTokens     int64   `json:"totalPromptTokens"`
	TotalCompletionTokens int64   `json:"totalCompletionTokens"`
	TotalBaselineTokens   int64   `json:"totalBaselineTokens"`
	TotalInjectedTokens   int64   `json:"totalInjectedTokens"`
	TotalTokensSaved      int64   `json:"totalTokensSaved"`
	SavingsPercentage     float64 `json:"savingsPercentage"`
}

// TokenStore records and calculates token economics.
type TokenStore struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewTokenStore opens or creates an SQLite-backed token store.
func NewTokenStore(path string) (*TokenStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS token_telemetry (
			id TEXT PRIMARY KEY,
			ts_unix_ms INTEGER NOT NULL,
			prompt_tokens INTEGER NOT NULL,
			completion_tokens INTEGER NOT NULL,
			tools_injected INTEGER NOT NULL,
			baseline_tokens INTEGER NOT NULL,
			injected_tokens INTEGER NOT NULL,
			tokens_saved INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_token_telemetry_ts ON token_telemetry(ts_unix_ms);
	`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &TokenStore{db: db}, nil
}

// RecordTurn logs a turn's token statistics.
func (ts *TokenStore) RecordTurn(r TokenRecord) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	saved := r.BaselineTokens - r.InjectedTokens
	if saved < 0 {
		saved = 0
	}
	r.TokensSaved = saved
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now()
	}

	_, err := ts.db.Exec(`
		INSERT INTO token_telemetry (
			id, ts_unix_ms, prompt_tokens, completion_tokens,
			tools_injected, baseline_tokens, injected_tokens, tokens_saved
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.Timestamp.UnixMilli(), r.PromptTokens, r.CompletionTokens,
		r.ToolsInjected, r.BaselineTokens, r.InjectedTokens, r.TokensSaved)
	return err
}

// Summary returns the aggregated token economics.
func (ts *TokenStore) Summary() (TokenSummary, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var sum TokenSummary
	row := ts.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0),
			COALESCE(SUM(baseline_tokens), 0),
			COALESCE(SUM(injected_tokens), 0),
			COALESCE(SUM(tokens_saved), 0)
		FROM token_telemetry
	`)
	err := row.Scan(
		&sum.TotalTurns,
		&sum.TotalPromptTokens,
		&sum.TotalCompletionTokens,
		&sum.TotalBaselineTokens,
		&sum.TotalInjectedTokens,
		&sum.TotalTokensSaved,
	)
	if err != nil {
		return sum, err
	}
	if sum.TotalBaselineTokens > 0 {
		sum.SavingsPercentage = float64(sum.TotalTokensSaved) / float64(sum.TotalBaselineTokens) * 100.0
	}
	return sum, nil
}

// Recent returns the last N token records.
func (ts *TokenStore) Recent(limit int) ([]TokenRecord, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	n := limit
	if n <= 0 {
		n = 20
	}
	rows, err := ts.db.Query(`
		SELECT id, ts_unix_ms, prompt_tokens, completion_tokens,
		       tools_injected, baseline_tokens, injected_tokens, tokens_saved
		FROM token_telemetry
		ORDER BY ts_unix_ms DESC
		LIMIT ?
	`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TokenRecord
	for rows.Next() {
		var r TokenRecord
		var ms int64
		if err := rows.Scan(&r.ID, &ms, &r.PromptTokens, &r.CompletionTokens,
			&r.ToolsInjected, &r.BaselineTokens, &r.InjectedTokens, &r.TokensSaved); err == nil {
			r.Timestamp = time.UnixMilli(ms)
			out = append(out, r)
		}
	}
	return out, nil
}

// Close closes the underlying database.
func (ts *TokenStore) Close() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.db != nil {
		return ts.db.Close()
	}
	return nil
}
