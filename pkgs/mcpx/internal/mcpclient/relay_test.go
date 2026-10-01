package mcpclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// metaCaps is the clientCapabilities the last request of method declared.
func metaCaps(t *testing.T, s *scriptedServer, method string) map[string]any {
	t.Helper()
	var last map[string]any
	for _, r := range s.requests() {
		if r["method"] == method {
			last = r
		}
	}
	if last == nil {
		t.Fatalf("no %s request was sent", method)
	}
	params, _ := last["params"].(map[string]any)
	meta, _ := params["_meta"].(map[string]any)
	caps, _ := meta[mcpclient.MetaClientCapabilities].(map[string]any)
	return caps
}

// Relaying for a client, mcpx declares no more than that client did. The
// official suite's test_missing_capability ran, where it should have been
// refused -32021, because mcpx declared sampling on behalf of a client that
// declared nothing.
func TestARelayedClientsCapabilitiesBoundWhatIsDeclared(t *testing.T) {
	s := newScripted(func(method string, params map[string]any) any {
		switch method {
		case "server/discover":
			return modernDiscover(params)
		case "tools/call":
			return map[string]any{"resultType": "complete", "content": []any{}}
		}
		return nil
	})
	c := dialModern(t, s, mcpclient.Options{
		OnServerRequest: func(context.Context, string, json.RawMessage) (any, error) { return nil, nil },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := c.CallTool(ctx, "t", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	own := metaCaps(t, s, "tools/call")
	if own["sampling"] == nil || own["elicitation"] == nil || own["roots"] == nil {
		t.Fatalf("premise: with a handler mcpx declares sampling, elicitation and roots, got %v", own)
	}

	relayed := mcpclient.WithClientCapabilities(ctx, json.RawMessage(`{"elicitation":{},"experimental":{}}`))
	if _, err := c.CallTool(relayed, "t", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	got := metaCaps(t, s, "tools/call")
	if got["sampling"] != nil || got["roots"] != nil {
		t.Errorf("declared what the relayed client did not: %v", got)
	}
	if got["elicitation"] == nil {
		t.Errorf("dropped what both declared: %v", got)
	}
	if got["experimental"] != nil {
		t.Errorf("declared what mcpx itself cannot answer: %v", got)
	}
}

// A roots/list in an input_required round goes to the handler when the
// relayed client declared roots -- its roots are the ones that matter -- and
// falls back to mcpx's own when the handler cannot relay it or the client
// declared none. It used to be answered from mcpx's configuration always,
// so the client was never asked.
func TestRootsAreRelayedForAClientThatDeclaredThem(t *testing.T) {
	s := newScripted(func(method string, params map[string]any) any {
		switch method {
		case "server/discover":
			return modernDiscover(params)
		case "tools/call":
			resp, _ := params["inputResponses"].(map[string]any)
			if resp == nil {
				return map[string]any{"resultType": "input_required",
					"inputRequests": map[string]any{
						"where": map[string]any{"method": "roots/list", "params": map[string]any{}}}}
			}
			b, _ := json.Marshal(resp)
			return map[string]any{"resultType": "complete",
				"content": []any{map[string]any{"type": "text", "text": string(b)}}}
		}
		return nil
	})
	var mu sync.Mutex
	asked := 0
	relay := true
	c := dialModern(t, s, mcpclient.Options{
		Roots: []mcpclient.Root{{URI: "file:///mcpx", Name: "mcpx"}},
		OnServerRequest: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			if method != "roots/list" {
				return nil, nil
			}
			asked++
			if !relay {
				return nil, mcpclient.ErrNotRelayed
			}
			return map[string]any{"roots": []any{map[string]any{"uri": "file:///client"}}}, nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	declared := mcpclient.WithClientCapabilities(ctx, json.RawMessage(`{"roots":{}}`))

	out, err := c.CallTool(declared, "t", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if asked != 1 || !strings.Contains(string(out), "file:///client") {
		t.Fatalf("roots should come from the relayed client (asked %d):\n%s", asked, out)
	}

	relay = false
	out, err = c.CallTool(declared, "t", map[string]any{})
	if err != nil || !strings.Contains(string(out), "file:///mcpx") {
		t.Fatalf("a roots question nobody could relay is answered with mcpx's own: %v\n%s", err, out)
	}

	asked = 0
	out, err = c.CallTool(ctx, "t", map[string]any{})
	if err != nil || asked != 0 || !strings.Contains(string(out), "file:///mcpx") {
		t.Fatalf("with no relayed client, mcpx answers itself (asked %d): %v\n%s", asked, err, out)
	}
}

// A JSON body that carries more than one message is delivered message by
// message, as each arrives. The official suite's reference server streams
// subscriptions/listen as newline-delimited JSON under application/json;
// reading to EOF meant the acknowledgement and every list_changed after it
// sat unread for as long as the subscription lasted, which is forever.
func TestAJSONBodyIsReadMessageByMessage(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/subscriptions/acknowledged","params":{}}` + "\n"))
		f.Flush()
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/prompts/list_changed","params":{}}` + "\n"))
		f.Flush()
		<-release
	}))
	defer ts.Close()
	defer close(release)

	tr, err := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sent := make(chan error, 1)
	go func() {
		sent <- tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"subscriptions/listen","params":{}}`))
	}()

	got := make(chan []byte, 2)
	go func() {
		for i := 0; i < 2; i++ {
			b, err := tr.Recv()
			if err != nil {
				return
			}
			got <- b
		}
	}()
	for _, want := range []string{"acknowledged", "prompts/list_changed"} {
		select {
		case b := <-got:
			if !strings.Contains(string(b), want) {
				t.Fatalf("got %s, want %s", b, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s was not delivered while the body stayed open", want)
		}
	}
	select {
	case err := <-sent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Send should return once the first message is read, not at EOF")
	}

	// An ordinary single-object answer is still one message.
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`))
	}))
	defer ts2.Close()
	tr2, err := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: ts2.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer tr2.Close()
	if err := tr2.Send(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	if b, err := tr2.Recv(); err != nil || !strings.Contains(string(b), `"id":2`) {
		t.Fatalf("single answer: %s %v", b, err)
	}
}
