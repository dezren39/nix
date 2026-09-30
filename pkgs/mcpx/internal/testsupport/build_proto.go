package testsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	askOnce sync.Once
	askBin  string
	askErr  error
)

// AskMCPBinary compiles the fake MCP server that asks questions.
//
// Separate from fakemcp because the two are testing opposite things:
// fakemcp answers everything itself, which is what a proxy test needs, and
// nothing there ever elicits. Teaching it to would make every existing test
// carry a question it does not want.
func AskMCPBinary(t *testing.T) string {
	t.Helper()
	askOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mcpx-ask-")
		if err != nil {
			askErr = err
			return
		}
		askBin = filepath.Join(dir, "askmcp")
		cmd := exec.Command("go", "build", "-o", askBin, "./internal/testsupport/askmcp")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			askErr = &buildFailure{err: err, out: string(out)}
		}
	})
	if askErr != nil {
		t.Fatalf("build askmcp: %v", askErr)
	}
	return askBin
}
