// Package codegen turns MCP JSON Schemas into TypeScript.
//
// Two artifacts are produced from the same model:
//
//   - a declaration block for `mcpx types <ns>`, which an agent reads to learn
//     a namespace without loading every other server's schemas, and
//   - a runnable client module that scripts import, where each tool is a typed
//     async function that posts to the daemon.
package codegen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Tool is the codegen view of an MCP tool.
type Tool struct {
	Name         string
	Description  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
}

// Namespace groups tools under one TypeScript identifier.
type Namespace struct {
	Name        string
	Server      string
	Description string
	// Instructions is the server's own guidance from initialize. It carries
	// conventions no JSON Schema can express -- how an id is obtained, which
	// call must come first -- so it is rendered above the signatures.
	Instructions string
	// Prelude is operator-supplied guidance from config, rendered above the
	// server's own. It is the escape hatch for a server whose descriptions
	// leave out something a caller needs.
	Prelude string
	Tools   []Tool
}

// schema is the subset of JSON Schema that MCP servers actually emit.
type schema struct {
	Type                 any                `json:"type"`
	Description          string             `json:"description"`
	Title                string             `json:"title"`
	Enum                 []any              `json:"enum"`
	Const                any                `json:"const"`
	Properties           map[string]*schema `json:"properties"`
	Required             []string           `json:"required"`
	AdditionalProperties json.RawMessage    `json:"additionalProperties"`
	Items                json.RawMessage    `json:"items"`
	PrefixItems          []*schema          `json:"prefixItems"`
	AnyOf                []*schema          `json:"anyOf"`
	OneOf                []*schema          `json:"oneOf"`
	AllOf                []*schema          `json:"allOf"`
	Ref                  string             `json:"$ref"`
	Defs                 map[string]*schema `json:"$defs"`
	Definitions          map[string]*schema `json:"definitions"`
	Default              json.RawMessage    `json:"default"`
	Format               string             `json:"format"`
}

type renderer struct {
	root  *schema
	depth int
	seen  map[string]bool
}

// TypeFor renders a JSON Schema as a TypeScript type expression.
func TypeFor(raw json.RawMessage, indent string) string {
	if len(raw) == 0 {
		return "Record<string, unknown>"
	}
	var s schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return "unknown"
	}
	r := &renderer{root: &s, seen: map[string]bool{}}
	return r.render(&s, indent)
}

// ArgsTypeFor renders the argument object for a tool. Tools with no properties
// get an optional-empty object so callers may omit the argument entirely.
func ArgsTypeFor(raw json.RawMessage, indent string) (ts string, optional bool) {
	if len(raw) == 0 {
		return "Record<string, unknown>", true
	}
	var s schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return "Record<string, unknown>", true
	}
	if len(s.Properties) == 0 && s.Ref == "" && len(s.AnyOf) == 0 && len(s.OneOf) == 0 && len(s.AllOf) == 0 {
		return "Record<string, unknown>", true
	}
	r := &renderer{root: &s, seen: map[string]bool{}}
	return r.render(&s, indent), len(s.Required) == 0
}

// ResultTypeFor renders the return type expression for a tool. If outputSchema
// is present and non-empty, it renders the schema and wraps it in Attached<...>;
// otherwise it falls back to ToolResult.
func ResultTypeFor(raw json.RawMessage, indent string) string {
	if len(raw) == 0 {
		return "ToolResult"
	}
	var s schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return "ToolResult"
	}
	r := &renderer{root: &s, seen: map[string]bool{}}
	res := r.render(&s, indent)
	if res == "unknown" || res == "" {
		return "ToolResult"
	}
	return "Attached<" + res + ">"
}

func (r *renderer) render(s *schema, indent string) string {
	if s == nil {
		return "unknown"
	}
	if r.depth > 12 {
		return "unknown"
	}
	r.depth++
	defer func() { r.depth-- }()

	if s.Ref != "" {
		if target := r.resolve(s.Ref); target != nil {
			if r.seen[s.Ref] {
				return "unknown" // cycle
			}
			r.seen[s.Ref] = true
			out := r.render(target, indent)
			delete(r.seen, s.Ref)
			return out
		}
		return "unknown"
	}

	if s.Const != nil {
		return literal(s.Const)
	}
	if len(s.Enum) > 0 {
		parts := make([]string, 0, len(s.Enum))
		for _, e := range s.Enum {
			parts = append(parts, literal(e))
		}
		return strings.Join(dedupe(parts), " | ")
	}
	if len(s.OneOf) > 0 {
		return r.union(s.OneOf, indent)
	}
	if len(s.AnyOf) > 0 {
		return r.union(s.AnyOf, indent)
	}
	if len(s.AllOf) > 0 {
		parts := make([]string, 0, len(s.AllOf))
		for _, sub := range s.AllOf {
			parts = append(parts, r.render(sub, indent))
		}
		return strings.Join(dedupe(parts), " & ")
	}

	types := typeList(s.Type)
	if len(types) == 0 {
		if len(s.Properties) > 0 {
			types = []string{"object"}
		} else {
			return "unknown"
		}
	}
	if len(types) > 1 {
		parts := make([]string, 0, len(types))
		for _, t := range types {
			c := *s
			c.Type = t
			parts = append(parts, r.render(&c, indent))
		}
		return strings.Join(dedupe(parts), " | ")
	}

	switch types[0] {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "null":
		return "null"
	case "array":
		if len(s.PrefixItems) > 0 {
			parts := make([]string, 0, len(s.PrefixItems))
			for _, p := range s.PrefixItems {
				parts = append(parts, r.render(p, indent))
			}
			return "[" + strings.Join(parts, ", ") + "]"
		}
		if len(s.Items) == 0 {
			return "unknown[]"
		}
		var item schema
		if err := json.Unmarshal(s.Items, &item); err != nil {
			return "unknown[]"
		}
		inner := r.render(&item, indent)
		if needsParens(inner) {
			return "(" + inner + ")[]"
		}
		return inner + "[]"
	case "object":
		return r.object(s, indent)
	}
	return "unknown"
}

func (r *renderer) union(subs []*schema, indent string) string {
	parts := make([]string, 0, len(subs))
	for _, sub := range subs {
		parts = append(parts, r.render(sub, indent))
	}
	return strings.Join(dedupe(parts), " | ")
}

func (r *renderer) object(s *schema, indent string) string {
	if len(s.Properties) == 0 {
		if extra := r.additional(s, indent); extra != "" {
			return "Record<string, " + extra + ">"
		}
		return "Record<string, unknown>"
	}
	req := map[string]bool{}
	for _, k := range s.Required {
		req[k] = true
	}
	keys := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	inner := indent + "  "
	var b strings.Builder
	b.WriteString("{\n")
	for _, k := range keys {
		p := s.Properties[k]
		if doc := jsdoc(p, inner); doc != "" {
			b.WriteString(doc)
		}
		opt := ""
		if !req[k] {
			opt = "?"
		}
		b.WriteString(fmt.Sprintf("%s%s%s: %s;\n", inner, propKey(k), opt, r.render(p, inner)))
	}
	if extra := r.additional(s, inner); extra != "" {
		b.WriteString(fmt.Sprintf("%s[key: string]: %s;\n", inner, extra))
	}
	b.WriteString(indent + "}")
	return b.String()
}

func (r *renderer) additional(s *schema, indent string) string {
	if len(s.AdditionalProperties) == 0 {
		return ""
	}
	var asBool bool
	if err := json.Unmarshal(s.AdditionalProperties, &asBool); err == nil {
		if asBool {
			return "unknown"
		}
		return ""
	}
	var sub schema
	if err := json.Unmarshal(s.AdditionalProperties, &sub); err != nil {
		return ""
	}
	return r.render(&sub, indent)
}

func (r *renderer) resolve(ref string) *schema {
	const p1, p2 = "#/$defs/", "#/definitions/"
	switch {
	case strings.HasPrefix(ref, p1):
		if r.root.Defs != nil {
			return r.root.Defs[strings.TrimPrefix(ref, p1)]
		}
	case strings.HasPrefix(ref, p2):
		if r.root.Definitions != nil {
			return r.root.Definitions[strings.TrimPrefix(ref, p2)]
		}
	}
	return nil
}

func typeList(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func literal(v any) string {
	switch x := v.(type) {
	case string:
		return quote(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		b, _ := json.Marshal(x)
		return string(b)
	case nil:
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "unknown"
	}
	return string(b)
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var identRe = func() func(string) bool {
	return func(s string) bool {
		if s == "" {
			return false
		}
		for i, c := range s {
			ok := c == '_' || c == '$' ||
				(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(i > 0 && c >= '0' && c <= '9')
			if !ok {
				return false
			}
		}
		return true
	}
}()

func propKey(k string) string {
	if identRe(k) {
		return k
	}
	return quote(k)
}

func needsParens(t string) bool {
	return strings.Contains(t, "|") || strings.Contains(t, "&") || strings.Contains(t, "=>")
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return []string{"unknown"}
	}
	return out
}

func jsdoc(s *schema, indent string) string {
	if s == nil {
		return ""
	}
	desc := strings.TrimSpace(s.Description)
	if desc == "" {
		desc = strings.TrimSpace(s.Title)
	}
	if desc == "" && len(s.Default) == 0 {
		return ""
	}
	lines := strings.Split(desc, "\n")
	var b strings.Builder
	if len(lines) == 1 && len(s.Default) == 0 && len(lines[0]) < 100 {
		return fmt.Sprintf("%s/** %s */\n", indent, sanitizeComment(lines[0]))
	}
	b.WriteString(indent + "/**\n")
	for _, l := range lines {
		b.WriteString(indent + " * " + sanitizeComment(l) + "\n")
	}
	if len(s.Default) > 0 {
		b.WriteString(indent + " * @default " + sanitizeComment(string(s.Default)) + "\n")
	}
	b.WriteString(indent + " */\n")
	return b.String()
}

func sanitizeComment(s string) string {
	return strings.ReplaceAll(strings.TrimRight(s, " \t\r"), "*/", "*\\/")
}

// ToolFuncName converts an MCP tool name into a TypeScript identifier.
func ToolFuncName(name string) string {
	var b strings.Builder
	for _, c := range name {
		switch {
		case c == '_' || c == '$' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9'):
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "_"
	}
	// A digit is legal inside an identifier but not as the first character.
	if out[0] >= '0' && out[0] <= '9' {
		out = "_" + out
	}
	// A reserved word is legal as a property and not as a declared
	// function, which is what `mcpx types` emits: `function delete(...)`.
	// Suffixed everywhere, so the catalog, the types and the client agree.
	if jsReserved[out] {
		out += "_"
	}
	return out
}

// funcCollisions groups a namespace's tools by the identifier each becomes,
// returning only the identifiers more than one tool maps to. Two tools that
// normalise alike (`get-item`, `get_item`) became duplicate keys, and the
// last silently won, so a script called the wrong tool (#215).
func funcCollisions(tools []Tool) map[string][]string {
	by := map[string][]string{}
	for _, t := range tools {
		fn := ToolFuncName(t.Name)
		by[fn] = append(by[fn], t.Name)
	}
	for fn, names := range by {
		if len(names) < 2 {
			delete(by, fn)
		}
	}
	return by
}
