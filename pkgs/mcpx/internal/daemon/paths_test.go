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
