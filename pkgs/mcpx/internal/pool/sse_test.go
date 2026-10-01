package pool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/pool"
)

// oldSSEServer speaks only the 2024-11-05 HTTP+SSE transport, and counts the
// Streamable HTTP POSTs it refuses.
type oldSSEServer struct {
	*httptest.Server
	mu      sync.Mutex
	refused int
}

func newOldSSEServer(t *testing.T) *oldSSEServer {
	s := &oldSSEServer{}
	// One reply channel per GET stream, named in the endpoint it hands out,
	// as a real HTTP+SSE server routes by session. A single shared channel
	// let a stream the client had already closed -- whose handler had not
	// yet noticed -- take the reply meant for the new one, and the new
	// client waited out its whole budget for an initialize answer that had
	// gone to a dead connection (#272).
	var (
		smu      sync.Mutex
		sessions = map[string]chan []byte{}
		next     int
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.mu.Lock()
			s.refused++
			s.mu.Unlock()
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		out := make(chan []byte, 16)
		smu.Lock()
		next++
		id := fmt.Sprint(next)
		sessions[id] = out
		smu.Unlock()
		defer func() {
			smu.Lock()
			delete(sessions, id)
			smu.Unlock()
		}()
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		fmt.Fprintf(w, "event: endpoint\ndata: /message?session=%s\n\n", id)
		f.Flush()
		for {
			select {
			case b := <-out:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				f.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		smu.Lock()
		out, ok := sessions[r.URL.Query().Get("session")]
		smu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var f struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(b, &f)
		w.WriteHeader(http.StatusAccepted)
		if len(f.ID) == 0 || f.Method == "" {
			return
		}
		var res any = map[string]any{}
		switch f.Method {
		case "initialize":
			res = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "old", "version": "1"}}
		case "tools/list":
			res = map[string]any{"tools": []any{map[string]any{"name": "t", "inputSchema": map[string]any{"type": "object"}}}}
		}
		reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": res})
		out <- reply
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *oldSSEServer) refusals() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.refused
	s.refused = 0
	return n
}

// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#backwards-compatibility
func TestHTTPSSEFallback(t *testing.T) {
	t.Run("2025-11-25/transport-http-sse/client-falls-back-to-get-endpoint-and-caches-it", func(t *testing.T) {
		srv := newOldSSEServer(t)
		cfg := &config.Config{MCPServers: map[string]*config.Server{"old": {Name: "old", URL: srv.URL + "/mcp"}}}
		r, err := cfg.Resolve("old")
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "eras.json")
		p := pool.New(r)
		p.Hooks = &pool.Hooks{Eras: pool.OpenEraFile(file)}
		defer p.Close()

		startOnce(t, p)
		if n := srv.refusals(); n == 0 {
			t.Fatal("the cold start should have tried Streamable HTTP first")
		}
		rec := readEraFile(t, file)[pool.Identity(r)]
		if rec.Transport != pool.TransportHTTPSSE || rec.Era != mcpclient.EraLegacy || rec.Version != "2024-11-05" {
			t.Fatalf("record = %+v", rec)
		}
		startOnce(t, p)
		if n := srv.refusals(); n != 0 {
			t.Errorf("a cached HTTP+SSE endpoint was probed again (%d refused POSTs)", n)
		}
		tools, _, err := p.RefreshSchemas(ctx20(t))
		if err != nil || len(tools) != 1 {
			t.Fatalf("tools %v err %v", tools, err)
		}
	})
}

func ctx20(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
