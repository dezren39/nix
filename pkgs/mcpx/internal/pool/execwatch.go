package pool

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// ExecCheckInterval is how often a pool with a live child re-stats the
// executable its command resolves to. It is read when a pool is made, so a
// test sets it before the registry is built. Zero turns the check off.
var ExecCheckInterval = defaults.ExecCheckInterval

// ExecDrainTimeout bounds how long a child being replaced waits for the calls
// already running on it. Read when a replacement happens.
var ExecDrainTimeout = defaults.ExecDrainTimeout

// A pool whose command is re-pointed, re-installed or rewritten under a
// running server (an npx or uvx cache update, a nix profile that moves to a new
// store path) would otherwise keep the old program until something reloaded
// it. This is the periodic check that notices, and the restart that follows.
//
// The check is two steps. A stat of the resolved file is the cheap part, and
// it decides whether anything needs doing at all. Only when the stat moved is
// the identity worked out again, and only a changed identity restarts the
// pool's children. The restart is for this pool alone: its neighbours keep
// their processes.

// watchExecLocked starts the periodic check the first time the pool has a live
// child. A pool that never starts one has nothing to restart and costs
// nothing. Called with p.mu held.
func (p *Pool) watchExecLocked() {
	if p.watching || p.execEvery <= 0 || !p.cfg.Stdio() {
		return
	}
	p.watching = true
	go p.watchExec(p.execEvery)
}

func (p *Pool) watchExec(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-p.execStop:
			return
		case <-t.C:
			if p.isClosed() {
				return
			}
			p.checkExec()
		}
	}
}

func (p *Pool) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// checkExec compares the executable the command resolves to now with the one
// the pool was made from. Only the watch goroutine calls it, so the stamp and
// the identity it compares against are not changing under it.
func (p *Pool) checkExec() {
	if !p.cfg.Stdio() {
		return
	}
	st, found := p.cfg.ExecStamp()

	p.execMu.Lock()
	unchanged := found && st == p.execStamp
	p.execMu.Unlock()
	if unchanged {
		return
	}

	id := ""
	if found {
		var err error
		if id, err = st.Identity(); err != nil {
			found = false
		}
	}
	if !found {
		p.execGone(p.cfg.Command)
		return
	}

	p.execMu.Lock()
	wasMissing := p.execMissing
	p.execMissing = false
	old := p.exec
	p.exec = id
	p.execStamp = st
	p.execMu.Unlock()

	if id == old {
		// The file was touched or re-pointed at the same bytes. The stamp is
		// now current, and the child keeps running.
		if wasMissing {
			lifecycle("server.exec", map[string]any{
				"server": p.cfg.Name, "state": "restored", "path": st.Path,
				"identity": id, "restarted": false,
			})
		}
		return
	}
	lifecycle("server.restart", map[string]any{
		"server": p.cfg.Name, "reason": "executable changed", "path": st.Path,
		"oldIdentity": old, "newIdentity": id,
	})
	p.replaceChildren()
}

// execGone records that the executable cannot be read. The running child is
// kept, since it still has the program open, and the warning is logged once
// rather than on every check until the file returns.
func (p *Pool) execGone(path string) {
	p.execMu.Lock()
	first := !p.execMissing
	p.execMissing = true
	p.execMu.Unlock()
	if first {
		lifecycle("server.warning", map[string]any{
			"server": p.cfg.Name, "reason": "executable missing; keeping the running child and checking again",
			"path": path,
		})
	}
}

// replaceChildren restarts this pool's live children after the executable
// they run changed. Each child is retired at once, so no new call reaches it;
// the calls already running on it have ExecDrainTimeout to finish; then it is
// closed gracefully and a replacement starts under the scope key it served.
//
// A caller that arrives meanwhile waits for the replacement rather than
// starting a rival, because each child keeps its slot until its replacement is
// in the list. A call that outlives the drain window fails with an error that
// names the restart.
func (p *Pool) replaceChildren() {
	p.mu.Lock()
	var old []*Instance
	for _, in := range p.instances {
		if !in.retiring {
			in.retiring = true
			p.reserveSlotLocked(in)
			old = append(old, in)
		}
	}
	p.mu.Unlock()

	for _, in := range old {
		p.replaceChild(in)
	}
}

// retire takes a child out of service with no replacement: no new call reaches
// it, the calls running on it have the drain window to finish, and then it is
// closed and removed. The caller holds the child's slot while this runs, so a
// caller that arrives meanwhile waits rather than starting a rival. drain is
// passed in, not read here, because a lazy restart runs this in the background.
// stopReason goes to the lifecycle log; callErr is how a call cut off by the
// drain window is described (see replacedErr).
func (p *Pool) retire(in *Instance, drain time.Duration, stopReason, callErr string) {
	if !p.drain(in, drain) {
		lifecycle("server.warning", map[string]any{
			"server": p.cfg.Name, "instance": in.ID,
			"reason": "calls still running at the drain timeout are cut off",
		})
	}
	in.swapped.Store(&callErr)
	p.stopped(in, stopReason)
	_ = in.Client.Close()

	p.mu.Lock()
	p.removeLocked(in)
	p.mu.Unlock()
}

func (p *Pool) replaceChild(in *Instance) {
	replace := p.replaceable(in)
	p.retire(in, ExecDrainTimeout, "executable changed", "restarted for a changed executable")

	if !replace {
		p.mu.Lock()
		p.releaseSlotLocked(in)
		p.cond.Broadcast()
		p.mu.Unlock()
		return
	}
	repl, err := p.startLane(context.Background(), in.legacyLane)

	p.mu.Lock()
	p.releaseSlotLocked(in)
	if err != nil {
		p.lastErr = err
		p.cond.Broadcast()
		p.mu.Unlock()
		lifecycle("server.warning", map[string]any{
			"server": p.cfg.Name, "reason": "restart for a changed executable failed; the next call starts one",
			"error": err.Error(),
		})
		return
	}
	// The pool may have closed while the replacement started, or a concurrent
	// Restart or caller may already have made one for this key. Either way the
	// new child is not wanted.
	if p.closed || p.findLaneLocked(in.key, in.legacyLane) != nil {
		p.cond.Broadcast()
		p.mu.Unlock()
		_ = repl.Client.Close()
		return
	}
	repl.key = in.key
	repl.holders = 0
	repl.lastUsed = time.Now()
	p.instances = append(p.instances, repl)
	p.cond.Broadcast()
	p.mu.Unlock()
}

// reserveSlotLocked and releaseSlotLocked hold a child's place in its lane
// while it is replaced, in the same counts a start in progress uses, so a
// caller that finds the lane full waits for the replacement instead of
// starting a rival. Called with p.mu held.
func (p *Pool) reserveSlotLocked(in *Instance) {
	if in.legacyLane {
		p.startingLegacy++
	} else {
		p.starting++
	}
}

func (p *Pool) releaseSlotLocked(in *Instance) {
	if in.legacyLane {
		p.startingLegacy--
	} else {
		p.starting--
	}
}

// replaceable says whether a retired child gets a replacement. A per-call
// child is over when its call is, and a pid-scoped child whose process has
// exited has no caller left to serve.
func (p *Pool) replaceable(in *Instance) bool {
	if strings.HasPrefix(in.key, "call:") {
		return false
	}
	return !(p.cfg.Scope.WatchesPID() && !keyPIDAlive(in.key))
}

// drain waits until the calls running on in have finished, or d has passed,
// and reports whether they all did. Each call's end broadcasts the pool's
// condition variable, so the wait is woken by the last one; a timer wakes it
// at the deadline.
func (p *Pool) drain(in *Instance, d time.Duration) bool {
	deadline := time.Now().Add(d)
	timer := time.AfterFunc(d, func() {
		p.mu.Lock()
		p.cond.Broadcast()
		p.mu.Unlock()
	})
	defer timer.Stop()

	p.mu.Lock()
	defer p.mu.Unlock()
	for in.holders > 0 && time.Now().Before(deadline) {
		p.cond.Wait()
	}
	return in.holders == 0
}

// replacedErr says what a call that failed on a child taken down by a restart
// reports. The tool did not fail; its server was replaced under it.
func (p *Pool) replacedErr(in *Instance, err error) error {
	why := in.swapped.Load()
	if err == nil || why == nil {
		return err
	}
	return fmt.Errorf("server %q was %s while this call was running: %w", p.cfg.Name, *why, err)
}
