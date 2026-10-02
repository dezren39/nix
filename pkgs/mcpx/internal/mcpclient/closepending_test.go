package mcpclient_test

import (
	"context"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// Close marked the client closed without answering what was in flight. The
// transport then died, and fail -- which does answer pending requests --
// returned early because the client was already closed. Every call waiting on
// a server mcpx was shutting down (a restart, a reload, an idle reap racing a
// call) sat until its own deadline instead of failing at once.
func TestCloseFailsCallsInFlight(t *testing.T) {
	s := newProbeServer(func(method string, _ map[string]any) (any, map[string]any, bool, time.Duration) {
		switch method {
		case "initialize":
			return initOK, nil, false, 0
		case "server/discover":
			return nil, rpcErr(-32601, "no server/discover", nil), false, 0
		}
		return nil, nil, false, 0 // tools/call: never answered
	})
	c, err := probeDial(t, s, mcpclient.Options{})
	if err != nil {
		t.Fatal(err)
	}

	// A deadline far longer than the test is allowed to take: a call that
	// returns at all within the bound below was failed by Close, not by it.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, err := c.CallTool(ctx, "slow", map[string]any{})
		errc <- err
	}()
	// Let the request reach the server before closing.
	for deadline := time.Now().Add(2 * time.Second); !sent(s, "tools/call"); {
		if time.Now().After(deadline) {
			t.Fatal("the call never reached the server")
		}
		time.Sleep(5 * time.Millisecond)
	}

	start := time.Now()
	c.Close()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("a call cut off by Close must fail")
		}
		if ctx.Err() != nil {
			t.Fatalf("the call ended by its own deadline, not by Close: %v", err)
		}
		t.Logf("failed %v after Close: %v", time.Since(start).Round(time.Millisecond), err)
	case <-time.After(5 * time.Second):
		t.Fatal("a call in flight was still waiting 5s after Close; it would have waited out its deadline")
	}
}
