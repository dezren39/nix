package logstore

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// IngestResult reports what a pass over the log directory did.
type IngestResult struct {
	Files    int `json:"files"`
	Scanned  int `json:"scanned"`
	Inserted int `json:"inserted"`
	Skipped  int `json:"skipped"`
	Renamed  int `json:"renamed"`
}

// Ingest brings the index up to date with the JSONL files in the log
// directory.
//
// It is incremental and idempotent: a file whose size and mtime match what was
// recorded, and which has been read to its end, is not opened at all. Running
// this before every query is what keeps the design free of a background
// indexer thread, which is a thing that can be wrong without anyone noticing.
func (s *Store) Ingest() (IngestResult, error) {
	var out IngestResult
	paths, err := filepath.Glob(filepath.Join(s.dir, "*.jsonl"))
	if err != nil {
		return out, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		n, err := s.ingestFile(p, &out)
		if err != nil {
			// One unreadable file must not hide the rest; the log directory is
			// exactly where a half-written file is normal.
			continue
		}
		out.Files++
		out.Inserted += n
	}
	return out, nil
}

type fileState struct {
	path        string
	fingerprint string
	size        int64
	mtimeMs     int64
	offset      int64
}

func (s *Store) ingestFile(path string, res *IngestResult) (int, error) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return 0, err
	}
	fp, err := fingerprint(path)
	if err != nil {
		return 0, err
	}
	// A file with no complete line yet has no identity, and inserting rows for
	// a partial line would pin a wrong offset forever.
	if fp == "" {
		return 0, nil
	}

	prev, err := s.lookup(path, fp)
	if err != nil {
		return 0, err
	}
	if prev != nil && prev.path != path {
		// Rotation renames the live file out from under us. Identity is the
		// first line -- a timestamp to the nanosecond plus a trace id, which
		// is unique in practice -- rather than the path, so the rows already
		// ingested are re-pointed instead of ingested a second time under the
		// new name.
		if err := s.renameFile(prev.path, path); err != nil {
			return 0, err
		}
		res.Renamed++
		prev.path = path
	}

	start := int64(0)
	if prev != nil {
		start = prev.offset
		// Shrinking means the file was replaced, not appended to, so the
		// recorded offset points into content that no longer exists.
		if st.Size() < prev.offset {
			start = 0
			if err := s.forget(path); err != nil {
				return 0, err
			}
		} else if st.Size() == prev.size && st.ModTime().UnixMilli() == prev.mtimeMs &&
			prev.offset >= st.Size() {
			return 0, nil
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	rd := bufio.NewReaderSize(f, 128*1024)
	offset := start
	inserted, skipped := 0, 0
	for {
		line, rerr := rd.ReadBytes('\n')
		if rerr != nil && len(line) == 0 {
			break
		}
		// A trailing fragment with no newline is a record still being written.
		// Leaving the offset short of it is what makes the next pass pick it
		// up whole.
		if rerr != nil {
			break
		}
		lineStart := offset
		offset += int64(len(line))
		res.Scanned++
		r, ok := parseLine(line)
		if !ok {
			// A line that does not parse is a line, not a crash. Counting them
			// is the difference between "the log is fine" and "something is
			// writing garbage into it".
			skipped++
			res.Skipped++
			continue
		}
		if err := r.insert(stmt, path, lineStart); err != nil {
			return 0, err
		}
		inserted++
	}

	if _, err := tx.Exec(`
		INSERT INTO files(path, fingerprint, size, mtime_unix_ms, offset, records, skipped)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET
		  fingerprint=excluded.fingerprint, size=excluded.size,
		  mtime_unix_ms=excluded.mtime_unix_ms, offset=excluded.offset,
		  records=files.records+excluded.records,
		  skipped=files.skipped+excluded.skipped`,
		path, fp, st.Size(), st.ModTime().UnixMilli(), offset, inserted, skipped); err != nil {
		return 0, err
	}
	return inserted, tx.Commit()
}

// fingerprint hashes the first complete line of a file.
func fingerprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(f, 64*1024).ReadBytes('\n')
	if err != nil || len(line) == 0 {
		return "", nil
	}
	sum := sha256.Sum256(line)
	return hex.EncodeToString(sum[:16]), nil
}

func (s *Store) lookup(path, fp string) (*fileState, error) {
	row := s.db.QueryRow(`
		SELECT path, fingerprint, size, mtime_unix_ms, "offset" FROM files
		WHERE path = ? OR fingerprint = ?
		ORDER BY (path = ?) DESC LIMIT 1`, path, fp, path)
	var st fileState
	switch err := row.Scan(&st.path, &st.fingerprint, &st.size, &st.mtimeMs, &st.offset); err {
	case nil:
		return &st, nil
	case sql.ErrNoRows:
		return nil, nil
	default:
		return nil, err
	}
}

func (s *Store) renameFile(from, to string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM files WHERE path = ?`, to); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE files SET path = ? WHERE path = ?`, to, from); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE records SET file = ? WHERE file = ?`, to, from); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) forget(path string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM records WHERE file = ?`, path); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE path = ?`, path); err != nil {
		return err
	}
	return tx.Commit()
}

const insertSQL = `INSERT OR IGNORE INTO records(
  ts, ts_unix_ms, level, msg, template, event, trace, parent, session,
  server, tool, instance, duration_ms, ok, error,
  source_file, source_line, source_function, cwd, pid, host, user,
  attrs, file, "offset")
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

func (r *parsed) insert(stmt *sql.Stmt, file string, offset int64) error {
	_, err := stmt.Exec(
		r.ts, r.tsUnixMs, r.level, r.msg, null(r.template), null(r.event),
		null(r.trace), null(r.parent), null(r.session), null(r.server),
		null(r.tool), null(r.instance), r.durationMs, r.ok, null(r.errText),
		null(r.sourceFile), r.sourceLine, null(r.sourceFunc), null(r.cwd),
		r.pid, null(r.host), null(r.user), null(r.attrs), file, offset)
	return err
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// parsed is one JSONL line split into the columns worth indexing and a JSON
// blob of everything else.
type parsed struct {
	ts         string
	tsUnixMs   int64
	level      string
	msg        string
	template   string
	event      string
	trace      string
	parent     string
	session    string
	server     string
	tool       string
	instance   string
	durationMs any
	ok         any
	errText    string
	sourceFile string
	sourceLine any
	sourceFunc string
	cwd        string
	pid        any
	host       string
	user       string
	attrs      string
}

// promoted are the attribute keys that become columns. The rest stay in the
// attrs blob, reachable through json_extract, because promoting every key a
// script might invent would turn the schema into a moving target.
var promoted = map[string]bool{
	"ts": true, "level": true, "msg": true, "template": true,
	"event": true, "trace": true, "trace.parent": true,
	"session": true, "server": true, "tool": true, "instance": true,
	"durationMs": true, "duration_ms": true, "ok": true, "error": true,
	"source.file": true, "source.line": true, "source.function": true,
	"cwd": true, "process.cwd": true, "pid": true, "process.pid": true,
	"host.name": true, "user.name": true,
}

func parseLine(line []byte) (*parsed, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal(line, &obj); err != nil {
		return nil, false
	}
	tsRaw, _ := obj["ts"].(string)
	t, err := time.Parse(time.RFC3339Nano, tsRaw)
	if err != nil {
		// A record with no usable timestamp cannot be placed on the timeline,
		// which is the one thing every query needs.
		return nil, false
	}

	r := &parsed{
		// Normalised to millisecond RFC3339 so that string ordering and
		// ts_unix_ms ordering agree. Nanosecond precision from a script and
		// second precision from Go would otherwise sort differently as text.
		ts:       t.Format("2006-01-02T15:04:05.000Z07:00"),
		tsUnixMs: t.UnixMilli(),
		level:    str(obj["level"]),
		msg:      str(obj["msg"]),
		template: str(obj["template"]),
		event:    str(obj["event"]),
		trace:    str(obj["trace"]),
		parent:   str(obj["trace.parent"]),
		session:  str(obj["session"]),
		server:   str(obj["server"]),
		tool:     str(obj["tool"]),
		instance: str(obj["instance"]),
		errText:  str(obj["error"]),
		cwd:      first(str(obj["cwd"]), str(obj["process.cwd"])),
		host:     str(obj["host.name"]),
		user:     str(obj["user.name"]),
	}
	if r.level == "" {
		r.level = "info"
	}
	if v, ok := num(obj["durationMs"]); ok {
		r.durationMs = v
	} else if v, ok := num(obj["duration_ms"]); ok {
		r.durationMs = v
	}
	switch v := obj["ok"].(type) {
	case bool:
		if v {
			r.ok = int64(1)
		} else {
			r.ok = int64(0)
		}
	}
	if r.ok == nil && r.errText != "" {
		r.ok = int64(0)
	}
	r.sourceFile = str(obj["source.file"])
	if v, ok := num(obj["source.line"]); ok {
		r.sourceLine = int64(v)
	}
	r.sourceFunc = str(obj["source.function"])
	if v, ok := num(obj["pid"]); ok {
		r.pid = int64(v)
	} else if v, ok := num(obj["process.pid"]); ok {
		r.pid = int64(v)
	}

	rest := make(map[string]any, len(obj))
	for k, v := range obj {
		if !promoted[k] {
			rest[k] = v
		}
	}
	if len(rest) > 0 {
		if b, err := json.Marshal(rest); err == nil {
			r.attrs = string(b)
		}
	}
	return r, true
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func first(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
