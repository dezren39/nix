package defaults_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A documentation link that points at nothing is the documentation form of
// #177: something declared and not delivered. Nothing checked for it, and the
// cost is paid by whoever follows the link -- one document in this tree named
// three files that had never existed, and the time to discover that was spent
// three separate times before anyone wrote it down.
//
// This lives beside the constants guard because that is the other test that
// walks the whole tree rather than one package's files; there is no `docs`
// package to put it in, and inventing one for a single test would be worse.
//
// It is deliberately cheap: two regexps and a stat per candidate. It checks
// two things, which are the two ways a document names a file:
//
//   - a relative markdown link, `[text](path)`, resolved against the
//     directory of the document that contains it;
//   - a backticked repository path, `internal/cli/root.go:40`, resolved
//     against the repository root.
//
// External URLs and bare anchors are skipped: this test may not reach the
// internet (a previous one did, and failed only in CI), and an anchor is a
// claim about a heading rather than about a file.

var (
	// mdLink matches [text](target). The target stops at a space so a
	// titled link -- [t](p "title") -- yields just the path.
	mdLink = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)`)
	// backticked matches the contents of a single-backtick span. Multi-line
	// fenced blocks are left alone: they are transcripts, not references.
	backticked = regexp.MustCompile("`([^`\n]+)`")
	// lineSuffix is the `:40` or `:40-52` a reference to a specific line
	// carries. It is part of the citation, not of the path.
	lineSuffix = regexp.MustCompile(`:\d+(-\d+)?$`)
	// pathShaped is conservative: only the characters a path is made of, and
	// no spaces, so `mcpx serve --transport http` is not a candidate.
	pathShaped = regexp.MustCompile(`^[A-Za-z0-9_.@\-]+(/[A-Za-z0-9_.@\-]+)+/?$`)
)

// repoRoot is pkgs/mcpx: the root of the Go module and of everything the
// documentation cites. It is also what the nix derivation copies, so the
// test sees the same tree there as here.
const repoRoot = "../.."

// skipDirs are not documentation and not cited.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "bin": true, "result": true,
}

// foreignPrefixes are paths inside somebody else's repository that happen to
// start with a directory name this one also has.
//
// `docs/compare/**` is a register of what other specifications say, and it
// cites them by their own layout. The modelcontextprotocol repository keeps
// its prose under `docs/extensions/` and `docs/docs/`; neither exists here and
// neither is a claim about this tree. Named as prefixes rather than as whole
// paths so a new citation into the same spec does not have to be added, and
// narrowly enough that `docs/archive.md` -- a real dangling reference this
// found -- is still caught.
var foreignPrefixes = []string{
	"docs/extensions/",
	"docs/docs/",
}

// TestEveryDocumentationLinkResolves fails on a relative link or a backticked
// repository path that names a file that is not there.
func TestEveryDocumentationLinkResolves(t *testing.T) {
	docs, err := markdownFiles(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("no markdown found; the walk root is wrong")
	}
	// The set of top-level names is what tells a repository path from a URL
	// path or a command fragment. `github.com/x/y` and `/v1/health` do not
	// begin with one of these; `internal/cli/root.go` does.
	tops, err := topLevelNames(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	var dangling []string
	for _, doc := range docs {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, ref := range referencesIn(string(b), tops) {
			var abs string
			if ref.relative {
				abs = filepath.Join(filepath.Dir(doc), ref.path)
			} else {
				abs = filepath.Join(repoRoot, ref.path)
			}
			if _, err := os.Stat(abs); err != nil {
				rel, _ := filepath.Rel(repoRoot, doc)
				dangling = append(dangling,
					filepath.ToSlash(rel)+":"+strconv.Itoa(ref.line)+": "+ref.raw)
			}
		}
	}
	sort.Strings(dangling)
	if len(dangling) > 0 {
		t.Errorf("documentation names %d file(s) that do not exist:\n  %s",
			len(dangling), strings.Join(dangling, "\n  "))
	}
}

type reference struct {
	path     string // cleaned, with any anchor and line suffix removed
	raw      string // as written, for the failure message
	line     int
	relative bool // resolved against the document, not the repository root
}

// referencesIn extracts every file a document claims exists.
func referencesIn(doc string, tops map[string]bool) []reference {
	var out []reference
	inFence := false
	for i, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
			target := m[1]
			if isExternal(target) || strings.HasPrefix(target, "#") {
				continue
			}
			p := stripAnchor(target)
			// A target with neither a separator nor an extension does not
			// name a file: `…` is a placeholder in a quoted example, not a
			// broken link. `client-obligations.md` still qualifies.
			if p == "" || (!strings.Contains(p, "/") && !strings.Contains(p, ".")) {
				continue
			}
			out = append(out, reference{path: p, raw: m[0] + ")", line: i + 1, relative: true})
		}
		for _, m := range backticked.FindAllStringSubmatch(line, -1) {
			p := lineSuffix.ReplaceAllString(m[1], "")
			if !pathShaped.MatchString(p) || isExternal(p) {
				continue
			}
			// `pkgs/mcpx/...` is how a document addresses this tree from the
			// outer nix repository; the same file, named from further out.
			p = strings.TrimPrefix(p, "pkgs/mcpx/")
			if !tops[strings.Split(p, "/")[0]] || isForeign(p) {
				continue
			}
			out = append(out, reference{path: p, raw: m[0], line: i + 1})
		}
	}
	return out
}

func isExternal(s string) bool {
	for _, p := range []string{"http://", "https://", "mailto:", "//"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func isForeign(p string) bool {
	for _, pre := range foreignPrefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

func stripAnchor(s string) string {
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	return s
}

func markdownFiles(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			// README.md is a symlink to docs/story.md. Its relative links
			// are written for docs/, so following the symlink would resolve
			// every one of them from the wrong directory and report ten
			// failures for a file that is walked correctly anyway.
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func topLevelNames(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out, nil
}
