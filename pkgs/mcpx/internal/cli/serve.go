package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/adapter"
	"github.com/dezren39/mcpx/internal/artifacts"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/events"
	"github.com/dezren39/mcpx/internal/execsvc"
	"github.com/dezren39/mcpx/internal/logstore"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// mcpBackend answers the MCP server's requests using the daemon.
//
// Everything returns text, because that is what an MCP tool result is. The
// alternative -- structured content -- exists in the protocol but is unevenly
// supported, and the text these produce is already what a person would want
// to read.
type mcpBackend struct{ app *App }

func (b mcpBackend) Namespaces(ctx context.Context) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	if err := b.app.ensureAnySchemas(ctx, c); err != nil {
		return "", err
	}
	ns, err := c.Namespaces(ctx, b.app.Profile)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-20s %6s %-10s %s\n", "NAMESPACE", "TOOLS", "STATE", "DESCRIPTION")
	for _, n := range ns {
		state := "ready"
		if n.Error != "" {
			state = "error"
		}
		fmt.Fprintf(&sb, "%-20s %6d %-10s %s\n", n.Namespace, n.Tools, state, n.Description)
	}
	fmt.Fprintf(&sb, "\n%d servers. mcpx_types for signatures, mcpx_exec to run code against them.\n",
		len(ns))
	return sb.String(), nil
}

func (b mcpBackend) Catalog(ctx context.Context, budget int, bias string) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	if err := b.app.ensureAnySchemas(ctx, c); err != nil {
		return "", err
	}
	return c.Catalog(ctx, nil, budget, bias, b.app.Profile)
}

func (b mcpBackend) Types(ctx context.Context, namespaces []string) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	if err := b.app.ensureSchemas(ctx, c, namespaces); err != nil {
		return "", err
	}
	return c.Types(ctx, namespaces, true, b.app.Profile)
}

func (b mcpBackend) Search(ctx context.Context, query string, limit int) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	if err := b.app.ensureAnySchemas(ctx, c); err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = 20
	}
	hits, err := c.Search(ctx, query, limit)
	if err != nil {
		return "", err
	}
	visible, err := b.app.visibleNamespaces(ctx, c)
	if err != nil {
		return "", err
	}
	kept := hits[:0]
	for _, h := range hits {
		if visible(h.Namespace) {
			kept = append(kept, h)
		}
	}
	hits = kept
	if len(hits) == 0 {
		return "No matching tools. mcpx_namespaces lists what exists.", nil
	}
	var sb strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&sb, "%s.%s\n    %s\n", h.Namespace, h.Tool, firstLine(h.Description))
	}
	return sb.String(), nil
}

func (b mcpBackend) Call(ctx context.Context, ns, tool string, args json.RawMessage) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	visible, err := b.app.visibleNamespaces(ctx, c)
	if err != nil {
		return "", err
	}
	if !visible(ns) {
		return "", unknownNamespace(ns)
	}
	res, err := c.Call(ctx, ns, tool, b.app.mcpCaller(ctx), args)
	if err != nil {
		return "", err
	}
	text, failed := renderResult(res.Result)
	if failed {
		// mcpserver turns a backend error into a result with isError, and
		// /v1/tools into ok:false; ToolFailure lets the latter tell it from
		// a call mcpx could not make.
		return "", daemon.ToolFailure{Text: text}
	}
	return text, nil
}

// Exec runs a script through the daemon's /v1/exec.
//
// Through the daemon rather than in this process, so that the model's script
// and a person's script are the same execution with the same artifact
// behaviour. It also means `mcpx serve` can front a daemon on another machine
// and the script runs where the servers are, which is the case that made the
// service exist.
//
// The caller declares artifacts, because an MCP client can always be handed a
// resource_link and told to read it. That is what keeps a screenshot out of
// the model's context: the link costs a line, the megabyte costs a
// resources/read the model only makes if it needs to look.
func (b mcpBackend) Exec(ctx context.Context, source string, timeoutSec int) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	if err := b.app.ensureAnySchemas(ctx, c); err != nil {
		return "", err
	}
	opts := b.execOptions(ctx, timeoutSec)
	body, err := c.do(ctx, http.MethodPost, "/v1/exec",
		map[string]any{"source": source, "options": opts})
	if err != nil {
		return "", err
	}
	var res execsvc.Result
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}
	return renderExec(res)
}

// execOptions is what mcpx_exec asks /v1/exec for, whichever path runs it.
func (b mcpBackend) execOptions(ctx context.Context, timeoutSec int) execsvc.Options {
	opts := execsvc.Options{
		// The connection's own identity, not the process's: see mcpCaller.
		Session:      b.app.mcpCaller(ctx).SessionID,
		Output:       execsvc.OutputStructured,
		Capabilities: []string{execsvc.CapabilityArtifacts},
	}
	if timeoutSec > 0 {
		opts.Timeout = (time.Duration(timeoutSec) * time.Second).String()
	}
	return opts
}

// renderExec turns a script's result into mcpx_exec's tool result. Shared by
// the direct path and the interruptible one (serve_ask.go), so a script
// answered inline renders exactly like one that was never asked anything.
func renderExec(res execsvc.Result) (string, error) {

	var sb strings.Builder
	for _, e := range res.Emits {
		sb.Write(e)
		sb.WriteByte('\n')
	}
	sb.WriteString(res.Stdout)
	text := strings.TrimRight(sb.String(), "\n")
	if res.Error != "" {
		if text != "" {
			return "", fmt.Errorf("%s\n\n%s", res.Error, text)
		}
		return "", errors.New(res.Error)
	}
	if text == "" && len(res.Artifacts) == 0 {
		return "(the script printed nothing; use console.log or emit to return a value)", nil
	}
	// One resource_link per artifact. The specification's own answer to "a
	// tool produced a file": a URI, a name and a type, which the client reads
	// with resources/read if and when it wants the bytes.
	var blocks []map[string]any
	for _, art := range res.Artifacts {
		blocks = append(blocks, map[string]any{
			"type":        "resource_link",
			"uri":         art.URI,
			"name":        art.Name,
			"mimeType":    art.Mime,
			"description": fmt.Sprintf("%d bytes, produced by this script", art.Size),
		})
	}
	return mcpserver.EncodeResult(text, blocks), nil
}

func (b mcpBackend) Log(ctx context.Context, since, level, event string, limit int) (string, error) {
	st, err := b.app.openStore("")
	if err != nil {
		return "", err
	}
	defer st.Close()
	if limit <= 0 {
		limit = 50
	}
	q := logstore.Query{Level: level, Event: event, Limit: limit}
	if q.Since, err = logstore.ParseWhen(since, time.Now()); err != nil {
		return "", err
	}
	recs, err := st.Records(q)
	if err != nil {
		return "", err
	}
	if len(recs) == 0 {
		return "No records match.", nil
	}
	var sb strings.Builder
	for _, r := range recs {
		fmt.Fprintf(&sb, "%s %-5s %s %s\n", r.Time.Format("15:04:05.000"),
			strings.ToUpper(r.Level.String()), r.Msg, flatten(r.Attrs))
	}
	return sb.String(), nil
}

func (b mcpBackend) Stats(ctx context.Context, dimension string) (string, error) {
	if dimension == "" {
		dimension = "calls"
	}
	t, err := tuiSource{app: b.app}.Stats(ctx, dimension)
	if err != nil {
		if dimension == "instances" {
			t, err = tuiSource{app: b.app}.Instances(ctx)
		}
		if err != nil {
			return "", err
		}
	}
	plain := func(s string) string { return s }
	return t.Render(100, -1, plain, plain), nil
}

func (b mcpBackend) Status(ctx context.Context) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	st, err := c.Status(ctx)
	if err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (b mcpBackend) RegistrySearch(ctx context.Context, query string, limit int) (string, error) {
	if limit <= 0 {
		// The setting, not a literal: the CLI and /v1 both read registry.limit,
		// and a number here meant the MCP tool was the one surface where
		// config = env = cli = /v1 did not hold.
		limit = b.app.Settings().Int("registry.limit")
	}
	res, err := b.app.registrySearch(ctx, query, limit)
	if err != nil {
		return "", err
	}
	servers := res.Servers
	if len(servers) == 0 {
		return "Nothing matched. The registry matches server names as a substring, " +
			"so try one word rather than a phrase.", nil
	}
	var sb strings.Builder
	for _, s := range servers {
		in, ierr := s.ToInstall(false)
		how := "not installable by mcpx"
		if ierr == nil {
			how = in.How
		}
		fmt.Fprintf(&sb, "%s\n    %s\n    %s\n    add with: mcpx registry add %s --write\n",
			s.Name, firstLine(s.Description), how, s.Name)
	}
	if res.Truncated {
		// Every other surface says this. Without it a model reads a cut list
		// as the whole answer and stops looking.
		fmt.Fprintf(&sb, "\n%d shown; more matched. Ask again with a higher limit.\n", len(servers))
	}
	return sb.String(), nil
}

func flatten(attrs map[string]any) string {
	if len(attrs) == 0 {
		return ""
	}
	var parts []string
	for k, v := range attrs {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}

// mcpCaller is who a request made through the MCP server is from, as the
// daemon's call context.
//
// Built per request from the identity mcpserver resolved for it, not per
// App. Inside the daemon the App is the daemon's own, so asking the process
// who it is answered with the daemon's pid and cwd for every HTTP client:
// they all shared one session, and every session-scoped upstream instance
// with it (conflict #2). Only stdio -- one process, one client -- may still
// answer from the process. See docs/spec/identity.md.
func (a *App) mcpCaller(ctx context.Context) config.CallContext {
	id := mcpserver.IdentityFrom(ctx)
	switch id.Source {
	case mcpserver.IdentityProcess:
		s := a.mcpSession()
		return a.callContext(s, s)
	case mcpserver.IdentitySession:
		// Prefixed so a session id can never collide with a name a CLI
		// user or a client chose. No cwd and no pid: the daemon's are not
		// the client's, and a scope that needs one degrades to per-call,
		// which is the direction that cannot leak.
		s := "mcp-" + id.Key
		return config.CallContext{SessionID: s, CallID: s}
	case mcpserver.IdentityClient:
		return config.CallContext{SessionID: id.Key, CallID: id.Key}
	}
	// Nobody in particular: the request is its own scope.
	return config.CallContext{CallID: newSessionKey()}
}

// mcpSession is the session key of a process that is itself one MCP
// client's server -- `mcpx serve` over stdio.
//
// A host that speaks MCP has its own notion of a session and no way to tell
// us, so one is derived per process. That is the honest answer there: every
// call from one connection shares a lease, and two connections do not.
func (a *App) mcpSession() string {
	if s := os.Getenv("MCPX_SESSION_ID"); s != "" {
		return s
	}
	return fmt.Sprintf("mcp-%d", os.Getpid())
}

// MCPServer builds mcpx's own MCP server.
//
// Built here and mounted by whoever is listening, rather than owning a
// listener of its own. There was a second HTTP server for exactly as long as
// `mcpx serve --transport http` existed, with its own /v1 prefix over the
// daemon's, and which of the two you reached decided which half of the API
// existed.
func (a *App) MCPServer(ctx context.Context) (*mcpserver.Server, error) {
	srv := mcpserver.New(mcpBackend{app: a}, "mcpx", a.Version)
	srv.Notify = daemonNotifier{app: a}
	srv.PageSize = a.Settings().Int("mcp.pageSize")
	// A function, because completion.maxValues is hot: the daemon's /mcp
	// is built once and a value copied here never followed a change.
	srv.MaxCompletions = func() int { return a.Settings().Int("completion.maxValues") }
	// The ask loop's five bounds. They were read from internal/defaults at
	// the point of use, so the settings that name them did nothing.
	srv.Timing = mcpserver.Timing{
		AskTimeout:  a.Settings().Duration("proto.askTimeout"),
		AskPoll:     a.Settings().Duration("proto.askPoll"),
		AskRounds:   a.Settings().Int("proto.askRounds"),
		StateTTL:    a.Settings().Duration("proto.stateTTL"),
		SessionIdle: a.Settings().Duration("proto.sessionIdle"),
		TaskAfter:   a.Settings().Duration("protoMessages.taskAfter"),
		// One keep-alive for every event stream mcpx serves: a legacy GET
		// stream and a 2026-07-28 listen stream are the same thing to a proxy.
		SSEKeepAlive: a.Settings().Duration("transport.sseKeepAlive"),
		StdioDrain:   a.Settings().Duration("transport.stdioDrain"),
		TaskPoll:     a.Settings().Duration("protoTasks.pollInterval"),
	}
	srv.Cache = mcpserver.Cache{
		List: a.Settings().Duration("protoMessages.listMaxAge"),
		Read: a.Settings().Duration("protoMessages.readMaxAge"),
	}
	// Browser origins the HTTP transport serves: loopback at any port, the
	// daemon's own address when it listens somewhere else, and whatever
	// was configured. The daemon's address is named explicitly rather than
	// taken from the request's Host, which DNS rebinding controls.
	hosts := append([]string(nil), defaults.TransportLoopbackHosts...)
	if addr := a.Settings().String("daemon.address"); addr != "" && !unspecifiedHost(addr) {
		hosts = append(hosts, addr)
	}
	srv.Origins = mcpserver.OriginPolicy{Hosts: hosts,
		Origins: a.Settings().List("transport.allowedOrigins")}
	if a.Settings().Bool("proto.native") {
		// Native elicitation and sampling: a question an upstream server
		// asks is put to mcpx's own client, if that client said it could
		// answer one. Off, every question goes to the broker's default
		// audience, which is what happened before any client could.
		srv.Ask = daemonAsker{app: a}
	}

	// Adapted programs are offered as tools in their own right, not only
	// through mcpx_exec. A host that wants git as a tool should get git as a
	// tool; routing it through a script would be a worse answer for a
	// question nobody asked.
	specs, err := a.loadAdapters()
	if err != nil {
		return nil, err
	}
	extras := adapterTools(specs)
	// Operations from declared OpenAPI documents are tools in their own
	// right too, for the same reason adapted programs are: a host that wants
	// an endpoint should get the endpoint.
	extras = append(extras, a.apiTools(ctx)...)
	// And every /v1 operation the daemon serves, as a tool apiece. An
	// endpoint reachable from curl and from the plugin but not from MCP is
	// reachable from fewer places than it needs to be.
	extras = append(extras, a.v1OpTools()...)
	if len(extras) > 0 {
		srv = srv.WithExtras(extras)
	}
	return srv, nil
}

// CmdServe runs mcpx as an MCP server over stdio.
//
// stdio is inherent: an MCP host starts its servers by spawning a process
// and talking down the pipe, so something has to be spawnable. It stays a
// thin shim over the daemon for that reason and no other. Every other
// transport is the daemon's, which already has listeners, a socket and a
// lifetime -- see docs/protocol.md.
func (a *App) CmdServe(ctx context.Context, args []string) error {
	fs := newFlagSet("serve")
	transport := fs.String("transport", "stdio", "stdio; HTTP is served by the daemon at "+defaults.ProtoMCPPath)
	printTools := fs.Bool("tools", false, "print the exposed tool list and exit")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	srv, err := a.MCPServer(ctx)
	if err != nil {
		return err
	}
	if *printTools {
		return a.out(srv.Tools())
	}
	if *transport != "stdio" {
		return fmt.Errorf("no transport %q; mcpx serves MCP over stdio here, and over "+
			"HTTP from the daemon at %s -- run `mcpx daemon --port N` and point the "+
			"host at http://127.0.0.1:N%s", *transport, defaults.ProtoMCPPath, defaults.ProtoMCPPath)
	}
	// Nothing may write to stdout except protocol frames, or the host sees a
	// parse error and disconnects. This is the single most common way an MCP
	// server over stdio fails.
	a.machineOutput = true
	return srv.ServeStdio(ctx, os.Stdin, os.Stdout)
}

// unspecifiedHost reports whether an address means every interface, which
// names no origin a browser could present.
func unspecifiedHost(h string) bool {
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsUnspecified()
}

func writeJSONResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// adapterTools turns declarations into MCP tools.
func adapterTools(specs []adapter.Spec) []mcpserver.Extra {
	var out []mcpserver.Extra
	for _, spec := range specs {
		spec := spec
		for _, t := range spec.Tools {
			t := t
			desc := t.Description
			if spec.Instructions != "" {
				desc = strings.TrimSpace(desc + "\n\n" + spec.Instructions)
			}
			out = append(out, mcpserver.Extra{
				Tool: mcpserver.Tool{
					Name:        spec.Name + "_" + t.Name,
					Description: desc,
					InputSchema: t.Schema(),
				},
				Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
					var args map[string]any
					if len(raw) > 0 {
						if err := json.Unmarshal(raw, &args); err != nil {
							return "", err
						}
					}
					res, err := spec.Call(ctx, t.Name, args)
					if err != nil {
						return "", err
					}
					return renderAdapterResult(res), nil
				},
			})
		}
	}
	return out
}

// renderAdapterResult favours stdout, because that is what a program has to
// say. Stderr and a non-zero exit are appended rather than substituted: a
// program that warns and succeeds should not look like a failure.
func renderAdapterResult(r *adapter.Result) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(r.Stdout, "\n"))
	if s := strings.TrimSpace(r.Stderr); s != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("stderr:\n" + s)
	}
	if r.ExitCode != 0 {
		fmt.Fprintf(&b, "\n\n(exit %d)", r.ExitCode)
	}
	if b.Len() == 0 {
		return "(no output)"
	}
	return b.String()
}

// runInline runs a snippet and collects what it printed.
//
// It goes through the same runScript the exec command uses rather than a
// parallel path, because two ways to run a script is two sets of bugs, and
// the one nobody runs by hand is the one that rots.
func (a *App) runInline(ctx context.Context, source string, out interface {
	Write([]byte) (int, error)
}) error {
	// The redirection is set and restored rather than done on a copy. An App
	// holds a sync.Once, and copying one copies the Once -- which vet catches
	// and which would otherwise mean the clone silently re-resolved every
	// setting from scratch.
	prevOut, prevMachine := a.stdoutOverride, a.machineOutput
	a.stdoutOverride, a.machineOutput = out, true
	defer func() { a.stdoutOverride, a.machineOutput = prevOut, prevMachine }()
	return a.runScript(ctx, []string{source}, true)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Resources passes through what the upstream servers publish.
func (b mcpBackend) Resources(ctx context.Context) ([]mcpserver.ResourceRef, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return nil, err
	}
	if err := b.app.ensureAnySchemas(ctx, c); err != nil {
		return nil, err
	}
	list, err := c.Resources(ctx, nil)
	if err != nil {
		return nil, err
	}
	visible, err := b.app.visibleNamespaces(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserver.ResourceRef, 0, len(list))
	for _, r := range list {
		if !visible(r.Namespace) {
			continue
		}
		// Namespaced, because two servers may publish the same URI and a
		// caller has no way to say which one it meant otherwise.
		out = append(out, mcpserver.ResourceRef{
			URI:         "mcpx://" + r.Namespace + "/" + strings.TrimPrefix(r.URI, "/"),
			Name:        r.Name,
			Description: strings.TrimSpace(r.Description + " (" + r.Namespace + ")"),
			MimeType:    r.MimeType,
		})
	}
	return append(out, b.artifactResources(ctx)...), nil
}

// artifactResources lists what scripts produced, as resources.
//
// Artifacts are resources in the protocol's own sense -- a URI, a name, a
// type, bytes behind them -- so they belong in resources/list rather than
// behind a tool of their own. A client that already knows how to read a
// resource needs nothing new to collect a screenshot.
//
// Listed rather than only linked because a link in a result scrolls out of a
// conversation and the file does not.
func (b mcpBackend) artifactResources(ctx context.Context) []mcpserver.ResourceRef {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return nil
	}
	session := b.app.mcpCaller(ctx).SessionID
	if session == "" {
		// A caller with no identity has no artifacts of its own, and the
		// daemon reads an empty session as "every session's".
		return nil
	}
	list, err := c.Artifacts(ctx, "", session)
	if err != nil || len(list) == 0 {
		return nil
	}
	out := make([]mcpserver.ResourceRef, 0, len(list))
	for _, a := range list {
		out = append(out, mcpserver.ResourceRef{
			URI:         a.URI,
			Name:        a.Name,
			Description: fmt.Sprintf("%d bytes, produced by run %s", a.Size, a.Run),
			MimeType:    a.Mime,
		})
	}
	return out
}

// Prompts passes through what the upstream servers publish.
func (b mcpBackend) Prompts(ctx context.Context) ([]mcpserver.PromptRef, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return nil, err
	}
	if err := b.app.ensureAnySchemas(ctx, c); err != nil {
		return nil, err
	}
	list, err := c.Prompts(ctx, nil)
	if err != nil {
		return nil, err
	}
	visible, err := b.app.visibleNamespaces(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserver.PromptRef, 0, len(list))
	for _, p := range list {
		if !visible(p.Namespace) {
			continue
		}
		args := make([]mcpserver.PromptArg, 0, len(p.Arguments))
		for _, a := range p.Arguments {
			args = append(args, mcpserver.PromptArg{
				Name: a.Name, Description: a.Description, Required: a.Required,
			})
		}
		out = append(out, mcpserver.PromptRef{
			Name:        p.Namespace + "_" + p.Name,
			Title:       p.Title,
			Description: p.Description,
			Arguments:   args,
		})
	}
	return out, nil
}

// ReadResource resolves a namespaced URI back to its server.
//
// A resource that does not exist is reported as mcpserver.ErrResourceNotFound
// so the protocol layer can answer with the code the client's revision
// defines for it; everything else is a failure to read, which is a
// different code. The four ways a URI names nothing: it is not an mcpx URI
// at all, it names no namespace, the namespace is not configured, or the
// upstream server itself said not-found.
func (b mcpBackend) ReadResource(ctx context.Context, uri string) ([]mcpserver.ResourceContents, error) {
	// Artifacts are answered before the namespace split, because "artifacts"
	// is not a server and would otherwise be looked up as one.
	if id, ok := artifacts.IDFromURI(uri); ok {
		body, mime, err := b.readArtifact(ctx, id)
		// The daemon's 404 arrives as its message; artifacts.ErrNotFound
		// is the only thing that says "no artifact <id>".
		if err != nil && strings.Contains(err.Error(), "no artifact "+id) {
			return nil, fmt.Errorf("%w: %v", mcpserver.ErrResourceNotFound, err)
		}
		if err != nil {
			return nil, err
		}
		entry := mcpserver.ResourceContents{MimeType: mime, Text: body}
		if !textMime(mime) {
			// ArtifactBody already base64-encoded it; it is a blob, and
			// was being sent as text a client could not tell from prose.
			entry = mcpserver.ResourceContents{MimeType: mime, Blob: body}
		}
		return []mcpserver.ResourceContents{entry}, nil
	}
	rest, isOurs := strings.CutPrefix(uri, "mcpx://")
	ns, inner, ok := strings.Cut(rest, "/")
	if !isOurs || !ok || ns == "" {
		return nil, fmt.Errorf("%w: a resource URI looks like mcpx://<namespace>/<uri>, got %q",
			mcpserver.ErrResourceNotFound, uri)
	}
	c, err := b.app.ensure(ctx)
	if err != nil {
		return nil, err
	}
	visible, err := b.app.visibleNamespaces(ctx, c)
	if err != nil {
		return nil, err
	}
	if !visible(ns) {
		return nil, fmt.Errorf("%w: %v", mcpserver.ErrResourceNotFound, unknownNamespace(ns))
	}
	raw, err := c.ReadResource(ctx, ns, inner, b.app.mcpCaller(ctx))
	if err != nil {
		if upstreamNotFound(err) {
			return nil, fmt.Errorf("%w: %v", mcpserver.ErrResourceNotFound, err)
		}
		return nil, err
	}
	return resourceContents(raw, "mcpx://"+ns+"/"), nil
}

// resourceContents reads a resources/read reply into entries, keeping each
// blob a blob. Entry URIs are namespaced the way the listing namespaces
// them, so a client can read any of them back.
func resourceContents(raw json.RawMessage, prefix string) []mcpserver.ResourceContents {
	var doc struct {
		Contents []struct {
			URI      string `json:"uri"`
			Text     string `json:"text"`
			Blob     string `json:"blob"`
			MimeType string `json:"mimeType"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []mcpserver.ResourceContents{{MimeType: "application/json", Text: string(raw)}}
	}
	out := make([]mcpserver.ResourceContents, 0, len(doc.Contents))
	for _, c := range doc.Contents {
		uri := ""
		if c.URI != "" {
			uri = prefix + strings.TrimPrefix(c.URI, "/")
		}
		out = append(out, mcpserver.ResourceContents{URI: uri, MimeType: c.MimeType,
			Text: c.Text, Blob: c.Blob})
	}
	return out
}

// textMime is what the daemon serves an artifact as text; everything else
// arrives base64.
func textMime(mime string) bool {
	return strings.HasPrefix(mime, "text/") || strings.HasPrefix(mime, "application/json")
}

// upstreamNotFound reads a not-found out of the daemon's answer.
//
// The daemon returns an upstream failure as text -- "unknown server or
// namespace", or the upstream JSON-RPC error rendered as "mcp error <code>:"
// -- so this is string matching across a process boundary. The codes it
// looks for are the only two any revision uses for a missing resource:
// -32002 up to 2025-11-25, -32602 since.
func upstreamNotFound(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unknown server or namespace") ||
		strings.Contains(msg, "mcp error -32002:") ||
		strings.Contains(msg, "mcp error -32602:")
}

// GetPrompt resolves a namespaced prompt back to its server.
func (b mcpBackend) GetPrompt(ctx context.Context, name string, args map[string]string) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	ns, inner, err := b.app.resolvePrompt(ctx, c, name)
	if err != nil {
		return "", err
	}
	raw, err := c.GetPrompt(ctx, ns, inner, args, b.app.mcpCaller(ctx))
	if err != nil {
		if upstreamInvalid(err) {
			// The upstream server said the request was wrong -- a missing
			// required argument, most often -- which is the client's to fix.
			return "", fmt.Errorf("%w: %v", mcpserver.ErrInvalidParams, err)
		}
		return "", err
	}
	return renderPrompt(raw), nil
}

// resolvePrompt maps a namespaced prompt name back to its server, within the
// profile. A name that resolves to nothing is the client's mistake, so it
// wraps ErrInvalidParams: -32602, not the -32603 of a server that failed.
func (a *App) resolvePrompt(ctx context.Context, c *Client, name string) (string, string, error) {
	list, err := c.Prompts(ctx, nil)
	if err != nil {
		return "", "", err
	}
	visible, err := a.visibleNamespaces(ctx, c)
	if err != nil {
		return "", "", err
	}
	for _, p := range list {
		if !visible(p.Namespace) {
			continue
		}
		if p.Namespace+"_"+p.Name == name || p.Name == name {
			return p.Namespace, p.Name, nil
		}
	}
	return "", "", fmt.Errorf("%w: no prompt named %q", mcpserver.ErrInvalidParams, name)
}

// upstreamInvalid reads an upstream -32602 out of the daemon's answer, the
// same string match upstreamNotFound makes and for the same reason.
func upstreamInvalid(err error) bool {
	return strings.Contains(err.Error(), "mcp error -32602:")
}

// Complete forwards completion/complete to the server that owns the ref,
// through /v1/complete, so the two surfaces give the same answer to the same
// question (conflict #9).
func (b mcpBackend) Complete(ctx context.Context, params json.RawMessage) ([]string, error) {
	var p struct {
		Ref struct {
			Type string `json:"type"`
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"ref"`
		Argument json.RawMessage `json:"argument"`
		Context  json.RawMessage `json:"context"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", mcpserver.ErrInvalidParams, err)
	}
	c, err := b.app.ensure(ctx)
	if err != nil {
		return nil, err
	}
	ref := map[string]any{"type": p.Ref.Type}
	var server string
	switch p.Ref.Type {
	case "ref/prompt":
		ns, inner, err := b.app.resolvePrompt(ctx, c, p.Ref.Name)
		if err != nil {
			return nil, err
		}
		server, ref["name"] = ns, inner
	case "ref/resource":
		if _, ok := artifacts.IDFromURI(p.Ref.URI); ok || p.Ref.URI == artifactTemplate {
			// mcpx's own template. Its ids are unguessable by design, so
			// there is nothing to offer.
			return nil, nil
		}
		ns, inner, ok := strings.Cut(strings.TrimPrefix(p.Ref.URI, "mcpx://"), "/")
		visible, err := b.app.visibleNamespaces(ctx, c)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(p.Ref.URI, "mcpx://") || !ok || !visible(ns) {
			return nil, fmt.Errorf("%w: no resource template %q", mcpserver.ErrInvalidParams, p.Ref.URI)
		}
		server, ref["uri"] = ns, inner
	}
	body := map[string]any{"server": server, "ref": ref, "argument": p.Argument,
		"context": b.app.mcpCaller(ctx)}
	raw, err := c.do(ctx, http.MethodPost, "/v1/complete", body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Completion struct {
			Values []string `json:"values"`
		} `json:"completion"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Completion.Values, nil
}

// visibleNamespaces is the profile applied as a boundary, not only a view.
//
// Namespaces, catalog and types were filtered by the profile and search,
// resources, prompts and calls were not, so a server hidden from
// mcpx_namespaces was still listed, searchable and callable over MCP
// (conflict #14). The daemon's namespace listing is the one place the
// profile is resolved, so it is asked here rather than re-deriving the rule.
func (a *App) visibleNamespaces(ctx context.Context, c *Client) (func(string) bool, error) {
	nss, err := c.Namespaces(ctx, a.Profile)
	if err != nil {
		return nil, err
	}
	in := map[string]bool{}
	for _, n := range nss {
		in[n.Namespace], in[n.Server] = true, true
	}
	return func(ns string) bool { return in[ns] }, nil
}

func unknownNamespace(ns string) error {
	return fmt.Errorf("unknown server or namespace %q", ns)
}

// renderResource pulls the text out of a resources/read reply.
func renderResource(raw json.RawMessage) (string, string, error) {
	var doc struct {
		Contents []struct {
			Text     string `json:"text"`
			Blob     string `json:"blob"`
			MimeType string `json:"mimeType"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return string(raw), "application/json", nil
	}
	var parts []string
	mime := ""
	for _, c := range doc.Contents {
		if mime == "" {
			mime = c.MimeType
		}
		if c.Text != "" {
			parts = append(parts, c.Text)
			continue
		}
		if c.Blob != "" {
			// Binary is described rather than inlined. A megabyte of base64
			// in a model's context is the failure this whole tool exists to
			// prevent.
			parts = append(parts, fmt.Sprintf("(%d bytes of %s, base64)", len(c.Blob), c.MimeType))
		}
	}
	return strings.Join(parts, "\n"), mime, nil
}

// renderPrompt flattens a prompts/get reply into text.
func renderPrompt(raw json.RawMessage) string {
	var doc struct {
		Description string `json:"description"`
		Messages    []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return string(raw)
	}
	var b strings.Builder
	if doc.Description != "" {
		b.WriteString(doc.Description + "\n\n")
	}
	for _, m := range doc.Messages {
		if m.Content.Text == "" {
			continue
		}
		if m.Role != "" && m.Role != "user" {
			fmt.Fprintf(&b, "[%s] ", m.Role)
		}
		b.WriteString(m.Content.Text)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ResourceTemplates passes through what upstream servers publish.
func (b mcpBackend) ResourceTemplates(ctx context.Context) ([]mcpserver.ResourceRef, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return nil, err
	}
	list, err := c.ResourceTemplates(ctx)
	if err != nil {
		return nil, err
	}
	visible, err := b.app.visibleNamespaces(ctx, c)
	if err != nil {
		return nil, err
	}
	// The template comes first because it is always true, where the list
	// below depends on what happens to be configured. A client reading this
	// learns it can address any artifact by id without having listed it.
	out := []mcpserver.ResourceRef{{
		URI:  artifactTemplate,
		Name: "mcpx artifact",
		Description: "A file a script produced. The id comes from a resource_link in " +
			"an mcpx_exec result, or from GET /v1/artifacts. Binary bodies arrive " +
			"base64 encoded with their type stated.",
	}}
	for _, r := range list {
		if !visible(r.Namespace) {
			continue
		}
		out = append(out, mcpserver.ResourceRef{
			URI:         "mcpx://" + r.Namespace + "/" + strings.TrimPrefix(r.URI, "/"),
			Name:        r.Name,
			Description: strings.TrimSpace(r.Description + " (" + r.Namespace + ")"),
			MimeType:    r.MimeType,
		})
	}
	return out, nil
}

// artifactTemplate is the one resource template mcpx itself publishes.
const artifactTemplate = "mcpx://artifacts/{id}"

// readArtifact fetches one artifact's body from the daemon.
//
// A binary body comes back base64, which ReadResource sends as a blob with
// its real type beside it.
func (b mcpBackend) readArtifact(ctx context.Context, id string) (string, string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", "", err
	}
	return c.ArtifactBody(ctx, id)
}

// daemonNotifier reads the daemon's event stream and forwards the parts MCP
// has a notification for.
//
// `mcpx serve` is a separate process from the daemon, so it cannot touch the
// daemon's bus directly. It subscribes over the same socket everything else
// uses, and translates. The translation is deliberately narrow: only the four
// notification kinds the specification defines leave this function, because
// a server MUST NOT send what the client did not ask for.
type daemonNotifier struct{ app *App }

func (n daemonNotifier) Listen(ctx context.Context, f mcpserver.ListenFilter, send func(string, any)) {
	var kinds []string
	if f.ToolsListChanged {
		kinds = append(kinds, string(events.ToolsChanged))
	}
	if f.PromptsListChanged {
		kinds = append(kinds, string(events.PromptsChanged))
	}
	if f.ResourcesListChanged {
		kinds = append(kinds, string(events.ResourcesChanged))
	}
	if len(f.ResourceSubscriptions) > 0 {
		kinds = append(kinds, string(events.ResourceUpdated))
	}
	if len(kinds) == 0 {
		return
	}
	c, err := n.app.ensure(ctx)
	if err != nil {
		return
	}
	// Resource URIs arrive namespaced from mcpx's own listings; the daemon
	// knows them by their upstream form. The update goes back out under the
	// URI the client subscribed with: a client matches notifications to its
	// subscriptions by URI, and one naming the upstream form matches nothing
	// it asked for.
	uris := make([]string, 0, len(f.ResourceSubscriptions))
	asked := map[string][]string{}
	for _, u := range f.ResourceSubscriptions {
		if rest, ok := strings.CutPrefix(u, "mcpx://"); ok {
			if _, uri, ok := strings.Cut(rest, "/"); ok {
				uris = append(uris, uri)
				asked[uri] = append(asked[uri], u)
				continue
			}
		}
		uris = append(uris, u)
	}
	_ = c.Stream(ctx, events.Filter{Kinds: kinds, URIs: uris}, func(e events.Event) {
		method, params, ok := events.MCPNotification(e)
		if !ok {
			return
		}
		for _, p := range subscribedURIs(e, params, asked) {
			send(method, p)
		}
	})
}

// subscribedURIs rewrites a resource update to the mcpx:// URI the client
// subscribed with. The event names its server but the listing names a
// namespace, which a config may override and this process cannot see; so
// the match is on the upstream URI, and every subscription with that
// upstream form hears it. An update is a hint to read again, so hearing one
// meant for a same-named resource elsewhere costs a read, where hearing none
// costs the update.
func subscribedURIs(e events.Event, params any, asked map[string][]string) []any {
	if e.Kind != events.ResourceUpdated {
		return []any{params}
	}
	origs := asked[strings.TrimPrefix(e.URI, "/")]
	if len(origs) == 0 {
		return []any{params}
	}
	out := make([]any, 0, len(origs))
	for _, o := range origs {
		out = append(out, map[string]any{"uri": o})
	}
	return out
}
