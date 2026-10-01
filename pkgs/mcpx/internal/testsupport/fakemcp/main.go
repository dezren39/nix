// Command fakemcp is a stdio MCP server used by the mcpx test suite.
//
// It is deliberately stateful: `open` records a value and `state` reports
// everything this process has seen. That makes cross-instance leakage
// observable, which is exactly what the session-pool tests need to assert.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type req struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

var (
	mu    sync.Mutex
	state []string
	pid   = os.Getpid()

	// notify writes an unsolicited frame; set by main.
	notify func(map[string]any)
	// subscribed are the URIs a client subscribed to, in FAKEMCP_SUBSCRIBE
	// mode.
	subscribed = map[string]bool{}
)

// FAKEMCP_SUBSCRIBE declares resources.subscribe, lists an absolute-path
// resource beside the greeting, and while any resource is subscribed sends
// notifications/resources/updated for it every FAKEMCP_UPDATE_EVERY. Every
// subscribe and unsubscribe is appended to FAKEMCP_SUB_LOG, so a test can
// see what mcpx asked upstream rather than what it says it asked. Off by
// default, so no other test's capabilities or listings move.
var subscribeMode = os.Getenv("FAKEMCP_SUBSCRIBE") != ""

// absResource is the absolute-path URI subscribe mode lists: a listing
// drops its leading "/" when it namespaces it, which is the case #241 found
// unmatchable.
const absResource = "/abs/doc"

func subLog(line string) {
	path := os.Getenv("FAKEMCP_SUB_LOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "%d %s\n", pid, line)
	f.Close()
}

func emitUpdates() {
	every, err := time.ParseDuration(os.Getenv("FAKEMCP_UPDATE_EVERY"))
	if err != nil || every <= 0 {
		return
	}
	for range time.Tick(every) {
		mu.Lock()
		uris := make([]string, 0, len(subscribed))
		for u := range subscribed {
			uris = append(uris, u)
		}
		mu.Unlock()
		for _, u := range uris {
			notify(map[string]any{"jsonrpc": "2.0", "method": "notifications/resources/updated",
				"params": map[string]any{"uri": u}})
		}
	}
}

func main() {
	// FAKEMCP_FAIL_START makes the server exit before the handshake, so the
	// pool's start-failure path can be exercised.
	if os.Getenv("FAKEMCP_FAIL_START") != "" {
		fmt.Fprintln(os.Stderr, "fakemcp: refusing to start (FAKEMCP_FAIL_START)")
		os.Exit(3)
	}
	if d := os.Getenv("FAKEMCP_START_DELAY"); d != "" {
		if dur, err := time.ParseDuration(d); err == nil {
			time.Sleep(dur)
		}
	}
	// Servers commonly print a banner on stdout; mcpx must skip it.
	fmt.Println("fakemcp banner line, not JSON")

	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	out := bufio.NewWriter(os.Stdout)
	var writeMu sync.Mutex
	var wg sync.WaitGroup

	send := func(resp map[string]any) {
		b, _ := json.Marshal(resp)
		writeMu.Lock()
		out.Write(b)
		out.WriteByte('\n')
		out.Flush()
		writeMu.Unlock()
	}
	notify = send
	if subscribeMode {
		go emitUpdates()
	}

	for {
		line, err := readLine(in)
		if err != nil {
			wg.Wait()
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var r req
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		// Handle off the read loop, as a correct MCP server does: JSON-RPC ids
		// let a client have many requests in flight at once.
		wg.Add(1)
		go func(r req) {
			defer wg.Done()
			if resp := handle(r); resp != nil {
				send(resp)
			}
		}(r)
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

func ok(id *int64, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func fail(id *int64, code int, msg string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

func handle(r req) map[string]any {
	switch r.Method {
	case "initialize":
		resources := map[string]any{}
		if subscribeMode {
			resources["subscribe"] = true
		}
		return ok(r.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"serverInfo":      map[string]any{"name": "fakemcp", "version": "1.0.0"},
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": resources,
				"prompts":   map[string]any{},
			},
		})
	case "resources/subscribe", "resources/unsubscribe":
		if !subscribeMode {
			break
		}
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(r.Params, &p)
		mu.Lock()
		if r.Method == "resources/subscribe" {
			subscribed[p.URI] = true
		} else {
			delete(subscribed, p.URI)
		}
		mu.Unlock()
		subLog(strings.TrimPrefix(r.Method, "resources/") + " " + p.URI)
		return ok(r.ID, map[string]any{})
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		return ok(r.ID, map[string]any{})
	case "tools/list":
		return ok(r.ID, map[string]any{"tools": toolDefs()})
	case "resources/list":
		list := []any{
			map[string]any{
				"uri": "demo://greeting", "name": "greeting",
				"description": "a fixed greeting", "mimeType": "text/plain",
				// The optional fields, so a test can see they survive.
				"title": "Greeting", "size": 21,
				"annotations": map[string]any{"audience": []any{"user"}, "priority": 0.5,
					"lastModified": "2025-01-01T00:00:00Z"},
				"icons": []any{map[string]any{"src": "https://example.com/g.png"}},
				"_meta": map[string]any{"example.com/k": "resource"},
			},
		}
		if subscribeMode {
			list = append(list, map[string]any{"uri": absResource, "name": "abs",
				"description": "an absolute-path resource", "mimeType": "text/plain"})
		}
		return ok(r.ID, map[string]any{"resources": list})
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(r.Params, &p)
		if p.URI == "demo://logo" {
			// Binary, and deliberately unlisted so no listing test moves:
			// the eight bytes of a PNG signature, as a blob.
			return ok(r.ID, map[string]any{"contents": []any{
				map[string]any{"uri": p.URI, "mimeType": "image/png", "blob": "iVBORw0KGgo=",
					"_meta": map[string]any{"example.com/k": "contents"}},
			}})
		}
		if subscribeMode && p.URI == absResource {
			return ok(r.ID, map[string]any{"contents": []any{
				map[string]any{"uri": p.URI, "mimeType": "text/plain", "text": "an absolute-path resource"},
			}})
		}
		if p.URI != "demo://greeting" {
			return fail(r.ID, -32602, "no such resource: "+p.URI)
		}
		return ok(r.ID, map[string]any{"contents": []any{
			map[string]any{"uri": p.URI, "mimeType": "text/plain", "text": "hello from a resource"},
		}})
	case "prompts/list":
		return ok(r.ID, map[string]any{"prompts": []any{
			map[string]any{
				"name": "summarise", "description": "summarise some text",
				"title": "Summarise",
				"icons": []any{map[string]any{"src": "https://example.com/p.png"}},
				"_meta": map[string]any{"example.com/k": "prompt"},
				"arguments": []any{
					map[string]any{"name": "text", "title": "Text", "description": "what to summarise", "required": true},
					map[string]any{"name": "style", "description": "how"},
				},
			},
		}})
	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		_ = json.Unmarshal(r.Params, &p)
		if p.Name != "summarise" {
			return fail(r.ID, -32602, "no such prompt: "+p.Name)
		}
		style := p.Arguments["style"]
		if style == "" {
			style = "briefly"
		}
		return ok(r.ID, map[string]any{
			"description": "a summarisation prompt",
			"messages": []any{map[string]any{
				"role": "user",
				"content": map[string]any{
					"type": "text",
					"text": "Summarise " + style + ": " + p.Arguments["text"],
				},
			}},
		})
	case "resources/templates/list":
		return ok(r.ID, map[string]any{"resourceTemplates": []any{map[string]any{
			"uriTemplate": "demo://items/{id}", "name": "item",
			"description": "one item by id", "mimeType": "text/plain",
			"title": "Item", "icons": []any{map[string]any{"src": "https://example.com/t.png"}},
			"_meta": map[string]any{"example.com/k": "template"},
		}}})
	case "tools/call":
		return callTool(r)
	}
	return fail(r.ID, -32601, "method not found: "+r.Method)
}

func toolDefs() []map[string]any {
	return append(baseTools(), schemaTools()...)
}

// schemaVersion is the contents of FAKEMCP_SCHEMA_FILE, read on every request
// so a test can change a schema under a running server, which is what a
// server upgrade looks like from the client's side. Empty when the variable
// is unset, and then the tools that depend on it do not exist, so no other
// test sees them.
func schemaVersion() string {
	path := os.Getenv("FAKEMCP_SCHEMA_FILE")
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// schemaTools are the tools whose schema moves: create_issue takes a title
// in v1, and in v2 also requires a repo. outage always fails with a protocol
// error that has nothing to do with its arguments.
func schemaTools() []map[string]any {
	v := schemaVersion()
	if v == "" {
		return nil
	}
	props := map[string]any{"title": map[string]any{"type": "string"}}
	required := []string{"title"}
	if v == "v2" {
		props["repo"] = map[string]any{"type": "string"}
		required = append(required, "repo")
	}
	return []map[string]any{
		{
			"name":        "create_issue",
			"description": "File an issue.",
			"inputSchema": map[string]any{
				"type": "object", "properties": props, "required": required,
			},
		},
		{
			"name":        "outage",
			"description": "Always fails upstream, for a reason unrelated to its arguments.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func baseTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "echo",
			"description": "Echo a message back.\nSecond line of description.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"message": map[string]any{"type": "string", "description": "text to echo"}},
				"required":   []string{"message"},
			},
			// Read-only, so the destructive-confirmation policy has a tool
			// it must leave alone: an unannotated one may be destructive.
			"annotations": map[string]any{"readOnlyHint": true},
		},
		{
			"name":        "open",
			"description": "Record a value in this process's state.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"value": map[string]any{"type": "string"}},
				"required":   []string{"value"},
			},
		},
		{
			"name":        "chatty",
			"description": "Report progress and log messages, then echo the request's _meta.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "state",
			"description": "Report this process's pid and everything it has recorded.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "slow",
			"description": "Sleep for ms milliseconds, then return.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"ms": map[string]any{"type": "integer"}},
				"required":   []string{"ms"},
			},
		},
		{
			"name":        "boom",
			"description": "Always returns a tool error.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "structured",
			"title":       "Structured",
			"icons":       []any{map[string]any{"src": "https://example.com/s.png"}},
			"_meta":       map[string]any{"example.com/k": "tool"},
			"description": "Return structuredContent.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"outputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"n": map[string]any{"type": "integer"}},
			},
		},
		{
			// Annotated destructive so the confirmation policy has something
			// to fire on. Nothing here actually destroys anything.
			"name":        "wipe",
			"description": "Forget everything this process recorded.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"annotations": map[string]any{
				"title": "Wipe state", "destructiveHint": true, "readOnlyHint": false,
			},
		},
		{
			"name":        "fancy-name",
			"description": "Tool whose name is not a TypeScript identifier.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode":  map[string]any{"type": "string", "enum": []string{"a", "b"}},
					"count": map[string]any{"type": []any{"integer", "null"}},
					"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"nested": map[string]any{
						"type":       "object",
						"properties": map[string]any{"deep": map[string]any{"type": "boolean"}},
						"required":   []string{"deep"},
					},
				},
			},
		},
	}
}

var (
	barrierMu      sync.Mutex
	barrierArrived int
	barrierMet     = make(chan struct{})
)

// barrierWait reports whether n callers arrived before limit elapsed.
func barrierWait(n int, limit time.Duration) bool {
	barrierMu.Lock()
	barrierArrived++
	if barrierArrived == n {
		close(barrierMet)
	}
	met := barrierMet
	barrierMu.Unlock()
	select {
	case <-met:
		return true
	case <-time.After(limit):
		return false
	}
}

func textResult(s string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}}
}

func callTool(r req) map[string]any {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(r.Params, &p); err != nil {
		return fail(r.ID, -32602, "bad params")
	}
	switch p.Name {
	case "echo":
		msg, _ := p.Arguments["message"].(string)
		return ok(r.ID, textResult(msg))
	case "open":
		v, _ := p.Arguments["value"].(string)
		mu.Lock()
		state = append(state, v)
		mu.Unlock()
		return ok(r.ID, textResult("opened "+v))
	case "state":
		mu.Lock()
		s := append([]string(nil), state...)
		mu.Unlock()
		b, _ := json.Marshal(map[string]any{"pid": pid, "seen": s})
		return ok(r.ID, textResult(string(b)))
	case "wipe":
		mu.Lock()
		state = nil
		mu.Unlock()
		return ok(r.ID, textResult("wiped"))
	case "slow":
		ms := 0
		switch v := p.Arguments["ms"].(type) {
		case float64:
			ms = int(v)
		case string:
			ms, _ = strconv.Atoi(v)
		}
		if n, _ := p.Arguments["barrier"].(float64); n > 0 {
			// Wait until n barrier calls are in this process at once, with
			// ms only as the failure bound. Proves concurrency without a
			// wall-clock assertion that fails on a loaded machine.
			// Deliberately absent from the schema, so listings are unchanged.
			if barrierWait(int(n), time.Duration(ms)*time.Millisecond) {
				return ok(r.ID, textResult("barrier met"))
			}
			return ok(r.ID, textResult("barrier timeout"))
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return ok(r.ID, textResult(fmt.Sprintf("slept %dms on pid %d", ms, pid)))
	case "boom":
		return ok(r.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "boom: deliberate failure"}},
		})
	case "structured":
		// Every kind of block a proxy could flatten, so a test can see
		// which survive.
		return ok(r.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": `{"n":42}`},
				{"type": "image", "data": "iVBORw0KGgo=", "mimeType": "image/png"},
				{"type": "resource_link", "uri": "demo://greeting", "name": "greeting", "mimeType": "image/png"},
			},
			"structuredContent": map[string]any{"n": 42},
			"_meta":             map[string]any{"example.com/k": "result"},
		})
	case "fancy-name":
		b, _ := json.Marshal(p.Arguments)
		return ok(r.ID, textResult(string(b)))
	case "chatty":
		// Reports progress for the token it was given, logs at two levels,
		// and answers with the _meta it received, so a test can see both
		// what a proxy relayed back and what it passed on.
		var m struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		}
		_ = json.Unmarshal(r.Params, &m)
		if tok, ok := m.Meta["progressToken"]; ok {
			for i := 1; i <= 2; i++ {
				notify(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress",
					"params": map[string]any{"progressToken": tok, "progress": i, "total": 2}})
			}
		}
		for _, lvl := range []string{"debug", "warning"} {
			notify(map[string]any{"jsonrpc": "2.0", "method": "notifications/message",
				"params": map[string]any{"level": lvl, "data": "chatty " + lvl}})
		}
		b, _ := json.Marshal(m.Meta)
		return ok(r.ID, textResult(string(b)))
	}
	if v := schemaVersion(); v != "" {
		switch p.Name {
		case "create_issue":
			// Rejected the way an SDK's input validation rejects it: -32602
			// and a message that names nothing, which is the failure the
			// daemon's diagnostic exists to explain.
			if _, has := p.Arguments["title"]; !has {
				return fail(r.ID, -32602, "Invalid params")
			}
			if _, has := p.Arguments["repo"]; v == "v2" && !has {
				return fail(r.ID, -32602, "Invalid params")
			}
			return ok(r.ID, textResult("filed"))
		case "outage":
			return fail(r.ID, -32603, "internal error: the backend is unavailable")
		}
	}
	return fail(r.ID, -32602, "unknown tool "+p.Name)
}
