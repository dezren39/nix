package secrets

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSetLookupUnset(t *testing.T) {
	s := NewStore()
	if _, ok := s.Lookup("TOKEN"); ok {
		t.Fatal("an empty store holds nothing")
	}
	if err := s.Set("TOKEN", "v1"); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Lookup("TOKEN"); !ok || v != "v1" {
		t.Fatalf("Lookup = %q, %v; want v1, true", v, ok)
	}
	if err := s.Set("TOKEN", "v2"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Lookup("TOKEN"); v != "v2" {
		t.Fatalf("a second Set replaces the first: got %q", v)
	}
	if !s.Unset("TOKEN") {
		t.Fatal("Unset should report the value it dropped")
	}
	if s.Unset("TOKEN") {
		t.Fatal("Unset of a name with no value reports false")
	}
	if _, ok := s.Lookup("TOKEN"); ok {
		t.Fatal("a dropped value is gone")
	}
}

func TestStoreRefusesAnEmptyValue(t *testing.T) {
	s := NewStore()
	if err := s.Set("TOKEN", ""); err == nil {
		t.Fatal("an empty value must be refused: it would read as absent")
	}
	if _, ok := s.Lookup("TOKEN"); ok {
		t.Fatal("a refused value is not stored")
	}
}

func TestStoreNamesAreSortedAndHoldNoValues(t *testing.T) {
	s := NewStore()
	const secret = "s3cr3t-value-that-must-not-leak"
	_ = s.Set("ZED", secret)
	_ = s.Set("ALPHA", "x")
	got := strings.Join(s.Names(), ",")
	if got != "ALPHA,ZED" {
		t.Fatalf("Names = %q; want sorted names", got)
	}
	// A store printed with %v or %+v shows its names at most, never a value.
	for _, form := range []string{"%v", "%+v", "%#v"} {
		if out := fmt.Sprintf(form, s); strings.Contains(out, secret) {
			t.Errorf("%s of a store leaked a value: %s", form, out)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"TOKEN", "_x", "GITHUB_TOKEN_2", "a"} {
		if !ValidName(ok) {
			t.Errorf("%q should be a valid name", ok)
		}
	}
	for _, bad := range []string{"", "1X", "has space", "dash-name", "a=b", "tab\t", "x.y"} {
		if ValidName(bad) {
			t.Errorf("%q should not be a valid name", bad)
		}
	}
}

// TestThePackageWritesNothingToDisk is the guard on the promise in the package
// comment. A value that reached a file would survive a restart and outlive the
// decision to forget it, so the source is checked for every call that creates,
// writes, renames or opens a file.
func TestThePackageWritesNothingToDisk(t *testing.T) {
	forbidden := map[string]bool{
		"Create": true, "WriteFile": true, "OpenFile": true, "Rename": true,
		"Mkdir": true, "MkdirAll": true, "Remove": true, "RemoveAll": true,
		"Symlink": true, "Link": true,
	}
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" && forbidden[sel.Sel.Name] {
				t.Errorf("%s: os.%s would write to disk; secrets live in memory only", fset.Position(call.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no source files were checked")
	}
}
