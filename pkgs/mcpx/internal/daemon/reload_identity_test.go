package daemon

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// copyProgram writes a fresh copy of the fake MCP server to dst. extra bytes
// are appended after the ELF image, which the loader ignores, so the copy runs
// exactly as the original does while its contents differ.
func copyProgram(t *testing.T, src, dst string, extra []byte) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, extra...)
	if err := os.WriteFile(dst, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// replaceProgram puts a new file at path the way an installer or a nix
// profile does: a new inode, renamed into place, so the running child keeps
// the old file it already has open.
func replaceProgram(t *testing.T, src, path string, extra []byte) {
	t.Helper()
	tmp := path + ".new"
	copyProgram(t, src, tmp, extra)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestReloadRestartsOnlyTheServerWhoseEnvOrCwdChanged(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	other := t.TempDir()
	cases := map[string]func(b *config.Server){
		"env": func(b *config.Server) { b.Env = map[string]string{"TOKEN": "rotated"} },
		"cwd": func(b *config.Server) { b.Cwd = other },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			cfgOf := func(changed bool) *config.Config {
				b := &config.Server{Name: "b", Command: fake, Env: map[string]string{"TOKEN": "first"}}
				if changed {
					change(b)
				}
				return &config.Config{MCPServers: map[string]*config.Server{
					"a": {Name: "a", Command: fake},
					"b": b,
				}}
			}
			r, err := NewRegistry(cfgOf(false), Paths{State: dir, Cache: dir}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			pa, _ := r.Pool("a")
			pb, _ := r.Pool("b")
			pidA, pidB := childPID(t, pa), childPID(t, pb)

			if _, _, err := r.Reload(cfgOf(true)); err != nil {
				t.Fatal(err)
			}
			pa2, _ := r.Pool("a")
			pb2, _ := r.Pool("b")
			if pa2 != pa || childPID(t, pa2) != pidA {
				t.Fatalf("a server whose %s did not change was restarted", name)
			}
			if pb2 == pb {
				t.Fatalf("a server whose %s changed kept its pool", name)
			}
			if got := childPID(t, pb2); got == pidB {
				t.Fatalf("a server whose %s changed kept child pid %d", name, pidB)
			}
			waitGone(t, pidB)
		})
	}
}

// The command is the same string and the configuration is byte-identical; only
// the program at that path changed. The pool must be replaced, and the server
// beside it must not.
func TestReloadRestartsAPoolWhoseBinaryChangedAtTheSamePath(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-server")
	copyProgram(t, fake, bin, nil)

	cfg := func() *config.Config {
		return &config.Config{MCPServers: map[string]*config.Server{
			"a": {Name: "a", Command: fake},
			"b": {Name: "b", Command: bin},
		}}
	}
	r, err := NewRegistry(cfg(), Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pa, _ := r.Pool("a")
	pb, _ := r.Pool("b")
	pidA, pidB := childPID(t, pa), childPID(t, pb)
	identityBefore := pb.ExecIdentity()
	if identityBefore == "" {
		t.Fatal("the pool does not record the identity it was started with")
	}

	// An unchanged file reloads to the same pool and the same child.
	if _, _, err := r.Reload(cfg()); err != nil {
		t.Fatal(err)
	}
	if pb2, _ := r.Pool("b"); pb2 != pb || childPID(t, pb2) != pidB {
		t.Fatal("reloading an unchanged binary restarted its server")
	}

	replaceProgram(t, fake, bin, []byte("new build"))
	if _, _, err := r.Reload(cfg()); err != nil {
		t.Fatal(err)
	}
	pb2, _ := r.Pool("b")
	if pb2 == pb {
		t.Fatal("a binary replaced at the same path did not get a new pool")
	}
	if pb2.ExecIdentity() == identityBefore {
		t.Fatal("the new pool records the old identity")
	}
	if got := childPID(t, pb2); got == pidB {
		t.Fatalf("the replaced binary's child pid %d was kept", pidB)
	}
	waitGone(t, pidB)

	pa2, _ := r.Pool("a")
	if pa2 != pa || childPID(t, pa2) != pidA {
		t.Fatal("the server beside the replaced binary was restarted")
	}
}

// A symlink re-pointed at a byte-identical file runs the same program, so a
// reload keeps the pool and its child. Identity is by content for a small
// file, not by the path it was reached by. A link re-pointed at a file with
// different bytes is a different program and gets a new pool.
//
// Caveat: identity is the bytes of the file and nothing else. A program that
// finds its own files relative to its real location, such as a node script
// beside its node_modules, is kept when the link points at an identical copy
// in another directory, and its child keeps the dependencies of the old
// location. An operator who wants the new location in use restarts the server
// (mcpx restart). Above limits.execHashMax the identity is the inode instead,
// so an oversized copy is restarted; see
// TestReloadRestartsAnOversizedProgramRePointedAtACopy.
func TestReloadFollowsAForRePointedCommandByWhatItRuns(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	other := filepath.Join(dir, "other")
	link := filepath.Join(dir, "mcp-server")
	writeWrapper(t, fake, first, "same program")
	writeWrapper(t, fake, second, "same program")
	writeWrapper(t, fake, other, "a different program")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}

	cfg := func() *config.Config {
		return &config.Config{MCPServers: map[string]*config.Server{
			"b": {Name: "b", Command: link},
		}}
	}
	r, err := NewRegistry(cfg(), Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pb, _ := r.Pool("b")
	pid := childPID(t, pb)

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
	if _, _, err := r.Reload(cfg()); err != nil {
		t.Fatal(err)
	}
	if pb2, _ := r.Pool("b"); pb2 != pb || childPID(t, pb2) != pid {
		t.Fatal("a command re-pointed at byte-identical bytes was restarted")
	}

	repoint(other)
	if _, _, err := r.Reload(cfg()); err != nil {
		t.Fatal(err)
	}
	if pb2, _ := r.Pool("b"); pb2 == pb || childPID(t, pb2) == pid {
		t.Fatal("a command re-pointed at different bytes kept its old child")
	}
	waitGone(t, pid)
}

// The inverse boundary. A full-size program is identified by its inode, so a
// link re-pointed at a byte-identical copy of it is a different program and
// the pool is replaced. This is the behaviour the re-pointed-command test
// used to pin before identity was by content; it still holds above
// limits.execHashMax, and the test keeps it so that the size rule is checked
// and not just assumed.
func TestReloadRestartsAnOversizedProgramRePointedAtACopy(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	link := filepath.Join(dir, "mcp-server")
	copyProgram(t, fake, first, nil)
	copyProgram(t, fake, second, nil)
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() <= defaults.ExecHashMax {
		t.Fatalf("the test program is %d bytes, not over limits.execHashMax (%d)", st.Size(), defaults.ExecHashMax)
	}
	// The copy is byte-identical, so the restart can only come from the size rule.
	a, errA := os.ReadFile(first)
	b, errB := os.ReadFile(second)
	if errA != nil || errB != nil || !bytes.Equal(a, b) {
		t.Fatal("the oversized copy is not byte-identical to the program it replaces")
	}

	cfg := func() *config.Config {
		return &config.Config{MCPServers: map[string]*config.Server{
			"b": {Name: "b", Command: link},
		}}
	}
	r, err := NewRegistry(cfg(), Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pb, _ := r.Pool("b")
	pid := childPID(t, pb)

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Reload(cfg()); err != nil {
		t.Fatal(err)
	}
	if pb2, _ := r.Pool("b"); pb2 == pb || childPID(t, pb2) == pid {
		t.Fatal("a re-pointed command over the hash limit kept its old child")
	}
	waitGone(t, pid)
}

// A pool recorded before the binary was replaced must not be handed to a
// successor that finds the new binary: the successor's children would be the
// old program. Detach records the identity the pool was made with.
func TestHandoffRecordsTheIdentityAPoolWasMadeWith(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-server")
	copyProgram(t, fake, bin, nil)
	cfg := &config.Config{MCPServers: map[string]*config.Server{"b": {Name: "b", Command: bin}}}

	old, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pb, _ := old.Pool("b")
	childPID(t, pb)
	recorded := pb.PoolID()

	replaceProgram(t, fake, bin, []byte("new build"))
	ho, err := old.Detach(0)
	if err != nil {
		t.Fatal(err)
	}
	ho.Commit()
	if len(ho.Pools) != 1 {
		t.Fatalf("the handoff carries %d pools, want the one with a child", len(ho.Pools))
	}
	if ho.Pools[0].PoolID != recorded {
		t.Fatalf("the handoff names %q, the pool was made as %q", ho.Pools[0].PoolID, recorded)
	}

	next, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	ad, err := next.Adopt(ho.Pools)
	if err != nil {
		t.Fatal(err)
	}
	ad.Attach()
	pn, _ := next.Pool("b")
	if pn.PoolID() == recorded {
		t.Fatal("the successor kept a pool made with the old binary")
	}
	if got := childPID(t, pn); got == 0 || syscall.Kill(got, 0) != nil {
		t.Fatal("the successor could not start a child for the new binary")
	}
}

// writeWrapper writes a small executable that runs the fake MCP server. It
// execs the fake, so the child process is the fake and keeps its pid. The tag
// is a comment: two wrappers with the same tag are byte-identical, and under the
// hash limit, which is when identity is by content. The fake itself is over
// that limit and is identified by its inode, which the first test covers.
func writeWrapper(t *testing.T, fake, path, tag string) {
	t.Helper()
	body := fmt.Sprintf("#!/bin/sh\n# %s\nexec '%s' \"$@\"\n", tag, fake)
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}
