package conformance

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Paths inside the module.
const (
	CataloguePath = "internal/conformance/requirements.json"
	MarkdownPath  = "docs/spec/requirements.md"
	OverridesDir  = "internal/conformance/overrides"
	MatrixPath    = "docs/spec/conformance-matrix.md"
)

// Generate is the requirements.json the sources imply.
func Generate(root string) ([]byte, error) {
	reqs, err := Build(filepath.Join(root, MarkdownPath), filepath.Join(root, OverridesDir))
	if err != nil {
		return nil, err
	}
	return Encode(reqs)
}

// PkgPattern is one package's `go test -run` selection.
type PkgPattern struct{ Pkg, Pattern string }

// RunPatterns selects the top-level tests every cover names.
func RunPatterns() []PkgPattern {
	by := map[string]map[string]bool{}
	for _, c := range Covers() {
		r, err := ParseRef(c.Test)
		if err != nil {
			continue
		}
		if by[r.Pkg] == nil {
			by[r.Pkg] = map[string]bool{}
		}
		by[r.Pkg][r.Func] = true
	}
	var out []PkgPattern
	for pkg, fns := range by {
		var names []string
		for f := range fns {
			names = append(names, regexp.QuoteMeta(f))
		}
		sort.Strings(names)
		out = append(out, PkgPattern{pkg, "^(" + strings.Join(names, "|") + ")$"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pkg < out[j].Pkg })
	return out
}
