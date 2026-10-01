package mcpclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

type originKey struct{}

// A legacy server's request on a POST's response stream is related to that
// request, and the handler is given the context of the call that provoked
// it. Before, it got context.Background(), so the daemon had to guess whose
// question it was from who else shared the session -- and with a second
// call in flight (one the server never finished was enough) it could not,
// and the question never reached the client that could answer it.
func TestServerRequestCarriesTheOriginatingCallsContext(t *testing.T) {
	answered := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &m)
		w.Header().Set("Mcp-Session-Id", "s")
		switch {
		case m.Method == "initialize":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"t","version":"1"}}}`, m.ID)
		case m.Method == "tools/call":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":\"q1\",\"method\":\"elicitation/create\",\"params\":{\"message\":\"?\",\"requestedSchema\":{\"type\":\"object\"}}}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-answered:
			case <-time.After(10 * time.Second):
			}
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[]}}\n\n", m.ID)
		case m.Method == "" && string(m.ID) == `"q1"`:
			w.WriteHeader(http.StatusAccepted)
			close(answered)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	got := make(chan any, 1)
	tr, err := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cl, err := mcpclient.NewWithOptions(ctx, tr, mcpclient.Options{
		ClientName: "t", ClientVersion: "1", Preference: mcpclient.ForceLegacy,
		OnServerRequest: func(hctx context.Context, method string, _ json.RawMessage) (any, error) {
			got <- hctx.Value(originKey{})
			return map[string]any{"action": "decline"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	if _, err := cl.CallTool(context.WithValue(ctx, originKey{}, "the call"), "ask", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-got:
		if v != "the call" {
			t.Fatalf("handler context carried %v, want the originating call's value", v)
		}
	default:
		t.Fatal("the server's request never reached the handler")
	}
}
