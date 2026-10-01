package daemon

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// RelayContentType is the /v1/call response that carries notifications
// before its result: one JSON object per line, each a RelayFrame.
const RelayContentType = "application/x-ndjson"

// CallRelay is what a /v1/call caller relays from its own MCP client: the
// progress token, the log level it asked for, and trace context. Only
// mcpx's MCP server sends it; every other caller leaves it out and gets the
// plain JSON reply it always has, so a CLI call's output is unchanged.
type CallRelay struct {
	ProgressToken json.RawMessage            `json:"progressToken,omitempty"`
	LogLevel      string                     `json:"logLevel,omitempty"`
	Meta          map[string]json.RawMessage `json:"meta,omitempty"`
}

// RelayFrame is one line of a relayed /v1/call response: a notification
// from the upstream, or -- last -- the status and body the call would have
// been answered with as plain JSON.
type RelayFrame struct {
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Status int             `json:"status,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// relayWriter turns a /v1/call response into a stream the first time the
// upstream sends something to relay, and not before: a call that produced
// no notification is answered exactly as one without a relay.
type relayWriter struct {
	w         http.ResponseWriter
	mu        sync.Mutex
	streaming bool
	done      bool
}

func (rw *relayWriter) note(method string, params json.RawMessage) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.done {
		return
	}
	if !rw.streaming {
		rw.w.Header().Set("Content-Type", RelayContentType)
		rw.w.Header().Set("Cache-Control", "no-cache")
		rw.w.WriteHeader(http.StatusOK)
		rw.streaming = true
	}
	rw.line(RelayFrame{Method: method, Params: params})
}

func (rw *relayWriter) line(f RelayFrame) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	_, _ = rw.w.Write(append(b, '\n'))
	if fl, ok := rw.w.(http.Flusher); ok {
		fl.Flush()
	}
}

// finish writes the reply: as plain JSON if nothing was relayed, else as
// the stream's last line.
func (rw *relayWriter) finish(status int, v any) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.done = true
	if !rw.streaming {
		writeJSON(rw.w, status, v)
		return
	}
	b, _ := json.Marshal(v)
	rw.line(RelayFrame{Status: status, Body: b})
}

func (rw *relayWriter) relay(r *CallRelay) *mcpclient.Relay {
	return &mcpclient.Relay{
		ProgressToken: r.ProgressToken,
		LogLevel:      r.LogLevel,
		Meta:          r.Meta,
		OnProgress:    func(p json.RawMessage) { rw.note("notifications/progress", p) },
		OnMessage:     func(p json.RawMessage) { rw.note("notifications/message", p) },
	}
}

// relayBuffer holds what an interruptible call (/v1/ask) relayed until the
// next poll collects it. That route answers by long poll, not by stream, so
// a notification waits here and wakes the poll that is waiting for it.
type relayBuffer struct {
	mu     sync.Mutex
	frames []RelayFrame
	wake   chan struct{}
}

func newRelayBuffer() *relayBuffer { return &relayBuffer{wake: make(chan struct{}, 1)} }

func (b *relayBuffer) note(method string, params json.RawMessage) {
	b.mu.Lock()
	b.frames = append(b.frames, RelayFrame{Method: method, Params: params})
	b.mu.Unlock()
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *relayBuffer) pending() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.frames) > 0
}

func (b *relayBuffer) drain() []RelayFrame {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.frames
	b.frames = nil
	return out
}

func (b *relayBuffer) relay(r *CallRelay) *mcpclient.Relay {
	return &mcpclient.Relay{
		ProgressToken: r.ProgressToken,
		LogLevel:      r.LogLevel,
		Meta:          r.Meta,
		OnProgress:    func(p json.RawMessage) { b.note("notifications/progress", p) },
		OnMessage:     func(p json.RawMessage) { b.note("notifications/message", p) },
	}
}

// askRelays are the relay buffers of interruptible calls, by call id.
var askRelays sync.Map
