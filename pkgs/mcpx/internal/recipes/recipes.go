// Package recipes turns saved scripts into parameterised, matchable units.
//
// A recipe is not a new artefact. It is a script that already exists on the
// scripts search path, which happens to declare the holes in itself. That
// matters for the reason the issue gives: repeat work should be deterministic
// and free, and the cheapest way to make it so is to reuse what somebody
// already wrote and got working rather than to generate it again.
//
// Declaration is a header comment, because a comment survives every runtime,
// cannot affect execution, and is visible in the file somebody edits:
//
//	// Close stale issues in a repository.
//	// @param repo:string        which repository, as owner/name
//	// @param days:number = 30   how old counts as stale
//	// @param dryRun:boolean?    report without closing
//
//	const stale = await demo.search({ repo: @repo, olderThanDays: @days });
//
// References in the body are `@name`, the same spelling the launcher uses for
// its own holes, and they are replaced by the JSON encoding of the value. A
// recipe is therefore still a valid script to read, and an invalid one to run
// until its holes are filled -- which is the right way round: the file cannot
// be run by accident with a placeholder left in it.
package recipes

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dezren39/mcpx/internal/diagnose"
)

// Type is the type of a placeholder value.
type Type string

const (
	TypeString  Type = "string"
	TypeNumber  Type = "number"
	TypeBoolean Type = "boolean"
	// TypeJSON is anything else: an object, an array, a value the recipe
	// wants to pass through untouched.
	TypeJSON Type = "json"
)

// Placeholder is one parameter of a recipe.
type Placeholder struct {
	Name        string `json:"name"`
	Type        Type   `json:"type"`
	Description string `json:"description,omitempty"`
	// Default is the literal as written in the declaration, already valid
	// for the type. Absent means there is none.
	Default *string `json:"default,omitempty"`
	// Required is false when a default was given or the name was marked
	// optional with a trailing question mark.
	Required bool `json:"required"`
}

// Recipe is a saved script with its holes described.
type Recipe struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Summary is the first comment line, as `mcpx scripts` shows it.
	Summary      string        `json:"summary,omitempty"`
	Placeholders []Placeholder `json:"placeholders,omitempty"`
	// Tools are the namespace.tool calls the body makes, which is what makes
	// matching free text to a recipe more than a name comparison.
	Tools []string `json:"tools,omitempty"`
	// Source is the file's contents. Omitted from listings, which would
	// otherwise be dominated by it.
	Source string `json:"source,omitempty"`
}

// paramDecl matches the head of one `@param name:type[?] [= default]
// description` line, leaving the tail to be taken apart by hand -- a default
// may be a quoted string or a brace-delimited literal, and a regular
// expression that got that right would be unreadable.
//
// The type is optional and defaults to string, because most placeholders are
// one, and making every declaration carry `:string` would be ceremony that
// teaches nothing.
var paramDecl = regexp.MustCompile(
	`^\s*(?://|#|\*)\s*@param\s+([A-Za-z_][A-Za-z0-9_]*)(\?)?(?:\s*:\s*([A-Za-z]+))?(\?)?\s*(.*)$`)

// declLine recognises a declaration without taking it apart, so the body
// scanner can skip one. Without it the `@param` of the declaration itself
// reads as a placeholder called "param".
var declLine = regexp.MustCompile(`^\s*(?://|#|\*)\s*@param\b`)

// Parse reads a script and describes it as a recipe.
func Parse(name, path, src string) Recipe {
	r := Recipe{Name: name, Path: path, Source: src}
	var declared []Placeholder
	seen := map[string]bool{}

	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if r.Summary == "" && strings.HasPrefix(trimmed, "//") &&
			!strings.Contains(trimmed, "@param") {
			r.Summary = strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
		}
		m := paramDecl.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		p := Placeholder{Name: m[1], Type: normaliseType(m[3]), Required: true}
		if m[2] == "?" || m[4] == "?" {
			p.Required = false
		}
		rest := strings.TrimSpace(m[5])
		if after, ok := strings.CutPrefix(rest, "="); ok {
			// `= 30 how old counts as stale`: the default is the first
			// token unless the value is quoted or bracketed, in which case
			// it runs to its closing mark, and the rest is the description.
			value, tail := splitDefault(strings.TrimSpace(after))
			p.Default = &value
			p.Required = false
			rest = strings.TrimSpace(tail)
		}
		// A `--` before the description is accepted and not required: some
		// people write one, and insisting either way would be a rule whose
		// only effect is to make a declaration silently lose its prose.
		p.Description = strings.TrimSpace(strings.TrimPrefix(rest, "--"))
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		declared = append(declared, p)
	}

	used := refs(src)
	// A name used in the body but never declared is still a placeholder:
	// refusing to run it would be right, but describing it as a required
	// string is more useful, because the form then asks for it.
	for _, name := range used {
		if seen[name] {
			continue
		}
		seen[name] = true
		declared = append(declared, Placeholder{Name: name, Type: TypeString, Required: true})
	}
	// Declared but unused placeholders stay: a recipe may pass one through
	// to a tool by another route, and dropping it would make the form
	// disagree with the file.
	r.Placeholders = declared

	toolSet := map[string]bool{}
	for _, c := range diagnose.Calls(src) {
		if jsGlobals[c.Namespace] {
			continue
		}
		toolSet[c.Path()] = true
	}
	for t := range toolSet {
		r.Tools = append(r.Tools, t)
	}
	sort.Strings(r.Tools)
	return r
}

// jsGlobals are the built-in objects whose members look exactly like tool
// calls. This is a fact about JavaScript rather than a setting, which is why
// it is a list here and not a knob: nobody wants `console.log` counted as a
// tool, and nobody wants to configure that.
//
// Getting it wrong is cheap in one direction and not the other. A namespace
// missing from a recipe's tool list only weakens its ranking; `console.log`
// present in one makes every recipe match the word "log".
var jsGlobals = map[string]bool{
	"console": true, "JSON": true, "Math": true, "Object": true, "Array": true,
	"String": true, "Number": true, "Boolean": true, "Promise": true,
	"Date": true, "RegExp": true, "Map": true, "Set": true, "Error": true,
	"globalThis": true, "process": true, "Deno": true, "Bun": true,
	"window": true, "document": true, "localStorage": true, "Reflect": true,
	"Symbol": true, "BigInt": true, "Intl": true, "WebAssembly": true,
}

func normaliseType(s string) Type {
	switch strings.ToLower(s) {
	case "number", "int", "integer", "float":
		return TypeNumber
	case "bool", "boolean":
		return TypeBoolean
	case "json", "object", "array", "any":
		return TypeJSON
	}
	return TypeString
}

// splitDefault takes the literal off the front of the text after `=`.
func splitDefault(s string) (value, tail string) {
	if s == "" {
		return "", ""
	}
	switch s[0] {
	case '"', '\'', '`':
		q := s[0]
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == q {
				return s[:i+1], s[i+1:]
			}
		}
		return s, ""
	case '{', '[':
		open, close := s[0], byte('}')
		if open == '[' {
			close = ']'
		}
		depth := 0
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return s[:i+1], s[i+1:]
				}
			}
		}
		return s, ""
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// refs finds every `@name` in the body, ignoring declarations, comments and
// anything inside a string.
func refs(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(src, "\n") {
		if declLine.MatchString(line) {
			continue
		}
		for i := 0; i < len(line); i++ {
			if line[i] != '@' {
				continue
			}
			// An email address or a decorator is not a placeholder, and the
			// cheapest way to tell is that a placeholder is preceded by
			// whitespace or punctuation rather than by a word character.
			if i > 0 && isWordByte(line[i-1]) {
				continue
			}
			j := i + 1
			for j < len(line) && isWordByte(line[j]) {
				j++
			}
			if j == i+1 {
				continue
			}
			name := line[i+1 : j]
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
			i = j - 1
		}
	}
	sort.Strings(out)
	return out
}

func isWordByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// Missing lists the placeholders a set of values does not supply and that
// have no default to fall back on.
func (r Recipe) Missing(values map[string]any) []Placeholder {
	var out []Placeholder
	for _, p := range r.Placeholders {
		if _, given := values[p.Name]; given {
			continue
		}
		if p.Default != nil || !p.Required {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Render substitutes values into the body.
//
// Values are written as JSON, so a string arrives quoted and an object
// arrives as an object. Substituting raw text would make every recipe an
// injection site, and a recipe is run with the user's credentials.
func (r Recipe) Render(values map[string]any) (string, error) {
	lits := map[string]string{}
	for _, p := range r.Placeholders {
		v, given := values[p.Name]
		switch {
		case given:
			lit, err := literal(p, v)
			if err != nil {
				return "", err
			}
			lits[p.Name] = lit
		case p.Default != nil:
			lits[p.Name] = *p.Default
		case !p.Required:
			lits[p.Name] = "undefined"
		default:
			return "", fmt.Errorf("recipe %s needs %s", r.Name, p.Name)
		}
	}

	var b strings.Builder
	for _, line := range strings.Split(r.Source, "\n") {
		if declLine.MatchString(line) {
			// The declaration is consumed. Leaving it in would be harmless
			// but the rendered script is what a diagnostic reports line
			// numbers against, and they should match what ran.
			b.WriteString("//" + strings.TrimPrefix(strings.TrimSpace(line), "//") + "\n")
			continue
		}
		b.WriteString(substitute(line, lits))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func substitute(line string, lits map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] != '@' || (i > 0 && isWordByte(line[i-1])) {
			b.WriteByte(line[i])
			continue
		}
		j := i + 1
		for j < len(line) && isWordByte(line[j]) {
			j++
		}
		name := line[i+1 : j]
		lit, ok := lits[name]
		if !ok {
			b.WriteByte(line[i])
			continue
		}
		b.WriteString(lit)
		i = j - 1
	}
	return b.String()
}

func literal(p Placeholder, v any) (string, error) {
	switch p.Type {
	case TypeNumber:
		switch n := v.(type) {
		case float64:
			return strconv.FormatFloat(n, 'g', -1, 64), nil
		case int:
			return strconv.Itoa(n), nil
		case string:
			if _, err := strconv.ParseFloat(n, 64); err != nil {
				return "", fmt.Errorf("%s is a number, got %q", p.Name, n)
			}
			return n, nil
		}
	case TypeBoolean:
		switch t := v.(type) {
		case bool:
			return strconv.FormatBool(t), nil
		case string:
			b, err := strconv.ParseBool(t)
			if err != nil {
				return "", fmt.Errorf("%s is a boolean, got %q", p.Name, t)
			}
			return strconv.FormatBool(b), nil
		}
	case TypeJSON:
		if s, ok := v.(string); ok && json.Valid([]byte(s)) {
			return s, nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("%s cannot be encoded: %w", p.Name, err)
	}
	return string(b), nil
}

// FormSchema is the elicitation schema that asks for the missing values.
//
// Every field carries its description and, where there is one, its default,
// because the broker's contract is that a question can be answered without a
// person: a headless caller times out and the defaults are what remains.
func FormSchema(ps []Placeholder) json.RawMessage {
	props := map[string]any{}
	var required []string
	for _, p := range ps {
		f := map[string]any{"type": jsonType(p.Type)}
		if p.Description != "" {
			f["description"] = p.Description
		}
		if p.Default != nil {
			if v, ok := decodeLiteral(p, *p.Default); ok {
				f["default"] = v
			}
		}
		props[p.Name] = f
		if p.Required && p.Default == nil {
			required = append(required, p.Name)
		}
	}
	sort.Strings(required)
	doc := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		doc["required"] = required
	}
	b, _ := json.Marshal(doc)
	return b
}

func jsonType(t Type) string {
	switch t {
	case TypeNumber:
		return "number"
	case TypeBoolean:
		return "boolean"
	case TypeJSON:
		return "object"
	}
	return "string"
}

func decodeLiteral(p Placeholder, lit string) (any, bool) {
	var v any
	if json.Unmarshal([]byte(lit), &v) == nil {
		return v, true
	}
	if p.Type == TypeString {
		return strings.Trim(lit, "\"'`"), true
	}
	return nil, false
}
