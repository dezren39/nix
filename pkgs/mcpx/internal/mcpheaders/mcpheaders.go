// Package mcpheaders is 2026-07-28's request-metadata header encoding:
// the Base64 sentinel, x-mcp-header annotations, and the Mcp-Param-*
// values they mirror. One implementation, used by mcpx as a client (to
// send the headers) and as a server (to check them), so the two sides
// cannot drift apart.
//
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#custom-headers-from-tool-parameters
package mcpheaders

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	SentinelPrefix = "=?base64?"
	SentinelSuffix = "?="
	ParamPrefix    = "Mcp-Param-"
)

// Encode renders a value for a header, using the Base64 sentinel
// where the plain form is not safe: anything outside visible ASCII, space and
// tab; leading or trailing whitespace, which HTTP strips; and any plain value
// that already looks like the sentinel, which would otherwise be decoded into
// something the client never sent.
func Encode(v string) string {
	if Safe(v) && !(strings.HasPrefix(v, SentinelPrefix) && strings.HasSuffix(v, SentinelSuffix)) {
		return v
	}
	return SentinelPrefix + base64.StdEncoding.EncodeToString([]byte(v)) + SentinelSuffix
}

func Safe(v string) bool {
	if v == "" {
		return true
	}
	if first, last := v[0], v[len(v)-1]; first == ' ' || first == '\t' || last == ' ' || last == '\t' {
		return false
	}
	for i := 0; i < len(v); i++ {
		b := v[i]
		if b == '\t' || (b >= 0x20 && b <= 0x7e) {
			continue
		}
		return false
	}
	return true
}

// Param is one x-mcp-header annotation: the header name part and the
// chain of properties keys that leads to the annotated value.
type Param struct {
	Name string
	Path []string
}

// ToolParams validates a tool's x-mcp-header annotations and returns them.
// An error means the tool definition is invalid and must be excluded.
//
// The constraints are the schema-extension list: a non-empty RFC 9110 token,
// unique case-insensitively, on a primitive (integer, string, boolean -- not
// number) reached only through "properties". An annotation anywhere else --
// under items, a composition keyword, a $ref -- makes the whole tool invalid,
// so the walk looks everywhere and only accepts the reachable ones.
func ToolParams(schema json.RawMessage) ([]Param, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, nil // not our concern; the schema is forwarded as it is
	}
	var out []Param
	seen := map[string]bool{}
	var walk func(node any, path []string, reachable bool) error
	walk = func(node any, path []string, reachable bool) error {
		switch n := node.(type) {
		case []any:
			for _, v := range n {
				if err := walk(v, path, false); err != nil {
					return err
				}
			}
		case map[string]any:
			if raw, ok := n["x-mcp-header"]; ok {
				if !reachable || len(path) == 0 {
					return fmt.Errorf("x-mcp-header %v is not on a property reachable through properties alone", raw)
				}
				name, ok := raw.(string)
				if !ok {
					return fmt.Errorf("x-mcp-header on %s is not a string", strings.Join(path, "."))
				}
				if err := validHeaderName(name); err != nil {
					return fmt.Errorf("x-mcp-header %q on %s: %w", name, strings.Join(path, "."), err)
				}
				if t, _ := n["type"].(string); t != "string" && t != "integer" && t != "boolean" {
					return fmt.Errorf("x-mcp-header %q on %s: type %v is not integer, string or boolean",
						name, strings.Join(path, "."), n["type"])
				}
				if seen[strings.ToLower(name)] {
					return fmt.Errorf("x-mcp-header %q is not unique (case-insensitively)", name)
				}
				seen[strings.ToLower(name)] = true
				out = append(out, Param{Name: name, Path: append([]string(nil), path...)})
			}
			keys := make([]string, 0, len(n))
			for k := range n {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := n[k]
				if k == "properties" {
					if props, ok := v.(map[string]any); ok {
						pk := make([]string, 0, len(props))
						for p := range props {
							pk = append(pk, p)
						}
						sort.Strings(pk)
						for _, p := range pk {
							if err := walk(props[p], append(append([]string(nil), path...), p), reachable); err != nil {
								return err
							}
						}
						continue
					}
				}
				if k == "x-mcp-header" {
					continue
				}
				if err := walk(v, path, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, nil, true); err != nil {
		return nil, err
	}
	return out, nil
}

// validHeaderName is RFC 9110 token syntax, 1*tchar.
func validHeaderName(s string) error {
	if s == "" {
		return fmt.Errorf("empty")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return fmt.Errorf("byte %q is not an HTTP token character", c)
		}
	}
	return nil
}

// Values extracts the Mcp-Param-* headers for one call. A value that is
// absent or null omits its header; a value of the wrong type is an error,
// because sending a header the server will reject is worse than not calling.
func Values(params []Param, args any) (map[string]string, error) {
	if len(params) == 0 {
		return nil, nil
	}
	root, err := decodeArgs(args)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, p := range params {
		s, present, err := plain(root, p)
		if err != nil {
			return nil, err
		}
		if present {
			out[ParamPrefix+p.Name] = Encode(s)
		}
	}
	return out, nil
}

// Check is the server half of Values: every recognised Mcp-Param-* header
// must carry header-safe characters, decode, and equal the body value at its
// annotated path; a body value with no header, or a header with no body
// value, is a mismatch too. header returns a header's value and whether it
// was sent at all, since an empty string is a value. A non-nil error is a
// HeaderMismatch.
func Check(params []Param, args json.RawMessage, header func(string) (string, bool)) error {
	if len(params) == 0 {
		return nil
	}
	var root any
	if len(args) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(args)))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			return err
		}
	}
	for _, p := range params {
		name := ParamPrefix + p.Name
		raw, sent := header(name)
		body, present, err := plain(root, p)
		if err != nil {
			return err
		}
		if sent && !Safe(raw) {
			return fmt.Errorf("%s contains characters a header may not carry", name)
		}
		switch {
		case !sent && !present:
			continue
		case !sent:
			return fmt.Errorf("%s header is required: the body has a value at %s", name, strings.Join(p.Path, "."))
		case !present:
			return fmt.Errorf("%s header was sent but the body has no value at %s", name, strings.Join(p.Path, "."))
		}
		got, err := Decode(raw)
		if err != nil {
			return fmt.Errorf("%s header is not valid Base64: %v", name, err)
		}
		if got == body {
			continue
		}
		// Integers compare numerically: 42.0 in a header is 42.
		if _, isNum := lookupNumber(root, p.Path); isNum {
			hf, herr := strconv.ParseFloat(got, 64)
			bf, berr := strconv.ParseFloat(body, 64)
			if herr == nil && berr == nil && hf == bf {
				continue
			}
		}
		return fmt.Errorf("%s header value '%s' does not match body value '%s'", name, got, body)
	}
	return nil
}

// Decode undoes the Base64 sentinel. A value without both markers is
// literal. The encoding is strict -- padding required, alphabet only --
// because the specification's test-case table rejects both.
func Decode(v string) (string, error) {
	if !strings.HasPrefix(v, SentinelPrefix) || !strings.HasSuffix(v, SentinelSuffix) ||
		len(v) < len(SentinelPrefix)+len(SentinelSuffix) {
		return v, nil
	}
	b, err := base64.StdEncoding.Strict().DecodeString(v[len(SentinelPrefix) : len(v)-len(SentinelSuffix)])
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeArgs(args any) (any, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	var root any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	return root, nil
}

func lookupNumber(root any, path []string) (json.Number, bool) {
	v, _ := lookup(root, path)
	n, ok := v.(json.Number)
	return n, ok
}

// plain is the unencoded header form of the value at p's path, and whether
// there is one: absent and null are both "no header".
func plain(root any, p Param) (string, bool, error) {
	v, ok := lookup(root, p.Path)
	if !ok || v == nil {
		return "", false, nil
	}
	switch x := v.(type) {
	case string:
		return x, true, nil
	case bool:
		return strconv.FormatBool(x), true, nil
	case json.Number:
		i, err := strconv.ParseInt(x.String(), 10, 64)
		if err != nil {
			return "", false, fmt.Errorf("%s: %s is not an integer", strings.Join(p.Path, "."), x)
		}
		if i > maxSafeInt || i < -maxSafeInt {
			return "", false, fmt.Errorf("%s: %d is outside the JavaScript safe-integer range", strings.Join(p.Path, "."), i)
		}
		return strconv.FormatInt(i, 10), true, nil
	}
	return "", false, fmt.Errorf("%s: a %T cannot be mirrored into a header", strings.Join(p.Path, "."), v)
}

// maxSafeInt is 2^53-1, JavaScript's Number.MAX_SAFE_INTEGER.
const maxSafeInt = int64(1)<<53 - 1

func lookup(root any, path []string) (any, bool) {
	cur := root
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[k]; !ok {
			return nil, false
		}
	}
	return cur, true
}
