package mcpclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Request metadata headers, 2026-07-28.
//
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#standard-request-headers
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#custom-headers-from-tool-parameters
//
// A modern POST repeats parts of its body in headers so that something in
// front of the server -- a gateway, a load balancer -- can route without
// parsing JSON. The server checks that the two agree and answers -32020 when
// they do not, so a header that is missing is as fatal as one that is wrong.

const (
	sentinelPrefix = "=?base64?"
	sentinelSuffix = "?="
	paramHeader    = "Mcp-Param-"
)

// encodeHeaderValue renders a value for a header, using the Base64 sentinel
// where the plain form is not safe: anything outside visible ASCII, space and
// tab; leading or trailing whitespace, which HTTP strips; and any plain value
// that already looks like the sentinel, which would otherwise be decoded into
// something the client never sent.
func encodeHeaderValue(v string) string {
	if headerSafe(v) && !(strings.HasPrefix(v, sentinelPrefix) && strings.HasSuffix(v, sentinelSuffix)) {
		return v
	}
	return sentinelPrefix + base64.StdEncoding.EncodeToString([]byte(v)) + sentinelSuffix
}

func headerSafe(v string) bool {
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

// standardHeaders are Mcp-Method and Mcp-Name for one modern frame. Mcp-Name
// is the tool or prompt name, or the resource URI, of the three methods the
// table names.
func standardHeaders(msg []byte) map[string]string {
	var f struct {
		Method string `json:"method"`
		Params struct {
			Name *string `json:"name"`
			URI  *string `json:"uri"`
		} `json:"params"`
	}
	if json.Unmarshal(msg, &f) != nil || f.Method == "" {
		return nil
	}
	h := map[string]string{"Mcp-Method": f.Method}
	var name *string
	switch f.Method {
	case "tools/call", "prompts/get":
		name = f.Params.Name
	case "resources/read":
		name = f.Params.URI
	}
	if name != nil {
		h["Mcp-Name"] = encodeHeaderValue(*name)
	}
	return h
}

// headerParam is one x-mcp-header annotation: the header name part and the
// chain of properties keys that leads to the annotated value.
type headerParam struct {
	Name string
	Path []string
}

// toolHeaders validates a tool's x-mcp-header annotations and returns them.
// An error means the tool definition is invalid and must be excluded.
//
// The constraints are the schema-extension list: a non-empty RFC 9110 token,
// unique case-insensitively, on a primitive (integer, string, boolean -- not
// number) reached only through "properties". An annotation anywhere else --
// under items, a composition keyword, a $ref -- makes the whole tool invalid,
// so the walk looks everywhere and only accepts the reachable ones.
func toolHeaders(schema json.RawMessage) ([]headerParam, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, nil // not our concern; the schema is forwarded as it is
	}
	var out []headerParam
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
				out = append(out, headerParam{Name: name, Path: append([]string(nil), path...)})
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

// paramHeaders extracts the Mcp-Param-* headers for one call. A value that is
// absent or null omits its header; a value of the wrong type is an error,
// because sending a header the server will reject is worse than not calling.
func paramHeaders(params []headerParam, args any) (map[string]string, error) {
	if len(params) == 0 {
		return nil, nil
	}
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
	out := map[string]string{}
	for _, p := range params {
		v, ok := lookup(root, p.Path)
		if !ok || v == nil {
			continue
		}
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case bool:
			s = strconv.FormatBool(x)
		case json.Number:
			i, err := strconv.ParseInt(x.String(), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %s is not an integer", strings.Join(p.Path, "."), x)
			}
			if i > maxSafeInt || i < -maxSafeInt {
				return nil, fmt.Errorf("%s: %d is outside the JavaScript safe-integer range", strings.Join(p.Path, "."), i)
			}
			s = strconv.FormatInt(i, 10)
		default:
			return nil, fmt.Errorf("%s: a %T cannot be mirrored into a header", strings.Join(p.Path, "."), v)
		}
		out[paramHeader+p.Name] = encodeHeaderValue(s)
	}
	return out, nil
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

// extraHeadersKey carries per-request headers from the client to the HTTP
// transport. Transport.Send takes bytes; this is the one thing about a
// request that is not in them.
type extraHeadersKey struct{}

func withExtraHeaders(ctx context.Context, h map[string]string) context.Context {
	if len(h) == 0 {
		return ctx
	}
	return context.WithValue(ctx, extraHeadersKey{}, h)
}

func extraHeaders(ctx context.Context, req *http.Request) {
	if ctx == nil {
		return
	}
	if h, ok := ctx.Value(extraHeadersKey{}).(map[string]string); ok {
		for k, v := range h {
			req.Header.Set(k, v)
		}
	}
}
