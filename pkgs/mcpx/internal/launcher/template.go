// Package launcher builds the shim that wraps a script.
//
// The generated launcher is a template with named holes. Most of the time the
// built-in template is what runs and the holes are filled with the configured
// phases. When that is not enough, the whole template can be replaced, because
// a launcher that is only half-configurable invites someone to copy it out and
// maintain a fork -- and a fork stops getting fixes.
package launcher

import (
	"fmt"
	"sort"
	"strings"
)

// Placeholder is a name that may appear in a launcher template as @name.
type Placeholder string

const (
	// Entry is the script itself: the import and the call. A template
	// without it does not run anything, which is legal but almost never
	// meant, so it is reported.
	Entry Placeholder = "entry"
	// Globals installs log, emit, tools and the rest onto globalThis.
	Globals Placeholder = "globals"
	// Console patches the console, subject to script.captureConsole.
	Console Placeholder = "console"
	// Import is the dynamic import on its own, for a template that wants to
	// call the entry point itself.
	Import Placeholder = "import"
	// Header is the generated preamble: the client import and the script
	// and result objects every phase can see.
	Header Placeholder = "header"

	Before    Placeholder = "before"
	Prefix    Placeholder = "prefix"
	OnSuccess Placeholder = "onSuccess"
	OnError   Placeholder = "onError"
	Suffix    Placeholder = "suffix"
)

// All is every placeholder a template may use.
var All = []Placeholder{
	Header, Globals, Console, Before, Prefix, Import, Entry, OnSuccess, OnError, Suffix,
}

// Template is a launcher source with holes.
type Template struct {
	// Text is the template source.
	Text string
	// Name is where it came from, for error messages.
	Name string
}

// Fill is the content for each placeholder.
type Fill map[Placeholder]string

// Options controls expansion.
type Options struct {
	// AllowRepeat names placeholders that may resolve more than once.
	//
	// A placeholder used twice is normally a mistake -- the same hook firing
	// on two paths, a fragment pasted in the wrong place -- and a mistake
	// that silently doubles an effect is expensive to find. Naming one here
	// says the repetition is deliberate.
	AllowRepeat []Placeholder
	// MaxDepth bounds substitution when fills themselves contain
	// placeholders.
	MaxDepth int
	// Defs are user-declared placeholders, which may take parameters.
	Defs []Definition
}

// Expand substitutes every @name in the template.
//
// Fills may themselves contain placeholders, which is what makes the
// interesting rearrangements possible: a suffix that ends with @prefix runs
// the prefix again on the way out. That also makes cycles possible, so the
// reference graph is checked before anything is substituted.
func Expand(t Template, fill Fill, opt Options) (string, error) {
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 16
	}
	allowed := map[Placeholder]bool{}
	for _, p := range opt.AllowRepeat {
		allowed[p] = true
	}

	if err := checkAcyclic(t, fill); err != nil {
		return "", err
	}
	if err := checkUnknown(t, fill); err != nil {
		return "", err
	}

	counts := map[Placeholder]int{}
	out, err := expand(t.Text, fill, opt, counts, 0)
	if err != nil {
		return "", err
	}

	var repeated []string
	for p, n := range counts {
		if n > 1 && !allowed[p] {
			repeated = append(repeated, fmt.Sprintf("@%s used %d times", p, n))
		}
	}
	if len(repeated) > 0 {
		sort.Strings(repeated)
		return "", fmt.Errorf("%s: %s; if that is deliberate, name it in "+
			"plumbing.launcherPlaceholderRepeat", t.Name, strings.Join(repeated, ", "))
	}
	return out, nil
}

func expand(text string, fill Fill, opt Options, counts map[Placeholder]int, depth int) (string, error) {
	if depth > opt.MaxDepth {
		return "", fmt.Errorf("placeholder substitution went deeper than %d levels", opt.MaxDepth)
	}
	var b strings.Builder
	i := 0
	for i < len(text) {
		j := strings.IndexByte(text[i:], '@')
		if j < 0 {
			b.WriteString(text[i:])
			break
		}
		j += i
		b.WriteString(text[i:j])
		ref, ok := readRef(text[j:])
		if !ok {
			b.WriteByte('@')
			i = j + 1
			continue
		}
		body, known := fill[ref.Name]
		if !known {
			b.WriteString(text[j : j+ref.Width])
			i = j + ref.Width
			continue
		}
		counts[ref.Name]++

		// Arguments bind inside the body only. A local fill shadows the
		// outer one for the parameter names, so @console(header, prefix)
		// reaches exactly those fragments and nothing leaks back out.
		inner := fill
		if ref.HasArgs {
			inner = withArgs(fill, ref, opt.Defs)
		}
		sub, err := expand(body, inner, opt, counts, depth+1)
		if err != nil {
			return "", err
		}
		b.WriteString(sub)
		i = j + ref.Width
	}
	return b.String(), nil
}

// withArgs layers a definition's parameters over the outer fill.
//
// A declaration that named its parameters binds them positionally. One that
// did not still gets @1, @2 and @args, because a fragment used with arguments
// is usually short enough that naming them is ceremony.
func withArgs(outer Fill, ref Ref, defs []Definition) Fill {
	inner := Fill{}
	for k, v := range outer {
		inner[k] = v
	}
	var params []string
	for _, d := range defs {
		if d.Name == ref.Name {
			params = d.Params
			break
		}
	}
	for i, arg := range ref.Args {
		// An argument naming another placeholder expands to it; anything
		// else is used literally. That is what makes @console(header,
		// prefix) mean "those fragments" rather than "those two words".
		value := arg
		if body, ok := outer[Placeholder(arg)]; ok {
			value = body
		}
		if i < len(params) {
			inner[Placeholder(params[i])] = value
		}
		inner[Placeholder(fmt.Sprint(i+1))] = value
	}
	joined := make([]string, 0, len(ref.Args))
	for _, a := range ref.Args {
		if body, ok := outer[Placeholder(a)]; ok {
			joined = append(joined, body)
			continue
		}
		joined = append(joined, a)
	}
	inner["args"] = strings.Join(joined, "\n")
	return inner
}

// readName reads @name, returning the name and how many bytes it occupied.
func readName(s string) (string, int) {
	if len(s) < 2 || s[0] != '@' {
		return "", 0
	}
	i := 1
	for i < len(s) && (isLetter(s[i]) || (i > 1 && isDigit(s[i]))) {
		i++
	}
	if i == 1 {
		return "", 0
	}
	return s[1:i], i
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

// checkAcyclic refuses a reference cycle, naming the path that closed it.
//
// Without this a cycle is a hang or a stack overflow, discovered by waiting.
// With it the message says exactly which fragment refers back to which.
func checkAcyclic(t Template, fill Fill) error {
	const root = Placeholder("\x00root")
	edges := map[Placeholder][]Placeholder{root: refs(t.Text, fill)}
	for p, body := range fill {
		edges[p] = refs(body, fill)
	}

	state := map[Placeholder]int{} // 0 unvisited, 1 on stack, 2 done
	var stack []Placeholder
	var visit func(Placeholder) error
	visit = func(p Placeholder) error {
		switch state[p] {
		case 1:
			at := -1
			for i, s := range stack {
				if s == p {
					at = i
					break
				}
			}
			cycle := append(append([]Placeholder(nil), stack[at:]...), p)
			var parts []string
			for _, c := range cycle {
				if c == root {
					parts = append(parts, t.Name)
					continue
				}
				parts = append(parts, "@"+string(c))
			}
			return fmt.Errorf("%s: placeholders refer to each other in a loop: %s",
				t.Name, strings.Join(parts, " -> "))
		case 2:
			return nil
		}
		state[p] = 1
		stack = append(stack, p)
		for _, next := range edges[p] {
			if err := visit(next); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[p] = 2
		return nil
	}
	return visit(root)
}

func refs(text string, fill Fill) []Placeholder {
	var out []Placeholder
	seen := map[Placeholder]bool{}
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		ref, ok := readRef(text[i:])
		if !ok {
			continue
		}
		if _, known := fill[ref.Name]; known && !seen[ref.Name] {
			seen[ref.Name] = true
			out = append(out, ref.Name)
		}
		i += ref.Width - 1
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// checkUnknown reports an @name that looks like a placeholder but is not one.
//
// A typo silently leaves the text in place, producing a syntax error from the
// runtime that points at a line the user did not write. Catching it here
// means the message names the misspelling and lists what was available.
func checkUnknown(t Template, fill Fill) error {
	known := map[string]bool{}
	for _, p := range All {
		known[string(p)] = true
	}
	for p := range fill {
		known[string(p)] = true
	}
	var bad []string
	seen := map[string]bool{}
	for i := 0; i < len(t.Text); i++ {
		if t.Text[i] != '@' {
			continue
		}
		ref, ok := readRef(t.Text[i:])
		if !ok {
			continue
		}
		name := string(ref.Name)
		i += max(ref.Width-1, 0)
		if known[name] || seen[name] {
			continue
		}
		// Only complain about names that look deliberate. An email address
		// or a decorator should not trip this.
		if looksLikePlaceholder(name) {
			seen[name] = true
			bad = append(bad, "@"+name)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	var names []string
	for _, p := range All {
		names = append(names, "@"+string(p))
	}
	return fmt.Errorf("%s: unknown placeholder %s; available are %s",
		t.Name, strings.Join(bad, ", "), strings.Join(names, " "))
}

// looksLikePlaceholder is a near-miss test against the known names. It is
// deliberately narrow: the cost of a false positive is refusing a valid
// launcher, which is worse than letting an odd @word through.
func looksLikePlaceholder(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range All {
		known := strings.ToLower(string(p))
		if lower == known {
			return true
		}
		if len(lower) > 2 && (strings.HasPrefix(known, lower) || strings.HasPrefix(lower, known)) {
			return true
		}
		if editDistanceAtMostOne(lower, known) || isTransposition(lower, known) {
			return true
		}
	}
	return false
}

// isTransposition catches a swapped pair, which plain edit distance scores as
// two changes. Swaps are among the most common typos -- "entyr" for "entry" --
// so treating them as one is what makes the check useful.
func isTransposition(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff []int
	for i := range a {
		if a[i] != b[i] {
			diff = append(diff, i)
			if len(diff) > 2 {
				return false
			}
		}
	}
	return len(diff) == 2 && diff[1] == diff[0]+1 &&
		a[diff[0]] == b[diff[1]] && a[diff[1]] == b[diff[0]]
}

func editDistanceAtMostOne(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	i, j, edits := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		edits++
		if edits > 1 {
			return false
		}
		if len(a) == len(b) {
			i++
		}
		j++
	}
	return true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Used reports which placeholders a template actually references, which is
// how the caller learns that a custom launcher never runs the script.
func Used(t Template) []Placeholder {
	fill := Fill{}
	for _, p := range All {
		fill[p] = ""
	}
	return refs(t.Text, fill)
}
