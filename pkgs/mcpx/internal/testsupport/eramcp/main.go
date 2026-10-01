// Command eramcp is a stdio MCP server whose protocol era is chosen by
// ERAMCP_MODE, for testing how mcpx decides which era a server speaks.
//
// Separate from fakemcp so that fakemcp's behaviour -- which most of the
// suite relies on -- does not change. Every method received is appended to
// the file named by ERAMCP_LOG, one per line, so a test can assert on what
// was actually sent rather than on what mcpx says it sent.
//
// Modes:
//
//	legacy-32601   unknown methods before initialize get -32601
//	legacy-32602   unknown methods before initialize get -32602
//	legacy-silent  unknown methods get no answer at all
//	legacy-exit    any first message but initialize makes it exit
//	legacy-catchall unknown methods get an empty success result
//	modern         answers server/discover; rejects initialize naming its versions
//	dual           answers both
//
// Tools: hello says which era its session is ("hello from <mode> over
// legacy|modern"); ask, on a modern session, answers input_required with an
// elicitation and then "answered <the response>".
//
// ERAMCP_DISCOVER_DELAY delays the discover answer (a slow-starting server).
// ERAMCP_SUPPORTED overrides the versions a modern server supports
// (comma-separated); a discover asking for another gets -32022.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type frame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

var (
	mode      = os.Getenv("ERAMCP_MODE")
	writeMu   sync.Mutex
	out       = bufio.NewWriter(os.Stdout)
	supported = []string{"2026-07-28"}
	// initialized is set once this process has answered initialize: the
	// session is legacy from then on.
	initialized atomic.Bool
)

func main() {
	if s := os.Getenv("ERAMCP_SUPPORTED"); s != "" {
		supported = strings.Split(s, ",")
	}
	in := bufio.NewScanner(os.Stdin)
	first := true
	for in.Scan() {
		var f frame
		if json.Unmarshal(in.Bytes(), &f) != nil {
			continue
		}
		record(f.Method)
		if first && mode == "legacy-exit" && f.Method != "initialize" {
			fmt.Fprintln(os.Stderr, "eramcp: first message was not initialize; exiting")
			os.Exit(1)
		}
		first = false
		go handle(f)
	}
}

func record(method string) {
	path := os.Getenv("ERAMCP_LOG")
	if path == "" || method == "" {
		return
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer fh.Close()
	fmt.Fprintln(fh, method)
}

func send(v map[string]any) {
	v["jsonrpc"] = "2.0"
	b, _ := json.Marshal(v)
	writeMu.Lock()
	defer writeMu.Unlock()
	out.Write(b)
	out.WriteByte('\n')
	out.Flush()
}

func ok(id json.RawMessage, result any) { send(map[string]any{"id": id, "result": result}) }

func fail(id json.RawMessage, code int, msg string, data any) {
	e := map[string]any{"code": code, "message": msg}
	if data != nil {
		e["data"] = data
	}
	send(map[string]any{"id": id, "error": e})
}

func legacy() bool { return strings.HasPrefix(mode, "legacy") || mode == "dual" }
func modern() bool { return mode == "modern" || mode == "dual" }

func handle(f frame) {
	if len(f.ID) == 0 {
		return // a notification
	}
	switch f.Method {
	case "initialize":
		if !legacy() {
			fail(f.ID, -32601, "initialize is not supported; this server speaks "+strings.Join(supported, ", "), nil)
			return
		}
		initialized.Store(true)
		ok(f.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"serverInfo":      map[string]any{"name": "eramcp", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		})
		return
	case "server/discover":
		if modern() {
			if d := os.Getenv("ERAMCP_DISCOVER_DELAY"); d != "" {
				if dur, err := time.ParseDuration(d); err == nil {
					time.Sleep(dur)
				}
			}
			var p struct {
				Meta map[string]any `json:"_meta"`
			}
			_ = json.Unmarshal(f.Params, &p)
			asked, _ := p.Meta["io.modelcontextprotocol/protocolVersion"].(string)
			if !contains(supported, asked) {
				fail(f.ID, -32022, "Unsupported protocol version",
					map[string]any{"supported": supported, "requested": asked})
				return
			}
			ok(f.ID, map[string]any{
				"resultType":        "complete",
				"supportedVersions": supported,
				"capabilities":      map[string]any{"tools": map[string]any{}},
				"_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": "eramcp", "version": "1"},
				},
			})
			return
		}
	case "ping":
		ok(f.ID, map[string]any{})
		return
	case "tools/list":
		ok(f.ID, map[string]any{"tools": []any{map[string]any{
			"name": "hello", "description": "says hello",
			"inputSchema": map[string]any{"type": "object"},
		}, map[string]any{
			"name": "ask", "description": "asks a question, the 2026-07-28 way",
			"inputSchema": map[string]any{"type": "object"},
		}}})
		return
	case "tools/call":
		var call struct {
			Name           string                     `json:"name"`
			InputResponses map[string]json.RawMessage `json:"inputResponses"`
		}
		_ = json.Unmarshal(f.Params, &call)
		if call.Name == "ask" && !initialized.Load() {
			// The 2026-07-28 way to ask: answer input_required and expect
			// the same request again with the answer attached.
			if a, has := call.InputResponses["q"]; has {
				ok(f.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "answered " + string(a)}}})
				return
			}
			ok(f.ID, map[string]any{"resultType": "input_required", "inputRequests": map[string]any{
				"q": map[string]any{"method": "elicitation/create", "params": map[string]any{
					"mode": "form", "message": "which colour?",
					"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{
						"colour": map[string]any{"type": "string"}}},
				}},
			}})
			return
		}
		// Which era this session is, so a test can tell which of a dual
		// server's sessions answered.
		over := "modern"
		if initialized.Load() {
			over = "legacy"
		}
		ok(f.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "hello from " + mode + " over " + over}}})
		return
	}
	// Unknown here, or discover to a server that does not speak it.
	switch mode {
	case "legacy-silent":
		return
	case "legacy-32602":
		fail(f.ID, -32602, "invalid params: not initialized", nil)
	case "legacy-catchall":
		ok(f.ID, map[string]any{})
	default:
		fail(f.ID, -32601, "method not found: "+f.Method, nil)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
