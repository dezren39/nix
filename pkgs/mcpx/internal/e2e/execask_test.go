package e2e_test

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// Questions raised inside mcpx_exec reach the MCP client that ran the
// script, correlated through the script's run id (#77). Before, a script's
// upstream calls were not registered for correlation at all, so every
// question it provoked went to the broker and the client that could have
// answered it was never asked.

// A script calling two eliciting tools in parallel, through a legacy stdio
// client that declared elicitation: both questions arrive on the wire, and
// the result keeps the script's artifact as a resource_link.
func TestAScriptsQuestionsReachALegacyStdioClient(t *testing.T) {
	e := askEnv(t)
	e.run("refresh")

	cmd := exec.Command(e.mcpx, "serve")
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

	var wmu sync.Mutex
	send := func(v any) {
		b, _ := json.Marshal(v)
		wmu.Lock()
		defer wmu.Unlock()
		if _, err := stdin.Write(append(b, '\n')); err != nil {
			t.Errorf("write: %v", err)
		}
	}
	dec := json.NewDecoder(bufio.NewReaderSize(stdout, 1<<20))
	next := func() map[string]any {
		t.Helper()
		var f map[string]any
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("read: %v", err)
		}
		return f
	}

	// https://modelcontextprotocol.io/specification/2025-06-18/client/elicitation
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{"elicitation": map[string]any{}}}})
	next()

	src := `const [a, b] = await Promise.all([tools.ask.need_repo({}), tools.ask.need_repo({})]);
await artifact("note.txt", new Uint8Array([104, 105]));
console.log("got", a, "and", b);`
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "mcpx_exec",
			"arguments": map[string]any{"source": src}}})
	asked := 0
	var final map[string]any
	for final == nil {
		f := next()
		if f["method"] == "elicitation/create" {
			asked++
			send(map[string]any{"jsonrpc": "2.0", "id": f["id"],
				"result": map[string]any{"action": "accept",
					"content": map[string]any{"repo": "me/script"}}})
			continue
		}
		if f["id"] == float64(2) {
			final = f
		}
	}
	t.Run("2025-06-18/elicitation/exec-script-questions-sent-to-declaring-client", func(t *testing.T) {
		if asked != 2 {
			t.Fatalf("the client should have been asked twice, was asked %d times:\n%s",
				asked, dumpJSON(t, final))
		}
	})
	body := dumpJSON(t, final)
	t.Run("2025-06-18/elicitation/exec-script-answers-reach-the-script", func(t *testing.T) {
		if strings.Count(body, "using me/script") != 2 {
			t.Fatalf("both answers should have reached the script:\n%s", body)
		}
		if strings.Contains(body, `"isError":true`) {
			t.Fatalf("the script failed:\n%s", body)
		}
	})
	t.Run("2025-06-18/tools/exec-inline-result-keeps-resource-link", func(t *testing.T) {
		if !strings.Contains(body, `"resource_link"`) || !strings.Contains(body, "note.txt") {
			t.Fatalf("the artifact's resource_link was lost on the interruptible path:\n%s", body)
		}
	})
}

// A modern client answers by sending the request again. A script that asks
// twice, one after the other, takes two input_required round trips on the
// same call, and the script keeps running across both.
func TestAScriptsQuestionsReachAModernClientAsInputRequired(t *testing.T) {
	e := askEnv(t)
	e.run("refresh")
	ep := e.endpoint(t)

	meta := map[string]any{
		"io.modelcontextprotocol/protocolVersion": "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{
			"elicitation": map[string]any{"form": map[string]any{}}},
		"io.modelcontextprotocol/clientInfo": map[string]any{"name": "probe", "version": "1"},
	}
	disc := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 1,
		"method": "server/discover", "params": map[string]any{"_meta": meta}})
	session := disc.Header.Get("Mcp-Session-Id")
	disc.Body.Close()

	src := `const a = await tools.ask.need_repo({});
const b = await tools.ask.need_repo({});
console.log("first", a, "second", b);`
	params := map[string]any{"name": "mcpx_exec",
		"arguments": map[string]any{"source": src}, "_meta": meta}

	rounds := 0
	var last map[string]any
	for id := 2; id < 10; id++ {
		resp := mcpPost(t, ep, session, map[string]any{"jsonrpc": "2.0", "id": id,
			"method": "tools/call", "params": params})
		last = decodeJSON(t, resp.Body)
		resp.Body.Close()
		result, _ := last["result"].(map[string]any)
		if result == nil || result["resultType"] != "input_required" {
			break
		}
		rounds++
		requests, _ := result["inputRequests"].(map[string]any)
		responses := map[string]any{}
		for qid := range requests {
			responses[qid] = map[string]any{"action": "accept",
				"content": map[string]any{"repo": "me/round" + string(rune('0'+rounds))}}
		}
		params["inputResponses"] = responses
		params["requestState"] = result["requestState"]
	}
	body := dumpJSON(t, last)
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/mrtr
	t.Run("2026-07-28/input-required/exec-script-questions-returned-as-input-required", func(t *testing.T) {
		if rounds != 2 {
			t.Fatalf("expected two input_required rounds, got %d:\n%s", rounds, body)
		}
	})
	t.Run("2026-07-28/input-required/exec-script-resumes-across-rounds", func(t *testing.T) {
		if !strings.Contains(body, "using me/round1") || !strings.Contains(body, "using me/round2") {
			t.Fatalf("each round's answer should have reached the script:\n%s", body)
		}
		if !strings.Contains(body, `"resultType":"complete"`) {
			t.Errorf("the final result must be complete:\n%s", body)
		}
	})
}
