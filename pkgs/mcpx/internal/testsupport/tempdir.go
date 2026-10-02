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
