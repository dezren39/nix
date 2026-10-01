// Command askmcp is a stdio MCP server that asks its client questions.
//
// fakemcp answers everything itself, which is the right shape for testing a
// proxy and useless for testing elicitation: nothing there ever asks. This
// one elicits, samples, and implements completion/complete, so that the path
// from an upstream question to whoever answers it can be driven end to end
// rather than asserted about in isolation.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
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
	// clientCaps is what the client declared, so a test can assert that
	// nothing undeclared was ever sent.
	clientCaps map[string]json.RawMessage
}

func main() {
	s := &server{out: bufio.NewWriter(os.Stdout), pending: map[int64]chan *frame{}}
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

func (s *server) handle(f frame) map[string]any {
	switch f.Method {
	case "initialize":
		var p struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		}
		_ = json.Unmarshal(f.Params, &p)
		s.mu.Lock()
		s.clientCaps = p.Capabilities
		s.mu.Unlock()
		return ok(f.ID, map[string]any{
			"protocolVersion": "2025-11-25",
			"serverInfo":      map[string]any{"name": "askmcp", "version": "1.0.0"},
			"capabilities": map[string]any{
				"tools":       map[string]any{},
				"prompts":     map[string]any{},
				"resources":   map[string]any{},
				"completions": map[string]any{},
			},
		})
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		return ok(f.ID, map[string]any{})
	case "tools/list":
		return ok(f.ID, map[string]any{"tools": []map[string]any{
			{
				"name":        "need_repo",
				"description": "Asks which repository, then reports the answer.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			},
			{
				"name":        "need_model",
				"description": "Asks the client's model to write something.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			},
			{
				"name":        "declared",
				"description": "Reports the capabilities the client declared.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}})
	case "prompts/list":
		return ok(f.ID, map[string]any{"prompts": []map[string]any{
			{"name": "confirmed", "description": "a prompt that asks first"},
		}})
	case "prompts/get":
		res, err := s.ask("elicitation/create", map[string]any{
			"message":         "Which repository should this go in?",
			"requestedSchema": repoSchema(),
		})
		if err != nil {
			return fail(f.ID, -32603, err.Error())
		}
		return ok(f.ID, map[string]any{"messages": []map[string]any{{
			"role": "user", "content": map[string]any{"type": "text",
				"text": "prompt for " + string(res)}}}})
	case "resources/list":
		return ok(f.ID, map[string]any{"resources": []map[string]any{
			{"uri": "ask://secret", "name": "secret", "mimeType": "text/plain"},
		}})
	case "resources/templates/list":
		return ok(f.ID, map[string]any{"resourceTemplates": []any{}})
	case "resources/read":
		res, err := s.ask("elicitation/create", map[string]any{
			"message":         "Which repository should this go in?",
			"requestedSchema": repoSchema(),
		})
		if err != nil {
			return fail(f.ID, -32603, err.Error())
		}
		return ok(f.ID, map[string]any{"contents": []map[string]any{
			{"uri": "ask://secret", "mimeType": "text/plain", "text": "read " + string(res)}}})
	case "completion/complete":
		// Values mcpx could not have guessed, so a test can tell an
		// upstream answer from the cached one.
		// And context.arguments echoed back, so a test can see they arrived.
		var cp struct {
			Context struct {
				Arguments map[string]string `json:"arguments"`
			} `json:"context"`
		}
		_ = json.Unmarshal(f.Params, &cp)
		values := []string{"from-upstream-a", "from-upstream-b"}
		for k, v := range cp.Context.Arguments {
			values = append(values, "ctx:"+k+"="+v)
		}
		return ok(f.ID, map[string]any{"completion": map[string]any{
			"values":  values,
			"total":   len(values),
			"hasMore": false,
		}})
	case "tools/call":
		return s.callTool(f)
	}
	return fail(f.ID, -32601, "method not found: "+f.Method)
}

func repoSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"repo": map[string]any{"type": "string", "description": "owner/name"},
		},
		"required": []string{"repo"},
	}
}

func (s *server) callTool(f frame) map[string]any {
	var p struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(f.Params, &p)
	switch p.Name {
	case "need_repo":
		res, err := s.ask("elicitation/create", map[string]any{
			"message":         "Which repository should this go in?",
			"requestedSchema": repoSchema(),
		})
		if err != nil {
			return fail(f.ID, -32603, err.Error())
		}
		var e struct {
			Action  string         `json:"action"`
			Content map[string]any `json:"content"`
		}
		_ = json.Unmarshal(res, &e)
		if e.Action != "accept" {
			return ok(f.ID, text("no repository was chosen ("+e.Action+")"))
		}
		return ok(f.ID, text(fmt.Sprintf("using %v", e.Content["repo"])))
	case "need_model":
		res, err := s.ask("sampling/createMessage", map[string]any{
			"maxTokens":    64,
			"systemPrompt": "write one word",
			"messages": []map[string]any{{
				"role": "user", "content": map[string]any{"type": "text", "text": "a word"}}},
		})
		if err != nil {
			return fail(f.ID, -32603, err.Error())
		}
		return ok(f.ID, text("model said "+string(res)))
	case "declared":
		s.mu.Lock()
		b, _ := json.Marshal(s.clientCaps)
		s.mu.Unlock()
		return ok(f.ID, text(string(b)))
	}
	return fail(f.ID, -32602, "unknown tool "+p.Name)
}
