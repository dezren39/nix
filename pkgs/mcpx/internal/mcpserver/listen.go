package mcpserver

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// The reserved _meta keys a 2026-07-28 server writes.
const (
	// MetaServerInfo identifies mcpx on every modern result.
	MetaServerInfo = "io.modelcontextprotocol/serverInfo"
	// MetaSubscriptionID ties a notification, an acknowledgement and a
	// listen's closing result to the subscriptions/listen that opened it.
	MetaSubscriptionID = "io.modelcontextprotocol/subscriptionId"
	// MetaLogLevel is the per-request log level a modern client may set.
	MetaLogLevel = "io.modelcontextprotocol/logLevel"
	// ExtTasks is the tasks extension's identifier.
	ExtTasks = "io.modelcontextprotocol/tasks"
)

// listenStream is one open notification stream.
//
// A connection may hold several: 2026-07-28 lets a client open as many
// subscriptions/listen requests as it likes, each identified by its own
// request id and each tagged with it. mcpx kept one per connection and
// replaced it on every new listen, so a client that opened a tools stream
// and then a resources stream silently lost the first.
type listenStream struct {
	// id is the subscriptions/listen request id, or nil for the legacy
	// resources/subscribe stream, whose notifications are untagged because
	// no legacy revision has anywhere to put the tag.
	id json.RawMessage
	// send writes a frame to wherever this stream goes: the listen
	// request's own response on HTTP, the shared pipe on stdio.
	send   func(frame any) error
	cancel context.CancelFunc
	// done is closed when the stream has ended for any reason.
	done chan struct{}
	once sync.Once
}

func (l *listenStream) end() { l.once.Do(func() { l.cancel(); close(l.done) }) }

// legacyListen is the key of the resources/subscribe stream. A JSON-RPC id
// is never empty, so it cannot collide with a modern listen.
const legacyListen = ""

// listenKey is the same key the transport's in-flight table uses (idKey),
// so a notifications/cancelled that names a listen by 7.0 finds the listen
// opened as 7, exactly as it would find an ordinary request. Two different
// normalisations would let the cancellation cancel one and miss the other.
func listenKey(id json.RawMessage) string { return idKey(id) }

// allowedMethods is the notification each filter field admits.
//
// Checked on the way out as well as passed to the notifier. The notifier is
// the daemon's event bus translated, and it knows notification kinds -- a
// finished url elicitation, say -- that no filter field names. The
// specification says a server MUST NOT send a type the client did not ask
// for, and that is easier to guarantee at the last step than to trust of
// every notifier.
func (f ListenFilter) allows(method string) bool {
	switch method {
	case "notifications/tools/list_changed":
		return f.ToolsListChanged
	case "notifications/prompts/list_changed":
		return f.PromptsListChanged
	case "notifications/resources/list_changed":
		return f.ResourcesListChanged
	case "notifications/resources/updated":
		return len(f.ResourceSubscriptions) > 0
	}
	return false
}

func (f ListenFilter) empty() bool {
	return !f.ToolsListChanged && !f.PromptsListChanged && !f.ResourcesListChanged &&
		len(f.ResourceSubscriptions) == 0
}

// tagged adds the subscription id to a notification's params, keeping any
// _meta the params already had.
func tagged(params any, id json.RawMessage) map[string]any {
	m, ok := copyMap(params)
	if !ok {
		m = map[string]any{}
	}
	meta, _ := m["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta[MetaSubscriptionID] = id
	m["_meta"] = meta
	return m
}

// listen opens a subscriptions/listen stream.
//
// The order is the specification's: the acknowledgement is the first thing
// sent carrying this subscription's id, and nothing else is sent on it
// before. Then notifications, each tagged, until one side ends it.
//
// On stdio the reply is withheld -- the stream's result arrives only when it
// ends -- and the handler returns at once, because the pipe outlives it. On
// Streamable HTTP the listen request's response *is* the stream, so the
// handler holds it open: it returns when the client closes it, which the
// transport page says is how an HTTP client cancels, or when mcpx ends the
// stream itself. It used to return at once there too, which ended the
// response with nothing on it: an HTTP client that listened got a 202 and
// never heard anything again.
func (s *Server) listen(ctx context.Context, c *Conn, req request, peer Peer) *response {
	fail := func(code int, msg string, data any) *response {
		return &response{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: code, Message: msg, Data: data}}
	}
	var p struct {
		Notifications json.RawMessage `json:"notifications"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return fail(codeInvalidParams, err.Error(), nil)
	}
	var f ListenFilter
	var ext struct {
		TaskIDs []string `json:"taskIds"`
	}
	if len(p.Notifications) > 0 {
		if err := json.Unmarshal(p.Notifications, &f); err != nil {
			return fail(codeInvalidParams, "notifications: "+err.Error(), nil)
		}
		_ = json.Unmarshal(p.Notifications, &ext)
	}
	if len(ext.TaskIDs) > 0 && !peer.DeclaredExtension(ExtTasks) {
		// The tasks extension's own MUST: asking for task notifications
		// without declaring the extension is a missing capability, not an
		// unknown field to ignore.
		return fail(codeMissingCapability, "Missing required client capability",
			map[string]any{"requiredCapabilities": map[string]any{
				"extensions": map[string]any{ExtTasks: map[string]any{}}}})
	}

	send := senderFrom(ctx)
	overHTTP := send != nil
	if send == nil {
		c.mu.Lock()
		send = c.send
		push := c.pushFn
		c.mu.Unlock()
		if send == nil && push != nil {
			// A connection that can carry notifications but not arbitrary
			// frames -- SetPush's in-process hook. Everything on a stream
			// is a notification except the closing result, which such a
			// connection has no way to deliver and simply does not get.
			send = func(frame any) error {
				if m, ok := frame.(map[string]any); ok {
					if method, _ := m["method"].(string); method != "" {
						push(method, m["params"])
					}
				}
				return nil
			}
		}
	}
	if send == nil {
		return fail(codeInternal, "this transport cannot carry a subscription stream", nil)
	}

	// The agreed subset. Without a notifier mcpx can deliver nothing, and
	// says so by agreeing to nothing rather than by refusing the stream: the
	// acknowledgement exists precisely to report what was honoured.
	agreed := f
	if s.Notify == nil {
		agreed = ListenFilter{}
	}
	// Agreed only where capabilities() declares it: in pass-through mode,
	// where the list is the upstreams' and changes with theirs.
	if !s.toolsVary() {
		agreed.ToolsListChanged = false
	}

	lctx, cancel := context.WithCancel(context.Background())
	l := &listenStream{id: req.ID, send: send, cancel: cancel, done: make(chan struct{})}
	key := listenKey(req.ID)
	c.mu.Lock()
	if c.listens == nil {
		c.listens = map[string]*listenStream{}
	}
	prev := c.listens[key]
	c.listens[key] = l
	c.mu.Unlock()
	if prev != nil {
		// The same id twice is a client bug; the newer one wins, quietly.
		prev.end()
	}

	// stopped closes when nothing can write to send any more. The HTTP
	// path waits for it before returning, because a response writer used
	// after its handler returns is a data race, and the notifier is on
	// another goroutine.
	stopped := make(chan struct{})
	open := func() {}
	if s.Notify != nil && !agreed.empty() {
		var watching []string
		watching, _, open, stopped = s.runNotifier(lctx, ctx, agreed, func(method string, params any) {
			_ = send(map[string]any{"jsonrpc": "2.0", "method": method,
				"params": tagged(params, req.ID)})
		}, func() {
			// The notifier stopped on its own -- the daemon's event
			// stream ended. That is mcpx ending the subscription, and
			// the client has to be told so, or it waits forever on a
			// stream nothing will write to.
			c.closeListen(key, l)
		})
		// Only the resources whose updates will actually arrive: those
		// whose server declares resources.subscribe and accepted mcpx's
		// own subscription.
		agreed.ResourceSubscriptions = watching
	} else {
		close(stopped)
	}

	// taskIds: the tasks this client can see that still exist. They come
	// from the task store, not the notifier, so they are agreed with or
	// without one. An id mcpx does not hold is left out, which is how the
	// acknowledgement says it will hear nothing about it.
	var taskIDs []string
	for _, id := range ext.TaskIDs {
		if _, ok := s.tasks().Get(id); ok && s.visible(id, c) {
			taskIDs = append(taskIDs, id)
		}
	}
	ackFilter := map[string]any{}
	if b, err := json.Marshal(agreed); err == nil {
		_ = json.Unmarshal(b, &ackFilter)
	}
	if len(taskIDs) > 0 {
		ackFilter["taskIds"] = taskIDs
	}

	if err := send(map[string]any{"jsonrpc": "2.0",
		"method": "notifications/subscriptions/acknowledged",
		"params": map[string]any{
			"_meta":         map[string]any{MetaSubscriptionID: req.ID},
			"notifications": ackFilter,
		}}); err != nil {
		c.dropListen(key, l)
		<-stopped
		return nil
	}
	open()
	stopTasks := func() {}
	if len(taskIDs) > 0 {
		stopTasks = s.watchTasks(req.ID, taskIDs, send)
		go func() { <-l.done; stopTasks() }()
	}

	if !overHTTP {
		return nil
	}
	// Stopped before returning, for the same reason as the notifier: the
	// response writer must not be used after the handler returns.
	defer stopTasks()
	keepAlive := keepAliveFrom(ctx)
	tick := time.NewTicker(s.Timing.resolved().SSEKeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			// The client closed the stream: that is its cancellation, and
			// no notifications/cancelled is expected or sent.
			c.dropListen(key, l)
			<-stopped
			return nil
		case <-l.done:
			<-stopped
			return nil
		case <-tick.C:
			if keepAlive != nil && keepAlive() != nil {
				c.dropListen(key, l)
				<-stopped
				return nil
			}
		}
	}
}

// watchTasks sends notifications/tasks on a listen stream for each agreed
// task: its current state at once, so a task that changed between its
// creation and this listen is not missed, then every change after. Each
// carries the DetailedTask tasks/get would answer at that moment, tagged
// with the subscription id. stop ends it; nothing is sent after stop
// returns.
func (s *Server) watchTasks(sub json.RawMessage, ids []string, send func(frame any) error) (stop func()) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var mu sync.Mutex
	closed := false
	deliver := func(id string) {
		if !want[id] {
			return
		}
		// Read and sent under one lock, so two changes in quick
		// succession cannot reach the client in the wrong order.
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		d, ok := s.detailedNow(context.Background(), id)
		if !ok {
			return
		}
		_ = send(map[string]any{"jsonrpc": "2.0", "method": "notifications/tasks",
			"params": tagged(d, sub)})
	}
	unwatch := s.tasks().Watch(deliver)
	for _, id := range ids {
		deliver(id)
	}
	return func() {
		unwatch()
		mu.Lock()
		closed = true
		mu.Unlock()
	}
}

// dropListen ends a stream the client ended. Nothing is sent: the client
// already knows, and a cancellation SHOULD NOT be answered.
func (c *Conn) dropListen(key string, l *listenStream) {
	c.mu.Lock()
	if c.listens[key] == l {
		delete(c.listens, key)
	}
	c.mu.Unlock()
	l.end()
}

// closeListen ends a stream on mcpx's own initiative, and says so.
//
// The specification contradicts itself about how. The subscriptions page
// says a server tearing a subscription down SHOULD send a successful
// subscriptions/listen result carrying the subscriptionId. The cancellation
// page says a server MUST send notifications/cancelled referencing the
// listen request id when it tears the stream down, and MUST NOT send
// notifications/cancelled for any other purpose. The MUST is honoured
// first, while the request is still in progress -- a cancellation may only
// name one that is. The result follows, because the SHOULD costs nothing:
// a client that acted on the cancellation ignores a late response, as the
// cancellation page tells it to, and one that waits for the result gets it.
func (c *Conn) closeListen(key string, l *listenStream) {
	c.mu.Lock()
	if c.listens[key] == l {
		delete(c.listens, key)
	}
	c.mu.Unlock()
	c.announceEnd(l)
	l.end()
}

// announceEnd tells the client mcpx is ending a modern stream.
func (c *Conn) announceEnd(l *listenStream) {
	if l.id != nil {
		_ = l.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled",
			"params": map[string]any{"requestId": l.id,
				"reason": "mcpx ended this subscription"}})
		_ = l.send(response{JSONRPC: "2.0", ID: l.id, Result: map[string]any{
			"resultType": "complete",
			"_meta": map[string]any{
				MetaSubscriptionID: l.id,
				MetaServerInfo:     map[string]any{"name": c.s.name, "version": c.s.version},
			},
		}})
	}
}

// cancelListen ends the stream a client's notifications/cancelled names,
// if it names one. Reports whether it did.
func (c *Conn) cancelListen(requestID json.RawMessage) bool {
	key := listenKey(requestID)
	if key == legacyListen {
		return false
	}
	c.mu.Lock()
	l := c.listens[key]
	c.mu.Unlock()
	if l == nil {
		return false
	}
	c.dropListen(key, l)
	return true
}

// runNotifier runs the notifier for f until ctx ends.
//
// It returns once the notifier has said which of f's resources it will
// deliver updates for -- f's own, for a notifier that has nothing to
// arrange -- so that the caller can agree to exactly those. Nothing reaches
// deliver, and ended is not called, until the caller calls open: the
// acknowledgement is the first thing a stream carries. stopped closes when
// the notifier has returned.
func (s *Server) runNotifier(ctx, wait context.Context, f ListenFilter,
	deliver func(method string, params any), ended func(),
) (watching []string, refused map[string]string, open func(), stopped chan struct{}) {
	gate := make(chan struct{})
	var once sync.Once
	open = func() { once.Do(func() { close(gate) }) }
	stopped = make(chan struct{})
	type result struct {
		watching []string
		refused  map[string]string
	}
	ready := make(chan result, 1)
	var agreedURIs map[string]bool // written before gate closes, read after
	send := func(method string, params any) {
		select {
		case <-gate:
		case <-ctx.Done():
			return
		}
		if ctx.Err() != nil || !f.allows(method) {
			return
		}
		if method == "notifications/resources/updated" {
			// Only what was agreed; the notifier may match more loosely.
			m, _ := copyMap(params)
			if u, _ := m["uri"].(string); !agreedURIs[u] {
				return
			}
		}
		deliver(method, params)
	}
	rn, arranges := s.Notify.(ResourceNotifier)
	go func() {
		defer close(stopped)
		if arranges && len(f.ResourceSubscriptions) > 0 {
			rn.ListenResources(ctx, f, func(w []string, r map[string]string) {
				select {
				case ready <- result{w, r}:
				default:
				}
			}, send)
		} else {
			ready <- result{watching: f.ResourceSubscriptions}
			s.Notify.Listen(ctx, f, send)
		}
		select {
		case <-gate:
		case <-ctx.Done():
		}
		if ctx.Err() == nil && ended != nil {
			ended()
		}
	}()
	var got result
	select {
	case got = <-ready:
	case <-stopped:
		select {
		case got = <-ready:
		default:
		}
	case <-ctx.Done():
	case <-wait.Done():
		// Whoever asked stopped waiting. Nothing was agreed, so nothing
		// may be delivered: close the gate on an empty agreement.
	}
	refused = map[string]string{}
	agreedURIs = map[string]bool{}
	for _, u := range got.watching {
		agreedURIs[u] = true
	}
	for _, u := range f.ResourceSubscriptions {
		if agreedURIs[u] {
			continue
		}
		reason := got.refused[u]
		if reason == "" {
			reason = "mcpx could not arrange updates for it"
		}
		refused[u] = reason
	}
	watching = []string{}
	for _, u := range f.ResourceSubscriptions {
		if agreedURIs[u] {
			watching = append(watching, u)
		}
	}
	return watching, refused, open, stopped
}

// restartListen replaces the legacy resources/subscribe stream with one for
// f. What it could not honour is simply not agreed, so never delivered. Legacy
// subscriptions are per connection, so there is only ever one, and its
// notifications go out untagged on the connection's push.
//
// The new stream is running before the old one ends, so a resource both
// name stays subscribed upstream throughout rather than being dropped and
// taken again on every change to the set.
func (s *Server) restartListen(wait context.Context, c *Conn, f ListenFilter) {
	c.mu.Lock()
	push := c.pushFn
	c.mu.Unlock()
	notify := s.Notify
	var l *listenStream
	if push != nil && notify != nil && !f.empty() {
		lctx, cancel := context.WithCancel(context.Background())
		l = &listenStream{cancel: cancel, done: make(chan struct{})}
		_, _, open, _ := s.runNotifier(lctx, wait, f, push, nil)
		open()
	}
	c.mu.Lock()
	if c.listens == nil {
		c.listens = map[string]*listenStream{}
	}
	prev := c.listens[legacyListen]
	delete(c.listens, legacyListen)
	if l != nil {
		c.listens[legacyListen] = l
	}
	c.mu.Unlock()
	if prev != nil {
		prev.end()
	}
	select {
	case <-c.ended:
		// The connection ended while the new stream was being arranged,
		// after its streams were stopped; this one would outlive it.
		c.stopListen()
	default:
	}
}

// stopListen ends every stream this connection opened: the transport is
// going away. Modern streams are closed the way mcpx closes any stream it
// ends, because to the client this is mcpx tearing them down.
func (c *Conn) stopListen() {
	c.mu.Lock()
	all := c.listens
	c.listens = nil
	c.mu.Unlock()
	for _, l := range all {
		c.announceEnd(l) // a no-op for the untagged legacy stream
		l.end()
	}
}

// Listening reports how many notification streams a connection holds, for
// tests that need to see one end.
func (c *Conn) Listening() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.listens)
}

// keepAlive writes a comment on an HTTP stream, to keep an idle one from
// being closed by whatever sits between mcpx and the client.
type keepAliveKey struct{}

func withKeepAlive(ctx context.Context, fn func() error) context.Context {
	return context.WithValue(ctx, keepAliveKey{}, fn)
}

func keepAliveFrom(ctx context.Context) func() error {
	fn, _ := ctx.Value(keepAliveKey{}).(func() error)
	return fn
}
