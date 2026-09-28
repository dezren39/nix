package daemon_test

import (
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
	if !strings.Contains(p.Socket, "mcpx-") {
		t.Fatalf("fallback socket should be recognisable: %q", p.Socket)
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
