package logstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"log/slog"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/logging"
)

// Record is one indexed line, rehydrated far enough to be rendered by the
// logging package's own formatters. Nothing here re-implements rendering: a
// second formatter is a second thing to keep in step with the first.
type Record struct {
	ID       int64
	Time     time.Time
	Level    slog.Level
	Msg      string
	Template string
	Attrs    map[string]any
}

// Logging converts to the record type the renderers take.
func (r Record) Logging() logging.Record {
	return logging.Record{Time: r.Time, Level: r.Level, Msg: r.Msg,
		Template: r.Template, Attrs: r.Attrs}
}

// Query selects records. A zero Query matches everything.
type Query struct {
	Since   time.Time
	Until   time.Time
	Level   string
	Event   string
	Server  string
	Tool    string
	Session string
	Trace   string
	Grep    string
	Limit   int
	Reverse bool
	// AfterID restricts to rows indexed after a known point, which is how
	// --follow avoids re-printing what it has already shown.
	AfterID int64
}

// levelsAtOrAbove expands a threshold into the set of stored names.
//
// The level column holds names rather than numbers because that is what the
// JSONL carries and what a hand-written SQL query wants to compare against.
// The cost is this expansion; the alternative, a parallel numeric column,
// buys ordering nobody has asked for.
func levelsAtOrAbove(name string) ([]string, error) {
	want, err := logging.ParseLevel(name)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if l >= want {
			out = append(out, logging.LevelName(l))
		}
	}
	return out, nil
}

func (q Query) where() (string, []any, error) {
	var conds []string
	var args []any
	add := func(c string, a ...any) {
		conds = append(conds, c)
		args = append(args, a...)
	}
	if !q.Since.IsZero() {
		add(`ts_unix_ms >= ?`, q.Since.UnixMilli())
	}
	if !q.Until.IsZero() {
		add(`ts_unix_ms <= ?`, q.Until.UnixMilli())
	}
	if q.Level != "" {
		names, err := levelsAtOrAbove(q.Level)
		if err != nil {
			return "", nil, err
		}
		holders := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
		conds = append(conds, `level IN (`+holders+`)`)
		for _, n := range names {
			args = append(args, n)
		}
	}
	if q.Event != "" {
		// GLOB rather than LIKE: the pattern language a user expects from
		// `server.*` is shell globbing, and LIKE would read the dot literally
		// and the star as a plain character.
		add(`event GLOB ?`, q.Event)
	}
	if q.Server != "" {
		add(`server = ?`, q.Server)
	}
	if q.Tool != "" {
		add(`tool = ?`, q.Tool)
	}
	if q.Session != "" {
		add(`session = ?`, q.Session)
	}
	if q.Trace != "" {
		add(`trace = ?`, q.Trace)
	}
	if q.Grep != "" {
		// event too. It is promoted out of attrs into its own column, so
		// matching only msg and attrs meant `--grep server.start` found
		// nothing while `--event server.start` found everything.
		add(`(ifnull(msg,'') REGEXP ? OR ifnull(attrs,'') REGEXP ? OR ifnull(event,'') REGEXP ?)`,
			q.Grep, q.Grep, q.Grep)
	}
	if q.AfterID > 0 {
		add(`id > ?`, q.AfterID)
	}
	if len(conds) == 0 {
		return "", nil, nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args, nil
}

const selectCols = `id, ts, level, msg, template, event, trace, parent, session,
  server, tool, instance, duration_ms, ok, error, source_file, source_line,
  source_function, cwd, pid, host, user, attrs, file, "offset"`

// Records runs a query.
//
// The limit is applied to the newest rows and the result is then flipped back
// into chronological order, because "the last 100 lines" means the last
// hundred read forwards, not the first hundred of the whole log.
func (s *Store) Records(q Query) ([]Record, error) {
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaults.LogQueryLimit
	}
	sqlText := `SELECT ` + selectCols + ` FROM records` + where +
		` ORDER BY ts_unix_ms DESC, id DESC LIMIT ?`
	rows, err := s.db.Query(sqlText, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	out, err := scanRecords(rows)
	if err != nil {
		return nil, err
	}
	if !q.Reverse {
		reverse(out)
	}
	return out, nil
}

func reverse(rs []Record) {
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
}

func scanRecords(rows *sql.Rows) ([]Record, error) {
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var (
			id                                  int64
			ts, level                           string
			msg, template, event, trace, parent sql.NullString
			session, server, tool, instance     sql.NullString
			duration                            sql.NullFloat64
			ok                                  sql.NullInt64
			errText, srcFile, srcFunc, cwd      sql.NullString
			srcLine, pid                        sql.NullInt64
			host, user, attrs, file             sql.NullString
			offset                              int64
		)
		if err := rows.Scan(&id, &ts, &level, &msg, &template, &event, &trace,
			&parent, &session, &server, &tool, &instance, &duration, &ok,
			&errText, &srcFile, &srcLine, &srcFunc, &cwd, &pid, &host, &user,
			&attrs, &file, &offset); err != nil {
			return nil, err
		}
		lvl, _ := logging.ParseLevel(level)
		t, _ := time.Parse(time.RFC3339Nano, ts)
		r := Record{ID: id, Time: t, Level: lvl, Msg: msg.String,
			Template: template.String, Attrs: map[string]any{}}
		if attrs.Valid && attrs.String != "" {
			_ = unmarshalInto(attrs.String, r.Attrs)
		}
		put := func(k string, v sql.NullString) {
			if v.Valid && v.String != "" {
				r.Attrs[k] = v.String
			}
		}
		put("event", event)
		put("trace", trace)
		put("trace.parent", parent)
		put("session", session)
		put("server", server)
		put("tool", tool)
		put("instance", instance)
		put("error", errText)
		put("source.file", srcFile)
		put("source.function", srcFunc)
		put("cwd", cwd)
		put("host.name", host)
		put("user.name", user)
		if duration.Valid {
			r.Attrs["durationMs"] = duration.Float64
		}
		if ok.Valid {
			r.Attrs["ok"] = ok.Int64 == 1
		}
		if srcLine.Valid {
			r.Attrs["source.line"] = srcLine.Int64
		}
		if pid.Valid {
			r.Attrs["pid"] = pid.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ChainLevel is one trace in an ancestry, with the records that belong to it.
type ChainLevel struct {
	Trace   string   `json:"trace"`
	Parent  string   `json:"parent,omitempty"`
	Depth   int      `json:"depth"`
	Records []Record `json:"-"`
}

// Chain returns a trace and every ancestor, oldest first.
//
// Only the record that creates a trace carries its parent, so the walk asks
// for exactly that record at each step rather than reading ancestry off the
// record the user started from. The seen set is not paranoia: a log can
// contain anything, and a cycle would otherwise hang the CLI rather than
// print a wrong tree.
func (s *Store) Chain(id string, limitPerLevel int) ([]ChainLevel, error) {
	var ids []string
	seen := map[string]bool{}
	for cur := id; cur != "" && !seen[cur]; {
		seen[cur] = true
		ids = append(ids, cur)
		var parent sql.NullString
		err := s.db.QueryRow(
			`SELECT parent FROM records WHERE trace = ? AND parent IS NOT NULL
			 AND parent <> '' ORDER BY ts_unix_ms ASC, id ASC LIMIT 1`, cur).Scan(&parent)
		if err != nil || !parent.Valid {
			break
		}
		cur = parent.String
	}

	out := make([]ChainLevel, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		depth := len(ids) - 1 - i
		recs, err := s.Records(Query{Trace: ids[i], Limit: limitPerLevel})
		if err != nil {
			return nil, err
		}
		lvl := ChainLevel{Trace: ids[i], Depth: depth, Records: recs}
		if i+1 < len(ids) {
			lvl.Parent = ids[i+1]
		}
		out = append(out, lvl)
	}
	return out, nil
}

func unmarshalInto(s string, dst map[string]any) error {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return err
	}
	for k, v := range m {
		dst[k] = v
	}
	return nil
}

// ParseWhen accepts a duration before now, or an absolute time.
func ParseWhen(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d > 0 {
			d = -d
		}
		return now.Add(d), nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339,
		"2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot read %q as a duration (15m, 2h) or a time (RFC3339)", s)
}
