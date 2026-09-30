package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/daemon"
)

// A stream that ends is a failure, not a success.
//
// api.Op.Streams marks "an endpoint whose response never ends", and streamOp
// returned bufio.Scanner's error for the end of it -- which is nil on a clean
// EOF. So a daemon that accepted the connection and hung up immediately made
// `mcpx events` exit 0 having printed nothing and written nothing.
//
// That is indistinguishable, from outside the process, from a healthy stream
// that nobody has published to yet. internal/e2e's
// TestAStreamingCommandWritesToTheFileItWasGiven spent a day being read as
// the first when it was the second, because an empty file and an empty stderr
// is all either one leaves behind.
func TestAStreamThatEndsOnItsOwnIsReported(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		frames int
	}{
		// The subscription never took: accepted, then closed.
		{name: "nothing at all", body: "", frames: 0},
		// Only SSE framing that carries no payload, which is exactly what
		// /v1/events sends before its first event -- so "the file is empty"
		// and "the stream said nothing" are not the same thing.
		{name: "framing only", body: "retry: 5000\n\n", frames: 0},
		// The daemon went away mid-stream. Still an error: the caller asked
		// for a stream that does not end.
		{name: "one event then gone", body: "id: 1\nevent: server\ndata: {\"kind\":\"server.start\"}\n\n", frames: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := NewClientAt(daemon.Paths{}, "", srv.URL)
			var out bytes.Buffer
			err := c.streamOp(context.Background(), "GET", "/v1/events", nil, &out)
			if err == nil {
				t.Fatal("a stream the server closed by itself must not be reported as success; " +
					"this is the exit-0-having-done-nothing case")
			}
			if !strings.Contains(err.Error(), "/v1/events") {
				t.Errorf("the error should name the stream that ended, got: %v", err)
			}
			// The payload still has to arrive; reporting the close must not
			// cost the caller the events that did come.
			if tc.frames > 0 && !strings.Contains(out.String(), `"kind"`) {
				t.Errorf("events received before the close should still reach the writer, got %q", out.String())
			}
		})
	}
}

// Ctrl-C is the ordinary way an event stream ends, and it is not a failure.
// The cancelled-context branch is what keeps the check above from turning
// every normal exit into an error.
func TestACancelledStreamIsNotAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("id: 1\nevent: server\ndata: {\"kind\":\"server.start\"}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := NewClientAt(daemon.Paths{}, "", srv.URL)
	// Cancel once the stream is demonstrably live, so this exercises the
	// Ctrl-C branch rather than racing the request itself.
	var out bytes.Buffer
	w := writeFunc(func(p []byte) (int, error) {
		n, err := out.Write(p)
		cancel()
		return n, err
	})
	if err := c.streamOp(ctx, "GET", "/v1/events", nil, w); err != nil {
		t.Fatalf("cancelling the stream is how it is meant to end, got: %v", err)
	}
	if !strings.Contains(out.String(), `"kind"`) {
		t.Fatalf("the test cancelled before it read anything, so it proves nothing; got %q", out.String())
	}
}

type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(p []byte) (int, error) { return f(p) }
