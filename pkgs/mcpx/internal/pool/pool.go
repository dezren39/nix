// Package pool manages the live MCP server processes behind each namespace.
//
// The central problem it solves is that MCP servers fall into two very
// different classes:
//
//   - Stateless (search, docs, database). One process can serve any number of
//     concurrent callers because JSON-RPC ids multiplex cleanly. Mode "shared".
//
//   - Stateful (chrome-devtools and friends). The server holds a browser, a
//     selected page, a scroll position. Two agents interleaving calls on one
//     process corrupt each other. Modes "pooled" and "session" give each
//     caller its own process.
//
// "session" mode is the important one: a lease is pinned to a session key for
// the whole script run, so navigate -> snapshot -> click all reach the same
// browser, while a concurrent run gets a different browser entirely.
package pool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpclient"
)

// Version reported to MCP servers during initialize.
var Version = "dev"

// Trace, when set, receives a line per tool call naming the instance that
// served it. The daemon wires this to its logger when MCPX_TRACE is set.
var Trace func(string, ...any)

// Instance is one live MCP server process (or remote session).
type Instance struct {
	ID        string
	Client    *mcpclient.Client
	transport mcpclient.Transport

	busy      bool
	session   string
	lastUsed  time.Time
	startedAt time.Time
	calls     atomic.Int64
}

// PID returns the child process id for stdio instances, 0 otherwise.
func (i *Instance) PID() int {
	if st, ok := i.transport.(*mcpclient.StdioTransport); ok {
		return st.PID()
	}
	return 0
}

// Lease is a borrowed instance. Release must be called exactly once.
type Lease struct {
	inst *Instance
	pool *Pool
	done bool
}

// Client is the MCP session behind the lease.
func (l *Lease) Client() *mcpclient.Client { return l.inst.Client }

// InstanceID identifies the process serving this lease.
func (l *Lease) InstanceID() string { return l.inst.ID }

// Release returns the instance to the pool.
func (l *Lease) Release() {
	if l == nil || l.done {
		return
	}
	l.done = true
	l.pool.release(l.inst)
}

// Pool owns every instance of a single configured server.
type Pool struct {
	cfg *config.Resolved

	mu        sync.Mutex
	cond      *sync.Cond
	instances []*Instance
	starting  int
	seq       int
	closed    bool

	// schema cache
	schemaMu  sync.RWMutex
	tools     []mcpclient.Tool
	resources []mcpclient.Resource
	schemaAt  time.Time
	schemaErr error

	lastErr   error
	failCount int
	// cooldownUntil throttles restart storms after repeated start failures.
	cooldownUntil time.Time
}

// New creates an empty pool. No process is started until first use.
func New(cfg *config.Resolved) *Pool {
	p := &Pool{cfg: cfg}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// Config exposes the resolved server config.
func (p *Pool) Config() *config.Resolved { return p.cfg }

// Name is the configured server name.
func (p *Pool) Name() string { return p.cfg.Name }

// Namespace is the TypeScript namespace for this server.
func (p *Pool) Namespace() string { return p.cfg.Namespace }

var errClosed = errors.New("pool closed")

// Acquire borrows an instance. sessionKey may be empty for stateless use.
func (p *Pool) Acquire(ctx context.Context, sessionKey string) (*Lease, error) {
	p.mu.Lock()

	for {
		if p.closed {
			p.mu.Unlock()
			return nil, errClosed
		}

		// Drop dead instances before deciding anything.
		p.reapDeadLocked()

		// Session affinity: reuse the pinned instance if we already have one.
		if p.cfg.Mode == config.ModeSession && sessionKey != "" {
			for _, in := range p.instances {
				if in.session == sessionKey {
					in.busy = true
					in.lastUsed = time.Now()
					p.mu.Unlock()
					return &Lease{inst: in, pool: p}, nil
				}
			}
		}

		// Shared mode: one instance, unlimited concurrent callers.
		if p.cfg.Mode == config.ModeShared {
			if len(p.instances) > 0 {
				in := p.instances[0]
				in.busy = true
				in.lastUsed = time.Now()
				p.mu.Unlock()
				return &Lease{inst: in, pool: p}, nil
			}
		} else {
			// Pooled/session: find an idle, unpinned instance.
			for _, in := range p.instances {
				if !in.busy && in.session == "" {
					in.busy = true
					in.session = sessionKey
					in.lastUsed = time.Now()
					p.mu.Unlock()
					return &Lease{inst: in, pool: p}, nil
				}
			}
		}

		// Room to grow?
		if len(p.instances)+p.starting < p.cfg.Max {
			if cd := p.cooldownUntil; time.Now().Before(cd) {
				err := p.lastErr
				p.mu.Unlock()
				return nil, fmt.Errorf("server %q is in restart cooldown for %s: %w",
					p.cfg.Name, time.Until(cd).Truncate(time.Millisecond), err)
			}
			p.starting++
			p.mu.Unlock()

			in, err := p.start(ctx)

			p.mu.Lock()
			p.starting--
			if err != nil {
				p.failCount++
				p.lastErr = err
				// Exponential-ish backoff capped at 30s.
				backoff := time.Duration(p.failCount) * 2 * time.Second
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
				p.cooldownUntil = time.Now().Add(backoff)
				p.cond.Broadcast()
				p.mu.Unlock()
				return nil, err
			}
			p.failCount = 0
			p.lastErr = nil
			p.cooldownUntil = time.Time{}
			in.busy = true
			if p.cfg.Mode != config.ModeShared {
				in.session = sessionKey
			}
			in.lastUsed = time.Now()
			p.instances = append(p.instances, in)
			p.cond.Broadcast()
			p.mu.Unlock()
			return &Lease{inst: in, pool: p}, nil
		}

		// At capacity: wait for a release, honouring ctx.
		if err := p.waitLocked(ctx); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
}

// waitLocked blocks on the condition variable but returns early if ctx ends.
func (p *Pool) waitLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("waiting for a free %q instance (max=%d): %w", p.cfg.Name, p.cfg.Max, err)
	}
	done := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.cond.Broadcast()
			p.mu.Unlock()
		case <-stop:
		}
		close(done)
	}()
	p.cond.Wait()
	close(stop)
	<-done
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("waiting for a free %q instance (max=%d): %w", p.cfg.Name, p.cfg.Max, err)
	}
	return nil
}

func (p *Pool) release(in *Instance) {
	p.mu.Lock()
	in.busy = false
	in.lastUsed = time.Now()
	// Session pins survive release; they are cleared by ReleaseSession or the
	// idle reaper. Pooled and shared leases free immediately.
	if p.cfg.Mode == config.ModePooled {
		in.session = ""
	}
	p.cond.Broadcast()
	p.mu.Unlock()
}

// ReleaseSession unpins and stops instances held for a session key.
func (p *Pool) ReleaseSession(key string) int {
	if key == "" {
		return 0
	}
	p.mu.Lock()
	var stop []*Instance
	kept := p.instances[:0]
	for _, in := range p.instances {
		if in.session == key && !in.busy {
			stop = append(stop, in)
			continue
		}
		if in.session == key && in.busy {
			// Still running a call; unpin so it is reaped when it finishes.
			in.session = ""
		}
		kept = append(kept, in)
	}
	p.instances = kept
	p.cond.Broadcast()
	p.mu.Unlock()

	for _, in := range stop {
		_ = in.Client.Close()
	}
	return len(stop)
}

func (p *Pool) reapDeadLocked() {
	kept := p.instances[:0]
	for _, in := range p.instances {
		if in.Client.Alive() {
			kept = append(kept, in)
			continue
		}
		if p.lastErr == nil {
			p.lastErr = in.Client.Err()
		}
		go in.Client.Close()
	}
	p.instances = kept
}

func (p *Pool) start(ctx context.Context) (*Instance, error) {
	sctx, cancel := context.WithTimeout(ctx, p.cfg.StartTimeout)
	defer cancel()

	var (
		tr  mcpclient.Transport
		err error
	)
	if p.cfg.Stdio() {
		tr, err = mcpclient.NewStdio(mcpclient.StdioOptions{
			Command:    p.cfg.Command,
			Args:       p.cfg.Args,
			Env:        p.cfg.Env,
			Cwd:        p.cfg.Cwd,
			InheritEnv: true,
		})
	} else {
		tr, err = mcpclient.NewHTTP(mcpclient.HTTPOptions{
			URL:     p.cfg.URL,
			Headers: p.cfg.Headers,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("server %q: %w", p.cfg.Name, err)
	}

	cl, err := mcpclient.New(sctx, tr, "mcpx", Version)
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("server %q: %w", p.cfg.Name, err)
	}

	p.mu.Lock()
	p.seq++
	id := fmt.Sprintf("%s#%d", p.cfg.Name, p.seq)
	p.mu.Unlock()

	return &Instance{
		ID:        id,
		Client:    cl,
		transport: tr,
		startedAt: time.Now(),
		lastUsed:  time.Now(),
	}, nil
}

// Schemas returns the cached tool and resource lists, fetching them on first
// call. Every later call is a map read, which is what makes `mcpx ls` instant.
func (p *Pool) Schemas(ctx context.Context) ([]mcpclient.Tool, []mcpclient.Resource, error) {
	p.schemaMu.RLock()
	if !p.schemaAt.IsZero() {
		t, r, e := p.tools, p.resources, p.schemaErr
		p.schemaMu.RUnlock()
		return t, r, e
	}
	p.schemaMu.RUnlock()
	return p.RefreshSchemas(ctx)
}

// RefreshSchemas re-reads tools/resources from a live instance.
//
// On failure the cache timestamp is deliberately left unset. Recording a
// timestamp would make a server that never started look like one that
// genuinely exposes no tools, and that state would then be persisted and
// reloaded, permanently hiding the failure.
func (p *Pool) RefreshSchemas(ctx context.Context) ([]mcpclient.Tool, []mcpclient.Resource, error) {
	lease, err := p.Acquire(ctx, "")
	if err != nil {
		p.schemaMu.Lock()
		p.schemaErr = err
		p.schemaMu.Unlock()
		return nil, nil, err
	}
	defer lease.Release()

	cl := lease.Client()
	tools, terr := cl.ListTools(ctx)
	if terr != nil {
		err := fmt.Errorf("server %q tools/list: %w", p.cfg.Name, terr)
		p.schemaMu.Lock()
		p.schemaErr = err
		p.schemaMu.Unlock()
		return nil, nil, err
	}
	tools = p.filterTools(tools)

	var resources []mcpclient.Resource
	if cl.Supports("resources") {
		resources, _ = cl.ListResources(ctx)
	}

	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	p.schemaMu.Lock()
	p.tools, p.resources, p.schemaErr, p.schemaAt = tools, resources, nil, time.Now()
	p.schemaMu.Unlock()
	return tools, resources, nil
}

// SetSchemas seeds the cache from disk so the daemon can answer discovery
// queries without starting a single child process.
func (p *Pool) SetSchemas(tools []mcpclient.Tool, resources []mcpclient.Resource, at time.Time) {
	p.schemaMu.Lock()
	p.tools, p.resources, p.schemaAt, p.schemaErr = tools, resources, at, nil
	p.schemaMu.Unlock()
}

// CachedSchemas returns whatever is cached without triggering a fetch.
func (p *Pool) CachedSchemas() ([]mcpclient.Tool, []mcpclient.Resource, time.Time) {
	p.schemaMu.RLock()
	defer p.schemaMu.RUnlock()
	return p.tools, p.resources, p.schemaAt
}

func (p *Pool) filterTools(in []mcpclient.Tool) []mcpclient.Tool {
	if len(p.cfg.Tools) == 0 && len(p.cfg.ExcludeTools) == 0 {
		return in
	}
	out := in[:0]
	for _, t := range in {
		if p.cfg.ExcludeTools[t.Name] {
			continue
		}
		if len(p.cfg.Tools) > 0 && !p.cfg.Tools[t.Name] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Call runs a tool on a leased instance.
func (p *Pool) Call(ctx context.Context, sessionKey, tool string, args any) (json.RawMessage, error) {
	lease, err := p.Acquire(ctx, sessionKey)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	lease.inst.calls.Add(1)
	if Trace != nil {
		Trace("call %s.%s session=%q instance=%s pid=%d", p.cfg.Name, tool, sessionKey, lease.inst.ID, lease.inst.PID())
	}

	cctx, cancel := context.WithTimeout(ctx, p.cfg.CallTimeout)
	defer cancel()
	return lease.Client().CallTool(cctx, tool, args)
}

// ReadResource reads a resource URI on a leased instance.
func (p *Pool) ReadResource(ctx context.Context, sessionKey, uri string) (json.RawMessage, error) {
	lease, err := p.Acquire(ctx, sessionKey)
	if err != nil {
		return nil, err
	}
	defer lease.Release()

	cctx, cancel := context.WithTimeout(ctx, p.cfg.CallTimeout)
	defer cancel()
	return lease.Client().ReadResource(cctx, uri)
}

// ReapIdle stops instances idle beyond the configured timeout. Shared
// instances are kept because they are cheap and hot-path.
func (p *Pool) ReapIdle(now time.Time) int {
	p.mu.Lock()
	var stop []*Instance
	kept := p.instances[:0]
	for _, in := range p.instances {
		idle := now.Sub(in.lastUsed)
		if !in.busy && idle > p.cfg.IdleTimeout && (p.cfg.Mode != config.ModeShared || len(p.instances) > p.cfg.Min) {
			stop = append(stop, in)
			continue
		}
		kept = append(kept, in)
	}
	p.instances = kept
	p.cond.Broadcast()
	p.mu.Unlock()

	for _, in := range stop {
		_ = in.Client.Close()
	}
	return len(stop)
}

// InstanceStatus is a diagnostic snapshot.
type InstanceStatus struct {
	ID        string `json:"id"`
	PID       int    `json:"pid,omitempty"`
	Busy      bool   `json:"busy"`
	Session   string `json:"session,omitempty"`
	Calls     int64  `json:"calls"`
	UptimeSec int    `json:"uptimeSec"`
	IdleSec   int    `json:"idleSec"`
}

// Status describes the pool.
type Status struct {
	Name      string           `json:"name"`
	Namespace string           `json:"namespace"`
	Mode      string           `json:"mode"`
	Max       int              `json:"max"`
	Live      int              `json:"live"`
	Tools     int              `json:"tools"`
	SchemaAge string           `json:"schemaAge,omitempty"`
	LastError string           `json:"lastError,omitempty"`
	Instances []InstanceStatus `json:"instances,omitempty"`
}

// Status snapshots the pool for `mcpx status`.
func (p *Pool) Status() Status {
	p.mu.Lock()
	st := Status{
		Name:      p.cfg.Name,
		Namespace: p.cfg.Namespace,
		Mode:      string(p.cfg.Mode),
		Max:       p.cfg.Max,
		Live:      len(p.instances),
	}
	if p.lastErr != nil {
		st.LastError = p.lastErr.Error()
	}
	now := time.Now()
	for _, in := range p.instances {
		st.Instances = append(st.Instances, InstanceStatus{
			ID:        in.ID,
			PID:       in.PID(),
			Busy:      in.busy,
			Session:   in.session,
			Calls:     in.calls.Load(),
			UptimeSec: int(now.Sub(in.startedAt).Seconds()),
			IdleSec:   int(now.Sub(in.lastUsed).Seconds()),
		})
	}
	p.mu.Unlock()

	p.schemaMu.RLock()
	st.Tools = len(p.tools)
	if !p.schemaAt.IsZero() {
		st.SchemaAge = time.Since(p.schemaAt).Truncate(time.Second).String()
	}
	if p.schemaErr != nil && st.LastError == "" {
		st.LastError = p.schemaErr.Error()
	}
	p.schemaMu.RUnlock()
	return st
}

// Restart stops every instance. The next Acquire starts fresh ones.
func (p *Pool) Restart() int {
	p.mu.Lock()
	stop := p.instances
	p.instances = nil
	p.failCount = 0
	p.lastErr = nil
	p.cooldownUntil = time.Time{}
	p.cond.Broadcast()
	p.mu.Unlock()
	for _, in := range stop {
		_ = in.Client.Close()
	}
	return len(stop)
}

// Close shuts the pool down permanently.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	stop := p.instances
	p.instances = nil
	p.cond.Broadcast()
	p.mu.Unlock()
	for _, in := range stop {
		_ = in.Client.Close()
	}
}
