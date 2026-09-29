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
	"strings"

	"github.com/dezren39/mcpx/internal/defaults"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocolVersion is the MCP revision mcpx negotiates.
const ProtocolVersion = "2025-11-25"

// ModernVersions are the per-request-metadata revisions mcpx can speak,
// newest first.
var ModernVersions = []string{"2026-07-28"}

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
	// Era is which protocol generation this connection settled on.
	Era Era
	// Negotiated is the version actually in use.
	Negotiated string
	// onElicit answers server-initiated requests.
	onElicit ElicitHandler
	// notif holds notification handlers.
	notif Notifications
	// roots are the directories servers may work within.
	roots []Root
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

// Era is which protocol generation a server speaks.
type Era string

const (
	// EraLegacy establishes a session with an initialize handshake.
	// Everything published today.
	EraLegacy Era = "legacy"
	// EraModern carries the version on every request and has no handshake.
	EraModern Era = "modern"
)

// Preference controls which era to try first.
type Preference string

const (
	// PreferLegacy tries initialize first. The right default today: nearly
	// every server in existence is legacy, and probing modern first costs a
	// round trip on every one of them.
	PreferLegacy Preference = "legacy"
	// PreferModern probes server/discover first.
	PreferModern Preference = "modern"
	// ForceLegacy and ForceModern skip the fallback, for a server known to
	// be one or the other, or to diagnose which it is.
	ForceLegacy Preference = "force-legacy"
	ForceModern Preference = "force-modern"
)

// New connects, discovering which era the server speaks.
func New(ctx context.Context, t Transport, clientName, clientVersion string) (*Client, error) {
	return NewWithPreference(ctx, t, clientName, clientVersion, PreferLegacy)
}

// NewWithPreference connects, trying the given era first.
//
// The fallback is what makes mcpx dual-era. A modern client against a legacy
// server fails, and a legacy client against a modern server fails; only
// something that can do both reaches the whole ecosystem.
//
// The probe order is a real trade. Legacy first costs a modern server one
// wasted initialize; modern first costs every legacy server -- which is to
// say almost all of them -- a wasted discover. Legacy first is correct today
// and will stop being correct, which is why it is configurable rather than
// decided.
func NewWithPreference(ctx context.Context, t Transport, clientName, clientVersion string, pref Preference) (*Client, error) {
	c := &Client{
		t:       t,
		pending: map[int64]chan *rpcResponse{},
		closeCh: make(chan struct{}),
	}
	go c.recvLoop()

	try := func(era Era) error {
		switch era {
		case EraModern:
			return c.discoverModern(ctx)
		default:
			return c.initializeLegacy(ctx, clientName, clientVersion)
		}
	}

	var first, second Era
	switch pref {
	case PreferModern:
		first, second = EraModern, EraLegacy
	case ForceModern:
		first, second = EraModern, ""
	case ForceLegacy:
		first, second = EraLegacy, ""
	default:
		first, second = EraLegacy, EraModern
	}

	err := try(first)
	if err == nil {
		c.Era = first
		return c, nil
	}
	if second == "" {
		c.Close()
		return nil, err
	}
	// A recognised modern error identifies a modern server, so there is
	// nothing to fall back to -- the version is wrong, not the era.
	if isVersionError(err) {
		c.Close()
		return nil, err
	}
	if ferr := try(second); ferr != nil {
		c.Close()
		// The first error is the one worth reporting: it came from the era
		// this server most likely is.
		return nil, fmt.Errorf("%s handshake failed (%w); %s also failed (%v)",
			first, err, second, ferr)
	}
	c.Era = second
	return c, nil
}

func (c *Client) initializeLegacy(ctx context.Context, clientName, clientVersion string) error {
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": ProtocolVersion,
		// Declared only where mcpx can actually deliver. Claiming a
		// capability it cannot serve invites a server to use it and get
		// silence, which is worse than not offering it.
		"capabilities": map[string]any{
			"elicitation": map[string]any{},
			"roots":       map[string]any{"listChanged": false},
		},
		"clientInfo": map[string]any{"name": clientName, "version": clientVersion},
	})
	var ir initResult
	if err := c.call(ctx, "initialize", params, &ir); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	c.ServerInfo = ir.ServerInfo
	c.Capabilities = ir.Capabilities
	c.Instructions = ir.Instructions
	c.Negotiated = ir.ProtocolVersion

	if err := c.notify(ctx, "notifications/initialized", json.RawMessage(`{}`)); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}
	return nil
}

type discoverResult struct {
	ProtocolVersions []string                   `json:"protocolVersions"`
	ServerInfo       ServerInfo                 `json:"serverInfo"`
	Capabilities     map[string]json.RawMessage `json:"capabilities"`
	Instructions     string                     `json:"instructions"`
}

func (c *Client) discoverModern(ctx context.Context) error {
	var dr discoverResult
	if err := c.call(ctx, "server/discover", json.RawMessage(`{}`), &dr); err != nil {
		return fmt.Errorf("server/discover: %w", err)
	}
	// Pick the newest version both sides implement, rather than assuming the
	// first one listed is acceptable.
	chosen := ""
	for _, want := range ModernVersions {
		for _, have := range dr.ProtocolVersions {
			if want == have {
				chosen = want
				break
			}
		}
		if chosen != "" {
			break
		}
	}
	if chosen == "" {
		return fmt.Errorf("no shared protocol version; the server offers %v",
			dr.ProtocolVersions)
	}
	c.ServerInfo = dr.ServerInfo
	c.Capabilities = dr.Capabilities
	c.Instructions = dr.Instructions
	c.Negotiated = chosen
	return nil
}

// isVersionError reports an UnsupportedProtocolVersionError, which identifies
// a modern server whatever else went wrong.
func isVersionError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "-32022")
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
		// A server-initiated request has an id AND a method. Matching only
		// on the id -- which is what this did -- made such a frame look like
		// a reply to nothing and dropped it, so the server waited until the
		// call timed out and mcpx reported a timeout. True, useless, and
		// pointing at the wrong thing.
		var probe struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.Method != "" {
			if probe.ID != nil {
				c.handleServerRequest(*probe.ID, probe.Method, probe.Params)
			} else {
				c.handleNotification(probe.Method, probe.Params)
			}
			continue
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

// OnElicit is called when a server asks a question. Nil means mcpx answers
// on the server's behalf, which it must do rather than ignore: a server that
// asks into silence waits until the call times out.
type ElicitHandler func(ctx context.Context, method string, params json.RawMessage) (any, error)

// SetElicitHandler installs the handler for server-initiated requests.
func (c *Client) SetElicitHandler(h ElicitHandler) {
	c.mu.Lock()
	c.onElicit = h
	c.mu.Unlock()
}

// handleServerRequest answers a request the server sent to us.
//
// Always answers. The alternative -- dropping what we do not understand --
// is what the old code did by accident, and it is indistinguishable from a
// hung server.
func (c *Client) handleServerRequest(id int64, method string, params json.RawMessage) {
	c.mu.Lock()
	h := c.onElicit
	c.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaults.ElicitHandlerTimeout)
		defer cancel()

		var result any
		var rpcErr *rpcError

		switch {
		case h != nil:
			out, err := h(ctx, method, params)
			if err != nil {
				rpcErr = &rpcError{Code: -32603, Message: err.Error()}
			} else {
				result = out
			}
		case method == "roots/list":
			c.mu.Lock()
			roots := append([]Root(nil), c.roots...)
			c.mu.Unlock()
			if roots == nil {
				roots = []Root{}
			}
			result = map[string]any{"roots": roots}
		case method == "elicitation/create":
			// Cancel, not decline. Nobody was asked, so nobody said no.
			result = map[string]any{"action": "cancel"}
		default:
			rpcErr = &rpcError{Code: -32601, Message: "mcpx does not implement " + method}
		}

		reply := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			reply["error"] = rpcErr
		} else {
			reply["result"] = result
		}
		b, err := json.Marshal(reply)
		if err != nil {
			return
		}
		sctx, scancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer scancel()
		_ = c.t.Send(sctx, b)
	}()
}

// ServerMessage is a log line a server sent us.
//
// Servers emit these to explain what they are doing, and mcpx dropped every
// one. A server that logs "retrying against the replica" is telling you
// exactly why a call was slow, and losing it means diagnosing from the
// outside what was explained from the inside.
type ServerMessage struct {
	Level  string          `json:"level"`
	Logger string          `json:"logger,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Progress is an update on a long operation.
type Progress struct {
	Token    any     `json:"progressToken"`
	Progress float64 `json:"progress"`
	Total    float64 `json:"total,omitempty"`
	Message  string  `json:"message,omitempty"`
}

// Notifications a caller may subscribe to.
type Notifications struct {
	// OnMessage receives a server's log lines.
	OnMessage func(ServerMessage)
	// OnProgress receives progress on a long call.
	OnProgress func(Progress)
	// OnListChanged fires when the server says its tools, resources or
	// prompts have changed. The kind is "tools", "resources" or "prompts".
	OnListChanged func(kind string)
}

// Subscribe installs notification handlers.
func (c *Client) Subscribe(n Notifications) {
	c.mu.Lock()
	c.notif = n
	c.mu.Unlock()
}

// handleNotification routes a server-initiated notification.
//
// Every one of these was previously discarded. They are the server
// explaining itself, and throwing that away means every diagnosis starts
// from the outside.
func (c *Client) handleNotification(method string, params json.RawMessage) {
	c.mu.Lock()
	n := c.notif
	c.mu.Unlock()

	switch method {
	case "notifications/message":
		if n.OnMessage == nil {
			return
		}
		var m ServerMessage
		if json.Unmarshal(params, &m) == nil {
			n.OnMessage(m)
		}
	case "notifications/progress":
		if n.OnProgress == nil {
			return
		}
		var p Progress
		if json.Unmarshal(params, &p) == nil {
			n.OnProgress(p)
		}
	case "notifications/tools/list_changed":
		c.invalidate("tools", n)
	case "notifications/resources/list_changed":
		c.invalidate("resources", n)
	case "notifications/prompts/list_changed":
		c.invalidate("prompts", n)
	}
}

func (c *Client) invalidate(kind string, n Notifications) {
	if n.OnListChanged != nil {
		n.OnListChanged(kind)
	}
}

// SetLogLevel asks the server to send messages at or above a level.
//
// Servers send nothing until asked, so a client that never calls this sees
// no log messages and concludes the server does not emit any.
func (c *Client) SetLogLevel(ctx context.Context, level string) error {
	if !c.Supports("logging") {
		return nil
	}
	params, _ := json.Marshal(map[string]string{"level": level})
	var out json.RawMessage
	return c.call(ctx, "logging/setLevel", params, &out)
}

// Root is a directory a server may work within.
type Root struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

// SetRoots declares the directories servers may operate on.
//
// Without this a filesystem server has no idea what it is allowed to touch
// and must be told through its own configuration, separately, in a second
// place that drifts from the first.
func (c *Client) SetRoots(roots []Root) {
	c.mu.Lock()
	c.roots = roots
	c.mu.Unlock()
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

// Prompt is a reusable template a server offers.
//
// Prompts are the part of MCP that is not tools: a server saying "here is the
// wording that works for this" rather than "here is a function". A server
// that publishes a good one has encoded expertise that would otherwise have
// to be rediscovered by whoever writes the request.
type Prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument is one substitution a prompt takes.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type promptsListResult struct {
	Prompts    []Prompt `json:"prompts"`
	NextCursor string   `json:"nextCursor"`
}

// ListPrompts returns every prompt a server offers.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var all []Prompt
	cursor := ""
	for i := 0; i < 100; i++ {
		params := json.RawMessage(`{}`)
		if cursor != "" {
			params, _ = json.Marshal(map[string]string{"cursor": cursor})
		}
		var res promptsListResult
		if err := c.call(ctx, "prompts/list", params, &res); err != nil {
			// A server without prompts answers method-not-found, which is an
			// absence rather than a failure. Treating it as an error would
			// make every listing fail on the majority of servers.
			return all, nil
		}
		all = append(all, res.Prompts...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// GetPrompt renders one prompt with its arguments filled in.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (json.RawMessage, error) {
	if args == nil {
		args = map[string]string{}
	}
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.call(ctx, "prompts/get", params, &raw); err != nil {
		return nil, err
	}
	return raw, nil
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
