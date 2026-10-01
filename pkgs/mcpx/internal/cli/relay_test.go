package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// The capabilities of the client mcpx relays for travel to the daemon with
// the call, which is the only way the upstream request can declare them.
func TestRelayedCapabilitiesReachTheDaemon(t *testing.T) {
	got := make(chan string, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get(daemon.ClientCapsHeader)
		_, _ = w.Write([]byte(`{"result":{}}`))
	}))
	defer ts.Close()
	c := NewClientAt(daemon.Paths{}, "", ts.URL)

	ctx := mcpclient.WithClientCapabilities(context.Background(), json.RawMessage(`{"roots":{}}`))
	if _, err := c.Call(ctx, "demo", "t", config.CallContext{}, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if h := <-got; h != `{"roots":{}}` {
		t.Errorf("header = %q", h)
	}
	if _, err := c.Call(context.Background(), "demo", "t", config.CallContext{}, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if h := <-got; h != "" {
		t.Errorf("no relayed client, no header; got %q", h)
	}
}

// A pass-through upstream's JSON-RPC error, as the daemon reports it, comes
// back as the error mcpserver relays verbatim; anything else is unchanged.
func TestAnUpstreamsErrorIsRelayedAsItself(t *testing.T) {
	body, _ := json.Marshal(daemon.CallErrorBody{Error: "mcp error -32021",
		Upstream: &daemon.UpstreamError{Code: -32021, Message: "missing",
			Data: json.RawMessage(`{"requiredCapabilities":{"sampling":{}}}`)}})
	err := relayedFault(&HTTPError{Status: 502, Body: body, Msg: "mcp error -32021"})
	var up *mcpserver.UpstreamError
	if !errors.As(err, &up) || up.Code != -32021 || string(up.Data) != `{"requiredCapabilities":{"sampling":{}}}` {
		t.Fatalf("relayed = %#v", err)
	}

	plain := &HTTPError{Status: 502, Body: []byte(`{"error":"timed out"}`), Msg: "timed out"}
	if relayedFault(plain) != error(plain) {
		t.Error("a failure with no server error behind it must stay what it was")
	}
}
