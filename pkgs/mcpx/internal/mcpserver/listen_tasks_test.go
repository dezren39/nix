package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// SEP-2663, Task Status Notifications: a client that names a task in a
// subscriptions/listen's taskIds is told which it will hear about, then
// receives notifications/tasks carrying the DetailedTask tasks/get would
// answer -- through to the terminal state with its result inlined.
//
// This is the only test of this behaviour: the official conformance suite's
// tasks-status-notifications scenario returns SKIPPED unconditionally ("pending
// subscriptions/listen rewrite", src/scenarios/server/tasks/notifications.ts),
// so it is one of the four skipped checks listed in docs/spec/official-suite.md.
// When the suite is rewritten, that skip should become a pass; if it becomes a
// failure, this test and the suite disagree about the spec.
func TestListenDeliversTaskStatusNotifications(t *testing.T) {
	u := &taskUpstream{fakeBackend: newBackend(), delay: 300 * time.Millisecond,
		tools: map[string][]mcpserver.Tool{"tasks": {supportTool("slow_compute", "optional")}}}
	s := taskServer(u, "tasks")
	s.Timing.TaskEager = 20 * time.Millisecond

	r := resultOf(t, handle(t, s, "tools/call", callTool(extCaps, "slow_compute")))
	id, _ := r["taskId"].(string)
	if r["resultType"] != "task" || id == "" {
		t.Fatalf("premise: want a task, got %v", r)
	}

	rec := &recorder{}
	c := s.ConnWithSend("sess-tasks", rec.send)
	s.HandleOn(context.Background(), c, mcpserver.Request(9, "subscriptions/listen",
		taskParams(map[string]any{"notifications": map[string]any{"taskIds": []string{id, "tsk-unknown"}}})))

	ack := rec.wait(t, 1)[0]
	agreed, _ := ack["params"].(map[string]any)["notifications"].(map[string]any)
	ids, _ := agreed["taskIds"].([]any)
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("acknowledged taskIds %v; want exactly the known task %s", agreed["taskIds"], id)
	}

	var final map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for final == nil && time.Now().Before(deadline) {
		rec.mu.Lock()
		for _, f := range rec.frames[1:] {
			if f["method"] != "notifications/tasks" {
				t.Errorf("unexpected frame %v", f)
				continue
			}
			p, _ := f["params"].(map[string]any)
			if p["taskId"] != id {
				t.Errorf("notification for another task: %v", p)
			}
			meta, _ := p["_meta"].(map[string]any)
			if string(mustJSON(t, meta[mcpserver.MetaSubscriptionID])) != "9" {
				t.Errorf("not tagged with the subscription: %v", p)
			}
			if p["status"] == "completed" {
				final = p
			}
		}
		rec.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if final == nil {
		t.Fatalf("no completed notifications/tasks arrived: %v", rec.frames)
	}
	if final["result"] == nil {
		t.Errorf("the terminal notification does not inline the result: %v", final)
	}
}

// A task that already finished before the listen opened is still reported:
// the current state goes out at once, or the client would wait forever on a
// change that has already happened.
func TestListenReportsATaskThatAlreadyFinished(t *testing.T) {
	u := &taskUpstream{fakeBackend: newBackend(), delay: 100 * time.Millisecond,
		tools: map[string][]mcpserver.Tool{"tasks": {supportTool("slow_compute", "optional")}}}
	s := taskServer(u, "tasks")
	s.Timing.TaskEager = 10 * time.Millisecond
	r := resultOf(t, handle(t, s, "tools/call", callTool(extCaps, "slow_compute")))
	id, _ := r["taskId"].(string)
	waitTask(t, s, id, terminal)

	rec := &recorder{}
	c := s.ConnWithSend("sess-done", rec.send)
	s.HandleOn(context.Background(), c, mcpserver.Request(3, "subscriptions/listen",
		taskParams(map[string]any{"notifications": map[string]any{"taskIds": []string{id}}})))
	f := rec.wait(t, 2)[1]
	if f["method"] != "notifications/tasks" || f["params"].(map[string]any)["status"] != "completed" {
		t.Fatalf("%v", f)
	}
}

// In pass-through mode tools/list is the upstreams' list, so it changes when
// theirs does: tools.listChanged is declared and a listen agrees to
// toolsListChanged and delivers it. The fixed-list case, which must not, is
// TestToolsListChangedIsNeverDeclared.
func TestPassThroughToolsListChangedReachesListenStreams(t *testing.T) {
	n := &fakeNotifier{got: make(chan mcpserver.ListenFilter, 4), fire: make(chan [2]any, 4)}
	u := &taskUpstream{fakeBackend: newBackend(),
		tools: map[string][]mcpserver.Tool{"up": {supportTool("t", "")}}}
	s := taskServer(u, "up")
	s.Notify = n
	s.SetPush(func(string, any) {})

	b, _ := json.Marshal(s.Handle(context.Background(), mcpserver.Request(2, "server/discover", nil)))
	if !strings.Contains(string(b), `"tools":{"listChanged":true}`) {
		t.Fatalf("discover: %s", b)
	}
	b, _ = json.Marshal(s.Handle(context.Background(), mcpserver.Request(1, "initialize",
		map[string]any{"protocolVersion": "2025-11-25"})))
	if !strings.Contains(string(b), `"tools":{"listChanged":true}`) {
		t.Fatalf("initialize: %s", b)
	}

	rec := &recorder{}
	c := s.ConnWithSend("sess-tools", rec.send)
	s.HandleOn(context.Background(), c, mcpserver.Request(7, "subscriptions/listen",
		modernParams(map[string]any{"notifications": map[string]any{"toolsListChanged": true}})))
	ack := rec.wait(t, 1)[0]
	agreed, _ := ack["params"].(map[string]any)["notifications"].(map[string]any)
	if agreed["toolsListChanged"] != true {
		t.Fatalf("toolsListChanged not agreed: %v", ack)
	}
	select {
	case lf := <-n.got:
		if !lf.ToolsListChanged {
			t.Fatalf("the notifier was not asked for tool changes: %+v", lf)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notifier started")
	}
	n.fire <- [2]any{"notifications/tools/list_changed", map[string]any{}}
	if f := rec.wait(t, 2)[1]; f["method"] != "notifications/tools/list_changed" {
		t.Fatalf("%v", f)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
