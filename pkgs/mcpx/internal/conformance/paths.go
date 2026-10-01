package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
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

// InputGlobs are the files the matrix is computed from, relative to the
// module root: the catalogue, its overrides and the coverage registry.
var InputGlobs = []string{MarkdownPath, OverridesDir + "/*.json", "internal/conformance/coverage*.go"}

// InputDigest hashes InputGlobs' files, names and contents, in order.
func InputDigest(root string) (string, error) {
	h := sha256.New()
	for _, g := range InputGlobs {
		files, err := filepath.Glob(filepath.Join(root, g))
		if err != nil {
			return "", err
		}
		sort.Strings(files)
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return "", err
			}
			rel, _ := filepath.Rel(root, f)
			h.Write([]byte(filepath.ToSlash(rel) + "\x00"))
			h.Write(b)
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
