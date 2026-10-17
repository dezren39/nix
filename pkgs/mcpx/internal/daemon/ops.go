package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/api"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/logstore"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/registry"
	"github.com/dezren39/mcpx/internal/tasks"
)

// routesV1Ops registers the operations that are not part of the original
// route table. They live in their own file so the surface can grow without
// the file that owns the daemon's lifecycle growing with it.
func (s *Server) routesV1Ops(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/log", s.handleLogQuery)
	mux.HandleFunc("GET /v1/stats", s.handleStats)
	mux.HandleFunc("GET /v1/registry/search", s.handleRegistrySearch)
	mux.HandleFunc("POST /v1/complete", s.handleComplete)
	mux.HandleFunc("GET /v1/tasks", s.handleTasksList)
	mux.HandleFunc("GET /v1/tasks/{id}", s.handleTaskGet)
	mux.HandleFunc("GET /v1/tasks/{id}/result", s.handleTaskResult)
	mux.HandleFunc("POST /v1/tasks/{id}/cancel", s.handleTaskCancel)
	mux.HandleFunc("GET /v1/openapi.json", s.handleOpenAPI)
}

// ---- the log ----

// logDir resolves where the JSONL logs live.
//
// The sink is asked first, because it is the file the daemon is actually
// writing: a config that changed since startup would otherwise send a query
// to a directory nothing is being written to, and an empty answer reads as
// "nothing happened".
func (s *Server) logDir() string {
	if s.sink != nil {
		if p := s.sink.Path(); p != "" {
			return filepath.Dir(p)
		}
	}
	if cfg := s.currentConfig(); cfg != nil && cfg.Logging.Dir != "" {
		return cfg.Logging.Dir
	}
	return filepath.Join(s.paths.State, "logs")
}

// openStore opens the index and brings it up to date, exactly as the CLI
// does. Ingest on the way into a query rather than in a background thread:
// a lazy index is either correct or visibly slow, and a background one can
// be quietly stale.
func (s *Server) openStore() (*logstore.Store, error) {
	dir := s.logDir()
	if err := os.MkdirAll(dir, defaults.DirMode); err != nil {
		return nil, err
	}
	st, err := logstore.Open(dir)
	if err != nil {
		return nil, err
	}
	if s.set.Bool("plumbing.indexOnQuery") {
		if _, err := st.Ingest(); err != nil {
			st.Close()
			return nil, err
		}
	}
	return st, nil
}

// logRecord is one record on the wire.
//
// A DTO rather than logstore.Record marshalled directly, because that type
// has no JSON tags and would put Go field names and a numeric slog level in
// front of every caller.
type logRecord struct {
	ID       int64          `json:"id"`
	Time     time.Time      `json:"time"`
	Level    string         `json:"level"`
	Msg      string         `json:"msg,omitempty"`
	Template string         `json:"template,omitempty"`
	Attrs    map[string]any `json:"attrs,omitempty"`
}

func wireRecords(recs []logstore.Record) []logRecord {
	out := make([]logRecord, 0, len(recs))
	for _, r := range recs {
		out = append(out, logRecord{ID: r.ID, Time: r.Time,
			Level: logging.LevelName(r.Level), Msg: r.Msg,
			Template: r.Template, Attrs: r.Attrs})
	}
	return out
}

// logFilterNames are the query parameters that narrow a log query. They are
// listed rather than derived so that adding one to the op declaration without
// deciding what chain should do with it is a compile-free but visible
// omission rather than a silent drop.
var logFilterNames = []string{
	"level", "event", "server", "tool", "session", "trace", "grep", "since", "until",
}

func namedFilters(q url.Values) []string {
	var out []string
	for _, name := range logFilterNames {
		if q.Get(name) != "" {
			out = append(out, name)
		}
	}
	return out
}

func (s *Server) handleLogQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit == 0 {
		limit = s.callSettings(r).Int("logstore.queryLimit")
	}
	query := logstore.Query{
		Level: q.Get("level"), Event: q.Get("event"), Server: q.Get("server"),
		Tool: q.Get("tool"), Session: q.Get("session"), Trace: q.Get("trace"),
		Grep: q.Get("grep"), Limit: limit, Reverse: q.Get("reverse") == "1",
	}
	now := time.Now()
	var err error
	if query.Since, err = logstore.ParseWhen(q.Get("since"), now); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if query.Until, err = logstore.ParseWhen(q.Get("until"), now); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	st, err := s.openStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	defer st.Close()

	if chain := q.Get("chain"); chain != "" {
		// Chain walks a trace tree by id and takes no filters. Accepting
		// them and dropping them returned the whole tree at every level and
		// looked like it had worked, which is a worse answer than a refusal:
		// a caller reading it concludes the trace touched everything.
		if named := namedFilters(q); len(named) > 0 {
			writeErr(w, http.StatusBadRequest, fmt.Errorf(
				"chain returns a whole trace tree and cannot be filtered; "+
					"drop %s, or drop chain", strings.Join(named, ", ")))
			return
		}
		levels, err := st.Chain(chain, query.Limit)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		type level struct {
			Trace   string      `json:"trace"`
			Parent  string      `json:"parent,omitempty"`
			Depth   int         `json:"depth"`
			Records []logRecord `json:"records"`
		}
		out := make([]level, 0, len(levels))
		for _, l := range levels {
			out = append(out, level{Trace: l.Trace, Parent: l.Parent,
				Depth: l.Depth, Records: wireRecords(l.Records)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"chain": out})
		return
	}

	recs, err := st.Records(query)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": wireRecords(recs)})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dim := q.Get("by")
	if dim == "" {
		dim = "calls"
	}
	top, _ := strconv.Atoi(q.Get("top"))
	if top <= 0 {
		top = s.callSettings(r).Int("stats.top")
	}
	// Limit -1 is "every record in the window": an aggregate over the last
	// hundred rows is not an aggregate, it is a sample nobody asked for.
	query := logstore.Query{Server: q.Get("server"), Tool: q.Get("tool"),
		Session: q.Get("session"), Limit: -1}
	now := time.Now()
	var err error
	if query.Since, err = logstore.ParseWhen(q.Get("since"), now); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if query.Until, err = logstore.ParseWhen(q.Get("until"), now); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	st, err := s.openStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	defer st.Close()

	var rows any
	switch dim {
	case "calls":
		rows, err = st.Calls(query)
	case "servers":
		rows, err = st.Servers(query)
	case "instances":
		rows, err = st.Instances(query)
	case "errors":
		var list []logstore.ErrorStat
		if list, err = st.Errors(query); err == nil {
			rows = capped(list, top)
		}
	case "sessions":
		var list []logstore.SessionStat
		if list, err = st.Sessions(query); err == nil {
			rows = capped(list, top)
		}
	case "volume":
		var buckets []logstore.VolumeBucket
		var files []logstore.FileStat
		if buckets, files, err = st.Volume(query); err == nil {
			rows = map[string]any{"hours": buckets, "files": files}
		}
	case "slowest":
		rows, err = st.Slowest(query, top)
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"unknown dimension %q; want one of %v", dim, logstore.Dimensions))
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dimension": dim, "rows": rows})
}

func capped[T any](rows []T, top int) []T {
	if top > 0 && len(rows) > top {
		return rows[:top]
	}
	return rows
}

// ---- the registry ----

func (s *Server) handleRegistrySearch(w http.ResponseWriter, r *http.Request) {
	cs := s.callSettings(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = cs.Int("registry.limit")
	}
	// The daemon now has a settings resolver, so the registry URL is read
	// the same way everywhere: a config file, MCPX_REGISTRY_URL, or a flag.
	// It used to be environment-only here and config-only in the CLI, which
	// meant `mcpx registry search` and GET /v1/registry/search could quietly
	// query two different registries.
	client := registry.New(cs.String("registry.url"), registry.Options{
		Timeout:  cs.Duration("registry.timeout"),
		PageSize: cs.Int("registry.pageSize"),
		MaxPages: cs.Int("registry.maxPages"),
	})
	res, err := client.Search(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	type entry struct {
		registry.Server
		Namespace string `json:"namespace"`
		Install   string `json:"install,omitempty"`
		AddWith   string `json:"addWith"`
	}
	out := make([]entry, 0, len(res.Servers))
	for _, srv := range res.Servers {
		e := entry{Server: srv, Namespace: registry.Namespace(srv.Name),
			AddWith: "mcpx registry add " + srv.Name + " --write"}
		if in, ierr := srv.ToInstall(false); ierr == nil {
			e.Install = in.How
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out, "truncated": res.Truncated})
}

// ---- completion ----

type completeReq struct {
	Server string `json:"server"`
	Ref    struct {
		Type string `json:"type"`
		Name string `json:"name"`
		URI  string `json:"uri"`
	} `json:"ref"`
	Argument struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"argument"`
	// Arguments is CompleteRequest.context.arguments: the values already
	// chosen for the prompt's other arguments, which an upstream may use to
	// narrow its answer. Named apart from Context, the caller's identity.
	Arguments map[string]string  `json:"arguments"`
	Context   config.CallContext `json:"context"`
	Session   string             `json:"session"`
}

func (s *Server) handleComplete(w http.ResponseWriter, r *http.Request) {
	var req completeReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body,
		s.set.Bytes("http.bodyLimit"))).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Server == "" {
		writeErr(w, http.StatusBadRequest, errors.New("server is required"))
		return
	}
	switch req.Ref.Type {
	case "ref/prompt", "ref/resource":
	default:
		writeErr(w, http.StatusBadRequest,
			errors.New(`ref.type is "ref/prompt" or "ref/resource"`))
		return
	}
	cs := s.callSettings(r)
	p, ok := s.reg.Pool(req.Server)
	if !ok {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown server or namespace %q", req.Server))
		return
	}

	// Upstream first, always. The server knows the *values* an argument may
	// take; mcpx knows only the names it has cached, which is a floor
	// rather than a substitute.
	fwd := map[string]any{"ref": req.Ref, "argument": req.Argument}
	if len(req.Arguments) > 0 {
		fwd["context"] = map[string]any{"arguments": req.Arguments}
	}
	params, _ := json.Marshal(fwd)
	cc := callContext(r, req.Context, req.Session)
	raw, upstream, err := p.Complete(r.Context(), s.reg.keyFor(p, cc), params)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if upstream {
		// The server answers a CompleteResult, whose payload is under
		// `completion`. Passing the envelope through would nest it, and a
		// client reading `completion.values` would find `completion.completion`.
		var res struct {
			Completion json.RawMessage `json:"completion"`
		}
		payload := json.RawMessage(raw)
		if json.Unmarshal(raw, &res) == nil && len(res.Completion) > 0 {
			payload = res.Completion
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"completion": payload, "upstream": true, "source": "upstream"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"completion": localCompletion(p.CachedPrompts(), p.CachedTemplates(), req,
			cs.Int("completion.maxValues")),
		// Said plainly, because a client that cannot tell an empty answer
		// from an unimplemented one shows nothing and the user concludes
		// completion is broken. `upstream` stays for the callers that read
		// it; `source` says the same thing in a word rather than a
		// negation.
		"upstream": false,
		"source":   "cache",
		"because":  "this server did not declare the completions capability",
	})
}

// localCompletion answers from the schemas mcpx already holds.
//
// Only the names it knows: prompt names and resource template URIs. A
// server's own completion knows the *values* an argument may take, which
// mcpx cannot guess, so this is a floor rather than a substitute.
func localCompletion(prompts []mcpclient.Prompt, templates []mcpclient.Resource, req completeReq, max int) map[string]any {
	var values []string
	switch req.Ref.Type {
	case "ref/prompt":
		for _, p := range prompts {
			if hasPrefixFold(p.Name, req.Argument.Value) {
				values = append(values, p.Name)
			}
		}
	case "ref/resource":
		for _, t := range templates {
			if hasPrefixFold(t.URI, req.Argument.Value) {
				values = append(values, t.URI)
			}
		}
	}
	if values == nil {
		values = []string{}
	}
	// The specification caps a completion reply at a hundred; the setting
	// may lower that but a conforming client will reject more.
	total := len(values)
	if max > 0 && len(values) > max {
		values = values[:max]
	}
	return map[string]any{"values": values, "total": total, "hasMore": total > len(values)}
}

// ---- tasks ----

// taskStore is the daemon's own, created on first use so a daemon that never
// runs one carries nothing.
func (s *Server) taskStore() *tasks.Store {
	s.taskOnce.Do(func() {
		if s.paths.State != "" {
			dbPath := filepath.Join(s.paths.State, "tasks.db")
			if store, err := tasks.Open(dbPath); err == nil {
				s.tasks = store
			}
		}
		if s.tasks == nil {
			s.tasks = tasks.New()
		}
		if s.set != nil {
			s.tasks.PollInterval = s.set.Duration("protoTasks.pollInterval")
		}
	})
	return s.tasks
}

func (s *Server) handleTasksList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tasks": s.taskStore().List()})
}

func (s *Server) handleTaskGet(w http.ResponseWriter, r *http.Request) {
	t, ok := s.taskStore().Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, tasks.ErrNoTask{ID: r.PathValue("id")})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleTaskResult(w http.ResponseWriter, r *http.Request) {
	wait := s.callSettings(r).Duration("tasks.resultWait")
	if ms, err := strconv.Atoi(r.URL.Query().Get("waitMs")); err == nil && ms > 0 {
		wait = time.Duration(ms) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(r.Context(), wait)
	defer cancel()

	result, fault, err := s.taskStore().Result(ctx, r.PathValue("id"))
	switch {
	case err != nil:
		var missing tasks.ErrNoTask
		if errors.As(err, &missing) {
			writeErr(w, http.StatusNotFound, missing)
			return
		}
		// 408 rather than an error body: the task is still running, and a
		// caller that waited long enough should retry rather than conclude
		// it failed.
		writeJSON(w, http.StatusRequestTimeout, map[string]any{
			"error": "the task has not finished yet", "taskId": r.PathValue("id")})
	case fault != nil:
		body := map[string]any{"error": fault.Message, "code": fault.Code}
		// Set by startCallTask, so a call collected later is explained the
		// same as one answered at once.
		if ds, ok := fault.Data.([]diagnose.Diagnostic); ok {
			body["diagnostics"] = ds
		}
		writeJSON(w, http.StatusBadGateway, body)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"result": result})
	}
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	reason := r.URL.Query().Get("reason")
	if reason == "" {
		reason = "cancelled by client"
	}
	t, ok := s.taskStore().CancelWithReason(r.PathValue("id"), reason)
	if !ok {
		writeErr(w, http.StatusNotFound, tasks.ErrNoTask{ID: r.PathValue("id")})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// startCallTask runs a tool call in the background and hands back a handle.
func (s *Server) startCallTask(ttl int64, server, tool string, cc config.CallContext, args any) tasks.Task {
	return s.taskStore().Start(ttl, func(ctx context.Context) (any, *tasks.Fault) {
		start := time.Now()
		res, err := s.reg.Call(ctx, server, tool, cc, args)
		if err != nil {
			f := &tasks.Fault{Code: http.StatusBadGateway, Message: err.Error()}
			if ds := callDiagnostics(err); len(ds) > 0 {
				f.Data = ds
			}
			return nil, f
		}
		return map[string]any{"result": res,
			"durationMs": time.Since(start).Milliseconds()}, nil
	})
}

// ---- the specification ----

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	doc := api.OpenAPI(s.version)
	// Generated per request rather than once, so a server that appears after
	// startup is described without a restart. The per-tool paths were served
	// only by `mcpx serve --transport http`; a document without them
	// describes an API whose most useful half is missing.
	if paths, ok := doc["paths"].(map[string]any); ok {
		for k, v := range s.toolPaths() {
			paths[k] = v
		}
	}
	writeJSON(w, http.StatusOK, doc)
}

// toolPaths describes every upstream tool as its own POST path.
func (s *Server) toolPaths() map[string]any {
	out := map[string]any{}
	for _, t := range s.reg.Tools(nil) {
		schema := any(map[string]any{"type": "object"})
		if len(t.InputSchema) > 0 {
			schema = json.RawMessage(t.InputSchema)
		}
		out["/v1/call/"+t.Namespace+"/"+t.Tool] = map[string]any{
			"post": map[string]any{
				"operationId": "call_" + t.Namespace + "_" + t.Tool,
				"summary":     firstSentence(t.Description),
				"tags":        []string{t.Namespace},
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{"schema": schema}}},
				"responses": map[string]any{
					"200": map[string]any{"description": "the tool result"}},
			},
		}
	}
	return out
}

// firstSentence keeps a summary to one line, because a whole description in
// an OpenAPI summary renders as a wall in every viewer.
func firstSentence(s string) string {
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// hasPrefixFold matches the way a person types: case is not a filter.
func hasPrefixFold(s, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
}
