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

	mcpxOnce sync.Once
	mcpxBin  string
	mcpxErr  error
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

// MCPXBinary compiles the mcpx command once per test binary.
//
// Every end-to-end test needs the real binary, and building it per test meant
// roughly a hundred `go build` invocations in one package. Go runs package
// test binaries concurrently, so under a full `go test ./...` that load was
// enough to push start timeouts over the edge -- producing a different
// spurious failure on each run, which is the signature that sends someone
// hunting a logic bug that is not there.
func MCPXBinary(t *testing.T) string {
	t.Helper()
	mcpxOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mcpx-bin-")
		if err != nil {
			mcpxErr = err
			return
		}
		mcpxBin = filepath.Join(dir, "mcpx")
		cmd := exec.Command("go", "build", "-o", mcpxBin, "./cmd/mcpx")
		cmd.Dir = repoRoot()
		out, err := cmd.CombinedOutput()
		if err != nil {
			mcpxErr = &buildFailure{err: err, out: string(out)}
		}
	})
	if mcpxErr != nil {
		t.Fatalf("build mcpx: %v", mcpxErr)
	}
	return mcpxBin
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
