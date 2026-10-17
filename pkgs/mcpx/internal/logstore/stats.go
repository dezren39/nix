package logstore

import (
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
)

// Dimensions are the aggregates `mcpx stats` can produce.
var Dimensions = []string{"calls", "servers", "instances", "errors", "sessions", "volume", "slowest"}

// percentile returns an exact percentile from an already sorted slice.
//
// Nearest-rank rather than interpolation, and exact rather than a sketch:
// these datasets are thousands of rows, not billions, so an approximation
// would trade the only property that matters -- that the number printed is a
// call that really took that long -- for a saving nobody would notice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// CallStat is one server+tool pair's call history.
type CallStat struct {
	Server  string  `json:"server"`
	Tool    string  `json:"tool"`
	Calls   int     `json:"calls"`
	Errors  int     `json:"errors"`
	ErrRate float64 `json:"errorRate"`
	P50     float64 `json:"p50Ms"`
	P95     float64 `json:"p95Ms"`
	P99     float64 `json:"p99Ms"`
	Max     float64 `json:"maxMs"`
	TotalMs float64 `json:"totalMs"`
}

// Calls aggregates tool calls by server and tool.
func (s *Store) Calls(q Query) ([]CallStat, error) {
	q.Event = firstNonEmpty(q.Event, "mcp.call")
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT ifnull(server,''), ifnull(tool,''),
		ifnull(duration_ms,-1), ok FROM records`+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type key struct{ server, tool string }
	durations := map[key][]float64{}
	counts := map[key]*CallStat{}
	for rows.Next() {
		var k key
		var dur float64
		var ok sql.NullInt64
		if err := rows.Scan(&k.server, &k.tool, &dur, &ok); err != nil {
			return nil, err
		}
		st := counts[k]
		if st == nil {
			st = &CallStat{Server: k.server, Tool: k.tool}
			counts[k] = st
		}
		st.Calls++
		if ok.Valid && ok.Int64 == 0 {
			st.Errors++
		}
		if dur >= 0 {
			durations[k] = append(durations[k], dur)
			st.TotalMs += dur
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]CallStat, 0, len(counts))
	for k, st := range counts {
		d := durations[k]
		sort.Float64s(d)
		st.P50, st.P95, st.P99 = percentile(d, 50), percentile(d, 95), percentile(d, 99)
		if len(d) > 0 {
			st.Max = d[len(d)-1]
		}
		if st.Calls > 0 {
			st.ErrRate = float64(st.Errors) / float64(st.Calls)
		}
		out = append(out, *st)
	}
	// Ordered by time spent, because the question behind this table is almost
	// always "what is costing me", and a rare tool that takes ten seconds
	// matters more than a fast one called constantly.
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalMs != out[j].TotalMs {
			return out[i].TotalMs > out[j].TotalMs
		}
		return out[i].Server+out[i].Tool < out[j].Server+out[j].Tool
	})
	return out, nil
}

// ServerStat summarises one server's process lifecycle.
type ServerStat struct {
	Server    string  `json:"server"`
	Starts    int     `json:"starts"`
	Stops     int     `json:"stops"`
	Restarts  int     `json:"restarts"`
	Unclean   int     `json:"unclean"`
	Running   int     `json:"running"`
	UptimeSec int64   `json:"uptimeSec"`
	ReadyP50  float64 `json:"readyP50Ms"`
	ReadyMax  float64 `json:"readyMaxMs"`
	Calls     int     `json:"calls"`
}

// orderly are the stop reasons the pool produces on purpose. Anything else is
// a process that went away without being asked to, which is the number worth
// looking at.
var orderly = map[string]bool{
	"released": true, "idle": true, "restart": true, "shutdown": true, "stopped": true,
}

// Servers summarises starts, stops and readiness per server.
func (s *Store) Servers(q Query) ([]ServerStat, error) {
	q.Event = firstNonEmpty(q.Event, "server.*")
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT ifnull(server,''), ifnull(event,''),
		ifnull(attrs,'') FROM records`+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := map[string]*ServerStat{}
	ready := map[string][]float64{}
	for rows.Next() {
		var server, event, attrs string
		if err := rows.Scan(&server, &event, &attrs); err != nil {
			return nil, err
		}
		st := stats[server]
		if st == nil {
			st = &ServerStat{Server: server}
			stats[server] = st
		}
		extra := map[string]any{}
		if attrs != "" {
			_ = json.Unmarshal([]byte(attrs), &extra)
		}
		switch event {
		case "server.start":
			st.Starts++
			if v, ok := num(extra["readyMs"]); ok {
				ready[server] = append(ready[server], v)
			}
		case "server.stop":
			st.Stops++
			reason, _ := extra["reason"].(string)
			if reason == "restart" {
				st.Restarts++
			}
			if !orderly[reason] {
				st.Unclean++
			}
			if v, ok := num(extra["uptimeSec"]); ok {
				st.UptimeSec += int64(v)
			}
			if v, ok := num(extra["calls"]); ok {
				st.Calls += int(v)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]ServerStat, 0, len(stats))
	for name, st := range stats {
		d := ready[name]
		sort.Float64s(d)
		st.ReadyP50 = percentile(d, 50)
		if len(d) > 0 {
			st.ReadyMax = d[len(d)-1]
		}
		st.Running = st.Starts - st.Stops
		if st.Running < 0 {
			st.Running = 0
		}
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Server < out[j].Server })
	return out, nil
}

// InstanceStat is one server process.
type InstanceStat struct {
	Server    string    `json:"server"`
	Instance  string    `json:"instance"`
	PID       int64     `json:"pid"`
	Started   time.Time `json:"started"`
	Stopped   time.Time `json:"stopped,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	UptimeSec int64     `json:"uptimeSec"`
	Calls     int       `json:"calls"`
	ReadyMs   float64   `json:"readyMs"`
}

// Instances lists individual processes.
//
// Distinct from Servers because an aggregate cannot tell you *which* process
// died: when a server restarts six times an hour, the row you want is the one
// with the odd stop reason, and that only exists per instance.
func (s *Store) Instances(q Query) ([]InstanceStat, error) {
	q.Event = firstNonEmpty(q.Event, "server.*")
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT ifnull(server,''), ifnull(instance,''),
		ifnull(pid,0), ts, ifnull(event,''), ifnull(attrs,'') FROM records`+where+
		` ORDER BY ts_unix_ms ASC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[string]*InstanceStat{}
	var order []string
	for rows.Next() {
		var server, instance, ts, event, attrs string
		var pid int64
		if err := rows.Scan(&server, &instance, &pid, &ts, &event, &attrs); err != nil {
			return nil, err
		}
		if instance == "" {
			continue
		}
		st := byID[instance]
		if st == nil {
			st = &InstanceStat{Server: server, Instance: instance, PID: pid}
			byID[instance] = st
			order = append(order, instance)
		}
		extra := map[string]any{}
		if attrs != "" {
			_ = json.Unmarshal([]byte(attrs), &extra)
		}
		t, _ := time.Parse(time.RFC3339Nano, ts)
		switch event {
		case "server.start":
			st.Started = t
			if v, ok := num(extra["readyMs"]); ok {
				st.ReadyMs = v
			}
		case "server.stop":
			st.Stopped = t
			st.Reason, _ = extra["reason"].(string)
			if v, ok := num(extra["uptimeSec"]); ok {
				st.UptimeSec = int64(v)
			}
			if v, ok := num(extra["calls"]); ok {
				st.Calls = int(v)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]InstanceStat, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// ErrorStat is one recurring failure.
type ErrorStat struct {
	Message  string    `json:"message"`
	Count    int       `json:"count"`
	Servers  string    `json:"servers,omitempty"`
	LastSeen time.Time `json:"lastSeen"`
}

// Errors groups failures by their template where one exists, and by message
// otherwise.
//
// Grouping on the template is the whole point: "call timed out after 120s on
// chrome-devtools" with six different servers in it is six rows by message and
// one row by template, and one row is the answer.
func (s *Store) Errors(q Query) ([]ErrorStat, error) {
	if q.Level == "" {
		q.Level = "error"
	}
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT
		coalesce(nullif(template,''), nullif(error,''), ifnull(msg,'')) AS shape,
		count(*), max(ts_unix_ms), group_concat(DISTINCT server)
		FROM records`+where+` GROUP BY shape ORDER BY count(*) DESC, max(ts_unix_ms) DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ErrorStat
	for rows.Next() {
		var e ErrorStat
		var lastMs int64
		var servers sql.NullString
		if err := rows.Scan(&e.Message, &e.Count, &lastMs, &servers); err != nil {
			return nil, err
		}
		e.LastSeen = time.UnixMilli(lastMs)
		e.Servers = servers.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// SessionStat is one script run or agent session.
type SessionStat struct {
	Session  string    `json:"session"`
	Calls    int       `json:"calls"`
	Errors   int       `json:"errors"`
	Servers  string    `json:"servers"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	SpanSec  float64   `json:"spanSec"`
	BusyMs   float64   `json:"busyMs"`
	Records  int       `json:"records"`
	Distinct int       `json:"distinctServers"`
}

// Sessions reports what each session did.
func (s *Store) Sessions(q Query) ([]SessionStat, error) {
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	if where == "" {
		where = " WHERE session IS NOT NULL AND session <> ''"
	} else {
		where += " AND session IS NOT NULL AND session <> ''"
	}
	rows, err := s.db.Query(`SELECT session,
		count(*),
		sum(CASE WHEN event='mcp.call' THEN 1 ELSE 0 END),
		sum(CASE WHEN ok=0 THEN 1 ELSE 0 END),
		min(ts), max(ts), min(ts_unix_ms), max(ts_unix_ms),
		ifnull(sum(duration_ms),0),
		group_concat(DISTINCT server)
		FROM records`+where+` GROUP BY session ORDER BY max(ts_unix_ms) DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionStat
	for rows.Next() {
		var st SessionStat
		var firstTS, lastTS string
		var firstMs, lastMs int64
		var servers sql.NullString
		if err := rows.Scan(&st.Session, &st.Records, &st.Calls, &st.Errors,
			&firstTS, &lastTS, &firstMs, &lastMs, &st.BusyMs, &servers); err != nil {
			return nil, err
		}
		st.First, _ = time.Parse(time.RFC3339Nano, firstTS)
		st.Last, _ = time.Parse(time.RFC3339Nano, lastTS)
		st.SpanSec = float64(lastMs-firstMs) / 1000
		st.Servers = servers.String
		if st.Servers != "" {
			st.Distinct = strings.Count(st.Servers, ",") + 1
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// VolumeBucket is one hour of log.
type VolumeBucket struct {
	Hour  string `json:"hour"`
	Debug int    `json:"debug"`
	Info  int    `json:"info"`
	Warn  int    `json:"warn"`
	Error int    `json:"error"`
	Total int    `json:"total"`
}

// FileStat is one JSONL file on disk and how much of it is indexed.
type FileStat struct {
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	Records int64  `json:"records"`
	Skipped int64  `json:"skipped"`
}

// Volume reports records per level per hour, and what the logs cost on disk.
func (s *Store) Volume(q Query) ([]VolumeBucket, []FileStat, error) {
	where, args, err := q.where()
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.db.Query(`SELECT substr(ts,1,13), level, count(*) FROM records`+
		where+` GROUP BY 1, 2 ORDER BY 1 ASC`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var buckets []VolumeBucket
	index := map[string]int{}
	for rows.Next() {
		var hour, level string
		var n int
		if err := rows.Scan(&hour, &level, &n); err != nil {
			return nil, nil, err
		}
		i, ok := index[hour]
		if !ok {
			buckets = append(buckets, VolumeBucket{Hour: hour})
			i = len(buckets) - 1
			index[hour] = i
		}
		switch level {
		case "debug":
			buckets[i].Debug += n
		case "warn":
			buckets[i].Warn += n
		case "error":
			buckets[i].Error += n
		default:
			buckets[i].Info += n
		}
		buckets[i].Total += n
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	frows, err := s.db.Query(`SELECT path, size, records, skipped FROM files ORDER BY path`)
	if err != nil {
		return nil, nil, err
	}
	defer frows.Close()
	var files []FileStat
	for frows.Next() {
		var f FileStat
		if err := frows.Scan(&f.Path, &f.Bytes, &f.Records, &f.Skipped); err != nil {
			return nil, nil, err
		}
		files = append(files, f)
	}
	return buckets, files, frows.Err()
}

// SlowCall is one individual call, kept with its trace so the next command is
// obvious: `mcpx log --chain <trace>`.
type SlowCall struct {
	Time     time.Time `json:"ts"`
	Server   string    `json:"server"`
	Tool     string    `json:"tool"`
	Duration float64   `json:"durationMs"`
	OK       bool      `json:"ok"`
	Session  string    `json:"session"`
	Trace    string    `json:"trace"`
}

// Slowest returns the n slowest calls.
func (s *Store) Slowest(q Query, n int) ([]SlowCall, error) {
	q.Event = firstNonEmpty(q.Event, "mcp.call")
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	if n <= 0 {
		n = 20
	}
	if where == "" {
		where = " WHERE 1=1"
	}
	rows, err := s.db.Query(`SELECT ts, ifnull(server,''), ifnull(tool,''),
		ifnull(duration_ms,0), ifnull(ok,1), ifnull(session,''), ifnull(trace,'')
		FROM records`+where+` AND duration_ms IS NOT NULL
		ORDER BY duration_ms DESC LIMIT ?`, append(args, n)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SlowCall
	for rows.Next() {
		var c SlowCall
		var ts string
		var ok int64
		if err := rows.Scan(&ts, &c.Server, &c.Tool, &c.Duration, &ok,
			&c.Session, &c.Trace); err != nil {
			return nil, err
		}
		c.Time, _ = time.Parse(time.RFC3339Nano, ts)
		c.OK = ok == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// ToolCorrelation represents two tools frequently called within the same session.
type ToolCorrelation struct {
	ToolA string `json:"toolA"`
	ToolB string `json:"toolB"`
	Count int    `json:"count"`
}

// Correlations finds tools that are commonly used together in sessions.
func (s *Store) Correlations(q Query, maxRows int) ([]ToolCorrelation, error) {
	n := maxRows
	if n <= 0 {
		n = 20
	}
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	whereClause := " WHERE a.event = 'mcp.call' AND b.event = 'mcp.call' AND ifnull(a.session,'') != '' AND ifnull(a.tool,'') != '' AND ifnull(b.tool,'') != '' AND a.tool < b.tool "
	if where != "" {
		whereClause += " AND " + strings.TrimPrefix(where, " WHERE ")
	}
	rows, err := s.db.Query(`SELECT a.tool, b.tool, count(*) AS cnt
		FROM records a
		JOIN records b ON a.session = b.session
		`+whereClause+`
		GROUP BY a.tool, b.tool
		ORDER BY cnt DESC LIMIT ?`, append(args, n)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolCorrelation
	for rows.Next() {
		var tc ToolCorrelation
		if err := rows.Scan(&tc.ToolA, &tc.ToolB, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// ToolTimelinePoint represents call and error counts over time.
type ToolTimelinePoint struct {
	TimeBucket string `json:"timeBucket"`
	Calls      int    `json:"calls"`
	Errors     int    `json:"errors"`
}

// Timeline aggregates calls and errors over hourly buckets.
func (s *Store) Timeline(q Query, maxPoints int) ([]ToolTimelinePoint, error) {
	n := maxPoints
	if n <= 0 {
		n = 24
	}
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	whereClause := " WHERE event = 'mcp.call' "
	if where != "" {
		whereClause += " AND " + strings.TrimPrefix(where, " WHERE ")
	}
	rows, err := s.db.Query(`SELECT strftime('%Y-%m-%d %H:00', ts_unix_ms / 1000, 'unixepoch') AS bucket,
		count(*), count(CASE WHEN ok = 0 THEN 1 END)
		FROM records `+whereClause+`
		GROUP BY bucket
		ORDER BY bucket DESC LIMIT ?`, append(args, n)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolTimelinePoint
	for rows.Next() {
		var p ToolTimelinePoint
		if err := rows.Scan(&p.TimeBucket, &p.Calls, &p.Errors); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
