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

// startTask runs fn in the background and returns its handle at once.
func (s *Server) startTask(ttl int64, fn func(ctx context.Context) (any, *rpcError)) *Task {
	return s.tasks().Start(ttl, func(ctx context.Context) (any, *tasks.Fault) {
		result, err := fn(ctx)
		if err != nil {
			return nil, &tasks.Fault{Code: err.Code, Message: err.Message, Data: err.Data}
		}
		return result, nil
	})
}

// SetTaskStatus lets a long-running call report progress into its task, and
// is how a call waiting on an elicitation shows input_required.
func (s *Server) SetTaskStatus(taskID, status, message string) {
	s.tasks().SetStatus(taskID, status, message)
}

func (s *Server) handleTask(ctx context.Context, req request) *response {
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

	switch req.Method {
	case "tasks/list":
		items, next := page(st.List(), req.Params, s.pageSize())
		out := map[string]any{"tasks": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "tasks/get":
		snap, ok := st.Get(p.TaskID)
		if !ok {
			return fail(codeInvalidParams, tasks.ErrNoTask{ID: p.TaskID}.Error())
		}
		return reply(snap)

	case "tasks/result":
		result, fault, err := st.Result(ctx, p.TaskID)
		switch {
		case err != nil:
			var missing tasks.ErrNoTask
			if errors.As(err, &missing) {
				return fail(codeInvalidParams, missing.Error())
			}
			return fail(codeInternal, "the request ended before the task did")
		case fault != nil:
			return &response{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: fault.Code, Message: fault.Message, Data: fault.Data}}
		}
		return reply(result)

	case "tasks/cancel":
		snap, ok := st.Cancel(p.TaskID)
		if !ok {
			return fail(codeInvalidParams, "no task "+p.TaskID)
		}
		return reply(snap)
	}
	return fail(codeMethodNotFound, "no method "+req.Method)
}
