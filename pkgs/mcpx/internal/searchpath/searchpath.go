package searchpath

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Package searchpath turns a user's list of candidate locations into the
// ordered set of places to actually look.
//
// Three things make this more than a slice of strings. A user needs to say
// "mine first, then the usual places" without restating the usual places,
// which is what the null splice is for. An entry may reasonably name a single
// file rather than a directory, and refusing that would be arbitrary. And the
// result has to be inspectable, because "why is it not finding my script" is
// answered by showing the list that was actually searched.

// Entry is one resolved location.
type Entry struct {
	Path string
	// IsFile is true when the entry names a file rather than a directory.
	//
	// A list of directories that also accepts a file is not a special case
	// worth forbidding: someone with one script in an odd place should be
	// able to name it without inventing a directory to hold it.
	IsFile bool
	// Exists records whether it was there at resolution time.
	Exists bool
	// From is where this entry came from, for `--sources` style output.
	From string
}

// Resolved is an ordered search path.
type Resolved struct {
	Entries []Entry
	// Ambiguous lists names found in more than one form, such as a .ts and a
	// .js of the same base.
	Ambiguous []Ambiguity
}

// Ambiguity is one name that resolved more than one way.
type Ambiguity struct {
	Name  string
	Paths []string
}

func (a Ambiguity) Error() string {
	return fmt.Sprintf("%s matches %s", a.Name, strings.Join(a.Paths, " and "))
}

// Options controls resolution.
type Options struct {
	// Dir is what relative entries resolve against.
	Dir string
	// Builtin is what a null entry expands to.
	Builtin []string
	// RequireExists drops entries that are not present. Off by default,
	// because a missing directory is not an error -- it is simply empty --
	// and reporting it as one would make every fresh checkout noisy.
	RequireExists bool
}

// Resolve expands a candidate list.
func Resolve(list []string, nullMarker string, opt Options) Resolved {
	expanded := splice(list, nullMarker, opt.Builtin)

	var out Resolved
	seen := map[string]bool{}
	for _, raw := range expanded {
		path := absolute(raw, opt.Dir)
		if seen[path] {
			continue
		}
		seen[path] = true

		e := Entry{Path: path, From: raw}
		if info, err := os.Stat(path); err == nil {
			e.Exists = true
			e.IsFile = !info.IsDir()
		}
		if opt.RequireExists && !e.Exists {
			continue
		}
		out.Entries = append(out.Entries, e)
	}
	return out
}

func splice(list []string, marker string, builtin []string) []string {
	if len(list) == 0 {
		return builtin
	}
	found := false
	for _, e := range list {
		if e == marker {
			found = true
			break
		}
	}
	if !found {
		return list
	}
	out := make([]string, 0, len(list)+len(builtin))
	for _, e := range list {
		if e == marker {
			out = append(out, builtin...)
			continue
		}
		out = append(out, e)
	}
	return out
}

func absolute(path, dir string) string {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return filepath.Join(dir, path)
}

// FindOptions controls a lookup along a resolved path.
type FindOptions struct {
	// Extensions are tried in order for a name given without one.
	Extensions []string
	// AllowOverlap permits a name that matches more than one extension.
	//
	// Off, that is refused: which of foo.ts and foo.js runs is a coin flip
	// nobody should have to call, and silently preferring one means an edit
	// to the other does nothing with no indication why.
	AllowOverlap bool
}

// Find locates a name along the path, nearest first.
func (r Resolved) Find(name string, opt FindOptions) (string, *Ambiguity, error) {
	if len(opt.Extensions) == 0 {
		opt.Extensions = []string{""}
	}
	for _, e := range r.Entries {
		if !e.Exists {
			continue
		}
		if e.IsFile {
			// A file entry matches when it is the name, or when its base
			// without extension is.
			base := filepath.Base(e.Path)
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			if base == name || stem == name {
				return e.Path, nil, nil
			}
			continue
		}
		var hits []string
		for _, ext := range opt.Extensions {
			candidate := filepath.Join(e.Path, name+ext)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				hits = append(hits, candidate)
			}
		}
		if len(hits) == 0 {
			continue
		}
		if len(hits) > 1 && !opt.AllowOverlap {
			amb := &Ambiguity{Name: name, Paths: hits}
			return "", amb, fmt.Errorf(
				"%s is ambiguous: %s; which one runs is not something you should "+
					"have to guess, so pick one or set plumbing.allowTsJsOverlap",
				name, strings.Join(hits, " and "))
		}
		// With overlap permitted the first extension in the list wins, and
		// the list is ordered deliberately: .ts before .js, because a project
		// holding both is almost always compiling one into the other.
		return hits[0], nil, nil
	}
	return "", nil, nil
}

// List returns every name found along the path, nearest first, with the
// nearer occurrence of a duplicated name winning.
func (r Resolved) List(opt FindOptions) ([]string, []Ambiguity) {
	seen := map[string]string{}
	var order []string
	var ambiguous []Ambiguity

	for _, e := range r.Entries {
		if !e.Exists {
			continue
		}
		var files []string
		if e.IsFile {
			files = []string{e.Path}
		} else {
			entries, err := os.ReadDir(e.Path)
			if err != nil {
				continue
			}
			for _, de := range entries {
				if de.IsDir() {
					continue
				}
				files = append(files, filepath.Join(e.Path, de.Name()))
			}
		}
		byStem := map[string][]string{}
		for _, f := range files {
			base := filepath.Base(f)
			ext := filepath.Ext(base)
			if !wantedExt(ext, opt.Extensions) {
				continue
			}
			stem := strings.TrimSuffix(base, ext)
			byStem[stem] = append(byStem[stem], f)
		}
		stems := make([]string, 0, len(byStem))
		for stem := range byStem {
			stems = append(stems, stem)
		}
		sort.Strings(stems)

		for _, stem := range stems {
			hits := byStem[stem]
			if len(hits) > 1 && !opt.AllowOverlap {
				sort.Strings(hits)
				ambiguous = append(ambiguous, Ambiguity{Name: stem, Paths: hits})
				continue
			}
			if _, dup := seen[stem]; dup {
				continue // a nearer entry already provided it
			}
			sort.Strings(hits)
			seen[stem] = hits[0]
			order = append(order, stem)
		}
	}
	return order, ambiguous
}

func wantedExt(ext string, exts []string) bool {
	if len(exts) == 0 {
		return true
	}
	for _, e := range exts {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// Describe renders the path for diagnostics. "Why is it not finding my
// script" is answered by showing what was actually searched.
func (r Resolved) Describe() string {
	var b strings.Builder
	for _, e := range r.Entries {
		mark := "  "
		switch {
		case !e.Exists:
			mark = "- "
		case e.IsFile:
			mark = "f "
		}
		fmt.Fprintf(&b, "%s%s\n", mark, e.Path)
	}
	return b.String()
}
