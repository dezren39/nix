package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The repo and worktree scopes, through the real binary and a live daemon.
// Repositories are laid out by hand -- HEAD, objects/, refs/, and for a
// linked worktree the two files git writes -- so these run with no git on
// PATH, which is the configuration the container ships.

const repoScoped = `{
  "mcpServers": {
    "byrepo": { "command": "FAKE", "mcpx": { "sharing": "shared", "scope": "repo" } },
    "bytree": { "command": "FAKE", "mcpx": { "sharing": "shared", "scope": "worktree" } }
  }
}`

func handGitRepo(t *testing.T, dir string) string {
	t.Helper()
	for _, d := range []string{"objects", "refs/heads"} {
		if err := os.MkdirAll(filepath.Join(dir, ".git", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeTestFile(t, filepath.Join(dir, ".git", "config"), "[core]\n\trepositoryformatversion = 0\n\tbare = false\n")
	return realDir(t, dir)
}

func handGitWorktree(t *testing.T, repo, name, dir string) string {
	t.Helper()
	admin := filepath.Join(repo, ".git", "worktrees", name)
	writeTestFile(t, filepath.Join(admin, "HEAD"), "ref: refs/heads/"+name+"\n")
	writeTestFile(t, filepath.Join(admin, "commondir"), "../..\n")
	writeTestFile(t, filepath.Join(admin, "gitdir"), filepath.Join(dir, ".git")+"\n")
	writeTestFile(t, filepath.Join(dir, ".git"), "gitdir: "+admin+"\n")
	return realDir(t, dir)
}

func writeTestFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func realDir(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// runtimesOnlyPath is a PATH with the JavaScript runtimes this machine has
// and nothing else -- in particular, no git.
func runtimesOnlyPath(t *testing.T) (string, int) {
	t.Helper()
	dir := t.TempDir()
	n := 0
	for _, name := range []string{"deno", "bun", "node"} {
		if p, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(p, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			n++
		}
	}
	return dir, n
}

// tryIn is e.try from another directory: the cwd is what the scopes key on.
func (e *env) tryIn(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.mcpx, args...)
	cmd.Dir = dir
	cmd.Env = e.envVars
	out, err := cmd.CombinedOutput()
	return string(out), harnessTimeout(ctx, err)
}

func (e *env) runIn(dir string, args ...string) string {
	e.t.Helper()
	out, err := e.tryIn(dir, args...)
	if err != nil {
		e.t.Fatalf("mcpx %s (in %s) failed: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

// instanceKeys is every live instance's key, per server.
func (e *env) instanceKeys() map[string][]string {
	e.t.Helper()
	var st struct {
		Servers []struct {
			Name      string `json:"name"`
			Instances []struct {
				Key string `json:"key"`
			} `json:"instances"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(jsonOf(e.t, e.run("--json", "status"))), &st); err != nil {
		e.t.Fatalf("status: %v", err)
	}
	keys := map[string][]string{}
	for _, s := range st.Servers {
		for _, in := range s.Instances {
			keys[s.Name] = append(keys[s.Name], in.Key)
		}
		sort.Strings(keys[s.Name])
	}
	return keys
}

func TestRepoAndWorktreeScopesResolveWithNoGitInstalled(t *testing.T) {
	e := newEnv(t, repoScoped)
	repo := handGitRepo(t, filepath.Join(e.dir, "proj"))
	side := handGitWorktree(t, repo, "side", filepath.Join(e.dir, "side"))
	deep := filepath.Join(side, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	path, runtimes := runtimesOnlyPath(t)
	// The daemon this starts inherits the PATH, and it is the daemon that
	// resolves keys.
	e.envVars = append(e.envVars, "PATH="+path,
		// Nothing above the test's directory can answer.
		"GIT_CEILING_DIRECTORIES="+realDir(t, filepath.Dir(e.dir)))

	for _, dir := range []string{repo, deep} {
		e.runIn(dir, "call", "byrepo.echo", `{"message":"r"}`)
		e.runIn(dir, "call", "bytree.echo", `{"message":"w"}`)
	}
	if runtimes > 0 {
		out := e.runIn(deep, "exec", `console.log(String(await byrepo.echo({ message: "from exec" })));`)
		if !strings.Contains(out, "from exec") {
			t.Errorf("exec should reach the server:\n%s", out)
		}
	}

	keys := e.instanceKeys()
	// One clone: every worktree shares one repo-scoped instance.
	if want := []string{"repo:" + filepath.Join(repo, ".git")}; strings.Join(keys["byrepo"], ",") != strings.Join(want, ",") {
		t.Errorf("repo scope: got %q, want %q", keys["byrepo"], want)
	}
	// Two checkouts: one instance each, keyed by the checkout's top, not by
	// the subdirectory the call came from.
	want := []string{"worktree:" + repo, "worktree:" + side}
	sort.Strings(want)
	if strings.Join(keys["bytree"], ",") != strings.Join(want, ",") {
		t.Errorf("worktree scope: got %q, want %q", keys["bytree"], want)
	}

	doctor, _ := e.tryIn(deep, "doctor", "-v")
	var line string
	for _, l := range strings.Split(doctor, "\n") {
		if strings.Contains(l, " git ") {
			line = l
		}
	}
	if !strings.HasPrefix(line, "ok") || !strings.Contains(line, "worktree "+side) ||
		!strings.Contains(line, "resolved natively") || !strings.Contains(line, "no git on PATH") {
		t.Errorf("doctor should report the native answer and that git is absent, without warning:\n%s", doctor)
	}
}

func TestTheDaemonsOwnGitDirDoesNotPinEveryCaller(t *testing.T) {
	// An autostarted daemon inherits the environment of whoever ran mcpx
	// first. A git hook exports GIT_DIR, so a hook that runs mcpx leaves a
	// daemon carrying it for hours; git honoured it for every later caller,
	// wherever they were.
	for _, tc := range []struct {
		name   string
		gitDir func(other string) string
	}{
		{"absolute, as a script might set it", func(other string) string { return filepath.Join(other, ".git") }},
		{"relative, as a hook sets it", func(string) string { return ".git" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, repoScoped)
			repo := handGitRepo(t, filepath.Join(e.dir, "proj"))
			other := handGitRepo(t, filepath.Join(e.dir, "other"))
			sub := filepath.Join(repo, "sub")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			e.envVars = append(e.envVars,
				"GIT_DIR="+tc.gitDir(other),
				"GIT_CEILING_DIRECTORIES="+realDir(t, filepath.Dir(e.dir)))

			e.runIn(sub, "call", "byrepo.echo", `{"message":"x"}`)
			e.runIn(sub, "call", "bytree.echo", `{"message":"x"}`)
			keys := e.instanceKeys()
			if got, want := strings.Join(keys["byrepo"], ","), "repo:"+filepath.Join(repo, ".git"); got != want {
				t.Errorf("repo scope from %s: got %q, want %q", sub, got, want)
			}
			if got, want := strings.Join(keys["bytree"], ","), "worktree:"+repo; got != want {
				t.Errorf("worktree scope from %s: got %q, want %q", sub, got, want)
			}
		})
	}
}
