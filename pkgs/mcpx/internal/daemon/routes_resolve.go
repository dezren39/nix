package daemon

import (
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
)

// routesResolve registers the directory-to-daemon lookup.
//
// One line in the route table, one file, because this endpoint exists for a
// caller that has nothing else: no mcpx binary, possibly not even a
// filesystem view of the state directory, only a socket it already found.
func (s *Server) routesResolve(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/resolve", s.handleResolve)
}

// Resolution is the answer to "which daemon serves this directory".
//
// ConfigHash is the *daemon key*: the fingerprint over every configuration
// file that contributed, which is what names the socket and the info file.
// It is deliberately not the hash that daemon-<key>.json carries in its
// "configHash" field -- that one is a digest of the server definitions, used
// to invalidate the schema cache, and the two are different numbers for the
// same daemon. Discovery needs the key, so the key is what this returns.
type Resolution struct {
	Socket     string   `json:"socket"`
	Endpoint   string   `json:"endpoint"`
	ConfigPath string   `json:"configPath"`
	ConfigHash string   `json:"configHash"`
	Running    bool     `json:"running"`
	Sources    []string `json:"sources"`
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "dir is required"})
		return
	}
	if !filepath.IsAbs(dir) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "dir must be absolute: " + dir,
		})
		return
	}
	out := resolveDir(unkeyed(s.paths), dir)

	// Live facts beat the file when the answer is this daemon. The info file
	// is written once at startup; the endpoint can be ephemeral, and a
	// daemon that knows it is listening should not have to dial itself to
	// find out.
	if out.Socket == s.paths.Socket {
		out.Running = true
		if s.endpoint != "" {
			out.Endpoint = s.endpoint
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// unkeyed strips the configuration key from this daemon's paths.
//
// s.paths is already keyed to the config this daemon loaded, so its Socket
// and Info name *this* daemon. Resolution has to start from the state
// directory instead: otherwise a directory with no configuration at all
// resolves to whichever daemon happened to be asked, which is precisely the
// wrong answer given confidently.
func unkeyed(p Paths) Paths {
	return Paths{
		State:  p.State,
		Cache:  p.Cache,
		Socket: socketPath(p.State, ""),
		Info:   filepath.Join(p.State, "daemon.json"),
	}
}

// resolveDir answers for a directory without changing this process's.
//
// A chdir would be a race against every other request in flight, so the
// search path is computed for the directory instead. The directory is
// resolved through symlinks first, for the same reason FingerprintConfig
// resolves the files it hashes: /tmp is a symlink to /private/tmp on macOS,
// and a caller whose $PWD holds the unresolved spelling would otherwise get
// a different key for the same project and be told no daemon is running.
func resolveDir(base Paths, dir string) Resolution {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	out := Resolution{}
	cfg, err := config.LoadFrom("", dir)
	if err != nil || cfg == nil || len(cfg.Sources) == 0 {
		// No configuration anywhere above this directory. The unkeyed
		// default daemon is what mcpx would use, so that is the answer.
		out.Socket = base.Socket
		out.Running = listening(base.Socket)
		if info, err := base.ReadInfo(); err == nil {
			out.Endpoint = info.Endpoint
		}
		return out
	}
	out.ConfigPath = cfg.Path
	out.Sources = cfg.Sources
	out.ConfigHash = FingerprintConfig(cfg.Sources)

	keyed := base.ForConfig(out.ConfigHash)
	out.Socket = keyed.Socket
	if info, err := keyed.ReadInfo(); err == nil {
		// The info file knows where the daemon actually bound, which is not
		// always where the key says: a state path too long for sun_path
		// moves the socket to a private runtime directory.
		if info.Socket != "" {
			out.Socket = info.Socket
		}
		out.Endpoint = info.Endpoint
	}
	out.Running = listening(out.Socket)
	return out
}

// listening reports whether anything accepts on a unix socket.
//
// A socket file outlives the process that made it, so its existence proves
// nothing; connecting proves it. The connection is closed immediately and no
// request is sent -- accept() succeeding is the whole question.
func listening(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	c, err := net.DialTimeout("unix", path, defaults.ResolveDialTimeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
