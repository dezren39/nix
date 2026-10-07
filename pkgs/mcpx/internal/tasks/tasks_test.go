package tasks_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/tasks"
)

func TestAResultIsWaitedForRatherThanPolledFor(t *testing.T) {
	st := tasks.New()
	task := st.Start(0, func(ctx context.Context) (any, *tasks.Fault) {
		time.Sleep(20 * time.Millisecond)
		return map[string]any{"ok": true}, nil
	})
	if task.Status != tasks.Working {
		t.Fatalf("a fresh task is working, got %q", task.Status)
	}
	result, fault, err := st.Result(context.Background(), task.TaskID)
	if err != nil || fault != nil {
		t.Fatalf("err=%v fault=%v", err, fault)
	}
	if m, _ := result.(map[string]any); m["ok"] != true {
		t.Errorf("result = %v", result)
	}
	snap, ok := st.Get(task.TaskID)
	if !ok || snap.Status != tasks.Completed {
		t.Errorf("after collection the task is completed, got %+v", snap)
	}
}

func TestAToolResultThatIsItselfAnErrorIsAFailedTask(t *testing.T) {
	// The specification's own note on TaskStatus. Reporting it as completed
	// makes a client that checks only the status miss every failure.
	st := tasks.New()
	task := st.Start(0, func(ctx context.Context) (any, *tasks.Fault) {
		return map[string]any{"isError": true}, nil
	})
	if _, _, err := st.Result(context.Background(), task.TaskID); err != nil {
		t.Fatal(err)
	}
	snap, _ := st.Get(task.TaskID)
	if snap.Status != tasks.Failed {
		t.Errorf("status = %q", snap.Status)
	}
}

func TestCancellingStopsTheWorkAndStands(t *testing.T) {
	st := tasks.New()
	released := make(chan struct{})
	task := st.Start(0, func(ctx context.Context) (any, *tasks.Fault) {
		<-ctx.Done()
		close(released)
		return "too late", nil
	})
	snap, ok := st.Cancel(task.TaskID)
	if !ok || snap.Status != tasks.Cancelled {
		t.Fatalf("cancel returned %+v", snap)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("cancelling should cancel the context the work runs under")
	}
	_, fault, err := st.Result(context.Background(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if fault == nil {
		t.Fatal("collecting a cancelled task should report the cancellation")
	}
	if after, _ := st.Get(task.TaskID); after.Status != tasks.Cancelled {
		t.Errorf("a cancellation stands even though the work returned: %q", after.Status)
	}
}

func TestAnUnknownHandleSaysItMayHaveExpired(t *testing.T) {
	// Tasks are deleted at their TTL, so "no such task" is usually "you were
	// too slow" rather than "you made that up", and the message has to say
	// which.
	st := tasks.New()
	_, _, err := st.Result(context.Background(), "tsk-nothing")
	var missing tasks.ErrNoTask
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v", err)
	}
	if got := missing.Error(); got == "" || !strings.Contains(got, "expired") {
		t.Errorf("message = %q", got)
	}
}

func TestWaitingIsBoundedByTheCallersContext(t *testing.T) {
	st := tasks.New()
	task := st.Start(0, func(ctx context.Context) (any, *tasks.Fault) {
		<-ctx.Done()
		return nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := st.Result(ctx, task.TaskID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a caller that waited long enough should stop waiting, not the task: %v", err)
	}
	if snap, _ := st.Get(task.TaskID); snap.Status != tasks.Working {
		t.Errorf("the task keeps running: %q", snap.Status)
	}
}

func TestTaskPersistenceAcrossReopen(t *testing.T) {
	dbPath := t.TempDir() + "/tasks.db"
	st1, err := tasks.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// 1. Completed task
	t1 := st1.Start(60000, func(ctx context.Context) (any, *tasks.Fault) {
		return map[string]any{"data": "persisted-result"}, nil
	})
	// Wait for t1 to complete
	_, _, err = st1.Result(context.Background(), t1.TaskID)
	if err != nil {
		t.Fatalf("t1 result: %v", err)
	}

	// 2. Working task that was interrupted
	t2 := st1.Start(60000, func(ctx context.Context) (any, *tasks.Fault) {
		<-ctx.Done()
		return nil, nil
	})

	_ = st1.Close()

	// Reopen in new store instance
	st2, err := tasks.Open(dbPath)
	if err != nil {
		t.Fatalf("Open reopen failed: %v", err)
	}
	defer st2.Close()

	// t1 should still exist and be completed with its result
	snap1, ok := st2.Get(t1.TaskID)
	if !ok {
		t.Fatalf("t1 missing after reopen")
	}
	if snap1.Status != tasks.Completed {
		t.Errorf("t1 status = %q, want completed", snap1.Status)
	}
	res, fault, err := st2.Result(context.Background(), t1.TaskID)
	if err != nil || fault != nil {
		t.Errorf("t1 result err=%v fault=%v", err, fault)
	}
	if m, _ := res.(map[string]any); m["data"] != "persisted-result" {
		t.Errorf("t1 result data = %v", res)
	}

	// t2 (which was working when st1 was closed) should be marked failed due to daemon restart
	snap2, ok := st2.Get(t2.TaskID)
	if !ok {
		t.Fatalf("t2 missing after reopen")
	}
	if snap2.Status != tasks.Failed {
		t.Errorf("t2 status = %q, want failed", snap2.Status)
	}
	if !strings.Contains(snap2.StatusMessage, "daemon restarted") {
		t.Errorf("t2 statusMessage = %q", snap2.StatusMessage)
	}
}

func TestCooperativeCancellationHandshake(t *testing.T) {
	st := tasks.New()
	cooperativeCalled := false
	var cancelReason string

	task := st.Start(0, func(ctx context.Context) (any, *tasks.Fault) {
		<-ctx.Done()
		return nil, nil
	})

	st.SetTaskCancel(task.TaskID, func(ctx context.Context, reason string) {
		cooperativeCalled = true
		cancelReason = reason
	})

	snap, ok := st.CancelWithReason(task.TaskID, "user aborted run")
	if !ok || snap.Status != tasks.Cancelled {
		t.Fatalf("cancel returned %+v", snap)
	}
	if !cooperativeCalled {
		t.Error("expected cooperative cancel hook to be called")
	}
	if cancelReason != "user aborted run" {
		t.Errorf("expected reason 'user aborted run', got %q", cancelReason)
	}
}
