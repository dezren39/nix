// Package tasks holds background requests and their results.
//
// A client opts in by asking for a task instead of a result. Instead of
// waiting it gets a handle back immediately, and polls for status or blocks
// for the result. That is the right shape for a genuinely slow call -- a
// build, a crawl, a browser session -- where holding a request open for
// minutes invites every intermediary to time it out.
//
// The store lives here rather than beside either caller because both the MCP
// server and the daemon's /v1 offer the same thing. Two stores would be two
// sets of expiry bugs, and a task started over one surface would be
// invisible from the other.
package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// Task is one background request.
type Task struct {
	TaskID        string    `json:"taskId"`
	Status        string    `json:"status"`
	StatusMessage string    `json:"statusMessage,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	LastUpdatedAt time.Time `json:"lastUpdatedAt"`
	// TTL is milliseconds after creation that the result is kept. Null in
	// the MCP specification means unbounded; mcpx never offers unbounded,
	// because a result nobody collects is memory nobody frees.
	TTL          int64 `json:"ttl"`
	PollInterval int64 `json:"pollInterval,omitempty"`
	// InputRequests are the questions an input_required task is waiting
	// on, keyed as the client answers them. The tasks extension carries them
	// on tasks/get; 2025-11-25 has no field for them, so they are not part
	// of this shape. Replaced whole, never mutated, so a snapshot may share
	// it.
	InputRequests map[string]any `json:"-"`

	result any
	fault  *Fault
	cancel context.CancelFunc
	done   chan struct{}
}

// Statuses, from the specification.
const (
	Working       = "working"
	InputRequired = "input_required"
	Completed     = "completed"
	Failed        = "failed"
	Cancelled     = "cancelled"
)

// Terminal reports whether a status is final.
func Terminal(status string) bool {
	return status == Completed || status == Failed || status == Cancelled
}

// Fault is why a task failed, carrying the JSON-RPC code so that an MCP
// client gets the error its transport defines rather than a flattened
// string.
type Fault struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (f *Fault) Error() string { return f.Message }

// Store holds running and recently finished tasks.
type Store struct {
	mu    sync.Mutex
	tasks map[string]*Task
	// PollInterval is the pollInterval each new task carries. Zero means
	// the built-in default. Set before the store is shared.
	PollInterval time.Duration
}

type idKey struct{}

// IDFrom is the id of the task whose body ctx belongs to, or "". It is how a
// call running as a task finds the task it should report its status into.
func IDFrom(ctx context.Context) string {
	id, _ := ctx.Value(idKey{}).(string)
	return id
}

// New builds an empty store.
func New() *Store { return &Store{tasks: map[string]*Task{}} }

func (s *Store) ensure() {
	if s.tasks == nil {
		s.tasks = map[string]*Task{}
	}
}

// NewID is the handle a caller holds a task by.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "tsk-" + hex.EncodeToString(b[:])
}

// DefaultTTL is the retention a caller gets when it does not ask for one, in
// milliseconds.
func DefaultTTL() int64 { return int64(defaults.TaskTTL / time.Millisecond) }

// Start runs fn in the background and returns its handle at once.
//
// The handle is a value, not a pointer into the store. A pointer is a
// reference to something the goroutine below is already writing: every
// caller copied it to put on the wire, and every one of those copies raced
// with the first status update. It never failed a test because the suite
// does not run under -race, which is the whole difficulty with this class
// of bug.
func (s *Store) Start(ttl int64, fn func(ctx context.Context) (any, *Fault)) Task {
	if ttl <= 0 {
		ttl = DefaultTTL()
	}
	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	poll := s.PollInterval
	if poll <= 0 {
		poll = defaults.TaskPollInterval
	}
	t := &Task{
		TaskID: NewID(), Status: Working,
		CreatedAt: now, LastUpdatedAt: now,
		TTL: ttl, PollInterval: poll.Milliseconds(),
		cancel: cancel, done: make(chan struct{}),
	}
	ctx = context.WithValue(ctx, idKey{}, t.TaskID)
	s.mu.Lock()
	s.ensure()
	s.tasks[t.TaskID] = t
	s.mu.Unlock()

	go func() {
		defer close(t.done)
		result, fault := fn(ctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		if t.Status == Cancelled {
			return // cancelled while running; the cancellation stands
		}
		t.LastUpdatedAt = time.Now()
		t.InputRequests = nil
		if fault != nil {
			t.Status, t.fault = Failed, fault
			t.StatusMessage = fault.Message
			return
		}
		t.Status, t.result = Completed, result
		// A tool result that is itself an error is a failed task, per the
		// specification's own note on TaskStatus.
		if m, ok := result.(map[string]any); ok {
			if isErr, _ := m["isError"].(bool); isErr {
				t.Status = Failed
			}
		}
	}()

	// Expire it after its TTL, so an uncollected result does not live
	// forever.
	time.AfterFunc(time.Duration(ttl)*time.Millisecond, func() {
		s.mu.Lock()
		delete(s.tasks, t.TaskID)
		s.mu.Unlock()
		cancel()
	})
	// Snapshotted under the lock, so the value handed back cannot be read
	// while the goroutine above is writing the original.
	s.mu.Lock()
	defer s.mu.Unlock()
	return *t
}

// List returns a snapshot, oldest first.
func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Get returns one task's current state.
func (s *Store) Get(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, false
	}
	return *t, true
}

// SetStatus lets a running call report progress, and is how a call waiting
// on an elicitation shows input_required.
func (s *Store) SetStatus(id, status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok && !Terminal(t.Status) {
		t.Status, t.StatusMessage, t.LastUpdatedAt = status, message, time.Now()
	}
}

// SetInput records the questions a running task is waiting on. A non-empty
// set makes it input_required, an empty one working again. Called with the
// same set it changes nothing, lastUpdatedAt included.
func (s *Store) SetInput(id string, requests map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || Terminal(t.Status) {
		return
	}
	status := Working
	if len(requests) > 0 {
		status = InputRequired
	} else {
		requests = nil
	}
	if status == t.Status && sameKeys(requests, t.InputRequests) {
		return
	}
	t.Status, t.InputRequests, t.LastUpdatedAt = status, requests, time.Now()
}

func sameKeys(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// Cancel stops a task and returns its final state.
func (s *Store) Cancel(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, false
	}
	if !Terminal(t.Status) {
		t.Status, t.LastUpdatedAt = Cancelled, time.Now()
		t.cancel()
	}
	return *t, true
}

// ErrNoTask is returned for a handle the store does not hold, which usually
// means it expired rather than that it never existed.
type ErrNoTask struct{ ID string }

func (e ErrNoTask) Error() string { return "no task " + e.ID + "; it may have expired" }

// Result waits for a task to finish and returns what it produced.
//
// Blocking is the point: this is the "wait for it" half of the pair, and Get
// is the "check on it" half. The context bounds the wait, so a caller that
// waited long enough can stop without stopping the task.
func (s *Store) Result(ctx context.Context, id string) (any, *Fault, error) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	s.mu.Unlock()
	if !ok {
		return nil, nil, ErrNoTask{ID: id}
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.fault != nil {
		return nil, t.fault, nil
	}
	if t.Status == Cancelled {
		return nil, &Fault{Code: -32603, Message: "the task was cancelled"}, nil
	}
	return t.result, nil, nil
}
