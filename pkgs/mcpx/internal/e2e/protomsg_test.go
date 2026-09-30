package e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The message-level requirements of 2026-07-28 and the legacy revisions,
// driven through the daemon's own /mcp endpoint rather than the package.

func modernMeta() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
}

func TestTheDaemonServesModernMessageShapes(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("refresh")
	ep := e.endpoint(t)

	t.Run("2026-07-28/discover/shape-cache-hints-serverInfo-and-no-session", func(t *testing.T) {
		r := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 1,
			"method": "server/discover", "params": map[string]any{"_meta": modernMeta()}})
		doc := decodeJSON(t, r.Body)
		r.Body.Close()
		if s := r.Header.Get("Mcp-Session-Id"); s != "" {
			t.Errorf("a session was issued on discover: %s", s)
		}
		res, _ := doc["result"].(map[string]any)
		meta, _ := res["_meta"].(map[string]any)
		if res["supportedVersions"] == nil || res["ttlMs"] == nil || res["cacheScope"] != "public" ||
			meta["io.modelcontextprotocol/serverInfo"] == nil || res["serverInfo"] != nil {
			t.Errorf("discover:\n%s", dumpJSON(t, doc))
		}
	})

	t.Run("2026-07-28/resources/not-found-is-32602", func(t *testing.T) {
		r := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 2,
			"method": "resources/read", "params": map[string]any{
				"uri": "mcpx://no-such-namespace/x", "_meta": modernMeta()}})
		doc := decodeJSON(t, r.Body)
		r.Body.Close()
		errObj, _ := doc["error"].(map[string]any)
		if errObj["code"] != float64(-32602) {
			t.Errorf("got:\n%s", dumpJSON(t, doc))
		}
	})

	t.Run("2025-11-25/resources/not-found-is-32002", func(t *testing.T) {
		init := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 1,
			"method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25",
				"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}})
		session := init.Header.Get("Mcp-Session-Id")
		init.Body.Close()
		r := mcpPost(t, ep, session, map[string]any{"jsonrpc": "2.0", "id": 2,
			"method": "resources/read", "params": map[string]any{"uri": "not-an-mcpx-uri"}})
		doc := decodeJSON(t, r.Body)
		r.Body.Close()
		errObj, _ := doc["error"].(map[string]any)
		if errObj["code"] != float64(-32002) {
			t.Errorf("got:\n%s", dumpJSON(t, doc))
		}
	})

	t.Run("2025-11-25/lifecycle/initialize-unsupported-version-answers-latest", func(t *testing.T) {
		r := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 1,
			"method": "initialize", "params": map[string]any{"protocolVersion": "1999-01-01",
				"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}})
		doc := decodeJSON(t, r.Body)
		r.Body.Close()
		res, _ := doc["result"].(map[string]any)
		if res["protocolVersion"] != "2025-11-25" {
			t.Errorf("got:\n%s", dumpJSON(t, doc))
		}
	})

	t.Run("2026-07-28/subscriptions/http-listen-is-an-sse-stream-until-the-client-closes-it", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 5,
			"method": "subscriptions/listen", "params": map[string]any{
				"_meta": modernMeta(), "notifications": map[string]any{"toolsListChanged": true}}})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ep+"/mcp", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		// Required on every modern POST by the Streamable HTTP transport.
		req.Header.Set("MCP-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", "subscriptions/listen")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("status %d, content type %q", resp.StatusCode, ct)
		}
		got := make(chan string, 1)
		go func() {
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if strings.HasPrefix(sc.Text(), "data:") {
					got <- sc.Text()
					return
				}
			}
		}()
		select {
		case line := <-got:
			if !strings.Contains(line, "notifications/subscriptions/acknowledged") ||
				!strings.Contains(line, `"io.modelcontextprotocol/subscriptionId":5`) {
				t.Errorf("first event: %s", line)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("no acknowledgement on the stream")
		}
		cancel()
	})
}
