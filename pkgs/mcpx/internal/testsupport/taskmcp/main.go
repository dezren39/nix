// Command taskmcp is a stdio MCP server whose tools are the fixtures the
// official conformance suite's tasks-extension scenarios ask for
// (src/scenarios/server/tasks/*.ts in modelcontextprotocol/conformance).
//
// The suite's own reference server, everything-server.ts, does not define
// them; each SDK's conformance server does. This is mcpx's, versioned with it,
// and fronted by mcpx in pass-through mode next to everything-server (see
// scripts/conformance.sh). It speaks 2025-11-25 and knows nothing of tasks:
// whether a call becomes a task is mcpx's decision, made from each tool's
// execution.taskSupport. What the upstream contributes is a tool that takes a
// while, fails, or asks a question.
//
//	greet               no taskSupport. "Hello, {name}!" at once.
//	slow_compute        optional. Sleeps `seconds`, then reports `label`.
//	failing_job         required. After ~1s returns a tool error (isError).
//	protocol_error_job  optional. After ~1s answers with a JSON-RPC error.
//	confirm_delete      optional. Works for a second, then elicits a
//	                    confirmation for `filename`.
//	multi_input         optional. Works for a second, then elicits two
//	                    things at once.
//	test_tool_with_task required. Elicits user_name at once, then works
//	                    for a second and greets the name it was given.
//
// The delays before a question are what make the difference the scenarios
// test, seen from a gateway that cannot see into the tool: a question asked
// at once is answered inline on the original request (MRTR), one asked after
// the call has become a task parks the task in input_required.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type server struct {
	out     *bufio.Writer
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *frame
	// running lets notifications/cancelled stop a sleeping call.
	running map[string]chan struct{}
}

// work is how long the tools that "work for a second" work.
const work = time.Second

func main() {
	s := &server{out: bufio.NewWriter(os.Stdout), pending: map[int64]chan *frame{},
		running: map[string]chan struct{}{}}
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	var wg sync.WaitGroup
	for {
		line, err := readLine(in)
		if err != nil {
			wg.Wait()
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var f frame
		if json.Unmarshal(line, &f) != nil {
			continue
		}
		if f.Method == "" && f.ID != nil {
			s.deliver(&f)
			continue
		}
		wg.Add(1)
		go func(f frame) {
			defer wg.Done()
			if resp := s.handle(f); resp != nil {
				s.send(resp)
			}
		}(f)
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if !isPrefix {
			return buf, nil
		}
	}
}

func (s *server) send(v any) {
	b, _ := json.Marshal(v)
	s.writeMu.Lock()
	s.out.Write(b)
	s.out.WriteByte('\n')
	s.out.Flush()
	s.writeMu.Unlock()
}

func (s *server) deliver(f *frame) {
	s.mu.Lock()
	ch := s.pending[*f.ID]
	delete(s.pending, *f.ID)
	s.mu.Unlock()
	if ch != nil {
		ch <- f
	}
}

// ask sends the client a request and waits for its answer.
func (s *server) ask(method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID + 1000
	ch := make(chan *frame, 1)
	s.pending[id] = ch
	s.mu.Unlock()

	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	reply := <-ch
	if len(reply.Error) > 0 {
		return nil, fmt.Errorf("client error: %s", reply.Error)
	}
	return reply.Result, nil
}

func ok(id *int64, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func fail(id *int64, code int, msg string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": msg}}
}

func text(s string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}}
}

func obj(props map[string]any) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	return map[string]any{"type": "object", "properties": props}
}

func tool(name, desc, support string, props map[string]any) map[string]any {
	t := map[string]any{"name": name, "description": desc, "inputSchema": obj(props)}
	if support != "" {
		t["execution"] = map[string]any{"taskSupport": support}
	}
	return t
}

func (s *server) handle(f frame) map[string]any {
	switch f.Method {
	case "initialize":
		return ok(f.ID, map[string]any{
			"protocolVersion": "2025-11-25",
			"serverInfo":      map[string]any{"name": "taskmcp", "version": "1.0.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		})
	case "notifications/initialized":
		return nil
	case "notifications/cancelled":
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		_ = json.Unmarshal(f.Params, &p)
		s.mu.Lock()
		if ch, ok := s.running[string(p.RequestID)]; ok {
			close(ch)
			delete(s.running, string(p.RequestID))
		}
		s.mu.Unlock()
		return nil
	case "ping":
		return ok(f.ID, map[string]any{})
	case "tools/list":
		str := map[string]any{"type": "string"}
		return ok(f.ID, map[string]any{"tools": []map[string]any{
			tool("greet", "Says hello.", "", map[string]any{"name": str}),
			tool("slow_compute", "Sleeps for `seconds`, then reports `label`.", "optional",
				map[string]any{"seconds": map[string]any{"type": "number"}, "label": str}),
			tool("failing_job", "Fails with a tool error after about a second.", "required", nil),
			tool("protocol_error_job", "Fails with a JSON-RPC error.", "optional", nil),
			tool("confirm_delete", "Asks to confirm deleting `filename`.", "optional",
				map[string]any{"filename": str}),
			tool("multi_input", "Asks two questions at once.", "optional", nil),
			tool("test_tool_with_task", "Asks for a name, then works, then greets it.", "required", nil),
			// SEP-2243: the suite's custom-header scenario needs a tool whose
			// string parameter is mirrored into Mcp-Param-Region.
			tool("echo_region", "Echoes `region`, which is mirrored into the Mcp-Param-Region header.", "",
				map[string]any{"region": map[string]any{"type": "string", "x-mcp-header": "Region"}}),
		}})
	case "tools/call":
		return s.call(f)
	}
	return fail(f.ID, -32601, "method not found: "+f.Method)
}

// sleep waits d, or until the call is cancelled; false means cancelled.
func (s *server) sleep(id *int64, d time.Duration) bool {
	ch := make(chan struct{})
	key := ""
	if id != nil {
		key = fmt.Sprint(*id)
		s.mu.Lock()
		s.running[key] = ch
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.running, key)
			s.mu.Unlock()
		}()
	}
	select {
	case <-time.After(d):
		return true
	case <-ch:
		return false
	}
}

type elicitResult struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content"`
}

func (s *server) elicit(message string, props map[string]any) (elicitResult, error) {
	raw, err := s.ask("elicitation/create", map[string]any{
		"message":         message,
		"requestedSchema": obj(props),
	})
	var e elicitResult
	if err == nil {
		err = json.Unmarshal(raw, &e)
	}
	return e, err
}

func (s *server) call(f frame) map[string]any {
	var p struct {
		Name      string `json:"name"`
		Arguments struct {
			Name     string  `json:"name"`
			Seconds  float64 `json:"seconds"`
			Label    string  `json:"label"`
			Filename string  `json:"filename"`
			Region   string  `json:"region"`
		} `json:"arguments"`
	}
	_ = json.Unmarshal(f.Params, &p)
	a := p.Arguments
	switch p.Name {
	case "greet":
		return ok(f.ID, text("Hello, "+a.Name+"!"))
	case "echo_region":
		return ok(f.ID, text("region "+a.Region))
	case "slow_compute":
		if !s.sleep(f.ID, time.Duration(a.Seconds*float64(time.Second))) {
			return fail(f.ID, -32800, "cancelled")
		}
		return ok(f.ID, text(fmt.Sprintf("computed %s after %gs", a.Label, a.Seconds)))
	case "failing_job":
		s.sleep(f.ID, work)
		r := text("failing_job failed, as it always does")
		r["isError"] = true
		return ok(f.ID, r)
	case "protocol_error_job":
		// After a second, like failing_job: an error at once is answered
		// in line, which the extension permits, and the scenario wants
		// the failed-task path.
		s.sleep(f.ID, work)
		return fail(f.ID, -32603, "protocol_error_job: internal error, as designed")
	case "confirm_delete":
		if !s.sleep(f.ID, work) {
			return fail(f.ID, -32800, "cancelled")
		}
		e, err := s.elicit("Delete "+a.Filename+"?", map[string]any{"confirm": map[string]any{"type": "boolean"}})
		if err != nil {
			return fail(f.ID, -32603, err.Error())
		}
		if e.Action != "accept" || e.Content["confirm"] != true {
			return ok(f.ID, text("kept "+a.Filename))
		}
		return ok(f.ID, text("deleted "+a.Filename))
	case "multi_input":
		if !s.sleep(f.ID, work) {
			return fail(f.ID, -32800, "cancelled")
		}
		var wg sync.WaitGroup
		got := make([]string, 2)
		errs := make([]error, 2)
		for i, q := range []string{"First name?", "Second name?"} {
			wg.Add(1)
			go func(i int, q string) {
				defer wg.Done()
				e, err := s.elicit(q, map[string]any{"name": map[string]any{"type": "string"}})
				errs[i] = err
				got[i] = fmt.Sprint(e.Content["name"])
			}(i, q)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return fail(f.ID, -32603, err.Error())
			}
		}
		return ok(f.ID, text("got "+strings.Join(got, " and ")))
	case "test_tool_with_task":
		e, err := s.elicit("What is your name?", map[string]any{"name": map[string]any{"type": "string"}})
		if err != nil {
			return fail(f.ID, -32603, err.Error())
		}
		if !s.sleep(f.ID, work) {
			return fail(f.ID, -32800, "cancelled")
		}
		return ok(f.ID, text(fmt.Sprintf("Hello, %v!", e.Content["name"])))
	}
	return fail(f.ID, -32602, "unknown tool "+p.Name)
}
