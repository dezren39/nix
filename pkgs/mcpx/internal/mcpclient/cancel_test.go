package mcpclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// TestAHungServerDoesNotOutliveTheCallTimeout is the behaviour a call timeout
// exists to provide. Send is synchronous, so a server that accepts the
// connection and never answers blocks before the caller reaches any timeout
// of its own; the request has to carry the deadline itself.
func TestAHungServerDoesNotOutliveTheCallTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // accept, then never answer
	}))
	// Ordering matters and defer is LIFO: the handler must be released
	// before Close waits for it, or the test deadlocks on its own fixture.
	defer srv.Close()
	defer close(block)

	tr, err := mcpclient.NewHTTP(mcpclient.HTTPOptions{URL: srv.URL, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a hung server should not look like success")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Logf("returned %v", err) // any error is acceptable; hanging is not
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send outlived its context; the call timeout cannot work")
	}
}
