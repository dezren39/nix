package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// A modern server delivers list changes and resource updates only on a
// subscriptions/listen stream: there is no GET stream and no
// resources/subscribe in 2026-07-28. Without one, a modern upstream's tool
// list changes and mcpx's cached catalogue never hears of it.
//
// https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/subscriptions

// ListenFilter is a subscriptions/listen notifications filter.
type ListenFilter struct {
	ToolsListChanged      bool     `json:"toolsListChanged,omitempty"`
	PromptsListChanged    bool     `json:"promptsListChanged,omitempty"`
	ResourcesListChanged  bool     `json:"resourcesListChanged,omitempty"`
	ResourceSubscriptions []string `json:"resourceSubscriptions,omitempty"`
}

func (f ListenFilter) empty() bool {
	return !f.ToolsListChanged && !f.PromptsListChanged && !f.ResourcesListChanged && len(f.ResourceSubscriptions) == 0
}

// listener keeps one subscriptions/listen stream open for a modern
// connection, reopening it when it ends and replacing it when the set of
// subscribed resources changes.
type listener struct {
	c       *Client
	changed chan struct{}

	mu   sync.Mutex
	uris map[string]bool
	// id is the JSON-RPC id of the stream currently open, as it appears in
	// a notification's subscriptionId; "" when none is.
	id string
	// asked and acked are the filter sent and the subset the server agreed
	// to honour.
	asked, acked ListenFilter
}

func (c *Client) startListen() {
	l := &listener{c: c, changed: make(chan struct{}, 1), uris: map[string]bool{}}
	c.mu.Lock()
	c.listen = l
	c.mu.Unlock()
	go l.run()
}

// filter is what mcpx needs from this server: whatever list changes the
// server says it can announce, and updates for the resources subscribed.
// Nothing the server did not declare is asked for.
func (l *listener) filter() ListenFilter {
	caps := l.c.Capabilities
	flag := func(cap, key string) bool { return declares(caps, cap, key) }
	f := ListenFilter{
		ToolsListChanged:     flag("tools", "listChanged"),
		PromptsListChanged:   flag("prompts", "listChanged"),
		ResourcesListChanged: flag("resources", "listChanged"),
	}
	if flag("resources", "subscribe") {
		l.mu.Lock()
		for u := range l.uris {
			f.ResourceSubscriptions = append(f.ResourceSubscriptions, u)
		}
		l.mu.Unlock()
		sort.Strings(f.ResourceSubscriptions)
	}
	return f
}

func (l *listener) set(uri string, on bool) {
	l.mu.Lock()
	if l.uris[uri] == on {
		l.mu.Unlock()
		return
	}
	if on {
		l.uris[uri] = true
	} else {
		delete(l.uris, uri)
	}
	l.mu.Unlock()
	select {
	case l.changed <- struct{}{}:
	default:
	}
}

func (l *listener) run() {
	c := l.c
	for c.Alive() {
		f := l.filter()
		if f.empty() {
			select {
			case <-l.changed:
				continue
			case <-c.Done():
				return
			}
		}
		id, ch, stop := l.open(f)
		reopen := false
		select {
		case <-ch:
			// The server ended it, gracefully or not; either way mcpx still
			// wants the notifications, so it asks again after a pause.
			reopen = true
		case <-l.changed:
		case <-c.Done():
		}
		stop()
		c.forget(id)
		l.mu.Lock()
		l.id = ""
		l.mu.Unlock()
		if reopen {
			select {
			case <-time.After(defaults.UpstreamListenReopenDelay):
			case <-l.changed:
			case <-c.Done():
				return
			}
		}
	}
}

// open sends subscriptions/listen. The returned stop ends the stream the way
// the transport requires: closing it over HTTP, notifications/cancelled
// referencing the listen id on stdio.
func (l *listener) open(f ListenFilter) (int64, <-chan *rpcResponse, func()) {
	c := l.c
	ctx, cancel := context.WithCancel(context.Background())
	params, _ := json.Marshal(map[string]any{"notifications": f})
	params, _ = c.withMeta(ctx, params, c.metaVersion)
	id := c.nextID.Add(1)
	l.mu.Lock()
	// Recorded before the request goes out: the acknowledgement can arrive
	// before the send returns.
	l.id = strconv.FormatInt(id, 10)
	l.asked, l.acked = f, ListenFilter{}
	l.mu.Unlock()
	gotID, ch := c.beginID(ctx, id, "subscriptions/listen", params)
	return gotID, ch, func() {
		cancel()
		c.cancelled("subscriptions/listen", gotID)
	}
}

// accepts reports whether a notification belongs to a stream mcpx has open.
// On stdio every stream shares one channel, and a notification tagged with
// the id of a stream mcpx has since replaced is for nobody: the spec's MUST is
// to correlate by subscriptionId, and delivering a stale one would announce a
// change for a filter mcpx no longer holds. Untagged notifications are the
// ones that belong to a request rather than a stream (progress, log lines)
// and pass.
func (l *listener) accepts(method string, params json.RawMessage) bool {
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
		ListenFilter
		Notifications *ListenFilter `json:"notifications"`
	}
	_ = json.Unmarshal(params, &p)
	raw, tagged := p.Meta[MetaSubscriptionID]
	if !tagged {
		return method != "notifications/subscriptions/acknowledged"
	}
	var id any
	if json.Unmarshal(raw, &id) != nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.id == "" || fmt.Sprint(id) != l.id {
		return false
	}
	if method == "notifications/subscriptions/acknowledged" && p.Notifications != nil {
		l.acked = *p.Notifications
		if narrowed := narrowed(l.asked, l.acked); narrowed != "" {
			go l.c.warn(Warning{Reason: "subscriptions/listen: the server declined " + narrowed})
		}
	}
	return true
}

// narrowed names what was asked for and not acknowledged.
func narrowed(asked, acked ListenFilter) string {
	var out []string
	if asked.ToolsListChanged && !acked.ToolsListChanged {
		out = append(out, "toolsListChanged")
	}
	if asked.PromptsListChanged && !acked.PromptsListChanged {
		out = append(out, "promptsListChanged")
	}
	if asked.ResourcesListChanged && !acked.ResourcesListChanged {
		out = append(out, "resourcesListChanged")
	}
	have := map[string]bool{}
	for _, u := range acked.ResourceSubscriptions {
		have[u] = true
	}
	for _, u := range asked.ResourceSubscriptions {
		if !have[u] {
			out = append(out, "resource "+u)
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// Listening returns the filter of the open subscriptions/listen stream and
// the subset the server acknowledged, for diagnostics.
func (c *Client) Listening() (asked, acked ListenFilter, open bool) {
	c.mu.Lock()
	l := c.listen
	c.mu.Unlock()
	if l == nil {
		return ListenFilter{}, ListenFilter{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.asked, l.acked, l.id != ""
}
