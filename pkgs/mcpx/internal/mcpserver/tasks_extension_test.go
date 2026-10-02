package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// The 2026-07-28 tasks extension as the official conformance suite's
// src/scenarios/server/tasks/*.ts drives it, against a pass-through upstream
// whose tools declare execution.taskSupport. Each test names the scenario
// check it stands for.

// taskUpstream is a pass-through backend with several upstreams, each tool
// carrying the taskSupport given.
type taskUpstream struct {
	*fakeBackend
	tools map[string][]mcpserver.Tool // by namespace
	// callErr is what Call returns, for the direct path.
	callErr error
	delay   time.Duration
	mu      sync.Mutex
	calls   []string
}

func (u *taskUpstream) UpstreamTools(_ context.Context, ns string) ([]mcpserver.Tool, error) {
	return u.tools[ns], nil
}

func (u *taskUpstream) Call(ctx context.Context, ns, tool string, args json.RawMessage) (string, error) {
	u.mu.Lock()
	u.calls = append(u.calls, ns+"."+tool)
	u.mu.Unlock()
	time.Sleep(u.delay)
	if u.callErr != nil {
		return "", u.callErr
	}
	return u.fakeBackend.Call(ctx, ns, tool, args)
}

func supportTool(name, support string) mcpserver.Tool {
	t := mcpserver.Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)}
	if support != "" {
		t.Execution = json.RawMessage(`{"taskSupport":"` + support + `"}`)
	}
	return t
}

func taskServer(u *taskUpstream, pass ...string) *mcpserver.Server {
	s := mcpserver.New(u, "mcpx", "test")
	s.Passthrough = pass
	return s
}

// callTool is a 2026-07-28 tools/call with the given capabilities.
func callTool(caps, name string) map[string]any {
	return modernWith(caps, map[string]any{"name": name, "arguments": map[string]any{}})
}

const extCaps = `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`

// extAskCaps is the suite's tasks client: it declares the extension and can
// answer questions inline.
const extAskCaps = `{"elicitation":{"form":{}},"extensions":{"io.modelcontextprotocol/tasks":{}}}`

func waitTask(t *testing.T, s *mcpserver.Server, id string, until func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r := resultOf(t, handle(t, s, "tasks/get", taskParams(map[string]any{"taskId": id})))
		if until(r) {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s never reached the state wanted", id)
	return nil
}

func terminal(r map[string]any) bool {
	st := r["status"]
	return st == "completed" || st == "failed" || st == "cancelled"
}

// mcp.passthrough naming two upstreams offers both under their own names
// and routes each call to the one that owns the tool. The conformance run
// fronts the suite's everything-server and mcpx's task fixture this way.
func TestPassThroughMergesSeveralUpstreams(t *testing.T) {
	u := &taskUpstream{fakeBackend: newBackend(), tools: map[string][]mcpserver.Tool{
		"demo":  {supportTool("echo", "")},
		"tasks": {supportTool("greet", "")},
	}}
	s := taskServer(u, "demo", "tasks")
	list := protoJSON(t, handle(t, s, "tools/list", map[string]any{}))
	for _, name := range []string{`"echo"`, `"greet"`} {
		if !strings.Contains(list, name) {
			t.Errorf("tools/list is missing %s: %s", name, list)
		}
	}
	for tool, ns := range map[string]string{"echo": "demo", "greet": "tasks"} {
		m := handle(t, s, "tools/call", map[string]any{"name": tool, "arguments": map[string]any{}})
		if !strings.Contains(protoJSON(t, m), "called "+ns+"."+tool) {
			t.Errorf("%s did not reach %s: %v", tool, ns, m)
		}
	}
}

// Two upstreams offering one name is refused, not resolved: whichever won,
// the other's tool would be unreachable under the name the client was shown.
func TestPassThroughRefusesAToolNameTwoUpstreamsOffer(t *testing.T) {
	u := &taskUpstream{fakeBackend: newBackend(), tools: map[string][]mcpserver.Tool{
		"a": {supportTool("greet", "")},
		"b": {supportTool("greet", "")},
	}}
	s := taskServer(u, "a", "b")
	m := handle(t, s, "tools/list", map[string]any{})
	if errCode(m) != -32603 {
		t.Fatalf("tools/list with a collision: want an error, got %v", m)
	}
	if msg := protoJSON(t, m); !strings.Contains(msg, `\"a\"`) || !strings.Contains(msg, `\"b\"`) ||
		!strings.Contains(msg, "greet") {
		t.Errorf("the error should name both upstreams and the tool: %s", msg)
	}
}

// The pass-through tools/list carries the upstream's execution object, which
// is how a client learns which tools run as tasks.
func TestPassThroughToolsListCarriesExecution(t *testing.T) {
	u := &taskUpstream{fakeBackend: newBackend(), tools: map[string][]mcpserver.Tool{
		"tasks": {supportTool("slow_compute", "optional")},
	}}
	s := taskServer(u, "tasks")
	list := protoJSON(t, handle(t, s, "tools/list", modernParams(nil)))
	if !strings.Contains(list, `"execution":{"taskSupport":"optional"}`) {
		t.Errorf("tools/list lost execution.taskSupport: %s", list)
	}
}

// tasks-required-task-error / sep-2663-server-returns-missing-capability-when-required:
// a taskSupport "required" tool from a client that did not declare the
// extension is -32021 naming it, and nothing runs.
func TestARequiredTaskToolWithoutTheExtensionIsMissingCapability(t *testing.T) {
	u := &taskUpstream{fakeBackend: newBackend(), tools: map[string][]mcpserver.Tool{
		"tasks": {supportTool("failing_job", "required")},
	}}
	s := taskServer(u, "tasks")
	m := handle(t, s, "tools/call", callTool(`{}`, "failing_job"))
	if errCode(m) != -32021 {
		t.Fatalf("want -32021, got %v", m)
	}
	if !strings.Contains(protoJSON(t, m), `"requiredCapabilities":{"extensions":{"io.modelcontextprotocol/tasks":{}}}`) {
		t.Errorf("error data should name the extension: %v", m)
	}
	if len(u.calls) != 0 {
		t.Errorf("the tool ran anyway: %v", u.calls)
	}
}

// tasks-lifecycle / sep-2663-result-type-task-on-create, and
// tasks-dispatch-and-envelope / tasks-server-directed-creation-no-hint: a
// task-supporting tool that takes a second becomes a task, even though the
// general threshold (TaskAfter) is longer than that.
func TestATaskSupportingToolBecomesATaskWithoutWaitingTaskAfter(t *testing.T) {
	u := &taskUpstream{fakeBackend: newBackend(), delay: 400 * time.Millisecond,
		tools: map[string][]mcpserver.Tool{"tasks": {supportTool("slow_compute", "optional"), supportTool("greet", "")}}}
	s := taskServer(u, "tasks")
	s.Timing.TaskAfter = 5 * time.Second
	s.Timing.TaskEager = 50 * time.Millisecond

	r := resultOf(t, handle(t, s, "tools/call", callTool(extCaps, "slow_compute")))
	if r["resultType"] != "task" || r["taskId"] == nil {
		t.Fatalf("want a CreateTaskResult, got %v", r)
	}
	done := waitTask(t, s, r["taskId"].(string), terminal)
	if done["status"] != "completed" || done["result"] == nil {
		t.Errorf("tasks/get at the end: %v", done)
	}

	// The control: a tool that declared nothing is still run in line.
	if r := resultOf(t, handle(t, s, "tools/call", callTool(extCaps, "greet"))); r["resultType"] != "complete" {
		t.Errorf("a tool without taskSupport was made a task: %v", r)
	}
}

// tasks-lifecycle / sep-2663-tasks-get-status-failed: an upstream that
// answers a pass-through call with a JSON-RPC error fails the task with that
// error -- and the direct path relays it as the error too, rather than as a
// tool result with isError, which is what a tool failure looks like.
func TestAnUpstreamProtocolErrorIsRelayedAsAnError(t *testing.T) {
	up := &mcpserver.UpstreamError{Code: -32603, Message: "as designed"}
	u := &taskUpstream{fakeBackend: newBackend(), callErr: up, delay: 200 * time.Millisecond,
		tools: map[string][]mcpserver.Tool{"tasks": {supportTool("protocol_error_job", "optional")}}}
	s := taskServer(u, "tasks")
	s.Timing.TaskEager = 50 * time.Millisecond

	r := resultOf(t, handle(t, s, "tools/call", callTool(extCaps, "protocol_error_job")))
	if r["taskId"] == nil {
		t.Fatalf("want a task, got %v", r)
	}
	done := waitTask(t, s, r["taskId"].(string), terminal)
	e, _ := done["error"].(map[string]any)
	if done["status"] != "failed" || e == nil || e["code"] != float64(-32603) || done["result"] != nil {
		t.Errorf("want failed with the upstream's error and no result, got %v", done)
	}

	m := handle(t, s, "tools/call", callTool(`{}`, "protocol_error_job"))
	if errCode(m) != -32603 {
		t.Errorf("direct path: want the upstream's -32603, got %v", m)
	}

	// The control: a gateway tool's failure stays a tool result.
	u.callErr = errors.New("mcp error -32603: boom")
	m = handle(t, s, "tools/call", modernParams(map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "x", "tool": "y"}}))
	if errCode(m) != 0 {
		t.Errorf("a gateway tool's failure became a protocol error: %v", m)
	}
}

// timedAsker is an upstream call that asks its questions askAfter into the
// call and finishes workAfter after the last answer.
//
// askAfter > 0 means "ask once the call is a task", and that is enforced, not
// left to the clock: the server's first Poll is the TaskEager window, and a
// question it sees there is rightly answered inline with no task. Timing alone
// lost that race under a loaded nix build -- the eager Poll checks for
// questions before its deadline, so a stalled scheduler let a question due at
// 200-300ms land inside a 100ms window, and the call came back input_required
// instead of a task (2026-10-02, two different tests). Withholding questions
// from that first Poll makes the outcome independent of machine load.
// askAfter == 0 still asks up front, inside the window.
type timedAsker struct {
	mu         sync.Mutex
	begun      time.Time
	polls      int
	askAfter   time.Duration
	workAfter  time.Duration
	qs         []mcpserver.Question
	answers    map[string]json.RawMessage
	answeredAt time.Time
	text       string
	upstream   *mcpserver.UpstreamError
	abandoned  bool
}

func (a *timedAsker) Begin(context.Context, string, json.RawMessage) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.begun, a.answers, a.polls = time.Now(), map[string]json.RawMessage{}, 0
	return "call-1", nil
}

func (a *timedAsker) state(eager bool) mcpserver.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Since(a.begun) < a.askAfter || (eager && a.askAfter > 0) {
		return mcpserver.Outcome{}
	}
	var open []mcpserver.Question
	for _, q := range a.qs {
		if _, ok := a.answers[q.ID]; !ok {
			open = append(open, q)
		}
	}
	if len(open) > 0 {
		return mcpserver.Outcome{Questions: open}
	}
	from := a.answeredAt
	if len(a.qs) == 0 {
		from = a.begun
	}
	if time.Since(from) >= a.workAfter {
		if a.upstream != nil {
			return mcpserver.Outcome{Done: true, IsError: true, Text: a.upstream.Error(), Upstream: a.upstream}
		}
		return mcpserver.Outcome{Done: true, Text: a.text}
	}
	return mcpserver.Outcome{}
}

func (a *timedAsker) Poll(ctx context.Context, _ string, wait time.Duration) (mcpserver.Outcome, error) {
	a.mu.Lock()
	a.polls++
	eager := a.polls == 1
	a.mu.Unlock()
	deadline := time.Now().Add(wait)
	for {
		out := a.state(eager)
		if out.Done || len(out.Questions) > 0 || time.Now().After(deadline) || ctx.Err() != nil {
			return out, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (a *timedAsker) Reply(_ context.Context, _ string, answers map[string]json.RawMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range answers {
		a.answers[k] = v
	}
	a.answeredAt = time.Now()
	return nil
}

func (a *timedAsker) Abandon(string) {
	a.mu.Lock()
	a.abandoned = true
	a.mu.Unlock()
}

func elicitQ(id string) mcpserver.Question {
	return mcpserver.Question{ID: id, Method: "elicitation/create", Mode: "form", Server: "tasks",
		Params: json.RawMessage(`{"message":"` + id + `?","requestedSchema":{"type":"object"}}`)}
}

func askingServer(a *timedAsker, name string) *mcpserver.Server {
	u := &taskUpstream{fakeBackend: newBackend(),
		tools: map[string][]mcpserver.Tool{"tasks": {supportTool(name, "optional")}}}
	s := taskServer(u, "tasks")
	s.Ask = a
	// TaskPoll is long so that the task's own loop cannot be what clears
	// an answered question before the next tasks/get: tasks/update must.
	s.Timing = mcpserver.Timing{TaskEager: 100 * time.Millisecond, AskPoll: 20 * time.Millisecond,
		TaskPoll: 500 * time.Millisecond}
	return s
}

// tasks-mrtr-input / sep-2663-tasks-get-status-input-required,
// tasks-mrtr-tasks-update-resumes and tasks-mrtr-partial-fulfillment: a
// task-supporting call from a client that can answer questions is still a
// task, and a question it asks after that parks the task in input_required
// with inputRequests, answered key by key through tasks/update.
func TestATaskAskingAQuestionIsInputRequiredAndTasksUpdateAnswersIt(t *testing.T) {
	a := &timedAsker{askAfter: 300 * time.Millisecond, workAfter: 50 * time.Millisecond,
		qs: []mcpserver.Question{elicitQ("first"), elicitQ("second")}, text: "got both"}
	s := askingServer(a, "multi_input")

	r := resultOf(t, handle(t, s, "tools/call", callTool(extAskCaps, "multi_input")))
	id, _ := r["taskId"].(string)
	if r["resultType"] != "task" || id == "" {
		t.Fatalf("want a CreateTaskResult, got %v", r)
	}
	parked := waitTask(t, s, id, func(r map[string]any) bool {
		reqs, _ := r["inputRequests"].(map[string]any)
		return r["status"] == "input_required" && len(reqs) == 2 || terminal(r)
	})
	reqs, _ := parked["inputRequests"].(map[string]any)
	if parked["status"] != "input_required" || len(reqs) != 2 {
		t.Fatalf("want input_required with two inputRequests, got %v", parked)
	}
	for k, v := range reqs {
		if v.(map[string]any)["method"] != "elicitation/create" {
			t.Errorf("inputRequests[%s] carries no method: %v", k, v)
		}
	}

	accept := json.RawMessage(`{"action":"accept","content":{"confirm":true}}`)
	ack := resultOf(t, handle(t, s, "tasks/update", taskParams(map[string]any{"taskId": id,
		"inputResponses": map[string]any{"first": accept, "not-asked": accept}})))
	for _, k := range []string{"taskId", "status", "inputRequests"} {
		if _, ok := ack[k]; ok {
			t.Errorf("tasks/update ack carries %s: %v", k, ack)
		}
	}
	after := resultOf(t, handle(t, s, "tasks/get", taskParams(map[string]any{"taskId": id})))
	left, _ := after["inputRequests"].(map[string]any)
	if after["status"] != "input_required" || len(left) != 1 || left["second"] == nil {
		t.Fatalf("after answering one of two: want input_required with only \"second\", got %v", after)
	}

	handle(t, s, "tasks/update", taskParams(map[string]any{"taskId": id,
		"inputResponses": map[string]any{"second": accept}}))
	done := waitTask(t, s, id, terminal)
	if done["status"] != "completed" || !strings.Contains(protoJSON(t, done), "got both") {
		t.Errorf("want completed with the call's result, got %v", done)
	}
	if _, ok := done["inputRequests"]; ok {
		t.Errorf("a finished task still offers inputRequests: %v", done)
	}
}

// tasks-mrtr-composition / sep-2663-mrtr-synchronous-before-task-creation: a
// question asked at once is answered inline (input_required on the request,
// no task), and the retry carrying the answer becomes the task.
func TestAQuestionAskedUpFrontIsAnsweredBeforeTheTaskExists(t *testing.T) {
	a := &timedAsker{workAfter: 400 * time.Millisecond, qs: []mcpserver.Question{elicitQ("user_name")},
		text: "Hello, Alice!"}
	s := askingServer(a, "test_tool_with_task")

	r1 := resultOf(t, handle(t, s, "tools/call", callTool(extAskCaps, "test_tool_with_task")))
	if r1["resultType"] != "input_required" || r1["taskId"] != nil {
		t.Fatalf("round 1: want input_required and no task, got %v", r1)
	}
	keys := r1["inputRequests"].(map[string]any)
	if keys["user_name"] == nil {
		t.Fatalf("round 1: want inputRequests[user_name], got %v", r1)
	}
	params := callTool(extAskCaps, "test_tool_with_task")
	params["requestState"] = r1["requestState"]
	params["inputResponses"] = map[string]any{"user_name": json.RawMessage(`{"action":"accept","content":{"name":"Alice"}}`)}
	r2 := resultOf(t, handle(t, s, "tools/call", params))
	if r2["resultType"] != "task" || r2["taskId"] == nil {
		t.Fatalf("round 2: want a CreateTaskResult, got %v", r2)
	}
	for _, k := range []string{"requestState", "inputRequests"} {
		if _, ok := r2[k]; ok {
			t.Errorf("round 2 CreateTaskResult carries %s: %v", k, r2)
		}
	}
	done := waitTask(t, s, r2["taskId"].(string), terminal)
	if done["status"] != "completed" || !strings.Contains(protoJSON(t, done), "Alice") {
		t.Errorf("want completed with the answer in the result, got %v", done)
	}
}

// The same through the Asker, which is the path a client that can answer
// questions takes.
func TestAnUpstreamProtocolErrorFailsAnAskedTask(t *testing.T) {
	a := &timedAsker{workAfter: 300 * time.Millisecond,
		upstream: &mcpserver.UpstreamError{Code: -32603, Message: "as designed"}}
	s := askingServer(a, "protocol_error_job")
	r := resultOf(t, handle(t, s, "tools/call", callTool(extAskCaps, "protocol_error_job")))
	if r["taskId"] == nil {
		t.Fatalf("want a task, got %v", r)
	}
	done := waitTask(t, s, r["taskId"].(string), terminal)
	e, _ := done["error"].(map[string]any)
	if done["status"] != "failed" || e == nil || e["code"] != float64(-32603) || done["result"] != nil {
		t.Errorf("want failed with the upstream's error and no result, got %v", done)
	}
}

// Cancelling an asked task abandons the call behind it: nobody is going to
// answer its question.
func TestCancellingAnAskedTaskAbandonsTheCall(t *testing.T) {
	// askAfter > 0 keeps the question out of the eager window (see
	// timedAsker), so this is a task however loaded the machine is. The
	// unchecked r["taskId"].(string) here used to panic when it was not.
	a := &timedAsker{askAfter: 200 * time.Millisecond, qs: []mcpserver.Question{elicitQ("confirm")}}
	s := askingServer(a, "confirm_delete")
	r := resultOf(t, handle(t, s, "tools/call", callTool(extAskCaps, "confirm_delete")))
	id, _ := r["taskId"].(string)
	if id == "" {
		t.Fatalf("want a task, got %v", r)
	}
	waitTask(t, s, id, func(r map[string]any) bool { return r["status"] == "input_required" })
	handle(t, s, "tasks/cancel", taskParams(map[string]any{"taskId": id}))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		gone := a.abandoned
		a.mu.Unlock()
		if gone {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the call behind a cancelled task was never abandoned")
}

// tasks-request-headers / sep-2663-server-rejects-mismatched-mcp-name-on-tasks-get:
// Mcp-Name on tasks/get mirrors the taskId, and one that contradicts it is
// -32020. One left out is tolerated: SEP-2243 does not name the tasks
// methods, and a client built to it alone does not know to send it.
func TestMcpNameOnTasksMethodsMustMatchTheTaskID(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	body := frame(7, "tasks/get", taskParams(map[string]any{"taskId": "tsk-nope"}))
	w := post(s, body, modernHeaders("tasks/get", "tsk-other"))
	if code, _ := rpcErr(t, w.Body.Bytes()); code != -32020 {
		t.Errorf("mismatched Mcp-Name: want -32020, got %d:\n%s", code, w.Body)
	}
	w = post(s, body, modernHeaders("tasks/get", "tsk-nope"))
	if code, _ := rpcErr(t, w.Body.Bytes()); code != -32602 {
		t.Errorf("matching Mcp-Name: want the unknown-task -32602, got %d:\n%s", code, w.Body)
	}
	w = post(s, body, modernHeaders("tasks/get", ""))
	if code, _ := rpcErr(t, w.Body.Bytes()); code != -32602 {
		t.Errorf("no Mcp-Name: want the unknown-task -32602, got %d:\n%s", code, w.Body)
	}
}
