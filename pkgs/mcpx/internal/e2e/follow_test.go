package e2e_test

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// protocol: follow gives a caller that reached mcpx in a legacy revision a
// legacy session of a dual-era upstream -- the only kind on which the
// upstream can send it requests -- and a modern caller the modern one. The
// caller's revision has to cross mcpx serve, /v1 and the pool for this to
// happen; eramcp says which of its sessions answered.
func TestFollowServesEachCallerOverItsOwnEra(t *testing.T) {
	call := func(e *env, first string) string {
		out := e.runStdin(first+"\n", "serve", "--passthrough", "era")
		return out
	}
	legacy := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello","arguments":{}}}`
	modern := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello","arguments":{},"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},` +
		`"io.modelcontextprotocol/clientInfo":{"name":"t","version":"1"}}}}`

	e, _ := eraEnv(t, "dual", "follow", "MCPX_UPSTREAM_PROBE_TIMEOUT=10s")
	if out := call(e, legacy); !strings.Contains(out, "hello from dual over legacy") {
		t.Errorf("a 2025-11-25 caller under follow should reach the legacy session:\n%s", out)
	}
	// A caller that can answer questions is served through /v1/ask, a task
	// that outlives its request; the revision has to survive that too.
	if out := call(e, strings.Replace(legacy, `"capabilities":{}`, `"capabilities":{"elicitation":{}}`, 1)); !strings.Contains(out, "hello from dual over legacy") {
		t.Errorf("a 2025-11-25 caller that answers elicitation should reach the legacy session too:\n%s", out)
	}
	if out := call(e, modern); !strings.Contains(out, "hello from dual over modern") {
		t.Errorf("a 2026-07-28 caller under follow should keep the modern session:\n%s", out)
	}

	m, _ := eraEnv(t, "dual", "modern", "MCPX_UPSTREAM_PROBE_TIMEOUT=10s")
	if out := call(m, legacy); !strings.Contains(out, "hello from dual over modern") {
		t.Errorf("without follow a legacy caller shares the modern session:\n%s", out)
	}
}

// The reverse bridge: a 2026-07-28 upstream asks by answering
// input_required, and a 2025-11-25 caller -- which has a session and no
// notion of input_required -- is sent the question as an elicitation/create
// request, and its answer goes back on the retried request.
func TestAModernUpstreamsQuestionReachesALegacyCallerAsARequest(t *testing.T) {
	e, _ := eraEnv(t, "modern", "")
	e.run("refresh")
	cmd := exec.Command(e.mcpx, "serve", "--passthrough", "era")
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
		"params": map[string]any{"protocolVersion": "2025-11-25",
			"capabilities": map[string]any{"elicitation": map[string]any{}},
			"clientInfo":   map[string]any{"name": "t", "version": "1"}}})
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "ask", "arguments": map[string]any{}}})
	asked := make(chan string, 1)
	done := make(chan map[string]any, 1)
	go func() {
		for {
			var f map[string]any
			if err := dec.Decode(&f); err != nil {
				close(done)
				return
			}
			if f["method"] == "elicitation/create" {
				asked <- toJSON(f["params"])
				send(map[string]any{"jsonrpc": "2.0", "id": f["id"], "result": map[string]any{
					"action": "accept", "content": map[string]any{"colour": "teal"}}})
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
		select {
		case q := <-asked:
			if !strings.Contains(q, "which colour?") {
				t.Errorf("the question arrived changed: %s", q)
			}
		default:
			t.Fatalf("the legacy caller was never sent elicitation/create; the call answered %s", toJSON(f))
		}
		if s := toJSON(f); !strings.Contains(s, `answered`) || !strings.Contains(s, "teal") {
			t.Errorf("the answer should reach the upstream on the retried request: %s", s)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the call did not finish")
	}
}
