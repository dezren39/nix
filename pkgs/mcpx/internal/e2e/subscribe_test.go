package e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Resource subscriptions reaching the upstream server (#241).
//
// mcpx declared resources.subscribe to its clients and never subscribed
// upstream, so no notifications/resources/updated ever flowed. These drive
// all three ways a client subscribes -- legacy resources/subscribe on stdio,
// the same on Streamable HTTP with the update on the GET stream, and a
// 2026-07-28 subscriptions/listen stream -- against a fake upstream that
// sends updates only for what it was subscribed to, and log every
// subscribe/unsubscribe it received.

// subscribeEnv is a daemon with two servers: demo declares
// resources.subscribe, plain does not.
func subscribeEnv(t *testing.T) (*env, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "subs.log")
	// \u0046 is F: newEnv replaces every FAKE with the fake's path.
	cfg := fmt.Sprintf(`{"mcpServers":{
  "demo": {"command": "FAKE", "env": {"\u0046AKEMCP_SUBSCRIBE": "1", "\u0046AKEMCP_UPDATE_EVERY": "100ms", "\u0046AKEMCP_SUB_LOG": %q},
           "mcpx": {"sharing": "shared", "scope": "global"}},
  "plain": {"command": "FAKE", "mcpx": {"sharing": "shared", "scope": "global"}}
}}`, log)
	e := newEnv(t, cfg)
	e.run("refresh")
	return e, log
}

// The absolute-path resource, as mcpx's listing names it: the leading "/"
// of /abs/doc is dropped when it is namespaced.
const absURI = "mcpx://demo/abs/doc"

// subLines returns the fake's subscribe log, without the pid column.
func subLines(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if _, rest, ok := strings.Cut(l, " "); ok {
			out = append(out, rest)
		}
	}
	return out
}

func count(lines []string, want string) int {
	n := 0
	for _, l := range lines {
		if l == want {
			n++
		}
	}
	return n
}

// waitLog waits for the fake to have logged want at least n times.
func waitLog(t *testing.T, log, want string, n int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if count(subLines(t, log), want) >= n {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("upstream never logged %q x%d; log:\n%s", want, n, strings.Join(subLines(t, log), "\n"))
}

// frames decodes newline-delimited JSON-RPC frames.
func frames(r io.Reader) <-chan map[string]any {
	ch := make(chan map[string]any, 64)
	go func() {
		defer close(ch)
		dec := json.NewDecoder(r)
		for {
			var f map[string]any
			if dec.Decode(&f) != nil {
				return
			}
			ch <- f
		}
	}()
	return ch
}

// sseFrames decodes the data lines of an SSE body.
func sseFrames(r io.Reader) <-chan map[string]any {
	ch := make(chan map[string]any, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data:")
			if !ok {
				continue
			}
			var f map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &f) == nil {
				ch <- f
			}
		}
	}()
	return ch
}

// await returns the first frame match accepts.
func await(t *testing.T, ch <-chan map[string]any, what string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	timeout := time.After(20 * time.Second)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("stream ended waiting for %s", what)
			}
			if match(f) {
				return f
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func isUpdate(uri string) func(map[string]any) bool {
	return func(f map[string]any) bool {
		p, _ := f["params"].(map[string]any)
		return f["method"] == "notifications/resources/updated" && p["uri"] == uri
	}
}

func isReply(id float64) func(map[string]any) bool {
	return func(f map[string]any) bool { return f["id"] == id && f["method"] == nil }
}

// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#subscriptions
func TestResourceSubscriptionsReachUpstream(t *testing.T) {
	t.Run("2025-11-25/resources/stdio-subscribe-delivers-upstream-updates-under-the-mcpx-uri", func(t *testing.T) {
		e, log := subscribeEnv(t)
		cmd := exec.Command(e.mcpx, "serve")
		cmd.Dir = e.dir
		cmd.Env = e.envVars
		stdin, _ := cmd.StdinPipe()
		stdout, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		in := frames(stdout)
		write := func(s string) {
			if _, err := stdin.Write([]byte(s + "\n")); err != nil {
				t.Fatal(err)
			}
		}
		write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
		init := await(t, in, "initialize", isReply(1))
		caps, _ := init["result"].(map[string]any)["capabilities"].(map[string]any)
		if res, _ := caps["resources"].(map[string]any); res["subscribe"] != true {
			t.Fatalf("resources.subscribe not declared: %v", caps)
		}
		write(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		write(`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`)
		list := await(t, in, "resources/list", isReply(2))
		if !strings.Contains(fmt.Sprint(list), absURI) {
			t.Fatalf("the absolute-path resource is not listed as %s: %v", absURI, list)
		}
		// The listing's spelling must also be readable: it names /abs/doc,
		// which the server knows with its leading slash.
		write(`{"jsonrpc":"2.0","id":30,"method":"resources/read","params":{"uri":"` + absURI + `"}}`)
		if r := await(t, in, "read reply", isReply(30)); r["error"] != nil {
			t.Fatalf("reading the listed absolute-path resource: %v", r)
		}
		write(`{"jsonrpc":"2.0","id":3,"method":"resources/subscribe","params":{"uri":"` + absURI + `"}}`)
		if r := await(t, in, "subscribe reply", isReply(3)); r["error"] != nil {
			t.Fatalf("subscribe: %v", r)
		}
		await(t, in, "an update for "+absURI, isUpdate(absURI))
		if n := count(subLines(t, log), "subscribe /abs/doc"); n != 1 {
			t.Errorf("upstream subscribed %d times, want 1: %v", n, subLines(t, log))
		}

		// A server that does not declare resources.subscribe: the legacy
		// subscribe still succeeds (#251 -- it is a standing interest, and
		// legacy has no way to say "agreed, but nothing will come"), and
		// the daemon records why nothing will arrive.
		write(`{"jsonrpc":"2.0","id":4,"method":"resources/subscribe","params":{"uri":"mcpx://plain/demo://greeting"}}`)
		if r := await(t, in, "subscribe reply", isReply(4)); r["error"] != nil {
			t.Errorf("subscribing where upstream cannot: %v", r)
		}
		warned := func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://mcpx/v1/events?kinds=server.log&since=0", nil)
			resp, err := e.socketClient(t).Do(req)
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if strings.Contains(sc.Text(), "no updates will be delivered for mcpx://plain/demo://greeting") {
					return true
				}
			}
			return false
		}
		if !warned() {
			t.Errorf("no warning recorded for a subscription whose updates cannot be delivered")
		}

		// The connection ending ends the subscription upstream.
		stdin.Close()
		waitLog(t, log, "unsubscribe /abs/doc", 1)
	})

	t.Run("2025-11-25/resources/http-subscribe-delivers-upstream-updates-on-the-get-stream", func(t *testing.T) {
		e, log := subscribeEnv(t)
		ep := e.endpoint(t)
		init := mcpPost(t, ep, "", map[string]any{"jsonrpc": "2.0", "id": 1,
			"method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25",
				"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}})
		session := init.Header.Get("Mcp-Session-Id")
		init.Body.Close()
		mcpPost(t, ep, session, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}).Body.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ep+"/mcp", nil)
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Mcp-Session-Id", session)
		get, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer get.Body.Close()
		if get.StatusCode != http.StatusOK {
			t.Fatalf("GET stream: %d", get.StatusCode)
		}
		stream := sseFrames(get.Body)

		const uri = "mcpx://demo/demo://greeting"
		r := mcpPost(t, ep, session, map[string]any{"jsonrpc": "2.0", "id": 2,
			"method": "resources/subscribe", "params": map[string]any{"uri": uri}})
		doc := decodeJSON(t, r.Body)
		r.Body.Close()
		if doc["error"] != nil {
			t.Fatalf("subscribe: %s", dumpJSON(t, doc))
		}
		await(t, stream, "an update for "+uri+" on the GET stream", isUpdate(uri))
		waitLog(t, log, "subscribe demo://greeting", 1)

		r = mcpPost(t, ep, session, map[string]any{"jsonrpc": "2.0", "id": 3,
			"method": "resources/unsubscribe", "params": map[string]any{"uri": uri}})
		r.Body.Close()
		waitLog(t, log, "unsubscribe demo://greeting", 1)
	})

	t.Run("2026-07-28/subscriptions/listen-delivers-upstream-updates-and-acks-only-what-it-can", func(t *testing.T) {
		e, log := subscribeEnv(t)
		ep := e.endpoint(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stream := openListen(t, ctx, ep, 7, absURI, "mcpx://plain/demo://greeting")

		ack := await(t, stream, "acknowledgement", func(f map[string]any) bool {
			return f["method"] == "notifications/subscriptions/acknowledged"
		})
		n, _ := ack["params"].(map[string]any)["notifications"].(map[string]any)
		if got := fmt.Sprint(n["resourceSubscriptions"]); got != "["+absURI+"]" {
			t.Errorf("agreed %s; want only %s, the one whose server declares subscribe", got, absURI)
		}
		up := await(t, stream, "an update for "+absURI, isUpdate(absURI))
		meta, _ := up["params"].(map[string]any)["_meta"].(map[string]any)
		if meta["io.modelcontextprotocol/subscriptionId"] != float64(7) {
			t.Errorf("update not tagged with the listen id: %v", up)
		}
		cancel()
		waitLog(t, log, "unsubscribe /abs/doc", 1)
	})

	t.Run("2025-11-25/resources/upstream-subscription-is-counted-across-clients", func(t *testing.T) {
		e, log := subscribeEnv(t)
		ep := e.endpoint(t)
		ctxA, cancelA := context.WithCancel(context.Background())
		defer cancelA()
		a := openListen(t, ctxA, ep, 1, absURI)
		await(t, a, "update on A", isUpdate(absURI))
		ctxB, cancelB := context.WithCancel(context.Background())
		defer cancelB()
		b := openListen(t, ctxB, ep, 2, absURI)
		await(t, b, "update on B", isUpdate(absURI))
		if n := count(subLines(t, log), "subscribe /abs/doc"); n != 1 {
			t.Errorf("two clients, %d upstream subscriptions; want 1: %v", n, subLines(t, log))
		}
		cancelA()
		// B still wants it: updates keep arriving and nothing unsubscribed.
		for range 3 {
			await(t, b, "update on B after A left", isUpdate(absURI))
		}
		if n := count(subLines(t, log), "unsubscribe /abs/doc"); n != 0 {
			t.Errorf("unsubscribed upstream while B still listens: %v", subLines(t, log))
		}
		cancelB()
		waitLog(t, log, "unsubscribe /abs/doc", 1)
	})

	t.Run("2025-11-25/resources/upstream-subscription-survives-a-server-restart", func(t *testing.T) {
		e, log := subscribeEnv(t)
		ep := e.endpoint(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := openListen(t, ctx, ep, 1, absURI)
		await(t, s, "update before restart", isUpdate(absURI))
		e.run("restart", "demo")
		waitLog(t, log, "subscribe /abs/doc", 2)
		pids := map[string]bool{}
		b, _ := os.ReadFile(log)
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if pid, rest, _ := strings.Cut(l, " "); rest == "subscribe /abs/doc" {
				pids[pid] = true
			}
		}
		if len(pids) != 2 {
			t.Errorf("want the replacement process subscribed: %s", b)
		}
		// Drain what the old process sent, then hear the new one.
		time.Sleep(300 * time.Millisecond)
		await(t, s, "update after restart", isUpdate(absURI))
	})
}

// openListen opens a 2026-07-28 subscriptions/listen stream on /mcp.
func openListen(t *testing.T, ctx context.Context, ep string, id int, uris ...string) <-chan map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id,
		"method": "subscriptions/listen", "params": map[string]any{
			"_meta": modernMeta(), "notifications": map[string]any{"resourceSubscriptions": uris}}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ep+"/mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("listen: %d %q %s", resp.StatusCode, ct, b)
	}
	return sseFrames(resp.Body)
}
