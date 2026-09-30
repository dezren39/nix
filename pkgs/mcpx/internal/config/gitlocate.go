package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dezren39/mcpx/internal/defaults"
)

// locateGit answers from disk, and asks git only when discovery is unsure.
// how says which one answered.
//
// Nothing is cached. The old git-backed lookup cached every answer for the
// daemon's lifetime, negative ones included, so a directory that became a
// repository after the first call there stayed "not a repository" for hours.
// A native answer costs a few dozen stat calls (BenchmarkDiscoverGit), which
// is cheaper than the lock a cache would take.
func locateGit(cwd string) (w gitWhere, how string, err error) {
	w, err = discoverGit(cwd)
	var ge *gitError
	if !errors.As(err, &ge) || ge.kind != gitUnsure {
		return w, "native", err
	}
	gw, gerr := revParse(cwd)
	if gerr != nil {
		return gitWhere{}, "git", &gitError{kind: gitFailed, msg: ge.msg + "; " + gerr.Error(), cause: gerr}
	}
	return gw, "git", nil
}

// errNoGit is the fallback with nothing to fall back to.
var errNoGit = errors.New("git is not installed to decide")

// revParse asks git both questions in one process. --git-common-dir prints
// before --show-toplevel dies, so a bare repository still yields its common
// directory.
func revParse(cwd string) (gitWhere, error) {
	bin, err := exec.LookPath(defaults.GitBin)
	if err != nil {
		return gitWhere{}, errNoGit
	}
	cmd := exec.Command(bin, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-toplevel")
	cmd.Dir = cwd
	cmd.Env = gitEnv(os.Environ())
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()

	var lines []string
	if s := strings.TrimRight(string(out), "\n"); s != "" {
		lines = strings.Split(s, "\n")
	}
	for _, l := range lines {
		// Before 2.31 --path-format is not an option, and rev-parse echoes
		// what it does not understand.
		if !filepath.IsAbs(l) {
			return gitWhere{}, fmt.Errorf("%s printed %q; --path-format needs git 2.31 or later", bin, l)
		}
	}
	why := firstLine(stderr.String())
	if why == "" && runErr != nil {
		why = runErr.Error()
	}
	var w gitWhere
	if len(lines) > 0 {
		w.commonDir = canonical(lines[0])
	}
	if runErr == nil && len(lines) > 1 {
		w.worktree = canonical(lines[1])
	}
	if w.commonDir == "" {
		return gitWhere{}, fmt.Errorf("git rev-parse: %s", why)
	}
	if w.worktree == "" {
		w.noWorktree = "git rev-parse: " + why
	}
	return w, nil
}

// gitLocalEnv is git's local_repo_env (environment.c), printed by `git
// rev-parse --local-env-vars`: the variables that describe *the* repository
// a process is working on. Git clears exactly these before it works on a
// different one -- a submodule -- which is the position the daemon is in
// for every caller. It is the union over git versions, since the fallback
// runs whichever git is installed; a test checks it against that git.
var gitLocalEnv = []string{
	// Before 2.40 (Debian bookworm ships 2.39).
	"GIT_INTERNAL_SUPER_PREFIX",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_CONFIG",
	"GIT_CONFIG_PARAMETERS",
	"GIT_CONFIG_COUNT",
	"GIT_OBJECT_DIRECTORY",
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_IMPLICIT_WORK_TREE",
	"GIT_GRAFT_FILE",
	"GIT_INDEX_FILE",
	"GIT_NO_REPLACE_OBJECTS",
	"GIT_REPLACE_REF_BASE",
	"GIT_PREFIX",
	"GIT_SHALLOW_FILE",
	"GIT_COMMON_DIR",
}

// gitEnv is env without gitLocalEnv, so the fallback answers the question
// native discovery answers: which repository is this directory in.
func gitEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(gitLocalEnv, name) {
			out = append(out, kv)
		}
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// DiagnoseGit is what `mcpx doctor` reports about the repo and worktree
// scopes from dir: what they resolve to, what resolved them, and whether a
// git is there for the layouts native discovery hands on. git's absence is
// not a warning; it is only worth one when a repository here needs it.
func DiagnoseGit(dir string) (status, detail, fix string) {
	fallback := "no git on PATH, which only matters for a repository native discovery cannot read"
	if p, err := exec.LookPath(defaults.GitBin); err == nil {
		fallback = "git at " + p + " is the fallback"
	}
	w, how, err := locateGit(dir)
	var ge *gitError
	errors.As(err, &ge)
	switch {
	case err == nil:
		d := "repo " + w.commonDir
		if w.worktree != "" {
			d += ", worktree " + w.worktree
		} else {
			d += ", no worktree (" + w.noWorktree + ")"
		}
		by := "resolved natively"
		if how == "git" {
			by = "resolved by git"
		}
		return "ok", d + "; " + by + "; " + fallback, ""
	case ge != nil && ge.kind == gitNotRepo:
		return "ok", "not in a git repository here, so repo and worktree scopes key by directory; " + fallback, ""
	case ge != nil && ge.kind == gitBroken:
		return "warn", err.Error(),
			"repo and worktree scopes key by directory until it is fixed; `git worktree repair` mends a worktree whose clone moved"
	case errors.Is(err, errNoGit):
		return "warn", err.Error(), "install git, or see docs/git-discovery.md for what native discovery reads"
	default:
		return "warn", err.Error(), "repo and worktree scopes key by directory here until git accepts the repository"
	}
}
