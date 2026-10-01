package mcpserver

import (
	"context"
	"encoding/json"
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

// passTools is the pass-through upstream's tool list, or nil when the mode is
// off or the upstream cannot be listed.
func (s *Server) passTools(ctx context.Context) []Tool {
	if s.Passthrough == "" {
		return nil
	}
	pb, ok := s.backend.(PassthroughBackend)
	if !ok {
		return nil
	}
	ts, err := pb.UpstreamTools(ctx, s.Passthrough)
	if err != nil {
		return nil
	}
	return ts
}

// surface is what tools/list offers: the pass-through upstream's tools first,
// then every gateway tool whose name the upstream did not take.
func (s *Server) surface(ctx context.Context) []Tool {
	pass := s.passTools(ctx)
	if len(pass) == 0 {
		return s.Tools()
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
	return out
}

// isPassTool reports whether name is served by the pass-through upstream.
func (s *Server) isPassTool(ctx context.Context, name string) bool {
	for _, t := range s.passTools(ctx) {
		if t.Name == name {
			return true
		}
	}
	return false
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
