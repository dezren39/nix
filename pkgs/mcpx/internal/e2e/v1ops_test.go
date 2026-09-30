package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/api"
)

// getJSON reads one /v1 route over the daemon's socket.
func getJSON(t *testing.T, c *http.Client, path string) map[string]any {
	t.Helper()
	resp, err := c.Get("http://mcpx" + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, resp.StatusCode, b)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, b)
	}
	return doc
}

func postJSON(t *testing.T, c *http.Client, path, body string) (int, map[string]any) {
	t.Helper()
	resp, err := c.Post("http://mcpx"+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	return resp.StatusCode, doc
}

func TestTheLogIsQueryableOverTheAPI(t *testing.T) {
	// The CLI reads the log through logstore.Query; so does this. One query
	// builder, or the two surfaces disagree about what "--since 1h --level
	// warn" selects and only one of them is ever tested.
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"queryable"}`)
	c := e.socketClient(t)

	doc := getJSON(t, c, "/v1/log?event=mcp.call&limit=5")
	recs, _ := doc["records"].([]any)
	if len(recs) == 0 {
		t.Fatalf("the call should be in the log: %v", doc)
	}
	first, _ := recs[0].(map[string]any)
	attrs, _ := first["attrs"].(map[string]any)
	if attrs["tool"] != "echo" {
		t.Errorf("record = %v", first)
	}
	if _, ok := first["level"].(string); !ok {
		t.Errorf("the level should be a name, not a number: %v", first["level"])
	}

	// A filter that matches nothing is an empty list, not an error.
	empty := getJSON(t, c, "/v1/log?server=nothing-here")
	if recs, _ := empty["records"].([]any); len(recs) != 0 {
		t.Errorf("records = %v", recs)
	}

	// An unreadable window is a 400 with the reason in it, not an empty
	// answer that reads as "nothing happened".
	resp, err := c.Get("http://mcpx/v1/log?since=yesterday")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestTheLogChainIsATreeOverTheAPI(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"traced"}`)
	c := e.socketClient(t)

	doc := getJSON(t, c, "/v1/log?event=mcp.call&limit=1")
	recs, _ := doc["records"].([]any)
	if len(recs) == 0 {
		t.Fatal("no call recorded")
	}
	attrs, _ := recs[0].(map[string]any)["attrs"].(map[string]any)
	trace, _ := attrs["trace"].(string)
	if trace == "" {
		t.Fatalf("the call should carry a trace: %v", attrs)
	}
	chain := getJSON(t, c, "/v1/log?chain="+trace)
	levels, _ := chain["chain"].([]any)
	if len(levels) == 0 {
		t.Fatalf("the chain should hold at least the trace itself: %v", chain)
	}
}

func TestStatsOverTheAPIMirrorTheCommand(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"counted"}`)
	c := e.socketClient(t)

	doc := getJSON(t, c, "/v1/stats?by=calls")
	if doc["dimension"] != "calls" {
		t.Errorf("dimension = %v", doc["dimension"])
	}
	rows, _ := doc["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("the call should be aggregated: %v", doc)
	}
	row, _ := rows[0].(map[string]any)
	if row["Server"] != "demo" && row["server"] != "demo" {
		t.Errorf("row = %v", row)
	}

	// Every dimension answers, because a dimension that only the CLI can
	// reach is a gap nobody notices until they look for it.
	for _, dim := range []string{"servers", "instances", "errors", "sessions", "volume", "slowest"} {
		if doc := getJSON(t, c, "/v1/stats?by="+dim); doc["dimension"] != dim {
			t.Errorf("%s: %v", dim, doc)
		}
	}
	resp, err := c.Get("http://mcpx/v1/stats?by=nonsense")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown dimension should be refused: %d", resp.StatusCode)
	}
}

func TestRegistrySearchOverTheAPIFailsWithoutANetworkRatherThanHanging(t *testing.T) {
	// The public registry is not reachable from a test, and should not be.
	// What is worth pinning is that the route exists, that it validates, and
	// that a failure comes back as a status rather than as a timeout.
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)
	resp, err := c.Get("http://mcpx/v1/registry/search?q=weather")
	if err != nil {
		t.Fatalf("the route should answer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestCompletionOverTheAPI(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("prompts")
	c := e.socketClient(t)

	code, doc := postJSON(t, c, "/v1/complete",
		`{"server":"demo","ref":{"type":"ref/prompt","name":"summarise"},"argument":{"name":"style","value":""}}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, doc)
	}
	comp, _ := doc["completion"].(map[string]any)
	if comp == nil {
		t.Fatalf("a completion reply always carries values: %v", doc)
	}
	if _, ok := comp["values"].([]any); !ok {
		t.Errorf("values should be a list: %v", comp)
	}
	// Whether the answer came from the server or from what mcpx knows is
	// said in band, because a client that cannot tell an empty answer from
	// an unimplemented one shows nothing and the user blames completion.
	if _, ok := doc["upstream"].(bool); !ok {
		t.Errorf("the answer should say where it came from: %v", doc)
	}

	if code, _ := postJSON(t, c, "/v1/complete", `{"server":"demo","ref":{"type":"nonsense"}}`); code != http.StatusBadRequest {
		t.Errorf("an unknown ref type should be refused: %d", code)
	}
	if code, _ := postJSON(t, c, "/v1/complete", `{"server":"nope","ref":{"type":"ref/prompt"}}`); code != http.StatusBadRequest {
		t.Errorf("an unknown server should be refused: %d", code)
	}
}

func TestALongCallCanBeRunAsATask(t *testing.T) {
	// The reason tasks exist: a call that takes minutes should not hold a
	// request open for minutes, because every intermediary between the two
	// ends has an opinion about how long that is allowed to be.
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)

	code, doc := postJSON(t, c, "/v1/call",
		`{"server":"demo","tool":"echo","args":{"message":"backgrounded"},"task":{}}`)
	if code != http.StatusAccepted {
		t.Fatalf("a task request is accepted, not answered: %d %v", code, doc)
	}
	task, _ := doc["task"].(map[string]any)
	id, _ := task["taskId"].(string)
	if id == "" {
		t.Fatalf("no handle came back: %v", doc)
	}

	list := getJSON(t, c, "/v1/tasks")
	if tasks, _ := list["tasks"].([]any); len(tasks) == 0 {
		t.Errorf("the task should be listed: %v", list)
	}
	if got := getJSON(t, c, "/v1/tasks/"+id); got["taskId"] != id {
		t.Errorf("task = %v", got)
	}

	result := getJSON(t, c, "/v1/tasks/"+id+"/result")
	b, _ := json.Marshal(result)
	if !strings.Contains(string(b), "backgrounded") {
		t.Errorf("the result should be the call's: %s", b)
	}

	// An unknown handle is a 404 that says it may have expired, rather than
	// an empty result that reads as success.
	resp, err := c.Get("http://mcpx/v1/tasks/tsk-nothing")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestATaskCanBeCancelled(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)

	// A tool slow enough to still be running when the cancellation
	// arrives. With `echo` this was a race the test won almost always and
	// not quite: the call finished first, Cancel found a terminal status,
	// and the assertion below read "completed" for reasons that had nothing
	// to do with cancellation.
	_, doc := postJSON(t, c, "/v1/call",
		`{"server":"demo","tool":"slow","args":{"ms":5000},"task":{}}`)
	task, _ := doc["task"].(map[string]any)
	id, _ := task["taskId"].(string)
	if id == "" {
		t.Fatalf("no handle: %v", doc)
	}
	code, cancelled := postJSON(t, c, "/v1/tasks/"+id+"/cancel", `{}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, cancelled)
	}
	if cancelled["status"] != "cancelled" {
		t.Errorf("status = %v", cancelled["status"])
	}
	// A cancellation stands even if the work finished in the meantime.
	if got := getJSON(t, c, "/v1/tasks/"+id); got["status"] != "cancelled" {
		t.Errorf("status = %v", got["status"])
	}
}

func TestTheDaemonPublishesItsOwnOpenAPIDocument(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)

	doc := getJSON(t, c, "/v1/openapi.json")
	paths, _ := doc["paths"].(map[string]any)
	for _, op := range api.Ops() {
		item, _ := paths[op.Path].(map[string]any)
		if item == nil {
			t.Errorf("%s is missing from the document", op.Path)
			continue
		}
		if _, ok := item[strings.ToLower(op.Method)]; !ok {
			t.Errorf("%s %s is missing from the document", op.Method, op.Path)
		}
	}
}

func TestEveryDeclaredRouteAnswersOverTheSocket(t *testing.T) {
	// The parity test proves a route is declared; this proves it is served.
	// A declaration matching a route that 404s is a specification that lies.
	e := newEnv(t, oneServer)
	e.run("ls")
	c := e.socketClient(t)

	for _, op := range api.Ops() {
		if op.Streams || op.Name == "shutdown" || op.Name == "restart" || op.Name == "refresh" {
			// Streaming never ends; the other three would take the daemon
			// out from under the rest of this test.
			continue
		}
		args := map[string]any{}
		for _, p := range op.Params {
			if !p.Required {
				continue
			}
			switch p.Name {
			case "id":
				args[p.Name] = "tsk-nothing"
			case "action":
				args[p.Name] = "cancel"
			case "server":
				args[p.Name] = "demo"
			case "tool":
				args[p.Name] = "echo"
			case "name":
				args[p.Name] = "summarise"
			case "uri":
				args[p.Name] = "demo://greeting"
			case "q":
				args[p.Name] = "x"
			case "ref":
				args[p.Name] = map[string]any{"type": "ref/prompt", "name": "summarise"}
			case "argument":
				args[p.Name] = map[string]any{"name": "style", "value": ""}
			case "record":
				args[p.Name] = map[string]any{"msg": "parity probe"}
			default:
				args[p.Name] = "x"
			}
		}
		if op.Name == "task_result" {
			// Otherwise this waits out the full result timeout on a handle
			// that does not exist.
			args["waitMs"] = 100
		}
		path, body, err := op.Request(args)
		if err != nil {
			t.Errorf("%s: %v", op.Name, err)
			continue
		}
		var resp *http.Response
		if op.Method == http.MethodGet {
			resp, err = c.Get("http://mcpx" + path)
		} else {
			resp, err = c.Post("http://mcpx"+path, "application/json", strings.NewReader(string(body)))
		}
		if err != nil {
			t.Errorf("%s %s: %v", op.Method, path, err)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound && strings.Contains(string(b), "404 page not found") {
			t.Errorf("%s: %s %s is declared but not routed", op.Name, op.Method, path)
		}
		if resp.StatusCode >= 500 && resp.StatusCode != http.StatusBadGateway &&
			resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s: %s %s answered %d: %s", op.Name, op.Method, path, resp.StatusCode, b)
		}
	}
}

func TestEveryV1OperationIsReachableAsAnMCPTool(t *testing.T) {
	// The end of the chain the parity test starts: not just that a tool with
	// the right name exists, but that calling it reaches the daemon and
	// brings the JSON back.
	e := newEnv(t, oneServer)
	e.run("ls")

	var in strings.Builder
	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n")
	fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcpx_health","arguments":{}}}`+"\n")
	fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mcpx_log_query","arguments":{"limit":3}}}`+"\n")
	fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"mcpx_stats_query","arguments":{"by":"calls"}}}`+"\n")
	fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"mcpx_tasks_list","arguments":{}}}`+"\n")
	fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"mcpx_task_get","arguments":{}}}`+"\n")
	out := e.runStdin(in.String(), "serve", "--mcp-page-size", "200")

	if !strings.Contains(out, `uptime`) {
		t.Errorf("mcpx_health should return the daemon's health:\n%s", out)
	}
	if !strings.Contains(out, `\"records\"`) {
		t.Errorf("mcpx_log_query should return records:\n%s", out)
	}
	if !strings.Contains(out, `\"dimension\"`) {
		t.Errorf("mcpx_stats_query should return an aggregate:\n%s", out)
	}
	if !strings.Contains(out, `\"tasks\"`) {
		t.Errorf("mcpx_tasks_list should return the task list:\n%s", out)
	}
	// A missing path parameter is refused by the proxy rather than sent as a
	// literal brace, and refusal is a tool error rather than a protocol one.
	if !strings.Contains(out, "id is required") {
		t.Errorf("mcpx_task_get without an id should say so:\n%s", out)
	}
	// Privileged operations are offered, and say that they are.
	for _, want := range []string{"mcpx_shutdown", "mcpx_restart", "mcpx_log_record", "Privileged"} {
		if !strings.Contains(out, want) {
			t.Errorf("tools/list should carry %s:\n%s", want, out)
		}
	}
	if !strings.Contains(out, `"destructiveHint":true`) {
		t.Errorf("a destructive tool should be annotated as one:\n%s", out)
	}
}

func TestTheProxyToolsReachAServerTool(t *testing.T) {
	// One round trip through the generic proxy that changes something,
	// rather than only reads: the argument mapping for a body is the half
	// that a query-only test would not exercise.
	e := newEnv(t, oneServer)
	e.run("ls")
	out := e.runStdin(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcpx_log_record","arguments":{"level":"warn","record":{"msg":"through the proxy","event":"proxy.probe"}}}}`+"\n",
		"serve")
	if !strings.Contains(out, `ok\": true`) {
		t.Fatalf("the record should be accepted:\n%s", out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		logged := e.run("--json", "log", "--grep", "through the proxy", "--limit", "5")
		if strings.Contains(logged, "through the proxy") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the proxied record never reached the log:\n%s", logged)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
