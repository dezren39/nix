package e2e_test

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// replies splits newline-delimited JSON-RPC replies by id.
func replies(t *testing.T, out string) map[int]map[string]any {
	t.Helper()
	got := map[int]map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		var f struct {
			ID     *int           `json:"id"`
			Result map[string]any `json:"result"`
			Error  map[string]any `json:"error"`
		}
		if json.Unmarshal([]byte(line), &f) != nil || f.ID == nil {
			continue
		}
		if f.Error != nil {
			got[*f.ID] = map[string]any{"error": f.Error}
			continue
		}
		got[*f.ID] = f.Result
	}
	return got
}

// A server's resources/list carries resources, never templates. The pool
// caches both in one slice, and the template -- which has no uri -- was
// listed as a resource with an empty one.
func TestResourcesListOmitsTemplates(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.runStdin(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`+"\n"+
			`{"jsonrpc":"2.0","id":3,"method":"resources/templates/list"}`+"\n",
		"serve")
	f := replies(t, out)
	res, _ := f[2]["resources"].([]any)
	if len(res) == 0 {
		t.Fatalf("no resources listed:\n%s", out)
	}
	for _, r := range res {
		m, _ := r.(map[string]any)
		u, _ := m["uri"].(string)
		if !strings.HasPrefix(u, "mcpx://demo/") || strings.Contains(u, "{id}") || u == "mcpx://demo/" {
			t.Errorf("resources/list entry %v is not a resource", m)
		}
	}
	if !strings.Contains(out, "demo://items/{id}") {
		t.Errorf("the template belongs in resources/templates/list:\n%s", out)
	}
}

// mcp.passthrough serves one upstream under its own names: bare tool and
// prompt names, the upstream's own resource URIs, and its results verbatim.
func TestPassthroughExposesOneUpstreamUnrenamed(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.runStdin(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"structured","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"summarise","arguments":{"text":"x"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":8,"method":"resources/read","params":{"uri":"demo://greeting"}}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"mcpx_status","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"no_such_tool","arguments":{}}}`,
	}, "\n")+"\n", "serve", "--passthrough", "demo")
	f := replies(t, out)

	names := map[string]bool{}
	tools, _ := f[2]["tools"].([]any)
	for _, x := range tools {
		m, _ := x.(map[string]any)
		n, _ := m["name"].(string)
		names[n] = true
	}
	if !names["echo"] || !names["structured"] || !names["mcpx_call"] {
		t.Errorf("tools/list should carry the upstream's own names and the gateway's: %v", names)
	}
	if sc, _ := f[3]["structuredContent"].(map[string]any); sc["n"] != float64(42) {
		t.Errorf("the upstream result should arrive verbatim, structuredContent included: %v", f[3])
	}
	if f[4]["isError"] != true {
		t.Errorf("an upstream tool error should stay isError: %v", f[4])
	}
	if !strings.Contains(toJSON(f[5]), `"name":"summarise"`) {
		t.Errorf("prompts should be listed unprefixed: %v", f[5])
	}
	if !strings.Contains(toJSON(f[6]), "Summarise") || f[6]["description"] != "a summarisation prompt" {
		t.Errorf("prompts/get should be the upstream's result verbatim: %v", f[6])
	}
	if !strings.Contains(toJSON(f[7]), `"uri":"demo://greeting"`) {
		t.Errorf("resources should keep their own URIs: %v", f[7])
	}
	if !strings.Contains(toJSON(f[8]), "hello from a resource") || !strings.Contains(toJSON(f[8]), `"uri":"demo://greeting"`) {
		t.Errorf("an unrenamed URI should read: %v", f[8])
	}
	if f[9]["isError"] == true || f[9]["content"] == nil {
		t.Errorf("gateway tools without a collision stay callable: %v", f[9])
	}
	if e, _ := f[10]["error"].(map[string]any); e == nil || e["code"] != float64(-32602) {
		t.Errorf("an unknown tool is still -32602: %v", f[10])
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// A pass-through tool that asks a question puts it to the client that called
// it, as mcpx_call does. It was left to the broker, where no client could
// answer, and the call hung until it timed out.
func TestPassthroughToolQuestionsReachTheClient(t *testing.T) {
	e := askEnv(t)
	e.run("refresh")
	cmd := exec.Command(e.mcpx, "serve", "--passthrough", "ask")
	cmd.Dir = e.dir
	cmd.Env = e.envVars
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	send := func(v any) {
		b, _ := json.Marshal(v)
		if _, err := stdin.Write(append(b, '\n')); err != nil {
			t.Errorf("write: %v", err)
		}
	}
	dec := json.NewDecoder(bufio.NewReader(stdout))
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{"elicitation": map[string]any{}}}})
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "need_repo", "arguments": map[string]any{}}})
	asked := false
	done := make(chan map[string]any, 1)
	go func() {
		for {
			var f map[string]any
			if err := dec.Decode(&f); err != nil {
				close(done)
				return
			}
			if f["method"] == "elicitation/create" {
				asked = true
				send(map[string]any{"jsonrpc": "2.0", "id": f["id"], "result": map[string]any{
					"action": "accept", "content": map[string]any{"repo": "me/pass"}}})
				continue
			}
			if id, _ := f["id"].(float64); id == 2 {
				done <- f
				return
			}
		}
	}()
	select {
	case f := <-done:
		if !asked {
			t.Fatalf("the client was never asked: %v", f)
		}
		if !strings.Contains(toJSON(f), "me/pass") {
			t.Errorf("the answer should reach the tool: %v", f)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the call hung: its question never reached the client")
	}
}

// resources/templates/list on a cold daemon reads the upstream first, as
// resources/list does. It read the daemon's cache as it stood, and the first
// client to ask got mcpx's own template and nothing from any server.
func TestTemplatesListedColdIncludeTheServers(t *testing.T) {
	e := newEnv(t, oneServer)
	// No startup warm, so nothing has read the server before the request.
	e.envVars = append(e.envVars, "MCPX_DAEMON_WARM=false")
	out := e.runStdin(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"resources/templates/list"}`+"\n",
		"serve")
	if !strings.Contains(out, "mcpx://demo/demo://items/{id}") {
		t.Errorf("the server's template should be listed on the first ask:\n%s", out)
	}
}
