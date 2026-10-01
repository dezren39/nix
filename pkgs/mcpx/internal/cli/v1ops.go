package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dezren39/mcpx/internal/api"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// Do performs one declared /v1 request and returns the raw response body.
//
// The generic escape hatch the proxy tools need. It goes through the same
// `do` every typed method uses, so a proxied call cannot reach a daemon the
// typed ones cannot, or skip the error unwrapping they get.
func (c *Client) Do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var payload any
	if len(body) > 0 && method != http.MethodGet {
		payload = json.RawMessage(body)
	}
	return c.do(ctx, method, path, payload)
}

// v1OpTools exposes every non-streaming /v1 operation as an MCP tool.
//
// The daemon is started first for the same reason every other command starts
// it: a tool that reports "no daemon" the first time it is called, and works
// the second, is worse than one that waits.
func (a *App) v1OpTools() []mcpserver.Extra {
	return mcpserver.OpTools(func(ctx context.Context, op api.Op, path string, body []byte) (string, error) {
		c, err := a.ensure(ctx)
		if err != nil {
			return "", err
		}
		raw, err := c.DoOp(ctx, op, path, body)
		if err != nil {
			return "", fmt.Errorf("%s %s: %w", op.Method, path, err)
		}
		text := strings.TrimRight(string(raw), "\n")
		if text == "" {
			return "(the daemon answered with an empty body)", nil
		}
		if op.Text {
			return text, nil
		}
		// Re-indented, because these bodies are read by a model and a single
		// line of minified JSON costs more tokens to understand than it saves
		// to send.
		var doc any
		if json.Unmarshal(raw, &doc) != nil {
			return text, nil
		}
		pretty, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return text, nil
		}
		return string(pretty), nil
	})
}
