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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpclient"
)

// Version reported to MCP servers during initialize.
var Version = "dev"

// Trace, when set, receives a line per tool call naming the instance that
// served it. The daemon wires this to its logger when MCPX_TRACE is set.
var Trace func(string, ...any)

// Lifecycle, when set, receives structured lifecycle events: a server
// starting, stopping or being reaped. Reported here rather than inferred from
// logs so that a consumer sees the same events the pool acted on.
var Lifecycle func(event string, attrs map[string]any)

func lifecycle(event string, attrs map[string]any) {
	if Lifecycle != nil {
		Lifecycle(event, attrs)
	}
}

// Instance is one live MCP server process (or remote session).
type Instance struct {
	ID        string
	Client    *mcpclient.Client
	transport mcpclient.Transport

	// key is the scope-resolved identity this instance serves. One live
	// instance per distinct key.
	key string
	// holders counts current callers. Exclusive sharing admits one; shared
	// sharing admits any number.
	holders   int
	lastUsed  time.Time
	startedAt time.Time
	trace     string
	calls     atomic.Int64
}

// Trace is this instance's identifier, carried by every record about it.
func (i *Instance) Trace() string { return i.trace }

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
	schemaMu     sync.RWMutex
	tools        []mcpclient.Tool
	resources    []mcpclient.Resource
	instructions string
	schemaAt     time.Time
	schemaErr    error

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

// Acquire borrows an instance for a resolved scope key.
//
// The key decides *which* process; Sharing decides whether that process may
// serve more than one caller at once. Those are independent, which is why they
// are separate config axes.
func (p *Pool) Acquire(ctx context.Context, key string) (*Lease, error) {
	exclusive := p.cfg.Sharing == config.SharingExclusive

	p.mu.Lock()
	for {
		if p.closed {
			p.mu.Unlock()
			return nil, errClosed
		}
		p.reapDeadLocked()

		// An instance already serving this key is the only correct choice:
		// the key is the caller's identity, and a second process would mean a
		// second browser, a second index, a second anything.
		if in := p.findLocked(key); in != nil {
			if !exclusive || in.holders == 0 {
				in.holders++
				in.lastUsed = time.Now()
				p.mu.Unlock()
				return &Lease{inst: in, pool: p}, nil
			}
			// Exclusive and busy: queue rather than start a rival process.
			if err := p.waitLocked(ctx); err != nil {
				p.mu.Unlock()
				return nil, err
			}
			continue
		}

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
				backoff := time.Duration(p.failCount) * defaults.RestartBackoffStep
				if backoff > 30*time.Second {
					backoff = defaults.RestartBackoffMax
				}
				p.cooldownUntil = time.Now().Add(backoff)
				p.cond.Broadcast()
				p.mu.Unlock()
				return nil, err
			}
			p.failCount = 0
			p.lastErr = nil
			p.cooldownUntil = time.Time{}
			// Another caller may have created this key while the lock was
			// released; keep theirs and retire the duplicate.
			if dup := p.findLocked(key); dup != nil {
				p.cond.Broadcast()
				p.mu.Unlock()
				go in.Client.Close()
				p.mu.Lock()
				continue
			}
			in.key = key
			in.holders = 1
			in.lastUsed = time.Now()
			p.instances = append(p.instances, in)
			p.cond.Broadcast()
			p.mu.Unlock()
			return &Lease{inst: in, pool: p}, nil
		}

		// At capacity. An idle instance serving a key nobody is using can be
		// retired to make room, which is what keeps a per-call scope from
		// deadlocking at Max.
		if in := p.evictableLocked(); in != nil {
			p.removeLocked(in)
			p.mu.Unlock()
			_ = in.Client.Close()
			p.mu.Lock()
			continue
		}

		if err := p.waitLocked(ctx); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
}

// findLocked returns the instance serving key, if any.
func (p *Pool) findLocked(key string) *Instance {
	for _, in := range p.instances {
		if in.key == key {
			return in
		}
	}
	return nil
}

// evictableLocked picks the least recently used instance with no holders.
func (p *Pool) evictableLocked() *Instance {
	var best *Instance
	for _, in := range p.instances {
		if in.holders > 0 {
			continue
		}
		if best == nil || in.lastUsed.Before(best.lastUsed) {
			best = in
		}
	}
	return best
}

func (p *Pool) removeLocked(target *Instance) {
	kept := p.instances[:0]
	for _, in := range p.instances {
		if in != target {
			kept = append(kept, in)
		}
	}
	p.instances = kept
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
	if in.holders > 0 {
		in.holders--
	}
	in.lastUsed = time.Now()
	p.cond.Broadcast()
	p.mu.Unlock()
}

// ReleaseKey stops the instance serving a key, if it is idle. Used when a
// script exits so a browser goes back immediately instead of waiting out the
// idle timer.
func (p *Pool) ReleaseKey(key string) int {
	if key == "" {
		return 0
	}
	p.mu.Lock()
	in := p.findLocked(key)
	if in == nil || in.holders > 0 {
		p.mu.Unlock()
		return 0
	}
	p.removeLocked(in)
	p.cond.Broadcast()
	p.mu.Unlock()
	p.stopped(in, "released")
	_ = in.Client.Close()
	return 1
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
	launched := time.Now()

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

	in := &Instance{
		ID:        id,
		Client:    cl,
		transport: tr,
		trace:     newTraceID("srv"),
		startedAt: time.Now(),
		lastUsed:  time.Now(),
	}
	lifecycle("server.start", map[string]any{
		"server": p.cfg.Name, "instance": in.ID, "pid": in.PID(),
		"trace": in.trace, "sharing": string(p.cfg.Sharing), "scope": string(p.cfg.Scope),
		"transport": transportName(p.cfg),
		// Time to ready, not time to spawn: the event fires after initialize
		// has answered, so this is when the server could first take a call.
		"readyMs": float64(time.Since(launched).Microseconds()) / 1000,
	})
	return in, nil
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
	// Reading a catalogue is not a caller's work, so it borrows the scope's
	// own key. For a global scope that is the shared instance everyone uses;
	// for anything narrower it is a throwaway, stopped again below so a
	// per-session server does not keep a process nobody asked for.
	key := p.schemaKey()
	lease, err := p.Acquire(ctx, key)
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
	var resources []mcpclient.Resource
	if cl.Supports("resources") {
		resources, _ = cl.ListResources(ctx)
	}

	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	p.schemaMu.Lock()
	p.tools, p.resources, p.schemaErr, p.schemaAt = tools, resources, nil, time.Now()
	p.instructions = cl.Instructions
	p.schemaMu.Unlock()

	lease.Release()
	lease.done = true // Release is idempotent, but be explicit before ReleaseKey
	if p.cfg.Scope != config.ScopeGlobal {
		p.ReleaseKey(key)
	}
	return tools, resources, nil
}

// schemaKey is the instance a catalogue read borrows.
func (p *Pool) schemaKey() string {
	key, _ := p.cfg.Scope.Key(config.CallContext{CallID: "schema"})
	return key
}

// SetSchemas seeds the cache from disk so the daemon can answer discovery
// queries without starting a single child process.
func (p *Pool) SetSchemas(tools []mcpclient.Tool, resources []mcpclient.Resource, instructions string, at time.Time) {
	p.schemaMu.Lock()
	p.tools, p.resources, p.instructions, p.schemaAt, p.schemaErr = tools, resources, instructions, at, nil
	p.schemaMu.Unlock()
}

// Instructions returns the server's own guidance, cached from initialize.
func (p *Pool) Instructions() string {
	p.schemaMu.RLock()
	defer p.schemaMu.RUnlock()
	return p.instructions
}

// CachedSchemas returns whatever is cached without triggering a fetch.
func (p *Pool) CachedSchemas() ([]mcpclient.Tool, []mcpclient.Resource, time.Time) {
	p.schemaMu.RLock()
	defer p.schemaMu.RUnlock()
	return p.tools, p.resources, p.schemaAt
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
	started := time.Now()
	res, err := lease.Client().CallTool(cctx, tool, args)
	// Reported from here rather than from the daemon's HTTP handler because
	// this is the only place that knows which instance served the call. The
	// handler sees a namespace; the log wants the process, so that a slow call
	// can be traced back to the server that was started for it.
	attrs := map[string]any{
		"server": p.cfg.Name, "tool": tool, "instance": lease.inst.ID,
		"pid": lease.inst.PID(), "session": sessionKey,
		"durationMs": float64(time.Since(started).Microseconds()) / 1000,
		"ok":         err == nil,
		"trace":      newTraceID("cal"), "trace.parent": lease.inst.trace,
	}
	if err != nil {
		attrs["error"] = err.Error()
	}
	lifecycle("mcp.call", attrs)
	return res, err
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

// ReapIdle stops instances that nobody holds and that have gone quiet, plus
// any whose watched pid has exited. Min keeps a floor of warm instances.
func (p *Pool) ReapIdle(now time.Time) int {
	p.mu.Lock()
	var stop []*Instance
	kept := p.instances[:0]
	for _, in := range p.instances {
		expired := in.holders == 0 && now.Sub(in.lastUsed) > p.cfg.IdleTimeout &&
			len(p.instances) > p.cfg.Min
		// A pid-scoped instance belongs to a process. When that process is
		// gone the instance has no possible future caller, so it goes
		// immediately rather than waiting out the idle timer.
		orphaned := in.holders == 0 && p.cfg.Scope.WatchesPID() && !keyPIDAlive(in.key)
		if expired || orphaned {
			stop = append(stop, in)
			continue
		}
		kept = append(kept, in)
	}
	p.instances = kept
	p.cond.Broadcast()
	p.mu.Unlock()

	for _, in := range stop {
		p.stopped(in, "idle")
		_ = in.Client.Close()
	}
	return len(stop)
}

// keyPIDAlive reports whether the process a pid-scoped key names still exists.
// An unparseable key is treated as alive so a bug here cannot kill instances.
func keyPIDAlive(key string) bool {
	pid, ok := config.PIDOf(key)
	if !ok {
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// InstanceStatus is a diagnostic snapshot.
type InstanceStatus struct {
	ID        string `json:"id"`
	PID       int    `json:"pid,omitempty"`
	Holders   int    `json:"holders"`
	Key       string `json:"key,omitempty"`
	Calls     int64  `json:"calls"`
	UptimeSec int    `json:"uptimeSec"`
	IdleSec   int    `json:"idleSec"`
}

// Status describes the pool.
type Status struct {
	Name      string           `json:"name"`
	Namespace string           `json:"namespace"`
	Sharing   string           `json:"sharing"`
	Scope     string           `json:"scope"`
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
		Sharing:   string(p.cfg.Sharing),
		Scope:     string(p.cfg.Scope),
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
			Holders:   in.holders,
			Key:       in.key,
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
		p.stopped(in, "restart")
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

// newTraceID mints an identifier. Kept here rather than imported so the pool
// does not depend on the logging package.
func newTraceID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "-0000000000000000"
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func transportName(c *config.Resolved) string {
	if c.Stdio() {
		return "stdio"
	}
	if c.Transport != "" {
		return c.Transport
	}
	return "http"
}

// stopped reports a server going away, with why.
func (p *Pool) stopped(in *Instance, reason string) {
	lifecycle("server.stop", map[string]any{
		"server": p.cfg.Name, "instance": in.ID, "pid": in.PID(),
		"trace": in.trace, "reason": reason,
		"calls": in.calls.Load(), "uptimeSec": int(time.Since(in.startedAt).Seconds()),
	})
}
