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
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	_ "modernc.org/sqlite"
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

	result   any
	fault    *Fault
	cancel   context.CancelFunc
	done     chan struct{}
	OnCancel func(ctx context.Context, reason string) `json:"-"`
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
	db    *sql.DB
	// PollInterval is the pollInterval each new task carries. Zero means
	// the built-in default. Set before the store is shared.
	PollInterval time.Duration

	// watchers hear the id of every task whose state changed, after the
	// change and outside the lock, so a watcher may read the store.
	watchers map[int]func(id string)
	nextW    int

	// OnCancelHook is called when a task is cancelled to perform cooperative
	// upstream cancellation (e.g. notifications/cancelled or process interrupt)
	OnCancelHook func(id string, reason string)
}

// Watch calls fn with a task's id each time its status, input requests or
// status message change -- which is what notifications/tasks reports. The
// call comes after the change is visible to Get, and for a finished task
// after Result has stopped blocking. stop ends the watch.
func (s *Store) Watch(fn func(id string)) (stop func()) {
	s.mu.Lock()
	if s.watchers == nil {
		s.watchers = map[int]func(string){}
	}
	n := s.nextW
	s.nextW++
	s.watchers[n] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.watchers, n)
		s.mu.Unlock()
	}
}

// changed tells the watchers. Called without the lock held.
func (s *Store) changed(id string) {
	s.mu.Lock()
	fns := make([]func(string), 0, len(s.watchers))
	for _, fn := range s.watchers {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(id)
	}
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

// Open opens or creates a persistent SQLite-backed task store.
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("creating task db dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("opening task db: %w", err)
	}
	st := &Store{
		tasks: map[string]*Task{},
		db:    db,
	}
	if err := st.initDB(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) initDB() error {
	schema := `
CREATE TABLE IF NOT EXISTS tasks (
    task_id        TEXT PRIMARY KEY,
    status         TEXT NOT NULL,
    status_message TEXT,
    created_at_ms  INTEGER NOT NULL,
    updated_at_ms  INTEGER NOT NULL,
    ttl_ms         INTEGER NOT NULL,
    poll_interval  INTEGER NOT NULL,
    result_json    TEXT,
    fault_json     TEXT
);`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("initializing tasks schema: %w", err)
	}
	return s.loadTasksFromDB()
}

func (s *Store) loadTasksFromDB() error {
	rows, err := s.db.Query(`
SELECT task_id, status, status_message, created_at_ms, updated_at_ms, ttl_ms, poll_interval, result_json, fault_json
FROM tasks`)
	if err != nil {
		return fmt.Errorf("querying stored tasks: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	nowMs := now.UnixMilli()

	var toDelete []string
	var toUpdate []*Task

	for rows.Next() {
		var id, status, statusMsg string
		var createdMs, updatedMs, ttlMs, pollInterval int64
		var resultJSON, faultJSON sql.NullString

		if err := rows.Scan(&id, &status, &statusMsg, &createdMs, &updatedMs, &ttlMs, &pollInterval, &resultJSON, &faultJSON); err != nil {
			continue
		}

		expiresAtMs := createdMs + ttlMs
		if expiresAtMs <= nowMs {
			toDelete = append(toDelete, id)
			continue
		}

		createdAt := time.UnixMilli(createdMs)
		updatedAt := time.UnixMilli(updatedMs)

		t := &Task{
			TaskID:        id,
			Status:        status,
			StatusMessage: statusMsg,
			CreatedAt:     createdAt,
			LastUpdatedAt: updatedAt,
			TTL:           ttlMs,
			PollInterval:  pollInterval,
			done:          make(chan struct{}),
		}

		if resultJSON.Valid && resultJSON.String != "" {
			var res any
			if json.Unmarshal([]byte(resultJSON.String), &res) == nil {
				t.result = res
			}
		}
		if faultJSON.Valid && faultJSON.String != "" {
			var fault Fault
			if json.Unmarshal([]byte(faultJSON.String), &fault) == nil {
				t.fault = &fault
			}
		}

		if !Terminal(t.Status) {
			t.Status = Failed
			t.StatusMessage = "daemon restarted before task completion"
			t.fault = &Fault{Code: -32603, Message: t.StatusMessage}
			t.LastUpdatedAt = now
			toUpdate = append(toUpdate, t)
		}

		close(t.done)
		s.tasks[t.TaskID] = t

		remaining := time.Duration(expiresAtMs-nowMs) * time.Millisecond
		time.AfterFunc(remaining, func() {
			s.mu.Lock()
			delete(s.tasks, id)
			s.deleteFromDBLocked(id)
			s.mu.Unlock()
		})
	}

	for _, id := range toDelete {
		s.deleteFromDBLocked(id)
	}
	for _, t := range toUpdate {
		s.saveTaskLocked(t)
	}
	return nil
}

func (s *Store) saveTaskLocked(t *Task) {
	if s.db == nil {
		return
	}
	var resJSON, faultJSON *string
	if t.result != nil {
		if b, err := json.Marshal(t.result); err == nil {
			str := string(b)
			resJSON = &str
		}
	}
	if t.fault != nil {
		if b, err := json.Marshal(t.fault); err == nil {
			str := string(b)
			faultJSON = &str
		}
	}

	query := `
INSERT INTO tasks (task_id, status, status_message, created_at_ms, updated_at_ms, ttl_ms, poll_interval, result_json, fault_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(task_id) DO UPDATE SET
  status = excluded.status,
  status_message = excluded.status_message,
  updated_at_ms = excluded.updated_at_ms,
  result_json = excluded.result_json,
  fault_json = excluded.fault_json;`

	_, _ = s.db.Exec(query,
		t.TaskID,
		t.Status,
		t.StatusMessage,
		t.CreatedAt.UnixMilli(),
		t.LastUpdatedAt.UnixMilli(),
		t.TTL,
		t.PollInterval,
		resJSON,
		faultJSON,
	)
}

func (s *Store) deleteFromDBLocked(id string) {
	if s.db == nil {
		return
	}
	_, _ = s.db.Exec("DELETE FROM tasks WHERE task_id = ?", id)
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) SetTaskCancel(id string, fn func(ctx context.Context, reason string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.OnCancel = fn
	}
}

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
	s.saveTaskLocked(t)
	s.mu.Unlock()

	go func() {
		result, fault := fn(ctx)
		moved := s.finish(t, result, fault)
		close(t.done)
		if moved {
			s.changed(t.TaskID)
		}
	}()

	// Expire it after its TTL, so an uncollected result does not live
	// forever.
	time.AfterFunc(time.Duration(ttl)*time.Millisecond, func() {
		s.mu.Lock()
		delete(s.tasks, t.TaskID)
		s.deleteFromDBLocked(t.TaskID)
		s.mu.Unlock()
		cancel()
	})
	// Snapshotted under the lock, so the value handed back cannot be read
	// while the goroutine above is writing the original.
	s.mu.Lock()
	defer s.mu.Unlock()
	return *t
}

// finish records what a task's body produced, and reports whether that
// changed its status: a task cancelled while running keeps its cancellation.
func (s *Store) finish(t *Task, result any, fault *Fault) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.Status == Cancelled {
		return false // cancelled while running; the cancellation stands
	}
	t.LastUpdatedAt = time.Now()
	t.InputRequests = nil
	if fault != nil {
		t.Status, t.fault = Failed, fault
		t.StatusMessage = fault.Message
		s.saveTaskLocked(t)
		return true
	}
	t.Status, t.result = Completed, result
	// A tool result that is itself an error is a failed task, per the
	// specification's own note on TaskStatus.
	if m, ok := result.(map[string]any); ok {
		if isErr, _ := m["isError"].(bool); isErr {
			t.Status = Failed
		}
	}
	s.saveTaskLocked(t)
	return true
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
	t, ok := s.tasks[id]
	moved := ok && !Terminal(t.Status) && (t.Status != status || t.StatusMessage != message)
	if moved {
		t.Status, t.StatusMessage, t.LastUpdatedAt = status, message, time.Now()
		s.saveTaskLocked(t)
	}
	s.mu.Unlock()
	if moved {
		s.changed(id)
	}
}

// SetInput records the questions a running task is waiting on. A non-empty
// set makes it input_required, an empty one working again. Called with the
// same set it changes nothing, lastUpdatedAt included.
func (s *Store) SetInput(id string, requests map[string]any) {
	if s.setInput(id, requests) {
		s.changed(id)
	}
}

func (s *Store) setInput(id string, requests map[string]any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || Terminal(t.Status) {
		return false
	}
	status := Working
	if len(requests) > 0 {
		status = InputRequired
	} else {
		requests = nil
	}
	if status == t.Status && sameKeys(requests, t.InputRequests) {
		return false
	}
	t.Status, t.InputRequests, t.LastUpdatedAt = status, requests, time.Now()
	s.saveTaskLocked(t)
	return true
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
	return s.CancelWithReason(id, "cancelled")
}

// CancelWithReason stops a task with an explanatory reason and returns its final state,
// performing a cooperative cancellation handshake before hard termination.
func (s *Store) CancelWithReason(id string, reason string) (Task, bool) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return Task{}, false
	}
	moved := !Terminal(t.Status)
	var onCancel func(ctx context.Context, reason string)
	var cancel context.CancelFunc
	if moved {
		t.Status, t.LastUpdatedAt = Cancelled, time.Now()
		if t.StatusMessage == "" && reason != "" {
			t.StatusMessage = reason
		}
		s.saveTaskLocked(t)
		onCancel = t.OnCancel
		cancel = t.cancel
	}
	snap := *t
	hook := s.OnCancelHook
	s.mu.Unlock()

	if moved {
		if onCancel != nil {
			ctx, cancelCtx := context.WithTimeout(context.Background(), defaults.UpstreamCancelSendTimeout)
			onCancel(ctx, reason)
			cancelCtx()
		}
		if hook != nil {
			hook(id, reason)
		}
		if cancel != nil {
			cancel()
		}
		s.changed(id)
	}
	return snap, true
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
