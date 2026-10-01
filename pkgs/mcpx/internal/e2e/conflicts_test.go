package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/testsupport"
)

// Tests for the communication conflicts recorded by WP7 and fixed in WP9.
// Each is named for the conflict it pins.

// longSocketClient is socketClient without its 3s ceiling, for calls that
// are meant to wait on a person.
func (e *env) longSocketClient(t *testing.T) *http.Client {
	t.Helper()
	var st struct {
		Socket string `json:"socket"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", st.Socket)
		}},
	}
}

func sockDo(t *testing.T, c *http.Client, method, path string, body any, header ...string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://mcpx"+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s %s: %v\n%s", method, path, err, raw)
	}
	return resp.StatusCode, out
}

// askWith is askEnv with the server's leasing spelled out.
func askWith(t *testing.T, mcpx string) *env {
	t.Helper()
	e := newEnv(t, oneServer)
	ask := testsupport.AskMCPBinary(t)
	cfg := fmt.Sprintf(`{"mcpServers":{"ask":{"command":%q,"mcpx":%s}}}`, ask, mcpx)
	if err := os.WriteFile(filepath.Join(e.dir, ".mcpx.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

// pendingQuestions lists the broker's open questions.
func pendingQuestions(t *testing.T, e *env) []string {
	t.Helper()
	out, err := e.try("--json", "elicit", "list")
	if err != nil {
		return nil
	}
	var pending []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(out), &pending)
	var ids []string
	for _, p := range pending {
		ids = append(ids, p.ID)
	}
	return ids
}

func waitQuestions(t *testing.T, e *env, n int) []string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ids := pendingQuestions(t, e); len(ids) >= n {
			return ids
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("expected %d pending questions, saw %v", n, pendingQuestions(t, e))
	return nil
}

// Conflict #1. A question on a shared connection was attributed to the one
// /v1/ask call in the table even when a plain /v1/call was running on the
// same connection, so the ask caller was handed -- and could answer -- the
// other caller's question.
// https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation
func TestConflicts(t *testing.T) {
	t.Run("2025-11-25/elicitation/question-never-attributed-to-another-callers-call", func(t *testing.T) {
		e := askWith(t, `{"sharing":"shared","scope":"global"}`)
		e.run("refresh")
		c := e.longSocketClient(t)

		_, begun := sockDo(t, c, http.MethodPost, "/v1/ask",
			map[string]any{"kind": "tools/call", "server": "ask", "tool": "need_repo"})
		id, _ := begun["callId"].(string)
		if id == "" {
			t.Fatalf("no call id: %v", begun)
		}
		waitQuestions(t, e, 1)

		plain := make(chan map[string]any, 1)
		go func() {
			_, out := sockDo(t, c, http.MethodPost, "/v1/call",
				map[string]any{"server": "ask", "tool": "need_repo"}, "X-Mcpx-Session", "cli-user")
			plain <- out
		}()
		ids := waitQuestions(t, e, 2)

		_, poll := sockDo(t, c, http.MethodGet, "/v1/ask/"+id+"?waitMs=1", nil)
		qs, _ := poll["questions"].([]any)
		if len(qs) != 1 {
			t.Fatalf("the ask call must see only its own question, saw %d: %v", len(qs), poll["questions"])
		}
		for _, q := range ids {
			e.run("elicit", "answer", q, `{"repo":"r"}`)
		}
		select {
		case <-plain:
		case <-time.After(30 * time.Second):
			t.Fatal("the plain call did not finish")
		}
	})

	// Conflict #3. The same isError result read as success on three of five
	// surfaces. https://modelcontextprotocol.io/specification/2025-11-25/server/tools#error-handling
	t.Run("2025-11-25/tools/upstream-isError-preserved", func(t *testing.T) {
		e := newEnv(t, oneServer)
		e.run("ls")
		c := e.longSocketClient(t)

		if out, err := e.try("call", "demo.boom", "{}"); err == nil {
			t.Errorf("cli: mcpx call of a failing tool exited 0:\n%s", out)
		} else if !strings.Contains(out, "deliberate failure") {
			t.Errorf("cli: the failure text should still be shown:\n%s", out)
		}

		st, out := sockDo(t, c, http.MethodPost, "/v1/call", map[string]any{"server": "demo", "tool": "boom"})
		res, _ := out["result"].(map[string]any)
		if st != http.StatusOK || res["isError"] != true {
			t.Errorf("/v1/call: %d %v", st, out)
		}
		st, out = sockDo(t, c, http.MethodPost, "/v1/call/demo/boom", map[string]any{})
		if st != http.StatusOK || out["ok"] != false {
			t.Errorf("/v1/call/{s}/{t}: want 200 ok:false, got %d %v", st, out)
		}
		st, out = sockDo(t, c, http.MethodPost, "/v1/tools/mcpx_call",
			map[string]any{"namespace": "demo", "tool": "boom"})
		if st != http.StatusOK || out["ok"] != false || !strings.Contains(fmt.Sprint(out["result"]), "deliberate failure") {
			t.Errorf("/v1/tools/mcpx_call: want 200 ok:false with the text, got %d %v", st, out)
		}

		frames := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcpx_call","arguments":{"namespace":"demo","tool":"boom"}}}
`
		mcp := e.runStdin(frames, "serve")
		var got map[string]any
		for _, line := range strings.Split(mcp, "\n") {
			var f map[string]any
			if json.Unmarshal([]byte(line), &f) == nil && fmt.Sprint(f["id"]) == "2" {
				got = f
			}
		}
		r, _ := got["result"].(map[string]any)
		if r["isError"] != true {
			t.Errorf("MCP mcpx_call: want isError:true, got %v", got)
		}
	})

	// Conflict #5. /v1/ask went straight to the pool, so the destructive
	// guard /v1/call applies was skipped for exactly the interruptible path.
	t.Run("mcpx/consumer/ask-path-honours-confirmDestructive", func(t *testing.T) {
		e := newEnv(t, oneServer)
		e.setenv("MCPX_ELICIT_CONFIRM_DESTRUCTIVE=true", "MCPX_ELICIT_ASK_TIMEOUT=1s")
		e.run("ls")
		c := e.longSocketClient(t)
		_, begun := sockDo(t, c, http.MethodPost, "/v1/ask",
			map[string]any{"kind": "tools/call", "server": "demo", "tool": "wipe"})
		id, _ := begun["callId"].(string)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			_, poll := sockDo(t, c, http.MethodGet, "/v1/ask/"+id+"?waitMs=500", nil)
			if poll["done"] == true {
				if s := fmt.Sprint(poll); !strings.Contains(s, "destructive") || strings.Contains(s, "wiped") {
					t.Fatalf("an unconfirmed destructive ask call must be refused: %v", poll)
				}
				return
			}
		}
		t.Fatal("the ask call never finished")
	})

	// Conflict #6. pool.callTimeout counted the time a person spent
	// answering, so a call whose question was answered after the timeout
	// died although its question was still open.
	t.Run("mcpx/timeouts/call-budget-paused-while-a-question-is-pending", func(t *testing.T) {
		e := askWith(t, `{"sharing":"shared","scope":"global","callTimeout":"1s"}`)
		e.run("refresh")
		c := e.longSocketClient(t)
		done := make(chan map[string]any, 1)
		go func() {
			_, out := sockDo(t, c, http.MethodPost, "/v1/call", map[string]any{"server": "ask", "tool": "need_repo"})
			done <- out
		}()
		ids := waitQuestions(t, e, 1)
		time.Sleep(2500 * time.Millisecond) // well past the 1s budget
		e.run("elicit", "answer", ids[0], `{"repo":"late"}`)
		select {
		case out := <-done:
			if !strings.Contains(fmt.Sprint(out), "using late") {
				t.Fatalf("a call waiting on a person must survive pool.callTimeout: %v", out)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("the call did not finish")
		}
	})

	// Conflict #11. With no session and no call id, every request shared
	// the one "call:anonymous" instance.
	t.Run("mcpx/scope/anonymous-calls-are-isolated", func(t *testing.T) {
		e := newEnv(t, statefulServer)
		e.run("ls")
		c := e.longSocketClient(t)
		sockDo(t, c, http.MethodPost, "/v1/call", map[string]any{"server": "demo", "tool": "open",
			"args": map[string]any{"value": "from-first"}})
		_, out := sockDo(t, c, http.MethodPost, "/v1/call", map[string]any{"server": "demo", "tool": "state"})
		if strings.Contains(fmt.Sprint(out), "from-first") {
			t.Fatalf("a second anonymous caller saw the first one's state: %v", out)
		}
	})
}
