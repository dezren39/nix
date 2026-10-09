package daemon

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// isolate removes every ambient configuration source so a test sees only the
// files it wrote. Without this the developer's own ~/.mcpx.json contributes
// to every fingerprint, and the test passes or fails depending on whose
// machine it runs on.
//
// The fake home is created under /tmp (not t.TempDir, and not os.TempDir):
// the upward config walk checks $dir/.config/mcpx/config.json at EVERY
// ancestor level, so a temp dir nested anywhere under the real $HOME —
// including via TMPDIR — walks straight through the real home and picks up
// the developer's own config, changing every daemon key. With the fake home
// outside the real $HOME tree the walk can never reach it.
//
// GOTMPDIR (not TMPDIR) is what testing.TempDir consults, and it is read
// once per test: every t.TempDir in the test then nests under the fake home,
// keeping the whole test tree outside the real $HOME.
func isolate(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "mcpx-test-home-")
	if err != nil {
		t.Fatal(err)
	}
	// Through the symlink, because /tmp is one to /private/tmp on macOS and
	// everything that reports a config path back has already resolved it --
	// FingerprintConfig does it deliberately, for the reason recorded there.
	// Without this the fake home is /tmp/... while every path the resolver
	// answers with is /private/tmp/..., and the two never compare equal, so
	// the test fails on every developer machine and passes on Linux CI.
	if real, rerr := filepath.EvalSymlinks(home); rerr == nil {
		home = real
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("MCPX_CONFIG", "")
	t.Setenv("MCPX_SOCKET", "")
	t.Setenv("GOTMPDIR", home)
	t.Setenv("TMPDIR", home)
	return home
}

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, ".mcpx.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const emptyConfig = `{"mcpServers":{}}`

func TestResolveNamesTheDaemonKeyedToTheNearestConfig(t *testing.T) {
	isolate(t)
	state := t.TempDir()
	base := Paths{State: state, Socket: filepath.Join(state, "daemon.sock")}

	project := t.TempDir()
	cfg := writeConfig(t, project, emptyConfig)

	got := resolveDir(base, project)
	if got.ConfigPath != cfg {
		t.Errorf("configPath = %q, want %q", got.ConfigPath, cfg)
	}
	want := FingerprintConfig([]string{cfg})
	if got.ConfigHash != want {
		t.Errorf("configHash = %q, want %q", got.ConfigHash, want)
	}
	if got.Socket != base.ForConfig(want).Socket {
		t.Errorf("socket = %q, want %q", got.Socket, base.ForConfig(want).Socket)
	}
	if got.Running {
		t.Error("nothing is listening, so running must be false")
	}
}

// The case the fingerprint exists for: a subdirectory with no config of its
// own belongs to the daemon of the config it inherits. A resolver that only
// looked for the nearest .mcpx.json would answer "none" here.
func TestResolveInheritsAParentsConfig(t *testing.T) {
	isolate(t)
	state := t.TempDir()
	base := Paths{State: state, Socket: filepath.Join(state, "daemon.sock")}

	root := t.TempDir()
	writeConfig(t, root, emptyConfig)
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	if resolveDir(base, child).ConfigHash != resolveDir(base, root).ConfigHash {
		t.Error("a child directory must resolve to the same daemon as its config's directory")
	}
}

// Two projects whose configuration is byte-identical are still two projects.
// Sharing a daemon between them would give one project the other's servers as
// soon as either inherited anything.
func TestResolveKeepsIdenticalConfigsInDifferentPlacesApart(t *testing.T) {
	isolate(t)
	state := t.TempDir()
	base := Paths{State: state, Socket: filepath.Join(state, "daemon.sock")}

	one := t.TempDir()
	two := t.TempDir()
	writeConfig(t, one, emptyConfig)
	writeConfig(t, two, emptyConfig)

	if resolveDir(base, one).ConfigHash == resolveDir(base, two).ConfigHash {
		t.Error("identical configs in different directories must not share a daemon")
	}
}

func TestResolveReportsRunningOnlyWhenSomethingAccepts(t *testing.T) {
	isolate(t)
	state := t.TempDir()
	base := Paths{State: state, Socket: filepath.Join(state, "daemon.sock")}
	project := t.TempDir()
	cfg := writeConfig(t, project, emptyConfig)

	keyed := base.ForConfig(FingerprintConfig([]string{cfg}))

	// A socket file with nothing behind it: exactly what a crashed daemon
	// leaves, and exactly the thing discovery must not offer as a candidate.
	if err := os.WriteFile(keyed.Socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if resolveDir(base, project).Running {
		t.Error("a socket file with no listener must not be reported as running")
	}
	if err := os.Remove(keyed.Socket); err != nil {
		t.Fatal(err)
	}

	l, err := net.Listen("unix", keyed.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !resolveDir(base, project).Running {
		t.Error("a listening socket must be reported as running")
	}
}

// The info file, not the key, says where the daemon actually bound: a state
// path too long for sun_path moves the socket to a private runtime
// directory, and a client that trusted the key would dial nothing.
func TestResolvePrefersTheSocketTheInfoFileRecords(t *testing.T) {
	isolate(t)
	state := t.TempDir()
	base := Paths{State: state, Socket: filepath.Join(state, "daemon.sock")}
	project := t.TempDir()
	cfg := writeConfig(t, project, emptyConfig)

	keyed := base.ForConfig(FingerprintConfig([]string{cfg}))
	elsewhere := filepath.Join(t.TempDir(), "d.sock")
	if err := keyed.WriteInfo(Info{Socket: elsewhere, Endpoint: "http://127.0.0.1:9", ConfigPath: cfg}); err != nil {
		t.Fatal(err)
	}

	got := resolveDir(base, project)
	if got.Socket != elsewhere {
		t.Errorf("socket = %q, want the one in the info file %q", got.Socket, elsewhere)
	}
	if got.Endpoint != "http://127.0.0.1:9" {
		t.Errorf("endpoint = %q, want the one in the info file", got.Endpoint)
	}
}

func TestResolveRejectsAMissingOrRelativeDirectory(t *testing.T) {
	isolate(t)
	s := &Server{paths: Paths{State: t.TempDir()}}
	for _, q := range []string{"", "?dir=relative/path"} {
		rec := httptest.NewRecorder()
		s.handleResolve(rec, httptest.NewRequest("GET", "/v1/resolve"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("dir%q: status %d, want 400", q, rec.Code)
		}
	}
}

func TestResolveOverHTTPAnswersForTheAskingDaemonItself(t *testing.T) {
	isolate(t)
	state := t.TempDir()
	project := t.TempDir()
	cfg := writeConfig(t, project, emptyConfig)

	base := Paths{State: state, Socket: filepath.Join(state, "daemon.sock")}
	keyed := base.ForConfig(FingerprintConfig([]string{cfg}))

	// A daemon that is this project's daemon reports itself running without
	// dialing its own socket -- it is inside the process that would answer.
	s := &Server{paths: keyed, endpoint: "http://127.0.0.1:4242"}
	rec := httptest.NewRecorder()
	s.handleResolve(rec, httptest.NewRequest("GET", "/v1/resolve?dir="+project, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got Resolution
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Running {
		t.Error("a daemon asked about its own directory must report running")
	}
	if got.Endpoint != "http://127.0.0.1:4242" {
		t.Errorf("endpoint = %q, want the live one", got.Endpoint)
	}
	if len(got.Sources) != 1 || got.Sources[0] != cfg {
		t.Errorf("sources = %v, want [%s]", got.Sources, cfg)
	}
}

// A directory with no configuration anywhere above it belongs to the unkeyed
// default daemon -- not to whichever daemon happened to be asked. Answering
// with the asked daemon's own socket would be a confident wrong answer, and
// the caller has no way to tell it from a right one.
func TestResolveForADirectoryWithNoConfigNamesTheDefaultDaemon(t *testing.T) {
	home := isolate(t)
	state := t.TempDir()
	project := t.TempDir()
	cfg := writeConfig(t, project, emptyConfig)

	// A daemon serving some other project, asked about a directory that has
	// no configuration of its own.
	s := &Server{paths: Paths{State: state}.ForConfig(FingerprintConfig([]string{cfg})), endpoint: "http://127.0.0.1:1"}
	rec := httptest.NewRecorder()
	s.handleResolve(rec, httptest.NewRequest("GET", "/v1/resolve?dir="+home, nil))

	var got Resolution
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Socket == s.paths.Socket {
		t.Errorf("resolve named the asking daemon's own socket %q for a directory it does not serve", got.Socket)
	}
	if got.ConfigHash != "" {
		t.Errorf("configHash = %q, want empty: nothing contributed", got.ConfigHash)
	}
	if got.Running {
		t.Error("no default daemon is running")
	}
}
