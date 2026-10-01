package conformance_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/conformance"
)

// update rewrites the generated files instead of comparing them.
var update = os.Getenv("MCPX_UPDATE_MATRIX") == "1"

func root(t *testing.T) string {
	t.Helper()
	r, err := conformance.ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func golden(t *testing.T, rel string, want []byte) {
	t.Helper()
	p := filepath.Join(root(t), rel)
	if update {
		if err := os.WriteFile(p, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (regenerate with MCPX_UPDATE_MATRIX=1)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is stale; regenerate with MCPX_UPDATE_MATRIX=1 go test ./internal/conformance -run TestMatrix", rel)
	}
}

func catalogue(t *testing.T) []conformance.Requirement {
	t.Helper()
	b, err := conformance.Generate(root(t))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, conformance.CataloguePath, b)
	reqs, err := conformance.Load(filepath.Join(root(t), conformance.CataloguePath))
	if err != nil {
		t.Fatal(err)
	}
	return reqs
}

func TestMatrix(t *testing.T) {
	reqs := catalogue(t)
	for _, r := range reqs {
		for side, a := range r.Applicability {
			if !conformance.ValidApplicability(a) {
				t.Errorf("%s/%s: applicability %q", r.ID, side, a)
			}
		}
	}

	t.Run("every-reference-names-a-real-test", func(t *testing.T) {
		src := conformance.NewSources(root(t))
		byID := map[string]conformance.Requirement{}
		for _, r := range reqs {
			byID[r.ID] = r
		}
		for _, c := range conformance.Covers() {
			ref, err := conformance.ParseRef(c.Test)
			if err != nil {
				t.Errorf("%s: %v", c.ID, err)
				continue
			}
			if ref.Template {
				r := byID[c.ID]
				if len(r.Revs) == 0 {
					t.Errorf("%s: unknown requirement", c.ID)
					continue
				}
				ref.Sub = conformance.Expand(ref.Sub, r.Revs[0], r)
			}
			if err := src.Check(ref); err != nil {
				t.Errorf("%s (%s): %v", c.ID, c.Side, err)
			}
		}
		for _, g := range conformance.Gaps() {
			if g.Test == "" {
				continue
			}
			test := g.Test
			if !strings.Contains(test, "@") && !strings.Contains(test, "/") {
				test += "@all"
			}
			ref, err := conformance.ParseRef(test)
			if err != nil {
				t.Errorf("gap %s: %v", g.ID, err)
				continue
			}
			ref.Sub = "" // a gap's test is expected to be skipped
			if err := src.Check(ref); err != nil && !strings.Contains(err.Error(), "skipped as a gap") {
				t.Errorf("gap %s: %v", g.ID, err)
			}
		}
	})

	st, err := conformance.Evaluate(reqs, conformance.Covers(), conformance.Gaps())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("every-applicable-requirement-is-tested-or-a-gap", func(t *testing.T) {
		// MCPX_MATRIX_AREAS=tools,resources narrows the report to those area
		// groups, for whoever is working through them.
		only := map[string]bool{}
		for _, a := range strings.Split(os.Getenv("MCPX_MATRIX_AREAS"), ",") {
			if a != "" {
				only[a] = true
			}
		}
		area := map[string]string{}
		for _, r := range reqs {
			area[r.ID] = conformance.AreaGroup(r.Area)
		}
		var missing []string
		for k, s := range st {
			if s.Kind == "missing" && (len(only) == 0 || only[area[k.ID]]) {
				missing = append(missing, k.Rev+" "+k.Side+" "+k.ID)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%d cells apply to mcpx with neither a covering test nor a gap:\n%s",
				len(missing), strings.Join(missing, "\n"))
		}
	})

	inputs, err := conformance.InputDigest(root(t))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, conformance.MatrixPath, []byte(conformance.Render(reqs, st, inputs)))
}
