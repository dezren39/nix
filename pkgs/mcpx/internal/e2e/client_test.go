package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// mcpx as a client of mcpx: one isolated mcpx's daemon /mcp configured as an
// HTTP upstream of a second. The upstream daemon REJECTS a modern POST whose
// Mcp-Method / Mcp-Name headers are missing or wrong (-32020), so a modern
// call that succeeds end to end is a real server agreeing with mcpx's
// client about the 2026-07-28 request headers -- not a fake written to
// match.
//
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#standard-request-headers
func TestMcpxIsAModernClientOfMcpx(t *testing.T) {
	up := newEnv(t, oneServer)
	up.run("ls")
	ep := up.endpoint(t)

	for _, protocol := range []string{"force-modern", "modern", "force-legacy"} {
		t.Run("2026-07-28/transport/mcpx-client-against-mcpx-server-"+protocol, func(t *testing.T) {
			cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"up": map[string]any{
				"url": ep + "/mcp", "protocol": protocol,
				"mcpx": map[string]any{"sharing": "shared", "scope": "global"},
			}}})
			down := newEnv(t, string(cfg))
			if out := down.run("--json", "search", "namespaces"); !strings.Contains(out, "mcpx_namespaces") {
				t.Fatalf("the upstream's tools were not listed:\n%s", out)
			}
			// tools/call carries Mcp-Name; the upstream checks it.
			out := down.run("call", "up.mcpx_namespaces", "{}")
			if !strings.Contains(out, "demo") {
				t.Fatalf("call through the modern upstream: %s", out)
			}
			resp, err := down.socketClient(t).Get("http://mcpx/v1/protocol")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body := decodeJSON(t, resp.Body)
			servers, _ := body["asClient"].(map[string]any)["servers"].([]any)
			want := "modern"
			if protocol == "force-legacy" {
				want = "legacy"
			}
			for _, s := range servers {
				if row, _ := s.(map[string]any); row["server"] == "up" {
					if row["era"] != want {
						t.Fatalf("era %v, want %s: %v", row["era"], want, row)
					}
					return
				}
			}
			t.Fatalf("no row for up:\n%s", dumpJSON(t, body))
		})
	}
}
