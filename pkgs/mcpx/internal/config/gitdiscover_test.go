package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/dezren39/mcpx/internal/defaults"
)

// Parity: every layout native discovery claims to read is built here with the
// real git, and native's answer is compared with `git rev-parse`. A case that
// native hands to git is marked unsure, and then the full resolver (native
// plus fallback) is what must agree. The handful of places mcpx deliberately
// differs from git have their own tests further down, each asserting the
// difference exactly.

// gitFix is a hermetic place to build repositories: its own global config,
// no system config, none of the variables that name a repository, and
// discovery fenced at the temp root's parent so no answer can come from
// whatever the machine running the tests happens to have above it.
type gitFix struct {
	t     *testing.T
	root  string
	fence string
	major int
	minor int
}

func newGitFix(t *testing.T) *gitFix {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; parity needs the real git to compare with")
	}
	out, err := exec.Command("git", "version").Output()
	if err != nil {
		t.Skipf("git version: %v", err)
	}
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		t.Skipf("cannot read git version from %q", out)
	}
	f := &gitFix{t: t}
	f.major, _ = strconv.Atoi(m[1])
	f.minor, _ = strconv.Atoi(m[2])
	if !f.atLeast(2, 31) {
		t.Skipf("git %s predates --path-format, which the comparison uses", strings.TrimSpace(string(out)))
	}

	f.root = mustReal(t, t.TempDir())
	f.fence = filepath.Dir(f.root)
	// Where fixtures park git directories that live apart from a checkout.
	if err := os.Mkdir(filepath.Join(f.root, "stores"), 0o755); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	writeFile(t, global, "[user]\n\tname = t\n\temail = t@t\n[commit]\n\tgpgsign = false\n"+
		"[init]\n\tdefaultBranch = main\n[protocol \"file\"]\n\tallow = always\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CEILING_DIRECTORIES", f.fence)
	for _, k := range append(append([]string{}, gitLocalEnv...),
		"GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_TEST_ASSUME_DIFFERENT_OWNER") {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	return f
}

func (f *gitFix) atLeast(major, minor int) bool {
	return f.major > major || (f.major == major && f.minor >= minor)
}

func (f *gitFix) git(dir string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo is a fresh clone-shaped repository with one commit.
func (f *gitFix) repo(name string, initArgs ...string) string {
	f.t.Helper()
	dir := filepath.Join(f.root, name)
	f.git(f.root, append(append([]string{"init", "-q"}, initArgs...), dir)...)
	f.git(dir, "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

func (f *gitFix) path(parts ...string) string {
	return filepath.Join(append([]string{f.root}, parts...)...)
}

func (f *gitFix) mkdir(parts ...string) string {
	f.t.Helper()
	p := f.path(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		f.t.Fatal(err)
	}
	return p
}

// ref is git's answer to one question, or its error message.
func ref(cwd, flag string) (string, string) {
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", flag)
	cmd.Dir = cwd
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", strings.TrimSpace(stderr.String()) + " (" + err.Error() + ")"
	}
	return canonical(strings.TrimSpace(string(out))), ""
}

// expect is what a probe must show.
type expect int

const (
	certain expect = iota // native answers, and equals git
	unsure                // native hands it to git; the full resolver equals git
)

// same asserts native (or, for unsure cases, the full resolver) says what git
// says about cwd.
func (f *gitFix) same(cwd string, want expect) {
	f.t.Helper()
	common, commonErr := ref(cwd, "--git-common-dir")
	top, topErr := ref(cwd, "--show-toplevel")

	w, err := discoverGit(cwd)
	var ge *gitError
	errors.As(err, &ge)
	isUnsure := ge != nil && ge.kind == gitUnsure
	switch {
	case want == certain && isUnsure:
		f.t.Errorf("%s: native should answer this itself, but was unsure: %v", cwd, err)
		return
	case want == unsure && !isUnsure:
		f.t.Errorf("%s: native should hand this to git, but answered %+v, %v", cwd, w, err)
		return
	case want == unsure:
		w, _, err = locateGit(cwd)
	}

	if commonErr != "" {
		if err == nil {
			f.t.Errorf("%s: git fails (%s) but mcpx found %+v", cwd, commonErr, w)
		}
		return
	}
	if err != nil {
		f.t.Errorf("%s: git finds %s but mcpx failed: %v", cwd, common, err)
		return
	}
	if w.commonDir != common {
		f.t.Errorf("%s: common dir\n  git:  %s\n  mcpx: %s", cwd, common, w.commonDir)
	}
	switch {
	case topErr == "" && w.worktree != top:
		f.t.Errorf("%s: toplevel\n  git:  %s\n  mcpx: %s (%s)", cwd, top, w.worktree, w.noWorktree)
	case topErr != "" && w.worktree != "":
		f.t.Errorf("%s: git has no toplevel (%s) but mcpx says %s", cwd, topErr, w.worktree)
	case topErr != "" && w.noWorktree == "":
		f.t.Errorf("%s: no worktree, and no reason given", cwd)
	}
}

// fails asserts git fails outright here, so a case that is meant to be a
// failure cannot pass by accident because the fixture built a valid repo.
func (f *gitFix) gitFails(cwd string) {
	f.t.Helper()
	if c, _ := ref(cwd, "--git-common-dir"); c != "" {
		f.t.Fatalf("fixture is wrong: git resolves %s to %s", cwd, c)
	}
}

func (f *gitFix) gitHasNoToplevel(cwd string) {
	f.t.Helper()
	if top, _ := ref(cwd, "--show-toplevel"); top != "" {
		f.t.Fatalf("fixture is wrong: git gives %s a toplevel %s", cwd, top)
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReal(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestGitParityCheckouts(t *testing.T) {
	f := newGitFix(t)

	t.Run("main clone, from its root and from deep inside", func(t *testing.T) {
		f.t = t
		r := f.repo("main")
		f.same(r, certain)
		f.same(f.mkdir("main", "a", "b", "c"), certain)
	})

	t.Run("linked worktree with git's relative commondir", func(t *testing.T) {
		f.t = t
		r := f.repo("lw")
		f.git(r, "worktree", "add", "-q", "-b", "side", f.path("lw-wt"))
		if b, _ := os.ReadFile(filepath.Join(r, ".git", "worktrees", "lw-wt", "commondir")); filepath.IsAbs(strings.TrimSpace(string(b))) {
			t.Fatalf("expected git to write a relative commondir, got %q", b)
		}
		f.same(f.path("lw-wt"), certain)
		f.same(f.mkdir("lw-wt", "sub"), certain)
		f.same(r, certain)
	})

	t.Run("linked worktree with an absolute commondir", func(t *testing.T) {
		f.t = t
		r := f.repo("abs")
		f.git(r, "worktree", "add", "-q", "-b", "side", f.path("abs-wt"))
		writeFile(t, filepath.Join(r, ".git", "worktrees", "abs-wt", "commondir"), filepath.Join(r, ".git")+"\n")
		f.same(f.path("abs-wt"), certain)
	})

	t.Run("linked worktree with relative paths (worktree.useRelativePaths)", func(t *testing.T) {
		f.t = t
		if !f.atLeast(2, 48) {
			t.Skip("worktree.useRelativePaths is git 2.48")
		}
		r := f.repo("relwt")
		f.git(r, "-c", "worktree.useRelativePaths=true", "worktree", "add", "-q", "-b", "side", f.path("relwt-wt"))
		b, _ := os.ReadFile(f.path("relwt-wt", ".git"))
		if !strings.HasPrefix(string(b), "gitdir: ../") {
			t.Fatalf("expected a relative gitfile, got %q", b)
		}
		// This also sets extensions.relativeWorktrees and format version
		// 1, so the extension table is exercised too.
		if v := f.git(r, "config", "core.repositoryformatversion"); v != "1" {
			t.Fatalf("expected format version 1, got %s", v)
		}
		f.same(f.path("relwt-wt"), certain)
		f.same(r, certain)
	})

	t.Run("linked worktree moved by hand still resolves", func(t *testing.T) {
		f.t = t
		r := f.repo("mv")
		f.git(r, "worktree", "add", "-q", "-b", "side", f.path("mv-wt"))
		if err := os.Rename(f.path("mv-wt"), f.path("mv-wt-moved")); err != nil {
			t.Fatal(err)
		}
		f.same(f.path("mv-wt-moved"), certain)
	})

	t.Run("main clone moved: its worktree points nowhere", func(t *testing.T) {
		f.t = t
		r := f.repo("gone")
		f.git(r, "worktree", "add", "-q", "-b", "side", f.path("gone-wt"))
		if err := os.Rename(r, f.path("gone-moved")); err != nil {
			t.Fatal(err)
		}
		f.gitFails(f.path("gone-wt"))
		f.same(f.path("gone-wt"), certain)
		f.same(f.path("gone-moved"), certain)
	})

	t.Run("submodule is its own repository", func(t *testing.T) {
		f.t = t
		src := f.repo("subsrc")
		super := f.repo("super")
		// A local path: file transport, no network.
		f.git(super, "submodule", "add", "-q", src, "sub")
		f.git(super, "commit", "-q", "-m", "add sub")
		if b, _ := os.ReadFile(filepath.Join(super, "sub", ".git")); !strings.HasPrefix(string(b), "gitdir: ../") {
			t.Fatalf("expected a relative gitfile in the submodule, got %q", b)
		}
		// Its config carries core.worktree, so this also covers reading it.
		if wt := f.git(filepath.Join(super, ".git", "modules", "sub"), "config", "core.worktree"); wt == "" {
			t.Fatal("expected the submodule's config to set core.worktree")
		}
		f.same(super, certain)
		f.same(filepath.Join(super, "sub"), certain)
		f.same(f.mkdir("super", "sub", "deeper"), certain)
		f.same(filepath.Join(super, ".git", "modules", "sub"), certain)
	})

	t.Run("nested repository: innermost wins", func(t *testing.T) {
		f.t = t
		outer := f.repo("outer")
		inner := filepath.Join(outer, "inner")
		f.git(outer, "init", "-q", inner)
		f.same(inner, certain)
		f.same(f.mkdir("outer", "inner", "x"), certain)
		f.same(outer, certain)
	})

	t.Run("separate git dir", func(t *testing.T) {
		f.t = t
		dir := f.path("sep")
		f.git(f.root, "init", "-q", "--separate-git-dir", f.path("stores", "sep.git"), dir)
		f.same(dir, certain)
	})

	t.Run("gitfile with a relative path, LF and CRLF", func(t *testing.T) {
		f.t = t
		for _, eol := range []string{"\n", "\r\n", ""} {
			name := "relfile" + strconv.Itoa(len(eol))
			r := f.repo(name)
			store := f.path("stores", name+".git")
			if err := os.Rename(filepath.Join(r, ".git"), store); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(r, ".git"), "gitdir: ../stores/"+name+".git"+eol)
			f.same(r, certain)
			f.same(f.mkdir(name, "sub"), certain)
		}
	})

	t.Run("gitfile path through a symlink and ..: resolved by the kernel, not lexically", func(t *testing.T) {
		f.t = t
		r := f.repo("dots")
		deep := f.mkdir("stores", "real", "deep")
		if err := os.Rename(filepath.Join(r, ".git"), f.path("stores", "real", "dots.git")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(deep, f.path("lnk")); err != nil {
			t.Fatal(err)
		}
		// Lexically ../lnk/../dots.git is <root>/dots.git, which does not
		// exist; physically lnk/.. is stores/real.
		writeFile(t, filepath.Join(r, ".git"), "gitdir: ../lnk/../dots.git\n")
		f.same(r, certain)
	})

	t.Run(".git is a symlink to a git directory elsewhere", func(t *testing.T) {
		f.t = t
		r := f.repo("dotlink")
		store := f.path("stores", "dotlink.git")
		if err := os.Rename(filepath.Join(r, ".git"), store); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(store, filepath.Join(r, ".git")); err != nil {
			t.Fatal(err)
		}
		f.same(r, certain)
	})

	t.Run("symlinked checkout resolves to its target", func(t *testing.T) {
		f.t = t
		r := f.repo("target")
		f.mkdir("target", "sub")
		if err := os.Symlink(r, f.path("via-link")); err != nil {
			t.Fatal(err)
		}
		f.same(f.path("via-link"), certain)
		f.same(f.path("via-link", "sub"), certain)
		a, _ := discoverGit(r)
		b, _ := discoverGit(f.path("via-link"))
		if a != b {
			t.Errorf("a symlink and its target must resolve alike: %+v vs %+v", a, b)
		}
	})
}

func TestGitParityGitDirectories(t *testing.T) {
	f := newGitFix(t)

	t.Run("bare repository, from itself and from inside it", func(t *testing.T) {
		f.t = t
		src := f.repo("baresrc")
		f.git(f.root, "clone", "-q", "--bare", src, f.path("b.git"))
		f.gitHasNoToplevel(f.path("b.git"))
		f.same(f.path("b.git"), certain)
		f.same(f.path("b.git", "refs", "heads"), certain)
	})

	t.Run("worktree of a bare repository ignores the common core.bare", func(t *testing.T) {
		f.t = t
		src := f.repo("bwsrc")
		f.git(f.root, "clone", "-q", "--bare", src, f.path("bw.git"))
		f.git(f.path("bw.git"), "worktree", "add", "-q", f.path("bw-wt"))
		f.same(f.path("bw-wt"), certain)
	})

	t.Run("bare repository with worktreeConfig makes its worktrees bare too", func(t *testing.T) {
		f.t = t
		src := f.repo("bwcsrc")
		f.git(f.root, "clone", "-q", "--bare", src, f.path("bwc.git"))
		f.git(f.path("bwc.git"), "worktree", "add", "-q", f.path("bwc-wt"))
		f.git(f.path("bwc.git"), "config", "extensions.worktreeConfig", "true")
		// git's documented trap, and the reason has_common is reset.
		f.gitHasNoToplevel(f.path("bwc-wt"))
		f.same(f.path("bwc-wt"), certain)
	})

	t.Run("a directory that is both a repository and holds a .git: the .git wins", func(t *testing.T) {
		f.t = t
		src := f.repo("bothsrc")
		other := f.repo("bothother")
		f.git(f.root, "clone", "-q", "--bare", src, f.path("both.git"))
		writeFile(t, f.path("both.git", ".git"), "gitdir: "+filepath.Join(other, ".git")+"\n")
		f.same(f.path("both.git"), certain)
		if w, _ := discoverGit(f.path("both.git")); w.commonDir != filepath.Join(other, ".git") {
			t.Errorf("the .git file should be read before the directory itself: %+v", w)
		}
	})

	t.Run("inside .git and inside a worktree's private directory", func(t *testing.T) {
		f.t = t
		r := f.repo("inside")
		f.git(r, "worktree", "add", "-q", "-b", "side", f.path("inside-wt"))
		for _, p := range []string{
			filepath.Join(r, ".git"),
			filepath.Join(r, ".git", "refs"),
			filepath.Join(r, ".git", "worktrees", "inside-wt"),
		} {
			f.gitHasNoToplevel(p)
			f.same(p, certain)
		}
	})

	t.Run("HEAD forms: symlink, detached SHA-1, detached SHA-256", func(t *testing.T) {
		f.t = t
		r := f.repo("headlink")
		head := filepath.Join(r, ".git", "HEAD")
		os.Remove(head)
		if err := os.Symlink("refs/heads/main", head); err != nil {
			t.Fatal(err)
		}
		f.same(r, certain)

		d := f.repo("detached")
		f.git(d, "checkout", "-q", "--detach")
		f.same(d, certain)

		if f.atLeast(2, 29) {
			s := f.repo("sha256", "--object-format=sha256")
			f.git(s, "checkout", "-q", "--detach")
			if b, _ := os.ReadFile(filepath.Join(s, ".git", "HEAD")); len(strings.TrimSpace(string(b))) != 64 {
				t.Fatalf("expected a 64-digit HEAD, got %q", b)
			}
			f.same(s, certain)
		}
	})

	t.Run("reftable repository", func(t *testing.T) {
		f.t = t
		if !f.atLeast(2, 45) {
			t.Skip("reftable is git 2.45")
		}
		r := f.repo("reftable", "--ref-format=reftable")
		f.same(r, certain)
		f.same(f.mkdir("reftable", "sub"), certain)
	})
}

func TestGitParityStrayAndBrokenDotGit(t *testing.T) {
	f := newGitFix(t)

	t.Run("a .git directory that is not a repository is skipped", func(t *testing.T) {
		f.t = t
		r := f.repo("stray")
		f.mkdir("stray", "sub", ".git")
		f.same(filepath.Join(r, "sub"), certain)
		f.mkdir("plain", ".git")
		f.gitFails(f.path("plain"))
		f.same(f.path("plain"), certain)
	})

	t.Run("a nested repository with an invalid HEAD is skipped", func(t *testing.T) {
		f.t = t
		outer := f.repo("badhead")
		inner := filepath.Join(outer, "inner")
		f.git(outer, "init", "-q", inner)
		writeFile(t, filepath.Join(inner, ".git", "HEAD"), "not a ref\n")
		f.same(inner, certain)
	})

	t.Run("HEAD that is almost valid is not", func(t *testing.T) {
		f.t = t
		for i, bad := range []func(head string){
			// Forty characters, not forty hex digits.
			func(head string) { writeFile(t, head, strings.Repeat("z", 40)+"\n") },
			// A symlink, but not into refs/.
			func(head string) { os.Remove(head); os.Symlink("../elsewhere", head) },
			// A symbolic ref outside refs/.
			func(head string) { writeFile(t, head, "ref: heads/main\n") },
		} {
			outer := f.repo("almost" + strconv.Itoa(i))
			inner := filepath.Join(outer, "inner")
			f.git(outer, "init", "-q", inner)
			bad(filepath.Join(inner, ".git", "HEAD"))
			f.same(inner, certain)
			if w, _ := discoverGit(inner); w.worktree != outer {
				t.Errorf("case %d: expected the outer repository, got %+v", i, w)
			}
		}
	})

	t.Run("a nested repository without objects/ is skipped", func(t *testing.T) {
		f.t = t
		outer := f.repo("noobjects")
		inner := filepath.Join(outer, "inner")
		f.git(outer, "init", "-q", inner)
		if err := os.RemoveAll(filepath.Join(inner, ".git", "objects")); err != nil {
			t.Fatal(err)
		}
		f.same(inner, certain)
		if w, _ := discoverGit(inner); w.worktree != outer {
			t.Errorf("expected the outer repository, got %+v", w)
		}
	})

	t.Run("a dangling .git symlink is no .git at all", func(t *testing.T) {
		f.t = t
		r := f.repo("dangle")
		if err := os.Symlink(f.path("nowhere"), filepath.Join(f.mkdir("dangle", "sub"), ".git")); err != nil {
			t.Fatal(err)
		}
		f.same(filepath.Join(r, "sub"), certain)
	})

	// A .git file git refuses is fatal to discovery, not skipped: git stops
	// there, so mcpx must too rather than find a repository above it.
	f.mkdir("plainstore")
	ts := f.repo("tssrc")
	if err := os.Rename(filepath.Join(ts, ".git"), f.path("stores", "ts.git")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, content string }{
		{"garbage", "hello\n"},
		{"no space after gitdir:", "gitdir:/x\n"},
		{"no path", "gitdir: \n"},
		{"points nowhere", "gitdir: /nonexistent/really/not/here\n"},
		{"points at a directory that is not a repository", "gitdir: ../plainstore\n"},
		{"trailing spaces are part of the path", "gitdir: ../stores/ts.git  \n"},
	} {
		t.Run("broken .git file: "+tc.name, func(t *testing.T) {
			f.t = t
			name := "broken-" + strings.NewReplacer(" ", "-", ":", "").Replace(tc.name)
			f.repo(name)
			// Inside a real repository, so skipping it would find one.
			sub := f.mkdir(name, "sub")
			writeFile(t, filepath.Join(sub, ".git"), strings.ReplaceAll(tc.content, "../", "../../"))
			f.gitFails(sub)
			f.same(sub, certain)
			w, err := discoverGit(sub)
			var ge *gitError
			if !errors.As(err, &ge) || ge.kind != gitBroken {
				t.Errorf("expected a broken-gitfile error, got %+v %v", w, err)
			}
		})
	}

	t.Run("a .git file larger than git accepts", func(t *testing.T) {
		f.t = t
		target := f.repo("bigtarget")
		f.repo("big")
		sub := f.mkdir("big", "sub")
		// Valid once its trailing newlines are trimmed, which is exactly
		// what git will not do past its size ceiling.
		body := "gitdir: " + filepath.Join(target, ".git") + strings.Repeat("\n", int(defaults.GitfileMaxBytes))
		writeFile(t, filepath.Join(sub, ".git"), body)
		f.gitFails(sub)
		f.same(sub, certain)
	})

	t.Run("a directory discovery cannot look inside", func(t *testing.T) {
		f.t = t
		if os.Getuid() == 0 {
			t.Skip("root reads through permissions")
		}
		f.repo("locked")
		sub := f.mkdir("locked", "sub")
		if err := os.Chmod(sub, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(sub, 0o755) })
		// git cannot even chdir there; skipping the unreadable .git would
		// instead find the repository above it.
		if w, err := discoverGit(sub); err == nil {
			t.Errorf("an unsearchable directory must not resolve to the one above it: %+v", w)
		}
	})

	t.Run("a .git that is neither file nor directory", func(t *testing.T) {
		f.t = t
		f.repo("fifo")
		sub := f.mkdir("fifo", "sub")
		if err := syscall.Mkfifo(filepath.Join(sub, ".git"), 0o644); err != nil {
			t.Skipf("mkfifo: %v", err)
		}
		// git 2.54 made this fatal; before that it was skipped. mcpx
		// follows current git, so compare only against a git that agrees.
		if _, err := discoverGit(sub); err == nil {
			t.Error("a fifo .git should stop discovery")
		}
		if f.atLeast(2, 54) {
			f.same(sub, certain)
		}
	})

	t.Run("a commondir that cannot be read is fatal", func(t *testing.T) {
		f.t = t
		r := f.repo("emptycd")
		f.git(r, "worktree", "add", "-q", "-b", "side", f.path("emptycd-wt"))
		writeFile(t, filepath.Join(r, ".git", "worktrees", "emptycd-wt", "commondir"), "")
		f.gitFails(f.path("emptycd-wt"))
		f.same(f.path("emptycd-wt"), certain)
	})

	t.Run("cwd that does not exist, and cwd that is a file", func(t *testing.T) {
		f.t = t
		r := f.repo("cwdfile")
		file := filepath.Join(r, "README")
		writeFile(t, file, "x")
		for _, p := range []string{filepath.Join(r, "missing"), file} {
			if _, err := discoverGit(p); err == nil {
				t.Errorf("%s: git cannot even start there; discovery must fail", p)
			}
		}
	})
}

func TestGitParityConfig(t *testing.T) {
	f := newGitFix(t)

	t.Run("core.worktree absolute, relative, and with a missing last component", func(t *testing.T) {
		f.t = t
		abs := f.repo("cwabs")
		elsewhere := f.mkdir("elsewhere")
		f.git(abs, "config", "core.worktree", elsewhere)
		f.same(abs, certain)
		f.same(f.mkdir("cwabs", "sub"), certain)

		rel := f.repo("cwrel")
		f.mkdir("elsewhere2")
		f.git(rel, "config", "core.worktree", "../../elsewhere2")
		f.same(rel, certain)

		missing := f.repo("cwmissing")
		f.git(missing, "config", "core.worktree", f.path("not-yet"))
		f.same(missing, certain)
	})

	t.Run("core.worktree git cannot enter fails both answers", func(t *testing.T) {
		f.t = t
		rel := f.repo("cwbadrel")
		f.git(rel, "config", "core.worktree", "../../nope")
		f.gitFails(rel)
		f.same(rel, certain)

		deep := f.repo("cwbadabs")
		f.git(deep, "config", "core.worktree", f.path("m1", "m2"))
		f.gitFails(deep)
		f.same(deep, certain)
	})

	t.Run("core.bare, and core.bare with core.worktree", func(t *testing.T) {
		f.t = t
		r := f.repo("isbare")
		f.git(r, "config", "core.bare", "true")
		f.gitHasNoToplevel(r)
		f.same(r, certain)

		both := f.repo("bothset")
		f.git(both, "config", "core.bare", "true")
		f.git(both, "config", "core.worktree", f.mkdir("both-wt"))
		f.gitHasNoToplevel(both)
		f.same(both, certain)
	})

	t.Run("worktreeConfig: per-worktree core.bare and core.worktree", func(t *testing.T) {
		f.t = t
		r := f.repo("wtc")
		f.git(r, "worktree", "add", "-q", "-b", "side", f.path("wtc-wt"))
		f.git(r, "config", "extensions.worktreeConfig", "true")
		f.git(r, "config", "--worktree", "core.bare", "true")
		f.gitHasNoToplevel(r)
		f.same(r, certain)
		f.same(f.path("wtc-wt"), certain)
		f.git(f.path("wtc-wt"), "config", "--worktree", "core.worktree", f.mkdir("wtc-elsewhere"))
		f.same(f.path("wtc-wt"), certain)
		// Relative, it is relative to the worktree's private directory,
		// not to the common one.
		f.mkdir("wtc-elsewhere2")
		f.git(f.path("wtc-wt"), "config", "--worktree", "core.worktree", "../../../../wtc-elsewhere2")
		if top, _ := ref(f.path("wtc-wt"), "--show-toplevel"); top != f.path("wtc-elsewhere2") {
			t.Fatalf("fixture is wrong: git's toplevel is %q", top)
		}
		f.same(f.path("wtc-wt"), certain)
	})

	t.Run("no config file at all", func(t *testing.T) {
		f.t = t
		r := f.repo("nocfg")
		os.Remove(filepath.Join(r, ".git", "config"))
		f.same(r, certain)
	})

	t.Run("without repositoryformatversion, core.bare is ignored", func(t *testing.T) {
		f.t = t
		r := f.repo("nover")
		writeFile(t, filepath.Join(r, ".git", "config"), "[core]\n\tbare = true\n")
		f.same(r, certain)
	})

	t.Run("[include] does not reach core.worktree", func(t *testing.T) {
		f.t = t
		r := f.repo("incl")
		inc := f.path("incl.cfg")
		writeFile(t, inc, "[core]\n\tworktree = "+f.mkdir("incl-elsewhere")+"\n")
		f.git(r, "config", "include.path", inc)
		f.same(r, certain)
	})

	t.Run("parser: case, subsections, quoting, comments, continuation, BOM, CRLF", func(t *testing.T) {
		f.t = t
		spaced := f.mkdir("with space")
		for i, cfg := range []string{
			// Upper-case section and key; a quoted, commented false.
			"[CORE]\n\tRepositoryFormatVersion = 0\n\tBare = \"fal\"se ; not bare\n",
			// A subsection is a different variable.
			"[core]\n\trepositoryformatversion = 0\n[core \"x\"]\n\tbare = true\n",
			// And a section that happens to have "core" as its
			// subsection is not [core].
			"[core]\n\trepositoryformatversion = 0\n[foo \"core\"]\n\tbare = true\n",
			// Deprecated [section.subsection] likewise.
			"[core]\n\trepositoryformatversion = 0\n[core.x]\n\tbare = true\n",
			// Section and key on one line; quoted path with a space;
			// trailing comment.
			"[core]\n\trepositoryformatversion = 0\n[core] worktree = \"" + spaced + "\" # there\n",
			// A value continued onto the next line.
			"[core]\n\trepositoryformatversion = 0\n\tworktree = " + spaced[:5] + "\\\n" + spaced[5:] + "\n",
			// Byte order mark and CRLF.
			"\xEF\xBB\xBF[core]\r\n\trepositoryformatversion = 0\r\n\tbare = yes\r\n",
			// A bare key is true.
			"[core]\n\trepositoryformatversion = 0\n\tbare\n",
			// Numeric booleans.
			"[core]\n\trepositoryformatversion = 0\n\tbare = 0\n",
		} {
			r := f.repo("parse" + strconv.Itoa(i))
			writeFile(t, filepath.Join(r, ".git", "config"), cfg)
			f.same(r, certain)
		}
	})

	t.Run("formats and values native hands to git", func(t *testing.T) {
		f.t = t
		for i, cfg := range []string{
			// Syntax errors git itself refuses.
			"[core]\n\trepositoryformatversion = 0\ngarbage line\n",
			"[core\n",
			"[core]\n\tbare # comment after a bare key\n",
			"[core]\n\tworktree = \"unterminated\n",
			"[core]\n\tworktree = bad\\qescape\n",
			// A version from the future.
			"[core]\n\trepositoryformatversion = 2\n",
			// An extension nobody knows, in a version-1 repository.
			"[core]\n\trepositoryformatversion = 1\n[extensions]\n\tfrobnicate = true\n",
			// A version-1 extension in a version-0 repository.
			"[core]\n\trepositoryformatversion = 0\n[extensions]\n\tobjectformat = sha1\n",
			// Values git refuses.
			"[core]\n\trepositoryformatversion = 0\n\tbare = maybe\n",
			"[core]\n\trepositoryformatversion = 1\n[extensions]\n\tobjectformat = md5\n",
			"[core]\n\trepositoryformatversion = 0\n\tworktree\n",
			// A value git accepts that this port does not parse.
			"[core]\n\trepositoryformatversion = 0\n\tbare = 1k\n",
		} {
			r := f.repo("unsure" + strconv.Itoa(i))
			writeFile(t, filepath.Join(r, ".git", "config"), cfg)
			f.same(r, unsure)
		}
	})

	t.Run("an unknown extension in a version-0 repository is ignored, as git does", func(t *testing.T) {
		f.t = t
		r := f.repo("v0ext")
		writeFile(t, filepath.Join(r, ".git", "config"), "[core]\n\trepositoryformatversion = 0\n[extensions]\n\tfrobnicate = true\n")
		f.same(r, certain)
	})
}

func TestGitParityCeilingDirectories(t *testing.T) {
	f := newGitFix(t)
	r := f.repo("ceil")
	sub := f.mkdir("ceil", "sub")
	link := f.path("ceil-link")
	if err := os.Symlink(r, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, entries string
		cwd           string
		found         bool
	}{
		{"a ceiling above the repository", f.root, sub, true},
		{"the repository root is never examined from below it", r, sub, false},
		{"unless discovery starts there", r, r, true},
		{"a trailing slash is the same ceiling", r + "/", sub, false},
		// Relative to the directory the tests run in -- which is set to
		// the fixture root below -- "ceil" would be the repository root.
		{"a relative entry is ignored", "ceil", sub, true},
		{"an entry is symlink-resolved", link, sub, false},
		{"but not after an empty entry", ":" + link, sub, true},
		{"an entry that does not exist is dropped", f.path("nope", "deeper"), sub, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.t = t
			// The fence stays first, so it is always resolved.
			t.Setenv("GIT_CEILING_DIRECTORIES", f.fence+":"+tc.entries)
			t.Chdir(f.root)
			if c, _ := ref(tc.cwd, "--git-common-dir"); (c != "") != tc.found {
				t.Fatalf("fixture is wrong: git found=%v (%q)", c != "", c)
			}
			f.same(tc.cwd, certain)
		})
	}
}

func TestGitDiscoveryAcrossFilesystemIsHandedToGitWhenUnparseable(t *testing.T) {
	f := newGitFix(t)
	r := f.repo("dafs")
	t.Setenv("GIT_DISCOVERY_ACROSS_FILESYSTEM", "sometimes")
	f.same(r, unsure)
	t.Setenv("GIT_DISCOVERY_ACROSS_FILESYSTEM", "true")
	f.same(r, certain)
}

// Deliberate differences from git. Each is asserted as exactly that
// difference, so a change that widens one fails here.

func TestGitEnvironmentThatNamesARepositoryIsIgnored(t *testing.T) {
	f := newGitFix(t)
	f.repo("mine")
	sub := f.mkdir("mine", "sub")
	other := f.repo("other")
	for _, kv := range [][2]string{
		{"GIT_DIR", filepath.Join(other, ".git")},
		{"GIT_DIR", ".git"}, // what a git hook exports: relative, and wrong from a subdirectory
		{"GIT_COMMON_DIR", filepath.Join(other, ".git")},
		{"GIT_WORK_TREE", other},
	} {
		t.Run(kv[0]+"="+kv[1], func(t *testing.T) {
			f.t = t
			want, _ := discoverGit(sub)
			t.Setenv(kv[0], kv[1])
			// The variable is live: git as the daemon used to run it
			// answers differently.
			if c, _ := ref(sub, "--git-common-dir"); c == want.commonDir {
				if t2, _ := ref(sub, "--show-toplevel"); t2 == want.worktree {
					t.Fatalf("fixture is wrong: %s does not change git's answer", kv[0])
				}
			}
			got, err := discoverGit(sub)
			if err != nil || got != want {
				t.Errorf("native discovery read %s: %+v %v, want %+v", kv[0], got, err, want)
			}
			// And the fallback scrubs it, so the two agree.
			gw, err := revParse(sub)
			if err != nil || gw.commonDir != want.commonDir || gw.worktree != want.worktree {
				t.Errorf("fallback read %s: %+v %v, want %+v", kv[0], gw, err, want)
			}
		})
	}
}

func TestGitLocalEnvCoversWhatGitCallsLocal(t *testing.T) {
	newGitFix(t)
	out, err := exec.Command("git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range strings.Fields(string(out)) {
		found := false
		for _, have := range gitLocalEnv {
			found = found || have == v
		}
		if !found {
			t.Errorf("git calls %s repository-local; gitEnv does not scrub it", v)
		}
	}
}

func TestGitOwnershipIsNotCheckedNatively(t *testing.T) {
	// safe.directory exists so that git does not run configuration from a
	// repository someone else owns. Native discovery runs nothing from the
	// repository -- it reads HEAD, commondir, a .git file and four config
	// keys -- so it answers where git refuses. A container with a mounted
	// checkout owned by another uid is the common case.
	f := newGitFix(t)
	r := f.repo("foreign")
	t.Setenv("GIT_TEST_ASSUME_DIFFERENT_OWNER", "1")
	if _, e := ref(r, "--git-common-dir"); !strings.Contains(e, "dubious ownership") {
		t.Skipf("this git does not honour GIT_TEST_ASSUME_DIFFERENT_OWNER: %q", e)
	}
	w, err := discoverGit(r)
	if err != nil || w.commonDir != filepath.Join(r, ".git") || w.worktree != r {
		t.Errorf("native should resolve a foreign-owned repository: %+v %v", w, err)
	}
	// The fallback does not override git's check, so an unsure layout in a
	// foreign repository degrades, with git's reason.
	writeFile(t, filepath.Join(r, ".git", "config"), "[core]\n\trepositoryformatversion = 2\n")
	key, why := ScopeRepo.Key(CallContext{Cwd: r})
	if !strings.HasPrefix(key, "cwd:") || !strings.Contains(why, "dubious ownership") {
		t.Errorf("expected a cwd key with git's reason, got %q, %q", key, why)
	}
}

func TestGitSafeBareRepositoryIsNotCheckedNatively(t *testing.T) {
	f := newGitFix(t)
	src := f.repo("sbsrc")
	f.git(f.root, "clone", "-q", "--bare", src, f.path("sb.git"))
	global := os.Getenv("GIT_CONFIG_GLOBAL")
	b, _ := os.ReadFile(global)
	strict := filepath.Join(t.TempDir(), "gitconfig")
	writeFile(t, strict, string(b)+"[safe]\n\tbareRepository = explicit\n")
	t.Setenv("GIT_CONFIG_GLOBAL", strict)
	if _, e := ref(f.path("sb.git"), "--git-common-dir"); !strings.Contains(e, "bare") {
		t.Skipf("this git does not enforce safe.bareRepository: %q", e)
	}
	if w, err := discoverGit(f.path("sb.git")); err != nil || w.commonDir != f.path("sb.git") {
		t.Errorf("native should resolve the bare repository: %+v %v", w, err)
	}
}

// The single-process fallback relies on rev-parse printing --git-common-dir
// before --show-toplevel dies.
func TestGitFallbackAnswersBothFromOneProcess(t *testing.T) {
	f := newGitFix(t)
	r := f.repo("fb")
	f.git(r, "worktree", "add", "-q", "-b", "side", f.path("fb-wt"))
	f.git(f.root, "clone", "-q", "--bare", r, f.path("fb.git"))
	for _, cwd := range []string{r, f.mkdir("fb", "x"), f.path("fb-wt"), f.path("fb.git"), filepath.Join(r, ".git")} {
		native, err := discoverGit(cwd)
		if err != nil {
			t.Fatal(err)
		}
		viaGit, err := revParse(cwd)
		if err != nil {
			t.Fatalf("%s: %v", cwd, err)
		}
		if native.commonDir != viaGit.commonDir || native.worktree != viaGit.worktree ||
			(native.worktree == "") != (viaGit.noWorktree != "") {
			t.Errorf("%s: native %+v, fallback %+v", cwd, native, viaGit)
		}
	}
	if _, err := revParse(f.mkdir("nothing-here")); err == nil {
		t.Error("the fallback must fail outside a repository")
	}
}

func TestGitFallbackRejectsAGitWithoutPathFormat(t *testing.T) {
	// Before 2.31 rev-parse echoes an option it does not know, which would
	// otherwise be read as a path.
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\nfor a in \"$@\"; do case $a in rev-parse) ;; *) echo \"$a\";; esac; done\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	_, err := revParse(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "2.31") {
		t.Errorf("expected the old-git message, got %v", err)
	}
}

// Without git: everything below builds repositories by hand, so it runs where
// git does not exist -- including the Nix build sandbox and the image.

// handRepo lays out the minimum git accepts as a repository.
func handRepo(t *testing.T, dir string) string {
	t.Helper()
	for _, d := range []string{"objects", "refs/heads"} {
		if err := os.MkdirAll(filepath.Join(dir, ".git", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(dir, ".git", "config"), "[core]\n\trepositoryformatversion = 0\n\tbare = false\n")
	return mustReal(t, dir)
}

// handWorktree adds a linked worktree the way `git worktree add` lays it out.
func handWorktree(t *testing.T, repo, name, dir string) string {
	t.Helper()
	admin := filepath.Join(repo, ".git", "worktrees", name)
	writeFile(t, filepath.Join(admin, "HEAD"), "ref: refs/heads/"+name+"\n")
	writeFile(t, filepath.Join(admin, "commondir"), "../..\n")
	writeFile(t, filepath.Join(admin, "gitdir"), filepath.Join(dir, ".git")+"\n")
	writeFile(t, filepath.Join(dir, ".git"), "gitdir: "+admin+"\n")
	return mustReal(t, dir)
}

func withoutGit(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	if _, err := exec.LookPath("git"); err == nil {
		t.Fatal("git is still reachable")
	}
}

func TestScopesResolveWithoutGit(t *testing.T) {
	withoutGit(t)
	root := mustReal(t, t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	repo := handRepo(t, filepath.Join(root, "r"))
	wt := handWorktree(t, repo, "wt", filepath.Join(root, "wt"))
	sub := filepath.Join(wt, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		scope Scope
		cwd   string
		want  string
	}{
		{ScopeRepo, repo, "repo:" + filepath.Join(repo, ".git")},
		{ScopeRepo, sub, "repo:" + filepath.Join(repo, ".git")},
		{ScopeWorktree, repo, "worktree:" + repo},
		{ScopeWorktree, sub, "worktree:" + wt},
	} {
		key, why := tc.scope.Key(CallContext{Cwd: tc.cwd, CallID: "c"})
		if key != tc.want || why != "" {
			t.Errorf("%s from %s: got %q (%s), want %q", tc.scope, tc.cwd, key, why, tc.want)
		}
	}

	status, detail, _ := DiagnoseGit(sub)
	if status != "ok" || !strings.Contains(detail, "resolved natively") || !strings.Contains(detail, "no git on PATH") {
		t.Errorf("doctor should report a native answer and no git: %s %s", status, detail)
	}
	if !strings.Contains(detail, "worktree "+wt) {
		t.Errorf("doctor should name the worktree: %s", detail)
	}
}

func TestWithoutGitAnUnreadableLayoutDegradesAndSaysWhy(t *testing.T) {
	withoutGit(t)
	root := mustReal(t, t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	repo := handRepo(t, filepath.Join(root, "r"))
	writeFile(t, filepath.Join(repo, ".git", "config"), "[core]\n\trepositoryformatversion = 2\n")

	key, why := ScopeRepo.Key(CallContext{Cwd: repo, CallID: "c"})
	if key != "cwd:"+repo {
		t.Errorf("expected the directory key, got %q", key)
	}
	if !strings.Contains(why, "version 2") || !strings.Contains(why, "git is not installed") {
		t.Errorf("the reason should say what native could not read and that git is missing: %q", why)
	}
	status, detail, fix := DiagnoseGit(repo)
	if status != "warn" || !strings.Contains(fix, "install git") {
		t.Errorf("doctor should warn and say how to fix it: %s %s / %s", status, detail, fix)
	}
}

func TestWithoutGitOutsideARepositoryIsNotAWarning(t *testing.T) {
	withoutGit(t)
	root := mustReal(t, t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	status, detail, _ := DiagnoseGit(root)
	if status != "ok" || !strings.Contains(detail, "not in a git repository") {
		t.Errorf("no repository and no git is a normal state: %s %s", status, detail)
	}
	key, why := ScopeWorktree.Key(CallContext{Cwd: root, CallID: "c"})
	if key != "cwd:"+root || !strings.Contains(why, "not in a git repository") {
		t.Errorf("got %q (%s)", key, why)
	}
}

func TestWithoutGitABrokenGitfileIsAWarning(t *testing.T) {
	withoutGit(t)
	root := mustReal(t, t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	repo := handRepo(t, filepath.Join(root, "r"))
	sub := filepath.Join(repo, "moved-wt")
	writeFile(t, filepath.Join(sub, ".git"), "gitdir: /nonexistent/.git/worktrees/x\n")
	status, detail, fix := DiagnoseGit(sub)
	if status != "warn" || !strings.Contains(detail, "not a git repository: /nonexistent") ||
		!strings.Contains(fix, "worktree repair") {
		t.Errorf("a worktree whose clone moved should be a warning with the fix: %s %s / %s", status, detail, fix)
	}
}

func TestDiscoveryStopsAtAFilesystemBoundary(t *testing.T) {
	// A mount point cannot be made without privileges, so the device lookup
	// is swapped for one that puts a boundary at <repo>/mnt.
	root := mustReal(t, t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	t.Setenv("GIT_DISCOVERY_ACROSS_FILESYSTEM", "")
	os.Unsetenv("GIT_DISCOVERY_ACROSS_FILESYSTEM")
	repo := handRepo(t, filepath.Join(root, "r"))
	mnt := filepath.Join(repo, "mnt")
	cwd := filepath.Join(mnt, "sub")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	real := gitDevice
	t.Cleanup(func() { gitDevice = real })
	gitDevice = func(p string) (uint64, error) {
		if p == mnt || strings.HasPrefix(p, mnt+"/") {
			return 2, nil
		}
		return 1, nil
	}

	_, err := discoverGit(cwd)
	var ge *gitError
	if !errors.As(err, &ge) || ge.kind != gitNotRepo || !strings.Contains(err.Error(), "mount point "+mnt) {
		t.Errorf("discovery should stop at the mount point: %v", err)
	}
	// Discovery that starts on the far side of the boundary is unaffected.
	if w, err := discoverGit(repo); err != nil || w.worktree != repo {
		t.Errorf("from the repository itself: %+v %v", w, err)
	}
	t.Setenv("GIT_DISCOVERY_ACROSS_FILESYSTEM", "1")
	if w, err := discoverGit(cwd); err != nil || w.worktree != repo {
		t.Errorf("GIT_DISCOVERY_ACROSS_FILESYSTEM=1 should cross it: %+v %v", w, err)
	}
}

func TestLongestAncestorLength(t *testing.T) {
	for _, tc := range []struct {
		path     string
		ceilings []string
		want     int
	}{
		{"/", []string{"/"}, -1},
		{"/a", []string{"/"}, 0},
		{"/a/b", []string{"/a"}, 2},
		{"/a/b", []string{"/a/"}, 2},
		{"/a", []string{"/a"}, -1},
		{"/ab", []string{"/a"}, -1},
		{"/a/b/c", []string{"/a", "/a/b", "/x"}, 4},
		{"/a/b", nil, -1},
	} {
		if got := longestAncestorLength(tc.path, tc.ceilings); got != tc.want {
			t.Errorf("longestAncestorLength(%q, %q) = %d, want %d", tc.path, tc.ceilings, got, tc.want)
		}
	}
}

func TestGitConfigParserAgainstGitsOwn(t *testing.T) {
	// The parser is also checked value by value against `git config
	// --file`, which is git's parser on the same bytes.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	for i, cfg := range []string{
		"[core]\n\tworktree = plain\n",
		"[core]\n\tworktree =   leading and trailing   \n",
		"[core]\n\tworktree = \"  quoted  \" tail ; comment\n",
		"[core]\n\tworktree = a\\tb\\\"c\\\\d\n",
		"[core]\n\tworktree = one\\\ntwo\n",
		"[Core]\n\tWorkTree = case\n",
		"[core] worktree = sameline\n",
		"\xEF\xBB\xBF[core]\r\n\tworktree = bom\r\n",
		"[core]\n\tworktree = first\n\tworktree = last wins\n",
		"[core]\n\tworktree = a # b\n",
		"[core]\n\tworktree = \"a # b\"\n",
	} {
		p := filepath.Join(dir, fmt.Sprintf("c%d", i))
		writeFile(t, p, cfg)
		out, err := exec.Command("git", "config", "--file", p, "--get-all", "core.worktree").Output()
		if err != nil {
			t.Fatalf("%q: git config: %v", cfg, err)
		}
		lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
		want := lines[len(lines)-1]
		var f repoFormat
		f.bare = -1
		if err := parseGitConfig([]byte(cfg), f.set); err != nil {
			t.Errorf("%q: %v", cfg, err)
			continue
		}
		if f.worktree == nil || *f.worktree != want {
			t.Errorf("%q: git reads %q, mcpx reads %v", cfg, want, f.worktree)
		}
	}
}

func BenchmarkDiscoverGit(b *testing.B) {
	root, _ := filepath.EvalSymlinks(b.TempDir())
	repo := filepath.Join(root, "r")
	for _, d := range []string{"objects", "refs"} {
		os.MkdirAll(filepath.Join(repo, ".git", d), 0o755)
	}
	os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	os.WriteFile(filepath.Join(repo, ".git", "config"), []byte("[core]\n\trepositoryformatversion = 0\n"), 0o644)
	deep := filepath.Join(repo, "a", "b", "c", "d", "e")
	os.MkdirAll(deep, 0o755)
	b.ResetTimer()
	for b.Loop() {
		if _, err := discoverGit(deep); err != nil {
			b.Fatal(err)
		}
	}
}
