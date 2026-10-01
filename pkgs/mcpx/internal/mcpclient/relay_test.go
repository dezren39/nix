package mcpclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// chattyModern is a 2026-07-28 server whose tools/call answers under
// application/json with several JSON values, one per line -- progress for
// the token it was sent, a debug and a warning log message, and then the
// response, which echoes the _meta it received. The official conformance
// fixture streams that way.
func chattyModern(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s := decodePost(r)
		if len(s.id) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		switch s.method {
		case "server/discover":
			writeJSON(w, result(s.id, discoverWith(map[string]any{"tools": map[string]any{}})))
		case "tools/list":
			writeJSON(w, result(s.id, map[string]any{"tools": []any{map[string]any{"name": "chatty",
				"inputSchema": map[string]any{"type": "object"}}}, "ttlMs": 0, "cacheScope": "private"}))
		case "tools/call":
			var p struct {
				Meta map[string]json.RawMessage `json:"_meta"`
			}
			params, _ := json.Marshal(s.body["params"])
			_ = json.Unmarshal(params, &p)
			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			if tok, ok := p.Meta["progressToken"]; ok {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress",
					"params": map[string]any{"progressToken": tok, "progress": 1}})
			}
			for _, l := range []string{"debug", "warning"} {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/message",
					"params": map[string]any{"level": l, "data": l}})
			}
			meta, _ := json.Marshal(p.Meta)
			_ = enc.Encode(result(s.id, map[string]any{"content": []any{
				map[string]any{"type": "text", "text": string(meta)}}}))
		default:
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": s.id, "error": map[string]any{"code": -32601, "message": "no"}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A response that arrives as several JSON values under application/json is
// read as a sequence. Read as one value it was not JSON, the response was
// dropped, and the call hung to its deadline.
func TestNewlineDelimitedJSONResponseIsRead(t *testing.T) {
	c := dialURL(t, chattyModern(t).URL, mcpclient.PreferModern, mcpclient.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.CallTool(ctx, "chatty", nil); err != nil {
		t.Fatalf("the response was the last of several values on the body: %v", err)
	}
}

// A relay sends mcpx's own token upstream, gives progress back under the
// host's, passes trace context through, asks the upstream for the host's log
// level, and filters what comes back to it (#212).
func TestRelayCarriesProgressLogsAndTrace(t *testing.T) {
	c := dialURL(t, chattyModern(t).URL, mcpclient.PreferModern, mcpclient.Options{})
	var mu sync.Mutex
	var progress, messages []string
	r := &mcpclient.Relay{
		ProgressToken: json.RawMessage(`"host-tok"`),
		LogLevel:      "warning",
		Meta:          map[string]json.RawMessage{"traceparent": json.RawMessage(`"00-abc-def-01"`)},
		OnProgress: func(p json.RawMessage) {
			mu.Lock()
			progress = append(progress, string(p))
			mu.Unlock()
		},
		OnMessage: func(p json.RawMessage) {
			mu.Lock()
			messages = append(messages, string(p))
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := c.CallTool(mcpclient.WithRelay(ctx, r), "chatty", nil)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(raw, &res)
	if len(res.Content) == 0 {
		t.Fatalf("no content: %s", raw)
	}
	var sent map[string]json.RawMessage
	_ = json.Unmarshal([]byte(res.Content[0].Text), &sent)
	if string(sent["traceparent"]) != `"00-abc-def-01"` {
		t.Errorf("traceparent not passed upstream: %v", sent)
	}
	if string(sent[mcpclient.MetaLogLevel]) != `"warning"` {
		t.Errorf("the host's level should be asked of a modern upstream: %v", sent)
	}
	var tok string
	if json.Unmarshal(sent["progressToken"], &tok) != nil || tok == "" || tok == "host-tok" {
		t.Errorf("the upstream should get a token of mcpx's own: %s", sent["progressToken"])
	}

	mu.Lock()
	defer mu.Unlock()
	if len(progress) != 1 {
		t.Fatalf("want one progress, got %v", progress)
	}
	var p map[string]any
	_ = json.Unmarshal([]byte(progress[0]), &p)
	if p["progressToken"] != "host-tok" {
		t.Errorf("progress should come back under the host's token: %v", p)
	}
	if fmt.Sprint(messages) != `[{"data":"warning","level":"warning"}]` {
		t.Errorf("want only the warning, got %v", messages)
	}
}
