package daemon

import (
	"errors"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/pool"
)

// RegistryHandoff is the registry's pools taken out of service for a successor.
type RegistryHandoff struct {
	Pools    []pool.PoolHandoff
	detached []*pool.Pool
}

// Detach takes every pool out of service. Names may share a pool, which is
// detached once. A pool with calls still in flight is waited for, up to wait,
// since detaching it would fail those calls.
func (r *Registry) Detach(wait time.Duration) (*RegistryHandoff, error) {
	r.mu.RLock()
	seen := map[*pool.Pool]bool{}
	var pools []*pool.Pool
	for _, name := range r.order {
		if p := r.pools[name]; !seen[p] {
			seen[p] = true
			pools = append(pools, p)
		}
	}
	r.mu.RUnlock()

	deadline := time.Now().Add(wait)
	for {
		h, err := detachAll(pools)
		if !errors.Is(err, pool.ErrBusy) || !time.Now().Before(deadline) {
			return h, err
		}
		time.Sleep(defaults.HandoffRetryInterval)
	}
}

func detachAll(pools []*pool.Pool) (*RegistryHandoff, error) {
	h := &RegistryHandoff{}
	for _, p := range pools {
		ph, err := p.Detach()
		if err != nil {
			h.Restore()
			return nil, err
		}
		h.detached = append(h.detached, p)
		if len(ph.Instances) > 0 {
			h.Pools = append(h.Pools, *ph)
		}
	}
	return h, nil
}

// Commit closes what was not handed on, once the successor has taken the rest.
func (h *RegistryHandoff) Commit() {
	for _, p := range h.detached {
		p.Commit()
	}
}

// Restore puts every detached pool back, for a handoff the successor refused.
func (h *RegistryHandoff) Restore() {
	for _, p := range h.detached {
		p.Restore()
	}
	h.detached = nil
}

// Adoption is a successor's children, held until the handoff commits. A child
// is served by the pool with the same identity; one whose pool this
// configuration no longer has is closed after commit, and its server starts
// again from the new definition on its next use.
type Adoption struct {
	all     []*pool.Adopted
	byPool  map[*pool.Pool][]*pool.Adopted
	seq     map[*pool.Pool]int
	orphans []*pool.Adopted
}

// Adopt takes the children of every handed pool. Nothing reads them yet.
func (r *Registry) Adopt(hs []pool.PoolHandoff) (*Adoption, error) {
	byID := map[string]*pool.Pool{}
	for _, name := range r.order {
		p := r.pools[name]
		if _, ok := byID[p.PoolID()]; !ok {
			byID[p.PoolID()] = p
		}
	}
	a := &Adoption{byPool: map[*pool.Pool][]*pool.Adopted{}, seq: map[*pool.Pool]int{}}
	for _, h := range hs {
		ads, err := pool.Adopt(h.Instances)
		a.all = append(a.all, ads...)
		if err != nil {
			a.Abort()
			return nil, err
		}
		p, ok := byID[h.PoolID]
		if !ok {
			a.orphans = append(a.orphans, ads...)
			continue
		}
		sessions, err := p.AdoptSessions(h.Instances)
		a.all = append(a.all, sessions...)
		if err != nil {
			a.Abort()
			return nil, err
		}
		ads = append(ads, sessions...)
		a.byPool[p] = append(a.byPool[p], ads...)
		if h.Seq > a.seq[p] {
			a.seq[p] = h.Seq
		}
	}
	return a, nil
}

// Abort gives every adopted child back, for a handoff that did not commit.
func (a *Adoption) Abort() {
	for _, ad := range a.all {
		ad.Abort()
	}
	a.all = nil
}

// Attach serves the adopted children and closes the orphans. Call it only
// after the handoff has committed.
func (a *Adoption) Attach() {
	for p, ads := range a.byPool {
		p.Attach(ads, a.seq[p])
	}
	for _, o := range a.orphans {
		o.Close()
	}
}
