package testsupport

import (
	"path/filepath"
	"testing"
)

// TempDir is t.TempDir with its symlinks resolved.
//
// The product canonicalises the paths it reports (config.Load walks from
// os.Getwd, which the kernel answers with the physical path, and
// FingerprintConfig resolves deliberately). On macOS both /tmp and /var --
// so both a TMPDIR of /tmp and the stock /var/folders/... -- are symlinks
// into /private, so a test comparing an unresolved t.TempDir against a path
// the product returned fails on every Mac and passes on Linux (#281).
func TempDir(t testing.TB) string {
	t.Helper()
	return Resolved(t, t.TempDir())
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
