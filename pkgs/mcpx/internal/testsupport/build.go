// Package testsupport builds the fake MCP server used across mcpx tests.
package testsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	once     sync.Once
	binary   string
	buildErr error
)

// FakeMCPBinary compiles the fake MCP server once per test binary and returns
// its path.
func FakeMCPBinary(t *testing.T) string {
	t.Helper()
	once.Do(func() {
		dir, err := os.MkdirTemp("", "mcpx-fake-")
		if err != nil {
			buildErr = err
			return
		}
		binary = filepath.Join(dir, "fakemcp")
		cmd := exec.Command("go", "build", "-o", binary, "./internal/testsupport/fakemcp")
		cmd.Dir = repoRoot()
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = &buildFailure{err: err, out: string(out)}
		}
	})
	if buildErr != nil {
		t.Fatalf("build fakemcp: %v", buildErr)
	}
	return binary
}

type buildFailure struct {
	err error
	out string
}

func (b *buildFailure) Error() string { return b.err.Error() + "\n" + b.out }

// repoRoot walks up from the working directory to the module root.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
