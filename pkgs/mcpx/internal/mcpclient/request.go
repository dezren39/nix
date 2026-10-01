package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Request performs any MCP method and returns the raw result.
//
// The typed helpers above cover what a code-mode host needs, and that set is
// deliberately small. It is not the set the protocol defines: completion,
// logging levels, a method an extension adds, a method a server implements
// beyond its negotiated revision -- none of those had a way through, and the
// absence surfaced as "mcpx cannot do that" rather than "mcpx never asked".
//
// Era handling comes along for free, because this is the same call path the
// typed helpers use: a modern connection stamps _meta and resolves
// input_required; a legacy one sends the params as given.
func (c *Client) Request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if method == "" {
		return nil, fmt.Errorf("a request needs a method")
	}
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	var raw json.RawMessage
	if err := c.call(ctx, method, params, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Notify sends a notification, which has no reply by definition.
func (c *Client) Notify(ctx context.Context, method string, params json.RawMessage) error {
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	return c.notify(ctx, method, params)
}

// Complete asks the server what an argument's value could be.
//
// Returns ok=false when the server never declared `completions`, which is an
// absence rather than a failure: the caller has something cached to fall back
// on and needs to know which of the two it is showing. Asking anyway would
// earn a method-not-found from a well-behaved server and an unpredictable
// answer from the rest.
func (c *Client) Complete(ctx context.Context, params json.RawMessage) (json.RawMessage, bool, error) {
	// 2024-11-05 defined completion/complete with no capability to gate
	// it, so a server of that era is asked, and its method-not-found read
	// as the same absence a later server signals by not declaring.
	legacy := c.Negotiated == "2024-11-05"
	if !legacy && !c.Supports("completions") {
		return nil, false, nil
	}
	raw, err := c.Request(ctx, "completion/complete", params)
	if err != nil {
		var re *rpcError
		if legacy && !c.Supports("completions") && errors.As(err, &re) && re.Code == -32601 {
			return nil, false, nil
		}
		return nil, true, err
	}
	return raw, true, nil
}
