package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/dezren39/mcpx/internal/mcpheaders"
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
		h["Mcp-Name"] = mcpheaders.Encode(*name)
	}
	return h
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
