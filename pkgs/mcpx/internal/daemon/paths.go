// Package daemon owns the MCP server pools and serves the local HTTP API that
// the CLI and generated script clients talk to.
package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// PathsAt is ResolvePaths with the two directories supplied.
//
// ResolvePaths runs before any configuration file has been found, so it can
// only read the environment; that is why MCPX_STATE_DIR is read by name
// there. Once the settings are resolved the caller may know better -- a
// paths.state in a config file, or --paths-state -- and this is how it says
// so without a second copy of the socket-naming rule. An empty argument keeps
// what ResolvePaths worked out.
func PathsAt(state, cache string) Paths {
	p := ResolvePaths()
	if state != "" {
		p.State = state
		p.Socket = socketPath(state, "")
		p.Info = filepath.Join(state, "daemon.json")
	}
	if cache != "" {
		p.Cache = cache
	}
	return p
}

// ForConfig keys the daemon to a particular configuration.
//
// Two things fall out of this, both of which matter once more than one repo is
// in play. Different configs get different daemons automatically, so a project
// with its own .mcpx.json does not have to agree with the user-level one. And
// the key is the config *file set* (FingerprintConfig), not its contents, so
// editing a config keeps the same key and the same daemon: the edit is picked
// up by that daemon, which compares ContentFingerprint on every request and
// reap tick (Server.watchConfig in routes_settings.go) and reloads through
// Server.reloadConfig and Registry.Reload (reload.go). Keying on contents
// was removed on purpose (#56): a reloading daemon moved its own key and
// became unreachable.
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
	raw := sha256.Sum256([]byte(state + "\x00" + key))
	sum := hex.EncodeToString(raw[:])[:16]

	// The fallback puts the socket in a private directory rather than
	// directly in a shared temp directory.
	//
	// A control socket at a predictable path in a world-writable directory
	// can be pre-created by anyone else on the machine, and then the CLI
	// sends daemon commands to whatever is listening there. The directory is
	// per-user and 0700, so the name being predictable stops mattering.
	if dir, err := privateRuntimeDir(); err == nil {
		short := filepath.Join(dir, "d-"+sum+".sock")
		if len(short) <= maxSocketPath {
			return short
		}
	}
	// Last resort, still inside a directory this user owns.
	return filepath.Join(os.TempDir(), "mcpx-"+sum+".sock")
}

// privateRuntimeDir returns a per-user directory only that user can enter.
//
// XDG_RUNTIME_DIR is already private where it exists. Elsewhere -- macOS
// among them -- a directory is created under the temp directory with the
// user id in its name, and its permissions are verified rather than assumed,
// because a directory that already exists may not be ours.
func privateRuntimeDir() (string, error) {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		dir := filepath.Join(d, "mcpx")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			return dir, nil
		}
	}
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("mcpx-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm() != 0o700 {
		// Pre-existing and more permissive than we would have made it.
		// Tightening is better than trusting, and an error if that fails.
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// FingerprintConfig derives the daemon key from every file that contributed.
//
// All of them matter, not just the nearest: two projects whose own config is
// byte-identical can still inherit different servers from different parents,
// and sharing a daemon between them would give one project the other's
// servers. Paths are what is hashed, so two identical files in different
// places stay separate.
//
// "Different places" means different files, not different spellings of one.
// Each path is resolved through symlinks first: /tmp is a symlink to
// /private/tmp on macOS, and a process whose $PWD holds the unresolved form
// -- a shell, an editor, the opencode plugin -- otherwise computed a second
// key for the same file, reported no daemon running, and started another.
//
// The *contents* used to be in the key as well, so that editing a config got
// a fresh daemon rather than a stale one. That stopped being a good trade the
// moment the daemon learned to reload: a daemon that reloads changes the
// contents of the files it was keyed by, which moves its own key, which makes
// it unreachable by the client that just edited it -- and, worse, hands the
// next command a *different* daemon that happens to match the new key with
// old servers loaded. That was observed: `mcpx servers remove` succeeded, the
// file was correct, and the next `mcpx servers list` showed the removed
// server, because it had found a daemon started two edits ago.
//
// So the key is the file set, and staleness is handled where it belongs: the
// daemon notices its files changed and re-reads them.
func FingerprintConfig(paths []string) string {
	h := sha256.New()
	for _, p := range paths {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// ContentFingerprint hashes what the config files currently say.
//
// Not the daemon key -- that is the file set -- but the thing the daemon
// compares against to notice somebody edited one behind its back.
func ContentFingerprint(paths []string) string {
	h := sha256.New()
	for _, p := range paths {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
		if b, err := os.ReadFile(p); err == nil {
			h.Write(b)
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// DefaultStateDir is the state directory a daemon uses when nobody asked
// for another one: XDG_STATE_HOME/mcpx, or ~/.local/state/mcpx. It ignores
// MCPX_STATE_DIR and paths.state on purpose -- it is the one location every
// invocation can compute, which is what makes it usable as the place custom
// state directories mirror their daemon records into (#369).
func DefaultStateDir() string {
	home, _ := os.UserHomeDir()
	return firstNonEmpty(
		envJoin("XDG_STATE_HOME", "mcpx"),
		filepath.Join(home, ".local", "state", "mcpx"),
	)
}

// envStateDir is the state directory MCPX_STATE_DIR names, or "" when it
// is not set. ResolvePaths honours it before the XDG location, so a daemon
// whose state directory is this one is a default daemon -- the plain
// invocation's daemon -- not a custom-state one (#407).
func envStateDir() string {
	return os.Getenv("MCPX_STATE_DIR")
}

// DaemonSearchDirs is where `mcpx daemons` looks for daemon records: the
// caller's own state directory, plus the default one. A daemon started
// with its own state directory mirrors its record into the default
// directory (see WriteInfo), so the second entry is what makes the listing
// cover the whole machine rather than only the caller's directory.
//
// The MCPX_STATE_DIR directory is searched too (#407): it is where a plain
// invocation's daemon lives when the variable is set, and a caller whose
// own state came from a config file or a flag would otherwise never see
// that daemon, mirror or no mirror.
func DaemonSearchDirs(primary string) []string {
	out := []string{primary}
	for _, d := range []string{DefaultStateDir(), envStateDir()} {
		if d == "" {
			continue
		}
		seen := false
		for _, have := range out {
			if have == d {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, d)
		}
	}
	return out
}

// ProcessAlive reports whether pid names a process that still exists.
// Signal 0 probes without delivering anything; EPERM means the process is
// there but owned by somebody else, which is still alive.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// PruneStale removes this state directory's daemon records whose process
// is gone, and returns what it removed. A daemon that exits cleanly
// removes its own record; one that is killed or crashes cannot, and
// without pruning its record is listed forever (#369). Records with no
// PID recorded are kept: there is nothing to check them against.
func (p Paths) PruneStale() []Info {
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
		path := filepath.Join(p.State, name)
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var i Info
		if json.Unmarshal(b, &i) != nil || i.PID == 0 || ProcessAlive(i.PID) {
			continue
		}
		if err := os.Remove(path); err == nil {
			out = append(out, i)
		}
	}
	return out
}

// mirrorInfoPath is where a daemon in a custom state directory also
// publishes its record: the default state directory, under a name keyed
// by its own directory so two custom directories cannot collide and a
// custom record cannot shadow the default directory's own daemon for the
// same configuration. Empty when this directory is the default one --
// the XDG location, or the MCPX_STATE_DIR location a plain invocation
// resolves to (#407). Mirroring the env-default daemon would leak a
// sandboxed daemon's record into the real default directory, where a
// non-sandboxed listing could see it and stop it.
func (p Paths) mirrorInfoPath() string {
	def := DefaultStateDir()
	if def == "" || p.State == "" || p.State == def || p.Info == "" {
		return ""
	}
	if env := envStateDir(); env != "" && p.State == env {
		return ""
	}
	sum := sha256.Sum256([]byte(p.State))
	return filepath.Join(def, "daemon-x"+hex.EncodeToString(sum[:])[:12]+"-"+filepath.Base(p.Info))
}

// RemoveInfo removes the daemon record, including its mirror in the
// default state directory when there is one.
func (p Paths) RemoveInfo() {
	if p.Info != "" {
		_ = os.Remove(p.Info)
	}
	if m := p.mirrorInfoPath(); m != "" {
		_ = os.Remove(m)
	}
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
//
// A daemon in a custom state directory also mirrors the record into the
// default state directory (#369): `mcpx daemons` promises every daemon on
// the machine, and a listing that only reads the caller's own directory
// can never see it. The mirror is best-effort -- the primary record is
// authoritative, and a daemon must not fail to start because a listing
// convenience could not be written.
func (p Paths) WriteInfo(i Info) error {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.Info + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p.Info); err != nil {
		return err
	}
	if m := p.mirrorInfoPath(); m != "" {
		if err := os.MkdirAll(filepath.Dir(m), 0o700); err == nil {
			mtmp := m + ".tmp"
			if os.WriteFile(mtmp, b, 0o600) == nil {
				_ = os.Rename(mtmp, m)
			}
		}
	}
	return nil
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
