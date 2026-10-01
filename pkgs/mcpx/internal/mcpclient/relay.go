package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ProgressMinInterval is the least time between two progress notifications
// passed on for one token. An upstream may report as often as it likes; the
// spec has receivers rate-limit so a flood does not reach the host. The last
// report (progress reaching total) is never held back.
const ProgressMinInterval = 20 * time.Millisecond

// ProgressBurst is how many of a call's progress notifications are passed on
// before ProgressMinInterval starts to apply.
//
// A plain interval drops progress a host asked for: a server reporting three
// steps as fast as it does them is not flooding, and the official suite's
// tools-call-with-progress sends exactly that and expects all three
// ("Expected at least 3 progress notifications, got 2", 3 runs of 3). The
// burst lets ordinary reporting through and still bounds a server that sends
// thousands.
const ProgressBurst = 5

// relayState is one call in flight: the token mcpx sent upstream for it, and
// the last progress passed on, which the next must exceed.
type relayState struct {
	token string
	last  float64
	seen  bool
	done  bool // progress reached total: nothing more is passed on
	at    time.Time
	n     int // passed on so far, against ProgressBurst
}

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
		c.relays = map[*Relay]*relayState{}
	}
	c.relays[r] = &relayState{token: token}
	c.mu.Unlock()
	return out, func() {
		c.mu.Lock()
		delete(c.relays, r)
		c.mu.Unlock()
	}, nil
}

// acceptProgress decides whether one notifications/progress is passed on,
// and to which relay. Progress is accepted only for the token of a call
// still in flight -- once the response is in, the relay is gone and so is
// its token -- only when it exceeds the last value passed on (the spec has
// it increase with every notification, and a host must not see it go
// backwards from mcpx), and -- after ProgressBurst of them -- no more often
// than ProgressMinInterval.
func (c *Client) acceptProgress(params json.RawMessage) (*Relay, map[string]json.RawMessage, bool) {
	var p map[string]json.RawMessage
	if json.Unmarshal(params, &p) != nil {
		return nil, nil, false
	}
	var tok string
	if json.Unmarshal(p["progressToken"], &tok) != nil || tok == "" {
		return nil, nil, false
	}
	var v struct {
		Progress *float64 `json:"progress"`
		Total    *float64 `json:"total"`
	}
	if json.Unmarshal(params, &v) != nil || v.Progress == nil {
		return nil, nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for r, st := range c.relays {
		if st.token == "" || st.token != tok {
			continue
		}
		if st.done || (st.seen && *v.Progress <= st.last) {
			return nil, nil, false
		}
		final := v.Total != nil && *v.Progress >= *v.Total
		now := time.Now()
		if st.n >= ProgressBurst && !final && now.Sub(st.at) < ProgressMinInterval {
			return nil, nil, false
		}
		st.last, st.seen, st.done, st.at = *v.Progress, true, final, now
		st.n++
		return r, p, true
	}
	return nil, nil, false
}

// relayNotification hands a notification to the calls that asked for it.
//
// Progress goes to the one call whose token it carries, with the host's own
// token put back, if acceptProgress lets it through; it reports whether it
// did. A log message names no request, so it goes to every call in flight on
// this connection whose host asked for that level: on a legacy session
// logging is session-wide anyway, and that is the only association the
// protocol offers.
func (c *Client) relayNotification(method string, params json.RawMessage) bool {
	switch method {
	case "notifications/progress":
		r, p, ok := c.acceptProgress(params)
		if !ok {
			return false
		}
		if r.OnProgress != nil {
			p["progressToken"] = r.ProgressToken
			if b, err := json.Marshal(p); err == nil {
				r.OnProgress(b)
			}
		}
		return true
	case "notifications/message":
		var m struct {
			Level string `json:"level"`
		}
		_ = json.Unmarshal(params, &m)
		c.mu.Lock()
		targets := make([]*Relay, 0, len(c.relays))
		for r := range c.relays {
			targets = append(targets, r)
		}
		c.mu.Unlock()
		for _, r := range targets {
			if r.OnMessage != nil && r.LogLevel != "" && LogAtLeast(m.Level, r.LogLevel) {
				r.OnMessage(params)
			}
		}
	}
	return true
}
