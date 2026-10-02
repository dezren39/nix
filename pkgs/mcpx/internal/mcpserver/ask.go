package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Asker runs a request that an upstream server may interrupt with a
// question.
//
// It exists because the upstream call has to outlive the client request that
// started it. A modern client answers a question by sending the *same*
// request again, which means the original one has already been answered and
// returned; if the upstream call were tied to it, there would be nothing
// left to resume. So the call runs as a daemon task and this is the handle
// to it.
//
// Declared here as an interface for the same reason Backend is: the protocol
// package does not know about the daemon, and a test can drive it with a
// fake that asks whatever it likes.
type Asker interface {
	// Begin starts a call and returns its identifier. kind is the MCP
	// method being performed -- tools/call, prompts/get, resources/read.
	Begin(ctx context.Context, kind string, params json.RawMessage) (string, error)
	// Poll waits up to wait for the call to finish or to raise questions.
	Poll(ctx context.Context, callID string, wait time.Duration) (Outcome, error)
	// Reply answers questions the call raised. The values are the MCP
	// result objects -- ElicitResult, CreateMessageResult -- exactly as the
	// client produced them.
	Reply(ctx context.Context, callID string, answers map[string]json.RawMessage) error
	// Abandon stops a call nobody is going to come back for.
	Abandon(callID string)
}

// ErrNotInterruptible means this request has nothing an upstream server
// could interrupt -- a tool that reaches no server, a script, a /v1
// operation. The caller runs it the ordinary way instead of paying for a
// task and a poll loop to discover it finished immediately.
var ErrNotInterruptible = errors.New("this request cannot be interrupted")

// Outcome is where a call has got to.
type Outcome struct {
	// Done means the call finished, one way or another.
	Done bool
	// Text is the result, rendered the way every other mcpx tool result is.
	Text string
	// Contents is a finished resource read, in place of Text.
	Contents []ResourceContents
	// Err classifies a failed prompts/get or resources/read, the way a
	// Backend does: wrapping ErrResourceNotFound or ErrInvalidParams when
	// the upstream server said so. Text still carries the message.
	Err error
	// IsError marks a tool that failed, which is a result rather than a
	// protocol error: a client that retries the wrong thing on a tool
	// failure never converges.
	IsError bool
	// Questions are what the call is waiting on, oldest first.
	Questions []Question
	// Notifications are what the upstream sent for the client since the
	// last poll -- progress and log messages, already in the client's
	// terms (see CallRelay) -- to be delivered before anything else.
	Notifications []Notification
	// Upstream is set when a pass-through tool's upstream answered with a
	// JSON-RPC error, which is relayed as that error; see UpstreamError.
	Upstream *UpstreamError
}

// Notification is one MCP notification to pass on to the client.
type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// canAsk reports whether this request should go through the Asker at all.
//
// Only for a client that declared it can answer something. A client that
// declared neither elicitation nor sampling gets the direct path and the
// broker's own routing, unchanged -- which is the behaviour that works
// today and must keep working, because most MCP hosts implement neither.
func (s *Server) canAsk(ctx context.Context, c *Conn, p Peer) bool {
	if s.Ask == nil || !p.AnswersInline() {
		return false
	}
	// A modern client is never sent anything: it is handed the question
	// inside a result and retries. A legacy one has to be reachable, and on
	// Streamable HTTP that is a property of the exchange rather than of the
	// connection -- the frame goes out on the response stream of the
	// request that is waiting for it.
	return p.Modern || senderFrom(ctx) != nil || c.canPush()
}

// resumeOf reads the two fields a modern client sends when answering.
func resumeOf(params json.RawMessage) (state string, answers map[string]json.RawMessage, ok bool) {
	var p struct {
		RequestState   string                     `json:"requestState"`
		InputResponses map[string]json.RawMessage `json:"inputResponses"`
	}
	if json.Unmarshal(params, &p) != nil || p.RequestState == "" {
		return "", nil, false
	}
	return p.RequestState, p.InputResponses, true
}

// inputResponsesOf reads a request's inputResponses, whether or not it
// carries a requestState.
func inputResponsesOf(params json.RawMessage) map[string]json.RawMessage {
	var p struct {
		InputResponses map[string]json.RawMessage `json:"inputResponses"`
	}
	_ = json.Unmarshal(params, &p)
	return p.InputResponses
}

// forAsk strips the protocol's own fields, leaving what the method means.
//
// _meta, inputResponses and requestState are how the request travelled, not
// what it asked for, and passing them to a backend that does not know them
// is how an unknown-argument error turns up three layers down.
func forAsk(params json.RawMessage) json.RawMessage {
	m := map[string]json.RawMessage{}
	if json.Unmarshal(params, &m) != nil {
		return params
	}
	delete(m, "_meta")
	delete(m, "inputResponses")
	delete(m, "requestState")
	b, err := json.Marshal(m)
	if err != nil {
		return params
	}
	return b
}

// viaAsk answers a request that may be interrupted by a question.
//
// Returns nil when the request turns out not to be interruptible at all, so
// the caller falls through to the ordinary path. That is the common case --
// most of mcpx's own tools reach no upstream server -- and paying for a task
// and a poll loop to discover it would be a cost on every call.
//
// Both eras run through here, and the difference is only how the question
// travels: a legacy client is sent elicitation/create on the wire while its
// own call is still open, a modern one is handed the question inside an
// input_required result and sends the whole request again. The call itself
// does not know which happened.
func (s *Server) viaAsk(ctx context.Context, c *Conn, req request, peer Peer) *response {
	reply := func(result any) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	fail := func(code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}

	callID, upfront, rounds, failed := s.beginAsk(ctx, req, peer)
	if failed != nil || callID == "" {
		return failed
	}

	tm := s.Timing.resolved()
	deadline := time.Now().Add(tm.AskTimeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			// The client gave up, or the transport did. The call itself
			// keeps running as a task, and its questions keep their
			// deadlines; abandoning it here would throw away work somebody
			// may still collect from /v1/tasks.
			return fail(codeInternal, req.Method+": "+err.Error())
		}
		wait := tm.AskPoll
		if left := time.Until(deadline); left < wait {
			wait = left
		}
		started := time.Now()
		out, err := s.Ask.Poll(ctx, callID, wait)
		if err != nil {
			return fail(codeInternal, err.Error())
		}
		relayNotes(ctx, out)
		if out.Done {
			return finishedAsk(req, out, peer, s.passCall(ctx, req))
		}

		sendable := sendableTo(out.Questions, peer)
		if len(sendable) == 0 && len(out.Notifications) > 0 {
			continue // Poll returned to deliver these; go straight back
		}
		if len(sendable) == 0 {
			// Either nothing was asked yet, or what was asked is something
			// this client cannot answer -- a url flow to a form-only
			// client, say. Either way the broker still holds it and its
			// default audience can answer, so waiting is right.
			//
			// Poll is meant to block for the whole interval; sleeping out
			// whatever it did not is what stops an implementation that
			// returns early turning this into a busy loop.
			if rest := wait - time.Since(started); rest > 0 {
				select {
				case <-ctx.Done():
				case <-time.After(rest):
				}
			}
			continue
		}
		if len(upfront) > 0 {
			fit := map[string]json.RawMessage{}
			for _, q := range sendable {
				if v, ok := upfront[wireKey(q, sendable)]; ok {
					fit[wireKey(q, sendable)] = v
				}
			}
			upfront = nil
			if len(fit) > 0 {
				if err := s.Ask.Reply(ctx, callID, answersByID(fit, sendable)); err != nil {
					return fail(codeInvalidParams, err.Error())
				}
				continue
			}
		}
		if rounds++; rounds > tm.AskRounds {
			s.Ask.Abandon(callID)
			return fail(codeInternal, fmt.Sprintf(
				"%s was still asking for input after %d rounds", req.Method, tm.AskRounds))
		}

		if peer.Modern {
			// An upstream that asks several things at once raises them
			// together but not in the same instant: the gateway answers
			// them concurrently and they arrive one at a time. Each
			// question says how many its round holds, so the wait ends the
			// moment they are all here rather than after a fixed pause --
			// which was too short on a loaded machine (the official
			// suite's sep-2322-multiple-inputs-incomplete, 2 of 3) and
			// wasted time on an idle one.
			if m, ok := s.awaitRound(ctx, callID, sendable, peer); ok {
				sendable = m
			}
			state, err := s.states().mintRound(callID, requestBinding(req), rounds)
			if err != nil {
				// No verifiable state means no safe resume, so the question
				// goes back to the broker rather than out on a token
				// anybody could replay.
				continue
			}
			return reply(inputRequired(sendable, state, peer))
		}

		answers := map[string]json.RawMessage{}
		// The question itself is bounded by the same deadline: a client
		// that never answers must not hold the call past AskTimeout, and
		// the expiry is what makes askClient cancel the request it sent.
		askCtx, cancelAsk := context.WithDeadline(ctx, deadline)
		for _, q := range sendable {
			raw, aerr := c.askClient(askCtx, peer, q)
			if aerr != nil {
				if errors.Is(aerr, ErrNoPush) {
					// This transport cannot carry a request to the client.
					// The broker keeps the question; stop trying to ask.
					break
				}
				continue
			}
			answers[q.ID] = raw
		}
		cancelAsk()
		if len(answers) > 0 {
			// The questions still open, to undo wireKey's renaming.
			if open, perr := s.Ask.Poll(ctx, callID, time.Millisecond); perr == nil {
				relayNotes(ctx, open)
				answers = answersByID(answers, sendableTo(open.Questions, peer))
			}
			if err := s.Ask.Reply(ctx, callID, answers); err != nil {
				return fail(codeInternal, err.Error())
			}
		}
	}
	s.Ask.Abandon(callID)
	return fail(codeInternal, req.Method+": the call did not finish before mcpx stopped waiting for it")
}

// relayNotes delivers what a poll collected to the client whose call it is.
func relayNotes(ctx context.Context, out Outcome) {
	r := RelayFrom(ctx)
	if r == nil || r.Notify == nil {
		return
	}
	for _, n := range out.Notifications {
		r.Notify(n.Method, n.Params)
	}
}

// beginAsk starts the call a request names through the Asker, or resumes the
// one its requestState names and hands it the answers attached. ("", nil)
// means the request is not interruptible and the caller runs it the ordinary
// way; a non-nil response is the error to answer with.
//
// upfront is the inputResponses a new request carried with nothing to resume:
// answers given before the question, held until the upstream asks. rounds is
// how many rounds the call has already asked, from a resumed requestState,
// so AskRounds bounds the whole exchange rather than each retry of it.
func (s *Server) beginAsk(ctx context.Context, req request, peer Peer) (id string, upfront map[string]json.RawMessage, rounds int, failed *response) {
	fail := func(code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}
	if state, answers, resuming := resumeOf(req.Params); resuming {
		id, rounds, err := s.states().verifyRound(state, requestBinding(req))
		if err != nil {
			return "", nil, 0, fail(codeInvalidParams, err.Error())
		}
		if len(answers) > 0 {
			// The questions still open, to undo wireKey's renaming.
			if open, perr := s.Ask.Poll(ctx, id, time.Millisecond); perr == nil {
				relayNotes(ctx, open)
				answers = answersByID(answers, sendableTo(open.Questions, peer))
			}
			if err := s.Ask.Reply(ctx, id, answers); err != nil {
				return "", nil, 0, fail(codeInvalidParams, err.Error())
			}
		}
		return id, nil, rounds, nil
	}
	id, err := s.Ask.Begin(ctx, req.Method, forAsk(req.Params))
	switch {
	case errors.Is(err, ErrNotInterruptible):
		return "", nil, 0, nil
	case errors.Is(err, ErrInvalidParams):
		return "", nil, 0, fail(codeInvalidParams, err.Error())
	case err != nil:
		return "", nil, 0, fail(codeInternal, err.Error())
	}
	return id, inputResponsesOf(req.Params), 0, nil
}

// finishedAsk is the answer to a request whose asked call has finished.
//
// pass says the request is a pass-through upstream's tools/call, the one case
// an upstream's JSON-RPC error is relayed as itself.
func finishedAsk(req request, out Outcome, peer Peer, pass bool) *response {
	fail := func(code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}
	// An upstream error carried whole -- -32021 with its requiredCapabilities
	// -- is relayed as itself on every path.
	if up := (*UpstreamError)(nil); errors.As(out.Err, &up) {
		return up.relay(req.ID)
	}
	if pass && out.Upstream != nil && req.Method == "tools/call" {
		return out.Upstream.relay(req.ID)
	}
	if out.IsError && req.Method != "tools/call" {
		// Only a tool has a result that can say it failed. A read or
		// a prompt that failed upstream is an error, and was being
		// returned as contents whose text was the error message --
		// which is how a read of a URI that exists nowhere "passed"
		// the official suite's resources-read-text.
		switch {
		case req.Method == "resources/read" && errors.Is(out.Err, ErrResourceNotFound):
			var p struct {
				URI string `json:"uri"`
			}
			_ = json.Unmarshal(req.Params, &p)
			return notFound(req.ID, p.URI, peer, errors.New(out.Text))
		case req.Method == "prompts/get" && errors.Is(out.Err, ErrInvalidParams):
			return fail(codeInvalidParams, out.Text)
		}
		return fail(codeInternal, out.Text)
	}
	return &response{JSONRPC: "2.0", ID: req.ID, Result: askResult(req, out)}
}

// passCall reports whether req is a tools/call of a pass-through tool.
func (s *Server) passCall(ctx context.Context, req request) bool {
	if req.Method != "tools/call" {
		return false
	}
	var p struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(req.Params, &p)
	ok, _ := s.isPassTool(ctx, p.Name)
	return ok
}

// sendableTo picks the questions this client may be sent.
//
// Never send a request type the client did not declare. A server that
// receives elicitation/create from a client that declared nothing has been
// lied to about capabilities, and the whole negotiation stops meaning
// anything.
func sendableTo(qs []Question, p Peer) []Question {
	var out []Question
	for _, q := range qs {
		if q.Sendable(p) {
			out = append(out, q)
		}
	}
	return out
}

// inputRequired is the modern era's way of asking.
//
// A modern server has no connection to send a request on, so it answers
// "not yet, first tell me these" and expects the same request again with the
// answers attached.
func inputRequired(qs []Question, state string, p Peer) map[string]any {
	requests := map[string]any{}
	for _, q := range qs {
		// Rendered for the client's revision exactly as a wire request
		// would be. It used to be the upstream server's params verbatim,
		// which carried a 2025-11-25 elicitationId to a 2026-07-28 client
		// whose schema has none.
		params, err := q.paramsFor(p)
		if err != nil {
			params = q.Params
		}
		requests[wireKey(q, qs)] = map[string]any{"method": q.Method, "params": params}
	}
	return map[string]any{
		"resultType":    "input_required",
		"inputRequests": requests,
		"requestState":  state,
	}
}

// askResult renders a finished call as the method's own result shape.
func askResult(req request, out Outcome) map[string]any {
	switch req.Method {
	case "prompts/get":
		if raw, ok := decodeRaw(out.Text); ok {
			return raw
		}
		return map[string]any{"messages": []any{map[string]any{
			"role":    "user",
			"content": map[string]any{"type": "text", "text": out.Text},
		}}}
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(req.Params, &p)
		return readResult(p.URI, out.Contents)
	default:
		// Decoded as the direct path decodes it, so an mcpx_exec answered
		// inline keeps its resource_link blocks.
		if raw, ok := decodeRaw(out.Text); ok {
			return raw
		}
		text, blocks := decodeResult(out.Text)
		content := []any{map[string]any{"type": "text", "text": text}}
		for _, b := range blocks {
			content = append(content, b)
		}
		res := map[string]any{"content": content}
		if out.IsError {
			res["isError"] = true
		}
		return res
	}
}

// requestBinding is what a requestState is tied to: the request itself.
//
// The MRTR page asks a server to put, inside the integrity-protected state,
// an identifier for the originating request -- the method and a digest of
// its salient parameters -- and the authenticated principal, and to reject
// state presented on a request that does not match. It used to be bound to
// an Mcp-Session-Id instead, which 2026-07-28 does not have: mcpx minted one
// on server/discover just so there was something to bind to, and a client
// that skipped discover could never resume at all.
//
// The salient parameters are everything except how the request travelled
// (_meta) and the two fields a retry adds (inputResponses, requestState).
// Re-marshalling through a generic value sorts every object's keys, so the digest does not
// depend on the order a client happened to write them in. mcpx has no
// authenticated principal of its own to add: whoever can reach its socket
// or port is, as far as mcpx can tell, the same caller.
func requestBinding(req request) string {
	var v any
	canonical := forAsk(req.Params)
	if json.Unmarshal(canonical, &v) == nil {
		if b, err := json.Marshal(v); err == nil {
			canonical = b
		}
	}
	sum := sha256.Sum256(canonical)
	return req.Method + ":" + hex.EncodeToString(sum[:])
}

// states returns the signer, built once per server.
func (s *Server) states() *stateSigner {
	s.stateOnce.Do(func() {
		s.signer = newStateSigner()
		s.signer.ttl = s.Timing.resolved().StateTTL
	})
	return s.signer
}

// mayBlockOnClient reports whether answering this request could require a
// frame from the client, which decides whether it can be handled in line.
func (s *Server) mayBlockOnClient(c *Conn, req request) bool {
	switch req.Method {
	case "tools/call", "prompts/get", "resources/read":
	default:
		return false
	}
	return s.canAsk(context.Background(), c, c.peerFor(req.Params))
}

// wireKey is the inputRequests key q is sent under: the upstream's own key
// where it has one no other pending question shares, otherwise mcpx's id.
func wireKey(q Question, qs []Question) string {
	if q.Key == "" {
		return q.ID
	}
	for _, o := range qs {
		if o.ID != q.ID && (o.Key == q.Key || o.ID == q.Key) {
			return q.ID
		}
	}
	return q.Key
}

// answersByID maps a client's inputResponses, keyed as wireKey sent them,
// back to the question ids the asker knows. A key it does not recognise is
// passed through unchanged, for the asker to judge.
func answersByID(answers map[string]json.RawMessage, qs []Question) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(answers))
	for k, v := range answers {
		out[k] = v
	}
	for _, q := range qs {
		k := wireKey(q, qs)
		if k == q.ID {
			continue
		}
		if v, ok := answers[k]; ok {
			delete(out, k)
			out[q.ID] = v
		}
	}
	return out
}

// awaitRound waits for the rest of a round's questions and returns them.
//
// have is what is already open. The questions name the round's size, so this
// returns as soon as they are all here; a round that never completes -- an
// upstream whose later question fails, or a size that never arrives -- costs
// askRoundWait once. ok is false when nothing more came.
func (s *Server) awaitRound(ctx context.Context, callID string, have []Question, peer Peer) ([]Question, bool) {
	want := 0
	for _, q := range have {
		if q.Round > want {
			want = q.Round
		}
	}
	if want > 0 && len(have) >= want {
		return nil, false // already complete
	}
	// Without a round size there is nothing to wait for, so the wait is the
	// short grace that lets a sibling question raised in the same instant
	// land. A known size extends it: those questions are known to be coming.
	deadline := time.Now().Add(askSettle)
	if want > 0 {
		deadline = time.Now().Add(askRoundWait)
	}
	best, found := have, false
	for {
		out, err := s.Ask.Poll(ctx, callID, askRoundPoll)
		if err != nil {
			return best, found
		}
		relayNotes(ctx, out)
		if out.Done {
			return best, found
		}
		if m := sendableTo(out.Questions, peer); len(m) > len(best) {
			best, found = m, true
			for _, q := range m {
				if q.Round > want {
					want = q.Round
					deadline = time.Now().Add(askRoundWait)
				}
			}
		}
		if want > 0 && len(best) >= want {
			return best, found
		}
		if time.Now().After(deadline) {
			return best, found
		}
		select {
		case <-ctx.Done():
			return best, found
		case <-time.After(askRoundPoll):
		}
	}
}

// askRoundWait bounds the wait for a round that never completes, and
// askRoundPoll is how often it looks; see awaitRound.
const (
	askRoundWait = 5 * time.Second
	askRoundPoll = 5 * time.Millisecond
	// askSettle is the wait when no question names a round size: a legacy
	// question, or one raised as a request of its own. Long enough for a
	// sibling raised in the same instant, short enough that a call with one
	// question is not held up.
	askSettle = 50 * time.Millisecond
)
