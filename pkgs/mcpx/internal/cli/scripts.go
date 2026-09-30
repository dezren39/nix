package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dezren39/mcpx/internal/recipes"
	"github.com/dezren39/mcpx/internal/runner"
	"github.com/dezren39/mcpx/internal/searchpath"
	"github.com/dezren39/mcpx/internal/settings"
	"github.com/dezren39/mcpx/internal/source"
)

// ScriptsDirNames are the per-project directories mcpx looks in for named
// scripts, most specific first. `.config/mcpx` wins over `.mcpx` because it is
// the more explicit spelling and nests with other tools' config.
var ScriptsDirNames = recipes.DirNames

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
	// The resolved settings carry the configured list, wherever it was set.
	// The environment variable is read directly as well, because script
	// resolution happens on paths that do not always have an App -- notably
	// inside the daemon.
	var configured []string
	if plumbingApp != nil {
		configured = splitPathList(plumbingApp.Settings().String("paths.scripts"))
	}
	if len(configured) == 0 {
		configured = splitPathList(os.Getenv("MCPX_PATHS_SCRIPTS"))
	}
	return searchpath.Resolve(configured, settings.NullMarker, searchpath.Options{
		Dir:     wd,
		Builtin: builtinScriptDirs(),
	})
}

// builtinScriptDirs is recipes.Dirs plus the warning.
//
// The list itself lives in internal/recipes because the daemon serves
// recipes from the same directories and two copies would drift the moment
// one of them gained a spelling. The warning stays here: a package that
// prints is a package a daemon cannot use.
func builtinScriptDirs() []string {
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	dirs, both := recipes.Dirs(wd, home, os.Getenv("XDG_CONFIG_HOME"))
	for _, dir := range both {
		warnBothScriptDirs(dir)
	}
	return dirs
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

// plumbingApp is the App whose settings the free functions in this file
// consult. Script resolution is reached from several places that do not carry
// an App, and threading one through every call site to read two booleans
// would be a worse trade than a package-level pointer set once at startup.
var plumbingApp *App

// SetPlumbingSource tells the script resolver which App to read settings from.
func SetPlumbingSource(a *App) { plumbingApp = a }

func plumbingBool(path string) bool {
	if plumbingApp == nil {
		// No App means no config file and no flags -- a daemon, or a test
		// calling in directly. The declared default is still the right
		// answer, and it used to return false regardless, which silently
		// inverted every switch whose default is on.
		return defaultPlumbing().Bool(path)
	}
	return plumbingApp.Plumbing(path)
}

var (
	defaultPlumbingOnce sync.Once
	defaultPlumbingSet  *settings.Set
)

func defaultPlumbing() *settings.Set {
	defaultPlumbingOnce.Do(func() {
		sch, err := settings.New(settings.Registry())
		if err != nil {
			panic("mcpx: settings registry is invalid: " + err.Error())
		}
		defaultPlumbingSet = settings.NewSet(sch)
	})
	return defaultPlumbingSet
}

func allowOverlap() bool { return plumbingBool("plumbing.allowTsJsOverlap") }

// plumbingSourceOptions builds source.Options from the plumbing settings.
//
// source.Options has always said in its doc comment that it comes from the
// plumbing settings; it did not. Every call site wrote the three booleans as
// literals, so plumbing.sourceDirAllowed, sourceDirRecursive and
// sourceProbePaths were three switches wired to nothing. The literals also
// disagreed with each other -- a launcher refused a directory while a phase
// accepted one -- which is the kind of difference a setting exists to make
// deliberate.
//
// Extensions stays a caller's argument: it is not a policy, it is what this
// particular call is willing to read.
func plumbingSourceOptions(dir string, exts []string) source.Options {
	return source.Options{
		Dir:        dir,
		AllowDir:   plumbingBool("plumbing.sourceDirAllowed"),
		Recursive:  plumbingBool("plumbing.sourceDirRecursive"),
		Probe:      plumbingBool("plumbing.sourceProbePaths"),
		Extensions: exts,
	}
}

// PlaceholderFiles lists every file on the placeholder search path.
//
// Empty by default: declaring placeholders is opt-in, because scanning every
// script directory for declarations would make an ordinary script's filename
// quietly meaningful.
func PlaceholderFiles() []string {
	if plumbingApp == nil {
		return nil
	}
	configured := splitPathList(plumbingApp.Settings().String("paths.placeholders"))
	if len(configured) == 0 {
		return nil
	}
	wd, _ := os.Getwd()
	resolved := searchpath.Resolve(configured, settings.NullMarker, searchpath.Options{
		Dir:           wd,
		Builtin:       builtinPlaceholderDirs(),
		RequireExists: true,
	})
	var out []string
	for _, e := range resolved.Entries {
		if e.IsFile {
			out = append(out, e.Path)
			continue
		}
		entries, err := os.ReadDir(e.Path)
		if err != nil {
			continue
		}
		for _, de := range entries {
			if de.IsDir() {
				continue
			}
			switch strings.ToLower(filepath.Ext(de.Name())) {
			case ".ts", ".js", ".mts", ".mjs":
				out = append(out, filepath.Join(e.Path, de.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}

func builtinPlaceholderDirs() []string {
	var out []string
	if wd, err := os.Getwd(); err == nil {
		out = append(out,
			filepath.Join(wd, ".config", "mcpx", "placeholders"),
			filepath.Join(wd, ".mcpx", "placeholders"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".config", "mcpx", "placeholders"))
	}
	return out
}

// splitPathList accepts either a JSON array, as a configuration file holds
// it, or a separator-joined string, as an environment variable must. "-" and
// "null" both stand for the built-in list, because one of them is what
// somebody will type.
func splitPathList(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	var parts []string
	if strings.HasPrefix(v, "[") {
		var arr []any
		if json.Unmarshal([]byte(v), &arr) == nil {
			for _, e := range arr {
				if e == nil {
					parts = append(parts, settings.NullMarker)
					continue
				}
				parts = append(parts, fmt.Sprint(e))
			}
			return parts
		}
	}
	for _, part := range strings.Split(v, string(os.PathListSeparator)) {
		part = strings.TrimSpace(part)
		switch part {
		case "":
		case "-", "null":
			parts = append(parts, settings.NullMarker)
		default:
			parts = append(parts, part)
		}
	}
	return parts
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
