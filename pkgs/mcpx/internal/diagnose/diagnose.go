// Package diagnose explains a failing script in terms of what changed.
//
// The value here is that it needs no model. mcpx already knows every tool's
// input schema, and -- once the history in this package is kept -- when each
// of those schemas last changed and how. A script that calls a tool with no
// arguments after that tool gained a required one is not a mystery: it is two
// facts mcpx is already holding, put next to each other.
//
// So the diagnostic is deterministic, and it is produced on the paths that
// already exist: preflight before a run, and the error from a call that came
// back invalid params. Sampling a model to reword an error mcpx can state
// exactly would cost a round trip to say something less precise.
package diagnose

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Kind classifies a diagnostic, so a caller can act on one without reading
// the prose.
type Kind string

const (
	// KindMissingArgument is a required argument the call does not pass.
	KindMissingArgument Kind = "missing-argument"
	// KindUnknownArgument is an argument the tool does not declare.
	KindUnknownArgument Kind = "unknown-argument"
	// KindUnknownTool is a call to something that is not there.
	KindUnknownTool Kind = "unknown-tool"
	// KindUnknownNamespace is a call on a server that is not configured.
	KindUnknownNamespace Kind = "unknown-namespace"
	// KindInvalidParams is an upstream -32602, mapped back to a field.
	KindInvalidParams Kind = "invalid-params"
)

// ChangeKind says what happened to a tool or one of its arguments.
type ChangeKind string

const (
	ChangeToolAdded   ChangeKind = "tool-added"
	ChangeToolRemoved ChangeKind = "tool-removed"
	ChangeArgAdded    ChangeKind = "argument-added"
	ChangeArgRemoved  ChangeKind = "argument-removed"
	ChangeArgRequired ChangeKind = "argument-required"
	ChangeArgOptional ChangeKind = "argument-optional"
	ChangeArgRetyped  ChangeKind = "argument-retyped"
)

// Change is one thing that happened to a tool's schema, and when.
type Change struct {
	When  time.Time  `json:"when"`
	Kind  ChangeKind `json:"kind"`
	Tool  string     `json:"tool"`
	Field string     `json:"field,omitempty"`
	// What is the sentence a diagnostic quotes.
	What string `json:"what"`
}

func (c Change) String() string {
	return fmt.Sprintf("%s (%s)", c.What, c.When.Format(time.DateOnly))
}

// Site is where in a script something was written.
type Site struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Diagnostic is one specific, actionable complaint.
type Diagnostic struct {
	Kind Kind `json:"kind"`
	// Tool is namespace.function, in the spelling a script uses.
	Tool string `json:"tool,omitempty"`
	// Field is the argument at fault, when there is exactly one.
	Field string `json:"field,omitempty"`
	// Message states the problem.
	Message string `json:"message"`
	// Changed is what the catalog history says about this tool, when the
	// history explains the problem. Absent when nothing is recorded, which
	// is the honest answer on a first run.
	Changed *Change `json:"changed,omitempty"`
	// Source is the call as written.
	Source *Site `json:"source,omitempty"`
	// Fix is the minimal edit that makes the call legal. It is source, not
	// prose, so it can be pasted.
	Fix string `json:"fix,omitempty"`
	// Fatal marks a diagnostic that should stop a run. An unknown argument
	// is not fatal: servers accept extras all the time.
	Fatal bool `json:"fatal,omitempty"`
}

// String renders a diagnostic the way the issue asked for: what is wrong,
// what changed and when, what was written, and the minimum that works.
func (d Diagnostic) String() string {
	var b strings.Builder
	if d.Tool != "" {
		b.WriteString(d.Tool + ": ")
	}
	b.WriteString(d.Message)
	if d.Changed != nil {
		fmt.Fprintf(&b, " (schema changed %s: %s)",
			d.Changed.When.Format(time.DateOnly), d.Changed.What)
	}
	if d.Source != nil {
		fmt.Fprintf(&b, "\n  you wrote:  %s", d.Source.Text)
		if d.Source.Line > 0 {
			fmt.Fprintf(&b, "   (line %d)", d.Source.Line)
		}
	}
	if d.Fix != "" {
		fmt.Fprintf(&b, "\n  minimum:    %s", d.Fix)
	}
	return b.String()
}

// Render writes a list of diagnostics as text.
func Render(ds []Diagnostic) string {
	var lines []string
	for _, d := range ds {
		lines = append(lines, d.String())
	}
	return strings.Join(lines, "\n")
}

// Tool is what a diagnostic needs to know about one tool.
type Tool struct {
	Namespace string `json:"namespace"`
	// Name is the MCP name; Func is the generated TypeScript identifier.
	Name string `json:"name"`
	Func string `json:"func"`
	// Shape is the flattened input schema.
	Shape Shape `json:"shape"`
}

// Path is the spelling a script uses.
func (t Tool) Path() string { return t.Namespace + "." + t.Func }

// Shape is the part of an input schema a deterministic diagnostic can use.
//
// Top-level properties only. Nesting is where schemas get interesting and
// where guessing gets expensive; every failure this package exists to explain
// -- an argument appearing, becoming required, changing type -- is visible at
// the top level.
type Shape struct {
	Props    map[string]string `json:"props"`
	Required []string          `json:"required"`
}

// RequiredSet is the required names as a set.
func (s Shape) RequiredSet() map[string]bool {
	out := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		out[r] = true
	}
	return out
}

// ShapeOf flattens a JSON Schema into what this package reasons about.
func ShapeOf(raw json.RawMessage) Shape {
	sh := Shape{Props: map[string]string{}}
	if len(raw) == 0 {
		return sh
	}
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return sh
	}
	for name, p := range doc.Properties {
		sh.Props[name] = typeOf(p)
	}
	sh.Required = append([]string(nil), doc.Required...)
	sort.Strings(sh.Required)
	return sh
}

func typeOf(raw json.RawMessage) string {
	var doc struct {
		Type any `json:"type"`
		Enum []any
	}
	if json.Unmarshal(raw, &doc) != nil {
		return "unknown"
	}
	switch t := doc.Type.(type) {
	case string:
		return t
	case []any:
		var parts []string
		for _, v := range t {
			parts = append(parts, fmt.Sprint(v))
		}
		return strings.Join(parts, "|")
	}
	if len(doc.Enum) > 0 {
		return "enum"
	}
	return "unknown"
}

// Catalog is the view of the world a diagnostic is computed against.
type Catalog struct {
	// Tools is every tool currently visible.
	Tools []Tool
	// Changes maps namespace.func to what the history knows, newest first.
	Changes map[string][]Change
}

func (c Catalog) lookup(ns, fn string) (Tool, bool) {
	for _, t := range c.Tools {
		if t.Namespace == ns && (t.Func == fn || t.Name == fn) {
			return t, true
		}
	}
	return Tool{}, false
}

func (c Catalog) hasNamespace(ns string) bool {
	for _, t := range c.Tools {
		if t.Namespace == ns {
			return true
		}
	}
	return false
}

// change returns the most recent recorded change for a tool, preferring one
// that mentions the field at fault: "options became required" explains a
// missing `options` in a way "description was retyped" does not.
func (c Catalog) change(path, field string) *Change {
	list := c.Changes[path]
	var fallback *Change
	for i := range list {
		ch := list[i]
		if field != "" && ch.Field == field {
			return &ch
		}
		if fallback == nil {
			fallback = &ch
		}
	}
	if field != "" {
		return nil
	}
	return fallback
}

// Script diagnoses a script's tool calls against the catalog.
//
// Only calls whose namespace is configured are considered. Everything else in
// a script is somebody's own code, and this package has no business having an
// opinion about it.
func Script(src string, cat Catalog) []Diagnostic {
	var out []Diagnostic
	for _, call := range Calls(src) {
		out = append(out, diagnoseCall(call, cat)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		la, lb := 0, 0
		if a.Source != nil {
			la = a.Source.Line
		}
		if b.Source != nil {
			lb = b.Source.Line
		}
		return la < lb
	})
	return out
}

func diagnoseCall(call Call, cat Catalog) []Diagnostic {
	if !cat.hasNamespace(call.Namespace) {
		// Silence rather than a guess: `console.log(...)` and `JSON.parse(x)`
		// both look exactly like a namespace call from here.
		return nil
	}
	site := &Site{Line: call.Line, Text: call.Text}
	path := call.Namespace + "." + call.Func

	tool, ok := cat.lookup(call.Namespace, call.Func)
	if !ok {
		d := Diagnostic{
			Kind: KindUnknownTool, Tool: path, Source: site, Fatal: true,
			Message: "this server has no such tool",
		}
		if ch := cat.change(path, ""); ch != nil && ch.Kind == ChangeToolRemoved {
			d.Changed = ch
		}
		if near := nearest(call.Func, cat, call.Namespace); near != "" {
			d.Fix = strings.Replace(call.Text, call.Func, near, 1)
			d.Message += "; the closest name it does have is " + near
		}
		return []Diagnostic{d}
	}

	required := tool.Shape.Required
	if len(required) == 0 {
		return unknownArgs(call, tool, site)
	}

	if call.Empty {
		d := Diagnostic{
			Kind: KindMissingArgument, Tool: path, Field: required[0], Source: site,
			Fatal: true,
			Message: fmt.Sprintf("the argument object is no longer optional: %s",
				requiredList(required)),
			Changed: cat.change(path, required[0]),
			Fix:     fixWith(call, tool, required),
		}
		return []Diagnostic{d}
	}

	keys, understood := ObjectKeys(call.Args)
	if !understood {
		return nil
	}
	have := map[string]bool{}
	for _, k := range keys {
		have[k] = true
	}
	var out []Diagnostic
	for _, req := range required {
		if have[req] {
			continue
		}
		out = append(out, Diagnostic{
			Kind: KindMissingArgument, Tool: path, Field: req, Source: site, Fatal: true,
			Message: fmt.Sprintf("%s is required and is not passed", req),
			Changed: cat.change(path, req),
			Fix:     addKey(call, tool, req),
		})
	}
	return append(out, unknownArgs(call, tool, site)...)
}

// unknownArgs reports arguments the tool does not declare.
//
// Not fatal. A server is free to accept more than it documents, and several
// do; refusing to run on that basis would break working scripts to make a
// point about a schema somebody else wrote.
func unknownArgs(call Call, tool Tool, site *Site) []Diagnostic {
	if call.Empty || len(tool.Shape.Props) == 0 {
		return nil
	}
	keys, understood := ObjectKeys(call.Args)
	if !understood {
		return nil
	}
	var out []Diagnostic
	for _, k := range keys {
		if _, ok := tool.Shape.Props[k]; ok {
			continue
		}
		out = append(out, Diagnostic{
			Kind: KindUnknownArgument, Tool: tool.Path(), Field: k, Source: site,
			Message: fmt.Sprintf("%s is not an argument this tool declares; it takes %s",
				k, strings.Join(sortedKeys(tool.Shape.Props), ", ")),
		})
	}
	return out
}

func requiredList(required []string) string {
	if len(required) == 1 {
		return required[0] + " is required"
	}
	return strings.Join(required, ", ") + " are required"
}

// fixWith writes the smallest call that could type check: every required
// argument present, with a placeholder value of the right kind.
func fixWith(call Call, tool Tool, required []string) string {
	var parts []string
	for _, r := range required {
		parts = append(parts, fmt.Sprintf("%s: %s", r, zeroFor(tool.Shape.Props[r])))
	}
	return fmt.Sprintf("await %s({ %s })", tool.Path(), strings.Join(parts, ", "))
}

func addKey(call Call, tool Tool, key string) string {
	return fmt.Sprintf("await %s({ %s: %s, ... })", tool.Path(), key,
		zeroFor(tool.Shape.Props[key]))
}

func zeroFor(typ string) string {
	switch {
	case strings.HasPrefix(typ, "string"):
		return `""`
	case strings.HasPrefix(typ, "number"), strings.HasPrefix(typ, "integer"):
		return "0"
	case strings.HasPrefix(typ, "boolean"):
		return "false"
	case strings.HasPrefix(typ, "array"):
		return "[]"
	case strings.HasPrefix(typ, "object"):
		return "{}"
	}
	return "…"
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// nearest is the closest tool name in the namespace, when one is close
// enough to be worth suggesting.
func nearest(name string, cat Catalog, ns string) string {
	best, bestD := "", 1<<30
	for _, t := range cat.Tools {
		if t.Namespace != ns {
			continue
		}
		d := editDistance(strings.ToLower(name), strings.ToLower(t.Func))
		if d < bestD {
			best, bestD = t.Func, d
		}
	}
	// A third of the name may differ. Beyond that the suggestion is noise
	// and reads as the tool having guessed.
	if bestD > 1+len(name)/3 {
		return ""
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// CallErrorInput is everything known about a call that failed upstream.
type CallErrorInput struct {
	// Namespace and Tool name the call. Tool may be either spelling.
	Namespace string
	Tool      string
	// Args is what was sent.
	Args json.RawMessage
	// Code is the JSON-RPC error code, when there was one. -32602 is invalid
	// params, which is the code this function exists for.
	Code int
	// Message is the server's error text.
	Message string
	// Source, when known, is the line that made the call.
	Source *Site
}

// InvalidParams is the JSON-RPC code for a call whose arguments do not fit
// the schema the server published.
const InvalidParams = -32602

// JSON-RPC 2.0 reserves serverErrorLow..serverErrorHigh for
// implementation-defined server errors.
const (
	serverErrorHigh = -32000
	serverErrorLow  = -32099
)

// CallError explains a failed upstream call.
//
// The interesting case is -32602 and schema validation text, because those
// are exactly the failures a schema change produces, and the server's own
// message rarely says which argument or when it changed.
func CallError(in CallErrorInput, cat Catalog) []Diagnostic {
	// Decided before the catalog is consulted. The lookups below answer
	// "no such tool" and "no such namespace", and those are only true when
	// the server said the call itself was wrong: said about a crash or a
	// timeout they blame the caller for the server's failure, and said about
	// a namespace whose schemas have not been read yet they are false.
	if !aboutArguments(in.Code, in.Message) {
		return nil
	}
	tool, ok := cat.lookup(in.Namespace, in.Tool)
	if !ok {
		if !cat.hasNamespace(in.Namespace) {
			return []Diagnostic{{
				Kind: KindUnknownNamespace, Tool: in.Namespace + "." + in.Tool,
				Message: "no server is configured under this namespace",
				Source:  in.Source, Fatal: true,
			}}
		}
		d := Diagnostic{
			Kind: KindUnknownTool, Tool: in.Namespace + "." + in.Tool,
			Message: "this server has no such tool", Source: in.Source, Fatal: true,
		}
		if ch := cat.change(in.Namespace+"."+in.Tool, ""); ch != nil {
			d.Changed = ch
		}
		return []Diagnostic{d}
	}

	sent := map[string]bool{}
	if len(in.Args) > 0 {
		var obj map[string]json.RawMessage
		if json.Unmarshal(in.Args, &obj) == nil {
			for k := range obj {
				sent[k] = true
			}
		}
	}
	var out []Diagnostic
	for _, req := range tool.Shape.Required {
		if sent[req] {
			continue
		}
		out = append(out, Diagnostic{
			Kind: KindInvalidParams, Tool: tool.Path(), Field: req, Fatal: true,
			Message: fmt.Sprintf("the call was rejected and %s is required but was not sent", req),
			Changed: cat.change(tool.Path(), req),
			Source:  in.Source,
			Fix:     fmt.Sprintf("await %s({ %s: %s, ... })", tool.Path(), req, zeroFor(tool.Shape.Props[req])),
		})
	}
	if len(out) > 0 {
		return out
	}
	// Nothing missing: name the field the server complained about, if it
	// named one, rather than repeating its message unchanged.
	if field := fieldInMessage(in.Message, tool.Shape); field != "" {
		return []Diagnostic{{
			Kind: KindInvalidParams, Tool: tool.Path(), Field: field, Fatal: true,
			Message: fmt.Sprintf("the server rejected %s (it wants %s): %s",
				field, tool.Shape.Props[field], strings.TrimSpace(in.Message)),
			Changed: cat.change(tool.Path(), field),
			Source:  in.Source,
		}}
	}
	return []Diagnostic{{
		Kind: KindInvalidParams, Tool: tool.Path(), Fatal: true,
		Message: fmt.Sprintf("the server rejected the arguments: %s; it takes %s",
			strings.TrimSpace(in.Message), strings.Join(sortedKeys(tool.Shape.Props), ", ")),
		Changed: cat.change(tool.Path(), ""),
		Source:  in.Source,
	}}
}

// aboutArguments reports whether a failure is the server rejecting what was
// sent, which is the only failure a schema can explain.
func aboutArguments(code int, msg string) bool {
	if code == InvalidParams {
		return true
	}
	// A code in the server-error range names its own condition, so its text
	// is not read for schema words. MCP forbids reading cross-implementation
	// meaning into -32000..-32019 and allocates -32020..-32099 to conditions
	// of its own (2026-07-28 schema). And mcpx's own client reports a dropped
	// connection as -32000 "connection closed: unexpected EOF"
	// (internal/mcpclient/client.go, fail), which the text match below would
	// take for a complaint about a type: "unexpected" contains "expected".
	if code <= serverErrorHigh && code >= serverErrorLow {
		return false
	}
	return looksLikeSchemaError(msg)
}

func looksLikeSchemaError(msg string) bool {
	l := strings.ToLower(msg)
	for _, s := range []string{"invalid param", "invalid_param", "schema", "required",
		"validation", "expected", "must be"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// fieldInMessage finds a declared argument named in the server's message.
func fieldInMessage(msg string, shape Shape) string {
	best := ""
	for name := range shape.Props {
		if strings.Contains(msg, name) && len(name) > len(best) {
			best = name
		}
	}
	return best
}
