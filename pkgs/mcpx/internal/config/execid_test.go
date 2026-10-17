package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/defaults"
)

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestExecIdentityFollowsTheExecutableNotTheCommandName(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "tool")
	writeExec(t, bin, "#!/bin/sh\necho one\n")
	r := &Resolved{Server: &Server{Command: "tool", Env: map[string]string{"PATH": filepath.Join(dir, "bin")}}}

	first := r.ExecIdentity()
	sum := sha256.Sum256([]byte("#!/bin/sh\necho one\n"))
	if want := "sha256:" + hex.EncodeToString(sum[:])[:16]; first != want {
		t.Fatalf("identity %q of a small file is not its content hash %q", first, want)
	}
	if st, ok := r.ExecStamp(); !ok || st.Path != bin {
		t.Fatalf("stamp %+v does not name the resolved file %s", st, bin)
	}
	if again := r.ExecIdentity(); again != first {
		t.Fatalf("identity moved with nothing changed: %q then %q", first, again)
	}
	writeExec(t, bin, "#!/bin/sh\necho two\n")
	if changed := r.ExecIdentity(); changed == first {
		t.Fatal("a rewritten executable kept its identity")
	}
}

func TestExecIdentityResolvesAgainstTheServersOwnPath(t *testing.T) {
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "elsewhere", "tool"), "#!/bin/sh\n")
	r := &Resolved{Server: &Server{Command: "tool", Env: map[string]string{"PATH": filepath.Join(dir, "elsewhere")}}}
	if st, ok := r.ExecStamp(); !ok || st.Path != filepath.Join(dir, "elsewhere", "tool") {
		t.Fatalf("stamp %+v ignores the PATH the server is given", st)
	}
}

// A symlink re-pointed at a byte-identical file runs the same program, so it
// keeps its identity; the stamp still moves, which is what makes the check
// look again.
func TestExecIdentityOfASmallFileIsItsContentNotItsPath(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	third := filepath.Join(dir, "third")
	writeExec(t, first, "#!/bin/sh\necho same\n")
	writeExec(t, second, "#!/bin/sh\necho same\n")
	writeExec(t, third, "#!/bin/sh\necho other\n")
	link := filepath.Join(dir, "tool")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	r := &Resolved{Server: &Server{Command: link}}
	before, _ := r.ExecStamp()
	id := r.ExecIdentity()

	repoint := func(to string) {
		t.Helper()
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(to, link); err != nil {
			t.Fatal(err)
		}
	}
	repoint(second)
	if after, _ := r.ExecStamp(); after == before {
		t.Fatal("re-pointing the link did not move the stamp")
	}
	if got := r.ExecIdentity(); got != id {
		t.Fatalf("a link re-pointed at identical bytes changed identity: %q -> %q", id, got)
	}
	repoint(third)
	if got := r.ExecIdentity(); got == id {
		t.Fatal("a link re-pointed at different bytes kept its identity")
	}
}

// A file above the hashing limit is told apart by its inode, not its bytes:
// two files with the same content are still different programs to the pool.
func TestExecIdentityOfALargeFileIsItsInode(t *testing.T) {
	old := defaults.ExecHashMax
	defaults.ExecHashMax = 4
	t.Cleanup(func() { defaults.ExecHashMax = old })

	dir := t.TempDir()
	bin := filepath.Join(dir, "tool")
	writeExec(t, bin, "#!/bin/sh\necho same\n")
	r := &Resolved{Server: &Server{Command: bin}}
	id := r.ExecIdentity()
	if !strings.HasPrefix(id, "stat:") {
		t.Fatalf("identity %q of a file over the hash limit is not a stat identity", id)
	}
	if again := r.ExecIdentity(); again != id {
		t.Fatalf("stat identity moved with nothing changed: %q then %q", id, again)
	}

	// A second file with the same bytes is a different program: the identity
	// is the file, not what it holds, above the limit.
	twin := filepath.Join(dir, "twin")
	writeExec(t, twin, "#!/bin/sh\necho same\n")
	if got := (&Resolved{Server: &Server{Command: twin}}).ExecIdentity(); got == id {
		t.Fatal("a large file with the same bytes as another kept its identity")
	}

	// A replacement at the same path with the same bytes is a new inode.
	tmp := filepath.Join(dir, "tool.new")
	writeExec(t, tmp, "#!/bin/sh\necho same\n")
	if err := os.Rename(tmp, bin); err != nil {
		t.Fatal(err)
	}
	if got := r.ExecIdentity(); got == id {
		t.Fatal("a replaced large file kept its identity")
	}
}

func TestExecIdentityOfAMissingCommandIsItsOwnIdentity(t *testing.T) {
	r := &Resolved{Server: &Server{Command: "no-such-mcpx-tool-anywhere", Env: map[string]string{"PATH": t.TempDir()}}}
	if id := r.ExecIdentity(); !strings.HasPrefix(id, "missing:") {
		t.Fatalf("identity %q for a command that does not resolve", id)
	}
}

func TestAServerThatIsNotAChildHasNoExecIdentity(t *testing.T) {
	r := &Resolved{Server: &Server{URL: "https://example.invalid/mcp"}}
	if id := r.ExecIdentity(); id != "" {
		t.Fatalf("a remote server has identity %q", id)
	}
	if r.ProcessID() != r.PoolID()+"/" {
		t.Fatalf("ProcessID %q is not PoolID plus the identity", r.ProcessID())
	}
}
