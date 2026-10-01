package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/dezren39/mcpx/internal/api"
)

// OpCaller performs one /v1 request on behalf of a tool.
//
// An interface-shaped function rather than a client, so this package still
// knows nothing about the daemon: the protocol does not depend on the thing
// it fronts, and a test can answer with a fake.
type OpCaller func(ctx context.Context, op api.Op, path string, body []byte) (string, error)

// OpTools turns the declared /v1 operations into MCP tools.
//
// The principle behind it: anything the CLI can do can be done from a
// plugin, from /v1, and from MCP. A hand-written tool per endpoint would
// satisfy that on the day it was written and fail quietly on the day
// somebody added a route, so the tools are derived from the same table the
// routes are and a parity test holds the two together.
//
// Privileged operations are included rather than hidden. A host that can
// reach the socket can already stop the daemon; pretending otherwise would
// buy no safety and would cost the agent the ability to restart a server
// that has wedged. What they get instead is a description that says so and
// annotations a client can scope on.
func OpTools(call OpCaller) []Extra {
	var out []Extra
	for _, op := range api.Ops() {
		if !op.Generated() {
			continue
		}
		op := op
		ann, _ := json.Marshal(op.Annotations())
		out = append(out, Extra{
			Tool: Tool{
				Name:        op.ToolName(),
				Description: op.ToolDescription(),
				InputSchema: schema(string(op.InputSchema())),
				Annotations: ann,
			},
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
				args := map[string]any{}
				if len(raw) > 0 {
					if err := json.Unmarshal(raw, &args); err != nil {
						return "", err
					}
				}
				path, body, err := op.Request(args)
				if err != nil {
					return "", err
				}
				return call(ctx, op, path, body)
			},
		})
	}
	return out
}

// InvokeTool runs one tool by name and returns its text.
//
// The plain-POST projection of the MCP surface goes through here rather than
// through a copy of the dispatch, so a tool cannot behave one way for an
// agent and another for curl.
func (s *Server) InvokeTool(ctx context.Context, tool string, args json.RawMessage) (string, error) {
	return s.invoke(s.withPass(ctx), tool, args)
}
