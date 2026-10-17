package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
)

// resumeProbeTimeout bounds the check that a handed-on upstream session is
// still live. Attach runs before the listeners serve, so the bound is also how
// long a takeover can be held up by an upstream that has gone quiet.
const resumeProbeTimeout = 5 * time.Second

// InstanceHandoff is a live instance in transit to a successor. A stdio child's
// pipes travel beside it as file descriptors, not in the JSON. A streamable
// HTTP instance carries only its upstream session.
type InstanceHandoff struct {
	ID         string                  `json:"id"`
	Trace      string                  `json:"trace"`
	Key        string                  `json:"key"`
	StartedAt  time.Time               `json:"startedAt"`
	LastUsed   time.Time               `json:"lastUsed"`
	Calls      int64                   `json:"calls"`
	EraSource  string                  `json:"eraSource,omitempty"`
	LegacyLane bool                    `json:"legacyLane,omitempty"`
	Subscribed []string                `json:"subscribed,omitempty"`
	Stdio      *mcpclient.StdioHandoff `json:"stdio"`
	HTTP       *mcpclient.HTTPHandoff  `json:"http,omitempty"`
	Session    mcpclient.Session       `json:"session"`
}

// PoolHandoff is one pool's children in transit. PoolID names the process
// definition, so a successor matches it to its own pool of the same identity.
type PoolHandoff struct {
	PoolID    string            `json:"poolId"`
	Seq       int               `json:"seq"`
	Instances []InstanceHandoff `json:"instances"`
}

// ErrBusy is returned by Detach while calls are in flight or a child is still
// starting. Nothing has changed when it is returned, so the caller may retry.
var ErrBusy = errors.New("calls in flight")

// Detach takes the pool out of service so a successor can serve its live
// stdio children and HTTP sessions. Every other instance stays parked until
// Commit closes it or Restore puts it back.
func (p *Pool) Detach() (*PoolHandoff, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("pool is closed")
	}
	if p.starting > 0 {
		p.mu.Unlock()
		return nil, fmt.Errorf("server %q: a child is starting: %w", p.cfg.Name, ErrBusy)
	}
	for _, in := range p.instances {
		if in.holders > 0 {
			p.mu.Unlock()
			return nil, fmt.Errorf("server %q: %w", p.cfg.Name, ErrBusy)
		}
	}
	p.closed = true
	parked := p.instances
	p.parked = parked
	p.instances = nil
	seq := p.seq
	metas := make([]InstanceHandoff, len(parked))
	for i, in := range parked {
		metas[i] = in.describe()
	}
	p.cond.Broadcast()
	p.mu.Unlock()

	var handed []*Instance
	var out []InstanceHandoff
	for i, in := range parked {
		if !in.Client.Alive() {
			continue
		}
		meta := metas[i]
		switch in.transport.(type) {
		case *mcpclient.StdioTransport:
			h, err := in.Client.Detach()
			if err != nil {
				p.restore(handed)
				return nil, fmt.Errorf("server %q: %w", p.cfg.Name, err)
			}
			meta.Stdio = h.Stdio
			meta.Session = h.Session
		case *mcpclient.HTTPTransport:
			hs, ok := in.Client.HTTPSession()
			if !ok || hs.SessionID == "" {
				continue
			}
			meta.HTTP = &hs
			meta.Session = in.Client.SessionState()
		default:
			continue
		}
		out = append(out, meta)
		handed = append(handed, in)
	}
	p.mu.Lock()
	p.handed = handed
	p.mu.Unlock()
	return &PoolHandoff{PoolID: p.PoolID(), Seq: seq, Instances: out}, nil
}

// Commit finishes a detach the successor accepted: handed children now belong
// to the successor, and everything else parked is closed.
func (p *Pool) Commit() {
	p.mu.Lock()
	parked, handed := p.parked, p.handed
	p.parked, p.handed = nil, nil
	p.mu.Unlock()
	released := make(map[*Instance]bool, len(handed))
	for _, in := range handed {
		released[in] = true
		if _, ok := in.transport.(*mcpclient.HTTPTransport); ok {
			in.Client.Abandon()
		} else {
			in.Client.Release()
		}
	}
	for _, in := range parked {
		if !released[in] {
			_ = in.Client.Close()
		}
	}
}

// Restore undoes a Detach the successor did not accept. The children never
// stopped and their pipes were only paused, so the pool carries on as before.
func (p *Pool) Restore() {
	p.mu.Lock()
	handed := p.handed
	p.handed = nil
	p.mu.Unlock()
	p.restore(handed)
}

func (p *Pool) restore(handed []*Instance) {
	for _, in := range handed {
		if _, ok := in.transport.(*mcpclient.StdioTransport); ok {
			in.Client.Reattach()
		}
	}
	p.mu.Lock()
	p.instances = p.parked
	p.parked = nil
	p.closed = false
	p.cond.Broadcast()
	p.mu.Unlock()
}

// describe is the instance's record without its pipes. p.mu is held.
func (in *Instance) describe() InstanceHandoff {
	in.subMu.Lock()
	subs := make([]string, 0, len(in.subscribed))
	for uri := range in.subscribed {
		subs = append(subs, uri)
	}
	in.subMu.Unlock()
	return InstanceHandoff{
		ID:         in.ID,
		Trace:      in.trace,
		Key:        in.key,
		StartedAt:  in.startedAt,
		LastUsed:   in.lastUsed,
		Calls:      in.calls.Load(),
		EraSource:  in.eraSource,
		LegacyLane: in.legacyLane,
		Subscribed: subs,
	}
}

// Adopted is an instance taken from a predecessor. Nothing reads a stdio
// child's pipes or contacts an upstream until Attach, so Abort can give them
// back untouched.
type Adopted struct {
	handoff InstanceHandoff
	tr      mcpclient.Transport
}

// Adopt takes the pipes of each handed stdio child that is still running. A
// child that has exited is dropped; any other failure releases what was taken.
// HTTP sessions are taken by their own pool, see AdoptSessions.
func Adopt(hs []InstanceHandoff) ([]*Adopted, error) {
	var out []*Adopted
	for _, h := range hs {
		if h.Stdio == nil {
			if h.HTTP != nil {
				continue
			}
			abortAll(out)
			return nil, fmt.Errorf("instance %s: no child or session to adopt", h.ID)
		}
		tr, err := mcpclient.AdoptStdio(*h.Stdio)
		if errors.Is(err, mcpclient.ErrChildGone) {
			continue
		}
		if err != nil {
			abortAll(out)
			return nil, fmt.Errorf("instance %s: %w", h.ID, err)
		}
		out = append(out, &Adopted{handoff: h, tr: tr})
	}
	return out, nil
}

// AdoptSessions prepares the upstream HTTP sessions a predecessor handed on.
// Nothing is contacted here; Attach checks each session is still live.
func (p *Pool) AdoptSessions(hs []InstanceHandoff) ([]*Adopted, error) {
	var out []*Adopted
	for _, h := range hs {
		if h.HTTP == nil {
			continue
		}
		tr, err := p.dial()
		if err != nil {
			abortAll(out)
			return nil, fmt.Errorf("instance %s: %w", h.ID, err)
		}
		ht, ok := tr.(*mcpclient.HTTPTransport)
		if !ok {
			_ = tr.Close()
			abortAll(out)
			return nil, fmt.Errorf("instance %s: server %q is not a streamable HTTP upstream", h.ID, p.cfg.Name)
		}
		ht.Resume(*h.HTTP)
		out = append(out, &Adopted{handoff: h, tr: tr})
	}
	return out, nil
}

// Abort releases an adopted instance that will not be served.
func (a *Adopted) Abort() {
	switch tr := a.tr.(type) {
	case *mcpclient.StdioTransport:
		_, _ = tr.Detach()
	case *mcpclient.HTTPTransport:
		tr.Abandon()
	}
}

func abortAll(ads []*Adopted) {
	for _, a := range ads {
		a.Abort()
	}
}

// Close ends an adopted child that no pool wants.
func (a *Adopted) Close() {
	_ = a.tr.Close()
}

// Attach serves adopted instances from this pool, resuming each session. Call
// it only after the handoff has committed. HTTP sessions are probed first, and
// one the upstream no longer knows is dropped, so its next call starts afresh.
func (p *Pool) Attach(ads []*Adopted, seq int) {
	var sessions []*Adopted
	p.mu.Lock()
	for _, a := range ads {
		if _, ok := a.tr.(*mcpclient.HTTPTransport); ok {
			sessions = append(sessions, a)
			continue
		}
		in := p.resumeInstance(a)
		p.instances = append(p.instances, in)
		in.Client.Activate()
	}
	if seq > p.seq {
		p.seq = seq
	}
	p.cond.Broadcast()
	p.mu.Unlock()

	var wg sync.WaitGroup
	for _, a := range sessions {
		wg.Add(1)
		go func(a *Adopted) {
			defer wg.Done()
			p.attachSession(a)
		}(a)
	}
	wg.Wait()
}

func (p *Pool) resumeInstance(a *Adopted) *Instance {
	h := a.handoff
	opts := mcpclient.Options{ClientName: "mcpx", ClientVersion: Version}
	ref := &instanceRef{p: p}
	p.installServerHooks(&opts, ref)
	cl := mcpclient.Resume(a.tr, h.Session, opts)
	p.subscribeServerEvents(cl)
	in := &Instance{
		ID:         h.ID,
		Client:     cl,
		transport:  a.tr,
		key:        h.Key,
		trace:      h.Trace,
		startedAt:  h.StartedAt,
		lastUsed:   h.LastUsed,
		eraSource:  h.EraSource,
		legacyLane: h.LegacyLane,
		subscribed: make(map[string]bool, len(h.Subscribed)),
	}
	in.calls.Store(h.Calls)
	for _, uri := range h.Subscribed {
		in.subscribed[uri] = true
	}
	ref.set(in)
	return in
}

// attachSession serves a handed-on HTTP session once the upstream has
// confirmed it. A session it no longer knows, or cannot reach, is closed
// without a DELETE and left for the next call to replace.
func (p *Pool) attachSession(a *Adopted) {
	in := p.resumeInstance(a)
	in.Client.Activate()
	ctx, cancel := context.WithTimeout(context.Background(), resumeProbeTimeout)
	err := in.Client.Ping(ctx)
	cancel()
	if err != nil {
		in.Client.Abandon()
		lifecycle("upstream.session.reinit", map[string]any{
			"server":  p.cfg.Name,
			"session": a.handoff.HTTP.SessionID,
			"reason":  err.Error(),
		})
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		in.Client.Abandon()
		return
	}
	p.instances = append(p.instances, in)
	p.cond.Broadcast()
}

// PoolID is the identity this pool's process definition shares with others:
// its configuration and the executable it was created to run. The executable
// is the one recorded when the pool was made, not the one on disk now, so a
// binary replaced since then makes a successor or a reload start a new pool
// rather than adopt children running the old program.
func (p *Pool) PoolID() string { return p.cfg.PoolID() + "/" + p.ExecIdentity() }

// ExecIdentity is the executable identity the pool's children are running: the
// one recorded when the pool was made, or the one a restart for a changed
// executable moved it to.
func (p *Pool) ExecIdentity() string {
	p.execMu.Lock()
	defer p.execMu.Unlock()
	return p.exec
}
