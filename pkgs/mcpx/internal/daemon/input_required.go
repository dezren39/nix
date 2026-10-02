package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/elicit"
	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/tasks"
)

// InputHeader is how a caller of /v1/call says it cannot answer a question
// mid-call and wants to be told about one instead of waiting.
//
// `mcpx call`, and the scripts `mcpx exec` and `mcpx run` start, have nobody
// to answer an elicitation: the terminal is not theirs to prompt on, and an
// agent driving them is blocked on the very call that is waiting. Before
// this the call held for elicit.ttl, the server was told "cancel", and the
// command exited 0 with whatever the server made of a cancellation. With it
// the call keeps running as a task, the question stays answerable, and the
// caller gets an InputRequired document at once (#286).
const InputHeader = "X-Mcpx-Input"

// InputReport is the one value InputHeader takes.
const InputReport = "report"

// InputRequired is what /v1/call answers, with status 202, when the call it
// is running stopped to ask something.
type InputRequired struct {
	// CallID is the task the call continues as; `mcpx task result <id>`
	// collects it once the question is answered.
	CallID    string          `json:"callId"`
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Questions []InputQuestion `json:"questions"`
	// Text is the whole explanation, rendered once here so that every caller
	// -- the CLI, and the generated client inside a script -- prints the
	// same thing.
	Text string `json:"text"`
	// Ambiguous is set when the server's instance was shared by other calls
	// at the time, so the question could not be tied to this one with
	// certainty. Without it a second call made while the first was waiting
	// held for elicit.ttl and exited 0.
	Ambiguous bool `json:"ambiguous,omitempty"`
}

// InputQuestion is one open question, with how to answer it.
type InputQuestion struct {
	ID              string          `json:"id"`
	Method          string          `json:"method"`
	Mode            string          `json:"mode,omitempty"`
	Server          string          `json:"server,omitempty"`
	Message         string          `json:"message,omitempty"`
	URL             string          `json:"url,omitempty"`
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	ExpiresAt       time.Time       `json:"expiresAt,omitzero"`
	RespondWith     string          `json:"respondWith"`
}

// CallReporting is Call for a caller that cannot answer: the call registers
// under id, so a question the server raises is attributed to it and can be
// reported rather than waited on.
func (r *Registry) CallReporting(ctx context.Context, id, server, tool string, cc config.CallContext, args any) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, UnknownServer{Name: server}
	}
	key, err := r.resolveAndGuard(ctx, p, server, tool, cc)
	if err != nil {
		return nil, err
	}
	r.beginAsk(id, server, key, cc.SessionID)
	defer r.endAsk(id)
	res, err := p.Call(ctx, key, tool, args)
	if err != nil {
		return nil, r.explainCall(ctx, server, tool, args, err)
	}
	return res, nil
}

// handleCallReporting runs a /v1/call whose caller asked to be told about
// questions. The call runs as a task so that it outlives this request: the
// caller goes away to get an answer, and the server is still waiting for it.
func (s *Server) handleCallReporting(w http.ResponseWriter, r *http.Request, req callReq, cc config.CallContext, args any) {
	type outcome struct {
		res json.RawMessage
		err error
	}
	done := make(chan outcome, 1)
	ready := make(chan string, 1)
	start := time.Now()
	run := r.Header.Get("X-Mcpx-Run")
	t := s.taskStore().Start(s.set.Duration("proto.askTTL").Milliseconds(),
		func(ctx context.Context) (any, *tasks.Fault) {
			id := <-ready
			res, err := s.reg.CallReporting(withRun(ctx, run), id, req.Server, req.Tool, cc, args)
			done <- outcome{res, err}
			if err != nil {
				return nil, &tasks.Fault{Code: failureStatus(err), Message: err.Error()}
			}
			return map[string]any{"result": res, "kind": "tools/call", "server": req.Server,
				"durationMs": time.Since(start).Milliseconds()}, nil
		})
	ready <- t.TaskID

	// Polled rather than signalled: the call registers itself only once its
	// instance key is known, after this loop has started, and a short tick
	// is simpler than a second rendezvous for that.
	tick := time.NewTicker(defaults.ElicitPollInterval)
	defer tick.Stop()
	for {
		select {
		case o := <-done:
			dur := time.Since(start).Truncate(time.Millisecond)
			if o.err != nil {
				s.logger.Printf("call %s.%s failed in %s: %v", req.Server, req.Tool, dur, o.err)
				writeJSON(w, failureStatus(o.err), callErrorBody(o.err))
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"result": o.res, "durationMs": dur.Milliseconds()})
			return
		case <-r.Context().Done():
			// The caller left before anything was asked; nobody will
			// collect this call, so it is stopped as an ordinary call is.
			s.taskStore().Cancel(t.TaskID)
			return
		case <-tick.C:
			var open []mcpserver.Question
			if call, live := s.reg.Asks().get(t.TaskID); live {
				qs, _ := call.snapshot()
				open = s.openQuestions(qs)
			}
			ambiguous := false
			if len(open) == 0 {
				open = s.unattributedSince(req.Server, start)
				ambiguous = len(open) > 0
			}
			if len(open) == 0 {
				continue
			}
			doc := s.describeInput(t.TaskID, req.Server, req.Tool, open)
			if ambiguous {
				doc.Ambiguous = true
				doc.Text = fmt.Sprintf("%s.%s's server asked a question while this call "+
					"was running, and other calls share the instance, so mcpx cannot be "+
					"sure it is this call's. `mcpx elicit list` shows every open question.\n\n",
					req.Server, req.Tool) + doc.Text
			}
			writeJSON(w, http.StatusAccepted, map[string]any{"inputRequired": doc})
			return
		}
	}
}

// unattributedSince lists open questions from server, raised after since,
// that no call could be identified as the owner of.
func (s *Server) unattributedSince(server string, since time.Time) []mcpserver.Question {
	if s.reg.broker == nil {
		return nil
	}
	pending, err := s.reg.broker.Pending(elicit.Filter{})
	if err != nil {
		return nil
	}
	var out []mcpserver.Question
	for _, rq := range pending {
		// mcpx's own policy questions (a destructive confirmation, a
		// disambiguation, a recipe form) carry the caller's session and have
		// their own timeout and fallback; a question a server raised through
		// elicitViaBroker carries neither a session nor a tool. Only those.
		if rq.Server != server || rq.Trace != "" || rq.Tool != "" || rq.Session != "" ||
			rq.Created.Before(since) {
			continue
		}
		method := "elicitation/create"
		if rq.Mode == elicit.Sample {
			method = "sampling/createMessage"
		}
		params, _ := json.Marshal(map[string]any{"message": rq.Message,
			"requestedSchema": rq.Schema, "url": rq.URL})
		out = append(out, mcpserver.Question{ID: rq.ID, Method: method, Params: params,
			Mode: string(rq.Mode), Server: rq.Server})
	}
	return out
}

func (s *Server) describeInput(callID, server, tool string, open []mcpserver.Question) InputRequired {
	out := InputRequired{CallID: callID, Server: server, Tool: tool}
	for _, q := range open {
		iq := InputQuestion{ID: q.ID, Method: q.Method, Mode: q.Mode, Server: q.Server}
		var p struct {
			Message         string          `json:"message"`
			URL             string          `json:"url"`
			RequestedSchema json.RawMessage `json:"requestedSchema"`
			SystemPrompt    string          `json:"systemPrompt"`
		}
		_ = json.Unmarshal(q.Params, &p)
		iq.Message, iq.URL = p.Message, p.URL
		if string(p.RequestedSchema) != "null" {
			iq.RequestedSchema = p.RequestedSchema
		}
		if iq.Message == "" {
			iq.Message = p.SystemPrompt
		}
		if s.reg.broker != nil {
			if rq, found, err := s.reg.broker.Get(q.ID); err == nil && found {
				iq.ExpiresAt = rq.ExpiresAt
			}
		}
		iq.RespondWith = respondWith(q.ID, q.Mode, p.RequestedSchema)
		out.Questions = append(out.Questions, iq)
	}
	out.Text = renderInputRequired(out)
	return out
}

// respondWith spells out the answering command with a skeleton of the
// requested fields, because the alternative is the reader assembling it from
// three places and getting the quoting wrong.
func respondWith(id, mode string, schema json.RawMessage) string {
	if mode == string(elicit.URL) {
		return "mcpx elicit answer " + id + " '{}'"
	}
	var sch struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(schema, &sch)
	names := make([]string, 0, len(sch.Properties))
	for n := range sch.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, n := range names {
		v := `"..."`
		switch sch.Properties[n].Type {
		case "number", "integer":
			v = "0"
		case "boolean":
			v = "true"
		}
		parts[i] = fmt.Sprintf("%q:%s", n, v)
	}
	return "mcpx elicit answer " + id + " '{" + strings.Join(parts, ",") + "}'"
}

func renderInputRequired(in InputRequired) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.%s is waiting for input and nothing here can answer it.\n", in.Server, in.Tool)
	for _, q := range in.Questions {
		fmt.Fprintf(&b, "\n  asks     %s\n", q.Message)
		if q.URL != "" {
			fmt.Fprintf(&b, "  open     %s\n", q.URL)
		}
		if len(q.RequestedSchema) > 0 {
			fmt.Fprintf(&b, "  schema   %s\n", compactJSON(q.RequestedSchema))
		}
		if !q.ExpiresAt.IsZero() {
			fmt.Fprintf(&b, "  expires  %s (%s from now)\n", q.ExpiresAt.Format(time.RFC3339),
				time.Until(q.ExpiresAt).Round(time.Second))
		}
		fmt.Fprintf(&b, "  answer   %s\n", q.RespondWith)
		fmt.Fprintf(&b, "           mcpx elicit decline %s | mcpx elicit cancel %s\n", q.ID, q.ID)
	}
	fmt.Fprintf(&b, "\nThe call keeps running until it is answered or expires; "+
		"collect its result with: mcpx task result %s\n", in.CallID)
	return b.String()
}

func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}
