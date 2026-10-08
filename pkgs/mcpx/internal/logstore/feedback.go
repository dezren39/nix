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

// InteractionRecord represents an input/output turn or tool execution with its trace ID and feedback.
type InteractionRecord struct {
	TraceID     string    `json:"traceId"`
	Timestamp   time.Time `json:"timestamp"`
	SessionID   string    `json:"sessionId,omitempty"`
	Source      string    `json:"source"` // "proxy", "call", "task", etc.
	Input       string    `json:"input"`
	Output      string    `json:"output,omitempty"`
	ToolsUsed   []string  `json:"toolsUsed,omitempty"`
	Score       *float64  `json:"score,omitempty"`       // primary score (e.g. 1.0 = good, 0.0 = bad)
	ScoresJSON  string    `json:"scoresJson,omitempty"`  // multi-dimension representation (e.g. {"relevance": 1.0, "latency": 0.8})
	FeedbackNotes string  `json:"feedbackNotes,omitempty"`
	FeedbackAt  *time.Time `json:"feedbackAt,omitempty"`
}

// InteractionFilter defines parameters for filtering interaction history.
type InteractionFilter struct {
	Query       string // search across input, output, or feedback notes
	Source      string
	SessionID   string
	HasFeedback *bool  // true = has feedback, false = lacks feedback, nil = any
	MinScore    *float64
	MaxScore    *float64
	Limit       int
	Offset      int
	SortBy      string // "timestamp", "score"
	SortDesc    bool
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
			scores_json TEXT,
			feedback_notes TEXT,
			feedback_ts_unix_ms INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_interactions_ts ON interactions(ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_interactions_score ON interactions(score);
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
			tools_used, score, scores_json, feedback_notes, feedback_ts_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(trace_id) DO UPDATE SET
			output_text = COALESCE(excluded.output_text, interactions.output_text),
			tools_used = CASE WHEN excluded.tools_used != '' THEN excluded.tools_used ELSE interactions.tools_used END,
			score = COALESCE(excluded.score, interactions.score),
			scores_json = COALESCE(excluded.scores_json, interactions.scores_json),
			feedback_notes = COALESCE(excluded.feedback_notes, interactions.feedback_notes),
			feedback_ts_unix_ms = COALESCE(excluded.feedback_ts_unix_ms, interactions.feedback_ts_unix_ms)
	`, rec.TraceID, tsMs, rec.SessionID, rec.Source, rec.Input, rec.Output,
		toolsStr, rec.Score, rec.ScoresJSON, rec.FeedbackNotes, fbMs)
	return err
}

// ProvideFeedback updates scores and notes for a specific trace ID.
func (fs *FeedbackStore) ProvideFeedback(traceID string, score float64, scores map[string]float64, notes string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var scoresJSON string
	if len(scores) > 0 {
		b, _ := json.Marshal(scores)
		scoresJSON = string(b)
	}

	nowMs := time.Now().UnixMilli()
	res, err := fs.db.Exec(`
		UPDATE interactions SET
			score = ?,
			scores_json = COALESCE(NULLIF(?, ''), scores_json),
			feedback_notes = ?,
			feedback_ts_unix_ms = ?
		WHERE trace_id = ?
	`, score, scoresJSON, notes, nowMs, traceID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		// If trace record wasn't present beforehand, insert stub
		_, err = fs.db.Exec(`
			INSERT INTO interactions (
				trace_id, ts_unix_ms, source, input_text, score, scores_json, feedback_notes, feedback_ts_unix_ms
			) VALUES (?, ?, 'feedback', '', ?, ?, ?, ?)
		`, traceID, nowMs, score, scoresJSON, notes, nowMs)
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
		       tools_used, score, scores_json, feedback_notes, feedback_ts_unix_ms
		FROM interactions WHERE trace_id = ?
	`, traceID)

	var rec InteractionRecord
	var tsMs int64
	var fbMs sql.NullInt64
	var toolsStr sql.NullString
	var sessID, outText, sJSON, notes sql.NullString
	var sc sql.NullFloat64

	err := row.Scan(&rec.TraceID, &tsMs, &sessID, &rec.Source, &rec.Input, &outText,
		&toolsStr, &sc, &sJSON, &notes, &fbMs)
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
	if filter.SortBy == "score" {
		orderBy = "score"
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
		       tools_used, score, scores_json, feedback_notes, feedback_ts_unix_ms
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
		var sessID, outText, sJSON, notes sql.NullString
		var sc sql.NullFloat64

		if err := rows.Scan(&rec.TraceID, &tsMs, &sessID, &rec.Source, &rec.Input, &outText,
			&toolsStr, &sc, &sJSON, &notes, &fbMs); err == nil {
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

// ToolFeedbackAdjustment computes the feedback multiplier for a given tool based on past interactions.
func (fs *FeedbackStore) ToolFeedbackAdjustment(tool string) float32 {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// Find interactions where tool was used and feedback score was recorded
	likePattern := "%" + tool + "%"
	row := fs.db.QueryRow(`
		SELECT COUNT(*), AVG(score)
		FROM interactions
		WHERE tools_used LIKE ? AND score IS NOT NULL
	`, likePattern)

	var count int
	var avgScore sql.NullFloat64
	if err := row.Scan(&count, &avgScore); err != nil || count == 0 || !avgScore.Valid {
		return 1.0 // neutral
	}

	// Average score scale: 0.0 (bad) -> 1.0 (good)
	// Multiplier range: 0.5 (consistently bad) -> 1.5 (consistently good)
	val := float32(avgScore.Float64)
	return 0.5 + val
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
