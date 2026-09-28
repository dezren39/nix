// Package openapi turns an OpenAPI 3.x specification into callable tools.
//
// This is the piece opencode v2's code mode has and mcpx did not. The value
// is not subtle: an enormous amount of capability is already described by a
// specification somebody else maintains, and turning one into tools is a
// mechanical transformation nobody should do by hand.
//
// A specification is a better source than a hand-written adapter for the same
// reason a generated client is better than a hand-written one: it is already
// correct, it is already complete, and it changes when the service does.
package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Spec is the subset of OpenAPI that matters for making calls.
//
// Deliberately partial. A full model of OpenAPI is thousands of lines and
// most of it describes documentation, not invocation; what is needed here is
// enough to build a request and describe its arguments.
type Spec struct {
	OpenAPI string `json:"openapi" yaml:"openapi"`
	Swagger string `json:"swagger" yaml:"swagger"`
	Info    struct {
		Title       string `json:"title" yaml:"title"`
		Version     string `json:"version" yaml:"version"`
		Description string `json:"description" yaml:"description"`
	} `json:"info" yaml:"info"`
	Servers []struct {
		URL         string `json:"url" yaml:"url"`
		Description string `json:"description" yaml:"description"`
	} `json:"servers" yaml:"servers"`
	// Host, BasePath and Schemes are Swagger 2.0's way of saying Servers.
	Host     string   `json:"host" yaml:"host"`
	BasePath string   `json:"basePath" yaml:"basePath"`
	Schemes  []string `json:"schemes" yaml:"schemes"`

	// Source is where the document was loaded from, kept because a server
	// URL may be relative and has to resolve against it.
	Source string `json:"-" yaml:"-"`

	Paths      map[string]PathItem `json:"paths" yaml:"paths"`
	Components struct {
		Schemas map[string]any `json:"schemas" yaml:"schemas"`
	} `json:"components" yaml:"components"`
	Definitions map[string]any `json:"definitions" yaml:"definitions"`
}

// PathItem is the operations available at one path.
type PathItem struct {
	Parameters []Parameter          `json:"parameters" yaml:"parameters"`
	Get        *Operation           `json:"get" yaml:"get"`
	Put        *Operation           `json:"put" yaml:"put"`
	Post       *Operation           `json:"post" yaml:"post"`
	Delete     *Operation           `json:"delete" yaml:"delete"`
	Patch      *Operation           `json:"patch" yaml:"patch"`
	Head       *Operation           `json:"head" yaml:"head"`
	Options    *Operation           `json:"options" yaml:"options"`
	rest       map[string]Operation `json:"-" yaml:"-"`
}

func (p PathItem) operations() map[string]*Operation {
	out := map[string]*Operation{}
	for method, op := range map[string]*Operation{
		"GET": p.Get, "PUT": p.Put, "POST": p.Post, "DELETE": p.Delete,
		"PATCH": p.Patch, "HEAD": p.Head, "OPTIONS": p.Options,
	} {
		if op != nil {
			out[method] = op
		}
	}
	return out
}

// Operation is one callable endpoint.
type Operation struct {
	OperationID string      `json:"operationId" yaml:"operationId"`
	Summary     string      `json:"summary" yaml:"summary"`
	Description string      `json:"description" yaml:"description"`
	Tags        []string    `json:"tags" yaml:"tags"`
	Deprecated  bool        `json:"deprecated" yaml:"deprecated"`
	Parameters  []Parameter `json:"parameters" yaml:"parameters"`
	RequestBody *struct {
		Required bool `json:"required" yaml:"required"`
		Content  map[string]struct {
			Schema map[string]any `json:"schema" yaml:"schema"`
		} `json:"content" yaml:"content"`
	} `json:"requestBody" yaml:"requestBody"`
}

// Parameter is one argument, wherever it goes.
type Parameter struct {
	Name        string         `json:"name" yaml:"name"`
	In          string         `json:"in" yaml:"in"` // path, query, header, cookie
	Description string         `json:"description" yaml:"description"`
	Required    bool           `json:"required" yaml:"required"`
	Schema      map[string]any `json:"schema" yaml:"schema"`
	// Swagger 2.0 puts the type directly on the parameter.
	Type string `json:"type" yaml:"type"`
	Enum []any  `json:"enum" yaml:"enum"`
}

// Tool is one operation, described the way an MCP client expects.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`

	Method string      `json:"-"`
	Path   string      `json:"-"`
	Params []Parameter `json:"-"`
	Body   bool        `json:"-"`
}

// API is a loaded specification ready to call.
type API struct {
	Name    string
	Spec    *Spec
	BaseURL string
	Tools   []Tool
	Headers map[string]string
	HTTP    *http.Client
}

// Load reads a specification from a file path or a URL.
func Load(ctx context.Context, source string, timeout time.Duration) (*Spec, error) {
	var raw []byte
	var err error

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Accept", "application/json, application/yaml, text/yaml")
		client := &http.Client{Timeout: timeout}
		resp, derr := client.Do(req)
		if derr != nil {
			return nil, fmt.Errorf("fetching %s: %w", source, derr)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s returned %s", source, resp.Status)
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	} else {
		raw, err = os.ReadFile(source)
	}
	if err != nil {
		return nil, err
	}
	spec, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	spec.Source = source
	return spec, nil
}

// Parse reads a specification in JSON or YAML.
//
// Both, because specifications are published in both and which one you get is
// not the caller's choice. YAML is a superset of JSON, so one parser would
// do -- but the JSON path is tried first because its errors are far more
// precise when the document really is JSON.
func Parse(raw []byte) (*Spec, error) {
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err == nil && spec.hasContent() {
		return &spec, nil
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("not a usable OpenAPI document: %w", err)
	}
	if !spec.hasContent() {
		return nil, fmt.Errorf("the document parsed but declares no paths")
	}
	return &spec, nil
}

func (s Spec) hasContent() bool { return len(s.Paths) > 0 }

// Version reports which dialect this is, since Swagger 2.0 spells several
// things differently and saying so is more useful than silently coping.
func (s Spec) Version() string {
	switch {
	case s.OpenAPI != "":
		return s.OpenAPI
	case s.Swagger != "":
		return "swagger " + s.Swagger
	}
	return "unknown"
}

// Options control how a specification becomes tools.
type Options struct {
	// Name prefixes every tool. Empty derives one from the title.
	Name string
	// BaseURL overrides the specification's own server list, which is often
	// a placeholder or a production URL somebody does not want hit.
	BaseURL string
	// Include and Exclude filter operations by a substring of the tool name,
	// the path or a tag. A large specification is mostly not what you want.
	Include []string
	Exclude []string
	// Methods restricts which verbs are exposed. Defaulting to read-only is
	// deliberate: turning a specification into tools should not, by itself,
	// hand something the ability to delete.
	Methods []string
	// IncludeDeprecated exposes operations the specification marks as such.
	IncludeDeprecated bool
	// Headers are sent on every request.
	Headers map[string]string
	Timeout time.Duration
}

// DefaultMethods are exposed unless a caller says otherwise.
//
// Read-only by default. A specification describes what a service can do, not
// what you meant to allow, and the difference between listing orders and
// cancelling them should be a deliberate keystroke.
var DefaultMethods = []string{"GET", "HEAD", "OPTIONS"}

// Build turns a specification into callable tools.
func Build(spec *Spec, opt Options) (*API, error) {
	name := opt.Name
	if name == "" {
		name = slug(spec.Info.Title)
	}
	if name == "" {
		name = "api"
	}
	base := opt.BaseURL
	if base == "" {
		base = spec.baseURL()
	}
	if base == "" {
		return nil, fmt.Errorf("%s declares no server URL; give one with --base-url", name)
	}
	// A relative server URL resolves against wherever the document was
	// fetched from. The specification permits it and real services use it --
	// the Swagger petstore declares "/api/v3" -- and without this the result
	// is a request with no scheme, which fails in a way that looks like a
	// network problem rather than an unresolved reference.
	if resolved, err := resolveBase(base, spec.Source); err == nil {
		base = resolved
	} else {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	allowed := map[string]bool{}
	methods := opt.Methods
	if len(methods) == 0 {
		methods = DefaultMethods
	}
	for _, m := range methods {
		allowed[strings.ToUpper(m)] = true
	}
	if allowed["ALL"] || allowed["*"] {
		for _, m := range []string{"GET", "PUT", "POST", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
			allowed[m] = true
		}
	}

	api := &API{
		Name: name, Spec: spec, BaseURL: strings.TrimRight(base, "/"),
		Headers: opt.Headers,
		HTTP:    &http.Client{Timeout: orDefault(opt.Timeout, 60*time.Second)},
	}

	paths := make([]string, 0, len(spec.Paths))
	for p := range spec.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	seen := map[string]bool{}
	for _, path := range paths {
		item := spec.Paths[path]
		for method, op := range item.operations() {
			if !allowed[method] {
				continue
			}
			if op.Deprecated && !opt.IncludeDeprecated {
				continue
			}
			toolName := operationName(name, method, path, op)
			if !matches(toolName, path, op, opt) {
				continue
			}
			// An operationId is not guaranteed unique in practice, whatever
			// the specification says. A collision would silently shadow one
			// operation with another, so it is disambiguated rather than
			// dropped.
			base := toolName
			for i := 2; seen[toolName]; i++ {
				toolName = fmt.Sprintf("%s_%d", base, i)
			}
			seen[toolName] = true

			params := append(append([]Parameter{}, item.Parameters...), op.Parameters...)
			tool := Tool{
				Name:        toolName,
				Description: describe(method, path, op),
				InputSchema: buildSchema(params, op),
				Method:      method,
				Path:        path,
				Params:      params,
				Body:        op.RequestBody != nil,
			}
			api.Tools = append(api.Tools, tool)
		}
	}
	sort.Slice(api.Tools, func(i, j int) bool { return api.Tools[i].Name < api.Tools[j].Name })
	if len(api.Tools) == 0 {
		return nil, fmt.Errorf("%s: nothing matched; %d paths were considered and the "+
			"allowed methods were %s", name, len(spec.Paths), strings.Join(methods, ", "))
	}
	return api, nil
}

// resolveBase makes a server URL absolute.
func resolveBase(base, source string) (string, error) {
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		return base, nil
	}
	if source == "" || !(strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")) {
		return "", fmt.Errorf("the document declares the relative server URL %q, and it "+
			"was not loaded over HTTP so there is nothing to resolve it against; "+
			"give one with --base-url", base)
	}
	from, err := url.Parse(source)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return from.ResolveReference(rel).String(), nil
}

func orDefault(d, fallback time.Duration) time.Duration {
	if d == 0 {
		return fallback
	}
	return d
}

func (s Spec) baseURL() string {
	for _, srv := range s.Servers {
		if srv.URL != "" && !strings.Contains(srv.URL, "{") {
			return srv.URL
		}
	}
	// Swagger 2.0.
	if s.Host != "" {
		scheme := "https"
		for _, sc := range s.Schemes {
			if sc == "https" {
				scheme = sc
				break
			}
			scheme = sc
		}
		return scheme + "://" + s.Host + s.BasePath
	}
	// A templated server URL is better than nothing when it is all there is;
	// the caller can override, and the error otherwise names no candidate.
	for _, srv := range s.Servers {
		if srv.URL != "" {
			return srv.URL
		}
	}
	return ""
}

func matches(toolName, path string, op *Operation, opt Options) bool {
	hay := strings.ToLower(toolName + " " + path + " " + strings.Join(op.Tags, " ") + " " + op.Summary)
	for _, ex := range opt.Exclude {
		if ex != "" && strings.Contains(hay, strings.ToLower(ex)) {
			return false
		}
	}
	if len(opt.Include) == 0 {
		return true
	}
	for _, in := range opt.Include {
		if in != "" && strings.Contains(hay, strings.ToLower(in)) {
			return true
		}
	}
	return false
}

// operationName prefers the operationId, which is what the publisher chose.
// Falling back to the method and path produces something stable and readable
// rather than something generated, because a tool name ends up in a model's
// reasoning and "get_pets_by_id" is a better prompt than "op_17".
func operationName(prefix, method, path string, op *Operation) string {
	if op.OperationID != "" {
		return prefix + "_" + slug(op.OperationID)
	}
	clean := strings.NewReplacer("{", "", "}", "", "/", "_", "-", "_").Replace(path)
	return slug(prefix + "_" + strings.ToLower(method) + "_" + clean)
}

func describe(method, path string, op *Operation) string {
	parts := []string{}
	if op.Summary != "" {
		parts = append(parts, op.Summary)
	}
	if op.Description != "" && op.Description != op.Summary {
		parts = append(parts, firstSentences(op.Description, 2))
	}
	if len(parts) == 0 {
		parts = append(parts, method+" "+path)
	}
	// The verb and path are always appended. A model choosing between two
	// similar tools is helped more by knowing one is a DELETE than by another
	// adjective.
	parts = append(parts, "("+method+" "+path+")")
	if op.Deprecated {
		parts = append(parts, "[deprecated]")
	}
	return strings.Join(parts, " ")
}

func firstSentences(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	count := 0
	for i, r := range s {
		if r == '.' {
			count++
			if count >= n {
				return s[:i+1]
			}
		}
	}
	if len(s) > 300 {
		return s[:297] + "..."
	}
	return s
}

// buildSchema renders an operation's arguments as one JSON Schema object.
//
// Path, query and header parameters and the request body are flattened into
// a single object, because that is what a tool call is: one bag of named
// arguments. Where they go is remembered separately, in the Tool.
func buildSchema(params []Parameter, op *Operation) json.RawMessage {
	props := map[string]any{}
	var required []string

	for _, p := range params {
		if p.Name == "" || p.In == "cookie" {
			continue
		}
		entry := map[string]any{}
		if len(p.Schema) > 0 {
			for k, v := range p.Schema {
				entry[k] = v
			}
		}
		if _, has := entry["type"]; !has {
			entry["type"] = orString(p.Type, "string")
		}
		if len(p.Enum) > 0 {
			entry["enum"] = p.Enum
		}
		desc := p.Description
		if p.In != "query" {
			// Saying where a parameter goes matters for path parameters,
			// which are part of the URL and cannot be omitted.
			desc = strings.TrimSpace(desc + " (" + p.In + ")")
		}
		if desc != "" {
			entry["description"] = firstSentences(desc, 2)
		}
		props[p.Name] = entry
		if p.Required {
			required = append(required, p.Name)
		}
	}

	if op.RequestBody != nil {
		for media, c := range op.RequestBody.Content {
			if !strings.Contains(media, "json") {
				continue
			}
			entry := map[string]any{"description": "the request body"}
			for k, v := range c.Schema {
				entry[k] = v
			}
			props["body"] = entry
			if op.RequestBody.Required {
				required = append(required, "body")
			}
			break
		}
	}

	doc := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		doc["required"] = required
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return b
}

func orString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// Result is one call's outcome.
type Result struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	JSON    json.RawMessage   `json:"json,omitempty"`
	URL     string            `json:"url"`
}

// Call invokes one tool.
func (a *API) Call(ctx context.Context, tool string, args map[string]any) (*Result, error) {
	var spec *Tool
	for i := range a.Tools {
		if a.Tools[i].Name == tool {
			spec = &a.Tools[i]
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("%s has no operation %q", a.Name, tool)
	}

	path := spec.Path
	query := url.Values{}
	headers := map[string]string{}
	for k, v := range a.Headers {
		headers[k] = v
	}

	for _, p := range spec.Params {
		raw, given := args[p.Name]
		if !given || raw == nil {
			if p.Required && p.In == "path" {
				return nil, fmt.Errorf("%s is part of the URL and is required", p.Name)
			}
			continue
		}
		value := toString(raw)
		switch p.In {
		case "path":
			// Escaped, because a path parameter carrying a slash would
			// otherwise silently change which endpoint is called.
			path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(value))
		case "header":
			headers[p.Name] = value
		default:
			if list, ok := raw.([]any); ok {
				for _, e := range list {
					query.Add(p.Name, toString(e))
				}
				continue
			}
			query.Set(p.Name, value)
		}
	}
	if i := strings.Index(path, "{"); i >= 0 {
		return nil, fmt.Errorf("%s left %s unfilled", tool, path[i:])
	}

	target := a.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var body io.Reader
	if spec.Body {
		if v, ok := args["body"]; ok && v != nil {
			encoded, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("body is not encodable: %w", err)
			}
			body = bytes.NewReader(encoded)
			if _, set := headers["Content-Type"]; !set {
				headers["Content-Type"] = "application/json"
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, spec.Method, target, body)
	if err != nil {
		return nil, err
	}
	if _, set := headers["Accept"]; !set {
		headers["Accept"] = "application/json"
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", spec.Method, target, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}

	res := &Result{Status: resp.StatusCode, Body: string(raw), URL: target}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "json") {
		var probe any
		if json.Unmarshal(raw, &probe) == nil {
			res.JSON = json.RawMessage(raw)
		}
	}
	// A non-2xx is returned rather than raised, for the same reason a
	// non-zero exit is: the service answered, and what the answer means is
	// the caller's judgement.
	return res, nil
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case nil:
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return strings.Trim(string(b), `"`)
}

func slug(s string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
			prevUnderscore = false
		default:
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}
