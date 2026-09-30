package e2e_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Transport conformance against the real binary and the real daemon.

// TestServeStdoutCarriesOnlyMCP is the stdio rule every revision states: the
// server MUST NOT write anything to stdout that is not a valid MCP message,
// and each message is one line.
//
// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#stdio
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio
func TestServeStdoutCarriesOnlyMCP(t *testing.T) {
	t.Run("2025-11-25/stdio/stdout-is-only-mcp-one-message-per-line", func(t *testing.T) {
		e := newEnv(t, oneServer)
		in := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcpx_call","arguments":{"namespace":"demo","tool":"echo","arguments":{"text":"multi\nline"}}}}`,
			`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mcpx_call","arguments":{"namespace":"demo","tool":"boom"}}}`,
			`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"no_such_tool"}}`,
			`{"jsonrpc":"2.0","id":6,"method":"no/such/method"}`,
		}, "\n") + "\n"
		cmd := exec.Command(e.mcpx, "serve")
		cmd.Dir = e.dir
		cmd.Env = e.envVars
		cmd.Stdin = strings.NewReader(in)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("serve: %v\nstderr:\n%s", err, stderr.String())
		}
		seen := map[float64]bool{}
		sc := bufio.NewScanner(&stdout)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			var f map[string]any
			if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
				t.Fatalf("stdout carried a line that is not one JSON-RPC message: %q", sc.Text())
			}
			if f["jsonrpc"] != "2.0" {
				t.Errorf("not JSON-RPC 2.0: %q", sc.Text())
			}
			_, hasResult := f["result"]
			_, hasError := f["error"]
			_, hasMethod := f["method"]
			if !hasResult && !hasError && !hasMethod {
				t.Errorf("neither a response nor a request/notification: %q", sc.Text())
			}
			if id, ok := f["id"].(float64); ok {
				seen[id] = true
			}
		}
		for id := 1.0; id <= 6; id++ {
			if !seen[id] {
				t.Errorf("request %v was not answered before exit:\n%s", id, stdout.String())
			}
		}
	})
}

// TestServeStdioCancellation drives cancellation through the real binary:
// the cancelled request gets no reply and the server keeps answering.
//
// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation
func TestServeStdioCancellation(t *testing.T) {
	t.Run("2025-11-25/stdio/cancelled-request-is-not-answered", func(t *testing.T) {
		e := newEnv(t, oneServer)
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
		frames := make(chan map[string]any, 16)
		go func() {
			defer close(frames)
			dec := json.NewDecoder(stdout)
			for {
				var f map[string]any
				if dec.Decode(&f) != nil {
					return
				}
				frames <- f
			}
		}()
		write := func(s string) {
			if _, err := stdin.Write([]byte(s + "\n")); err != nil {
				t.Fatal(err)
			}
		}
		write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`)
		<-frames
		write(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcpx_call","arguments":{"namespace":"demo","tool":"slow","arguments":{"ms":3000}}}}`)
		time.Sleep(300 * time.Millisecond)
		write(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2,"reason":"test"}}`)
		write(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
		select {
		case f := <-frames:
			if f["id"] != float64(3) {
				t.Fatalf("expected the ping's reply first, got %v", f)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("a ping behind a cancelled call was not answered")
		}
		stdin.Close()
		for f := range frames {
			if f["id"] == float64(2) {
				t.Fatalf("the cancelled request was answered: %v", f)
			}
		}
	})
}

// TestDaemonMCPTransport covers the HTTP rules on the daemon's own /mcp.
func TestDaemonMCPTransport(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	ep := e.endpoint(t)
	do := func(method string, body string, h map[string]string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, ep+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range h {
			req.Header.Set(k, v)
		}
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#security-warning
	t.Run("2025-11-25/streamable-http/foreign-origin-is-403", func(t *testing.T) {
		r := do(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"ping"}`,
			map[string]string{"Origin": "https://attacker.example"})
		r.Body.Close()
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d, want 403", r.StatusCode)
		}
		r = do(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"ping"}`,
			map[string]string{"Origin": "http://localhost:5173"})
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("a loopback origin should be served, got %d", r.StatusCode)
		}
	})
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#server-validation
	t.Run("2026-07-28/streamable-http/missing-headers-is-400-header-mismatch", func(t *testing.T) {
		r := do(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{`+
			`"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`, nil)
		defer r.Body.Close()
		var f struct {
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(r.Body).Decode(&f)
		if r.StatusCode != http.StatusBadRequest || f.Error.Code != -32020 {
			t.Fatalf("status %d code %d, want 400 -32020", r.StatusCode, f.Error.Code)
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#listening-for-messages-from-the-server
	t.Run("2025-11-25/streamable-http/get-stream-then-delete-then-404", func(t *testing.T) {
		init := do(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`, nil)
		init.Body.Close()
		sid := init.Header.Get("Mcp-Session-Id")
		if sid == "" {
			t.Fatal("no session")
		}
		req, _ := http.NewRequest(http.MethodGet, ep+"/mcp", nil)
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Mcp-Session-Id", sid)
		get, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer get.Body.Close()
		if !strings.HasPrefix(get.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("GET: status %d type %q", get.StatusCode, get.Header.Get("Content-Type"))
		}
		del := do(http.MethodDelete, "", map[string]string{"Mcp-Session-Id": sid})
		del.Body.Close()
		ended := make(chan struct{})
		go func() {
			sc := bufio.NewScanner(get.Body)
			for sc.Scan() {
			}
			close(ended)
		}()
		select {
		case <-ended:
		case <-time.After(10 * time.Second):
			t.Fatal("DELETE should end the session's GET stream")
		}
		r := do(http.MethodPost, `{"jsonrpc":"2.0","id":2,"method":"ping"}`, map[string]string{"Mcp-Session-Id": sid})
		r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Fatalf("status %d after DELETE, want 404", r.StatusCode)
		}
	})
}
