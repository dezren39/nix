// Package daemon owns the MCP server pools and serves the local HTTP API that
// the CLI and generated script clients talk to.
package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Paths resolves the on-disk locations mcpx uses.
type Paths struct {
	State  string
	Cache  string
	Socket string
	Info   string
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// ResolvePaths returns XDG-correct locations, honouring MCPX_STATE_DIR and
// MCPX_CACHE_DIR for tests and sandboxes.
func ResolvePaths() Paths {
	home, _ := os.UserHomeDir()
	state := firstNonEmpty(
		os.Getenv("MCPX_STATE_DIR"),
		envJoin("XDG_STATE_HOME", "mcpx"),
		filepath.Join(home, ".local", "state", "mcpx"),
	)
	cache := firstNonEmpty(
		os.Getenv("MCPX_CACHE_DIR"),
		envJoin("XDG_CACHE_HOME", "mcpx"),
		filepath.Join(home, ".cache", "mcpx"),
	)
	return Paths{
		State:  state,
		Cache:  cache,
		Socket: socketPath(state, ""),
		Info:   filepath.Join(state, "daemon.json"),
	}
}

// ForConfig keys the daemon to a particular configuration.
//
// Two things fall out of this, both of which matter once more than one repo is
// in play. Different configs get different daemons automatically, so a project
// with its own .mcpx.json does not have to agree with the user-level one. And
// editing a config produces a new key, so the next command starts a daemon
// that has actually read the change instead of silently talking to a stale
// one.
func (p Paths) ForConfig(hash string) Paths {
	if hash == "" {
		return p
	}
	out := p
	out.Socket = socketPath(p.State, hash)
	out.Info = filepath.Join(p.State, "daemon-"+hash+".json")
	return out
}

// maxSocketPath is the portable ceiling for sun_path. macOS allows 104 bytes
// including the NUL; Linux allows 108. Staying under the smaller figure keeps
// behaviour identical on both.
const maxSocketPath = 100

// socketPath keeps the socket beside the state directory when it fits, and
// falls back to a short hashed name in the temp directory when the state path
// is too long for a unix socket. Deep XDG paths and Go's t.TempDir() both
// exceed the limit easily, and the failure mode is an opaque
// "bind: invalid argument", so this is worth handling rather than documenting.
func socketPath(state, key string) string {
	if p := os.Getenv("MCPX_SOCKET"); p != "" {
		return p
	}
	name := "daemon.sock"
	if key != "" {
		name = "daemon-" + key + ".sock"
	}
	preferred := filepath.Join(state, name)
	if len(preferred) <= maxSocketPath {
		return preferred
	}
	sum := sha256.Sum256([]byte(state + "\x00" + key))
	short := filepath.Join(os.TempDir(), "mcpx-"+hex.EncodeToString(sum[:])[:16]+".sock")
	if len(short) <= maxSocketPath {
		return short
	}
	// Last resort: /tmp is present on every platform mcpx targets.
	return "/tmp/mcpx-" + hex.EncodeToString(sum[:])[:16] + ".sock"
}

// FingerprintConfig derives the daemon key from every file that contributed.
//
// All of them matter, not just the nearest: two projects whose own config is
// byte-identical can still inherit different servers from different parents,
// and sharing a daemon between them would give one project the other's
// servers. Paths contribute as well as contents, so two identical files in
// different places stay separate.
func FingerprintConfig(paths []string) string {
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
		if b, err := os.ReadFile(p); err == nil {
			h.Write(b)
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// ListDaemons returns the info records of every daemon in this state dir.
func (p Paths) ListDaemons() []Info {
	entries, err := os.ReadDir(p.State)
	if err != nil {
		return nil
	}
	var out []Info
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "daemon") || !strings.HasSuffix(name, ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(p.State, name))
		if err != nil {
			continue
		}
		var i Info
		if json.Unmarshal(b, &i) == nil && i.Socket != "" {
			out = append(out, i)
		}
	}
	return out
}

func envJoin(env, sub string) string {
	if v := os.Getenv(env); v != "" {
		return filepath.Join(v, sub)
	}
	return ""
}

// EnsureDirs creates the state, cache and socket directories.
func (p Paths) EnsureDirs() error {
	if err := os.MkdirAll(p.State, 0o700); err != nil {
		return err
	}
	if dir := filepath.Dir(p.Socket); dir != p.State {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.MkdirAll(p.Cache, 0o700)
}

// Info is the record a running daemon publishes for CLI discovery.
type Info struct {
	PID        int    `json:"pid"`
	Socket     string `json:"socket"`
	Endpoint   string `json:"endpoint"`
	ConfigPath string `json:"configPath"`
	ConfigHash string `json:"configHash"`
	Version    string `json:"version"`
	StartedAt  string `json:"startedAt"`
}

// WriteInfo publishes the daemon record atomically.
func (p Paths) WriteInfo(i Info) error {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.Info + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.Info)
}

// ReadInfo loads the daemon record, if any.
func (p Paths) ReadInfo() (*Info, error) {
	b, err := os.ReadFile(p.Info)
	if err != nil {
		return nil, err
	}
	var i Info
	if err := json.Unmarshal(b, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

// SchemaCachePath is the cache file for a given config fingerprint.
func (p Paths) SchemaCachePath(hash string) string {
	return filepath.Join(p.Cache, "schemas-"+hash+".json")
}

// HashConfig fingerprints the server definitions so a stale cache is never
// served after the config changes.
func HashConfig(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}
