package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dezren39/mcpx/internal/tasks"
)

// The task store moved to internal/tasks so the daemon's /v1 and this server
// share one implementation. Aliases rather than new names: the protocol code
// here still says Task and taskStore, and a task started over either surface
// behaves identically because there is only one store type.
type (
	// Task is a request running in the background.
	Task = tasks.Task
	// taskStore is where they live.
	taskStore = tasks.Store
)

// Task statuses, from the specification.
const (
	TaskWorking       = tasks.Working
	TaskInputRequired = tasks.InputRequired
	TaskCompleted     = tasks.Completed
	TaskFailed        = tasks.Failed
	TaskCancelled     = tasks.Cancelled
)

func (s *Server) tasks() *taskStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taskStore == nil {
		s.taskStore = tasks.New()
		s.taskStore.PollInterval = s.Timing.resolved().TaskPoll
	}
	return s.taskStore
}

// wantsTask reports whether a request asked to run as a task, and with what
// TTL.
func wantsTask(params json.RawMessage) (bool, int64) {
	var p struct {
		Task *struct {
			TTL int64 `json:"ttl"`
		} `json:"task"`
	}
	if json.Unmarshal(params, &p) != nil || p.Task == nil {
		return false, 0
	}
	ttl := p.Task.TTL
	if ttl <= 0 {
		ttl = tasks.DefaultTTL()
	}
	return true, ttl
}

// startTask runs fn in the background and returns its handle at once,
// recording which connection owns it.
//
// Ownership is what stops tasks/list being a directory of everybody's work.
// The store is one per server, and over HTTP one server answers every
// client: without an owner, any client could list -- and then read -- the
// results of every other client's calls.
func (s *Server) startTask(owner string, ttl int64, fn func(ctx context.Context) (any, *rpcError)) Task {
	t := s.tasks().Start(ttl, func(ctx context.Context) (any, *tasks.Fault) {
		result, err := fn(ctx)
		if err != nil {
			return nil, &tasks.Fault{Code: err.Code, Message: err.Message, Data: err.Data}
		}
		return result, nil
	})
	s.mu.Lock()
	if s.taskOwners == nil {
		s.taskOwners = map[string]string{}
	}
	s.taskOwners[t.TaskID] = owner
	s.mu.Unlock()
	return t
}

// visible reports whether a connection may see a task.
//
// A task with no owner -- started by a modern client, or by a legacy one on
// a connection with no identity -- is reachable by its id alone, which is
// unguessable: that is the tasks extension's whole model. One with an owner
// is reachable only from that owner, whoever else learns the id.
func (s *Server) visible(taskID string, c *Conn) bool {
	s.mu.Lock()
	owner := s.taskOwners[taskID]
	s.mu.Unlock()
	return owner == "" || owner == c.id
}

// runInner is how a request becomes the body of a task: the same dispatch,
// on the same connection, with the task request removed so it does not ask
// to become a task again and recurse.
func (s *Server) runInner(c *Conn, req request) func(ctx context.Context) (any, *rpcError) {
	inner := req
	inner.Params = withoutTask(req.Params)
	return func(tctx context.Context) (any, *rpcError) {
		// Answered on the same connection, so a question the call raises
		// reaches the client that started it rather than whichever one the
		// server happens to call default.
		resp := s.HandleOn(withinTask(tctx), c, inner)
		if resp == nil {
			return nil, &rpcError{Code: codeInternal, Message: "no result"}
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

type withinTaskKey struct{}

func withinTask(ctx context.Context) context.Context {
	return context.WithValue(ctx, withinTaskKey{}, true)
}

func isWithinTask(ctx context.Context) bool {
	v, _ := ctx.Value(withinTaskKey{}).(bool)
	return v
}

// Tool-level task support, from execution.taskSupport.
const (
	supportForbidden = "forbidden"
	supportOptional  = "optional"
	supportRequired  = "required"
)

// taskSupportOf is what the tool a tools/call names declared about running
// as a task: a pass-through upstream's execution.taskSupport, verbatim, or
// "" for a tool that declared nothing -- every gateway tool, and an upstream
// tool without the field.
func (s *Server) taskSupportOf(ctx context.Context, params json.RawMessage) string {
	var call struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(params, &call) != nil || call.Name == "" {
		return ""
	}
	t, ok := s.passTool(ctx, call.Name)
	if !ok || len(t.Execution) == 0 {
		return ""
	}
	var ex struct {
		TaskSupport string `json:"taskSupport"`
	}
	_ = json.Unmarshal(t.Execution, &ex)
	return ex.TaskSupport
}

// legacyTaskSupport is the taskSupport of the tool a 2025-11-25 tools/call
// names, from wherever tools/list got it: the pass-through upstream's
// declaration, or mcpx's own. Absent is "forbidden".
//
// The 2026-07-28 path keeps taskSupportOf, which reads pass-through tools
// only: there the server decides, and mcpx's own tools stay on TaskAfter.
func (s *Server) legacyTaskSupport(ctx context.Context, params json.RawMessage) string {
	if sup := s.taskSupportOf(ctx, params); sup != "" {
		return sup
	}
	var call struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(params, &call) != nil {
		return supportForbidden
	}
	if _, isPass := s.passTool(ctx, call.Name); isPass {
		return supportForbidden
	}
	for _, t := range s.Tools() {
		if t.Name == call.Name && len(t.Execution) > 0 {
			var ex struct {
				TaskSupport string `json:"taskSupport"`
			}
			_ = json.Unmarshal(t.Execution, &ex)
			return ex.TaskSupport
		}
	}
	return supportForbidden
}

// missingTasks is -32021 naming the tasks extension: what a client that did
// not declare it gets when the only answer is a task.
func missingTasks(id json.RawMessage) *response {
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{
		Code: codeMissingCapability, Message: "Missing required client capability",
		Data: map[string]any{"requiredCapabilities": map[string]any{
			"extensions": map[string]any{ExtTasks: map[string]any{}}}}}}
}

// maybeTask decides whether a tools/call becomes a task, and if so answers
// it with the handle. nil means run it the ordinary way.
//
// The two eras decide differently, and each forbids the other's way:
//
//   - 2025-11-25 core tasks are client-directed. The request carries a task
//     field and gets {task} back at once. mcpx honours the field from any
//     legacy client, accepting liberally.
//   - The 2026-07-28 extension is server-directed. The task field is gone,
//     and the SEP says a server MUST ignore it rather than treat it as an
//     opt-in; a server MUST NOT return a task to a client that did not
//     declare the extension on that request.
//
// For a modern client the tool's execution.taskSupport decides:
//
//   - "required" from a client that did not declare the extension is
//     -32021, before anything runs: the only answer would be a task.
//   - "optional" or "required" is task-supporting. The call gets
//     Timing.TaskEager to finish or to ask its first question; past that it
//     is a task. A question asked in that window goes to the client inline
//     (input_required on this request), so the questions a tool asks up
//     front are answered before any task exists, as the SEP asks; one asked
//     after parks the task in input_required with inputRequests, answered
//     through tasks/update.
//   - anything else is run in line for Timing.TaskAfter and handed a task
//     only if it has not finished by then -- except for a client that can
//     answer questions inline, which is served on the original request.
//   - "forbidden" never becomes a task.
func (s *Server) maybeTask(ctx context.Context, c *Conn, req request, peer Peer) *response {
	if isWithinTask(ctx) {
		return nil
	}
	if !peer.Modern {
		want, ttl := wantsTask(req.Params)
		if !want {
			return nil
		}
		if sup := s.legacyTaskSupport(ctx, req.Params); sup != supportOptional && sup != supportRequired {
			// 2025-11-25: a tool without taskSupport is "forbidden", the
			// client MUST NOT augment a call to it, and the server SHOULD
			// refuse one that does with -32601.
			return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeMethodNotFound,
				Message: "this tool does not support task-augmented execution (execution.taskSupport is forbidden)"}}
		}
		t := s.startTask(c.id, ttl, s.runInner(c, req))
		return &response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"task": t}}
	}
	support := s.taskSupportOf(ctx, req.Params)
	if !peer.DeclaredExtension(ExtTasks) {
		if support == supportRequired {
			return missingTasks(req.ID)
		}
		return nil
	}
	eager := support == supportOptional || support == supportRequired
	tm := s.Timing.resolved()
	if s.canAsk(ctx, c, peer) {
		if !eager {
			return nil
		}
		if resp, handled := s.askTask(ctx, req, peer); handled {
			return resp
		}
	} else if _, _, resuming := resumeOf(req.Params); resuming {
		return nil
	}
	if support == supportForbidden {
		return nil
	}
	after := tm.TaskAfter
	if eager {
		after = tm.TaskEager
	}
	t := s.startTask("", tasks.DefaultTTL(), s.runInner(c, req))
	wait, cancel := context.WithTimeout(ctx, after)
	defer cancel()
	result, fault, err := s.tasks().Result(wait, t.TaskID)
	if err == nil {
		// Finished in time: answered as if tasks did not exist. The task
		// stays in the store until its TTL, harmlessly; nobody holds its id.
		if fault != nil {
			return &response{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: fault.Code, Message: fault.Message, Data: fault.Data}}
		}
		return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	return s.created(req, t.TaskID)
}

// created is the CreateTaskResult for a task: the task itself, flat.
func (s *Server) created(req request, id string) *response {
	snap, _ := s.tasks().Get(id)
	out := modernTask(snap)
	out["resultType"] = "task"
	return &response{JSONRPC: "2.0", ID: req.ID, Result: out}
}

// askedTask is a task whose body is a call running through the Asker, so
// that tasks/update can answer the questions it raises.
type askedTask struct {
	callID string
	peer   Peer
}

// askTask runs a task-supporting tools/call through the Asker. Only a
// pass-through tool declares task support, which is why the calls here are
// finished as pass-through calls. handled is
// false when the call turns out not to be interruptible, and the caller
// runs it as an ordinary task instead.
func (s *Server) askTask(ctx context.Context, req request, peer Peer) (*response, bool) {
	callID, _, _, failed := s.beginAsk(ctx, req, peer)
	if failed != nil {
		return failed, true
	}
	if callID == "" {
		return nil, false
	}
	tm := s.Timing.resolved()
	out, err := s.Ask.Poll(ctx, callID, tm.TaskEager)
	if err != nil {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternal, Message: err.Error()}}, true
	}
	if out.Done {
		return finishedAsk(req, out, peer, true), true
	}
	if sendable := sendableTo(out.Questions, peer); len(sendable) > 0 {
		// Asked before the call became a task: answered inline, on this
		// request, as any other modern request is. The whole round, not
		// the first of it to arrive (awaitRound).
		if m, ok := s.awaitRound(ctx, callID, sendable, peer); ok {
			sendable = m
		}
		if state, err := s.states().mint(callID, requestBinding(req)); err == nil {
			return &response{JSONRPC: "2.0", ID: req.ID, Result: inputRequired(sendable, state, peer)}, true
		}
	}

	t := s.tasks().Start(tasks.DefaultTTL(), func(tctx context.Context) (any, *tasks.Fault) {
		return s.driveAsked(tctx, req, peer, callID)
	})
	s.mu.Lock()
	if s.askedTasks == nil {
		s.askedTasks = map[string]askedTask{}
	}
	s.askedTasks[t.TaskID] = askedTask{callID: callID, peer: peer}
	s.mu.Unlock()
	return s.created(req, t.TaskID), true
}

// driveAsked is the body of an asked task: wait for the call, publishing the
// questions it is waiting on as the task's inputRequests.
func (s *Server) driveAsked(ctx context.Context, req request, peer Peer, callID string) (any, *tasks.Fault) {
	id := tasks.IDFrom(ctx)
	tm := s.Timing.resolved()
	for {
		if ctx.Err() != nil {
			// Cancelled: tasks/cancel, or the TTL. Nobody will answer.
			s.Ask.Abandon(callID)
			return nil, &tasks.Fault{Code: codeInternal, Message: "the task was cancelled"}
		}
		out, err := s.Ask.Poll(ctx, callID, tm.AskPoll)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			return nil, &tasks.Fault{Code: codeInternal, Message: err.Error()}
		}
		if out.Done {
			resp := finishedAsk(req, out, peer, true)
			if resp.Error != nil {
				return nil, &tasks.Fault{Code: resp.Error.Code, Message: resp.Error.Message, Data: resp.Error.Data}
			}
			return resp.Result, nil
		}
		sendable := sendableTo(out.Questions, peer)
		s.tasks().SetInput(id, inputRequestsOf(sendable, peer))
		if len(sendable) > 0 {
			// Poll returns at once while a question is open; wait for an
			// answer, a cancel, or the next look.
			select {
			case <-ctx.Done():
			case <-time.After(tm.TaskPoll):
			}
		}
	}
}

// inputRequestsOf is the inputRequests map for a set of open questions,
// keyed as the client answers them.
func inputRequestsOf(qs []Question, p Peer) map[string]any {
	if len(qs) == 0 {
		return nil
	}
	return inputRequired(qs, "", p)["inputRequests"].(map[string]any)
}

// updateAsked hands tasks/update's answers to the call behind an asked task.
//
// Only answers to questions still open are passed on; the SEP says the rest
// are ignored. The answered keys leave inputRequests at once, so a tasks/get
// straight after the acknowledgement does not offer them again.
func (s *Server) updateAsked(ctx context.Context, taskID string, at askedTask, responses map[string]json.RawMessage) error {
	open, err := s.Ask.Poll(ctx, at.callID, time.Millisecond)
	if err != nil || open.Done {
		return nil
	}
	qs := sendableTo(open.Questions, at.peer)
	current := inputRequestsOf(qs, at.peer)
	answers := map[string]json.RawMessage{}
	remaining := map[string]any{}
	for k, v := range current {
		if a, ok := responses[k]; ok {
			answers[k] = a
		} else {
			remaining[k] = v
		}
	}
	if len(answers) == 0 {
		return nil
	}
	if err := s.Ask.Reply(ctx, at.callID, answersByID(answers, qs)); err != nil {
		return err
	}
	s.tasks().SetInput(taskID, remaining)
	return nil
}

// modernTask renders a task in the extension's shape: ttlMs and
// pollIntervalMs, where 2025-11-25 said ttl and pollInterval.
func modernTask(t Task) map[string]any {
	out := map[string]any{
		"taskId":         t.TaskID,
		"status":         t.Status,
		"createdAt":      t.CreatedAt,
		"lastUpdatedAt":  t.LastUpdatedAt,
		"ttlMs":          t.TTL,
		"pollIntervalMs": t.PollInterval,
	}
	if t.StatusMessage != "" {
		out["statusMessage"] = t.StatusMessage
	}
	return out
}

// detailedTask is modernTask plus what tasks/get adds while a task runs: the
// questions an input_required task is waiting on.
func detailedTask(t Task) map[string]any {
	out := modernTask(t)
	if t.Status == TaskInputRequired && len(t.InputRequests) > 0 {
		out["inputRequests"] = t.InputRequests
	}
	return out
}

// detailedNow is the extension's DetailedTask for a task as it stands: what
// tasks/get answers and what notifications/tasks carries, which the SEP says
// are identical. A terminal task inlines its result or error.
func (s *Server) detailedNow(ctx context.Context, id string) (map[string]any, bool) {
	st := s.tasks()
	snap, ok := st.Get(id)
	if !ok {
		return nil, false
	}
	out := detailedTask(snap)
	if !tasks.Terminal(snap.Status) {
		return out, true
	}
	// Terminal, so this does not block.
	result, fault, err := st.Result(ctx, id)
	switch {
	case err != nil:
		return nil, false
	case snap.Status == tasks.Cancelled:
		// Nothing inlined: a cancelled task carries neither result
		// nor error.
	case fault != nil:
		out["status"] = tasks.Failed
		out["error"] = map[string]any{"code": fault.Code, "message": fault.Message, "data": fault.Data}
	default:
		// The store calls a tool result with isError a failed task,
		// which is 2025-11-25's rule. The extension says the opposite:
		// failed is for JSON-RPC errors only, and a tool that ran and
		// reported an error is completed with that result.
		out["status"] = tasks.Completed
		out["result"] = result
	}
	return out, true
}

func (s *Server) handleTask(ctx context.Context, c *Conn, req request, peer Peer) *response {
	reply := func(result any) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	fail := func(code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}
	var p struct {
		TaskID string `json:"taskId"`
	}
	_ = json.Unmarshal(req.Params, &p)
	st := s.tasks()
	missing := func() *response {
		return fail(codeInvalidParams, tasks.ErrNoTask{ID: p.TaskID}.Error())
	}
	if peer.Modern && req.Method != "tasks/list" && req.Method != "tasks/result" &&
		!peer.DeclaredExtension(ExtTasks) {
		// The extension's methods belong to clients that declared it:
		// "Servers MUST return this error for non-declaring clients issuing
		// tasks/get, tasks/update, and tasks/cancel requests" (SEP-2663),
		// the error being 2026-07-28's -32021 with requiredCapabilities.
		// Checked before the id, so a non-declaring client learns nothing
		// about which tasks exist. tasks/list and tasks/result are removed
		// methods and keep their -32601.
		return missingTasks(req.ID)
	}
	if req.Method != "tasks/list" && !s.visible(p.TaskID, c) {
		// Said exactly as for a task that does not exist. Anything else
		// confirms to a stranger that the id is live.
		return missing()
	}

	if peer.Modern {
		return s.handleModernTask(ctx, req, p.TaskID, missing)
	}

	switch req.Method {
	case "tasks/list":
		var mine []Task
		for _, t := range st.List() {
			s.mu.Lock()
			owner := s.taskOwners[t.TaskID]
			s.mu.Unlock()
			// Only a connection with an identity lists anything, and only
			// its own. One without an identity has no "own" to list.
			if c.id != "" && owner == c.id {
				mine = append(mine, t)
			}
		}
		if mine == nil {
			mine = []Task{}
		}
		items, next, perr := page(mine, req.Params, s.pageSize())
		if perr != nil {
			return fail(codeInvalidParams, perr.Error())
		}
		out := map[string]any{"tasks": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "tasks/get":
		snap, ok := st.Get(p.TaskID)
		if !ok {
			return missing()
		}
		return reply(snap)

	case "tasks/result":
		result, fault, err := st.Result(ctx, p.TaskID)
		switch {
		case err != nil:
			var gone tasks.ErrNoTask
			if errors.As(err, &gone) {
				return fail(codeInvalidParams, gone.Error())
			}
			return fail(codeInternal, "the request ended before the task did")
		case fault != nil:
			return &response{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: fault.Code, Message: fault.Message, Data: fault.Data}}
		}
		// 2025-11-25: the tasks/result response MUST carry the
		// related-task _meta naming its task, since the result itself is
		// the underlying request's and does not say which task it was.
		raw, err := json.Marshal(result)
		if err == nil {
			raw, err = withRelatedTask(raw, p.TaskID)
		}
		var named map[string]any
		if err != nil || json.Unmarshal(raw, &named) != nil {
			// Not an object, so there is nowhere to put _meta.
			return reply(result)
		}
		return reply(named)

	case "tasks/cancel":
		// 2025-11-25: cancelling a task already in a terminal status is
		// an invalid request (-32602), not a success that changes nothing.
		if cur, ok := st.Get(p.TaskID); ok && tasks.Terminal(cur.Status) {
			return fail(codeInvalidParams, "task "+p.TaskID+" is already "+cur.Status)
		}
		snap, ok := st.Cancel(p.TaskID)
		if !ok {
			return missing()
		}
		return reply(snap)
	}
	return fail(codeMethodNotFound, "no method "+req.Method)
}

// handleModernTask answers the tasks extension's three methods.
//
// tasks/list and tasks/result are not among them. The SEP removed both --
// list because a stateless server cannot scope it, result because it
// blocked -- and says a client calling tasks/result MUST get -32601. That
// overrides "accept liberally": the extension defines the answer.
func (s *Server) handleModernTask(ctx context.Context, req request, id string, missing func() *response) *response {
	reply := func(result any) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	st := s.tasks()
	switch req.Method {
	case "tasks/get":
		out, ok := s.detailedNow(ctx, id)
		if !ok {
			return missing()
		}
		return reply(out)

	case "tasks/update":
		// Answers for an asked task's open questions. Keys that are not
		// outstanding are ignored, as the SEP says, so an update to a task
		// with nothing open -- or to one that is not asked at all -- is
		// acknowledged and changes nothing.
		if _, ok := st.Get(id); !ok {
			return missing()
		}
		var p struct {
			InputResponses map[string]json.RawMessage `json:"inputResponses"`
		}
		_ = json.Unmarshal(req.Params, &p)
		s.mu.Lock()
		at, asked := s.askedTasks[id]
		s.mu.Unlock()
		if asked && len(p.InputResponses) > 0 {
			if err := s.updateAsked(ctx, id, at, p.InputResponses); err != nil {
				return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternal, Message: err.Error()}}
			}
		}
		return reply(map[string]any{})

	case "tasks/cancel":
		if _, ok := st.Cancel(id); !ok {
			return missing()
		}
		// An empty acknowledgement, not the task: the extension's
		// CancelTaskResult carries nothing.
		return reply(map[string]any{})
	}
	return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{
		Code: codeMethodNotFound, Message: req.Method + " does not exist in 2026-07-28's tasks extension"}}
}
