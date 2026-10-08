// Package api declares the daemon's /v1 surface once.
//
// The principle is that anything the CLI can do can be done from a plugin,
// from /v1, and from MCP. That only holds if the three agree, and they only
// agree if they are generated from the same declaration: a route table in
// one file, a tool list in another and a specification in a third drift
// apart within a release, and the drift is invisible until somebody asks for
// the thing that was forgotten.
//
// So every /v1 operation is declared here, with enough detail to build the
// route's documentation, an MCP tool that proxies it, and an OpenAPI
// document. A parity test then fails if a route exists without an entry, an
// entry without a route, or a non-streaming entry without a tool.
package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// In says where a parameter travels.
type In string

// The three places a parameter can be.
const (
	InPath  In = "path"
	InQuery In = "query"
	InBody  In = "body"
)

// Param is one input an operation takes.
type Param struct {
	Name string
	In   In
	// Type is a JSON Schema type: string, integer, boolean, object, array.
	Type     string
	Desc     string
	Required bool
	Enum     []string
	// Schema overrides the generated property schema, for the few inputs
	// whose shape is more than a scalar.
	Schema string
	// Raw marks the parameter that is the request body itself, sent as
	// bytes rather than as a field of a JSON object. An artifact is a file,
	// and wrapping a file in JSON to unwrap it again on the other side would
	// cost a base64 pass in each direction for nothing.
	Raw bool
	// DefaultCwd fills an absent directory parameter, on the command line
	// only, with the caller's working directory. Only the CLI has a working
	// directory that is the caller's; an MCP host's or a plugin's is
	// somebody else's, so there the parameter stays required.
	DefaultCwd bool
}

// Op is one /v1 operation.
type Op struct {
	// Name is snake_case and unique; it is the operationId, and the MCP
	// tool name is derived from it.
	Name        string
	Method      string
	Path        string
	Summary     string
	Description string
	Params      []Param

	// Admin marks an operation that changes the daemon rather than reading
	// it. Carried here so a later OAuth pass has one place to read scopes
	// from, and so the MCP tool can say so in its description.
	Admin bool
	// Mutating marks anything that is not a plain read.
	Mutating bool
	// Destructive marks an operation a caller cannot undo -- stopping the
	// daemon, restarting a server. It maps to the MCP destructiveHint.
	Destructive bool
	// Idempotent marks a mutating operation that has no further effect when
	// repeated with the same arguments -- MCP's idempotentHint, which a
	// client may use to retry without asking. Reads are idempotent without
	// saying so. It is declared rather than derived: "destructive implies
	// idempotent" made restart claim it, and a second restart kills the
	// calls the first one let start.
	Idempotent bool
	// Streams marks an endpoint whose response never ends. MCP cannot carry
	// a stream as a tool result, so these are excluded from tools; the MCP
	// equivalent is subscriptions/listen.
	Streams bool
	// CoveredBy names a hand-written MCP tool that already exposes this
	// operation. Empty means a proxy tool is generated as mcpx_<Name>.
	CoveredBy string
	// Text marks a response that is text/plain rather than JSON.
	Text bool

	// Command names the hand-written CLI command that reaches this
	// operation, as the words after `mcpx` ("ls", "elicit answer"). Empty
	// means the command line reaches it through a command generated from
	// this entry instead, so an operation added here is reachable from the
	// shell the moment it is declared.
	Command string
	// CLI overrides the words of the generated command. Empty derives them
	// from Name, "task_get" becoming `mcpx task get`.
	CLI string
}

// CLIWords is how the command line reaches this operation: the hand-written
// command when there is one, otherwise the generated one.
func (o Op) CLIWords() []string {
	switch {
	case o.Command != "":
		return strings.Fields(o.Command)
	case o.CLI != "":
		return strings.Fields(o.CLI)
	}
	return strings.Split(o.Name, "_")
}

// GeneratedCommand reports whether the CLI reaches this operation through a
// command built from this entry rather than a hand-written one.
func (o Op) GeneratedCommand() bool { return o.Command == "" }

// RawParam is the parameter that is the whole request body, if any.
func (o Op) RawParam() (Param, bool) {
	for _, p := range o.Params {
		if p.Raw {
			return p, true
		}
	}
	return Param{}, false
}

// ToolName is the MCP tool that reaches this operation.
func (o Op) ToolName() string {
	if o.CoveredBy != "" {
		return o.CoveredBy
	}
	return "mcpx_" + o.Name
}

// Generated reports whether this operation needs a proxy tool built for it.
func (o Op) Generated() bool { return !o.Streams && o.CoveredBy == "" }

// Route is the "METHOD /path" key a Go 1.22 mux registers under.
func (o Op) Route() string { return o.Method + " " + o.Path }

// profileParams are the namespace-selection query parameters the discovery
// routes share. Repeated by value rather than referenced, so each operation's
// schema stands alone.
func profileParams() []Param {
	return []Param{
		{Name: "profile", In: InQuery, Type: "string", Desc: "profile names, comma separated"},
		{Name: "skipDefault", In: InQuery, Type: "string", Enum: []string{"0", "1"}, Desc: "1 to exclude the default profile"},
		{Name: "allProfiles", In: InQuery, Type: "string", Enum: []string{"0", "1"}, Desc: "1 to include every profile"},
	}
}

func nsParam() Param {
	return Param{Name: "ns", In: InQuery, Type: "string",
		Desc: "namespaces to restrict to, comma separated"}
}

func callContextParams() []Param {
	return []Param{
		{Name: "session", In: InBody, Type: "string", Desc: "the caller's session id, which decides which pooled instance serves the call"},
		{Name: "context", In: InBody, Type: "object", Desc: "full call context: sessionId, callId, parentSessionId, cwd, pid, ephemeral",
			Schema: `{"type":"object","additionalProperties":true}`},
	}
}

// logFilterParams are exactly the filters `mcpx log` accepts, so a query over
// HTTP and a query at the prompt select the same records.
func logFilterParams() []Param {
	return []Param{
		{Name: "since", In: InQuery, Type: "string", Desc: "start of the window: a duration (15m, 2h) or an RFC3339 time"},
		{Name: "until", In: InQuery, Type: "string", Desc: "end of the window: a duration before now, or an RFC3339 time"},
		{Name: "level", In: InQuery, Type: "string", Enum: []string{"debug", "info", "warn", "error"}, Desc: "minimum level"},
		{Name: "event", In: InQuery, Type: "string", Desc: "event glob: server.*, mcp.call"},
		{Name: "server", In: InQuery, Type: "string", Desc: "only records from this server or namespace"},
		{Name: "tool", In: InQuery, Type: "string", Desc: "only records for this tool"},
		{Name: "session", In: InQuery, Type: "string", Desc: "only records from this session"},
		{Name: "trace", In: InQuery, Type: "string", Desc: "only records carrying this trace id"},
		{Name: "chain", In: InQuery, Type: "string", Desc: "this trace and every ancestor, oldest first"},
		{Name: "grep", In: InQuery, Type: "string", Desc: "regular expression over the message and the attributes"},
		{Name: "limit", In: InQuery, Type: "integer", Desc: "maximum records"},
		{Name: "reverse", In: InQuery, Type: "string", Enum: []string{"0", "1"}, Desc: "1 for newest first"},
	}
}

// Ops is every operation the daemon serves under /v1.
//
// Ordered by path for reading, not by importance: this is a reference, and a
// reference that is sorted can be scanned.
func Ops() []Op {
	ops := []Op{
		{
			Name: "health", Method: "GET", Path: "/v1/health",
			Summary:     "Liveness, version and the loopback endpoint",
			Description: "The cheapest way to find out whether a daemon is answering at all.",
		},
		{
			Name: "status", Method: "GET", Path: "/v1/status",
			Command: "status",
			Summary: "The daemon, its pools and live instances",
			Description: "Everything mcpx knows about itself: version, uptime, socket, " +
				"config path, and one entry per configured server with its live " +
				"instance count.",
			CoveredBy: "mcpx_status",
		},
		{
			Name: "events", Method: "GET", Path: "/v1/events",
			Summary: "Server-sent event stream",
			Description: "Everything the daemon notices, as SSE. Not reachable as an MCP " +
				"tool: a tool result is one value and this is a stream that does not " +
				"end. The MCP equivalent is subscriptions/listen, which pushes the " +
				"same events as notifications.",
			Streams: true,
			Params: []Param{
				{Name: "kinds", In: InQuery, Type: "string", Desc: "kind prefixes, comma separated; empty means everything"},
				{Name: "session", In: InQuery, Type: "string", Desc: "only events belonging to this session"},
				{Name: "server", In: InQuery, Type: "string", Desc: "only events about this server"},
				{Name: "uri", In: InQuery, Type: "string", Desc: "resource URIs, comma separated"},
				{Name: "since", In: InQuery, Type: "integer", Desc: "replay everything after this sequence number"},
			},
		},
		{
			Name: "elicit_list", Method: "GET", Path: "/v1/elicit",
			Command:     "elicit list",
			Summary:     "Questions servers are waiting on an answer for",
			Description: "Elicitations and sampling requests that have not been answered yet.",
			Params: []Param{
				{Name: "session", In: InQuery, Type: "string", Desc: "only questions raised by this session"},
				{Name: "audience", In: InQuery, Type: "string", Desc: "who the question is for"},
			},
		},
		{
			Name: "elicit_get", Method: "GET", Path: "/v1/elicit/{id}",
			Command:     "elicit show",
			Summary:     "One question, with its answer once it has one",
			Description: "Poll this after answering to see what was recorded.",
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the elicitation id"},
			},
		},
		{
			Name: "elicit_answer", Method: "POST", Path: "/v1/elicit/{id}/{action}",
			Command: "elicit answer",
			Summary: "Answer a question a server asked",
			Description: "The action is in the path and the body is only ever content, so " +
				"there is no way to send an accept with the wrong shape or a decline " +
				"carrying content that will be ignored. Answering on a user's behalf " +
				"is a privileged act: the answer goes to the server as though the " +
				"person at the keyboard gave it.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the id of the question being answered"},
				{Name: "action", In: InPath, Type: "string", Required: true, Enum: []string{"accept", "decline", "cancel"}, Desc: "accept, decline or cancel"},
				{Name: "content", In: InBody, Type: "object", Desc: "the accepted content; ignored for decline and cancel",
					Schema: `{"type":"object","additionalProperties":true}`},
			},
		},
		{
			Name: "log_record", Method: "POST", Path: "/v1/log",
			Command: "log record",
			Summary: "Append one record to the durable log",
			Description: "`mcpx log record` without the process. Anything not recognised " +
				"becomes an attribute, so a caller that can produce JSON does not " +
				"also have to learn a schema. Writes into mcpx's own log, which is " +
				"why it counts as privileged.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "level", In: InQuery, Type: "string", Enum: []string{"debug", "info", "warn", "error"}, Desc: "the level to record at: debug, info, warn or error"},
				{Name: "record", In: InBody, Type: "object", Required: true, Desc: "the record; msg, event, server, tool and duration are recognised, the rest become attributes",
					Schema: `{"type":"object","additionalProperties":true}`},
			},
		},
		{
			Name: "log_query", Method: "GET", Path: "/v1/log",
			Command: "log",
			Summary: "Query the durable log",
			Description: "The same filters `mcpx log` accepts, over HTTP. Use it to find " +
				"out why something did not work without re-running it. With chain " +
				"set, the answer is the trace and every ancestor as a tree rather " +
				"than a flat list.",
			Params: logFilterParams(),
		},
		{
			Name: "stats_query", Method: "GET", Path: "/v1/stats",
			Command: "stats",
			Summary: "Aggregate the log into numbers",
			Description: "`mcpx stats` as JSON: calls, servers, instances, errors, " +
				"sessions, volume or slowest. Answers 'what is slow' and 'what keeps " +
				"failing' without reading records one at a time.",
			Params: []Param{
				{Name: "by", In: InQuery, Type: "string", Desc: "the dimension (default calls)",
					Enum: []string{"calls", "servers", "instances", "errors", "sessions", "volume", "slowest"}},
				{Name: "since", In: InQuery, Type: "string", Desc: "start of the window: a duration or an RFC3339 time"},
				{Name: "until", In: InQuery, Type: "string", Desc: "end of the window"},
				{Name: "server", In: InQuery, Type: "string", Desc: "restrict to this server"},
				{Name: "tool", In: InQuery, Type: "string", Desc: "restrict to this tool"},
				{Name: "session", In: InQuery, Type: "string", Desc: "restrict to this session"},
				{Name: "top", In: InQuery, Type: "integer", Desc: "rows to show where the dimension is a ranking"},
			},
		},
		{
			Name: "registry_search", Method: "GET", Path: "/v1/registry/search",
			Command: "registry search",
			Summary: "Search a public registry of MCP servers",
			Description: "Servers that are not configured here yet. The registry matches " +
				"names as a substring, so one word finds more than a phrase. The " +
				"answer carries enough to add one with `mcpx registry add`, and " +
				"`truncated` is true when the registry had more than was returned.",
			Params: []Param{
				{Name: "q", In: InQuery, Type: "string", Required: true, Desc: "one word; matched against server names"},
				{Name: "limit", In: InQuery, Type: "integer", Desc: "results to return"},
			},
		},
		{
			Name: "complete", Method: "POST", Path: "/v1/complete",
			Summary: "Argument autocomplete from an upstream server",
			Description: "Forwards MCP completion/complete to the named server, so a " +
				"client offering completion over mcpx offers what the server itself " +
				"would -- which knows the values an argument may take, where mcpx " +
				"knows only the names it has cached. A server that never declared " +
				"the completions capability is not asked; the cached names are " +
				"returned instead and `source` says which of the two you are " +
				"looking at. An unimplemented completion answers with an empty " +
				"value list rather than an error, because a client that sees " +
				"method-not-found shows nothing and the user concludes the feature " +
				"is broken.",
			Params: append([]Param{
				{Name: "server", In: InBody, Type: "string", Required: true, Desc: "server name or namespace"},
				{Name: "ref", In: InBody, Type: "object", Required: true, Desc: "what is being completed",
					Schema: `{"type":"object","properties":{` +
						`"type":{"type":"string","enum":["ref/prompt","ref/resource"]},` +
						`"name":{"type":"string","description":"with ref/prompt"},` +
						`"uri":{"type":"string","description":"with ref/resource"}},` +
						`"required":["type"],"additionalProperties":false}`},
				{Name: "argument", In: InBody, Type: "object", Required: true, Desc: "the argument being typed",
					Schema: `{"type":"object","properties":{"name":{"type":"string"},"value":{"type":"string"}},` +
						`"required":["name"],"additionalProperties":false}`},
				{Name: "arguments", In: InBody, Type: "object", Desc: "values already chosen for the prompt's other arguments; forwarded as the request's context.arguments",
					Schema: `{"type":"object","additionalProperties":{"type":"string"}}`},
			}, callContextParams()...),
		},
		{
			Name: "resource_templates", Method: "GET", Path: "/v1/resource-templates",
			Command:     "resources --templates",
			Summary:     "Every templated resource the configured servers offer",
			Description: "Parameterised resources, namespaced.",
			Params:      []Param{nsParam()},
		},
		{
			Name: "prompts", Method: "GET", Path: "/v1/prompts",
			Command: "prompts",
			Summary: "Every prompt the configured servers offer",
			Description: "Prompts are the part of MCP that is not tools: a server saying " +
				"'here is the wording that works for this'.",
			Params: []Param{nsParam()},
		},
		{
			Name: "resources", Method: "GET", Path: "/v1/resources",
			Command: "resources",
			Summary: "Every resource the configured servers offer",
			Params:  []Param{nsParam()},
		},
		{
			Name: "prompt_get", Method: "POST", Path: "/v1/prompt",
			Command:     "prompts",
			Summary:     "Render one prompt with its arguments filled in",
			Description: "The server does the substitution; mcpx passes the arguments through.",
			Params: []Param{
				{Name: "server", In: InBody, Type: "string", Required: true, Desc: "the server holding the prompt"},
				{Name: "name", In: InBody, Type: "string", Required: true, Desc: "the prompt's name, as it appears in the server's prompt list"},
				{Name: "arguments", In: InBody, Type: "object", Desc: "string values, by argument name",
					Schema: `{"type":"object","additionalProperties":{"type":"string"}}`},
				{Name: "sessionId", In: InBody, Type: "string", Desc: "the session to attribute the call to"},
				{Name: "callId", In: InBody, Type: "string", Desc: "an id for this call, used to tie elicitations back to it"},
			},
		},
		{
			Name: "namespaces", Method: "GET", Path: "/v1/namespaces",
			Command:     "ls",
			Summary:     "Every configured server, with tool counts",
			Description: "Small, and it starts nothing.",
			CoveredBy:   "mcpx_namespaces",
			Params:      profileParams(),
		},
		{
			Name: "tools", Method: "GET", Path: "/v1/tools",
			Summary:     "Every tool of every configured server",
			Description: "The full list, with input schemas. Large by design; mcpx_catalog is the budgeted view.",
			Params:      []Param{nsParam()},
		},
		{
			Name: "search", Method: "GET", Path: "/v1/search",
			Command:   "search",
			Summary:   "Rank tools against a query",
			CoveredBy: "mcpx_search",
			Params: []Param{
				{Name: "q", In: InQuery, Type: "string", Required: true, Desc: "what to match against tool names and descriptions"},
				{Name: "limit", In: InQuery, Type: "integer", Desc: "how many results to return"},
				{Name: "semantic", In: InQuery, Type: "boolean", Desc: "rank results using local dense vector embeddings"},
			},
		},
		{
			Name: "types", Method: "GET", Path: "/v1/types",
			Command:   "types",
			Summary:   "TypeScript declarations for the named namespaces",
			CoveredBy: "mcpx_types", Text: true,
			Params: append([]Param{
				nsParam(),
				{Name: "instructions", In: InQuery, Type: "string", Enum: []string{"0", "1"}, Desc: "0 to omit each server's own guidance"},
			}, profileParams()...),
		},
		{
			Name: "catalog", Method: "GET", Path: "/v1/catalog",
			Command:   "catalog",
			Summary:   "Every namespace with as many signatures as fit a token budget",
			CoveredBy: "mcpx_catalog", Text: true,
			Params: append([]Param{
				nsParam(),
				{Name: "budget", In: InQuery, Type: "integer", Desc: "approximate token ceiling"},
				{Name: "bias", In: InQuery, Type: "string", Desc: "words that pull matching tools toward the front"},
			}, profileParams()...),
		},
		{
			Name: "client_module", Method: "GET", Path: "/v1/client.ts",
			Command: "client",
			Summary: "The generated TypeScript client",
			Description: "A module binding every tool as an async function. This is what " +
				"a script imports.",
			Text: true,
			Params: append([]Param{
				nsParam(),
				{Name: "session", In: InQuery, Type: "string", Desc: "session id baked into the module"},
			}, profileParams()...),
		},
		{
			Name: "globals", Method: "GET", Path: "/v1/globals.d.ts",
			Summary:     "Ambient declarations for the installed globals",
			Description: "What an editor needs to typecheck a script that uses `tools` without importing it.",
			Text:        true,
			Params:      append([]Param{nsParam()}, profileParams()...),
		},
		{
			Name: "call", Method: "POST", Path: "/v1/call",
			Command: "call",
			Summary: "Call one tool on one server",
			Description: "With task set, the answer is a handle rather than a result: the " +
				"call runs in the background and is collected from /v1/tasks. That is " +
				"the right shape for a genuinely slow tool, where holding a request " +
				"open for minutes invites every intermediary to time it out.",
			CoveredBy: "mcpx_call", Mutating: true,
			Params: append([]Param{
				{Name: "server", In: InBody, Type: "string", Required: true, Desc: "server name or namespace"},
				{Name: "tool", In: InBody, Type: "string", Required: true, Desc: "the tool's name, as it appears in this server's tool list"},
				{Name: "args", In: InBody, Type: "object", Desc: "the tool's arguments",
					Schema: `{"type":"object","additionalProperties":true}`},
				{Name: "task", In: InBody, Type: "object", Desc: "run as a task and return a handle at once",
					Schema: `{"type":"object","properties":{"ttl":{"type":"integer","description":"milliseconds the result is kept"}},"additionalProperties":false}`},
			}, callContextParams()...),
		},
		{
			Name: "batch", Method: "POST", Path: "/v1/batch",
			Command: "batch",
			Summary: "Execute multiple tool calls in a single turn",
			Description: "Executes multiple calls sequentially or in parallel, returning all results in a single response.",
			CoveredBy: "mcpx_batch", Mutating: true,
			Params: append([]Param{
				{Name: "calls", In: InBody, Type: "array", Required: true, Desc: "list of tool calls to execute",
					Schema: `{"type":"array","items":{"type":"object","properties":{"server":{"type":"string"},"tool":{"type":"string"},"args":{"type":"object"}},"required":["tool"]}}`},
				{Name: "stopOnError", In: InBody, Type: "boolean", Desc: "stop execution on first failure (default true)"},
				{Name: "parallel", In: InBody, Type: "boolean", Desc: "execute calls concurrently (default false)"},
			}, callContextParams()...),
		},
		{
			Name: "resource_read", Method: "POST", Path: "/v1/resource",
			Command:  "resources",
			Summary:  "Read one resource from one server",
			Mutating: true,
			Params: append([]Param{
				{Name: "server", In: InBody, Type: "string", Required: true, Desc: "the server holding the resource"},
				{Name: "uri", In: InBody, Type: "string", Required: true, Desc: "the resource's URI, as it appears in the server's resource list"},
			}, callContextParams()...),
		},
		{
			Name: "tasks_list", Method: "GET", Path: "/v1/tasks",
			CLI:         "task list",
			Summary:     "Background calls and their status",
			Description: "Tasks expire: a result nobody collects is memory nobody frees.",
		},
		{
			Name: "task_get", Method: "GET", Path: "/v1/tasks/{id}",
			Summary:     "One task's status",
			Description: "Check on it. /v1/tasks/{id}/result waits for it.",
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the task's id, as returned when it was started"},
			},
		},
		{
			Name: "task_result", Method: "GET", Path: "/v1/tasks/{id}/result",
			Summary: "Wait for a task and collect its result",
			Description: "Blocks until the task ends, bounded by a timeout, after which it " +
				"answers 408 and the task keeps running. A caller that would rather " +
				"poll should use /v1/tasks/{id}.",
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the task's id, as returned when it was started"},
				{Name: "waitMs", In: InQuery, Type: "integer", Desc: "how long to wait before giving up"},
			},
		},
		{
			Name: "task_cancel", Method: "POST", Path: "/v1/tasks/{id}/cancel",
			Idempotent: true,
			Summary:    "Cancel a running task",
			Mutating:   true,
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the task's id, as returned when it was started"},
			},
		},
		{
			Name: "session_release", Method: "POST", Path: "/v1/session/release",
			Summary: "Free the instances a finished caller created",
			Description: "Called when a session ends. Releasing somebody else's session " +
				"stops servers they are still using, which is why it is privileged.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "session", In: InBody, Type: "string", Required: true, Desc: "the session whose instances should be released"},
			},
		},
		{
			Name: "refresh", Method: "POST", Path: "/v1/refresh",
			Command: "refresh",
			Summary: "Re-read the configuration and every server's schemas",
			Description: "Picks up configuration files edited since the daemon started " +
				"-- a server added by hand, or by `mcpx registry add` -- and then " +
				"starts every configured server to read its schemas. A server whose " +
				"definition did not change keeps its running process. Cheap to ask " +
				"for and expensive to serve, which is the reason it is marked " +
				"privileged rather than anything it destroys.",
			Admin: true, Mutating: true,
		},
		{
			Name: "restart", Method: "POST", Path: "/v1/restart",
			Command: "restart",
			Summary: "Restart a server's instances, or every server's",
			Description: "Stops every running instance and starts a replacement under the " +
				"same scope key, waiting for each to initialize; a replacement that " +
				"does not come up is reported with the server's own error. With " +
				"nothing running, a global-scope server starts one instance, so a " +
				"restart verifies the server comes up; a scoped server with nothing " +
				"running has no caller to start one for and starts nothing. In-flight " +
				"calls on the stopped instances fail. Omit the server to restart all " +
				"of them. lazy stops without starting anything: the next call starts " +
				"a fresh instance.",
			Admin: true, Mutating: true, Destructive: true,
			Params: []Param{
				{Name: "server", In: InBody, Type: "string", Desc: "empty means every server"},
				{Name: "lazy", In: InBody, Type: "boolean",
					Desc: "stop only; start on the next call instead of now"},
			},
		},
		{
			Name: "shutdown", Method: "POST", Path: "/v1/shutdown",
			Idempotent: true,
			Command:    "stop",
			Summary:    "Stop the daemon",
			Description: "Every pooled server process stops with it, and every other " +
				"client of this daemon loses its session.",
			Admin: true, Mutating: true, Destructive: true,
		},
		{
			Name: "daemon_restart", Method: "POST", Path: "/v1/daemon/restart",
			Command: "restart",
			Summary: "Restart the mcpx daemon",
			Description: "Gracefully reloads or restarts the mcpx daemon process itself.",
			Admin: true, Mutating: true, Destructive: true,
		},
		{
			Name: "dashboard", Method: "GET", Path: "/v1/dashboard",
			Command: "dashboard",
			Text:    true,
			Summary: "Live web dashboard HTML",
			Description: "Single-page dashboard application displaying active pools and token economics telemetry.",
		},
		{
			Name: "metrics_tokens", Method: "GET", Path: "/v1/metrics/tokens",
			Summary: "Token economics telemetry summary",
			Description: "Aggregated stats on prompt turns, baseline tool schema tokens, injected tokens, and tokens saved.",
		},
		{
			Name: "metrics_tools", Method: "GET", Path: "/v1/metrics/tools",
			Summary: "Tool call telemetry, error rates, correlations and timelines",
			Description: "Aggregated stats on tool call volume, error rates, error shapes, correlated tool pairs, and usage over time.",
		},
		{
			Name: "chat_completions", Method: "POST", Path: "/v1/chat/completions",
			Command: "proxy",
			Summary: "OpenAI-compatible LLM chat completions endpoint",
			Description: "Proxies chat completions requests while dynamically injecting semantically discovered tools.",
			Mutating: true,
			Params: []Param{
				{Name: "model", In: InBody, Type: "string", Desc: "the model ID"},
				{Name: "messages", In: InBody, Type: "array", Required: true, Desc: "the list of chat messages",
					Schema: `{"type":"array","items":{"type":"object"}}`},
			},
		},
		{
			Name: "embeddings", Method: "POST", Path: "/v1/embeddings",
			Command: "embeddings",
			Summary: "Compute vector embeddings for text",
			Description: "OpenAI-compatible vector embedding endpoint using pure-Go, WASM, or remote engines.",
			Mutating: true,
			Params: []Param{
				{Name: "input", In: InBody, Type: "string", Required: true, Desc: "input text string or array of strings"},
				{Name: "model", In: InBody, Type: "string", Desc: "the model ID"},
			},
		},
		{
			Name: "embeddings_export", Method: "GET", Path: "/v1/embeddings/export",
			Command: "embeddings export",
			Summary: "Export cached vector embeddings",
			Description: "Exports all computed and indexed tool vector embeddings as JSON.",
		},
		{
			Name: "embeddings_import", Method: "POST", Path: "/v1/embeddings/import",
			Command: "embeddings import",
			Summary: "Import pre-computed vector embeddings",
			Description: "Imports pre-computed tool vector embeddings from JSON payload.",
			Mutating: true,
			Params: []Param{
				{Name: "content", In: InBody, Type: "string", Required: true, Raw: true,
					Desc: "the JSON payload, sent as the whole request body"},
			},
		},
		{
			Name: "feedback", Method: "POST", Path: "/v1/feedback",
			Command: "feedback",
			Summary: "Submit quality feedback for a trace",
			Description: "Attaches a score and feedback notes to an interaction trace ID for adaptive routing.",
			Mutating: true,
			Params: []Param{
				{Name: "traceId", In: InBody, Type: "string", Required: true, Desc: "the interaction trace ID"},
				{Name: "score", In: InBody, Type: "number", Required: true, Desc: "primary score between 0.0 (bad) and 1.0 (good)"},
				{Name: "target", In: InBody, Type: "string", Desc: "feedback target: 'retrieval' (search relevance) or 'execution' (tool runtime success)"},
				{Name: "retrievalScore", In: InBody, Type: "number", Desc: "retrieval relevance score between 0.0 and 1.0"},
				{Name: "executionScore", In: InBody, Type: "number", Desc: "execution reliability score between 0.0 and 1.0"},
				{Name: "notes", In: InBody, Type: "string", Desc: "commentary or feedback explanation"},
			},
		},
		{
			Name: "interactions_list", Method: "GET", Path: "/v1/interactions",
			Command: "interactions",
			Summary: "List and filter past interaction traces",
			Description: "Returns history of inputs, outputs, tools used, and recorded feedback scores.",
			Params: []Param{
				{Name: "q", In: InQuery, Type: "string", Desc: "search text across inputs, outputs, and feedback notes"},
				{Name: "source", In: InQuery, Type: "string", Desc: "filter by source ('proxy', 'call', etc.)"},
				{Name: "target", In: InQuery, Type: "string", Desc: "filter by feedback target ('retrieval' or 'execution')"},
				{Name: "hasFeedback", In: InQuery, Type: "string", Desc: "filter by feedback status: '1' (rated), '0' (unrated)"},
			},
		},
		{
			Name: "interactions_get", Method: "GET", Path: "/v1/interactions/{id}",
			Command: "interactions show",
			Summary: "Inspect a specific interaction trace",
			Description: "Returns full details of an interaction by its trace ID.",
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the interaction trace ID"},
			},
		},
		{
			Name: "openapi", Method: "GET", Path: "/v1/openapi.json",
			Command:     "openapi",
			Summary:     "This API, as an OpenAPI 3.1 document",
			Description: "Generated from the same table the routes and the MCP tools are.",
		},
	}
	ops = append(ops, protoOps()...)
	ops = append(ops, consumerOps()...)
	ops = append(ops, execOps()...)
	ops = append(ops, settingsOps()...)
	ops = append(ops, resolveOps()...)
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path == ops[j].Path {
			return ops[i].Method < ops[j].Method
		}
		return ops[i].Path < ops[j].Path
	})
	return ops
}

// ByName finds one operation.
func ByName(name string) (Op, bool) {
	for _, o := range Ops() {
		if o.Name == name {
			return o, true
		}
	}
	return Op{}, false
}

// InputSchema is the JSON Schema for everything the operation takes, path,
// query and body together.
//
// One flat object rather than three nested ones. A caller filling in a tool
// call should not have to know which of its arguments the daemon reads from
// the URL, and the mapping is this package's business anyway.
func (o Op) InputSchema() json.RawMessage {
	props := make([]string, 0, len(o.Params))
	var required []string
	for _, p := range o.Params {
		props = append(props, fmt.Sprintf("%q:%s", p.Name, p.schema()))
		if p.Required {
			required = append(required, fmt.Sprintf("%q", p.Name))
		}
	}
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	b.WriteString(strings.Join(props, ","))
	b.WriteString(`}`)
	if len(required) > 0 {
		b.WriteString(`,"required":[` + strings.Join(required, ",") + `]`)
	}
	b.WriteString(`,"additionalProperties":false}`)
	return json.RawMessage(b.String())
}

func (p Param) schema() string {
	if p.Schema != "" {
		return p.Schema
	}
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":%q`, p.typeOr())
	if p.Desc != "" {
		d, _ := json.Marshal(p.Desc)
		fmt.Fprintf(&b, `,"description":%s`, d)
	}
	if len(p.Enum) > 0 {
		e, _ := json.Marshal(p.Enum)
		fmt.Fprintf(&b, `,"enum":%s`, e)
	}
	b.WriteString("}")
	return b.String()
}

func (p Param) typeOr() string {
	if p.Type == "" {
		return "string"
	}
	return p.Type
}

// Request turns tool arguments into an HTTP request for this operation.
//
// This is the whole of the proxy. Every generated MCP tool goes through it,
// so a tool cannot map an argument to the wrong place for one route and the
// right place for another.
func (o Op) Request(args map[string]any) (path string, body []byte, err error) {
	path = o.Path
	query := url.Values{}
	bodyObj := map[string]any{}

	known := map[string]Param{}
	for _, p := range o.Params {
		known[p.Name] = p
	}
	for name, v := range args {
		p, ok := known[name]
		if !ok {
			return "", nil, fmt.Errorf("%s takes no argument %q", o.Name, name)
		}
		switch p.In {
		case InPath:
			s := scalar(v)
			if s == "" {
				return "", nil, fmt.Errorf("%s: %s is part of the path and cannot be empty", o.Name, name)
			}
			path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(s))
		case InQuery:
			if s := scalar(v); s != "" {
				query.Set(name, s)
			}
		case InBody:
			bodyObj[name] = v
		}
	}
	for _, p := range o.Params {
		if !p.Required {
			continue
		}
		if _, given := args[p.Name]; !given {
			return "", nil, fmt.Errorf("%s: %s is required", o.Name, p.Name)
		}
	}
	if strings.Contains(path, "{") {
		return "", nil, fmt.Errorf("%s: unfilled path parameter in %q", o.Name, path)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if raw, ok := o.RawParam(); ok {
		if v, given := args[raw.Name]; given {
			body = []byte(scalar(v))
		}
		return path, body, nil
	}
	if o.Method != "GET" {
		// Some routes take their whole body as one named object -- a log
		// record, an elicitation answer -- because the body is the thing
		// rather than a field of it.
		if wrapper, ok := o.bodyIsOneObject(); ok {
			inner, present := bodyObj[wrapper]
			if !present {
				inner = map[string]any{}
			}
			body, err = json.Marshal(inner)
			return path, body, err
		}
		body, err = json.Marshal(bodyObj)
		if err != nil {
			return "", nil, err
		}
	}
	return path, body, nil
}

// bodyIsOneObject names the parameter that *is* the body, for the routes
// that take a bare document rather than a set of fields.
func (o Op) bodyIsOneObject() (string, bool) {
	switch o.Name {
	case "log_record":
		return "record", true
	case "elicit_answer":
		return "content", true
	case "tool_invoke", "call_path":
		return "arguments", true
	}
	return "", false
}

func scalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "1"
		}
		return "0"
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// Annotations are the MCP tool hints for this operation.
//
// readOnlyHint and destructiveHint are what the specification gives a client
// to decide whether a tool may be run without asking, so they have to follow
// the same facts a scope check would.
func (o Op) Annotations() map[string]any {
	return map[string]any{
		"title":           o.Summary,
		"readOnlyHint":    !o.Mutating,
		"destructiveHint": o.Destructive,
		"idempotentHint":  !o.Mutating || o.Idempotent,
		"openWorldHint":   true,
	}
}

// ToolDescription is what a model reads when deciding whether to call it.
func (o Op) ToolDescription() string {
	parts := []string{o.Summary + ". " + o.Method + " " + o.Path + " on the mcpx daemon."}
	if o.Description != "" {
		parts = append(parts, o.Description)
	}
	if o.Admin {
		// "Privileged" is mcpx's word, not the specification's, and on its own
		// it reads like a permission something checks. Nothing does, so the
		// label says it is advisory where a model reads it
		// (docs/decisions/0003).
		parts = append(parts, "Privileged: this changes the daemon rather than reading it. "+
			"Advisory: the daemon does not check who calls it.")
	}
	if o.Destructive {
		parts = append(parts, "Destructive: it cannot be undone, and other callers of this daemon are affected.")
	}
	return strings.Join(parts, " ")
}
