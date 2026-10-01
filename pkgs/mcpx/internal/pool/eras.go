package pool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/mcpclient"
)

// EraRecord is what is remembered about one server configuration's era.
type EraRecord struct {
	Era     mcpclient.Era `json:"era"`
	Version string        `json:"version,omitempty"`
	At      time.Time     `json:"at"`
	// Source is how the era was learned: probe, cache or forced.
	Source string `json:"source"`
	// Transport is TransportHTTPSSE for an endpoint that speaks only the
	// 2024-11-05 HTTP+SSE transport; empty for the configured one.
	Transport string `json:"transport,omitempty"`
}

// TransportHTTPSSE marks an era record for an HTTP+SSE-only endpoint.
const TransportHTTPSSE = "http+sse"

// EraStore remembers which era each server configuration speaks.
//
// The 2026-07-28 versioning page makes the era a property of the server:
// clients SHOULD cache it for the life of the process and MAY persist it
// across restarts of the same configuration. The saving is the probe's round
// trip -- or, for a legacy server that ignores unknown methods, the whole
// probe timeout -- on every start after the first.
type EraStore interface {
	Get(key string) (EraRecord, bool)
	Put(key string, rec EraRecord) error
}

// Identity is a server configuration's identity for the era cache: the
// process definition and nothing else.
//
// Not PoolID. That includes leasing knobs, which do not change what the
// server speaks, and it hashes headers in map iteration order, so it is not
// even stable between runs. The era is a property of what is started, so
// that is all this hashes; encoding/json sorts map keys, which is what makes
// env and headers deterministic here.
func Identity(c *config.Resolved) string {
	b, _ := json.Marshal(struct {
		Command   string            `json:"command"`
		Args      []string          `json:"args"`
		Env       map[string]string `json:"env"`
		Cwd       string            `json:"cwd"`
		Transport string            `json:"transport"`
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
	}{c.Command, c.Args, c.Env, c.Cwd, c.Transport, c.URL, c.Headers})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// EraFile is an EraStore held in memory and persisted to one JSON file.
type EraFile struct {
	path string
	mu   sync.Mutex
	m    map[string]EraRecord
}

type eraFileDoc struct {
	Version int                  `json:"version"`
	Servers map[string]EraRecord `json:"servers"`
}

// OpenEraFile loads the cache at path.
//
// A file that is missing, unreadable, truncated, or of another version is
// treated as empty rather than as an error: the worst a lost cache costs is
// one probe per server, and refusing to start over it would cost everything.
// The next Put rewrites it whole.
func OpenEraFile(path string) *EraFile {
	f := &EraFile{path: path, m: map[string]EraRecord{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return f
	}
	var doc eraFileDoc
	if json.Unmarshal(b, &doc) != nil || doc.Version != defaults.UpstreamEraFileVersion {
		return f
	}
	for k, v := range doc.Servers {
		if v.Era == mcpclient.EraLegacy || v.Era == mcpclient.EraModern {
			f.m[k] = v
		}
	}
	return f
}

// Get returns the remembered era for a configuration.
func (f *EraFile) Get(key string) (EraRecord, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.m[key]
	return r, ok
}

// Put remembers an era and rewrites the file atomically: a temporary file in
// the same directory, then a rename, so a crash mid-write leaves the old
// file or the new one and never half of each.
func (f *EraFile) Put(key string, rec EraRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[key] = rec
	b, err := json.MarshalIndent(eraFileDoc{Version: defaults.UpstreamEraFileVersion, Servers: f.m}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, defaults.DirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(f.path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Chmod(defaults.PrivateMode); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, f.path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
