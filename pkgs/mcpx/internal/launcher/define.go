package launcher

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Ref is one @name or @name(arg, arg) occurrence in a template.
type Ref struct {
	Name Placeholder
	// Args are the comma-separated arguments, trimmed. Nil when the
	// occurrence had no parentheses at all, which is different from an empty
	// list: @console means "whatever the default is", @console() means
	// "explicitly nothing".
	Args []string
	// HasArgs distinguishes those two cases.
	HasArgs bool
	// Width is how many bytes the occurrence occupied.
	Width int
}

// readRef parses an @name or @name(a, b) at the start of s.
//
// Arguments turn a placeholder from a slot into a small instruction.
// @console(header, prefix) says which fragments this console setup should be
// able to see, which is the difference between a template that can be
// rearranged and one that can only be filled in.
func readRef(s string) (Ref, bool) {
	if len(s) < 2 || s[0] != '@' {
		return Ref{}, false
	}
	i := 1
	// A purely numeric name is allowed, because positional arguments are
	// addressed as @1 and @2 inside a body. It is safe despite looking
	// greedy: a reference only substitutes when the fill actually holds that
	// key, and numeric keys exist only inside a body that was given
	// arguments. An @2024 in a comment elsewhere resolves to nothing and is
	// left alone.
	for i < len(s) && (isLetter(s[i]) || s[i] == '_' || isDigit(s[i])) {
		i++
	}
	if i == 1 {
		return Ref{}, false
	}
	ref := Ref{Name: Placeholder(s[1:i]), Width: i}
	if i >= len(s) || s[i] != '(' {
		return ref, true
	}
	depth := 0
	j := i
	for ; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				inner := s[i+1 : j]
				ref.HasArgs = true
				ref.Width = j + 1
				for _, part := range splitArgs(inner) {
					if part = strings.TrimSpace(part); part != "" {
						ref.Args = append(ref.Args, strings.TrimPrefix(part, "@"))
					}
				}
				return ref, true
			}
		}
	}
	// Unbalanced: treat the bare name as the reference and leave the rest
	// alone, rather than swallowing the remainder of the file.
	return Ref{Name: ref.Name, Width: i}, true
}

func splitArgs(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// Definition is a placeholder a user declared, rather than one built in.
type Definition struct {
	Name Placeholder
	// Body is the source it expands to.
	Body string
	// Params are the names its arguments bind to, usable inside Body as
	// @param.
	Params []string
	// From is the file it was declared in.
	From string
}

// Declarations recognised in a file.
//
// Three spellings, because the right one depends on what the file is. A
// comment works in any language and cannot affect runtime. An exported
// constant is visible to tooling. The filename is the least ceremony for a
// directory of one-line fragments.
var (
	commentDecl = regexp.MustCompile(`^\s*(?://|#|/\*)\s*@mcpx:placeholder\s+([A-Za-z_][A-Za-z0-9_]*)\s*(\(([^)]*)\))?`)
	constDecl   = regexp.MustCompile(`^\s*export\s+const\s+MCPX_PLACEHOLDER\s*=\s*["'` + "`" + `]([A-Za-z_][A-Za-z0-9_]*)["'` + "`" + `]`)
)

// LoadDefinitions reads placeholder declarations from files.
//
// A file may declare the name it provides, which is what allows a template to
// use an @name nobody built in. Without a declaration a file is still usable
// as a phase; declaring one only makes it addressable by name from a
// template.
func LoadDefinitions(paths []string) ([]Definition, error) {
	var out []Definition
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("reading placeholder from %s: %w", path, err)
		}
		body, name, params := readDeclaration(f)
		f.Close()

		if name == "" {
			// Filename fallback: report.ts declares @report. Least ceremony
			// for a directory of small fragments, and unambiguous because a
			// filename is already unique within its directory.
			base := filepath.Base(path)
			name = strings.TrimSuffix(base, filepath.Ext(base))
			if !validName(name) {
				continue
			}
		}
		out = append(out, Definition{
			Name: Placeholder(name), Body: body, Params: params, From: path,
		})
	}
	return out, nil
}

func readDeclaration(f *os.File) (body, name string, params []string) {
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if name == "" {
			if m := commentDecl.FindStringSubmatch(line); m != nil {
				name = m[1]
				if m[3] != "" {
					for _, p := range strings.Split(m[3], ",") {
						if p = strings.TrimSpace(p); p != "" {
							params = append(params, p)
						}
					}
				}
				continue // the declaration is not part of the body
			}
			if m := constDecl.FindStringSubmatch(line); m != nil {
				name = m[1]
				continue
			}
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), name, params
}

func validName(s string) bool {
	if s == "" || !(isLetter(s[0]) || s[0] == '_') {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isLetter(s[i]) && !isDigit(s[i]) && s[i] != '_' {
			return false
		}
	}
	return true
}

// Bind folds user definitions into a fill, rejecting collisions with the
// built-in names.
//
// Shadowing a built-in would be the worst kind of surprise: a template that
// reads correctly and means something else. A user who wants their own
// @entry has to pick another name.
func Bind(fill Fill, defs []Definition) (Fill, error) {
	builtin := map[Placeholder]bool{}
	for _, p := range All {
		builtin[p] = true
	}
	out := Fill{}
	for k, v := range fill {
		out[k] = v
	}
	seen := map[Placeholder]string{}
	var problems []string
	for _, d := range defs {
		if builtin[d.Name] {
			problems = append(problems, fmt.Sprintf(
				"%s declares @%s, which is built in; pick another name", d.From, d.Name))
			continue
		}
		if prev, dup := seen[d.Name]; dup {
			problems = append(problems, fmt.Sprintf(
				"@%s declared by both %s and %s", d.Name, prev, d.From))
			continue
		}
		seen[d.Name] = d.From
		out[d.Name] = d.Body
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("placeholder declarations conflict:\n  %s",
			strings.Join(problems, "\n  "))
	}
	return out, nil
}
