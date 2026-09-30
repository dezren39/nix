package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/dezren39/mcpx/internal/defaults"
)

// Native repository discovery.
//
// The repo and worktree scopes need two facts about a directory: the
// repository's common directory (`git rev-parse --git-common-dir`) and the
// top of the worktree it is in (`--show-toplevel`). Both are a walk up the
// directory tree reading a handful of small files, and git's rules for that
// walk are fixed and documented in its source (setup.c:
// setup_git_directory_gently_1 and the functions it calls). This file is a
// port of those rules, so that finding a repository does not need a git
// binary -- and in a container image, git is 105 MB, mostly perl.
//
// Where the port and git could disagree, it does not guess: discovery
// returns gitUnsure and the caller asks git. docs/git-discovery.md lists every
// case and what was decided for it; the parity tests build each one with the
// real git and compare.

// gitWhere is both answers for one directory.
type gitWhere struct {
	// commonDir is what --git-common-dir prints: the directory every
	// worktree of one clone shares. Symlink-resolved, like git's.
	commonDir string
	// worktree is what --show-toplevel prints, or empty when there is none.
	worktree string
	// noWorktree says why worktree is empty when commonDir is not.
	noWorktree string
}

type gitErrKind int

const (
	// gitNotRepo is the ordinary answer outside a repository.
	gitNotRepo gitErrKind = iota
	// gitBroken is something on disk git itself would die on: a .git file
	// that points nowhere, an unreadable commondir. Git fails for these
	// too, so there is nothing to ask it.
	gitBroken
	// gitUnsure is a layout this port does not vouch for. Git decides.
	gitUnsure
	// gitFailed is git asked and refusing, or git not there to ask.
	gitFailed
)

type gitError struct {
	kind  gitErrKind
	msg   string
	cause error
}

func (e *gitError) Error() string { return e.msg }
func (e *gitError) Unwrap() error { return e.cause }

func gitErr(kind gitErrKind, format string, args ...any) error {
	return &gitError{kind: kind, msg: fmt.Sprintf(format, args...)}
}

// Facts of git's on-disk format rather than preferences, which is why they
// are not in defaults.json: changing one would only make mcpx disagree with
// git about which files are valid.
const (
	// headProbe is how much of HEAD validate_headref reads.
	headProbe = 255
	// sha1Hex is the shortest object name a detached HEAD can hold. A
	// SHA-256 one is longer and begins with 40 hex digits too.
	sha1Hex = 40
	// dotGit is the name discovery looks for while walking up. The names
	// read *inside* a git directory -- HEAD, config, objects, refs,
	// commondir, config.worktree -- are spelled at their single use sites,
	// where the format they belong to is in view.
	dotGit = ".git"
)

// discoverGit is setup_git_directory_gently_1 with GIT_DIR unset, followed by
// the setup_*_git_dir step that decides the worktree.
//
// The environment variables that name a repository outright -- GIT_DIR,
// GIT_WORK_TREE, GIT_COMMON_DIR, GIT_OBJECT_DIRECTORY -- are not read. This
// runs in the daemon, for many callers; its environment is whoever happened
// to start it, and honouring a GIT_DIR from there would pin every caller to
// one repository. The two that tune the search itself --
// GIT_CEILING_DIRECTORIES and GIT_DISCOVERY_ACROSS_FILESYSTEM -- are read:
// they are machine policy, set in a profile, and whoever set one expects mcpx
// to agree with their shell's git.
func discoverGit(cwd string) (gitWhere, error) {
	if cwd == "" {
		return gitWhere{}, gitErr(gitNotRepo, "cwd is unknown")
	}
	if !filepath.IsAbs(cwd) {
		if wd, err := os.Getwd(); err == nil {
			cwd = filepath.Join(wd, cwd)
		}
	}
	// Git walks the physical path: it starts from getcwd(), which has
	// already resolved every symlink. Walking the logical one would find a
	// .git beside a symlink rather than beside its target.
	dir, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		if missing(err) {
			return gitWhere{}, gitErr(gitNotRepo, "%s does not exist", cwd)
		}
		return gitWhere{}, gitErr(gitUnsure, "cannot resolve %s: %v", cwd, err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return gitWhere{}, gitErr(gitNotRepo, "%s is not a directory", cwd)
	}

	ceil := ceilingOffset(dir)
	oneFS := true
	if v, ok := os.LookupEnv("GIT_DISCOVERY_ACROSS_FILESYSTEM"); ok {
		across, err := gitBool("GIT_DISCOVERY_ACROSS_FILESYSTEM", &v)
		if err != nil {
			return gitWhere{}, gitErr(gitUnsure, "%v", err)
		}
		oneFS = !across
	}
	var dev0 uint64
	if oneFS {
		if dev0, err = gitDevice(dir); err != nil {
			return gitWhere{}, gitErr(gitUnsure, "cannot stat %s: %v", dir, err)
		}
	}

	// At each level: a .git file or directory, then the directory itself as
	// a bare repository, then the parent. That order is git's.
	for cur := dir; ; {
		gitdir, err := probeDotGit(cur)
		if err != nil {
			return gitWhere{}, err
		}
		if gitdir != "" {
			return setupGitDir(cur, gitdir, false)
		}
		isRepo, err := isGitDir(cur)
		if err != nil {
			return gitWhere{}, err
		}
		if isRepo {
			return setupGitDir(cur, cur, true)
		}
		parent, why := gitParent(cur, ceil)
		if parent == "" {
			return gitWhere{}, gitErr(gitNotRepo, "not in a git repository%s", why)
		}
		if oneFS {
			d, err := gitDevice(parent)
			if err != nil {
				return gitWhere{}, gitErr(gitUnsure, "cannot stat %s: %v", parent, err)
			}
			if d != dev0 {
				return gitWhere{}, gitErr(gitNotRepo,
					"not in a git repository (discovery stops at the mount point %s; "+
						"GIT_DISCOVERY_ACROSS_FILESYSTEM is not set)", cur)
			}
		}
		cur = parent
	}
}

// gitParent is the step up, or "" with the reason there is none. The
// arithmetic is git's: a parent is only examined when its length is greater
// than the ceiling's, and "/" counts as length zero, so a ceiling is never
// itself examined -- except when the walk starts there, because
// longestAncestorLength does not count a directory as its own ancestor.
func gitParent(cur string, ceil int) (string, string) {
	if cur == string(filepath.Separator) {
		return "", ""
	}
	parent := filepath.Dir(cur)
	off := len(parent)
	if parent == string(filepath.Separator) {
		off = 0
	}
	if off <= ceil {
		return "", " (GIT_CEILING_DIRECTORIES stops discovery above " + cur + ")"
	}
	return parent, ""
}

// ceilingOffset is how setup.c reads GIT_CEILING_DIRECTORIES: relative
// entries are ignored, the rest are symlink-resolved -- unless they come
// after an empty entry, which is git's switch for "do not touch these, they
// are slow automounts". Unresolvable entries are dropped.
func ceilingOffset(dir string) int {
	env, ok := os.LookupEnv("GIT_CEILING_DIRECTORIES")
	if !ok {
		return -1
	}
	var ceilings []string
	raw := false
	for _, c := range strings.Split(env, string(os.PathListSeparator)) {
		switch {
		case c == "":
			raw = true
		case !filepath.IsAbs(c):
		case raw:
			ceilings = append(ceilings, c)
		default:
			if r, ok := realpathMissingLast(c); ok {
				ceilings = append(ceilings, r)
			}
		}
	}
	return longestAncestorLength(dir, ceilings)
}

// longestAncestorLength is path.c's: the length of the longest ceiling that
// is a proper ancestor of path, with a trailing slash not counted, or -1.
func longestAncestorLength(path string, ceilings []string) int {
	if path == "/" {
		return -1
	}
	longest := -1
	for _, c := range ceilings {
		n := len(c)
		if n > 0 && c[n-1] == '/' {
			n--
		}
		if !strings.HasPrefix(path, c[:n]) || len(path) <= n+1 || path[n] != '/' {
			continue
		}
		longest = max(longest, n)
	}
	return longest
}

// probeDotGit is read_gitfile_gently plus the switch around it in 2.54 and
// later: no .git moves on; a .git directory counts if it is a repository and
// is skipped if not; a .git file must be a valid gitfile or discovery dies;
// anything else dies. Returns the git directory, or "".
func probeDotGit(cur string) (string, error) {
	p := slashJoin(cur, dotGit)
	fi, err := os.Stat(p)
	if err != nil {
		if missing(err) {
			return "", nil
		}
		return "", gitErr(gitBroken, "error reading '%s': %v", p, err)
	}
	if fi.IsDir() {
		ok, err := isGitDir(p)
		if err != nil || !ok {
			return "", err
		}
		return p, nil
	}
	if !fi.Mode().IsRegular() {
		return "", gitErr(gitBroken, "not a regular file: '%s'", p)
	}
	if fi.Size() > defaults.GitfileMaxBytes {
		return "", gitErr(gitBroken, "too large to be a .git file: '%s'", p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", gitErr(gitBroken, "error reading %s: %v", p, err)
	}
	s, ok := strings.CutPrefix(string(b), "gitdir: ")
	if !ok {
		return "", gitErr(gitBroken, "invalid gitfile format: %s", p)
	}
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return "", gitErr(gitBroken, "no path in gitfile: %s", p)
	}
	s = cString(s)
	// Relative to the directory holding the .git file, joined as text:
	// the kernel, not a lexical Clean, resolves any "..", so a symlink in
	// the path means what it means to git.
	if !filepath.IsAbs(s) {
		s = strings.TrimSuffix(p, dotGit) + s
	}
	ok, err = isGitDir(s)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", gitErr(gitBroken, "not a git repository: %s", s)
	}
	real, err := filepath.EvalSymlinks(s)
	if err != nil {
		return "", gitErr(gitBroken, "cannot resolve %s: %v", s, err)
	}
	return real, nil
}

// isGitDir is is_git_directory: a valid HEAD here, and objects/ and refs/ in
// the common directory. A linked worktree's private directory passes through
// its commondir file; that is how it counts as a repository at all.
func isGitDir(suspect string) (bool, error) {
	if !validHead(slashJoin(suspect, "HEAD")) {
		return false, nil
	}
	common, _, err := commonDirOf(suspect)
	if err != nil {
		return false, err
	}
	return accessX(slashJoin(common, "objects")) && accessX(slashJoin(common, "refs")), nil
}

// commonDirOf is get_common_dir_noenv. A commondir file that exists (even as
// a dangling symlink -- git tests with lstat) but cannot be read is fatal in
// git, so it is gitBroken here.
func commonDirOf(gitdir string) (string, bool, error) {
	p := slashJoin(gitdir, "commondir")
	if _, err := os.Lstat(p); err != nil {
		return gitdir, false, nil
	}
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return "", false, gitErr(gitBroken, "failed to read %s", p)
	}
	s := cString(strings.TrimRight(string(b), "\r\n"))
	if !filepath.IsAbs(s) {
		s = gitdir + "/" + s
	}
	real, ok := realpathMissingLast(s)
	if !ok {
		return "", false, gitErr(gitBroken, "%s names %s, which cannot be resolved", p, s)
	}
	return real, true, nil
}

// validHead is validate_headref: a symlink into refs/, a symbolic ref into
// refs/, or an object name.
func validHead(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		t, err := os.Readlink(path)
		return err == nil && strings.HasPrefix(t, "refs/")
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, headProbe)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false
	}
	s := cString(string(buf[:n]))
	if ref, ok := strings.CutPrefix(s, "ref:"); ok {
		if strings.HasPrefix(strings.TrimLeft(ref, " \t\n\r"), "refs/") {
			return true
		}
	}
	return len(s) >= sha1Hex && isHex(s[:sha1Hex])
}

// setupGitDir decides the worktree once a git directory is found: it is
// check_repository_format_gently followed by setup_discovered_git_dir, or by
// setup_bare_git_dir when the directory itself was the repository.
func setupGitDir(dir, gitdir string, bareDiscovery bool) (gitWhere, error) {
	common, hasCommon, err := commonDirOf(gitdir)
	if err != nil {
		return gitWhere{}, err
	}
	cfgPath := slashJoin(common, "config")
	f := repoFormat{version: -1, bare: -1}
	if err := readGitConfig(cfgPath, f.set); err != nil {
		return gitWhere{}, err
	}
	var bare = -1
	var wt *string
	// Without core.repositoryformatversion git discards everything else it
	// read, so core.bare and core.worktree then mean nothing here.
	if f.version >= 0 {
		if err := f.verify(); err != nil {
			return gitWhere{}, gitErr(gitUnsure, "%s: %v", cfgPath, err)
		}
		if f.worktreeConfig {
			if err := readGitConfig(slashJoin(gitdir, "config.worktree"), f.setWorktree); err != nil {
				return gitWhere{}, err
			}
			// With per-worktree config git applies core.bare and
			// core.worktree even to a linked worktree -- including the
			// common config's, which is how enabling worktreeConfig on a
			// bare clone makes every one of its worktrees bare.
			hasCommon = false
		}
		// Otherwise a linked worktree ignores both: they describe the main
		// one.
		if !hasCommon {
			bare, wt = f.bare, f.worktree
		}
	}

	commonReal, err := filepath.EvalSymlinks(common)
	if err != nil {
		return gitWhere{}, gitErr(gitBroken, "cannot resolve %s: %v", common, err)
	}
	w := gitWhere{commonDir: commonReal}
	switch {
	case wt != nil && bare > 0:
		w.noWorktree = "core.bare and core.worktree are both set in " + cfgPath + "; git treats that as bare"
	case wt != nil:
		top, err := coreWorktree(gitdir, *wt)
		if err != nil {
			// Git dies during setup here, before printing either answer.
			return gitWhere{}, err
		}
		w.worktree = top
	case bare > 0:
		w.noWorktree = "core.bare is true in " + cfgPath
	case bareDiscovery:
		w.noWorktree = dir + " is a git directory, not a checkout"
	default:
		w.worktree = dir
	}
	return w, nil
}

// coreWorktree resolves core.worktree the way setup_explicit_git_dir does.
// An absolute value goes through realpath, which tolerates a missing last
// component; a relative one is two chdirs, from the git directory, and both
// must succeed.
func coreWorktree(gitdir, value string) (string, error) {
	if filepath.IsAbs(value) {
		if r, ok := realpathMissingLast(value); ok {
			return r, nil
		}
		return "", gitErr(gitBroken, "core.worktree %s cannot be resolved", value)
	}
	r, err := filepath.EvalSymlinks(gitdir + "/" + value)
	if err == nil {
		var fi os.FileInfo
		if fi, err = os.Stat(r); err == nil && !fi.IsDir() {
			err = syscall.ENOTDIR
		}
	}
	if err != nil {
		return "", gitErr(gitBroken, "cannot chdir to core.worktree '%s' from %s: %v", value, gitdir, err)
	}
	return r, nil
}

// readGitConfig feeds one config file to fn. A missing file is an empty one,
// as in git. Anything else that stops it being read -- permissions, a
// syntax error, a value this port does not parse -- is unsure.
func readGitConfig(path string, fn func(string, *string) error) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return gitErr(gitUnsure, "cannot read %s: %v", path, err)
	}
	if err := parseGitConfig(b, fn); err != nil {
		return gitErr(gitUnsure, "cannot read %s natively: %v", path, err)
	}
	return nil
}

// realpathMissingLast is strbuf_realpath as git calls it for ceilings,
// commondir and core.worktree: every component resolved, and only the last
// allowed not to exist.
func realpathMissingLast(p string) (string, bool) {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r, true
	} else if !missing(err) {
		return "", false
	}
	p = filepath.Clean(p)
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return "", false
	}
	last := filepath.Join(parent, filepath.Base(p))
	if _, err := os.Lstat(last); !missing(err) {
		return "", false
	}
	return last, true
}

// missing is ENOENT or ENOTDIR: the two ways a path is simply not there.
func missing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// slashJoin appends one component the way strbuf_complete does, without
// cleaning -- "/" stays "/" rather than becoming "//".
func slashJoin(dir, name string) string {
	if strings.HasSuffix(dir, "/") {
		return dir + name
	}
	return dir + "/" + name
}

// cString truncates at the first NUL, which is where git's C strings end.
func cString(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}
	return s
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
