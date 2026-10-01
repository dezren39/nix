package testsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	taskOnce sync.Once
	taskBin  string
	taskErr  error
)

// TaskMCPBinary compiles taskmcp, the server whose tools are the official
// conformance suite's tasks-extension fixtures; see its package doc.
func TaskMCPBinary(t *testing.T) string {
	t.Helper()
	taskOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mcpx-task-")
		if err != nil {
			taskErr = err
			return
		}
		taskBin = filepath.Join(dir, "taskmcp")
		cmd := exec.Command("go", "build", "-o", taskBin, "./internal/testsupport/taskmcp")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			taskErr = &buildFailure{err: err, out: string(out)}
		}
	})
	if taskErr != nil {
		t.Fatalf("build taskmcp: %v", taskErr)
	}
	return taskBin
}
