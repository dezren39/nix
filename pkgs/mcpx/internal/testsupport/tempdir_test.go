package testsupport_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/testsupport"
)

// mcpx finds config, scripts and recipes by walking up from the working
// directory. A temporary directory under the user's home therefore reaches
// the user's own ~/.config/mcpx whatever HOME is set to -- the walk keys on
// the path, not the variable -- and the developer's scripts joined every
// test's fixtures. TMPDIR is under home on this machine (opencode sets it to
// ~/.local/share/opencode/tmp) and is not in CI, which is why those tests
// passed there and failed here.
func TestTempDirIsNeverInsideHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	// A TMPDIR under home, as opencode's is: t.TempDir would answer inside it.
	tmp := filepath.Join(home, ".cache", "mcpx-tempdir-test")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	t.Setenv("TMPDIR", tmp)

	dir := testsupport.TempDir(t)
	real := testsupport.Resolved(t, home)
	if rel, err := filepath.Rel(real, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Errorf("TempDir returned %q, inside the home directory %q: the upward walk will reach the user's own config", dir, real)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("TempDir must still be a usable directory: %v", err)
	}
}

// SocketDir must stay short even when TMPDIR is a long, deeply nested
// directory (a nix sandbox build's is): the whole point is that a socket
// bound inside it fits Darwin's 104-byte sockaddr_un limit (#434).
func TestSocketDirStaysShortUnderALongTmpDir(t *testing.T) {
	long := filepath.Join(os.TempDir(), strings.Repeat("a-very-long-directory-name/", 6))
	t.Setenv("TMPDIR", long)
	t.Setenv("GOTMPDIR", long)

	dir := testsupport.SocketDir(t)
	if strings.HasPrefix(dir, long) {
		t.Errorf("SocketDir returned %q, inside the long TMPDIR %q", dir, long)
	}
	if n := len(filepath.Join(dir, "daemon.sock")); n > 100 {
		t.Errorf("a daemon.sock inside SocketDir is %d bytes, over the 100-byte unix socket ceiling", n)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("SocketDir must be a usable directory: %v", err)
	}
}
