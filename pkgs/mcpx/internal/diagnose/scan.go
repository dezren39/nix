package diagnose

import (
	"strings"
	"unicode"
)

// Call is one `namespace.tool(...)` occurrence found in a script.
//
// Found by scanning rather than by parsing. mcpx has no TypeScript parser and
// will not grow one for this: the shape being looked for is two identifiers, a
// dot and a parenthesis, and every construct that could hide one -- a string, a
// comment, a template literal -- is skipped rather than understood. The cost of
// that choice is that a call assembled dynamically is invisible here, which is
// the correct failure: a diagnostic about a call nobody wrote is worse than no
// diagnostic at all.
type Call struct {
	// Namespace and Func are the two identifiers, as written.
	Namespace string
	Func      string
	// Args is the source between the parentheses, trimmed.
	Args string
	// Empty distinguishes `f()` from `f(x)`; it is the case the schema
	// diagnostics care about most.
	Empty bool
	// Line is 1-based. Text is that whole line, trimmed.
	Line int
	Text string
}

// Path is the `namespace.func` spelling used everywhere else.
func (c Call) Path() string { return c.Namespace + "." + c.Func }

// Calls finds every call that looks like a tool invocation.
func Calls(src string) []Call {
	var out []Call
	lineStarts := lineIndex(src)

	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '/':
			if j, ok := skipComment(src, i); ok {
				i = j - 1
				continue
			}
		case '\'', '"', '`':
			i = skipString(src, i) - 1
			continue
		}
		if !isIdentStart(src[i]) {
			continue
		}
		// Not a member access of something else: `a.b.c(` should be read as
		// b.c only when a is `tools`, because `client.issues.create()` is not
		// a namespace call and pretending it is produces nonsense.
		start := i
		first, next := readIdent(src, i)
		if next >= len(src) || src[next] != '.' {
			i = next - 1
			continue
		}
		ns, afterNS := first, next
		second, next2 := readIdent(src, next+1)
		if second == "" {
			i = next
			continue
		}
		if next2 < len(src) && src[next2] == '.' {
			// Three segments: accept only the `tools.ns.func` spelling.
			third, next3 := readIdent(src, next2+1)
			if third == "" || ns != "tools" {
				i = next2
				continue
			}
			ns, second, next2 = second, third, next3
		}
		_ = afterNS
		if next2 >= len(src) || src[next2] != '(' {
			i = next2 - 1
			continue
		}
		if precededByMemberAccess(src, start) {
			i = next2
			continue
		}
		args, _, ok := balanced(src, next2)
		if !ok {
			i = next2
			continue
		}
		line := lineOf(lineStarts, start)
		out = append(out, Call{
			Namespace: ns,
			Func:      second,
			Args:      strings.TrimSpace(args),
			Empty:     strings.TrimSpace(args) == "",
			Line:      line,
			Text:      strings.TrimSpace(lineText(src, lineStarts, line)),
		})
		// Carry on from just inside the parenthesis rather than past it. A
		// call is frequently an argument to another one --
		// console.log(await demo.echo({...})) -- and skipping the arguments
		// made every nested call invisible.
		i = next2
	}
	return out
}

// precededByMemberAccess reports whether the identifier at i is itself the
// tail of a longer chain, which means the leading name is not a namespace.
func precededByMemberAccess(src string, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch src[j] {
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

func isIdentStart(b byte) bool {
	return b == '_' || b == '$' || unicode.IsLetter(rune(b))
}

func isIdentByte(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func readIdent(src string, i int) (string, int) {
	if i >= len(src) || !isIdentStart(src[i]) {
		return "", i
	}
	j := i
	for j < len(src) && isIdentByte(src[j]) {
		j++
	}
	return src[i:j], j
}

// skipComment returns the index after a comment starting at i.
func skipComment(src string, i int) (int, bool) {
	if i+1 >= len(src) {
		return 0, false
	}
	switch src[i+1] {
	case '/':
		j := strings.IndexByte(src[i:], '\n')
		if j < 0 {
			return len(src), true
		}
		return i + j, true
	case '*':
		j := strings.Index(src[i+2:], "*/")
		if j < 0 {
			return len(src), true
		}
		return i + 2 + j + 2, true
	}
	return 0, false
}

// skipString returns the index after the string literal opening at i.
func skipString(src string, i int) int {
	quote := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		case '\n':
			if quote != '`' {
				return j
			}
		}
	}
	return len(src)
}

// balanced returns the text between the parenthesis at i and its partner.
func balanced(src string, i int) (string, int, bool) {
	depth := 0
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '\'', '"', '`':
			j = skipString(src, j) - 1
		case '/':
			if k, ok := skipComment(src, j); ok {
				j = k - 1
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return src[i+1 : j], j + 1, true
			}
		}
	}
	return "", len(src), false
}

// ObjectKeys returns the top-level keys of an object literal, and whether the
// literal was understood well enough for their absence to mean anything.
//
// A spread makes the answer unknowable without evaluating the program, so it
// is reported as not understood rather than guessed at. Diagnosing a missing
// argument that a spread supplies would be worse than staying quiet.
func ObjectKeys(args string) (keys []string, ok bool) {
	args = strings.TrimSpace(args)
	if !strings.HasPrefix(args, "{") || !strings.HasSuffix(args, "}") {
		return nil, false
	}
	inner := args[1 : len(args)-1]
	depth := 0
	start := 0
	var parts []string
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '\'', '"', '`':
			i = skipString(inner, i) - 1
		case '/':
			if k, cok := skipComment(inner, i); cok {
				i = k - 1
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, inner[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, inner[start:])
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "...") {
			return nil, false
		}
		name := p
		if c := strings.IndexByte(p, ':'); c >= 0 {
			name = strings.TrimSpace(p[:c])
		} else if c := strings.IndexByte(p, '('); c >= 0 {
			name = strings.TrimSpace(p[:c]) // a method shorthand
		}
		name = strings.Trim(name, "\"'`")
		if name == "" || strings.ContainsAny(name, "[] ") {
			// A computed key is a name only the runtime knows.
			return nil, false
		}
		keys = append(keys, name)
	}
	return keys, true
}

func lineIndex(src string) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func lineOf(starts []int, off int) int {
	lo, hi := 0, len(starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1
}

func lineText(src string, starts []int, line int) string {
	if line < 1 || line > len(starts) {
		return ""
	}
	start := starts[line-1]
	end := len(src)
	if line < len(starts) {
		end = starts[line] - 1
	}
	if end > len(src) {
		end = len(src)
	}
	return strings.TrimRight(src[start:end], "\r")
}
