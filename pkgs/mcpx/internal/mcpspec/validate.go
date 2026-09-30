// Package mcpspec validates JSON-RPC frames against the official MCP JSON schemas.
//
// It is test support: import it only from _test.go files so the embedded schemas never enter the binary
// (TestNotImportedByBinary enforces this). The validator implements exactly the keywords the vendored
// schemas use -- $ref (to #/definitions/... and #/$defs/...), type, properties, required, items,
// additionalProperties, anyOf, oneOf, allOf, enum, const, minimum, maximum, maxItems -- and the formats
// "byte" (standard base64) and "uri" (must parse and carry a scheme). "uri-template" is advisory: RFC 6570
// accepts almost any string, so checking it would catch nothing. Keywords outside that set are ignored, and
// TestSchemasUseOnlyKnownKeywords fails if a refreshed schema starts using one.
package mcpspec

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed schema
var schemaFS embed.FS

// Schema is one revision's parsed schema.json.
type Schema struct {
	Rev  string
	Defs map[string]any
}

var (
	loadOnce sync.Once
	schemas  map[string]*Schema
	loadErr  error
)

func load() {
	schemas = map[string]*Schema{}
	entries, err := fs.ReadDir(schemaFS, "schema")
	if err != nil {
		loadErr = err
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := schemaFS.ReadFile("schema/" + e.Name() + "/schema.json")
		if err != nil {
			loadErr = err
			return
		}
		v, err := decode(raw)
		if err != nil {
			loadErr = fmt.Errorf("%s: %w", e.Name(), err)
			return
		}
		root, _ := v.(map[string]any)
		defs, _ := root["definitions"].(map[string]any)
		if defs == nil {
			defs, _ = root["$defs"].(map[string]any)
		}
		schemas[e.Name()] = &Schema{Rev: e.Name(), Defs: defs}
	}
}

// Revisions lists the embedded protocol revisions, oldest first.
func Revisions() []string {
	loadOnce.Do(load)
	out := make([]string, 0, len(schemas))
	for r := range schemas {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// Get returns the schema for rev.
func Get(rev string) (*Schema, error) {
	loadOnce.Do(load)
	if loadErr != nil {
		return nil, loadErr
	}
	s, ok := schemas[rev]
	if !ok {
		return nil, fmt.Errorf("mcpspec: no schema for revision %q", rev)
	}
	return s, nil
}

// Examples returns the official example files for rev keyed by "<Def>/<file>".
func Examples(rev string) (map[string][]byte, error) {
	out := map[string][]byte{}
	root := "schema/" + rev + "/examples"
	err := fs.WalkDir(schemaFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := schemaFS.ReadFile(p)
		out[strings.TrimPrefix(p, root+"/")] = b
		return err
	})
	if err != nil && !strings.Contains(err.Error(), "file does not exist") {
		return nil, err
	}
	return out, nil
}

// decode keeps numbers as json.Number so integer-ness is checkable.
func decode(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// openBags are properties whose value is caller data or a JSON Schema rather than protocol structure, so
// strict mode does not descend into them: a tool's inputSchema legitimately says "additionalProperties".
var openBags = map[string]bool{"_meta": true, "inputSchema": true, "outputSchema": true, "requestedSchema": true,
	"structuredContent": true, "arguments": true, "data": true, "experimental": true, "extensions": true}

// Error is a validation failure at a JSON path ("$.result.tools[0].inputSchema").
type Error struct {
	Path string
	Msg  string
}

func (e *Error) Error() string { return e.Path + ": " + e.Msg }

// Validate checks the JSON document v against definition defName of revision rev.
func Validate(rev, defName string, v []byte) error {
	s, err := Get(rev)
	if err != nil {
		return err
	}
	doc, err := decode(v)
	if err != nil {
		return &Error{Path: "$", Msg: "invalid JSON: " + err.Error()}
	}
	return s.ValidateValue(defName, doc)
}

// ValidateStrict is Validate that also rejects properties the definition does not define.
func ValidateStrict(rev, defName string, v []byte) error {
	s, err := Get(rev)
	if err != nil {
		return err
	}
	doc, err := decode(v)
	if err != nil {
		return &Error{Path: "$", Msg: "invalid JSON: " + err.Error()}
	}
	return s.validateAt(defName, doc, "$", true)
}

// ValidateValue checks an already-decoded value (numbers as json.Number or float64).
func (s *Schema) ValidateValue(defName string, doc any) error {
	return s.validateAt(defName, doc, "$", false)
}

func (s *Schema) validateAt(defName string, doc any, path string, strict bool) error {
	def, ok := s.Defs[defName]
	if !ok {
		return fmt.Errorf("mcpspec: %s has no definition %q", s.Rev, defName)
	}
	if e := s.checkMode(def, doc, path, strict, nil); e != nil {
		return e
	}
	return nil
}

func (s *Schema) resolve(ref string) (any, error) {
	for _, p := range []string{"#/definitions/", "#/$defs/"} {
		if name, ok := strings.CutPrefix(ref, p); ok {
			if d, ok := s.Defs[name]; ok {
				return d, nil
			}
		}
	}
	return nil, fmt.Errorf("unresolvable $ref %q", ref)
}

func (s *Schema) check(schema any, v any, path string) *Error {
	return s.checkMode(schema, v, path, false, nil)
}

func (s *Schema) checkMode(schema any, v any, path string, strict bool, inherited map[string]bool) *Error {
	sch, ok := schema.(map[string]any)
	if !ok {
		// Boolean schemas: true accepts everything, false nothing.
		if b, isBool := schema.(bool); isBool && !b {
			return &Error{path, "schema false admits no value"}
		}
		return nil
	}
	// 2020-12 applies $ref alongside sibling keywords; draft-07 ignores siblings, but none of the draft-07
	// files put siblings other than "description" next to a $ref, so one rule serves both.
	if ref, ok := sch["$ref"].(string); ok {
		target, err := s.resolve(ref)
		if err != nil {
			return &Error{path, err.Error()}
		}
		if e := s.checkMode(target, v, path, strict, s.allowed(sch, inherited)); e != nil {
			return e
		}
	}
	if t, ok := sch["type"]; ok {
		if e := checkType(t, v, path); e != nil {
			return e
		}
	}
	if c, ok := sch["const"]; ok && !equal(c, v) {
		return &Error{path, fmt.Sprintf("must be %s, got %s", show(c), show(v))}
	}
	if en, ok := sch["enum"].([]any); ok {
		found := false
		for _, c := range en {
			if equal(c, v) {
				found = true
				break
			}
		}
		if !found {
			return &Error{path, fmt.Sprintf("%s is not one of %s", show(v), show(en))}
		}
	}
	if n, ok := num(v); ok {
		if m, ok := num(sch["minimum"]); ok && n < m {
			return &Error{path, fmt.Sprintf("%v is below minimum %v", n, m)}
		}
		if m, ok := num(sch["maximum"]); ok && n > m {
			return &Error{path, fmt.Sprintf("%v is above maximum %v", n, m)}
		}
	}
	if str, ok := v.(string); ok {
		if e := checkFormat(sch["format"], str, path); e != nil {
			return e
		}
	}
	if obj, ok := v.(map[string]any); ok {
		if req, ok := sch["required"].([]any); ok {
			var missing []string
			for _, r := range req {
				if k, _ := r.(string); k != "" {
					if _, has := obj[k]; !has {
						missing = append(missing, strconv.Quote(k))
					}
				}
			}
			if len(missing) > 0 {
				return &Error{path, "missing required property " + strings.Join(missing, ", ")}
			}
		}
		props, _ := sch["properties"].(map[string]any)
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sub := path + "." + k
			if ps, ok := props[k]; ok {
				if e := s.checkMode(ps, obj[k], sub, strict && !openBags[k], nil); e != nil {
					return e
				}
				continue
			}
			ap, declared := sch["additionalProperties"]
			// Strict mode is "send conservatively": the schemas leave additionalProperties open so a
			// receiver tolerates the future, but a sender that adds keys its revision never defined is
			// offering the peer something it did not negotiate. _meta is the sanctioned open bag.
			if strict && !declared && !envelopeKeys[k] && !s.branches(sch) && s.closed(sch) && !s.allowed(sch, inherited)[k] {
				return &Error{sub, "property not defined by this revision (strict)"}
			}
			if declared {
				if b, isBool := ap.(bool); isBool && !b {
					return &Error{sub, "property not allowed"}
				}
				if e := s.checkMode(ap, obj[k], sub, strict, nil); e != nil {
					return e
				}
			}
		}
	}
	if arr, ok := v.([]any); ok {
		if m, ok := num(sch["maxItems"]); ok && float64(len(arr)) > m {
			return &Error{path, fmt.Sprintf("%d items exceeds maxItems %v", len(arr), m)}
		}
		if items, ok := sch["items"]; ok {
			for i, it := range arr {
				if e := s.checkMode(items, it, path+"["+strconv.Itoa(i)+"]", strict, nil); e != nil {
					return e
				}
			}
		}
	}
	if all, ok := sch["allOf"].([]any); ok {
		for _, sub := range all {
			if e := s.checkMode(sub, v, path, strict, s.allowed(sch, inherited)); e != nil {
				return e
			}
		}
	}
	if any_, ok := sch["anyOf"].([]any); ok {
		if e := s.anyOf(any_, v, path, strict, s.allowed(sch, inherited)); e != nil {
			return e
		}
	}
	if one, ok := sch["oneOf"].([]any); ok {
		n := 0
		var first *Error
		for _, sub := range one {
			if e := s.checkMode(sub, v, path, strict, s.allowed(sch, inherited)); e == nil {
				n++
			} else if first == nil {
				first = e
			}
		}
		if n != 1 {
			if n == 0 {
				return &Error{path, "matches no oneOf branch; first: " + first.Error()}
			}
			return &Error{path, fmt.Sprintf("matches %d oneOf branches, want exactly 1", n)}
		}
	}
	return nil
}

// allowed is every property name the schema admits at this level: its own, those of the $ref and allOf
// schemas applied to the same value, and those inherited from an enclosing schema that applied this one.
// Strict mode needs the union because the 2026-07-28 file composes (JSONRPCErrorResponse = allOf of an id
// part and an error part), and each part alone names only some of the keys.
func (s *Schema) allowed(sch map[string]any, inherited map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range inherited {
		out[k] = true
	}
	var walk func(x any, depth int)
	walk = func(x any, depth int) {
		m, ok := x.(map[string]any)
		if !ok || depth > 32 {
			return
		}
		if props, ok := m["properties"].(map[string]any); ok {
			for k := range props {
				out[k] = true
			}
		}
		if ref, ok := m["$ref"].(string); ok {
			if t, err := s.resolve(ref); err == nil {
				walk(t, depth+1)
			}
		}
		if all, ok := m["allOf"].([]any); ok {
			for _, b := range all {
				walk(b, depth+1)
			}
		}
	}
	walk(sch, 0)
	return out
}

// envelopeKeys are JSON-RPC framing, allowed anywhere: 2026-07-28 embeds requests in inputRequests without
// an id, yet its own ListRootsRequest example carries one.
var envelopeKeys = map[string]bool{"_meta": true, "jsonrpc": true, "id": true}

// closed reports whether strict mode may reject unknown keys here: the schema names properties and nothing
// on the $ref/allOf chain declares additionalProperties (which makes the object a map, like InputRequests).
func (s *Schema) closed(sch map[string]any) bool {
	named, open := false, false
	var walk func(x any, depth int)
	walk = func(x any, depth int) {
		m, ok := x.(map[string]any)
		if !ok || depth > 32 {
			return
		}
		if _, ok := m["properties"]; ok {
			named = true
		}
		if _, ok := m["additionalProperties"]; ok {
			open = true
		}
		if ref, ok := m["$ref"].(string); ok {
			if t, err := s.resolve(ref); err == nil {
				walk(t, depth+1)
			}
		}
		if all, ok := m["allOf"].([]any); ok {
			for _, b := range all {
				walk(b, depth+1)
			}
		}
	}
	walk(sch, 0)
	return named && !open
}

// branches reports whether an anyOf/oneOf applies at this level (directly or through $ref/allOf). The
// union's branches then judge unknown keys, each with this level's names inherited, since this level
// cannot know which branch the value is.
func (s *Schema) branches(sch map[string]any) bool {
	var walk func(x any, depth int) bool
	walk = func(x any, depth int) bool {
		m, ok := x.(map[string]any)
		if !ok || depth > 32 {
			return false
		}
		if m["anyOf"] != nil || m["oneOf"] != nil {
			return true
		}
		if ref, ok := m["$ref"].(string); ok {
			if t, err := s.resolve(ref); err == nil && walk(t, depth+1) {
				return true
			}
		}
		if all, ok := m["allOf"].([]any); ok {
			for _, b := range all {
				if walk(b, depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(sch, 0)
}

// anyOf reports the failure of the branch that got deepest, which is almost always the branch the author
// meant (a ContentBlock with type "text" fails TextContent deep inside, and every other branch at "type").
func (s *Schema) anyOf(branches []any, v any, path string, strict bool, inherited map[string]bool) *Error {
	var best *Error
	for _, sub := range branches {
		e := s.checkMode(sub, v, path, strict, inherited)
		if e == nil {
			return nil
		}
		if best == nil || depth(e.Path) > depth(best.Path) {
			best = e
		}
	}
	return &Error{best.Path, "matches no anyOf branch; closest: " + best.Msg}
}

func depth(p string) int { return strings.Count(p, ".") + strings.Count(p, "[") }

func checkType(t any, v any, path string) *Error {
	var names []string
	switch tt := t.(type) {
	case string:
		names = []string{tt}
	case []any:
		for _, x := range tt {
			if s, ok := x.(string); ok {
				names = append(names, s)
			}
		}
	}
	for _, n := range names {
		if isType(n, v) {
			return nil
		}
	}
	return &Error{path, fmt.Sprintf("want type %s, got %s", strings.Join(names, "|"), typeOf(v))}
}

func isType(name string, v any) bool {
	switch name {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "number":
		_, ok := num(v)
		return ok
	case "integer":
		n, ok := num(v)
		return ok && n == math.Trunc(n)
	}
	return false
}

func typeOf(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case nil:
		return "null"
	}
	if _, ok := num(v); ok {
		return "number"
	}
	return fmt.Sprintf("%T", v)
}

func num(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

func checkFormat(f any, s, path string) *Error {
	switch f {
	case "byte":
		if _, err := base64.StdEncoding.DecodeString(s); err != nil {
			return &Error{path, "format byte: not standard base64: " + err.Error()}
		}
	case "uri":
		u, err := url.Parse(s)
		if err != nil || u.Scheme == "" {
			return &Error{path, fmt.Sprintf("format uri: %q is not an absolute URI", s)}
		}
	}
	return nil
}

func equal(a, b any) bool {
	if x, ok := num(a); ok {
		y, ok := num(b)
		return ok && x == y
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

func show(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 120 {
		return string(b[:120]) + "..."
	}
	return string(b)
}
