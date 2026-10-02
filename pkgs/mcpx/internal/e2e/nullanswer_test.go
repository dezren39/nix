package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// An accepted sampling answer whose content is JSON null passed the broker
// (its check was len(content) == 0, and "null" is four bytes), unmarshalled
// into a nil map without error, and the next write to that map panicked on an
// upstream client's goroutine -- where nothing recovers. One POST killed the
// daemon and every session on it.
func TestANullSamplingAnswerIsRefusedAndTheDaemonSurvives(t *testing.T) {
	e := askEnv(t)
	e.run("refresh")
	ep := e.endpoint(t)
	// The daemon by pid: `mcpx status` starts a new one if the old died, so
	// "status works afterwards" would prove nothing.
	var st struct {
		Socket string `json:"socket"`
		PID    int    `json:"pid"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil || st.PID == 0 {
		t.Fatalf("no daemon to watch: %v %+v", err, st)
	}

	// A client that declared no sampling capability, so the question goes
	// to the broker, where anything that can reach the daemon may answer it.
	done := make(chan string, 1)
	go func() {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1,
			"method": "tools/call", "params": map[string]any{"name": "mcpx_call",
				"arguments": map[string]any{"namespace": "ask", "tool": "need_model"}}})
		resp, err := (&http.Client{Timeout: 2 * time.Minute}).Post(
			ep+"/mcp", "application/json", strings.NewReader(string(b)))
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		done <- string(raw)
	}()

	var id string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline) && id == ""; {
		listed, _ := e.try("--json", "elicit", "list")
		var pending []struct {
			ID   string `json:"id"`
			Mode string `json:"mode"`
		}
		if json.Unmarshal([]byte(jsonOf(t, listed)), &pending) == nil {
			for _, p := range pending {
				if p.Mode == "sample" {
					id = p.ID
				}
			}
		}
		if id == "" {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if id == "" {
		t.Fatal("the sampling request never reached the broker")
	}

	resp, err := e.socketClient(t).Post("http://mcpx/v1/elicit/"+id+"/accept",
		"application/json", strings.NewReader("null"))
	if err != nil {
		t.Fatalf("the daemon did not answer the POST: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("a null answer to a sampling request must be refused, got 200: %s", body)
	}

	// Still the same daemon, and the question still open for a real answer.
	time.Sleep(500 * time.Millisecond) // the panic, if any, is on another goroutine
	if !daemonAlive(st.Socket, st.PID) {
		t.Fatalf("daemon %d died after a null sampling answer", st.PID)
	}
	e.run("elicit", "answer", id, "--json",
		`{"role":"assistant","model":"m","content":{"type":"text","text":"banana"}}`)
	select {
	case got := <-done:
		if !strings.Contains(got, "banana") {
			t.Fatalf("a valid answer after the refused one should resume the call:\n%s", got)
		}
	case <-time.After(time.Minute):
		t.Fatal("the call never finished")
	}
}
