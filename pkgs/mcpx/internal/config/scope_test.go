package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

func ctx() config.CallContext {
	return config.CallContext{Cwd: "/tmp/x", SessionID: "s1", ParentSessionID: "p1", PID: 4242, CallID: "c1"}
}

func TestGlobalScopeIsOneKeyForEveryone(t *testing.T) {
	a, _ := config.ScopeGlobal.Key(ctx())
	b, _ := config.ScopeGlobal.Key(config.CallContext{Cwd: "/other", SessionID: "s2", CallID: "c2"})
	if a != b {
		t.Fatalf("global must collapse to one key: %q vs %q", a, b)
	}
}

func TestCallScopeIsUniquePerInvocation(t *testing.T) {
	a, _ := config.ScopeCall.Key(config.CallContext{CallID: "c1"})
	b, _ := config.ScopeCall.Key(config.CallContext{CallID: "c2"})
	if a == b {
		t.Fatal("call scope must not collide across invocations")
	}
}

func TestSessionScopeSeparatesSessions(t *testing.T) {
	a, deg := config.ScopeSession.Key(config.CallContext{SessionID: "s1", CallID: "c1"})
	if deg != "" {
		t.Fatalf("a present session id should not degrade: %s", deg)
	}
	b, _ := config.ScopeSession.Key(config.CallContext{SessionID: "s2", CallID: "c2"})
	if a == b {
		t.Fatal("different sessions must get different keys")
	}
	// The same session from two different calls is the same key. This is the
	// property that lets a subagent keep one browser across several runs.
	c, _ := config.ScopeSession.Key(config.CallContext{SessionID: "s1", CallID: "c9"})
	if a != c {
		t.Fatalf("one session must be stable across calls: %q vs %q", a, c)
	}
}

func TestSessionScopeDegradesToPerCallWithoutAnID(t *testing.T) {
	a, deg := config.ScopeSession.Key(config.CallContext{CallID: "c1"})
	if deg == "" {
		t.Fatal("a missing session id must be reported, not silently shared")
	}
	if !strings.Contains(deg, "MCPX_SESSION_ID") {
		t.Errorf("the reason should say how to fix it: %q", deg)
	}
	b, _ := config.ScopeSession.Key(config.CallContext{CallID: "c2"})
	if a == b {
		t.Fatal("degrading must isolate per call, never share")
	}
}

func TestParentSessionScopeGroupsSubagents(t *testing.T) {
	parent, _ := config.ScopeParentSession.Key(
		config.CallContext{SessionID: "sub-1", ParentSessionID: "root", CallID: "c1"})
	sibling, _ := config.ScopeParentSession.Key(
		config.CallContext{SessionID: "sub-2", ParentSessionID: "root", CallID: "c2"})
	if parent != sibling {
		t.Fatal("two subagents of one parent must share a key")
	}
	other, _ := config.ScopeParentSession.Key(
		config.CallContext{SessionID: "sub-3", ParentSessionID: "elsewhere", CallID: "c3"})
	if parent == other {
		t.Fatal("different parents must not share")
	}
}

func TestParentSessionFallsBackToSession(t *testing.T) {
	got, deg := config.ScopeParentSession.Key(config.CallContext{SessionID: "s1", CallID: "c1"})
	if deg == "" {
		t.Fatal("the fallback should be reported")
	}
	want, _ := config.ScopeSession.Key(config.CallContext{SessionID: "s1", CallID: "c1"})
	if got != want {
		t.Fatalf("should fall back to the session key: %q vs %q", got, want)
	}
}

func TestPidScopeKeysOnCallerAndIsWatchable(t *testing.T) {
	key, deg := config.ScopePid.Key(config.CallContext{PID: 1234, CallID: "c1"})
	if deg != "" {
		t.Fatalf("unexpected degrade: %s", deg)
	}
	if !config.ScopePid.WatchesPID() {
		t.Fatal("pid scope must declare that it watches a pid")
	}
	pid, ok := config.PIDOf(key)
	if !ok || pid != 1234 {
		t.Fatalf("PIDOf(%q) = %d, %v", key, pid, ok)
	}
	if _, ok := config.PIDOf("session:abc"); ok {
		t.Fatal("PIDOf must only match pid keys")
	}
}

func TestCwdScopeSeparatesDirectories(t *testing.T) {
	a, _ := config.ScopeCwd.Key(config.CallContext{Cwd: "/a", CallID: "c1"})
	b, _ := config.ScopeCwd.Key(config.CallContext{Cwd: "/b", CallID: "c2"})
	if a == b {
		t.Fatal("different directories must not share")
	}
}

// git-backed scopes need a real repository, so build one.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed: %v\n%s", err, out)
		}
	}
	return root
}

func TestWorktreeScopeSeparatesWorktreesAndRepoScopeJoinsThem(t *testing.T) {
	root := gitRepo(t)
	linked := filepath.Join(t.TempDir(), "wt")
	cmd := exec.Command("git", "worktree", "add", "-q", "-b", "side", linked)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git worktree unavailable: %v\n%s", err, out)
	}

	mainWt, _ := config.ScopeWorktree.Key(config.CallContext{Cwd: root, CallID: "c1"})
	sideWt, _ := config.ScopeWorktree.Key(config.CallContext{Cwd: linked, CallID: "c2"})
	if mainWt == sideWt {
		t.Fatal("worktree scope must separate two worktrees")
	}

	mainRepo, _ := config.ScopeRepo.Key(config.CallContext{Cwd: root, CallID: "c1"})
	sideRepo, _ := config.ScopeRepo.Key(config.CallContext{Cwd: linked, CallID: "c2"})
	if mainRepo != sideRepo {
		t.Fatalf("repo scope must join worktrees of one clone: %q vs %q", mainRepo, sideRepo)
	}
}

func TestRepoScopeOutsideGitFallsBackToCwd(t *testing.T) {
	dir := t.TempDir()
	// A temp dir can still sit inside a repo on some machines; only assert the
	// fallback when git genuinely says no.
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		t.Skip("temp dir is inside a git repository")
	}
	key, deg := config.ScopeRepo.Key(config.CallContext{Cwd: dir, CallID: "c1"})
	if deg == "" {
		t.Fatal("falling back should be reported")
	}
	if !strings.HasPrefix(key, "cwd:") {
		t.Fatalf("expected a cwd key, got %q", key)
	}
}

func TestPidScopeWithoutAPidDegrades(t *testing.T) {
	_, deg := config.ScopePid.Key(config.CallContext{CallID: "c1"})
	if deg == "" {
		t.Fatal("a missing pid must be reported")
	}
}

func TestEveryValidScopeResolvesWithoutPanicking(t *testing.T) {
	for _, sc := range config.ValidScopes {
		key, _ := sc.Key(ctx())
		if key == "" {
			t.Errorf("scope %q produced an empty key", sc)
		}
	}
}

func TestLivePidIsDetected(t *testing.T) {
	key, _ := config.ScopePid.Key(config.CallContext{PID: os.Getpid(), CallID: "c"})
	pid, ok := config.PIDOf(key)
	if !ok {
		t.Fatal("PIDOf failed")
	}
	if pid != os.Getpid() {
		t.Fatalf("got %d want %d", pid, os.Getpid())
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(key, "pid:")); err != nil {
		t.Fatalf("pid key should be numeric: %q", key)
	}
}

func TestRepoScopeIsStableFromAnySubdirectory(t *testing.T) {
	root := gitRepo(t)
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// --git-common-dir answers relative to cwd, so a deep subdirectory gets a
	// path like ../../../.git. All of them must resolve to one key.
	fromRoot, _ := config.ScopeRepo.Key(config.CallContext{Cwd: root, CallID: "c1"})
	fromSub, _ := config.ScopeRepo.Key(config.CallContext{Cwd: sub, CallID: "c2"})
	if fromRoot != fromSub {
		t.Fatalf("repo key must not depend on cwd depth: %q vs %q", fromRoot, fromSub)
	}
	if !filepath.IsAbs(strings.TrimPrefix(fromRoot, "repo:")) {
		t.Fatalf("repo key must be absolute, got %q", fromRoot)
	}
}

func TestCwdKeysAreSymlinkCanonical(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viaReal, _ := config.ScopeCwd.Key(config.CallContext{Cwd: real, CallID: "c1"})
	viaLink, _ := config.ScopeCwd.Key(config.CallContext{Cwd: link, CallID: "c2"})
	if viaReal != viaLink {
		t.Fatalf("a symlinked path must key the same as its target: %q vs %q", viaReal, viaLink)
	}
}

func TestWorktreeKeysAreSymlinkCanonical(t *testing.T) {
	root := gitRepo(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	a, _ := config.ScopeWorktree.Key(config.CallContext{Cwd: root, CallID: "c1"})
	b, _ := config.ScopeWorktree.Key(config.CallContext{Cwd: link, CallID: "c2"})
	if a != b {
		t.Fatalf("worktree key must be symlink-stable: %q vs %q", a, b)
	}
}

func TestOnlyKeysTheCallerMintedAreCallerOwned(t *testing.T) {
	eph := config.CallContext{SessionID: "mine", PID: 77, CallID: "c", Ephemeral: true}
	for _, key := range []string{"call:c", "session:mine", "pid:77"} {
		if !eph.CallerOwned(key) {
			t.Errorf("%q is built from this caller's own identity and should be released with it", key)
		}
	}
	// Keys another caller resolves to as well. Releasing these stopped
	// shared servers at the end of every sessionless exec.
	for _, key := range []string{"global", "repo:/r/.git", "worktree:/r", "cwd:/r",
		"session:other", "psession:mine", "pid:78"} {
		if eph.CallerOwned(key) {
			t.Errorf("%q can be shared, so an ephemeral caller must not own it", key)
		}
	}
	// A host-assigned session is shared with sibling runs.
	hosted := eph
	hosted.Ephemeral = false
	if hosted.CallerOwned("session:mine") || hosted.CallerOwned("pid:77") {
		t.Error("a host-assigned session is not the caller's to release")
	}
	if !hosted.CallerOwned("call:c") {
		t.Error("a per-call key is always the caller's")
	}
}
