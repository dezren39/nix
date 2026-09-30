package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /v1/events must fail loudly on a writer it cannot flush.
//
// This was the first suspect for a CI-only failure where `mcpx events` wrote
// nothing and said nothing: without an http.Flusher the daemon cannot hold a
// response open, and a handler that simply returned would end the stream the
// instant it began. It is not what happened -- net/http's own ResponseWriter
// is always a Flusher over HTTP/1, and trackActivity, the only middleware in
// front of this route, passes the writer through unwrapped -- but the reason
// it could not have happened is one wrapper away from being untrue.
//
// So the branch is pinned: a non-flushable writer gets a 500 with a reason,
// which the client reports as an HTTP error, rather than 200 and an empty
// body, which the client cannot tell from a stream nobody has published to.
func TestEventsRefusesAWriterItCannotFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	s := &Server{}
	s.handleEvents(noFlush{rec}, httptest.NewRequest("GET", "/v1/events", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a writer that cannot stream should be a 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "streaming") {
		t.Errorf("the 500 should say why, got %q", rec.Body.String())
	}
}

// noFlush is an http.ResponseWriter and nothing more. The recorder is a named
// field rather than an embedded one on purpose: embedding would promote the
// recorder's own Flush and the type would satisfy http.Flusher after all,
// which is the one thing this test needs it not to do.
type noFlush struct {
	rec *httptest.ResponseRecorder
}

func (n noFlush) Header() http.Header         { return n.rec.Header() }
func (n noFlush) Write(b []byte) (int, error) { return n.rec.Write(b) }
func (n noFlush) WriteHeader(code int)        { n.rec.WriteHeader(code) }
