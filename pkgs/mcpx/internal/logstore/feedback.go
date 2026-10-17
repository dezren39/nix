package logstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	_ "modernc.org/sqlite"
)

// TargetType defines what the feedback is evaluating.
const (
	TargetExecution = "execution" // bad/good tool runtime execution (crashes, errors, slow, wrong params)
	TargetRetrieval = "retrieval" // bad/good search matching from input (irrelevant tool chosen, false positive/negative)
)

// InteractionRecord represents an input/output turn or tool execution with its trace ID and feedback.
type InteractionRecord struct {
	TraceID          string     `json:"traceId"`
	Timestamp        time.Time  `json:"timestamp"`
	SessionID        string     `json:"sessionId,omitempty"`
	Source           string     `json:"source"` // "proxy", "call", "task", etc.
	Input            string     `json:"input"`
	Output           string     `json:"output,omitempty"`
	ToolsUsed        []string   `json:"toolsUsed,omitempty"`
	Score            *float64   `json:"score,omitempty"`            // primary score (e.g. 1.0 = good, 0.0 = bad)
	Target           string     `json:"target,omitempty"`           // "retrieval" vs "execution"
	RetrievalScore   *float64   `json:"retrievalScore,omitempty"`   // score for search / tool match relevance
	ExecutionScore   *float64   `json:"executionScore,omitempty"`   // score for tool runtime behavior
	ScoresJSON       string     `json:"scoresJson,omitempty"`       // multi-dimension representation (e.g. {"relevance": 1.0, "latency": 0.8})
	FeedbackNotes    string     `json:"feedbackNotes,omitempty"`
	FeedbackAt       *time.Time `json:"feedbackAt,omitempty"`
}

// InteractionFilter defines parameters for filtering interaction history.
type InteractionFilter struct {
	Query       string // search across input, output, or feedback notes
	Source      string
	SessionID   string
	Target      string // "retrieval", "execution"
	HasFeedback *bool  // true = has feedback, false = lacks feedback, nil = any
	MinScore    *float64
	MaxScore    *float64
	Limit       int
	Offset      int
	SortBy      string // "timestamp", "score", "retrieval", "execution"
	SortDesc    bool
}

// FeedbackParams carries inputs when providing feedback.
type FeedbackParams struct {
	TraceID        string             `json:"traceId"`
	Score          float64            `json:"score"`
	Target         string             `json:"target,omitempty"` // "retrieval", "execution", or empty (sets primary)
	RetrievalScore *float64           `json:"retrievalScore,omitempty"`
	ExecutionScore *float64           `json:"executionScore,omitempty"`
	Scores         map[string]float64 `json:"scores,omitempty"`
	Notes          string             `json:"notes,omitempty"`
}

// FeedbackStore persists interactions and feedback in SQLite.
type FeedbackStore struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewFeedbackStore opens or creates an SQLite-backed feedback store.
func NewFeedbackStore(path string) (*FeedbackStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS interactions (
			trace_id TEXT PRIMARY KEY,
			ts_unix_ms INTEGER NOT NULL,
			session_id TEXT,
			source TEXT NOT NULL,
			input_text TEXT NOT NULL,
			output_text TEXT,
			tools_used TEXT,
			score REAL,
			target TEXT,
			retrieval_score REAL,
			execution_score REAL,
			scores_json TEXT,
			feedback_notes TEXT,
			feedback_ts_unix_ms INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_interactions_ts ON interactions(ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_interactions_score ON interactions(score);
		CREATE INDEX IF NOT EXISTS idx_interactions_target ON interactions(target);
		CREATE INDEX IF NOT EXISTS idx_interactions_retrieval ON interactions(retrieval_score);
		CREATE INDEX IF NOT EXISTS idx_interactions_execution ON interactions(execution_score);
		CREATE INDEX IF NOT EXISTS idx_interactions_source ON interactions(source);
		CREATE INDEX IF NOT EXISTS idx_interactions_session ON interactions(session_id);
	`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &FeedbackStore{db: db}, nil
}

// RecordInteraction inserts or updates an interaction record.
func (fs *FeedbackStore) RecordInteraction(rec InteractionRecord) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	toolsStr := strings.Join(rec.ToolsUsed, ",")
	tsMs := rec.Timestamp.UnixMilli()
	if tsMs == 0 {
		tsMs = time.Now().UnixMilli()
	}

	var fbMs *int64
	if rec.FeedbackAt != nil {
		m := rec.FeedbackAt.UnixMilli()
		fbMs = &m
	}

	_, err := fs.db.Exec(`
		INSERT INTO interactions (
			trace_id, ts_unix_ms, session_id, source, input_text, output_text,
			tools_used, score, target, retrieval_score, execution_score, scores_json, feedback_notes, feedback_ts_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(trace_id) DO UPDATE SET
			output_text = COALESCE(excluded.output_text, interactions.output_text),
			tools_used = CASE WHEN excluded.tools_used != '' THEN excluded.tools_used ELSE interactions.tools_used END,
			score = COALESCE(excluded.score, interactions.score),
			target = COALESCE(excluded.target, interactions.target),
			retrieval_score = COALESCE(excluded.retrieval_score, interactions.retrieval_score),
			execution_score = COALESCE(excluded.execution_score, interactions.execution_score),
			scores_json = COALESCE(excluded.scores_json, interactions.scores_json),
			feedback_notes = COALESCE(excluded.feedback_notes, interactions.feedback_notes),
			feedback_ts_unix_ms = COALESCE(excluded.feedback_ts_unix_ms, interactions.feedback_ts_unix_ms)
	`, rec.TraceID, tsMs, rec.SessionID, rec.Source, rec.Input, rec.Output,
		toolsStr, rec.Score, rec.Target, rec.RetrievalScore, rec.ExecutionScore, rec.ScoresJSON, rec.FeedbackNotes, fbMs)
	return err
}

// ProvideFeedback updates scores, target distinction, and notes for a specific trace ID.
func (fs *FeedbackStore) ProvideFeedback(params FeedbackParams) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var scoresJSON string
	if len(params.Scores) > 0 {
		b, _ := json.Marshal(params.Scores)
		scoresJSON = string(b)
	}

	nowMs := time.Now().UnixMilli()
	target := params.Target
	if target == "" {
		if params.RetrievalScore != nil && params.ExecutionScore == nil {
			target = TargetRetrieval
		} else if params.ExecutionScore != nil && params.RetrievalScore == nil {
			target = TargetExecution
		}
	}

	retrievalScore := params.RetrievalScore
	executionScore := params.ExecutionScore
	if retrievalScore == nil && target == TargetRetrieval {
		v := params.Score
		retrievalScore = &v
	}
	if executionScore == nil && target == TargetExecution {
		v := params.Score
		executionScore = &v
	}

	res, err := fs.db.Exec(`
		UPDATE interactions SET
			score = ?,
			target = COALESCE(NULLIF(?, ''), target),
			retrieval_score = COALESCE(?, retrieval_score),
			execution_score = COALESCE(?, execution_score),
			scores_json = COALESCE(NULLIF(?, ''), scores_json),
			feedback_notes = ?,
			feedback_ts_unix_ms = ?
		WHERE trace_id = ?
	`, params.Score, target, retrievalScore, executionScore, scoresJSON, params.Notes, nowMs, params.TraceID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		// If trace record wasn't present beforehand, insert stub
		_, err = fs.db.Exec(`
			INSERT INTO interactions (
				trace_id, ts_unix_ms, source, input_text, score, target, retrieval_score, execution_score, scores_json, feedback_notes, feedback_ts_unix_ms
			) VALUES (?, ?, 'feedback', '', ?, ?, ?, ?, ?, ?, ?)
		`, params.TraceID, nowMs, params.Score, target, retrievalScore, executionScore, scoresJSON, params.Notes, nowMs)
		return err
	}
	return nil
}

// Get retrieves a single record by trace ID.
func (fs *FeedbackStore) Get(traceID string) (*InteractionRecord, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	row := fs.db.QueryRow(`
		SELECT trace_id, ts_unix_ms, session_id, source, input_text, output_text,
		       tools_used, score, target, retrieval_score, execution_score,
		       scores_json, feedback_notes, feedback_ts_unix_ms
		FROM interactions WHERE trace_id = ?
	`, traceID)

	var rec InteractionRecord
	var tsMs int64
	var fbMs sql.NullInt64
	var toolsStr sql.NullString
	var sessID, outText, tgt, sJSON, notes sql.NullString
	var sc, rSc, eSc sql.NullFloat64

	err := row.Scan(&rec.TraceID, &tsMs, &sessID, &rec.Source, &rec.Input, &outText,
		&toolsStr, &sc, &tgt, &rSc, &eSc, &sJSON, &notes, &fbMs)
	if err != nil {
		return nil, err
	}

	rec.Timestamp = time.UnixMilli(tsMs)
	if sessID.Valid {
		rec.SessionID = sessID.String
	}
	if outText.Valid {
		rec.Output = outText.String
	}
	if toolsStr.Valid && toolsStr.String != "" {
		rec.ToolsUsed = strings.Split(toolsStr.String, ",")
	}
	if sc.Valid {
		v := sc.Float64
		rec.Score = &v
	}
	if tgt.Valid {
		rec.Target = tgt.String
	}
	if rSc.Valid {
		v := rSc.Float64
		rec.RetrievalScore = &v
	}
	if eSc.Valid {
		v := eSc.Float64
		rec.ExecutionScore = &v
	}
	if sJSON.Valid {
		rec.ScoresJSON = sJSON.String
	}
	if notes.Valid {
		rec.FeedbackNotes = notes.String
	}
	if fbMs.Valid {
		t := time.UnixMilli(fbMs.Int64)
		rec.FeedbackAt = &t
	}
	return &rec, nil
}

// List returns interaction records matching the given filter.
func (fs *FeedbackStore) List(filter InteractionFilter) ([]InteractionRecord, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var where []string
	var args []any

	if filter.Source != "" {
		where = append(where, "source = ?")
		args = append(args, filter.Source)
	}
	if filter.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.Target != "" {
		where = append(where, "target = ?")
		args = append(args, filter.Target)
	}
	if filter.HasFeedback != nil {
		if *filter.HasFeedback {
			where = append(where, "score IS NOT NULL")
		} else {
			where = append(where, "score IS NULL")
		}
	}
	if filter.MinScore != nil {
		where = append(where, "score >= ?")
		args = append(args, *filter.MinScore)
	}
	if filter.MaxScore != nil {
		where = append(where, "score <= ?")
		args = append(args, *filter.MaxScore)
	}
	if filter.Query != "" {
		q := "%" + strings.ToLower(filter.Query) + "%"
		where = append(where, "(LOWER(input_text) LIKE ? OR LOWER(output_text) LIKE ? OR LOWER(feedback_notes) LIKE ? OR LOWER(trace_id) LIKE ?)")
		args = append(args, q, q, q, q)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	orderBy := "ts_unix_ms"
	switch filter.SortBy {
	case "score":
		orderBy = "score"
	case "retrieval":
		orderBy = "retrieval_score"
	case "execution":
		orderBy = "execution_score"
	}

	dir := "DESC"
	if !filter.SortDesc && filter.SortBy != "" {
		dir = "ASC"
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = defaults.RegistryPageSize
	}
	args = append(args, limit)

	offsetClause := ""
	if filter.Offset > 0 {
		offsetClause = "OFFSET ?"
		args = append(args, filter.Offset)
	}

	query := fmt.Sprintf(`
		SELECT trace_id, ts_unix_ms, session_id, source, input_text, output_text,
		       tools_used, score, target, retrieval_score, execution_score,
		       scores_json, feedback_notes, feedback_ts_unix_ms
		FROM interactions
		%s
		ORDER BY %s %s
		LIMIT ? %s
	`, whereClause, orderBy, dir, offsetClause)

	rows, err := fs.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InteractionRecord
	for rows.Next() {
		var rec InteractionRecord
		var tsMs int64
		var fbMs sql.NullInt64
		var toolsStr sql.NullString
		var sessID, outText, tgt, sJSON, notes sql.NullString
		var sc, rSc, eSc sql.NullFloat64

		if err := rows.Scan(&rec.TraceID, &tsMs, &sessID, &rec.Source, &rec.Input, &outText,
			&toolsStr, &sc, &tgt, &rSc, &eSc, &sJSON, &notes, &fbMs); err == nil {
			rec.Timestamp = time.UnixMilli(tsMs)
			if sessID.Valid {
				rec.SessionID = sessID.String
			}
			if outText.Valid {
				rec.Output = outText.String
			}
			if toolsStr.Valid && toolsStr.String != "" {
				rec.ToolsUsed = strings.Split(toolsStr.String, ",")
			}
			if sc.Valid {
				v := sc.Float64
				rec.Score = &v
			}
			if tgt.Valid {
				rec.Target = tgt.String
			}
			if rSc.Valid {
				v := rSc.Float64
				rec.RetrievalScore = &v
			}
			if eSc.Valid {
				v := eSc.Float64
				rec.ExecutionScore = &v
			}
			if sJSON.Valid {
				rec.ScoresJSON = sJSON.String
			}
			if notes.Valid {
				rec.FeedbackNotes = notes.String
			}
			if fbMs.Valid {
				t := time.UnixMilli(fbMs.Int64)
				rec.FeedbackAt = &t
			}
			out = append(out, rec)
		}
	}
	return out, nil
}

// ToolFeedbackAdjustment computes the feedback multipliers for tool execution and retrieval.
// Returns (executionMultiplier, retrievalMultiplier).
func (fs *FeedbackStore) ToolFeedbackAdjustment(tool string) (execMult float32, retrMult float32) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	execMult = 1.0
	retrMult = 1.0

	likePattern := "%" + tool + "%"

	// 1. Tool execution score: how reliably this tool executes
	rowExec := fs.db.QueryRow(`
		SELECT COUNT(*), AVG(COALESCE(execution_score, score))
		FROM interactions
		WHERE tools_used LIKE ? AND (execution_score IS NOT NULL OR target = 'execution' OR (target IS NULL AND score IS NOT NULL))
	`, likePattern)
	var eCount int
	var eAvg sql.NullFloat64
	if err := rowExec.Scan(&eCount, &eAvg); err == nil && eCount > 0 && eAvg.Valid {
		// Multiplier range: 0.5 (bad execution) -> 1.5 (flawless execution)
		execMult = 0.5 + float32(eAvg.Float64)
	}

	// 2. Retrieval relevance score: how relevant this tool is when recommended for queries
	rowRetr := fs.db.QueryRow(`
		SELECT COUNT(*), AVG(COALESCE(retrieval_score, score))
		FROM interactions
		WHERE tools_used LIKE ? AND (retrieval_score IS NOT NULL OR target = 'retrieval')
	`, likePattern)
	var rCount int
	var rAvg sql.NullFloat64
	if err := rowRetr.Scan(&rCount, &rAvg); err == nil && rCount > 0 && rAvg.Valid {
		// Multiplier range: 0.5 (irrelevant recommendation) -> 1.5 (consistently relevant recommendation)
		retrMult = 0.5 + float32(rAvg.Float64)
	}

	return execMult, retrMult
}

// FindCorrelatedQueries returns user queries that resulted in high retrieval feedback for a tool.
func (fs *FeedbackStore) FindCorrelatedQueries(tool string, minScore float64) []string {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	likePattern := "%" + tool + "%"
	rows, err := fs.db.Query(fmt.Sprintf(`
		SELECT input_text
		FROM interactions
		WHERE tools_used LIKE ?
		  AND (retrieval_score >= ? OR (target = 'retrieval' AND score >= ?))
		  AND input_text != ''
		ORDER BY ts_unix_ms DESC
		LIMIT %d
	`, defaults.RegistryLimit), likePattern, minScore, minScore)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var queries []string
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err == nil && strings.TrimSpace(q) != "" {
			queries = append(queries, strings.TrimSpace(q))
		}
	}
	return queries
}

// Close closes the underlying database.
func (fs *FeedbackStore) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.db != nil {
		return fs.db.Close()
	}
	return nil
}
