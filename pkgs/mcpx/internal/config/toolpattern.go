package config

import (
	"fmt"
	"regexp"
	"strings"
)

// ToolPatterns is a compiled `tools` or `excludeTools` list. An entry is one
// of three things:
//
//   - a regular expression between slashes, `/^delete_/`: Go RE2 syntax,
//     unanchored like any regexp, so `/delete/` matches anywhere in the name
//   - a glob, if it contains `*`, `?` or `[`: `*` is any run of characters,
//     `?` one character, `[abc]`/`[a-z]`/`[!a-z]` a class. Anchored: the
//     whole name must match, so `delete_*` does not match `undelete_x`
//   - otherwise the exact tool name
//
// Servers name tools freely and some offer no filter of their own, so a list
// of exact names breaks the first time a server adds a `delete_` tool; a
// pattern keeps covering it.
type ToolPatterns struct {
	exact map[string]bool
	res   []*regexp.Regexp
}

// CompileToolPatterns compiles a list, failing on the first entry that is
// not a valid pattern: a deny entry that silently matches nothing is the
// failure this exists to prevent.
func CompileToolPatterns(list []string) (ToolPatterns, error) {
	var p ToolPatterns
	for _, s := range list {
		switch {
		case len(s) >= 2 && strings.HasPrefix(s, "/") && strings.HasSuffix(s, "/"):
			re, err := regexp.Compile(s[1 : len(s)-1])
			if err != nil {
				return ToolPatterns{}, fmt.Errorf("tool pattern %q: %w", s, err)
			}
			p.res = append(p.res, re)
		case strings.ContainsAny(s, "*?["):
			re, err := globRegexp(s)
			if err != nil {
				return ToolPatterns{}, fmt.Errorf("tool pattern %q: %w", s, err)
			}
			p.res = append(p.res, re)
		case s == "":
			return ToolPatterns{}, fmt.Errorf("tool pattern: empty entry")
		default:
			if p.exact == nil {
				p.exact = map[string]bool{}
			}
			p.exact[s] = true
		}
	}
	return p, nil
}

// Empty reports whether the list has no entries. An empty `tools` list
// means "no allowlist", not "allow nothing".
func (p ToolPatterns) Empty() bool { return len(p.exact) == 0 && len(p.res) == 0 }

// Match reports whether any entry matches name.
func (p ToolPatterns) Match(name string) bool {
	if p.exact[name] {
		return true
	}
	for _, re := range p.res {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// globRegexp translates a glob into an anchored regexp. path.Match is not
// used because its `*` stops at `/`, and nothing about a tool name makes `/`
// a separator.
func globRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			j := i + 1
			neg := j < len(glob) && glob[j] == '!'
			if neg {
				j++
			}
			body := j
			// As in a shell, a `]` first in the class is a literal.
			if j < len(glob) && glob[j] == ']' {
				j++
			}
			k := strings.IndexByte(glob[j:], ']')
			if k < 0 {
				return nil, fmt.Errorf("unterminated [")
			}
			b.WriteString("[")
			if neg {
				b.WriteString("^")
			}
			for _, r := range glob[body : j+k] {
				if r == '\\' || r == '[' || r == ']' {
					b.WriteByte('\\')
				}
				b.WriteRune(r)
			}
			b.WriteString("]")
			i = j + k
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
