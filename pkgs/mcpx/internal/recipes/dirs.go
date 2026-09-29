package recipes

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirNames are the per-project directories a script may live in, most
// specific first. Declared here rather than in the CLI because the daemon
// serves recipes too, and two copies of this list would drift the moment one
// of them gained a spelling.
var DirNames = []string{
	filepath.Join(".config", "mcpx", "scripts"),
	filepath.Join(".mcpx", "scripts"),
}

// Ext is the extension a saved script carries.
const Ext = ".ts"

// Dirs is every directory that may hold a saved script, nearest first:
// each project directory from dir up to the root, then the user's.
//
// Both is the subset that carry both spellings at once, which the CLI warns
// about. Returned rather than warned about here, because a package that
// prints is a package that cannot be used from a daemon.
func Dirs(dir, home, xdg string) (all []string, both []string) {
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		all = append(all, p)
	}
	if dir != "" {
		for {
			present := 0
			for _, name := range DirNames {
				candidate := filepath.Join(dir, name)
				if st, err := os.Stat(candidate); err == nil && st.IsDir() {
					present++
				}
				add(candidate)
			}
			if present > 1 {
				both = append(both, dir)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if xdg != "" {
		add(filepath.Join(xdg, "mcpx", "scripts"))
	}
	if home != "" {
		add(filepath.Join(home, ".config", "mcpx", "scripts"))
	}
	return all, both
}

// Load reads every recipe from a list of directories.
//
// A name found in a nearer directory shadows the same name further out, which
// is the rule the script search path already follows. Shadowed files are
// dropped rather than reported: a recipe list is a menu, and two entries with
// one name is not a menu.
func Load(dirs []string, skip map[string]bool) []Recipe {
	var out []Recipe
	claimed := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) || skip[e.Name()] {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, n := range names {
			name := strings.TrimSuffix(n, Ext)
			if claimed[name] {
				continue
			}
			claimed[name] = true
			path := filepath.Join(dir, n)
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			out = append(out, Parse(name, path, string(b)))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SaveDir is where a new recipe should be written: the nearest project
// directory on the path, created if it is not there yet.
//
// The user directory is deliberately not the answer. A recipe generated while
// working on one repository is about that repository, and putting it where
// every other project sees it is a surprise.
func SaveDir(dirs []string) (string, error) {
	if len(dirs) == 0 {
		return "", os.ErrNotExist
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d, nil
		}
	}
	if err := os.MkdirAll(dirs[0], 0o755); err != nil {
		return "", err
	}
	return dirs[0], nil
}
