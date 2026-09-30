package pool

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// instanceRef names the instance a server-initiated request arrived on.
//
// The handler that answers those requests has to be installed before the
// handshake -- capabilities are declared there, and a handler added later
// can never be declared -- which is before the Instance exists, and long
// before Acquire assigns it a key. So the handler closes over this instead,
// and reads the key when a question actually arrives.
type instanceRef struct {
	p   *Pool
	ptr atomic.Pointer[Instance]
}

func (r *instanceRef) set(in *Instance) { r.ptr.Store(in) }

func (r *instanceRef) key() string {
	in := r.ptr.Load()
	if in == nil {
		return ""
	}
	r.p.mu.Lock()
	defer r.p.mu.Unlock()
	return in.key
}

// Request performs any MCP method on a leased instance.
//
// The pool's typed methods -- Call, ReadResource, GetPrompt -- are the ones
// a code-mode host needs, and nothing else could reach the wire at all. That
// is why /v1/complete answered from cache with `upstream:false`: not because
// the server could not complete, but because no code path existed to ask it.
func (p *Pool) Request(ctx context.Context, sessionKey, method string, params json.RawMessage) (json.RawMessage, error) {
	lease, err := p.Acquire(ctx, sessionKey)
	if err != nil {
		return nil, err
	}
	defer lease.Release()

	cctx, finish := p.upstream(ctx, sessionKey, lease)
	res, err := lease.Client().Request(cctx, method, params)
	return res, finish(err)
}

// Complete forwards completion/complete upstream.
//
// ok is false when the server never declared `completions`. The caller has a
// cached answer to fall back on and has to say which of the two it is
// showing, because a client that cannot tell an empty list from an
// unimplemented method shows nothing and the user concludes the feature is
// broken.
func (p *Pool) Complete(ctx context.Context, sessionKey string, params json.RawMessage) (json.RawMessage, bool, error) {
	lease, err := p.Acquire(ctx, sessionKey)
	if err != nil {
		return nil, false, err
	}
	defer lease.Release()

	cctx, finish := p.upstream(ctx, sessionKey, lease)
	res, ok, err := lease.Client().Complete(cctx, params)
	return res, ok, finish(err)
}

// Era reports which protocol generation a live instance settled on, and the
// version it negotiated. Both are empty when nothing is running: the answer
// is a property of a connection, not of a configuration, and guessing it
// from the configured preference would report an intention as a fact.
func (p *Pool) Era() (mcpclient.Era, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, in := range p.instances {
		if in.Client.Alive() {
			return in.Client.Era, in.Client.Negotiated
		}
	}
	return "", ""
}

// EraSource says how a live instance's era was settled -- probe, cache or
// forced -- or "" when nothing is running.
func (p *Pool) EraSource() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, in := range p.instances {
		if in.Client.Alive() {
			return in.eraSource
		}
	}
	return ""
}

// CachedEra returns what the era cache remembers for this configuration.
func (p *Pool) CachedEra() (EraRecord, bool) {
	if p.Hooks == nil || p.Hooks.Eras == nil {
		return EraRecord{}, false
	}
	return p.Hooks.Eras.Get(Identity(p.cfg))
}

// Capabilities returns what a live instance declared, or nil when none is up.
func (p *Pool) Capabilities() map[string]json.RawMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, in := range p.instances {
		if in.Client.Alive() {
			return in.Client.Capabilities
		}
	}
	return nil
}
