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

	"github.com/dezren39/mcpx/internal/catalog"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/spec"
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
	// ReadResource returns a resource's contents as the upstream server
	// gave them: text stays text and a blob stays a blob.
	ReadResource(ctx context.Context, uri string) ([]ResourceContents, error)
	GetPrompt(ctx context.Context, name string, args map[string]string) (string, error)
	// Complete answers completion/complete for a ref/prompt or ref/resource
	// by asking the upstream server that owns it. params is the request as
	// the client sent it. An unknown ref wraps ErrInvalidParams.
	Complete(ctx context.Context, params json.RawMessage) ([]string, error)
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

// ResourceNotifier is a Notifier that must arrange for resource updates
// before it can deliver them -- by subscribing upstream -- and so can only
// say which of the requested resources it will deliver once it has tried.
//
// A server MUST NOT agree to what it cannot deliver: the subscriptions/listen
// acknowledgement exists to report what was honoured. A legacy
// resources/subscribe cannot report it (#251), so it succeeds regardless, but
// still waits for ready so only honoured URIs are ever delivered.
type ResourceNotifier interface {
	Notifier
	// ListenResources is Listen for a filter that names resources. ready
	// is called at most once, before any send, with the requested URIs
	// whose updates will be delivered and a reason for each other one.
	ListenResources(ctx context.Context, f ListenFilter,
		ready func(watching []string, refused map[string]string),
		send func(method string, params any))
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
	// Carried from the upstream as published; downgrade() removes what an
	// older revision does not define.
	Title       string          `json:"title,omitempty"`
	Size        *int64          `json:"size,omitempty"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
	Icons       json.RawMessage `json:"icons,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

// ResourceContents is one entry of a resources/read result.
//
// Blob, when set, is base64 and wins over Text: that is the schema's
// BlobResourceContents, and a binary body flattened into text -- which is
// what mcpx sent -- is a string a client cannot tell from a document.
type ResourceContents struct {
	// URI is the entry's own URI; empty means the one that was read.
	URI      string
	MimeType string
	Text     string
	Blob     string
	// Meta is the entry's _meta, carried as the upstream sent it.
	Meta json.RawMessage
}

// readResult renders contents in the schema's shape for a read of uri.
func readResult(uri string, contents []ResourceContents) map[string]any {
	out := make([]any, 0, len(contents))
	for _, c := range contents {
		entry := map[string]any{"uri": c.URI}
		if c.URI == "" {
			entry["uri"] = uri
		}
		if c.MimeType != "" {
			entry["mimeType"] = c.MimeType
		}
		if c.Blob != "" {
			entry["blob"] = c.Blob
		} else {
			if c.MimeType == "" {
				entry["mimeType"] = "text/plain"
			}
			entry["text"] = c.Text
		}
		if len(c.Meta) > 0 {
			entry["_meta"] = c.Meta
		}
		out = append(out, entry)
	}
	return map[string]any{"contents": out}
}

// PromptRef is one prompt a server offers.
type PromptRef struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Arguments   []PromptArg     `json:"arguments,omitempty"`
	Icons       json.RawMessage `json:"icons,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

// PromptArg is one substitution a prompt takes.
type PromptArg struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
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

	// ExtrasOnly makes the extras the whole tool surface: no mcpx_* tools.
	// It is how one adapted program is served as an upstream server of its
	// own (`mcpx adapter serve`), where the meta-tools would be a second,
	// recursive copy of mcpx inside one of its own namespaces.
	ExtrasOnly bool
	// OwnInstructions replaces mcpx's initialize instructions when
	// ExtrasOnly is set: they describe the meta-tools, which are absent.
	OwnInstructions string

	// PageSize caps how many items a list reply carries.
	PageSize int

	// Cache is the freshness a 2026-07-28 client is told it may assume.
	// New fills it from the defaults; unlike Timing, zero is a meaningful
	// value here -- "immediately stale" -- so it is not a sentinel.
	Cache Cache

	// MaxCompletions caps a completion/complete reply, read on every
	// request because the setting behind it is hot. Nil, or a value <= 0,
	// uses the built-in default.
	MaxCompletions func() int

	// Timing is the ask loop's policy. A zero field means the built-in
	// default; the whole struct zero is what a test that does not care
	// should be able to leave alone.
	Timing Timing
	// OnCancel is called when a client cancels a request.
	OnCancel func(id, reason string)

	// Origins decides which browser origins the HTTP transport serves.
	Origins OriginPolicy

	// askedTasks are the tasks whose body is a call through the Asker,
	// by task id; see askTask.
	askedTasks map[string]askedTask

	// Passthrough names the upstreams whose tools are offered under their
	// own names alongside, and ahead of, the gateway's; see passthrough.go.
	Passthrough []string

	// UpstreamSkills returns skills discovered from upstream servers to relay.
	UpstreamSkills func() []*UpstreamSkill

	// Reactive enables Mode 2 dynamic tool discovery (request_tools + working set).
	Reactive bool
	// PinnedTools are tools always exposed in reactive mode.
	PinnedTools []string
	// ReactiveWorkingSet is the bounded tool retention manager for reactive mode.
	ReactiveWorkingSet *catalog.WorkingSet
	// SearchTools is called by request_tools to find tools across the catalog.
	SearchTools func(ctx context.Context, query string) ([]Tool, error)

	mu sync.Mutex

	// Notify is where pushed notifications come from. Nil means mcpx never
	// pushes, and declares so.
	Notify Notifier

	// Ask runs a request that may ask questions back. Nil means mcpx
	// answers every upstream question through the daemon's broker, which is
	// what it did before any client could answer one inline.
	Ask Asker

	// stdioBusy is set while a ServeStdio loop holds the default connection.
	stdioBusy bool

	// def is the connection the in-process entry points use. A transport
	// that serves many clients makes one Conn apiece instead.
	def     *Conn
	defOnce sync.Once

	// taskStore holds background requests.
	taskStore *taskStore
	// taskOwners is which connection started each task, by its id. Empty
	// means nobody in particular: the task id alone is the handle.
	taskOwners map[string]string

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
	s.defOnce.Do(func() {
		s.def = s.newConn(newSessionID(), nil)
		s.def.process = true
	})
	return s.def
}

// New builds a server.
func New(b Backend, name, version string) *Server {
	return &Server{backend: b, name: name, version: version,
		Cache: Cache{List: defaults.ProtoListMaxAge, Read: defaults.ProtoReadMaxAge}}
}

// Cache is the ttlMs mcpx attaches to the results 2026-07-28 makes
// cacheable.
type Cache struct {
	// List covers server/discover and the four list methods.
	List time.Duration
	// Read covers resources/read.
	Read time.Duration
}

// WithExtras returns a server that also offers these tools.
//
// A new value rather than a mutation, so a caller cannot change the surface
// of a server another goroutine is already answering with.
func (s *Server) WithExtras(extras []Extra) *Server {
	return &Server{
		backend: s.backend, name: s.name, version: s.version,
		PageSize: s.PageSize, MaxCompletions: s.MaxCompletions, Cache: s.Cache,
		Timing: s.Timing, OnCancel: s.OnCancel, Origins: s.Origins, Passthrough: s.Passthrough,
		// Notify comes along. Dropping it silently turned off every push
		// capability the moment a single extra tool existed, and a client
		// cannot detect a server that declared nothing.
		Notify:             s.Notify,
		Ask:                s.Ask,
		ExtrasOnly:         s.ExtrasOnly,
		OwnInstructions:    s.OwnInstructions,
		Reactive:           s.Reactive,
		PinnedTools:        s.PinnedTools,
		ReactiveWorkingSet: s.ReactiveWorkingSet,
		SearchTools:        s.SearchTools,
		UpstreamSkills:     s.UpstreamSkills,
		extras:             sortedExtras(append(append([]Extra(nil), s.extras...), extras...)),
	}
}

// sortedExtras orders contributed tools by name.
//
// tools/list SHOULD be deterministic, and the extras arrive from three
// sources -- adapter declarations, OpenAPI documents, the /v1 operation
// table -- whose order is whatever each happened to produce. Sorting here,
// once, makes the list the same across calls and across restarts without
// trusting every source to stay stable.
func sortedExtras(in []Extra) []Extra {
	sort.SliceStable(in, func(i, j int) bool { return in[i].Tool.Name < in[j].Tool.Name })
	return in
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
	// codeMissingCapability is MissingRequiredClientCapability, 2026-07-28.
	codeMissingCapability = -32021
	// codeResourceNotFoundLegacy is what 2025-11-25 and earlier call a
	// resource that does not exist. 2026-07-28 retired it in favour of
	// -32602 and forbids emitting it, so it goes to legacy clients only.
	codeResourceNotFoundLegacy = -32002
)

// ErrResourceNotFound is what a Backend wraps when a resource does not
// exist, as opposed to failing to read one that does. The difference is an
// error code the client acts on: not-found is the caller's mistake,
// anything else is mcpx's.
var ErrResourceNotFound = errors.New("resource not found")

// ErrInvalidParams is what a Backend wraps when the request named something
// that does not exist or was malformed -- an unknown prompt, a missing
// required argument -- as opposed to failing to carry out a good one. The
// former is -32602, the client's to fix; the latter -32603.
var ErrInvalidParams = errors.New("invalid params")

// unsupportedVersion is the answer to a modern request carrying a version
// mcpx does not implement.
//
// The list matters: a client has no other way to discover what would work,
// and the specification says it SHOULD retry with something from it. Only
// the modern era reaches here -- a legacy initialize with an unknown version
// is answered with a version, not refused (see negotiate).
func unsupportedVersion(id json.RawMessage, params json.RawMessage) *response {
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{
		Code:    codeUnsupportedVersion,
		Message: "Unsupported protocol version",
		Data: map[string]any{
			"supported": Supported,
			"requested": requestVersion(params),
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
	// Execution is a pass-through upstream's execution object, verbatim,
	// or for mcpx's own tools the taskSupport it declares. Its taskSupport
	// decides whether a call may, must or must not run as a task; see
	// taskSupportOf and legacyTaskSupport.
	Execution json.RawMessage `json:"execution,omitempty"`
	// The rest of an upstream tool, for pass-through listings. mcpx's own
	// tools leave them empty.
	Title        string          `json:"title,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Icons        json.RawMessage `json:"icons,omitempty"`
	Meta         json.RawMessage `json:"_meta,omitempty"`
}

// taskOptional is the execution object of mcpx's own tools that may run as
// a 2025-11-25 task: the two that wait on upstreams. The rest answer from
// what the daemon already holds, so they declare nothing -- "forbidden".
var taskOptional = json.RawMessage(`{"taskSupport":"optional"}`)

// withoutOwnExecution drops the execution object from mcpx's own tools.
// It is 2025-11-25's core-tasks declaration, which 2026-07-28's Tool does
// not define; there the server decides what becomes a task. A pass-through
// upstream's own execution is its business and is relayed as it came.
func withoutOwnExecution(tools []Tool) []Tool {
	out := make([]Tool, len(tools))
	for i, t := range tools {
		if len(t.Execution) > 0 && &t.Execution[0] == &taskOptional[0] {
			t.Execution = nil
		}
		out[i] = t
	}
	return out
}

// Tools is the surface.
//
// Deliberately small. The whole reason mcpx exists is that three hundred tool
// schemas in a context window crowds out the work; exposing three hundred
// again over MCP would rebuild the problem with extra steps. These ten reach
// all of them, and the schemas stay here.
func (s *Server) Tools() []Tool {
	if s.ExtrasOnly {
		out := make([]Tool, 0, len(s.extras))
		for _, e := range s.extras {
			out = append(out, e.Tool)
		}
		return out
	}
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
			// An upstream call can take as long as the upstream likes.
			Execution: taskOptional,
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
			Execution: taskOptional,
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
		{
			Name: "mcpx_batch",
			Description: "Execute one or more tools in a single turn. " +
				"Accepts any tool name (e.g. namespace.tool or bare tool name), reducing round trips.",
			InputSchema: schema(`{"type":"object","properties":{
				"calls":{"type":"array","description":"List of tools to execute","items":{"type":"object","properties":{
					"namespace":{"type":"string"},
					"tool":{"type":"string"},
					"name":{"type":"string"},
					"arguments":{"type":"object","description":"arguments for the tool"},
					"args":{"type":"object","description":"alias for arguments"}
				},"required":["tool"]}},
				"stopOnError":{"type":"boolean","description":"stop execution on first failure (default true)"},
				"parallel":{"type":"boolean","description":"execute calls concurrently (default false)"}
			},"required":["calls"],"additionalProperties":false}`),
		},
		{
			Name: "batch_call",
			Description: "Alias for mcpx_batch. Execute one or more tools in a single turn.",
			InputSchema: schema(`{"type":"object","properties":{
				"calls":{"type":"array","description":"List of tools to execute","items":{"type":"object","properties":{
					"namespace":{"type":"string"},
					"tool":{"type":"string"},
					"name":{"type":"string"},
					"arguments":{"type":"object","description":"arguments for the tool"},
					"args":{"type":"object","description":"alias for arguments"}
				},"required":["tool"]}},
				"stopOnError":{"type":"boolean","description":"stop execution on first failure (default true)"},
				"parallel":{"type":"boolean","description":"execute calls concurrently (default false)"}
			},"required":["calls"],"additionalProperties":false}`),
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
	if req.Method == "server/discover" {
		// Only the modern era has it, so whatever the request carried the
		// answer is spelled in the modern shape. A probe that sent no _meta
		// is still a client asking which era mcpx is.
		peer = Peer{Version: ModernLatest, Modern: true, Caps: requestCapabilities(req.Params)}
	}
	// Resolved here, once, so every backend call made for this request --
	// including one run later as a task -- knows which client it serves.
	ctx = withIdentity(ctx, c.identityFor(req.Params))
	resp := s.handle(ctx, c, req)
	if resp == nil || resp.Error != nil {
		return resp
	}
	if peer.Modern {
		resp.Result = s.envelope(req, resp.Result)
	}
	// Send conservatively. Everything above builds results in the newest
	// shape; this is the one place that spells them in the client's own.
	resp.Result = downgrade(resp.Result, peer.Version)
	return resp
}

// cacheable is the method list the caching page makes a MUST, with the
// scope each gets.
//
// public means identical for every caller of this daemon: the tools, the
// prompts and the templates follow configuration, not who is asking.
// resources/list is private because it includes the caller's own session
// artifacts, and resources/read because a resource may be anything an
// upstream server decides it is for this caller.
var cacheable = map[string]struct {
	scope string
	read  bool
}{
	"server/discover":          {scope: "public"},
	"tools/list":               {scope: "public"},
	"prompts/list":             {scope: "public"},
	"resources/templates/list": {scope: "public"},
	"resources/list":           {scope: "private"},
	"resources/read":           {scope: "private", read: true},
	// GetSkillResult extends CacheableResult, as resources/read does.
	"skills/list": {scope: "public"},
	"skills/get":  {scope: "public", read: true},
}

// envelope adds what 2026-07-28 wants on every result: resultType, the
// server's identity, and on the cacheable ones a freshness hint.
func (s *Server) envelope(req request, result any) any {
	// A copy, for the reason downgrade copies: a stored task result is
	// handed out more than once.
	m, ok := copyMap(result)
	if !ok {
		return result
	}
	if _, set := m["resultType"]; !set {
		// Mandatory in 2026-07-28: it is how a client tells a finished
		// result from one still asking for input.
		m["resultType"] = "complete"
	}
	// Only on a complete result. An input_required is an interim answer the
	// caching page says carries no hints, and a task handle is not the
	// method's result at all.
	if c, ok := cacheable[req.Method]; ok && m["resultType"] == "complete" {
		ttl := s.Cache.List
		if c.read {
			ttl = s.Cache.Read
		}
		if ttl < 0 {
			ttl = 0 // the specification requires >= 0
		}
		m["ttlMs"] = ttl.Milliseconds()
		m["cacheScope"] = c.scope
	}
	// SHOULD on every result, merged rather than assigned: a result may
	// already carry _meta of its own -- a listen's subscriptionId -- and
	// that must survive.
	meta, _ := m["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta[MetaServerInfo] = map[string]any{"name": s.name, "version": s.version}
	m["_meta"] = meta
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
		return unsupportedVersion(req.ID, req.Params)
	}
	if bad := malformedMeta(req); bad != nil {
		return bad
	}
	peer := c.peerFor(req.Params)
	ctx = withPeerVersion(ctx, peer)

	// A method the peer's own revision removed is method-not-found for that
	// peer, whatever mcpx is still willing to do for an older one. See
	// removedIn: the 404 this becomes on HTTP is how a dual-era client tells
	// a modern server from a legacy endpoint that is simply not there.
	if peer.Modern && Removed(peer.Version, req.Method) {
		return fail(codeMethodNotFound, "no method "+req.Method+" in "+peer.Version)
	}

	switch req.Method {
	case "tasks/get", "tasks/list", "tasks/result", "tasks/cancel", "tasks/update":
		return s.handleTask(ctx, c, req, peer)
	case "skills/list", "skills/get":
		return s.handleSkills(ctx, req)
	case "resources/directory/read":
		return s.handleDirectoryRead(ctx, req)
	case "tools/call":
		// Finding the tool is the protocol's business, running it the
		// tool's: an unknown name is -32602, in every revision, before a
		// task or a question is started for a call that cannot happen.
		var call struct {
			Name string `json:"name"`
		}
		ctx = s.withPass(ctx)
		if json.Unmarshal(req.Params, &call) == nil && call.Name != "" {
			found, err := s.hasTool(ctx, call.Name)
			if err == nil && !found {
				// The upstream decides what names it answers to, listed or
				// not; it says -32602 itself, relayed as such, for one it
				// does not know.
				found, err = s.routesToPass(ctx, call.Name)
			}
			if err != nil {
				// Not -32602: the name may well be right. The server that
				// would know is the one not answering.
				return &response{JSONRPC: "2.0", ID: req.ID,
					Error: &rpcError{Code: codeInternal, Message: err.Error()}}
			}
			if !found {
				return &response{JSONRPC: "2.0", ID: req.ID,
					Error: &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("no tool named %q", call.Name)}}
			}
		}
		if resp := s.maybeTask(ctx, c, req, peer); resp != nil {
			return resp
		}
	}

	switch req.Method {
	case "initialize":
		version := negotiate(req.Params)
		// What the client declared here governs everything mcpx may send it
		// for the life of the connection. A legacy server never asks again,
		// so not recording it is the same as deciding the answer is "no".
		var ip struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		}
		_ = json.Unmarshal(req.Params, &ip)
		c.SetCapabilities(ip.Capabilities, version)
		return reply(map[string]any{
			"protocolVersion": version,
			"capabilities":    s.capabilities(ctx, version, c),
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
			"instructions":    s.instructions(),
		})

	case "server/discover":
		// Mandatory in the modern revisions, and the probe a dual-era client
		// uses to decide which era it is talking to. serverInfo is not here:
		// 2026-07-28 moved it into _meta, which envelope adds to every
		// modern result, this one included.
		return reply(map[string]any{
			"supportedVersions": Supported,
			"capabilities":      s.capabilities(ctx, ModernLatest, c),
			"instructions":      s.instructions(),
		})

	case "notifications/initialized", "initialized":
		return nil // a notification: no reply, by definition

	case "ping":
		// Legacy only. 2026-07-28 removed ping (changelog item 5), so a
		// modern request never reaches here: the Removed check above
		// answers it -32601, which on HTTP is the 404 streamable-http
		// requires for "does not implement the requested RPC method".
		return reply(map[string]any{})

	case "tools/list":
		// Paginated, because mcpx fronts every tool of every configured
		// server and a client with a frame limit has no other way to read
		// the list. Ignoring the cursor meant a large installation was
		// simply unreadable by such a client.
		all, serr := s.surface(ctx)
		if serr != nil {
			return fail(codeInternal, serr.Error())
		}
		tools, next, perr := page(all, req.Params, s.pageSize())
		if perr != nil {
			return fail(codeInvalidParams, perr.Error())
		}
		if peer.Modern {
			tools = withoutOwnExecution(tools)
		}
		out := map[string]any{"tools": tools}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "completion/complete":
		// Argument autocomplete, forwarded to the server that owns the ref.
		completion, rerr := s.complete(ctx, req.Params)
		if rerr != nil {
			return &response{JSONRPC: "2.0", ID: req.ID, Error: rerr}
		}
		return reply(map[string]any{"completion": completion})

	case "logging/setLevel":
		// The level governs which upstream log messages are relayed to this
		// client during its calls (see CallRelay). A 2026-07-28 client, whose
		// revision removed the method (changelog item 5), is answered -32601
		// by the Removed check above and names a level per request instead.
		// A level that is not one of the eight is malformed, and every
		// revision's setLevel asks for -32602.
		var lv struct {
			Level string `json:"level"`
		}
		_ = json.Unmarshal(req.Params, &lv)
		if lv.Level != "" && !logLevels[lv.Level] {
			return fail(codeInvalidParams, fmt.Sprintf("%q is not a log level", lv.Level))
		}
		if c != nil {
			c.mu.Lock()
			c.logLevel = lv.Level
			c.mu.Unlock()
		}
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
		items, next, perr := page(asTemplates(ts), req.Params, s.pageSize())
		if perr != nil {
			return fail(codeInvalidParams, perr.Error())
		}
		out := map[string]any{"resourceTemplates": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "resources/subscribe", "resources/unsubscribe":
		// The legacy mechanism: per-URI, per-connection, its notifications
		// untagged. The modern revision replaced it with
		// resourceSubscriptions on subscriptions/listen, handled below; both
		// feed the same notifier.
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.URI == "" {
			return fail(codeInvalidParams, "uri is required")
		}
		// Serialised per connection: each change replaces the stream, and
		// two racing replacements could leave the older set in force.
		c.subMu.Lock()
		defer c.subMu.Unlock()
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
		sort.Strings(uris)
		// A legacy subscription always succeeds, even where mcpx can
		// arrange no updates (a URI no server owns, a server without
		// resources.subscribe). It is a standing interest, not a lookup:
		// the spec does not require the resource to exist, nor updates to
		// be deliverable, and the legacy revisions have no way to say
		// "agreed, but nothing will come" -- refusing broke every client
		// that subscribes before it knows (#251). The URI stays out of the
		// stream's agreed set, so nothing is delivered for it; the daemon
		// records why as a warning. The 2026-07-28 listen acknowledgement,
		// which can say it, still lists only what will be delivered.
		s.restartListen(ctx, c, ListenFilter{ResourceSubscriptions: uris})
		return reply(map[string]any{})

	case "subscriptions/listen":
		return s.listen(ctx, c, req, peer)

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
		ctx = withPeerCaps(ctx, peer)
		if s.canAsk(ctx, c, peer) {
			if resp := s.viaAsk(s.withRelay(ctx, c, req, peer), c, req, peer); resp != nil {
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
		text, err := s.invoke(s.withRelay(ctx, c, req, peer), p.Name, p.Arguments)
		if up := (*UpstreamError)(nil); errors.As(err, &up) {
			return up.relay(req.ID)
		}
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
		if raw, ok := decodeRaw(text); ok {
			return reply(raw)
		}
		text, blocks := decodeResult(text)
		content := []any{map[string]any{"type": "text", "text": text}}
		for _, b := range blocks {
			content = append(content, b)
		}
		return reply(map[string]any{"content": content})

	case "resources/list":
		rs := s.skillResources()
		if s.backend != nil {
			up, err := s.backend.Resources(ctx)
			if err != nil {
				return fail(codeInternal, err.Error())
			}
			rs = append(rs, up...)
		}
		if rs == nil {
			rs = []ResourceRef{}
		}
		items, next, perr := page(rs, req.Params, s.pageSize())
		if perr != nil {
			return fail(codeInvalidParams, perr.Error())
		}
		out := map[string]any{"resources": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "resources/read":
		var sk struct {
			URI string `json:"uri"`
		}
		// Before the ask path: a skill file is mcpx's own and never needs
		// an upstream to answer for it.
		if json.Unmarshal(req.Params, &sk) == nil {
			if contents, ok := s.readSkillFile(sk.URI); ok {
				return reply(readResult(sk.URI, contents))
			}
		}
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
		if s.backend == nil {
			return notFound(req.ID, p.URI, peer, ErrResourceNotFound)
		}
		contents, err := s.backend.ReadResource(ctx, p.URI)
		if err != nil {
			if errors.Is(err, ErrResourceNotFound) {
				return notFound(req.ID, p.URI, peer, err)
			}
			// Everything else is mcpx failing to read something that may
			// well exist, which the resources page says is -32603. It was
			// -32602 for every failure, telling a client its perfectly good
			// URI was the problem when an upstream server had timed out.
			return fail(codeInternal, err.Error())
		}
		return reply(readResult(p.URI, contents))

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
		items, next, perr := page(ps, req.Params, s.pageSize())
		if perr != nil {
			return fail(codeInvalidParams, perr.Error())
		}
		out := map[string]any{"prompts": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(codeInvalidParams, err.Error())
		}
		if s.backend == nil {
			return fail(codeInternal, "no prompts are available")
		}
		// Arguments are checked here, before anything runs or asks: a
		// missing required one is the client's error (-32602), and once
		// the request is known to be well-formed, a failure is mcpx's or
		// the upstream's (-32603).
		missing := s.promptArgs(ctx, p.Name, p.Arguments)
		if missing != "" {
			return fail(codeInvalidParams, fmt.Sprintf("prompt %q needs argument %q", p.Name, missing))
		}
		if s.canAsk(ctx, c, peer) {
			if resp := s.viaAsk(ctx, c, req, peer); resp != nil {
				return resp
			}
		}
		text, err := s.backend.GetPrompt(ctx, p.Name, p.Arguments)
		if err != nil {
			// Classified, as resources/read is (conflict #12): the backend
			// says, with ErrInvalidParams, when the request was at fault.
			// https://modelcontextprotocol.io/specification/2025-11-25/server/prompts#error-handling
			if errors.Is(err, ErrInvalidParams) {
				return fail(codeInvalidParams, err.Error())
			}
			return fail(codeInternal, err.Error())
		}
		if raw, ok := decodeRaw(text); ok {
			return reply(raw)
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

// notFound is the error for a resource that does not exist, in the code the
// client's revision defines.
//
// 2026-07-28 moved it to -32602 and forbids -32002; every earlier revision
// says -32002. Both carry the URI in data, as each revision's example does.
func notFound(id json.RawMessage, uri string, p Peer, err error) *response {
	code := codeResourceNotFoundLegacy
	if p.Modern {
		code = codeInvalidParams
	}
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{
		Code: code, Message: err.Error(), Data: map[string]any{"uri": uri}}}
}

// asTemplates renders templates in the shape every revision's
// ResourceTemplate has.
//
// The field is uriTemplate. mcpx sent `uri`, which no revision defines on a
// template, so a strict client rejected the whole list and a lenient one
// found a template with no template in it.
func asTemplates(ts []ResourceRef) []map[string]any {
	out := make([]map[string]any, 0, len(ts))
	for _, t := range ts {
		m := map[string]any{"uriTemplate": t.URI, "name": t.Name}
		if t.Description != "" {
			m["description"] = t.Description
		}
		if t.MimeType != "" {
			m["mimeType"] = t.MimeType
		}
		// size is a Resource field; a template has none.
		if t.Title != "" {
			m["title"] = t.Title
		}
		for k, v := range map[string]json.RawMessage{
			"annotations": t.Annotations, "icons": t.Icons, "_meta": t.Meta} {
			if len(v) > 0 {
				m[k] = v
			}
		}
		out = append(out, m)
	}
	return out
}

// The log levels a 2026-07-28 request may name, from the schema's
// LoggingLevel.
var logLevels = map[string]bool{
	"debug": true, "info": true, "notice": true, "warning": true,
	"error": true, "critical": true, "alert": true, "emergency": true,
}

// malformedMeta refuses a modern request missing a field 2026-07-28 makes
// mandatory on every request, or naming a log level that does not exist.
//
// Only a request -- a notification has no id to answer on, and the
// per-request fields are defined for requests. And only one that declared a
// protocol version: without one it is a legacy request, which has no _meta
// requirements at all.
func malformedMeta(req request) *response {
	if len(req.ID) == 0 || requestVersion(req.Params) == "" || !Modern(requestVersion(req.Params)) {
		return nil
	}
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	_ = json.Unmarshal(req.Params, &p)
	bad := func(msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: codeInvalidParams, Message: msg}}
	}
	raw, ok := p.Meta[MetaClientCapabilities]
	var caps map[string]json.RawMessage
	if !ok || json.Unmarshal(raw, &caps) != nil || caps == nil {
		return bad(MetaClientCapabilities + " is required on every 2026-07-28 request and must be an object")
	}
	if raw, ok := p.Meta[MetaLogLevel]; ok {
		var level string
		if json.Unmarshal(raw, &level) != nil || !logLevels[level] {
			return bad(MetaLogLevel + " is not a log level: " + string(raw))
		}
	}
	return nil
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
//
// 2024-11-05 is served over stdio and Streamable HTTP. Its own HTTP
// transport -- HTTP+SSE, a GET stream plus a POST endpoint -- is not hosted:
// 2025-03-26 replaced it, and 2026-07-28 says new implementations SHOULD NOT
// adopt it. A 2024-11-05 client speaking Streamable HTTP is served; one that
// only knows HTTP+SSE is not.
var Supported = spec.Revisions

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
// A version mcpx serves is agreed as asked. Anything else -- a revision mcpx
// does not know, or a modern one, which has no handshake -- is answered with
// Latest. Every legacy lifecycle page says the same thing: if the server
// does not support the requested version it MUST respond with another
// version it supports, and SHOULD pick its latest. The client then decides
// whether it can speak that, and disconnects if not.
//
// mcpx used to refuse with -32022 instead. That code is 2026-07-28's, and a
// legacy client has no idea what it means: it saw an initialize fail and
// had no version to fall back to, which is exactly the situation the rule
// exists to prevent. Echoing whatever was asked -- the bug before that --
// is wrong the other way: a client requesting 2026-07-28 was told yes and
// then found no per-request metadata handling.
func negotiate(params json.RawMessage) string {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(params, &p) != nil {
		return Latest
	}
	if supports(p.ProtocolVersion) && !Modern(p.ProtocolVersion) {
		return p.ProtocolVersion
	}
	return Latest
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
	if s.ExtrasOnly {
		return s.callExtra(ctx, name, raw)
	}
	switch name {
	case "request_tools":
		return s.executeRequestTools(ctx, raw)

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

	case "mcpx_batch", "batch_call":
		var p struct {
			Calls []struct {
				Namespace string          `json:"namespace"`
				Tool      string          `json:"tool"`
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
				Args      json.RawMessage `json:"args"`
			} `json:"calls"`
			StopOnError *bool `json:"stopOnError"`
			Parallel    bool  `json:"parallel"`
		}
		if err := arg(&p); err != nil {
			return "", err
		}
		if len(p.Calls) == 0 {
			return `{"results":[]}`, nil
		}
		stopOnErr := true
		if p.StopOnError != nil {
			stopOnErr = *p.StopOnError
		}
		items := make([]batchCallItem, len(p.Calls))
		for i, c := range p.Calls {
			items[i] = batchCallItem{
				Namespace: c.Namespace,
				Tool:      c.Tool,
				Name:      c.Name,
				Arguments: c.Arguments,
				Args:      c.Args,
			}
		}
		return s.executeBatch(ctx, items, stopOnErr, p.Parallel)
	}
	return s.callExtra(ctx, name, raw)
}

type batchCallItem struct {
	Namespace string          `json:"namespace"`
	Tool      string          `json:"tool"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Args      json.RawMessage `json:"args"`
}

type batchItemResult struct {
	Index   int    `json:"index"`
	Tool    string `json:"tool"`
	Status  string `json:"status"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
	IsError bool   `json:"isError,omitempty"`
}

func (s *Server) executeBatch(ctx context.Context, calls []batchCallItem, stopOnError bool, parallel bool) (string, error) {
	results := make([]batchItemResult, len(calls))

	runOne := func(ctx context.Context, i int, call batchCallItem) (batchItemResult, error) {
		toolName := call.Tool
		if toolName == "" {
			toolName = call.Name
		}
		ns := call.Namespace
		if ns == "" && strings.Contains(toolName, ".") {
			ns, toolName, _ = strings.Cut(toolName, ".")
		}
		args := call.Arguments
		if len(args) == 0 {
			args = call.Args
		}
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}

		if toolName == "mcpx_batch" || toolName == "batch_call" {
			return batchItemResult{
				Index:   i,
				Tool:    toolName,
				Status:  "error",
				Error:   "nested batch calls are not supported",
				IsError: true,
			}, errors.New("nested batch calls are not supported")
		}

		var out string
		var err error
		if ns != "" {
			out, err = s.backend.Call(ctx, ns, toolName, args)
		} else {
			out, err = s.invoke(ctx, toolName, args)
		}

		if err != nil {
			return batchItemResult{
				Index:   i,
				Tool:    toolName,
				Status:  "error",
				Error:   err.Error(),
				IsError: true,
			}, err
		}

		var parsed any
		if json.Unmarshal([]byte(out), &parsed) == nil {
			return batchItemResult{
				Index:  i,
				Tool:   toolName,
				Status: "success",
				Result: parsed,
			}, nil
		}
		return batchItemResult{
			Index:  i,
			Tool:   toolName,
			Status: "success",
			Result: out,
		}, nil
	}

	if parallel {
		var wg sync.WaitGroup
		wg.Add(len(calls))
		for i, call := range calls {
			go func(i int, call batchCallItem) {
				defer wg.Done()
				res, _ := runOne(ctx, i, call)
				results[i] = res
			}(i, call)
		}
		wg.Wait()
	} else {
		for i, call := range calls {
			res, err := runOne(ctx, i, call)
			results[i] = res
			if stopOnError && err != nil {
				results = results[:i+1]
				break
			}
		}
	}

	b, err := json.Marshal(map[string]any{"results": results})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// instructions is what initialize says this server is for.
func (s *Server) instructions() string {
	if s.ExtrasOnly {
		return s.OwnInstructions
	}
	return Instructions
}

// callExtra runs a contributed tool by name.
func (s *Server) callExtra(ctx context.Context, name string, raw json.RawMessage) (string, error) {
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
	// The first loop is the process's own client and takes the default
	// connection, which SetPush and Cancelled address. A second loop on the
	// same Server gets a connection of its own: sharing one sent each
	// loop's questions to whichever had installed its writer last.
	c := s.conn()
	s.mu.Lock()
	if s.stdioBusy {
		c = s.newConn(newSessionID(), nil)
		c.process = true
	} else {
		s.stdioBusy = true
		defer func() { s.mu.Lock(); s.stdioBusy = false; s.mu.Unlock() }()
	}
	s.mu.Unlock()
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
		v = Headerless // see Headerless: a batch before initialize is 2025-03-26's
	}
	// An element that names its own revision speaks for itself: a
	// 2026-07-28 request in an array is a batch that revision forbids,
	// whatever this process was (not) initialized as.
	for _, e := range elems {
		var el struct {
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(e, &el) == nil {
			if rv := requestVersion(el.Params); rv != "" && !Defines(rv, FeatBatch) {
				return batchRefused(rv)
			}
		}
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
	if bad := s.checkParamHeaders(r, req); bad != nil {
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
	ctx := withKeepAlive(withSender(r.Context(), ex.send), ex.comment)
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
		bad := unsupportedVersion(req.ID, nil)
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
	stopQuiet := s.keepQuietAlive(r, ex, req)
	resp := s.HandleOn(withSender(ctx, ex.send), c, req)
	stopQuiet()
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
		version = Headerless // no header, no session: see Headerless
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
// Only initialize issues one. server/discover used to as well, so that a
// modern client had an identity to bind a requestState to; but 2026-07-28
// has no sessions, and a requestState is now bound to the request that
// carries it (see requestBinding), which needs none.
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
	if req.Method != "initialize" {
		// Stateless. Correct for every modern request and for a legacy one
		// that will never be asked anything, and the alternative -- minting
		// a session per request -- is a map that only grows.
		c := s.newConn("", nil)
		if !modern {
			// No handshake on this connection, so the header is the version:
			// 2025-06-18 on send it, and absent means 2025-03-26.
			v := r.Header.Get("MCP-Protocol-Version")
			if v == "" {
				v = Headerless
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

// comment writes an SSE comment line, which a client ignores and a proxy
// counts as traffic. Only on a stream already open: a keep-alive is never
// what turns a plain reply into a stream.
func (e *httpExchange) comment() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.streaming || e.flusher == nil {
		return nil
	}
	if _, err := io.WriteString(e.w, ": keep-alive\n\n"); err != nil {
		return err
	}
	e.flusher.Flush()
	return nil
}

// keepQuietAlive opens the event stream of a request that has gone quiet
// for SSEKeepAlive, and from then on writes a comment every SSEKeepAlive
// until the answer is ready. The returned func stops it, and returns only
// once nothing more will be written.
//
// A legacy POST was answered with nothing at all -- not even headers --
// until the upstream call behind it finished, which pool.callTimeout allows
// to take two minutes. A client or a proxy with an idle timeout gave up on
// a call that was still running: the official suite's server-sse-polling
// scenario did, at 30 seconds, while an upstream that had lost the result
// kept mcpx waiting for it. A client that offered text/event-stream accepts
// either answer; one that did not keeps getting JSON.
func (s *Server) keepQuietAlive(r *http.Request, ex *httpExchange, req request) func() {
	if len(req.ID) == 0 || ex.flusher == nil ||
		!strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	every := s.Timing.resolved().SSEKeepAlive
	go func() {
		defer close(done)
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-r.Context().Done():
				return
			case <-tick.C:
				ex.open()
				if ex.comment() != nil {
					return
				}
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
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

// capabilities declares what this server can actually do, in the shape the
// revision defines.
//
// Push-dependent capabilities are declared only when something can push, and
// "something can push" is a property of the connection rather than of the
// server. It used to be tested as s.Notify != nil, which is true for every
// connection the daemon serves -- including the HTTP ones, which are built
// with no send function at all and can therefore deliver nothing.
//
// The two eras reach a client differently. A legacy client receives
// list_changed unsolicited, so it needs a connection that can push. A modern
// client receives it only on a subscriptions/listen stream it opened, and
// that stream is the listen request's own response -- over HTTP as much as
// over stdio -- so a transport that can stream a response can deliver it.
func (s *Server) capabilities(ctx context.Context, version string, c *Conn) map[string]any {
	push := s.Notify != nil && c != nil && c.canPush()
	if Modern(version) {
		push = s.Notify != nil && ((c != nil && c.canPush()) || senderFrom(ctx) != nil)
	}
	caps := map[string]any{
		// mcpx's own tool list is fixed when the server is built, so it
		// declares listChanged only in pass-through mode, where tools/list
		// is the upstreams' list and changes when theirs does. It used to
		// be declared for the fixed list too, and a client re-listed on
		// every upstream change and got the same ten tools back.
		"tools":     map[string]any{"listChanged": push && s.toolsVary()},
		"resources": map[string]any{"subscribe": push, "listChanged": push},
		"prompts":   map[string]any{"listChanged": push},
	}
	if Defines(version, FeatLoggingSetLevel) && !Modern(version) {
		// mcpx relays its upstreams' log messages to a client that set a
		// level, during that client's calls. 2026-07-28 has no capability
		// for it: the client names a level on each request.
		caps["logging"] = map[string]any{}
	}
	if Defines(version, FeatCompletions) {
		// completion/complete is answered for every revision; only the
		// capability that declares it is newer than 2024-11-05.
		caps["completions"] = map[string]any{}
	}
	if Defines(version, FeatTasks) {
		// Core tasks, 2025-11-25 only. 2026-07-28 removed this capability
		// and the SEP that did so says a server MUST NOT keep advertising
		// it under a revision that has the extension.
		caps["tasks"] = map[string]any{
			"list": map[string]any{}, "cancel": map[string]any{},
			"requests": map[string]any{"tools": map[string]any{"call": map[string]any{}}},
		}
	}
	if Defines(version, FeatExtensions) {
		// ServerCapabilities.extensions exists only in 2026-07-28. mcpx
		// sent it to 2025-11-25 as well, whose schema has no such field.
		caps["extensions"] = map[string]any{
			ExtTasks: map[string]any{},
		}
		if len(s.skills()) > 0 {
			// directoryRead: resources/directory/read lists any
			// directory of a served skill. Declared only here, where
			// skills are served, since it lists nothing else.
			caps["extensions"].(map[string]any)[ExtSkills] = map[string]any{"directoryRead": true}
		}
	}
	return caps
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
//
// A cursor mcpx did not issue is an error (-32602), as every revision's
// pagination page says, rather than a quiet restart at the first page: a
// client that loops on nextCursor would otherwise read page one forever.
// A cursor past the end of a list that has since shrunk is still one mcpx
// issued, and gets an empty last page.
func page[T any](all []T, params json.RawMessage, size int) ([]T, string, error) {
	start := 0
	if len(params) > 0 {
		var p struct {
			Cursor *string `json:"cursor"`
		}
		if json.Unmarshal(params, &p) == nil && p.Cursor != nil {
			n, err := decodeCursor(*p.Cursor)
			if err != nil {
				return nil, "", fmt.Errorf("invalid cursor %q", *p.Cursor)
			}
			start = n
		}
	}
	if start >= len(all) {
		return []T{}, "", nil
	}
	end := start + size
	if end >= len(all) {
		return all[start:], "", nil
	}
	return all[start:end], encodeCursor(end), nil
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
	n, err := strconv.Atoi(s[2:])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad cursor")
	}
	return n, nil
}

// cancel records a client's cancellation and cancels the request it names,
// if this connection is still answering it.
func (c *Conn) cancel(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
		Reason    string          `json:"reason"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	// On stdio this is how a client ends a subscriptions/listen: there is
	// no per-request stream to close.
	c.cancelListen(p.RequestID)
	id := listenKey(p.RequestID)
	if unq, err := strconv.Unquote(id); err == nil {
		id = unq // a string id is recorded as the string, not its JSON
	}
	c.mu.Lock()
	if c.cancelled == nil {
		c.cancelled = map[string]string{}
	}
	c.cancelled[id] = p.Reason
	c.mu.Unlock()
	if c.s.OnCancel != nil {
		c.s.OnCancel(id, p.Reason)
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

// complete answers completion/complete by asking the server that owns the
// prompt or template.
//
// It answered from mcpx's own tool names and ignored ref, so a client
// completing an argument of a pass-through prompt was offered tool names,
// while /v1/complete asked the upstream server the same question and got
// real values (conflict #9). The ref names a prompt or template mcpx only
// passes through, so the only server that knows its argument values is the
// one behind it.
//
// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/completion#error-handling
// -- an unknown prompt or a malformed ref is -32602; a failure to reach the
// server that would answer is -32603.
func (s *Server) complete(ctx context.Context, params json.RawMessage) (map[string]any, *rpcError) {
	var p struct {
		Ref struct {
			Type string `json:"type"`
		} `json:"ref"`
	}
	_ = json.Unmarshal(params, &p)
	if p.Ref.Type != "ref/prompt" && p.Ref.Type != "ref/resource" {
		return nil, &rpcError{Code: codeInvalidParams,
			Message: `ref.type is "ref/prompt" or "ref/resource", got "` + p.Ref.Type + `"`}
	}
	var values []string
	if s.backend != nil {
		got, err := s.backend.Complete(ctx, params)
		if err != nil {
			code := codeInternal
			if errors.Is(err, ErrInvalidParams) {
				code = codeInvalidParams
			}
			return nil, &rpcError{Code: code, Message: err.Error()}
		}
		values = got
	}
	// The ceiling the specification sets. Read per request, because
	// completion.maxValues is a hot setting: a value read once when the
	// server was built made /v1/complete follow a change and this not.
	max := defaults.CompletionValues
	if s.MaxCompletions != nil {
		if n := s.MaxCompletions(); n > 0 {
			max = n
		}
	}
	// The setting may lower the count, not raise it past what every
	// revision's CompleteResult allows.
	if max > specMaxCompletions {
		max = specMaxCompletions
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
	}, nil
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

// specMaxCompletions is CompleteResult.values' maxItems in every revision: a
// fact of the protocol, not a default.
const specMaxCompletions = 100

// hasTool reports whether tools/call can reach name.
func (s *Server) hasTool(ctx context.Context, name string) (bool, error) {
	all, err := s.surface(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range all {
		if t.Name == name {
			return true, nil
		}
	}
	// On-demand auto-adaptation: if in reactive mode and tool exists in allAvailableTools,
	// seamlessly adapt the working set, notify clients/sessions, and allow the call!
	if s.Reactive {
		avail, aerr := s.allAvailableTools(ctx)
		if aerr == nil {
			for _, t := range avail {
				if t.Name == name {
					if s.ReactiveWorkingSet == nil {
						s.ReactiveWorkingSet = catalog.NewWorkingSet(24, 2*time.Hour, s.PinnedTools)
					}
					s.ReactiveWorkingSet.Add("", name, catalog.OriginSticky)
					s.NotifyToolsListChanged()
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// invoke runs a tool by name: the pass-through upstream's when the name is
// its, otherwise the gateway's. Every route that calls a tool by name goes
// through here, so pass-through cannot reach one route and miss another.
func (s *Server) invoke(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if s.Reactive && s.ReactiveWorkingSet != nil {
		if !s.ReactiveWorkingSet.Contains("", name) {
			s.ReactiveWorkingSet.Add("", name, catalog.OriginSticky)
			s.NotifyToolsListChanged()
		} else {
			s.ReactiveWorkingSet.Touch("", name)
		}
	} else if s.ReactiveWorkingSet != nil {
		s.ReactiveWorkingSet.Touch("", name)
	}
	if name == "request_tools" {
		return s.executeRequestTools(ctx, args)
	}
	pass, err := s.routesToPass(ctx, name)
	if err != nil {
		return "", err
	}
	if pass {
		ns, _ := s.passOwner(ctx, name)
		if ns == "" {
			// A name no upstream lists: the first upstream given decides
			// whether it exists, as a bare resource URI nobody lists goes
			// to the first (passthrough.go).
			ns = s.Passthrough[0]
		}
		return s.backend.Call(ctx, ns, name, args)
	}
	return s.dispatch(ctx, name, args)
}

// promptArgs finds a prompt by its exact name and reports the first
// required argument not supplied. A prompt it cannot find, or a listing that
// fails, is left to the backend, which also accepts a name without its
// namespace.
func (s *Server) promptArgs(ctx context.Context, name string, args map[string]string) (missing string) {
	refs, err := s.backend.Prompts(ctx)
	if err != nil {
		return ""
	}
	for _, r := range refs {
		if r.Name != name {
			continue
		}
		for _, a := range r.Arguments {
			if _, ok := args[a.Name]; a.Required && !ok {
				return a.Name
			}
		}
		return ""
	}
	return ""
}
