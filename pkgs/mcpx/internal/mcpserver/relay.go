package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/dezren39/mcpx/internal/spec"
)

// CallRelay is what a client asked of one tools/call that only the upstream
// server can deliver: progress for its progressToken, log messages at its
// level, and its trace context carried onward. A Backend that reaches an
// upstream reads it with RelayFrom and hands anything that comes back to
// Notify, which writes it to the client on the call's own stream.
type CallRelay struct {
	// ProgressToken is the client's _meta.progressToken, verbatim; nil if it
	// sent none.
	ProgressToken json.RawMessage
	// LogLevel is the least severe level the client wants: its request's
	// _meta logLevel in 2026-07-28, what it set with logging/setLevel before.
	// Empty means it asked for none.
	LogLevel string
	// Meta is the trace context from _meta -- traceparent, tracestate,
	// baggage -- to pass upstream untouched.
	Meta map[string]json.RawMessage
	// Notify sends a notification to the client that made the call.
	Notify func(method string, params json.RawMessage)
}

// traceMetaKeys are the OpenTelemetry keys a request's _meta may carry.
var traceMetaKeys = []string{"traceparent", "tracestate", "baggage"}

type relayKey struct{}

// RelayFrom is the relay for the call ctx belongs to, or nil.
func RelayFrom(ctx context.Context) *CallRelay {
	r, _ := ctx.Value(relayKey{}).(*CallRelay)
	return r
}

// withRelay attaches a relay for a tools/call when the client asked for
// anything one could carry, and the call has somewhere to send it.
func (s *Server) withRelay(ctx context.Context, c *Conn, req request, p Peer) context.Context {
	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	_ = json.Unmarshal(req.Params, &params)
	r := &CallRelay{}
	if tok, ok := params.Meta["progressToken"]; ok && string(tok) != "null" {
		r.ProgressToken = tok
	}
	if p.Modern {
		if raw, ok := params.Meta[MetaLogLevel]; ok {
			_ = json.Unmarshal(raw, &r.LogLevel)
		}
	} else if c != nil {
		c.mu.Lock()
		r.LogLevel = c.logLevel
		c.mu.Unlock()
	}
	for _, k := range traceMetaKeys {
		if v, ok := params.Meta[k]; ok {
			if r.Meta == nil {
				r.Meta = map[string]json.RawMessage{}
			}
			r.Meta[k] = v
		}
	}
	if r.ProgressToken == nil && r.LogLevel == "" && r.Meta == nil {
		return ctx
	}
	send := senderFrom(ctx)
	if send == nil && c != nil {
		c.mu.Lock()
		send = c.send
		c.mu.Unlock()
	}
	if send != nil {
		version := p.Version
		r.Notify = func(method string, params json.RawMessage) {
			_ = send(map[string]any{"jsonrpc": "2.0", "method": method, "params": downgradeNotification(method, params, version)})
		}
	} else {
		// Nowhere to deliver: still carry the trace context upstream.
		r.ProgressToken, r.LogLevel = nil, ""
	}
	return context.WithValue(ctx, relayKey{}, r)
}

// downgradeNotification fits a relayed notification to the client's
// revision. The upstream may speak a newer one than the client: progress
// with a message, relayed verbatim to a 2024-11-05 client, is a frame that
// revision does not define.
//
// Only when 2024-11-05 is first in spec.precedence and held strictly. Every
// 2024-era client in the wild ignores the extra field, and the message is the
// only human-readable part of a progress report, so under the default
// (2026-07-28 first) it is sent; a user who wants the 2024-11-05 schema to
// the letter puts that revision first (#307).
func downgradeNotification(method string, params json.RawMessage, version string) json.RawMessage {
	if method != "notifications/progress" || Defines(version, FeatProgressMessage) {
		return params
	}
	// 2024-11-05 has no `message` on progress; every later revision does.
	// Stripped only when that revision's rule governs (see spec.Governs).
	if !spec.Current().Governs(version) {
		return params
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(params, &m) != nil {
		return params
	}
	if _, has := m["message"]; !has {
		return params
	}
	delete(m, "message")
	if b, err := json.Marshal(m); err == nil {
		return b
	}
	return params
}
