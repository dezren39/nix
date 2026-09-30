package runner

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentRunsInASharedWorkDirDoNotClobberEachOther(t *testing.T) {
	// The daemon's /v1/exec has always used one directory for every run, so
	// that the runtime's type-check cache keeps the generated client. With a
	// fixed "script.ts" in it, two concurrent runs wrote the same path and
	// one could execute the other's program.
	dir := t.TempDir()
	var wg sync.WaitGroup
	seen := make([]string, 8)
	for i := range seen {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name, err := uniqueScriptName()
			if err != nil {
				t.Errorf("name: %v", err)
				return
			}
			seen[i] = name
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
				t.Errorf("write: %v", err)
			}
		}(i)
	}
	wg.Wait()
	uniq := map[string]bool{}
	for _, n := range seen {
		if n == "" || uniq[n] {
			t.Fatalf("names must be unique per run, got %v", seen)
		}
		uniq[n] = true
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != len(seen) {
		t.Errorf("%d files for %d runs: one overwrote another", len(ents), len(seen))
	}
}
