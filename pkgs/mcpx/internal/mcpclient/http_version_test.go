package mcpclient

import (
	"net/http"
	"testing"
)

func TestTheVersionHeaderFollowsTheFrame(t *testing.T) {
	// 2026-07-28 requires MCP-Protocol-Version to match the version in the
	// request's _meta, or the server answers 400. The header used to be a
	// constant, which was right for every legacy frame and wrong for every
	// modern one.
	tr := &HTTPTransport{}
	for frame, want := range map[string]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`: "2026-07-28",
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`:                                                                 ProtocolVersion,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`:                                                                     ProtocolVersion,
		``: ProtocolVersion,
	} {
		req, _ := http.NewRequest(http.MethodPost, "http://x", nil)
		tr.setHeadersFor(req, []byte(frame))
		if got := req.Header.Get("MCP-Protocol-Version"); got != want {
			t.Errorf("%s\n  header %q, want %q", frame, got, want)
		}
	}
}
