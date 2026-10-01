package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
)

// Relay carries what a downstream client asked of one call through to the
// upstream server, and what the upstream sends back during it.
//
// mcpx is a proxy: a host that asks for progress, or for log messages, is
// asking the server that actually does the work. Without this the host's
// progressToken was stripped, so the upstream never reported progress, and
// the upstream's log messages stopped at the daemon's event log.
type Relay struct {
	// ProgressToken is the downstream client's token, verbatim. When set,
	// the upstream request carries a token of mcpx's own -- unique across
	// every call on the connection, which two hosts' tokens are not -- and
	// progress for it is handed to OnProgress with this token restored.
	ProgressToken json.RawMessage
	// LogLevel is the least severe level the downstream client wants; empty
	// means it asked for none, and OnMessage is never called.
	LogLevel string
	// Meta is further _meta passed through untouched: traceparent,
	// tracestate and baggage.
	Meta map[string]json.RawMessage
	// OnProgress receives notifications/progress params for this call.
	OnProgress func(params json.RawMessage)
	// OnMessage receives notifications/message params at or above LogLevel.
	OnMessage func(params json.RawMessage)
}

// TraceMetaKeys are the OpenTelemetry context keys 2026-07-28 reserves in
// _meta. A proxy that drops them breaks the trace at itself.
var TraceMetaKeys = []string{"traceparent", "tracestate", "baggage"}

type relayKey struct{}

// WithRelay attaches a relay to the calls made under ctx.
func WithRelay(ctx context.Context, r *Relay) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, relayKey{}, r)
}

func relayFrom(ctx context.Context) *Relay {
	r, _ := ctx.Value(relayKey{}).(*Relay)
	return r
}

// logSeverity orders the eight syslog levels MCP uses, least severe first.
var logSeverity = map[string]int{
	"debug": 0, "info": 1, "notice": 2, "warning": 3,
	"error": 4, "critical": 5, "alert": 6, "emergency": 7,
}

// LogAtLeast reports whether level is at or above min. An unknown level is
// let through: dropping a message for a spelling is worse than showing it.
func LogAtLeast(level, min string) bool {
	l, ok := logSeverity[level]
	m, mok := logSeverity[min]
	return !ok || !mok || l >= m
}

// moreVerbose is the less severe of two levels, empty counting as none.
func moreVerbose(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" || LogAtLeast(b, a) {
		return a
	}
	return b
}

// beginRelay registers r for one call and returns params with the relay's
// _meta merged in, and a function that unregisters it.
func (c *Client) beginRelay(r *Relay, params json.RawMessage) (json.RawMessage, func(), error) {
	if r == nil {
		return params, func() {}, nil
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(params, &m); err != nil {
		return nil, nil, err
	}
	meta := map[string]json.RawMessage{}
	if raw, ok := m["_meta"]; ok {
		_ = json.Unmarshal(raw, &meta)
	}
	for k, v := range r.Meta {
		meta[k] = v
	}
	var token string
	if len(r.ProgressToken) > 0 && r.OnProgress != nil {
		token = fmt.Sprintf("mcpx-%d", c.relaySeq.Add(1))
		meta["progressToken"], _ = json.Marshal(token)
	}
	if c.metaVersion != "" && r.LogLevel != "" {
		// 2026-07-28 logs only for a request that names a level. The more
		// verbose of the host's and the daemon's own wins, so neither loses
		// what it asked for; OnMessage filters back down to the host's.
		c.mu.Lock()
		lvl := moreVerbose(r.LogLevel, c.logLevel)
		c.mu.Unlock()
		meta[MetaLogLevel], _ = json.Marshal(lvl)
	}
	if len(meta) > 0 {
		b, err := json.Marshal(meta)
		if err != nil {
			return nil, nil, err
		}
		m["_meta"] = b
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	if c.relays == nil {
		c.relays = map[*Relay]string{}
	}
	c.relays[r] = token
	c.mu.Unlock()
	return out, func() {
		c.mu.Lock()
		delete(c.relays, r)
		c.mu.Unlock()
	}, nil
}

// relayNotification hands a notification to the calls that asked for it.
//
// Progress goes to the one call whose token it carries, with the host's own
// token put back. A log message names no request, so it goes to every call
// in flight on this connection whose host asked for that level: on a legacy
// session logging is session-wide anyway, and that is the only association
// the protocol offers.
func (c *Client) relayNotification(method string, params json.RawMessage) {
	c.mu.Lock()
	if len(c.relays) == 0 {
		c.mu.Unlock()
		return
	}
	type target struct {
		r     *Relay
		token string
	}
	targets := make([]target, 0, len(c.relays))
	for r, tok := range c.relays {
		targets = append(targets, target{r, tok})
	}
	c.mu.Unlock()

	switch method {
	case "notifications/progress":
		var p map[string]json.RawMessage
		if json.Unmarshal(params, &p) != nil {
			return
		}
		var tok string
		if json.Unmarshal(p["progressToken"], &tok) != nil || tok == "" {
			return
		}
		for _, t := range targets {
			if t.token == tok {
				p["progressToken"] = t.r.ProgressToken
				if b, err := json.Marshal(p); err == nil {
					t.r.OnProgress(b)
				}
				return
			}
		}
	case "notifications/message":
		var m struct {
			Level string `json:"level"`
		}
		_ = json.Unmarshal(params, &m)
		for _, t := range targets {
			if t.r.OnMessage != nil && t.r.LogLevel != "" && LogAtLeast(m.Level, t.r.LogLevel) {
				t.r.OnMessage(params)
			}
		}
	}
}
