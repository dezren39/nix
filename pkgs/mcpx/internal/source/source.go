// Package source resolves a value that may be inline text, a path to a file,
// or a directory of files.
//
// Every string-shaped input mcpx accepts -- a launcher, a prefix, an error
// hook -- is more useful when it can also be a file, because the moment a
// snippet grows past one line, putting it in a command becomes unpleasant and
// putting it in JSON becomes unreadable. Making each of those settings accept
// either form costs one resolver; making the user choose up front costs them
// a rewrite the first time a snippet grows.
package source

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Kind is how a value was interpreted.
type Kind string

const (
	KindText Kind = "text"
	KindFile Kind = "file"
	KindDir  Kind = "dir"
	KindNone Kind = "none"
)

// Resolved is one input after resolution.
type Resolved struct {
	// Text is the source, whatever it came from.
	Text string
	Kind Kind
	// Files lists what was read, in the order it was concatenated. Empty for
	// inline text.
	Files []string
	// Explicit is true when the caller used @text: or @file: rather than
	// letting the probe decide.
	Explicit bool
}

// Options controls resolution. They come from the plumbing settings, so a
// decision that could reasonably go either way is not made permanently here.
type Options struct {
	// Dir is what relative paths resolve against.
	Dir string
	// AllowDir permits a directory to stand in for a source.
	AllowDir bool
	// Recursive descends into subdirectories when a directory is given.
	Recursive bool
	// Probe allows a bare argument naming an existing file to be read as
	// one. With it off, only the explicit @file: form touches the disk.
	Probe bool
	// Extensions limits which files in a directory are taken. Empty takes
	// every regular file.
	Extensions []string
}

// Prefixes that force an interpretation.
const (
	textPrefix = "@text:"
	filePrefix = "@file:"
	dirPrefix  = "@dir:"
	nonePrefix = "none"
)

// Resolve turns one value into source.
func Resolve(value string, opt Options) (Resolved, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Resolved{Kind: KindNone}, nil
	}
	if trimmed == nonePrefix {
		return Resolved{Kind: KindNone, Explicit: true}, nil
	}

	switch {
	case strings.HasPrefix(trimmed, textPrefix):
		return Resolved{Text: strings.TrimPrefix(trimmed, textPrefix), Kind: KindText, Explicit: true}, nil
	case strings.HasPrefix(trimmed, filePrefix):
		path := abs(strings.TrimSpace(strings.TrimPrefix(trimmed, filePrefix)), opt.Dir)
		text, err := readFile(path)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Text: text, Kind: KindFile, Files: []string{path}, Explicit: true}, nil
	case strings.HasPrefix(trimmed, dirPrefix):
		path := abs(strings.TrimSpace(strings.TrimPrefix(trimmed, dirPrefix)), opt.Dir)
		return readDir(path, opt, true)
	}

	// No explicit marker: probe. A value that names something on disk is
	// almost certainly meant to be it -- nobody writes a one-line script
	// that happens to equal a path in their working directory -- and the
	// explicit forms exist for the case where somebody does.
	if opt.Probe {
		path := abs(trimmed, opt.Dir)
		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
				return readDir(path, opt, false)
			}
			text, err := readFile(path)
			if err != nil {
				return Resolved{}, err
			}
			return Resolved{Text: text, Kind: KindFile, Files: []string{path}}, nil
		}
	}
	return Resolved{Text: value, Kind: KindText}, nil
}

func abs(path, dir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return filepath.Join(dir, path)
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(b), nil
}

func readDir(dir string, opt Options, explicit bool) (Resolved, error) {
	if !opt.AllowDir {
		return Resolved{}, fmt.Errorf("%s is a directory, and this setting does not take one; "+
			"name a file, or set plumbing.sourceDirAllowed", dir)
	}
	var files []string
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == dir || opt.Recursive {
				return nil
			}
			return fs.SkipDir
		}
		if !wanted(path, opt.Extensions) {
			return nil
		}
		files = append(files, path)
		return nil
	}
	if err := filepath.WalkDir(dir, walk); err != nil {
		return Resolved{}, fmt.Errorf("reading %s: %w", dir, err)
	}
	if len(files) == 0 {
		return Resolved{}, fmt.Errorf("%s holds no files to read", dir)
	}
	sortNatural(files)

	var b strings.Builder
	for i, f := range files {
		text, err := readFile(f)
		if err != nil {
			return Resolved{}, err
		}
		if i > 0 {
			b.WriteString("\n")
		}
		// A comment naming the file, because a stack trace into a
		// concatenation is otherwise unattributable.
		fmt.Fprintf(&b, "// --- %s ---\n", f)
		b.WriteString(strings.TrimRight(text, "\n"))
		b.WriteString("\n")
	}
	return Resolved{Text: b.String(), Kind: KindDir, Files: files, Explicit: explicit}, nil
}

func wanted(path string, exts []string) bool {
	if len(exts) == 0 {
		return true
	}
	got := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if got == strings.ToLower(e) {
			return true
		}
	}
	return false
}

// sortNatural orders files the way a person numbering them expects.
//
// Byte order puts 10 before 9, which is wrong for exactly the case
// directory concatenation exists to serve: a set of fragments numbered to
// control their order. Comparing digit runs numerically fixes it, and costs
// nothing anywhere else.
func sortNatural(files []string) {
	sort.Slice(files, func(i, j int) bool { return naturalLess(files[i], files[j]) })
}

func naturalLess(a, b string) bool {
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		ac, bc := a[ai], b[bi]
		if isDigit(ac) && isDigit(bc) {
			as, bs := ai, bi
			for ai < len(a) && isDigit(a[ai]) {
				ai++
			}
			for bi < len(b) && isDigit(b[bi]) {
				bi++
			}
			an, aerr := strconv.ParseInt(a[as:ai], 10, 64)
			bn, berr := strconv.ParseInt(b[bs:bi], 10, 64)
			if aerr == nil && berr == nil && an != bn {
				return an < bn
			}
			continue
		}
		la, lb := unicode.ToLower(rune(ac)), unicode.ToLower(rune(bc))
		if la != lb {
			return la < lb
		}
		ai++
		bi++
	}
	return len(a)-ai < len(b)-bi
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// ResolveAll resolves a list of values and joins them, in order.
func ResolveAll(values []string, opt Options) (Resolved, error) {
	var parts []string
	var files []string
	kind := KindNone
	for _, v := range values {
		r, err := Resolve(v, opt)
		if err != nil {
			return Resolved{}, err
		}
		if r.Kind == KindNone {
			continue
		}
		parts = append(parts, r.Text)
		files = append(files, r.Files...)
		if kind == KindNone {
			kind = r.Kind
		} else if kind != r.Kind {
			kind = KindText // mixed
		}
	}
	if len(parts) == 0 {
		return Resolved{Kind: KindNone}, nil
	}
	return Resolved{Text: strings.Join(parts, "\n"), Kind: kind, Files: files}, nil
}

// Conflict reports a value given in two mutually exclusive forms at one
// level.
//
// The rule the user asked for: inline text and a file path for the same
// setting, both set at the same level, cannot be ordered, so it is an error.
// Set at different levels it is ordinary precedence and no conflict at all --
// which is why this is only ever called with values from a single layer.
func Conflict(setting string, text, file string) error {
	if text != "" && file != "" {
		return fmt.Errorf("%s was given as both text and a file at the same level; "+
			"there is no order between them, so nothing to prefer -- "+
			"use one, or move one to a different layer", setting)
	}
	return nil
}
