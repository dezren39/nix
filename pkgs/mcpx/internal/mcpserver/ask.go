package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
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

// Outcome is where a call has got to.
type Outcome struct {
	// Done means the call finished, one way or another.
	Done bool
	// Text is the result, rendered the way every other mcpx tool result is.
	Text string
	// MimeType applies to a resource read.
	MimeType string
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
func (s *Server) canAsk(c *Conn, p Peer) bool {
	return s.Ask != nil && p.AnswersInline() && (p.Modern || c.canPush())
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
		id, err := s.states().verify(state, c.binding())
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
		if err != nil {
			return fail(codeInternal, err.Error())
		}
		callID = id
	}

	deadline := time.Now().Add(defaults.ProtoAskTimeout)
	rounds := 0
	for time.Now().Before(deadline) {
		wait := defaults.ProtoAskPoll
		if left := time.Until(deadline); left < wait {
			wait = left
		}
		out, err := s.Ask.Poll(ctx, callID, wait)
		if err != nil {
			return fail(codeInternal, err.Error())
		}
		if out.Done {
			return reply(askResult(req, out))
		}

		sendable := sendableTo(out.Questions, peer)
		if len(sendable) == 0 {
			// Either nothing was asked yet, or what was asked is something
			// this client cannot answer -- a url flow to a form-only
			// client, say. Either way the broker still holds it and its
			// default audience can answer, so waiting is right.
			continue
		}
		if rounds++; rounds > defaults.ProtoAskRounds {
			s.Ask.Abandon(callID)
			return fail(codeInternal, fmt.Sprintf(
				"%s was still asking for input after %d rounds", req.Method, defaults.ProtoAskRounds))
		}

		if peer.Modern {
			state, err := s.states().mint(callID, c.binding())
			if err != nil {
				// No verifiable state means no safe resume, so the question
				// goes back to the broker rather than out on a token
				// anybody could replay.
				continue
			}
			return reply(inputRequired(sendable, state))
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
func inputRequired(qs []Question, state string) map[string]any {
	requests := map[string]any{}
	for _, q := range qs {
		requests[q.ID] = map[string]any{"method": q.Method, "params": json.RawMessage(q.Params)}
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
		mime := out.MimeType
		if mime == "" {
			mime = "text/plain"
		}
		return map[string]any{"contents": []any{map[string]any{
			"uri": p.URI, "mimeType": mime, "text": out.Text,
		}}}
	default:
		res := map[string]any{"content": []any{
			map[string]any{"type": "text", "text": out.Text}}}
		if out.IsError {
			res["isError"] = true
		}
		return res
	}
}

// binding is the identity a requestState is tied to.
//
// Empty means this connection has no identity that survives the request, so
// no resumable state can be issued for it: a token bound to nothing is a
// token anyone may present.
func (c *Conn) binding() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bind
}

// states returns the signer, built once per server.
func (s *Server) states() *stateSigner {
	s.stateOnce.Do(func() { s.signer = newStateSigner() })
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
	return s.canAsk(c, c.peerFor(req.Params))
}
