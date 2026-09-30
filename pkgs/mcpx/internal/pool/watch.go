package pool

import (
	"context"
	"errors"
	"sync"
)

// Resource subscriptions, upstream.
//
// mcpx declares resources.subscribe to its own clients, and a client that
// subscribes expects notifications/resources/updated. Those can only come
// from the upstream server that owns the resource, and only once mcpx has
// subscribed there itself -- which, until this file, it never did: the
// capability was declared and nothing was delivered (#241).
//
// Subscriptions are counted per URI across every mcpx client, because the
// upstream sees one connection, mcpx's, and one resources/unsubscribe from
// it ends the updates for everybody. The first watcher subscribes; the last
// one to leave unsubscribes. The instance holding the subscription is kept
// from the idle reaper, and when it goes away anyway -- a restart, a crash
// -- a replacement is started and subscribed, since nobody else would ever
// start one and the updates would stop without a word.

// ErrNotSubscribable is Watch's answer for a server that does not declare
// resources.subscribe. Asking it anyway would be sending a server a request
// for a capability it did not declare.
var ErrNotSubscribable = errors.New("the server does not declare resources.subscribe")

// Watch subscribes to updates for uri on this server until release is
// called. key is the scope key the instance is resolved under.
func (p *Pool) Watch(ctx context.Context, key, uri string) (release func(), err error) {
	lease, err := p.Acquire(ctx, key)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	if !lease.Client().CanSubscribeResources() {
		return nil, ErrNotSubscribable
	}
	p.watchMu.Lock()
	if p.watched == nil {
		p.watched = map[string]int{}
	}
	p.watched[uri]++
	p.watchKey = key
	p.watchMu.Unlock()
	if err := p.subscribeOn(ctx, lease.inst, uri); err != nil {
		p.unwatch(uri)
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { p.unwatch(uri) }) }, nil
}

// Watching reports the URIs with at least one watcher, for tests and status.
func (p *Pool) Watching() map[string]int {
	p.watchMu.Lock()
	defer p.watchMu.Unlock()
	out := make(map[string]int, len(p.watched))
	for u, n := range p.watched {
		out[u] = n
	}
	return out
}

func (p *Pool) unwatch(uri string) {
	p.watchMu.Lock()
	p.watched[uri]--
	last := p.watched[uri] <= 0
	if last {
		delete(p.watched, uri)
	}
	p.watchMu.Unlock()
	if !last {
		return
	}
	p.mu.Lock()
	live := append([]*Instance(nil), p.instances...)
	p.mu.Unlock()
	for _, in := range live {
		in.subMu.Lock()
		had := in.subscribed[uri]
		delete(in.subscribed, uri)
		in.subMu.Unlock()
		if had && in.Client.Alive() {
			ctx, cancel := context.WithTimeout(context.Background(), p.cfg.CallTimeout)
			_ = in.Client.UnsubscribeResource(ctx, uri)
			cancel()
		}
	}
}

// subscribeOn subscribes one instance to uri, once.
func (p *Pool) subscribeOn(ctx context.Context, in *Instance, uri string) error {
	in.subMu.Lock()
	done := in.subscribed[uri]
	in.subMu.Unlock()
	if done {
		return nil
	}
	if err := in.Client.SubscribeResource(ctx, uri); err != nil {
		return err
	}
	in.subMu.Lock()
	if in.subscribed == nil {
		in.subscribed = map[string]bool{}
	}
	in.subscribed[uri] = true
	monitor := !in.monitored
	in.monitored = true
	in.subMu.Unlock()
	if monitor {
		go p.monitor(in)
	}
	return nil
}

// resubscribe gives a newly started instance every subscription still
// wanted.
func (p *Pool) resubscribe(ctx context.Context, in *Instance) {
	p.watchMu.Lock()
	uris := make([]string, 0, len(p.watched))
	for u := range p.watched {
		uris = append(uris, u)
	}
	p.watchMu.Unlock()
	if len(uris) == 0 || !in.Client.CanSubscribeResources() {
		return
	}
	for _, u := range uris {
		if err := p.subscribeOn(ctx, in, u); err != nil {
			lifecycle("server.warning", map[string]any{"server": p.cfg.Name,
				"reason": "resubscribing " + u + ": " + err.Error()})
		}
	}
}

// monitor replaces a subscribed instance when it ends while its
// subscriptions are still wanted.
func (p *Pool) monitor(in *Instance) {
	<-in.Client.Done()
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	p.watchMu.Lock()
	wanted := len(p.watched) > 0
	key := p.watchKey
	p.watchMu.Unlock()
	if closed || !wanted {
		return
	}
	// Acquire starts the replacement, and start() subscribes it. A failure
	// here is the pool's ordinary start failure, recorded as lastErr; the
	// next caller that reaches this server retries it and resubscribes.
	lease, err := p.Acquire(context.Background(), key)
	if err == nil {
		lease.Release()
	}
}

func (in *Instance) hasSubscriptions() bool {
	in.subMu.Lock()
	defer in.subMu.Unlock()
	return len(in.subscribed) > 0
}
