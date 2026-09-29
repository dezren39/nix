package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/elicit"
	"github.com/dezren39/mcpx/internal/events"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
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
type askTable struct {
	mu    sync.Mutex
	byID  map[string]*askCall
	byKey map[string][]*askCall
}

func newAskTable() *askTable {
	return &askTable{byID: map[string]*askCall{}, byKey: map[string][]*askCall{}}
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

func (t *askTable) end(id string) {
	t.mu.Lock()
	a := t.byID[id]
	delete(t.byID, id)
	if a != nil {
		k := askKey(a.Server, a.Key)
		kept := t.byKey[k][:0]
		for _, other := range t.byKey[k] {
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
	t.mu.Unlock()
}

func (t *askTable) get(id string) (*askCall, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	a, ok := t.byID[id]
	return a, ok
}

// forKey returns the one call a question on this connection belongs to.
func (t *askTable) forKey(server, key string) (*askCall, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.byKey[askKey(server, key)]
	if len(list) != 1 {
		return nil, false
	}
	return list[0], true
}

// attach records a question against the call that provoked it, if one can be
// identified, and returns whether it was.
func (r *Registry) attach(server, key string, req elicit.Request, method string, params json.RawMessage) (string, bool) {
	a, ok := r.asks.forKey(server, key)
	if !ok {
		return "", false
	}
	a.add(mcpserver.Question{
		ID: req.ID, Method: method, Params: params,
		Mode: string(req.Mode), Server: server,
	})
	return a.ID, true
}

// CallAsk runs an interruptible call and registers it for correlation.
func (r *Registry) CallAsk(ctx context.Context, id, server, tool string, cc config.CallContext, args any) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, fmt.Errorf("unknown server or namespace %q", server)
	}
	key := r.keyFor(p, cc)
	r.beginAsk(id, server, key, cc.SessionID)
	defer r.endAsk(id)
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
	return p.ReadResource(ctx, key, uri)
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
	return p.GetPrompt(ctx, key, name, args)
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
}

func (s *Server) handleAskBegin(w http.ResponseWriter, r *http.Request) {
	var req askReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Server == "" {
		writeErr(w, http.StatusBadRequest, errors.New("server is required"))
		return
	}
	switch req.Kind {
	case "tools/call", "prompts/get", "resources/read":
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"kind is tools/call, prompts/get or resources/read, not %q", req.Kind))
		return
	}
	cc := callContext(r, req.Context, req.Session)
	ttl := req.TTL
	if ttl <= 0 {
		ttl = int64(defaults.ProtoAskTTL / time.Millisecond)
	}

	// The task id is the call id, and the call has to be registered under it
	// before the upstream request goes out -- a server can ask its question
	// in the first millisecond. Start hands the id back on this channel, and
	// the body waits for it, so there is no window in which a question
	// arrives for a call nothing has heard of.
	ready := make(chan string, 1)
	t := s.taskStore().Start(ttl, func(ctx context.Context) (any, *tasks.Fault) {
		id := <-ready
		start := time.Now()
		var (
			raw json.RawMessage
			err error
		)
		switch req.Kind {
		case "tools/call":
			var args any = map[string]any{}
			if len(req.Args) > 0 {
				if uerr := json.Unmarshal(req.Args, &args); uerr != nil {
					return nil, &tasks.Fault{Code: http.StatusBadRequest, Message: "args: " + uerr.Error()}
				}
			}
			raw, err = s.reg.CallAsk(ctx, id, req.Server, req.Tool, cc, args)
		case "prompts/get":
			raw, err = s.reg.GetPromptAsk(ctx, id, req.Server, req.Name, req.Arguments, cc)
		case "resources/read":
			raw, err = s.reg.ReadResourceAsk(ctx, id, req.Server, req.URI, cc)
		}
		if err != nil {
			return nil, &tasks.Fault{Code: http.StatusBadGateway, Message: err.Error()}
		}
		return map[string]any{"result": raw, "kind": req.Kind,
			"durationMs": time.Since(start).Milliseconds()}, nil
	})
	ready <- t.TaskID

	writeJSON(w, http.StatusAccepted, map[string]any{"callId": t.TaskID, "task": t})
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

	call, live := s.reg.Asks().get(id)
	if !tasks.Terminal(t.Status) && live {
		before, changed := call.snapshot()
		// Wait when there is nothing to answer -- which is not the same as
		// nothing having been asked. After a question is answered the call
		// keeps running, and returning immediately because it once asked
		// something would turn the caller's long poll into a spin.
		if len(s.openQuestions(before)) == 0 {
			ctx, cancel := context.WithTimeout(r.Context(), wait)
			done := s.taskDone(ctx, id)
			select {
			case <-changed:
			case <-done:
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
		default:
			out["result"] = result
		}
		out["done"] = true
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body); err != nil {
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
	if method == "sampling/createMessage" {
		// Sampling has no decline shape in the specification, so anything
		// that arrives is an acceptance and a refusal has to be an error.
		if len(raw) == 0 || string(raw) == "null" {
			return elicit.Answer{}, errors.New("a sampling answer must be a CreateMessageResult")
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
		Server     string   `json:"server"`
		Namespace  string   `json:"namespace"`
		Preference string   `json:"preference,omitempty"`
		Era        string   `json:"era,omitempty"`
		Negotiated string   `json:"negotiated,omitempty"`
		Declared   []string `json:"declared,omitempty"`
	}
	var upstream []serverRow
	for _, name := range s.reg.Names() {
		p, ok := s.reg.Pool(name)
		if !ok {
			continue
		}
		era, negotiated := p.Era()
		row := serverRow{Server: name, Namespace: p.Namespace(),
			Preference: p.Config().Protocol, Era: string(era), Negotiated: negotiated}
		for cap := range p.Capabilities() {
			row.Declared = append(row.Declared, cap)
		}
		sort.Strings(row.Declared)
		upstream = append(upstream, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asServer": map[string]any{
			"supported":    mcpserver.Supported,
			"legacy":       mcpserver.LegacySupported(),
			"modern":       []string{mcpserver.ModernLatest},
			"latest":       mcpserver.Latest,
			"oldest":       mcpserver.Oldest,
			"features":     mcpserver.FeatureMatrix(),
			"nativeElicit": defaults.ProtoNative,
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
	body, err := readBody(w, r, 64<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	text, err := s.MCPTool(r.Context(), r.PathValue("tool"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
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
	body, err := readBody(w, r, 64<<20)
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
	res, err := s.reg.Call(r.Context(), r.PathValue("server"), r.PathValue("tool"), cc, args)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res,
		"durationMs": time.Since(start).Milliseconds()})
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
