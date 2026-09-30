package api_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
)

// testFuncs is every Test function in the module, found by parsing rather
// than grepping, so a name in a comment or a string does not count.
func testFuncs(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				out[fn.Name.Name] = path
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("found no tests at all; the walk is looking in the wrong place")
	}
	return out
}

// TestEveryEnforcedBoundaryNamesItsRefusal: "enforced" is a claim that
// something refuses, and the only evidence is a test that tried. A boundary
// that names none, or names one that does not exist -- renamed, deleted --
// is a claim with nothing behind it.
func TestEveryEnforcedBoundaryNamesItsRefusal(t *testing.T) {
	tests := testFuncs(t)
	for _, b := range api.Boundaries() {
		switch b.State {
		case api.Enforced:
			if len(b.Refusals) == 0 {
				t.Errorf("%s is enforced and names no test that tries to get round it", b.Name)
			}
			for _, name := range b.Refusals {
				if _, ok := tests[name]; !ok {
					t.Errorf("%s names %s as its refusal, and there is no such test", b.Name, name)
				}
			}
			if b.Until != "" {
				t.Errorf("%s is enforced and still says it waits for %s", b.Name, b.Until)
			}
		case api.Advisory:
			if !regexp.MustCompile(`^#\d+$`).MatchString(b.Until) {
				t.Errorf("%s is advisory and does not name the issue that will enforce it (Until %q)", b.Name, b.Until)
			}
			if len(b.Refusals) > 0 {
				t.Errorf("%s is advisory but names refusal tests; if something refuses, it is enforced", b.Name)
			}
		default:
			t.Errorf("%s is %q; a restriction is enforced or advisory, with no third state", b.Name, b.State)
		}
		if strings.TrimSpace(b.Declares) == "" {
			t.Errorf("%s declares nothing", b.Name)
		}
	}
}
