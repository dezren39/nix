package e2e_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/testsupport"
)

// askEnv is an installation whose one server asks questions back.
//
// fakemcp answers everything itself, which is what every other test here
// wants and useless for this one: nothing there ever elicits.
func askEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, oneServer)
	ask := testsupport.AskMCPBinary(t)
	cfg := fmt.Sprintf(`{"mcpServers":{"ask":{"command":%q}}}`, ask)
	if err := os.WriteFile(filepath.Join(e.dir, ".mcpx.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

// endpoint is the daemon's loopback base URL, which is where /mcp lives.
func (e *env) endpoint(t *testing.T) string {
	t.Helper()
	var st struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatal(err)
	}
	if st.Endpoint == "" {
		t.Fatal("status did not report an endpoint")
	}
	return st.Endpoint
}

// mcpPost sends one frame to the daemon's MCP endpoint.
func mcpPost(t *testing.T, endpoint, session string, frame any) *http.Response {
	t.Helper()
	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint+"/mcp", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	mirrorModernHeaders(req, b)
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// mirrorModernHeaders sets the headers a 2026-07-28 POST MUST carry, from
// its body, as a conforming client would. A legacy frame gets none.
func mirrorModernHeaders(req *http.Request, body []byte) {
	var f struct {
		Method string `json:"method"`
		Params struct {
			Name string         `json:"name"`
			URI  string         `json:"uri"`
			Meta map[string]any `json:"_meta"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &f) != nil {
		return
	}
	v, _ := f.Params.Meta["io.modelcontextprotocol/protocolVersion"].(string)
	if v == "" {
		return
	}
	req.Header.Set("MCP-Protocol-Version", v)
	req.Header.Set("Mcp-Method", f.Method)
	switch f.Method {
	case "tools/call", "prompts/get":
		req.Header.Set("Mcp-Name", f.Params.Name)
	case "resources/read":
		req.Header.Set("Mcp-Name", f.Params.URI)
	}
}

func dumpJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decodeJSON(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestALegacyClientIsAskedOverStreamableHTTP is the hard half of the
// feature.
//
// A legacy client expects a genuine mid-flight request, and Streamable HTTP
// has no connection to send one on: the POST is the only channel, and it is
// the thing waiting for the answer. So the response becomes an event stream
// carrying the question, and the client's answer arrives on a second POST
// keyed by the session. Without that, the only legacy transport that could
// elicit was stdio.
func TestALegacyClientIsAskedOverStreamableHTTP(t *testing.T) {
	e := askEnv(t)
	e.run("refresh")
	ep := e.endpoint(t)

	init := mcpPost(t, ep, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]any{"elicitation": map[string]any{"form": map[string]any{}}},
		},
	})
	session := init.Header.Get("Mcp-Session-Id")
	init.Body.Close()
	if session == "" {
		t.Fatal("a legacy client needs a session, or its answer has nowhere to go")
	}

	resp := mcpPost(t, ep, session, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "mcpx_call",
			"arguments": map[string]any{"namespace": "ask", "tool": "need_repo"}},
	})
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("a request that has to ask something must stream, got %q", ct)
	}

	var asked, final map[string]any
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		switch frame["method"] {
		case "elicitation/create":
			asked = frame
			answer := map[string]any{"jsonrpc": "2.0", "id": frame["id"],
				"result": map[string]any{"action": "accept",
					"content": map[string]any{"repo": "me/thing"}}}
			// On a separate POST, which is the whole reason a session
			// exists: this connection is busy being the answer to the
			// first request.
			go func() { mcpPost(t, ep, session, answer).Body.Close() }()
		default:
			if frame["result"] != nil {
				final = frame
			}
		}
		if final != nil {
			break
		}
	}
	if asked == nil {
		t.Fatal("the client was never asked; the question went to the broker instead")
	}
	params, _ := asked["params"].(map[string]any)
	msg, _ := params["message"].(string)
	if !strings.Contains(msg, "ask") {
		t.Errorf("a client must be told which server is asking, got %q", msg)
	}
	if final == nil {
		t.Fatal("the stream ended without the result of the original call")
	}
	if body := dumpJSON(t, final); !strings.Contains(body, "me/thing") {
		t.Errorf("the answer should have reached the server:\n%s", body)
	}
}

// TestAModernClientIsHandedTheQuestionInTheResult is the other half.
//
// A modern server has no connection to send a request on, so it answers
// "not yet" and expects the whole request again. The call it names has to
// still be running when that happens, which is why it is a task.
func TestAModernClientIsHandedTheQuestionInTheResult(t *testing.T) {
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
	issued := disc.Header.Get("Mcp-Session-Id")
	disc.Body.Close()
	if issued != "" {
		// 2026-07-28 has no sessions. One used to be minted here only so a
		// requestState had something to be bound to; it is bound to the
		// request now, and the retry below works without any.
		t.Errorf("server/discover issued a session: %s", issued)
	}
	session := ""

	params := map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "ask", "tool": "need_repo"},
		"_meta":     meta}

	resp := mcpPost(t, ep, session, map[string]any{"jsonrpc": "2.0", "id": 2,
		"method": "tools/call", "params": params})
	first := decodeJSON(t, resp.Body)
	resp.Body.Close()
	result, _ := first["result"].(map[string]any)
	if result == nil || result["resultType"] != "input_required" {
		t.Fatalf("a modern client is asked by being answered:\n%s", dumpJSON(t, first))
	}
	state, _ := result["requestState"].(string)
	if state == "" {
		t.Fatal("without a requestState the server cannot know which call is resumed")
	}
	requests, _ := result["inputRequests"].(map[string]any)
	if len(requests) != 1 {
		t.Fatalf("expected one question:\n%s", dumpJSON(t, result))
	}
	responses := map[string]any{}
	for id, raw := range requests {
		one, _ := raw.(map[string]any)
		if one["method"] != "elicitation/create" {
			t.Errorf("unexpected question: %v", one["method"])
		}
		responses[id] = map[string]any{"action": "accept",
			"content": map[string]any{"repo": "me/modern"}}
	}

	params["inputResponses"] = responses
	params["requestState"] = state
	resp = mcpPost(t, ep, session, map[string]any{"jsonrpc": "2.0", "id": 3,
		"method": "tools/call", "params": params})
	second := decodeJSON(t, resp.Body)
	resp.Body.Close()
	body := dumpJSON(t, second)
	if !strings.Contains(body, "me/modern") {
		t.Fatalf("the retry should carry the original call through:\n%s", body)
	}
	if !strings.Contains(body, `"resultType":"complete"`) {
		t.Errorf("2026-07-28 makes resultType mandatory on every result:\n%s", body)
	}
}

// A requestState is opaque to the client, and has to be more than a handle:
// the MRTR page says a server SHOULD bind it to the originating request -- the
// method and its salient parameters -- and reject it on any other. A valid
// one presented on a different call is somebody steering a call they did not
// start. It used to be bound to an Mcp-Session-Id, which 2026-07-28 does not
// have.
func TestARequestStateOnAnotherRequestIsRefused(t *testing.T) {
	e := askEnv(t)
	e.run("refresh")
	ep := e.endpoint(t)
	meta := map[string]any{
		"io.modelcontextprotocol/protocolVersion": "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{
			"elicitation": map[string]any{}},
	}
	params := map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "ask", "tool": "need_repo"},
		"_meta":     meta}
	r := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 2,
		"method": "tools/call", "params": params})
	first := decodeJSON(t, r.Body)
	r.Body.Close()
	result, _ := first["result"].(map[string]any)
	state, _ := result["requestState"].(string)
	if state == "" {
		t.Fatalf("expected an input_required:\n%s", dumpJSON(t, first))
	}

	other := map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "ask", "tool": "need_repo", "arguments": map[string]any{"x": 1}},
		"_meta":     meta, "requestState": state, "inputResponses": map[string]any{}}
	r = mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 3,
		"method": "tools/call", "params": other})
	stolen := decodeJSON(t, r.Body)
	r.Body.Close()
	if stolen["error"] == nil {
		t.Fatalf("a requestState was accepted on a request it was not issued for:\n%s", dumpJSON(t, stolen))
	}
}

// TestAStdioClientIsAskedOnTheWire covers the transport an MCP host actually
// spawns, and sampling as well as elicitation: the two are the same shape
// seen from different angles, and mcpx must not confuse them.
func TestAStdioClientIsAskedOnTheWire(t *testing.T) {
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

	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{
				"elicitation": map[string]any{}, "sampling": map[string]any{}}}})
	next()

	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "mcpx_call",
			"arguments": map[string]any{"namespace": "ask", "tool": "need_model"}}})
	sampled := false
	for {
		f := next()
		if f["method"] == "sampling/createMessage" {
			sampled = true
			send(map[string]any{"jsonrpc": "2.0", "id": f["id"],
				"result": map[string]any{"role": "assistant", "model": "test-model",
					"content": map[string]any{"type": "text", "text": "banana"}}})
			continue
		}
		if body := dumpJSON(t, f); !strings.Contains(body, "banana") {
			t.Errorf("the model's answer should have reached the server:\n%s", body)
		}
		break
	}
	if !sampled {
		t.Fatal("a client that declared sampling should have been asked")
	}

	// Declining is not cancelling, and the server has to hear the
	// difference: one means offer an alternative, the other means ask again
	// later.
	send(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "mcpx_call",
			"arguments": map[string]any{"namespace": "ask", "tool": "need_repo"}}})
	for {
		f := next()
		if f["method"] == "elicitation/create" {
			params, _ := f["params"].(map[string]any)
			if _, has := params["mode"]; has {
				t.Error("2025-06-18 has no mode field; a strict client rejects the request over it")
			}
			send(map[string]any{"jsonrpc": "2.0", "id": f["id"],
				"result": map[string]any{"action": "decline"}})
			continue
		}
		if body := dumpJSON(t, f); !strings.Contains(body, "decline") {
			t.Errorf("the server should have been told it was declined, not cancelled:\n%s", body)
		}
		break
	}
}

// A client that declared nothing keeps the behaviour that worked before any
// of this existed: the question is stored, routed, and answerable from
// anywhere -- including a different process, minutes later.
func TestAClientThatDeclaredNothingStillReachesTheBroker(t *testing.T) {
	e := askEnv(t)
	e.run("refresh")
	ep := e.endpoint(t)

	type outcome struct {
		body string
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1,
			"method": "tools/call", "params": map[string]any{"name": "mcpx_call",
				"arguments": map[string]any{"namespace": "ask", "tool": "need_repo"}}})
		resp, err := (&http.Client{Timeout: 3 * time.Minute}).Post(
			ep+"/mcp", "application/json", strings.NewReader(string(b)))
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		done <- outcome{body: string(raw), err: err}
	}()

	// The question is state with a deadline, so it can be found by anything
	// that can reach the daemon -- here, a second process.
	var id string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && id == "" {
		listed, _ := e.try("--json", "elicit", "list")
		var pending []struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(jsonOf(t, listed)), &pending) == nil {
			for _, p := range pending {
				if strings.Contains(p.Message, "repository") {
					id = p.ID
				}
			}
		}
		if id == "" {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if id == "" {
		t.Fatal("a client that declared nothing should leave the question in the broker")
	}
	e.run("elicit", "answer", id, "repo=me/broker")

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if !strings.Contains(got.body, "me/broker") {
			t.Fatalf("the answer should have resumed the call:\n%s", got.body)
		}
	case <-time.After(time.Minute):
		t.Fatal("the call never finished")
	}
}

// Completion was answered from cache with upstream:false because nothing
// exposed a raw request, not because the server could not complete.
func TestCompletionComesFromTheServerAndSaysSo(t *testing.T) {
	e := askEnv(t)
	e.run("refresh")
	client := e.socketClient(t)

	resp, err := client.Post("http://mcpx/v1/complete", "application/json", strings.NewReader(
		`{"server":"ask","ref":{"type":"ref/prompt","name":"confirmed"},"argument":{"name":"x","value":""}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := decodeJSON(t, resp.Body)
	text := dumpJSON(t, body)
	if body["upstream"] != true || body["source"] != "upstream" {
		t.Fatalf("the server declares completions and should have been asked:\n%s", text)
	}
	if !strings.Contains(text, "from-upstream-a") {
		t.Fatalf("mcpx could not have guessed these values, so they prove who answered:\n%s", text)
	}
}

// A server that never declared completions is not asked, and the answer says
// which of the two you are looking at -- because a client that cannot tell an
// empty list from an unimplemented method shows nothing and the user
// concludes the feature is broken.
func TestCompletionFallsBackToTheCacheAndSaysWhy(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("refresh")
	client := e.socketClient(t)

	resp, err := client.Post("http://mcpx/v1/complete", "application/json", strings.NewReader(
		`{"server":"demo","ref":{"type":"ref/prompt","name":"s"},"argument":{"name":"x","value":"sum"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := decodeJSON(t, resp.Body)
	text := dumpJSON(t, body)
	if body["upstream"] != false || body["source"] != "cache" {
		t.Fatalf("fakemcp declares no completions capability:\n%s", text)
	}
	if !strings.Contains(text, "did not declare") {
		t.Errorf("the reason should be in the answer:\n%s", text)
	}
	if !strings.Contains(text, "summarise") {
		t.Errorf("the cached names are the floor, and should still be offered:\n%s", text)
	}
}

// One server, not two. Everything `mcpx serve --transport http` used to own
// is on the daemon's own listeners now, under one /v1.
func TestTheDaemonServesMCPAndTheRoutesTheSecondServerOwned(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("refresh")
	ep := e.endpoint(t)
	client := &http.Client{Timeout: 30 * time.Second}

	// MCP itself.
	resp := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 1,
		"method": "tools/list", "params": map[string]any{}})
	body := dumpJSON(t, decodeJSON(t, resp.Body))
	resp.Body.Close()
	if !strings.Contains(body, "mcpx_catalog") {
		t.Fatalf("the daemon should serve MCP on its own listeners:\n%s", body)
	}

	// One upstream tool, by path.
	r, err := client.Post(ep+"/v1/call/demo/echo", "application/json",
		strings.NewReader(`{"message":"through the daemon"}`))
	if err != nil {
		t.Fatal(err)
	}
	called := dumpJSON(t, decodeJSON(t, r.Body))
	r.Body.Close()
	if !strings.Contains(called, "through the daemon") {
		t.Fatalf("POST /v1/call/{server}/{tool} should reach the tool:\n%s", called)
	}

	// One of mcpx's own tools, as a plain POST.
	r, err = client.Post(ep+"/v1/tools/mcpx_namespaces", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	invoked := dumpJSON(t, decodeJSON(t, r.Body))
	r.Body.Close()
	if !strings.Contains(invoked, "demo") {
		t.Fatalf("POST /v1/tools/{tool} should run the tool:\n%s", invoked)
	}

	// And the document describes the per-tool paths, which only the second
	// server used to publish.
	r, err = client.Get(ep + "/v1/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	doc := dumpJSON(t, decodeJSON(t, r.Body))
	r.Body.Close()
	if !strings.Contains(doc, "/v1/call/demo/echo") {
		t.Errorf("the specification should describe every tool path:\n%s", firstN(doc, 400))
	}
}

// `serve --transport http` is gone, and the error names what replaced it.
// A command that fails without saying where the thing went is a command that
// costs somebody an afternoon.
func TestServeOverHTTPNamesTheDaemonInstead(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("serve", "--transport", "http")
	if err == nil {
		t.Fatalf("there is one HTTP server now:\n%s", out)
	}
	for _, want := range []string{"daemon", "/mcp"} {
		if !strings.Contains(out, want) {
			t.Errorf("the error should say where MCP over HTTP lives, got:\n%s", out)
		}
	}
}

func TestTheProtocolMatrixIsServedFromTheTableTheCodeReads(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("refresh")
	client := e.socketClient(t)
	resp, err := client.Get("http://mcpx/v1/protocol")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := decodeJSON(t, resp.Body)
	text := dumpJSON(t, body)
	for _, want := range []string{"2026-07-28", "2025-03-26", "structuredContent", "elicitationURL"} {
		if !strings.Contains(text, want) {
			t.Errorf("the matrix should mention %s:\n%s", want, firstN(text, 400))
		}
	}
	asClient, _ := body["asClient"].(map[string]any)
	servers, _ := asClient["servers"].([]any)
	if len(servers) == 0 {
		t.Fatalf("what each configured server actually settled on is the half that cannot be guessed:\n%s", text)
	}
	row, _ := servers[0].(map[string]any)
	if row["era"] != "legacy" {
		t.Errorf("fakemcp shakes hands, so it is legacy: %v", row["era"])
	}
}

// The MCP tool for an interruptible call has to exist, because the parity
// rule is that everything /v1 offers is reachable from MCP too.
func TestTheInterruptibleCallIsReachableFromMCP(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("serve", "--tools")
	for _, want := range []string{"mcpx_ask_begin", "mcpx_ask_poll", "mcpx_ask_answers", "mcpx_protocol"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s should be an MCP tool:\n%s", want, firstN(out, 400))
		}
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --mcp-spec / MCPX_MCP_SPEC and spec.lenient reach the daemon's revision
// policy, which /v1/protocol reports as it is in effect (#307).
func TestTheDaemonReportsItsRevisionPolicy(t *testing.T) {
	e := newEnv(t, oneServer)
	e.setenv("MCPX_MCP_SPEC=2024-11-05", "MCPX_SPEC_LENIENT=2026-07-28")
	e.run("refresh")
	resp, err := e.socketClient(t).Get("http://mcpx/v1/protocol")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := decodeJSON(t, resp.Body)
	sp, _ := body["spec"].(map[string]any)
	order, _ := sp["precedence"].([]any)
	strict, _ := sp["strict"].(map[string]any)
	if len(order) != 5 || order[0] != "2024-11-05" || order[1] != "2026-07-28" {
		t.Errorf("precedence %v, want 2024-11-05 then the rest newest first", order)
	}
	if strict["2026-07-28"] != false || strict["2024-11-05"] != true {
		t.Errorf("strict %v, want 2026-07-28 lenient and the rest strict", strict)
	}
}
