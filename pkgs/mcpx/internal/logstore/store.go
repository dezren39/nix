// Package logstore indexes the JSONL logs into SQLite so they can be queried.
//
// The JSONL files stay the record of truth. This is an index and nothing more:
// deleting the database costs a re-scan and no data, which is why ingest is
// safe to run on demand and why nothing writes here except ingest. A store
// that owned the only copy would have to be correct; one that can be rebuilt
// from the files beside it only has to be useful.
//
// The driver is modernc.org/sqlite, a pure-Go translation of SQLite. A cgo
// driver would be faster and is not an option: the binary is built by Nix and
// has to cross-compile and build in a sandbox without a C toolchain.
package logstore

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"regexp"
	"sync"

	sqlite3 "modernc.org/sqlite"
)

// schema is the whole database. It is exported through `mcpx log sql --schema`
// because anyone writing a query needs it and should not have to read Go.
const schema = `
CREATE TABLE IF NOT EXISTS records (
  id INTEGER PRIMARY KEY,
  ts TEXT NOT NULL,
  ts_unix_ms INTEGER NOT NULL,
  level TEXT NOT NULL,
  msg TEXT,
  template TEXT,
  event TEXT,
  trace TEXT,
  parent TEXT,
  session TEXT,
  server TEXT,
  tool TEXT,
  instance TEXT,
  duration_ms REAL,
  ok INTEGER,
  error TEXT,
  source_file TEXT,
  source_line INTEGER,
  source_function TEXT,
  cwd TEXT,
  pid INTEGER,
  host TEXT,
  user TEXT,
  attrs TEXT,
  file TEXT NOT NULL,
  offset INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS records_origin ON records(file, offset);
CREATE INDEX IF NOT EXISTS records_ts ON records(ts_unix_ms);
CREATE INDEX IF NOT EXISTS records_trace ON records(trace);
CREATE INDEX IF NOT EXISTS records_parent ON records(parent);
CREATE INDEX IF NOT EXISTS records_event ON records(event);
CREATE INDEX IF NOT EXISTS records_server_tool ON records(server, tool);
CREATE INDEX IF NOT EXISTS records_session ON records(session);

CREATE TABLE IF NOT EXISTS files (
  path TEXT PRIMARY KEY,
  fingerprint TEXT,
  size INTEGER NOT NULL,
  mtime_unix_ms INTEGER NOT NULL,
  offset INTEGER NOT NULL,
  records INTEGER NOT NULL DEFAULT 0,
  skipped INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS files_fingerprint ON files(fingerprint);
`

// Schema returns the DDL.
func Schema() string { return schema }

// Store is an open index.
type Store struct {
	db   *sql.DB
	path string
	dir  string
}

var registerOnce sync.Once

// registerRegexp teaches SQLite the REGEXP operator.
//
// SQLite parses `x REGEXP y` but ships no implementation, so the operator is a
// syntax error until something supplies a two-argument function called
// "regexp". Note the argument order is reversed from the infix form: SQLite
// calls regexp(pattern, subject).
func registerRegexp() {
	registerOnce.Do(func() {
		cache := map[string]*regexp.Regexp{}
		var mu sync.Mutex
		_ = sqlite3.RegisterDeterministicScalarFunction("regexp", 2,
			func(ctx *sqlite3.FunctionContext, args []driver.Value) (driver.Value, error) {
				pattern, _ := args[0].(string)
				subject, _ := args[1].(string)
				mu.Lock()
				re, ok := cache[pattern]
				if !ok {
					var err error
					re, err = regexp.Compile(pattern)
					if err != nil {
						mu.Unlock()
						return nil, err
					}
					cache[pattern] = re
				}
				mu.Unlock()
				if re.MatchString(subject) {
					return int64(1), nil
				}
				return int64(0), nil
			})
	})
}

// openMu serialises Open; see the comment there.
var openMu sync.Mutex

// Open creates or opens the index for a log directory.
func Open(logDir string) (*Store, error) {
	registerRegexp()
	path := filepath.Join(logDir, "index.db")
	// WAL, because the daemon may be writing while the CLI reads, and the
	// default rollback journal makes a reader block a writer. busy_timeout is
	// the other half: without it a concurrent ingest surfaces as an immediate
	// "database is locked" rather than as a short wait. It comes first
	// because pragmas apply in order and the WAL switch itself needs a lock.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	// Creating the schema upgrades a read lock to a write lock, which SQLite
	// refuses with SQLITE_BUSY *without* consulting busy_timeout when another
	// connection is doing the same (it would otherwise deadlock). Two MCP
	// requests reading the log concurrently -- which stdio now allows -- hit
	// exactly that on a fresh index. Serialising opens in this process removes
	// the in-process race; another process opening at the same instant is
	// still possible and rare, and fails loudly rather than corrupting.
	openMu.Lock()
	defer openMu.Unlock()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db, path: path, dir: logDir}, nil
}

// Path is the database file, for `mcpx log sql --path`.
func (s *Store) Path() string { return s.path }

// Dir is the log directory this index covers.
func (s *Store) Dir() string { return s.dir }

// DB exposes the handle for raw queries.
func (s *Store) DB() *sql.DB { return s.db }

// Close releases the handle.
func (s *Store) Close() error { return s.db.Close() }
