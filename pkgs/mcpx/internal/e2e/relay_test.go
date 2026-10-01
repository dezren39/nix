package e2e_test

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// relaySession drives `mcpx serve --passthrough demo` over stdio: initialize
// with caps, optionally logging/setLevel, then one tools/call of the fake
// server's chatty tool with meta. It returns every frame that arrived before
// the call's response, and the response.
func relaySession(t *testing.T, e *env, caps map[string]any, level string, meta map[string]any) (notes []map[string]any, result map[string]any) {
	t.Helper()
	cmd := exec.Command(e.mcpx, "serve", "--passthrough", "demo")
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
	frames := make(chan map[string]any, 64)
	go func() {
		defer close(frames)
		dec := json.NewDecoder(bufio.NewReader(stdout))
		for {
			var f map[string]any
			if dec.Decode(&f) != nil {
				return
			}
			frames <- f
		}
	}()
	await := func(id float64) map[string]any {
		deadline := time.After(30 * time.Second)
		for {
			select {
			case f, ok := <-frames:
				if !ok {
					t.Fatalf("serve exited before answering %v", id)
				}
				if got, _ := f["id"].(float64); got == id && f["method"] == nil {
					return f
				}
				if m, _ := f["method"].(string); strings.HasPrefix(m, "notifications/progress") || m == "notifications/message" {
					notes = append(notes, f)
				}
			case <-deadline:
				t.Fatalf("no reply to %v", id)
			}
		}
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": caps,
			"clientInfo": map[string]any{"name": "t", "version": "1"}}})
	init := await(1)
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if r, _ := init["result"].(map[string]any); r != nil {
		c, _ := r["capabilities"].(map[string]any)
		if _, ok := c["logging"]; !ok {
			t.Errorf("mcpx relays log messages, so a legacy client should be told logging is available: %v", c)
		}
	}
	if level != "" {
		send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "logging/setLevel",
			"params": map[string]any{"level": level}})
		await(2)
	}
	params := map[string]any{"name": "chatty", "arguments": map[string]any{}}
	if meta != nil {
		params["_meta"] = meta
	}
	notes = nil
	send(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": params})
	return notes, await(3)
}

// A host's progressToken, log level and trace context reach the upstream
// through mcpx, and the upstream's progress and log messages come back to
// that host during the call. mcpx stripped the token, sent no trace
// context, and kept every upstream log message to itself (#212).
//
// Both routes a tools/call takes: a client that declared nothing goes
// straight to /v1/call, one that can answer an elicitation goes through the
// interruptible /v1/ask poll loop.
func TestUpstreamProgressAndLogsReachTheHost(t *testing.T) {
	const trace = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	for _, tc := range []struct {
		name string
		caps map[string]any
	}{
		{"direct", map[string]any{}},
		{"ask", map[string]any{"elicitation": map[string]any{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, oneServer)
			e.run("refresh")
			notes, res := relaySession(t, e, tc.caps, "info",
				map[string]any{"progressToken": "host-tok", "traceparent": trace})

			var progress []float64
			var levels []string
			for _, n := range notes {
				p, _ := n["params"].(map[string]any)
				switch n["method"] {
				case "notifications/progress":
					if p["progressToken"] != "host-tok" {
						t.Errorf("progress must carry the host's own token, not mcpx's: %v", p)
					}
					v, _ := p["progress"].(float64)
					progress = append(progress, v)
				case "notifications/message":
					l, _ := p["level"].(string)
					levels = append(levels, l)
				}
			}
			if len(progress) != 2 || progress[0] != 1 || progress[1] != 2 {
				t.Errorf("want progress 1 then 2 before the result, got %v (frames %v)", progress, notes)
			}
			if strings.Join(levels, ",") != "warning" {
				t.Errorf("want only the warning (the host set info; debug is below it), got %v", levels)
			}

			// What the upstream received, as it echoed it.
			got := toJSON(res)
			if !strings.Contains(got, trace) {
				t.Errorf("traceparent should reach the upstream: %s", got)
			}
			if strings.Contains(got, "host-tok") || !strings.Contains(got, `progressToken\":\"mcpx-`) {
				t.Errorf("the upstream should get mcpx's own token, never the host's: %s", got)
			}
		})
	}
}

// Nothing asked for, nothing relayed: no token means no progress, no level
// means no log messages -- the 2025 revisions leave the default to the
// server, and mcpx's default is silence.
func TestNothingRelayedUnlessTheHostAsked(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("refresh")
	notes, res := relaySession(t, e, map[string]any{}, "", nil)
	if len(notes) != 0 {
		t.Errorf("the host asked for nothing and got %v", notes)
	}
	if res["result"] == nil {
		t.Errorf("the call should still answer: %v", res)
	}
}

// `mcpx call` prints the tool's result and nothing else: the relay is for
// MCP hosts, and an upstream's chatter must not reach a CLI's stdout.
func TestCLICallOutputCarriesNoNotifications(t *testing.T) {
	e := newEnv(t, oneServer)
	out := e.run("call", "demo.chatty")
	if strings.Contains(out, "notifications/") || strings.Contains(out, "chatty warning") {
		t.Errorf("mcpx call leaked the upstream's notifications:\n%s", out)
	}
}
