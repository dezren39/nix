package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dezren39/mcpx/internal/runner"
	"github.com/dezren39/mcpx/internal/searchpath"
	"github.com/dezren39/mcpx/internal/settings"
)

// ScriptsDirNames are the per-project directories mcpx looks in for named
// scripts, most specific first. `.config/mcpx` wins over `.mcpx` because it is
// the more explicit spelling and nests with other tools' config.
var ScriptsDirNames = []string{
	filepath.Join(".config", "mcpx", "scripts"),
	filepath.Join(".mcpx", "scripts"),
}

// ScriptsDirName is the directory mcpx suggests creating.
var ScriptsDirName = ScriptsDirNames[0]

// ScriptExt is the extension a named script must carry on disk.
const ScriptExt = ".ts"

// scriptSearchDirs returns the ordered list of directories that can hold named
// scripts: every `.mcpx/scripts` from the working directory up to the
// filesystem root (nearest wins, so a repo overrides a parent), then the user
// directory. It mirrors how the config file is discovered, so a project can
// keep its scripts and its server list together.
// ScriptExtensions are tried in order for a name given without one. The
// order is deliberate: a project holding both foo.ts and foo.js is almost
// always compiling one into the other, and the source is what someone means
// to run.
var ScriptExtensions = []string{".ts", ".mts", ".js", ".mjs"}

// scriptPath resolves the configured search path, splicing the built-in list
// wherever the user left a null.
func scriptPath() searchpath.Resolved {
	wd, _ := os.Getwd()
	var configured []string
	if v := os.Getenv("MCPX_PATHS_SCRIPTS"); v != "" {
		for _, part := range strings.Split(v, string(os.PathListSeparator)) {
			if part = strings.TrimSpace(part); part != "" {
				if part == "-" || part == "null" {
					configured = append(configured, settings.NullMarker)
					continue
				}
				configured = append(configured, part)
			}
		}
	}
	return searchpath.Resolve(configured, settings.NullMarker, searchpath.Options{
		Dir:     wd,
		Builtin: builtinScriptDirs(),
	})
}

func builtinScriptDirs() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for {
			present := 0
			for _, name := range ScriptsDirNames {
				candidate := filepath.Join(dir, name)
				if st, err := os.Stat(candidate); err == nil && st.IsDir() {
					present++
				}
				add(candidate)
			}
			if present > 1 {
				warnBothScriptDirs(dir)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	home, _ := os.UserHomeDir()
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		add(filepath.Join(xdg, "mcpx", "scripts"))
	}
	if home != "" {
		add(filepath.Join(home, ".config", "mcpx", "scripts"))
	}
	return out
}

// scriptSearchDirs is the flat list, kept for the environment variable the
// runner passes to scripts.
func scriptSearchDirs() []string {
	var out []string
	for _, e := range scriptPath().Entries {
		out = append(out, e.Path)
	}
	return out
}

func allowOverlap() bool {
	return os.Getenv("MCPX_PLUMBING_ALLOW_TS_JS_OVERLAP") == "true"
}

// looksLikePath reports whether an argument should be used verbatim rather
// than resolved as a script name.
func looksLikePath(arg string) bool {
	return strings.ContainsRune(arg, filepath.Separator) ||
		strings.HasSuffix(arg, ScriptExt) ||
		strings.HasSuffix(arg, ".js") ||
		strings.HasSuffix(arg, ".mts") ||
		arg == "-"
}

// resolveScript turns `mcpx run report` into a concrete file. A name that
// already looks like a path is returned unchanged so existing usage and
// absolute paths keep working.
func resolveScript(arg string) (string, error) {
	if looksLikePath(arg) {
		return arg, nil
	}
	path := scriptPath()
	found, _, err := path.Find(arg, searchpath.FindOptions{
		Extensions:   ScriptExtensions,
		AllowOverlap: allowOverlap(),
	})
	if err != nil {
		return "", err
	}
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("no script named %q; looked in:\n%s\nCreate one with:\n  mkdir -p %s && $EDITOR %s",
		arg, path.Describe(), ScriptsDirName, filepath.Join(ScriptsDirName, arg+ScriptExt))
}

// ScriptEntry is one discoverable script.
type ScriptEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      string `json:"dir"`
	Summary  string `json:"summary,omitempty"`
	Shadowed bool   `json:"shadowed,omitempty"`
}

// discoverScripts lists every named script on the search path. Scripts found
// in a nearer directory shadow ones with the same name further out; the
// shadowed entries are still returned so `mcpx scripts` can show them.
func discoverScripts() ([]ScriptEntry, error) {
	var out []ScriptEntry
	claimed := map[string]bool{}
	for _, dir := range scriptSearchDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ScriptExt) {
				continue
			}
			// The generated client lives beside the scripts that import it; it
			// is machinery, not something a user would run.
			if e.Name() == runner.ClientFileName {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, n := range names {
			name := strings.TrimSuffix(n, ScriptExt)
			path := filepath.Join(dir, n)
			entry := ScriptEntry{Name: name, Path: path, Dir: dir, Summary: scriptSummary(path)}
			if claimed[name] {
				entry.Shadowed = true
			}
			claimed[name] = true
			out = append(out, entry)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no scripts found")
	}
	return out, nil
}

// scriptSummary reads the first line of leading comment from a script, so
// `mcpx scripts` can describe each one without opening it.
func scriptSummary(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if s, ok := strings.CutPrefix(line, "//"); ok {
			return strings.TrimSpace(s)
		}
		if s, ok := strings.CutPrefix(line, "/**"); ok {
			return strings.TrimSpace(strings.TrimSuffix(s, "*/"))
		}
		if s, ok := strings.CutPrefix(line, "/*"); ok {
			return strings.TrimSpace(strings.TrimSuffix(s, "*/"))
		}
		return ""
	}
	return ""
}

var warnedScriptDirs sync.Map

// warnBothScriptDirs complains once per directory when a project carries both
// spellings. Silently preferring one hides scripts the author expected to run.
func warnBothScriptDirs(dir string) {
	if _, seen := warnedScriptDirs.LoadOrStore(dir, true); seen {
		return
	}
	fmt.Fprintf(os.Stderr,
		"mcpx: %s has both %s and %s; %s wins. Consolidate to one.\n",
		dir, ScriptsDirNames[0], ScriptsDirNames[1], ScriptsDirNames[0])
}
