// Package mcpserver exposes mcpx itself over the Model Context Protocol.
//
// The inversion is the point. mcpx exists so an agent can reach MCP servers
// without their schemas entering its context: it runs them, generates a typed
// client, and the agent writes a script. But a host that already speaks MCP
// and nothing else -- a different editor, a hosted agent, something that is
// not opencode -- cannot use any of that.
//
// So mcpx speaks MCP too. A host connects to one server and gets a handful of
// tools that reach every server mcpx knows about, with the schemas still on
// this side of the wire. `mcpx_catalog` describes what exists within a token
// budget, `mcpx_exec` runs a script, `mcpx_call` reaches one tool. Ten tools
// instead of three hundred.
package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// Backend is what the server exposes. Defined here rather than taken from the
// CLI so this package can be driven by a fake, and so the dependency runs one
// way: the protocol does not know about the daemon.
type Backend interface {
	Namespaces(ctx context.Context) (string, error)
	Catalog(ctx context.Context, budget int, bias string) (string, error)
	Types(ctx context.Context, namespaces []string) (string, error)
	Search(ctx context.Context, query string, limit int) (string, error)
	Call(ctx context.Context, namespace, tool string, args json.RawMessage) (string, error)
	Exec(ctx context.Context, source string, timeoutSec int) (string, error)
	Log(ctx context.Context, since, level, event string, limit int) (string, error)
	Stats(ctx context.Context, dimension string) (string, error)
	Status(ctx context.Context) (string, error)
	RegistrySearch(ctx context.Context, query string, limit int) (string, error)
	// Resources and Prompts pass through what the upstream servers offer.
	// Returning an empty list -- which mcpx did -- throws away everything a
	// server published that is not a tool.
	Resources(ctx context.Context) ([]ResourceRef, error)
	Prompts(ctx context.Context) ([]PromptRef, error)
	ReadResource(ctx context.Context, uri string) (string, string, error)
	GetPrompt(ctx context.Context, name string, args map[string]string) (string, error)
	ResourceTemplates(ctx context.Context) ([]ResourceRef, error)
}

// Notifier is where the server hears about things worth pushing.
//
// Defined as an interface so the protocol package does not depend on the
// daemon's event bus, and so a test can feed it directly.
type Notifier interface {
	// Listen delivers MCP notifications matching the filter until the
	// context ends. method and params are ready to send.
	Listen(ctx context.Context, f ListenFilter, send func(method string, params any))
}

// ListenFilter is what a client opted in to.
//
// Exactly the fields the 2026-07-28 SubscriptionFilter defines, and no
// more: the specification says a server MUST NOT send notification types the
// client did not request, so inventing extras here would be a violation.
type ListenFilter struct {
	ToolsListChanged      bool     `json:"toolsListChanged,omitempty"`
	PromptsListChanged    bool     `json:"promptsListChanged,omitempty"`
	ResourcesListChanged  bool     `json:"resourcesListChanged,omitempty"`
	ResourceSubscriptions []string `json:"resourceSubscriptions,omitempty"`
}

// ResourceRef is one resource a server offers.
type ResourceRef struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// PromptRef is one prompt a server offers.
type PromptRef struct {
	Name        string      `json:"name"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Arguments   []PromptArg `json:"arguments,omitempty"`
}

// PromptArg is one substitution a prompt takes.
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Extra is a tool contributed from outside the fixed set.
//
// Adapted command-line programs arrive this way, so an adapter is a first
// class server rather than something reachable only through mcpx_exec. A host
// that wants git as a tool should get git as a tool.
type Extra struct {
	Tool Tool
	Call func(ctx context.Context, args json.RawMessage) (string, error)
}

// Server answers MCP requests.
type Server struct {
	backend Backend
	name    string
	version string
	extras  []Extra

	// PageSize caps how many items a list reply carries.
	PageSize int

	// MaxCompletions caps a completion/complete reply. Zero uses the
	// built-in default.
	MaxCompletions int

	// Timing is the ask loop's policy. A zero field means the built-in
	// default; the whole struct zero is what a test that does not care
	// should be able to leave alone.
	Timing Timing
	// OnCancel is called when a client cancels a request.
	OnCancel func(id, reason string)

	// Origins decides which browser origins the HTTP transport serves.
	Origins OriginPolicy

	mu sync.Mutex

	// Notify is where pushed notifications come from. Nil means mcpx never
	// pushes, and declares so.
	Notify Notifier

	// Ask runs a request that may ask questions back. Nil means mcpx
	// answers every upstream question through the daemon's broker, which is
	// what it did before any client could answer one inline.
	Ask Asker

	// def is the connection the in-process entry points use. A transport
	// that serves many clients makes one Conn apiece instead.
	def     *Conn
	defOnce sync.Once

	// taskStore holds background requests.
	taskStore *taskStore

	// sessions are the Streamable HTTP connections, keyed by the id mcpx
	// issued at initialize.
	sessMu   sync.Mutex
	sessions map[string]*Conn

	// signer mints the opaque requestState a modern client resumes with.
	stateOnce sync.Once
	signer    *stateSigner
}

// conn returns the connection the in-process entry points share.
func (s *Server) conn() *Conn {
	// Given an identity even though no transport issued one: the default
	// connection outlives every request on it, so a requestState bound to
	// it is safe, and stdio has no session header to take one from.
	s.defOnce.Do(func() { s.def = s.newConn(newSessionID(), nil) })
	return s.def
}

// New builds a server.
func New(b Backend, name, version string) *Server {
	return &Server{backend: b, name: name, version: version}
}

// WithExtras returns a server that also offers these tools.
//
// A new value rather than a mutation, so a caller cannot change the surface
// of a server another goroutine is already answering with.
func (s *Server) WithExtras(extras []Extra) *Server {
	return &Server{
		backend: s.backend, name: s.name, version: s.version,
		PageSize: s.PageSize, MaxCompletions: s.MaxCompletions,
		Timing: s.Timing, OnCancel: s.OnCancel, Origins: s.Origins,
		// Notify comes along. Dropping it silently turned off every push
		// capability the moment a single extra tool existed, and a client
		// cannot detect a server that declared nothing.
		Notify: s.Notify,
		Ask:    s.Ask,
		extras: append(append([]Extra(nil), s.extras...), extras...),
	}
}

// ---- protocol types ----

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error codes from the JSON-RPC specification. Using the standard ones means
// a client's existing error handling works without being taught anything.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
	// codeUnsupportedVersion is defined by the specification, not by
	// JSON-RPC, and carries the supported list in its data.
	codeUnsupportedVersion = -32022
)

// unsupportedVersion is the answer to a version mcpx does not implement.
//
// The list matters: a client has no other way to discover what would work,
// and the specification says it SHOULD retry with something from it.
func supportedFor(legacyOnly bool) []string {
	if legacyOnly {
		return LegacySupported()
	}
	return Supported
}

func unsupportedVersion(id json.RawMessage, params json.RawMessage, legacyOnly bool) *response {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{
		Code:    codeUnsupportedVersion,
		Message: "Unsupported protocol version",
		Data: map[string]any{
			"supported": supportedFor(legacyOnly),
			"requested": p.ProtocolVersion,
		},
	}}
}

// Tool is one exposed function.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	// Annotations are the specification's behavioural hints -- readOnlyHint,
	// destructiveHint -- which is how a client decides whether a tool may be
	// run without asking. Omitted where mcpx has nothing to declare.
	Annotations json.RawMessage `json:"annotations,omitempty"`
}

// Tools is the surface.
//
// Deliberately small. The whole reason mcpx exists is that three hundred tool
// schemas in a context window crowds out the work; exposing three hundred
// again over MCP would rebuild the problem with extra steps. These ten reach
// all of them, and the schemas stay here.
func (s *Server) Tools() []Tool {
	base := []Tool{
		{
			Name: "mcpx_namespaces",
			Description: "List every MCP server mcpx knows about, with tool counts. " +
				"Start here: it is small, it starts nothing, and it tells you what " +
				"else is worth asking for.",
			InputSchema: schema(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		{
			Name: "mcpx_catalog",
			Description: "Every namespace with as many tool signatures as fit a token " +
				"budget. Round-robins across servers so a large one cannot crowd out " +
				"a small one. Use this when you do not yet know which server you want.",
			InputSchema: schema(`{"type":"object","properties":{
				"budget":{"type":"integer","description":"approximate token ceiling (default 2000)"},
				"bias":{"type":"string","description":"words that pull matching tools toward the front"}
			},"additionalProperties":false}`),
		},
		{
			Name: "mcpx_types",
			Description: "Full TypeScript signatures for named namespaces, including each " +
				"server's own guidance. Ask for this once you know which server you " +
				"want; it is large, which is why it is not the default.",
			InputSchema: schema(`{"type":"object","properties":{
				"namespaces":{"type":"array","items":{"type":"string"},
					"description":"namespace names, or namespace.tool for one tool"}
			},"required":["namespaces"],"additionalProperties":false}`),
		},
		{
			Name: "mcpx_search",
			Description: "Find tools by name and description across every server. " +
				"Cheaper than the catalog when you already know roughly what you want.",
			InputSchema: schema(`{"type":"object","properties":{
				"query":{"type":"string"},
				"limit":{"type":"integer","description":"default 20"}
			},"required":["query"],"additionalProperties":false}`),
		},
		{
			Name: "mcpx_call",
			Description: "Call one tool on one server. Use this for a single result. " +
				"When you need several calls, or want to filter a large result before " +
				"reading it, use mcpx_exec instead -- it runs on this side of the wire " +
				"and only what it prints comes back.",
			InputSchema: schema(`{"type":"object","properties":{
				"namespace":{"type":"string"},
				"tool":{"type":"string"},
				"arguments":{"type":"object","description":"the tool's arguments"}
			},"required":["namespace","tool"],"additionalProperties":false}`),
		},
		{
			Name: "mcpx_exec",
			Description: "Run TypeScript against every server at once. Tools are bound as " +
				"async functions -- await tools.<namespace>.<tool>({...}) -- and only " +
				"what you print or emit() comes back. This is the one that saves " +
				"context: filter, join and summarise here rather than reading a " +
				"megabyte of JSON into your own.",
			InputSchema: schema(`{"type":"object","properties":{
				"source":{"type":"string","description":"TypeScript; top-level await is available"},
				"timeoutSec":{"type":"integer","description":"default 120"}
			},"required":["source"],"additionalProperties":false}`),
		},
		{
			Name: "mcpx_log",
			Description: "Query mcpx's durable log: what ran, what it cost, what failed. " +
				"Use it to find out why something did not work without re-running it.",
			InputSchema: schema(`{"type":"object","properties":{
				"since":{"type":"string","description":"a duration like 15m or 2h, or an RFC3339 time"},
				"level":{"type":"string","enum":["debug","info","warn","error"]},
				"event":{"type":"string","description":"glob: server.*, mcp.call"},
				"limit":{"type":"integer","description":"default 50"}
			},"additionalProperties":false}`),
		},
		{
			Name: "mcpx_stats",
			Description: "Aggregate the log: calls, servers, errors, sessions, slowest, " +
				"volume. Answers 'what is slow' and 'what keeps failing' without " +
				"reading records one at a time.",
			InputSchema: schema(`{"type":"object","properties":{
				"dimension":{"type":"string",
					"enum":["calls","servers","errors","sessions","slowest","volume","instances"]}
			},"additionalProperties":false}`),
		},
		{
			Name: "mcpx_registry",
			Description: "Search a public registry of MCP servers that are not yet " +
				"configured here. Use it when the capability you need does not " +
				"appear in mcpx_namespaces: the answer includes how to add it.",
			InputSchema: schema(`{"type":"object","properties":{
				"query":{"type":"string","description":"one word; the registry matches names as a substring"},
				"limit":{"type":"integer","description":"default 20"}
			},"additionalProperties":false}`),
		},
		{
			Name: "mcpx_status",
			Description: "The daemon, its pools and live instances. Use it when a call " +
				"behaves oddly and you want to know whether the server is even up.",
			InputSchema: schema(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
	}
	for _, e := range s.extras {
		base = append(base, e.Tool)
	}
	return base
}

func schema(s string) json.RawMessage {
	// Compacted so the wire carries no incidental whitespace; these go out on
	// every tools/list and the saving is free.
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		// A malformed schema here is a programming error that a test catches,
		// so passing the original through keeps the server answering rather
		// than turning a typo into a dead tool.
		return json.RawMessage(s)
	}
	return json.RawMessage(buf.String())
}

// Instructions are sent at initialize, where a server explains what its
// schemas cannot.
const Instructions = `mcpx runs MCP servers and exposes them through a few tools rather than many.

Start with mcpx_namespaces. It is small and starts nothing.

For one result use mcpx_call. For anything more -- several calls, a large
result you want to filter, a join across servers -- use mcpx_exec: it runs
TypeScript next to the servers and only what it prints returns to you. That is
the difference between reading a megabyte of JSON into your context and reading
the one line you wanted.

Tools inside mcpx_exec are bound as tools.<namespace>.<tool>(args), all async,
with top-level await available. log.info() and emit() are there too.`

// Handle answers one request.
//
// A request that declared a 2026-07-28 version gets `resultType` on its
// result, which that revision makes mandatory: it is how a client tells a
// finished result from an input_required one. Legacy results are left as
// they were; the field means nothing to a client that never asked for it.
func (s *Server) Handle(ctx context.Context, req request) *response {
	return s.HandleOn(ctx, s.conn(), req)
}

// HandleOn answers one request on a named connection.
//
// The connection is what decides whether a question can be put to this
// client, which notifications it asked for, and which revision governs the
// reply. Everything that used to sit on the Server and be correct for one
// stdio client lives there now.
func (s *Server) HandleOn(ctx context.Context, c *Conn, req request) *response {
	peer := c.peerFor(req.Params)
	resp := s.handle(ctx, c, req)
	if resp == nil || resp.Error != nil {
		return resp
	}
	if peer.Modern {
		// Mandatory in 2026-07-28: it is how a client tells a finished
		// result from one still asking for input.
		resp.Result = stampComplete(resp.Result)
	}
	// Send conservatively. Everything above builds results in the newest
	// shape; this is the one place that spells them in the client's own.
	resp.Result = downgrade(resp.Result, peer.Version)
	return resp
}

// stampComplete marks a result complete unless it already says otherwise.
func stampComplete(result any) any {
	m, ok := result.(map[string]any)
	if !ok {
		b, err := json.Marshal(result)
		if err != nil || json.Unmarshal(b, &m) != nil || m == nil {
			return result
		}
	}
	if _, set := m["resultType"]; !set {
		m["resultType"] = "complete"
	}
	return m
}

func (s *Server) handle(ctx context.Context, c *Conn, req request) *response {
	reply := func(result any) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	fail := func(code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}

	// A modern request carries its version in _meta and expects no
	// handshake. Checked before dispatch so an unsupported one is refused
	// uniformly rather than by whichever handler happens to notice.
	if v := requestVersion(req.Params); v != "" && !supports(v) {
		return unsupportedVersion(req.ID, req.Params, false)
	}
	peer := c.peerFor(req.Params)

	switch req.Method {
	case "tasks/get", "tasks/list", "tasks/result", "tasks/cancel":
		return s.handleTask(ctx, req)
	case "tools/call":
		// A caller that asked for a task gets a handle now and the result
		// later, rather than holding a request open for however long the
		// tool takes. Handled here, ahead of the ordinary dispatch, by
		// running that same dispatch in the background with the task
		// request stripped off.
		if want, ttl := wantsTask(req.Params); want {
			inner := req
			inner.Params = withoutTask(req.Params)
			t := s.startTask(ttl, func(tctx context.Context) (any, *rpcError) {
				// Answered on the same connection, so a question the call
				// raises reaches the client that started it rather than
				// whichever one the server happens to call default.
				resp := s.HandleOn(tctx, c, inner)
				if resp == nil {
					return nil, &rpcError{Code: codeInternal, Message: "no result"}
				}
				if resp.Error != nil {
					return nil, resp.Error
				}
				return resp.Result, nil
			})
			return &response{JSONRPC: "2.0", ID: req.ID,
				Result: map[string]any{"task": t}}
		}
	}

	switch req.Method {
	case "initialize":
		version := negotiate(req.Params)
		if version == "" {
			return unsupportedVersion(req.ID, req.Params, true)
		}
		// What the client declared here governs everything mcpx may send it
		// for the life of the connection. A legacy server never asks again,
		// so not recording it is the same as deciding the answer is "no".
		var ip struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		}
		_ = json.Unmarshal(req.Params, &ip)
		c.SetCapabilities(ip.Capabilities, version)
		return reply(map[string]any{
			// Echo the protocol version the client asked for when it is one
			// we understand, rather than insisting on ours. A client that
			// speaks an older revision of a compatible protocol is better
			// served than refused.
			"protocolVersion": version,
			"capabilities":    s.capabilities(version, c),
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
			"instructions":    Instructions,
		})

	case "server/discover":
		// Mandatory in the modern revisions, and the probe a dual-era client
		// uses to decide which era it is talking to. Answering it is what
		// makes mcpx reachable from a modern client at all.
		return reply(map[string]any{
			"protocolVersions": Supported,
			"serverInfo":       map[string]any{"name": s.name, "version": s.version},
			"capabilities":     s.capabilities(ModernLatest, c),
			"instructions":     Instructions,
		})

	case "notifications/initialized", "initialized":
		return nil // a notification: no reply, by definition

	case "ping":
		return reply(map[string]any{})

	case "tools/list":
		// Paginated, because mcpx fronts every tool of every configured
		// server and a client with a frame limit has no other way to read
		// the list. Ignoring the cursor meant a large installation was
		// simply unreadable by such a client.
		tools, next := page(s.Tools(), req.Params, s.pageSize())
		out := map[string]any{"tools": tools}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "completion/complete":
		// Argument autocomplete. Answered from what mcpx already knows --
		// namespace names, tool names -- because a client offering
		// completion and receiving method-not-found simply shows nothing,
		// and the user concludes the feature is broken.
		return reply(map[string]any{"completion": s.complete(ctx, req.Params)})

	case "logging/setLevel":
		// Accepted, but no longer declared. The capability means "this server
		// sends log messages to the client", and mcpx sends none: there is no
		// notifications/message anywhere in this package. Declaring it was a
		// promise of a stream that does not exist. The method still answers,
		// because refusing would make a well-behaved client that asked anyway
		// treat the whole connection as degraded.
		return reply(map[string]any{})

	case "resources/templates/list":
		// mcpx consumed templates from upstream servers and never offered
		// them onward, so a parameterised resource became invisible one hop
		// down. Passed through now, namespaced like everything else.
		if s.backend == nil {
			return reply(map[string]any{"resourceTemplates": []any{}})
		}
		ts, err := s.backend.ResourceTemplates(ctx)
		if err != nil {
			return fail(codeInternal, err.Error())
		}
		if ts == nil {
			ts = []ResourceRef{}
		}
		items, next := page(ts, req.Params, s.pageSize())
		out := map[string]any{"resourceTemplates": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "resources/subscribe", "resources/unsubscribe":
		// The legacy mechanism: per-URI, per-connection. The modern revision
		// replaced it with resourceSubscriptions on subscriptions/listen,
		// which is handled below; both feed the same forwarding.
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.URI == "" {
			return fail(codeInvalidParams, "uri is required")
		}
		c.mu.Lock()
		if c.subs == nil {
			c.subs = map[string]bool{}
		}
		if req.Method == "resources/subscribe" {
			c.subs[p.URI] = true
		} else {
			delete(c.subs, p.URI)
		}
		uris := make([]string, 0, len(c.subs))
		for u := range c.subs {
			uris = append(uris, u)
		}
		c.mu.Unlock()
		s.restartListen(c, ListenFilter{ResourceSubscriptions: uris})
		return reply(map[string]any{})

	case "subscriptions/listen":
		// A long-lived stream. On stdio its result arrives only when it
		// ends, so the reply is withheld here and notifications flow in the
		// meantime; the transport sends the terminating
		// notifications/cancelled when the client closes it.
		var p struct {
			Notifications ListenFilter `json:"notifications"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(codeInvalidParams, err.Error())
		}
		if s.Notify == nil {
			return fail(codeMethodNotFound, "this mcpx pushes no notifications")
		}
		s.restartListen(c, p.Notifications)
		c.push("notifications/subscriptions/acknowledged", map[string]any{
			"subscriptionId": json.RawMessage(req.ID),
		})
		return nil

	case "notifications/cancelled":
		// A notification, so no reply. Recorded rather than ignored: a
		// client that cancels and sees work continue has no way to tell
		// whether the message arrived.
		c.cancel(req.Params)
		return nil

	case "tools/call":
		// A client that can answer a question gets the call run as a task
		// it can be interrupted, and resumed, across. One that cannot gets
		// the direct path and the broker's own routing, exactly as before.
		if s.canAsk(ctx, c, peer) {
			if resp := s.viaAsk(ctx, c, req, peer); resp != nil {
				return resp
			}
		}
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(codeInvalidParams, err.Error())
		}
		text, err := s.dispatch(ctx, p.Name, p.Arguments)
		if err != nil {
			// A tool that fails is a result with isError, not a protocol
			// error. The distinction matters: a protocol error means the
			// client did something wrong, and a client that retries the
			// wrong thing on a tool failure never converges.
			return reply(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": err.Error()}},
				"isError": true,
			})
		}
		text, blocks := decodeResult(text)
		content := []any{map[string]any{"type": "text", "text": text}}
		for _, b := range blocks {
			content = append(content, b)
		}
		return reply(map[string]any{"content": content})

	case "resources/list":
		if s.backend == nil {
			return reply(map[string]any{"resources": []any{}})
		}
		rs, err := s.backend.Resources(ctx)
		if err != nil {
			return fail(codeInternal, err.Error())
		}
		if rs == nil {
			rs = []ResourceRef{}
		}
		items, next := page(rs, req.Params, s.pageSize())
		out := map[string]any{"resources": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "resources/read":
		if s.canAsk(ctx, c, peer) {
			if resp := s.viaAsk(ctx, c, req, peer); resp != nil {
				return resp
			}
		}
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(codeInvalidParams, err.Error())
		}
		text, mime, err := s.backend.ReadResource(ctx, p.URI)
		if err != nil {
			return fail(codeInvalidParams, err.Error())
		}
		if mime == "" {
			mime = "text/plain"
		}
		return reply(map[string]any{"contents": []any{
			map[string]any{"uri": p.URI, "mimeType": mime, "text": text},
		}})

	case "prompts/list":
		if s.backend == nil {
			return reply(map[string]any{"prompts": []any{}})
		}
		ps, err := s.backend.Prompts(ctx)
		if err != nil {
			return fail(codeInternal, err.Error())
		}
		if ps == nil {
			ps = []PromptRef{}
		}
		items, next := page(ps, req.Params, s.pageSize())
		out := map[string]any{"prompts": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "prompts/get":
		if s.canAsk(ctx, c, peer) {
			if resp := s.viaAsk(ctx, c, req, peer); resp != nil {
				return resp
			}
		}
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(codeInvalidParams, err.Error())
		}
		text, err := s.backend.GetPrompt(ctx, p.Name, p.Arguments)
		if err != nil {
			return fail(codeInvalidParams, err.Error())
		}
		return reply(map[string]any{
			"messages": []any{map[string]any{
				"role":    "user",
				"content": map[string]any{"type": "text", "text": text},
			}},
		})
	}
	return fail(codeMethodNotFound, "no method "+req.Method)
}

// negotiate picks a protocol version.
// Supported are the protocol versions mcpx actually implements, newest first.
//
// Both eras. The legacy revisions negotiate once through an initialize
// handshake; 2026-07-28 carries the version on every request and has no
// handshake at all. Supporting only one would make mcpx unreachable from half
// the ecosystem, and the matrix in the specification is unforgiving about it:
// modern against legacy fails, legacy against modern fails, and only a
// dual-era implementation bridges them.
var Supported = []string{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26"}

// ModernLatest is the newest per-request-metadata revision mcpx serves.
// server/discover answers with this one's capability shape.
const ModernLatest = "2026-07-28"

// Latest is what mcpx prefers when the client expresses no opinion.
//
// The newest legacy revision rather than the newest overall, because a client
// that sent `initialize` has already told us it is legacy, and answering with
// a modern version would be answering a question it did not ask.
const Latest = "2025-11-25"

// Modern reports whether a version uses per-request metadata rather than a
// handshake.
func Modern(version string) bool { return version >= "2026-07-28" }

// withoutTask removes the task field, so the background run of a request does
// not ask to become a task again and recurse.
func withoutTask(params json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(params, &m) != nil {
		return params
	}
	delete(m, "task")
	b, err := json.Marshal(m)
	if err != nil {
		return params
	}
	return b
}

// requestVersion reads the per-request protocol version the modern revisions
// carry in _meta. Empty means the request did not declare one, which is how
// every legacy request looks.
func requestVersion(params json.RawMessage) string {
	var p struct {
		Meta map[string]any `json:"_meta"`
	}
	if json.Unmarshal(params, &p) != nil {
		return ""
	}
	v, _ := p.Meta["io.modelcontextprotocol/protocolVersion"].(string)
	return v
}

// supports reports whether mcpx implements a version.
func supports(version string) bool {
	for _, v := range Supported {
		if v == version {
			return true
		}
	}
	return false
}

// negotiate picks a version for a legacy initialize.
//
// It returns the empty string when the requested version is one mcpx does not
// implement, so the caller can answer with an UnsupportedProtocolVersionError
// rather than agreeing to something it cannot do.
//
// The previous implementation echoed whatever was asked for. A client
// requesting 2026-07-28 was told yes, and then found no server/discover and
// no per-request metadata handling. Agreeing to everything is the same as
// declaring nothing.
func negotiate(params json.RawMessage) string {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(params, &p) != nil || p.ProtocolVersion == "" {
		return Latest
	}
	// Only a legacy version can be agreed here. A client that sent
	// `initialize` is legacy by definition -- the modern revisions have no
	// handshake at all -- so agreeing to a modern version over this channel
	// would promise a protocol neither side is speaking.
	if supports(p.ProtocolVersion) && !Modern(p.ProtocolVersion) {
		return p.ProtocolVersion
	}
	return ""
}

// LegacySupported is what an initialize may negotiate.
func LegacySupported() []string {
	var out []string
	for _, v := range Supported {
		if !Modern(v) {
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) dispatch(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	arg := func(v any) error {
		if len(raw) == 0 {
			return nil
		}
		return json.Unmarshal(raw, v)
	}
	switch name {
	case "mcpx_namespaces":
		return s.backend.Namespaces(ctx)

	case "mcpx_catalog":
		var p struct {
			Budget int    `json:"budget"`
			Bias   string `json:"bias"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		return s.backend.Catalog(ctx, p.Budget, p.Bias)

	case "mcpx_types":
		var p struct {
			Namespaces []string `json:"namespaces"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		if len(p.Namespaces) == 0 {
			return "", errors.New("namespaces is required; mcpx_namespaces lists them")
		}
		return s.backend.Types(ctx, p.Namespaces)

	case "mcpx_search":
		var p struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		if p.Query == "" {
			return "", errors.New("query is required")
		}
		return s.backend.Search(ctx, p.Query, p.Limit)

	case "mcpx_call":
		var p struct {
			Namespace string          `json:"namespace"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		// A dotted name in the namespace field is what somebody will send,
		// because that is how the tools are written everywhere else.
		if p.Tool == "" {
			if ns, tool, ok := strings.Cut(p.Namespace, "."); ok {
				p.Namespace, p.Tool = ns, tool
			}
		}
		if p.Namespace == "" || p.Tool == "" {
			return "", errors.New("namespace and tool are required")
		}
		return s.backend.Call(ctx, p.Namespace, p.Tool, p.Arguments)

	case "mcpx_exec":
		var p struct {
			Source     string `json:"source"`
			TimeoutSec int    `json:"timeoutSec"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		if strings.TrimSpace(p.Source) == "" {
			return "", errors.New("source is required")
		}
		return s.backend.Exec(ctx, p.Source, p.TimeoutSec)

	case "mcpx_log":
		var p struct {
			Since string `json:"since"`
			Level string `json:"level"`
			Event string `json:"event"`
			Limit int    `json:"limit"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		return s.backend.Log(ctx, p.Since, p.Level, p.Event, p.Limit)

	case "mcpx_stats":
		var p struct {
			Dimension string `json:"dimension"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		return s.backend.Stats(ctx, p.Dimension)

	case "mcpx_registry":
		var p struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		return s.backend.RegistrySearch(ctx, p.Query, p.Limit)

	case "mcpx_status":
		return s.backend.Status(ctx)
	}
	for _, e := range s.extras {
		if e.Tool.Name == name {
			return e.Call(ctx, raw)
		}
	}
	return "", fmt.Errorf("no tool named %q", name)
}

// ServeStdio runs the server over a pipe, which is how most MCP hosts start
// one: spawn a process and talk newline-delimited JSON to it.
//
// stdio is also the only transport on which a legacy client can be asked a
// question without any session machinery: the pipe is the session, and it
// stays open for as long as the process does.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	// One writer, guarded, because replies and notifications come from many
	// goroutines and two frames interleaved mid-line is a corrupt stream the
	// client cannot recover from. json.Encoder escapes every newline inside
	// a string, so each frame is exactly one line.
	var wmu sync.Mutex
	rawEnc := json.NewEncoder(out)
	enc := lockedEncoder{mu: &wmu, enc: rawEnc}
	c := s.conn()
	c.mu.Lock()
	c.send = func(frame any) error { return enc.Encode(frame) }
	c.pushFn = func(method string, params any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}
	c.mu.Unlock()
	defer c.stopListen()
	defer s.stopListChanged(c)

	// Every request runs on its own goroutine under its own context. In
	// line, a notifications/cancelled for a running request could not even
	// be read until that request had finished, so cancellation did nothing;
	// and a request waiting on the client's answer to a question would be
	// waiting for a frame that arrives on this very loop.
	base, cancelAll := context.WithCancel(ctx)
	defer cancelAll()
	var inflight sync.WaitGroup

	sc := bufio.NewScanner(in)
	// Tool results carry whole documents, so the default 64KB line limit is
	// far too small and the failure it produces -- a truncated request --
	// looks like a malformed client.
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if isBatch([]byte(line)) {
			inflight.Add(1)
			go func(line string) {
				defer inflight.Done()
				if resp := s.stdioBatch(base, c, []byte(line)); resp != nil {
					_ = enc.Encode(resp)
				}
			}(line)
			continue
		}
		// A frame with an id and no method is the client answering something
		// mcpx asked it. Dispatching it as a request -- which is what
		// matching on method alone did -- answers the client's own answer
		// with method-not-found and leaves the question hanging.
		if id, result, rerr, ok := replyOf([]byte(line)); ok {
			c.deliver(id, result, rerr)
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(response{JSONRPC: "2.0",
				Error: &rpcError{Code: codeParse, Message: err.Error()}})
			continue
		}
		if req.JSONRPC != "" && req.JSONRPC != "2.0" {
			_ = enc.Encode(response{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: codeInvalidRequest, Message: "unsupported jsonrpc version"}})
			continue
		}
		if len(req.ID) == 0 || req.Method == "initialize" {
			// In line. A notification is ordered by nature -- a cancellation
			// has to land before whatever the client sends next -- and
			// initialize has to be answered before anything that follows
			// it, because what it settles governs every later reply.
			resp := s.HandleOn(base, c, req)
			if req.Method == "initialize" && resp != nil && resp.Error == nil {
				s.startListChanged(c)
			}
			// Never a reply to a notification, not even an error: JSON-RPC
			// forbids it, and a client would have nothing to match it to.
			if resp != nil && len(req.ID) > 0 {
				_ = enc.Encode(resp)
			}
			continue
		}
		rctx, done, cancelled := c.track(base, req.ID)
		inflight.Add(1)
		go func(req request) {
			defer inflight.Done()
			defer done()
			resp := s.HandleOn(rctx, c, req)
			if resp != nil && !cancelled() {
				_ = enc.Encode(resp)
			}
		}(req)
		if ctx.Err() != nil {
			break
		}
	}

	// The input is closed, which is how a host asks a server to stop. What
	// is already running gets a bounded chance to answer, then is cancelled,
	// so a hung call cannot keep the process alive.
	drained := make(chan struct{})
	go func() { inflight.Wait(); close(drained) }()
	drain := s.Timing.resolved().StdioDrain
	select {
	case <-drained:
	case <-time.After(drain):
		cancelAll()
		select {
		case <-drained:
		case <-time.After(drain):
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return sc.Err()
}

// stdioBatch answers one batch line. nil means nothing is sent back, which is
// the answer to a batch of notifications and responses only.
func (s *Server) stdioBatch(ctx context.Context, c *Conn, line []byte) any {
	var elems []json.RawMessage
	if err := json.Unmarshal(line, &elems); err != nil {
		return response{JSONRPC: "2.0", Error: &rpcError{Code: codeParse, Message: err.Error()}}
	}
	if len(elems) == 0 {
		return response{JSONRPC: "2.0", Error: &rpcError{Code: codeInvalidRequest,
			Message: "an empty batch is not a request"}}
	}
	v := c.Version()
	if v == "" {
		v = Oldest
	}
	if !Defines(v, FeatBatch) {
		return batchRefused(v)
	}
	replies := s.runBatch(ctx, c, elems, true)
	if len(replies) == 0 {
		return nil
	}
	return replies
}

// RESTHandler exposes one tool as a plain POST.
//
// The protocol form and this are the same code underneath. Offering only
// JSON-RPC would make mcpx reachable from MCP hosts and from nothing else,
// which is the opposite of the point: a shell script with curl should be able
// to ask the same questions an agent does.
func (s *Server) RESTHandler(tool string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "POST a JSON object of arguments", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			body = []byte("{}")
		}
		text, err := s.dispatch(r.Context(), tool, body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"ok": false, "error": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": text})
	}
}

// ServeHTTP answers a Streamable HTTP request, which is how a remote host
// reaches a server it did not start.
//
// Both eras share the endpoint. A request carrying
// io.modelcontextprotocol/protocolVersion in _meta (or a modern
// MCP-Protocol-Version header) is a 2026-07-28 request: its mirrored headers
// are validated, its status follows its error code, and it has no session.
// Anything else is legacy (2025-03-26 .. 2025-11-25): sessions minted at
// initialize, a GET notification stream, batches for 2025-03-26, and
// responses to questions mcpx asked.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r.Header.Get("Origin")) {
		writeJSON(w, http.StatusForbidden, response{JSONRPC: "2.0", Error: &rpcError{
			Code: codeInvalidRequest, Message: "Origin " + r.Header.Get("Origin") + " is not allowed"}})
		return
	}
	switch r.Method {
	case http.MethodPost:
	case http.MethodGet:
		s.serveGET(w, r)
		return
	case http.MethodDelete:
		s.serveDELETE(w, r)
		return
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "POST a JSON-RPC message", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hv := r.Header.Get("MCP-Protocol-Version")

	if isBatch(body) {
		s.serveBatch(w, r, body, hv)
		return
	}

	// A response to something mcpx asked. It carries the session header, and
	// the request that is waiting for it is being answered on another
	// goroutine with its stream still open.
	if id, result, rerr, ok := replyOf(body); ok {
		if modernHeaderVersion(hv) {
			writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0", Error: &rpcError{
				Code: codeInvalidRequest, Message: "2026-07-28 clients send no responses"}})
			return
		}
		c, found := s.session(r.Header.Get(sessionHeader))
		if !found || !c.deliver(id, result, rerr) {
			writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0", Error: &rpcError{
				Code: codeInvalidRequest, Message: "no request is waiting for that id on this session"}})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0",
			Error: &rpcError{Code: codeParse, Message: err.Error()}})
		return
	}
	if requestVersion(req.Params) != "" || modernHeaderVersion(hv) {
		s.serveModern(w, r, req)
		return
	}
	s.serveLegacy(w, r, req, hv)
}

// serveModern answers one 2026-07-28 POST.
func (s *Server) serveModern(w http.ResponseWriter, r *http.Request, req request) {
	if bad := checkModernHeaders(r, req); bad != nil {
		writeJSON(w, http.StatusBadRequest, bad)
		return
	}
	c, issued := s.sessionFor(r, req)
	if issued != "" {
		w.Header().Set(sessionHeader, issued)
	}
	ex := &httpExchange{w: w, flusher: asFlusher(w)}
	// The request's own context: closing the response stream is this
	// revision's cancellation, and it has to reach the upstream call.
	ctx := withSender(r.Context(), ex.send)
	resp := s.HandleOn(ctx, c, req)
	if r.Context().Err() != nil {
		// Cancelled. 2026-07-28: the server MUST NOT send any further
		// messages for it.
		return
	}
	s.finish(w, ex, req, resp, modernStatus(resp))
}

// serveLegacy answers one 2025-03-26 .. 2025-11-25 POST.
func (s *Server) serveLegacy(w http.ResponseWriter, r *http.Request, req request, hv string) {
	// 2025-06-18+: an invalid or unsupported MCP-Protocol-Version MUST get
	// 400. Absent means 2025-03-26, or whatever the session negotiated.
	if hv != "" && (!supports(hv) || Modern(hv)) {
		bad := unsupportedVersion(req.ID, nil, true)
		bad.Error.Data = map[string]any{"supported": LegacySupported(), "requested": hv}
		writeJSON(w, http.StatusBadRequest, bad)
		return
	}
	if id := r.Header.Get(sessionHeader); id != "" {
		if _, ok := s.session(id); !ok {
			// Unknown, expired or DELETEd. The client MUST then start a new
			// session with an initialize that carries no session id.
			writeJSON(w, http.StatusNotFound, response{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: codeInvalidRequest,
					Message: "no such session; send initialize without " + sessionHeader + " to start one"}})
			return
		}
	}
	c, issued := s.sessionFor(r, req)
	if issued != "" {
		w.Header().Set(sessionHeader, issued)
	}
	ex := &httpExchange{w: w, flusher: asFlusher(w)}
	ctx := r.Context()
	cancelled := func() bool { return false }
	if c.id != "" && len(req.ID) > 0 {
		// In a session, a dropped connection is not a cancellation: the
		// legacy revisions say disconnection SHOULD NOT be read as one, and
		// the client cancels with notifications/cancelled on the session.
		// Without a session no such notification could ever reach this
		// request, so the disconnect stays the only signal there is.
		var done func()
		ctx, done, cancelled = c.track(context.WithoutCancel(ctx), req.ID)
		defer done()
	}
	resp := s.HandleOn(withSender(ctx, ex.send), c, req)
	if cancelled() {
		// Withheld, as the legacy cancellation page asks. The POST still
		// needs an answer, and an event stream that ends without one is the
		// only shape that carries none.
		ex.open()
		return
	}
	s.finish(w, ex, req, resp, http.StatusOK)
}

// finish writes the answer to one POSTed message.
func (s *Server) finish(w http.ResponseWriter, ex *httpExchange, req request, resp *response, status int) {
	if len(req.ID) == 0 {
		// A notification: accepted is 202 with no body; not accepted is an
		// error status with an id-less JSON-RPC error.
		// An unknown notification is ignored rather than refused: JSON-RPC
		// gives a notification no reply, and every revision says to ignore
		// what is not understood. Any other error means it was not accepted.
		if resp != nil && resp.Error != nil && resp.Error.Code != codeMethodNotFound {
			resp.ID = nil
			writeJSON(w, http.StatusBadRequest, resp)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if ex.streaming {
		// The stream carried the questions; it carries the answer too, and
		// then ends. A client reading SSE has no other signal that the
		// exchange is over.
		if resp != nil {
			_ = ex.send(resp)
		}
		return
	}
	if resp == nil {
		// A request whose answer is withheld -- a listen stream, over a
		// transport that has nowhere to hold it open.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, status, resp)
}

// serveBatch answers a POSTed JSON-RPC batch, which only 2025-03-26 defines.
func (s *Server) serveBatch(w http.ResponseWriter, r *http.Request, body []byte, hv string) {
	if modernHeaderVersion(hv) {
		writeJSON(w, http.StatusBadRequest, batchRefused(hv))
		return
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(body, &elems); err != nil {
		writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0",
			Error: &rpcError{Code: codeParse, Message: err.Error()}})
		return
	}
	if len(elems) == 0 {
		writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0",
			Error: &rpcError{Code: codeInvalidRequest, Message: "an empty batch is not a request"}})
		return
	}
	var c *Conn
	version := hv
	ctx := r.Context()
	tracked := false
	if id := r.Header.Get(sessionHeader); id != "" {
		sc, ok := s.session(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, response{JSONRPC: "2.0",
				Error: &rpcError{Code: codeInvalidRequest, Message: "no such session"}})
			return
		}
		c, tracked, ctx = sc, true, context.WithoutCancel(ctx)
		if v := sc.Version(); v != "" {
			version = v
		}
	} else {
		c = s.newConn("", nil)
	}
	if version == "" {
		version = Oldest
	}
	if !Defines(version, FeatBatch) {
		writeJSON(w, http.StatusBadRequest, batchRefused(version))
		return
	}
	if c.Version() == "" {
		c.SetCapabilities(nil, version)
	}
	ex := &httpExchange{w: w, flusher: asFlusher(w)}
	replies := s.runBatch(withSender(ctx, ex.send), c, elems, tracked)
	switch {
	case ex.streaming:
		for _, resp := range replies {
			_ = ex.send(resp)
		}
	case len(replies) == 0:
		w.WriteHeader(http.StatusAccepted)
	default:
		writeJSON(w, http.StatusOK, replies)
	}
}

// serveDELETE ends a legacy session, and the streams open on it.
func (s *Server) serveDELETE(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get(sessionHeader)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0", Error: &rpcError{
			Code: codeInvalidRequest, Message: "DELETE ends a session; name it in " + sessionHeader}})
		return
	}
	if _, ok := s.session(id); !ok {
		writeJSON(w, http.StatusNotFound, response{JSONRPC: "2.0", Error: &rpcError{
			Code: codeInvalidRequest, Message: "no such session"}})
		return
	}
	s.dropSession(id)
	w.WriteHeader(http.StatusNoContent)
}

// sessionHeader is what the Streamable HTTP transport keys a session by.
const sessionHeader = "Mcp-Session-Id"

// sessionFor resolves the connection a request belongs to.
//
// A modern client never sends initialize, so the session is issued on
// server/discover instead. Without one it has no identity that outlives a
// POST, and mcpx cannot hand it a requestState it could verify later.
//
// A session exists so that a client's *answer* -- which arrives on a later,
// separate POST -- can be matched to the request still waiting for it. A
// modern client never needs one, because it is never asked anything
// mid-request; it gets an input_required result and retries.
func (s *Server) sessionFor(r *http.Request, req request) (*Conn, string) {
	modern := requestVersion(req.Params) != ""
	if id := r.Header.Get(sessionHeader); id != "" {
		// A modern request is never bound to a legacy session: 2026-07-28
		// has no sessions and says to ignore the header, and binding it
		// would hand it that client's pending questions and version.
		if c, ok := s.session(id); ok && !(modern && c.legacy) {
			return c, ""
		}
	}
	if req.Method != "initialize" && req.Method != "server/discover" {
		// Stateless. Correct for every modern request and for a legacy one
		// that will never be asked anything, and the alternative -- minting
		// a session per request -- is a map that only grows.
		c := s.newConn("", nil)
		if !modern {
			// No handshake on this connection, so the header is the version:
			// 2025-06-18 on send it, and absent means 2025-03-26.
			v := r.Header.Get("MCP-Protocol-Version")
			if v == "" {
				v = Oldest
			}
			c.SetCapabilities(nil, v)
		}
		return c, ""
	}
	id := newSessionID()
	c := s.newConn(id, nil)
	if req.Method == "initialize" {
		c.legacy = true
		// Notifications reach a legacy session through its GET stream, when
		// the client has one open. Installed now so a subscription made
		// before the stream opens still delivers once it does.
		c.pushFn = c.streamPush
	}
	s.sessMu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]*Conn{}
	}
	s.reapSessionsLocked()
	s.sessions[id] = c
	s.sessMu.Unlock()
	return c, id
}

func (s *Server) session(id string) (*Conn, bool) {
	if id == "" {
		return nil, false
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	c, ok := s.sessions[id]
	if ok {
		c.mu.Lock()
		c.lastUsed = time.Now()
		c.mu.Unlock()
	}
	return c, ok
}

func (s *Server) dropSession(id string) {
	if id == "" {
		return
	}
	s.sessMu.Lock()
	c := s.sessions[id]
	delete(s.sessions, id)
	s.sessMu.Unlock()
	if c != nil {
		c.end()
	}
}

// reapSessionsLocked drops connections nothing has used for a while.
//
// A session is only ever ended by a DELETE the client may never send, so
// without this the map is a leak that grows with every host that connects
// once.
func (s *Server) reapSessionsLocked() {
	cutoff := time.Now().Add(-s.Timing.resolved().SessionIdle)
	for id, c := range s.sessions {
		c.mu.Lock()
		idle := c.lastUsed.Before(cutoff)
		c.mu.Unlock()
		if idle {
			delete(s.sessions, id)
			c.end()
		}
	}
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "sess-0"
	}
	return "sess-" + hex.EncodeToString(b[:])
}

// httpExchange turns one POST response into an event stream, but only if
// something actually needs to be sent before the result.
//
// Lazily, because a stream is the more expensive answer for both sides and
// almost no request needs one: a client that asked a plain question should
// get a plain JSON object back.
type httpExchange struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	mu        sync.Mutex
	streaming bool
}

func (e *httpExchange) send(frame any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.flusher == nil {
		// Without flushing, the frame sits in a buffer until the handler
		// returns -- which is exactly when it is too late, because the
		// handler is waiting for the answer to it.
		return ErrNoPush
	}
	if !e.streaming {
		e.w.Header().Set("Content-Type", "text/event-stream")
		e.w.Header().Set("Cache-Control", "no-cache")
		e.w.Header().Set("X-Accel-Buffering", "no")
		e.w.WriteHeader(http.StatusOK)
		e.streaming = true
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.w, "data: %s\n\n", b); err != nil {
		return err
	}
	e.flusher.Flush()
	return nil
}

// open starts the event stream with nothing on it yet.
func (e *httpExchange) open() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.streaming {
		return
	}
	e.w.Header().Set("Content-Type", "text/event-stream")
	e.w.Header().Set("Cache-Control", "no-cache")
	e.w.Header().Set("X-Accel-Buffering", "no")
	e.w.WriteHeader(http.StatusOK)
	e.streaming = true
}

func asFlusher(w http.ResponseWriter) http.Flusher {
	f, _ := w.(http.Flusher)
	return f
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Request builds a request, for tests and for the client side of a loopback.
func Request(id int, method string, params any) request {
	idRaw, _ := json.Marshal(id)
	var p json.RawMessage
	if params != nil {
		p, _ = json.Marshal(params)
	}
	return request{JSONRPC: "2.0", ID: idRaw, Method: method, Params: p}
}

// ResultOf extracts the text from a tools/call reply, which is the shape
// every caller wants and nobody wants to unwrap by hand.
func ResultOf(resp *response) (string, bool, error) {
	if resp == nil {
		return "", false, errors.New("no response")
	}
	if resp.Error != nil {
		return "", true, errors.New(resp.Error.Message)
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return "", false, err
	}
	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", false, err
	}
	var parts []string
	for _, c := range r.Content {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n"), r.IsError, nil
}

// capabilities declares what this server can actually do.
//
// Push-dependent capabilities are declared only when something can push, and
// "something can push" is a property of the connection rather than of the
// server. It used to be tested as s.Notify != nil, which is true for every
// connection the daemon serves -- including the HTTP ones, which are built
// with no send function at all and can therefore deliver nothing. A client
// over that transport was told subscribe and listChanged were available, sent
// subscriptions/listen, got an acknowledgement and then silence forever.
//
// The delivery of list_changed also needs a stream the client opened, which
// only 2026-07-28 has. An older client has no way to ask for one, so it is
// told the truth: mcpx will not push it a list_changed it cannot receive.
func (s *Server) capabilities(version string, c *Conn) map[string]any {
	push := s.Notify != nil && c != nil && c.canPush()
	// listChanged is not gated on subscriptions/listen. That mechanism is
	// 2026-07-28's, but notifications/tools|prompts|resources/list_changed
	// are defined in every revision mcpx serves and arrive unsolicited on a
	// connection that can push. Gating them on the 2026 mechanism told every
	// legacy client the list would never change, which is both untrue and a
	// capability mcpx implements.
	streamed := push
	caps := map[string]any{
		"tools":       map[string]any{"listChanged": streamed},
		"resources":   map[string]any{"subscribe": push, "listChanged": streamed},
		"prompts":     map[string]any{"listChanged": streamed},
		"completions": map[string]any{},
	}
	if Defines(version, FeatTasks) {
		// Core in 2025-11-25 and an extension in 2026-07-28, so it is
		// declared both ways and a client of either era finds it where it
		// looks. mcpx accepts tasks/* from an older client too -- offering
		// more than a revision requires withholds nothing -- but it does
		// not advertise them there, because a declaration is a promise
		// about the revision in force.
		caps["tasks"] = map[string]any{
			"list": map[string]any{}, "cancel": map[string]any{},
			"requests": map[string]any{"tools": map[string]any{"call": map[string]any{}}},
		}
		caps["extensions"] = map[string]any{
			"io.modelcontextprotocol/tasks": map[string]any{},
		}
	}
	return caps
}

// restartListen replaces the active notification stream with one for f.
//
// One stream per connection. The specification allows a client to reopen
// with a different filter, and the simplest faithful implementation is to
// stop the old and start the new.
func (s *Server) restartListen(c *Conn, f ListenFilter) {
	c.mu.Lock()
	if c.listening != nil {
		c.listening()
		c.listening = nil
	}
	push := c.pushFn
	notify := s.Notify
	if push == nil || notify == nil {
		c.mu.Unlock()
		return
	}
	lctx, cancel := context.WithCancel(context.Background())
	c.listening = cancel
	c.mu.Unlock()

	go notify.Listen(lctx, f, push)
}

// stopListen ends any stream this connection opened.
func (c *Conn) stopListen() {
	c.mu.Lock()
	if c.listening != nil {
		c.listening()
		c.listening = nil
	}
	c.mu.Unlock()
}

// SetPush installs how to reach the default connection's client.
func (s *Server) SetPush(fn func(method string, params any)) {
	c := s.conn()
	c.mu.Lock()
	c.pushFn = fn
	c.mu.Unlock()
}

// pageSize is how many items one list reply carries.
func (s *Server) pageSize() int {
	if s.PageSize > 0 {
		return s.PageSize
	}
	return 100
}

// page slices a list according to an opaque cursor.
//
// The cursor is the offset, encoded, because the specification says it is
// opaque and a client that parses one is relying on something it was told not
// to. Encoding it costs nothing and removes the temptation.
func page[T any](all []T, params json.RawMessage, size int) ([]T, string) {
	start := 0
	if len(params) > 0 {
		var p struct {
			Cursor string `json:"cursor"`
		}
		if json.Unmarshal(params, &p) == nil && p.Cursor != "" {
			if n, err := decodeCursor(p.Cursor); err == nil {
				start = n
			}
		}
	}
	if start >= len(all) {
		return []T{}, ""
	}
	end := start + size
	if end >= len(all) {
		return all[start:], ""
	}
	return all[start:end], encodeCursor(end)
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

func decodeCursor(c string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, err
	}
	s := string(raw)
	if !strings.HasPrefix(s, "o:") {
		return 0, fmt.Errorf("bad cursor")
	}
	return strconv.Atoi(s[2:])
}

// cancel records a client's cancellation and cancels the request it names,
// if this connection is still answering it.
func (c *Conn) cancel(params json.RawMessage) {
	var p struct {
		RequestID any    `json:"requestId"`
		Reason    string `json:"reason"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	c.mu.Lock()
	if c.cancelled == nil {
		c.cancelled = map[string]string{}
	}
	c.cancelled[fmt.Sprint(p.RequestID)] = p.Reason
	c.mu.Unlock()
	if c.s.OnCancel != nil {
		c.s.OnCancel(fmt.Sprint(p.RequestID), p.Reason)
	}
	// And acted on: the request's context is cancelled, which is what
	// reaches the upstream call, and its response is withheld.
	c.cancelInflight(p.RequestID)
}

// Cancelled reports whether a request was cancelled on the default
// connection, and why.
func (s *Server) Cancelled(id string) (string, bool) {
	c := s.conn()
	c.mu.Lock()
	defer c.mu.Unlock()
	reason, ok := c.cancelled[id]
	return reason, ok
}

// complete answers an autocomplete request.
//
// Only from what mcpx already holds: namespaces and tool names. A client that
// offers completion and gets method-not-found shows nothing, and the user
// concludes the feature is broken rather than unimplemented.
func (s *Server) complete(ctx context.Context, params json.RawMessage) map[string]any {
	var p struct {
		Argument struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"argument"`
	}
	_ = json.Unmarshal(params, &p)

	prefix := strings.ToLower(p.Argument.Value)
	var values []string
	seen := map[string]bool{}
	for _, t := range s.Tools() {
		for _, candidate := range []string{t.Name, namespaceOf(t.Name)} {
			if candidate == "" || seen[candidate] {
				continue
			}
			if prefix == "" || strings.Contains(strings.ToLower(candidate), prefix) {
				seen[candidate] = true
				values = append(values, candidate)
			}
		}
	}
	sort.Strings(values)
	// The ceiling the specification sets, and the one /v1/complete already
	// read from completion.maxValues. It was written here as the literal 100,
	// so configuring the setting moved one of the two surfaces and not the
	// other.
	max := s.MaxCompletions
	if max <= 0 {
		max = defaults.CompletionValues
	}
	total := len(values)
	if len(values) > max {
		values = values[:max]
	}
	if values == nil {
		values = []string{}
	}
	return map[string]any{
		"values":  values,
		"total":   total,
		"hasMore": total > len(values),
	}
}

func namespaceOf(tool string) string {
	if i := strings.Index(tool, "_"); i > 0 {
		return tool[:i]
	}
	return ""
}

type lockedEncoder struct {
	mu  *sync.Mutex
	enc *json.Encoder
}

func (l lockedEncoder) Encode(v any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enc.Encode(v)
}
