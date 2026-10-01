package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// endpointOf is the daemon's loopback TCP address, as it reports it.
func endpointOf(t *testing.T, e *env) string {
	t.Helper()
	var h struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.Unmarshal([]byte(e.run("--json", "status")), &h); err != nil || h.Endpoint == "" {
		// status carries it too; health is the fallback.
		c := e.socketClient(t)
		resp, gerr := c.Get("http://mcpx/v1/health")
		if gerr != nil {
			t.Fatal(gerr)
		}
		defer resp.Body.Close()
		if derr := json.NewDecoder(resp.Body).Decode(&h); derr != nil || h.Endpoint == "" {
			t.Fatalf("no endpoint: %v %v", err, derr)
		}
	}
	return h.Endpoint
}

func postWith(t *testing.T, url, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestAWebPageCannotRunCodeThroughTheDaemon: /v1 is unauthenticated and also
// listens on loopback TCP. A page in the user's browser could POST a
// text/plain body -- a "simple" request, no preflight -- to /v1/exec, and the
// handler read it as JSON and ran it. /mcp on the same listener already
// refused foreign origins; /v1 did not.
func TestAWebPageCannotRunCodeThroughTheDaemon(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	ep := endpointOf(t, e)
	src := `{"source":"console.log('ran-' + 'here')"}`

	code, body := postWith(t, ep+"/v1/exec", src,
		map[string]string{"Origin": "https://evil.example", "Content-Type": "text/plain"})
	if code != http.StatusForbidden || strings.Contains(body, "ran-here") {
		t.Fatalf("a foreign origin reached /v1/exec: %d %s", code, body)
	}

	// DNS rebinding: a page whose own name now resolves to 127.0.0.1 is
	// same-origin to itself and sends no Origin on a GET.
	req, _ := http.NewRequest(http.MethodGet, ep+"/v1/log?limit=1", nil)
	req.Host = "rebound.evil.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a request naming a foreign host was answered: %d", resp.StatusCode)
	}

	// What must keep working: a loopback page, a client that sends no
	// Origin (every CLI, script and SDK), and the socket.
	if code, body := postWith(t, ep+"/v1/exec", src,
		map[string]string{"Origin": "http://localhost:5173", "Content-Type": "application/json"}); code != http.StatusOK || !strings.Contains(body, "ran-here") {
		t.Fatalf("a loopback origin was refused: %d %s", code, body)
	}
	if code, body := postWith(t, ep+"/v1/exec", src,
		map[string]string{"Content-Type": "application/json"}); code != http.StatusOK || !strings.Contains(body, "ran-here") {
		t.Fatalf("a client with no Origin was refused: %d %s", code, body)
	}
	if out := e.run("exec", `console.log("still-" + "fine")`); !strings.Contains(out, "still-fine") {
		t.Fatalf("the CLI stopped working: %s", out)
	}
}

// TestAConfiguredOriginIsServed: transport.allowedOrigins is how a real web
// frontend is let in, on /v1 as on /mcp.
func TestAConfiguredOriginIsServed(t *testing.T) {
	e := newEnv(t, oneServer)
	e.setenv("MCPX_TRANSPORT_ALLOWED_ORIGINS=https://app.example.com")
	e.run("ls")
	ep := endpointOf(t, e)
	code, body := postWith(t, ep+"/v1/exec", `{"source":"console.log('ok-'+'app')"}`,
		map[string]string{"Origin": "https://app.example.com", "Content-Type": "application/json"})
	if code != http.StatusOK || !strings.Contains(body, "ok-app") {
		t.Fatalf("a configured origin was refused: %d %s", code, body)
	}
}

// TestV1AndMCPAgreeOnOrigins: both answer on the same listener, and the two
// policies are built in two places. A page one refuses and the other serves
// is a hole, so the verdicts are compared for the same origins.
func TestV1AndMCPAgreeOnOrigins(t *testing.T) {
	e := newEnv(t, oneServer)
	e.setenv("MCPX_TRANSPORT_ALLOWED_ORIGINS=https://app.example.com")
	e.run("ls")
	ep := endpointOf(t, e)
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for _, origin := range []string{
		"https://evil.example", "http://localhost:3000", "http://127.0.0.1:9",
		"https://app.example.com", "null", "http://10.1.2.3",
	} {
		v1, _ := postWith(t, ep+"/v1/health", "{}", map[string]string{"Origin": origin})
		mcp, _ := postWith(t, ep+"/mcp", ping, map[string]string{"Origin": origin,
			"Content-Type": "application/json", "Accept": "application/json, text/event-stream"})
		v1Refused := v1 == http.StatusForbidden
		mcpRefused := mcp == http.StatusForbidden
		if v1Refused != mcpRefused {
			t.Errorf("origin %s: /v1 answered %d and /mcp %d; they must agree", origin, v1, mcp)
		}
	}
}
