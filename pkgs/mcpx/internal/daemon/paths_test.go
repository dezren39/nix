package daemon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/daemon"
)

func TestSocketStaysBesideStateWhenShort(t *testing.T) {
	t.Setenv("MCPX_SOCKET", "")
	t.Setenv("MCPX_STATE_DIR", "/tmp/s")
	t.Setenv("MCPX_CACHE_DIR", "/tmp/c")
	p := daemon.ResolvePaths()
	if p.Socket != filepath.Join("/tmp/s", "daemon.sock") {
		t.Fatalf("got %q", p.Socket)
	}
}

func TestSocketFallsBackWhenPathTooLongForSunPath(t *testing.T) {
	long := "/tmp/" + strings.Repeat("abcdefghij/", 12) + "state"
	t.Setenv("MCPX_SOCKET", "")
	t.Setenv("MCPX_STATE_DIR", long)
	t.Setenv("MCPX_CACHE_DIR", "/tmp/c")
	p := daemon.ResolvePaths()
	if strings.HasPrefix(p.Socket, long) {
		t.Fatalf("socket should not sit under an over-long state dir: %q", p.Socket)
	}
	if len(p.Socket) > 100 {
		t.Fatalf("fallback socket is still too long (%d bytes): %q", len(p.Socket), p.Socket)
	}
	// Recognisable as mcpx's, wherever it landed. On Linux XDG_RUNTIME_DIR
	// is set and the fallback goes to /run/user/<uid>/mcpx/; on macOS it goes
	// under the temp directory. Asserting one shape made this pass on the
	// machine it was written on and fail on the first Linux runner.
	if !strings.Contains(p.Socket, "mcpx") {
		t.Fatalf("fallback socket should be recognisable: %q", p.Socket)
	}
}

func TestTheFallbackSocketUsesXDGRuntimeDirWhenSet(t *testing.T) {
	// XDG_RUNTIME_DIR is already private to the user, which is exactly the
	// property the fallback needs.
	//
	// A short directory, deliberately. t.TempDir() on macOS is long enough
	// that the socket inside it exceeds the path limit too, and the code
	// then -- correctly -- falls through to the next candidate, which is not
	// what this test is about.
	dir, err := os.MkdirTemp("/tmp", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	long := "/tmp/" + strings.Repeat("abcdefghij/", 12) + "state"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("MCPX_SOCKET", "")
	t.Setenv("MCPX_STATE_DIR", long)
	t.Setenv("MCPX_CACHE_DIR", "/tmp/c")
	p := daemon.ResolvePaths()
	if !strings.HasPrefix(p.Socket, dir) {
		t.Errorf("with XDG_RUNTIME_DIR set, the socket should be under it: %q", p.Socket)
	}
}

func TestDistinctStateDirsGetDistinctSockets(t *testing.T) {
	long := func(n string) string {
		return "/tmp/" + strings.Repeat("abcdefghij/", 12) + n
	}
	t.Setenv("MCPX_SOCKET", "")
	t.Setenv("MCPX_CACHE_DIR", "/tmp/c")

	t.Setenv("MCPX_STATE_DIR", long("one"))
	a := daemon.ResolvePaths().Socket
	t.Setenv("MCPX_STATE_DIR", long("two"))
	b := daemon.ResolvePaths().Socket
	if a == b {
		t.Fatalf("two installations collapsed onto one socket: %q", a)
	}
}

func TestExplicitSocketOverrideWins(t *testing.T) {
	t.Setenv("MCPX_SOCKET", "/tmp/explicit.sock")
	t.Setenv("MCPX_STATE_DIR", "/tmp/s")
	if got := daemon.ResolvePaths().Socket; got != "/tmp/explicit.sock" {
		t.Fatalf("got %q", got)
	}
}

func TestHashConfigIsStableAndDiscriminating(t *testing.T) {
	a := daemon.HashConfig([]byte(`{"a":1}`))
	b := daemon.HashConfig([]byte(`{"a":1}`))
	c := daemon.HashConfig([]byte(`{"a":2}`))
	if a != b {
		t.Fatal("hash is not stable")
	}
	if a == c {
		t.Fatal("hash does not discriminate")
	}
	if len(a) != 16 {
		t.Fatalf("unexpected hash length %d", len(a))
	}
}

func TestOneConfigReachedThroughASymlinkIsOneDaemon(t *testing.T) {
	// /tmp -> /private/tmp on macOS made the same file two daemons, depending
	// only on how the caller's working directory happened to be spelled.
	real := t.TempDir()
	cfg := filepath.Join(real, ".mcpx.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "via-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	a := daemon.FingerprintConfig([]string{cfg})
	b := daemon.FingerprintConfig([]string{filepath.Join(link, ".mcpx.json")})
	if a != b {
		t.Errorf("one file, two keys: %s vs %s", a, b)
	}

	// And two genuinely different files with identical contents stay apart.
	other := filepath.Join(t.TempDir(), ".mcpx.json")
	if err := os.WriteFile(other, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if daemon.FingerprintConfig([]string{other}) == a {
		t.Error("identical contents in different places must not share a daemon")
	}
}

// ForConfig's key is the config file set, and both halves of that are
// load-bearing (#183). Stable across an edit is why a daemon that reloads stays
// reachable by the client that edited it (#56); unstable across the set is why
// two projects get two daemons.
func TestTheDaemonKeyIsTheFileSetNotTheContents(t *testing.T) {
	t.Setenv("MCPX_SOCKET", "")
	base := daemon.Paths{State: "/tmp/s"}
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".mcpx.json")
	write := func(p, body string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	keyed := func(files ...string) daemon.Paths {
		return base.ForConfig(daemon.FingerprintConfig(files))
	}

	write(cfg, `{"mcpServers":{}}`)
	before := keyed(cfg)
	write(cfg, `{"mcpServers":{"added":{"command":"x"}}}`)
	after := keyed(cfg)
	if before != after {
		t.Errorf("editing a config moved the daemon's key, so the daemon that "+
			"reloads it becomes unreachable:\n  before %+v\n  after  %+v", before, after)
	}

	parent := filepath.Join(t.TempDir(), "mcpx.json")
	write(parent, `{"mcpServers":{}}`)
	if chained := keyed(cfg, parent); chained == after {
		t.Errorf("adding a file to the chain kept the key; two file sets would share a daemon: %+v", chained)
	}
	other := filepath.Join(t.TempDir(), ".mcpx.json")
	write(other, `{"mcpServers":{"added":{"command":"x"}}}`)
	if elsewhere := keyed(other); elsewhere == after {
		t.Errorf("a different path with the same contents kept the key: %+v", elsewhere)
	}
}
