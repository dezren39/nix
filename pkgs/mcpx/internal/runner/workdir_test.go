package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

func TestPruneWorkDirsKeepsTheNewest(t *testing.T) {
	// One directory per distinct catalogue, and a catalogue changes whenever
	// a server updates its schema, so without a bound this grows for as long
	// as mcpx is used.
	root := t.TempDir()
	keep := filepath.Join(root, "keepme")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < defaults.ExecWorkDirs+5; i++ {
		d := filepath.Join(root, fmt.Sprintf("d%02d", i))
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		// Oldest first, so the low-numbered ones are the ones to lose.
		if err := os.Chtimes(d, now, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	pruneWorkDirs(root, keep)

	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) > defaults.ExecWorkDirs {
		t.Errorf("kept %d directories, want at most %d", len(ents), defaults.ExecWorkDirs)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the directory in use must never be pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "d00")); err == nil {
		t.Error("the oldest should have gone first")
	}
}

func TestPrunePermitsTheProgramInUse(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "script-aaaaaaaaaaaaaaaa.ts")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < defaults.ExecPrograms+3; i++ {
		p := filepath.Join(dir, fmt.Sprintf("script-%016d.ts", i))
		if err := os.WriteFile(p, []byte("y"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	// Not a program, and must survive: it is the thing the directory is for.
	client := filepath.Join(dir, clientFileName)
	if err := os.WriteFile(client, []byte("client"), 0o600); err != nil {
		t.Fatal(err)
	}
	prunePrograms(dir, keep)

	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the program being run must never be pruned: %v", err)
	}
	if _, err := os.Stat(client); err != nil {
		t.Errorf("the generated client is not a program and must survive: %v", err)
	}
}
