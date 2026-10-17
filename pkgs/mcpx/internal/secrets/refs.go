package secrets

import (
	"regexp"
	"sort"
	"strings"
)

// References returns the namespaces a script names, sorted and without
// repeats. It finds the spellings that reach a server through the tools
// object, tools.<ns>.<tool> and tools["<ns>"]["<tool>"], and the bare spelling
// <ns>.<tool> for a namespace configured here, which mcpx binds as a global in
// every script it runs.
//
// The scan is static and nothing is evaluated. Strings and comments are
// skipped, and the inside of a template literal's ${...} is read, because a
// call there is still a call. A name the script declares itself is not a
// namespace, so a local called like one is not counted. A reference assembled
// at run time, or a bare use of the tools object, is invisible here; the check
// made when the call happens is what catches those.
func References(src string, configured func(string) bool) []string {
	s := &refScan{
		src:        src,
		configured: configured,
		declared:   declaredNames(src),
		found:      map[string]bool{},
	}
	s.code(0, false)
	out := make([]string, 0, len(s.found))
	for ns := range s.found {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

type refScan struct {
	src        string
	configured func(string) bool
	declared   map[string]bool
	found      map[string]bool
}

// code scans program text from i. With untilBrace it stops at the } that
// closes a ${...} and returns the index after it; otherwise it scans to the
// end of the source.
func (s *refScan) code(i int, untilBrace bool) int {
	depth := 0
	for i < len(s.src) {
		c := s.src[i]
		switch {
		case c == '/' && i+1 < len(s.src) && s.src[i+1] == '/':
			i = s.lineEnd(i)
		case c == '/' && i+1 < len(s.src) && s.src[i+1] == '*':
			i = s.blockEnd(i)
		case c == '\'' || c == '"':
			i = s.quoteEnd(i)
		case c == '`':
			i = s.template(i)
		case c == '{':
			depth++
			i++
		case c == '}':
			if untilBrace && depth == 0 {
				return i + 1
			}
			depth--
			i++
		case isIdentStart(c):
			name, next := s.ident(i)
			if !s.afterDot(i) {
				s.reference(name, next)
			}
			i = next
		default:
			i++
		}
	}
	return i
}

// reference looks at what follows an identifier that is not a member of
// something else.
func (s *refScan) reference(name string, next int) {
	k := s.skipSpace(next)
	if name == "tools" {
		if k < len(s.src) && s.src[k] == '.' {
			if ns, _ := s.ident(s.skipSpace(k + 1)); ns != "" {
				s.found[ns] = true
			}
			return
		}
		if k < len(s.src) && s.src[k] == '[' {
			j := s.skipSpace(k + 1)
			if j < len(s.src) && (s.src[j] == '"' || s.src[j] == '\'') {
				if ns, ok := s.literal(j); ok {
					s.found[ns] = true
				}
			}
		}
		return
	}
	if s.configured(name) && !s.declared[name] && k < len(s.src) && s.src[k] == '.' {
		s.found[name] = true
	}
}

// afterDot reports whether the identifier at i is the tail of a longer chain,
// such as the fs in a.tools.fs or x?.fs.
func (s *refScan) afterDot(i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch s.src[j] {
		case ' ', '\t', '\n', '\r':
			continue
		case '.':
			return true
		default:
			return false
		}
	}
	return false
}

func (s *refScan) ident(i int) (string, int) {
	if i >= len(s.src) || !isIdentStart(s.src[i]) {
		return "", i
	}
	j := i
	for j < len(s.src) && isIdentByte(s.src[j]) {
		j++
	}
	return s.src[i:j], j
}

func (s *refScan) skipSpace(i int) int {
	for i < len(s.src) && (s.src[i] == ' ' || s.src[i] == '\t' || s.src[i] == '\n' || s.src[i] == '\r') {
		i++
	}
	return i
}

// literal reads a plain string literal opening at i. An escape makes it not a
// name this scan can be sure of, so it is refused rather than guessed at.
func (s *refScan) literal(i int) (string, bool) {
	q := s.src[i]
	for j := i + 1; j < len(s.src); j++ {
		switch s.src[j] {
		case '\\', '\n':
			return "", false
		case q:
			return s.src[i+1 : j], s.src[i+1:j] != ""
		}
	}
	return "", false
}

func (s *refScan) lineEnd(i int) int {
	if j := strings.IndexByte(s.src[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(s.src)
}

func (s *refScan) blockEnd(i int) int {
	if j := strings.Index(s.src[i+2:], "*/"); j >= 0 {
		return i + 2 + j + 2
	}
	return len(s.src)
}

func (s *refScan) quoteEnd(i int) int {
	q := s.src[i]
	for j := i + 1; j < len(s.src); j++ {
		switch s.src[j] {
		case '\\':
			j++
		case q:
			return j + 1
		case '\n':
			// An unterminated quote ends at its line, as the language does.
			return j
		}
	}
	return len(s.src)
}

// template skips a template literal, reading the program inside each ${...}.
func (s *refScan) template(i int) int {
	for j := i + 1; j < len(s.src); {
		switch s.src[j] {
		case '\\':
			j += 2
		case '`':
			return j + 1
		case '$':
			if j+1 < len(s.src) && s.src[j+1] == '{' {
				j = s.code(j+2, true)
				continue
			}
			j++
		default:
			j++
		}
	}
	return len(s.src)
}

var (
	declRe        = regexp.MustCompile(`\b(?:const|let|var|function|class|import)\s+([A-Za-z_$][\w$]*)`)
	importNamedRe = regexp.MustCompile(`import\s*\{([^}]*)\}`)
	importStarRe  = regexp.MustCompile(`import\s*\*\s*as\s+([A-Za-z_$][\w$]*)`)
)

// declaredNames are the names the source binds for itself. A bare use of one
// of them is a local, whatever namespace it happens to share a name with.
func declaredNames(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range declRe.FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	for _, m := range importStarRe.FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	for _, m := range importNamedRe.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			part = strings.TrimSpace(part)
			if i := strings.LastIndex(part, " as "); i >= 0 {
				part = part[i+len(" as "):]
			}
			if part != "" {
				out[strings.TrimSpace(part)] = true
			}
		}
	}
	return out
}

func isIdentStart(b byte) bool {
	return b == '_' || b == '$' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

func isIdentByte(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}
