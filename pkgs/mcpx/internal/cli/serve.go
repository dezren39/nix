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
	session := b.app.mcpSession()
	res, err := c.Call(ctx, ns, tool, b.app.callContext(session, session), args)
	if err != nil {
		return "", err
	}
	return renderResult(res.Result), nil
}

func (b mcpBackend) Exec(ctx context.Context, source string, timeoutSec int) (string, error) {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	rctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Routed through the same path `mcpx exec` uses rather than a parallel
	// one, so a script behaves identically whether a person or a model wrote
	// it. Two ways to run a script is two sets of bugs.
	var out strings.Builder
	err := b.app.runInline(rctx, source, &out)
	text := strings.TrimRight(out.String(), "\n")
	if err != nil {
		if text != "" {
			return "", fmt.Errorf("%w\n\n%s", err, text)
		}
		return "", err
	}
	if text == "" {
		return "(the script printed nothing; use console.log or emit to return a value)", nil
	}
	return text, nil
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
		limit = 20
	}
	servers, err := b.app.registrySearch(ctx, query, limit)
	if err != nil {
		return "", err
	}
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

// mcpSession is the session key calls made through the MCP server belong to.
//
// A host that speaks MCP has its own notion of a session and no way to tell
// us, so one is derived per process. That is the honest answer: every call
// from one connection shares a lease, and two connections do not.
func (a *App) mcpSession() string {
	if s := os.Getenv("MCPX_SESSION_ID"); s != "" {
		return s
	}
	return fmt.Sprintf("mcp-%d", os.Getpid())
}

// CmdServe runs mcpx as an MCP server.
func (a *App) CmdServe(ctx context.Context, args []string) error {
	fs := newFlagSet("serve")
	transport := fs.String("transport", "stdio", "stdio or http")
	addr := fs.String("addr", "127.0.0.1:0", "address to listen on with --transport http")
	printTools := fs.Bool("tools", false, "print the exposed tool list and exit")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	srv := mcpserver.New(mcpBackend{app: a}, "mcpx", a.Version)

	// Adapted programs are offered as tools in their own right, not only
	// through mcpx_exec. A host that wants git as a tool should get git as a
	// tool; routing it through a script would be a worse answer for a
	// question nobody asked.
	specs, err := a.loadAdapters()
	if err != nil {
		return err
	}
	extras := adapterTools(specs)
	// Operations from declared OpenAPI documents are tools in their own
	// right too, for the same reason adapted programs are: a host that wants
	// an endpoint should get the endpoint.
	extras = append(extras, a.apiTools(ctx)...)
	if len(extras) > 0 {
		srv = srv.WithExtras(extras)
	}

	if *printTools {
		return a.out(srv.Tools())
	}

	switch *transport {
	case "stdio":
		// Nothing may write to stdout except protocol frames, or the host
		// sees a parse error and disconnects. This is the single most common
		// way an MCP server over stdio fails.
		a.machineOutput = true
		return srv.ServeStdio(ctx, os.Stdin, os.Stdout)

	case "http":
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.Handle("/mcp", srv)
		// The same tools as plain POSTs, so curl can ask what an agent asks.
		for _, tool := range srv.Tools() {
			mux.HandleFunc("/v1/tools/"+tool.Name, srv.RESTHandler(tool.Name))
		}
		// And every upstream tool, individually. A tool reachable from MCP
		// and from a script but not from curl is reachable from fewer places
		// than it needs to be.
		upstream := a.restToolRoutes(ctx, mux)
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSONResponse(w, map[string]any{"ok": true, "version": a.Version})
		})
		mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
			doc := OpenAPI(a.Version)
			// Generated per request rather than once, so a server that
			// appears after startup is described without a restart.
			if c, err := a.ensure(r.Context()); err == nil {
				if tools, terr := c.Tools(r.Context(), nil); terr == nil {
					paths, _ := doc["paths"].(map[string]any)
					for k, v := range toolPaths(tools) {
						paths[k] = v
					}
				}
			}
			writeJSONResponse(w, doc)
		})
		fmt.Fprintf(os.Stderr, "mcpx: MCP on http://%s/mcp (%d upstream tools as REST)\n",
			ln.Addr(), upstream)

		server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		}()
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	return fmt.Errorf("no transport %q; stdio or http", *transport)
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
	out := make([]mcpserver.ResourceRef, 0, len(list))
	for _, r := range list {
		// Namespaced, because two servers may publish the same URI and a
		// caller has no way to say which one it meant otherwise.
		out = append(out, mcpserver.ResourceRef{
			URI:         "mcpx://" + r.Namespace + "/" + strings.TrimPrefix(r.URI, "/"),
			Name:        r.Name,
			Description: strings.TrimSpace(r.Description + " (" + r.Namespace + ")"),
			MimeType:    r.MimeType,
		})
	}
	return out, nil
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
	out := make([]mcpserver.PromptRef, 0, len(list))
	for _, p := range list {
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
func (b mcpBackend) ReadResource(ctx context.Context, uri string) (string, string, error) {
	ns, rest, ok := strings.Cut(strings.TrimPrefix(uri, "mcpx://"), "/")
	if !ok {
		return "", "", fmt.Errorf("a resource URI looks like mcpx://<namespace>/<uri>, got %q", uri)
	}
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", "", err
	}
	session := b.app.mcpSession()
	raw, err := c.ReadResource(ctx, ns, rest, b.app.callContext(session, session))
	if err != nil {
		return "", "", err
	}
	return renderResource(raw)
}

// GetPrompt resolves a namespaced prompt back to its server.
func (b mcpBackend) GetPrompt(ctx context.Context, name string, args map[string]string) (string, error) {
	c, err := b.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	list, err := c.Prompts(ctx, nil)
	if err != nil {
		return "", err
	}
	for _, p := range list {
		if p.Namespace+"_"+p.Name != name && p.Name != name {
			continue
		}
		session := b.app.mcpSession()
		raw, err := c.GetPrompt(ctx, p.Namespace, p.Name, args, b.app.callContext(session, session))
		if err != nil {
			return "", err
		}
		return renderPrompt(raw), nil
	}
	return "", fmt.Errorf("no prompt named %q", name)
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
