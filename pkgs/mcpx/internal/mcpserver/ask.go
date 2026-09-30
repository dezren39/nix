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
	// IsError marks a tool that failed, which is a result rather than a
	// protocol error: a client that retries the wrong thing on a tool
	// failure never converges.
	IsError bool
	// Questions are what the call is waiting on, oldest first.
	Questions []Question
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

	var callID string
	if state, answers, resuming := resumeOf(req.Params); resuming {
		id, err := s.states().verify(state, requestBinding(req))
		if err != nil {
			return fail(codeInvalidParams, err.Error())
		}
		callID = id
		if len(answers) > 0 {
			if err := s.Ask.Reply(ctx, callID, answers); err != nil {
				return fail(codeInvalidParams, err.Error())
			}
		}
	} else {
		id, err := s.Ask.Begin(ctx, req.Method, forAsk(req.Params))
		if errors.Is(err, ErrNotInterruptible) {
			return nil
		}
		if errors.Is(err, ErrInvalidParams) {
			return fail(codeInvalidParams, err.Error())
		}
		if err != nil {
			return fail(codeInternal, err.Error())
		}
		callID = id
	}

	tm := s.Timing.resolved()
	deadline := time.Now().Add(tm.AskTimeout)
	rounds := 0
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
		if out.Done {
			if out.IsError && req.Method != "tools/call" {
				// Only a tool has a result that can say it failed. A read or
				// a prompt that failed upstream is an error, and was being
				// returned as contents whose text was the error message.
				return fail(codeInternal, out.Text)
			}
			return reply(askResult(req, out))
		}

		sendable := sendableTo(out.Questions, peer)
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
		if rounds++; rounds > tm.AskRounds {
			s.Ask.Abandon(callID)
			return fail(codeInternal, fmt.Sprintf(
				"%s was still asking for input after %d rounds", req.Method, tm.AskRounds))
		}

		if peer.Modern {
			state, err := s.states().mint(callID, requestBinding(req))
			if err != nil {
				// No verifiable state means no safe resume, so the question
				// goes back to the broker rather than out on a token
				// anybody could replay.
				continue
			}
			return reply(inputRequired(sendable, state, peer))
		}

		answers := map[string]json.RawMessage{}
		for _, q := range sendable {
			raw, aerr := c.askClient(ctx, peer, q)
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
		if len(answers) > 0 {
			if err := s.Ask.Reply(ctx, callID, answers); err != nil {
				return fail(codeInternal, err.Error())
			}
		}
	}
	s.Ask.Abandon(callID)
	return fail(codeInternal, req.Method+": the call did not finish before mcpx stopped waiting for it")
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
		requests[q.ID] = map[string]any{"method": q.Method, "params": params}
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
		res := map[string]any{"content": []any{
			map[string]any{"type": "text", "text": out.Text}}}
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
