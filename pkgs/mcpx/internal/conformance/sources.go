package conformance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ModuleRoot finds the directory holding go.mod, walking up from dir.
func ModuleRoot(dir string) (string, error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		p := filepath.Dir(d)
		if p == d {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		d = p
	}
}

// testFunc is what the sources say about one test function.
type testFunc struct {
	// literals are every string literal in the function's file: subtest
	// names are often spelled in a table outside the function.
	literals map[string]bool
	// gapSkipped are subtest-name literals whose t.Run body skips with
	// "gap:", and "" when the function itself does.
	gapSkipped map[string]bool
}

// Sources indexes the test functions of packages under internal/.
type Sources struct {
	root string
	mu   sync.Mutex
	pkgs map[string]map[string]*testFunc
}

func NewSources(root string) *Sources {
	return &Sources{root: root, pkgs: map[string]map[string]*testFunc{}}
}

func (s *Sources) pkg(name string) (map[string]*testFunc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.pkgs[name]; ok {
		return p, nil
	}
	dir := filepath.Join(s.root, "internal", filepath.FromSlash(name))
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no test files in internal/%s", name)
	}
	out := map[string]*testFunc{}
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			return nil, err
		}
		lits := map[string]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if v, err := strconv.Unquote(bl.Value); err == nil {
					lits[v] = true
				}
			}
			return true
		})
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || !strings.HasPrefix(fd.Name.Name, "Test") || fd.Body == nil {
				continue
			}
			out[fd.Name.Name] = &testFunc{literals: lits, gapSkipped: gapSkips(fd.Body)}
		}
	}
	s.pkgs[name] = out
	return out, nil
}

func isGapSkip(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Skip" && sel.Sel.Name != "Skipf") || len(call.Args) == 0 {
		return false
	}
	bl, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		return false
	}
	v, _ := strconv.Unquote(bl.Value)
	return strings.HasPrefix(v, "gap:")
}

// gapSkips finds the subtests (by name literal) whose bodies skip as a gap.
func gapSkips(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	// A gap skip directly in the function body skips everything in it.
	for _, st := range body.List {
		if es, ok := st.(*ast.ExprStmt); ok && isGapSkip(es.X) {
			out[""] = true
		}
	}
	// Any call that takes a name and a func literal -- t.Run, forReq -- and
	// whose func skips as a gap marks the names it was given.
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		fl, ok := call.Args[len(call.Args)-1].(*ast.FuncLit)
		if !ok {
			return true
		}
		skipped := false
		for _, st := range fl.Body.List {
			if es, ok := st.(*ast.ExprStmt); ok && isGapSkip(es.X) {
				skipped = true
			}
		}
		if !skipped {
			return true
		}
		for _, a := range call.Args[:len(call.Args)-1] {
			ast.Inspect(a, func(m ast.Node) bool {
				if bl, ok := m.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					v, _ := strconv.Unquote(bl.Value)
					out[v] = true
				}
				return true
			})
		}
		return true
	})
	return out
}

// Check verifies a reference names a test that exists and is not a skipped
// gap. The test's source file must spell each subtest name as a string
// literal, give or take a leading "<rev>" (names built as rev+"/area/id")
// and a computed tail after a literal ending in "/", "-" or "=".
func (s *Sources) Check(r Ref) error {
	p, err := s.pkg(r.Pkg)
	if err != nil {
		return err
	}
	f, ok := p[r.Func]
	if !ok {
		return fmt.Errorf("internal/%s has no %s", r.Pkg, r.Func)
	}
	if f.gapSkipped[""] {
		return fmt.Errorf("%s.%s is skipped as a gap", r.Pkg, r.Func)
	}
	if r.Sub == "" {
		return nil
	}
	lit, ok := s.explain(f, r.Sub)
	if !ok {
		return fmt.Errorf("%s.%s: no literal in its file spells subtest %q", r.Pkg, r.Func, r.Sub)
	}
	for _, l := range lit {
		if f.gapSkipped[l] {
			return fmt.Errorf("%s.%s/%s is skipped as a gap", r.Pkg, r.Func, r.Sub)
		}
	}
	return nil
}

// explain splits sub at "/" boundaries into chunks each accounted for by a
// literal, returning the literals used.
func (s *Sources) explain(f *testFunc, sub string) ([]string, bool) {
	var cuts []int
	cuts = append(cuts, 0)
	for i := 0; i < len(sub); i++ {
		if sub[i] == '/' {
			cuts = append(cuts, i+1)
		}
	}
	cuts = append(cuts, len(sub)+1)
	var walk func(k int) ([]string, bool)
	walk = func(k int) ([]string, bool) {
		if cuts[k] == len(sub)+1 {
			return nil, true
		}
		for j := k + 1; j < len(cuts); j++ {
			chunk := sub[cuts[k] : cuts[j]-1]
			if l, ok := match(f.literals, chunk, k == 0); ok {
				if rest, ok := walk(j); ok {
					return append([]string{l}, rest...), true
				}
			}
		}
		return nil, false
	}
	return walk(0)
}

func match(lits map[string]bool, chunk string, first bool) (string, bool) {
	cands := []string{chunk}
	if first {
		if rev, rest, ok := strings.Cut(chunk, "/"); ok && isRev(rev) {
			cands = append(cands, rest, "/"+rest)
			// forReq's "<rev>/<area>/<id>": the id is the literal; the
			// area comes from the catalogue.
			if _, id, ok := strings.Cut(rest, "/"); ok && !strings.Contains(id, "/") {
				if lits[id] {
					return id, true
				}
			}
		}
	}
	for _, c := range cands {
		if lits[c] {
			return c, true
		}
	}
	// A name with a computed tail: the literal is the fixed head.
	for _, c := range cands {
		for i := len(c) - 1; i > 0; i-- {
			switch c[i-1] {
			case '/', '-', '=', '_':
				if lits[c[:i]] && i >= 8 {
					return c[:i], true
				}
			}
		}
	}
	return "", false
}
