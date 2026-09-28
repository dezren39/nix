// Package mcpclient is a minimal Model Context Protocol client.
//
// It implements only what a code-mode host needs: initialize, tools/list,
// tools/call, resources/list, resources/read and ping. Requests are
// id-multiplexed, so a single connection serves many concurrent callers.
package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocolVersion is the MCP revision mcpx negotiates.
const ProtocolVersion = "2025-06-18"

// Transport moves JSON-RPC frames to and from a server.
type Transport interface {
	// Send writes one JSON-RPC message.
	Send(ctx context.Context, msg []byte) error
	// Recv returns the next JSON-RPC message, blocking until one arrives.
	Recv() ([]byte, error)
	// Close shuts the transport down.
	Close() error
	// Info describes the transport for diagnostics.
	Info() string
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("mcp error %d: %s (%s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message)
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
}

// Client is a connected MCP session.
type Client struct {
	t      Transport
	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan *rpcResponse
	closed  bool
	closeCh chan struct{}
	recvErr error

	ServerInfo   ServerInfo
	Capabilities map[string]json.RawMessage
	// Instructions is the free-text guidance a server returns from
	// initialize. Servers use it to explain conventions their schemas cannot:
	// chrome-devtools-mcp, for instance, describes how page ids are obtained.
	Instructions string
}

// ServerInfo is the identity a server reports during initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Title   string `json:"title,omitempty"`
}

type initResult struct {
	ProtocolVersion string                     `json:"protocolVersion"`
	ServerInfo      ServerInfo                 `json:"serverInfo"`
	Capabilities    map[string]json.RawMessage `json:"capabilities"`
	Instructions    string                     `json:"instructions,omitempty"`
}

// Tool is one entry from tools/list.
type Tool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type toolsListResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// Resource is one entry from resources/list.
type Resource struct {
	URI         string `json:"uri,omitempty"`
	URITemplate string `json:"uriTemplate,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type resourcesListResult struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

type resourceTemplatesListResult struct {
	ResourceTemplates []Resource `json:"resourceTemplates"`
	NextCursor        string     `json:"nextCursor,omitempty"`
}

// New starts the receive loop and performs the MCP initialize handshake.
func New(ctx context.Context, t Transport, clientName, clientVersion string) (*Client, error) {
	c := &Client{
		t:       t,
		pending: map[int64]chan *rpcResponse{},
		closeCh: make(chan struct{}),
	}
	go c.recvLoop()

	params, _ := json.Marshal(map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	})
	var ir initResult
	if err := c.call(ctx, "initialize", params, &ir); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	c.ServerInfo = ir.ServerInfo
	c.Capabilities = ir.Capabilities
	c.Instructions = ir.Instructions

	if err := c.notify(ctx, "notifications/initialized", json.RawMessage(`{}`)); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialized notification: %w", err)
	}
	return c, nil
}

// Supports reports whether the server advertised a capability.
func (c *Client) Supports(cap string) bool {
	_, ok := c.Capabilities[cap]
	return ok
}

func (c *Client) recvLoop() {
	for {
		raw, err := c.t.Recv()
		if err != nil {
			c.fail(err)
			return
		}
		var resp rpcResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			continue // ignore malformed frames rather than killing the session
		}
		if resp.ID == nil {
			continue // server notification; mcpx does not subscribe to any
		}
		c.mu.Lock()
		ch, ok := c.pending[*resp.ID]
		if ok {
			delete(c.pending, *resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- &resp
		}
	}
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.recvErr = err
	pending := c.pending
	c.pending = map[int64]chan *rpcResponse{}
	close(c.closeCh)
	c.mu.Unlock()

	for _, ch := range pending {
		ch <- &rpcResponse{Error: &rpcError{Code: -32000, Message: "connection closed: " + err.Error()}}
	}
}

// Done is closed when the session dies.
func (c *Client) Done() <-chan struct{} { return c.closeCh }

// Err returns the error that terminated the session, if any.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recvErr
}

// Alive reports whether the session is still usable.
func (c *Client) Alive() bool {
	select {
	case <-c.closeCh:
		return false
	default:
		return true
	}
}

func (c *Client) notify(ctx context.Context, method string, params json.RawMessage) error {
	b, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	return c.t.Send(ctx, b)
}

func (c *Client) call(ctx context.Context, method string, params json.RawMessage, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan *rpcResponse, 1)

	c.mu.Lock()
	if c.closed {
		err := c.recvErr
		c.mu.Unlock()
		if err == nil {
			err = errors.New("client closed")
		}
		return err
	}
	c.pending[id] = ch
	c.mu.Unlock()

	b, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return err
	}
	if err := c.t.Send(ctx, b); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		// Best-effort cancellation so the server can stop work.
		cp, _ := json.Marshal(map[string]any{"requestId": id, "reason": "timeout"})
		_ = c.notify(context.Background(), "notifications/cancelled", cp)
		return ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
}

// Ping issues an MCP ping, used as a liveness probe.
func (c *Client) Ping(ctx context.Context) error {
	return c.call(ctx, "ping", json.RawMessage(`{}`), nil)
}

// ListTools returns every tool, following pagination cursors.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	for i := 0; i < 100; i++ {
		params := json.RawMessage(`{}`)
		if cursor != "" {
			params, _ = json.Marshal(map[string]string{"cursor": cursor})
		}
		var res toolsListResult
		if err := c.call(ctx, "tools/list", params, &res); err != nil {
			return all, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// ListResources returns static resources plus resource templates.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var all []Resource
	cursor := ""
	for i := 0; i < 100; i++ {
		params := json.RawMessage(`{}`)
		if cursor != "" {
			params, _ = json.Marshal(map[string]string{"cursor": cursor})
		}
		var res resourcesListResult
		if err := c.call(ctx, "resources/list", params, &res); err != nil {
			return all, err
		}
		all = append(all, res.Resources...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	var tres resourceTemplatesListResult
	if err := c.call(ctx, "resources/templates/list", json.RawMessage(`{}`), &tres); err == nil {
		all = append(all, tres.ResourceTemplates...)
	}
	return all, nil
}

// CallTool invokes a tool and returns the raw CallToolResult.
func (c *Client) CallTool(ctx context.Context, name string, args any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.call(ctx, "tools/call", params, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ReadResource reads a resource URI.
func (c *Client) ReadResource(ctx context.Context, uri string) (json.RawMessage, error) {
	params, _ := json.Marshal(map[string]string{"uri": uri})
	var raw json.RawMessage
	if err := c.call(ctx, "resources/read", params, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Close terminates the session.
func (c *Client) Close() error {
	c.mu.Lock()
	already := c.closed
	if !already {
		c.closed = true
		c.recvErr = errors.New("closed by client")
		close(c.closeCh)
	}
	c.mu.Unlock()
	return c.t.Close()
}

// CallTimeout is a convenience wrapper applying a deadline.
func (c *Client) CallTimeout(parent context.Context, d time.Duration, name string, args any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	return c.CallTool(ctx, name, args)
}
