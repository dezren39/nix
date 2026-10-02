package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/testsupport"
)

// eraEnv is an isolated mcpx whose one server is eramcp in the given mode.
func eraEnv(t *testing.T, mode, protocol string, extra ...string) (*env, string) {
	t.Helper()
	bin := testsupport.EraMCPBinary(t)
	log := filepath.Join(t.TempDir(), "frames")
	server := map[string]any{"command": bin, "env": map[string]string{
		"ERAMCP_MODE": mode, "ERAMCP_LOG": log,
	}}
	if protocol != "" {
		server["protocol"] = protocol
	}
	cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"era": server}})
	e := newEnv(t, string(cfg))
	e.envVars = append(e.envVars, extra...)
	return e, log
}

// upstreamRow reads /v1/protocol's row for the era server.
func upstreamRow(t *testing.T, e *env) map[string]any {
	t.Helper()
	resp, err := e.socketClient(t).Get("http://mcpx/v1/protocol")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := decodeJSON(t, resp.Body)
	asClient, _ := body["asClient"].(map[string]any)
	servers, _ := asClient["servers"].([]any)
	for _, s := range servers {
		if row, _ := s.(map[string]any); row["server"] == "era" {
			return row
		}
	}
	t.Fatalf("no row for era:\n%s", dumpJSON(t, body))
	return nil
}

func requests(t *testing.T, log string) []string {
	t.Helper()
	b, _ := os.ReadFile(log)
	_ = os.Remove(log)
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "" && !strings.HasPrefix(l, "notifications/") {
			out = append(out, l)
		}
	}
	return out
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions
func TestTheEraIsProbedOnceAndRememberedAcrossDaemonRestarts(t *testing.T) {
	t.Run("2026-07-28/era-cache/persisted-across-daemon-restart", func(t *testing.T) {
		e, log := eraEnv(t, "legacy-silent", "", "MCPX_UPSTREAM_PROBE_TIMEOUT=300ms")
		if out := e.run("call", "era.hello", "{}"); !strings.Contains(out, "hello from legacy-silent") {
			t.Fatalf("call: %s", out)
		}
		row := upstreamRow(t, e)
		if row["era"] != "legacy" || row["eraSource"] != "probe" || row["preference"] != "prefer-discover" {
			t.Errorf("cold row = %v", row)
		}
		if got := requests(t, log); len(got) < 2 || got[0] != "server/discover" || got[1] != "initialize" {
			t.Errorf("cold frames = %v", got)
		}

		if _, err := os.Stat(filepath.Join(e.dir, "state", "upstream-eras.json")); err != nil {
			t.Errorf("the era should be persisted in the state directory: %v", err)
		}
		e.run("stop", "--all")
		e.run("call", "era.hello", "{}")
		row = upstreamRow(t, e)
		if row["era"] != "legacy" || row["eraSource"] != "cache" {
			t.Errorf("warm row = %v", row)
		}
		cached, _ := row["cached"].(map[string]any)
		if cached["era"] != "legacy" {
			t.Errorf("cached = %v", row["cached"])
		}
		for _, m := range requests(t, log) {
			if m == "server/discover" {
				t.Errorf("a cached legacy server must not be probed again")
			}
		}
	})

	t.Run("2026-07-28/era-cache/eraCache-off-probes-every-start", func(t *testing.T) {
		e, log := eraEnv(t, "legacy-32601", "", "MCPX_UPSTREAM_ERA_CACHE=false")
		e.run("call", "era.hello", "{}")
		e.run("stop", "--all")
		_ = requests(t, log)
		e.run("call", "era.hello", "{}")
		if got := requests(t, log); len(got) == 0 || got[0] != "server/discover" {
			t.Errorf("with the cache off every start probes: %v", got)
		}
		row := upstreamRow(t, e)
		if row["eraSource"] != "probe" || row["cached"] != nil {
			t.Errorf("row = %v", row)
		}
		if _, err := os.Stat(filepath.Join(e.dir, "state", "upstream-eras.json")); !os.IsNotExist(err) {
			t.Errorf("no era file should be written with the cache off: %v", err)
		}
	})

	t.Run("2026-07-28/stdio-compat/legacy-exiting-on-discover-is-reached", func(t *testing.T) {
		e, _ := eraEnv(t, "legacy-exit", "")
		if out := e.run("call", "era.hello", "{}"); !strings.Contains(out, "hello from legacy-exit") {
			t.Fatalf("call: %s", out)
		}
		if row := upstreamRow(t, e); row["era"] != "legacy" {
			t.Errorf("row = %v", row)
		}
	})

	t.Run("2026-07-28/stdio-compat/modern-server-is-reached-modern", func(t *testing.T) {
		e, log := eraEnv(t, "modern", "")
		if out := e.run("call", "era.hello", "{}"); !strings.Contains(out, "hello from modern") {
			t.Fatalf("call: %s", out)
		}
		row := upstreamRow(t, e)
		if row["era"] != "modern" || row["negotiated"] != "2026-07-28" {
			t.Errorf("row = %v", row)
		}
		for _, m := range requests(t, log) {
			if m == "initialize" {
				t.Errorf("a modern server answered discover; initialize should not follow")
			}
		}
	})

	t.Run("upstream/upstream-protocol-setting-is-honoured", func(t *testing.T) {
		e, log := eraEnv(t, "legacy-32601", "", "MCPX_UPSTREAM_PROTOCOL=legacy")
		e.run("call", "era.hello", "{}")
		if got := requests(t, log); len(got) == 0 || got[0] != "initialize" {
			t.Errorf("upstream.protocol=legacy (an alias of prefer-initialize) should send initialize first: %v", got)
		}
		if row := upstreamRow(t, e); row["preference"] != "prefer-initialize" {
			t.Errorf("row = %v", row)
		}
	})

	t.Run("upstream/per-server-protocol-overrides-upstream-protocol", func(t *testing.T) {
		e, log := eraEnv(t, "legacy-32601", "force-legacy", "MCPX_UPSTREAM_PROTOCOL=modern")
		e.run("call", "era.hello", "{}")
		if got := requests(t, log); len(got) == 0 || got[0] != "initialize" {
			t.Errorf("the server's own protocol key should win: %v", got)
		}
		if row := upstreamRow(t, e); row["preference"] != "force-initialize" || row["eraSource"] != "forced" {
			t.Errorf("row = %v", row)
		}
	})
}
