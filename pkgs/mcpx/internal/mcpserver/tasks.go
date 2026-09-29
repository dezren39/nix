package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// Task is a request running in the background.
//
// A client opts in by adding `task` to a request's params. Instead of waiting
// for the result it gets a handle back immediately, and polls with tasks/get
// or collects with tasks/result. That is the right shape for a genuinely slow
// tool -- a build, a crawl, a browser session -- where holding a request open
// for minutes invites every intermediary to time it out.
type Task struct {
	TaskID        string    `json:"taskId"`
	Status        string    `json:"status"`
	StatusMessage string    `json:"statusMessage,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	LastUpdatedAt time.Time `json:"lastUpdatedAt"`
	// TTL is milliseconds after creation that the result is kept. Null in
	// the specification means unbounded; mcpx never offers unbounded,
	// because a result nobody collects is memory nobody frees.
	TTL          int64 `json:"ttl"`
	PollInterval int64 `json:"pollInterval,omitempty"`

	result any
	err    *rpcError
	cancel context.CancelFunc
	done   chan struct{}
}

// Task statuses, from the specification.
const (
	TaskWorking       = "working"
	TaskInputRequired = "input_required"
	TaskCompleted     = "completed"
	TaskFailed        = "failed"
	TaskCancelled     = "cancelled"
)

func terminal(status string) bool {
	return status == TaskCompleted || status == TaskFailed || status == TaskCancelled
}

type taskStore struct {
	mu    sync.Mutex
	tasks map[string]*Task
}

func (s *Server) tasks() *taskStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taskStore == nil {
		s.taskStore = &taskStore{tasks: map[string]*Task{}}
	}
	return s.taskStore
}

func newTaskID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "tsk-" + hex.EncodeToString(b[:])
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
		ttl = int64(defaults.TaskTTL / time.Millisecond)
	}
	return true, ttl
}

// startTask runs fn in the background and returns its handle at once.
func (s *Server) startTask(ttl int64, fn func(ctx context.Context) (any, *rpcError)) *Task {
	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	t := &Task{
		TaskID: newTaskID(), Status: TaskWorking,
		CreatedAt: now, LastUpdatedAt: now,
		TTL: ttl, PollInterval: 1000,
		cancel: cancel, done: make(chan struct{}),
	}
	st := s.tasks()
	st.mu.Lock()
	st.tasks[t.TaskID] = t
	st.mu.Unlock()

	go func() {
		defer close(t.done)
		result, err := fn(ctx)
		st.mu.Lock()
		defer st.mu.Unlock()
		if t.Status == TaskCancelled {
			return // cancelled while running; the cancellation stands
		}
		t.LastUpdatedAt = time.Now()
		if err != nil {
			t.Status, t.err = TaskFailed, err
			t.StatusMessage = err.Message
			return
		}
		t.Status, t.result = TaskCompleted, result
		// A tool result that is itself an error is a failed task, per the
		// specification's own note on TaskStatus.
		if m, ok := result.(map[string]any); ok {
			if isErr, _ := m["isError"].(bool); isErr {
				t.Status = TaskFailed
			}
		}
	}()

	// Expire it after its TTL, so an uncollected result does not live
	// forever.
	time.AfterFunc(time.Duration(ttl)*time.Millisecond, func() {
		st.mu.Lock()
		delete(st.tasks, t.TaskID)
		st.mu.Unlock()
		cancel()
	})
	return t
}

// SetTaskStatus lets a long-running call report progress into its task, and
// is how a call waiting on an elicitation shows input_required.
func (s *Server) SetTaskStatus(taskID, status, message string) {
	st := s.tasks()
	st.mu.Lock()
	defer st.mu.Unlock()
	if t, ok := st.tasks[taskID]; ok && !terminal(t.Status) {
		t.Status, t.StatusMessage, t.LastUpdatedAt = status, message, time.Now()
	}
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
		st.mu.Lock()
		list := make([]Task, 0, len(st.tasks))
		for _, t := range st.tasks {
			list = append(list, *t)
		}
		st.mu.Unlock()
		sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
		items, next := page(list, req.Params, s.pageSize())
		out := map[string]any{"tasks": items}
		if next != "" {
			out["nextCursor"] = next
		}
		return reply(out)

	case "tasks/get":
		st.mu.Lock()
		t, ok := st.tasks[p.TaskID]
		var snap Task
		if ok {
			snap = *t
		}
		st.mu.Unlock()
		if !ok {
			return fail(codeInvalidParams, "no task "+p.TaskID+"; it may have expired")
		}
		return reply(snap)

	case "tasks/result":
		st.mu.Lock()
		t, ok := st.tasks[p.TaskID]
		st.mu.Unlock()
		if !ok {
			return fail(codeInvalidParams, "no task "+p.TaskID+"; it may have expired")
		}
		// Blocks until the task ends, which is what the specification asks
		// of tasks/result -- it is the "wait for it" half of the pair, and
		// tasks/get is the "check on it" half.
		select {
		case <-t.done:
		case <-ctx.Done():
			return fail(codeInternal, "the request ended before the task did")
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		if t.err != nil {
			return &response{JSONRPC: "2.0", ID: req.ID, Error: t.err}
		}
		if t.Status == TaskCancelled {
			return fail(codeInternal, "the task was cancelled")
		}
		return reply(t.result)

	case "tasks/cancel":
		st.mu.Lock()
		t, ok := st.tasks[p.TaskID]
		if ok && !terminal(t.Status) {
			t.Status, t.LastUpdatedAt = TaskCancelled, time.Now()
			t.cancel()
		}
		var snap Task
		if ok {
			snap = *t
		}
		st.mu.Unlock()
		if !ok {
			return fail(codeInvalidParams, "no task "+p.TaskID)
		}
		return reply(snap)
	}
	return fail(codeMethodNotFound, "no method "+req.Method)
}
