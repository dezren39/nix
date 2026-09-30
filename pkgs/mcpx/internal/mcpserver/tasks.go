package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

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
//     declare the extension on that request. mcpx runs the call and, if it
//     has not finished within Timing.TaskAfter, answers with a
//     CreateTaskResult. A fast call is answered directly, which the SEP
//     permits.
//
// A modern client that can answer questions inline is never handed a task:
// its questions travel as input_required on the original request, and the
// SEP asks that those be resolved before any task is created.
func (s *Server) maybeTask(ctx context.Context, c *Conn, req request, peer Peer) *response {
	if isWithinTask(ctx) {
		return nil
	}
	if !peer.Modern {
		want, ttl := wantsTask(req.Params)
		if !want {
			return nil
		}
		t := s.startTask(c.id, ttl, s.runInner(c, req))
		return &response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"task": t}}
	}
	if !peer.DeclaredExtension(ExtTasks) || s.canAsk(ctx, c, peer) {
		return nil
	}
	t := s.startTask("", tasks.DefaultTTL(), s.runInner(c, req))
	wait, cancel := context.WithTimeout(ctx, s.Timing.resolved().TaskAfter)
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
	snap, _ := s.tasks().Get(t.TaskID)
	out := modernTask(snap)
	out["resultType"] = "task"
	return &response{JSONRPC: "2.0", ID: req.ID, Result: out}
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

// SetTaskStatus lets a long-running call report progress into its task, and
// is how a call waiting on an elicitation shows input_required.
func (s *Server) SetTaskStatus(taskID, status, message string) {
	s.tasks().SetStatus(taskID, status, message)
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
		items, next := page(mine, req.Params, s.pageSize())
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
		return reply(result)

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
		snap, ok := st.Get(id)
		if !ok {
			return missing()
		}
		out := modernTask(snap)
		if !tasks.Terminal(snap.Status) {
			return reply(out)
		}
		// Terminal, so this does not block.
		result, fault, err := st.Result(ctx, id)
		switch {
		case err != nil:
			return missing()
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
		return reply(out)

	case "tasks/update":
		// mcpx never moves an extension task to input_required -- a client
		// that can answer questions is served by input_required on the
		// original request instead -- so no key is ever outstanding, and
		// the SEP says responses to keys that are not outstanding are
		// ignored. The acknowledgement is all there is.
		if _, ok := st.Get(id); !ok {
			return missing()
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
