// Package buildcheck holds guards on how package.nix builds and tests mcpx.
package buildcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const moduleRoot = "../.."

// The check phase must derive its package list from the tree, so a package
// gains test coverage in the nix build the moment it gains a test file.
const derivedList = `go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...`

var excludeBlock = regexp.MustCompile(`(?s)checkExclude = \[(.*?)\];`)

func readPackageNix(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot, "package.nix"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// testedPackages returns every directory (relative to the module root) that
// holds a _test.go file, which is what the go list above selects.
func testedPackages(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(moduleRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && p != moduleRoot {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, "_test.go") {
			rel, _ := filepath.Rel(moduleRoot, filepath.Dir(p))
			out[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type exclusion struct{ pkg, reason string }

// parseExclusions reads checkExclude: each entry is a quoted package path,
// and the comment lines directly above it are its reason.
func parseExclusions(t *testing.T, nix string) []exclusion {
	t.Helper()
	m := excludeBlock.FindStringSubmatch(nix)
	if m == nil {
		t.Fatal("package.nix has no `checkExclude = [ ... ];` list")
	}
	var out []exclusion
	var reason []string
	for _, line := range strings.Split(m[1], "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			reason = append(reason, strings.TrimSpace(strings.TrimPrefix(line, "#")))
		case strings.HasPrefix(line, `"`) && strings.HasSuffix(line, `"`):
			out = append(out, exclusion{strings.Trim(line, `"`), strings.Join(reason, " ")})
			reason = nil
		default:
			t.Fatalf("checkExclude: cannot read entry %q", line)
		}
	}
	return out
}

func TestCheckPhaseTestsEveryPackageWithTests(t *testing.T) {
	nix := readPackageNix(t)
	if !strings.Contains(nix, derivedList) {
		t.Fatalf("checkPhase must compute its packages with\n  %s\nso new packages are tested by default", derivedList)
	}
	if !strings.Contains(nix, "go test $pkgs") {
		t.Fatal("checkPhase must run `go test $pkgs` on the computed list")
	}
	// A hand-written package list next to the computed one would silently
	// narrow it again.
	if regexp.MustCompile(`go test[^\n]*\./internal/`).MatchString(nix) {
		t.Fatal("checkPhase names packages by hand; exclude them via checkExclude instead")
	}

	tested := testedPackages(t)
	excluded := map[string]bool{}
	for _, ex := range parseExclusions(t, nix) {
		excluded[ex.pkg] = true
		if !tested[ex.pkg] {
			t.Errorf("checkExclude lists %q, which has no tests (or does not exist)", ex.pkg)
		}
		if len(ex.reason) < 20 {
			t.Errorf("checkExclude entry %q needs a comment above it saying what the sandbox lacks", ex.pkg)
		}
	}
	n := 0
	for p := range tested {
		if !excluded[p] {
			n++
		}
	}
	if n == 0 {
		t.Fatal("found no tested packages; the walk is broken")
	}
	t.Logf("%d packages with tests, %d excluded", len(tested), len(excluded))
}
