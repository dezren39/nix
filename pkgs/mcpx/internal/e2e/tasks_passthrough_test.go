package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/testsupport"
)

// The daemon's /mcp fronting two upstreams in pass-through mode -- fakemcp
// and taskmcp, the tasks-extension fixture -- exactly as the conformance run
// fronts everything-server and taskmcp. End to end because each piece lives
// in a different layer: execution.taskSupport has to survive the upstream's
// tools/list, the daemon's schema cache and /v1/tools to reach the protocol
// layer at all, and an upstream's JSON-RPC error has to survive the daemon's
// error text.
func TestTasksExtensionThroughAPassThroughDaemon(t *testing.T) {
	e := newEnv(t, oneServer)
	cfg := fmt.Sprintf(`{"mcpServers":{"demo":{"command":%q,"mcpx":{"sharing":"shared","scope":"global"}},`+
		`"tasks":{"command":%q,"mcpx":{"sharing":"shared","scope":"global"}}}}`, e.fake, testsupport.TaskMCPBinary(t))
	if err := os.WriteFile(filepath.Join(e.dir, ".mcpx.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	e.envVars = append(e.envVars, "MCPX_MCP_PASSTHROUGH=demo,tasks")
	e.run("refresh")
	ep := e.endpoint(t)

	caps := map[string]any{"elicitation": map[string]any{"form": map[string]any{}},
		"extensions": map[string]any{"io.modelcontextprotocol/tasks": map[string]any{}}}
	id := 0
	rpc := func(method string, params map[string]any, caps map[string]any) map[string]any {
		t.Helper()
		id++
		params["_meta"] = map[string]any{
			"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
			"io.modelcontextprotocol/clientCapabilities": caps,
			"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "t", "version": "1"},
		}
		resp := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		defer resp.Body.Close()
		return decodeJSON(t, resp.Body)
	}
	result := func(m map[string]any) map[string]any {
		t.Helper()
		r, _ := m["result"].(map[string]any)
		if r == nil {
			t.Fatalf("no result: %s", dumpJSON(t, m))
		}
		return r
	}
	until := func(taskID string, ok func(map[string]any) bool) map[string]any {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			r := result(rpc("tasks/get", map[string]any{"taskId": taskID}, caps))
			if ok(r) {
				return r
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("task %s never got there", taskID)
		return nil
	}
	terminal := func(r map[string]any) bool {
		return r["status"] == "completed" || r["status"] == "failed" || r["status"] == "cancelled"
	}

	list := dumpJSON(t, rpc("tools/list", map[string]any{}, caps))
	for _, want := range []string{`"name":"echo"`, `"name":"slow_compute"`,
		`"execution":{"taskSupport":"optional"}`, `"execution":{"taskSupport":"required"}`} {
		if !strings.Contains(list, want) {
			t.Errorf("tools/list is missing %s:\n%s", want, list)
		}
	}

	// Required, from a client without the extension: -32021.
	m := rpc("tools/call", map[string]any{"name": "failing_job", "arguments": map[string]any{}}, map[string]any{})
	if e, _ := m["error"].(map[string]any); e == nil || e["code"] != float64(-32021) {
		t.Errorf("failing_job without the extension: want -32021, got %s", dumpJSON(t, m))
	}

	// A question asked after the call became a task parks it.
	r := result(rpc("tools/call", map[string]any{"name": "confirm_delete",
		"arguments": map[string]any{"filename": "x.txt"}}, caps))
	taskID, _ := r["taskId"].(string)
	if r["resultType"] != "task" || taskID == "" {
		t.Fatalf("confirm_delete: want a CreateTaskResult, got %v", r)
	}
	parked := until(taskID, func(r map[string]any) bool { return r["status"] == "input_required" || terminal(r) })
	reqs, _ := parked["inputRequests"].(map[string]any)
	if parked["status"] != "input_required" || len(reqs) != 1 {
		t.Fatalf("confirm_delete: want input_required with one inputRequest, got %v", parked)
	}
	answers := map[string]any{}
	for k := range reqs {
		answers[k] = map[string]any{"action": "accept", "content": map[string]any{"confirm": true}}
	}
	result(rpc("tasks/update", map[string]any{"taskId": taskID, "inputResponses": answers}, caps))
	done := until(taskID, terminal)
	if done["status"] != "completed" || !strings.Contains(dumpJSON(t, done), "deleted x.txt") {
		t.Errorf("confirm_delete after the answer: %v", done)
	}

	// An upstream's JSON-RPC error fails the task with that error.
	r = result(rpc("tools/call", map[string]any{"name": "protocol_error_job", "arguments": map[string]any{}}, caps))
	taskID, _ = r["taskId"].(string)
	if taskID == "" {
		t.Fatalf("protocol_error_job: want a task, got %v", r)
	}
	done = until(taskID, terminal)
	if e, _ := done["error"].(map[string]any); done["status"] != "failed" || e == nil || e["code"] != float64(-32603) {
		t.Errorf("protocol_error_job: want failed with -32603, got %v", done)
	}
}

// With several pass-through upstreams a bare resource URI goes to the one
// that lists it, not to whichever is named first; and two upstreams offering
// one prompt or tool name are refused rather than one silently shadowing
// the other.
func TestSeveralPassThroughUpstreamsRouteAndRefuse(t *testing.T) {
	e := newEnv(t, oneServer)
	cfg := fmt.Sprintf(`{"mcpServers":{"demo":{"command":%q,"mcpx":{"sharing":"shared","scope":"global"}},`+
		`"twin":{"command":%q,"mcpx":{"sharing":"shared","scope":"global"}},`+
		`"tasks":{"command":%q,"mcpx":{"sharing":"shared","scope":"global"}}}}`,
		e.fake, e.fake, testsupport.TaskMCPBinary(t))
	if err := os.WriteFile(filepath.Join(e.dir, ".mcpx.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

	// tasks lists no resources, so demo://greeting is demo's though tasks
	// is named first.
	f := replies(t, e.runStdin(init+"\n"+
		`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"demo://greeting"}}`+"\n",
		"serve", "--passthrough", "tasks,demo"))
	if !strings.Contains(toJSON(f[2]), "hello from a resource") {
		t.Errorf("a bare URI should reach the upstream that lists it: %v", f[2])
	}

	f = replies(t, e.runStdin(init+"\n"+
		`{"jsonrpc":"2.0","id":2,"method":"prompts/list"}`+"\n"+
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`+"\n",
		"serve", "--passthrough", "demo,twin"))
	for _, id := range []int{2, 3} {
		e, _ := f[id]["error"].(map[string]any)
		msg, _ := e["message"].(string)
		if !strings.Contains(msg, `"demo"`) || !strings.Contains(msg, `"twin"`) {
			t.Errorf("reply %d: want a collision error naming both upstreams, got %v", id, f[id])
		}
	}
}
