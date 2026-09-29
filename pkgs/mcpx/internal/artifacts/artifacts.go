// Package artifacts is the outbox: files a script produced, held by the
// daemon and fetched by whoever asked for the run.
//
// The problem it solves is stated in issue 40. A script that produces a
// screenshot has, without this, exactly one way to hand it back: print it.
// That means base64 (+33%), mixed into text output, landing in an agent's
// context where a single screenshot costs more than the rest of the task.
// With this, the bytes stay on the daemon's disk and the caller receives a
// name, a type, a size and an id -- and fetches the body only if it wants it.
//
// Two decisions shape the storage.
//
// Content addressing. The body lives under its sha256, so a script that
// produces the same screenshot twice stores it once, and a caller that
// already holds that hash can skip the transfer. The *handle* is a separate
// random id, because a content hash is guessable by anyone who can guess the
// content: serving by hash would mean anyone who knows what a file looks
// like can read yours.
//
// An index beside it. Name, mime, run, session, expiry -- everything that is
// about this particular registration rather than about the bytes. SQLite
// because it is already a dependency and because "list the artifacts of this
// run" is a query, not a directory walk.
package artifacts

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	_ "modernc.org/sqlite" // the driver the log index already registers
)

// Meta is everything known about one registered artifact.
//
// The bytes are not here, and that is the point: this struct is what travels
// in a result, and it is small enough that a hundred of them cost less than
// one inline image.
type Meta struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Mime    string    `json:"mime"`
	Size    int64     `json:"size"`
	SHA256  string    `json:"sha256"`
	Run     string    `json:"run,omitempty"`
	Session string    `json:"session,omitempty"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
	// URI is the MCP resource form, mcpx://artifacts/<id>.
	URI string `json:"uri"`
}

// Options configure a store. Every value comes from the settings registry;
// none of them are decided here.
type Options struct {
	// Dir is the artifacts root. Blobs and the index live under it.
	Dir string
	// TTL is how long an artifact is kept before GC may remove it.
	TTL time.Duration
	// MaxBytes is the ceiling for one artifact. Zero means no ceiling.
	MaxBytes int64
	// Quota is the ceiling for the whole store. Zero means no ceiling.
	Quota int64
}

// Store holds artifacts and their index.
type Store struct {
	opts Options
	db   *sql.DB
	dir  string
}

const schema = `
CREATE TABLE IF NOT EXISTS artifacts (
  id      TEXT PRIMARY KEY,
  hash    TEXT NOT NULL,
  name    TEXT NOT NULL,
  mime    TEXT NOT NULL,
  size    INTEGER NOT NULL,
  run     TEXT NOT NULL DEFAULT '',
  session TEXT NOT NULL DEFAULT '',
  created INTEGER NOT NULL,
  expires INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS artifacts_run ON artifacts(run);
CREATE INDEX IF NOT EXISTS artifacts_session ON artifacts(session);
CREATE INDEX IF NOT EXISTS artifacts_expires ON artifacts(expires);
CREATE INDEX IF NOT EXISTS artifacts_hash ON artifacts(hash);
`

// Open creates or opens a store rooted at opts.Dir.
func Open(opts Options) (*Store, error) {
	if opts.Dir == "" {
		return nil, errors.New("artifacts: no directory")
	}
	if err := os.MkdirAll(filepath.Join(opts.Dir, "blobs"), 0o700); err != nil {
		return nil, err
	}
	// The same pragmas the log index uses, for the same reason: the daemon
	// writes while a CLI reads, and the default journal makes those block.
	dsn := filepath.Join(opts.Dir, "index.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("artifacts schema: %w", err)
	}
	return &Store{opts: opts, db: db, dir: opts.Dir}, nil
}

// Close releases the index handle.
func (s *Store) Close() error { return s.db.Close() }

// Dir is the store root.
func (s *Store) Dir() string { return s.dir }

// ErrTooLarge is returned when one artifact exceeds the per-artifact cap.
type ErrTooLarge struct{ Size, Max int64 }

func (e ErrTooLarge) Error() string {
	return fmt.Sprintf("artifact is %d bytes, over the %d byte limit "+
		"(raise artifacts.maxBytes)", e.Size, e.Max)
}

// ErrQuota is returned when storing would push the store over its quota.
type ErrQuota struct{ Used, Quota int64 }

func (e ErrQuota) Error() string {
	return fmt.Sprintf("the artifact store holds %d bytes of a %d byte quota "+
		"(raise artifacts.quota, or delete some)", e.Used, e.Quota)
}

// PutOptions describe one registration.
type PutOptions struct {
	Name    string
	Mime    string
	Run     string
	Session string
	// TTL overrides the store default for this one artifact.
	TTL time.Duration
}

func (s *Store) blobPath(hash string) string {
	// Two-level fan-out. A flat directory of a hundred thousand files is
	// slow to list on every filesystem worth naming.
	return filepath.Join(s.dir, "blobs", hash[:2], hash[2:])
}

// NewID is the handle an artifact is served by.
//
// Random rather than derived from the content: see the package comment. 128
// bits, because this is the whole of the access control until OAuth scopes
// land, and a guessable handle would make every artifact world-readable to
// anything that can reach the daemon.
func NewID() string {
	b := make([]byte, defaults.ArtifactIDBytes)
	_, _ = rand.Read(b)
	return "art-" + hex.EncodeToString(b)
}

// Put stores the bytes from r and registers them.
//
// The body is written to a temporary file and renamed into place only once
// it is complete, so a cancelled upload leaves nothing a later reader could
// mistake for a whole file. That is the same rule the streaming delivery
// obeys on the receiving side.
func (s *Store) Put(r io.Reader, p PutOptions) (Meta, error) {
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "blobs"), ".incoming-*")
	if err != nil {
		return Meta{}, err
	}
	tmpName := tmp.Name()
	// Removed unconditionally: on the success path the file has already been
	// renamed away and this is a no-op.
	defer os.Remove(tmpName)

	h := sha256.New()
	var reader io.Reader = io.TeeReader(r, h)
	if s.opts.MaxBytes > 0 {
		// One byte past the limit, so exceeding it is detectable rather than
		// silently truncating -- a truncated screenshot is worse than a
		// refused one, because it looks like it worked.
		reader = io.LimitReader(reader, s.opts.MaxBytes+1)
	}
	size, err := io.Copy(tmp, reader)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Meta{}, err
	}
	if s.opts.MaxBytes > 0 && size > s.opts.MaxBytes {
		return Meta{}, ErrTooLarge{Size: size, Max: s.opts.MaxBytes}
	}
	hash := hex.EncodeToString(h.Sum(nil))
	if err := s.checkQuota(hash, size); err != nil {
		return Meta{}, err
	}
	if err := s.adopt(tmpName, hash); err != nil {
		return Meta{}, err
	}
	return s.register(hash, size, p)
}

// PutFile registers a file that is already on this machine's disk.
//
// A hardlink where the filesystem allows one, a copy where it does not. This
// is the local case the issue singles out: a screenshot already written to
// disk should not be read, hashed into memory and written again just to be
// handed to a caller who can see the same disk.
//
// It is still hashed, because the store is content addressed and because a
// caller comparing hashes is how a transfer gets skipped.
func (s *Store) PutFile(path string, p PutOptions) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Meta{}, err
	}
	if st.IsDir() {
		return Meta{}, fmt.Errorf("%s is a directory", path)
	}
	if s.opts.MaxBytes > 0 && st.Size() > s.opts.MaxBytes {
		return Meta{}, ErrTooLarge{Size: st.Size(), Max: s.opts.MaxBytes}
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Meta{}, err
	}
	hash := hex.EncodeToString(h.Sum(nil))
	if err := s.checkQuota(hash, st.Size()); err != nil {
		return Meta{}, err
	}
	dest := s.blobPath(hash)
	if _, err := os.Stat(dest); err != nil {
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return Meta{}, err
		}
		if lerr := os.Link(path, dest); lerr != nil {
			// A different device, or a filesystem with no links. Copy.
			if _, serr := f.Seek(0, io.SeekStart); serr != nil {
				return Meta{}, serr
			}
			if cerr := copyInto(dest, f); cerr != nil {
				return Meta{}, cerr
			}
		}
	}
	if p.Name == "" {
		p.Name = filepath.Base(path)
	}
	return s.register(hash, st.Size(), p)
}

func copyInto(dest string, r io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".incoming-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// adopt moves a completed temporary file into its content address.
func (s *Store) adopt(tmp, hash string) error {
	dest := s.blobPath(hash)
	if _, err := os.Stat(dest); err == nil {
		// Already stored. Identical content by definition, so keep the one
		// that is there: it may already be hardlinked into a caller's
		// directory, and replacing it would break that link's meaning.
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func (s *Store) register(hash string, size int64, p PutOptions) (Meta, error) {
	ttl := p.TTL
	if ttl <= 0 {
		ttl = s.opts.TTL
	}
	now := time.Now()
	m := Meta{
		ID:      NewID(),
		Name:    SanitizeName(p.Name),
		Mime:    p.Mime,
		Size:    size,
		SHA256:  hash,
		Run:     p.Run,
		Session: p.Session,
		Created: now,
		Expires: now.Add(ttl),
	}
	if m.Mime == "" {
		m.Mime = MimeForName(m.Name)
	}
	m.URI = URI(m.ID)
	_, err := s.db.Exec(
		`INSERT INTO artifacts (id,hash,name,mime,size,run,session,created,expires)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		m.ID, m.SHA256, m.Name, m.Mime, m.Size, m.Run, m.Session,
		m.Created.UnixMilli(), m.Expires.UnixMilli())
	if err != nil {
		return Meta{}, err
	}
	return m, nil
}

// URI is the MCP resource form of an artifact id.
const uriPrefix = "mcpx://artifacts/"

// URI renders an id as the resource URI an MCP client reads it by.
func URI(id string) string { return uriPrefix + id }

// IDFromURI recovers the id from a resource URI.
func IDFromURI(uri string) (string, bool) {
	id, ok := strings.CutPrefix(uri, uriPrefix)
	if !ok || id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// checkQuota refuses a write that would take the store over its ceiling.
//
// Content that is already stored costs nothing to register again, so it is
// exempt: refusing a duplicate would be refusing a write that frees no space
// by being refused.
func (s *Store) checkQuota(hash string, size int64) error {
	if s.opts.Quota <= 0 {
		return nil
	}
	if _, err := os.Stat(s.blobPath(hash)); err == nil {
		return nil
	}
	used, err := s.Used()
	if err != nil {
		return err
	}
	if used+size > s.opts.Quota {
		return ErrQuota{Used: used, Quota: s.opts.Quota}
	}
	return nil
}

// Used is the number of distinct bytes the store holds.
func (s *Store) Used() (int64, error) {
	// DISTINCT hash: two registrations of the same content share one blob,
	// so summing every row would report several times what is on disk.
	row := s.db.QueryRow(
		`SELECT COALESCE(SUM(size),0) FROM (SELECT DISTINCT hash, size FROM artifacts)`)
	var n int64
	return n, row.Scan(&n)
}

// Filter narrows a listing.
type Filter struct {
	Run     string
	Session string
	Limit   int
}

// List returns matching artifacts, newest first.
func (s *Store) List(f Filter) ([]Meta, error) {
	q := `SELECT id,hash,name,mime,size,run,session,created,expires FROM artifacts WHERE 1=1`
	var args []any
	if f.Run != "" {
		q += " AND run = ?"
		args = append(args, f.Run)
	}
	if f.Session != "" {
		q += " AND session = ?"
		args = append(args, f.Session)
	}
	q += " ORDER BY created DESC, rowid DESC"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Meta{}
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scan(r scanner) (Meta, error) {
	var m Meta
	var created, expires int64
	if err := r.Scan(&m.ID, &m.SHA256, &m.Name, &m.Mime, &m.Size,
		&m.Run, &m.Session, &created, &expires); err != nil {
		return Meta{}, err
	}
	m.Created = time.UnixMilli(created)
	m.Expires = time.UnixMilli(expires)
	m.URI = URI(m.ID)
	return m, nil
}

// ErrNotFound is returned for an id the store does not hold, which usually
// means it expired rather than that it never existed.
type ErrNotFound struct{ ID string }

func (e ErrNotFound) Error() string {
	return "no artifact " + e.ID + "; it may have expired"
}

// Get returns one artifact's metadata.
func (s *Store) Get(id string) (Meta, error) {
	row := s.db.QueryRow(
		`SELECT id,hash,name,mime,size,run,session,created,expires FROM artifacts WHERE id = ?`, id)
	m, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Meta{}, ErrNotFound{ID: id}
	}
	return m, err
}

// OpenBody returns the artifact's bytes alongside its metadata.
func (s *Store) OpenBody(id string) (*os.File, Meta, error) {
	m, err := s.Get(id)
	if err != nil {
		return nil, Meta{}, err
	}
	f, err := os.Open(s.blobPath(m.SHA256))
	if err != nil {
		return nil, Meta{}, err
	}
	return f, m, nil
}

// BodyPath is where the bytes live, for a caller on the same filesystem.
func (s *Store) BodyPath(m Meta) string { return s.blobPath(m.SHA256) }

// Bytes reads a whole artifact. For inline delivery, where the caller has
// already agreed to a size cap.
func (s *Store) Bytes(id string) ([]byte, Meta, error) {
	f, m, err := s.OpenBody(id)
	if err != nil {
		return nil, Meta{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	return b, m, err
}

// Delete removes one registration, and the blob when nothing else uses it.
func (s *Store) Delete(id string) error {
	m, err := s.Get(id)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM artifacts WHERE id = ?`, id); err != nil {
		return err
	}
	return s.sweepBlob(m.SHA256)
}

// sweepBlob removes a body no registration refers to any more.
func (s *Store) sweepBlob(hash string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE hash = ?`, hash).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	err := os.Remove(s.blobPath(hash))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GC removes everything that has expired, and returns how many went.
func (s *Store) GC(now time.Time) (int, error) {
	rows, err := s.db.Query(`SELECT id FROM artifacts WHERE expires <= ?`, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if err := s.Delete(id); err == nil {
			n++
		}
	}
	return n, nil
}

// Export places an artifact in a directory the caller named.
//
// A hardlink first, because the local case should not copy megabytes to hand
// a file to something that can already see the disk it is on. A copy when
// the link fails, which is what a different device or a foreign filesystem
// looks like.
//
// The name is sanitised and collisions are resolved by suffixing, never by
// overwriting: a script that emits two files called "shot.png" has produced
// two files, and silently keeping one of them is data loss.
func (s *Store) Export(m Meta, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dest, err := uniquePath(dir, m.Name)
	if err != nil {
		return "", err
	}
	src := s.blobPath(m.SHA256)
	if err := os.Link(src, dest); err == nil {
		return dest, nil
	}
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := copyInto(dest, f); err != nil {
		return "", err
	}
	return dest, nil
}
