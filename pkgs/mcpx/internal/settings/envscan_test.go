package settings_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The scanners behind the environment guards.
//
// They walk syntax rather than grep text, because the regex scans this
// replaces could pass while blind: a guard that matched `5 * time.Minute`
// missed every `5*time.Minute` gofmt wrote (#56), and the old
// os\.Getenv\("MCPX_..."\) pattern could not see a read through a constant, a
// wrapper function or an index into an environment map. Every way a Go
// program can name an MCPX_ variable is one of four shapes -- a call argument,
// a map key, an index, or a KEY=VALUE string -- and each is recognised here
// whatever the spacing, including through a named constant.

var (
	envNameRE = regexp.MustCompile(`^MCPX_[A-Z0-9_]+$`)
	envPairRE = regexp.MustCompile(`^(MCPX_[A-Z0-9_]+)=`)
	// templateReadRE finds a variable read by TypeScript that mcpx generates
	// from a Go string: the client's env() helper, Deno.env.get, and
	// process.env in both spellings.
	templateReadRE = regexp.MustCompile(
		`\benv\(\s*"(MCPX_[A-Z0-9_]+)"\s*\)` +
			`|\.env\.get\(\s*"(MCPX_[A-Z0-9_]+)"\s*\)` +
			`|process\.env\.(MCPX_[A-Z0-9_]+)` +
			`|process\.env\[\s*"(MCPX_[A-Z0-9_]+)"\s*\]`)
	// templateSetRE finds generated TypeScript writing a variable for a
	// process it starts in turn.
	templateSetRE = regexp.MustCompile(
		`\.env\.set\(\s*"(MCPX_[A-Z0-9_]+)"` +
			`|process\.env\.(MCPX_[A-Z0-9_]+)\s*=[^=]` +
			`|process\.env\[\s*"(MCPX_[A-Z0-9_]+)"\s*\]\s*=[^=]`)
)

type envSite struct {
	name string
	file string // relative to the module root
	line int
	// how is the shape that was recognised, for the failure message.
	how string
}

func (s envSite) at() string { return s.file + ":" + strconv.Itoa(s.line) }

type goEnvScan struct {
	sets []envSite
	// reads are every place a name is looked up, by call or by index.
	reads []envSite
	// dynamic are lookups whose name is built at run time from an MCPX_
	// prefix, which no table can be checked against.
	dynamic []envSite
	// template are reads by generated TypeScript held in Go strings.
	template []envSite
}

// readCallee reports whether a call looks something up in the environment
// by the name it is given. Anything else that takes a bare MCPX_ name as an
// argument is treated as a read as well -- that is what a wrapper around
// os.Getenv looks like from the call site.
var envLookups = map[string]bool{
	"os.Getenv": true, "os.LookupEnv": true, "syscall.Getenv": true,
}

var envWrites = map[string]bool{
	"os.Setenv": true, "syscall.Setenv": true,
}

// scanGoEnv visits every non-test Go file in the module.
func scanGoEnv(t *testing.T, root string) goEnvScan {
	t.Helper()
	type file struct {
		rel string
		dir string
		f   *ast.File
	}
	fset := token.NewFileSet()
	var files []file
	// Package-level string constants by directory, so that
	// os.Getenv(sessionVar) is resolved when sessionVar is declared in a
	// sibling file.
	consts := map[string]map[string]string{}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			// vendor/ exists in the nix build (buildGoModule copies the
			// vendored modules into the source), and parsing it is minutes
			// of work on third-party code that reads no MCPX_ variable.
			case ".git", "node_modules", "testdata", "vendor":
				return fs.SkipDir
			// Harnesses that run the mcpx binary rather than being part of
			// it. They set MCPX_* for a daemon under test, which is the
			// e2e harness's job done from a main package instead of a
			// _test.go file -- the opposite direction from the contract,
			// which is about what mcpx sets for children it starts.
			case "conformance", "testsupport":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		dir := filepath.Dir(rel)
		files = append(files, file{rel: rel, dir: dir, f: f})
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if s, ok := stringLit(vs.Values[i]); ok {
						if consts[dir] == nil {
							consts[dir] = map[string]string{}
						}
						consts[dir][n.Name] = s
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var out goEnvScan
	for _, fl := range files {
		value := func(e ast.Expr) (string, bool) { return constValue(consts[fl.dir], e) }
		site := func(name string, n ast.Node, how string) envSite {
			return envSite{name: name, file: fl.rel, line: fset.Position(n.Pos()).Line, how: how}
		}
		written := map[*ast.IndexExpr]bool{}

		ast.Inspect(fl.f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				// env["MCPX_CWD"] = cwd
				for _, lhs := range x.Lhs {
					ie, ok := lhs.(*ast.IndexExpr)
					if !ok {
						continue
					}
					written[ie] = true
					if s, ok := value(ie.Index); ok && envNameRE.MatchString(s) {
						out.sets = append(out.sets, site(s, ie, "index assignment"))
					}
				}
			case *ast.IndexExpr:
				if written[x] {
					return true
				}
				if s, ok := value(x.Index); ok && envNameRE.MatchString(s) {
					out.reads = append(out.reads, site(s, x, "index"))
				}
			case *ast.CompositeLit:
				// map[string]string{"MCPX_RUN": id}
				for _, elt := range x.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if s, ok := value(kv.Key); ok && envNameRE.MatchString(s) {
						out.sets = append(out.sets, site(s, kv, "map key"))
					}
				}
			case *ast.CallExpr:
				callee := calleeName(x.Fun)
				for i, arg := range x.Args {
					s, ok := value(arg)
					if !ok {
						if envLookups[callee] && i == 0 && mentionsPrefix(arg) {
							out.dynamic = append(out.dynamic, site("MCPX_?", x, callee))
						}
						continue
					}
					if !envNameRE.MatchString(s) {
						continue
					}
					if envWrites[callee] && i == 0 {
						out.sets = append(out.sets, site(s, x, callee))
						continue
					}
					if callee == "os.Unsetenv" {
						continue
					}
					out.reads = append(out.reads, site(s, x, "call to "+callee))
				}
			case *ast.BasicLit:
				s, ok := stringLit(x)
				if !ok {
					return true
				}
				// "MCPX_RUNTIME="+rt.Name, "MCPX_ALLOW_READ=1"
				if m := envPairRE.FindStringSubmatch(s); m != nil {
					out.sets = append(out.sets, site(m[1], x, "KEY=VALUE string"))
				}
				for _, m := range templateSetRE.FindAllStringSubmatch(s, -1) {
					for _, g := range m[1:] {
						if g != "" {
							out.sets = append(out.sets, site(g, x, "generated TypeScript"))
						}
					}
				}
				for _, m := range templateReadRE.FindAllStringSubmatchIndex(s, -1) {
					for g := 1; g < len(m)/2; g++ {
						if m[2*g] < 0 {
							continue
						}
						name := s[m[2*g]:m[2*g+1]]
						line := fset.Position(x.Pos()).Line + strings.Count(s[:m[0]], "\n")
						out.template = append(out.template,
							envSite{name: name, file: fl.rel, line: line, how: "generated TypeScript"})
					}
				}
			}
			return true
		})
	}
	return out
}

func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}

// constValue resolves a literal, a named string constant, or a
// concatenation of those.
func constValue(pkgConsts map[string]string, e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		return stringLit(x)
	case *ast.ParenExpr:
		return constValue(pkgConsts, x.X)
	case *ast.Ident:
		if x.Obj != nil && x.Obj.Kind == ast.Con {
			if vs, ok := x.Obj.Decl.(*ast.ValueSpec); ok {
				for i, n := range vs.Names {
					if n.Name == x.Name && i < len(vs.Values) {
						return constValue(pkgConsts, vs.Values[i])
					}
				}
			}
		}
		s, ok := pkgConsts[x.Name]
		return s, ok
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := constValue(pkgConsts, x.X)
		r, rok := constValue(pkgConsts, x.Y)
		return l + r, lok && rok
	}
	return "", false
}

// mentionsPrefix reports whether an expression the scanner could not
// resolve still spells out an MCPX_ prefix -- os.Getenv("MCPX_" + name).
func mentionsPrefix(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if s, ok := stringLit(asExpr(n)); ok && strings.HasPrefix(s, "MCPX_") {
			found = true
		}
		return !found
	})
	return found
}

func asExpr(n ast.Node) ast.Expr {
	e, _ := n.(ast.Expr)
	return e
}

func calleeName(fun ast.Expr) string {
	switch x := fun.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name + "." + x.Sel.Name
		}
		return x.Sel.Name
	}
	return ""
}

// ---- the plugin ----

var (
	// A quoted name on its own is a key the plugin puts into a command's
	// environment -- put(e, "MCPX_SESSION_ID", ...) -- unless it indexes
	// something, in which case it is a lookup.
	tsQuotedRE = regexp.MustCompile("\"(MCPX_[A-Z0-9_]+)\"|'(MCPX_[A-Z0-9_]+)'|`(MCPX_[A-Z0-9_]+)`")
	// env.MCPX_FOO, process.env.MCPX_FOO, opts.env?.MCPX_FOO.
	tsPropertyReadRE = regexp.MustCompile(`\benv\??\.(MCPX_[A-Z0-9_]+)`)
)

// scanPluginEnv reads the plugin's TypeScript sources, not its tests or its
// README: a test can assert a variable nothing sets, and a README can
// describe one nothing reads.
func scanPluginEnv(t *testing.T, root string) (sets, reads []envSite) {
	t.Helper()
	dir := filepath.Join(root, "plugin")
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !(strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx")) ||
			strings.HasSuffix(name, ".test.ts") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		for i, line := range strings.Split(string(b), "\n") {
			// Comment lines name variables in prose -- `MCPX_PLUGIN_BIN_ARGS`
			// in a doc comment is not a key being set.
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") ||
				strings.HasPrefix(trimmed, "/*") {
				continue
			}
			for _, m := range tsPropertyReadRE.FindAllStringSubmatch(line, -1) {
				reads = append(reads, envSite{name: m[1], file: rel, line: i + 1, how: "property"})
			}
			for _, m := range tsQuotedRE.FindAllStringSubmatchIndex(line, -1) {
				var n string
				for g := 1; g <= 3; g++ {
					if m[2*g] >= 0 {
						n = line[m[2*g]:m[2*g+1]]
					}
				}
				before := strings.TrimRight(line[:m[0]], " \t")
				if strings.HasSuffix(before, "[") {
					reads = append(reads, envSite{name: n, file: rel, line: i + 1, how: "index"})
					continue
				}
				sets = append(sets, envSite{name: n, file: rel, line: i + 1, how: "quoted key"})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading the plugin sources: %v", err)
	}
	return sets, reads
}
