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
	upstream := 0
	for _, r := range res {
		m, _ := r.(map[string]any)
		u, _ := m["uri"].(string)
		if strings.HasPrefix(u, "skill://mcpx/") {
			continue // mcpx's own skills (SEP-2640), not the upstream's
		}
		upstream++
		if !strings.HasPrefix(u, "mcpx://demo/") || strings.Contains(u, "{id}") || u == "mcpx://demo/" {
			t.Errorf("resources/list entry %v is not a resource", m)
		}
	}
	if upstream == 0 {
		t.Fatalf("no upstream resources listed:\n%s", out)
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

// The optional fields an upstream publishes on its tools, resources,
// templates and prompts were dropped when mcpx parsed the list, so no host
// ever saw a title, size, annotation, icon or _meta (#207). And a 2025-03-26
// host must not be sent the ones its revision lacks.
func TestUpstreamListMetadataSurvives(t *testing.T) {
	e := newEnv(t, oneServer)
	lists := func(version string, args ...string) map[int]map[string]any {
		return replies(t, e.runStdin(strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version + `","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			`{"jsonrpc":"2.0","id":3,"method":"resources/list"}`,
			`{"jsonrpc":"2.0","id":4,"method":"resources/templates/list"}`,
			`{"jsonrpc":"2.0","id":5,"method":"prompts/list"}`,
		}, "\n")+"\n", append([]string{"serve"}, args...)...))
	}
	find := func(f map[string]any, key, field, want string) map[string]any {
		list, _ := f[key].([]any)
		for _, x := range list {
			m, _ := x.(map[string]any)
			if m[field] == want {
				return m
			}
		}
		t.Fatalf("%s has no %s=%s: %s", key, field, want, toJSON(f))
		return nil
	}

	f := lists("2025-11-25", "--passthrough", "demo")
	tool := find(f[2], "tools", "name", "structured")
	res := find(f[3], "resources", "uri", "demo://greeting")
	tmpl := find(f[4], "resourceTemplates", "uriTemplate", "demo://items/{id}")
	pr := find(f[5], "prompts", "name", "summarise")
	for name, item := range map[string]map[string]any{"tool": tool, "resource": res, "template": tmpl, "prompt": pr} {
		if item["title"] == nil || item["icons"] == nil || item["_meta"] == nil {
			t.Errorf("%s lost title, icons or _meta: %s", name, toJSON(item))
		}
	}
	if tool["outputSchema"] == nil {
		t.Errorf("tool lost outputSchema: %s", toJSON(tool))
	}
	if res["size"] != float64(21) || !strings.Contains(toJSON(res["annotations"]), "lastModified") {
		t.Errorf("resource lost size or annotations: %s", toJSON(res))
	}
	if !strings.Contains(toJSON(pr["arguments"]), `"title":"Text"`) {
		t.Errorf("prompt argument lost its title: %s", toJSON(pr))
	}

	// Namespaced, not pass-through: the same fields under mcpx's names.
	f = lists("2025-11-25")
	if r := find(f[3], "resources", "uri", "mcpx://demo/demo://greeting"); r["title"] != "Greeting" {
		t.Errorf("namespaced resource lost its title: %s", toJSON(r))
	}

	// 2025-03-26 defines none of title, icons or _meta on these, nor
	// lastModified; size and resource annotations are older and stay.
	f = lists("2025-03-26", "--passthrough", "demo")
	for name, item := range map[string]map[string]any{
		"tool":     find(f[2], "tools", "name", "structured"),
		"resource": find(f[3], "resources", "uri", "demo://greeting"),
		"template": find(f[4], "resourceTemplates", "uriTemplate", "demo://items/{id}"),
		"prompt":   find(f[5], "prompts", "name", "summarise"),
	} {
		s := toJSON(item)
		for _, k := range []string{`"title"`, `"icons"`, `"_meta"`, `"lastModified"`} {
			if strings.Contains(s, k) {
				t.Errorf("2025-03-26 %s carries %s: %s", name, k, s)
			}
		}
	}
	if r := find(f[3], "resources", "uri", "demo://greeting"); r["size"] != float64(21) || r["annotations"] == nil {
		t.Errorf("2025-03-26 defines size and resource annotations: %s", toJSON(r))
	}
}

// mcpx_call rendered every upstream result to one text block: images,
// resource links, structuredContent and _meta were lost, and prompts/get
// became one user message (#206). The namespaced route now carries the
// result as the pass-through route does, with resource URIs rewritten to
// ones /mcp can read, and downgrade() spelling it for an older host.
func TestNamespacedResultsArriveVerbatim(t *testing.T) {
	e := newEnv(t, oneServer)
	run := func(version string) map[int]map[string]any {
		return replies(t, e.runStdin(strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version + `","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcpx_call","arguments":{"namespace":"demo","tool":"structured"}}}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcpx_call","arguments":{"namespace":"demo","tool":"boom"}}}`,
			`{"jsonrpc":"2.0","id":4,"method":"prompts/get","params":{"name":"demo_summarise","arguments":{"text":"x"}}}`,
		}, "\n")+"\n", "serve"))
	}
	f := run("2025-11-25")
	res := f[2]
	if sc, _ := res["structuredContent"].(map[string]any); sc["n"] != float64(42) {
		t.Errorf("structuredContent lost: %s", toJSON(res))
	}
	if m, _ := res["_meta"].(map[string]any); m["example.com/k"] != "result" {
		t.Errorf("_meta lost: %s", toJSON(res))
	}
	s := toJSON(res["content"])
	if !strings.Contains(s, `"type":"image"`) || !strings.Contains(s, `"data":"iVBORw0KGgo="`) {
		t.Errorf("image lost: %s", s)
	}
	if !strings.Contains(s, `"type":"resource_link"`) || !strings.Contains(s, `"uri":"mcpx://demo/demo://greeting"`) {
		t.Errorf("resource_link lost or not readable through mcpx: %s", s)
	}
	if f[3]["isError"] != true || !strings.Contains(toJSON(f[3]), "deliberate failure") {
		t.Errorf("an upstream isError should arrive as isError with its text: %s", toJSON(f[3]))
	}
	if f[4]["description"] != "a summarisation prompt" || !strings.Contains(toJSON(f[4]), `"role":"user"`) ||
		!strings.Contains(toJSON(f[4]), "Summarise briefly: x") {
		t.Errorf("prompts/get should be the upstream's result: %s", toJSON(f[4]))
	}

	// 2025-03-26 has no structuredContent and no resource_link.
	f = run("2025-03-26")
	res = f[2]
	if _, present := res["structuredContent"]; present {
		t.Errorf("2025-03-26 has no structuredContent: %s", toJSON(res))
	}
	s = toJSON(res["content"])
	if strings.Contains(s, "resource_link") || !strings.Contains(s, `"type":"image"`) {
		t.Errorf("2025-03-26 content: %s", s)
	}
	// The link becomes an embedded resource whose body is a text label, so
	// its type is text/plain, not the image/png the link pointed at.
	if !strings.Contains(s, `"resource":{"mimeType":"text/plain","text":"greeting — mcpx://demo/demo://greeting","uri":"mcpx://demo/demo://greeting"}`) {
		t.Errorf("downgraded link: %s", s)
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
