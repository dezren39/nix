package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Pass-through exposure: one or more upstreams served under their own names.
//
// mcpx normally publishes a small gateway surface and namespaces everything
// behind it (mcpx_call, mcpx://<ns>/<uri>, <ns>_<prompt>). A host that only
// wants one server -- a single-upstream gateway, which is what a sandboxed or
// audited deployment of one server looks like -- is better served by that
// server's own tools, prompts and resources, unrenamed, with mcpx in front for
// pooling, logging and policy. Server.Passthrough names that upstream -- or
// several, whose surfaces are merged into one; a name two of them offer is
// refused rather than silently given to either.
//
// The tools half lives here, because tools/list and tools/call are this
// package's; prompts and resources are named by the Backend, which already
// decides what each list carries.
//
// On a collision between an upstream and the gateway the upstream wins: the gateway tool of that name is not
// listed and cannot be called over MCP. The point of the mode is that the
// upstream is the server, and a client that reads tools/list must be able to
// call every name on it and get that tool.

// PassthroughBackend is a Backend that can list one upstream's tools.
type PassthroughBackend interface {
	UpstreamTools(ctx context.Context, namespace string) ([]Tool, error)
}

// passKey carries one request's pass-through lookup, so the upstreams' tool
// lists are fetched once per request rather than once per question asked of
// them. A tools/call asks twice -- does the name exist, and is it an
// upstream's -- and each fetch is several daemon round trips.
type passKey struct{}

type passLookup struct {
	tools []Tool
	// owner maps each pass-through tool to the upstream that serves it.
	owner map[string]string
	err   error
}

// toolsVary reports whether tools/list can change while the server runs:
// only in pass-through mode or reactive mode, where tools can change dynamically. That is what
// decides tools.listChanged and whether a listen agrees to toolsListChanged.
func (s *Server) toolsVary() bool { return len(s.Passthrough) > 0 || s.Reactive }

// withPass resolves the pass-through list for the rest of this request.
func (s *Server) withPass(ctx context.Context) context.Context {
	if len(s.Passthrough) == 0 {
		return ctx
	}
	if _, ok := ctx.Value(passKey{}).(*passLookup); ok {
		return ctx
	}
	return context.WithValue(ctx, passKey{}, s.lookupPass(ctx))
}

// pass is this request's pass-through lookup, or nil when the mode is off.
// An upstream that cannot be listed is an error, not an empty list: an empty
// list would hide its tools, and a client calling one would be told it does
// not exist when it is the server that is not answering.
func (s *Server) pass(ctx context.Context) *passLookup {
	if len(s.Passthrough) == 0 {
		return nil
	}
	if l, ok := ctx.Value(passKey{}).(*passLookup); ok {
		return l
	}
	return s.lookupPass(ctx)
}

// passTools is the pass-through upstreams' merged tool list.
func (s *Server) passTools(ctx context.Context) ([]Tool, error) {
	l := s.pass(ctx)
	if l == nil {
		return nil, nil
	}
	return l.tools, l.err
}

// lookupPass lists every pass-through upstream, in the order configured, and
// merges them into one surface.
//
// Two upstreams offering the same tool name is refused rather than resolved:
// whichever won, a client reading tools/list would be calling a tool it was
// not shown, and the other upstream's tool would be unreachable without a
// word. The operator has to choose -- drop one from mcp.passthrough, or
// reach it through mcpx_call under its namespace.
func (s *Server) lookupPass(ctx context.Context) *passLookup {
	pb, ok := s.backend.(PassthroughBackend)
	if !ok {
		return &passLookup{err: fmt.Errorf("pass-through %q: this backend cannot list an upstream's tools",
			strings.Join(s.Passthrough, ","))}
	}
	l := &passLookup{owner: map[string]string{}}
	for _, ns := range s.Passthrough {
		ts, err := pb.UpstreamTools(ctx, ns)
		if err != nil {
			return &passLookup{err: fmt.Errorf("pass-through upstream %q is not answering: %w", ns, err)}
		}
		for _, t := range ts {
			if prev, dup := l.owner[t.Name]; dup {
				return &passLookup{err: fmt.Errorf(
					"pass-through upstreams %q and %q both offer a tool named %q; "+
						"mcp.passthrough cannot serve both under one name", prev, ns, t.Name)}
			}
			l.owner[t.Name] = ns
			l.tools = append(l.tools, t)
		}
	}
	return l
}

// allAvailableTools returns all available tools from pass-through upstreams and gateway extras.
func (s *Server) allAvailableTools(ctx context.Context) ([]Tool, error) {
	pass, err := s.passTools(ctx)
	if err != nil {
		return nil, err
	}
	if len(pass) == 0 {
		return s.Tools(), nil
	}
	taken := make(map[string]bool, len(pass))
	for _, t := range pass {
		taken[t.Name] = true
	}
	out := append([]Tool(nil), pass...)
	for _, t := range s.Tools() {
		if !taken[t.Name] {
			out = append(out, t)
		}
	}
	return out, nil
}

// surface is what tools/list offers: the pass-through upstreams' tools first,
// then every gateway tool whose name no upstream took. In reactive mode, it
// offers request_tools, batch_call, pinned tools, and active retained tools.
func (s *Server) surface(ctx context.Context) ([]Tool, error) {
	if s.Reactive {
		return s.reactiveSurface(ctx)
	}
	return s.allAvailableTools(ctx)
}

// isPassTool reports whether name is served by a pass-through upstream.
func (s *Server) isPassTool(ctx context.Context, name string) (bool, error) {
	ns, err := s.passOwner(ctx, name)
	return ns != "", err
}

// passOwner is the pass-through upstream serving name, or "".
func (s *Server) passOwner(ctx context.Context, name string) (string, error) {
	l := s.pass(ctx)
	if l == nil {
		return "", nil
	}
	if l.err != nil {
		return "", l.err
	}
	return l.owner[name], nil
}

// passTool is the pass-through definition of name, if an upstream serves it.
func (s *Server) passTool(ctx context.Context, name string) (Tool, bool) {
	l := s.pass(ctx)
	if l == nil || l.err != nil {
		return Tool{}, false
	}
	for _, t := range l.tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// routesToPass reports whether a call to name goes to the pass-through
// upstream: a tool it lists, or any name that is not one of the gateway's
// own. An upstream may answer to tools it does not list -- the conformance
// suite's own server has diagnostic hooks such as test_trigger_prompt_change
// -- and in pass-through mode the upstream is the server, so it is the one
// to say whether a name exists. mcpx answering "no tool named" for them was
// mcpx deciding on the server's behalf.
func (s *Server) routesToPass(ctx context.Context, name string) (bool, error) {
	pass, err := s.isPassTool(ctx, name)
	if err != nil || pass || len(s.Passthrough) == 0 {
		return pass, err
	}
	for _, t := range s.Tools() {
		if t.Name == name {
			return false, nil
		}
	}
	for _, e := range s.extras {
		if e.Tool.Name == name {
			return false, nil
		}
	}
	return true, nil
}

// PassToolOf reports what this request already knows about name: whether a
// pass-through lookup has been made for the request (known), and if so
// which upstream serves it ("" for none). Lets a Backend reuse the lookup the
// protocol layer made instead of repeating it.
func PassToolOf(ctx context.Context, name string) (ns string, known bool) {
	l, ok := ctx.Value(passKey{}).(*passLookup)
	if !ok || l.err != nil {
		return "", false
	}
	return l.owner[name], true
}

// EncodeRaw packs a complete upstream result -- a CallToolResult or a
// GetPromptResult -- into the string a Backend returns, so the protocol layer
// can send it verbatim rather than flattening it to one text block. Images,
// audio, embedded resources, structuredContent and isError survive.
func EncodeRaw(raw json.RawMessage) string {
	b, err := json.Marshal(richResult{Raw: raw})
	if err != nil {
		return string(raw)
	}
	return resultMarker + string(b)
}

// DecodeRaw returns the result EncodeRaw packed, if s is one, for a caller
// that has to render it rather than send it.
func DecodeRaw(s string) (json.RawMessage, bool) {
	rest, ok := cutMarker(s)
	if !ok {
		return nil, false
	}
	var r richResult
	if json.Unmarshal([]byte(rest), &r) != nil || len(r.Raw) == 0 {
		return nil, false
	}
	return r.Raw, true
}

// decodeRaw returns the verbatim result EncodeRaw packed, if s is one.
func decodeRaw(s string) (map[string]any, bool) {
	rest, ok := cutMarker(s)
	if !ok {
		return nil, false
	}
	var r richResult
	if json.Unmarshal([]byte(rest), &r) != nil || len(r.Raw) == 0 {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(r.Raw, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}
