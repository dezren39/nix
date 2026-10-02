package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/elicit"
	"github.com/dezren39/mcpx/internal/events"
	"github.com/dezren39/mcpx/internal/execsvc"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/spec"
	"github.com/dezren39/mcpx/internal/tasks"
)

// routesProto registers the operations that let a client of mcpx answer a
// question an upstream server asked, and say what mcpx speaks.
func (s *Server) routesProto(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/ask", s.handleAskBegin)
	mux.HandleFunc("GET /v1/ask/{id}", s.handleAskPoll)
	mux.HandleFunc("POST /v1/ask/{id}/answers", s.handleAskAnswers)
	mux.HandleFunc("POST /v1/ask/{id}/abandon", s.handleAskAbandon)
	mux.HandleFunc("GET /v1/protocol", s.handleProtocol)
	mux.HandleFunc("POST /v1/tools/{tool}", s.handleToolInvoke)
	mux.HandleFunc("POST /v1/call/{server}/{tool}", s.handleCallPath)
}

// ---- a call that can be interrupted ----
//
// An ordinary /v1/call holds the request open for the whole of the upstream
// call. That is fine until the server stops to ask something, because the
// answer may arrive from a different process, or from a modern client that
// has to be *given back its request* before it can answer at all. So a call
// that might be interrupted runs as a task -- the same internal/tasks store
// /v1/call's own task option uses, and collectable from the same
// /v1/tasks/{id} -- and this is the handle to it.
//
// Three methods can elicit: tools/call, prompts/get and resources/read. They
// share one entry point rather than each growing a task option of its own,
// because the correlation machinery below is identical for all three and
// three copies of it is three sets of bugs.

// askCall is one interruptible call.
type askCall struct {
	ID      string
	Server  string
	Key     string
	Session string
	// Run is the exec run id when this call is a script, "" otherwise.
	Run string

	mu sync.Mutex
	// questions are what the upstream server has asked so far, in order.
	questions []mcpserver.Question
	// changed is closed and replaced whenever a question appears, so a
	// waiting poll wakes at once rather than at the end of its interval.
	changed chan struct{}
}

func (a *askCall) add(q mcpserver.Question) {
	a.mu.Lock()
	a.questions = append(a.questions, q)
	prev := a.changed
	a.changed = make(chan struct{})
	a.mu.Unlock()
	if prev != nil {
		close(prev)
	}
}

func (a *askCall) snapshot() ([]mcpserver.Question, chan struct{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.changed == nil {
		a.changed = make(chan struct{})
	}
	return append([]mcpserver.Question(nil), a.questions...), a.changed
}

// askTable maps a live upstream connection back to the call using it.
//
// This is the whole of the correlation. A question arrives on a connection,
// not on a call: the server sends elicitation/create down the same pipe it
// is answering tools/call on, and nothing in the frame says which call it
// belongs to. What mcpx does know is that one pooled instance serves one
// scope key, so a question on (server, key) belongs to whatever is calling
// on (server, key).
//
// When two calls share a key -- which a shared server allows -- the
// attribution is genuinely ambiguous, and the honest answer is to make none
// and let the broker route it. Guessing would hand one client's credential
// prompt to another client.
//
// A script (mcpx_exec) is one ask-call making many upstream calls. Each of
// them joins the table under (server, key) as the *run's* askCall, so a
// question on a connection whose in-flight calls all belong to one run is
// that run's. Plain calls do not register; the pool's in-flight count is what
// makes a shared key ambiguous (see askFor).
type askTable struct {
	mu    sync.Mutex
	byID  map[string]*askCall
	byKey map[string][]*askCall
	// byRun maps an exec run id to the ask-call running that script.
	byRun map[string]*askCall
}

func newAskTable() *askTable {
	return &askTable{byID: map[string]*askCall{}, byKey: map[string][]*askCall{},
		byRun: map[string]*askCall{}}
}

func askKey(server, key string) string { return server + "\x00" + key }

func (t *askTable) begin(id, server, key, session string) *askCall {
	a := &askCall{ID: id, Server: server, Key: key, Session: session,
		changed: make(chan struct{})}
	k := askKey(server, key)
	t.mu.Lock()
	t.byID[id] = a
	t.byKey[k] = append(t.byKey[k], a)
	t.mu.Unlock()
	return a
}

// beginRun registers the ask-call running a script. It holds no key of its
// own; its upstream calls join under theirs as they are made.
func (t *askTable) beginRun(id, run, session string) *askCall {
	a := &askCall{ID: id, Run: run, Session: session, changed: make(chan struct{})}
	t.mu.Lock()
	t.byID[id] = a
	t.byRun[run] = a
	t.mu.Unlock()
	return a
}

// join records one in-flight upstream call on (server, key) as a member of
// run, when that run is live. leave undoes exactly this entry. A call from no
// live run registers nothing: the pool's in-flight count already sees it.
func (t *askTable) join(run, server, key string) (leave func()) {
	k := askKey(server, key)
	t.mu.Lock()
	owner := t.byRun[run]
	if run == "" || owner == nil {
		t.mu.Unlock()
		return func() {}
	}
	t.byKey[k] = append(t.byKey[k], owner)
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		list := t.byKey[k]
		for i, other := range list {
			if other == owner {
				t.byKey[k] = append(list[:i:i], list[i+1:]...)
				break
			}
		}
		if len(t.byKey[k]) == 0 {
			delete(t.byKey, k)
		}
	}
}

func (t *askTable) end(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	a := t.byID[id]
	delete(t.byID, id)
	if a == nil {
		return
	}
	if a.Run != "" {
		delete(t.byRun, a.Run)
	}
	// Every key, not just the call's own: a run's members may still be in
	// flight after the script is gone (killed on timeout, say), and a
	// question they raise must not be attributed to a call nobody polls.
	for k, list := range t.byKey {
		kept := list[:0]
		for _, other := range list {
			if other != a {
				kept = append(kept, other)
			}
		}
		if len(kept) == 0 {
			delete(t.byKey, k)
		} else {
			t.byKey[k] = kept
		}
	}
}

func (t *askTable) get(id string) (*askCall, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	a, ok := t.byID[id]
	return a, ok
}

// owner returns the one call that owns every registered entry on this
// connection, and how many entries that is. Entries are interruptible calls
// and the in-flight upstream calls of a running script (its members); plain
// calls are not registered here -- the pool counts them (see askFor).
func (t *askTable) owner(server, key string) (*askCall, int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.byKey[askKey(server, key)]
	if len(list) == 0 {
		return nil, 0, false
	}
	// Several entries are one run calling in parallel; a second owner is
	// two callers sharing an instance, which is ambiguous.
	for _, a := range list[1:] {
		if a != list[0] {
			return nil, 0, false
		}
	}
	return list[0], len(list), true
}

// askFor returns the call a question on (server, key) belongs to.
//
// One rule for single calls and scripts alike: a call owns the question when
// every upstream request in flight on the key is one of its own. The table
// knows the owned requests; the pool counts all of them, from /v1/call, exec,
// the CLI and the plugin, none of which register. Equal counts mean nobody
// else is on the connection. Before #229 the table alone decided, and one ask
// call beside a plain call on the same key was handed the plain caller's
// question.
//
// The residual window: an entry is registered just before its request is
// counted, so a stranger's question arriving in that instant, while the
// owner's request has not yet been sent, can still be misattributed.
func (r *Registry) askFor(ctx context.Context, server, key string) (*askCall, bool) {
	// The call itself, when the question arrived on its context: a 2026-07-28
	// upstream asks inside the call's own result, so there is no doubt whose
	// question it is. Inferring it from the instance alone failed whenever
	// a second call shared the instance -- an earlier caller that never
	// resumed was enough -- and the question went to the broker, where the
	// client that could answer it never saw it and the request hung.
	if id, ok := ctx.Value(askCallKey{}).(string); ok {
		if a, ok := r.asks.get(id); ok && a.Server == server {
			return a, true
		}
	}
	p, ok := r.Pool(server)
	if !ok {
		return nil, false
	}
	return r.asks.sole(server, key, p.InFlight(key))
}

// sole is askFor's rule given the pool's in-flight count for the key.
func (t *askTable) sole(server, key string, inflight int) (*askCall, bool) {
	a, n, ok := t.owner(server, key)
	if !ok || inflight != n {
		return nil, false
	}
	return a, true
}

// attach records a question against the call that provoked it, if one can be
// identified, and returns whether it was.
func (r *Registry) attach(ctx context.Context, server, key string, req elicit.Request, method string, params json.RawMessage) (string, bool) {
	a, ok := r.askFor(ctx, server, key)
	if !ok {
		return "", false
	}
	a.add(mcpserver.Question{
		ID: req.ID, Method: method, Params: params,
		Mode: string(req.Mode), Server: server, Key: mcpclient.InputKey(ctx),
		Round: mcpclient.InputRound(ctx),
	})
	return a.ID, true
}

// CallAsk runs an interruptible call and registers it for correlation.
func (r *Registry) CallAsk(ctx context.Context, id, server, tool string, cc config.CallContext, args any) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, fmt.Errorf("unknown server or namespace %q", server)
	}
	// The key is only known after disambiguation, and the call has to be in
	// the table before the upstream request goes out, so it is registered
	// between the two. The guard's own questions are mcpx's, not the
	// server's, and are never attributed through the table.
	key, err := r.resolveAndGuard(ctx, p, server, tool, cc)
	if err != nil {
		return nil, err
	}
	r.beginAsk(id, server, key, cc.SessionID)
	defer r.endAsk(id)
	ctx = withAskCall(ctx, id)
	return p.Call(ctx, key, tool, args)
}

// ReadResourceAsk is resources/read, interruptibly.
func (r *Registry) ReadResourceAsk(ctx context.Context, id, server, uri string, cc config.CallContext) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, fmt.Errorf("unknown server or namespace %q", server)
	}
	key := r.keyFor(p, cc)
	r.beginAsk(id, server, key, cc.SessionID)
	defer r.endAsk(id)
	ctx = withAskCall(ctx, id)
	return p.ReadResource(ctx, key, upstreamResourceURI(ctx, p, uri))
}

// GetPromptAsk is prompts/get, interruptibly.
func (r *Registry) GetPromptAsk(ctx context.Context, id, server, name string, args map[string]string, cc config.CallContext) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, fmt.Errorf("unknown server or namespace %q", server)
	}
	key := r.keyFor(p, cc)
	r.beginAsk(id, server, key, cc.SessionID)
	defer r.endAsk(id)
	ctx = withAskCall(ctx, id)
	return p.GetPrompt(ctx, key, name, args)
}

// runKey carries the exec run a /v1/call was made from.
type runKey struct{}

// withRun marks ctx as belonging to the script run, when there is one.
func withRun(ctx context.Context, run string) context.Context {
	if run == "" {
		return ctx
	}
	return context.WithValue(ctx, runKey{}, run)
}

func runFrom(ctx context.Context) string {
	v, _ := ctx.Value(runKey{}).(string)
	return v
}

// joinAsk registers an ordinary call on (server, key) for correlation. See
// askTable for why even calls that can own no question are registered.
func (r *Registry) joinAsk(ctx context.Context, server, key string) func() {
	if r.asks == nil {
		return func() {}
	}
	return r.asks.join(runFrom(ctx), server, key)
}

func (r *Registry) beginAsk(id, server, key, session string) {
	r.asks.begin(id, server, key, session)
}

func (r *Registry) endAsk(id string) { r.asks.end(id) }

// Asks exposes the table so the routes below can read it.
func (r *Registry) Asks() *askTable { return r.asks }

// ---- the routes ----

type askReq struct {
	// Kind is the MCP method: tools/call, prompts/get or resources/read.
	Kind   string `json:"kind"`
	Server string `json:"server"`
	Tool   string `json:"tool"`
	Name   string `json:"name"`
	URI    string `json:"uri"`
	// Args is a tool's arguments; Arguments is a prompt's, which are
	// strings. Kept apart because the two are different types and merging
	// them would make one of the two silently lossy.
	Args      json.RawMessage    `json:"args"`
	Arguments map[string]string  `json:"arguments"`
	Context   config.CallContext `json:"context"`
	Session   string             `json:"session"`
	TTL       int64              `json:"ttl"`
	// Source and Options are a script, for kind "exec": mcpx_exec, whose
	// many upstream calls are correlated through its run id.
	Source  string          `json:"source"`
	Options execsvc.Options `json:"options"`
	// Relay is what the client asked of a tools/call; see CallRelay. What
	// comes back is collected by the polls, as "notifications".
	Relay *CallRelay `json:"relay,omitempty"`
}

func (s *Server) handleAskBegin(w http.ResponseWriter, r *http.Request) {
	var req askReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, defaults.HTTPCallBodyLimit)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	switch {
	case req.Kind == "exec":
		if req.Source == "" {
			writeErr(w, http.StatusBadRequest, errors.New("source is required"))
			return
		}
	case req.Server == "":
		writeErr(w, http.StatusBadRequest, errors.New("server is required"))
		return
	}
	switch req.Kind {
	case "tools/call", "prompts/get", "resources/read", "exec":
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"kind is tools/call, prompts/get, resources/read or exec, not %q", req.Kind))
		return
	}
	cc := callContext(r, req.Context, req.Session)
	ttl := req.TTL
	if ttl <= 0 {
		ttl = s.set.Duration("proto.askTTL").Milliseconds()
	}

	// The task id is the call id, and the call has to be registered under it
	// before the upstream request goes out -- a server can ask its question
	// in the first millisecond. Start hands the id back on this channel, and
	// the body waits for it, so there is no window in which a question
	// arrives for a call nothing has heard of.
	ready := make(chan string, 1)
	caps := withClientCaps(context.Background(), r)
	t := s.taskStore().Start(ttl, func(ctx context.Context) (any, *tasks.Fault) {
		id := <-ready
		start := time.Now()
		// The task outlives this request, so the header is carried over
		// by value rather than through r.Context().
		ctx = mcpclient.WithClientCapabilities(ctx, mcpclient.ClientCapabilitiesFrom(caps))
		ctx = mcpclient.WithCallerVersion(ctx, mcpclient.CallerVersionFrom(caps))
		var (
			raw json.RawMessage
			err error
		)
		switch req.Kind {
		case "exec":
			return s.runAsk(ctx, id, cc, req)
		case "tools/call":
			var args any = map[string]any{}
			if len(req.Args) > 0 {
				if uerr := json.Unmarshal(req.Args, &args); uerr != nil {
					return nil, &tasks.Fault{Code: http.StatusBadRequest, Message: "args: " + uerr.Error()}
				}
			}
			if req.Relay != nil {
				buf := newRelayBuffer()
				askRelays.Store(id, buf)
				ctx = mcpclient.WithRelay(ctx, buf.relay(req.Relay))
			}
			raw, err = s.reg.CallAsk(ctx, id, req.Server, req.Tool, cc, args)
		case "prompts/get":
			raw, err = s.reg.GetPromptAsk(ctx, id, req.Server, req.Name, req.Arguments, cc)
		case "resources/read":
			raw, err = s.reg.ReadResourceAsk(ctx, id, req.Server, req.URI, cc)
		}
		if err != nil {
			f := &tasks.Fault{Code: http.StatusBadGateway, Message: err.Error()}
			if up := callErrorBody(err).Upstream; up != nil {
				f.Data = up
			}
			return nil, f
		}
		return map[string]any{"result": raw, "kind": req.Kind, "server": req.Server,
			"durationMs": time.Since(start).Milliseconds()}, nil
	})
	ready <- t.TaskID

	writeJSON(w, http.StatusAccepted, map[string]any{"callId": t.TaskID, "task": t})
}

// runAsk runs a script as an interruptible call.
//
// The run id is chosen here, before the script exists, and registered
// against this call: the script's first tool call may be asked a question
// in its first millisecond, and it carries the run id (X-Mcpx-Run) so the
// daemon can tell which call it belongs to.
func (s *Server) runAsk(ctx context.Context, id string, cc config.CallContext, req askReq) (any, *tasks.Fault) {
	run := execsvc.NewRunID()
	opts := req.Options
	if opts.Session == "" {
		opts.Session = cc.SessionID
	}
	s.reg.asks.beginRun(id, run, opts.Session)
	defer s.reg.endAsk(id)
	start := time.Now()
	res, err := s.ExecService().Run(ctx, execsvc.Request{Source: req.Source, Opts: opts, RunID: run}, nil)
	if err != nil && res == nil {
		return nil, &tasks.Fault{Code: http.StatusBadRequest, Message: err.Error()}
	}
	raw, merr := json.Marshal(res)
	if merr != nil {
		return nil, &tasks.Fault{Code: http.StatusInternalServerError, Message: merr.Error()}
	}
	return map[string]any{"result": json.RawMessage(raw), "kind": req.Kind,
		"durationMs": time.Since(start).Milliseconds()}, nil
}

// handleAskPoll reports where a call has got to, waiting for it to move.
//
// Long-polled rather than polled, because the thing being waited for is a
// person answering a question. A fixed interval would be either a busy loop
// or a delay somebody notices, and this is neither.
func (s *Server) handleAskPoll(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wait := defaults.ProtoAskPoll
	if ms, err := strconv.Atoi(r.URL.Query().Get("waitMs")); err == nil && ms > 0 {
		wait = time.Duration(ms) * time.Millisecond
	}
	t, known := s.taskStore().Get(id)
	if !known {
		writeErr(w, http.StatusNotFound, tasks.ErrNoTask{ID: id})
		return
	}

	var notes *relayBuffer
	if v, ok := askRelays.Load(id); ok {
		notes = v.(*relayBuffer)
	}
	call, live := s.reg.Asks().get(id)
	if !tasks.Terminal(t.Status) && live {
		before, changed := call.snapshot()
		// Wait when there is nothing to answer -- which is not the same as
		// nothing having been asked. After a question is answered the call
		// keeps running, and returning immediately because it once asked
		// something would turn the caller's long poll into a spin.
		if len(s.openQuestions(before)) == 0 && (notes == nil || !notes.pending()) {
			var wake chan struct{}
			if notes != nil {
				wake = notes.wake
			}
			ctx, cancel := context.WithTimeout(r.Context(), wait)
			done := s.taskDone(ctx, id)
			select {
			case <-changed:
			case <-done:
			case <-wake:
			case <-ctx.Done():
			}
			cancel()
		}
	}

	t, _ = s.taskStore().Get(id)
	out := map[string]any{"callId": id, "status": t.Status}
	if call != nil {
		qs, _ := call.snapshot()
		out["questions"] = s.openQuestions(qs)
	}
	if tasks.Terminal(t.Status) {
		result, fault, err := s.taskStore().Result(r.Context(), id)
		switch {
		case err != nil:
			out["status"] = tasks.Failed
			out["error"] = err.Error()
		case fault != nil:
			out["error"] = fault.Message
			if up, ok := fault.Data.(*UpstreamError); ok {
				out["upstream"] = up
			}
		default:
			out["result"] = result
		}
		out["done"] = true
	}
	if notes != nil {
		// Drained after the status is read, so a call that finished has
		// already relayed everything it will: none is left behind.
		if n := notes.drain(); len(n) > 0 {
			out["notifications"] = n
		}
		if out["done"] == true {
			askRelays.Delete(id)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// openQuestions drops the ones already answered, so a client is never shown
// a question it cannot usefully answer.
func (s *Server) openQuestions(qs []mcpserver.Question) []mcpserver.Question {
	out := make([]mcpserver.Question, 0, len(qs))
	for _, q := range qs {
		if s.reg.broker != nil {
			if _, answered, _ := s.reg.broker.Lookup(q.ID); answered {
				continue
			}
		}
		out = append(out, q)
	}
	return out
}

// taskDone returns a channel closed when a task reaches a terminal status.
func (s *Server) taskDone(ctx context.Context, id string) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		_, _, _ = s.taskStore().Result(ctx, id)
	}()
	return ch
}

// handleAskAnswers relays a client's answers to the broker.
//
// The wire shape is MCP's own -- an ElicitResult or a CreateMessageResult,
// exactly as the client produced it -- and the translation into the broker's
// vocabulary happens here. A client should not have to learn mcpx's storage
// model to answer a question the protocol already defines an answer for.
func (s *Server) handleAskAnswers(w http.ResponseWriter, r *http.Request) {
	if s.reg.broker == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("elicitation is disabled in this daemon"))
		return
	}
	var body struct {
		Answers map[string]json.RawMessage `json:"answers"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, defaults.HTTPStreamBufferMax)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	call, ok := s.reg.Asks().get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no call "+r.PathValue("id")+
			"; it may have finished or expired"))
		return
	}
	known := map[string]mcpserver.Question{}
	qs, _ := call.snapshot()
	for _, q := range qs {
		known[q.ID] = q
	}

	by := r.Header.Get("X-Mcpx-Answerer")
	if by == "" {
		by = "mcp-client"
	}
	accepted := 0
	problems := map[string]string{}
	for id, raw := range body.Answers {
		q, isKnown := known[id]
		if !isKnown {
			// Refused rather than passed through. A client answering a
			// question this call did not raise is either confused or
			// reaching for somebody else's.
			problems[id] = "this call did not ask that"
			continue
		}
		ans, err := answerFromResult(id, q.Method, raw)
		if err != nil {
			problems[id] = err.Error()
			continue
		}
		ans.By = by
		if err := s.reg.broker.Respond(ans); err != nil {
			problems[id] = err.Error()
			continue
		}
		accepted++
		s.Events.Publish(events.Event{Kind: events.ElicitAnswered, Server: q.Server,
			Trace: call.ID, Data: mustJSON(map[string]any{
				"id": id, "action": string(ans.Action), "by": by, "callId": call.ID})})
	}
	out := map[string]any{"accepted": accepted}
	if len(problems) > 0 {
		out["problems"] = problems
	}
	writeJSON(w, http.StatusOK, out)
}

// answerFromResult converts an MCP result into the broker's answer.
func answerFromResult(id, method string, raw json.RawMessage) (elicit.Answer, error) {
	if method == "sampling/createMessage" || method == "roots/list" {
		// Sampling has no decline shape in the specification, so anything
		// that arrives is an acceptance and a refusal has to be an error.
		if len(raw) == 0 || string(raw) == "null" {
			return elicit.Answer{}, fmt.Errorf("a %s answer must be its result object", method)
		}
		return elicit.Answer{ID: id, Action: elicit.Accept, Content: raw}, nil
	}
	var res struct {
		Action  string          `json:"action"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return elicit.Answer{}, err
	}
	switch elicit.Action(res.Action) {
	case elicit.Accept:
		return elicit.Answer{ID: id, Action: elicit.Accept, Content: res.Content}, nil
	case elicit.Decline:
		return elicit.Answer{ID: id, Action: elicit.Decline}, nil
	case elicit.Cancel, "":
		// An answer with no action is a dismissal, not a refusal. Reading it
		// as decline would tell the server the user said no.
		return elicit.Answer{ID: id, Action: elicit.Cancel}, nil
	}
	return elicit.Answer{}, fmt.Errorf("action is accept, decline or cancel, not %q", res.Action)
}

func (s *Server) handleAskAbandon(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	askRelays.Delete(id)
	t, ok := s.taskStore().Cancel(id)
	if !ok {
		writeErr(w, http.StatusNotFound, tasks.ErrNoTask{ID: id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"callId": id, "status": t.Status})
}

// ---- what mcpx speaks ----

// handleProtocol reports the revisions and mechanisms in play.
//
// Written because the matrix in docs/protocol.md is a claim about the code,
// and a claim nobody can check from a running daemon is a claim that rots.
// This is the same table the code consults, so the two cannot disagree.
func (s *Server) handleProtocol(w http.ResponseWriter, r *http.Request) {
	type serverRow struct {
		Server     string `json:"server"`
		Namespace  string `json:"namespace"`
		Preference string `json:"preference,omitempty"`
		Era        string `json:"era,omitempty"`
		Negotiated string `json:"negotiated,omitempty"`
		// EraSource is how the live instance's era was settled: probe,
		// cache or forced.
		EraSource string `json:"eraSource,omitempty"`
		// Cached is what the era cache remembers, live instance or not.
		Cached   *pool.EraRecord `json:"cached,omitempty"`
		Declared []string        `json:"declared,omitempty"`
	}
	var upstream []serverRow
	for _, name := range s.reg.Names() {
		p, ok := s.reg.Pool(name)
		if !ok {
			continue
		}
		era, negotiated := p.Era()
		row := serverRow{Server: name, Namespace: p.Namespace(),
			Preference: string(p.Preference()), Era: string(era), Negotiated: negotiated,
			EraSource: p.EraSource()}
		if rec, ok := p.CachedEra(); ok {
			row.Cached = &rec
		}
		for cap := range p.Capabilities() {
			row.Declared = append(row.Declared, cap)
		}
		sort.Strings(row.Declared)
		upstream = append(upstream, row)
	}
	pol := spec.Current()
	writeJSON(w, http.StatusOK, map[string]any{
		// The effective revision policy (#307): spec.precedence with
		// spec.first applied, and which revisions are held strictly.
		"spec": map[string]any{"precedence": pol.Order(), "strict": pol.StrictTable()},
		"asServer": map[string]any{
			"supported":    mcpserver.Supported,
			"legacy":       mcpserver.LegacySupported(),
			"modern":       []string{mcpserver.ModernLatest},
			"latest":       mcpserver.Latest,
			"oldest":       mcpserver.Oldest,
			"features":     mcpserver.FeatureMatrix(),
			"nativeElicit": s.set.Bool("proto.native"),
		},
		"asClient": map[string]any{
			"legacy":  mcpclient.ProtocolVersion,
			"modern":  mcpclient.ModernVersions,
			"servers": upstream,
		},
	})
}

// ---- the routes `mcpx serve --transport http` used to own ----
//
// They were served by a second process, on a second HTTP server, under a
// second /v1 prefix. Whichever one a caller reached decided which half of
// the API existed, and the two could not be told apart from the outside.
// They live here now, declared in the same table as everything else.

// handleToolInvoke runs one of mcpx's own MCP tools as a plain POST.
//
// Most of them have a /v1 route of their own, because the tools are
// generated from that table. The ones that do not are the interesting case:
// an adapted command-line program, an operation from a declared OpenAPI
// document, anything contributed from outside the fixed set. Without this
// they are reachable from an MCP host and from nowhere else.
func (s *Server) handleToolInvoke(w http.ResponseWriter, r *http.Request) {
	if s.MCPTool == nil {
		writeErr(w, http.StatusNotFound, errors.New("this daemon serves no MCP tools"))
		return
	}
	body, err := readBody(w, r, defaults.HTTPCallBodyLimit)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	text, err := s.MCPTool(r.Context(), r.PathValue("tool"), body)
	var failed ToolFailure
	if errors.As(err, &failed) {
		// The tool ran and said it failed: the same 200-with-a-flag /v1/call
		// gives, not the 400 reserved for a request mcpx could not carry out.
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "result": failed.Text})
		return
	}
	if err != nil {
		// The status the daemon gave the call behind the tool, when there
		// was one: a tool that reached an upstream server which failed is
		// a 502 here as on /v1/call, not the 400 of a malformed request.
		status := http.StatusBadRequest
		var carried interface{ HTTPStatus() int }
		if errors.As(err, &carried) && carried.HTTPStatus() >= 400 {
			status = carried.HTTPStatus()
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": text})
}

// handleCallPath is /v1/call with the pair in the URL.
//
// The same call, spelled the way a shell script wants to spell it: one path
// per tool, arguments as the whole body, nothing to assemble. It is what the
// generated OpenAPI document describes, so a client built from that document
// has somewhere to send its request.
func (s *Server) handleCallPath(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r, defaults.HTTPCallBodyLimit)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var args any = map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &args); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("args: %w", err))
			return
		}
	}
	cc := callContext(r, config.CallContext{}, r.URL.Query().Get("session"))
	start := time.Now()
	res, err := s.reg.Call(withRun(withCallSettings(r.Context(), s.callSettings(r)), r.Header.Get("X-Mcpx-Run")), r.PathValue("server"), r.PathValue("tool"), cc, args)
	if err != nil {
		writeJSON(w, failureStatus(err), map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": !resultFailed(res), "result": res,
		"durationMs": time.Since(start).Milliseconds()})
}

// ToolFailure is a tool that ran and returned isError. It travels as an error
// so the MCP server's existing path sets isError, and is typed so /v1/tools
// can report it as the tool's failure rather than as a bad request.
type ToolFailure struct{ Text string }

func (f ToolFailure) Error() string { return f.Text }

// resultFailed reports a CallToolResult's isError. /v1/call/{s}/{t} said
// ok:true beside isError:true, which is two answers to one question.
func resultFailed(raw json.RawMessage) bool {
	var r struct {
		IsError bool `json:"isError"`
	}
	return json.Unmarshal(raw, &r) == nil && r.IsError
}

// readBody reads a request body, defaulting an empty one to an empty object
// so that a caller with no arguments need not send `{}` by hand.
func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return []byte("{}"), nil
	}
	return b, nil
}

// upstreamHooks sets how pools connect to their servers: the era preference
// for a server that names none, the probe timeout, and the era cache. Read
// once, when hooks are installed, because a pool's era is settled per start
// and the cache file is opened once.
func (r *Registry) upstreamHooks(h *pool.Hooks) {
	h.Protocol = mcpclient.Preference(defaults.UpstreamProtocol)
	h.ProbeTimeout = defaults.UpstreamProbeTimeout
	cache := defaults.UpstreamEraCache
	if r.set != nil {
		h.Protocol = mcpclient.Preference(r.set.String("upstream.protocol"))
		h.ProbeTimeout = r.set.Duration("upstream.probeTimeout")
		cache = r.set.Bool("upstream.eraCache")
	}
	if cache && r.paths.State != "" {
		h.Eras = pool.OpenEraFile(filepath.Join(r.paths.State, defaults.UpstreamEraFile))
	}
}

// ---- resource subscriptions ----
//
// A /v1/events stream that names resources is also a subscription to them
// upstream, held exactly as long as the stream is open. Tying the two
// together is what makes the reference count honest: a client that crashes
// or disconnects releases its subscriptions without having to say so, and
// one that reconnects -- to a restarted daemon, say -- subscribes again by
// reconnecting. `mcpx serve` is such a client for everything its own MCP
// clients subscribe to.

// resourceWatch is what one event stream subscribed.
type resourceWatch struct {
	// Watching are the URIs, as the stream named them, whose updates will
	// be delivered; Refused says why each other named one will not.
	Watching []string          `json:"watching"`
	Refused  map[string]string `json:"refused,omitempty"`
	releases []func()
}

func (w *resourceWatch) release() {
	for _, r := range w.releases {
		r()
	}
}

// announce tells the stream what it subscribed, before any event. A client
// that must say what it agreed to -- a subscriptions/listen
// acknowledgement, a resources/subscribe reply -- waits for this.
func (w *resourceWatch) announce(out io.Writer) {
	data, _ := json.Marshal(w)
	b, _ := json.Marshal(events.Event{Kind: events.ResourceWatching, At: time.Now(), Data: data})
	fmt.Fprintf(out, "event: %s\ndata: %s\n\n", events.ResourceWatching, b)
}

// watchResources subscribes upstream to the resources a stream names and
// returns the URIs to match its events against.
//
// A URI of the form mcpx://<namespace>/<uri> -- how mcpx's own listings
// name resources -- identifies its server. A bare URI does with server=,
// and without it names no server and is matched but not subscribed.
func (s *Server) watchResources(ctx context.Context, server string, uris []string) ([]string, *resourceWatch) {
	w := &resourceWatch{Watching: []string{}}
	match := make([]string, 0, len(uris))
	for _, u := range uris {
		owner, upstream := server, u
		if rest, ok := strings.CutPrefix(u, "mcpx://"); ok {
			if ns, inner, ok := strings.Cut(rest, "/"); ok && ns != "" {
				owner, upstream = ns, inner
			}
		}
		if owner == "" {
			match = append(match, u)
			s.warnUnwatched(owner, u, "no configured server owns it; name it mcpx://<server>/<uri>")
			continue
		}
		p, ok := s.reg.Pool(owner)
		if !ok {
			w.refuse(u, UnknownServer{Name: owner}.Error())
			s.warnUnwatched(owner, u, w.Refused[u])
			continue
		}
		upstream = upstreamResourceURI(ctx, p, upstream)
		match = append(match, upstream)
		release, err := p.Watch(ctx, s.reg.keyFor(p, config.CallContext{}), upstream)
		if err != nil {
			w.refuse(u, err.Error())
			s.warnUnwatched(owner, u, err.Error())
			continue
		}
		w.Watching = append(w.Watching, u)
		w.releases = append(w.releases, release)
	}
	return match, w
}

// warnUnwatched records, as a warning on the bus, a resource a stream named
// that will get no updates. A legacy resources/subscribe for it still
// succeeds (#251: the legacy revisions have no way to say "agreed, but
// nothing will come"), so this record is the only place the reason survives.
func (s *Server) warnUnwatched(server, uri, reason string) {
	s.reg.publish(events.Event{Kind: events.ServerLog, Server: server, URI: uri,
		Data: mustJSON(mcpclient.ServerMessage{Level: "warning", Logger: "mcpx",
			Data: mustJSON("no updates will be delivered for " + uri + ": " + reason)})})
}

func (w *resourceWatch) refuse(uri, reason string) {
	if w.Refused == nil {
		w.Refused = map[string]string{}
	}
	w.Refused[uri] = reason
}

// upstreamResourceURI undoes the namespacing of a listed resource. The
// listing drops a leading "/" so that mcpx://ns//abs does not appear, which
// leaves "abs" ambiguous between a relative URI and the absolute "/abs"; the
// server's own list says which it published.
func upstreamResourceURI(ctx context.Context, p *pool.Pool, inner string) string {
	_, resources, err := p.Schemas(ctx)
	if err != nil {
		return inner
	}
	for _, r := range resources {
		if r.URI == inner {
			return inner
		}
	}
	for _, r := range resources {
		if strings.TrimPrefix(r.URI, "/") == inner {
			return r.URI
		}
	}
	return inner
}

// askCallKey carries the id of the interruptible call a context belongs to.
type askCallKey struct{}

func withAskCall(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, askCallKey{}, id)
}
