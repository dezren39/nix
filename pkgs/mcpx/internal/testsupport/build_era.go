package testsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	eraOnce sync.Once
	eraBin  string
	eraErr  error
)

// EraMCPBinary compiles the fake server whose protocol era is chosen by
// ERAMCP_MODE. See internal/testsupport/eramcp.
func EraMCPBinary(t *testing.T) string {
	t.Helper()
	eraOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mcpx-era-")
		if err != nil {
			eraErr = err
			return
		}
		eraBin = filepath.Join(dir, "eramcp")
		cmd := exec.Command("go", "build", "-o", eraBin, "./internal/testsupport/eramcp")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			eraErr = &buildFailure{err: err, out: string(out)}
		}
	})
	if eraErr != nil {
		t.Fatalf("build eramcp: %v", eraErr)
	}
	return eraBin
}
