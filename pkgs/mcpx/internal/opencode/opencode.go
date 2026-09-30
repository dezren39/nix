// Package opencode reads statistics out of an opencode v1 database.
//
// The database is opencode's, not mcpx's. It is opened read-only and never
// written, because it belongs to a running program that has its own ideas
// about its schema. That constraint shapes everything here: queries are
// defensive, a missing column is a skipped statistic rather than an error,
// and nothing assumes a version.
//
// The reason to read it at all is that the numbers worth knowing about an
// agent's behaviour -- what it cost, how long it waited, which model answered
// -- live there and are pruned over time. mcpx already keeps a durable log of
// its own; folding these in beside it means one place to ask.
package opencode

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB is a read-only handle on an opencode database.
type DB struct {
	db   *sql.DB
	path string
	cols map[string]map[string]bool
}

// DefaultPaths are where an opencode v1 database is looked for, nearest
// first. OPENCODE_DB wins, because that is how a caller says "this one".
func DefaultPaths() []string {
	var out []string
	if p := os.Getenv("OPENCODE_DB"); p != "" {
		out = append(out, p)
	}
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		out = append(out, filepath.Join(data, "opencode", "opencode.db"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".local", "share", "opencode", "opencode.db"))
	}
	return out
}

// Find returns the first database that exists.
func Find(explicit string) (string, error) {
	candidates := DefaultPaths()
	if explicit != "" {
		candidates = []string{explicit}
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("no opencode database found; looked in:\n  %s\n"+
		"name one with --db, or set OPENCODE_DB",
		strings.Join(candidates, "\n  "))
}

// Open opens a database read-only.
//
// immutable=1 is deliberately not used: opencode may be running and writing,
// and immutable would let us read a torn page. mode=ro with WAL gives a
// consistent snapshot of a live database, which is the situation this will
// almost always be in.
func Open(path string) (*DB, error) {
	dsn := "file:" + path + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return &DB{db: db, path: path, cols: map[string]map[string]bool{}}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// Path is the file this handle reads.
func (d *DB) Path() string { return d.path }

// has reports whether a table has a column.
//
// Every statistic checks before it reads. opencode's schema is not mcpx's to
// depend on, so a column that has moved should cost one number, not the whole
// command.
func (d *DB) has(table, column string) bool {
	set, ok := d.cols[table]
	if !ok {
		set = map[string]bool{}
		rows, err := d.db.Query("PRAGMA table_info(" + table + ")")
		if err == nil {
			for rows.Next() {
				var cid int
				var name, typ string
				var notnull, pk int
				var dflt sql.NullString
				if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil {
					set[name] = true
				}
			}
			rows.Close()
		}
		d.cols[table] = set
	}
	return set[column]
}

// Overview is the headline set.
type Overview struct {
	Path        string    `json:"path"`
	SizeBytes   int64     `json:"sizeBytes"`
	Sessions    int64     `json:"sessions"`
	Messages    int64     `json:"messages"`
	Parts       int64     `json:"parts"`
	Projects    int64     `json:"projects"`
	Earliest    time.Time `json:"earliest"`
	Latest      time.Time `json:"latest"`
	TotalCost   float64   `json:"totalCost"`
	TokensIn    int64     `json:"tokensInput"`
	TokensOut   int64     `json:"tokensOutput"`
	TokensThink int64     `json:"tokensReasoning"`
	CacheRead   int64     `json:"tokensCacheRead"`
	CacheWrite  int64     `json:"tokensCacheWrite"`
}

func (d *DB) Overview(since, until time.Time) (*Overview, error) {
	o := &Overview{Path: d.path}
	if st, err := os.Stat(d.path); err == nil {
		o.SizeBytes = st.Size()
	}
	where, args := timeWhere("time_created", since, until)

	d.scalar(&o.Sessions, "SELECT count(*) FROM session"+where, args...)
	d.scalar(&o.Messages, "SELECT count(*) FROM message"+where, args...)
	d.scalar(&o.Parts, "SELECT count(*) FROM part"+where, args...)
	d.scalar(&o.Projects, "SELECT count(DISTINCT project_id) FROM session"+where, args...)

	for col, dst := range map[string]*int64{
		"tokens_input":       &o.TokensIn,
		"tokens_output":      &o.TokensOut,
		"tokens_reasoning":   &o.TokensThink,
		"tokens_cache_read":  &o.CacheRead,
		"tokens_cache_write": &o.CacheWrite,
	} {
		if d.has("session", col) {
			d.scalar(dst, "SELECT COALESCE(SUM("+col+"),0) FROM session"+where, args...)
		}
	}
	if d.has("session", "cost") {
		d.scalarF(&o.TotalCost, "SELECT COALESCE(SUM(cost),0) FROM session"+where, args...)
	}
	var lo, hi sql.NullInt64
	_ = d.db.QueryRow("SELECT MIN(time_created), MAX(time_created) FROM session"+where, args...).
		Scan(&lo, &hi)
	if lo.Valid {
		o.Earliest = time.UnixMilli(lo.Int64)
	}
	if hi.Valid {
		o.Latest = time.UnixMilli(hi.Int64)
	}
	return o, nil
}

// Group is one row of a grouped statistic.
type Group struct {
	Key       string  `json:"key"`
	Sessions  int64   `json:"sessions"`
	Messages  int64   `json:"messages"`
	Cost      float64 `json:"cost"`
	TokensIn  int64   `json:"tokensInput"`
	TokensOut int64   `json:"tokensOutput"`
}

// ByAgent groups sessions by the agent that ran them.
func (d *DB) ByAgent(since, until time.Time, limit int) ([]Group, error) {
	if !d.has("session", "agent") {
		return nil, nil
	}
	return d.group("COALESCE(NULLIF(agent,''),'(none)')", since, until, limit)
}

// ByModel groups by model.
//
// The model column holds a JSON object rather than a name, so the id is
// pulled out in Go. Doing it in SQL would need json_extract, which is present
// in most builds and absent in enough of them to be worth avoiding.
func (d *DB) ByModel(since, until time.Time, limit int) ([]Group, error) {
	if !d.has("session", "model") {
		return nil, nil
	}
	raw, err := d.group("COALESCE(model,'(none)')", since, until, 0)
	if err != nil {
		return nil, err
	}
	merged := map[string]*Group{}
	for _, g := range raw {
		key := modelName(g.Key)
		m, ok := merged[key]
		if !ok {
			m = &Group{Key: key}
			merged[key] = m
		}
		m.Sessions += g.Sessions
		m.Messages += g.Messages
		m.Cost += g.Cost
		m.TokensIn += g.TokensIn
		m.TokensOut += g.TokensOut
	}
	out := make([]Group, 0, len(merged))
	for _, g := range merged {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return trim(out, limit), nil
}

func modelName(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return raw
	}
	var m struct {
		ID       string `json:"id"`
		Provider string `json:"providerID"`
		Variant  string `json:"variant"`
	}
	if json.Unmarshal([]byte(raw), &m) != nil || m.ID == "" {
		return raw
	}
	name := m.ID
	if m.Provider != "" {
		name = m.Provider + "/" + m.ID
	}
	if m.Variant != "" {
		name += ":" + m.Variant
	}
	return name
}

// ByProject groups by directory, which is what a person recognises.
func (d *DB) ByProject(since, until time.Time, limit int) ([]Group, error) {
	col := "project_id"
	if d.has("session", "directory") {
		col = "COALESCE(NULLIF(directory,''),project_id)"
	}
	return d.group(col, since, until, limit)
}

func (d *DB) group(expr string, since, until time.Time, limit int) ([]Group, error) {
	where, args := timeWhere("time_created", since, until)
	cost, tin, tout := "0", "0", "0"
	if d.has("session", "cost") {
		cost = "COALESCE(SUM(cost),0)"
	}
	if d.has("session", "tokens_input") {
		tin = "COALESCE(SUM(tokens_input),0)"
	}
	if d.has("session", "tokens_output") {
		tout = "COALESCE(SUM(tokens_output),0)"
	}
	q := fmt.Sprintf(`SELECT %s AS k, count(*), %s, %s, %s FROM session%s
		GROUP BY k ORDER BY 3 DESC, 2 DESC`, expr, cost, tin, tout, where)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Key, &g.Sessions, &g.Cost, &g.TokensIn, &g.TokensOut); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return trim(out, limit), rows.Err()
}

// Busiest returns the sessions with the most messages.
type Session struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Agent    string    `json:"agent"`
	Model    string    `json:"model"`
	Parent   string    `json:"parent,omitempty"`
	Messages int64     `json:"messages"`
	Cost     float64   `json:"cost"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

func (d *DB) Busiest(since, until time.Time, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 20
	}
	where, args := timeWhere("s.time_created", since, until)
	agent, model, parent := "''", "''", "''"
	if d.has("session", "agent") {
		agent = "COALESCE(s.agent,'')"
	}
	if d.has("session", "model") {
		model = "COALESCE(s.model,'')"
	}
	if d.has("session", "parent_id") {
		parent = "COALESCE(s.parent_id,'')"
	}
	q := fmt.Sprintf(`SELECT s.id, COALESCE(s.title,''), %s, %s, %s,
		(SELECT count(*) FROM message m WHERE m.session_id = s.id),
		COALESCE(s.cost,0), s.time_created, COALESCE(s.time_updated, s.time_created)
		FROM session s%s ORDER BY 6 DESC LIMIT ?`, agent, model, parent, where)
	rows, err := d.db.Query(q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		var created, updated int64
		if err := rows.Scan(&s.ID, &s.Title, &s.Agent, &s.Model, &s.Parent,
			&s.Messages, &s.Cost, &created, &updated); err != nil {
			return nil, err
		}
		s.Model = modelName(s.Model)
		s.Created = time.UnixMilli(created)
		s.Updated = time.UnixMilli(updated)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Activity is a count per hour, for seeing when work happens.
type Bucket struct {
	When     time.Time `json:"when"`
	Sessions int64     `json:"sessions"`
	Messages int64     `json:"messages"`
}

func (d *DB) Activity(since, until time.Time, limit int) ([]Bucket, error) {
	if limit <= 0 {
		limit = 24
	}
	where, args := timeWhere("time_created", since, until)
	// Truncation in SQL rather than Go, so the grouping happens where the
	// rows are and only the buckets cross the boundary.
	q := `SELECT (time_created/3600000)*3600000 AS b, count(*) FROM session` + where +
		` GROUP BY b ORDER BY b DESC LIMIT ?`
	rows, err := d.db.Query(q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bucket
	for rows.Next() {
		var ms, n int64
		if err := rows.Scan(&ms, &n); err != nil {
			return nil, err
		}
		out = append(out, Bucket{When: time.UnixMilli(ms), Sessions: n})
	}
	for i := range out {
		_ = d.db.QueryRow(
			`SELECT count(*) FROM message WHERE time_created >= ? AND time_created < ?`,
			out[i].When.UnixMilli(), out[i].When.Add(time.Hour).UnixMilli()).Scan(&out[i].Messages)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.Before(out[j].When) })
	return out, rows.Err()
}

func timeWhere(col string, since, until time.Time) (string, []any) {
	var parts []string
	var args []any
	if !since.IsZero() {
		parts = append(parts, col+" >= ?")
		args = append(args, since.UnixMilli())
	}
	if !until.IsZero() {
		parts = append(parts, col+" <= ?")
		args = append(args, until.UnixMilli())
	}
	if len(parts) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func (d *DB) scalar(dst *int64, q string, args ...any) {
	var v sql.NullInt64
	if err := d.db.QueryRow(q, args...).Scan(&v); err == nil && v.Valid {
		*dst = v.Int64
	}
}

func (d *DB) scalarF(dst *float64, q string, args ...any) {
	var v sql.NullFloat64
	if err := d.db.QueryRow(q, args...).Scan(&v); err == nil && v.Valid {
		*dst = v.Float64
	}
}

func trim[T any](in []T, limit int) []T {
	if limit > 0 && len(in) > limit {
		return in[:limit]
	}
	return in
}
