package testsupport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TempDir is t.TempDir with its symlinks resolved, and never inside the
// user's home directory.
//
// The product canonicalises the paths it reports (config.Load walks from
// os.Getwd, which the kernel answers with the physical path, and
// FingerprintConfig resolves deliberately). On macOS both /tmp and /var --
// so both a TMPDIR of /tmp and the stock /var/folders/... -- are symlinks
// into /private, so a test comparing an unresolved t.TempDir against a path
// the product returned fails on every Mac and passes on Linux (#281).
//
// Outside home, because config and script discovery walk upward from the
// working directory. When TMPDIR is under $HOME -- opencode sets it to
// ~/.local/share/opencode/tmp -- that walk reaches the real ~/.config/mcpx,
// and its scripts and config leak into tests whatever $HOME is set to: the
// walk keys on the path, not the variable. A user's greet.ts outranked a
// test's own recipe that way, and passed in CI only because CI's TMPDIR is
// not under home.
func TempDir(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	if home, err := os.UserHomeDir(); err == nil && within(Resolved(t, dir), Resolved(t, home)) {
		out, err := os.MkdirTemp("/tmp", "mcpx-test-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(out) })
		dir = out
	}
	return Resolved(t, dir)
}

// SocketDir is a scratch directory short enough to bind a unix socket in,
// however deep the ambient TMPDIR is.
//
// t.TempDir() nests under TMPDIR as <tmp>/<TestName><random>/001, and a nix
// sandbox build's TMPDIR is long and deeply nested itself, so a socket path
// built on it easily exceeds Darwin's 104-byte sockaddr_un limit (100 usable,
// the ceiling daemon.maxSocketPath uses) and bind fails with an opaque
// EINVAL -- in a test whose subject has nothing to do with socket paths
// (#434). The product's own sockets dodge this via daemon.socketPath's
// hashed fallback; a test that binds a socket directly has no such layer,
// so it needs the short directory itself. Like TempDir's out-of-home
// fallback above, the directory is created directly under /tmp, resolved,
// and fails the test if a daemon.sock inside it would not fit.
func SocketDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mcpx-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir = Resolved(t, dir)
	if n := len(filepath.Join(dir, "daemon.sock")); n > 100 {
		t.Fatalf("socket directory %q leaves a %d-byte daemon.sock path, over the 100-byte unix socket ceiling", dir, n)
	}
	return dir
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Resolved is dir with its symlinks resolved, failing the test if it cannot be.
func Resolved(t testing.TB, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}
