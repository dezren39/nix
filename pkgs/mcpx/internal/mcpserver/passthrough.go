package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
)

// Pass-through exposure: one upstream served under its own names.
//
// mcpx normally publishes a small gateway surface and namespaces everything
// behind it (mcpx_call, mcpx://<ns>/<uri>, <ns>_<prompt>). A host that only
// wants one server -- a single-upstream gateway, which is what a sandboxed or
// audited deployment of one server looks like -- is better served by that
// server's own tools, prompts and resources, unrenamed, with mcpx in front for
// pooling, logging and policy. Server.Passthrough names that upstream.
//
// The tools half lives here, because tools/list and tools/call are this
// package's; prompts and resources are named by the Backend, which already
// decides what each list carries.
//
// On a name collision the upstream wins: the gateway tool of that name is not
// listed and cannot be called over MCP. The point of the mode is that the
// upstream is the server, and a client that reads tools/list must be able to
// call every name on it and get that tool.

// PassthroughBackend is a Backend that can list one upstream's tools.
type PassthroughBackend interface {
	UpstreamTools(ctx context.Context, namespace string) ([]Tool, error)
}

// passKey carries one request's pass-through lookup, so the upstream's tool
// list is fetched once per request rather than once per question asked of it.
// A tools/call asks twice -- does the name exist, and is it the upstream's --
// and each fetch is several daemon round trips.
type passKey struct{}

type passLookup struct {
	tools []Tool
	err   error
}

// withPass resolves the pass-through list for the rest of this request.
func (s *Server) withPass(ctx context.Context) context.Context {
	if s.Passthrough == "" {
		return ctx
	}
	if _, ok := ctx.Value(passKey{}).(*passLookup); ok {
		return ctx
	}
	ts, err := s.lookupPass(ctx)
	return context.WithValue(ctx, passKey{}, &passLookup{tools: ts, err: err})
}

// passTools is the pass-through upstream's tool list, or nil when the mode is
// off. An upstream that cannot be listed is an error, not an empty list: an
// empty list would hide its tools, and a client calling one would be told it
// does not exist when it is the server that is not answering.
func (s *Server) passTools(ctx context.Context) ([]Tool, error) {
	if s.Passthrough == "" {
		return nil, nil
	}
	if l, ok := ctx.Value(passKey{}).(*passLookup); ok {
		return l.tools, l.err
	}
	return s.lookupPass(ctx)
}

func (s *Server) lookupPass(ctx context.Context) ([]Tool, error) {
	pb, ok := s.backend.(PassthroughBackend)
	if !ok {
		return nil, fmt.Errorf("pass-through %q: this backend cannot list an upstream's tools", s.Passthrough)
	}
	ts, err := pb.UpstreamTools(ctx, s.Passthrough)
	if err != nil {
		return nil, fmt.Errorf("pass-through upstream %q is not answering: %w", s.Passthrough, err)
	}
	return ts, nil
}

// surface is what tools/list offers: the pass-through upstream's tools first,
// then every gateway tool whose name the upstream did not take.
func (s *Server) surface(ctx context.Context) ([]Tool, error) {
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

// isPassTool reports whether name is served by the pass-through upstream.
func (s *Server) isPassTool(ctx context.Context, name string) (bool, error) {
	pass, err := s.passTools(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range pass {
		if t.Name == name {
			return true, nil
		}
	}
	return false, nil
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
	if err != nil || pass || s.Passthrough == "" {
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
// whether name is the upstream's. Lets a Backend reuse the lookup the protocol
// layer made instead of repeating it.
func PassToolOf(ctx context.Context, name string) (isPass, known bool) {
	l, ok := ctx.Value(passKey{}).(*passLookup)
	if !ok || l.err != nil {
		return false, false
	}
	for _, t := range l.tools {
		if t.Name == name {
			return true, true
		}
	}
	return false, true
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
